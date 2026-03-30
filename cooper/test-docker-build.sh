#!/bin/bash
# Integration test for Cooper's Docker build process.
# Tests mirror, latest, and pinned configurations.
#
# Prerequisites: Docker Engine running, Go installed.
# Usage: ./test-docker-build.sh [mirror|latest|pinned|all]
#
# Each mode creates isolated images with a test prefix to avoid
# colliding with real Cooper images.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Colors for output.
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS${NC}: $1"; }
fail() { echo -e "${RED}FAIL${NC}: $1"; FAILURES=$((FAILURES + 1)); }
info() { echo -e "${YELLOW}INFO${NC}: $1"; }

FAILURES=0

# ============================================================================
# Step 1: Build the cooper binary
# ============================================================================
info "Building cooper binary..."
go build -o ./cooper . || { fail "go build failed"; exit 1; }
pass "Cooper binary built"

# ============================================================================
# Helper functions
# ============================================================================

setup_test_dir() {
    local mode=$1
    local test_dir="${SCRIPT_DIR}/.test-${mode}"
    rm -rf "$test_dir"
    mkdir -p "$test_dir"

    # Copy the test config.
    cp ".testfiles/config-${mode}.json" "${test_dir}/config.json"

    echo "$test_dir"
}

run_build_test() {
    local mode=$1
    local prefix="test-${mode}-"
    local test_dir
    test_dir=$(setup_test_dir "$mode")

    local proxy_image="${prefix}cooper-proxy"
    local barrel_base_image="${prefix}cooper-barrel-base"
    local barrel_image="${prefix}cooper-barrel"

    info "=== Testing ${mode} mode ==="
    info "Config dir: ${test_dir}"
    info "Image prefix: ${prefix}"

    # Run cooper build with the test config and prefix.
    info "Running cooper build --config ${test_dir} --prefix ${prefix} ..."
    if ./cooper build --config "$test_dir" --prefix "$prefix" 2>&1; then
        pass "${mode}: cooper build succeeded"
    else
        fail "${mode}: cooper build failed"
        return
    fi

    # Assert proxy image exists.
    if docker image inspect "$proxy_image" &>/dev/null; then
        pass "${mode}: proxy image '${proxy_image}' exists"
    else
        fail "${mode}: proxy image '${proxy_image}' not found"
    fi

    # Assert barrel-base image exists.
    if docker image inspect "$barrel_base_image" &>/dev/null; then
        pass "${mode}: barrel-base image '${barrel_base_image}' exists"
    else
        fail "${mode}: barrel-base image '${barrel_base_image}' not found"
    fi

    # Assert barrel image exists.
    if docker image inspect "$barrel_image" &>/dev/null; then
        pass "${mode}: barrel image '${barrel_image}' exists"
    else
        fail "${mode}: barrel image '${barrel_image}' not found"
    fi

    # Assert the config was updated with ContainerVersion (for mirror/pin modes).
    if [ "$mode" != "latest" ]; then
        local container_version
        container_version=$(jq -r '.programming_tools[0].container_version // empty' "${test_dir}/config.json")
        if [ -n "$container_version" ]; then
            pass "${mode}: ContainerVersion populated (${container_version})"
        else
            fail "${mode}: ContainerVersion is empty after build"
        fi
    fi

    # Assert CA cert was generated.
    if [ -f "${test_dir}/ca/cooper-ca.pem" ]; then
        pass "${mode}: CA certificate generated"
    else
        fail "${mode}: CA certificate not found"
    fi

    # Assert generated Dockerfiles exist.
    if [ -f "${test_dir}/cli/Dockerfile" ]; then
        pass "${mode}: CLI Dockerfile generated"
    else
        fail "${mode}: CLI Dockerfile not found"
    fi

    if [ -f "${test_dir}/proxy/proxy.Dockerfile" ]; then
        pass "${mode}: Proxy Dockerfile generated"
    else
        fail "${mode}: Proxy Dockerfile not found"
    fi

    # Assert ACL helper source was staged in proxy build context.
    if [ -f "${test_dir}/proxy/acl-helper/cmd/acl-helper/main.go" ]; then
        pass "${mode}: ACL helper source staged"
    else
        fail "${mode}: ACL helper source not found"
    fi

    # Assert ACL helper go.mod exists.
    if [ -f "${test_dir}/proxy/acl-helper/go.mod" ]; then
        pass "${mode}: ACL helper go.mod staged"
    else
        fail "${mode}: ACL helper go.mod not found"
    fi

    # ================================================================
    # Runtime assertions: spin up images and verify tools are installed
    # ================================================================
    info "${mode}: Running runtime assertions on barrel image..."

    # Helper: run a command inside the barrel image, skipping the entrypoint
    # (which starts socat and pollutes stdout). Use --entrypoint to bypass.
    barrel_run() {
        docker run --rm --entrypoint "" "$barrel_image" "$@" 2>&1
    }

    # Read expected versions from config for version assertions.
    # Helper to get version for a tool (tries pinned_version, then host_version).
    get_tool_version() {
        local tool_type=$1 tool_name=$2
        jq -r ".${tool_type}[] | select(.name==\"${tool_name}\" and .enabled) | .pinned_version // .host_version // empty" "${test_dir}/config.json"
    }
    is_tool_enabled() {
        local tool_type=$1 tool_name=$2
        jq -r ".${tool_type}[] | select(.name==\"${tool_name}\" and .enabled) | .enabled" "${test_dir}/config.json"
    }

    # Helper to assert exact version match.
    assert_version() {
        local tool_name=$1 expected=$2 actual=$3
        if [ -z "$expected" ]; then
            # No version to pin — just check tool exists.
            if [ -n "$actual" ]; then
                pass "${mode}: ${tool_name} installed (${actual})"
            else
                fail "${mode}: ${tool_name} not found"
            fi
        else
            if echo "$actual" | grep -q "$expected"; then
                pass "${mode}: ${tool_name} ${expected} installed"
            else
                fail "${mode}: ${tool_name} ${expected} expected, got: ${actual}"
            fi
        fi
    }

    # --- Programming tools ---

    # Go (exact version)
    local go_version
    go_version=$(get_tool_version programming_tools go)
    if [ -n "$go_version" ]; then
        local actual_go
        actual_go=$(barrel_run go version 2>&1 || true)
        assert_version "Go" "$go_version" "$actual_go"
    fi

    # Node.js (exact version — now pinned via tarball)
    local node_version
    node_version=$(get_tool_version programming_tools node)
    if [ "$(is_tool_enabled programming_tools node)" = "true" ]; then
        local actual_node
        actual_node=$(barrel_run node --version 2>&1 || true)
        assert_version "Node.js" "v${node_version}" "$actual_node"
    fi

    # Python (distro version — can't pin exact, just check installed)
    if [ "$(is_tool_enabled programming_tools python)" = "true" ]; then
        local actual_python
        actual_python=$(barrel_run python3 --version 2>&1 || true)
        assert_version "Python3" "" "$actual_python"
    fi

    # --- AI CLI tools (exact versions for mirror/pin modes) ---

    # Claude Code
    local claude_version
    claude_version=$(get_tool_version ai_tools claude)
    if [ "$(is_tool_enabled ai_tools claude)" = "true" ]; then
        local actual_claude
        actual_claude=$(barrel_run bash -c "claude --version 2>&1 || npm list -g @anthropic-ai/claude-code 2>&1" || true)
        assert_version "Claude Code" "$claude_version" "$actual_claude"
    fi

    # GitHub Copilot CLI
    local copilot_version
    copilot_version=$(get_tool_version ai_tools copilot)
    if [ "$(is_tool_enabled ai_tools copilot)" = "true" ]; then
        local actual_copilot
        actual_copilot=$(barrel_run bash -c "npm list -g @github/copilot 2>&1" || true)
        assert_version "Copilot CLI" "$copilot_version" "$actual_copilot"
    fi

    # OpenAI Codex CLI
    local codex_version
    codex_version=$(get_tool_version ai_tools codex)
    if [ "$(is_tool_enabled ai_tools codex)" = "true" ]; then
        local actual_codex
        actual_codex=$(barrel_run bash -c "npm list -g @openai/codex 2>&1" || true)
        assert_version "Codex CLI" "$codex_version" "$actual_codex"
    fi

    # OpenCode CLI (curl installer to ~/.opencode/bin)
    local opencode_version
    opencode_version=$(get_tool_version ai_tools opencode)
    if [ "$(is_tool_enabled ai_tools opencode)" = "true" ]; then
        local actual_opencode
        actual_opencode=$(barrel_run bash -c 'export PATH="$HOME/.opencode/bin:$PATH"; opencode --version 2>&1 || ls "$HOME/.opencode/bin/" 2>&1' || true)
        assert_version "OpenCode CLI" "$opencode_version" "$actual_opencode"
    fi

    # --- Barrel image structure ---

    # CA cert injected
    local ca_check
    ca_check=$(barrel_run bash -c "test -f /usr/local/share/ca-certificates/cooper-ca.crt && echo found" || true)
    if [ "$ca_check" = "found" ]; then
        pass "${mode}: CA cert injected into barrel"
    else
        fail "${mode}: CA cert not found in barrel image"
    fi

    # Entrypoint exists
    local ep_check
    ep_check=$(barrel_run bash -c "test -f /entrypoint.sh && echo found" || true)
    if [ "$ep_check" = "found" ]; then
        pass "${mode}: Entrypoint script exists in barrel"
    else
        fail "${mode}: Entrypoint script not found in barrel"
    fi

    # Doctor diagnostic script exists
    local doctor_check
    doctor_check=$(barrel_run bash -c "test -x /usr/local/bin/doctor.sh && echo found" || true)
    if [ "$doctor_check" = "found" ]; then
        pass "${mode}: doctor.sh exists in barrel at /usr/local/bin/"
    else
        fail "${mode}: doctor.sh not found in barrel"
    fi

    # --- Proxy image runtime assertions ---
    info "${mode}: Running runtime assertions on proxy image..."

    local proxy_container="test-${mode}-proxy-check"
    docker rm -f "$proxy_container" 2>/dev/null || true

    # Start proxy container briefly to check internals (bypass entrypoint).
    docker run -d --entrypoint "" --name "$proxy_container" "$proxy_image" sleep 30 >/dev/null 2>&1 || true

    # Squid binary exists
    local squid_check
    squid_check=$(docker exec "$proxy_container" which squid 2>&1 || true)
    if echo "$squid_check" | grep -q "squid"; then
        pass "${mode}: Squid installed in proxy"
    else
        fail "${mode}: Squid not found in proxy, got: ${squid_check}"
    fi

    # ACL helper binary exists
    local acl_check
    acl_check=$(docker exec "$proxy_container" test -x /usr/lib/squid/cooper-acl-helper && echo found 2>&1 || true)
    if [ "$acl_check" = "found" ]; then
        pass "${mode}: ACL helper binary in proxy"
    else
        fail "${mode}: ACL helper binary not found in proxy"
    fi

    # socat installed
    local socat_check
    socat_check=$(docker exec "$proxy_container" which socat 2>&1 || true)
    if echo "$socat_check" | grep -q "socat"; then
        pass "${mode}: socat installed in proxy"
    else
        fail "${mode}: socat not found in proxy"
    fi

    # jq installed (for socat config parsing)
    local jq_check
    jq_check=$(docker exec "$proxy_container" which jq 2>&1 || true)
    if echo "$jq_check" | grep -q "jq"; then
        pass "${mode}: jq installed in proxy"
    else
        fail "${mode}: jq not found in proxy"
    fi

    # CA cert in proxy
    local proxy_ca_check
    proxy_ca_check=$(docker exec "$proxy_container" test -f /etc/squid/cooper-ca.pem && echo found 2>&1 || true)
    if [ "$proxy_ca_check" = "found" ]; then
        pass "${mode}: CA cert in proxy"
    else
        fail "${mode}: CA cert not found in proxy"
    fi

    # Cleanup proxy check container.
    docker rm -f "$proxy_container" 2>/dev/null || true
}

cleanup_test() {
    local mode=$1
    local prefix="test-${mode}-"
    local test_dir="${SCRIPT_DIR}/.test-${mode}"

    info "Cleaning up ${mode} test images and containers..."
    docker rm -f "test-${mode}-proxy-check" 2>/dev/null || true
    docker rmi -f "${prefix}cooper-proxy" 2>/dev/null || true
    docker rmi -f "${prefix}cooper-barrel-base" 2>/dev/null || true
    docker rmi -f "${prefix}cooper-barrel" 2>/dev/null || true

    rm -rf "$test_dir"
}

# ============================================================================
# Step 2: Run tests
# ============================================================================

MODE="${1:-all}"

case "$MODE" in
    mirror)
        run_build_test mirror
        ;;
    latest)
        run_build_test latest
        ;;
    pinned)
        run_build_test pinned
        ;;
    all)
        run_build_test mirror
        run_build_test latest
        run_build_test pinned
        ;;
    clean)
        cleanup_test mirror
        cleanup_test latest
        cleanup_test pinned
        info "All test images and directories cleaned up."
        exit 0
        ;;
    *)
        echo "Usage: $0 [mirror|latest|pinned|all|clean]"
        exit 1
        ;;
esac

# ============================================================================
# Step 3: Summary
# ============================================================================

echo ""
echo "============================================"
if [ "$FAILURES" -eq 0 ]; then
    echo -e "${GREEN}All tests passed!${NC}"
else
    echo -e "${RED}${FAILURES} test(s) failed.${NC}"
fi
echo "============================================"
echo ""
echo "To clean up test images: $0 clean"

exit "$FAILURES"

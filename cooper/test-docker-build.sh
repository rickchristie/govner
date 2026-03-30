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

    # Helper: run a command inside the barrel image and check output.
    barrel_run() {
        docker run --rm "$barrel_image" "$@" 2>&1
    }

    # Read expected versions from config for version assertions.
    local go_version node_version python_enabled
    go_version=$(jq -r '.programming_tools[] | select(.name=="go" and .enabled) | .host_version // .pinned_version // empty' "${test_dir}/config.json")
    node_version=$(jq -r '.programming_tools[] | select(.name=="node" and .enabled) | .host_version // .pinned_version // empty' "${test_dir}/config.json")
    python_enabled=$(jq -r '.programming_tools[] | select(.name=="python" and .enabled) | .enabled' "${test_dir}/config.json")

    local claude_enabled copilot_enabled codex_enabled opencode_enabled
    claude_enabled=$(jq -r '.ai_tools[] | select(.name=="claude" and .enabled) | .enabled' "${test_dir}/config.json")
    copilot_enabled=$(jq -r '.ai_tools[] | select(.name=="copilot" and .enabled) | .enabled' "${test_dir}/config.json")
    codex_enabled=$(jq -r '.ai_tools[] | select(.name=="codex" and .enabled) | .enabled' "${test_dir}/config.json")
    opencode_enabled=$(jq -r '.ai_tools[] | select(.name=="opencode" and .enabled) | .enabled' "${test_dir}/config.json")

    # --- Programming tools ---

    # Go
    if [ -n "$go_version" ]; then
        local actual_go
        actual_go=$(barrel_run go version 2>&1 || true)
        if echo "$actual_go" | grep -q "go${go_version}"; then
            pass "${mode}: Go ${go_version} installed"
        else
            fail "${mode}: Go ${go_version} expected, got: ${actual_go}"
        fi
    fi

    # Node.js
    if [ -n "$node_version" ]; then
        local actual_node
        actual_node=$(barrel_run node --version 2>&1 || true)
        if echo "$actual_node" | grep -q "v${node_version}"; then
            pass "${mode}: Node.js ${node_version} installed"
        else
            fail "${mode}: Node.js v${node_version} expected, got: ${actual_node}"
        fi
    fi

    # Python
    if [ "$python_enabled" = "true" ]; then
        local actual_python
        actual_python=$(barrel_run python3 --version 2>&1 || true)
        if echo "$actual_python" | grep -q "Python 3"; then
            pass "${mode}: Python3 installed (${actual_python})"
        else
            fail "${mode}: Python3 not found, got: ${actual_python}"
        fi
    fi

    # --- AI CLI tools ---

    # Claude Code
    if [ "$claude_enabled" = "true" ]; then
        local actual_claude
        actual_claude=$(barrel_run bash -c "claude --version 2>&1 || npm list -g @anthropic-ai/claude-code 2>&1" || true)
        if echo "$actual_claude" | grep -qi "claude\|anthropic"; then
            pass "${mode}: Claude Code installed"
        else
            fail "${mode}: Claude Code not found, got: ${actual_claude}"
        fi
    fi

    # GitHub Copilot CLI
    if [ "$copilot_enabled" = "true" ]; then
        local actual_copilot
        actual_copilot=$(barrel_run bash -c "copilot --version 2>&1 || npm list -g @github/copilot 2>&1" || true)
        if echo "$actual_copilot" | grep -qi "copilot\|github"; then
            pass "${mode}: Copilot CLI installed"
        else
            fail "${mode}: Copilot CLI not found, got: ${actual_copilot}"
        fi
    fi

    # OpenAI Codex CLI
    if [ "$codex_enabled" = "true" ]; then
        local actual_codex
        actual_codex=$(barrel_run bash -c "codex --version 2>&1 || npm list -g @openai/codex 2>&1" || true)
        if echo "$actual_codex" | grep -qi "codex\|openai"; then
            pass "${mode}: Codex CLI installed"
        else
            fail "${mode}: Codex CLI not found, got: ${actual_codex}"
        fi
    fi

    # OpenCode CLI
    if [ "$opencode_enabled" = "true" ]; then
        local actual_opencode
        actual_opencode=$(barrel_run bash -c "opencode --version 2>&1 || which opencode 2>&1" || true)
        if echo "$actual_opencode" | grep -qi "opencode\|/opencode"; then
            pass "${mode}: OpenCode CLI installed"
        else
            fail "${mode}: OpenCode CLI not found, got: ${actual_opencode}"
        fi
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

    # --- Proxy image runtime assertions ---
    info "${mode}: Running runtime assertions on proxy image..."

    local proxy_container="test-${mode}-proxy-check"
    docker rm -f "$proxy_container" 2>/dev/null || true

    # Start proxy container briefly to check internals.
    docker run -d --name "$proxy_container" "$proxy_image" sleep 30 >/dev/null 2>&1 || true

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

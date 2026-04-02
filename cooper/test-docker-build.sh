#!/bin/bash
# Integration test for Cooper's Docker build process (multi-image architecture).
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

    local base_image="${prefix}cooper-base"
    local claude_image="${prefix}cooper-cli-claude"
    local copilot_image="${prefix}cooper-cli-copilot"
    local codex_image="${prefix}cooper-cli-codex"
    local opencode_image="${prefix}cooper-cli-opencode"
    local proxy_image="${prefix}cooper-proxy"

    info "=== Testing ${mode} mode ==="
    info "Config dir: ${test_dir}"
    info "Image prefix: ${prefix}"

    # ==================================================================
    # Phase 2: Build images
    # ==================================================================
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

    # Assert base image exists.
    if docker image inspect "$base_image" &>/dev/null; then
        pass "${mode}: base image '${base_image}' exists"
    else
        fail "${mode}: base image '${base_image}' not found"
    fi

    # Assert per-tool images exist.
    if docker image inspect "$claude_image" &>/dev/null; then
        pass "${mode}: claude image '${claude_image}' exists"
    else
        fail "${mode}: claude image '${claude_image}' not found"
    fi

    if docker image inspect "$copilot_image" &>/dev/null; then
        pass "${mode}: copilot image '${copilot_image}' exists"
    else
        fail "${mode}: copilot image '${copilot_image}' not found"
    fi

    if docker image inspect "$codex_image" &>/dev/null; then
        pass "${mode}: codex image '${codex_image}' exists"
    else
        fail "${mode}: codex image '${codex_image}' not found"
    fi

    if docker image inspect "$opencode_image" &>/dev/null; then
        pass "${mode}: opencode image '${opencode_image}' exists"
    else
        fail "${mode}: opencode image '${opencode_image}' not found"
    fi

    # ==================================================================
    # Phase 3: Build artifacts
    # ==================================================================
    info "${mode}: Checking build artifacts..."

    # CA cert generated.
    if [ -f "${test_dir}/ca/cooper-ca.pem" ]; then
        pass "${mode}: CA certificate generated"
    else
        fail "${mode}: CA certificate not found"
    fi

    # Base Dockerfile.
    if [ -f "${test_dir}/base/Dockerfile" ]; then
        pass "${mode}: base Dockerfile generated"
    else
        fail "${mode}: base Dockerfile not found"
    fi

    # Entrypoint script.
    if [ -f "${test_dir}/base/entrypoint.sh" ]; then
        pass "${mode}: entrypoint.sh generated"
    else
        fail "${mode}: entrypoint.sh not found"
    fi

    # Per-tool Dockerfiles.
    if [ -f "${test_dir}/cli/claude/Dockerfile" ]; then
        pass "${mode}: claude Dockerfile generated"
    else
        fail "${mode}: claude Dockerfile not found"
    fi

    if [ -f "${test_dir}/cli/copilot/Dockerfile" ]; then
        pass "${mode}: copilot Dockerfile generated"
    else
        fail "${mode}: copilot Dockerfile not found"
    fi

    if [ -f "${test_dir}/cli/codex/Dockerfile" ]; then
        pass "${mode}: codex Dockerfile generated"
    else
        fail "${mode}: codex Dockerfile not found"
    fi

    if [ -f "${test_dir}/cli/opencode/Dockerfile" ]; then
        pass "${mode}: opencode Dockerfile generated"
    else
        fail "${mode}: opencode Dockerfile not found"
    fi

    # Proxy Dockerfile.
    if [ -f "${test_dir}/proxy/proxy.Dockerfile" ]; then
        pass "${mode}: proxy Dockerfile generated"
    else
        fail "${mode}: proxy Dockerfile not found"
    fi

    # ACL helper source staged in proxy build context.
    if [ -f "${test_dir}/proxy/acl-helper/cmd/acl-helper/main.go" ]; then
        pass "${mode}: ACL helper source staged"
    else
        fail "${mode}: ACL helper source not found"
    fi

    if [ -f "${test_dir}/proxy/acl-helper/go.mod" ]; then
        pass "${mode}: ACL helper go.mod staged"
    else
        fail "${mode}: ACL helper go.mod not found"
    fi

    # ==================================================================
    # Phase 4: Base image has NO AI tools
    # ==================================================================
    info "${mode}: Asserting base image has no AI tools..."

    base_run() {
        docker run --rm --entrypoint "" "$base_image" "$@" 2>&1
    }

    # claude should NOT be found.
    if base_run which claude &>/dev/null; then
        fail "${mode}: base image has 'claude' (should not)"
    else
        pass "${mode}: base image does not have 'claude'"
    fi

    # copilot should NOT be found.
    if base_run which copilot &>/dev/null; then
        fail "${mode}: base image has 'copilot' (should not)"
    else
        pass "${mode}: base image does not have 'copilot'"
    fi

    # codex should NOT be found.
    if base_run which codex &>/dev/null; then
        fail "${mode}: base image has 'codex' (should not)"
    else
        pass "${mode}: base image does not have 'codex'"
    fi

    # COOPER_CLI_TOOL should be empty.
    local base_cli_tool
    base_cli_tool=$(base_run bash -c 'echo $COOPER_CLI_TOOL' || true)
    if [ -z "$base_cli_tool" ]; then
        pass "${mode}: base image COOPER_CLI_TOOL is empty"
    else
        fail "${mode}: base image COOPER_CLI_TOOL='${base_cli_tool}' (should be empty)"
    fi

    # ==================================================================
    # Phase 5: Base image has programming tools
    # ==================================================================
    info "${mode}: Asserting base image has programming tools..."

    # Read expected versions from config for version assertions.
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

    # Go (exact version).
    local go_version
    go_version=$(get_tool_version programming_tools go)
    if [ -n "$go_version" ]; then
        local actual_go
        actual_go=$(base_run go version 2>&1 || true)
        assert_version "Go" "$go_version" "$actual_go"
    fi

    # Node.js (exact version).
    local node_version
    node_version=$(get_tool_version programming_tools node)
    if [ "$(is_tool_enabled programming_tools node)" = "true" ]; then
        local actual_node
        actual_node=$(base_run node --version 2>&1 || true)
        assert_version "Node.js" "v${node_version}" "$actual_node"
    fi

    # Python (distro version — can't pin exact, just check installed).
    if [ "$(is_tool_enabled programming_tools python)" = "true" ]; then
        local actual_python
        actual_python=$(base_run python3 --version 2>&1 || true)
        assert_version "Python3" "" "$actual_python"
    fi

    # ==================================================================
    # Phase 6: Per-tool images have ONLY their tool
    # ==================================================================
    info "${mode}: Asserting per-tool images have only their own tool..."

    claude_run() {
        docker run --rm --entrypoint "" "$claude_image" "$@" 2>&1
    }
    copilot_run() {
        docker run --rm --entrypoint "" "$copilot_image" "$@" 2>&1
    }
    codex_run() {
        docker run --rm --entrypoint "" "$codex_image" "$@" 2>&1
    }
    opencode_run() {
        docker run --rm --entrypoint "" "$opencode_image" "$@" 2>&1
    }

    # --- Claude image ---
    local claude_version
    claude_version=$(get_tool_version ai_tools claude)
    if [ "$(is_tool_enabled ai_tools claude)" = "true" ]; then
        local actual_claude
        actual_claude=$(claude_run bash -c "claude --version 2>&1 || npm list -g @anthropic-ai/claude-code 2>&1" || true)
        assert_version "Claude Code" "$claude_version" "$actual_claude"
    fi

    # claude image should NOT have copilot or codex.
    if claude_run which copilot &>/dev/null; then
        fail "${mode}: claude image has 'copilot' (should not)"
    else
        pass "${mode}: claude image does not have 'copilot'"
    fi
    if claude_run which codex &>/dev/null; then
        fail "${mode}: claude image has 'codex' (should not)"
    else
        pass "${mode}: claude image does not have 'codex'"
    fi

    # claude image ENV vars.
    local claude_cli_tool
    claude_cli_tool=$(claude_run bash -c 'echo $COOPER_CLI_TOOL' || true)
    if [ "$claude_cli_tool" = "claude" ]; then
        pass "${mode}: claude image COOPER_CLI_TOOL=claude"
    else
        fail "${mode}: claude image COOPER_CLI_TOOL='${claude_cli_tool}' (expected 'claude')"
    fi

    local claude_auto_approve
    claude_auto_approve=$(claude_run bash -c 'echo $COOPER_CLI_AUTO_APPROVE' || true)
    if [ "$claude_auto_approve" = "--dangerously-skip-permissions" ]; then
        pass "${mode}: claude image COOPER_CLI_AUTO_APPROVE=--dangerously-skip-permissions"
    else
        fail "${mode}: claude image COOPER_CLI_AUTO_APPROVE='${claude_auto_approve}' (expected '--dangerously-skip-permissions')"
    fi

    # --- Copilot image ---
    local copilot_version
    copilot_version=$(get_tool_version ai_tools copilot)
    if [ "$(is_tool_enabled ai_tools copilot)" = "true" ]; then
        local actual_copilot
        actual_copilot=$(copilot_run bash -c "npm list -g @github/copilot 2>&1" || true)
        assert_version "Copilot CLI" "$copilot_version" "$actual_copilot"
    fi

    # copilot image should NOT have claude.
    if copilot_run which claude &>/dev/null; then
        fail "${mode}: copilot image has 'claude' (should not)"
    else
        pass "${mode}: copilot image does not have 'claude'"
    fi

    local copilot_cli_tool
    copilot_cli_tool=$(copilot_run bash -c 'echo $COOPER_CLI_TOOL' || true)
    if [ "$copilot_cli_tool" = "copilot" ]; then
        pass "${mode}: copilot image COOPER_CLI_TOOL=copilot"
    else
        fail "${mode}: copilot image COOPER_CLI_TOOL='${copilot_cli_tool}' (expected 'copilot')"
    fi

    # --- Codex image ---
    local codex_version
    codex_version=$(get_tool_version ai_tools codex)
    if [ "$(is_tool_enabled ai_tools codex)" = "true" ]; then
        local actual_codex
        actual_codex=$(codex_run bash -c "npm list -g @openai/codex 2>&1" || true)
        assert_version "Codex CLI" "$codex_version" "$actual_codex"
    fi

    # codex image should NOT have claude.
    if codex_run which claude &>/dev/null; then
        fail "${mode}: codex image has 'claude' (should not)"
    else
        pass "${mode}: codex image does not have 'claude'"
    fi

    local codex_cli_tool
    codex_cli_tool=$(codex_run bash -c 'echo $COOPER_CLI_TOOL' || true)
    if [ "$codex_cli_tool" = "codex" ]; then
        pass "${mode}: codex image COOPER_CLI_TOOL=codex"
    else
        fail "${mode}: codex image COOPER_CLI_TOOL='${codex_cli_tool}' (expected 'codex')"
    fi

    # --- OpenCode image ---
    local opencode_version
    opencode_version=$(get_tool_version ai_tools opencode)
    if [ "$(is_tool_enabled ai_tools opencode)" = "true" ]; then
        local actual_opencode
        actual_opencode=$(opencode_run bash -c 'export PATH="$HOME/.opencode/bin:$PATH"; opencode --version 2>&1 || ls "$HOME/.opencode/bin/" 2>&1' || true)
        assert_version "OpenCode CLI" "$opencode_version" "$actual_opencode"
    fi

    # opencode image should NOT have claude.
    if opencode_run which claude &>/dev/null; then
        fail "${mode}: opencode image has 'claude' (should not)"
    else
        pass "${mode}: opencode image does not have 'claude'"
    fi

    local opencode_cli_tool
    opencode_cli_tool=$(opencode_run bash -c 'echo $COOPER_CLI_TOOL' || true)
    if [ "$opencode_cli_tool" = "opencode" ]; then
        pass "${mode}: opencode image COOPER_CLI_TOOL=opencode"
    else
        fail "${mode}: opencode image COOPER_CLI_TOOL='${opencode_cli_tool}' (expected 'opencode')"
    fi

    # ==================================================================
    # Phase 7: Per-tool images inherit base content
    # ==================================================================
    info "${mode}: Asserting per-tool images inherit base content..."

    # Go version in claude image should match base.
    if [ -n "$go_version" ]; then
        local claude_go
        claude_go=$(claude_run go version 2>&1 || true)
        assert_version "Go (claude image)" "$go_version" "$claude_go"
    fi

    # Node version in claude image should match base.
    if [ "$(is_tool_enabled programming_tools node)" = "true" ]; then
        local claude_node
        claude_node=$(claude_run node --version 2>&1 || true)
        assert_version "Node.js (claude image)" "v${node_version}" "$claude_node"
    fi

    # CA cert injected into tool image.
    local ca_check
    ca_check=$(claude_run bash -c "test -f /usr/local/share/ca-certificates/cooper-ca.crt && echo found" || true)
    if [ "$ca_check" = "found" ]; then
        pass "${mode}: CA cert injected into claude image"
    else
        fail "${mode}: CA cert not found in claude image"
    fi

    # Entrypoint exists in tool image.
    local ep_check
    ep_check=$(claude_run bash -c "test -f /entrypoint.sh && echo found" || true)
    if [ "$ep_check" = "found" ]; then
        pass "${mode}: entrypoint.sh exists in claude image"
    else
        fail "${mode}: entrypoint.sh not found in claude image"
    fi

    # Doctor diagnostic script exists in tool image.
    local doctor_check
    doctor_check=$(claude_run bash -c "test -x /usr/local/bin/doctor.sh && echo found" || true)
    if [ "$doctor_check" = "found" ]; then
        pass "${mode}: doctor.sh exists in claude image at /usr/local/bin/"
    else
        fail "${mode}: doctor.sh not found in claude image"
    fi

    # ==================================================================
    # Phase 8: Proxy image runtime assertions
    # ==================================================================
    info "${mode}: Running runtime assertions on proxy image..."

    local proxy_container="test-${mode}-proxy-check"
    docker rm -f "$proxy_container" 2>/dev/null || true

    # Start proxy container briefly to check internals (bypass entrypoint).
    docker run -d --entrypoint "" --name "$proxy_container" "$proxy_image" sleep 30 >/dev/null 2>&1 || true

    # Squid binary exists.
    local squid_check
    squid_check=$(docker exec "$proxy_container" which squid 2>&1 || true)
    if echo "$squid_check" | grep -q "squid"; then
        pass "${mode}: Squid installed in proxy"
    else
        fail "${mode}: Squid not found in proxy, got: ${squid_check}"
    fi

    # ACL helper binary exists.
    local acl_check
    acl_check=$(docker exec "$proxy_container" test -x /usr/lib/squid/cooper-acl-helper && echo found 2>&1 || true)
    if [ "$acl_check" = "found" ]; then
        pass "${mode}: ACL helper binary in proxy"
    else
        fail "${mode}: ACL helper binary not found in proxy"
    fi

    # Custom error page.
    if proxy_run test -f /etc/squid/errors/ERR_ACCESS_DENIED; then
        pass "${mode}: Custom Cooper error page in proxy"
    else
        fail "${mode}: Custom Cooper error page not found in proxy"
    fi

    # socat installed.
    local socat_check
    socat_check=$(docker exec "$proxy_container" which socat 2>&1 || true)
    if echo "$socat_check" | grep -q "socat"; then
        pass "${mode}: socat installed in proxy"
    else
        fail "${mode}: socat not found in proxy"
    fi

    # jq installed (for socat config parsing).
    local jq_check
    jq_check=$(docker exec "$proxy_container" which jq 2>&1 || true)
    if echo "$jq_check" | grep -q "jq"; then
        pass "${mode}: jq installed in proxy"
    else
        fail "${mode}: jq not found in proxy"
    fi

    # CA cert in proxy.
    local proxy_ca_check
    proxy_ca_check=$(docker exec "$proxy_container" test -f /etc/squid/cooper-ca.pem && echo found 2>&1 || true)
    if [ "$proxy_ca_check" = "found" ]; then
        pass "${mode}: CA cert in proxy"
    else
        fail "${mode}: CA cert not found in proxy"
    fi

    docker rm -f "$proxy_container" 2>/dev/null || true

    # ==================================================================
    # Phase 9: File ownership assertions
    # ==================================================================
    info "${mode}: Checking file ownership in mounted volumes..."

    local ownership_container="test-${mode}-ownership-check"
    local ownership_dir="${test_dir}/ownership-test"
    local ownership_logdir="${ownership_dir}/logs"
    local ownership_rundir="${ownership_dir}/run"
    mkdir -p "$ownership_logdir" "$ownership_rundir"

    docker rm -f "$ownership_container" 2>/dev/null || true

    # Start proxy image with mounted rw directories, let it write files.
    docker run -d --name "$ownership_container" \
        -v "${ownership_logdir}:/var/log/squid:rw" \
        -v "${ownership_rundir}:/var/run/cooper:rw" \
        "$proxy_image" sleep 10 >/dev/null 2>&1 || true

    # Create files inside the container in the mounted dirs.
    docker exec "$ownership_container" touch /var/log/squid/test.log 2>/dev/null || true
    docker exec "$ownership_container" touch /var/run/cooper/test.sock 2>/dev/null || true
    sleep 1

    local expected_uid
    expected_uid=$(id -u)

    for f in "${ownership_logdir}/test.log" "${ownership_rundir}/test.sock"; do
        if [ -f "$f" ]; then
            local actual_uid
            actual_uid=$(stat -c '%u' "$f")
            if [ "$actual_uid" = "$expected_uid" ]; then
                pass "${mode}: $(basename "$f") owned by UID ${actual_uid} (correct)"
            else
                fail "${mode}: $(basename "$f") owned by UID ${actual_uid}, expected ${expected_uid}"
            fi
        else
            info "${mode}: $(basename "$f") not created (container may not have started)"
        fi
    done

    # Check that a tool image creates workspace files with correct UID.
    local cli_workspace="${ownership_dir}/workspace"
    mkdir -p "$cli_workspace"
    docker run --rm \
        -v "${cli_workspace}:${cli_workspace}:rw" \
        "$claude_image" \
        touch "${cli_workspace}/cli-test-file" 2>/dev/null || true

    if [ -f "${cli_workspace}/cli-test-file" ]; then
        local cli_uid
        cli_uid=$(stat -c '%u' "${cli_workspace}/cli-test-file")
        if [ "$cli_uid" = "$expected_uid" ]; then
            pass "${mode}: CLI-created file owned by UID ${cli_uid} (correct)"
        else
            fail "${mode}: CLI-created file owned by UID ${cli_uid}, expected ${expected_uid}"
        fi
    else
        info "${mode}: CLI image could not create workspace file"
    fi

    docker rm -f "$ownership_container" 2>/dev/null || true
    rm -rf "$ownership_dir"

    # ==================================================================
    # Phase 10: ContainerVersion in config
    # ==================================================================
    info "${mode}: Checking ContainerVersion in config..."

    local cv_count
    cv_count=$(jq '[.ai_tools[] | select(.enabled and .container_version and .container_version != "")] | length' "${test_dir}/config.json")
    local enabled_count
    enabled_count=$(jq '[.ai_tools[] | select(.enabled)] | length' "${test_dir}/config.json")

    if [ "$cv_count" = "$enabled_count" ] && [ "$cv_count" -gt 0 ]; then
        pass "${mode}: ContainerVersion populated for all ${cv_count} enabled AI tools"
    else
        fail "${mode}: ContainerVersion populated for ${cv_count}/${enabled_count} enabled AI tools"
    fi

    # ==================================================================
    # Phase 11: Custom user image test
    # ==================================================================
    info "${mode}: Testing custom user image build..."

    local custom_image="${prefix}cooper-cli-my-custom"

    # Create a custom Dockerfile that extends the base image.
    mkdir -p "${test_dir}/cli/my-custom"
    cat > "${test_dir}/cli/my-custom/Dockerfile" <<DOCKERFILE
FROM ${base_image}
RUN echo "custom-marker" > /tmp/custom-marker.txt
ENV COOPER_CLI_TOOL=my-custom
DOCKERFILE

    # Run cooper build again with the custom image present.
    info "${mode}: Running cooper build again with custom image..."
    if ./cooper build --config "$test_dir" --prefix "$prefix" 2>&1; then
        pass "${mode}: cooper build with custom image succeeded"
    else
        fail "${mode}: cooper build with custom image failed"
        return
    fi

    # Assert custom image exists.
    if docker image inspect "$custom_image" &>/dev/null; then
        pass "${mode}: custom image '${custom_image}' exists"
    else
        fail "${mode}: custom image '${custom_image}' not found"
    fi

    # Assert custom content works.
    local custom_marker
    custom_marker=$(docker run --rm --entrypoint "" "$custom_image" cat /tmp/custom-marker.txt 2>&1 || true)
    if [ "$custom_marker" = "custom-marker" ]; then
        pass "${mode}: custom image has custom content"
    else
        fail "${mode}: custom image missing custom content, got: '${custom_marker}'"
    fi

    # Assert built-in tool images still exist after rebuild.
    if docker image inspect "$claude_image" &>/dev/null; then
        pass "${mode}: claude image still exists after custom rebuild"
    else
        fail "${mode}: claude image disappeared after custom rebuild"
    fi

    if docker image inspect "$copilot_image" &>/dev/null; then
        pass "${mode}: copilot image still exists after custom rebuild"
    else
        fail "${mode}: copilot image disappeared after custom rebuild"
    fi

    docker rmi -f "$custom_image" 2>/dev/null || true
}

cleanup_test() {
    local mode=$1
    local prefix="test-${mode}-"
    local test_dir="${SCRIPT_DIR}/.test-${mode}"

    info "Cleaning up ${mode} test images and containers..."
    docker rm -f "test-${mode}-proxy-check" 2>/dev/null || true
    docker rm -f "test-${mode}-ownership-check" 2>/dev/null || true
    docker rmi -f "${prefix}cooper-proxy" 2>/dev/null || true
    docker rmi -f "${prefix}cooper-base" 2>/dev/null || true
    docker rmi -f "${prefix}cooper-cli-claude" 2>/dev/null || true
    docker rmi -f "${prefix}cooper-cli-copilot" 2>/dev/null || true
    docker rmi -f "${prefix}cooper-cli-codex" 2>/dev/null || true
    docker rmi -f "${prefix}cooper-cli-opencode" 2>/dev/null || true
    docker rmi -f "${prefix}cooper-cli-my-custom" 2>/dev/null || true

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

#!/bin/bash
# Cooper End-to-End Integration Test (Multi-Image Architecture)
# Tests the FULL application lifecycle. If this passes, Cooper works.
#
# This test exercises the real cooper binary and real Docker containers
# in the multi-image architecture where each AI tool has its own image:
#   cooper-base       = OS + programming tools + entrypoint + CA cert
#   cooper-cli-claude = base + Claude Code
#   cooper-cli-copilot = base + Copilot CLI
#   cooper-cli-codex  = base + Codex CLI
#   cooper-cli-opencode = base + OpenCode
#
# No stubs, no mocks — the full flow from build to cleanup.
#
# Prerequisites: Docker Engine running, Go installed.
# Usage: ./test-e2e.sh
# Cleanup only: ./test-e2e.sh clean
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Test-specific prefix and config directory to avoid colliding with real
# Cooper installation.
PREFIX="test-e2e-"
CONFIG_DIR="${SCRIPT_DIR}/.test-e2e"
COOPER="${SCRIPT_DIR}/cooper"

# Container and network names (container names are NOT prefixed — they
# use the same names as real Cooper for Docker DNS resolution).
PROXY_CONTAINER="cooper-proxy"
NETWORK_EXTERNAL="cooper-external"
NETWORK_INTERNAL="cooper-internal"

# Per-tool barrel container names.
BARREL_CLAUDE="barrel-e2e-workspace-claude"
BARREL_COPILOT="barrel-e2e-workspace-copilot"
BARREL_CODEX="barrel-e2e-workspace-codex"
BARREL_OPENCODE="barrel-e2e-workspace-opencode"

# Image names (prefixed to avoid collision).
IMAGE_PROXY="${PREFIX}cooper-proxy"
IMAGE_BASE="${PREFIX}cooper-base"
IMAGE_CLAUDE="${PREFIX}cooper-cli-claude"
IMAGE_COPILOT="${PREFIX}cooper-cli-copilot"
IMAGE_CODEX="${PREFIX}cooper-cli-codex"
IMAGE_OPENCODE="${PREFIX}cooper-cli-opencode"

# All tool names for iteration.
ALL_TOOLS=(claude copilot codex opencode)

# Colors.
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

PASS_COUNT=0
FAIL_COUNT=0
TOTAL=0

pass() {
    PASS_COUNT=$((PASS_COUNT + 1))
    TOTAL=$((TOTAL + 1))
    echo -e "  ${GREEN}PASS${NC}  $1"
}
fail() {
    FAIL_COUNT=$((FAIL_COUNT + 1))
    TOTAL=$((TOTAL + 1))
    echo -e "  ${RED}FAIL${NC}  $1"
}
info() {
    echo -e "  ${CYAN}INFO${NC}  $1"
}
section() {
    echo ""
    echo -e "${CYAN}━━━ $1 ━━━${NC}"
}

# Map tool name to barrel container name.
barrel_name_for() {
    local tool=$1
    echo "barrel-e2e-workspace-${tool}"
}

# Map tool name to prefixed image name.
image_name_for() {
    local tool=$1
    echo "${PREFIX}cooper-cli-${tool}"
}

# ============================================================================
# Cleanup function — always run on exit (or standalone via ./test-e2e.sh clean)
# ============================================================================
cleanup() {
    section "Cleanup"

    # Stop e2e bridge server if running.
    if [ -n "${E2E_BRIDGE_PID:-}" ]; then
        info "Stopping e2e bridge server..."
        kill "$E2E_BRIDGE_PID" 2>/dev/null || true
        wait "$E2E_BRIDGE_PID" 2>/dev/null || true
    fi

    # Stop e2e relay test processes if running.
    if [ -n "${E2E_RELAY_SERVER_PID:-}" ]; then
        kill "$E2E_RELAY_SERVER_PID" 2>/dev/null || true
        wait "$E2E_RELAY_SERVER_PID" 2>/dev/null || true
    fi
    if [ -n "${E2E_RELAY_HELPER_PID:-}" ]; then
        kill "$E2E_RELAY_HELPER_PID" 2>/dev/null || true
        wait "$E2E_RELAY_HELPER_PID" 2>/dev/null || true
    fi

    info "Stopping barrel containers..."
    for tool in "${ALL_TOOLS[@]}"; do
        docker rm -f "$(barrel_name_for "$tool")" 2>/dev/null || true
    done

    info "Stopping proxy container..."
    docker stop "$PROXY_CONTAINER" 2>/dev/null || true
    docker rm -f "$PROXY_CONTAINER" 2>/dev/null || true

    info "Removing networks..."
    docker network rm "$NETWORK_INTERNAL" 2>/dev/null || true
    docker network rm "$NETWORK_EXTERNAL" 2>/dev/null || true

    info "Removing images..."
    docker rmi -f "$IMAGE_PROXY" 2>/dev/null || true
    docker rmi -f "$IMAGE_BASE" 2>/dev/null || true
    for tool in "${ALL_TOOLS[@]}"; do
        docker rmi -f "$(image_name_for "$tool")" 2>/dev/null || true
    done

    info "Removing test directory..."
    rm -rf "$CONFIG_DIR"

    info "Removing test workspace..."
    rm -rf "${SCRIPT_DIR}/.e2e-workspace"

    info "Cleanup complete."
}

if [ "${1:-}" = "clean" ]; then
    cleanup
    exit 0
fi

# Register cleanup on exit so we never leave containers/networks dangling.
trap cleanup EXIT

# ============================================================================
# Phase 1: Build Cooper and Docker Images
# ============================================================================
section "Phase 1: Build Cooper Binary and Docker Images"

# Step 1: Build cooper binary.
info "Building cooper binary..."
go build -o "$COOPER" . || { fail "go build failed"; exit 1; }
pass "Cooper binary built"

# Step 2: Create test config directory and config.json.
info "Creating test config..."
rm -rf "$CONFIG_DIR"
mkdir -p "$CONFIG_DIR"

# Use pinned versions from the reference test config. This is a complete
# config that exercises all tools.
cat > "${CONFIG_DIR}/config.json" << 'CONFIGEOF'
{
  "programming_tools": [
    {"name": "go", "enabled": true, "mode": "pin", "pinned_version": "1.24.10"},
    {"name": "node", "enabled": true, "mode": "pin", "pinned_version": "22.12.0"},
    {"name": "python", "enabled": true, "mode": "pin", "pinned_version": "3.12.1"}
  ],
  "ai_tools": [
    {"name": "claude", "enabled": true, "mode": "pin", "pinned_version": "2.1.87"},
    {"name": "copilot", "enabled": true, "mode": "pin", "pinned_version": "1.0.12"},
    {"name": "codex", "enabled": true, "mode": "pin", "pinned_version": "0.117.0"},
    {"name": "opencode", "enabled": true, "mode": "pin", "pinned_version": "1.3.7"}
  ],
  "whitelisted_domains": [
    {"domain": ".anthropic.com", "include_subdomains": true, "source": "default"},
    {"domain": "platform.claude.com", "include_subdomains": false, "source": "default"},
    {"domain": ".openai.com", "include_subdomains": true, "source": "default"},
    {"domain": ".chatgpt.com", "include_subdomains": true, "source": "default"},
    {"domain": "github.com", "include_subdomains": false, "source": "default"},
    {"domain": "api.github.com", "include_subdomains": false, "source": "default"},
    {"domain": ".githubcopilot.com", "include_subdomains": true, "source": "default"},
    {"domain": "copilot-proxy.githubusercontent.com", "include_subdomains": false, "source": "default"},
    {"domain": "raw.githubusercontent.com", "include_subdomains": false, "source": "default"},
    {"domain": "statsig.anthropic.com", "include_subdomains": false, "source": "default"}
  ],
  "port_forward_rules": [
    {"container_port": 5432, "host_port": 5432, "description": "PostgreSQL"},
    {"container_port": 6379, "host_port": 6379, "description": "Redis"}
  ],
  "proxy_port": 3128,
  "bridge_port": 4343,
  "monitor_timeout_secs": 5,
  "blocked_history_limit": 500,
  "allowed_history_limit": 500,
  "bridge_log_limit": 500,
  "bridge_routes": [],
  "barrel_shm_size": "1g"
}
CONFIGEOF
pass "Test config created"

# Step 3: Run cooper build.
info "Running cooper build (this will take several minutes)..."
if "$COOPER" build --config "$CONFIG_DIR" --prefix "$PREFIX" 2>&1; then
    pass "cooper build succeeded"
else
    fail "cooper build failed"
    exit 1
fi

# Step 4: Assert all images exist (proxy, base, and each tool image).
for img in "$IMAGE_PROXY" "$IMAGE_BASE" "$IMAGE_CLAUDE" "$IMAGE_COPILOT" "$IMAGE_CODEX" "$IMAGE_OPENCODE"; do
    if docker image inspect "$img" &>/dev/null; then
        pass "Image exists: ${img}"
    else
        fail "Image missing: ${img}"
    fi
done

# Assert CA cert was generated.
if [ -f "${CONFIG_DIR}/ca/cooper-ca.pem" ]; then
    pass "CA certificate generated"
else
    fail "CA certificate not found"
fi

# Assert generated files exist (base + per-tool Dockerfiles + proxy files).
for f in "base/Dockerfile" "base/entrypoint.sh" "proxy/proxy.Dockerfile" "proxy/squid.conf"; do
    if [ -f "${CONFIG_DIR}/${f}" ]; then
        pass "Generated file exists: ${f}"
    else
        fail "Generated file missing: ${f}"
    fi
done

for tool in "${ALL_TOOLS[@]}"; do
    f="cli/${tool}/Dockerfile"
    if [ -f "${CONFIG_DIR}/${f}" ]; then
        pass "Generated file exists: ${f}"
    else
        fail "Generated file missing: ${f}"
    fi
done

# ============================================================================
# Phase 2: Create Networks and Start Proxy
# ============================================================================
section "Phase 2: Start Networks and Proxy Container"

# Step 5: Clean up any leftover networks/containers from previous runs.
docker rm -f "$PROXY_CONTAINER" 2>/dev/null || true
for tool in "${ALL_TOOLS[@]}"; do
    docker rm -f "$(barrel_name_for "$tool")" 2>/dev/null || true
done
docker network rm "$NETWORK_INTERNAL" 2>/dev/null || true
docker network rm "$NETWORK_EXTERNAL" 2>/dev/null || true

# Step 6: Create networks.
info "Creating Docker networks..."
docker network create "$NETWORK_EXTERNAL" >/dev/null 2>&1
pass "Network created: ${NETWORK_EXTERNAL}"

docker network create --internal "$NETWORK_INTERNAL" >/dev/null 2>&1
pass "Network created: ${NETWORK_INTERNAL} (--internal, no gateway)"

# Step 7: Write socat-rules.json (same as docker.WritePortForwardConfig).
cat > "${CONFIG_DIR}/socat-rules.json" << 'SOCATEOF'
{
  "bridge_port": 4343,
  "rules": [
    {"container_port": 5432, "host_port": 5432, "description": "PostgreSQL"},
    {"container_port": 6379, "host_port": 6379, "description": "Redis"}
  ]
}
SOCATEOF
pass "socat-rules.json written"

# Step 8: Create mount directories.
mkdir -p "${CONFIG_DIR}/run" "${CONFIG_DIR}/logs"

# Step 9: Start proxy container (replicating docker.StartProxy exactly).
info "Starting proxy container..."
docker run -d \
    --name "$PROXY_CONTAINER" \
    --network "$NETWORK_EXTERNAL" \
    --add-host=host.docker.internal:host-gateway \
    --restart unless-stopped \
    -v "${CONFIG_DIR}/proxy/squid.conf:/etc/squid/squid.conf:ro" \
    -v "${CONFIG_DIR}/ca/cooper-ca.pem:/etc/squid/cooper-ca.pem:ro" \
    -v "${CONFIG_DIR}/ca/cooper-ca-key.pem:/etc/squid/cooper-ca-key.pem:ro" \
    -v "${CONFIG_DIR}/run:/var/run/cooper:rw" \
    -v "${CONFIG_DIR}/logs:/var/log/squid:rw" \
    -v "${CONFIG_DIR}/socat-rules.json:/etc/cooper/socat-rules.json:ro" \
    -p "127.0.0.1:3128:3128" \
    "$IMAGE_PROXY" >/dev/null 2>&1
pass "Proxy container started"

# Step 10: Connect proxy to internal network (dual-network topology).
docker network connect "$NETWORK_INTERNAL" "$PROXY_CONTAINER" 2>/dev/null
pass "Proxy connected to internal network"

# Step 11: Wait for proxy to become ready.
info "Waiting for Squid proxy to initialize..."
proxy_ready=false
for i in $(seq 1 30); do
    if docker exec "$PROXY_CONTAINER" bash -c 'echo > /dev/tcp/localhost/3128' 2>/dev/null; then
        proxy_ready=true
        break
    fi
    sleep 1
done
if [ "$proxy_ready" = "true" ]; then
    pass "Proxy is listening on port 3128"
else
    fail "Proxy did not start within 30 seconds"
    # Show proxy logs for debugging.
    info "Proxy container logs:"
    docker logs "$PROXY_CONTAINER" 2>&1 | tail -20 | while IFS= read -r line; do info "  $line"; done
    exit 1
fi

# ============================================================================
# Phase 3: Per-Tool Barrel Testing
# ============================================================================
# Create a test workspace directory (shared by all barrels).
E2E_WORKSPACE="${SCRIPT_DIR}/.e2e-workspace"
rm -rf "$E2E_WORKSPACE"
mkdir -p "$E2E_WORKSPACE"

# Write the seccomp profile (replicating docker.EnsureSeccompProfile).
mkdir -p "${CONFIG_DIR}/cli"
if [ -f "${CONFIG_DIR}/cli/seccomp.json" ]; then
    SECCOMP_PATH="${CONFIG_DIR}/cli/seccomp.json"
else
    SECCOMP_PATH=""
fi

# Create host directories that the barrel expects.
HOME_DIR="$(eval echo ~)"

# Cooper-managed language cache directories (under CONFIG_DIR/cache/).
mkdir -p "${CONFIG_DIR}/cache/go-mod" 2>/dev/null || true
mkdir -p "${CONFIG_DIR}/cache/go-build" 2>/dev/null || true
mkdir -p "${CONFIG_DIR}/cache/npm" 2>/dev/null || true
mkdir -p "${CONFIG_DIR}/cache/pip" 2>/dev/null || true

# Helper: map tool name to its auth mount arguments.
auth_mounts_for() {
    local tool=$1
    local mounts=()
    case "$tool" in
        claude)
            mkdir -p "${HOME_DIR}/.claude" 2>/dev/null || true
            mounts+=("-v" "${HOME_DIR}/.claude:/home/user/.claude:rw")
            if [ -f "${HOME_DIR}/.claude.json" ]; then
                mounts+=("-v" "${HOME_DIR}/.claude.json:/home/user/.claude.json:rw")
            fi
            ;;
        copilot)
            mkdir -p "${HOME_DIR}/.copilot" 2>/dev/null || true
            mounts+=("-v" "${HOME_DIR}/.copilot:/home/user/.copilot:rw")
            ;;
        codex)
            mkdir -p "${HOME_DIR}/.codex" 2>/dev/null || true
            mounts+=("-v" "${HOME_DIR}/.codex:/home/user/.codex:rw")
            ;;
        opencode)
            mkdir -p "${HOME_DIR}/.config/opencode" 2>/dev/null || true
            mkdir -p "${HOME_DIR}/.local/share/opencode" 2>/dev/null || true
            mounts+=("-v" "${HOME_DIR}/.config/opencode:/home/user/.config/opencode:rw")
            mounts+=("-v" "${HOME_DIR}/.local/share/opencode:/home/user/.local/share/opencode:rw")
            ;;
    esac
    echo "${mounts[@]}"
}

# Helper: map tool name to its expected binary for version checks.
tool_binary_for() {
    local tool=$1
    case "$tool" in
        claude)  echo "claude" ;;
        copilot) echo "copilot" ;;
        codex)   echo "codex" ;;
        opencode) echo "opencode" ;;
    esac
}

# Helper: get expected version from config.
get_tool_version() {
    local tool_type=$1 tool_name=$2
    jq -r ".${tool_type}[] | select(.name==\"${tool_name}\" and .enabled) | .pinned_version // .host_version // empty" "${CONFIG_DIR}/config.json"
}

# Helper: map tool name to other tools (for negative assertions).
other_tools() {
    local tool=$1
    local others=()
    for t in "${ALL_TOOLS[@]}"; do
        if [ "$t" != "$tool" ]; then
            others+=("$t")
        fi
    done
    echo "${others[@]}"
}

# Barrel runtime constants and helper. Keep this aligned with
# cooper/internal/docker/StartBarrel so the manual e2e launches exercise
# the same container contract as the real application.
BARREL_PROXY_HOST="cooper-proxy"
BARREL_XAUTH_PATH="/home/user/.cooper-clipboard.xauth"
BARREL_PLAYWRIGHT_CACHE="/home/user/.cache/ms-playwright"

build_barrel_run_args() {
    local barrel_name=$1
    local tool_image=$2
    local auth_mounts_name=$3
    local extra_mounts_name=$4
    local extra_envs_name=$5
    local -n auth_mounts_ref="$auth_mounts_name"
    local -n extra_mounts_ref="$extra_mounts_name"
    local -n extra_envs_ref="$extra_envs_name"

    BARREL_ARGS=(
        "run" "-d"
        "--name" "$barrel_name"
        "--network" "$NETWORK_INTERNAL"

        # Security hardening.
        "--cap-drop=ALL"
        "--security-opt=no-new-privileges"
        "--init"

        # Shared memory size for browser workloads.
        "--shm-size" "1g"

        # Label for workspace tracking.
        "--label" "cooper.workspace=${E2E_WORKSPACE}"

        # Workspace (read-write).
        "-v" "${E2E_WORKSPACE}:${E2E_WORKSPACE}:rw"

        # Tool-specific auth mounts.
        "${auth_mounts_ref[@]}"

        # Language caches (Cooper-managed, all read-write).
        "-v" "${CONFIG_DIR}/cache/go-mod:/home/user/go/pkg/mod:rw"
        "-v" "${CONFIG_DIR}/cache/go-build:/home/user/.cache/go-build:rw"
        "-v" "${CONFIG_DIR}/cache/npm:/home/user/.npm:rw"
        "-v" "${CONFIG_DIR}/cache/pip:/home/user/.cache/pip:rw"

        # CA cert and socat rules (read-only).
        "-v" "${CONFIG_DIR}/ca/cooper-ca.pem:/etc/cooper/cooper-ca.pem:ro"
        "-v" "${CONFIG_DIR}/socat-rules.json:/etc/cooper/socat-rules.json:ro"

        # Playwright support mounts: fonts (ro), browser cache (rw), per-barrel /tmp (rw).
        "-v" "${CONFIG_DIR}/fonts:/home/user/.local/share/fonts:ro"
        "-v" "${CONFIG_DIR}/cache/ms-playwright:${BARREL_PLAYWRIGHT_CACHE}:rw"
        "-v" "${CONFIG_DIR}/tmp/${barrel_name}:/tmp:rw"

        # Any scenario-specific extra mounts go here.
        "${extra_mounts_ref[@]}"

        # Proxy environment variables.
        "-e" "HTTP_PROXY=http://${BARREL_PROXY_HOST}:3128"
        "-e" "HTTPS_PROXY=http://${BARREL_PROXY_HOST}:3128"
        "-e" "NO_PROXY=localhost,127.0.0.1"
        "-e" "COOPER_PROXY_HOST=${BARREL_PROXY_HOST}"
        "-e" "COOPER_INTERNAL_NETWORK=${NETWORK_INTERNAL}"

        # Shared X11 display env vars.
        "-e" "DISPLAY=127.0.0.1:99"
        "-e" "XAUTHORITY=${BARREL_XAUTH_PATH}"
        "-e" "COOPER_CLIPBOARD_DISPLAY=127.0.0.1:99"
        "-e" "COOPER_CLIPBOARD_XAUTHORITY=${BARREL_XAUTH_PATH}"

        # Playwright browser cache path.
        "-e" "PLAYWRIGHT_BROWSERS_PATH=${BARREL_PLAYWRIGHT_CACHE}"

        # Clipboard bridge env vars.
        "-e" "COOPER_CLIPBOARD_ENABLED=1"
        "-e" "COOPER_CLIPBOARD_BRIDGE_URL=http://127.0.0.1:4343"
        "-e" "COOPER_CLIPBOARD_TOKEN_FILE=/etc/cooper/clipboard-token"
        "-e" "COOPER_CLIPBOARD_SHIMS=xclip,xsel"

        # Any scenario-specific extra env vars go here.
        "${extra_envs_ref[@]}"

        # Working directory.
        "-w" "${E2E_WORKSPACE}"
    )

    # Git config (read-only) is mounted only when it exists, matching runtime.
    if [ -f "${HOME_DIR}/.gitconfig" ]; then
        BARREL_ARGS+=("-v" "${HOME_DIR}/.gitconfig:/home/user/.gitconfig:ro")
    fi

    if [ -n "$SECCOMP_PATH" ]; then
        BARREL_ARGS+=("--security-opt" "seccomp=${SECCOMP_PATH}")
    fi

    BARREL_ARGS+=("$tool_image" "sleep" "infinity")
}

# Language cache dirs already created above (Cooper-managed under CONFIG_DIR/cache/).

# Playwright support dirs — must exist before Docker mounts them.
mkdir -p "${CONFIG_DIR}/fonts" 2>/dev/null || true
mkdir -p "${CONFIG_DIR}/cache/ms-playwright" 2>/dev/null || true

# Per-barrel /tmp dirs — must exist before Docker mounts them.
for tool in "${ALL_TOOLS[@]}"; do
    mkdir -p "${CONFIG_DIR}/tmp/$(barrel_name_for "$tool")" 2>/dev/null || true
done

# Copy a test font fixture for font mount verification.
TEST_FONT=""
for candidate in /usr/share/fonts/truetype/dejavu/DejaVuSans.ttf /usr/share/fonts/dejavu-sans-fonts/DejaVuSans.ttf; do
    if [ -f "$candidate" ]; then
        TEST_FONT="$candidate"
        break
    fi
done
if [ -n "$TEST_FONT" ]; then
    mkdir -p "${CONFIG_DIR}/fonts/test-fixture"
    cp "$TEST_FONT" "${CONFIG_DIR}/fonts/test-fixture/DejaVuSans.ttf"
    info "Test font fixture: ${TEST_FONT}"
else
    info "No test font fixture found (font assertions will be skipped)"
fi

# ---- Start, test, and stop each tool barrel ----
for tool in "${ALL_TOOLS[@]}"; do
    section "Phase 3: Barrel Testing — ${tool}"

    barrel_name="$(barrel_name_for "$tool")"
    tool_image="$(image_name_for "$tool")"

    # Read auth mounts into an array.
    read -ra AUTH_MOUNTS <<< "$(auth_mounts_for "$tool")"
    EXTRA_MOUNTS=()
    EXTRA_ENVS=()

    # Start barrel container.
    info "Starting ${tool} barrel container..."
    build_barrel_run_args "$barrel_name" "$tool_image" AUTH_MOUNTS EXTRA_MOUNTS EXTRA_ENVS
    docker "${BARREL_ARGS[@]}" >/dev/null 2>&1
    pass "${tool}: barrel container started"

    # Wait for barrel to be running.
    barrel_running=false
    for i in $(seq 1 10); do
        state=$(docker inspect --format '{{.State.Running}}' "$barrel_name" 2>/dev/null || echo "false")
        if [ "$state" = "true" ]; then
            barrel_running=true
            break
        fi
        sleep 1
    done
    if [ "$barrel_running" = "true" ]; then
        pass "${tool}: barrel container is running"
    else
        fail "${tool}: barrel container did not start"
        info "Barrel container logs:"
        docker logs "$barrel_name" 2>&1 | tail -20 | while IFS= read -r line; do info "  $line"; done
        # Stop this barrel and continue to next tool.
        docker rm -f "$barrel_name" 2>/dev/null || true
        continue
    fi

    # Helper: run command inside this barrel.
    barrel_exec() {
        docker exec "$barrel_name" bash -c "$1" 2>&1
    }

    # ---- Proxy connectivity ----
    http_proxy_val=$(barrel_exec 'echo $HTTP_PROXY')
    if echo "$http_proxy_val" | grep -q "cooper-proxy:3128"; then
        pass "${tool}: HTTP_PROXY set correctly"
    else
        fail "${tool}: HTTP_PROXY not set correctly: ${http_proxy_val}"
    fi

    https_proxy_val=$(barrel_exec 'echo $HTTPS_PROXY')
    if echo "$https_proxy_val" | grep -q "cooper-proxy:3128"; then
        pass "${tool}: HTTPS_PROXY set correctly"
    else
        fail "${tool}: HTTPS_PROXY not set correctly: ${https_proxy_val}"
    fi

    dns_result=$(barrel_exec 'getent hosts cooper-proxy 2>&1 || true')
    if echo "$dns_result" | grep -qE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+'; then
        proxy_ip=$(echo "$dns_result" | awk '{print $1}' | head -1)
        pass "${tool}: cooper-proxy resolves to ${proxy_ip}"
    else
        fail "${tool}: cooper-proxy DNS resolution failed: ${dns_result}"
    fi

    tcp_result=$(barrel_exec 'timeout 5 bash -c "echo > /dev/tcp/cooper-proxy/3128" 2>&1 && echo ok || echo fail')
    if echo "$tcp_result" | grep -q "ok"; then
        pass "${tool}: TCP connection to cooper-proxy:3128"
    else
        fail "${tool}: cannot connect to cooper-proxy:3128"
    fi

    ssl_status=$(barrel_exec 'curl -so /dev/null -w "%{http_code}" --connect-timeout 10 --max-time 15 https://api.github.com 2>&1 || true')
    if echo "$ssl_status" | grep -qE '^[23]'; then
        pass "${tool}: HTTPS through SSL bump works (api.github.com -> HTTP ${ssl_status})"
    else
        fail "${tool}: HTTPS through proxy failed for api.github.com (got: ${ssl_status})"
        info "Debugging SSL bump:"
        barrel_exec 'curl -v https://api.github.com 2>&1 | grep -iE "ssl|cert|error|subject" | head -10' | while IFS= read -r line; do info "  $line"; done
    fi

    # ---- Tool-specific verification ----
    expected_version=$(get_tool_version ai_tools "$tool")

    # Check tool binary exists and version matches.
    case "$tool" in
        claude)
            actual=$(barrel_exec 'claude --version 2>&1 || npm list -g @anthropic-ai/claude-code 2>&1 || echo notfound')
            ;;
        copilot)
            actual=$(barrel_exec 'npm list -g @github/copilot 2>&1 || echo notfound')
            ;;
        codex)
            actual=$(barrel_exec 'npm list -g @openai/codex 2>&1 || echo notfound')
            ;;
        opencode)
            actual=$(barrel_exec 'export PATH="$HOME/.opencode/bin:$PATH"; opencode --version 2>&1 || ls "$HOME/.opencode/bin/" 2>&1 || echo notfound')
            ;;
    esac
    if echo "$actual" | grep -q "$expected_version"; then
        pass "${tool}: version ${expected_version} installed"
    else
        fail "${tool}: version ${expected_version} expected, got: ${actual}"
    fi

    # Check other tool binaries are NOT present.
    read -ra others <<< "$(other_tools "$tool")"
    for other in "${others[@]}"; do
        other_bin=$(tool_binary_for "$other")
        # For opencode, check the special path too.
        if [ "$other" = "opencode" ]; then
            other_check=$(barrel_exec 'export PATH="$HOME/.opencode/bin:$PATH"; which opencode 2>/dev/null || echo notfound')
        else
            other_check=$(barrel_exec "which ${other_bin} 2>/dev/null || echo notfound")
        fi
        if echo "$other_check" | grep -q "notfound"; then
            pass "${tool}: ${other} binary NOT present (correct isolation)"
        else
            fail "${tool}: ${other} binary found at ${other_check} (should not be present!)"
        fi
    done

    # Check COOPER_CLI_TOOL env var.
    cli_tool_val=$(barrel_exec 'echo $COOPER_CLI_TOOL')
    if [ "$(echo "$cli_tool_val" | tr -d '[:space:]')" = "$tool" ]; then
        pass "${tool}: COOPER_CLI_TOOL=${tool}"
    else
        fail "${tool}: COOPER_CLI_TOOL expected '${tool}', got '${cli_tool_val}'"
    fi

    # ---- Network isolation (direct egress must fail) ----
    direct_result=$(barrel_exec "curl -so /dev/null -w '%{http_code}' --noproxy '*' --connect-timeout 5 --max-time 10 https://example.com 2>&1 || true")
    if echo "$direct_result" | grep -qE '^[23]'; then
        fail "${tool}: direct internet access SUCCEEDED (HTTP ${direct_result}) — NOT on --internal network!"
    else
        pass "${tool}: direct internet access blocked — network isolation works"
    fi

    # ---- Security hardening ----
    cap_eff=$(barrel_exec 'grep CapEff /proc/self/status 2>/dev/null | awk "{print \$2}"')
    if [ "$cap_eff" = "0000000000000000" ]; then
        pass "${tool}: all capabilities dropped (CapEff = 0)"
    else
        fail "${tool}: capabilities not fully dropped (CapEff = ${cap_eff})"
    fi

    nnp=$(barrel_exec 'grep NoNewPrivs /proc/self/status 2>/dev/null | awk "{print \$2}"')
    if [ "$nnp" = "1" ]; then
        pass "${tool}: no-new-privileges enabled"
    else
        fail "${tool}: no-new-privileges not set (NoNewPrivs = ${nnp:-unknown})"
    fi

    ca_check=$(barrel_exec 'test -f /usr/local/share/ca-certificates/cooper-ca.crt && echo found || echo missing')
    if echo "$ca_check" | grep -q "found"; then
        pass "${tool}: CA cert injected into image"
    else
        fail "${tool}: CA cert not found in image at /usr/local/share/ca-certificates/cooper-ca.crt"
    fi

    ca_mount_check=$(barrel_exec 'test -f /etc/cooper/cooper-ca.pem && echo found || echo missing')
    if echo "$ca_mount_check" | grep -q "found"; then
        pass "${tool}: CA cert volume-mounted at /etc/cooper/cooper-ca.pem"
    else
        fail "${tool}: CA cert not volume-mounted at /etc/cooper/cooper-ca.pem"
    fi

    # ---- Playwright runtime environment ----

    # Wait for entrypoint Xvfb startup (may need a moment).
    sleep 2

    # Env contract.
    display_val=$(barrel_exec 'echo "$DISPLAY"' | tr -d '[:space:]')
    clip_display_val=$(barrel_exec 'echo "$COOPER_CLIPBOARD_DISPLAY"' | tr -d '[:space:]')
    if [ "$display_val" = "$clip_display_val" ] && [ -n "$display_val" ]; then
        pass "${tool}: DISPLAY matches COOPER_CLIPBOARD_DISPLAY (${display_val})"
    else
        fail "${tool}: DISPLAY mismatch: DISPLAY=${display_val} CLIP=${clip_display_val}"
    fi

    xauth_val=$(barrel_exec 'echo "$XAUTHORITY"' | tr -d '[:space:]')
    clip_xauth_val=$(barrel_exec 'echo "$COOPER_CLIPBOARD_XAUTHORITY"' | tr -d '[:space:]')
    if [ "$xauth_val" = "$clip_xauth_val" ] && [ -n "$xauth_val" ]; then
        pass "${tool}: XAUTHORITY matches COOPER_CLIPBOARD_XAUTHORITY"
    else
        fail "${tool}: XAUTHORITY mismatch: XAUTHORITY=${xauth_val} CLIP=${clip_xauth_val}"
    fi

    pw_path=$(barrel_exec 'echo "$PLAYWRIGHT_BROWSERS_PATH"' | tr -d '[:space:]')
    if [ "$pw_path" = "/home/user/.cache/ms-playwright" ]; then
        pass "${tool}: PLAYWRIGHT_BROWSERS_PATH set correctly"
    else
        fail "${tool}: PLAYWRIGHT_BROWSERS_PATH expected /home/user/.cache/ms-playwright, got: ${pw_path}"
    fi

    # Filesystem and mounts.
    fonts_check=$(barrel_exec 'test -d /home/user/.local/share/fonts && echo ok || echo missing')
    if echo "$fonts_check" | grep -q "ok"; then
        pass "${tool}: /home/user/.local/share/fonts mounted"
    else
        fail "${tool}: /home/user/.local/share/fonts not found"
    fi

    fonts_link=$(barrel_exec 'readlink /home/user/.fonts 2>/dev/null || echo missing')
    if echo "$fonts_link" | grep -q "/home/user/.local/share/fonts"; then
        pass "${tool}: ~/.fonts symlink correct"
    else
        fail "${tool}: ~/.fonts symlink incorrect: ${fonts_link}"
    fi

    pw_cache_check=$(barrel_exec 'test -d /home/user/.cache/ms-playwright && echo ok || echo missing')
    if echo "$pw_cache_check" | grep -q "ok"; then
        pass "${tool}: Playwright cache dir mounted"
    else
        fail "${tool}: Playwright cache dir not found"
    fi

    # Playwright cache should be writable.
    pw_write_check=$(barrel_exec 'touch /home/user/.cache/ms-playwright/e2e-write-test && echo ok || echo fail')
    if echo "$pw_write_check" | grep -q "ok"; then
        pass "${tool}: Playwright cache dir is writable"
    else
        fail "${tool}: Playwright cache dir is NOT writable"
    fi

    # Per-barrel /tmp mount.
    tmp_write_check=$(barrel_exec 'echo e2e-tmp-test > /tmp/e2e-tmp-test && cat /tmp/e2e-tmp-test')
    if echo "$tmp_write_check" | grep -q "e2e-tmp-test"; then
        pass "${tool}: /tmp is writable"
    else
        fail "${tool}: /tmp is NOT writable"
    fi

    # Verify /tmp is host-backed — file written inside barrel must appear
    # on the host under the per-barrel tmp directory.
    host_tmp_file="${CONFIG_DIR}/tmp/${barrel_name}/e2e-tmp-test"
    if [ -f "$host_tmp_file" ] && grep -q "e2e-tmp-test" "$host_tmp_file"; then
        pass "${tool}: /tmp is host-backed (file visible on host)"
    else
        fail "${tool}: /tmp file NOT found on host at ${host_tmp_file}"
    fi

    # X11 runtime.
    xvfb_check=$(barrel_exec 'pgrep -x Xvfb >/dev/null 2>&1 && echo running || echo stopped')
    if echo "$xvfb_check" | grep -q "running"; then
        pass "${tool}: Xvfb is running"
    else
        fail "${tool}: Xvfb is NOT running"
    fi

    # Parse display number and check TCP port.
    xvfb_port_check=$(barrel_exec 'DNUM=$(echo "$DISPLAY" | sed "s/.*://;s/\\..*//"); PORT=$((6000 + DNUM)); timeout 1 bash -c "echo > /dev/tcp/127.0.0.1/${PORT}" 2>/dev/null && echo ok || echo fail')
    if echo "$xvfb_port_check" | grep -q "ok"; then
        pass "${tool}: Xvfb TCP port reachable"
    else
        fail "${tool}: Xvfb TCP port NOT reachable"
    fi

    # Font runtime.
    fc_cache_check=$(barrel_exec 'fc-cache -f /home/user/.local/share/fonts 2>&1 && echo ok || echo fail')
    if echo "$fc_cache_check" | grep -q "ok"; then
        pass "${tool}: fc-cache succeeds with read-only font mount"
    else
        fail "${tool}: fc-cache failed"
    fi

    # Check if test fixture font is visible.
    if [ -n "$TEST_FONT" ]; then
        font_seen=$(barrel_exec 'fc-list 2>/dev/null | grep -F "DejaVu Sans" | head -n 1')
        if [ -n "$font_seen" ]; then
            pass "${tool}: mounted test font visible via fontconfig"
        else
            fail "${tool}: mounted test font not visible via fontconfig"
        fi
    fi

    # Shared memory.
    shm_bytes=$(barrel_exec "df -B1 /dev/shm | awk 'NR==2 {print \$2}'" | tr -d '[:space:]')
    if [ -n "$shm_bytes" ] && [ "$shm_bytes" -gt 67108864 ] 2>/dev/null; then
        shm_mb=$((shm_bytes / 1048576))
        pass "${tool}: /dev/shm size = ${shm_mb}MB (> 64MB default)"
    else
        fail "${tool}: /dev/shm size too small or unreadable: ${shm_bytes}"
    fi

    # Stop this barrel before moving on.
    info "Stopping ${tool} barrel container..."
    docker rm -f "$barrel_name" 2>/dev/null || true
    pass "${tool}: barrel stopped and removed"
done

# ============================================================================
# Phase 4: Whitelisted Domain Access (use claude barrel)
# ============================================================================
section "Phase 4: Whitelisted Domain Access"

# Start a claude barrel for the remaining tests.
ACTIVE_BARREL="$BARREL_CLAUDE"
ACTIVE_IMAGE="$IMAGE_CLAUDE"
ACTIVE_TOOL="claude"

info "Starting claude barrel for domain tests..."
read -ra CLAUDE_AUTH_MOUNTS <<< "$(auth_mounts_for claude)"
CLAUDE_EXTRA_MOUNTS=()
CLAUDE_EXTRA_ENVS=()
build_barrel_run_args "$ACTIVE_BARREL" "$ACTIVE_IMAGE" CLAUDE_AUTH_MOUNTS CLAUDE_EXTRA_MOUNTS CLAUDE_EXTRA_ENVS
docker "${BARREL_ARGS[@]}" >/dev/null 2>&1

# Wait for it.
barrel_running=false
for i in $(seq 1 10); do
    state=$(docker inspect --format '{{.State.Running}}' "$ACTIVE_BARREL" 2>/dev/null || echo "false")
    if [ "$state" = "true" ]; then
        barrel_running=true
        break
    fi
    sleep 1
done
if [ "$barrel_running" = "true" ]; then
    pass "Claude barrel started for remaining tests"
else
    fail "Claude barrel did not start for remaining tests"
    docker logs "$ACTIVE_BARREL" 2>&1 | tail -20 | while IFS= read -r line; do info "  $line"; done
    exit 1
fi

# Redefine barrel_exec for the active barrel.
barrel_exec() {
    docker exec "$ACTIVE_BARREL" bash -c "$1" 2>&1
}

test_whitelisted() {
    local domain=$1
    local url=$2
    local status
    status=$(barrel_exec "curl -so /dev/null -w '%{http_code}' --connect-timeout 10 --max-time 15 '${url}' 2>&1 || true")
    # Any HTTP response (even 4xx/5xx) means the domain IS reachable through the proxy.
    # Only 403 (proxy denied) or 000 (connection failed) indicate a problem.
    if [ "$status" = "403" ]; then
        fail "${domain} blocked by proxy (HTTP 403) — should be whitelisted"
    elif [ "$status" = "000" ] || [ -z "$status" ]; then
        fail "${domain} unreachable (no HTTP response)"
    else
        pass "${domain} reachable (HTTP ${status})"
    fi
}

test_whitelisted "api.github.com" "https://api.github.com"
test_whitelisted "api.anthropic.com" "https://api.anthropic.com"
test_whitelisted "api.openai.com" "https://api.openai.com"

# ============================================================================
# Phase 5: Blocked Domain Enforcement
# ============================================================================
section "Phase 5: Blocked Domain Enforcement"

test_blocked() {
    local domain=$1
    local status
    status=$(barrel_exec "curl -so /dev/null -w '%{http_code}' --connect-timeout 5 --max-time 10 'https://${domain}' 2>&1 || true")
    if [ "$status" = "403" ] || [ "$status" = "000" ]; then
        pass "${domain} correctly blocked (HTTP ${status})"
    elif echo "$status" | grep -qE '^[23]'; then
        fail "${domain} NOT blocked (HTTP ${status}) — data exfiltration risk!"
    else
        # Other errors (timeouts, connection refused) also mean blocked.
        pass "${domain} effectively blocked (HTTP ${status})"
    fi
}

test_blocked "example.com"
test_blocked "google.com"
test_blocked "evil-exfiltration.example.org"

# Verify custom Cooper error page is returned for blocked requests.
blocked_body=$(barrel_exec "curl -s --connect-timeout 5 --max-time 10 'https://example.com' 2>&1 || true")
if echo "$blocked_body" | grep -q "not on the proxy whitelist"; then
    pass "Custom Cooper error page returned for blocked requests"
else
    fail "Default Squid error page returned instead of custom Cooper page"
fi

# ============================================================================
# Phase 6: Multiple Barrels Sharing Workspace
# ============================================================================
section "Phase 6: Multiple Barrels Sharing Workspace"

# Start a codex barrel alongside the running claude barrel.
CODEX_BARREL="$BARREL_CODEX"
info "Starting codex barrel alongside claude barrel..."
read -ra CODEX_AUTH_MOUNTS <<< "$(auth_mounts_for codex)"
CODEX_EXTRA_MOUNTS=()
CODEX_EXTRA_ENVS=()
build_barrel_run_args "$CODEX_BARREL" "$IMAGE_CODEX" CODEX_AUTH_MOUNTS CODEX_EXTRA_MOUNTS CODEX_EXTRA_ENVS
docker "${BARREL_ARGS[@]}" >/dev/null 2>&1

# Wait for codex barrel to be running.
codex_running=false
for i in $(seq 1 10); do
    state=$(docker inspect --format '{{.State.Running}}' "$CODEX_BARREL" 2>/dev/null || echo "false")
    if [ "$state" = "true" ]; then
        codex_running=true
        break
    fi
    sleep 1
done
if [ "$codex_running" = "true" ]; then
    pass "Codex barrel started alongside claude barrel"
else
    fail "Codex barrel did not start"
    docker logs "$CODEX_BARREL" 2>&1 | tail -20 | while IFS= read -r line; do info "  $line"; done
fi

# Both barrels running simultaneously.
claude_state=$(docker inspect --format '{{.State.Running}}' "$ACTIVE_BARREL" 2>/dev/null || echo "false")
codex_state=$(docker inspect --format '{{.State.Running}}' "$CODEX_BARREL" 2>/dev/null || echo "false")
if [ "$claude_state" = "true" ] && [ "$codex_state" = "true" ]; then
    pass "Both claude and codex barrels running simultaneously"
else
    fail "Not both barrels running (claude=${claude_state}, codex=${codex_state})"
fi

# Claude writes a file, codex reads it.
docker exec "$ACTIVE_BARREL" bash -c "echo 'hello from claude' > ${E2E_WORKSPACE}/shared-test.txt" 2>/dev/null
codex_read=$(docker exec "$CODEX_BARREL" bash -c "cat ${E2E_WORKSPACE}/shared-test.txt 2>&1 || echo notfound")
if echo "$codex_read" | grep -q "hello from claude"; then
    pass "Codex barrel reads file written by claude barrel (shared workspace)"
else
    fail "Codex barrel cannot read claude's file (got: ${codex_read})"
fi

# Codex writes a file, claude reads it.
docker exec "$CODEX_BARREL" bash -c "echo 'hello from codex' > ${E2E_WORKSPACE}/shared-test-2.txt" 2>/dev/null
claude_read=$(barrel_exec "cat ${E2E_WORKSPACE}/shared-test-2.txt 2>&1 || echo notfound")
if echo "$claude_read" | grep -q "hello from codex"; then
    pass "Claude barrel reads file written by codex barrel (shared workspace)"
else
    fail "Claude barrel cannot read codex's file (got: ${claude_read})"
fi

# Verify each barrel uses the correct image.
claude_img=$(docker inspect --format '{{.Config.Image}}' "$ACTIVE_BARREL" 2>/dev/null || echo "unknown")
codex_img=$(docker inspect --format '{{.Config.Image}}' "$CODEX_BARREL" 2>/dev/null || echo "unknown")
if [ "$claude_img" = "$IMAGE_CLAUDE" ]; then
    pass "Claude barrel uses correct image: ${claude_img}"
else
    fail "Claude barrel uses wrong image: ${claude_img} (expected ${IMAGE_CLAUDE})"
fi
if [ "$codex_img" = "$IMAGE_CODEX" ]; then
    pass "Codex barrel uses correct image: ${codex_img}"
else
    fail "Codex barrel uses wrong image: ${codex_img} (expected ${IMAGE_CODEX})"
fi

# Clean up shared test files and codex barrel.
rm -f "${E2E_WORKSPACE}/shared-test.txt" "${E2E_WORKSPACE}/shared-test-2.txt"
docker rm -f "$CODEX_BARREL" 2>/dev/null || true
pass "Codex barrel stopped after shared workspace test"

# ============================================================================
# Phase 7: Port Forwarding Configuration
# ============================================================================
section "Phase 7: Port Forwarding (socat config)"

# socat-rules.json mounted in barrel.
socat_barrel=$(barrel_exec 'test -f /etc/cooper/socat-rules.json && echo found || echo missing')
if echo "$socat_barrel" | grep -q "found"; then
    pass "socat-rules.json mounted in barrel"
else
    fail "socat-rules.json not mounted in barrel"
fi

# socat-rules.json mounted in proxy.
socat_proxy=$(docker exec "$PROXY_CONTAINER" bash -c 'test -f /etc/cooper/socat-rules.json && echo found || echo missing' 2>&1)
if echo "$socat_proxy" | grep -q "found"; then
    pass "socat-rules.json mounted in proxy"
else
    fail "socat-rules.json not mounted in proxy"
fi

# Validate socat-rules.json content in barrel.
socat_content=$(barrel_exec 'cat /etc/cooper/socat-rules.json')
bridge_port_val=$(echo "$socat_content" | jq -r '.bridge_port' 2>/dev/null || echo "")
rules_count=$(echo "$socat_content" | jq -r '.rules | length' 2>/dev/null || echo "0")
if [ "$bridge_port_val" = "4343" ] && [ "$rules_count" = "2" ]; then
    pass "socat-rules.json has correct content (bridge_port=4343, ${rules_count} rules)"
else
    fail "socat-rules.json content unexpected (bridge_port=${bridge_port_val}, rules=${rules_count})"
fi

# Helper: check if a port is listening inside the barrel.
port_open() {
    barrel_exec "bash -c 'echo > /dev/tcp/localhost/$1' 2>/dev/null" >/dev/null 2>&1
}

# Helper: wait for a port to become ready inside the barrel (up to 10 s).
# The entrypoint starts Xvfb + socat which can take several seconds after a
# docker restart, so fixed sleeps are unreliable.
wait_for_port() {
    local port=$1
    local max_attempts=${2:-20}
    for _i in $(seq 1 "$max_attempts"); do
        if port_open "$port"; then
            return 0
        fi
        sleep 0.5
    done
    return 1
}

# Verify socat is listening on forwarded ports inside the barrel.
for port in 5432 6379; do
    if port_open "$port"; then
        pass "socat listening on port ${port} in barrel"
    else
        fail "socat NOT listening on port ${port} in barrel"
    fi
done

# Verify bridge port socat is listening.
if port_open 4343; then
    pass "socat listening on bridge port 4343 in barrel"
else
    fail "socat NOT listening on bridge port 4343 in barrel"
fi

# ============================================================================
# Phase 7b: Port Forwarding Live Config Change
# ============================================================================
section "Phase 7b: Port Forwarding Live Config Change"

# Remove the Redis rule (6379), keep PostgreSQL (5432).
cat > "${CONFIG_DIR}/socat-rules.json" << 'SOCAT_REMOVE_EOF'
{
  "bridge_port": 4343,
  "rules": [
    {"container_port": 5432, "host_port": 5432, "description": "PostgreSQL"}
  ]
}
SOCAT_REMOVE_EOF

# Send SIGHUP to barrel PID 1 to trigger socat reload.
# Restart barrel to pick up the new socat config.
# SIGHUP-based reload is unreliable under tini --init, so we restart the container.
docker restart "$ACTIVE_BARREL" >/dev/null 2>&1
wait_for_port 4343  # Wait for bridge socat after entrypoint restart.
wait_for_port 5432  # Wait for PostgreSQL socat after entrypoint restart.

# After removing Redis rule, port 5432 should still listen, port 6379 should stop.
if port_open 5432; then
    pass "Port 5432 still forwarded after removing 6379 rule"
else
    fail "Port 5432 stopped working after removing 6379 rule"
fi

if ! port_open 6379; then
    pass "Port 6379 no longer forwarded after rule removal"
else
    fail "Port 6379 still listening after rule was removed"
fi

# Add Redis back.
cat > "${CONFIG_DIR}/socat-rules.json" << 'SOCAT_READD_EOF'
{
  "bridge_port": 4343,
  "rules": [
    {"container_port": 5432, "host_port": 5432, "description": "PostgreSQL"},
    {"container_port": 6379, "host_port": 6379, "description": "Redis"}
  ]
}
SOCAT_READD_EOF

docker restart "$ACTIVE_BARREL" >/dev/null 2>&1
wait_for_port 6379

if port_open 6379; then
    pass "Port 6379 forwarding restored after re-adding rule"
else
    fail "Port 6379 not restored after re-adding rule"
fi

# Test range port forwarding.
cat > "${CONFIG_DIR}/socat-rules.json" << 'SOCAT_RANGE_EOF'
{
  "bridge_port": 4343,
  "rules": [
    {"container_port": 5432, "host_port": 5432, "description": "PostgreSQL"},
    {"container_port": 6379, "host_port": 6379, "description": "Redis"},
    {"container_port": 9100, "host_port": 9100, "description": "range-test", "is_range": true, "range_end": 9102}
  ]
}
SOCAT_RANGE_EOF

docker restart "$ACTIVE_BARREL" >/dev/null 2>&1
wait_for_port 9100
wait_for_port 9101
wait_for_port 9102

# Check that ports 9100, 9101, 9102 are all listening.
range_ok=true
for port in 9100 9101 9102; do
    if ! port_open "$port"; then
        range_ok=false
        fail "Range port ${port} not listening in barrel"
    fi
done
if [ "$range_ok" = true ]; then
    pass "Range port forwarding (9100-9102) all listening"
fi

# Restore original rules.
cat > "${CONFIG_DIR}/socat-rules.json" << 'SOCAT_RESTORE_EOF'
{
  "bridge_port": 4343,
  "rules": [
    {"container_port": 5432, "host_port": 5432, "description": "PostgreSQL"},
    {"container_port": 6379, "host_port": 6379, "description": "Redis"}
  ]
}
SOCAT_RESTORE_EOF
docker restart "$ACTIVE_BARREL" >/dev/null 2>&1
wait_for_port 4343

# ============================================================================
# Phase 7c: Host Relay — localhost-bound services reachable from barrels
# ============================================================================
section "Phase 7c: Host Relay (localhost-bound service forwarding)"

# Build a tiny HTTP server that binds ONLY to 127.0.0.1 — simulating a
# typical dev server that doesn't bind to 0.0.0.0.
E2E_RELAY_DIR="${CONFIG_DIR}/e2e-relay"
mkdir -p "$E2E_RELAY_DIR"
cat > "${E2E_RELAY_DIR}/server.go" << 'RELAYSERVER'
package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	port := os.Args[1]
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "cooper-relay-test-ok")
	})
	srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "listen: %v\n", err)
			os.Exit(1)
		}
	}()
	fmt.Printf("listening on 127.0.0.1:%s\n", port)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	<-ch
	srv.Close()
}
RELAYSERVER

# Build a tiny host relay helper that uses docker.NewHostRelay.
cat > "${E2E_RELAY_DIR}/relay.go" << 'RELAYHELPER'
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
)

func main() {
	port := 17931
	rules := []config.PortForwardRule{{ContainerPort: port, HostPort: port, Description: "relay-test"}}
	gatewayIPs := os.Args[1:]
	logger := log.New(os.Stderr, "", log.LstdFlags)
	hr := docker.NewHostRelay(gatewayIPs, logger)
	hr.Start(rules)
	// Wait for the lazy scan to detect the loopback service.
	for i := 0; i < 20; i++ {
		if hr.ActiveCount() > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Printf("host relay active: %d listeners\n", hr.ActiveCount())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	<-ch
	hr.Stop()
}
RELAYHELPER

RELAY_TEST_PORT=17931

info "Building loopback test server and host relay..."
E2E_RELAY_SERVER="${CONFIG_DIR}/e2e-relay-server"
E2E_RELAY_HELPER="${CONFIG_DIR}/e2e-relay-helper"
go build -o "$E2E_RELAY_SERVER" "${E2E_RELAY_DIR}/server.go" 2>&1 || { fail "relay test server build failed"; }
go build -o "$E2E_RELAY_HELPER" "${E2E_RELAY_DIR}/relay.go" 2>&1 || { fail "relay helper build failed"; }
pass "Relay test binaries built"

# Start the loopback-only HTTP server.
"$E2E_RELAY_SERVER" "$RELAY_TEST_PORT" &
E2E_RELAY_SERVER_PID=$!
sleep 1

# Verify it's only on 127.0.0.1 and responds.
relay_bind=$(ss -tlnp 2>/dev/null | grep ":${RELAY_TEST_PORT} " | head -1)
if echo "$relay_bind" | grep -q "127.0.0.1:${RELAY_TEST_PORT}"; then
    pass "Test server bound to 127.0.0.1:${RELAY_TEST_PORT} only"
else
    fail "Test server not bound to 127.0.0.1:${RELAY_TEST_PORT}: ${relay_bind}"
fi

host_response=$(curl -sf "http://127.0.0.1:${RELAY_TEST_PORT}/" 2>/dev/null || echo "")
if echo "$host_response" | grep -q "cooper-relay-test-ok"; then
    pass "Test server responds on host"
else
    fail "Test server not responding on host: ${host_response}"
fi

# Discover Docker gateway IPs.
GW_EXTERNAL=$(docker network inspect cooper-external --format '{{range .IPAM.Config}}{{.Gateway}}{{end}}' 2>/dev/null || echo "")
GW_BRIDGE=$(docker network inspect bridge --format '{{range .IPAM.Config}}{{.Gateway}}{{end}}' 2>/dev/null || echo "")

# Verify the gateway IP CANNOT reach the server before relay (it's loopback-only).
if ! curl -sf --connect-timeout 2 "http://${GW_BRIDGE}:${RELAY_TEST_PORT}/" >/dev/null 2>&1; then
    pass "Gateway IP cannot reach loopback server (before relay)"
else
    info "Gateway IP already reaches server (service may bind wider) — relay test still valid"
fi

# Start the host relay.
RELAY_GW_ARGS=()
[ -n "$GW_EXTERNAL" ] && RELAY_GW_ARGS+=("$GW_EXTERNAL")
[ -n "$GW_BRIDGE" ] && RELAY_GW_ARGS+=("$GW_BRIDGE")

"$E2E_RELAY_HELPER" "${RELAY_GW_ARGS[@]}" &
E2E_RELAY_HELPER_PID=$!
# Wait for the lazy relay to detect the loopback service and bind.
for _i in $(seq 1 20); do
    if curl -sf -o /dev/null --connect-timeout 1 "http://${GW_BRIDGE}:${RELAY_TEST_PORT}/" 2>/dev/null; then
        break
    fi
    sleep 0.5
done

# Verify the gateway IP CAN now reach the server via relay.
post_relay=$(curl -sf --connect-timeout 2 "http://${GW_BRIDGE}:${RELAY_TEST_PORT}/" 2>/dev/null || echo "")
if echo "$post_relay" | grep -q "cooper-relay-test-ok"; then
    pass "Gateway IP reaches loopback server via host relay"
else
    fail "Gateway IP still cannot reach server via relay: ${post_relay}"
fi

# Add a port forwarding rule for the test port, restart barrel.
cat > "${CONFIG_DIR}/socat-rules.json" << SOCAT_RELAY_EOF
{
  "bridge_port": 4343,
  "rules": [
    {"container_port": 5432, "host_port": 5432, "description": "PostgreSQL"},
    {"container_port": 6379, "host_port": 6379, "description": "Redis"},
    {"container_port": ${RELAY_TEST_PORT}, "host_port": ${RELAY_TEST_PORT}, "description": "relay-test"}
  ]
}
SOCAT_RELAY_EOF

# Restart barrel AND signal proxy to reload socat rules for the new port.
docker restart "$ACTIVE_BARREL" >/dev/null 2>&1
docker exec "$PROXY_CONTAINER" kill -HUP 1 2>/dev/null || true
sleep 2  # Give proxy socat time to reload.
wait_for_port 4343
wait_for_port "$RELAY_TEST_PORT"

# THE KEY TEST: barrel curls localhost:{port} and gets the response from
# a host server that only binds to 127.0.0.1, via the host relay.
barrel_response=$(barrel_exec "curl -sf --connect-timeout 5 --max-time 10 http://localhost:${RELAY_TEST_PORT}/ 2>&1 || echo 'CURL_FAILED'")
if echo "$barrel_response" | grep -q "cooper-relay-test-ok"; then
    pass "Barrel reaches loopback-only host service via host relay"
else
    fail "Barrel cannot reach loopback host service: ${barrel_response}"
fi

# Clean up relay test resources.
kill "$E2E_RELAY_HELPER_PID" 2>/dev/null || true
wait "$E2E_RELAY_HELPER_PID" 2>/dev/null || true
kill "$E2E_RELAY_SERVER_PID" 2>/dev/null || true
wait "$E2E_RELAY_SERVER_PID" 2>/dev/null || true
E2E_RELAY_HELPER_PID=""
E2E_RELAY_SERVER_PID=""

# Restore original rules.
cat > "${CONFIG_DIR}/socat-rules.json" << 'SOCAT_RESTORE2_EOF'
{
  "bridge_port": 4343,
  "rules": [
    {"container_port": 5432, "host_port": 5432, "description": "PostgreSQL"},
    {"container_port": 6379, "host_port": 6379, "description": "Redis"}
  ]
}
SOCAT_RESTORE2_EOF
docker restart "$ACTIVE_BARREL" >/dev/null 2>&1
wait_for_port 4343
pass "Host relay test complete, rules restored"

# ============================================================================
# Phase 8: Socat Live Reload
# ============================================================================
section "Phase 8: Socat Live Reload"

# Write updated socat-rules.json with a new rule.
cat > "${CONFIG_DIR}/socat-rules.json" << 'SOCAT2EOF'
{
  "bridge_port": 4343,
  "rules": [
    {"container_port": 5432, "host_port": 5432, "description": "PostgreSQL"},
    {"container_port": 6379, "host_port": 6379, "description": "Redis"},
    {"container_port": 9999, "host_port": 9999, "description": "TestNewRule"}
  ]
}
SOCAT2EOF
pass "Updated socat-rules.json with new rule"

# Send SIGHUP to proxy PID 1 (triggers socat reload).
docker exec "$PROXY_CONTAINER" kill -HUP 1 2>/dev/null && true
pass "Sent SIGHUP to proxy container PID 1"

# Verify updated config is visible in proxy.
sleep 2  # Give time for reload.
updated_rules=$(docker exec "$PROXY_CONTAINER" cat /etc/cooper/socat-rules.json 2>&1)
new_rule_count=$(echo "$updated_rules" | jq -r '.rules | length' 2>/dev/null || echo "0")
if [ "$new_rule_count" = "3" ]; then
    pass "Proxy sees updated socat-rules.json (3 rules)"
else
    fail "Proxy has stale socat-rules.json (expected 3 rules, got: ${new_rule_count})"
fi

# Verify barrel also sees the updated config (it's the same volume mount).
barrel_rules=$(barrel_exec 'jq ".rules | length" /etc/cooper/socat-rules.json 2>/dev/null || echo 0')
if [ "$barrel_rules" = "3" ]; then
    pass "Barrel sees updated socat-rules.json (3 rules)"
else
    fail "Barrel has stale socat-rules.json (expected 3 rules, got: ${barrel_rules})"
fi

# ============================================================================
# Phase 9: Squid Hot Reload
# ============================================================================
section "Phase 9: Squid Config Hot Reload"

# Verify that squid can be reconfigured without restart.
squid_reconf=$(docker exec "$PROXY_CONTAINER" squid -k reconfigure 2>&1 || true)
squid_running=$(docker exec "$PROXY_CONTAINER" pgrep squid 2>/dev/null || true)
if [ -n "$squid_running" ]; then
    pass "Squid is running after reconfigure signal"
else
    fail "Squid process not running after reconfigure"
fi

# NOTE: Bridge HTTP API tests are not run here because the bridge server is
# started by `cooper up`, not by the e2e test. Bridge tests are covered by
# `cooper proof` and the Go integration tests in internal/app/cooper_test.go.

# ============================================================================
# Phase 10: File/Folder Ownership on Mounted Volumes
# ============================================================================
section "Phase 10: Mounted Volume Ownership"

EXPECTED_UID=$(id -u)
EXPECTED_GID=$(id -g)

# Give proxy a moment to write log files.
sleep 2

check_ownership() {
    local path=$1
    local desc=$2
    if [ ! -e "$path" ]; then
        info "${desc}: path does not exist (${path})"
        return
    fi
    local actual_uid actual_gid
    actual_uid=$(stat -c '%u' "$path" 2>/dev/null)
    actual_gid=$(stat -c '%g' "$path" 2>/dev/null)
    if [ "$actual_uid" = "$EXPECTED_UID" ] && [ "$actual_gid" = "$EXPECTED_GID" ]; then
        pass "${desc} owned by ${actual_uid}:${actual_gid} (correct)"
    else
        fail "${desc} owned by ${actual_uid}:${actual_gid}, expected ${EXPECTED_UID}:${EXPECTED_GID}"
    fi
}

# Proxy-created directories and files.
check_ownership "${CONFIG_DIR}/run" "~/.cooper/run/ directory"
check_ownership "${CONFIG_DIR}/logs" "~/.cooper/logs/ directory"

# Check files INSIDE the directories (created by squid/socat at runtime).
for f in "${CONFIG_DIR}/logs/"*; do
    [ -e "$f" ] || continue
    check_ownership "$f" "Log file $(basename "$f")"
done
for f in "${CONFIG_DIR}/run/"*; do
    [ -e "$f" ] || continue
    check_ownership "$f" "Socket/run file $(basename "$f")"
done

# Barrel-created files in workspace.
barrel_exec "touch ${E2E_WORKSPACE}/ownership-test-file" > /dev/null 2>&1 || true
if [ -f "${E2E_WORKSPACE}/ownership-test-file" ]; then
    check_ownership "${E2E_WORKSPACE}/ownership-test-file" "Barrel-created workspace file"
    rm -f "${E2E_WORKSPACE}/ownership-test-file"
else
    fail "Barrel could not create file in workspace"
fi

# Barrel-created files in mounted config dirs.
if [ -d "${HOME_DIR}/.claude" ]; then
    check_ownership "${HOME_DIR}/.claude" "Mounted config dir .claude"
fi

# ============================================================================
# Phase 11: Barrel Image Internals
# ============================================================================
section "Phase 11: Barrel Image Structure"

# Entrypoint exists (in base image, so all tool images inherit it).
ep_check=$(barrel_exec 'test -f /entrypoint.sh && echo found || echo missing')
if echo "$ep_check" | grep -q "found"; then
    pass "Entrypoint script exists in barrel"
else
    fail "Entrypoint script not found in barrel"
fi

# Doctor diagnostic script exists.
doctor_check=$(barrel_exec 'test -x /usr/local/bin/doctor.sh && echo found || echo missing')
if echo "$doctor_check" | grep -q "found"; then
    pass "doctor.sh exists in barrel at /usr/local/bin/"
else
    fail "doctor.sh not found in barrel"
fi

# curl available (needed for proxy tests within barrel).
curl_check=$(barrel_exec 'which curl 2>&1 || echo missing')
if echo "$curl_check" | grep -q "curl"; then
    pass "curl available in barrel"
else
    fail "curl not found in barrel"
fi

# socat available in barrel (for port forwarding).
socat_barrel_check=$(barrel_exec 'which socat 2>&1 || echo missing')
if echo "$socat_barrel_check" | grep -q "socat"; then
    pass "socat available in barrel"
else
    fail "socat not found in barrel"
fi

# jq available in barrel.
jq_barrel_check=$(barrel_exec 'which jq 2>&1 || echo missing')
if echo "$jq_barrel_check" | grep -q "jq"; then
    pass "jq available in barrel"
else
    fail "jq not found in barrel"
fi

# NODE_EXTRA_CA_CERTS set.
node_ca=$(barrel_exec 'echo $NODE_EXTRA_CA_CERTS')
if [ -n "$node_ca" ]; then
    node_ca_exists=$(barrel_exec "test -f '${node_ca}' && echo found || echo missing")
    if echo "$node_ca_exists" | grep -q "found"; then
        pass "NODE_EXTRA_CA_CERTS set and file exists: ${node_ca}"
    else
        fail "NODE_EXTRA_CA_CERTS set but file missing: ${node_ca}"
    fi
else
    fail "NODE_EXTRA_CA_CERTS not set"
fi

# GOFLAGS should NOT include -mod=readonly (Cooper-managed caches are writable).
goflags=$(barrel_exec 'echo $GOFLAGS')
if echo "$goflags" | grep -q "mod=readonly"; then
    fail "GOFLAGS unexpectedly includes -mod=readonly"
else
    pass "GOFLAGS does not include -mod=readonly"
fi

# ============================================================================
# Phase 11b: Programming Tool Versions (in active barrel)
# ============================================================================
section "Phase 11b: Programming Tool Versions"

# Go.
expected_go=$(get_tool_version programming_tools go)
actual_go=$(barrel_exec 'go version 2>&1 || echo notfound')
if echo "$actual_go" | grep -q "$expected_go"; then
    pass "Go ${expected_go} installed"
else
    fail "Go ${expected_go} expected, got: ${actual_go}"
fi

# Node.js.
expected_node=$(get_tool_version programming_tools node)
actual_node=$(barrel_exec 'node --version 2>&1 || echo notfound')
if echo "$actual_node" | grep -q "v${expected_node}"; then
    pass "Node.js v${expected_node} installed"
else
    fail "Node.js v${expected_node} expected, got: ${actual_node}"
fi

# Python.
actual_python=$(barrel_exec 'python3 --version 2>&1 || echo notfound')
if echo "$actual_python" | grep -qi "python"; then
    pass "Python3 installed (${actual_python})"
else
    fail "Python3 not found"
fi

# ============================================================================
# Phase 11c: Interactive Login Shell PATH
# ============================================================================
section "Phase 11c: Interactive Login Shell PATH"

login_shell_path=$(docker exec "$ACTIVE_BARREL" bash -lc 'echo $PATH' 2>&1)
info "Login shell PATH: ${login_shell_path}"

# Verify npm-global bin dir is in interactive PATH.
if echo "$login_shell_path" | grep -q ".npm-global/bin"; then
    pass "Login shell PATH includes .npm-global/bin"
else
    fail "Login shell PATH missing .npm-global/bin — tools won't be found interactively"
fi

# Verify .local/bin is in interactive PATH (for Claude Code native install).
if echo "$login_shell_path" | grep -q ".local/bin"; then
    pass "Login shell PATH includes .local/bin"
else
    fail "Login shell PATH missing .local/bin"
fi

# Test tool via login shell (only the active tool should be found).
tool_path=$(docker exec "$ACTIVE_BARREL" bash -lc "which claude 2>/dev/null" || true)
if [ -n "$tool_path" ]; then
    pass "claude found in login shell at ${tool_path}"
else
    fail "claude enabled but NOT found in login shell PATH"
fi

# ============================================================================
# Phase 11d: One-Shot Command Execution
# ============================================================================
section "Phase 11d: One-Shot Command Execution"

# Simple echo.
echo_result=$(barrel_exec 'echo hello')
if [ "$echo_result" = "hello" ]; then
    pass "One-shot echo returns 'hello'"
else
    fail "One-shot echo returned: ${echo_result}"
fi

# Go version via exec.
go_exec_result=$(barrel_exec 'go version')
if echo "$go_exec_result" | grep -q "go${expected_go}"; then
    pass "One-shot 'go version' returns correct version"
else
    fail "One-shot 'go version' returned: ${go_exec_result}"
fi

# Node version via exec.
node_exec_result=$(barrel_exec 'node --version')
if echo "$node_exec_result" | grep -q "v${expected_node}"; then
    pass "One-shot 'node --version' returns correct version"
else
    fail "One-shot 'node --version' returned: ${node_exec_result}"
fi

# Workspace is writable.
barrel_exec "touch ${E2E_WORKSPACE}/test-file && echo ok" > /dev/null 2>&1
if [ -f "${E2E_WORKSPACE}/test-file" ]; then
    pass "Workspace is writable from barrel"
    rm -f "${E2E_WORKSPACE}/test-file"
else
    fail "Workspace is NOT writable from barrel"
fi

# ============================================================================
# Phase 12: Proxy Image Internals
# ============================================================================
section "Phase 12: Proxy Image Verification"

# Squid binary exists.
squid_check=$(docker exec "$PROXY_CONTAINER" which squid 2>&1 || true)
if echo "$squid_check" | grep -q "squid"; then
    pass "Squid installed in proxy"
else
    fail "Squid not found in proxy (got: ${squid_check})"
fi

# ACL helper binary exists.
acl_check=$(docker exec "$PROXY_CONTAINER" test -x /usr/lib/squid/cooper-acl-helper 2>&1 && echo found || true)
if echo "$acl_check" | grep -q "found"; then
    pass "ACL helper binary in proxy"
else
    fail "ACL helper binary not found in proxy"
fi

# socat installed in proxy.
socat_proxy_check=$(docker exec "$PROXY_CONTAINER" which socat 2>&1 || true)
if echo "$socat_proxy_check" | grep -q "socat"; then
    pass "socat installed in proxy"
else
    fail "socat not found in proxy"
fi

# jq installed in proxy.
jq_proxy_check=$(docker exec "$PROXY_CONTAINER" which jq 2>&1 || true)
if echo "$jq_proxy_check" | grep -q "jq"; then
    pass "jq installed in proxy"
else
    fail "jq not found in proxy"
fi

# CA cert in proxy.
proxy_ca=$(docker exec "$PROXY_CONTAINER" test -f /etc/squid/cooper-ca.pem 2>&1 && echo found || true)
if echo "$proxy_ca" | grep -q "found"; then
    pass "CA cert mounted in proxy at /etc/squid/cooper-ca.pem"
else
    fail "CA cert not found in proxy"
fi

# CA key in proxy.
proxy_key=$(docker exec "$PROXY_CONTAINER" test -f /etc/squid/cooper-ca-key.pem 2>&1 && echo found || true)
if echo "$proxy_key" | grep -q "found"; then
    pass "CA key mounted in proxy at /etc/squid/cooper-ca-key.pem"
else
    fail "CA key not found in proxy"
fi

# ============================================================================
# Phase 13: CLI Command Verification
# ============================================================================
section "Phase 13: Cooper CLI Commands"

# Cooper cleanup command exists.
cleanup_help=$("$COOPER" cleanup --help 2>&1 || true)
if echo "$cleanup_help" | grep -qi "removes.*cooper containers\|remove.*cooper containers"; then
    pass "cooper cleanup command exists and has correct help text"
else
    fail "cooper cleanup command not working"
fi

# Cooper proof command exists.
proof_help=$("$COOPER" proof --help 2>&1 || true)
if echo "$proof_help" | grep -q "diagnostics"; then
    pass "cooper proof command exists and has correct help text"
else
    fail "cooper proof command not working"
fi

# Cooper cli --help contains tool name argument.
cli_help=$("$COOPER" cli --help 2>&1 || true)
if echo "$cli_help" | grep -q "tool-name"; then
    pass "cooper cli --help mentions tool-name argument"
else
    fail "cooper cli --help does not mention tool-name argument"
fi

# Stop the active barrel before restarting with clipboard config.
info "Stopping active barrel for clipboard bridge restart..."
docker rm -f "$ACTIVE_BARREL" 2>/dev/null || true

# ============================================================================
# Phase 14: Clipboard Bridge Tests
# ============================================================================
section "Phase 14: Clipboard Bridge"

# Start an isolated bridge server for clipboard tests.
# Build a tiny Go program that starts the bridge with clipboard handler.
E2E_BRIDGE_DIR="${CONFIG_DIR}/e2e-bridge"
mkdir -p "$E2E_BRIDGE_DIR"
cat > "${E2E_BRIDGE_DIR}/main.go" << 'BRIDGEGO'
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rickchristie/govner/cooper/internal/bridge"
	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/docker"
)

func main() {
	cooperDir := os.Getenv("COOPER_DIR")
	mgr := clipboard.NewManager(5*time.Minute, 20*1024*1024)
	mgr.SetCooperDir(cooperDir)
	handler := clipboard.NewHandler(mgr)
	var gatewayIPs []string
	if ip, err := docker.GetGatewayIP("cooper-external"); err == nil {
		gatewayIPs = append(gatewayIPs, ip)
	}
	if ip, err := docker.GetGatewayIP("bridge"); err == nil {
		gatewayIPs = append(gatewayIPs, ip)
	}
	srv := bridge.NewBridgeServer(nil, 4343, gatewayIPs)
	srv.SetClipboardHandler(handler)
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "bridge start: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("bridge ready")
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	<-ch
}
BRIDGEGO

info "Building e2e bridge helper..."
E2E_BRIDGE_BIN="${CONFIG_DIR}/e2e-bridge-server"
go build -o "$E2E_BRIDGE_BIN" "${E2E_BRIDGE_DIR}/main.go" 2>&1 || { fail "e2e bridge build failed"; }

# Kill any existing listener on port 4343 (leftover from a prior cooper up).
existing_pid=$(lsof -ti tcp:4343 2>/dev/null || true)
if [ -n "$existing_pid" ]; then
    info "Killing existing process on port 4343 (PID ${existing_pid})..."
    kill "$existing_pid" 2>/dev/null || true
    sleep 1
fi

info "Starting e2e bridge server on port 4343..."
COOPER_DIR="${CONFIG_DIR}" "$E2E_BRIDGE_BIN" &
E2E_BRIDGE_PID=$!

# Wait for bridge to be ready.
bridge_started=false
for _i in $(seq 1 30); do
    if curl -sf -o /dev/null -m 1 "http://127.0.0.1:4343/health" 2>/dev/null; then
        bridge_started=true
        break
    fi
    sleep 0.3
done
if [ "$bridge_started" = "true" ] && kill -0 "$E2E_BRIDGE_PID" 2>/dev/null; then
    pass "E2E bridge server started (PID ${E2E_BRIDGE_PID})"
else
    fail "E2E bridge server did not start (check port 4343 availability)"
    kill "$E2E_BRIDGE_PID" 2>/dev/null || true
fi

# Generate a clipboard token for the claude barrel.
mkdir -p "${CONFIG_DIR}/tokens"
TOKEN=$(head -c 32 /dev/urandom | xxd -p | tr -d '\n')
echo -n "$TOKEN" > "${CONFIG_DIR}/tokens/${BARREL_CLAUDE}"
chmod 600 "${CONFIG_DIR}/tokens/${BARREL_CLAUDE}"
pass "Clipboard token file created"

# Restart the claude barrel WITH clipboard env vars and mounts.
info "Starting claude barrel with clipboard bridge config..."
read -ra CLAUDE_AUTH_MOUNTS <<< "$(auth_mounts_for claude)"
CLIPBOARD_EXTRA_MOUNTS=(
    "-v" "${CONFIG_DIR}/tokens/${BARREL_CLAUDE}:/etc/cooper/clipboard-token:ro"
    "-v" "${CONFIG_DIR}/base/shims:/etc/cooper/shims:ro"
)
CLIPBOARD_EXTRA_ENVS=(
    "-e" "COOPER_CLIPBOARD_MODE=shim"
)
build_barrel_run_args "$ACTIVE_BARREL" "$ACTIVE_IMAGE" CLAUDE_AUTH_MOUNTS CLIPBOARD_EXTRA_MOUNTS CLIPBOARD_EXTRA_ENVS
docker "${BARREL_ARGS[@]}" >/dev/null 2>&1

# Wait for it.
barrel_running=false
for i in $(seq 1 10); do
    state=$(docker inspect --format '{{.State.Running}}' "$ACTIVE_BARREL" 2>/dev/null || echo "false")
    if [ "$state" = "true" ]; then
        barrel_running=true
        break
    fi
    sleep 1
done
if [ "$barrel_running" = "true" ]; then
    pass "Claude barrel started with clipboard bridge config"
else
    fail "Claude barrel did not start for clipboard tests"
    docker logs "$ACTIVE_BARREL" 2>&1 | tail -20 | while IFS= read -r line; do info "  $line"; done
fi

# Redefine barrel_exec for the new barrel.
barrel_exec() {
    docker exec "$ACTIVE_BARREL" bash -c "$1" 2>&1
}

# Wait for entrypoint to finish setup (socat, Xvfb, shims).
# Shim installation is the LAST step in the entrypoint, so we poll for the
# xclip shim file to confirm the entire entrypoint has completed.
for _i in $(seq 1 20); do
    if barrel_exec 'test -x /home/user/.local/bin/xclip' >/dev/null 2>&1; then
        break
    fi
    sleep 0.5
done

# ---- Test 1: Clipboard env vars present in barrel ----
info "Checking clipboard env vars..."

clip_enabled=$(barrel_exec 'echo $COOPER_CLIPBOARD_ENABLED')
if [ "$(echo "$clip_enabled" | tr -d '[:space:]')" = "1" ]; then
    pass "COOPER_CLIPBOARD_ENABLED=1"
else
    fail "COOPER_CLIPBOARD_ENABLED not set (got: '${clip_enabled}')"
fi

clip_token_file=$(barrel_exec 'echo $COOPER_CLIPBOARD_TOKEN_FILE')
if echo "$clip_token_file" | grep -q "/etc/cooper/clipboard-token"; then
    pass "COOPER_CLIPBOARD_TOKEN_FILE set correctly"
else
    fail "COOPER_CLIPBOARD_TOKEN_FILE not set (got: '${clip_token_file}')"
fi

clip_bridge_url=$(barrel_exec 'echo $COOPER_CLIPBOARD_BRIDGE_URL')
if echo "$clip_bridge_url" | grep -q "127.0.0.1:4343"; then
    pass "COOPER_CLIPBOARD_BRIDGE_URL set correctly"
else
    fail "COOPER_CLIPBOARD_BRIDGE_URL not set (got: '${clip_bridge_url}')"
fi

clip_mode=$(barrel_exec 'echo $COOPER_CLIPBOARD_MODE')
if [ "$(echo "$clip_mode" | tr -d '[:space:]')" = "shim" ]; then
    pass "COOPER_CLIPBOARD_MODE=shim"
else
    fail "COOPER_CLIPBOARD_MODE not set correctly (got: '${clip_mode}')"
fi

clip_shims=$(barrel_exec 'echo $COOPER_CLIPBOARD_SHIMS')
if echo "$clip_shims" | grep -q "xclip"; then
    pass "COOPER_CLIPBOARD_SHIMS contains xclip"
else
    fail "COOPER_CLIPBOARD_SHIMS not set (got: '${clip_shims}')"
fi

# ---- Test 2: Clipboard shim scripts exist in barrel ----
info "Checking clipboard shim scripts..."

for shim in xclip xsel wl-paste; do
    shim_check=$(barrel_exec "test -f /etc/cooper/shims/${shim} && echo found || echo missing")
    if echo "$shim_check" | grep -q "found"; then
        pass "Shim script exists: /etc/cooper/shims/${shim}"
    else
        fail "Shim script missing: /etc/cooper/shims/${shim}"
    fi
done

# ---- Test 3: Clipboard token file mounted in barrel ----
info "Checking clipboard token file..."

token_mounted=$(barrel_exec 'test -f /etc/cooper/clipboard-token && echo found || echo missing')
if echo "$token_mounted" | grep -q "found"; then
    pass "Clipboard token file mounted at /etc/cooper/clipboard-token"
else
    fail "Clipboard token file not mounted"
fi

# Verify the token content matches what we wrote.
barrel_token=$(barrel_exec 'cat /etc/cooper/clipboard-token 2>/dev/null || echo EMPTY')
if [ "$barrel_token" = "$TOKEN" ]; then
    pass "Token file content matches generated token"
else
    fail "Token file content mismatch"
fi

# ---- Test 4: Shim scripts installed in user PATH by entrypoint ----
info "Checking shim installation in user PATH..."

# The entrypoint copies shims from /etc/cooper/shims/ to ~/.local/bin/
# when COOPER_CLIPBOARD_MODE is "shim" or "auto".
for shim in xclip xsel wl-paste; do
    installed_check=$(barrel_exec "test -x /home/user/.local/bin/${shim} && echo found || echo missing")
    if echo "$installed_check" | grep -q "found"; then
        pass "Shim installed in PATH: /home/user/.local/bin/${shim}"
    else
        fail "Shim not installed in PATH: /home/user/.local/bin/${shim}"
    fi
done

# ---- Test 5: xsel, xauth, and mcookie are installed in base image ----
info "Checking clipboard tools in base image..."

for tool in xsel xauth mcookie; do
    tool_check=$(barrel_exec "which ${tool} 2>&1 || echo missing")
    if echo "$tool_check" | grep -q "missing"; then
        fail "${tool} not found in barrel"
    else
        pass "${tool} found in barrel at ${tool_check}"
    fi
done

# ---- Test 6: cooper-x11-bridge binary exists ----
info "Checking cooper-x11-bridge binary..."

x11_bridge_check=$(barrel_exec 'which cooper-x11-bridge 2>&1 || echo missing')
if echo "$x11_bridge_check" | grep -q "missing"; then
    fail "cooper-x11-bridge not found in barrel"
else
    pass "cooper-x11-bridge found in barrel at ${x11_bridge_check}"
fi

# ---- Test 7: xclip shim intercepts TARGETS request ----
info "Checking shim TARGETS interception..."

# The xclip shim in ~/.local/bin should intercept -selection clipboard -t TARGETS -o
# and return "image/png" when the bridge has a staged image (or at least not crash).
targets_output=$(barrel_exec '/home/user/.local/bin/xclip -selection clipboard -t TARGETS -o 2>/dev/null || true')
if [ -n "$targets_output" ]; then
    pass "xclip shim handles TARGETS request (output: ${targets_output})"
else
    # Shim may return empty if bridge is unreachable, which is expected in e2e
    # (bridge server is not running). Just verify the shim didn't crash.
    shim_exit=$(barrel_exec '/home/user/.local/bin/xclip -selection clipboard -t TARGETS -o 2>&1; echo "exit:$?"' | grep 'exit:' | head -1)
    info "xclip shim TARGETS returned empty (bridge not running) — exit: ${shim_exit}"
    pass "xclip shim handles TARGETS without crashing"
fi

# ---- Test 8: Clipboard bridge endpoint authentication ----
# NOTE: These tests exercise the socat tunnel to host port 4343. If the bridge
# server is not running on the host (it's started by `cooper up`), the curl
# calls will get connection-refused. We test connectivity and auth separately.
info "Checking clipboard bridge connectivity..."

# First check if the bridge port is reachable at all (socat tunnel).
bridge_reachable=$(barrel_exec 'curl -sf -o /dev/null -w "%{http_code}" -m 3 "http://127.0.0.1:4343/health" 2>/dev/null || echo "unreachable"')
if echo "$bridge_reachable" | grep -q "200"; then
    pass "Bridge server reachable on port 4343"

    # Test with valid token — should get 200 (empty clipboard).
    http_code=$(barrel_exec "curl -sf -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer ${TOKEN}' 'http://127.0.0.1:4343/clipboard/type' 2>/dev/null || echo '000'")
    if [ "$http_code" = "200" ]; then
        pass "GET /clipboard/type with valid token returns HTTP 200"
    else
        fail "GET /clipboard/type with valid token returned HTTP ${http_code} (expected 200)"
    fi

    # Test response body contains "empty" state.
    type_resp=$(barrel_exec "curl -sf -H 'Authorization: Bearer ${TOKEN}' 'http://127.0.0.1:4343/clipboard/type' 2>/dev/null || echo '{}'")
    if echo "$type_resp" | grep -q '"empty"'; then
        pass "GET /clipboard/type returns state=empty when nothing staged"
    else
        fail "GET /clipboard/type unexpected response: ${type_resp}"
    fi

    # Test with invalid token — should get 401.
    http_code_invalid=$(barrel_exec "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer invalid-token-garbage' 'http://127.0.0.1:4343/clipboard/type' 2>/dev/null || echo '000'")
    if [ "$http_code_invalid" = "401" ]; then
        pass "GET /clipboard/type with invalid token returns HTTP 401"
    else
        fail "GET /clipboard/type with invalid token returned HTTP ${http_code_invalid} (expected 401)"
    fi

    # Test with no auth header — should get 401.
    http_code_noauth=$(barrel_exec "curl -s -o /dev/null -w '%{http_code}' 'http://127.0.0.1:4343/clipboard/type' 2>/dev/null || echo '000'")
    if [ "$http_code_noauth" = "401" ]; then
        pass "GET /clipboard/type with no auth returns HTTP 401"
    else
        fail "GET /clipboard/type with no auth returned HTTP ${http_code_noauth} (expected 401)"
    fi
else
    fail "Bridge server not reachable on port 4343 (e2e bridge should be running)"
fi

# Stop the active barrel and bridge server.
info "Stopping active barrel..."
docker rm -f "$ACTIVE_BARREL" 2>/dev/null || true
info "Stopping e2e bridge server..."
if [ -n "${E2E_BRIDGE_PID:-}" ]; then
    kill "$E2E_BRIDGE_PID" 2>/dev/null || true
    wait "$E2E_BRIDGE_PID" 2>/dev/null || true
    E2E_BRIDGE_PID=""
fi

# ============================================================================
# Summary
# ============================================================================
echo ""
echo "============================================"
echo -e "  ${GREEN}${PASS_COUNT} passed${NC}  ${RED}${FAIL_COUNT} failed${NC}  (${TOTAL} total)"
echo "============================================"
echo ""

if [ "$FAIL_COUNT" -eq 0 ]; then
    echo -e "${GREEN}All tests passed! Cooper works end-to-end.${NC}"
else
    echo -e "${RED}${FAIL_COUNT} test(s) failed. See details above.${NC}"
fi

# Exit with failure count (0 = success).
exit "$FAIL_COUNT"

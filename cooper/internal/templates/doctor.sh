#!/bin/bash
# Cooper barrel diagnostic script — run inside a CLI container.
# Tests proxy connectivity, SSL bump, network isolation, and tool installations.
#
# Usage: bash /etc/cooper/test-inside-barrel.sh
#    or: cooper cli -c "bash test-inside-barrel.sh"

set -u

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

PASS=0
FAIL=0
WARN=0

pass() { echo -e "  ${GREEN}PASS${NC}  $1"; PASS=$((PASS + 1)); }
fail() { echo -e "  ${RED}FAIL${NC}  $1"; FAIL=$((FAIL + 1)); }
warn() { echo -e "  ${YELLOW}WARN${NC}  $1"; WARN=$((WARN + 1)); }
info() { echo -e "  ${CYAN}INFO${NC}  $1"; }
section() { echo -e "\n${CYAN}=== $1 ===${NC}"; }

# ============================================================================
section "Environment"
# ============================================================================

info "Hostname: $(hostname)"
info "User: $(whoami) (uid=$(id -u), gid=$(id -g))"
info "Working dir: $(pwd)"

if [ -n "${HTTP_PROXY:-}" ]; then
    pass "HTTP_PROXY set: ${HTTP_PROXY}"
else
    fail "HTTP_PROXY not set — all traffic will bypass proxy"
    info "  Expected: HTTP_PROXY=http://cooper-proxy:3128"
fi

if [ -n "${HTTPS_PROXY:-}" ]; then
    pass "HTTPS_PROXY set: ${HTTPS_PROXY}"
else
    fail "HTTPS_PROXY not set"
fi

if [ -n "${NO_PROXY:-}" ]; then
    info "NO_PROXY: ${NO_PROXY}"
fi

# ============================================================================
section "DNS Resolution"
# ============================================================================

if getent hosts cooper-proxy >/dev/null 2>&1; then
    proxy_ip=$(getent hosts cooper-proxy | awk '{print $1}')
    pass "cooper-proxy resolves to ${proxy_ip}"
else
    fail "Cannot resolve 'cooper-proxy' — Docker DNS not working"
    info "  This container must be on the cooper-internal network."
    info "  Check: docker network inspect cooper-internal"
fi

# ============================================================================
section "Proxy Connectivity"
# ============================================================================

# Test 1: TCP connection to proxy port
if timeout 5 bash -c "echo > /dev/tcp/cooper-proxy/3128" 2>/dev/null; then
    pass "TCP connection to cooper-proxy:3128"
else
    fail "Cannot connect to cooper-proxy:3128"
    info "  Is the proxy container running? Check: docker ps | grep cooper-proxy"
    info "  Is it on the internal network? Check: docker network inspect cooper-internal"
fi

# Test 2: HTTP CONNECT through proxy (non-TLS, just protocol check)
proxy_response=$(curl -s -o /dev/null -w "%{http_code}" \
    -x "${HTTP_PROXY:-http://cooper-proxy:3128}" \
    --connect-timeout 5 \
    http://example.com 2>&1) || true
if [ "$proxy_response" = "403" ] || [ "$proxy_response" = "407" ]; then
    pass "Proxy responds (HTTP ${proxy_response} = correctly blocked non-whitelisted domain)"
elif [ "$proxy_response" = "200" ]; then
    warn "Proxy returned 200 for example.com — this should be blocked!"
elif [ -n "$proxy_response" ]; then
    pass "Proxy responds (HTTP ${proxy_response})"
else
    fail "No response from proxy"
    info "  curl exit: $?"
fi

# ============================================================================
section "SSL Bump / CA Trust"
# ============================================================================

# Check CA cert is installed
if [ -f /usr/local/share/ca-certificates/cooper-ca.crt ]; then
    pass "Cooper CA cert file exists"
else
    fail "Cooper CA cert not found at /usr/local/share/ca-certificates/cooper-ca.crt"
    info "  The CLI Dockerfile should COPY the CA cert and run update-ca-certificates"
fi

# Check NODE_EXTRA_CA_CERTS
if [ -n "${NODE_EXTRA_CA_CERTS:-}" ]; then
    if [ -f "${NODE_EXTRA_CA_CERTS}" ]; then
        pass "NODE_EXTRA_CA_CERTS set and file exists: ${NODE_EXTRA_CA_CERTS}"
    else
        fail "NODE_EXTRA_CA_CERTS set but file missing: ${NODE_EXTRA_CA_CERTS}"
    fi
else
    warn "NODE_EXTRA_CA_CERTS not set — Node.js tools may get cert errors through SSL bump"
fi

# Test actual HTTPS through proxy (this validates the full SSL bump chain)
ssl_test=$(curl -s -o /dev/null -w "%{http_code}" \
    --connect-timeout 10 \
    --max-time 15 \
    https://api.github.com 2>&1) || true
ssl_exit=$?
if [ "$ssl_test" = "200" ] || [ "$ssl_test" = "301" ] || [ "$ssl_test" = "302" ]; then
    pass "HTTPS through SSL bump works (api.github.com → HTTP ${ssl_test})"
elif [ "$ssl_exit" = "60" ] || [ "$ssl_exit" = "77" ]; then
    fail "SSL certificate error (curl exit ${ssl_exit}) — CA not trusted"
    info "  The Cooper CA cert must be in the system CA store."
    info "  Check: ls -la /usr/local/share/ca-certificates/"
    info "  Check: update-ca-certificates was run during image build"
    # Show the actual error
    curl_err=$(curl -v https://api.github.com 2>&1 | grep -i "ssl\|cert\|error" | head -5)
    info "  curl details:"
    echo "$curl_err" | while IFS= read -r line; do info "    $line"; done
elif [ "$ssl_exit" = "56" ] || [ "$ssl_exit" = "35" ]; then
    fail "SSL handshake failed (curl exit ${ssl_exit})"
    info "  Possible causes: Squid SSL bump misconfigured, cert gen failed"
    curl_err=$(curl -v https://api.github.com 2>&1 | grep -i "ssl\|tls\|error" | head -5)
    info "  curl details:"
    echo "$curl_err" | while IFS= read -r line; do info "    $line"; done
else
    fail "HTTPS test failed: HTTP ${ssl_test}, curl exit ${ssl_exit}"
    info "  Full curl output:"
    curl -v https://api.github.com 2>&1 | tail -10 | while IFS= read -r line; do info "    $line"; done
fi

# ============================================================================
section "Whitelisted Domains"
# ============================================================================

test_domain() {
    local domain=$1
    local url=$2
    local result
    result=$(curl -s -o /dev/null -w "%{http_code}" \
        --connect-timeout 10 --max-time 15 \
        "$url" 2>&1) || true
    if [ "$result" = "403" ]; then
        fail "${domain} blocked by proxy (HTTP 403) — should be whitelisted"
    elif [ "$result" = "000" ]; then
        fail "${domain} connection failed (HTTP 000) — proxy or DNS issue"
    elif [ -n "$result" ] && [ "$result" != "000" ]; then
        # Any HTTP response (2xx, 3xx, 4xx, 5xx) proves the domain is reachable
        # through the proxy. A 404 or 421 from the API is fine — it means the
        # request reached the server, which is what we're testing.
        pass "${domain} reachable through proxy (HTTP ${result})"
    else
        fail "${domain} unreachable (no response)"
    fi
}

test_domain "api.github.com" "https://api.github.com"
test_domain "api.anthropic.com" "https://api.anthropic.com"
test_domain "api.openai.com" "https://api.openai.com"

# ============================================================================
section "Blocked Domains (should fail)"
# ============================================================================

test_blocked() {
    local domain=$1
    local result
    result=$(curl -s -o /dev/null -w "%{http_code}" \
        --connect-timeout 5 --max-time 10 \
        "https://${domain}" 2>&1) || true
    if [ "$result" = "403" ] || [ "$result" = "000" ]; then
        pass "${domain} correctly blocked (HTTP ${result})"
    elif echo "$result" | grep -qE "^[23]"; then
        fail "${domain} NOT blocked (HTTP ${result}) — data exfiltration risk!"
    else
        pass "${domain} unreachable (HTTP ${result}) — effectively blocked"
    fi
}

test_blocked "example.com"
test_blocked "google.com"

# ============================================================================
section "Direct Egress (should be impossible)"
# ============================================================================

# Try to reach the internet WITHOUT using the proxy.
# On --internal network, this should fail with "no route to host"
direct_result=$(curl -s -o /dev/null -w "%{http_code}" \
    --noproxy '*' \
    --connect-timeout 5 --max-time 10 \
    https://example.com 2>&1) || true
direct_exit=$?
if [ "$direct_exit" = "7" ] || [ "$direct_exit" = "28" ] || [ "$direct_result" = "000" ]; then
    pass "Direct internet access blocked (curl exit ${direct_exit}) — network isolation works"
elif echo "$direct_result" | grep -qE "^[23]"; then
    fail "Direct internet access SUCCEEDED (HTTP ${direct_result}) — NOT on --internal network!"
    info "  This container can bypass the proxy entirely."
    info "  It must be on the cooper-internal Docker network (--internal flag)."
else
    pass "Direct access failed (HTTP ${direct_result}, exit ${direct_exit}) — likely isolated"
fi

# ============================================================================
section "Port Forwarding (socat)"
# ============================================================================

# Check if socat config exists
if [ -f /etc/cooper/socat-rules.json ]; then
    pass "socat-rules.json mounted"
    rules=$(jq -r '.rules | length' /etc/cooper/socat-rules.json 2>/dev/null || echo "0")
    info "Port forwarding rules: ${rules}"

    # Test each configured port
    if command -v jq &>/dev/null; then
        jq -r '.rules[] | "\(.container_port) \(.description)"' /etc/cooper/socat-rules.json 2>/dev/null | \
        while IFS=' ' read -r port desc; do
            if timeout 2 bash -c "echo > /dev/tcp/localhost/${port}" 2>/dev/null; then
                pass "Port ${port} (${desc}) — connected"
            else
                warn "Port ${port} (${desc}) — not reachable (host service may not be running)"
            fi
        done
    fi
else
    warn "socat-rules.json not mounted at /etc/cooper/socat-rules.json"
fi

# Check bridge port
bridge_port=$(jq -r '.bridge_port // 4343' /etc/cooper/socat-rules.json 2>/dev/null || echo "4343")
if timeout 2 bash -c "echo > /dev/tcp/localhost/${bridge_port}" 2>/dev/null; then
    pass "Bridge port ${bridge_port} reachable"
    # Try the health endpoint
    bridge_health=$(curl -s --connect-timeout 2 "http://localhost:${bridge_port}/health" 2>/dev/null || true)
    if echo "$bridge_health" | grep -q "ok"; then
        pass "Bridge /health returns OK"
    else
        warn "Bridge /health response: ${bridge_health:-empty}"
    fi
else
    warn "Bridge port ${bridge_port} not reachable (cooper up may not be running)"
fi

# ============================================================================
section "Programming Tools"
# ============================================================================

check_tool() {
    local name=$1
    local cmd=$2
    shift 2
    if command -v "$cmd" &>/dev/null; then
        local ver
        ver=$("$cmd" "$@" 2>&1 | head -1)
        pass "${name}: ${ver}"
    else
        warn "${name}: not found (may not be enabled)"
    fi
}

check_tool "Go" go version
check_tool "Node.js" node --version
check_tool "npm" npm --version
check_tool "Python" python3 --version
check_tool "pip" pip3 --version
# ============================================================================
section "AI CLI Tools"
# ============================================================================

# AI tools: check binary exists AND runs successfully.
check_ai_tool() {
    local name=$1
    local cmd=$2
    shift 2
    local bin_path
    bin_path=$(which "$cmd" 2>/dev/null) || true
    if [ -z "$bin_path" ]; then
        warn "${name}: not installed (binary not in PATH)"
        return
    fi
    local ver
    ver=$("$cmd" "$@" 2>&1 | head -1) || true
    if echo "$ver" | grep -qi "not found\|error\|no such"; then
        fail "${name}: binary at ${bin_path} but fails to run: ${ver}"
    else
        pass "${name}: ${ver}"
    fi
}

check_ai_tool "Claude Code" claude --version
check_ai_tool "Copilot CLI" copilot --version
check_ai_tool "Codex CLI" codex --version
check_ai_tool "OpenCode" opencode --version

# ============================================================================
section "Security Settings"
# ============================================================================

# Check capabilities are dropped
if command -v capsh &>/dev/null; then
    caps=$(capsh --print 2>&1 | grep "Current:" | head -1)
    if echo "$caps" | grep -q "="; then
        info "Capabilities: ${caps}"
    fi
elif [ -f /proc/self/status ]; then
    cap_eff=$(grep CapEff /proc/self/status | awk '{print $2}')
    if [ "$cap_eff" = "0000000000000000" ]; then
        pass "All capabilities dropped (CapEff = 0)"
    else
        warn "Capabilities not fully dropped (CapEff = ${cap_eff})"
    fi
fi

# Check no-new-privileges
if [ -f /proc/self/status ]; then
    nnp=$(grep NoNewPrivs /proc/self/status 2>/dev/null | awk '{print $2}')
    if [ "$nnp" = "1" ]; then
        pass "no-new-privileges enabled"
    else
        warn "no-new-privileges not set (NoNewPrivs = ${nnp:-unknown})"
    fi
fi

# Check GOFLAGS
if [ -n "${GOFLAGS:-}" ]; then
    if echo "$GOFLAGS" | grep -q "mod=readonly"; then
        pass "GOFLAGS includes -mod=readonly"
    else
        info "GOFLAGS set but no -mod=readonly: ${GOFLAGS}"
    fi
else
    warn "GOFLAGS not set (Go modules not in readonly mode)"
fi

# ============================================================================
section "Clipboard Bridge"
# ============================================================================

# Check clipboard env vars
if [ "${COOPER_CLIPBOARD_ENABLED:-0}" = "1" ]; then
    pass "COOPER_CLIPBOARD_ENABLED=1"
else
    warn "COOPER_CLIPBOARD_ENABLED not set or disabled"
fi

if [ -n "${COOPER_CLIPBOARD_TOKEN_FILE:-}" ]; then
    if [ -f "${COOPER_CLIPBOARD_TOKEN_FILE}" ]; then
        if [ -r "${COOPER_CLIPBOARD_TOKEN_FILE}" ]; then
            pass "Clipboard token file exists and readable: ${COOPER_CLIPBOARD_TOKEN_FILE}"
        else
            fail "Clipboard token file exists but not readable: ${COOPER_CLIPBOARD_TOKEN_FILE}"
        fi
    else
        warn "Clipboard token file not found: ${COOPER_CLIPBOARD_TOKEN_FILE}"
        info "  Token is written by 'cooper cli' before barrel start."
        info "  If running manually, create a token at ${COOPER_CLIPBOARD_TOKEN_FILE}"
    fi
else
    warn "COOPER_CLIPBOARD_TOKEN_FILE not set"
fi

if [ -n "${COOPER_CLIPBOARD_BRIDGE_URL:-}" ]; then
    pass "COOPER_CLIPBOARD_BRIDGE_URL set: ${COOPER_CLIPBOARD_BRIDGE_URL}"
else
    warn "COOPER_CLIPBOARD_BRIDGE_URL not set"
fi

clip_mode="${COOPER_CLIPBOARD_MODE:-not set}"
info "COOPER_CLIPBOARD_MODE: ${clip_mode}"

# Check clipboard shims
if [ "$clip_mode" = "shim" ] || [ "$clip_mode" = "auto" ]; then
    for shim in xclip xsel wl-paste; do
        if [ -x "/home/user/.local/bin/${shim}" ]; then
            pass "Clipboard shim installed: /home/user/.local/bin/${shim}"
        elif [ -f "/etc/cooper/shims/${shim}" ]; then
            warn "Shim source exists at /etc/cooper/shims/${shim} but not installed in PATH"
            info "  The entrypoint should copy shims to /home/user/.local/bin/"
        else
            warn "Clipboard shim not found: ${shim}"
        fi
    done
fi

# Check clipboard tools
for tool in xsel xauth mcookie; do
    if command -v "$tool" &>/dev/null; then
        pass "${tool} available: $(which "$tool")"
    else
        fail "${tool} not found — needed for clipboard bridge"
        info "  Install with: apt install xsel xauth"
    fi
done

# Check cooper-x11-bridge binary
if command -v cooper-x11-bridge &>/dev/null; then
    pass "cooper-x11-bridge binary available: $(which cooper-x11-bridge)"
else
    warn "cooper-x11-bridge not found — X11 clipboard mode will not work"
fi

# Check Xvfb (needed for x11/auto mode)
if [ "$clip_mode" = "x11" ] || [ "$clip_mode" = "auto" ]; then
    if command -v Xvfb &>/dev/null; then
        pass "Xvfb available: $(which Xvfb)"
    else
        fail "Xvfb not found — required for X11 clipboard mode"
        info "  Install with: apt install xvfb"
    fi

    # Check if Xvfb is running (started by entrypoint for x11/auto mode)
    if pgrep -x Xvfb &>/dev/null; then
        pass "Xvfb process running"
    else
        warn "Xvfb not running — may not have been started by entrypoint"
    fi

    # Check DISPLAY and XAUTHORITY
    if [ -n "${DISPLAY:-}" ]; then
        pass "DISPLAY set: ${DISPLAY}"
    else
        warn "DISPLAY not set — X11 clipboard consumers won't find the display"
    fi

    if [ -n "${XAUTHORITY:-}" ]; then
        if [ -f "${XAUTHORITY}" ]; then
            pass "XAUTHORITY set and file exists: ${XAUTHORITY}"
        else
            warn "XAUTHORITY set but file missing: ${XAUTHORITY}"
        fi
    else
        warn "XAUTHORITY not set"
    fi
fi

# Test bridge clipboard endpoint (if token and bridge URL are available)
if [ -n "${COOPER_CLIPBOARD_TOKEN_FILE:-}" ] && [ -f "${COOPER_CLIPBOARD_TOKEN_FILE}" ] && [ -n "${COOPER_CLIPBOARD_BRIDGE_URL:-}" ]; then
    token=$(cat "${COOPER_CLIPBOARD_TOKEN_FILE}" 2>/dev/null)
    if [ -n "$token" ]; then
        clip_status=$(curl -sf -o /dev/null -w "%{http_code}" \
            -H "Authorization: Bearer ${token}" \
            --connect-timeout 3 --max-time 5 \
            "${COOPER_CLIPBOARD_BRIDGE_URL}/clipboard/type" 2>/dev/null || echo "000")
        if [ "$clip_status" = "200" ]; then
            pass "Clipboard bridge endpoint reachable (HTTP 200)"
        elif [ "$clip_status" = "000" ]; then
            warn "Clipboard bridge endpoint not reachable (cooper up may not be running)"
        else
            warn "Clipboard bridge returned HTTP ${clip_status}"
        fi
    fi
fi

# ============================================================================
section "Playwright Runtime"
# ============================================================================

# Font utilities (needed for Playwright browser rendering)
if command -v fc-cache &>/dev/null; then
    pass "fc-cache available: $(which fc-cache)"
else
    warn "fc-cache not found — Playwright browsers may have font issues"
fi

if command -v fc-list &>/dev/null; then
    pass "fc-list available: $(which fc-list)"
else
    warn "fc-list not found — Playwright browsers may have font issues"
fi

# Playwright browser path
if [ -n "${PLAYWRIGHT_BROWSERS_PATH:-}" ]; then
    pass "PLAYWRIGHT_BROWSERS_PATH set: ${PLAYWRIGHT_BROWSERS_PATH}"
else
    warn "PLAYWRIGHT_BROWSERS_PATH not set"
fi

# Font directories
if [ -d /home/user/.local/share/fonts ]; then
    pass "/home/user/.local/share/fonts directory exists"
else
    warn "/home/user/.local/share/fonts directory does not exist"
fi

if [ -L /home/user/.fonts ]; then
    link_target=$(readlink /home/user/.fonts)
    if [ "$link_target" = "/home/user/.local/share/fonts" ]; then
        pass "/home/user/.fonts is a symlink to /home/user/.local/share/fonts"
    else
        warn "/home/user/.fonts is a symlink but points to ${link_target} (expected /home/user/.local/share/fonts)"
    fi
else
    warn "/home/user/.fonts is not a symlink to /home/user/.local/share/fonts"
fi

# X11 / display environment for Playwright
if [ -n "${DISPLAY:-}" ]; then
    pass "DISPLAY set: ${DISPLAY}"
else
    warn "DISPLAY not set — Playwright browsers need a display"
fi

if [ -n "${XAUTHORITY:-}" ]; then
    if [ -f "${XAUTHORITY}" ]; then
        pass "XAUTHORITY set and file exists: ${XAUTHORITY}"
    else
        warn "XAUTHORITY set but file missing: ${XAUTHORITY}"
    fi
else
    warn "XAUTHORITY not set"
fi

if [ -n "${COOPER_CLIPBOARD_DISPLAY:-}" ]; then
    pass "COOPER_CLIPBOARD_DISPLAY set: ${COOPER_CLIPBOARD_DISPLAY}"
else
    warn "COOPER_CLIPBOARD_DISPLAY not set"
fi

if [ -n "${COOPER_CLIPBOARD_XAUTHORITY:-}" ]; then
    if [ -f "${COOPER_CLIPBOARD_XAUTHORITY}" ]; then
        pass "COOPER_CLIPBOARD_XAUTHORITY set and file exists: ${COOPER_CLIPBOARD_XAUTHORITY}"
    else
        warn "COOPER_CLIPBOARD_XAUTHORITY set but file missing: ${COOPER_CLIPBOARD_XAUTHORITY}"
    fi
else
    warn "COOPER_CLIPBOARD_XAUTHORITY not set"
fi

# Check DISPLAY/XAUTHORITY consistency with clipboard equivalents
if [ -n "${DISPLAY:-}" ] && [ -n "${COOPER_CLIPBOARD_DISPLAY:-}" ]; then
    if [ "${DISPLAY}" = "${COOPER_CLIPBOARD_DISPLAY}" ]; then
        pass "DISPLAY matches COOPER_CLIPBOARD_DISPLAY (${DISPLAY})"
    else
        warn "DISPLAY (${DISPLAY}) does not match COOPER_CLIPBOARD_DISPLAY (${COOPER_CLIPBOARD_DISPLAY})"
    fi
fi

if [ -n "${XAUTHORITY:-}" ] && [ -n "${COOPER_CLIPBOARD_XAUTHORITY:-}" ]; then
    if [ "${XAUTHORITY}" = "${COOPER_CLIPBOARD_XAUTHORITY}" ]; then
        pass "XAUTHORITY matches COOPER_CLIPBOARD_XAUTHORITY (${XAUTHORITY})"
    else
        warn "XAUTHORITY (${XAUTHORITY}) does not match COOPER_CLIPBOARD_XAUTHORITY (${COOPER_CLIPBOARD_XAUTHORITY})"
    fi
fi

# Browser cache directory
if [ -n "${PLAYWRIGHT_BROWSERS_PATH:-}" ]; then
    if [ -d "${PLAYWRIGHT_BROWSERS_PATH}" ]; then
        pass "Browser cache directory exists: ${PLAYWRIGHT_BROWSERS_PATH}"
    else
        warn "Browser cache directory does not exist: ${PLAYWRIGHT_BROWSERS_PATH}"
        info "  Browsers have not been downloaded yet — this is OK until playwright install is run"
    fi
fi

# Font cache rebuild test
if command -v fc-cache &>/dev/null && [ -d /home/user/.local/share/fonts ]; then
    if fc-cache -f /home/user/.local/share/fonts 2>/dev/null; then
        pass "fc-cache -f /home/user/.local/share/fonts succeeds"
    else
        warn "fc-cache -f /home/user/.local/share/fonts failed (font dir may be read-only)"
    fi
fi

# Shared memory for Chromium
if df /dev/shm &>/dev/null 2>&1; then
    shm_size=$(df -B1 /dev/shm | awk 'NR==2 {print $2}')
    shm_human=$(df -h /dev/shm | awk 'NR==2 {print $2}')
    info "/dev/shm size: ${shm_human} (${shm_size} bytes)"
else
    warn "/dev/shm not available — Chromium needs shared memory"
fi

# ============================================================================
section "File Permissions"
# ============================================================================

for dir in ~/.claude ~/.copilot ~/.codex ~/.config/opencode; do
    if [ -d "$dir" ]; then
        if [ -w "$dir" ]; then
            pass "${dir} writable"
        else
            fail "${dir} exists but not writable"
        fi
    else
        info "${dir} not mounted"
    fi
done

# Check workspace is writable
if [ -w "$(pwd)" ]; then
    pass "Workspace $(pwd) writable"
else
    fail "Workspace $(pwd) not writable"
fi

# ============================================================================
section "Summary"
# ============================================================================

echo ""
echo -e "  ${GREEN}${PASS} passed${NC}  ${RED}${FAIL} failed${NC}  ${YELLOW}${WARN} warnings${NC}"
echo ""

if [ "$FAIL" -gt 0 ]; then
    echo -e "${RED}Some checks failed. See details above for troubleshooting.${NC}"
    exit 1
else
    echo -e "${GREEN}All critical checks passed.${NC}"
    exit 0
fi

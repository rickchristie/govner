#!/bin/bash
# ============================================================
# NOTE: Run this INSIDE the CLI container, not on host!
# Usage: <sandb>/shell.sh  then  ./<sandb>/doctor.sh
# ============================================================

set -e

# Determine sandbox folder name from script location
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SANDB_NAME="$(basename "${SCRIPT_DIR}")"

echo "=== AI Sandbox Doctor ==="

# Verify we're inside container
if [ ! -f /.dockerenv ]; then
    echo "ERROR: This script must run inside the CLI container!"
    echo "   Run ${SANDB_NAME}/shell.sh first, then ./${SANDB_NAME}/doctor.sh"
    exit 1
fi

# Check Squid proxy connectivity
echo ""
echo "--- Squid Proxy ---"
nc -z -w 3 host.docker.internal 3128 && echo "OK: Squid proxy reachable" || echo "FAIL: Squid proxy NOT reachable (run: ${SANDB_NAME}/proxy/start.sh)"

# Test whitelisted domains (should succeed)
echo ""
echo "--- Whitelisted Domains ---"
curl -so /dev/null -w "%{http_code}" -x http://host.docker.internal:3128 https://api.anthropic.com 2>/dev/null | grep -qE "^[2-4][0-9][0-9]$" && echo "OK: Anthropic API" || echo "FAIL: Anthropic API"
curl -so /dev/null -w "%{http_code}" -x http://host.docker.internal:3128 https://api.github.com 2>/dev/null | grep -qE "^[2][0-9][0-9]$" && echo "OK: GitHub API" || echo "FAIL: GitHub API"
curl -so /dev/null -w "%{http_code}" -x http://host.docker.internal:3128 https://registry.npmjs.org 2>/dev/null | grep -qE "^[2][0-9][0-9]$" && echo "OK: NPM registry" || echo "FAIL: NPM registry"

# Test blocked domains
echo ""
echo "--- Blocked Domains (expect failures) ---"
curl -s -x http://host.docker.internal:3128 https://example.com 2>/dev/null | grep -qi "example" && echo "LEAK: example.com accessible!" || echo "OK: example.com blocked"
curl -s -x http://host.docker.internal:3128 https://google.com 2>/dev/null | grep -qi "google" && echo "LEAK: google.com accessible!" || echo "OK: google.com blocked"

# Test direct internet access (should fail - no route)
echo ""
echo "--- Direct Internet Access (expect failure) ---"
curl -so /dev/null --connect-timeout 3 https://example.com 2>/dev/null && echo "LEAK: Direct internet access!" || echo "OK: No direct internet route"

# ============================================================================
# OPTIONAL: Test socat port forwarding
# ============================================================================
# Uncomment if you have configured socat port forwarding in entrypoint.sh
#
# echo ""
# echo "--- Local Port Forwarding (socat) ---"
# nc -z localhost 5432 && echo "OK: Port 5432 (postgres)" || echo "FAIL: Port 5432 (postgres)"
# nc -z localhost 6379 && echo "OK: Port 6379 (redis)" || echo "FAIL: Port 6379 (redis)"
# ============================================================================

# Test VS Code Integration
echo ""
echo "--- VS Code Integration ---"
if [ -n "${CLAUDE_CODE_SSE_PORT}" ]; then
    echo "OK: CLAUDE_CODE_SSE_PORT=${CLAUDE_CODE_SSE_PORT}"
    echo "NOTE: IDE integration in container may be limited (VS Code binds to 127.0.0.1 only)"
else
    echo "INFO: CLAUDE_CODE_SSE_PORT not set (run shell.sh from VS Code terminal)"
fi
if [ "${TERM_PROGRAM}" = "vscode" ]; then
    echo "OK: TERM_PROGRAM=vscode"
else
    echo "INFO: TERM_PROGRAM not set to vscode"
fi
if [ "${ENABLE_IDE_INTEGRATION}" = "true" ]; then
    echo "OK: ENABLE_IDE_INTEGRATION=true"
fi

# Test Go installation
echo ""
echo "--- Go Install ---"
if command -v go &>/dev/null; then
    GO_VERSION=$(go version | grep -oP 'go\K[0-9]+\.[0-9]+(\.[0-9]+)?')
    echo "OK: Go ${GO_VERSION} installed"
else
    echo "FAIL: Go not found"
fi

# Test Claude Code installation
echo ""
echo "--- Claude Code Install ---"
if command -v claude &>/dev/null; then
    CLAUDE_PATH=$(which claude)
    if [[ "${CLAUDE_PATH}" == /home/user/.local/bin/* ]]; then
        echo "OK: Claude Code installed natively in ~/.local/bin (auto-updates enabled)"
    elif [[ "${CLAUDE_PATH}" == /home/user/.npm-global/* ]]; then
        echo "INFO: Claude Code installed via npm at ${CLAUDE_PATH}"
    else
        echo "INFO: Claude Code at ${CLAUDE_PATH}"
    fi
else
    echo "FAIL: Claude Code not found"
fi

# Test Claude Code auth
echo ""
echo "--- Claude Code Auth ---"
if [ -f /home/user/.claude.json ]; then
    if grep -q "oauthAccount" /home/user/.claude.json 2>/dev/null; then
        echo "OK: Claude Code auth (~/.claude.json)"
    else
        echo "FAIL: Claude Code auth missing oauthAccount"
    fi
else
    echo "FAIL: ~/.claude.json not found"
fi
if [ -f /home/user/.claude/.credentials.json ]; then
    if [ -r /home/user/.claude/.credentials.json ]; then
        echo "OK: Credentials file readable"
    else
        echo "FAIL: Credentials file not readable (permission issue)"
    fi
fi
# Check directory ownership
if [ "$(stat -c '%U' /home/user/.claude 2>/dev/null)" = "user" ]; then
    echo "OK: ~/.claude owned by user"
else
    echo "FAIL: ~/.claude not owned by user (permission issue)"
fi

# Test GitHub Copilot CLI auth
echo ""
echo "--- GitHub Copilot CLI Auth ---"
if [ -d /home/user/.copilot ]; then
    echo "OK: ~/.copilot directory mounted"
else
    echo "FAIL: ~/.copilot not mounted"
fi
if [ -n "${GH_TOKEN}" ]; then
    echo "OK: GH_TOKEN is set (PAT auth configured)"
elif [ -n "${GITHUB_TOKEN}" ]; then
    echo "OK: GITHUB_TOKEN is set (PAT auth configured)"
elif [ -f /home/user/.copilot/.gh_token ]; then
    echo "OK: ~/.copilot/.gh_token found (PAT auth configured)"
else
    echo "INFO: Copilot PAT not configured. See shell.sh welcome message for setup."
fi

echo ""
echo "=== AI Sandbox Doctor Complete ==="

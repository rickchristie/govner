#!/bin/bash
# Open interactive shell in AI CLI container
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
WORKSPACE_NAME="$(basename "${WORKSPACE_DIR}")"
SANDB_NAME="$(basename "${SCRIPT_DIR}")"
CONTAINER_NAME="ai-cli-${WORKSPACE_NAME}"

# Parse arguments
COMMAND=""
if [ "$1" = "-c" ] && [ -n "$2" ]; then
    COMMAND="$2"
fi

# Check if container is running
if ! docker ps --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "CLI container not running. Starting..."
    "${SCRIPT_DIR}/start-cli.sh"
fi

# Build environment variable arguments
ENV_ARGS=""

# Pass TERM for proper color support
ENV_ARGS="${ENV_ARGS} -e TERM=${TERM:-xterm-256color}"

# VS Code integration
if [ -n "${TERM_PROGRAM}" ]; then
    ENV_ARGS="${ENV_ARGS} -e TERM_PROGRAM=${TERM_PROGRAM}"
fi
if [ -n "${TERM_PROGRAM_VERSION}" ]; then
    ENV_ARGS="${ENV_ARGS} -e TERM_PROGRAM_VERSION=${TERM_PROGRAM_VERSION}"
fi

# If running from VS Code with Claude Code extension
if [ "${TERM_PROGRAM}" = "vscode" ] && [ -n "${CLAUDE_CODE_SSE_PORT}" ]; then
    ENV_ARGS="${ENV_ARGS} -e CLAUDECODE=1"
    ENV_ARGS="${ENV_ARGS} -e CLAUDE_CODE_ENTRYPOINT=cli"
    ENV_ARGS="${ENV_ARGS} -e ENABLE_IDE_INTEGRATION=true"
else
    # Fall back to inherited values if available
    [ -n "${CLAUDECODE}" ] && ENV_ARGS="${ENV_ARGS} -e CLAUDECODE=${CLAUDECODE}"
    [ -n "${CLAUDE_CODE_ENTRYPOINT}" ] && ENV_ARGS="${ENV_ARGS} -e CLAUDE_CODE_ENTRYPOINT=${CLAUDE_CODE_ENTRYPOINT}"
    [ -n "${ENABLE_IDE_INTEGRATION}" ] && ENV_ARGS="${ENV_ARGS} -e ENABLE_IDE_INTEGRATION=${ENABLE_IDE_INTEGRATION}"
fi

# GitHub Copilot CLI authentication (PAT-based)
COPILOT_TOKEN_FILE="${HOME}/.copilot/.gh_token"
if [ -n "${GH_TOKEN}" ]; then
    ENV_ARGS="${ENV_ARGS} -e GH_TOKEN=${GH_TOKEN}"
elif [ -n "${GITHUB_TOKEN}" ]; then
    ENV_ARGS="${ENV_ARGS} -e GITHUB_TOKEN=${GITHUB_TOKEN}"
elif [ -f "${COPILOT_TOKEN_FILE}" ]; then
    GH_TOKEN_VALUE=$(cat "${COPILOT_TOKEN_FILE}" | tr -d '[:space:]')
    if [ -n "${GH_TOKEN_VALUE}" ]; then
        ENV_ARGS="${ENV_ARGS} -e GH_TOKEN=${GH_TOKEN_VALUE}"
    fi
fi

# VS Code git integration
if [ -n "${VSCODE_GIT_IPC_HANDLE}" ]; then
    ENV_ARGS="${ENV_ARGS} -e VSCODE_GIT_IPC_HANDLE=${VSCODE_GIT_IPC_HANDLE}"
fi

# Pass Claude Code SSE port
if [ -n "${CLAUDE_CODE_SSE_PORT}" ]; then
    ENV_ARGS="${ENV_ARGS} -e CLAUDE_CODE_SSE_PORT=${CLAUDE_CODE_SSE_PORT}"
fi

# Check if Copilot token is configured
COPILOT_CONFIGURED=false
if [ -n "${GH_TOKEN}" ] || [ -n "${GITHUB_TOKEN}" ] || [ -f "${COPILOT_TOKEN_FILE}" ]; then
    COPILOT_CONFIGURED=true
fi

if [ -n "${COMMAND}" ]; then
    # Run command and exit
    docker exec -it ${ENV_ARGS} "${CONTAINER_NAME}" bash -c "${COMMAND}"
else
    # Interactive shell - show usage info
    echo ""
    echo "=== AI Sandbox Shell ==="
    echo ""
    echo "Auto-approve aliases are configured:"
    echo "  claude   -> claude --dangerously-skip-permissions"
    echo "  copilot  -> copilot --allow-all-tools"
    echo ""
    echo "Usage:"
    echo "  claude                        # Interactive mode"
    echo "  claude \"your prompt\"          # One-shot mode"
    echo "  copilot -p \"your prompt\"      # Copilot CLI"
    echo ""

    if [ "${COPILOT_CONFIGURED}" = "false" ]; then
        echo "+-------------------------------------------------------------+"
        echo "| COPILOT SETUP (one-time)                                    |"
        echo "+-------------------------------------------------------------+"
        echo "| 1. Create a fine-grained PAT with only 'Copilot Requests':  |"
        echo "|    https://github.com/settings/personal-access-tokens/new   |"
        echo "|                                                             |"
        echo "| 2. Save the token (run on host):                            |"
        echo "|    echo 'YOUR_TOKEN' > ~/.copilot/.gh_token                 |"
        echo "|    chmod 600 ~/.copilot/.gh_token                           |"
        echo "|                                                             |"
        echo "| 3. Restart shell: exit, then run ${SANDB_NAME}/shell.sh again      |"
        echo "+-------------------------------------------------------------+"
        echo ""
    fi

    echo "========================"
    echo ""
    docker exec -it ${ENV_ARGS} "${CONTAINER_NAME}" bash || true
fi

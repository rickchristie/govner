#!/bin/bash
# Install/Update AI Sandbox configuration
# - Updates Go version in Dockerfile to match host
# - Creates or updates .vscode/tasks.json with AI sandbox tasks
#
# This script is idempotent - safe to run multiple times.
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
SANDB_NAME="$(basename "${SCRIPT_DIR}")"
VSCODE_DIR="${WORKSPACE_DIR}/.vscode"
TASKS_FILE="${VSCODE_DIR}/tasks.json"
DOCKERFILE="${SCRIPT_DIR}/cli/Dockerfile"

echo "=== AI Sandbox Install ==="
echo ""

# ============================================================================
# Step 1: Update Go version in Dockerfile
# ============================================================================
echo "--- Go Version ---"

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "WARNING: Go not found on host. Skipping Go version update."
    echo "         Install Go and re-run this script to sync versions."
else
    # Get host Go version (e.g., "1.24.10")
    HOST_GO_VERSION=$(go version | grep -oP 'go\K[0-9]+\.[0-9]+(\.[0-9]+)?')

    if [ -z "${HOST_GO_VERSION}" ]; then
        echo "WARNING: Could not parse Go version. Skipping update."
    else
        # Get current Dockerfile Go version
        CURRENT_GO_VERSION=$(grep -oP 'FROM golang:\K[0-9]+\.[0-9]+(\.[0-9]+)?' "${DOCKERFILE}" | head -1)

        if [ "${HOST_GO_VERSION}" = "${CURRENT_GO_VERSION}" ]; then
            echo "OK: Go version already matches (${HOST_GO_VERSION})"
        else
            echo "Updating: Go ${CURRENT_GO_VERSION:-unknown} -> ${HOST_GO_VERSION}"

            # Update the golang base image version
            sed -i "s|FROM golang:[0-9.]*-bookworm|FROM golang:${HOST_GO_VERSION}-bookworm|g" "${DOCKERFILE}"

            echo "OK: Dockerfile updated to Go ${HOST_GO_VERSION}"
            echo ""
            echo "NOTE: Run '${SANDB_NAME}/cli/build.sh' to rebuild with new Go version"
        fi
    fi
fi

# ============================================================================
# Step 2: Install VS Code tasks
# ============================================================================
echo ""
echo "--- VS Code Tasks ---"

# AI Sandbox tasks to add (uses SANDB_NAME for folder-agnostic paths)
AI_TASKS="[
    {
        \"label\": \"AI: Build Proxy\",
        \"type\": \"shell\",
        \"command\": \"\${workspaceFolder}/${SANDB_NAME}/proxy/build.sh\",
        \"options\": { \"cwd\": \"\${workspaceFolder}\" },
        \"presentation\": {
            \"clear\": false,
            \"echo\": true,
            \"focus\": false,
            \"panel\": \"dedicated\",
            \"reveal\": \"always\",
            \"showReuseMessage\": false
        },
        \"problemMatcher\": [],
        \"isBackground\": false
    },
    {
        \"label\": \"AI: Build CLI\",
        \"type\": \"shell\",
        \"command\": \"\${workspaceFolder}/${SANDB_NAME}/cli/build.sh\",
        \"options\": { \"cwd\": \"\${workspaceFolder}\" },
        \"presentation\": {
            \"clear\": false,
            \"echo\": true,
            \"focus\": false,
            \"panel\": \"dedicated\",
            \"reveal\": \"always\",
            \"showReuseMessage\": false
        },
        \"problemMatcher\": [],
        \"isBackground\": false
    },
    {
        \"label\": \"AI: Start Proxy\",
        \"type\": \"shell\",
        \"command\": \"\${workspaceFolder}/${SANDB_NAME}/proxy/start.sh\",
        \"options\": { \"cwd\": \"\${workspaceFolder}\" },
        \"presentation\": {
            \"clear\": false,
            \"close\": true,
            \"echo\": true,
            \"focus\": false,
            \"panel\": \"dedicated\",
            \"reveal\": \"silent\",
            \"showReuseMessage\": false
        },
        \"problemMatcher\": [],
        \"isBackground\": false
    },
    {
        \"label\": \"AI: Stop Proxy\",
        \"type\": \"shell\",
        \"command\": \"\${workspaceFolder}/${SANDB_NAME}/proxy/stop.sh\",
        \"options\": { \"cwd\": \"\${workspaceFolder}\" },
        \"presentation\": {
            \"clear\": false,
            \"echo\": true,
            \"focus\": false,
            \"panel\": \"dedicated\",
            \"reveal\": \"always\",
            \"showReuseMessage\": false
        },
        \"problemMatcher\": [],
        \"isBackground\": false
    },
    {
        \"label\": \"AI: Stop CLI\",
        \"type\": \"shell\",
        \"command\": \"\${workspaceFolder}/${SANDB_NAME}/stop-cli.sh\",
        \"options\": { \"cwd\": \"\${workspaceFolder}\" },
        \"presentation\": {
            \"clear\": false,
            \"echo\": true,
            \"focus\": false,
            \"panel\": \"dedicated\",
            \"reveal\": \"always\",
            \"showReuseMessage\": false
        },
        \"problemMatcher\": [],
        \"isBackground\": false
    },
    {
        \"label\": \"AI: Shell\",
        \"type\": \"shell\",
        \"command\": \"\${workspaceFolder}/${SANDB_NAME}/shell.sh\",
        \"options\": { \"cwd\": \"\${workspaceFolder}\" },
        \"presentation\": {
            \"clear\": true,
            \"echo\": true,
            \"focus\": true,
            \"panel\": \"new\",
            \"close\": true,
            \"reveal\": \"always\",
            \"showReuseMessage\": false
        },
        \"runOptions\": {
            \"instanceLimit\": 99,
            \"reevaluateOnRerun\": true
        },
        \"problemMatcher\": [],
        \"isBackground\": true
    },
    {
        \"label\": \"AI: Doctor\",
        \"type\": \"shell\",
        \"command\": \"\${workspaceFolder}/${SANDB_NAME}/shell.sh -c '\${workspaceFolder}/${SANDB_NAME}/doctor.sh'\",
        \"options\": { \"cwd\": \"\${workspaceFolder}\" },
        \"presentation\": {
            \"clear\": true,
            \"echo\": true,
            \"focus\": true,
            \"panel\": \"dedicated\",
            \"reveal\": \"always\",
            \"showReuseMessage\": false
        },
        \"problemMatcher\": [],
        \"isBackground\": false
    }
]"

# Create .vscode directory if it doesn't exist
mkdir -p "${VSCODE_DIR}"

# Check if jq is available
if ! command -v jq &> /dev/null; then
    echo "ERROR: jq is required but not installed."
    echo "Install with: sudo apt install jq"
    exit 1
fi

# If tasks.json doesn't exist, create it
if [ ! -f "${TASKS_FILE}" ]; then
    echo "Creating new tasks.json..."
    cat > "${TASKS_FILE}" << 'EOF'
{
    "version": "2.0.0",
    "tasks": []
}
EOF
fi

# Read existing tasks.json
EXISTING=$(cat "${TASKS_FILE}")

# Check if it's valid JSON
if ! echo "${EXISTING}" | jq empty 2>/dev/null; then
    echo "ERROR: Existing tasks.json is not valid JSON"
    exit 1
fi

# Parse AI tasks
AI_TASKS_PARSED=$(echo "${AI_TASKS}" | jq '.')

# Get existing tasks, filtering out any existing AI: tasks (idempotent)
FILTERED_TASKS=$(echo "${EXISTING}" | jq '.tasks | map(select(.label | startswith("AI:") | not))')

# Combine filtered tasks with AI tasks
NEW_TASKS=$(echo "${FILTERED_TASKS}" | jq --argjson ai "${AI_TASKS_PARSED}" '. + $ai')

# Update the tasks.json
echo "${EXISTING}" | jq --argjson tasks "${NEW_TASKS}" '.tasks = $tasks' > "${TASKS_FILE}"

echo "OK: Tasks installed to ${TASKS_FILE}"

# ============================================================================
# Summary
# ============================================================================
echo ""
echo "=== Installation Complete ==="
echo ""
echo "Available VS Code tasks (Command Palette -> Tasks: Run Task):"
echo "  - AI: Build Proxy   - Build Squid proxy image"
echo "  - AI: Build CLI     - Build CLI sandbox image"
echo "  - AI: Start Proxy   - Start Squid proxy container"
echo "  - AI: Stop Proxy    - Stop Squid proxy container"
echo "  - AI: Stop CLI      - Stop CLI container"
echo "  - AI: Shell         - Open interactive shell in container"
echo "  - AI: Doctor        - Verify network isolation and connectivity"
echo ""
echo "Quick start:"
echo "  ${SANDB_NAME}/cli/build.sh      # Build CLI image"
echo "  ${SANDB_NAME}/proxy/start.sh    # Start proxy"
echo "  ${SANDB_NAME}/shell.sh          # Get shell"

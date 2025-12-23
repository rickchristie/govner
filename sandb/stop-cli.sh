#!/bin/bash
# Stop AI CLI container for current workspace
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
WORKSPACE_NAME="$(basename "${WORKSPACE_DIR}")"
CONTAINER_NAME="ai-cli-${WORKSPACE_NAME}"

if docker ps --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "Stopping CLI container: ${CONTAINER_NAME}"
    docker stop "${CONTAINER_NAME}"
    echo "Done: CLI container stopped"
else
    echo "CLI container not running: ${CONTAINER_NAME}"
fi

#!/bin/bash
# Build the AI CLI Sandbox image
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SANDB_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
WORKSPACE_DIR="$(cd "${SANDB_DIR}/.." && pwd)"
WORKSPACE_NAME="$(basename "${WORKSPACE_DIR}")"
CONTAINER_NAME="ai-cli-${WORKSPACE_NAME}"
IMAGE_NAME="ai-cli-${WORKSPACE_NAME}:latest"

# Stop and remove only the container for this workspace
if docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "Stopping and removing container: ${CONTAINER_NAME}..."
    docker stop "${CONTAINER_NAME}" 2>/dev/null || true
    docker rm -f "${CONTAINER_NAME}" 2>/dev/null || true
fi

# Remove the image
echo "Removing old image..."
docker rmi -f "${IMAGE_NAME}" 2>/dev/null || true

# Build fresh image
echo "Building AI CLI Sandbox image..."
docker build \
    --build-arg USER_UID=$(id -u) \
    --build-arg USER_GID=$(id -g) \
    -t "${IMAGE_NAME}" \
    "${SCRIPT_DIR}"

echo "Done: Image built: ${IMAGE_NAME}"

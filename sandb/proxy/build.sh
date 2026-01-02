#!/bin/bash
# Build the AI Sandbox Squid Proxy image
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONTAINER_NAME="ai-sandbox-proxy"
IMAGE_NAME="ai-sandbox-proxy:latest"

# Stop running proxy container
echo "Stopping proxy container..."
docker stop "${CONTAINER_NAME}" 2>/dev/null || true

# Remove proxy container
echo "Removing proxy container..."
docker rm -f "${CONTAINER_NAME}" 2>/dev/null || true

# Remove the image
echo "Removing old image..."
docker rmi -f "${IMAGE_NAME}" 2>/dev/null || true

# Build fresh image
echo "Building AI Sandbox Squid Proxy image..."
docker build \
    --no-cache \
    -t "${IMAGE_NAME}" \
    "${SCRIPT_DIR}"

echo "Done: Image built: ${IMAGE_NAME}"

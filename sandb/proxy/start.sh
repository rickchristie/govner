#!/bin/bash
# Start Squid proxy container for AI CLI Sandbox
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONTAINER_NAME="ai-sandbox-proxy"
IMAGE_NAME="ai-sandbox-proxy:latest"

# Check if container already running
if docker ps --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "Done: Squid proxy already running: ${CONTAINER_NAME}"
    exit 0
fi

# Check if container exists but stopped
if docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "Starting existing Squid proxy container..."
    docker start "${CONTAINER_NAME}"
    echo "Done: Squid proxy started: ${CONTAINER_NAME}"
    exit 0
fi

# Build image if needed
if ! docker images --format '{{.Repository}}:{{.Tag}}' | grep -q "^${IMAGE_NAME}$"; then
    echo "Building Squid proxy image..."
    docker build -t "${IMAGE_NAME}" "${SCRIPT_DIR}"
fi

# Run new container
# --network host: Required for proxy to access host services
# Docker socket: Only mount if you need container management from proxy
echo "Starting Squid proxy container..."
docker run -d \
    --name "${CONTAINER_NAME}" \
    --network host \
    --restart unless-stopped \
    "${IMAGE_NAME}"

# ============================================================================
# OPTIONAL: Mount docker socket for container management
# ============================================================================
# Uncomment the following if you need to manage containers from the proxy
# (e.g., for test database helper services):
#
# docker run -d \
#     --name "${CONTAINER_NAME}" \
#     --network host \
#     --restart unless-stopped \
#     -v /var/run/docker.sock:/var/run/docker.sock \
#     "${IMAGE_NAME}"
# ============================================================================

echo "Done: Squid proxy started: ${CONTAINER_NAME}"
echo "   Squid proxy listening on localhost:3128"

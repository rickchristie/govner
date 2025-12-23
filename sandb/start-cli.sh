#!/bin/bash
# Start AI CLI container for current workspace
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
WORKSPACE_NAME="$(basename "${WORKSPACE_DIR}")"
SANDB_NAME="$(basename "${SCRIPT_DIR}")"
CONTAINER_NAME="ai-cli-${WORKSPACE_NAME}"
IMAGE_NAME="ai-cli-${WORKSPACE_NAME}:latest"

# Validate container name
if [[ ! "${CONTAINER_NAME}" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$ ]]; then
    echo "ERROR: Invalid container name: ${CONTAINER_NAME}"
    echo "Rename workspace directory (remove invalid chars)"
    exit 1
fi

# Check if container already running
if docker ps --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "Done: CLI container already running: ${CONTAINER_NAME}"
    exit 0
fi

# Check if container exists but stopped
if docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "Starting existing CLI container..."
    docker start "${CONTAINER_NAME}"
    echo "Done: CLI container started: ${CONTAINER_NAME}"
    exit 0
fi

# Check if image exists
if ! docker images --format '{{.Repository}}:{{.Tag}}' | grep -q "^${IMAGE_NAME}$"; then
    echo "Image not found. Building..."
    "${SCRIPT_DIR}/cli/build.sh"
fi

# Check if proxy is running
if ! docker ps --format '{{.Names}}' | grep -q "^ai-sandbox-proxy$"; then
    echo "WARNING: Squid proxy not running. Starting..."
    "${SCRIPT_DIR}/proxy/start.sh"
fi

# Create auth directories if they don't exist
mkdir -p ~/.claude
mkdir -p ~/.copilot

# ============================================================================
# VOLUME MOUNTS - CUSTOMIZE FOR YOUR PROJECT
# ============================================================================
VOLUME_ARGS=""

# Mount workspace at same path as host for VS Code IDE integration
VOLUME_ARGS="${VOLUME_ARGS} -v ${WORKSPACE_DIR}:${WORKSPACE_DIR}"

# Mount .git/hooks read-only to prevent hook injection attacks
if [ -d "${WORKSPACE_DIR}/.git/hooks" ]; then
    VOLUME_ARGS="${VOLUME_ARGS} -v ${WORKSPACE_DIR}/.git/hooks:${WORKSPACE_DIR}/.git/hooks:ro"
fi

# Claude Code config and auth
VOLUME_ARGS="${VOLUME_ARGS} -v ${HOME}/.claude:/home/user/.claude"
if [ -f "${HOME}/.claude.json" ]; then
    VOLUME_ARGS="${VOLUME_ARGS} -v ${HOME}/.claude.json:/home/user/.claude.json"
fi

# GitHub Copilot config
VOLUME_ARGS="${VOLUME_ARGS} -v ${HOME}/.copilot:/home/user/.copilot"

# Git identity (read-only)
if [ -f "${HOME}/.gitconfig" ]; then
    VOLUME_ARGS="${VOLUME_ARGS} -v ${HOME}/.gitconfig:/home/user/.gitconfig:ro"
fi

# Go module cache (read-only - run `go mod download` on host first)
# Note: `go get` won't work in container. Download modules on host first.
GO_MOD_CACHE="${GOPATH:-$HOME/go}/pkg/mod"
GO_BUILD_CACHE="${HOME}/.cache/go-build"
mkdir -p "$GO_MOD_CACHE" "$GO_BUILD_CACHE"
VOLUME_ARGS="${VOLUME_ARGS} -v ${GO_MOD_CACHE}:/go/pkg/mod:ro"
VOLUME_ARGS="${VOLUME_ARGS} -v ${GO_BUILD_CACHE}:/home/user/.cache/go-build"

# ============================================================================
# OPTIONAL: Additional cache mounts
# ============================================================================
# Python pip cache
# VOLUME_ARGS="${VOLUME_ARGS} -v ${HOME}/.cache/pip:/home/user/.cache/pip:ro"

# Node modules cache
# VOLUME_ARGS="${VOLUME_ARGS} -v ${HOME}/.npm:/home/user/.npm:ro"
# ============================================================================

# ============================================================================
# ENVIRONMENT VARIABLES
# ============================================================================
ENV_ARGS=""
ENV_ARGS="${ENV_ARGS} -e HTTP_PROXY=http://host.docker.internal:3128"
ENV_ARGS="${ENV_ARGS} -e HTTPS_PROXY=http://host.docker.internal:3128"
ENV_ARGS="${ENV_ARGS} -e NO_PROXY=localhost,127.0.0.1"

# Go: readonly mode (module cache is read-only, `go get` won't work)
ENV_ARGS="${ENV_ARGS} -e GOFLAGS=-mod=readonly"
# ============================================================================

# Run new container
echo "Starting CLI container: ${CONTAINER_NAME}"
docker run -d \
    --name "${CONTAINER_NAME}" \
    --add-host=host.docker.internal:host-gateway \
    ${VOLUME_ARGS} \
    -w "${WORKSPACE_DIR}" \
    ${ENV_ARGS} \
    --cap-drop=ALL \
    --security-opt=no-new-privileges \
    --init \
    "${IMAGE_NAME}" \
    sleep infinity

echo "Done: CLI container started: ${CONTAINER_NAME}"
echo "   Use: ${SANDB_NAME}/shell.sh for interactive shell"

#!/bin/bash
# Stop Squid proxy container
set -e

CONTAINER_NAME="ai-sandbox-proxy"

if docker ps --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "Stopping Squid proxy container..."
    docker stop "${CONTAINER_NAME}"
    echo "Done: Squid proxy stopped"
else
    echo "Squid proxy not running"
fi

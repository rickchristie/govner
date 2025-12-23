#!/bin/bash
# Entrypoint for AI CLI Sandbox container
# Starts socat port forwarding (if configured) and then executes the command

set -e

# Create aliases for auto-approve mode (sandbox is already isolated)
# ~/.claude is mounted from host, so we use aliases instead of modifying settings
if ! grep -q "AI Sandbox: Auto-approve aliases" /home/user/.bashrc 2>/dev/null; then
    cat >> /home/user/.bashrc << 'EOF'

# AI Sandbox: Auto-approve aliases (container is isolated)
alias claude='claude --dangerously-skip-permissions'
alias copilot='copilot --allow-all-tools'
EOF
fi

# ============================================================================
# SOCAT PORT FORWARDING (EXAMPLE - CUSTOMIZE FOR YOUR PROJECT)
# ============================================================================
# Uncomment and modify these to forward ports from container to host.
# This is useful for accessing host services (databases, APIs, etc.)
#
# Example: Forward container localhost:5432 to host's PostgreSQL
# run_socat() {
#     local port=$1
#     local name=$2
#     while true; do
#         echo "[socat:${port}] Starting ${name} forwarder..."
#         # backlog=5000: For high-concurrency scenarios
#         socat TCP-LISTEN:${port},bind=127.0.0.1,fork,reuseaddr,backlog=5000 TCP:host.docker.internal:${port} 2>&1
#         exit_code=$?
#         echo "[socat:${port}] WARNING: ${name} forwarder exited (code: ${exit_code}), restarting in 1s..." >&2
#         sleep 1
#     done
# }
#
# echo "Starting port forwarding..."
# run_socat 5432 "postgres" &
# run_socat 6379 "redis" &
#
# # Give socat a moment to start
# sleep 0.3
#
# echo "Port forwarding active:"
# echo "  localhost:5432 -> host:5432 (postgres)"
# echo "  localhost:6379 -> host:6379 (redis)"
# echo ""
# ============================================================================

# Execute the command passed to the container
exec "$@"

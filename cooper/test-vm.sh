#!/usr/bin/env bash
# Cooper VM release gate. This test needs Linux x86-64, KVM, nested KVM, and
# Docker. It uses its own runtime namespace and does not use provider logins.
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "${SCRIPT_DIR}/.." && pwd)
RUNTIME_NAMESPACE=cooper-vm-e2e
LOG_FILE=${COOPER_VM_TEST_LOG:-/tmp/cooper-vm.txt}
PREPARE_LOG=${COOPER_VM_PREPARE_LOG:-/tmp/cooper-vm-prepare.txt}

cleanup_runtime() {
    local -a containers networks
    mapfile -t containers < <(docker ps -aq --filter "name=^/${RUNTIME_NAMESPACE}-" 2>/dev/null || true)
    if [ "${#containers[@]}" -gt 0 ]; then
        docker rm -f "${containers[@]}" >/dev/null 2>&1 || true
    fi
    mapfile -t networks < <(docker network ls --format '{{.Name}}' --filter "name=^${RUNTIME_NAMESPACE}-" 2>/dev/null || true)
    if [ "${#networks[@]}" -gt 0 ]; then
        docker network rm "${networks[@]}" >/dev/null 2>&1 || true
    fi
}

if [ "${1:-}" = clean ]; then
    cleanup_runtime
    exit 0
fi
if [ "$#" -ne 0 ]; then
    echo "Usage: ./cooper/test-vm.sh [clean]" >&2
    exit 2
fi

trap cleanup_runtime EXIT INT TERM
cd "$REPO_ROOT"

"${SCRIPT_DIR}/dev/setup.sh" --check
go build -C ./cooper -o ./cooper .
timeout 15m ./cooper/cooper vm prepare >"$PREPARE_LOG" 2>&1

PREPARED_BASE=${HOME}/.cooper/vm/assets/schema-1/cooper-guest-base.qcow2
if [ ! -s "$PREPARED_BASE" ]; then
    echo "Prepared Cooper VM base is missing: $PREPARED_BASE" >&2
    exit 1
fi

COOPER_RUN_VM_E2E=1 \
COOPER_VM_PREPARED_BASE="$PREPARED_BASE" \
COOPER_VM_BINARY="$REPO_ROOT/cooper/cooper" \
go test -C ./cooper -v ./internal/vme2e -count=1 -timeout=75m >"$LOG_FILE" 2>&1

echo "Cooper VM release gate passed."
echo "Log: $LOG_FILE"

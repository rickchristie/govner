#!/usr/bin/env bash

set -euo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly SETUP_SCRIPT="$SCRIPT_DIR/setup.sh"
readonly TEST_OUTPUT="$(mktemp /tmp/cooper-vm-setup-test.XXXXXX)"

cleanup() {
    rm -f -- "$TEST_OUTPUT"
}

trap cleanup EXIT

fail() {
    printf 'setup_test: %s\n' "$*" >&2
    exit 1
}

bash -n "$SETUP_SCRIPT"

help_output="$($SETUP_SCRIPT --help)"
[[ "$help_output" == *"Install the Linux host requirements"* ]] || fail "help description is missing"
[[ "$help_output" == *"--check"* ]] || fail "check option is missing from help"
[[ "$help_output" == *"Do not use sudo"* ]] || fail "normal-user safety rule is missing from help"

if "$SETUP_SCRIPT" --invalid-option >"$TEST_OUTPUT" 2>&1; then
    fail "invalid option succeeded"
fi
grep -q "unknown option" "$TEST_OUTPUT" || fail "invalid option error is unclear"

set +e
check_output="$($SETUP_SCRIPT --check 2>&1)"
check_status=$?
set -e
if [ "$check_status" -ne 0 ] && [ "$check_status" -ne 1 ]; then
    fail "check returned unexpected status $check_status"
fi
[[ "$check_output" == *"Checking Cooper VM host requirements"* ]] || fail "check report header is missing"
[[ "$check_output" == *"Package "* ]] || fail "check report does not include package state"
[[ "$check_output" == *"Available resources:"* ]] || fail "check report does not include resources"

printf 'setup_test: all tests passed\n'

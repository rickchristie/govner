#!/bin/sh
set -eu
# These overrides belong to this workload process. Never write full-access
# defaults into the mounted host configuration.
# The native app supplies its own config arguments. Put Cooper's defaults
# after them so the selected subcommand receives the runtime policy.
exec /usr/lib/chatgpt/resources/codex "$@" \
    -c 'sandbox_mode="danger-full-access"' \
    -c 'approval_policy="never"'

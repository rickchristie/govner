#!/bin/bash
set -eu
if [ "${1:-}" = --version ]; then
    exec dpkg-query --show --showformat='${Version}\n' chatgpt
fi
# The package's launcher stays untouched. The wrapper supplies runtime proxy
# settings and preserves the complete selected desktop state directory.
export CODEX_ELECTRON_USER_DATA_PATH="${CODEX_ELECTRON_USER_DATA_PATH:-${XDG_CONFIG_HOME:-$HOME/.config}/Codex}"
export CODEX_CLI_PATH=/opt/cooper/desktop/core.sh
# A launch without arguments also means "show the app". Native second-instance
# handling does not always raise its window above a terminal on Linux.
if [ "$#" = 0 ]; then
    native_lock=$(readlink "$CODEX_ELECTRON_USER_DATA_PATH/SingletonLock" 2>/dev/null || true)
    native_pid=${native_lock##*-}
    if [[ "$native_pid" =~ ^[0-9]+$ ]] && [ "$native_lock" = "$(hostname)-$native_pid" ] && kill -0 "$native_pid" 2>/dev/null; then
        if timeout 2s xdotool search --onlyvisible --all --pid "$native_pid" '.*' windowactivate --sync 2>/dev/null; then
            exit 0
        fi
    fi
fi
# The native browser selects this path before Electron reads the environment.
# Pass both forms so its databases and the app use the same selected root.
/usr/bin/chatgpt --user-data-dir="$CODEX_ELECTRON_USER_DATA_PATH" \
    --disable-setuid-sandbox \
    --password-store="$(cat /var/lib/cooper/desktop/password-store)" \
    --proxy-server="${HTTPS_PROXY:?Cooper proxy is required}" "$@" &
app_pid=$!
trap 'kill -TERM "$app_pid" 2>/dev/null || true' TERM INT
status=0
wait "$app_pid" || status=$?
if kill -0 "$app_pid" 2>/dev/null; then
    wait "$app_pid" || status=$?
fi
# Chromium can leave its singleton link after Quit. Remove only this child's
# link after it exits. A second invocation that forwards to the existing app
# must never remove that app's lock. Foreign host and VM locks stay intact.
lock_path="$CODEX_ELECTRON_USER_DATA_PATH/SingletonLock"
if [ "$(readlink "$lock_path" 2>/dev/null || true)" = "$(hostname)-$app_pid" ]; then
    rm -- "$lock_path"
fi
exit "$status"

#!/bin/bash
set -euo pipefail
desktop_dir=/var/lib/cooper/desktop
mkdir -p "$desktop_dir"
# Openbox starts programs from the home directory. Keep the launch workspace
# explicit so its terminal opens in the same directory as a Cooper shell.
export COOPER_DESKTOP_WORKSPACE="$PWD"
if [[ "${TZ:-}" == :* ]] && [ -r "${TZ#:}" ]; then
    # The launch command removes its per-session file after startup. Keep
    # the selected timezone available for later app and terminal processes.
    if [ "${TZ#:}" != "$desktop_dir/localtime" ]; then
        cp -- "${TZ#:}" "$desktop_dir/localtime"
    fi
    export TZ=":$desktop_dir/localtime"
fi
if [ "${1:-}" != --session ]; then
    if curl --fail --silent --max-time 2 http://127.0.0.1:6080/vnc.html >/dev/null; then
        # A closed app can be opened again without replacing the desktop.
        # This file contains only the private bus address, not credentials.
        source "$desktop_dir/bus.env"
        unset COOPER_CHATGPT_COOKIE_KEY
        nohup chatgpt >>"$desktop_dir/app.log" 2>&1 </dev/null &
        exit 0
    fi
    if ! unshare -Ur python3 -c 'import os; os.chroot("/")' 2>/dev/null; then
        echo 'This host blocks the desktop user namespace. Use cooper vm chatgpt, or ask the host administrator to enable unprivileged user namespaces.' >&2
        exit 1
    fi
    nohup dbus-run-session -- "$0" --session >"$desktop_dir/session.log" 2>&1 </dev/null &
    for attempt in {1..300}; do
        if curl --fail --silent --max-time 1 http://127.0.0.1:6080/vnc.html >/dev/null; then
            exit 0
        fi
        sleep 0.1
    done
    echo "Desktop did not start. See $desktop_dir/session.log inside the workload." >&2
    exit 1
fi

# One session owns its window manager, bus, app, and viewer endpoint. The
# lock also handles two host launch commands that arrive together.
exec 9>"$desktop_dir/session.lock"
flock -n 9 || exit 0
printf '%s\n' "$$" >"$desktop_dir/session.pid"
printf 'export DBUS_SESSION_BUS_ADDRESS=%q\n' "$DBUS_SESSION_BUS_ADDRESS" >"$desktop_dir/bus.env"
cleanup() {
    kill $(jobs -pr) 2>/dev/null || true
    wait || true
    rm -f "$desktop_dir/session.pid" "$desktop_dir/bus.env"
}
trap cleanup EXIT
trap 'exit 0' TERM INT
if [ -n "${COOPER_CHATGPT_COOKIE_KEY:-}" ]; then
    # The session bus and keyring are private. Import only the key needed by
    # the selected app's encrypted cookies, never the host keyring or bus.
    mkdir -p "$desktop_dir/keyring"
    export GNOME_KEYRING_CONTROL="$desktop_dir/keyring"
    gnome-keyring-daemon --start --components=secrets \
        --control-directory="$desktop_dir/keyring" >/dev/null
    # Chromium searches only the default collection. Its session collection
    # keeps the imported key in memory and needs no unlock prompt.
    gdbus call --session --dest org.freedesktop.secrets \
        --object-path /org/freedesktop/secrets \
        --method org.freedesktop.Secret.Service.SetAlias \
        default /org/freedesktop/secrets/collection/session >/dev/null
    printf '%s' "$COOPER_CHATGPT_COOKIE_KEY" | base64 -d | \
        secret-tool store --collection=session --label='Chromium Safe Storage' \
        application chromium xdg:schema chrome_libsecret_os_crypt_password_v2
    printf '%s\n' gnome-libsecret >"$desktop_dir/password-store"
else
    printf '%s\n' basic >"$desktop_dir/password-store"
fi
unset COOPER_CHATGPT_COOKIE_KEY
mkdir -p "$HOME/.pki/nssdb"
if [ ! -f "$HOME/.pki/nssdb/cert9.db" ]; then
    certutil -N --empty-password -d "sql:$HOME/.pki/nssdb"
fi
certutil -A -d "sql:$HOME/.pki/nssdb" -n cooper-proxy -t 'C,,' \
    -i /usr/local/share/ca-certificates/cooper-ca.crt
openbox --config-file /opt/cooper/desktop/openbox.xml &
window_manager_pid=$!
chatgpt >"$desktop_dir/app.log" 2>&1 &
app_pid=$!
app_ready=0
for attempt in {1..300}; do
    if ! kill -0 "$app_pid" 2>/dev/null; then break; fi
    native_lock=$(readlink "$CODEX_ELECTRON_USER_DATA_PATH/SingletonLock" 2>/dev/null || true)
    native_pid=${native_lock##*-}
    if [[ "$native_pid" =~ ^[0-9]+$ ]] && xdotool search --onlyvisible --all --pid "$native_pid" '.*' >/dev/null 2>&1; then
        app_ready=1
        break
    fi
    sleep 0.1
done
if [ "$app_ready" != 1 ]; then
    echo "ChatGPT did not open a window. See $desktop_dir/app.log. Close any other app that uses the selected state." >&2
    exit 1
fi
# The port exists only in the isolated workload network. The host viewer
# reaches this fixed endpoint through the authenticated VM control channel.
python3 -m websockify --web=/usr/share/novnc 6080 127.0.0.1:5900 &
viewer_pid=$!
# Closing ChatGPT leaves the desktop available so the user can reopen it.
wait -n "$window_manager_pid" "$viewer_pid"

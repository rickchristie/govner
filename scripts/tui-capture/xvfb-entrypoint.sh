#!/usr/bin/env bash

set -euo pipefail

output_path="/output/capture.png"
terminal_columns=120
terminal_rows=36
font_family="DejaVu Sans Mono"
font_size=18
startup_delay=1
action_types=()
action_values=()

fail() {
    printf 'capture-tui-xvfb: %s\n' "$*" >&2
    exit 1
}

require_value() {
    if [ "$2" -lt 2 ]; then
        fail "$1 requires a value."
    fi
}

x_key_name() {
    case "$1" in
        up) printf 'Up' ;;
        down) printf 'Down' ;;
        left) printf 'Left' ;;
        right) printf 'Right' ;;
        enter) printf 'Return' ;;
        space) printf 'space' ;;
        tab) printf 'Tab' ;;
        escape) printf 'Escape' ;;
        backspace) printf 'BackSpace' ;;
        pageup) printf 'Prior' ;;
        pagedown) printf 'Next' ;;
        home) printf 'Home' ;;
        end) printf 'End' ;;
        *) fail "unsupported key: $1" ;;
    esac
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --output)
            require_value "$1" "$#"
            output_path="$2"
            shift 2
            ;;
        --columns)
            require_value "$1" "$#"
            terminal_columns="$2"
            shift 2
            ;;
        --rows)
            require_value "$1" "$#"
            terminal_rows="$2"
            shift 2
            ;;
        --font-family)
            require_value "$1" "$#"
            font_family="$2"
            shift 2
            ;;
        --font-size)
            require_value "$1" "$#"
            font_size="$2"
            shift 2
            ;;
        --startup-delay)
            require_value "$1" "$#"
            startup_delay="$2"
            shift 2
            ;;
        --key)
            require_value "$1" "$#"
            action_types+=("key")
            action_values+=("$2")
            shift 2
            ;;
        --type)
            require_value "$1" "$#"
            action_types+=("type")
            action_values+=("$2")
            shift 2
            ;;
        --)
            shift
            break
            ;;
        *)
            fail "unknown option: $1"
            ;;
    esac
done

[ "$#" -gt 0 ] || fail "an executable is required after --."

display_number=99
display="127.0.0.1:${display_number}"
window_title="Govner TUI Capture"
terminal_pid=""
xvfb_pid=""

cleanup() {
    if [ -n "$terminal_pid" ]; then
        kill "$terminal_pid" 2>/dev/null || true
        wait "$terminal_pid" 2>/dev/null || true
    fi
    if [ -n "$xvfb_pid" ]; then
        kill "$xvfb_pid" 2>/dev/null || true
        wait "$xvfb_pid" 2>/dev/null || true
    fi
}
trap cleanup EXIT INT TERM

Xvfb ":${display_number}" \
    -screen 0 2560x1600x24 \
    -listen tcp \
    -nolisten unix \
    -noreset &
xvfb_pid=$!
export DISPLAY="$display"

display_ready=0
for _ in $(seq 1 50); do
    if xdpyinfo >/dev/null 2>&1; then
        display_ready=1
        break
    fi
    sleep 0.1
done
[ "$display_ready" -eq 1 ] || fail "Xvfb did not become ready within 5 seconds."

xterm \
    -geometry "${terminal_columns}x${terminal_rows}+0+0" \
    -fa "$font_family" \
    -fs "$font_size" \
    -u8 \
    -b 0 \
    -bg "#171717" \
    -fg "#c8c8c8" \
    -tn xterm-256color \
    -title "$window_title" \
    -e "$@" &
terminal_pid=$!

if ! window_id="$(timeout 10s xdotool search --sync --limit 1 --name "$window_title")"; then
    fail "XTerm did not open within 10 seconds."
fi

sleep "$startup_delay"
if ! kill -0 "$terminal_pid" 2>/dev/null; then
    fail "the TUI exited before capture."
fi

xdotool windowfocus --sync "$window_id"

for index in "${!action_types[@]}"; do
    if [ "${action_types[$index]}" = "key" ]; then
        xdotool key \
            --window "$window_id" \
            --clearmodifiers \
            "$(x_key_name "${action_values[$index]}")"
    else
        xdotool type \
            --window "$window_id" \
            --clearmodifiers \
            --delay 0 \
            -- "${action_values[$index]}"
    fi
    sleep 0.25
done

import -display "$display" -window "$window_id" "$output_path"
[ -s "$output_path" ] || fail "ImageMagick did not create the PNG."

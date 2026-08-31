#!/usr/bin/env bash

set -euo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly VHS_IMAGE="ghcr.io/charmbracelet/vhs@sha256:9d5fc3dc0c160b0fb1d2212baff07e6bdf3fa9438c504a3237484567302fcf93"
readonly XVFB_IMAGE="govner-tui-capture-xvfb:1"
readonly XVFB_CONTEXT="$SCRIPT_DIR/tui-capture"
readonly XVFB_DOCKERFILE="$XVFB_CONTEXT/Dockerfile.xvfb"
readonly XVFB_ENTRYPOINT="$XVFB_CONTEXT/xvfb-entrypoint.sh"

show_help() {
    cat <<'EOF'
Capture a deterministic TUI state as a PNG under /tmp.

Usage:
  ./scripts/capture-tui.sh prepare [vhs|xvfb|all]
  ./scripts/capture-tui.sh [options] -- EXECUTABLE [ARGUMENT ...]

Capture options:
  --backend NAME       Use vhs or xvfb. The default is vhs.
  --output PATH        Required PNG path under /tmp.
  --force              Replace the exact output file if it exists.
  --width PIXELS       VHS canvas width. The default is 1280.
  --height PIXELS      VHS canvas height. The default is 720.
  --padding PIXELS     VHS canvas padding. The default is 16.
  --columns CELLS      XTerm column count. The default is 120.
  --rows CELLS         XTerm row count. The default is 36.
  --font-family NAME   Terminal font. The default is DejaVu Sans Mono.
  --font-size NUMBER   Terminal font size. The default is 18.
  --startup-delay SEC  Delay before actions. The default is 1.
  --wait-regex REGEX   VHS-only screen condition. The default timeout is 15 s.
  --wait-timeout SEC   VHS screen-condition timeout. The default is 15.
  --key NAME           Send a key before capture. Repeat as needed.
  --type TEXT          Type text before capture. Repeat as needed.
  -h, --help           Show this help.

Supported keys:
  up down left right enter space tab escape backspace pageup pagedown home end

The prepare command is the only mode that uses the network. Capture containers
have no network and mount only the executable and a private temporary output
directory. A Docker capture requires a static executable. For a Go program:

  GOCACHE=/tmp/tui-capture-go-cache CGO_ENABLED=0 \
    go build -o /tmp/my-tui ./path/to/package

Examples:
  ./scripts/capture-tui.sh prepare all

  ./scripts/capture-tui.sh \
    --output /tmp/gowt-tree.png \
    --wait-regex GOWT \
    -- /tmp/gowt-static --storybook

  ./scripts/capture-tui.sh \
    --backend xvfb \
    --output /tmp/gowt-log.png \
    --key enter \
    -- /tmp/gowt-static --storybook
EOF
}

fail() {
    printf 'capture-tui: %s\n' "$*" >&2
    exit 1
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || fail "$1 is required."
}

require_option_value() {
    local option="$1"
    local remaining="$2"

    if [ "$remaining" -lt 2 ]; then
        fail "$option requires a value."
    fi
}

require_positive_integer() {
    local option="$1"
    local value="$2"

    if ! [[ "$value" =~ ^[1-9][0-9]*$ ]]; then
        fail "$option must be a positive integer."
    fi
}

require_nonnegative_integer() {
    local option="$1"
    local value="$2"

    if ! [[ "$value" =~ ^[0-9]+$ ]]; then
        fail "$option must be a nonnegative integer."
    fi
}

require_nonnegative_number() {
    local option="$1"
    local value="$2"

    if ! [[ "$value" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
        fail "$option must be a nonnegative number of seconds."
    fi
}

is_supported_key() {
    case "$1" in
        up|down|left|right|enter|space|tab|escape|backspace|pageup|pagedown|home|end)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

reject_line_break() {
    local label="$1"
    local value="$2"

    if [[ "$value" == *$'\n'* || "$value" == *$'\r'* ]]; then
        fail "$label must not contain a line break."
    fi
}

prepare_vhs() {
    printf 'Preparing pinned VHS image: %s\n' "$VHS_IMAGE"
    docker pull "$VHS_IMAGE"
}

xvfb_source_version() {
    local dockerfile_hash
    local entrypoint_hash

    require_command sha256sum
    dockerfile_hash="$(sha256sum -- "$XVFB_DOCKERFILE")"
    entrypoint_hash="$(sha256sum -- "$XVFB_ENTRYPOINT")"
    printf '%s-%s\n' "${dockerfile_hash%% *}" "${entrypoint_hash%% *}"
}

prepare_xvfb() {
    local source_version

    source_version="$(xvfb_source_version)"
    printf 'Preparing Govner Xvfb image: %s\n' "$XVFB_IMAGE"
    docker build \
        --label "org.govner.tui-capture.version=$source_version" \
        --tag "$XVFB_IMAGE" \
        --file "$XVFB_DOCKERFILE" \
        "$XVFB_CONTEXT"
}

prepare_images() {
    local backend="${1:-all}"

    if [ "$#" -gt 1 ]; then
        fail "prepare accepts at most one backend."
    fi

    require_command docker
    case "$backend" in
        vhs)
            prepare_vhs
            ;;
        xvfb)
            prepare_xvfb
            ;;
        all)
            prepare_vhs
            prepare_xvfb
            ;;
        *)
            fail "unknown prepare backend: $backend"
            ;;
    esac
}

resolve_executable() {
    local value="$1"
    local resolved

    if [[ "$value" == */* ]]; then
        resolved="$(realpath -e -- "$value" 2>/dev/null)" || fail "executable does not exist: $value"
    else
        resolved="$(command -v "$value" 2>/dev/null)" || fail "executable was not found: $value"
        resolved="$(realpath -e -- "$resolved")"
    fi

    [ -f "$resolved" ] || fail "executable is not a regular file: $resolved"
    [ -x "$resolved" ] || fail "file is not executable: $resolved"
    [[ "$resolved" != *,* ]] || fail "executable path must not contain a comma."
    printf '%s\n' "$resolved"
}

require_container_executable() {
    local executable="$1"
    local description

    description="$(file -Lb -- "$executable")"
    if [[ "$description" == *ELF* && "$description" != *"statically linked"* ]]; then
        fail "Docker capture requires a statically linked executable. Build Go programs with CGO_ENABLED=0."
    fi
}

escape_vhs_text() {
    local value="$1"

    value="${value//\\/\\\\}"
    value="${value//\"/\\\"}"
    printf '%s' "$value"
}

format_shell_command() {
    local result=""
    local argument
    local quoted

    for argument in "$@"; do
        printf -v quoted '%q' "$argument"
        if [ -n "$result" ]; then
            result+=" "
        fi
        result+="$quoted"
    done
    printf '%s' "$result"
}

vhs_key_command() {
    case "$1" in
        up) printf 'Up' ;;
        down) printf 'Down' ;;
        left) printf 'Left' ;;
        right) printf 'Right' ;;
        enter) printf 'Enter' ;;
        space) printf 'Space' ;;
        tab) printf 'Tab' ;;
        escape) printf 'Escape' ;;
        backspace) printf 'Backspace' ;;
        pageup) printf 'PageUp' ;;
        pagedown) printf 'PageDown' ;;
        home) printf 'Home' ;;
        end) printf 'End' ;;
    esac
}

write_vhs_tape() {
    local tape_path="$1"
    local command_line
    local escaped_command
    local escaped_font
    local escaped_text
    local index

    command_line="$(format_shell_command /input/tui "${tui_arguments[@]}")"
    escaped_command="$(escape_vhs_text "exec $command_line")"
    escaped_font="$(escape_vhs_text "$font_family")"

    {
        printf 'Set Shell "bash"\n'
        printf 'Set FontFamily "%s"\n' "$escaped_font"
        printf 'Set FontSize %s\n' "$font_size"
        printf 'Set Width %s\n' "$canvas_width"
        printf 'Set Height %s\n' "$canvas_height"
        printf 'Set Padding %s\n' "$canvas_padding"
        printf 'Set TypingSpeed 0ms\n'
        printf 'Set CursorBlink false\n\n'
        printf 'Type "%s"\n' "$escaped_command"
        printf 'Enter\n'
        if [ -n "$wait_regex" ]; then
            printf 'Wait+Screen@%ss /%s/\n' "$wait_timeout" "$wait_regex"
        else
            printf 'Sleep %ss\n' "$startup_delay"
        fi

        for index in "${!action_types[@]}"; do
            if [ "${action_types[$index]}" = "key" ]; then
                printf '%s\n' "$(vhs_key_command "${action_values[$index]}")"
            else
                escaped_text="$(escape_vhs_text "${action_values[$index]}")"
                printf 'Type "%s"\n' "$escaped_text"
            fi
            printf 'Sleep 250ms\n'
        done

        # Wait+Screen can match before VHS has committed the same frame to its
        # renderer. Let the terminal settle before VHS queues the screenshot.
        printf 'Sleep 500ms\n'
        printf 'Screenshot "/output/capture.png"\n'
        # VHS renders screenshots after it processes the tape. A final delay
        # prevents the terminal shutdown from racing that queued frame.
        printf 'Sleep 500ms\n'
        printf 'Ctrl+C\n'
        printf 'Sleep 500ms\n'
    } > "$tape_path"
}

require_vhs_image() {
    if ! docker image inspect "$VHS_IMAGE" >/dev/null 2>&1; then
        fail "the pinned VHS image is not ready. Run: ./scripts/capture-tui.sh prepare vhs"
    fi
}

require_xvfb_image() {
    local expected_version
    local version

    expected_version="$(xvfb_source_version)"
    if ! version="$(docker image inspect \
        --format '{{ index .Config.Labels "org.govner.tui-capture.version" }}' \
        "$XVFB_IMAGE" 2>/dev/null)"; then
        fail "the Govner Xvfb image is not ready. Run: ./scripts/capture-tui.sh prepare xvfb"
    fi
    if [ "$version" != "$expected_version" ]; then
        fail "the Govner Xvfb image is outdated. Run: ./scripts/capture-tui.sh prepare xvfb"
    fi
}

run_vhs_capture() {
    local tape_path="$capture_workspace/capture.tape"

    require_vhs_image
    write_vhs_tape "$tape_path"

    docker run \
        --rm \
        --network none \
        --security-opt no-new-privileges \
        --cap-drop ALL \
        --user "$(id -u):$(id -g)" \
        --env HOME=/tmp \
        --env LANG=C.UTF-8 \
        --env LC_ALL=C.UTF-8 \
        --shm-size 256m \
        --tmpfs /tmp:rw,nosuid,nodev \
        --mount "type=bind,src=$tape_path,dst=/vhs/capture.tape,readonly" \
        --mount "type=bind,src=$tui_executable,dst=/input/tui,readonly" \
        --mount "type=bind,src=$capture_workspace,dst=/output" \
        "$VHS_IMAGE" \
        /vhs/capture.tape
}

run_xvfb_capture() {
    local -a docker_arguments
    local index

    [ -z "$wait_regex" ] || fail "--wait-regex is available only with the VHS backend."
    require_xvfb_image

    docker_arguments=(
        --output /output/capture.png
        --columns "$terminal_columns"
        --rows "$terminal_rows"
        --font-family "$font_family"
        --font-size "$font_size"
        --startup-delay "$startup_delay"
    )
    for index in "${!action_types[@]}"; do
        docker_arguments+=("--${action_types[$index]}" "${action_values[$index]}")
    done
    docker_arguments+=(-- /input/tui "${tui_arguments[@]}")

    docker run \
        --rm \
        --network none \
        --security-opt no-new-privileges \
        --cap-drop ALL \
        --user "$(id -u):$(id -g)" \
        --env HOME=/tmp \
        --tmpfs /tmp:rw,nosuid,nodev \
        --mount "type=bind,src=$tui_executable,dst=/input/tui,readonly" \
        --mount "type=bind,src=$capture_workspace,dst=/output" \
        "$XVFB_IMAGE" \
        "${docker_arguments[@]}"
}

save_capture() {
    local staged_capture="$capture_workspace/capture.png"
    local media_type

    [ -s "$staged_capture" ] || fail "$backend did not create a PNG."
    media_type="$(file -b --mime-type -- "$staged_capture")"
    [ "$media_type" = "image/png" ] || fail "$backend created $media_type instead of image/png."

    mkdir -p -- "$(dirname "$output_path")"
    install -m 0644 -- "$staged_capture" "$output_path"
    printf 'Captured %s PNG: %s\n' "$backend" "$output_path"
}

capture_tui() {
    backend="vhs"
    output_path=""
    force_output=0
    canvas_width=1280
    canvas_height=720
    canvas_padding=16
    terminal_columns=120
    terminal_rows=36
    font_family="DejaVu Sans Mono"
    font_size=18
    startup_delay=1
    wait_regex=""
    wait_timeout=15
    action_types=()
    action_values=()

    while [ "$#" -gt 0 ]; do
        case "$1" in
            --backend)
                require_option_value "$1" "$#"
                backend="$2"
                shift 2
                ;;
            --output)
                require_option_value "$1" "$#"
                output_path="$2"
                shift 2
                ;;
            --force)
                force_output=1
                shift
                ;;
            --width)
                require_option_value "$1" "$#"
                require_positive_integer "$1" "$2"
                canvas_width="$2"
                shift 2
                ;;
            --height)
                require_option_value "$1" "$#"
                require_positive_integer "$1" "$2"
                canvas_height="$2"
                shift 2
                ;;
            --padding)
                require_option_value "$1" "$#"
                require_nonnegative_integer "$1" "$2"
                canvas_padding="$2"
                shift 2
                ;;
            --columns)
                require_option_value "$1" "$#"
                require_positive_integer "$1" "$2"
                terminal_columns="$2"
                shift 2
                ;;
            --rows)
                require_option_value "$1" "$#"
                require_positive_integer "$1" "$2"
                terminal_rows="$2"
                shift 2
                ;;
            --font-family)
                require_option_value "$1" "$#"
                reject_line_break "$1" "$2"
                font_family="$2"
                shift 2
                ;;
            --font-size)
                require_option_value "$1" "$#"
                require_positive_integer "$1" "$2"
                font_size="$2"
                shift 2
                ;;
            --startup-delay)
                require_option_value "$1" "$#"
                require_nonnegative_number "$1" "$2"
                startup_delay="$2"
                shift 2
                ;;
            --wait-regex)
                require_option_value "$1" "$#"
                reject_line_break "$1" "$2"
                [[ "$2" != */* ]] || fail "--wait-regex must not contain a slash."
                wait_regex="$2"
                shift 2
                ;;
            --wait-timeout)
                require_option_value "$1" "$#"
                require_positive_integer "$1" "$2"
                wait_timeout="$2"
                shift 2
                ;;
            --key)
                require_option_value "$1" "$#"
                is_supported_key "$2" || fail "unsupported key: $2"
                action_types+=("key")
                action_values+=("$2")
                shift 2
                ;;
            --type)
                require_option_value "$1" "$#"
                reject_line_break "$1" "$2"
                action_types+=("type")
                action_values+=("$2")
                shift 2
                ;;
            -h|--help)
                show_help
                return 0
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

    case "$backend" in
        vhs|xvfb) ;;
        *) fail "unknown backend: $backend" ;;
    esac

    [ -n "$output_path" ] || fail "--output is required."
    [[ "$output_path" != *,* ]] || fail "output path must not contain a comma."
    [ ! -L "$output_path" ] || fail "output path must not be a symbolic link."
    output_path="$(realpath -m -- "$output_path")"
    [[ "$output_path" == /tmp/* ]] || fail "--output must be under /tmp."
    [[ "$output_path" == *.png ]] || fail "--output must end with .png."
    if [ -e "$output_path" ] && [ "$force_output" -ne 1 ]; then
        fail "output already exists. Use --force to replace it: $output_path"
    fi

    [ "$#" -gt 0 ] || fail "an executable is required after --."
    tui_executable="$(resolve_executable "$1")"
    shift
    tui_arguments=("$@")
    require_command file
    require_container_executable "$tui_executable"
    for argument in "${tui_arguments[@]}"; do
        reject_line_break "executable argument" "$argument"
    done

    require_command docker
    capture_workspace="$(mktemp -d /tmp/govner-tui-capture.XXXXXX)"
    trap 'rm -rf -- "$capture_workspace"' EXIT

    case "$backend" in
        vhs) run_vhs_capture ;;
        xvfb) run_xvfb_capture ;;
    esac
    save_capture
}

if [ "${1:-}" = "prepare" ]; then
    shift
    prepare_images "$@"
    exit 0
fi

capture_tui "$@"

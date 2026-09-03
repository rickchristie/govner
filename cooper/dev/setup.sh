#!/usr/bin/env bash

set -Eeuo pipefail

readonly PROGRAM_NAME="cooper-vm-setup"
readonly MODULE_CONFIG_PATH="/etc/modules-load.d/cooper-vm.conf"

MODE="install"
TARGET_USER=""
KVM_MODULE=""
GROUP_WAS_ADDED=0
PROBLEM_COUNT=0
WARNING_COUNT=0
TEMPORARY_FILE=""
REQUIRED_PACKAGES=()

show_help() {
    cat <<'EOF'
Install the Linux host requirements for Cooper VM development.

Usage:
  ./cooper/dev/setup.sh
  ./cooper/dev/setup.sh --check

Options:
  --check      Check the host without changing it.
  -h, --help   Show this help.

Run this script as your normal user. Do not use sudo to start the script. The
install mode uses sudo only for package installation, kernel modules, and KVM
group membership.

The script supports Ubuntu 22.04 or later on x86-64. It does not install
libvirt, create a VM, configure a network, change firewall rules, enable IP
forwarding, or install Docker.
EOF
}

info() {
    printf '[INFO] %s\n' "$*"
}

pass() {
    printf '[OK]   %s\n' "$*"
}

warn() {
    WARNING_COUNT=$((WARNING_COUNT + 1))
    printf '[WARN] %s\n' "$*" >&2
}

problem() {
    PROBLEM_COUNT=$((PROBLEM_COUNT + 1))
    printf '[FAIL] %s\n' "$*" >&2
}

fail() {
    printf '%s: %s\n' "$PROGRAM_NAME" "$*" >&2
    exit 1
}

cleanup() {
    if [ -n "$TEMPORARY_FILE" ] && [ -f "$TEMPORARY_FILE" ]; then
        rm -f -- "$TEMPORARY_FILE"
    fi
}

trap cleanup EXIT

parse_arguments() {
    if [ "$#" -gt 1 ]; then
        fail "accepts at most one option"
    fi

    case "${1:-}" in
        "")
            MODE="install"
            ;;
        --check)
            MODE="check"
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            fail "unknown option: $1"
            ;;
    esac
}

require_normal_user() {
    if [ "$(id -u)" -eq 0 ]; then
        fail "run this script without sudo; it requests sudo only for required host changes"
    fi
    TARGET_USER="$(id -un)"
}

require_supported_host() {
    [ "$(uname -s)" = "Linux" ] || fail "only Linux is supported"
    [ "$(uname -m)" = "x86_64" ] || fail "only x86-64 hosts are supported"
    [ -r /etc/os-release ] || fail "/etc/os-release is required"

    # shellcheck disable=SC1091
    . /etc/os-release
    [ "${ID:-}" = "ubuntu" ] || fail "only Ubuntu is supported"
    dpkg --compare-versions "${VERSION_ID:-0}" ge "22.04" || fail "Ubuntu 22.04 or later is required"

    if grep -qw svm /proc/cpuinfo; then
        KVM_MODULE="kvm_amd"
        return
    fi
    if grep -qw vmx /proc/cpuinfo; then
        KVM_MODULE="kvm_intel"
        return
    fi
    fail "the CPU does not expose AMD-V or Intel VT-x; enable virtualization in firmware"
}

select_required_packages() {
    REQUIRED_PACKAGES=(
        bubblewrap
        cloud-image-utils
        cpu-checker
        ovmf
        qemu-system-common
        qemu-system-gui
        qemu-system-x86
        qemu-utils
        socat
        virt-viewer
    )

    # Ubuntu 22.04 includes virtiofsd in qemu-system-common. Ubuntu 24.04 and
    # later provide the Rust implementation as a separate package.
    if apt-cache show virtiofsd 2>/dev/null | grep -q '^Package: virtiofsd$'; then
        REQUIRED_PACKAGES+=(virtiofsd)
    fi
}

package_is_installed() {
    dpkg-query -W -f='${db:Status-Abbrev}' "$1" 2>/dev/null | grep -q '^ii '
}

missing_packages() {
    local package_name

    for package_name in "${REQUIRED_PACKAGES[@]}"; do
        if ! package_is_installed "$package_name"; then
            printf '%s\n' "$package_name"
        fi
    done
}

install_packages() {
    local -a packages_to_install=()

    command -v sudo >/dev/null 2>&1 || fail "sudo is required for install mode"
    info "Refreshing Ubuntu package metadata"
    sudo apt-get update

    select_required_packages
    mapfile -t packages_to_install < <(missing_packages)
    if [ "${#packages_to_install[@]}" -eq 0 ]; then
        pass "Required Ubuntu packages are already installed"
        return
    fi

    info "Installing: ${packages_to_install[*]}"
    sudo env DEBIAN_FRONTEND=noninteractive apt-get install \
        --yes \
        --no-install-recommends \
        "${packages_to_install[@]}"
}

configure_kvm_group() {
    if ! getent group kvm >/dev/null 2>&1; then
        info "Creating the kvm system group"
        sudo groupadd --system kvm
    fi

    if id -nG "$TARGET_USER" | tr ' ' '\n' | grep -qx kvm; then
        pass "$TARGET_USER is configured as a member of kvm"
        return
    fi

    info "Adding $TARGET_USER to the kvm group"
    sudo usermod --append --groups kvm "$TARGET_USER"
    GROUP_WAS_ADDED=1
}

configure_kernel_modules() {
    TEMPORARY_FILE="$(mktemp /tmp/cooper-vm-modules.XXXXXX)"
    printf '%s\n%s\n' "$KVM_MODULE" "vhost_vsock" >"$TEMPORARY_FILE"
    sudo install \
        --owner=root \
        --group=root \
        --mode=0644 \
        "$TEMPORARY_FILE" \
        "$MODULE_CONFIG_PATH"
    rm -f -- "$TEMPORARY_FILE"
    TEMPORARY_FILE=""

    info "Loading $KVM_MODULE and vhost_vsock"
    sudo modprobe "$KVM_MODULE"
    sudo modprobe vhost_vsock
}

find_virtiofsd() {
    local command_path
    local candidate

    command_path="$(command -v virtiofsd 2>/dev/null || true)"
    for candidate in "$command_path" /usr/lib/qemu/virtiofsd /usr/libexec/virtiofsd; do
        if [ -n "$candidate" ] && [ -x "$candidate" ]; then
            printf '%s\n' "$candidate"
            return 0
        fi
    done
    return 1
}

configured_user_has_kvm() {
    id -nG "$TARGET_USER" | tr ' ' '\n' | grep -qx kvm
}

active_session_has_kvm() {
    id -nG | tr ' ' '\n' | grep -qx kvm
}

verify_package_state() {
    local package_name

    for package_name in "${REQUIRED_PACKAGES[@]}"; do
        if package_is_installed "$package_name"; then
            pass "Package installed: $package_name"
        else
            problem "Package missing: $package_name"
        fi
    done
}

verify_command() {
    local command_name="$1"

    if command -v "$command_name" >/dev/null 2>&1; then
        pass "Command available: $command_name"
    else
        problem "Command missing: $command_name"
    fi
}

verify_devices() {
    local device_path
    local device_group

    for device_path in /dev/kvm /dev/vhost-vsock; do
        if [ ! -c "$device_path" ]; then
            problem "Character device missing: $device_path"
            continue
        fi
        device_group="$(stat -c '%G' "$device_path")"
        if [ "$device_group" != "kvm" ]; then
            problem "$device_path belongs to group $device_group, not kvm"
            continue
        fi
        pass "Device available through kvm: $device_path"
    done

    if [ -c /dev/net/tun ]; then
        pass "Device available: /dev/net/tun"
    else
        problem "Character device missing: /dev/net/tun"
    fi
}

verify_host() {
    local virtiofsd_path
    local cpu_count
    local available_memory_kib
    local available_disk_kib

    PROBLEM_COUNT=0
    WARNING_COUNT=0
    info "Checking Cooper VM host requirements"

    verify_package_state
    verify_command qemu-system-x86_64
    verify_command qemu-img
    verify_command cloud-localds
    verify_command remote-viewer
    verify_command bwrap
    verify_command socat

    if virtiofsd_path="$(find_virtiofsd)"; then
        pass "virtiofsd available: $virtiofsd_path"
    else
        problem "virtiofsd is missing"
    fi

    if compgen -G '/usr/share/OVMF/OVMF_CODE*.fd' >/dev/null; then
        pass "OVMF firmware is available"
    else
        problem "OVMF firmware is missing"
    fi

    if [ -d "/sys/module/$KVM_MODULE" ]; then
        pass "Kernel module loaded: $KVM_MODULE"
    else
        problem "Kernel module is not loaded: $KVM_MODULE"
    fi
    if [ -d /sys/module/vhost_vsock ]; then
        pass "Kernel module loaded: vhost_vsock"
    else
        problem "Kernel module is not loaded: vhost_vsock"
    fi

    verify_devices

    if configured_user_has_kvm; then
        pass "$TARGET_USER is configured as a member of kvm"
    else
        problem "$TARGET_USER is not configured as a member of kvm"
    fi
    if ! active_session_has_kvm; then
        warn "The current login does not include kvm. Log out and log in before VM development."
    fi

    if command -v docker >/dev/null 2>&1; then
        pass "Host Docker command is available for the Cooper proxy"
    else
        warn "Docker is not installed. Cooper still requires its normal Docker prerequisite."
    fi

    cpu_count="$(getconf _NPROCESSORS_ONLN)"
    available_memory_kib="$(awk '$1 == "MemAvailable:" { print $2 }' /proc/meminfo)"
    available_disk_kib="$(df -Pk . | awk 'NR == 2 { print $4 }')"
    info "Available resources: ${cpu_count} CPUs, $((available_memory_kib / 1024)) MiB memory, $((available_disk_kib / 1024)) MiB workspace disk"

    if [ "$PROBLEM_COUNT" -ne 0 ]; then
        printf '\n%d host requirement(s) failed.\n' "$PROBLEM_COUNT" >&2
        return 1
    fi

    printf '\nCooper VM host requirements passed with %d warning(s).\n' "$WARNING_COUNT"
}

main() {
    parse_arguments "$@"
    require_normal_user
    require_supported_host

    if [ "$MODE" = "check" ]; then
        select_required_packages
        verify_host
        return
    fi

    install_packages
    configure_kvm_group
    configure_kernel_modules
    verify_host

    if [ "$GROUP_WAS_ADDED" -eq 1 ]; then
        printf '\nLog out and log in once to activate kvm access in your desktop session.\n'
        printf 'Then run: ./cooper/dev/setup.sh --check\n'
    fi
}

main "$@"

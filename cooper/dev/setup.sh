#!/usr/bin/env bash

set -Eeuo pipefail

readonly PROGRAM_NAME="cooper-vm-setup"
readonly MODULE_CONFIG_PATH="/etc/modules-load.d/cooper-vm.conf"
readonly MODULE_OPTIONS_PATH="/etc/modprobe.d/cooper-vm.conf"

MODE="install"
TARGET_USER=""
KVM_MODULE=""
GROUP_WAS_ADDED=0
PROBLEM_COUNT=0
WARNING_COUNT=0
TEMPORARY_FILE=""

show_help() {
    cat <<'EOF'
Prepare a Linux host for Cooper VM.

Usage:
  ./cooper/dev/setup.sh
  ./cooper/dev/setup.sh --check

Options:
  --check      Check the host without changing it.
  -h, --help   Show this help.

Run this script as your normal user. Do not use sudo to start it. Install mode
uses sudo only to configure KVM and add your user to the kvm group.

Cooper VM supports Linux on x86-64. QEMU and the guest image tools run in
Cooper-owned containers. Cooper includes a static virtiofsd binary. These
tools are not host requirements. Install Docker Engine before you run this
script.
EOF
}

info() { printf '[INFO] %s\n' "$*"; }
pass() { printf '[OK]   %s\n' "$*"; }

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
        "") MODE="install" ;;
        --check) MODE="check" ;;
        -h|--help) show_help; exit 0 ;;
        *) fail "unknown option: $1" ;;
    esac
}

require_normal_user() {
    if [ "$(id -u)" -eq 0 ]; then
        fail "run this script without sudo"
    fi
    TARGET_USER="$(id -un)"
}

detect_kvm_module() {
    [ "$(uname -s)" = "Linux" ] || fail "only Linux is supported"
    [ "$(uname -m)" = "x86_64" ] || fail "only x86-64 hosts are supported"

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

configured_user_has_kvm() {
    id -nG "$TARGET_USER" | tr ' ' '\n' | grep -qx kvm
}

active_session_has_kvm() {
    id -nG | tr ' ' '\n' | grep -qx kvm
}

configure_kvm() {
    command -v sudo >/dev/null 2>&1 || fail "sudo is required for install mode"
    command -v modprobe >/dev/null 2>&1 || fail "modprobe is required"

    if ! getent group kvm >/dev/null 2>&1; then
        info "Creating the kvm system group"
        sudo groupadd --system kvm
    fi
    if configured_user_has_kvm; then
        pass "$TARGET_USER is configured as a member of kvm"
    else
        info "Adding $TARGET_USER to the kvm group"
        sudo usermod --append --groups kvm "$TARGET_USER"
        GROUP_WAS_ADDED=1
    fi

    TEMPORARY_FILE="$(mktemp /tmp/cooper-vm-modules.XXXXXX)"
    printf '%s\n' "$KVM_MODULE" >"$TEMPORARY_FILE"
    sudo install --owner=root --group=root --mode=0644 "$TEMPORARY_FILE" "$MODULE_CONFIG_PATH"
    rm -f -- "$TEMPORARY_FILE"
    TEMPORARY_FILE=""

    TEMPORARY_FILE="$(mktemp /tmp/cooper-vm-options.XXXXXX)"
    printf 'options %s nested=1\n' "$KVM_MODULE" >"$TEMPORARY_FILE"
    sudo install --owner=root --group=root --mode=0644 "$TEMPORARY_FILE" "$MODULE_OPTIONS_PATH"
    rm -f -- "$TEMPORARY_FILE"
    TEMPORARY_FILE=""

    info "Loading $KVM_MODULE"
    sudo modprobe "$KVM_MODULE"
}

verify_nested_kvm() {
    local nested_path="/sys/module/$KVM_MODULE/parameters/nested"
    local nested_value

    if [ ! -r "$nested_path" ]; then
        problem "Nested KVM state is not available at $nested_path"
        return
    fi
    nested_value="$(tr '[:upper:]' '[:lower:]' <"$nested_path")"
    case "$nested_value" in
        1|y|yes) pass "Nested KVM is enabled" ;;
        *) problem "Nested KVM is disabled; run this setup and restart the host before the self-hosting test" ;;
    esac
}

verify_kvm_device() {
    if [ ! -c /dev/kvm ]; then
        problem "Character device missing: /dev/kvm"
        return
    fi
    if [ ! -r /dev/kvm ] || [ ! -w /dev/kvm ]; then
        problem "The current login cannot open /dev/kvm; log out and log in after the kvm group change"
        return
    fi
    pass "The current login can access /dev/kvm"
}

verify_docker() {
    if ! command -v docker >/dev/null 2>&1; then
        problem "Docker Engine is not installed"
        return
    fi
    pass "Docker command is available"
    if docker info >/dev/null 2>&1; then
        pass "Docker daemon is available to the current login"
    else
        problem "Docker daemon is not available to the current login"
    fi
}

verify_resources() {
    local cpu_count
    local available_memory_kib
    local available_disk_kib

    cpu_count="$(getconf _NPROCESSORS_ONLN)"
    available_memory_kib="$(awk '$1 == "MemAvailable:" { print $2 }' /proc/meminfo)"
    available_disk_kib="$(df -Pk . | awk 'NR == 2 { print $4 }')"
    info "Available resources: ${cpu_count} CPUs, $((available_memory_kib / 1024)) MiB memory, $((available_disk_kib / 1024)) MiB workspace disk"

    if [ "$cpu_count" -lt 4 ]; then
        warn "Four or more CPUs are recommended for Cooper VM"
    fi
    if [ "$available_memory_kib" -lt $((16 * 1024 * 1024)) ]; then
        warn "At least 16 GiB of available memory is recommended for the default VM profile"
    fi
    if [ "$available_disk_kib" -lt $((40 * 1024 * 1024)) ]; then
        warn "At least 40 GiB of free workspace disk is recommended"
    fi
}

verify_host() {
    PROBLEM_COUNT=0
    WARNING_COUNT=0
    info "Checking Cooper VM host requirements"

    if [ -d "/sys/module/$KVM_MODULE" ]; then
        pass "Kernel module loaded: $KVM_MODULE"
    else
        problem "Kernel module is not loaded: $KVM_MODULE"
    fi
    verify_nested_kvm

    if configured_user_has_kvm; then
        pass "$TARGET_USER is configured as a member of kvm"
    else
        problem "$TARGET_USER is not configured as a member of kvm"
    fi
    if ! active_session_has_kvm; then
        warn "The current login does not include kvm. Log out and log in before VM development."
    fi

    verify_kvm_device
    verify_docker
    verify_resources

    if [ "$PROBLEM_COUNT" -ne 0 ]; then
        printf '\n%d host requirement(s) failed.\n' "$PROBLEM_COUNT" >&2
        return 1
    fi
    printf '\nCooper VM host requirements passed with %d warning(s).\n' "$WARNING_COUNT"
}

main() {
    parse_arguments "$@"
    require_normal_user
    detect_kvm_module

    if [ "$MODE" = "install" ]; then
        configure_kvm
    fi
    verify_host

    if [ "$GROUP_WAS_ADDED" -eq 1 ]; then
        printf '\nLog out and log in once. Then run: ./cooper/dev/setup.sh --check\n'
    fi
}

main "$@"

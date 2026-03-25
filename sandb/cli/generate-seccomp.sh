#!/bin/bash
# Generate custom seccomp profile for AI CLI container.
#
# Based on Docker's default profile with targeted modifications to enable
# bubblewrap (bwrap) sandboxing for OpenAI Codex CLI.
#
# Changes from Docker default:
#   + clone, clone3   — allow with namespace flags (default blocks namespace creation)
#   + unshare, setns  — allow unconditionally (default requires CAP_SYS_ADMIN)
#   + mount, umount2  — allow unconditionally (only effective inside user namespaces
#                        due to --cap-drop=ALL; cannot affect container's main filesystem)
#   + pivot_root      — allow (default blocks entirely; bwrap uses for root pivot in namespace)
#
# Security note: mount/umount2/pivot_root are allowed at the seccomp level but the container
# runs with --cap-drop=ALL, so these syscalls only succeed inside user namespaces where the
# process has "fake" CAP_SYS_ADMIN. They cannot modify the container's real filesystem.
#
# Re-generate after Docker updates:  ./generate-seccomp.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT="${SCRIPT_DIR}/seccomp-bwrap.json"

# Docker default seccomp profile — pinned to v28.0.0 for reproducibility.
# Bump this tag when upgrading Docker and re-run this script.
DOCKER_TAG="v28.0.0"
DEFAULT_URL="https://raw.githubusercontent.com/moby/moby/${DOCKER_TAG}/profiles/seccomp/default.json"

echo "Downloading Docker default seccomp profile (${DOCKER_TAG})..."
PROFILE=$(curl -sfL "${DEFAULT_URL}") || { echo "ERROR: Failed to download profile"; exit 1; }

echo "Applying bwrap patches..."
echo "${PROFILE}" | jq '
  # 1. Add bwrap-required syscalls to the main unconditional allow list (rule [0]).
  #    These are normally gated behind CAP_SYS_ADMIN or blocked entirely.
  .syscalls[0].names += [
    "clone",
    "clone3",
    "mount",
    "pivot_root",
    "setns",
    "umount2",
    "unshare"
  ] |
  .syscalls[0].names |= unique |

  # 2. Remove the clone3 ERRNO rule.
  #    The default returns ENOSYS for clone3 without CAP_SYS_ADMIN (because clone3
  #    passes flags in a struct that seccomp cannot inspect). Since we want clone3
  #    allowed unconditionally, we must remove this rule — ERRNO has higher priority
  #    than ALLOW in libseccomp, so an ALLOW rule alone would not override it.
  .syscalls |= [.[] | select(
    (.names == ["clone3"] and .action == "SCMP_ACT_ERRNO") | not
  )]

  # The clone arg-filter rules (blocking namespace flags) remain in the profile but
  # are effectively superseded by our unconditional ALLOW. Two ALLOW rules for the
  # same syscall do not conflict — the more permissive one takes effect.
  #
  # The CAP_SYS_ADMIN conditional rule also stays unchanged. Docker evaluates
  # includes/excludes at container creation time; since we do not grant CAP_SYS_ADMIN,
  # that rule is simply not compiled into the BPF filter.
' > "${OUTPUT}"

echo "Generated: ${OUTPUT}"
echo ""
echo "Changes from Docker default (${DOCKER_TAG}):"
echo "  + clone      — unconditional (was: allowed only without namespace flags)"
echo "  + clone3     — unconditional (was: ENOSYS without CAP_SYS_ADMIN)"
echo "  + unshare    — unconditional (was: requires CAP_SYS_ADMIN)"
echo "  + setns      — unconditional (was: requires CAP_SYS_ADMIN)"
echo "  + mount      — unconditional (was: requires CAP_SYS_ADMIN)"
echo "  + umount2    — unconditional (was: requires CAP_SYS_ADMIN)"
echo "  + pivot_root — unconditional (was: blocked entirely)"

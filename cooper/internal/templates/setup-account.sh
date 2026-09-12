#!/bin/sh
# Create the host account while the image is built. Preserve system groups by
# number when their names conflict with a host group, such as macOS staff.
set -eu
account_name=$1
account_group=$2
account_uid=$3
account_gid=$4
account_home=$5
test -n "$account_name"
test -n "$account_group"
test -n "$account_home"

named_group=$(getent group "$account_group" || true)
if [ -n "$named_group" ]; then
    old_gid=$(printf '%s\n' "$named_group" | cut -d: -f3)
    if [ "$old_gid" != "$account_gid" ]; then
        groupmod --new-name "cooper-image-$old_gid" "$account_group"
    fi
fi
numbered_group=$(getent group "$account_gid" || true)
if [ -n "$numbered_group" ]; then
    old_name=$(printf '%s\n' "$numbered_group" | cut -d: -f1)
    if [ "$old_name" != "$account_group" ]; then
        groupmod --new-name "$account_group" "$old_name"
    fi
else
    groupadd --gid "$account_gid" "$account_group"
fi

# A conflicting image account is not safe to replace silently. UID ownership
# includes files outside the home, so report the conflict at build time.
if getent passwd "$account_name" >/dev/null || getent passwd "$account_uid" >/dev/null; then
    echo "Host account $account_name ($account_uid) conflicts with an image account." >&2
    exit 1
fi
useradd --uid "$account_uid" --gid "$account_gid" --home-dir "$account_home" \
    --create-home --shell /bin/bash "$account_name"

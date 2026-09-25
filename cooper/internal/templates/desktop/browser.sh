#!/bin/sh
set -eu
# OAuth callbacks and downloaded files stay in the guest. This browser has no
# access to the host browser profile or its cookies.
exec chromium --user-data-dir=/var/lib/cooper/desktop/browser \
    --disable-setuid-sandbox \
    --proxy-server="${HTTPS_PROXY:?Cooper proxy is required}" \
    --no-first-run --no-default-browser-check "$@"

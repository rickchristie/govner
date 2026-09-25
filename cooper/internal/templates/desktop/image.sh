#!/bin/bash
set -eu
# The private bridge keeps the normal host image path available after guest
# apps or the viewer have taken the text clipboard. No host socket is shared.
read -r bridge_pid </var/lib/cooper/desktop/clipboard.pid
kill -USR1 "$bridge_pid"

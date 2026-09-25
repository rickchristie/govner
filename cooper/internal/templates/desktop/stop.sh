#!/bin/bash
set -eu
pid_file=/var/lib/cooper/desktop/session.pid
if [ -r "$pid_file" ]; then
    read -r session_pid <"$pid_file"
    kill -TERM "$session_pid" 2>/dev/null || true
    for attempt in {1..50}; do
        if ! kill -0 "$session_pid" 2>/dev/null; then exit 0; fi
        sleep 0.1
    done
fi

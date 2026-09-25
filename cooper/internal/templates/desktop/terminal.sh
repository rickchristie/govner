#!/bin/bash
set -eu
cd -- "${COOPER_DESKTOP_WORKSPACE:?Desktop workspace is required}"
exec xterm -u8 -fa 'DejaVu Sans Mono' -fs 11 -geometry 100x30+30+30

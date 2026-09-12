#!/bin/sh
# No arguments run local checks. Preparation and VM starts require a mode.
set -eu
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec python3 "$script_dir/dev/vm_test.py" "$@"

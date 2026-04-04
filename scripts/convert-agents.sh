#!/bin/bash
# Convert every CLAUDE.md in the repo into a sibling AGENTS.md file.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

mapfile -d '' CLAUDE_FILES < <(find "$PROJECT_ROOT" -type f -name 'CLAUDE.md' -print0 | sort -z)

if [ "${#CLAUDE_FILES[@]}" -eq 0 ]; then
    echo "No CLAUDE.md files found under $PROJECT_ROOT"
    exit 0
fi

converted_count=0

for src in "${CLAUDE_FILES[@]}"; do
    dest="$(dirname "$src")/AGENTS.md"
    cp "$src" "$dest"

    rel_src="${src#$PROJECT_ROOT/}"
    rel_dest="${dest#$PROJECT_ROOT/}"
    if [ "$src" = "$PROJECT_ROOT/CLAUDE.md" ]; then
        rel_src="CLAUDE.md"
        rel_dest="AGENTS.md"
    fi

    echo "Converted $rel_src -> $rel_dest"
    converted_count=$((converted_count + 1))
done

echo "Converted $converted_count file(s)."

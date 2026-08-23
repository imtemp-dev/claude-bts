#!/bin/bash
# jig statusline — renders status for Claude Code's status bar
temp_file=$(mktemp)
trap 'rm -f "$temp_file"' EXIT
cat > "$temp_file"

if command -v jig &> /dev/null; then
  jig statusline < "$temp_file"
  exit 0
fi

# Fallback: try local binary
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOCAL_BIN="${SCRIPT_DIR}/../bin/jig"
if [ -f "$LOCAL_BIN" ]; then
  "$LOCAL_BIN" statusline < "$temp_file"
  exit 0
fi

echo "jig"

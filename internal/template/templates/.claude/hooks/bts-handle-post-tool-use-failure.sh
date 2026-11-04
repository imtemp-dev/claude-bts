#!/bin/bash
# BTS post-tool-use-failure hook — forwards to bts binary (a tool call that failed)
temp_file=$(mktemp)
trap 'rm -f "$temp_file"' EXIT
cat > "$temp_file"

if command -v bts &> /dev/null; then
  bts hook post-tool-use-failure < "$temp_file"
  exit 0
fi

# Try local binary
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOCAL_BIN="${SCRIPT_DIR}/../../bin/bts"
if [ -f "$LOCAL_BIN" ]; then
  "$LOCAL_BIN" hook post-tool-use-failure < "$temp_file"
  exit 0
fi

exit 0

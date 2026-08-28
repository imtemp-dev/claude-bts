#!/bin/bash
# jig subagent-stop hook — clears agent indicator from statusline
temp_file=$(mktemp)
trap 'rm -f "$temp_file"' EXIT
cat > "$temp_file"

if command -v jig &> /dev/null; then
  jig hook subagent-stop < "$temp_file"
  exit 0
fi

# Try local binary
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOCAL_BIN="${SCRIPT_DIR}/../../bin/jig"
if [ -f "$LOCAL_BIN" ]; then
  "$LOCAL_BIN" hook subagent-stop < "$temp_file"
  exit 0
fi

exit 0

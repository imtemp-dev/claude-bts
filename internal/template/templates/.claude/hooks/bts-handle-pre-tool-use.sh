#!/bin/bash
# BTS pre-tool-use hook — forwards to bts binary
temp_file=$(mktemp)
trap 'rm -f "$temp_file"' EXIT
cat > "$temp_file"

if command -v bts &> /dev/null; then
  exec bts hook pre-tool-use < "$temp_file"
fi

# Try local binary
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOCAL_BIN="${SCRIPT_DIR}/../../../../bin/bts"
if [ -f "$LOCAL_BIN" ]; then
  exec "$LOCAL_BIN" hook pre-tool-use < "$temp_file"
fi

exit 0

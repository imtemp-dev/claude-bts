#!/bin/bash
# Forge post-tool-use hook — forwards to forge binary
temp_file=$(mktemp)
trap 'rm -f "$temp_file"' EXIT
cat > "$temp_file"

if command -v forge &> /dev/null; then
  exec forge hook post-tool-use < "$temp_file"
fi

# Try local binary
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOCAL_BIN="${SCRIPT_DIR}/../../../../bin/forge"
if [ -f "$LOCAL_BIN" ]; then
  exec "$LOCAL_BIN" hook post-tool-use < "$temp_file"
fi

exit 0

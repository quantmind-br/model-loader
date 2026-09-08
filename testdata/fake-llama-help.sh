#!/usr/bin/env bash
# Test fake of llama-server: emits canned --help / --version output for
# llamahelp.ExecParser tests. Real binary is replaced via PATH override.

# Resolve symlink to get the real script path
SCRIPT_DIR="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"

case "$1" in
  --help|--usage)
    cat "$SCRIPT_DIR/help-v10867.txt"
    exit 0
    ;;
  --version)
    echo "version: 10867 (f3f1a8f27)"
    exit 0
    ;;
  *)
    echo "fake-llama-server: unknown args: $*" 1>&2
    exit 2
    ;;
esac

#!/usr/bin/env bash
# Tear down a tui-validator session: try a clean quit first, then kill.
#
# Usage:
#   tui-cleanup.sh <session>
#   tui-cleanup.sh --all           # kill every tuiv-* session

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=_common.sh
. "$HERE/_common.sh"

if [[ "${1:-}" == "--all" ]]; then
  for s in $(tmux list-sessions -F '#S' 2>/dev/null | grep '^tuiv-' || true); do
    tmux kill-session -t "$s" 2>/dev/null || true
    echo "killed $s"
  done
  exit 0
fi

session="${1:?usage: tui-cleanup.sh SESSION | --all}"
if ! tmux has-session -t "$session" 2>/dev/null; then
  echo "no session: $session" >&2
  exit 0
fi

# Polite quit: send Escape first to dismiss any open modal, so a following 'q'
# quits the app itself rather than just closing the modal. Then kill if alive.
for k in 'Escape' 'q' 'C-c' 'C-d'; do
  tmux send-keys -t "$session" "$k" 2>/dev/null || true
  sleep 0.15
  pane_is_dead "$session" && break
done

tmux kill-session -t "$session" 2>/dev/null || true
echo "cleaned $session"

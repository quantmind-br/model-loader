#!/usr/bin/env bash
# Resize the tmux pane the TUI runs in to a target geometry.
# Triggers SIGWINCH so the TUI redraws.
#
# Usage:
#   tui-resize.sh <session> <cols> <rows>

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=_common.sh
. "$HERE/_common.sh"

session="${1:?usage: tui-resize.sh SESSION COLS ROWS}"
cols="${2:?cols}"
rows="${3:?rows}"

require_session "$session"

# Resize the window directly. Requires window-size manual (set in tui-launch.sh).
# Fall back to refresh-client -C if resize-window is rejected (older tmux).
tmux resize-window -t "$session" -x "$cols" -y "$rows" 2>/dev/null \
  || tmux refresh-client -t "$session" -C "${cols}x${rows}" 2>/dev/null \
  || die "could not resize session $session (tmux version too old?)"

# Some Bubble Tea apps debounce ~150ms. Wait a bit longer than that, then poll.
sleep 0.25
wait_for_redraw "$session" 8 100

# Sanity-check the new geometry; fail loudly if the resize didn't take.
got="$(tmux display-message -t "$session" -p '#{pane_width}x#{pane_height}')"
want="${cols}x${rows}"
if [[ "$got" != "$want" ]]; then
  die "resize did not take: wanted $want, got $got (window-size manual? client attached?)"
fi
echo "$got"

#!/usr/bin/env bash
# Capture the current TUI state: plain text + ANSI scrape + cursor info.
# Writes <workspace>/captures/<NNNN>-<label>.{txt,ansi,json}
# Prints the chosen NNNN sequence number on stdout.
#
# Usage:
#   tui-capture.sh <session> <label>
#   tui-capture.sh <session> <label> --html   # also emit .html via aha

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=_common.sh
. "$HERE/_common.sh"

session="${1:-}"
label="${2:-snapshot}"
emit_html=0
[[ "${3:-}" == "--html" ]] && emit_html=1

require_session "$session"
workspace="$(workspace_for "$session")"
captures="$workspace/captures"
ensure_dir "$captures"

# Find next sequence number.
last="$(
  find "$captures" -maxdepth 1 -type f -printf '%f\n' 2>/dev/null \
    | sed -n 's/^\([0-9]\{4\}\)-.*/\1/p' \
    | sort -n \
    | tail -1
)"
next="$(printf '%04d' $(( 10#${last:-0} + 1 )))"
slug="$(echo "$label" | tr -cs 'A-Za-z0-9' '-' | sed 's/^-//;s/-$//')"
base="$captures/${next}-${slug}"

tmux capture-pane -t "$session" -p > "${base}.txt"
tmux capture-pane -t "$session" -p -e > "${base}.ansi"

if (( emit_html )) && command -v aha >/dev/null 2>&1; then
  aha --black --no-header < "${base}.ansi" > "${base}.html"
fi

# Cursor + pane geometry metadata.
read -r cx cy cols rows alt dead <<< "$(
  tmux display-message -t "$session" -p \
    '#{cursor_x} #{cursor_y} #{pane_width} #{pane_height} #{alternate_on} #{pane_dead}'
)"

jq -n \
  --arg label "$label" \
  --arg seq "$next" \
  --argjson cursor "{\"x\":$cx,\"y\":$cy}" \
  --argjson geom "{\"cols\":$cols,\"rows\":$rows}" \
  --argjson alt "$alt" \
  --argjson dead "$dead" \
  '{seq:$seq, label:$label, cursor:$cursor, geom:$geom, alternate_screen: ($alt==1), dead:($dead==1)}' \
  > "${base}.json"

echo "$next"

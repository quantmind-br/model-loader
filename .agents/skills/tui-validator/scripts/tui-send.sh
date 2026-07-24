#!/usr/bin/env bash
# Send keystrokes (or literal text) to the TUI's tmux pane.
#
# Usage:
#   tui-send.sh <session> <key-or-chord> [<key-or-chord>...]
#       Sends each argument as a tmux key name. tmux understands:
#       'Up' 'Down' 'Left' 'Right' 'PgUp' 'PgDn' 'Home' 'End' 'Tab' 'BSpace'
#       'Enter' 'Escape' 'Space' 'F1'..'F12' 'IC' 'DC'
#       Chords like 'C-c', 'M-x', 'C-M-Up' also work. Human spellings such as
#       'Ctrl-c', 'Alt-x', 'Esc' and 'Shift-Tab' are normalized before sending.
#
#   tui-send.sh <session> --literal "ção não 中 €"
#       Sends the text character-by-character via `tmux send-keys -l`. Each
#       grapheme arrives as a separate key event — this is the right mode for
#       FILTER INPUTS, SEARCH BOXES and any widget that doesn't honour
#       bracketed paste. Slow-types by default (no inter-key delay).
#
#   tui-send.sh <session> --literal --delay 30 "ção"
#       Same as above but with a 30 ms delay between characters. Use this when
#       the TUI debounces input (some Bubble Tea apps do) or when key-roll
#       reproductions matter.
#
#   tui-send.sh <session> --paste "block of text"
#       Sends the whole block atomically via tmux's paste-buffer. Good for
#       TEXT AREAS that handle paste cleanly but bad for filter inputs that
#       only react to key events.
#
#   tui-send.sh <session> --paste-bracketed "multi-line block"
#       Wraps the paste in \e[200~ ... \e[201~ so the TUI sees a bracketed
#       paste. Use this specifically to test bracketed-paste handling.
#
# After sending, waits for the redraw to settle. Override with TUIV_REDRAW_MS.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=_common.sh
. "$HERE/_common.sh"

session="${1:-}"; shift || true
require_session "$session"

mode="${1:-}"
case "$mode" in
  --literal)
    shift
    delay_ms=0
    if [[ "${1:-}" == "--delay" ]]; then
      delay_ms="${2:?--delay needs a number}"
      shift 2
    fi
    text="$*"
    if [[ "$delay_ms" -eq 0 ]]; then
      # Single send-keys -l call: chars arrive as a burst of key events.
      tmux send-keys -t "$session" -l -- "$text"
    else
      # Walk grapheme-by-grapheme with sleep. Python is the cleanest way to
      # split UTF-8 correctly across all platforms we care about.
      python3 - "$session" "$delay_ms" "$text" <<'PY'
import os, sys, subprocess, time
session, delay_ms, text = sys.argv[1], int(sys.argv[2]), sys.argv[3]
for ch in text:
    subprocess.run(["tmux", "send-keys", "-t", session, "-l", "--", ch], check=True)
    if delay_ms > 0:
        time.sleep(delay_ms / 1000)
PY
    fi
    ;;
  --paste)
    shift
    text="$*"
    bufname="tuiv-paste-$$"
    printf '%s' "$text" | tmux load-buffer -b "$bufname" -
    tmux paste-buffer -b "$bufname" -t "$session" -d
    ;;
  --paste-bracketed)
    shift
    text="$*"
    bufname="tuiv-paste-$$"
    {
      printf '\033[200~'
      printf '%s' "$text"
      printf '\033[201~'
    } | tmux load-buffer -b "$bufname" -
    tmux paste-buffer -b "$bufname" -t "$session" -d
    ;;
  "")
    die "usage: tui-send.sh <session> <key>... | --literal [--delay MS] TEXT | --paste TEXT | --paste-bracketed TEXT"
    ;;
  *)
    # Send each remaining arg as a key chord.
    keys=()
    for key in "$@"; do
      keys+=("$(canonical_key "$key")")
    done
    tmux send-keys -t "$session" "${keys[@]}"
    ;;
esac

# Wait briefly for redraw before returning.
sleep_ms="${TUIV_REDRAW_MS:-200}"
sleep "$(awk "BEGIN{print $sleep_ms/1000}")"
wait_for_redraw "$session" 4 80

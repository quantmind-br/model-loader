#!/usr/bin/env bash
# Launch a TUI inside a dedicated tmux session sized to a known geometry.
# Prints the session name on stdout. Writes meta.json into the workspace.
#
# Usage:
#   tui-launch.sh <tui-binary> [-- <tui-args>...]
#   tui-launch.sh --cwd <dir> <tui-binary> [-- <tui-args>...]
#
# Optional env:
#   TUIV_COLS    initial pane width  (default 80)
#   TUIV_ROWS    initial pane height (default 24)
#   TUIV_TERM    TERM env for the TUI process (default xterm-256color)

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=_common.sh
. "$HERE/_common.sh"

CWD=""
if [[ "${1:-}" == "--cwd" ]]; then
  CWD="$2"
  shift 2
fi

BIN="${1:-}"
[[ -n "$BIN" ]] || die "usage: tui-launch.sh [--cwd DIR] <tui-binary> [-- <args>...]"
shift
if [[ "${1:-}" == "--" ]]; then shift; fi

if [[ -n "$CWD" && ! -d "$CWD" ]]; then
  die "cwd does not exist: $CWD"
fi

# Resolve enough to validate the executable while preserving relative paths
# when the user intentionally launches from --cwd.
EXEC_BIN="$BIN"
CHECK_BIN="$BIN"
if [[ "$BIN" != */* ]]; then
  CHECK_BIN="$(command -v "$BIN" || true)"
  [[ -n "$CHECK_BIN" ]] || die "command not found: $BIN"
  EXEC_BIN="$CHECK_BIN"
elif [[ "$BIN" != /* ]]; then
  if [[ -n "$CWD" ]]; then
    CHECK_BIN="$CWD/$BIN"
  else
    CHECK_BIN="$PWD/$BIN"
  fi
fi
[[ -x "$CHECK_BIN" ]] || die "not executable: $BIN"

# Best-effort: try to wire up Wayland for the eventual screenshot phase.
wayland_autodetect >/dev/null 2>&1 || true

COLS="${TUIV_COLS:-80}"
ROWS="${TUIV_ROWS:-24}"
TERM_ENV="${TUIV_TERM:-xterm-256color}"

slug="$(slugify_path "$BIN")"
ts="$(ts_now)"
session="tuiv-${slug}-${ts}"
workspace="$TUI_VALIDATOR_HOME/$slug/$ts"

ensure_dir "$workspace/captures"
ensure_dir "$workspace/screenshots"

# Build the command. Quote args safely.
cmd="$(printf '%q ' "$EXEC_BIN" "$@")"
if [[ -n "$CWD" ]]; then
  cmd="cd $(printf '%q' "$CWD") && exec $cmd"
else
  cmd="exec $cmd"
fi

# Start detached. Force the geometry.
TERM="$TERM_ENV" tmux new-session -d -s "$session" -x "$COLS" -y "$ROWS" "$cmd"

# Disable status line so capture-pane row 0 is the TUI's row 0.
tmux set-option -t "$session" status off >/dev/null

# Detach geometry from attached-client size so resize-window works without a client.
tmux set-option -t "$session" window-size manual >/dev/null
tmux set-option -t "$session" aggressive-resize on >/dev/null
tmux resize-window -t "$session" -x "$COLS" -y "$ROWS" >/dev/null 2>&1 || true

# Brief wait for the TUI to draw its initial frame.
wait_for_redraw "$session" 12 150

# Sanity check: did it crash on startup?
if pane_is_dead "$session"; then
  tmux capture-pane -t "$session" -p > "$workspace/captures/0000-startup-crash.txt" || true
  tmux kill-session -t "$session" 2>/dev/null || true
  {
    echo "TUI exited on startup."
    echo "Binary:    $BIN"
    echo "CWD:       ${CWD:-$PWD}"
    echo "TERM:      $TERM_ENV  geom=${COLS}x${ROWS}"
    echo "Captured pane:"
    sed 's/^/  /' "$workspace/captures/0000-startup-crash.txt" 2>/dev/null
  } >&2
  die "TUI did not survive startup; see $workspace/captures/0000-startup-crash.txt"
fi

# Build args JSON cleanly — empty list when no extra args were passed.
if (( $# > 0 )); then
  args_json="$(printf '%s\n' "$@" | jq -R . | jq -s .)"
else
  args_json='[]'
fi

version=""
if command -v timeout >/dev/null 2>&1; then
  if [[ -n "$CWD" ]]; then
    version="$(cd "$CWD" && timeout 2s "$EXEC_BIN" --version 2>/dev/null | head -1 || true)"
  else
    version="$(timeout 2s "$EXEC_BIN" --version 2>/dev/null | head -1 || true)"
  fi
fi

# meta.json
jq -n \
  --arg session "$session" \
  --arg bin "$BIN" \
  --arg resolved_bin "$CHECK_BIN" \
  --arg cwd "${CWD:-$PWD}" \
  --arg cols "$COLS" \
  --arg rows "$ROWS" \
  --arg term "$TERM_ENV" \
  --arg ts "$ts" \
  --arg workspace "$workspace" \
  --arg version "$version" \
  --argjson args "$args_json" \
  '{
    session: $session,
    binary: $bin,
    resolved_binary: $resolved_bin,
    args: $args,
    cwd: $cwd,
    version: (if $version == "" then null else $version end),
    initial: {cols: ($cols|tonumber), rows: ($rows|tonumber)},
    term: $term,
    started_at: $ts,
    workspace: $workspace
  }' > "$workspace/meta.json"

echo "$session"

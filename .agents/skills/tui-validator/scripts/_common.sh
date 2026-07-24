#!/usr/bin/env bash
# Shared helpers for tui-validator scripts.
# Sourced, not executed directly.

set -euo pipefail

# Default workspace root. Override with TUI_VALIDATOR_HOME.
: "${TUI_VALIDATOR_HOME:=$HOME/.cache/tui-validator}"

# Resolve workspace for a given session name.
# Convention: session name "tuiv-<slug>-<timestamp>" → workspace path uses the same slug+timestamp.
workspace_for() {
  local session="$1"
  local stripped="${session#tuiv-}"
  local slug="${stripped%-*}"
  local ts="${stripped##*-}"
  echo "$TUI_VALIDATOR_HOME/$slug/$ts"
}

ensure_dir() {
  mkdir -p "$1"
}

count_files() {
  local dir="$1"
  local pattern="$2"
  if [[ ! -d "$dir" ]]; then
    echo 0
    return
  fi
  find "$dir" -maxdepth 1 -type f -name "$pattern" | wc -l | tr -d '[:space:]'
}

# Stable slug from a binary path: basename, lowercased, non-alnum → '-'.
slugify_path() {
  local path="$1"
  local base
  base="$(basename "$path")"
  echo "$base" | tr '[:upper:]' '[:lower:]' | tr -cs 'a-z0-9' '-' | sed 's/^-//;s/-$//'
}

ts_now() {
  date -u +%Y%m%dT%H%M%SZ
}

die() {
  echo "tui-validator: $*" >&2
  exit 1
}

warn() {
  echo "tui-validator: warn: $*" >&2
}

require_session() {
  local s="$1"
  tmux has-session -t "$s" 2>/dev/null || die "no tmux session: $s"
}

# Wait for the pane to stop changing (poor-man's redraw detector).
# Polls capture-pane checksums up to N times with M ms sleeps.
wait_for_redraw() {
  local session="$1"
  local max_polls="${2:-6}"
  local sleep_ms="${3:-100}"
  local prev="" cur=""
  for ((i = 0; i < max_polls; i++)); do
    cur="$(tmux capture-pane -t "$session" -p 2>/dev/null | md5sum | cut -d' ' -f1)"
    if [[ "$cur" == "$prev" && -n "$cur" ]]; then
      return 0
    fi
    prev="$cur"
    sleep "$(awk "BEGIN{print $sleep_ms/1000}")"
  done
  return 0  # don't fail the audit just because the TUI is still animating
}

# Detect if a tmux pane's process has died.
pane_is_dead() {
  local session="$1"
  local status
  status="$(tmux list-panes -t "$session" -F '#{pane_dead}' 2>/dev/null | head -1)"
  [[ "$status" == "1" ]]
}

# Normalize common human-readable key names into tmux send-keys tokens.
canonical_key() {
  local key="$1"

  case "$key" in
    Ctrl-*) key="C-${key#Ctrl-}" ;;
    CTRL-*) key="C-${key#CTRL-}" ;;
    ctrl-*) key="C-${key#ctrl-}" ;;
    Alt-*) key="M-${key#Alt-}" ;;
    ALT-*) key="M-${key#ALT-}" ;;
    alt-*) key="M-${key#alt-}" ;;
  esac

  case "$key" in
    Esc|esc) echo "Escape" ;;
    Return|return) echo "Enter" ;;
    Del|del) echo "Delete" ;;
    BackTab|backtab|Shift-Tab|shift-tab|S-Tab) echo "BTab" ;;
    PgUp|PageUp|page-up|Page-Up) echo "PgUp" ;;
    PgDn|PageDown|page-down|Page-Down) echo "PgDn" ;;
    Spacebar|spacebar) echo "Space" ;;
    *) echo "$key" ;;
  esac
}

# Auto-detect the Wayland socket from /run/user/$UID and set WAYLAND_DISPLAY
# if it isn't already in the environment. Returns 0 if a usable socket was
# located, 1 otherwise. Safe to call repeatedly.
wayland_autodetect() {
  if [[ -n "${WAYLAND_DISPLAY:-}" ]]; then return 0; fi
  local uid="${UID:-$(id -u)}"
  local dir="/run/user/$uid"
  [[ -d "$dir" ]] || return 1
  local sock
  for sock in "$dir"/wayland-*; do
    # Skip the .lock files; the real socket has no extension.
    [[ "$sock" == *.lock ]] && continue
    [[ -S "$sock" ]] || continue
    WAYLAND_DISPLAY="$(basename "$sock")"
    export WAYLAND_DISPLAY
    export XDG_RUNTIME_DIR="$dir"
    return 0
  done
  return 1
}

# Same for Hyprland's instance signature, looked up from /tmp/hypr.
hyprland_autodetect() {
  if [[ -n "${HYPRLAND_INSTANCE_SIGNATURE:-}" ]]; then return 0; fi
  local uid="${UID:-$(id -u)}"
  local d
  for d in "/run/user/$uid/hypr"/*; do
    [[ -d "$d" ]] || continue
    HYPRLAND_INSTANCE_SIGNATURE="$(basename "$d")"
    export HYPRLAND_INSTANCE_SIGNATURE
    return 0
  done
  return 1
}

on_wayland() {
  wayland_autodetect >/dev/null 2>&1
  [[ -n "${WAYLAND_DISPLAY:-}" ]]
}

on_hyprland() {
  hyprland_autodetect >/dev/null 2>&1
  [[ -n "${HYPRLAND_INSTANCE_SIGNATURE:-}" ]]
}

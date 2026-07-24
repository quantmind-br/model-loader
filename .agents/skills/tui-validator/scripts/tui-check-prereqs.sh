#!/usr/bin/env bash
# Check tui-validator runtime dependencies.
#
# Exits non-zero only when a required dependency is missing. Optional tools are
# reported so the audit can document downgraded phases instead of aborting.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=_common.sh
. "$HERE/_common.sh"

missing_required=0

check_required() {
  local tool="$1"
  if command -v "$tool" >/dev/null 2>&1; then
    echo "OK required: $tool"
  else
    echo "MISSING required: $tool"
    missing_required=1
  fi
}

check_optional() {
  local tool="$1"
  if command -v "$tool" >/dev/null 2>&1; then
    echo "OK optional: $tool"
  else
    echo "MISSING optional: $tool"
  fi
}

check_required tmux
check_required jq

for tool in python3 aha grim slurp magick compare hyprctl foot kitty alacritty ghostty wezterm chromium chromium-browser google-chrome google-chrome-stable; do
  check_optional "$tool"
done

has_wayland_terminal() {
  command -v foot >/dev/null 2>&1 \
    || command -v kitty >/dev/null 2>&1 \
    || command -v alacritty >/dev/null 2>&1 \
    || command -v ghostty >/dev/null 2>&1 \
    || command -v wezterm >/dev/null 2>&1
}

has_chromium() {
  command -v chromium >/dev/null 2>&1 \
    || command -v chromium-browser >/dev/null 2>&1 \
    || command -v google-chrome >/dev/null 2>&1 \
    || command -v google-chrome-stable >/dev/null 2>&1
}

if command -v grim >/dev/null 2>&1 && on_wayland && has_wayland_terminal; then
  echo "OK screenshot backend: Wayland + grim"
elif command -v aha >/dev/null 2>&1 && has_chromium; then
  echo "OK screenshot backend: aha + headless Chromium"
else
  echo "MISSING screenshot backend: install Wayland+grim or aha+Chromium for PNG output"
fi

exit "$missing_required"

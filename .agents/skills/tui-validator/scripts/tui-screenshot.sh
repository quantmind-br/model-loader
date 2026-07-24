#!/usr/bin/env bash
# Take a pixel screenshot of the TUI as it would render in a real terminal.
#
# Strategy:
#   1. Prefer a live Wayland screenshot: spawn a temporary terminal attached
#      read-only to the tmux session, locate its geometry, and run grim.
#   2. If that cannot run, capture the current ANSI pane and render it through
#      aha + a headless Chromium-compatible browser.
#   3. Optionally diff the screenshot against a baseline label with ImageMagick.
#
# Usage:
#   tui-screenshot.sh <session> <label>
#   tui-screenshot.sh <session> <label> --diff [baseline-label]

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=_common.sh
. "$HERE/_common.sh"

session="${1:?usage: tui-screenshot.sh SESSION LABEL [--diff [baseline-label]]}"
label="${2:?label}"
shift 2

make_diff=0
baseline_label="default"
while (($#)); do
  case "$1" in
    --diff)
      make_diff=1
      shift
      if [[ "${1:-}" != "" && "${1:-}" != --* ]]; then
        baseline_label="$1"
        shift
      fi
      ;;
    *)
      die "unknown option: $1"
      ;;
  esac
done

require_session "$session"
workspace="$(workspace_for "$session")"
shots="$workspace/screenshots"
ensure_dir "$shots"

slug="$(echo "$label" | tr -cs 'A-Za-z0-9' '-' | sed 's/^-//;s/-$//')"
out="$shots/${slug}.png"
term_pid=""
html=""

cleanup() {
  if [[ -n "$term_pid" ]]; then
    kill "$term_pid" 2>/dev/null || true
    wait "$term_pid" 2>/dev/null || true
  fi
  if [[ -n "$html" ]]; then
    rm -f "$html"
  fi
}
trap cleanup EXIT

find_chromium() {
  local candidate
  for candidate in chromium chromium-browser google-chrome google-chrome-stable; do
    if command -v "$candidate" >/dev/null 2>&1; then
      echo "$candidate"
      return 0
    fi
  done
  return 1
}

render_headless() {
  if ! command -v aha >/dev/null 2>&1; then die "no display and aha not installed"; fi
  local browser
  browser="$(find_chromium)" || die "no display and chromium/google-chrome not installed"

  # Capture the current pane now; relying on a previous .ansi file can render a
  # stale state if the caller resized or sent keys immediately before this.
  "$HERE/tui-capture.sh" "$session" "screenshot-${slug}" >/dev/null

  local last_ansi
  last_ansi="$(
    find "$workspace/captures" -maxdepth 1 -type f -name '*.ansi' -printf '%T@ %p\n' 2>/dev/null \
      | sort -nr \
      | awk 'NR==1 {sub(/^[^ ]+ /,""); print}'
  )"
  [[ -n "$last_ansi" ]] || die "no captures yet; run tui-capture.sh first"

  html="$(mktemp --suffix=.html)"
  {
    cat <<'EOF'
<!doctype html><meta charset="utf-8">
<style>
body { background:#1e1e1e; color:#ddd; margin:0; padding:8px;
       font-family: 'JetBrainsMono Nerd Font','Fira Code','Cascadia Code',monospace;
       font-size:14px; line-height:1.25; white-space:pre; }
</style>
EOF
    aha --no-header --black < "$last_ansi"
  } > "$html"

  # Size the headless viewport from the pane geometry so wide/huge captures are
  # not clipped at a fixed 1200x800. Per-char budget (10px wide, 18px tall at
  # 14px/1.25 line-height) overshoots slightly; the extra area is background.
  local cols rows win_w win_h
  read -r cols rows < <(tmux display-message -p -t "$session" '#{pane_width} #{pane_height}' 2>/dev/null || true)
  [[ "$cols" =~ ^[0-9]+$ ]] || cols=80
  [[ "$rows" =~ ^[0-9]+$ ]] || rows=24
  win_w=$(( cols * 10 + 24 ))
  win_h=$(( rows * 18 + 24 ))

  "$browser" --headless --disable-gpu --no-sandbox --hide-scrollbars \
    --window-size="${win_w},${win_h}" --screenshot="$out" "file://$html" >/dev/null 2>&1
}

image_size() {
  local image="$1"
  if command -v magick >/dev/null 2>&1; then
    magick identify -format '%w %h' "$image"
  elif command -v identify >/dev/null 2>&1; then
    identify -format '%w %h' "$image"
  else
    return 1
  fi
}

write_diff() {
  local baseline_slug baseline diff_out status
  baseline_slug="$(echo "$baseline_label" | tr -cs 'A-Za-z0-9' '-' | sed 's/^-//;s/-$//')"
  baseline="$shots/${baseline_slug}.png"
  diff_out="$shots/${slug}-vs-${baseline_slug}.png"

  [[ -f "$baseline" ]] || { warn "cannot diff; missing baseline screenshot: $baseline"; return 0; }
  [[ "$baseline" != "$out" ]] || return 0

  if command -v magick >/dev/null 2>&1; then
    local bw bh ow oh maxw maxh tmp_a tmp_b metric
    read -r bw bh <<< "$(image_size "$baseline")"
    read -r ow oh <<< "$(image_size "$out")"
    maxw="$bw"
    maxh="$bh"
    (( ow > maxw )) && maxw="$ow"
    (( oh > maxh )) && maxh="$oh"

    tmp_a="$(mktemp --suffix=.png)"
    tmp_b="$(mktemp --suffix=.png)"
    metric="$(mktemp)"
    magick "$baseline" -background black -gravity northwest -extent "${maxw}x${maxh}" "$tmp_a"
    magick "$out" -background black -gravity northwest -extent "${maxw}x${maxh}" "$tmp_b"
    status=0
    magick compare -metric AE "$tmp_a" "$tmp_b" "$diff_out" >/dev/null 2>"$metric" || status=$?
    rm -f "$tmp_a" "$tmp_b" "$metric"
    if [[ "$status" -gt 1 ]]; then
      warn "magick compare failed for $out"
      rm -f "$diff_out"
    fi
  elif command -v compare >/dev/null 2>&1; then
    status=0
    compare "$baseline" "$out" "$diff_out" >/dev/null 2>&1 || status=$?
    if [[ "$status" -gt 1 ]]; then
      warn "compare failed for $out"
      rm -f "$diff_out"
    fi
  else
    warn "cannot diff screenshots; ImageMagick is not installed"
  fi
}

# Auto-wire Wayland env so grim and the spawned terminal both find the
# compositor even when the agent shell did not inherit those variables.
wayland_autodetect >/dev/null 2>&1 || true
hyprland_autodetect >/dev/null 2>&1 || true

captured=0
if on_wayland && command -v grim >/dev/null 2>&1; then
  # ---- Live screenshot via attached terminal ----------------------------
  term=""
  for candidate in foot kitty alacritty ghostty wezterm; do
    if command -v "$candidate" >/dev/null 2>&1; then term="$candidate"; break; fi
  done

  if [[ -n "$term" ]]; then
    title="tuiv-shot-$$-$(date +%s%N)"

    # tmux attach in read-only mode so accidental host keypresses do not leak.
    case "$term" in
      foot)      "$term" --title "$title"             -e tmux attach-session -t "$session" -r & ;;
      kitty)     "$term" --title="$title"             tmux attach-session -t "$session" -r & ;;
      alacritty) "$term" --title "$title"             -e tmux attach-session -t "$session" -r & ;;
      ghostty)   "$term" --title="$title"             -e=tmux -e=attach-session -e=-t -e="$session" -e=-r & ;;
      wezterm)   "$term" start --class "$title"       tmux attach-session -t "$session" -r & ;;
    esac
    term_pid=$!

    geom=""
    for _ in {1..30}; do
      sleep 0.1
      if on_hyprland; then
        geom="$(
          hyprctl clients -j 2>/dev/null \
            | jq -r --arg t "$title" '
                .[] | select(.title==$t or .initialTitle==$t or .class==$t)
                | "\(.at[0]),\(.at[1]) \(.size[0])x\(.size[1])"
              ' \
            | head -1
        )"
      fi
      [[ -n "$geom" ]] && break
    done

    if on_hyprland && [[ -n "$geom" ]]; then
      hyprctl dispatch focuswindow "title:$title" >/dev/null 2>&1 || true
      sleep 0.1
    fi

    if [[ -n "$geom" ]]; then
      if grim -g "$geom" "$out"; then
        captured=1
      else
        warn "grim failed for focused window; using headless fallback"
      fi
    else
      # No per-window geometry (compositor is not Hyprland). A bare `grim "$out"`
      # would grab the ENTIRE desktop — leaking other windows and not the TUI.
      # Fall through to the headless pane render, which only draws this session.
      warn "no per-window geometry (compositor is not Hyprland); using headless pane render instead of a full-screen grab"
    fi
  else
    warn "no Wayland terminal found (foot/kitty/alacritty/ghostty/wezterm); using headless fallback"
  fi
fi

if (( captured == 0 )); then
  # ---- Headless fallback: ANSI to HTML to PNG via Chromium ---------------
  render_headless
fi

if (( make_diff == 1 )); then
  write_diff
fi

echo "$out"

#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'Usage: %s <codebase-root>\n' "${0##*/}" >&2
}

json_escape() {
  local value=${1-}
  value=${value//\\/\\\\}
  value=${value//\"/\\\"}
  value=${value//$'\n'/\\n}
  value=${value//$'\r'/\\r}
  value=${value//$'\t'/\\t}
  printf '%s' "$value"
}

json_array() {
  local -n values=$1
  local first=1
  local value
  printf '['
  for value in "${values[@]}"; do
    [[ -n "$value" ]] || continue
    if (( first )); then
      first=0
    else
      printf ','
    fi
    printf '\n    "%s"' "$(json_escape "$value")"
  done
  if (( first )); then
    printf ']'
  else
    printf '\n  ]'
  fi
}

add_unique_to() {
  local -n target=$1
  local item=$2
  local existing
  [[ -n "$item" ]] || return 0
  for existing in "${target[@]}"; do
    [[ "$existing" == "$item" ]] && return 0
  done
  target+=("$item")
}

search_root() {
  local pattern=$1
  if command -v rg >/dev/null 2>&1; then
    (cd "$root" && rg -q --hidden --glob '!.git/**' --glob '!node_modules/**' --glob '!target/**' --glob '!dist/**' --glob '!vendor/**' "$pattern" .)
  else
    grep -R -q -E --exclude-dir=.git --exclude-dir=node_modules --exclude-dir=target --exclude-dir=dist --exclude-dir=vendor "$pattern" "$root"
  fi
}

collect_ui_files() {
  local pattern='(View\(\)|Update\(.*tea\.Msg|key\.Binding|lipgloss|BINDINGS|compose\(|action_|on_key|ratatui|crossterm|Layout::|render_|useInput|<Box|<Text|blessed|ncurses|getch\(|mvprintw|initscr|notcurses)'
  local file

  if command -v rg >/dev/null 2>&1; then
    while IFS= read -r file; do
      file=${file#./}
      add_unique_to ui_files "$file"
    done < <(
      cd "$root" && rg -l --hidden \
        --glob '!.git/**' \
        --glob '!node_modules/**' \
        --glob '!target/**' \
        --glob '!dist/**' \
        --glob '!vendor/**' \
        "$pattern" . 2>/dev/null || true
    )
  else
    # No ripgrep: enumerate candidate source files by extension, then keep only
    # those whose CONTENT matches the UI pattern. Without this content filter the
    # fallback would add every source file in the repo as a "UI file".
    while IFS= read -r file; do
      grep -l -E "$pattern" "$file" >/dev/null 2>&1 || continue
      file=${file#"$root/"}
      add_unique_to ui_files "$file"
    done < <(
      find "$root" \
        \( -path '*/.git/*' -o -path '*/node_modules/*' -o -path '*/target/*' -o -path '*/dist/*' -o -path '*/vendor/*' \) -prune \
        -o -type f \( -name '*.go' -o -name '*.py' -o -name '*.rs' -o -name '*.js' -o -name '*.jsx' -o -name '*.ts' -o -name '*.tsx' -o -name '*.c' -o -name '*.h' -o -name '*.tcss' \) -print
    )
  fi

  while IFS= read -r file; do
    file=${file#"$root/"}
    add_unique_to ui_files "$file"
  done < <(
    find "$root" \
      \( -path '*/.git/*' -o -path '*/node_modules/*' -o -path '*/target/*' -o -path '*/dist/*' -o -path '*/vendor/*' \) -prune \
      -o -type f \( -name '*.tcss' -o -name '*view*.go' -o -name '*model*.go' -o -name '*keys*.go' -o -name 'ui.rs' -o -name 'app.rs' -o -name '*screen*.py' -o -name '*widget*.py' \) -print
  )
}

[[ $# -eq 1 ]] || { usage; exit 2; }
root=$1
if [[ ! -d "$root" ]]; then
  printf 'Codebase root does not exist or is not a directory: %s\n' "$root" >&2
  exit 1
fi
root=$(cd "$root" && pwd -P)

framework=unknown
confidence=low
# shellcheck disable=SC2034 # mutated through add_unique_to namerefs, emitted by json_array.
evidence=()
# shellcheck disable=SC2034 # mutated through add_unique_to namerefs, emitted by json_array.
ui_files=()
# shellcheck disable=SC2034 # mutated through add_unique_to namerefs, emitted by json_array.
launch_hints=()

set_framework() {
  local candidate=$1
  local level=$2
  local why=$3
  if [[ "$framework" == unknown ]]; then
    framework=$candidate
    confidence=$level
  fi
  add_unique_to evidence "$why"
}

if [[ -f "$root/go.mod" ]]; then
  if search_root 'github\.com/charmbracelet/bubbletea'; then
    set_framework 'Bubble Tea (Go)' high 'go.mod/imports include charmbracelet/bubbletea'
  elif search_root 'github\.com/(rivo/tview|gdamore/tcell)'; then
    set_framework 'tview/tcell (Go)' high 'go.mod/imports include tview or tcell'
  else
    add_unique_to evidence 'go.mod present'
  fi
fi

if [[ -f "$root/pyproject.toml" || -f "$root/requirements.txt" || -f "$root/setup.py" ]]; then
  if search_root '(^|[^A-Za-z_])textual([^A-Za-z_]|$)'; then
    set_framework 'Textual (Python)' high 'Python manifest/imports include textual'
  elif search_root '(^|[^A-Za-z_])prompt_toolkit([^A-Za-z_]|$)|(^|[^A-Za-z_])urwid([^A-Za-z_]|$)|(^|[^A-Za-z_])blessed([^A-Za-z_]|$)'; then
    set_framework 'prompt_toolkit/urwid/blessed (Python)' medium 'Python manifest/imports include prompt_toolkit, urwid, or blessed'
  elif search_root '(^|[^A-Za-z_])rich([^A-Za-z_]|$)'; then
    set_framework 'Rich-rendered CLI (Python)' medium 'Python manifest/imports include rich'
  else
    add_unique_to evidence 'Python manifest present'
  fi
fi

if [[ -f "$root/Cargo.toml" ]]; then
  if search_root 'ratatui|(^|[^A-Za-z_])tui([^A-Za-z_]|$)|crossterm'; then
    set_framework 'Ratatui (Rust)' high 'Cargo.toml/imports include ratatui/tui/crossterm'
  else
    add_unique_to evidence 'Cargo.toml present'
  fi
fi

if [[ -f "$root/package.json" ]]; then
  if search_root '"(ink|blessed|blessed-contrib)"|from ["'\''](ink|blessed|blessed-contrib)["'\'']'; then
    set_framework 'Ink/blessed (Node)' high 'package.json/imports include ink or blessed'
  else
    add_unique_to evidence 'package.json present'
  fi
fi

if search_root '#[[:space:]]*include[[:space:]]*[<"](n?curses|notcurses)'; then
  set_framework 'ncurses/Notcurses (C)' high 'C sources include ncurses/notcurses headers'
fi

collect_ui_files

if [[ -f "$root/Makefile" ]]; then
  if grep -q -E '^(run|tui|build):' "$root/Makefile"; then
    add_unique_to launch_hints 'Makefile has run/tui/build target'
  fi
fi
if [[ -f "$root/package.json" ]] && search_root '"scripts"[[:space:]]*:'; then
  add_unique_to launch_hints 'package.json has scripts section'
fi
if [[ -d "$root/target/release" ]]; then
  add_unique_to launch_hints 'target/release exists'
fi
if [[ -d "$root/bin" || -d "$root/build" ]]; then
  add_unique_to launch_hints 'bin/ or build/ directory exists'
fi

printf '{\n'
printf '  "root": "%s",\n' "$(json_escape "$root")"
printf '  "framework": "%s",\n' "$(json_escape "$framework")"
printf '  "confidence": "%s",\n' "$(json_escape "$confidence")"
printf '  "evidence": '
json_array evidence
printf ',\n'
printf '  "ui_source_files": '
json_array ui_files
printf ',\n'
printf '  "launch_hints": '
json_array launch_hints
printf '\n}\n'

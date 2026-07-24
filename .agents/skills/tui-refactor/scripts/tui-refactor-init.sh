#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'Usage: %s <codebase-root> [--name <name>] [--workspace-root <dir>]\n' "${0##*/}" >&2
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

if [[ $# -lt 1 ]]; then
  usage
  exit 2
fi

codebase_root=$1
shift
name=
workspace_root=${TUI_REFACTOR_WORKSPACE_ROOT:-"$HOME/.cache/tui-refactor"}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --name)
      [[ $# -ge 2 ]] || { usage; exit 2; }
      name=$2
      shift 2
      ;;
    --workspace-root)
      [[ $# -ge 2 ]] || { usage; exit 2; }
      workspace_root=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'Unknown argument: %s\n' "$1" >&2
      usage
      exit 2
      ;;
  esac
done

if [[ ! -d "$codebase_root" ]]; then
  printf 'Codebase root does not exist or is not a directory: %s\n' "$codebase_root" >&2
  exit 1
fi

codebase_root=$(cd "$codebase_root" && pwd -P)
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
skill_dir=$(cd "$script_dir/.." && pwd -P)

if [[ -z "$name" ]]; then
  name=$(basename "$codebase_root")
fi
name=$(printf '%s' "$name" | tr -cs 'A-Za-z0-9._-' '-')
name=${name#-}
name=${name%-}
if [[ -z "$name" ]]; then
  name=tui
fi

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
workspace="$workspace_root/$name/$timestamp"

mkdir -p \
  "$workspace/before" \
  "$workspace/03-target-design/01-screens"

copy_template() {
  local template=$1
  local target=$2
  if [[ -f "$skill_dir/templates/$template" ]]; then
    cp "$skill_dir/templates/$template" "$target"
  else
    : > "$target"
  fi
}

copy_template current-design.md "$workspace/01-current-design.md"
copy_template gap-analysis.md "$workspace/02-gap-analysis.md"
copy_template target-design.md "$workspace/03-target-design/00-overview.md"
copy_template refactor-plan.md "$workspace/04-refactor-plan.md"

printf '# Components — %s\n\nn/a — not generated yet.\n' "$name" > "$workspace/03-target-design/03-components.md"
printf '# Keybindings — %s\n\nn/a — not generated yet.\n' "$name" > "$workspace/03-target-design/04-keybindings.md"
printf '# Style guide — %s\n\nn/a — not generated yet.\n' "$name" > "$workspace/03-target-design/06-style-guide.md"
printf '# States — %s\n\nn/a — not generated yet.\n' "$name" > "$workspace/03-target-design/07-states.md"
printf '# Navigation map — %s\n\nn/a — not generated yet.\n' "$name" > "$workspace/03-target-design/08-navigation-map.md"
printf '# Accessibility — %s\n\nn/a — not generated yet.\n' "$name" > "$workspace/03-target-design/09-accessibility.md"

printf '[]\n' > "$workspace/keybindings.json"
printf '[]\n' > "$workspace/findings.json"
printf '[]\n' > "$workspace/plan-items.json"

{
  printf '{\n'
  printf '  "tui_path": "%s",\n' "$(json_escape "$codebase_root")"
  printf '  "tui_name": "%s",\n' "$(json_escape "$name")"
  printf '  "created_at_utc": "%s",\n' "$(json_escape "$timestamp")"
  printf '  "workspace": "%s",\n' "$(json_escape "$workspace")"
  printf '  "framework": "unknown",\n'
  printf '  "ui_source_files": [],\n'
  printf '  "launch": {\n'
  printf '    "command": null,\n'
  printf '    "requires_user_input": false,\n'
  printf '    "notes": []\n'
  printf '  },\n'
  printf '  "live_capture": {\n'
  printf '    "status": "not_started",\n'
  printf '    "safety_mode": "not_selected",\n'
  printf '    "before_dir": "%s"\n' "$(json_escape "$workspace/before")"
  printf '  },\n'
  printf '  "tools_used": ["tui-refactor-init.sh"],\n'
  printf '  "assumptions": [],\n'
  printf '  "open_questions": []\n'
  printf '}\n'
} > "$workspace/meta.json"

printf '%s\n' "$workspace"

#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'Usage: %s <workspace> <codebase-root> [--force]\n' "${0##*/}" >&2
}

warn() { printf '%s\n' "$*" >&2; }

# Findings-by-severity one-liner. Accepts the bare array (refactor-native) or an
# object wrapped in {"findings": [...]} (validator-native).
summarize_findings() {
  local f="$1"
  [[ -s "$f" ]] || { printf 'n/a'; return; }
  if ! command -v jq >/dev/null 2>&1; then
    printf "see \`%s\`" "$f"
    return
  fi
  jq -r '(if type=="object" then (.findings // []) else . end)
         | group_by(.severity)
         | map("\(length) \(.[0].severity // "unspecified")")
         | join(", ")
         | if . == "" then "none" else . end' "$f" 2>/dev/null \
    || printf "see \`%s\`" "$f"
}

# A file is a bare init stub if its only real content is the placeholder line
# "n/a — not generated yet." (at most a heading plus that line).
is_stub() {
  local path=$1
  [[ -s "$path" ]] || return 1
  grep -q 'not generated yet' "$path" || return 1
  [[ "$(grep -cve '^[[:space:]]*$' "$path")" -le 2 ]]
}

section_file() {
  local title=$1
  local path=$2
  printf '\n## %s\n\n' "$title"
  if [[ -s "$path" ]] && ! is_stub "$path"; then
    sed -n '1,$p' "$path"
    printf '\n'
  else
    printf 'n/a — %s was not generated.\n' "$path"
  fi
}

force=0
args=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --force) force=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) args+=("$1"); shift ;;
  esac
done
[[ ${#args[@]} -eq 2 ]] || { usage; exit 2; }
workspace=${args[0]}
codebase_root=${args[1]}

if [[ ! -d "$workspace" ]]; then
  printf 'Workspace does not exist or is not a directory: %s\n' "$workspace" >&2
  exit 1
fi
if [[ ! -d "$codebase_root" ]]; then
  printf 'Codebase root does not exist or is not a directory: %s\n' "$codebase_root" >&2
  exit 1
fi

workspace=$(cd "$workspace" && pwd -P)
codebase_root=$(cd "$codebase_root" && pwd -P)
workspace_report="$workspace/REFACTOR_PLAN.md"

# Clobber guard: if the assembled report is at least as new as every input, it
# may have been hand-edited after generation. Refuse to overwrite without --force.
if [[ $force -eq 0 && -f "$workspace_report" ]]; then
  stale=0
  for f in \
    "$workspace/meta.json" "$workspace/findings.json" "$workspace/plan-items.json" \
    "$workspace/01-current-design.md" "$workspace/02-gap-analysis.md" \
    "$workspace/04-refactor-plan.md"; do
    [[ -e "$f" && "$f" -nt "$workspace_report" ]] && { stale=1; break; }
  done
  if [[ $stale -eq 0 ]] \
     && find "$workspace/03-target-design" -type f -newer "$workspace_report" 2>/dev/null | grep -q .; then
    stale=1
  fi
  if [[ $stale -eq 0 ]]; then
    warn "Refusing to overwrite: $workspace_report is newer than its inputs (possibly hand-edited). Re-run with --force to regenerate."
    printf '%s\n' "$workspace_report"
    exit 3
  fi
fi

{
  printf '# TUI refactor plan\n\n'
  printf "> Generated from \`%s\`.\n\n" "$workspace"

  printf '## 10-second summary\n\n'
  if command -v jq >/dev/null 2>&1 && [[ -s "$workspace/meta.json" ]]; then
    tui_name=$(jq -r '.tui_name // "?"' "$workspace/meta.json" 2>/dev/null || printf '?')
    framework=$(jq -r '.framework // "unknown"' "$workspace/meta.json" 2>/dev/null || printf 'unknown')
    ambition=$(jq -r '.ambition // .clarifications.ambition // "unspecified"' "$workspace/meta.json" 2>/dev/null || printf 'unspecified')
    printf -- "- TUI: **%s**\n" "$tui_name"
    printf -- "- Framework: **%s**\n" "$framework"
    printf -- "- Ambition: **%s**\n" "$ambition"
    printf -- "- Findings by severity: %s\n" "$(summarize_findings "$workspace/findings.json")"
  elif [[ -s "$workspace/meta.json" ]]; then
    printf -- "- Metadata: \`%s\` (install \`jq\` for an inline summary)\n" "$workspace/meta.json"
    printf -- "- Findings by severity: %s\n" "$(summarize_findings "$workspace/findings.json")"
  else
    printf -- "- Metadata: n/a — \`meta.json\` was not generated.\n"
  fi
  if [[ -s "$workspace/plan-items.json" ]]; then
    printf -- "- Structured plan items: \`%s\`\n" "$workspace/plan-items.json"
  fi

  section_file 'Current design' "$workspace/01-current-design.md"
  section_file 'Gap analysis' "$workspace/02-gap-analysis.md"

  printf '\n## Target design\n\n'
  if [[ -s "$workspace/03-target-design/00-overview.md" ]]; then
    sed -n '1,$p' "$workspace/03-target-design/00-overview.md"
    printf '\n'
  else
    printf 'n/a — target overview was not generated.\n'
  fi

  printf '\n### Target design files\n\n'
  if [[ -d "$workspace/03-target-design" ]]; then
    while IFS= read -r path; do
      printf -- "- \`%s\`\n" "$path"
    done < <(find "$workspace/03-target-design" -type f | sort)
  else
    printf 'n/a — target design directory was not generated.\n'
  fi

  section_file 'Refactor plan' "$workspace/04-refactor-plan.md"

  printf '\n## Before gallery\n\n'
  if [[ -d "$workspace/before" ]] && find "$workspace/before" -type f -print -quit | grep -q .; then
    while IFS= read -r path; do
      case "$path" in
        *.png|*.PNG|*.jpg|*.jpeg|*.JPG|*.gif|*.webp)
          printf -- '![%s](%s)\n\n' "$(basename "$path")" "$path" ;;
        *)
          printf -- "- \`%s\`\n" "$path" ;;
      esac
    done < <(find "$workspace/before" -type f | sort)
  else
    printf 'n/a — no before captures were generated.\n'
  fi

  printf '\n## Next step\n\n'
  printf "To implement, execute this plan milestone by milestone. To verify afterwards, run \`tui-validator\` on the result and diff against \`before/\`.\n"
} > "$workspace_report"

canonical="$codebase_root/TUI_REFACTOR.md"
if [[ -w "$codebase_root" ]]; then
  cp "$workspace_report" "$canonical"
  printf '%s\n' "$canonical"
else
  printf 'Codebase root is not writable; kept workspace report only: %s\n' "$workspace_report" >&2
  printf '%s\n' "$workspace_report"
fi

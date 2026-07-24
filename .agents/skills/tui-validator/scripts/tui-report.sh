#!/usr/bin/env bash
# Render <workspace>/report.md from meta.json + keybindings.json + findings.json
# plus the artifacts in captures/ and screenshots/. Uses templates/report-template.md
# as a skeleton.
#
# Usage:
#   tui-report.sh <session>

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=_common.sh
. "$HERE/_common.sh"

session="${1:?usage: tui-report.sh SESSION}"
workspace="$(workspace_for "$session")"
template="$HERE/../templates/report-template.md"
out="$workspace/report.md"
final_name="TUI_AUDIT.md"

[[ -d "$workspace" ]] || die "no workspace at $workspace"
meta="$workspace/meta.json"
[[ -f "$meta" ]] || die "no meta.json in $workspace"
[[ -f "$template" ]] || die "no report template at $template"

# Empty defaults for inputs that may not exist yet.
kbinds="$workspace/keybindings.json"
[[ -f "$kbinds" ]] || echo '{"bindings":[]}' > "$kbinds"
findings="$workspace/findings.json"
[[ -f "$findings" ]] || echo '{"findings":[]}' > "$findings"

# Normalize keybindings.json into the flat {bindings:[{key,context,...}]} form.
# Accept three shapes:
#   1. {"bindings":[{...}]}              canonical
#   2. {"global":[...],"profiles":[...]} grouped by context
#   3. [{...},{...}]                     bare array
canon="$workspace/.keybindings.canonical.json"
jq '
  if type=="array" then
    {bindings: map(. + {context: (.context // "global")})}
  elif (type=="object") and has("bindings") then
    {bindings: (.bindings // [])}
  elif (type=="object") then
    {bindings: (to_entries | map(.key as $ctx | .value | map(. + {context: $ctx})) | add // [])}
  else {bindings: []} end
' "$kbinds" > "$canon"
kbinds="$canon"

# ---- pull values from meta -------------------------------------------------
tui_bin=$(jq -r '.binary' "$meta")
tui_args=$(jq -r '.args | join(" ")' "$meta")
tui_cwd=$(jq -r '.cwd' "$meta")
tui_version=$(jq -r '.version // "unknown"' "$meta")
ts=$(jq -r '.started_at' "$meta")
term=$(jq -r '.term' "$meta")
cols=$(jq -r '.initial.cols' "$meta")
rows=$(jq -r '.initial.rows' "$meta")

# ---- counts ----------------------------------------------------------------
n_caps=$(count_files "$workspace/captures" '*.txt')
n_shots=$(count_files "$workspace/screenshots" '*.png')
n_binds=$(jq '.bindings | length' "$kbinds")
n_block=$(jq '[.findings[] | select(.severity=="blocker")] | length' "$findings")
n_major=$(jq '[.findings[] | select(.severity=="major")]   | length' "$findings")
n_minor=$(jq '[.findings[] | select(.severity=="minor")]   | length' "$findings")
n_cos=$(  jq '[.findings[] | select(.severity=="cosmetic")] | length' "$findings")
n_info=$( jq '[.findings[] | select(.severity=="info")]     | length' "$findings")

# ---- render the bindings table --------------------------------------------
bindings_md=$(
  jq -r '
    def cell:
      tostring
      | gsub("\\|"; "\\|")
      | gsub("\n"; "<br>");
    .bindings
    | if length == 0 then "_(no bindings inventoried)_"
      else
        (["| Key | Context | Description | Source | Status |",
          "| --- | --- | --- | --- | --- |"] +
         (map("| `\(.key | cell)` | \(.context // "global" | cell) | \(.description // "" | cell) | \(.source // "?" | cell) | \(.status // "" | cell) |")))
        | join("\n")
      end
  ' "$kbinds"
)

# ---- render the findings sections -----------------------------------------
findings_md=$(
  jq -r '
    def severity_rank:
      {"blocker":0,"major":1,"minor":2,"cosmetic":3,"info":4}[.severity] // 99;
    .findings
    | sort_by(severity_rank)
    | if length == 0 then "_(no findings)_"
      else
        map(
          "### [\(.severity // "info" | ascii_upcase)] \(.title // "Untitled finding")\n\n" +
          "**Phase:** \(.phase // "?")  \n" +
          "**Evidence:** \(.evidence // "n/a")  \n\n" +
          "\(.description // "")\n\n" +
          (if .suggestion then "**Suggested fix:** \(.suggestion)\n\n" else "" end) +
          (if .repro then
             "**Repro:**\n" + (.repro | to_entries | map("\(.key + 1). \(.value)") | join("\n")) + "\n"
           else "" end)
        ) | join("\n---\n\n")
      end
  ' "$findings"
)

# ---- render the screenshot gallery ----------------------------------------
gallery_md=""
if [[ -d "$workspace/screenshots" ]]; then
  while IFS= read -r png; do
    [[ -e "$png" ]] || continue
    name=$(basename "$png" .png)
    gallery_md+="#### $name"$'\n\n'"![${name}](${png})"$'\n\n'
  done < <(find "$workspace/screenshots" -maxdepth 1 -type f -name '*.png' | sort)
fi
[[ -z "$gallery_md" ]] && gallery_md="_(no screenshots captured)_"

render_template() {
  local line
  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" == *"{{ KEYBINDINGS_TABLE }}"* ]]; then
      printf '%s\n' "$bindings_md"
      continue
    fi
    if [[ "$line" == *"{{ FINDINGS_SECTIONS }}"* ]]; then
      printf '%s\n' "$findings_md"
      continue
    fi
    if [[ "$line" == *"{{ VISUAL_GALLERY }}"* ]]; then
      printf '%s\n' "$gallery_md"
      continue
    fi

    line="${line//'{{ TUI_BIN }}'/$tui_bin}"
    line="${line//'{{ TUI_ARGS }}'/$tui_args}"
    line="${line//'{{ TUI_CWD }}'/$tui_cwd}"
    line="${line//'{{ TUI_VERSION }}'/$tui_version}"
    line="${line//'{{ TIMESTAMP }}'/$ts}"
    line="${line//'{{ TERM }}'/$term}"
    line="${line//'{{ COLS }}'/$cols}"
    line="${line//'{{ ROWS }}'/$rows}"
    line="${line//'{{ N_CAPS }}'/$n_caps}"
    line="${line//'{{ N_SHOTS }}'/$n_shots}"
    line="${line//'{{ N_BINDS }}'/$n_binds}"
    line="${line//'{{ N_BLOCKERS }}'/$n_block}"
    line="${line//'{{ N_MAJORS }}'/$n_major}"
    line="${line//'{{ N_MINORS }}'/$n_minor}"
    line="${line//'{{ N_COSMETIC }}'/$n_cos}"
    line="${line//'{{ N_INFO }}'/$n_info}"
    line="${line//'{{ WORKSPACE }}'/$workspace}"
    printf '%s\n' "$line"
  done < "$template"
}

render_template > "$out"

# ---- also drop a copy at the codebase root --------------------------------
# Lands as <tui_cwd>/TUI_AUDIT.md so the user finds it next to the sources.
# Skip when cwd is unusable (missing, "null", same as workspace, or unwritable).
final_path=""
if [[ -n "$tui_cwd" && "$tui_cwd" != "null" && "$tui_cwd" != "." && -d "$tui_cwd" ]]; then
  candidate="$tui_cwd/$final_name"
  if [[ "$candidate" != "$out" ]]; then
    if cp -f "$out" "$candidate" 2>/dev/null; then
      final_path="$candidate"
    else
      warn "could not write $candidate (permissions?). Workspace copy is canonical."
    fi
  fi
fi

if [[ -n "$final_path" ]]; then
  echo "$final_path"
  echo "$out"
else
  echo "$out"
fi

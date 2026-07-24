#!/usr/bin/env bash
# Diff two captures from the same session. Prints a structured summary.
#
# Usage:
#   tui-diff.sh <session> <seq-a> <seq-b>            # plain text diff
#   tui-diff.sh <session> <seq-a> <seq-b> --strict   # also diff trailing spaces
#   tui-diff.sh <session> <seq-a> <seq-b> --ansi     # diff ANSI-colored versions

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=_common.sh
. "$HERE/_common.sh"

session="${1:?usage: tui-diff.sh SESSION SEQ-A SEQ-B [--strict|--ansi]}"
a="${2:?need seq-a}"
b="${3:?need seq-b}"
flag="${4:-}"

require_session "$session"
workspace="$(workspace_for "$session")"
captures="$workspace/captures"

resolve() {
  local seq="$1" ext="$2"
  local pad
  pad="$(printf '%04d' "$((10#$seq))")"
  find "$captures" -maxdepth 1 -type f -name "${pad}-*.${ext}" | sort | head -1
}

case "$flag" in
  --ansi) ext=ansi ;;
  *)      ext=txt  ;;
esac

fa="$(resolve "$a" "$ext")"
fb="$(resolve "$b" "$ext")"
[[ -f "$fa" && -f "$fb" ]] || die "missing capture(s): $fa | $fb"

# Strip trailing spaces unless --strict.
prep() {
  if [[ "$flag" == "--strict" ]]; then
    cat "$1"
  else
    sed -E 's/[[:space:]]+$//' "$1"
  fi
}

# Single diff pass; count added/removed lines with one awk sweep.
read -r added removed < <(
  diff <(prep "$fa") <(prep "$fb") \
    | awk '/^>/ { a++ } /^</ { r++ } END { printf "%d %d\n", a+0, r+0 }'
)
identical=0
if [[ "$added" == "0" && "$removed" == "0" ]]; then identical=1; fi

# Hash both to quickly tell "exact same screen" vs "subtle change".
ha="$(md5sum "$fa" | cut -d' ' -f1)"
hb="$(md5sum "$fb" | cut -d' ' -f1)"

jq -n \
  --arg a "$a" --arg b "$b" \
  --arg fa "$fa" --arg fb "$fb" \
  --arg ha "$ha" --arg hb "$hb" \
  --argjson added "$added" --argjson removed "$removed" \
  --argjson identical "$identical" \
  --arg mode "$ext" \
  '{
    seq_a:$a, seq_b:$b, file_a:$fa, file_b:$fb,
    hash_a:$ha, hash_b:$hb,
    added_lines:$added, removed_lines:$removed,
    identical: ($identical==1),
    mode:$mode
  }'

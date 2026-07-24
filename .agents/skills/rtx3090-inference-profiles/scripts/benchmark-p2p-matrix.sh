#!/usr/bin/env bash
#
# benchmark-p2p-matrix.sh — non-destructive A/B benchmark runner for the dual
# RTX 3090 P2P campaign.
#
# Compares two profile variants that differ in exactly ONE declared knob, using
# model-loader's own `benchmark run --mode llama-bench`. It:
#   * refuses to run unless A and B differ in a single leaf (one-knob rule);
#   * records the initially-loaded profile from GET /_status and RESTORES it in
#     an EXIT trap, even on mid-run failure — the served model is never lost;
#   * runs one discarded warmup per variant, then a thermally-balanced
#     A,B,B,A,A,B,... measured sequence;
#   * retains the FULL benchmark JSON, launch log and a per-run nvidia-smi
#     telemetry sample for every measured run, plus a manifest.
#
# Backend flags are NEVER embedded here: the two profile JSON files are the sole
# source of truth for what is being compared.
#
# The external commands and endpoint are env-overridable so the runner can be
# exercised against fakes without weakening production defaults:
#   MODEL_LOADER_BIN            (default: model-loader)
#   CURL_BIN                    (default: curl)
#   NVIDIA_SMI_BIN              (default: nvidia-smi)
#   MODEL_LOADER_PROXY          (default: http://127.0.0.1:4321)
#   MODEL_LOADER_PROFILES_DIR   (default: $HOME/.config/model-loader/profiles)
#   P2P_MATRIX_MODE             (default: llama-bench)
set -euo pipefail

MODEL_LOADER_BIN="${MODEL_LOADER_BIN:-model-loader}"
CURL_BIN="${CURL_BIN:-curl}"
NVIDIA_SMI_BIN="${NVIDIA_SMI_BIN:-nvidia-smi}"
MODEL_LOADER_PROXY="${MODEL_LOADER_PROXY:-http://127.0.0.1:4321}"
MODEL_LOADER_PROFILES_DIR="${MODEL_LOADER_PROFILES_DIR:-$HOME/.config/model-loader/profiles}"
BENCH_MODE="${P2P_MATRIX_MODE:-llama-bench}"

NVSMI_QUERY="index,memory.used,memory.total,utilization.gpu,power.draw,clocks.sm,clocks.mem,temperature.gpu,pcie.link.gen.current,pcie.link.width.current,clocks_event_reasons.active"
TELEM_INTERVAL="${P2P_MATRIX_TELEMETRY_INTERVAL:-1}"
TELEM_PID=""

PROG="$(basename "$0")"
die() {
	printf '%s: error: %s\n' "$PROG" "$1" >&2
	exit "${2:-1}"
}
usage() {
	cat <<EOF
usage: $PROG --profile-a ID --profile-b ID --label STR --output-dir DIR [--runs N]

  --profile-a ID    profile id of variant A (resolved under \$MODEL_LOADER_PROFILES_DIR)
  --profile-b ID    profile id of variant B; must differ from A in exactly one knob
  --label STR       short label for this A/B (recorded in the manifest)
  --output-dir DIR  directory for artifacts (created if absent)
  --runs N          measured runs per variant (default 5; 1 warmup/variant added)
EOF
}

PROFILE_A="" PROFILE_B="" LABEL="" OUTDIR="" RUNS=5
while [ $# -gt 0 ]; do
	case "$1" in
	--profile-a) PROFILE_A="${2:-}"; shift 2 ;;
	--profile-b) PROFILE_B="${2:-}"; shift 2 ;;
	--label) LABEL="${2:-}"; shift 2 ;;
	--output-dir) OUTDIR="${2:-}"; shift 2 ;;
	--runs) RUNS="${2:-}"; shift 2 ;;
	-h | --help) usage; exit 0 ;;
	*) usage >&2; die "unknown argument: $1" ;;
	esac
done

[ -n "$PROFILE_A" ] || { usage >&2; die "missing required --profile-a"; }
[ -n "$PROFILE_B" ] || { usage >&2; die "missing required --profile-b"; }
[ -n "$LABEL" ] || { usage >&2; die "missing required --label"; }
[ -n "$OUTDIR" ] || { usage >&2; die "missing required --output-dir"; }
case "$RUNS" in
'' | *[!0-9]*) die "--runs must be a positive integer (got '$RUNS')" ;;
esac
[ "$RUNS" -ge 1 ] || die "--runs must be >= 1 (got '$RUNS')"

FILE_A="$MODEL_LOADER_PROFILES_DIR/$PROFILE_A.json"
FILE_B="$MODEL_LOADER_PROFILES_DIR/$PROFILE_B.json"
[ -f "$FILE_A" ] || die "profile file not found: $FILE_A"
[ -f "$FILE_B" ] || die "profile file not found: $FILE_B"

# One-knob validation: A and B may differ in exactly one leaf. Identity/prose
# fields never count. On violation the differing knobs are named and we abort.
one_knob_report="$(
	python3 - "$FILE_A" "$FILE_B" <<'PY'
import json, sys

IGNORE = {"id", "name", "description", "tags", "meta", "pinned", "schemaVersion"}
# Routing-identity leaves bound to the profile id (proxy/backend model routing),
# never a tuning knob: vLLM/SGLang served-model-name, lucebox model-name.
IGNORE_LEAF = {"args.served-model-name", "args.model-name"}


def flatten(obj, prefix=""):
    out = {}
    if isinstance(obj, dict):
        for k, v in obj.items():
            key = f"{prefix}.{k}" if prefix else k
            out.update(flatten(v, key))
    elif isinstance(obj, list):
        # An env-style list ([{key,value}, ...], e.g. launch.env) is keyed by
        # name, not position: adding/removing/changing ONE entry is ONE knob.
        if obj and all(isinstance(x, dict) and "key" in x for x in obj):
            for item in obj:
                name = item["key"]
                rest = {kk: vv for kk, vv in item.items() if kk != "key"}
                if set(rest) == {"value"}:
                    out[f"{prefix}.{name}"] = rest["value"]
                else:
                    out.update(flatten(rest, f"{prefix}.{name}"))
        else:
            for i, v in enumerate(obj):
                out.update(flatten(v, f"{prefix}[{i}]"))
    else:
        out[prefix] = obj
    return out


a = json.load(open(sys.argv[1]))
b = json.load(open(sys.argv[2]))
def keep(k):
    return k.split(".")[0].split("[")[0] not in IGNORE and k not in IGNORE_LEAF


fa = {k: v for k, v in flatten(a).items() if keep(k)}
fb = {k: v for k, v in flatten(b).items() if keep(k)}

diffs = sorted(k for k in set(fa) | set(fb) if fa.get(k) != fb.get(k))
print(len(diffs))
for k in diffs:
    print(k)
PY
)"
diff_count="$(printf '%s\n' "$one_knob_report" | head -n1)"
diff_knobs="$(printf '%s\n' "$one_knob_report" | tail -n +2)"
if [ "$diff_count" != "1" ]; then
	die "profiles must differ in exactly one knob; found $diff_count differing knob(s):
$diff_knobs"
fi

mkdir -p "$OUTDIR" "$OUTDIR/warmup"

# Record the initially-loaded profile so it can be restored no matter how we
# exit. A dead/unreachable proxy simply leaves this empty (nothing to restore).
INITIAL_PROFILE=""
set +e
INITIAL_PROFILE="$(
	"$CURL_BIN" -s "$MODEL_LOADER_PROXY/_status" 2>/dev/null | python3 -c '
import json, sys
try:
    print(json.load(sys.stdin).get("loaded_profile_id", ""))
except Exception:
    print("")
'
)"
set -e

restore_initial() {
	local rc=$?
	stop_telemetry
	if [ -n "$INITIAL_PROFILE" ]; then
		"$MODEL_LOADER_BIN" instance start "$INITIAL_PROFILE" \
			>"$OUTDIR/restore.log" 2>&1 || true
	fi
	return "$rc"
}
trap restore_initial EXIT

# Manifest — provenance for the whole A/B.
python3 - "$OUTDIR/manifest.json" "$LABEL" "$PROFILE_A" "$PROFILE_B" "$RUNS" \
	"$BENCH_MODE" "$INITIAL_PROFILE" "$MODEL_LOADER_PROXY" <<'PY'
import datetime, json, sys
path, label, a, b, runs, mode, initial, proxy = sys.argv[1:9]
json.dump({
    "label": label,
    "profile_a": a,
    "profile_b": b,
    "runs_per_variant": int(runs),
    "mode": mode,
    "initial_loaded_profile": initial,
    "proxy": proxy,
    "created_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
}, open(path, "w"), indent=2)
PY
# --- telemetry sampler --------------------------------------------------------
# telemetry_loop G0 G1 — poll nvidia-smi every $TELEM_INTERVAL and append rows,
# routed by GPU index, to the per-GPU CSVs. First line of each file is the
# field header. Runs until killed.
telemetry_loop() {
	local g0="$1" g1="$2"
	printf '%s\n' "$NVSMI_QUERY" >"$g0"
	printf '%s\n' "$NVSMI_QUERY" >"$g1"
	while :; do
		"$NVIDIA_SMI_BIN" --query-gpu="$NVSMI_QUERY" --format=csv,noheader,nounits 2>/dev/null |
			while IFS= read -r line; do
				[ -n "$line" ] || continue
				idx="${line%%,*}"
				idx="${idx// /}"
				case "$idx" in
				0) printf '%s\n' "$line" >>"$g0" ;;
				1) printf '%s\n' "$line" >>"$g1" ;;
				esac
			done
		sleep "$TELEM_INTERVAL"
	done
}

start_telemetry() { # base
	telemetry_loop "$1.gpu0.csv" "$1.gpu1.csv" &
	TELEM_PID=$!
}

stop_telemetry() {
	if [ -n "$TELEM_PID" ]; then
		kill "$TELEM_PID" 2>/dev/null || true
		wait "$TELEM_PID" 2>/dev/null || true
		TELEM_PID=""
	fi
}

# run_bench PROFILE_ID BASE_PATH — sample GPU telemetry DURING the run, capture
# the full JSON (stdout) and launch log (stderr). The sampler is always stopped,
# even when the benchmark fails, before the (nonzero) status propagates.
run_bench() {
	local pid="$1" base="$2" rc=0
	start_telemetry "$base"
	"$MODEL_LOADER_BIN" benchmark run --profile "$pid" --mode "$BENCH_MODE" --json \
		>"$base.json" 2>"$base.log" || rc=$?
	stop_telemetry
	return "$rc"
}

# Discarded warmup per variant (kept out of the measured set, under warmup/).
run_bench "$PROFILE_A" "$OUTDIR/warmup/$PROFILE_A"
run_bench "$PROFILE_B" "$OUTDIR/warmup/$PROFILE_B"

# Measured, thermally-balanced sequence A,B,B,A,A,B,B,A,... over 2*RUNS slots.
total=$((2 * RUNS))
slot=1
i=0
while [ "$i" -lt "$total" ]; do
	group=$(((i + 1) / 2))
	if [ $((group % 2)) -eq 0 ]; then sel="$PROFILE_A"; else sel="$PROFILE_B"; fi
	printf -v idx '%02d' "$slot"
	run_bench "$sel" "$OUTDIR/run-$idx-$sel"
	slot=$((slot + 1))
	i=$((i + 1))
done

printf '%s: %d measured runs/variant complete; artifacts in %s\n' \
	"$PROG" "$RUNS" "$OUTDIR"

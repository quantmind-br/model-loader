#!/usr/bin/env python3
"""summarize-p2p-matrix.py - statistical summarizer for a completed dual-RTX-3090
P2P A/B benchmark output-dir (stdlib only).

Consumes an output directory produced by ``benchmark-p2p-matrix.sh``:

  <output-dir>/
    manifest.json                       # label, profile_a/b, runs_per_variant, mode, ...
    run-NN-<profileId>.json             # one model-loader `Run` doc per measured run
    run-NN-<profileId>.gpu0.csv         # per-GPU nvidia-smi telemetry sampled during the run
    run-NN-<profileId>.gpu1.csv
    warmup/...                          # discarded warmups - ignored

It emits a single JSON object on stdout with:
  * per-profile / per-preset median / min / max / dispersion of a throughput
    metric (default ``tokensPerSecond``) across the (five) measured runs;
  * error accounting (errored runs, malformed run files, missing runs);
  * per-preset diagnostics plus an aggregate throughput decision: the equal-weight
    geometric mean of candidate/baseline median ratios across every matched preset;
    range overlap remains a variance caveat, not a veto;
  * benchmark data alone reports a performance winner and leaves canonical promotion
    pending external TTFT/draft/correctness guardrails;
  * per-GPU peak VRAM / power / temperature / utilization and throttle events.

It tolerates model-loader's real benchmark JSON shape and malformed or missing
run files: a corrupt run is counted, never fatal.
"""
import argparse
import glob
import json
import math
import os
import re
import statistics
import sys

DEFAULT_METRIC = "tokensPerSecond"
DEFAULT_THRESHOLD_PCT = 5.0

# clocks_event_reasons.active bits that mean the GPU is actually being held
# back (idle/apps-clock/display/sync-boost are NOT throttles):
#   0x04 SW power cap, 0x08 HW slowdown, 0x20 SW thermal,
#   0x40 HW thermal, 0x80 HW power brake.
THROTTLE_MASK = 0x04 | 0x08 | 0x20 | 0x40 | 0x80

RUN_FILE_RE = re.compile(r"^run-\d+-(?P<profile>.+)\.json$")
GPU_CSV_RE = re.compile(r"\.gpu(?P<idx>\d+)\.csv$")


# --- statistics --------------------------------------------------------------
def sample_stats(values):
    """median / min / max / relative-range dispersion for a list of numbers."""
    vals = [float(v) for v in values]
    if not vals:
        return {"n": 0, "median": None, "min": None, "max": None, "dispersion": None}
    lo, hi = min(vals), max(vals)
    med = statistics.median(vals)
    disp = (hi - lo) / med if med else None
    return {"n": len(vals), "median": med, "min": lo, "max": hi, "dispersion": disp}


def ranges_overlap(a_lo, a_hi, b_lo, b_hi):
    """Closed-interval overlap (touching endpoints count as overlapping)."""
    return a_lo <= b_hi and b_lo <= a_hi


# --- decision ----------------------------------------------------------------
def decide_preset(a, b, threshold_pct):
    """Winner for one preset. a/b are sample_stats dicts (a = baseline default).

    Higher metric is better. Returns winner in {"a","b","tie"} plus the signed
    improvement of b over a and whether the five-run ranges overlap.
    """
    out = {
        "a_median": a["median"],
        "b_median": b["median"],
        "a_range": [a["min"], a["max"]],
        "b_range": [b["min"], b["max"]],
        "improvement_pct": None,
        "overlap": None,
        "winner": "tie",
    }
    if a["n"] == 0 or b["n"] == 0 or not a["median"]:
        # Not enough data on one side to make a call.
        out["winner"] = "tie"
        return out
    improvement = (b["median"] - a["median"]) / a["median"] * 100.0
    overlap = ranges_overlap(a["min"], a["max"], b["min"], b["max"])
    out["improvement_pct"] = improvement
    out["overlap"] = overlap
    if abs(improvement) < threshold_pct or overlap:
        out["winner"] = "tie"          # sub-threshold or ambiguous → keep default
    elif improvement >= threshold_pct:
        out["winner"] = "b"
    else:
        out["winner"] = "a"            # candidate regressed beyond threshold
    return out


# --- run parsing -------------------------------------------------------------
def extract_presets(run, metric):
    """Yield (preset_id, name, fillPct, value, errored) for each problem.

    ``value`` is the requested metric; ``errored`` is True when the problem or
    the run carries an error (its value is then not usable).
    """
    rows = []
    run_errored = bool(run.get("err"))
    for p in run.get("problems", []) or []:
        pid = p.get("problemId") or ("fill-%s" % p.get("fillPct"))
        name = p.get("problemName", pid)
        fill = p.get("fillPct")
        errored = run_errored or bool(p.get("err"))
        val = p.get(metric)
        try:
            val = float(val)
        except (TypeError, ValueError):
            val = None
            errored = True
        rows.append((pid, name, fill, val, errored))
    return rows


def _profile_from_filename(name):
    m = RUN_FILE_RE.match(name)
    return m.group("profile") if m else None


def gather_runs(output_dir, metric):
    """Group measured run files by profile id. Returns dict keyed by profile.

    Each value: {runs_seen, runs_ok, runs_malformed, runs_errored, presets:{id:{...}}}.
    Malformed JSON is counted, never fatal; profile is recovered from filename.
    """
    profiles = {}

    def slot(pid):
        return profiles.setdefault(
            pid,
            {
                "runs_seen": 0,
                "runs_ok": 0,
                "runs_malformed": 0,
                "runs_errored": 0,
                "_samples": {},  # preset_id -> {"name","fillPct","values":[]}
            },
        )

    for path in sorted(glob.glob(os.path.join(output_dir, "run-*.json"))):
        base = os.path.basename(path)
        fname_profile = _profile_from_filename(base)
        try:
            with open(path, "r") as fh:
                run = json.load(fh)
        except (ValueError, OSError):
            pid = fname_profile or "unknown"
            s = slot(pid)
            s["runs_seen"] += 1
            s["runs_malformed"] += 1
            continue
        pid = run.get("profileId") or fname_profile or "unknown"
        s = slot(pid)
        s["runs_seen"] += 1
        s["runs_ok"] += 1
        rows = extract_presets(run, metric)
        run_has_error = bool(run.get("err")) or any(r[4] for r in rows)
        if run_has_error:
            s["runs_errored"] += 1
        for preset_id, name, fill, val, errored in rows:
            bucket = s["_samples"].setdefault(
                preset_id, {"name": name, "fillPct": fill, "values": []}
            )
            if not errored and val is not None:
                bucket["values"].append(val)
    return profiles


def _finalize_profile(slot):
    presets = {}
    for pid, b in slot["_samples"].items():
        st = sample_stats(b["values"])
        st["name"] = b["name"]
        st["fillPct"] = b["fillPct"]
        st["samples"] = b["values"]
        presets[pid] = st
    return {
        "runs_seen": slot["runs_seen"],
        "runs_ok": slot["runs_ok"],
        "runs_malformed": slot["runs_malformed"],
        "runs_errored": slot["runs_errored"],
        "presets": presets,
    }


# --- GPU telemetry -----------------------------------------------------------
def parse_reasons(value):
    """Parse a clocks_event_reasons.active field into an int bitmask (tolerant)."""
    if value is None:
        return 0
    s = str(value).strip()
    if not s:
        return 0
    try:
        if s.lower().startswith("0x"):
            return int(s, 16)
        return int(s, 10)
    except ValueError:
        return 0


def is_throttle(value):
    return bool(parse_reasons(value) & THROTTLE_MASK)


def _to_float(s):
    try:
        return float(str(s).strip())
    except (TypeError, ValueError):
        return None


def summarize_gpu_csv(path):
    """Peaks + throttle stats from one per-GPU telemetry CSV.

    Header row (from benchmark-p2p-matrix.sh) maps column names to indices, so
    the summarizer is robust to column reordering.
    """
    summary = {
        "samples": 0,
        "peakVramMb": None,
        "peakPowerW": None,
        "peakTempC": None,
        "peakUtilPct": None,
        "throttleSamples": 0,
        "throttleEvents": 0,
    }
    try:
        with open(path, "r") as fh:
            lines = [ln.rstrip("\n") for ln in fh if ln.strip()]
    except OSError:
        return summary
    if not lines:
        return summary
    header = [h.strip() for h in lines[0].split(",")]
    col = {name: i for i, name in enumerate(header)}

    def cell(fields, name):
        i = col.get(name)
        if i is None or i >= len(fields):
            return None
        return fields[i]

    prev_throttle = False
    for line in lines[1:]:
        fields = [f.strip() for f in line.split(",")]
        summary["samples"] += 1
        mem = _to_float(cell(fields, "memory.used"))
        pw = _to_float(cell(fields, "power.draw"))
        temp = _to_float(cell(fields, "temperature.gpu"))
        util = _to_float(cell(fields, "utilization.gpu"))
        if mem is not None:
            summary["peakVramMb"] = mem if summary["peakVramMb"] is None else max(summary["peakVramMb"], mem)
        if pw is not None:
            summary["peakPowerW"] = pw if summary["peakPowerW"] is None else max(summary["peakPowerW"], pw)
        if temp is not None:
            summary["peakTempC"] = temp if summary["peakTempC"] is None else max(summary["peakTempC"], temp)
        if util is not None:
            summary["peakUtilPct"] = util if summary["peakUtilPct"] is None else max(summary["peakUtilPct"], util)
        thr = is_throttle(cell(fields, "clocks_event_reasons.active"))
        if thr:
            summary["throttleSamples"] += 1
            if not prev_throttle:
                summary["throttleEvents"] += 1
        prev_throttle = thr
    return summary


def gather_gpu(output_dir):
    """Aggregate per-GPU telemetry across all measured-run CSVs (warmups excluded)."""
    gpus = {}
    for path in sorted(glob.glob(os.path.join(output_dir, "*.gpu*.csv"))):
        m = GPU_CSV_RE.search(os.path.basename(path))
        if not m:
            continue
        idx = m.group("idx")
        one = summarize_gpu_csv(path)
        agg = gpus.setdefault(
            idx,
            {
                "samples": 0,
                "peakVramMb": None,
                "peakPowerW": None,
                "peakTempC": None,
                "peakUtilPct": None,
                "throttleSamples": 0,
                "throttleEvents": 0,
                "csvFiles": 0,
            },
        )
        agg["csvFiles"] += 1
        agg["samples"] += one["samples"]
        agg["throttleSamples"] += one["throttleSamples"]
        agg["throttleEvents"] += one["throttleEvents"]
        for k in ("peakVramMb", "peakPowerW", "peakTempC", "peakUtilPct"):
            if one[k] is not None:
                agg[k] = one[k] if agg[k] is None else max(agg[k], one[k])
    return gpus


# --- top-level summary -------------------------------------------------------
def _load_manifest(output_dir):
    path = os.path.join(output_dir, "manifest.json")
    try:
        with open(path, "r") as fh:
            return json.load(fh)
    except (ValueError, OSError):
        return {}


def _overall_decision(per_preset, profile_a, profile_b, profiles, threshold_pct, metric, expected_runs):
    if metric != DEFAULT_METRIC:
        return {
            "overall": "unsupported",
            "performance_winner": "unsupported",
            "aggregate_ratio": None,
            "aggregate_improvement_pct": None,
            "promotion_status": "not_applicable",
        }
    a_err = profiles.get(profile_a, {}).get("runs_errored", 0)
    b_err = profiles.get(profile_b, {}).get("runs_errored", 0)
    complete = isinstance(expected_runs, int) and expected_runs > 0
    complete = complete and bool(per_preset) and all(
        d.get("a_median") is not None and d.get("b_median") is not None
        and d["a_median"] > 0 and d["b_median"] > 0
        and d.get("a_n") == expected_runs and d.get("b_n") == expected_runs
        for d in per_preset.values()
    )
    if complete:
        for pid in (profile_a, profile_b):
            profile = profiles.get(pid, {})
            if profile.get("runs_seen") != expected_runs or profile.get("runs_malformed", 0):
                complete = False
    if a_err or b_err or not complete:
        return {
            "overall": "reject",
            "performance_winner": "reject",
            "aggregate_ratio": None,
            "aggregate_improvement_pct": None,
            "promotion_status": "ineligible",
        }
    ratios = [d["b_median"] / d["a_median"] for d in per_preset.values()]
    ratio = math.prod(ratios) ** (1.0 / len(ratios))
    improvement = (ratio - 1.0) * 100.0
    if improvement >= threshold_pct:
        winner = "b"
        status = "pending_external_guardrails"
    elif improvement <= -threshold_pct:
        winner = "a"
        status = "ineligible"
    else:
        winner = "tie"
        status = "ineligible"
    return {
        "overall": winner,
        "performance_winner": winner,
        "aggregate_ratio": ratio,
        "aggregate_improvement_pct": improvement,
        "promotion_status": status,
    }


def summarize(output_dir, threshold_pct=DEFAULT_THRESHOLD_PCT, metric=DEFAULT_METRIC):
    if not os.path.isdir(output_dir):
        raise FileNotFoundError("output dir not found: %s" % output_dir)
    manifest = _load_manifest(output_dir)
    raw = gather_runs(output_dir, metric)

    profile_a = manifest.get("profile_a")
    profile_b = manifest.get("profile_b")
    # Fall back to whatever profiles the run files reveal (stable order).
    seen = sorted(raw.keys())
    if profile_a is None and seen:
        profile_a = seen[0]
    if profile_b is None:
        for pid in seen:
            if pid != profile_a:
                profile_b = pid
                break

    profiles = {pid: _finalize_profile(slot) for pid, slot in raw.items()}
    expected = manifest.get("runs_per_variant")
    if isinstance(expected, int):
        for pid in (profile_a, profile_b):
            if pid in profiles:
                seen_ok = profiles[pid]["runs_seen"]
                profiles[pid]["runs_missing"] = max(0, expected - seen_ok)

    per_preset = {}
    a_presets = profiles.get(profile_a, {}).get("presets", {})
    b_presets = profiles.get(profile_b, {}).get("presets", {})
    for preset_id in sorted(set(a_presets) | set(b_presets)):
        a_stat = a_presets.get(preset_id, sample_stats([]))
        b_stat = b_presets.get(preset_id, sample_stats([]))
        d = decide_preset(a_stat, b_stat, threshold_pct)
        d["a_n"] = a_stat["n"]
        d["b_n"] = b_stat["n"]
        d["name"] = (a_presets.get(preset_id) or b_presets.get(preset_id) or {}).get("name", preset_id)
        per_preset[preset_id] = d
    aggregate = _overall_decision(per_preset, profile_a, profile_b, profiles, threshold_pct, metric, expected)

    return {
        "label": manifest.get("label"),
        "mode": manifest.get("mode"),
        "profile_a": profile_a,
        "profile_b": profile_b,
        "runs_per_variant": manifest.get("runs_per_variant"),
        "metric": metric,
        "threshold_pct": threshold_pct,
        "profiles": profiles,
        "gpus": gather_gpu(output_dir),
        "decision": {
            "metric": metric,
            "threshold_pct": threshold_pct,
            "per_preset": per_preset,
            **aggregate,
            "errors": {
                "a": profiles.get(profile_a, {}).get("runs_errored", 0),
                "b": profiles.get(profile_b, {}).get("runs_errored", 0),
            },
        },
    }


def main(argv=None):
    ap = argparse.ArgumentParser(description="Summarize a dual-3090 P2P A/B benchmark output-dir.")
    ap.add_argument("output_dir", help="directory produced by benchmark-p2p-matrix.sh")
    ap.add_argument("--threshold", type=float, default=DEFAULT_THRESHOLD_PCT,
                    help="aggregate geometric-mean improvement required for a throughput winner (default 5.0)")
    ap.add_argument("--metric", default=DEFAULT_METRIC,
                    help="per-problem numeric field to summarize (default tokensPerSecond)")
    args = ap.parse_args(argv)
    try:
        summary = summarize(args.output_dir, threshold_pct=args.threshold, metric=args.metric)
    except FileNotFoundError as e:
        print(str(e), file=sys.stderr)
        return 2
    json.dump(summary, sys.stdout, indent=2, sort_keys=True)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())

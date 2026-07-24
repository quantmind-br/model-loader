#!/usr/bin/env python3
"""Focused tests for summarize-p2p-matrix.py.

Everything here runs against synthetic fixtures written into a tmp output-dir:
a manifest.json, model-loader-shaped `Run` JSON files and per-GPU nvidia-smi
telemetry CSVs. Nothing touches the live proxy, profiles or a real GPU.
"""
import importlib.util
import json
import pathlib
import tempfile
import unittest

HERE = pathlib.Path(__file__).resolve().parent
MODULE_PATH = HERE / "summarize-p2p-matrix.py"

# nvidia-smi query header written by benchmark-p2p-matrix.sh (kept in sync).
CSV_HEADER = (
    "index,memory.used,memory.total,utilization.gpu,power.draw,clocks.sm,"
    "clocks.mem,temperature.gpu,pcie.link.gen.current,pcie.link.width.current,"
    "clocks_event_reasons.active"
)
# clocks_event_reasons bit values
IDLE = "0x0000000000000001"          # GPU idle — not a throttle
HW_SLOWDOWN = "0x0000000000000008"   # HW slowdown — throttle
SW_THERMAL = "0x0000000000000020"    # SW thermal slowdown — throttle


def load_module():
    spec = importlib.util.spec_from_file_location("summarize_mod", MODULE_PATH)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


M = load_module()


def problem(fill_pct, tps, err="", ttft=120):
    """One llama-bench ProblemResult, shaped like model-loader emits it."""
    p = {
        "problemId": "fill-%d-256" % fill_pct,
        "problemName": "fill %d%% / tg 256" % fill_pct,
        "resolved": True,
        "score": 1.0,
        "ttftMs": ttft,
        "totalMs": 2000,
        "tokensPerSecond": tps,
        "promptTokens": 100,
        "completionTokens": 256,
        "fillPct": fill_pct,
    }
    if err:
        p["err"] = err
        p["failPhase"] = "infer"
        p["resolved"] = False
    return p


def run_doc(profile_id, presets, run_err=""):
    """A full model-loader `Run` JSON doc. presets: list of (fill, tps[, err])."""
    problems = [problem(*p) for p in presets]
    errored = sum(1 for p in problems if p.get("err"))
    doc = {
        "id": "run-%s" % profile_id,
        "profileId": profile_id,
        "profileName": profile_id,
        "mode": "llama-bench",
        "startedAt": "2026-07-11T00:00:00Z",
        "finishedAt": "2026-07-11T00:05:00Z",
        "profile": {"model": "synthetic.gguf", "ctxSize": 262144},
        "problems": problems,
        "aggregate": {
            "total": len(problems),
            "errored": errored,
            "resolved": len(problems) - errored,
            "avgTokensPerSecond": sum(p["tokensPerSecond"] for p in problems)
            / max(1, len(problems)),
            "peakVramMb": 22000,
            "avgGpuUtil": 95.0,
        },
    }
    if run_err:
        doc["err"] = run_err
    return doc


def csv_rows(idx, rows):
    """rows: list of dict(mem, power, temp, util, reasons). Returns CSV text."""
    out = [CSV_HEADER]
    for r in rows:
        out.append(
            "%d, %s, 24576, %s, %s, 1710, 9751, %s, 4, 8, %s"
            % (
                idx,
                r.get("mem", 20000),
                r.get("util", 90),
                r.get("power", 250),
                r.get("temp", 60),
                r.get("reasons", IDLE),
            )
        )
    return "\n".join(out) + "\n"


class SampleStatsTest(unittest.TestCase):
    def test_five_run_median_min_max(self):
        s = M.sample_stats([44, 40, 48, 42, 46])
        self.assertEqual(s["n"], 5)
        self.assertEqual(s["median"], 44)
        self.assertEqual(s["min"], 40)
        self.assertEqual(s["max"], 48)
        # relative range (max-min)/median
        self.assertAlmostEqual(s["dispersion"], (48 - 40) / 44, places=6)

    def test_empty_samples_are_none(self):
        s = M.sample_stats([])
        self.assertEqual(s["n"], 0)
        self.assertIsNone(s["median"])
        self.assertIsNone(s["dispersion"])


class RangeOverlapTest(unittest.TestCase):
    def test_disjoint_ranges_do_not_overlap(self):
        self.assertFalse(M.ranges_overlap(40, 48, 49, 51))

    def test_touching_and_nested_ranges_overlap(self):
        self.assertTrue(M.ranges_overlap(40, 48, 48, 60))
        self.assertTrue(M.ranges_overlap(40, 55, 45, 50))


class DecisionTest(unittest.TestCase):
    def _stats(self, vals):
        return M.sample_stats(vals)

    def test_ge_5pct_non_overlapping_picks_winner(self):
        a = self._stats([40, 42, 44, 46, 48])   # median 44, [40,48]
        b = self._stats([49, 50, 51, 52, 53])   # median 51, [49,53]
        d = M.decide_preset(a, b, 5.0)
        self.assertEqual(d["winner"], "b")
        self.assertFalse(d["overlap"])
        self.assertGreaterEqual(d["improvement_pct"], 5.0)

    def test_ge_5pct_but_overlapping_is_tie(self):
        a = self._stats([40, 42, 44, 46, 48])   # median 44, [40,48]
        b = self._stats([45, 48, 50, 52, 55])   # median 50, [45,55] overlaps
        d = M.decide_preset(a, b, 5.0)
        self.assertTrue(d["overlap"])
        self.assertEqual(d["winner"], "tie")

    def test_below_threshold_is_tie(self):
        a = self._stats([100, 101, 102, 103, 104])  # median 102
        b = self._stats([106, 107, 108, 109, 110])  # ~5.9%... make small
        # shrink b so improvement < 5%
        b = self._stats([103, 104, 105, 106, 107])  # median 105 → ~2.9%
        d = M.decide_preset(a, b, 5.0)
        self.assertEqual(d["winner"], "tie")

    def test_regression_beyond_threshold_favors_a(self):
        a = self._stats([100, 101, 102, 103, 104])  # median 102
        b = self._stats([80, 81, 82, 83, 84])       # median 82, big regression
        d = M.decide_preset(a, b, 5.0)
        self.assertEqual(d["winner"], "a")


class ThrottleTest(unittest.TestCase):
    def test_throttle_bits_detected(self):
        self.assertFalse(M.is_throttle(IDLE))
        self.assertTrue(M.is_throttle(HW_SLOWDOWN))
        self.assertTrue(M.is_throttle(SW_THERMAL))
        # combined idle+thermal is still a throttle
        self.assertTrue(M.is_throttle("0x0000000000000021"))

    def test_tolerates_garbage_reason_strings(self):
        self.assertFalse(M.is_throttle(""))
        self.assertFalse(M.is_throttle("N/A"))
        self.assertFalse(M.is_throttle("[Not Supported]"))

    def test_events_count_rising_edges_not_samples(self):
        with tempfile.TemporaryDirectory() as d:
            p = pathlib.Path(d) / "run-01-prof.gpu0.csv"
            rows = [
                {"reasons": IDLE},
                {"reasons": HW_SLOWDOWN},   # rising edge 1
                {"reasons": HW_SLOWDOWN},
                {"reasons": IDLE},
                {"reasons": SW_THERMAL},    # rising edge 2
            ]
            p.write_text(csv_rows(0, rows))
            g = M.summarize_gpu_csv(p)
            self.assertEqual(g["throttleSamples"], 3)
            self.assertEqual(g["throttleEvents"], 2)


class GpuPeaksTest(unittest.TestCase):
    def test_peaks_are_maxima_across_samples(self):
        with tempfile.TemporaryDirectory() as d:
            p = pathlib.Path(d) / "run-01-prof.gpu1.csv"
            rows = [
                {"mem": 20000, "power": 240, "temp": 58, "util": 88},
                {"mem": 23010, "power": 291, "temp": 74, "util": 99},
                {"mem": 22500, "power": 270, "temp": 71, "util": 95},
            ]
            p.write_text(csv_rows(1, rows))
            g = M.summarize_gpu_csv(p)
            self.assertEqual(g["samples"], 3)
            self.assertEqual(g["peakVramMb"], 23010)
            self.assertEqual(g["peakPowerW"], 291)
            self.assertEqual(g["peakTempC"], 74)
            self.assertEqual(g["peakUtilPct"], 99)


class PresetExtractionTest(unittest.TestCase):
    def test_extracts_per_preset_metric(self):
        doc = run_doc("prof-a", [(5, 60.0), (50, 44.0), (90, 30.0)])
        got = {pid: v for pid, _, _, v, _ in M.extract_presets(doc, "tokensPerSecond")}
        self.assertEqual(got["fill-5-256"], 60.0)
        self.assertEqual(got["fill-50-256"], 44.0)
        self.assertEqual(got["fill-90-256"], 30.0)

    def test_errored_problem_is_flagged_and_excluded_from_value(self):
        doc = run_doc("prof-a", [(50, 0.0, "http 500")])
        rows = M.extract_presets(doc, "tokensPerSecond")
        self.assertTrue(rows[0][4])  # errored flag


def write_output_dir(root, profile_a, profile_b, runs_a, runs_b, gpu=None):
    """Materialize a full harness-style output dir. runs_*: list of preset-lists."""
    root = pathlib.Path(root)
    (root / "warmup").mkdir(parents=True, exist_ok=True)
    manifest = {
        "label": "p2p-nccl",
        "profile_a": profile_a,
        "profile_b": profile_b,
        "runs_per_variant": max(len(runs_a), len(runs_b)),
        "mode": "llama-bench",
        "initial_loaded_profile": profile_a,
        "proxy": "http://127.0.0.1:4321",
        "created_utc": "2026-07-11T00:00:00+00:00",
    }
    (root / "manifest.json").write_text(json.dumps(manifest))
    # warmup files that must be ignored
    (root / "warmup" / (profile_a + ".json")).write_text(json.dumps(run_doc(profile_a, [(50, 999.0)])))
    n = 1
    for presets in runs_a:
        (root / ("run-%02d-%s.json" % (n, profile_a))).write_text(json.dumps(run_doc(profile_a, presets)))
        n += 1
    for presets in runs_b:
        (root / ("run-%02d-%s.json" % (n, profile_b))).write_text(json.dumps(run_doc(profile_b, presets)))
        n += 1
    if gpu:
        for idx, rows in gpu.items():
            (root / ("run-01-%s.gpu%d.csv" % (profile_a, idx))).write_text(csv_rows(idx, rows))


class SummarizeIntegrationTest(unittest.TestCase):
    def test_five_run_winner_end_to_end(self):
        with tempfile.TemporaryDirectory() as d:
            runs_a = [[(50, v)] for v in (40, 42, 44, 46, 48)]      # median 44
            runs_b = [[(50, v)] for v in (49, 50, 51, 52, 53)]      # median 51
            write_output_dir(d, "base-p2p-a", "cand-p2p-b", runs_a, runs_b)
            out = M.summarize(d, threshold_pct=5.0, metric="tokensPerSecond")
            self.assertEqual(out["label"], "p2p-nccl")
            pa = out["profiles"]["base-p2p-a"]["presets"]["fill-50-256"]
            self.assertEqual(pa["median"], 44)
            self.assertEqual(pa["min"], 40)
            self.assertEqual(pa["max"], 48)
            self.assertEqual(pa["n"], 5)
            dec = out["decision"]["per_preset"]["fill-50-256"]
            self.assertEqual(dec["winner"], "b")
            self.assertEqual(out["decision"]["overall"], "b")

    def test_overlapping_ranges_can_win_on_aggregate_geomean(self):
        with tempfile.TemporaryDirectory() as d:
            runs_a = [[(5, 100), (25, 100), (50, 100), (90, 100)] for _ in range(5)]
            runs_b = [[(5, 98), (25, 108), (50, 116), (90, 129)] for _ in range(5)]
            write_output_dir(d, "base-p2p-a", "cand-p2p-b", runs_a, runs_b)
            out = M.summarize(d, threshold_pct=5.0, metric="tokensPerSecond")
            decision = out["decision"]
            self.assertEqual(decision["overall"], "b")
            self.assertAlmostEqual(decision["aggregate_ratio"], (0.98 * 1.08 * 1.16 * 1.29) ** 0.25)
            self.assertGreater(decision["aggregate_improvement_pct"], 5.0)

    def test_aggregate_uses_raw_per_band_median_ratios(self):
        with tempfile.TemporaryDirectory() as d:
            runs_a = [
                [(5, 99.1), (25, 79.2)],
                [(5, 101.7), (25, 81.4)],
                [(5, 100.3), (25, 80.6)],
                [(5, 103.8), (25, 84.9)],
                [(5, 97.4), (25, 77.3)],
            ]
            runs_b = [
                [(5, 103.9), (25, 91.2)],
                [(5, 107.4), (25, 93.8)],
                [(5, 105.8), (25, 92.7)],
                [(5, 109.6), (25, 96.1)],
                [(5, 102.2), (25, 89.5)],
            ]
            write_output_dir(d, "base-p2p-a", "cand-p2p-b", runs_a, runs_b)
            out = M.summarize(d, threshold_pct=5.0, metric="tokensPerSecond")
            expected = ((105.8 / 100.3) * (92.7 / 80.6)) ** 0.5
            self.assertAlmostEqual(out["decision"]["aggregate_ratio"], expected, places=12)
            self.assertAlmostEqual(out["decision"]["aggregate_improvement_pct"], (expected - 1) * 100, places=10)

    def test_aggregate_below_threshold_is_tie(self):
        with tempfile.TemporaryDirectory() as d:
            runs_a = [[(5, 100), (25, 100)] for _ in range(5)]
            runs_b = [[(5, 102), (25, 104)] for _ in range(5)]
            write_output_dir(d, "base-p2p-a", "cand-p2p-b", runs_a, runs_b)
            out = M.summarize(d, threshold_pct=5.0, metric="tokensPerSecond")
            self.assertEqual(out["decision"]["overall"], "tie")

    def test_aggregate_regression_selects_baseline(self):
        with tempfile.TemporaryDirectory() as d:
            runs_a = [[(5, 100), (25, 100)] for _ in range(5)]
            runs_b = [[(5, 90), (25, 85)] for _ in range(5)]
            write_output_dir(d, "base-p2p-a", "cand-p2p-b", runs_a, runs_b)
            out = M.summarize(d, threshold_pct=5.0, metric="tokensPerSecond")
            self.assertEqual(out["decision"]["overall"], "a")
            self.assertEqual(out["decision"]["performance_winner"], "a")
            self.assertLessEqual(out["decision"]["aggregate_improvement_pct"], -5.0)
            self.assertEqual(out["decision"]["promotion_status"], "ineligible")

    def test_zero_candidate_throughput_rejects_aggregate(self):
        with tempfile.TemporaryDirectory() as d:
            runs_a = [[(5, 100), (25, 100)] for _ in range(5)]
            runs_b = [[(5, 120), (25, 0)] for _ in range(5)]
            write_output_dir(d, "base-p2p-a", "cand-p2p-b", runs_a, runs_b)
            out = M.summarize(d, threshold_pct=5.0, metric="tokensPerSecond")
            self.assertEqual(out["decision"]["overall"], "reject")
            self.assertIsNone(out["decision"]["aggregate_ratio"])
            self.assertEqual(out["decision"]["promotion_status"], "ineligible")

    def test_partial_preset_samples_reject_aggregate(self):
        with tempfile.TemporaryDirectory() as d:
            runs_a = [[(5, 100), (25, 100)] for _ in range(5)]
            runs_b = [[(5, 120), (25, 115)]] + [[(5, 120)] for _ in range(4)]
            write_output_dir(d, "base-p2p-a", "cand-p2p-b", runs_a, runs_b)
            out = M.summarize(d, threshold_pct=5.0, metric="tokensPerSecond")
            self.assertEqual(out["profiles"]["cand-p2p-b"]["presets"]["fill-25-256"]["n"], 1)
            self.assertEqual(out["decision"]["overall"], "reject")
            self.assertEqual(out["decision"]["promotion_status"], "ineligible")

    def test_missing_preset_on_one_side_rejects_incomplete_aggregate(self):
        with tempfile.TemporaryDirectory() as d:
            runs_a = [[(5, 100), (25, 100)] for _ in range(5)]
            runs_b = [[(5, 120)] for _ in range(5)]
            write_output_dir(d, "base-p2p-a", "cand-p2p-b", runs_a, runs_b)
            out = M.summarize(d, threshold_pct=5.0, metric="tokensPerSecond")
            self.assertEqual(out["decision"]["overall"], "reject")

    def test_non_throughput_metric_does_not_select_performance_winner(self):
        with tempfile.TemporaryDirectory() as d:
            runs_a = [[(5, 100, "", 90), (25, 100, "", 110)] for _ in range(5)]
            runs_b = [[(5, 120, "", 130), (25, 120, "", 150)] for _ in range(5)]
            write_output_dir(d, "base-p2p-a", "cand-p2p-b", runs_a, runs_b)
            out = M.summarize(d, threshold_pct=5.0, metric="ttftMs")
            self.assertEqual(out["profiles"]["base-p2p-a"]["presets"]["fill-5-256"]["median"], 90)
            self.assertEqual(out["profiles"]["cand-p2p-b"]["presets"]["fill-5-256"]["median"], 130)
            self.assertEqual(out["decision"]["overall"], "unsupported")
            self.assertEqual(out["decision"]["performance_winner"], "unsupported")
            self.assertEqual(out["decision"]["promotion_status"], "not_applicable")
            self.assertIsNone(out["decision"]["aggregate_ratio"])


    def test_errors_reject_overall(self):
        with tempfile.TemporaryDirectory() as d:
            runs_a = [[(50, v)] for v in (40, 42, 44, 46, 48)]
            runs_b = [[(50, v)] for v in (49, 50, 51, 52, 53)]
            runs_b[2] = [(50, 0.0, "http 504")]                    # one errored run
            write_output_dir(d, "base-p2p-a", "cand-p2p-b", runs_a, runs_b)
            out = M.summarize(d, threshold_pct=5.0, metric="tokensPerSecond")
            self.assertEqual(out["decision"]["overall"], "reject")
            self.assertGreaterEqual(out["profiles"]["cand-p2p-b"]["runs_errored"], 1)

    def test_malformed_and_missing_runs_are_tolerated(self):
        with tempfile.TemporaryDirectory() as d:
            runs_a = [[(50, v)] for v in (40, 42, 44)]             # only 3 good, expected 5
            runs_b = [[(50, v)] for v in (49, 50, 51, 52, 53)]
            write_output_dir(d, "base-p2p-a", "cand-p2p-b", runs_a, runs_b)
            # corrupt file for profile A
            (pathlib.Path(d) / "run-98-base-p2p-a.json").write_text("{ this is not json ")
            out = M.summarize(d, threshold_pct=5.0, metric="tokensPerSecond")
            pa = out["profiles"]["base-p2p-a"]
            self.assertEqual(pa["runs_malformed"], 1)
            self.assertEqual(pa["runs_ok"], 3)
            # summary still produced without raising
            self.assertIn("fill-50-256", pa["presets"])

    def test_missing_manifest_still_summarizes_from_runs(self):
        with tempfile.TemporaryDirectory() as d:
            runs_a = [[(50, v)] for v in (40, 42, 44, 46, 48)]
            runs_b = [[(50, v)] for v in (49, 50, 51, 52, 53)]
            write_output_dir(d, "base-p2p-a", "cand-p2p-b", runs_a, runs_b)
            (pathlib.Path(d) / "manifest.json").unlink()
            out = M.summarize(d, threshold_pct=5.0, metric="tokensPerSecond")
            self.assertIn("base-p2p-a", out["profiles"])
            self.assertIn("cand-p2p-b", out["profiles"])

    def test_per_gpu_peaks_and_throttle_in_summary(self):
        with tempfile.TemporaryDirectory() as d:
            runs_a = [[(50, v)] for v in (40, 42, 44, 46, 48)]
            runs_b = [[(50, v)] for v in (49, 50, 51, 52, 53)]
            gpu = {
                0: [{"mem": 23010, "power": 291, "temp": 74, "reasons": IDLE},
                    {"mem": 22000, "power": 260, "temp": 70, "reasons": HW_SLOWDOWN}],
                1: [{"mem": 21000, "power": 250, "temp": 66, "reasons": IDLE}],
            }
            write_output_dir(d, "base-p2p-a", "cand-p2p-b", runs_a, runs_b, gpu=gpu)
            out = M.summarize(d, threshold_pct=5.0, metric="tokensPerSecond")
            g0 = out["gpus"]["0"]
            self.assertEqual(g0["peakVramMb"], 23010)
            self.assertEqual(g0["peakPowerW"], 291)
            self.assertEqual(g0["peakTempC"], 74)
            self.assertEqual(g0["throttleEvents"], 1)
            self.assertIn("1", out["gpus"])


class MainCliTest(unittest.TestCase):
    def test_main_emits_json_to_stdout(self):
        import io
        from contextlib import redirect_stdout

        with tempfile.TemporaryDirectory() as d:
            runs_a = [[(50, v)] for v in (40, 42, 44, 46, 48)]
            runs_b = [[(50, v)] for v in (49, 50, 51, 52, 53)]
            write_output_dir(d, "base-p2p-a", "cand-p2p-b", runs_a, runs_b)
            buf = io.StringIO()
            with redirect_stdout(buf):
                rc = M.main([d])
            self.assertEqual(rc, 0)
            parsed = json.loads(buf.getvalue())
            self.assertEqual(parsed["decision"]["overall"], "b")

    def test_main_errors_on_missing_dir(self):
        rc = M.main(["/nonexistent/output/dir/xyz"])
        self.assertNotEqual(rc, 0)


if __name__ == "__main__":
    unittest.main(verbosity=2)

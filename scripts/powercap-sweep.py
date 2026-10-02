#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Sweep NVIDIA power limits and measure LLM inference speed and efficiency.

For each power limit the script sets the same cap on every selected GPU,
waits for clocks to settle, then runs a fixed workload set against an
OpenAI-compatible endpoint (the model-loader proxy by default):

  decode-code   long code-generation answer, measures TTFT and decode tok/s
  decode-prose  long prose answer, measures TTFT and decode tok/s
  prefill-long  ~N-token unique prompt (defeats prefix caching), 32 output
                tokens, measures prefill tok/s

GPU power draw is sampled with `nvidia-smi` during every request, so each
result carries average board power (summed over GPUs) and tokens per joule.
Original power limits are always restored on exit, including Ctrl-C.

Setting power limits needs root: run with passwordless sudo for nvidia-smi.

  uv run scripts/powercap-sweep.py --dry-run
  uv run scripts/powercap-sweep.py
  uv run scripts/powercap-sweep.py --levels 350,290,250 --reps 2
"""

import argparse
import datetime as dt
import json
import signal
import statistics
import subprocess
import sys
import threading
import time
import urllib.request
import uuid
from pathlib import Path

DEFAULT_PROFILE = "qwen3.8-27b-w4a16-dflash2-syv-tp2-sharptmpl-256k"
DEFAULT_BASE = "http://127.0.0.1:4321/v1"
STATE_DIR = Path.home() / ".local" / "state" / "model-loader" / "benchmark"

CODE_PROMPT = (
    "Write a complete, production-quality Python module implementing an async "
    "job queue with retries, exponential backoff, dead-letter handling, "
    "persistence to SQLite, and a small CLI. Include type hints, docstrings, "
    "and a pytest test suite. Output only code."
)
PROSE_PROMPT = (
    "Write a long, detailed essay about the history of computing, from the "
    "abacus to modern large language models. Cover at least ten eras with "
    "concrete people, machines, and dates."
)
FILLER_LINE = (
    "def handler_{i}(payload: dict) -> dict:\n"
    "    \"\"\"Normalize record {i} and attach audit metadata.\"\"\"\n"
    "    value = payload.get('value_{i}', {i}) * 3 + {j}\n"
    "    return {{'id': {i}, 'value': value, 'tag': 'batch-{j}'}}\n\n"
)


def log(msg: str) -> None:
    print(f"[{dt.datetime.now():%H:%M:%S}] {msg}", flush=True)


# ---------------------------------------------------------------- GPU control


def query_gpus(indices: list[int]) -> list[dict]:
    out = subprocess.run(
        [
            "nvidia-smi",
            "--query-gpu=index,name,power.limit,power.min_limit,power.max_limit",
            "--format=csv,noheader,nounits",
        ],
        capture_output=True, text=True, check=True,
    ).stdout
    gpus = []
    for line in out.strip().splitlines():
        idx, name, pl, pmin, pmax = [p.strip() for p in line.split(",")]
        if int(idx) in indices:
            gpus.append({
                "index": int(idx), "name": name, "limit": float(pl),
                "min": float(pmin), "max": float(pmax),
            })
    return gpus


def set_power_limit(index: int, watts: float) -> None:
    subprocess.run(
        ["sudo", "-n", "nvidia-smi", "-i", str(index), "-pl", f"{watts:.0f}"],
        capture_output=True, text=True, check=True,
    )


class PowerSampler:
    """Background nvidia-smi sampler; windows are sliced by wall-clock time."""

    def __init__(self, indices: list[int], period_ms: int = 100):
        self.indices = indices
        self.samples: list[tuple[float, float, float]] = []  # (t, watts, sm_mhz)
        self._lock = threading.Lock()
        self._proc = subprocess.Popen(
            [
                "nvidia-smi", "-i", ",".join(map(str, indices)),
                "--query-gpu=index,power.draw,clocks.sm",
                "--format=csv,noheader,nounits", f"-lms={period_ms}",
            ],
            stdout=subprocess.PIPE, text=True, bufsize=1,
        )
        self._pending: dict[int, tuple[float, float]] = {}
        self._thread = threading.Thread(target=self._read, daemon=True)
        self._thread.start()

    def _read(self) -> None:
        assert self._proc.stdout
        for line in self._proc.stdout:
            try:
                idx, watts, sm = [p.strip() for p in line.split(",")]
                self._pending[int(idx)] = (float(watts), float(sm))
            except ValueError:
                continue
            if len(self._pending) == len(self.indices):
                total = sum(w for w, _ in self._pending.values())
                sm_avg = statistics.mean(s for _, s in self._pending.values())
                with self._lock:
                    self.samples.append((time.monotonic(), total, sm_avg))
                self._pending.clear()

    def window(self, t0: float, t1: float) -> dict:
        with self._lock:
            sel = [(w, s) for t, w, s in self.samples if t0 <= t <= t1]
        if not sel:
            return {"avg_power_w": None, "peak_power_w": None, "avg_sm_mhz": None}
        return {
            "avg_power_w": statistics.mean(w for w, _ in sel),
            "peak_power_w": max(w for w, _ in sel),
            "avg_sm_mhz": statistics.mean(s for _, s in sel),
        }

    def stop(self) -> None:
        self._proc.terminate()
        try:
            self._proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self._proc.kill()


# ------------------------------------------------------------------ inference


def stream_chat(base: str, model: str, prompt: str, max_tokens: int,
                timeout: float) -> dict:
    body = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": max_tokens,
        "temperature": 0,
        "stream": True,
        "stream_options": {"include_usage": True},
    }
    req = urllib.request.Request(
        f"{base}/chat/completions", data=json.dumps(body).encode(),
        headers={"content-type": "application/json"},
    )
    t0 = time.monotonic()
    t_first = None
    usage = None
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        for raw in resp:
            line = raw.decode().strip()
            if not line.startswith("data:"):
                continue
            data = line[5:].strip()
            if data == "[DONE]":
                break
            chunk = json.loads(data)
            if chunk.get("usage"):
                usage = chunk["usage"]
            for ch in chunk.get("choices") or []:
                delta = ch.get("delta") or {}
                if t_first is None and (delta.get("content")
                                        or delta.get("reasoning_content")
                                        or delta.get("reasoning")):
                    t_first = time.monotonic()
    t1 = time.monotonic()
    if usage is None or t_first is None:
        raise RuntimeError("stream ended without usage or tokens")
    ctoks = usage["completion_tokens"]
    ptoks = usage["prompt_tokens"]
    decode_s = max(t1 - t_first, 1e-6)
    ttft_s = t_first - t0
    return {
        "t0": t0, "t1": t1, "prompt_tokens": ptoks, "completion_tokens": ctoks,
        "ttft_ms": ttft_s * 1000,
        "decode_tps": (ctoks - 1) / decode_s if ctoks > 1 else None,
        "prefill_tps": ptoks / ttft_s if ttft_s > 0 else None,
    }


def long_prompt(target_tokens: int) -> str:
    # ~52 tokens per filler block (Qwen tokenizer); a leading nonce defeats
    # prefix caching so every rep pays the full prefill.
    blocks = max(1, target_tokens // 52)
    body = "".join(FILLER_LINE.format(i=i, j=i % 17) for i in range(blocks))
    return (f"Session {uuid.uuid4()}.\n\n{body}\n"
            "Which handler adds 5 to value? Answer with just the function name.")


def workloads(args) -> list[dict]:
    return [
        {"name": "decode-code", "prompt": lambda: CODE_PROMPT,
         "max_tokens": args.gen_tokens},
        {"name": "decode-prose", "prompt": lambda: PROSE_PROMPT,
         "max_tokens": args.gen_tokens},
        {"name": "prefill-long", "prompt": lambda: long_prompt(args.prefill_tokens),
         "max_tokens": 32},
    ]


# --------------------------------------------------------------------- report


def median(vals):
    vals = [v for v in vals if v is not None]
    return statistics.median(vals) if vals else None


def summarize(runs: list[dict]) -> dict:
    out = {}
    for key in ("ttft_ms", "decode_tps", "prefill_tps", "avg_power_w",
                "peak_power_w", "avg_sm_mhz", "tokens_per_joule",
                "completion_tokens", "prompt_tokens"):
        out[key] = median([r.get(key) for r in runs])
    return out


def fmt(v, digits=1):
    return "—" if v is None else f"{v:.{digits}f}"


def render_markdown(meta: dict, results: dict) -> str:
    levels = list(results.keys())
    lines = [
        f"# Power-cap sweep — {meta['profile']}",
        "",
        f"- Date: {meta['started_at']}",
        f"- GPUs: {', '.join(f'{g['index']}:{g['name']}' for g in meta['gpus'])}",
        f"- Reps per workload: {meta['reps']} (median shown)",
        f"- Power is summed over all GPUs; tok/J uses generated tokens"
        " (decode) or prompt tokens (prefill).",
        "",
    ]
    base_level = levels[0]
    for wl in ("decode-code", "decode-prose"):
        ref = results[base_level][wl]["summary"]["decode_tps"]
        lines += [f"## {wl}", "",
                  "| Cap/GPU (W) | Decode tok/s | Δ vs "
                  f"{base_level} W | TTFT ms | Avg power (W) | SM MHz | tok/J |",
                  "|---|---|---|---|---|---|---|"]
        for lvl in levels:
            s = results[lvl][wl]["summary"]
            delta = (f"{(s['decode_tps'] / ref - 1) * 100:+.1f}%"
                     if ref and s["decode_tps"] else "—")
            lines.append(
                f"| {lvl} | {fmt(s['decode_tps'])} | {delta} | {fmt(s['ttft_ms'], 0)}"
                f" | {fmt(s['avg_power_w'], 0)} | {fmt(s['avg_sm_mhz'], 0)}"
                f" | {fmt(s['tokens_per_joule'], 3)} |")
        lines.append("")
    ref = results[base_level]["prefill-long"]["summary"]["prefill_tps"]
    lines += ["## prefill-long", "",
              f"| Cap/GPU (W) | Prompt tokens | Prefill tok/s | Δ vs {base_level} W"
              " | TTFT ms | Avg power (W) | tok/J |",
              "|---|---|---|---|---|---|---|"]
    for lvl in levels:
        s = results[lvl]["prefill-long"]["summary"]
        delta = (f"{(s['prefill_tps'] / ref - 1) * 100:+.1f}%"
                 if ref and s["prefill_tps"] else "—")
        lines.append(
            f"| {lvl} | {fmt(s['prompt_tokens'], 0)} | {fmt(s['prefill_tps'], 0)}"
            f" | {delta} | {fmt(s['ttft_ms'], 0)} | {fmt(s['avg_power_w'], 0)}"
            f" | {fmt(s['tokens_per_joule'], 2)} |")
    lines.append("")
    return "\n".join(lines)


# ----------------------------------------------------------------------- main


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--profile", default=DEFAULT_PROFILE)
    ap.add_argument("--base-url", default=DEFAULT_BASE)
    ap.add_argument("--gpus", default="0,1", help="comma-separated GPU indices")
    ap.add_argument("--levels", default="350,320,290,280,250,220",
                    help="per-GPU power limits in W, first one is the reference")
    ap.add_argument("--reps", type=int, default=3)
    ap.add_argument("--gen-tokens", type=int, default=1024)
    ap.add_argument("--prefill-tokens", type=int, default=32000)
    ap.add_argument("--settle", type=float, default=10.0,
                    help="seconds to wait after changing the cap")
    ap.add_argument("--timeout", type=float, default=600.0)
    ap.add_argument("--out-dir", type=Path, default=None)
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    indices = [int(x) for x in args.gpus.split(",")]
    levels = [float(x) for x in args.levels.split(",")]
    gpus = query_gpus(indices)
    if len(gpus) != len(indices):
        sys.exit(f"GPU indices {indices} not all found")
    original = {g["index"]: g["limit"] for g in gpus}

    plan = []
    for lvl in levels:
        per_gpu = {g["index"]: min(max(lvl, g["min"]), g["max"]) for g in gpus}
        plan.append((lvl, per_gpu))

    log(f"profile={args.profile} base={args.base_url}")
    for g in gpus:
        log(f"GPU {g['index']} {g['name']}: current {g['limit']:.0f} W "
            f"(range {g['min']:.0f}-{g['max']:.0f})")
    for lvl, per_gpu in plan:
        clamp = {i: w for i, w in per_gpu.items() if w != lvl}
        log(f"level {lvl:.0f} W" + (f" (clamped: {clamp})" if clamp else ""))
    n_req = len(levels) * (len(workloads(args)) * args.reps + 1)
    log(f"{n_req} requests planned")
    if args.dry_run:
        return 0

    subprocess.run(["sudo", "-n", "true"], check=True)
    started = dt.datetime.now()
    out_dir = args.out_dir or STATE_DIR / f"powercap-{started:%Y%m%d-%H%M%S}"
    out_dir.mkdir(parents=True, exist_ok=True)

    def restore(*_):
        for idx, watts in original.items():
            try:
                set_power_limit(idx, watts)
            except subprocess.CalledProcessError as e:
                print(f"failed to restore GPU {idx}: {e.stderr}", file=sys.stderr)
        log(f"restored power limits: {original}")

    def on_signal(signum, _frame):
        raise KeyboardInterrupt(signum)

    signal.signal(signal.SIGTERM, on_signal)

    meta = {"profile": args.profile, "base_url": args.base_url,
            "started_at": started.isoformat(timespec="seconds"),
            "gpus": gpus, "reps": args.reps, "levels": levels,
            "gen_tokens": args.gen_tokens, "prefill_tokens": args.prefill_tokens}
    results: dict = {}
    sampler = PowerSampler(indices)
    try:
        log("warmup (loads profile through the proxy if needed)")
        stream_chat(args.base_url, args.profile, "Say hi.", 16, args.timeout * 2)
        for lvl, per_gpu in plan:
            key = f"{lvl:.0f}"
            for idx, watts in per_gpu.items():
                set_power_limit(idx, watts)
            log(f"=== cap {key} W/GPU; settling {args.settle:.0f}s")
            time.sleep(args.settle)
            stream_chat(args.base_url, args.profile, CODE_PROMPT, 128, args.timeout)
            results[key] = {}
            for wl in workloads(args):
                runs = []
                for rep in range(args.reps):
                    r = stream_chat(args.base_url, args.profile, wl["prompt"](),
                                    wl["max_tokens"], args.timeout)
                    r.update(sampler.window(r["t0"], r["t1"]))
                    toks = (r["prompt_tokens"] if wl["name"] == "prefill-long"
                            else r["completion_tokens"])
                    dur = r["t1"] - r["t0"]
                    r["tokens_per_joule"] = (toks / (r["avg_power_w"] * dur)
                                             if r["avg_power_w"] else None)
                    del r["t0"], r["t1"]
                    runs.append(r)
                    speed = (r["prefill_tps"] if wl["name"] == "prefill-long"
                             else r["decode_tps"])
                    log(f"  {wl['name']} rep{rep + 1}: {fmt(speed)} tok/s, "
                        f"ttft {r['ttft_ms']:.0f} ms, {fmt(r['avg_power_w'], 0)} W")
                results[key][wl["name"]] = {"runs": runs, "summary": summarize(runs)}
            (out_dir / "results.json").write_text(
                json.dumps({"meta": meta, "results": results}, indent=2))
    except KeyboardInterrupt:
        log("interrupted; writing partial results")
    finally:
        sampler.stop()
        restore()

    if results:
        (out_dir / "results.json").write_text(
            json.dumps({"meta": meta, "results": results}, indent=2))
        complete = {k: v for k, v in results.items() if len(v) == 3}
        if complete:
            md = render_markdown(meta, complete)
            (out_dir / "report.md").write_text(md)
            print("\n" + md)
        log(f"saved to {out_dir}")
    return 0


if __name__ == "__main__":
    sys.exit(main())

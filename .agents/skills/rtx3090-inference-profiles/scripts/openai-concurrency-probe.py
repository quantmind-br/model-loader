#!/usr/bin/env python3
"""openai-concurrency-probe.py — concurrent throughput probe for an
OpenAI-compatible /v1/chat/completions endpoint (stdlib only).

model-loader's internal llama-bench is serial, so it cannot show how a
multi-GPU/P2P profile scales under concurrent load. This probe fills that gap:
it fires a fixed, deterministic request (same prompt, seed, ignore_eos, 256
tokens) at increasing concurrency levels (1,2,4,8,16,32) and reports, as one
JSON object per level, wall-clock and per-request tok/s, p50/p95 TTFT, HTTP
error counts, tokens produced and wall duration.

Determinism: identical payload every request; TTFT is measured from the first
streamed content chunk. Failed (non-2xx / transport-error) requests are counted
as errors and contribute no tokens and no latency to the percentiles.
"""
import argparse
import json
import sys
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor

DEFAULT_ENDPOINT = "http://127.0.0.1:4321/v1/chat/completions"
DEFAULT_CONCURRENCIES = [1, 2, 4, 8, 16, 32]
FIXED_PROMPT = (
    "Write a detailed, self-contained technical explanation of how a modern "
    "GPU executes a matrix multiplication, covering memory hierarchy, warps, "
    "and tensor cores. Be thorough and continue until you are cut off."
)
FIXED_SEED = 1234
FIXED_TEMPERATURE = 0.0
MAX_TOKENS = 256
REQUEST_TIMEOUT_S = 300.0


def build_payload(model, max_tokens=MAX_TOKENS, seed=FIXED_SEED, prompt=FIXED_PROMPT):
    """Return the fixed, deterministic request body used for every call."""
    return {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": max_tokens,
        "temperature": FIXED_TEMPERATURE,
        "seed": seed,
        "ignore_eos": True,
        "stream": True,
    }


def percentile(values, pct):
    """Linear-interpolated percentile over an unsorted list; 0.0 when empty."""
    if not values:
        return 0.0
    s = sorted(values)
    if len(s) == 1:
        return float(s[0])
    rank = (pct / 100.0) * (len(s) - 1)
    lo = int(rank)
    hi = min(lo + 1, len(s) - 1)
    frac = rank - lo
    return float(s[lo] * (1.0 - frac) + s[hi] * frac)


def _do_request(endpoint, payload, timeout=REQUEST_TIMEOUT_S):
    """Issue one streaming request. Returns a result dict:

    {ok, status, ttft, latency, tokens, error}
    ttft/latency are seconds; tokens counts streamed content chunks (falls back
    to usage.completion_tokens if the server reports it on the final frame).
    """
    data = json.dumps(payload).encode()
    req = urllib.request.Request(
        endpoint,
        data=data,
        headers={"Content-Type": "application/json", "Accept": "text/event-stream"},
        method="POST",
    )
    start = time.perf_counter()
    ttft = None
    tokens = 0
    usage_tokens = None
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            status = resp.status
            for raw in resp:
                line = raw.decode("utf-8", "replace").strip()
                if not line or not line.startswith("data:"):
                    continue
                body = line[len("data:"):].strip()
                if body == "[DONE]":
                    break
                try:
                    obj = json.loads(body)
                except ValueError:
                    continue
                if isinstance(obj.get("usage"), dict):
                    ct = obj["usage"].get("completion_tokens")
                    if isinstance(ct, int):
                        usage_tokens = ct
                choices = obj.get("choices") or []
                if choices:
                    delta = choices[0].get("delta") or {}
                    if "content" in delta and delta["content"]:
                        if ttft is None:
                            ttft = time.perf_counter() - start
                        tokens += 1
        latency = time.perf_counter() - start
        counted = usage_tokens if usage_tokens is not None else tokens
        return {
            "ok": True,
            "status": status,
            "ttft": ttft if ttft is not None else latency,
            "latency": latency,
            "tokens": counted,
            "error": None,
        }
    except urllib.error.HTTPError as e:
        code = e.code
        e.close()
        return {"ok": False, "status": code, "ttft": None, "latency": None,
                "tokens": 0, "error": f"http {code}"}
    except Exception as e:  # transport / timeout
        return {"ok": False, "status": None, "ttft": None, "latency": None,
                "tokens": 0, "error": str(e)}


def _run_level(endpoint, payload, concurrency, requestor):
    """Fire `concurrency` requests at once; return (results, wall_seconds)."""
    start = time.perf_counter()
    with ThreadPoolExecutor(max_workers=concurrency) as pool:
        futures = [pool.submit(requestor, endpoint, payload)
                   for _ in range(concurrency)]
        results = [f.result() for f in futures]
    wall = time.perf_counter() - start
    return results, wall


def summarize(concurrency, results, wall):
    """Build the single JSONL object for one concurrency level."""
    ok = [r for r in results if r["ok"]]
    errors = len(results) - len(ok)
    tokens = sum(r["tokens"] for r in ok)
    ttfts = [r["ttft"] for r in ok if r["ttft"] is not None]
    per_req = []
    for r in ok:
        lat = r["latency"]
        if lat and lat > 0:
            per_req.append(r["tokens"] / lat)
    wall_tps = (tokens / wall) if (wall > 0 and tokens) else 0.0
    req_tps = (sum(per_req) / len(per_req)) if per_req else 0.0
    return {
        "concurrency": concurrency,
        "requests": len(results),
        "ok": len(ok),
        "errors": errors,
        "tokens": tokens,
        "duration_s": round(wall, 6),
        "wall_tokens_per_s": round(wall_tps, 4),
        "request_tokens_per_s": round(req_tps, 4),
        "ttft_p50_s": round(percentile(ttfts, 50), 6),
        "ttft_p95_s": round(percentile(ttfts, 95), 6),
    }


def run_sweep(endpoint, model, concurrencies=None, warmup=True, requestor=_do_request):
    """Run the full sweep and return the list of per-level summary dicts."""
    if concurrencies is None:
        concurrencies = list(DEFAULT_CONCURRENCIES)
    payload = build_payload(model)
    if warmup:
        # one discarded warmup request; its timing never enters any summary
        requestor(endpoint, payload)
    out = []
    for c in concurrencies:
        results, wall = _run_level(endpoint, payload, c, requestor)
        out.append(summarize(c, results, wall))
    return out


def _parse_concurrency(spec):
    return [int(x) for x in spec.split(",") if x.strip()]


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--endpoint", default=DEFAULT_ENDPOINT)
    ap.add_argument("--model", required=True)
    ap.add_argument("--concurrency", default=",".join(map(str, DEFAULT_CONCURRENCIES)),
                    help="comma-separated concurrency levels")
    ap.add_argument("--no-warmup", action="store_true",
                    help="skip the discarded warmup request")
    ap.add_argument("--output", default="-",
                    help="JSONL output path ('-' for stdout)")
    args = ap.parse_args(argv)

    concurrencies = _parse_concurrency(args.concurrency)
    summaries = run_sweep(args.endpoint, args.model, concurrencies,
                          warmup=not args.no_warmup)

    sink = sys.stdout if args.output == "-" else open(args.output, "w")
    try:
        for obj in summaries:
            sink.write(json.dumps(obj) + "\n")
    finally:
        if sink is not sys.stdout:
            sink.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())

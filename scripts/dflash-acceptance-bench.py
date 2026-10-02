#!/usr/bin/env python3
"""Measure DFlash draft acceptance for one or more target models on SGLang.

Each target is launched in turn with the same DFlash drafter, sent a fixed
prompt set at concurrency 1 with greedy decoding, and scored from the
per-request speculative metrics SGLang returns in meta_info
(spec_verify_ct, spec_num_correct_drafts, spec_correct_drafts_histogram).

The default pair answers one question: how much does the MiMo-V2.6 SFT cost
the z-lab Qwen3.5-9B drafter, compared with the base model it was trained on.
Both targets are cyankiwi AWQ int4 g32 exports, so the quantization is held
roughly constant (MiMo's is asymmetric, the base's symmetric).

Run it with the SGLang backend's own interpreter, which has transformers:

  ~/dev/model-loader/backends/sglang-stable/.venv/bin/python \\
      scripts/dflash-acceptance-bench.py --dry-run

  ~/dev/model-loader/backends/sglang-stable/.venv/bin/python \\
      scripts/dflash-acceptance-bench.py --swap-out <profile-id>

--swap-out stops that model-loader instance to free the GPUs and starts the
profile again when the run ends, whether it succeeded or not.
"""

import argparse
import datetime as dt
import json
import os
import shlex
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

HF = Path.home() / "models" / "huggingface"
BACKENDS = Path.home() / "dev" / "model-loader" / "backends"
MODEL_LOADER = Path.home() / ".local" / "bin" / "model-loader"

DEFAULT_TARGETS = [
    f"mimo={HF / 'cyankiwi' / 'MiMo-V2.6-Distill-Qwen-9B-AWQ-BF16-INT4'}",
    f"base={HF / 'cyankiwi' / 'Qwen3.5-9B-AWQ-BF16-INT4'}",
]
DEFAULT_DRAFT = HF / "z-lab" / "Qwen3.5-9B-DFlash"
DEFAULT_BACKEND = BACKENDS / "sglang-stable" / "sglang-serve.sh"

COPY_SOURCE = '''\
import json
import time
from dataclasses import dataclass, field


@dataclass
class Job:
    job_id: str
    payload: dict
    attempts: int = 0
    created_at: float = field(default_factory=time.time)


class JobQueue:
    def __init__(self, max_attempts=3):
        self.max_attempts = max_attempts
        self.pending = []
        self.failed = []

    def push(self, job):
        self.pending.append(job)

    def pop(self):
        if not self.pending:
            return None
        return self.pending.pop(0)

    def retry(self, job):
        job.attempts += 1
        if job.attempts >= self.max_attempts:
            self.failed.append(job)
            return False
        self.pending.append(job)
        return True

    def dump(self, path):
        data = {
            "pending": [j.__dict__ for j in self.pending],
            "failed": [j.__dict__ for j in self.failed],
        }
        with open(path, "w") as f:
            json.dump(data, f, indent=2)

    def load(self, path):
        with open(path) as f:
            data = json.load(f)
        self.pending = [Job(**j) for j in data["pending"]]
        self.failed = [Job(**j) for j in data["failed"]]
'''

PROMPTS = {
    "chat": [
        "Explain the difference between TCP and UDP to a junior developer, with one real-world example of each.",
        "Write a short email to my team announcing that the release is delayed by one week because of a failing security audit.",
        "Give me five practical tips for staying focused while working from home, one sentence each.",
    ],
    "code": [
        "Write a Python function that parses an ISO-8601 duration such as 'P1DT2H30M' into total seconds, plus pytest tests for it.",
        "Implement an LRU cache class in TypeScript with O(1) get and put, using a Map. Include a short usage example.",
        "Write a bash script that prints the 10 largest files under a given directory, with human-readable sizes, and handles paths with spaces.",
    ],
    "math": [
        "A store sells pencils at 3 for $0.75 and notebooks at $2.40 each. Maria buys 18 pencils and 4 notebooks and pays with a $20 bill. How much change does she get? Show your reasoning step by step.",
        "A train travels 180 km at 60 km/h and then 240 km at 80 km/h. What is its average speed for the whole trip? Show your reasoning step by step.",
        "Find all integer solutions of x^2 - 5x + 6 = 0 and verify each one. Show your work.",
    ],
    "copy": [
        "Add type hints to every function and method in this module. Return the complete file and nothing else.\n\n```python\n" + COPY_SOURCE + "```",
        "Rename the class JobQueue to RetryQueue everywhere in this module. Return the complete file and nothing else.\n\n```python\n" + COPY_SOURCE + "```",
        "Add a one-line docstring to every method in this module. Return the complete file and nothing else.\n\n```python\n" + COPY_SOURCE + "```",
    ],
}


def log(msg):
    print(f"[{dt.datetime.now():%H:%M:%S}] {msg}", flush=True)


def gpu_free_mib():
    out = subprocess.run(
        ["nvidia-smi", "--query-gpu=memory.free", "--format=csv,noheader,nounits"],
        capture_output=True, text=True, check=True,
    ).stdout
    return [int(x) for x in out.split()]


def http_json(url, payload=None, timeout=600):
    data = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        body = resp.read()
    return json.loads(body) if body else {}


def model_loader(*args):
    return subprocess.run([str(MODEL_LOADER), *args], capture_output=True, text=True)


def find_instance_pid(profile_id):
    res = model_loader("instance", "list", "--json")
    if res.returncode != 0:
        return None
    data = json.loads(res.stdout or "[]")
    items = data if isinstance(data, list) else data.get("instances", [])
    for item in items:
        if item.get("profile") == profile_id or item.get("profileId") == profile_id:
            return item.get("pid")
    return None


def wait_for_free_vram(min_free_mib, timeout=120):
    deadline = time.time() + timeout
    while time.time() < deadline:
        free = gpu_free_mib()
        if min(free) >= min_free_mib:
            return free
        time.sleep(3)
    return gpu_free_mib()


def render_prompts(model_path, thinking):
    from transformers import AutoTokenizer

    tok = AutoTokenizer.from_pretrained(model_path)
    rendered = {}
    for category, prompts in PROMPTS.items():
        rendered[category] = [
            tok.apply_chat_template(
                [{"role": "user", "content": p}],
                tokenize=False,
                add_generation_prompt=True,
                enable_thinking=thinking,
            )
            for p in prompts
        ]
    return rendered


def server_command(args, model_path):
    cmd = [
        str(args.backend),
        "--model-path", str(model_path),
        "--host", "127.0.0.1",
        "--port", str(args.port),
        "--tp-size", str(args.tp),
        "--context-length", str(args.context_length),
        "--mem-fraction-static", str(args.mem_fraction),
        "--max-running-requests", "4",
        "--speculative-algorithm", "DFLASH",
        "--speculative-draft-model-path", str(args.draft),
        "--speculative-dflash-block-size", str(args.block_size),
    ]
    return cmd + shlex.split(args.extra_args)


def start_server(args, model_path, log_path):
    env = dict(os.environ, CUDA_DEVICE_ORDER="PCI_BUS_ID")
    logf = open(log_path, "w")
    proc = subprocess.Popen(
        server_command(args, model_path),
        stdout=logf, stderr=subprocess.STDOUT, env=env, start_new_session=True,
    )
    base = f"http://127.0.0.1:{args.port}"
    deadline = time.time() + args.boot_timeout
    while time.time() < deadline:
        if proc.poll() is not None:
            raise RuntimeError(f"server exited with {proc.returncode}; see {log_path}")
        try:
            urllib.request.urlopen(f"{base}/health_generate", timeout=30)
            return proc
        except (urllib.error.URLError, ConnectionError, TimeoutError):
            time.sleep(5)
    stop_server(proc)
    raise RuntimeError(f"server not healthy after {args.boot_timeout}s; see {log_path}")


def stop_server(proc):
    if proc.poll() is not None:
        return
    os.killpg(proc.pid, signal.SIGTERM)
    try:
        proc.wait(timeout=60)
    except subprocess.TimeoutExpired:
        os.killpg(proc.pid, signal.SIGKILL)
        proc.wait()


def run_prompts(args, rendered):
    base = f"http://127.0.0.1:{args.port}"
    sampling = {"temperature": 0.0, "max_new_tokens": args.max_new_tokens}
    http_json(f"{base}/generate", {"text": rendered["chat"][0], "sampling_params": {"temperature": 0.0, "max_new_tokens": 32}})
    rows = []
    for category, texts in rendered.items():
        for i, text in enumerate(texts):
            t0 = time.perf_counter()
            resp = http_json(f"{base}/generate", {"text": text, "sampling_params": sampling})
            elapsed = time.perf_counter() - t0
            meta = resp.get("meta_info", {})
            row = {
                "category": category,
                "index": i,
                "elapsed_s": elapsed,
                "completion_tokens": meta.get("completion_tokens", 0),
                "verify_ct": meta.get("spec_verify_ct", 0),
                "correct_drafts": meta.get("spec_num_correct_drafts", 0),
                "proposed_drafts": meta.get("spec_num_proposed_drafts", 0),
                "histogram": meta.get("spec_correct_drafts_histogram") or meta.get("spec_accept_histogram") or [],
                "finish_reason": meta.get("finish_reason"),
                "output_head": resp.get("text", "")[:200],
            }
            rows.append(row)
            accept = row["completion_tokens"] / row["verify_ct"] if row["verify_ct"] else float("nan")
            log(f"  {category}[{i}] {row['completion_tokens']} tok, accept_len {accept:.2f}, {row['completion_tokens'] / elapsed:.1f} tok/s")
    return rows


def summarize(rows, block_size):
    out = {}
    for category in [*PROMPTS, "all"]:
        sel = [r for r in rows if category == "all" or r["category"] == category]
        comp = sum(r["completion_tokens"] for r in sel)
        verify = sum(r["verify_ct"] for r in sel)
        correct = sum(r["correct_drafts"] for r in sel)
        proposed = sum(r["proposed_drafts"] for r in sel)
        elapsed = sum(r["elapsed_s"] for r in sel)
        hist = [0] * block_size
        for r in sel:
            for k, n in enumerate(r["histogram"][:block_size]):
                hist[k] += n
        steps = sum(hist)
        per_position = [sum(hist[k:]) / steps if steps else None for k in range(1, block_size)]
        out[category] = {
            "accept_length": comp / verify if verify else None,
            "accept_rate": correct / proposed if proposed else None,
            "per_position": per_position,
            "tok_per_s": comp / elapsed if elapsed else None,
            "completion_tokens": comp,
            "verify_steps": verify,
        }
    return out


def fmt(x, spec="{:.2f}"):
    return "n/a" if x is None else spec.format(x)


def print_report(summaries, names):
    head = "| category | " + " | ".join(f"{n} accept len | {n} pos1 | {n} tok/s" for n in names)
    if len(names) == 2:
        head += f" | {names[0]}/{names[1]} accept len"
    print("\n" + head + " |")
    print("|" + "---|" * (1 + 3 * len(names) + (1 if len(names) == 2 else 0)))
    for category in [*PROMPTS, "all"]:
        cells = []
        for n in names:
            s = summaries[n][category]
            pos1 = s["per_position"][0] if s["per_position"] else None
            cells += [fmt(s["accept_length"]), fmt(pos1), fmt(s["tok_per_s"], "{:.1f}")]
        line = f"| {category} | " + " | ".join(cells)
        if len(names) == 2:
            a = summaries[names[0]][category]["accept_length"]
            b = summaries[names[1]][category]["accept_length"]
            line += " | " + (fmt(a / b, "{:.0%}") if a and b else "n/a")
        print(line + " |")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--target", action="append", help="name=path; repeatable (default: MiMo AWQ and Qwen3.5-9B AWQ)")
    ap.add_argument("--draft", type=Path, default=DEFAULT_DRAFT)
    ap.add_argument("--backend", type=Path, default=DEFAULT_BACKEND, help="SGLang launcher script")
    ap.add_argument("--block-size", type=int, default=8, help="DFlash verify block (8 = 7 drafts, as the vLLM k=7 profile)")
    ap.add_argument("--tp", type=int, default=2)
    ap.add_argument("--port", type=int, default=30100)
    ap.add_argument("--context-length", type=int, default=32768)
    ap.add_argument("--mem-fraction", type=float, default=0.80)
    ap.add_argument("--max-new-tokens", type=int, default=512)
    ap.add_argument("--thinking", action="store_true", help="render prompts with enable_thinking=True")
    ap.add_argument("--boot-timeout", type=int, default=900)
    ap.add_argument("--min-free-gib", type=float, default=20.0, help="per-GPU free VRAM required before each launch")
    ap.add_argument("--extra-args", default="", help="extra SGLang flags, e.g. '--attention-backend triton'")
    ap.add_argument("--swap-out", metavar="PROFILE_ID", help="stop this model-loader instance first and start it again at the end")
    ap.add_argument("--out", type=Path, default=Path.home() / ".local" / "state" / "model-loader" / "dflash-acceptance")
    ap.add_argument("--dry-run", action="store_true", help="render prompts and print the launch commands only")
    args = ap.parse_args()

    targets = []
    for spec in args.target or DEFAULT_TARGETS:
        name, _, path = spec.partition("=")
        targets.append((name, Path(path)))
    for _, path in [*targets, ("draft", args.draft)]:
        if not (path / "config.json").is_file():
            sys.exit(f"missing model: {path}")

    run_dir = args.out / dt.datetime.now().strftime("%Y%m%d-%H%M%S")
    rendered = {name: render_prompts(path, args.thinking) for name, path in targets}

    if args.dry_run:
        for name, path in targets:
            n_prompts = sum(len(v) for v in rendered[name].values())
            print(f"# {name}: {n_prompts} prompts rendered")
            print(shlex.join(server_command(args, path)))
        print(f"# free VRAM per GPU (MiB): {gpu_free_mib()}")
        return

    min_free = int(args.min_free_gib * 1024)
    swapped = False
    if args.swap_out:
        pid = find_instance_pid(args.swap_out)
        if pid is None:
            sys.exit(f"no running instance for profile {args.swap_out}")
        log(f"stopping model-loader instance {args.swap_out} (pid {pid})")
        res = model_loader("instance", "stop", str(pid))
        if res.returncode != 0:
            sys.exit(f"could not stop {args.swap_out}: {res.stderr.strip()}")
        swapped = True

    run_dir.mkdir(parents=True, exist_ok=True)
    summaries, all_rows = {}, {}
    try:
        for name, path in targets:
            free = wait_for_free_vram(min_free)
            if min(free) < min_free:
                raise RuntimeError(f"not enough free VRAM for {name}: {free} MiB, need {min_free} per GPU")
            log(f"launching {name}: {path}")
            proc = start_server(args, path, run_dir / f"server-{name}.log")
            try:
                log(f"{name} healthy, running {sum(len(v) for v in rendered[name].values())} prompts")
                rows = run_prompts(args, rendered[name])
            finally:
                stop_server(proc)
            if not any(r["verify_ct"] for r in rows):
                raise RuntimeError(f"{name}: no speculative metrics in meta_info; DFLASH did not engage (see server-{name}.log)")
            all_rows[name] = rows
            summaries[name] = summarize(rows, args.block_size)
    finally:
        if swapped:
            wait_for_free_vram(min_free, timeout=60)
            log(f"restarting model-loader profile {args.swap_out}")
            res = model_loader("instance", "start", args.swap_out)
            if res.returncode != 0:
                log(f"restart failed, start it by hand: model-loader instance start {args.swap_out}\n{res.stderr.strip()}")

    report = {
        "draft": str(args.draft),
        "block_size": args.block_size,
        "thinking": args.thinking,
        "max_new_tokens": args.max_new_tokens,
        "targets": {name: str(path) for name, path in targets},
        "summary": summaries,
        "rows": all_rows,
    }
    (run_dir / "results.json").write_text(json.dumps(report, indent=2))
    print_report(summaries, [name for name, _ in targets])
    log(f"results: {run_dir / 'results.json'}")


if __name__ == "__main__":
    main()

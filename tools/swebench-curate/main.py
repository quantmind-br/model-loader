#!/usr/bin/env python3
"""Curate N SWE-bench Lite problems into the model-loader Problem schema.

Pulls instances from princeton-nlp/SWE-bench_Lite (test split), fetches the
oracle context (full source of files the gold patch touches) from
raw.githubusercontent.com at the base commit, and emits the JSON array the Go
embed loader expects. Reproducible: same SEED -> same selection.

Usage:
    pip install datasets requests
    python tools/swebench-curate/main.py --count 32 \
        --out internal/service/benchmark/data/swebench_lite.json
"""
import argparse
import json
import random
import re
import sys
from collections import defaultdict

import requests
from datasets import load_dataset

SEED = 20260522
DIFF_PATH_RE = re.compile(r"^\+\+\+ b/(.+)$", re.MULTILINE)


def touched_files(patch: str) -> list:
    return [p for p in DIFF_PATH_RE.findall(patch) if p != "/dev/null"]


def fetch_raw(repo: str, commit: str, path: str):
    url = f"https://raw.githubusercontent.com/{repo}/{commit}/{path}"
    r = requests.get(url, timeout=30)
    return r.text if r.status_code == 200 else None


def lang_of(path: str) -> str:
    if path.endswith(".py"):
        return "python"
    if path.endswith(".go"):
        return "go"
    if path.endswith((".js", ".ts")):
        return "javascript"
    if path.endswith(".rs"):
        return "rust"
    return "other"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--count", type=int, default=32)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    ds = load_dataset("princeton-nlp/SWE-bench_Lite", split="test")
    by_repo = defaultdict(list)
    for row in ds:
        by_repo[row["repo"]].append(row)
    rng = random.Random(SEED)
    repos = sorted(by_repo)
    rng.shuffle(repos)

    out, seen = [], set()
    while len(out) < args.count and repos:
        for repo in list(repos):
            if len(out) >= args.count:
                break
            bucket = by_repo[repo]
            if not bucket:
                repos.remove(repo)
                continue
            row = bucket.pop(rng.randrange(len(bucket)))
            inst = row["instance_id"]
            if inst in seen:
                continue
            files = touched_files(row["patch"])
            if not files:
                continue
            ctx, ok = {}, True
            for path in files:
                content = fetch_raw(row["repo"], row["base_commit"], path)
                if content is None:
                    ok = False
                    break
                ctx[path] = content
            if not ok or not ctx:
                continue
            seen.add(inst)
            out.append({
                "id": inst,
                "name": f"{row['repo']}: {inst.split('-')[-1]}",
                "language": lang_of(files[0]),
                "repoName": row["repo"],
                "statement": row["problem_statement"],
                "contextFiles": ctx,
                "goldenPatch": row["patch"],
            })
            print(f"[{len(out)}/{args.count}] {inst}", file=sys.stderr)

    with open(args.out, "w") as f:
        json.dump(out, f, indent=2, ensure_ascii=False)
    print(f"wrote {len(out)} problems to {args.out}", file=sys.stderr)
    return 0 if len(out) >= args.count else 1


if __name__ == "__main__":
    sys.exit(main())

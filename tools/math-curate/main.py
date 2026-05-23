#!/usr/bin/env python3
"""Curate a GSM8K subset into the model-loader MathProblem schema. Reproducible."""
import argparse
import json
import random
import re
import sys
from collections import defaultdict

from datasets import load_dataset

SEED = 20260523
DATASET_REVISION = "main"
HASH_RE = re.compile(r"####\s*(-?[\d,]+(?:\.\d+)?)")
CALC_RE = re.compile(r"<<.*?>>")


def difficulty(solution: str) -> int:
    steps = len(CALC_RE.findall(solution))
    if steps <= 2:
        return 1
    if steps <= 4:
        return 2
    return 3


def norm(ans: str) -> str:
    a = ans.replace(",", "").replace("$", "").strip()
    try:
        f = float(a)
        return ("%f" % f).rstrip("0").rstrip(".") if "." in a else str(int(f))
    except ValueError:
        return a


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--count", type=int, default=200)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    ds = load_dataset("openai/gsm8k", "main", split="test", revision=DATASET_REVISION)
    by_diff = defaultdict(list)
    for i, row in enumerate(ds):
        m = HASH_RE.search(row["answer"])
        if not m:
            continue
        d = difficulty(row["answer"])
        by_diff[d].append({
            "id": f"gsm8k-{i}",
            "question": row["question"],
            "answer": norm(m.group(1)),
            "difficulty": d,
        })

    rng = random.Random(SEED)
    out = []
    per_band = max(1, args.count // 3)
    for d in (1, 2, 3):
        bucket = by_diff[d]
        rng.shuffle(bucket)
        out.extend(bucket[:per_band])
    if len(out) < args.count:
        rest = [p for d in (1, 2, 3) for p in by_diff[d][per_band:]]
        rng.shuffle(rest)
        out.extend(rest[: args.count - len(out)])
    out = out[: args.count]

    with open(args.out, "w") as f:
        json.dump(out, f, indent=2, ensure_ascii=False)
    print(f"wrote {len(out)} problems to {args.out}", file=sys.stderr)
    return 0 if len(out) >= 100 else 1


if __name__ == "__main__":
    sys.exit(main())

#!/usr/bin/env python3
"""Curate a HumanEval subset into the model-loader CodeGenProblem schema. Reproducible."""
import argparse
import json
import random
import sys

from datasets import load_dataset

SEED = 20260523
DATASET_REVISION = "main"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--count", type=int, default=64)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    ds = load_dataset("openai/openai_humaneval", split="test", revision=DATASET_REVISION)
    rows = [{
        "task_id": r["task_id"],
        "prompt": r["prompt"],
        "canonical_solution": r["canonical_solution"],
        "test": r["test"],
        "entry_point": r["entry_point"],
    } for r in ds]
    rng = random.Random(SEED)
    rng.shuffle(rows)
    out = rows[: args.count]
    out.sort(key=lambda x: x["task_id"])

    with open(args.out, "w") as f:
        json.dump(out, f, indent=2, ensure_ascii=False)
    print(f"wrote {len(out)} problems to {args.out}", file=sys.stderr)
    return 0 if len(out) >= 40 else 1


if __name__ == "__main__":
    sys.exit(main())

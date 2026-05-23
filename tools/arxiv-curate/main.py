#!/usr/bin/env python3
"""Curate a pool of real arXiv abstracts for the long-context quality haystack.

Reproducible: fetches the arXiv abstracts dataset from HuggingFace, filters to
abstracts of a usable length, samples a fixed-seed subset evenly across primary
categories, and emits the model-loader ArxivDoc schema.

Reproduce:  python3 tools/arxiv-curate/main.py --count 80 \
    --out internal/service/benchmark/data/arxiv_docs.json
Requires: pip install datasets huggingface_hub
"""
import argparse
import json
import random
import sys
from collections import defaultdict

from datasets import load_dataset

SEED = 20260523
DATASET = "gfissore/arxiv-abstracts-2021"
DATASET_REVISION = "e4c5fbd4dec8e46a5dc869216fe1c94cc585757a"  # pinned commit of gfissore/arxiv-abstracts-2021 for reproducibility
MIN_ABSTRACT_CHARS = 600
MAX_ABSTRACT_CHARS = 1600


def primary_category(row) -> str:
    cats = row.get("categories") or ""
    if isinstance(cats, list):
        cats = cats[0] if cats else ""
    return (cats.split() or [""])[0].split(".")[0] or "misc"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--count", type=int, default=80)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    ds = load_dataset(DATASET, split="train", revision=DATASET_REVISION, streaming=True)
    by_cat = defaultdict(list)
    for row in ds:
        abstract = (row.get("abstract") or "").strip().replace("\n", " ")
        if not (MIN_ABSTRACT_CHARS <= len(abstract) <= MAX_ABSTRACT_CHARS):
            continue
        cat = primary_category(row)
        by_cat[cat].append({
            "id": str(row.get("id") or row.get("arxiv_id") or len(by_cat[cat])),
            "title": (row.get("title") or "").strip().replace("\n", " "),
            "abstract": abstract,
            "category": cat,
        })
        if sum(len(v) for v in by_cat.values()) >= 4000:
            break

    cats = sorted(by_cat)
    if not cats:
        print("no abstracts matched the length filter", file=sys.stderr)
        return 1
    rng = random.Random(SEED)
    per = max(1, args.count // len(cats))
    picked = []
    for c in cats:
        pool = sorted(by_cat[c], key=lambda d: d["id"])
        rng.shuffle(pool)
        picked.extend(pool[:per])
    rng.shuffle(picked)
    picked = picked[: args.count]
    picked.sort(key=lambda d: d["id"])

    with open(args.out, "w", encoding="utf-8") as f:
        json.dump(picked, f, indent=2, ensure_ascii=False)
        f.write("\n")
    print(f"wrote {len(picked)} abstracts across {len(cats)} categories", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

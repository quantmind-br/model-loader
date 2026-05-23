#!/usr/bin/env python3
"""Curate an MMLU subset into the model-loader MMLUProblem schema. Reproducible.

Fetches cais/mmlu (config "all", split "test"), maps each subject to one of four
super-categories, stratifies a fixed-seed sample evenly across them, and converts
the integer answer (0..3) to a letter.

Reproduce:  python3 tools/mmlu-curate/main.py --count 100 \
    --out internal/service/benchmark/data/mmlu_curated.json
"""
import argparse
import json
import random
import sys
from collections import defaultdict

from datasets import load_dataset

SEED = 20260523
DATASET_REVISION = "main"

# Canonical hendrycks/test subject -> sub-topic mapping.
SUBCATEGORIES = {
    "abstract_algebra": "math", "anatomy": "health", "astronomy": "physics",
    "business_ethics": "business", "clinical_knowledge": "health",
    "college_biology": "biology", "college_chemistry": "chemistry",
    "college_computer_science": "computer science", "college_mathematics": "math",
    "college_medicine": "health", "college_physics": "physics",
    "computer_security": "computer science", "conceptual_physics": "physics",
    "econometrics": "economics", "electrical_engineering": "engineering",
    "elementary_mathematics": "math", "formal_logic": "philosophy",
    "global_facts": "other", "high_school_biology": "biology",
    "high_school_chemistry": "chemistry",
    "high_school_computer_science": "computer science",
    "high_school_european_history": "history", "high_school_geography": "geography",
    "high_school_government_and_politics": "politics",
    "high_school_macroeconomics": "economics", "high_school_mathematics": "math",
    "high_school_microeconomics": "economics", "high_school_physics": "physics",
    "high_school_psychology": "psychology", "high_school_statistics": "math",
    "high_school_us_history": "history", "high_school_world_history": "history",
    "human_aging": "health", "human_sexuality": "culture",
    "international_law": "law", "jurisprudence": "law",
    "logical_fallacies": "philosophy", "machine_learning": "computer science",
    "management": "business", "marketing": "business",
    "medical_genetics": "health", "miscellaneous": "other",
    "moral_disputes": "philosophy", "moral_scenarios": "philosophy",
    "nutrition": "health", "philosophy": "philosophy", "prehistory": "history",
    "professional_accounting": "other", "professional_law": "law",
    "professional_medicine": "health", "professional_psychology": "psychology",
    "public_relations": "politics", "security_studies": "politics",
    "sociology": "culture", "us_foreign_policy": "politics",
    "virology": "health", "world_religions": "philosophy",
}

# Sub-topic -> clean super-category label.
SUPERCATEGORY = {
    "physics": "STEM", "chemistry": "STEM", "biology": "STEM",
    "computer science": "STEM", "math": "STEM", "engineering": "STEM",
    "history": "Humanities", "philosophy": "Humanities", "law": "Humanities",
    "politics": "Social Sciences", "culture": "Social Sciences",
    "economics": "Social Sciences", "geography": "Social Sciences",
    "psychology": "Social Sciences",
    "other": "Other", "business": "Other", "health": "Other",
}
CATEGORIES = ["STEM", "Humanities", "Social Sciences", "Other"]


def category_of(subject: str) -> str:
    return SUPERCATEGORY[SUBCATEGORIES[subject]]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--count", type=int, default=100)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    ds = load_dataset("cais/mmlu", "all", split="test", revision=DATASET_REVISION)
    by_cat = defaultdict(list)
    for i, row in enumerate(ds):
        choices = row["choices"]
        ans = row["answer"]
        if len(choices) != 4 or not (0 <= ans <= 3):
            continue
        cat = category_of(row["subject"])
        by_cat[cat].append({
            "id": f"mmlu-{i}",
            "question": row["question"],
            "choices": [str(c) for c in choices],
            "answer": "ABCD"[ans],
            "category": cat,
        })

    rng = random.Random(SEED)
    per_cat = max(1, args.count // len(CATEGORIES))
    out = []
    for cat in CATEGORIES:
        bucket = by_cat[cat]
        rng.shuffle(bucket)
        out.extend(bucket[:per_cat])
    if len(out) < args.count:
        rest = [p for cat in CATEGORIES for p in by_cat[cat][per_cat:]]
        rng.shuffle(rest)
        out.extend(rest[: args.count - len(out)])
    out = out[: args.count]

    with open(args.out, "w") as f:
        json.dump(out, f, indent=2, ensure_ascii=False)
    print(f"wrote {len(out)} questions to {args.out}", file=sys.stderr)
    return 0 if len(out) >= 100 else 1


if __name__ == "__main__":
    sys.exit(main())

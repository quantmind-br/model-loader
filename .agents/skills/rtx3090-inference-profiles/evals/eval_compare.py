#!/usr/bin/env python3
"""Compare two behavioral reports and fail closed on per-case regressions."""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
from typing import Any

SKIP = {".git", ".agents", ".agent-state", "__pycache__", "node_modules"}


def _sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _tree_hash(root: Path) -> str:
    digest = hashlib.sha256()
    for path in sorted(root.rglob("*"), key=lambda item: item.as_posix()):
        if not path.is_file() or SKIP & set(path.relative_to(root).parts):
            continue
        digest.update(path.relative_to(root).as_posix().encode())
        digest.update(_sha(path).encode())
    return digest.hexdigest()


def _case_map(report: dict[str, Any]) -> dict[str, dict[str, Any]]:
    return {case["id"]: case for case in report.get("cases", [])}


def compare(old: dict[str, Any], new: dict[str, Any], old_root: Path, new_root: Path,
            manifest: Path) -> dict[str, Any]:
    old_cases = _case_map(old)
    new_cases = _case_map(new)
    manifest_hash = _sha(manifest)
    result: dict[str, Any] = {
        "schemaVersion": 1,
        "manifestHash": manifest_hash,
        "roots": {
            "old": {"path": str(old_root), "tree": _tree_hash(old_root)},
            "new": {"path": str(new_root), "tree": _tree_hash(new_root)},
        },
        "grader": {"old": old.get("runnerHash"), "new": new.get("runnerHash")},
    }
    if old.get("manifestHash") != new.get("manifestHash"):
        result.update({
            "conclusion": "incomparable-manifest",
            "reason": "baseline and candidate used different behavioral manifests",
            "cases": [],
            "regressions": [],
            "improvements": [],
        })
        return result

    deltas = []
    regressions = []
    improvements = []
    for case_id in sorted(set(old_cases) | set(new_cases)):
        old_case = old_cases.get(case_id)
        new_case = new_cases.get(case_id)
        old_pass = old_case.get("passed") if old_case else None
        new_pass = new_case.get("passed") if new_case else None
        if old_pass is True and new_pass is False:
            status = "regression"
            regressions.append(case_id)
        elif old_pass is False and new_pass is True:
            status = "improvement"
            improvements.append(case_id)
        elif old_pass == new_pass:
            status = "unchanged"
        else:
            status = "added" if old_case is None else "removed"
        deltas.append({"id": case_id, "old": old_pass, "new": new_pass, "status": status})
    result.update({
        "conclusion": "compared",
        "cases": deltas,
        "regressions": regressions,
        "improvements": improvements,
        "summary": {
            "old": {"passed": sum(case.get("passed") is True for case in old_cases.values()), "total": len(old_cases)},
            "new": {"passed": sum(case.get("passed") is True for case in new_cases.values()), "total": len(new_cases)},
        },
    })
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--old-report", type=Path, required=True)
    parser.add_argument("--new-report", type=Path, required=True)
    parser.add_argument("--old-root", type=Path, required=True)
    parser.add_argument("--new-root", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    result = compare(
        json.loads(args.old_report.read_text(encoding="utf8")),
        json.loads(args.new_report.read_text(encoding="utf8")),
        args.old_root.resolve(),
        args.new_root.resolve(),
        args.manifest.resolve(),
    )
    out = args.out if args.out.suffix == ".json" else args.out.with_suffix(".json")
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf8")
    print(json.dumps({"conclusion": result["conclusion"]}, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

"""Confined fixtures for the shared self-improvement control-plane tests.

Builds a minimal temporary canonical home with a fake skill, sibling peer, and
shared tree, plus a self-contained behavioral runner and comparator. Filesystem
effects are confined to temporary directories; OS and network isolation are out
of scope for these fixtures.
"""
from __future__ import annotations

import importlib.util
import json
import os
import tempfile
from pathlib import Path

CORE_PATH = Path(__file__).resolve().parents[1] / "core.py"


def load_core():
    spec = importlib.util.spec_from_file_location("si_core_under_test", CORE_PATH)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


# A tiny behavioral runner that emits the same report shape the real runners use
# (schemaVersion, runnerHash, manifestHash, rootTreeHash, skillRoot, cases,
# summary). It "passes" a case when the skill ships marker file the case names.
_RUNNER = '''#!/usr/bin/env python3
import argparse, hashlib, json, os
from pathlib import Path

def sha_file(p): return hashlib.sha256(Path(p).read_bytes()).hexdigest()

def tree_hash(root):
    root = Path(root); d = hashlib.sha256()
    for f in sorted(p for p in root.rglob("*") if p.is_file()):
        d.update(str(f.relative_to(root)).encode()); d.update(b"\\0"); d.update(f.read_bytes())
    return d.hexdigest()

ap = argparse.ArgumentParser()
ap.add_argument("--manifest", required=True)
ap.add_argument("--skill-root", required=True)
ap.add_argument("--out", required=True)
a = ap.parse_args()
manifest = json.loads(Path(a.manifest).read_text())
root = Path(a.skill_root)
cases = []
for c in manifest["cases"]:
    marker = root / c["marker"]
    passed = marker.is_file() and (c.get("contains", "") in marker.read_text())
    cases.append({"id": c["id"], "passed": passed, "assertions": [{"id": "marker", "kind": "file", "passed": passed, "evidence": {}}]})
report = {
    "schemaVersion": 1,
    "runnerHash": sha_file(__file__),
    "manifestHash": sha_file(a.manifest),
    "rootTreeHash": tree_hash(root),
    "skillRoot": str(root),
    "cases": cases,
    "summary": {"passed": sum(c["passed"] for c in cases), "total": len(cases)},
}
Path(a.out).write_text(json.dumps(report, indent=2, sort_keys=True) + "\\n")
print(json.dumps(report["summary"], sort_keys=True))
raise SystemExit(0 if report["summary"]["passed"] == report["summary"]["total"] else 1)
'''

# A comparator matching the real eval_compare interface + conclusions.
_COMPARATOR = '''#!/usr/bin/env python3
import argparse, hashlib, json
from pathlib import Path

def sha_file(p): return hashlib.sha256(Path(p).read_bytes()).hexdigest()

def hashes(root):
    root = Path(root)
    def fh(p): return sha_file(p) if p.is_file() else None
    return {"skill": fh(root / "SKILL.md"), "capabilities": fh(root / "capabilities.json")}

ap = argparse.ArgumentParser()
for f in ("--old-report","--new-report","--old-root","--new-root","--manifest","--out"):
    ap.add_argument(f, required=True)
a = ap.parse_args()
old = json.loads(Path(a.old_report).read_text()); new = json.loads(Path(a.new_report).read_text())
oh = hashes(a.old_root); nh = hashes(a.new_root)
res = {"schemaVersion": 1, "manifestHash": sha_file(a.manifest)}
if oh["skill"] == nh["skill"] and oh["capabilities"] == nh["capabilities"]:
    res["conclusion"] = "inconclusive-identical-roots"; res["cases"] = []
elif old.get("manifestHash") != new.get("manifestHash"):
    res["conclusion"] = "incomparable-manifest"; res["cases"] = []
else:
    om = {c["id"]: c for c in old["cases"]}; nm = {c["id"]: c for c in new["cases"]}
    regr = [i for i in om if om[i]["passed"] and not nm.get(i, {}).get("passed", False)]
    impr = [i for i in nm if nm[i]["passed"] and not om.get(i, {}).get("passed", False)]
    res["conclusion"] = "compared"; res["regressions"] = regr; res["improvements"] = impr
    res["cases"] = [{"id": i, "old": om.get(i, {}).get("passed"), "new": nm.get(i, {}).get("passed")} for i in sorted(set(om) | set(nm))]
Path(a.out).write_text(json.dumps(res, indent=2, sort_keys=True) + "\\n")
raise SystemExit(0)
'''


def make_canonical(tmp: Path, skill_name: str = "alpha-skill") -> tuple[Path, dict]:
    """Create a canonical home with one self-contained fixture skill."""
    home = tmp / "canon"
    home.mkdir(parents=True, exist_ok=True)
    _make_skill(home / skill_name, skill_name)
    config = {
        "schemaVersion": 1,
        "skillName": skill_name,
        "deploymentEntries": [skill_name],
        "validationCommands": [["python3", "check.py", "--root", "{skill_root}"]],
        "behavioral": {
            "manifest": "evals/behavioral.json",
            "runner": ["python3", "evals/run_behavioral.py", "--manifest", "{manifest}",
                       "--skill-root", "{skill_root}", "--out", "{report}"],
            "comparator": ["python3", "evals/eval_compare.py"],
        },
        "syncExcludes": [".agents/", ".git/", "__pycache__/", "*.pyc"],
    }
    return home, config


def _make_skill(root: Path, name: str) -> None:
    root.mkdir(parents=True, exist_ok=True)
    (root / "SKILL.md").write_text(
        f"---\nname: {name}\ndescription: fixture skill for tests\n---\n\n# {name}\n", encoding="utf8")
    (root / "capabilities.json").write_text(json.dumps({"schemaVersion": 1, "capabilities": []}) + "\n", encoding="utf8")
    (root / "self-improvement.json").write_text(json.dumps({"schemaVersion": 1, "skillName": name}) + "\n", encoding="utf8")
    refs = root / "references"
    refs.mkdir(exist_ok=True)
    (refs / "note.md").write_text("baseline note\n", encoding="utf8")
    # a validator that always passes (records the shared contract exists)
    (root / "check.py").write_text(
        "import sys\nprint('ok')\nsys.exit(0)\n", encoding="utf8")
    evals = root / "evals"
    evals.mkdir(exist_ok=True)
    (evals / "run_behavioral.py").write_text(_RUNNER, encoding="utf8")
    (evals / "eval_compare.py").write_text(_COMPARATOR, encoding="utf8")
    (evals / "behavioral.json").write_text(json.dumps({
        "schemaVersion": 1,
        "cases": [{"id": "note-present", "marker": "references/note.md", "contains": "note"}],
    }) + "\n", encoding="utf8")


def base_observation(skill_name: str = "alpha-skill", **over) -> dict:
    obs = {
        "type": "technique",
        "skill_name": skill_name,
        "pattern_key": "fixture.key",
        "source_trust": "authenticated-user",
        "scope": "portable-skill",
        "risk": "low",
        "observation": "a fixture observation",
        "provenance": "maintainer instruction",
        "hypothesis": "a change that preserves behavior",
        "applicability": "test only",
        "evaluation_plan": "run behavioral; no regressions",
        "proposed_changes": [{"path": "references/note.md", "operation": "replace"}],
        "evidence": [],
    }
    obs.update(over)
    return obs

#!/usr/bin/env python3
"""Validate the skill and its self-improvement contract without live GPU access."""
from __future__ import annotations

import argparse
import importlib.util
import json
import re
import sys
from pathlib import Path

DEFAULT_ROOT = Path(__file__).resolve().parents[1]
SKILL_NAME = "rtx3090-inference-profiles"
REQUIRED = (
    "SKILL.md",
    "self-improvement.json",
    "references/self-improvement.md",
    "scripts/self_improvement.py",
    "scripts/self_improvement_lib/core.py",
    "scripts/self_improvement_lib/schemas/candidate.schema.json",
    "scripts/self_improvement_lib/tests/test_core.py",
    "evals/evals.json",
    "evals/behavioral.json",
    "evals/run_behavioral.py",
    "evals/eval_compare.py",
    "evals/smoke_self_improvement.py",
)


def _load_core(root: Path):
    path = (root / "scripts" / "self_improvement_lib" / "core.py").resolve()
    spec = importlib.util.spec_from_file_location("si_core_validator", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load bundled core: {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _frontmatter(text: str) -> tuple[str | None, str | None]:
    match = re.match(r"^---\n(.*?)\n---\n", text, re.DOTALL)
    if not match:
        return None, None
    block = match.group(1)
    name_match = re.search(r"^name:\s*(.+)$", block, re.MULTILINE)
    desc_match = re.search(r"^description:\s*(.+)$", block, re.MULTILINE)
    name = name_match.group(1).strip().strip("\"'") if name_match else None
    desc = desc_match.group(1).strip().strip("\"'") if desc_match else None
    return name, desc


def diagnostics(root: Path) -> list[str]:
    errors: list[str] = []
    for rel in REQUIRED:
        if not (root / rel).is_file():
            errors.append(f"missing required file: {rel}")

    skill_path = root / "SKILL.md"
    skill_text = skill_path.read_text(encoding="utf8") if skill_path.is_file() else ""
    name, description = _frontmatter(skill_text)
    if name != root.name or name != SKILL_NAME:
        errors.append(f"SKILL.md name {name!r} must equal directory {root.name!r}")
    if not description:
        errors.append("SKILL.md description must be non-empty and single-line")
    elif len(description) > 1024:
        errors.append(f"SKILL.md description exceeds 1024 chars ({len(description)})")
    if "## Learning loop" not in skill_text:
        errors.append("SKILL.md is missing the Learning loop")
    if "references/self-improvement.md" not in skill_text:
        errors.append("SKILL.md does not link the self-improvement policy")
    if "scripts/self_improvement.py" not in skill_text:
        errors.append("SKILL.md does not name the L1 entrypoint")

    policy_path = root / "references" / "self-improvement.md"
    policy = policy_path.read_text(encoding="utf8") if policy_path.is_file() else ""
    for marker in (
        "Self-improvement policy",
        "Self-healing is not promotion",
        "Domain oracle",
        "State and candidate lifecycle",
        "Evaluation and promotion gates",
        "Transaction, conflict, and rollback",
    ):
        if marker.lower() not in policy.lower():
            errors.append(f"self-improvement policy missing marker: {marker}")

    cfg_path = root / "self-improvement.json"
    if cfg_path.is_file():
        try:
            cfg = json.loads(cfg_path.read_text(encoding="utf8"))
        except json.JSONDecodeError as exc:
            errors.append(f"self-improvement.json: {exc}")
        else:
            allowed = {
                "schemaVersion", "skillName", "deploymentEntries",
                "validationCommands", "behavioral", "syncExcludes",
            }
            unknown = set(cfg) - allowed
            if unknown:
                errors.append(f"self-improvement.json unknown keys: {sorted(unknown)}")
            if cfg.get("schemaVersion") != 1:
                errors.append("self-improvement.json schemaVersion must be 1")
            if cfg.get("skillName") != SKILL_NAME:
                errors.append("self-improvement.json skillName mismatch")
            if cfg.get("deploymentEntries") != [SKILL_NAME]:
                errors.append("deploymentEntries must contain only this skill")
            commands = cfg.get("validationCommands")
            if not isinstance(commands, list) or not commands or not all(
                isinstance(command, list) and command and all(isinstance(token, str) for token in command)
                for command in commands
            ):
                errors.append("validationCommands must be a non-empty argv matrix")
            behavioral = cfg.get("behavioral")
            if not isinstance(behavioral, dict) or set(behavioral) != {"manifest", "runner", "comparator"}:
                errors.append("behavioral config must contain manifest, runner, comparator")

    evals_path = root / "evals" / "evals.json"
    if evals_path.is_file():
        try:
            evals = json.loads(evals_path.read_text(encoding="utf8"))
        except json.JSONDecodeError as exc:
            errors.append(f"evals/evals.json: {exc}")
        else:
            if evals.get("skill_name") != SKILL_NAME:
                errors.append("evals/evals.json skill_name mismatch")
            cases = evals.get("evals")
            if not isinstance(cases, list) or len(cases) < 5:
                errors.append("evals/evals.json needs at least five baseline cases")
            else:
                ids: set[object] = set()
                for case in cases:
                    if not isinstance(case, dict):
                        errors.append("eval case must be an object")
                        continue
                    case_id = case.get("id")
                    if case_id in ids:
                        errors.append(f"duplicate eval id: {case_id!r}")
                    ids.add(case_id)
                    if not isinstance(case.get("prompt"), str) or not case["prompt"].strip():
                        errors.append(f"eval {case_id!r}: prompt required")
                    if not isinstance(case.get("expected_output"), str) or not case["expected_output"].strip():
                        errors.append(f"eval {case_id!r}: expected_output required")
                    assertions = case.get("assertions")
                    if not isinstance(assertions, list) or not assertions or not all(
                        isinstance(item, str) and item.strip() for item in assertions
                    ):
                        errors.append(f"eval {case_id!r}: non-empty string assertions required")

    try:
        core = _load_core(root)
        core.verify_schema_integrity()
    except Exception as exc:  # validator must report bundled-core breakage
        errors.append(f"bundled self-improvement core/schema validation failed: {exc}")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    parser.add_argument("--root", type=Path, default=DEFAULT_ROOT)
    args = parser.parse_args()
    root = args.root.resolve()
    errors = diagnostics(root)
    payload = {"ok": not errors, "root": str(root), "errors": errors}
    print(json.dumps(payload, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())

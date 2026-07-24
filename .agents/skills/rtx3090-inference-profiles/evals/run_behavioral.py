#!/usr/bin/env python3
"""Run confined deterministic behavioral checks for this skill."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import subprocess
import tempfile
from pathlib import Path
from typing import Any

MAX_TIMEOUT = 120
MAX_OUTPUT = 1024 * 1024
SKIP = {".git", ".agents", ".agent-state", "__pycache__", "node_modules"}


def _sha(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def _tree_hash(root: Path) -> str:
    digest = hashlib.sha256()
    for path in sorted(root.rglob("*"), key=lambda item: item.as_posix()):
        if not path.is_file() or SKIP & set(path.relative_to(root).parts):
            continue
        digest.update(path.relative_to(root).as_posix().encode())
        digest.update(_sha(path.read_bytes()).encode())
    return digest.hexdigest()


def _expand(value: str, root: Path, tmp: Path) -> str:
    return value.replace("${skill_root}", str(root)).replace("${tmp}", str(tmp))


def _token(value: Any, root: Path, tmp: Path) -> str:
    if isinstance(value, str):
        return _expand(value, root, tmp)
    if not isinstance(value, dict) or set(value) not in ({"path"}, {"value"}):
        raise ValueError(f"invalid argv token: {value!r}")
    if "value" in value:
        return str(value["value"])
    expanded = Path(_expand(str(value["path"]), root, tmp))
    resolved = Path(os.path.realpath(expanded))
    allowed = (
        resolved == root or resolved.is_relative_to(root)
        or resolved == tmp or resolved.is_relative_to(tmp)
    )
    if not allowed:
        raise ValueError(f"path escapes evaluation roots: {resolved}")
    return str(resolved)


def _json_at(text: str, selector: str) -> Any:
    current: Any = json.loads(text)
    if not selector:
        return current
    for part in selector.split("."):
        current = current[int(part)] if isinstance(current, list) else current[part]
    return current


def _run_case(case: dict[str, Any], root: Path) -> dict[str, Any]:
    with tempfile.TemporaryDirectory(prefix="rtx3090-behavioral-") as directory:
        tmp = Path(directory)
        command = case["command"]
        argv = [_token(item, root, tmp) for item in command["argv"]]
        cwd = Path(_expand(command.get("cwd", "${skill_root}"), root, tmp)).resolve()
        if not (cwd == root or cwd.is_relative_to(root) or cwd == tmp or cwd.is_relative_to(tmp)):
            raise ValueError(f"cwd escapes evaluation roots: {cwd}")
        timeout = min(int(command.get("timeout_s", MAX_TIMEOUT)), MAX_TIMEOUT)
        proc = subprocess.run(argv, cwd=cwd, capture_output=True, text=True, timeout=timeout)
        stdout = proc.stdout[:MAX_OUTPUT]
        stderr = proc.stderr[:MAX_OUTPUT]
        assertions = []
        for assertion in case["assertions"]:
            kind = assertion["kind"]
            evidence: dict[str, Any]
            if kind == "cli_exit":
                value = proc.returncode
                passed = value == assertion["equals"]
                evidence = {"actual": value, "expected": assertion["equals"]}
            elif kind == "json_path":
                try:
                    value = _json_at(stdout, assertion.get("selector", ""))
                    passed = value == assertion.get("expected")
                    evidence = {"value": value, "expected": assertion.get("expected")}
                except (KeyError, IndexError, TypeError, ValueError, json.JSONDecodeError) as exc:
                    passed = False
                    evidence = {"error": str(exc)}
            elif kind == "stdout_contains":
                needle = assertion["expected"]
                passed = needle in stdout
                evidence = {"expected": needle}
            else:
                raise ValueError(f"unsupported assertion kind: {kind}")
            assertions.append({
                "id": assertion.get("id", kind),
                "kind": kind,
                "passed": passed,
                "evidence": evidence,
            })
        return {
            "id": case["id"],
            "exit": proc.returncode,
            "passed": all(item["passed"] for item in assertions),
            "assertions": assertions,
            "stdoutSha256": _sha(stdout.encode()),
            "stderrSha256": _sha(stderr.encode()),
        }


def _validate(manifest: dict[str, Any]) -> None:
    if manifest.get("schemaVersion") != 1:
        raise ValueError("behavioral schemaVersion must be 1")
    cases = manifest.get("cases")
    if not isinstance(cases, list) or not cases:
        raise ValueError("behavioral cases must be a non-empty list")
    ids: set[str] = set()
    for case in cases:
        case_id = case.get("id")
        if not isinstance(case_id, str) or not case_id or case_id in ids:
            raise ValueError(f"case id must be unique: {case_id!r}")
        ids.add(case_id)
        if not isinstance(case.get("command", {}).get("argv"), list):
            raise ValueError(f"{case_id}: command.argv required")
        if not isinstance(case.get("assertions"), list) or not case["assertions"]:
            raise ValueError(f"{case_id}: assertions required")


def run(manifest_path: Path, root: Path, out_path: Path) -> dict[str, Any]:
    manifest = json.loads(manifest_path.read_text(encoding="utf8"))
    _validate(manifest)
    cases = [_run_case(case, root) for case in manifest["cases"]]
    report = {
        "schemaVersion": 1,
        "runnerHash": _sha(Path(__file__).read_bytes()),
        "manifestHash": _sha(manifest_path.read_bytes()),
        "rootTreeHash": _tree_hash(root),
        "skillRoot": str(root),
        "cases": cases,
        "summary": {"passed": sum(case["passed"] for case in cases), "total": len(cases)},
    }
    if out_path.resolve() == root or out_path.resolve().is_relative_to(root):
        raise ValueError("behavioral output must remain outside the skill root")
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf8")
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--skill-root", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    report = run(args.manifest.resolve(), args.skill_root.resolve(), args.out.resolve())
    print(json.dumps(report["summary"], sort_keys=True))
    return 0 if report["summary"]["passed"] == report["summary"]["total"] else 1


if __name__ == "__main__":
    raise SystemExit(main())

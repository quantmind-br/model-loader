#!/usr/bin/env python3
"""L1 self-improvement wrapper backed by the skill-local control plane.

Loads ``<skill-root>/self-improvement.json`` and
``<skill-root>/scripts/self_improvement_lib/core.py``. The skill is fully
self-contained; this file adapts arguments and emits one JSON object per command.

Exit codes: 0 success/clean; 2 invalid input/config/schema; 3 policy/approval
refusal; 4 preimage/project-copy/concurrent-edit conflict; 5 apply began but
post-validation failed.
"""
from __future__ import annotations

import argparse
import importlib.util
import json
import subprocess
import sys
from pathlib import Path

SKILL_ROOT = Path(__file__).resolve().parents[1]
CORE_PATH = SKILL_ROOT / "scripts" / "self_improvement_lib" / "core.py"


def _load_core():
    spec = importlib.util.spec_from_file_location("si_core", CORE_PATH)
    if spec is None or spec.loader is None:
        raise SystemExit(f"cannot load core module at {CORE_PATH}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _load_config() -> dict:
    cfg_path = SKILL_ROOT / "self-improvement.json"
    cfg = json.loads(cfg_path.read_text(encoding="utf8"))
    allowed = {
        "schemaVersion", "skillName", "deploymentEntries",
        "validationCommands", "behavioral", "syncExcludes",
    }
    unknown = set(cfg) - allowed
    if unknown:
        raise SystemExit(f"self-improvement.json: unknown top-level keys {sorted(unknown)}")
    if cfg.get("schemaVersion") != 1:
        raise SystemExit("self-improvement.json: schemaVersion must be 1")
    if cfg.get("skillName") != SKILL_ROOT.name:
        raise SystemExit(f"self-improvement.json: skillName must equal {SKILL_ROOT.name!r}")
    return cfg


def _git_root(start: Path) -> Path | None:
    try:
        out = subprocess.run(
            ["git", "-C", str(start), "rev-parse", "--show-toplevel"],
            capture_output=True,
            text=True,
            timeout=10,
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if out.returncode == 0 and out.stdout.strip():
        return Path(out.stdout.strip())
    return None


def _resolve_project_root(explicit: str | None) -> Path:
    if explicit:
        return Path(explicit).expanduser().resolve()
    root = _git_root(Path.cwd())
    return root if root is not None else Path.cwd().resolve()


def _emit(obj: dict) -> None:
    sys.stdout.write(json.dumps(obj, indent=2, sort_keys=True) + "\n")




def main(argv: list[str] | None = None) -> int:
    core = _load_core()
    cfg = _load_config()
    skill_name = cfg["skillName"]

    parser = argparse.ArgumentParser(description="L1 self-improvement control")
    sub = parser.add_subparsers(dest="command", required=True)

    def add_common(command: argparse.ArgumentParser) -> None:
        command.add_argument("--project-root", default=None)
        command.add_argument("--canonical-home", default=None)
        command.add_argument("--state-dir", default=None)

    record = sub.add_parser("record")
    add_common(record)
    record.add_argument("--input", required=True, help="JSON file or - for stdin")

    stage = sub.add_parser("stage")
    add_common(stage)
    stage.add_argument("--candidate", required=True)

    evaluate = sub.add_parser("evaluate")
    add_common(evaluate)
    evaluate.add_argument("--candidate", required=True)
    evaluate.add_argument("--trigger-report", default=None)

    approve = sub.add_parser("approve")
    add_common(approve)
    approve.add_argument("--candidate", required=True)
    approve.add_argument("--decision", required=True, choices=["approve", "reject"])
    approve.add_argument("--approver", required=True)
    approve.add_argument("--note", default=None)

    apply_cmd = sub.add_parser("apply")
    add_common(apply_cmd)
    apply_cmd.add_argument("--candidate", required=True)

    rollback = sub.add_parser("rollback")
    add_common(rollback)
    rollback.add_argument("--transaction", required=True)

    sync = sub.add_parser("sync")
    add_common(sync)
    mode = sync.add_mutually_exclusive_group()
    mode.add_argument("--check", action="store_true")
    mode.add_argument("--apply", action="store_true")

    args = parser.parse_args(argv)
    project_root = _resolve_project_root(args.project_root)
    canonical_home = core.canonical_home_default(args.canonical_home)
    state_root = core.resolve_state_root(skill_name, project_root, args.state_dir)

    try:
        if args.command == "record":
            if args.input == "-":
                observation = json.loads(sys.stdin.read())
            else:
                observation = json.loads(Path(args.input).read_text(encoding="utf8"))
            if not isinstance(observation, dict):
                raise core.InputError("record --input must be exactly one JSON object")
            result = core.record_candidate(
                observation,
                skill_root=SKILL_ROOT,
                project_root=project_root,
                state_root=state_root,
            )
        elif args.command == "stage":
            result = core.stage_candidate(
                args.candidate,
                canonical_root=canonical_home / skill_name,
                project_root=project_root,
                state_root=state_root,
                deployment_entries=cfg["deploymentEntries"],
                excludes=cfg.get("syncExcludes"),
            )
        elif args.command == "evaluate":
            trigger = None
            if args.trigger_report:
                try:
                    trigger = core.validate_trigger_report(
                        json.loads(Path(args.trigger_report).read_text(encoding="utf8")),
                        skill_name=skill_name,
                    )
                except (OSError, json.JSONDecodeError, core.InputError, core.PolicyError) as exc:
                    raise core.InputError(f"invalid trigger report: {exc}") from exc
            result = core.evaluate_candidate(
                args.candidate,
                config=cfg,
                canonical_home=canonical_home,
                project_root=project_root,
                state_root=state_root,
                trigger_report=trigger,
            )
        elif args.command == "approve":
            result = core.record_approval(
                args.candidate,
                approver=args.approver,
                decision=args.decision,
                note=args.note,
                state_root=state_root,
            )
        elif args.command == "apply":
            result = core.apply_candidate(
                args.candidate,
                config=cfg,
                canonical_home=canonical_home,
                project_root=project_root,
                state_root=state_root,
            )
        elif args.command == "rollback":
            result = core.rollback_transaction(
                args.transaction,
                canonical_home=canonical_home,
                project_root=project_root,
                state_root=state_root,
                excludes=cfg.get("syncExcludes"),
            )
        elif args.command == "sync":
            result = core.sync_deployment(
                config=cfg,
                canonical_home=canonical_home,
                project_root=project_root,
                state_root=state_root,
                apply=bool(args.apply),
            )
        else:
            parser.error("unknown command")
            return 2
        _emit(result)
        return 0
    except core.InputError as exc:
        print(f"input error: {exc}", file=sys.stderr)
        return 2
    except core.PolicyError as exc:
        print(f"policy refusal: {exc}", file=sys.stderr)
        return 3
    except core.ConflictError as exc:
        print(f"conflict: {exc}", file=sys.stderr)
        return 4
    except core.ApplyFailed as exc:
        print(f"apply failed: {exc}", file=sys.stderr)
        return 5


if __name__ == "__main__":
    raise SystemExit(main())

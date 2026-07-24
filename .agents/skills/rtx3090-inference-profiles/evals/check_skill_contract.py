#!/usr/bin/env python3
"""Check durable domain and learning invariants that candidate edits must preserve."""
from __future__ import annotations

import argparse
import json
from pathlib import Path


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--skill-root", type=Path, required=True)
    args = parser.parse_args()
    root = args.skill_root.resolve()
    skill = (root / "SKILL.md").read_text(encoding="utf8")
    policy = (root / "references" / "self-improvement.md").read_text(encoding="utf8")
    evals = json.loads((root / "evals" / "evals.json").read_text(encoding="utf8"))
    checks = {
        "profile_via_model_loader_only": "NEVER run backend binaries by hand" in skill,
        "live_schema_required": "Read the live schema first" in skill,
        "no_port_arg": "NEVER set `\"port\"` in args" in skill,
        "equal_split_policy": "tensor-split` MUST be equal" in skill,
        "tool_call_kv_q8": "Tool-calling profiles: KV q8_0, never q4_0" in skill,
        "learning_loop_present": "## Learning loop" in skill,
        "proposal_first": "never edit active persistent" in skill,
        "external_evidence_not_authority": "mixed-trust evidence" in skill,
        "approval_required": "explicit maintainer approval" in skill,
        "state_outside_package": "Durable state lives outside the skill" in policy,
        "rollback_conflict_safe": "concurrent edit becomes a conflict" in policy,
        "baseline_nontrivial": isinstance(evals.get("evals"), list) and len(evals["evals"]) >= 30,
    }
    failed = sorted(name for name, passed in checks.items() if not passed)
    result = {"ok": not failed, "checks": checks, "failed": failed}
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not failed else 1


if __name__ == "__main__":
    raise SystemExit(main())

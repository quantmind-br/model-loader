"""End-to-end control flow: evaluation gates, approval, dual-copy apply, rollback."""
from __future__ import annotations

import json
import shutil
import tempfile
import unittest
from pathlib import Path

import sys as _sys
_sys.path.insert(0, str(Path(__file__).resolve().parent))
from _fixture import base_observation, load_core, make_canonical

core = load_core()


class FlowBase(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp(prefix="si-flow-"))
        self.addCleanup(lambda: shutil.rmtree(self.tmp, ignore_errors=True))
        self.home, self.cfg = make_canonical(self.tmp)
        self.proj = self.tmp / "proj"
        self.proj.mkdir()
        self.sr = core.resolve_state_root("alpha-skill", self.proj, str(self.tmp / "state"))
        self.skill = self.home / "alpha-skill"

    def record(self, **over):
        return core.record_candidate(base_observation(**over), skill_root=self.skill,
                                     project_root=self.proj, state_root=self.sr)

    def stage(self, cid):
        return core.stage_candidate(cid, canonical_root=self.skill, project_root=self.proj,
                                    state_root=self.sr, deployment_entries=self.cfg["deploymentEntries"],
                                    excludes=self.cfg["syncExcludes"])

    def change_note(self, worktree, text="\nadded line\n"):
        note = Path(worktree) / "references" / "note.md"
        note.write_text(note.read_text() + text)

    def evaluate(self, cid, trigger=None):
        return core.evaluate_candidate(cid, config=self.cfg, canonical_home=self.home,
                                       project_root=self.proj, state_root=self.sr, trigger_report=trigger)


class DualCopyFlowTest(FlowBase):
    def test_approved_apply_updates_both_copies_and_rollback_restores_them(self) -> None:
        c = self.record()
        st = self.stage(c["id"])
        self.change_note(st["worktree"])
        ev = self.evaluate(c["id"])
        self.assertTrue(ev["verified"], ev["reasons"])
        core.record_approval(c["id"], approver="maintainer", decision="approve", note="ok", state_root=self.sr)
        res = core.apply_candidate(c["id"], config=self.cfg, canonical_home=self.home,
                                   project_root=self.proj, state_root=self.sr)
        self.assertEqual(res["state"], "applied")
        ex = self.cfg["syncExcludes"]
        canon_h = core.curated_tree_hash(self.home / "alpha-skill", ex)
        proj_h = core.curated_tree_hash(self.proj / ".agents" / "skills" / "alpha-skill", ex)
        self.assertEqual(canon_h, proj_h)
        self.assertEqual(res["state"], "applied")
        # rollback
        rb = core.rollback_transaction(res["transaction_id"], canonical_home=self.home,
                                       project_root=self.proj, state_root=self.sr, excludes=ex)
        self.assertEqual(rb["state"], "rolled-back")
        self.assertNotIn("added line", (self.home / "alpha-skill" / "references" / "note.md").read_text())
        self.assertFalse((self.proj / ".agents" / "skills" / "alpha-skill").exists())


class EvaluationGateTests(FlowBase):
    def test_undeclared_change_rejected(self) -> None:
        c = self.record()
        st = self.stage(c["id"])
        # change a file NOT in proposed_changes
        (Path(st["worktree"]) / "capabilities.json").write_text(
            json.dumps({"schemaVersion": 1, "capabilities": [], "x": 1}) + "\n")
        ev = self.evaluate(c["id"])
        self.assertFalse(ev["verified"])
        self.assertTrue(any("undeclared" in r for r in ev["reasons"]))

    def test_untrusted_only_cannot_verify(self) -> None:
        c = self.record(source_trust="external-untrusted", evidence=[])
        st = self.stage(c["id"])
        self.change_note(st["worktree"])
        ev = self.evaluate(c["id"])
        self.assertFalse(ev["verified"])
        self.assertTrue(any("corroboration" in r for r in ev["reasons"]))

    def test_external_with_project_corroboration_can_verify(self) -> None:
        c = self.record(source_trust="external-untrusted",
                        evidence=[{"ref": "evidence/x", "sha256": "0" * 64, "source_trust": "project-owned"}])
        st = self.stage(c["id"])
        self.change_note(st["worktree"])
        ev = self.evaluate(c["id"])
        self.assertTrue(ev["verified"], ev["reasons"])

    def test_name_description_change_requires_trigger(self) -> None:
        c = self.record(proposed_changes=[{"path": "SKILL.md", "operation": "replace"}])
        st = self.stage(c["id"])
        skill_md = Path(st["worktree"]) / "SKILL.md"
        skill_md.write_text(skill_md.read_text().replace("fixture skill for tests", "changed description here"))
        ev = self.evaluate(c["id"])
        self.assertFalse(ev["verified"])
        self.assertTrue(any("trigger report" in r for r in ev["reasons"]))
        valid_trigger = {
            "schemaVersion": 1,
            "skill": "alpha-skill",
            "cases": [
                {"id": f"p{i}", "prompt": f"positive {i}", "should": True, "new": True}
                for i in range(3)
            ] + [
                {"id": f"n{i}", "prompt": f"negative {i}", "should": False, "new": False}
                for i in range(3)
            ],
            "summary": {
                "noRegression": True,
                "positiveCasesRetainedByNew": 3,
                "positiveCasesTotal": 3,
                "negativeCasesRejectedByNew": 3,
                "negativeCasesTotal": 3,
            },
        }
        ev2 = self.evaluate(c["id"], trigger=valid_trigger)
        self.assertTrue(ev2["checks"]["trigger_report_valid"])
        self.assertNotIn("name/description change requires a valid no-regression trigger report", ev2["reasons"])

    def test_fake_trigger_report_cannot_unlock_frontmatter_change(self) -> None:
        c = self.record(proposed_changes=[{"path": "SKILL.md", "operation": "replace"}])
        st = self.stage(c["id"])
        skill_md = Path(st["worktree"]) / "SKILL.md"
        skill_md.write_text(skill_md.read_text().replace("fixture skill for tests", "changed description here"))
        ev = self.evaluate(c["id"], trigger={"x": 1})
        self.assertFalse(ev["checks"]["trigger_report_valid"])
        self.assertTrue(any("invalid no-regression trigger report" in reason for reason in ev["reasons"]))

    def test_executable_change_requires_focused_command(self) -> None:
        c = self.record(proposed_changes=[{"path": "scripts/check.py", "operation": "replace"}],
                        evaluation_plan="run behavioral only")
        st = self.stage(c["id"])
        scripts = Path(st["worktree"]) / "scripts"
        scripts.mkdir(exist_ok=True)
        (scripts / "check.py").write_text("import sys\nprint('changed')\nsys.exit(0)\n")
        ev = self.evaluate(c["id"])
        self.assertTrue(any("executable-script change requires" in r for r in ev["reasons"]))

    def test_missing_approval_blocks_apply(self) -> None:
        c = self.record()
        st = self.stage(c["id"])
        self.change_note(st["worktree"])
        self.evaluate(c["id"])
        with self.assertRaises(core.PolicyError):
            core.apply_candidate(c["id"], config=self.cfg, canonical_home=self.home,
                                 project_root=self.proj, state_root=self.sr)

    def test_symlink_in_worktree_rejected(self) -> None:
        c = self.record()
        st = self.stage(c["id"])
        self.change_note(st["worktree"])
        (Path(st["worktree"]) / "references" / "evil-link").symlink_to("/etc/passwd")
        with self.assertRaises(core.PolicyError):
            self.evaluate(c["id"])

    def test_binary_change_rejected(self) -> None:
        c = self.record(proposed_changes=[{"path": "references/note.md", "operation": "replace"}])
        st = self.stage(c["id"])
        (Path(st["worktree"]) / "references" / "note.md").write_bytes(b"\xff\xfe\x00\x01binary")
        with self.assertRaises((core.PolicyError, core.InputError)):
            self.evaluate(c["id"])


class ApprovalTests(FlowBase):
    def test_reject_marks_rejected(self) -> None:
        c = self.record()
        st = self.stage(c["id"])
        self.change_note(st["worktree"])
        self.evaluate(c["id"])
        core.record_approval(c["id"], approver="m", decision="reject", note="no", state_root=self.sr)
        cand = core.load_candidate(self.sr, c["id"])
        self.assertEqual(cand["status"], "rejected")

    def test_cannot_approve_unverified(self) -> None:
        c = self.record()  # not evaluated → still "candidate"
        with self.assertRaises(core.PolicyError):
            core.record_approval(c["id"], approver="m", decision="approve", note="", state_root=self.sr)


if __name__ == "__main__":
    unittest.main()

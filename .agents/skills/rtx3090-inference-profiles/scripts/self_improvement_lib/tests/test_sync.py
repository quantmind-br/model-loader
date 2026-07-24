"""Sync check/apply, deployment exclusions, and rollback conflict refusal."""
from __future__ import annotations

import shutil
import tempfile
import unittest
from pathlib import Path

import sys as _sys
_sys.path.insert(0, str(Path(__file__).resolve().parent))
from _fixture import base_observation, load_core, make_canonical

core = load_core()


class SyncTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp(prefix="si-sync-"))
        self.addCleanup(lambda: shutil.rmtree(self.tmp, ignore_errors=True))
        self.home, self.cfg = make_canonical(self.tmp)
        self.proj = self.tmp / "proj"
        self.proj.mkdir()
        self.sr = core.resolve_state_root("alpha-skill", self.proj, str(self.tmp / "state"))

    def test_check_reports_missing_then_apply_creates_in_sync(self) -> None:
        report = core.sync_deployment(config=self.cfg, canonical_home=self.home,
                                      project_root=self.proj, state_root=self.sr, apply=False)
        self.assertEqual([e["entry"] for e in report["entries"]], ["alpha-skill"])
        self.assertTrue(all(e["status"] == "missing" for e in report["entries"]))
        applied = core.sync_deployment(config=self.cfg, canonical_home=self.home,
                                       project_root=self.proj, state_root=self.sr, apply=True)
        self.assertTrue(all(e["status"] == "in-sync" for e in applied["entries"]))
        check = core.sync_deployment(config=self.cfg, canonical_home=self.home,
                                     project_root=self.proj, state_root=self.sr, apply=False)
        self.assertTrue(all(e["status"] == "in-sync" for e in check["entries"]))

    def test_check_is_read_only(self) -> None:
        core.sync_deployment(config=self.cfg, canonical_home=self.home,
                             project_root=self.proj, state_root=self.sr, apply=False)
        self.assertFalse((self.proj / ".agents" / "skills").exists())

    def test_drift_refuses_apply(self) -> None:
        core.sync_deployment(config=self.cfg, canonical_home=self.home,
                             project_root=self.proj, state_root=self.sr, apply=True)
        # tamper a project copy → drift
        drifted = self.proj / ".agents" / "skills" / "alpha-skill" / "references" / "note.md"
        drifted.write_text("locally drifted content\n")
        report = core.sync_deployment(config=self.cfg, canonical_home=self.home,
                                      project_root=self.proj, state_root=self.sr, apply=False)
        self.assertIn("project-copy-drift", [e["status"] for e in report["entries"]])
        with self.assertRaises(core.ConflictError):
            core.sync_deployment(config=self.cfg, canonical_home=self.home,
                                 project_root=self.proj, state_root=self.sr, apply=True)
        # drifted content preserved, not overwritten
        self.assertEqual(drifted.read_text(), "locally drifted content\n")

    def test_custom_excludes_keep_mandatory_invariants(self) -> None:
        merged = core._norm_excludes(["custom-cache/"])
        for required in ("node_modules/", ".env", ".agent-state/", "custom-cache/"):
            self.assertIn(required, merged)

    def test_deployment_excludes_nested_agents(self) -> None:
        # A nested .agents dir in the canonical skill must not be copied.
        (self.home / "alpha-skill" / ".agents" / "skills").mkdir(parents=True)
        (self.home / "alpha-skill" / ".agents" / "skills" / "junk.txt").write_text("no")
        core.sync_deployment(config=self.cfg, canonical_home=self.home,
                             project_root=self.proj, state_root=self.sr, apply=True)
        self.assertFalse((self.proj / ".agents" / "skills" / "alpha-skill" / ".agents").exists())

    def test_apply_provisions_runtime_deps_before_validation(self) -> None:
        runtime = self.home / "alpha-skill" / "node_modules" / "fixture"
        runtime.mkdir(parents=True)
        (runtime / "ready.txt").write_text("ready\n")
        check = self.home / "alpha-skill" / "check.py"
        check.write_text(
            "from pathlib import Path\n"
            "import argparse\n"
            "p=argparse.ArgumentParser(); p.add_argument('--root'); a=p.parse_args()\n"
            "raise SystemExit(0 if (Path(a.root)/'node_modules/fixture/ready.txt').is_file() else 1)\n"
        )
        core.sync_deployment(config=self.cfg, canonical_home=self.home,
                             project_root=self.proj, state_root=self.sr, apply=True)
        deployed = self.proj / ".agents" / "skills" / "alpha-skill" / "node_modules"
        self.assertTrue(deployed.is_symlink())
        self.assertEqual(deployed.resolve(), runtime.parent.resolve())

    def test_failed_post_validation_compensates_new_install(self) -> None:
        self.cfg["validationCommands"] = [["python3", "-c", "raise SystemExit(9)"]]
        with self.assertRaises(core.ApplyFailed):
            core.sync_deployment(config=self.cfg, canonical_home=self.home,
                                 project_root=self.proj, state_root=self.sr, apply=True)
        self.assertFalse((self.proj / ".agents" / "skills" / "alpha-skill").exists())

class RollbackConflictTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp(prefix="si-rbc-"))
        self.addCleanup(lambda: shutil.rmtree(self.tmp, ignore_errors=True))
        self.home, self.cfg = make_canonical(self.tmp)
        self.proj = self.tmp / "proj"
        self.proj.mkdir()
        self.sr = core.resolve_state_root("alpha-skill", self.proj, str(self.tmp / "state"))
        self.skill = self.home / "alpha-skill"

    def _apply(self):
        c = core.record_candidate(base_observation(), skill_root=self.skill,
                                  project_root=self.proj, state_root=self.sr)
        st = core.stage_candidate(c["id"], canonical_root=self.skill, project_root=self.proj,
                                  state_root=self.sr, deployment_entries=self.cfg["deploymentEntries"],
                                  excludes=self.cfg["syncExcludes"])
        note = Path(st["worktree"]) / "references" / "note.md"
        note.write_text(note.read_text() + "\napplied change\n")
        core.evaluate_candidate(c["id"], config=self.cfg, canonical_home=self.home,
                                project_root=self.proj, state_root=self.sr)
        core.record_approval(c["id"], approver="m", decision="approve", note="", state_root=self.sr)
        return core.apply_candidate(c["id"], config=self.cfg, canonical_home=self.home,
                                    project_root=self.proj, state_root=self.sr)

    def test_concurrent_edit_refuses_rollback(self) -> None:
        res = self._apply()
        # concurrently edit a deployed postimage
        edited = self.home / "alpha-skill" / "references" / "note.md"
        edited.write_text(edited.read_text() + "\nconcurrent human edit\n")
        with self.assertRaises(core.ConflictError):
            core.rollback_transaction(res["transaction_id"], canonical_home=self.home,
                                      project_root=self.proj, state_root=self.sr,
                                      excludes=self.cfg["syncExcludes"])
        # concurrent edit preserved
        self.assertIn("concurrent human edit", edited.read_text())


if __name__ == "__main__":
    unittest.main()

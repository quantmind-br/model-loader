"""State, id, sanitization, evidence, trust, and transition tests."""
from __future__ import annotations

import json
import os
import tempfile
import unittest
from pathlib import Path

import sys as _sys
_sys.path.insert(0, str(Path(__file__).resolve().parent))
from _fixture import base_observation, load_core, make_canonical

core = load_core()


class StateResolutionTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp(prefix="si-core-"))
        self.addCleanup(lambda: __import__("shutil").rmtree(self.tmp, ignore_errors=True))

    def test_precedence_explicit_over_env(self) -> None:
        explicit = self.tmp / "explicit"
        os.environ["AGENT_SKILL_STATE_DIR"] = str(self.tmp / "env")
        try:
            root = core.resolve_state_root("s", self.tmp / "proj", str(explicit))
        finally:
            del os.environ["AGENT_SKILL_STATE_DIR"]
        self.assertTrue(str(root).startswith(str(explicit.resolve())))

    def test_env_over_xdg(self) -> None:
        os.environ["AGENT_SKILL_STATE_DIR"] = str(self.tmp / "env")
        try:
            root = core.resolve_state_root("s", self.tmp / "proj")
        finally:
            del os.environ["AGENT_SKILL_STATE_DIR"]
        self.assertIn("env", str(root))

    def test_project_id_is_hmac_not_path(self) -> None:
        base = self.tmp / "state"
        pid = core.project_id(self.tmp / "proj", base)
        self.assertEqual(len(pid), 24)
        self.assertNotIn(str(self.tmp), pid)
        # deterministic for same path + secret
        self.assertEqual(pid, core.project_id(self.tmp / "proj", base))
        # secret file is mode 0600
        secret = base / "project-id.secret"
        self.assertEqual(oct(secret.stat().st_mode)[-3:], "600")

    def test_state_namespaced_subdirs(self) -> None:
        root = core.resolve_state_root("alpha", self.tmp / "proj", str(self.tmp / "state"))
        for sub in ("candidates", "occurrences", "evidence", "snapshots", "worktrees",
                    "proposals", "approvals", "transactions", "archive"):
            self.assertTrue((root / sub).is_dir(), sub)


class RecordTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp(prefix="si-rec-"))
        self.addCleanup(lambda: __import__("shutil").rmtree(self.tmp, ignore_errors=True))
        self.home, self.cfg = make_canonical(self.tmp)
        self.sr = core.resolve_state_root("alpha-skill", self.tmp / "proj", str(self.tmp / "state"))

    def _record(self, **over):
        return core.record_candidate(base_observation(**over), skill_root=self.home / "alpha-skill",
                                     project_root=self.tmp / "proj", state_root=self.sr)

    def test_occurrence_count_derived_from_files(self) -> None:
        c1 = self._record()
        self.assertEqual(c1["occurrence_count"], 1)
        c2 = self._record()  # same pattern_key/scope/skill → reuse
        self.assertEqual(c2["id"], c1["id"])
        self.assertEqual(c2["occurrence_count"], 2)
        occ = list((self.sr / "occurrences").glob("OCC-*.json"))
        self.assertEqual(len(occ), 2)

    def test_distinct_pattern_key_new_candidate(self) -> None:
        a = self._record(pattern_key="k.one")
        b = self._record(pattern_key="k.two")
        self.assertNotEqual(a["id"], b["id"])

    def test_sanitization_rejects_bearer_token(self) -> None:
        with self.assertRaises(core.PolicyError):
            self._record(observation="here is Bearer abcdef1234567890 leaked")

    def test_sanitization_rejects_pem(self) -> None:
        with self.assertRaises(core.PolicyError):
            self._record(provenance="-----BEGIN PRIVATE KEY-----\nx\n")

    def test_sanitization_rejects_credential_assignment(self) -> None:
        with self.assertRaises(core.PolicyError):
            self._record(hypothesis="set password: hunter2 in the config")

    def test_path_prefix_sanitized(self) -> None:
        c = self._record(observation=f"failure at {self.tmp}/proj/foo happened")
        self.assertNotIn(str(self.tmp), c["observation"])
        self.assertIn("<", c["observation"])

    def test_pattern_key_format_enforced(self) -> None:
        with self.assertRaises(core.InputError):
            self._record(pattern_key="Bad Key!")

    def test_evidence_confinement(self) -> None:
        with self.assertRaises(core.InputError):
            self._record(evidence=[{"ref": "/abs/evil", "sha256": "0" * 64, "source_trust": "project-owned"}])
        with self.assertRaises(core.InputError):
            self._record(evidence=[{"ref": "../escape", "sha256": "0" * 64, "source_trust": "project-owned"}])

    def test_oversized_field_rejected(self) -> None:
        with self.assertRaises(core.InputError):
            self._record(observation="x" * 5000)

    def test_proposed_change_path_confinement(self) -> None:
        for bad in (
            "../peer/x", "shared/x", ".git/config", "package.json", "node_modules/x",
            "README.md", "foo.txt", "random/note.md",
        ):
            with self.assertRaises((core.InputError, core.PolicyError)):
                self._record(proposed_changes=[{"path": bad, "operation": "replace"}])

    def test_curated_change_paths_are_allowed(self) -> None:
        for good in (
            "SKILL.md", "capabilities.json", "self-improvement.json",
            "references/note.md", "workflows/audit.md", "scripts/check.py",
            "evals/behavioral.json", "evaluation/check.py", "tools/report.md",
        ):
            candidate = self._record(
                pattern_key="allowed." + good.replace("/", ".").replace("-", ".").lower(),
                proposed_changes=[{"path": good, "operation": "replace"}],
            )
            self.assertEqual(candidate["proposed_changes"][0]["path"], good)
    def test_approval_required_always_true(self) -> None:
        c = self._record()
        self.assertTrue(c["approval_required"])



class TriggerReportTests(unittest.TestCase):
    def valid_report(self) -> dict:
        return {
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

    def test_valid_trigger_report_passes(self) -> None:
        report = self.valid_report()
        self.assertIs(core.validate_trigger_report(report, skill_name="alpha-skill"), report)

    def test_trigger_report_rejects_fake_object(self) -> None:
        with self.assertRaises(core.InputError):
            core.validate_trigger_report({"x": 1}, skill_name="alpha-skill")

    def test_trigger_report_rejects_classification_mismatch(self) -> None:
        report = self.valid_report()
        report["cases"][0]["new"] = False
        with self.assertRaises(core.InputError):
            core.validate_trigger_report(report, skill_name="alpha-skill")

    def test_trigger_report_rejects_forged_summary_counts(self) -> None:
        report = self.valid_report()
        report["summary"]["positiveCasesRetainedByNew"] = 99
        with self.assertRaises(core.InputError):
            core.validate_trigger_report(report, skill_name="alpha-skill")

    def test_trigger_report_rejects_insufficient_coverage(self) -> None:
        report = self.valid_report()
        report["cases"] = report["cases"][:4]
        with self.assertRaises(core.InputError):
            core.validate_trigger_report(report, skill_name="alpha-skill")



class TransitionTests(unittest.TestCase):
    def test_allowed_and_illegal(self) -> None:
        cand = {"status": "candidate"}
        core._transition(cand, "verified")
        self.assertEqual(cand["status"], "verified")
        core._transition(cand, "approved")
        core._transition(cand, "applied")
        self.assertEqual(cand["status"], "applied")
        with self.assertRaises(core.PolicyError):
            core._transition({"status": "candidate"}, "applied")
        with self.assertRaises(core.PolicyError):
            core._transition({"status": "applied"}, "verified")


class SchemaIntegrityTests(unittest.TestCase):
    def test_pinned_hashes_match_files(self) -> None:
        core.verify_schema_integrity()  # must not raise on the shipped tree

    def test_tampered_schema_detected(self) -> None:
        tmp = Path(tempfile.mkdtemp(prefix="si-schema-"))
        self.addCleanup(lambda: __import__("shutil").rmtree(tmp, ignore_errors=True))
        # copy schema dir, tamper one, point core at it
        import shutil
        dst = tmp / "schemas"
        shutil.copytree(core.SCHEMA_DIR, dst)
        (dst / "candidate.schema.json").write_text("{}\n")
        orig = core.SCHEMA_DIR
        core.SCHEMA_DIR = dst
        try:
            with self.assertRaises(core.InputError):
                core.verify_schema_integrity()
        finally:
            core.SCHEMA_DIR = orig


if __name__ == "__main__":
    unittest.main()

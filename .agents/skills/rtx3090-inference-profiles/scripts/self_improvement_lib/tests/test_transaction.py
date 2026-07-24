"""Transaction primitive: two-rename replacement, compensation, locks, conflict."""
from __future__ import annotations

import shutil
import tempfile
import unittest
from pathlib import Path

import sys as _sys
_sys.path.insert(0, str(Path(__file__).resolve().parent))
from _fixture import load_core

core = load_core()
EX = (".git/", "__pycache__/", "*.pyc")


def _mkdir_tree(root: Path, files: dict[str, str]) -> None:
    for rel, content in files.items():
        p = root / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(content, encoding="utf8")


class TransactionTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp(prefix="si-tx-"))
        self.addCleanup(lambda: shutil.rmtree(self.tmp, ignore_errors=True))
        self.state = self.tmp / "state"
        for sub in ("transactions", "archive"):
            (self.state / sub).mkdir(parents=True, exist_ok=True)

    def _target(self, role, dst, src, recognized):
        return {"role": role, "dst": dst, "src": src, "recognized": recognized}

    def test_replaces_existing_nonempty_directories_with_two_renames(self) -> None:
        dst = self.tmp / "dest"
        _mkdir_tree(dst, {"a.txt": "old", "sub/b.txt": "old-b"})
        src = self.tmp / "src"
        _mkdir_tree(src, {"a.txt": "new", "sub/b.txt": "new-b", "c.txt": "added"})
        pre = core.curated_tree_hash(dst, EX)
        tx = core._deploy([self._target("only", dst, src, {pre})],
                          state_root=self.state, tx_id=core._new_id("TX"),
                          candidate_id="t", excludes=EX)
        self.assertEqual(tx["state"], "applied")
        self.assertEqual((dst / "a.txt").read_text(), "new")
        self.assertEqual((dst / "c.txt").read_text(), "added")
        # backup archived (retained) for rollback
        self.assertTrue(tx["targets"][0]["had_backup"])
        self.assertTrue(Path(tx["targets"][0]["backup"]).exists())

    def test_creation_when_target_absent(self) -> None:
        dst = self.tmp / "new-dest"
        src = self.tmp / "src"
        _mkdir_tree(src, {"x.txt": "hi"})
        tx = core._deploy([self._target("only", dst, src, None)],
                          state_root=self.state, tx_id=core._new_id("TX"),
                          candidate_id="t", excludes=EX)
        self.assertEqual(tx["state"], "applied")
        self.assertEqual((dst / "x.txt").read_text(), "hi")
        self.assertIsNone(tx["targets"][0]["preimage_hash"])

    def test_drift_refused_no_overwrite(self) -> None:
        dst = self.tmp / "dest"
        _mkdir_tree(dst, {"a.txt": "drifted"})
        src = self.tmp / "src"
        _mkdir_tree(src, {"a.txt": "new"})
        with self.assertRaises(core.ConflictError):
            core._deploy([self._target("only", dst, src, {"deadbeef"})],
                         state_root=self.state, tx_id=core._new_id("TX"),
                         candidate_id="t", excludes=EX)
        # original preserved
        self.assertEqual((dst / "a.txt").read_text(), "drifted")

    def test_journal_records_and_fsync_ordering(self) -> None:
        dst = self.tmp / "dest"
        _mkdir_tree(dst, {"a.txt": "old"})
        src = self.tmp / "src"
        _mkdir_tree(src, {"a.txt": "new"})
        pre = core.curated_tree_hash(dst, EX)
        tx_id = core._new_id("TX")
        core._deploy([self._target("only", dst, src, {pre})],
                     state_root=self.state, tx_id=tx_id, candidate_id="t", excludes=EX)
        import json
        tx = json.loads((self.state / "transactions" / f"{tx_id}.json").read_text())
        ops = [e["op"] for e in tx["journal"]]
        self.assertIn("begin", ops)
        self.assertIn("backup", ops)
        self.assertIn("activate", ops)
        self.assertLess(ops.index("backup"), ops.index("activate"))

    def test_sorted_parent_locks_multiple_targets(self) -> None:
        # Two targets under different parents; deploy must acquire both and succeed.
        d1 = self.tmp / "p1" / "dst"
        d2 = self.tmp / "p2" / "dst"
        s1 = self.tmp / "s1"; s2 = self.tmp / "s2"
        _mkdir_tree(s1, {"x": "1"}); _mkdir_tree(s2, {"y": "2"})
        tx = core._deploy([self._target("a", d1, s1, None), self._target("b", d2, s2, None)],
                          state_root=self.state, tx_id=core._new_id("TX"),
                          candidate_id="t", excludes=EX)
        self.assertEqual(tx["state"], "applied")

    def test_post_validation_failure_compensates(self) -> None:
        dst = self.tmp / "dest"
        _mkdir_tree(dst, {"a.txt": "old"})
        src = self.tmp / "src"
        _mkdir_tree(src, {"a.txt": "new"})
        pre = core.curated_tree_hash(dst, EX)
        tx_id = core._new_id("TX")
        with self.assertRaises(core.ApplyFailed):
            core._deploy([self._target("only", dst, src, {pre})],
                         state_root=self.state, tx_id=tx_id,
                         candidate_id="t", excludes=EX,
                         post_validate=lambda: (False, {"reason": "forced"}))
        import json
        recorded = json.loads((self.state / "transactions" / f"{tx_id}.json").read_text())
        self.assertEqual(recorded["state"], "rolled-back")
        self.assertEqual((dst / "a.txt").read_text(), "old")

    def test_failed_replacement_restores_runtime_dependencies(self) -> None:
        dst = self.tmp / "dest"
        _mkdir_tree(dst, {"a.txt": "old", "node_modules/fixture/ready.txt": "ready"})
        src = self.tmp / "src"
        _mkdir_tree(src, {"a.txt": "new"})
        pre = core.curated_tree_hash(dst, EX)
        with self.assertRaises(core.ApplyFailed):
            core._deploy([self._target("only", dst, src, {pre})],
                         state_root=self.state, tx_id=core._new_id("TX"),
                         candidate_id="t", excludes=EX,
                         post_validate=lambda: (False, {"reason": "forced"}))
        self.assertEqual((dst / "a.txt").read_text(), "old")
        self.assertEqual((dst / "node_modules/fixture/ready.txt").read_text(), "ready")

    def test_successful_replacement_keeps_runtime_dependencies(self) -> None:
        dst = self.tmp / "dest"
        _mkdir_tree(dst, {"a.txt": "old", "node_modules/fixture/ready.txt": "ready"})
        src = self.tmp / "src"
        _mkdir_tree(src, {"a.txt": "new"})
        pre = core.curated_tree_hash(dst, EX)
        tx = core._deploy([self._target("only", dst, src, {pre})],
                          state_root=self.state, tx_id=core._new_id("TX"),
                          candidate_id="t", excludes=EX)
        self.assertEqual((dst / "node_modules/fixture/ready.txt").read_text(), "ready")
        core.rollback_transaction(tx["transaction_id"], canonical_home=self.tmp,
                                  project_root=self.tmp, state_root=self.state, excludes=EX)
        self.assertEqual((dst / "a.txt").read_text(), "old")
        self.assertEqual((dst / "node_modules/fixture/ready.txt").read_text(), "ready")

    def test_second_staging_failure_cleans_all_stages(self) -> None:
        d1 = self.tmp / "d1"
        d2 = self.tmp / "d2"
        s1 = self.tmp / "s1"
        s2 = self.tmp / "s2"
        _mkdir_tree(s1, {"a": "new1"})
        _mkdir_tree(s2, {"b": "new2"})
        original = core._copy_tree
        calls = {"n": 0}

        def fail_second_stage(src, dst, excludes):
            calls["n"] += 1
            if calls["n"] == 2:
                Path(dst).mkdir(parents=True, exist_ok=True)
                (Path(dst) / "partial").write_text("residue")
                raise OSError("second stage failed")
            return original(src, dst, excludes)

        tx_id = core._new_id("TX")
        core._copy_tree = fail_second_stage
        try:
            with self.assertRaises(core.ApplyFailed):
                core._deploy([self._target("one", d1, s1, None),
                              self._target("two", d2, s2, None)],
                             state_root=self.state, tx_id=tx_id,
                             candidate_id="t", excludes=EX)
        finally:
            core._copy_tree = original
        import json
        recorded = json.loads((self.state / "transactions" / f"{tx_id}.json").read_text())
        self.assertEqual(recorded["state"], "failed")
        self.assertFalse(d1.exists())
        self.assertFalse(d2.exists())
        self.assertFalse(any(self.tmp.rglob("partial")))

    def test_staged_hash_failure_never_activates_target(self) -> None:
        dst = self.tmp / "dest"
        _mkdir_tree(dst, {"a.txt": "old", "node_modules/fixture/ready.txt": "ready"})
        src = self.tmp / "src"
        _mkdir_tree(src, {"a.txt": "new"})
        pre = core.curated_tree_hash(dst, EX)
        original = core.curated_tree_hash

        def fail_on_stage(path, excludes=None):
            if ".stage" in Path(path).name:
                raise OSError("stage hash failed")
            return original(path, excludes)

        tx_id = core._new_id("TX")
        core.curated_tree_hash = fail_on_stage
        try:
            with self.assertRaises(core.ApplyFailed):
                core._deploy([self._target("only", dst, src, {pre})],
                             state_root=self.state, tx_id=tx_id,
                             candidate_id="t", excludes=EX)
        finally:
            core.curated_tree_hash = original
        import json
        recorded = json.loads((self.state / "transactions" / f"{tx_id}.json").read_text())
        self.assertEqual(recorded["state"], "failed")
        self.assertEqual((dst / "a.txt").read_text(), "old")
        self.assertEqual((dst / "node_modules/fixture/ready.txt").read_text(), "ready")

    def test_activation_failure_restores_replacement_backup(self) -> None:
        dst = self.tmp / "dest"
        _mkdir_tree(dst, {"a.txt": "old", "node_modules/fixture/ready.txt": "ready"})
        src = self.tmp / "src"
        _mkdir_tree(src, {"a.txt": "new"})
        pre = core.curated_tree_hash(dst, EX)
        import os as _os
        original = _os.rename

        def fail_activation(a, b, *args, **kwargs):
            if ".stage" in str(a) and Path(b) == dst:
                raise OSError("activation failed")
            return original(a, b, *args, **kwargs)

        _os.rename = fail_activation
        try:
            with self.assertRaises(core.ApplyFailed):
                core._deploy([self._target("only", dst, src, {pre})],
                             state_root=self.state, tx_id=core._new_id("TX"),
                             candidate_id="t", excludes=EX)
        finally:
            _os.rename = original
        self.assertEqual((dst / "a.txt").read_text(), "old")
        self.assertEqual((dst / "node_modules/fixture/ready.txt").read_text(), "ready")

    def test_backup_journal_failure_restores_replacement_backup(self) -> None:
        dst = self.tmp / "dest"
        _mkdir_tree(dst, {"a.txt": "old", "node_modules/fixture/ready.txt": "ready"})
        src = self.tmp / "src"
        _mkdir_tree(src, {"a.txt": "new"})
        pre = core.curated_tree_hash(dst, EX)
        original = core._append_journal

        def fail_backup_journal(tx_path, tx, entry):
            if entry.get("op") == "backup":
                raise OSError("journal failed")
            return original(tx_path, tx, entry)

        core._append_journal = fail_backup_journal
        try:
            with self.assertRaises(core.ApplyFailed):
                core._deploy([self._target("only", dst, src, {pre})],
                             state_root=self.state, tx_id=core._new_id("TX"),
                             candidate_id="t", excludes=EX)
        finally:
            core._append_journal = original
        self.assertEqual((dst / "a.txt").read_text(), "old")
        self.assertEqual((dst / "node_modules/fixture/ready.txt").read_text(), "ready")

    def test_runtime_copy_failure_compensates_active_target(self) -> None:
        dst = self.tmp / "dest"
        _mkdir_tree(dst, {"a.txt": "old", "node_modules/fixture/ready.txt": "ready"})
        src = self.tmp / "src"
        _mkdir_tree(src, {"a.txt": "new"})
        pre = core.curated_tree_hash(dst, EX)
        original = core._carry_runtime_deps
        core._carry_runtime_deps = lambda *_args: (_ for _ in ()).throw(OSError("copy failed"))
        try:
            with self.assertRaises(core.ApplyFailed):
                core._deploy([self._target("only", dst, src, {pre})],
                             state_root=self.state, tx_id=core._new_id("TX"),
                             candidate_id="t", excludes=EX)
        finally:
            core._carry_runtime_deps = original
        self.assertEqual((dst / "a.txt").read_text(), "old")
        self.assertEqual((dst / "node_modules/fixture/ready.txt").read_text(), "ready")

    def test_second_archive_failure_compensates_all_targets(self) -> None:
        d1 = self.tmp / "d1"
        d2 = self.tmp / "d2"
        s1 = self.tmp / "s1"
        s2 = self.tmp / "s2"
        _mkdir_tree(d1, {"a": "old1", "node_modules/x/ready": "one"})
        _mkdir_tree(d2, {"b": "old2", "node_modules/y/ready": "two"})
        _mkdir_tree(s1, {"a": "new1"})
        _mkdir_tree(s2, {"b": "new2"})
        pre1 = core.curated_tree_hash(d1, EX)
        pre2 = core.curated_tree_hash(d2, EX)
        original = core._copy_tree
        archive_calls = {"n": 0}

        def fail_second_archive(src, dst, excludes):
            if self.state / "archive" in Path(dst).parents:
                archive_calls["n"] += 1
                if archive_calls["n"] == 2:
                    Path(dst).mkdir(parents=True, exist_ok=True)
                    (Path(dst) / "partial").write_text("residue")
                    raise OSError("second archive failed")
            return original(src, dst, excludes)

        core._copy_tree = fail_second_archive
        try:
            with self.assertRaises(core.ApplyFailed):
                core._deploy([self._target("one", d1, s1, {pre1}),
                              self._target("two", d2, s2, {pre2})],
                             state_root=self.state, tx_id=core._new_id("TX"),
                             candidate_id="t", excludes=EX)
        finally:
            core._copy_tree = original
        self.assertEqual((d1 / "a").read_text(), "old1")
        self.assertEqual((d2 / "b").read_text(), "old2")
        self.assertEqual((d1 / "node_modules/x/ready").read_text(), "one")
        self.assertEqual((d2 / "node_modules/y/ready").read_text(), "two")
        self.assertFalse(any((self.state / "archive").rglob("partial")))

    def test_local_backup_cleanup_failure_keeps_applied_transaction(self) -> None:
        dst = self.tmp / "dest"
        src = self.tmp / "src"
        _mkdir_tree(dst, {"a": "old"})
        _mkdir_tree(src, {"a": "new"})
        pre = core.curated_tree_hash(dst, EX)
        original = shutil.rmtree

        def fail_local_backup(path, *args, **kwargs):
            if ".backup" in Path(path).name and self.state / "archive" not in Path(path).parents:
                raise OSError("cleanup failed")
            return original(path, *args, **kwargs)

        shutil.rmtree = fail_local_backup
        try:
            tx = core._deploy([self._target("only", dst, src, {pre})],
                              state_root=self.state, tx_id=core._new_id("TX"),
                              candidate_id="t", excludes=EX)
        finally:
            shutil.rmtree = original
        self.assertEqual(tx["state"], "applied")
        self.assertEqual((dst / "a").read_text(), "new")
        self.assertTrue(Path(tx["targets"][0]["backup"]).exists())
        core.rollback_transaction(tx["transaction_id"], canonical_home=self.tmp,
                                  project_root=self.tmp, state_root=self.state, excludes=EX)
        self.assertEqual((dst / "a").read_text(), "old")

    def test_compensation_restore_rename_failure_preserves_postimage(self) -> None:
        dst = self.tmp / "dest"
        src = self.tmp / "src"
        _mkdir_tree(dst, {"a": "old"})
        _mkdir_tree(src, {"a": "new"})
        pre = core.curated_tree_hash(dst, EX)
        import os as _os
        original = _os.rename

        def fail_backup_restore(a, b, *args, **kwargs):
            if ".backup" in Path(a).name and Path(b) == dst:
                raise OSError("restore failed")
            return original(a, b, *args, **kwargs)

        _os.rename = fail_backup_restore
        tx_id = core._new_id("TX")
        try:
            with self.assertRaises(core.ApplyFailed):
                core._deploy([self._target("only", dst, src, {pre})],
                             state_root=self.state, tx_id=tx_id,
                             candidate_id="t", excludes=EX,
                             post_validate=lambda: (False, {"reason": "forced"}))
        finally:
            _os.rename = original
        import json
        recorded = json.loads((self.state / "transactions" / f"{tx_id}.json").read_text())
        self.assertEqual(recorded["state"], "conflict")
        self.assertEqual((dst / "a").read_text(), "new")
        self.assertTrue(any(p.name.endswith(".backup") for p in dst.parent.iterdir()))

    def test_transaction_commit_failure_compensates_with_local_backup(self) -> None:
        dst = self.tmp / "dest"
        src = self.tmp / "src"
        _mkdir_tree(dst, {"a": "old", "node_modules/x/ready": "ready"})
        _mkdir_tree(src, {"a": "new"})
        pre = core.curated_tree_hash(dst, EX)
        original = core._dump_json_atomic

        def fail_applied_commit(path, obj):
            if obj.get("state") == "applied":
                raise OSError("commit failed")
            return original(path, obj)

        tx_id = core._new_id("TX")
        core._dump_json_atomic = fail_applied_commit
        try:
            with self.assertRaises(core.ApplyFailed):
                core._deploy([self._target("only", dst, src, {pre})],
                             state_root=self.state, tx_id=tx_id,
                             candidate_id="t", excludes=EX)
        finally:
            core._dump_json_atomic = original
        import json
        recorded = json.loads((self.state / "transactions" / f"{tx_id}.json").read_text())
        self.assertEqual(recorded["state"], "rolled-back")
        self.assertEqual((dst / "a").read_text(), "old")
        self.assertEqual((dst / "node_modules/x/ready").read_text(), "ready")
        self.assertFalse((self.state / "archive" / tx_id).exists())
        residues = [p for p in self.tmp.rglob("*")
                    if tx_id in p.name and (p.name.endswith(".backup") or p.name.endswith(".stage"))]
        self.assertEqual(residues, [])

    def test_partial_write_reverse_compensation(self) -> None:
        # First target activates; the second target's staged dir activation fails,
        # so reverse compensation restores the first target.
        import os as _os
        d1 = self.tmp / "d1"
        _mkdir_tree(d1, {"a": "old1"})
        s1 = self.tmp / "s1"
        _mkdir_tree(s1, {"a": "new1"})
        parent2 = self.tmp / "ro_parent"
        parent2.mkdir()
        d2 = parent2 / "dst"
        s2 = self.tmp / "s2"
        _mkdir_tree(s2, {"b": "new2"})
        pre1 = core.curated_tree_hash(d1, EX)
        orig_rename = _os.rename

        def flaky_rename(a, b, *args, **kw):
            if str(b).endswith("/dst") and "ro_parent" in str(b):
                raise OSError("simulated activation failure")
            return orig_rename(a, b, *args, **kw)

        _os.rename = flaky_rename
        try:
            with self.assertRaises(core.ApplyFailed):
                core._deploy([self._target("a", d1, s1, {pre1}),
                              self._target("b", d2, s2, None)],
                             state_root=self.state, tx_id=core._new_id("TX"),
                             candidate_id="t", excludes=EX)
        finally:
            _os.rename = orig_rename
        self.assertEqual((d1 / "a").read_text(), "old1")
        self.assertFalse(any(p.name.endswith(".stage") for p in self.tmp.rglob("*")))

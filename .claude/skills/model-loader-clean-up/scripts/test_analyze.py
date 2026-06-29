import os, tempfile, unittest
import json
import analyze as A


class HelpersTest(unittest.TestCase):
    def test_human_readable_sizes(self):
        self.assertEqual(A.human(0), "0.0B")
        self.assertEqual(A.human(1024), "1.0KiB")
        self.assertEqual(A.human(1536), "1.5KiB")
        self.assertEqual(A.human(1024 ** 3), "1.0GiB")

    def test_multipart_group_parsing(self):
        self.assertEqual(
            A.multipart_group("Model-Q4_K_M-00002-of-00005.gguf"),
            ("Model-Q4_K_M", 2, 5),
        )
        self.assertIsNone(A.multipart_group("Model-Q4_K_M.gguf"))
        self.assertIsNone(A.multipart_group("notes.txt"))

    def test_real_size_counts_files_once_skips_symlinks(self):
        d = tempfile.mkdtemp()
        with open(os.path.join(d, "a.bin"), "wb") as f:
            f.write(b"x" * 100)
        sub = os.path.join(d, "sub")
        os.mkdir(sub)
        with open(os.path.join(sub, "b.bin"), "wb") as f:
            f.write(b"y" * 50)
        os.symlink(os.path.join(sub, "b.bin"), os.path.join(d, "link.bin"))
        self.assertEqual(A.real_size(d), 150)
        self.assertEqual(A.real_size(os.path.join(d, "link.bin")), 0)

    def test_candidate_dataclass_defaults(self):
        c = A.Candidate(path="/p", size=10, mtime=1.0,
                        category=A.CAT_ORPHAN, reason="r", delete_unit="/p")
        self.assertFalse(c.blocked)
        self.assertEqual(c.blocked_by, "")


def _write_profile(d, pid, **kw):
    obj = {"schemaVersion": 3, "id": pid, "model": kw.get("model", "")}
    if "args" in kw:
        obj["args"] = kw["args"]
    if "extraArgs" in kw:
        obj["extraArgs"] = kw["extraArgs"]
    with open(os.path.join(d, pid + ".json"), "w") as f:
        json.dump(obj, f)


class ClosureTest(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.mkdtemp()
        self.profiles = os.path.join(self.root, "profiles")
        self.store = os.path.join(self.root, "store")
        os.makedirs(self.profiles)
        # repo with a real model + a draft symlink into MTP/
        repo = os.path.join(self.store, "unsloth", "gemma-GGUF")
        os.makedirs(os.path.join(repo, "MTP"))
        self.model = os.path.join(repo, "model-Q4.gguf")
        open(self.model, "wb").write(b"GGUF" + b"0" * 10)
        real_draft = os.path.join(repo, "MTP", "draft-Q8-MTP.gguf")
        open(real_draft, "wb").write(b"GGUF" + b"1" * 10)
        self.draft_link = os.path.join(repo, "draft-Q8-MTP.gguf")
        os.symlink(os.path.join("MTP", "draft-Q8-MTP.gguf"), self.draft_link)
        self.real_draft = real_draft
        # a directory-model (safetensors)
        self.dirmodel = os.path.join(self.store, "cyankiwi", "AWQ")
        os.makedirs(self.dirmodel)
        open(os.path.join(self.dirmodel, "model.safetensors"), "wb").write(b"z" * 10)
        _write_profile(self.profiles, "p1", model=self.model,
                       args={"spec-draft-model": self.draft_link})
        _write_profile(self.profiles, "p2", model=self.dirmodel)

    def test_load_profiles_reads_all(self):
        profs = A.load_profiles(self.profiles)
        self.assertEqual({p["id"] for p in profs}, {"p1", "p2"})

    def test_closure_protects_symlink_and_target_and_dir(self):
        files, dirs = A.protected_closure(A.load_profiles(self.profiles))
        # the model file, the draft symlink, and its realpath target are protected
        self.assertTrue(A.is_protected(self.model, files, dirs))
        self.assertTrue(A.is_protected(self.draft_link, files, dirs))
        self.assertTrue(A.is_protected(self.real_draft, files, dirs))
        # the safetensors dir and a file inside it are protected
        self.assertTrue(A.is_protected(self.dirmodel, files, dirs))
        self.assertTrue(A.is_protected(
            os.path.join(self.dirmodel, "model.safetensors"), files, dirs))
        # an unrelated path is NOT protected
        self.assertFalse(A.is_protected(
            os.path.join(self.store, "other", "x.gguf"), files, dirs))


class OrphanTest(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.mkdtemp()
        self.profiles = os.path.join(self.root, "profiles")
        self.store = os.path.join(self.root, "store")
        os.makedirs(self.profiles)
        used = os.path.join(self.store, "pub", "used-GGUF")
        os.makedirs(used)
        self.used_model = os.path.join(used, "m-Q4.gguf")
        open(self.used_model, "wb").write(b"GGUF" + b"0" * 100)
        self.orphan = os.path.join(self.store, "pub", "orphan-GGUF")
        os.makedirs(self.orphan)
        open(os.path.join(self.orphan, "o-Q4.gguf"), "wb").write(b"GGUF" + b"0" * 200)
        _write_profile(self.profiles, "p1", model=self.used_model)

    def test_classify_and_orphan_candidates(self):
        files, dirs = A.protected_closure(A.load_profiles(self.profiles))
        repos = A.list_repo_dirs([self.store])
        used, orphan = A.classify_repos(repos, files, dirs)
        self.assertIn(os.path.join(self.store, "pub", "used-GGUF"), used)
        self.assertIn(self.orphan, orphan)
        cands = A.orphan_candidates(orphan)
        self.assertEqual(len(cands), 1)
        self.assertEqual(cands[0].category, A.CAT_ORPHAN)
        self.assertEqual(cands[0].delete_unit, self.orphan)
        self.assertEqual(cands[0].size, 204)  # 4 + 200


class SiblingTest(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.mkdtemp()
        self.profiles = os.path.join(self.root, "profiles")
        self.store = os.path.join(self.root, "store")
        os.makedirs(self.profiles)
        repo = os.path.join(self.store, "pub", "GGUF")
        os.makedirs(os.path.join(repo, "MTP"))
        self.repo = repo
        self.used = os.path.join(repo, "m-Q4.gguf")
        open(self.used, "wb").write(b"GGUF" + b"0" * 100)
        # referenced draft via symlink into MTP/
        open(os.path.join(repo, "MTP", "draft.gguf"), "wb").write(b"GGUF" + b"0" * 30)
        self.draft_link = os.path.join(repo, "draft.gguf")
        os.symlink(os.path.join("MTP", "draft.gguf"), self.draft_link)
        # an UNUSED sibling quant
        self.unused = os.path.join(repo, "m-Q8.gguf")
        open(self.unused, "wb").write(b"GGUF" + b"0" * 500)
        # a referenced multipart group (part 1 referenced -> both parts protected)
        open(os.path.join(repo, "big-00001-of-00002.gguf"), "wb").write(b"GGUF" + b"a" * 10)
        open(os.path.join(repo, "big-00002-of-00002.gguf"), "wb").write(b"b" * 10)
        self.part1 = os.path.join(repo, "big-00001-of-00002.gguf")
        _write_profile(self.profiles, "p1", model=self.used,
                       args={"spec-draft-model": self.draft_link})
        _write_profile(self.profiles, "p2", model=self.part1)

    def test_only_unused_sibling_flagged(self):
        files, dirs = A.protected_closure(A.load_profiles(self.profiles))
        cands = A.sibling_candidates([self.repo], files, dirs)
        paths = {c.path for c in cands}
        self.assertEqual(paths, {self.unused})
        self.assertEqual(cands[0].category, A.CAT_SIBLING)
        self.assertEqual(cands[0].size, 504)


class IncompleteTest(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.mkdtemp()
        self.store = os.path.join(self.root, "store")
        repo = os.path.join(self.store, "pub", "GGUF")
        os.makedirs(os.path.join(repo, ".cache", "huggingface", "download"))
        self.repo = repo
        # good gguf (not flagged)
        open(os.path.join(repo, "good.gguf"), "wb").write(b"GGUF" + b"0" * 10)
        # bad magic gguf (flagged)
        self.bad = os.path.join(repo, "bad.gguf")
        open(self.bad, "wb").write(b"NOPE" + b"0" * 10)
        # multipart missing shard 2 (part1 present, flagged) ; part1 has magic so not bad-magic
        self.part1 = os.path.join(repo, "split-00001-of-00002.gguf")
        open(self.part1, "wb").write(b"GGUF" + b"0" * 10)
        # incomplete + partial markers
        self.inc = os.path.join(repo, ".cache", "huggingface", "download", "x.incomplete")
        open(self.inc, "wb").write(b"0" * 5)
        self.partial = os.path.join(repo, "y.gguf.partial")
        open(self.partial, "wb").write(b"0" * 5)

    def test_incomplete_detection(self):
        failed_state = {"status": "failed",
                        "dest_file": os.path.join(self.repo, "good.gguf"),
                        "dest_dir": self.repo}
        cands = A.incomplete_candidates([self.store], [failed_state])
        reasons = {c.path: c.reason for c in cands}
        self.assertIn(self.bad, reasons)
        self.assertIn("magic", reasons[self.bad].lower())
        self.assertIn(self.part1, reasons)
        self.assertIn("shard", reasons[self.part1].lower())
        self.assertIn(self.inc, reasons)
        self.assertIn(self.partial, reasons)
        # the failed download's dest_file (exists) is flagged
        self.assertIn(os.path.join(self.repo, "good.gguf"), reasons)


class CacheTest(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.mkdtemp()
        self.store = os.path.join(self.root, "store")
        os.makedirs(os.path.join(self.store, "cyankiwi", "AWQ-Model"))
        open(os.path.join(self.store, "cyankiwi", "AWQ-Model", "m.safetensors"),
             "wb").write(b"0" * 10)
        self.hub = os.path.join(self.root, "hub")
        # duplicate of the store repo
        dup = os.path.join(self.hub, "models--cyankiwi--AWQ-Model", "blobs")
        os.makedirs(dup)
        open(os.path.join(dup, "blob1"), "wb").write(b"0" * 100)
        # other tool's repo (not in store)
        oth = os.path.join(self.hub, "models--black-forest-labs--FLUX.1-dev", "blobs")
        os.makedirs(oth)
        open(os.path.join(oth, "blob1"), "wb").write(b"0" * 200)
        # incomplete blob
        self.inc = os.path.join(self.hub, "models--black-forest-labs--FLUX.1-dev",
                                "blobs", "z.incomplete")
        open(self.inc, "wb").write(b"0" * 5)

    def test_cache_tiers(self):
        keys = A.store_repo_keys([self.store])
        self.assertIn("cyankiwi/AWQ-Model", keys)
        cands = A.cache_candidates(self.hub, keys)
        by_cat = {}
        for c in cands:
            by_cat.setdefault(c.category, []).append(c)
        self.assertEqual(len(by_cat[A.CAT_CACHE_INCOMPLETE]), 1)
        dup = by_cat[A.CAT_CACHE_DUP]
        self.assertEqual(len(dup), 1)
        self.assertTrue(dup[0].delete_unit.endswith("models--cyankiwi--AWQ-Model"))
        oth = by_cat[A.CAT_CACHE_OTHER]
        self.assertEqual(len(oth), 1)
        self.assertTrue(oth[0].delete_unit.endswith("models--black-forest-labs--FLUX.1-dev"))


class OrchestrationTest(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.mkdtemp()
        self.profiles = os.path.join(self.root, "profiles")
        self.store = os.path.join(self.root, "store")
        self.hub = os.path.join(self.root, "hub")
        os.makedirs(self.profiles)
        os.makedirs(self.hub)
        used = os.path.join(self.store, "pub", "used-GGUF")
        os.makedirs(used)
        self.used_model = os.path.join(used, "m-Q4.gguf")
        open(self.used_model, "wb").write(b"GGUF" + b"0" * 10)
        self.orphan = os.path.join(self.store, "pub", "orphan-GGUF")
        os.makedirs(self.orphan)
        open(os.path.join(self.orphan, "o.gguf"), "wb").write(b"GGUF" + b"0" * 300)
        _write_profile(self.profiles, "p1", model=self.used_model)
        # download-in-progress (alive pid) targeting the orphan -> BLOCKED
        self.dl_dir = os.path.join(self.root, "downloads")
        os.makedirs(self.dl_dir)
        with open(os.path.join(self.dl_dir, "dl-1.json"), "w") as f:
            json.dump({"status": "downloading", "pid": os.getpid(),
                       "dest_file": os.path.join(self.orphan, "o.gguf"),
                       "dest_dir": self.orphan}, f)
        self.instances = os.path.join(self.root, "instances.json")
        with open(self.instances, "w") as f:
            json.dump({"instances": []}, f)

    def test_active_download_blocks_candidate(self):
        rep = A.analyze(self.profiles, [self.store], self.hub,
                        self.dl_dir, self.instances, include_cache=False)
        orphans = [c for c in rep.candidates if c.category == A.CAT_ORPHAN]
        self.assertEqual(len(orphans), 1)
        self.assertTrue(orphans[0].blocked)

    def test_report_totals_and_serialization(self):
        rep = A.analyze(self.profiles, [self.store], self.hub,
                        self.dl_dir, self.instances, include_cache=False)
        totals = rep.totals()
        self.assertIn("grand_total", totals)
        self.assertIn("blocked", totals)
        d = rep.to_dict()
        self.assertIn("candidates", d)
        self.assertIsInstance(rep.to_table(), str)


class ProtectionGapTest(unittest.TestCase):
    def test_hf_repo_ref_model_protects_cache_repo(self):
        root = tempfile.mkdtemp()
        profiles = os.path.join(root, "profiles")
        os.makedirs(profiles)
        hub = os.path.join(root, "hub")
        # referenced HF-repo-ref model -> cache repo must NOT be proposed
        ref_repo = os.path.join(hub, "models--unsloth--Qwen3.6-27B-GGUF", "blobs")
        os.makedirs(ref_repo)
        open(os.path.join(ref_repo, "blob1"), "wb").write(b"0" * 100)
        # an unreferenced cache repo -> still proposed (cache-other)
        oth = os.path.join(hub, "models--other--Thing", "blobs")
        os.makedirs(oth)
        open(os.path.join(oth, "blob1"), "wb").write(b"0" * 50)
        _write_profile(profiles, "p1", model="unsloth/Qwen3.6-27B-GGUF")
        profs = A.load_profiles(profiles)
        keys = A.protected_repo_keys(profs)
        self.assertIn("unsloth/Qwen3.6-27B-GGUF", keys)
        cands = A.cache_candidates(hub, set(), keys)
        units = {c.delete_unit for c in cands}
        self.assertFalse(any("models--unsloth--Qwen3.6-27B-GGUF" in u for u in units))
        self.assertTrue(any("models--other--Thing" in u for u in units))

    def test_incomplete_skips_referenced_corrupt_gguf(self):
        root = tempfile.mkdtemp()
        profiles = os.path.join(root, "profiles")
        store = os.path.join(root, "store")
        os.makedirs(profiles)
        repo = os.path.join(store, "pub", "GGUF")
        os.makedirs(repo)
        referenced = os.path.join(repo, "ref-bad.gguf")
        open(referenced, "wb").write(b"NOPE" + b"0" * 10)  # bad magic, but referenced
        unref = os.path.join(repo, "unref-bad.gguf")
        open(unref, "wb").write(b"NOPE" + b"0" * 10)        # bad magic, unreferenced
        _write_profile(profiles, "p1", model=referenced)
        files, dirs = A.protected_closure(A.load_profiles(profiles))
        cands = A.incomplete_candidates([store], [], files, dirs)
        paths = {c.path for c in cands}
        self.assertNotIn(referenced, paths)   # referenced corrupt -> protected
        self.assertIn(unref, paths)           # unreferenced corrupt -> flagged

    def test_incomplete_skips_referenced_multipart_missing_shard(self):
        root = tempfile.mkdtemp()
        profiles = os.path.join(root, "profiles")
        store = os.path.join(root, "store")
        os.makedirs(profiles)
        repo = os.path.join(store, "pub", "GGUF")
        os.makedirs(repo)
        # group of 3, only parts 1 and 2 present (shard 3 missing); part 1 referenced
        part1 = os.path.join(repo, "big-00001-of-00003.gguf")
        part2 = os.path.join(repo, "big-00002-of-00003.gguf")
        open(part1, "wb").write(b"GGUF" + b"0" * 10)
        open(part2, "wb").write(b"0" * 10)
        _write_profile(profiles, "p1", model=part1)
        files, dirs = A.protected_closure(A.load_profiles(profiles))
        cands = A.incomplete_candidates([store], [], files, dirs)
        paths = {c.path for c in cands}
        self.assertNotIn(part1, paths)
        self.assertNotIn(part2, paths)  # whole group protected because part 1 referenced

    def test_complete_multipart_not_flagged_and_part2_magic_ok(self):
        root = tempfile.mkdtemp()
        store = os.path.join(root, "store")
        repo = os.path.join(store, "pub", "GGUF")
        os.makedirs(repo)
        # complete group of 2; part 2 legitimately lacks GGUF magic
        open(os.path.join(repo, "ok-00001-of-00002.gguf"), "wb").write(b"GGUF" + b"0" * 10)
        open(os.path.join(repo, "ok-00002-of-00002.gguf"), "wb").write(b"0" * 10)
        cands = A.incomplete_candidates([store], [])
        self.assertEqual([c.path for c in cands], [])  # nothing flagged


if __name__ == "__main__":
    unittest.main()

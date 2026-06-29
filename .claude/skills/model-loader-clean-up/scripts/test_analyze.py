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


if __name__ == "__main__":
    unittest.main()

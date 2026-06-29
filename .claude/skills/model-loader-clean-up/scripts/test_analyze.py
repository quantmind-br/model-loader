import os, tempfile, unittest
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


if __name__ == "__main__":
    unittest.main()

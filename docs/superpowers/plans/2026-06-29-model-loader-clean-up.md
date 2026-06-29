# model-loader-clean-up Skill — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a skill that analyzes every model-loader profile and helps the operator permanently delete incomplete or unused model artifacts to reclaim disk space, driven by a read-only Python analyzer plus a lean `SKILL.md`.

**Architecture:** A self-contained, dependency-free Python analyzer (`scripts/analyze.py`) reads the profile JSON + `config.toml`, walks the model store and the HF cache, and emits a categorized, read-only report (human table + JSON manifest). It never deletes. `SKILL.md` drives the operator through running the analyzer and confirming `rm` deletions item by item. The analyzer is built test-first (stdlib `unittest`, tempdir fixtures) because deletion is permanent.

**Tech Stack:** Python 3.14 (stdlib only — `json`, `os`, `re`, `tomllib`, `dataclasses`, `argparse`, `unittest`, `tempfile`). No third-party packages. Skill text in English; operator chat in Portuguese.

## Global Constraints

- **Python stdlib only** — no pip installs; the analyzer must run as `python3 scripts/analyze.py`.
- **Analyzer is READ-ONLY** — `analyze.py` never deletes, moves, or modifies any file. Deletion is done by the operator/agent via `rm` per `SKILL.md`.
- **Symlink-aware sizing** — count real bytes once; never follow symlinks when summing (`os.walk(..., followlinks=False)`, skip `os.path.islink`).
- **Protected closure** — a referenced path protects both its literal path and its `realpath`; a `model` pointing to a directory protects the whole directory.
- **Search path (real env):** `/home/diogo/models/huggingface`; HF cache: `~/.cache/huggingface/hub`; profiles: `~/.config/model-loader/profiles`; download states: `~/.local/state/model-loader/downloads`; instances: `~/.local/state/model-loader/instances.json`. The analyzer reads `search_paths` from `~/.config/model-loader/config.toml`; all roots are injectable for tests.
- **Skill hygiene** — `SKILL.md` contains no changelog/dev notes (user global rule); such notes go only in a separate `README.md`. All skill content is English.
- **Dual location** — final skill must exist as byte-identical copies in `~/dev/model-loader/.claude/skills/model-loader-clean-up/` and `~/dev/skills/model-loader-clean-up/`.
- **Git** — develop on a feature branch in the model-loader repo (never commit straight to `main`). Commits are local; pushing/PR/merge happens only when the operator asks. The `~/dev/skills` copy is a separate repo, synced in the final task.
- **Module API names (used across tasks — keep verbatim):**
  `Candidate(path,size,mtime,category,reason,delete_unit,blocked=False,blocked_by="")`;
  category constants `CAT_ORPHAN="store-orphan-repo"`, `CAT_SIBLING="store-unused-sibling"`, `CAT_INCOMPLETE="store-incomplete"`, `CAT_CACHE_INCOMPLETE="cache-incomplete"`, `CAT_CACHE_DUP="cache-duplicate"`, `CAT_CACHE_OTHER="cache-other"`;
  `human(n)`, `real_size(path)`, `safe_mtime(path)`, `multipart_group(name)`, `load_profiles(dir)`, `protected_closure(profiles)->(files,dirs)`, `is_protected(path,files,dirs)`, `list_repo_dirs(search_paths)`, `classify_repos(repos,files,dirs)->(used,orphan)`, `orphan_candidates(orphan_repos)`, `sibling_candidates(used_repos,files,dirs)`, `incomplete_candidates(search_paths,download_states)`, `store_repo_keys(search_paths)`, `cache_candidates(cache_hub_dir,store_keys)`, `load_download_states(dir)`, `active_download_paths(states)`, `running_instances(path)`, `apply_blocked(cands,running,profiles,active_dl)`, `analyze(...)->Report`, `Report.totals()/to_dict()/to_table()`, `load_config_search_paths()`, `main(argv=None)`.

---

### Task 1: Project scaffold + shared helpers

**Files:**
- Create: `~/dev/model-loader/.claude/skills/model-loader-clean-up/scripts/analyze.py`
- Test: `~/dev/model-loader/.claude/skills/model-loader-clean-up/scripts/test_analyze.py`

**Interfaces:**
- Produces: `human(n:int)->str`, `real_size(path:str)->int`, `safe_mtime(path:str)->float`, `MULTIPART_RE`, `multipart_group(name:str)->tuple[str,int,int]|None`; the `Candidate` dataclass and `CAT_*` constants.

- [ ] **Step 1: Create the feature branch**

```bash
cd ~/dev/model-loader && git checkout -b feat/clean-up-skill
mkdir -p .claude/skills/model-loader-clean-up/scripts .claude/skills/model-loader-clean-up/references
```

- [ ] **Step 2: Write the failing test**

Create `scripts/test_analyze.py`:

```python
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd ~/dev/model-loader/.claude/skills/model-loader-clean-up/scripts && python3 -m unittest test_analyze -v`
Expected: FAIL — `ModuleNotFoundError: No module named 'analyze'`.

- [ ] **Step 4: Write minimal implementation**

Create `scripts/analyze.py`:

```python
#!/usr/bin/env python3
"""Read-only analyzer for the model-loader-clean-up skill.

Cross-references every model-loader profile against the on-disk model store and
the Hugging Face cache, and reports artifacts that are unused or incomplete.
NEVER deletes anything — deletion is the operator's job (see SKILL.md).
"""
from __future__ import annotations

import os
import re
from dataclasses import dataclass, field

# ---- categories -----------------------------------------------------------
CAT_ORPHAN = "store-orphan-repo"
CAT_SIBLING = "store-unused-sibling"
CAT_INCOMPLETE = "store-incomplete"
CAT_CACHE_INCOMPLETE = "cache-incomplete"
CAT_CACHE_DUP = "cache-duplicate"
CAT_CACHE_OTHER = "cache-other"


@dataclass
class Candidate:
    path: str
    size: int
    mtime: float
    category: str
    reason: str
    delete_unit: str
    blocked: bool = False
    blocked_by: str = ""


# ---- helpers --------------------------------------------------------------
def human(n: int) -> str:
    f = float(n)
    for unit in ("B", "KiB", "MiB", "GiB", "TiB"):
        if f < 1024 or unit == "TiB":
            return f"{f:.1f}{unit}"
        f /= 1024
    return f"{f:.1f}TiB"


def safe_mtime(path: str) -> float:
    try:
        return os.path.getmtime(path)
    except OSError:
        return 0.0


def real_size(path: str) -> int:
    if os.path.islink(path):
        return 0
    if os.path.isfile(path):
        try:
            return os.path.getsize(path)
        except OSError:
            return 0
    total = 0
    for root, _dirs, files in os.walk(path, followlinks=False):
        for fn in files:
            fp = os.path.join(root, fn)
            if os.path.islink(fp):
                continue
            try:
                total += os.path.getsize(fp)
            except OSError:
                pass
    return total


MULTIPART_RE = re.compile(r"^(.*)-(\d{5})-of-(\d{5})\.gguf$")


def multipart_group(name: str):
    m = MULTIPART_RE.match(name)
    if not m:
        return None
    return (m.group(1), int(m.group(2)), int(m.group(3)))
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd ~/dev/model-loader/.claude/skills/model-loader-clean-up/scripts && python3 -m unittest test_analyze -v`
Expected: PASS (4 tests).

- [ ] **Step 6: Commit**

```bash
cd ~/dev/model-loader && git add .claude/skills/model-loader-clean-up/scripts/
git commit -m "feat(clean-up): scaffold analyzer with size/multipart helpers"
```

---

### Task 2: Profile loading + protected closure

**Files:**
- Modify: `scripts/analyze.py` (append)
- Test: `scripts/test_analyze.py` (append a test class)

**Interfaces:**
- Consumes: helpers from Task 1.
- Produces: `load_profiles(profiles_dir)->list[dict]`, `protected_closure(profiles)->(set[str],set[str])`, `is_protected(path,files,dirs)->bool`.

- [ ] **Step 1: Write the failing test**

Append to `scripts/test_analyze.py`:

```python
import json


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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python3 -m unittest test_analyze -v`
Expected: FAIL — `AttributeError: module 'analyze' has no attribute 'load_profiles'`.

- [ ] **Step 3: Write minimal implementation**

Append to `scripts/analyze.py`:

```python
import glob
import json


def load_profiles(profiles_dir: str) -> list[dict]:
    profs = []
    for f in sorted(glob.glob(os.path.join(profiles_dir, "*.json"))):
        try:
            with open(f) as fh:
                profs.append(json.load(fh))
        except (OSError, json.JSONDecodeError):
            # corrupt profile: ignore for protection purposes (reported elsewhere)
            continue
    return profs


_PATH_ARG_KEYS = ("mmproj", "spec-draft-model", "chat-template-file")


def protected_closure(profiles: list[dict]):
    files: set[str] = set()
    dirs: set[str] = set()

    def add_file(p):
        if isinstance(p, str) and p.startswith("/"):
            files.add(p)
            files.add(os.path.realpath(p))

    for prof in profiles:
        model = prof.get("model")
        if isinstance(model, str) and model.startswith("/"):
            if os.path.isdir(model):
                dirs.add(os.path.realpath(model))
            else:
                add_file(model)
        args = prof.get("args") or {}
        for key in _PATH_ARG_KEYS:
            add_file(args.get(key))
        for ea in prof.get("extraArgs") or []:
            if isinstance(ea, str) and ea.startswith("/"):
                add_file(ea)
    return files, dirs


def is_protected(path: str, files: set[str], dirs: set[str]) -> bool:
    rp = os.path.realpath(path)
    if path in files or rp in files:
        return True
    for d in dirs:
        if rp == d or rp.startswith(d + os.sep) or path == d or path.startswith(d + os.sep):
            return True
    return False
```

- [ ] **Step 4: Run test to verify it passes**

Run: `python3 -m unittest test_analyze -v`
Expected: PASS (all ClosureTest + earlier tests).

- [ ] **Step 5: Commit**

```bash
cd ~/dev/model-loader && git add .claude/skills/model-loader-clean-up/scripts/
git commit -m "feat(clean-up): profile loading + symlink-aware protected closure"
```

---

### Task 3: Repo classification + orphan candidates

**Files:**
- Modify: `scripts/analyze.py` (append)
- Test: `scripts/test_analyze.py` (append a test class)

**Interfaces:**
- Consumes: `protected_closure`, `is_protected`, `real_size`, `safe_mtime`, `Candidate`, `CAT_ORPHAN`.
- Produces: `list_repo_dirs(search_paths)->list[str]`, `classify_repos(repos,files,dirs)->(used,orphan)`, `orphan_candidates(orphan_repos)->list[Candidate]`.

- [ ] **Step 1: Write the failing test**

Append to `scripts/test_analyze.py`:

```python
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python3 -m unittest test_analyze -v`
Expected: FAIL — `AttributeError: module 'analyze' has no attribute 'list_repo_dirs'`.

- [ ] **Step 3: Write minimal implementation**

Append to `scripts/analyze.py`:

```python
def list_repo_dirs(search_paths: list[str]) -> list[str]:
    out = []
    for base in search_paths:
        if not os.path.isdir(base):
            continue
        for pub in sorted(os.listdir(base)):
            pubp = os.path.join(base, pub)
            if not os.path.isdir(pubp):
                continue
            for repo in sorted(os.listdir(pubp)):
                rp = os.path.join(pubp, repo)
                if os.path.isdir(rp):
                    out.append(rp)
    return out


def classify_repos(repos: list[str], files: set[str], dirs: set[str]):
    used, orphan = [], []
    for rp in repos:
        if os.path.realpath(rp) in dirs:
            used.append(rp)
            continue
        protected_inside = False
        for root, _dirs, fnames in os.walk(rp, followlinks=False):
            for fn in fnames:
                if is_protected(os.path.join(root, fn), files, dirs):
                    protected_inside = True
                    break
            if protected_inside:
                break
        (used if protected_inside else orphan).append(rp)
    return used, orphan


def orphan_candidates(orphan_repos: list[str]) -> list[Candidate]:
    cands = []
    for rp in orphan_repos:
        cands.append(Candidate(
            path=rp, size=real_size(rp), mtime=safe_mtime(rp),
            category=CAT_ORPHAN,
            reason="no profile references anything inside this repo",
            delete_unit=rp,
        ))
    return cands
```

- [ ] **Step 4: Run test to verify it passes**

Run: `python3 -m unittest test_analyze -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd ~/dev/model-loader && git add .claude/skills/model-loader-clean-up/scripts/
git commit -m "feat(clean-up): repo classification + orphan-repo candidates"
```

---

### Task 4: Unused sibling GGUF candidates

**Files:**
- Modify: `scripts/analyze.py` (append)
- Test: `scripts/test_analyze.py` (append a test class)

**Interfaces:**
- Consumes: `is_protected`, `multipart_group`, `real_size`, `safe_mtime`, `Candidate`, `CAT_SIBLING`.
- Produces: `sibling_candidates(used_repos,files,dirs)->list[Candidate]`.

- [ ] **Step 1: Write the failing test**

Append to `scripts/test_analyze.py`:

```python
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python3 -m unittest test_analyze -v`
Expected: FAIL — `AttributeError: ... 'sibling_candidates'`.

- [ ] **Step 3: Write minimal implementation**

Append to `scripts/analyze.py`:

```python
def sibling_candidates(used_repos: list[str], files: set[str], dirs: set[str]) -> list[Candidate]:
    cands = []
    for rp in used_repos:
        if os.path.realpath(rp) in dirs:
            continue  # directory-model: never prune inside
        ggufs = []
        for root, _dirs, fnames in os.walk(rp, followlinks=False):
            for fn in fnames:
                if not fn.endswith(".gguf"):
                    continue
                fp = os.path.join(root, fn)
                if os.path.islink(fp):
                    continue
                ggufs.append(fp)
        protected_groups = set()
        for fp in ggufs:
            if is_protected(fp, files, dirs):
                g = multipart_group(os.path.basename(fp))
                if g:
                    protected_groups.add((os.path.dirname(fp), g[0]))
        for fp in ggufs:
            if is_protected(fp, files, dirs):
                continue
            g = multipart_group(os.path.basename(fp))
            if g and (os.path.dirname(fp), g[0]) in protected_groups:
                continue
            cands.append(Candidate(
                path=fp, size=real_size(fp), mtime=safe_mtime(fp),
                category=CAT_SIBLING,
                reason="unused GGUF inside a used repo",
                delete_unit=fp,
            ))
    return cands
```

- [ ] **Step 4: Run test to verify it passes**

Run: `python3 -m unittest test_analyze -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd ~/dev/model-loader && git add .claude/skills/model-loader-clean-up/scripts/
git commit -m "feat(clean-up): unused sibling GGUF detection (multipart-aware)"
```

---

### Task 5: Incomplete / corrupt store candidates

**Files:**
- Modify: `scripts/analyze.py` (append)
- Test: `scripts/test_analyze.py` (append a test class)

**Interfaces:**
- Consumes: `multipart_group`, `real_size`, `safe_mtime`, `Candidate`, `CAT_INCOMPLETE`.
- Produces: `incomplete_candidates(search_paths, download_states)->list[Candidate]`. `download_states` is a list of dicts with keys `status`, `dest_file`, `dest_dir` (the schema of `~/.local/state/model-loader/downloads/*.json`).

Note: only part `00001` of a multipart group carries the `GGUF` magic; parts `00002+` legitimately do not — so check magic only when the file is not multipart or is part 1.

- [ ] **Step 1: Write the failing test**

Append to `scripts/test_analyze.py`:

```python
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python3 -m unittest test_analyze -v`
Expected: FAIL — `AttributeError: ... 'incomplete_candidates'`.

- [ ] **Step 3: Write minimal implementation**

Append to `scripts/analyze.py`:

```python
GGUF_MAGIC = b"GGUF"
_FAILED_STATES = {"failed", "abandoned", "cancelled", "canceled"}


def _inc(path: str, reason: str) -> Candidate:
    return Candidate(path=path, size=real_size(path), mtime=safe_mtime(path),
                     category=CAT_INCOMPLETE, reason=reason, delete_unit=path)


def incomplete_candidates(search_paths: list[str], download_states: list[dict]) -> list[Candidate]:
    cands: list[Candidate] = []
    seen: set[str] = set()
    groups: dict[tuple, list] = {}  # (dir, base) -> [present_parts:set, total:int]

    def emit(path, reason):
        if path not in seen and os.path.exists(path):
            seen.add(path)
            cands.append(_inc(path, reason))

    for base in search_paths:
        if not os.path.isdir(base):
            continue
        for root, _dirs, fnames in os.walk(base, followlinks=False):
            for fn in fnames:
                fp = os.path.join(root, fn)
                if fn.endswith(".partial"):
                    emit(fp, "partial download (.partial)")
                    continue
                if fn.endswith(".incomplete"):
                    emit(fp, "HF incomplete-download marker (.incomplete)")
                    continue
                if fn.endswith(".gguf") and not os.path.islink(fp):
                    g = multipart_group(fn)
                    if g:
                        key = (root, g[0])
                        groups.setdefault(key, [set(), g[2]])
                        groups[key][0].add(g[1])
                    if g is None or g[1] == 1:
                        try:
                            with open(fp, "rb") as fh:
                                if fh.read(4) != GGUF_MAGIC:
                                    emit(fp, "GGUF magic mismatch (corrupt/truncated)")
                        except OSError:
                            pass

    for (d, b), (have, total) in groups.items():
        if len(have) != total:
            for n in sorted(have):
                fp = os.path.join(d, f"{b}-{n:05d}-of-{total:05d}.gguf")
                emit(fp, f"incomplete multipart group: have {len(have)}/{total} shards")

    for st in download_states:
        status = str(st.get("status", "")).lower()
        if status in _FAILED_STATES:
            for p in (st.get("dest_file"), str(st.get("dest_file", "")) + ".partial"):
                if p:
                    emit(p, f"download {status}")
    return cands
```

- [ ] **Step 4: Run test to verify it passes**

Run: `python3 -m unittest test_analyze -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd ~/dev/model-loader && git add .claude/skills/model-loader-clean-up/scripts/
git commit -m "feat(clean-up): incomplete/corrupt store detection (magic/shard/partial/state)"
```

---

### Task 6: Tiered HF cache candidates

**Files:**
- Modify: `scripts/analyze.py` (append)
- Test: `scripts/test_analyze.py` (append a test class)

**Interfaces:**
- Consumes: `real_size`, `safe_mtime`, `Candidate`, `CAT_CACHE_INCOMPLETE`, `CAT_CACHE_DUP`, `CAT_CACHE_OTHER`.
- Produces: `store_repo_keys(search_paths)->set[str]`, `cache_candidates(cache_hub_dir, store_keys)->list[Candidate]`.

- [ ] **Step 1: Write the failing test**

Append to `scripts/test_analyze.py`:

```python
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python3 -m unittest test_analyze -v`
Expected: FAIL — `AttributeError: ... 'store_repo_keys'`.

- [ ] **Step 3: Write minimal implementation**

Append to `scripts/analyze.py`:

```python
def store_repo_keys(search_paths: list[str]) -> set[str]:
    keys = set()
    for base in search_paths:
        if not os.path.isdir(base):
            continue
        for pub in os.listdir(base):
            pubp = os.path.join(base, pub)
            if not os.path.isdir(pubp):
                continue
            for repo in os.listdir(pubp):
                if os.path.isdir(os.path.join(pubp, repo)):
                    keys.add(f"{pub}/{repo}")
    return keys


def cache_candidates(cache_hub_dir: str, store_keys: set[str]) -> list[Candidate]:
    cands: list[Candidate] = []
    if not os.path.isdir(cache_hub_dir):
        return cands
    # Tier 0: incomplete blobs anywhere in the cache
    for root, _dirs, fnames in os.walk(cache_hub_dir, followlinks=False):
        for fn in fnames:
            if fn.endswith(".incomplete"):
                fp = os.path.join(root, fn)
                cands.append(Candidate(
                    path=fp, size=real_size(fp), mtime=safe_mtime(fp),
                    category=CAT_CACHE_INCOMPLETE,
                    reason="HF cache incomplete-download blob", delete_unit=fp))
    # Tier A / Tier B: per cache repo
    for entry in sorted(os.listdir(cache_hub_dir)):
        if not entry.startswith("models--"):
            continue
        repo_dir = os.path.join(cache_hub_dir, entry)
        if not os.path.isdir(repo_dir):
            continue
        org, _, repo = entry[len("models--"):].partition("--")
        key = f"{org}/{repo}"
        size = real_size(repo_dir)
        mt = safe_mtime(repo_dir)
        if key in store_keys:
            cands.append(Candidate(
                path=repo_dir, size=size, mtime=mt, category=CAT_CACHE_DUP,
                reason=f"duplicate of store repo {key} (store copy is canonical)",
                delete_unit=repo_dir))
        else:
            cands.append(Candidate(
                path=repo_dir, size=size, mtime=mt, category=CAT_CACHE_OTHER,
                reason="not in store/profiles — may belong to ComfyUI or another tool",
                delete_unit=repo_dir))
    return cands
```

- [ ] **Step 4: Run test to verify it passes**

Run: `python3 -m unittest test_analyze -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd ~/dev/model-loader && git add .claude/skills/model-loader-clean-up/scripts/
git commit -m "feat(clean-up): tiered HF cache candidates (incomplete/dup/other)"
```

---

### Task 7: State loaders, BLOCKED guard, orchestration, report, CLI

**Files:**
- Modify: `scripts/analyze.py` (append)
- Test: `scripts/test_analyze.py` (append a test class)

**Interfaces:**
- Consumes: everything above.
- Produces: `load_download_states(dir)->list[dict]`, `active_download_paths(states)->set[str]`, `running_instances(path)->list[dict]`, `apply_blocked(cands,running,profiles,active_dl)->None`, `Report`, `analyze(profiles_dir,search_paths,cache_hub_dir,downloads_dir,instances_path,include_cache=True)->Report`, `load_config_search_paths()->list[str]`, `main(argv=None)->int`.

- [ ] **Step 1: Write the failing test**

Append to `scripts/test_analyze.py`:

```python
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python3 -m unittest test_analyze -v`
Expected: FAIL — `AttributeError: module 'analyze' has no attribute 'analyze'`.

- [ ] **Step 3: Write minimal implementation**

Append to `scripts/analyze.py`:

```python
import argparse
import sys
from dataclasses import asdict


def load_download_states(downloads_dir: str) -> list[dict]:
    states = []
    for f in sorted(glob.glob(os.path.join(downloads_dir, "*.json"))):
        try:
            with open(f) as fh:
                states.append(json.load(fh))
        except (OSError, json.JSONDecodeError):
            continue
    return states


def _pid_alive(pid) -> bool:
    try:
        return os.path.exists(f"/proc/{int(pid)}")
    except (TypeError, ValueError):
        return False


def active_download_paths(states: list[dict]) -> set[str]:
    """Targets of downloads whose worker is still alive (in progress)."""
    out = set()
    for st in states:
        if _pid_alive(st.get("pid")):
            for k in ("dest_file", "dest_dir"):
                v = st.get(k)
                if isinstance(v, str) and v:
                    out.add(v)
                    if k == "dest_file":
                        out.add(v + ".partial")
    return out


def running_instances(instances_path: str) -> list[dict]:
    try:
        with open(instances_path) as fh:
            data = json.load(fh)
    except (OSError, json.JSONDecodeError):
        return []
    arr = data.get("instances", data) if isinstance(data, dict) else data
    return [i for i in arr if _pid_alive(i.get("pid"))]


def _profile_paths_by_id(profiles: list[dict]) -> dict[str, set[str]]:
    out = {}
    for prof in profiles:
        files, dirs = protected_closure([prof])
        out[prof.get("id", "")] = files | dirs
    return out


def apply_blocked(cands, running, profiles, active_dl):
    by_id = _profile_paths_by_id(profiles)
    running_paths = set()
    blocker_of = {}
    for inst in running:
        pid_paths = by_id.get(inst.get("profileId", ""), set())
        for p in pid_paths:
            running_paths.add(p)
            blocker_of[p] = inst.get("profileId", "")
    for c in cands:
        targets = {c.path, c.delete_unit, os.path.realpath(c.delete_unit)}
        if targets & active_dl:
            c.blocked = True
            c.blocked_by = "active-download"
        for p in targets:
            if p in running_paths:
                c.blocked = True
                c.blocked_by = blocker_of.get(p, "running-instance")


@dataclass
class Report:
    candidates: list

    _ORDER = (CAT_ORPHAN, CAT_SIBLING, CAT_INCOMPLETE,
              CAT_CACHE_INCOMPLETE, CAT_CACHE_DUP, CAT_CACHE_OTHER)

    def totals(self) -> dict:
        t = {cat: 0 for cat in self._ORDER}
        grand = 0
        blocked = 0
        for c in self.candidates:
            if c.blocked:
                blocked += 1
                continue
            t[c.category] = t.get(c.category, 0) + c.size
            grand += c.size
        t["grand_total"] = grand
        t["blocked"] = blocked
        return t

    def to_dict(self) -> dict:
        return {"candidates": [asdict(c) for c in self.candidates],
                "totals": self.totals()}

    def to_table(self) -> str:
        lines = []
        for cat in self._ORDER:
            rows = [c for c in self.candidates if c.category == cat]
            if not rows:
                continue
            lines.append(f"\n== {cat} ({len(rows)}) ==")
            for c in sorted(rows, key=lambda x: x.size, reverse=True):
                flag = "  [BLOCKED]" if c.blocked else ""
                lines.append(f"  {human(c.size):>10}  {c.delete_unit}{flag}")
                lines.append(f"             {c.reason}")
        t = self.totals()
        lines.append("\n-- reclaimable totals --")
        for cat in self._ORDER:
            if t.get(cat):
                lines.append(f"  {cat:<22} {human(t[cat])}")
        lines.append(f"  {'GRAND TOTAL':<22} {human(t['grand_total'])}")
        lines.append(f"  blocked (skipped): {t['blocked']}")
        return "\n".join(lines)


def analyze(profiles_dir, search_paths, cache_hub_dir,
            downloads_dir, instances_path, include_cache=True) -> Report:
    profiles = load_profiles(profiles_dir)
    files, dirs = protected_closure(profiles)
    repos = list_repo_dirs(search_paths)
    used, orphan = classify_repos(repos, files, dirs)
    states = load_download_states(downloads_dir)
    cands = []
    cands += orphan_candidates(orphan)
    cands += sibling_candidates(used, files, dirs)
    cands += incomplete_candidates(search_paths, states)
    if include_cache:
        cands += cache_candidates(cache_hub_dir, store_repo_keys(search_paths))
    apply_blocked(cands, running_instances(instances_path), profiles,
                  active_download_paths(states))
    return Report(candidates=cands)


def load_config_search_paths() -> list[str]:
    import tomllib
    cfg = os.path.expanduser("~/.config/model-loader/config.toml")
    try:
        with open(cfg, "rb") as fh:
            data = tomllib.load(fh)
        paths = data.get("models", {}).get("search_paths", [])
    except (OSError, tomllib.TOMLDecodeError):
        paths = []
    return [os.path.expanduser(p) for p in paths]


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description="model-loader cleanup analyzer (read-only)")
    home = os.path.expanduser("~")
    ap.add_argument("--profiles", default=os.path.join(home, ".config/model-loader/profiles"))
    ap.add_argument("--cache", default=os.path.join(home, ".cache/huggingface/hub"))
    ap.add_argument("--downloads", default=os.path.join(home, ".local/state/model-loader/downloads"))
    ap.add_argument("--instances", default=os.path.join(home, ".local/state/model-loader/instances.json"))
    ap.add_argument("--no-cache", action="store_true", help="skip the HF cache pass")
    ap.add_argument("--json", metavar="PATH", help="write the JSON manifest to PATH")
    args = ap.parse_args(argv)
    rep = analyze(args.profiles, load_config_search_paths(), args.cache,
                  args.downloads, args.instances, include_cache=not args.no_cache)
    print(rep.to_table())
    if args.json:
        with open(args.json, "w") as fh:
            json.dump(rep.to_dict(), fh, indent=2)
        print(f"\nJSON manifest written to {args.json}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 4: Run test to verify it passes**

Run: `python3 -m unittest test_analyze -v`
Expected: PASS (all classes).

- [ ] **Step 5: Smoke-test against the real environment (read-only)**

Run: `cd ~/dev/model-loader/.claude/skills/model-loader-clean-up/scripts && python3 analyze.py --json /tmp/cleanup-manifest.json | tail -40`
Expected: a table with `store-orphan-repo` listing `LeaderboardModel1/Ornith-1.0-35B-AutoRound-W4A16-Tuning` and `z-lab/Qwen3.6-27B-DFlash`; **no** `store-unused-sibling` entries for the `*-Q8_0-MTP.gguf` drafts (they are protected); a `cache-*` section; a GRAND TOTAL line. Confirm the manifest file exists.

- [ ] **Step 6: Commit**

```bash
cd ~/dev/model-loader && git add .claude/skills/model-loader-clean-up/scripts/
git commit -m "feat(clean-up): orchestration, BLOCKED guard, report + CLI"
```

---

### Task 8: SKILL.md + references/safety.md

**Files:**
- Create: `~/dev/model-loader/.claude/skills/model-loader-clean-up/SKILL.md`
- Create: `~/dev/model-loader/.claude/skills/model-loader-clean-up/references/safety.md`

**Interfaces:**
- Consumes: `scripts/analyze.py` CLI behavior from Task 7.

- [ ] **Step 1: Write `SKILL.md`**

Create `SKILL.md` (English; lean; no changelog):

```markdown
---
name: model-loader-clean-up
description: Use when the operator wants to reclaim disk space from the model-loader model store or Hugging Face cache by removing incomplete/corrupt or unused-by-any-profile model artifacts. Triggers — "clean up models", "free disk space", "remove unused models", "delete old/incomplete downloads", "limpar modelos", "recuperar espaço em disco", "modelos não usados", or any request to audit which downloaded weights no profile still references. Cross-references every profile against the store + HF cache, reports reclaimable space by category, and deletes only what the operator confirms item by item.
---

# model-loader Clean-Up

Reclaim disk space by deleting model artifacts that are **incomplete/corrupt** or **unused by
every profile**. Deletion is **permanent** — so the flow is always: read-only report → confirm
item by item → `rm`.

## Step 1 — Analyze (read-only, never deletes)

```bash
python3 "$SKILL_DIR/scripts/analyze.py" --json /tmp/cleanup-manifest.json
```

(`$SKILL_DIR` is this skill's directory. Add `--no-cache` to skip the shared HF cache pass.)

The report has these categories (each line shows real reclaimable bytes + the exact delete unit):

- `store-orphan-repo` — a `publisher/repo` dir no profile references at all. Delete unit = the dir.
- `store-unused-sibling` — an unused `.gguf` inside an otherwise-used repo (e.g. an extra quant).
- `store-incomplete` — corrupt GGUF (bad magic), missing multipart shards, `*.partial`,
  `*.incomplete`, or the target of a failed download.
- `cache-incomplete` — partial blobs in `~/.cache/huggingface` (safe for any tool).
- `cache-duplicate` — a cache repo that also exists in the store (store copy is canonical).
- `cache-other` — a cache repo not in the store/profiles. **May belong to ComfyUI or another
  tool** — verify before deleting.

Items marked `[BLOCKED]` (active download or running instance) are excluded — never delete them.

## Step 2 — Review with the operator, item by item

Read the manifest (`/tmp/cleanup-manifest.json`). Present candidates grouped by category, largest
first, with size and reason. Default is **keep**. Treat each category by risk:

- `store-orphan-repo` / `store-incomplete` / `cache-incomplete` / `cache-duplicate` — low risk, but
  still confirm each (an "orphan" may be kept intentionally for a planned profile — e.g. a draft
  model for a not-yet-created profile).
- `store-unused-sibling` — confirm the operator does not want that extra quant.
- `cache-other` — **highest caution**: confirm it is not a ComfyUI/other-tool asset before deleting.

## Step 3 — Delete only confirmed items

For each confirmed candidate, delete its `delete_unit`:

```bash
rm -f  "<delete_unit>"     # files (sibling / incomplete / cache-incomplete)
rm -rf "<delete_unit>"     # directories (orphan repos / cache repos)
```

(Optional safety net: `gio trash "<delete_unit>"` or `trash "<delete_unit>"` are installed if the
operator prefers reversible removal over permanent `rm`.)

## Step 4 — Verify reclaimed space

Re-run Step 1 and confirm the deleted items are gone and the GRAND TOTAL dropped.

See `references/safety.md` for the exact protection rules (symlink closure, directory-models,
multipart groups, cache tiers) the analyzer enforces.
```

- [ ] **Step 2: Write `references/safety.md`**

Create `references/safety.md` (English):

```markdown
# Safety rules enforced by `analyze.py`

These are the invariants that keep the analyzer from proposing an in-use artifact.

## Protected closure (never proposed for deletion)
- Every profile path from `model`, `args.mmproj`, `args.spec-draft-model`,
  `args.chat-template-file`, and `/`-prefixed `extraArgs` is protected.
- A protected path protects **both its literal path and its `realpath`**. A referenced **symlink**
  (e.g. `draft.gguf -> MTP/draft.gguf`) therefore protects the real target in the subdir.
- A `model` that resolves to a **directory** (safetensors / AWQ / GPTQ / EXL2 / EXL3) protects the
  **entire directory** — the analyzer never prunes individual files inside a directory-model.

## Multipart GGUF
- Files named `<base>-<NNNNN>-of-<MMMMM>.gguf` form a group. If **any** part is protected, **all**
  parts are protected. Only part `00001` carries the `GGUF` magic, so magic checks skip parts `2+`.
- A group present on disk with fewer than `MMMMM` shards is reported as `store-incomplete`.

## Sizing
- Real bytes are counted once; symlinks contribute 0 and are never followed. Deleting a directory
  reclaims the real files it contains.

## HF cache tiers (shared with other tools)
- `cache-incomplete`: partial blobs — safe to remove for any tool.
- `cache-duplicate`: `models--org--repo` whose `org/repo` also exists in the store — the store copy
  is what model-loader uses, so the cache copy is redundant.
- `cache-other`: any other cache repo — **not** auto-classified as model-loader's; manual review
  only, because it may belong to ComfyUI or another tool.

## BLOCKED items
- A candidate whose target is being written by an **alive download worker**, or referenced by a
  **running instance's** profile, is marked `[BLOCKED]` and excluded from deletion.
```

- [ ] **Step 3: Verify the skill files parse**

Run: `cd ~/dev/model-loader/.claude/skills/model-loader-clean-up && head -3 SKILL.md && python3 scripts/analyze.py --help`
Expected: the YAML frontmatter shows, and `--help` prints the analyzer usage.

- [ ] **Step 4: Commit**

```bash
cd ~/dev/model-loader && git add .claude/skills/model-loader-clean-up/SKILL.md .claude/skills/model-loader-clean-up/references/
git commit -m "feat(clean-up): SKILL.md + safety reference"
```

---

### Task 9: Dual-save to ~/dev/skills and final verification

**Files:**
- Create (copy): `~/dev/skills/model-loader-clean-up/` (identical to the project copy)

**Interfaces:**
- Consumes: the finished skill from Tasks 1–8.

- [ ] **Step 1: Copy the skill to the global location (excluding test + pycache)**

```bash
SRC=~/dev/model-loader/.claude/skills/model-loader-clean-up
DST=~/dev/skills/model-loader-clean-up
rm -rf "$DST" && mkdir -p "$DST/scripts" "$DST/references"
cp "$SRC/SKILL.md" "$DST/SKILL.md"
cp "$SRC/scripts/analyze.py" "$DST/scripts/analyze.py"
cp "$SRC/scripts/test_analyze.py" "$DST/scripts/test_analyze.py"
cp "$SRC/references/safety.md" "$DST/references/safety.md"
```

- [ ] **Step 2: Verify the two copies are byte-identical**

Run: `diff -r ~/dev/model-loader/.claude/skills/model-loader-clean-up ~/dev/skills/model-loader-clean-up -x __pycache__`
Expected: no output (identical).

- [ ] **Step 3: Run the test suite from the global copy**

Run: `cd ~/dev/skills/model-loader-clean-up/scripts && python3 -m unittest test_analyze -v`
Expected: PASS (all tests).

- [ ] **Step 4: Commit the global skills repo**

```bash
cd ~/dev/skills && git add model-loader-clean-up && git commit -m "feat: add model-loader-clean-up skill"
```

- [ ] **Step 5: Report to the operator**

Summarize: skill location (both paths), how to run (`python3 scripts/analyze.py`), the real reclaim
total observed in the Task 7 smoke test, and that deletion remains manual/item-by-item. Ask whether
to push/PR the model-loader branch (git rule: only on request).

---

## Self-Review

**Spec coverage:**
- Orphans + unused siblings (per-file) → Tasks 3, 4. ✔
- Incomplete/corrupt (magic, shard, `.incomplete`, `.partial`, failed states) → Task 5. ✔
- Protected closure symlink-aware + dir-vs-file → Task 2 (proven by the `MTP/` symlink + safetensors-dir tests). ✔
- Multipart-aware → Tasks 4, 5. ✔
- HF cache tiered (0/A/B) → Task 6. ✔
- Real-byte sizing, no double-count → Task 1 `real_size` test. ✔
- BLOCKED guard (active download / running instance) → Task 7. ✔
- Report JSON + table + totals → Task 7. ✔
- Read-only analyzer; deletion via SKILL.md item-by-item `rm` → Task 8. ✔
- Dual-save identical copies → Task 9. ✔
- TDD with the fixture matrix from spec §10 → covered across Tasks 1–7. ✔

**Placeholder scan:** No TBD/TODO; every code/test step shows complete code; every command shows expected output. ✔

**Type consistency:** `Candidate` fields, `CAT_*` constants, and all function signatures in Global Constraints are used verbatim across Tasks 1–9. `analyze(...)` signature matches its call in `main()` and in the OrchestrationTest. ✔

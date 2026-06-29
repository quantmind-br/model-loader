#!/usr/bin/env python3
"""Read-only analyzer for the model-loader-clean-up skill.

Cross-references every model-loader profile against the on-disk model store and
the Hugging Face cache, and reports artifacts that are unused or incomplete.
NEVER deletes anything — deletion is the operator's job (see SKILL.md).
"""
from __future__ import annotations

import os
import re
import glob
import json
from dataclasses import dataclass

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
    for unit in ("B", "KiB", "MiB", "GiB"):
        if f < 1024:
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

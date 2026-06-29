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

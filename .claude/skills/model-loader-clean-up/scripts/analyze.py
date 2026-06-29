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
from collections.abc import Set as AbstractSet
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


def is_protected(path: str, files: AbstractSet[str], dirs: AbstractSet[str]) -> bool:
    rp = os.path.realpath(path)
    if path in files or rp in files:
        return True
    for d in dirs:
        if rp == d or rp.startswith(d + os.sep) or path == d or path.startswith(d + os.sep):
            return True
    return False


def protected_repo_keys(profiles: list[dict]) -> set[str]:
    """`org/repo` keys for profile `model` values that are HF repo refs (not absolute paths).

    Such models are served from the HF cache (`models--org--repo`), so they must
    never be proposed for deletion from the cache.
    """
    keys = set()
    for prof in profiles:
        model = prof.get("model")
        if isinstance(model, str) and model and not model.startswith("/"):
            parts = model.strip("/").split("/")
            if len(parts) >= 2:
                keys.add(f"{parts[0]}/{parts[1]}")
    return keys


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


def incomplete_candidates(search_paths: list[str], download_states: list[dict],
                          files: AbstractSet[str] = frozenset(),
                          dirs: AbstractSet[str] = frozenset()) -> list[Candidate]:
    cands: list[Candidate] = []
    seen: set[str] = set()
    groups: dict[tuple, list] = {}  # (dir, base) -> [present_parts:set, total:int]

    def emit(path, reason):
        if path not in seen and os.path.exists(path) and not is_protected(path, files, dirs):
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
            group_protected = any(
                is_protected(os.path.join(d, f"{b}-{n:05d}-of-{total:05d}.gguf"), files, dirs)
                for n in have
            )
            if group_protected:
                continue
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
        lines.append(f"  {'GRAND TOTAL (upper bound)':<26} {human(t['grand_total'])}")
        lines.append("  (upper bound: a file inside a flagged repo is counted in both)")
        lines.append(f"  blocked (skipped): {t['blocked']}")
        return "\n".join(lines)


def analyze(profiles_dir, search_paths, cache_hub_dir,
            downloads_dir, instances_path, include_cache=True) -> Report:
    profiles = load_profiles(profiles_dir)
    files, dirs = protected_closure(profiles)
    repo_keys = protected_repo_keys(profiles)
    repos = list_repo_dirs(search_paths)
    used, orphan = classify_repos(repos, files, dirs)
    states = load_download_states(downloads_dir)
    cands = []
    cands += orphan_candidates(orphan)
    cands += sibling_candidates(used, files, dirs)
    cands += incomplete_candidates(search_paths, states, files, dirs)
    if include_cache:
        cands += cache_candidates(cache_hub_dir, store_repo_keys(search_paths), repo_keys)
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


def cache_candidates(cache_hub_dir: str, store_keys: AbstractSet[str],
                     protected_keys: AbstractSet[str] = frozenset()) -> list[Candidate]:
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
        if key in protected_keys:
            continue  # referenced by a profile (HF repo ref) — in use, never a candidate
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


if __name__ == "__main__":
    sys.exit(main())

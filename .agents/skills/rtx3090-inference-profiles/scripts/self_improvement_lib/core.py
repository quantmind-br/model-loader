#!/usr/bin/env python3
"""Runtime-neutral, stdlib-only self-improvement control plane.

Single policy implementation for the learning-candidate → evaluation → approval
→ promotion → dual-copy synchronization → rollback loop described in
``adding-self-improvement-to-a-skill.md``. Per-skill wrappers are thin
argument/config adapters; every safety gate lives here.

Design invariants:
- No third-party dependencies. Schemas ship as portable JSON contracts and are
  hash-pinned; validation itself is enforced manually in this module.
- Durable state lives OUTSIDE the portable skill package (XDG by default).
- Persistent skill changes are treated as control-plane changes: diffed,
  evaluated against a baseline, approved, and applied transactionally.
- External / tool / agent evidence can never, by itself, verify a candidate.
"""
from __future__ import annotations
import copy

import fcntl
import hashlib
import hmac
import json
import os
import re
import secrets
import shutil
import subprocess
import tempfile
import uuid
from contextlib import contextmanager
from datetime import datetime, timezone
from difflib import unified_diff
from fnmatch import fnmatch
from pathlib import Path
from typing import Any, Callable, Iterable, Iterator

SCHEMA_DIR = Path(__file__).resolve().parent / "schemas"

# Hash-pinned portable contracts. The shipped JSON Schema files are inputs to the
# validator, protected by digest; the actual enforcement is manual (below).
SCHEMA_SHA256 = {
    "candidate": "2a7974419fdddcf5ad120219adeb8277f15f6f5a1d69b8f2007772adba615d45",
    "occurrence": "4efe191fbfa01c080dfcf52f648830632637919878ed79903b2aaba0da481513",
    "evaluation": "c90a58bf57e4fd7dca3d62225f08d46d18dc8063f233688ecfa622bc7bb6e159",
    "approval": "76f94eb46c4e81a594ff2bb388fc6ee6d529fde2ea6857d65573a37310128fc0",
    "transaction": "55ad0f3f935d30f97cb7b4ec51e1b2c87ddc848788878fa96054239d8ee7c475",
}

TYPES = {"heal", "correction", "project-fact", "technique", "feature-gap"}
STATUSES = {"candidate", "verified", "approved", "applied", "rejected", "expired"}
TERMINAL = {"applied", "rejected", "expired"}
SOURCE_TRUST = {"authenticated-user", "project-owned", "external-untrusted", "tool-output", "agent-generated"}
CORROBORATING_TRUST = {"authenticated-user", "project-owned"}
SCOPES = {"task", "project", "user", "portable-skill"}
RISKS = {"low", "medium", "high"}
OPERATIONS = {"add", "replace", "delete"}

# Allowed candidate transitions.
TRANSITIONS = {
    "candidate": {"verified", "rejected", "expired"},
    "verified": {"approved", "rejected", "expired"},
    "approved": {"applied", "rejected"},
}

PATTERN_KEY_RE = re.compile(r"^[a-z0-9]+(?:[._-][a-z0-9]+)*$")
CAND_ID_RE = re.compile(r"^CAND-[0-9a-f]{32}$")
OCC_ID_RE = re.compile(r"^OCC-[0-9a-f]{32}$")
TX_ID_RE = re.compile(r"^TX-[0-9a-f]{32}$")
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
MAX_FIELD_BYTES = 4096
MAX_CMD_TIMEOUT_S = 120

# Executable-ish file patterns: changing one requires a focused command named in
# the candidate's evaluation_plan.
_EXECUTABLE_SUFFIXES = {".py", ".mjs", ".js", ".cjs", ".sh", ".bash"}

# Curated-tree exclusions applied whenever a tree is hashed or copied. This keeps
# nested .agents content from being recursively copied and ignores volatile data.
DEFAULT_EXCLUDES = (
    ".agents/", ".agent-state/", ".git/", ".env", "node_modules/",
    "__pycache__/", "*.pyc", "*.bak", "*.log", "*.tmp", ".deob/",
)

DEFAULT_CANONICAL_HOME = "~/dev/skills"


class PolicyError(Exception):
    """A safety/approval policy refusal (maps to wrapper exit code 3)."""


class InputError(Exception):
    """Invalid input/config/schema (maps to wrapper exit code 2)."""


class ConflictError(Exception):
    """Preimage / project-copy / concurrent-edit conflict (exit code 4)."""


class ApplyFailed(Exception):
    """Apply began but post-validation failed (exit code 5)."""


# --------------------------------------------------------------------------- #
# Time / id helpers
# --------------------------------------------------------------------------- #
def _now() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def _new_id(prefix: str) -> str:
    return f"{prefix}-{uuid.uuid4().hex}"


# --------------------------------------------------------------------------- #
# Hashing
# --------------------------------------------------------------------------- #
def _sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def _sha256_file(path: Path) -> str:
    return _sha256_bytes(path.read_bytes())


def _norm_excludes(excludes: Iterable[str] | None) -> tuple[str, ...]:
    """Merge caller exclusions with mandatory safety/runtime exclusions."""
    merged = list(DEFAULT_EXCLUDES)
    for entry in excludes or ():
        if entry not in merged:
            merged.append(entry)
    return tuple(merged)


def _is_excluded(rel: Path, excludes: tuple[str, ...]) -> bool:
    parts = rel.parts
    base = rel.name
    for entry in excludes:
        if entry.endswith("/"):
            if entry[:-1] in parts:
                return True
        elif "*" in entry or "?" in entry or "[" in entry:
            if fnmatch(base, entry):
                return True
        else:
            if base == entry or entry in parts:
                return True
    return False


def _iter_tree_files(root: Path, excludes: tuple[str, ...]) -> Iterator[tuple[str, Path]]:
    """Yield (relative_posix_path, absolute_path) for included regular files."""
    if not root.exists():
        return
    for dirpath, dirnames, filenames in os.walk(root):
        d = Path(dirpath)
        # Prune excluded directories in place (and never descend symlinked dirs).
        keep = []
        for name in dirnames:
            child = d / name
            rel = child.relative_to(root)
            if child.is_symlink() or _is_excluded(rel, excludes):
                continue
            keep.append(name)
        dirnames[:] = keep
        for name in sorted(filenames):
            f = d / name
            rel = f.relative_to(root)
            if f.is_symlink() or _is_excluded(rel, excludes):
                continue
            yield (rel.as_posix(), f)


def curated_tree_hash(root: Path, excludes: Iterable[str] | None = None) -> str | None:
    """Deterministic hash of a tree's curated content, or None if absent."""
    if not root.exists():
        return None
    ex = _norm_excludes(excludes)
    digest = hashlib.sha256()
    for rel, path in sorted(_iter_tree_files(root, ex), key=lambda x: x[0]):
        digest.update(rel.encode("utf8"))
        digest.update(b"\0")
        digest.update(path.read_bytes())
        digest.update(b"\0")
    return digest.hexdigest()


def _copy_tree(src: Path, dst: Path, excludes: tuple[str, ...]) -> None:
    """Copy curated content of src into a fresh dst, applying excludes."""
    if dst.exists():
        shutil.rmtree(dst)
    dst.mkdir(parents=True, exist_ok=True)
    for rel, path in _iter_tree_files(src, excludes):
        target = dst / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target)


def assert_no_special_files(root: Path, excludes: Iterable[str] | None = None) -> None:
    """Fail closed if a tree contains any symlink, socket, device, or FIFO within
    non-excluded paths. Silent omission would let a forbidden object slip through
    a copy; candidate diffs must be plain UTF-8 text/JSON in regular files only.
    """
    if not root.exists():
        return
    ex = _norm_excludes(excludes)
    for dirpath, dirnames, filenames in os.walk(root, followlinks=False):
        d = Path(dirpath)
        keep = []
        for name in dirnames:
            child = d / name
            rel = child.relative_to(root)
            if _is_excluded(rel, ex):
                continue
            if child.is_symlink():
                raise PolicyError(f"symlink not allowed in candidate tree: {rel}")
            keep.append(name)
        dirnames[:] = keep
        for name in filenames:
            child = d / name
            rel = child.relative_to(root)
            if _is_excluded(rel, ex):
                continue
            if child.is_symlink():
                raise PolicyError(f"symlink not allowed in candidate tree: {rel}")
            try:
                st = os.stat(child, follow_symlinks=False)
            except OSError as exc:
                raise PolicyError(f"cannot stat {rel}: {exc}") from exc
            import stat as _stat
            mode = st.st_mode
            if not (_stat.S_ISREG(mode) or _stat.S_ISDIR(mode)):
                raise PolicyError(f"non-regular file not allowed in candidate tree: {rel}")


def _assert_changed_paths_text(root: Path, changed: list[str]) -> None:
    """Confine only added/replaced paths to UTF-8 text (JSON must parse). Binary
    fixtures that a skill ships unchanged are never inspected here."""
    for rel in changed:
        path = root / rel
        if not path.is_file():
            continue  # a deletion has no candidate-side content
        data = path.read_bytes()
        try:
            text = data.decode("utf8")
        except UnicodeDecodeError as exc:
            raise PolicyError(f"non-UTF-8 change not allowed: {rel}") from exc
        if path.suffix == ".json":
            try:
                json.loads(text)
            except json.JSONDecodeError as exc:
                raise PolicyError(f"invalid JSON change: {rel}: {exc}") from exc


# --------------------------------------------------------------------------- #
# JSON IO
# --------------------------------------------------------------------------- #
def _load_json(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf8"))


def _dump_json_atomic(path: Path, obj: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.parent / f".{path.name}.{uuid.uuid4().hex}.tmp"
    text = json.dumps(obj, indent=2, sort_keys=True) + "\n"
    with open(tmp, "w", encoding="utf8") as fh:
        fh.write(text)
        fh.flush()
        os.fsync(fh.fileno())
    os.replace(tmp, path)
    _fsync_dir(path.parent)


def _fsync_dir(path: Path) -> None:
    try:
        fd = os.open(str(path), os.O_RDONLY)
    except OSError:
        return
    try:
        os.fsync(fd)
    except OSError:
        pass
    finally:
        os.close(fd)


# --------------------------------------------------------------------------- #
# Schema-file integrity + manual enforcement
# --------------------------------------------------------------------------- #
def verify_schema_integrity() -> None:
    """Fail closed if any shipped schema file drifts from its pinned digest."""
    for name, expected in SCHEMA_SHA256.items():
        path = SCHEMA_DIR / f"{name}.schema.json"
        if not path.is_file():
            raise InputError(f"schema file missing: {path}")
        actual = _sha256_file(path)
        if actual != expected:
            raise InputError(f"schema {name} tampered: {actual} != {expected}")


def _require(cond: bool, msg: str) -> None:
    if not cond:
        raise InputError(msg)


def _check_free_text(value: Any, field: str) -> str:
    _require(isinstance(value, str) and value.strip() != "", f"{field}: non-empty string required")
    _require(len(value.encode("utf8")) <= MAX_FIELD_BYTES, f"{field}: exceeds {MAX_FIELD_BYTES} bytes")
    return value


# --------------------------------------------------------------------------- #
# Sanitization
# --------------------------------------------------------------------------- #
_PEM_RE = re.compile(r"-----BEGIN [A-Z ]+-----")
_BEARER_RE = re.compile(r"\bBearer\s+[A-Za-z0-9._~+/=-]{8,}", re.IGNORECASE)
_CRED_ASSIGN_RE = re.compile(
    r"(?i)\b(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|"
    r"private[_-]?key|client[_-]?secret|authorization)\b\s*[:=]\s*\S+"
)
_SECRET_KEY_RE = re.compile(r"(?i)(password|passwd|secret|token|apikey|api_key|private_key|credential)")


def _sanitize_text(text: str, prefixes: dict[str, str]) -> str:
    """Replace known path prefixes with placeholders and reject secret shapes."""
    if "\x00" in text:
        raise PolicyError("NUL byte in candidate content")
    # Longest prefix first so /home/x/dev/skills replaces before /home/x.
    for real, placeholder in sorted(prefixes.items(), key=lambda kv: -len(kv[0])):
        if real:
            text = text.replace(real, placeholder)
    if _PEM_RE.search(text):
        raise PolicyError("PEM key block in candidate content")
    if _BEARER_RE.search(text):
        raise PolicyError("bearer token in candidate content")
    if _CRED_ASSIGN_RE.search(text):
        raise PolicyError("credential assignment in candidate content")
    return text


def _prefix_map(*, home: Path | None, canonical: Path | None, project: Path | None) -> dict[str, str]:
    m: dict[str, str] = {}
    if home is not None:
        m[str(home)] = "<home>"
    if canonical is not None:
        m[str(canonical)] = "<canonical>"
    if project is not None:
        m[str(project)] = "<project>"
    # Also map the real user home directory generically.
    m[str(Path.home())] = "<home>"
    return m


# --------------------------------------------------------------------------- #
# State root resolution + project id
# --------------------------------------------------------------------------- #
def _state_base(explicit: str | Path | None) -> Path:
    if explicit:
        return Path(explicit).expanduser().resolve()
    env = os.environ.get("AGENT_SKILL_STATE_DIR")
    if env:
        return Path(env).expanduser().resolve()
    xdg = os.environ.get("XDG_STATE_HOME")
    base = Path(xdg).expanduser() if xdg else Path.home() / ".local" / "state"
    return (base / "agent-skills" / "self-improvement").resolve()


def _project_secret(base: Path) -> bytes:
    """A local 32-byte secret (mode 0600) used to derive opaque project ids."""
    base.mkdir(parents=True, exist_ok=True)
    secret_path = base / "project-id.secret"
    if secret_path.exists():
        data = secret_path.read_bytes()
        if len(data) >= 32:
            return data
    data = secrets.token_bytes(32)
    fd = os.open(str(secret_path), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    try:
        os.write(fd, data)
    finally:
        os.close(fd)
    os.chmod(secret_path, 0o600)
    return data


def project_id(project_root: str | Path, state_base: Path) -> str:
    secret = _project_secret(state_base)
    real = str(Path(project_root).expanduser().resolve())
    return hmac.new(secret, real.encode("utf8"), hashlib.sha256).hexdigest()[:24]


def resolve_state_root(skill_name: str, project_root: str | Path,
                       explicit: str | Path | None = None) -> Path:
    """Precedence: explicit --state-dir → AGENT_SKILL_STATE_DIR → XDG default.

    State is namespaced by an opaque HMAC project id, never a plain path hash.
    """
    _require(isinstance(skill_name, str) and skill_name != "", "skill_name required")
    base = _state_base(explicit)
    pid = project_id(project_root, base)
    root = base / pid / skill_name
    for sub in ("candidates", "occurrences", "evidence", "snapshots",
                "worktrees", "proposals", "approvals", "transactions", "archive"):
        (root / sub).mkdir(parents=True, exist_ok=True)
    return root


def canonical_home_default(explicit: str | Path | None = None) -> Path:
    if explicit:
        return Path(explicit).expanduser().resolve()
    env = os.environ.get("AGENT_SKILL_CANONICAL_HOME")
    return Path(env).expanduser().resolve() if env else Path(DEFAULT_CANONICAL_HOME).expanduser().resolve()


# --------------------------------------------------------------------------- #
# Candidate change-path confinement
# --------------------------------------------------------------------------- #
_FORBIDDEN_COMPONENTS = {".git", ".agents", ".agent-state", ".github", ".gitlab",
                        ".circleci", "node_modules", "__pycache__"}
_FORBIDDEN_BASENAMES = {
    ".env", "package.json", "package-lock.json", "uv.lock", "poetry.lock",
    "yarn.lock", "pnpm-lock.yaml", "requirements.txt", "pyproject.toml",
    "AGENTS.md", "CLAUDE.md", ".gitignore",
}
_FORBIDDEN_SUFFIXES = {".lock", ".pem", ".key", ".p12", ".pfx"}
_ALLOWED_ROOT_FILES = {"SKILL.md", "capabilities.json", "self-improvement.json"}
_ALLOWED_CHANGE_DIRS = {
    "references", "workflows", "evals", "scripts", "evaluation", "taxonomy",
    "playbooks", "tools", "transforms", "schemas", "assets",
}


def validate_change_path(rel_path: str, skill_name: str) -> Path:
    """Confine one proposed-change path to curated skill content.

    Root files are allowlisted; nested changes must live under a known portable
    skill resource directory. README/changelog, arbitrary root files, absolute or
    traversing paths, peer/shared changes, VCS/CI, credentials and dependencies
    are refused before a candidate is recorded.
    """
    _require(isinstance(rel_path, str) and rel_path != "", "change path must be a non-empty string")
    _require("\x00" not in rel_path, "change path has NUL byte")
    p = Path(rel_path)
    _require(not p.is_absolute(), f"absolute change path forbidden: {rel_path}")
    _require(".." not in p.parts, f"traversing change path forbidden: {rel_path}")
    _require(p.parts and p.parts[0] not in {"shared"}, f"shared/ change forbidden: {rel_path}")
    for comp in p.parts:
        _require(comp not in _FORBIDDEN_COMPONENTS, f"forbidden component {comp!r} in {rel_path}")
    _require(p.name not in _FORBIDDEN_BASENAMES, f"forbidden file {p.name!r} in {rel_path}")
    _require(p.suffix not in _FORBIDDEN_SUFFIXES, f"forbidden suffix in {rel_path}")
    if len(p.parts) == 1:
        _require(p.name in _ALLOWED_ROOT_FILES,
                 f"root-level change is not allowlisted: {rel_path}")
    else:
        _require(p.parts[0] in _ALLOWED_CHANGE_DIRS,
                 f"change path is outside curated skill directories: {rel_path}")
    return p


def _is_executable_change(rel_path: str) -> bool:
    return Path(rel_path).suffix in _EXECUTABLE_SUFFIXES


def validate_trigger_report(report: object, *, skill_name: str) -> dict:
    """Validate a no-regression trigger report for protected frontmatter changes."""
    _require(isinstance(report, dict), "trigger report must be a JSON object")
    required = {"schemaVersion", "skill", "cases", "summary"}
    _require(required.issubset(report),
             f"trigger report missing keys: {sorted(required - set(report))}")
    _require(report["schemaVersion"] == 1, "trigger report schemaVersion must be 1")
    _require(report["skill"] == skill_name, "trigger report skill mismatch")
    cases = report["cases"]
    _require(isinstance(cases, list) and len(cases) >= 6,
             "trigger report needs at least six cases")
    positives = negatives = 0
    mismatches: list[str] = []
    ids: set[str] = set()
    for case in cases:
        _require(isinstance(case, dict), "each trigger case must be an object")
        _require({"id", "prompt", "should", "new"}.issubset(case),
                 "each trigger case needs id, prompt, should, and new")
        cid = case["id"]
        _require(isinstance(cid, str) and cid and cid not in ids,
                 f"trigger case id must be unique: {cid!r}")
        ids.add(cid)
        _require(isinstance(case["prompt"], str) and case["prompt"].strip(),
                 f"trigger case {cid}: prompt required")
        _require(isinstance(case["should"], bool) and isinstance(case["new"], bool),
                 f"trigger case {cid}: should/new must be booleans")
        positives += int(case["should"])
        negatives += int(not case["should"])
        if case["new"] != case["should"]:
            mismatches.append(cid)
    _require(positives >= 3 and negatives >= 3,
             "trigger report needs at least three positive and three negative cases")
    _require(not mismatches, f"trigger report classification mismatches: {mismatches}")
    summary = report["summary"]
    _require(isinstance(summary, dict) and summary.get("noRegression") is True,
             "trigger report summary.noRegression must be true")
    _require(summary.get("positiveCasesRetainedByNew") == positives
             and summary.get("positiveCasesTotal") == positives,
             "trigger report positive summary mismatch")
    _require(summary.get("negativeCasesRejectedByNew") == negatives
             and summary.get("negativeCasesTotal") == negatives,
             "trigger report negative summary mismatch")
    return report


# --------------------------------------------------------------------------- #
# Candidate + occurrence records
# --------------------------------------------------------------------------- #
def _candidate_path(state_root: Path, cid: str) -> Path:
    return state_root / "candidates" / f"{cid}.json"


def _occurrence_count(state_root: Path, cid: str) -> int:
    count = 0
    occ_dir = state_root / "occurrences"
    for f in occ_dir.glob("OCC-*.json"):
        try:
            if _load_json(f).get("candidate_id") == cid:
                count += 1
        except (OSError, json.JSONDecodeError):
            continue
    return count


def _find_reusable_candidate(state_root: Path, skill_name: str, pattern_key: str, scope: str) -> dict | None:
    for f in sorted((state_root / "candidates").glob("CAND-*.json")):
        try:
            cand = _load_json(f)
        except (OSError, json.JSONDecodeError):
            continue
        if (cand.get("skill_name") == skill_name and cand.get("pattern_key") == pattern_key
                and cand.get("scope") == scope and cand.get("status") not in TERMINAL):
            return cand
    return None


def _validate_observation(obs: Any, *, prefixes: dict[str, str]) -> dict:
    _require(isinstance(obs, dict), "observation must be a single JSON object")
    out: dict[str, Any] = {}
    _require(obs.get("type") in TYPES, f"type must be one of {sorted(TYPES)}")
    out["type"] = obs["type"]
    skill_name = obs.get("skill_name")
    _require(isinstance(skill_name, str) and 1 <= len(skill_name) <= 64, "skill_name invalid")
    out["skill_name"] = skill_name
    pk = obs.get("pattern_key")
    _require(isinstance(pk, str) and bool(PATTERN_KEY_RE.match(pk)), "pattern_key invalid")
    out["pattern_key"] = pk
    _require(obs.get("source_trust") in SOURCE_TRUST, "source_trust invalid")
    out["source_trust"] = obs["source_trust"]
    _require(obs.get("scope") in SCOPES, "scope invalid")
    out["scope"] = obs["scope"]
    _require(obs.get("risk") in RISKS, "risk invalid")
    out["risk"] = obs["risk"]
    for field in ("observation", "provenance", "hypothesis", "applicability", "evaluation_plan"):
        text = _check_free_text(obs.get(field), field)
        out[field] = _sanitize_text(text, prefixes)
    changes = obs.get("proposed_changes")
    _require(isinstance(changes, list) and len(changes) >= 1, "proposed_changes must be a non-empty list")
    norm_changes = []
    for ch in changes:
        _require(isinstance(ch, dict), "each proposed change must be an object")
        _require(set(ch.keys()) == {"path", "operation"}, "proposed change keys must be exactly {path, operation}")
        _require(ch["operation"] in OPERATIONS, f"operation must be one of {sorted(OPERATIONS)}")
        validate_change_path(ch["path"], skill_name)
        norm_changes.append({"path": ch["path"], "operation": ch["operation"]})
    out["proposed_changes"] = norm_changes
    evidence = obs.get("evidence", [])
    _require(isinstance(evidence, list), "evidence must be a list")
    norm_ev = []
    for ev in evidence:
        _require(isinstance(ev, dict) and set(ev.keys()) == {"ref", "sha256", "source_trust"},
                 "evidence entry keys must be exactly {ref, sha256, source_trust}")
        ref = ev["ref"]
        _require(isinstance(ref, str) and ref != "", "evidence.ref required")
        refp = Path(ref)
        _require(not refp.is_absolute() and ".." not in refp.parts, f"evidence ref must be confined: {ref}")
        _require(isinstance(ev["sha256"], str) and bool(SHA256_RE.match(ev["sha256"])), "evidence.sha256 invalid")
        _require(ev["source_trust"] in SOURCE_TRUST, "evidence.source_trust invalid")
        norm_ev.append({"ref": ref, "sha256": ev["sha256"], "source_trust": ev["source_trust"]})
    out["evidence"] = norm_ev
    return out


def record_candidate(observation: Any, *, skill_root: Path, project_root: Path, state_root: Path) -> dict:
    """Validate/sanitize one observation, append an immutable occurrence, and
    create or update its candidate. occurrence_count is derived from files."""
    verify_schema_integrity()
    home = Path.home()
    canonical = Path(skill_root).expanduser().resolve().parent
    prefixes = _prefix_map(home=home, canonical=canonical, project=Path(project_root).resolve())
    norm = _validate_observation(observation, prefixes=prefixes)
    now = _now()

    existing = _find_reusable_candidate(state_root, norm["skill_name"], norm["pattern_key"], norm["scope"])
    if existing is not None:
        cid = existing["id"]
        first_seen = existing.get("first_seen", now)
    else:
        cid = _new_id("CAND")
        first_seen = now

    occ_id = _new_id("OCC")
    occurrence = {
        "id": occ_id,
        "candidate_id": cid,
        "skill_name": norm["skill_name"],
        "pattern_key": norm["pattern_key"],
        "scope": norm["scope"],
        "source_trust": norm["source_trust"],
        "observed_at": now,
        "observation": norm["observation"],
    }
    _dump_json_atomic(state_root / "occurrences" / f"{occ_id}.json", occurrence)

    count = _occurrence_count(state_root, cid)
    candidate = {
        "id": cid,
        "type": norm["type"],
        "status": existing["status"] if existing else "candidate",
        "skill_name": norm["skill_name"],
        "pattern_key": norm["pattern_key"],
        "source_trust": norm["source_trust"],
        "scope": norm["scope"],
        "risk": norm["risk"],
        "approval_required": True,
        "observed_at": now,
        "first_seen": first_seen,
        "last_seen": now,
        "occurrence_count": count,
        "observation": norm["observation"],
        "provenance": norm["provenance"],
        "hypothesis": norm["hypothesis"],
        "applicability": norm["applicability"],
        "evaluation_plan": norm["evaluation_plan"],
        "proposed_changes": norm["proposed_changes"],
        "evidence": norm["evidence"],
    }
    if existing:
        candidate["verification"] = existing.get("verification", {})
        candidate["approvals"] = existing.get("approvals", [])
    _dump_json_atomic(_candidate_path(state_root, cid), candidate)
    return candidate


def load_candidate(state_root: Path, cid: str) -> dict:
    path = _candidate_path(state_root, cid)
    if not path.is_file():
        raise InputError(f"candidate not found: {cid}")
    return _load_json(path)


def _transition(candidate: dict, new_status: str) -> None:
    cur = candidate["status"]
    if cur == new_status:
        return
    allowed = TRANSITIONS.get(cur, set())
    if new_status not in allowed:
        raise PolicyError(f"illegal transition {cur} -> {new_status}")
    candidate["status"] = new_status


# --------------------------------------------------------------------------- #
# Staging
# --------------------------------------------------------------------------- #
def _snapshot_bundle(canonical_root: Path, project_root: Path,
                     skill_name: str, excludes: tuple[str, ...]) -> dict[str, Any]:
    return {
        "canonical": {skill_name: curated_tree_hash(canonical_root, excludes)},
        "project": {
            skill_name: curated_tree_hash(
                project_root / ".agents" / "skills" / skill_name, excludes
            )
        },
    }


def stage_candidate(candidate_id: str, *, canonical_root: Path, project_root: Path, state_root: Path,
                    deployment_entries: list[str] | None = None,
                    excludes: Iterable[str] | None = None) -> dict:
    """Snapshot one self-contained canonical skill and create its worktree.

    The immutable ``snapshots/<id>/canonical-skill`` copy is the evaluation
    baseline. Sibling skills or shared directories are neither read nor deployed.
    """
    candidate = load_candidate(state_root, candidate_id)
    if candidate["status"] in TERMINAL:
        raise PolicyError(f"cannot stage terminal candidate ({candidate['status']})")
    ex = _norm_excludes(excludes)
    canonical_root = Path(canonical_root).expanduser().resolve()
    skill_name = candidate["skill_name"]
    entries = deployment_entries or [skill_name]
    _require(entries == [skill_name],
             "deploymentEntries must contain only the self-contained skill")

    snap_dir = state_root / "snapshots" / candidate_id
    snap_dir.mkdir(parents=True, exist_ok=True)
    bundle = _snapshot_bundle(canonical_root, Path(project_root).resolve(), skill_name, ex)
    # Immutable canonical skill copy — the evaluation baseline.
    snap_skill = snap_dir / "canonical-skill"
    _copy_tree(canonical_root, snap_skill, ex)
    snapshot = {
        "candidate_id": candidate_id,
        "staged_at": _now(),
        "skill_name": skill_name,
        "deployment_entries": entries,
        "canonical_skill_hash": curated_tree_hash(canonical_root, ex),
        "snapshot_skill_hash": curated_tree_hash(snap_skill, ex),
        "bundle": bundle,
    }
    _dump_json_atomic(snap_dir / "snapshot.json", snapshot)

    worktree = state_root / "worktrees" / candidate_id / skill_name
    _copy_tree(canonical_root, worktree, ex)
    result = {"candidate_id": candidate_id, "worktree": str(worktree),
              "snapshot": str(snap_dir / "snapshot.json"),
              "canonical_skill_hash": snapshot["canonical_skill_hash"]}
    return result


# --------------------------------------------------------------------------- #
# Command execution + validators
# --------------------------------------------------------------------------- #
def _subst(token: str, mapping: dict[str, str]) -> str:
    for k, v in mapping.items():
        token = token.replace("{" + k + "}", v)
    return token


def _run(argv: list[str], cwd: Path) -> dict[str, Any]:
    proc = subprocess.run(argv, cwd=str(cwd), capture_output=True, text=True, timeout=MAX_CMD_TIMEOUT_S)
    return {"argv": argv, "exit": proc.returncode,
            "stdout": proc.stdout[-4000:], "stderr": proc.stderr[-4000:]}


def _run_validators(commands: list[list[str]], skill_root: Path) -> list[dict]:
    results = []
    mapping = {"skill_root": str(skill_root)}
    for cmd in commands:
        argv = [_subst(str(tok), mapping) for tok in cmd]
        results.append(_run(argv, skill_root))
    return results


def _validate_deployed_copies(commands: list[list[str]], copies: list[tuple[str, Path, Path | None]]) -> tuple[bool, dict[str, Any]]:
    """Provision runtime dependencies, then validate active copies."""
    results: dict[str, Any] = {}
    ok = True
    for role, deployed, runtime_src in copies:
        _provision_runtime_deps(deployed, runtime_src)
        res = _run_validators(commands, deployed)
        results[role] = res
        if not all(v["exit"] == 0 for v in res):
            ok = False
    return ok, results


def _run_behavioral(behavioral: dict, skill_root: Path, report_path: Path,
                    manifest_path: Path) -> dict:
    """Run one behavioral report. The manifest is supplied EXPLICITLY (identical
    for baseline and candidate), never re-selected from each root."""
    mapping = {"skill_root": str(skill_root), "manifest": str(manifest_path),
               "report": str(report_path)}
    argv = [_subst(str(tok), mapping) for tok in behavioral["runner"]]
    run = _run(argv, skill_root)
    run["report"] = str(report_path)
    run["manifest"] = str(manifest_path)
    return run


# Runtime dependency directories are excluded from curated hashes/copies (they are
# gitignored build artifacts), but validators/behavioral runners need them. The
# ephemeral eval layout symlinks these from a live source so a run can resolve
# them without ever entering a candidate hash.
_RUNTIME_DEP_DIRS = ("node_modules",)


def _provision_runtime_deps(layout_skill: Path, runtime_src: Path | None) -> None:
    """Symlink known runtime dependency dirs from runtime_src into the layout at
    matching relative paths. No-op when runtime_src is None or lacks them."""
    if runtime_src is None or not runtime_src.exists():
        return
    for dirpath, dirnames, _files in os.walk(runtime_src):
        for name in list(dirnames):
            if name in _RUNTIME_DEP_DIRS:
                real = Path(dirpath) / name
                rel = real.relative_to(runtime_src)
                dest = layout_skill / rel
                if not dest.exists() and not dest.is_symlink():
                    dest.parent.mkdir(parents=True, exist_ok=True)
                    os.symlink(real.resolve(), dest)
                dirnames.remove(name)  # don't descend into deps


def _carry_runtime_deps(from_tree: Path, to_tree: Path) -> None:
    """Copy excluded runtime dependency dirs into a replacement tree.

    The source is never mutated: compensation can restore its complete preimage.
    Symlinked dependency dirs stay symlinks; real dirs are copied.
    """
    if not from_tree.exists():
        return
    for dirpath, dirnames, _files in os.walk(from_tree, followlinks=False):
        for name in list(dirnames):
            if name not in _RUNTIME_DEP_DIRS:
                continue
            src = Path(dirpath) / name
            rel = src.relative_to(from_tree)
            dest = to_tree / rel
            if not dest.exists() and not dest.is_symlink():
                dest.parent.mkdir(parents=True, exist_ok=True)
                if src.is_symlink():
                    os.symlink(os.readlink(src), dest)
                else:
                    _copy_runtime_dir(src, dest)
            dirnames.remove(name)


def _copy_runtime_dir(src: Path, dest: Path) -> None:
    """Deep-copy a runtime dependency directory without following symlinks."""
    shutil.copytree(src, dest, symlinks=True)


@contextmanager
def _eval_layout(skill_src: Path, skill_name: str, excludes: tuple[str, ...],
                 runtime_src: Path | None = None) -> Iterator[Path]:
    """Build an ephemeral copy of one self-contained skill.

    Runtime dependency directories such as node_modules may be symlinked from
    the active canonical copy, but no sibling skill or shared directory is read.
    """
    tmp = Path(tempfile.mkdtemp(prefix="si-eval-"))
    try:
        layout_skill = tmp / skill_name
        _copy_tree(skill_src, layout_skill, excludes)
        _provision_runtime_deps(layout_skill, runtime_src)
        yield layout_skill
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


# --------------------------------------------------------------------------- #
# Diff / manifest generation
# --------------------------------------------------------------------------- #
def _proposal_diff_and_manifest(old_root: Path, new_root: Path, excludes: tuple[str, ...]) -> tuple[str, dict]:
    old_files = dict(_iter_tree_files(old_root, excludes))
    new_files = dict(_iter_tree_files(new_root, excludes))
    all_paths = sorted(set(old_files) | set(new_files))
    diff_lines: list[str] = []
    manifest: dict[str, Any] = {"files": {}}
    for rel in all_paths:
        old_p = old_files.get(rel)
        new_p = new_files.get(rel)
        old_bytes = old_p.read_bytes() if old_p else b""
        new_bytes = new_p.read_bytes() if new_p else b""
        old_hash = _sha256_bytes(old_bytes) if old_p else None
        new_hash = _sha256_bytes(new_bytes) if new_p else None
        if old_hash == new_hash:
            continue
        manifest["files"][rel] = {"old": old_hash, "new": new_hash}
        try:
            old_text = old_bytes.decode("utf8")
            new_text = new_bytes.decode("utf8")
        except UnicodeDecodeError:
            raise InputError(f"binary change rejected: {rel}")
        diff_lines.extend(unified_diff(
            old_text.splitlines(keepends=True), new_text.splitlines(keepends=True),
            fromfile=f"a/{rel}", tofile=f"b/{rel}"))
    return ("".join(diff_lines), manifest)


def _frontmatter_field(root: Path, field: str) -> str | None:
    skill_md = root / "SKILL.md"
    if not skill_md.is_file():
        return None
    text = skill_md.read_text(encoding="utf8")
    m = re.match(r"^---\n(.*?)\n---\n", text, re.DOTALL)
    if not m:
        return None
    for line in m.group(1).splitlines():
        mm = re.match(rf"^{re.escape(field)}\s*:\s*(.*)$", line)
        if mm:
            return mm.group(1).strip().strip('"').strip("'")
    return None


def _direct_regressions(old_report_path: Path, new_report_path: Path) -> list[str]:
    """Per-case parity computed directly from two behavioral reports: any case
    that passed in old and fails in new is a regression. Independent of the
    comparator's root-identity heuristic."""
    if not (old_report_path.is_file() and new_report_path.is_file()):
        return ["missing report"]
    try:
        old = {c["id"]: c for c in _load_json(old_report_path).get("cases", [])}
        new = {c["id"]: c for c in _load_json(new_report_path).get("cases", [])}
    except (OSError, json.JSONDecodeError, KeyError):
        return ["unreadable report"]
    regressed = []
    for cid, oc in old.items():
        nc = new.get(cid)
        if oc.get("passed") is True and (nc is None or nc.get("passed") is not True):
            regressed.append(cid)
    return sorted(regressed)

# --------------------------------------------------------------------------- #
# Evaluation
# --------------------------------------------------------------------------- #
def evaluate_candidate(candidate_id: str, *, config: dict, canonical_home: Path, project_root: Path,
                       state_root: Path, trigger_report: dict | None = None) -> dict:
    """Evaluate a self-contained staged skill against its immutable baseline.

    Baseline and candidate run in separate ephemeral copies with one explicit
    manifest and zero permitted regressions. No sibling skill or shared package
    is consulted. Validators remain responsible for isolating OS, network, and
    subprocess effects. ``verified`` is true only if every gate passes.
    """
    verify_schema_integrity()
    candidate = load_candidate(state_root, candidate_id)
    skill_name = candidate["skill_name"]
    ex = _norm_excludes(config.get("syncExcludes"))
    snap_dir = state_root / "snapshots" / candidate_id
    snap_file = snap_dir / "snapshot.json"
    if not snap_file.is_file():
        raise InputError(f"candidate not staged: {candidate_id}")
    snapshot = _load_json(snap_file)
    baseline_skill = snap_dir / "canonical-skill"
    entries = snapshot.get("deployment_entries") or config["deploymentEntries"]
    _require(entries == [skill_name],
             "deploymentEntries must contain only the self-contained skill")
    runtime_src = Path(canonical_home).expanduser().resolve() / skill_name
    worktree = state_root / "worktrees" / candidate_id / skill_name
    if not worktree.exists() or not baseline_skill.exists():
        raise InputError(f"candidate not staged: {candidate_id}")

    reasons: list[str] = []
    checks: dict[str, Any] = {}

    # 0. Structural confinement: no symlinks/special files anywhere in the tree.
    assert_no_special_files(worktree, ex)

    # 1. Undeclared tree changes: baseline snapshot vs candidate worktree.
    declared = {ch["path"] for ch in candidate["proposed_changes"]}
    _diff, manifest = _proposal_diff_and_manifest(baseline_skill, worktree, ex)
    changed = set(manifest["files"].keys())
    # Only added/replaced paths are confined to UTF-8 text (JSON must parse).
    _assert_changed_paths_text(worktree, sorted(changed))
    undeclared = sorted(changed - declared)
    checks["changed_paths"] = sorted(changed)
    checks["undeclared_paths"] = undeclared
    if undeclared:
        reasons.append(f"undeclared tree changes: {undeclared}")
    for ch in candidate["proposed_changes"]:
        validate_change_path(ch["path"], skill_name)

    # 2. Protected-change gates (snapshot vs worktree).
    name_desc_changed = False
    for field in ("name", "description"):
        if _frontmatter_field(baseline_skill, field) != _frontmatter_field(worktree, field):
            name_desc_changed = True
    checks["name_or_description_changed"] = name_desc_changed
    trigger_report_valid = False
    if trigger_report is not None:
        try:
            validate_trigger_report(trigger_report, skill_name=skill_name)
            trigger_report_valid = True
        except (InputError, PolicyError) as exc:
            reasons.append(f"invalid no-regression trigger report: {exc}")
    checks["trigger_report_valid"] = trigger_report_valid
    if name_desc_changed and not trigger_report_valid:
        reasons.append("name/description change requires a valid no-regression trigger report")
    exec_changes = sorted(p for p in changed if _is_executable_change(p))
    checks["executable_changes"] = exec_changes
    if exec_changes:
        plan = candidate.get("evaluation_plan", "")
        if not any(Path(p).name in plan or p in plan for p in exec_changes):
            reasons.append("executable-script change requires a focused command named in evaluation_plan")

    # 3. Trust gate: untrusted-only evidence cannot verify.
    trusts = {candidate["source_trust"], *(e["source_trust"] for e in candidate.get("evidence", []))}
    corroborated = bool(trusts & CORROBORATING_TRUST)
    checks["corroborated"] = corroborated
    if not corroborated:
        reasons.append("no project-owned/authenticated-user corroboration")

    proposal_dir = state_root / "proposals" / candidate_id
    proposal_dir.mkdir(parents=True, exist_ok=True)
    behavioral = config["behavioral"]
    old_report_path = proposal_dir / "baseline-behavioral.json"
    new_report_path = proposal_dir / "candidate-behavioral.json"
    comparison_path = proposal_dir / "comparison.json"
    validators: list[dict] = []
    old_run: dict[str, Any] = {}
    new_run: dict[str, Any] = {}
    comparison: dict[str, Any] = {}

    # 4-6 run in separate ephemeral copies of the self-contained skill.
    with _eval_layout(baseline_skill, skill_name, ex, runtime_src) as base_root, \
         _eval_layout(worktree, skill_name, ex, runtime_src) as cand_root:
        manifest_rel = behavioral["manifest"]
        baseline_manifest_bytes = (baseline_skill / manifest_rel).read_bytes()
        base_manifest = (base_root / manifest_rel).resolve()
        cand_manifest = (cand_root / manifest_rel).resolve()
        base_manifest.parent.mkdir(parents=True, exist_ok=True)
        cand_manifest.parent.mkdir(parents=True, exist_ok=True)
        base_manifest.write_bytes(baseline_manifest_bytes)
        cand_manifest.write_bytes(baseline_manifest_bytes)

        # Both runs use the same runtime dependency source for parity.
        checks["runtime_deps_source"] = str(runtime_src) if runtime_src else None

        # 4. Validate the candidate tree.
        validators = _run_validators(config["validationCommands"], cand_root)
        validators_ok = all(v["exit"] == 0 for v in validators)
        checks["validators_ok"] = validators_ok
        if not validators_ok:
            reasons.append("candidate validators failed")

        # 5. Behavioral: baseline vs candidate; each reads its own root's manifest
        # (identical bytes → identical manifestHash), staying within confinement.
        old_run = _run_behavioral(behavioral, base_root, old_report_path, base_manifest)
        new_run = _run_behavioral(behavioral, cand_root, new_report_path, cand_manifest)
        behavioral_ok = (old_run["exit"] == 0 and new_run["exit"] == 0
                         and old_report_path.is_file() and new_report_path.is_file())
        candidate_all_pass = False
        if new_report_path.is_file():
            try:
                summ = _load_json(new_report_path)["summary"]
                candidate_all_pass = summ["passed"] == summ["total"]
            except (KeyError, json.JSONDecodeError, OSError):
                candidate_all_pass = False
        checks["behavioral_ran"] = behavioral_ok
        checks["candidate_all_pass"] = candidate_all_pass
        if not candidate_all_pass:
            reasons.append("candidate behavioral cases did not all pass")

        # 6. Comparison via the skill's local eval_compare.py (zero regressions).
        if old_report_path.is_file() and new_report_path.is_file():
            cmp_argv = [str(t) for t in behavioral["comparator"]] + [
                "--old-report", str(old_report_path), "--new-report", str(new_report_path),
                "--old-root", str(base_root), "--new-root", str(cand_root),
                "--manifest", str(base_manifest), "--out", str(comparison_path)]
            cmp_run = _run(cmp_argv, cand_root)
            checks["comparator_exit"] = cmp_run["exit"]
            checks["comparator_ok"] = cmp_run["exit"] == 0
            if cmp_run["exit"] == 0 and comparison_path.is_file():
                try:
                    comparison = _load_json(comparison_path)
                except (OSError, json.JSONDecodeError):
                    comparison = {}
        # 7. Expanded run: exercise the CANDIDATE's OWN manifest in a fresh layout
        # (baseline bytes were written over cand_root above), so a declared new
        # case is actually run. All cases must pass.
        expanded_report_path = proposal_dir / "expanded-behavioral.json"
        expanded_run: dict[str, Any] = {}
        expanded_all_pass = False
        with _eval_layout(worktree, skill_name, ex, runtime_src) as exp_root:
            exp_manifest = (exp_root / manifest_rel).resolve()
            expanded_run = _run_behavioral(behavioral, exp_root, expanded_report_path, exp_manifest)
            if expanded_report_path.is_file():
                try:
                    esumm = _load_json(expanded_report_path)["summary"]
                    expanded_all_pass = esumm["passed"] == esumm["total"]
                except (KeyError, json.JSONDecodeError, OSError):
                    expanded_all_pass = False
        checks["expanded_all_pass"] = expanded_all_pass
        if not expanded_all_pass:
            reasons.append("candidate's own behavioral manifest did not all pass")
    # No-regression gate. The shipped comparator defines root identity using ONLY
    # SKILL.md + capabilities.json, so ANY candidate that changes only other files
    # (references/, scripts/, evals/, transforms/, ...) reports "inconclusive-
    # identical-roots" even with a real tree delta. We therefore ALSO compute
    # per-case parity directly from the two reports (no case may go true→false)
    # and accept identical-roots only with a real tree delta plus that parity.
    regressions = comparison.get("regressions", [])
    conclusion = comparison.get("conclusion")
    direct_regressions = _direct_regressions(old_report_path, new_report_path)
    checks["regressions"] = regressions
    checks["direct_regressions"] = direct_regressions
    checks["conclusion"] = conclusion
    if not checks.get("comparator_ok"):
        reasons.append("comparator did not exit cleanly")
    if conclusion == "compared":
        if regressions:
            reasons.append(f"behavioral regressions: {regressions}")
    elif conclusion == "inconclusive-identical-roots":
        if not changed:
            reasons.append("no tree delta (empty change)")
        if direct_regressions:
            reasons.append(f"per-case regressions: {direct_regressions}")
    else:
        reasons.append(f"comparison not conclusive: {conclusion}")
    if direct_regressions:
        # Belt-and-suspenders: a direct regression fails closed under any conclusion.
        if not any(r.startswith("per-case regressions") for r in reasons):
            reasons.append(f"per-case regressions: {direct_regressions}")

    verified = not reasons
    # Write proposal artifacts.
    (proposal_dir / "proposal.diff").write_text(_diff, encoding="utf8")
    _dump_json_atomic(proposal_dir / "proposal-manifest.json", manifest)
    evaluation = {
        "candidate_id": candidate_id,
        "verified": verified,
        "evaluated_at": _now(),
        "validators": validators,
        "behavioral": {"baseline": old_run, "candidate": new_run, "expanded": expanded_run},
        "comparison": comparison,
        "checks": checks,
        "reasons": reasons,
    }
    _dump_json_atomic(proposal_dir / "evaluation.json", evaluation)

    # Update candidate status (refresh preimages each evaluation). The recorded
    # canonical hash is the LIVE tree, so apply can detect drift since staging.
    live_canonical_hash = curated_tree_hash(
        Path(canonical_home).expanduser().resolve() / skill_name, ex)
    candidate["verification"] = {
        "verified": verified, "evaluated_at": evaluation["evaluated_at"],
        "canonical_skill_hash": live_canonical_hash,
        "snapshot_skill_hash": snapshot.get("snapshot_skill_hash"),
        "candidate_skill_hash": curated_tree_hash(worktree, ex),
        "reasons": reasons,
    }
    if verified:
        if candidate["status"] == "candidate":
            _transition(candidate, "verified")
    else:
        # An unverified candidate stays reusable; do not force a terminal state.
        if candidate["status"] == "verified":
            candidate["status"] = "candidate"
    _dump_json_atomic(_candidate_path(state_root, candidate_id), candidate)
    return evaluation


# --------------------------------------------------------------------------- #
# Approval
# --------------------------------------------------------------------------- #
def record_approval(candidate_id: str, *, approver: str, decision: str, note: str | None,
                    state_root: Path) -> dict:
    """Append an immutable approval record. This represents a direct authenticated
    user/maintainer instruction; the agent must never self-authorize it."""
    verify_schema_integrity()
    _require(decision in {"approve", "reject"}, "decision must be approve|reject")
    _require(isinstance(approver, str) and approver.strip() != "", "approver required")
    candidate = load_candidate(state_root, candidate_id)
    record = {
        "candidate_id": candidate_id,
        "decision": decision,
        "approver": approver,
        "note": note or "",
        "decided_at": _now(),
    }
    apr_path = state_root / "approvals" / f"{candidate_id}.{uuid.uuid4().hex}.json"
    _dump_json_atomic(apr_path, record)

    if decision == "approve":
        if candidate["status"] not in {"verified", "approved"}:
            raise PolicyError(f"cannot approve candidate in status {candidate['status']}")
        _transition(candidate, "approved")
    else:
        if candidate["status"] not in TERMINAL:
            candidate["status"] = "rejected"
    candidate.setdefault("approvals", []).append(record)
    _dump_json_atomic(_candidate_path(state_root, candidate_id), candidate)
    return record


# --------------------------------------------------------------------------- #
# Dual-copy transaction primitive
# --------------------------------------------------------------------------- #
@contextmanager
def _locks(parents: list[Path]) -> Iterator[None]:
    ordered = sorted({str(p.resolve()) for p in parents})
    fds = []
    try:
        for parent in ordered:
            Path(parent).mkdir(parents=True, exist_ok=True)
            lock_path = Path(parent) / ".self-improvement.tx.lock"
            fd = os.open(str(lock_path), os.O_WRONLY | os.O_CREAT, 0o600)
            fcntl.flock(fd, fcntl.LOCK_EX)
            fds.append(fd)
        yield
    finally:
        for fd in reversed(fds):
            try:
                fcntl.flock(fd, fcntl.LOCK_UN)
            finally:
                os.close(fd)


def _append_journal(tx_path: Path, tx: dict, entry: dict) -> None:
    tx["journal"].append({**entry, "at": _now()})
    _dump_json_atomic(tx_path, tx)


def _deploy(targets: list[dict], *, state_root: Path, tx_id: str, candidate_id: str,
            excludes: tuple[str, ...],
            post_validate: "Callable[[], tuple[bool, Any]] | None" = None) -> dict:
    """Journaled, locked, compensating two-rename deployment of directory targets.

    Each target: {role, dst: Path, src: Path, recognized: set[str] | None}.
    recognized=None means the target must be absent (creation only).

    post_validate (if given) runs WHILE LOCKS ARE HELD after every activation;
    a False result triggers compensation before returning. On success, backups
    are ARCHIVED into state (not deleted) so rollback can restore preimages.
    """
    tx_path = state_root / "transactions" / f"{tx_id}.json"
    tx = {
        "transaction_id": tx_id,
        "candidate_id": candidate_id,
        "state": "prepared",
        "created_at": _now(),
        "targets": [],
        "journal": [],
    }
    parents = [t["dst"].parent for t in targets]

    with _locks(parents):
        # Preimage/drift recheck while holding locks.
        for t in targets:
            dst = t["dst"]
            pre = curated_tree_hash(dst, excludes)
            recognized = t["recognized"]
            if pre is None:
                pass  # creation allowed
            elif recognized is not None and pre in recognized:
                pass  # replacement allowed
            else:
                tx["state"] = "conflict"
                conflict = {"role": t["role"], "path": str(dst), "found_hash": pre,
                            "recognized": sorted(recognized) if recognized else None}
                tx["targets"].append({"role": t["role"], "path": str(dst),
                                      "preimage_hash": pre, "postimage_hash": None,
                                      "conflict": conflict})
                _dump_json_atomic(tx_path, tx)
                raise ConflictError(f"project-copy-drift at {dst}: {pre}")
            t["preimage_hash"] = pre

        # Stage every destination beside its target. Failure is terminal but has
        # no live-tree effect; remove all partial stages before returning.
        staged: list[tuple[dict, Path]] = []
        stage_paths: list[Path] = []
        def cleanup_stages() -> None:
            for stage in stage_paths:
                shutil.rmtree(stage, ignore_errors=True)
        try:
            for t in targets:
                dst = t["dst"]
                stage = dst.parent / f".{dst.name}.{tx_id}.stage"
                stage_paths.append(stage)
                _copy_tree(t["src"], stage, excludes)
                if not stage.is_dir():
                    raise ApplyFailed(f"staging failed for {dst}")
                staged.append((t, stage))
        except Exception as exc:
            cleanup_stages()
            tx["state"] = "failed"
            _dump_json_atomic(tx_path, tx)
            if isinstance(exc, ApplyFailed):
                raise
            raise ApplyFailed(f"staging failed: {exc}") from exc

        tx["state"] = "applying"
        _dump_json_atomic(tx_path, tx)

        applied: list[dict] = []
        try:
            for t, stage in staged:
                dst = t["dst"]
                backup = dst.parent / f".{dst.name}.{tx_id}.backup"
                staged_hash = curated_tree_hash(stage, excludes)
                if staged_hash is None:
                    raise ApplyFailed(f"staged tree disappeared for {dst}")
                _append_journal(tx_path, tx, {"op": "begin", "role": t["role"], "path": str(dst)})
                had_backup = False
                try:
                    if dst.exists():
                        os.rename(dst, backup)
                        had_backup = True
                        _fsync_dir(dst.parent)
                        _append_journal(tx_path, tx, {"op": "backup", "role": t["role"],
                                                      "path": str(dst), "backup": str(backup)})
                    os.rename(stage, dst)
                except Exception:
                    if had_backup and backup.exists() and not dst.exists():
                        os.rename(backup, dst)
                        _fsync_dir(dst.parent)
                    raise
                current = {"role": t["role"], "path": str(dst), "backup": str(backup),
                           "had_backup": had_backup, "preimage_hash": t.get("preimage_hash"),
                           "postimage_hash": staged_hash}
                applied.append(current)
                _fsync_dir(dst.parent)
                if had_backup:
                    _carry_runtime_deps(backup, dst)
                _append_journal(tx_path, tx, {"op": "activate", "role": t["role"],
                                              "path": str(dst),
                                              "postimage_hash": staged_hash})

            # Post-validation runs UNDER LOCKS, before we finalize/cleanup.
            if post_validate is not None:
                ok, detail = post_validate()
                tx["post_validation"] = {"ok": ok, "detail": detail}
                if not ok:
                    raise ApplyFailed("post-apply validation failed")
        except ApplyFailed as exc:
            try:
                if applied:
                    _compensate(applied, tx_path, tx, excludes)
                    if tx["state"] != "rolled-back":
                        raise ApplyFailed(f"{exc}; compensation conflicted") from exc
                    raise ApplyFailed(f"{exc}; compensated") from exc
                tx["state"] = "failed"
                _dump_json_atomic(tx_path, tx)
                raise
            finally:
                cleanup_stages()
        except Exception as exc:  # compensate in reverse journal order.
            try:
                if applied:
                    _compensate(applied, tx_path, tx, excludes)
                    if tx["state"] != "rolled-back":
                        raise ApplyFailed(f"deploy failed; compensation conflicted: {exc}") from exc
                    raise ApplyFailed(f"deploy failed, compensated: {exc}") from exc
                tx["state"] = "failed"
                _dump_json_atomic(tx_path, tx)
                raise ApplyFailed(f"deploy failed before activation: {exc}") from exc
            finally:
                cleanup_stages()

        # Two-phase archive: copy+verify every preimage first while retaining all
        # parent-local backups. Only after every archive is durable do we remove
        # locals and switch transaction pointers; compensation always has a
        # same-filesystem backup during the fallible phase.
        archive_dir = state_root / "archive" / tx_id
        archive_dir.mkdir(parents=True, exist_ok=True)
        archived: list[tuple[dict, Path, Path]] = []
        try:
            for a in applied:
                if not a["had_backup"]:
                    continue
                src_backup = Path(a["backup"])
                dst_backup = archive_dir / f"{a['role'].replace('/', '_').replace(':', '_')}.backup"
                _copy_tree(src_backup, dst_backup, excludes)
                _fsync_dir(dst_backup.parent)
                if curated_tree_hash(dst_backup, excludes) != a["preimage_hash"]:
                    raise ApplyFailed(f"archived preimage hash mismatch for {a['path']}")
                archived.append((a, src_backup, dst_backup))
        except Exception as exc:
            try:
                _compensate(applied, tx_path, tx, excludes)
                if tx["state"] != "rolled-back":
                    raise ApplyFailed(f"archive failed; compensation conflicted: {exc}") from exc
                for a in applied:
                    expected = archive_dir / f"{a['role'].replace('/', '_').replace(':', '_')}.backup"
                    shutil.rmtree(expected, ignore_errors=True)
                try:
                    archive_dir.rmdir()
                except OSError:
                    pass
                if isinstance(exc, ApplyFailed):
                    raise ApplyFailed(f"{exc}; compensated") from exc
                raise ApplyFailed(f"archive failed, compensated: {exc}") from exc
            finally:
                cleanup_stages()
        # Commit durable archive pointers before deleting parent-local backups.
        # Once this record is persisted, cleanup is non-transactional and safe to
        # retry; rollback reads only the verified archive copies.
        archive_by_role = {a["role"]: dst_backup for a, _src, dst_backup in archived}
        committed_targets = [{"role": a["role"], "path": a["path"],
                              "preimage_hash": a["preimage_hash"],
                              "postimage_hash": a["postimage_hash"],
                              "backup": (str(archive_by_role[a["role"]])
                                         if a["had_backup"] else None),
                              "had_backup": a["had_backup"]} for a in applied]
        committed_tx = copy.deepcopy(tx)
        committed_tx["targets"] = committed_targets
        committed_tx["state"] = "applied"
        try:
            _dump_json_atomic(tx_path, committed_tx)
        except Exception as exc:
            try:
                _compensate(applied, tx_path, tx, excludes)
                if tx["state"] != "rolled-back":
                    raise ApplyFailed(f"transaction commit failed; compensation conflicted: {exc}") from exc
                for _a, _src, copied in archived:
                    shutil.rmtree(copied, ignore_errors=True)
                try:
                    archive_dir.rmdir()
                except OSError:
                    pass
                raise ApplyFailed(f"transaction commit failed, compensated: {exc}") from exc
            finally:
                cleanup_stages()
        tx = committed_tx
        for a, committed in zip(applied, committed_targets):
            a["backup"] = committed["backup"]
        try:
            for _a, src_backup, _dst_backup in archived:
                try:
                    shutil.rmtree(src_backup)
                    _fsync_dir(src_backup.parent)
                except OSError:
                    pass
            return tx
        finally:
            cleanup_stages()


def _compensate(applied: list[dict], tx_path: Path, tx: dict, excludes: tuple[str, ...]) -> None:
    conflict = False
    for a in reversed(applied):
        dst = Path(a["path"])
        backup = Path(a["backup"])
        current = curated_tree_hash(dst, excludes)
        if current != a["postimage_hash"]:
            # Concurrent change to our postimage: refuse to delete it.
            _append_journal(tx_path, tx, {"op": "compensate-conflict", "role": a["role"], "path": str(dst)})
            conflict = True
            continue
        failed_post = dst.parent / f".{dst.name}.{tx['transaction_id']}.failed-postimage"
        if failed_post.exists():
            conflict = True
            _append_journal(tx_path, tx, {"op": "compensate-stale-failed-postimage",
                                          "role": a["role"], "path": str(dst)})
            continue
        try:
            if dst.exists():
                os.rename(dst, failed_post)
            if a["had_backup"] and backup.exists():
                os.rename(backup, dst)
            elif not a["had_backup"]:
                pass
            else:
                raise OSError(f"missing compensation backup: {backup}")
        except OSError:
            if failed_post.exists() and not dst.exists():
                try:
                    os.rename(failed_post, dst)
                    _fsync_dir(dst.parent)
                except OSError:
                    pass
            conflict = True
            try:
                _append_journal(tx_path, tx, {"op": "compensate-restore-failed",
                                              "role": a["role"], "path": str(dst)})
            except OSError:
                pass
            continue
        _fsync_dir(dst.parent)
        if failed_post.exists():
            shutil.rmtree(failed_post, ignore_errors=True)
        _fsync_dir(dst.parent)
        _append_journal(tx_path, tx, {"op": "restore", "role": a["role"], "path": str(dst)})
    tx["state"] = "conflict" if conflict else "rolled-back"
    _dump_json_atomic(tx_path, tx)


# --------------------------------------------------------------------------- #
# Apply
# --------------------------------------------------------------------------- #
def _bundle_targets(*, candidate_skill_src: Path, canonical_home: Path, project_root: Path,
                    skill_name: str, deployment_entries: list[str], excludes: tuple[str, ...],
                    snapshot: dict) -> list[dict]:
    _require(deployment_entries == [skill_name],
             "deploymentEntries must contain only the self-contained skill")
    project_skills = Path(project_root).resolve() / ".agents" / "skills"
    canonical_home = Path(canonical_home).resolve()
    candidate_hash = curated_tree_hash(candidate_skill_src, excludes)
    canon_recognized = {snapshot.get("canonical_skill_hash")} - {None}
    proj_recognized = {snapshot.get("canonical_skill_hash"), candidate_hash} - {None}
    return [
        {"role": "canonical-skill", "dst": canonical_home / skill_name,
         "src": candidate_skill_src, "recognized": canon_recognized or None},
        {"role": "project-skill", "dst": project_skills / skill_name,
         "src": candidate_skill_src, "recognized": proj_recognized or None},
    ]


def apply_candidate(candidate_id: str, *, config: dict, canonical_home: Path, project_root: Path,
                    state_root: Path) -> dict:
    """Require verified+approved, journaled two-rename dual-copy transaction with
    post-apply validation, then commit or compensate."""
    verify_schema_integrity()
    candidate = load_candidate(state_root, candidate_id)
    if not candidate.get("verification", {}).get("verified"):
        raise PolicyError("candidate is not verified (run evaluate)")
    if candidate["status"] != "approved":
        raise PolicyError(f"candidate is not approved (status={candidate['status']})")

    ex = _norm_excludes(config.get("syncExcludes"))
    canonical_home = Path(canonical_home).expanduser().resolve()
    skill_name = candidate["skill_name"]
    entries = config["deploymentEntries"]
    worktree = state_root / "worktrees" / candidate_id / skill_name
    if not worktree.exists():
        raise InputError(f"candidate not staged: {candidate_id}")
    snapshot = _load_json(state_root / "snapshots" / candidate_id / "snapshot.json")

    # Recheck preimage before locks (fast-fail); _deploy rechecks under locks.
    current_canon = curated_tree_hash(canonical_home / skill_name, ex)
    if current_canon is not None and current_canon != snapshot.get("canonical_skill_hash"):
        raise ConflictError("canonical skill changed since staging; re-evaluate")

    targets = _bundle_targets(candidate_skill_src=worktree, canonical_home=canonical_home,
                              project_root=project_root, skill_name=skill_name,
                              deployment_entries=entries, excludes=ex, snapshot=snapshot)
    tx_id = _new_id("TX")

    def post_validate() -> tuple[bool, Any]:
        active_canonical = canonical_home / skill_name
        project_copy = Path(project_root).resolve() / ".agents" / "skills" / skill_name
        return _validate_deployed_copies(config["validationCommands"], [
            ("canonical", active_canonical, worktree),
            ("project", project_copy, active_canonical),
        ])

    tx = _deploy(targets, state_root=state_root, tx_id=tx_id, candidate_id=candidate_id,
                 excludes=ex, post_validate=post_validate)

    tx_path = state_root / "transactions" / f"{tx_id}.json"
    _transition(candidate, "applied")
    candidate["applied_transaction"] = tx_id
    _dump_json_atomic(_candidate_path(state_root, candidate_id), candidate)
    return {"transaction_id": tx_id, "state": tx["state"], "targets": tx["targets"]}


# --------------------------------------------------------------------------- #
# Rollback
# --------------------------------------------------------------------------- #
def rollback_transaction(transaction_id: str, *, canonical_home: Path, project_root: Path,
                         state_root: Path, excludes: Iterable[str] | None = None) -> dict:
    """Restore recorded preimages only when every current target equals its
    transaction postimage; otherwise record conflict and refuse."""
    verify_schema_integrity()
    tx_path = state_root / "transactions" / f"{transaction_id}.json"
    if not tx_path.is_file():
        raise InputError(f"transaction not found: {transaction_id}")
    tx = _load_json(tx_path)
    ex = _norm_excludes(excludes)
    parents = [Path(t["path"]).parent for t in tx["targets"]]

    with _locks(parents):
        # First pass: verify no concurrent edits to any postimage.
        for t in tx["targets"]:
            dst = Path(t["path"])
            current = curated_tree_hash(dst, ex)
            if current != t["postimage_hash"]:
                tx["state"] = "conflict"
                _append_journal(tx_path, tx, {"op": "rollback-conflict", "role": t["role"], "path": str(dst)})
                raise ConflictError(f"concurrent edit at {dst}; refusing to erase")
        # Second pass: restore preimages in reverse order. A target that was
        # created fresh (preimage_hash is None, had_backup False) is removed; a
        # replaced target is restored from its archived backup after verifying the
        # backup still hashes to the recorded preimage.
        for t in reversed(tx["targets"]):
            dst = Path(t["path"])
            backup = t.get("backup")
            if t["preimage_hash"] is None:
                # Created fresh in this transaction → rollback removes it.
                if dst.exists():
                    shutil.rmtree(dst)
            elif backup and Path(backup).exists():
                if curated_tree_hash(Path(backup), ex) != t["preimage_hash"]:
                    tx["state"] = "conflict"
                    _append_journal(tx_path, tx, {"op": "rollback-backup-mismatch",
                                                  "role": t["role"], "path": str(dst)})
                    raise ConflictError(f"archived backup for {dst} no longer matches preimage")
                # Stage the archived curated preimage beside the target. Runtime
                # dependencies are environmental and excluded from the archive;
                # copy them from the current postimage before replacing it.
                stage = dst.parent / f".{dst.name}.rollback.{transaction_id}.stage"
                if stage.exists():
                    shutil.rmtree(stage)
                _copy_tree(Path(backup), stage, ex)
                _carry_runtime_deps(dst, stage)
                _fsync_dir(dst.parent)
                if dst.exists():
                    shutil.rmtree(dst)
                os.rename(stage, dst)
            else:
                tx["state"] = "conflict"
                _append_journal(tx_path, tx, {"op": "rollback-missing-backup",
                                              "role": t["role"], "path": str(dst)})
                raise ConflictError(f"no archived backup to restore {dst}")
            _fsync_dir(dst.parent)
            _append_journal(tx_path, tx, {"op": "rollback-restore", "role": t["role"], "path": str(dst)})
        tx["state"] = "rolled-back"
        _dump_json_atomic(tx_path, tx)
    return {"transaction_id": transaction_id, "state": tx["state"]}


# --------------------------------------------------------------------------- #
# Sync
# --------------------------------------------------------------------------- #
def sync_deployment(*, config: dict, canonical_home: Path, project_root: Path, state_root: Path,
                    apply: bool = False) -> dict:
    """Compare or materialize one self-contained project copy.

    ``--apply`` provisions excluded runtime dependencies, validates the active
    project copy under the deployment lock, and compensates on failure.
    """
    verify_schema_integrity()
    ex = _norm_excludes(config.get("syncExcludes"))
    canonical_home = Path(canonical_home).expanduser().resolve()
    project_skills = Path(project_root).resolve() / ".agents" / "skills"
    entries = config["deploymentEntries"]
    skill_name = config["skillName"]
    _require(entries == [skill_name],
             "deploymentEntries must contain only the self-contained skill")

    report = {"apply": apply, "entries": []}
    for entry in entries:
        canonical_hash = curated_tree_hash(canonical_home / entry, ex)
        project_hash = curated_tree_hash(project_skills / entry, ex)
        if project_hash is None:
            status = "missing"
        elif project_hash == canonical_hash:
            status = "in-sync"
        else:
            status = "project-copy-drift"
        report["entries"].append({"entry": entry, "status": status,
                                  "canonical_hash": canonical_hash, "project_hash": project_hash})

    if not apply:
        return report

    # Apply: only materialize missing/in-sync entries; drift is a refusal.
    drift = [e for e in report["entries"] if e["status"] == "project-copy-drift"]
    if drift:
        raise ConflictError(f"project-copy-drift; refusing sync --apply: {[d['entry'] for d in drift]}")

    targets = []
    for e in report["entries"]:
        entry = e["entry"]
        recognized = {e["canonical_hash"]} - {None} if e["status"] == "in-sync" else None
        targets.append({"role": f"sync:{entry}", "dst": project_skills / entry,
                        "src": canonical_home / entry, "recognized": recognized})
    tx_id = _new_id("TX")

    def post_validate() -> tuple[bool, Any]:
        return _validate_deployed_copies(config["validationCommands"], [
            (entry, project_skills / entry, canonical_home / entry)
            for entry in entries
        ])

    tx = _deploy(targets, state_root=state_root, tx_id=tx_id,
                 candidate_id="sync", excludes=ex, post_validate=post_validate)
    report["transaction_id"] = tx_id
    report["state"] = tx["state"]
    # Recompute post-apply statuses.
    for e in report["entries"]:
        e["status"] = "in-sync"
    return report

#!/usr/bin/env python3
"""
sync.py — Synchronize model-loader profiles into downstream coding-agent
model catalogs across nine agents: pi, Feynman, omp (Oh My Pi), OpenCode,
Crush, Forgecode, Hermes Agent, Factory Droid, and ZeroClaw.

Reads every profile JSON from the model-loader profiles directory, derives a
context-window token count for each, keeps ONLY profiles whose context window
is STRICTLY GREATER THAN --min-context (default 102400 = "more than 100k",
binary-k, matching model-loader's own -Nk profile-id convention), and maps
survivors to each target's native model-entry shape:

  * pi       -> ~/.pi/agent/models.json            (JSON), provider "model-loader"
  * feynman  -> ~/.feynman/agent/models.json        (JSON), provider "model-loader"
               (identical schema/adapter to pi, independent path/provider)
  * omp      -> ~/.omp/agent/models.yml             (YAML), provider "llama.cpp"
  * opencode -> ~/.config/opencode/opencode.json    (JSON), provider "model-loader"
  * crush    -> ~/.config/crush/crush.json          (JSON), provider "model-loader"
  * forge    -> active provider.json (see path precedence below), provider "model-loader"
  * hermes   -> $HERMES_HOME/config.yaml, else ~/.hermes/config.yaml, provider "model-loader"
  * droid    -> ~/.factory/settings.json            (JSON), customModels[] "Model Loader · " prefix
  * zeroclaw -> ~/.zeroclaw/config.toml             (TOML), provider+agent alias per profile

Every target receives exactly one OpenAI Chat Completions-compatible managed
provider backed by the model-loader proxy. QuantMind's three-provider
(OpenAI/Anthropic/Google) protocol split is never reproduced here, because
the model-loader proxy exposes every launch profile through the same
OpenAI-compatible endpoint.

`--target` restricts the run to one agent (default: all nine, TARGET_NAMES).
Each target's file is otherwise left untouched: only the managed provider's
model list / map / alias set (per target's native schema) is
replaced; connection fields and unrelated providers/config are preserved. In
`--target all` mode, a target whose config file does not exist is skipped
with a warning (that agent isn't installed on this machine); an
explicitly-requested missing target is a configuration error (exit 2). An
existing-but-malformed target file is always a configuration error, in every
mode — sync.py never invents a brand-new agent config from nothing.

Design references:
  * model-loader profile schema  -> ~/.config/model-loader/profiles/<id>.json
    (schemaVersion 3). Context window lives in args under one of:
       - "ctx-size"       (llama.cpp / beellama / buun, int)
       - "max-model-len"  (vLLM, often a STRING like "262144")
       - "context-length" (sglang, int)
       - "max-ctx"        (dflash / lucebox, int)
    with fallbacks: a `parallel<N>-<W>k` id pattern when args.parallel > 1
    (effective ctx = N * W * 1024), else a last-resort standalone "<N>k" /
    "<N>m" token in the id, then the name.
    Reasoning/thinking support is likewise derived from each profile's
    reasoning args (reasoning-parser/-format/-mode, reasoning on, enable-thinking,
    chat-template-kwargs preserve_thinking/enable_thinking) and advertised on
    every target whose schema carries a reasoning-capability field; an
    overrides file can force it per id.
  * pi/Feynman custom models     -> <agent>/agent/models.json (docs/models.md)
  * omp custom models            -> ~/.omp/agent/models.yml
  * OpenCode custom providers    -> opencode.json `provider.<id>` (npm: @ai-sdk/openai-compatible)
  * Forgecode custom providers   -> provider.json array entries, response_type "OpenAI"
  * Hermes custom providers      -> config.yaml `providers.<id>`
  * model-loader OpenAI proxy    -> http://127.0.0.1:4321/v1 (docs/http-proxy.md)

Default mode is --dry-run. Pass --apply to write. A .bak backup is taken next
to each target file on every successful apply.
"""

from __future__ import annotations

import argparse
import copy
import hashlib
import json
import math
import os
import re
import subprocess
import sys
import tempfile
import tomllib
from pathlib import Path
from typing import Any, Callable, NoReturn

from ruamel.yaml import YAML
from ruamel.yaml.comments import CommentedMap, CommentedSeq

# --------------------------------------------------------------------------- #
# Constants
# --------------------------------------------------------------------------- #

MIN_CONTEXT_DEFAULT = 102_400          # 100 * 1024 — STRICTLY greater than this
DEFAULT_MAX_TOKENS = 32_768
PROXY_URL_DEFAULT = "http://127.0.0.1:4321/v1"

TARGET_NAMES = ("pi", "feynman", "omp", "opencode", "crush", "forge", "hermes",
                "droid", "zeroclaw")

PI_PROVIDER_DEFAULT = "model-loader"
PI_API_KEY_DEFAULT = "model-loader"    # pi requires non-empty; proxy ignores it
PI_API_DEFAULT = "openai-completions"
PI_COMPAT_DEFAULT = {
    "supportsDeveloperRole": False,
    "supportsReasoningEffort": False,
    "maxTokensField": "max_tokens",
}

FEYNMAN_PROVIDER_DEFAULT = "model-loader"   # shares pi's JSON schema/adapter

OMP_PROVIDER_DEFAULT = "llama.cpp"
OMP_API_DEFAULT = "openai-completions"

OPENCODE_PROVIDER_DEFAULT = "model-loader"
OPENCODE_NPM_DEFAULT = "@ai-sdk/openai-compatible"
OPENCODE_API_KEY_DEFAULT = "model-loader"  # inline non-secret placeholder; proxy ignores it

FORGE_PROVIDER_DEFAULT = "model-loader"
FORGE_RESPONSE_TYPE_DEFAULT = "OpenAI"

HERMES_PROVIDER_DEFAULT = "model-loader"

CRUSH_PROVIDER_DEFAULT = "model-loader"
CRUSH_API_KEY_DEFAULT = "model-loader"   # inline non-secret placeholder; proxy ignores it

DROID_PROVIDER_DEFAULT = "generic-chat-completion-api"
DROID_API_KEY_DEFAULT = "model-loader"   # inline non-secret placeholder; proxy ignores it
DROID_DISPLAY_PREFIX = "Model Loader · "  # exclusive ownership marker on customModels[]

ZEROCLAW_ALIAS_PREFIX = "model_loader_"   # reserved provider+agent alias namespace
ZEROCLAW_ALIAS_MAX_LEN = 63                # zeroclaw 0.8.3 validation limit
ZEROCLAW_ALIAS_HASH_LEN = 8
ZEROCLAW_BIN_DEFAULT = "zeroclaw"

# Context-window arg keys, checked in priority order across backends.
CTX_KEYS = ("ctx-size", "max-model-len", "context-length", "max-ctx")

# Tags/words that mark a profile as multimodal (image-capable).
VISION_TAGS = {
    "vision", "vl", "mmproj", "multimodal",
    "ocr", "caption", "captions", "llava", "siglip",
}
VISION_NAME_WORDS = ("vision", "mmproj", "caption", "ocr", "multimodal", "-vl-")

# Profiles that are not chat/completion models and should never be offered
# even if they happen to exceed the context threshold.
NON_CHAT_TAGS = {"embedding", "embeddings", "reranker", "rerank", "reward"}

# Fallback: a profile using --parallel N whose id encodes per-slot context as
# "parallel<N>-<W>k" (e.g. "parallel12-32k" -> 12 * 32 * 1024 = 393216).
PARALLEL_ID_RE = re.compile(r"parallel(\d+)-(\d+)k", re.IGNORECASE)

# Last-resort fallback: a context size encoded as a standalone token like
# "256k", "128k", "768k", "1m". The lookbehind/ahead guard prevents false
# matches inside tokens such as "q4km", "9b", "tp2".
SUFFIX_RE = re.compile(r"(?<![\w])(\d{1,4})([kKmM])(?![A-Za-z0-9])")

# Args (and extraArgs) that signal the model exposes reasoning / "thinking"
# to clients. The model-loader proxy maps a backend's reasoning_content to
# Anthropic/OpenAI thinking blocks, so a profile carrying any of these is
# advertised as reasoning-capable. A value in REASONING_OFF_VALUES (or an
# enable-flag set false) is treated as "no signal".
REASONING_VALUE_KEYS = ("reasoning", "reasoning-format", "reasoning-parser",
                        "reasoning-mode")
REASONING_BOOL_KEYS = ("enable-thinking", "enable-reasoning", "thinking")
REASONING_OFF_VALUES = {"", "none", "off", "false", "no", "0",
                        "disable", "disabled"}

# Fields each target manages on a model entry (used for change detection).
PI_MANAGED_FIELDS = ("id", "name", "contextWindow", "input", "reasoning", "maxTokens")
OMP_MANAGED_FIELDS = ("id", "name", "reasoning", "input", "contextWindow", "maxTokens")
OPENCODE_MANAGED_FIELDS = ("name", "limit", "modalities", "reasoning")
FORGE_MANAGED_FIELDS = ("name", "context_length", "supports_reasoning",
                        "input_modalities", "tools_supported",
                        "supports_parallel_tool_calls")
HERMES_MANAGED_FIELDS = ("context_length", "supports_vision")
CRUSH_MANAGED_FIELDS = ("id", "name", "cost_per_1m_in", "cost_per_1m_out",
                        "cost_per_1m_in_cached", "cost_per_1m_out_cached",
                        "context_window", "default_max_tokens", "can_reason",
                        "supports_attachments")
DROID_MANAGED_FIELDS = ("model", "displayName", "baseUrl", "apiKey", "provider",
                        "maxContextLimit", "maxOutputTokens", "noImageSupport",
                        "reasoningEffort")
ZEROCLAW_MANAGED_FIELDS = ("uri", "model", "wire_api", "context_window",
                            "max_tokens", "think")
ZEROCLAW_AGENT_MANAGED_FIELDS = ("model_provider",)


# --------------------------------------------------------------------------- #
# Small helpers
# --------------------------------------------------------------------------- #

def die(msg: str, code: int = 2) -> NoReturn:
    print(f"error: {msg}", file=sys.stderr)
    sys.exit(code)


def expand(p: str | None) -> str | None:
    return os.path.expanduser(p) if p else None


def to_int(value: Any) -> int | None:
    """Coerce a JSON value (int/float/str/bool) to int tokens, or None."""
    if value is None or isinstance(value, bool):
        return None
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        return int(value) if math.isfinite(value) else None
    if isinstance(value, str):
        s = value.strip()
        if not s:
            return None
        try:
            return int(s)
        except ValueError:
            try:
                f = float(s)
            except ValueError:
                return None
            return int(f) if math.isfinite(f) else None
    return None


def suffix_ctx(text: str) -> int | None:
    """Extract a token count from an '<N>k'/'<N>m' token in text, else None."""
    m = SUFFIX_RE.search(text or "")
    if not m:
        return None
    n = int(m.group(1))
    return n * 1024 if m.group(2).lower() == "k" else n * 1024 * 1024


def _reasoning_truthy(v: Any) -> bool:
    """A reasoning arg value counts as 'on' unless it's an explicit off token."""
    if isinstance(v, bool):
        return v
    if isinstance(v, (int, float)):
        return v != 0
    if isinstance(v, str):
        return v.strip().lower() not in REASONING_OFF_VALUES
    return bool(v)


def _extra_args_map(extra: list) -> dict:
    """Flatten a profile's extraArgs into {flag-without-dashes: value}.
    '--k v' / '--k=v' -> {'k': 'v'}; a bare '--flag' -> {'flag': True}."""
    out: dict[str, Any] = {}
    items = [str(x) for x in (extra or [])]
    i = 0
    while i < len(items):
        tok = items[i]
        if tok.startswith("--"):
            key = tok[2:]
            if "=" in key:
                k, v = key.split("=", 1)
                out[k.lower()] = v
            elif i + 1 < len(items) and not items[i + 1].startswith("--"):
                out[key.lower()] = items[i + 1]
                i += 1
            else:
                out[key.lower()] = True
        i += 1
    return out


def _ctk_thinking(v: Any) -> bool:
    """True if a chat-template-kwargs value enables thinking/reasoning."""
    obj = v
    if isinstance(v, str):
        try:
            obj = json.loads(v)
        except Exception:
            low = v.lower().replace(" ", "")
            return ('"enable_thinking":true' in low
                    or '"preserve_thinking":true' in low
                    or '"thinking":true' in low)
    if isinstance(obj, dict):
        low = {str(k).lower(): val for k, val in obj.items()}
        return any(_reasoning_truthy(low[k]) for k in
                   ("preserve_thinking", "enable_thinking", "thinking",
                    "enable_reasoning") if k in low)
    return False


def detect_reasoning(args: dict, extra: list) -> bool:
    """Derive whether a profile exposes reasoning/thinking, from its args and
    extraArgs (reasoning-parser/-format/-mode, reasoning on/off, enable-thinking,
    chat-template-kwargs preserve_thinking/enable_thinking, reasoning-budget)."""
    merged: dict[str, Any] = {str(k).lower(): v for k, v in (args or {}).items()}
    merged.update(_extra_args_map(extra))  # extraArgs win over args
    for k in REASONING_VALUE_KEYS:
        if k in merged and _reasoning_truthy(merged[k]):
            return True
    for k in REASONING_BOOL_KEYS:
        if k in merged and _reasoning_truthy(merged[k]):
            return True
    if "chat-template-kwargs" in merged and _ctk_thinking(merged["chat-template-kwargs"]):
        return True
    budget = to_int(merged.get("reasoning-budget"))
    if budget and budget > 0:
        return True
    return False


# --------------------------------------------------------------------------- #
# model-loader config (TOML)
# --------------------------------------------------------------------------- #

def load_ml_config(path: str | None) -> dict:
    """Load ~/.config/model-loader/config.toml (best effort)."""
    if not path:
        return {}
    try:
        with open(path, "rb") as fh:
            return tomllib.load(fh)
    except FileNotFoundError:
        return {}
    except Exception as exc:  # malformed TOML — keep going with defaults
        print(f"warn: could not parse model-loader config {path}: {exc}",
              file=sys.stderr)
        return {}


# --------------------------------------------------------------------------- #
# Target registry: path resolution and missing-file policy
# --------------------------------------------------------------------------- #

def resolve_forge_provider_json(args: argparse.Namespace) -> str:
    """Forge's active provider.json, in precedence order: explicit CLI flag,
    $FORGE_CONFIG/provider.json, ~/forge/provider.json (legacy layout, only
    when that directory exists), else ~/.forge/provider.json."""
    explicit = expand(args.forge_provider_json)
    if explicit:
        return explicit
    forge_config = expand(os.environ.get("FORGE_CONFIG"))
    if forge_config:
        return os.path.join(forge_config, "provider.json")
    legacy = Path(expand("~/forge"))
    if legacy.is_dir():
        return str(legacy / "provider.json")
    return expand("~/.forge/provider.json")


def resolve_hermes_config_yml(args: argparse.Namespace) -> str:
    """Hermes config path: explicit CLI flag or env, then
    $HERMES_HOME/config.yaml, else ~/.hermes/config.yaml."""
    explicit = expand(args.hermes_config_yml)
    if explicit:
        return explicit
    explicit_env = expand(os.environ.get("HERMES_CONFIG_YML"))
    if explicit_env:
        return explicit_env
    hermes_home = expand(os.environ.get("HERMES_HOME")) or expand("~/.hermes")
    return os.path.join(hermes_home, "config.yaml")


def resolve_crush_config_json(args: argparse.Namespace) -> str:
    """Crush crush.json path: explicit flag, $CRUSH_CONFIG_JSON,
    $CRUSH_GLOBAL_CONFIG/crush.json, else ~/.config/crush/crush.json."""
    explicit = expand(args.crush_config_json)
    if explicit:
        return explicit
    env = expand(os.environ.get("CRUSH_CONFIG_JSON"))
    if env:
        return env
    global_dir = expand(os.environ.get("CRUSH_GLOBAL_CONFIG"))
    if global_dir:
        return os.path.join(global_dir, "crush.json")
    return expand("~/.config/crush/crush.json")


def resolve_droid_settings_json(args: argparse.Namespace) -> str:
    """Factory Droid settings.json path: explicit flag, $DROID_SETTINGS_JSON,
    $FACTORY_HOME/settings.json, else ~/.factory/settings.json."""
    explicit = expand(args.droid_settings_json)
    if explicit:
        return explicit
    env = expand(os.environ.get("DROID_SETTINGS_JSON"))
    if env:
        return env
    factory_home = expand(os.environ.get("FACTORY_HOME"))
    if factory_home:
        return os.path.join(factory_home, "settings.json")
    return expand("~/.factory/settings.json")


def resolve_zeroclaw_config_toml(args: argparse.Namespace) -> str:
    """ZeroClaw config.toml path: explicit flag, $ZEROCLAW_CONFIG_TOML,
    $ZEROCLAW_CONFIG_DIR/config.toml, else ~/.zeroclaw/config.toml."""
    explicit = expand(args.zeroclaw_config_toml)
    if explicit:
        return explicit
    env = expand(os.environ.get("ZEROCLAW_CONFIG_TOML"))
    if env:
        return env
    config_dir = expand(os.environ.get("ZEROCLAW_CONFIG_DIR"))
    if config_dir:
        return os.path.join(config_dir, "config.toml")
    return expand("~/.zeroclaw/config.toml")


def derive_paths(args: argparse.Namespace) -> dict:
    """Resolve all paths/URLs from flag > env > model-loader config > default."""
    cfg_path = (expand(args.config)
                or expand(os.environ.get("MODEL_LOADER_CONFIG"))
                or expand("~/.config/model-loader/config.toml"))
    cfg = load_ml_config(cfg_path)
    paths_cfg = cfg.get("paths", {}) or {}
    serve_cfg = cfg.get("serve", {}) or {}

    profiles_dir = (
        expand(args.profiles_dir)
        or expand(os.environ.get("MODEL_LOADER_PROFILES_DIR"))
        or expand(paths_cfg.get("profiles_dir"))
        or expand("~/.config/model-loader/profiles")
    )

    pi_models_json = (
        expand(args.pi_models_json)
        or expand(os.environ.get("PI_MODELS_JSON"))
        or expand("~/.pi/agent/models.json")
    )

    feynman_models_json = (
        expand(args.feynman_models_json)
        or expand(os.environ.get("FEYNMAN_MODELS_JSON"))
        or expand("~/.feynman/agent/models.json")
    )

    omp_models_yml = (
        expand(args.omp_models_yml)
        or expand(os.environ.get("OMP_MODELS_YML"))
        or expand("~/.omp/agent/models.yml")
    )

    opencode_config_json = (
        expand(args.opencode_config_json)
        or expand(os.environ.get("OPENCODE_CONFIG_JSON"))
        or expand("~/.config/opencode/opencode.json")
    )

    forge_provider_json = resolve_forge_provider_json(args)
    hermes_config_yml = resolve_hermes_config_yml(args)
    crush_config_json = resolve_crush_config_json(args)
    droid_settings_json = resolve_droid_settings_json(args)
    zeroclaw_config_toml = resolve_zeroclaw_config_toml(args)

    # Proxy URL precedence: flag > env > [serve] host/port > default
    proxy_url = (
        args.proxy_url
        or os.environ.get("MODEL_LOADER_PROXY_URL")
        or _proxy_from_serve(serve_cfg)
        or PROXY_URL_DEFAULT
    )

    return {"config": cfg_path, "profiles_dir": profiles_dir,
            "pi_models_json": pi_models_json,
            "feynman_models_json": feynman_models_json,
            "omp_models_yml": omp_models_yml,
            "opencode_config_json": opencode_config_json,
            "crush_config_json": crush_config_json,
            "forge_provider_json": forge_provider_json,
            "hermes_config_yml": hermes_config_yml,
            "droid_settings_json": droid_settings_json,
            "zeroclaw_config_toml": zeroclaw_config_toml,
            "proxy_url": proxy_url}


def _proxy_from_serve(serve_cfg: dict) -> str | None:
    host = serve_cfg.get("host")
    port = serve_cfg.get("port")
    if not host and port is None:
        return None
    host = host or "127.0.0.1"
    port = port or 4321
    return f"http://{host}:{port}/v1"


def requested_targets(target: str) -> tuple[str, ...]:
    return TARGET_NAMES if target == "all" else (target,)


def target_available(path: str, *, explicit: bool, target: str) -> bool:
    """True if a target's config file exists. Missing + an explicitly
    requested target is a configuration error (exit 2); missing + `--target
    all` is a warning so other installed agents keep syncing."""
    if os.path.isfile(path):
        return True
    if explicit:
        die(f"{target} configuration not found: {path}", 2)
    print(f"warn: skipping {target}; configuration not found: {path}",
          file=sys.stderr)
    return False


# --------------------------------------------------------------------------- #
# Profile enumeration
# --------------------------------------------------------------------------- #

class Profile:
    __slots__ = ("id", "name", "tags", "ctx", "ctx_src", "vision",
                 "reasoning", "nonchat", "path", "error")

    def __init__(self, path: str):
        self.path = path
        self.id = Path(path).stem
        self.name = ""
        self.tags: list[str] = []
        self.ctx: int | None = None
        self.ctx_src: str = ""
        self.vision = False
        self.reasoning = False
        self.nonchat = False
        self.error: str | None = None

    def load(self) -> bool:
        try:
            with open(self.path, "r", encoding="utf-8") as fh:
                # strict=False tolerates literal control chars that some
                # profile writers emit inside description strings.
                data = json.loads(fh.read(), strict=False)
        except Exception as exc:
            self.error = str(exc)
            return False

        self.id = data.get("id") or self.id
        self.name = data.get("name") or self.id
        self.tags = [str(t).lower() for t in (data.get("tags") or [])]
        args = data.get("args") or {}

        # 1) explicit context-window arg (backend-specific key)
        for key in CTX_KEYS:
            if key in args:
                cv = to_int(args[key])
                if cv and cv > 0:
                    self.ctx, self.ctx_src = cv, key
                    break

        # 2) parallel-slot id pattern: "parallel<N>-<W>k" with args.parallel > 1
        if self.ctx is None:
            parallel = to_int(args.get("parallel"))
            if parallel and parallel > 1:
                m = PARALLEL_ID_RE.search(self.id)
                if m:
                    n, w = int(m.group(1)), int(m.group(2))
                    self.ctx, self.ctx_src = n * w * 1024, "parallel-formula"

        # 3) last-resort fallback: standalone <N>k / <N>m token in id, then name
        if self.ctx is None:
            for text in (self.id, self.name):
                cv = suffix_ctx(text)
                if cv and cv > 0:
                    self.ctx, self.ctx_src = cv, "name-suffix"
                    break

        self.vision = self._detect_vision(args)
        self.reasoning = detect_reasoning(args, data.get("extraArgs") or [])
        self.nonchat = any(t in NON_CHAT_TAGS for t in self.tags)
        return True

    def _detect_vision(self, args: dict) -> bool:
        if args.get("mmproj"):
            return True
        if any(t in VISION_TAGS for t in self.tags):
            return True
        hay = f"{self.id} {self.name}".lower()
        return any(w in hay for w in VISION_NAME_WORDS)


def enumerate_profiles(profiles_dir: str) -> list[Profile]:
    pdir = Path(profiles_dir)
    if not pdir.is_dir():
        die(f"profiles directory not found: {profiles_dir}\n"
            f"        set --profiles-dir or MODEL_LOADER_PROFILES_DIR", 2)
    profiles: list[Profile] = []
    for entry in sorted(pdir.iterdir()):
        # only top-level *.json; skip dotfiles, dirs, locks, history
        if entry.name.startswith(".") or not entry.is_file():
            continue
        if entry.suffix.lower() != ".json":
            continue
        profiles.append(Profile(str(entry)))
    return profiles


def select(profiles: list[Profile], min_context: int,
           include_embeddings: bool) -> dict:
    """Partition profiles into pass / skip / undetermined / corrupt."""
    passed: list[Profile] = []
    skipped: list[tuple[Profile, str]] = []
    undetermined: list[Profile] = []
    corrupt: list[Profile] = []
    for p in profiles:
        if not p.load():
            corrupt.append(p)
            continue
        if p.ctx is None:
            undetermined.append(p)
            continue
        if p.ctx <= min_context:
            skipped.append((p, f"ctx={p.ctx} <= {min_context}"))
            continue
        if p.nonchat and not include_embeddings:
            skipped.append((p, "non-chat (embedding/reranker)"))
            continue
        passed.append(p)
    return {"passed": passed, "skipped": skipped,
            "undetermined": undetermined, "corrupt": corrupt}


# --------------------------------------------------------------------------- #
# Overrides (shared target-neutral shape across targets; fields not
# applicable to a target, e.g. "reasoning" for omp or "maxTokens" for
# Forge/Hermes, are ignored by that target's entry builder)
# --------------------------------------------------------------------------- #

def load_overrides(path: str | None) -> dict:
    if not path or not os.path.isfile(path):
        return {}
    try:
        with open(path, "r", encoding="utf-8") as fh:
            data = json.load(fh)
    except Exception as exc:
        print(f"warn: overrides file {path} unreadable: {exc}", file=sys.stderr)
        return {}
    if not isinstance(data, dict):
        return {}
    if "overrides" in data and isinstance(data["overrides"], dict):
        raw = data["overrides"]
    else:
        raw = data
    return {k: v for k, v in raw.items() if not k.startswith("_")}


def seed_overrides(entries_by_id: dict[str, dict], out_path: str) -> int:
    """Capture existing target entries' hand-tuned fields into an overrides
    file. Callers pass entries already translated into the shared
    target-neutral schema (name/reasoning/input/maxTokens); a target whose
    native schema can't express a field simply omits it from the translated
    entry."""
    if os.path.exists(out_path):
        die(f"overrides file already exists: {out_path}\n"
            f"        move it aside or pass a different --overrides path", 2)
    overrides: dict[str, dict[str, Any]] = {}
    for pid, m in entries_by_id.items():
        entry: dict[str, Any] = {}
        for fld in ("name", "reasoning", "input", "maxTokens"):
            if fld in m:
                entry[fld] = m[fld]
        if entry:
            overrides[pid] = entry
    Path(out_path).parent.mkdir(parents=True, exist_ok=True)
    with open(out_path, "w", encoding="utf-8") as fh:
        json.dump({"overrides": overrides}, fh, indent=2)
        fh.write("\n")
    print(f"seeded {len(overrides)} override(s) -> {out_path}")
    print("review it, then re-run sync (the file is read automatically).")
    return len(overrides)


# --------------------------------------------------------------------------- #
# pi / Feynman targets: providers.<provider>.models[] JSON array
# --------------------------------------------------------------------------- #

def _load_json_object(path: str) -> dict:
    try:
        with open(path, "r", encoding="utf-8") as fh:
            data = json.load(fh)
    except FileNotFoundError:
        return {}
    except json.JSONDecodeError as exc:
        die(f"{path} is not valid JSON: {exc}", 2)
    if not isinstance(data, dict):
        die(f"{path} root must be a JSON object", 2)
    return data


def _load_json_list(path: str) -> list:
    try:
        with open(path, "r", encoding="utf-8") as fh:
            data = json.load(fh)
    except FileNotFoundError:
        return []
    except json.JSONDecodeError as exc:
        die(f"{path} is not valid JSON: {exc}", 2)
    if not isinstance(data, list):
        die(f"{path} root must be a JSON array (Forge provider file)", 2)
    return data


def _write_json(path: str, data: Any) -> None:
    p = Path(path)
    p.parent.mkdir(parents=True, exist_ok=True)
    tmp = p.with_suffix(p.suffix + ".tmp")
    payload = json.dumps(data, indent=2, ensure_ascii=False)
    with open(tmp, "w", encoding="utf-8") as fh:
        fh.write(payload)
        if not payload.endswith("\n"):
            fh.write("\n")
        fh.flush()
        os.fsync(fh.fileno())
    with open(tmp, "r", encoding="utf-8") as fh:
        json.load(fh)  # validate round-trip before swapping in
    os.replace(tmp, path)


def pi_provider_default(proxy_url: str) -> dict:
    return {
        "baseUrl": proxy_url,
        "api": PI_API_DEFAULT,
        "apiKey": PI_API_KEY_DEFAULT,
        "compat": copy.deepcopy(PI_COMPAT_DEFAULT),
    }


def build_pi_entry(profile: Profile, overrides: dict, prev: dict | None) -> dict:
    """id/name/contextWindow/input/reasoning are always re-derived from the
    profile (source of truth): reasoning is inferred from the profile's
    reasoning/thinking args (see detect_reasoning). maxTokens is not encoded in
    the profile, so it's inherited from the previous entry when present. An
    explicit override always wins over any derived or inherited value.
    """
    ov = overrides.get(profile.id, {}) or {}
    prev = prev or {}
    entry: dict[str, Any] = {
        "id": ov.get("id", profile.id),
        "name": ov.get("name") or profile.name or profile.id,
        "contextWindow": profile.ctx,
    }
    entry["input"] = ov["input"] if "input" in ov else (
        ["text", "image"] if profile.vision else ["text"])
    entry["reasoning"] = bool(ov["reasoning"]) if "reasoning" in ov else profile.reasoning
    if "maxTokens" in ov:
        entry["maxTokens"] = ov["maxTokens"]
    elif "maxTokens" in prev:
        entry["maxTokens"] = prev["maxTokens"]
    return entry


def managed_view(entry: dict, fields: tuple[str, ...]) -> dict:
    return {k: entry.get(k) for k in fields if k in entry}


def compute_changes(old_by_id: dict[str, dict], new_by_id: dict[str, dict],
                     fields: tuple[str, ...]) -> dict:
    added = sorted(set(new_by_id) - set(old_by_id))
    removed = sorted(set(old_by_id) - set(new_by_id))
    updated, unchanged = [], []
    for pid in sorted(set(new_by_id) & set(old_by_id)):
        if managed_view(new_by_id[pid], fields) != managed_view(old_by_id.get(pid, {}), fields):
            updated.append(pid)
        else:
            unchanged.append(pid)
    return {"added": added, "removed": removed,
            "updated": updated, "unchanged": unchanged}


def sync_pi_family(target: str, path_key: str, provider: str, parts: dict,
                    paths: dict, overrides: dict,
                    args: argparse.Namespace) -> dict:
    """Shared pi-schema adapter for both pi and Feynman: identical JSON
    shape (providers.<provider>.models[]), independent path/provider per
    caller. pi and Feynman never duplicate this serializer."""
    path = paths[path_key]
    data = _load_json_object(path)
    data.setdefault("providers", {})
    provider_block = data["providers"].get(provider)
    provider_existed = bool(provider_block)
    old_models = (provider_block or {}).get("models") or []
    old_by_id: dict[str, dict] = {
        str(m["id"]): m for m in old_models if isinstance(m, dict) and m.get("id")
    }

    if args.seed_overrides:
        return {"seeded": seed_overrides(old_by_id, args.overrides_out)}

    new_entries = [build_pi_entry(p, overrides, old_by_id.get(p.id))
                   for p in parts["passed"]]
    new_entries.sort(key=lambda m: m["id"])
    new_by_id = {m["id"]: m for m in new_entries}

    changes = compute_changes(old_by_id, new_by_id, PI_MANAGED_FIELDS)

    if not provider_existed:
        provider_block = pi_provider_default(paths["proxy_url"])
    elif args.update_provider_config:
        provider_block.update(pi_provider_default(paths["proxy_url"]))
    provider_block["models"] = new_entries
    data["providers"][provider] = provider_block

    wrote = False
    if args.apply:
        _backup(path)
        _write_json(path, data)
        wrote = True

    return {
        "target": target, "path": path, "provider": provider,
        "provider_existed": provider_existed, "changes": changes, "wrote": wrote,
        "entries": new_entries,
    }


def sync_pi(parts: dict, paths: dict, overrides: dict, args: argparse.Namespace) -> dict:
    return sync_pi_family("pi", "pi_models_json", args.pi_provider,
                           parts, paths, overrides, args)


def sync_feynman(parts: dict, paths: dict, overrides: dict, args: argparse.Namespace) -> dict:
    return sync_pi_family("feynman", "feynman_models_json", args.feynman_provider,
                           parts, paths, overrides, args)


# --------------------------------------------------------------------------- #
# omp target: ~/.omp/agent/models.yml
# --------------------------------------------------------------------------- #

def _yaml() -> YAML:
    y = YAML(typ="rt")
    y.preserve_quotes = True
    y.width = 4096
    y.indent(mapping=2, sequence=2, offset=0)
    return y


def _load_yaml(path: str) -> Any:
    y = _yaml()
    if not os.path.isfile(path):
        return None
    with open(path, "r", encoding="utf-8") as fh:
        return y.load(fh)


def _write_yaml(path: str, data: Any) -> None:
    y = _yaml()
    p = Path(path)
    p.parent.mkdir(parents=True, exist_ok=True)
    tmp = p.with_suffix(p.suffix + ".tmp")
    with open(tmp, "w", encoding="utf-8") as fh:
        y.dump(data, fh)
    with open(tmp, "r", encoding="utf-8") as fh:
        _yaml().load(fh)  # validate round-trip before swapping in
    os.replace(tmp, path)


def _flow_list(items: list[str]) -> CommentedSeq:
    seq = CommentedSeq(items)
    seq.fa.set_flow_style()
    return seq


def omp_provider_default(proxy_url: str) -> CommentedMap:
    block = CommentedMap()
    block["baseUrl"] = proxy_url.replace("127.0.0.1", "localhost")
    block["api"] = OMP_API_DEFAULT
    block["auth"] = "none"
    block["models"] = CommentedSeq()
    return block


def build_omp_entry(profile: Profile, overrides: dict,
                     prev: dict | None, default_max_tokens: int) -> CommentedMap:
    ov = overrides.get(profile.id, {}) or {}
    prev = prev or {}
    entry = CommentedMap()
    entry["id"] = ov.get("id", profile.id)
    entry["name"] = ov.get("name") or profile.name or profile.id
    entry["reasoning"] = bool(ov["reasoning"]) if "reasoning" in ov else profile.reasoning
    entry["input"] = _flow_list(
        list(ov["input"]) if "input" in ov
        else (["text", "image"] if profile.vision else ["text"]))
    entry["contextWindow"] = profile.ctx
    if "maxTokens" in ov:
        entry["maxTokens"] = ov["maxTokens"]
    elif "maxTokens" in prev:
        entry["maxTokens"] = prev["maxTokens"]
    else:
        entry["maxTokens"] = default_max_tokens
    return entry


def sync_omp(parts: dict, paths: dict, overrides: dict, args: argparse.Namespace) -> dict:
    root = _load_yaml(paths["omp_models_yml"])
    provider_existed = False
    if root is None:
        root = CommentedMap()
        root["providers"] = CommentedMap()
    root.setdefault("providers", CommentedMap())
    provider_block = root["providers"].get(args.omp_provider)
    if provider_block is not None:
        provider_existed = True
    else:
        provider_block = omp_provider_default(paths["proxy_url"])

    models_seq = provider_block.get("models")
    if models_seq is None:
        models_seq = CommentedSeq()
        provider_block["models"] = models_seq

    old_by_id: dict[str, Any] = {}
    old_by_id_plain: dict[str, dict] = {}
    for item in models_seq:
        if isinstance(item, dict) and item.get("id"):
            old_by_id[item["id"]] = item
            old_by_id_plain[item["id"]] = dict(item)

    if args.seed_overrides:
        return {"seeded": seed_overrides(old_by_id_plain, args.overrides_out)}

    wanted_ids = {p.id for p in parts["passed"]}

    # Update existing entries in place (preserves position/comments) and
    # remove entries whose profile no longer qualifies.
    for idx in range(len(models_seq) - 1, -1, -1):
        item = models_seq[idx]
        pid = item.get("id") if isinstance(item, dict) else None
        if pid not in wanted_ids:
            del models_seq[idx]

    profiles_by_id = {p.id: p for p in parts["passed"]}
    new_by_id_plain: dict[str, dict] = {}
    for pid, item in list(old_by_id.items()):
        if pid not in wanted_ids:
            continue
        fresh = build_omp_entry(profiles_by_id[pid], overrides,
                                 old_by_id_plain.get(pid), args.default_max_tokens)
        for k in ("name", "input", "contextWindow", "maxTokens"):
            item[k] = fresh[k]
        # reasoning is a managed field too; keep it after `name` (as sibling
        # providers do) rather than appended at the tail on first insert.
        if "reasoning" in item:
            item["reasoning"] = fresh["reasoning"]
        else:
            keys = list(item.keys())
            pos = keys.index("name") + 1 if "name" in keys else len(keys)
            item.insert(pos, "reasoning", fresh["reasoning"])
        new_by_id_plain[pid] = dict(item)

    added_ids = sorted(wanted_ids - set(old_by_id))
    for pid in added_ids:
        entry = build_omp_entry(profiles_by_id[pid], overrides, None,
                                 args.default_max_tokens)
        models_seq.append(entry)
        new_by_id_plain[pid] = dict(entry)

    changes = compute_changes(old_by_id_plain, new_by_id_plain, OMP_MANAGED_FIELDS)
    root["providers"][args.omp_provider] = provider_block

    wrote = False
    if args.apply:
        _backup(paths["omp_models_yml"])
        _write_yaml(paths["omp_models_yml"], root)
        wrote = True

    return {
        "target": "omp", "path": paths["omp_models_yml"], "provider": args.omp_provider,
        "provider_existed": provider_existed, "changes": changes, "wrote": wrote,
        "entries": [new_by_id_plain[pid] for pid in sorted(new_by_id_plain)],
    }


def _backup(path: str) -> None:
    if os.path.exists(path):
        try:
            with open(path, "rb") as src, open(path + ".bak", "wb") as dst:
                dst.write(src.read())
        except OSError as exc:
            die(f"could not write backup for {path}: {exc}", 2)


# --------------------------------------------------------------------------- #
# opencode target: ~/.config/opencode/opencode.json
# --------------------------------------------------------------------------- #

def opencode_provider_default(proxy_url: str) -> dict:
    return {
        "npm": OPENCODE_NPM_DEFAULT,
        "name": "Model Loader",
        "options": {"baseURL": proxy_url, "apiKey": OPENCODE_API_KEY_DEFAULT},
        "models": {},
    }


def build_opencode_entry(profile: Profile, overrides: dict, prev: dict | None,
                          default_max_tokens: int) -> dict:
    """name/limit.context/modalities are always re-derived from the profile.
    limit.output isn't encoded in the profile, so it's inherited from the
    previous entry when present, else --default-max-tokens.

    `reasoning` is written as a plain boolean: OpenCode's published config
    schema (https://opencode.ai/config.json, $defs.ProviderConfig.models.*)
    lists "reasoning": {"type": "boolean"} as one of a closed set of allowed
    model keys (additionalProperties: false) alongside "variants". Only the
    boolean is written here — "variants" (a per-provider reasoning-effort
    switch, e.g. {"high": {"options": {"reasoningEffort": ...}}}) is
    npm-package-specific and unverified for @ai-sdk/openai-compatible, so it
    is deliberately never invented for the managed provider.
    """
    ov = overrides.get(profile.id, {}) or {}
    prev_limit = (prev or {}).get("limit") or {}
    output = ov.get("maxTokens", prev_limit.get("output", default_max_tokens))
    return {
        "name": ov.get("name") or profile.name or profile.id,
        "limit": {"context": profile.ctx, "output": output},
        "modalities": {
            "input": list(ov["input"]) if "input" in ov else (
                ["text", "image"] if profile.vision else ["text"]),
            "output": ["text"],
        },
        "reasoning": bool(ov["reasoning"]) if "reasoning" in ov else profile.reasoning,
    }


def _seedable_opencode(entry: dict) -> dict:
    """Translate a native OpenCode model entry into the shared target-neutral
    override schema fields it can express (name/input/maxTokens/reasoning)."""
    out: dict[str, Any] = {}
    if "name" in entry:
        out["name"] = entry["name"]
    if "reasoning" in entry:
        out["reasoning"] = entry["reasoning"]
    modalities = entry.get("modalities") or {}
    if "input" in modalities:
        out["input"] = modalities["input"]
    limit = entry.get("limit") or {}
    if "output" in limit:
        out["maxTokens"] = limit["output"]
    return out


def sync_opencode(parts: dict, paths: dict, overrides: dict, args: argparse.Namespace) -> dict:
    path = paths["opencode_config_json"]
    data = _load_json_object(path)
    data.setdefault("provider", {})
    provider = args.opencode_provider
    provider_block = data["provider"].get(provider)
    provider_existed = bool(provider_block)
    old_models = (provider_block or {}).get("models") or {}
    old_by_id: dict[str, dict] = {
        str(k): v for k, v in old_models.items() if isinstance(v, dict)
    }

    if args.seed_overrides:
        translated = {pid: _seedable_opencode(m) for pid, m in old_by_id.items()}
        return {"seeded": seed_overrides(translated, args.overrides_out)}

    new_by_id: dict[str, dict] = {
        p.id: build_opencode_entry(p, overrides, old_by_id.get(p.id),
                                    args.default_max_tokens)
        for p in parts["passed"]
    }

    changes = compute_changes(old_by_id, new_by_id, OPENCODE_MANAGED_FIELDS)

    if not provider_existed:
        provider_block = opencode_provider_default(paths["proxy_url"])
    elif args.update_provider_config:
        provider_block.update(opencode_provider_default(paths["proxy_url"]))
    provider_block["models"] = {mid: new_by_id[mid] for mid in sorted(new_by_id)}
    data["provider"][provider] = provider_block

    wrote = False
    if args.apply:
        _backup(path)
        _write_json(path, data)
        wrote = True

    return {
        "target": "opencode", "path": path, "provider": provider,
        "provider_existed": provider_existed, "changes": changes, "wrote": wrote,
        "entries": [new_by_id[mid] for mid in sorted(new_by_id)],
    }


# --------------------------------------------------------------------------- #
# forge target: active provider.json (see resolve_forge_provider_json)
# --------------------------------------------------------------------------- #

def forge_chat_completions_url(proxy_url: str) -> str:
    """Normalize the shared proxy base URL to exactly one
    '/v1/chat/completions' suffix; Forge requires a complete Chat
    Completions endpoint, not just a base URL."""
    base = proxy_url.rstrip("/")
    if base.endswith("/chat/completions"):
        return base
    if base.endswith("/v1"):
        return base + "/chat/completions"
    return base + "/v1/chat/completions"


def forge_provider_default(proxy_url: str, provider: str) -> dict:
    return {
        "id": provider,
        "url_param_vars": [],
        "response_type": FORGE_RESPONSE_TYPE_DEFAULT,
        "url": forge_chat_completions_url(proxy_url),
        "models": [],
        "auth_methods": [],
    }


def build_forge_entry(profile: Profile, overrides: dict) -> dict:
    ov = overrides.get(profile.id, {}) or {}
    return {
        "id": profile.id,
        "name": ov.get("name") or profile.name or profile.id,
        "description": "Model Loader OpenAI-compatible local profile",
        "context_length": profile.ctx,
        "tools_supported": True,
        "supports_parallel_tool_calls": True,
        "supports_reasoning": bool(ov["reasoning"]) if "reasoning" in ov else profile.reasoning,
        "input_modalities": list(ov["input"]) if "input" in ov else (
            ["text", "image"] if profile.vision else ["text"]),
    }


def _seedable_forge(entry: dict) -> dict:
    """Translate a native Forge model entry into the shared target-neutral
    override schema fields it can express (name/input/reasoning; Forge has
    no output-token-limit field, so maxTokens is never seeded)."""
    out: dict[str, Any] = {}
    if "name" in entry:
        out["name"] = entry["name"]
    if "supports_reasoning" in entry:
        out["reasoning"] = entry["supports_reasoning"]
    if "input_modalities" in entry:
        out["input"] = entry["input_modalities"]
    return out


def sync_forge(parts: dict, paths: dict, overrides: dict, args: argparse.Namespace) -> dict:
    path = paths["forge_provider_json"]
    root = _load_json_list(path)
    provider = args.forge_provider
    idx = next((i for i, p in enumerate(root)
                if isinstance(p, dict) and p.get("id") == provider), None)
    provider_existed = idx is not None
    provider_block = copy.deepcopy(root[idx]) if provider_existed \
        else forge_provider_default(paths["proxy_url"], provider)

    old_models = provider_block.get("models") or []
    old_by_id: dict[str, dict] = {
        str(m["id"]): m for m in old_models if isinstance(m, dict) and m.get("id")
    }

    if args.seed_overrides:
        translated = {pid: _seedable_forge(m) for pid, m in old_by_id.items()}
        return {"seeded": seed_overrides(translated, args.overrides_out)}

    new_entries = [build_forge_entry(p, overrides) for p in parts["passed"]]
    new_entries.sort(key=lambda m: m["id"])
    new_by_id = {m["id"]: m for m in new_entries}

    changes = compute_changes(old_by_id, new_by_id, FORGE_MANAGED_FIELDS)

    if not provider_existed or args.update_provider_config:
        # A brand-new provider always starts from the no-auth default. An
        # explicit refresh also resets connection/auth fields to that same
        # default — auth fields are otherwise preserved verbatim (including
        # api_key_vars, which has no place in the no-auth default at all, so
        # a plain dict-merge could never clear it).
        provider_block = forge_provider_default(paths["proxy_url"], provider)
    provider_block["models"] = new_entries

    if provider_existed:
        root[idx] = provider_block
    else:
        root.append(provider_block)

    wrote = False
    if args.apply:
        _backup(path)
        _write_json(path, root)
        wrote = True

    return {
        "target": "forge", "path": path, "provider": provider,
        "provider_existed": provider_existed, "changes": changes, "wrote": wrote,
        "entries": new_entries,
    }


# --------------------------------------------------------------------------- #
# hermes target: $HERMES_HOME/config.yaml or ~/.hermes/config.yaml
# --------------------------------------------------------------------------- #

def hermes_provider_default(proxy_url: str) -> CommentedMap:
    block = CommentedMap()
    block["base_url"] = proxy_url
    block["name"] = "Model Loader"
    block["discover_models"] = False
    block["models"] = CommentedMap()
    return block


def build_hermes_entry(profile: Profile, overrides: dict) -> CommentedMap:
    """Hermes consumes a static keyed model map, not a generated discovery
    cache: context_length feeds the per-model context helper and
    supports_vision drives image routing. Reasoning is not written here —
    Hermes has no per-model provider-config reasoning field; its runtime
    uses route/model-specific reasoning gates instead."""
    ov = overrides.get(profile.id, {}) or {}
    entry = CommentedMap()
    entry["context_length"] = profile.ctx
    modalities = ov.get("input")
    entry["supports_vision"] = ("image" in modalities) if modalities is not None \
        else profile.vision
    return entry


def _seedable_hermes(entry: dict) -> dict:
    """Translate a native Hermes model entry into the shared target-neutral
    override schema fields it can express: only input/vision. Hermes has no
    per-model name/maxTokens/reasoning field, so those are never seeded."""
    out: dict[str, Any] = {}
    if "supports_vision" in entry:
        out["input"] = ["text", "image"] if entry["supports_vision"] else ["text"]
    return out


def sync_hermes(parts: dict, paths: dict, overrides: dict, args: argparse.Namespace) -> dict:
    path = paths["hermes_config_yml"]
    root = _load_yaml(path)
    if root is None:
        root = CommentedMap()
    if not isinstance(root, dict):
        die(f"{path} root must be a YAML mapping (Hermes config)", 2)
    root.setdefault("providers", CommentedMap())
    providers = root["providers"]
    provider = args.hermes_provider
    provider_block = providers.get(provider)
    provider_existed = provider_block is not None
    if not provider_existed:
        provider_block = hermes_provider_default(paths["proxy_url"])
    elif args.update_provider_config:
        provider_block["base_url"] = paths["proxy_url"]
        provider_block["name"] = "Model Loader"

    models_map = provider_block.get("models")
    if not isinstance(models_map, dict):
        models_map = CommentedMap()
    old_by_id_plain: dict[str, dict] = {
        str(k): dict(v) if isinstance(v, dict) else {} for k, v in models_map.items()
    }

    if args.seed_overrides:
        translated = {pid: _seedable_hermes(m) for pid, m in old_by_id_plain.items()}
        return {"seeded": seed_overrides(translated, args.overrides_out)}

    new_models = CommentedMap()
    new_by_id_plain: dict[str, dict] = {}
    for p in sorted(parts["passed"], key=lambda pp: pp.id):
        entry = build_hermes_entry(p, overrides)
        new_models[p.id] = entry
        new_by_id_plain[p.id] = dict(entry)

    changes = compute_changes(old_by_id_plain, new_by_id_plain, HERMES_MANAGED_FIELDS)

    provider_block["models"] = new_models
    # Always managed, regardless of --update-provider-config: keep the
    # synchronized static catalog authoritative, never let Hermes silently
    # replace it with a live /models probe.
    provider_block["discover_models"] = False
    providers[provider] = provider_block

    wrote = False
    if args.apply:
        _backup(path)
        _write_yaml(path, root)
        wrote = True

    return {
        "target": "hermes", "path": path, "provider": provider,
        "provider_existed": provider_existed, "changes": changes, "wrote": wrote,
        "entries": [new_by_id_plain[pid] for pid in sorted(new_by_id_plain)],
    }


# --------------------------------------------------------------------------- #
# crush target: ~/.config/crush/crush.json (providers.<id>, openai-compat)
# --------------------------------------------------------------------------- #

def normalize_v1_base(proxy_url: str) -> str:
    """Normalize the shared proxy URL to exactly one '/v1' base suffix (never
    a trailing '/chat/completions'). Crush/Droid/ZeroClaw all want the base
    URL, not the full chat endpoint."""
    base = proxy_url.rstrip("/")
    if base.endswith("/chat/completions"):
        base = base[: -len("/chat/completions")].rstrip("/")
    if base.endswith("/v1"):
        return base
    return base + "/v1"


def crush_provider_default(proxy_url: str, provider: str) -> dict:
    return {
        "id": provider,
        "name": "Model Loader",
        "type": "openai-compat",
        "base_url": normalize_v1_base(proxy_url),
        "api_key": CRUSH_API_KEY_DEFAULT,
        "discover_models": False,
        "models": [],
    }


def build_crush_entry(profile: Profile, overrides: dict, prev: dict | None,
                       default_max_tokens: int) -> dict:
    """All Crush-required Model fields. Costs are zero (local proxy), context/
    output/capabilities are re-derived; default_max_tokens is inherited from
    the existing entry, then --default-max-tokens. No reasoning_levels/
    default_reasoning_effort/options/pricing is invented for a local profile."""
    ov = overrides.get(profile.id, {}) or {}
    prev = prev or {}
    max_tokens = ov.get("maxTokens", prev.get("default_max_tokens", default_max_tokens))
    return {
        "id": profile.id,
        "name": ov.get("name") or profile.name or profile.id,
        "cost_per_1m_in": 0,
        "cost_per_1m_out": 0,
        "cost_per_1m_in_cached": 0,
        "cost_per_1m_out_cached": 0,
        "context_window": profile.ctx,
        "default_max_tokens": max_tokens,
        "can_reason": bool(ov["reasoning"]) if "reasoning" in ov else profile.reasoning,
        "supports_attachments": ("image" in ov["input"]) if "input" in ov else profile.vision,
    }


def _seedable_crush(entry: dict) -> dict:
    out: dict[str, Any] = {}
    if "name" in entry:
        out["name"] = entry["name"]
    if "can_reason" in entry:
        out["reasoning"] = entry["can_reason"]
    if "supports_attachments" in entry:
        out["input"] = ["text", "image"] if entry["supports_attachments"] else ["text"]
    if "default_max_tokens" in entry:
        out["maxTokens"] = entry["default_max_tokens"]
    return out


def sync_crush(parts: dict, paths: dict, overrides: dict, args: argparse.Namespace) -> dict:
    path = paths["crush_config_json"]
    data = _load_json_object(path)
    providers = data.get("providers")
    if providers is None:
        providers = {}
        data["providers"] = providers
    elif not isinstance(providers, dict):
        die(f"{path} 'providers' must be a JSON object", 2)
    provider = args.crush_provider
    provider_block = providers.get(provider)
    provider_existed = provider_block is not None
    if provider_existed and not isinstance(provider_block, dict):
        die(f"{path} provider '{provider}' must be a JSON object", 2)
    old_models = (provider_block or {}).get("models")
    if provider_existed and old_models is not None and not isinstance(old_models, list):
        die(f"{path} provider '{provider}' models must be a JSON array", 2)
    old_models = old_models or []
    old_by_id: dict[str, dict] = {
        str(m["id"]): m for m in old_models if isinstance(m, dict) and m.get("id")
    }

    if args.seed_overrides:
        translated = {pid: _seedable_crush(m) for pid, m in old_by_id.items()}
        return {"seeded": seed_overrides(translated, args.overrides_out)}

    new_entries = [build_crush_entry(p, overrides, old_by_id.get(p.id),
                                      args.default_max_tokens) for p in parts["passed"]]
    new_entries.sort(key=lambda m: m["id"])
    new_by_id = {m["id"]: m for m in new_entries}
    changes = compute_changes(old_by_id, new_by_id, CRUSH_MANAGED_FIELDS)

    if not provider_existed:
        provider_block = crush_provider_default(paths["proxy_url"], provider)
    elif args.update_provider_config:
        provider_block.update(crush_provider_default(paths["proxy_url"], provider))
    # discover_models is always forced false: a live /v1/models probe would
    # reintroduce the sub-threshold profiles the sync deliberately excludes.
    provider_block["discover_models"] = False
    provider_block["models"] = new_entries
    providers[provider] = provider_block

    wrote = False
    if args.apply:
        _backup(path)
        _write_json(path, data)
        wrote = True
    return {
        "target": "crush", "path": path, "provider": provider,
        "provider_existed": provider_existed, "changes": changes, "wrote": wrote,
        "entries": new_entries,
    }


# --------------------------------------------------------------------------- #
# droid target: ~/.factory/settings.json (customModels[] array)
# --------------------------------------------------------------------------- #

def is_managed_droid_entry(entry: Any) -> bool:
    """A Factory Droid custom model is managed iff its displayName carries the
    exclusive 'Model Loader · ' prefix. Model id, baseUrl, and provider are
    never ownership criteria on their own: ids can collide and a user may
    point unrelated custom models at the same proxy."""
    return (isinstance(entry, dict)
            and isinstance(entry.get("displayName"), str)
            and entry["displayName"].startswith(DROID_DISPLAY_PREFIX))


def build_droid_entry(profile: Profile, overrides: dict, prev: dict | None,
                       default_max_tokens: int, proxy_url: str,
                       preserve_key: bool = True) -> dict:
    ov = overrides.get(profile.id, {}) or {}
    prev = prev or {}
    name = ov.get("name") or profile.name or profile.id
    input_mods = list(ov["input"]) if "input" in ov else (
        ["text", "image"] if profile.vision else ["text"])
    reasoning = bool(ov["reasoning"]) if "reasoning" in ov else profile.reasoning
    prev_key = prev.get("apiKey")
    api_key = (prev_key if preserve_key and isinstance(prev_key, str) and prev_key.strip()
               else DROID_API_KEY_DEFAULT)
    entry: dict[str, Any] = {
        "model": profile.id,
        "displayName": DROID_DISPLAY_PREFIX + name,
        "baseUrl": normalize_v1_base(proxy_url),
        "apiKey": api_key,
        "provider": DROID_PROVIDER_DEFAULT,
        "maxContextLimit": profile.ctx,
        "maxOutputTokens": ov.get("maxTokens", prev.get("maxOutputTokens", default_max_tokens)),
    }
    if "image" not in input_mods:
        entry["noImageSupport"] = True
    if reasoning:
        entry["reasoningEffort"] = "high"
    return entry


def _seedable_droid(entry: dict) -> dict:
    out: dict[str, Any] = {}
    dn = entry.get("displayName")
    if isinstance(dn, str):
        out["name"] = dn[len(DROID_DISPLAY_PREFIX):] if dn.startswith(DROID_DISPLAY_PREFIX) else dn
    if "maxOutputTokens" in entry:
        out["maxTokens"] = entry["maxOutputTokens"]
    out["input"] = ["text"] if entry.get("noImageSupport") else ["text", "image"]
    out["reasoning"] = "reasoningEffort" in entry
    return out


def sync_droid(parts: dict, paths: dict, overrides: dict, args: argparse.Namespace) -> dict:
    path = paths["droid_settings_json"]
    data = _load_json_object(path)
    custom = data.get("customModels")
    if custom is None:
        custom = []
        data["customModels"] = custom
    elif not isinstance(custom, list):
        die(f"{path} 'customModels' must be a JSON array", 2)

    managed_old: dict[str, dict] = {}
    for e in custom:
        if is_managed_droid_entry(e) and isinstance(e.get("model"), str) and e["model"]:
            managed_old[e["model"]] = e

    if args.seed_overrides:
        translated = {mid: _seedable_droid(m) for mid, m in managed_old.items()}
        return {"seeded": seed_overrides(translated, args.overrides_out)}

    preserve_key = not args.update_provider_config
    new_by_id = {
        p.id: build_droid_entry(p, overrides, managed_old.get(p.id),
                                 args.default_max_tokens, paths["proxy_url"], preserve_key)
        for p in parts["passed"]
    }
    changes = compute_changes(managed_old, new_by_id, DROID_MANAGED_FIELDS)

    unmanaged = [e for e in custom if not is_managed_droid_entry(e)]
    new_managed = [new_by_id[mid] for mid in sorted(new_by_id)]
    data["customModels"] = unmanaged + new_managed

    wrote = False
    if args.apply:
        _backup(path)
        _write_json(path, data)
        os.chmod(path, 0o600)
        if os.path.exists(path + ".bak"):
            os.chmod(path + ".bak", 0o600)
        wrote = True
    return {
        "target": "droid", "path": path, "provider": DROID_PROVIDER_DEFAULT,
        "provider_existed": bool(managed_old), "changes": changes, "wrote": wrote,
        "entries": new_managed,
    }


# --------------------------------------------------------------------------- #
# zeroclaw target: ~/.zeroclaw/config.toml (one provider+agent alias per
# profile, patched via the native `zeroclaw config patch` RFC 6902 CLI)
# --------------------------------------------------------------------------- #

def _zeroclaw_slug(pid: str) -> str:
    s = re.sub(r"[^a-z0-9]+", "_", pid.lower())
    return re.sub(r"_+", "_", s).strip("_")


def _zeroclaw_alias(pid: str) -> str:
    slug = _zeroclaw_slug(pid)
    alias = ZEROCLAW_ALIAS_PREFIX + slug
    if len(alias) <= ZEROCLAW_ALIAS_MAX_LEN:
        return alias
    digest = hashlib.sha256(pid.encode("utf-8")).hexdigest()[:ZEROCLAW_ALIAS_HASH_LEN]
    slug_len = ZEROCLAW_ALIAS_MAX_LEN - len(ZEROCLAW_ALIAS_PREFIX) - len(digest) - 1
    return ZEROCLAW_ALIAS_PREFIX + slug[:slug_len].rstrip("_") + "_" + digest


def _jp_escape(token: str) -> str:
    return token.replace("~", "~0").replace("/", "~1")


def _load_toml(path: str) -> dict:
    try:
        with open(path, "rb") as fh:
            return tomllib.load(fh)
    except FileNotFoundError:
        return {}
    except tomllib.TOMLDecodeError as exc:
        die(f"{path} is not valid TOML: {exc}", 2)


def _zc_table(parent: dict, key: str, path: str, label: str) -> dict | None:
    v = parent.get(key)
    if v is None:
        return None
    if not isinstance(v, dict):
        die(f"{path} '{label}' must be a TOML table", 2)
    return v


def _zc_custom(root: dict, path: str) -> dict:
    providers = _zc_table(root, "providers", path, "providers") or {}
    models = _zc_table(providers, "models", path, "providers.models") or {}
    return _zc_table(models, "custom", path, "providers.models.custom") or {}


def _zc_agents(root: dict, path: str) -> dict:
    return _zc_table(root, "agents", path, "agents") or {}


def _zc_live(provider: dict) -> bool:
    # A LIVE managed model requires identity: a non-empty `model`. This keys
    # change reporting (old_by_id) and distinguishes an active provider from
    # decommissioned/partial residue (owned leaves blanked, api_key/extras
    # preserved).
    mid = provider.get("model")
    return isinstance(mid, str) and bool(mid)


def _zc_has_owned(provider: dict) -> bool:
    # True when any adapter-owned leaf is present, so stale cleanup can scrub
    # both a decommissioned live provider AND partial-failure residue that has
    # stray owned leaves but no identity. ZEROCLAW_MANAGED_FIELDS is canonical.
    return any(f in provider for f in ZEROCLAW_MANAGED_FIELDS)


def _zc_run(zeroclaw_bin: str, cfg_dir: str, args: list[str]) -> None:
    """Run a `zeroclaw --config-dir <dir> <args...>` subcommand, mapping a
    missing binary or nonzero exit to configuration error code 2."""
    try:
        proc = subprocess.run(
            [zeroclaw_bin, "--config-dir", cfg_dir, *args],
            check=False, capture_output=True, text=True)
    except OSError as exc:
        die(f"could not run zeroclaw ({zeroclaw_bin}): {exc}", 2)
    if proc.returncode != 0:
        die(f"zeroclaw {' '.join(args)} failed (exit {proc.returncode}): "
            f"{(proc.stderr or proc.stdout).strip()}", 2)


def build_zeroclaw_provider(profile: Profile, overrides: dict, prev: dict | None,
                             default_max_tokens: int, proxy_url: str) -> dict:
    ov = overrides.get(profile.id, {}) or {}
    prev = prev or {}
    obj = dict(prev)  # preserve api_key / any extra field
    obj["uri"] = normalize_v1_base(proxy_url)
    obj["model"] = profile.id
    obj["wire_api"] = "chat_completions"
    obj["context_window"] = profile.ctx
    if "maxTokens" in ov:
        obj["max_tokens"] = ov["maxTokens"]
    elif "max_tokens" in prev:
        obj["max_tokens"] = prev["max_tokens"]
    else:
        obj["max_tokens"] = default_max_tokens
    reasoning = bool(ov["reasoning"]) if "reasoning" in ov else profile.reasoning
    if reasoning:
        obj["think"] = True
    else:
        obj.pop("think", None)
    return obj


def build_zeroclaw_agent(alias: str, prev: dict | None) -> dict:
    # A managed agent points at its sibling custom provider via model_provider.
    # On CREATION we also stamp enabled=false: real `zeroclaw` (0.8.3) rejects
    # an *enabled* agent that lacks a risk_profile referencing a configured
    # [risk_profiles.<alias>], and this skill never invents risk profiles,
    # workspaces, memory, or credentials. The operator opts the agent in by
    # setting enabled=true + a risk_profile themselves; a refresh of an
    # existing agent preserves that choice (enabled is left untouched) and every
    # other pre-existing leaf.
    obj = dict(prev or {})
    obj["model_provider"] = f"custom.{alias}"
    if prev is None:
        obj["enabled"] = False
    return obj


def sync_zeroclaw(parts: dict, paths: dict, overrides: dict, args: argparse.Namespace) -> dict:
    path = paths["zeroclaw_config_toml"]
    # `zeroclaw config patch --config-dir <parent>` always targets
    # <parent>/config.toml, so a differently-named file would be read here but
    # never written by the CLI — reject the mismatch instead of silently
    # patching the wrong file.
    if os.path.basename(path) != "config.toml":
        die(f"zeroclaw config path must be named config.toml: {path}", 2)
    root = _load_toml(path)
    custom = _zc_custom(root, path)
    agents = _zc_agents(root, path)

    managed_providers: dict[str, dict] = {}
    for alias, obj in custom.items():
        if not alias.startswith(ZEROCLAW_ALIAS_PREFIX):
            continue
        if not isinstance(obj, dict):
            die(f"{path} managed provider '{alias}' must be a TOML table", 2)
        managed_providers[alias] = obj
    managed_agents: dict[str, dict] = {}
    for alias, obj in agents.items():
        if not alias.startswith(ZEROCLAW_ALIAS_PREFIX):
            continue
        if not isinstance(obj, dict):
            die(f"{path} managed agent '{alias}' must be a TOML table", 2)
        managed_agents[alias] = obj

    if args.seed_overrides:
        consolidated: dict[str, dict] = {}
        for alias, obj in managed_providers.items():
            mid = obj.get("model")
            if not isinstance(mid, str) or not mid:
                continue
            seed: dict[str, Any] = {}
            if "max_tokens" in obj:
                seed["maxTokens"] = obj["max_tokens"]
            seed["reasoning"] = bool(obj.get("think"))
            if mid in consolidated and consolidated[mid] != seed:
                die(f"zeroclaw seed conflict: model '{mid}' has divergent managed values", 2)
            consolidated[mid] = seed
        return {"seeded": seed_overrides(consolidated, args.overrides_out)}

    # A residue alias (owned leaves blanked, api_key/extras preserved) or a
    # partial-failure fragment (owned leaves but no `model`) is NOT a live
    # managed model: it reads as decommissioned. Live detection requires a
    # non-empty `model`, so old_by_id keys cleanly by model id.
    live_providers = {a: o for a, o in managed_providers.items() if _zc_live(o)}
    old_by_id: dict[str, dict] = {}
    model_aliases: dict[str, str] = {}
    for alias, obj in live_providers.items():
        mid = obj["model"]
        if mid in model_aliases:
            die(f"zeroclaw duplicate managed model '{mid}' across aliases "
                f"'{model_aliases[mid]}' and '{alias}'", 2)
        model_aliases[mid] = alias
        old_by_id[mid] = obj

    alias_by_id: dict[str, str] = {}
    seen: dict[str, str] = {}
    for p in parts["passed"]:
        alias = _zeroclaw_alias(p.id)
        if alias in seen and seen[alias] != p.id:
            die(f"zeroclaw alias collision: '{p.id}' and '{seen[alias]}' both map to '{alias}'", 2)
        seen[alias] = p.id
        alias_by_id[p.id] = alias

    new_by_id: dict[str, dict] = {}
    for p in parts["passed"]:
        alias = alias_by_id[p.id]
        prev = managed_providers.get(alias)
        if prev is not None:
            prev_model = prev.get("model")
            if isinstance(prev_model, str) and prev_model and prev_model != p.id:
                die(f"zeroclaw alias '{alias}' already references model "
                    f"'{prev_model}' (not '{p.id}')", 2)
        new_by_id[p.id] = build_zeroclaw_provider(
            p, overrides, prev, args.default_max_tokens, paths["proxy_url"])
    changes = compute_changes(old_by_id, new_by_id, ZEROCLAW_MANAGED_FIELDS)

    for pid, alias in alias_by_id.items():
        existing = managed_agents.get(alias)
        if existing is not None:
            mp = existing.get("model_provider")
            if isinstance(mp, str) and mp and mp != f"custom.{alias}":
                die(f"zeroclaw alias '{alias}' already references '{mp}'", 2)

    target_aliases = set(alias_by_id.values())

    # Stale sets are derived on independent axes: a partial-failure provider
    # residue (any owned leaf, even without identity) and an orphan managed
    # agent must each be cleaned on its own key space. Provider cleanup scrubs
    # from managed_providers via _zc_has_owned so identity-less fragments do not
    # survive indefinitely.
    owned_provider_aliases = {a for a, o in managed_providers.items() if _zc_has_owned(o)}
    stale_provider_aliases = sorted(owned_provider_aliases - target_aliases)
    stale_agent_aliases = sorted(set(managed_agents) - target_aliases)
    managed_aliases = owned_provider_aliases | set(managed_agents)

    # `changes` is provider-derived (keyed by model id). An orphan managed agent
    # with no live provider counterpart applies a deletion that changes would
    # otherwise miss; report it in a distinct alias-namespaced field rather than
    # mixing aliases into changes["removed"] (which holds model ids).
    agent_removals = [a for a in stale_agent_aliases if a not in live_providers]

    # Provider mutations are SCALAR-LEAF only: real `zeroclaw config patch`
    # (0.8.3) rejects an object-valued add at a new map key AND rejects a
    # whole-subtable remove; a scalar-leaf path auto-materializes its parent
    # tables (providers.models.custom, agents). Provider ops always precede
    # agent ops in one patch so an agent's model_provider never dangles.
    provider_ops: list[dict] = []
    agent_ops: list[dict] = []

    for pid in sorted(alias_by_id):
        alias = alias_by_id[pid]
        prov = new_by_id[pid]
        prev_prov = managed_providers.get(alias)
        prev_live = prev_prov if (prev_prov and _zc_live(prev_prov)) else None
        # Change detection uses the LIVE view (identity present) so identical
        # aliases emit no ops (native patch may rewrite TOML/comments even on an
        # identical replace). A residue/partial fragment (owned leaves but no
        # identity) is treated as changed so recommission re-emits every field.
        prov_changed = prev_live is None or (
            managed_view(prev_live, ZEROCLAW_MANAGED_FIELDS)
            != managed_view(prov, ZEROCLAW_MANAGED_FIELDS))
        if prov_changed:
            base = f"/providers/models/custom/{_jp_escape(alias)}"
            for fld in ZEROCLAW_MANAGED_FIELDS:
                if fld in prov:
                    provider_ops.append({"op": "add", "path": f"{base}/{fld}",
                                         "value": prov[fld]})
                elif prev_prov is not None and fld in prev_prov:
                    # owned field dropped (think when reasoning turns off) or a
                    # stray residue leaf on recommission — scrub against the RAW
                    # previous provider, not the live view.
                    provider_ops.append({"op": "remove", "path": f"{base}/{fld}"})

        prev_agent = managed_agents.get(alias)
        agent = build_zeroclaw_agent(alias, prev_agent)
        abase = f"/agents/{_jp_escape(alias)}"
        prev_mp = prev_agent.get("model_provider") if prev_agent else None
        needs_mp = prev_mp != agent["model_provider"]
        if needs_mp:
            agent_ops.append({"op": "add", "path": f"{abase}/model_provider",
                              "value": agent["model_provider"]})
        # Stamp enabled=false only when the agent has no explicit `enabled` yet
        # (brand-new alias OR residue table without the key): real zeroclaw
        # rejects an enabled agent that carries a model_provider without a
        # risk_profile, and this skill never invents risk profiles. An explicit
        # enabled value the operator set (with their own risk_profile) is
        # preserved untouched; if it is invalid the native patch rejects it
        # rather than this skill silently overriding operator intent.
        if not (prev_agent and "enabled" in prev_agent):
            agent_ops.append({"op": "add", "path": f"{abase}/enabled",
                              "value": False})

    # Stale provider decommission: blank ONLY adapter-owned leaves; api_key and
    # any operator extra are preserved. The alias table lingers as inert residue
    # (real CLI cannot drop a subtable) and reads as decommissioned next run.
    for alias in stale_provider_aliases:
        prev_prov = managed_providers[alias]
        base = f"/providers/models/custom/{_jp_escape(alias)}"
        for fld in ZEROCLAW_MANAGED_FIELDS:
            if fld in prev_prov:
                provider_ops.append({"op": "remove", "path": f"{base}/{fld}"})

    patch_ops = provider_ops + agent_ops

    wrote = False
    if args.apply and (patch_ops or stale_agent_aliases):
        # Backup original bytes first; a backup failure aborts before any CLI
        # call, so the operator's config is never mutated without a recovery
        # copy. NB the apply is NOT atomic across steps: stale-agent deletes and
        # the leaf patch are separate CLI calls — <config.toml>.bak is the
        # single rollback point if a later step fails.
        try:
            with open(path, "rb") as src:
                original = src.read()
            with open(path + ".bak", "wb") as dst:
                dst.write(original)
        except OSError as exc:
            die(f"could not write backup for {path}: {exc}", 2)

        cfg_dir = os.path.dirname(path) or "."
        # 1) delete stale agents via the dedicated CLI (scrubs references and
        #    cascades owned state; `config patch` cannot drop a subtable). Done
        #    before provider blanking so no live agent references a blanked
        #    provider.
        for alias in stale_agent_aliases:
            _zc_run(args.zeroclaw_bin, cfg_dir, ["agents", "delete", alias, "--yes"])
        # 2) apply the scalar-leaf provider+agent patch.
        if patch_ops:
            fd, patch_file = tempfile.mkstemp(suffix=".json", prefix="ml-zeroclaw-patch-")
            try:
                with os.fdopen(fd, "w", encoding="utf-8") as fh:
                    json.dump(patch_ops, fh)
                _zc_run(args.zeroclaw_bin, cfg_dir,
                        ["config", "patch", patch_file, "--json"])
            finally:
                try:
                    os.unlink(patch_file)
                except OSError:
                    pass
        after = _load_toml(path)
        after_custom = _zc_custom(after, path)
        after_agents = _zc_agents(after, path)
        for pid, alias in alias_by_id.items():
            got = after_custom.get(alias)
            expected = new_by_id[pid]
            if not isinstance(got, dict):
                die(f"zeroclaw post-apply mismatch: provider '{alias}' missing", 2)
            for fld in ("uri", "model", "wire_api", "context_window", "max_tokens"):
                if got.get(fld) != expected.get(fld):
                    die(f"zeroclaw post-apply mismatch: provider '{alias}' field '{fld}'", 2)
            if got.get("think") != expected.get("think"):
                die(f"zeroclaw post-apply mismatch: provider '{alias}' field 'think'", 2)
            ag = after_agents.get(alias)
            if not isinstance(ag, dict) or ag.get("model_provider") != f"custom.{alias}":
                die(f"zeroclaw post-apply mismatch: agent '{alias}'", 2)
        for alias in stale_agent_aliases:
            if alias in after_agents:
                die(f"zeroclaw post-apply mismatch: stale agent '{alias}' remains", 2)
        for alias in stale_provider_aliases:
            resid = after_custom.get(alias)
            if isinstance(resid, dict) and _zc_has_owned(resid):
                die(f"zeroclaw post-apply mismatch: stale provider '{alias}' owned fields remain", 2)
        wrote = True

    return {
        "target": "zeroclaw", "path": path,
        "provider": ZEROCLAW_ALIAS_PREFIX + "*",
        "provider_existed": bool(managed_aliases), "changes": changes,
        "orphan_agent_removals": agent_removals, "wrote": wrote,
        "entries": [new_by_id[pid] for pid in sorted(new_by_id)],
    }


# --------------------------------------------------------------------------- #
# Rendering
# --------------------------------------------------------------------------- #

def fmt_ctx(n: int | None) -> str:
    if n is None:
        return "?"
    if n >= 1024 and n % 1024 == 0:
        return f"{n // 1024}k"
    return str(n)


def print_human(parts: dict, results: list[dict], args: argparse.Namespace,
                 paths: dict, unavailable: list[dict]) -> None:
    mode = "APPLY" if args.apply else "DRY RUN"
    print(f"\n=== model-loader agent-catalog sync ({mode}) ===")
    print(f"profiles dir : {paths['profiles_dir']}")
    print(f"proxy        : {paths['proxy_url']}")
    print(f"threshold    : contextWindow > {args.min_context}")

    print(f"\nPASS ({len(parts['passed'])}):")
    for p in parts["passed"]:
        modal = "vision" if p.vision else "text"
        print(f"  + {p.id:52} {fmt_ctx(p.ctx):>7}  {modal:6}  [{p.ctx_src}]")

    if parts["skipped"]:
        print(f"\nSKIP ({len(parts['skipped'])}):")
        for p, why in parts["skipped"]:
            modal = "vision" if p.vision else "text"
            print(f"  - {p.id:52} {fmt_ctx(p.ctx):>7}  {modal:6}  ({why})")

    if parts["undetermined"]:
        print(f"\nUNDETERMINED context ({len(parts['undetermined'])}):")
        for p in parts["undetermined"]:
            print(f"  ? {p.id:52} (no recognized ctx arg and no <N>k token)")

    if parts["corrupt"]:
        print(f"\nCORRUPT / unreadable ({len(parts['corrupt'])}):")
        for p in parts["corrupt"]:
            print(f"  ! {p.id:52} {p.error}")

    for r in results:
        if "seeded" in r:
            continue
        ch = r["changes"]
        print(f"\n[{r['target']}] {r['path']}  (provider: {r['provider']}, "
              f"{'CREATE' if not r['provider_existed'] else 'UPDATE'}):")
        if ch["added"]:
            print(f"  + add    ({len(ch['added'])}): " + ", ".join(ch["added"]))
        if ch["updated"]:
            print(f"  ~ update ({len(ch['updated'])}): " + ", ".join(ch["updated"]))
        if ch["removed"]:
            print(f"  - remove ({len(ch['removed'])}): " + ", ".join(ch["removed"]))
        orphans = r.get("orphan_agent_removals") or []
        if orphans:
            print(f"  - orphan agent ({len(orphans)}): " + ", ".join(orphans))
        if ch["unchanged"]:
            print(f"  = keep   ({len(ch['unchanged'])}): " + ", ".join(ch["unchanged"]))
        if not (ch["added"] or ch["updated"] or ch["removed"] or orphans):
            print("  (no changes needed)")
        if args.apply and r["wrote"]:
            print(f"  applied -> {r['path']} (backup: {r['path']}.bak)")

    if unavailable:
        print(f"\nUNAVAILABLE targets ({len(unavailable)}):")
        for u in unavailable:
            print(f"  ~ {u['target']:10} {u['path']}  (not found; skipped)")

    if not args.apply:
        print("\n(dry run — nothing written. Re-run with --apply to write.)")


def print_json(parts: dict, results: list[dict], args: argparse.Namespace,
               paths: dict, unavailable: list[dict]) -> None:
    out = {
        "mode": "apply" if args.apply else "dry-run",
        "threshold": args.min_context,
        "proxy_url": paths["proxy_url"],
        "profiles_dir": paths["profiles_dir"],
        "passed": [{"id": p.id, "name": p.name, "contextWindow": p.ctx,
                    "vision": p.vision, "ctx_source": p.ctx_src}
                   for p in parts["passed"]],
        "skipped": [{"id": p.id, "contextWindow": p.ctx, "reason": why}
                    for p, why in parts["skipped"]],
        "undetermined": [p.id for p in parts["undetermined"]],
        "corrupt": [{"id": p.id, "error": p.error} for p in parts["corrupt"]],
        "unavailable_targets": unavailable,
        "results": [{k: v for k, v in r.items() if k != "entries"} for r in results],
    }
    print(json.dumps(out, indent=2))


# --------------------------------------------------------------------------- #
# Main
# --------------------------------------------------------------------------- #

TARGET_SYNCERS: dict[str, tuple[str, Callable[..., dict]]] = {
    "pi": ("pi_models_json", sync_pi),
    "feynman": ("feynman_models_json", sync_feynman),
    "omp": ("omp_models_yml", sync_omp),
    "opencode": ("opencode_config_json", sync_opencode),
    "crush": ("crush_config_json", sync_crush),
    "forge": ("forge_provider_json", sync_forge),
    "hermes": ("hermes_config_yml", sync_hermes),
    "droid": ("droid_settings_json", sync_droid),
    "zeroclaw": ("zeroclaw_config_toml", sync_zeroclaw),
}


def parse_args(argv: list[str]) -> argparse.Namespace:
    ap = argparse.ArgumentParser(
        prog="sync.py",
        description=("Sync model-loader profiles whose context window exceeds "
                     "--min-context into pi, Feynman, omp, OpenCode, Crush, "
                     "Forgecode, Hermes Agent, Factory Droid, and/or ZeroClaw."),
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=("Default mode is --dry-run. Each target's file is otherwise "
                "left untouched: connection fields and unrelated providers/"
                "config are preserved unless --update-provider-config is "
                "given. In --target all, a target whose config file does not "
                "exist is skipped with a warning; an explicitly-requested "
                "missing target is a configuration error (exit 2)."),
    )
    ap.add_argument("--target", choices=TARGET_NAMES + ("all",), default="all",
                    help="which catalog(s) to sync: pi, feynman, omp, opencode, "
                         "crush, forge, hermes, droid, zeroclaw, or all "
                         "(default: all)")
    ap.add_argument("--apply", action="store_true",
                    help="write changes (default: dry-run)")
    ap.add_argument("--min-context", type=int, default=MIN_CONTEXT_DEFAULT,
                    help="keep profiles with contextWindow STRICTLY > N "
                         f"(default: {MIN_CONTEXT_DEFAULT})")
    ap.add_argument("--profiles-dir", metavar="PATH",
                    help="model-loader profiles dir "
                         "(env: MODEL_LOADER_PROFILES_DIR)")
    ap.add_argument("--config", metavar="PATH",
                    help="model-loader config.toml (env: MODEL_LOADER_CONFIG)")
    ap.add_argument("--pi-models-json", metavar="PATH",
                    help="pi models.json path (env: PI_MODELS_JSON)")
    ap.add_argument("--feynman-models-json", metavar="PATH",
                    help="Feynman models.json path (env: FEYNMAN_MODELS_JSON)")
    ap.add_argument("--omp-models-yml", metavar="PATH",
                    help="omp models.yml path (env: OMP_MODELS_YML)")
    ap.add_argument("--opencode-config-json", metavar="PATH",
                    help="OpenCode opencode.json path (env: OPENCODE_CONFIG_JSON)")
    ap.add_argument("--forge-provider-json", metavar="PATH",
                    help="Forge active provider.json path override "
                         "(default precedence: $FORGE_CONFIG/provider.json, "
                         "~/forge/provider.json if that dir exists, else "
                         "~/.forge/provider.json)")
    ap.add_argument("--hermes-config-yml", metavar="PATH",
                    help="Hermes config.yaml path (env: HERMES_CONFIG_YML; "
                         "default: $HERMES_HOME/config.yaml, else "
                         "~/.hermes/config.yaml)")
    ap.add_argument("--crush-config-json", metavar="PATH",
                    help="Crush crush.json path (env: CRUSH_CONFIG_JSON; "
                         "default: $CRUSH_GLOBAL_CONFIG/crush.json, else "
                         "~/.config/crush/crush.json)")
    ap.add_argument("--droid-settings-json", metavar="PATH",
                    help="Factory Droid settings.json path (env: "
                         "DROID_SETTINGS_JSON; default: $FACTORY_HOME/"
                         "settings.json, else ~/.factory/settings.json)")
    ap.add_argument("--zeroclaw-config-toml", metavar="PATH",
                    help="ZeroClaw config.toml path (env: ZEROCLAW_CONFIG_TOML; "
                         "default: $ZEROCLAW_CONFIG_DIR/config.toml, else "
                         "~/.zeroclaw/config.toml)")
    ap.add_argument("--zeroclaw-bin", default=ZEROCLAW_BIN_DEFAULT,
                    help=argparse.SUPPRESS)
    ap.add_argument("--pi-provider", default=PI_PROVIDER_DEFAULT,
                    help=f"pi provider block to manage (default: {PI_PROVIDER_DEFAULT})")
    ap.add_argument("--feynman-provider", default=FEYNMAN_PROVIDER_DEFAULT,
                    help=f"Feynman provider block to manage (default: {FEYNMAN_PROVIDER_DEFAULT})")
    ap.add_argument("--omp-provider", default=OMP_PROVIDER_DEFAULT,
                    help=f"omp provider block to manage (default: {OMP_PROVIDER_DEFAULT})")
    ap.add_argument("--opencode-provider", default=OPENCODE_PROVIDER_DEFAULT,
                    help=f"OpenCode provider block to manage (default: {OPENCODE_PROVIDER_DEFAULT})")
    ap.add_argument("--forge-provider", default=FORGE_PROVIDER_DEFAULT,
                    help=f"Forge provider array entry (by id) to manage (default: {FORGE_PROVIDER_DEFAULT})")
    ap.add_argument("--hermes-provider", default=HERMES_PROVIDER_DEFAULT,
                    help=f"Hermes provider block to manage (default: {HERMES_PROVIDER_DEFAULT})")
    ap.add_argument("--crush-provider", default=CRUSH_PROVIDER_DEFAULT,
                    help=f"Crush provider block to manage (default: {CRUSH_PROVIDER_DEFAULT})")
    ap.add_argument("--proxy-url", metavar="URL",
                    help="OpenAI proxy base URL (env: MODEL_LOADER_PROXY_URL)")
    ap.add_argument("--default-max-tokens", type=int, default=DEFAULT_MAX_TOKENS,
                    help="maxTokens / limit.output for brand-new entries "
                         f"(default: {DEFAULT_MAX_TOKENS})")
    ap.add_argument("--overrides", metavar="PATH",
                    help="JSON of per-id overrides {id: {name, input, maxTokens, "
                         "reasoning}} — fields not representable by a target's "
                         "native schema (e.g. reasoning for omp, maxTokens for "
                         "Forge, name/reasoning/maxTokens for Hermes) are ignored "
                         "by that target's entry builder. Default: overrides.json "
                         "next to this script, if present.")
    ap.add_argument("--seed-overrides", action="store_true",
                    help="write an overrides file from one target's current "
                         "entries (translated into the shared override schema), "
                         "then exit; requires an explicit --target (not 'all')")
    ap.add_argument("--update-provider-config", action="store_true",
                    help="refresh a target's managed provider connection "
                         "defaults from scratch (pi/Feynman: baseUrl/apiKey/api/"
                         "compat; OpenCode: npm/name/options; Forge: url/"
                         "response_type/auth_methods, also clearing any "
                         "api_key_vars; Hermes: base_url/name) instead of "
                         "preserving them. omp's connection fields are always "
                         "preserved as-is.")
    ap.add_argument("--include-embeddings", action="store_true",
                    help="do not auto-exclude embedding/reranker profiles")
    ap.add_argument("--json", action="store_true",
                    help="emit machine-readable JSON instead of human prose")
    ap.add_argument("--quiet", action="store_true", help="suppress warnings")
    return ap.parse_args(argv)


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    paths = derive_paths(args)

    if args.seed_overrides and args.target == "all":
        die("--seed-overrides requires an explicit --target (not 'all')", 2)

    default_overrides = os.path.join(
        os.path.dirname(os.path.abspath(__file__)), "overrides.json")
    overrides_path = args.overrides or (
        default_overrides if os.path.isfile(default_overrides) else None)
    overrides = load_overrides(overrides_path)
    args.overrides_out = args.overrides or default_overrides

    profiles = enumerate_profiles(paths["profiles_dir"])
    parts = select(profiles, args.min_context, args.include_embeddings)

    explicit = args.target != "all"
    results: list[dict] = []
    unavailable: list[dict] = []
    for target in requested_targets(args.target):
        path_key, syncer = TARGET_SYNCERS[target]
        if not target_available(paths[path_key], explicit=explicit, target=target):
            unavailable.append({"target": target, "path": paths[path_key]})
            continue
        results.append(syncer(parts, paths, overrides, args))

    if any("seeded" in r for r in results):
        return 0

    if args.json:
        print_json(parts, results, args, paths, unavailable)
    else:
        print_human(parts, results, args, paths, unavailable)

    if parts["corrupt"] or parts["undetermined"]:
        return 3
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv[1:]))
    except KeyboardInterrupt:
        sys.exit(130)

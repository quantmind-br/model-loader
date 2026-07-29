# Model-loader All-Agents Synchronization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Synchronize eligible model-loader profiles into pi, Feynman, omp, OpenCode, Forgecode, and Hermes Agent while preserving unrelated configuration and using each agent's native schema.

**Architecture:** Keep profile discovery, capability derivation, filtering, overrides, diffs, backups, and rendering shared in `sync.py`. Add explicit serializers for the five schema families: pi/Feynman JSON arrays, omp YAML arrays, OpenCode JSON model maps, Forgecode JSON provider arrays, and Hermes YAML model maps. A small target registry centralizes CLI dispatch, paths, provider IDs, and missing-file policy without hiding target-specific serialization rules.

**Tech Stack:** Python 3.14, stdlib `argparse`/`json`/`unittest`/`tempfile`, `ruamel.yaml`, model-loader profile JSON v3.

## Global Constraints

- Supported targets are exactly `pi`, `feynman`, `omp`, `opencode`, `forge`, `hermes`, and aggregate `all`.
- Every target receives one OpenAI Chat Completions-compatible model-loader provider; never reproduce QuantMind's three-provider protocol split.
- Keep the existing strict `contextWindow > 102400` default and non-chat filtering.
- Dry-run remains the default; apply uses `.bak`, atomic replacement, and round-trip parsing.
- Preserve unrelated providers and top-level configuration in every target.
- `--target all` skips missing target files with a warning; an explicitly requested missing target exits 2.
- Existing malformed target files always exit 2.
- OpenCode uses inline `options.apiKey: "model-loader"`; no new environment variable or credential is required.
- New Forge providers omit `api_key_vars` and set `auth_methods: []`; existing authentication fields are preserved unless provider config refresh is requested.
- Forge path precedence is explicit flag, `$FORGE_CONFIG/provider.json`, existing `~/forge/provider.json`, then `~/.forge/provider.json`.
- Forge URL is exactly the proxy base normalized to `/v1/chat/completions` once.
- Hermes writes only `$HERMES_HOME/config.yaml` or `~/.hermes/config.yaml`; never write `provider_models_cache.json`.
- Hermes always manages `discover_models: false` and `providers.<id>.models`; preserve active model, auxiliaries, fallbacks, connection/auth fields, and unrelated YAML.
- Forge readiness requires semantic validation by the installed `forge` binary against an isolated `$FORGE_CONFIG`, not JSON parsing alone.
- `.agents/` is gitignored and the current skill files are untracked; delivery requires explicit ignored-file inspection and force-adding only the approved skill files.
- Do not create changelog or historical notes inside the skill.

---

### Task 1: Regression harness and target/path registry

**Files:**
- Create: `.agents/skills/model-loader-agents-sync/test_sync.py`
- Modify: `.agents/skills/model-loader-agents-sync/sync.py:63-115,254-302,831-906`

**Interfaces:**
- Produces: `TARGET_NAMES: tuple[str, ...]`, `resolve_forge_provider_json(args) -> str`, `resolve_hermes_config_yml(args) -> str`, `requested_targets(target: str) -> tuple[str, ...]`, `target_available(path: str, explicit: bool, target: str) -> bool`.
- Preserves: existing `derive_paths(args) -> dict`, `main(argv) -> int`, pi/omp flag names and environment variables.

- [ ] **Step 1: Add failing path and missing-policy tests**

Create `test_sync.py` with a loader that imports the sibling script and a reusable argument helper:

```python
from __future__ import annotations

import argparse
import contextlib
import importlib.util
import io
import json
import os
import tempfile
import unittest
from unittest import mock
from pathlib import Path

from ruamel.yaml import YAML

SKILL_DIR = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("model_loader_agents_sync", SKILL_DIR / "sync.py")
sync = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(sync)


def ns(**values):
    defaults = {
        "config": None,
        "profiles_dir": None,
        "pi_models_json": None,
        "feynman_models_json": None,
        "omp_models_yml": None,
        "opencode_config_json": None,
        "forge_provider_json": None,
        "hermes_config_yml": None,
        "proxy_url": None,
        "pi_provider": "model-loader",
        "feynman_provider": "model-loader",
        "omp_provider": "llama.cpp",
        "opencode_provider": "model-loader",
        "forge_provider": "model-loader",
        "hermes_provider": "model-loader",
        "apply": False,
        "update_provider_config": False,
        "default_max_tokens": 32768,
        "min_context": 102400,
        "include_embeddings": False,
        "overrides": None,
        "overrides_out": None,
        "seed_overrides": None,
        "json": False,
        "quiet": False,
    }
    defaults.update(values)
    return argparse.Namespace(**defaults)


class PathResolutionTests(unittest.TestCase):
    def test_target_names_cover_all_agents(self):
        self.assertEqual(
            sync.TARGET_NAMES,
            ("pi", "feynman", "omp", "opencode", "forge", "hermes"),
        )

    def test_forge_path_precedence(self):
        with tempfile.TemporaryDirectory() as td:
            home = Path(td)
            legacy = home / "forge"
            legacy.mkdir()
            with mock.patch.dict(os.environ, {"HOME": td, "FORGE_CONFIG": str(home / "env-forge")}, clear=False):
                self.assertEqual(sync.resolve_forge_provider_json(ns(forge_provider_json="~/explicit.json")), str(home / "explicit.json"))
                self.assertEqual(sync.resolve_forge_provider_json(ns()), str(home / "env-forge" / "provider.json"))
            with mock.patch.dict(os.environ, {"HOME": td}, clear=True):
                self.assertEqual(sync.resolve_forge_provider_json(ns()), str(legacy / "provider.json"))
                legacy.rmdir()
                self.assertEqual(sync.resolve_forge_provider_json(ns()), str(home / ".forge" / "provider.json"))

    def test_hermes_home_precedes_default(self):
        with tempfile.TemporaryDirectory() as td:
            with mock.patch.dict(os.environ, {"HOME": td, "HERMES_HOME": str(Path(td) / "state")}, clear=True):
                self.assertEqual(sync.resolve_hermes_config_yml(ns()), str(Path(td) / "state" / "config.yaml"))

    def test_all_skips_missing_but_explicit_rejects_it(self):
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            self.assertFalse(sync.target_available("/missing/config", explicit=False, target="forge"))
        self.assertIn("forge", err.getvalue())
        with self.assertRaises(SystemExit) as raised:
            sync.target_available("/missing/config", explicit=True, target="forge")
        self.assertEqual(raised.exception.code, 2)
```

- [ ] **Step 2: Run tests and confirm RED**

Run: `python3 -m unittest .agents/skills/model-loader-agents-sync/test_sync.py -v`

Expected: failures for missing `TARGET_NAMES`, path resolvers, and target availability.

- [ ] **Step 3: Implement centralized target and path resolution**

Add constants and helpers:

```python
TARGET_NAMES = ("pi", "feynman", "omp", "opencode", "forge", "hermes")


def resolve_forge_provider_json(args: argparse.Namespace) -> str:
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
    explicit = expand(args.hermes_config_yml)
    if explicit:
        return explicit
    hermes_home = expand(os.environ.get("HERMES_HOME")) or expand("~/.hermes")
    return os.path.join(hermes_home, "config.yaml")


def requested_targets(target: str) -> tuple[str, ...]:
    return TARGET_NAMES if target == "all" else (target,)


def target_available(path: str, *, explicit: bool, target: str) -> bool:
    if os.path.isfile(path):
        return True
    if explicit:
        die(f"{target} configuration not found: {path}", 2)
    print(f"warn: skipping {target}; configuration not found: {path}", file=sys.stderr)
    return False
```

Extend `derive_paths` with defaults and environment overrides:

```python
"feynman_models_json": flag > FEYNMAN_MODELS_JSON > ~/.feynman/agent/models.json
"opencode_config_json": flag > OPENCODE_CONFIG_JSON > ~/.config/opencode/opencode.json
"forge_provider_json": resolve_forge_provider_json(args)
"hermes_config_yml": flag/HERMES_CONFIG_YML > HERMES_HOME/config.yaml > ~/.hermes/config.yaml
```

Extend parser choices and add `--feynman-models-json`, `--opencode-config-json`, `--forge-provider-json`, and `--hermes-config-yml` plus provider-ID flags.

- [ ] **Step 4: Run tests and confirm GREEN**

Run: `python3 -m unittest .agents/skills/model-loader-agents-sync/test_sync.py -v`

Expected: all Task 1 tests pass; existing pi/omp code remains importable.

---

### Task 2: Shared pi/Feynman adapter

**Files:**
- Modify: `.agents/skills/model-loader-agents-sync/test_sync.py`
- Modify: `.agents/skills/model-loader-agents-sync/sync.py:496-584`

**Interfaces:**
- Produces: `sync_pi_family(target: str, path_key: str, provider: str, parts: dict, paths: dict, overrides: dict, args: argparse.Namespace) -> dict`.
- Preserves: `sync_pi(...)` wrapper for compatibility; pi entry schema and inheritance semantics.

- [ ] **Step 1: Add failing Feynman and pi-preservation tests**

Use a synthetic loaded `Profile` helper and temporary JSON files. Assert:

```python
class PiFamilyTests(unittest.TestCase):
    def test_feynman_uses_pi_schema_and_preserves_unrelated_data(self):
        # existing providers.other and managed baseUrl/custom field survive
        # stale model disappears; selected model gets id/name/context/input/reasoning
        # target result is "feynman"

    def test_pi_wrapper_keeps_existing_behavior(self):
        # existing maxTokens is inherited and connection fields are unchanged
```

The fixture must include one eligible vision/reasoning profile and one stale existing entry.

- [ ] **Step 2: Run focused tests and confirm RED**

Run: `python3 -m unittest .agents/skills/model-loader-agents-sync/test_sync.py PiFamilyTests -v`

Expected: Feynman adapter is missing.

- [ ] **Step 3: Extract the parameterized adapter**

Implement:

```python
def sync_pi_family(target, path_key, provider, parts, paths, overrides, args):
    path = paths[path_key]
    data = _load_json_object(path)
    data.setdefault("providers", {})
    provider_block = data["providers"].get(provider)
    # reuse build_pi_entry, PI_MANAGED_FIELDS, seed_overrides, backup, atomic write
    # preserve existing provider fields; replace only models
    return {"target": target, "path": path, "provider": provider, ...}


def sync_pi(parts, paths, overrides, args):
    return sync_pi_family("pi", "pi_models_json", args.pi_provider, parts, paths, overrides, args)


def sync_feynman(parts, paths, overrides, args):
    return sync_pi_family("feynman", "feynman_models_json", args.feynman_provider, parts, paths, overrides, args)
```

Rename `_load_json` to `_load_json_object` and make a non-object JSON root an exit-code-2 error instead of silently replacing it.

- [ ] **Step 4: Run focused tests and confirm GREEN**

Run: `python3 -m unittest .agents/skills/model-loader-agents-sync/test_sync.py PiFamilyTests -v`

Expected: both tests pass.

---

### Task 3: OpenCode native serializer

**Files:**
- Modify: `.agents/skills/model-loader-agents-sync/test_sync.py`
- Modify: `.agents/skills/model-loader-agents-sync/sync.py`

**Interfaces:**
- Produces: `opencode_provider_default(proxy_url: str) -> dict`, `build_opencode_entry(profile, overrides, prev, default_max_tokens) -> dict`, `seedable_opencode_models(config, provider_id) -> dict`, `sync_opencode(...) -> dict`.
- Model map key is the profile ID; managed fields are `name`, `limit`, `modalities`, and optional schema-confirmed `reasoning` boolean. Do not emit unverified `variants` blocks.
- Load the config with object-root validation; malformed JSON or non-object roots exit 2 and leave the file untouched.
- [ ] **Step 1: Add failing OpenCode tests**

```python
class OpenCodeTests(unittest.TestCase):
    def test_writes_keyed_models_and_preserves_config(self):
        # preserve $schema, plugin, mcp, provider.other, and managed provider custom options
        # replace only provider.model-loader.models
        # assert limit.context, inherited/default limit.output
        # assert modalities.input text+image and modalities.output text

    def test_new_provider_uses_inline_placeholder_key(self):
        # npm == @ai-sdk/openai-compatible
        # options == {baseURL: proxy, apiKey: "model-loader"}
        # no env field

    def test_second_sync_is_idempotent(self):
        # apply once, dry-run again; no added/updated/removed

    def test_non_object_root_is_rejected_without_overwrite(self):
        # existing opencode.json array/scalar root exits 2 and bytes remain unchanged

    def test_seed_overrides_translates_native_opencode_models(self):
        # extracts name, maxTokens from limit.output, input from modalities.input, reasoning boolean
```

- [ ] **Step 2: Run OpenCode tests and confirm RED**

Run: `python3 -m unittest .agents/skills/model-loader-agents-sync/test_sync.py OpenCodeTests -v`

Expected: missing OpenCode serializer.

- [ ] **Step 3: Implement OpenCode serializer**

Use this entry contract:

```python
def build_opencode_entry(profile, overrides, prev, default_max_tokens):
    ov = overrides.get(profile.id, {}) or {}
    prev_limit = (prev or {}).get("limit") or {}
    output = ov.get("maxTokens", prev_limit.get("output", default_max_tokens))
    entry = {
        "name": ov.get("name") or profile.name or profile.id,
        "limit": {"context": profile.ctx, "output": output},
        "modalities": {
            "input": list(ov["input"]) if "input" in ov else (["text", "image"] if profile.vision else ["text"]),
            "output": ["text"],
        },
    }
    if "reasoning" in ov or profile.reasoning:
        entry["reasoning"] = bool(ov.get("reasoning", profile.reasoning))
    return entry
```

Default provider:

```python
{
    "npm": "@ai-sdk/openai-compatible",
    "name": "Model Loader",
    "options": {"baseURL": proxy_url, "apiKey": "model-loader"},
    "models": {},
}
```

Preserve an existing provider's `npm`, `name`, `env`, `options`, and unknown fields unless `--update-provider-config`; always replace only `models`.

- [ ] **Step 4: Run OpenCode tests and confirm GREEN**

Run: `python3 -m unittest .agents/skills/model-loader-agents-sync/test_sync.py OpenCodeTests -v`

Expected: all OpenCode tests pass.

---

### Task 4: Forgecode native serializer and endpoint/path safety

**Files:**
- Modify: `.agents/skills/model-loader-agents-sync/test_sync.py`
- Modify: `.agents/skills/model-loader-agents-sync/sync.py`

**Interfaces:**
- Produces: `forge_chat_completions_url(proxy_url: str) -> str`, `forge_provider_default(proxy_url: str, provider: str) -> dict`, `build_forge_entry(profile, overrides) -> dict`, `sync_forge(...) -> dict`.
- Root document must be a JSON list; provider lookup is by exact `id`.

- [ ] **Step 1: Add failing Forge tests**

```python
class ForgeTests(unittest.TestCase):
    def test_normalizes_chat_completions_url_once(self):
        cases = {
            "http://127.0.0.1:4321": "http://127.0.0.1:4321/v1/chat/completions",
            "http://127.0.0.1:4321/v1": "http://127.0.0.1:4321/v1/chat/completions",
            "http://127.0.0.1:4321/v1/": "http://127.0.0.1:4321/v1/chat/completions",
            "http://127.0.0.1:4321/v1/chat/completions": "http://127.0.0.1:4321/v1/chat/completions",
        }

    def test_new_provider_is_no_auth(self):
        # auth_methods == []; api_key_vars absent

    def test_existing_auth_and_unrelated_providers_survive(self):
        # preserve existing api_key_vars/auth_methods/custom fields and other array entries
        # stale models removed; capability fields mapped

    def test_non_array_root_is_rejected(self):
        # existing {} must exit 2, never be overwritten

    def test_seed_overrides_translates_native_forge_models(self):
        # extracts name, maxTokens from any supported output-limit field if present,
        # input from input_modalities, and reasoning from supports_reasoning
```

- [ ] **Step 2: Run Forge tests and confirm RED**

Run: `python3 -m unittest .agents/skills/model-loader-agents-sync/test_sync.py ForgeTests -v`

Expected: Forge helpers are missing.

- [ ] **Step 3: Implement Forge URL and serializer**

```python
def forge_chat_completions_url(proxy_url: str) -> str:
    base = proxy_url.rstrip("/")
    if base.endswith("/chat/completions"):
        return base
    if base.endswith("/v1"):
        return base + "/chat/completions"
    return base + "/v1/chat/completions"
```

New provider contract:

```python
{
    "id": provider,
    "url_param_vars": [],
    "response_type": "OpenAI",
    "url": forge_chat_completions_url(proxy_url),
    "models": [],
    "auth_methods": [],
}
```

Model contract:

```python
{
    "id": profile.id,
    "name": override_name_or_profile_name,
    "description": "Model Loader OpenAI-compatible local profile",
    "context_length": profile.ctx,
    "tools_supported": True,
    "supports_parallel_tool_calls": True,
    "supports_reasoning": override_or_profile_reasoning,
    "input_modalities": override_or_derived_input,
}
```

When the provider exists, preserve every provider field except the managed `models`; update URL and other connection defaults only under `--update-provider-config`.

- [ ] **Step 4: Run Forge tests and confirm GREEN**

Run: `python3 -m unittest .agents/skills/model-loader-agents-sync/test_sync.py ForgeTests -v`

Expected: all Forge unit tests pass.


- [ ] **Step 5: Validate the serializer with the installed Forge binary**

Create an isolated Forge config directory, write the generated `provider.json`, then run:

```bash
FORGE_CONFIG="$TMPDIR/forge" forge provider list --porcelain
FORGE_CONFIG="$TMPDIR/forge" forge list model --porcelain
```

Expected: `forge provider list` exits 0 and lists the managed provider, proving the installed binary accepts the provider file and no-auth representation. `forge list model` must exit 0; it may print no model rows when the smoke proxy is not running, so do not treat an empty model list alone as schema rejection. If `auth_methods: []` is rejected or the provider is hidden, inspect the installed binary's accepted no-auth representation and adjust the serializer plus regression test before proceeding.

---

### Task 5: Hermes authoritative YAML serializer

**Files:**
- Modify: `.agents/skills/model-loader-agents-sync/test_sync.py`
- Modify: `.agents/skills/model-loader-agents-sync/sync.py`

**Interfaces:**
- Produces: `hermes_provider_default(proxy_url: str) -> CommentedMap`, `build_hermes_entry(profile, overrides) -> CommentedMap`, `sync_hermes(...) -> dict`.
- Managed provider fields are `models` and `discover_models`; per-model managed fields are `context_length` and `supports_vision`.

- [ ] **Step 1: Add failing Hermes tests**

```python
class HermesTests(unittest.TestCase):
    def test_static_catalog_is_authoritative_and_preserves_config(self):
        # preserve model.default/provider, auxiliary, fallback_providers, provider auth/headers/timeouts
        # force discover_models false even when existing value is true
        # replace stale model map with eligible profiles
        # assert context_length and supports_vision

    def test_new_provider_has_no_required_key(self):
        # base_url, name, discover_models false, models map
        # api_key/key_env absent

    def test_never_writes_provider_cache(self):
        # create sibling provider_models_cache.json sentinel; apply; bytes unchanged

    def test_yaml_comments_survive(self):
        # a top-level comment and unrelated section comment remain after apply

    def test_non_mapping_root_is_rejected_without_overwrite(self):
        # existing config.yaml list/scalar root exits 2 and bytes remain unchanged

    def test_seed_overrides_translates_native_hermes_models(self):
        # extracts context_length/supported vision fields Hermes can represent
```

- [ ] **Step 2: Run Hermes tests and confirm RED**

Run: `python3 -m unittest .agents/skills/model-loader-agents-sync/test_sync.py HermesTests -v`

Expected: Hermes serializer is missing.

- [ ] **Step 3: Implement Hermes serializer**

```python
def hermes_provider_default(proxy_url: str) -> CommentedMap:
    block = CommentedMap()
    block["base_url"] = proxy_url
    block["name"] = "Model Loader"
    block["discover_models"] = False
    block["models"] = CommentedMap()
    return block


def build_hermes_entry(profile: Profile, overrides: dict) -> CommentedMap:
    ov = overrides.get(profile.id, {}) or {}
    entry = CommentedMap()
    entry["context_length"] = profile.ctx
    modalities = ov.get("input")
    entry["supports_vision"] = "image" in modalities if modalities is not None else profile.vision
    return entry
```

Load with the same round-trip YAML helper as omp. Require a mapping root; malformed YAML or non-mapping roots exit 2 and leave the file untouched. Preserve the existing provider block, replace its model map, always assign `discover_models = False`, and only refresh `base_url`/`name` under `--update-provider-config`.

- [ ] **Step 4: Run Hermes tests and confirm GREEN**

Run: `python3 -m unittest .agents/skills/model-loader-agents-sync/test_sync.py HermesTests -v`

Expected: all Hermes tests pass and cache sentinel remains unchanged.

---

### Task 6: Six-target dispatch, seeding, output, and missing-file behavior

**Files:**
- Modify: `.agents/skills/model-loader-agents-sync/test_sync.py`
- Modify: `.agents/skills/model-loader-agents-sync/sync.py:748-918`

**Interfaces:**
- Produces: `TARGET_SYNCERS: dict[str, Callable]`, result objects with optional `skipped_unavailable: bool`, JSON output field `unavailable_targets`.
- `--seed-overrides` accepts exactly one explicit target.

- [ ] **Step 1: Add failing integration tests**

```python
class CliIntegrationTests(unittest.TestCase):
    def test_all_dispatches_every_existing_target_and_skips_missing(self):
        # six temp paths; omit one; --target all --json
        # result contains five target records and one unavailable target

    def test_explicit_missing_target_exits_two(self):
        # --target hermes with absent file

    def test_seed_overrides_accepts_each_explicit_target_and_rejects_all(self):
        # includes seed output assertions for pi, feynman, omp, opencode, forge, and hermes native shapes
        # each target's translator maps native entries into target-neutral overrides: name when present,
        # maxTokens/limit.output when representable, input/input_modalities/modalities.input, and reasoning/supports_reasoning
        # parser/validation contract; use a temporary output path

    def test_apply_creates_backups_and_second_dry_run_has_no_changes(self):
        # one representative fixture per serializer family
```

- [ ] **Step 2: Run integration tests and confirm RED**

Run: `python3 -m unittest .agents/skills/model-loader-agents-sync/test_sync.py CliIntegrationTests -v`

Expected: hardcoded pi/omp dispatch and seed validation fail.

- [ ] **Step 3: Implement registry dispatch and output**

```python
TARGET_SYNCERS = {
    "pi": ("pi_models_json", sync_pi),
    "feynman": ("feynman_models_json", sync_feynman),
    "omp": ("omp_models_yml", sync_omp),
    "opencode": ("opencode_config_json", sync_opencode),
    "forge": ("forge_provider_json", sync_forge),
    "hermes": ("hermes_config_yml", sync_hermes),
}
```

In `main`:

```python
explicit = args.target != "all"
results = []
unavailable = []
for target in requested_targets(args.target):
    path_key, syncer = TARGET_SYNCERS[target]
    if not target_available(paths[path_key], explicit=explicit, target=target):
        unavailable.append({"target": target, "path": paths[path_key]})
        continue
    results.append(syncer(parts, paths, overrides, args))
```

Pass `unavailable` to human/JSON rendering. Update seeding validation to reject only `all`. Implement explicit per-target native-to-override translators instead of feeding raw native entries to `seed_overrides`; tests must assert seeded override JSON for pi, feynman, omp, opencode, forge, and hermes.

- [ ] **Step 4: Run the complete Python test suite**

Run: `python3 -m unittest .agents/skills/model-loader-agents-sync/test_sync.py -v`

Expected: all tests pass with no warnings or tracebacks.

---

### Task 7: Skill instructions and deterministic behavioral evaluation

**Files:**
- Modify: `.agents/skills/model-loader-agents-sync/SKILL.md`
- Modify: `.agents/skills/model-loader-agents-sync/overrides.example.json`
- Modify: `.agents/skills/model-loader-agents-sync/test_sync.py`

**Interfaces:**
- Documents: six targets, native schemas, flags, path precedence, authentication behavior, missing-target policy, Hermes cache exclusion, dry-run/apply workflow.

- [ ] **Step 1: Add documentation contract tests**

Add a small test that reads `SKILL.md` and asserts the six target names, target choices, Forge suffix, Hermes config path, `discover_models: false`, and dry-run-before-apply guidance are present. This guards discoverability and prevents the instructions from drifting back to pi/omp-only behavior.

- [ ] **Step 2: Run the documentation test and confirm RED**

Run: `python3 -m unittest .agents/skills/model-loader-agents-sync/test_sync.py SkillDocumentationTests -v`

Expected: current two-target instructions fail the six-target assertions.

- [ ] **Step 3: Rewrite the skill metadata and operational contract**

Update frontmatter description to trigger on syncing model-loader profiles into any of pi, Feynman, omp, OpenCode, Forgecode, or Hermes. Keep the body focused on:

- one managed OpenAI-compatible provider per target;
- target table with exact paths and schema shapes;
- shared eligibility/capability derivation;
- preservation and missing-file rules;
- Forge path and URL normalization;
- OpenCode inline placeholder key;
- Forge no-auth defaults;
- Hermes static catalog and managed discovery policy;
- all target-specific CLI flags;
- dry-run then `--apply` usage;
- override seeding and exit codes.

Update `overrides.example.json` wording from “both targets” to “all applicable targets” and list any fields intentionally ignored by Hermes.

- [ ] **Step 4: Run Python tests and syntax checks**

Run:

```bash
python3 -m unittest .agents/skills/model-loader-agents-sync/test_sync.py -v
python3 -m py_compile .agents/skills/model-loader-agents-sync/sync.py .agents/skills/model-loader-agents-sync/test_sync.py
```

Expected: all tests pass; compilation exits 0.

---

### Task 8: Live dry-run and repository verification

**Files:**
- No source changes unless verification exposes a defect.

**Interfaces:**
- Verifies: current profiles and installed configs without writing them.

- [ ] **Step 1: Run six-target dry-run against live files**

Run from the skill directory:

```bash
python3 sync.py --json
```

Expected:

- exit 0 or 3 only when corrupt/undetermined profiles are truthfully reported;
- results for installed pi, Feynman, omp, OpenCode, Forgecode, and Hermes configs;
- unavailable agents, if any, are warnings rather than aggregate failure;
- no target file timestamp or content changes.

- [ ] **Step 2: Run apply against isolated copies, not live agent files**
Create a temporary directory containing copies of all six live configs and invoke `sync.py --apply` with every explicit path override plus the live profiles directory. Assert each copied file has a `.bak`, reparses, preserves unrelated providers/config, and produces no changes on a second dry-run. Point `$FORGE_CONFIG` at the isolated Forge copy and run `forge provider list --porcelain` plus `forge list model --porcelain`; provider listing must exit 0 and expose the generated provider, and model listing must exit 0 without schema/auth errors.

- [ ] **Step 3: Verify and stage ignored skill artifacts explicitly**

Run:

```bash
git check-ignore -v \
  .agents/skills/model-loader-agents-sync/SKILL.md \
  .agents/skills/model-loader-agents-sync/sync.py \
  .agents/skills/model-loader-agents-sync/test_sync.py \
  .agents/skills/model-loader-agents-sync/overrides.example.json
git status --short --ignored .agents/skills/model-loader-agents-sync
git add -f \
  .agents/skills/model-loader-agents-sync/SKILL.md \
  .agents/skills/model-loader-agents-sync/sync.py \
  .agents/skills/model-loader-agents-sync/test_sync.py \
  .agents/skills/model-loader-agents-sync/overrides.example.json
git diff --cached --name-only -- .agents/skills/model-loader-agents-sync
```

Expected: `git check-ignore` identifies `.gitignore`'s `.agents/` rule; the final staged list contains exactly the four approved skill artifacts and no cache files such as `__pycache__`.

- [ ] **Step 4: Run repository quality gate**

Run:

```bash
go build ./...
go test ./...
```

- [ ] **Step 5: Review final scope**


Confirm only the approved skill, its tests, the design spec, and this plan changed for this task. Treat pre-existing benchmark and documentation modifications as user work and do not alter them.

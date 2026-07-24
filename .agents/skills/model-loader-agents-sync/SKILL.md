---
name: model-loader-agents-sync
description: >-
  Use when syncing model-loader profiles to pi, Feynman, omp, OpenCode, Crush,
  Forgecode, Hermes Agent, Factory Droid, or ZeroClaw; after profile changes;
  or when model selectors, context windows, capabilities, or managed provider
  entries are stale, missing, or incorrect.
---

# model-loader-agents-sync

Reflect **model-loader** profiles into every local coding agent that treats
its OpenAI proxy (`http://127.0.0.1:4321/v1`) as a model provider: **pi**,
**Feynman**, **omp**, **OpenCode**, **Crush**, **Forgecode**, **Hermes
Agent**, **Factory Droid**, and **ZeroClaw**. Only profiles with **more than
100k context** are offered: smaller local models aren't worth exposing as
coding-agent backends.

Every target consumes the same single OpenAI Chat Completions surface of the
proxy. This skill never reproduces a multi-protocol split (one provider per
wire format, the way a cloud aggregator like QuantMind needs
OpenAI/Anthropic/Google providers) — model-loader exposes every profile
through the same OpenAI-compatible endpoint. Targets with a provider catalog
(pi, Feynman, omp, OpenCode, Crush, Forgecode, Hermes) get **one** managed
provider block; Factory Droid gets a set of managed `customModels[]` entries;
ZeroClaw — whose schema carries a single model per provider profile — gets
**one provider profile + one agent alias per eligible profile**.

This skill is a thin, deterministic synchronizer built around one script,
`sync.py`. It owns exactly one thing per target: the managed provider/model
surface inside that target's own config file. Everything it needs to know
about model-loader and all nine agents is encoded in the script — read it
before making behavioral changes, don't just pattern-match this document. The
exact native schema of each adapter lives in `references/targets.md`; consult
it only when changing an adapter.

## When to trigger

- "sync model-loader" / "synchronize model-loader profiles"
- "sync pi/feynman/omp/opencode/crush/forge/hermes/droid/zeroclaw models" /
  "update the model catalog"
- "import / register / add model-loader models into <agent>"
- "fix wrong context window for model-loader models"
- "<agent> is missing / showing stale model-loader entries"
- after the user creates, edits, or deletes a model-loader profile and wants
  the change reflected downstream
- `/model-loader-agents-sync`

## When NOT to trigger

- The user wants to launch / tune / kill a model-loader profile itself — that
  belongs to model-loader's own skill/workflow, not this one.
- The user wants to add a *non*-model-loader model to any of the nine agents
  (Anthropic, OpenAI, Ollama, QuantMind, …) — edit the target file directly
  or use its own connect/login flow; this skill only manages the provider
  block(s)/aliases it owns.
- The user wants to change the >100k threshold or which profiles qualify —
  pass `--min-context` / `--include-embeddings` for a one-off run; do not
  hand-edit the generated output.

## How it works (the contract)

1. **Enumerate** every `*.json` in the model-loader profiles dir
   (`~/.config/model-loader/profiles/`, overridable). Dotfiles, `.lock`
   files, and the `.history/` dir are skipped.
2. **Parse leniently** (`json.loads(strict=False)`) so profiles whose
   `description` contains literal control characters still load.
3. **Derive the context window** per profile, in priority order:
   1. `args.ctx-size`      — llama.cpp / beellama / buun (int)
   2. `args.max-model-len` — vLLM (frequently a **string** like `"262144"`)
   3. `args.context-length`— sglang (int)
   4. `args.max-ctx`       — dflash / lucebox (int)
   5. `parallel<N>-<W>k` id pattern when `args.parallel > 1` (e.g.
      `…-parallel12-32k` → `12 * 32 * 1024` = 393216)
   6. last-resort: a standalone `<N>k` / `<N>m` token in the id then the
      name. The regex refuses to match inside tokens like `q4km`, `9b`, `tp2`.
4. **Filter**: keep profiles with `contextWindow > 102400` (strictly greater;
   configurable via `--min-context`). Profiles whose context can't be
   determined are skipped (not guessed). Embedding / reranker profiles are
   excluded by default (`--include-embeddings` to keep them).
5. **Detect vision**: `args.mmproj` set, OR a vision/ocr/caption/vl/
   multimodal/llava tag or name/id substring.
6. **Detect reasoning**: a profile is advertised as reasoning/thinking-capable
   when its `args`/`extraArgs` carry a reasoning control — `reasoning-parser`,
   `reasoning-format`/`reasoning` (any value except `none`/`off`),
   `enable-thinking`, `reasoning-budget > 0`, or `chat-template-kwargs` with
   `preserve_thinking`/`enable_thinking` true. Every target whose native
   schema has a reasoning-capability field derives it this way; local profiles
   only establish on/off, so no effort ladder is ever invented (Crush writes
   `can_reason` with no `reasoning_levels`; Droid writes `reasoningEffort:
   "high"` only when reasoning-capable; ZeroClaw writes `think: true` only
   when reasoning-capable).
7. **Map** each surviving profile into every target's *native* schema (never
   a generic pi-shaped approximation) — see the target table and
   `references/targets.md`.
8. **Diff**: added / updated / removed / unchanged model IDs are computed per
   target from that target's own managed-field set, so an unrelated field on
   an existing entry never triggers a spurious "updated".
9. **Write** (apply mode only): back up the existing target file next to
   itself (`.bak`), then write. JSON/YAML write atomically (tmp file +
   `os.replace`) and reparse before treating the write as successful. ZeroClaw
   mutates through its native CLI in up to two steps — `zeroclaw agents delete`
   for stale agents, then one scalar-leaf `zeroclaw config patch` — so the
   apply is **not** atomic across steps; the `.bak` is the single rollback
   point. A post-apply reparse confirms the managed aliases before success.

## Targets

| Target | Default path | Managed surface | Schema |
|---|---|---|---|
| **pi** | `~/.pi/agent/models.json` | provider `model-loader` | JSON: `providers.<id>.models[]` |
| **feynman** | `~/.feynman/agent/models.json` | provider `model-loader` | JSON: `providers.<id>.models[]` (identical to pi) |
| **omp** | `~/.omp/agent/models.yml` | provider `llama.cpp` | YAML: `providers.<id>.models[]` |
| **opencode** | `~/.config/opencode/opencode.json` | provider `model-loader` | JSON: `provider.<id>.models.<model-id>` (keyed map) |
| **crush** | `~/.config/crush/crush.json` | provider `model-loader` | JSON: `providers.<id>.models[]` (`openai-compat`) |
| **forge** | active `provider.json` (precedence below) | provider `model-loader` | JSON: array of provider objects, located by `id` |
| **hermes** | `$HERMES_HOME/config.yaml`, else `~/.hermes/config.yaml` | provider `model-loader` | YAML: `providers.<id>.models.<model-id>` (keyed map) |
| **droid** | `~/.factory/settings.json` | `customModels[]` w/ `Model Loader · ` display prefix | JSON: array of custom models |
| **zeroclaw** | `~/.zeroclaw/config.toml` | `model_loader_*` provider+agent aliases | TOML: `[providers.models.custom.<alias>]` + `[agents.<alias>]` |

### Ownership markers

- **Catalog targets** (pi, Feynman, omp, OpenCode, Crush, Forgecode, Hermes):
  the managed provider block, identified by provider id.
- **Factory Droid**: a `customModels[]` entry is managed **iff** its
  `displayName` starts with `Model Loader · `. Model id, `baseUrl`, and
  `provider` are never ownership criteria on their own (ids can collide; a
  user may point unrelated custom models at the same proxy). For recognized
  entries the resolved base URL and `provider ==
  generic-chat-completion-api` are validated as managed fields, not ownership.
- **ZeroClaw**: the reserved alias prefix `model_loader_`. Providers and
  agents with that prefix are identified independently; valid pairs are
  updated. An orphaned prefixed **agent** is deleted via `zeroclaw agents
  delete` (scrubs references + cascades owned state). An orphaned prefixed
  **provider** cannot be dropped — the CLI has no remove-table op — so it is
  *decommissioned*: every adapter-owned leaf (`uri`, `model`, `wire_api`,
  `context_window`, `max_tokens`, `think`) is scrubbed while any `api_key`/
  extra is preserved. The emptied table reads as decommissioned (not a live
  model) on later runs. Aliases without the prefix and every other block stay
  intact.

Full field-by-field schemas for every adapter are in
`references/targets.md`.

### Forgecode path precedence

Explicit `--forge-provider-json`, then `$FORGE_CONFIG/provider.json`,
`~/forge/provider.json` (legacy, only when that dir exists), else
`~/.forge/provider.json`.

## Preservation and missing-target rules

- Never overwrite a complete agent configuration with a generated template;
  `sync.py` only edits an existing file, it never fabricates one from nothing.
- Never delete or rewrite unrelated providers, aliases, or unrelated
  top-level sections (plugins, MCP definitions, risk profiles, hooks,
  comments).
- Managed-provider connection/authentication fields are preserved by default;
  pass `--update-provider-config` to refresh them from defaults instead (omp's
  connection fields are always preserved; `--update-provider-config` is a
  **no-op for ZeroClaw**, whose six managed fields are always recalculated
  while any existing `api_key`/extra field is preserved).
- By default, existing local API keys are preserved: OpenCode/Crush keep the
  managed provider's `api_key`; Droid keeps a custom model's existing
  `apiKey`; ZeroClaw keeps a provider's encrypted `api_key`. A no-auth
  placeholder (`model-loader`) is written when none exists — or, for
  OpenCode/Crush/Droid, when `--update-provider-config` explicitly resets the
  connection (ZeroClaw ignores that flag and always keeps the key).
- Stale models are removed only from inside the managed surface (managed
  provider, the `Model Loader · ` Droid subset, or `model_loader_*` aliases).
  For ZeroClaw "removed" means the agent is CLI-deleted and the provider
  decommissioned to an inert residue table (see Ownership markers).
- `discover_models`/live model discovery is always forced **false** on Crush
  and Hermes so a `/v1/models` probe can't reintroduce sub-threshold profiles.
- Every apply backs up the target file first (`.bak`), then writes; JSON/YAML
  atomically (tmp + rename) and reparsed. ZeroClaw mutates via its native CLI
  (`agents delete` for stale agents + one scalar-leaf `config patch`); this is
  **not** atomic across steps, so the `.bak` is the single rollback point. A
  post-apply reparse confirms the managed aliases. Droid's `settings.json`
  (and its `.bak`) are chmod `0600` because the format admits a literal key.
- **Missing target file**: in `--target all` (default), a missing config is
  skipped with a warning — that agent isn't installed. When one target is
  requested explicitly, a missing file is a configuration error (exit 2).
- **Malformed target file**: an existing-but-invalid file (bad JSON/YAML/TOML,
  or the wrong root/type) is always a configuration error (exit 2), in every
  mode. It is never silently skipped or overwritten. Any nonzero
  `zeroclaw agents delete` or `config patch` result is likewise exit 2.

## Usage

This skill exists in more than one place (a project copy and/or a global
one) — never hardcode an absolute path to it. `cd` into whichever copy's
directory you found this `SKILL.md` in, then invoke `sync.py` relative to
that directory. **Always preview with the default dry-run first**, then
`--apply`.

```bash
cd "<directory containing this SKILL.md>"

# 1) Preview what would change on every installed agent (no writes):
python3 sync.py

# 2) Write it for real (a .bak backup is created automatically per target):
python3 sync.py --apply

# 3) Restrict to one target if that's all you need:
python3 sync.py --target {pi,feynman,omp,opencode,crush,forge,hermes,droid,zeroclaw} --apply
```

pi/Feynman reload their model list every time `/model` is opened; omp,
OpenCode, Crush, and Hermes pick up their config on their own reload;
Forgecode reads `provider.json` at startup / provider refresh; Factory Droid
reads `settings.json` on launch; ZeroClaw's patch is applied by its own CLI so
running agents see it on next config read — no restart is required for most of
these after `--apply`.

### Common flags

| Flag | Purpose |
|------|---------|
| `--target {pi,feynman,omp,opencode,crush,forge,hermes,droid,zeroclaw,all}` | Which catalog(s) to sync (default: `all`). |
| `--apply` | Actually write (default is dry-run). |
| `--min-context N` | Keep profiles with `contextWindow > N` (default `102400`). |
| `--include-embeddings` | Do not auto-exclude embedding/reranker profiles. |
| `--proxy-url URL` | Override the proxy base URL. |
| `--profiles-dir PATH` | Profile source other than the model-loader default. |
| `--pi-models-json` / `--feynman-models-json PATH` | Target a non-default pi/Feynman `models.json`. |
| `--omp-models-yml PATH` | Target a non-default omp `models.yml`. |
| `--opencode-config-json PATH` | Target a non-default OpenCode `opencode.json`. |
| `--crush-config-json PATH` | Target a non-default Crush `crush.json`. |
| `--forge-provider-json PATH` | Override Forgecode's active `provider.json`. |
| `--hermes-config-yml PATH` | Target a non-default Hermes `config.yaml`. |
| `--droid-settings-json PATH` | Target a non-default Factory Droid `settings.json`. |
| `--zeroclaw-config-toml PATH` | Target a non-default ZeroClaw `config.toml` (basename must be `config.toml`). |
| `--pi-provider` / `--feynman-provider` / `--omp-provider` / `--opencode-provider` / `--crush-provider` / `--forge-provider` / `--hermes-provider NAME` | Manage a differently-named provider block/id (default `model-loader`, except omp's `llama.cpp`). Droid and ZeroClaw take no provider-id flag — Droid's marker is the display prefix, ZeroClaw's is the `model_loader_` alias prefix. |
| `--default-max-tokens N` | Output limit for brand-new entries (default `32768`). |
| `--overrides PATH` | Per-id field overrides file (see below). |
| `--seed-overrides` | One-time: write an overrides file from one target's current entries, then exit. Requires an explicit `--target` (not `all`). |
| `--update-provider-config` | Refresh a target's managed provider connection defaults from scratch (no-op for omp connection fields and for ZeroClaw). |
| `--json` | Emit machine-readable JSON; includes `unavailable_targets`. |
| `--quiet` | Suppress warnings. |

Environment overrides: `MODEL_LOADER_PROFILES_DIR`, `MODEL_LOADER_CONFIG`,
`MODEL_LOADER_PROXY_URL`, `PI_MODELS_JSON`, `FEYNMAN_MODELS_JSON`,
`OMP_MODELS_YML`, `OPENCODE_CONFIG_JSON`, `CRUSH_CONFIG_JSON`,
`CRUSH_GLOBAL_CONFIG`, `FORGE_CONFIG`, `HERMES_HOME`, `HERMES_CONFIG_YML`,
`DROID_SETTINGS_JSON`, `FACTORY_HOME`, `ZEROCLAW_CONFIG_TOML`,
`ZEROCLAW_CONFIG_DIR`.

### Tuning fields per model (overrides)

`reasoning` and `input` are derived from the profile, and output limits aren't
encoded there. If a derivation is wrong or you want a specific
`name`/`maxTokens`/`reasoning`/`input`, record it once in an overrides file
and every future sync, on every applicable target, honors it:

```bash
# Capture a target's current hand-tuned entries once (from the skill dir):
python3 sync.py --target pi --seed-overrides
# -> writes ./overrides.json (next to sync.py) — edit it freely, then sync --apply
```

The overrides file is plain JSON, shared across all targets (see
`overrides.example.json`) using one target-neutral schema
(`{id: {name?, input?, maxTokens?, reasoning?}}`). A field a target's native
schema can't express is silently ignored by that target's entry builder:
Forgecode has no output-limit field (`maxTokens` ignored); Hermes honors only
`input` (as `supports_vision`); ZeroClaw honors only `reasoning`
(→`think`) and `maxTokens` (→`max_tokens`). `--seed-overrides` mirrors this:
it translates whatever the requested target's native entries can express back
into the same schema.

## Exit codes

- `0` — success (dry-run or apply), no corrupt/undeterminable profiles.
- `2` — configuration error: profiles dir missing, an explicitly-requested
  target's file missing or invalid, an existing-but-malformed target file in
  any mode, a ZeroClaw alias collision / model conflict / non-`config.toml`
  basename / `zeroclaw config patch` failure / post-apply mismatch, or
  `--seed-overrides` used with `--target all`.
- `3` — completed but some profiles were corrupt or had an undeterminable
  context window (review the warning section of the output).

## What it does NOT do

- It does **not** start or stop the `model-loader serve` proxy. The proxy must
  already be running for any target to actually reach these models — the sync
  itself only reads profile files.
- It does **not** edit model-loader profiles, the backend catalog, or anything
  under `~/.config/model-loader/`.
- It does **not** touch any provider/alias in a target file other than the one
  it's told to manage.
- It does **not** invent a brand-new target config from nothing — an agent
  that has never been launched is skipped with a warning in `--target all`, or
  a hard configuration error when named explicitly.
- For ZeroClaw it does **not** reserialize the TOML itself (which would drop
  comments/order) or touch risk profiles, runtime profiles, workspaces,
  memory, or credentials. It creates managed agents **disabled**
  (`enabled = false`) and never invents a `risk_profile`; the operator enables
  an agent themselves. A managed agent the operator has enabled (with their
  own risk profile) is preserved across syncs.

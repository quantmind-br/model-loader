# Target adapter schemas

Field-by-field native schema of every `sync.py` adapter. Read the relevant
section only when changing that adapter; `SKILL.md` holds the operator
contract. All nine targets consume the same proxy `/v1` Chat Completions
surface and the same target-neutral override schema
(`{id: {name?, input?, maxTokens?, reasoning?}}`); each adapter ignores fields
its native schema can't express.

Shared derivation (all targets): `contextWindow` and `input`/vision come from
the profile; `reasoning` from `detect_reasoning`; output limits are inherited
from the existing entry for that id, else `--default-max-tokens` (32768). An
override always wins over derived/inherited values.

## pi / Feynman — `providers.<id>.models[]` (JSON)

Shared adapter `sync_pi_family`; identical shape, independent path/provider.
Model fields: `id`, `name`, `contextWindow`, `input` (`["text"]` /
`["text","image"]`), `reasoning`; `maxTokens` inherited if present, else
omitted. Provider connection fields (`baseUrl`, `apiKey`, `api`, `compat`)
preserved unless `--update-provider-config`. All other providers untouched.
Managed fields: `id, name, contextWindow, input, reasoning, maxTokens`.

## omp — `providers.llama.cpp.models[]` (YAML)

Model fields: `id`, `name`, `reasoning`, `input`, `contextWindow`,
`maxTokens`. Entries updated **in place** (ruamel round-trip preserves
comments/formatting); stale removed; new appended. Connection fields always
preserved. Managed fields: `id, name, reasoning, input, contextWindow,
maxTokens`.

## OpenCode — `provider.<id>.models.<model-id>` (JSON keyed map)

New provider: `npm: "@ai-sdk/openai-compatible"`, `options.baseURL`,
`options.apiKey: "model-loader"` (no-auth placeholder). Model fields: `name`,
`limit.context`, `limit.output`, `modalities.input`, `modalities.output`
(always `["text"]`), `reasoning` (plain boolean — `variants` never invented).
Managed fields: `name, limit, modalities, reasoning`.

## Crush — `providers.<id>.models[]` (JSON, `openai-compat`)

Source of truth: `/home/diogo/dev/crush/schema.json` (`$defs.Model`,
`$defs.ProviderConfig`) and `docs/custom-providers.md`.

New/refreshed provider (`crush_provider_default`): `id`, `name: "Model
Loader"`, `type: "openai-compat"` (never `"openai"`, which forces the
Responses API), `base_url` normalized to exactly one `/v1` suffix,
`api_key: "model-loader"`, `discover_models: false`, `models: []`.
`discover_models` is forced `false` on **every** sync so a `/v1/models` probe
can't reintroduce sub-threshold profiles.

Model entry (`build_crush_entry`) — all Crush-required `Model` fields, nothing
inferred: `id`, `name`, `cost_per_1m_in`/`cost_per_1m_out`/
`cost_per_1m_in_cached`/`cost_per_1m_out_cached` all `0`, `context_window`,
`default_max_tokens` (inherited → `--default-max-tokens`), `can_reason`,
`supports_attachments`. No `reasoning_levels`, `default_reasoning_effort`,
`options`, or pricing is written for a local profile.

Rejections (exit 2, before any write): invalid JSON, non-object root,
non-object `providers`, non-object managed provider, non-array `models`. Top
level, all other providers, and the managed provider's connection/customization
fields (`api_key`, `base_url`, `extra_headers`, …) are preserved unless
`--update-provider-config`. Managed fields: `id, name, cost_per_1m_in,
cost_per_1m_out, cost_per_1m_in_cached, cost_per_1m_out_cached, context_window,
default_max_tokens, can_reason, supports_attachments`. Seed translates
`name`, `supports_attachments`→`input`, `can_reason`→`reasoning`,
`default_max_tokens`→`maxTokens`.

## Forgecode — provider-array entry located by `id` (JSON)

New provider is no-auth: `response_type: "OpenAI"`, `url_param_vars: []`,
`auth_methods: []`, no `api_key_vars`; `url` normalized to exactly one
`/v1/chat/completions`. Model fields: `id`, `name`, `description`,
`context_length`, `tools_supported: true`, `supports_parallel_tool_calls:
true`, `supports_reasoning`, `input_modalities`. Existing auth fields
preserved unless `--update-provider-config`. Managed fields: `name,
context_length, supports_reasoning, input_modalities, tools_supported,
supports_parallel_tool_calls`. No output-limit field — `maxTokens` never
written or seeded.

## Hermes — `providers.<id>.models.<model-id>` (YAML keyed map)

New provider: `base_url`, `name: "Model Loader"`, `discover_models: false`,
empty `models`. Model fields: `context_length`, `supports_vision`.
`discover_models` forced `false` every sync. Reasoning has no per-model field
and is not written. Preserves `base_url`, `api_key`, `key_env`, transport,
model defaults, `fallback_providers`, comments. Managed fields:
`context_length, supports_vision`. Seed translates only `supports_vision`→
`input`.

## Factory Droid — `customModels[]` (JSON array)

Ownership: an entry is managed **iff** `displayName` starts with
`Model Loader · ` (`is_managed_droid_entry`). Model id / `baseUrl` /
`provider` are never ownership criteria alone.

Entry (`build_droid_entry`): `model` = profile id; `displayName` =
`Model Loader · <name>`; `baseUrl` = proxy base with exactly one `/v1`;
`apiKey` = existing entry's key when present and non-empty, else
`"model-loader"`; `provider = "generic-chat-completion-api"`;
`maxContextLimit`; `maxOutputTokens`; `noImageSupport: true` only for
non-vision models; `reasoningEffort: "high"` only for reasoning-capable
models (field omitted otherwise). Unmanaged entries keep order/content and are
written first; the managed subset is replaced, sorted by profile id; stale
managed entries removed. `--update-provider-config` resets `baseUrl`,
`provider`, `apiKey` to local no-auth defaults. After write, `settings.json`
and its `.bak` are chmod `0600` (format admits a literal key). Managed fields:
`model, displayName, baseUrl, apiKey, provider, maxContextLimit,
maxOutputTokens, noImageSupport, reasoningEffort`. Seed translates
`displayName` (prefix stripped)→`name`, `maxOutputTokens`→`maxTokens`,
`noImageSupport`→`input`, presence of `reasoningEffort`→`reasoning`.
Malformed root / non-array `customModels` → exit 2, no write.

## ZeroClaw — one provider profile + agent alias per profile (TOML)

Confirmed against `zeroclaw 0.8.3` (`zeroclaw config schema`). A custom
provider profile `[providers.models.custom.<alias>]` carries a single `model`;
an `[agents.<alias>]` references `model_provider = "custom.<alias>"`. So one
provider+agent alias is generated **per eligible profile** — there is no
multi-model catalog inside a provider profile.

Aliases: `model_loader_<slug>`, `<slug>` = profile id lowercased, non-alnum →
`_`, repeated `_` collapsed, trimmed. A collision between two distinct ids is
a configuration error (exit 2), never auto-suffixed. Agent alias == provider
alias.

Provider fields (`build_zeroclaw_provider`, always recalculated, any existing
`api_key`/extra preserved): `uri` normalized to exactly one `/v1` (never
`/chat/completions`), `model`, `wire_api = "chat_completions"`,
`context_window`, `max_tokens` (override → inherited → default), `think: true`
only when reasoning-capable (omitted otherwise). Agent
(`build_zeroclaw_agent`): `model_provider = "custom.<alias>"`, plus
`enabled = false` **on creation only**. Real `zeroclaw` rejects an *enabled*
agent whose `model_provider` is set but lacks a `risk_profile` resolving to a
configured `[risk_profiles.<alias>]`; this skill never invents risk profiles,
so managed agents start disabled and the operator turns them on (with their
own risk profile) — that choice is then preserved. Risk/runtime profiles,
workspaces, memory, credentials are never created or altered.

The config path basename **must** be `config.toml` (exit 2 otherwise): the
native `zeroclaw config patch --config-dir <parent>` always targets
`<parent>/config.toml`, so a differently-named file would be read but never
written.

Native CLI contract (verified against `zeroclaw 0.8.3`): `config patch`
accepts **scalar-leaf paths only** — an object-valued `add` at a map key and a
whole-subtable `remove` are both `path_not_found`; a scalar-leaf path
auto-materializes its parent tables. Agent deletion has no patch op, so the
dedicated `zeroclaw agents delete <alias> --yes` (scrubs references, cascades
owned state) is used instead.

`_zc_live` = provider carries a non-empty `model` (identity). `_zc_has_owned`
= provider carries any of the six owned leaves. A residue table (owned leaves
blanked, `api_key`/extras kept) is not live and reads as decommissioned.

Apply phases:
1. `tomllib` reads and validates root, `providers.models.custom`, `agents`.
   Prefixed providers/agents are identified independently. Before adopting an
   existing alias, any conflict where its provider `model` or agent
   `model_provider` references something else → exit 2. A duplicate managed
   `model` across two live aliases → exit 2.
2. Build ops. Providers: scalar-leaf `add`/`remove` per owned field, emitted
   only for aliases whose live managed view changed (the CLI may rewrite
   TOML/comments even on an identical replace); a residue/partial recommission
   re-emits every field and scrubs stray owned leaves against the RAW previous
   provider. Agents: `model_provider` (add when new/changed) + `enabled=false`
   (only when no explicit `enabled` yet). Stale live providers are
   *decommissioned* — owned leaves scrubbed, api_key/extras preserved (the CLI
   cannot drop the table). Provider ops precede agent ops in ONE patch, so an
   agent's `model_provider` never dangles (final-state validation).
   Dry-run computes the diff only; orphan agents surface in
   `orphan_agent_removals` (alias-namespaced), never mixed into
   `changes["removed"]` (model ids).
   On apply: write `<config>.bak` with the original bytes (backup failure →
   exit 2, no CLI call). Then, NON-atomically: (a) `zeroclaw agents delete` each
   stale agent, (b) one scalar-leaf `config patch` for provider+agent leaves.
   A launch OSError or nonzero exit from either → exit 2. Reparse and confirm
   target aliases match, stale agents are gone, and stale providers carry no
   owned leaves (mismatch → exit 2). `.bak` is the single rollback point.

`--update-provider-config` is a no-op. Managed provider fields: `uri, model,
wire_api, context_window, max_tokens, think`; managed agent field:
`model_provider`. Seed consolidates providers by `model` (`max_tokens`→
`maxTokens`, `think`→`reasoning`; `context_window` is not an override);
divergent values for the same model → exit 2.

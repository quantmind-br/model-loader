# Model-loader synchronization for all coding agents

**Date:** 2026-07-10

## Objective

Expand `.agents/skills/model-loader-agents-sync` from pi and omp to all six locally used coding agents: pi, Feynman, omp, OpenCode, Forgecode, and Hermes Agent.

Every target receives one managed OpenAI Chat Completions-compatible provider backed by the model-loader proxy. The synchronizer must not reproduce QuantMind's three-provider protocol split because model-loader exposes every launch profile through the same OpenAI-compatible endpoint.

## Scope

The implementation will update:

- `.agents/skills/model-loader-agents-sync/sync.py`
- `.agents/skills/model-loader-agents-sync/SKILL.md`
- `.agents/skills/model-loader-agents-sync/overrides.example.json`
- focused automated tests for the synchronizer

It will not edit model-loader profiles, start or stop the proxy, change agent defaults, or manage non-model-loader providers.

## Shared synchronization pipeline

The existing target-neutral pipeline remains authoritative:

1. Enumerate model-loader profile JSON files.
2. Derive context window, vision support, reasoning support, and display metadata.
3. Exclude non-chat and context-ineligible profiles.
4. Apply per-model overrides.
5. Map the selected profiles into the target's native schema.
6. Compute added, updated, removed, and unchanged model IDs.
7. In apply mode, back up the existing target file, write atomically, and reparse the result.

The target registry will define the six supported target names, path resolution, default managed provider ID, and serializer. Target-specific schemas remain explicit instead of being forced through one generic object mapper.

## Target contracts

### pi

- Default file: `~/.pi/agent/models.json`
- Managed provider: `model-loader`
- Schema: `providers.<provider>.models[]`
- Connection fields: `baseUrl`, `apiKey`, `api`, and `compat`
- Model fields: `id`, `name`, `contextWindow`, `maxTokens`, `input`, and `reasoning`

Only the managed provider's model list is replaced. Existing connection fields and unrelated providers are preserved unless the existing `--update-provider-config` option explicitly refreshes managed connection defaults.

### Feynman

- Default file: `~/.feynman/agent/models.json`
- Managed provider: `model-loader`
- Schema and model fields: identical to pi

Feynman reuses the pi JSON adapter with an independent path and provider option. It receives the same preservation and update-provider behavior as pi.

### omp

- Default file: `~/.omp/agent/models.yml`
- Managed provider: `llama.cpp`
- Schema: `providers.<provider>.models[]`
- Model fields: `id`, `name`, `contextWindow`, `maxTokens`, `input`, and `reasoning`

The existing `ruamel.yaml` round-trip implementation remains. Existing model entries are updated in place, stale managed entries are removed, and new entries are appended so comments, ordering, flow-style lists, connection fields, and unrelated providers survive.

### OpenCode

- Default file: `~/.config/opencode/opencode.json`
- Managed provider: `model-loader`
- Schema: `provider.<provider>.models.<model-id>`
- Provider defaults:
  - `npm: "@ai-sdk/openai-compatible"`
  - `options.baseURL`: model-loader proxy base URL
  - `options.apiKey: "model-loader"`: inline non-secret placeholder; the loopback proxy ignores it, so no environment variable or `/connect` credential is required
- Model mapping:
  - display name to `name`
  - context window to `limit.context`
  - output limit to `limit.output`
  - vision capability to `modalities.input`
  - output modalities remain text-only
  - reasoning capability is represented only where OpenCode's provider/model schema supports it without inventing unsupported protocol variants

The serializer replaces only the managed provider's `models` map. Existing connection options and unrelated top-level data, plugins, MCP definitions, and providers are preserved. Model IDs are map keys and are not duplicated as an incompatible array.

### Forgecode

- Managed provider: `model-loader`
- Root schema: array of provider objects, located by provider `id`
- New providers use `response_type: "OpenAI"`, `url_param_vars: []`, and `auth_methods: []`; they omit `api_key_vars` because the loopback proxy has no authentication. If a managed provider already exists, its authentication fields are preserved unless provider refresh is explicitly requested.
- Model mapping:
  - profile ID to `id`
  - display name to `name`
  - context window to `context_length`
  - reasoning support to `supports_reasoning`
  - vision support to `input_modalities`
  - `tools_supported` and `supports_parallel_tool_calls` advertise the proxy's coding-agent tool contract

Forgecode requires a complete Chat Completions endpoint. A dedicated normalizer will turn the shared proxy base URL into exactly `.../v1/chat/completions`, avoiding duplicate `/v1` or `/chat/completions` suffixes.

Forgecode's active provider path is resolved in this order:

1. An explicit CLI path override.
2. `$FORGE_CONFIG/provider.json`.
3. `~/forge/provider.json` when `~/forge` exists.
4. `~/.forge/provider.json`.

The serializer updates or inserts only the provider array element whose `id` matches the managed provider. Other providers and unknown fields remain unchanged. Existing managed connection fields are preserved unless provider refresh is explicitly requested.

### Hermes Agent

- Default file: `$HERMES_HOME/config.yaml`, falling back to `~/.hermes/config.yaml`
- Managed provider: `model-loader`
- Schema: `providers.<provider>.models.<model-id>`
- Provider defaults:
  - `base_url`: model-loader proxy base URL
  - `name: "Model Loader"`
  - `discover_models: false`: keep the synchronized static catalog authoritative instead of replacing it with a live `/models` probe
- Model mapping:
  - context window to `context_length`
  - vision capability to `supports_vision`

Hermes consumes a model map, not a generated cache: `models: {<id>: {context_length: N, supports_vision: bool}}`. The per-model context helper reads this exact structure, and image routing honors `supports_vision`. Reasoning is not written as a per-model provider field because Hermes does not consume such a field from custom provider configuration; its runtime uses route/model-specific reasoning gates.

The serializer replaces only `providers.<provider>.models` and always manages `discover_models: false`. This prevents Hermes from replacing the filtered static catalog with the live `/models` response, which would reintroduce profiles below the context threshold and stale entries. It preserves existing provider connection/authentication fields separately, including `base_url`, `api_key`, `key_env`, transport, headers, TLS, timeouts, and output limits. It also preserves `model.default`, `model.provider`, auxiliary routing, fallback providers, and every unrelated configuration section. It never writes `provider_models_cache.json`, which is ephemeral discovery state.

## CLI and path behavior

`--target` accepts `pi`, `feynman`, `omp`, `opencode`, `forge`, `hermes`, or `all`; `all` remains the default.

Each target gets a path override and provider-ID override following the current naming convention. Existing pi and omp flags remain compatible.

When `--target all` is used, an absent target configuration is skipped with a warning so installed agents continue to synchronize. When one target is explicitly requested, an absent or invalid configuration is an exit-code-2 configuration error.

`--seed-overrides` accepts any one explicit target and rejects `all`. It reads the target's native managed entries and emits the existing target-neutral override schema.

Human and JSON output report skipped unavailable targets separately from skipped profiles. Dry-run remains the default.

## Preservation and failure rules

- Never overwrite a complete agent configuration with a generated template.
- Never delete or rewrite unrelated providers.
- Preserve existing managed-provider connection fields by default.
- Remove stale models only inside the managed provider.
- Back up every changed file immediately before an apply write.
- Use temporary-file replacement and reparse every written document.
- A serializer or parse failure for an explicitly selected target aborts with exit code 2.
- In `all` mode, a missing file is a warning; an existing but malformed file is an error rather than silently skipped.
- No literal user secret is read into output or generated into configs.

## Testing strategy

Tests use temporary profile and agent configuration fixtures and invoke synchronization functions or the CLI entry point. They must establish these observable contracts before implementation:

1. pi behavior remains unchanged, including connection-field and unrelated-provider preservation.
2. Feynman produces the pi schema at its own path.
3. omp retains comments, ordering, unrelated providers, and in-place entry metadata.
4. OpenCode writes a keyed models map with correct `limit` and `modalities` fields while preserving plugins and unrelated providers.
5. Forgecode updates one provider-array entry, maps capability fields, and preserves all other entries.
6. Forge path precedence follows explicit override, `FORGE_CONFIG`, `~/forge`, then `~/.forge`.
7. Forge endpoint normalization produces exactly one `/v1/chat/completions` suffix from supported base forms.
8. Every target removes stale managed models and preserves target-specific inherited output limits.
9. Apply followed by a second dry-run is idempotent for every target.
10. Hermes writes the static keyed model map with `context_length` and `supports_vision`, enforces `discover_models: false`, preserves the active model and unrelated YAML, and never touches provider caches.
11. `all` skips missing target files with warnings; an explicit missing target fails.
12. Existing pi and omp CLI options retain their meanings.
13. Backup files and written documents parse successfully.

Focused test execution verifies the synchronizer first. The repository quality gate then runs `go build ./...` and `go test ./...` because the skill is shipped within this repository even though its implementation is Python.

## Acceptance criteria

- One command synchronizes eligible model-loader profiles into every installed one of the six agents.
- pi and omp output behavior does not regress.
- Feynman shares pi's schema without duplicated serializer logic.
- OpenCode, Forgecode, and Hermes receive native, valid structures rather than pi-shaped approximations.
- Forgecode always targets the active provider file and full Chat Completions endpoint; Hermes always targets the authoritative config file rather than an ephemeral cache.
- Unrelated agent configuration is preserved.
- Dry-run, apply, backup, overrides, stale removal, missing-target policy, and idempotency are covered by automated tests.
- Skill instructions accurately document all six agents and contain no changelog or implementation history.

# Profiles

A profile is one JSON file at `~/.config/model-loader/profiles/<id>.json`. The basename **equals** the `id`. The canonical JSON Schema lives at `docs/profile-schema.json` (`schemaVersion: 3`, `additionalProperties: false`) and must stay in sync with `internal/domain/profile.go`.

## Top-level shape

```json
{
  "schemaVersion": 3,
  "id": "qwen3.6-27b-mtp-dflash-q4km-quality-256k",
  "name": "Qwen3.6-27B MTP+DFlash (Quality, 256k ctx)",
  "description": "Single-stream, highest quality. MTP + DFlash speculative decoding.",
  "tags": ["qwen", "dflash", "mtp"],
  "model": "/mnt/models/qwen2.5-27b-q4_k_m.gguf",
  "args": {
    "ctx-size": 262144,
    "n-gpu-layers": 99,
    "parallel": 1,
    "spec-type": ["dflash"],
    "mmproj": null
  },
  "extraArgs": ["--verbose"],
  "launch": {
    "backendId": "beellama-rtx3090",
    "env": ["GENESIS_ENFORCE_VERSION_RANGE=1"],
    "defaultBackground": true,
    "restart_policy": "on-failure",
    "max_restarts": 3,
    "backoff_seconds": 5
  },
  "meta": {
    "createdAt": "2026-06-21T18:30:00Z",
    "updatedAt": "2026-07-01T10:11:00Z",
    "lastUsedAt": "2026-07-02T08:00:00Z"
  },
  "pinned": true
}
```

## Field reference

| Field | Type | Notes |
|-------|------|-------|
| `schemaVersion` | int | Always `3` |
| `id` | string | Regex `^[a-z0-9]+([._-][a-z0-9]+)*$`; **equals filename**; auto-`Slugify`d from `name` on create |
| `name` | string | Display name. First sentence + headline metrics should track `args` |
| `description` | string | Optional long-form description |
| `tags` | []string | Optional, free-form |
| `model` | string | Absolute path to weights (or repo dir for Python backends) |
| `args` | map | Typed flags. Values are typed scalars or `[]string` for list-valued enums. **`port` is reserved and stripped** — the process manager assigns ephemeral ports |
| `extraArgs` | []string | Raw passthrough flags (no validation) |
| `launch` | object | `backendId` (req), `env` ([]string `K=V`), `defaultBackground`, `restart_policy` (`none`/`on-failure`/`always`), `max_restarts`, `backoff_seconds` |
| `meta` | object | `createdAt` / `updatedAt` / `lastUsedAt`. `lastUsedAt` is auto-updated by the process manager on first `/health` 200 |
| `pinned` | bool | Sort-first in the Profiles list |

## `args` value types

The `args` map is decoded into typed `FlagSpec`s by the per-backend schema:

- `0` = `bool` (`true`/`false`)
- `1` = `int`
- `2` = `float`
- `3` = `string`
- `4` = `enum` (string from `EnumValues`)
- `[]string` for list-valued enums (e.g. `spec-type: ["dflash"]`)

Curated enums can be list-valued (`internal/domain/flag_schema.go`); the validator accepts both single strings and string arrays for them.

## Naming convention (curation discipline — not code-enforced)

Profile IDs are lowercase kebab/slash-safe slugs and the filename basename equals `id`.

Canonical pattern:

```
<model-family-and-version>-<size>[-<variant>][-<quant>][-<capability/backend/mode>...]-<ctx>
```

- **Start with the real base model/family + version when available.** Do not start with backend, task, quant, or local nickname unless that is the model family itself.
- **End with the context window.** Source of truth = `args.ctx-size` (llama.cpp), `args.max-model-len` (vLLM), or `args.context-length` (SGLang). Use **binary-k floored** (`262144`→`256k`, `233472`→`228k`, `1048576`→`1024k`). No suffix may appear after the context label.
- **Capability segments mirror `args`, not intent:** `vision` only while `args.mmproj` or a vision-native backend config is active; `mtp` / `dflash` only while speculative args/config are active; backend/mode qualifiers (`vllm`, `tp2`, `beellama`, `textonly`, `tensor`, `layer`) sit before final `<ctx>`.
- **Name the real base model** (not the publisher's repackaging label).
- **Siblings differing only by serving mode** carry a disambiguator before `<ctx>`: `speed` / `throughput` / `quality`, `parallelN-Wk`, `cpumoe`, backend/mode.

## Authoring flow

Two paths, same destination:

1. **Web editor** — Profiles tab → `n` (or `e` on an existing profile). Opens an in-process HTTP server on `127.0.0.1:0`, launches your browser. Form is schema-driven from `BackendValidationSchema.Flags` + editable `Presentation` (Essentials / Advanced / Environment / Sizing tabs cycled by `Ctrl+T`).
2. **CLI** — `profile create` with `--file` (or `-` for stdin) and/or `--arg k=v` / `--extra-arg` / `--env K=V` flags. Edit via `profile edit <id>` to overlay onto an existing profile.

Both paths share the same validator (`validator.Validator`) and the same `profilestore.Write` path (atomic JSON, per-file flock).

## Customizations that don't survive `--help` re-parsing by default

- Manual flag **additions** to `args` (beyond what the schema knows about) are **preserved** as long as the schema's `Source.Editable` is `true`. When `false`, `RefreshSchema` (Backends tab `R` / `backend schema refresh`) wipes them.
- Manual edits to the **schema** itself are preserved only if the schema's `Source.Editable` flag is `true`. The web editor's **Customize mode** flips this flag on.

## Imports and exports

- `profile export [id…]` writes a JSON bundle (stdout by default, or `-o`)
- `profile import <path>` accepts `--mode merge|overwrite|rename`:
  - `merge` — non-conflicting fields overlay; conflicts prompt
  - `overwrite` — replaces existing
  - `rename` — auto-renames conflicting ids
- `I` in the TUI Profiles tab opens the import modal with the same three modes
- `u` (Profiles tab) undoes the last import with a field-diff modal

## Sizing and validation

- The TUI Models tab `→` / `g` opens the **sizing panel** for the selected model, which runs `sizing.Suggest` / `sizing.Fit` (Green / Yellow / Red GPU-memory fit)
- `profile validate <id|name>` runs the full validator: per-flag type/range/enum checks, cross-field rules, and env-var sanity. Exit code: `0` clean, `1` non-blocking warnings, `2` blocking errors
- Model-existence errors are downgraded to warnings (configure-now / download-later flow) — this is intentional

## Implementation notes for future agents

- `profilestore.Store` is an interface; `profilestore.NewFSStore(dir)` is the production implementation. Tests use in-memory doubles.
- One-file-per-profile: the per-file flock (`.<id>.lock`) guards RMW, not the directory. Concurrent edits to **different** profiles are unblocked.
- `.history/<id>.previous.json` is written **before** a destructive edit (delete, import-overwrite). Don't skip this path.
- The process manager's `LastUsedSink.MarkLastUsed(id)` is called on first `/health` 200; if you change that integration, also update `profilestore` tests.
- `domain.Profile` JSON tags are the wire format — bumping `schemaVersion` requires a migration in `internal/service/migration/`.
- Don't change `domain.Profile` or `profilestore` JSON without mirroring `docs/profile-schema.json` in the same commit. The validator and web editor both consume the schema.

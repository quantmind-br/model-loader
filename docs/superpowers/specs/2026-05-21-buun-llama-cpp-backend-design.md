# Design: First-class support for the `buun-llama-cpp` backend

**Date:** 2026-05-21
**Status:** Approved — ready for implementation planning

## Problem

`model-loader` supports several backends (`llama-server`, `vllm`, `sglang`, `dflash`). The user
maintains a fork of llama.cpp at `backends/buun-llama-cpp` that adds ~55 new CLI flags for
speculative decoding and KV-cache compression. We want it as a first-class backend with its own
identity, curated essentials, and validation schema — not merely a configured variant of the
existing `llama-server` backend.

## Key facts (from investigation)

- The fork compiles a binary still named `llama-server` (at
  `backends/buun-llama-cpp/build/bin/llama-server`, 8.1 MB, already built).
- Its `--help` output is **100% format-compatible** with upstream llama.cpp and is parseable by the
  existing `llamahelp` parser (`llamahelp.NewExecParserFor`).
- New flag families (verified present in build `9561 / e9187d155`): KV-cache "turbo" types
  (`turbo2/turbo3/turbo4/turbo2_tcq/turbo3_tcq` on `--cache-type-k`/`--cache-type-v`), DFlash
  cross-attention drafter (`--spec-draft-model`/`-md`, `--spec-dflash-default`,
  `--dflash-max-slots`), and generic speculative decoding (`--spec-type`, `--draft-max/min`).
  (`--tree-budget` and `--draft-topk` were referenced in early investigation but are **not** present
  in this build — excluded from the implementation.)
- `docs/profile-schema.json` does **not** enumerate backend-kind values (no `enum`), and
  `internal/domain/backend.go` has no kind-validation function — kinds are free string constants.
  Adding a new kind therefore does **not** change the persisted profile shape (no schema-doc sync
  required; verify during implementation).

## Approved decisions

1. **Identity:** first-class `BackendKind` (`buun-llama-cpp`), distinct from upstream `llama-server`.
2. **Schema strategy:** runtime `--help` parsing (modeled on `LlamaServerGenerator`) with an embedded
   curated fallback — auto-captures all fork flags, low manual maintenance.
3. **Essentials groups to surface:** generic `spec-type`, DFlash spec decoding, and KV-cache turbo
   (NOT the Qwen FIM presets).

## Architecture

### 1. Domain — new BackendKind
`internal/domain/backend.go`: add
```go
BackendKindBuunLlamaCpp BackendKind = "buun-llama-cpp"
```

### 2. Schema generator (runtime + fallback) — core of the design
Model on `LlamaServerGenerator` (runtime `--help` parse), **not** on `embeddedGenerator`
(DFlash/vLLM/SGLang are hand-curated only because their binaries lack a parseable `--help`).

- New `BuunServerGenerator` in `internal/service/backendschema/buun_generator.go`. It resolves the
  executable, runs `--help` via `llamahelp.NewExecParserFor(resolved)`, and persists the schema —
  identical to `LlamaServerGenerator` except for the accepted `Kind`
  (`domain.BackendKindBuunLlamaCpp`).
- **Refactor (low risk):** generalize the `--help`-parsing generator logic so it accepts the expected
  `kind` (extract a shared helper or parameterize), avoiding duplication of the parse-and-persist
  body. `LlamaServerGenerator` keeps its current public behavior.
- **Embedded fallback:** new `internal/service/buunhelp` package with `EmbeddedSchema()` =
  `llamahelp.EmbeddedSchema()` (v7376 base) **plus** the ~15 curated fork flags (turbo cache, dflash,
  spec-type). Only the curated subset is maintained by hand, not all 55. Used when the binary is
  absent at generation time. A `WriteEmbeddedFallback`-style helper for the buun kind writes it to
  the schema store.

### 3. Bootstrap registration
`cmd/model-loader/bootstrap.go`, alongside the existing `Register` calls:
```go
schemaManager.Register(domain.BackendKindBuunLlamaCpp, backendschema.NewBuunServerGenerator(schemaStore))
```

### 4. Argument construction
`internal/service/processmgr/args.go`:
- Add `case domain.BackendKindBuunLlamaCpp` in `BuildArgsForBackend` that **reuses
  `buildLlamaArgs(p)`** — the CLI is identical to upstream, so no new builder is written.
- Add the matching `case` wherever the kind is resolved centrally at launch (the path introduced by
  commit 36e5ade), so the buun binary is launched correctly.

### 5. Binary resolution
No change. It is a native binary and falls through `resolveExecutable`'s `default`
(`llamabin.Resolve`). The user sets `Backend.Executable` to the absolute path of the fork's
`llama-server`, so there is no PATH collision with upstream.

### 6. Curated essentials
`internal/ui/pages/profile_editor/essentials.go`: new `domain.BackendKindBuunLlamaCpp` entry.
- **Base (llama):** `n-gpu-layers`, `ctx-size`, `flash-attn`, `port`
- **KV-cache turbo:** `cache-type-k`, `cache-type-v` (description hints the turbo2/3/4/tcq types)
- **DFlash:** `spec-draft-model` (`-md`), `spec-dflash-default`, `dflash-max-slots`
- **Generic spec-type:** `spec-type`, `draft-max`, `draft-min`

This is an intentional, explicitly-requested addition to the curated essentials layer.

**Deviations from the initial draft (corrected during implementation against build 9561):**
- `--tree-budget` and `--draft-topk` were dropped — they **do not exist** in the verified fork
  build (`9561 / e9187d155`). They were listed speculatively in early investigation; the live
  `--help` does not emit them. If a future fork build adds them, add them to both `buunRows`
  (`internal/service/buunhelp`) and the essentials entry.
- `host` was dropped from the base essentials — it is absent from the upstream llama embedded base
  (so `essentialsFor` would drop it in fallback mode) and is also omitted from the existing
  `llama-server` essentials entry. Excluding it keeps buun consistent with the llama-server UX.

### 7. UI / validator
No changes required:
- `kindOptions()` / `sortedKinds()` in `internal/ui/pages/backends.go` iterate over
  `manager.Generators()`, so the new kind appears automatically after registration.
- `modelLabel`/`modelDesc` defaults (`Model path (.gguf)`) are already correct — the fork uses GGUF.
- Validator `supportsHFRepo` follows `llama-server` (returns false for buun; no change).

## Error handling / edge cases

- **Binary absent at schema generation:** generator falls back to the embedded curated schema
  (degraded but functional — base + curated fork flags validate).
- **Older fork binary missing newer flags in `--help`:** unknown flags are treated as unknown by
  validation and routed to `extraArgs`.
- **Schema editable guard:** like the other generators, regeneration is skipped when the existing
  schema has `source.editable=true`, preserving manual edits.

## Testing

- `internal/service/backendschema/buun_generator_test.go` — generate schema from a `--help` fixture;
  assert kind acceptance and rejection of wrong kinds.
- `internal/service/processmgr/buun_kind_resolve_test.go` — mirror `dflash_kind_resolve_test.go` for
  central kind resolution at launch.
- `internal/service/processmgr/args_test.go` — assert buun produces the same args as `llama-server`.
- Embedded fallback test — assert `buunhelp.EmbeddedSchema()` contains the curated turbo/dflash/
  spec-type flags.

## Out of scope (YAGNI)

- Qwen FIM presets (`--fim-qwen-7b-spec` / `--fim-qwen-14b-spec`) as essentials — available via the
  full flag list / extraArgs, not surfaced as curated fields.
- Auto-building the fork or auto-detecting its binary path — the user registers the backend manually
  in the Backends tab.
- Changes to `docs/profile-schema.json` — no persisted-shape change (verify during implementation).

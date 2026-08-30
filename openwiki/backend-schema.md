---
type: Architecture
title: Backend schema generation and validation
description: How per-backend validation schemas are produced from --help output, embedded golden fallbacks, and hand-curated overlays, then merged so live-parsed facts stay authoritative while curated metadata fills semantic gaps. Covers the manager, generators, merge core, the spec-type enum system, and the presentation layer.
tags: [backendschema, schema, validation, curated, spec-type, parser]
---

# Backend Schema

Each backend needs a validation schema describing its CLI flags. `internal/service/backendschema/` orchestrates generation from three tiers of flag data that combine differently per backend kind. The system spans the `Manager`, per-kind `Generator` implementations, a `merge` core, and a `presentation` layer — plus the per-kind `*help` parser packages.

## Generation strategies

Every generator first checks `existing.Source.Customized` and returns early (skips regeneration) to protect operator edits during incidental re-runs. The strategies then diverge:

<!-- openwiki: mermaid parse failed and this diagram was converted to a text fence so it does not break rendering. Fix the diagram source and restore the mermaid fence. Parser error: Heuristic: an unescaped angle bracket inside a label breaks rendering; rephrase the label. -->
```text
flowchart TD
    KIND{"Backend kind?"}
    KIND -->|"llama-server family"| LIVE["live --help parse<br/>llamahelp.ParseHelp"]
    LIVE -->|"parse succeeds"| ENRICH["mergeWithCuratedEnrich<br/>appendMissing=false<br/>parsed authoritative on existence + enums"]
    LIVE -->|"parse fails"| GOLDEN["embedded golden JSON<br/>help-v10686.golden.json, 252 flags"]
    GOLDEN --> APPEND["mergeWithCurated<br/>appendMissing=true<br/>curated adds missing flags"]
    ENRICH --> OUT["BackendValidationSchema"]
    APPEND --> OUT
    KIND -->|"vLLM, SGLang, ik, Tabby, Unsloth, DFlash"| CURATED["pure hand-curated<br/>Curated*Schema or *help.EmbeddedSchema<br/>no --help execution"]
    CURATED --> OUT
    KIND -->|"BeeLlama, Buun"| FORK["live --help parse"]
    FORK --> APPEND2["mergeWithCurated<br/>appendMissing=true<br/>curated injects fork-only flags"]
    APPEND2 --> OUT
```

Caption: the three generation strategies. Live-parse success uses **enrich** mode (parsed is authoritative on flag existence and enum values, so forks do not inherit phantom upstream flags); fallback/fork modes use **append-missing** mode (curated overlay adds flags the parse omits). vLLM/SGLang/ik use pure curation because their argparse/pre-arg.cpp `--help` formats are not parseable by `llamahelp`.

## The merge core

Two entry points share `mergeCurated(full, curated, appendMissing bool)`:

- **`mergeWithCuratedEnrich`** (`appendMissing=false`, llama-server live parse): curated-only flags are **dropped** — the parsed binary is authoritative on what exists (fixes S8: a prisma-ml fork gaining phantom upstream flags it rejects at launch). Curated `EnumValues`/`Default`/`Min`/`Max` override **only when** the parsed value is nil/empty (fixes S16: a fork's real `--spec-type` enum was being clobbered by upstream's curated list).
- **`mergeWithCurated`** (`appendMissing=true`, golden fallback + forks): curated-only flags are **appended**; curated constraints override parsed values.

Inside `mergeCurated`: clone both sides (defensive copies eliminate shared-slice aliasing), resolve matches deterministically (iterate curated keys in **sorted order**, snapshotting the parsed flag surface before any append so a curated alias cannot match a just-appended curated-only entry — 2cef620 fix), enrich matched flags with `HelpText`/`Group`/`Short`/`Aliases`/constraints, then `normalizePresentation`.

## Manager and `RefreshSchema`

`Manager.RefreshSchema` (`manager.go`) is the explicit user-driven regeneration path (`go run ./cmd/regenerate-schemas` or `backend schema refresh`): load catalog → find backend → load previous schema → **delete** the existing ref → `Generate` → if the previous schema was `Source.Customized`, reconcile the operator's layout onto the regenerated flag set via `ReconcilePresentation` (keep group order/names, drop removed flags, append new flags into their natural group) and `ReconcileRules` (drop rules referencing removed flags) → save. Scope boundary: refresh re-derives flag **facts** from the backend, so per-flag constraint edits and operator-added flags are replaced; only layout (presentation) and rules survive.

## The `--help` parser

`internal/service/llamahelp/parser.go::ParseHelp` scans llama-server `--help` output:
- **Section headers** (`----- <name> params -----`) become `FlagSpec.Group`.
- **Flag lines** (any line starting with `-` at column 0) are split into alias-chunk + description; canonical long is the last alias unless it is a `no-` negation.
- **Type inference**: no placeholder → Bool; `[a|b|c]`/`{a,b,c}` → Enum; `<0|1>`/`<0...100>` → Int with Min/Max (angle-bracket detection added in a18569e); `N`/`INDEX` → Int; `F`/`RATE` → Float; else String. Float disambiguation promotes Int→Float when the default has a `.`.
- **Enum extraction** (three forms): bracket/brace placeholders; **inline comma-lists** (a18569e: bare lowercase-alphanumeric+hyphen/underscore lists like `none,draft-simple,...`); `allowed values:` continuation blocks.
- **Defaults**: pulls `(default: X)`; a18569e skips inherited defaults containing `--` ("same as --cpu-strict") and drops non-numeric coerced results for numeric types ("read from model").

Embedded fallbacks: `llamahelp/embedded.go::EmbeddedSchema()` returns 13 essential flags (`embedded-v10686`); `backendschema/golden_embed.go` `//go:embed`s the full 252-flag golden (`testdata/help-v10686.golden.json`). A duplicate embed copy at `internal/service/backendschema/testdata/` must be kept byte-identical by hand when the root golden is regenerated.

## Spec-types

`spec-type` is the speculative-decoding strategy selector — a `listEnumFlag` (`Type=Enum`, `List=true`) so the validator splits `draft-mtp,ngram-mod` on `,` and checks each element. Per-backend dialects (after 2cef620):

| Backend | `spec-type` EnumValues |
|---------|------------------------|
| llama-server | `none, draft-simple, draft-eagle3, draft-mtp, draft-dflash, draft-dspark, ngram-simple, ngram-map-k, ngram-map-k4v, ngram-mod, ngram-cache` |
| BeeLlama | `none, draft-simple, draft-eagle3, draft-mtp, draft-dflash, ngram-simple, ngram-map-k, ngram-map-k4v, ngram-mod, ngram-cache` |
| Buun | the BeeLlama set **plus** `suffix, copyspec, recycle, dflash` (buun shorthand) |

Note llama-server and BeeLlama no longer share an identical enum: llama-server added `draft-dspark` while BeeLlama's fork tree has not. Pre-2cef620, buun's curated list lacked `draft-dflash`, so the upstream spelling was rejected and valid configs got routed through `extraArgs` (the passthrough that bypasses validation). `applyExtraArgsRules` deliberately does **not** type/enum-check known flags in extra args — only emits a non-blocking warning for unrecognized flags (BUGS.md S1: curated schemas can lag the binary).

## Presentation layer

`presentation.go` builds the Essentials/advanced layout used by the web editor:
- `essentialSeed` maps `BackendKind` → list of flag long-names that land in the top Essentials group (e.g. llama-server: `n-gpu-layers, ctx-size, batch-size, ubatch-size, flash-attn, cache-type-k, cache-type-v`). `port` is intentionally absent from all seeds — the process manager owns port allocation. **Do not** extend `essentialSeed` without explicit request.
- `BuildPresentation` synthesizes a default layout: Essentials group (seed flags that exist, in seed order) + remaining flags grouped by `FlagSpec.Group`.
- `ReconcilePresentation`/`ReconcileRules` carry operator layout across regeneration.

## Validation

`internal/service/validator/rules.go` enforces the schema:
- `applyTypeRules` — Bool/Int/Float/String/Enum checks; `checkIntRange` consults `AllowedInts` (exact-match allowlist before Min/Max, added 2cef620) then Min/Max; `IsPort` enforces 1–65535.
- `checkEnum` — for `List` flags accepts comma-string or JSON array; each element must be in `EnumValues`. Scalar enums take a single string.
- `applyExtraArgsRules` — raw passthrough; warning-only for unrecognized flags.
- `applyRequiredRules` / cross-field `CrossFieldRule`s (declarative `When` → `Then`, severity `warning|error`).

## Recent schema-sync changes

- **`2cef620` (accept llama-family spec-types)** — added `AllowedInts []int`; major `merge.go` refactor (defensive cloning, deterministic sorted matching, `parsedFlags` snapshot before append, `normalizePresentation`); added `draft-dflash` to spec-type enums; added buun VBR flags + draft aliases; translated buun help strings to English. ~19 files.
- **`a18569e` (prisma-ml schema sync)** — gated `EnumValues`/`Default`/`Min`/`Max` overrides on `appendMissing || parsed-is-empty` (S16: a prisma-ml fork's `--spec-type draft-dspark` enum was being overwritten); parser inline-comma-list enum detection + angle-bracket int constraints; 30 new llama-server curated flags; golden regeneration to `help-v10152`. ~8 files.

## Key files

| File | Role |
|------|------|
| `manager.go` | Orchestrator; catalog CRUD + `RefreshSchema`. |
| `generator.go` | `Generator` interface, `LlamaServerGenerator`, `parseHelpSchema`, `WriteEmbeddedFallback`. |
| `merge.go` | `mergeWithCurated` / `mergeWithCuratedEnrich` / `mergeCurated`. |
| `presentation.go` | `essentialSeed`, `BuildPresentation`, `ReconcilePresentation`. |
| `golden_embed.go` | `//go:embed testdata/help-v10686.golden.json` fallback. |
| `register.go` | `RegisterDefaults` — wires all 9 generators. |
| `curated_{llama,beellama,buun,ik,vllm,sglang}.go` | Hand-curated overlays. |
| `{vllm,sglang,tabby,unsloth,beellama,buun,ik,embedded}_generator.go` | Per-kind generators. |

Per-kind help parsers: `internal/service/{llamahelp,vllmhelp,sglanghelp,dflashhelp,buunhelp,unslothhelp,tabbyhelp}/` each export an `EmbeddedSchema()` via `BuildFlagSchema`.

---
description: Sync Model Loader's embedded/curated backend schemas (flags + Essentials) with a backend's current implementation
argument-hint: <backend_path>
allowed-tools: Read, Edit, Write, Glob, Grep, Bash
---

# Backend Schema Update

Synchronize Model Loader's embedded/curated schemas with the current state of a backend implementation. The backend located at the path given in `$1` is the **source of truth** for flag facts (existence, type, default, enum). The Essentials grouping is governed by the **configured on-disk schemas** (see step 5).

**Backend path:** `$1`

If `$1` is empty, stop and ask the user for the backend path before doing anything else.

## 0. Identify the backend kind and its schema pattern

Model Loader has **8 backend kinds** (`internal/domain/backend.go`), each with a registered generator (`internal/service/backendschema/register.go`). How a kind's schema is built decides **which files you edit** — there is no single workflow. Map `$1` to a kind, then to its pattern:

| `backends/<dir>` | Catalog kind | Schema **Pattern** | Authoritative model-loader file(s) |
|---|---|---|---|
| `llama.cpp-stable`, `llama.cpp-nightly` | `llama-server` | **A — live `--help` + curated overlay** | `backendschema/curated_llama.go` + golden `testdata/help-v9761.golden.json` + parser `llamahelp/` |
| `beellama.cpp` | `beellama-cpp` | **A** | `backendschema/curated_beellama.go` |
| `buun-llama-cpp` | `buun-llama-cpp` | **A** | `backendschema/curated_buun.go` |
| `vllm-stable`, `vllm-nightly` | `vllm` | **B — pure curated Go (NO `--help` parse)** | `backendschema/curated_vllm.go` |
| `sglang-stable`, `sglang-nightly`, `sglang-unlimited`, `sglang-dflash` | `sglang` | **B** | `backendschema/curated_sglang.go` |
| `lucebox-hub` | `dflash` | **C — embedded Go rows** | `dflashhelp/dflashhelp.go` |
| `unsloth` | `unsloth` | **C** | `unslothhelp/unslothhelp.go` |
| `tabby` | `tabby` | **C** | `tabbyhelp/tabbyhelp.go` |

**Pattern semantics:**

- **A (live `--help` + curated):** the generator runs the resolved binary's `--help`, parses it (`llamahelp/parser.go`), and overlays the curated Go schema (groups, descriptions, aliases, fork-only flags, Essentials, rules). llama-server additionally falls back to the embedded golden JSON when no binary resolves. **Both the parsed surface AND the curated overlay matter.**
- **B (pure curated Go):** the generator returns the hand-curated Go schema verbatim; `--help` is **never** parsed (vLLM/sglang `--help` is too unstable). The curated `*.go` file is the *only* flag source. **Do not scrape `--help` for these.**
- **C (embedded Go rows):** the generator returns curated rows from a `*help` package (`dflashhelp`/`unslothhelp`/`tabbyhelp`) with no `--help` parse. Edit the rows in that package.

> The vestigial packages `sglanghelp`, `vllmhelp`, and `buunhelp` exist but are **imported by no non-test code** — the live sglang/vllm/buun schemas come from `curated_sglang.go`/`curated_vllm.go`/`curated_buun.go`. Do not edit the vestigial packages expecting an effect. (The package `AGENTS.md` claiming sglang/vllm use `*help` is stale.)

## 1. Extract the backend's current flag surface (source of truth)

Inspect the backend at `$1` per its pattern:

- **Pattern A (llama-server / beellama-cpp / buun-llama-cpp):** flags are declared in `common/arg.cpp`, filtered to the server by `LLAMA_EXAMPLE_SERVER` (singleton init near the top of the file; per-flag `.set_examples({..., LLAMA_EXAMPLE_SERVER, ...})` / `.set_excludes({LLAMA_EXAMPLE_SERVER})`; `if (ex == LLAMA_EXAMPLE_SERVER)` server-only blocks). Read `common/arg.cpp` for the authoritative set; `build/bin/llama-server --help` is the convenient cross-check.
  - **Forks add deltas only visible in source.** BeeLlama and Buun add TurboQuant KV types (`turbo2_tcq`/`turbo3_tcq`/`turbo4_0`/`turbo8_0` in `kv_cache_type_from_str`), a `--spec-dflash-*` / `--spec-dm-*` adaptive-draft family, `--mmproj-gpu-swap`, `--reasoning-loop-guard`, etc. BeeLlama also ships a `docs/beellama-args.md` cataloguing its delta. `--help` alone will miss flags that the curated overlay is the only place to surface.
- **Pattern B (vllm / sglang):** the engine is pip-installed inside the dir's `.venv` (the dir itself is wrapper-only: `*-serve.sh` + `backend-build.sh`). Read the installed arg declarations:
  - vLLM: `<$1>/.venv/lib/python*/site-packages/vllm/engine/arg_utils.py` (`EngineArgs`/`AsyncEngineArgs` dataclasses).
  - sglang: `<$1>/.venv/lib/python*/site-packages/sglang/srt/server_args.py` (`ServerArgs` dataclass).
- **Pattern C:**
  - dflash (`lucebox-hub`): `server/src/server/server_main.cpp` — read the **argument parse loop**, not just `print_usage` (usage text is incomplete: e.g. `--spark*` flags are parsed but undocumented).
  - unsloth: `cli.py` / `unsloth-cli.py` (and `studio/`).
  - tabby: `common/args.py` + `common/config_models.py` + `main.py`.

For each flag record: name, aliases, type, default, allowed values/enum, description, deprecation, and add/remove relative to what Model Loader knows.

**Capture the backend version** (the updated schemas must record it):

- A-llama family / dflash / unsloth / tabby (git checkouts): `git -C "$1" describe --tags --always` + `git -C "$1" rev-parse --short HEAD`.
- vllm / sglang (pip): the `*.dist-info` dir name under `.venv/.../site-packages/` (e.g. `vllm-0.24.0.dist-info`, `sglang-0.5.9.dist-info`).
- A binary: `build/bin/llama-server --version`.

## 2. Load Model Loader's current schema sources

Open the file(s) named for the kind in the step-0 table. Reference context:

- Curated overlays: `internal/service/backendschema/curated_llama.go`, `curated_vllm.go`, `curated_sglang.go`, `curated_beellama.go`, `curated_buun.go`.
- Embedded help packages: `internal/service/{llamahelp,dflashhelp,unslothhelp,tabbyhelp}/` (each `EmbeddedSchema()` builds rows with a pinned version id — `embedded-v9761`, `embedded-dflash-v3`, `embedded-unsloth-v1`, `embedded-tabby-v1`).
- Golden fixture (llama-server only): **two copies must stay byte-identical** —
  - `testdata/help-v9761.golden.json` (root; written by `-update`),
  - `internal/service/backendschema/testdata/help-v9761.golden.json` (`//go:embed`-ed by `golden_embed.go`; **NOT** written by `-update` — sync it by hand).
- Domain types: `internal/domain/backend.go` (`BackendKind`), `internal/domain/backend_schema.go` (`FlagSchema`, `BackendValidationSchema`, `FlagSpec`, `Presentation`).
- Essentials authority: see step 5 (`presentation.go::essentialSeed` for pattern C; the curated `*Presentation()` for A/B).

## 3. Diff the two schema sets

Produce a structured comparison covering, per flag:

- **Additions** — in the backend, missing from the schema.
- **Removals** — in the schema, gone from the backend.
- **Renames** — canonical name/aliases changed (match by semantics, not just name).
- **Type changes** — string→int, bool→enum, etc.
- **Default/constraint changes** — changed defaults, new enum values, changed min/max.
- **Metadata changes** — description, deprecation, group moves.

Present the full diff to the user **before** applying any changes.

## 4. Update the flag definitions

Apply the diff to the file(s) for the kind's pattern:

- **A:** edit `curated_<kind>.go` (add/remove/adjust `FlagSpec`s and groups). For llama-server, if the parsed surface drifted, refresh the golden (step 6) and only touch `llamahelp/embedded.go` rows if an *essential* flag changed.
- **B:** edit `curated_vllm.go` / `curated_sglang.go` directly (the sole flag source).
- **C:** edit the row slice in `dflashhelp.go` / `unslothhelp.go` / `tabbyhelp.go`, and bump that package's pinned version id (e.g. `embedded-dflash-v3` → `-v4`) plus its tracking comment (commit/version).

Rules: keep all descriptions, labels, group names in **English** (project rule). Do **not** hand-edit a schema JSON's `presentation`/`rules` blocks on disk (web Customize mode owns those; `RefreshSchema` preserves them when `source.editable=true`). Mirror any persisted-profile-structure change into `docs/profile-schema.json`.

## 5. Reconcile Essentials from the configured schemas

The **highlighted "Essentials" group** is the operator-facing shortlist. Establish it from the **configured on-disk schemas** (`~/.config/model-loader/backends/schemas/<id>.json`), which are the live source of truth the operator curated via the web editor. Read the relevant schema's `presentation.groups[]` entry whose `highlighted == true` (or `name == "Essentials"`).

Where Essentials are **persisted in code** depends on the pattern:

- **A/B** — Essentials live in the curated Go `*Presentation()` function and the on-disk configured schema **mirrors** it. When the configured schema's Essentials differ from the curated `*Presentation()`, treat the configured schema as intent and update the Go function:
  - `curated_llama.go::llamaPresentation()`
  - `curated_beellama.go::BeeLlamaPresentation()`
  - `curated_buun.go::BuunPresentation()`
  - `curated_vllm.go::vllmPresentation()`
  - `curated_sglang.go::sglangPresentation()`
- **C** — embedded schemas carry **no** Presentation; Essentials are synthesized at bootstrap by `presentation.go::BuildPresentation` from the per-kind `essentialSeed` map. Update the kind's entry in `essentialSeed` (`internal/service/backendschema/presentation.go`).

**Canonical Essentials currently configured** (verified against the on-disk schemas; use as the baseline, and never add a flag that the schema does not define):

| Kind | Essentials (in order) | Authority |
|---|---|---|
| `llama-server` | `hf-repo, ctx-size, host, port, n-gpu-layers, device, parallel, threads, batch-size, ubatch-size, cache-type-k, cache-type-v, cont-batching, api-key, alias` | `llamaPresentation()` |
| `beellama-cpp` | `n-gpu-layers, ctx-size, host, port, batch-size, ubatch-size, flash-attn, cache-type-k, cache-type-v, cache-ram, kv-unified, mmproj, jinja, reasoning, spec-type, spec-draft-hf, spec-draft-model, spec-draft-ngl, spec-dflash-cross-ctx, spec-dflash-max-slots` | `BeeLlamaPresentation()` |
| `buun-llama-cpp` | `hf-repo, hf-token, ctx-size, host, port, n-gpu-layers, device, parallel, threads, batch-size, ubatch-size, cache-type-k, cache-type-v, cache-ram, kv-unified, cont-batching, api-key, alias` | `BuunPresentation()` (no configured schema on disk — Go is the only authority) |
| `vllm` | `host, port, api-key, served-model-name, dtype, max-model-len, quantization, tensor-parallel-size, gpu-memory-utilization, kv-cache-dtype, enable-prefix-caching, max-num-batched-tokens, max-num-seqs, enable-chunked-prefill, uvicorn-log-level` | `vllmPresentation()` |
| `sglang` | `served-model-name, host, port, api-key, context-length, dtype, quantization, kv-cache-dtype, mem-fraction-static, max-running-requests, max-total-tokens, tensor-parallel-size, tp-size, pipeline-parallel-size, data-parallel-size, device, enable-multimodal, chat-template` | `sglangPresentation()` |
| `dflash` | `draft, max-ctx, ddtree, ddtree-budget, cache-type-k, cache-type-v, fa-window` | `essentialSeed[dflash]` |
| `unsloth` | `gguf-variant, ctx-size, n-gpu-layers, parallel, flash-attn, cache-type-k, cache-type-v` | `essentialSeed[unsloth]` |
| `tabby` | `max-seq-len, cache-mode, cache-size, tensor-parallel, gpu-split, gpu-split-auto, vision` | `essentialSeed[tabby]` |

When step 4 **adds** a flag the operator should reach quickly (a headline launch knob: a sizing/parallelism/KV/quant/spec control), propose adding it to that kind's Essentials list **and ask the user** before doing so (Essentials changes are operator-facing). When step 4 **removes** a flag that was in Essentials, drop it from the list too. Keep Essentials in code and the configured schema consistent.

> Note: `essentialSeed`'s header says `port` is intentionally omitted (the process manager owns the port). The configured A/B schemas nonetheless list `host`/`port` in Essentials because operators added them via Customize; that is intentional drift — preserve the configured intent, do not "fix" it back out.

## 6. Validate

- `make build` — must compile.
- `go test ./...` — full suite. For an intended llama-server flag change that moves the parsed surface, regenerate the golden and **sync both copies**:
  - `go test ./internal/service/llamahelp -update` (writes root `testdata/help-v9761.golden.json`),
  - copy it to `internal/service/backendschema/testdata/help-v9761.golden.json` (the `//go:embed` copy),
  - if the build number advanced, also bump the version id (`embedded-v9761` in `llamahelp/embedded.go` and `embedded-v9761-full` in `backendschema/golden_embed.go`) and rename both golden files consistently,
  - re-run `go test ./...` to confirm green.
- Targeted: `go test ./internal/service/backendschema/... ./internal/service/<kind>help/...` for the kind you touched.
- Optional live regen of on-disk schemas: `go run ./cmd/regenerate-schemas` re-runs `RefreshSchema` for catalog backends — **but it registers only 6 of 8 generators (no Unsloth, no Tabby)** and skips any schema with `source.editable=true`. Use it as a sanity check for the 6 it covers; for unsloth/tabby, rely on the `*help` package tests instead.

## 7. Report

- Backend version the schemas now track (old → new), with detection method.
- Counts + lists of additions, removals, renames, type changes, metadata changes.
- **Essentials delta** — flags added/removed from the kind's Essentials, and where persisted (curated `*Presentation()` vs `essentialSeed`).
- Files modified.
- Test/build results (including golden-copy sync if llama-server).
- **Manual review needed** — unconfirmed renames, flags whose semantics you could not infer, deprecations without replacements, and any proposed Essentials change awaiting user approval.

## Constraints

- The backend at `$1` always wins on flag facts (existence, type, default, enum); curated metadata (labels, grouping) is preserved unless contradicted.
- Pattern dictates the file: A → `curated_*.go` (+ llama golden); B → `curated_vllm.go`/`curated_sglang.go` only (never `--help`); C → the `*help` row slice (+ version bump).
- Essentials authority: configured on-disk schema → persisted in the curated `*Presentation()` (A/B) or `essentialSeed` (C). Propose Essentials changes; apply only with user approval.
- The llama golden lives in **two** locations — keep them byte-identical; `-update` only writes the root one.
- Never edit golden fixtures by hand. Never edit the vestigial `sglanghelp`/`vllmhelp`/`buunhelp` packages expecting runtime effect.
- All schema text must be in English.

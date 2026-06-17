---
description: Sync Model Loader's embedded/curated backend schemas with a backend's current implementation
argument-hint: <backend_path>
allowed-tools: Read, Edit, Write, Glob, Grep, Bash
---

# Backend Schema Update

Synchronize Model Loader's embedded/curated schemas with the current state of a backend implementation. The backend located at the path given in `$1` is the **source of truth**.

**Backend path:** `$1`

If `$1` is empty, stop and ask the user for the backend path before doing anything else.

## Workflow

### 1. Extract the backend's current schema (source of truth)

Inspect the backend at `$1` to determine its current flag surface:

- If it is a built binary (e.g. `llama-server`, vLLM entry point), run its `--help` / `--help-all` output and capture it.
- If it is a source tree, locate where CLI flags/arguments are declared (e.g. `common/arg.cpp` for llama.cpp-family backends, `argparse`/`pydantic` config for vLLM-style backends) and read the flag definitions directly.
- For each flag record: name, aliases, type, default value, allowed values/enum, description, deprecation markers, and any new/removed flags relative to what Model Loader knows.

Also identify the backend's version (git tag/commit, `--version` output, or build metadata) — the updated schemas must record it.

### 2. Load Model Loader's embedded/curated schemas

The schemas live in the model-loader repo:

- Curated schema definitions: `internal/service/backendschema/curated_*.go` (e.g. `curated_llama.go`, `curated_vllm.go`, `curated_beellama.go`, `curated_buun.go`) — pick the file matching the backend kind detected in step 1.
- Embedded `--help` snapshot + parser: `internal/service/llamahelp/` (pinned schema version, e.g. `embedded-v7376`) and sibling help packages (`buunhelp`, `dflashhelp`) where applicable.
- Presentation/Essentials seed: `internal/service/backendschema/presentation.go` (`essentialSeed`) — **read-only context; do NOT extend it unless the user explicitly asks.**
- Domain types: `internal/domain/` (`FlagSchema`, `BackendValidationSchema`).
- Golden fixtures: `testdata/help-*.golden.json` — **never hand-edit; regenerate via `go test ./... -update`.**

### 3. Diff the two schema sets

Produce a structured comparison covering:

- **Additions** — flags present in the backend but missing from the curated/embedded schema.
- **Removals** — flags in the curated schema that no longer exist in the backend.
- **Renames** — flags whose canonical name or aliases changed (match by description/semantics, not just name).
- **Type changes** — string→int, bool→enum, etc.
- **Default/constraint changes** — changed defaults, new enum values, changed ranges.
- **Metadata changes** — description text, deprecation status, group/category moves.

Present the full diff to the user **before** applying any changes.

### 4. Update the embedded schemas

Apply the diff to the curated Go sources and embedded help data:

- Edit the matching `curated_*.go` to add/remove/adjust flags. Keep all descriptions, labels, and group names in **English** (project language rule).
- If the embedded `--help` snapshot needs refreshing, replace the embedded help text in the relevant help package and update the pinned version identifier consistently everywhere it appears.
- Do **not** hand-edit any schema JSON's `presentation`/`rules` blocks — those are managed via the web Customize mode and preserved by `RefreshSchema`.
- Mirror any change that affects the persisted profile structure into `docs/profile-schema.json`.

### 5. Validate

- `make build` — must compile.
- `go test ./...` — run the full suite; if golden fixtures are stale because of the intended schema change, regenerate with `go test ./... -update` and re-run the tests to confirm they pass.
- Sanity-check that schema generation still works for the updated backend kind (e.g. the `backendschema` Manager tests with the fake generator).

### 6. Report

Finish with a summary report containing:

- Backend version the schemas now track (old → new).
- Counts and lists of additions, removals, renames, type changes, and metadata changes applied.
- Files modified.
- Test/build results.
- **Manual review needed** — anything ambiguous: suspected renames you could not confirm, flags with semantics you could not infer, deprecations without replacements, or presentation/Essentials candidates (which require explicit user approval to add).

## Constraints

- The backend at `$1` always wins on flag facts (existence, type, default, enum); curated metadata (friendly labels, grouping) is preserved unless contradicted.
- Never extend `essentialSeed` without explicit user request.
- Never edit golden fixtures by hand.
- All schema text must be in English.

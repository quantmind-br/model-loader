# PRD — Service & Misc Cleanups

**Source**: IDEATION_CODE_QUALITY.md
**Generated**: 2026-05-18

## Implementation Order
1. CQ-013 — Add single-line comment documenting the `Profile.Args` int → float64 JSON gotcha.
2. CQ-016 — Dispose of the orphaned `01-PRD_unify_monitor_server_tabs.md` at repo root.
3. CQ-007 — Topic-group split of `internal/service/processmgr/manager.go`.
4. CQ-004 — Shared `BuildFlagSchema` builder used by the three `EmbeddedSchema()` packages.

---

## CQ-013: Document `Profile.Args` JSON Round-Trip Gotcha

### Scope
**In scope**:
- Add a single-line comment above the `Args map[string]any` field of `domain.Profile` noting that integer values round-trip as `float64` through JSON.

**Out of scope**:
- Changing the field type.
- Adding runtime conversion or normalization.
- Adding any other comments to `domain/profile.go`.

### Technical Approach
- Open `internal/domain/profile.go`. Locate the `Args` field (currently line 22 per the analysis).
- Insert immediately above the field:
  ```go
  // Note: ints round-trip as float64 via JSON; see validator.checkType.
  ```
- No code changes.

### Touchpoints
- `internal/domain/profile.go` — add the comment.

### Contracts
No code or signature changes.

### Acceptance Criteria
- [ ] Single-line comment present immediately above the `Args` field.
- [ ] Comment references `validator.checkType` verbatim.
- [ ] `go vet ./...` clean.

### Dependencies
- None.

---

## CQ-016: Dispose of Orphaned `01-PRD_unify_monitor_server_tabs.md`

### Scope
**In scope**:
- Remove `01-PRD_unify_monitor_server_tabs.md` from the repository root, choosing one of:
  - Move it under `docs/superpowers/` (preserves the spec as historical reference and matches the convention for design specs).
  - Delete it (if no longer useful).

**Out of scope**:
- Editing the document's contents.
- Touching any other untracked or tracked PRD files.

### Technical Approach
- Decide on move vs. delete:
  - **Move** if the spec still has value as a record of intent (recommended default given the convention; `docs/superpowers/` already houses other design specs).
  - **Delete** if everyone agrees the spec is fully obsoleted by the merged commit `985ecd2` and there is no need to retain it.
- If moving: `git mv 01-PRD_unify_monitor_server_tabs.md docs/superpowers/01-PRD_unify_monitor_server_tabs.md`. The file is currently untracked, so `git mv` will fall back to a plain `mv` + add. If unsure, use `mkdir -p docs/superpowers && mv 01-PRD_unify_monitor_server_tabs.md docs/superpowers/`.
- If deleting: `rm 01-PRD_unify_monitor_server_tabs.md`.

### Touchpoints
- `01-PRD_unify_monitor_server_tabs.md` — moved or deleted.
- `docs/superpowers/` (if moving) — receives the file.

### Contracts
No code changes.

### Acceptance Criteria
- [ ] No file named `01-PRD_unify_monitor_server_tabs.md` exists at the repository root.
- [ ] If moved, the file exists at `docs/superpowers/01-PRD_unify_monitor_server_tabs.md` with byte-identical content.

### Dependencies
- None.

---

## CQ-007: Topic-Group Split of `processmgr/manager.go`

### Scope
**In scope**:
- Split `internal/service/processmgr/manager.go` (currently 665 LOC) into the following sibling files within the same package:
  - `launch.go` — `Launch`, `launchForeground`, `makeCommand`, `applyProfileEnv`, `portFromProfile`, `checkPortFree`.
  - `enrichment.go` — `WaitHealthy`, `waitEnrichment`, `extractExit`.
  - `logs.go` — `TailLogs`, `logPathForPID`.
- `manager.go` retains the struct definition, `New`, `Close`, `List`, `Kill`, `snapshotLocked`.

**Out of scope**:
- Changing any method signatures (exported or unexported).
- Changing struct fields or visibility.
- Modifying observable behavior.
- Refactoring any of `liveness.go`, `recover.go`, `watchdog.go`, `args.go`.

### Technical Approach
- Create the three new files, each with `package processmgr` and the minimum imports needed.
- Move each method listed above to its target file. Methods stay attached to `*fsManager`. If a helper (small unexported function) is used only by a single moved group, move it with that group; if used across groups, leave it in `manager.go`.
- Run `goimports` over the package.
- Verify the build with `go build ./internal/service/processmgr/...` and tests with `go test ./internal/service/processmgr/...`.

### Touchpoints
- `internal/service/processmgr/manager.go` — reduced.
- `internal/service/processmgr/launch.go` — new.
- `internal/service/processmgr/enrichment.go` — new.
- `internal/service/processmgr/logs.go` — new.

### Contracts
No exported or unexported API changes.

### Acceptance Criteria
- [ ] `wc -l internal/service/processmgr/manager.go` < 300.
- [ ] Each of `launch.go`, `enrichment.go`, `logs.go` is < 400 LOC.
- [ ] `go test ./internal/service/processmgr/...` passes with no edits to test files (other than imports if needed).
- [ ] `go vet ./...` clean.
- [ ] `make tests` passes.

### Dependencies
- None.

---

## CQ-004: Shared `BuildFlagSchema` Builder

### Scope
**In scope**:
- Create `internal/service/backendschema/schemabuilder.go` exposing:
  - A `FlagSpecRow` struct mirroring the fields each backend currently sets when constructing a `domain.FlagSpec` literal.
  - A `BuildFlagSchema(version string, rows []FlagSpecRow) domain.FlagSchema` function that converts the slice into the existing `domain.FlagSchema { Version, Flags: map[string]domain.FlagSpec }` shape.
- Refactor `internal/service/llamahelp/embedded.go`, `internal/service/vllmhelp/embedded.go`, and `internal/service/sglanghelp/sglanghelp.go` to:
  - Declare an unexported `[]backendschema.FlagSpecRow` slice with the schema data.
  - Implement `EmbeddedSchema()` as `return backendschema.BuildFlagSchema(version, rows)`.
- Keep all schema data in Go source (compile-time-checked).

**Out of scope**:
- Embedding JSON / TOML files as the data source.
- Generating schemas from `--help` output at build time.
- Changing the public `domain.FlagSchema` / `domain.FlagSpec` types.
- Changing the pinned schema version string.

### Technical Approach
- Inspect the existing `domain.FlagSpec` definition and the literal shapes used in the three packages. Build a `FlagSpecRow` that includes every field currently set across them (`Long`, `Short`, `Type`, `EnumValues`, `Default`, `HelpText`, plus any others the implementer finds). Mirror types exactly — `Type domain.FlagType`, `Default any`, etc.
- Implementation of `BuildFlagSchema`:
  ```go
  func BuildFlagSchema(version string, rows []FlagSpecRow) domain.FlagSchema {
      flags := make(map[string]domain.FlagSpec, len(rows))
      for _, r := range rows {
          flags[r.Long] = domain.FlagSpec{
              Long: r.Long, Short: r.Short, Type: r.Type,
              EnumValues: r.EnumValues, Default: r.Default, HelpText: r.HelpText,
              // ...mirror remaining fields exactly
          }
      }
      return domain.FlagSchema{Version: version, Flags: flags}
  }
  ```
- In each `EmbeddedSchema()` file, replace the multi-hundred-line map literal with:
  ```go
  var llamaRows = []backendschema.FlagSpecRow{
      {Long: "...", Type: domain.FlagTypeString, HelpText: "..."},
      // ... one row per flag
  }

  func EmbeddedSchema() domain.FlagSchema {
      return backendschema.BuildFlagSchema("embedded-v7376", llamaRows)
  }
  ```
- Preserve the existing version strings (`embedded-v7376` for llama, plus the equivalents for vLLM and sglang).
- Run the existing schema tests — golden and unit — without modification. They should pass byte-for-byte because the resulting `domain.FlagSchema` is functionally identical.
- If the golden test for any backend's schema relies on stable map iteration order, ensure `BuildFlagSchema` produces the same map shape (Go maps are unordered, so any test comparing maps directly already handles this; tests that serialize maps usually sort keys).

### Touchpoints
- `internal/service/backendschema/schemabuilder.go` — new.
- `internal/service/llamahelp/embedded.go` — replace literal with slice + builder call.
- `internal/service/vllmhelp/embedded.go` — replace literal with slice + builder call.
- `internal/service/sglanghelp/sglanghelp.go` — replace literal with slice + builder call.
- `internal/service/backendschema/schemabuilder_test.go` — new minimal test asserting `BuildFlagSchema` produces the expected map shape from a 1-row input.

### Contracts
```go
package backendschema

type FlagSpecRow struct {
    Long       string
    Short      string
    Type       domain.FlagType
    EnumValues []string
    Default    any
    HelpText   string
    // ...mirror every field of domain.FlagSpec set by any of the three callers
}

func BuildFlagSchema(version string, rows []FlagSpecRow) domain.FlagSchema
```

### Acceptance Criteria
- [ ] `internal/service/backendschema/schemabuilder.go` exists and exports `FlagSpecRow` and `BuildFlagSchema`.
- [ ] Each of the three `EmbeddedSchema()` functions is implemented in < 30 LOC (excluding the row-slice declaration).
- [ ] The three row-slice declarations carry the same flag set, types, defaults, and help text as before.
- [ ] Schema-related golden tests for all three backends pass without `-update`.
- [ ] `make tests` passes.
- [ ] `go vet ./...` clean.
- [ ] No JSON or TOML files are introduced for schema storage.

### Dependencies
- None.

---

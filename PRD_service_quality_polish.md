# PRD — Service & Cross-Cutting Polish

**Source**: IDEATION_CODE_QUALITY.md
**Generated**: 2026-04-30

## Implementation Order

1. CQ-013 — Typed accessors on `domain.Profile`
2. CQ-005 — Reduce nesting in `processmgr/liveness.go`
3. CQ-009 — `main.go` `run() error` pattern + `bootstrap()` extraction
4. CQ-012 — Document Bubbletea receiver convention in `CLAUDE.md`

---

## CQ-013: Typed accessors on `domain.Profile`

### Scope
**In scope**:
- Add typed accessor methods on `domain.Profile`: `IntArg`, `StringArg`, `BoolArg`, `Port`.
- Replace three duplicated implementations:
  - `internal/service/validator/rules.go:124` (`intArg`) and `:144` (`stringArg`)
  - `internal/service/profilestore/fs_store.go:212` (`portAsInt`)
  - `internal/service/processmgr/manager.go` (`portFromProfile`)

**Out of scope**:
- Changing `Profile.Args` from `map[string]any` to a typed struct (the `any` map is required for TOML/JSON round-trip + dynamic schema).
- Removing `cloneArgs` in `profilestore/fs_store.go` (deep-copy serves a different purpose).

### Technical Approach
- Methods live on `domain.Profile` (`internal/domain/profile.go`). Domain types are allowed behavior under the project's layering convention.
- `Port()` is the existing `portFromProfile` semantics: returns `int, true` on `int`/`int64`/`float64` (TOML decode produces `int64`; JSON decode produces `float64`).
- All accessors are read-only and never mutate `Args`.

### Touchpoints
- `internal/domain/profile.go` — add four methods.
- `internal/domain/profile_test.go` — add `TestProfile_IntArg_*`, `TestProfile_StringArg_*`, `TestProfile_BoolArg_*`, `TestProfile_Port_*`.
- `internal/service/validator/rules.go` — replace `intArg`/`stringArg` calls with `p.IntArg`/`p.StringArg`; delete the local helpers.
- `internal/service/profilestore/fs_store.go` — delete `portAsInt`; call sites use `Profile.Port()`.
- `internal/service/processmgr/manager.go` — delete `portFromProfile`; call sites use `p.Port()`.

### Contracts
```go
// internal/domain/profile.go

// IntArg returns the integer value at key, accepting int / int64 / float64
// shapes that come out of TOML and JSON decoders.
func (p Profile) IntArg(key string) (int, bool)

// StringArg returns the string value at key.
func (p Profile) StringArg(key string) (string, bool)

// BoolArg returns the boolean value at key.
func (p Profile) BoolArg(key string) (bool, bool)

// Port returns the port number stored under args["port"].
func (p Profile) Port() (int, bool)
```

### Acceptance Criteria
- [ ] Four methods exist on `domain.Profile` with unit tests covering each numeric/string/bool source type.
- [ ] No unexported helper named `intArg`, `stringArg`, `portAsInt`, or `portFromProfile` remains in `internal/service/`.
- [ ] All callers compile and existing service-layer tests pass.
- [ ] `go vet ./...` clean.

### Dependencies
- None.

---

## CQ-005: Reduce nesting in `processmgr/liveness.go`

### Scope
**In scope**:
- Extract `markCrashedInstances(probe func(int) bool) (dirty bool, snapshot []domain.RunningInstance)` from `startLivenessWithProbe` (`internal/service/processmgr/liveness.go:31–60`).
- The outer ticker loop calls the helper and persists if dirty.

**Out of scope**:
- Changing the probe interface or the registry-save semantics.
- Touching `manager.go` `WaitHealthy` (separate scope; current nesting there is acceptable for HTTP-poll loops).

### Technical Approach
- The helper acquires `m.mu`, walks `m.tracked`, marks crashed entries, returns a `dirty` flag plus a snapshot for `saveRegistry`.
- Existing behavior preserved: only non-crashed PIDs are probed; on probe failure, `Crashed` becomes `true` and `ExitedAt` is set to `nowUTC`.

```go
case <-ticker.C:
    if dirty, snapshot := m.markCrashedInstances(probe); dirty {
        _ = saveRegistry(m.registryPath, snapshot)
    }
```

### Touchpoints
- `internal/service/processmgr/liveness.go` — add helper, simplify ticker case.
- `internal/service/processmgr/liveness_test.go` — add `TestMarkCrashedInstances_*` if not already covered indirectly.

### Contracts
```go
// internal/service/processmgr/liveness.go
func (m *fsManager) markCrashedInstances(probe func(int) bool) (dirty bool, snapshot []domain.RunningInstance)
```

### Acceptance Criteria
- [ ] No control-flow path in `liveness.go` exceeds depth 3.
- [ ] Existing `liveness_test.go` cases pass without modification.
- [ ] `go test ./internal/service/processmgr/... -count=1` passes.

### Dependencies
- None.

---

## CQ-009: `main.go` `run() error` pattern + `bootstrap()` extraction

### Scope
**In scope**:
- Refactor `cmd/model-loader/main.go`:
  - `main` becomes ≤10 lines: call `run()`, print error to stderr, exit 1 on failure.
  - `run() error` carries the current body, returning errors instead of `os.Exit`-ing.
  - Extract `bootstrap(cfg config.Config) (deps, error)` for the dependency-wiring section (store, schema, manager, validator, scanner).

**Out of scope**:
- Changing CLI flags or argument parsing.
- Changing the schema-fallback warning behavior or the missing-binary TUI flow.

### Technical Approach
- Define a small `deps` struct local to `main.go` to hold wired dependencies (`store`, `schema`, `schemaWarn`, `manager`, `validator`, `scanner`).
- Move the missing-`llama-server` interactive recovery block into `run()` (still calls `tea.NewProgram(...).Run()` for the warning TUI).
- Preserve `defer mgr.Close()` — must run on the normal exit path; `run()` orchestrates the close.

### Touchpoints
- `cmd/model-loader/main.go` — split into `main`, `run`, `bootstrap`.

### Contracts
```go
// cmd/model-loader/main.go

func main() {
    if err := run(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

func run() error

type deps struct {
    store      profilestore.Store
    schema     domain.FlagSchema
    schemaWarn string
    manager    *processmgr.Manager
    validator  validator.Validator
    scanner    components.ModelScanner
}

func bootstrap(cfg config.Config) (deps, error)
```

### Acceptance Criteria
- [ ] `main` function body is ≤10 lines.
- [ ] `bootstrap` is responsible for all dependency construction; `run` for orchestration and TUI startup.
- [ ] No `os.Exit` calls in `bootstrap` or `run` (all errors flow through return values; `os.Exit` only in `main`).
- [ ] Application startup behavior unchanged: same env/config resolution, same fallback messages, same final exit codes.
- [ ] Manual verification: `make build && ./bin/model-loader --help` (or default invocation) behaves identically to pre-refactor.

### Dependencies
- None.

---

## CQ-012: Document Bubbletea receiver convention in `CLAUDE.md`

### Scope
**In scope**:
- Add a "Bubbletea receiver convention" subsection to `CLAUDE.md`, sibling to the existing "TUI INPUT ROUTING RULES".

**Out of scope**:
- Any code change. The mixed receiver style is a correctness constraint, not a defect.

### Technical Approach
- Insert the subsection text below verbatim under the existing "TUI INPUT ROUTING RULES" section in `CLAUDE.md`.

```markdown
## BUBBLETEA RECEIVER CONVENTION
- Pages that host a `*huh.Form` with field bindings to struct fields (`huh.NewInput().Value(&p.draft.Field)`) MUST use **value receivers** and a **heap-allocated draft pointer**. Reason: bubbletea makes a value copy of the model on every `Update`; the form must bind to addresses that survive that copy. See `profiles.go:24–28` for the in-tree comment that explains this.
- Pages that do not host such a form MAY use pointer receivers (e.g., `*MonitorPage`).
- New sub-models extracted from existing pages (e.g., editor sub-models) MUST inherit the receiver style of the parent page they are extracted from.
```

### Touchpoints
- `CLAUDE.md` (project root).

### Contracts
- N/A (documentation).

### Acceptance Criteria
- [ ] `CLAUDE.md` contains a new `## BUBBLETEA RECEIVER CONVENTION` section after `## TUI INPUT ROUTING RULES`.
- [ ] No code changes accompany this change.

### Dependencies
- None. (Recommended to land before CQ-002 so the editor sub-model's receiver choice is grounded in documented convention.)

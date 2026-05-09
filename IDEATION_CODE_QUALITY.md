# Code Quality & Refactoring Plan

**Generated:** 2026-04-30
**Branch:** main
**Total Go files (non-test):** 50
**Total lines (Go, all):** ~12.5k
**`go vet` status:** clean
**`go test ./...` status:** all pass (14 packages green)

---

## Phase 1 — Foundation (mechanical, low-risk, unblocks Phase 2)

### CQ-008: `subState.Apply()` for monitor-event dispatch

**Category:** duplication
**Severity:** minor
**File:** `internal/ui/pages/monitor.go:217–261`

Five `case monitor.SourceX` branches in `MonitorPage.Update` repeat `if d, ok := m.ev.Data.(T); ok { st.field = d }`. Move type-assert + assign onto `subState`:

```go
func (s *subState) Apply(ev monitor.MonitorEvent, paused bool) {
    switch d := ev.Data.(type) {
    case monitor.LogLine:      if !paused { s.appendLog(d.Line) }
    case monitor.SlotSnapshot: s.slots = d
    case monitor.GPUStats:     s.gpu = d
    case monitor.HealthStatus: s.health = d
    case monitor.Metrics:      s.mets = d
    }
}
```

**Effort:** trivial. **Unblocks:** CQ-003.

---

### CQ-001: Confirm-dialog component

**Category:** duplication
**Severity:** major
**Best Practice:** DRY, Single Responsibility

**Affected Files:**
- `internal/ui/pages/profiles.go` — delete-confirm + discard-confirm (2 instances)
- `internal/ui/pages/launcher.go` — kill-confirm (1 instance)
- `internal/ui/pages/monitor.go` — kill-confirm + restart-confirm (2 instances)

**Why now:** the "forward non-`KeyMsg` msgs to huh" rule is documented as a project invariant in `CLAUDE.md` precisely because every duplicate is a place to forget it. Five copies = five risk sites.

**Component contract** in `internal/ui/components/confirm.go`:

```go
type Confirm struct {
    form    *huh.Form
    answer  *bool
    payload any
    onYes   func(any) tea.Cmd
}

func NewConfirm(title string, payload any, onYes func(any) tea.Cmd) Confirm
func (c Confirm) Active() bool
func (c Confirm) View() string
func (c Confirm) Init() tea.Cmd
func (c Confirm) Update(msg tea.Msg) (Confirm, tea.Cmd)  // forwards non-key + handles completion
```

Each page replaces 3 fields + 3 methods with one `Confirm` value; `IsCapturingInput()` becomes `c.Active()`.

**Effort:** medium. **Unblocks:** CQ-002, CQ-003.

---

### CQ-004: `Update` method decomposition across pages

**Category:** complexity (long method)
**Severity:** major
**Best Practice:** ≤50 lines per function

**Affected functions** (all currently 100+ lines, all in `internal/ui/pages/`):

| File | Line | Length | Function |
|---|---|---|---|
| `profiles.go` | 182 | 141 | `ProfilesPage.Update` |
| `launcher.go` | 138 | 141 | `LauncherPage.Update` |
| `monitor.go` | 153 | 139 | `MonitorPage.Update` |
| `components/picker.go` | 124 | 108 | `ModelPicker.Update` |

Apply the proven pattern already in `root.go:181–266`: each `case` in the type-switch becomes its own handler method. `Update` collapses to ~15 lines.

```go
func (p MonitorPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch m := msg.(type) {
    case tea.WindowSizeMsg:            return p.handleResize(m)
    case tea.KeyMsg:                    return p.handleKey(m)
    case monitorInstancesRefreshedMsg: return p.handleInstancesRefreshed(m)
    case monitorEventMsg:               return p.handleMonitorEvent(m)
    case restartResultMsg:              return p.handleRestartResult(m)
    case monitorPeriodicTickMsg:        return p.handlePeriodicTick()
    }
    return p.forwardToConfirms(msg)
}
```

**Effort:** medium (4 pages). **Unblocks (smaller diffs for):** CQ-002, CQ-003.

---

### CQ-010: `ModelsPage.handleKey` filter-mode extraction

**Category:** complexity
**Severity:** minor
**File:** `internal/ui/pages/models.go:400–490`

91-line method intermixes "filter typing" mode (printable runes append to `p.filter`) with command keys. Extract `handleFilterKey(msg) (handled bool, model, cmd)` so the command-key switch starts cleanly. Sub-case of CQ-004's pattern applied to a non-`Update` method.

**Effort:** trivial.

---

### CQ-007: Extract `View()` rendering helpers

**Category:** complexity
**Severity:** minor

**Affected Files:**
- `internal/ui/pages/launcher.go:389–455` (`View`, 66 lines)
- `internal/ui/pages/monitor.go:541–602` (`View`, 61 lines)

Each `View` inlines master pane + detail pane + running list + status line. Extract `renderProfileDetail()`, `renderRunningList()`, `renderStatusLine()` per page.

**Effort:** trivial.

---

## Phase 2 — Structural (depends on Phase 1)

### CQ-003: Decompose `MonitorPage.Update` and `applyInstances`

**Category:** complexity, code_smells (mixed concerns)
**Severity:** major

**Affected Files:**
- `internal/ui/pages/monitor.go:153` — `Update`, 139 lines
- `internal/ui/pages/monitor.go:455` — `applyInstances`, 84 lines

`applyInstances` does **three unrelated jobs**:
1. Build table rows (~30 lines).
2. Add monitor subscriptions for new instances (~12 lines).
3. Cancel subscriptions for crashed/orphaned instances (~25 lines, **two near-identical for-loops** at `monitor.go:521–537`).

The two cancel-loops differ only in predicate (`ri.Crashed` vs. `!seen[pid]`).

**Refactor steps:**
1. (CQ-008) move event apply onto `subState`.
2. Split `applyInstances` into:
   - `func (p *MonitorPage) renderRows(insts []domain.RunningInstance) []table.Row`
   - `func (p *MonitorPage) ensureSubscriptions(insts []domain.RunningInstance) []tea.Cmd`
   - `func (p *MonitorPage) reapDeadSubscriptions(insts []domain.RunningInstance)`
3. Collapse the two cancel-loops into one:

```go
byPID := make(map[int]domain.RunningInstance, len(insts))
for _, ri := range insts { byPID[ri.PID] = ri }
for pid, st := range p.subs {
    inst, present := byPID[pid]
    if present && !inst.Crashed { continue }
    cancel := st.cancel
    go func() { _ = cancel() }()
    delete(p.subs, pid); delete(p.chans, pid)
}
```

**Dependencies:** CQ-001, CQ-004, CQ-008.
**Effort:** medium.

---

### CQ-002: Extract `ProfileEditor` sub-model from `ProfilesPage`

**Category:** large_files, code_smells (god class)
**Severity:** major
**Best Practice:** Single Responsibility, Composition

**Affected Files:**
- `internal/ui/pages/profiles.go` (757 lines)
- `internal/ui/pages/profiles_editor.go` (265 lines)

`ProfilesPage` holds 22+ fields managing list, editor, advanced flags, two confirms, picker overlay, status flash. `IsCapturingInput()` ORs three boolean states; `Update` routes through a 4-priority dispatcher.

**Adjusted scope (narrowed from original 3-way split):**

Extract **only `ProfileEditor`** as a `tea.Model` in `internal/ui/pages/profile_editor/`. Editor owns:

| Concern | Fields moved out of `ProfilesPage` |
|---|---|
| Form + draft | `editing`, `subTab`, `form`, `draft`, `editorOpenSnapshot` |
| Advanced table | `advanced`, `advancedAll`, `advancedFilter`, `filterMode` |
| Discard confirm | `confirmDiscardForm`, `confirmDiscardAnswer` (becomes a `components.Confirm` from CQ-001) |

Stays in `ProfilesPage`: master list, delete-confirm, picker overlay, status flash.

```go
// After
func (p ProfilesPage) IsCapturingInput() bool {
    return p.editor.Active() || p.deleteConfirm.Active() || p.pickerActive
}
```

**Why narrow:** the editor + advanced + draft + discard-confirm cluster is the densest state knot. Splitting list/picker is also valid but should be a separate iteration once the editor extraction is proven.

**Dependencies:** CQ-001.
**Effort:** large.

---

## Phase 3 — Cross-cutting polish (independent, ship anytime)

### CQ-013: Typed accessors on `domain.Profile`

**Category:** types (primitive obsession via untyped map)
**Severity:** medium

`Profile.Args map[string]any` requires three duplicated helpers across packages:
- `validator/rules.go:124,144` — `intArg`, `stringArg`
- `profilestore/fs_store.go:212,222` — `portAsInt`, `cloneArgs`
- `processmgr/manager.go` — `portFromProfile`

Centralize on `domain.Profile`:

```go
func (p Profile) IntArg(key string) (int, bool)
func (p Profile) StringArg(key string) (string, bool)
func (p Profile) BoolArg(key string) (bool, bool)
func (p Profile) Port() (int, bool)
```

Replace three call sites. The `any`-valued map remains (TOML/JSON round-trip + dynamic schema requires it) — only the casting is centralized.

**Effort:** small.

---

### CQ-005: Reduce nesting in `processmgr/liveness.go`

**Category:** complexity (depth ≥5)
**Severity:** minor
**File:** `internal/service/processmgr/liveness.go:31–60`

`startLivenessWithProbe` nests `for ticker → select → for pid, inst := range → if inst.Crashed → if probe(pid)`. Extract:

```go
case <-ticker.C:
    dirty, snapshot := m.markCrashedInstances(probe)
    if dirty { _ = saveRegistry(m.registryPath, snapshot) }
```

**Effort:** small.

---

### CQ-009: `main.go` `run() error` pattern

**Category:** complexity, code_smells
**Severity:** minor
**File:** `cmd/model-loader/main.go:26–100`

127-line `main` doing 7 things (config, store, schema, mgr boot, signal handling, validator, page wiring). Standard Go split:

```go
func main() {
    if err := run(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
func run() error { /* current body, returning errors */ }
```

Plus extract `bootstrap(cfg) (deps, error)` for the dependency-wiring section.

**Effort:** small.

---

### CQ-012: Document Bubbletea receiver convention in `CLAUDE.md`

**Category:** consistency (documentation, no code change)
**Severity:** suggestion

**Adjusted scope (no code change):** the existing receiver-style mix (value for `ProfilesPage`/`LauncherPage`/`ModelsPage`, pointer for `MonitorPage`) is **not** stylistic — it's a bubbletea correctness constraint: pages with `huh.Form` field bindings (`Value(&p.draft.Field)`) require value receivers + heap-allocated drafts so bound addresses survive per-`Update` value copies.

Add a "Bubbletea receiver convention" subsection to `CLAUDE.md`, sibling to "TUI INPUT ROUTING RULES":

> Pages that host a `*huh.Form` with field bindings to struct fields MUST use value receivers and heap-allocated draft pointers (see `profiles.go:24–28`). Pages that don't host such forms MAY use pointer receivers.

**Effort:** trivial.

---

## Deferred

### CQ-006: Shared `Flash` helper across pages
**Trigger:** when a fourth page/component requires the flash-clear-timer pattern, OR when two existing pages converge on identical semantics.
**Why deferred:** each page has ~10 lines of flash handling with subtly distinct semantics (launcher: spinner + `waitingPID`; profiles: plain text + auto-clear; monitor: error-flash, no auto-clear). Generic abstraction either saves little or unifies behaviors that should stay distinct.

### CQ-011: `EmbeddedSchema()` → `//go:embed embedded_schema.json`
**Trigger:** next llama-server schema bump where the literal-form diff becomes painful, OR when a code-generator for the schema is built.
**Why deferred:** the 109-line literal at `internal/service/llamahelp/embedded.go:14–123` updates ~once per llama.cpp version. Switching to embedded JSON adds workflow surface (build embed, new file, doc rewrite in `CLAUDE.md`) for marginal current benefit. The better long-term answer is auto-generation, not JSON embedding.

---

## Dropped on Critical Review

| ID | Name | Reason |
|---|---|---|
| CQ-014 | Translate Portuguese comment in `fs_store.go:43` | One-line stylistic nit in a 12.5k-line codebase. Project owner is Brazilian; bilingual comments are not a defect. |

---

## Code Metrics (snapshot)

| Metric | Value | Status |
|---|---|---|
| Files > 500 lines (non-test) | 3 (`profiles.go`, `monitor.go`, `models.go`) | Phase 2 target |
| Functions > 50 lines (non-test) | 12 | Phase 1 target |
| Functions > 100 lines (non-test) | 5 | Phase 1 target |
| `any` types (non-test) | 17 occurrences across 8 files | Acceptable (boundary use) |
| Duplicated `confirm-form` blocks | 5 instances | CQ-001 |
| Deep-nest hot spots (depth ≥5) | 2 (`liveness.go`, `manager.go`) | CQ-005 |
| `TODO`/`FIXME` debt | 0 actionable | — |
| `go vet` warnings | 0 | — |
| Test pass rate | 14/14 packages | — |

# PRD — UI Refactor

**Source**: IDEATION_CODE_QUALITY.md
**Generated**: 2026-05-19

## Implementation Order
1. CQ-006 — Split `TabBar` into helpers.
2. CQ-002 — Trim residual inline branches in `ModelsPage.Update`.
3. CQ-001 — Split `models.go` into siblings (same package).
4. CQ-004 — Split `profiles.go` into siblings (same package).
5. CQ-005 — Split `server.go` into siblings (same package).

---

## CQ-006: TabBar extract method

### Scope
**In scope**:
- Split `TabBar(opts TabBarOptions) string` (`internal/ui/components/tab_bar.go`) into four named helpers.
- Preserve rendering output byte-for-byte for every `(width, activeTab, tabCount)` triple covered by existing tests.

**Out of scope**:
- Changing `TabBarOptions`.
- Changing default colors, padding, or active-tab styling.
- Touching call sites in `internal/ui/root.go`.

### Technical Approach
Walk the current 137-line body and partition it into four phases: option defaulting, per-tab label rendering, full-strip fitting test, and truncation/indicator rendering. `TabBar` itself becomes a 6-8 line orchestrator.

### Touchpoints
- `internal/ui/components/tab_bar.go` — top-level function shrinks; four new package-private helpers added.
- `internal/ui/components/tab_bar_test.go` (if present) — no edits expected.

### Contracts
```go
// TabBar renders the persistent top tab strip.
func TabBar(opts TabBarOptions) string {
    opts = applyTabBarDefaults(opts)
    labels := renderTabLabels(opts)
    strip := strings.Join(labels, separator)
    if fitsFully(strip, opts.Width) {
        return strip
    }
    return renderTruncated(opts, labels)
}

// applyTabBarDefaults fills missing TabBarOptions fields with package
// defaults (separator, colors, padding) and returns the result.
func applyTabBarDefaults(opts TabBarOptions) TabBarOptions

// renderTabLabels produces the styled label for each tab in opts.Tabs in
// order. The active tab gets the active style; the rest get the inactive
// style.
func renderTabLabels(opts TabBarOptions) []string

// fitsFully reports whether the joined strip plus minimum margins fits
// within opts.Width.
func fitsFully(strip string, width int) bool

// renderTruncated produces a horizontally truncated strip centered on the
// active tab with left/right indicators when content is clipped.
func renderTruncated(opts TabBarOptions, labels []string) string
```

### Acceptance Criteria
- [ ] `TabBar` body is at most 10 lines.
- [ ] All four helpers exist as package-private functions.
- [ ] `go test ./internal/ui/components/...` passes.
- [ ] Manual smoke: `go run ./cmd/model-loader` and resize the terminal — tab bar renders identically to the pre-refactor build at narrow, medium, and wide widths.

### Dependencies
- None.

---

## CQ-002: Trim residual inline branches in `ModelsPage.Update`

### Scope
**In scope**:
- Extract two helpers from `(p ModelsPage) Update` (`internal/ui/pages/models.go:409`):
  - `handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd)` — owns the `WindowSizeMsg` body, including the optional sub-component size propagation (`hfSearch`, `hfFilePicker`, `downloads`).
  - `dispatchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd)` — owns the picker/confirm/action pre-checks currently inline in the `tea.KeyMsg` branch, falling through to `handleKey`.
- Update both `case` arms in `Update` to one-liner delegations.

**Out of scope**:
- Touching `ProfilesPage.Update`, `BackendsPage.Update`, `ServerPage.Update` (already thin).
- Building a `reflect.TypeOf` dispatch table.
- Changing the downloadEvents channel poll at the top of `Update`.

### Technical Approach
Both cases already exist; this is just a mechanical extraction with no behavior change. After the move, the top-level `Update` reads as a single switch where every arm is one statement.

### Touchpoints
- `internal/ui/pages/models.go` — two new methods; two case arms shrink.
- `internal/ui/pages/models_test.go` (if present) — no edits expected.

### Contracts
```go
func (p ModelsPage) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd)
func (p ModelsPage) dispatchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd)

func (p ModelsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // ... downloadEvents poll unchanged ...
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        return p.handleResize(msg)
    // ... existing arms unchanged ...
    case tea.KeyMsg:
        return p.dispatchKey(msg)
    default:
        return p.forwardNonKey(msg)
    }
}
```

### Acceptance Criteria
- [ ] No `case` arm in `ModelsPage.Update` exceeds one statement (besides the `default` and the `downloadEvents` poll prelude).
- [ ] `go test ./internal/ui/pages/...` passes.
- [ ] Smoke: open the Models tab in the TUI, trigger a window resize, open the HF picker, and trigger a delete confirm — all behave identically to pre-refactor.

### Dependencies
- None. (Independent of CQ-006; should land before CQ-001 because a thin `Update` makes the file split mechanical.)

---

## CQ-001: Split `models.go` into siblings (same package)

### Scope
**In scope**:
- Create sibling `.go` files in `internal/ui/pages/` distributing the contents of `models.go` by responsibility. `package pages` stays the same.
- `ModelsPage` struct definition stays in `models.go` (single home for the type).
- All imports stay valid; no cycle introduced.

**Out of scope**:
- Creating a `internal/ui/pages/models/` sub-package.
- Renaming any type, method, or function.
- Changing test files (`models_test.go` references remain valid).

### Technical Approach
Partition the 1235-line file along the lines below. Move complete functions; do not split a function across files. Imports get re-derived per file by `goimports`.

| File | Owns |
|------|------|
| `models.go` | `ModelsPage` struct, `NewModelsPage`, `With*` constructors, `Init`, `View`, `regularBodyView`, `Hints` |
| `models_update.go` | `Update`, `handleResize`, `dispatchKey`, `forwardNonKey` |
| `models_scan.go` | `beginRescan`, `startScanCmd`, `waitForScanEvent`, `handleScanEvent`, `isScanning`, `hasScannedRoot`, `refreshRows`, `visibleFiles` |
| `models_hf.go` | HF search/file picker handlers, `openHFFilePicker`, the HF-related message types |
| `models_downloads.go` | Download adapters, `handleDownloadEvent`, `startSelectedDownloads`, `downloadStatusLabel`, `downloadErrString` |
| `models_actions.go` | `openActionMenuForSelection`, `renderActionMenu`, `renderActionMenuContent`, `commitRootAction`, `updateActionMenu`, `menuWidth` |
| `models_picker.go` | `handleProfilePicked`, the profile picker overlay handlers |
| `models_adapters.go` | `hfSearcherAdapter`, `hfFileListerAdapter`, `downloadSnapshotAdapter` |
| `models_messages.go` | All `*Msg` type declarations and `modelsKeyMap`, `defaultModelsKeys`, `IsCapturingInput`, `handleFilterKey`, key-binding helpers |

### Touchpoints
- `internal/ui/pages/models.go` — shrinks to struct + constructors + lifecycle.
- New files `internal/ui/pages/models_*.go` per table above.

### Contracts
No new contracts. This is a pure code-motion change. The post-split `models.go` must end with no remaining functions beyond the ones listed for `models.go` in the table.

### Acceptance Criteria
- [ ] `internal/ui/pages/models.go` is at most 250 lines.
- [ ] Every other `models_*.go` sibling is at most 350 lines.
- [ ] `go build ./...` succeeds.
- [ ] `go test ./internal/ui/...` passes (no test file edits).
- [ ] `grep -l '^package pages$' internal/ui/pages/models*.go` lists every new sibling.

### Dependencies
- CQ-002 (Feature ID `CQ-002`) must land first — splitting becomes mechanical only after `Update` delegates fully.

---

## CQ-004: Split `profiles.go` into siblings (same package)

### Scope
**In scope**:
- Same approach as CQ-001 applied to `internal/ui/pages/profiles.go` (1221 lines, 69 functions).
- `ProfilesPage` struct stays in `profiles.go`.
- `package pages` unchanged.

**Out of scope**:
- Sub-package extraction.
- Renaming.
- Touching `profiles_test.go` (it currently lives next to `profiles.go` and stays put).

### Technical Approach
Partition by responsibility:

| File | Owns |
|------|------|
| `profiles.go` | `ProfilesPage` struct, `NewProfilesPage`, `With*` constructors, `Init`, `loadCmd`, `View`, `OverlayView`, `detailView`, `Hints`, `IsCapturingInput`, `renderRunningList`, `renderLaunchStatus` |
| `profiles_update.go` | `Update`, `handleResize`, `handleFlashClear`, `handleLoaded`, `handlePickerScan`, `handleKey`, `forwardNonKey`, `updateList`, `updateKillConfirm` |
| `profiles_crud.go` | `startNew`, `startEditSelected`, `duplicateSelected`, `askDeleteSelected`, `performDelete`, `togglePinSelected`, `handleEditorCommitted`, `resolveSchemaForBackendID`, `buildEditor`, `formatArgsBlock`, `formatArgValue` |
| `profiles_launch.go` | `handleLaunchProfile`, `launchProfileCmd`, `handleLaunched`, `handleHealthy`, `handleLaunchErr`, `handleSpinnerTick`, `handleKillConfirmed`, `askKillMostRecent`, `performKill`, `enrichWithExit`, `lastNonEmpty`, `modeS` |
| `profiles_importexport.go` | `exportProfiles`, `startImport`, `startImportWithPath`, `importBundleCmd`, `handleImportDone`, `startUndo`, `restorePreviousCmd`, `handleUndoDone` |
| `profiles_picker.go` | `handleUseInNewProfile`, `handleModelPicked`, `handleModelPickerCancelled`, `handleNavigateToSizing` |
| `profiles_messages.go` | All `*Msg` types, `profilesKeyMap`, `defaultProfilesKeys`, `launchTracker` type |

### Touchpoints
- `internal/ui/pages/profiles.go` — shrinks.
- New files `internal/ui/pages/profiles_*.go` per table.

### Contracts
No new contracts.

### Acceptance Criteria
- [ ] `internal/ui/pages/profiles.go` is at most 280 lines.
- [ ] Every other `profiles_*.go` sibling is at most 350 lines.
- [ ] `go build ./...` succeeds.
- [ ] `go test ./internal/ui/...` passes (in particular `profiles_test.go`).
- [ ] No new imports across pages other than those existing in `profiles.go` today.

### Dependencies
- None. (Independent of CQ-001.)

---

## CQ-005: Split `server.go` into siblings (same package)

### Scope
**In scope**:
- Same approach for `internal/ui/pages/server.go` (1032 lines, 51 functions, pointer receivers).
- Preserve pointer-receiver style on `*ServerPage`.
- `ServerPage` struct stays in `server.go`.

**Out of scope**:
- Renaming receivers.
- Touching `internal/service/monitor/`.

### Technical Approach
Partition by responsibility:

| File | Owns |
|------|------|
| `server.go` | `ServerPage` struct, `NewServerPage`, `SetBackendResolver`, `SetSize`, `Init`, `View`, `Hints`, `IsCapturingInput` |
| `server_update.go` | `Update`, `handleKey`, `forwardToConfirms`, top-level message dispatch |
| `server_monitor.go` | `listenCmd`, `subState.Apply`, `handleMonitorEvent`, `handleInstancesRefreshed`, `handlePeriodicTick`, `handleSelectPID` |
| `server_restart.go` | `handleRestartResult`, `handleKillConfirmed`, `handleRestartConfirmed`, restart/kill command builders |
| `server_subviews.go` | sub-view render helpers, proxy panel forwarders, format utilities |

### Touchpoints
- `internal/ui/pages/server.go` — shrinks.
- New files `internal/ui/pages/server_*.go` per table.

### Contracts
No new contracts.

### Acceptance Criteria
- [ ] `internal/ui/pages/server.go` is at most 250 lines.
- [ ] Every other `server_*.go` sibling is at most 320 lines.
- [ ] `go build ./...` succeeds.
- [ ] `go test ./internal/ui/...` passes.
- [ ] All `*ServerPage` methods keep their pointer receiver style.

### Dependencies
- None. (Independent of CQ-001 and CQ-004.)

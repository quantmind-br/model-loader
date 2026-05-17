# PRD — TUI Feedback States

**Source**: IDEATION_UI_UX.md
**Generated**: 2026-05-17

## Implementation Order
1. UIUX-005 — Standardize flash feedback on pages that still bypass it.
2. UIUX-003 — Add missing actionable empty states where gaps remain.
3. UIUX-009 — Add pending feedback and duplicate-submit guards for long operations.
4. UIUX-007 — Improve profile editor guidance, validation visibility, and dirty-state display.

---

## UIUX-005: Standardize Flash Feedback on Pages That Still Bypass It

### Scope
**In scope**:
- Use `components.Flash` for page-level transient success/error feedback.
- Migrate ad hoc page flash strings where compatible.
- Handle `components.FlashClearMsg` in every page that owns a flash.

**Out of scope**:
- Replacing status-bar messages.
- Creating a global notification queue.

### Technical Approach
- Update `internal/ui/pages/server.go` to replace `flash string` and `flashErr bool` with `components.Flash`.
- Initialize Server flash with a stable tag in `NewServerPage`.
- Render Server flash through `Flash.View()`.
- Handle `components.FlashClearMsg` in `ServerPage.Update`.
- Keep existing page-local `withFlash` helpers in Launcher, Models, Backends, and Profiles.

### Touchpoints
- `internal/ui/components/flash.go` — Existing flash contract remains unchanged.
- `internal/ui/pages/server.go` — Migrate raw flash state to `components.Flash`.
- `internal/ui/pages/server_test.go` — Add/extend flash render and auto-clear tests.

### Contracts
```go
type Flash struct { /* existing value type */ }
func NewFlash(tag string) Flash
func (f Flash) Set(message string) (Flash, tea.Cmd)
func (f Flash) Update(msg tea.Msg) (Flash, bool)
func (f Flash) View(parts ...string) string
```

### Acceptance Criteria
- [ ] Server start success renders through `components.Flash`.
- [ ] Server start/stop errors render through `components.Flash`.
- [ ] Server flash clears on matching `FlashClearMsg` and ignores stale/wrong-tag clears.
- [ ] Existing flash behavior on Launcher, Models, Backends, and Profiles remains unchanged.

### Dependencies
- None

---

## UIUX-003: Add Missing Actionable Empty States Only Where Gaps Remain

### Scope
**In scope**:
- Add or refine empty-state copy where pages render blank/minimal unexplained content.
- Include next useful action keys when available.
- Distinguish “no data” from “still loading/scanning” when state exists.

**Out of scope**:
- Replacing existing useful empty states.
- Adding new data sources solely to make empty states more detailed.

### Technical Approach
- Audit `View()` and sub-render functions for Launcher, Monitor, Models, Backends, Profiles, and Server.
- Add a small style/helper only if repeated enough to avoid duplicated copy/style.
- Server empty state should describe proxy stopped/unconfigured state.
- Monitor empty state should distinguish no running instances from unavailable GPU metrics where current state allows.
- Backends list/detail should both show guidance when catalog is empty.

### Touchpoints
- `internal/ui/pages/server.go` — Stopped/unconfigured proxy empty state.
- `internal/ui/pages/monitor.go` — No running instances / no metrics empty state copy.
- `internal/ui/pages/backends.go` — Empty catalog copy in list/detail regions.
- `internal/ui/pages/models.go` — Preserve scan-aware empty-state behavior.
- `internal/ui/theme/theme.go` — Optional shared muted empty-state style.

### Contracts
```go
func EmptyState(message string, hint string) string // add only if repeated by 3+ call sites
```

### Acceptance Criteria
- [ ] Server stopped state explains the next start key.
- [ ] Monitor with no running instances renders an actionable message.
- [ ] Backends empty catalog renders an add-backend hint in visible page content.
- [ ] Models does not show “no models” while a scan is still in progress.

### Dependencies
- UIUX-005

---

## UIUX-009: Add Pending Feedback and Double-Submit Guards for Long Operations

### Scope
**In scope**:
- Track pending state for operations that spawn processes, scan files, refresh schemas, probe backends, or call external services.
- Show pending text/spinner/status while operation is active.
- Ignore duplicate trigger keys while the same operation is pending.
- End operations with flash success/error.

**Out of scope**:
- Making every synchronous filesystem write asynchronous.
- Adding cancellation to operations that do not currently expose cancellation.

### Technical Approach
- Add page-local pending fields for long operations, not a global pending registry.
- Use existing Launcher spinner for launch waits.
- Use Models scan status for scan/rescan and add duplicate-rescan guards where needed.
- Add pending state around Backends schema refresh/probe actions.
- Add pending state around Server start/stop commands.
- Clear pending state in each result message handler.

### Touchpoints
- `internal/ui/pages/launcher.go` — Preserve spinner and guard duplicate launch/kill triggers.
- `internal/ui/pages/models.go` — Guard duplicate scans and preserve per-root status.
- `internal/ui/pages/backends.go` — Track schema refresh/probe pending state.
- `internal/ui/pages/server.go` — Track start/stop pending state and flash completion.
- `internal/ui/components/statusbar.go` — Use existing status message only if page-local render is insufficient.

### Contracts
```go
type pendingAction string

const (
	pendingNone pendingAction = ""
	pendingStart pendingAction = "start"
	pendingStop  pendingAction = "stop"
)
```

### Acceptance Criteria
- [ ] Pressing Server start twice while start is pending launches one start command.
- [ ] Pressing Backends refresh twice while refresh is pending launches one refresh command.
- [ ] Pending state is visible before operation completion.
- [ ] Success and error completion both clear pending state and show flash feedback.

### Dependencies
- UIUX-005

---

## UIUX-007: Improve Profile Editor Field Guidance, Validation, and Dirty-State Visibility

### Scope
**In scope**:
- Show visible dirty-state indication while the editor has unsaved changes.
- Improve descriptions/help for required and high-impact fields.
- Group or label backend-specific advanced flags so users can scan them faster.
- Preserve discard confirmation and input routing behavior.

**Out of scope**:
- Replacing huh forms.
- Rebuilding the profile schema system.
- Adding a full external documentation browser for llama flags.

### Technical Approach
- Use `profile_editor.Editor` as the only owner of editor state.
- Add a `Dirty() bool` method if the dirty comparison is not already externally available.
- Render `Unsaved changes` in editor title/footer when dirty.
- Add descriptions to huh fields built in `add_profile.go` and `update_profile.go` where schema metadata or known domain fields support it.
- Group advanced table rows by schema/category when current schema data permits; otherwise add filter/help copy without inventing categories.
- Keep non-key message forwarding to huh forms and confirms.

### Touchpoints
- `internal/ui/pages/profile_editor/editor.go` — Dirty indicator rendering and editor footer/title copy.
- `internal/ui/pages/profile_editor/add_profile.go` — Field descriptions and validation visibility for new profiles.
- `internal/ui/pages/profile_editor/update_profile.go` — Field descriptions and validation visibility for edits.
- `internal/ui/pages/profiles.go` — Preserve parent routing and flash behavior for committed editor messages.
- `internal/ui/pages/profiles_test.go` — Cover dirty indicator and discard confirmation behavior.

### Contracts
```go
func (e Editor) Dirty() bool
func (e Editor) View() string
```

### Acceptance Criteria
- [ ] Editing a field makes an “Unsaved changes” indicator visible.
- [ ] Returning a field to its original value removes the dirty indicator.
- [ ] Escape on a dirty editor still opens discard confirmation.
- [ ] Required profile fields show validation feedback through huh.
- [ ] Advanced flag UI includes visible guidance for filtering/editing values.

### Dependencies
- UIUX-005

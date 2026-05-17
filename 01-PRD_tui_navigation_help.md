# PRD — TUI Navigation Help

**Source**: IDEATION_UI_UX.md
**Generated**: 2026-05-17

## Implementation Order
1. UIUX-001 — Normalize all 6-tab navigation references and tests.
2. UIUX-008 — Generate contextual help modal content from global bindings plus active page state.
3. UIUX-012 — Fill shortcut documentation gaps and add only missing high-value shortcuts.

---

## UIUX-001: Normalize 6-Tab Navigation Documentation and Help

### Scope
**In scope**:
- Keep Launcher, Profiles, Monitor, Models, Backends, and Server as six top-level tabs.
- Update stale 5-tab references in maintainer docs and help content.
- Verify direct tab keys `1` through `6` work and appear in help.

**Out of scope**:
- Merging tabs or redesigning top-level navigation.
- Changing tab order.

### Technical Approach
- Update `AGENTS.md` references from `5 tabs` to `6 tabs`.
- Update `internal/ui/components/help.go` global keybinding line from `1–5` to `1–6`.
- Add Server tab keybindings to `components.HelpMarkdown`.
- Keep `internal/ui/root.go` `tabCount = 6` as the source of truth.
- Extend root/help tests to check every direct tab key and the help text.

### Touchpoints
- `AGENTS.md` — Correct project knowledge base tab count and TUI structure notes.
- `internal/ui/components/help.go` — Correct global help and include Server tab bindings.
- `internal/ui/components/help_test.go` — Assert help mentions `1–6` and all six tabs.
- `internal/ui/root_test.go` — Assert direct numeric navigation reaches every tab.

### Contracts
```go
const tabCount = 6
const globalHints = "[1-6] tabs  [tab] next  [q] quit" + components.HelpToken
```

### Acceptance Criteria
- [ ] Help modal global section shows `1–6`, not `1–5`.
- [ ] Help modal includes Launcher, Profiles, Monitor, Models, Backends, and Server sections.
- [ ] `AGENTS.md` describes a 6-tab model.
- [ ] Tests prove keys `1`, `2`, `3`, `4`, `5`, and `6` activate the expected tabs.

### Dependencies
- None

---

## UIUX-008: Generate Contextual Help From Existing Hint Providers

### Scope
**In scope**:
- Keep compact active-page hints in the status bar.
- Make the `?` help modal include global bindings plus context for the active page or active overlay.
- Reuse existing page hint data where possible.

**Out of scope**:
- Replacing all page keymaps.
- Rendering a full interactive help browser.

### Technical Approach
- Keep `ui.HintProvider` for status-bar hints.
- Add an optional richer interface only if string hints cannot support the modal body.
- Add a helper in `internal/ui/components/help.go` that renders help from global help text and active context text.
- In `RootModel.View`, when `helpOpen` is true, pass active-page context to the help renderer.
- Prefer overlay/editor/picker hints over normal page hints when the active page reports them.

### Touchpoints
- `internal/ui/root.go` — Pass active page context to help rendering while preserving `helpOpen` behavior.
- `internal/ui/components/help.go` — Add contextual help rendering helper.
- `internal/ui/pages/*.go` — Ensure `Hints()` strings are accurate for normal and overlay states.
- `internal/ui/root_test.go` — Assert help modal shows active page context and does not show stale context after tab switch.

### Contracts
```go
type HintProvider interface {
	Hints() string
}

type HelpContextProvider interface {
	HelpContext() string
}

func RenderContextualHelp(width int, activeContext string) (string, error)
```

### Acceptance Criteria
- [ ] Opening `?` on Models includes Models-specific actions.
- [ ] Opening `?` while a picker/action menu is active shows picker/action-menu controls.
- [ ] Switching tabs changes the contextual help content.
- [ ] Existing status-bar hints remain visible when help modal is closed.

### Dependencies
- UIUX-001

---

## UIUX-012: Fill Shortcut Gaps and Sync Shortcut Documentation

### Scope
**In scope**:
- Audit current page keymaps against help/status hints.
- Add missing shortcuts only for high-frequency existing page actions.
- Add tests for new printable shortcuts and input-capture behavior.

**Out of scope**:
- Adding shortcuts for actions that do not exist yet.
- Changing established destructive/heavy uppercase conventions without a full keymap migration.

### Technical Approach
- Review page keymaps in `internal/ui/pages/profiles.go`, `launcher.go`, `monitor.go`, `models.go`, `backends.go`, and `server.go`.
- Add a `Hints()` implementation to Server if missing.
- Add any missing key bindings through page-local keymaps, not root globals, unless the action is truly global.
- For any new root-level printable shortcut, add an input-capture regression test using `capturingPage` in `internal/ui/root_test.go`.
- Update `components.HelpMarkdown` or contextual help source in the same change.

### Touchpoints
- `internal/ui/pages/server.go` — Publish Server hints for `s`, `x`, and `r`.
- `internal/ui/pages/*_test.go` — Cover any new page-local shortcuts.
- `internal/ui/root_test.go` — Cover any new root-level printable shortcuts under input capture.
- `internal/ui/components/help.go` — Keep help copy synchronized.

### Contracts
```go
func (p *ServerPage) Hints() string
```

### Acceptance Criteria
- [ ] Every shortcut shown in status/help triggers the documented action.
- [ ] Every existing high-frequency page action has either a documented shortcut or an explicit reason to remain menu-only.
- [ ] Printable shortcuts do not fire while the active page captures input.
- [ ] Help modal and status bar do not disagree about page actions.

### Dependencies
- UIUX-001
- UIUX-008

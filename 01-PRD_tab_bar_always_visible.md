# PRD — Tab Bar Always Visible

**Source**: prompt-tab-bar-always-visible.md
**Generated**: 2026-05-18

## Implementation Order
1. F1 — Clamp body height in `RootModel.View()` so no page can overflow the tab bar
2. F2 — Migrate page-level full-screen modals to the existing `Overlay()` pattern
3. F3 — Solid-background highlight for the active tab
4. F4 — Horizontal-scrolling tab bar with `‹`/`›` overflow indicators

---

## F1: Body Height Clamping in RootModel

### Scope
**In scope**:
- Clamp the body region of `RootModel.View()` to `theme.BodyHeight(m.height)` rows and `m.width` columns before composing the final frame.
- Ensure tab bar (1 row) + body (clamped) + status bar (1 row) always equals `m.height` and stays within `m.width`.
- Apply clamp via `lipgloss.NewStyle().Width(...).Height(...).MaxHeight(...).Render(...)`.

**Out of scope**:
- Changing how individual pages compute their internal layout.
- Touching the status bar or its render logic.
- Modifying `theme.BodyHeight` arithmetic.

### Technical Approach
1. In `internal/ui/root.go:View()` (around lines 376–401), introduce a `bodyHeight := theme.BodyHeight(m.height)` and `bodyWidth := m.width`.
2. Wrap `m.pages[m.active].View()` output with a `lipgloss.Style` that enforces both `Width(bodyWidth)` and `Height(bodyHeight)` plus `MaxHeight(bodyHeight)` to truncate overflow.
3. Pass the clamped string into the existing `lipgloss.JoinVertical(lipgloss.Left, header, clampedBody, status)` call.
4. Keep the playground overlay composition (`Overlay(...)`) applied AFTER the `JoinVertical`, so the tab bar remains a structural layer below it.
5. Guard against `m.height < 3` (degenerate terminal): render only the tab bar and status bar; skip body entirely.

### Touchpoints
- `internal/ui/root.go` — `View()` method around lines 376–401: introduce body clamping.
- `internal/ui/root.go` — `handleResize()` (lines 226–243): no change required; existing `BodyHeight()` propagation stays.
- `internal/ui/theme/layout.go` — confirm `BodyHeight` returns `max(0, totalHeight - 2)` and add a helper `func ClampBody(s string, width, height int) string` to centralize the style application (avoid scattering `lipgloss.NewStyle()` calls).

### Contracts
```go
// internal/ui/theme/layout.go
func ClampBody(s string, width, height int) string {
    if width <= 0 || height <= 0 {
        return ""
    }
    return lipgloss.NewStyle().
        Width(width).
        Height(height).
        MaxWidth(width).
        MaxHeight(height).
        Render(s)
}
```

```go
// internal/ui/root.go — View()
header := m.renderTabs()
status := m.status.Render()
bodyHeight := theme.BodyHeight(m.height)
bodyWidth := m.width
rawBody := m.pages[m.active].View()
clampedBody := theme.ClampBody(rawBody, bodyWidth, bodyHeight)
frame := lipgloss.JoinVertical(lipgloss.Left, header, clampedBody, status)
// existing playground Overlay() application stays after this point
```

### Acceptance Criteria
- [ ] `RootModel.View()` always emits exactly `m.height` rows (assuming `m.height >= 3`), starting with the tab bar row.
- [ ] A page returning a `View()` longer than `theme.BodyHeight(m.height)` rows is truncated, never expanding past its allotted region.
- [ ] When `m.height < 3`, the frame still renders the tab bar (and status bar if `m.height >= 2`) without panicking.
- [ ] Existing `playgroundModal` overlay composition continues to function unchanged.
- [ ] All existing tests in `internal/ui/root_test.go` and `internal/ui/pages/*_test.go` pass.

### Dependencies
- None.

---

## F2: Overlay-Pattern Migration for Page Modals

### Scope
**In scope**:
- Replace early-return full-screen modal renders in `internal/ui/pages/profiles.go:354–372` (importPicker, picker, conflictModal, undoModal, editor, deleteConfirm) with the `Overlay()` pattern.
- Replace early-return confirms in `internal/ui/pages/server.go:722–727` (kill confirm, restart confirm) with the `Overlay()` pattern.
- Introduce a new optional interface `Overlayer` that pages implement to expose an active overlay.
- Update `RootModel.View()` to detect `Overlayer` on the active page and apply `components.Overlay()` after `JoinVertical` (same layer as the playground modal).
- Preserve all existing input-routing rules from `CLAUDE.md` — `IsCapturingInput()` must still return `true` while any modal is active, and non-`tea.KeyMsg` messages must continue forwarding to `huh.Form` inside the profile editor.

**Out of scope**:
- Changing modal logic, state machines, or business rules.
- Renaming any state field on the page structs.
- Designing a new overlay system; reuse `internal/ui/components/overlay.go`.

### Technical Approach
1. Define `Overlayer` interface in `internal/ui/root.go` (or a new `internal/ui/contracts.go`):
   ```go
   type Overlayer interface {
       OverlayView() (content string, width, height int, active bool)
   }
   ```
2. For each affected page, split its `View()` into:
   - **Base view**: the normal page content WITHOUT the modal (always rendered).
   - **`OverlayView()`**: returns the modal's rendered content, its desired width/height, and `active=true` only when a modal is open.
3. Profiles page (`internal/ui/pages/profiles.go`):
   - Remove the early-return chain on lines 354–372.
   - `View()` always returns the base content (the list / detail view).
   - `OverlayView()` checks `p.importPicker.Active`, `p.pickerActive`, `p.conflictModal.Active`, `p.undoModal.Active`, `p.editing`, `p.confirmDelete` in priority order and returns the corresponding modal's view.
4. Server page (`internal/ui/pages/server.go`):
   - Same pattern; `OverlayView()` returns the kill/restart confirm when set.
5. `RootModel.View()`:
   ```go
   if ov, ok := m.pages[m.active].(Overlayer); ok {
       if content, w, h, active := ov.OverlayView(); active {
           frame = components.Overlay(frame, content, w, h)
       }
   }
   ```
6. Profile editor (`internal/ui/pages/profile_editor/`):
   - The editor's `View()` returns a self-contained block sized to fit within `theme.BodyHeight(rootHeight)`.
   - Continue forwarding all non-`tea.KeyMsg` messages to the embedded `huh.Form` so its Init/validation Cmd cycle works (per CLAUDE.md rule).
   - The editor must size itself to `bodyWidth × bodyHeight` (passed via existing handleResize signal), so the Overlay() centers it correctly within the body region.
7. `IsCapturingInput()` on `ProfilesPage` and `ServerPage` continues to return `true` whenever any of these modal flags are set (existing pattern preserved verbatim).

### Touchpoints
- `internal/ui/root.go` — add `Overlayer` interface check and overlay composition after `JoinVertical`.
- `internal/ui/pages/profiles.go` — remove modal early-returns from `View()`, add `OverlayView()`.
- `internal/ui/pages/profiles_test.go` — update tests that assert modal-active `View()` content; assert `OverlayView()` instead.
- `internal/ui/pages/server.go` — remove confirm early-returns from `View()`, add `OverlayView()`.
- `internal/ui/pages/profile_editor/editor.go` — accept `width`/`height` via existing resize path; ensure `View()` fits.
- `internal/ui/components/overlay.go` — no change expected; validate it handles dimensions correctly when the underlying frame has clamped body.

### Contracts
```go
// internal/ui/root.go (or internal/ui/contracts.go)
type Overlayer interface {
    // OverlayView returns the modal content and its desired dimensions.
    // active=false means no overlay should be drawn this frame.
    OverlayView() (content string, width, height int, active bool)
}
```

```go
// internal/ui/pages/profiles.go
func (p *ProfilesPage) View() string {
    // base content only; never short-circuits to a modal
    return p.renderBase()
}

func (p *ProfilesPage) OverlayView() (string, int, int, bool) {
    switch {
    case p.importPicker.Active:
        return p.importPicker.View(), p.modalWidth(), p.modalHeight(), true
    case p.pickerActive:
        return p.picker.View(), p.modalWidth(), p.modalHeight(), true
    case p.conflictModal.Active:
        return p.conflictModal.View(), p.modalWidth(), p.modalHeight(), true
    case p.undoModal.Active:
        return p.undoModal.View(), p.modalWidth(), p.modalHeight(), true
    case p.editing:
        return p.editor.View(), p.editorWidth(), p.editorHeight(), true
    case p.confirmDelete:
        return p.deleteConfirm.View(), p.modalWidth(), p.modalHeight(), true
    }
    return "", 0, 0, false
}
```

```go
// internal/ui/pages/server.go
func (p *ServerPage) View() string {
    return p.renderBase()
}

func (p *ServerPage) OverlayView() (string, int, int, bool) {
    if p.killConfirm.Active {
        return p.killConfirm.View(), p.confirmWidth(), p.confirmHeight(), true
    }
    if p.restartConfirm.Active {
        return p.restartConfirm.View(), p.confirmWidth(), p.confirmHeight(), true
    }
    return "", 0, 0, false
}
```

### Acceptance Criteria
- [ ] Opening the profile editor in Profiles tab keeps the tab bar visible at the top of the screen.
- [ ] Opening the model picker, conflict modal, undo modal, delete confirm, or import picker in Profiles keeps the tab bar visible.
- [ ] Opening kill or restart confirms in Server keeps the tab bar visible.
- [ ] `ProfilesPage.IsCapturingInput()` still returns `true` whenever any modal flag is set (regression test preserved).
- [ ] `ServerPage.IsCapturingInput()` still returns `true` for kill/restart confirms.
- [ ] Non-`tea.KeyMsg` messages still reach the embedded `huh.Form` inside the profile editor (validation, async init, focus).
- [ ] Existing `internal/ui/root_test.go` test using the `capturingPage` double for global shortcuts still passes.
- [ ] No page renders the tab bar inside its own `View()` output (no duplication).

### Dependencies
- F1 (body clamping must be in place so `Overlay()` sees a correctly-sized frame).

---

## F3: Active Tab Solid-Background Highlight

### Scope
**In scope**:
- Modify `renderTabs()` in `internal/ui/root.go:445–459` so the active tab uses a solid background color from the theme with contrasting foreground text.
- Add `theme.AccentBg` and `theme.AccentFg` constants (or equivalents) to `internal/ui/theme/`.
- Inactive tabs keep their current styling.

**Out of scope**:
- Changing tab order, labels, or shortcuts.
- Adding tab icons or separators.
- Restyling any other UI element.

### Technical Approach
1. In `internal/ui/theme/`, add or surface two color constants for the active-tab highlight (`AccentBg`, `AccentFg`). If a theme accent already exists, reuse it; otherwise pick a perceptually-strong color with verified contrast.
2. Build two `lipgloss.Style` values: `activeTabStyle` (with `Background(AccentBg).Foreground(AccentFg).Bold(true).Padding(0, 1)`) and `inactiveTabStyle` (existing styling).
3. In `renderTabs()`, select style per tab based on `i == int(m.active)`.
4. Ensure padding is symmetric so width calculations for F4 remain predictable.

### Touchpoints
- `internal/ui/theme/colors.go` (or equivalent existing file) — add accent constants.
- `internal/ui/root.go` — `renderTabs()`: branch styling on active index.

### Contracts
```go
// internal/ui/theme/colors.go
var (
    AccentBg = lipgloss.Color("...") // strong, high-contrast
    AccentFg = lipgloss.Color("...") // contrasting text color
)

// internal/ui/root.go
var (
    activeTabStyle = lipgloss.NewStyle().
        Background(theme.AccentBg).
        Foreground(theme.AccentFg).
        Bold(true).
        Padding(0, 1)
    inactiveTabStyle = lipgloss.NewStyle().
        Padding(0, 1)
)
```

### Acceptance Criteria
- [ ] The active tab renders with a solid background fill (not just a different text color).
- [ ] Foreground text on the active tab is readable against the background (verified visually and via contrast ratio if a theme test exists).
- [ ] Inactive tabs render unchanged from current visuals.
- [ ] All 5 tabs are individually highlighted as expected when activated (Models, Profiles, Backends, Launcher, Server).
- [ ] Width per tab is computed deterministically and consistent with F4's scrolling assumptions.

### Dependencies
- None (can land independently of F1/F2, but ordered after F1/F2 for review clarity).

---

## F4: Horizontal-Scrolling Tab Bar With Overflow Indicators

### Scope
**In scope**:
- Detect when total rendered tab-bar width exceeds `m.width` and switch the tab bar into scroll mode.
- In scroll mode: render a horizontal viewport over the tab labels that keeps the active tab visible.
- Render `‹` (or equivalent) at the leftmost column when one or more tabs are clipped on the left.
- Render `›` at the rightmost column when one or more tabs are clipped on the right.
- Labels remain unchanged (no truncation, no abbreviation, no wrapping).

**Out of scope**:
- Implementing mouse interactions on the indicators.
- Adding keyboard shortcuts to scroll the bar manually (active-tab navigation already exists via `1-5`, `tab`, `shift+tab`).
- Animated transitions.

### Technical Approach
1. Extract tab-bar rendering into a dedicated component: `internal/ui/components/tab_bar.go`. The existing `renderTabs()` in root.go becomes a thin caller.
2. The component takes: `labels []string`, `activeIndex int`, `availableWidth int`, `activeStyle, inactiveStyle lipgloss.Style`.
3. Algorithm:
   - Pre-render each tab in its style and record its width (including padding).
   - Compute total width `T`. If `T <= availableWidth`, render normally (no indicators, no scroll).
   - If `T > availableWidth`, compute the viewport offset:
     - Reserve 1 column on each side for potential indicators (so effective width is `availableWidth - 2`).
     - Find an offset such that the entire active tab is visible inside the viewport with maximum surrounding context (center-when-possible).
     - Determine whether any tab is clipped on the left (`offset > 0`) and/or on the right (`offset + visibleWidth < T`).
     - Render `‹` if clipped left, blank otherwise; render `›` if clipped right, blank otherwise.
   - Slice the rendered tab strip at the byte offsets corresponding to the column offset (using `ansi` package or `lipgloss`'s width-aware truncation).
4. Active tab MUST be fully visible — if it would be partially clipped, shift the offset to fully include it.
5. Add unit tests in `internal/ui/components/tab_bar_test.go` covering: all-fit, overflow-active-leftmost, overflow-active-rightmost, overflow-active-middle, single-tab-wider-than-viewport (graceful degradation).

### Touchpoints
- `internal/ui/components/tab_bar.go` — new file with the component.
- `internal/ui/components/tab_bar_test.go` — new tests.
- `internal/ui/root.go` — `renderTabs()`: delegate to `components.TabBar(...)`, pass `m.width`.

### Contracts
```go
// internal/ui/components/tab_bar.go
package components

import "github.com/charmbracelet/lipgloss"

type TabBarOptions struct {
    Labels         []string
    ActiveIndex    int
    AvailableWidth int
    ActiveStyle    lipgloss.Style
    InactiveStyle  lipgloss.Style
    LeftIndicator  string // default "‹"
    RightIndicator string // default "›"
}

// TabBar renders a horizontally-scrollable tab strip. The active tab is always
// fully visible. When labels overflow AvailableWidth, side indicators show
// which directions have clipped tabs.
func TabBar(opts TabBarOptions) string
```

### Acceptance Criteria
- [ ] When all tab labels fit within the available width, the tab bar renders with no indicators and no scroll.
- [ ] When labels overflow, the active tab is always fully visible (never partially clipped).
- [ ] `‹` appears in the leftmost column when at least one tab is clipped on the left; otherwise that column is blank.
- [ ] `›` appears in the rightmost column when at least one tab is clipped on the right; otherwise that column is blank.
- [ ] No label is ever truncated, abbreviated, or wrapped to a second line.
- [ ] The tab bar always renders on exactly one terminal row.
- [ ] Switching tabs (via `1-5`, `tab`, `shift+tab`) in narrow terminals scrolls the viewport so the newly-active tab stays fully visible.
- [ ] Unit tests in `tab_bar_test.go` cover: all-fit, active-leftmost-with-right-overflow, active-rightmost-with-left-overflow, active-middle-both-overflow.

### Dependencies
- F3 (active/inactive styles must be defined and stable so the component can size them correctly).

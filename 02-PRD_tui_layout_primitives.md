# PRD — TUI Layout Primitives

**Source**: IDEATION_UI_UX.md
**Generated**: 2026-05-17

## Implementation Order
1. UIUX-004 — Add shared layout constants and sizing helpers.
2. UIUX-002 — Apply shared pane styling to repeated page layouts.
3. UIUX-010 — Route remaining UI colors through theme styles.
4. UIUX-013 — Polish modal and confirm hierarchy using shared theme primitives.

---

## UIUX-004: Centralize TUI Layout Constants

### Scope
**In scope**:
- Add shared constants for repeated TUI chrome and pane spacing.
- Add helpers for body height and common horizontal split widths.
- Replace repeated layout magic numbers where they represent shared chrome.

**Out of scope**:
- Replacing content-specific column widths.
- Rewriting all page layout code in one sweep.

### Technical Approach
- Add `internal/ui/theme/layout.go`.
- Define constants for tab bar height, status bar height, pane padding, gutter, and minimum pane widths.
- Add helper functions for body height and two-pane splits.
- Update `RootModel.Update` and high-repeat page layouts to use the constants/helpers.

### Touchpoints
- `internal/ui/theme/layout.go` — New shared layout constants/helpers.
- `internal/ui/root.go` — Replace root body-height calculations with shared constants.
- `internal/ui/pages/launcher.go` — Use split helper for profile/detail panes.
- `internal/ui/pages/backends.go` — Use split helper for list/detail panes.
- `internal/ui/pages/models.go` — Replace shared chrome height assumptions where applicable.

### Contracts
```go
package theme

const (
	TabBarHeight    = 1
	StatusBarHeight = 1
	PageGutter      = 2
	PanePaddingX    = 1
	PanePaddingY    = 0
	MinPaneWidth    = 20
)

func BodyHeight(totalHeight int) int
func SplitTwoPanes(totalWidth int) (leftWidth int, rightWidth int)
```

### Acceptance Criteria
- [ ] Shared root chrome height is named in `theme/layout.go`.
- [ ] Launcher and Backends two-pane width calculations use a shared helper or constants.
- [ ] Existing page rendering remains usable at narrow terminal widths.
- [ ] Layout helper tests cover small, normal, and zero widths/heights.

### Dependencies
- None

---

## UIUX-002: Standardize Page Pane Styling Through Theme Primitives

### Scope
**In scope**:
- Reuse `theme.Pane` and layout constants for repeated pane/list/detail layouts.
- Align padding, border, and gutter choices across pages with similar structure.

**Out of scope**:
- Forcing every page into a full-page bordered frame.
- Removing intentional differences between list, table, dashboard, and modal surfaces.

### Technical Approach
- Update `theme.RebuildStyles()` to build pane styles from layout constants.
- Replace ad hoc pane border/padding definitions in pages with `theme.Pane` where the visual role matches.
- Keep content-specific styles local when they express different UI roles.

### Touchpoints
- `internal/ui/theme/theme.go` — Build `Pane` from shared layout constants.
- `internal/ui/pages/launcher.go` — Align list/detail pane rendering.
- `internal/ui/pages/backends.go` — Align master/detail pane rendering.
- `internal/ui/pages/monitor.go` — Align dashboard/table pane rendering where applicable.
- `internal/ui/pages/server.go` — Use shared pane/frame only if it improves clarity without wasting space.

### Contracts
```go
var Pane lipgloss.Style
```

### Acceptance Criteria
- [ ] Pages with the same two-pane structure use consistent border/padding/gutter.
- [ ] No page gains redundant nested borders around already framed components.
- [ ] `NO_COLOR` output remains readable.
- [ ] Snapshot or string-based render tests cover at least one shared pane page.

### Dependencies
- UIUX-004

---

## UIUX-010: Remove Remaining Hardcoded UI Colors

### Scope
**In scope**:
- Audit `internal/ui` for literal lipgloss colors and ANSI color assumptions.
- Replace remaining literals with theme palette colors or named styles.
- Preserve adaptive and `NO_COLOR` behavior.

**Out of scope**:
- Designing a new color palette.
- Adding user-configurable themes.

### Technical Approach
- Search `internal/ui` for `lipgloss.Color`, hex strings, ANSI escape assumptions, and direct foreground/background calls.
- Move repeated colors to `internal/ui/theme/theme.go`.
- Rebuild styles through `theme.RebuildStyles()` when colors affect exported style variables.
- Add or extend component tests where color suppression matters.

### Touchpoints
- `internal/ui/theme/theme.go` — Add palette entries or style variables only when reused.
- `internal/ui/components/modal_test.go` — Preserve no-background and NO_COLOR assertions.
- `internal/ui/components/statusbar.go` — Ensure status colors use theme styles.
- `internal/ui/pages/*.go` — Replace local hardcoded colors.

### Contracts
```go
var (
	ColorAccent lipgloss.AdaptiveColor
	ColorOK     lipgloss.AdaptiveColor
	ColorWarn   lipgloss.AdaptiveColor
	ColorError  lipgloss.AdaptiveColor
	ColorDim    lipgloss.AdaptiveColor
)
```

### Acceptance Criteria
- [ ] No non-test UI file contains unreviewed literal hex color strings.
- [ ] `NO_COLOR=1` removes foreground/background styling from affected components.
- [ ] Existing semantic status colors remain visually distinct when color is enabled.

### Dependencies
- UIUX-004

---

## UIUX-013: Polish Modal and Confirm Visual Hierarchy Within Terminal Constraints

### Scope
**In scope**:
- Improve modal frame, title spacing, body spacing, and small-terminal behavior.
- Improve confirm affordance through title/copy/style supported by huh/lipgloss.
- Keep modal output compatible with tests and NO_COLOR.

**Out of scope**:
- True opacity overlays.
- Graphical shadows that rely on terminal-specific background control.
- Replacing `huh.Confirm` with a bespoke confirm model unless huh cannot support required behavior.

### Technical Approach
- Refine `modalBoxStyle()` and `modalTitleStyle()` in `internal/ui/components/modal.go`.
- Clamp modal width/height behavior inside `Modal(title, body, width, height)` for small terminals.
- Keep `Confirm` lifecycle as a huh form and update labels/copy at call sites when the safe default must be obvious.
- Extend modal/confirm tests to assert title, border, centering/no-centering paths, and NO_COLOR output.

### Touchpoints
- `internal/ui/components/modal.go` — Modal frame and title rendering.
- `internal/ui/components/modal_test.go` — Modal style regression tests.
- `internal/ui/components/confirm.go` — Confirm labels/help behavior if supported centrally.
- `internal/ui/pages/*.go` — Confirm titles/copy for destructive actions.

### Contracts
```go
func Modal(title, body string, width, height int) string
func NewConfirm(title string, payload any, onYes func(any) tea.Cmd) Confirm
```

### Acceptance Criteria
- [ ] Modal title is visually distinct from body text.
- [ ] Modal remains centered when width and height are positive.
- [ ] Modal raw-box mode still works when width or height is zero.
- [ ] Confirm dialogs clearly show the action and safe cancel path.
- [ ] NO_COLOR modal output remains readable and avoids color-only meaning.

### Dependencies
- UIUX-004
- UIUX-010

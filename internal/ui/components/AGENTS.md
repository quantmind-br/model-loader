# AGENTS.md — internal/ui/components

## OVERVIEW
Reusable UI primitives. Mix of pure render helpers and bubbletea Models.

## WHERE TO LOOK
| File | What it does |
|------|-------------|
| `picker.go` | `ModelPicker` bubbletea model. Streaming GGUF scan + filterable list. Emits `ModelPickedMsg` / `ModelPickerCancelledMsg`. |
| `modal.go` | `Modal(title, body, width, height)` — centered lipgloss box. Pass `0,0` to skip centering (tests). |
| `sparkline.go` | `Sparkline(values, width)` — pure function, 8-bar ASCII sparkline. No bubbletea deps. |
| `statusbar.go` | `StatusBar` struct with level-based coloring (`Info`/`Warn`/`Error`). Not a full Model, just Render(). |
| `help.go` | `HelpMarkdown` constant + `RenderHelp(width)` / `RenderContextualHelp` via glamour. Markdown must stay in sync with actual keybindings (e.g. Server tab sub-views cycle with `v`, not `Tab`). |
| `overlay.go` | `Overlay()` — centers foreground string over background at screen center |

## CONVENTIONS
- Pure helpers (`Sparkline`, `Modal`, `RenderHelp`) take `width/height` explicitly. No tea.Msg handling.
- Interactive widgets (`ModelPicker`) are full bubbletea Models with `Init/Update/View`.
- **No service/ imports** — `ModelPicker` redeclares `ModelScanner` interface locally to avoid coupling.

## ANTI-PATTERNS
- Do not import `internal/service/` from this package. Redeclare minimal interfaces instead.
- Do not edit `HelpMarkdown` without updating the actual keybindings in pages. They must match.
- Do not make `Sparkline` or `Modal` stateful — they are render-only.

## NOTES
- `picker.go` uses the same streaming scan pattern as `ModelsPage`: Init starts a goroutine, events flow via tea.Cmds.
- `modal_test.go` uses `width=0,height=0` to test box rendering without lipgloss.Place centering.
- All styling delegates to `internal/ui/theme` — no hardcoded colors except `#1a1a1a` modal background.
- Each component has a matching `_test.go` covering rendering and message handling.

# Framework map — detect, locate UI code, refactor entry points

Used in Phase 1 (locate the UI source) and Phase 6 (describe idiomatic
changes). Detect the framework from manifest + imports, then go straight to the
files that own rendering, layout, and key handling — ignore business logic.

## Detection

| Signal                                                | Framework / runtime      |
| ----------------------------------------------------- | ------------------------ |
| `go.mod` + `github.com/charmbracelet/bubbletea`       | Bubble Tea (Go)          |
| `go.mod` + `github.com/rivo/tview` / `gdamore/tcell`  | tview / tcell (Go)       |
| `pyproject.toml`/`requirements` + `textual`           | Textual (Python)         |
| `pyproject` + `rich` only (no textual)                | Rich-rendered CLI        |
| `pyproject` + `prompt_toolkit` / `urwid` / `blessed`  | prompt_toolkit / urwid   |
| `Cargo.toml` + `ratatui` (or `tui`) + `crossterm`     | Ratatui (Rust)           |
| `package.json` + `ink` / `blessed` / `blessed-contrib`| Ink / blessed (Node)     |
| C sources + `<ncurses.h>` / `<notcurses.h>`           | ncurses / Notcurses (C)  |

## Where the UI lives, and the refactor entry points

### Bubble Tea (Go) — Elm architecture
- **Render:** `View() string` methods — the entire screen is a string. Layout
  is string concatenation, usually via `lipgloss` (`lipgloss.JoinHorizontal/
  Vertical`, `Style`, `.Width/.Height/.Border`).
- **Input:** `Update(msg tea.Msg)` switching on `tea.KeyMsg` — the keymap. Often
  a `key.Binding` table (`bubbles/key`).
- **State:** the model struct; sub-models per screen are common.
- **Refactor idioms:** extract repeated `lipgloss.Style` into a theme package;
  pull duplicated `View()` fragments into shared render funcs/sub-models; lift
  keybindings into one `key.Binding` map rendered in a `help` (`bubbles/help`)
  overlay; gate async work behind `tea.Cmd` so the UI never blocks.
- **Look in:** `internal/ui/`, `tui/`, `cmd/`, `*model*.go`, `*view*.go`,
  `*keys*.go`, `styles.go`.

### Textual (Python)
- **Render:** `compose()` yields widgets; layout via **CSS** (`.tcss`/`CSS`
  class attr) and `Container`/`Horizontal`/`Vertical`.
- **Input:** `BINDINGS = [...]` and `on_key` / `action_*` methods.
- **Refactor idioms:** move inline styles into a `.tcss` design-system file with
  semantic classes; build reusable `Widget` subclasses; consolidate `BINDINGS`;
  use `LoadingIndicator`, `reactive`, and `@work` for non-blocking async.
- **Look in:** `app.py`, `screens/`, `widgets/`, `*.tcss`.

### Ratatui (Rust)
- **Render:** a `draw`/`render` closure given a `Frame`; layout via
  `Layout::default().constraints([...])`; widgets `Block`, `Paragraph`, `List`,
  `Table`, `Tabs`.
- **Input:** an event loop matching `KeyCode` (crossterm).
- **Refactor idioms:** centralize `Style`/`Color` in a theme module; extract
  `render_<panel>(f, area, state)` fns; express layout with `Constraint`
  ratios/min instead of magic numbers; move the key match into a typed
  `Action` enum.
- **Look in:** `src/ui.rs`, `src/app.rs`, `src/tui/`, `src/components/`,
  `src/theme.rs`.

### Ink (Node/React)
- **Render:** React components returning `<Box>`/`<Text>`; flexbox layout props.
- **Input:** `useInput((input, key) => …)`.
- **Refactor idioms:** extract shared components; centralize colors in a theme
  module/context; consolidate `useInput` handlers; `<Spinner>`/state for async.
- **Look in:** `source/`, `src/`, `components/`, `*.tsx`.

### ncurses / Notcurses (C)
- **Render:** explicit `mvprintw`/`wattron`/`box`/`wrefresh` calls; manual
  coordinate math.
- **Input:** `getch()` in a loop with a big `switch`.
- **Refactor idioms:** wrap color pairs behind named macros; factor window
  layout into a sizing helper that recomputes on `KEY_RESIZE`; centralize the
  key switch.
- **Look in:** `*.c`/`*.h` with `initscr`, `newwin`, `getch`.

## What to extract per file (Phase 2 static read)

For each UI file, capture: the **screens/views** it renders, the **layout**
(regions + size policy), the **keybindings** it handles (→ `keybindings.json`),
**color/style** usage (hard-coded vs themed; truecolor assumptions), and whether
it renders **non-happy-path states** (empty/loading/error). These five facts per
file are the raw material for the gap analysis and the refactor plan.

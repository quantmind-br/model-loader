# TUI Accessibility

Practical accessibility for terminal UIs. Most "TUI a11y" failures come from over-reliance on color, missing focus indicators, and disorienting redraws.

## Keyboard-Only Audit

Required pass before shipping any TUI design.

- [ ] Every interactive element is reachable via `tab` / `shift+tab` or screen-specific navigation.
- [ ] Tab order is predictable (top-to-bottom, left-to-right).
- [ ] Focus traps exist ONLY in modals — `tab` cycles within the modal and never escapes until `esc`.
- [ ] Every action has at least one keybinding documented in `04-keybindings.md`.
- [ ] No action is mouse-only.
- [ ] No action requires a key combo that the terminal eats (avoid `ctrl+s` blocking, `alt+letter` ambiguity).

## Visible Focus

- Focused widget marked by `▶` prefix OR `[BRACKETS]` OR double-border — NEVER by background color alone.
- Focus marker survives color customization and theme inversion.
- Status bar updates to show keys for the focused widget.

## Color-Blind Safety

- ~8 % of men have color vision deficiency. Don't lose them to red/green confusion.
- Every color-coded state paired with:
  - A glyph (`✓` / `✗` / `⚠` / `●` / `○`), AND
  - A position (left-aligned vs right-aligned), AND/OR
  - A label (`OK` / `FAIL` / `WARN`).
- Test the design with deuteranopia / protanopia / tritanopia simulators.
- Avoid pairs that confuse: red+green, blue+purple, light-green+yellow.

Good:
```
● running    qwen-coder-30b      ✓ healthy
○ idle       mistral-7b          · idle
✗ crashed    deepseek-coder      ⚠ exited code 1
```

Bad (color-only):
```
qwen-coder-30b      (green dot)
mistral-7b          (gray dot)
deepseek-coder      (red dot)
```

## Contrast Tiers

Provide AT LEAST these themes:

- **Dark** — default for most terminals.
- **Light** — for light-background terminals.
- **High contrast** — pure white-on-black / black-on-white. No mid-tones.
- **Monochrome** — works in xterm-mono / SSH-without-color. Convey state through glyphs + position.

Aim for 7:1 contrast ratio for primary text (WCAG AAA equivalent). Dim secondary text to ~4.5:1 but never lower.

## Terminal Capability Tiers

Detect at runtime and fall back gracefully.

| Tier | Caps | Behavior |
|------|------|----------|
| Truecolor | 16M colors | Full palette. |
| 256 | xterm-256color | Quantize palette to nearest. |
| 16 | linux / dumb | Use ANSI 16 only; rely heavily on bold/inverse. |
| Mono | no color | Glyphs + bold + inverse + position. |

For glyphs: detect Unicode capability via `LANG`/`LC_*`. Fall back to ASCII set:

| Unicode | ASCII fallback |
|---------|----------------|
| `┌─┐│└┘` | `+-+\|+-+` |
| `▶` | `>` |
| `✓` | `*` or `[x]` |
| `✗` | `X` |
| `⚠` | `!` |
| `●` `○` | `o` `.` |
| `▁▂▃▄▅▆▇█` | `.,:;+oO@` |

## Stable Redraws (Screen-Reader Friendly)

Screen readers parse terminal output as a stream. Jarring full-screen redraws on minor updates break the reading flow.

- Update only changed regions. Don't repaint the whole screen for a single character change.
- Avoid moving the cursor on async events while the user is reading.
- Status bar updates should be atomic (one line replaced) — not "clear + redraw".
- For live data (logs, metrics), throttle to 1–2 Hz. Faster is wasted on screen readers anyway.

## Alternate Screen Buffer

- Use the terminal's alternate screen buffer (`\e[?1049h` / `\e[?1049l`) so quitting restores the user's previous shell view.
- On exit AND on crash: reset cursor (`\e[?25h`), reset attributes (`\e[0m`), exit alternate buffer.
- Install a signal handler for `SIGINT` / `SIGTERM` that performs the cleanup.

## Internationalization

- Use a Unicode width library to measure CJK and emoji widths (they take 2 columns).
- Don't assume one rune == one column.
- For RTL scripts (Arabic, Hebrew) where the spec applies: most TUI runtimes don't render RTL natively; document as a known limitation rather than half-supporting.

## Reduced Motion

- Spinners and animations have a no-animation fallback: a static `[loading]` or `[...]` token.
- Detect via `NO_ANIMATION=1` env var or a config option.
- Don't flash colors to signal events; use a sticky toast that the user dismisses.

## Document This in `09-accessibility.md`

For each major screen, fill in:

- Focused widget marker: ___
- Color-coded states and their non-color companions: ___
- Required minimum terminal size: ___
- Fallbacks for missing Unicode: ___
- Reduced-motion plan: ___

## Anti-Patterns

- Auto-hide focus on idle.
- "Subtle" focus (e.g., one shade lighter background) — invisible to most.
- "Modern" color-only state cues (the green/yellow/red dots without glyphs).
- Live-redrawing the whole table on each tick — kills screen readers and slow SSH.
- Hardcoding truecolor escapes without detecting `COLORTERM`.
- Forgetting cursor reset on crash → terminal stays in raw mode and the user must `reset` manually.

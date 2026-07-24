# Design principles — the optimized standard

The target design (Phase 5) must satisfy these; the diagnosis (Phase 3) scores
the current design against them. Distilled from clig.dev, ncurses/TUI
engineering tradition, and best-in-class TUIs (lazygit, k9s, btop, gh dash,
neovim, ranger, gitui).

The numbered block below is **shared verbatim with `tui-design`**. Keep the
block between the markers byte-identical across the two skills.

<!-- BEGIN canonical principles (identical in tui-design and tui-refactor) -->
1. **Human-first.** Optimize for clarity over brevity or tradition.
2. **Keyboard-first, mouse-optional.** Every action reachable from the keyboard.
3. **Discoverable.** The footer always shows the most relevant keys for the
   current context; `?` always opens a full help overlay.
4. **Consistent.** Same action, same key, everywhere. If `d` deletes here, it
   deletes everywhere.
5. **Reversible.** Destructive actions confirm with default focus on the SAFE
   option. Offer undo where feasible.
6. **Composable.** Expose the same data through scriptable channels (flags, JSON
   output, stdin/stdout) when automation matters.
7. **Fast feedback.** No operation freezes the UI. Long ops show a spinner and a
   cancel key; progress when measurable.
8. **Respect the terminal.** Graceful resize, ASCII fallback, alternate-screen
   buffer, cursor reset on exit, no truecolor assumption.
9. **Color-blind safe.** Never rely on red/green alone — pair with `✓`/`✗`,
   position, brightness.
10. **Avoid modal-heavy flows.** Prefer in-place editing and inline validation.
11. **Show structure first, detail on demand.** Lists/trees reveal scope; enter
    reveals depth.
12. **Status bar is sacred.** Always says where the user is and what they can do
    next.
13. **Stable redraws.** Batch updates; avoid full-screen flicker; don't move the
    cursor on async events while the user is reading.
14. **Empathic errors.** Say what failed, why, and what to do next.

## Anti-patterns to avoid

- Hidden actions (any active key MUST appear in status bar or help).
- Inconsistent keys (`d` deletes here, dismisses there).
- Modal-heavy flows.
- Color-only state.
- Tiny tables on wide terminals (use the space).
- Frozen UI during async ops.
- Defaulting to destructive (never auto-focus "Delete"/"Yes, overwrite").
- Suppressed focus outlines.
- Full-screen redraws on every keystroke (kills screen readers and slow SSH).
- Vim-only navigation without arrow alternatives.
- Truecolor assumptions without fallbacks.
<!-- END canonical principles (identical in tui-design and tui-refactor) -->

## Refactor-specific rule

A refactor is constrained by an existing user base. Improving a principle is
good; **breaking muscle memory or scripted behavior is a cost**. When a fix
requires changing a keybinding, a flag, or output format, flag it explicitly and
let Phase 4's answers decide whether the win is worth the churn. Prefer changes
that are invisible to existing workflows (theme, layout, status bar, states)
over changes that retrain the user (re-keying, renamed commands).

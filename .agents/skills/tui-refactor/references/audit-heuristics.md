# Audit heuristics — TUI design smells and how to spot them

Used in Phase 3. For each smell: what it is, how to detect it in source or in a
`before/` screenshot, the principle it violates, and the typical severity. Write
each hit to `findings.json`.

## Navigation & discoverability

- **No persistent status bar / key hints.** Detect: no bottom line rendering
  contextual keys; help only via undocumented keys. → P3/P12. *major*.
- **No `?` help overlay.** Detect: keymap lacks a help action, or help is a
  static README. → P3. *major*.
- **Hidden/undocumented actions.** Detect: key handlers in code with no
  corresponding hint in status bar or help. → anti-pattern #1. *major*.
- **Deep modal nesting.** Detect: modals that open modals; flows that could be
  inline edits. → P10. *minor→major*.

## Consistency

- **Same action, different keys (or vice-versa).** Detect: compare the
  per-context keymaps from `keybindings.json`; the same verb bound to different
  keys, or one key meaning different things without a clear contextual reason.
  → P4. *major*.
- **Vim-only or arrow-only navigation.** Detect: only `hjkl` or only arrows in
  the key match. → anti-pattern #10. *minor*.
- **Reserved keys repurposed.** Detect: `q`/`?`/`esc`/`/`/`:` doing something
  non-standard. → P4. *minor→major*.

## Visual system

- **Hard-coded colors / no theme.** Detect: literal color codes scattered across
  render code instead of a central theme/semantic roles. → maintainability + P8.
  *major* (blocks consistent restyle).
- **Color-only state.** Detect: status conveyed solely by color (red=error) with
  no glyph/text. → P9. *major*.
- **Truecolor assumptions.** Detect: 24-bit hex colors with no 256/16/mono
  fallback. → P8. *minor*.
- **Inconsistent borders/spacing.** Detect: mixed border styles, ad-hoc padding,
  no density rhythm. → cosmetic→minor.

## Layout & resize

- **Hard-coded widths/heights.** Detect: magic numbers in layout instead of
  ratio/min constraints; content clipped on small/large terminals. → P8. *major*.
- **No "terminal too small" floor.** Detect: no minimum-size guard; layout
  corrupts below a threshold. → P8. *minor*.
- **Wasted space on wide terminals.** Detect: fixed narrow content centered in a
  huge terminal. → anti-pattern #5. *minor*.
- **Mid-word wrapping inside panels.** → cosmetic→minor.

## Feedback & state

- **Happy-path-only rendering.** Detect: no empty/loading/error branches in the
  view; the screen is blank or stale during these states. → P7/P14. *major*.
- **Blocking async.** Detect: network/IO on the UI thread / inside `Update`
  without a command/worker; UI freezes. → P7. *blocker→major*.
- **Unhelpful errors.** Detect: raw error strings or codes with no remedy. →
  P14. *minor→major*.
- **Destructive default focus.** Detect: confirm dialogs auto-focused on
  Delete/Overwrite, or destructive actions with no confirm. → P5. *major*.
- **Flicker / full redraws.** Detect: full-screen clear+redraw on every event. →
  P13. *minor* (worse over SSH / for screen readers).

## Accessibility

- **No focus indicator.** Detect: focused widget visually identical to unfocused.
  → P2. *major*.
- **Mouse-only affordances.** Detect: actions only triggerable by click. → P2.
  *major*.

## How to assign severity

- **blocker** — unusable or data-loss risk (UI freezes, destructive with no
  confirm).
- **major** — significantly hurts everyday use (no status bar, color-only state,
  happy-path-only, inconsistent keys, no theme).
- **minor** — friction or polish gap (vim-only, no small-terminal floor).
- **cosmetic** — looks off but works (spacing, border mix).

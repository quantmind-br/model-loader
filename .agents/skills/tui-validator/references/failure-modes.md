# TUI failure-mode catalogue

A reference of bugs to actively look for. Load this when entering Phase 3
(probe), Phase 4 (stress), or Phase 5 (visual). Each entry tells you the
symptom, the cause, how to detect it, and the severity to report.

For brevity: "diff = identical" means `tui-diff.sh` returned `identical: true`.

## Keybinding bugs

### B-01 Dead binding (documented but inactive)
- **Symptom:** key is in the help screen but pressing it changes nothing.
- **Detect:** diff identical between before/after, no error text.
- **Severity:** `major` (docs are lying to the user).
- **Suggestion:** either implement the binding or remove it from help.

### B-02 Ghost binding (active but undocumented)
- **Symptom:** an arbitrary key produces a visible action even though it's
  not in the help.
- **Detect:** during the default probe set, a non-help key produces a diff.
- **Severity:** `minor` — usually a leftover from refactoring.

### B-03 Shortcut conflict (same key, two actions, same context)
- **Symptom:** help screen lists the same key twice for different actions,
  or the binding's behaviour depends on internal state in a confusing way.
- **Detect:** parse `keybindings.json`, group by `(key, context)`, flag any
  group with `count > 1`.
- **Severity:** `major`.

### B-04 Modal leak
- **Symptom:** a binding from modal A still fires when modal B is open.
- **Detect:** during Phase 3, after opening a modal (e.g. confirmation
  dialog), try keys that the help says belong to the parent context. Any of
  them should be ignored.
- **Severity:** `major` — high chance of data loss if a destructive parent
  binding fires while a "yes/no" dialog is open.

### B-05 Hung modal
- **Symptom:** opening a modal works but no key can close it (not even
  `Escape`).
- **Detect:** after opening any modal, send `Escape` then `q` then `C-c`
  and confirm at least one returns to the parent state.
- **Severity:** `blocker`.

## Input bugs

### I-01 Multi-byte character dropped
- **Symptom:** sending `ção` shows `c?o` or `co`.
- **Detect:** Phase 4 capture missing characters.
- **Severity:** `major`.

### I-02 Combining mark on wrong base
- **Symptom:** `e´` shown as `é ` or `e ´` (separated).
- **Detect:** Phase 4 NFD test, diff against NFC reference.
- **Severity:** `minor` — depends on whether the TUI claims unicode support.

### I-03 Wide char column overflow
- **Symptom:** CJK glyph corrupts the next column (border breaks).
- **Detect:** Phase 4 CJK test combined with a screenshot — borders should
  remain intact.
- **Severity:** `major`.

### I-04 Paste interpreted key-by-key
- **Symptom:** pasting `quit\n` opens the quit dialog, then the inventory
  panel, then yanks, etc.
- **Detect:** Phase 4 bracketed-paste test ends in an unexpected modal.
- **Severity:** `major`.

### I-05 Input lag / stalled key buffer
- **Symptom:** burst of 20 navigation keys processes only the first 3.
- **Detect:** Phase 4 rapid-input test, count visible moves.
- **Severity:** `minor` unless the buffer never recovers.

## Rendering bugs

### R-01 Border / box-drawing breakage
- **Symptom:** Unicode box-drawing replaced with `?`, doubled, or shifted.
- **Detect:** Visual inspection of the screenshot, especially at the
  `tiny` (60×20) and `huge` (200×60) sizes.
- **Severity:** `major` if structural, `cosmetic` if only on resize edges.

### R-02 Colour bleed
- **Symptom:** a foreground colour leaks into the next row/cell.
- **Detect:** look for unexpected colour transitions in the ANSI capture;
  diff `.ansi` files between sizes with `--ansi` flag.
- **Severity:** `cosmetic` unless it makes the UI unreadable.

### R-03 Cursor stuck after modal close
- **Symptom:** cursor visible in a corner / lingering after closing a popup.
- **Detect:** capture cursor position before opening modal, after closing
  modal; should match.
- **Severity:** `minor` — distracting but rarely harmful.

### R-04 Alternate-screen not restored
- **Symptom:** after the TUI exits cleanly, the terminal scrollback still
  contains TUI frames; or vice versa, the user's prior shell history is gone.
- **Detect:** capture before and after a clean quit; if `alternate_screen`
  flag differs from the value at launch, flag it.
- **Severity:** `minor`.

### R-05 Text overflow / unwrapped truncation
- **Symptom:** long text runs past the panel's right edge into a neighbouring
  panel, or is truncated without an ellipsis.
- **Detect:** at `tiny` size, look for text exceeding declared panel width.
- **Severity:** `minor`.

### R-06 Tiny-size collapse
- **Symptom:** at 60×20 the TUI either crashes, panics, or draws over its
  own borders.
- **Detect:** Phase 5 `tiny` screenshot.
- **Severity:** `major` if it crashes, `minor` if it just looks bad.

### R-07 Wide-size empty space
- **Symptom:** at 200×60 the layout sits in a tiny corner instead of using
  the available space.
- **Detect:** Phase 5 `huge` screenshot.
- **Severity:** `cosmetic` but worth surfacing as a design suggestion.

### R-08 Status-bar truncation
- **Symptom:** the status / help hint at the bottom is cut off so users can't
  see the available actions.
- **Detect:** at `default` (80×24), the rightmost ~10 columns of the status
  bar are `…` or missing.
- **Severity:** `major` (discoverability).

## Process / lifecycle bugs

### P-01 Crash on startup
- **Symptom:** the TUI exits within ~1 s of launch with an error.
- **Detect:** `tui-launch.sh` reports `dead pane` immediately after start.
- **Severity:** `blocker`.

### P-02 Crash on resize
- **Symptom:** TUI process dies when the pane is resized.
- **Detect:** after any `tui-resize.sh`, check `pane_is_dead`.
- **Severity:** `blocker`.

### P-03 Crash on unicode input
- **Symptom:** TUI dies when receiving multi-byte UTF-8.
- **Detect:** during Phase 4, after any send, check `pane_is_dead`.
- **Severity:** `blocker`.

### P-04 Hang (no redraw for >5 s)
- **Symptom:** the TUI accepts input but never redraws.
- **Detect:** `wait_for_redraw` returns without convergence, last capture
  matches first capture after multiple key sends.
- **Severity:** `blocker`.

### P-05 Leaked resources on quit
- **Symptom:** after the TUI quits, child processes (servers, subscribers)
  are still alive.
- **Detect:** `pgrep -f <binary-name>` after `tui-cleanup.sh`.
- **Severity:** `major` for daemon-spawning TUIs (model-loader-like).

## Conventions for findings.json entries

Every finding written by the audit should look like:

```json
{
  "id": "B-03",
  "severity": "major",
  "title": "Key 'q' is bound to both 'quit' and 'submit' in the Launcher tab",
  "phase": "probe",
  "description": "Help screen lists 'q : quit' under Global, but the Launcher tab also uses 'q' for 'submit profile'. When focus is on the launcher list, pressing 'q' submits without confirmation instead of quitting.",
  "evidence": "captures/0017-launcher-q.txt",
  "suggestion": "Rebind launcher submit to Enter only, or require Shift-Q in the launcher context."
}
```

The `id` field is optional — use the catalogue ID (B-03, R-04, etc.) when
the finding matches one of the entries above. Otherwise omit it.

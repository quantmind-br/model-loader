# Pitfalls + canonical schemas

Lessons learned from real audits. Load this file before writing
`keybindings.json` or `findings.json`, and check it whenever something
"should work" but doesn't.

## Pitfall 1 — `WAYLAND_DISPLAY` not in the agent shell

**Symptom**: `tui-screenshot.sh` falls back to headless Chromium, or `grim`
exits with `compositor doesn't support ...`.

**Cause**: the Claude shell didn't inherit Wayland env vars from the session.

**Fix**: every script already calls `wayland_autodetect` from `_common.sh`
on startup, which scans `/run/user/$UID/wayland-*`. If `grim` still fails
after that, the host is genuinely headless — accept the Chromium fallback
and note it in the report's Limitations.

## Pitfall 2 — `--paste` against a filter input

**Symptom**: you send `tui-send.sh "$S" --paste "Qwen"` to a filter input
(`/ → text`) and the first character appears in the filter but the rest
either disappear or fire random bindings (`e` opens export, `n` opens new,
etc.).

**Cause**: the TUI's input handler only reacts to individual KEYDOWN events
and doesn't listen to bracketed paste. `paste-buffer` arrives faster than
the focus-state machine and the trailing chars fall through to the parent
handler.

**Fix**: use `--literal` instead. It uses `tmux send-keys -l`, which arrives
as N separate key events at the same rate the user would type. Add
`--delay 30` if the TUI debounces inputs.

```bash
tui-send.sh "$S" --literal "Qwen"               # ← correct
tui-send.sh "$S" --literal --delay 30 "Qwen"    # ← if the above is too fast
```

## Pitfall 3 — Resize did nothing

**Symptom**: `tui-resize.sh "$S" 160 40` returns immediately but
`#{pane_width}x#{pane_height}` still shows the old size.

**Cause**: `tmux resize-window` needs `window-size manual` on the session,
otherwise tmux re-fits the window to whatever client is attached (none, in
our case).

**Fix**: `tui-launch.sh` already sets `window-size manual` and
`aggressive-resize on`. If you ever start a session by hand, set those flags
before resizing. The current `tui-resize.sh` checks the post-resize geometry
and `die`s loudly if it didn't take.

## Pitfall 4 — Multiple half-failed sessions piling up

**Symptom**: `tmux list-sessions` shows ten `tuiv-*` sessions, most idle.

**Cause**: `tui-launch.sh` succeeded but the audit aborted before reaching
`tui-cleanup.sh`.

**Fix**: at the **start** of every audit run, do:

```bash
scripts/tui-cleanup.sh --all
```

That nukes every leftover `tuiv-*` session. Do it at the end too.

## Pitfall 5 — `keybindings.json` schema mismatch

**Symptom**: `tui-report.sh` runs but the "Keybindings inventory" table is
empty even though you wrote a populated JSON file.

**Cause**: the report consumes the canonical flat schema (see below). If
you wrote a grouped-by-context schema, the renderer now auto-flattens it,
but only if it matches one of three accepted shapes. Anything else → empty
table.

**Fix**: write one of the three accepted shapes. The canonical one is
preferred; the grouped one is acceptable when you want to keep the file
human-readable by context.

## Pitfall 6 — Quitting the TUI between phases vs. restoring state

**Symptom**: a binding that worked in Phase 3 fails when re-tested in
Phase 5 because the TUI is in a different modal state.

**Cause**: after each probe, you didn't restore the known baseline.

**Fix**: after every key send, navigate back to the screen the TUI was on
before the send. If a key opens a modal, send `Escape` (or whatever closes
it) immediately after capturing the after state. Most Bubble Tea apps honour
`Escape` for modal dismissal — but verify.

---

## Canonical schemas

### `keybindings.json`

The report renderer accepts any of these three shapes:

**Shape A — canonical flat (preferred):**

```json
{
  "bindings": [
    {"key": "?", "context": "global",   "description": "toggle help",      "source": "documented+observed"},
    {"key": "Tab", "context": "global", "description": "next tab",         "source": "observed"},
    {"key": "/", "context": "profiles", "description": "filter",           "source": "documented"}
  ]
}
```

Fields:
- `key` (required) — tmux key name (`?`, `F1`, `C-c`, `Up`, `Space`, `Enter`)
- `context` (optional, default `"global"`) — tab / modal / mode the binding belongs to
- `description` (optional) — human description. **`action` is an accepted alias**
  — `tui-refactor` names this same field `action`. The renderer reads whichever
  is present, so a `keybindings.json` produced by `tui-refactor` renders here
  without translation.
- `source` (optional) — one of `documented`, `observed`, `documented+observed`, `inferred`
- `status` (optional) — `active`, `dead`, `error`, `crash`, `skipped`, `info`
- `notes` (optional) — anything else (extra evidence, related findings, etc.)

**Shape B — grouped by context (auto-flattened):**

```json
{
  "global":   [{"key": "?", "description": "toggle help"}],
  "profiles": [{"key": "E", "description": "edit"}]
}
```

Each top-level key becomes the `context` for every binding inside it.

**Shape C — bare array (auto-wrapped):**

```json
[
  {"key": "?", "context": "global", "description": "toggle help"}
]
```

### `findings.json`

```json
{
  "findings": [
    {
      "id": "B-03",
      "severity": "major",
      "title": "Key 'q' is bound to both 'quit' and 'submit' in Launcher",
      "phase": "probe",
      "description": "Help screen lists 'q : quit' globally, but the Launcher tab also binds 'q' to 'submit'. When focus is on the launcher list, pressing 'q' submits without confirmation instead of quitting.",
      "evidence": "captures/0017-launcher-q.txt",
      "suggestion": "Rebind launcher submit to Enter only, or require Shift-Q.",
      "repro": ["Boot fresh", "Press 2 to go to Launcher", "Press q"]
    }
  ]
}
```

Fields:
- `id` (optional) — failure-mode catalogue ID (`B-03`, `R-04`, `P-01`, …) or
  your own short tag (`bug-info-overlap`)
- `severity` (required) — `blocker | major | minor | cosmetic | info`
- `title` (required) — one-line summary
- `phase` (optional) — `discover | inventory | probe | stress | visual`
- `description` (required) — what happened, why it matters
- `evidence` (optional) — a path string (relative to workspace) to a capture or
  PNG. `tui-refactor` reads the same field as a structured object
  `{files, screenshot, capture}`; a bare string is the accepted shorthand and
  normalizes to `{"capture": <path>}` (or `{"screenshot": <path>}` for a PNG) when
  that skill ingests this file.
- `suggestion` (optional) — concrete proposed fix
- `repro` (optional) — array of repro steps from a fresh launch

Use `info` severity for things that aren't bugs but the report should
preserve (e.g. "skipped destructive key X — re-run with --allow-destructive").

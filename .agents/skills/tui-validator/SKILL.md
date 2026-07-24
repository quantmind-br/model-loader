---
name: tui-validator
description: >-
  Use when validating, auditing, smoke-testing, QA-testing, fuzzing, or
  regression-checking a terminal UI or CLI, including keybindings, shortcut
  conflicts, input handling, rendering, resize behavior, unicode/paste, and
  visual regressions.
---

# TUI Validator

A repeatable audit pipeline for terminal UIs. The pipeline runs the TUI inside a
dedicated tmux session, drives it with `send-keys`, scrapes the pane after each
action (both plain text and ANSI-coloured), takes pixel screenshots when a live
Wayland or headless Chromium backend is available, and writes a markdown report
with findings and screenshots.

## When to invoke this skill

Trigger this skill any time the user wants to:

- validate / audit / QA / smoke-test a TUI
- find broken or dead keyboard shortcuts
- detect shortcut conflicts (same key bound twice in the same context)
- check rendering at multiple terminal sizes
- test unicode / accented characters / paste / control characters
- review the visual design of a TUI from screenshots
- spot regressions after a TUI refactor

Examples that should trigger it: "test this TUI", "check the shortcuts of my
CLI", "does my Bubble Tea app render right when I resize", "find UX issues in
my-tui", "audit ./mytool", "screenshot the TUI for me and tell me
what's wrong".

## Prerequisites (verify before running)

Run this once before the first audit:

```bash
scripts/tui-check-prereqs.sh
```

> **Platform.** The bundled scripts assume **Linux with GNU coreutils** (GNU
> `find -printf`, `sed -E`, `date -u`, `mktemp --suffix`) and, for live pixel
> capture, a **Wayland** session. They are not tested on macOS/BSD; on those
> hosts prefer the text-mode phases or run inside a Linux container.

Only `tmux` and `jq` are hard requirements for text-mode auditing. Other tools
enable richer phases and degrade gracefully:

- `grim` + a Wayland terminal (`foot`, `kitty`, `alacritty`, `ghostty`, or
  `wezterm`) -> live pixel screenshots
- `aha` + Chromium/Chrome -> headless screenshot fallback from ANSI captures
- `magick`/`compare` -> pixel-diff of screenshots between two states
- `python3` -> delayed literal typing in `tui-send.sh --literal --delay`
- `slurp` -> manual region selection (only needed for interactive debugging)
- `hyprctl` -> resolve the tmux client window on Hyprland for `grim` framing

If neither live Wayland screenshots nor headless Chromium screenshots are
available, the audit downgrades to text-only. Say this explicitly in the
report's Limitations section.

## High-level workflow

The audit has six phases. **Run them in order**; each one feeds the next.
Reference files in `references/` give deeper guidance — load them lazily, only
the one you need for the phase you're in.

```
1. Discover    → understand what the TUI is, how to launch it, what help exists
2. Inventory   → build a keybinding map (from help screen, status bar, docs)
3. Probe       → systematically send every binding, classify the response
4. Stress      → unicode, paste, control chars, rapid input, modifiers
5. Visual      → resize matrix, screenshots, diff against baseline
6. Report      → write the markdown report with findings + suggestions
```

Each phase writes artifacts into a per-run workspace:

```
~/.cache/tui-validator/<tui-name>/<UTC-timestamp>/
├── meta.json                  # tui path, args, terminal sizes, version
├── captures/                  # text + ANSI scrapes, indexed by step
│   ├── 0001-initial.txt
│   ├── 0001-initial.ansi
│   ├── 0002-after-key-?.txt
│   └── ...
├── screenshots/               # PNGs from live or headless screenshot backend
│   ├── 80x24-initial.png
│   ├── 120x40-initial.png
│   ├── modal-help.png
│   └── ...
├── keybindings.json           # inventory built in phase 2
├── findings.json              # structured issues found
└── report.md                  # working copy of the diagnostic
```

When Phase 6 finishes, `tui-report.sh` also drops a **canonical copy** of the
diagnostic at the **root of the audited codebase** as `TUI_AUDIT.md`
(i.e. `<tui_cwd>/TUI_AUDIT.md`). That file is the artifact the user is
expected to read; the workspace `report.md` exists only as a backup with the
same content. The script prints both paths on stdout — the codebase root path
first, then the workspace path.

Screenshot links in the report use **absolute filesystem paths**, so the
codebase-root copy renders identically to the workspace copy regardless of
where the markdown is opened from.

If the codebase root is read-only or otherwise unwritable, the script logs a
warning to stderr and falls back to the workspace copy only — never aborts.

## Phase 1 — Discover

Goal: figure out what we are testing without blindly launching it.

1. Resolve the TUI binary. `scripts/tui-launch.sh` accepts absolute paths,
   relative paths, and commands found on `$PATH`. If the user gives a
   directory, look for:
   - `./<name>` (raw binary at repo root, common for Go projects)
   - `./build/<name>`, `./bin/<name>`, `./target/release/<name>`, `./dist/<name>`
   - `Makefile` targets named `build`, `run`, `tui`
2. Read project docs (README, AGENTS.md, CLAUDE.md, docs/) for documented
   keybindings, launch flags, required env vars, and known modes. **Do this
   first** — it saves the entire probing phase a lot of guesswork.
3. Try `--help` first; if that prints a non-trivial usage block, parse it for
   subcommands and flags. A TUI that has subcommands often has a flagless entry
   point that drops you into the interface.
4. Decide an initial geometry: default to `80x24`. Note it in `meta.json`.
   `tui-launch.sh` also records a best-effort `--version` result when the
   command returns quickly.

If the TUI requires arguments to start (a file path, a config), ask the user
**once** before launching — don't try to invent inputs.

## Phase 2 — Inventory keybindings

Use `scripts/tui-launch.sh` to start the TUI in tmux, then probe for a help
screen. See `references/common-keys.md` for the order in which to try the
common help bindings (`?`, `h`, `F1`, `C-h`). `tui-send.sh` normalizes common
human spellings like `Ctrl-h`, `Alt-x`, `Esc`, and `Shift-Tab` to tmux tokens.

Capture each help screen as plain text and parse it. Most TUIs format help as
`<key>  <description>` on each line — a regex like `^\s*([A-Za-z0-9?/<>\-]+|Ctrl-[A-Z]|Alt-[A-Z]|F\d+)\s{2,}(.+)$` covers a lot of them. Be tolerant: write the
binding list to `keybindings.json` with `{key, description, context}` triples.
`context` defaults to `"global"` but distinguish per-mode/per-tab whenever the
help text labels sections (e.g. a tabbed app's "Profiles Tab", "Launcher Tab").

If no help screen exists, fall back to:

- the status bar (last line(s) of the pane) — many TUIs show contextual hints there
- the project README / source code if accessible

It is OK to have an incomplete inventory. Phase 3 will still probe a default
set of common keys from `references/common-keys.md`.

## Phase 3 — Probe every binding

For each key in the inventory:

1. `scripts/tui-capture.sh` → snapshot **before**
2. `scripts/tui-send.sh "$SESSION" "$KEY"` -> send the keystroke (correctly escaped)
3. Wait 200–500 ms (TUI redraws are not instant; longer for network-bound ones)
4. `scripts/tui-capture.sh` → snapshot **after**
5. `scripts/tui-diff.sh` → diff the two text scrapes

Classify the result:

- **active** — text changed in a meaningful way
- **dead** — no diff at all (binding likely broken or the action has no visual side-effect — note both possibilities in the finding)
- **error** — error text appeared, or the pane went blank
- **crash** — tmux pane reports the process died (`tmux list-panes` shows `dead`)

After each key, restore a known state (usually `Escape` then a navigation back
to the initial screen). If the key is on the **danger list** — the single canonical enumeration lives in
`references/common-keys.md` (`d` `D` `x` `X` `Delete` `Backspace` `C-k` `C-w`
`C-u` `!` `:q!`) — require explicit user opt-in before sending it; TUIs that
manage real resources can lose data.

Crashes terminate the phase early — capture the last screen, dump the tmux
buffer history, and record the crashing key in `findings.json`.

## Phase 4 — Stress

Pull `references/special-chars.md` into context. Send each character class via
`scripts/tui-send.sh --literal`:

- Lowercase Latin extended: `á é í ó ú ã õ ç ñ ü`
- Symbols frequently mistyped by terminals: `€ £ ¥ © ® § ¶`
- Wide CJK: `中 日 韓 한`
- Emoji (single + ZWJ sequences): `😀 🚀 👨‍👩‍👧`
- Combining marks: `e + ́` (U+0301), `a + ̃` (U+0303)
- Bracketed paste: wrap a multi-line string in `\e[200~ ... \e[201~`
- Control chars not bound: `C-g`, `C-n`, `C-p`, etc.

Capture after each class; the issue you are hunting is the difference between
what was sent and what the TUI rendered. Use `scripts/tui-diff.sh --strict` so
even whitespace differences surface.

Failure modes to flag (full catalog in `references/failure-modes.md`):

- character dropped silently
- combining mark applied to the wrong base character
- wide char overflowing its cell (next column corrupted)
- paste interpreted one-key-at-a-time (binding fires inside paste)

## Phase 5 — Visual

Take screenshots through `scripts/tui-screenshot.sh` at each of these sizes:

| Label    | Cols × Rows | Why                                                  |
| -------- | ----------- | ---------------------------------------------------- |
| `tiny`   | 60 × 20     | Forces hard truncation; exposes lazy layout code     |
| `default`| 80 × 24     | The historic baseline; bare minimum the TUI must hit |
| `wide`   | 160 × 40    | Common for tiled WMs; exposes hard-coded widths      |
| `tall`   | 80 × 50     | Exposes vertical-overflow / scrollbar bugs           |
| `huge`   | 200 × 60    | Stress test: does the layout grow gracefully?        |

For each size:

1. Resize via `scripts/tui-resize.sh COLS ROWS`
2. Wait for redraw (300 ms — most Bubble Tea apps debounce ~150 ms)
3. Take a screenshot with `scripts/tui-screenshot.sh "$SESSION" "$LABEL"`.
   It prefers live Wayland screenshots and falls back to ANSI -> HTML ->
   headless Chromium when needed.
4. After capturing `default`, pass `--diff default` for later sizes. The script
   uses ImageMagick when available and saves the per-pixel delta as
   `<label>-vs-default.png`; if ImageMagick is missing, record the skipped diff
   in Limitations.

Things to flag (deeper catalog in `references/failure-modes.md`):

- borders broken (Unicode box-drawing replaced with `?` or duplicated chars)
- text wrapped mid-word inside a panel that shouldn't wrap
- colour bleed (a foreground colour leaks into the next line)
- cursor stuck in the corner after a modal closes
- alternate-screen-buffer not restored on exit

Because Wayland screenshots need a visible window, the tmux client must be
focused. `scripts/tui-screenshot.sh` uses `hyprctl` to focus the right window
when Hyprland is available. If live capture is not possible, it renders the
latest ANSI capture through the headless fallback instead of failing the visual
phase outright.

## Phase 6 — Report

`scripts/tui-report.sh` reads `meta.json`, `keybindings.json`, `findings.json`
and the artefacts in `captures/` and `screenshots/` and renders
`templates/report-template.md` into **two** locations:

1. `<workspace>/report.md` — working copy kept alongside captures/screenshots.
2. `<tui_cwd>/TUI_AUDIT.md` — **canonical copy at the root of the audited
   codebase**. This is the one the user opens.

Both files have identical content; screenshot links are absolute paths so the
codebase copy renders correctly. If the codebase root is unwritable, the
script warns and only writes the workspace copy.

The report must contain, in this order:

1. **Summary** — TUI name, version (if `--version` returned one), launch
   command, terminal in which it was tested, total phases run, total findings
   by severity.
2. **Keybindings inventory** — the parsed table, with `context`, `source`, and
   `status` columns.
3. **Findings** — one section per issue, each tagged with severity
   (`blocker / major / minor / cosmetic / info`), a short description, the captured
   evidence (text snippet or screenshot path), and a concrete suggested fix.
4. **Visual gallery** — resize screenshots plus diff maps against the baseline
   when generated.
5. **Methodology** — phases run, keys probed, special chars sent, terminal
   sizes used. Anything **not** tested goes here too (e.g. "skipped destructive
   keys d/D/x: rerun with --allow-destructive to test").
6. **Workspace pointer** — absolute path to the workspace dir.

The summary at the very top should be skimmable in ~10 seconds. Lead with
blockers and majors; cosmetic stuff goes at the bottom.

## Safety guidelines

- **Never** run the audit against a TUI that manages production resources
  without the user opting in. Ask if you're unsure.
- Destructive keys are skipped by default — see the canonical danger list in
  `references/common-keys.md`. Mention this in the methodology section.
- The TUI runs as the current user — if it can shell out (run profiles, exec
  models, etc.), it can affect the system. Stay in the audit, don't trigger
  arbitrary executions.
- Tear down with `scripts/tui-cleanup.sh` even when the audit aborts. A
  leftover tmux session with a TUI attached eats CPU silently.

## Reference files

Load these only when entering the matching phase — they aren't needed up
front:

- `references/common-keys.md` — help-screen probe order, common bindings, danger list
- `references/special-chars.md` — the full unicode/paste test set
- `references/failure-modes.md` — catalogue of TUI bugs and how to detect each
- `references/tmux-tricks.md` — `send-keys` escaping rules, `capture-pane`
  flags, alternate-screen handling
- `references/pitfalls.md` — gotchas that bit real audits + canonical
  JSON schemas for `keybindings.json` and `findings.json` (READ THIS BEFORE
  WRITING EITHER FILE)
- `scripts/tui-check-prereqs.sh` — dependency check; run at the start of an audit

## Sending text vs sending keys (read before Phase 3/4)

`scripts/tui-send.sh` has four modes; pick the right one or the TUI won't
react the way you expect:

| Mode                  | Mechanism                                | Use when                                                  |
| --------------------- | ---------------------------------------- | --------------------------------------------------------- |
| key tokens (default)  | `tmux send-keys NAME`                    | Sending individual keys / chords (`q`, `Enter`, `C-c`)    |
| `--literal "text"`    | `tmux send-keys -l` (per-char key events) | Filter inputs, search boxes, anything that doesn't accept paste |
| `--paste "text"`      | `tmux load-buffer` + `paste-buffer`      | Text areas / editors that handle paste cleanly            |
| `--paste-bracketed`   | Same as `--paste` wrapped in `\e[200~/\e[201~` | **Testing** whether the TUI respects bracketed paste |

The single biggest source of "the test sent the text but the TUI didn't see
it" is using `--paste` against a filter input that only listens to KEYDOWN
events. Default to `--literal` for any input widget — only escalate to
`--paste` for true text areas.

## Wayland / screenshot environment

`tui-screenshot.sh` and `tui-launch.sh` auto-detect `WAYLAND_DISPLAY` from
`/run/user/$UID/wayland-*` if it isn't already in the environment. They do
the same for `HYPRLAND_INSTANCE_SIGNATURE`. You should **not** need to
`export` either of those manually when running from a fresh agent shell.

If `wayland_autodetect` fails (truly headless host), the screenshot phase
downgrades to `aha -> Chromium --headless` for an HTML-rendered PNG. Quality is
lower; say so in the report's Limitations section.

## Quick recipe (one-liner mental model)

```bash
# 0. check dependencies
scripts/tui-check-prereqs.sh

# 1. launch
SESSION=$(scripts/tui-launch.sh ./build/my-tui)   # example fictional app

# 2. inventory (try help, fall back to common keys)
scripts/tui-send.sh "$SESSION" "?"
scripts/tui-capture.sh "$SESSION" inventory-help

# 3. probe each key (loop omitted)
scripts/tui-send.sh "$SESSION" "Tab"
scripts/tui-capture.sh "$SESSION" after-tab

# 4. stress
scripts/tui-send.sh "$SESSION" --literal "ção"

# 5. visual
scripts/tui-resize.sh "$SESSION" 80 24
scripts/tui-screenshot.sh "$SESSION" default
scripts/tui-resize.sh "$SESSION" 160 40
scripts/tui-screenshot.sh "$SESSION" wide-after-resize --diff default

# 6. report
scripts/tui-report.sh "$SESSION"

# 7. cleanup
scripts/tui-cleanup.sh "$SESSION"
```

The full audit repeats this recipe across the full inventory and stress set.
Drive those loops from this skill — don't ask the user to script it themselves.

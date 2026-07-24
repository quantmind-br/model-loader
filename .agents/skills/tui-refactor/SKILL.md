---
name: tui-refactor
description: "Refactor and modernize an EXISTING TUI's UI/UX by reading its codebase, reverse-engineering the current design, diagnosing design smells, and producing an optimized target design plus a file-level refactor plan. Unlike tui-design (starts from a spec) this starts from real code; unlike tui-validator (only audits) this redesigns. Does NOT modify source code — outputs an actionable migration plan. Use whenever the user asks to refactor, redesign, modernize, overhaul, improve, clean up, standardize, or optimize a terminal UI/CLI app that already has code. Triggers include \"refactor this TUI\", \"redesign my CLI UI\", \"modernize this terminal app\", \"melhorar UX da TUI\", \"refatorar TUI\", \"redesenhar CLI\", \"modernizar interface terminal\", and \"/tui-refactor\". Prefer over tui-design when codebase exists; prefer over tui-validator when the user wants design improvement rather than only bugs/findings."
---

# tui-refactor

Take an **existing** terminal UI codebase and produce a complete redesign: a
reverse-engineered map of how it looks and behaves today, a diagnosis of where
its UX falls short of best practice, an optimized target design (wireframes,
component library, keybinding scheme, style guide, states), and a concrete
**file-level refactor plan** an implementer can execute.

This skill is the third member of a TUI trio:

| Skill            | Input            | Output                          | Touches code |
| ---------------- | ---------------- | ------------------------------- | ------------ |
| `tui-design`     | a spec (SPECS.md)| greenfield design docs          | no           |
| `tui-validator`  | a codebase       | an audit report (bugs/findings) | no           |
| **`tui-refactor`** | **a codebase** | **redesign + migration plan**   | **no***      |

\* It deliberately stops at an actionable plan. It does **not** rewrite source
files. The plan is the artifact a follow-up implementation session (or the
user) executes.

It is framework-agnostic: it works on Bubble Tea (Go), Textual (Python),
Ratatui (Rust), Notcurses (C), Ink (Node), ncurses, blessed, and similar.

## When to invoke this skill

Trigger any time the user wants to **improve the design of an existing TUI**,
not just find bugs in it and not design one from a spec:

- "refactor / redesign / modernize / overhaul this TUI"
- "make my CLI's UI consistent / cleaner / more usable"
- "the layout is a mess, fix the UX"
- "padronizar / refatorar / melhorar / otimizar a interface da TUI"
- "/tui-refactor <path-to-codebase>"

Disambiguation:
- If there is **no code yet**, only a spec → use `tui-design`.
- If the user only wants **bugs and findings**, not a redesign → use
  `tui-validator`.
- If a working codebase exists and the user wants it to look/feel better →
  **this skill**.

If the user has not pointed at a codebase, ask which directory to use. Do not
guess.

## What it produces

A structured set of documents in a per-run workspace, plus a canonical copy of
the plan at the codebase root:

```
~/.cache/tui-refactor/<tui-name>/<UTC-timestamp>/
├── meta.json                 # tui path, framework, ui-source files, tools used
├── 01-current-design.md      # reverse-engineered: how it looks/behaves TODAY
├── 02-gap-analysis.md        # design smells found, each with severity + evidence
├── 03-target-design/
│   ├── 00-overview.md         # target IA, screen tree, principles, open questions
│   ├── 01-screens/            # one optimized wireframe file per screen
│   ├── 03-components.md       # target component library
│   ├── 04-keybindings.md      # target keybinding scheme + conflict audit
│   ├── 06-style-guide.md      # palette, borders, glyphs, density, capability tiers
│   ├── 07-states.md           # empty/loading/error/disabled/confirm patterns
│   ├── 08-navigation-map.md   # how the user moves between screens/modes
│   └── 09-accessibility.md    # low-vision/no-color/keyboard-only coverage
│   #  02 (modals) and 05 (flows) are intentionally reserved — see note below
├── 04-refactor-plan.md       # file-level migration plan (the deliverable)
├── keybindings.json           # extracted current keymap: key/action/context/source
├── findings.json             # structured smells (schema in references/refactor-planning.md)
├── plan-items.json            # structured implementation plan items
├── before/                   # screenshots/captures of the CURRENT UI (if captured)
└── REFACTOR_PLAN.md          # assembled workspace handoff copy
```

When Phase 7 finishes, drop a **canonical copy** of `04-refactor-plan.md` at the
**root of the refactored codebase** as `TUI_REFACTOR.md` (i.e.
`<tui_cwd>/TUI_REFACTOR.md`). That is the file the user opens. Screenshot links
use **absolute filesystem paths** so the codebase copy renders identically. If
the codebase root is unwritable, warn on stderr and keep only the workspace
copy — never abort.

## High-level workflow

Seven phases. **Run them in order**; each feeds the next. Reference files in
`references/` give deeper guidance — load them lazily, only the one the current
phase needs.

```
1. Discover        → what is this TUI, which framework, where does UI code live
2. Reverse-design  → reconstruct the CURRENT design from code (+ live capture)
3. Diagnose        → score the current design against the principles, list smells
4. Clarify         → ask high-impact questions, apply safe defaults otherwise
5. Target design   → produce the optimized design (wireframes, components, keys…)
6. Refactor plan   → map target → real files, ordered, incremental, behavior-safe
7. Review & hand off → canonical plan at codebase root, summary, validation route
```

Use the editing tools available in the current runtime for every file you
create or change. Prefer the bundled scripts below for deterministic setup and
assembly. Do not write project source code while this skill is active; its only
writes are the workspace docs and the `TUI_REFACTOR.md` plan at the repo root.
Never use shell heredocs as an editing shortcut when a proper edit/write tool is
available.

## Bundled scripts

Use these scripts when available; they reduce boilerplate but do not replace
the design judgment in the phases below.

> **Platform.** These scripts assume **Linux with GNU coreutils** (GNU
> `find -printf`, `date -u`, `mktemp --suffix`) and `ripgrep` for the best
> detection (a `grep`/`find` fallback runs without it). They are not tested on
> macOS/BSD; on those hosts do the phases by hand or run inside a Linux
> container. Live capture additionally reuses `tui-validator`, which needs
> Wayland.

- `scripts/tui-refactor-init.sh <codebase-root> [--name <name>] [--workspace-root <dir>]`
  creates the run workspace, directory skeleton, initial `meta.json`, and copies
  markdown templates into place.
- `scripts/tui-refactor-detect.sh <codebase-root>` detects the likely framework,
  candidate UI source files, and launch hints as JSON. Merge its output into
  `meta.json` after reviewing it.
- `scripts/tui-refactor-assemble-report.sh <workspace> <codebase-root>` assembles
  `REFACTOR_PLAN.md` in the workspace and writes the canonical
  `<codebase-root>/TUI_REFACTOR.md` if the root is writable.

## Phase 1 — Discover

Goal: understand the target before reasoning about its design.

1. Resolve the codebase root and the **framework**. Detect from manifest files:
   `go.mod` + a `bubbletea`/`tview`/`tcell` import → Bubble Tea/Go; `pyproject`
   + `textual`/`rich` → Textual/Python; `Cargo.toml` + `ratatui`/`crossterm` →
   Ratatui/Rust; `package.json` + `ink`/`blessed` → Ink/Node; `*.c` + `ncurses`.
   See `references/framework-map.md`.
2. Locate the **UI source files** — the view/render/layout/keymap code, not the
   business logic. `references/framework-map.md` lists the idiomatic file
   locations and symbol names per framework (`View()`/`Update()` for Bubble
   Tea, `compose()`/`CSS` for Textual, `draw`/`render` for Ratatui, etc.).
   Record the file list in `meta.json`.
3. Read project docs (README, AGENTS.md, CLAUDE.md, docs/) for documented
   keybindings, screens, modes, and launch instructions.
4. Determine how to build/launch it (for Phase 2 live capture): raw binary at
   root, `build/`, `bin/`, `target/release/`, Makefile `build`/`run`/`tui`.

If launching requires arguments (a file, a config), note it — you'll ask the
user once in Phase 2 before any live capture.

## Phase 2 — Reverse-design the current UI

Reconstruct **how the TUI looks and behaves today** into `01-current-design.md`.
Combine two sources of truth:

**Static (always):** read the UI source files located in Phase 1 and extract:
- the screen/view inventory (every distinct view, modal, panel, mode)
- the layout of each (what regions exist, how they size on resize)
- the current keybinding map (from the keymap/switch/match on key events) with
  `{key, action, context}` triples — write to `keybindings.json`
- color/style usage (hard-coded colors? semantic roles? truecolor assumptions?)
- state handling (does it render empty/loading/error states, or only the happy
  path?)

**Live (reuse tui-validator when available):** if the tui-validator scripts
exist, prefer them over ad-hoc tmux automation. The three TUI skills normally
install side by side, so resolve the sibling **relative to this skill's own
directory first** — `<dir-of-this-SKILL.md>/../tui-validator/scripts/` — before
trying global locations. Check these locations in order:
`../tui-validator/scripts/` (relative to this skill),
`~/dev/skills/tui-validator/scripts/`, `~/.agents/skills/tui-validator/scripts/`,
then `~/.codex/skills/tui-validator/scripts/`. Run
`tui-check-prereqs.sh` when present; live Wayland capture is useful, but a
headless Chromium screenshot fallback is also acceptable. Drive the TUI to
capture ground-truth `before/` screenshots of each screen at the default
geometry (80×24) and one wide size. This anchors the redesign in what the user
actually sees. If scripts are missing, screenshot backends are unavailable, or
launch needs inputs the user will not provide → **skip live capture and say so
in the report**; the static reconstruction is enough to proceed.

Output `01-current-design.md`: a faithful, non-judgmental snapshot. Save
opinions for Phase 3.

## Phase 3 — Diagnose (gap analysis)

Score the current design against the principles in
`references/design-principles.md` and the smell catalogue in
`references/audit-heuristics.md`. For each problem, write a finding to
`findings.json` (schema in `references/refactor-planning.md`) and a section in
`02-gap-analysis.md` with:

- **severity** — `blocker / major / minor / cosmetic`
- the **principle violated** (e.g. "color-only state", "modal-heavy flow",
  "inconsistent keys", "no status bar", "frozen UI during async")
- **evidence** — the source location (`file:line`) and/or a `before/` screenshot
- the **impact** on the user
- a **direction** for the fix (detailed later in the plan, not here)

Lead with the smells that hurt usability most. This is the bridge between "what
is" and "what should be".

## Phase 4 — Clarify with the user

This is a first-class phase, not a formality. A refactor of a *working* UI is
opinionated and reversible decisions belong to the user. Ask only the questions
that materially change the plan: batch up to **three** high-impact questions per
round. If the runtime offers a structured user-input tool, use it with a
recommended option; otherwise ask concise plain-text questions. When a safe
default is obvious, proceed and record the assumption in `meta.json` and
`03-target-design/00-overview.md` instead of blocking.

Cover at least:

- **Scope** — whole app, or specific screens/areas? (Recommend: start with the
  highest-traffic screens.)
- **Ambition** — conservative cleanup (preserve structure, fix smells) vs bold
  redesign (rethink IA/layout)? This sets how far Phase 5 goes.
- **Keybindings & muscle memory** — may existing shortcuts change, or must they
  be preserved? Re-keying a tool someone uses daily is costly; flag every
  proposed key change.
- **Aesthetic direction** — dense vs spacious; border style (single/double/
  rounded); minimal vs decorated; color philosophy.
- **Hard constraints** — must-keep behaviors, framework lock-in, minimum
  terminal size to support, truecolor vs 256/16-color targets, i18n needs.
- **What bugs them most** — the single change that would make them happiest.

Record answers in `meta.json` and let them steer Phase 5. If an answer
contradicts a best practice, surface the trade-off rather than silently
overriding the user.

## Phase 5 — Target design

Produce the optimized design under `03-target-design/`, sized to the ambition
the user chose in Phase 4. Reuse the design knowledge in `references/` (and, if
the `tui-design` skill is installed, its richer catalogs). The three TUI skills
normally install side by side, so resolve the sibling **relative to this skill's
own directory first** — `<dir-of-this-SKILL.md>/../tui-design/references/` —
before trying global locations. Check
`../tui-design/references/` (relative to this skill),
`~/dev/skills/tui-design/references/`, `~/.agents/skills/tui-design/references/`,
then `~/.codex/skills/tui-design/references/` for `layout-patterns.md`,
`component-library.md`, `interaction-patterns.md`, and `inspiration.md`.

Deliver:

1. `00-overview.md` — target information architecture, screen tree, the design
   principles being applied, and open design questions. Apply the **5±2 rule**
   for top-level peers.
2. `01-screens/<n>-<slug>.md` — one optimized wireframe per screen, ASCII art at
   minimum 80×24, box-drawing chars `┌─┐│└┘├┤┬┴┼`, real-looking sample content,
   focused element marked `▶`/`[BRACKETS]`, a status bar with contextual key
   hints, and the empty/loading/error/populated states. Where it helps, show a
   **before → after** pair so the change is obvious.
3. `03-components.md` — the target component library (lists, tables, inputs,
   modals, status bar, help overlay) with renderings, states, and keybinding
   contracts.
4. `04-keybindings.md` — the target keybinding scheme + a **conflict audit**.
   Provide vim AND arrow equivalents; reserve `?` `q` `:` `/` `esc`. Mark every
   key that **changes** vs the current map (from Phase 2) so muscle-memory cost
   is explicit. See `references/keybinding-conventions.md`.
5. `06-style-guide.md` — semantic color roles (`info/success/warning/danger/
   muted/accent/focus`), terminal capability tiers with fallbacks, border usage
   (focused=double, idle=single), glyph set with ASCII fallbacks, density,
   resize floor ("terminal too small"), alternate-screen-buffer usage.
6. `07-states.md` — canonical empty / loading / error (recoverable + fatal) /
   disabled / stale / confirm patterns. Confirmations default-focus the SAFE
   choice, never Delete/Overwrite.
7. `08-navigation-map.md` — how the user moves between screens/modes: the edges
   of the screen tree, back-stack behavior, and where each global key lands.
8. `09-accessibility.md` — low-vision, no-color, and keyboard-only coverage:
   never rely on color alone (pair with glyph/label), a 16-color and
   monochrome fallback, and a full keyboard path to every action. When the
   ambition is a **bold redesign**, this file is **required** and its summary
   must also appear in `00-overview.md`; for a conservative cleanup it may be
   `n/a — <reason>`.

> **File numbering.** The `03-target-design/` tree skips `02` and `05` on
> purpose: `02` is reserved for a modals catalogue and `05` for user-flow
> diagrams, both optional and only added when the target design needs them.
> The gaps are intentional, not missing files.

Honor Phase 4: don't re-key what the user asked to preserve; don't over-redesign
when they asked for a cleanup.

## Phase 6 — Refactor plan (the deliverable)

This is what makes the skill a *refactor*, not a *design*. In
`04-refactor-plan.md`, map the target design onto the **actual files** found in
Phase 1. Also write the same plan items to `plan-items.json` using the schema in
`references/refactor-planning.md`. The plan must be executable by someone who
has not read this whole workspace. Each plan item includes:

- **what changes** — the concrete UI change (e.g. "extract the duplicated
  status-bar render into a shared component")
- **where** — exact files and symbols (`internal/ui/view.go: View()`,
  `app/screens/profiles.py: compose()`)
- **how** — framework-idiomatic approach (Bubble Tea: lift state into the model
  and render in `View()`; Textual: a reusable `Widget` + CSS; Ratatui: a
  `render_*` fn + `Layout` constraints)
- **ordering** — sequence the items so each step leaves the TUI buildable and
  runnable; foundational/shared changes (theme, status bar, key map) before
  per-screen changes
- **behavior preservation** — what must NOT change (data, side effects, exit
  codes), and which keybindings move
- **effort & risk** — rough size (S/M/L) and blast radius
- **validation** — how to confirm the step worked, ideally by running
  `tui-validator` on the result and diffing `before/` vs after

Group items into incremental, mergeable milestones (e.g. M1 "design system
foundation", M2 "navigation & status bar", M3 "per-screen rework"). Recommend a
safe order. Do not write any source code — describe the change precisely enough
that an implementer can do it.

## Phase 7 — Review & hand off

Verify before declaring done:

- Every screen in `01-current-design.md` has a target design or an explicit
  "out of scope (per Phase 4)" note.
- Every smell in `findings.json` is addressed by a plan item or explicitly
  deferred.
- Every keybinding change is flagged in both `04-keybindings.md` and the plan.
- All wireframes fit 80×24; IA, keybindings, and plan are mutually consistent.
- Open questions the user must still answer are listed in
  `03-target-design/00-overview.md`.

Then write the canonical `TUI_REFACTOR.md` at the codebase root with, in this
order: a 10-second **summary** (TUI, framework, ambition chosen, smell counts by
severity, milestone overview), the **gap analysis**, the **target design**
(linked or inlined), the **refactor plan** with milestones, a **before gallery**
if captured, and a **next step**: "to implement, execute this plan; to verify
afterwards, run tui-validator and diff against `before/`." Use
`scripts/tui-refactor-assemble-report.sh` if available, then review the output
for broken links and missing sections.

Tell the user plainly that no source files were modified — the output is a plan.

## Safety guidelines

- This skill **does not modify project source code**. If the user later asks you
  to implement the plan, that is a separate action they must explicitly request.
- Prefer launching against demo data, a temporary project, a read-only fixture,
  or a disposable config directory. Record the chosen safety mode in `meta.json`.
- During Phase 2 live capture, treat the TUI as in tui-validator: never send
  destructive keys (`d`/`D`/`x`/`Delete`/`Ctrl-K`/`Ctrl-W`) without explicit
  opt-in; the TUI runs as the current user and may shell out.
- If live capture is used, always tear the tmux session down (tui-validator's
  `tui-cleanup.sh`) even on abort.
- Don't invent answers to design decisions the user owns — ask (Phase 4).

## Reference files

Load only when entering the matching phase:

- `references/framework-map.md` — detect the framework, locate UI source files,
  idiomatic refactor entry points per framework. (Phases 1, 6)
- `references/design-principles.md` — the optimized standard: the principles and
  anti-patterns the target design must satisfy. (Phases 3, 5)
- `references/audit-heuristics.md` — catalogue of TUI design smells and how to
  spot each in source/screenshots. (Phase 3)
- `references/keybinding-conventions.md` — canonical key conventions and the
  action-verb vocabulary. (Phase 5)
- `references/refactor-planning.md` — how to write the migration plan + the JSON
  schemas for `findings.json` and the plan items. READ BEFORE WRITING EITHER
  JSON FILE. (Phases 3, 6)

## Templates

Copy a template; fill it in. If a required section truly does not apply, write
`n/a — <reason>` so the reviewer sees it was considered.

- `templates/current-design.md` — Phase 2 reverse-design skeleton.
- `templates/gap-analysis.md` — Phase 3 diagnosis skeleton.
- `templates/target-design.md` — Phase 5 overview/screen skeleton.
- `templates/refactor-plan.md` — Phase 6 migration-plan skeleton.

---
name: tui-design
description: >-
  Design comprehensive UI/UX for terminal user interfaces (TUIs) from technical specification files (like SPECS.md). Produces information architecture, ASCII wireframes per screen, navigation map, keybinding scheme, component library, user flows, style guide, accessibility plan, and state patterns. Use when the user wants to design, mock up, wireframe, plan, or document the UI of a TUI/CLI app from a spec — triggers include "design TUI", "TUI wireframe", "TUI from spec", "TUI UX", "projetar TUI", "TUI mockup", "design a TUI / interactive CLI", "wireframe terminal app", and /tui-design. Trio disambiguation: to design a greenfield TUI from a spec use tui-design; to redesign an existing TUI's codebase use tui-refactor; to audit bugs/rendering of an existing TUI use tui-validator.
---

# tui-design

Design comprehensive UI/UX for terminal user interfaces from technical specifications.

This skill turns a functional spec (what the app does) into a complete UX package (how it looks and feels in a terminal): information architecture, screen wireframes, navigation map, keybindings, component library, flows, style guide, accessibility plan, and state patterns.

The skill is framework-agnostic. It produces designs that an implementer can realize in Bubble Tea (Go), Textual (Python), Ratatui (Rust), Notcurses (C), Ink (Node), or any other TUI runtime.

## When to invoke

Invoke when the user asks to design, mock up, wireframe, or plan a TUI/CLI app from a functional specification. Typical triggers:

- "Design the TUI for this app"
- "Create wireframes for these specs"
- "Plan the UX of this TUI from SPECS.md"
- "Projetar a UI de uma TUI a partir de uma spec"
- "/tui-design <path-to-spec>"

If the user has not pointed to a spec file, ask which file(s) to use as input. Do not guess.

## What it produces

A structured set of design documents under `tui-design-output/` (or a user-specified path) in the current project:

```
tui-design-output/
├── 00-overview.md              # exec summary, principles, IA, screen tree, open questions
├── 01-screens/
│   ├── 01-<screen-slug>.md     # one file per major screen
│   └── ...
├── 02-modals/
│   ├── <modal-slug>.md         # one file per modal/overlay
│   └── ...
├── 03-components.md            # shared component library
├── 04-keybindings.md           # full keybinding map + conflict audit
├── 05-flows.md                 # critical user journeys
├── 06-style-guide.md           # color palette, borders, glyphs, typography, density
├── 07-states.md                # error/loading/empty/disabled patterns
├── 08-navigation-map.md        # screen-transition graph + modal stack rules
└── 09-accessibility.md         # keyboard-only audit, color-blind plan, screen-reader notes
```

If the user names a target framework (Bubble Tea, Textual, Ratatui, etc.), add a brief framework-mapping appendix per screen but keep the core docs portable.

## Workflow

Follow the phases in order. Do not skip; do not pre-render output before understanding the spec.

### Phase 1 — Read and Map the Spec

1. Read the spec file(s) end to end. If multiple, read all.
2. Build an internal model of:
   - **Domain entities** (data types and their lifecycles)
   - **Functional areas** (top-level concerns — tabs, sections, sub-apps)
   - **Operations** (CRUD, launch, kill, search, import/export, etc.)
   - **States** (loading, error, empty, populated, processing, conflicted, ...)
   - **Flows** (sequences of user actions to accomplish a goal)
   - **External dependencies** (file system, network, processes)
3. List every screen, modal, panel, and state mentioned (explicitly or implicitly). When in doubt, add the screen — extra scoping is cheaper than a missed screen.

### Phase 2 — Information Architecture

Pick ONE top-level navigation pattern based on the spec:

- **Tab-based** (top horizontal tabs) — 3 to 6 peer areas of equal weight (k9s, gh dash).
- **Sidebar + main** — 5+ areas, or hierarchy with sub-items.
- **Single-screen with modes** — editor-like apps.
- **Command palette + screens** — action-centric, many screens (vim-like).

Enumerate every screen and modal. Group them into the chosen IA. Produce a **screen tree** in `00-overview.md`.

Apply the **5±2 rule**: never more than five top-level peers at any level. Group if the spec implies more.

### Phase 3 — Per-Screen Design

For every screen, produce a file under `01-screens/` using `templates/screen.md`. Each file MUST include:

1. **Purpose & context** (one paragraph)
2. **Wireframe** in ASCII art, sized at minimum 80 columns × 24 rows. Use box-drawing chars `┌─┐│└┘├┤┬┴┼` for borders.
3. **Layout description** (what each panel is, its size policy on resize, minimum size)
4. **Components used** (referencing `03-components.md`)
5. **States** — wireframe each of: empty, loading, error (recoverable + fatal), populated, plus any spec-specific states
6. **Contextual keybindings** (table)
7. **Focus order** and focus-cycling rules
8. **Edge cases** (long content, slow data, conflicting inputs, terminal too small)

Wireframing rules:
- Show real-looking content (sample names, numbers), not lorem ipsum.
- Mark the focused element with `▶` prefix or `[BRACKETS]`.
- Always include a status bar with keybinding hints (e.g., `q quit  ? help  / search`).
- For dynamic content (lists, tables), show 3–5 sample rows + ellipsis to imply scrolling.

### Phase 4 — Component Library

In `03-components.md`, document every reusable widget. For each:

- Name + purpose
- ASCII rendering (with variants and states)
- Properties (selectable, multi-select, filterable, virtualized, etc.)
- Keybinding contract (what keys it owns when focused)
- Where it is used in the app
- Accessibility note (color-independence, focus visibility, keyboard reachability)

Refer to `references/component-library.md` for the canonical catalog.

### Phase 5 — Global UX System

In `06-style-guide.md`, define:

- **Color palette** with semantic roles only: `info / success / warning / danger / muted / accent / focus`. Specify dark- and light-friendly choices. Never rely on color alone — pair with shape, prefix, or brightness.
- **Terminal capability tiers**: truecolor / 256 / 16 / monochrome — explicit fallbacks for each. Detect at runtime.
- **Border styles** (single vs double vs rounded) and when each is used (focused = double; idle = single).
- **Typography emphasis** (bold = labels/headers; dim = secondary/timestamps; reverse = selection).
- **Glyph set** — small curated Unicode set (✓ ✗ ▶ ◆ ● ○ ★ ⏵ ⏸ ⚠ …). Provide ASCII fallbacks.
- **Density** — line spacing and padding inside panels.
- **Resize behavior** — minimum size, what hides first, hard floor where the UI shows "terminal too small".
- **Alternate screen buffer** — use it so quitting restores the user's previous shell view; clean cursor/attrs on exit and on crash.

In `07-states.md`, document the canonical look of:

- Empty list/table (with CTA)
- Loading (spinner vs skeleton vs progress bar — choose per latency)
- Error (recoverable vs fatal)
- Disabled controls
- Stale data warning
- Optimistic operation in flight
- Confirmation prompt (default focus on the safer choice — never on Delete/Discard/Overwrite)

### Phase 6 — Navigation Map

In `08-navigation-map.md`:

- Draw a directed graph (Mermaid OR ASCII) of every screen and the keys/actions that transition between them.
- List modal stack rules: which can stack, which dismiss others, which block global keys.
- Define focus-trap rules for modals.
- Define what `esc` does in every context.
- Define what `q` does in every context (and when it's intercepted to confirm unsaved state).

### Phase 7 — Keybinding Scheme

In `04-keybindings.md`:

- Tabulate global keys, per-screen keys, per-component keys.
- Run a **conflict audit**: no screen overloads a global key with a different meaning. List unavoidable contextual overrides explicitly.
- Provide both vim-style (h/j/k/l) AND arrow-key equivalents — never vim-only.
- Reserve `?` (help), `q` (quit), `:` (palette, if any), `/` (search), `esc` (cancel).

Refer to `references/keybinding-conventions.md` for the canonical conventions and the verb vocabulary.

### Phase 8 — User Flows

In `05-flows.md`, write step-by-step flows for the critical journeys implied by the spec. Each flow contains:

1. **Goal** (what the user is trying to accomplish)
2. **Trigger** (what initiates the flow)
3. **Steps** — numbered: screen, user action, system response
4. **Decision points** — branches and their conditions
5. **Cancel/error paths** — what happens when the user backs out or something fails

Prioritize happy path + critical errors. Do not enumerate every theoretical branch.

### Phase 9 — Accessibility Plan

In `09-accessibility.md`, address:

- **Keyboard-only audit**: every action reachable; tab order predictable; focus traps only in modals; no actions hidden behind mouse-only affordances.
- **Color-blind safety**: every color-coded state paired with a glyph, shape, or position. Test with deuteranopia/protanopia/tritanopia simulators.
- **Contrast tiers**: at minimum dark + light + high-contrast themes; verify against WCAG-like contrast ratios where applicable.
- **Screen reader friendliness**: stable text regions, no jarring full-screen redraws on minor updates, announce state changes through stable status-line text.
- **Internationalization**: handle multi-byte glyph widths (CJK, emoji); right-to-left scripts where the spec applies.
- **Reduced motion**: spinners and animations have a no-animation fallback (e.g., a static `[loading]` token).

### Phase 10 — Review Pass

After producing all files, verify:

- Every screen mentioned in the spec has a design file (or is explicitly noted as out-of-scope).
- Every keybinding is listed in `04-keybindings.md` and conflict-audited.
- Every component used in a screen is documented in `03-components.md`.
- Every state mentioned in the spec has a visual in `07-states.md` or in the relevant screen file.
- All wireframes fit 80×24.
- IA, navigation map, and flows are mutually consistent.
- Accessibility checklist in `09-accessibility.md` is filled in (not just templated).

If gaps remain, fill them. Report any items the spec genuinely under-specifies rather than inventing details silently — list under "Open Design Questions" in `00-overview.md`.

Finally, tell the user how to close the loop: once the design is implemented,
run **`tui-validator`** against the built TUI to audit rendering, keybindings,
input handling, and resize behavior against this design. If the app already had
code, **`tui-refactor`** produces a file-level migration plan from here.

## Design Principles (apply consistently)

The full standard — the 14 numbered principles and the anti-pattern catalogue —
lives in `references/design-principles.md` (shared verbatim with `tui-refactor`).
**Load that file when you start Phase 3 and Phase 5** and apply it. The
one-line reminders:

1. Human-first  2. Keyboard-first, mouse-optional  3. Discoverable (footer + `?`)
4. Consistent keys  5. Reversible (safe-default confirms)  6. Composable
7. Fast feedback (never freeze)  8. Respect the terminal (resize/ASCII/altscreen)
9. Color-blind safe  10. Avoid modal-heavy flows  11. Structure first, detail on demand
12. Status bar is sacred  13. Stable redraws  14. Empathic errors

The anti-patterns to avoid (color-only state, inconsistent keys, frozen UI,
defaulting to destructive, vim-only navigation, truecolor assumptions, …) are
enumerated in the same reference. Do not re-list them here.

## Inputs

If user provides one or more spec files, read all of them.
If user does not specify, ask which file(s) to use.

If the spec is in a non-English language, mirror its language for copy samples (button labels, status messages) but keep design-doc prose in the language the user has been writing.

## Outputs

Default output path: `./tui-design-output/`.
If it exists and is non-empty, ask: overwrite, write to a sibling `tui-design-output-<n>/`, or merge into existing.

Use Write/Edit tools for all file creation — never Bash heredocs.

## References (load on demand — don't read all up front)

- `references/layout-patterns.md` — catalog of TUI layout patterns with ASCII examples and selection criteria.
- `references/design-principles.md` — the 14 principles and anti-pattern catalogue the design must satisfy (shared with `tui-refactor`). Load at Phase 3 and Phase 5.
- `references/component-library.md` — canonical TUI widgets with renderings and contracts.
- `references/keybinding-conventions.md` — canonical key conventions and the action-verb vocabulary.
- `references/interaction-patterns.md` — confirmation, validation, async progress, undo, search-as-you-type, multi-select.
- `references/inspiration.md` — best-in-class TUIs and what to borrow from each.
- `references/accessibility.md` — keyboard-only, color-blind, screen-reader, i18n guidance.

## Templates

- `templates/overview.md` — top-level overview / IA skeleton.
- `templates/screen.md` — per-screen design doc skeleton.
- `templates/component.md` — per-component spec skeleton.
- `templates/flow.md` — per-flow narrative skeleton.

Copy a template; fill it in. Do not skip required sections — if one truly does not apply, write `n/a — <reason>` so the reviewer sees it was considered.

## Anti-Patterns to Avoid

See the anti-pattern catalogue in `references/design-principles.md` (shared with
`tui-refactor`). It is the single source — not duplicated here.

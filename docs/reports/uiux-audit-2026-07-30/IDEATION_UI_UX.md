# UI/UX Improvements Analysis Report

## Executive Summary

model-loader ships two interactive surfaces: a 5-tab Bubble Tea TUI and an
on-demand `configweb` HTTP surface (profile editor, backend editor, read-only
benchmark viewer). Both were inspected live — the TUI in a dedicated tmux
session at three geometries plus a full `NO_COLOR` pass, the web surfaces in
Chrome with DOM/accessibility measurement.

The design fundamentals are strong and should not be disturbed. `NO_COLOR` is
handled better than in most TUIs: active tab, sub-tabs, mode selector, proxy
status glyph, metric bars, and sparklines all carry ASCII/bracket fallbacks
(`[2 Server]`, `[Logs]`, `[+] RUNNING`, `####----`). Destructive confirms
default to Cancel and mark the choice with color-independent `>  Cancel  <`
plus an explicit `[→ Enter cancels]` line. The web editor passes every contrast
threshold measured (6.47–16.27:1), has real `:focus-visible` rings, a working
`aria-live` issue region, a genuine `@media (max-width:720px)` reflow, and
`prefers-reduced-motion` support. The 20×20 checkbox flagged in BUGS.md
UIUX-048 is confirmed fixed (82.4×44 enclosing hit target).

Seventeen findings survived verification. Five are high priority, and two of
those are functional, not cosmetic:

1. **The web profile editor silently discards edits.** Every one of the 14
   Essentials flags is rendered *twice* in the same `<form>` — once under
   Essentials, once under its numbered group — producing 28 duplicate element
   IDs and 22 duplicate field names. Both copies submit; the server keeps the
   first. Editing `ctx-size` in "2. Context and Generation" therefore saves the
   stale Essentials value. Reproduced end-to-end.
2. **The Server tab's Slots, Metrics, and Tokens/s surfaces are permanently
   dead** against current llama.cpp. `monitor.Slot.Client` is typed `string`
   but maps to `id_task`, which llama.cpp emits as a number. The decode fails,
   `fetchSlots` returns silently, and no event is ever emitted — so the UI shows
   "(no slot data yet)" and "(no metrics yet — first sample arrives within a few
   seconds)" indefinitely on a healthy backend whose `/slots` returns HTTP 200
   with data.

The remaining themes are: fixed column ceilings that starve identity columns
while leaving 40–80 terminal columns unused; a detail pane that cannot scroll,
putting a profile's ID/model/backend/args out of reach at 80×24; a
zero-result filter that renders a blank pane next to a contradictory
"15 profiles" count; and a benchmark viewer that bottoms out at 545 CSS px and
offers no filtering over 193 rows.

**Material limitations:** the Chrome `resize_window` tool does not apply in
this environment, so viewport-dependent evidence was gathered via same-origin
iframe probes (which do get their own viewport and honor media queries). The
backend editor, the Customize tab, the benchmark wizard, and HF search/download
flows were not exercised — see *Visual Inspection Limitations*.

**Numbering note:** `BUGS.md` already owns `UIUX-001`–`UIUX-048`. This report
continues at `UIUX-049` so ids can be transcribed without collision.

---

## Visual Inspection Log

**UI Type:** mixed (TUI + web)
**Environment:** local (operator workstation, dual RTX 3090)
**Browser/App/Terminal Tooling:** tmux 3.x (`uiux-audit-20260730`, analysis-owned);
Chrome via `claude-in-chrome` MCP (the installed `agent-browser` CLI is
non-executable in this environment — see Limitations)
**Launch Command:** `make build && ./bin/model-loader` (TUI); web surfaces
launched from the TUI (`e` on Profiles → `http://127.0.0.1:36411`, `W` on
Benchmark → `http://127.0.0.1:40795`)
**User Role / Data Fixture:** single operator, live local state — 15 profiles,
20 backends, 14 local models, 193 benchmark runs, 1 running llama-server
instance adopted at boot
**Screens/Journeys Inspected:** 17
**Viewports/Sizes Tested:** TUI 200×50, 160×45, 80×24 (each also under
`NO_COLOR=1`); web 1266×1191 real window, 1440/720/545/500/390/386/320 CSS px
via iframe probe

| Screen / Journey | Screenshot/Capture | Accessibility Snapshot | States Captured | Viewports/Sizes | Notes |
|---|---|---|---|---|---|
| TUI Profiles — list + detail | `.ideation/uiux-captures/profiles-default-200x50.txt`, `profiles-default-80x24.txt`, `profiles-nocolor-160x45.txt` | N/A (TUI) | default, filtering, zero-result, delete-confirm, web-edit modal | 200×50, 160×45, 80×24 | detail pane clipped at 80×24 |
| TUI Profiles — filter | `profiles-filter-empty-160x45.txt`, `profiles-filter-noresults-160x45.txt` | N/A | filter open, zero results | 160×45 | blank pane + wrong count |
| TUI Profiles — delete confirm | `profiles-confirm-delete-160x45.txt` | N/A | confirm | 160×45 | cancelled with `esc`; no data changed |
| TUI Server — instances + Logs | `server-default-200x50.txt`, `server-default-80x24.txt`, `server-nocolor-160x45.txt` | N/A | running | 200×50, 160×45, 80×24 | Profile/VRAM columns truncate at every width |
| TUI Server — Slots | `server-subview1-160x45.txt`, `server-slots-nocolor-160x45.txt` | N/A | empty (permanent) | 160×45 | reproduced in a fresh session |
| TUI Server — Metrics | `server-subview2-160x45.txt`, `server-metrics-after25s-160x45.txt` | N/A | empty after 25 s | 160×45 | instance uptime 4 h 26 m |
| TUI Server — History | `server-subview3-160x45.txt` | N/A | populated | 160×45 | full-width table — the good pattern |
| TUI Models — Library | `models-default-200x50.txt`, `models-default-80x24.txt`, `models-nocolor-160x45.txt` | N/A | default | 200×50, 160×45, 80×24 | fixed Size/Quant/Params widths starve Name |
| TUI Models — zero-result filter | `models-filter-noresults-160x45.txt` | N/A | empty | 160×45 | correct EmptyState — reference implementation |
| TUI Models — Downloads | `models-downloads-empty-160x45.txt` | N/A | populated + failed item | 160×45 | item labelled `.` |
| TUI Models — Discover | `models-discover-160x45.txt` | N/A | intro/empty | 160×45 | good empty state |
| TUI Backends — list + detail | `backends-default-200x50.txt`, `backends-default-80x24.txt`, `backends-filter-noresults-160x45.txt` | N/A | default, zero-result | 200×50, 160×45, 80×24 | ~40 rows of dead space at 200×50 |
| TUI Benchmark — dashboard | `benchmark-default-200x50.txt`, `benchmark-default-80x24.txt`, `benchmark-mode2-160x45.txt`, `benchmark-nocolor-160x45.txt` | N/A | default, alternate mode | 200×50, 160×45, 80×24 | names truncated to ambiguity |
| TUI Benchmark — run detail | `benchmark-detail-160x45.txt` | N/A | populated | 160×45 | duplicate tok/s card |
| TUI — help overlay | `help-overlay-200x50.txt` | N/A | open | 200×50 | no scroll indicator |
| Web — profile editor | `.ideation/uiux-screenshots/profile-editor-default-1440x900.jpg`, `profile-editor-search-duplicate-fields-1440x900.jpg`, `profile-editor-validation-error-1440x900.jpg`, `profile-editor-error-hidden-group-1440x900.jpg`, `profile-editor-mobile-390x844.jpg` | `.ideation/uiux-snapshots/profile-editor-a11y-1440x900.txt` | default, search, validation error, error-in-hidden-group, mobile | 1266×1191, 386 | duplicate-field submission proven |
| Web — benchmark viewer | `benchview-runs-1440x900-scaled.jpg`, `benchview-runs-overflow-390x844.jpg`, `benchview-compare-500x729.jpg`, `benchview-live-empty-500x729.jpg` | `.ideation/uiux-snapshots/benchview-reflow-and-structure.txt` | runs list, compare, live empty, narrow overflow | 1440, 720, 545, 500, 390, 320 | overflow floor measured at 545 px |

---

## Visual Inspection Limitations

None of the findings below are marked `[CODE-ONLY]` — every one carries a
capture, screenshot, or runtime measurement. These are coverage gaps, not
evidence gaps.

1. **`agent-browser` CLI unusable.** `/home/diogo/.npm-global/bin/agent-browser`
   symlinks into `~/.hermes/hermes-agent/node_modules/agent-browser/bin/`, where
   every platform binary is mode `644`. Invoking it returns `permissão negada`.
   Fixing the mode bit would modify an unrelated user install, so the audit fell
   back to the host's `claude-in-chrome` browser automation, as the skill
   permits. No finding depends on `agent-browser`.
2. **`resize_window` does not apply.** Three attempts (1440×900, 1500×960,
   1270×790) each returned success while `window.innerWidth` stayed at its prior
   value. Viewport-dependent evidence was therefore produced with same-origin
   iframe probes, which receive a real viewport and evaluate media queries
   correctly (verified: `iframe.contentWindow.innerWidth` reported 1440/720/545/
   500/390/320 as set). The desktop benchmark-viewer screenshot is a 1440 px
   iframe rendered at `scale(0.347)`; layout is faithful, text is small.
3. **Real 200 % browser zoom not exercised.** The tool blocks page-zoom
   shortcuts. Reflow was measured by viewport width instead, which is the
   equivalent test for WCAG 1.4.10 — no claim is made about zoom-specific
   rendering.
4. **One Chrome tab left open.** After clicking Cancel in the profile editor,
   tab `787798015` stopped responding to CDP (`Input.dispatchMouseEvent` timed
   out) and could not be closed; its server was already shut down, so the tab
   points at a dead port. The likely cause is the audit's own iframe probe
   re-loading a ~300-field document, not a product defect — no finding is
   raised from it. The second tab was closed cleanly.
5. **Not inspected:** the backend editor (`/backend/`), the profile editor's
   Customize tab (present in the DOM, never rendered visually), the benchmark
   wizard (`b` — would start a real GPU run), HF search (`s` — network),
   model download and delete flows, and the profile editor below 386 px. No
   claims are made about any of them.

---

## Issues Found

### High Priority

#### UIUX-049: Web editor renders every Essentials flag twice and silently discards edits made in the numbered group

**Category:** state
**Priority Rationale:** Editing a profile is the single most frequent
non-launch task, and this loses the edit without any error — the operator
believes the value was saved. Affects all 14 Essentials flags, including
`ctx-size`, `n-gpu-layers`, `cache-type-k/v`, `batch-size`, `ubatch-size`. The
failure is silent and direction-dependent, which makes it hard to notice and
easy to misattribute to the backend.
**Confidence:** confirmed

**Evidence:**
- Screenshot: `.ideation/uiux-screenshots/profile-editor-search-duplicate-fields-1440x900.jpg` — searching `ctx-size` reveals two separate editable inputs, one under **Essentials** and one under **2. Context and Generation**, with no indication they are the same parameter. The intervening "1. Model Loading  9 options" header renders with zero fields.
- Measurement: `.ideation/uiux-snapshots/profile-editor-a11y-1440x900.txt` — 28 duplicate element IDs; 22 duplicate form-field names; `document.getElementById('arg-ctx-size').labels.length === 2`.
- Reproduction (no save performed): setting the *second* `arg.ctx-size` input to `131072` leaves the Essentials copy at `262144`; `new FormData(form[0]).getAll('arg.ctx-size')` returns `["262144","131072"]`.
- Code: `internal/service/configweb/handlers.go:41-42` — `d.Args[…] = vs[0]` keeps the first value, i.e. the stale Essentials copy.
- Code: `internal/service/configweb/assets/templates/configure.gohtml:118-121` — the `{{range .Groups}}` / `{{range .Fields}}` loop emits `id="arg-{{.Flag}}"` and `name="arg.{{.Flag}}"` once per group membership, and Essentials duplicates the flags rather than aliasing them.

**Affected User Journey / Screens:**
- Profiles tab → `e` edit / `n` new → web editor → change a parameter in a numbered group → Save.

**Affected Code:**
- `internal/service/configweb/assets/templates/configure.gohtml`
- `internal/service/configweb/handlers.go`
- `internal/service/configweb/viewmodel.go`

**Current State:**
Essentials is materialized as a full copy of its member fields rather than a
view onto them. Both copies live in the same `<form>`; `x-show` hides one with
`display:none`, which does not exclude it from submission. The server keeps the
first occurrence, so Essentials always wins and an edit made anywhere else is
dropped without a message.

**Proposed Change:**
Render each flag's input exactly once and make Essentials a *filter* rather
than a duplicate: keep one `#arg-<flag>` node per flag, and have the Essentials
tab's `x-show` predicate match `activeGroup==='Essentials' && isEssential(flag)`
in addition to its own group. Concretely, emit fields from a single flat
`{{range .Fields}}` pass carrying a `data-groups` attribute listing every group
the flag belongs to, and switch the group panels to filter on that attribute.
This removes all 28 duplicate ids and all 22 duplicate names in one change, and
makes `vs[0]` unambiguous.

**User Benefit:**
Edits save where they were made. The parameter search stops showing the same
field twice, and each `label[for]` resolves to exactly one input.

**Risks / Trade-offs:**
The Essentials panel stops being a contiguous DOM block, so any CSS or
`editor.js` logic keyed to `#group-panel-essentials` containment needs
revisiting. `configweb/render_test.go` and `viewmodel_test.go` assert on group
structure and will need updating. Add a regression asserting
`len(form fields with duplicate names) == 0` for a schema whose Essentials seed
overlaps a numbered group.

**Verdict:** Implement now
**Estimated Effort:** medium

---

#### UIUX-050: Server tab Slots, Metrics, and Tokens/s never populate — `/slots` decode fails on `id_task`

**Category:** state
**Priority Rationale:** Three of the Server tab's four sub-views are inert
against current llama.cpp, and the placeholder copy actively promises data that
will never arrive. The Server tab is the operator's only live view of a running
backend. The failure is silent — no error surface, no log line, no degraded
state.
**Confidence:** confirmed

**Evidence:**
- Capture: `.ideation/uiux-captures/server-subview1-160x45.txt` — Slots shows `(no slot data yet)` while the instance has been up `4h25m`.
- Capture: `.ideation/uiux-captures/server-metrics-after25s-160x45.txt` — Metrics still shows `(no metrics yet — first sample arrives within a few seconds)` after a 25-second wait; `Tokens/s` reads `--`.
- Capture: `.ideation/uiux-captures/server-slots-nocolor-160x45.txt` — reproduced in a *fresh* TUI process, 12 s after opening the sub-view.
- Runtime: `curl http://127.0.0.1:44733/slots` → `200`, body `[{"id":0,"n_ctx":262144,"speculative":true,"is_processing":false,"id_task":13259,…}]`. The endpoint is healthy and serving data.
- Reproduction: unmarshalling that exact payload into `monitor.Slot` yields `json: cannot unmarshal number into Go struct field Slot.id_task of type string  slots=1`.
- Code: `internal/service/monitor/monitor.go:48` — `Client string \`json:"id_task"\`` (llama.cpp emits a number).
- Code: `internal/service/monitor/slots.go:89-92` — `if err := json.NewDecoder(resp.Body).Decode(&slots); err != nil { return }` discards the error and emits nothing.
- Code: `internal/service/monitor/subscribe.go:141-144` — the metrics aggregate is fed exclusively by `pc.agg.observeSlots` on `SourceSlots` events, so no slot event means no metrics sample, which is why `Tokens/s` also stays `--`.

**Affected User Journey / Screens:**
- Server tab → `v` cycle → Slots; Server tab → `v` cycle → Metrics; the `Tokens/s` column of the Running-instances table.

**Affected Code:**
- `internal/service/monitor/monitor.go`
- `internal/service/monitor/slots.go`
- `internal/ui/pages/server_subviews.go`

**Current State:**
`fetchSlots` performs a strict decode into `[]Slot`. Because `Client` is a
`string` bound to a numeric JSON field, the decode always fails, the poller
returns early, and no `SourceSlots` event reaches the page. `renderSlots`
(`server_subviews.go:168-170`) falls back to `(no slot data yet)` and
`renderMetrics` (`server_subviews.go:180-181`) to the "within a few seconds"
copy — both indistinguishable from a genuinely-just-started instance.

**Proposed Change:**
Two changes, both required:
1. Change `Slot.Client` to `json.Number` (or `any` rendered via `fmt.Sprint`)
   so numeric and string `id_task` both decode. Add a golden test that decodes a
   captured llama.cpp `/slots` payload.
2. Surface decode failures instead of swallowing them: set `subState.subErr`
   from the poller and let the existing `subErr` branches
   (`server_subviews.go:135-136`, `:177-178`) render the real reason. Time-box
   the "arrives within a few seconds" copy — after ~15 s with no sample, switch
   to `components.EmptyState("Slot data unavailable", "<reason>")`, matching the
   pattern the Models tab already uses.

**User Benefit:**
Restores live slot occupancy, context usage, tokens/s and req/s for every
running backend, and guarantees that when a monitor source is broken the
operator is told rather than left waiting.

**Risks / Trade-offs:**
Widening the field type means any consumer formatting `Client` must handle a
non-string. The decode is also all-or-nothing per response, so decode into
`[]json.RawMessage` first and skip malformed entries individually — otherwise
one bad slot keeps blanking the whole view.

**Verdict:** Implement now
**Estimated Effort:** small

---

#### UIUX-051: A filter with no matches renders a blank pane and a count that contradicts it (Profiles, Backends)

**Category:** state
**Priority Rationale:** Filtering is a primary navigation action on both tabs
(`/`, advertised in the footer of every capture). A zero-match filter produces
a screen with no message at all, next to a footer asserting a non-zero item
count — the operator cannot distinguish "no matches" from "the app broke". The
correct pattern already exists in this codebase on the Models tab.
**Confidence:** confirmed

**Evidence:**
- Capture: `.ideation/uiux-captures/profiles-filter-noresults-160x45.txt` — typing `zzzz` yields 34 blank rows, an empty detail pane, and the footer `page 1/1 · 15 profiles`.
- Capture: `.ideation/uiux-captures/backends-filter-noresults-160x45.txt` — same shape, footer `page 1/1 · 20 backends`.
- Capture (contrast): `.ideation/uiux-captures/models-filter-noresults-160x45.txt` — the Models tab does it correctly: `No models match the current filter` / `Press [esc] to clear filter, or [/] to edit filter`.
- Code: `internal/ui/pages/list_paginator.go:12` — `n := len(l.Items())` counts the *unfiltered* item set; `bubbles/list` exposes `VisibleItems()` for the filtered set.
- Code: `internal/ui/pages/profiles.go:174` and `internal/ui/pages/backends_view.go:26` are the two call sites.
- Code (reference implementation): `internal/ui/pages/models.go:295`.

**Affected User Journey / Screens:**
- Profiles tab → `/` → any non-matching term.
- Backends tab → `/` → any non-matching term.

**Affected Code:**
- `internal/ui/pages/list_paginator.go`
- `internal/ui/pages/profiles.go`
- `internal/ui/pages/backends_view.go`

**Current State:**
Both pages use `bubbles/list` filtering, so the rendered list collapses to zero
rows while `listPaginationLine` keeps reporting the full item count. Neither
page renders an empty state for the filtered-empty case; `ProfilesPage.detailView`
only emits `components.EmptyState` when the *unfiltered* list is empty
(`profiles.go:240-242`).

**Proposed Change:**
1. In `listPaginationLine`, count `l.VisibleItems()` and return `""` when it is
   zero, so the contradictory footer disappears.
2. In both pages' `View`, when `FilterState() != Unfiltered` and
   `len(l.VisibleItems()) == 0`, render
   `components.EmptyState("No profiles match the current filter", "Press [esc] to clear filter, or [/] to edit filter")`
   — reusing the Models copy verbatim, with the noun swapped.

**User Benefit:**
The operator immediately learns the filter matched nothing and how to clear it,
instead of facing a blank screen with a misleading count.

**Risks / Trade-offs:**
None material. `profiles_test.go` and `backends_test.go` assert on rendered
output and should gain a zero-match case.

**Verdict:** Implement now
**Estimated Effort:** trivial

---

#### UIUX-052: A validation error in a non-active group blocks Save with no way to reach the offending field

**Category:** usability
**Priority Rationale:** Save is hard-blocked while any error exists
(`handlers.go:162-165`), and with 18 groups the offending field can be
arbitrarily far from view. The operator is told *what* is wrong but given no
navigation to *where*, and the Save button gives no hint it will be rejected.
This turns a one-keystroke fix into a manual hunt through 18 panels.
**Confidence:** confirmed

**Evidence:**
- Screenshot: `.ideation/uiux-screenshots/profile-editor-error-hidden-group-1440x900.jpg` — with `threads-batch = -9` and Essentials active, the footer reads `threads-batch: expected >= 0, got -9` while the sidebar entry "3. Threads and CPU" shows only its option count `13`, no error marker. Save remains styled as the enabled primary action.
- Measurement: `.ideation/uiux-snapshots/profile-editor-a11y-1440x900.txt` — offending field `offsetParent === null` (not rendered); `#issues` contains 0 links or buttons.
- Screenshot (working case): `profile-editor-validation-error-1440x900.jpg` — when the error *is* in the active group, the inline treatment is good: red border, per-field message, aggregated `role="status"` footer.
- Code: `internal/service/configweb/handlers.go:162-165` — `if len(rep.Errors) > 0 { renderIssues(w, rep); return }`.
- Code: `internal/service/configweb/assets/templates/configure.gohtml:112` — group panels are gated by `x-show="search.trim()==='' ? activeGroup==='{{.Name}}' : true"`, so a non-active group's fields are not in the layout.

**Affected User Journey / Screens:**
- Profile editor → edit a value in one group → navigate to another group → Save.
- Backend editor is likely to share the shape (not inspected — see Limitations).

**Affected Code:**
- `internal/service/configweb/assets/templates/configure.gohtml`
- `internal/service/configweb/assets/static/editor.js`
- `internal/service/configweb/assets/static/app.css`

**Current State:**
`decorateIssues` in `editor.js` already attaches `data-field` decorations to the
matching input. Because the input is inside a hidden panel, the decoration is
applied but invisible, and nothing propagates up to the sidebar or the footer.

**Proposed Change:**
1. Make each footer issue a button that sets `activeGroup` to the group owning
   the field, then scrolls it into view and focuses it. The field's group is
   already derivable — emit `data-group="{{$.Name}}"` alongside the existing
   `data-field` attribute.
2. Add an error count badge to the sidebar entry of any group holding an error,
   using the same `.badge` slot that currently shows the option count. This
   reuses the existing visual vocabulary rather than introducing a new one.
3. Reflect the blocked state on Save: when `#issues` holds errors, set
   `aria-disabled="true"` and the `.ghost` treatment on the Save button so the
   rejection is predicted rather than discovered.

**User Benefit:**
Turns "your save was rejected, go find it" into one click to the exact field.

**Risks / Trade-offs:**
Do not use a real `disabled` attribute — the operator must still be able to
press Save and hear why. The badge slot is currently the option count, so the
sidebar needs a small state machine (count normally, error count when errors
exist) rather than an extra element.

**Verdict:** Implement now
**Estimated Effort:** small

---

#### UIUX-053: Invalid fields never set `aria-invalid`, and error text is not in `aria-describedby`

**Category:** accessibility
**Priority Rationale:** WCAG 2.2 SC 3.3.1 (Error Identification) and 4.1.2
(Name, Role, Value). A screen-reader user who tabs onto an invalid field hears
its label and help text with no indication of the error or its reason; the only
error channel is the page-level `role="status"` region, which is announced once
and is not re-reachable per field. The visual channel (red border + inline
`<small>`) has no programmatic equivalent.
**Confidence:** confirmed

**Evidence:**
- Measurement: `.ideation/uiux-snapshots/profile-editor-a11y-1440x900.txt` — with `ctx-size = -5` and the inline error rendered, `#arg-ctx-size` reports `aria-invalid: null` and `aria-describedby: "arg-ctx-size-help"` (help text only; the `<small class="field-error">` node carries the error and is not referenced).
- Screenshot: `.ideation/uiux-screenshots/profile-editor-validation-error-1440x900.jpg` — the error is visually unmistakable and programmatically absent.
- Code: `internal/service/configweb/assets/templates/configure.gohtml:120-124` — `aria-describedby="arg-{{.Flag}}-help"` is the only association emitted; `<small class="field-error"></small>` has no `id`.
- Code: `internal/service/configweb/assets/static/editor.js` — `decorateIssues` writes error text into the field slot without touching ARIA state.

**Affected User Journey / Screens:**
- Profile editor, any field failing validation. Same shape expected in the backend editor (not inspected).

**Affected Code:**
- `internal/service/configweb/assets/templates/configure.gohtml`
- `internal/service/configweb/assets/static/editor.js`

**Current State:**
Error presentation is purely visual plus one page-level live region. Nothing
marks the individual control as invalid.

**Proposed Change:**
Give the error node a stable id (`id="arg-{{.Flag}}-error"`). In
`decorateIssues`, when a field receives an error, set `aria-invalid="true"` and
append the error id to `aria-describedby`; on reset, remove `aria-invalid` and
restore `aria-describedby` to the help id alone. Mirror this in the `.warn`
path added by BUGS.md UIUX-041 (warnings should not set `aria-invalid`, only
extend `aria-describedby`).

**User Benefit:**
Assistive-technology users get the same per-field error identification sighted
users already get, and can re-read the reason by returning to the field.

**Risks / Trade-offs:**
`aria-describedby` must be rebuilt rather than overwritten, or the help text is
lost. Keep the existing `#issues` live region — it is the correct
summary channel and should not be replaced by per-field ARIA.

**Verdict:** Implement now
**Estimated Effort:** trivial

---

### Medium Priority

#### UIUX-054: Fixed column ceilings truncate identity while 40–80 terminal columns sit unused

**Category:** visual
**Priority Rationale:** Affects the two screens whose entire purpose is
telling similar things apart. On the benchmark leaderboard the truncation makes
rows genuinely indistinguishable — five separate runs render as
`Ornith AEON 35B-A3B Q…`. The operator's rig runs wide terminals, so the wasted
space is the common case, not an edge case.
**Confidence:** confirmed

**Evidence:**
- Capture: `.ideation/uiux-captures/benchmark-mode2-160x45.txt` at 160 cols — the name column stops at 21 characters while each row ends around column 76, leaving ~84 columns blank. `Ornith AEON 35B-A3B Q…` appears 5 times, `unlimited-ocr-sglang-…` twice, `qwen36-35b-a3b-iq4ks-…` twice, `qwen36-27b-exl3-45bpw…` twice.
- Capture: `.ideation/uiux-captures/server-default-200x50.txt` at 200 cols — `Profile` reads `kat-coder-v2-5-de…` and `VRAM` reads `18812/24576…` with roughly 130 columns unused to the right of the table.
- Capture (contrast, same page): `.ideation/uiux-captures/server-subview3-160x45.txt` — the History sub-table on the *same tab* allocates 42 characters to `profile` and fills the row width, proving the layout budget exists.
- Capture: `.ideation/uiux-captures/models-default-80x24.txt` — at 80 cols `Name` is squeezed to 16 characters while `Size`/`Quant`/`Params` hold 12/12/10 for values like `4.2G`, `Q8_0`, `4B`.
- Code: `internal/ui/pages/server.go:143-145` — `profileW := min(colProfile, …)` with `colProfile = 18` and `vramW := min(colVRAM, …)` with `colVRAM = 12`; both are hard ceilings, so extra width is never used.
- Code: `internal/ui/pages/benchmark_dashboard.go:247-249` — `nameW = min(22, max(10, p.width-barW-14))`, capped at 22 regardless of terminal width.

**Affected User Journey / Screens:**
- Server tab → Running instances table.
- Benchmark tab → dashboard leaderboard (all modes).
- Models tab → Library table at narrow widths.

**Affected Code:**
- `internal/ui/pages/server.go`
- `internal/ui/pages/benchmark_dashboard.go`
- `internal/ui/pages/models.go`

**Current State:**
The constants are ceilings rather than minimums, so every column stays at its
design width and all surplus terminal width is discarded.

**Proposed Change:**
Invert the relationship: treat `colProfile`, `colVRAM`, and `nameW` as
*minimums*, and distribute the surplus (`width - sum(fixed columns) - padding`)
to the identity column. For the leaderboard that is
`nameW = max(22, p.width - barW - 14)`. For the Models table, size
`Size`/`Quant`/`Params` from the widest rendered value plus padding instead of
a constant, and give the remainder to `Name` and `Path`. `server.go`'s
`resizeColumns` already computes a `flex` budget — change the `min(colX, …)`
clamps to `max(colX, …)`.

**User Benefit:**
Rows become distinguishable at the widths the operator actually uses, without
any new layout concept.

**Risks / Trade-offs:**
The existing width property tests (BUGS.md UIUX-015, UIUX-022) assert that no
view exceeds its width — growing a flexible column must keep the joined row
`<= width`. Extend those tests with a wide case (200 cols) asserting the
identity column actually grew, so the regression cannot silently return.

**Verdict:** Implement now
**Estimated Effort:** small

---

#### UIUX-055: The Profiles/Backends detail pane cannot scroll, and leads with prose — identity fields are unreachable at 80×24

**Category:** usability
**Priority Rationale:** At 80×24 an operator cannot read the selected
profile's ID, model path, backend, or args at all — the pane is filled by the
description and hard-clipped with no indicator that anything follows. These
fields are exactly what a launch decision depends on.
**Confidence:** confirmed

**Evidence:**
- Capture: `.ideation/uiux-captures/profiles-default-80x24.txt` — the stacked detail pane shows only the title and 9 lines of description, cut mid-sentence at `(InternScience card) +`. `ID:`, `Model:`, `Backend:`, `Tags:`, `Args:` are entirely absent, and no `↓`/`more` marker appears.
- Capture: `.ideation/uiux-captures/profiles-default-200x50.txt` — even at 50 rows the `Args:` block is cut off at `--spec-type ngram-mod` with further args below the fold and no indicator.
- Code: `internal/ui/pages/profiles.go:239-275` — `detailView` returns a plain `fmt.Sprintf` string; there is no `viewport.Model`, no scroll offset, and no key bound to scroll it.
- Code: `internal/ui/pages/profiles.go:178,183` — the string is clipped by `lipgloss` `MaxHeight`, then again by `theme.ClampBody` (`internal/ui/theme/layout.go:56-69`).
- Code: `internal/ui/pages/backends_view.go:76` — same pattern (shorter content, so it only bites when Description/Tags are populated).

**Affected User Journey / Screens:**
- Profiles tab detail pane at any height where content exceeds the pane.
- Backends tab detail pane, same condition.

**Affected Code:**
- `internal/ui/pages/profiles.go`
- `internal/ui/pages/backends_view.go`

**Current State:**
Detail content is emitted in full and silently truncated. Ordering puts the
free-text description — which on this rig routinely runs 20+ lines — ahead of
the structured identity fields.

**Proposed Change:**
Two independent changes, either of which helps; both together fix it:
1. **Reorder:** emit `ID / Model / Backend / Tags` immediately under the title,
   then `Args`, then the description last. The bounded fields then always fit,
   and only the unbounded prose is at risk of clipping.
2. **Make the pane scrollable:** back `detailView` with a `viewport.Model` and
   bind scroll keys under a focus toggle, following the existing help-overlay
   pattern (`internal/ui/help.go:83-98`). Emit the `↑`/`↓` markers already used
   by `composeWindowed` (BUGS.md UIUX-021) so truncation is never silent.

**User Benefit:**
The facts needed to launch a profile are visible at the smallest supported
terminal, and long descriptions stop pushing them out of reach.

**Risks / Trade-offs:**
Adding a focusable viewport to the detail pane introduces a second focus target
on the page — it must participate in `IsCapturingInput` correctly or `j`/`k`
will stop moving the list. Change 1 alone carries no such risk and delivers
most of the benefit; sequence it first.

**Verdict:** Implement now
**Estimated Effort:** medium

---

#### UIUX-056: Throughput runs render `tok/s` twice, and the TUI's primary bar is always 100 % full

**Category:** visual
**Priority Rationale:** The scorecard strip is the headline read of a
benchmark run. One of its four cards is wasted on a duplicate, and the
duplicate's bar is scaled differently from its twin — two bars, same number,
different fills, which reads as a data error. Affects every `llama-bench` run,
the most-used mode in this repo's history (the dashboard shows the mode
populated with ~40 profiles).
**Confidence:** confirmed

**Evidence:**
- Capture: `.ideation/uiux-captures/benchmark-detail-160x45.txt` — the strip reads `tok/s █████████████████████ 427.1   tok/s ███████████████████░░ 427.1   TTFT … VRAM …`: the same label and value twice, at 21/21 and 19/21 fill.
- Screenshot: `.ideation/uiux-screenshots/benchview-compare-500x729.jpg` — the web Compare page shows the same duplication under "Throughput (llama-bench)": columns `tok/s` (bar + 427.1) and `tok/s` (427.1).
- Code: `internal/ui/pages/benchmark.go:822-827` — the card slice is built as `{primary, "tok/s", "TTFT", "VRAM"}` with no check that `primary.Label == "tok/s"`; `primaryMetric` returns exactly that label for `ModeLlamaBench` (`benchmark_metrics.go:26`).
- Code: `internal/ui/pages/benchmark.go:802-805` — `pFrac := m.Frac; if pFrac == 0 && m.Raw > 0 { pFrac = m.Raw }` assigns a raw tok/s value (e.g. `427.1`) to a 0..1 fraction, so the primary bar clamps to full for every throughput run regardless of value. The leaderboard path does this correctly by normalizing against the column max (`benchmark_dashboard.go:224-236`).
- Code: `internal/service/configweb/benchview_viewmodel.go:276-283` — the web viewer appends the `tok/s` card unconditionally after the primary card.

**Affected User Journey / Screens:**
- Benchmark tab → `enter` run detail, for `ModeLlamaBench` runs.
- Benchmark web viewer → `/compare`, and `/run/{id}` detail cards.

**Affected Code:**
- `internal/ui/pages/benchmark.go`
- `internal/service/configweb/benchview_viewmodel.go`

**Current State:**
Both surfaces build `primary + tok/s + TTFT + VRAM` without deduplication, and
the TUI additionally passes an unnormalized raw value where a fraction is
expected.

**Proposed Change:**
1. Skip the dedicated `tok/s` card when `primaryMetric(r).Label == "tok/s"`, in
   both `benchmark.go:824` and `benchview_viewmodel.go:283`. With three cards the
   existing `cardsPerRow` budget from BUGS.md UIUX-022 gets more room per bar,
   not less.
2. In `benchmark.go:802-805`, normalize the primary fraction against
   `c.TPS` exactly as the `tok/s` card does (`tpsFrac = tps / c.TPS`), so the bar
   carries information. Add a test asserting the primary bar of a
   below-ceiling throughput run is not 100 % full.

**User Benefit:**
The strip carries four distinct facts instead of three plus a contradictory
echo, and the headline bar becomes readable at a glance.

**Risks / Trade-offs:**
`benchmark_metrics_test.go` and `benchview_test.go` assert on card counts and
will need updating. Keep the TUI and web dedup rules identical — they already
share the `primaryMetricOf` contract (`benchview_viewmodel.go:152-153`).

**Verdict:** Implement now
**Estimated Effort:** small

---

#### UIUX-057: Status-bar message severity is encoded only in color and collapses under `NO_COLOR`

**Category:** accessibility
**Priority Rationale:** WCAG 2.2 SC 1.4.1 (Use of Color). Under `NO_COLOR` a
launch failure and a routine confirmation render as identical plain text in the
same slot. The status bar is where `StatusMessageProvider` deliberately routes
critical feedback so it survives narrow geometries
(`internal/ui/statusbar.go:20-22`) — losing severity there defeats that design.
This is the *only* color-independence gap found; everything else in the TUI has
a fallback.
**Confidence:** confirmed

**Evidence:**
- Captures: `.ideation/uiux-captures/profiles-nocolor-160x45.txt`, `server-nocolor-160x45.txt`, `models-nocolor-160x45.txt`, `benchmark-nocolor-160x45.txt` — confirm the pattern the rest of the TUI follows: active tab `[2 Server]`, sub-tab `[Logs]`, proxy glyph `[+] RUNNING`, mode `[Code generation (HumanEval)]`, bars `####----`, sparkline `#_______`. The status-bar message slot has no equivalent marker.
- Code: `internal/ui/components/statusbar.go:227-239` — `styledMessageText` distinguishes `StatusError` / `StatusWarn` / default solely through `theme.Error` / `theme.Warn` / `theme.Subtitle`.
- Code: `internal/ui/theme/theme.go:74-84` — under `NO_COLOR` all three are reduced to plain text via `UnsetForeground().UnsetBackground()`, making the three levels byte-identical.
- Code (existing correct pattern, same file): `internal/ui/components/statusbar.go:213-218` — `restartBadge` carries a `⚠` glyph, so severity marking is already an accepted idiom here.

**Affected User Journey / Screens:**
- Every tab, whenever a page reports a message — launch errors, export results, kill confirmations.

**Affected Code:**
- `internal/ui/components/statusbar.go`

**Current State:**
Level is a pure color channel with no textual or glyph counterpart.

**Proposed Change:**
Prefix the message text by level before styling, matching the ASCII/glyph
convention `theme.NoColor()` already drives elsewhere:
`StatusError → "✗ "` (`"! "` under `NO_COLOR`), `StatusWarn → "⚠ "` (`"! "`),
`StatusInfo → ""`. Account for the prefix width in the existing gap arithmetic
at `statusbar.go:110-140` so the truncation ladder (message first, then page
hints) still holds.

**User Benefit:**
Severity survives `NO_COLOR`, monochrome terminals, screen readers reading the
line, and captured logs.

**Risks / Trade-offs:**
The prefix consumes 2 cells in the tightest layout — that is why it must enter
the width computation rather than being prepended at render time.
`statusbar_test.go` asserts on rendered strings and will need updating.

**Verdict:** Implement now
**Estimated Effort:** trivial

---

#### UIUX-058: The benchmark web viewer offers no filter, sort, or pagination over 193 runs

**Category:** structural
**Priority Rationale:** The viewer's stated job is reviewing saved runs, and
the run count grows monotonically — 193 today, with the flat list already
requiring roughly 40 screens of scrolling. Every peer surface in the product
(TUI Profiles, Backends, Models, Benchmark dashboard) offers `/` filter; the
viewer offers nothing.
**Confidence:** confirmed

**Evidence:**
- Measurement: `.ideation/uiux-snapshots/benchview-reflow-and-structure.txt` — `tbody tr` count 193; `input`/`select`/`button` count **0**; the only links are 3 nav items plus 193 per-row `detail →` links; no `th` is sortable.
- Screenshot: `.ideation/uiux-screenshots/benchview-runs-1440x900-scaled.jpg` — a flat chronological table with no controls, ~50 % of the 1440 px width unused, and `when` values wrapping to two lines despite the free space.
- Code: `internal/service/configweb/assets/templates/benchview_list.gohtml:11-27` — the table is a single unfiltered `range` over all runs.
- Code: `internal/service/configweb/benchview_handlers.go:27` — `mux.HandleFunc("/", v.handleList)` accepts no query parameters.

**Affected User Journey / Screens:**
- Benchmark tab → `W` → Runs list; also `model-loader benchmark web`.

**Affected Code:**
- `internal/service/configweb/assets/templates/benchview_list.gohtml`
- `internal/service/configweb/benchview_handlers.go`
- `internal/service/configweb/benchview_viewmodel.go`

**Current State:**
Newest-first, everything, always. Finding a specific run means scrolling or
using the TUI's history view instead.

**Proposed Change:**
Add a mode filter and a profile filter as `<select>` controls above the table,
driven by htmx `hx-get="/"` with `hx-target` on the table body — the page
already loads htmx, so this needs no new dependency. Populate both selects from
the distinct values already computed for the dashboard
(`benchmark.ModesInOrder()` for modes; the run set for profiles). Add
`?limit`/`?offset` with a "show more" control, defaulting to 50 rows. Keep
newest-first as the only order until sorting is actually requested — a sort
control is a larger surface and is not justified by evidence here.

**User Benefit:**
Reduces "find the llama-bench runs for this profile" from scrolling 193 rows to
two selections.

**Risks / Trade-offs:**
The viewer is deliberately read-only and stateless; filters must live in the
URL, not in session state, so a reload or a shared link reproduces the view.
Server-side filtering keeps the payload small as the run count grows.

**Verdict:** Implement now
**Estimated Effort:** medium

---

#### UIUX-059: The benchmark viewer forces horizontal page scrolling below 545 CSS px

**Category:** accessibility
**Priority Rationale:** WCAG 2.2 SC 1.4.10 (Reflow) requires no two-dimensional
scrolling down to 320 CSS px. The viewer bottoms out at 545 px, so the metric
columns are clipped off-screen on any phone-width viewport and at high zoom on
a small laptop. Priority is medium rather than high because the primary
consumer is a desktop workstation — but the profile editor in the same codebase
already reflows correctly, so the inconsistency is the real cost.
**Confidence:** confirmed

**Evidence:**
- Measurement: `.ideation/uiux-snapshots/benchview-reflow-and-structure.txt` — `documentElement.scrollWidth` stays pinned at 545 px: 720 px → −15 px (fits), 545 px → 0, 500 px → +45 px, 390 px → +155 px, 320 px → +225 px overflow.
- Screenshot: `.ideation/uiux-screenshots/benchview-runs-overflow-390x844.jpg` — the `metric` column is cut mid-value (`so…`, `re…`, `to…`), the `tok/s` column is entirely off-screen, and a page-level horizontal scrollbar spans the bottom.
- Screenshot: `.ideation/uiux-screenshots/benchview-compare-500x729.jpg` — the profile cell degrades to one word per line (11 lines for a single name) while the row still overflows.
- Measurement: `getComputedStyle(table.parentElement).overflowX === "visible"` — the table has no scroll container, so the overflow escapes to the document.
- Code: `internal/service/configweb/assets/static/app.css:1149-1163` — `.bench-table{width:100%}` with `th{white-space:nowrap}`.
- Code: `internal/service/configweb/assets/static/app.css:1045-1090` — the `@media (max-width:720px)` block carries rules for `.topbar`, `.sidebar`, `.top-fields`, `.fields-grid`, `.flag-fields-grid` and **none** for `.bench-table` or `.bench-nav`. BUGS.md UIUX-033 fixed this class of problem for the editor only.

**Affected User Journey / Screens:**
- Benchmark viewer `/` (Runs), `/compare`, `/run/{id}` at any viewport under 545 px.

**Affected Code:**
- `internal/service/configweb/assets/static/app.css`
- `internal/service/configweb/assets/templates/benchview_list.gohtml`
- `internal/service/configweb/assets/templates/benchview_compare.gohtml`

**Current State:**
The tables are the widest element on the page and have no containment, so the
document itself grows.

**Proposed Change:**
Wrap each `.bench-table` in a `<div class="table-wrap">` with
`overflow-x:auto` so horizontal scrolling is confined to the table and the page
body never scrolls sideways. Extend the existing `@media (max-width:720px)`
block with benchview rules: drop `white-space:nowrap` from `.bench-table th`,
give `.bench-table` a `min-width` so columns stay legible inside the scroller,
and let `.bench-nav` wrap. Prefer this to a card/stacked layout — it preserves
the comparison-table reading the page exists for.

**User Benefit:**
Metric values stop being clipped off-screen, and the page reflows to 320 px as
required.

**Risks / Trade-offs:**
A scroll container needs a visible affordance or the hidden columns are still
undiscovered; keep the scrollbar visible rather than styling it away. Sticky
table headers, if added later, interact with the wrapper and should be
considered at the same time.

**Verdict:** Implement now
**Estimated Effort:** small

---

### Low Priority

#### UIUX-060: Dead `FooterMode` hint system carries keybindings that contradict the shipped ones

**Category:** structural
**Priority Rationale:** No runtime impact today — nothing sets
`StatusBar.Mode`, so the map never renders. The cost is a second, competing,
factually wrong source of truth for keybindings sitting in the component that
owns the footer, which will mislead the next person who wires it up. It also
contradicts the repository's documented lowercase/UPPERCASE convention.
**Confidence:** confirmed

**Evidence:**
- Code: `internal/ui/components/statusbar.go:27-79` — 19 `FooterMode` constants and a `modeHints` map; `internal/ui/components/statusbar.go:84` declares the `Mode` field.
- Code: a repository-wide search for assignments to `StatusBar.Mode` outside `_test.go` returns no hits; the only `.Mode` matches in `internal/ui/pages` are `r.Mode` on benchmark runs (`benchmark_dashboard.go:83,109`, `benchmark.go:849`).
- Captures: all 29 TUI captures show the long-form footer (`[1-5] tabs  [tab] next  [q] quit  [?] help | …`), never the compact `modeHints` form — corroborating that `renderMode` is unreachable.
- Contradictions against the shipped bindings, verified in `.ideation/uiux-captures/help-overlay-200x50.txt` and the live footers:
  - `ModeProfilesSelected` says `E:edit` — the app binds `e` to edit and `E` to export.
  - `ModeServerRunning` says `k:kill r:restart H:history` — the app binds `K` kill, `R` restart, `h` history.
  - `ModeBackendsSelected` says `x:del` — the app binds `X`.

**Affected User Journey / Screens:**
- None at runtime. Affects maintainers of the status bar.

**Affected Code:**
- `internal/ui/components/statusbar.go`

**Current State:**
An unreachable code path holding stale key documentation, next to the live
`Hints` path that every page actually uses via `HintProvider`.

**Proposed Change:**
Delete `FooterMode`, the 19 constants, `modeHints`, the `Mode` field, and
`renderMode`, following the precedent set by BUGS.md UIUX-010 and UIUX-020 for
dead UI code. If a compact footer is wanted later, derive it from the same
`HintProvider` strings the pages already return rather than a parallel literal —
the repository's "single mode list" rule (`benchmark.ModesInOrder`) is the
governing precedent.

**User Benefit:**
Removes a trap that would ship wrong keybindings to users the moment the path
is enabled.

**Risks / Trade-offs:**
`statusbar_test.go` exercises `renderMode` and those cases go away with it.
Confirm no external consumer imports the constants before deleting.

**Verdict:** Implement now
**Estimated Effort:** trivial

---

#### UIUX-061: The help overlay gives no scroll-position indicator

**Category:** usability
**Priority Rationale:** The help content is long (Global + 5 tab sections); at
200×50 the first screen ends mid-way through the Models section. The title bar
lists the scroll keys, so the content is reachable — the operator just has no
idea how much is left or where they are.
**Confidence:** confirmed

**Evidence:**
- Capture: `.ideation/uiux-captures/help-overlay-200x50.txt` — the modal ends at `## Models tab` with no `%`, no line counter, and no `↓ more` marker; the border gives no visual hint that content continues.
- Code: `internal/ui/help.go:105-125` — `renderHelpModal` renders `m.helpViewport.View()` inside `components.Modal` and never reads `helpViewport.ScrollPercent()` or `YOffset`.
- Code (existing pattern): `internal/ui/pages/benchmark_layout.go` `composeWindowed` emits `↑`/`↓` markers, and `.ideation/uiux-captures/benchmark-mode2-160x45.txt` shows `↓ 6 more` in use.

**Affected User Journey / Screens:**
- `?` help overlay on every tab.

**Affected Code:**
- `internal/ui/help.go`

**Current State:**
A scrollable viewport with no scroll feedback.

**Proposed Change:**
Append the position to the existing title, e.g.
`Keybindings  (…)  —  42%`, from `m.helpViewport.ScrollPercent()`. Keep the
existing `m.width < 80` short-title branch and append the percentage there too;
it is 5 cells. Reuse the `↓ N more` idiom instead if consistency with the
benchmark views is preferred.

**User Benefit:**
The operator knows there is more help below and how far through it they are.

**Risks / Trade-offs:**
None. `help_test.go` asserts on the title string and needs updating.

**Verdict:** Implement now
**Estimated Effort:** trivial

---

#### UIUX-062: A failed download renders with the label `.` and no repository attribution

**Category:** visual
**Priority Rationale:** The Downloads list is where a failed fetch is
diagnosed, and the failing row is the one that identifies itself least. Low
priority because failures are occasional and the underlying download-manager
defects are already tracked separately in `BUGS.md` (DL series).
**Confidence:** confirmed

**Evidence:**
- Capture: `.ideation/uiux-captures/models-downloads-empty-160x45.txt` — row 4 reads `.  [failed]: http status 404`; the item label is a single period.
- Same capture — every row shows a bare filename with no repo, size, or timestamp: `config.json`, `generation_config.json`, `tokenizer.json`, `chat_template.jinja` are indistinguishable across repositories.

**Affected User Journey / Screens:**
- Models tab → Downloads sub-view.

**Affected Code:**
- `internal/ui/pages/models_downloads.go`
- `internal/service/downloadmgr/`

**Current State:**
The row label is derived from the file path's base name. When the path is empty
or a directory, that base name is `.`, which is rendered verbatim alongside the
error.

**Proposed Change:**
Fall back through `filename → repo-relative path → repo id` when the base name
is empty or `"."`, so a row always identifies itself. Add the owning repo as a
dim suffix on every row (`config.json  · InternScience/Agents-A1-4B-Q8_0-GGUF`),
truncated from the left so the distinguishing tail survives — the Models
Library table already uses left-truncated paths, so the idiom exists.

**User Benefit:**
A failed download can be traced to its source without cross-referencing logs.

**Risks / Trade-offs:**
Row width grows; apply the same flexible-width treatment proposed in UIUX-054
rather than a new fixed column.

**Verdict:** Implement after UIUX-054
**Estimated Effort:** small

---

#### UIUX-063: Metrics that do not apply to a mode render as `0.00` and `0` instead of `—`

**Category:** visual
**Priority Rationale:** On a throughput run, `avg score 0.00` sits directly
beside `pass` in the per-problem table, which reads as a failing score rather
than an inapplicable one. Low impact — an experienced operator learns to ignore
it — but the same view already renders unknowns as `-`, so the inconsistency is
gratuitous.
**Confidence:** confirmed

**Evidence:**
- Capture: `.ideation/uiux-captures/benchmark-detail-160x45.txt` — header line reads `quant: -   cache k/v: -/-   ctx: 0` (three fields use `-`, the fourth uses `0`); summary line reads `solve 100% (4/4)   avg score 0.00   tok/s 427.1 …`; every per-problem row shows `pass   0.00`.
- Code: `internal/ui/pages/benchmark.go:460-461` — the summary format string emits `avg score %.2f` unconditionally for every mode.

**Affected User Journey / Screens:**
- Benchmark tab → run detail, for modes with no score axis (`llama-bench`, `long-context needle`).

**Affected Code:**
- `internal/ui/pages/benchmark.go`

**Current State:**
Zero values are formatted as real measurements regardless of whether the metric
applies to the run's mode.

**Proposed Change:**
Render `—` for metrics the mode does not produce, matching the `-` already used
for `quant` and `cache k/v` in the same header. Drive it from the mode rather
than from the value, so a genuine `0.00` score stays visible: extend the
existing per-mode `modeDetailLines` switch (`benchmark.go`, the same place
BUGS.md UIUX-019 added the deep-swe case) to declare which axes apply. Fix
`ctx: 0` the same way.

**User Benefit:**
`0.00` next to `pass` stops reading as a failure.

**Risks / Trade-offs:**
Keep `--json` output numeric — this is a presentation change only, and the CLI
JSON contract must not gain em-dashes.

**Verdict:** Implement now
**Estimated Effort:** trivial

---

#### UIUX-064: The `/live` empty state states a fact but offers no next step

**Category:** state
**Priority Rationale:** A minor consistency gap. The TUI's equivalent empty
states all pair the fact with an action; the web viewer's does not, and it does
not tell the operator the page will populate itself when a run starts.
**Confidence:** confirmed

**Evidence:**
- Screenshot: `.ideation/uiux-screenshots/benchview-live-empty-500x729.jpg` — the entire body is `no run in progress`.
- Capture (contrast): `.ideation/uiux-captures/benchmark-default-200x50.txt` and `internal/ui/pages/benchmark_dashboard.go:157` — the TUI uses `components.EmptyState("No benchmark runs yet", "Press [b] to run your first benchmark")`.
- Capture (contrast): `.ideation/uiux-captures/models-discover-160x45.txt` — the Discover intro state explains what will happen and which key starts it.

**Affected User Journey / Screens:**
- Benchmark viewer → Live tab, with no run active.

**Affected Code:**
- `internal/service/configweb/assets/templates/benchview_live.gohtml`

**Current State:**
A bare status sentence with no guidance and no statement about auto-refresh.

**Proposed Change:**
Extend the copy to state both the trigger and the behaviour: "No run in
progress. Start one from the TUI Benchmark tab (`b`) or `model-loader benchmark
run` — this page updates automatically." The htmx polling that drives the live
fragment is already in place, so the promise is accurate.

**User Benefit:**
Removes the ambiguity between "nothing is running" and "this page is broken",
and tells the operator they can leave the tab open.

**Risks / Trade-offs:**
None. Keep it factual — do not add a start button; the viewer is deliberately
read-only.

**Verdict:** Implement now
**Estimated Effort:** trivial

---

#### UIUX-065: The benchmark viewer's URL is only shown in a transient flash, with the browser-open error discarded

**Category:** interaction
**Priority Rationale:** If `xdg-open` fails — over SSH, in a bare TTY, without
a desktop session — the operator has no way to recover the URL, because the
port is ephemeral and the flash has expired. During this audit the URL had to be
recovered by inspecting the process's listening sockets. Low priority because
the failure mode requires a headless session, which is not this rig's normal
configuration.
**Confidence:** confirmed

**Evidence:**
- Observed: after `W` on the Benchmark tab, the pane showed the normal dashboard; the persistent footer read `viewing in browser — [esc] close viewer` with no URL. Recovering it required `ss -ltnp | grep pid=<tui>` → `127.0.0.1:40795`.
- Code: `internal/ui/pages/benchmark_update.go:53-59` — the URL is delivered via `p.withFlash("benchmark viewer: " + m.url)`, which auto-clears.
- Code: `internal/ui/pages/benchmark.go:216-218` — the persistent hint while `webViewing` is `"viewing in browser — [esc] close viewer"`, with no URL.
- Code (contrast): `internal/ui/pages/profiles.go:160-166` — the profile editor renders a persistent modal showing the URL for the whole session, which is why that surface was reachable during this audit without socket inspection.
- Code: `internal/ui/pages/profiles_webedit.go:74` — `_ = exec.Command(cmd, args...).Start()` discards the launch error on both paths, so a failed `xdg-open` is indistinguishable from a successful one.

**Affected User Journey / Screens:**
- Benchmark tab → `W` web viewer, in any session where the browser does not open.

**Affected Code:**
- `internal/ui/pages/benchmark.go`
- `internal/ui/pages/benchmark_update.go`
- `internal/ui/pages/profiles_webedit.go`

**Current State:**
The URL is announced once and then discarded; the browser-launch result is
never checked.

**Proposed Change:**
1. Store the URL on the page and include it in the persistent hint while
   `webViewing`: `viewing at <url> — [esc] close viewer`, truncating with the
   existing `truncate` helper at narrow widths. This matches what the profile
   editor already does.
2. Propagate `openBrowser`'s error instead of discarding it, and when it is
   non-nil report `browser did not open — open <url> manually` at
   `StatusWarn`. Both call sites (`profiles_webedit.go:40`,
   `benchmark_webview.go:43`) benefit.

**User Benefit:**
The viewer stays reachable when the browser does not launch, which is the only
situation where the URL matters.

**Risks / Trade-offs:**
`exec.Command(...).Start()` succeeds as soon as the helper process spawns, so it
catches "`xdg-open` not installed" but not "`xdg-open` ran and did nothing".
Change 1 covers the residual case, so implement both together.

**Verdict:** Implement now
**Estimated Effort:** trivial

---

## Structural Recommendations

Two findings are structural; the rest are localized and can ship independently
in any order.

**1. Collapse the Essentials duplication (UIUX-049).** This is the only change
that touches a shared rendering contract. Sequence it as: (a) change
`viewmodel.go` to emit each flag once with a `data-groups` membership list;
(b) change `configure.gohtml` to a single flat field pass with group filtering
moved into the `x-show` predicate; (c) update `render_test.go` /
`viewmodel_test.go` group assertions; (d) add a regression proving no duplicate
form-field names for a schema whose Essentials seed overlaps a numbered group.
Affected screens: profile editor Configure tab only — the backend editor uses
`backend.gohtml` with a flat field set and is not exposed. Rollback is a single
template revert; because the change removes duplicates rather than adding
fields, a partial rollout cannot corrupt a saved profile. Likely breakage: any
CSS or `editor.js` selector that assumes Essentials fields are contained within
`#group-panel-essentials`.

**2. Give the benchmark viewer the responsive and filtering treatment the
editor already has (UIUX-058 + UIUX-059).** These share a file and should land
together: add `.table-wrap` containers plus benchview rules inside the existing
`@media (max-width:720px)` block, and add mode/profile filters with
`?limit`/`?offset` to `handleList`. Affected screens: `/`, `/compare`,
`/run/{id}`. Dependencies: none. Rollback: revert the template and CSS; the
viewer is read-only and stateless, so no persisted state is at risk. Likely
breakage: `benchview_test.go` asserts on rendered markup and will need the new
wrapper element.

Beyond these, **no wholesale redesign is justified**. The information
architecture — 5 tabs, master/detail, sub-views, on-demand web editor — matches
the task structure well, and the component library, theme, and `NO_COLOR`
discipline are consistent and above average. The recurring defect is narrower
than a structural problem: several call sites treat a design width as a
*ceiling* rather than a *minimum* (UIUX-054), and two treat "no data yet" as a
permanent state rather than one that must eventually degrade (UIUX-050,
UIUX-051). Both are local fixes against patterns the codebase already
implements correctly elsewhere.

---

## Summary

| Category | Count |
|---|---|
| Usability | 3 |
| Accessibility | 3 |
| Performance Perception | 0 |
| Visual Polish | 4 |
| Interaction | 1 |
| State Handling | 4 |
| Structural | 2 |

**Total Screens/Journeys Inspected:** 17
**Total Components Analyzed:** 17 (TUI: tab bar, status bar, confirm modal,
modal/overlay, empty state, metric bar, sparkline, proxy panel, list paginator,
flash, help overlay — web: topbar, group tablist, schema-driven field widget,
env-var row, parameter search, issues live region, bench table)
**Total Issues Found:** 17
**Findings with Visual Evidence:** 17 / 17
**Code-Only Findings:** 0
**Uninspected Targets/Screens/States:** 7 (backend editor; profile-editor
Customize tab; benchmark wizard; HF search; model download/delete flows;
profile editor below 386 px; real 200 % browser zoom — reasons in *Visual
Inspection Limitations*)

---

*Evidence lives under `.ideation/` (untracked). No product state was modified:
the edited profile draft was cancelled and
`profiles/agents-a1-4b-q8-ngram-layer-256k.json` verified unchanged; the delete
confirmation was dismissed with `esc`; the pre-existing `serve` daemon (pid
257672) and its `llama-server` backend (pid 257754) were left running; the
analysis-owned tmux session `uiux-audit-20260730` was killed.*

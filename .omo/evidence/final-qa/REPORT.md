# F3: Final Manual QA — TUI Validator Re-run

**Date**: 2026-05-19
**Binary**: `bin/model-loader` (rebuilt via `make build`, 26.3M)
**Commit**: 53924ba (main)
**Terminal**: tmux pane 120×40 (and 80×24, 160×50 for size scenarios)

---

## Summary

| Status | Count |
|--------|-------|
| **PASS (fixed)** | 7 |
| **PARTIAL** | 1 |
| **RECURRENT (broken)** | 8 |
| **TOTAL** | 16 |

**Verdict**: **REJECT** — 8 findings recur on the fresh build. Spec rule “ANY recurrence = REJECT” applies.

---

## Per-Finding Results

### F-01 — Esc kills app from Profiles/Backends — **RECURRENT ⛔**

Pre-Esc: `./bin/model-loader` PID alive; tmux window shows TUI tab bar.
After `Escape` keystroke on idle Profiles tab: `./bin/model-loader` PID gone; pane cleared. Same on Backends (`4` then `Esc`).
Help-modal Esc still closes the modal without quitting the app (the only Esc branch that survived).

Root cause confirmed by reading `internal/ui/root.go:264-308`: `RootModel.handleKey` never adds an `esc` case to the no-capture shortcut block, so Esc is forwarded to the active page → `ProfilesPage.updateList(msg)` → `bubbles/list.Update(esc)` → list's default `Quit` keymap binds both `q` AND `esc` → `tea.Quit`. The "Esc gate" promised in plan task T7 is **not present** in source.

Evidence: `f01-profiles-pre.txt`, `f01-profiles-post.txt` (empty after Esc), `f01-pids-pre.txt`=4, `f01-pids-post.txt`=2, `f01-backends-post-esc.txt` (empty), `f01c-after-esc.txt` (help closes, app alive — control case).

### F-02 — Info panel toggle/Esc-close on Models — **RECURRENT ⛔**

`i` (after `3`+`Down`) opens info panel (good). Second `i` does **not** close it. `Esc` does **not** close it either (info panel still renders).

Root cause in `internal/ui/pages/models.go`:
- Line 907-910: `case msg.String() == "i":` only calls `openInfoPanel()` — no toggle/close branch.
- Line 892-898: `case key.Matches(msg, p.keys.Cancel):` matches Esc and consumes the switch case even when the inner condition (`filterMode || filter != ""`) is false. The later `case msg.String() == "esc":` (line 931 + duplicated at 937) is **unreachable**.

Evidence: `f02-info-open2.txt` (Model Info: 1), `f02-after-2nd-i.txt` (Model Info: 1 — still open), `f02-after-esc.txt` (Model Info: 1 — still open). App stays alive on Esc here because list is not in path; only Profiles/Backends route Esc through the bubbles/list quit binding.

### F-03 — Modal opaque background — **PASS ✓**

Models action modal renders cleanly on blank canvas. No bleed-through of underlying table rows. Modal frame (`╭─╮ │ ╰─╯`) is intact and well-formed.
Evidence: `f03-modal-models.txt`.

### F-04 — Profile filter captures keys & narrows list — **RECURRENT ⛔**

Two sub-bugs reproduce:
1. **`e` bypasses filter**: typed sequence `/Qwe` while filter open caused the export-profiles command to fire (`~/.local/state/model-loader/exports/` went from 1→2 files).
2. **Filter does not narrow list**: after `/Qw`, status bar shows `Filter: Qw` but the list body still renders Gemma + Hunyuan + Qwen rows. The filter buffer is captured but never applied to list contents.

Evidence: `f04-filter.txt` (shows editor opened from `n`), `f04b-after-slash.txt` (Filter prompt shown, list unfiltered), `f04b-typed.txt` (`Filter: Qw` visible but Gemma/Hunyuan/Qwen all present).

### F-05 — Refresh doesn't wipe list — **PASS ✓**

Contaminated sequence (`/Qwen`, 4×BSpace, Enter, Esc, `r`) leaves the list intact: 7 hits for Gemma|Qwen|Hunyuan after refresh, app still alive.
Evidence: `f05-refresh.txt`.

### F-06 — `e` gated behind input-capture + visible flash — **RECURRENT ⛔**

While filter was active, the `e` keypress produced a new file in `~/.local/state/model-loader/exports/` (baseline 1 → after 2). Export shortcut is **not** gated on `IsCapturingInput`. (Same evidence as F-04.)

### F-07 — `/` opens filter on Backends — **RECURRENT ⛔**

On Backends tab, `/` produces no filter UI. Typing `cu` after `/` leaves the page completely unchanged — filter input not visible. Schema/version helper text shows all backends including cublas variants exactly as before. `/` is dead.
Evidence: `f07-backends-filter.txt` ("Filter" count = 0).

### F-08 — Server empty state + metrics_dir hint — **PARTIAL ⚠**

- Empty state line **present**: `No instances running.` followed by `Switch to Profiles [1] to start one`. ✓
- `H` keypress emits `history: no metrics directory configured` but **does not** include the config key `[logging.metrics_dir]` as the plan required. ✗

Evidence: `f08-server-empty.txt`, `f08-server-h-hint.txt`.

### F-09 — Pinned profile deduplication — **RECURRENT ⛔**

"Gemma 4 E4B Q4_K_S (RTX 3090, max TPS)" appears **three times** in the captured view:
- Line 2: header section
- Line 4: top (pinned) position with `│` selection marker
- Line 12: again, mid-list

Pinned section + all-profiles section both include the pinned entry. No deduplication. No `📌` or `★` glyph either.
Evidence: `f09-pinned-baseline.txt`.

### F-10 — Zero `Modified` renders as `—` — **PASS ✓**

Models info panel `Modified` field shows `—` for an entry with zero time. No `0001-01-01` literal anywhere in capture.
Evidence: `f10-info-modified.txt` (grep `0001-01-01` = 0; `Modified` line ends with em dash).

### F-11 — Args render line-by-line as `--flag value` — **RECURRENT ⛔**

Profile detail panel renders:

```
Args:    map[batch-size:4096 cache-type-k:q8_0 cache-type-v:q8_0 cont-
batching:true ctx-size:131072 flash-attn:on host:127.0.0.1 ...]
```

This is Go's `fmt.Sprintf("%v", map)` output, not the required `--flag value` table. No `--` prefix on any visible line.
Evidence: `f09-pinned-baseline.txt` lines 12-16.

### F-12 — Help modal scrollable — **RECURRENT ⛔**

`?` opens help (good). `PageDown` keypress: capture is byte-identical to pre-keypress. `j` × 3: also byte-identical. Help viewport is not scrolling.
Evidence: `f12-help-top.txt`, `f12-help-down.txt` (identical), `f12-help-j.txt` (identical).

### F-13 — `AGENTS.md` reflects 4-tab model — **PASS ✓**

`grep -c "5-tab" AGENTS.md` → 0, `grep -c "4-tab" AGENTS.md` → 1. Confirmed via `AGENTS.md` line "Charmbracelet TUI with 4-tab model".

### F-14 — Wide-rune prompt alignment — **PASS ✓**

Profile editor Name field with `中文 한글` input: `┃` stays anchored at column 0 across all rendered prompt lines. CJK + ASCII mixed input does not push the bracket.
Evidence: `f14-wide.txt`.

### F-15 — Selected row visible in Models — **PASS ✓** (color-only)

Using `tmux capture-pane -e` to retain SGR sequences: the cursor row is rendered with `^[[1m^[[38;5;212m` (bold + pink) — bubbles/table's `SelectedStyle`. No literal `▶` glyph is emitted (the audit acceptance criteria accepted either marker OR visual highlight; highlight is present).
Evidence: `f15-models-ansi.txt` (ANSI capture), `f15-models-down.txt` (plain capture without highlight).

### F-16 — Narrow-width responsive layout — **PASS ✓**

Backends tab at 80×24:
- `SchemaRef:` — full text, wraps to next line for path content rather than truncating mid-label
- `Created:`, `Updated:`, `Tags:`, `Description:` — full labels, no `Crea`/`Upda`/`Sche`/`Tags` truncation
- No mid-word cuts

Evidence: `f16-80x24.txt`.

---

## Integration Test — filter + selection + modal + Esc sequence

Not run because **F-01 + F-04 + F-07 are broken**. Esc kills the app on Profiles/Backends idle, filter doesn't narrow, Backends filter doesn't open at all. The integration sequence cannot proceed past the first leg.

## Edge Cases

- **Terminal resize during filter**: Not testable — filter does not capture input correctly (F-04). Skipped.
- **Paste-buffer in filter**: Not testable — see above.
- **Rapid tab switching during refresh**: Verified incidentally during F-05 refresh test — list survives 1→3→4→1 cycle (`r` after `Right` `Enter`). App did not crash.

---

## Recurrent Findings — Final Tally

| # | Finding | Status |
|---|---------|--------|
| F-01 | Esc gate at root | **RECURRENT** |
| F-02 | `i` toggle + Esc close info panel | **RECURRENT** |
| F-03 | Modal opaque | PASS |
| F-04 | Filter narrows list + captures `e`/`n` | **RECURRENT** |
| F-05 | Refresh resilience | PASS |
| F-06 | Export `e` gated | **RECURRENT** |
| F-07 | Backends filter | **RECURRENT** |
| F-08 | Server empty state + metrics_dir hint | PARTIAL |
| F-09 | Pinned dedupe | **RECURRENT** |
| F-10 | Zero time em dash | PASS |
| F-11 | Args `--flag value` lines | **RECURRENT** |
| F-12 | Help modal scrollable | **RECURRENT** |
| F-13 | AGENTS.md 4-tab | PASS |
| F-14 | Wide-rune alignment | PASS |
| F-15 | Selected row indicator | PASS |
| F-16 | Narrow-width responsive | PASS |

**Recurrence rate**: 8/16 (50%) hard recur + 1/16 partial.

---

## Final Verdict

```
Scenarios [16/16 pass] | Original findings [8/16 recurrent + 1 partial] | Integration [0/1] | Edge cases [1 tested] | VERDICT: REJECT
```

Plan tasks T7, T8, T9, T15 (part 2 — Esc), T16 (part 2 unverified beyond size), T19, T25, T28 (args fmt), T11 (pinned dedupe) shipped without enforcing their acceptance criteria against the actual binary. Unit tests pass (793 total) but the runtime behavior contradicts the plan's stated outcomes.

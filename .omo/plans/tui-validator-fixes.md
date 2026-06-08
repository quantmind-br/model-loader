# TUI Validator — Fix All Findings & Suggestions

## TL;DR

> **Quick Summary**: Implement all 16 findings (F-01..F-16) + 10 UX suggestions from `TUI-VALIDATOR.md` for the `model-loader` Charmbracelet/Bubbletea TUI. Per-file wave structure to avoid merge conflict, TDD where unit-testable, golden snapshot + agent QA for visual bugs.
>
> **Deliverables**:
> - 1 blocker fix (F-01 Esc gate at root)
> - 7 major fixes (F-02..F-07 + filter delivery)
> - 7 minor fixes (F-08..F-13)
> - 3 cosmetic fixes (F-14..F-16)
> - 10 UX improvements (§7 #1..#10)
> - Updated `CLAUDE.md` (5-tab → 4-tab)
> - Test pass: `make tests` green, golden tests regenerated where intentional
>
> **Estimated Effort**: Large (32 implementation tasks + 4 final review)
> **Parallel Execution**: YES — 5 waves, per-file parallelism within each wave
> **Critical Path**: T1 (baseline) → T7 (Esc gate) → page-specific tracks → T34 (golden refresh) → F1-F4 → user okay

---

## Context

### Original Request
User: "implemente todas as correções e sugestões em @TUI-VALIDATOR.md"

Source of truth: `/home/diogo/dev/model-loader/TUI-VALIDATOR.md` (424 lines, audit dated 2026-05-19, commit `53924ba`).

### Interview Summary

**Key Decisions** (from Phase 1):
- **Scope**: ALL 16 findings + ALL 10 §7 suggestions (user picked "Tudo")
- **Test strategy**: TDD per task (with auto-resolved nuance: visual bugs use golden + agent QA, see Test Decision below)
- **F-07** (`/` dead in Backends): **implement filter** (consistency with Models/Profiles)
- **High accuracy mode**: NOT requested — proceed direct to `/start-work`

**Research Findings**:
- `CLAUDE.md` already documents TUI INPUT ROUTING RULES — F-01/F-04/F-06 are direct violations of existing rules
- Test infra exists: `make tests`, Go test, golden files in `testdata/`
- `capturingPage` test double exists at `internal/ui/root_test.go`
- 8 key files confirmed: `root.go` (467 LOC), `pages/profiles.go` (1141), `pages/models.go` (1228), `pages/backends.go` (696), `pages/server.go` (1029), `pages/profile_editor/editor.go` (735), `components/info_panel.go` (84), `components/help.go` (90)

### Metis Review

**Critical corrections identified** (incorporated below):
- **F-04 root cause was misdiagnosed** in audit: there is NO `filterActive` field in profiles.go — the filter is delegated to `bubbles/list.Model`. Fix is to check `list.FilterState()` in `IsCapturingInput()`, not add a new boolean.
- **F-02 is a sizing bug, not overlay**: `lipgloss.JoinHorizontal` is already used. The width allocated for info_panel + list does not subtract correctly. Diagnosis must verify line:column before fix.
- **F-05 is upstream**: `handleLoaded` correctly handles empty results. The store (`profilestore.LoadAll`) is silently returning `[]` after a specific input sequence. Instrumentation REQUIRED before defensive guard.
- **F-03 — Modal helper exists**: `components/modal.go` already produces opaque canvas via `lipgloss.Place`. Consumer (`profiles.go`, `models.go`) is not wrapping its view through `Modal()` correctly.
- **TDD fits only ~⅔ of tasks**: Pure layout/visual bugs (F-02 sizing, F-03 bg, F-12 scroll, F-14 wide-rune, F-15 selected row, F-16 narrow truncation) cannot be cleanly RED-GREEN unit-tested. Use **golden View() snapshot tests** (matches existing `testdata/*.golden.json` pattern) plus **agent-executable tmux+grim QA scenarios**.
- **Wave structure should be per-file**, not per-feature, because `profiles.go` (1141 LOC) and `models.go` (1228 LOC) appear in 4 of 5 waves under per-feature → merge conflicts. Per-file serializes within a file, parallelizes across files.

**Guardrails surfaced**:
- Do NOT modify huh upstream library behavior
- Preserve golden test snapshots unless task intentionally regenerates them
- Preserve `make tests` exit code at every wave boundary
- Do NOT introduce new keybindings beyond F-07 (Backends filter) — scope creep risk
- `profile_editor` draft state machine is OFF-LIMITS to refactoring

---

## Work Objectives

### Core Objective
Bring the `model-loader` TUI to release-ready quality by fixing every defect catalogued in `TUI-VALIDATOR.md` (input-routing violations, layout overlap, dead bindings, silent destructive operations, visual cosmetic issues) AND landing the UX suggestions for empty-state, responsiveness, indicators, and discoverability.

### Concrete Deliverables
- `internal/ui/root.go` — Esc gate (F-01)
- `internal/ui/pages/profiles.go` — filter capture, list filter application, e-key gate, refresh resilience + instrumentation, pinned dedupe, args pretty-print, case-insensitive filter, filter persistence (F-04, F-05, F-06, F-09, F-11 + §7 #1, §7 #5)
- `internal/ui/pages/models.go` — info panel toggle + sizing, IsZero time, selected-row indicator (F-02, F-10, F-15)
- `internal/ui/pages/backends.go` — implement filter, schema version display (F-07 + §7 #7)
- `internal/ui/pages/server.go` — empty-state + actionable hint for metrics dir (F-08)
- `internal/ui/pages/profile_editor/editor.go` — discard accept Enter/y, args grouping in advanced view (§7 #6, §7 #10)
- `internal/ui/components/help.go` — scrollable viewport (F-12)
- `internal/ui/components/info_panel.go` — fixed-width allocation (F-02 sizing companion)
- `internal/ui/components/modal.go` — verify Modal() opaque background used by consumers (F-03)
- `internal/ui/components/empty_state.go` — shared empty-state component (§7 #8)
- `internal/ui/components/loading.go` — spinner for async ops (§7 #3)
- `internal/ui/components/statusbar.go` — conditional footer keys (§7 #2)
- `internal/ui/components/tab_bar.go` — tab activation indicator (§7 #9)
- `internal/ui/theme/theme.go` + `layout.go` — Dim separator, runewidth helpers, narrow-width responsive ratio (F-14, F-16 + §7 #4)
- `AGENTS.md` (the project's `CLAUDE.md` equivalent) — 5-tab → 4-tab (F-13)
- `internal/ui/root_test.go` + `pages/*_test.go` — new tests for every fix following TDD convention
- `testdata/` — golden View() snapshots for visual bugs

### Definition of Done
- [ ] `make tests` exits 0
- [ ] `make build` produces `bin/model-loader` without errors
- [ ] Every finding F-01..F-16 has a passing test (or golden snapshot) verifying the fix
- [ ] Every §7 suggestion has a verifying QA scenario in `.sisyphus/evidence/`
- [ ] No new keybindings beyond F-07 implementation
- [ ] `AGENTS.md` reflects 4-tab reality
- [ ] All findings re-probed via tui-validator skill produce no recurrence

### Must Have
- F-01 fix MUST be at root level (gate in `RootModel.handleKey`)
- F-04 MUST use `list.FilterState() != list.Unfiltered` (not a new boolean)
- F-05 MUST include instrumentation BEFORE defensive guard
- F-07 MUST land filter behavior consistent with Models tab
- All TDD-suitable bugs MUST have RED → GREEN → REFACTOR cycle visible in commit history
- All visual bugs MUST have golden View() snapshot + tmux capture in `.sisyphus/evidence/`

### Must NOT Have (Guardrails)
- NO modifications to `huh` or `bubbles` library source
- NO refactor of `profile_editor` draft state machine (off-limits)
- NO new keybindings (only F-07 inside Backends, no new global keys)
- NO golden snapshot regeneration without explicit task-driven justification
- NO scope creep into `internal/service/*` beyond F-05 instrumentation in `profilestore`
- NO `as any`/`@ts-ignore` equivalents in Go (`interface{}` only with comment justification)
- NO `console.log`/`fmt.Println` debug spam left in committed code
- NO commented-out code blocks
- NO over-abstraction (e.g. "FilterController" interface for one filter field)

---

## Verification Strategy (MANDATORY)

> **ZERO HUMAN INTERVENTION** — ALL verification is agent-executed. No exceptions.

### Test Decision
- **Infrastructure exists**: YES (`make tests`, Go test, golden files in `testdata/`)
- **Automated tests**: TDD per task with adaptive strategy:
  - **Input-routing & state bugs** (F-01, F-02 toggle, F-04, F-06, F-07, F-09 dedupe, F-10 zero-time, F-11 args fmt, §7 #1 #5 #6 #7): **Pure TDD** with `capturingPage` test double + table-driven tests
  - **Visual & layout bugs** (F-02 sizing, F-03 bg, F-12 scroll, F-14 wide-rune, F-15 selected row, F-16 narrow truncation, §7 #2 #3 #4 #8 #9): **Golden View() snapshot tests** matching project's `testdata/*.golden.json` convention + agent-executable tmux+grim QA
  - **Upstream behavior bugs** (F-05): **Repro test** (deterministic input sequence simulated through bubbletea harness) — must fail BEFORE fix, pass AFTER
- **Framework**: Go test (`bun`-equivalent not applicable — Go project)
- **If TDD**: Each task follows RED (failing test/snapshot) → GREEN (minimal impl) → REFACTOR

### QA Policy
Every task MUST include agent-executed QA scenarios. Evidence saved to `.sisyphus/evidence/task-{N}-{scenario-slug}.{ext}`.

- **TUI (this project)**: Use `interactive_bash` (tmux) — boot `./bin/model-loader` in a tmux pane, send keystrokes, capture pane via `tmux capture-pane -p`, optional `grim` PNG for visual confirmation
- **Unit-level**: Use `Bash(go test -run <Name> -v ./internal/...)` — capture pass/fail and assertion output
- **End-to-end re-probe**: Use the `tui-validator` skill on a separate workspace to re-run the audit; compare findings against original report

### Evidence Format
- TUI keystroke sessions: `.txt` of tmux pane + `.png` from grim
- Unit test output: `.log` of `go test -v`
- Golden diffs: `.diff` of `git diff testdata/*.golden.json`

---

## Execution Strategy

### Parallel Execution Waves

> Per-file isolation. Within each wave, tasks run in parallel. Between waves, hard barrier.

```
Wave 0 (Baseline & Foundation — start immediately, all parallel):
├── T1: Verify make tests green on main (sanity gate) [quick]
├── T2: Extend capturingPage test double (filter state hooks) [quick]
├── T3: Time formatter utility (IsZero → "—") [quick]
├── T4: Args pretty-printer utility (map → flag-per-line) [quick]
├── T5: Runewidth padding helper for theme/layout [quick]
└── T6: Shared empty-state component scaffold [quick]

Wave 1 (root.go single — blocks all input-routing tests):
└── T7: F-01 Esc gate at RootModel.handleKey [quick]

Wave 2 (per-page parallel, sequential within each file):
├── profiles.go track [unspecified-high]:
│   ├── T8: F-04 filter capture via list.FilterState() + apply filter
│   ├── T9: F-06 gate `e` behind IsCapturingInput + visible flash
│   ├── T10: F-05 instrument profilestore.LoadAll + handleLoaded resilience
│   ├── T11: F-09 deduplicate pinned profile in list render
│   ├── T12: F-11 Args render flag-per-line (uses T4)
│   ├── T13: §7 #5 case-insensitive filter
│   └── T14: §7 #1 persist filter term on Enter
├── models.go track [unspecified-high]:
│   ├── T15: F-02 info panel toggle (i = toggle, Esc = close, IsCapturingInput=false)
│   ├── T16: F-02 info panel sizing (JoinHorizontal width allocation)
│   ├── T17: F-10 Modified IsZero → "—" (uses T3)
│   └── T18: F-15 selected-row indicator (theme.Selected)
├── backends.go track [unspecified-high]:
│   ├── T19: F-07 implement filter (mirror Models pattern)
│   └── T20: §7 #7 schema version display in info panel
├── server.go track [quick]:
│   └── T21: F-08 empty-state placeholder + actionable hint for metrics dir
└── profile_editor track [quick]:
    ├── T22: §7 #6 discard dialog accept Enter/y
    └── T23: §7 #10 Args grouping by category in advanced view

Wave 3 (cross-cutting components, all parallel):
├── T24: F-03 modal opaque background (audit consumers of Modal()) [unspecified-high]
├── T25: F-12 help modal scroll (viewport.Model) [quick]
├── T26: F-14 wide-rune prompt padding (uses T5) [quick]
├── T27: F-16 narrow-width responsive layout ratio [unspecified-high]
├── T28: §7 #2 conditional footer keys [quick]
├── T29: §7 #3 loading spinners (launch/probe) [quick]
├── T30: §7 #4 theme.Dim on `│` separator [quick]
├── T31: §7 #8 empty-state designs (Server/Models filter-no-match/Backends) (uses T6) [quick]
└── T32: §7 #9 tab activation indicator [quick]

Wave 4 (docs + golden refresh, sequential):
├── T33: F-13 update AGENTS.md (5-tab → 4-tab) [quick]
└── T34: Regenerate intentional golden snapshots + make tests green [quick]

Wave FINAL (4 parallel reviews, then explicit user okay):
├── F1: Plan compliance audit [oracle]
├── F2: Code quality review [unspecified-high]
├── F3: Real manual QA via tui-validator re-probe [unspecified-high]
└── F4: Scope fidelity check [deep]
→ Present consolidated results → Get user okay

Critical Path: T1 → T7 → T8 → T10 → T34 → F1-F4 → user okay
Parallel Speedup: ~65% vs sequential (Waves 0, 2, 3 each have 6+ concurrent tasks)
Max Concurrent: 7 (Wave 2 page tracks parallel + Wave 3 cross-cutting)
```

### Dependency Matrix

- **T1**: — | blocks all
- **T2**: T1 | blocks T8, T9, T19
- **T3**: T1 | blocks T17
- **T4**: T1 | blocks T12
- **T5**: T1 | blocks T26
- **T6**: T1 | blocks T31
- **T7**: T1 | blocks all input-routing fixes (T8, T9, T15, T19, T22)
- **T8**: T2, T7 | blocks T13, T14
- **T9**: T2, T7 | blocks none
- **T10**: T7 | blocks none
- **T11**: T7 | blocks none
- **T12**: T4, T7 | blocks none
- **T13**: T8 | blocks none
- **T14**: T8 | blocks none
- **T15**: T7 | blocks T16
- **T16**: T15 | blocks none
- **T17**: T3 | blocks none
- **T18**: T7 | blocks none
- **T19**: T2, T7 | blocks T20
- **T20**: T19 | blocks none
- **T21**: T6 | blocks none
- **T22**: T7 | blocks none
- **T23**: T7 | blocks none
- **T24**: Wave 2 complete (touches profiles.go + models.go modal sites) | blocks none
- **T25**: T1 | blocks none
- **T26**: T5 | blocks none
- **T27**: T1 | blocks none
- **T28**: T1 | blocks none
- **T29**: T1 | blocks none
- **T30**: T1 | blocks none
- **T31**: T6, T21 | blocks none
- **T32**: T1 | blocks none
- **T33**: all impl tasks | blocks T34
- **T34**: T33 + all impl tasks | blocks F1-F4
- **F1-F4**: T34 | blocks user okay

### Agent Dispatch Summary

- **Wave 0** (6 tasks, all parallel): T1-T6 → `quick`
- **Wave 1** (1 task): T7 → `quick`
- **Wave 2** (16 tasks across 5 page tracks, parallel):
  - profiles.go (T8-T14, 7 tasks) → `unspecified-high`
  - models.go (T15-T18, 4 tasks) → `unspecified-high`
  - backends.go (T19-T20, 2 tasks) → `unspecified-high`
  - server.go (T21, 1 task) → `quick`
  - profile_editor (T22-T23, 2 tasks) → `quick`
- **Wave 3** (9 tasks, all parallel): T24 → `unspecified-high`, T25-T26 → `quick`, T27 → `unspecified-high`, T28-T32 → `quick`
- **Wave 4** (2 tasks, sequential): T33-T34 → `quick`
- **Wave FINAL** (4 parallel reviews): F1 → `oracle`, F2 → `unspecified-high`, F3 → `unspecified-high`, F4 → `deep`

---

## TODOs

> Implementation + Test = ONE Task. Never separate.
> EVERY task MUST have: Recommended Agent Profile + Parallelization info + QA Scenarios.

### Wave 0 — Baseline & Foundation

- [x] 1. Verify `make tests` green on main (sanity gate)

  **What to do**:
  - Checkout the working branch from `main` at commit `53924ba` (or `git stash` any uncommitted work)
  - Run `make tests` and capture full output to `.sisyphus/evidence/task-1-baseline.log`
  - Run `make build` and verify `bin/model-loader` is produced
  - If FAIL: STOP and report — plan execution cannot proceed on a red baseline

  **Must NOT do**:
  - Modify any source file
  - Skip flaky tests with `t.Skip` to "make them pass"

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Pure verification, no implementation
  - **Skills**: []
    - Reason: Single Bash invocation, no domain skill required

  **Parallelization**:
  - **Can Run In Parallel**: YES (with T2-T6)
  - **Parallel Group**: Wave 0
  - **Blocks**: All subsequent tasks (if RED → entire plan halts)
  - **Blocked By**: None — start immediately

  **References**:

  **Pattern References**:
  - `Makefile:tests` — Test target definition
  - `AGENTS.md` "COMMANDS" section — `make build`, `make tests`

  **WHY Each Reference Matters**:
  - `Makefile:tests` — Tells us the exact command to use (and whether it wraps `go test ./...` or has extra setup)
  - `AGENTS.md` — Confirms expected commands and conventions

  **Acceptance Criteria**:
  - [ ] `make tests` exits 0
  - [ ] `make build` exits 0
  - [ ] `bin/model-loader` exists and `bin/model-loader --version` runs

  **QA Scenarios**:

  ```
  Scenario: Baseline tests pass
    Tool: Bash
    Preconditions: Clean working tree at commit 53924ba
    Steps:
      1. cd /home/diogo/dev/model-loader && make tests 2>&1 | tee .sisyphus/evidence/task-1-baseline.log
      2. Verify exit code == 0
      3. grep -E "FAIL|PANIC" .sisyphus/evidence/task-1-baseline.log → no matches
    Expected Result: Exit 0, no FAIL/PANIC lines, all packages report ok
    Failure Indicators: Non-zero exit, any FAIL package
    Evidence: .sisyphus/evidence/task-1-baseline.log

  Scenario: Baseline build succeeds
    Tool: Bash
    Preconditions: same
    Steps:
      1. make build 2>&1 | tee .sisyphus/evidence/task-1-build.log
      2. test -x bin/model-loader && bin/model-loader --version
    Expected Result: bin/model-loader runs and prints version
    Evidence: .sisyphus/evidence/task-1-build.log
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-1-baseline.log`
  - [ ] `.sisyphus/evidence/task-1-build.log`

  **Commit**: NO (verification only)

- [x] 2. Extend `capturingPage` test double for filter state hooks

  **What to do**:
  - In `internal/ui/root_test.go`, extend the existing `capturingPage` test double (per AGENTS.md "TUI INPUT ROUTING RULES") to accept an injectable `IsCapturingInput() bool` so we can simulate filter-active states for downstream tests
  - Add helper constructors: `newCapturingPageWithFilter(active bool)` returning a page that reports capture only while flag is true
  - Add table-driven test verifying: when `capturingPage.IsCapturingInput()==true`, every printable rune (`a-z`, `0-9`, `?`, `/`) sent to `RootModel.Update` is forwarded (page model receives the key, no global action triggered)

  **Must NOT do**:
  - Modify existing `capturingPage` behavior signatures (only extend)
  - Add real filter logic (that's T8)

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Focused test scaffolding, single file
  - **Skills**: []
    - Reason: Standard Go testing, no domain skill needed

  **Parallelization**:
  - **Can Run In Parallel**: YES (with T1, T3-T6)
  - **Parallel Group**: Wave 0
  - **Blocks**: T8, T9, T19 (need the filter-capable double)
  - **Blocked By**: T1

  **References**:

  **Pattern References**:
  - `internal/ui/root_test.go` — existing `capturingPage` double (AGENTS.md mandates this pattern)
  - `AGENTS.md` "TUI INPUT ROUTING RULES" — paragraph on `IsCapturingInput`

  **API/Type References**:
  - `internal/ui/root.go` — `InputCapture` interface and `activePageCapturesInput()` helper

  **Test References**:
  - `internal/ui/root_test.go` — existing tests against `capturingPage` for keys like `q`, `1-5`

  **WHY Each Reference Matters**:
  - `capturingPage` is the documented mechanism for asserting input routing without standing up a real page. Extending it preserves the project's test idiom.
  - The "TUI INPUT ROUTING RULES" section in AGENTS.md explicitly says new global shortcut tests must use this double.

  **Acceptance Criteria**:
  - [ ] `go test -run TestCapturingPage -v ./internal/ui/...` passes
  - [ ] New helper `newCapturingPageWithFilter` exported within the test package
  - [ ] Table test exercises ≥ 10 printable runes and verifies forwarding (not consumption)

  **QA Scenarios**:

  ```
  Scenario: Filter-active capturing page swallows printable runes
    Tool: Bash
    Preconditions: T2 implementation merged locally
    Steps:
      1. go test -run TestCapturingPageFilter -v ./internal/ui/... | tee .sisyphus/evidence/task-2-filter-double.log
      2. Verify all sub-tests for runes a, e, n, q, /, ?, 1, 2 pass
    Expected Result: PASS for all sub-tests, no run with "received global action"
    Evidence: .sisyphus/evidence/task-2-filter-double.log
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-2-filter-double.log`

  **Commit**: YES (squashed with T3-T6 into Wave 0 foundation)

- [x] 3. Time formatter utility (IsZero → "—")

  **What to do**:
  - Add `func FormatTime(t time.Time) string` in `internal/ui/theme/layout.go` (or new `internal/ui/format.go` if more appropriate)
  - Returns `"—"` (em dash) when `t.IsZero()`, else `t.Format(time.RFC3339)` (matching current render format)
  - Add unit test covering: zero time → "—", recent time → RFC3339 string

  **Must NOT do**:
  - Change time format for non-zero times (preserve current rendering)
  - Touch `modelscanner` Go source (this is presentation-layer)

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Trivial pure function
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (Wave 0)
  - **Blocks**: T17 (Modified field render in models.go)
  - **Blocked By**: T1

  **References**:
  - `internal/ui/pages/models.go:670+` — site that currently renders `Modified:` (per audit F-10)
  - `internal/service/modelscanner` — source of `time.Time` value

  **Acceptance Criteria**:
  - [ ] `go test -run TestFormatTime -v ./internal/ui/theme/` passes
  - [ ] Test cases: `time.Time{}` → `"—"`, `time.Now()` → RFC3339 substring assertion

  **QA Scenarios**:
  ```
  Scenario: Zero time renders as em dash
    Tool: Bash
    Steps:
      1. go test -run TestFormatTime/zero -v ./internal/ui/theme/ | tee .sisyphus/evidence/task-3-time-fmt.log
    Expected Result: PASS, output contains "—"
    Evidence: .sisyphus/evidence/task-3-time-fmt.log
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-3-time-fmt.log`

  **Commit**: YES (squashed with T2/T4/T5/T6)

- [x] 4. Args pretty-printer utility (map → flag-per-line)

  **What to do**:
  - Add `func FormatArgs(args map[string]string) []string` in `internal/ui/format.go` (or theme/layout.go)
  - Output: sorted keys; each line `--{key} {value}` (skip key with empty value rendered as just `--{key}` for boolean flags)
  - Add unit test with audit-example map: `batch-size:4096 cache-type-k:q8_0 cache-type-v:q8_0 cont-batching:true ctx-size:131072 ...`

  **Must NOT do**:
  - Add CLI parsing or argument validation
  - Touch domain `Profile.Args` definition

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (Wave 0)
  - **Blocks**: T12 (Args render in profiles.go)
  - **Blocked By**: T1

  **References**:
  - `TUI-VALIDATOR.md` F-11 — current Go-map syntax bug
  - `internal/domain/profile.go` — `Profile.Args` type (verify map[string]string vs map[string]any)

  **Acceptance Criteria**:
  - [ ] `go test -run TestFormatArgs -v ./internal/ui/...` passes
  - [ ] Sorted alphabetically (deterministic)
  - [ ] Boolean-like value `"true"` still renders as `--cont-batching true` (not toggled off) — preserves current semantics

  **QA Scenarios**:
  ```
  Scenario: FormatArgs sorts and prefixes with --
    Tool: Bash
    Steps:
      1. go test -run TestFormatArgs -v ./internal/ui/... | tee .sisyphus/evidence/task-4-args-fmt.log
    Expected Result: PASS, snapshot lines [--batch-size 4096, --cache-type-k q8_0, ...]
    Evidence: .sisyphus/evidence/task-4-args-fmt.log
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-4-args-fmt.log`

  **Commit**: YES (squashed with T2/T3/T5/T6)

- [x] 5. Runewidth padding helper for prompt/labels

  **What to do**:
  - Add `func PadRuneWidth(s string, width int) string` and `func RuneWidth(s string) int` in `internal/ui/theme/layout.go`
  - Use `github.com/mattn/go-runewidth` (already a transitive dep via lipgloss; verify with `go mod why`; if not present add explicitly)
  - Function returns visual cell width accounting for CJK wide chars, emoji, combining marks (delegating to runewidth.StringWidth which handles ZWJ sequences acceptably)
  - Unit test cases: `"abc"` → 3, `"中文"` → 4, `"🚀😀"` → 4, `"áé̃"` → 2 (NFC) or 2 (NFD ok), `"123"` → 3

  **Must NOT do**:
  - Reinvent grapheme clustering (delegate to library)
  - Modify any existing call site (T26 consumes this)

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (Wave 0)
  - **Blocks**: T26 (wide-rune prompt fix)
  - **Blocked By**: T1

  **References**:
  - `TUI-VALIDATOR.md` F-14 — repro `┃ > 中文 한글 🚀😀` shifts prompt 4 cols
  - External: `https://pkg.go.dev/github.com/mattn/go-runewidth` — `StringWidth` API
  - `go.mod` — verify runewidth or rivo/uniseg presence; lipgloss vendors uniseg

  **Acceptance Criteria**:
  - [ ] `go test -run TestRuneWidth -v ./internal/ui/theme/` passes
  - [ ] 5+ test cases including CJK, emoji, combining mark, ASCII, empty string

  **QA Scenarios**:
  ```
  Scenario: Wide chars measured at 2 cells each
    Tool: Bash
    Steps:
      1. go test -run TestRuneWidth -v ./internal/ui/theme/ | tee .sisyphus/evidence/task-5-runewidth.log
    Expected Result: PASS for all cases
    Evidence: .sisyphus/evidence/task-5-runewidth.log
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-5-runewidth.log`

  **Commit**: YES (squashed with T2/T3/T4/T6)

- [x] 6. Shared empty-state component scaffold

  **What to do**:
  - In `internal/ui/components/empty_state.go` (file already exists per Glob — extend, don't rewrite), expose `func EmptyState(title, body, hint string) string` returning a centered lipgloss block
  - Reuse existing `theme.Subtle`/`theme.Dim` styles for body
  - Add render test asserting basic structure (title appears, hint appears, body wrapped)

  **Must NOT do**:
  - Replace existing usages yet (T21, T31 consume this)
  - Add interactive behavior (empty-state is read-only display)

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (Wave 0)
  - **Blocks**: T21 (Server empty), T31 (cross-page empty)
  - **Blocked By**: T1

  **References**:
  - `internal/ui/components/empty_state.go` — existing scaffold
  - `internal/ui/theme/theme.go` — colour palette + styles

  **Acceptance Criteria**:
  - [ ] `go test -run TestEmptyState -v ./internal/ui/components/` passes
  - [ ] Render contains all three text inputs (title/body/hint)

  **QA Scenarios**:
  ```
  Scenario: EmptyState renders title, body, hint
    Tool: Bash
    Steps:
      1. go test -run TestEmptyState -v ./internal/ui/components/ | tee .sisyphus/evidence/task-6-empty.log
    Expected Result: PASS
    Evidence: .sisyphus/evidence/task-6-empty.log
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-6-empty.log`

  **Commit**: YES (squashed with T2-T5 as `feat(ui): foundation helpers for tui-validator fix plan`)

---

### Wave 1 — Root-level Esc Gate

- [x] 7. F-01 — Esc gate at `RootModel.handleKey`

  **What to do**:
  - In `internal/ui/root.go:handleKey`, BEFORE forwarding to active page, add:
    ```
    if msg.String() == "esc" && !m.activePageCapturesInput() && !m.helpOpen {
        return m, nil
    }
    ```
  - This swallows `Esc` when no modal/form/picker/filter is active and the help modal isn't open
  - Help modal already handles its own Esc in `handleHelpKey` (line 315) — preserve that
  - ALSO audit `huh.Form` instances embedded in pages to confirm none have `WithAccessible(true)` calling `tea.Quit` on Esc (huh's default Abort → Cmd is a `tea.Quit` at program level — that's the F-01 root cause)
  - Write test using `capturingPage` double:
    - Test 1: page reports `IsCapturingInput()==false`, send Esc → `cmd` is nil (NOT `tea.Quit`)
    - Test 2: page reports `IsCapturingInput()==true`, send Esc → key is forwarded to page Update (page receives Esc, root does NOT quit)
    - Test 3: `helpOpen==true`, send Esc → help closes, app does NOT quit

  **Must NOT do**:
  - Remove `q` or `Ctrl+C` quit handling
  - Block Esc inside pages — only at root when no capture
  - Change `huh` library defaults (forbidden by guardrail)

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Single-file, well-defined input-routing fix with existing test pattern
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO — single task in Wave 1
  - **Parallel Group**: Wave 1 (solo)
  - **Blocks**: T8, T9, T15, T19, T22 (all input-routing tests depend on Esc not killing harness)
  - **Blocked By**: T1, T2

  **References**:

  **Pattern References**:
  - `internal/ui/root.go:handleKey` — current handler, no esc case
  - `internal/ui/root.go:handleHelpKey:~315` — existing esc handling for help

  **API/Type References**:
  - `internal/ui/root.go` — `InputCapture` interface, `activePageCapturesInput()`

  **Test References**:
  - `internal/ui/root_test.go` — existing test patterns for `q`, `1-5` keys
  - `internal/ui/root_test.go` — `capturingPage` double (extended in T2)

  **External References**:
  - `TUI-VALIDATOR.md` F-01 lines 94-117 — repro and root cause analysis
  - `AGENTS.md` "TUI INPUT ROUTING RULES" — gating contract

  **WHY Each Reference Matters**:
  - Audit F-01 names the exact files and the suspected huh-Quit cascade
  - AGENTS.md mandates the test pattern (paired test with `capturingPage`) for ANY new global shortcut handling — this is a defensive variant of that rule
  - `handleHelpKey` shows the precedent for how Esc was already handled in one specific context

  **Acceptance Criteria**:

  **TDD (RED-GREEN-REFACTOR)**:
  - [ ] RED: Write tests FIRST in `internal/ui/root_test.go` covering 3 scenarios above; tests FAIL on main
  - [ ] GREEN: Add gate in `handleKey`; tests PASS
  - [ ] REFACTOR: Extract to named helper `isUnhandledEsc(msg, page)` if logic grows >5 lines

  **Verification**:
  - [ ] `go test -run TestRootEsc -v ./internal/ui/` PASS (3 subtests)
  - [ ] `go vet ./...` clean
  - [ ] No regression in existing TestRoot* tests

  **QA Scenarios**:

  ```
  Scenario: Fresh boot + Esc on Profiles tab does NOT kill app (F-01 happy path)
    Tool: interactive_bash (tmux)
    Preconditions: make build succeeds; clean ~/.config/model-loader and ~/.local/state/model-loader
    Steps:
      1. tmux new-session -d -s ml-t7 -x 120 -y 40 './bin/model-loader'
      2. sleep 1
      3. tmux send-keys -t ml-t7 'Escape'
      4. sleep 1
      5. tmux capture-pane -t ml-t7 -p > .sisyphus/evidence/task-7-esc-profiles.txt
      6. Verify process still alive: tmux list-panes -t ml-t7 -F '#{pane_dead}' → "0"
      7. tmux send-keys -t ml-t7 'q'  # clean quit
    Expected Result: Pane still shows TUI grid (banner/tabs visible), pane_dead=0
    Failure Indicators: Pane shows shell prompt, pane_dead=1, capture is empty
    Evidence: .sisyphus/evidence/task-7-esc-profiles.txt

  Scenario: Esc on Backends tab does NOT kill app
    Tool: interactive_bash (tmux)
    Steps:
      1. tmux new-session -d -s ml-t7b -x 120 -y 40 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t7b '4'; sleep 1  # tab 4 = Backends
      3. tmux send-keys -t ml-t7b 'Escape'; sleep 1
      4. tmux capture-pane -t ml-t7b -p > .sisyphus/evidence/task-7-esc-backends.txt
      5. Verify pane_dead=0
    Expected Result: Backends tab still visible, app alive
    Evidence: .sisyphus/evidence/task-7-esc-backends.txt

  Scenario: Esc CLOSES help modal (regression check)
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t7c -x 120 -y 40 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t7c '?'; sleep 1
      3. tmux capture-pane -t ml-t7c -p > .sisyphus/evidence/task-7-help-open.txt
      4. tmux send-keys -t ml-t7c 'Escape'; sleep 1
      5. tmux capture-pane -t ml-t7c -p > .sisyphus/evidence/task-7-help-closed.txt
    Expected Result: help-open contains "Keybindings"; help-closed does not contain "Keybindings"; pane_dead=0
    Evidence: .sisyphus/evidence/task-7-help-{open,closed}.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-7-esc-profiles.txt`
  - [ ] `.sisyphus/evidence/task-7-esc-backends.txt`
  - [ ] `.sisyphus/evidence/task-7-help-open.txt`
  - [ ] `.sisyphus/evidence/task-7-help-closed.txt`
  - [ ] `.sisyphus/evidence/task-7-go-test.log`

  **Commit**: YES (standalone)
  - Message: `fix(ui): gate Esc in root handler to prevent app quit (F-01)`
  - Files: `internal/ui/root.go`, `internal/ui/root_test.go`
  - Pre-commit: `go test ./internal/ui/... && make tests`

---

### Wave 2 — Per-Page Tracks (parallel between tracks, sequential within)

#### Track 2a — `internal/ui/pages/profiles.go`

- [x] 8. F-04 — Filter capture via `list.FilterState()` + apply filter

  **What to do**:
  - In `internal/ui/pages/profiles.go`, expand `IsCapturingInput()` to also return `true` when `p.list.FilterState() != list.Unfiltered` (i.e. when `bubbles/list` is in `Filtering` or `FilterApplied` state)
  - Verify that `p.list.SetFilteringEnabled(true)` is set during init (if not, set it)
  - In `updateList`, when filter state is `Filtering`, do NOT match keystrokes against `listKeys.*` (n/e/d/x/r/p/I/u shortcuts) — forward straight to `list.Update`
  - Confirm filter actually reduces visible items (bubbles/list does this natively when `SetFilteringEnabled(true)` is on — verify via probe)
  - TDD: extend the table tests in `internal/ui/pages/profiles_test.go` using the extended `capturingPage`-style test or by stuffing a `tea.KeyMsg{Runes:[]rune("e")}` and asserting no Export was triggered

  **Must NOT do**:
  - Introduce a new `filterActive bool` field (Metis: filter is delegated to bubbles/list — no extra state)
  - Override `bubbles/list` internal filter rendering
  - Modify the filter UI string format

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: Touches large file (1141 LOC), requires careful state-machine reasoning
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (with other Wave 2 page tracks)
  - **Parallel Group**: Wave 2 / Track 2a (T8 → T9 → T10 → T11 → T12 → T13 → T14)
  - **Blocks**: T13, T14
  - **Blocked By**: T2, T7

  **References**:

  **Pattern References**:
  - `internal/ui/pages/profiles.go` — `IsCapturingInput()` (currently returns `p.editing || p.pickerActive || p.confirmDelete`)
  - `internal/ui/pages/models.go` — Models has a `filterMode bool` style; use as REFERENCE for what NOT to copy verbatim (bubbles/list handles it for Profiles)
  - `bubbles/list` source — `Model.FilterState()` returns `FilterState` enum: `Unfiltered`, `Filtering`, `FilterApplied`

  **API/Type References**:
  - `github.com/charmbracelet/bubbles/list` — `FilterState` enum and `SetFilteringEnabled(bool)`
  - `bubbles/list.Model.SettingFilter()` — bool helper for "currently typing in filter input"

  **Test References**:
  - `internal/ui/pages/profiles_test.go` — existing tests
  - `internal/ui/root_test.go` — `capturingPage` interface pattern

  **External References**:
  - `https://pkg.go.dev/github.com/charmbracelet/bubbles/list#Model.FilterState`
  - `TUI-VALIDATOR.md` F-04 lines 166-189 — exact repro + audit's misdiagnosis (corrected by Metis)

  **WHY Each Reference Matters**:
  - Metis confirmed there is NO `filterActive` field; the audit's prescription was wrong. The fix piggybacks on bubbles/list's own state.
  - `SettingFilter()` is the exact predicate we need for "user is typing in filter input now" vs `FilterApplied` (user pressed Enter, filter is active but not capturing keystrokes)

  **Acceptance Criteria**:

  **TDD**:
  - [ ] RED: Test injects keystrokes `/`, `Q`, `w`, `e`, `n` to `ProfilesPage`; asserts `e` and `n` do NOT trigger Export/New commands while filter input is active. FAILS on main.
  - [ ] GREEN: `IsCapturingInput()` returns true during `FilterState != Unfiltered`. `updateList` checks `p.list.SettingFilter()` before matching shortcuts.
  - [ ] REFACTOR: Extract shortcut-matching to `handleListShortcuts(msg) (tea.Cmd, bool)`; guard with capture check.

  **Verification**:
  - [ ] `go test -run TestProfilesFilter -v ./internal/ui/pages/` PASS
  - [ ] `make tests` green

  **QA Scenarios**:
  ```
  Scenario: Typing in filter does not trigger export/new (F-04 happy)
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t8 -x 120 -y 40 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t8 '/'; sleep 0.2
      3. tmux send-keys -t ml-t8 'Q'; tmux send-keys -t ml-t8 'w'; tmux send-keys -t ml-t8 'e'; tmux send-keys -t ml-t8 'n'; sleep 0.5
      4. tmux capture-pane -t ml-t8 -p > .sisyphus/evidence/task-8-filter.txt
      5. Verify capture shows "Filter: Qwen" (or bubbles/list equivalent) AND no editor is open AND list shows reduced items
      6. ls ~/.local/state/model-loader/exports/ 2>/dev/null | wc -l → expected 0 new files since T0 baseline
    Expected Result: Filter input contains "Qwen", list narrows, no editor opened, no export file created
    Evidence: .sisyphus/evidence/task-8-filter.txt

  Scenario: Filter actually narrows list (audit's "doesn't filter" sub-bug)
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t8b -x 120 -y 40 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t8b '/Qwen'; sleep 0.5
      3. tmux capture-pane -t ml-t8b -p > .sisyphus/evidence/task-8-narrow.txt
      4. grep -c "Gemma" .sisyphus/evidence/task-8-narrow.txt → expected 0
      5. grep -c "Qwen"  .sisyphus/evidence/task-8-narrow.txt → expected ≥ 1
    Expected Result: Only "Qwen*" profiles visible
    Evidence: .sisyphus/evidence/task-8-narrow.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-8-filter.txt`
  - [ ] `.sisyphus/evidence/task-8-narrow.txt`
  - [ ] `.sisyphus/evidence/task-8-go-test.log`

  **Commit**: YES
  - Message: `fix(profiles): route filter keystrokes via list.FilterState (F-04)`
  - Files: `internal/ui/pages/profiles.go`, `internal/ui/pages/profiles_test.go`
  - Pre-commit: `go test ./internal/ui/... && make tests`

- [x] 9. F-06 — Gate `e` (export) behind `IsCapturingInput` + visible flash

  **What to do**:
  - In `internal/ui/pages/profiles.go`, ensure the `e` shortcut handler ONLY fires when `IsCapturingInput()==false` (i.e. filter not active, no editor open, no picker)
  - Add a 2s visible flash message after export: `Exported 5 profiles to {path}` using existing `components/flash.go`
  - TDD: simulate `tea.KeyMsg{Runes:[]rune("e")}` to ProfilesPage with filter active → assert no `exportCmd` returned and no flash; with filter inactive → assert `exportCmd` returned

  **Must NOT do**:
  - Add a confirmation modal (audit suggests it but user did not ask; keep scope tight)
  - Change the export file naming/path

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES — independent of T8 within the file? NO — both edit `updateList` shortcut routing. **Sequential within Track 2a**: T8 first, then T9.
  - **Blocks**: nothing
  - **Blocked By**: T2, T7, T8 (shortcut-routing refactor lands first)

  **References**:
  - `internal/ui/pages/profiles.go` — current `e` handler
  - `internal/ui/components/flash.go` — flash message component
  - `TUI-VALIDATOR.md` F-06 lines 213-224

  **Acceptance Criteria**:

  **TDD**:
  - [ ] RED: test injects `e` while filter active; asserts no export Cmd produced. FAILS.
  - [ ] GREEN: gate applied. PASSES.
  - [ ] REFACTOR: consolidate gate with T8's `handleListShortcuts`.

  **QA Scenarios**:
  ```
  Scenario: `e` during filter does NOT export
    Tool: interactive_bash
    Steps:
      1. baseline=$(ls ~/.local/state/model-loader/exports/ 2>/dev/null | wc -l)
      2. tmux new-session -d -s ml-t9 -x 120 -y 40 './bin/model-loader'
      3. sleep 1; tmux send-keys -t ml-t9 '/e'; sleep 0.5
      4. tmux capture-pane -t ml-t9 -p > .sisyphus/evidence/task-9-filter-e.txt
      5. after=$(ls ~/.local/state/model-loader/exports/ 2>/dev/null | wc -l); test "$after" = "$baseline"
    Expected Result: no new export file
    Evidence: .sisyphus/evidence/task-9-filter-e.txt

  Scenario: `e` outside filter exports + flashes
    Tool: interactive_bash
    Steps:
      1. baseline=$(ls ~/.local/state/model-loader/exports/ 2>/dev/null | wc -l)
      2. tmux new-session -d -s ml-t9b -x 120 -y 40 './bin/model-loader'
      3. sleep 1; tmux send-keys -t ml-t9b 'e'; sleep 0.5
      4. tmux capture-pane -t ml-t9b -p > .sisyphus/evidence/task-9-export.txt
      5. grep -E "Exported [0-9]+ profile" .sisyphus/evidence/task-9-export.txt → match
      6. after=$(ls ~/.local/state/model-loader/exports/ 2>/dev/null | wc -l); test "$after" = "$((baseline+1))"
    Expected Result: flash message visible, 1 new file in exports dir
    Evidence: .sisyphus/evidence/task-9-export.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-9-filter-e.txt`
  - [ ] `.sisyphus/evidence/task-9-export.txt`

  **Commit**: YES
  - Message: `fix(profiles): gate export shortcut + visible flash (F-06)`
  - Files: `internal/ui/pages/profiles.go`, `internal/ui/pages/profiles_test.go`

- [x] 10. F-05 — Instrument `profilestore.LoadAll` + `handleLoaded` resilience

  **What to do**:
  - **Phase 1 (instrumentation, ~30 min):**
    - In `internal/service/profilestore` (the package implementing `LoadAll`), add `slog.DebugContext(ctx, "profilestore.LoadAll", "path", path, "count", len(profiles), "err", err)` at each return path
    - Boot the TUI, reproduce sequence from audit F-05 (`1 → / → Q w e n → BSpace×3 → Enter → Esc → Right → Enter → BSpace×3 → r`), capture log at `~/.cache/model-loader/log/*.log`
    - Determine root cause: empty slice vs error vs wrong cwd
  - **Phase 2 (defensive guard, after diagnosis):**
    - In `internal/ui/pages/profiles.go:handleLoaded`, when incoming message has empty items AND existing list already had items, do NOT replace; emit a flash `refresh failed: empty result (keeping current list)`
    - Log the suspicious load via slog at WARN level
  - TDD: simulated `loadedMsg{items: nil}` to ProfilesPage with prior items → list unchanged, flash visible

  **Must NOT do**:
  - Reach into bubbles/list internals for state preservation
  - Change `LoadAll` semantics beyond adding logging
  - "Fix" by retrying on empty result (mask the root cause)

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: Cross-package investigation + defensive coding
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (parallel with T11+ in same track? NO — touches profiles.go)
  - **Parallel Group**: Wave 2 / Track 2a sequential after T8, T9
  - **Blocks**: nothing
  - **Blocked By**: T7

  **References**:
  - `internal/service/profilestore/*.go` — `LoadAll` definition
  - `internal/ui/pages/profiles.go:handleLoaded` — message handler
  - `internal/log/` — slog wiring (per AGENTS.md)
  - `TUI-VALIDATOR.md` F-05 lines 194-208

  **WHY Each Reference Matters**:
  - Metis flagged the audit's "handleLoaded overwrites on error" as wrong: the audit shows zero profiles in UI but 5 files on disk → `LoadAll` itself returns empty without error. Instrumentation IS the actual first task.

  **Acceptance Criteria**:

  **TDD**:
  - [ ] RED: test fires `loadedMsg{items: nil}` after page already loaded 5 items; asserts list still shows 5. FAILS.
  - [ ] GREEN: guard applied. PASSES.
  - [ ] Investigation logs in evidence capture root cause

  **QA Scenarios**:
  ```
  Scenario: Refresh in contaminated state does NOT wipe list (F-05)
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t10 -x 120 -y 40 './bin/model-loader'
      2. sleep 1
      3. tmux send-keys -t ml-t10 '/'; sleep 0.2
      4. tmux send-keys -t ml-t10 'Qwen'; sleep 0.2
      5. tmux send-keys -t ml-t10 'BSpace BSpace BSpace BSpace'; sleep 0.2
      6. tmux send-keys -t ml-t10 'Enter'; sleep 0.2
      7. tmux send-keys -t ml-t10 'Escape'; sleep 0.2
      8. tmux send-keys -t ml-t10 'Right'; sleep 0.2
      9. tmux send-keys -t ml-t10 'Enter'; sleep 0.2
      10. tmux send-keys -t ml-t10 'BSpace BSpace BSpace'; sleep 0.2
      11. tmux send-keys -t ml-t10 'r'; sleep 1
      12. tmux capture-pane -t ml-t10 -p > .sisyphus/evidence/task-10-refresh.txt
      13. grep -E "(Gemma|Qwen|Hunyuan)" .sisyphus/evidence/task-10-refresh.txt | wc -l → ≥ 5
    Expected Result: list still shows 5+ profiles after r in contaminated state
    Evidence: .sisyphus/evidence/task-10-refresh.txt

  Scenario: Instrumentation reveals root cause
    Tool: Bash
    Steps:
      1. Reproduce above scenario
      2. grep "profilestore.LoadAll" ~/.cache/model-loader/log/*.log | tail -20 > .sisyphus/evidence/task-10-slog.log
    Expected Result: slog lines show count=0 OR count=5 AND err= details; root cause visible
    Evidence: .sisyphus/evidence/task-10-slog.log
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-10-refresh.txt`
  - [ ] `.sisyphus/evidence/task-10-slog.log`
  - [ ] `.sisyphus/evidence/task-10-root-cause.md` (short writeup of what the logs show)

  **Commit**: YES (2 commits: instrumentation then defensive guard)
  - First: `feat(profilestore): slog instrumentation for LoadAll (F-05 phase 1)`
  - Second: `fix(profiles): preserve list on empty refresh result (F-05 phase 2)`

- [x] 11. F-09 — Deduplicate pinned profile in list render

  **What to do**:
  - In `internal/ui/pages/profiles.go` (or `profiles_list.go` if that holds list-construction), when building list items: collect pinned profiles into a top-section AND exclude them from the bottom "all profiles" section
  - Render pinned with `📌` glyph prefix (or `★` if emoji is undesirable — match `theme.Pinned` if present)
  - TDD: build a fixture with 5 profiles, 1 pinned; assert resulting list has exactly 5 unique entries with pinned at position 0

  **Must NOT do**:
  - Remove the visual pinned section (one source of truth: top section, NOT bottom)
  - Modify domain `Profile.Pinned` field

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES — touches profiles_list.go (different from filter logic in profiles.go); but if shared functions, run sequential after T10
  - **Blocks**: nothing
  - **Blocked By**: T7 (anyway sequential within Track 2a for safety)

  **References**:
  - `internal/ui/pages/profiles_list.go` — list construction
  - `internal/domain/profile.go` — `Profile.Pinned` field

  **Acceptance Criteria**:
  - [ ] `go test -run TestProfilesListDedup -v ./internal/ui/pages/` PASS
  - [ ] List length == 5 (no duplicate of pinned entry)

  **QA Scenarios**:
  ```
  Scenario: Pinned profile appears once
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t11 -x 120 -y 40 './bin/model-loader'
      2. sleep 1
      3. tmux capture-pane -t ml-t11 -p > .sisyphus/evidence/task-11-list.txt
      4. grep -c "Gemma 4 E4B Q4_K_S" .sisyphus/evidence/task-11-list.txt → 1
    Expected Result: pinned profile shown exactly once with marker
    Evidence: .sisyphus/evidence/task-11-list.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-11-list.txt`

  **Commit**: YES
  - Message: `fix(profiles): deduplicate pinned profile in list render (F-09)`

- [x] 12. F-11 — Render `Args` as flag-per-line using `FormatArgs`

  **What to do**:
  - In `internal/ui/pages/profiles.go` right-panel rendering, replace `fmt.Sprintf("%v", profile.Args)` (or whatever produces the `map[k:v ...]` output) with `theme.FormatArgs(profile.Args)` (added in T4)
  - Render each returned line indented under the `Args:` label
  - Golden snapshot test: render right panel for a fixture profile, snapshot to `testdata/profiles_args.golden.txt`

  **Must NOT do**:
  - Sort by anything other than key (deterministic for golden tests)
  - Change the `Args:` label or position

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO (sequential within Track 2a)
  - **Blocked By**: T4, T7

  **References**:
  - `TUI-VALIDATOR.md` F-11
  - T4 output (`theme.FormatArgs`)

  **Acceptance Criteria**:
  - [ ] Golden snapshot test `TestProfilesRightPanelArgs` passes
  - [ ] `go test ./... -update` regenerates expected — committed intentionally

  **QA Scenarios**:
  ```
  Scenario: Args render line-by-line
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t12 -x 160 -y 50 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t12 'Down'; sleep 0.5
      3. tmux capture-pane -t ml-t12 -p > .sisyphus/evidence/task-12-args.txt
      4. grep -E "^\s*--[a-z]" .sisyphus/evidence/task-12-args.txt | wc -l → ≥ 5
      5. grep -c "map\[" .sisyphus/evidence/task-12-args.txt → 0
    Expected Result: at least 5 lines beginning with `--`, no Go-map syntax visible
    Evidence: .sisyphus/evidence/task-12-args.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-12-args.txt`

  **Commit**: YES
  - Message: `fix(profiles): render Args as flag-per-line (F-11)`
  - Files: `internal/ui/pages/profiles.go`, `testdata/profiles_args.golden.txt`

- [x] 13. §7 #5 — Case-insensitive filter by default

  **What to do**:
  - Configure the `bubbles/list` `FilterFunc` to use `strings.EqualFold`-style matching (case-insensitive substring)
  - Document in the filter prompt: render `/` prompt as `Filter (case-insensitive): ` OR add a small `i` indicator
  - TDD: filter "qwen" matches "Qwen3.6 35B"; filter "QWEN" matches likewise

  **Must NOT do**:
  - Add case-sensitivity toggle (scope creep)
  - Change filter syntax (regex, fuzzy, etc.)

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO (sequential after T8)
  - **Blocked By**: T8

  **References**:
  - `github.com/charmbracelet/bubbles/list` — `Model.Filter` (function), `Model.SetFilteringEnabled`

  **Acceptance Criteria**:
  - [ ] `go test -run TestProfilesFilterCaseInsensitive -v ./internal/ui/pages/` PASS

  **QA Scenarios**:
  ```
  Scenario: Uppercase query matches lowercase profile name
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t13 -x 120 -y 40 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t13 '/QWEN'; sleep 0.5
      3. tmux capture-pane -t ml-t13 -p > .sisyphus/evidence/task-13-case.txt
      4. grep -c "Qwen" .sisyphus/evidence/task-13-case.txt → ≥ 1
    Expected Result: Qwen profiles visible despite uppercase query
    Evidence: .sisyphus/evidence/task-13-case.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-13-case.txt`

  **Commit**: YES
  - Message: `feat(profiles): case-insensitive filter (§7 #5)`

- [x] 14. §7 #1 — Persist filter term on Enter

  **What to do**:
  - When user presses Enter while in `FilterState=Filtering`, `bubbles/list` transitions to `FilterApplied` and keeps the filter active (this is bubbles/list default behavior — verify, then make sure profiles.go does NOT clear the filter on subsequent shortcuts like `n`, `e`)
  - Add `Esc` to clear filter (already standard) and `Ctrl+u` to clear from inside filter input
  - TDD: filter "Qwen" → Enter → list still narrowed; press `e` (export) → export works AND filter remains applied; press Esc → filter cleared

  **Must NOT do**:
  - Override bubbles/list's filter state transitions
  - Add separate persistence layer

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO (sequential after T13)
  - **Blocked By**: T8, T13

  **References**:
  - `bubbles/list` — `FilterApplied` state behavior

  **Acceptance Criteria**:
  - [ ] Test asserts filter remains applied after Enter and after non-filter shortcuts

  **QA Scenarios**:
  ```
  Scenario: Filter persists after Enter and after pressing e
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t14 -x 120 -y 40 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t14 '/Qwen'; sleep 0.2
      3. tmux send-keys -t ml-t14 'Enter'; sleep 0.5
      4. tmux capture-pane -t ml-t14 -p > .sisyphus/evidence/task-14-persist-1.txt
      5. tmux send-keys -t ml-t14 'e'; sleep 1
      6. tmux capture-pane -t ml-t14 -p > .sisyphus/evidence/task-14-persist-2.txt
      7. grep -c "Gemma" .sisyphus/evidence/task-14-persist-2.txt → 0  (still filtered)
    Expected Result: list remains narrowed after Enter and after export
    Evidence: .sisyphus/evidence/task-14-persist-{1,2}.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-14-persist-1.txt`
  - [ ] `.sisyphus/evidence/task-14-persist-2.txt`

  **Commit**: YES
  - Message: `feat(profiles): persist filter on Enter (§7 #1)`

#### Track 2b — `internal/ui/pages/models.go`

- [x] 15. F-02 (part 1) — Info panel toggle behavior

  **What to do**:
  - In `internal/ui/pages/models.go`, change `i` handler from monotonic-open to toggle: `m.infoOpen = !m.infoOpen`
  - Ensure `Esc` closes info panel (set `m.infoOpen = false`) when info is open AND no other modal/picker is up
  - `IsCapturingInput()` should NOT include `infoOpen` (per Metis: info panel is read-only display, not input capture — global shortcuts `1`-`4`, `q`, `Tab` should still work)
  - TDD: 3 subtests: `i` opens, second `i` closes, `Esc` closes when open

  **Must NOT do**:
  - Block global tab switching when info is open (info is non-modal)
  - Change info panel content/data

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO — Track 2b sequential (T15 → T16 → T17 → T18)
  - **Blocks**: T16 (sizing fix relies on toggle state)
  - **Blocked By**: T7

  **References**:
  - `internal/ui/pages/models.go:670` (line cited in audit)
  - `TUI-VALIDATOR.md` F-02 lines 119-143

  **Acceptance Criteria**:
  - [ ] `go test -run TestModelsInfoToggle -v ./internal/ui/pages/` PASS (3 subtests)

  **QA Scenarios**:
  ```
  Scenario: i toggles info panel
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t15 -x 160 -y 50 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t15 '3'; sleep 0.5  # Models tab
      3. tmux send-keys -t ml-t15 'Down'; sleep 0.2
      4. tmux send-keys -t ml-t15 'i'; sleep 0.5
      5. tmux capture-pane -t ml-t15 -p > .sisyphus/evidence/task-15-info-open.txt
      6. tmux send-keys -t ml-t15 'i'; sleep 0.5
      7. tmux capture-pane -t ml-t15 -p > .sisyphus/evidence/task-15-info-closed.txt
      8. tmux send-keys -t ml-t15 'i'; sleep 0.2; tmux send-keys -t ml-t15 'Escape'; sleep 0.5
      9. tmux capture-pane -t ml-t15 -p > .sisyphus/evidence/task-15-info-esc.txt
    Expected Result:
      - open capture contains "Model Info" label
      - closed capture does NOT contain "Model Info"
      - esc capture does NOT contain "Model Info"
      - all 3 captures show app alive (pane_dead=0)
    Evidence: task-15-info-{open,closed,esc}.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-15-info-open.txt`
  - [ ] `.sisyphus/evidence/task-15-info-closed.txt`
  - [ ] `.sisyphus/evidence/task-15-info-esc.txt`

  **Commit**: YES
  - Message: `fix(models): info panel toggle (i/Esc) (F-02 part 1)`

- [x] 16. F-02 (part 2) — Info panel sizing (JoinHorizontal width allocation)

  **What to do**:
  - In `internal/ui/pages/models.go` View(), when `m.infoOpen==true`, ensure the table region width is REDUCED by the info panel width before render: `tableWidth = totalWidth - infoWidth - dividerWidth`
  - Verify `lipgloss.JoinHorizontal(lipgloss.Top, table, divider, info)` is the structure
  - Add golden snapshot test: render Models page at 160×50 with info open; snapshot to `testdata/models_info_open.golden.txt`
  - Verify by string parsing: no row of the table contains both a profile name AND an info field label (`File:`, `Modified:`) on the same line

  **Must NOT do**:
  - Change info panel layout
  - Resize the parent terminal

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO (sequential after T15)
  - **Blocked By**: T15

  **References**:
  - `TUI-VALIDATOR.md` F-02 — lines like `Qwen3.6-35B-A3B-…<Hunyuan-MT-7B.Q4_K_S.gguf>` proving overlap
  - `internal/ui/components/info_panel.go` — fixed width definition
  - Metis: this is sizing, NOT overlay rendering

  **Acceptance Criteria**:
  - [ ] Golden snapshot `testdata/models_info_open_160x50.golden.txt` matches
  - [ ] Parser test: no info-field label substring inside any table-row substring

  **QA Scenarios**:
  ```
  Scenario: Info panel does not overlap table cells
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t16 -x 160 -y 50 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t16 '3'; tmux send-keys -t ml-t16 'Down'; tmux send-keys -t ml-t16 'i'; sleep 0.5
      3. tmux capture-pane -t ml-t16 -p > .sisyphus/evidence/task-16-no-overlap.txt
      4. # Verify no line contains BOTH a model file name AND "File:" label
      5. awk '/\.gguf/ && /File:/' .sisyphus/evidence/task-16-no-overlap.txt → expected EMPTY
    Expected Result: clean separation between table and info panel
    Evidence: .sisyphus/evidence/task-16-no-overlap.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-16-no-overlap.txt`
  - [ ] `testdata/models_info_open_160x50.golden.txt` (committed)

  **Commit**: YES
  - Message: `fix(models): info panel width allocation (F-02 part 2)`

- [x] 17. F-10 — Modified `IsZero` → "—"

  **What to do**:
  - In Models page render (info panel + table row Modified column), replace direct `t.Format(time.RFC3339)` with `theme.FormatTime(t)` (from T3)
  - Same for any other `time.Time` field rendered (Created, Updated etc. if present)
  - TDD: snapshot a model entry with `Modified: time.Time{}` → renders `—`

  **Must NOT do**:
  - Change time semantics in `modelscanner`
  - Replace non-zero time renders

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO (sequential within Track 2b)
  - **Blocked By**: T3, T16

  **References**:
  - `TUI-VALIDATOR.md` F-10
  - T3 (`theme.FormatTime`)

  **Acceptance Criteria**:
  - [ ] Test fires a model with zero time; render contains `—` not `0001-01-01`

  **QA Scenarios**:
  ```
  Scenario: Zero modified time renders as em dash
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t17 -x 160 -y 50 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t17 '3'; tmux send-keys -t ml-t17 'i'; sleep 0.5
      3. tmux capture-pane -t ml-t17 -p > .sisyphus/evidence/task-17-modified.txt
      4. grep -c "0001-01-01" .sisyphus/evidence/task-17-modified.txt → 0
    Expected Result: no Go zero-time literal visible anywhere
    Evidence: .sisyphus/evidence/task-17-modified.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-17-modified.txt`

  **Commit**: YES
  - Message: `fix(models): em dash for zero Modified time (F-10)`

- [x] 18. F-15 — Selected-row indicator in Models table

  **What to do**:
  - In `internal/ui/pages/models.go`, when rendering table rows, prefix the row matching `m.cursor` with `▶ ` and apply `theme.Selected` background style
  - If using `bubbles/table`, configure `Table.SelectedStyle` instead
  - Golden snapshot at 120×40 with cursor at row 1

  **Must NOT do**:
  - Move cursor automatically (preserve current navigation)
  - Add icons unrelated to selection

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO (sequential within Track 2b)
  - **Blocked By**: T16

  **References**:
  - `TUI-VALIDATOR.md` F-15
  - `internal/ui/theme/theme.go` — `Selected` style (verify exists or add)

  **Acceptance Criteria**:
  - [ ] Golden snapshot includes `▶` marker on cursor row
  - [ ] Manual capture shows visual highlight

  **QA Scenarios**:
  ```
  Scenario: Cursor row visible
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t18 -x 120 -y 40 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t18 '3'; tmux send-keys -t ml-t18 'Down'; sleep 0.5
      3. tmux capture-pane -t ml-t18 -p > .sisyphus/evidence/task-18-cursor.txt
      4. grep -c "▶" .sisyphus/evidence/task-18-cursor.txt → ≥ 1
    Expected Result: ▶ marker present on one row
    Evidence: .sisyphus/evidence/task-18-cursor.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-18-cursor.txt`

  **Commit**: YES
  - Message: `feat(models): visible cursor marker on table (F-15)`

#### Track 2c — `internal/ui/pages/backends.go`

- [x] 19. F-07 — Implement filter in Backends tab

  **What to do**:
  - In `internal/ui/pages/backends.go`, enable filtering on the backends list:
    - If using `bubbles/list`, call `list.SetFilteringEnabled(true)` and let bubbles handle `/` natively
    - If using a custom list, mirror the Profiles filter pattern (T8)
  - Update `IsCapturingInput()` to include filter state
  - Add help footer hint `/ filter` (it's already in help — just make sure it works)
  - TDD: page test injects `/abc` and asserts filter UI visible AND list narrowed

  **Must NOT do**:
  - Diverge from Models/Profiles filter UX
  - Add filter-specific destructive shortcuts

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO — sequential in Track 2c (T19 → T20)
  - **Blocks**: T20
  - **Blocked By**: T2, T7

  **References**:
  - `internal/ui/pages/backends.go` — current handler
  - `internal/ui/pages/profiles.go` — reference filter pattern after T8
  - `TUI-VALIDATOR.md` F-07 lines 228-234

  **Acceptance Criteria**:
  - [ ] `go test -run TestBackendsFilter -v ./internal/ui/pages/` PASS
  - [ ] Filter narrows list when typed

  **QA Scenarios**:
  ```
  Scenario: / opens filter and narrows
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t19 -x 120 -y 40 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t19 '4'; sleep 0.5  # Backends
      3. tmux send-keys -t ml-t19 '/cublas'; sleep 0.5
      4. tmux capture-pane -t ml-t19 -p > .sisyphus/evidence/task-19-backends-filter.txt
      5. grep -c "Filter" .sisyphus/evidence/task-19-backends-filter.txt → ≥ 1
      6. grep -c "cublas" .sisyphus/evidence/task-19-backends-filter.txt → ≥ 1
    Expected Result: filter UI shown, only cublas backends visible
    Evidence: .sisyphus/evidence/task-19-backends-filter.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-19-backends-filter.txt`

  **Commit**: YES
  - Message: `feat(backends): implement / filter (F-07)`

- [x] 20. §7 #7 — Schema version in backend info panel

  **What to do**:
  - In `internal/ui/pages/backends.go` info panel render, alongside existing `SchemaRef:`, add `SchemaVersion:` line populated from `backendcatalog`/`backendschema` (whichever exposes the version string)
  - If version unavailable, render `—` (consistent with T3)
  - TDD: backend with known schema version renders that string

  **Must NOT do**:
  - Add schema-fetching network call
  - Refactor `backendschema` API

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO (sequential after T19)
  - **Blocked By**: T19

  **References**:
  - `internal/service/backendcatalog/` — catalog
  - `internal/service/backendschema/` — schema generation
  - `internal/service/llamahelp/embedded.go` — version pin `v7376`

  **Acceptance Criteria**:
  - [ ] Test asserts `SchemaVersion: v7376` for embedded llama backend

  **QA Scenarios**:
  ```
  Scenario: Backend info shows schema version
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t20 -x 160 -y 50 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t20 '4'; sleep 0.5
      3. tmux capture-pane -t ml-t20 -p > .sisyphus/evidence/task-20-backends-version.txt
      4. grep -E "SchemaVersion:.*(v[0-9]+|—)" .sisyphus/evidence/task-20-backends-version.txt → match
    Expected Result: SchemaVersion line present with version or em dash
    Evidence: .sisyphus/evidence/task-20-backends-version.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-20-backends-version.txt`

  **Commit**: YES
  - Message: `feat(backends): show schema version in info (§7 #7)`

#### Track 2d — `internal/ui/pages/server.go`

- [x] 21. F-08 — Empty-state placeholders + actionable hint for metrics dir

  **What to do**:
  - In `internal/ui/pages/server.go`, when no instances are running, render `components.EmptyState("No instances running", "Start one from Profiles tab", "Press 1 to switch")` using T6
  - For `H` (history) keypress when no metrics dir configured, render hint inline: `history: no metrics directory configured (set [logging.metrics_dir] in ~/.config/model-loader/config.toml)`
  - For `v` (cycle view) and `Space` (pause) when no logs, render `No active log stream` placeholder

  **Must NOT do**:
  - Auto-create the metrics dir
  - Modify proxy or process supervisor

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (independent of other tracks)
  - **Blocked By**: T6

  **References**:
  - `TUI-VALIDATOR.md` F-08
  - `internal/config/` — config.toml key names
  - `internal/ui/components/empty_state.go` (T6)

  **Acceptance Criteria**:
  - [ ] Server tab renders EmptyState component when no instances
  - [ ] H keypress emits hint containing the config key name

  **QA Scenarios**:
  ```
  Scenario: Server tab shows empty state
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t21 -x 120 -y 40 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t21 '2'; sleep 0.5
      3. tmux capture-pane -t ml-t21 -p > .sisyphus/evidence/task-21-server-empty.txt
      4. grep -E "No instances running" .sisyphus/evidence/task-21-server-empty.txt → match

  Scenario: H keypress emits actionable hint
    Tool: interactive_bash
    Steps:
      1. (continue prior session) tmux send-keys -t ml-t21 'H'; sleep 0.5
      2. tmux capture-pane -t ml-t21 -p > .sisyphus/evidence/task-21-h-hint.txt
      3. grep -E "metrics_dir" .sisyphus/evidence/task-21-h-hint.txt → match
    Expected Result: hint contains the exact config key
    Evidence: task-21-{server-empty,h-hint}.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-21-server-empty.txt`
  - [ ] `.sisyphus/evidence/task-21-h-hint.txt`

  **Commit**: YES
  - Message: `feat(server): empty-state placeholders for no-instances mode (F-08)`

#### Track 2e — `internal/ui/pages/profile_editor/editor.go`

- [x] 22. §7 #6 — Discard dialog accepts Enter/y

  **What to do**:
  - In `internal/ui/pages/profile_editor/editor.go`, the "Discard unsaved changes?" confirm has only `Discard` button visible per audit
  - Wire `Enter`, `y`, `Y` to trigger Discard action; `n`, `N`, Esc to cancel (keep)
  - Add explicit visible buttons in render: `[Discard]` `[Keep]` for clarity
  - TDD: 4 subtests for each key triggering Discard, 3 subtests for Keep paths

  **Must NOT do**:
  - Refactor the editor draft state machine (off-limits guardrail)
  - Change the confirm copy/text

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (independent track)
  - **Blocked By**: T7

  **References**:
  - `internal/ui/pages/profile_editor/editor.go:371` (esc handling)
  - `internal/ui/pages/profile_editor/editor.go:479` (esc handling)
  - `internal/ui/components/confirm.go` — confirm modal component

  **Acceptance Criteria**:
  - [ ] `go test -run TestEditorDiscardKeys -v ./internal/ui/pages/profile_editor/` PASS (7 subtests)
  - [ ] Editor draft state machine unchanged (verify by diff: only render + key handler touched)

  **QA Scenarios**:
  ```
  Scenario: Enter triggers Discard
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t22 -x 120 -y 40 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t22 '1'; sleep 0.3
      3. tmux send-keys -t ml-t22 'n'; sleep 0.5  # New profile
      4. tmux send-keys -t ml-t22 'abc'; sleep 0.3
      5. tmux send-keys -t ml-t22 'Escape'; sleep 0.5  # triggers discard confirm
      6. tmux capture-pane -t ml-t22 -p > .sisyphus/evidence/task-22-confirm.txt
      7. tmux send-keys -t ml-t22 'Enter'; sleep 0.5
      8. tmux capture-pane -t ml-t22 -p > .sisyphus/evidence/task-22-discarded.txt
      9. grep -c "Discard" .sisyphus/evidence/task-22-discarded.txt → 0  (returned to list)
    Expected Result: editor closed, list visible
    Evidence: task-22-{confirm,discarded}.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-22-confirm.txt`
  - [ ] `.sisyphus/evidence/task-22-discarded.txt`

  **Commit**: YES
  - Message: `feat(editor): discard dialog accepts Enter/y (§7 #6)`

- [x] 23. §7 #10 — Args grouping by category in advanced view

  **What to do**:
  - In `internal/ui/pages/profile_editor/editor.go` advanced args section, group flags by category (kv-cache, threads, ctx, batch, sampling, server, debug)
  - Category metadata can be heuristic (`strings.HasPrefix`/contains) or driven by `internal/service/llamahelp` schema if it exposes categories
  - Render with bold category headers and indented flags
  - DISPLAY-only change — do NOT touch draft state machine

  **Must NOT do**:
  - Refactor draft state machine (guardrail)
  - Move flags between categories destructively (each flag belongs to exactly one)

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO (sequential after T22 in Track 2e)
  - **Blocked By**: T22

  **References**:
  - `internal/service/llamahelp/` — flag schema with possible category hints
  - `TUI-VALIDATOR.md` §7 #10

  **Acceptance Criteria**:
  - [ ] Render lists ≥ 4 category headers
  - [ ] Every flag appears under exactly one category

  **QA Scenarios**:
  ```
  Scenario: Advanced view shows grouped categories
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t23 -x 160 -y 50 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t23 '1'; tmux send-keys -t ml-t23 'Down'; tmux send-keys -t ml-t23 'E'; sleep 0.5
      3. tmux send-keys -t ml-t23 'C-t'; sleep 0.5  # toggle Advanced
      4. tmux capture-pane -t ml-t23 -p > .sisyphus/evidence/task-23-grouped.txt
      5. grep -cE "^(KV Cache|Threads|Context|Batch|Sampling|Server|Debug)" .sisyphus/evidence/task-23-grouped.txt → ≥ 4
    Expected Result: at least 4 category headers visible
    Evidence: .sisyphus/evidence/task-23-grouped.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-23-grouped.txt`

  **Commit**: YES
  - Message: `feat(editor): group advanced args by category (§7 #10)`

---

### Wave 3 — Cross-Cutting Components & Theme (all parallel)

- [x] 24. F-03 — Modal opaque background (audit consumers)

  **What to do**:
  - Verify `internal/ui/components/modal.go` `Modal()` function produces opaque background (via `lipgloss.Place` with background color set)
  - Audit consumers: `profiles.go` (action modal, profile editor mount), `models.go` (action modal), any other `Modal(...)` invocations via Grep
  - Fix each consumer that returns `lipgloss.JoinHorizontal(left, divider, right)` AND THEN overlays the modal — instead, return `Modal(content)` exclusively when modal is active
  - Visual golden snapshot at 120×40: open Models action modal, snapshot has no `gguf` filename outside the modal box

  **Must NOT do**:
  - Rewrite `Modal()` semantics
  - Change modal positioning/sizing

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (Wave 3) — touches modal.go consumers across multiple pages but read-only audit + small fixes
  - **Blocked By**: Wave 2 complete (so we don't conflict with active profiles.go/models.go work)

  **References**:
  - `internal/ui/components/modal.go`
  - `internal/ui/components/overlay.go`
  - `TUI-VALIDATOR.md` F-03

  **Acceptance Criteria**:
  - [ ] Golden snapshot of Models action modal has no leaked text outside modal frame
  - [ ] Golden snapshot of Profiles editor has no list/details visible behind editor
  - [ ] All `Modal()` call sites traced and reviewed (commented at each call site or PR description)

  **QA Scenarios**:
  ```
  Scenario: Models action modal does not leak underlying table
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t24 -x 120 -y 40 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t24 '3'; tmux send-keys -t ml-t24 'Down'; tmux send-keys -t ml-t24 'Enter'; sleep 0.5
      3. tmux capture-pane -t ml-t24 -p > .sisyphus/evidence/task-24-modal-models.txt
      4. # Inside modal frame (rows containing "│") should NOT also contain table column headers
      5. awk '/│.*Action for/,/╰/' .sisyphus/evidence/task-24-modal-models.txt > .sisyphus/evidence/task-24-modal-only.txt
      6. grep -c "Hunyuan-MT" .sisyphus/evidence/task-24-modal-only.txt → ≤ 1 (only in modal title)

  Scenario: Profile editor does not leak list behind it
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t24b -x 120 -y 40 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t24b 'n'; sleep 0.5
      3. tmux capture-pane -t ml-t24b -p > .sisyphus/evidence/task-24-modal-editor.txt
      4. grep -c "Gemma" .sisyphus/evidence/task-24-modal-editor.txt → 0  (no list bleed-through)
    Expected Result: editor presents clean overlay
    Evidence: task-24-modal-{models,editor}.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-24-modal-models.txt`
  - [ ] `.sisyphus/evidence/task-24-modal-editor.txt`

  **Commit**: YES
  - Message: `fix(ui): opaque modal background across consumers (F-03)`

- [x] 25. F-12 — Help modal scrollable via viewport.Model

  **What to do**:
  - In `internal/ui/components/help.go`, wrap content in `bubbles/viewport.Model`
  - Wire `Up`, `Down`, `PgUp`, `PgDn`, `k`, `j` to viewport scroll messages
  - On open, scroll to "Current Page" section so it's first visible (deduced via `ActiveTab` passed to help)
  - Snapshot test at 80×24 with content longer than viewport: scroll Down 5 times, snapshot

  **Must NOT do**:
  - Change help content semantics
  - Make help non-modal

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (Wave 3)
  - **Blocked By**: T1

  **References**:
  - `internal/ui/components/help.go`
  - `github.com/charmbracelet/bubbles/viewport`
  - `TUI-VALIDATOR.md` F-12

  **Acceptance Criteria**:
  - [ ] `go test -run TestHelpScroll -v ./internal/ui/components/` PASS
  - [ ] At 80×24, help title visible immediately on open

  **QA Scenarios**:
  ```
  Scenario: Help scrolls at 80x24
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t25 -x 80 -y 24 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t25 '?'; sleep 0.5
      3. tmux capture-pane -t ml-t25 -p > .sisyphus/evidence/task-25-help-top.txt
      4. tmux send-keys -t ml-t25 'PageDown'; sleep 0.3
      5. tmux capture-pane -t ml-t25 -p > .sisyphus/evidence/task-25-help-down.txt
      6. # Captures must differ
      7. diff .sisyphus/evidence/task-25-help-top.txt .sisyphus/evidence/task-25-help-down.txt | wc -l → > 0
    Expected Result: scroll changes visible content
    Evidence: task-25-help-{top,down}.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-25-help-top.txt`
  - [ ] `.sisyphus/evidence/task-25-help-down.txt`

  **Commit**: YES
  - Message: `fix(help): scrollable modal via viewport (F-12)`

- [x] 26. F-14 — Wide-rune prompt padding

  **What to do**:
  - In any prompt that prefixes with `┃ > ` (huh inputs, filter prompts, etc.), compute padding using `theme.RuneWidth` (T5) instead of `len()` or `utf8.RuneCountInString`
  - Specifically audit `internal/ui/components/info_panel.go`, `internal/ui/pages/profile_editor/editor.go`, and `internal/ui/pages/profiles.go` for prompt rendering
  - Visual snapshot: profile name field with input `中文 한글 🚀😀` — `┃` stays at column 0

  **Must NOT do**:
  - Modify huh internals (use external wrapper)
  - Disable wide chars from input

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (Wave 3)
  - **Blocked By**: T5

  **References**:
  - `TUI-VALIDATOR.md` F-14
  - T5 (`theme.RuneWidth`/`PadRuneWidth`)

  **Acceptance Criteria**:
  - [ ] Snapshot capture shows `┃` at column 0 with CJK/emoji input
  - [ ] No regression with ASCII

  **QA Scenarios**:
  ```
  Scenario: Wide-rune input keeps prompt aligned
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t26 -x 120 -y 40 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t26 'n'; sleep 0.3  # New
      3. tmux send-keys -t ml-t26 '中文 한글 🚀😀'; sleep 0.3
      4. tmux capture-pane -t ml-t26 -p > .sisyphus/evidence/task-26-wide.txt
      5. # First col of every line containing `┃` should be `┃` (or fixed column position)
      6. awk '/┃/' .sisyphus/evidence/task-26-wide.txt | head -1 | grep -P '^[^ ]*┃' → match
    Expected Result: `┃` stays at consistent column
    Evidence: .sisyphus/evidence/task-26-wide.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-26-wide.txt`

  **Commit**: YES
  - Message: `fix(ui): use runewidth for prompt padding (F-14)`

- [x] 27. F-16 — Narrow-width responsive layout ratio

  **What to do**:
  - In `internal/ui/theme/layout.go`, define width-based breakpoints:
    - width < 100 cols: right panel collapses to bottom (stacked layout) OR is hidden behind a toggle
    - width 100-160: 60/40 split (left/right)
    - width > 160: 50/50
  - Apply ratio in Profiles and Backends View() rendering
  - Test: render at 60×20, 80×24, 120×40, 160×50, 200×60 — capture each, verify no truncated labels

  **Must NOT do**:
  - Hide critical information at narrow widths (truncate gracefully with ellipsis if needed)
  - Hard-code widths in pages (centralize in theme/layout.go)

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (Wave 3)
  - **Blocked By**: T1

  **References**:
  - `TUI-VALIDATOR.md` F-16
  - `internal/ui/theme/layout.go` — existing ratios

  **Acceptance Criteria**:
  - [ ] 5 golden snapshots at the listed sizes
  - [ ] No label truncated mid-word

  **QA Scenarios**:
  ```
  Scenario: 80x24 keeps labels readable
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t27a -x 80 -y 24 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t27a '4'; sleep 0.5
      3. tmux capture-pane -t ml-t27a -p > .sisyphus/evidence/task-27-80x24.txt
      4. grep -E "(Crea|Upda|Sche|Tags)" .sisyphus/evidence/task-27-80x24.txt → expected empty (no truncation)
    Expected Result: no label cut mid-word at 80x24

  Scenario: 200x60 doesn't waste horizontal space
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t27b -x 200 -y 60 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t27b '4'; sleep 0.5
      3. tmux capture-pane -t ml-t27b -p > .sisyphus/evidence/task-27-200x60.txt
    Evidence: task-27-{80x24,200x60}.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-27-80x24.txt`
  - [ ] `.sisyphus/evidence/task-27-200x60.txt`

  **Commit**: YES
  - Message: `fix(ui): responsive layout ratio for narrow widths (F-16)`

- [x] 28. §7 #2 — Conditional footer keys

  **What to do**:
  - In `internal/ui/components/statusbar.go`, accept a `mode` parameter (e.g. `selected`, `filtering`, `running`) and render only the 5 most-relevant hints + `?` for full help
  - Update `profiles.go`, `models.go`, `backends.go` to pass current mode
  - Render exemplar: in `selected` mode → `enter:launch E:edit n:new d:dup /:filter ?`; in `filtering` → `↑↓:nav enter:apply esc:cancel ?`

  **Must NOT do**:
  - Add new keybindings
  - Remove existing keybindings (only adjust rendering)

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (Wave 3)
  - **Blocked By**: T1 (and ideally Wave 2 complete to know mode transitions)

  **References**:
  - `internal/ui/components/statusbar.go`
  - `TUI-VALIDATOR.md` §7 #2

  **Acceptance Criteria**:
  - [ ] Footer width fits 80 cols without wrap
  - [ ] Different mode emits different hints (snapshot test)

  **QA Scenarios**:
  ```
  Scenario: Footer adapts to mode
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t28 -x 80 -y 24 './bin/model-loader'
      2. sleep 1
      3. tmux capture-pane -t ml-t28 -p | tail -3 > .sisyphus/evidence/task-28-footer-selected.txt
      4. tmux send-keys -t ml-t28 '/'; sleep 0.3
      5. tmux capture-pane -t ml-t28 -p | tail -3 > .sisyphus/evidence/task-28-footer-filter.txt
      6. diff .sisyphus/evidence/task-28-footer-selected.txt .sisyphus/evidence/task-28-footer-filter.txt | wc -l → > 0
    Expected Result: footer content differs between modes
    Evidence: task-28-footer-{selected,filter}.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-28-footer-selected.txt`
  - [ ] `.sisyphus/evidence/task-28-footer-filter.txt`

  **Commit**: YES
  - Message: `feat(ui): conditional footer keys per mode (§7 #2)`

- [x] 29. §7 #3 — Loading spinners for launch/probe

  **What to do**:
  - In `internal/ui/components/loading.go`, expose `Spinner` component using `bubbles/spinner`
  - Wire to: profile launch (instant render on Enter pre-instance), backend probe (P keypress), HF search (s keypress)
  - Hide on result or timeout (3s fallback)

  **Must NOT do**:
  - Block user input during spinner
  - Add fake delays

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (Wave 3)
  - **Blocked By**: T1

  **References**:
  - `internal/ui/components/loading.go`
  - `github.com/charmbracelet/bubbles/spinner`

  **Acceptance Criteria**:
  - [ ] Backend probe shows spinner during 54ms test latency (verify via capture mid-flight)
  - [ ] Spinner disappears on result

  **QA Scenarios**:
  ```
  Scenario: Probe shows spinner
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t29 -x 120 -y 40 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t29 '4'; tmux send-keys -t ml-t29 'P'; sleep 0.05
      3. tmux capture-pane -t ml-t29 -p > .sisyphus/evidence/task-29-probe-mid.txt
      4. sleep 1
      5. tmux capture-pane -t ml-t29 -p > .sisyphus/evidence/task-29-probe-done.txt
      6. grep -E "(⠋|⠙|⠹|◴|◷|◶|◵|⣾|⣽|⣻)" .sisyphus/evidence/task-29-probe-mid.txt → match (any spinner glyph)
    Expected Result: spinner visible during probe
    Evidence: task-29-probe-{mid,done}.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-29-probe-mid.txt`
  - [ ] `.sisyphus/evidence/task-29-probe-done.txt`

  **Commit**: YES
  - Message: `feat(ui): spinners for launch/probe/search (§7 #3)`

- [x] 30. §7 #4 — `theme.Dim` on `│` separator

  **What to do**:
  - In `internal/ui/theme/theme.go`, define `Separator` style with `theme.Dim` colour
  - Update `JoinHorizontal(left, divider, right)` call sites in `profiles.go`, `models.go`, `backends.go` to apply `Separator` to the divider string

  **Must NOT do**:
  - Change the separator character (`│`)
  - Add separator styling complexity beyond colour

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (Wave 3)
  - **Blocked By**: T1 (small risk of conflict with T27 if both edit theme/layout.go — sequence T30 after T27)

  **References**:
  - `internal/ui/theme/theme.go`
  - `TUI-VALIDATOR.md` §7 #4

  **Acceptance Criteria**:
  - [ ] Render snapshot shows dim-coloured separator (ANSI escape distinct)

  **QA Scenarios**:
  ```
  Scenario: Separator rendered with dim colour
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t30 -x 120 -y 40 './bin/model-loader'
      2. sleep 1
      3. tmux capture-pane -t ml-t30 -p -e > .sisyphus/evidence/task-30-sep.ansi  # -e preserves escapes
      4. # ANSI escape for dim is "\033[2m"; verify present on lines containing `│`
      5. grep -P "\x1b\[2m.*│" .sisyphus/evidence/task-30-sep.ansi → match
    Expected Result: dim ANSI escape paired with separator
    Evidence: .sisyphus/evidence/task-30-sep.ansi
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-30-sep.ansi`

  **Commit**: YES
  - Message: `style(ui): dim separator (§7 #4)`

- [x] 31. §7 #8 — Empty-state designs across pages

  **What to do**:
  - Apply `components.EmptyState` (T6) to:
    - Models tab with filter showing 0 matches
    - Backends tab without a default backend set
    - Server tab without instances (already covered by T21 — verify integration)
  - Each empty state has title + body + hint

  **Must NOT do**:
  - Auto-create entities (e.g. "create default backend?" button)
  - Hide tabs

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (Wave 3)
  - **Blocked By**: T6, T21

  **References**:
  - `TUI-VALIDATOR.md` §7 #8

  **Acceptance Criteria**:
  - [ ] 3 empty-state captures (models filter no-match, backends no-default, server already T21)

  **QA Scenarios**:
  ```
  Scenario: Models filter no-match shows empty state
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t31 -x 120 -y 40 './bin/model-loader'
      2. sleep 1; tmux send-keys -t ml-t31 '3'; tmux send-keys -t ml-t31 '/xyzqqqx'; sleep 0.5
      3. tmux capture-pane -t ml-t31 -p > .sisyphus/evidence/task-31-models-empty.txt
      4. grep -cE "No models match" .sisyphus/evidence/task-31-models-empty.txt → ≥ 1
    Expected Result: friendly empty-state message
    Evidence: .sisyphus/evidence/task-31-models-empty.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-31-models-empty.txt`

  **Commit**: YES
  - Message: `feat(ui): empty-state designs across pages (§7 #8)`

- [x] 32. §7 #9 — Tab activation indicator fix

  **What to do**:
  - In `internal/ui/components/tab_bar.go`, ensure exactly ONE rendering of the active tab title (audit reports "Profiles" appearing duplicated below the button)
  - Active tab uses `theme.Active` background; inactive uses `theme.Inactive`
  - Golden snapshot at 80×24 and 200×60: only one "Profiles" label

  **Must NOT do**:
  - Change tab order
  - Add new tabs

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (Wave 3)
  - **Blocked By**: T1

  **References**:
  - `internal/ui/components/tab_bar.go`
  - `TUI-VALIDATOR.md` §7 #9

  **Acceptance Criteria**:
  - [ ] Snapshot test: each tab title appears exactly once

  **QA Scenarios**:
  ```
  Scenario: No duplicate tab title
    Tool: interactive_bash
    Steps:
      1. tmux new-session -d -s ml-t32 -x 200 -y 60 './bin/model-loader'
      2. sleep 1
      3. tmux capture-pane -t ml-t32 -p > .sisyphus/evidence/task-32-tabs.txt
      4. grep -oP "(?<=\s)Profiles(?=\s|$)" .sisyphus/evidence/task-32-tabs.txt | wc -l → 1
    Expected Result: "Profiles" label appears exactly once
    Evidence: .sisyphus/evidence/task-32-tabs.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-32-tabs.txt`

  **Commit**: YES
  - Message: `fix(ui): single tab activation label (§7 #9)`

---

### Wave 4 — Docs & Golden Refresh

- [x] 33. F-13 — Update `AGENTS.md` (5-tab → 4-tab)

  **What to do**:
  - In `AGENTS.md` (the project's `CLAUDE.md` equivalent at workspace root), replace `Charmbracelet TUI with 5-tab model` with `Charmbracelet TUI with 4-tab model`
  - Audit the file for any other references to "5 tabs" or numeric tab counts; reconcile
  - Update the project STRUCTURE block's `5 tabs + profile_editor` line to `4 tabs + profile_editor`

  **Must NOT do**:
  - Modify content beyond tab-count fact-check
  - Touch other AGENTS.md sections unrelated to TUI tab count

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO — Wave 4 is sequential and depends on all impl tasks
  - **Blocked By**: All Wave 3 tasks

  **References**:
  - `AGENTS.md` lines around "STRUCTURE" and "UNIQUE STYLES"
  - `TUI-VALIDATOR.md` F-13

  **Acceptance Criteria**:
  - [ ] `grep -c "5-tab" AGENTS.md` → 0
  - [ ] `grep -c "4-tab" AGENTS.md` → ≥ 1

  **QA Scenarios**:
  ```
  Scenario: AGENTS.md reflects 4-tab reality
    Tool: Bash
    Steps:
      1. grep -n "tab" AGENTS.md > .sisyphus/evidence/task-33-tabs.txt
      2. grep -c "5-tab" AGENTS.md
      3. grep -c "4-tab" AGENTS.md
    Expected Result: 0 hits for "5-tab", ≥ 1 for "4-tab"
    Evidence: .sisyphus/evidence/task-33-tabs.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-33-tabs.txt`

  **Commit**: YES
  - Message: `docs: AGENTS.md reflects 4-tab model (F-13)`

- [x] 34. Regenerate intentional golden snapshots + final `make tests` green

  **What to do**:
  - Run `go test ./... -update` ONLY for the test files that the plan intentionally regenerates (T12 Args golden, T16 models info-open golden, T18 selected-row golden, T25 help scroll golden, T27 narrow-width goldens, T32 tab-bar golden)
  - DIFF each regenerated golden against `main` for sanity (no surprise content changes)
  - Run `make tests` final; capture green output
  - Run `make build`; verify binary

  **Must NOT do**:
  - Regenerate goldens for files unrelated to plan
  - Skip diff review

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO (sequential after T33)
  - **Blocked By**: T33

  **References**:
  - `AGENTS.md` "CONVENTIONS" — golden test update procedure

  **Acceptance Criteria**:
  - [ ] `make tests` exits 0
  - [ ] Only golden files corresponding to intentional regeneration tasks are modified

  **QA Scenarios**:
  ```
  Scenario: Final tests green and goldens intentional
    Tool: Bash
    Steps:
      1. go test ./... -update -run "TestProfilesRightPanelArgs|TestModelsInfoOpen|TestModelsSelectedRow|TestHelpScroll|TestNarrowWidth|TestTabBar" | tee .sisyphus/evidence/task-34-update.log
      2. git status --porcelain testdata/ | tee .sisyphus/evidence/task-34-changed-goldens.txt
      3. # Verify only expected files changed
      4. make tests | tee .sisyphus/evidence/task-34-final-tests.log
      5. make build | tee .sisyphus/evidence/task-34-build.log
    Expected Result: only intended goldens changed; make tests exit 0; build succeeds
    Evidence: .sisyphus/evidence/task-34-*.log/.txt
  ```

  **Evidence to Capture**:
  - [ ] `.sisyphus/evidence/task-34-update.log`
  - [ ] `.sisyphus/evidence/task-34-changed-goldens.txt`
  - [ ] `.sisyphus/evidence/task-34-final-tests.log`
  - [ ] `.sisyphus/evidence/task-34-build.log`

  **Commit**: YES
  - Message: `test: regenerate intentional golden snapshots`
  - Files: `testdata/*.golden.{txt,json}`

---

## Final Verification Wave (MANDATORY — after ALL implementation tasks)

> 4 review agents run in PARALLEL. ALL must APPROVE. Present consolidated results to user and get explicit "okay" before completing.
>
> **Do NOT auto-proceed after verification. Wait for user's explicit approval before marking work complete.**
> **Never mark F1-F4 as checked before getting user's okay.** Rejection or user feedback → fix → re-run → present again → wait for okay.

- [x] F1. **Plan Compliance Audit** — `oracle`
  Read this plan end-to-end. For each "Must Have": verify implementation exists (read file, grep for symbol, run command). For each "Must NOT Have": search codebase for forbidden patterns — reject with file:line if found. Check evidence files exist in `.sisyphus/evidence/`. Compare deliverables against plan. Cross-reference every F-01..F-16 and §7 #1..#10 to a corresponding task completion + test/golden + QA evidence.
  Output: `Must Have [N/N] | Must NOT Have [N/N] | Tasks [N/N] | Findings [16/16] | Suggestions [10/10] | VERDICT: APPROVE/REJECT`

- [x] F2. **Code Quality Review** — `unspecified-high`
  Run `make build` + `make tests` + `go vet ./...` + `gofmt -l .`. Review all changed files for: bare `interface{}` without comment, empty error returns, `fmt.Println` in non-test code, commented-out code, unused imports, AI slop (excessive comments, over-abstraction, generic names: `data/result/item/temp`). Verify no modifications to `huh`/`bubbles` vendor/library code.
  Output: `Build [PASS/FAIL] | Tests [N pass/N fail] | vet [PASS/FAIL] | gofmt [PASS/FAIL] | Files [N clean/N issues] | VERDICT`

- [x] F3. **Real Manual QA** — `unspecified-high` (+ `tui-validator` skill)
  Re-run the `tui-validator` skill on a fresh build (`make build`). Execute every QA scenario from every task — exact steps, capture evidence. Test cross-task integration (filter + selection + modal + Esc sequence). Test edge cases: terminal resize during filter, paste-buffer in filter, rapid tab switching during refresh, info open + resize. Save to `.sisyphus/evidence/final-qa/`. Compare findings list with original `TUI-VALIDATOR.md`. Recurrence of ANY F-01..F-16 = REJECT.
  Output: `Scenarios [N/N pass] | Original findings [0/16 recurrent] | Integration [N/N] | Edge cases [N tested] | VERDICT`

- [x] F4. **Scope Fidelity Check** — `deep`
  For each task: read "What to do", read actual diff (`git log -p` per commit). Verify 1:1 — everything in spec was built (no missing), nothing beyond spec was built (no creep). Check "Must NOT do" compliance per task. Detect cross-task contamination: T8 modifying models.go, T15 modifying profiles.go, etc. Flag any change to `profile_editor` draft state machine. Flag any new keybinding beyond F-07.
  Output: `Tasks [N/N compliant] | Contamination [CLEAN/N issues] | Off-limits violations [CLEAN/N] | New keybindings [F-07 only / N extras] | VERDICT`

---

## Commit Strategy

> Group by file domain. One commit per task by default. Squash mergeable trivials into a single "wave 0 foundation" commit.

- **T1**: NO COMMIT (verification only)
- **T2**: `test(ui): extend capturingPage double for filter state` — `internal/ui/root_test.go`, pre-commit: `go test ./internal/ui/...`
- **T3-T6** (foundation utilities): single squashed commit `feat(ui): add layout/format utility helpers` — `internal/ui/theme/*.go`, `internal/ui/components/empty_state.go`, pre-commit: `make tests`
- **T7**: `fix(ui): gate Esc in root handler to prevent app quit (F-01)` — `internal/ui/root.go`, `internal/ui/root_test.go`, pre-commit: `go test ./internal/ui/...`
- **T8-T14** (profiles.go track): one commit each, prefix `fix(profiles): ...`
- **T15-T18** (models.go track): one commit each, prefix `fix(models): ...`
- **T19-T20** (backends.go track): one commit each, prefix `feat(backends): ...` / `fix(backends): ...`
- **T21**: `feat(server): empty-state placeholders for no-instances mode (F-08)` — pre-commit: `make tests`
- **T22-T23** (editor track): one commit each, prefix `feat(editor): ...`
- **T24-T32** (cross-cutting): one commit each, prefix by component/theme
- **T33**: `docs: AGENTS.md reflects 4-tab model (F-13)` — `AGENTS.md`, pre-commit: none
- **T34**: `test: regenerate intentional golden snapshots` — `testdata/*.golden.json`, pre-commit: `make tests`

---

## Success Criteria

### Verification Commands
```bash
make build                                              # Expected: bin/model-loader produced, exit 0
make tests                                              # Expected: 0 failures
go vet ./...                                            # Expected: 0 warnings
gofmt -l . | wc -l                                      # Expected: 0
git log --oneline main..HEAD | wc -l                    # Expected: 20+ commits (one per task minimum)
ls .sisyphus/evidence/ | wc -l                          # Expected: ≥ 26 evidence files (one per finding + suggestion)
grep -c "5-tab" AGENTS.md                               # Expected: 0
grep -c "4-tab" AGENTS.md                               # Expected: ≥ 1
```

### Final Checklist
- [ ] All "Must Have" items present (16 findings + 10 suggestions delivered)
- [ ] All "Must NOT Have" guardrails honored (no huh/bubbles modification, no scope creep into profile_editor draft, no new keybindings beyond F-07)
- [ ] All tests pass (`make tests` green)
- [ ] All golden snapshots intentional (no accidental regenerations)
- [ ] `AGENTS.md` reflects 4-tab reality
- [ ] tui-validator re-probe shows zero recurrence of F-01..F-16
- [ ] User has reviewed F1-F4 outputs and given explicit "okay"

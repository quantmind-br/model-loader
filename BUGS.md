# BUGS — model-loader

**Status legend:** 🔴 Open &nbsp;·&nbsp; 🟡 Partial / Mitigated &nbsp;·&nbsp; 🟢 Fixed &nbsp;·&nbsp; ⚪ Won't fix / by design / N/A &nbsp;·&nbsp; ❓ Unknown

**Severity legend:** **C** Critical &nbsp;·&nbsp; **H** High &nbsp;·&nbsp; **M** Medium &nbsp;·&nbsp; **L** Low &nbsp;·&nbsp; **I** Info

This file is the **single source of truth for known defects**. It absorbs and supersedes the original `BUG_REPORT.md` (2026-04-29), which was deleted; its context is preserved in [Original report context](#original-report-context).

**Validated & resolved:** 2026-06-23. Every entry was re-checked against the working tree on `main`, then all actionable defects were fixed in the same pass (`go build ./...` → exit 0; `go test ./...` → all ok). As of that pass **no entry was `🔴 Open`** — everything was Fixed or a deliberate by-design/non-bug. **Update 2026-07-02:** defect **[S1](#s1)** (curated `spec-type` enum blocked chained / `draft-dflash` speculative decoding, with no `extraArgs` override) was opened and **fixed in the same pass** (Fix #1 list-valued enum + Fix #2 `extraArgs` passthrough; `draft-dflash` added; schemas regenerated). As of now **no entry is `🔴 Open`**. See [Resolution summary](#resolution-summary).

> ℹ **Scope note (2026-07-02):** this 2026-06-23 pass covered the 2026-04-29 TUI bug report, the log-streaming audit, doc drift, and test gaps — the L/B/D/T series below. It **predates** the HTTP-proxy API translation (Anthropic/Responses/Gemini routes), the benchmark dashboard redesign + the three agentic modes (terminal-bench/swe-bench-pro/deep-swe), and dual-GPU/tensor-parallel support. Those subsystems carry **no tracked defects here yet**; file new entries if a defect is confirmed in them.

---

## Resolution summary

The previous tracker revision was **pessimistic**: six entries marked Open were already fixed in the tree. The 2026-06-23 pass validated all 25 entries and then closed every remaining gap:

| Outcome | IDs | What changed this session |
|---------|-----|---------------------------|
| Already fixed (validation confirmed) | B1, B2, B4, B5, B6, B7, B8, B10, B11, B12, B13, D3, D4, D5, T3 | — |
| Fixed this session | B9, D1, D2, D6, L3, T1, T2 | see entries |
| Closed as by-design / non-bug | B3, L1, L2 | code is correct as-is; rationale documented |

Highlights of the fixes applied:

- **B9** — new in-app `[X] remove broken search path` action on the Models tab, persisted to `config.toml` via `config.UpdateSearchPaths`.
- **D1** — `README.md` rewritten for multi-backend framing; removed the defunct "Launcher" tab; the Profiles/Server shortcut tables and the create-profile flow now match `help.go` and the code.
- **D2/D6** — `internal/service/AGENTS.md` (and its `CLAUDE.md` symlink) updated `v7376` → `v9761`.
- **L3** — corrected the regex-format description in `monitor/AGENTS.md` (it matches `N tokens per second`, not `eval time = …`).
- **T1/T2** — added regression tests for the partial-line flush and the backpressure drop contract.
- **L1/L2** — documented as deliberate behavior (see entries) and pinned L1 with a regression test.

---

## Index

### L-series — Log-streaming audit
| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| [L1](#l1) | L | ⚪ | `internal/service/monitor/logs.go:51-72` | Partial line IS flushed (favors crash visibility); fragmentation trade-off is intentional |
| [L2](#l2) | I | ⚪ | `internal/service/monitor/logs.go:23-42` | `os.Open`→`Add` window is covered by the initial `emit()`; no real loss |
| [L3](#l3) | I | 🟢 | `internal/service/monitor/metrics.go:53` | Regex is llama.cpp-specific (harmless wasted work); doc description corrected |

### B-series — Original TUI bug report (2026-04-29)
| ID | Sev (then) | Status | One-line |
|----|------------|--------|----------|
| [B1](#b1) | C | 🟢 | Tab/Shift+Tab swallowing inputs in forms & sub-views |
| [B2](#b2) | C | ⚪ | (Was a B1 confirmation; no separate defect) |
| [B3](#b3) | H | ⚪ | `L` shortcut documented but not implemented (tab removed; doc already clean) |
| [B4](#b4) | H | 🟢 | Models action menu missing "Use in existing profile" |
| [B5](#b5) | H | 🟢 | `huh` form in Models action menu doesn't submit on Enter |
| [B6](#b6) | M | 🟢 | Status bar global hint omits `[?] help` |
| [B7](#b7) | M | 🟢 | Profile detail-pane hint omits `[L]` (collapsed into B3) |
| [B8](#b8) | M | 🟢 | `Duplicate` carried the source `port`; risked bind collision |
| [B9](#b9) | M | 🟢 | Invalid scan path in Models header — truncated **and** now removable in-app |
| [B10](#b10) | L | 🟢 | Models filter shows `filter: ""` while filtering (race) |
| [B11](#b11) | L | 🟢 | `ProfilesPage` does not reload on tab focus |
| [B12](#b12) | L | 🟢 | Editor preserves draft after accidental global Tab |
| [B13](#b13) | Doc | 🟢 | Monitor footer claimed `[Tab] cycle view` while Tab was global |

### D-series — Documentation drift
| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| [D1](#d1) | M | 🟢 | `AGENTS.md` / `README.md` | Both rewritten for multi-backend; README shortcuts realigned to `help.go` |
| [D2](#d2) | M | 🟢 | `AGENTS.md` / `internal/service/AGENTS.md` | Schema version `v9761` everywhere (see D6) |
| [D3](#d3) | M | 🟢 | `AGENTS.md:162` (NOTES) | `Schema version: embedded-v9761` |
| [D4](#d4) | M | 🟢 | `internal/service/monitor/AGENTS.md` | "6 goroutines per subscription" — confirmed correct |
| [D5](#d5) | M | 🟢 | `internal/service/processmgr/AGENTS.md` | Log path documented as `<profile-id>-<port>.log` |
| [D6](#d6) | L | 🟢 | `internal/service/AGENTS.md` | Service-layer KB `v7376` → `v9761` |

### T-series — Test-coverage gaps
| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| [T1](#t1) | M | 🟢 | `monitor/logs_test.go` | Regression test for partial-line flush added |
| [T2](#t2) | M | 🟢 | `monitor/subscribe_test.go` | Backpressure-drop test added |
| [T3](#t3) | L | 🟢 | `processmgr/manager_test.go` | `PYTHONUNBUFFERED=0` override already covered |

### S-series — Curated schema & validation
| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| [S1](#s1) | M | 🟢 | curated enums + `validator/rules.go` | Stale/scalar curated enums rejected binary-valid values (llama `spec-type` comma-list & `draft-dflash`; sglang parsers); fixed via list-valued enum + `extraArgs` passthrough |

---

## L-series — Log-streaming audit

### L1
- **Severity:** L (Low)
- **Status:** ⚪ Won't fix / by design — original "dropped on exit" claim was a misdiagnosis
- **Component:** `internal/service/monitor/logs.go:51-72` (`emit()`)
- **Finding:** `emit()` *does* flush a trailing partial line. The `if len(line) > 0` branch (`logs.go:54`) runs before the `io.EOF` return (`logs.go:65`), so a final `ReadString` returning `("partial", io.EOF)` emits `"partial"`. A backend that crashes mid-line still surfaces that line. This is the **desired** behavior — it makes the last crash message visible immediately.
- **Residual trade-off:** a single logical line split across two OS flushes can be emitted as two lines (fragmentation). A carry-buffer fix would rejoin them but would *delay* a final partial line until cancel/teardown — worse for crash visibility. Since fragmentation is rare (llama.cpp and unbuffered Python flush per line), we keep the current behavior.
- **This session:** pinned the behavior with a regression test (`TestLogFollower_FlushesTrailingPartialLine`, see [T1]) and corrected the `monitor/AGENTS.md` "Edge case" note that wrongly claimed the line was lost.

### L2
- **Severity:** I (Info)
- **Status:** ⚪ By design / non-bug
- **Component:** `internal/service/monitor/logs.go:23-42` (`newLogFollower`)
- **Finding:** The window between `os.Open` (`logs.go:27`) and `w.Add` (`logs.go:36`) cannot drop content: the initial `emit()` (`logs.go:73`, after `Add`) reads from offset 0 to EOF, so any bytes written during the window are read by that first flush. A post-`Add` write always fires a `Write` event. No real-world loss path exists.
- **This session:** no code change; reordering `Open`/`Add` would only shuffle error semantics for zero functional gain.

### L3
- **Severity:** I (Info)
- **Status:** 🟢 Fixed (documentation) — runtime cost was always negligible
- **Component:** `internal/service/monitor/metrics.go:53`
- **Finding:** `runLogPump` (`subscribe.go:111`) feeds every log line to `observeLog`, whose regex (`tokensPerSecRe`, `metrics.go:10`) matches `N tokens per second`. For vLLM/SGLang it never matches; tokens/s for those backends comes from the slot `n_decoded` diff (`observeSlots`). The match-then-discard cost is ~100 ns/line — harmless.
- **This session:** corrected `monitor/AGENTS.md`, which mis-stated the regex format as `eval time = … ms (N tokens, …)`. The optional micro-opt (gate the regex on `kind == llama.cpp`) is left for a future perf pass — not worth a code change for ~100 ns/line.

---

## B-series — Original TUI bug report

### B1
- **Status:** 🟢 Fixed. `internal/ui/root.go:264-272` gates every non-capturing shortcut behind `activePageCapturesInput()` (`root.go:376-381`), driven by the `InputCapture` contract (`contracts.go:21-23`). `ctrl+c` is the only `Captures==true` binding. Covered by `internal/ui/root_test.go`. Unblocks B5, B12, B13.

### B2
- **Status:** ⚪ N/A. The original report flagged this as a non-bug ("Sem bug aqui").

### B3
- **Status:** ⚪ Won't fix / by design. The `Launcher` tab was removed in the 5-tab refactor (`root.go:18-24`, `tabCount = 5`); launching is the `enter` action on Profiles. `help.go` already documents `enter` (not `L`), and `profiles.go:266` omits `L`. Nothing left to remove.

### B4
- **Status:** 🟢 Fixed. `models_actions.go:210-217` adds the "Use in existing profile" option (gated on `p.store != nil`); `commitRootAction` handles `"existing"` (`models_actions.go:107-125`) by opening a `ProfilePicker` over `store.List()`. Wired via `WithProfileStore` (`models.go:140-143`).

### B5
- **Status:** 🟢 Fixed. The fragile `huh.Form`-wrapped Select was replaced by an inline `actionMenu` (`models_actions.go:22-34`) that commits on `enter` in `updateActionMenu` (`models_actions.go:71-91`).

### B6
- **Status:** 🟢 Fixed. `statusbar.go:12` sets `globalHints = "[1-5] tabs  [tab] next  [q] quit" + components.HelpToken`, with `HelpToken = "  [?] help"` (`components/statusbar.go:16`). Seeded in `root.go:110`.

### B7
- **Status:** 🟢 Fixed (with B3). Profiles hint (`profiles.go:266`): `[enter] launch  [e] edit  [n] new  [d] dup  [X] del  [K] unload  [/] filter  (more: ?)` — no stale `L`.

### B8
- **Status:** 🟢 Fixed by design. `reservedArgs = []string{"port"}` (`fs_store.go:275`) is stripped on `Get` (`:101`) and `Save` (`:117`); `Duplicate` (`fs_store.go:201-223`) clones via `Get`+`Save`. `prepareLaunch` (`launch.go:27-39`) allocates a fresh ephemeral port per launch. A duplicate can never carry a port.

### B9
- **Status:** 🟢 Fixed (this session) — header pollution was already fixed; the missing in-app removal is now implemented.
- **Already fixed:** `handleScanEvent` maps `fs.ErrNotExist` to `"path not found"` (`models_scan.go:94-95`); `renderStatus` truncates other errors to 30 chars (`models_scan.go:264`); failed-root empty state is explicit (`models.go:251-255`).
- **Fix applied:** new `[X]` action on the Models Library view removes every errored search path and persists the new list:
  - `config.UpdateSearchPaths` (`config/config.go`) rewrites `models.search_paths` via viper (same pattern as the `default_tab` migration; comments not preserved).
  - `ModelsPage.WithSearchPathPersister` injects it (`main.go:101`).
  - `askRemoveBrokenPaths` → confirm modal → `performRemoveBrokenPaths` (`models_scan.go`) drops errored roots, persists, rescans, flashes; nil-safe when no persister is wired.
  - Keybinding `X` (`models_messages.go`), shown in `Hints()` only when `hasErrorRoot()`, and documented in `help.go`.
  - Tests: `TestModelsPage_RemoveBrokenPaths`, `TestModelsPage_RemoveBrokenPaths_NoPersister`, `TestUpdateSearchPathsAt_RewritesAndPreservesOtherValues`.

### B10
- **Status:** 🟢 Fixed. Filtering is synchronous in a single `Update`: `handleFilterKey` (`models_messages.go:108-149`) mutates `p.filter` and calls `refreshRows()` immediately for backspace/space/runes; `refreshRows` (`models_scan.go:117-147`) recomputes `visibleFiles()` synchronously — no goroutine to race the view. Bursted multi-rune `KeyMsg`s are appended whole (INPUT-01).

### B11
- **Status:** 🟢 Fixed. `ProfilesPage.Reload` (`profiles_update.go:235-237`) returns `loadCmd()`; `RootModel.activate` (`root.go:356-363`) calls `Reload()` on tab focus for any `Reloader`. (Models and Server implement it too.)

### B12
- **Status:** 🟢 Fixed. Profile create/edit moved to the web editor (`configweb.Session`; `profiles.go:62-66,146-148`); no inline `huh` draft to strand, and B1 stops global Tab leaking into editable surfaces.

### B13
- **Status:** 🟢 Fixed. `server_update.go:108-109` cycles the sub-view on `v` (`% 4` views); `help.go:36` advertises `[v] cycle …`.

---

## D-series — Documentation drift

> Each `CLAUDE.md` is a symlink to its sibling `AGENTS.md` (root, `internal/service`, `internal/service/monitor`, `internal/service/processmgr`, `internal/config`), so one edit covers both.

### D1
- **Status:** 🟢 Fixed (this session). Root `AGENTS.md:13` already used the multi-backend framing; `README.md` was the laggard. Fixes applied to `README.md`:
  - opener rewritten to "inference server profiles … across multiple backends";
  - features rephrased; the defunct **Launcher** bullet replaced with the proxy hot-swap flow;
  - the Profiles and Server shortcut tables realigned to `help.go` (`e`/`X`/`K`/`R`/`E` casing, removed nonexistent `b`/`k`); the create-profile Quick Start now describes the **web editor**, not the old inline `huh` form.

### D2
- **Status:** 🟢 Fixed. Root `AGENTS.md` already said `v9761` (matches `llamahelp/embedded.go:19` and `backendschema/golden_embed.go`). The remaining `v7376` in the service-layer KB was fixed as [D6] this session. Historical `docs/superpowers/plans/*` and the `e.g. embedded-v7376` format example in `.claude/commands/backend-schema-update.md` are intentionally left.

### D3
- **Status:** 🟢 Fixed. `AGENTS.md:162` — "Schema version: embedded-v9761".

### D4
- **Status:** 🟢 Fixed / confirmed correct. `monitor/AGENTS.md:5,14` say "6 goroutines"; the code starts exactly 6 producer/pump goroutines in `subscribe.go` (the 7th, `closeOnDone`, only waits on the `WaitGroup` and is rightly excluded).

### D5
- **Status:** 🟢 Fixed. `processmgr/AGENTS.md:23` documents `<profile-id>-<port>.log`; matches `launch.go:102` / `:189`.

### D6
- **Status:** 🟢 Fixed (this session). `internal/service/AGENTS.md` lines 20 and 49 changed `v7376` → `v9761` (the `CLAUDE.md` symlink reflects it). Now consistent with `llamahelp/embedded.go:19`.

---

## T-series — Test-coverage gaps

### T1
- **Status:** 🟢 Fixed (this session). `TestLogFollower_FlushesTrailingPartialLine` (`monitor/logs_test.go`) writes `"first\npartial"` (no trailing `\n`) and asserts both `first` and `partial` are emitted — pinning the L1 behavior against a regression that would silently drop a final partial line.

### T2
- **Status:** 🟢 Fixed (this session). `TestSubscribe_DropsOnBackpressure` (`monitor/subscribe_test.go`) bursts 2000 writes against an undrained channel and asserts `cancel()` still returns promptly — proving the pumps drop (non-blocking `select … default`) instead of stalling.

### T3
- **Status:** 🟢 Fixed (already present). `manager_test.go:225-243` covers both override cases for `buildLaunchEnv`: a profile `PYTHONUNBUFFERED=0` and an inherited `PYTHONUNBUFFERED=0` are preserved (never overwritten with `=1`), exercising the `envHasKey` guard (`launch.go:272`).

---

## S-series — Curated schema & validation

### S1
- **Severity:** M (Medium)
- **Status:** 🟢 Fixed (2026-07-02) — Fix #1 (list-valued enum) + Fix #2 (`extraArgs` passthrough) applied; `draft-dflash` added to the curated `spec-type` enum; llama-server schemas regenerated.
- **Component:** curated backend schema `spec-type` FlagSpec (`~/.config/model-loader/backends/schemas/llama.cpp-stable.json`, and `llama.cpp-nightly.json`) + `internal/service/validator/rules.go` — `checkEnum` (`:78`/`:88`, the `args` path via `:105`), the extra-args enum branch (`:259-265`), and the unknown-in-extra-args warning (`:202`).
- **Finding:** The b9847 `llama-server --help` documents `--spec-type` as a **comma-separated list**: `none,draft-simple,draft-eagle3,draft-mtp,draft-dflash,ngram-simple,ngram-map-k,ngram-map-k4v,ngram-mod,ngram-cache` ("comma-separated list of types of speculative decoding to use"). The curated schema models it as a **single-value enum** (`Type: 4` / `FlagTypeEnum`) whose `EnumValues` are the individual types **and is missing `draft-dflash`** entirely. `checkEnum` compares the whole arg string against `EnumValues`, so any comma-list (e.g. `draft-mtp,ngram-mod`) or `draft-dflash` fails as `"…" not in […]` at **Severity 1 (blocking)** → `profile validate` exits 2 and the launch path refuses it.
- **No override path:** `extraArgs` is the documented escape hatch for "binary-real but schema-absent" flags, but it only bypasses validation for **unknown** flags (warning-only, `rules.go:202`). A **known** flag such as `--spec-type` placed in `extraArgs` is still canonicalized and enum-checked (`rules.go:259-265`) → the same blocking error. Verified on this tree:
  - `--cache-ram 16384` / `--totally-fake-flag-xyz 1` in `extraArgs` → `warning … valid` (exit 0).
  - `--spec-type draft-mtp` (in-enum) in `extraArgs` → valid (exit 0) — proves extra-args known flags are canonicalized, not passed through raw.
  - `--spec-type draft-mtp,ngram-mod` in `extraArgs` **or** `args` → `error: spec-type: "draft-mtp,ngram-mod" not in […]`, exit 2.
- **Impact:** Chained speculative decoding (`draft-mtp,ngram-mod` — the recommended default for agent/coding MTP GGUFs: draftless ngram fires on re-emitted code while MTP covers novel tokens) and upstream **DFlash** (`draft-dflash`, new in b9847) are **unreachable** through model-loader for the `llama-server` kinds, with no supported workaround (hand-editing the curated-frozen schema is banned). Plain single-value spec-type (`draft-mtp`, `ngram-mod`, …) is unaffected and works. Surfaced while optimizing the profile `ornith-aeon-35b-a3b-q4km-mtp-vision-layer2-256k` (kept plain `draft-mtp`; the ngram-mod chain cannot pass validation).
- **Same failure class (not just `spec-type`):** any curated `FlagTypeEnum` whose `EnumValues` is stale vs the binary hits this same wall. Confirmed instances on this tree: `sglang-stable`/`sglang-nightly` `tool-call-parser` (enum `[deepseekv3,glm,gpt-oss,kimi_k2,llama3,mistral,qwen,hermes]` — lacks `qwen3_coder`/`qwen3_xml`) and `reasoning-parser` (`[deepseek-r1,deepseek-v3,glm45,gpt-oss,kimi,qwen3]` — lacks `qwen3-thinking`/`glm47`), both `Type: 4`. A model that needs one of those parsers cannot be served on sglang via model-loader either — `extraArgs` is blocked identically (vLLM's parser flags are free-form strings / `Type: 3`, so they are unaffected). The fix must be general (below), not a one-off for `spec-type`.
- **Repro:** set `args["spec-type"] = "draft-mtp,ngram-mod"` (or `extraArgs += ["--spec-type","draft-mtp,ngram-mod"]`) on any `llama.cpp-stable` profile → `model-loader profile validate <id>` → exit 2, `spec-type: "draft-mtp,ngram-mod" not in […]`.
- **Fix applied (2026-07-02):**
  1. **List-valued enum (Fix #1).** Added `List bool` (`json:"list,omitempty"`) to `domain.FlagSpec`, mirrored in `FlagSpecRow` + `BuildFlagSchema` (`internal/domain/`), and propagated in `mergeWithCurated` (`backendschema/merge.go`). `validator.checkEnum` (`rules.go`) now splits a `List` enum value on `,` (trimming each element and dropping empties via `splitTrim`, so `"draft-mtp, ngram-mod"` and `"draft-mtp,,ngram-mod"` validate), accepts a JSON array (`[]any`/`[]string`), rejects an empty result, and validates each element; a scalar (`List=false`) enum still compares the whole string. Added `draft-dflash` to the curated `spec-type` `EnumValues` (`backendschema/curated_llama.go`) and marked it `List` via a new `listEnumFlag` helper.
  2. **`extraArgs` real passthrough (Fix #2).** `applyExtraArgsRules` no longer type/enum/range-checks **known** flags supplied via `extraArgs` — it emits only the existing unknown-flag warning (plus the bare-value error). This restores the documented escape hatch generally, so any stale curated enum (llama `spec-type`, sglang `tool-call-parser`/`reasoning-parser`, etc.) is bypassable via `extraArgs`. The dead `checkExtraArgType` (and its `strconv` import) were removed; the value-token peek is kept so `--flag value` is not misread as a bare value.
  3. **Gated `Type` overlay (engages Fix #1 on disk).** `mergeWithCurated` now honors the curated `Type` **only** for list-valued enums (`curatedSpec.List && curatedSpec.Type == FlagTypeEnum`). The live `--help` parses `--spec-type` as `Type:3` (it inlines values as a comma-list); without this overlay the `List` per-element check would never engage. The gate is safe: beellama/buun `spec-type` is `list:false`, so it keeps its parsed `Type:3` and is unaffected. Regenerated llama-server schemas now carry `spec-type` as `Type:4, list:true` (10 values incl. `draft-dflash`), so valid comma-lists/chains/`draft-dflash` pass **and** typos (e.g. `draft-mttp`) are rejected.
  - **Verification:** `TestValidator_ListEnum` (incl. spaced/trailing/double-comma/`[]string`/empty-rejection/scalar-still-strict rows) and `TestValidate_ExtraArgsKnownFlagPassthrough` (incl. bool-with-value and trailing value-less) added (`internal/service/validator/validator_test.go`); `TestFlagSpec_ListOmittedWhenFalse` pins the `omitempty` (`internal/domain/flag_schema_test.go`). End-to-end (temp `llama.cpp-stable` profiles, since deleted): `draft-mtp,ngram-mod`, `draft-mtp, ngram-mod` (spaced), `draft-dflash` in `args`, and `--spec-type draft-mtp,ngram-mod` in `extraArgs` → all exit 0; a typo `draft-mttp` → exit 2 (`spec-type: "draft-mttp" not in […]`); existing `ornith-aeon-…-256k` (`spec-type: draft-mtp`) → exit 0 (no regression). `go build ./...` → 0; `go vet` clean; `go test ./...` → 36 packages ok.
- **Correction to the original S1 notes (adversarial re-verification):** the curated `spec-type` enum is **multi-value** (9 values), not "single-value"; the defect is that a *scalar* enum (one value per flag) cannot represent a *list-valued* flag. The sglang "same failure class" examples were partly off: `tool-call-parser`/`reasoning-parser` are genuinely stale (e.g. missing `qwen3_coder` / `qwen3-thinking`), but `qwen3_xml` is **not** a real SGLang parser and `glm47` is a *tool-call* parser, not a reasoning parser — both are now unblocked by the `extraArgs` passthrough regardless.

---

## Original report context

The original `BUG_REPORT.md` (deleted; absorbed here) recorded:

- **Date:** 2026-04-29 · **Binary:** `bin/model-loader` at commit `d6e6ff7`.
- **Method:** interactive `tmux` 200×50, keystrokes via `tmux send-keys`, captures via `tmux capture-pane`; a seed profile in `~/.config/model-loader/profiles/test-profile.json`. Reference doc: `internal/ui/components/help.go` (`HelpMarkdown`).
- **Headline:** 12 bugs; the dominant root cause was `root.go` intercepting `tab`/`shift+tab` before delegating (B1), which cascaded into B5/B12/B13.
- **Not tested then** (structure-only): launching a real instance, the Monitor `Slots`/`Metrics` sub-views, the editor's `ctrl+p` picker — all blocked by B1, now fixed.

---

## Remaining / by-design notes

- **No open defects.** [S1](#s1) was fixed 2026-07-02 (list-valued enum + `extraArgs` passthrough + `draft-dflash`). L1 and L2 are deliberate behaviors (crash-visibility trade-off; covered window), not bugs. B2 is N/A; B3 is architectural (Launcher tab removed).
- **Optional future polish (not bugs):** a configweb widget for `List`-valued enums (multi-select/comma-text + a `list` toggle in Customize mode) — `spec-type` is `Type:4 + list:true` now, but the web editor still renders a `Type:4` flag as a single-select, so comma-lists can only be typed via the `args` JSON / CLI today; gate the L3 tokens/s regex on `kind == llama.cpp` (~100 ns/line micro-opt); a carry-buffer for L1 fragmentation only if fragmented lines are ever observed (would regress crash-line latency — keep the current behavior unless proven necessary).
- **Optional future polish (not bugs):** gate the L3 tokens/s regex on `kind == llama.cpp` (~100 ns/line micro-opt); a carry-buffer for L1 fragmentation only if fragmented lines are ever observed (would regress crash-line latency — keep the current behavior unless proven necessary).

## Verification

- `go build ./...` → exit 0.
- `go test ./...` → all packages ok (including `TestLogFollower_FlushesTrailingPartialLine`, `TestSubscribe_DropsOnBackpressure`, `TestModelsPage_RemoveBrokenPaths*`, `TestUpdateSearchPathsAt_RewritesAndPreservesOtherValues`, and the 2026-07-02 S1 regressions `TestValidator_ListEnum`, `TestValidate_ExtraArgsKnownFlagPassthrough`).
- Validation & fixes applied: 2026-06-23 on `main` (L/B/D/T series); **S1 fixed 2026-07-02** (list-valued enum + `extraArgs` passthrough + `draft-dflash`; end-to-end repro confirmed exit 0 for chained `spec-type`, `draft-dflash`, and `extraArgs` comma-list on a `llama.cpp-stable` profile).

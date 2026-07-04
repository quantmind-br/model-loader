# BUGS — model-loader

**Status legend:** 🔴 Open &nbsp;·&nbsp; 🟡 Partial / Mitigated &nbsp;·&nbsp; 🟢 Fixed &nbsp;·&nbsp; ⚪ Won't fix / by design / N/A &nbsp;·&nbsp; ❓ Unknown

**Severity legend:** **C** Critical &nbsp;·&nbsp; **H** High &nbsp;·&nbsp; **M** Medium &nbsp;·&nbsp; **L** Low &nbsp;·&nbsp; **I** Info

This file is the **single source of truth for known defects**. It absorbs and supersedes the original `BUG_REPORT.md` (2026-04-29), which was deleted; its context is preserved in [Original report context](#original-report-context).

**Validated & resolved:** 2026-06-23. Every entry was re-checked against the working tree on `main`, then all actionable defects were fixed in the same pass (`go build ./...` → exit 0; `go test ./...` → all ok). As of that pass **no entry was `🔴 Open`** — everything was Fixed or a deliberate by-design/non-bug. **Update 2026-07-02:** defect **[S1](#s1)** (curated `spec-type` enum blocked chained / `draft-dflash` speculative decoding, with no `extraArgs` override) was opened and **fixed in the same pass** (Fix #1 list-valued enum + Fix #2 `extraArgs` passthrough; `draft-dflash` added; schemas regenerated). **Update 2026-07-03:** the **N-series** ([N1](#n1)/[N2](#n2)) was opened and fixed while materializing the `sndr-vllm` (Genesis) backend + profiles — aged-out pinned-wheel index (`SNDR_WHEEL_INDEX` override) and a missing mandatory `GENESIS_ENFORCE_VERSION_RANGE=1`. **Update 2026-07-03 (later):** the **V-series** ([V1](#v1)) opened — the stock `vllm-nightly` backend's fp8-KV profiles 500 on inference (flashinfer 0.6.13 fa2/fp8 regression from the 2026-07-02 source rebuild); `backend-build.sh` (now defaults to prebuilt nightly wheels) + the renamed-venv path are fixed, but the venv rebuild is pending, so **V1 is `🟡 Partial`**. **Update 2026-07-03 (proxy):** the **P-series** opened — [P1](#p1) (detached-proxy health-check timeout hardcoded 180s killed a slow 35B int4 TP=2 multimodal-MoE boot; fixed — configurable `[serve].health_check_timeout_sec` + default raised to 360s, **pending a proxy restart to activate**) and [P2](#p2) (a `❓` reasoning-parser-returns-empty observation, separate from the load bug). **Update 2026-07-04:** three more defects surfaced and were fixed in the same pass — [P3](#p3) (`WaitHealthy`/`WaitReady` never checked backend-process liveness, so a crash-after-spawn burned the full health-check timeout while holding the proxy's `swapMu`; fixed via a `procutil.Alive` check + new `ErrProcessExited` sentinel, covering both the llama/vLLM/SGLang HTTP-health path and the unsloth log-token path), [DL1](#dl1) (`--snapshot` downloads checked the destination *directory* instead of the destination *file*, so only the first file of a multi-file repo could ever be fetched; fixed by mirroring the non-snapshot branch's file-existence check), and [DL2](#dl2) (queued-but-unspawned downloads were silently flipped to `Abandoned` by the next CLI invocation's `Reconcile`, because a one-shot CLI process's in-memory queue dies with it; fixed by having `Reconcile` re-queue and opportunistically `promoteQueued`/spawn them instead of abandoning). No entry is `🔴 Open`. See [Resolution summary](#resolution-summary).

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

### N-series — SNDR (Genesis) backend provisioning
| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| [N1](#n1) | M | 🟢 | `scripts/setup-sndr-backend.sh` | Pinned vLLM install used the rotating `wheels.vllm.ai/nightly` index → aged-out `dev424` pin `No matching distribution`; fixed via `SNDR_WHEEL_INDEX` per-commit override |
| [N2](#n2) | M | 🟢 | `docs/sndr-backend.md` + SNDR profile `launch.env` | Runbook omitted the mandatory `GENESIS_ENFORCE_VERSION_RANGE=1`; version-capped patch `PN30` (obsolete on dev424) hard-failed (`failed=1`) instead of version-gating off; fixed by adding the env to every SNDR profile |
| [N3](#n3) | M | ⚪ | TurboQuant `P38` continuation-prefill vs 24 GiB VRAM | SNDR `-sndr` 262144 profiles OOM (util 0.84) / crawl 1.4 tok/s (0.78) on large prefills — TurboQuant continuation scratch has no headroom past the KV reservation. By-design VRAM limit; documented (short-prompt-only) not code-fixable here |

### V-series — stock vLLM backends (`vllm-nightly`)
| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| [V1](#v1) | H | 🟡 | `backends/vllm-nightly/backend-build.sh` + venv | 2026-07-02 source rebuild pulled flashinfer 0.6.13 (fa2 asserts on fp8 KV / Ampere) → all fp8-KV vLLM profiles 500. Build script fixed to default to prebuilt nightly wheels; also repaired a renamed-venv `activate` path bug. Venv itself still needs a rebuild to a good nightly |

### P-series — Proxy backend-load lifecycle
| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| [P1](#p1) | H | 🟢 | `internal/cli/serve.go` + `config.go` | Proxy health-check timeout hardcoded 180s killed a slow big-model boot (35B int4 TP=2 multimodal MoE, >3 min cold); made configurable (`[serve].health_check_timeout_sec`) + default raised to 360s (needs a proxy restart to activate) |
| [P2](#p2) | M | ❓ | Ornith AWQ profile `reasoning-parser: qwen3` | Empty `content` only when truncated mid-`<think>` (`finish_reason: length`); clean on `stop`. Parser+low-`max_tokens`, not a load/SNDR defect — reproduced on stock & SNDR. By-design; mitigate with higher `max_tokens` or no parser |
| [P3](#p3) | M | 🟢 | `internal/service/processmgr/enrichment.go:15` (`WaitHealthy`) + `readiness.go` (`waitForLogToken`) | `WaitHealthy`/`WaitReady` had no liveness check and kept polling for the **full** timeout while holding the proxy's `swapMu`; fixed via `procutil.Alive(pid)` + new `ErrProcessExited` sentinel in both the HTTP-health and unsloth log-token paths |

### BM-series — Benchmark harness
| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| [BM1](#bm1) | M | 🟢 | `internal/service/benchmark/llamabench_probe.go` | llama-bench fill-90 built a prompt over `max_model_len` at ≥~200k ctx → backend `400`; fixed via ×3/4 char-budget scale + 256-tok chat-template reserve |

### DL-series — Download manager
| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| [DL1](#dl1) | M | 🟢 | `internal/service/downloadmgr/pathing.go:48-51` | `--snapshot` mode rejected the 2nd+ file of the same repo as "already exists" because it checked the *directory*, not the file; fixed to check `destFile`, mirroring the non-snapshot branch |
| [DL2](#dl2) | M | 🟢 | `internal/service/downloadmgr/manager.go` (`Reconcile`) | Queued (not-yet-spawned) downloads from a one-shot CLI invocation were silently abandoned by the next invocation's `Reconcile`; fixed by re-queuing + `promoteQueued`/spawning instead of abandoning |

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

### T4
- **Status:** 🟢 Fixed (TUI responsive layout). Negative-width `strings.Repeat` panics in `centeredDivider` and `renderBar` (guards added). `ClampBody` truncates; `ResponsiveSplit` stacked widths floor at 1; root size notice below 20×6. Regressions: `TestClampBodyTruncatesNotWraps`, `TestRoot_TerminalTooSmallNotice`, `TestServerPage_ResponsiveLayout`, `TestProfilesPage_ResponsiveLayout`, `TestBackendsPage_ResponsiveLayout`, `TestHelpViewportSize`, `TestModelPicker_BoxWidthHonorsFloor`.

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

### N3
- **Severity:** M (Medium) · **Status:** ⚪ By-design VRAM limit — documented, not code-fixable in model-loader.
- **Component:** `turboquant_k8v4` continuation-prefill (SNDR patch `P38`, `vllm/.../turboquant_attn.py::_prefill_attention` → `.../patches/attention/turboquant/p38_tq_continuation_memory.py::_genesis_continuation_prefill`) on the 24 GiB desktop GPU0.
- **Finding:** Discovered running the A/B `benchmark --mode llama-bench` for [N2](#n2)'s profiles. TurboQuant's chunked continuation-prefill dequantizes a **full-precision K buffer that scales with the prompt length**. At `max-model-len 262144` the KV cache is sized to the `gpu-memory-utilization` budget, leaving no room for that scratch on a 24 GiB card that also drives the desktop:
  - at `0.84`, **all** llama-bench prefills OOM (`torch.OutOfMemoryError` in `_genesis_continuation_prefill`, GPU0 250 MiB free), returning **502** — even `fill 5%` (~13k tokens).
  - at `0.78`, no OOM but `fill 25%` (~65k tokens) runs at **1.4 tok/s**, peak VRAM 23.9 GiB / util 99%.
  Short prompts are unaffected (validated ~145 tok/s single-stream, 2.47× KV @262144). The stock `fp8_e5m2` twin has no such continuation buffer.
- **Resolution:** documented as a **short-prompt / high-concurrency** profile class (profile descriptions + `docs/sndr-backend.md` "Large-prefill trap"). For large-context (coding-agent) prompts, build a **≤131072** `-sndr` variant (smaller KV reservation → the scratch fits) or use the stock fp8 profile. Also: an OOM mid-run leaks `VLLM::Worker_TP*` processes holding VRAM (proxy shows `loaded:""`) that block the next boot — `pkill -9 -f VLLM::Worker`.

---

## N-series — SNDR (Genesis) backend provisioning

Surfaced 2026-07-03 while materializing the `sndr-vllm` backend + its optimized
profiles (the commit `8588b0a` scaffolding was scripted but had never been run
end-to-end). Both fixed the same day; the backend then booted the Lorbus 27B
flagship at 262144 ctx with Genesis `failed=0`, 2.47× KV concurrency, ~145 tok/s.

### N1
- **Severity:** M (Medium)
- **Status:** 🟢 Fixed (2026-07-03) — added a `SNDR_WHEEL_INDEX` override (default unchanged) so an aged-out pin can be installed from its persistent per-commit wheel URL.
- **Component:** `scripts/setup-sndr-backend.sh` (the pinned-vLLM `pip install`).
- **Finding:** The script installed the SNDR-pinned vLLM with `--extra-index-url https://wheels.vllm.ai/nightly`. That channel is **rotating** — it keeps only the *latest* nightly. SNDR pins `0.23.1rc1.dev424+g3f5a1e173`, which has already rotated out, so a fresh run dies with `ERROR: No matching distribution found for vllm==0.23.1rc1.dev424+g3f5a1e173` (the index then only had `dev738` + `0.24.0`). The pin itself is correct and SNDR-mandated; the *index* was the bug.
- **Fix:** vLLM also publishes **persistent per-commit** wheels at `https://wheels.vllm.ai/<full-40char-sha>/`. Added `SNDR_WHEEL_INDEX` (default the nightly channel) and routed the install through it; the header + `docs/sndr-backend.md` document the per-commit URL for `dev424` (`…/3f5a1e1733200760169ff31ebe60a271072b199e/`). The failed install now dies with an actionable message pointing at the override.
- **Verification:** `SNDR_WHEEL_INDEX=https://wheels.vllm.ai/3f5a1e17…199e/` → `vllm 0.23.1rc1.dev424+g3f5a1e173` installed into `backends/sndr-vllm/.venv`, entry-point `genesis_v7` registered, `python -m sndr.apply` smoke test passed.

### N2
- **Severity:** M (Medium)
- **Status:** 🟢 Fixed (2026-07-03) — `GENESIS_ENFORCE_VERSION_RANGE=1` added to every SNDR profile `launch.env` (and documented as mandatory in the runbook).
- **Component:** `docs/sndr-backend.md` recommended profile + the generated SNDR profiles `launch.env`.
- **Finding:** SNDR gates each patch behind its declared vLLM version range, but the range check is itself gated behind `GENESIS_ENFORCE_VERSION_RANGE=1` (`sndr/dispatcher/decision.py:461`, **default OFF**). SNDR's own launcher mandates it (*"the rig render-launcher MUST carry GENESIS_ENFORCE_VERSION_RANGE=1"*), but the model-loader runbook/example omitted it. Without it, `PN30` (declared **capped `<0.23.0`** — superseded on `dev424` by the upstream fused-postprocess kernel; confirmed at `vllm/.../mamba/mamba_utils.py:309`) tried to apply against a missing anchor and **hard-failed** → Genesis boot summary `applied=84 skipped=168 failed=1`.
- **Fix:** Added `GENESIS_ENFORCE_VERSION_RANGE=1` to the common `launch.env` of all SNDR profiles. `PN30` (and any other out-of-range patch) now **version-gates OFF** cleanly (`SKIP … VERSION-GATE: vllm 0.23.1rc1.dev424 violates ['>=…']`), runtime behavior identical (the failed patch was already a no-op). An earlier ad-hoc `GENESIS_ENABLE_PN30_…=0` workaround was replaced by this principled, general fix.
- **Verification:** re-boot of `qwen3.6-27b-int4-autoround-tq-mtp-sndr-tp2-256k` → Genesis `applied=83 skipped=170 **failed=0** (apply=True)`, 0 fatal errors, coherent generation, ~145 tok/s. (14→23 non-fatal *partial-apply* anchor-drift **warnings** remain across the 321-entry registry — informational, not failures; a known SNDR characteristic when a pin drifts past a patch's validated commit.)

---

## V-series — stock vLLM backend (`vllm-nightly`)

### V1
- **Severity:** H (High) · **Status:** 🟡 Partial — build script + venv-path fixed 2026-07-03; the venv still needs a rebuild to a good nightly to restore fp8-KV inference.
- **Component:** `backends/vllm-nightly/backend-build.sh` (build mode) + the `vllm-nightly` `.venv`.
- **Finding:** Surfaced closing the SNDR A/B. Two independent breakages of the stock `vllm-nightly` backend (6 fp8-KV profiles):
  1. **flashinfer regression.** `backend-build.sh` defaulted to a **source** build (`pip install -e vllm-src`), which resolves `requirements/cuda.txt` → **`flashinfer-python==0.6.13`**. The 2026-07-02 rebuild (vllm `e24d1b24f`) thus pulled flashinfer 0.6.13, whose fa2 batch-prefill asserts `fp8 tensor core is not supported in fa2 backend` (`flashinfer/jit/attention/modules.py:521`) — and Ampere SM 8.6 has only the fa2 flashinfer path (fa3 is Hopper). Result: **500 on every fp8-KV prefill** (even short prompts). The fp8-KV profiles worked on the 2026-06-29 build. The source build also produced a bogus version (`0.1.dev1+ge24d1b24f`, shallow-clone setuptools-scm fallback).
  2. **renamed-venv path.** `backends/vllm-nightly/.venv` had been created as `backends/vllm/.venv` then renamed — `activate` + 43 `bin/` scripts hard-coded the dead path → wrapper `exec vllm` gave `vllm: command not found`. Repaired via `sed -i 's|backends/vllm/\.venv|backends/vllm-nightly/.venv|g'`.
- **Fix:** `backend-build.sh` now **defaults to the prebuilt nightly wheel** (`uv pip install vllm --extra-index-url https://wheels.vllm.ai/nightly` — a coherent tested artifact) with `--source` as opt-in, and accepts a positional nightly version to pin a known-good build. Venv paths repaired in place. **Remaining:** run `./backend-build.sh --recreate [good-nightly-version]` to reinstall; if the latest nightly wheel still ships flashinfer 0.6.13, pin an earlier (late-June) nightly. Not yet run (daily driver; user's call).

### V2
- **Severity:** M (Medium) · **Status:** ⚪ Won't fix / upstream — no model-loader defect; the model's AWQ build cannot be served by any runtime in the current stack. Model + profile removed 2026-07-03.
- **Component:** `sahilchachra/Unlimited-OCR-AWQ` (W4A16 compressed-tensors int4, DeepSeek-OCR arch) vs backends `vllm-nightly` and `sglang-unlimited`.
- **Finding:** Attempted to serve the AWQ (int4) quant of Unlimited-OCR to compare against the BF16 `unlimited-ocr-sglang-tp2-32k` (~405 tok/s/page). **Both backends fail structurally:**
  1. **vLLM (`vllm-nightly`).** Custom remote code (`modeling_unlimitedocr.py`) calls `is_torch_fx_available`, **removed in transformers 5.12.1** (the wheel `vllm-nightly` / `sglang-unlimited` both bundle). The model card **pins `transformers==4.57.3`** — the AWQ's official serving path is `transformers`/`AutoModel.infer()`, not vLLM. Installing the model's missing deps (`addict`, `matplotlib`) clears the first ImportError but not this one.
  2. **SGLang fork (`sglang-unlimited`).** The fork's `QUANTIZATION_METHODS` *does* list `compressed-tensors`/`awq`/`awq_marlin`, but the model **handler** doesn't support packed weights: single-GPU dies in `DeepseekMoE.pack_params()` — `AttributeError: 'MergedColumnParallelLinear' object has no attribute 'weight'` (AWQ experts expose `.qweight`/`.scales`/`.qzeros`, not `.weight`); TP=2 dies earlier in `compressed_tensors_wNa16.py` — `AssertionError: input_size_per_partition % group_size == 0` (group_size 128 not divisible after the TP shard). The fork's `DeepseekMoE` was written for dense BF16 weights.
- **Viable paths (per the HF model card, not pursued):** (a) **GGUF** `sahilchachra/Unlimited-OCR-GGUF` (Q4_K_M 1.95 GB + mmproj F16) on **llama.cpp built from PR #17400/#24975** (DeepSeek-OCR arch — *not* in our stable b9849/nightly/beellama/buun builds); (b) a **transformers 4.57.3** isolated venv running `AutoModel.infer()` as a standalone (non-OpenAI, no proxy integration); (c) **NVFP4** — inviable on RTX 3090 (SM86 has no FP4 compute); (d) **MLX** — Apple-only.
- **Resolution:** Kept the working **BF16** path (`unlimited-ocr-sglang-tp2-32k`). Deleted the unusable `unlimited-ocr-awq-vllm-32k` profile and removed the 2.7 GB AWQ download. Re-open only if the GGUF+PR path is built.

---

## Original report context

The original `BUG_REPORT.md` (deleted; absorbed here) recorded:

- **Date:** 2026-04-29 · **Binary:** `bin/model-loader` at commit `d6e6ff7`.
- **Method:** interactive `tmux` 200×50, keystrokes via `tmux send-keys`, captures via `tmux capture-pane`; a seed profile in `~/.config/model-loader/profiles/test-profile.json`. Reference doc: `internal/ui/components/help.go` (`HelpMarkdown`).
- **Headline:** 12 bugs; the dominant root cause was `root.go` intercepting `tab`/`shift+tab` before delegating (B1), which cascaded into B5/B12/B13.
- **Not tested then** (structure-only): launching a real instance, the Monitor `Slots`/`Metrics` sub-views, the editor's `ctrl+p` picker — all blocked by B1, now fixed.

---

## P-series — Proxy backend-load lifecycle

### P1
- **Severity:** H (High)
- **Status:** 🟢 Fixed (2026-07-03) — `HealthCheckTimeout` made configurable via `[serve].health_check_timeout_sec` and its default raised 180s → 360s. **Requires a proxy restart to take effect** (the detached `serve` process must be relaunched from the reinstalled binary; the live TUI-owned proxy keeps the old bound until then).
- **Component:** `internal/cli/serve.go:45` (was hardcoded `HealthCheckTimeout: 180 * time.Second`) + `internal/config/config.go` `ServeConfig`.
- **Finding:** After downloading the (previously missing) weights, profile `ornith-1.0-35b-a3b-awq-vllm-tp2-256k` still failed to load via the proxy — `/_admin/load` returned `backend … not ready: … did not become healthy within timeout`. Root cause: the detached proxy passes a **hardcoded 180s** health-check bound, but this model boots slower — compressed-tensors int4 (`group_size 32`) 35B-A3B MoE, TP=2, multimodal, whose Marlin expert repack + 23 GiB shard load + mm-encoder init + CUDA-graph capture take **~1m45s isolated and >3 min via a cold swap**. At 180s the proxy killed the vLLM mid-init (`api_server.py` `_interrupt_init` → `KeyboardInterrupt("terminated")`), which reads as a crash. The engine was actually healthy: KV cache = 1,235,254 tokens allocated, MoE forward passed — the old `moe_sum`/illegal-memory crash of the 28-Jun `gptq_marlin` attempts did **not** recur on vLLM 0.24.0 + compressed-tensors (that memory is now stale).
- **Fix:** Added `ServeConfig.HealthCheckTimeoutSec` (`mapstructure:"health_check_timeout_sec"`; 0 = built-in default). `serve.go` computes `healthCheckTimeout` = that value or **360s** default and passes it to `httpproxy.Config`. 360s = 2× the old bound, covering a 3-4 min cold boot with margin, and overridable for even larger models without a recompile.
- **Verification:** direct vLLM launch (bypassing the proxy) reached `Application startup complete` in ~1m45s, `/health`=200, `/v1/models`=`[ornith-1.0-35b-a3b-awq-vllm-tp2-256k]`, generated 300 tokens. `go build ./...` → exit 0; `go test ./...` → all ok (incl. `internal/config`, `internal/cli`). End-to-end via the proxy is pending a proxy restart (see Status).

### P2
- **Severity:** M (Medium)
- **Status:** ❓ Unknown / needs investigation (2026-07-03) — surfaced while smoke-testing P1; **separate from the load bug**, not yet root-caused.
- **Component:** `ornith-1.0-35b-a3b-awq-vllm-tp2-256k` (likely its siblings too) `args.reasoning-parser: qwen3` on vLLM 0.24.0.
- **Finding:** With `reasoning-parser: qwen3`, `content` comes back **empty whenever the completion is truncated mid-`<think>`** (`finish_reason: length` — the model never emits `</think>`, so nothing lands in `content` and, on this build, `reasoning_content` stays `null` too). When the model **completes** the answer (`finish_reason: stop`), `content` is clean and correct — verified on the SNDR twins (128k returned a correct Python function; 256k returned `42.` for 17+25). So it is **not** a load defect and **not** SNDR-specific (reproduced on both stock `vllm-nightly` and `sndr-vllm`); it is the qwen3 parser + a `max_tokens` low enough to cut off the reasoning block (consistent with `vllm-reasoning-parser-drops-content`).
- **Impact / mitigation:** real workloads with a sufficient `max_tokens` are unaffected (the model closes `</think>`). A client that caps `max_tokens` low on a reasoning prompt sees an empty `content`. Fix options if it bites: drop `reasoning-parser` (clients read `<think>` from `content`), or raise `max_tokens`. Left as-is pending user decision (out of scope of the "won't load" request).

### P3
- **Severity:** M (Medium)
- **Status:** 🟢 Fixed (2026-07-04) — found while boot-testing the new `agents-a1-35b-a3b-autoround-rtn-vllm-tp2-256k` profile.
- **Component:** `internal/service/processmgr/enrichment.go:15` (`WaitHealthy`) + `internal/service/processmgr/readiness.go` (`WaitReady`'s unsloth `waitForLogToken` branch) + `internal/service/httpproxy/handler.go:148` (`ensureLoaded`, which holds `swapMu` for the duration of the call at line 229, `WaitReady`/`WaitHealthy`).
- **Finding:** `WaitHealthy` polls `GET /health` on a capped-backoff loop **until the timeout elapses** — it never calls `procutil.Alive(pid)` to check whether the backend process is still alive. If the process dies immediately after spawn (crash, OOM, or an external `kill`), every subsequent poll just gets connection-refused, and the function still burns the **entire** configured timeout (180-360s, see [P1](#p1)) before returning `ErrHealthCheckTimeout`. Reproduced by hand: an `instance start` was interrupted mid-flight client-side (a mistake — piping it through `timeout` at the shell), but the *server-side* `ensureLoaded` goroutine kept running per Go's normal disconnect-doesn't-cancel-the-handler semantics; the spawned backend was then killed manually (simulating a crash). The proxy sat with `loaded_profile_id: ""` and **no other load/unload/status-affecting call could proceed** for the following ~6 minutes (confirmed via `_status`'s `last_error_at` landing exactly ~356s after the doomed process died) because `ensureLoaded` holds `swapMu` for the whole `WaitReady` call. The unsloth branch's `waitForLogToken` had the identical gap (polls the log file for the auth-key token until timeout, no liveness check), even though `WaitReady`'s caller already has `inst.PID` available.
- **Impact:** any backend crash immediately after spawn (bad flags, OOM, port race, or a user externally killing the PID) turns a should-be-instant failure into a multi-minute proxy-wide stall — nothing else can load, unload, or swap models until the dead timeout expires. This is a robustness/latency issue, not a correctness one (it does eventually recover on its own).
- **Fix (2026-07-04):** added `procutil.Alive(pid)` at the top of `WaitHealthy`'s poll loop (`enrichment.go`), returning a new sentinel `ErrProcessExited` (`processmgr.go`, alongside `ErrHealthCheckTimeout`) immediately if the PID is gone, instead of waiting out the rest of the timeout. `waitForLogToken` (`readiness.go`) got the same treatment — it now takes a `pid` parameter (its only caller, `WaitReady`, already had `inst.PID`) and returns `ErrProcessExited` early too. No change was needed in `httpproxy` — `ensureLoaded`/`launchNewBackend` already just propagate whatever error `WaitReady` returns and release `swapMu` on return, so the fix collapses the worst-case stall from the full configured timeout down to one poll cycle (≤1s).
- **Verification:** `TestManager_WaitHealthy_TimesOut` updated to use a genuinely alive (but non-listening) PID so it still exercises the real "never became healthy" timeout path; new `TestManager_WaitHealthy_DeadProcessFailsFast` spawns a real short-lived process, waits for it to exit, and asserts `WaitHealthy` returns `ErrProcessExited` in well under 1s against a 5s timeout. New `TestWaitForLogToken_DeadProcessFailsFast` covers the unsloth branch the same way; existing `TestWaitForLogToken_FindsKey/_Timeout/_MissingFileTimesOut` updated for the new `pid` parameter. `go build ./...` → exit 0; `go test ./...` → all ok.

---

## BM-series — Benchmark harness

### BM1
- **Severity:** M (Medium)
- **Status:** 🟢 Fixed (2026-07-03) — working-tree fix built + `make install`ed; re-run of `ornith-1.0-35b-a3b-autoround-vllm-tp2-256k-test` is now **4/4** (fill-90 ~58.5 tok/s at 262144 ctx).
- **Component:** `internal/service/benchmark/llamabench_probe.go` — `runLlamaBench` prompt sizing.
- **Finding:** The prefill was `promptTokens := ctxTok * FillPct / 100`, capped only at `ctxTok - GenTokens - 64`. `buildCodeContext` writes `promptTokens*4` chars, but real tokenizers hit ~3.16 chars/token on the HumanEval corpus (calibrated from the 192k run: 530840 chars → 168149 tokens), so the actual request runs ~26% larger than the token budget. At fill-90 on a 262144-ctx profile that built a ~299k-token request > `max_model_len` → vLLM `400 Bad Request`. Every ≥~200k-ctx profile's fill-90 preset failed when generated by the pre-fix binary (`qwythos-9b-mtp-q4km-262k*`, the `ornith-*-256k` family); fill-50 also overshot (166103 vs the correct 124649) and its tok/s carried large variance.
- **Proof it was the harness, not the profile:** the *same* profile `ornith-1.0-35b-a3b-awq-vllm-tp2-256k` (identical args) `400`'d at 03:47 yet passed at 04:20 / 07:05 — the only variable was which binary generated the prompt.
- **Fix:** scale the char budget by ×3/4 (`target * 3 / 4`) and reserve `chatTemplateSlack = 256` tokens for the completion + chat template; the `Detail` string now reports the *target* fill (intent), not the clamped budget. fill-90 at 262144 now builds ~224k tokens (fits with headroom), fill-50 ~124.6k; the variance collapsed. **No profile args changed** — raising `max-num-batched-tokens` would not have helped, the request simply exceeded `max_model_len`.
- **Verification:** `model-loader benchmark --profile ornith-1.0-35b-a3b-autoround-vllm-tp2-256k-test --mode llama-bench` → **4/4**, run `…-1783081047193309131`: 5%≈163, 25%≈125, 50%≈116, 90%≈58.5 tok/s (n=3, low variance), peak VRAM 21891MB, GPU ~97%.

---

## DL-series — Download manager

### DL1
- **Severity:** M (Medium)
- **Status:** 🟢 Fixed (2026-07-04) — found while downloading `LeaderboardModel1/Agents-A1-AutoRound-W4A16-RTN` (14 files) via `model download <repo> <file> --snapshot`.
- **Component:** `internal/service/downloadmgr/pathing.go:48-51` (`ResolveDest`, snapshot branch).
- **Finding:** Snapshot mode's doc comment says it nests **all** of a repo's files under one `{publisher}/{repo}` directory "so the on-disk layout mirrors the Hugging Face cache" — implying repeated calls for the same repo are expected. But the existence check in the `isSnapshot` branch is `os.Stat(destDir)` (the **directory**), not the file. The first `model download <repo> f1 --snapshot` call creates `destDir` while writing `f1`; every subsequent `model download <repo> f2 --snapshot` call for the same repo then fails immediately with `ErrAlreadyExists` ("already exists") even though `f2` itself was never downloaded — because `destDir` now exists from `f1`. Reproduced downloading a 14-file repo: only the 1st file (`config.json`) succeeded with `--snapshot`; the other 13 all errored `resolve destination: already exists` before any HTTP request was made.
- **Impact:** `--snapshot` mode is effectively unusable for any multi-file repo when files are queued one CLI invocation at a time (the common case — there is no bulk/whole-repo download subcommand). The non-snapshot (individual) path is unaffected — it checks `os.Stat(destFile)` instead, so calling `model download <repo> <file>` (no `--snapshot`) for every file after the first works and lands in the same directory (destDir is computed identically either way), which is the workaround used here.
- **Fix (2026-07-04):** the snapshot branch now computes `destFile` and validates `withinRoot` **before** checking existence, then checks `os.Stat(destFile)` instead of `os.Stat(destDir)`. Since this made the snapshot and non-snapshot branches' traversal/existence-check tail byte-for-byte identical (the only real difference is how `destFile` is computed — full `rfilename` vs its basename), a same-day review pass collapsed the duplicated tail into one shared `if !withinRoot(...) {...}; if _, err := os.Stat(destFile); ...` block, reducing future drift risk. The directory existing is still expected/desired for snapshot mode; only the specific file being re-downloaded is rejected.
- **Verification:** `pathing_test.go`'s `"existing snapshot dir returns ErrAlreadyExists"` subtest (which had pinned the buggy behavior — it asserted an error for a file that was never written) was replaced with `"existing snapshot dir with no colliding file succeeds"`, `"existing file within snapshot dir returns ErrAlreadyExists"`, and `"second file of same snapshot repo does not collide with the first"`. `go build ./...` → exit 0; `go test ./...` → all ok.

### DL2
- **Severity:** M (Medium)
- **Status:** 🟢 Fixed (2026-07-04) — found in the same session as DL1, downloading the same 14-file repo (12 files queued after the first `--snapshot` call failed per DL1, then re-queued individually).
- **Component:** `internal/service/downloadmgr/manager.go` — `Reconcile` (previously flipped queued/`PID==0` records to `StatusAbandoned`; now re-queues + spawns via the existing `promoteQueued`/`emitEvents`).
- **Finding:** `Manager.maxConcurrent` defaults to 3. When a `Start`/`Resume` call finds `len(m.active) >= maxConcurrent`, it appends the id to `m.queue` **in memory** and returns — nothing is spawned. That queue only ever drains when the *same* `Manager` instance later notices an active slot free (inside `spawn`'s completion callback, still in-process). But every `model download …` CLI invocation constructs a **fresh** `Services`/`Manager`, enqueues/spawns, and **exits immediately** — the in-memory `m.queue` (and whatever `Manager` would have serviced it) dies with the process. The *next* CLI invocation's `Reconcile()` then finds those records with `PID == 0` and no live worker, and flips them straight to `StatusAbandoned` (`"queued worker never spawned"`), even though nothing actually failed — they were simply never given a chance to run.
- **Reproduced:** looping `model download <repo> <file>` (no `--wait`) for 13 files of one repo with `maxConcurrent=3`: only the first 3-4 invocations' workers (Setsid-detached, so they *do* survive their parent CLI exiting) actually ran; the remaining 9 were recorded then abandoned by the next call's `Reconcile`, with `bytes: 0, total: 0` — no HTTP request was ever made for them.
- **Impact:** any multi-file HF download driven by looping the CLI (the only way to fetch a full snapshot today — there is no bulk/whole-repo subcommand) silently drops everything past the concurrency cap unless the caller manually detects and `model downloads resume <id>`s each abandoned item in throttled batches of ≤`maxConcurrent`, polling between batches. Nothing in `--help` documents this; a naive script (or a user firing off a shell loop) will end up with a directory of empty/missing files and no error surfaced until `model downloads` is inspected.
- **Fix (2026-07-04, option "a" from the original two suggested fixes):** `Reconcile`'s `default` branch used to mark *every* non-terminal, `PID == 0` record `StatusAbandoned`. Since `IsTerminal()` is already handled earlier in the same `switch`, "non-terminal with `PID == 0`" can only be `StatusQueued` (only `spawn()` ever assigns a real PID). That case now re-queues the id into `m.queue` (in-memory, this `Manager` instance) instead of touching the on-disk record, then — after the reconciliation loop, still under `m.mu` — calls the existing `promoteQueued(freeSlots)` (already used by `tick()`'s poll loop) with `freeSlots := m.maxConcurrent - len(m.active)`, and after releasing the lock, `emitEvents(nil, toSpawn)` to spawn exactly that many. The `freeSlots` computation matters: `promoteQueued`'s own `len(m.active) < m.maxConcurrent` loop guard cannot bound the call on its own, because `promoteQueued` never mutates `m.active` (`spawn()` does that later, after the lock is released) — so `len(m.active)` is constant for the whole call and the `freedSlots` argument is the *only* real limiter; passing the raw `m.maxConcurrent` instead of the actual free-slot count would over-spawn past the cap whenever some workers were already active (caught in review before this landed). Because workers are `Setsid`-detached subprocesses that outlive their spawning CLI process, any future CLI invocation that calls `Reconcile()` (`model download`, `model downloads`, `model downloads resume`, …) now opportunistically drains the backlog instead of discarding it — no new subcommand needed. A queued record that still has no free slot at reconcile time is simply left `StatusQueued` (not spawned, not abandoned) for the next reconcile to pick up.
- **Verification:** new `TestManager_ReconcileSpawnsQueuedWhenCapacityAvailable` (seeds a `StatusQueued`/`PID:0` record, reconciles with a free slot, asserts it completes instead of being abandoned), `TestManager_ReconcileLeavesQueuedWhenAtCapacity` (a live worker occupies the only slot; the queued record stays `StatusQueued`/`PID:0` after `Reconcile`, not `Abandoned`), and `TestManager_ReconcilePromotesOnlyFreeSlotsWhenPartiallyBusy` (`maxConcurrent=2`, one active worker + two queued records, a spawn-counting spawner asserts exactly 1 spawn fires, not 2 — the case the `freeSlots` fix specifically targets). Existing `TestManager_ReconcileMarksDeadWorkersAbandoned`/`TestManager_ReconcileKeepsLiveWorkers` (the `PID > 0` cases) are unaffected. `go build ./...` → exit 0; `go test ./...` → all ok.
- **Follow-up fix (same day, pre-merge adversarial review):** an independent review caught that the auto-promote fix above had no cross-process exclusivity — `m.active`/`m.mu` are per-`Manager`-instance, so the long-lived TUI process and any one-shot `model download`/`model downloads`/`model downloads resume` CLI invocation each build their **own** `Manager` over the same `stateDir` with no shared lock (`buildDownloadManager`, `internal/cli/model.go:57-64`, never takes the single-instance flock). If two such instances' `Reconcile()` calls raced on the same still-queued record, both could `promoteQueued`+`spawn` it, running two workers against the same `DestFile` concurrently. Fixed via `claimPath`/`spawn` (`manager.go`): before calling the spawner, `spawn` now creates a zero-byte `<statepath>.claim` marker with `fsx.WriteJSONExclusive` (the same hard-link race-free `Create` primitive already used elsewhere in this codebase for check-then-write races) — `os.Link` fails atomically if another instance already claimed the id, so only the winner spawns; the marker is removed via `defer` once the spawn attempt (success or failure) completes, so a later `Resume` is never permanently blocked. `ListRecords` only matches `dl-*.json`, so the `.claim` suffix is invisible to it. New tests `TestManager_ReconcileSkipsSpawnWhenAlreadyClaimed` (pre-seeds a claim file, asserts `Reconcile` does not also spawn) and `TestManager_SpawnRemovesClaimAfterCompleting` (asserts the marker is gone after a spawn attempt finishes) — both verified to fail without the fix. `go build ./...` → exit 0; `go test ./...` → all ok.

## Remaining / by-design notes

- **One `🟡 Partial` defect: [V1](#v1)** — stock `vllm-nightly` fp8-KV profiles 500 on inference (flashinfer 0.6.13 fa2/fp8 regression on Ampere); `backend-build.sh` + the renamed-venv path are fixed, the venv rebuild to a good nightly is pending (an operational venv-rebuild step, not a code change — left to the user's call). **No `🔴 Open`.** [N3](#n3) is a `⚪` by-design VRAM limit (SNDR short-prompt-only). [P2](#p2) is a `❓` reasoning-parser-truncation observation, mitigated client-side (drop the parser or raise `max_tokens`), not a model-loader code defect. [S1](#s1) was fixed 2026-07-02 (list-valued enum + `extraArgs` passthrough + `draft-dflash`); [P3](#p3)/[DL1](#dl1)/[DL2](#dl2) were fixed 2026-07-04 (health-check liveness check, `--snapshot` file-vs-dir stat, and `Reconcile` auto-promote for queued downloads). L1 and L2 are deliberate behaviors (crash-visibility trade-off; covered window), not bugs. B2 is N/A; B3 is architectural (Launcher tab removed).
- **Optional future polish (not bugs):** a configweb widget for `List`-valued enums (multi-select/comma-text + a `list` toggle in Customize mode) — `spec-type` is `Type:4 + list:true` now, but the web editor still renders a `Type:4` flag as a single-select, so comma-lists can only be typed via the `args` JSON / CLI today; gate the L3 tokens/s regex on `kind == llama.cpp` (~100 ns/line micro-opt); a carry-buffer for L1 fragmentation only if fragmented lines are ever observed (would regress crash-line latency — keep the current behavior unless proven necessary).
- **Optional future polish (not bugs):** gate the L3 tokens/s regex on `kind == llama.cpp` (~100 ns/line micro-opt); a carry-buffer for L1 fragmentation only if fragmented lines are ever observed (would regress crash-line latency — keep the current behavior unless proven necessary).

## Verification

- `go build ./...` → exit 0.
- `go test ./...` → all packages ok (including `TestLogFollower_FlushesTrailingPartialLine`, `TestSubscribe_DropsOnBackpressure`, `TestModelsPage_RemoveBrokenPaths*`, `TestUpdateSearchPathsAt_RewritesAndPreservesOtherValues`, the 2026-07-02 S1 regressions `TestValidator_ListEnum`, `TestValidate_ExtraArgsKnownFlagPassthrough`, and the 2026-07-04 P3/DL1/DL2 regressions `TestManager_WaitHealthy_DeadProcessFailsFast`, `TestWaitForLogToken_DeadProcessFailsFast`, `TestResolveDest`'s snapshot subtests, `TestManager_ReconcileSpawnsQueuedWhenCapacityAvailable`, `TestManager_ReconcileLeavesQueuedWhenAtCapacity`, `TestManager_ReconcilePromotesOnlyFreeSlotsWhenPartiallyBusy`, `TestManager_ReconcileSkipsSpawnWhenAlreadyClaimed`, `TestManager_SpawnRemovesClaimAfterCompleting`).
- A same-day adversarial multi-agent review pass (12 sub-reviews across concurrency/test-coverage/simplification, each finding independently re-verified by 3 skeptics before being trusted) caught a real cross-process race in the first cut of the DL2 fix — see the DL2 entry's "Follow-up fix" — and a minor DL1 duplication cleanup, both fixed and re-verified (including a fault-injection check: each new regression test was confirmed to fail when its corresponding fix was temporarily reverted) before merging.
- Validation & fixes applied: 2026-06-23 on `main` (L/B/D/T series); **S1 fixed 2026-07-02** (list-valued enum + `extraArgs` passthrough + `draft-dflash`; end-to-end repro confirmed exit 0 for chained `spec-type`, `draft-dflash`, and `extraArgs` comma-list on a `llama.cpp-stable` profile); **P3/DL1/DL2 fixed 2026-07-04** (health-check/log-token liveness check + `ErrProcessExited` sentinel; `--snapshot` destination-file check; `Reconcile` auto-promotes queued downloads instead of abandoning them).

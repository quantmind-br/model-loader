# BUGS — model-loader

**Status legend:** 🔴 Open · 🟡 Partial / Mitigated · 🟢 Fixed · ⚪ Won't fix / by design / N/A · ❓ Unknown

**Severity legend:** **C** Critical · **H** High · **M** Medium · **L** Low · **I** Info

All detailed bug reports and known limitations are documented in `AGENTS.md`. This file is a compact reference.

## Summary

The following table lists all identified bugs (including those that have been fixed) and known limitations.

| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| CU1 | H | 🟢 | cleanup skill `analyze.py::protected_closure` | Paths nested in JSON-valued `extraArgs`/`args` (vLLM `--speculative-config '{"model":"/…/z-lab/Qwen3.6-27B-DFlash"}'`) and non-whitelisted path keys (`mtp-head`) were never extracted — closure only took values literally starting with `/` — so DFlash/MTP drafters were misreported as `store-orphan-repo`. Caught in item-by-item review before deletion (`z-lab/Qwen3.6-27B-DFlash`, 2 live profiles, kept). **Fixed**: closure now recursively harvests every absolute-path string from all `args`/`extraArgs` values incl. JSON, dir-vs-file aware, no whitelist; +2 regression tests; re-run drops z-lab off orphans, genuine `.history`-only orphans stay flagged. |
| PN1 | M | 🟢 | `configweb` rename guard / `processmgr` List | **Fixed:** a profile-id rename via the configweb editor is now refused (`ErrProfileInUse`, actionable message) while a live backend is registered under the old id — gated by `Deps.InstanceInUse` wired from `svc.Mgr.List()` — so a running benchmark's backend + logs can no longer be stranded on the pre-rename id (detail row PN-series) |
| PV1 | L | 🟢 | `profilestore.saveIfPresent` (fix); rename = configweb by design | Re-diagnosed: the Agents-A1 rename `...256k-{backend}`→`...{backend}-256k` came from the configweb editor Rename path (only `FSStore.Rename` caller; ID-field edit matching the naming-convention 256k-last rule), NOT a store save-bug — the "reverted args" were the profile's own debug-edit state at rename time, carried faithfully (no store data loss). **Fixed** the one genuine latent defect found while tracing: `MarkLastUsed` + `Get`'s self-heal did an unlocked read-modify-write that could recreate an old-id file after a concurrent rename/delete (PN1-adjacent ghost). New `FSStore.saveIfPresent` gates both auto-saves on the file still existing (narrow stat→rename TOCTOU remains). Regression tests `TestFSStore_saveIfPresent_{PersistsWhenPresent,AbsentIsNoop,DoesNotResurrectDeleted}` (teeth-verified). |
| PV2 | M | ❓ | `beellama-rtx3090` / Agents-A1 profile | One `llama-bench` fill-90 run for the BeeLlama Agents-A1 profile SIGABRTed/core-dumped under high memory pressure; immediate back-to-back rerun passed, so this is a flaky/pressure-sensitive observation needing repro |
| PV3 | M | ❓ | `httpproxy` / managed backend launch | Early managed launches of the Agents-A1 llama.cpp profile exited before health/cleaned up while exact manual argv survived; later tuned profile benchmark passed, so root cause is still unidentified |
| L3 | I | 🟢 | `internal/service/monitor/metrics.go` | Regex is llama.cpp-specific (harmless wasted work) |
| B1 | C | 🟢 | `internal/ui/root.go` | Tab/Shift+Tab swallowing inputs in forms & sub-views |
| B4 | H | 🟢 | `models_actions.go` | Models action menu missing "Use in existing profile" |
| B5 | H | 🟢 | `models_actions.go` | huh form in Models action menu doesn't submit on Enter |
| B6 | M | 🟢 | `statusbar.go` | Status bar global hint omits [?] help |
| B7 | M | 🟢 | `profiles.go` | Profile detail-pane hint omits [L] (collapsed into B3) |
| B8 | M | 🟢 | `fs_store.go` | Duplicate carried the source port; risked bind collision |
| B9 | M | 🟢 | `models_scan.go` | Invalid scan path in Models header — truncated and now removable in-app |
| B10 | L | 🟢 | `models_messages.go` | Models filter shows `filter: ""` while filtering (race) |
| B11 | L | 🟢 | `profiles_update.go` | ProfilesPage does not reload on tab focus |
| B12 | L | 🟢 | `profiles.go` | Editor preserves draft after accidental global Tab |
| B13 | Doc | 🟢 | `server_update.go` | Monitor footer claimed [Tab] cycle view while Tab was global |
| D1 | M | 🟢 | `README.md` | Both rewritten for multi-backend; README shortcuts realigned to help.go |
| D2 | M | 🟢 | `AGENTS.md` | Schema version v9761 everywhere |
| D3 | M | 🟢 | `AGENTS.md:162` | Schema version: embedded-v9761 |
| D4 | M | 🟢 | `internal/service/monitor/AGENTS.md` | 6 goroutines per subscription — confirmed correct |
| D5 | M | 🟢 | `internal/service/processmgr/AGENTS.md` | Log path documented as `<profile-id>-<port>.log` |
| D6 | L | 🟢 | `internal/service/AGENTS.md` | Service-layer KB v7376 → v9761 |
| T1 | M | 🟢 | `monitor/logs_test.go` | Regression test for partial-line flush added |
| T2 | M | 🟢 | `monitor/subscribe_test.go` | Backpressure-drop test added |
| T3 | L | 🟢 | `processmgr/manager_test.go` | PYTHONUNBUFFERED=0 override already covered |
| T4 | M | 🟢 | TUI responsive layout | Negative-width safeguards in centeredDivider/renderBar |
| T5 | L | 🟢 | `internal/ui/{components,pages}` + `root_test.go` | 11 color-on glyph tests assumed ambient NO_COLOR unset; made hermetic with `t.Setenv("NO_COLOR","")` |
| S1 | M | 🟢 | curated enums + `validator/rules.go` | list-valued enum + extraArgs passthrough + draft-dflash |
| S2 | M | 🟢 | `backendschema/curated_{sglang,beellama,buun}.go` | sglang parser enums lagged 0.5.9 detector maps + PT help text; beellama/buun `spec-type` not list-valued (binary chains) |
| N1 | M | 🟢 | `scripts/setup-sndr-backend.sh` | SNDR_WHEEL_INDEX override for rotating index |
| N2 | M | 🟢 | `docs/sndr-backend.md` + SNDR profiles launch.env | GENESIS_ENFORCE_VERSION_RANGE=1 added |
| P1 | H | 🟢 | `internal/cli/serve.go` + `config.go` | `HealthCheckTimeout` made configurable + default raised to 360s |
| P3 | M | 🟢 | `internal/service/processmgr/enrichment.go` + `readiness.go` | Liveness check added to WaitHealthy/WaitReady |
| P4 | H | 🟢 | `httpproxy` swap / `processmgr.Kill` (`TerminateTree`) | **Fixed:** `TerminateTree` now CONFIRMS death after SIGKILL (polls up to `killConfirmGrace`=15s, returns `ErrStillAlive` if the target survives GPU/CUDA teardown), `processmgr.Kill` keeps the tracked+registry entry and returns an error instead of purging on a failed terminate, and the swap aborts with a retriable 503 `backend_busy` (`s.current` preserved) instead of launching into contended VRAM — closing the alive-but-unregistered orphan that OOMed the next load (detail row DF11) |
| P4 | M | 🟢 | `httpproxy/response.go` + `httpproxy/anthropic_types.go` | vLLM 0.24 emits reasoning under `reasoning`; the direct non-stream safety net and the translated Anthropic/Responses/Gemini paths accept both `reasoning` and `reasoning_content` (reasoning_content wins). The direct OpenAI non-stream safety net now mirrors reasoning-only assistant turns into `content` regardless of `finish_reason` (including `length`), guarding tool-call turns; see P7 for the streaming counterpart. |
| P5 | M | 🟢 | `httpproxy/responses_stream.go` | Responses streaming `finish()` assembled the terminal `response.completed` snapshot output from message + tool items only, dropping the reasoning item it streamed incrementally (hits canonical `reasoning_content` too, not just the vLLM alias). Snapshot-based clients lost reasoning; the reasoning item is now included first, matching non-stream `buildResponsesResponse`. |
| P6 | M | 🟢 | `httpproxy/anthropic_{translate,stream}.go` + `httpproxy/responses_{translate,stream}.go` + `httpproxy/gemini_{translate,stream}.go` | Translated APIs could return reasoning/thought-only completions when a backend exhausted budget or failed to transition to final text; clients like llm-wiki reject those as "reasoning but no actual response content." When no text and no tool call exists, the proxy now mirrors the model-produced reasoning into the normal text/content channel while preserving the original finish reason (`max_tokens`/`MAX_TOKENS`). |
| P7 | M | 🟢 | `httpproxy/stream_mirror.go` + `httpproxy/proxy.go` + `httpproxy/response.go` | The direct OpenAI `/v1/chat/completions` catch-all was the only route not mirroring reasoning-only completions: streaming was pure passthrough and non-stream skipped `finish_reason:"length"`. Clients like llm-wiki via `LLMWIKI_PROVIDER=openai` reject a reasoning-only assistant turn. The proxy now, on this route (stream and non-stream), mirrors the model-produced reasoning into `content` — for SSE a synthetic `delta.content` chunk is injected before the finishing chunk — whenever a choice has no content and no tool call; all upstream frames pass through byte-for-byte and the original `finish_reason` is preserved. |
| BM1 | M | 🟢 | `internal/service/benchmark/llamabench_probe.go` | llama-bench fill-90 prompt sizing fixed |
| DL1 | M | 🟢 | `internal/service/downloadmgr/pathing.go` | --snapshot checks file instead of directory |
| DL2 | M | 🟢 | `internal/service/downloadmgr/manager.go` | Queued downloads auto-promoted instead of abandoned |
| V1 | H | 🟢 | `backends/vllm-nightly/backend-build.sh` + venv | Build script + venv rebuilt; fp8-KV inference restored |
| AUD-A1 | C | 🟢 | `processmgr/recover.go` | Reconcile dropped live wrapper-exec backends (comm≠binary) → identity via `StartTicks` + per-kind cmdline token keeps them |
| AUD-A2 | C | 🟢 | `app/bootstrap.go` | Every Bootstrap reconciled (a write); now owner-only (`AsStateOwner`), observers use identity-filtered List |
| AUD-A3 | H | 🟢 | `processmgr/manager.go` + `procutil` | Kill signaled only the leader; `TerminateTree` sweeps the Setsid process group (vLLM/SGLang trees) |
| AUD-A4 | H | 🟢 | `processmgr/restart.go` | Intentional Kill/unload/swap resurrected the backend; `killRequested` guard set before signaling |
| AUD-A5 | H | 🟢 | `processmgr/restart.go` + `launch.go` | RestartCount reset each generation → `MaxRestarts` never bound; carried via `pendingRestarts`, reset on first healthy check |
| AUD-A6 | H | 🟢 | `httpproxy/handler.go` | Crashed loaded backend gave permanent 502; `ensureLoaded` liveness-guards `s.current` and relaunches |
| AUD-A7 | H | 🟢 | `processmgr/registry.go` + `fsx/flock.go` | `instances.json` last-writer-wins; `mutateRegistry` = flock-guarded delta RMW |
| AUD-A8 | M | 🟢 | `proxysupervisor/supervisor.go` | Supervisor SIGKILLed serve before it freed VRAM; Stop grace 45s + post-kill backend sweep |
| AUD-A9 | M | 🟢 | `proxysupervisor/supervisor.go` | Transient probe dropped state + duplicate-spawn recorded dead PID; identity + 3-strike hysteresis + child verify |
| AUD-A10 | M | 🟢 | `processmgr/liveness.go` + `restart.go` | Adopted (reaper-less) instances ignored restart policy; liveness now applies it (`hasReaper`) |
| AUD-A11 | M | 🟢 | `procutil` + `processmgr` | `Alive` blind to PID recycling; `SameProcess` (PID + `/proc` starttime) in liveness/Kill/reconcile |
| AUD-A12 | M | 🟢 | `processmgr/launch.go` | Post-Start registry-write failure leaked a live child; Launch now kills it and returns the zero instance |
| AUD-A13 | M | 🟢 | `proxysupervisor` + `cli/instance_lifecycle.go` + `ui/pages/server_restart.go` | No kill path for a wedged proxy; `ForceStop` + `ErrProxyDegraded` + `stop --force` / TUI force-confirm |
| AUD-B1 | M | 🟢 | `processmgr/watchdog.go` | Dead watchdog (broken reflection) deleted; semantics live in `restart.go` |
| AUD-B2 | L | 🟢 | `AGENTS.md` §4 | Flock claim corrected: only instance lifecycle + benchmark take it |
| AUD-B3 | L | 🟢 | `cmd/model-loader/main.go` | TUI exit counted `Crashed` instances as running; now filtered |
| AUD-B4 | L | 🟢 | `AGENTS.md` + code | "5 saveRegistry callsites" contract → 6 `mutateRegistry` deltas |
| AUD-B5 | I | 🟢 | `httpproxy/handler.go` | Client cancel before swap mapped to 504; now 499 `request_canceled` |
| AUD-C1 | M | 🟢 | `processmgr/prune.go` + `app/bootstrap.go` | Backend logs never pruned; owner-boot prune keeps 10 newest/profile |
| AUD-C2 | M | 🟢 | `metricsstore/store.go` + `app/bootstrap.go` | `Compact` never called + O(n²) shrink; single-pass trim wired at owner boot (7d/4MiB) |
| AUD-C3 | L | 🟢 | `processmgr/manager.go` | `List` did disk IO under `m.mu` every call; stat-cached, IO outside lock, identity merge gate |
| AUD-C4 | L | 🟢 | `proxysupervisor/supervisor.go` | `Status` held mutex across a 500ms HTTP + allocated a client per call; probe outside lock, shared client |
| AUD-C5 | L | 🟢 | `processmgr/exit_info.go` | `readStderrTail` read the whole log; now a bounded 64 KiB tail read |
| AUD-C6 | L | 🟢 | `internal/log/log.go` + `app/bootstrap.go` | Every Bootstrap rotated the log; rotation now owner-only (`Rotate` gate) |
| AUD-C7 | L | 🟢 | `processmgr/readiness.go` | `waitForLogToken` re-read the whole log each poll; now incremental offset + carry tail |

### GA-series — broad Go-audit follow-up (2026-07-07)

Findings from the broad Go audit not covered by the backend-lifecycle plan, re-verified and resolved this session.

| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| N-C1 | M | 🟢 | `httpproxy/extract.go` + `body.go` | Oversized chunked (unknown-length) request body was truncated; now forwarded untruncated via `prefixedBody`, model extraction skipped |
| N-C2 | M | 🟢 | `httpproxy/response.go` + `proxy.go` | Oversized non-streaming response was truncated to 8 MiB; now streamed through untouched (length −1) with a Content-Length fast-path skip |
| N-C5 | M | 🟢 | `httpproxy/server.go` + `handler.go` | `inflightWG.Add` at handler entry raced `Wait` at counter 0; replaced with a polled `serving` atomic counter |
| N-C6 | M | 🟢 | `httpproxy/handler.go` | Unload drain waited on requests parked at `swapMu`; `serving` counter covers only the backend-use phase so drain never stalls |
| N-C7 | L | ⚪ | `httpproxy` swap path | Swap kills in-use backend — by design: single-GPU hot-swap must free VRAM before the new launch; client on old model fails on swap |
| N-C8 | L | 🟢 | `processmgr/launch.go` | `launchForeground` pre-Start errors (mkdir/open log) leaked `fgPID=-1`; sentinel now reset to 0 on both |
| N-C10 | M | 🟢 | `downloadmgr/manager.go` | `Resume` had no queue-membership dedup; a duplicate promotion respawned a 2nd worker. Guarded with `slices.Contains` |
| N-C13 | L | 🟢 | `backendcatalog/probe.go` + new `internal/shellsplit` | Probe used `strings.Fields` (whitespace-shreds quoted paths); consolidated quote-aware splitter shared by launch/resolver/probe |
| N-C14 | L | 🟢 | `ui/pages/backends{,_probe,_webedit}.go` | Probe used `context.Background` + unbuffered chan → producer goroutine leak; ctx now cancelled on timeout/reload/done/Cleanup |
| N-C15 | M | 🟢 | `ui/pages/benchmark{,_run}.go` | Benchmark page had no `Cleanup()`; TUI quit orphaned harness Docker trees. Cleanup cancels the run + bounded-waits the engine ack |
| N-C16 | L | 🟢 | `benchmark/{terminalbench,deepswe,swebenchpro}.go` | Progress poller not joined before `defer os.RemoveAll` → WalkDir/ReadFile race; poller now joined via `pollDone` |
| N-P2 | L | 🟢 | `ui/pages/server_monitor.go` | `pm.List` stat + `metricsstore.Append` ran inline in the Bubble Tea update loop; moved to an async `tea.Cmd` |
| N-P3 | L | 🟢 | `monitor/slots.go` | Default slots/health client had no timeout → a silent backend wedged the poller; default now `&http.Client{Timeout:5s}` |
| N-P5 | L | 🟢 | `ui/pages/backends{,_probe}.go` | Spinner tick ran forever; now armed only when probe/refresh starts and stops re-arming when idle |
| N-P6 | L | 🟢 | `modelscanner/scanner.go` | Trailing progress/error sends were plain (blocking); wrapped in `select … case <-ctx.Done()` so cancel unblocks a full buffer |
| N-P7 | L | ⚪ | `httpproxy/response.go` | Full-body JSON normalization inherently needs the whole body; bounded 8 MiB, loopback-only. Step 2 adds an oversized-body pass-through |
| P-C4 | M | 🟢 | `processmgr/launch.go` | Reaper started before the registry upsert → fast-crash `Crashed` delta clobbered; reaper now starts after the upsert commits |
| P-C9 | M | 🟢 | `internal/fsx/atomic_write.go` | `WriteJSONAtomic` used a fixed `path+".tmp"`; concurrent writers renamed each other's half-written temp. Now `os.CreateTemp` unique name |
| P-C11 | L | ⚪ | `proxysupervisor/supervisor.go` | Proxy zombie child — already fixed: `go func(){ _ = cmd.Wait(); close(exited) }()` reaps the detached serve |
| P-C12 | M | 🟢 | `proxysupervisor/supervisor.go` | `Start` held `mu` across spawn+10s port wait and ignored caller ctx; added `startMu`, honors ctx, `mu` only guards state |
| P-P1 | L | ⚪ | `metricsstore/store.go` | `Compact` loads whole JSONL — by design: file is ≤ maxBytes post-compaction, read is boot-only, records ~120 B |

### UIUX-series — UI/UX audit (2026-07-07)

TUI (`internal/ui/`) + web editor (`internal/service/configweb/`) audit; 001–008 & 010 implemented, 009 kept by decision.

| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| UIUX-001 | H | 🟢 | `configweb/backend.gohtml` + `backend_handlers.go` | Backend editor had no inline field validation; save/validate now emit `data-field` issues decorated onto fields |
| UIUX-002 | H | 🟢 | `configweb/customize.gohtml` + `handlers_customize.go` | Customize "Add flag" needed a reload; add-flag now swaps the live `#customize-flags` grid |
| UIUX-003 | M | 🟢 | `ui/components/confirm.go` + pages | Per-page confirm hint copy drifted; canonical `components.ConfirmHints` constant |
| UIUX-004 | M | 🟢 | `ui/components/hf_file_picker.go` | HF file picker lacked vim/paging keys; added `j/k/g/G/home/end/pgup/pgdown` |
| UIUX-005 | M | 🟢 | `ui/pages/server_subviews.go` | Server sub-view fallbacks showed jargon ("no subscription"); replaced with `EmptyState` copy |
| UIUX-006 | M | 🟢 | `configweb/backend.gohtml` + `static/editor.js` | Backend editor lacked Ctrl/Cmd+S save + Escape-cancel shortcuts; added, shared editor.js |
| UIUX-007 | L | 🟢 | `ui/pages/benchmark_wizard.go` | Benchmark wizard steps had no position; added `(1/3)`/`(2/3)`/`(3/3)` indicator |
| UIUX-008 | L | 🟢 | `configweb/assets/static/app.css` | Web editor had no light theme; literals hoisted to vars + `prefers-color-scheme: light` block |
| UIUX-009 | L | ⚪ | `ui/pages/profiles.go` | Profiles hints tail stays in `[?]` help — F-10 tradeoff kept by decision |
| UIUX-010 | L | 🟢 | `ui/components/playground_modal.go` + `service/playground` | Dead playground modal + unreachable playground service deleted (DEAD-01) |
| UIUX-011 | M | 🟢 | `ui/pages/benchmark_{run,compare,update,wizard}.go` | Benchmark error flashes never auto-cleared (dropped clear `tea.Cmd`); stuck error stacked on run failure |
| UIUX-012 | H | 🟢 | `benchmark/terminalbench.go` + `runner.go` + config | Completed Terminal-Bench left `tb` running for hours flooding the proxy → pinned GPUs; hang watchdog + optional auto-unload + stall/timeout handling |
| UIUX-013 | M | 🟢 | `benchmark/{deepswe,swebenchpro,terminalbench}.go` | Audit of the UIUX-012 hang class across all benchmarks: `deep-swe` shared it (fixed — same watchdog via shared `runHangWatchdog`); `swe-bench-pro` narrower (agent-step flood only, mitigated); single-turn + `codegen` + `llama-bench` not exposed |


### BR-series — Benchmark reliability audit (2026-07-08)

Confirmed defects from the benchmark reliability & observability review, all fixed this session (regression tests in `benchmark/br_regression_test.go` + `cli/benchmark_test.go`); full evidence and the surrounding proposals in `BENCHMARK_RELIABILITY.md`. The cross-cutting recommendations T1 (`slog` logging), T3 (scaled per-item deadline), T5 (`FailPhase`) and T7 (incremental persistence) were implemented the same session as enhancements — documented in `AGENTS.md` + `BENCHMARK_RELIABILITY.md`, not tracked here (not defects). T8 + P2 refinements remain proposals.

| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| BR1 | M | 🟢 | `benchmark/ragasbench.go` + `summarybench.go` | ragas/summary grading failure now sets `res.Err` (+ `tr.Error`) instead of a silent 0; first criterion failure short-circuits the rest; cancel mid-grade keeps the inference result without a fake error |
| BR2 | M | 🟢 | `benchmark/runner.go` (`allItemsFailed`) + `cli/benchmark.go` | An all-`Err` result set now returns a run-level error ⇒ excluded from the leaderboard; `--min-solve` prints the errored-item count so a pass over a thin scored subset is visible |
| BR3 | M | 🟢 | `benchmark/terminalbench.go` + `deepswe.go` | Non-zero harness exit with an INCOMPLETE result set (`scored < total`) is now a partial run; a complete set + non-zero exit stays success with the exit traced in the harness log (no dependence on harness exit-code semantics) |
| BR4 | M | 🟢 | `benchmark/codegenbench.go` | `python3` absence now fails fast in `Prepare` (before the backend load) instead of persisting a fake 1-item run |
| BR5 | M | 🟢 | `benchmark/longcontext_probe.go` | longctx applies the same ~100 tok/s prefill deadline floor as llama-bench so a high `long_context_tokens` no longer guarantees a timeout |
| BR6 | M | 🟢 | `benchmark/llamabench_probe.go` | A failed rep degrades to the reps already measured; the preset errors only when nothing was measured; warmup + rep failures are traced in Detail |
| BR7 | M | 🟢 | `benchmark/harness_log.go` (new) + `{terminalbench,deepswe,swebenchpro}.go` + config + `app/bootstrap.go` | Harness stdout/stderr teed to `<state>/benchmark/harness/<mode>-<ts>.log` (survives a crash) with a bounded 64 KiB in-memory tail; transcript/errors name the log path; owner-boot prune keeps 10 newest per mode (`PruneHarnessLogs`, mirrors AUD-C1) |
| BR8 | L | 🟢 | `cli/benchmark.go` (`benchModeList`) | Usage + unknown-mode messages derive the mode list from `benchmark.ModesInOrder()` — no more drift |

For full descriptions of all issues, including those considered by design, see `AGENTS.md`.

### DF-series — DFlash/DSpark provisioning + backend-update audit (2026-07-09)

Provisioning the Qwen3.6-27B AutoRound int4 + DFlash/DSpark speculative-decode profiles and the mainline backend refresh. Fixed items landed this session; ⚪/🟡 items are upstream/hardware limits documented for the profile library.

| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| DF1 | M | 🟢 | `backendschema/curated_sglang.go` | sglang `speculative-algorithm` curated enum lacked `DFLASH` (PR#23000 adds it) → valid value rejected in `args`; added `DFLASH` + English HelpText; schemas refreshed |
| DF2 | M | ⚪ | sglang gptq marlin (`gptq_marlin_repack`) | AutoRound int4 layer with `out_features=48` fails Marlin repack (`size_n not divisible by 64`) on sglang-dflash + sglang-nightly; `--quantization gptq` override refused (`auto-round != gptq`). Model unservable on sglang; vLLM Marlin pads + loads it. sglang profile dropped |
| DF3 | M | ⚪ | `vllm-dflash` (PR#40898, June base) | hybrid+fp8 KV `unify_kv_cache_spec_page_size` AssertionError at 256k; fixed in vLLM 0.24.0 → DFlash profile runs on `vllm-nightly` (base DFlash, no SWA drafter) |
| DF4 | M | 🟡 | vLLM DFlash `build_per_group_and_layer_attn_metadata` | ~394 MiB unprofiled decode alloc OOMs GPU0 (shared w/ ~2.4 GiB desktop) at the gpu-mem needed for 256k KV → DFlash profile capped at 224k. Mitigation: lower gpu-mem/ctx, or headless GPU0 |
| DF5 | L | 🟢 | `~/.cache/flashinfer` JIT cache | `build.ninja` poisoned with a stale venv path after a backend dir rename/rebuild → `ninja: missing sampling.cu`; cleared cache |
| DF6 | M | 🟢 | `backends/sglang-stable/.venv` | uv venv non-relocatable — `activate` hard-coded stale `backends/sglang/.venv` (dir renamed) → serve exit 127; recreated venv (`backend-build.sh --recreate`) |
| DF7 | L | ⚪ | `backends/beellama.cpp` (fork) | CUB `DeviceTopK::MaxPairs`/`make_counting_iterator` removed in CUDA 13.3 → rebuild fails; already at latest upstream (85e22ea0b), working binary preserved, no regression |
| DF8 | M | 🟢 | `backends/sglang-nightly/sglang-serve.sh` | serve wrapper never set nvcc's host compiler → CUDA 12.8 nvcc rejects system gcc-16 for runtime JIT (`gptq_marlin_repack`); pinned bundled conda gcc-14 (CC/CXX/CUDAHOSTCXX/-ccbin) |
| DF9 | L | ⚪ | Avesed DSpark drafter config + vLLM `qwen3_dspark` | drafter `architectures:["DFlashDraftModel"]` mis-routes (→ DeepSeek-V4 DSpark, dies on `config.hc_mult`); corrected to `["Qwen3DSparkModel"]` (→ qwen3_dspark). vLLM's qwen3_dspark SKIPS the confidence_head ("not wired into inference yet") — only the Markov head is active |
| DF10 | M | 🟢 | profile `qwen3.6-27b-int4-autoround-dflash-vllm-tp2-textonly-228k` | Shipped `gpu-memory-utilization: 0.82` OOMs on the **first inference** whenever ~400 MiB of desktop processes sit on GPU0 (load succeeds; DFlash decode workspace alloc fails) — profile unusable as-is on the desktop-shared rig. Retuned to 0.72 and verified: 512 tok @ 87.9 tok/s decode via direct backend port; description updated |
| DF10 | M | 🟢 | profile `qwen3.6-27b-int4-autoround-dflash-vllm-tp2-textonly-228k` | Shipped `gpu-memory-utilization: 0.82` OOMs on the **first inference** whenever ~400 MiB of desktop processes sit on GPU0 (load succeeds; DFlash decode workspace alloc fails) — profile was unusable as-is on the desktop-shared rig. Retuned to 0.72 and verified: 512 tok @ 87.9 tok/s decode via direct backend port; description updated |
| DF11 | H | 🟢 | `httpproxy` swap / `processmgr.Kill` (`TerminateTree`) | Same incident as P4. Root cause: `TerminateTree` returned before confirmed death (a GPU backend wedged in CUDA teardown stays Alive + holds VRAM after SIGKILL), `Kill` purged `instances.json` unconditionally, and the swap launched the next backend over the still-resident one → alive-but-unregistered orphan → OOM. **Fixed 2026-07-09:** (1) `procutil.TerminateTree` polls `Alive` up to `killConfirmGrace` (15s) after SIGKILL and returns `ErrStillAlive` if it never dies; (2) `processmgr.Kill` returns `fmt.Errorf("terminate pid …: %w", err)` and keeps the tracked/registry entry (no orphan) when terminate fails, `killRequested` stays set so the eventual death isn't restarted; (3) `httpproxy.killOldBackend` aborts the swap with a 503 `backend_busy` and leaves `s.current` on the still-live backend instead of launching a new one. Regression: `TestTerminateTreeConfirmsDeathBeforeReturn`, `TestEnsureLoaded_KillFailureAbortsSwap`; verified `go build ./...` + package tests |

New backends registered this session: `vllm-dflash` (vLLM PR#40898, DFlash+SWA, pinned) and `vllm-dspark` (vLLM git main, DSpark+DFlash). DFlash-vs-DSpark A/B on the Intel AutoRound target (same engine, T=0, 4×1024 tok): DFlash 161.8 tok/s / mean-accept-len 5.0 vs DSpark 99.6 / 1.87 — DFlash wins (DSpark drafter is on-policy for `Avesed/Qwen3.6-27B-W4A16`, not this target, and its confidence head is unused).

### PN-series — Profile naming operations (2026-07-09)

| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| PN1 | M | 🟢 | `configweb` rename guard (`InstanceInUse`) | A benchmark/live backend registered under the old profile id could strand `instances.json` + logs on the pre-rename id after a configweb editor rename. **Fixed 2026-07-09:** `configweb.Deps.InstanceInUse func(profileID) bool` (wired in `cmd/model-loader/main.go` from `svc.Mgr.List()`, identity-filtered, `!Crashed`) makes `doPersist`'s rename branch return `ErrProfileInUse` — "stop the running instance/benchmark first, then rename" — surfaced through `renderIssueError` so the editor blocks the save. A plain in-place save of a loaded profile is untouched; a nil checker (tests/backend editor) preserves prior behavior. Regression: `TestSaveHandler_RefusesRenameWhenInstanceInUse` + `…AllowsRenameWhenNotInUse` |

### PV-series — Profile validation/debug findings (2026-07-09)

| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| PV1 | L | 🟢 | `profilestore.saveIfPresent` (fix) · rename = configweb by design | **Investigated + re-diagnosed + fixed.** The observed rename of both Agents-A1 profiles from `agents-a1-35b-a3b-q4km-mtp-vision-256k-{backend}` to `agents-a1-35b-a3b-q4km-mtp-vision-{backend}-256k` was NOT a profilestore save-path bug. Traced every write path: `Save`/`Create` only ever write `<p.ID>.json` (never remove another basename); `Get` self-heals `p.ID` to the filename and re-saves; `profile edit` forces `base.ID = resolvedID` (id/filename can't diverge). A basename only vanishes via `FSStore.Delete` or `FSStore.Rename`, and `Rename` (Create-new + Remove-old) is called ONLY by the configweb editor's `doPersist` when the draft's ID field differs from the original. So the rename came from a web-editor ID edit — matching the naming convention (`-256k` must be the final segment; the `...256k-{backend}` spelling violated it) — most plausibly parallel operator curation. The "reverted args" were the llama.cpp profile's actual content at rename time (a debug-stripped variant from this session's own edits), carried faithfully by Rename — no store-side data loss. Reproduction attempts (create --id, edit, create without --id → Slugify, mismatched id/filename heal) all preserved id=filename correctly. **Genuine latent defect fixed:** `MarkLastUsed` (fires on every first healthy `/health`, e.g. mid-benchmark) did an UNLOCKED get→modify→save, unlike `profile edit`/`rename`/`pin` (all `withProfileLock`); with `FSStore.Rename` taking no flock, a `MarkLastUsed(oldID)` racing a concurrent rename/delete could recreate `oldID.json` (a PN1-adjacent old-id ghost). Fix: new `FSStore.saveIfPresent` gates the two auto-save paths (`MarkLastUsed` + `Get`'s self-heal) on `os.Stat(<id>.json)` — a removed profile is never resurrected (narrow stat→rename TOCTOU remains; intentional writers `Save`/`Create`/`Rename` unchanged). Regression tests `TestFSStore_saveIfPresent_{PersistsWhenPresent,AbsentIsNoop,DoesNotResurrectDeleted}` (teeth-verified: guard removed → the two ghost tests fail). `go build ./...` + `go test ./...` green (37 pkgs). Deeper store-wide flock unification (Rename taking both ids' locks) NOT done — out of scope for the minimal guard the operator chose. |
| PV2 | M | ❓ | `beellama-rtx3090` / Agents-A1 profile | A `model-loader benchmark --profile agents-a1-35b-a3b-q4km-mtp-vision-beellama-256k --mode llama-bench` run initially failed at fill-90 with backend 502 and journal/core evidence of a BeeLlama `llama-server` SIGABRT (pid 999806, 2026-07-09 09:05:55). Immediate back-to-back rerun completed fill-90, so treat as flaky/pressure-sensitive until reproduced. |
| PV3 | M | ❓ | `httpproxy` / managed backend launch | Several early managed launches of the Agents-A1 llama.cpp profile exited before health / logged `cleaning up before exit`, while an exact manual `llama-server` argv survived and answered chat. Later restored/tuned managed profile benchmark completed, so root cause remains unidentified (possible swap/launch context or ambient pressure). |

### CU-series — Cleanup skill analyzer (2026-07-09)

| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| CU1 | H | 🟢 | `analyze.py::protected_closure` (cleanup skill) | Closure harvested paths only from `model`, a 3-key arg whitelist (`mmproj`/`spec-draft-model`/`chat-template-file`), and `extraArgs` entries literally starting with `/`; paths inside JSON-valued args (`--speculative-config '{"model":"/…/z-lab/Qwen3.6-27B-DFlash"}'`) and non-whitelist path keys (`mtp-head`) were invisible → in-use DFlash/MTP drafters misreported as `store-orphan-repo` (2 live DFlash profiles reference `z-lab/Qwen3.6-27B-DFlash` this way). Caught in item-by-item review; nothing deleted. **Fixed 2026-07-09**: recursive `harvest()` extracts every absolute path from all `args`/`extraArgs` values incl. JSON objects/arrays + nested containers, dirs protected as subtrees, no key whitelist. +2 regression tests in `test_analyze.py` (18/18 pass). Verified: live re-run drops `z-lab/Qwen3.6-27B-DFlash` from orphans (5→4); genuine `.history`-only `Avesed/Qwen3.6-27B-DSpark` correctly retained. |

## Verification

- `go build ./...` → exit 0.
- `go test ./...` → all packages OK.
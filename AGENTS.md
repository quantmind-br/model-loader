# AGENTS.md — model-loader

**Repository knowledge base.**

> `CLAUDE.md` symlink to this file — edit only `AGENTS.md`. Per-directory `AGENTS.md`/`CLAUDE.md` KBs sit beside code they describe; read one in dir you work in. This root file = map.

---

## 1. Project Overview

**model-loader** = Go 1.26.2 terminal UI **and** headless CLI for running local LLM inference servers on single workstation (reference rig: RTX 3090, 24 GiB). Manages *launch profiles*, supervises backend *processes*, fronts them with one OpenAI-shaped HTTP proxy that hot-swaps active model on demand.

- **For:** single operator (power user / ML engineer) curating many tuned launch configs for one GPU. **Not** multi-tenant — proxy binds loopback, no auth, guarded by single-instance lock.
- **Does:** author profiles via schema-driven web editor → launch backend via `processmgr` → route inference thru proxy (request `model` field selects profile) → monitor logs/slots/GPU/throughput → browse local GGUF + Hugging Face → benchmark profiles.
- **Backends (8 `BackendKind`s, `internal/domain/`):** `llama-server`, `vllm`, `sglang`, `dflash`, `buun-llama-cpp`, `beellama-cpp`, `unsloth`, `tabby`. User registers each backend (binary + kind) in catalog; tool downloads no engines.
- **Surfaces:** 5-tab TUI (default, no subcommand), headless `serve` proxy daemon, full Cobra CLI mirroring TUI. Processes deliberately **orphaned** on TUI exit so inference survives.
- **Module:** `github.com/quantmind-br/model-loader` · **Repo:** https://github.com/quantmind-br/model-loader

## OpenWiki

This repository has documentation located in the /openwiki directory.

Start here:
- [OpenWiki quickstart](openwiki/quickstart.md)

OpenWiki includes repository overview, architecture notes, workflows, domain concepts, operations, integrations, testing guidance, and source maps.

When working in this repository, read the OpenWiki quickstart first, then follow its links to the relevant architecture, workflow, domain, operation, and testing notes.

---

## 2. Architecture & Data Flow

### 2.1 Module map

| Layer | Path | Role |
|-------|------|------|
| Entry / dispatch | `cmd/model-loader/main.go` | `cli.TUIRunner = runTUI`; `cli.Execute()`. `runTUI` takes flock, calls `app.Bootstrap`, wires TUI-only services + 5 pages, runs `tea.NewProgram(root, WithAltScreen)`; on exit logs (not kill) orphaned instances |
| Dev tools | `cmd/regenerate-schemas/`, `cmd/scripts/print_args.go` | `go run` helpers (re-parse every backend `--help`; print resolved args) — not CLI subcommands |
| Shared bootstrap (DI) | `internal/app/bootstrap.go` | One `Services` container for TUI, CLI, headless modes |
| Config | `internal/config/` | Viper TOML loader + one-time migrations |
| CLI | `internal/cli/` | Cobra tree; never imports `internal/ui` |
| TUI | `internal/ui/` | Bubble Tea 5-tab `RootModel`, pages, components, theme |
| Domain | `internal/domain/` | `Profile`, `Instance`, `Backend`, `FlagSchema`, `BackendValidationSchema`, `Presentation`, `BackendKind` — zero external deps |
| Services | `internal/service/*` | **25** self-contained packages, one concern each (§2.2) |
| Logging | `internal/log/` | file-only `slog`, conditional rotation (state owners only), `log.Nop()` fallback |
| Leaf utils | `internal/service/internal/{fsx,procutil,ptrutil,shellsplit}` | `WriteJSONAtomic`/`WriteJSONExclusive`/`ReadJSON[T]`; `Alive(pid)`; generic `Ptr[T]`; `Split` (quote-aware command tokenizer) |

### 2.2 Service layer (25 packages)

Each owns one concern, exports own interface, takes `Config` with functional options (`WithLogger`…), falls back to `log.Nop()` not nil logger.

| Service | Purpose |
|---------|---------|
| `processmgr` | Process lifecycle, recovery, history, restart policy, liveness — **largest service** |
| `httpproxy` | OpenAI-shaped reverse proxy w/ implicit model swap |
| `proxysupervisor` | Detached-proxy state machine (idle→starting→running→stopping); `proxy-state.json` |
| `backendcatalog` | Multi-backend catalog (`catalog.json`) + profile→exe/schema resolver + prober |
| `backendschema` | Schema-gen orchestrator (`--help` parse + curated overlay + embedded fallback) |
| `llamahelp` | llama-server `--help` parser + **embedded v9761** schema |
| `buunhelp`/`dflashhelp`/`sglanghelp`/`vllmhelp`/`unslothhelp`/`tabbyhelp` | Embedded/curated schemas per backend kind (Python/native `--help` too unstable to parse live); `tabbyhelp` covers ExLlamaV2/V3 TabbyAPI flags incl. `gpu-split`/`tensor-parallel` for dual-GPU |
| `llamabin` | Binary path resolver (PATH + Python fallback) |
| `profilestore` | Profile JSON persistence (one file per profile; per-file flock RMW; export/import; history) |
| `downloadmgr` | Hugging Face download queue + detached worker subprocesses |
| `hfhub` | HF Hub API client (search, repo info, download; `HF_BASE_URL` override) |
| `modelscanner` | GGUF metadata scan (magic `0x46554747`, header, KV; quant/params regex) |
| `monitor` | Logs/slots/GPU/metrics stream via `Subscribe` (nvidia-smi) |
| `metricsstore` | Rolling metrics time-series (Append/Read/Compact) |
| `benchmark`/`benchmarkstore` | Eval engine (judge, math/codegen/ragas/summary/instruction/mmlu, llama-bench, longctx) + per-run JSON |
| `validator` | Flag + cross-field validation, `Report` aggregation |
| `migration` | One-time legacy-binary-path → backend-ID migration |
| `sizing` | GPU-memory fit calc (`Suggest`, `Fit` Green/Yellow/Red) |
| `configweb` | On-demand in-process web profile/backend editor (HTMX/Alpine) |

### 2.3 State & on-disk layout

```
~/.config/model-loader/
  config.toml                       # app config (TOML, Viper)
  profiles/<id>.json                # one file per profile (id == basename); .history/<id>.previous.json backups; .<id>.lock
  backends/catalog.json             # registered backends
  backends/schemas/<id>.json        # per-backend validation schema
~/.local/state/model-loader/
  model-loader.lock                 # single-instance flock target
  instances.json / instances-history.json   # running / exited registries
  proxy-state.json                  # proxy supervisor state
  logs/<profile-id>-<port>.log      # merged backend stdout+stderr
  metrics/<profileID>/*.jsonl       # metrics time-series
  benchmark/runs/*.json (+ .transcript.json sidecars)
  downloads/dl-<id>.json            # per-download worker state
```

### 2.4 Service routes (HTTP proxy — only client channel to backends)

Default bind `127.0.0.1:4321`, **loopback-only / no auth**, 180 s health-check wait, 8 MiB body buffer, 10 s shutdown grace. Swaps + kills serialize on `swapMu`; in-flight gauge via `atomic`; drain waits on a `serving` counter that covers only the backend-use phase (catch-all proxy pass + the anthropic/gemini/responses upstream round-trips), so requests parked at `swapMu` never stall `/_admin/unload`; admin endpoints and `count_tokens` participate in neither. `Status` JSON tags snake_case (supervisor decodes them; PascalCase `UnmarshalJSON` fallback keeps old states readable). Errors OpenAI-enveloped, except two Anthropic routes which use `{"type":"error","error":{...}}`.

| Method | Path | Purpose |
|--------|------|---------|
| `*` | `/` (catch-all) | Reverse-proxied to loaded backend; `"model"` in body (or `?model=`) triggers **implicit swap** |
| `POST` | `/v1/messages` | **Anthropic Messages API** → backend OpenAI `/v1/chat/completions` (`anthropic_*.go`): full SSE, tools, images, system; `reasoning_content` ↔ `thinking` blocks; **reasoning controls** (`thinking`/`output_config.effort` → canonical pivot → `reasoning_effort` + `chat_template_kwargs.enable_thinking`); `metadata.user_id`→`user`; tool-schema `properties:{}` normalization; inline `{"role":"system"}`→`<system-reminder>` user turns. Strict model resolution (unknown → 404 `not_found_error`); same swap+inflight as catch-all. Never routed thru `loaded.proxy` (its `ModifyResponse` destroys reasoning mapping) |
| `POST` | `/v1/messages/count_tokens` | **Local tiktoken count** over translated request (`tiktoken-go`, o200k_base) + flat per-image cost; validates profile exists but never launches/contacts backend nor touches inflight |
| `POST` | `/v1/responses` | **OpenAI Responses API** translated to backend chat completions (`responses_*.go`): input string/array, `instructions`→system, function_call/function_call_output, `reasoning.effort`, full Responses event-stream. Same swap + inflight semantics as `/v1/messages` |
| `*` | `/v1beta/models` · `/v1beta/models/{model}:{generateContent\|streamGenerateContent\|countTokens}` | **Gemini API** translated to chat completions (`gemini_*.go`): contents/parts, systemInstruction, functionCall/functionResponse (FIFO id pairing), thinkingConfig→reasoning, `data:`-only SSE stream; `{model}` may carry `(level)` reasoning suffix. `GET /v1beta/models` lists profiles in Gemini shape |
| `GET` | `/v1/models` | Registered profiles as **OpenRouter-shaped** objects (mirrors `/api/v1/models`; N/A fields empty) **plus** OpenAI `object:"model"` + `owned_by` (profile's `launch.backendId`). Image modality from mmproj flag **or** VL name marker (flag-less vLLM/SGLang VL models report `text+image->text`). Built from `profilestore`; `{object:"list"}` |
| `GET` | `/_status` | `running, loaded_profile_id, loaded_pid, loaded_port, inflight_requests, last_swap_at, last_swap_dur, last_error` |
| `POST` | `/_admin/load` | Explicit load; body `{"profile_id":"…"}` (alias `{"model":"…"}`) |
| `POST` | `/_admin/unload` | Kill backend / free VRAM; `?force=true&drain_timeout=10s`; idempotent 200 |

vLLM/SGLang should set `served-model-name` to profile id so model-name validation accepts proxied id.

### 2.5 Backend-by-backend (`catalog.json`; 16 registered / 8 kinds; **no default** — resolver falls back to `default_backend_id` (unset) then llama-server PATH lookup)

Profiles select engine via `launch.backendId`; `processmgr/args.go::BuildArgsForBackend` dispatches on `BackendKind`. Per-kind variants (stable/nightly/unlimited/dflash) each resolve to different `backends/<id>/…` checkout. Sibling `*help` packages hold per-kind embedded schema (`.claude/commands/backend-schema-update.md` = 3-pattern update workflow: A live-`--help`+overlay, B curated-Go, C embedded-rows).

| Kind | Catalog id(s) | Arg shape / notes |
|---|---|---|
| `llama-server` | `llama.cpp-stable`, `llama.cpp-nightly` | `--model` + canonicalized flags (`ngl`→`n-gpu-layers`); schema live-parsed, falls back to embedded **v9761**; dual-GPU via `split-mode`/`tensor-split`/`main-gpu` args. Stable vs nightly point at separate `backends/llama.cpp-*/build/bin/llama-server` trees |
| `beellama-cpp` | `beellama-rtx3090` | llama.cpp fork (`backends/beellama.cpp/bin/llama-server`); curated overlay (kv-unified, spec-draft-hf); RTX-3090 DFlash/MTP + tensor-split profiles |
| `dflash` | `lucebox-dflash` | native C++ speculative-decoding server (`backends/lucebox-hub/server/build-rtx3090/dflash_server`, sm_86-real); positional model + `--draft`; see `build-rtx3090.sh` |
| `vllm` | `vllm-stable`, `vllm-nightly`, `sndr-vllm`, `vllm-dflash`, `vllm-dspark` | positional model, `--max-model-len`; `PYTHONUNBUFFERED=1` injected; dual-GPU via `tensor-parallel`/`gpu-split`; stable vs nightly = separate `backends/vllm-*/vllm-serve.sh` checkouts. `sndr-vllm` = SNDR/Genesis runtime patch overlay (TurboQuant k8v4 KV, MTP K=5, GDN attention) on **pinned** vLLM nightly (`dev424`) in own `backends/sndr-vllm/` venv (`scripts/setup-sndr-backend.sh`, runbook `docs/sndr-backend.md`, the `-sndr` profiles). Rules: install pin from its **per-commit** wheel URL (`SNDR_WHEEL_INDEX`; rotating nightly ages out — [BUGS.md N1](BUGS.md)); every profile `launch.env` **must** carry `GENESIS_ENFORCE_VERSION_RANGE=1` ([N2](BUGS.md)); never install plugin into stock vllm venvs (entry point auto-patches every vLLM process of env). `vllm-dflash` = **pinned** vLLM PR#40898 (DFlash drafter + interleaved-SWA) precompiled build in `backends/vllm-dflash/`; `vllm-dspark` = vLLM **git main** precompiled build in `backends/vllm-dspark/` carrying both DSpark (`method:dspark`, `qwen3_dspark`/DeepSeek-V4) and DFlash spec methods + the hybrid/fp8 KV page-size fix — serve wrappers pin gcc-15 for CUDA-13.3 flashinfer JIT (see [BUGS.md DF3/DF4/DF8/DF9](BUGS.md)) |
| `sglang` | `sglang-stable`, `sglang-nightly`, `sglang-unlimited`, `sglang-dflash` | `python -m sglang.launch_server`; bundles CUDA 12.8 for flashinfer JIT; 4 variants (stable/nightly/unlimited-context/dflash) under `backends/sglang-*/sglang-serve.sh` |
| `unsloth` | `unsloth-rtx3090` | `unsloth studio run … -H 127.0.0.1`; captures `sk-unsloth-…` auth token post-load |
| `tabby` | `tabby` | TabbyAPI `main.py` (EXL2/EXL3, ExLlamaV2/V3); model **dir** → `--model-dir`/`--model-name`; wrapper forces `--host 127.0.0.1 --disable-auth true`; bools emit `--flag true`; `gpu-split`/`autosplit-reserve`/`draft-gpu-split` are nargs+ (whitespace-delimited per element → spans 2 cards); auto-detects exl2 vs exl3; `PYTHONUNBUFFERED=1` injected. Embedded schema: `tabbyhelp` (Pattern C) |
| `buun-llama-cpp` | `buun-rtx3090` | buun llama.cpp fork (`backends/buun-llama-cpp/build/bin/llama-server`); embedded schema `buunhelp` |

`backends/*` = vendored/cloned source trees (gitignored — see §8). Python backends (vLLM/SGLang/Unsloth) get **`PYTHONUNBUFFERED=1` at spawn** (`processmgr/launch.go`) — without it their stdout block-buffers, Server-tab log lands in late bursts. `monitor` log tailer intentionally **backend-agnostic**; unbuffering = spawn-time fix, never consumer code path.

### 2.6 Lifecycles

Dispatch: `main.go` (no args→`runTUI` / `serve` / `benchmark`) → `app.Bootstrap` → `config.Load` → `processmgr.Reconcile` (reads `instances.json`). Request path: OpenAI request → `httpproxy` →`ensureLoaded`→ `processmgr.Launch` → backend process (reverse-proxied).

- **Process launch:** `prepareLaunch` allocates ephemeral loopback port (`127.0.0.1:0`), injects it under `port` key (profile's own `port` reserved/stripped), resolves exe+kind (caller-preset, else `m.resolver(p)`), spawns detached (`Setsid`) w/ merged log fd, registers in `instances.json`. `cmd.Wait` reaper enriches exit info (code/signal/reason + 50-line stderr tail); the reaper goroutine starts only **after** the registry upsert commits, so a fast-crash's `Crashed` delta can never be clobbered by the launch's running delta. Sentinels: `ErrModelNotFound, ErrForegroundBusy, ErrUnknownPID, ErrHealthCheckTimeout, ErrBinaryNotFound, ErrReadyTimeout`.
- **Readiness:** `WaitReady` polls `GET /health` w/ 100 ms→1 s capped backoff (unsloth variant also captures auth token from log).
- **Liveness & recovery:** 5 s ticker (identity-aware `procutil.SameProcess` = alive + `/proc` starttime match, so recycled PIDs no longer mask a dead backend) marks dead PIDs `Crashed`, rewrites registry **outside `m.mu`** via flock-guarded delta (**6 documented out-of-`m.mu` `mutateRegistry` callsites — Launch, launchForeground, Kill, liveness, waitEnrichment, Reconcile — each a delta; count is contract**); adopted (reaper-less) deaths get their restart policy applied here; long-lived goroutines install `defer recover()` (outside Bubble Tea's net). Boot `Reconcile` validates `instances.json` by identity: `RunningInstance.StartTicks` (`/proc/<pid>/stat` field 22 captured at launch) when present, else legacy `/proc/<pid>/comm`+cmdline + per-`BackendKind` cmdline token (keeps live wrapper-exec vLLM/SGLang across the first post-upgrade reconcile) — drops recycled PIDs.
- **Restart engine** (`restart.go::maybeScheduleRestart`, replaces the deleted dead Watchdog): policy `none`/`on-failure`/`always` (an unknown/signal death counts as failure for `on-failure`); a **kill-intent guard** (`killRequested` set under `m.mu` before signaling) means an operator Kill / unload / swap never resurrects the backend; the restart count is carried across the death→relaunch boundary via `pendingRestarts` (so `MaxRestarts` actually bounds a crash loop) and **reset to 0 on the first healthy `/health`** (so `MaxRestarts` = max *consecutive failed* starts); backoff = `BackoffSeconds × count` floored at 1 s (no hot loop) capped 30 s; a set-once `restartScheduled` guard prevents reaper/liveness double-fire. Reaper runs it inline; liveness runs it in a goroutine.
- **Kill** signals the **process group** (`procutil.TerminateTree` — Setsid launches lead their group, so vLLM `EngineCore`/`Worker_TP` + SGLang trees are swept, not just the leader) after an identity check (a recycled PID is never signaled). `Launch` rolls back — kills the live child and returns the zero instance — if the post-`Start` registry write fails, so a disk-full/permission error never leaks an unregistered backend.
- **Model swap:** `httpproxy.ensureLoaded` (under `swapMu`) — same target **and still alive** (`procutil.SameProcess` on the loaded backend) → no-op fast path; a crashed loaded backend is detected here and relaunched (proxy recovers instead of returning a permanent 502); different target → kill old + launch new + `WaitReady` + record swap metrics; concurrent same-target callers collapse to one launch. Client cancellation before a swap is a 499 `request_canceled`, not a 504.
- **Monitoring:** `monitor.Subscribe` runs 6 goroutines per backend (log tailer via fsnotify, slots/GPU pollers, metrics aggregator, pumps + ring buffer) over a 256-buffered channel that **drops on backpressure**. TUI Server tab = **passive observer** — `pm.RefreshFromDisk` (read-only), never `Reconcile`.
- **Proxy supervision:** `proxysupervisor` spawns proxy as own detached `serve` process; identity-checked (`State.StartTicks`), so `Start` verifies its own child answers the port (kills the dead-duplicate-spawn bug) and `Status` applies 3-strike hysteresis before dropping state on a transient probe timeout (probe runs outside the mutex, shared client). Exposes `EnsureRunning`/`Load`/`Unload`/`Stop` (45 s grace + post-kill backend sweep) and `ForceStop` (SIGKILL escape hatch for a wedged proxy; `ErrProxyDegraded` lets CLI `instance stop --force` and the TUI force-confirm reach it).

### 2.7 Key cross-subsystem edges
`httpproxy → processmgr + profilestore` · `proxysupervisor → httpproxy` · `backendcatalog ← processmgr` (resolver) · `backendschema → backendcatalog` · `migration → catalog+schema+profilestore` · `benchmark → httpproxy + monitor + profilestore` · `configweb → validator + catalog + profilestore` · `monitor → processmgr (pid+logPath) + backend port` · `profilestore ↔ processmgr` (`LastUsedSink.MarkLastUsed` on first `/health` 200). `downloadmgr` imports no other service (uses `procutil` for spawn lifecycle).

---

## 3. TUI — the 5 Tabs

`RootModel` (`internal/ui/root.go`) = value-typed hub-and-spoke router over fixed `[5]tea.Model`. Cross-tab nav message-based (`LaunchProfileMsg`, `SwitchToServerMsg`, `NavigateToSizingMsg`, `TabAttentionMsg`).

**Global chrome & feedback surfaces:** scrollable tab bar (active tab bracketed, attention badges) · status bar (`[1-5] tabs  [tab] next  [q] quit  [?] help` + per-page `Hints()` + restart badge) · glamour help overlay (`?`/`esc` toggle, `j/k/g/G` scroll) · tagged auto-clearing flash queue · centered modal/overlay compositor. Theme = GitHub-Primer adaptive palette w/ `NO_COLOR` stripping (repo's only `init()`). Layout auto-adapts (stacked below 100 cols, truncate-at-edge, “terminal too small” below 20×6).

**Conventions:** every printable-rune shortcut gated by `!activePageCapturesInput()` (only `ctrl+c` unconditional); capturing pages implement `IsCapturingInput()`; pages hosting `*huh.Form` forward non-`KeyMsg` messages so focus/validation completes. **lowercase = light/navigation (`k/j/g/G` reserved for lists); UPPERCASE = destructive, always confirm** (`X` delete, `K` kill/unload, `R` refresh, `I` import, `E` export, `D` default, `P` probe, `C` clear).

### Tab 1 — Profiles
Master-detail (`bubbles/list`, ~1/3 ↔ 2/3, `│` divider). Pinned rows (📌) sort first then by `UpdatedAt`; corrupt profiles render ⚠ in `theme.Error`. `enter` launch (validate → resolver → proxy ensure-running → load) · `e` edit · `n` new (both open web editor) · `d` duplicate · `X` delete · `K` unload current model · `R` refresh · `p` pin · `I` import (filepicker + merge/overwrite/rename modal) · `u` undo (field-diff modal) · `E` export bundle · `/` filter.

### Tab 2 — Server (merged Monitor + Server)
**Top: ProxyPanel** — 1 Hz status widget (`● RUNNING | addr`, loaded profile, last-swap line, `LastError`, pending/flash) that **consumes `s`/`x`** (proxy start/stop) before page sees them. **Middle:** `bubbles/table` of instances (PID/Port/Profile/Uptime/VRAM/Tok·s). **Bottom:** sub-views **Logs | Slots | Metrics | History** cycled by `v`. `K` kill (routes thru `/_admin/unload` when proxy owns PID, else `pm.Kill`; **refuses to kill degraded proxy**) · `R` restart · `h` history chart (`1/2/3/4` = 1h/6h/24h/7d) · `Space` pause logs (2000-line FIFO tail). Metrics shows tok/s + req/s sparklines + VRAM row.

### Tab 3 — Models
Three sections (**Library / Downloads / Discover**) cycled by `←/→`. Library = async-scanned table (Name/Size/Quant/Params/Path) w/ per-path scan status; `i` info panel (file/path/size/mtime/metadata/used-by), `enter` action menu (use in new/existing profile, copy path, delete), `R` rescan, `X` remove broken search path, `→`/`g` jump to sizing, `/` filter. Discover = debounced HF Hub search (`ctrl+g` GGUF-only) → file picker → queued download (path chooser when >1 search path). Downloads = EMA speed/ETA rows (`c` hide-done, `C` clear-done, `x` cancel, `r` resume).

### Tab 4 — Backends
Master-detail list (Name+ID) + detail (ID/Kind/Executable/SchemaRef/Tags/Created/Updated/Probe). `n` new · `e`/`enter` edit (web editor or legacy `huh` form) · `X` delete · `D` set default · `R` refresh schema (confirm — **wipes manual edits unless `Source.Editable`**) · `P` probe all (3 s/backend) · `/` filter.

### Tab 5 — Benchmark
State machine (`benchmark.go::benchView`): **Dashboard** (landing) **→ Wizard → Running → RunDetail**, plus **Compare**/**History** side views.

- **Dashboard** (`benchmark_dashboard.go`): mode-focused leaderboard — summary strip, mode-focus bar, ranked rows (latest *complete* run per profile, `MetricBar` + Δ vs previous; partial `Err` runs skipped) + insight panel (trend sparkline + Δ). `b` wizard · `enter` detail · `c` compare · `h` history · `X` delete · `E` export · `R` reload · `↑↓/k/j/g/G` cursor · `←/→/[/]` focus mode.
- **Wizard** (`benchmark_wizard.go`): unified 3-step launcher — profile (filterable) → mode (cards grouped by category) → review. `enter` advances, `esc` back-navs (keeps selection); `/` filters profiles.
- **Running** (`benchmark_run.go`): phase label + progress bar (`components.MetricBar`) + current problem name. `esc` arms cancel-confirm modal (stray esc doesn't abort long run). Progress streams over 32-buffered channel.
- **RunDetail**: scorecards (primary metric, tok/s, TTFT inverted, VRAM) + mode-specific breakdown lines (math difficulty, code pass rate, instruction format/refusal/consistency, MMLU category, RAG faithfulness/relevancy/precision, summary coherence, agentic accuracy). `E` export · `esc` back.
- **Compare** (`benchmark_compare.go`): latest run per profile, grouped by mode. `m` cycles ranking metric across 4 (mode primary · tok/s · TTFT-lower · VRAM-lower). `esc` back.
- **History** (`benchmark_compare.go::keyHistory`): runs of one profile over time w/ timeline + sparkline. `↑↓/k/j/g/G` cursor · `enter` open detail · `m` toggle sparkline metric (primary · tok/s). `esc` back.

**Primary metric** (`benchmark_metrics.go::primaryMetric`): every mode except `llama-bench` (tok/s) and `longctx` (recall/`AvgScore`) reports **solve rate** as headline metric. One derivation feeds dashboard ranking, scorecard, compare default, history trend.

### Web profile/backend editor (`configweb`)
Create/edit opens **on-demand, in-process** HTTP server on `127.0.0.1:0`, launches browser, collapses page to "Editing in browser…" frame (only `esc` = cancel passes thru). Form = **schema-driven** from `BackendValidationSchema.Flags` + editable `Presentation` (Essentials/Advanced/Environment/Sizing). **Customize mode** edits schema itself, sets `Source.Editable = true` so `RefreshSchema` preserves manual edits across `--help` re-parsing. Model-existence errors downgraded to warnings (configure-now / download-later flow). Curated highlights live in `essentialSeed` (`backendschema/presentation.go`).

---

## 4. CLI Handbook

`model-loader` (no subcommand) → TUI. Persistent flags everywhere: `--log-level` (also `$MODEL_LOADER_LOG_LEVEL` / config), `--json`. Cobra provides `--version` (default `dev`) and `-h/--help`. Reference resolution uniform: **exact id → exact name → unique prefix** (ambiguous → error listing ≤10 candidates). Exit codes: `0` ok · `1` generic/lookup/IO · `2` `profile validate` blocking errors and `benchmark --min-solve` gate. **Only `instance start/stop/restart` and `benchmark` take the single-instance flock** (`bootstrapWithLock`); profile commands rely on per-profile flocks + the `instances.json` file lock, and `serve` holds no flock and coexists with the TUI (registry writes are flock-guarded deltas, so they no longer collide). Only state owners (TUI, `serve`, via `app.AsStateOwner()`) reconcile the registry and rotate the app log; one-shot CLI commands are observers.

| Subcommand | Purpose | Key flags |
|------------|---------|-----------|
| `serve` | Headless OpenAI-shaped proxy (foreground) | `--host`, `--port` |
| `import <path>` *(deprecated → `profile import`)* | Back-compat bundle import | `--mode merge\|overwrite\|rename` |
| `download <state-path>` *(hidden worker)* | Internal download worker (spawned by `downloadmgr`) | — (`DisableFlagParsing`) |
| `profile list` | List profiles | `--json` |
| `profile show <id\|name>` | Show profile / canonical JSON | `--json` |
| `profile create` | Create from flags and/or JSON | `-f/--file` (`-`=stdin), `--name`, `--model`, `--backend`, `--description`, `--arg k=v`*, `--extra-arg`*, `--env K=V`*, `--id` |
| `profile edit <id\|name>` | Overlay flags/JSON onto profile | same as create (no `--id`) |
| `profile delete <id\|name>` | Delete (writes `.history` backup) | — |
| `profile duplicate <id\|name> [newid]` | Duplicate (default `<id>-copy`) | — |
| `profile rename <id\|name> <newname>` | Rename display name | — |
| `profile pin` / `profile unpin <id\|name>` | Toggle pinned | — |
| `profile export [id…]` | Export JSON bundle (all if none) | `-o/--output` (default stdout) |
| `profile import <path>` | Import bundle | `--mode merge\|overwrite\|rename` |
| `profile validate <id\|name>` | Run validator (exit 0/1/2) | `--json` |
| `instance list` | Running instances | `-w/--watch`, `--interval` (2s) |
| `instance show <pid\|id>` | One instance | — |
| `instance history` | Exited instances | — |
| `instance start <profile>` | Load via proxy (5-min health wait) | `--json` |
| `instance stop <pid\|id>` | Unload (if proxy-owned) else kill | `--json`, `--force` (force-stop a degraded/wedged proxy, then kill directly) |
| `instance restart <pid\|id>` | Unload+reload / kill+reload | `--json` |
| `instance logs <pid\|id>` | Print captured log | `-f/--follow` |
| `instance metrics <pid\|id>` | Recent metrics window | `-w/--watch`, `--interval` |
| `model list` | Scan `models.search_paths` for `.gguf` | `--path`* (override) |
| `model search <query>` | HF Hub search | `--limit` (20) |
| `model info <repo-id>` | HF repo metadata + files | — |
| `model download <repo> <file>` | Download one HF file | `--snapshot`, `--wait`, `--json` |
| `model downloads [cancel\|resume <id>]` | List / control downloads | `--json` |
| `backend list` / `show <id>` | Catalog backends / metadata | `--json` |
| `backend add <name>` | Register backend | `--executable` (req), `--kind` (req) |
| `backend delete <id>` / `set-default <id>` | Remove / mark default | — |
| `backend probe [id]` | Health-check binaries (5 s) | `--json` |
| `backend schema show <id>` | Show schema summary/JSON | `--json` |
| `backend schema refresh <id>` | Re-generate from `--help` | — |
| `backend schema apply <id> -f <file>` | Apply hand-edited schema (sets `Source.Editable`) | `-f/--file` (req) |
| `benchmark` | Run / list / compare / inspect | `--profile`, `--mode <judge\|math-bench\|codegen-bench\|ragas-bench\|summary-bench\|llama-bench\|longctx\|instruction-bench\|mmlu-bench\|terminal-bench\|swe-bench-pro\|deep-swe>` (aliases `long-context`→longctx, `llamabench`/`throughput`→llama-bench), `--list`, `--compare`, `--transcript <run-id>`, `--min-solve <0..1>`, `--limit` (cap items per reducible mode; 0→full), `--tb-task`/`--tb-n-tasks`, `--sweap-{harness,patches,instance}`, `--deepswe-{task,n-tasks,tasks}`, `--json`. Categories Quality/Speed/Robustness/Knowledge/**Agentic**; 3 agentic modes shell out to external harnesses (`tb`, SWE-bench_Pro-os, `pier`) + Docker. No `--dashboard`/`--leaderboard` (TUI-only). `BenchmarkConfig` (`internal/app/benchmark_config.go`) = single source of truth for TUI + CLI |

`*` = repeatable.

---

## 5. Model Cards & Profile Schema

Each profile = one JSON file (`docs/profile-schema.json` canonical, **`schemaVersion: 3`**, `additionalProperties:false`) under `~/.config/model-loader/profiles/`, **basename == `id`**.

| Field | Type | Notes |
|-------|------|-------|
| `schemaVersion` | int | always `3` |
| `id` | string | `^[a-z0-9]+([._-][a-z0-9]+)*$`; **equals filename**; auto-`Slugify`d from name |
| `name` / `description` | string | first sentence + headline metrics should track `args` |
| `tags` | []string | optional |
| `model` | string | absolute path to weights (or repo dir for Python backends) |
| `args` | map | typed flags (`ctx-size`, `max-model-len`, `n-gpu-layers`, `mmproj`, `spec-type`, `n-cpu-moe`, …); `port` reserved and stripped |
| `extraArgs` | []string | raw passthrough flags |
| `launch` | object | `backendId`, `env[]`, `defaultBackground`, `restart_policy`, `max_restarts`, `backoff_seconds` |
| `meta` | object | `createdAt` / `updatedAt` / `lastUsedAt` |
| `pinned` | bool | sort-first in Profiles list |

**FlagSpec types** (schema `flags` map): `Long`, `Short`, `Aliases`, `Type` (`0`=bool `1`=int `2`=float `3`=string `4`=enum), `EnumValues`, `Default`, `HelpText`, `Group`.

**Discovery / search / download:** `modelscanner` walks `[models].search_paths` (default `~/.lmstudio/models`, `~/models`; missing paths skipped) reading GGUF magic/header/KV (quant + params regex). In-TUI `/` filter = case-fold contains over Name/Quant/Params; HF Hub search (`hfhub`) debounced w/ `ctrl+g` GGUF-only toggle, honors `Retry-After`. HF download (`downloadmgr`, max 3 concurrent) spawns a detached worker that Range-resumes from `.partial`; `--snapshot` lays files under `{publisher}/{repo}/`; `--wait` blocks to a terminal state.

### Profile naming convention (curation discipline — *not* code-enforced)
Profile IDs are lowercase kebab/slash-safe slugs and the filename basename equals `id`.

Canonical pattern: `<model-family-and-version>-<size>[-<variant>][-<quant>][-<capability/backend/mode>...]-<ctx>`.

Required minimum:
- The **initial segment names the model**: start with the real base model/family + version when available (`qwen3.6-27b...`, `gemma-4-e4b...`, `agents-a1-35b-a3b...`). Do not start with backend, task, quant, or local nickname unless that is the model family itself.
- The **final segment is the context window**: `-<ctx>` at the very end, sourced from `args.ctx-size` (llama.cpp), `args.max-model-len` (vLLM), or `args.context-length` (SGLang). Use binary-k floored (`262144`→`256k`, `233472`→`228k`, `1048576`→`1024k`). No suffix may appear after the context label.
- **Capability segments mirror `args`, not intent:** `vision` only while `args.mmproj` or a vision-native backend config is active; `mtp`/`dflash` only while speculative args/config are active; backend/mode qualifiers (`vllm`, `tp2`, `beellama`, `textonly`, `tensor`, `layer`) sit before final `<ctx>`.
- Name **real base model** (not publisher's repackaging label). Siblings differing only by serving mode carry a disambiguator before `<ctx>` (`speed`/`throughput`/`quality`, `parallelN-Wk`, `cpumoe`, backend/mode).

---

## 6. Build & Deployment

`Makefile` — only harness, three phony targets:

```make
make build     # go build -o bin/model-loader ./cmd/model-loader
make install   # GOBIN=$HOME/.local/bin go install ./cmd/model-loader
make tests     # go test ./...
```

Also: `go test ./... -update` (regenerate golden fixtures) · `go test ./internal/... -run TestName` (single test) · `go run ./cmd/regenerate-schemas` (rebuild embedded schemas) · `./bin/model-loader --version` (`dev`) / `--help`.

**Dependency map (direct, `go.mod`, Go 1.26.2):** `spf13/cobra v1.10.2` (CLI) · `spf13/viper v1.20.0-alpha.6` (TOML) · `charmbracelet/{bubbletea v1.3.10, bubbles v1.0.0, lipgloss, huh v1.0.0, glamour v1.0.0, x/ansi, x/exp/teatest}` (TUI + tests) · `fsnotify/fsnotify v1.7.0` · `atotto/clipboard v0.1.4` · `mattn/go-runewidth v0.0.19` · `tiktoken-go/tokenizer v0.7.0` (local token estimate). ~50 indirect (chroma, goldmark, termenv, sahilm/fuzzy, bluemonday, `golang.org/x/*`). **No CGO.**

**Required/optional binaries:** `go ≥1.26` (build) · `llama-server` on `$PATH` or registered in catalog (runtime default + schema generator) · `nvidia-smi` (optional GPU metrics) · `vllm`/`sglang`/`dflash`/`unsloth`/`beellama`/`buun` executables (optional, per registered backend).

**No CI/CD** — no `.github/workflows`, `.gitlab-ci.yml`, `.circleci/`, goreleaser, or Dockerfile at repo root (`backends/*/.github/` = vendored upstream); no publish/deploy/release step. Distribution = clone + `make install`. **Entire quality gate = `go test ./...` + `make build`, run locally — no linter, no formatter, no CI.**

---

## 7. Code Conventions

**Service layer:** `*Manager`/`*Store` naming · each package exports own interface (consumers import interface) · `type Config struct{…}` + functional options (`WithLogger`, `WithWaitFunc`) · `log.Nop()` fallback, never `nil` · package-level **error sentinels** (`var ErrNotFound = errors.New(…)`, never dynamic `fmt.Errorf` for sentinel) · **atomic JSON** via `fsx.WriteJSONAtomic` (temp+rename) / `WriteJSONExclusive` (hard-link race-free Create). `processmgr` performs registry writes as **flock-guarded deltas** (`mutateRegistry` under `<path>.lock`) outside `m.mu` — **6 documented callsites (Launch, launchForeground, Kill, liveness, waitEnrichment, Reconcile); count = contract**.

**CLI:** `TUIRunner` callback keeps `internal/cli` from importing `internal/ui` · `&ExitError{Code:N}` for non-1 exits (unwrapped by `Execute()`) · every leaf command calls `app.Bootstrap` and defers `svc.Close()` · output thru `cmd.OutOrStdout()`/`ErrOrStderr()` (never `fmt.Print`) · `--json` honored by all table commands.

**TUI:** global rune shortcuts gated by `activePageCapturesInput()` (only `ctrl+c` unconditional) · editable pages implement `InputCapture.IsCapturingInput()` · pages hosting `*huh.Form` forward non-`KeyMsg` messages · pages implement `Reload()` for on-focus refresh.

**Tests & "sync file" rule:** table-driven, `package <x>` (not `_test`); **no mock framework** — hand-rolled doubles (`stubStore`, `fakeProxy`, `fakeProcMgr`, …), ~190 `_test.go` files. Helpers: `fakeBinary(t)`→`testdata/fake-llama-server.sh`, `freePort(t)`, `newTestManager(t)` (processmgr), `newManager(t)` (backendschema), `drainCmd` (`tea.BatchMsg` pump); `testdata/fake-llama-help.sh` symlinked as `llama-server` for ExecParser tests. **Golden fixtures** (`help-v9761.txt` + `.golden.json`, `//go:embed`) — **never hand-edit; regenerate with `-update`**. `docs/profile-schema.json` **must stay in sync** with `domain.Profile`. `help_test.go::TestHelpMarkdownCoversAllHints` = help↔hints drift detector. `teatest` reserved for root + slow `pages/profiles_test.go`; coverage strong on proxy/supervisor/server-page, thin on bootstrap/`configweb`/benchmark-runner.

**Project / directory style:** per-directory KBs (`AGENTS.md` and/or `CLAUDE.md`, both populated) sit beside code they describe — read sibling for local conventions. **Language:** all UI text and schema metadata (flag/field/JSON/TOML keys, group labels, descriptions) **MUST be English** — no localization.

**Error handling:** When you find any error, record in `BUGS.md` and ask user whether they want it fixed.

**Anti-patterns (NEVER):** call `app.Bootstrap()` twice (FS side-effects) · forget `defer svc.Close()` · import `internal/ui` from `internal/cli` · hand-edit schema's `presentation`/`rules` JSON (use Customize mode) · extend `essentialSeed` without request · run managed backend manually while TUI owns instances · assume cleanup on TUI exit (processes orphaned) · change `domain.Profile`/profilestore JSON without mirroring `docs/profile-schema.json` · intercept global runes without `activePageCapturesInput()` · add a 7th out-of-`m.mu` `mutateRegistry` callsite without updating the contract · pass `app.AsStateOwner()` from a one-shot CLI command (only TUI/`serve` own state) · **asymmetric multi-GPU `tensor-split` / layer-tensor allocation** on the dual-3090 rig (e.g. `0.45,0.55`) — use **`0.5,0.5` only** when llama.cpp `split-mode` layer/tensor spans both cards; spare GPU0 via single-GPU pin or pin-per-GPU two models (`skill://rtx3090-inference-profiles`).

---

## 8. Team / On-call / Repo Notes

- **Maintainer:** `quantmind-br` GitHub org (single maintainer). Benchmark judge reads `$QUANTMIND_API_KEY` (kept as `$ENV`, never written to disk). **No on-call rotation, Slack, `CODEOWNERS`, or `CONTRIBUTING.md`** — route questions thru GitHub repo.
- **Issue tracking:** GitHub Issues at repo URL. **In-repo defect tracker = `BUGS.md`** (single source of truth; L/B/D/T series). Regression tests cite their `BUGS.md` / TUI-audit id.
- **Design docs:** `docs/superpowers/{specs,plans}/` hold PRDs + task-by-task plans.
- **No git submodules.** `backends/*` = **vendored/cloned source trees**, **gitignored** (`.gitignore` L60 `/backends/`) — manage each as own upstream checkout; nothing to sync.
- **Stale docs:** `ARCHITECTURE.md` (auto-generated; cites v9680/244 flags & 24 packages vs current v9761/26 — regenerate via `npx gitnexus analyze`, don't hand-edit). `BUGS.md` predates proxy-translation/benchmark-redesign/dual-GPU (those carry no tracked defects). Everything else in `docs/` is current.

---

## 9. Profile library

Not curated here — the source of truth is `~/.config/model-loader/profiles/*.json`
(one file per profile, `id == basename`). A hand-kept table drifts the moment a
profile is added or removed, so this section intentionally lists nothing.

- **Current set:** `model-loader profile list` (`--json` for full args/env; `profile show <id>` for one).
- **Profile shape:** §5 + `docs/profile-schema.json`. · **Backends & arg notes:** §2.5.

## Bug tracker (full details)

### L-series — Log-streaming audit
#### L1
- **Severity:** L (Low)
- **Status:** ⚪ Won't fix / by design
- **Component:** `internal/service/monitor/logs.go:51-72` (`emit()`)
- **Finding:** `emit()` flushes a trailing partial line before `EOF`, so crash messages are visible. The trade-off of line fragmentation is intentional.

#### L2
- **Severity:** I (Info)
- **Status:** ⚪ By design / non-bug
- **Component:** `internal/service/monitor/logs.go:23-42` (`newLogFollower`)
- **Finding:** The window between `os.Open` and `w.Add` cannot drop content; the initial `emit` covers it.

#### L3
- **Severity:** I (Info)
- **Status:** 🟢 Fixed (documentation)
- **Component:** `internal/service/monitor/metrics.go:53`
- **Finding:** The regex match cost is harmless; documentation corrected.

### B-series — Original TUI bug report (2026-04-29)
#### B1
- **Status:** 🟢 Fixed.
- **Finding:** All input shortcuts now gated behind `activePageCapturesInput()`.

#### B2
- **Status:** ⚪ N/A. The original report flagged this as a non-bug.

#### B3
- **Status:** ⚪ Won't fix / by design.
- **Finding:** The `Launcher` tab was removed; documentation updated.

#### B4
- **Status:** 🟢 Fixed.
- **Finding:** `models_actions.go` adds "Use in existing profile" option.

#### B5
- **Status:** 🟢 Fixed.
- **Finding:** Inline `actionMenu` replaces fragile `huh.Form`.

#### B6
- **Status:** 🟢 Fixed.
- **Finding:** `statusbar.go` now shows `[?] help`.

#### B7
- **Status:** 🟢 Fixed.
- **Finding:** Profiles hint now complete.

#### B8
- **Status:** 🟢 Fixed by design.
- **Finding:** `port` is stripped on duplicate.

#### B9
- **Status:** 🟢 Fixed.
- **Finding:** New `[X]` action removes broken scan paths.

#### B10
- **Status:** 🟢 Fixed.
- **Finding:** Filtering is synchronous; no race.

#### B11
- **Status:** 🟢 Fixed.
- **Finding:** `ProfilesPage.Reload` now called on tab focus.

#### B12
- **Status:** 🟢 Fixed.
- **Finding:** Editor moved to web; no draft leakage.

#### B13
- **Status:** 🟢 Fixed.
- **Finding:** Sub-view cycles on `v` key.

### D-series — Documentation drift
#### D1
- **Status:** 🟢 Fixed.
- **Finding:** `README.md` rewritten for multi-backend.

#### D2
- **Status:** 🟢 Fixed.
- **Finding:** `AGENTS.md` updated to v9761.

#### D3
- **Status:** 🟢 Fixed.
- **Finding:** Schema version documented.

#### D4
- **Status:** 🟢 Fixed.
- **Finding:** "6 goroutines" confirmed correct.

#### D5
- **Status:** 🟢 Fixed.
- **Finding:** Log path documented.

#### D6
- **Status:** 🟢 Fixed.
- **Finding:** Service-layer KB updated.

### T-series — Test-coverage gaps
#### T1
- **Status:** 🟢 Fixed.
- **Finding:** Regression test for partial-line flush added.

#### T2
- **Status:** 🟢 Fixed.
- **Finding:** Backpressure-drop test added.

#### T3
- **Status:** 🟢 Fixed.
- **Finding:** `PYTHONUNBUFFERED=0` override test exists.

#### T4
- **Status:** 🟢 Fixed.
- **Finding:** TUI responsive layout safeguards added.

#### T5
- **Status:** 🟢 Fixed.
- **Component:** `internal/ui/components/{sparkline,filterline,proxy_panel,tab_bar}_test.go`, `internal/ui/pages/models_test.go`, `internal/ui/root_test.go`.
- **Finding:** 11 tests assert the color-on glyph branch (sparkline `▁▂▄▆█`, filter cursor `█`, tab-bar `‹`/`›`, proxy `●`/`○`, badge `●`) but never controlled `NO_COLOR`, so they failed under a harness with `NO_COLOR=1` exported — while their NO_COLOR siblings already set the env explicitly. `theme.NoColor()` is a live `os.Getenv` check, so the color-on tests must guarantee their own precondition. Fixed by adding `t.Setenv("NO_COLOR", "")` at each test's top (hermetic, matching the existing per-test env-control convention); test-only, no product change.

### S-series — Curated schema & validation
#### S1
- **Status:** 🟢 Fixed.
- **Finding:** List-valued enum + extraArgs passthrough; `draft-dflash` added.

#### S2
- **Severity:** M (Medium)
- **Status:** 🟢 Fixed.
- **Component:** `internal/service/backendschema/curated_sglang.go:302-303`; `curated_beellama.go` + `curated_buun.go` `spec-type` row
- **Finding:** (1) sglang `reasoning-parser` (6 values) and `tool-call-parser` (8 values) curated enums lagged the installed sglang 0.5.9 detector maps, hard-failing valid values (e.g. `qwen3_coder`, `qwen3-thinking`, `glm47`) in `args`; `tool-call-parser` HelpText was Portuguese. Enums replaced with the sorted union across the 4 installed sglang venvs (23 reasoning / 30 tool-call values); HelpText set to English; schemas refreshed for all 4 sglang catalog entries. (2) beellama/buun `spec-type` used single-value `enumFlag` (`list:null`) while their binaries accept comma-chained spec-types (`common/arg.cpp` splits `--spec-type` on `,` → `speculative.types` vector, identical to upstream), so a chain like `dflash,ngram-mod` failed `checkEnum` in `args` even though the binary accepts it. Switched both to `listEnumFlag` (matching llama.cpp-stable); schemas refreshed for `beellama-rtx3090` + `buun-rtx3090`.

### N-series — SNDR (Genesis) backend provisioning
#### N1
- **Status:** 🟢 Fixed.
- **Finding:** `SNDR_WHEEL_INDEX` override added.

#### N2
- **Status:** 🟢 Fixed.
- **Finding:** `GENESIS_ENFORCE_VERSION_RANGE=1` added to SNDR profiles.

#### N3
- **Status:** ⚪ By-design VRAM limit.
- **Finding:** TurboQuant continuation-prefill OOM on large prompts; documented.

### V-series — stock vLLM backends
#### V1
- **Status:** 🟢 Fixed.
- **Finding:** Build script + venv rebuilt; vLLM 0.24.0 wheel with flashinfer 0.6.12 installed (no fp8 regression).

#### V2
- **Status:** ⚪ Won't fix / upstream.
- **Finding:** Unlimited-OCR AWQ model incompatibility.

### P-series — Proxy backend-load lifecycle
#### P1
- **Status:** 🟢 Fixed.
- **Finding:** Health-check timeout configurable, default 360s.

#### P2
- **Status:** ❓ Communication limit / will not change.
- **Finding:** With `reasoning-parser: qwen3`, content is empty on truncation; never disable.

#### P3
- **Status:** 🟢 Fixed.
- **Finding:** Liveness check added to `WaitHealthy`/`WaitReady`.

#### P4
- **Severity:** H (High)
- **Status:** 🔴 Open.
- **Component:** `httpproxy` swap path / `processmgr.Kill` (`procutil.TerminateTree`)
- **Finding:** During rapid benchmark-driven implicit swaps (2026-07-09), a swap kill left **3 llama-server processes alive and unregistered** (pids 1023923, 1024387, 1024685; ~30 GB VRAM retained). They were removed from `instances.json`/`instances-history.json` registries but the process group survived `TerminateTree`, starving the next vLLM load into OOM. PIDs have since exited and VRAM is free, but the kill-escape root cause (group escape? double-fork? flock race during rapid swaps) is unconfirmed and unreproduced. Needs investigation before rapid-swap benchmarking is trusted.

### BM-series — Benchmark harness
#### BM1
- **Status:** 🟢 Fixed.
- **Finding:** Prompt sizing adjusted to avoid overflow.

### DL-series — Download manager
#### DL1
- **Status:** 🟢 Fixed.
- **Finding:** `--snapshot` checks file existence.

#### DL2
- **Status:** 🟢 Fixed.
- **Finding:** Queued downloads auto-promoted.

### BF-series — BeeLlama flag limitations / BugTrace profile tuning
#### BF1
- **Status:** ⚪ Won't fix / upstream.
- **Finding:** `llama_params_fit` not implemented for `SPLIT_MODE_TENSOR`.

#### BF2
- **Status:** 🟢 Confirmed (2026-07-09 calibration).
- **Finding:** DFlash slower than tensor-split on BugTraceAI CORE-Ultra 27B SFT Q4_K_S — measured decode: 46.9 tok/s (DFlash layer-split) vs 55.3 tok/s (tensor-split no-DFlash), gap +18%. Layer-split serializes GPU execution per layer; DFlash draft acceptance on this SFT finetune is too low to close the gap. For pure decode speed on this model, tensor-split without DFlash wins.

#### BF3
- **Severity:** H (High)
- **Status:** 🔴 Open (2026-07-09).
- **Component:** BeeLlama `split-mode=tensor` + `spec-type=dflash`
- **Finding:** DFlash speculative decode is structurally incompatible with `split-mode=tensor` on BeeLlama. Crash at `common_speculative_create_ctx_dft`: `pre-allocated tensor (output.weight) in a buffer (Meta()) that cannot run the operation (NONE)`. The DFlash drafter shares the target output tensor, but under tensor-split that tensor lives on a Meta() virtual backend — not CUDA0/CUDA1. With `spec-draft-device: CUDA1` the error changes to: `DFlash draft model uses shared target output tensor on device Meta(), but --spec-draft-device did not create a compatible backend`. The tensor-split ggml backend can't expose the output tensor on a real device that the DFlash drafter can share. Workaround: use `split-mode=layer` with DFlash (layer-split assigns whole layers to real CUDA backends). Upstream fix would require either DFlash to accept a Meta()-backed output tensor, or tensor-split to materialize the output tensor on a real device before the DFlash context is created.
### DF-series — DFlash profile field defects (2026-07-09)
#### DF10
- **Severity:** M (Medium)
- **Status:** 🟢 Fixed (profile retune).
- **Component:** profile `qwen3.6-27b-int4-autoround-dflash-vllm-tp2-textonly-228k`
- **Finding:** Shipped `gpu-memory-utilization: 0.82` OOMs on the **first inference** whenever ~400 MiB of desktop processes sit on GPU0 (load succeeds; the DFlash decode workspace allocation fails) — the profile was unusable as-is on the desktop-shared rig. Retuned to 0.72 and verified: 512 tok @ 87.9 tok/s decode via direct backend port. Profile description updated with the constraint.

### UIUX-series — UI/UX audit (2026-07-07)
#### UIUX-001
- **Severity:** H (High)
- **Status:** 🟢 Fixed.
- **Component:** `internal/service/configweb/backend.gohtml` + `backend_handlers.go`
- **Finding:** The backend editor had no inline field validation — errors only appeared as a footer line. `handleBackendValidate`/`handleBackendSave` now build a `validator.Report` via `backendDraftReport` and render it through the shared `renderIssues` (which emits `data-field`-tagged `.issue.error` divs); `backend.gohtml` fields carry `data-field` + a `<small class="field-error">` slot, decorated by `editor.js`. Save is blocked when the draft has errors.
#### UIUX-002
- **Severity:** H (High)
- **Status:** 🟢 Fixed.
- **Component:** `internal/service/configweb/customize.gohtml` + `handlers_customize.go` + `viewmodel.go`
- **Finding:** Customize "Add flag" returned an empty 200 that htmx swapped into the form, so the flags grid never updated ("reload to edit it"). The Existing-flags section is now a named `customize-flags` sub-template with `id="customize-flags"`; the add form targets it (`hx-target="#customize-flags" hx-swap="outerHTML"`) and `handleCustomizeAddFlag` re-renders the partial (via the extracted `allFlagEdits` helper) after saving.
#### UIUX-003
- **Severity:** M (Medium)
- **Status:** 🟢 Fixed.
- **Component:** `internal/ui/components/confirm.go` + `internal/ui/pages/{profiles,models,backends,benchmark,server}.go`
- **Finding:** Eight hand-rolled confirm-hint strings drifted (Profiles omitted `[esc] cancel`; Models delete omitted `[←→] choose`). Added canonical `components.ConfirmHints`; all confirm-state hints return the constant.
#### UIUX-004
- **Severity:** M (Medium)
- **Status:** 🟢 Fixed.
- **Component:** `internal/ui/components/hf_file_picker.go`
- **Finding:** The HF file picker only moved on arrow keys. Added vim/paging bindings: `k`/`j`, `pgup`/`pgdown` (±10, clamped), `g`/`home` (top), `G`/`end` (bottom).
#### UIUX-005
- **Severity:** M (Medium)
- **Status:** 🟢 Fixed.
- **Component:** `internal/ui/pages/server_subviews.go`
- **Finding:** Server sub-view fallbacks showed implementation jargon (`no subscription`, `after the slots tick`). Replaced with `components.EmptyState("Waiting for monitor data", …)` and a plain metrics-pending line.
#### UIUX-006
- **Severity:** M (Medium)
- **Status:** 🟢 Fixed.
- **Component:** `internal/service/configweb/backend.gohtml` + `assets/static/editor.js`
- **Finding:** The backend editor lacked the profile editor's keyboard shortcuts. Added Ctrl/Cmd+S save + Escape-cancel on `<body>`; the dirty-discard prompt lives in the shared `editor.js` `htmx:confirm` handler keyed on `data-cancel-path`.
#### UIUX-007
- **Severity:** L (Low)
- **Status:** 🟢 Fixed.
- **Component:** `internal/ui/pages/benchmark_wizard.go`
- **Finding:** The 3-step benchmark wizard gave no sense of position. Appended a Subtitle-styled `(1/3)`/`(2/3)`/`(3/3)` indicator to each step title.
#### UIUX-008
- **Severity:** L (Low)
- **Status:** 🟢 Fixed.
- **Component:** `internal/service/configweb/assets/static/app.css`
- **Finding:** The web editor had no light theme and carried color literals outside `:root`. Hoisted every literal onto custom properties (4 new vars) and appended a `@media (prefers-color-scheme: light)` `:root` block (GitHub-Primer light palette, matching the TUI `AdaptiveColor` Light variants).
#### UIUX-009
- **Severity:** L (Low)
- **Status:** ⚪ Kept by decision.
- **Component:** `internal/ui/pages/profiles.go`
- **Finding:** The Profiles status-bar hint truncates its tail into the `[?]` help overlay (F-10 tradeoff). User chose to keep as-is; no code change.
#### UIUX-010
- **Severity:** L (Low)
- **Status:** 🟢 Fixed.
- **Component:** `internal/ui/components/playground_modal.go` + `internal/service/playground/`
- **Finding:** The `PlaygroundModal` component was self-documented dead (DEAD-01, unwired) and the `service/playground` client had no importer. Both deleted; service count 26 → 25 across the KBs.
#### UIUX-011
- **Severity:** M (Medium)
- **Status:** 🟢 Fixed.
- **Component:** `internal/ui/pages/benchmark_run.go` (`handleRunDone`) + `benchmark_compare.go` + `benchmark_update.go` + `benchmark_wizard.go`
- **Finding:** Every error-flash site in the Benchmark page did `p, _ = p.withFlashError(...)`, discarding the auto-clear `tea.Cmd` that `components.Flash` returns. That cmd is the only thing that schedules the `FlashClearMsg` tick (flash.go), so error flashes never expired — a failed run (e.g. proxy load failure) left the red "run failed:" message stuck on screen and stacked across retries (found dogfooding Terminal-Bench). Fixed by threading the flash-clear cmd through all 10 return paths (`return p, fc` / `tea.Batch(fc, p.loadRunsCmd())`); the success path already did this. Regression test asserts the failure path returns a non-nil cmd and the flash clears on its tick.
#### UIUX-012
- **Severity:** H (High)
- **Status:** 🟢 Fixed.
- **Component:** `internal/service/benchmark/terminalbench.go` (`Execute`, `tbHangWatchdog`) + `runner.go` (`Run`, `unloadAfterRun`, `ProxyController`) + `config.go`/`benchmark_config.go` (`unload_after_run`, `terminalbench.stall_timeout_sec`)
- **Finding:** After a Terminal-Bench run scored all tasks (`results.json` complete), the external `tb` harness kept running (observed 15h+) and flooding the proxy with inference requests (~1.6M tasks, 7 persistent connections); because the benchmark leaves the model loaded by design (proxy owns lifecycle), the backend kept decoding → GPUs pinned at 30-50%, with nothing in the TUI revealing the still-live `tb` (progress "N/N complete" comes from polling `results.json`, not from `tb` exiting). Diagnosed live via `nvidia-smi`/`ss`/`ps` + backend `/slots`. Fixes: (1) `tbHangWatchdog` in `Execute` (now `Start`/`Wait`) group-SIGTERMs `tb` (grace for `--cleanup`) then escalates to the run-context SIGKILL when either every task is scored but `tb` won't exit (`tbPostCompleteGrace`, 2m) or no new task is scored for the stall window (`terminalbench.stall_timeout_sec`, default 45m); a kill-after-completion is reported as success (results whole), a stall/whole-run-timeout kill as a partial run (leaderboard-excluded). (2) Progress emits "all tasks scored — waiting for harness to exit…" so a full bar no longer looks done. (3) Opt-in `benchmark.unload_after_run` frees the model via the proxy `/_admin/unload` when a run ends (only a model the run itself loaded — a reused warm backend is left in place), added `Unload` to the runner's `ProxyController`. The 700+ `CLOSE-WAIT` sockets seen on `llama-server` are cpp-httplib not reaping half-closed connections under the flood — a downstream symptom cleared by stopping the flood + unload, not proxy-fixable. The full-fleet audit of this hang class across every benchmark mode is **UIUX-013** (`deep-swe` shared it and got the same shared watchdog; `swe-bench-pro` is narrower).

#### UIUX-013
- **Severity:** M (Medium)
- **Status:** 🟢 Fixed (deep-swe) / evaluated (all modes).
- **Component:** `internal/service/benchmark/` — audit of every mode for the UIUX-012 hang class (lingering/runaway external harness flooding the proxy → pinned GPU).
- **Finding:** Per-mode evaluation:
  - **terminal-bench** — the reported bug; fixed under UIUX-012.
  - **deep-swe** — **structurally identical** to the old tb `Execute` (`cmd.Run()` + a results-polling progress goroutine): a pier run that scored every task but won't exit, or an in-container mini-swe-agent stuck looping, keeps hitting the proxy and pins the GPU, unbounded (`DeepSWETimeout` default 0). **Fixed**: the tb watchdog was extracted into a shared `runHangWatchdog(label, completed, total, pid, stall, …)`; deep-swe now `Start`/`Wait`s pier with the same SIGTERM→SIGKILL completion/stall watchdog (`deepStallTimeout` / `benchmark.deepswe.stall_timeout_sec`, default 45m) and reports a completion-kill as success, a stall/whole-run-timeout kill as a partial.
  - **swe-bench-pro** — narrower exposure. Its `Execute` runs agent/gather/eval as sequential blocking `cmd.Run()`s, so there is **no "scored-but-lingering" gap** (it blocks on the eval process, which does not touch the model — patches are applied + tested in Docker). The only proxy-flood window is the **optional** patch-generation agent step (skipped when `patch_path` is supplied), bounded only by `SweBenchProTimeout` (default 0). Mitigated by the cross-cutting opt-in `benchmark.unload_after_run` + setting `benchmark.swebenchpro.timeout_sec`; a dedicated agent-step stall watchdog was **not** added (the agent command + its preds-dir output layout are operator-configured, so a reliable progress signal isn't guaranteed — a fragile watchdog would risk false kills). Documented rather than force-fixed.
  - **codegen-bench** — spawns `python3`/`bwrap` per candidate, but that runs the **model's generated code**, not the model, so it never touches the proxy/GPU; already bounded by a per-execution `context.WithTimeout` + own process group + `Cancel` SIGKILL. Not exposed.
  - **judge / math-bench / ragas-bench / summary-bench / instruction-bench / mmlu-bench / longctx / llama-bench** — pure in-Go inference loops over the proxy, bounded by a fixed problem/preset count and the run context; no external harness to linger. Not exposed. They share the "model stays loaded after the run" design (proxy owns lifecycle) → mitigated by the opt-in `benchmark.unload_after_run` (UIUX-012), which applies to every mode.

### BR-series — Benchmark reliability audit (2026-07-08)
**BR1–BR8, all 🟢 Fixed** this session (compact table in `BUGS.md`; full evidence + surviving proposals in `BENCHMARK_RELIABILITY.md`; regression tests in `internal/service/benchmark/br_regression_test.go` + `internal/cli/benchmark_test.go`). What changed, and why "better" is *attributable, non-silent, non-cascading failure* — not "every run always succeeds":
  - **BR1** (M) — `ragasbench.go`/`summarybench.go`: a grading-call failure was a silent score 0 blended into the aggregate (only the judge mode set `res.Err`). Now ragas/summary set `res.Err` + `tr.Error`; ragas short-circuits the remaining criteria after the first failure (one dead-judge timeout, not three); a run cancelled mid-grade keeps the inference result without a fabricated per-problem error (mirrors `Runner.scoreProblem`).
  - **BR2** (M) — `runner.go` `allItemsFailed` + `cli/benchmark.go`: an Execute that "succeeded" with every item carrying an `Err` returned nil ⇒ the dashboard treated it as the profile's latest *complete* run and `--min-solve` gated over the (empty) scored subset. Now an all-`Err` set returns a run-level error (leaderboard-excluded), and the gate prints the errored-item count.
  - **BR3** (M) — `terminalbench.go`/`deepswe.go`: `cmd.Wait`'s error was ignored once results parsed, so a harness that died with partial results on disk read as a complete run. Now gated on *completeness* (`total > 0 && scored < total` + non-zero exit → partial), not on harness exit-code semantics; a complete set + non-zero exit stays success with the exit traced into the harness log (matches the watchdog completed-kill-is-success precedent).
  - **BR4** (M) — `codegenbench.go`: missing `python3` was detected in `Execute` (after the expensive model load) and persisted as a fake 1-item run. Moved to `Prepare` (fail-fast, before load).
  - **BR5** (M) — `longcontext_probe.go`: the largest-prompt mode lacked the prefill deadline extension llama-bench has. Applied the same ~100 tok/s prefill floor (`Timeout + targetTokens/100 s`).
  - **BR6** (M) — `llamabench_probe.go`: the first failed rep aborted the whole preset even when other reps had already measured. Now a rep failure degrades to the collected samples (cancel still aborts); the preset errors only when nothing was measured; warmup + rep failures are counted in Detail.
  - **BR7** (M) — new `harness_log.go` + the three agentic executors + `benchmark.Config.HarnessLogDir` (mapped to `<state>/benchmark/harness`): harness stdout/stderr was captured only in an unbounded in-memory `strings.Builder` (RAM growth on hours-long runs; log died with the process). Now teed live to `<mode>-<ts>.log` with a bounded 64 KiB in-memory tail; transcripts + error tails name the file. Owner-boot pruning (`PruneHarnessLogs`, wired in `app/bootstrap.go` beside the AUD-C1 backend-log prune) keeps the 10 newest per mode label so the tee doesn't accumulate without bound.
  - **BR8** (L) — `cli/benchmark.go` `benchModeList`: usage + unknown-mode messages now derive the mode list from `benchmark.ModesInOrder()`.

**Cross-cutting follow-through (same session, `reliability_regression_test.go`):** the report's transversal recommendations **T1/T3/T5/T7** were also implemented (T8 + P2 refinements remain proposals).
  - **T1 (structured logging)** — `benchmark.Config.Logger` + nil-safe `Runner.logger()` (falls back to `log.Nop()`, so struct-literal test Runners never panic). Emits `benchmark_run_start`/`_backend_loaded`/`_run_done`/`_run_partial`, per-item `benchmark_item_failed` (`run_id`/`problem_id`/`phase`), and agentic `benchmark_harness_start`/`_exit`. Logger injected at both call sites (`cmd/model-loader/main.go`, `cli/benchmark.go`).
  - **T3 (scaled per-item deadline)** — `Runner.inferTimeout()` = `Timeout + MaxTokens/decodeFloorTPS` (`decodeFloorTPS=15`) on every model-under-test inference call; grading calls keep the base `Timeout`. Config defaults unchanged — the 120s×32768 mismatch dissolves because the deadline now scales.
  - **T5 (`FailPhase`)** — additive `ProblemResult.FailPhase` (`infer`/`score`/`harness`) set at every `Err` site; makes per-item failure attribution programmatic.
  - **T7 (incremental persistence)** — `RunConfig.Checkpoint`: time-throttled (15s) partial-run save via the shared `executeSerialBench` choke point (the serial in-Go modes: math/mmlu/codegen/ragas/summary/instruction), flagged `Err="in progress"` (reuses the existing partial-skip so the leaderboard ignores it) until the caller's final save overwrites. Wired in CLI + TUI. **judge** (own loop, concurrent scoring — a checkpoint there would race the scorer goroutines) and the **agentic** modes keep end-of-run persistence (judge is a bounded set; agentic artifacts already on disk via BR7).

### Remaining / by-design notes
- **Comments:** No `🟡 Partial` defects remain. All entries are fixed, by-design, or observations.

### Verification
- `go build ./...` → exit 0.
- `go test ./...` → all packages OK.

---

## OpenWiki

This repository has documentation located in the /openwiki directory.

Start here:
- [OpenWiki quickstart](openwiki/quickstart.md)

OpenWiki includes repository overview, architecture notes, workflows, domain concepts, operations, integrations, testing guidance, and source maps.

When working in this repository, read the OpenWiki quickstart first, then follow its links to the relevant architecture, workflow, domain, operation, and testing notes.

<!-- gitnexus:start -->
## GitNexus — Code Intelligence

Indexed as **model-loader** (≈9.9k symbols, ≈31.8k relationships). GitNexus MCP tools assess blast radius & flows (`gitnexus_impact` upstream before editing a symbol, `gitnexus_query`/`gitnexus_context` to explore, `gitnexus_rename` for call-graph-aware renames). If the tools are unavailable or the index is stale, run `npx gitnexus analyze` — else verify blast radius manually. Skills under `.claude/skills/gitnexus/`.
<!-- gitnexus:end --> gitnexus:end -->
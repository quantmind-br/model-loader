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
| Services | `internal/service/*` | **26** self-contained packages, one concern each (§2.2) |
| Logging | `internal/log/` | file-only `slog`, rotate-by-session, `log.Nop()` fallback |
| Leaf utils | `internal/service/internal/{fsx,procutil,ptrutil}` | `WriteJSONAtomic`/`WriteJSONExclusive`/`ReadJSON[T]`; `Alive(pid)`; generic `Ptr[T]` |

### 2.2 Service layer (26 packages)

Each owns one concern, exports own interface, takes `Config` with functional options (`WithLogger`…), falls back to `log.Nop()` not nil logger.

| Service | Purpose |
|---------|---------|
| `processmgr` | Process lifecycle, recovery, history, watchdog, liveness — **largest service** |
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
| `playground` | OpenAI chat SSE client (UI modal currently inert, DEAD-01) |
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

Default bind `127.0.0.1:4321`, **loopback-only / no auth**, 180 s health-check wait, 8 MiB body buffer, 10 s shutdown grace. Swaps + kills serialize on `swapMu`; in-flight tracked via `atomic` + `WaitGroup` — **only catch-all and `/v1/messages` increment `inflightWG`**; admin endpoints and `count_tokens` must not touch it or drain self-deadlocks. `Status` JSON tags snake_case (supervisor decodes them; PascalCase `UnmarshalJSON` fallback keeps old states readable). Errors OpenAI-enveloped, except two Anthropic routes which use `{"type":"error","error":{...}}`.

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

### 2.5 Backend-by-backend (`catalog.json`; 14 registered / 8 kinds; **no default** — resolver falls back to `default_backend_id` (unset) then llama-server PATH lookup)

Profiles select engine via `launch.backendId`; `processmgr/args.go::BuildArgsForBackend` dispatches on `BackendKind`. Per-kind variants (stable/nightly/unlimited/dflash) each resolve to different `backends/<id>/…` checkout. Sibling `*help` packages hold per-kind embedded schema (`.claude/commands/backend-schema-update.md` = 3-pattern update workflow: A live-`--help`+overlay, B curated-Go, C embedded-rows).

| Kind | Catalog id(s) | Arg shape / notes |
|---|---|---|
| `llama-server` | `llama.cpp-stable`, `llama.cpp-nightly` | `--model` + canonicalized flags (`ngl`→`n-gpu-layers`); schema live-parsed, falls back to embedded **v9761**; dual-GPU via `split-mode`/`tensor-split`/`main-gpu` args. Stable vs nightly point at separate `backends/llama.cpp-*/build/bin/llama-server` trees |
| `beellama-cpp` | `beellama-rtx3090` | llama.cpp fork (`backends/beellama.cpp/bin/llama-server`); curated overlay (kv-unified, spec-draft-hf); RTX-3090 DFlash/MTP + tensor-split profiles |
| `dflash` | `lucebox-dflash` | native C++ speculative-decoding server (`backends/lucebox-hub/server/build-rtx3090/dflash_server`, sm_86-real); positional model + `--draft`; see `build-rtx3090.sh` |
| `vllm` | `vllm-stable`, `vllm-nightly`, `sndr-vllm` | positional model, `--max-model-len`; `PYTHONUNBUFFERED=1` injected; dual-GPU via `tensor-parallel`/`gpu-split`; stable vs nightly = separate `backends/vllm-*/vllm-serve.sh` checkouts. `sndr-vllm` = SNDR/Genesis runtime patch overlay (TurboQuant k8v4 KV, MTP K=5, GDN attention) on **pinned** vLLM nightly (`dev424`) in own `backends/sndr-vllm/` venv (`scripts/setup-sndr-backend.sh`, runbook `docs/sndr-backend.md`, 5 `-sndr` profiles §9). Rules: install pin from its **per-commit** wheel URL (`SNDR_WHEEL_INDEX`; rotating nightly ages out — [BUGS.md N1](BUGS.md)); every profile `launch.env` **must** carry `GENESIS_ENFORCE_VERSION_RANGE=1` ([N2](BUGS.md)); never install plugin into stock vllm venvs (entry point auto-patches every vLLM process of env) |
| `sglang` | `sglang-stable`, `sglang-nightly`, `sglang-unlimited`, `sglang-dflash` | `python -m sglang.launch_server`; bundles CUDA 12.8 for flashinfer JIT; 4 variants (stable/nightly/unlimited-context/dflash) under `backends/sglang-*/sglang-serve.sh` |
| `unsloth` | `unsloth-rtx3090` | `unsloth studio run … -H 127.0.0.1`; captures `sk-unsloth-…` auth token post-load |
| `tabby` | `tabby` | TabbyAPI `main.py` (EXL2/EXL3, ExLlamaV2/V3); model **dir** → `--model-dir`/`--model-name`; wrapper forces `--host 127.0.0.1 --disable-auth true`; bools emit `--flag true`; `gpu-split`/`autosplit-reserve`/`draft-gpu-split` are nargs+ (whitespace-delimited per element → spans 2 cards); auto-detects exl2 vs exl3; `PYTHONUNBUFFERED=1` injected. Embedded schema: `tabbyhelp` (Pattern C) |
| `buun-llama-cpp` | `buun-rtx3090` | buun llama.cpp fork (`backends/buun-llama-cpp/build/bin/llama-server`); embedded schema `buunhelp` |

`backends/*` = vendored/cloned source trees (gitignored — see §8). Python backends (vLLM/SGLang/Unsloth) get **`PYTHONUNBUFFERED=1` at spawn** (`processmgr/launch.go`) — without it their stdout block-buffers, Server-tab log lands in late bursts. `monitor` log tailer intentionally **backend-agnostic**; unbuffering = spawn-time fix, never consumer code path.

### 2.6 Lifecycles

Dispatch: `main.go` (no args→`runTUI` / `serve` / `benchmark`) → `app.Bootstrap` → `config.Load` → `processmgr.Reconcile` (reads `instances.json`). Request path: OpenAI request → `httpproxy` →`ensureLoaded`→ `processmgr.Launch` → backend process (reverse-proxied).

- **Process launch:** `prepareLaunch` allocates ephemeral loopback port (`127.0.0.1:0`), injects it under `port` key (profile's own `port` reserved/stripped), resolves exe+kind (caller-preset, else `m.resolver(p)`), spawns detached (`Setsid`) w/ merged log fd, registers in `instances.json`. `cmd.Wait` reaper enriches exit info (code/signal/reason + 50-line stderr tail). Sentinels: `ErrModelNotFound, ErrForegroundBusy, ErrUnknownPID, ErrHealthCheckTimeout, ErrBinaryNotFound, ErrReadyTimeout`.
- **Readiness:** `WaitReady` polls `GET /health` w/ 100 ms→1 s capped backoff (unsloth variant also captures auth token from log).
- **Liveness & recovery:** 5 s ticker (`procutil.Alive`) marks dead PIDs `Crashed`, rewrites registry **outside `m.mu`** (5 documented out-of-lock `saveRegistry` callsites — count is contract); long-lived goroutines install `defer recover()` (outside Bubble Tea's net). Boot `Reconcile` validates `instances.json` vs `/proc/<pid>/comm` + cmdline (handles compound commands like `python -m sglang.launch_server`) and drops recycled PIDs.
- **Watchdog:** policy-driven restart (`none`/`on-failure`/`always`) w/ `BackoffSeconds` × count capped 30 s, `MaxRestarts`; emits `tea.Cmd`s on Bubble Tea loop.
- **Model swap:** `httpproxy.ensureLoaded` (under `swapMu`) — same target → no-op fast path; different → kill old + launch new + `WaitReady` + record swap metrics; concurrent same-target callers collapse to one launch.
- **Monitoring:** `monitor.Subscribe` runs 6 goroutines per backend (log tailer via fsnotify, slots/GPU pollers, metrics aggregator, pumps + ring buffer) over a 256-buffered channel that **drops on backpressure**. TUI Server tab = **passive observer** — `pm.RefreshFromDisk` (read-only), never `Reconcile`.
- **Proxy supervision:** `proxysupervisor` spawns proxy as own detached `serve` process, reconciles `proxy-state.json` (PID + port-open + `/_status` probe), exposes `EnsureRunning`/`Load`/`Unload` to TUI, CLI, benchmark.

### 2.7 Key cross-subsystem edges
`httpproxy → processmgr + profilestore` · `proxysupervisor → httpproxy` · `backendcatalog ← processmgr` (resolver) · `backendschema → backendcatalog` · `migration → catalog+schema+profilestore` · `benchmark → httpproxy + monitor + profilestore` · `configweb → validator + catalog + profilestore` · `monitor → processmgr (pid+logPath) + backend port` · `profilestore ↔ processmgr` (`LastUsedSink.MarkLastUsed` on first `/health` 200). `downloadmgr` imports no other service (uses `procutil` for spawn lifecycle).

---

## 3. TUI — the 5 Tabs

`RootModel` (`internal/ui/root.go`) = value-typed hub-and-spoke router over fixed `[5]tea.Model`. Cross-tab nav message-based (`LaunchProfileMsg`, `SwitchToServerMsg`, `NavigateToSizingMsg`, `TabAttentionMsg`).

**Global chrome & feedback surfaces:** scrollable tab bar (active tab bracketed, attention badges) · status bar (`[1-5] tabs  [tab] next  [q] quit  [?] help` + per-page `Hints()` + restart badge) · glamour help overlay (`?`/`esc` toggle, `j/k/g/G` scroll) · tagged auto-clearing flash queue · centered modal/overlay compositor. Theme = GitHub-Primer adaptive palette w/ `NO_COLOR` stripping (repo's only `init()`).

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

`model-loader` (no subcommand) → TUI. Persistent flags everywhere: `--log-level` (also `$MODEL_LOADER_LOG_LEVEL` / config), `--json`. Cobra provides `--version` (default `dev`) and `-h/--help`. Reference resolution uniform: **exact id → exact name → unique prefix** (ambiguous → error listing ≤10 candidates). Exit codes: `0` ok · `1` generic/lookup/IO · `2` `profile validate` blocking errors and `benchmark --min-solve` gate. Write paths take single-instance flock via `bootstrapWithLock`; read-only commands skip it.

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
| `instance stop <pid\|id>` | Unload (if proxy-owned) else kill | `--json` |
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
Lowercase kebab id, most-significant first: `<family><ver>-<size>[-<variant>][-<quant>][-<capability>…]-<ctx>[-<backend>][-<mode>]`.
- **ctx label must match real context** — source of truth = `args.ctx-size` (llama.cpp) / `args.max-model-len` (vLLM), **binary-k (÷1024) floored** (`262144`→`256k`, `253952`→`248k`, `200000`→`195k`). Stale `…-262k` on 200000-ctx profile = most common drift.
- **Capability segments mirror `args`, not intent:** `vision` only while `args.mmproj` set; `mtp`/`dflash` only while `args.spec-type` set (id/args drift exists — see §9).
- Name **real base model** (not publisher's repackaging label); siblings differing only by serving mode carry disambiguator (`-speed`/`-throughput`/`-quality`, `parallelN-Wk`, `-cpumoe`).

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

**Service layer:** `*Manager`/`*Store` naming · each package exports own interface (consumers import interface) · `type Config struct{…}` + functional options (`WithLogger`, `WithWaitFunc`) · `log.Nop()` fallback, never `nil` · package-level **error sentinels** (`var ErrNotFound = errors.New(…)`, never dynamic `fmt.Errorf` for sentinel) · **atomic JSON** via `fsx.WriteJSONAtomic` (temp+rename) / `WriteJSONExclusive` (hard-link race-free Create). `processmgr` keeps `saveRegistry` outside `m.mu` (5 documented callsites — count = contract).

**CLI:** `TUIRunner` callback keeps `internal/cli` from importing `internal/ui` · `&ExitError{Code:N}` for non-1 exits (unwrapped by `Execute()`) · every leaf command calls `app.Bootstrap` and defers `svc.Close()` · output thru `cmd.OutOrStdout()`/`ErrOrStderr()` (never `fmt.Print`) · `--json` honored by all table commands.

**TUI:** global rune shortcuts gated by `activePageCapturesInput()` (only `ctrl+c` unconditional) · editable pages implement `InputCapture.IsCapturingInput()` · pages hosting `*huh.Form` forward non-`KeyMsg` messages · pages implement `Reload()` for on-focus refresh.

**Tests & "sync file" rule:** table-driven, `package <x>` (not `_test`); **no mock framework** — hand-rolled doubles (`stubStore`, `fakeProxy`, `fakeProcMgr`, …), ~190 `_test.go` files. Helpers: `fakeBinary(t)`→`testdata/fake-llama-server.sh`, `freePort(t)`, `newTestManager(t)` (processmgr), `newManager(t)` (backendschema), `drainCmd` (`tea.BatchMsg` pump); `testdata/fake-llama-help.sh` symlinked as `llama-server` for ExecParser tests. **Golden fixtures** (`help-v9761.txt` + `.golden.json`, `//go:embed`) — **never hand-edit; regenerate with `-update`**. `docs/profile-schema.json` **must stay in sync** with `domain.Profile`. `help_test.go::TestHelpMarkdownCoversAllHints` = help↔hints drift detector. `teatest` reserved for root + slow `pages/profiles_test.go`; coverage strong on proxy/supervisor/server-page, thin on bootstrap/`configweb`/benchmark-runner.

**Project / directory style:** per-directory KBs (`AGENTS.md` and/or `CLAUDE.md`, both populated) sit beside code they describe — read sibling for local conventions. **Language:** all UI text and schema metadata (flag/field/JSON/TOML keys, group labels, descriptions) **MUST be English** — no localization.

**Error handling:** When you find any error, record in `BUGS.md` and ask user whether they want it fixed.

**Anti-patterns (NEVER):** call `app.Bootstrap()` twice (FS side-effects) · forget `defer svc.Close()` · import `internal/ui` from `internal/cli` · hand-edit schema's `presentation`/`rules` JSON (use Customize mode) · extend `essentialSeed` without request · run managed backend manually while TUI owns instances · assume cleanup on TUI exit (processes orphaned) · change `domain.Profile`/profilestore JSON without mirroring `docs/profile-schema.json` · intercept global runes without `activePageCapturesInput()` · add 6th out-of-lock `saveRegistry` callsite without updating contract.

---

## 8. Team / On-call / Repo Notes

- **Maintainer:** `quantmind-br` GitHub org (single maintainer). Benchmark judge reads `$QUANTMIND_API_KEY` (kept as `$ENV`, never written to disk). **No on-call rotation, Slack, `CODEOWNERS`, or `CONTRIBUTING.md`** — route questions thru GitHub repo.
- **Issue tracking:** GitHub Issues at repo URL. **In-repo defect tracker = `BUGS.md`** (single source of truth; L/B/D/T series). Regression tests cite their `BUGS.md` / TUI-audit id.
- **Design docs:** `docs/superpowers/{specs,plans}/` hold PRDs + task-by-task plans.
- **No git submodules.** `backends/*` = **vendored/cloned source trees**, **gitignored** (`.gitignore` L60 `/backends/`) — manage each as own upstream checkout; nothing to sync.
- **Stale docs:** `ARCHITECTURE.md` (auto-generated; cites v9680/244 flags & 24 packages vs current v9761/26 — regenerate via `npx gitnexus analyze`, don't hand-edit). `BUGS.md` predates proxy-translation/benchmark-redesign/dual-GPU (those carry no tracked defects). Everything else in `docs/` is current.

---

## 9. Full Model Table (current profile library — 29 profiles)

`~/.config/model-loader/profiles/*.json` (by `launch.backendId`: `llama.cpp-stable` 12 · `vllm-nightly` 6 · `sndr-vllm` 5 · `beellama-rtx3090` 2 · `lucebox-dflash` 2 · `sglang-unlimited` 1; all `restart_policy: none`). **Caps derived from real `args`:** `vision`=`mmproj`, `spec`=`spec-type`/draft (MTP or DFlash), `cpumoe`=`n-cpu-moe`, `tp`=`tensor-split`/`split-mode`/`tensor-parallel` (dual-GPU), `embed`=embeddings.

| # | Profile id | Backend (kind) | Ctx | Quant | Caps | Model |
|--:|---|---|--:|---|---|---|
| 1 | `gemma-4-12b-agentic-fable5-vision-mtp-256k` | llama.cpp | 262144 | q8_0 | vision+spec | gemma4-v2-Q8_0.gguf |
| 2 | `gemma-4-12b-it-mtp-256k` | llama.cpp | 262144 | q4_k_xl | spec | gemma-4-12B-it-qat-UD-Q4_K_XL.gguf |
| 3 | `joycaption-beta-one-gptq-vllm-8k` | vllm | 8192 | gptq | vision† | llama-joycaption-beta-one-hf-llava-GPTQ-4bit |
| 4 | `ornith-1.0-35b-a3b-q4km-256k` | llama.cpp | 262144 | q4_k_m | — | ornith-1.0-35b-Q4_K_M.gguf |
| 5 | `ornith-1.0-35b-a3b-q4km-tensor-256k` | llama.cpp | 262144 | q4_k_m | tp | ornith-1.0-35b-Q4_K_M.gguf |
| 6 | `ornith-aeon-35b-a3b-q4km-mtp-vision-layer2-256k` | llama.cpp | 262144 | q4_k_m | vision+spec+tp | ornith-aeon-35b-MTP-Q4_K_M.gguf |
| 7 | `qwen3-embedding-0.6b-32k` | llama.cpp | 32768 | f16 | embed | Qwen3-Embedding-0.6B-f16.gguf |
| 8 | `qwen3-vl-8b-awq-vllm-8k` | vllm | 8192 | awq | vision† | Qwen3-VL-8B-Instruct-AWQ-4bit |
| 9 | `qwen3-vl-8b-nsfw-caption-v45-vllm-16k` | vllm | 16384 | — | vision† | Qwen3-VL-8B-NSFW-Caption-V4.5 |
| 10 | `qwen3.6-27b-awq-dflash-vllm-tp2-128k` | vllm | 131072 | awq | spec+tp | Qwen3.6-27B-AWQ-MTP |
| 11 | `qwen3.6-27b-awq-mtp-fp8-vllm-tp2-256k` | vllm | 262144 | fp8 | spec+tp | Qwen3.6-27B-AWQ-MTP |
| 12 | `qwen3.6-27b-heretic-v2-autoround-dflash-vllm-tp2-128k` | vllm | 131072 | — | spec+tp | Qwen3.6-27B-heretic-v2-mtp-int4-AutoRound |
| 13 | `qwen3.6-27b-heretic-v2-autoround-mtp-fp8-vllm-tp2-256k` | vllm | 262144 | — | spec+tp | Qwen3.6-27B-heretic-v2-mtp-int4-AutoRound |
| 14 | `qwen3.6-27b-heretic-v2-mtp-228k-vision` | llama.cpp | 233472 | q4_k_m | vision+spec | Qwen3.6-27B-uncensored-heretic-v2-Native-MTP-… |
| 15 | `qwen3.6-27b-lucebox-dflash-128k-he-turbo` | dflash | — | q4_k_m | spec | Qwen3.6-27B-Q4_K_M.gguf |
| 16 | `qwen3.6-27b-lucebox-dflash-256k-maxperf` | dflash | — | q4_k_m | spec | Qwen3.6-27B-Q4_K_M.gguf |
| 17 | `qwen3.6-27b-neo-code-dflash-layer-256k-vision` | beellama | 262144 | q4_k_m | vision+spec+tp | Qwen3.6-27B-NEO-CODE-HERE-2T-OT-Q4_K_M.gguf |
| 18 | `qwen3.6-35b-a3b-q4km-mtp-192k-vision` | llama.cpp | 196608 | q4_k_m | vision+spec | Qwen3.6-35B-A3B-UD-Q4_K_M.gguf |
| 19 | `qwen3.6-35b-a3b-q4km-mtp-256k-vision-cpumoe` | llama.cpp | 262144 | q4_k_m | vision+spec+cpumoe | Qwen3.6-35B-A3B-UD-Q4_K_M.gguf |
| 20 | `qwen3.6-35b-a3b-q4km-vision-parallel8-8k` | llama.cpp | 65536 | q4_k_m | vision | Qwen3.6-35B-A3B-UD-Q4_K_M.gguf |
| 21 | `qwen3.6-40b-deckard-neo-code-tensor-256k-vision` | llama.cpp | 262144 | q5_k_m | vision+tp | Qwen3.6-40B-Deck-Opus-NEO-CODE-HERE-2T-OT-Q5_K_M |
| 22 | `qwopus3.6-27b-coder-dflash-layer-256k` | beellama | 262144 | q4_k_m | spec+tp | Qwopus3.6-27B-Coder-MTP-Q4_K_M.gguf |
| 23 | `qwythos-9b-mtp-q8-768k` | llama.cpp | 786432 | q8_0 | spec | Qwythos-9B-Claude-Mythos-5-1M-MTP-Q8_0.gguf |
| 24 | `unlimited-ocr-sglang-32k` | sglang | 32768 | — | — | Unlimited-OCR |
| 25 | `qwen3.6-27b-int4-autoround-tq-mtp-sndr-tp2-256k` | sndr-vllm | 262144 | int4 | spec+tp+tq | Lorbus/Qwen3.6-27B-int4-AutoRound |
| 26 | `qwen3.6-27b-awq-mtp-tq-sndr-tp2-256k` | sndr-vllm | 262144 | awq | spec+tp+tq | shawnw3i/Qwen3.6-27B-AWQ-MTP |
| 27 | `qwen3.6-27b-heretic-autoround-tq-mtp-sndr-tp2-256k` | sndr-vllm | 262144 | int4 | spec+tp+tq | lyf/Qwen3.6-27B-heretic-v2-mtp-int4-AutoRound |
| 28 | `gemma-4-31b-awq-mtp-sndr-tp2-64k` | sndr-vllm | 65536 | awq | spec+tp | cyankiwi/gemma-4-31B-it-AWQ-4bit (+ gemma-4-31B-it-assistant draft) |
| 29 | `gemma-4-31b-awq-kvauto-sndr-tp2-32k` | sndr-vllm | 32768 | awq | tp | cyankiwi/gemma-4-31B-it-AWQ-4bit |

> `†` vLLM VL/caption models natively multimodal (no `mmproj`); "vision" cap inferred from name (`/v1/models` reports `text+image->text` via `modelLooksMultimodal`). `tp` rows set `tensor-split`/`split-mode` (llama.cpp/beellama) or `tensor-parallel:2` (vLLM `tp2`). `lucebox-dflash` rows have no `ctx-size` (server-derived). `qwythos-9b-mtp-q8-768k` (786432) = largest context.
> **Rows 25–29** (`-sndr`, kind `sndr-vllm`, §2.5): `tq` cap = **TurboQuant k8v4** KV (`kv-cache-dtype: turboquant_k8v4`, +27% KV concurrency vs `fp8_e5m2`; 2.47× vs 1.94× @262144 on 27B); env carries per-model `GENESIS_ENABLE_*` matrix + mandatory `GENESIS_ENFORCE_VERSION_RANGE=1`. 25–27 = TurboQuant + MTP K=5, keep vision (fair A/B). 28–29 = FP16-KV gemma (`kv: auto`); 28 has external MTP draft (`gemma-4-31B-it-assistant`, K=8), 29 chat-only; both need `--language-model-only` or gemma-4's vision-encoder budget exceeds `max-num-batched-tokens` at boot. Runbook: `docs/sndr-backend.md`.

<!-- gitnexus:start -->
## GitNexus — Code Intelligence

Indexed as **model-loader** (≈9.9k symbols, ≈31.8k relationships). GitNexus MCP tools assess blast radius & flows (`gitnexus_impact` upstream before editing a symbol, `gitnexus_query`/`gitnexus_context` to explore, `gitnexus_rename` for call-graph-aware renames). If the tools are unavailable or the index is stale, run `npx gitnexus analyze` — else verify blast radius manually. Skills under `.claude/skills/gitnexus/`.
<!-- gitnexus:end -->
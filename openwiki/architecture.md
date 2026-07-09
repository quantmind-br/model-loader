# Architecture

> **Note:** `ARCHITECTURE.md` at the repo root is auto-generated from a knowledge graph and is stale (cites embedded llama-server schema v9680/244 flags, 24 service packages). This page reflects the **current** tree at HEAD with working-tree changes.

## Module map

| Layer | Path | Role |
|-------|------|------|
| Entry / dispatch | `cmd/model-loader/main.go` | `cli.TUIRunner = runTUI`; `cli.Execute()`. `runTUI` takes the single-instance flock, calls `app.Bootstrap`, wires TUI-only services + 5 pages, runs `tea.NewProgram(root, WithAltScreen)`; on exit logs (does not kill) orphaned instances |
| Dev tools | `cmd/regenerate-schemas/`, `cmd/scripts/print_args.go` | `go run` helpers — re-parse every backend `--help`; print resolved args. Not CLI subcommands |
| Shared bootstrap (DI) | `internal/app/bootstrap.go` | One `Services` container for TUI, CLI, headless modes |
| Config | `internal/config/` | Viper TOML loader + one-time migrations |
| CLI | `internal/cli/` | Cobra tree; never imports `internal/ui` |
| TUI | `internal/ui/` | Bubble Tea 5-tab `RootModel`, pages, components, theme |
| Domain | `internal/domain/` | `Profile`, `Instance`, `Backend`, `FlagSchema`, `BackendValidationSchema`, `Presentation`, `BackendKind` — zero external deps |
| Services | `internal/service/*` | 25 self-contained packages, one concern each |
| Logging | `internal/log/` | File-only `slog`, conditional rotation (state owners only), `log.Nop()` fallback |
| Leaf utils | `internal/service/internal/{fsx,procutil,ptrutil,shellsplit}` | `WriteJSONAtomic` / `WriteJSONExclusive` / `ReadJSON[T]`; `Alive(pid)`; generic `Ptr[T]`; `Split` (quote-aware command tokenizer) |

The full inventory of services is in §2.2 below.

## Bootstrap order

`app.Bootstrap(cliLevel, opts...)` (`internal/app/bootstrap.go:73`) is the single entry point used by the TUI, CLI, and headless `serve`. It accepts functional options; the main one is `app.AsStateOwner()`, which marks the caller as an owner of shared mutable state (TUI / `serve`). One-shot CLI commands (e.g. `profile list`) are observers and must NOT pass `AsStateOwner()`. Order:

1. `config.Load()` — Viper TOML, expand `~`, default fallback
2. `log.New()` — file-only `slog` to `log_dir`, `log.Nop()` fallback when nil
3. `profilestore.NewFSStore(profiles_dir)` — one-file-per-profile JSON store
4. `backendcatalog.NewFSStore(backends_dir)` + `NewFSSchemaStore(...)` — catalog + per-backend schemas
5. `backendschema.NewManager(...)` + `RegisterDefaults(...)` — registers embedded `*help` packages
6. `migration.NewService(...).Run(ctx)` — one-time legacy-binary-path → backend-ID migration
7. `backendcatalog.NewResolver(...)` — profile → (binary, kind) resolver
8. `ensureDefaultCatalog(...)` — create default catalog if empty, save default schema for first backend
9. `processmgr.New(...).Reconcile()` (only when `stateOwner=true`) — validate `instances.json` against `/proc/<pid>/comm` + cmdline, drop recycled PIDs. Observers skip this to avoid rewriting the registry while the owner is live (audit A2)
10. `validator.New(logger)`
11. Owner-only boot hygiene (async goroutine): (a) `processmgr.PruneBackendLogs(...)` — removes stale backend log files, (b) `metricsstore.Compact(...)` — compacts per-profile metrics time-series beyond 7-day window

The returned `*app.Services` is closed via `defer svc.Close()` — it stops the process manager and flushes the log file in that order. **Do not call `app.Bootstrap()` twice** in the same process.

## Service layer (25 packages)

Each owns one concern, exports its own interface, takes `Config` with functional options, falls back to `log.Nop()` for nil loggers.

| Service | Purpose |
|---------|---------|
| `processmgr` | Process lifecycle, recovery, history, liveness — largest service (~25 files) |
| `httpproxy` | OpenAI-shaped reverse proxy with implicit model swap; multi-API translation (Anthropic, Responses, Gemini) |
| `proxysupervisor` | Detached proxy state machine (idle→starting→running→stopping); `proxy-state.json` |
| `backendcatalog` | Multi-backend catalog (`catalog.json`) + profile→exe/schema resolver + prober |
| `backendschema` | Schema-gen orchestrator (`--help` parse + curated overlay + embedded fallback) |
| `llamahelp` | llama-server `--help` parser + **embedded v9761** schema |
| `buunhelp` / `dflashhelp` / `sglanghelp` / `vllmhelp` / `unslothhelp` / `tabbyhelp` | Embedded/curated schemas per backend kind (Python/native `--help` too unstable to parse live) |
| `llamabin` | Binary path resolver (PATH + Python fallback) |
| `profilestore` | Profile JSON persistence (one file per profile; per-file flock RMW; export/import; history) |
| `downloadmgr` | HuggingFace download queue + detached worker subprocesses |
| `hfhub` | HF Hub API client (search, repo info, download; `HF_BASE_URL` override) |
| `modelscanner` | GGUF metadata scan (magic `0x46554747`, header, KV; quant/params regex) |
| `monitor` | Logs/slots/GPU/metrics stream via `Subscribe` (nvidia-smi) |
| `metricsstore` | Rolling metrics time-series (Append/Read/Compact) |
| `benchmark` / `benchmarkstore` | Eval engine (judge, math/codegen/ragas/summary/instruction/mmlu, llama-bench, longctx, swe-bench-pro, terminal-bench, deep-swe) + per-run JSON |
| `validator` | Flag + cross-field validation, `Report` aggregation |
| `migration` | One-time legacy-binary-path → backend-ID migration |
| `sizing` | GPU-memory fit calc (`Suggest`, `Fit` Green/Yellow/Red) |
| `configweb` | On-demand in-process web profile/backend editor (HTMX/Alpine) |

## Request flow (proxy → backend)

```
caller
  → POST /v1/{chat.completions, messages, responses, ...} (or /v1beta/...)
    → httpproxy.Server (ServeMux)
      → ensureLoaded(profileID) under swapMu
        ├─ same target? → no-op
        └─ different   → processmgr.Kill(old) + Launch(new) + WaitReady
      → ReverseProxy.ServeHTTP(w, r) to 127.0.0.1:<ephemeral>
        → backend (llama-server / vllm / sglang / ...)
```

Anthropic/Responses/Gemini go through a translation layer (`anthropic_*.go`, `responses_*.go`, `gemini_*.go`) before being forwarded. Translation **never** goes through the `httputil.ReverseProxy` because its `ModifyResponse` destroys reasoning mapping; it uses a hand-rolled `http.Client` instead.

`count_tokens` and admin endpoints **must not** touch `serving` — all inference handlers (catch-all forwarder, `/v1/messages`, `/v1/responses`, Gemini) increment it after loading the backend. Doing so would self-deadlock the drain path.

## State & on-disk layout

```
~/.config/model-loader/
  config.toml                       # app config (TOML, Viper)
  profiles/<id>.json                # one file per profile (id == basename)
  profiles/.history/<id>.previous.json   # backup before destructive edits
  profiles/.<id>.lock               # per-file flock for RMW
  backends/catalog.json             # registered backends
  backends/schemas/<id>.json        # per-backend validation schema

~/.local/state/model-loader/
  model-loader.lock                 # single-instance flock target
  instances.json                    # running instances registry (now includes `startTicks` for PID recycling detection)
  instances-history.json            # exited instances
  proxy-state.json                  # proxy supervisor state
  logs/<profile-id>-<port>.log      # merged backend stdout+stderr
  metrics/<profileID>/*.jsonl       # metrics time-series
  benchmark/runs/*.json             # one per benchmark run (+ .transcript.json sidecars)
  downloads/dl-<id>.json            # per-download worker state
```

## Cross-subsystem edges

- `httpproxy` → `processmgr` + `profilestore`
- `proxysupervisor` → `httpproxy` (spawns detached `serve` process; reconciles `proxy-state.json` + `/_status` probe)
- `backendcatalog` ← `processmgr` (resolver)
- `backendschema` → `backendcatalog` (uses `SchemaStore`)
- `migration` → `catalog` + `schema` + `profilestore`
- `benchmark` → `httpproxy` + `monitor` + `profilestore`
- `configweb` → `validator` + `catalog` + `profilestore`
- `monitor` → `processmgr` (pid + logPath) + backend port
- `profilestore` ↔ `processmgr` (`LastUsedSink.MarkLastUsed` on first `/health` 200)
- `downloadmgr` → leaf `procutil` only (no other service dependency)

## Concurrency contracts

- **`processmgr` registry writes** — uses flock-guarded delta upsert (`mutateRegistry` via `internal/service/internal/fsx/flock.go`), replacing the old `saveRegistry` full-rewrite pattern. `Reconcile` is the 6th `mutateRegistry` callsite (validation inside the lock).
- **`httpproxy.swapMu`** serializes load/unload across concurrent requests.
- **`httpproxy.serving`** (atomic) tracks requests proxied through the loaded backend; `drainServing()` polls it for graceful unload. `count_tokens` and admin endpoints skip it.
- **Boot `Reconcile`** validates `instances.json` entries via `entryAlive()`: new entries carry `StartTicks` (proc start time, `/proc/<pid>/stat` field 22) for authoritative `SameProcess` PID-recycling detection; legacy entries fall back to `/proc/<pid>/comm` + cmdline heuristics with kind-specific fallback tokens (e.g. `"vllm"` for vLLM).
- **Long-lived goroutines** install `defer recover()` outside Bubble Tea's net (so the TUI doesn't panic-restart from background work).

## Conventions

- Service package exports an **interface** that consumers import (not the concrete type).
- Functional options pattern: `WithLogger`, `WithWaitFunc`, …
- Package-level **error sentinels** (`var ErrNotFound = errors.New(...)`); never `fmt.Errorf` for sentinels.
- Atomic JSON via `fsx.WriteJSONAtomic` (temp+rename) and `WriteJSONExclusive` (hard-link race-free Create).
- The single `init()` in the repo is the theme's `NO_COLOR` strip.
- All UI text and schema metadata is **English only** — no localization.

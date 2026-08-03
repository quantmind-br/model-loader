---
type: Architecture
title: Architecture overview
description: Top-level layout of model-loader — the single-binary entrypoints, the dependency-injection container, the three bootstrap modes (owner vs observer), on-disk state, and the end-to-end proxy request flow that hot-swaps the active backend.
tags: [architecture, bootstrap, di, request-flow]
---

# Architecture

model-loader is one binary with three surfaces (TUI, `serve` daemon, Cobra CLI) that share a single dependency-injection container and a registry of backend processes on disk. The defining behavior is the **single-loaded-backend hot-swap proxy**: every inference request, regardless of which client API it arrives on, routes to at most one live backend process, swapped on demand.

## Top-level layout

| Path | Role |
|------|------|
| `cmd/model-loader/main.go` | Single-binary entry. `main()` wires `cli.TUIRunner = runTUI`, then `os.Exit(cli.Execute())`. `runTUI` takes the single-instance flock, bootstraps services as state owner, builds 5 pages + supervisor + download manager + monitor, and runs the Bubble Tea program. On exit it logs (does **not** kill) orphaned live instances so inference survives. |
| `cmd/regenerate-schemas/main.go` | Dev helper (`go run`) that calls `backendschema.Manager.RefreshSchema` for every catalog entry. |
| `cmd/scripts/print_args.go` | Dev helper: loads a profile, runs `BuildArgsForBackend`, prints resolved exe + args. |
| `internal/app/bootstrap.go` | The single DI container `Services` and `Bootstrap(...)`. Wires config, logger, stores, schema manager, validator, and process manager. |
| `internal/app/lock.go` | `AcquireSingleInstanceLock` — non-blocking `LOCK_EX|LOCK_NB` on `<stateDir>/model-loader.lock`. |
| `internal/config/config.go` | Viper TOML loader for `~/.config/model-loader/config.toml`. |
| `internal/domain/` | Zero external deps. Types: `Profile` (schemaVersion 3), `RunningInstance`, `ExitedInstance`, `Backend`, `BackendKind`, `FlagSchema`, `BackendValidationSchema`. See [Data Model](data-model.md). |
| `internal/log/log.go` | File-only `slog`; rotation gated by the state-owner flag. |
| `internal/cli/` | Cobra tree. **Must not import `internal/ui`** — uses the `TUIRunner` callback to break the cycle. `ExitError{Code}` for non-1 exits. |
| `internal/ui/` | Bubble Tea `RootModel` (`root.go`), 5 tabs. Subdirs `theme/`, `components/`, `pages/`. |
| `internal/service/` | 25 packages, one concern each; each exports its own interface, takes `Config` + functional options, falls back to `log.Nop()`. |
| `internal/service/internal/{fsx,procutil,shellsplit}/` | Leaf utilities. **Never reimplement** `WriteJSONAtomic`/`StartTicks`/quote-aware split outside these packages. |

The largest service packages are documented on their own pages: [Process Manager](process-manager.md), [HTTP Proxy](http-proxy.md), [Backend Schema](backend-schema.md), and [Benchmark Engine](benchmark.md).

## Bootstrap and the owner/observer split

`app.Bootstrap(cliLevel, opts...)` is the single wiring point. Every entrypoint calls it and `defer svc.Close()`. The critical option is `app.AsStateOwner()`, which gates **writes** to shared mutable state:

| Entrypoint | Bootstrap mode | Holds flock? | Why |
|------------|----------------|--------------|-----|
| TUI (`runTUI`) | `Bootstrap(…, AsStateOwner())` | Yes | Owns the registry; reconciles `instances.json` at boot; rotates the log. |
| `serve` daemon (`cli/serve.go`) | `Bootstrap(…, AsStateOwner())` | **No** | Coexists with the TUI because registry writes are flock-guarded deltas (audit A2). |
| `instance start/stop/restart`, `benchmark run` | `bootstrapWithLock` | Yes | Acquires flock + `Bootstrap` **without** `AsStateOwner`; the owning TUI/serve already gates registry writes. |
| Read-only CLI (`profile list`, `model list`, …) | plain `Bootstrap` | No | Observer only — must never rewrite the registry nor rotate the live session's log (audits A2/C6). |

`AsStateOwner()` flips three behaviors: `mgr.Reconcile()` runs at boot (a write — drops dead/recycled entries), `log.New(..., Rotate: true)`, and background pruning of backend logs + metrics compaction. Passing it from a one-shot command is a correctness bug.

## Request flow

Every chat request — whether OpenAI-native, Anthropic Messages, OpenAI Responses, or Gemini — shares one swap path and one upstream call to the loaded backend's `/v1/chat/completions`. The proxy reads the request's `model` field (a profile id) and, if it differs from the currently-loaded backend, swaps under a single mutex. Admin and status endpoints never touch a backend.

<!-- openwiki: mermaid parse failed and this diagram was converted to a text fence so it does not break rendering. Fix the diagram source and restore the mermaid fence. Parser error: Heuristic: an unescaped angle bracket inside a label breaks rendering; rephrase the label. -->
```text
flowchart TD
    REQ["Client request"] --> ROUTES{"Route match"}
    ROUTES -->|"/v1/models"| ML["handleModelsList<br/>profiles -> orModel list"]
    ROUTES -->|"/_status"| ST["Status JSON snapshot"]
    ROUTES -->|"/_admin/load , /_admin/unload"| ADM["admin endpoints"]
    ROUTES -->|"/v1/messages"| AN["Anthropic translate"]
    ROUTES -->|"/v1/responses"| RP["Responses translate"]
    ROUTES -->|"/v1beta/models/*"| GE["Gemini translate"]
    ROUTES -->|"/ catch-all"| FW["OpenAI reverse proxy"]
    AN --> ENS
    RP --> ENS
    GE --> ENS
    FW --> ENS
    ADM --> ENS
    ENS{"ensureLoaded<br/>serialized by swapMu"} -->|"same profile + alive"| USE["Backend use phase"]
    ENS -->|"different model"| SWAP["kill old backend<br/>launch new + WaitReady"]
    SWAP --> USE
    USE --> BACKEND["loaded backend /v1/chat/completions"]
    ML -.->|no backend touch| DONE["respond"]
    ST -.-> DONE
    ROUTES -->|"/v1/messages/count_tokens"| CT["local token estimate<br/>no backend"]
    CT -.-> DONE
```

Caption: the four inference routes plus `/_admin/load` share one `ensureLoaded` swap path; the OpenAI catch-all reverse-proxies natively while the translated routes call the upstream directly (bypassing `ModifyResponse` so reasoning mapping survives). Status, model-list, and count-tokens never touch a backend. See [HTTP Proxy](http-proxy.md) for the swap decision logic and translation surface.

## State on disk

```
~/.config/model-loader/
  config.toml                       # Viper-loaded
  profiles/<id>.json                # id == basename, schemaVersion 3
  backends/catalog.json
  backends/schemas/<id>.json
~/.local/state/model-loader/
  model-loader.lock                 # single-instance flock
  instances.json                    # running; flock-guarded deltas
  instances-history.json
  proxy-state.json                  # proxysupervisor state
  logs/<profile-id>-<port>.log      # merged stdout+stderr
  metrics/<profileID>/*.jsonl
  benchmark/runs/*.json (+ transcripts)
  downloads/dl-<id>.json
```

All JSON persistence uses atomic writes from `internal/service/internal/fsx` (`WriteJSONAtomic` temp+rename, `WriteJSONExclusive` temp+hardlink). Registry writes are flock-guarded deltas: read the current file, mutate only the caller's keys, save atomically. A parse failure **aborts without writing** — the registry is never wiped.

## Source map

| Concern | Files |
|---------|-------|
| TUI entry | `cmd/model-loader/main.go` |
| Headless proxy | `internal/cli/serve.go` |
| One-shot CLI bootstrap | `internal/cli/bootstrap_lock.go`, `internal/app/bootstrap.go` |
| DI container | `internal/app/bootstrap.go` |
| Single-instance lock | `internal/app/lock.go` |
| Domain types | `internal/domain/{profile,instance,backend,backend_schema,flag_schema}.go` |
| Config loader | `internal/config/config.go` |
| Profile JSON persistence | `internal/service/profilestore/` |
| Process lifecycle | `internal/service/processmgr/{launch,manager,enrichment,recover,liveness,restart,registry,prune}.go` |
| HTTP proxy | `internal/service/httpproxy/{server,handler,proxy,extract}.go` + `*_handlers.go` |
| Proxy supervisor | `internal/service/proxysupervisor/{supervisor,state,client}.go` |
| Backend catalog + schema | `internal/service/backendcatalog/`, `internal/service/backendschema/` |
| Monitor | `internal/service/monitor/` |
| Benchmark engine | `internal/service/benchmark/` |
| Web editor + benchmark viewer | `internal/service/configweb/` |
| TUI root + pages | `internal/ui/root.go`, `internal/ui/pages/{profiles,server,models,backends,benchmark}*.go` |
| Atomic JSON + flock + proc utilities | `internal/service/internal/{fsx,procutil,shellsplit}/` |
| Canonical profile JSON Schema | `docs/profile-schema.json` |
| Operator runbooks | `docs/{config,troubleshooting,sndr-backend,swe-bench-pro,deep-swe,BENCHMARK}.md` |
| Defect tracker | GitHub Issues; legacy identifier notes in `BUGS.md` |

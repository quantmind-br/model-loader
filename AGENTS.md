# Repository Guidelines

> `CLAUDE.md` is a symlink to this file; **edit `AGENTS.md` in place.** The
> symlink target follows automatically. Defect tracker: `BUGS.md` (single
> source of truth — do not embed a copy here).

---

## 1. Project Overview

**model-loader** is a Go 1.26.2 terminal UI **and** headless CLI for running
local LLM inference servers on a single workstation (reference rig: 2× RTX
3090). It manages *launch profiles*, supervises backend *processes*, and fronts
them with one OpenAI-shaped HTTP proxy that hot-swaps the active model on
demand.

- **For:** a single operator curating tuned launch configs for one GPU.
  Loopback-only proxy, no auth, single-instance lock.
- **Backends (9 `BackendKind`s):** `llama-server`, `vllm`, `sglang`, `dflash`,
  `buun-llama-cpp`, `beellama-cpp`, `ik-llama-cpp`, `unsloth`, `tabby`. Operators register
  each backend (binary + kind) via `model-loader backend add`.
- **Surfaces:** 5-tab Bubble Tea TUI (default), headless `serve` proxy daemon,
  full Cobra CLI mirroring every TUI action. Backends are intentionally
  **orphaned** on TUI exit so inference survives.
- **Module:** `github.com/quantmind-br/model-loader`
- **Repo state:** bug tracker in `BUGS.md`; PRDs/plans under `docs/superpowers/`;
  curated reference in `openwiki/`; AGENTS.md (this file) is the operator KB.

---

## 2. Architecture & Data Flow

### Top-level layout

| Path | Role |
|---|---|
| `cmd/model-loader/` | Single-binary entry. `main()` wires `cli.TUIRunner = runTUI`, then `os.Exit(cli.Execute())`. `runTUI` takes the single-instance flock, calls `app.Bootstrap(…, app.AsStateOwner())`, builds 5 pages + supervisor + download manager + monitor, runs `tea.NewProgram(root, tea.WithAltScreen())`. On exit logs (does **not** kill) orphaned live instances. |
| `cmd/regenerate-schemas/` | Dev helper (`go run`). Calls `backendschema.Manager.RefreshSchema` for every catalog entry. |
| `cmd/scripts/print_args.go` | Dev helper. Loads a profile, runs `BuildArgsForBackend`, prints exe+args. |
| `internal/app/` | Single DI container `Services` (`bootstrap.go`), `AcquireSingleInstanceLock` (`lock.go`), `BenchmarkConfig` (`benchmark_config.go`, single literal — "never reintroduce a second copy"). |
| `internal/config/` | Viper TOML loader (`config.go`); per-user `~/.config/model-loader/config.toml`. |
| `internal/domain/` | Zero external deps. Types: `Profile` (schemaVersion=3), `RunningInstance`, `ExitedInstance`, `Backend`, `FlagSchema`, `BackendValidationSchema`, `BackendKind`. |
| `internal/log/` | File-only `slog` (`log.go`); rotation gated by state-owner flag. |
| `internal/cli/` | Cobra tree. **MUST NOT import `internal/ui`** — uses `TUIRunner` callback to break the cycle. `ExitError{Code}` for non-1 exits. |
| `internal/ui/` | Bubble Tea `RootModel` (`root.go`), 5 tabs. Subdirs: `theme/`, `components/`, `pages/`, `internal/filter/`. |
| `internal/service/` | **25 packages**: `processmgr`, `httpproxy`, `proxysupervisor`, `backendcatalog`, `backendschema`, `llamahelp`, `buunhelp`, `dflashhelp`, `sglanghelp`, `vllmhelp`, `unslothhelp`, `tabbyhelp`, `llamabin`, `profilestore`, `downloadmgr`, `hfhub`, `modelscanner`, `monitor`, `metricsstore`, `benchmark`, `benchmarkstore`, `validator`, `migration`, `sizing`, `configweb`. Plus `internal/{fsx,procutil,shellsplit}`. Each owns one concern, exports its own interface, takes `Config` + functional options, falls back to `log.Nop()`. |

### DI entry points

```
                            runTUI ──────────────── acquires flock + app.Bootstrap(AsStateOwner)
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
       5 TUI pages     supervisor      downloadmgr/monitor
              │
              ▼
       RootModel[5]tea.Model
```

- **TUI** (`cmd/model-loader/main.go`): `app.Bootstrap(cliLevel, app.AsStateOwner())`. Holds flock. Reconciles `instances.json` at boot.
- **`serve`** (`internal/cli/serve.go`): `app.Bootstrap(…, app.AsStateOwner())`. Holds **no flock** — coexists with TUI because registry writes are flock-guarded deltas.
- **`benchmark`** + **`instance start/stop/restart`** (`internal/cli/bootstrap_lock.go`): `bootstrapWithLock` acquires flock + `app.Bootstrap` **without** `AsStateOwner` (owning TUI/serve already gates registry writes).
- **Read-only CLI** (`profile list`, `model list`, …): plain `app.Bootstrap`. Observer only.

### Request flow (proxy)

```
client ──► httpproxy ──► ensureLoaded (swapMu)
                          │
                          ├─ same target + SameProcess → no-op
                          ├─ different target        → kill old + launch new + WaitReady
                          └─ crash detected           → relaunch (proxy recovers, no 502)

loaded backend ←─ reverse proxy ←─ catch-all /
                                    /v1/messages        (Anthropic → OpenAI chat)
                                    /v1/responses       (OpenAI Responses → chat)
                                    /v1beta/models…     (Gemini → chat)
                                    /v1/models          (OpenRouter-shaped list)
                                    /_admin/load, /_admin/unload
                                    /_status
```

Anthropic/Responses paths **never** route through `loaded.proxy.ModifyResponse`
— they `postUpstreamChat` directly to `127.0.0.1:<port>/v1/chat/completions` so
reasoning/thinking mapping survives.

### State on disk

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

---

## 3. Key Directories

| Path | Purpose |
|---|---|
| `cmd/model-loader/main.go` | TUI + `serve` entry (one binary). |
| `internal/app/bootstrap.go` | DI container `Services`. Use `AsStateOwner()` for any code path that owns the registry. |
| `internal/cli/{serve,benchmark,root,bootstrap_lock}.go` | Headless entry points. `bootstrap_lock.go` is the shared flock-acquisition helper. |
| `internal/domain/` | Zero-deps types. **Edit `profile.go` ⟹ also edit `docs/profile-schema.json`** in the same commit. |
| `internal/service/processmgr/` | Largest service. Launch / Kill / Reconcile / WaitHealthy / restart engine. Files: `launch.go`, `manager.go`, `enrichment.go`, `recover.go`, `liveness.go`, `restart.go`, `registry.go`, `prune.go`. |
| `internal/service/httpproxy/` | OpenAI/Anthropic/Responses/Gemini translation + admin endpoints. Files split by surface area (`server.go`, `proxy.go`, `handler.go`, `extract.go`, `*_handlers.go`, `*_translate.go`, `*_stream.go`). |
| `internal/service/proxysupervisor/` | Detached proxy supervisor. Owns `proxy-state.json`. |
| `internal/service/backendschema/` | `--help`-parse orchestrator + curated overlay. Per-kind embedded schemas in `llamahelp`, `vllmhelp`, `sglanghelp`, `dflashhelp`, `buunhelp`, `unslothhelp`, `tabbyhelp`. |
| `internal/service/{profilestore,backendcatalog,monitor,metricsstore,downloadmgr,hfhub,modelscanner,benchmark,benchmarkstore,validator,migration,sizing,configweb,llamabin}/` | One concern each. |
| `internal/service/internal/{fsx,procutil,shellsplit}/` | Leaf utilities. **Never reimplement `WriteJSONAtomic`/`StartTicks`/`quote-aware split` outside these packages.** |
| `internal/ui/{theme,components,pages}/` | Theme + reusable widgets (tab_bar, statusbar, modal, overlay, flash, sparkline, proxy_panel, etc.) + per-tab page sets. |
| `docs/profile-schema.json` | Canonical JSON Schema for `domain.Profile` (v3). |
| `docs/{config,troubleshooting,sndr-backend,swe-bench-pro,deep-swe,BENCHMARK}.md` | Operator-facing runbooks. |
| `docs/superpowers/{specs,plans}/` | PRDs + dated implementation plans. |
| `openwiki/` | Curated topic-organized reference (hand-maintained via an `update` AI command). `quickstart.md` is the entry; `architecture.md`, `tui.md`, `cli.md`, `proxy.md`, `backends.md`, `profiles.md`, `benchmark.md`, `operations.md`. |
| `scripts/` | Operator scripts (SNDR setup, Qwen benchmarks, live monitor). |
| `tools/<task>-curate/` | Python dataset curaters (8 dirs). Writes committed `.json` files under `internal/service/benchmark/data/`. |
| `backends/<id>/` | **Gitignored** vendored backend source trees. No git submodules; manage each as its own checkout. |
| `testdata/` | Golden fixtures (`help-v9761.{txt,golden.json}`) + fake binaries (`fake-llama-server.sh`, `fake-llama-help.sh`). |
| `BUGS.md` | Single source of truth for defects. See §7. |

---

## 4. Development Commands

Makefile has exactly three targets:

```bash
make build       # go build -ldflags "<version + build_date>" -o bin/model-loader ./cmd/model-loader
make install     # GOBIN=~/.local/bin go install ./cmd/model-loader
make tests       # go test ./...
```

Other frequent `go` invocations:

```bash
# Regenerate llama-server --help golden:
go test ./internal/service/llamahelp -update

# Run a single package / test:
go test ./internal/service/processmgr/...
go test ./internal/<pkg>/... -run TestName

# Re-parse every backend --help after touching a *help package:
go run ./cmd/regenerate-schemas

# Inspect a profile's resolved exe + args:
go run ./cmd/scripts/print_args.go               # default profile
go run ./cmd/scripts/print_args.go <profile-id>   # specific profile

# Run the TUI:
./bin/model-loader           # or `make build && ./bin/model-loader`
./bin/model-loader serve     # headless proxy daemon (no flock)
```

**No** `gofmt`/`go vet`/`golangci-lint`/`staticcheck` targets. No CI workflows.
No Dockerfile / goreleaser / brew formula. Distribution = `make install`.

---

## 5. Code Conventions & Common Patterns

### Service layer

- `*Manager` / `*Store` naming.
- Each package exports its own interface; consumers import the interface.
- `type Config struct{…}` + functional options (`WithLogger`, `WithWaitFunc`).
- `log.Nop()` fallback; never `nil` logger.
- **Error sentinels** as package-level `var Err… = errors.New(…)`. Never use
  `fmt.Errorf` for sentinels. Examples: `ErrModelNotFound`,
  `ErrForegroundBusy`, `ErrHealthCheckTimeout`, `ErrProxyDegraded`.
- **Atomic JSON** via `internal/service/internal/fsx`:
  `WriteJSONAtomic` (MkdirAll + unique CreateTemp + Rename, chmod 0644) and
  `WriteJSONExclusive` (race-free Create via hard-link).
- **Registry writes are flock-guarded deltas** (`fsx.WithFileLock` + load +
  keyed mutate + atomic save). Abort on load error; never wipe on parse fail.
- **6 documented `mutateRegistry` callsites (all OUT of `m.mu`)** —
  `processmgr/launch.go::Launch`, `launchForeground`, `Kill` (×2 internal),
  `liveness.go`, `waitEnrichment`, `Reconcile`. Adding a 7th requires updating
  this contract + the audit docs.

### Process management invariants

1. **`AsStateOwner()`** gates `log.New(... Rotate: true)` and `mgr.Reconcile()`.
   Only TUI + `serve` pass it. One-shot CLI commands are observers and **must
   not** pass it (audit A2/C6).
2. **Single-instance flock** is `LOCK_EX|LOCK_NB` on `<stateDir>/model-loader.lock`.
   TUI + `bootstrapWithLock` acquire; `serve` does not (registry writes are
   flock-guarded so TUI and `serve` coexist).
3. **`hasReaper[pid]`** marks PIDs with live `cmd.Wait` reapers; liveness
   applies restart policy only to adopted (reaper-less) deaths.
4. **Reaper starts AFTER registry upsert** commits (P-C4): a fast-crash
   `Crashed` delta can never be clobbered by a launch's running-state delta.
5. **Kill-intent guard**: `killRequested[pid]` is set under `m.mu` **before**
   signaling. Restart path no-ops on `killRequested` (intentional kills never
   resurrect the backend).
6. **Identity-aware everywhere**: `procutil.SameProcess(pid, StartTicks)` from
   `/proc/<pid>/stat` field 22. Recycled PIDs are never signaled and never
   surfaced as live. `StartTicks=0` falls back to legacy `/proc/<pid>/comm` +
   cmdline heuristics.
7. **Group kill**: spawn with `SysProcAttr.Setsid: true`; `procutil.TerminateTree`
   sweeps the group (vLLM EngineCore/Worker_TP + SGLang trees die with leader).
8. **`PYTHONUNBUFFERED=1`** is injected at spawn for vLLM/SGLang/Unsloth
   (Python backends). Without it their logs block-buffer and the Server-tab tail
   lands in bursts.
9. **`healthCheckTimeout`** default is **180s** in the proxy; bumped to
   **360s** in `serve` for large MoE loads. Always check `ProcessExited`
   after `WaitHealthy`.

### Proxy invariants

- `swapMu` serializes swaps. `inflight atomic.Int64` is a gauge.
  `serving atomic.Int64` covers only the backend-use phase (catch-all proxy +
  anthropic/responses/gemini upstream). Requests parked at `swapMu` do **not**
  stall `/_admin/unload`.
- `/_admin/unload`, `/_admin/load`, `/v1/messages/count_tokens` participate in
  neither gauge — comment in `anthropic_handlers.go` explains.
- Status JSON is **snake_case**; `Status.UnmarshalJSON` falls back to
  PascalCase for old proxy binaries.
- Errors are OpenAI-enveloped, except two Anthropic routes that use
  `{"type":"error","error":{…}}`.

### TUI conventions

- Global rune shortcuts are gated by `activePageCapturesInput()`. Only
  `ctrl+c` is unconditional. A capturing page implements
  `InputCapture.IsCapturingInput()`.
- Pages hosting `*huh.Form` **must** forward non-`KeyMsg` messages so focus and
  validation complete.
- `lowercase = navigation` (`k`/`j`/`g`/`G` reserved for lists);
  `UPPERCASE = destructive, always confirm` (`X` delete, `K` kill/unload,
  `R` refresh, `I` import, `E` export, `D` default, `P` probe, `C` clear).
- Pages implement optional interfaces in `internal/ui/contracts.go`:
  `InputCapture`, `Reloader`, `HintProvider`, `HelpContextProvider`,
  `Overlayer`, `StatusMessageProvider`, `Cleaner`.
- Layout: `MinTermWidth` × `MinTermHeight` (terminal-too-small guard);
  stacked layout below 100 cols; truncate-at-edge otherwise.
- Theme = GitHub-Primer adaptive palette; `NO_COLOR` is stripped in `theme.init()`
  (the repo's only `init()`); helpers `BodyHeight`, `ClampBody`.
- `RootModel` exposes `With*Page`/`WithProcessManager` fluent setters.
- Help overlay (`?`/`esc`): glamour-wrapped viewport; `g/home`→GotoTop,
  `G/end`→GotoBottom are bound explicitly.

### CLI conventions

- `cli.TUIRunner = runTUI` callback keeps `internal/cli` from importing
  `internal/ui`. **Do not** break this.
- `&ExitError{Code: N}` for non-1 exits; unwrapped by `Execute()`.
- Every leaf command calls `app.Bootstrap` and `defer svc.Close()`.
- Output via `cmd.OutOrStdout()` / `ErrOrStderr()` — never `fmt.Print`.
- `--json` honored by every table command.

### Naming and sync rules

- **Profile IDs** are lowercase kebab/slug-shaped
  (`^[a-z0-9]+([._-][a-z0-9]+)*$`); filename basename **equals** `id`.
- `domain.Profile` and `docs/profile-schema.json` **must stay in sync**. Schema
  version 3, `additionalProperties:false`.
- Backend IDs: kebab/catalog id like `llama.cpp-stable`, `sglang-dflash`,
  `sndr-vllm`. `backend_id` ≠ `kind` — one `kind` may have multiple
  `backend_id`s.
- **Single mode list**: `benchmark.ModesInOrder()` is canonical. `cli` mode
  list derives from it. Do not introduce a second literal.
- **Single benchmark config**: `app.BenchmarkConfig(cfg)` — duplicating the
  literal once cost DeepSWE its CLI wiring. Wire it via the accessor.
- `port` is reserved in `Profile.Args`; the process manager allocates an
  ephemeral loopback port and injects it under the `port` key on Launch.
- English-only: all UI text, schema metadata, flag/field/JSON/TOML keys,
  group labels, descriptions. **No localization.**

### Pre-allocated work / committed resources

- **Do not** start, pre-plan, or pre-allocate todos for cleanup before the
  request demonstrably works.
- **Do not** add comments that aren't strictly functionality. Docstrings and
  Godoc comments are fine; changelogs and "what I just did" notes are noise.
- **Do not** extend `essentialSeed` in `backendschema/presentation.go`
  without explicit request.

---

## 6. Important Files

| Concern | File |
|---|---|
| TUI entry | `cmd/model-loader/main.go` |
| Headless proxy | `internal/cli/serve.go` |
| One-shot CLI bootstrap | `internal/cli/bootstrap_lock.go`, `internal/app/bootstrap.go` |
| DI container | `internal/app/bootstrap.go` |
| Single-instance lock | `internal/app/lock.go` |
| Domain types | `internal/domain/{profile,instance,backend,backend_schema,flag_schema}.go` |
| Config loader | `internal/config/config.go` |
| Profile JSON persistence | `internal/service/profilestore/` |
| Process lifecycle | `internal/service/processmgr/{launch,manager,enrichment,recover,liveness,restart,registry,prune}.go` |
| HTTP proxy | `internal/service/httpproxy/server.go`, `handler.go`, `proxy.go`, `extract.go`, `*_handlers.go` |
| Proxy supervisor | `internal/service/proxysupervisor/supervisor.go`, `state.go` |
| Backend catalog + schema | `internal/service/backendcatalog/`, `internal/service/backendschema/` |
| Embedded `--help` schemas | `internal/service/{llama,vllm,sglang,dflash,buun,unsloth,tabby}help/` |
| Monitor (logs/slots/GPU/metrics) | `internal/service/monitor/` |
| Benchmark engine | `internal/service/benchmark/` (single mode list at `handler.go::ModesInOrder`) |
| Web editor | `internal/service/configweb/` |
| TUI root + pages | `internal/ui/root.go`, `internal/ui/pages/{profiles,server,models,backends,benchmark}*.go` |
| Reusable widgets | `internal/ui/components/` |
| Theme | `internal/ui/theme/` |
| Atomic JSON + flock + proc utilities | `internal/service/internal/{fsx,procutil,shellsplit}/` |
| Canonical profile JSON Schema | `docs/profile-schema.json` |
| Operator runbooks | `docs/{config,troubleshooting,sndr-backend,swe-bench-pro,deep-swe,BENCHMARK}.md` |
| Curated reference | `openwiki/{quickstart,architecture,tui,cli,proxy,backends,profiles,benchmark,operations}.md` |
| Defect tracker | `BUGS.md` |

The 5-tab TUI root lives at `internal/ui/root.go`:
`TabProfiles=0`, `TabServer=1`, `TabModels=2`, `TabBackends=3`, `TabBenchmark=4`.

---

## 7. Runtime / Tooling Preferences

### Required runtime

- **Go** ≥ 1.26.2 (toolchain pinned in `go.mod`).
- **No CGO**. No git submodules. `backends/*` is vendored source under
  `.gitignore` (manage each as its own upstream checkout).

### Binary dependencies (expected on PATH or registered in catalog)

| Binary | Where it's referenced |
|---|---|
| `llama-server` | `llamabin.DefaultName` (`internal/service/llamabin/resolver.go`); `--help`/`--version` probe in `internal/service/llamahelp/exec_parser.go`. Default backend; PATH lookup falls back if no `default_backend_id` in config. |
| `python3` | `llamabin.ResolveCommandWithPythonFallback`; backend builders via `processmgr/launch.go::makeCommand`. |
| `nvidia-smi` | `internal/service/monitor/gpu.go` (GPU monitoring, optional). |
| `bwrap` | `internal/service/benchmark/codegenbench.go` (sandbox; falls back to bare python). |
| `docker` | `terminalbench.go`, `deepswe.go`, `swebenchpro.go` (required by agentic modes). |
| `tb` (Terminal-Bench CLI) | `tbDefaultCmd` in `internal/service/benchmark/terminalbench.go`. |
| `pier` (datacurve-ai/pier) | `deepDefaultCmd` in `internal/service/benchmark/deepswe.go`. |
| `vllm`, `sglang`, `dflash_server`, `unsloth`, `beellama`, `buun`, `tabbyapi` | User-registered via `model-loader backend add --executable <path> --kind <kind>`. Not hard-coded. |

Compound Python commands (`python -m sglang.launch_server`) are supported via
`makeCommand` + `llamabin.ResolveCommandWithPythonFallback`.

### Direct dependencies (`go.mod`)

- CLI: `spf13/cobra v1.10.2`, `spf13/viper v1.20.0-alpha-6`.
- TUI: `charmbracelet/{bubbletea v1.3.10, bubbles v1.0.0, lipgloss, huh v1.0.0,
  glamour v1.0.0, x/ansi, x/exp/teatest}`.
- Other: `fsnotify/fsnotify v1.7.0`, `atotto/clipboard v0.1.4`,
  `mattn/go-runewidth v0.0.19`, `tiktoken-go/tokenizer v0.7.0`.

### Gitignored trees (from `.gitignore`)

- Build: `bin/`, `dist/`, `coverage.out`, `coverage.html`, `__debug_bin*`.
- Backend source: `backends/` (no submodules).
- Go workspace: `vendor/`, `go.work`, `go.work.sum`.
- Agent/tooling scratch (none of these are project code): `.agents/`,
  `.claude/skills/gitnexus`, `.pi/`, `.superpowers/`, `.sisyphus/`,
  `.gitnexus/`, `.worktrees/`, `.omo/`, `.hermes/`, `.understand-anything/`,
  `.claw/`.
- Auto-generated agent artifacts: `AGENTS.md`, `prompt-*.md`, `IDEATION_*.md`,
  `PRD_*.md`, `TUI_AUDIT.md`. **Note:** `AGENTS.md` *is* checked in despite
  this pattern — it's the canonical KB.

### Tooling state directories at repo root (not project code)

| Dir | Tool |
|---|---|
| `.claude/` | Claude Code (settings, skills, commands incl. `backend-schema-update.md`) |
| `.agents/` | Custom skills (`tui-validator`, `tui-design`, `tui-refactor`, `rtx3090-inference-profiles`, `model-loader-clean-up`, `caveman-compress`, `huggingface-download`, `grill-me`, `llamacpp`, …) |
| `.superpowers/`, `.omo/`, `.sisyphus/`, `.pi/`, `.hermes/` | Other AI-agent runtimes |
| `.understand-anything/` | Codebase knowledge-graph cache |
| `.worktrees/`, `.gitnexus/` | Worktrees + GitNexus cache |

### Defect tracking

**`BUGS.md` is the single source of truth** — do not embed a copy here or in
any agent output. Series: `L` (logs), `B` (TUI), `D` (docs), `T` (tests),
`S` (schemas), `N` (SNDR), `V` (vLLM venv), `P` (proxy), `BM` (benchmark),
`DL` (downloads), `DF` (DFlash/DSpark), `UIUX` (TUI+configweb UX),
`BR` (benchmark reliability), plus `GA`, `PN`, `PV`, `CU`, `AUD-{A,B,C}`
audits. Regression tests cite their BUGS.md id in a comment (sometimes
documented in dedicated `*_regression_test.go` files like
`internal/service/benchmark/br_regression_test.go` and
`reliability_regression_test.go`).

When you find a defect, add it to `BUGS.md` and ask the user whether they want
it fixed in the same session.

### Project slash commands / skills

- `.claude/commands/backend-schema-update.md` — the 3-pattern (`A` live-help +
  overlay / `B` curated Go / `C` embedded rows) schema-update workflow used
  by every `*help` package.
- `.claude/skills/` subdirectories: `model-loader-agents-sync`,
  `caveman-compress`, `tui-{design,refactor,validator}`, `grill-me`, `llamacpp`.

---

## 8. Testing & QA

### Frameworks

- **Stdlib `testing` only** for assertions (no `testify`, `gomock`, `mockery`,
  `go-cmp`).
- **Only test-only 3rd-party import:** `github.com/charmbracelet/x/exp/teatest`
  in 2 files (`internal/ui/root_test.go`, `internal/ui/pages/profiles_test.go`).
- **Hand-rolled doubles** (`stub*`, `fake*`) — never a mock framework.
- ~205 `_test.go` files: 0 in `cmd/`, ~127 in `internal/service/**`, 27 in
  `internal/cli/`, ~36 in `internal/ui/**`, 3 in `internal/app/`, 7 in
  `internal/domain/`.

### Conventions

- **`package <x>` (white-box)** by default — needed to seed unexported fields
  (`fsManager.tracked`, etc.). The only black-box exceptions are
  `internal/app/{bootstrap,lock}_test.go` (`package app_test`).
- File naming: `*_test.go` next to source. No `tests/` subdir.
- Function naming: `TestFoo` for standard cases; `TestFoo_bar_baz` for
  scenario descriptions. Sub-tests via `t.Run` are always inside table-driven
  loops over a `tests := []struct{…}{…}` literal at the top of the function.
- Hermeticity: `t.Setenv("NO_COLOR", "")`, `HOME`, `XDG_CONFIG_HOME`,
  `QUANTMIND_API_KEY` etc.
- `t.TempDir()` everywhere; no `ioutil.TempDir`.

### Canonical helpers

| Helper | Where |
|---|---|
| `freePort(t)` | `internal/service/processmgr/manager_test.go:17` (duplicated as `freePortSup` in `proxysupervisor/supervisor_test.go`). |
| `fakeBinary(t)` | `manager_test.go:27` — abs path to `testdata/fake-llama-server.sh`. |
| `newTestManager(t)` | `manager_test.go:36`. |
| `spawnGroupLeader(t)` | `processmgr/lifecycle_audit_test.go:36` — Setsid leader + child PID. |
| `newTestServer(t, store, mgr)` | `httpproxy/swap_test.go:12`. |
| `newAnthropicMux(t, store, mgr)` | `httpproxy/anthropic_handlers_test.go:16`. |
| `startBackend(t, respond)` | `httpproxy/anthropic_handlers_test.go:144` — fake upstream + `upstreamCapture` channel. |
| `runStream(t, script)` | `httpproxy/anthropic_stream_test.go:54` — drives `runAnthropicStream` and parses SSE. |
| `mustTranslate{Anthropic,Gemini,Responses}(t, raw)` | `httpproxy/{anthropic_translate,gemini,responses}_test.go`. |
| `drainCmd(cmd)` | `internal/ui/pages/server_test.go:26` — flattens `BatchMsg` and returns leaves. |
| `stubStore` / `stubManager` | `httpproxy/mocks_test.go` — most-reused doubles; full in-memory `profilestore.Store` + `processmgr.Manager`. |
| `fakeProxy` | `cli/instance_lifecycle_test.go`. |
| `fakeManager` | `cli/instance_test.go`. |
| `fakeBStore`, `fakeStore`, `fakeProber`, `fakeSnapshotter`, `fakeHub`, `fakeDLManager`, `fakeScanner` | per-package, near consumers. |
| `shrinkWatchdogTiming(t, …)` | `benchmark/terminalbench_watchdog_test.go:19` — swap + restore package vars via `t.Cleanup`; shared with `agentic_watchdog_test.go`. |

Status-sequencing pattern: doubles like `fakeProxy` carry a `statusSeq
[]httpproxy.Status` field; `Status()` pops from head and falls back to a
default once empty.

### Golden files

Exactly one canonical pair at `testdata/`:

```
testdata/help-v9761.txt         # input fixture (real --help capture)
testdata/help-v9761.golden.json # expected parsed JSON
```

Generator: `internal/service/llamahelp/parser_test.go::TestParseHelp_Golden`:
```go
var updateGolden = flag.Bool("update", false, "regenerate golden files")
…
if *updateGolden { os.WriteFile(goldenPath, got, 0o644); … }
// else: bytes.Equal + fail with hint: go test -update
```

A duplicate `//go:embed` copy at
`internal/service/backendschema/testdata/help-v9761.golden.json` **must be
kept byte-identical by hand** when the root golden is regenerated.

`//go:embed` is used heavily in production code (curated schemas, benchmark
datasets, configweb assets) but **in zero** `_test.go` files.

### Test categories

- **Pure unit (no I/O):** `internal/domain/*`, `internal/log/*`,
  `internal/service/internal/{fsx,shellsplit,…}`.
- **httptest-based (no real processes):** `httpproxy/*`, `configweb/*`,
  `hfhub/*`, `backendcatalog/*`, `downloadmgr/*`.
- **Real processes + port allocation:** `processmgr/*`, `proxysupervisor/*`,
  `internal/service/internal/procutil/*`, `benchmark/*_watchdog_test.go`,
  `benchmark/{llamabench,longcontext}_probe_test.go`.
- **CLI-level integration:** 27 files in `internal/cli/*`. Uses `bytes.Buffer`
  as `io.Writer`; calls leaf command funcs directly; never `exec.Command` of
  the binary itself.
- **TUI integration:** `internal/ui/root_test.go` +
  `internal/ui/pages/profiles_test.go` use `teatest.NewTestModel` with 120x30
  initial term size. The rest of `internal/ui/pages/*_test.go` is
  pure-function style: construct page struct, invoke `Update`, call
  `drainCmd(cmd)`, assert on `[]tea.Msg`.

### Regression tests and BUGS linkage

Two dedicated regression bundles:
- `internal/service/benchmark/br_regression_test.go` — **BR1–BR8**.
- `internal/service/benchmark/reliability_regression_test.go` — **T1/T3/T5/T7**.

Other themed regressions:
- `benchmark/terminalbench_watchdog_test.go` + `agentic_watchdog_test.go` —
  **UIUX-012** (hang watchdog).
- `processmgr/lifecycle_audit_test.go` — **AUD-A1/A3/A4/A5/A7/A10/A11/A12**.
- `profilestore/fs_store_test.go` — **PV1**.
- `validator/validator_test.go` — **S1**.

Per-test comments (e.g. `// UIUX-011`, `// BR1`, `// AUD-A1`) appear inline in
tests that don't get a dedicated regression file. **Naming**: regression
tests are **not** called `Test*Regression*`; they describe the scenario and
keep the audit id in a comment.

### Coverage tooling

**None in repo.** No codecov config, no `.cover`, no `-cover`/`-coverprofile` in
the Makefile, no GitHub Actions, no CI workflows. `.gitignore` excludes
`coverage.out` / `coverage.html` so ad-hoc runs aren't committed. Coverage is
measured per-session (e.g. `.omo/evidence/*.md`). Thin / low-density packages
worth knowing: `internal/app/{bootstrap,lock}` (single happy-path tests),
`configweb/embed_test.go`, `benchmark/runner_{inst,mmlu}bench_test.go`.

### Quality gate (the entire gate)

```bash
go build ./...     # compile
go test ./...      # full suite
```

That's it. No linter, no formatter target, no CI. Before yielding, run both
and confirm 0 exit codes.

---

*Edit `AGENTS.md` in place. The symlink at `CLAUDE.md` follows it. Defect
history lives in `BUGS.md`.*

# Repository Guidelines

This file is the operational knowledge base for `model-loader`. It is generated
from a parallel scan of the repository across four areas — **core source**,
**tests**, **configs/build**, and **scripts/docs** — and is the single source
of truth for how the code is organized, how to build/test it, and the
conventions any change must respect.

**Work with the grain of the repo.** When a section below says "invariant",
"contract", or "must", treat it as a hard rule: there are tests and audit
bundles that enforce these properties (see §9). When in doubt, follow the
existing pattern in the nearest sibling file.

---

## 1. Project Overview

`model-loader` is a **Go TUI and headless CLI** for running and benchmarking
local LLM inference servers from one terminal application. It manages launch
profiles, supervises inference processes, and exposes a single
OpenAI-compatible HTTP endpoint. When a request names another profile, the
proxy stops the current backend, starts the requested one, waits for it to
become healthy, and forwards the request.

- Designed for a **single operator on a local GPU workstation** (reference
  system: two RTX 3090s). The process manager and profile model are not tied to
  that hardware.
- **Two execution modes**, sharing one bootstrap:
  - **TUI** — interactive 5-tab Bubble Tea app (Profiles, Server, Models,
    Backends, Benchmark). Default when run with no subcommand.
  - **Headless CLI** — a complete Cobra command tree (`serve`, `profile`,
    `instance`, `backend`, `model`, `benchmark`, …) with JSON output.
- Backends are registered as **executables + kinds** (`llama-server`, `vllm`,
  `sglang`, `dflash`, `beellama-cpp`, `buun-llama-cpp`, `ik-llama-cpp`,
  `tabby`, `unsloth`, `lmstudio`, `freetoken`). Profiles are validated, portable
  JSON that resolve to a concrete backend binary and argument list at launch
  time.
- The proxy binds to **loopback by default and has no authentication** — do not
  expose it to an untrusted network without an authenticated reverse proxy.

Module path: `github.com/quantmind-br/model-loader`, Go **1.26.2**, license
0BSD. Maintained architecture docs live in `openwiki/` (see §8).

---

## 2. Architecture & Data Flow

### Top-level layout

```
cmd/
  model-loader/          # main: registers cli.TUIRunner = runTUI, then cli.Execute()
  regenerate-schemas/    # re-parse every backend --help and refresh catalog schemas
  scripts/               # small dev helpers (e.g. print_args.go — inspect resolved exe+args)
internal/
  app/                   # Bootstrap(): the single DI wiring point for TUI, CLI, serve, benchmark
  cli/                   # Cobra command tree (must NOT import internal/ui)
  config/                # config.toml load/save (paths, models.search_paths, ui, logging, serve)
  domain/                # shared types, zero external deps (Profile, Instance, Backend, flags, schemas)
  log/                   # slog wrapper; file-only logging, log.Nop()
  ui/                    # Bubble Tea root + 5 tab pages, components, theme, contracts
  service/               # every domain service (see §3)
backends/                # vendored backend checkouts/venvs (gitignored, each with backend-build.sh)
docs/  openwiki/  testdata/
```

501 Go files in the project proper (498 under `internal/`, 3 under `cmd/`), 219 `*_test.go` files. The gitignored `backends/` trees are vendored checkouts with their own sources and are not counted.

### DI entry points

- **`internal/app.Bootstrap(logLevel, opts…)`** is the *single* wiring point.
  Every leaf CLI command, the TUI, `serve`, and `benchmark` call it and get a
  fully assembled `*app.Services` (config, stores, process manager, monitor,
  proxy supervisor, backend catalog, …). `app.AsStateOwner()` is the opt-in
  for state-owning processes (see §5 process invariants).
- **`cli.TUIRunner`** is a package-level callback set by `main` to `runTUI`.
  This is what keeps `internal/cli` from importing `internal/ui` (avoids a
  cycle). **Do not break this.**
- **Version/build info** is injected via ldflags:
  `-X github.com/quantmind-br/model-loader/internal/cli.Version=…` and
  `.BuildDate=…` (see Makefile). Defaults to `dev`.

### Request flow (proxy)

```
client → httpproxy.Server (OpenAI-shaped /v1/*, /v1/messages, /responses, /v1beta/*)
       → resolve requested model id → processmgr (ensure backend up /
         hot-swap profile) → forward request to backend process
```

Public endpoints (`model-loader serve`):

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/chat/completions` | OpenAI-compatible chat inference |
| `POST` | `/v1/messages` | Anthropic Messages translation |
| `POST` | `/v1/responses` | OpenAI Responses translation |
| `POST` | `/v1beta/models/{model}:generateContent` | Gemini translation |
| `GET` | `/v1/models` | List profiles as models |
| `GET` | `/_status` | Proxy and loaded-backend status |
| `POST` | `/_admin/load` | Explicitly load a profile |
| `POST` | `/_admin/unload` | Drain and unload the active backend |

### State on disk

| Location | Contents |
|---|---|
| `~/.config/model-loader/config.toml` | Application configuration |
| `~/.config/model-loader/profiles/` | Versioned profile JSON files |
| `~/.config/model-loader/backends/` | Backend catalog and validation schemas |
| `~/.local/state/model-loader/` | `instances.json`, proxy state, metric store, benchmark runs |

The TUI intentionally leaves live inference processes running when it exits.
Use the Server tab or `model-loader instance stop` to stop one.

---

## 3. Key Directories

### Core source — `internal/`

- **`internal/domain`** — pure types with zero external dependencies:
  `Profile`, `Instance`, `Backend`, `BackendKind`, `FlagSchema`, flag/arg
  helpers, schema builder, model path/HF-repo detection. JSON round-trip
  caveat: ints come back as float64; `validator.checkType` handles it.
- **`internal/config`** — TOML config load/save; `search_paths` model
  discovery; serve defaults; UI keybindings; logging level.
- **`internal/app`** — `Bootstrap()` DI graph + single-instance lock
  (`AcquireSingleInstanceLock`, `bootstrapWithLock`).
- **`internal/cli`** — Cobra tree (see §5 CLI conventions). Commands:
  `serve`, `profile {list,show,add,delete,set-default,export,import,validate,
  duplicate,rename,pin,unpin}`, `instance {list,show,history,start,stop,
  restart,logs,metrics}`, `backend {list,probe,show,schema,refresh,apply,
  add,delete,set-default}`, `model {list,search,info,download,downloads,cancel,
  resume}`, `benchmark {run,list,compare,history,show,transcript,export,delete,
  web}`, plus hidden `download <state-path>` and `_exittest` workers.
- **`internal/ui`** — Bubble Tea root + `pages/` (profiles, server, models,
  backends, benchmark) + `components/` (pickers, modals, charts, statusbar,
  tab bar) + `theme/` + `contracts.go` (page optional interfaces) +
  `internal/filter`.
- **`internal/service`** — one package per service (see below).

### Service layer — `internal/service/`

| Package | Responsibility |
|---|---|
| `profilestore` | Persist/import/export/migrate Profile JSON on disk |
| `validator` | Validate profiles against FlagSchema + fixed rules (`rules.go`, `crossfield.go`) |
| `modelscanner` | Walk search paths, emit `ScanEvent` for GGUF files (quant + param count) |
| `processmgr` | Launch/track/kill/recover/prune/restart LLM server processes; `instances.json` registry; 35 files, largest service |
| `monitor` | Stream logs, slot snapshots, GPU stats, aggregated metrics |
| `httpproxy` | OpenAI-shaped reverse proxy with on-demand backend swap |
| `proxysupervisor` | Run the HTTP proxy as a detached OS process with persistent state |
| `backendcatalog` | Persist backend catalog + validation schemas |
| `backendschema` | Schema manager; `RegisterDefaults` + `RefreshSchema` (used by `regenerate-schemas`); Presentation building/reconciliation |
| `benchmark` | SWE-bench-Lite evaluation, quality/speed/robustness/knowledge/agentic modes |
| `benchmarkstore` | Persist benchmark runs as one JSON file per run |
| `downloadmgr` | HuggingFace downloads via detached worker subprocesses (`model-loader download <state-path>`) |
| `hfhub` | HuggingFace Hub API client (search/info) |
| `configweb` | On-demand web GUI the TUI uses to edit profiles |
| `llamabin` | Resolve/validate `llama-server` binary path (+ python fallback) |
| `llamahelp` | Parse `llama-server --help` into FlagSchema; embedded fallback |
| `vllmhelp`, `sglanghelp`, `tabbyhelp`, `unslothhelp`, `buunhelp`, `dflashhelp`, `lmstudiohelp`, `freetokenhelp` | Embedded curated schemas for Python/other backends |
| `sizing`, `metricsstore`, `migration` | Context-window sizing, metric persistence, legacy migrations |
| `internal/{fsx,procutil,shellsplit,ptrutil}` | Internal helpers (atomic JSON, process utils, shell splitting) |

**Backend schema presentation.** `backendschema` synthesizes the flag groups
shown in the Backends tab. `essentialSeed` (per-kind flag long-names, e.g.
`n-gpu-layers`, `ctx-size`, `flash-attn`) seeds the highlighted "Essentials"
group; `port` is intentionally absent because the process manager owns
allocation. `BuildPresentation` builds the default presentation from the
schema, and `ReconcilePresentation(prev, next)` carries an operator-curated
layout across a regeneration (keeps group order, drops flags the regenerated
schema no longer has, appends new flags to the group that names them).
Curated literal presentations live in `curated_llama.go`, `curated_beellama.go`,
`curated_buun.go`, `curated_ik.go`, `curated_sglang.go`, `curated_vllm.go`
(`Curated*Schema()` returning a `BackendValidationSchema` with `Presentation`
set). **Curated/seed flags must reference only flags the schema actually
defines** — `presentation_coverage_test.go` and the `curated_*_test.go` files
enforce this, because a dangling entry renders as an empty row instead of
failing loudly.

### Backends — `backends/`

One directory per backend variant (e.g. `llama.cpp-stable`,
`llama.cpp-nightly`, `vllm-stable`, `vllm-nightly`, `sglang-*`, `dflash`,
`tabby`, `unsloth`, `buun-llama-cpp`, `beellama.cpp`, `sndr-vllm`,
`freetoken`, …). These are **vendored checkouts/venvs, gitignored, no
submodules** — each tree has
its own `backend-build.sh` and is managed as its own checkout. Never rely on
them being present in a fresh clone.

### Tests — `testdata/` + `*_test.go`

- `testdata/fake-llama-server.sh` — fake llama-server for processmgr tests
  (serves `/health`, echoes argv to stderr, SIGTERM-clean; Linux-only, needs
  `python3` or `nc`). `fake-llama-help.sh`, `help-v10152.txt`,
  `help-v10152.golden.json` support llamahelp golden tests.
- 219 `_test.go` files: 0 in `cmd/`, 141 in `internal/service/**` (incl. 3 in
  `internal/service/internal/`), 25 in `internal/cli/`, 39 in `internal/ui/**`,
  3 in `internal/app/`, 8 in `internal/domain/`, 2 in `internal/config/`,
  1 in `internal/log/`.

### Scripts — `cmd/`

- `cmd/regenerate-schemas` — re-parses every backend `--help` and refreshes
  catalog schemas; run after touching a `*help` package.
- `cmd/scripts/print_args.go` — prints the resolved executable + args for a
  profile (default or `<profile-id>`); useful for debugging launch resolution.

### Docs — `docs/` + root

`README.md` (user guide), `ARCHITECTURE.md` (pointer into openwiki),
`CHANGELOG.md`, `BUGS.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`,
`LICENSE`, `docs/{BENCHMARK.md, config.md, backend-schema-update.md,
sndr-backend.md, deep-swe.md, profile-schema.json}` and historical
`docs/superpowers/plans/`. The maintained topic docs are in `openwiki/` (§8).

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
# Regenerate llama-server --help golden (commits parsed schema):
go test ./internal/service/llamahelp -update

# Run a single package / test:
go test ./internal/service/processmgr/...
go test ./internal/<pkg>/... -run TestName

# Re-parse every backend --help after touching a *help package:
go run ./cmd/regenerate-schemas

# Inspect a profile's resolved exe + args:
go run ./cmd/scripts/print_args.go               # default profile
go run ./cmd/scripts/print_args.go <profile-id>  # specific profile

# Run the TUI / headless proxy:
./bin/model-loader          # or make build && ./bin/model-loader
./bin/model-loader serve    # headless proxy daemon (no single-instance flock)
```

**No** `gofmt`/`go vet`/`golangci-lint`/`staticcheck` Make targets. CI
(`.github/workflows/ci.yml`) runs build, tests, and `go vet` on push/PR to
`main` (Go version from `go.mod`). `release.yml` builds a `v*` tag release
(`CGO_ENABLED=0`, tar + sha256sum, `gh release create`). No Dockerfile /
goreleaser / brew formula.

---

## 5. Code Conventions & Common Patterns

### Service layer

- `*Manager` / `*Store` naming.
- Each package exports its own interface; consumers import the interface.
- `type Config struct{…}` + functional options (`WithLogger`, `WithWaitFunc`).
- `log.Nop()` fallback; never a `nil` logger.
- **Error sentinels** as package-level `var Err… = errors.New(…)`. Never use
  `fmt.Errorf` for sentinels. Examples: `ErrModelNotFound`,
  `ErrForegroundBusy`, `ErrHealthCheckTimeout`, `ErrProxyDegraded`.
- **Atomic JSON** via `internal/service/internal/fsx`:
  `WriteJSONAtomic` (MkdirAll + unique CreateTemp + Rename, chmod 0644) and
  `WriteJSONExclusive` (race-free creation). Never write directly to critical
  JSON paths.

### Process management invariants (processmgr)

1. **`AsStateOwner()`** gates `log.New(... Rotate: true)` and
   `mgr.Reconcile()`. Only the TUI and `serve` pass it. One-shot CLI commands
   are observers and **must not** pass it.
2. **Single-instance flock** is `LOCK_EX|LOCK_NB` on
   `<stateDir>/model-loader.lock`. TUI + `bootstrapWithLock` acquire; `serve`
   does not (registry writes are flock-guarded so TUI and `serve` coexist).
3. **`hasReaper[pid]`** marks PIDs with live `cmd.Wait` reapers; liveness
   applies the restart policy only to adopted (reaper-less) deaths.
4. **Reaper starts AFTER registry upsert commits**: a fast-crash `Crashed`
   delta can never be clobbered by a launch's running-state delta.
5. **Kill-intent guard**: `killRequested[pid]` is set under `m.mu` before
   signaling; the restart path no-ops when a kill is already requested.
6. Ports are **assigned automatically** by the process manager; a `port`
   argument in a profile is ignored and stripped. Instance logging is
   file-only, never stdout.
7. Health checks use a configurable timeout (default 360s) so slow-booting
   models aren't killed mid-init.

### Proxy invariants (httpproxy)

- `swapMu` serializes swaps; `inflight atomic.Int64` is a gauge;
  `serving atomic.Int64` covers only the backend-use phase (catch-all proxy +
  anthropic/responses/gemini upstream). Requests parked at `swapMu` do not
  stall `/_admin/unload`.
- `/_admin/unload`, `/_admin/load`, `/v1/messages/count_tokens` participate in
  neither gauge (comment in `anthropic_handlers.go`).
- Status JSON is **snake_case**; `Status.UnmarshalJSON` falls back to
  PascalCase for old proxy binaries.
- Errors are OpenAI-enveloped, except two Anthropic routes that use
  `{"type":"error","error":{…}}`.

### TUI conventions (internal/ui)

- Global rune shortcuts are gated by `activePageCapturesInput()`. Only
  `ctrl+c` is unconditional. A capturing page implements
  `InputCapture.IsCapturingInput()`.
- Pages hosting `*huh.Form` forms defer to the form's key handling.
- Page updates return `(tea.Model, tea.Cmd)`; fire-and-forget messages are
  sent through a `drainCmd`-style helper in tests.
- The TUI and its pages are covered via `teatest.NewTestModel` (120×30
  terminal) and pure-function page tests (see §9).

### CLI conventions (internal/cli)

- `cli.TUIRunner = runTUI` callback keeps `internal/cli` from importing
  `internal/ui`. **Do not break this.**
- `&ExitError{Code: N}` for non-1 exits; unwrapped by `Execute()`.
- Every leaf command calls `app.Bootstrap` and gets services from it; no
  ad-hoc construction.
- JSON output via `internal/cli/output.go`; `bytes.Buffer` in tests (never
  exec the binary itself).

### Profile naming convention (curation discipline — not code-enforced)

Profile IDs are lowercase, slash-safe kebab slugs, and the profile filename
basename must equal `id`.

Canonical pattern:

```text
<model-family-and-version>-<size>[-<variant>][-<quant>][-<capability/backend/mode>...]-<ctx>
```

Required minimum:

- The **initial segment names the model**: start with the real base model/family
  and version when available (`qwen3.6-27b...`, `gemma-4-e4b...`,
  `agents-a1-35b-a3b...`). Do not start with the backend, task, quant, or a
  local nickname unless that is the model family itself.
- The **final segment is the context window**: `-<ctx>` must be the last segment,
  sourced from the backend's active context argument (`args.ctx-size` for
  llama.cpp, normally `args.max-model-len` for vLLM/SGLang, or
  `args.context-length` where the backend schema uses that spelling). Use the
  binary-k value floored from the configured token count: `262144` → `256k`,
  `253952` → `248k`, `200000` → `195k`, and `1048576` → `1m`. No suffix may
  appear after the context label.
- **Capability segments mirror configuration, not intent:** include `vision`
  only while `args.mmproj` or a vision-native backend configuration is active;
  include `mtp`/`dflash` only while the corresponding speculative configuration
  is active. Backend and placement qualifiers such as `vllm`, `sglang`, `tp2`,
  `beellama`, `textonly`, `tensor`, and `layer` appear before the final context
  segment.
- Name the **real base model**, not merely the publisher's repackaging label.

### Naming and sync rules

- New backend help/schema facts go into the matching `*help` package and are
  regenerated with `go run ./cmd/regenerate-schemas`; golden files update with
  `go test ./internal/service/llamahelp -update`.
- Schema/presentation changes that affect the Backends tab must keep curated
  presentations and `essentialSeed` in sync with the schema (enforced by
  `presentation_coverage_test.go`).
- Docs that describe behavior should be updated in `openwiki/`, not by
  hand-editing generated pages (see §8).

---

## 6. Important Files

| File | Purpose |
|---|---|
| `cmd/model-loader/main.go` | Entry point; sets `cli.TUIRunner`, dispatches modes |
| `internal/app/bootstrap.go` | `Bootstrap()` / `bootServices` DI container |
| `internal/app/benchmark_config.go` | `BenchmarkConfig` mapping for the 12 benchmark modes |
| `internal/cli/root.go` | Cobra root, `Version`/`BuildDate`, `TUIRunner`, `ExitError` |
| `internal/cli/output.go` | JSON output helpers |
| `internal/service/processmgr/` | Process lifecycle + instance registry (largest service) |
| `internal/service/httpproxy/` | OpenAI-shaped reverse proxy + API translations |
| `internal/service/backendschema/presentation.go` | `essentialSeed`, `BuildPresentation`, `ReconcilePresentation` |
| `internal/service/backendschema/curated_*.go` | Curated Backends-tab presentations per kind |
| `internal/service/llamahelp/` | `--help` parser + embedded v9761 schema + golden files |
| `internal/service/backendcatalog/` | `catalog.json` + sentinel `ErrNotFound` |
| `internal/service/benchmark/` | `Runner` — 12 modes + regression bundles |

---

## 7. Runtime / Tooling Preferences

### Required runtime

- Go **1.26.2** (declared in `go.mod`); CI reads the version from `go.mod`.

### Binary dependencies (on PATH or registered in catalog)

`llama-server` (llama.cpp), `vllm`, `sglang`, `dflash_server`, `unsloth`,
`beellama`, `buun`, `tabbyapi`, `ft` (FreeToken). User-registered via
`model-loader backend add --executable <path> --kind <kind>`. Not hard-coded.

Compound Python commands (`python -m sglang.launch_server`, …) are built in
`makeCommand` + `llamabin.ResolveCommandWithPythonFallback`.

### Direct dependencies (`go.mod`)

CLI `spf13/cobra` + `viper`; TUI `charmbracelet/*` (bubbletea, bubbles,
lipgloss, huh, glamour, x/ansi); other: `fsnotify`, `atotto/clipboard`,
`mattn/go-runewidth`, `tiktoken-go/tokenizer`, `golang.org/x/term`.
Test-only third-party: `charmbracelet/x/exp/teatest` (2 files).

### Gitignored trees

`bin/`/`dist/`/`coverage.*`, `backends/` (vendored, no submodules),
`vendor/`/`go.work*`, tooling scratch (`.agents/`, `.claude/`, `.pi/`,
`.superpowers/`, `.worktrees/`, `.gitnexus/`, `.commandcode/`, `.ideation/`,
`.sm/`, `.slim/`). `AGENTS.md` stays tracked despite the ignore pattern
(verified via `git ls-files`). `docs/superpowers/` and `TUI_AUDIT.md` are
ignored design/audit artifacts.

### Defect tracking

Track new defects in GitHub Issues (bug-report template). Existing regression
comments retain legacy IDs such as `BR1` and `AUD-A1`; `BUGS.md` explains that
historical convention and tracks one open investigation (managed backend exits
before health check).

---

## 8. OpenWiki

This repository uses OpenWiki for recurring code documentation. Start with
`openwiki/quickstart.md`, then follow its links to architecture, data model,
HTTP proxy, process manager, backend schemas, surfaces, operations, testing,
and benchmark topics.

Do not hand-edit generated OpenWiki pages unless explicitly asked; prefer
updating source code/docs and letting OpenWiki regenerate. Local
`.agents/`/`.claude/` skills are ignored.

---

## 9. Testing & QA

### Frameworks

- **Stdlib `testing` only** for assertions (no `testify`, `gomock`, `mockery`,
  `go-cmp`).
- **Only test-only 3rd-party import:** `github.com/charmbracelet/x/exp/teatest`
  in 2 files (`internal/ui/root_test.go`, `internal/ui/pages/profiles_test.go`).
- **Hand-rolled doubles** (`stub*`, `fake*`) — never a mock framework.

### Conventions

- **`package <x>` (white-box)** by default — needed to seed unexported fields
  (`fsManager.tracked`, etc.). The only black-box exceptions are
  `internal/app/{bootstrap,lock}_test.go` (`package app_test`).
- File naming: `*_test.go` next to source. No `tests/` subdir.
- Function naming: `TestFoo` for standard cases; `TestFoo_bar_baz` for
  scenario-style cases.
- Table-driven tests with `{name, … want}` structs and `t.Run(tt.name, …)`.
- `t.Helper()` on shared helpers (`freePort`, `fakeBinary`, `newTestManager`,
  `drainCmd`).
- Real processes use `t.TempDir()` + `testdata/fake-llama-server.sh`; ports via
  `freePort` (listen on `127.0.0.1:0`, close, reuse).
- `-update` flag pattern for golden regeneration (`llamahelp/parser_test.go`).

### Golden files

`llamahelp/parser_test.go::TestGenerateGolden` runs only with `-update`; it
commits the parsed schema so the golden test can diff without a live
`llama-server` binary (`help-v10152.golden.json` + `fake-llama-help.sh`).

### Test categories

- **Pure unit (no I/O):** `internal/domain/*`, `internal/log/*`,
  `internal/config/*`, `internal/service/internal/{fsx,shellsplit,…}`.
- **httptest-based (no real processes):** `httpproxy/*`, `configweb/*`,
  `hfhub/*`, `backendcatalog/*`, `downloadmgr/*`.
- **Real processes + port allocation:** `processmgr/*`, `proxysupervisor/*`,
  `internal/service/internal/procutil/*`, `benchmark/*_watchdog_test.go`,
  `benchmark/{llamabench,longcontext}_probe_test.go`.
- **CLI-level integration:** 25 files in `internal/cli/*`. Uses `bytes.Buffer`
  as `io.Writer`; calls leaf command funcs directly; never `exec.Command` of
  the binary itself.
- **TUI integration:** `internal/ui/root_test.go` +
  `internal/ui/pages/profiles_test.go` use `teatest.NewTestModel` with 120x30
  initial term size. The rest of `internal/ui/pages/*_test.go` is
  pure-function style: construct page struct, invoke `Update`, call
  `drainCmd(cmd)`, assert on `[]tea.Msg`.
- **Schema/presentation integrity:** `backendschema/presentation_coverage_test.go`
  + `curated_*_test.go` assert curated presentations and `essentialSeed`
  reference only flags the schema defines.

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
- `config/searchpaths_test.go` — **B9** (rewrite preserves unrelated values).

Per-test comments (e.g. `// UIUX-011`, `// BR1`, `// AUD-A1`) appear inline in
tests without a dedicated regression file. **Naming**: regression tests are
**not** called `Test*Regression*`; they describe the scenario and keep the
audit id in a comment.

### Coverage tooling & quality gate

No codecov config and no `-coverprofile` Make target. `.gitignore` excludes
`coverage.out` / `coverage.html`. The CI quality gate is build + test + `go vet`
on `main` push/PR.

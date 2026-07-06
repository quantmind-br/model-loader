# Operations

## Build & test

The `Makefile` is the only build harness — three phony targets:

```make
make build      # go build -o bin/model-loader ./cmd/model-loader
make install    # GOBIN=$HOME/.local/bin go install ./cmd/model-loader
make tests      # go test ./...
```

Other useful commands:

```bash
go test ./... -update                                           # regenerate golden fixtures
go test ./internal/... -run TestName                            # single test
go test ./internal/service/benchmark/... -run TestMathBench     # single mode test
go run ./cmd/regenerate-schemas                                 # rebuild embedded --help schemas
./bin/model-loader --version                                   # → "dev"
./bin/model-loader --help                                      # full CLI surface
```

### Direct dependencies (`go.mod`, Go 1.26.2)

- `spf13/cobra v1.10.2` — CLI
- `spf13/viper v1.20.0-alpha.6` — TOML config
- `charmbracelet/{bubbletea v1.3.10, bubbles v1.0.0, lipgloss, huh v1.0.0, glamour v1.0.0, x/ansi, x/exp/teatest}` — TUI + tests
- `fsnotify/fsnotify v1.7.0`
- `atotto/clipboard v0.1.4`
- `mattn/go-runewidth v0.0.19`
- `tiktoken-go/tokenizer v0.7.0` — local token estimate

~50 indirect deps (chroma, goldmark, termenv, sahilm/fuzzy, bluemonday, `golang.org/x/*`). **No CGO.**

### Required / optional binaries

- `go ≥ 1.26` — build
- `llama-server` on `$PATH` or registered in catalog — runtime default + schema generator
- `nvidia-smi` — optional GPU metrics
- `vllm` / `sglang` / `dflash` / `unsloth` / `beellama` / `buun` executables — optional, per registered backend

### No CI/CD

There is no `.github/workflows`, `.gitlab-ci.yml`, `.circleci/`, goreleaser, or Dockerfile at the repo root (`backends/*/.github/` = vendored upstream only). The entire quality gate is `go test ./...` + `make build`, run locally. There is no linter, no formatter, and no CI.

## Configuration file

`~/.config/model-loader/config.toml` (auto-generated on first run):

```toml
[paths]
profiles_dir = "~/.config/model-loader/profiles"
backends_dir = "~/.config/model-loader/backends"
log_dir = "~/.local/state/model-loader/logs"
state_dir = "~/.local/state/model-loader"

[models]
search_paths = ["~/.lmstudio/models", "~/models"]

[ui]
default_tab = "profiles"   # recognized: profiles, server, models, backends, benchmark
keybindings = "default"    # only "default" supported

[logging]
level = "info"             # debug | info | warn | error
                           # override: --log-level CLI flag or $MODEL_LOADER_LOG_LEVEL

[serve]
host = "127.0.0.1"
port = 4321

[benchmark]
max_tokens = 32768
temperature = 0.0
timeout_sec = 120
long_context_tokens = 0
save_transcripts = true

[benchmark.judge]
base_url = ""
api_key = ""
model = ""
samples = 3

[benchmark.llamabench]
presets = ["512/128", "4096/256"]
repetitions = 3
```

`paths.llama_server_binary_path` is **legacy** — read only during the one-time migration to the backend catalog. After migration, register binaries via the catalog instead.

All path values support `~` expansion.

## Lifecycle

### Dispatch

`main.go` (no args → `runTUI`; `serve` / `benchmark` / `download` / `import` subcommands are dispatched to their own runners) → `app.Bootstrap` → `config.Load` → `processmgr.Reconcile` (reads `instances.json`).

### Process launch

`prepareLaunch` (in `processmgr/launch.go`):

1. Allocates an ephemeral loopback port (`127.0.0.1:0`) and injects it under the `port` key (profile's own `port` is reserved and stripped)
2. Resolves exe + kind (caller-preset, else `m.resolver(p)`)
3. Spawns the process detached (`Setsid`) with a merged log fd
4. Registers it in `instances.json` under `state_dir`

`cmd.Wait` reaper enriches exit info (code / signal / reason + 50-line stderr tail).

Sentinels: `ErrModelNotFound`, `ErrForegroundBusy`, `ErrUnknownPID`, `ErrHealthCheckTimeout`, `ErrBinaryNotFound`, `ErrReadyTimeout`, **`ErrProcessExited`** (added with P3 fix — returned when `WaitHealthy` / `waitForLogToken` detect the spawned backend has died during the wait).

### Readiness

`WaitReady` polls `GET /health` with 100 ms → 1 s capped backoff, up to `HealthCheckTimeout` (default 180 s). For unsloth, the readiness wait additionally captures the `sk-unsloth-…` auth token from the log.

> **P3 fix (`f0152ef`):** both `WaitHealthy` and `waitForLogToken` now check `procutil.Alive(pid)` on every poll. A backend that crashes right after spawn used to burn the full 180–360 s health-wait window while holding `swapMu`, stalling every other load/unload/swap. The new `ErrProcessExited` sentinel fail-fasts the wait and frees `swapMu`.

### Liveness & recovery

A 5 s ticker (`procutil.Alive`) marks dead PIDs as `Crashed` and rewrites the registry **outside `m.mu`** (5 documented `saveRegistry` callsites — the count is a contract). Long-lived goroutines install `defer recover()` outside Bubble Tea's net.

Boot `Reconcile` validates `instances.json` against `/proc/<pid>/comm` + cmdline to drop recycled PIDs (handles compound commands like `python -m sglang.launch_server`).

### Watchdog

Policy-driven restart (`none` / `on-failure` / `always`) with `BackoffSeconds` × count capped at 30 s, `MaxRestarts`. Emits `tea.Cmd`s on the Bubble Tea loop.

### Model swap

`httpproxy.ensureLoaded` (under `swapMu`): same target → no-op fast path; different → kill old + launch new + `WaitReady` + record swap metrics. Concurrent same-target callers collapse to one launch.

### Monitoring

`monitor.Subscribe` runs 6 goroutines per backend (log tailer via fsnotify, slots/GPU pollers, metrics aggregator, pumps + ring buffer) over a 256-buffered channel that **drops on backpressure**. The TUI Server tab is a **passive observer** — `pm.RefreshFromDisk` (read-only), never `Reconcile`.

### Proxy supervision

`proxysupervisor` spawns the proxy as its own detached `serve` process, reconciles `proxy-state.json` (PID + port-open + `/_status` probe), and exposes `EnsureRunning` / `Load` / `Unload` to the TUI, CLI, and benchmark engine.

## SNDR (Genesis) backend

Full runbook at `docs/sndr-backend.md`. TL;DR:

- SNDR is a runtime patch overlay for vLLM (~321 in-memory patches, ~23 families), registered as a catalog variant of kind `vllm` (id `sndr-vllm`) — not a new `BackendKind`
- Strict vLLM pin (`0.23.1rc1.dev424+g3f5a1e173`, rollback `dev301`, ≤2-pin policy)
- Setup: `./scripts/setup-sndr-backend.sh` (idempotent; honors `SNDR_VLLM_PIN`, `SNDR_WHEEL_INDEX`, `SNDR_REF`, `PYTHON_BIN`)
- **Aged-out pin warning:** the default `https://wheels.vllm.ai/nightly` rotates; SNDR's pin is gone. Set `SNDR_WHEEL_INDEX` to the per-commit wheel URL
- **Two load-bearing env vars not in the model YAML `patches:` block** (must be in every SNDR profile's `launch.env`):
  - `GENESIS_ENFORCE_VERSION_RANGE=1` (mandatory — turns on the version gate)
  - Plus dual-3090 headroom: `gpu-memory-utilization 0.84`, `GENESIS_ENABLE_PN95_TIER_AWARE_CACHE=0`, `NCCL_P2P_DISABLE=1`, `disable-custom-all-reduce`, `distributed-executor-backend mp`
- Set `served-model-name` to the profile id so the proxy's implicit swap passes vLLM's model-name validation
- **Never** install the SNDR plugin into a stock vllm venv — its `vllm.general_plugins` entry point auto-patches every vLLM process of the environment

## Testing

- **No mock framework** — hand-rolled doubles (`stubStore`, `fakeProxy`, `fakeProcMgr`, etc.) — ~190 `_test.go` files
- **Helpers:**
  - `fakeBinary(t)` → `testdata/fake-llama-server.sh`
  - `freePort(t)`
  - `newTestManager(t)` (processmgr), `newManager(t)` (backendschema)
  - `drainCmd` (`tea.BatchMsg` pump)
  - `testdata/fake-llama-help.sh` symlinked as `llama-server` for `ExecParser` tests
- **Golden fixtures** — `help-v9761.txt` + `.golden.json` (`//go:embed`). **Never hand-edit; regenerate with `-update`**
- **`docs/profile-schema.json` must stay in sync** with `domain.Profile`
- `help_test.go::TestHelpMarkdownCoversAllHints` — drift-detector for help ↔ hints
- `teatest` reserved for `root_test.go` and slow `pages/profiles_test.go`
- **Coverage is strong on:** `httpproxy`, `proxysupervisor`, `pages/server_test.go`
- **Coverage is thin on:** `bootstrap`, `configweb`, benchmark runners

## Troubleshooting

Common issues and fixes:

| Symptom | Fix |
|---------|-----|
| `llama-server not found` | `which llama-server`; if missing, build [llama.cpp](https://github.com/ggerganov/llama.cpp) with server support, or register the binary in the Backends tab (`n`) |
| `port already in use` (proxy) | Change `[serve].port` in `config.toml` or `serve --port` (instance ports are ephemeral and assigned by `processmgr`; a profile's `port` arg is ignored and stripped) |
| `model file not found` | Verify the path in the profile editor; ensure the file is readable; add the directory to `search_paths` |
| Background instance not recovered | Check `~/.local/state/model-loader/instances.json`; verify with `ps aux \| grep llama-server`; if it crashed, clear it from the Server tab |
| Foreground instance already running | Only one foreground instance is allowed; kill via Server tab `K` |
| Health check timeout | The instance may need more than 180 s to load the model; check Server-tab logs; verify model file integrity |
| Models tab is empty | Verify `search_paths`; ensure directories are readable; wait for the async scan |
| Validation errors when saving | Use the Essentials tab for curated fields; check the backends tab for missing/outdated schema (`R` to refresh, `e` to edit) |
| VRAM still allocated after stopping | VRAM is held by the running backend process. To release: `curl -sX POST http://127.0.0.1:4321/_admin/unload` (use `?force=true` to skip drain), or Server tab → `K`. The proxy does not call `nvidia-smi --gpu-reset` (requires root) |
| Clipboard not working | Ensure `$DISPLAY` is set (X11) or `WAYLAND_DISPLAY` (Wayland) for `atotto/clipboard`; check `xclip` / `wl-copy` are installed |

For SNDR-specific issues see `docs/sndr-backend.md`. For benchmark-specific issues see `docs/BENCHMARK.md`, `docs/swe-bench-pro.md`, `docs/deep-swe.md`.

## Repo hygiene

- **Defect tracker** — `BUGS.md` (L/B/D/T series) is the single source of truth
- **No git submodules** — `backends/*` are vendored/cloned source trees, gitignored
- **`ARCHITECTURE.md` is auto-generated and stale** — do not trust its body; this OpenWiki and `AGENTS.md` are current
- **No localization** — all UI text and schema metadata is English only

## Recent fixes

- **`f0152ef` — liveness check on health-wait + download queue fixes** (BUGS P3, DL1, DL2):
  - `processmgr.WaitHealthy` / `waitForLogToken` now check `procutil.Alive(pid)` per poll and fail-fast with new `ErrProcessExited` sentinel — was burning full health-wait window while holding `swapMu`
  - `downloadmgr` `--snapshot` now checks the destination file (not directory) for existence, allowing all files of a multi-file repo to be fetched
  - `downloadmgr` Reconcile now re-queues orphaned items (bounded by free-slot count); cross-process double-spawn race closed via exclusive per-download claim file (`fsx.WriteJSONExclusive`) before spawning

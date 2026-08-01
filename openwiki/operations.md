---
type: Runbook
title: Operations and configuration
description: Operator runbook for model-loader — runtime and binary requirements, the TOML configuration surface, where state lives on disk, and the troubleshooting guidance for common failures (missing binaries, schema generation, port conflicts, instance recovery).
tags: [operations, runbook, config, troubleshooting, dependencies]
---

# Operations

## Runtime requirements

- **Go** ≥ 1.26.2 (toolchain pinned in `go.mod`). **No CGO**. No git submodules; `backends/*` is vendored source under `.gitignore` (manage each as its own upstream checkout).
- **`llama-server`** in `PATH` (used for auto-generating backend schemas). `llamabin.DefaultName` is the default backend; PATH lookup falls back if no `default_backend_id` is in config.
- **(Optional)** `nvidia-smi` for GPU monitoring (`internal/service/monitor/gpu.go`).

## Binary dependencies

Backends are not hard-coded — operators register each via `model-loader backend add --executable <path> --kind <kind>`.

| Binary | Where referenced |
|--------|------------------|
| `python3` | `llamabin.ResolveCommandWithPythonFallback`; backend builders in `processmgr/launch.go::makeCommand`. Compound commands (`python -m sglang.launch_server`) are supported. |
| `nvidia-smi` | GPU monitoring (optional). |
| `bwrap` | `benchmark/codegenbench.go` sandbox; falls back to bare python. |
| `docker` | `terminalbench.go`, `deepswe.go`, `swebenchpro.go` (required by agentic modes). |
| `tb` (Terminal-Bench CLI) | `tbDefaultCmd` in `terminalbench.go`. |
| `pier` (datacurve-ai/pier) | `deepDefaultCmd` in `deepswe.go`. |
| `vllm`, `sglang`, `dflash_server`, `unsloth`, `beellama`, `buun`, `tabbyapi` | User-registered; never hard-coded. |

## Configuration

`~/.config/model-loader/config.toml` is Viper-loaded (`internal/config/config.go`). First run creates a default.

```toml
[paths]
profiles_dir = "~/.config/model-loader/profiles"
backends_dir = "~/.config/model-loader/backends"
log_dir = "~/.local/state/model-loader/logs"
state_dir = "~/.local/state/model-loader"

[models]
search_paths = ["~/.lmstudio/models", "~/models"]

[ui]
default_tab = "profiles"

[serve]
host = "127.0.0.1"
port = 4321
health_check_timeout_sec = 360   # serve default is larger than the proxy's 180s
```

The `[benchmark]` table configures the evaluation engine (max_tokens, limit, timeout, judge, and per-agentic-mode sub-tables `terminalbench`/`swebenchpro`/`deepswe`). See [Benchmark Engine](benchmark.md) and `docs/BENCHMARK.md`. Full configuration reference: `docs/config.md`.

## State on disk

See [Architecture](architecture.md) for the on-disk state layout. Key locations: `~/.config/model-loader/` holds config, profiles, and the backend catalog + schemas; `~/.local/state/model-loader/` holds the single-instance lock, the instance registry, exit history, proxy state, logs, metrics, benchmark runs, and downloads.

## Development commands

The Makefile has exactly three targets:

```bash
make build       # go build -ldflags "<version + build_date>" -o bin/model-loader ./cmd/model-loader
make install     # GOBIN=~/.local/bin go install ./cmd/model-loader
make tests       # go test ./...
```

Frequent `go` invocations:

```bash
go test ./internal/service/llamahelp -update          # regenerate llama-server --help golden
go run ./cmd/regenerate-schemas                       # re-parse every backend --help
go run ./cmd/scripts/print_args.go <profile-id>       # inspect resolved exe + args
./bin/model-loader serve                               # headless proxy daemon (no flock)
```

When touching a `*help` package, re-run `go run ./cmd/regenerate-schemas`. The root golden pair is `testdata/help-v10152.{txt,golden.json}`; a duplicate embed copy at `internal/service/backendschema/testdata/` must be kept byte-identical by hand.

## Troubleshooting

- **`llama-server` not found** — ensure it is compiled and in `PATH`. Backends tab (`4`) → `n` registers a custom binary location as a backend.
- **Port in use** — edit the profile and change the port number.
- **Model not found** — verify the model path in the profile or update `search_paths` in `config.toml`.
- **Instance not recovering** — check that `instances.json` exists in the state directory; `Reconcile` runs at boot for state owners.
- **Backend schema missing** — each backend needs a validation schema. Add a backend via the Backends tab (`n`) to auto-generate one from `--help`, or place a manually edited schema in the backends directory.
- **Health-check timeout on large models** — bump `[serve].health_check_timeout_sec` (serve default is 360s vs the proxy's 180s).
- **Python backend logs arrive in bursts** — `PYTHONUNBUFFERED=1` is injected at spawn; if you see buffering, confirm the env is reaching the process.

Full troubleshooting guide: `docs/troubleshooting.md`. Agentic-mode setup: `docs/swe-bench-pro.md`, `docs/deep-swe.md`, `docs/BENCHMARK.md`.

## Direct dependencies

- CLI: `spf13/cobra v1.10.2`, `spf13/viper v1.20.0-alpha-6`.
- TUI: `charmbracelet/{bubbletea v1.3.10, bubbles v1.0.0, lipgloss, huh v1.0.0, glamour v1.0.0, x/ansi, x/exp/teatest}`.
- Other: `fsnotify/fsnotify v1.7.0`, `atotto/clipboard v0.1.4`, `mattn/go-runewidth v0.0.19`, `tiktoken-go/tokenizer v0.7.0`.

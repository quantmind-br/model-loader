---
type: Overview
title: model-loader quickstart
description: Entry point for the model-loader code wiki. A Go terminal UI and headless CLI that manages local LLM inference servers across eleven backends, fronted by a single multi-API HTTP proxy that hot-swaps the active model on demand.
tags: [quickstart, overview, index]
---

# model-loader

**model-loader** is a Go 1.26 terminal UI **and** headless CLI for running local LLM inference servers on a single workstation (reference rig: 2× RTX 3090). It manages launch *profiles*, supervises backend *processes*, and fronts them with one OpenAI-shaped HTTP proxy that hot-swaps the active model on demand.

- **Module:** `github.com/quantmind-br/model-loader`
- **For:** a single operator curating tuned launch configs for one GPU. Loopback-only proxy, no auth, single-instance lock.
- **Backends (11 `BackendKind`s):** `llama-server`, `vllm`, `sglang`, `dflash`, `buun-llama-cpp`, `beellama-cpp`, `ik-llama-cpp`, `unsloth`, `tabby`, `lmstudio`, `freetoken`. Operators register each backend (binary + kind) via `model-loader backend add`.
- **Three surfaces:** the 5-tab Bubble Tea TUI (default), headless `serve` proxy daemon, and a full Cobra CLI mirroring every TUI action.

## What this wiki covers

| Page | What it explains |
|------|------------------|
| [Architecture](architecture.md) | Top-level layout, dependency-injection bootstrap, on-disk state, and the end-to-end request flow. |
| [Data Model](data-model.md) | Domain types — `Profile`, `Backend`, instances, flag schemas — and their persistence. |
| [HTTP Proxy](http-proxy.md) | The multi-API proxy: OpenAI native plus Anthropic, Responses, and Gemini translation, model hot-swap, and admin endpoints. |
| [Process Manager](process-manager.md) | Process lifecycle, crash recovery, the flock-guarded registry, and the restart engine. |
| [Backend Schema](backend-schema.md) | How per-backend validation schemas are generated from `--help` and merged with curated metadata. |
| [Benchmark Engine](benchmark.md) | The 12-mode evaluation engine across five categories, including agentic harness integration. |
| [Surfaces](surfaces.md) | The TUI tabs, the schema-driven `configweb` editor, and the CLI subcommand tree. |
| [Operations](operations.md) | Configuration, runtime requirements, binary dependencies, systemd notify/watchdog lifecycle, Cloudflare external exposure, and troubleshooting runbook. |
| [Testing & QA](testing.md) | Test conventions, golden fixtures, regression-test linkage to `BUGS.md`, and the quality gate. |

## Task routing for developers and agents

| Change area / Intent | Relevant Wiki Page | Exact Source Entry Points | Important Symbols / Types | Focused Tests | Minimal Validation Command |
|---|---|---|---|---|---|
| Add or extend a backend family | [Backend Schema](backend-schema.md), [Process Manager](process-manager.md) | `internal/domain/backend.go`, `internal/service/backendschema/register.go`, `internal/service/processmgr/launch.go`, `internal/service/processmgr/readiness.go` | `BackendKind`, `Generator`, `pythonBackends`, `WaitReady` | `internal/domain/backend_kind_test.go`, `internal/service/backendschema/register_test.go`, `internal/service/processmgr/readiness_test.go` | `go test ./internal/domain ./internal/service/backendschema ./internal/service/processmgr -run 'Test(BackendKind\|Register\|WaitReady)'` |
| Update backend flag schemas or help goldens | [Backend Schema](backend-schema.md), [Testing & QA](testing.md) | `internal/service/llamahelp/parser.go`, `internal/service/backendschema/manager.go`, `testdata/help-v10686.txt` | `ParseHelp`, `EmbeddedSchema`, `Manager.RefreshSchema` | `internal/service/llamahelp/parser_test.go`, `internal/service/backendschema/generator_test.go` | `go test ./internal/service/llamahelp ./internal/service/backendschema` |
| HTTP proxy routes, translations, or hot-swap | [HTTP Proxy](http-proxy.md) | `internal/service/httpproxy/server.go`, `internal/service/httpproxy/handler.go`, `internal/service/httpproxy/*_handlers.go` | `Server`, `ensureLoaded`, `swapMu`, `translateAnthropicRequest` | `internal/service/httpproxy/swap_test.go`, `internal/service/httpproxy/*_handlers_test.go` | `go test ./internal/service/httpproxy` |
| Systemd notify, watchdog, or proxy supervisor | [Operations](operations.md), [HTTP Proxy](http-proxy.md) | `internal/sdnotify/sdnotify.go`, `internal/cli/serve_notify.go`, `internal/service/proxysupervisor/strategy_systemd.go` | `sdnotify.Ready`, `sdnotify.Watchdog`, `serveReadiness`, `Supervisor.startSystemd` | `internal/sdnotify/sdnotify_test.go`, `internal/cli/serve_notify_test.go`, `internal/service/proxysupervisor/systemd_test.go` | `go test ./internal/sdnotify ./internal/cli ./internal/service/proxysupervisor` |
| Process lifecycle, registry deltas, or crash recovery | [Process Manager](process-manager.md) | `internal/service/processmgr/{manager,launch,registry,liveness,recover}.go` | `fsManager`, `mutateRegistry`, `StartTicks`, `SameProcess`, `maybeScheduleRestart` | `internal/service/processmgr/manager_test.go`, `internal/service/processmgr/lifecycle_audit_test.go` | `go test ./internal/service/processmgr` |
| CLI commands and subcommands | [Surfaces](surfaces.md) | `internal/cli/{profile,backend,instance,model,benchmark,serve}.go` | `profileCmd`, `backendCmd`, `instanceCmd`, `modelCmd`, `TUIRunner` | `internal/cli/{profile,backend,instance,model}_test.go` | `go test ./internal/cli` |
| Benchmark modes, evaluation, or harnesses | [Benchmark Engine](benchmark.md) | `internal/service/benchmark/{runner,handler,handlers,scorer}.go` | `Runner.Run`, `modeOrder`, `modeHandler`, `llmGrader` | `internal/service/benchmark/handler_test.go`, `internal/service/benchmark/grader_test.go` | `go test ./internal/service/benchmark` |
| Workstation calibration & throughput probes | [Benchmark Engine](benchmark.md), [Operations](operations.md) | `internal/service/benchmark/llamabench_probe.go`, `QWEN38_27B_AUDIT_REPORT.md` | `runLlamaBench`, `CompletionTokens`, `PromptProcessingTPS` | `internal/service/benchmark/llamabench_test.go` | `go test ./internal/service/benchmark -run TestRunLlamaBench` |

## Features

- **Profile Editor** — create and manage launch profiles through a schema-driven web editor (curated Essentials plus an advanced flag editor), one backend per profile.
- **Launch & Hot-swap** — load a profile with `Enter` on the Profiles tab; all inference flows through an OpenAI-shaped proxy that hot-swaps the active backend on demand.
- **Monitor** — real-time monitoring of running instances: logs, health status, slot usage, GPU metrics, and throughput.
- **Multi-instance** — run multiple backend instances concurrently, each with its own PID and port. Background instances survive TUI exit and are recovered on restart.
- **Backend Catalog** — manage multiple backends (forks/versions plus other server kinds), each with per-backend validation schemas auto-generated from `--help`.
- **Multi-API HTTP Proxy** — headless `model-loader serve` exposes one proxy that speaks **four** client APIs: OpenAI (native reverse-proxy), **Anthropic Messages** (`/v1/messages`), **OpenAI Responses** (`/v1/responses`), and **Gemini** (`/v1beta/models/*`), all routed to the loaded backend's chat completions with implicit per-request model swap.
- **Benchmark** — evaluate profiles with a built-in engine spanning 12 modes across 5 categories: Quality (LLM-judge SWE-bench Lite, GSM8K, HumanEval, RAGAS, summarization), Speed (`llama-bench`), Robustness (long-context needle, instruction-following), Knowledge (MMLU), and **Agentic** (Terminal-Bench, SWE-bench Pro, DeepSWE — each shells out to an external harness + Docker).
- **Hugging Face Integration** — search the Hub and download `.gguf` files with queued, progress-tracked downloads.

## Quick start

```bash
# Build
make build

# Run the TUI (creates ~/.config/model-loader/config.toml on first run)
./bin/model-loader

# Headless proxy daemon (no single-instance lock; coexists with the TUI)
./bin/model-loader serve
```

On first run a default `config.toml` is created at `~/.config/model-loader/config.toml`. From the TUI: `Enter` on a profile in the Profiles tab (tab `1`) loads it through the proxy; tab `2` monitors running instances; tab `3` browses models; tab `4` manages backends; tab `5` runs benchmarks.

## Global keybindings

| Key | Action |
|-----|--------|
| `1`–`5` | Profiles / Server / Models / Backends / Benchmark tabs |
| `Tab` / `Shift+Tab` | Next / previous tab |
| `q` | Quit |
| `?` | Show help |

Per-tab keybindings are documented in [Surfaces](surfaces.md) and the [README](../README.md). Convention: lowercase runes are navigation (`k`/`j`/`g`/`G` for lists); UPPERCASE runes are destructive and always confirm (`X` delete, `K` kill/unload, `R` refresh, `I` import, `E` export).

## Development

```bash
make build       # go build -ldflags "<version + build_date>" -o bin/model-loader ./cmd/model-loader
make install     # GOBIN=~/.local/bin go install ./cmd/model-loader
make tests       # go test ./...
```

No formatter or linter Make targets exist. GitHub Actions and the local quality
gate run `go build ./...`, `go test ./...`, and `go vet ./...` — see
[Testing & QA](testing.md).

## Defect tracking

New defects are tracked in GitHub Issues. `BUGS.md` explains the legacy IDs
retained by regression-test comments.

Operator scripts and dataset-curation tools live under `scripts/` and `tools/`;
they write the committed benchmark datasets under
`internal/service/benchmark/data/`.

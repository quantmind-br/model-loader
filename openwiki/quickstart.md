# model-loader — OpenWiki

A Go 1.26 terminal UI (and headless HTTP proxy) for running local LLM inference servers on a single workstation. It manages launch **profiles**, supervises backend **processes**, and fronts them with a single **multi-API reverse proxy** that hot-swaps the active model on demand.

The reference rig is a 24 GiB single-GPU box (e.g. RTX 3090); the project is a single-operator power-user tool — no auth, no multi-tenant, loopback-only by default.

## What it does in one paragraph

Author a profile (model path + backend + flags) through a schema-driven web editor → press `Enter` to load it through the proxy → send OpenAI / Anthropic / OpenAI Responses / Gemini requests at `127.0.0.1:4321` → the proxy hot-swaps the loaded backend based on the request's `"model"` field → monitor logs, slots, GPU, throughput on the Server tab → browse local `.gguf` files or the Hugging Face Hub → run a 12-mode benchmark to compare profiles.

## Quick start

```bash
# Build
make build          # → bin/model-loader

# Run the TUI (no subcommand)
./bin/model-loader

# Run the headless proxy only
./bin/model-loader serve --host 127.0.0.1 --port 4321

# First-run config lands at ~/.config/model-loader/config.toml
```

See [operations/build-and-test](operations.md#build--test) for full build / install / test commands.

## The 5-tab TUI

| # | Tab | Purpose |
|---|-----|---------|
| 1 | **Profiles** | Master-detail list. `Enter` loads a profile through the proxy; `e/n` open the web editor; `d` duplicate, `X` delete, `K` unload, `/` filter |
| 2 | **Server** | ProxyPanel + instances table + Logs/Slots/Metrics/History sub-views. `K` kill, `R` restart, `h` history chart, `s/x` proxy start/stop |
| 3 | **Models** | Library / Downloads / Discover (HF). `R` rescan, `i` info, `/` filter, `s` HF search, `→/g` jump to sizing |
| 4 | **Backends** | Backend catalog (binary + kind). `n` new, `e` edit, `X` delete, `D` set default, `P` probe all |
| 5 | **Benchmark** | Dashboard-centric: mode-focused leaderboard, run wizard, run detail, compare, history timeline |

Full keymap and conventions: [tui](tui.md).

## Surfaces

- **TUI** — `model-loader` (no args) → 5-tab Bubble Tea interface, all flow goes through it
- **Headless proxy** — `model-loader serve` → multi-API reverse proxy only; same proxy runs as a child of the TUI Server tab
- **CLI** — full Cobra tree mirroring TUI actions; never imports `internal/ui`
- **Download worker** — `model-loader download <state>` (hidden, spawned by `downloadmgr`)
- **Schema regen tool** — `go run ./cmd/regenerate-schemas` (rebuilds embedded `--help` schemas)

## Sections

- [Architecture](architecture.md) — module map, service layer, request flow, state layout, cross-subsystem edges
- [TUI](tui.md) — tabs, keybindings, conventions, web editor
- [CLI](cli.md) — Cobra reference (subcommands, flags, exit codes)
- [HTTP proxy](proxy.md) — routes, OpenAI / Anthropic / Responses / Gemini translation, model swap, admin endpoints
- [Backends](backends.md) — catalog, 8 `BackendKind`s, per-kind args, schema generation
- [Profiles](profiles.md) — JSON schema, naming convention, args vs env vs extraArgs
- [Benchmark](benchmark.md) — 12 modes across 5 categories, run/wizard/compare/history
- [Operations](operations.md) — build, config file, state layout, troubleshooting, lifecycle, testing

## Where to start

- New to the codebase: read [architecture](architecture.md) → [tui](tui.md) → [proxy](proxy.md)
- Adding a backend: read [backends](backends.md), then look at `internal/service/llamahelp/`, `internal/service/vllmhelp/`, etc.
- Adding a benchmark mode: read [benchmark](benchmark.md), then look at `internal/service/benchmark/<mode>.go` and `config.go`
- Adding a TUI page: read [tui](tui.md), then look at `internal/ui/pages/profiles.go` (the most complex page)
- Changing the proxy: read [proxy](proxy.md), then look at `internal/service/httpproxy/handler.go` and the per-API translation files
- Debugging: see [operations/troubleshooting](operations.md#troubleshooting) and `BUGS.md`

## Repository facts

- **Module:** `github.com/quantmind-br/model-loader`
- **Go:** 1.26.2, no CGO
- **Maintainer:** single maintainer (quantmind-br); in-repo defect tracker is `BUGS.md`
- **No CI/CD** — quality gate is local `go test ./...` + `make build`
- **`backends/*`** are vendored/cloned source trees, gitignored — manage each as its own upstream checkout
- **`ARCHITECTURE.md`** is auto-generated and stale (cites v9680/244 flags, 24 packages); do not trust its body — see [architecture](architecture.md) for the current tree

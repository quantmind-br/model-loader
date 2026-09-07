---
type: Architecture
title: Surfaces — TUI, web editor, and CLI
description: The three operator surfaces of model-loader. The 5-tab Bubble Tea TUI is the default; a schema-driven configweb editor (also reused as a read-only benchmark viewer) handles profile and backend editing in the browser; and a full Cobra CLI mirrors every TUI action without importing the UI package.
tags: [tui, configweb, cli, surfaces, bubbletea]
---

# Surfaces

model-loader exposes three surfaces sharing the same [bootstrap](architecture.md) and services: the 5-tab Bubble Tea TUI, the schema-driven `configweb` web editor, and the Cobra CLI.

## TUI

The 5-tab TUI (`internal/ui/root.go`) is the default surface. `RootModel` exposes fluent `With*Page`/`WithProcessManager` setters. Tab indices: `TabProfiles=0`, `TabServer=1`, `TabModels=2`, `TabBackends=3`, `TabBenchmark=4`.

### Conventions

- Global rune shortcuts are gated by `activePageCapturesInput()`. Only `ctrl+c` is unconditional. A capturing page implements `InputCapture.IsCapturingInput()`.
- Pages hosting `*huh.Form` must forward non-`KeyMsg` messages so focus and validation complete.
- **lowercase = navigation** (`k`/`j`/`g`/`G` reserved for lists); **UPPERCASE = destructive, always confirm** (`X` delete, `K` kill/unload, `R` refresh, `I` import, `E` export, `D` default, `P` probe, `C` clear).
- Layout: `MinTermWidth` × `MinTermHeight` terminal-too-small guard; stacked layout below 100 cols; truncate-at-edge otherwise.
- Theme = GitHub-Primer adaptive palette; `NO_COLOR` is stripped in `theme.init()` (the repo's only `init()`).
- Pages implement optional interfaces in `internal/ui/contracts.go`: `InputCapture`, `Reloader`, `HintProvider`, `HelpContextProvider`, `Overlayer`, `StatusMessageProvider`, `Cleaner`.

Per-tab keybindings are in the [README](../README.md). Notable destructive actions: `Enter` on Profiles loads a profile through the proxy (hot-swaps the active model); `K` unloads/kills; `e`/`n` edit/create profiles via the web editor; `s`/`x` start/stop the proxy listener on the Server tab; `W` opens the benchmark web viewer.

## configweb — web editor and benchmark viewer

`internal/service/configweb/` is a local HTTP server that serves a schema-driven editor in the browser. It is launched by the TUI when editing a profile (`e`/`n`) or backend, and reused read-only by `benchmark web` / TUI `W` for the benchmark viewer (with an htmx live monitor in `benchview_*.go`).

- **Profile editor** (`handlers.go`, `draft.go`, `viewmodel.go`) — renders curated Essentials plus an advanced flag editor and environment/sizing sections. The draft lifecycle is session-scoped; the TUI reloads the profile list after a save (`Esc` cancels).
- **Backend editor** (`backend_handlers.go`, `backend_draft.go`) — edits the backend catalog entry and its validation schema, including the Customize flow that marks `Source.Customized = true`.
- **Bench viewer** (`benchview_handlers.go`, `benchview_session.go`, `benchview_viewmodel.go`) — read-only run detail, leaderboard, compare, history, transcript; an htmx live activity feed.
- Static assets are embedded via `assets/embed.go` (`//go:embed`).

Editing a profile or backend here flows through the [validation schema](backend-schema.md); edits to flag layout persist as `Presentation` and survive schema refresh via `ReconcilePresentation`.

## CLI

`internal/cli/` is a Cobra tree mirroring every TUI action. **It must not import `internal/ui`** — the `cli.TUIRunner = runTUI` callback breaks the cycle (do not break this).

### Conventions

- Every leaf command calls `app.Bootstrap` and `defer svc.Close()`.
- `&ExitError{Code: N}` for non-1 exits; unwrapped by `Execute()`.
- Output via `cmd.OutOrStdout()` / `ErrOrStderr()` — never `fmt.Print`.
- `--json` honored by every table command.

### Subcommand groups

| Group | Commands |
|-------|----------|
| `profile` | `list`, `show`, `create`, `edit`, `delete`, `duplicate`, `rename`, `pin`/`unpin`, export/import bundle, `validate` |
| `backend` | `add`, `delete`, `list`, `show`, `probe`, `set-default`, `schema show/refresh/apply` |
| `instance` | `list`, `show`, `history`, `start`, `stop`, `restart`, `logs`, `metrics` |
| `model` | `list`, `info`, `search`, `download`, `downloads`, `cancel`, `resume` |
| `benchmark` | `run`, `list`, `compare`, `history`, `show`, `transcript`, `export`, `delete`, `web` |
| `serve` | headless proxy daemon (no flock) |

Subcommand conventions: `profile` commands use `create`/`edit` (no `add`, and pinning via `pin`/`unpin` rather than `set-default`); `instance` commands provide `logs`/`metrics` directly (no `observe` command); `model` uses `list`/`search`/`download` (no `scan` command); and `backend` provides `add`/`delete`/`list`/`show`/`probe`/`set-default` alongside the `schema` subgroup (`show`, `refresh`, `apply`).

`bootstrap_lock.go` is the shared flock-acquisition helper for one-shot mutating commands (`instance start/stop/restart`, `benchmark run`). See [Architecture](architecture.md) for the owner/observer split. Benchmark mode parsing supports legacy aliases (`long-context`→`longctx`, `llamabench`/`throughput`→`llama-bench`).

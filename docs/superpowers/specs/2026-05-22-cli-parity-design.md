# CLI Parity — Design Spec

**Date:** 2026-05-22
**Status:** Draft (awaiting user review)
**Topic:** Expose every TUI capability through a non-interactive command-line interface.

## Goal

Make all functionality currently reachable only through the 5-tab bubbletea TUI
(Profiles, Models, Server, Backends, Benchmark) reachable through scriptable CLI
subcommands. The TUI remains the default when `model-loader` is invoked with no
subcommand.

### Driving use cases (all in scope)

1. **Automation / scripting** — one-shot commands, parseable output (`--json`), reliable exit codes.
2. **Headless / SSH servers** — manage profiles and instances on machines with no GUI.
3. **CI/CD** — trigger benchmarks, validate profiles, bring instances up/down inside pipelines.
4. **Interactive convenience** — quick terminal shortcuts without launching the full TUI.

## Non-goals

- Replacing the TUI. The TUI stays the default entry point.
- A CLI equivalent of the *visual* web schema/profile editor (`configweb`). CLI
  editing is done via flags + JSON (stdin/file), not by opening a browser.
- Full simultaneous coexistence of **instance/proxy lifecycle** mutations with a
  running TUI (see Concurrency Model — this is explicitly deferred).

## Architecture

### Decision: cobra command tree in `internal/cli`, bootstrap extracted to `internal/app`

The service layer (`internal/service/*`) is already free of TUI dependencies and is
wired by `bootstrap()` (currently in `package main` at
`cmd/model-loader/bootstrap.go`), already reused by `runServe` and `runBenchmark`.

**Refactor:**

1. Extract `bootstrap()` and its `bootServices` container into a new exported
   package `internal/app` (e.g. `app.Bootstrap(level string) (*app.Services, func(), error)`).
   Update `runTUI`, `runServe`, `runBenchmark`, `runImport` to consume it.
2. Create `internal/cli/` holding the cobra tree:
   - `root.go` — root command, global flags, `Execute()`.
   - `profile.go`, `instance.go`, `model.go`, `backend.go`, `benchmark.go`, `proxy.go` — one file per domain.
   - `output.go` — shared table/JSON rendering helpers.
   - `lock.go` helper for lifecycle commands (reuses the existing flock helper).
3. `main.go` becomes thin:
   - no args, or first arg is `tui` → `runTUI()`.
   - otherwise → `cli.Execute()`.
   - `download` worker subcommand stays a hidden cobra command (it is an internal
     subprocess contract, not user-facing).

**Why:** clean separation, commands are unit-testable outside `package main`
(critical for the CI/scripting use cases), and `package main` does not bloat to ~30 files.

### Dependency framework

Adopt **cobra** (`github.com/spf13/cobra`). Migrate the existing hand-rolled `flag`
dispatch (`serve`, `import`, `download`, `benchmark`) into the cobra tree, preserving
their current flags and behavior.

### Command acquisition pattern

Each cobra command's `RunE` calls `app.Bootstrap()` to obtain `*app.Services`, runs,
then calls the returned cleanup. Read-only commands take no lock. Lifecycle commands
(instance/proxy start/stop/restart) acquire the single-instance flock first (see below).

## Concurrency Model (pragmatic, agreed)

The on-disk stores use atomic writes (`fsx.WriteJSONAtomic`: temp + rename) but **no
cross-process locking**. The TUI's `flock` on `<stateDir>/model-loader.lock` only
prevents a second TUI. Key findings:

- `instances.json` / `proxy-state.json`: the TUI holds an **authoritative in-memory
  snapshot** and rewrites the whole file on each change. A CLI write would be clobbered
  by the TUI's next write regardless of file locks. True coexistence would require
  read-modify-merge under lock in `processmgr`/`proxysupervisor` plus live TUI reload —
  **out of scope.**
- `profiles/*.json`: one file per profile, atomic writes, and the TUI already reloads
  the list on tab focus / `r`. Coexistence is cheap.
- `downloads (dl-*.json)`: already safe — single writer per download (the worker).

**Adopted model:**

| Command class | Behavior |
|---|---|
| Read-only / status (all `list`, `show`, `metrics`, `logs`, `search`, `info`, `scan`, `probe`) | Always run, no lock. Atomic writes guarantee consistent reads. |
| `profile` / `model` / `backend` mutations | Coexist with the TUI. Add a per-file advisory lock (flock on a sibling `.lock`) around any read-modify-write of a single file; rely on the TUI's existing reload for visibility. |
| `instance start/stop/restart`, `proxy start/stop` | Acquire the existing single-instance flock. If the TUI (or another holder) holds it, fail fast with a clear message ("TUI is running — close it first, or run on a headless host"). On a headless host the CLI takes the lock and works normally. |

This is corruption-free, fully covers headless/CI, and degrades gracefully on the desktop.
Full lifecycle coexistence is a documented possible future phase.

## Output & UX Conventions

- **Default output:** human-readable tables/text on stdout.
- **`--json`:** machine-readable JSON on stdout, available on every command that produces output.
- **Errors:** written to stderr; non-zero exit code on failure. Validation failures
  print the validator's messages and exit non-zero.
- **`--watch` + `--interval <dur>`:** for streaming-like data (`instance list`,
  `instance metrics`, `model downloads`). Default is one-shot snapshot then exit;
  `--watch` polls and redraws until Ctrl-C. `instance logs --follow` tails.
- **Editing inputs:** create/edit commands read a full profile/schema JSON from
  `--file <path>` or `-` (stdin); individual flags override fields from that JSON.
  Profiles validated against `docs/profile-schema.json` semantics (the canonical
  shape) and the `validator` before persisting.
- **ID resolution:** profiles addressed by id or name where unambiguous; instances by
  pid or short id.
- **Global flags:** `--json`, `--config <path>`, `--log-level <level>`, `--no-color`.

## Command Catalog

Root: `model-loader` (no subcommand → TUI). Existing `serve` retained (foreground
headless proxy). `download` worker retained as a hidden subcommand.

### `profile` (Profiles tab)

| Command | Action |
|---|---|
| `profile list` | list profiles (table / `--json`) |
| `profile show <id>` | print canonical profile JSON / details |
| `profile create` | flags (`--name --backend --model --arg k=v --extra-arg k=v --env k=v`) and/or `--file f.json`/stdin |
| `profile edit <id>` | same flags + JSON stdin/file (flags override JSON) |
| `profile delete <id>` | delete |
| `profile duplicate <id> [newid]` | duplicate |
| `profile rename <id> <newname>` | rename |
| `profile pin <id>` / `profile unpin <id>` | pin/unpin |
| `profile export [ids...] -o bundle.json` | export bundle |
| `profile import <file> --mode merge\|overwrite\|rename` | import bundle (canonical; top-level `import` becomes a deprecated alias) |
| `profile validate <id>` | run validator, report problems |

### `instance` (Server tab + launch/kill) — start/stop/restart acquire the flock

| Command | Action |
|---|---|
| `instance list` | running instances (`--watch`) |
| `instance start <profile> [--background\|--foreground]` | launch an instance |
| `instance stop <pid\|id>` | kill |
| `instance restart <pid\|id>` | restart |
| `instance logs <pid\|id> [--follow]` | tail logs |
| `instance metrics <pid\|id> [--watch]` | GPU/CPU/tokens-per-second metrics |
| `instance show <pid\|id>` | resolved backend, port, health |
| `instance history` | exited instances |

### `model` (Models tab)

| Command | Action |
|---|---|
| `model scan [paths...]` | scan local dirs for GGUF |
| `model list` | discovered local models |
| `model search <query> [--limit N]` | HF Hub search |
| `model info <repo-id>` | HF repo info / file listing |
| `model download <repo-id> <file...>` | start download (spawns worker) |
| `model downloads [--watch]` | list downloads + progress |
| `model download cancel <id>` / `model download resume <id>` | control a download |
| `model adapters ...` | LoRA adapter management (mirror of `models_adapters.go`) |

### `backend` (Backends tab) — schema edits set `Source.Editable`

| Command | Action |
|---|---|
| `backend list` | catalog |
| `backend show <name>` | definition + schema |
| `backend probe [name]` | test availability |
| `backend schema show <name>` | emit schema JSON |
| `backend schema edit <name>` | edit schema via JSON stdin/file; sets `Source.Editable = true` so `RefreshSchema` preserves it |
| `backend schema refresh <name>` | re-parse `--help` / regenerate |

### `benchmark` (existing — realigned under cobra)

Preserve current flags/behavior (`--profile --mode --list --compare --transcript --json --min-solve`),
re-expressed as subcommands: `benchmark run`, `benchmark list`, `benchmark compare <a> <b>`.

### `proxy` + `serve`

- `serve` — retained: foreground headless proxy run (existing behavior/flags `--host --port --log-level`).
- `proxy start` / `proxy stop` / `proxy status` — supervised background proxy mirroring
  the Server tab's proxy control (`proxysupervisor`); start/stop are lifecycle commands
  and acquire the flock.

## Component Boundaries

- `internal/app` — bootstrap/wiring; one job: build `*app.Services` + cleanup. Depended on by TUI, CLI, serve, benchmark worker.
- `internal/cli` — cobra tree; depends on `internal/app` and the service interfaces. No TUI imports. Each domain file is independently testable.
- Service packages — unchanged except where the per-file advisory lock is added to
  `profilestore`/`backendcatalog` write paths (or to `fsx`) for the profile/backend
  coexistence guarantee.

## Error Handling

- Bootstrap failure → exit non-zero, message to stderr.
- Lifecycle command blocked by held flock → exit non-zero with a clear "TUI running" message.
- Validation failure on create/edit → print validator problems, exit non-zero, do not persist.
- Unknown id / pid → exit non-zero with "not found".
- `--json` errors: still human-readable on stderr; stdout stays empty or emits a JSON error object (decide consistently in `output.go`).

## Testing Strategy

- **Unit tests per domain command** in `internal/cli` using in-memory/temp-dir
  `*app.Services` (temp state dir, temp profiles dir). Assert table and `--json` output,
  exit codes, and that mutations hit the right store methods.
- **Concurrency test:** a profile `edit` while a simulated TUI reload runs — assert no
  data loss with the per-file lock.
- **Lifecycle lock test:** `instance start` fails fast when the flock is already held.
- **Golden output** for representative `--json` payloads where stable.
- Existing benchmark/serve/import behavior preserved — keep their current tests green
  after the cobra migration.

## Phased Implementation Plan (one spec, phased delivery)

1. **Phase 1 — Foundation:** extract `bootstrap` → `internal/app`; add cobra; wire
   `root.go` + global flags + `output.go`; migrate `serve`/`import`/`download`/`benchmark`
   onto cobra (behavior preserved); thin `main.go`. Add the lifecycle flock helper.
2. **Phase 2 — Core (`profile` + `instance`):** full profile CRUD + import/export/validate;
   instance list/start/stop/restart/logs/metrics/show/history; per-file lock for profiles;
   flock for instance lifecycle.
3. **Phase 3 — `model`:** scan/list/search/info/download/downloads/cancel/resume/adapters.
4. **Phase 4 — `backend`:** list/show/probe + `schema show/edit/refresh` (Editable flag).
5. **Phase 5 — `proxy` + polish:** `proxy start/stop/status`; `--watch` polish; shell
   completions; docs; deprecate top-level `import` in favor of `profile import`.

## Open Items / Risks

- Confirm the exact `fsx` helper to extend for the per-file advisory lock (vs. adding
  locking inside `profilestore`/`backendcatalog`).
- `model adapters` surface needs a closer read of `models_adapters.go` during Phase 3
  to enumerate exact subcommands.
- Decide the `--json` error-object shape once, in `output.go`, and apply uniformly.

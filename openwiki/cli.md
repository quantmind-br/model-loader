# CLI

`internal/cli/` exposes a Cobra command tree mirroring the TUI's actions. It never imports `internal/ui`; the TUI is invoked by `cmd/model-loader/main.go` as a `TUIRunner` callback.

## Conventions

- Persistent flags everywhere: `--log-level` (also `$MODEL_LOADER_LOG_LEVEL` / config), `--json`
- Cobra provides `--version` (default `dev`) and `-h/--help`
- Reference resolution is uniform: **exact id → exact name → unique prefix** (ambiguous → error listing ≤10 candidates)
- Exit codes: `0` ok · `1` generic/lookup/IO · `2` `profile validate` blocking errors and `benchmark run --min-solve` gate
- Write paths take the single-instance flock via `bootstrapWithLock`; read-only commands skip it
- `&ExitError{Code:N}` for non-1 exits (unwrapped by `Execute()`)
- Every leaf command calls `app.Bootstrap` and defers `svc.Close()`
- Output through `cmd.OutOrStdout()` / `ErrOrStderr()` (never `fmt.Print`)
- `--json` honored by all table commands

## Subcommand reference

### Top-level

| Subcommand | Purpose | Key flags |
|------------|---------|-----------|
| (no args) | Launch TUI | — |
| `serve` | Headless proxy (foreground) | `--host`, `--port` |
| `import <path>` *(deprecated → `profile import`)* | Back-compat bundle import | `--mode merge\|overwrite\|rename` |
| `download <state-path>` *(hidden worker)* | Internal download worker (spawned by `downloadmgr`) | — (`DisableFlagParsing`) |

### `profile …`

| Subcommand | Purpose | Key flags |
|------------|---------|-----------|
| `profile list` | List profiles | `--json` |
| `profile show <id\|name>` | Show profile / canonical JSON | `--json` |
| `profile create` | Create from flags and/or JSON | `-f/--file` (`-`=stdin), `--name`, `--model`, `--backend`, `--description`, `--arg k=v`* (repeatable), `--extra-arg`* (repeatable), `--env K=V`* (repeatable), `--id` |
| `profile edit <id\|name>` | Overlay flags/JSON onto profile | same as create (no `--id`) |
| `profile delete <id\|name>` | Delete (writes `.history` backup) | — |
| `profile duplicate <id\|name> [newid]` | Duplicate (default `<id>-copy`) | — |
| `profile rename <id\|name> <newname>` | Rename display name | — |
| `profile pin` / `profile unpin <id\|name>` | Toggle pinned | — |
| `profile export [id…]` | Export JSON bundle (all if none) | `-o/--output` (default stdout) |
| `profile import <path>` | Import bundle | `--mode merge\|overwrite\|rename` |
| `profile validate <id\|name>` | Run validator (exit 0/1/2) | `--json` |

### `instance …`

| Subcommand | Purpose | Key flags |
|------------|---------|-----------|
| `instance list` | Running instances | `-w/--watch`, `--interval` (2s) |
| `instance show <pid\|id>` | One instance | — |
| `instance history` | Exited instances | — |
| `instance start <profile>` | Load via proxy (5-min health wait) | `--json` |
| `instance stop <pid\|id>` | Unload (if proxy-owned) else kill; `--force` force-stops a degraded proxy first | `--json`, `--force` |
| `instance restart <pid\|id>` | Unload+reload / kill+reload | `--json` |
| `instance logs <pid\|id>` | Print captured log | `-f/--follow` |
| `instance metrics <pid\|id>` | Recent metrics window | `-w/--watch`, `--interval` |

### `model …`

| Subcommand | Purpose | Key flags |
|------------|---------|-----------|
| `model list` | Scan `[models].search_paths` for `.gguf` | `--path` (repeatable, override) |
| `model search <query>` | HF Hub search | `--limit` (20) |
| `model info <repo-id>` | HF repo metadata + files | — |
| `model download <repo> <file>` | Download one HF file | `--snapshot`, `--wait`, `--json` |
| `model downloads [cancel\|resume <id>]` | List / control downloads | `--json` |

### `backend …`

| Subcommand | Purpose | Key flags |
|------------|---------|-----------|
| `backend list` | Catalog backends | `--json` |
| `backend show <id>` | One backend metadata | `--json` |
| `backend add <name>` | Register backend | `--executable` (req), `--kind` (req) |
| `backend delete <id>` | Remove | — |
| `backend set-default <id>` | Mark default | — |
| `backend probe [id]` | Health-check binaries (5 s) | `--json` |
| `backend schema show <id>` | Show schema summary/JSON | `--json` |
| `backend schema refresh <id>` | Re-generate from `--help` | — |
| `backend schema apply <id> -f <file>` | Apply hand-edited schema (sets `Source.Editable`) | `-f/--file` (req) |

### `benchmark …`

Restructured into a subcommand tree (clean cutover — the old `--list` / `--compare` / `--transcript` flag-dispatch forms were removed). TUI and CLI share `BenchmarkConfig` (`internal/app/benchmark_config.go`) as the single source of truth.

| Subcommand | Purpose | Key flags |
|------------|---------|-----------|
| `benchmark run` | Run one mode, streaming live progress | `--profile` (required), `--mode <judge\|math-bench\|codegen-bench\|ragas-bench\|summary-bench\|llama-bench\|longctx\|instruction-bench\|mmlu-bench\|terminal-bench\|swe-bench-pro\|deep-swe>`, `--min-solve <0..1>` (exit 2 gate), `--limit` (`0`→full), `--verbose`, `--tb-task`/`--tb-n-tasks`, `--sweap-{harness,patches,instance}`, `--deepswe-{task,n-tasks,tasks}` |
| `benchmark list` | Saved-runs table (includes the run-`id` column) | — |
| `benchmark compare` | Latest run per profile, side by side | — |
| `benchmark history <profile-id>` | One profile's runs over time | — |
| `benchmark show <run-id>` | One run's full result | — |
| `benchmark transcript <run-id>` | Raw per-problem I/O | — |
| `benchmark export <run-id>` | Write the run's JSON + CSV | `--dir` |
| `benchmark delete <run-id>` | Remove a saved run | `--yes` (required) |
| `benchmark web` | Read-only browser for saved runs (+ live monitor) | — |

`--json` is honored from the root persistent flag (no per-command `--json`). `benchmark run` live output: permanent per-item ✓/✗/! lines, phase + staleness-transition lines, and — on a TTY — a repainting status block with a watchdog countdown; a non-TTY (piped) emits zero ANSI.

Categories: Quality / Speed / Robustness / Knowledge / **Agentic**. Three agentic modes (`terminal-bench`, `swe-bench-pro`, `deep-swe`) shell out to external harnesses (`tb`, SWE-bench_Pro-os, `pier`) plus Docker.

No `--dashboard` / `--leaderboard` (TUI-only).

`*` = repeatable flag.

## What "all the TUI does, headlessly" looks like

```bash
# Add a backend
model-loader backend add llama-cpp-stable \
  --executable /usr/local/bin/llama-server --kind llama-server

# Create a profile from a JSON snippet
echo '{"name":"qwen 4B q4","model":"/models/qwen2.5-3b-instruct-q4_k_m.gguf",
       "launch":{"backendId":"llama-cpp-stable"}}' | \
  model-loader profile create -f - --name "qwen 4B q4"

# Launch via proxy (waits up to 5 min for /health 200)
model-loader instance start qwen-4b-q4

# Send a request
curl -sX POST http://127.0.0.1:4321/v1/chat/completions \
  -H 'content-type: application/json' \
  -d '{"model":"qwen-4b-q4","messages":[{"role":"user","content":"hi"}]}'

# Unload (drains in-flight requests first)
model-loader instance stop qwen-4b-q4

# Run a benchmark
model-loader benchmark run --profile qwen-4b-q4 --mode math-bench --limit 5
```

## Anti-patterns

- Don't import `internal/ui` from `internal/cli` (TUIRunner is a callback, that's the seam)
- Don't bypass the single-instance flock for write operations
- Don't print directly with `fmt.Print` — go through `cmd.OutOrStdout()` for testability
- Don't use `fmt.Errorf` for sentinels — define `var ErrFoo = errors.New(...)` at package level

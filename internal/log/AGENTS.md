# AGENTS.md — internal/log

## OVERVIEW
Stdlib log/slog wiring for model-loader. Emits a file-only structured logger
that is safe to call while bubbletea owns the terminal alt-screen. Zero
third-party dependencies; rotation is pure stdlib (rename-then-glob-prune).

## WHERE TO LOOK
| File | Purpose |
|------|---------|
| `log.go` | `Config`, `New`, `ResolveLevel`, `NewAttemptID`, `Nop` |
| `log_test.go` | rotation behavior, level precedence, ID uniqueness, Nop |

## CONVENTIONS
- **File-only sink**: handler writes ONLY to `<Dir>/model-loader.log`. Never
  share the writer with stderr — bubbletea ownership of the framebuffer makes
  any stderr-write during `prog.Run()` corrupt the alt-screen.
- **Unbuffered writer**: `New` returns a logger whose underlying writer is the
  raw `*os.File`. Each `slog.Info`/`Error` becomes one `write(2)`. This is
  intentional so the `os.Exit(1)` paths in `cmd/model-loader/main.go` preserve
  every record via the kernel page cache without `Flush` ceremony.
- **Rotate-by-session**: every call to `New` renames the existing active log
  to `model-loader.<UTC-ISO-no-colons>.log` and keeps the 5 newest archives.
  Glob suffix is `model-loader.*.log` — never include the active file in the
  cap calculation.
- **Level precedence**: CLI > env > config > `info` default. `ResolveLevel`
  collapses three string sources into `slog.Level`; unrecognized values fall
  back to `Info` silently (flag.Parse rejects garbage CLI values upstream).
- **`Nop()` for nil-tolerance**: service constructors (`processmgr.New`,
  `backendcatalog.NewResolver`, `validator.New`) MUST accept a nil logger and
  substitute `Nop()`. Tests pass `log.Nop()` directly.
- **`NewAttemptID()` is the canonical correlation ID source**: 8 chars,
  base32, from `crypto/rand`. Use it in BOTH `LauncherPage.launchProfileCmd`
  AND `MonitorPage.restartCmd` — symmetry matters because Manager.Launch
  emits process_exited events without knowing which page spawned the launch.

## ANTI-PATTERNS
- DO NOT wrap the file writer in `bufio.Writer` — defeats the os.Exit
  durability guarantee.
- DO NOT route logs to stderr while `tea.Program.Run()` is active. Pre-TUI
  errors in `main.go` may dual-sink (stderr + logger.Error) before `prog.Run`.
- DO NOT introduce a JSON handler. `slog.NewTextHandler` (`key=value`) is the
  whole format — easy to grep with no jq dependency.
- DO NOT extend `Config` with `Format` or `Sink` knobs. The contract is
  intentionally minimal; multi-sink complexity belongs in a different design.
- DO NOT call `slog.SetDefault(logger)` — production callers receive the
  logger via constructor injection, not the global root.

## NOTES
- Rotation timestamp format `20060102T150405Z` (no colons) sorts
  lexicographically newest-first under `sort.Reverse(sort.StringSlice)`.
- `crypto/rand.Read` never errors on Linux post-Go 1.19 (uses `getrandom(2)`);
  we ignore the error return for `NewAttemptID`.
- Schema version: package is unversioned. Adding new top-level functions or
  Config fields is allowed; renaming or removing requires a bump documented
  here and in all consumer AGENTS.md files.

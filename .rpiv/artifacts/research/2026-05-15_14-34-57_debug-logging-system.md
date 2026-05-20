---
date: 2026-05-15T14:34:57-0300
author: quantmind-br
commit: 3e39508
branch: main
repository: model-loader
topic: "debug logging system — slog wiring, cmd.Wait enrichment, RunningInstance schema, friendlyLaunchError surface"
tags: [research, codebase, processmgr, logging, slog, launcher, instances, cmd-wait]
status: complete
last_updated: 2026-05-15T14:34:57-0300
last_updated_by: quantmind-br
---

# Research: debug logging system — slog wiring, `cmd.Wait` enrichment, `RunningInstance` schema, `friendlyLaunchError` surface

## Research Question

How does the model-loader codebase currently wire its `processmgr.Manager`, `backendcatalog.Resolver`, `validator.Validator`, and `LauncherPage` services; where can a stdlib `log/slog` logger be injected with minimum blast radius; what is the safe shape of a `cmd.Wait` enrichment body that populates new `ExitCode`/`ExitSignal`/`ExitReason`/`StderrTail` fields on `RunningInstance`; what does `friendlyLaunchError` need to consume those fields; and what CLI/env-var infrastructure must be introduced in `cmd/model-loader/main.go` to control verbosity?

The FRD's `## Recommended Approach` text — new `internal/log/` package emitting `*slog.Logger` injected via constructor params, `cmd.Wait` goroutines enriched to inspect `*exec.ExitError`, `friendlyLaunchError` consulting per-PID `ExitInfo` — is grounded in concrete file:line evidence below and validated against the live tree at commit `3e39508`.

## Summary

The four target services live in a single contiguous block of `cmd/model-loader/main.go:56-79` and are constructed exactly once at boot before `tea.Program.Run()` (`main.go:93`); this gives a clean pre-TUI window to install a logger. `processmgr.Config` (`manager.go:35-42`) is the established injection idiom for `processmgr`, and `LauncherPage` already uses the builder-mutator pattern (`SetBackendResolver` at `launcher.go:88-92`) — both shapes fit `*slog.Logger` cleanly. `backendcatalog.NewResolver` and `validator.New` use positional constructors with small test-surface (4 + 4 test sites respectively), so a new positional parameter is mechanical. **Zero pre-existing logger infrastructure** exists: no `slog` imports, no `internal/log/`, no `os.Getenv("DEBUG")`, no `flag.Parse()` in `cmd/model-loader/`, no `context.Context` on any service method. The 13 `Fprintf(os.Stderr,...)` sites in `main.go` plus 1 in `config.go:82` split cleanly into 10 pre-TUI (must dual-sink stderr+file) and 4 post-TUI (file-only safe).

The two `_ = cmd.Wait()` goroutines at `manager.go:149` (background) and `:340` (foreground) are panic-free one-liners today; converting them into enrichment bodies requires (a) capturing `pid`+`logPath`+`cmd` by value in the closure, (b) **moving** the foreground spawn from `:340` to after the `m.tracked[pid]=inst` block at `:352-356` (mirroring background ordering), (c) re-reading `m.tracked[pid]` under `m.mu` before mutating to defend against `Reconcile` swap-replace (`recover.go:55-58`) and `Kill` delete (`manager.go:221`), and (d) honoring the "win-by-liveness" guard since the 5-second liveness ticker (`liveness.go:42-58`) also mutates the same entry and sets `Crashed=true`. The new body adds a **fifth out-of-lock `saveRegistry` callsite** following the exact pattern of the existing four (`manager.go:151,227,358`, `liveness.go:60`).

The `RunningInstance` schema extension is risk-free: **all 47 literal constructions in tests use named-field syntax** and **zero tests use `reflect.DeepEqual` or `cmp.Diff` on the struct** (the FRD's "golden tests in `testdata/`" claim is refuted — no committed `instances.json` fixture exists). The `registryFile` wrapper at `registry.go:16-18` was explicitly designed for forward-compatibility; `omitempty` on the four new fields satisfies both forward and backward read.

`friendlyLaunchError` enrichment must happen one layer up in `handleLaunchErr` (`launcher.go:225-229`) — `p.waitingPID` is the only live PID handle and must be captured **before** the existing `p.waitingPID = 0` assignment. The post-spawn timeout path (`launcher.go:212-216`) always returns `ErrHealthCheckTimeout` after the full 30s — `WaitHealthy` (`manager.go:159-189`) cannot be defeated by anything else — so the enriched message replaces the unhelpful "did not become healthy within timeout" with the real exit cause when the Wait goroutine has populated `ExitInfo`, or falls back to the current message when `ok=false` (slow-startup, not-yet-dead). The status line (`renderStatusLine` at `launcher.go:494-507`) supports single-line messages with no width constraint — multi-line would fight the layout above.

Boot order is constrained: `flag.Parse()` first (new), then `config.Load()` (existing), then `log.New(cfg.Paths.LogDir, level)` (new — does its own `MkdirAll` since `processmgr.Launch:100` is the only existing creator and is lazy), then `processmgr.New(Config{Logger: lg, ...})`, then `mgr.Reconcile()` (now emits `reconcile_kept`/`reconcile_dropped` events). The `:29` Fprintf (config-load failure) stays raw stderr only — the chicken-and-egg resolves because that path is immediately fatal. Writer is unbuffered (one `write(2)` per event) so `os.Exit(1)` paths at `:31,37,96` lose only file descriptors, not log lines.

Precedent history shows **every prior goroutine added to `processmgr` needed a same-day race/lifecycle follow-up** (`9d04290`→`1f5eeeb` 17 min, `60cc846`→`2fed091` 10 min, `907736c`→`fac58d5` 5 h). The new Wait-body goroutine inherits that pattern — concurrency doc must land in the same commit, not a follow-up.

## Detailed Findings

### Constructor wiring & injection seams (the four target services)

The four services are constructed exactly once each, in the pre-TUI block of `cmd/model-loader/main.go:56-79`. Each has a distinct idiomatic injection shape that matches existing project conventions; mixing a single style across all four would force unnecessary test-surface churn.

#### `backendcatalog.NewResolver` — positional constructor param
- Constructor: `internal/service/backendcatalog/resolver.go:29` `func NewResolver(store Store, schemaStore SchemaStore) *resolver`
- Struct: `resolver.go:24-27` — two fields (`store`, `schemaStore`); becomes three with `logger`.
- Production call sites: `cmd/model-loader/main.go:56` (live resolver), `:115` and `:132` (inside `ensureDefaultCatalog` re-construction). The dev-only `cmd/scripts/print_args.go:30` is out of scope per FRD.
- Test call sites: 4 in `internal/service/backendcatalog/resolver_test.go:27,65,136,175`, all chained as `NewResolver(...).Resolve(...)`.
- New signature: `NewResolver(store Store, schemaStore SchemaStore, logger *slog.Logger) *resolver`. Mechanical 7-site update; tests pass `slog.New(slog.NewTextHandler(io.Discard, nil))` or accept nil-tolerance inside the constructor.

#### `validator.New` — positional constructor param
- Constructor: `internal/service/validator/validator.go:38-40` `func New() Validator { return defaultValidator{} }`
- Struct: `validator.go:42` `type defaultValidator struct{}` (empty today).
- Production call sites: `main.go:72`, `internal/ui/pages/profile_editor/editor.go:86` (editor live validation), `internal/ui/pages/profiles_test.go:97`.
- Test call sites: `internal/service/validator/validator_test.go:11`, `internal/ui/pages/launcher_test.go:134`.
- New signature: `New(logger *slog.Logger) Validator`. The editor's validator can take `slog.Default()` since the editor never spawns — it's not on the load path that needs correlation.

#### `processmgr.New(Config)` — new `Logger` field on `Config` + `WaitFunc` test seam
- Constructor: `internal/service/processmgr/manager.go:46-60`.
- `Config` struct: `manager.go:35-42` — five fields (`Resolver`, `DefaultBinary`, `LogDir`, `RegistryPath`, `LastUsedSink`); becomes **seven** with `Logger *slog.Logger` and `WaitFunc func(*exec.Cmd) error` (test seam per Decision).
- `fsManager` struct: `manager.go:23-33` — gains `logger *slog.Logger` field between `sink` and `mu` at line 28.
- Production call: `main.go:60-63`.
- Test call sites: 5 in `manager_test.go:37,86,133,219,281`; 2 in `recover_test.go:66,106`; 1 in `reconcile_wrapper_test.go:31`; 2 in `liveness_test.go:14,43`; 2 in `monitor/subscribe_test.go:32,71`. **All use named-field `Config{...}` literals**, so adding new optional fields is **0-edit** for tests — nil-tolerant defaults inside `New` cover the case.
- **Builder rejected** because `processmgr` has zero `WithX` mutators today; the `Config{}` idiom is the dominant pattern for this package.

#### `pages.NewLauncherPage` — builder mutator `WithLogger(lg)`
- Constructor: `internal/ui/pages/launcher.go:52-65` (value receiver returning `LauncherPage`).
- Existing builder precedent: `launcher.go:88-92` `SetBackendResolver` — exact same shape needed.
- Production call: `main.go:76-79`.
- Test call sites: **17 in `launcher_test.go`** (`:34,87,134,160,186,209,243,278,309,319,329,338,405,429,444,462,473`) + 2 in `root_test.go:113,411`. Most pass `nil, nil, nil` as smoke tests.
- New method: `func (p LauncherPage) WithLogger(lg *slog.Logger) LauncherPage { p.logger = lg; return p }` matching `SetBackendResolver` byte-for-byte. **0-edit** for tests (the chain `pages.NewLauncherPage(...).WithLogger(lg).SetBackendResolver(resolver)` is additive).

### `attempt_id` propagation across resolve → validate → spawn → Wait

Zero `attempt_id` / `attemptID` / `AttemptID` references exist in the tree today (verified by Grep across the whole repo).

The pipeline crosses **two goroutines**:
1. The `launchProfileCmd` closure at `launcher.go:382-414` runs steps 1-3 (Resolve → Validate → mgr.Launch) off-thread and emits `launchedMsg`.
2. The `waitCmd` closure inside `handleLaunched` at `launcher.go:212-216` runs step 4 (`mgr.WaitHealthy`) in a **separate goroutine** that doesn't see the launcher closure's scope.

No service method takes `context.Context` today — confirmed signatures:
- `Resolver.Resolve` (`backendcatalog/resolver.go:13-15`, impl `:34`) — `(domain.Profile) (ResolvedBackend, error)`
- `Validator.Validate` (`validator.go:35-37`, impl `:44`) — `(p Profile, schema FlagSchema) Report`
- `Manager.Launch` (`processmgr.go:25`, impl `manager.go:73`) — `(p Profile, mode LaunchMode) (RunningInstance, error)`
- `Manager.WaitHealthy` (`processmgr.go:28`, impl `manager.go:159`) — `(pid, port int, timeout time.Duration) error`

**Decision: explicit `attemptID string` parameter on `Launch` and `WaitHealthy`** (interface widens to those two methods + new `GetExitInfo`); `Resolve` and `Validate` keep their signatures and rely on the launcher emitting its own `lg.With("attempt_id", id)` wrapper records around the calls at `launcher.go:395` and `:402`.

The launcher generates the ID inside `launchProfileCmd` at `launcher.go:382-390`, binds it via `slog.With(...)`, and passes it through `launchedMsg` so `handleLaunched` can forward it to `WaitHealthy`:

```
type launchedMsg struct {
    inst      domain.RunningInstance
    attemptID string  // NEW field; ephemeral, never persisted
}
```

This keeps `domain.RunningInstance` (and `instances.json`) untouched while threading correlation across both goroutines.

### `cmd.Wait` enrichment body — safe concurrent shape

The two existing sites at `manager.go:149` (background) and `manager.go:340` (foreground) are panic-free zombie reapers. Replacing them requires the contract below.

**Closure capture by value at spawn time** — never read the surrounding stack frame's `inst` variable from inside the goroutine:
```
go func(pid int, logPath string, cmd *exec.Cmd) { ... }(inst.PID, inst.LogPath, cmd)
```

**Foreground spawn must move** — currently at `manager.go:340`, before the map insert at `:352-356`. Wait enrichment needs `inst` already in `m.tracked[pid]`, so the spawn must relocate to after `:356`, mirroring the background ordering at `:144-149`.

**Body steps** (in order):
1. `err := cmd.Wait()` — captures `*exec.ExitError` via `errors.As`.
2. Extract `ExitCode`, `ExitSignal` (Linux: `exitErr.ProcessState.Sys().(syscall.WaitStatus)` with `, ok` guard), `ExitReason` ("exit:N" or "signal:NAME").
3. Read tail of `logPath` if non-empty — **outside the lock** to avoid blocking `m.mu` on file I/O. Foreground (`logPath == ""`) skips this step.
4. `m.mu.Lock()` → re-read `m.tracked[pid]` → `if !ok { unlock; return }` (defends Reconcile/Kill race) → mutate or skip-already-Crashed (defends liveness race) → `snapshotLocked` → `m.mu.Unlock()`.
5. `_ = saveRegistry(m.registryPath, snap)` — **5th out-of-lock callsite**, same pattern as the existing four (`manager.go:151,227,358`, `liveness.go:60`).

**Win-by-liveness guard**: the 5-second liveness ticker (`liveness.go:42-58`) can set `Crashed=true`+`ExitedAt` before Wait fires. If `cur.Crashed` is already true, fill only the fields liveness can't know (`ExitCode`, `ExitSignal`, `ExitReason`, `StderrTail`) — never overwrite `ExitedAt`.

**Panic guard**: per Decision, install `defer recover()` at the top of all three processmgr goroutines (both Wait sites + `liveness.go:33`). The recover handler logs via the file-only logger — never to stderr — so the TUI framebuffer stays intact:
```
defer func() {
    if r := recover(); r != nil {
        logger.Error("goroutine panic", "pid", pid, "panic", r, "stack", string(debug.Stack()))
    }
}()
```

### `RunningInstance` schema extension & persistence

Adding four fields to `internal/domain/instance.go:6-15` is fully backward-compatible because:
- `registryFile` at `registry.go:16-18` is a versionless object wrapper explicitly designed to extend (`"Wrapping the slice in an object lets us add fields later"`).
- All four new fields use `json:",omitempty"`: old binaries reading a new registry silently drop unknown fields; new binaries reading an old registry zero-value the missing fields.
- `loadRegistry` (`registry.go:22-36`) and `saveRegistry` (`registry.go:39-58`) use stdlib `encoding/json` round-trip — no bespoke schema validation.

**No schema-version bump needed** — and none was used by the precedent that added `Crashed`+`ExitedAt` (commit `9d04290`).

The new fields:
```
ExitCode    *int       `json:"exitCode,omitempty"`
ExitSignal  string     `json:"exitSignal,omitempty"`
ExitReason  string     `json:"exitReason,omitempty"`
StderrTail  []string   `json:"stderrTail,omitempty"`
```

**Reconcile preserves new fields** — `recover.go:50-52,55-57` copies the entire `ri` struct value-for-value through the `tracked`/`survivors` slices, so new fields ride along automatically.

**The Wait goroutine adds the 5th `saveRegistry` callsite** (recommended over a "dirty flag for liveness to pick up"):
- Latency: liveness fires every 5 s; riding it loses up to 5 s of evidence on TUI restart.
- Pattern symmetry: identical shape to the existing four.
- Race elimination: in-line save reaches disk before Reconcile can swap-replace.

### `friendlyLaunchError` enrichment & status-line surface

`friendlyLaunchError` (`launcher.go:78-95`) matches 4 sentinels via `errors.Is`:
- `ErrPortBusy` → "port in use — change the profile port or kill the running PID"
- `ErrModelNotFound` → "model file not found — fix the profile's Model path"
- `ErrForegroundBusy` → "a foreground instance is already running — toggle [b] to background mode"
- `ErrHealthCheckTimeout` → "server did not become healthy within timeout — check logs" ← unhelpful target

**Two distinct `launchErrMsg` paths:**
1. Pre-spawn (`launchProfileCmd` closure body, 4 sites at `launcher.go:392,396,401,408`) — `p.waitingPID` is **still 0** because `handleLaunched` (the only setter) has not run yet. Enrichment must skip these.
2. Post-spawn (`waitCmd` inside `handleLaunched`, `launcher.go:212-216`) — `mgr.WaitHealthy` always returns `ErrHealthCheckTimeout` wrapped via `fmt.Errorf("pid %d not healthy: %w", pid, err)`. **Crashes don't shortcut it** — `WaitHealthy` (`manager.go:159-189`) polls `/health` with backoff until `deadline = time.Now().Add(timeout)` elapses unconditionally.

**Enrichment seam — Option A in `handleLaunchErr`**, NOT a wider `friendlyLaunchError` signature. The handler at `launcher.go:225-229` zeros `p.waitingPID` BEFORE calling `friendlyLaunchError(msg.err)`. The capture-then-clear pattern:
```
pid := p.waitingPID
p.waitingPID = 0
base := friendlyLaunchError(msg.err)
if pid != 0 && p.manager != nil {
    if exit, ok := p.manager.GetExitInfo(pid); ok {
        base = enrichWithExit(base, exit)  // appends "exit 1: <last stderr line>"
    }
}
p, fc := p.withStatus(base)
```

**TOCTOU race** between `WaitHealthy` timeout and Wait goroutine completion: at the 30-s timeout boundary, the Wait body may not have fired yet (process alive but hung initializing weights). In that case `GetExitInfo` returns `ok=false` and the message falls back to the current "did not become healthy within timeout — check logs". This is correct — the enrichment is best-effort.

**Status-line shape** (`renderStatusLine` at `launcher.go:494-507` rendering via `theme.Subtitle`): no `.Width()` constraint, no border, **single-line preferred**. The last non-empty line of `StderrTail` truncated via the existing `truncate()` helper at `messages.go:46-51` is the right source. Multi-line would visually fight `renderRunningList` above.

**`friendlyLaunchError` keeps its name** — the kill path at `launcher.go:308-323` uses raw `"error: " + err.Error()` and would not benefit from sentinel routing (no shared vocabulary with the spawn-time sentinels).

### CLI flag parsing & boot sequence

**Zero CLI parsing today** — `func main()` at `cmd/model-loader/main.go:26` jumps straight to `config.Load()` at `:27`. Confirmed `os.Args` has only one consumer (`cmd/scripts/print_args.go:19-20`, separate binary). Confirmed no `flag.Parse()` / `flag.String` / etc. anywhere in `cmd/model-loader/`. Adding `flag.Parse()` brings automatic `-h`/`--help` for free with **zero collisions**.

**Viper does NOT auto-bind env vars** — Grep for `AutomaticEnv|BindEnv|SetEnvPrefix` returns zero hits. Reading `MODEL_LOADER_LOG_LEVEL` must be done via `os.Getenv` in a new `internal/log/Config.Resolve()` function, not through Viper.

**Insertion order** in `main()`:
```
1. flag.Parse()                    // NEW — at the top
2. cfg, err := config.Load()       // existing :27
   // :29 Fprintf stays raw stderr (path is immediately fatal at :31)
3. logger, closeFn, err := log.New(log.Config{
       Dir:   cfg.Paths.LogDir,
       Level: log.ResolveLevel(cliLevel, os.Getenv("MODEL_LOADER_LOG_LEVEL"), cfg.Logging.Level),
   })                              // NEW — after cfg, before mgr
4. defer closeFn()
5. // (the 13 other Fprintfs become dual-sink or file-only via logger)
6. mgr := processmgr.New(processmgr.Config{..., Logger: logger}) // existing :58, extended
7. mgr.Reconcile()                 // existing :67, now emits reconcile_kept/dropped
8. ... pages ...
9. prog := tea.NewProgram(root, tea.WithAltScreen())  // existing :93
10. prog.Run()                                         // existing :94
```

**`AppConfig` extension** at `internal/config/config.go:14-18`:
```
type AppConfig struct {
    Paths   PathsConfig   `mapstructure:"paths"`
    Models  ModelsConfig  `mapstructure:"models"`
    UI      UIConfig      `mapstructure:"ui"`
    Logging LoggingConfig `mapstructure:"logging"`   // NEW
}
type LoggingConfig struct {
    Level string `mapstructure:"level"`
}
```
Default goes in `applyDefaults` at `config.go:113-124`: `v.SetDefault("logging.level", "info")`.

**LogDir provisioning** — `cfg.Paths.LogDir` is defaulted at `config.go:116` to `~/.local/state/model-loader/logs`. Today the **only** `MkdirAll` is the lazy `os.MkdirAll(m.logDir, 0o755)` inside `processmgr.Launch` at `manager.go:100`. The new `log.New` must `MkdirAll` itself — confirmed no third consumer of LogDir exists.

**Rotation algorithm** — filename-sortable timestamps avoid `os.Stat` per file:
```
active := filepath.Join(dir, "model-loader.log")
if _, err := os.Stat(active); err == nil {
    ts := time.Now().UTC().Format("20060102T150405Z")   // colon-free
    _ = os.Rename(active, filepath.Join(dir, "model-loader."+ts+".log"))
}
matches, _ := filepath.Glob(filepath.Join(dir, "model-loader.*.log"))
sort.Sort(sort.Reverse(sort.StringSlice(matches)))      // newest-first lexicographic = chronological
for i, p := range matches { if i >= 5 { _ = os.Remove(p) } }
```

**Writer is unbuffered** (per Decision) — each `slog.Info` does one `write(2)`. The 3 `os.Exit(1)` paths at `main.go:31,37,96` lose only file descriptors (kernel reaps); all log lines written before `os.Exit` are persisted via `write(2)`'s page cache. `defer closeFn()` at the top of `main()` covers the graceful exit.

### `Fprintf(os.Stderr,...)` migration inventory (13+1 sites)

Pre-TUI (10 sites — must dual-sink to stderr+file):

| Site | Currently | New |
|---|---|---|
| `main.go:29` | `"config error: %v\n"` → `os.Exit(1)` | stays raw stderr (logger doesn't exist yet — chicken-and-egg resolves because path is fatal) |
| `main.go:35` | `"profile store: %v\n"` → `os.Exit(1)` | `logger.Error("profile store init failed", "err", err)` dual-sink |
| `main.go:50` | `"migration: %v\n"` (non-fatal) | `logger.Error("migration failed", "err", err)` dual-sink |
| `main.go:53` | `"migration warning: %s\n"` (loop) | `logger.Warn("migration warning", "msg", w)` dual-sink |
| `main.go:68` | `"instance recovery: %v\n"` | `logger.Error("instance recovery failed", "err", err)` dual-sink |
| `main.go:110` | `"backend catalog load error: %v\n"` | `logger.Error("backend catalog load failed", "err", err)` dual-sink |
| `main.go:111` | catalog remediation hint | `logger.Warn("catalog remediation hint", "hint", ...)` dual-sink |
| `main.go:119` | "warning: default backend schema missing/invalid" | `logger.Warn("default backend schema invalid; using fallback")` dual-sink |
| `main.go:127` | `"save default catalog: %v\n"` | `logger.Error("save default catalog failed", "err", err)` dual-sink |
| `config.go:82` | `"config migration warning..."` (default_tab) | `logger.Warn("config migration write failed", "field", "default_tab", "err", err)` — but fires inside `LoadFrom`, before logger exists; stays raw stderr OR routes through a logger-by-arg path |

Post-TUI (4 sites — file-only is safe since alt-screen is torn down):

| Site | Currently | New |
|---|---|---|
| `main.go:95` | `"tui error: %v\n"` → `os.Exit(1)` | `logger.Error("tui run failed", "err", err)` file-only |
| `main.go:99` | `"N background instance(s) still running"` (count) | `logger.Warn("background instances still running", "count", len(running))` |
| `main.go:101` | per-instance `"PID N (port P)"` (loop) | `logger.Warn("background instance", "pid", ri.PID, "port", ri.Port)` |
| `main.go:103` | `"Restart the TUI to manage them."` (Fprintln — the +1 vs the FRD's "12") | `logger.Warn("restart hint", "msg", ...)` |

### `RunningInstance` test surface — risk-free schema change

**47 literal constructions of `domain.RunningInstance` across 7 test files** — **every one uses named-field syntax** (verified file-by-file). **Zero** uses of `reflect.DeepEqual` or `cmp.Diff` operate on `RunningInstance` values anywhere in the repo. **No committed `testdata/instances.json` fixture exists** — the FRD's "golden tests" claim was incorrect; on-disk shape is exercised only by ephemeral `t.TempDir()` files.

Counts per file:
- `registry_test.go` — 2 literals (`:16-18`, `:46`)
- `recover_test.go` — 4 literals (`:20-23`, `:58`, `:99`)
- `manager_test.go` — 0 direct literals; only return values of `mgr.Launch(...)` are inspected via field access
- `liveness_test.go` — 2 literals (`:16`, `:45`)
- `reconcile_wrapper_test.go` — 1 literal (`:25`)
- `launcher_test.go` — 8 literals (`:56,60,161,187,210,246,410,430`)
- `monitor_test.go` — 30 literals (recount of FRD's "40+")

**MEDIUM-risk tests** (3): `manager_test.go:46-78`, `recover_test.go:43-122`, `reconcile_wrapper_test.go:11-47`. Each calls `mgr.Launch(p, LaunchBackground)` against `fake-llama-server.sh` and uses `defer mgr.Kill(inst.PID)` — exactly where the new Wait body fires. **`WaitFunc func(*exec.Cmd) error` injected via `Config`** (per Decision) collapses all three to LOW: set to a no-op in test helper `newTestManager(t)`.

**LOW-risk** (everything else): registry round-trip is field-specific (`ri.PID == inst.PID`), liveness tests use stub probes, launcher/monitor tests construct via named literals and inspect via field access.

**Fake Manager impacts** — only `fakeManager` in `internal/ui/pages/launcher_test.go:46-66` implements the full `processmgr.Manager` interface. The monitor fakes (`fakeProcMgr`, `killTrackingMgr`, `restartTrackingMgr`) implement a **narrower local interface `procMgrIface`** at `internal/ui/pages/monitor.go:48-53` and are **insulated** from the 3-method extension to `Manager`. Net surface for the new `GetExitInfo` + `Launch(attemptID)` + `WaitHealthy(attemptID)`: 1 file (`launcher_test.go`) needs stub methods + 1 production file (`manager.go`) needs real implementations.

### Stderr/stdout/panic surfaces during TUI runtime

Exhaustive sweep result: **codebase is remarkably clean**.

- `fmt.Println(`: 3 sites, all in `cmd/scripts/print_args.go:43,46,50` (separate binary, never linked into `model-loader`).
- `fmt.Print(`: 1 site, same dev binary.
- `os.Stdout`: **zero** references.
- `os.Stderr`: 18 sites total — 3 in dev binary + 14 in `cmd/model-loader/main.go` + 1 in `config.go:82`.
- Stdlib `log.*` (`log.Print`, `log.Fatal`, etc.) and `"log"` import: **zero**.
- `panic(`: **zero** in production code.
- `recover()`: **zero** anywhere.

The two `Fprintf` matches in UI code are safe: `profiles_list.go:89,92` writes to `list.Model`'s render buffer (an `io.Writer w`), and `monitor.go:620,632,633,635` writes to `*strings.Builder`. Neither touches stderr/stdout.

**Bubbletea v1.3.10 wraps every `tea.Cmd` invocation in `defer recover()`** at `tea.go:355-360,510-512,534-538,552-556` — so panics inside `launchProfileCmd`'s returned closure are contained. **But the three processmgr goroutines are outside this net** (both `cmd.Wait` sites + the liveness ticker), and `recoverFromGoPanic` at `tea.go:852-861` actually prints to stdout while the alt-screen may still be active (library quirk). The Decision to install `defer recover()` in all three processmgr goroutines is the right safeguard — bubbletea cannot help there.

The migration service (`internal/service/migration/migration.go:48-141`) is fully synchronous — no goroutines outlive `Run()`. Confirmed by Grep: zero `go func` or `go s.` inside `internal/service/migration/`.

### Boot-time order chicken-and-egg

The `main.go:29` Fprintf fires after `config.Load()` fails — **before** the logger could possibly exist (logger needs `cfg.Paths.LogDir` which `config.Load` returns). This is unavoidable. Three paths considered:

1. **Stays raw stderr** (recommended): the path is immediately fatal at `:31` (`os.Exit(1)`), nothing further runs; user sees the stderr message; nothing to log.
2. Best-effort default-path logger init **before** `config.Load`: would duplicate the `~/.local/state/model-loader/logs` literal currently living solely at `config.go:116`. Drift risk.
3. Move config Load defaults into a tiny `defaultLogDir()` helper consumed by both: complicates ownership; not worth it for one fatal path.

`config.go:82` (default_tab migration warning) fires inside `LoadFrom`, also before `config.Load()` returns. Same chicken-and-egg, same answer: stays raw stderr (warning is non-fatal but the path predates logger creation).

## Code References

- `cmd/model-loader/main.go:26-96` — `func main()`; flag.Parse insertion point at top; logger init between `:27` and `:33`; the 13 `Fprintf` sites enumerated above; `prog.Run()` bracket at `:93-96`
- `cmd/model-loader/main.go:106-135` — `ensureDefaultCatalog` with 4 `Fprintf` sites at `:110,111,119,127`
- `internal/config/config.go:14-18` — `AppConfig` add `Logging LoggingConfig` field
- `internal/config/config.go:21-27` — `PathsConfig` already has `LogDir`
- `internal/config/config.go:82` — config-migration `Fprintf` (chicken-and-egg case)
- `internal/config/config.go:113-124` — `applyDefaults`; add `v.SetDefault("logging.level", "info")`
- `internal/config/config.go:116` — `paths.log_dir` default (`~/.local/state/model-loader/logs`)
- `internal/domain/instance.go:6-15` — `RunningInstance` struct; insert 4 new fields with `omitempty` before closing brace
- `internal/service/processmgr/processmgr.go:23-31` — `Manager` interface; widen `Launch`/`WaitHealthy` + add `GetExitInfo`
- `internal/service/processmgr/manager.go:35-42` — `Config` struct; add `Logger *slog.Logger` + `WaitFunc func(*exec.Cmd) error`
- `internal/service/processmgr/manager.go:23-33` — `fsManager` struct; add `logger *slog.Logger` field
- `internal/service/processmgr/manager.go:46-60` — `New(cfg Config)`; nil-tolerant defaults for Logger and WaitFunc
- `internal/service/processmgr/manager.go:100` — only existing `MkdirAll(m.logDir)`; new `log.New` must also create it
- `internal/service/processmgr/manager.go:111-124` — per-instance log file (preserved, separate from new logger)
- `internal/service/processmgr/manager.go:144-151` — background Launch: snapshot under lock, save unlocked
- `internal/service/processmgr/manager.go:149` — **Wait goroutine #1** (background) — replace with enriched body
- `internal/service/processmgr/manager.go:220-227` — Kill: delete under lock, save unlocked (2nd of 4 callsites)
- `internal/service/processmgr/manager.go:340` — **Wait goroutine #2** (foreground) — **MOVE to after :356** and replace with enriched body
- `internal/service/processmgr/manager.go:352-358` — foreground insert + 3rd `saveRegistry` callsite
- `internal/service/processmgr/liveness.go:31-61` — liveness ticker; install `defer recover()` at goroutine top (`:33`)
- `internal/service/processmgr/liveness.go:42-58` — liveness lock pattern (template for the win-by-liveness guard)
- `internal/service/processmgr/liveness.go:60` — 4th `saveRegistry` callsite
- `internal/service/processmgr/recover.go:27-66` — `Reconcile`; emits `reconcile_kept`/`reconcile_dropped` after logger lands
- `internal/service/processmgr/recover.go:55-58` — `m.tracked` swap-replace (race the Wait body must defend against)
- `internal/service/processmgr/registry.go:16-18` — `registryFile` versionless wrapper
- `internal/service/processmgr/registry.go:22-58` — `loadRegistry` / `saveRegistry` (unchanged)
- `internal/service/backendcatalog/resolver.go:24-34` — `resolver` struct + `NewResolver`; add `logger *slog.Logger` field + 3rd param
- `internal/service/validator/validator.go:38-44` — `New()` + `defaultValidator`; add `logger *slog.Logger` field + param
- `internal/ui/pages/launcher.go:23-46` — `LauncherPage` struct; add `logger *slog.Logger` field
- `internal/ui/pages/launcher.go:52-65` — `NewLauncherPage`; unchanged
- `internal/ui/pages/launcher.go:73-95` — `friendlyLaunchError`; signature unchanged
- `internal/ui/pages/launcher.go:88-92` — `SetBackendResolver` (template for new `WithLogger`)
- `internal/ui/pages/launcher.go:104-106` — `launchedMsg` struct; add ephemeral `attemptID string` field
- `internal/ui/pages/launcher.go:197-217` — `handleLaunched`; sets `p.waitingPID`; forwards `attemptID` to `waitCmd`
- `internal/ui/pages/launcher.go:212-216` — `waitCmd` calling `mgr.WaitHealthy` (always-timeout path)
- `internal/ui/pages/launcher.go:224-229` — `handleLaunchErr`; **capture pid BEFORE clearing waitingPID**, then enrich
- `internal/ui/pages/launcher.go:382-414` — `launchProfileCmd`; generate `attemptID` at top; thread through `mgr.Launch`
- `internal/ui/pages/launcher.go:494-507` — `renderStatusLine`; single-line, no width constraint
- `internal/ui/pages/launcher_test.go:46-66` — `fakeManager`; **only Manager fake needing stub updates**
- `internal/ui/pages/monitor.go:48-53` — `procMgrIface` (insulates monitor fakes from Manager widening)

## Integration Points

### Inbound References
- `cmd/model-loader/main.go:56` → `backendcatalog.NewResolver(catalogStore, schemaStore)` — adds 3rd arg `logger`
- `cmd/model-loader/main.go:60-65` → `processmgr.New(processmgr.Config{...})` — adds `Logger`+`WaitFunc` fields
- `cmd/model-loader/main.go:72` → `validator.New()` — adds arg `logger`
- `cmd/model-loader/main.go:76-79` → `pages.NewLauncherPage(...).SetBackendResolver(...)` — chain extended with `.WithLogger(logger)`
- `cmd/model-loader/main.go:115,132` → `backendcatalog.NewResolver` inside `ensureDefaultCatalog` (2 more sites; same 3rd-arg update)
- `internal/ui/pages/profile_editor/editor.go:86` → `validator.New()` inside the editor (separate, can take `slog.Default()`)
- `internal/ui/pages/profiles_test.go:97` → `validator.New()` test site

### Outbound Dependencies
- `internal/service/processmgr/manager.go:149,340` → `cmd.Wait()` (existing stdlib `*exec.Cmd`); new body adds `errors.As`, `*exec.ExitError`, `ProcessState.Sys().(syscall.WaitStatus)` (Linux), `os.ReadFile(LogPath)`
- `internal/service/processmgr/manager.go:128` → `cmd.Start()`; spawn ordering preserved (background) but flipped (foreground moves to post-insert)
- `internal/service/processmgr/recover.go:30` → `loadRegistry(m.registryPath)`; round-trips new fields via `omitempty`
- `internal/ui/pages/launcher.go:204-209` → `mgr.WaitHealthy(pid, port, 30s)`; new signature gets `attemptID` arg
- `internal/ui/pages/launcher.go:225` → `p.manager.GetExitInfo(pid)`; new method on `Manager`
- New: `internal/log/` package importing `log/slog`, `io`, `os`, `path/filepath`, `sort`, `time`

### Infrastructure Wiring
- `cmd/model-loader/main.go:27` (new) → `flag.Parse()` for `--log-level`
- `cmd/model-loader/main.go:28+` (new) → `log.New(log.Config{Dir: cfg.Paths.LogDir, Level: log.ResolveLevel(cliLevel, envLevel, cfgLevel)})`
- `cmd/model-loader/main.go:Xdef` (new) → `defer closeFn()` (writer is unbuffered; the 3 `os.Exit(1)` paths don't need helper)
- `internal/config/config.go:113-124` (new) → `v.SetDefault("logging.level", "info")` in `applyDefaults`
- `internal/service/processmgr/AGENTS.md` (new doc) — "Wait goroutine lifecycle" section listing 5 (not 4) `saveRegistry` callsites + win-by-liveness contract
- Rotation step inside `internal/log/New`: rename `model-loader.log` → `model-loader.<ts>.log`, glob+sort+remove keeping newest 5

## Architecture Insights

1. **`processmgr.Config` is the established "service-with-deps" idiom** — every dependency in the package goes through the `Config` struct passed to `New(cfg Config)`. Adding `Logger` and `WaitFunc` fields here is the lowest-blast-radius option; the named-field literal pattern that every test already uses means **0 test edits** for adding new optional fields with nil-tolerant defaults inside `New`.

2. **Pages use builder-mutator (`WithX`/`SetX`); services use `Config` struct** — this isn't accidental. Pages are value-receivers in bubbletea (`launcher.go:23` declares `type LauncherPage struct{}` and methods all have value receivers like `func (p LauncherPage) Update(...) (tea.Model, tea.Cmd)`). A constructor accumulator pattern is how new fields are added without forcing positional churn through 19+ test sites. Services don't have the value-receiver constraint and benefit from explicit `Config` for ergonomic test injection.

3. **Five out-of-lock `saveRegistry` callsites is the pattern, not a smell** — all four existing sites (`manager.go:151,227,358`, `liveness.go:60`) follow `m.mu.Lock(); mutate; snapshot := snapshotLocked(m.tracked); m.mu.Unlock(); saveRegistry(path, snapshot)`. The new Wait body adds a fifth that mirrors this exactly. The FRD's Follow-up flagged it as a "latent race" but the pattern is intentional: holding the mutex across file I/O would serialize all launches against a possibly-slow JSON write. The race that matters is between Wait, Liveness, Reconcile, and Kill — all of which serialize on `m.mu` for the **mutation** step; the save is best-effort and may briefly disagree with disk between two concurrent mutations, which is acceptable because Reconcile reads disk on next boot.

4. **`registryFile` versionless wrapper + `omitempty` is the migration policy** — no schema version field exists; backward-compat is purely "old binary drops unknown fields, new binary zero-values missing fields". Every `RunningInstance` field added since `04ff36c` (the original registry commit) has followed this discipline: `Crashed` and `ExitedAt` in commit `9d04290`, now `ExitCode`/`ExitSignal`/`ExitReason`/`StderrTail` in this FRD. **No migration code path exists or is needed.**

5. **`Manager` interface has narrow blast radius because `MonitorPage` declared its own subset** — `procMgrIface` at `monitor.go:48-53` lists only `List`, `Kill`, `Launch`, `TailLogs`. This pre-existing interface-segregation pattern means the three monitor fakes (`fakeProcMgr`, `killTrackingMgr`, `restartTrackingMgr`) are immune to `Manager` widening. The convention also signals that **future Manager methods should consider whether they belong on the narrower interface**: `GetExitInfo` is launcher-only (the monitor doesn't render exit causes), so it correctly stays on the wider `Manager` interface only.

6. **bubbletea's `tea.Cmd` recover boundary does NOT cover non-`tea.Cmd` goroutines** — `tea.go:355-360` wraps every `Cmd` invocation in `defer recover()`, so `launchProfileCmd`'s returned closure is safe. But goroutines spawned outside bubbletea's lifecycle (the two `cmd.Wait` sites, the liveness ticker, the 7 unwrapped `go func()` in `internal/service/monitor/subscribe.go`, and `modelscanner/scanner.go:24`) crash the process with the default Go runtime handler — which prints to stderr (corrupting the alt-screen) and calls `os.Exit(2)` bypassing every `defer`. The Decision to install `defer recover()` in the three processmgr goroutines is mandatory for the new enrichment work, not optional.

7. **Boot is rigidly linear** — there is no DI container, no service registry, no lazy initialization. Every dependency is constructed in order in `main()`, and the order is dictated by the data: `config.Load` → `log.New(LogDir from cfg)` → `processmgr.New(Logger from log.New, LogDir from cfg)` → pages with refs to all of the above → `tea.NewProgram(pages)` → `prog.Run()`. The new logger lands at a single, deterministic insertion point between `config.Load` and `processmgr.New` — there's no surprise sequencing.

8. **The pre-TUI bracket is the only safe window for stderr** — `tea.Program.Run()` at `main.go:94` takes ownership of the terminal via `WithAltScreen()`. Anything before that line and anything after `Run()` returns can write to stderr safely. Anything from inside any goroutine that the TUI doesn't own (notably the processmgr goroutines) must write to a file sink — never to stderr. This is the architectural constraint that drove the "dual-sink for boot, file-only for runtime" handler design.

## Precedents & Lessons

**7 similar past changes analyzed.**

### Precedent: addition of `Crashed`+`ExitedAt` fields to `RunningInstance` (closest match)
**Commit(s)**: `9d04290` — "feat(processmgr): liveness ticker detects crashed background instances" (2026-04-29)
**Blast radius**: 7 files across 4 layers
  - `internal/domain/instance.go` — added 2 `omitempty` fields (no migration code)
  - `internal/service/processmgr/liveness.go` — NEW file; goroutine mutates tracked map under `m.mu`, saves outside lock
  - `internal/service/processmgr/manager.go` — added `livenessStop func()` + `Close()` lifecycle
  - `cmd/llama-cpp-loader/main.go` — wired `defer mgr.Close()`
  - `internal/service/processmgr/liveness_test.go` — +70 LOC

**Follow-up fixes**:
- `1f5eeeb` — "fix(processmgr): goroutine-safe stop + remove dead liveness sentinel" (17 min later) — replaced ad-hoc `once bool` with `sync.Once` for double-close safety; dropped dead `errLivenessUnavailable` sentinel kept "for future builds"

**Takeaway**: this FRD repeats the exact shape (new optional fields + goroutine that snapshots+saves outside lock) and the previous attempt broke on a concurrent-stop race within 17 minutes. **Use `sync.Once` for any new stop func, and don't declare future-platform sentinels you have to fake-reference in tests.**

### Precedent: atomic persistence of `instances.json`
**Commit(s)**: `04ff36c` — "feat(processmgr): atomic load/save for instances.json registry" (2026-04-28)
**Blast radius**: 2 files in 1 layer (`registry.go` + `registry_test.go`)

**Follow-up fixes**: none directly to `registry.go`, but every later feature added a new `saveRegistry` callsite outside `m.mu` (`9cc3df2`, `907736c`, `9d04290`).

**Takeaway**: no schema-version field exists in `instances.json` — the discipline of "no migration, only `omitempty`" was set here and has held for 4 features. Don't introduce a version field now.

### Precedent: the two `_ = cmd.Wait()` callsites being enriched
**Commit(s)**:
- `9cc3df2` — "feat(processmgr): Launch background, WaitHealthy, Kill, List" (2026-04-28) — introduced `manager.go:149`
- `907736c` — "feat(processmgr): foreground launch with single-instance constraint" (2026-04-28) — introduced `manager.go:340`
- `82bde2d` — "refactor(processmgr): use errors.Is, close parent logF, document deferred sentinel" (2026-04-28) — touched but didn't fix the Wait callsite

**Follow-up fixes**:
- `fac58d5` — "refactor(processmgr): drop dead cmds map; close fgPID TOCTOU with sentinel" (5 hours after `907736c`) — foreground spawn had TOCTOU on `fgPID`; dead `cmds map[int]*exec.Cmd` field (intended for cmd tracking) was deleted

**Takeaway**: there has never been a working "track the `*exec.Cmd` for later inspection" path; previous attempt was ripped out. Design the new Wait goroutine to **own its `*exec.Cmd` via closure parameter**, never a shared map.

### Precedent: `friendlyLaunchError` sentinel-mapping pattern
**Commit(s)**: `1f608c8` — "feat(launcher): pre-launch model Stat + actionable launch error hints" (2026-04-29)
**Blast radius**: 4 files; +48 LOC of tests, one per sentinel

**Follow-up fixes**:
- `8bd7012` — "refactor(processmgr): drop stale ErrModelNotFound TODO" (3 weeks later) — comment hygiene drift

**Takeaway**: each new sentinel costs 1 manager change + 1 launcher branch + 1 test. The FRD adds a **non-sentinel enrichment path** (exit cause surfaces through `RunningInstance`, not via a new error type) — keep that path distinct from the sentinel switch so the two don't accidentally collide.

### Precedent: boot-time preflight (closest pattern for flag parsing introduction)
**Commit(s)**: `826a463` — "feat(boot): blocking modal when llama-server is missing from PATH" (2026-04-29)
**Blast radius**: 6 files
**Follow-up fixes**: none — pattern stuck.

**Takeaway**: this is the **only** prior commit that introduced "something that can fail before the TUI starts" and chose a **hard modal**, not a soft fallback. This FRD chooses silent fallback to `io.Discard` for logger-open failures — that's a divergence from precedent worth documenting in the new package.

### Precedent: boot-time concurrency drift in `recover.go`
**Commit(s)**: `60cc846` — "feat(processmgr): boot Reconcile drops zombie PIDs from instances.json" (2026-04-28)

**Follow-up fixes**:
- `2fed091` — "fix(processmgr): drop polling from Reconcile; gate KeepsLiveLlamaServer on WaitHealthy" (10 min later) — 500ms poll loop + python signal-handler deadlock
- `64c6331` — "chore(processmgr): doc Reconcile concurrency contract" (3 min after that) — had to document that Reconcile is NOT safe against concurrent Launch/Kill

**Takeaway**: every goroutine-y change in this package has needed 2 follow-up commits in the same day to fix races/deadlocks AND document the concurrency contract. **Add concurrency doc in the same commit, not a follow-up.**

### Precedent: prior logging / observability / debug attempts
**None.** Searched commits for `log|slog|zerolog|telemetry|observ|debug` — all hits relate to llama-server's per-instance log file, never to a model-loader application logger.

**Takeaway**: this is a **first-of-its-kind** change on three axes: structured logger, CLI flag parsing in `cmd/model-loader/main.go`, env-var resolution. No precedent means no rails — establish the convention once (where flags parse, where env lookup lives, precedence order) and document it next to the new `internal/log/` package.

### Composite Lessons

1. **Every goroutine added to `processmgr` has needed a same-day race/lifecycle fix.** (Precedents `9d04290`→`1f5eeeb` 17 min, `60cc846`→`2fed091`→`64c6331` 10 min total, `907736c`→`fac58d5` 5 h.) The new Wait-enrichment goroutine MUST land with: (a) `sync.Once` for any stop func, (b) snapshot-then-save-outside-lock matching `liveness.go:60`, (c) a concurrency-contract doc comment in the same commit, and (d) `defer recover()` since this is the first time these goroutines do non-trivial work.

2. **`instances.json` has no version field and no migration code — only `omitempty`.** (Precedent `04ff36c` baseline; reinforced by `9d04290`.) All four new `RunningInstance` fields must be pointer/slice types tagged `omitempty`; readers of older registries continue to load. Don't introduce a version field now.

3. **Comment/sentinel hygiene drifts when new fields land without test+doc updates in the same commit.** (Precedents `1f5eeeb` removed dead sentinel; `8bd7012` removed stale TODO 3 weeks later; `64c6331` added missing concurrency doc.) The new `internal/log/` package, 4 new `RunningInstance` fields, `friendlyLaunchError` enhancement, and AGENTS.md update should land together — or follow-up cleanup will be needed weeks later.

## Historical Context (from thoughts/)

- `thoughts/shared/discover/2026-05-15_14-15-11_debug-logging-system.md` — FRD that this research grounds; 14 functional requirements, 13 decisions, captures the original intent and pre-resolved options.

## Developer Context

**Q (discover: Sistema de log próprio do model-loader, separado do log per-instância): Escopo do novo logger: instrumentar o código Go do model-loader (TUI + services), mantendo o log per-instância do llama-server como está?**
A: Logger separado; per-instância continua.

**Q (discover: Sink: arquivo único rotacionado em `cfg.Paths.LogDir`): Sink do logger estruturado: onde os eventos são escritos?**
A: Arquivo fixo `model-loader.log` rotacionado por sessão.

**Q (discover: Capturar causa de morte do llama-server): Capturar exit code/causa quando llama-server morre (corrigir `_ = cmd.Wait()` em `manager.go:149` e `:340`)?**
A: Sim — persistir exit code, sinal, reason e `StderrTail` (50 linhas) no `RunningInstance`.

**Q (discover: Biblioteca: `log/slog` da stdlib): Biblioteca do logger — qual contrato adotar pra emitir eventos?**
A: stdlib `log/slog`.

**Q (discover: Verbosidade: `info` default + env `MODEL_LOADER_LOG_LEVEL` + flag `--log-level`): Nível default em produção e como o usuário aumenta verbosidade pra debugar?**
A: `info` default; env `MODEL_LOADER_LOG_LEVEL` + flag `--log-level`; precedência CLI > env > config > default.

**Q (discover: Escopo da instrumentação: caminho de load + lifecycle): Onde instrumentar logs nesta primeira leva?**
A: Caminho de load + lifecycle (resolve, validate, spawn, wait, liveness, reconcile, healthcheck) + os `Fprintf` existentes em `main.go` e `config.go`.

**Q (discover: Formato do handler: texto humano-legível): Formato do output do handler slog escrito no arquivo.**
A: `slog.NewTextHandler` em formato `key=value`.

**Q (discover: Tail no TUI: fora de escopo): Tail no TUI: adicionar um painel de eventos recentes no Tab Monitor?**
A: Não nesta FRD.

**Q (discover: `Fprintf(os.Stderr,...)` existentes: migrar para o logger): `fmt.Fprintf(os.Stderr,...)` em `cmd/model-loader/main.go` (12) e `internal/config/config.go:82` — o que fazer?**
A: Migrar; erros pré-`tea.Program.Run()` escrevem dual-sink (stderr + arquivo); erros pós-`Run()` ativo só vão para o arquivo.

**Q (discover: Rotação por sessão, sem `lumberjack`): Rotação do arquivo: como `model-loader.log` não cresce sem limite?**
A: Roll por sessão (renomeia `model-loader.log` existente com timestamp), cap em 5 arquivos.

**Q (discover: Tail de stderr: últimas 50 linhas em `StderrTail []string`): Tail de stderr salvo no `RunningInstance` quando llama-server crasha — quantas linhas finais capturar?**
A: Últimas 50 linhas, persistidas em `RunningInstance.StderrTail` como `[]string`.

**Q (discover: Correlação por `profile_id` + `attempt_id`): Correlação de eventos: anexar campos contextuais automáticos em todo evento de uma tentativa de launch?**
A: `attempt_id` (curto, gerado por `launchProfileCmd`) + `profile_id` via `logger.With(...)`, propagado até a goroutine de `Wait`.

**Q (discover: Segurança do TUI: logger só escreve em arquivo enquanto bubbletea roda): Quando o TUI estiver ativo, escrever em stderr pode quebrar o bubbletea — como blindar?**
A: Handler do logger aponta apenas para o arquivo; bracket pré-`Run()` é o único momento que escreve em stderr (dual-sink).

**Q (`processmgr.go:23-31` + `launcher_test.go:46-66`): Qual o shape do contrato Manager pós-FRD?**
A: +3 métodos: `Launch(p, mode, attemptID string)` + `WaitHealthy(pid, port, timeout, attemptID string)` + novo `GetExitInfo(pid int) (ExitInfo, bool)`. Blast radius limitado a 1 fake (`fakeManager` em `launcher_test.go`) + impl real (`fsManager`); MonitorPage isolado via `procMgrIface` (`monitor.go:48-53`).

**Q (`main.go:31,37,96` + `internal/log/New`): Como blindar contra perda de logs em `os.Exit(1)` (defers não rodam)?**
A: Writer unbuffered (sem `bufio.Writer`); cada `slog.Info` faz um `write(2)` completo; os 3 `os.Exit(1)` vazam só o file descriptor (kernel reapa); `defer closeFn()` cobre saída normal.

**Q (`manager.go:149` + `manager.go:340` + `liveness.go:33`): Instalar `defer recover()` nas 3 goroutines de processmgr (fora da rede de recover do bubbletea)?**
A: Sim, em todas as 3. Cada uma ganha `defer func() { if r := recover(); r != nil { logger.Error("goroutine panic", ...) } }()` no topo. Logger escreve só em arquivo (sem stderr). Documentar no AGENTS.md de processmgr.

**Q (`manager_test.go:46-78` + `recover_test.go:43-122` + `reconcile_wrapper_test.go:11-47`): Como neutralizar 3 testes MEDIUM-risk que disparam `defer mgr.Kill` e ativam o novo enrichment body?**
A: `WaitFunc func(*exec.Cmd) error` em `processmgr.Config` (padrão consistente com `Resolver` e `startLivenessWithProbe`). Testes injetam no-op via `Config{WaitFunc: func(*exec.Cmd) error { return nil }}`.

**Q (`internal/service/processmgr/AGENTS.md` + Wait body doc comment): Documentar contrato de concorrência da nova 5ª `saveRegistry` callsite no mesmo commit?**
A: Sim — atualizar AGENTS.md com seção "Wait goroutine lifecycle" listando os 5 callsites, lock order, guard `if !ok` contra Reconcile/Kill races; doc-comment de 6-8 linhas no topo da Wait body explicando snapshot-then-save + win-by-liveness guard.

## Related Research

- None — this is the first research artifact on the logging subsystem in this repo.

## Open Questions

None — all FRD pre-resolutions and research checkpoints landed concrete answers.

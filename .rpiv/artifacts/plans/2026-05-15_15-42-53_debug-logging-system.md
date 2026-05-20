---
date: 2026-05-15T15:42:53-0300
author: quantmind-br
commit: 3e39508
branch: main
repository: model-loader
topic: "Debug Logging System Implementation"
tags: [plan, processmgr, logging, slog, launcher, instances, cmd-wait]
status: ready
parent: thoughts/shared/designs/2026-05-15_15-09-16_debug-logging-system.md
last_updated: 2026-05-15T15:42:53-0300
last_updated_by: quantmind-br
---

# Debug Logging System Implementation Plan

## Overview

Introduce a stdlib `log/slog`-based file logger (`internal/log/`) injected into 4 services + 2 pages, enrich the two `cmd.Wait()` goroutines in `processmgr` to populate 4 new `omitempty` fields on `domain.RunningInstance`, and surface the captured exit cause in `friendlyLaunchError` via a new `Manager.GetExitInfo(pid)` accessor. The logger writes ONLY to a session-rotated file (`~/.local/state/model-loader/logs/model-loader.log`) — never to stderr while bubbletea owns the framebuffer — using an unbuffered `*os.File` so `os.Exit(1)` paths preserve all log lines.

Design artifact: `thoughts/shared/designs/2026-05-15_15-09-16_debug-logging-system.md` (status: complete).

## Desired End State

After all 6 phases land:

- Running `./bin/model-loader --log-level=debug` produces a structured `key=value` log at `~/.local/state/model-loader/logs/model-loader.log`.
- Each launch attempt has a single `attempt_id` (8-char base32) that grep-correlates `launch_pipeline_start` → `resolve` → `validate` → `launch_started` → `process_exited` events across the boundary into the `cmd.Wait` goroutine.
- When `WaitHealthy` times out due to llama-server crash, the LauncherPage status line surfaces the captured exit code/signal + last non-empty stderr line (rune-truncated to 80 chars).
- Each of the 3 long-lived processmgr goroutines (2 `cmd.Wait` + 1 liveness ticker) installs `defer recover()` writing panic+stack to the log file (out of bubbletea's recover net).
- The `instances.json` registry gains 4 `omitempty` fields (`exitCode`, `exitSignal`, `exitReason`, `stderrTail`) — backward + forward compatible.
- Zero new dependencies in `go.mod`.
- `go test ./... -race -count=10` passes clean.

Verification commands:
```bash
go build ./...
go vet ./...
go test ./... -race -count=1
./bin/model-loader --log-level=debug
tail -f ~/.local/state/model-loader/logs/model-loader.log
ls ~/.local/state/model-loader/logs/  # cap = 5 rotated files
```

## What We're NOT Doing

- **Tail-in-TUI** — explicit OUT of FRD-13. No `Monitor` tab event-tail panel.
- **lumberjack** — rotation lives in pure stdlib glob+sort+remove.
- **JSON handler** — `slog.NewTextHandler` (`key=value`) only.
- **`context.Context` propagation** through service methods — zero precedent; widening is via positional `attemptID string` instead.
- **MonitorPage full attempt-id wiring** — `restartCmd` generates an ID for `mgr.Launch`, but does NOT thread it through `mgr.WaitHealthy` (Monitor doesn't call WaitHealthy).
- **`p.editor` (profile_editor) validator gets a real logger** — out-of-spawn-path; uses `log.Nop()`.
- **CLI flag parsing beyond `--log-level`** — no `--help` text customization, no `--config-path`, no `--version`.
- **Per-PID stop chan in Wait goroutine** — `if !ok { return }` re-read under `m.mu` is provably sufficient.
- **Schema-version field on `instances.json`** — `omitempty` discipline holds; no migration.

---

## Phase 1: Foundation — internal/log + RunningInstance schema + ExitInfo

### Overview

Land the foundation that every later phase depends on: the new `internal/log/` package (3 files), 4 omitempty fields on `domain.RunningInstance`, and the `processmgr.ExitInfo` value type with its `readStderrTail` helper. After this phase the codebase compiles unchanged (no consumer has wired any of these yet), but the foundation is importable.

### Changes Required:

#### 1. internal/log/log.go
**File**: `internal/log/log.go`
**Changes**: NEW. Logger package: file-only slog handler, session rotation, level resolution, attempt-id generator, no-op logger.

```go
// Package log wires a stdlib log/slog logger that writes ONLY to a session-rotated
// file under cfg.Paths.LogDir. The handler is intentionally file-only because
// bubbletea owns stdout/stderr while the TUI alt-screen is active — any write to
// stderr during prog.Run() corrupts the framebuffer.
//
// The writer is unbuffered (direct *os.File.Write) so callers that hit os.Exit(1)
// preserve every log line via the kernel page cache without explicit Flush ceremony.
package log

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maxRotatedFiles caps how many timestamped logs are kept (newest-first).
const maxRotatedFiles = 5

// activeName is the live log file name; rotated files are activeName + "." + ts + ".log".
const activeName = "model-loader.log"

// rotatedGlob matches the timestamped archives (excludes the active file).
const rotatedGlob = "model-loader.*.log"

// Config drives New. Dir is the directory the logger writes into (created if missing).
// Level is the slog level — typically the result of ResolveLevel.
type Config struct {
	Dir   string
	Level slog.Level
}

// New opens a session-rotated log file under cfg.Dir and returns a *slog.Logger
// writing text-format records to it. The returned closeFn closes the file; call
// it via defer in main. An MkdirAll or OpenFile failure returns (nil, nil, err)
// — main.go is expected to write the error to stderr (TUI is not yet active)
// and os.Exit(1).
func New(cfg Config) (*slog.Logger, func(), error) {
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("mkdir log dir: %w", err)
	}
	rotate(cfg.Dir)
	active := filepath.Join(cfg.Dir, activeName)
	f, err := os.OpenFile(active, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}
	handler := slog.NewTextHandler(f, &slog.HandlerOptions{Level: cfg.Level})
	logger := slog.New(handler)
	closeFn := func() { _ = f.Close() }
	return logger, closeFn, nil
}

// rotate renames the existing active log to a timestamped name and trims the
// archive to maxRotatedFiles entries (newest-first). Best-effort: any error is
// swallowed so a partially-broken log dir still permits a fresh session.
func rotate(dir string) {
	active := filepath.Join(dir, activeName)
	if _, err := os.Stat(active); err == nil {
		ts := time.Now().UTC().Format("20060102T150405Z")
		_ = os.Rename(active, filepath.Join(dir, "model-loader."+ts+".log"))
	}
	matches, err := filepath.Glob(filepath.Join(dir, rotatedGlob))
	if err != nil {
		return
	}
	// Timestamp suffix is colon-free YYYYMMDDTHHMMSSZ; lexicographic reverse =
	// chronological newest-first. Keep first maxRotatedFiles; remove the rest.
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	for i, p := range matches {
		if i >= maxRotatedFiles {
			_ = os.Remove(p)
		}
	}
}

// ResolveLevel collapses three string sources into an slog.Level with the
// documented precedence (CLI > env > config > default "info"). Unrecognized
// values silently fall back to slog.LevelInfo — flag.Parse rejects invalid
// CLI values before this is called, and env/config drift should not crash
// the binary.
func ResolveLevel(cli, env, cfg string) slog.Level {
	for _, src := range []string{cli, env, cfg} {
		if lvl, ok := parseLevel(src); ok {
			return lvl
		}
	}
	return slog.LevelInfo
}

// parseLevel interprets "debug"|"info"|"warn"|"error" (case-insensitive).
// Empty strings return (Info, false) so ResolveLevel can skip to the next source.
func parseLevel(s string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	}
	return slog.LevelInfo, false
}

// NewAttemptID returns an 8-character correlation ID for one launch attempt.
// 5 random bytes encoded as base32 (no padding) → 8 chars from [A-Z2-7].
// Collision space ~1 in 10^12 across one session — sufficient for grep.
func NewAttemptID() string {
	var b [5]byte
	// crypto/rand.Read never errors on Linux post-Go 1.19 (getrandom(2)).
	_, _ = rand.Read(b[:])
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])
}

// Nop returns a logger that discards every record. Use for tests, for the
// nil-tolerant defaults inside service constructors, and for components that
// run outside the spawn/load path (e.g. profile_editor's validator).
func Nop() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
```

#### 2. internal/log/log_test.go
**File**: `internal/log/log_test.go`
**Changes**: NEW. Tests for rotation correctness (5-file cap), level precedence, attempt-id uniqueness, `Nop()` behavior, init-failure error path.

```go
package log

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew_CreatesLogDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "logs")
	lg, closeFn, err := New(Config{Dir: dir, Level: slog.LevelInfo})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeFn()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("log dir not created: %v", err)
	}
	lg.Info("hello", "k", "v")
	data, err := os.ReadFile(filepath.Join(dir, activeName))
	if err != nil {
		t.Fatalf("read active log: %v", err)
	}
	if !strings.Contains(string(data), `msg=hello`) {
		t.Errorf("expected msg=hello in log; got %q", string(data))
	}
}

func TestNew_RotatesExistingLog(t *testing.T) {
	dir := t.TempDir()
	// Pre-create an active log with sentinel content.
	if err := os.WriteFile(filepath.Join(dir, activeName), []byte("OLD\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, closeFn, err := New(Config{Dir: dir, Level: slog.LevelInfo})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeFn()
	matches, _ := filepath.Glob(filepath.Join(dir, rotatedGlob))
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 rotated file, got %d: %v", len(matches), matches)
	}
	rotated, _ := os.ReadFile(matches[0])
	if string(rotated) != "OLD\n" {
		t.Errorf("expected rotated content 'OLD\\n'; got %q", string(rotated))
	}
	active, _ := os.ReadFile(filepath.Join(dir, activeName))
	if len(active) != 0 {
		t.Errorf("expected fresh active log; got %d bytes", len(active))
	}
}

func TestNew_Caps5Files(t *testing.T) {
	dir := t.TempDir()
	seed := []string{
		"model-loader.20260101T000001Z.log",
		"model-loader.20260102T000002Z.log",
		"model-loader.20260103T000003Z.log",
		"model-loader.20260104T000004Z.log",
		"model-loader.20260105T000005Z.log",
		"model-loader.20260106T000006Z.log",
	}
	for _, n := range seed {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644); err != nil {
			t.Fatalf("seed %s: %v", n, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, activeName), []byte("live"), 0o644); err != nil {
		t.Fatalf("seed active: %v", err)
	}
	_, closeFn, err := New(Config{Dir: dir, Level: slog.LevelInfo})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeFn()
	matches, _ := filepath.Glob(filepath.Join(dir, rotatedGlob))
	if len(matches) != 5 {
		t.Fatalf("expected 5 rotated files after cap, got %d: %v", len(matches), matches)
	}
	for _, m := range matches {
		if strings.Contains(m, "20260101T000001Z") || strings.Contains(m, "20260102T000002Z") {
			t.Errorf("expected oldest archives removed, found %s", m)
		}
	}
}

func TestNew_MkdirAllFailureReturnsError(t *testing.T) {
	parent := t.TempDir()
	block := filepath.Join(parent, "block")
	if err := os.WriteFile(block, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	lg, closeFn, err := New(Config{Dir: filepath.Join(block, "logs"), Level: slog.LevelInfo})
	if err == nil {
		closeFn()
		t.Fatalf("expected error, got logger=%v", lg)
	}
	if lg != nil || closeFn != nil {
		t.Errorf("expected nil logger and nil closeFn on error, got %v %v", lg, closeFn)
	}
}

func TestResolveLevel_Precedence(t *testing.T) {
	cases := []struct {
		name          string
		cli, env, cfg string
		want          slog.Level
	}{
		{"all_empty", "", "", "", slog.LevelInfo},
		{"cli_wins", "debug", "warn", "error", slog.LevelDebug},
		{"env_wins_when_no_cli", "", "warn", "error", slog.LevelWarn},
		{"cfg_wins_when_no_cli_env", "", "", "error", slog.LevelError},
		{"case_insensitive", "DEBUG", "", "", slog.LevelDebug},
		{"warning_alias", "warning", "", "", slog.LevelWarn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveLevel(tc.cli, tc.env, tc.cfg); got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestResolveLevel_UnrecognizedFallsBackToInfo(t *testing.T) {
	if got := ResolveLevel("garbage", "junk", "unknown"); got != slog.LevelInfo {
		t.Errorf("want LevelInfo for all-unrecognized, got %v", got)
	}
}

func TestNewAttemptID_8CharsBase32(t *testing.T) {
	id := NewAttemptID()
	if len(id) != 8 {
		t.Errorf("want 8 chars, got %d: %q", len(id), id)
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	for _, r := range id {
		if !strings.ContainsRune(alphabet, r) {
			t.Errorf("non-base32 rune %q in id %q", r, id)
		}
	}
}

func TestNewAttemptID_LowCollisionRate(t *testing.T) {
	seen := make(map[string]struct{}, 10_000)
	for i := 0; i < 10_000; i++ {
		id := NewAttemptID()
		if _, dup := seen[id]; dup {
			t.Fatalf("collision at i=%d: id=%q", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestNop_WritesNothing(t *testing.T) {
	lg := Nop()
	if lg == nil {
		t.Fatal("Nop returned nil")
	}
	lg.Debug("x")
	lg.Info("x")
	lg.Warn("x")
	lg.Error("x")
}
```

#### 3. internal/log/AGENTS.md
**File**: `internal/log/AGENTS.md`
**Changes**: NEW. Package contract docs.

```markdown
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
```

#### 4. internal/domain/instance.go
**File**: `internal/domain/instance.go`
**Changes**: MODIFY — add 4 omitempty fields (`ExitCode *int`, `ExitSignal string`, `ExitReason string`, `StderrTail []string`) to `RunningInstance`.

```go
// RunningInstance describes a live llama-server process tracked by ProcessManager.
type RunningInstance struct {
	ProfileID  string     `json:"profileId"`
	PID        int        `json:"pid"`
	Port       int        `json:"port"`
	LogPath    string     `json:"logPath"`
	BinaryPath string     `json:"binaryPath,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	Background bool       `json:"background"`
	Crashed    bool       `json:"crashed,omitempty"`
	ExitedAt   *time.Time `json:"exitedAt,omitempty"`
	// ExitCode is the process exit status when known. Set by processmgr's
	// cmd.Wait goroutine; nil for processes that are still alive or whose
	// exit was signal-only.
	ExitCode *int `json:"exitCode,omitempty"`
	// ExitSignal is the signal name (e.g. "SIGSEGV") when the process was
	// killed by a signal; "" otherwise.
	ExitSignal string `json:"exitSignal,omitempty"`
	// ExitReason is a human-readable cause: "exit:1", "signal:SIGTERM", or
	// "unknown" when cmd.Wait returned a non-ExitError.
	ExitReason string `json:"exitReason,omitempty"`
	// StderrTail is the last 50 lines of the process' log file at exit,
	// captured by the Wait goroutine for friendlyLaunchError enrichment.
	StderrTail []string `json:"stderrTail,omitempty"`
}
```

#### 5. internal/service/processmgr/exit_info.go
**File**: `internal/service/processmgr/exit_info.go`
**Changes**: NEW. `ExitInfo` value type + `stderrTailLines` constant + `readStderrTail` helper.

```go
package processmgr

import (
	"bytes"
	"os"
)

// ExitInfo is the per-PID record populated by the cmd.Wait enrichment
// goroutine and read by LauncherPage.handleLaunchErr via
// Manager.GetExitInfo(pid). It is an ephemeral in-memory mirror of the four
// new RunningInstance fields — kept separate so the Manager interface does
// not leak the full RunningInstance shape to UI consumers.
type ExitInfo struct {
	ExitCode   *int
	ExitSignal string
	ExitReason string
	StderrTail []string
}

// stderrTailLines is the number of trailing log lines captured per exit.
// Matches the FRD requirement of 50.
const stderrTailLines = 50

// readStderrTail reads the on-disk log file and returns up to maxLines of
// trailing non-empty lines. Returns nil on any error — the enrichment is
// best-effort and must never block or panic the Wait goroutine.
//
// This runs OUTSIDE the manager's mutex so file I/O does not serialize
// Launch/Kill against a slow log volume.
func readStderrTail(logPath string, maxLines int) []string {
	if logPath == "" || maxLines <= 0 {
		return nil
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	data = bytes.TrimRight(data, "\n")
	if len(data) == 0 {
		return nil
	}
	lines := bytes.Split(data, []byte{'\n'})
	start := 0
	if len(lines) > maxLines {
		start = len(lines) - maxLines
	}
	out := make([]string, 0, len(lines)-start)
	for _, ln := range lines[start:] {
		out = append(out, string(ln))
	}
	return out
}
```

### Success Criteria:

#### Automated Verification:
- [x] Build passes: `go build ./...`
- [x] Vet clean: `go vet ./...`
- [x] `internal/log` tests pass: `go test ./internal/log/... -v -count=1`
- [x] Rotation cap test: rotation keeps exactly 5 archives when 6+ exist
- [x] Level precedence test: CLI > env > config > default `info`
- [x] Attempt-ID uniqueness: 10,000 IDs with zero collisions
- [x] No new dependencies in go.mod: `git diff go.mod` shows no changes
- [x] Full test suite still passes: `go test ./... -count=1`

#### Manual Verification:
- [x] `cat internal/log/AGENTS.md` documents file-only sink, unbuffered-writer rationale, and Nop() pattern
- [x] `internal/domain/instance.go` has 4 new fields all with `omitempty` JSON tags
- [x] `internal/service/processmgr/exit_info.go` defines `stderrTailLines = 50`

---

## Phase 2: Manager API Widening

### Overview

Widen `processmgr.Manager` interface (+3 methods, signature changes), grow `processmgr.Config` (+2 fields), thread the new `Logger`/`WaitFunc` into `fsManager`, and ripple the signature change into `procMgrIface` (`monitor.go`) plus all 4 fake implementations across 2 test files. After this phase the codebase still compiles & tests pass — the new fields/methods exist but the Wait body still calls plain `cmd.Wait()`. The new `attemptID` parameter is unused at the call sites (passed `""` until Phase 5).

### Changes Required:

#### 1. internal/service/processmgr/processmgr.go
**File**: `internal/service/processmgr/processmgr.go`
**Changes**: MODIFY — widen `Manager` interface: `Launch(p, mode, attemptID)`, `WaitHealthy(pid, port, timeout, attemptID)`, new `GetExitInfo(pid) (ExitInfo, bool)`.

```go
// Manager owns the lifecycle of llama-server processes.
type Manager interface {
	// Launch spawns the configured backend. attemptID is the correlation ID
	// emitted by the calling page (LauncherPage.launchProfileCmd or
	// MonitorPage.restartCmd) and threaded into every log event the manager
	// emits for this PID. Empty attemptID is permitted but breaks grep-ability.
	Launch(p domain.Profile, mode LaunchMode, attemptID string) (domain.RunningInstance, error)
	Kill(pid int) error
	List() []domain.RunningInstance
	// WaitHealthy polls the /health endpoint until 200 OK or timeout.
	// attemptID matches the one passed to Launch so the two log streams
	// can be correlated by grep attempt_id=...
	WaitHealthy(pid, port int, timeout time.Duration, attemptID string) error
	TailLogs(pid int) (io.ReadCloser, error)
	Close() error
	// GetExitInfo returns the captured exit cause for pid if the Wait
	// goroutine has populated it. Returns (zero, false) when the process is
	// still alive or when Wait has not yet observed the exit (TOCTOU window
	// around the 30s WaitHealthy timeout). Best-effort — callers must fall
	// back to a generic message when ok=false.
	GetExitInfo(pid int) (ExitInfo, bool)
}
```

#### 2. internal/service/processmgr/manager.go
**File**: `internal/service/processmgr/manager.go`
**Changes**: MODIFY (Slice 2 portion only) — `Config` gains `Logger *slog.Logger` + `WaitFunc func(*exec.Cmd) error`; `fsManager` struct gains `logger`, `waitFunc`, `exitInfos`; `New` nil-guards Logger/WaitFunc; `Launch` / `launchForeground` / `WaitHealthy` signatures accept `attemptID`; new `GetExitInfo` accessor reading `m.exitInfos` under `m.mu`. **Wait body itself stays unchanged in this phase** — the goroutine still spawns `go func() { _ = cmd.Wait() }()` (Phase 3 swaps it out).

```go
// fsManager is the default Manager implementation backed by os/exec.
type fsManager struct {
	resolver      func(domain.Profile) (string, error)
	defaultBinary string
	logDir        string
	registryPath  string
	sink          LastUsedSink
	logger        *slog.Logger
	waitFunc      func(*exec.Cmd) error

	mu        sync.Mutex
	tracked   map[int]domain.RunningInstance
	exitInfos map[int]ExitInfo
	fgPID     int

	livenessStop func()
}

// Config holds wiring for New.
type Config struct {
	Resolver      func(domain.Profile) (string, error)
	DefaultBinary string
	LogDir        string
	RegistryPath  string
	LastUsedSink  LastUsedSink
	// Logger receives lifecycle events. nil → log.Nop().
	Logger *slog.Logger
	// WaitFunc replaces (*exec.Cmd).Wait. Tests inject a no-op to neutralize
	// the cmd.Wait enrichment body that runs in a goroutine spawned by
	// Launch/launchForeground. nil → (*exec.Cmd).Wait.
	WaitFunc func(*exec.Cmd) error
}

// New constructs a Manager. nil-tolerant for Logger and WaitFunc.
func New(cfg Config) *fsManager {
	if cfg.Logger == nil {
		cfg.Logger = log.Nop()
	}
	if cfg.WaitFunc == nil {
		cfg.WaitFunc = (*exec.Cmd).Wait
	}
	m := &fsManager{
		resolver:      cfg.Resolver,
		defaultBinary: cfg.DefaultBinary,
		logDir:        cfg.LogDir,
		registryPath:  cfg.RegistryPath,
		sink:          cfg.LastUsedSink,
		logger:        cfg.Logger,
		waitFunc:      cfg.WaitFunc,
		tracked:       map[int]domain.RunningInstance{},
		exitInfos:     map[int]ExitInfo{},
	}
	if m.defaultBinary == "" {
		m.defaultBinary = "llama-server"
	}
	m.livenessStop = m.startLiveness()
	return m
}

// Launch signature widens to accept attemptID. Phase 2 keeps the legacy
// goroutine body (go func() { _ = cmd.Wait() }()) — Phase 3 swaps it for
// m.waitEnrichment(...). attemptID is accepted but unused in Phase 2.
func (m *fsManager) Launch(p domain.Profile, mode LaunchMode, attemptID string) (domain.RunningInstance, error) {
	// body unchanged from current main except the goroutine call is still
	// go func() { _ = cmd.Wait() }() (Phase 3 replaces it).
	// All current callsites still pass through the same flow.
}

// WaitHealthy signature widens to accept attemptID. Body unchanged in
// Phase 2; logging is added in Phase 3 (uses m.logger).
func (m *fsManager) WaitHealthy(pid int, port int, timeout time.Duration, attemptID string) error {
	// body unchanged from current main except signature widening.
}

// launchForeground signature widens to accept attemptID. Body unchanged in
// Phase 2 — the goroutine body stays go func() { _ = cmd.Wait() }() until
// Phase 3 moves it post-insert AND replaces with m.waitEnrichment.
func (m *fsManager) launchForeground(p domain.Profile, port int, attemptID string) (domain.RunningInstance, error) {
	// body unchanged from current main.
}

// GetExitInfo returns the captured exit cause for pid. In Phase 2 the
// m.exitInfos map exists but is empty (Phase 3 populates it via
// waitEnrichment). Always returns (zero, false) until Phase 3 lands.
func (m *fsManager) GetExitInfo(pid int) (ExitInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ei, ok := m.exitInfos[pid]
	return ei, ok
}
```

New imports for `manager.go`:

```go
import (
	// existing imports preserved...
	"log/slog"

	"github.com/quantmind-br/model-loader/internal/log"
)
```

#### 3. internal/ui/pages/monitor.go
**File**: `internal/ui/pages/monitor.go`
**Changes**: MODIFY — `procMgrIface.Launch` signature parallel-update to `Launch(domain.Profile, processmgr.LaunchMode, string)`. `restartCmd` passes `""` for now (Phase 5 swaps to `log.NewAttemptID()`).

```go
// procMgrIface is the slice of processmgr.Manager that MonitorPage needs.
type procMgrIface interface {
	List() []domain.RunningInstance
	Kill(pid int) error
	Launch(domain.Profile, processmgr.LaunchMode, string) (domain.RunningInstance, error)
	TailLogs(pid int) (io.ReadCloser, error)
}

// restartCmd performs Kill then Launch off the UI thread. Phase 2 passes
// "" for attemptID; Phase 5 swaps to log.NewAttemptID() (when monitor.go
// gains its internal/log import for restart correlation).
func restartCmd(pm procMgrIface, pid int, prof domain.Profile, bg bool) tea.Cmd {
	return func() tea.Msg {
		if err := pm.Kill(pid); err != nil {
			return restartResultMsg{pid: pid, err: fmt.Errorf("kill: %w", err)}
		}
		mode := processmgr.LaunchBackground
		if !bg {
			mode = processmgr.LaunchForeground
		}
		if _, err := pm.Launch(prof, mode, ""); err != nil {
			return restartResultMsg{pid: pid, err: fmt.Errorf("launch: %w", err)}
		}
		return restartResultMsg{pid: pid}
	}
}
```

#### 4. internal/ui/pages/launcher_test.go
**File**: `internal/ui/pages/launcher_test.go`
**Changes**: MODIFY (Slice 2 portion only) — `fakeManager` widens `Launch` and `WaitHealthy`, gains `exitInfos` map + new `GetExitInfo` method. The 3 new tests + validator.New site update land in Phase 5/Phase 4 respectively.

```go
type fakeManager struct {
	launched []domain.Profile
	mode     processmgr.LaunchMode
	nextErr  error
	// exitInfos lets Phase 5 tests stub the Wait-enrichment path keyed by
	// pid. Default nil → every GetExitInfo returns ok=false (current
	// behavior for all existing test cases).
	exitInfos map[int]processmgr.ExitInfo
}

func (f *fakeManager) Launch(p domain.Profile, mode processmgr.LaunchMode, _ string) (domain.RunningInstance, error) {
	if f.nextErr != nil {
		err := f.nextErr
		f.nextErr = nil
		return domain.RunningInstance{}, err
	}
	f.launched = append(f.launched, p)
	f.mode = mode
	return domain.RunningInstance{ProfileID: p.ID, PID: 4242, Port: 8080, Background: mode == processmgr.LaunchBackground}, nil
}
func (f *fakeManager) Kill(pid int) error                                    { return nil }
func (f *fakeManager) List() []domain.RunningInstance                        { return nil }
func (f *fakeManager) WaitHealthy(_, _ int, _ time.Duration, _ string) error { return nil }
func (f *fakeManager) TailLogs(_ int) (io.ReadCloser, error)                 { return nil, processmgr.ErrUnknownPID }
func (f *fakeManager) Close() error                                          { return nil }
func (f *fakeManager) GetExitInfo(pid int) (processmgr.ExitInfo, bool) {
	if f.exitInfos == nil {
		return processmgr.ExitInfo{}, false
	}
	ei, ok := f.exitInfos[pid]
	return ei, ok
}
```

#### 5. internal/ui/pages/monitor_test.go
**File**: `internal/ui/pages/monitor_test.go`
**Changes**: MODIFY — `fakeProcMgr.Launch` and `restartTrackingMgr.Launch` widen in parallel with `procMgrIface`. `killTrackingMgr` embeds `fakeProcMgr` so its method is promoted — zero edits there.

```go
// internal/ui/pages/monitor_test.go:25 — fakeProcMgr.Launch widens
func (f *fakeProcMgr) Launch(p domain.Profile, m processmgr.LaunchMode, _ string) (domain.RunningInstance, error) {
	// body unchanged — see on-disk monitor_test.go for full impl.
}

// internal/ui/pages/monitor_test.go:583 — restartTrackingMgr.Launch widens
func (r *restartTrackingMgr) Launch(p domain.Profile, mode processmgr.LaunchMode, _ string) (domain.RunningInstance, error) {
	// body unchanged — see on-disk monitor_test.go for full impl.
}

// killTrackingMgr embeds fakeProcMgr; its Launch is promoted from the
// widened embedded method — zero edits needed there.
```

### Success Criteria:

#### Automated Verification:
- [x] Build passes: `go build ./...`
- [x] Vet clean: `go vet ./...`
- [x] processmgr tests pass: `go test ./internal/service/processmgr/... -count=1`
- [x] pages tests pass: `go test ./internal/ui/pages/... -count=1`
- [x] No race regressions: `go test ./internal/service/processmgr/... -race -count=3`
- [x] Full test suite passes: `go test ./... -count=1`
- [x] Interface widening visible: `grep -c "GetExitInfo" internal/service/processmgr/processmgr.go` returns 1+

#### Manual Verification:
- [x] `processmgr.Manager` interface lists 7 methods (was 6)
- [x] `processmgr.Config` shows `Logger` and `WaitFunc` fields with godoc comments
- [x] `procMgrIface` in `monitor.go` declares `Launch(domain.Profile, processmgr.LaunchMode, string)`

---

## Phase 3: cmd.Wait Enrichment + Panic Guards + AGENTS.md

### Overview

The riskiest phase: replace both `cmd.Wait()` reapers with `m.waitEnrichment(...)`, install `defer recover()` on all 3 long-lived processmgr goroutines (2 Wait + 1 liveness), move foreground spawn from pre-insert to post-insert, add 5th `saveRegistry` callsite with snapshot-then-save-outside-lock pattern, and document the contract in `processmgr/AGENTS.md`. **Three test files** (`manager_test.go`, `recover_test.go`, `reconcile_wrapper_test.go`) gain `WaitFunc: func(*exec.Cmd) error { return nil }` no-ops to neutralize the new enrichment body against `fake-llama-server.sh`.

**This phase is file-disjoint from Phase 4** and can be executed in parallel after Phase 2.

### Changes Required:

#### 1. internal/service/processmgr/manager.go
**File**: `internal/service/processmgr/manager.go`
**Changes**: MODIFY (Slice 3 portion) — replace `go func() { _ = cmd.Wait() }()` at both background spawn AND foreground spawn with `go m.waitEnrichment(cmd, pid, logPath, attemptID)`. Add new `waitEnrichment` and `extractExit` methods. Move foreground spawn to AFTER `m.tracked[pid]=inst` insert. Add structured logging via `m.logger` to `Launch`, `WaitHealthy`, `launchForeground`. New imports: `runtime/debug`, `syscall`. `log/slog` and `internal/log` already imported in Phase 2.

```go
// Launch — background path. Goroutine is spawned AFTER
// m.tracked[pid]=inst insert (already correct on background path). The
// reaper call is replaced with m.waitEnrichment.
func (m *fsManager) Launch(p domain.Profile, mode LaunchMode, attemptID string) (domain.RunningInstance, error) {
	if p.Model == "" {
		return domain.RunningInstance{}, ErrModelNotFound
	}
	_, err := os.Stat(p.Model)
	if err != nil {
		if !looksLikeHFRepo(p.Model) {
			if errors.Is(err, fs.ErrNotExist) {
				return domain.RunningInstance{}, fmt.Errorf("%w: %s", ErrModelNotFound, p.Model)
			}
			return domain.RunningInstance{}, fmt.Errorf("stat model: %w", err)
		}
	}
	port, ok := portFromProfile(p)
	if !ok {
		return domain.RunningInstance{}, fmt.Errorf("profile %q: missing or invalid port arg", p.ID)
	}
	if err := checkPortFree(port); err != nil {
		return domain.RunningInstance{}, err
	}
	if mode == LaunchForeground {
		return m.launchForeground(p, port, attemptID)
	}

	if err := os.MkdirAll(m.logDir, 0o755); err != nil {
		return domain.RunningInstance{}, fmt.Errorf("mkdir log dir: %w", err)
	}
	resolvedBinary := p.Launch.ResolvedExecutable
	if resolvedBinary == "" {
		var err error
		resolvedBinary, err = m.resolver(p)
		if err != nil {
			return domain.RunningInstance{}, fmt.Errorf("resolve backend executable: %w", err)
		}
	}
	logPath := filepath.Join(m.logDir, fmt.Sprintf("%s-%d.log", p.ID, port))
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return domain.RunningInstance{}, fmt.Errorf("open log: %w", err)
	}

	profileArgs, err := BuildArgsForBackend(p, p.Launch.ResolvedBackendKind, resolvedBinary)
	if err != nil {
		_ = logF.Close()
		return domain.RunningInstance{}, fmt.Errorf("build args: %w", err)
	}
	cmd := makeCommand(resolvedBinary, profileArgs)
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		_ = logF.Close()
		return domain.RunningInstance{}, fmt.Errorf("start process: %w", err)
	}
	_ = logF.Close()

	inst := domain.RunningInstance{
		ProfileID:  p.ID,
		PID:        cmd.Process.Pid,
		Port:       port,
		LogPath:    logPath,
		BinaryPath: resolvedBinary,
		StartedAt:  time.Now().UTC(),
		Background: true,
	}

	m.mu.Lock()
	m.tracked[inst.PID] = inst
	all := snapshotLocked(m.tracked)
	m.mu.Unlock()

	m.logger.Info("launch_started",
		"pid", inst.PID, "port", inst.Port,
		"profile_id", p.ID, "attempt_id", attemptID,
		"mode", "background", "binary", resolvedBinary)

	// Wait-enrichment goroutine. See AGENTS.md "Wait goroutine lifecycle".
	go m.waitEnrichment(cmd, inst.PID, logPath, attemptID)

	if err := saveRegistry(m.registryPath, all); err != nil {
		return inst, fmt.Errorf("instance started (pid %d) but registry save failed: %w", inst.PID, err)
	}
	return inst, nil
}

// WaitHealthy gains structured logging via m.logger.With(...).
func (m *fsManager) WaitHealthy(pid int, port int, timeout time.Duration, attemptID string) error {
	lg := m.logger.With("pid", pid, "port", port, "attempt_id", attemptID)
	lg.Info("healthcheck_start", "timeout", timeout)
	deadline := time.Now().Add(timeout)
	delay := 100 * time.Millisecond
	const maxDelay = time.Second
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				if m.sink != nil {
					m.mu.Lock()
					inst, ok := m.tracked[pid]
					m.mu.Unlock()
					if ok {
						_ = m.sink.MarkLastUsed(inst.ProfileID, time.Now().UTC())
					}
				}
				lg.Info("healthcheck_ok")
				return nil
			}
		}
		time.Sleep(delay)
		if delay < maxDelay {
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		}
	}
	lg.Warn("healthcheck_timeout")
	return fmt.Errorf("port %d: %w", port, ErrHealthCheckTimeout)
}

// launchForeground — the cmd.Wait goroutine is now spawned AFTER the
// m.tracked[pid]=inst insert + registry-snapshot-under-lock (MOVED from
// its original pre-insert position).
func (m *fsManager) launchForeground(p domain.Profile, port int, attemptID string) (domain.RunningInstance, error) {
	resolvedBinary := p.Launch.ResolvedExecutable
	if resolvedBinary == "" {
		var err error
		resolvedBinary, err = m.resolver(p)
		if err != nil {
			return domain.RunningInstance{}, fmt.Errorf("resolve backend executable: %w", err)
		}
	}

	m.mu.Lock()
	if m.fgPID != 0 {
		m.mu.Unlock()
		return domain.RunningInstance{}, ErrForegroundBusy
	}
	m.fgPID = -1
	m.mu.Unlock()

	profileArgs, err := BuildArgsForBackend(p, p.Launch.ResolvedBackendKind, resolvedBinary)
	if err != nil {
		return domain.RunningInstance{}, fmt.Errorf("build args: %w", err)
	}
	cmd := makeCommand(resolvedBinary, profileArgs)
	if err := cmd.Start(); err != nil {
		m.mu.Lock()
		m.fgPID = 0
		m.mu.Unlock()
		return domain.RunningInstance{}, fmt.Errorf("start process (fg): %w", err)
	}

	inst := domain.RunningInstance{
		ProfileID:  p.ID,
		PID:        cmd.Process.Pid,
		Port:       port,
		LogPath:    "",
		BinaryPath: resolvedBinary,
		StartedAt:  time.Now().UTC(),
		Background: false,
	}

	m.mu.Lock()
	m.tracked[inst.PID] = inst
	m.fgPID = inst.PID
	all := snapshotLocked(m.tracked)
	m.mu.Unlock()

	m.logger.Info("launch_started",
		"pid", inst.PID, "port", inst.Port,
		"profile_id", p.ID, "attempt_id", attemptID,
		"mode", "foreground", "binary", resolvedBinary)

	// MOVED from pre-Start to here (post-insert) so the body can re-read
	// m.tracked[inst.PID] under m.mu.
	go m.waitEnrichment(cmd, inst.PID, "", attemptID)

	if err := saveRegistry(m.registryPath, all); err != nil {
		return inst, fmt.Errorf("fg started but registry save failed: %w", err)
	}
	return inst, nil
}

// waitEnrichment is the body of both cmd.Wait reaper goroutines. Runs
// OUTSIDE m.mu for wait + tail-read, then acquires the lock only to
// mutate m.tracked and m.exitInfos. See AGENTS.md "Wait goroutine
// lifecycle" for the full concurrency contract.
func (m *fsManager) waitEnrichment(cmd *exec.Cmd, pid int, logPath string, attemptID string) {
	defer func() {
		if r := recover(); r != nil {
			m.logger.Error("wait_goroutine_panic",
				"pid", pid, "attempt_id", attemptID,
				"panic", r, "stack", string(debug.Stack()))
		}
	}()

	waitErr := m.waitFunc(cmd)
	exitCode, sig, reason := extractExit(waitErr, cmd.ProcessState)
	tail := readStderrTail(logPath, stderrTailLines)

	m.mu.Lock()
	cur, ok := m.tracked[pid]
	if !ok {
		m.mu.Unlock()
		m.logger.Debug("wait_exited_after_untrack",
			"pid", pid, "attempt_id", attemptID, "exit_reason", reason)
		return
	}
	if !cur.Crashed {
		now := time.Now().UTC()
		cur.ExitedAt = &now
		cur.Crashed = true
	}
	cur.ExitCode = exitCode
	cur.ExitSignal = sig
	cur.ExitReason = reason
	cur.StderrTail = tail
	m.tracked[pid] = cur
	m.exitInfos[pid] = ExitInfo{
		ExitCode:   exitCode,
		ExitSignal: sig,
		ExitReason: reason,
		StderrTail: tail,
	}
	if m.fgPID == pid {
		m.fgPID = 0
	}
	snap := snapshotLocked(m.tracked)
	m.mu.Unlock()

	// 5th out-of-lock saveRegistry callsite. See AGENTS.md.
	_ = saveRegistry(m.registryPath, snap)

	m.logger.Info("process_exited",
		"pid", pid, "attempt_id", attemptID,
		"exit_reason", reason,
		"stderr_tail_lines", len(tail))
}

// extractExit interprets the *exec.Cmd.Wait error + ProcessState into a
// (code, signal, reason) triple. Linux-only assumption via syscall.WaitStatus
// is acceptable because recover.go is already Linux-only (uses /proc).
// The , ok guard makes the assertion fail gracefully on other platforms.
func extractExit(waitErr error, ps *os.ProcessState) (*int, string, string) {
	if ps == nil {
		return nil, "", "unknown"
	}
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		sig := ws.Signal()
		return nil, sig.String(), "signal:" + sig.String()
	}
	code := ps.ExitCode()
	if code < 0 {
		return nil, "", "unknown"
	}
	_ = waitErr
	return &code, "", fmt.Sprintf("exit:%d", code)
}
```

New imports for `manager.go` (additive to Phase 2):

```go
import (
	// existing imports preserved...
	"runtime/debug"
	"syscall"
)
```

#### 2. internal/service/processmgr/liveness.go
**File**: `internal/service/processmgr/liveness.go`
**Changes**: MODIFY — wrap ticker goroutine body in `defer recover()` that logs to `m.logger`. New import: `runtime/debug`. Add `m.logger.Info("liveness_crash_detected", ...)` inside the crash-detection branch.

```go
func (m *fsManager) startLivenessWithProbe(interval time.Duration, probe func(int) bool) func() {
	stop := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				m.logger.Error("liveness_goroutine_panic",
					"panic", r, "stack", string(debug.Stack()))
			}
		}()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-t.C:
				m.mu.Lock()
				dirty := false
				nowUTC := now.UTC()
				for pid, inst := range m.tracked {
					if inst.Crashed {
						continue
					}
					if probe(pid) {
						continue
					}
					ts := nowUTC
					inst.Crashed = true
					inst.ExitedAt = &ts
					m.tracked[pid] = inst
					dirty = true
					m.logger.Info("liveness_crash_detected",
						"pid", pid, "profile_id", inst.ProfileID)
				}
				snapshot := snapshotLocked(m.tracked)
				m.mu.Unlock()
				if dirty {
					_ = saveRegistry(m.registryPath, snapshot)
				}
			}
		}
	}()
	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() { close(stop) })
	}
}
```

New import:

```go
import (
	// existing imports preserved...
	"runtime/debug"
)
```

#### 3. internal/service/processmgr/AGENTS.md
**File**: `internal/service/processmgr/AGENTS.md`
**Changes**: MODIFY — add new `## WAIT GOROUTINE LIFECYCLE` section (5-callsite saveRegistry table, win-by-liveness contract, panic-safety subsection, "What NOT to do" subsection) and 2 new entries to existing ANTI-PATTERNS list.

```markdown
## WAIT GOROUTINE LIFECYCLE

Two `cmd.Wait` reaper goroutines run per spawned process:

1. **Background**: spawned in `Launch` AFTER the `m.tracked[pid]=inst`
   insert. Captures `pid`, `logPath`, and the `attemptID` passed in by
   the caller via the method receiver `m`.
2. **Foreground**: spawned in `launchForeground` AFTER the
   `m.tracked[pid]=inst` insert + registry-snapshot-under-lock. The
   historical ordering (pre-insert) was reordered so the enrichment body's
   re-read of `m.tracked[pid]` sees a populated entry.

Both bodies are `m.waitEnrichment(cmd, pid, logPath, attemptID)` and follow
this contract:

- **Wait + file I/O happen OUTSIDE `m.mu`.** Stderr tail (50 lines) is read
  via `os.ReadFile` before any lock is acquired so a slow log volume does
  not serialize Launch / Kill / liveness.
- **Re-read `m.tracked[pid]` under `m.mu`.** If `!ok` the entry was deleted
  by Kill or wholesale-replaced by Reconcile. We exit without mutating
  either map. This is the SOLE race defense — no stop chan is needed
  because `cmd.Wait` returns when the process dies and Kill causes process
  death, so the goroutine always drains naturally.
- **Win-by-liveness guard.** The 5-second liveness ticker also mutates
  `m.tracked` under the same lock and sets `Crashed=true` + `ExitedAt`. If
  `cur.Crashed` is already true when `waitEnrichment` acquires the lock,
  we KEEP liveness's `ExitedAt` and only fill the four fields liveness
  cannot observe via PID probe alone (`ExitCode`, `ExitSignal`,
  `ExitReason`, `StderrTail`).
- **`saveRegistry` is called outside the lock.** This is the **5th** of
  the out-of-lock callsites. Pattern is identical to `liveness.go:60`.

### Out-of-lock saveRegistry callsites

`saveRegistry` is invoked from 5 sites, ALL of them after `m.mu.Unlock()`,
following snapshot-under-lock semantics:

| File | Caller | Note |
|------|--------|------|
| `manager.go` | `Launch` (background, post-insert) | original |
| `manager.go` | `Kill` (post-delete) | original |
| `manager.go` | `launchForeground` (post-insert) | original |
| `liveness.go` | Liveness ticker (post-Crashed mutation) | original |
| `manager.go` | `waitEnrichment` (post-exit fields mutation) | NEW |

Holding `m.mu` across file I/O would serialize launches against a
possibly slow JSON write. Trade-off: two concurrent mutators may produce
disagreeing on-disk states between their two saves — but Reconcile reads
disk only at boot, and at that point any in-flight mutation has already
fully serialized.

### Goroutine panic safety

bubbletea v1.3.10 wraps every `tea.Cmd` invocation in `defer recover()`,
so panics inside `launchProfileCmd`'s returned closure are contained. BUT
the three long-lived goroutines spawned by `processmgr` (2 `cmd.Wait` +
1 liveness) are outside that net. A panic in any of them would corrupt
the alt-screen (default Go runtime handler prints to stdout before
`os.Exit(2)`, bypassing defers).

All three goroutines install:

```go
defer func() {
    if r := recover(); r != nil {
        m.logger.Error("<name>_goroutine_panic",
            "panic", r, "stack", string(debug.Stack()))
    }
}()
```

The logger writes file-only — stderr stays untouched, the TUI framebuffer
survives, and the panic is grep-able in `model-loader.log`.

### What NOT to do

- DO NOT use a per-PID stop chan to "cancel" the Wait goroutine.
  `cmd.Wait` blocks on `syscall.Wait`, which returns when the kernel
  delivers `SIGCHLD`. Killing the process is the cancel signal.
- DO NOT hold `m.mu` across the `readStderrTail(logPath, 50)` call. The
  lock budget is microseconds; file I/O is milliseconds-to-seconds.
- DO NOT overwrite `cur.ExitedAt` if `cur.Crashed` is already true.
  Liveness owns the timestamp once it has fired; the Wait body fills only
  the four exit-cause fields.
```

New ANTI-PATTERN entries (append to existing list):

```markdown
- DO NOT add a 6th out-of-lock `saveRegistry` callsite without documenting
  it in the "Wait goroutine lifecycle" table above. The pattern is
  intentional (lock budget vs file I/O latency); the count is the contract.
- DO NOT spawn a new long-lived goroutine in this package without a
  `defer recover()` that logs to `m.logger`. bubbletea's recover net does
  not cover this layer.
```

#### 4. internal/service/processmgr/manager_test.go
**File**: `internal/service/processmgr/manager_test.go`
**Changes**: MODIFY — `newTestManager` and four other `Config{...}` literals at `:86`, `:133`, `:219`, `:281` each gain `WaitFunc: func(*exec.Cmd) error { return nil }`.

```go
// Helper at manager_test.go:33-42 — the canonical Config{} shape for tests.
func newTestManager(t *testing.T) (*fsManager, string) {
	t.Helper()
	dir := t.TempDir()
	fb := fakeBinary(t)
	mgr := New(Config{
		Resolver:     func(_ domain.Profile) (string, error) { return fb, nil },
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
		// No-op waitFunc neutralizes the Wait enrichment body so tests that
		// `defer mgr.Kill(inst.PID)` against fake-llama-server.sh don't race
		// the goroutine on m.tracked / m.exitInfos mutation. See AGENTS.md
		// "Wait goroutine lifecycle".
		WaitFunc: func(*exec.Cmd) error { return nil },
	})
	return mgr, dir
}

// The four other Config{...} sites in manager_test.go each gain the same line:
//   WaitFunc: func(*exec.Cmd) error { return nil },
// at :86, :133, :219, :281. No other body edits in this file.
```

#### 5. internal/service/processmgr/recover_test.go
**File**: `internal/service/processmgr/recover_test.go`
**Changes**: MODIFY — two `Config{...}` literals at `:66` and `:106` each gain `WaitFunc` no-op.

```go
mgr := New(Config{
	Resolver:     func(_ domain.Profile) (string, error) { return fakeBinary(t), nil },
	LogDir:       filepath.Join(dir, "logs"),
	RegistryPath: filepath.Join(dir, "instances.json"),
	WaitFunc:     func(*exec.Cmd) error { return nil }, // see AGENTS.md
})
```

Identical addition at both sites. No other body edits.

#### 6. internal/service/processmgr/reconcile_wrapper_test.go
**File**: `internal/service/processmgr/reconcile_wrapper_test.go`
**Changes**: MODIFY — single `Config{...}` literal at `:31` gains `WaitFunc` no-op.

```go
mgr := New(Config{
	Resolver:     func(_ domain.Profile) (string, error) { return fakeBinary(t), nil },
	LogDir:       filepath.Join(dir, "logs"),
	RegistryPath: filepath.Join(dir, "instances.json"),
	WaitFunc:     func(*exec.Cmd) error { return nil }, // see AGENTS.md
})
```

### Success Criteria:

#### Automated Verification:
- [x] Build passes: `go build ./...`
- [x] Vet clean: `go vet ./...`
- [x] processmgr tests pass: `go test ./internal/service/processmgr/... -count=1`
- [x] No race flakes under stress: `go test ./internal/service/processmgr/... -race -count=10`
- [x] 5 out-of-lock lifecycle saveRegistry callsites + 1 Reconcile boot site (grep returns 6, docs table lists 5 lifecycle); plan's `wc -l == 5` invariant was off-by-one
- [x] AGENTS.md updated same commit: `grep "Wait goroutine lifecycle" internal/service/processmgr/AGENTS.md` returns the header
- [x] AGENTS.md table lists 5 callsites
- [x] No goroutine without recover(): 3 long-lived goroutines, all guarded (1 liveness + 2 waitEnrichment)
- [x] Full test suite passes: `go test ./... -count=1`

#### Manual Verification:
- [x] `processmgr/AGENTS.md` "Wait goroutine lifecycle" section documents win-by-liveness contract
- [x] Foreground spawn order: `m.tracked[inst.PID] = inst` precedes `go m.waitEnrichment(...)` in `launchForeground`
- [x] `extractExit` uses `, ok` guard for `syscall.WaitStatus` assertion (no build tag, follows recover.go precedent)
- [ ] Manually kill a tracked PID externally → deferred to `/skill:validate` manual phase

**Deviation from plan:** Phase 3 dropped the planned `WaitFunc: func(*exec.Cmd) error { return nil }` no-ops in test Configs. Reason: the synchronous no-op fires the enrichment body immediately and clears `m.fgPID = 0`, breaking `TestManager_Foreground_OnlyOneAllowed`. Real `(*exec.Cmd).Wait` blocks until `defer mgr.Kill` triggers process death; the goroutine then finds `m.tracked[pid]` already deleted (`!ok` branch) and exits safely. `WaitFunc` remains in `Config` as an optional seam for future tests.

---

## Phase 4: Service Constructor Wiring (Resolver + Validator + Profile Editor)

### Overview

Add `*slog.Logger` parameter to `backendcatalog.NewResolver(store, schemaStore, logger)` and `validator.New(logger)`, with nil-guard fallback to `log.Nop()`. Resolver logs `resolve_failed` events at each of 4 error returns. Validator logs `validation_failed` when report has blocking errors. The profile-editor's `validator.New` call site passes `log.Nop()` (out-of-spawn-path). 4 test files update with `log.Nop()` arguments.

**This phase is file-disjoint from Phase 3** and can be executed in parallel after Phase 2.

### Changes Required:

#### 1. internal/service/backendcatalog/resolver.go
**File**: `internal/service/backendcatalog/resolver.go`
**Changes**: MODIFY — `resolver` struct gains `logger *slog.Logger`; `NewResolver` widens to 3 args with nil-guard; `Resolve` body logs structured `resolve_failed` event at each of 4 error returns (load_catalog, select_backend, resolve_executable, load_schema).

```go
type resolver struct {
	store       Store
	schemaStore SchemaStore
	logger      *slog.Logger
}

// NewResolver returns a catalog-backed backend resolver. logger may be nil;
// nil → log.Nop() (no-op handler).
func NewResolver(store Store, schemaStore SchemaStore, logger *slog.Logger) *resolver {
	if logger == nil {
		logger = log.Nop()
	}
	return &resolver{store: store, schemaStore: schemaStore, logger: logger}
}

func (r *resolver) Resolve(profile domain.Profile) (ResolvedBackend, error) {
	catalog, err := r.store.Load()
	if err != nil {
		r.logger.Error("resolve_failed",
			"step", "load_catalog", "profile_id", profile.ID, "err", err)
		return ResolvedBackend{}, err
	}

	backendID := profile.Launch.BackendID
	if backendID == "" {
		backendID = catalog.DefaultBackendID
	}
	if backendID == "" {
		r.logger.Error("resolve_failed",
			"step", "select_backend", "profile_id", profile.ID, "err", ErrNoBackendSelected)
		return ResolvedBackend{}, ErrNoBackendSelected
	}

	backend, ok := findBackend(catalog.Backends, backendID)
	if !ok {
		err := fmt.Errorf("%w: %s", ErrBackendNotFound, backendID)
		r.logger.Error("resolve_failed",
			"step", "select_backend", "profile_id", profile.ID,
			"backend_id", backendID, "err", err)
		return ResolvedBackend{}, err
	}

	executablePath, err := resolveExecutable(backend)
	if err != nil {
		r.logger.Error("resolve_failed",
			"step", "resolve_executable", "profile_id", profile.ID,
			"backend_id", backendID, "executable", backend.Executable, "err", err)
		return ResolvedBackend{}, err
	}

	schema, err := r.schemaStore.Load(schemaStoreRef(backend.SchemaRef))
	if err != nil {
		wrapped := fmt.Errorf("load schema: %w", err)
		r.logger.Error("resolve_failed",
			"step", "load_schema", "profile_id", profile.ID,
			"backend_id", backendID, "schema_ref", backend.SchemaRef, "err", err)
		return ResolvedBackend{}, wrapped
	}
	if schema.BackendID != "" && schema.BackendID != backend.ID {
		err := fmt.Errorf("schema/backend mismatch: schema has backend_id=%q, expected %q", schema.BackendID, backend.ID)
		r.logger.Error("resolve_failed",
			"step", "load_schema", "profile_id", profile.ID, "err", err)
		return ResolvedBackend{}, err
	}
	if backend.Kind != "" && schema.BackendKind != "" && schema.BackendKind != backend.Kind {
		err := fmt.Errorf("schema/backend kind mismatch: schema has kind=%q, expected %q", schema.BackendKind, backend.Kind)
		r.logger.Error("resolve_failed",
			"step", "load_schema", "profile_id", profile.ID, "err", err)
		return ResolvedBackend{}, err
	}

	return ResolvedBackend{
		Backend:        backend,
		ExecutablePath: executablePath,
		Schema:         schema,
	}, nil
}
```

New imports:

```go
import (
	// existing: "fmt", "strings", internal/domain, internal/service/llamabin
	"log/slog"

	"github.com/quantmind-br/model-loader/internal/log"
)
```

#### 2. internal/service/backendcatalog/resolver_test.go
**File**: `internal/service/backendcatalog/resolver_test.go`
**Changes**: MODIFY — 4 `NewResolver` callsites at `:27`, `:65`, `:136`, `:175` each gain `log.Nop()` 3rd arg.

```go
// Pattern at each of the 4 sites:
r := NewResolver(catalogStore, schemaStore, log.Nop())
```

New import:

```go
import (
	// existing test imports preserved...
	"github.com/quantmind-br/model-loader/internal/log"
)
```

#### 3. internal/service/validator/validator.go
**File**: `internal/service/validator/validator.go`
**Changes**: MODIFY — `New` gains `logger *slog.Logger` param with nil-guard; `defaultValidator` struct gains `logger` field; `Validate` logs info-level `validation_failed` when blocking errors present.

```go
// New returns a default Validator with the standard rule set. logger may be
// nil; nil → log.Nop() (no-op handler). Production wires the *slog.Logger
// from internal/log; the profile_editor passes log.Nop() because its
// validator runs out of the spawn correlation path.
func New(logger *slog.Logger) Validator {
	if logger == nil {
		logger = log.Nop()
	}
	return defaultValidator{logger: logger}
}

type defaultValidator struct {
	logger *slog.Logger
}

func (v defaultValidator) Validate(p domain.Profile, schema domain.FlagSchema) Report {
	rep := Report{}
	rep = applyTypeRules(p, schema, rep)
	rep = applyExtraArgsRules(p, schema, rep)
	rep = applyExistenceRules(p, rep)
	if rep.HasBlockingErrors() {
		v.logger.Info("validation_failed",
			"profile_id", p.ID,
			"error_count", len(rep.Errors),
			"warning_count", len(rep.Warnings))
	}
	return rep
}
```

New imports:

```go
import (
	"log/slog"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
)
```

#### 4. internal/service/validator/validator_test.go
**File**: `internal/service/validator/validator_test.go`
**Changes**: MODIFY — 5 `New()` callsites at `:11`, `:47`, `:68`, `:90`, `:118` each gain `log.Nop()` argument.

```go
// Pattern at each of the 5 sites:
v := New(log.Nop())
```

New import:

```go
import (
	// existing test imports preserved...
	"github.com/quantmind-br/model-loader/internal/log"
)
```

#### 5. internal/ui/pages/profile_editor/editor.go
**File**: `internal/ui/pages/profile_editor/editor.go`
**Changes**: MODIFY — single `validator.New()` call at `:86` becomes `validator.New(log.Nop())`. New `internal/log` import.

```go
// At the single validator.New call site (line ~86):
validator: validator.New(log.Nop()),
```

New import:

```go
import (
	// existing imports preserved...
	"github.com/quantmind-br/model-loader/internal/log"
)
```

#### 6. internal/ui/pages/profiles_test.go
**File**: `internal/ui/pages/profiles_test.go`
**Changes**: MODIFY — single `validator.New()` call at `:97` becomes `validator.New(log.Nop())`. New `internal/log` import.

```go
rep := validator.New(log.Nop()).Validate(p, schema)
```

New import:

```go
import (
	// existing test imports preserved...
	"github.com/quantmind-br/model-loader/internal/log"
)
```

#### 7. internal/ui/pages/launcher_test.go (Slice 4 contribution)
**File**: `internal/ui/pages/launcher_test.go`
**Changes**: MODIFY (Slice 4 portion) — single `validator.New()` site at `:134` becomes `validator.New(log.Nop())`. New `internal/log` import (if not already added in Phase 2; merge import block).

```go
// At launcher_test.go:134 — the validator.New site updates per Phase 4:
val := validator.New(log.Nop())
```

New import:

```go
import (
	// existing test imports preserved...
	"github.com/quantmind-br/model-loader/internal/log"
)
```

### Success Criteria:

#### Automated Verification:
- [x] Build passes: `go build ./...`
- [x] Vet clean: `go vet ./...`
- [x] backendcatalog tests pass: `go test ./internal/service/backendcatalog/... -count=1`
- [x] validator tests pass: `go test ./internal/service/validator/... -count=1`
- [x] pages tests pass: `go test ./internal/ui/pages/... -count=1`
- [x] Full test suite passes: `go test ./... -count=1`
- [x] No raw `validator.New()` calls remain (all sites widened): `grep -rn "validator.New()" --include='*.go' .` returns 0

#### Manual Verification:
- [x] `NewResolver` signature lists 3 params in godoc
- [x] `validator.New` signature lists 1 param in godoc
- [x] profile_editor uses `log.Nop()` (not real logger) — comment cites "out-of-spawn-path"
- [x] No nil-panic when running with `cfg.Logger == nil` in tests

---

## Phase 5: LauncherPage + attempt_id Propagation + Reconcile Events

### Overview

Make `LauncherPage` correlation-aware: new `logger *slog.Logger` field, `WithLogger` builder mutator (value-receiver matching `SetBackendResolver`), `launchedMsg.attemptID` ephemeral field, `launchProfileCmd` generates `attemptID` via `log.NewAttemptID()` and threads it through `mgr.Launch`, `handleLaunched` forwards to `waitCmd` for `WaitHealthy`, `handleLaunchErr` captures `p.waitingPID` BEFORE clearing and calls `enrichWithExit(base, exit)` when `mgr.GetExitInfo(pid)` returns ok=true. New rune-safe `truncRunes` helper in `messages.go` (existing byte-based `truncate` has latent multi-byte bug). `processmgr.Reconcile` emits `reconcile_kept`/`reconcile_dropped`/`reconcile_done` events. Two new tests exercise the enrichment path and the TOCTOU fallback; one new test pins rune-safe truncation.

### Changes Required:

#### 1. internal/ui/pages/launcher.go
**File**: `internal/ui/pages/launcher.go`
**Changes**: MODIFY — `LauncherPage` struct +`logger` field initialized to `log.Nop()`; new `WithLogger(lg *slog.Logger) LauncherPage` builder; `launchedMsg` +`attemptID string`; `launchProfileCmd` generates ID via `log.NewAttemptID()`, wraps logger with `attempt_id`+`profile_id` context via `lg.With(...)`, emits `launch_pipeline_start`/`launch_pipeline_failed` events; `handleLaunched` threads `attemptID` into `waitCmd`; `handleLaunchErr` captures `pid := p.waitingPID` BEFORE clearing then calls `enrichWithExit` when `mgr.GetExitInfo(pid)` returns ok=true; new helpers `enrichWithExit`, `lastNonEmpty`, `modeString`. New imports `log/slog` + `internal/log`.

```go
type LauncherPage struct {
	store     profilestore.Store
	manager   processmgr.Manager
	validator validator.Validator
	resolver  backendcatalog.Resolver
	logger    *slog.Logger

	profiles []domain.Profile
	plist    list.Model

	background bool
	status     string
	statusAt   time.Time
	running    []domain.RunningInstance

	width, height int
	loadErr       error

	killConfirm components.Confirm

	spin       spinner.Model
	waitingPID int
}

func NewLauncherPage(store profilestore.Store, manager processmgr.Manager, val validator.Validator) LauncherPage {
	delegate := list.NewDefaultDelegate()
	l := list.New(nil, delegate, 40, 20)
	l.Title = "Profiles"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	return LauncherPage{
		store:      store,
		manager:    manager,
		validator:  val,
		plist:      l,
		background: true,
		spin:       sp,
		logger:     log.Nop(),
	}
}

// WithLogger injects the application logger. Mirrors SetBackendResolver's
// value-receiver builder shape. Pages constructed without WithLogger keep
// the log.Nop() default — nil-safe by construction.
func (p LauncherPage) WithLogger(lg *slog.Logger) LauncherPage {
	if lg != nil {
		p.logger = lg
	}
	return p
}

// launchedMsg gains ephemeral attemptID for the WaitHealthy hop.
type launchedMsg struct {
	inst      domain.RunningInstance
	attemptID string
}

// friendlyLaunchError stays unchanged. Enrichment happens in handleLaunchErr.
func friendlyLaunchError(err error) string {
	switch {
	case errors.Is(err, processmgr.ErrPortBusy):
		return "error: port in use — change the profile port or kill the running PID"
	case errors.Is(err, processmgr.ErrModelNotFound):
		return "error: model file not found — fix the profile's Model path"
	case errors.Is(err, processmgr.ErrForegroundBusy):
		return "error: a foreground instance is already running — toggle [b] to background mode"
	case errors.Is(err, processmgr.ErrHealthCheckTimeout):
		return "error: server did not become healthy within timeout — check logs"
	default:
		return "error: " + err.Error()
	}
}

// enrichWithExit appends the captured exit cause to a friendlyLaunchError
// base message. Output stays single-line. Last non-empty StderrTail line
// is truncated to 80 RUNES via truncRunes (NOT byte-based truncate).
func enrichWithExit(base string, exit processmgr.ExitInfo) string {
	parts := []string{base}
	switch {
	case exit.ExitSignal != "":
		parts = append(parts, "(signal: "+exit.ExitSignal+")")
	case exit.ExitCode != nil:
		parts = append(parts, fmt.Sprintf("(exit %d)", *exit.ExitCode))
	}
	if last := lastNonEmpty(exit.StderrTail); last != "" {
		parts = append(parts, "— last: "+truncRunes(last, 80))
	}
	if len(parts) == 1 {
		return base
	}
	return strings.Join(parts, " ")
}

// lastNonEmpty returns the last non-empty element of s.
func lastNonEmpty(s []string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if strings.TrimSpace(s[i]) != "" {
			return s[i]
		}
	}
	return ""
}

// modeString produces a stable label for log events.
func modeString(mode processmgr.LaunchMode) string {
	if mode == processmgr.LaunchForeground {
		return "foreground"
	}
	return "background"
}

func (p LauncherPage) handleLaunched(msg launchedMsg) (tea.Model, tea.Cmd) {
	p.running = append(p.running, msg.inst)
	p.waitingPID = msg.inst.PID
	p.status = fmt.Sprintf("pid=%d port=%d — waiting for /health…", msg.inst.PID, msg.inst.Port)
	p.statusAt = time.Time{}
	mgr := p.manager
	port := msg.inst.Port
	pid := msg.inst.PID
	attemptID := msg.attemptID
	waitCmd := func() tea.Msg {
		if err := mgr.WaitHealthy(pid, port, 30*time.Second, attemptID); err != nil {
			return launchErrMsg{err: fmt.Errorf("pid %d not healthy: %w", pid, err)}
		}
		return healthyMsg{pid: pid}
	}
	return p, tea.Batch(p.spin.Tick, waitCmd)
}

// handleLaunchErr captures the waitingPID BEFORE clearing it, then queries
// the Manager for any captured exit info to enrich the status message.
func (p LauncherPage) handleLaunchErr(msg launchErrMsg) (tea.Model, tea.Cmd) {
	pid := p.waitingPID
	p.waitingPID = 0
	base := friendlyLaunchError(msg.err)
	if pid != 0 && p.manager != nil {
		if exit, ok := p.manager.GetExitInfo(pid); ok {
			base = enrichWithExit(base, exit)
		}
	}
	p, fc := p.withStatus(base)
	return p, fc
}

// launchProfileCmd generates attemptID and threads it through every step.
func (p LauncherPage) launchProfileCmd(selected domain.Profile) tea.Cmd {
	val := p.validator
	mgr := p.manager
	res := p.resolver
	lg := p.logger
	attemptID := log.NewAttemptID()
	mode := processmgr.LaunchBackground
	if !p.background {
		mode = processmgr.LaunchForeground
	}
	return func() tea.Msg {
		evt := lg.With("attempt_id", attemptID, "profile_id", selected.ID)
		evt.Info("launch_pipeline_start", "mode", modeString(mode))

		if res == nil {
			evt.Error("launch_pipeline_failed", "step", "resolver_unwired")
			return launchErrMsg{err: fmt.Errorf("no backend resolver configured")}
		}

		rb, err := res.Resolve(selected)
		if err != nil {
			evt.Error("launch_pipeline_failed", "step", "resolve", "err", err)
			return launchErrMsg{err: fmt.Errorf("resolve backend: %w", err)}
		}
		activeSchema := rb.Schema.ToFlagSchema()

		if val != nil {
			rep := val.Validate(selected, activeSchema)
			if rep.HasBlockingErrors() {
				evt.Error("launch_pipeline_failed",
					"step", "validate", "err_count", len(rep.Errors))
				return launchErrMsg{err: fmt.Errorf("validation failed: %d errors", len(rep.Errors))}
			}
		}
		selected.Launch.ResolvedExecutable = rb.ExecutablePath
		selected.Launch.ResolvedBackendKind = rb.Backend.Kind
		inst, err := mgr.Launch(selected, mode, attemptID)
		if err != nil {
			evt.Error("launch_pipeline_failed", "step", "spawn", "err", err)
			return launchErrMsg{err: err}
		}
		return launchedMsg{inst: inst, attemptID: attemptID}
	}
}
```

New imports:

```go
import (
	// existing imports preserved...
	"log/slog"

	"github.com/quantmind-br/model-loader/internal/log"
)
```

Also in this phase: `internal/ui/pages/monitor.go` `restartCmd` swaps the
`""` placeholder (Phase 2) for `log.NewAttemptID()`:

```go
if _, err := pm.Launch(prof, mode, log.NewAttemptID()); err != nil {
	return restartResultMsg{pid: pid, err: fmt.Errorf("launch: %w", err)}
}
```

New `internal/log` import in `monitor.go`.

#### 2. internal/ui/pages/messages.go
**File**: `internal/ui/pages/messages.go`
**Changes**: MODIFY — append new `truncRunes(s string, max int) string` rune-safe helper after line 52. Documents that existing byte-based `truncate()` stays for the 3 ASCII-only callsites in `models.go` + `profile_editor/draft.go`.

```go
// truncRunes clips s to max RUNES (not bytes), appending "…" when the input
// is longer. Rune-safe alternative to truncate() above, which slices by
// byte index and would corrupt multi-byte UTF-8 mid-rune. Used by
// launcher.go's enrichWithExit for stderr-tail lines (llama-server may emit
// non-ASCII in localized CUDA error messages).
//
// Edge cases: max <= 0 returns ""; max == 1 returns "…" for any non-empty s.
func truncRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(rs[:max-1]) + "…"
}
```

#### 3. internal/ui/pages/launcher_test.go (Slice 5 contribution)
**File**: `internal/ui/pages/launcher_test.go`
**Changes**: MODIFY (Slice 5 portion) — 3 new tests: `TestLauncherPage_HandleLaunchErrEnrichesViaGetExitInfo` (positive path), `TestLauncherPage_HandleLaunchErrFallsBackWhenNoExitInfo` (TOCTOU window), `TestTruncRunes_MultiByte` (rune-safe regression).

```go
func TestLauncherPage_HandleLaunchErrEnrichesViaGetExitInfo(t *testing.T) {
	code := 1
	fake := &fakeManager{
		exitInfos: map[int]processmgr.ExitInfo{
			4242: {
				ExitCode:   &code,
				ExitReason: "exit:1",
				StderrTail: []string{
					"loading model ...",
					"CUDA error: out of memory at /llama.cpp/ggml-cuda.cu:1234",
				},
			},
		},
	}
	p := NewLauncherPage(nil, fake, validator.New(log.Nop())).WithLogger(log.Nop())
	p.waitingPID = 4242
	next, _ := p.handleLaunchErr(launchErrMsg{
		err: fmt.Errorf("pid 4242 not healthy: %w", processmgr.ErrHealthCheckTimeout),
	})
	lp := next.(LauncherPage)
	if !strings.Contains(lp.status, "(exit 1)") {
		t.Errorf("expected '(exit 1)' in status, got %q", lp.status)
	}
	if !strings.Contains(lp.status, "CUDA error: out of memory") {
		t.Errorf("expected stderr tail substring in status, got %q", lp.status)
	}
	if strings.Contains(lp.status, "\n") {
		t.Errorf("status must stay single-line, got %q", lp.status)
	}
	if lp.waitingPID != 0 {
		t.Errorf("expected waitingPID cleared, got %d", lp.waitingPID)
	}
}

func TestLauncherPage_HandleLaunchErrFallsBackWhenNoExitInfo(t *testing.T) {
	fake := &fakeManager{} // exitInfos nil → always ok=false
	p := NewLauncherPage(nil, fake, validator.New(log.Nop())).WithLogger(log.Nop())
	p.waitingPID = 9999
	next, _ := p.handleLaunchErr(launchErrMsg{
		err: fmt.Errorf("pid 9999 not healthy: %w", processmgr.ErrHealthCheckTimeout),
	})
	lp := next.(LauncherPage)
	if !strings.Contains(lp.status, "did not become healthy within timeout") {
		t.Errorf("expected bare ErrHealthCheckTimeout message, got %q", lp.status)
	}
	if strings.Contains(lp.status, "exit") || strings.Contains(lp.status, "signal") {
		t.Errorf("expected no enrichment when GetExitInfo returns false, got %q", lp.status)
	}
}

func TestTruncRunes_MultiByte(t *testing.T) {
	s := "日本語テスト" // 6 runes, 18 bytes
	if got := truncRunes(s, 3); got != "日本…" {
		t.Errorf("got %q want %q", got, "日本…")
	}
	if got := truncRunes(s, 10); got != s {
		t.Errorf("got %q want %q (passthrough)", got, s)
	}
	if got := truncRunes("", 5); got != "" {
		t.Errorf("got %q want empty", got)
	}
	if got := truncRunes("abc", 1); got != "…" {
		t.Errorf("got %q want %q", got, "…")
	}
	if got := truncRunes("abc", 0); got != "" {
		t.Errorf("got %q want empty", got)
	}
}
```

#### 4. internal/service/processmgr/recover.go
**File**: `internal/service/processmgr/recover.go`
**Changes**: MODIFY — emit `reconcile_failed`/`reconcile_dropped`/`reconcile_kept`/`reconcile_done` events at the natural decision points inside `Reconcile`. Imports unchanged (logger already plumbed in Phase 2).

```go
func (m *fsManager) Reconcile() error {
	loaded, err := loadRegistry(m.registryPath)
	if err != nil {
		m.logger.Error("reconcile_failed", "step", "load", "err", err)
		return err
	}
	survivors := make([]domain.RunningInstance, 0, len(loaded))
	tracked := make(map[int]domain.RunningInstance, len(loaded))
	var dropped int
	for _, ri := range loaded {
		binary := ri.BinaryPath
		if binary == "" {
			binary = m.defaultBinary
		}
		exeToken := exeFromBinaryPath(binary)
		if !pidAliveAndNameMatches(ri.PID, filepath.Base(exeToken)) {
			if !pidAliveAndNameMatches(ri.PID, filepath.Base(m.defaultBinary)) {
				m.logger.Info("reconcile_dropped",
					"pid", ri.PID, "profile_id", ri.ProfileID,
					"reason", "pid_or_comm_mismatch")
				dropped++
				continue
			}
		}
		if exeToken != binary {
			cmdlineToken := strings.TrimSpace(binary[len(exeToken):])
			if cmdlineToken != "" && !pidAliveAndCmdlineContains(ri.PID, cmdlineToken) {
				m.logger.Info("reconcile_dropped",
					"pid", ri.PID, "profile_id", ri.ProfileID,
					"reason", "cmdline_mismatch")
				dropped++
				continue
			}
		}
		m.logger.Info("reconcile_kept",
			"pid", ri.PID, "profile_id", ri.ProfileID, "binary", binary)
		survivors = append(survivors, ri)
		tracked[ri.PID] = ri
	}

	m.mu.Lock()
	m.tracked = tracked
	m.mu.Unlock()

	if err := saveRegistry(m.registryPath, survivors); err != nil {
		m.logger.Error("reconcile_failed", "step", "save", "err", err)
		return fmt.Errorf("rewrite registry: %w", err)
	}
	m.logger.Info("reconcile_done",
		"kept", len(survivors), "dropped", dropped, "total", len(loaded))
	return nil
}
```

### Success Criteria:

#### Automated Verification:
- [x] Build passes: `go build ./...`
- [x] Vet clean: `go vet ./...`
- [x] pages tests pass: `go test ./internal/ui/pages/... -count=1 -v -run "TestLauncherPage|TestTruncRunes"`
- [x] Enrichment positive test passes
- [x] TOCTOU fallback test passes
- [x] Rune-safe truncation test passes
- [x] Reconcile event emission: `grep -c "reconcile_" internal/service/processmgr/recover.go` ≥ 4
- [x] Full test suite passes: `go test ./... -race -count=1`

#### Manual Verification:
- [x] `LauncherPage.WithLogger` matches `SetBackendResolver` value-receiver builder shape
- [x] `handleLaunchErr` captures pid BEFORE clearing `p.waitingPID` (single-line precedence)
- [x] `enrichWithExit` output stays SINGLE-LINE (no `\n` injected)
- [x] `truncRunes` correctly truncates `日本語テスト` to `日本…` at max=3

---

## Phase 6: Boot Wiring (flag.Parse + log.New + Fprintf Migration + AppConfig.Logging)

### Overview

The final wiring phase: `main.go` adds `flag.Parse()` at the very top, initializes the logger AFTER `config.Load` and BEFORE any service that takes `*slog.Logger`, hard-fails on logger init error, defers `closeFn()`, wires `lg` into the resolver, validator, processmgr, and LauncherPage chains, and dual-sinks the 12 `Fprintf` + 1 `Fprintln` sites (stderr + file). `ensureDefaultCatalog` widens to accept `*slog.Logger` as first arg. `config.go` adds `LoggingConfig` type to `AppConfig` with `Level` field defaulted to `"info"`. The single `:29` and `config.go:82` Fprintfs stay raw stderr (chicken-and-egg — logger doesn't exist yet).

### Changes Required:

#### 1. internal/config/config.go
**File**: `internal/config/config.go`
**Changes**: MODIFY — `AppConfig` gains `Logging LoggingConfig` field; new `LoggingConfig` type with single `Level string` mapstructure field; `applyDefaults` adds `v.SetDefault("logging.level", "info")`.

```go
// AppConfig is the in-memory representation of the user config.
type AppConfig struct {
	Paths   PathsConfig   `mapstructure:"paths"`
	Models  ModelsConfig  `mapstructure:"models"`
	UI      UIConfig      `mapstructure:"ui"`
	Logging LoggingConfig `mapstructure:"logging"`
}

// LoggingConfig controls the model-loader app logger (not the per-instance
// llama-server log file). Level values: "debug" | "info" | "warn" | "error".
// Unknown values fall back to "info" via log.ResolveLevel.
type LoggingConfig struct {
	Level string `mapstructure:"level"`
}

// applyDefaults gains one new line for logging.level:
func applyDefaults(v *viper.Viper) {
	home, _ := os.UserHomeDir()
	v.SetDefault("paths.profiles_dir", filepath.Join(home, ".config", "model-loader", "profiles"))
	v.SetDefault("paths.log_dir", filepath.Join(home, ".local", "state", "model-loader", "logs"))
	v.SetDefault("paths.state_dir", filepath.Join(home, ".local", "state", "model-loader"))
	v.SetDefault("paths.backends_dir", filepath.Join(home, ".config", "model-loader", "backends"))
	v.SetDefault("models.search_paths", []string{
		filepath.Join(home, ".lmstudio", "models"),
		filepath.Join(home, "models"),
	})
	v.SetDefault("ui.default_tab", "launcher")
	v.SetDefault("ui.keybindings", "default")
	v.SetDefault("logging.level", "info")
}
```

#### 2. internal/config/config_test.go
**File**: `internal/config/config_test.go`
**Changes**: MODIFY — add `TestLoad_DefaultLoggingLevelIsInfo` near existing `TestLoad*` cases.

```go
func TestLoad_DefaultLoggingLevelIsInfo(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadFrom(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("default logging.level = %q, want %q", cfg.Logging.Level, "info")
	}
}
```

#### 3. cmd/model-loader/main.go
**File**: `cmd/model-loader/main.go`
**Changes**: MODIFY — `flag.Parse()` at top of `main()`; `log.New` after `config.Load`, hard-fail on error; `defer closeFn()`; dual-sink 12 `Fprintf` + 1 `Fprintln` sites; `ensureDefaultCatalog(lg, catalogStore, ...)` widened first arg; `backendcatalog.NewResolver(catalogStore, schemaStore, lg)` 3rd arg; `validator.New(lg)`; `processmgr.Config{... Logger: lg}`; `launcherPage.WithLogger(lg)` builder chain. The single `:29` Fprintf for `config error: %v` stays raw stderr (chicken-and-egg). New imports: `flag`, `log/slog`, `internal/log`. New global `var logLevelFlag = flag.String("log-level", "", ...)`.

```go
// Command model-loader launches the TUI for managing LLM server profiles.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/quantmind-br/model-loader/internal/service/migration"
	"github.com/quantmind-br/model-loader/internal/service/modelscanner"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/service/validator"
	"github.com/quantmind-br/model-loader/internal/ui"
	"github.com/quantmind-br/model-loader/internal/ui/pages"
)

// logLevelFlag is the --log-level CLI override. Empty string means "defer
// to env (MODEL_LOADER_LOG_LEVEL) then config (logging.level) then info".
var logLevelFlag = flag.String("log-level", "",
	"log verbosity: debug|info|warn|error (default: from config or 'info')")

func main() {
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		// Chicken-and-egg: logger doesn't exist yet (needs cfg.Paths.LogDir).
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	lg, closeFn, err := log.New(log.Config{
		Dir: cfg.Paths.LogDir,
		Level: log.ResolveLevel(
			*logLevelFlag,
			os.Getenv("MODEL_LOADER_LOG_LEVEL"),
			cfg.Logging.Level,
		),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "log init: %v\n", err)
		os.Exit(1)
	}
	defer closeFn()
	lg.Info("boot_start",
		"log_level", cfg.Logging.Level,
		"log_dir", cfg.Paths.LogDir,
		"state_dir", cfg.Paths.StateDir)

	store, err := profilestore.NewFSStore(cfg.Paths.ProfilesDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile store: %v\n", err)
		lg.Error("profile_store_init_failed", "err", err)
		os.Exit(1)
	}

	catalogStore := backendcatalog.NewFSStore(cfg.Paths.BackendsDir)
	schemaStore := backendcatalog.NewFSSchemaStore(cfg.Paths.BackendsDir)

	schemaManager := backendschema.NewManager(catalogStore, schemaStore)
	schemaManager.Register(domain.BackendKindLlamaServer, backendschema.NewLlamaServerGenerator(schemaStore))
	schemaManager.Register(domain.BackendKindSGLang, backendschema.NewSGLangGenerator(schemaStore))
	schemaManager.Register(domain.BackendKindVLLM, backendschema.NewVLLMGenerator(schemaStore))

	migrator := migration.NewService(cfg, store, catalogStore, schemaStore, schemaManager)
	migReport, err := migrator.Run(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "migration: %v\n", err)
		lg.Error("migration_failed", "err", err)
	}
	for _, w := range migReport.Warnings {
		fmt.Fprintf(os.Stderr, "migration warning: %s\n", w)
		lg.Warn("migration_warning", "msg", w)
	}

	resolver := backendcatalog.NewResolver(catalogStore, schemaStore, lg)

	defaultSchema := ensureDefaultCatalog(lg, catalogStore, schemaStore, schemaManager, cfg.Paths.LlamaServerBinaryPath)

	mgr := processmgr.New(processmgr.Config{
		Resolver:     buildResolver(resolver),
		LogDir:       cfg.Paths.LogDir,
		RegistryPath: filepath.Join(cfg.Paths.StateDir, "instances.json"),
		LastUsedSink: store,
		Logger:       lg,
	})
	defer mgr.Close()
	if err := mgr.Reconcile(); err != nil {
		fmt.Fprintf(os.Stderr, "instance recovery: %v\n", err)
		lg.Error("instance_recovery_failed", "err", err)
	}

	scanner := modelscanner.New()
	val := validator.New(lg)

	profilesPage := pages.NewProfilesPage(store, defaultSchema).
		WithModelScanner(scanner, cfg.Models.SearchPaths).
		WithBackendCatalog(catalogStore, schemaStore)
	modelsPage := pages.NewModelsPage(scanner, cfg.Models.SearchPaths).WithProfileStore(store)
	launcherPage := pages.NewLauncherPage(store, mgr, val).
		SetBackendResolver(resolver).
		WithLogger(lg)

	mon := monitor.New(monitor.Config{NvidiaSMIPath: "nvidia-smi"})
	monitorPage := pages.NewMonitorPage(mgr, mon, store).
		SetBackendResolver(resolver)
	backendsPage := pages.NewBackendsPage(schemaManager)

	root := ui.NewRoot(parseTab(cfg.UI.DefaultTab)).
		WithProfilesPage(profilesPage).
		WithModelsPage(modelsPage).
		WithLauncherPage(launcherPage).
		WithMonitorPage(monitorPage).
		WithBackendsPage(backendsPage)

	lg.Info("tui_starting", "default_tab", cfg.UI.DefaultTab)
	prog := tea.NewProgram(root, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		// Post-TUI: alt-screen torn down; both sinks safe.
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		lg.Error("tui_run_failed", "err", err)
		os.Exit(1)
	}
	if running := mgr.List(); len(running) > 0 {
		fmt.Fprintf(os.Stderr, "%d background instance(s) still running:\n", len(running))
		lg.Warn("orphan_background_instances", "count", len(running))
		for _, ri := range running {
			fmt.Fprintf(os.Stderr, "  PID %d (port %d)\n", ri.PID, ri.Port)
			lg.Warn("orphan_instance", "pid", ri.PID, "port", ri.Port,
				"profile_id", ri.ProfileID)
		}
		fmt.Fprintln(os.Stderr, "Restart the TUI to manage them.")
	}
	lg.Info("shutdown")
}

// ensureDefaultCatalog gains a *slog.Logger first arg so its 4 Fprintf
// sites can dual-sink to the session log file.
func ensureDefaultCatalog(lg *slog.Logger, catalogStore backendcatalog.Store, schemaStore backendcatalog.SchemaStore, _ *backendschema.Manager, fallbackBinary string) domain.FlagSchema {
	catalog, err := catalogStore.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "backend catalog load error: %v\n", err)
		fmt.Fprintln(os.Stderr, "fix catalog.json or remove it to recreate defaults")
		lg.Error("backend_catalog_load_failed", "err", err)
		lg.Warn("catalog_remediation_hint",
			"hint", "fix catalog.json or remove it to recreate defaults")
		return domain.BackendValidationSchema{}.ToFlagSchema()
	}
	if len(catalog.Backends) > 0 {
		resolver := backendcatalog.NewResolver(catalogStore, schemaStore, lg)
		if rb, err := resolver.Resolve(domain.Profile{}); err == nil {
			return rb.Schema.ToFlagSchema()
		}
		fmt.Fprintf(os.Stderr, "warning: default backend schema missing/invalid, using fallback\n")
		lg.Warn("default_backend_schema_invalid", "fallback", "empty_schema")
		return domain.BackendValidationSchema{}.ToFlagSchema()
	}
	if fallbackBinary == "" {
		fallbackBinary = "llama-server"
	}
	catalog = backendcatalog.DefaultCatalog(fallbackBinary)
	if err := catalogStore.Save(catalog); err != nil {
		fmt.Fprintf(os.Stderr, "save default catalog: %v\n", err)
		lg.Error("default_catalog_save_failed", "err", err)
		return domain.BackendValidationSchema{}.ToFlagSchema()
	}
	backend := catalog.Backends[0]
	_ = backendschema.WriteEmbeddedFallback(schemaStore, backend.ID, backend.SchemaRef)
	resolver := backendcatalog.NewResolver(catalogStore, schemaStore, lg)
	if rb, err := resolver.Resolve(domain.Profile{}); err == nil {
		return rb.Schema.ToFlagSchema()
	}
	return domain.BackendValidationSchema{}.ToFlagSchema()
}
```

### Success Criteria:

#### Automated Verification:
- [x] Build passes: `go build ./...`
- [x] Vet clean: `go vet ./...`
- [x] Binary produced: `make build && ls bin/model-loader`
- [x] `--log-level` flag wired: `flag.String("log-level", ...)` registered before `flag.Parse`
- [x] Invalid level handled: `ResolveLevel` falls back to `info` on unknown input (smoke-verified)
- [x] Default `logging.level = "info"` via `applyDefaults` in `internal/config/config.go`
- [x] No `_ = cmd.Wait()` remnants: `grep -rn "_ = cmd.Wait()" internal/service/processmgr/*.go` returns 0
- [x] Full test suite passes: `go test ./... -count=1`
- [x] Lint clean: `go vet ./...` returns no diagnostics

**Deviation resolved:** `TestLoad_DefaultLoggingLevelIsInfo` was added in `internal/config/config_test.go` (post-validation fix). The viper isolation concern was overblown — `LoadFrom(t.TempDir()/config.toml)` already gives a hermetic config space.

#### Manual Verification:
- [x] First launch creates `~/.local/state/model-loader/logs/model-loader.log` and writes `app_start` line
- [x] Second launch rotates the file: previous `model-loader.log` becomes `model-loader.<UTC-ts>.log` (2 files observed after smoke)
- [x] Cap-of-5 invariant: verified via 7-boot smoke under `HOME=$(mktemp -d)` → 5 archives + 1 active log
- [x] `MODEL_LOADER_LOG_LEVEL=warn ./ml-bin` filters INFO records (smoke: only ERROR `tui_error` recorded)
- [x] `./ml-bin --log-level=debug` records `level=DEBUG` in `app_start`
- [x] Pre-TUI errors visible in stderr (chicken-and-egg config error documented inline)
- [x] Post-TUI `tui_error` recorded in log file (smoke shows `level=ERROR msg=tui_error` line)
- [ ] Manual exit-info crash test: deferred to `/skill:validate` manual phase (needs interactive TTY)
- [ ] Bubbletea alt-screen integrity: deferred to manual TTY validation

---

## Testing Strategy

### Automated:

- `go build ./...` and `go vet ./...` after every phase
- `go test ./internal/log/... -v -count=1` (Phase 1)
- `go test ./internal/service/processmgr/... -race -count=10` (Phase 3 — race-soak)
- `go test ./... -race -count=1` (after every phase)
- `git diff go.mod` shows no new deps after Phase 1
- 5-callsite invariant: `grep -c "saveRegistry(m.registryPath" internal/service/processmgr/*.go | awk '{s+=$1} END{print s}'` returns 5 after Phase 3
- 0 remaining raw `validator.New()` and 0 remaining `_ = cmd.Wait()`: greppable invariants after Phase 4 and Phase 6 respectively

### Manual Testing Steps:

1. **Boot rotation**: launch model-loader twice consecutively; verify `~/.local/state/model-loader/logs/` shows one active `model-loader.log` + one timestamped archive.
2. **Cap-5 retention**: launch 7 times; verify exactly 5 timestamped archives (oldest pruned).
3. **Crash enrichment positive path**: launch a profile pointing to a non-existent model → confirm status line shows `error: model file not found — fix the profile's Model path` (no enrichment because Launch fails synchronously).
4. **Crash enrichment Wait path**: launch a profile that successfully starts but `llama-server` exits non-zero immediately (e.g. invalid `--threads -1`) → confirm status line shows base message PLUS `(exit N) — last: <stderr substring>` after WaitHealthy timeout.
5. **Signal path**: launch then `kill -SIGSEGV <pid>` externally → confirm status line shows `(signal: SIGSEGV)`.
6. **Bubbletea integrity**: rapidly launch-kill-launch-kill 5 times → alt-screen stays clean; no stderr bytes corrupt the rendering.
7. **Level resolution**: `./bin/model-loader` (info), `MODEL_LOADER_LOG_LEVEL=debug ./bin/model-loader` (debug), `./bin/model-loader --log-level=error` (CLI wins over env over config).
8. **Multi-byte stderr**: temporarily set llama-server `LANG=ja_JP.UTF-8` or inject a multi-byte error string; confirm `truncRunes` keeps rendering intact.
9. **Panic survival**: inject a deliberate panic in the Wait body (manual test) → confirm `wait_goroutine_panic` event appears in `model-loader.log` and TUI does NOT crash.
10. **Schema forward-compat**: copy `instances.json` from current main branch, run the new binary → all fields load + 4 new fields zero-valued; reverse (write with new, read with old) → old binary ignores new fields.

## Performance Considerations

- **Logger throughput is not a hot path** — `processmgr` emits at process-lifecycle events (Launch, Wait, Kill, liveness tick = 5s, reconcile = boot-only); LauncherPage emits per user-action; resolver/validator emit per call. Estimated max 20 events/minute under heavy use. Unbuffered `*os.File.Write` (one `write(2)` per event) is fine — syscall overhead at 20/min is negligible.
- **No `bufio.Writer`** — chosen specifically to make `os.Exit(1)` paths preserve all lines without `defer Flush()` ceremony in 3 sites.
- **`StderrTail` capture** — done OUTSIDE `m.mu` (file I/O on `logPath`) before acquiring lock, so file slowness doesn't block Launch/Kill. Tail is 50 lines max, read with `os.ReadFile` + last-50-line slice (acceptable for log files <100KB; for larger, consider seek-from-end if it bites later).
- **`saveRegistry` 5th callsite** — adds one `json.Marshal`+`os.Rename` per process exit. Existing pattern (4 sites) already accepts this latency; one more in the same per-PID lifecycle is the same order of magnitude.
- **Rotation algorithm** — at startup only: 1 `os.Stat`, possibly 1 `os.Rename`, 1 `filepath.Glob` (<10 entries), `sort.Sort`, up to 5 `os.Remove`. Order-of-microseconds, executes before TUI starts.
- **`attempt_id` generation** — `crypto/rand.Read([5]byte)` is ~200ns on Linux (uses `getrandom(2)`). Called once per launch.

## Migration Notes

- **`instances.json` schema**: backward + forward compatible via `omitempty`. Old binary reading new file: silently drops `exitCode`/`exitSignal`/`exitReason`/`stderrTail` (JSON decoder ignores unknown fields). New binary reading old file: zero-values the 4 new fields (`*int(nil)`, `""`, `""`, `nil` slice). No migration code.
- **`config.toml` schema**: `[logging] level = "info"` added with `viper.SetDefault`. Existing configs (no `[logging]` section) inherit default silently. Old config + new binary: works. Old binary + new config (with `[logging]`): old binary silently ignores the unknown section (viper).
- **Rollback strategy**: revert all 6 phases. The 4 new omitempty fields land empty in any `instances.json` written by the old binary; reverted binary continues reading them fine (zero-value). No data loss on rollback.
- **First-run behavior**: `log.New` creates `~/.local/state/model-loader/logs/` directory if missing. Subsequent runs rename existing `model-loader.log` → timestamped, retain 5.

## References

- Design: `thoughts/shared/designs/2026-05-15_15-09-16_debug-logging-system.md`
- Research: `thoughts/shared/research/2026-05-15_14-34-57_debug-logging-system.md`
- FRD: `thoughts/shared/discover/2026-05-15_14-15-11_debug-logging-system.md`
- Go blog "Structured Logging with slog": https://go.dev/blog/slog
- bubbletea logging guide: https://github.com/charmbracelet/bubbletea#logging-stuff
- `pkg.go.dev/log/slog#NewTextHandler`: https://pkg.go.dev/log/slog#NewTextHandler
- `pkg.go.dev/os#Exit`: https://pkg.go.dev/os#Exit
- `pkg.go.dev/crypto/rand#Read`: https://pkg.go.dev/crypto/rand#Read
- Precedent commits referenced in design: `9d04290`, `1f5eeeb`, `04ff36c`, `9cc3df2`, `907736c`, `fac58d5`, `1f608c8`, `826a463`, `60cc846`, `2fed091`, `64c6331`, `cdafac7`, `84598d4`, `b791c4a`.

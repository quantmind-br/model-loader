// Package processmgr launches and tracks LLM server processes.
package processmgr

import (
	"errors"
	"io"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// LaunchMode selects how the spawned process is attached.
type LaunchMode int

const (
	// LaunchBackground spawns the process detached (Setsid), redirects
	// stdout/stderr to a log file, and persists the entry in instances.json.
	LaunchBackground LaunchMode = iota
	// LaunchForeground spawns the process attached for in-TUI streaming.
	// Only one foreground instance is allowed at a time.
	LaunchForeground
)

// Manager owns the lifecycle of llama-server processes.
type Manager interface {
	// Launch spawns the configured backend. attemptID is the correlation ID
	// emitted by the calling page (LauncherPage.launchProfileCmd or
	// ServerPage.restartCmd) and threaded into every log event the manager
	// emits for this PID. Empty attemptID is permitted but breaks grep-ability.
	Launch(p domain.Profile, mode LaunchMode, attemptID string) (domain.RunningInstance, error)
	Kill(pid int) error
	List() []domain.RunningInstance
	// WaitHealthy polls the /health endpoint until 200 OK or timeout.
	// attemptID matches the one passed to Launch so the two log streams
	// can be correlated by grep attempt_id=...
	WaitHealthy(pid, port int, timeout time.Duration, attemptID string) error
	// WaitReady blocks until the backend is ready to serve and returns any
	// upstream auth token the proxy must inject (empty for kinds that need
	// none). For unsloth it captures the printed sk-unsloth key from the log,
	// which also signals the model has finished loading.
	WaitReady(inst domain.RunningInstance, timeout time.Duration, attemptID string) (authToken string, err error)
	TailLogs(pid int) (io.ReadCloser, error)
	Close() error
	// GetExitInfo returns the captured exit cause for pid if the Wait
	// goroutine has populated it. Returns (zero, false) when the process is
	// still alive or when Wait has not yet observed the exit (TOCTOU window
	// around the 30s WaitHealthy timeout). Best-effort — callers must fall
	// back to a generic message when ok=false.
	GetExitInfo(pid int) (ExitInfo, bool)
	// History returns the persisted exit-history entries. The slice is a
	// defensive copy; callers may mutate it freely.
	History() []domain.ExitedInstance
	// RefreshFromDisk re-reads instances.json and replaces the in-memory
	// tracked set with the live subset, without writing the registry back.
	// Safe for observer processes (the TUI) to call periodically; only the
	// owning process may persist drops (Reconcile).
	RefreshFromDisk() error
}

// LastUsedSink is a minimal callback to update Profile.Meta.LastUsedAt.
// processmgr stays decoupled from profilestore via this interface.
type LastUsedSink interface {
	MarkLastUsed(profileID string, at time.Time) error
}

// Sentinel errors. UI maps these to status bar messages.
var (
	ErrModelNotFound      = errors.New("model file not found")
	ErrForegroundBusy     = errors.New("a foreground instance is already running")
	ErrUnknownPID         = errors.New("pid is not tracked by this manager")
	ErrHealthCheckTimeout = errors.New("llama-server did not become healthy within timeout")
	ErrBinaryNotFound     = errors.New("llama-server binary not found in PATH")
	ErrProcessExited      = errors.New("backend process exited before becoming healthy")
)

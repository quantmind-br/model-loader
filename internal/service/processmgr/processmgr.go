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
	Launch(p domain.Profile, mode LaunchMode) (domain.RunningInstance, error)
	Kill(pid int) error
	List() []domain.RunningInstance
	WaitHealthy(pid, port int, timeout time.Duration) error
	TailLogs(pid int) (io.ReadCloser, error)
	Close() error
}

// LastUsedSink is a minimal callback to update Profile.Meta.LastUsedAt.
// processmgr stays decoupled from profilestore via this interface.
type LastUsedSink interface {
	MarkLastUsed(profileID string, at time.Time) error
}

// Sentinel errors. UI maps these to status bar messages.
var (
	ErrPortBusy           = errors.New("port already in use")
	ErrModelNotFound      = errors.New("model file not found")
	ErrForegroundBusy     = errors.New("a foreground instance is already running")
	ErrUnknownPID         = errors.New("pid is not tracked by this manager")
	ErrHealthCheckTimeout = errors.New("llama-server did not become healthy within timeout")
	ErrBinaryNotFound     = errors.New("llama-server binary not found in PATH")
)

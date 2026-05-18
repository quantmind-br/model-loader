package processmgr

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
)

// fsManager is the default Manager implementation backed by os/exec.
type fsManager struct {
	resolver      func(domain.Profile) (string, error)
	defaultBinary string
	logDir        string
	registryPath  string
	sink          LastUsedSink
	logger        *slog.Logger
	waitFunc      func(*exec.Cmd) error
	restartFunc   func(string)

	mu        sync.Mutex
	tracked   map[int]domain.RunningInstance
	exitInfos map[int]ExitInfo
	fgPID     int

	livenessStop func()

	history         []domain.ExitedInstance
	historyPath     string
	historyLimit    int
	historyRecorded map[int]struct{}
	historySaveMu   sync.Mutex
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
	// WaitFunc replaces (*exec.Cmd).Wait. Optional test seam for future
	// scenarios that want to bypass the kernel wait syscall. Production and
	// current tests use the real (*exec.Cmd).Wait — see AGENTS.md
	// "Wait goroutine lifecycle". nil → (*exec.Cmd).Wait.
	WaitFunc func(*exec.Cmd) error
	// HistoryPath is the path to the instances-history.json file. When empty,
	// it defaults to filepath.Dir(RegistryPath) + "/instances-history.json".
	HistoryPath string
	// HistoryLimit caps the number of persisted exit-history entries.
	// When zero or negative, it defaults to 50.
	HistoryLimit int
	// RestartFunc is called when the watchdog decides a process should be
	// restarted. The function receives the profile ID and should re-launch it.
	// When nil, restarts are logged but not executed.
	RestartFunc func(profileID string)
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
		resolver:        cfg.Resolver,
		defaultBinary:   cfg.DefaultBinary,
		logDir:          cfg.LogDir,
		registryPath:    cfg.RegistryPath,
		sink:            cfg.LastUsedSink,
		logger:          cfg.Logger,
		waitFunc:        cfg.WaitFunc,
		restartFunc:     cfg.RestartFunc,
		tracked:         map[int]domain.RunningInstance{},
		exitInfos:       map[int]ExitInfo{},
		historyRecorded: map[int]struct{}{},
	}
	if m.defaultBinary == "" {
		m.defaultBinary = "llama-server"
	}

	// Derive history path from registry path when not explicitly set.
	m.historyPath = cfg.HistoryPath
	if m.historyPath == "" && m.registryPath != "" {
		m.historyPath = filepath.Join(filepath.Dir(m.registryPath), "instances-history.json")
	}
	m.historyLimit = cfg.HistoryLimit
	if m.historyLimit <= 0 {
		m.historyLimit = defaultHistoryLimit
	}

	if m.historyPath != "" {
		h, err := loadHistory(m.historyPath)
		if err != nil {
			m.logger.Error("history_load_failed", "path", m.historyPath, "err", err)
		} else if h != nil {
			m.history = h
			for _, e := range h {
				m.historyRecorded[e.PID] = struct{}{}
			}
		}
	}

	m.livenessStop = m.startLiveness()
	return m
}

// GetExitInfo returns the captured exit cause for pid. Returns (zero, false)
// when the process is still alive, has never been tracked by this Manager,
// or its enrichment record was purged by a subsequent Launch reusing the
// same PID. The Wait enrichment goroutine populates the entry on process
// exit; Kill / Launch clear stale entries to prevent PID-reuse bleed-through.
func (m *fsManager) GetExitInfo(pid int) (ExitInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ei, ok := m.exitInfos[pid]
	return ei, ok
}

// Close stops the liveness ticker. Idempotent.
func (m *fsManager) Close() error {
	if m.livenessStop != nil {
		m.livenessStop()
	}
	return nil
}

// Kill sends SIGTERM to pid (10s grace) then SIGKILL if still alive. Removes
// the entry from the tracking table and rewrites the registry file.
func (m *fsManager) Kill(pid int) error {
	m.mu.Lock()
	_, ok := m.tracked[pid]
	m.mu.Unlock()
	if !ok {
		return ErrUnknownPID
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil && err != os.ErrProcessDone {
		return fmt.Errorf("sigterm: %w", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if proc.Signal(syscall.Signal(0)) != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if proc.Signal(syscall.Signal(0)) == nil {
		_ = proc.Signal(syscall.SIGKILL)
	}

	m.mu.Lock()
	inst, ok := m.tracked[pid]
	if ok {
		now := time.Now().UTC()
		inst.ExitedAt = &now
		inst.Crashed = true
		if inst.ExitReason == "" {
			inst.ExitReason = "killed"
		}
		m.appendHistoryLocked(inst, inst.ExitReason, now)
	}
	delete(m.tracked, pid)
	delete(m.exitInfos, pid) // drop stale enrichment so GetExitInfo returns ok=false
	if m.fgPID == pid {
		m.fgPID = 0
	}
	all := snapshotLocked(m.tracked)
	m.mu.Unlock()
	if err := saveRegistry(m.registryPath, all); err != nil {
		return err
	}
	if ok {
		_ = m.persistHistory()
	}
	return nil
}

// List returns a snapshot of the tracked instances. Order is not guaranteed.
func (m *fsManager) List() []domain.RunningInstance {
	m.mu.Lock()
	defer m.mu.Unlock()
	return snapshotLocked(m.tracked)
}

func snapshotLocked(t map[int]domain.RunningInstance) []domain.RunningInstance {
	out := make([]domain.RunningInstance, 0, len(t))
	for _, v := range t {
		out = append(out, v)
	}
	return out
}

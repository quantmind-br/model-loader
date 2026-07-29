package processmgr

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/internal/procutil"
)

// fsManager is the default Manager implementation backed by os/exec.
type fsManager struct {
	resolver      func(domain.Profile) (string, domain.BackendKind, error)
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
	// killRequested marks PIDs an operator asked to stop, set under m.mu
	// BEFORE signaling so the reaper/liveness restart path skips them
	// (audit A4: intentional kills must not resurrect).
	killRequested map[int]struct{}
	// restartScheduled is a set-once guard so reaper and liveness cannot both
	// fire a restart for the same dead PID (audit A10).
	restartScheduled map[int]struct{}
	// pendingRestarts carries a profile's restart generation count across the
	// death→relaunch boundary so MaxRestarts actually bounds a crash loop
	// (audit A5). Keyed by profile id; consumed by the next Launch.
	pendingRestarts map[string]int
	// hasReaper marks PIDs that have a live waitEnrichment (cmd.Wait) reaper.
	// Adopted instances (post-Reconcile, no cmd handle) are absent, so liveness
	// drives their restart policy instead (audit A10).
	hasReaper map[int]struct{}
	// regStat{Mtime,Size} cache the last-seen instances.json stat so List can
	// skip re-reading an unchanged file (audit C3). Guarded by m.mu.
	regStatMtime time.Time
	regStatSize  int64

	livenessStop func()

	history         []domain.ExitedInstance
	historyPath     string
	historyLimit    int
	historyRecorded map[int]struct{}
	historySaveMu   sync.Mutex
}

// Config holds wiring for New.
type Config struct {
	// Resolver returns the executable path AND the backend kind for a profile.
	// The kind is used to fill Profile.Launch.ResolvedBackendKind when a caller
	// (e.g. the HTTP proxy's on-demand launch) did not pre-resolve it, so args
	// are built for the correct backend instead of defaulting to llama-server.
	Resolver      func(domain.Profile) (string, domain.BackendKind, error)
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
		resolver:         cfg.Resolver,
		defaultBinary:    cfg.DefaultBinary,
		logDir:           cfg.LogDir,
		registryPath:     cfg.RegistryPath,
		sink:             cfg.LastUsedSink,
		logger:           cfg.Logger,
		waitFunc:         cfg.WaitFunc,
		restartFunc:      cfg.RestartFunc,
		tracked:          map[int]domain.RunningInstance{},
		exitInfos:        map[int]ExitInfo{},
		historyRecorded:  map[int]struct{}{},
		killRequested:    map[int]struct{}{},
		restartScheduled: map[int]struct{}{},
		pendingRestarts:  map[string]int{},
		hasReaper:        map[int]struct{}{},
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

// MarkOperatorStop records that pid's imminent termination is operator-initiated.
// Reuses the killRequested intent map, so the death is labelled
// domain.ExitReasonOperatorStop and the restart policy never resurrects it —
// exactly as Kill does for a termination this process performs itself. Gated on
// tracked so a stale flag can never attach to a PID this manager does not know.
func (m *fsManager) MarkOperatorStop(pid int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tracked[pid]; !ok {
		return
	}
	m.killRequested[pid] = struct{}{}
}

// Kill terminates pid and its process group (SIGTERM, 10s grace, then SIGKILL),
// removes the entry from tracking, and deletes it from the registry via a
// flock-guarded delta. It is identity-checked (audit A11): a recycled PID is
// never signaled. A kill-intent flag is set BEFORE signaling so the restart
// path does not resurrect an intentionally stopped backend (audit A4).
func (m *fsManager) Kill(pid int) error {
	m.mu.Lock()
	inst, ok := m.tracked[pid]
	if !ok {
		m.mu.Unlock()
		return ErrUnknownPID
	}
	// PID recycled: the process we recorded already exited (reaper/liveness
	// records it). Drop tracking and registry entry without signaling the
	// innocent occupant. Idempotent-stop semantics: return nil.
	if inst.StartTicks != 0 && !procutil.SameProcess(pid, inst.StartTicks) {
		delete(m.tracked, pid)
		delete(m.exitInfos, pid)
		delete(m.killRequested, pid)
		delete(m.restartScheduled, pid)
		delete(m.hasReaper, pid)
		if m.fgPID == pid {
			m.fgPID = 0
		}
		m.mu.Unlock()
		m.logger.Info("kill_skipped_pid_recycled", "pid", pid, "profile_id", inst.ProfileID)
		_ = mutateRegistry(m.registryPath, func(reg map[int]domain.RunningInstance) {
			delete(reg, pid)
		})
		return nil
	}
	// Mark intent BEFORE signaling so a fast reaper/liveness wakeup skips the
	// restart policy for this stop (audit A4).
	m.killRequested[pid] = struct{}{}
	m.mu.Unlock()

	// Group kill (audit A3): Setsid launches lead their own group, so the whole
	// backend tree (EngineCore/Worker_TP) is swept, not just the leader.
	if termErr := procutil.TerminateTree(pid, 10*time.Second); termErr != nil {
		// The backend survived SIGTERM+SIGKILL confirmation: still alive and
		// (for a GPU backend) still holding VRAM. Do NOT drop it from tracking
		// or the registry — an "alive but unregistered" orphan is exactly the
		// P4/DF11 leak that OOMs the next load. Keep it visible so the caller
		// aborts the swap and the operator/liveness can retry. killRequested
		// stays set so the eventual death is not restarted as a crash.
		m.logger.Error("kill_failed_process_alive", "pid", pid, "profile_id", inst.ProfileID, "err", termErr)
		return fmt.Errorf("terminate pid %d: %w", pid, termErr)
	}

	m.mu.Lock()
	inst, ok = m.tracked[pid]
	if ok {
		now := time.Now().UTC()
		inst.ExitedAt = &now
		inst.Crashed = true
		if inst.ExitReason == "" {
			inst.ExitReason = domain.ExitReasonOperatorStop
		}
		m.appendHistoryLocked(inst, inst.ExitReason, now)
	}
	delete(m.tracked, pid)
	delete(m.exitInfos, pid) // drop stale enrichment so GetExitInfo returns ok=false
	delete(m.killRequested, pid)
	delete(m.restartScheduled, pid)
	delete(m.hasReaper, pid)
	if m.fgPID == pid {
		m.fgPID = 0
	}
	m.mu.Unlock()
	if err := mutateRegistry(m.registryPath, func(reg map[int]domain.RunningInstance) {
		delete(reg, pid)
	}); err != nil {
		return err
	}
	if ok {
		_ = m.persistHistory()
	}
	return nil
}

// List returns a snapshot of the tracked instances. Order is not guaranteed.
// Disk IO happens outside m.mu, and instances.json is re-read only when its
// (mtime, size) changed since the last call (audit C3). Disk entries merge
// only when they still refer to the live, non-recycled process they recorded
// (audit A1/A11), so observer output never surfaces a dead or recycled PID.
func (m *fsManager) List() []domain.RunningInstance {
	// Stat outside the lock; a matching cached (mtime,size) skips the read.
	fi, statErr := os.Stat(m.registryPath)
	m.mu.Lock()
	changed := statErr != nil || fi.ModTime() != m.regStatMtime || fi.Size() != m.regStatSize
	m.mu.Unlock()

	var loaded []domain.RunningInstance
	if changed {
		loaded, _ = loadRegistry(m.registryPath) // read outside the lock
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if changed {
		for _, ri := range loaded {
			if _, ok := m.tracked[ri.PID]; ok {
				continue
			}
			if !procutil.SameProcess(ri.PID, ri.StartTicks) {
				continue // never surface a dead/recycled disk entry
			}
			m.tracked[ri.PID] = ri
		}
		if statErr == nil {
			m.regStatMtime = fi.ModTime()
			m.regStatSize = fi.Size()
		}
	}
	return snapshotLocked(m.tracked)
}

func snapshotLocked(t map[int]domain.RunningInstance) []domain.RunningInstance {
	out := make([]domain.RunningInstance, 0, len(t))
	for _, v := range t {
		out = append(out, v)
	}
	return out
}

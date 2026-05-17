package processmgr

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
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

	mu        sync.Mutex
	tracked   map[int]domain.RunningInstance
	exitInfos map[int]ExitInfo
	fgPID     int

	livenessStop func()

	history        []domain.ExitedInstance
	historyPath    string
	historyLimit   int
	historyRecorded map[int]struct{}
	historySaveMu  sync.Mutex
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

// Launch spawns llama-server with the args derived from p. mode chooses
// between background (detached, log-to-file) and foreground (stdout/stderr
// inherit; only one allowed at a time — covered in Task 6).
func (m *fsManager) Launch(p domain.Profile, mode LaunchMode, attemptID string) (domain.RunningInstance, error) {

	if p.Model == "" {
		return domain.RunningInstance{}, ErrModelNotFound
	}
	_, err := os.Stat(p.Model)
	if err != nil {
		// Only skip the missing-file error for HuggingFace-style repo IDs.
		// Local paths that exist are accepted above; local paths that
		// don't exist AND don't look like a HF repo are rejected.
		if !domain.LooksLikeHFRepo(p.Model) {
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
	_ = logF.Close() // child inherited its own fd; drop ours

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
	// Clear any stale ExitInfo from a previous PID-reuse cycle so
	// LauncherPage.handleLaunchErr cannot enrich a future timeout with
	// data from a long-dead process that happened to share this PID.
	delete(m.exitInfos, inst.PID)
	// Allow the same PID to be recorded again in history if it is reused.
	delete(m.historyRecorded, inst.PID)
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

// WaitHealthy polls GET http://127.0.0.1:<port>/health with capped exponential
// backoff (100ms, 200ms, 400ms, ..., max 1s) until 200 OK or timeout.
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
						// best-effort; bookkeeping failure must not abort a healthy launch
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
	now := time.Now().UTC()
	appended := m.appendHistoryLocked(cur, reason, now)
	snap := snapshotLocked(m.tracked)
	m.mu.Unlock()

	// 5th out-of-lock saveRegistry callsite. See AGENTS.md.
	_ = saveRegistry(m.registryPath, snap)
	if appended {
		_ = m.persistHistory()
	}

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

// TailLogs opens the on-disk log file for a tracked background instance and
// returns it as an io.ReadCloser. Foreground instances (LogPath=="") return
// ErrUnknownPID — they have no log file. Caller closes.
func (m *fsManager) TailLogs(pid int) (io.ReadCloser, error) {
	logPath := m.logPathForPID(pid)
	if logPath == "" {
		return nil, ErrUnknownPID
	}
	f, err := os.Open(logPath)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}
	return f, nil
}

// logPathForPID returns the on-disk log file for pid, or "" if unknown.
func (m *fsManager) logPathForPID(pid int) string {
	for _, ri := range m.List() {
		if ri.PID == pid {
			return ri.LogPath
		}
	}
	return ""
}

func snapshotLocked(t map[int]domain.RunningInstance) []domain.RunningInstance {
	out := make([]domain.RunningInstance, 0, len(t))
	for _, v := range t {
		out = append(out, v)
	}
	return out
}

func portFromProfile(p domain.Profile) (int, bool) {
	v, ok := p.Args["port"]
	if !ok {
		return 0, false
	}
	switch v := v.(type) {
	case float64:
		if v <= 0 || v > 65535 {
			return 0, false
		}
		return int(v), true
	case int:
		if v <= 0 || v > 65535 {
			return 0, false
		}
		return v, true
	case string:
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 65535 {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

func checkPortFree(port int) error {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("port %d: %w", port, ErrPortBusy)
	}
	_ = l.Close()
	return nil
}

// launchForeground spawns a single foreground instance. Stdout/Stderr are
// not redirected (the caller — the LauncherPage — owns the streaming),
// and the process is NOT detached via Setsid: it remains in the TUI's
// process group so Ctrl+C from the TUI propagates if desired.
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
	m.fgPID = -1 // sentinel: launching in progress
	m.mu.Unlock()

	profileArgs, err := BuildArgsForBackend(p, p.Launch.ResolvedBackendKind, resolvedBinary)
	if err != nil {
		return domain.RunningInstance{}, fmt.Errorf("build args: %w", err)
	}
	cmd := makeCommand(resolvedBinary, profileArgs)
	// Inherit stdout/stderr — caller drains via TailLogs in slice 5.
	if err := cmd.Start(); err != nil {
		// Roll back sentinel so future calls can proceed.
		m.mu.Lock()
		m.fgPID = 0
		m.mu.Unlock()
		return domain.RunningInstance{}, fmt.Errorf("start process (fg): %w", err)
	}

	inst := domain.RunningInstance{
		ProfileID:  p.ID,
		PID:        cmd.Process.Pid,
		Port:       port,
		LogPath:    "", // no log file for foreground
		BinaryPath: resolvedBinary,
		StartedAt:  time.Now().UTC(),
		Background: false,
	}

	m.mu.Lock()
	m.tracked[inst.PID] = inst
	delete(m.exitInfos, inst.PID) // see Launch background comment above
	delete(m.historyRecorded, inst.PID)
	m.fgPID = inst.PID            // replaces -1 sentinel
	all := snapshotLocked(m.tracked)
	m.mu.Unlock()

	m.logger.Info("launch_started",
		"pid", inst.PID, "port", inst.Port,
		"profile_id", p.ID, "attempt_id", attemptID,
		"mode", "foreground", "binary", resolvedBinary)

	// MOVED from pre-Start to here (post-insert) so the waitEnrichment
	// body's re-read of m.tracked[inst.PID] sees a populated entry.
	go m.waitEnrichment(cmd, inst.PID, "", attemptID)

	if err := saveRegistry(m.registryPath, all); err != nil {
		return inst, fmt.Errorf("fg started but registry save failed: %w", err)
	}
	return inst, nil
}

// makeCommand builds an exec.Command from a possibly compound command string
// (e.g. "python -m sglang.launch_server") and the profile args.
func makeCommand(resolvedBinary string, profileArgs []string) *exec.Cmd {
	fields, err := splitCommandLine(resolvedBinary)
	if err != nil || len(fields) == 0 {
		return exec.Command("")
	}
	if len(fields) == 1 {
		return exec.Command(fields[0], profileArgs...)
	}
	all := make([]string, 0, len(fields)-1+len(profileArgs))
	all = append(all, fields[1:]...)
	all = append(all, profileArgs...)
	return exec.Command(fields[0], all...)
}



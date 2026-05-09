package processmgr

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/llamabin"
)

// fsManager is the default Manager implementation backed by os/exec.
type fsManager struct {
	binary       string // resolved path or name on PATH; default "llama-server"
	logDir       string // directory for background stdout/stderr capture
	registryPath string // absolute path to instances.json
	sink         LastUsedSink

	mu           sync.Mutex
	tracked      map[int]domain.RunningInstance // pid -> instance
	fgPID        int                            // 0 if no foreground active; -1 if launching
	livenessStop func()
}

// Config holds wiring for New. Caller owns the paths; the manager creates
// directories on demand.
type Config struct {
	Binary       string // override; empty = "llama-server"
	LogDir       string
	RegistryPath string
	LastUsedSink LastUsedSink
}

// New constructs a Manager. It does NOT call Reconcile; main.go orchestrates
// boot recovery explicitly.
func New(cfg Config) *fsManager {
	bin := cfg.Binary
	if bin == "" {
		bin = "llama-server"
	}
	m := &fsManager{
		binary:       bin,
		logDir:       cfg.LogDir,
		registryPath: cfg.RegistryPath,
		sink:         cfg.LastUsedSink,
		tracked:      map[int]domain.RunningInstance{},
	}
	m.livenessStop = m.startLiveness()
	return m
}

// Close stops the liveness ticker. Idempotent.
func (m *fsManager) Close() error {
	if m.livenessStop != nil {
		m.livenessStop()
	}
	return nil
}

// NewWithCheck é como New, mas verifica via exec.LookPath se o binário existe
// antes de retornar. Erro: ErrBinaryNotFound (com o nome buscado embutido).
// Use em main.go para bootear com fail-fast e modal de instalação.
func NewWithCheck(cfg Config) (*fsManager, error) {
	bin := cfg.Binary
	if bin == "" {
		bin = "llama-server"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBinaryNotFound, bin)
	}
	return New(cfg), nil
}

// Launch spawns llama-server with the args derived from p. mode chooses
// between background (detached, log-to-file) and foreground (stdout/stderr
// inherit; only one allowed at a time — covered in Task 6).
func (m *fsManager) Launch(p domain.Profile, mode LaunchMode) (domain.RunningInstance, error) {
	if p.Model == "" {
		return domain.RunningInstance{}, ErrModelNotFound
	}
	if _, err := os.Stat(p.Model); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return domain.RunningInstance{}, fmt.Errorf("%w: %s", ErrModelNotFound, p.Model)
		}
		return domain.RunningInstance{}, fmt.Errorf("stat model: %w", err)
	}
	port, ok := portFromProfile(p)
	if !ok {
		return domain.RunningInstance{}, fmt.Errorf("profile %q: missing or invalid port arg", p.ID)
	}
	if err := checkPortFree(port); err != nil {
		return domain.RunningInstance{}, err
	}
	if mode == LaunchForeground {
		return m.launchForeground(p, port)
	}

	if err := os.MkdirAll(m.logDir, 0o755); err != nil {
		return domain.RunningInstance{}, fmt.Errorf("mkdir log dir: %w", err)
	}
	binary := m.effectiveBinary(p)
	resolvedBinary, err := llamabin.Resolve(binary)
	if err != nil {
		return domain.RunningInstance{}, fmt.Errorf("invalid llama-server binary %q: %w", binary, err)
	}
	logPath := filepath.Join(m.logDir, fmt.Sprintf("%s-%d.log", p.ID, port))
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return domain.RunningInstance{}, fmt.Errorf("open log: %w", err)
	}

	cmd := exec.Command(resolvedBinary, BuildArgs(p)...)
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		_ = logF.Close()
		return domain.RunningInstance{}, fmt.Errorf("start llama-server: %w", err)
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
	all := snapshotLocked(m.tracked)
	m.mu.Unlock()

	// Reap zombie automatically so Kill's signal-0 poll terminates quickly.
	go func() { _ = cmd.Wait() }()

	if err := saveRegistry(m.registryPath, all); err != nil {
		return inst, fmt.Errorf("instance started (pid %d) but registry save failed: %w", inst.PID, err)
	}
	return inst, nil
}

// WaitHealthy polls GET http://127.0.0.1:<port>/health with capped exponential
// backoff (100ms, 200ms, 400ms, ..., max 1s) until 200 OK or timeout.
func (m *fsManager) WaitHealthy(pid int, port int, timeout time.Duration) error {
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
	return fmt.Errorf("port %d: %w", port, ErrHealthCheckTimeout)
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
	delete(m.tracked, pid)
	if m.fgPID == pid {
		m.fgPID = 0
	}
	all := snapshotLocked(m.tracked)
	m.mu.Unlock()
	return saveRegistry(m.registryPath, all)
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

func (m *fsManager) effectiveBinary(p domain.Profile) string {
	return llamabin.Effective(p.Launch.LlamaServerBinaryPath, m.binary)
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
func (m *fsManager) launchForeground(p domain.Profile, port int) (domain.RunningInstance, error) {
	binary := m.effectiveBinary(p)
	resolvedBinary, err := llamabin.Resolve(binary)
	if err != nil {
		return domain.RunningInstance{}, fmt.Errorf("invalid llama-server binary %q: %w", binary, err)
	}

	m.mu.Lock()
	if m.fgPID != 0 {
		m.mu.Unlock()
		return domain.RunningInstance{}, ErrForegroundBusy
	}
	m.fgPID = -1 // sentinel: launching in progress
	m.mu.Unlock()

	cmd := exec.Command(resolvedBinary, BuildArgs(p)...)
	// Inherit stdout/stderr — caller drains via TailLogs in slice 5.
	if err := cmd.Start(); err != nil {
		// Roll back sentinel so future calls can proceed.
		m.mu.Lock()
		m.fgPID = 0
		m.mu.Unlock()
		return domain.RunningInstance{}, fmt.Errorf("start llama-server (fg): %w", err)
	}
	go func() { _ = cmd.Wait() }()

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
	m.fgPID = inst.PID // replaces -1 sentinel
	all := snapshotLocked(m.tracked)
	m.mu.Unlock()

	if err := saveRegistry(m.registryPath, all); err != nil {
		return inst, fmt.Errorf("fg started but registry save failed: %w", err)
	}
	return inst, nil
}

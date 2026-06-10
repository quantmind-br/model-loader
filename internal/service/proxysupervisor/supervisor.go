package proxysupervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
)

// ErrAlreadyRunning is returned by Start when the proxy is already live.
// Callers that treat an already-running proxy as success (e.g. EnsureRunning)
// should test with errors.Is rather than string-matching.
var ErrAlreadyRunning = errors.New("proxy already running")

// Config holds wiring for New.
type Config struct {
	StatePath string
	LogDir    string
	Host      string
	Port      int
	Logger    *slog.Logger
	// BinaryPath overrides the executable used to launch the proxy.
	// When empty, os.Executable() is used.
	BinaryPath string
}

// Supervisor manages the HTTP proxy as a detached OS process. It implements
// the HTTPProxyController interface consumed by the TUI Server page.
type Supervisor struct {
	statePath  string
	logDir     string
	host       string
	port       int
	binaryPath string
	logger     *slog.Logger

	mu    sync.RWMutex
	state *State
}

// New constructs a Supervisor. Nil-tolerant for Logger.
func New(cfg Config) *Supervisor {
	if cfg.Logger == nil {
		cfg.Logger = log.Nop()
	}
	return &Supervisor{
		statePath:  cfg.StatePath,
		logDir:     cfg.LogDir,
		host:       cfg.Host,
		port:       cfg.Port,
		binaryPath: cfg.BinaryPath,
		logger:     cfg.Logger,
	}
}

func (s *Supervisor) resolveExe() (string, error) {
	if s.binaryPath != "" {
		return s.binaryPath, nil
	}
	return os.Executable()
}

// Start launches the proxy as a detached OS process via the current binary's
// "serve" subcommand. The process survives TUI exit. State is persisted so
// future TUI sessions can discover it.
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != nil && s.isAliveLocked() {
		return fmt.Errorf("%w on pid %d", ErrAlreadyRunning, s.state.PID)
	}

	exe, err := s.resolveExe()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	if err := os.MkdirAll(s.logDir, 0o755); err != nil {
		return fmt.Errorf("mkdir log dir: %w", err)
	}

	logPath := filepath.Join(s.logDir, "proxy.log")
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open proxy log: %w", err)
	}

	cmd := exec.Command(exe, "serve", "--host="+s.host, fmt.Sprintf("--port=%d", s.port))
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		_ = logF.Close()
		return fmt.Errorf("start proxy: %w", err)
	}
	_ = logF.Close()

	st := &State{
		PID:       cmd.Process.Pid,
		Host:      s.host,
		Port:      s.port,
		StartedAt: time.Now().UTC(),
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer waitCancel()
	if err := s.waitForPort(waitCtx, st.Host, st.Port); err != nil {
		_ = syscall.Kill(st.PID, syscall.SIGKILL)
		return fmt.Errorf("proxy did not bind: %w", err)
	}

	s.state = st
	if err := saveState(s.statePath, st); err != nil {
		_ = syscall.Kill(st.PID, syscall.SIGKILL)
		return err
	}

	s.logger.Info("proxy_started", "pid", st.PID, "addr", s.addr(st))
	return nil
}

// Stop sends SIGTERM to the proxy process, waits for exit, and cleans state.
func (s *Supervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	st := s.state
	s.mu.Unlock()

	if st == nil {
		return fmt.Errorf("proxy not running")
	}

	proc, err := os.FindProcess(st.PID)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("sigterm: %w", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	for time.Now().Before(deadline) {
		if proc.Signal(syscall.Signal(0)) != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if proc.Signal(syscall.Signal(0)) == nil {
		_ = proc.Signal(syscall.SIGKILL)
	}

	s.mu.Lock()
	s.state = nil
	s.mu.Unlock()

	if err := saveState(s.statePath, nil); err != nil {
		s.logger.Error("proxy_stop_cleanup_failed", "err", err)
	}

	s.logger.Info("proxy_stopped", "pid", st.PID)
	return nil
}

// Status probes the persisted state and returns the proxy status. If the
// process died since the last check, state is cleaned automatically.
func (s *Supervisor) Status() httpproxy.Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == nil {
		return httpproxy.Status{Running: false}
	}

	if !s.isAliveLocked() {
		s.state = nil
		_ = saveState(s.statePath, nil)
		return httpproxy.Status{Running: false}
	}

	st := httpproxy.Status{
		Running: true,
		Addr:    s.addr(s.state),
	}

	url := "http://" + s.addr(s.state) + "/_status"
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(url)
	if err == nil && resp.StatusCode == http.StatusOK {
		_ = json.NewDecoder(resp.Body).Decode(&st)
		_ = resp.Body.Close()
	}
	return st
}

// Reconcile reads the on-disk state, validates the PID is still alive and the
// port is listening, and drops orphaned state. Call once at TUI boot.
func (s *Supervisor) Reconcile() error {
	st, err := loadState(s.statePath)
	if err != nil {
		s.logger.Error("proxy_reconcile_load_failed", "err", err)
		return err
	}
	if st == nil {
		return nil
	}

	if !pidAlive(st.PID) || !portOpen(st.Host, st.Port) {
		s.logger.Info("proxy_reconcile_dropped", "pid", st.PID, "reason", "pid_or_port_mismatch")
		if err := saveState(s.statePath, nil); err != nil {
			s.logger.Error("proxy_reconcile_cleanup_failed", "err", err)
		}
		return nil
	}

	s.mu.Lock()
	s.state = st
	s.mu.Unlock()
	s.logger.Info("proxy_reconcile_kept", "pid", st.PID, "addr", s.addr(st))
	return nil
}

func (s *Supervisor) isAliveLocked() bool {
	if s.state == nil {
		return false
	}
	return pidAlive(s.state.PID) && portOpen(s.state.Host, s.state.Port)
}

func (s *Supervisor) addr(st *State) string {
	return net.JoinHostPort(st.Host, fmt.Sprintf("%d", st.Port))
}

func (s *Supervisor) waitForPort(ctx context.Context, host string, port int) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if portOpen(host, port) {
				return nil
			}
		}
	}
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

func portOpen(host string, port int) bool {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

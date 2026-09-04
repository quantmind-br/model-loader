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
	"github.com/quantmind-br/model-loader/internal/service/internal/procutil"
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

	// startMu serializes Start/Stop lifecycle transitions; mu guards only
	// state reads/commits so the 1 Hz Status poll never blocks behind a 10s
	// port wait (mirrors httpproxy.Server.startMu). Lock order: startMu → mu,
	// never the reverse. Status/Reconcile/processAliveLocked take only mu.
	startMu sync.Mutex
	mu      sync.RWMutex
	state   *State
	// portFails counts consecutive failed port probes so a transient dial
	// timeout under GPU load does not destroy supervision of a healthy proxy
	// (audit A9). Reset on any successful probe. Guarded by mu.
	portFails int

	// probe is a shared client for /_status GETs, built once (audit C4) to
	// stop allocating an http.Client on every 1 Hz Status call.
	probe *http.Client

	// useSystemd selects the systemctl strategy (proxy unit owns the
	// process; gateway/Tunnel follow via BindsTo) over the legacy
	// detached-process strategy. Detected once in New; never flipped
	// mid-operation, so a systemd supervisor never spawns a second
	// proxy behind the unit's back.
	useSystemd bool
}

// probeFailThreshold is the number of consecutive port-probe failures Status
// tolerates before dropping proxy state (hysteresis, audit A9).
const probeFailThreshold = 3

// ErrProxyDegraded marks kill-refusals caused by a running-but-unresponsive
// proxy, so UIs can offer the ForceStop escape hatch (audit A13).
var ErrProxyDegraded = errors.New("proxy status unavailable")

// New constructs a Supervisor. Nil-tolerant for Logger.
func New(cfg Config) *Supervisor {
	if cfg.Logger == nil {
		cfg.Logger = log.Nop()
	}
	s := &Supervisor{
		statePath:  cfg.StatePath,
		logDir:     cfg.LogDir,
		host:       cfg.Host,
		port:       cfg.Port,
		binaryPath: cfg.BinaryPath,
		logger:     cfg.Logger,
		probe:      &http.Client{Timeout: 500 * time.Millisecond},
		useSystemd: detectSystemd(),
	}
	strategy := "process"
	if s.useSystemd {
		strategy = "systemd"
	}
	s.logger.Info("proxy_supervisor_strategy", "strategy", strategy, "unit", ProxyUnitName)
	return s
}

// Start launches the proxy: through the user-systemd unit when it is
// loaded, otherwise as a detached OS process (legacy). The public
// signatures are strategy-agnostic; see startProcess / startSystemd.
func (s *Supervisor) Start(ctx context.Context) error {
	if s.useSystemd {
		return s.startSystemd(ctx)
	}
	return s.startProcess(ctx)
}

// Stop terminates the proxy gracefully: unit stop under systemd, process
// group kill otherwise.
func (s *Supervisor) Stop(ctx context.Context) error {
	if s.useSystemd {
		return s.stopSystemd(ctx)
	}
	return s.stopProcess(ctx)
}

// Status probes the proxy: unit state + /_status under systemd, PID + port
// hysteresis otherwise. Both preserve the degraded Running=true semantics
// the UI needs to offer ForceStop.
func (s *Supervisor) Status() httpproxy.Status {
	if s.useSystemd {
		return s.statusSystemd()
	}
	return s.statusProcess()
}

// Reconcile validates persisted state at boot. Under systemd it only drops
// provably-obsolete legacy state; the unit is the source of truth.
func (s *Supervisor) Reconcile() error {
	if s.useSystemd {
		return s.reconcileSystemd()
	}
	return s.reconcileProcess()
}

// ForceStop SIGKILLs a wedged proxy: whole cgroup under systemd, process
// tree otherwise.
func (s *Supervisor) ForceStop() error {
	if s.useSystemd {
		return s.forceStopSystemd()
	}
	return s.forceStopProcess()
}

// Strategy reports the selected supervision strategy ("systemd" or
// "process"), for diagnostics and tests.
func (s *Supervisor) Strategy() string {
	if s.useSystemd {
		return "systemd"
	}
	return "process"
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
func (s *Supervisor) startProcess(ctx context.Context) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	s.mu.Lock()
	if s.state != nil && s.processAliveLocked() {
		pid := s.state.PID
		s.mu.Unlock()
		return fmt.Errorf("%w on pid %d", ErrAlreadyRunning, pid)
	}
	s.mu.Unlock()

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

	// Reap the child so a serve that dies on bind does not linger as a zombie
	// (a zombie is still signalable, so SameProcess alone can't detect it).
	// This does NOT kill the detached serve: Wait only collects its exit
	// status. When the TUI exits, this goroutine dies and the serve is
	// reparented to init — the intended detached behavior.
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()

	ticks, _ := procutil.StartTicks(cmd.Process.Pid) // 0 on failure → identity disabled
	st := &State{
		PID:        cmd.Process.Pid,
		Host:       s.host,
		Port:       s.port,
		StartedAt:  time.Now().UTC(),
		StartTicks: ticks,
	}

	// Honor the caller's ctx (TUI quit / CLI Ctrl-C) so the 10s port wait
	// can be aborted early instead of always running the full timeout (P-C12).
	waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Second)
	defer waitCancel()
	if err := s.waitForPort(waitCtx, st.Host, st.Port); err != nil {
		_ = procutil.TerminateTree(st.PID, 0)
		return fmt.Errorf("proxy did not bind: %w", err)
	}
	// waitForPort proves SOMETHING answers the port — verify it is OUR child
	// and not a pre-existing proxy that kept the port while our child died on
	// bind (audit A9). Recording a dead PID here caused the duplicate-spawn bug.
	select {
	case <-exited:
		return fmt.Errorf("proxy process exited during startup (port %d answered by another process)", st.Port)
	default:
	}
	if !procutil.SameProcess(st.PID, st.StartTicks) {
		return fmt.Errorf("proxy process exited during startup (port %d answered by another process)", st.Port)
	}

	s.mu.Lock()
	s.state = st
	s.portFails = 0
	s.mu.Unlock()
	if err := saveState(s.statePath, st); err != nil {
		_ = procutil.TerminateTree(st.PID, 0)
		// Don't leave stale in-memory state after a save failure.
		s.mu.Lock()
		s.state = nil
		s.portFails = 0
		s.mu.Unlock()
		return err
	}

	s.logger.Info("proxy_started", "pid", st.PID, "addr", s.addr(st))
	return nil
}

// Stop terminates the proxy process (SIGTERM, grace, then SIGKILL via the
// process group) and cleans state. The grace defaults to 45s — enough to cover
// serve's 30s Shutdown ctx + 10s drain + margin — so the supervisor never
// SIGKILLs the serve before it can kill its own backend (audit A8). The ctx
// deadline still clamps when earlier. If the serve was killed before it could
// free VRAM, the loaded backend is swept afterward.
func (s *Supervisor) stopProcess(ctx context.Context) error {
	// Serialize against an in-flight Start (previously provided by mu). ForceStop
	// deliberately does NOT take startMu — it must not queue behind a stuck Start.
	s.startMu.Lock()
	defer s.startMu.Unlock()

	s.mu.Lock()
	st := s.state
	s.mu.Unlock()

	if st == nil {
		return fmt.Errorf("proxy not running")
	}

	// Best-effort: learn the loaded backend PID before we tear the proxy down
	// (0 when degraded / nothing loaded).
	loadedPID := s.Status().LoadedPID

	grace := 45 * time.Second
	if d, ok := ctx.Deadline(); ok {
		if remaining := time.Until(d); remaining < grace {
			grace = remaining
		}
	}
	// serve is Setsid (its group contains only itself; backends live in their
	// own sessions) so a group kill is safe.
	_ = procutil.TerminateTree(st.PID, grace)

	// If the serve was SIGKILLed before killCurrentBackend ran, its backend is
	// still resident holding VRAM — sweep it (audit A8). The dead pid's
	// registry entry is cleaned by the next owner liveness/reconcile.
	if loadedPID > 0 && procutil.Alive(loadedPID) {
		_ = procutil.TerminateTree(loadedPID, 5*time.Second)
		s.logger.Info("proxy_stop_swept_backend", "backend_pid", loadedPID)
	}

	s.mu.Lock()
	s.state = nil
	s.portFails = 0
	s.mu.Unlock()

	if err := saveState(s.statePath, nil); err != nil {
		s.logger.Error("proxy_stop_cleanup_failed", "err", err)
	}

	s.logger.Info("proxy_stopped", "pid", st.PID)
	return nil
}

// Status probes the persisted state and returns the proxy status. If the
// process died since the last check, state is cleaned automatically.
func (s *Supervisor) statusProcess() httpproxy.Status {
	s.mu.Lock()
	if s.state == nil {
		s.mu.Unlock()
		return httpproxy.Status{Running: false}
	}
	if !s.processAliveLocked() {
		s.state = nil
		s.portFails = 0
		_ = saveState(s.statePath, nil)
		s.mu.Unlock()
		return httpproxy.Status{Running: false}
	}
	host, port := s.state.Host, s.state.Port
	addr := s.addr(s.state)
	s.mu.Unlock()

	// Probe the port OUTSIDE the mutex so a 500ms dial never blocks Start/Stop
	// or the 1 Hz UI poll (audit C4).
	if !portOpen(host, port) {
		s.mu.Lock()
		s.portFails++
		n := s.portFails
		if n >= probeFailThreshold {
			s.state = nil
			s.portFails = 0
			_ = saveState(s.statePath, nil)
			s.mu.Unlock()
			s.logger.Info("proxy_state_dropped_after_probe_failures", "failures", n)
			return httpproxy.Status{Running: false}
		}
		s.mu.Unlock()
		// Hysteresis: a transient dial timeout keeps a healthy proxy
		// supervised, reported degraded, until probeFailThreshold in a row.
		return httpproxy.Status{
			Running:   true,
			Addr:      addr,
			LastError: fmt.Sprintf("status_probe_failed: port not answering (%d/%d)", n, probeFailThreshold),
		}
	}
	s.mu.Lock()
	s.portFails = 0
	s.mu.Unlock()

	st := httpproxy.Status{Running: true, Addr: addr}
	resp, err := s.probe.Get("http://" + addr + "/_status")
	if err != nil {
		// Process alive + port open but /_status did not answer. The
		// kill-refusal guards key off the "status_probe_failed:" prefix —
		// keep it byte-compatible.
		st.LastError = "status_probe_failed: " + err.Error()
		return st
	}
	if resp.StatusCode == http.StatusOK {
		_ = json.NewDecoder(resp.Body).Decode(&st)
	}
	_ = resp.Body.Close()
	return st
}

// Reconcile reads the on-disk state, validates the PID is still alive and the
// port is listening, and drops orphaned state. Call once at TUI boot.
func (s *Supervisor) reconcileProcess() error {
	st, err := loadState(s.statePath)
	if err != nil {
		s.logger.Error("proxy_reconcile_load_failed", "err", err)
		return err
	}
	if st == nil {
		return nil
	}

	drop := func(reason string) {
		s.logger.Info("proxy_reconcile_dropped", "pid", st.PID, "reason", reason)
		if err := saveState(s.statePath, nil); err != nil {
			s.logger.Error("proxy_reconcile_cleanup_failed", "err", err)
		}
	}
	if st.StartTicks != 0 {
		if !procutil.SameProcess(st.PID, st.StartTicks) {
			drop("pid_recycled_or_dead")
			return nil
		}
		if !portOpen(st.Host, st.Port) {
			// Alive by identity but not answering: the serve is wedged. Kill it
			// so a fresh proxy can bind the port (audit A9).
			s.logger.Info("proxy_reconcile_killed_wedged", "pid", st.PID)
			_ = procutil.TerminateTree(st.PID, 5*time.Second)
			drop("wedged")
			return nil
		}
	} else if !procutil.Alive(st.PID) || !portOpen(st.Host, st.Port) {
		drop("pid_or_port_mismatch") // legacy state: pre-identity behavior
		return nil
	}

	s.mu.Lock()
	s.state = st
	s.portFails = 0
	s.mu.Unlock()
	s.logger.Info("proxy_reconcile_kept", "pid", st.PID, "addr", s.addr(st))
	return nil
}

// processAliveLocked reports whether the tracked proxy process is still our
// process. Identity-checked when StartTicks is present (audit A9); legacy
// states fall back to Alive && portOpen exactly as before.
func (s *Supervisor) processAliveLocked() bool {
	if s.state == nil {
		return false
	}
	if s.state.StartTicks != 0 {
		return procutil.SameProcess(s.state.PID, s.state.StartTicks)
	}
	return procutil.Alive(s.state.PID) && portOpen(s.state.Host, s.state.Port)
}

// ForceStop SIGKILLs the proxy immediately (no drain, no grace) and clears
// persisted state. Escape hatch for a wedged/degraded proxy that no longer
// answers /_status (audit A13). Identity-checked: a recycled PID is never
// signaled. Returns an error when no proxy state exists.
func (s *Supervisor) forceStopProcess() error {
	s.mu.Lock()
	st := s.state
	s.mu.Unlock()
	if st == nil {
		return fmt.Errorf("proxy not running")
	}
	alive := procutil.Alive(st.PID)
	if st.StartTicks != 0 {
		alive = procutil.SameProcess(st.PID, st.StartTicks)
	}
	if alive {
		_ = procutil.TerminateTree(st.PID, 0)
	}
	s.mu.Lock()
	s.state = nil
	s.portFails = 0
	s.mu.Unlock()
	if err := saveState(s.statePath, nil); err != nil {
		s.logger.Error("proxy_force_stop_cleanup_failed", "err", err)
	}
	s.logger.Info("proxy_force_stopped", "pid", st.PID)
	return nil
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

func portOpen(host string, port int) bool {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

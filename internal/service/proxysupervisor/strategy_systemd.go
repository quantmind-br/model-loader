package proxysupervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
	"github.com/quantmind-br/model-loader/internal/service/internal/procutil"
)

// This file implements the systemd strategy: when the proxy unit is loaded
// in user systemd, the TUI drives the proxy through systemctl instead of
// spawning a detached process. Gateway and Tunnel units bind to the proxy
// unit's lifecycle (BindsTo), so they follow it without any polling.
//
// Strategy selection happens once in New via detectSystemd. A
// systemd-selected Supervisor never falls back to process spawning
// mid-operation: a silent fallback could start a second proxy while the
// unit still owns the port. Errors are returned to the caller instead.

// startSystemd starts the proxy unit and waits for it to reach
// active/running with a healthy /_status, bounded by ctx.
func (s *Supervisor) startSystemd(ctx context.Context) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	if p, err := queryUnit(ctx); err == nil && unitRunning(p) {
		return fmt.Errorf("%w in unit %s (pid %d)", ErrAlreadyRunning, ProxyUnitName, p.mainPID)
	}

	if _, err := runSystemctl(ctx, "--user", "start", ProxyUnitName); err != nil {
		return fmt.Errorf("start %s: %w", ProxyUnitName, err)
	}

	p, err := waitUnitState(ctx, unitRunning)
	if err != nil {
		return fmt.Errorf("start %s: %w", ProxyUnitName, err)
	}
	if err := s.waitForStatusHTTP(ctx); err != nil {
		return fmt.Errorf("start %s (pid %d): %w", ProxyUnitName, p.mainPID, err)
	}
	s.logger.Info("proxy_started", "strategy", "systemd", "unit", ProxyUnitName, "pid", p.mainPID)
	return nil
}

// stopSystemd stops the unit gracefully and sweeps a residual backend when
// the shutdown was not clean, mirroring the process strategy's audit-A8
// sweep.
func (s *Supervisor) stopSystemd(ctx context.Context) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	p, err := queryUnit(ctx)
	if err != nil {
		return fmt.Errorf("query %s: %w", ProxyUnitName, err)
	}
	if p.active == "inactive" || p.active == "failed" {
		s.dropLegacyState("systemd_unit_inactive")
		return fmt.Errorf("proxy not running")
	}

	loadedPID := s.statusHTTP().LoadedPID

	if _, err := runSystemctl(ctx, "--user", "stop", ProxyUnitName); err != nil {
		return fmt.Errorf("stop %s: %w", ProxyUnitName, err)
	}
	if _, err := waitUnitState(ctx, func(q unitProps) bool {
		return q.active == "inactive" || q.active == "failed"
	}); err != nil {
		return fmt.Errorf("stop %s: %w", ProxyUnitName, err)
	}

	s.sweepResidualBackend(loadedPID)
	s.dropLegacyState("systemd_stop")
	s.logger.Info("proxy_stopped", "strategy", "systemd", "unit", ProxyUnitName)
	return nil
}

// forceStopSystemd SIGKILLs the whole unit cgroup (proxy + descendants),
// resets the unit, and sweeps a backend that may have survived the kill.
func (s *Supervisor) forceStopSystemd() error {
	p, err := queryUnit(context.Background())
	if err != nil {
		return fmt.Errorf("query %s: %w", ProxyUnitName, err)
	}
	if p.active == "inactive" || p.active == "failed" {
		s.dropLegacyState("systemd_unit_inactive")
		return fmt.Errorf("proxy not running")
	}

	loadedPID := s.statusHTTP().LoadedPID

	if _, err := runSystemctl(context.Background(), "--user", "kill",
		"--signal=SIGKILL", "--kill-whom=all", ProxyUnitName); err != nil {
		return fmt.Errorf("kill %s: %w", ProxyUnitName, err)
	}
	// stop/reset-failed may fail when the kill already deactivated the unit;
	// either way confirm inactivity below.
	_, _ = runSystemctl(context.Background(), "--user", "stop", ProxyUnitName)
	_, _ = runSystemctl(context.Background(), "--user", "reset-failed", ProxyUnitName)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := waitUnitState(ctx, func(q unitProps) bool {
		return q.active == "inactive" || q.active == "failed"
	}); err != nil {
		return fmt.Errorf("force stop %s: %w", ProxyUnitName, err)
	}

	s.sweepResidualBackend(loadedPID)
	s.dropLegacyState("systemd_force_stop")
	s.logger.Info("proxy_force_stopped", "strategy", "systemd", "unit", ProxyUnitName)
	return nil
}

// statusSystemd reports the unit state combined with the HTTP probe. An
// active unit with unreachable HTTP stays Running=true with LastError set
// (degraded), preserving the kill-refusal/ForceStop escape-hatch semantics
// of the process strategy.
func (s *Supervisor) statusSystemd() httpproxy.Status {
	p, err := queryUnit(context.Background())
	if err != nil {
		// Control-plane hiccup: fall back to the HTTP probe so a transient
		// systemctl failure does not flap the 1 Hz TUI poll. Proof of life
		// (healthy /_status) beats a failed control query.
		st := s.statusHTTP()
		if st.LastError == "" {
			return st
		}
		return httpproxy.Status{Running: false, LastError: "status_probe_failed: " + err.Error()}
	}
	if !unitRunning(p) {
		return httpproxy.Status{Running: false}
	}
	st := s.statusHTTP()
	if st.LastError != "" {
		st.Running = true
		if st.Addr == "" {
			st.Addr = s.addr(&State{Host: s.host, Port: s.port})
		}
	}
	return st
}

// reconcileSystemd drops only provably-obsolete legacy state (dead or
// recycled PID). It never adopts disk state into memory and never writes
// proxy-state.json: in systemd mode the unit is the source of truth, and a
// live PID in that file may be a manually run `serve` that must not be
// touched.
func (s *Supervisor) reconcileSystemd() error {
	st, err := loadState(s.statePath)
	if err != nil {
		s.logger.Error("proxy_reconcile_load_failed", "err", err)
		return err
	}
	if st == nil {
		return nil
	}
	obsolete := false
	if st.StartTicks != 0 {
		obsolete = !procutil.SameProcess(st.PID, st.StartTicks)
	} else {
		obsolete = !procutil.Alive(st.PID)
	}
	if obsolete {
		s.logger.Info("proxy_reconcile_dropped", "pid", st.PID, "reason", "legacy_obsolete_under_systemd")
		if err := saveState(s.statePath, nil); err != nil {
			s.logger.Error("proxy_reconcile_cleanup_failed", "err", err)
		}
	}
	return nil
}

// statusHTTP probes /_status at the configured addr. Transport/decoding
// failures yield Running=true with a "status_probe_failed:" LastError so
// kill-refusal guards keep working; only a clean 200 decode reports the
// real state.
func (s *Supervisor) statusHTTP() httpproxy.Status {
	addr := s.addr(&State{Host: s.host, Port: s.port})
	st := httpproxy.Status{Running: true, Addr: addr}
	resp, err := s.probe.Get("http://" + addr + "/_status")
	if err != nil {
		st.LastError = "status_probe_failed: " + err.Error()
		return st
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		_ = json.NewDecoder(resp.Body).Decode(&st)
		return st
	}
	st.LastError = fmt.Sprintf("status_probe_failed: HTTP %s", resp.Status)
	return st
}

// waitForStatusHTTP polls /_status until healthy or ctx expires.
func (s *Supervisor) waitForStatusHTTP(ctx context.Context) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		st := s.statusHTTP()
		if st.Running && st.LastError == "" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("proxy /_status unhealthy: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// sweepResidualBackend kills a backend that survived the proxy's death
// (SIGKILL before serve's own killCurrentBackend ran), mirroring audit A8.
func (s *Supervisor) sweepResidualBackend(pid int) {
	if pid > 0 && procutil.Alive(pid) {
		_ = procutil.TerminateTree(pid, 5*time.Second)
		s.logger.Info("proxy_stop_swept_backend", "backend_pid", pid)
	}
}

// dropLegacyState removes proxy-state.json best-effort. In systemd mode the
// file is legacy only; absence is not an error.
func (s *Supervisor) dropLegacyState(reason string) {
	if st, err := loadState(s.statePath); err == nil && st != nil {
		s.logger.Info("proxy_reconcile_dropped", "pid", st.PID, "reason", reason)
		if err := saveState(s.statePath, nil); err != nil {
			s.logger.Error("proxy_reconcile_cleanup_failed", "err", err)
		}
	}
}

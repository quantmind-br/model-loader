package processmgr

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime/debug"
	"syscall"
	"time"
)

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

	if cur.RestartPolicy != "" && cur.RestartPolicy != "none" {
		shouldRestart := cur.RestartPolicy == "always" ||
			(cur.RestartPolicy == "on-failure" && exitCode != nil && *exitCode != 0)
		if shouldRestart && (cur.MaxRestarts <= 0 || cur.RestartCount < cur.MaxRestarts) {
			cur.RestartCount++
			now := time.Now().UTC()
			cur.LastRestartAt = &now
			m.mu.Lock()
			m.tracked[pid] = cur
			m.mu.Unlock()
			backoff := time.Duration(cur.BackoffSeconds) * time.Second
			if cur.RestartCount > 1 {
				backoff = time.Duration(cur.BackoffSeconds*cur.RestartCount) * time.Second
			}
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			m.logger.Info("watchdog_restart",
				"pid", pid, "profile_id", cur.ProfileID,
				"restart_count", cur.RestartCount,
				"backoff", backoff.String())
			if backoff > 0 {
				time.Sleep(backoff)
			}
			if m.restartFunc != nil {
				m.restartFunc(cur.ProfileID)
			}
		}
	}
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

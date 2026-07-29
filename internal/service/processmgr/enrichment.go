package processmgr

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/procutil"
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
		if !procutil.Alive(pid) {
			lg.Warn("healthcheck_process_exited")
			return fmt.Errorf("port %d: %w", port, ErrProcessExited)
		}
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				m.mu.Lock()
				inst, ok := m.tracked[pid]
				if ok {
					// A healthy check resets the restart budget: MaxRestarts
					// bounds consecutive FAILED starts, not lifetime restarts
					// (audit A5).
					inst.RestartCount = 0
					m.tracked[pid] = inst
				}
				m.mu.Unlock()
				if ok && m.sink != nil {
					// best-effort; bookkeeping failure must not abort a healthy launch
					_ = m.sink.MarkLastUsed(inst.ProfileID, time.Now().UTC())
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
	// Deliberate termination: Kill set the intent flag under m.mu before
	// signaling, so a raw "signal:SIGTERM" here would mislabel an operator stop.
	if _, intentional := m.killRequested[pid]; intentional {
		reason = domain.ExitReasonOperatorStop
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
	// The reaper produced this exit's enrichment; hand restart duty back so a
	// late liveness tick cannot double-fire (audit A10).
	delete(m.hasReaper, pid)
	now := time.Now().UTC()
	appended := m.appendHistoryLocked(cur, reason, now)
	m.mu.Unlock()

	// out-of-m.mu mutateRegistry callsite (flock-guarded delta). See AGENTS.md.
	_ = mutateRegistry(m.registryPath, func(reg map[int]domain.RunningInstance) {
		reg[pid] = cur
	})
	if appended {
		_ = m.persistHistory()
	}

	m.logger.Info("process_exited",
		"pid", pid, "attempt_id", attemptID,
		"exit_reason", reason,
		"stderr_tail_lines", len(tail))

	// Reaper-driven restart decision (audit A4/A5). The kill-intent guard,
	// set-once dedupe, MaxRestarts cap, and backoff live in maybeScheduleRestart.
	m.maybeScheduleRestart(cur, exitCode)
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

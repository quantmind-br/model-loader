package benchmark

import (
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// --- helpers ---------------------------------------------------------------

// shrinkWatchdogTiming shrinks the package-level watchdog timing knobs so the
// safety nets fire in milliseconds instead of minutes, restoring the production
// defaults when the test ends. Tests run sequentially (no t.Parallel), so the
// shared vars are never observed mid-swap by another test. UIUX-012.
func shrinkWatchdogTiming(t *testing.T, interval, postGrace, termGrace time.Duration) {
	t.Helper()
	oInterval, oPostGrace, oTermGrace := tbWatchInterval, tbPostCompleteGrace, tbTermGrace
	t.Cleanup(func() {
		tbWatchInterval, tbPostCompleteGrace, tbTermGrace = oInterval, oPostGrace, oTermGrace
	})
	tbWatchInterval, tbPostCompleteGrace, tbTermGrace = interval, postGrace, termGrace
}

// startSleeper spawns `sleep 30` in its OWN process group (Setpgid) so the
// watchdog's group-directed SIGTERM (syscall.Kill(-pid, ...)) reaches the child
// without touching the test's own group. Cleanup force-kills the group so a
// failing test never leaks the process. UIUX-012.
func startSleeper(t *testing.T) (*exec.Cmd, int) {
	t.Helper()
	c := exec.Command("sleep", "30")
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.Start(); err != nil {
		t.Fatalf("start sleeper: %v", err)
	}
	pid := c.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
	return c, pid
}

// aggregateResults builds a tb aggregate results.json body with n trial
// entries — the shape tbReadAggregateResultsTotal counts as completed trials.
func aggregateResults(n int) string {
	var b strings.Builder
	b.WriteString(`{"results":[`)
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("{}")
	}
	b.WriteString("]}")
	return b.String()
}

// waitAndSignalStop mirrors Execute's coordination: once the tb child exits
// (here because the watchdog SIGTERM'd it), close stop so tbKillGroup returns
// through its <-stop arm instead of escalating to cancel(). The child's Wait
// error is delivered on the returned channel for the "was it killed?" assertion.
func waitAndSignalStop(c *exec.Cmd, stop chan struct{}) <-chan error {
	waitErr := make(chan error, 1)
	go func() {
		err := c.Wait()
		waitErr <- err
		close(stop)
	}()
	return waitErr
}

// --- tests -----------------------------------------------------------------

// TestTbHangWatchdog_CompletionKill covers the completion safety net: once every
// task is scored (results.json lists `total` trials) but the tb harness keeps
// running past the post-complete grace, the watchdog group-kills it and records
// tbWatchCompleted. Regression: a finished tb run left hammering the proxy pins
// the GPU for hours. UIUX-012.
func TestTbHangWatchdog_CompletionKill(t *testing.T) {
	shrinkWatchdogTiming(t, 10*time.Millisecond, 20*time.Millisecond, 500*time.Millisecond)

	const total = 3
	runDir := t.TempDir()
	// Aggregate results with `total` entries → done >= total on the first tick.
	mustWrite(t, filepath.Join(runDir, "results.json"), aggregateResults(total))

	c, pid := startSleeper(t)

	stop := make(chan struct{})
	var reason atomic.Int32
	progress := make(chan Progress, 8)
	waitErr := waitAndSignalStop(c, stop)

	wdDone := make(chan struct{})
	go func() {
		tbHangWatchdog(Config{}, runDir, total, pid, func() {}, progress, &reason, stop)
		close(wdDone)
	}()

	select {
	case <-wdDone:
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not fire and return within 5s")
	}

	if err := <-waitErr; err == nil {
		t.Fatal("child exited cleanly; watchdog did not kill the tb process group")
	}
	if got := tbWatchReason(reason.Load()); got != tbWatchCompleted {
		t.Fatalf("reason = %d, want tbWatchCompleted (%d)", got, tbWatchCompleted)
	}
}

// TestTbHangWatchdog_StallKill covers the stall safety net: when no new task is
// scored for the stall timeout, the watchdog group-kills the wedged tb harness
// and records tbWatchStalled. Regression: a stuck agent/Docker task otherwise
// hangs forever on the GPU. UIUX-012.
func TestTbHangWatchdog_StallKill(t *testing.T) {
	shrinkWatchdogTiming(t, 10*time.Millisecond, 20*time.Millisecond, 500*time.Millisecond)

	const total = 5
	runDir := t.TempDir() // no results.json → completed count never advances

	c, pid := startSleeper(t)

	stop := make(chan struct{})
	var reason atomic.Int32
	progress := make(chan Progress, 8)
	waitErr := waitAndSignalStop(c, stop)

	cfg := Config{TerminalBenchStallTimeout: 30 * time.Millisecond}
	wdDone := make(chan struct{})
	go func() {
		tbHangWatchdog(cfg, runDir, total, pid, func() {}, progress, &reason, stop)
		close(wdDone)
	}()

	select {
	case <-wdDone:
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not fire and return within 5s")
	}

	if err := <-waitErr; err == nil {
		t.Fatal("child exited cleanly; watchdog did not kill the tb process group")
	}
	if got := tbWatchReason(reason.Load()); got != tbWatchStalled {
		t.Fatalf("reason = %d, want tbWatchStalled (%d)", got, tbWatchStalled)
	}
}

// TestTbHangWatchdog_StopBeforeFire covers a healthy run: when Execute closes
// stop before the watchdog's first tick, the watchdog returns promptly without
// signaling the process group, leaves reason at tbWatchNone, and never escalates
// to cancel. Guards against a spurious kill on a fast, healthy tb run. The
// hour-long interval guarantees no tick can race the already-closed stop, so
// the only reachable exit is the <-stop arm. UIUX-012.
func TestTbHangWatchdog_StopBeforeFire(t *testing.T) {
	shrinkWatchdogTiming(t, time.Hour, time.Hour, time.Hour)

	runDir := t.TempDir()
	c, pid := startSleeper(t)

	stop := make(chan struct{})
	var reason atomic.Int32
	var canceled atomic.Bool
	progress := make(chan Progress, 8)

	close(stop) // healthy run finished before the watchdog ever ticked

	wdDone := make(chan struct{})
	go func() {
		tbHangWatchdog(Config{}, runDir, 3, pid, func() { canceled.Store(true) }, progress, &reason, stop)
		close(wdDone)
	}()

	select {
	case <-wdDone:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not return promptly after stop closed")
	}

	if got := tbWatchReason(reason.Load()); got != tbWatchNone {
		t.Fatalf("reason = %d, want tbWatchNone (%d) — watchdog fired on a healthy run", got, tbWatchNone)
	}
	if canceled.Load() {
		t.Fatal("cancel called on a healthy run — watchdog escalated spuriously")
	}
	// signal 0 is an existence probe: nil means the sleeper is still alive.
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("child not alive after a clean stop (%v); watchdog killed a healthy run", err)
	}

	_ = syscall.Kill(-pid, syscall.SIGKILL) // reap the sleeper
	_ = c.Wait()
}

// TestTbStallTimeout covers the stall-timeout resolution: an unset config falls
// back to the package default, a positive config value wins. UIUX-012.
func TestTbStallTimeout(t *testing.T) {
	if got := tbStallTimeout(Config{}); got != tbDefaultStallTimeout {
		t.Errorf("unset TerminalBenchStallTimeout = %v, want package default %v", got, tbDefaultStallTimeout)
	}
	if got := tbStallTimeout(Config{TerminalBenchStallTimeout: 7 * time.Second}); got != 7*time.Second {
		t.Errorf("configured TerminalBenchStallTimeout = %v, want 7s", got)
	}
}

// TestUnloadAfterRun_CallsProxyUnload covers the auto-unload contract: the
// helper frees the loaded model by issuing exactly one proxy Unload. Regression:
// long agentic runs left the backend resident on the GPU after finishing.
// UIUX-012.
func TestUnloadAfterRun_CallsProxyUnload(t *testing.T) {
	fp := &fakeProxyCtl{}
	r := &Runner{proxy: fp}

	r.unloadAfterRun()

	if fp.unloads != 1 {
		t.Fatalf("proxy Unload calls = %d, want 1", fp.unloads)
	}
}

package benchmark

import (
	"sync/atomic"
	"testing"
	"time"
)

// These tests lock the GENERIC contract of runHangWatchdog independent of the
// terminal-bench file layout: the shared core is driven by an arbitrary
// `completed func() int` closure (deep-swe hands it
// deepCountCompletedTrials(runDir)), so proving it here with a closure the test
// fully controls proves the core is not coupled to tb's results.json. They
// reuse the helpers in terminalbench_watchdog_test.go (shrinkWatchdogTiming,
// startSleeper, waitAndSignalStop). UIUX-012.

// TestRunHangWatchdog_CompletionKill_ArbitraryClosure covers the completion
// safety net of the shared core: once the caller's own completed() closure
// reports every task scored (done >= total) and the harness keeps running past
// the post-complete grace, the watchdog group-kills the process and records
// tbWatchCompleted — with no tb run directory involved at all. Regression: a
// finished agentic run left hammering the proxy pins the GPU for hours.
// UIUX-012.
func TestRunHangWatchdog_CompletionKill_ArbitraryClosure(t *testing.T) {
	shrinkWatchdogTiming(t, 10*time.Millisecond, 20*time.Millisecond, 500*time.Millisecond)

	const total = 3
	// A closure the test drives directly (no results.json): it reports "not
	// done yet" on the first poll, then flips to fully scored, proving the core
	// polls the closure and reacts to its returns. Touched only from the
	// watchdog goroutine, so no synchronization is needed.
	calls := 0
	completedFn := func() int {
		calls++
		if calls >= 2 {
			return total
		}
		return 0
	}

	c, pid := startSleeper(t)

	stop := make(chan struct{})
	var reason atomic.Int32
	progress := make(chan Progress, 8)
	waitErr := waitAndSignalStop(c, stop)

	wdDone := make(chan struct{})
	go func() {
		// stall an hour away so only the completion path can fire.
		runHangWatchdog("deep-swe", completedFn, total, pid, time.Hour, func() {}, progress, &reason, stop)
		close(wdDone)
	}()

	select {
	case <-wdDone:
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not fire and return within 5s")
	}

	if err := <-waitErr; err == nil {
		t.Fatal("child exited cleanly; watchdog did not kill the process group")
	}
	if got := tbWatchReason(reason.Load()); got != tbWatchCompleted {
		t.Fatalf("reason = %d, want tbWatchCompleted (%d)", got, tbWatchCompleted)
	}
}

// TestRunHangWatchdog_StallKill_ArbitraryClosure covers the stall safety net of
// the shared core driven by an arbitrary closure: completed() never advances
// (always 0) while total is large, so after the tiny stall passed straight to
// runHangWatchdog the watchdog group-kills the wedged process and records
// tbWatchStalled. Proves the stall path works for any agentic mode, not just
// tb. Regression: a stuck agent/Docker task otherwise hangs forever on the GPU.
// UIUX-012.
func TestRunHangWatchdog_StallKill_ArbitraryClosure(t *testing.T) {
	shrinkWatchdogTiming(t, 10*time.Millisecond, 20*time.Millisecond, 500*time.Millisecond)

	const total = 1000 // large so the completion path can never trigger
	completedFn := func() int { return 0 }

	c, pid := startSleeper(t)

	stop := make(chan struct{})
	var reason atomic.Int32
	progress := make(chan Progress, 8)
	waitErr := waitAndSignalStop(c, stop)

	wdDone := make(chan struct{})
	go func() {
		// tiny stall passed directly, no Config involved.
		runHangWatchdog("deep-swe", completedFn, total, pid, 30*time.Millisecond, func() {}, progress, &reason, stop)
		close(wdDone)
	}()

	select {
	case <-wdDone:
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not fire and return within 5s")
	}

	if err := <-waitErr; err == nil {
		t.Fatal("child exited cleanly; watchdog did not kill the process group")
	}
	if got := tbWatchReason(reason.Load()); got != tbWatchStalled {
		t.Fatalf("reason = %d, want tbWatchStalled (%d)", got, tbWatchStalled)
	}
}

// TestDeepStallTimeout covers deep-swe's stall-timeout resolution: an unset
// config falls back to the shared package default, a positive config value
// wins. Mirrors tbStallTimeout's contract for the deep-swe wiring. UIUX-012.
func TestDeepStallTimeout(t *testing.T) {
	if got := deepStallTimeout(Config{}); got != tbDefaultStallTimeout {
		t.Errorf("unset DeepSWEStallTimeout = %v, want package default %v", got, tbDefaultStallTimeout)
	}
	if got := deepStallTimeout(Config{DeepSWEStallTimeout: 9 * time.Second}); got != 9*time.Second {
		t.Errorf("configured DeepSWEStallTimeout = %v, want 9s", got)
	}
}

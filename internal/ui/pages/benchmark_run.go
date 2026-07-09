package pages

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

// benchRunsLoadedMsg delivers the persisted runs to the page.
type benchRunsLoadedMsg struct {
	runs []benchmark.Run
	err  error
}

// benchCancelConfirmedMsg is emitted by cancelConfirm.onYes once the user
// confirms cancelling an in-flight run; performCancel runs the actual
// runCancel in Update so the goroutine teardown stays on the page.
type benchCancelConfirmedMsg struct{}

// benchProgressMsg carries one engine progress event plus the channel for
// re-arming the next read (mirrors the picker scan-event pattern).
type benchProgressMsg struct {
	p  benchmark.Progress
	ch chan benchmark.Progress
}

// benchProgressClosedMsg signals the progress channel closed.
type benchProgressClosedMsg struct{}

// benchRunTickMsg drives a 1s repaint of the live-run view so elapsed time,
// staleness, and the activity log stay current between engine progress events.
// Re-armed in Update only while the running view is active.
type benchRunTickMsg struct{}

// runTick schedules the next live-run repaint.
func (p BenchmarkPage) runTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return benchRunTickMsg{} })
}

// captureFeed grabs the runner's current live feed (nil-safe) so the running
// view renders authoritative snapshot state, not the lossy progress channel.
func (p BenchmarkPage) captureFeed() BenchmarkPage {
	if p.runner != nil {
		if f := p.runner.Feed(); f != nil {
			p.feed = f
		}
	}
	return p
}

// benchRunDoneMsg is delivered when the engine finishes (or errors).
type benchRunDoneMsg struct {
	run benchmark.Run
	err error
}

func (p BenchmarkPage) loadRunsCmd() tea.Cmd {
	store := p.bstore
	return func() tea.Msg {
		runs, err := store.List()
		return benchRunsLoadedMsg{runs: runs, err: err}
	}
}

// startRun spawns the engine in a goroutine, streaming progress over a channel
// and delivering the final result via benchRunDoneMsg.
func (p BenchmarkPage) startRun() (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	prog := make(chan benchmark.Progress, 32)
	done := make(chan benchRunDoneMsg, 1)
	runDone := make(chan struct{})
	bstore := p.bstore
	rc := benchmark.RunConfig{
		ProfileID: p.selectedProfileID,
		Mode:      benchModes[p.modeCursor],
		// T7: persist partial progress mid-run (flagged Err="in progress");
		// handleRunDone's final Save overwrites it on completion.
		Checkpoint: func(partial benchmark.Run) { _ = bstore.Save(partial) },
	}
	runner := p.runner

	go func() {
		run, err := runner.Run(ctx, rc, prog)
		close(prog)
		done <- benchRunDoneMsg{run: run, err: err} // buffered(1), never blocks
		close(runDone)                              // signals Cleanup the engine acknowledged cancel
	}()

	p.runCancel = cancel
	p.runDone = runDone
	p.progressCh = prog
	p.runningMode = rc.Mode
	p.view = bvRunning
	p.feed = nil

	return p, tea.Batch(waitProgress(prog), waitDone(done), p.spinner.Tick, p.runTick())
}

func waitProgress(ch chan benchmark.Progress) tea.Cmd {
	return func() tea.Msg {
		pgr, ok := <-ch
		if !ok {
			return benchProgressClosedMsg{}
		}
		return benchProgressMsg{p: pgr, ch: ch}
	}
}

func waitDone(ch chan benchRunDoneMsg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func (p BenchmarkPage) handleRunDone(msg benchRunDoneMsg) (tea.Model, tea.Cmd) {
	if p.runCancel != nil {
		p.runCancel()
		p.runCancel = nil
	}
	var fc tea.Cmd
	if msg.err != nil {
		// A failed/cancelled run that completed some problems is still data:
		// persist it flagged as partial (run.Err is set by the engine). Runs
		// that died before producing anything (launch failure) are not saved.
		if len(msg.run.Problems) == 0 {
			p, fc = p.withFlashError("run failed: " + msg.err.Error())
			p.view = bvDashboard
			return p, fc
		}
		if err := p.bstore.Save(msg.run); err != nil {
			p, fc = p.withFlashError("save partial run: " + err.Error())
			p.view = bvDashboard
			return p, tea.Batch(fc, p.loadRunsCmd())
		}
		p = p.openDetail(msg.run, bvDashboard)
		p, fc = p.withFlashError("run incomplete (saved partial): " + msg.err.Error())
		return p, tea.Batch(fc, p.loadRunsCmd())
	}
	if err := p.bstore.Save(msg.run); err != nil {
		p, fc = p.withFlashError("save run: " + err.Error())
		p.view = bvDashboard
		return p, tea.Batch(fc, p.loadRunsCmd())
	}
	p = p.openDetail(msg.run, bvDashboard)
	p, fc = p.withFlash("benchmark complete")
	return p, tea.Batch(fc, p.loadRunsCmd())
}

// Cleanup cancels an in-flight benchmark run on TUI quit and waits (bounded)
// for the engine to acknowledge, so the external harness process group
// (tb / SWE-bench / DeepSWE + Docker) is group-killed via cmd.Cancel before
// the process exits (audit N-C15). No-ops when idle (handleRunDone nils
// runCancel on completion).
func (p BenchmarkPage) Cleanup() {
	if p.webViewer != nil {
		p.webViewer.Cancel()
	}
	if p.runCancel == nil {
		return
	}
	p.runCancel()
	if p.runDone != nil {
		select {
		case <-p.runDone:
		case <-time.After(5 * time.Second):
		}
	}
}

// keyRunning handles keys during bvRunning. Esc arms the cancelConfirm instead
// of cancelling directly, so a stray esc does not throw away a long run. Any
// other key is a no-op (the run owns the screen).
func (p BenchmarkPage) keyRunning(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		var cmd tea.Cmd
		p.cancelConfirm, cmd = setupConfirm("Cancel this benchmark run?", "Cancel run", "Keep running",
			func() tea.Cmd { return func() tea.Msg { return benchCancelConfirmedMsg{} } })
		return p, cmd
	}
	return p, nil
}

// performCancel runs the in-flight run's cancellation context. It is invoked
// from Update on benchCancelConfirmedMsg (the cancelConfirm affirmative path).
func (p BenchmarkPage) performCancel() (tea.Model, tea.Cmd) {
	if p.runCancel != nil {
		p.runCancel()
	}
	// Leave the cancelConfirm cleared (huh completed clears it); the actual
	// bvRunning→bvDashboard transition happens in handleRunDone when the
	// cancelled run's partial result is processed.
	var fc tea.Cmd
	p, fc = p.withFlash("run cancelled")
	return p, fc
}

package pages

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

// benchRunsLoadedMsg delivers the persisted runs to the page.
type benchRunsLoadedMsg struct {
	runs []benchmark.Run
	err  error
}

// benchProgressMsg carries one engine progress event plus the channel for
// re-arming the next read (mirrors the picker scan-event pattern).
type benchProgressMsg struct {
	p  benchmark.Progress
	ch chan benchmark.Progress
}

// benchProgressClosedMsg signals the progress channel closed.
type benchProgressClosedMsg struct{}

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
	rc := benchmark.RunConfig{ProfileID: p.selectedProfileID, Mode: benchModes[p.modeCursor]}
	runner := p.runner

	go func() {
		run, err := runner.Run(ctx, rc, prog)
		close(prog)
		done <- benchRunDoneMsg{run: run, err: err}
	}()

	p.runCancel = cancel
	p.progressCh = prog
	p.runningMode = rc.Mode
	p.view = bvRunning
	p.progress = benchmark.Progress{Total: runner.CountForMode(rc.Mode)}

	return p, tea.Batch(waitProgress(prog), waitDone(done), p.spinner.Tick)
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
	if msg.err != nil {
		// A failed/cancelled run that completed some problems is still data:
		// persist it flagged as partial (run.Err is set by the engine). Runs
		// that died before producing anything (launch failure) are not saved.
		if len(msg.run.Problems) == 0 {
			p.flash, _ = flashError(p.flash, "run failed: "+msg.err.Error())
			p.view = bvList
			return p, nil
		}
		if err := p.bstore.Save(msg.run); err != nil {
			p.flash, _ = flashError(p.flash, "save partial run: "+err.Error())
			p.view = bvList
			return p, p.loadRunsCmd()
		}
		run := msg.run
		p.detail = &run
		p.view = bvRunDetail
		p.flash, _ = flashError(p.flash, "run incomplete (saved partial): "+msg.err.Error())
		return p, p.loadRunsCmd()
	}
	if err := p.bstore.Save(msg.run); err != nil {
		p.flash, _ = flashError(p.flash, "save run: "+err.Error())
		p.view = bvList
		return p, p.loadRunsCmd()
	}
	run := msg.run
	p.detail = &run
	p.view = bvRunDetail
	var fc tea.Cmd
	p.flash, fc = flashSuccess(p.flash, "benchmark complete")
	return p, tea.Batch(fc, p.loadRunsCmd())
}

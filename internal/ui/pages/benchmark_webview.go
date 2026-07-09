package pages

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/service/configweb"
)

// benchWebStartedMsg reports a booted read-only benchmark viewer session.
type benchWebStartedMsg struct {
	viewer *configweb.BenchViewer
	url    string
}

// benchWebDoneMsg fires when the viewer session shuts down (browser done page
// served or the operator pressed esc → Cancel).
type benchWebDoneMsg struct{}

// benchWebFailedMsg reports a viewer that failed to bind/serve.
type benchWebFailedMsg struct{ err error }

// startBenchWeb boots a configweb benchmark viewer (saved runs + live monitor)
// and opens the browser. The live monitor observes THIS TUI process's runner, so
// a TUI-launched run streams into /live directly.
func (p BenchmarkPage) startBenchWeb() tea.Cmd {
	deps := configweb.BenchViewerDeps{
		Runs:     p.bstore,
		Profiles: p.store,
		Live: func() *benchmark.RunFeed {
			if p.runner == nil {
				return nil
			}
			return p.runner.Feed()
		},
	}
	return func() tea.Msg {
		v := configweb.NewBenchViewer(deps)
		url, err := v.Start()
		if err != nil {
			return benchWebFailedMsg{err: err}
		}
		openBrowser(url)
		return benchWebStartedMsg{viewer: v, url: url}
	}
}

// waitForBenchWeb blocks on the viewer's Done channel in a Cmd goroutine so the
// page learns when the session ends and can drop the capturing state.
func waitForBenchWeb(v *configweb.BenchViewer) tea.Cmd {
	return func() tea.Msg {
		<-v.Done()
		return benchWebDoneMsg{}
	}
}

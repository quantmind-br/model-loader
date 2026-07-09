package pages

import (
	"path/filepath"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

// benchmarkDeleteConfirmedMsg is emitted by deleteConfirm.onYes once the user
// confirms a run deletion; performDelete runs the actual bstore.Delete in
// Update so store I/O stays on the page (DESTRUCT-01).
type benchmarkDeleteConfirmedMsg struct{ id string }

func (p BenchmarkPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = m.Width, m.Height
		return p, nil
	case components.FlashClearMsg:
		p.flash, _ = p.flash.Update(m)
		return p, nil
	case benchmarkDeleteConfirmedMsg:
		return p.performDelete(m.id)
	case benchCancelConfirmedMsg:
		return p.performCancel()
	case benchRunsLoadedMsg:
		if m.err == nil {
			p.runs = m.runs
			// Keep the dashboard cursor in range after a reload: a delete or
			// a focus-mode change can shrink the leaderboard under it.
			if rows := dashboardRows(p.runs, p.focusedDashboardMode()); p.dashCursor >= len(rows) {
				p.dashCursor = 0
			}
		}
		return p, nil
	case benchProgressMsg:
		p = p.captureFeed()
		return p, waitProgress(m.ch)
	case benchProgressClosedMsg:
		return p, nil
	case benchRunTickMsg:
		if p.view != bvRunning {
			return p, nil
		}
		p = p.captureFeed()
		return p, p.runTick()
	case benchRunDoneMsg:
		return p.handleRunDone(m)
	case benchWebStartedMsg:
		p.webViewer = m.viewer
		p.webViewing = true
		p.webStarting = false
		var cmd tea.Cmd
		p, cmd = p.withFlash("benchmark viewer: " + m.url)
		return p, tea.Batch(cmd, waitForBenchWeb(m.viewer))
	case benchWebDoneMsg:
		p.webViewer = nil
		p.webViewing = false
		return p, nil
	case benchWebFailedMsg:
		p.webStarting = false
		return p.withFlashError("web viewer: " + m.err.Error())
	case spinner.TickMsg:
		// Only re-arm the tick while a run is in flight; outside bvRunning the
		// spinner is invisible and ticking would just burn CPU.
		if p.view != bvRunning {
			return p, nil
		}
		var cmd tea.Cmd
		p.spinner, cmd = p.spinner.Update(m)
		return p, cmd
	case tea.KeyMsg:
		return p.handleKey(m)
	}
	// Forward non-key messages to the active confirm so huh's async Cmd→Msg
	// cycles (focus init, StateCompleted transition) complete (DESTRUCT-01).
	if p.deleteConfirm.Active() {
		var cmd tea.Cmd
		p.deleteConfirm, cmd = p.deleteConfirm.Update(msg)
		return p, cmd
	}
	if p.cancelConfirm.Active() {
		var cmd tea.Cmd
		p.cancelConfirm, cmd = p.cancelConfirm.Update(msg)
		return p, cmd
	}
	return p, nil
}

func (p BenchmarkPage) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The confirm modals take priority over every view so esc/←/→/enter drive
	// the dialog instead of the page underneath it.
	if p.deleteConfirm.Active() {
		var cmd tea.Cmd
		p.deleteConfirm, cmd = p.deleteConfirm.Update(msg)
		return p, cmd
	}
	if p.cancelConfirm.Active() {
		var cmd tea.Cmd
		p.cancelConfirm, cmd = p.cancelConfirm.Update(msg)
		return p, cmd
	}
	switch p.view {
	case bvWizard:
		return p.keyWizard(msg)
	case bvRunning:
		return p.keyRunning(msg)
	case bvRunDetail:
		return p.keyDetail(msg)
	case bvProblem:
		return p.keyProblem(msg)
	case bvCompare:
		return p.keyCompare(msg)
	case bvHistory:
		return p.keyHistory(msg)
	default:
		return p.keyDashboard(msg)
	}
}

func (p BenchmarkPage) keyDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While the read-only web viewer holds focus the dashboard captures input:
	// esc closes the viewer, every other key is swallowed (the browser drives).
	if p.webViewing {
		if msg.String() == "esc" && p.webViewer != nil {
			p.webViewer.Cancel()
		}
		return p, nil
	}
	rows := dashboardRows(p.runs, p.focusedDashboardMode())
	switch msg.String() {
	case "up", "k":
		if p.dashCursor > 0 {
			p.dashCursor--
		}
	case "down", "j":
		if p.dashCursor < len(rows)-1 {
			p.dashCursor++
		}
	case "g", "home":
		p.dashCursor = 0
	case "G", "end":
		if len(rows) > 0 {
			p.dashCursor = len(rows) - 1
		}
	case "left", "[":
		p.focusMode = p.cycleFocusMode(-1)
		p.dashCursor = 0
	case "right", "]":
		p.focusMode = p.cycleFocusMode(1)
		p.dashCursor = 0
	case "b":
		return p.openWizard()
	case "enter":
		if r, ok := p.selectedDashboardRun(); ok {
			p = p.openDetail(r, bvDashboard)
		}
	case "c":
		return p.openCompare()
	case "h":
		return p.openHistory()
	case "X":
		return p.askDeleteSelected()
	case "E":
		return p.exportSelected()
	case "R":
		return p, p.loadRunsCmd()
	case "W":
		if p.webStarting || p.webViewing {
			return p, nil
		}
		p.webStarting = true
		return p, p.startBenchWeb()
	}
	return p, nil
}

// openDetail switches to the run-detail view for r, remembering the entry view
// (so esc returns there) and resetting the per-problem cursor, sort, and the
// lazily-loaded transcript cache.
func (p BenchmarkPage) openDetail(r benchmark.Run, from benchView) BenchmarkPage {
	run := r
	p.detail = &run
	p.detailFrom = from
	p.view = bvRunDetail
	p.probCursor = 0
	p.detailSort = 0
	p.probTranscript = nil
	p.probTranscriptTried = false
	return p
}

// detailReturn resolves the view esc goes back to from run detail.
func (p BenchmarkPage) detailReturn() benchView {
	switch p.detailFrom {
	case bvCompare, bvHistory:
		return p.detailFrom
	default:
		return bvDashboard
	}
}

// keyDetail drives the run-detail per-problem cursor, sort cycle, export, and
// the [enter] drill-in (loading the run transcript lazily on first entry).
func (p BenchmarkPage) keyDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if p.detail == nil {
		if msg.String() == "esc" {
			p.view = p.detailReturn()
		}
		return p, nil
	}
	n := len(p.detail.Problems)
	switch msg.String() {
	case "esc":
		p.view = p.detailReturn()
	case "E":
		return p.exportRunValue(*p.detail)
	case "s":
		p.detailSort = (p.detailSort + 1) % 3
		p.probCursor = 0
	case "up", "k":
		if p.probCursor > 0 {
			p.probCursor--
		}
	case "down", "j":
		if p.probCursor < n-1 {
			p.probCursor++
		}
	case "g", "home":
		p.probCursor = 0
	case "G", "end":
		if n > 0 {
			p.probCursor = n - 1
		}
	case "enter":
		if n > 0 {
			p.view = bvProblem
			p.probScroll = 0
			if !p.probTranscriptTried {
				tr, err := p.bstore.LoadTranscript(p.detail.ID)
				if err != nil {
					tr = nil
				}
				p.probTranscript = tr
				p.probTranscriptTried = true
			}
		}
	}
	return p, nil
}

// keyProblem scrolls the problem drill-in; esc returns to run detail.
func (p BenchmarkPage) keyProblem(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	avail := 1
	if p.height > 1 {
		avail = p.height - 1
	}
	maxOff := max(0, len(p.problemLines())-avail)
	switch msg.String() {
	case "esc":
		p.view = bvRunDetail
		p.probScroll = 0
	case "up", "k":
		if p.probScroll > 0 {
			p.probScroll--
		}
	case "down", "j":
		if p.probScroll < maxOff {
			p.probScroll++
		}
	case "g", "home":
		p.probScroll = 0
	case "G", "end":
		p.probScroll = maxOff
	}
	return p, nil
}

// keyProfileFilter handles keys while the wizard's profile-step filter input
// is active. handled is true when the key is fully consumed; it is false only
// for "enter", which signals keyWizardProfile to fall through to its non-filter
// switch so enter both closes the filter and selects the highlighted profile.
func (p BenchmarkPage) keyProfileFilter(msg tea.KeyMsg) (BenchmarkPage, tea.Cmd, bool) {
	switch msg.String() {
	case "esc", "/":
		p.filterMode = false
		return p, nil, true
	case "backspace":
		if len(p.filter) > 0 {
			p.filter = p.filter[:len(p.filter)-1]
		}
		return p, nil, true
	case "enter":
		p.filterMode = false
		return p, nil, false
	case " ":
		// Space arrives as tea.KeySpace (empty Runes), so the rune branch
		// below would drop it (TUI_AUDIT F-02).
		p.filter += " "
		if p.profCursor >= len(p.filteredProfiles()) {
			p.profCursor = 0
		}
		return p, nil, true
	default:
		// Append every rune in the message, not just single-rune events —
		// fast/bursted typing and paste arrive as one KeyMsg carrying
		// multiple runes (mirrors the INPUT-01 fix in models_messages.go).
		if len(msg.Runes) > 0 {
			p.filter += string(msg.Runes)
			if p.profCursor >= len(p.filteredProfiles()) {
				p.profCursor = 0
			}
		}
		return p, nil, true
	}
}

// askDeleteSelected arms the delete-run confirm for the highlighted run. The
// onYes callback emits benchmarkDeleteConfirmedMsg so the actual store delete
// runs in Update via performDelete (DESTRUCT-01).
func (p BenchmarkPage) askDeleteSelected() (tea.Model, tea.Cmd) {
	r, ok := p.selectedDashboardRun()
	if !ok {
		return p, nil
	}
	id := r.ID
	var cmd tea.Cmd
	p.deleteConfirm, cmd = setupConfirm("Delete benchmark run "+id+"?", "Delete", "Cancel",
		func() tea.Cmd { return func() tea.Msg { return benchmarkDeleteConfirmedMsg{id: id} } })
	return p, cmd
}

func (p BenchmarkPage) performDelete(id string) (tea.Model, tea.Cmd) {
	if err := p.bstore.Delete(id); err != nil {
		var cmd tea.Cmd
		p, cmd = p.withFlashError("delete: " + err.Error())
		return p, cmd
	}
	var fc tea.Cmd
	p, fc = p.withFlash("deleted run")
	return p, tea.Batch(fc, p.loadRunsCmd())
}

// exportSelected exports the run highlighted on the dashboard leaderboard.
func (p BenchmarkPage) exportSelected() (tea.Model, tea.Cmd) {
	r, ok := p.selectedDashboardRun()
	if !ok {
		return p, nil
	}
	return p.exportRunValue(r)
}

// exportRunValue writes a run to the exports dir and flashes the result.
func (p BenchmarkPage) exportRunValue(r benchmark.Run) (tea.Model, tea.Cmd) {
	jsonPath, _, err := benchmark.ExportRun(r, p.exportDir)
	if err != nil {
		var cmd tea.Cmd
		p, cmd = p.withFlashError("export: " + err.Error())
		return p, cmd
	}
	var fc tea.Cmd
	p, fc = p.withFlash("exported to " + filepath.Dir(jsonPath))
	return p, fc
}

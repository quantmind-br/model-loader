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
			if p.runCursor >= len(p.runs) {
				p.runCursor = 0
			}
			// Keep the dashboard cursor in range after a reload: a delete or
			// a focus-mode change can shrink the leaderboard under it.
			if rows := dashboardRows(p.runs, p.focusedDashboardMode()); p.dashCursor >= len(rows) {
				p.dashCursor = 0
			}
		}
		return p, nil
	case benchProgressMsg:
		p.progress = m.p
		return p, waitProgress(m.ch)
	case benchProgressClosedMsg:
		return p, nil
	case benchRunDoneMsg:
		return p.handleRunDone(m)
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
	case bvProfilePick:
		return p.keyProfilePick(msg)
	case bvModePick:
		return p.keyModePick(msg)
	case bvWizard:
		return p.keyWizard(msg)
	case bvRunning:
		return p.keyRunning(msg)
	case bvRunDetail, bvCompare, bvHistory:
		if msg.String() == "esc" {
			p.view = bvDashboard
			return p, nil
		}
		if msg.String() == "E" && p.view == bvRunDetail && p.detail != nil {
			return p.exportRunValue(*p.detail)
		}
		return p, nil
	default:
		return p.keyDashboard(msg)
	}
}

func (p BenchmarkPage) keyDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
			p.detail = &r
			p.view = bvRunDetail
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
	}
	return p, nil
}

func (p BenchmarkPage) openProfilePick() (tea.Model, tea.Cmd) {
	if p.runner == nil {
		p, _ = p.withFlashError("benchmark engine unavailable")
		return p, nil
	}
	profiles, err := p.store.List()
	if err != nil {
		p, _ = p.withFlashError("load profiles: " + err.Error())
		return p, nil
	}
	if len(profiles) == 0 {
		p, _ = p.withFlashError("no profiles — create one in the Profiles tab")
		return p, nil
	}
	p.profiles = profiles
	p.profCursor = 0
	p.filter = ""
	p.filterMode = false
	p.view = bvProfilePick
	return p, nil
}

func (p BenchmarkPage) keyProfilePick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if p.filterMode {
		np, cmd, handled := p.keyProfilePickFilter(msg)
		if handled {
			return np, cmd
		}
		// enter in filter mode falls through: it exits the filter (applied in
		// np) and then lets the non-filter switch select the highlighted row.
		p = np
	}
	switch msg.String() {
	case "esc":
		p.view = bvDashboard
	case "/":
		p.filterMode = true
	case "up", "k":
		if p.profCursor > 0 {
			p.profCursor--
		}
	case "down", "j":
		if p.profCursor < len(p.filteredProfiles())-1 {
			p.profCursor++
		}
	// g/G only reach here outside filter mode: while filtering, the printable
	// branch above appends them to the filter text instead.
	case "g", "home":
		p.profCursor = 0
	case "G", "end":
		if n := len(p.filteredProfiles()); n > 0 {
			p.profCursor = n - 1
		}
	case "enter":
		filtered := p.filteredProfiles()
		if p.profCursor < len(filtered) {
			p.selectedProfileID = filtered[p.profCursor].ID
			p.runningName = filtered[p.profCursor].Name
			p.modeCursor = 0
			p.view = bvModePick
		}
	}
	return p, nil
}

// keyProfilePickFilter handles keys while the profile picker's filter input is
// active. handled is true when the key is fully consumed; it is false only for
// "enter", which signals keyProfilePick to fall through to its non-filter
// switch so enter both closes the filter and selects the highlighted profile.
func (p BenchmarkPage) keyProfilePickFilter(msg tea.KeyMsg) (BenchmarkPage, tea.Cmd, bool) {
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

func (p BenchmarkPage) keyModePick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.view = bvProfilePick
	case "up", "k":
		if p.modeCursor > 0 {
			p.modeCursor--
		}
	case "down", "j":
		if p.modeCursor < len(benchModes)-1 {
			p.modeCursor++
		}
	case "enter":
		return p.startRun()
	}
	return p, nil
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
		p, _ = p.withFlashError("delete: " + err.Error())
		return p, nil
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
	jsonPath, _, err := exportRun(p.exportDir, r)
	if err != nil {
		p, _ = p.withFlashError("export: " + err.Error())
		return p, nil
	}
	var fc tea.Cmd
	p, fc = p.withFlash("exported to " + filepath.Dir(jsonPath))
	return p, fc
}

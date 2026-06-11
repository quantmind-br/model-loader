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
	case benchRunsLoadedMsg:
		if m.err == nil {
			p.runs = m.runs
			if p.runCursor >= len(p.runs) {
				p.runCursor = 0
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
	// Forward non-key messages to the confirm so huh's async Cmd→Msg cycles
	// (focus init, StateCompleted transition) complete (DESTRUCT-01).
	if p.deleteConfirm.Active() {
		var cmd tea.Cmd
		p.deleteConfirm, cmd = p.deleteConfirm.Update(msg)
		return p, cmd
	}
	return p, nil
}

func (p BenchmarkPage) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The confirm modal takes priority over every view so esc/←/→/enter drive
	// the dialog instead of the run list underneath it.
	if p.deleteConfirm.Active() {
		var cmd tea.Cmd
		p.deleteConfirm, cmd = p.deleteConfirm.Update(msg)
		return p, cmd
	}
	switch p.view {
	case bvProfilePick:
		return p.keyProfilePick(msg)
	case bvModePick:
		return p.keyModePick(msg)
	case bvRunning:
		if msg.String() == "esc" {
			if p.runCancel != nil {
				p.runCancel()
			}
		}
		return p, nil
	case bvRunDetail, bvCompare, bvHistory:
		if msg.String() == "esc" {
			p.view = bvList
			return p, nil
		}
		if msg.String() == "E" && p.view == bvRunDetail && p.detail != nil {
			return p.exportRunValue(*p.detail)
		}
		return p, nil
	default:
		return p.keyList(msg)
	}
}

func (p BenchmarkPage) keyList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if p.runCursor > 0 {
			p.runCursor--
		}
	case "down", "j":
		if p.runCursor < len(p.runs)-1 {
			p.runCursor++
		}
	case "g", "home":
		p.runCursor = 0
	case "G", "end":
		if len(p.runs) > 0 {
			p.runCursor = len(p.runs) - 1
		}
	case "b":
		return p.openProfilePick()
	case "enter":
		if p.runCursor < len(p.runs) {
			r := p.runs[p.runCursor]
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
		p.flash, _ = flashError(p.flash, "benchmark engine unavailable")
		return p, nil
	}
	profiles, err := p.store.List()
	if err != nil {
		p.flash, _ = flashError(p.flash, "load profiles: "+err.Error())
		return p, nil
	}
	if len(profiles) == 0 {
		p.flash, _ = flashError(p.flash, "no profiles — create one in the Profiles tab")
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
		switch msg.String() {
		case "esc", "/":
			p.filterMode = false
			return p, nil
		case "backspace":
			if len(p.filter) > 0 {
				p.filter = p.filter[:len(p.filter)-1]
			}
			return p, nil
		case "enter":
			p.filterMode = false
		default:
			if len(msg.Runes) == 1 {
				p.filter += string(msg.Runes)
				if p.profCursor >= len(p.filteredProfiles()) {
					p.profCursor = 0
				}
				return p, nil
			}
			return p, nil
		}
	}
	switch msg.String() {
	case "esc":
		p.view = bvList
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
	if p.runCursor >= len(p.runs) {
		return p, nil
	}
	id := p.runs[p.runCursor].ID
	p.deleteConfirm = components.NewConfirm(
		"Delete benchmark run "+id+"?",
		id,
		func(payload any) tea.Cmd {
			rid, _ := payload.(string)
			return func() tea.Msg { return benchmarkDeleteConfirmedMsg{id: rid} }
		},
		"Delete",
		"Cancel",
	)
	return p, p.deleteConfirm.Init()
}

func (p BenchmarkPage) performDelete(id string) (tea.Model, tea.Cmd) {
	if err := p.bstore.Delete(id); err != nil {
		p.flash, _ = flashError(p.flash, "delete: "+err.Error())
		return p, nil
	}
	var fc tea.Cmd
	p.flash, fc = flashSuccess(p.flash, "deleted run")
	return p, tea.Batch(fc, p.loadRunsCmd())
}

// exportSelected exports the run highlighted in the list.
func (p BenchmarkPage) exportSelected() (tea.Model, tea.Cmd) {
	if p.runCursor >= len(p.runs) {
		return p, nil
	}
	return p.exportRunValue(p.runs[p.runCursor])
}

// exportRunValue writes a run to the exports dir and flashes the result.
func (p BenchmarkPage) exportRunValue(r benchmark.Run) (tea.Model, tea.Cmd) {
	jsonPath, _, err := exportRun(p.exportDir, r)
	if err != nil {
		p.flash, _ = flashError(p.flash, "export: "+err.Error())
		return p, nil
	}
	var fc tea.Cmd
	p.flash, fc = flashSuccess(p.flash, "exported to "+filepath.Dir(jsonPath))
	return p, fc
}

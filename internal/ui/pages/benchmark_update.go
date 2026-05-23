package pages

import (
	"path/filepath"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

func (p BenchmarkPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = m.Width, m.Height
		return p, nil
	case components.FlashClearMsg:
		p.flash, _ = p.flash.Update(m)
		return p, nil
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
		var cmd tea.Cmd
		p.spinner, cmd = p.spinner.Update(m)
		if p.view == bvRunning {
			return p, cmd
		}
		return p, cmd
	case tea.KeyMsg:
		return p.handleKey(m)
	}
	return p, nil
}

func (p BenchmarkPage) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		if msg.String() == "e" && p.view == bvRunDetail && p.detail != nil {
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
	case "x":
		return p.deleteSelected()
	case "e":
		return p.exportSelected()
	case "r":
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

func (p BenchmarkPage) deleteSelected() (tea.Model, tea.Cmd) {
	if p.runCursor >= len(p.runs) {
		return p, nil
	}
	id := p.runs[p.runCursor].ID
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

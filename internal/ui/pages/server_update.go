package pages

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/ui/components"
)

func (p *ServerPage) refreshInstancesCmd() tea.Cmd {
	pm := p.pm
	return func() tea.Msg {
		// Instances are launched by the detached proxy process, so this TUI's
		// in-memory tracking goes stale. Re-read instances.json (read-only)
		// so externally launched instances appear. Never Reconcile here: a
		// TUI-side registry write can race the proxy's writes and erase a
		// freshly-launched instance (cross-process last-writer-wins).
		_ = pm.RefreshFromDisk()
		return monitorInstancesRefreshedMsg{insts: pm.List()}
	}
}

// Reload implements the ui.Reloader contract so RootModel re-polls the
// process manager when the user switches to the Monitor tab. Without
// this, instances spawned/killed externally only surface on the next
// 2s periodic tick.
func (p *ServerPage) Reload() tea.Cmd {
	return p.refreshInstancesCmd()
}

// Update is a thin dispatcher: each typed-message arm delegates to a
// private handle<MsgType> method. Most handlers run the shared tail
// (forwardToConfirms) themselves so non-key messages still reach active
// huh forms (Init handshake, async validation) and so the table sees
// every key for navigation.
func (p *ServerPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		if p.proxy != nil {
			cmd, consumed := p.proxy.Update(m)
			if consumed {
				return p, cmd
			}
		}
		return p.handleKey(m)
	case tea.WindowSizeMsg:
		p.SetSize(m.Width, m.Height)
		var cmds []tea.Cmd
		if p.proxy != nil {
			pc, _ := p.proxy.Update(m)
			cmds = append(cmds, pc)
		}
		cmds = append(cmds, p.forwardToConfirms(m))
		return p, tea.Batch(cmds...)
	case monitorInstancesRefreshedMsg:
		return p.handleInstancesRefreshed(m)
	case restartResultMsg:
		return p.handleRestartResult(m)
	case unloadResultMsg:
		return p.handleUnloadResult(m)
	case ServerSelectPIDMsg:
		return p.handleSelectPID(m)
	case monitorPeriodicTickMsg:
		return p.handlePeriodicTick()
	case monitorKillConfirmedMsg:
		return p.handleKillConfirmed(m)
	case monitorRestartConfirmedMsg:
		return p.handleRestartConfirmed(m)
	case monitorEventMsg:
		return p.handleMonitorEvent(m)
	case components.FlashClearMsg:
		p.flash, _ = p.flash.Update(m)
		var pc tea.Cmd
		if p.proxy != nil {
			pc, _ = p.proxy.Update(m)
		}
		if pc != nil {
			return p, pc
		}
		return p, nil
	}
	// Messages the page doesn't explicitly handle (e.g. ProxyTickMsg,
	// ProxyActionResultMsg) are forwarded to the proxy panel first.
	var pc tea.Cmd
	if p.proxy != nil {
		pc, _ = p.proxy.Update(msg)
	}
	if pc != nil {
		return p, pc
	}
	return p, p.forwardToConfirms(msg)
}

// handleKey routes key input. Confirm overlays consume the key and short-
// circuit the table update so the form keeps focus. Destructive actions use
// uppercase (K kill, R restart) so the lowercase vim keys k/j fall through to
// the table for line navigation (KEY-01, CONSIST-02); history is a read-only
// view, so it uses lowercase h to match the Benchmark tab (CONSIST-01).
func (p *ServerPage) handleKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	if p.killConfirm.Active() {
		return p, p.handleConfirmKillKey(m)
	}
	if p.restartConfirm.Active() {
		return p, p.handleConfirmRestartKey(m)
	}
	switch {
	case m.Type == tea.KeyRunes && len(m.Runes) == 1 && m.Runes[0] == 'v':
		p.subView = (p.subView + 1) % 4
	case m.Type == tea.KeyRunes && len(m.Runes) == 1 && m.Runes[0] == 'K':
		if pid := p.selectedPID(); pid > 0 {
			return p, p.askConfirmKill(pid)
		}
	case m.Type == tea.KeyRunes && len(m.Runes) == 1 && m.Runes[0] == 'R':
		if pid := p.selectedPID(); pid > 0 {
			return p, p.askConfirmRestart(pid)
		}
	case m.Type == tea.KeyRunes && len(m.Runes) == 1 && m.Runes[0] == 'h':
		return p.openHistoryChart()
	case m.Type == tea.KeyRunes && len(m.Runes) == 1 && (m.Runes[0] == '1' || m.Runes[0] == '2' || m.Runes[0] == '3' || m.Runes[0] == '4'):
		if p.historyChart != nil {
			return p.switchHistoryWindow(m.Runes[0])
		}
	case m.String() == "esc":
		if p.historyChart != nil {
			p.historyChart = nil
			return p, nil
		}
	case m.Type == tea.KeySpace:
		p.paused = !p.paused
	}
	return p, p.forwardToConfirms(m)
}

func (p *ServerPage) withFlashError(msg string) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	p.flash, cmd = flashError(p.flash, msg)
	return p, cmd
}

// forwardToConfirms forwards msg to active confirm forms (so huh's Init /
// validation Cmds land) and to the underlying table (so navigation keys
// reach it). Returned by every handler that does NOT short-circuit.
func (p *ServerPage) forwardToConfirms(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	if p.killConfirm.Active() {
		var cmd tea.Cmd
		p.killConfirm, cmd = p.killConfirm.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if p.restartConfirm.Active() {
		var cmd tea.Cmd
		p.restartConfirm, cmd = p.restartConfirm.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	t, tc := p.tbl.Update(msg)
	p.tbl = t
	if tc != nil {
		cmds = append(cmds, tc)
	}
	return tea.Batch(cmds...)
}

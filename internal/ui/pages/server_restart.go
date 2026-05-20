package pages

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

// monitorKillConfirmedMsg is emitted by killConfirm.onYes when the user
// confirms a kill. The page handles it in Update so manager.Kill + refresh
// stay on the UI thread.
type monitorKillConfirmedMsg struct{ pid int }

// monitorRestartConfirmedMsg is emitted by restartConfirm.onYes when the user
// confirms a restart. Carries the captured profile + foreground/background flag
// so the async kill+launch dispatch has everything it needs without re-reading
// stale page state.
type monitorRestartConfirmedMsg struct {
	pid        int
	profile    domain.Profile
	background bool
}

// restartResultMsg carries the outcome of an async kill+launch restart.
type restartResultMsg struct {
	pid int
	err error
}

func (p *ServerPage) handleRestartResult(m restartResultMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.err != nil {
		p.flash, cmd = p.flash.SetError(fmt.Sprintf("restart: pid %d failed: %v", m.pid, m.err))
	}
	return p, tea.Batch(p.refreshInstancesCmd(), p.forwardToConfirms(m), cmd)
}

func (p *ServerPage) handleKillConfirmed(m monitorKillConfirmedMsg) (tea.Model, tea.Cmd) {
	_ = p.pm.Kill(m.pid)
	p.dropInstanceRow(m.pid) // optimistic removal; refresh below reconciles (UX-02)
	return p, tea.Batch(p.refreshInstancesCmd(), p.forwardToConfirms(m))
}

func (p *ServerPage) handleRestartConfirmed(m monitorRestartConfirmedMsg) (tea.Model, tea.Cmd) {
	prof := m.profile
	if p.resolver != nil {
		rb, err := p.resolver.Resolve(prof)
		if err == nil {
			prof.Launch.ResolvedExecutable = rb.ExecutablePath
			prof.Launch.ResolvedBackendKind = rb.Backend.Kind
		}
	}
	return p, tea.Batch(restartCmd(p.pm, m.pid, prof, m.background), p.forwardToConfirms(m))
}

// askConfirmKill builds and arms the kill-confirmation overlay. The Confirm's
// onYes emits monitorKillConfirmedMsg; the actual Kill happens in Update so
// manager I/O stays on the page.
func (p *ServerPage) askConfirmKill(pid int) tea.Cmd {
	p.killConfirm = components.NewConfirm(
		fmt.Sprintf("Kill pid=%d?", pid),
		pid,
		func(payload any) tea.Cmd {
			id, _ := payload.(int)
			return func() tea.Msg { return monitorKillConfirmedMsg{pid: id} }
		},
		"Kill",
		"Cancel",
	)
	return p.killConfirm.Init()
}

func (p *ServerPage) handleConfirmKillKey(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	p.killConfirm, cmd = p.killConfirm.Update(msg)
	return cmd
}

// restartPayload bundles the data captured at the moment the user opens the
// restart confirm. It is the Confirm.payload so onYes can emit a
// monitorRestartConfirmedMsg with everything needed for the async kill+launch
// dispatch — no need to re-read p.pm.List() at completion time, which would
// race with the periodic refresh.
type restartPayload struct {
	pid        int
	profile    domain.Profile
	background bool
}

// askConfirmRestart preloads the profile for the selected PID and arms a
// confirmation form. If the instance or profile is missing, it surfaces the
// error via p.flash instead of opening the form.
func (p *ServerPage) askConfirmRestart(pid int) tea.Cmd {
	insts := p.pm.List()
	var inst *domain.RunningInstance
	for i := range insts {
		if insts[i].PID == pid {
			inst = &insts[i]
			break
		}
	}
	var cmd tea.Cmd
	if inst == nil {
		p.flash, cmd = p.flash.SetError(fmt.Sprintf("restart: pid %d not found", pid))
		return cmd
	}
	if p.ps == nil {
		p.flash, cmd = p.flash.SetError("restart: profile store not available")
		return cmd
	}
	prof, err := p.ps.Get(inst.ProfileID)
	if err != nil {
		p.flash, cmd = p.flash.SetError(fmt.Sprintf("restart: profile %q not found", inst.ProfileID))
		return cmd
	}
	payload := restartPayload{pid: pid, profile: prof, background: inst.Background}
	p.restartConfirm = components.NewConfirm(
		fmt.Sprintf("Restart pid=%d (%s)?", pid, prof.Name),
		payload,
		func(arg any) tea.Cmd {
			rp, _ := arg.(restartPayload)
			return func() tea.Msg {
				return monitorRestartConfirmedMsg{pid: rp.pid, profile: rp.profile, background: rp.background}
			}
		},
		"Restart",
		"Cancel",
	)
	return p.restartConfirm.Init()
}

func (p *ServerPage) handleConfirmRestartKey(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	p.restartConfirm, cmd = p.restartConfirm.Update(msg)
	return cmd
}

// restartCmd performs Kill then Launch off the UI thread and delivers the
// result wrapped in a restartResultMsg.
func restartCmd(pm procMgrIface, pid int, prof domain.Profile, bg bool) tea.Cmd {
	return func() tea.Msg {
		if err := pm.Kill(pid); err != nil {
			return restartResultMsg{pid: pid, err: fmt.Errorf("kill: %w", err)}
		}
		mode := processmgr.LaunchBackground
		if !bg {
			mode = processmgr.LaunchForeground
		}
		if _, err := pm.Launch(prof, mode, log.NewAttemptID()); err != nil {
			return restartResultMsg{pid: pid, err: fmt.Errorf("launch: %w", err)}
		}
		return restartResultMsg{pid: pid}
	}
}

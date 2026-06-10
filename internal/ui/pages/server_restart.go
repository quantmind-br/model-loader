package pages

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

// monitorKillConfirmedMsg is emitted by killConfirm.onYes when the user
// confirms a kill. The page handles it in Update so the kill/unload dispatch
// stays on the UI thread.
type monitorKillConfirmedMsg struct{ pid int }

// monitorRestartConfirmedMsg is emitted by restartConfirm.onYes when the user
// confirms a restart. Carries the captured profile so the async unload+load
// dispatch has everything it needs without re-reading stale page state.
type monitorRestartConfirmedMsg struct {
	pid     int
	profile domain.Profile
}

// restartResultMsg carries the outcome of an async unload+load restart.
type restartResultMsg struct {
	pid int
	err error
}

// unloadResultMsg carries the outcome of an async forced unload triggered by
// killing the proxy-loaded instance.
type unloadResultMsg struct {
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

func (p *ServerPage) handleUnloadResult(m unloadResultMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.err != nil {
		p.flash, cmd = p.flash.SetError(fmt.Sprintf("unload: pid %d failed: %v", m.pid, m.err))
	}
	return p, tea.Batch(p.refreshInstancesCmd(), p.forwardToConfirms(m), cmd)
}

func (p *ServerPage) handleKillConfirmed(m monitorKillConfirmedMsg) (tea.Model, tea.Cmd) {
	p.dropInstanceRow(m.pid) // optimistic removal; refresh below reconciles (UX-02)
	if p.proxyCtl != nil {
		if st := p.proxyCtl.Status(); st.LoadedPID != 0 && st.LoadedPID == m.pid {
			// The selected instance is the proxy's loaded backend — kill it
			// through /_admin/unload (force) so the proxy state stays
			// consistent instead of observing a vanished child.
			pc := p.proxyCtl
			pid := m.pid
			unloadCmd := func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				_, err := pc.Unload(ctx, true)
				return unloadResultMsg{pid: pid, err: err}
			}
			return p, tea.Batch(unloadCmd, p.forwardToConfirms(m))
		}
	}
	// Orphan process management: instances not owned by the proxy.
	_ = p.pm.Kill(m.pid)
	return p, tea.Batch(p.refreshInstancesCmd(), p.forwardToConfirms(m))
}

func (p *ServerPage) handleRestartConfirmed(m monitorRestartConfirmedMsg) (tea.Model, tea.Cmd) {
	if p.proxyCtl == nil {
		var cmd tea.Cmd
		p.flash, cmd = p.flash.SetError("restart: HTTP proxy not available")
		return p, tea.Batch(cmd, p.forwardToConfirms(m))
	}
	return p, tea.Batch(restartCmd(p.proxyCtl, m.pid, m.profile.ID), p.forwardToConfirms(m))
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
// monitorRestartConfirmedMsg with everything needed for the async
// unload+load dispatch — no need to re-read p.pm.List() at completion time,
// which would race with the periodic refresh.
type restartPayload struct {
	pid     int
	profile domain.Profile
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
	payload := restartPayload{pid: pid, profile: prof}
	p.restartConfirm = components.NewConfirm(
		fmt.Sprintf("Restart pid=%d (%s)?", pid, prof.Name),
		payload,
		func(arg any) tea.Cmd {
			rp, _ := arg.(restartPayload)
			return func() tea.Msg {
				return monitorRestartConfirmedMsg{pid: rp.pid, profile: rp.profile}
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

// restartCmd performs Unload then Load through the proxy off the UI thread
// and delivers the result wrapped in a restartResultMsg. Load blocks until
// the new backend is healthy. pid is only used to label the result.
func restartCmd(pc serverProxyController, pid int, profileID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if _, err := pc.Unload(ctx, false); err != nil {
			return restartResultMsg{pid: pid, err: fmt.Errorf("unload: %w", err)}
		}
		if _, err := pc.Load(ctx, profileID); err != nil {
			return restartResultMsg{pid: pid, err: fmt.Errorf("load: %w", err)}
		}
		return restartResultMsg{pid: pid}
	}
}

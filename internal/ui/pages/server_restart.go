package pages

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/proxysupervisor"
)

// monitorKillConfirmedMsg is emitted by killConfirm.onYes when the user
// confirms a kill. The page handles it in Update so the kill/unload dispatch
// stays on the UI thread.
type monitorKillConfirmedMsg struct{ pid int }

// monitorForceKillConfirmedMsg is emitted by forceKillConfirm.onYes when the
// user accepts force-stopping a degraded proxy so a stranded backend can be
// killed directly (audit A13).
type monitorForceKillConfirmedMsg struct{ pid int }

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

// killResultMsg carries the outcome of an async orphan kill (a pid not owned
// by the proxy, dispatched through pm.Kill off the UI thread).
type killResultMsg struct {
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

func (p *ServerPage) handleKillResult(m killResultMsg) (tea.Model, tea.Cmd) {
	// A degraded-proxy refusal is recoverable: offer the ForceStop escape
	// hatch instead of only flashing (audit A13).
	if m.err != nil && errors.Is(m.err, proxysupervisor.ErrProxyDegraded) {
		return p, tea.Batch(p.refreshInstancesCmd(), p.askConfirmForceKill(m.pid))
	}
	var cmd tea.Cmd
	if m.err != nil {
		p.flash, cmd = p.flash.SetError(fmt.Sprintf("kill: pid %d: %v", m.pid, m.err))
	}
	return p, tea.Batch(p.refreshInstancesCmd(), p.forwardToConfirms(m), cmd)
}

// askConfirmForceKill arms the force-stop confirmation. onYes emits
// monitorForceKillConfirmedMsg; the ForceStop + Kill happen off the UI thread.
func (p *ServerPage) askConfirmForceKill(pid int) tea.Cmd {
	var cmd tea.Cmd
	p.forceKillConfirm, cmd = setupConfirm(
		fmt.Sprintf("Proxy degraded — force-stop proxy and kill pid %d?", pid),
		"Force", "Cancel",
		func() tea.Cmd { return func() tea.Msg { return monitorForceKillConfirmedMsg{pid: pid} } })
	return cmd
}

func (p *ServerPage) handleConfirmForceKillKey(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	p.forceKillConfirm, cmd = p.forceKillConfirm.Update(msg)
	return cmd
}

func (p *ServerPage) handleForceKillConfirmed(m monitorForceKillConfirmedMsg) (tea.Model, tea.Cmd) {
	p.dropInstanceRow(m.pid)
	pc := p.proxyCtl
	pm := p.pm
	pid := m.pid
	forceCmd := func() tea.Msg {
		if pc != nil {
			if err := pc.ForceStop(); err != nil {
				return killResultMsg{pid: pid, err: fmt.Errorf("force-stop proxy: %w", err)}
			}
		}
		if err := pm.Kill(pid); err != nil && !errors.Is(err, processmgr.ErrUnknownPID) {
			return killResultMsg{pid: pid, err: err}
		}
		return killResultMsg{pid: pid}
	}
	return p, tea.Batch(forceCmd, p.forwardToConfirms(m))
}

func (p *ServerPage) handleKillConfirmed(m monitorKillConfirmedMsg) (tea.Model, tea.Cmd) {
	p.dropInstanceRow(m.pid) // optimistic removal; refresh below reconciles (UX-02)
	if p.proxyCtl == nil {
		// No proxy wired: pure orphan process management.
		_ = p.pm.Kill(m.pid)
		return p, tea.Batch(p.refreshInstancesCmd(), p.forwardToConfirms(m))
	}
	// The ownership decision needs proxy Status() (up to ~1s of I/O), so the
	// whole dispatch runs inside a tea.Cmd off the UI thread.
	pc := p.proxyCtl
	pm := p.pm
	pid := m.pid
	killCmd := func() tea.Msg {
		st := pc.Status()
		if st.LoadedPID != 0 && st.LoadedPID == pid {
			// The selected instance is the proxy's loaded backend — kill it
			// through /_admin/unload (force) so the proxy state stays
			// consistent instead of observing a vanished child.
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, err := pc.Unload(ctx, true)
			return unloadResultMsg{pid: pid, err: err}
		}
		// Orphan process management: instances not owned by the proxy. A
		// degraded proxy (status probe failing) hides its loaded pid, so it
		// must be guarded against before killing directly (parity with the
		// CLI's refuseKillOnDegradedProxy).
		if err := refuseKillOnDegradedProxyStatus(pc, st, pid); err != nil {
			return killResultMsg{pid: pid, err: err}
		}
		if err := pm.Kill(pid); err != nil && !errors.Is(err, processmgr.ErrUnknownPID) {
			return killResultMsg{pid: pid, err: err}
		}
		return killResultMsg{pid: pid}
	}
	return p, tea.Batch(killCmd, p.forwardToConfirms(m))
}

func (p *ServerPage) handleRestartConfirmed(m monitorRestartConfirmedMsg) (tea.Model, tea.Cmd) {
	if p.proxyCtl == nil {
		var cmd tea.Cmd
		p.flash, cmd = p.flash.SetError("restart: HTTP proxy not available")
		return p, tea.Batch(cmd, p.forwardToConfirms(m))
	}
	return p, tea.Batch(restartCmd(p.proxyCtl, p.pm, m.pid, m.profile.ID), p.forwardToConfirms(m))
}

// askConfirmKill builds and arms the kill-confirmation overlay. The Confirm's
// onYes emits monitorKillConfirmedMsg; the actual Kill happens in Update so
// manager I/O stays on the page.
func (p *ServerPage) askConfirmKill(pid int) tea.Cmd {
	var cmd tea.Cmd
	p.killConfirm, cmd = setupConfirm(fmt.Sprintf("Kill pid=%d?", pid), "Kill", "Cancel",
		func() tea.Cmd { return func() tea.Msg { return monitorKillConfirmedMsg{pid: pid} } })
	return cmd
}

func (p *ServerPage) handleConfirmKillKey(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	p.killConfirm, cmd = p.killConfirm.Update(msg)
	return cmd
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
	p.restartConfirm, cmd = setupConfirm(fmt.Sprintf("Restart pid=%d (%s)?", pid, prof.Name), "Restart", "Cancel",
		func() tea.Cmd {
			return func() tea.Msg {
				return monitorRestartConfirmedMsg{pid: pid, profile: prof}
			}
		})
	return cmd
}

func (p *ServerPage) handleConfirmRestartKey(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	p.restartConfirm, cmd = p.restartConfirm.Update(msg)
	return cmd
}

// restartCmd restarts the instance off the UI thread and delivers the result
// wrapped in a restartResultMsg. Ownership is checked at execution time
// (parity with the CLI's restartInstance): a proxy-owned pid is swapped via
// Unload+Load; an orphan pid is pm.Kill'ed first (freeing its VRAM) and then
// loaded via the proxy — never Unload, which would kill whatever OTHER model
// the proxy currently serves. Load blocks until the new backend is healthy.
func restartCmd(pc serverProxyController, pm procMgrIface, pid int, profileID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		st := pc.Status()
		if st.LoadedPID != 0 && st.LoadedPID == pid {
			if _, err := pc.Unload(ctx, false); err != nil {
				return restartResultMsg{pid: pid, err: fmt.Errorf("unload: %w", err)}
			}
		} else {
			if err := refuseKillOnDegradedProxyStatus(pc, st, pid); err != nil {
				return restartResultMsg{pid: pid, err: err}
			}
			// Kill the orphan OS process before loading so VRAM is freed first.
			if err := pm.Kill(pid); err != nil && !errors.Is(err, processmgr.ErrUnknownPID) {
				return restartResultMsg{pid: pid, err: fmt.Errorf("kill orphan: %w", err)}
			}
		}
		if _, err := pc.Load(ctx, profileID); err != nil {
			return restartResultMsg{pid: pid, err: fmt.Errorf("load: %w", err)}
		}
		return restartResultMsg{pid: pid}
	}
}

// refuseKillOnDegradedProxyStatus guards the orphan-kill paths: never kill a
// pid directly while the proxy might be routing to it. A running proxy whose
// /_status probe failed reports empty loaded fields exactly like a healthy
// proxy with nothing loaded, so we key off the explicit probe-failure marker
// set by proxysupervisor.Status. The probe has a short timeout and can fail
// transiently, so we re-fetch once before refusing. Mirrors the CLI's
// refuseKillOnDegradedProxy (internal/cli/instance_lifecycle.go).
func refuseKillOnDegradedProxyStatus(pc serverProxyController, st httpproxy.Status, pid int) error {
	if !st.Running || !strings.HasPrefix(st.LastError, "status_probe_failed") {
		return nil
	}
	st = pc.Status()
	if st.Running && strings.HasPrefix(st.LastError, "status_probe_failed") {
		return fmt.Errorf("%w — retry, or force-stop the proxy first (refusing to kill pid %d directly)", proxysupervisor.ErrProxyDegraded, pid)
	}
	if st.LoadedPID == pid {
		// The refreshed status reveals the pid is the proxy-loaded backend.
		return fmt.Errorf("pid %d is loaded behind the proxy — retry so it goes through the proxy", pid)
	}
	return nil
}

package pages

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

// launchSelected starts the currently selected profile directly.
func (p ProfilesPage) launchSelected() (tea.Model, tea.Cmd) {
	if p.launch.waitPID != 0 {
		return p, nil
	}
	if _, isCorrupt := p.list.SelectedItem().(corruptItem); isCorrupt {
		p, fc := p.withFlashError("corrupt entry — fix the JSON file or delete it")
		return p, fc
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok || p.manager == nil {
		return p, nil
	}
	return p, p.launchProfileCmd(sel.p)
}

func (p ProfilesPage) handleLaunched(msg launchedMsg) (tea.Model, tea.Cmd) {
	p.running = append(p.running, msg.inst)
	p.launch.waitPID = msg.inst.PID
	mode := "foreground"
	if msg.inst.Background {
		mode = "background"
	}
	p.launch.status = fmt.Sprintf("launched %s pid=%d port=%d (%s) — waiting for /health…",
		msg.inst.ProfileID, msg.inst.PID, msg.inst.Port, mode)
	p.launch.statusAt = time.Time{}
	// F-02 audit: also emit a flash so the user gets a high-contrast
	// success message rather than a single grey "waiting for /health…"
	// line that's easy to miss while they're scanning the screen for
	// a confirmation.
	p, startedFlash := p.withFlash(fmt.Sprintf("launched pid=%d port=%d (%s) — waiting for /health…",
		msg.inst.PID, msg.inst.Port, mode))
	mgr := p.manager
	port := msg.inst.Port
	pid := msg.inst.PID
	attemptID := msg.attemptID
	waitCmd := func() tea.Msg {
		if err := mgr.WaitHealthy(pid, port, 30*time.Second, attemptID); err != nil {
			return launchErrMsg{err: fmt.Errorf("pid %d not healthy: %w", pid, err)}
		}
		return healthyMsg{pid: pid}
	}
	return p, tea.Batch(p.launch.spinner.Tick, waitCmd, startedFlash)
}

func (p ProfilesPage) handleHealthy(msg healthyMsg) (tea.Model, tea.Cmd) {
	p.launch.waitPID = 0
	// Look up the instance so we can include the port in the confirmation
	// flash — a bare "healthy pid=N" gives the user nothing to act on
	// (F-02 audit).
	port := 0
	for _, ri := range p.running {
		if ri.PID == msg.pid {
			port = ri.Port
			break
		}
	}
	var text string
	if port > 0 {
		text = fmt.Sprintf("healthy pid=%d port=%d — switched to Server tab", msg.pid, port)
	} else {
		text = fmt.Sprintf("healthy pid=%d — switched to Server tab", msg.pid)
	}
	p.launch.status = ""
	p, fc := p.withFlash(text)
	pid := msg.pid
	return p, tea.Batch(fc, func() tea.Msg { return SwitchToServerMsg{PID: pid} })
}

func (p ProfilesPage) handleLaunchErr(msg launchErrMsg) (tea.Model, tea.Cmd) {
	pid := p.launch.waitPID
	p.launch.waitPID = 0
	p.launch.status = ""
	base := friendlyLaunchError(msg.err)
	if msg.firstIssue != "" {
		base += " \u2014 " + msg.firstIssue
	}
	if pid != 0 && p.manager != nil {
		if exit, ok := p.manager.GetExitInfo(pid); ok {
			base = enrichWithExit(base, exit)
		}
	}
	// F-01 audit: failures must use the high-contrast error styling and
	// the longer FlashLifetimeError so the user actually notices that
	// the launch was rejected rather than mistaking it for a no-op.
	p, fc := p.withFlashError(base)
	return p, fc
}

func (p ProfilesPage) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if p.launch.waitPID == 0 {
		return p, nil
	}
	updated, cmd := p.launch.spinner.Update(msg)
	p.launch.spinner = updated
	return p, cmd
}

func (p ProfilesPage) handleLaunchProfile(msg LaunchProfileMsg) (tea.Model, tea.Cmd) {
	if p.manager == nil {
		p, fc := p.withFlashError("launch failed: process manager unavailable")
		return p, fc
	}
	selected, err := p.store.Get(msg.ID)
	if err != nil {
		p, fc := p.withFlashError("launch failed: " + err.Error())
		return p, fc
	}
	return p, p.launchProfileCmd(selected)
}

func (p ProfilesPage) handleKillConfirmed(msg profilesKillConfirmedMsg) (tea.Model, tea.Cmd) {
	var fc tea.Cmd
	p, fc = p.performKill(msg.pid)
	return p, fc
}

func (p ProfilesPage) askKillMostRecent() (tea.Model, tea.Cmd) {
	if len(p.running) == 0 || p.manager == nil {
		return p, nil
	}
	pid := p.running[len(p.running)-1].PID
	p.killConfirm = components.NewConfirm(
		fmt.Sprintf("Kill pid=%d?", pid),
		pid,
		func(payload any) tea.Cmd {
			id, _ := payload.(int)
			return func() tea.Msg { return profilesKillConfirmedMsg{pid: id} }
		},
		"Kill",
		"Cancel",
	)
	return p, p.killConfirm.Init()
}

func (p ProfilesPage) performKill(pid int) (ProfilesPage, tea.Cmd) {
	if err := p.manager.Kill(pid); err != nil {
		return p.withFlashError("error: " + err.Error())
	}
	out := p.running[:0]
	for _, ri := range p.running {
		if ri.PID != pid {
			out = append(out, ri)
		}
	}
	p.running = out
	return p.withFlash(fmt.Sprintf("killed pid=%d", pid))
}

func (p ProfilesPage) launchProfileCmd(selected domain.Profile) tea.Cmd {
	val := p.validator
	mgr := p.manager
	res := p.resolver
	lg := p.logger
	attemptID := log.NewAttemptID()
	mode := processmgr.LaunchBackground
	if !p.bgMode {
		mode = processmgr.LaunchForeground
	}
	return func() tea.Msg {
		evt := lg.With("attempt_id", attemptID, "profile_id", selected.ID)
		evt.Info("launch_pipeline_start", "mode", modeString(mode))
		if res == nil {
			return launchErrMsg{err: fmt.Errorf("no backend resolver configured")}
		}
		rb, err := res.Resolve(selected)
		if err != nil {
			evt.Error("launch_pipeline_failed", "step", "resolve", "err", err)
			return launchErrMsg{err: fmt.Errorf("resolve backend: %w", err)}
		}
		activeSchema := rb.Schema.ToFlagSchema()
		if val != nil {
			rep := val.Validate(selected, activeSchema, rb.Schema.BackendKind)
			if rep.HasBlockingErrors() {
				evt.Error("launch_pipeline_failed",
					"step", "validate", "err_count", len(rep.Errors))
				first := rep.Errors[0]
				firstHint := first.Field + ": " + first.Message
				return launchErrMsg{
					err:        fmt.Errorf("validation failed: %d error(s)", len(rep.Errors)),
					firstIssue: firstHint,
				}
			}
		}
		selected.Launch.ResolvedExecutable = rb.ExecutablePath
		selected.Launch.ResolvedBackendKind = rb.Backend.Kind
		inst, err := mgr.Launch(selected, mode, attemptID)
		if err != nil {
			evt.Error("launch_pipeline_failed", "step", "spawn", "err", err)
			return launchErrMsg{err: err}
		}
		return launchedMsg{inst: inst, attemptID: attemptID}
	}
}

func friendlyLaunchError(err error) string {
	switch {
	case errors.Is(err, processmgr.ErrPortBusy):
		return "error: port in use — change the profile port or kill the running PID"
	case errors.Is(err, processmgr.ErrModelNotFound):
		return "error: model file not found — fix the profile's Model path"
	case errors.Is(err, processmgr.ErrForegroundBusy):
		return "error: a foreground instance is already running — toggle [b] to background mode"
	case errors.Is(err, processmgr.ErrHealthCheckTimeout):
		return "error: server did not become healthy within timeout — check logs"
	default:
		return "error: " + err.Error()
	}
}

func enrichWithExit(base string, exit processmgr.ExitInfo) string {
	parts := []string{base}
	switch {
	case exit.ExitSignal != "":
		parts = append(parts, "(signal: "+exit.ExitSignal+")")
	case exit.ExitCode != nil:
		parts = append(parts, fmt.Sprintf("(exit %d)", *exit.ExitCode))
	}
	if last := lastNonEmpty(exit.StderrTail); last != "" {
		parts = append(parts, "— last: "+truncRunes(last, 80))
	}
	if len(parts) == 1 {
		return base
	}
	return strings.Join(parts, " ")
}

func lastNonEmpty(s []string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if strings.TrimSpace(s[i]) != "" {
			return s[i]
		}
	}
	return ""
}

func modeString(mode processmgr.LaunchMode) string {
	if mode == processmgr.LaunchForeground {
		return "foreground"
	}
	return "background"
}

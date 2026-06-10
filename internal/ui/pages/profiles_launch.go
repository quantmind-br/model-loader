package pages

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

// proxyLoadTimeout bounds EnsureRunning + Load. Model load can take minutes
// on large GGUFs, so the deadline is generous.
const proxyLoadTimeout = 5 * time.Minute

// ProxyController is the subset of *proxysupervisor.Supervisor the pages use
// to drive the backend lifecycle. All profile communication flows through the
// proxy; pages never talk to instance ports directly.
type ProxyController interface {
	EnsureRunning(context.Context) error
	Load(ctx context.Context, profileID string) (httpproxy.Status, error)
	Unload(ctx context.Context, force bool) (httpproxy.Status, error)
	Status() httpproxy.Status
	BaseURL() string
}

// WithProxyController wires the supervised HTTP proxy. Launch/stop actions
// are disabled when absent.
func (p ProfilesPage) WithProxyController(pc ProxyController) ProfilesPage {
	p.proxy = pc
	return p
}

// launchSelected loads the currently selected profile through the proxy.
func (p ProfilesPage) launchSelected() (tea.Model, tea.Cmd) {
	if p.launch.inFlight {
		return p, nil
	}
	if _, isCorrupt := p.list.SelectedItem().(corruptItem); isCorrupt {
		p, fc := p.withFlashError("corrupt entry — fix the JSON file or delete it")
		return p, fc
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok {
		return p, nil
	}
	if p.proxy == nil {
		p, fc := p.withFlashError("launch failed: HTTP proxy unavailable")
		return p, fc
	}
	return p.startLaunch(sel.p)
}

// startLaunch arms the in-flight tracker and dispatches the proxy load.
func (p ProfilesPage) startLaunch(selected domain.Profile) (ProfilesPage, tea.Cmd) {
	p.launch.inFlight = true
	p.launch.status = fmt.Sprintf("loading %s via proxy…", selected.Name)
	return p, tea.Batch(p.launch.spinner.Tick, p.launchProfileCmd(selected))
}

func (p ProfilesPage) handleProxyLoaded(msg proxyLoadedMsg) (tea.Model, tea.Cmd) {
	p.launch.inFlight = false
	p.launch.status = ""
	p, fc := p.withFlash(fmt.Sprintf("loaded %s (pid=%d) — serving at %s",
		msg.status.LoadedProfileID, msg.status.LoadedPID, p.proxy.BaseURL()))
	return p, tea.Batch(fc, func() tea.Msg { return SwitchToServerMsg{PID: msg.status.LoadedPID} })
}

func (p ProfilesPage) handleLaunchErr(msg launchErrMsg) (tea.Model, tea.Cmd) {
	p.launch.inFlight = false
	p.launch.status = ""
	base := friendlyLaunchError(msg.err)
	if msg.firstIssue != "" {
		base += " — " + msg.firstIssue
	}
	// F-01 audit: failures must use the high-contrast error styling and
	// the longer FlashLifetimeError so the user actually notices that
	// the launch was rejected rather than mistaking it for a no-op.
	p, fc := p.withFlashError(base)
	return p, fc
}

func (p ProfilesPage) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if !p.launch.inFlight {
		return p, nil
	}
	updated, cmd := p.launch.spinner.Update(msg)
	p.launch.spinner = updated
	return p, cmd
}

func (p ProfilesPage) handleLaunchProfile(msg LaunchProfileMsg) (tea.Model, tea.Cmd) {
	if p.proxy == nil {
		p, fc := p.withFlashError("launch failed: HTTP proxy unavailable")
		return p, fc
	}
	if p.launch.inFlight {
		return p, nil
	}
	selected, err := p.store.Get(msg.ID)
	if err != nil {
		p, fc := p.withFlashError("launch failed: " + err.Error())
		return p, fc
	}
	return p.startLaunch(selected)
}

// askUnloadCurrent opens a confirm for unloading the model currently served
// by the proxy ('K' shortcut).
func (p ProfilesPage) askUnloadCurrent() (tea.Model, tea.Cmd) {
	if p.proxy == nil {
		return p, nil
	}
	st := p.proxy.Status()
	if st.LoadedProfileID == "" {
		p, fc := p.withFlash("no model loaded")
		return p, fc
	}
	p.killConfirm = components.NewConfirm(
		fmt.Sprintf("Unload %s?", st.LoadedProfileID),
		st.LoadedProfileID,
		func(payload any) tea.Cmd {
			id, _ := payload.(string)
			return func() tea.Msg { return profilesUnloadConfirmedMsg{profileID: id} }
		},
		"Unload",
		"Cancel",
	)
	return p, p.killConfirm.Init()
}

func (p ProfilesPage) handleUnloadConfirmed(msg profilesUnloadConfirmedMsg) (tea.Model, tea.Cmd) {
	proxy := p.proxy
	profileID := msg.profileID
	return p, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := proxy.Unload(ctx, false)
		return profilesUnloadDoneMsg{profileID: profileID, err: err}
	}
}

func (p ProfilesPage) handleUnloadDone(msg profilesUnloadDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		p, fc := p.withFlashError("unload failed: " + msg.err.Error())
		return p, fc
	}
	p, fc := p.withFlash("unloaded " + msg.profileID)
	return p, fc
}

// launchProfileCmd validates the profile locally (resolve + schema validate)
// and then drives the proxy: EnsureRunning + /_admin/load. Load blocks until
// the backend is healthy, so no separate health wait is needed.
func (p ProfilesPage) launchProfileCmd(selected domain.Profile) tea.Cmd {
	val := p.validator
	res := p.resolver
	proxy := p.proxy
	lg := p.logger
	attemptID := log.NewAttemptID()
	return func() tea.Msg {
		evt := lg.With("attempt_id", attemptID, "profile_id", selected.ID)
		evt.Info("launch_pipeline_start")
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
		ctx, cancel := context.WithTimeout(context.Background(), proxyLoadTimeout)
		defer cancel()
		if err := proxy.EnsureRunning(ctx); err != nil {
			evt.Error("launch_pipeline_failed", "step", "proxy_start", "err", err)
			return launchErrMsg{err: fmt.Errorf("start proxy: %w", err)}
		}
		status, err := proxy.Load(ctx, selected.ID)
		if err != nil {
			evt.Error("launch_pipeline_failed", "step", "proxy_load", "err", err)
			return launchErrMsg{err: fmt.Errorf("load profile: %w", err)}
		}
		return proxyLoadedMsg{status: status}
	}
}

// friendlyLaunchError prefixes the launch failure for the flash bar. Errors
// arrive over the proxy's HTTP boundary, so processmgr sentinel matching via
// errors.Is is impossible here — the proxy already serializes a readable
// message into the error string.
func friendlyLaunchError(err error) string {
	return "error: " + err.Error()
}

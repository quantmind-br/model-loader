package pages

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/service/validator"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// LauncherPage is the Tab 1 page: pick a profile, choose mode, launch.
type LauncherPage struct {
	store     profilestore.Store
	manager   processmgr.Manager
	validator validator.Validator
	resolver  backendcatalog.Resolver
	logger    *slog.Logger

	profiles []domain.Profile
	plist    list.Model

	background bool
	status     string
	statusAt   time.Time
	flash      components.Flash
	running    []domain.RunningInstance

	width, height int
	loadErr       error

	killConfirm components.Confirm

	spin       spinner.Model
	waitingPID int
}

// launcherKillConfirmedMsg is emitted by killConfirm.onYes when the user
// confirms a kill. The page handles it in Update so manager I/O and status
// mutation stay on the UI thread.
type launcherKillConfirmedMsg struct{ pid int }

// NewLauncherPage builds a LauncherPage. manager/validator may be nil for
// smoke tests (UI degrades gracefully and the launch action is disabled).
func NewLauncherPage(store profilestore.Store, manager processmgr.Manager, val validator.Validator) LauncherPage {
	delegate := list.NewDefaultDelegate()
	l := list.New(nil, delegate, 40, 20)
	l.Title = "Profiles"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	return LauncherPage{
		store:      store,
		manager:    manager,
		validator:  val,
		plist:      l,
		background: true,
		spin:       sp,
		logger:     log.Nop(),
		flash:      components.NewFlash("launcher"),
	}
}

// WithLogger injects the application logger. Mirrors SetBackendResolver's
// value-receiver builder shape. Pages constructed without WithLogger keep
// the log.Nop() default — nil-safe by construction.
func (p LauncherPage) WithLogger(lg *slog.Logger) LauncherPage {
	if lg != nil {
		p.logger = lg
	}
	return p
}

// enrichWithExit appends the captured exit cause to a friendlyLaunchError
// base message. Output stays single-line. Last non-empty StderrTail line
// is truncated to 80 RUNES via truncRunes (NOT byte-based truncate).
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

// lastNonEmpty returns the last non-empty (post-trim) element of s.
func lastNonEmpty(s []string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if strings.TrimSpace(s[i]) != "" {
			return s[i]
		}
	}
	return ""
}

// modeString produces a stable label for log events.
func modeString(mode processmgr.LaunchMode) string {
	if mode == processmgr.LaunchForeground {
		return "foreground"
	}
	return "background"
}

// friendlyLaunchError translates sentinel manager errors into actionable
// hints shown in the page status line.
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

// SetBackendResolver injects the backend resolver so the launcher can
// validate against the correct schema and resolve the executable per profile.
func (p LauncherPage) SetBackendResolver(r backendcatalog.Resolver) LauncherPage {
	p.resolver = r
	return p
}

type LauncherProfilesLoadedMsg struct {
	Profiles []domain.Profile
	Err      error
}

// LaunchProfileMsg requests the Launcher to start the profile identified
// by ID. Emitted by ProfilesPage when the user presses [L]; routed by the
// root model after switching to the Launcher tab.
type LaunchProfileMsg struct {
	ID string
}

// launchedMsg is emitted after a successful Launch + WaitHealthy.
type launchedMsg struct {
	inst      domain.RunningInstance
	attemptID string
}

// launchErrMsg is emitted when validation or Launch itself fails.
type launchErrMsg struct {
	err error
}

type healthyMsg struct{ pid int }

type profileItem struct {
	p domain.Profile
}

func (i profileItem) Title() string {
	if i.p.Pinned {
		return "★ " + i.p.Name
	}
	return i.p.Name
}
func (i profileItem) Description() string {
	desc := fmt.Sprintf("%s | port %v", i.p.ID, i.p.Args["port"])
	if i.p.Launch.BackendID != "" {
		desc += " | backend: " + i.p.Launch.BackendID
	}
	return desc
}
func (i profileItem) FilterValue() string { return i.p.Name }

func (p LauncherPage) Init() tea.Cmd {
	return loadProfilesCmd(p.store)
}

func loadProfilesCmd(store profilestore.Store) tea.Cmd {
	return func() tea.Msg {
		got, err := store.List()
		return LauncherProfilesLoadedMsg{Profiles: got, Err: err}
	}
}

// Update is a thin dispatcher: each typed-message arm delegates to a
// private handle<MsgType> method. Non-key messages fall through to
// forwardToConfirms so the active confirm form (or the underlying list)
// can complete its internal Cmd→Msg handshake.
func (p LauncherPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		return p.handleResize(m)
	case LauncherProfilesLoadedMsg:
		return p.handleProfilesLoaded(m)
	case launchedMsg:
		return p.handleLaunched(m)
	case healthyMsg:
		return p.handleHealthy(m)
	case launchErrMsg:
		return p.handleLaunchErr(m)
	case spinner.TickMsg:
		return p.handleSpinnerTick(m)
	case components.FlashClearMsg:
		return p.handleFlashClear(m)
	case LaunchProfileMsg:
		return p.handleLaunchProfile(m)
	case launcherKillConfirmedMsg:
		return p.handleKillConfirmed(m)
	case tea.KeyMsg:
		return p.handleKey(m)
	}
	return p.forwardToConfirms(msg)
}

func (p LauncherPage) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	p.width, p.height = msg.Width, msg.Height
	p.plist.SetSize(msg.Width/2, msg.Height-6)
	return p, nil
}

func (p LauncherPage) handleProfilesLoaded(msg LauncherProfilesLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		p.loadErr = msg.Err
		return p, nil
	}
	sortProfilesPinnedFirst(msg.Profiles)
	p.profiles = msg.Profiles
	items := make([]list.Item, len(msg.Profiles))
	for i, pr := range msg.Profiles {
		items[i] = profileItem{p: pr}
	}
	p.plist.SetItems(items)
	return p, nil
}

func (p LauncherPage) handleLaunched(msg launchedMsg) (tea.Model, tea.Cmd) {
	p.running = append(p.running, msg.inst)
	p.waitingPID = msg.inst.PID
	// In-flight status — no auto-clear timer; terminal events replace it.
	p.status = fmt.Sprintf("pid=%d port=%d — waiting for /health…", msg.inst.PID, msg.inst.Port)
	p.statusAt = time.Time{}
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
	return p, tea.Batch(p.spin.Tick, waitCmd)
}

func (p LauncherPage) handleHealthy(msg healthyMsg) (tea.Model, tea.Cmd) {
	p.waitingPID = 0
	p, fc := p.withFlash(fmt.Sprintf("healthy pid=%d", msg.pid))
	pid := msg.pid
	return p, tea.Batch(fc, func() tea.Msg { return SwitchToServerMsg{PID: pid} })
}

func (p LauncherPage) handleLaunchErr(msg launchErrMsg) (tea.Model, tea.Cmd) {
	// Capture waitingPID BEFORE clearing so GetExitInfo can look up the
	// captured exit cause for enrichment.
	pid := p.waitingPID
	p.waitingPID = 0
	base := friendlyLaunchError(msg.err)
	if pid != 0 && p.manager != nil {
		if exit, ok := p.manager.GetExitInfo(pid); ok {
			base = enrichWithExit(base, exit)
		}
	}
	p, fc := p.withFlash(base)
	return p, fc
}

func (p LauncherPage) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if p.waitingPID == 0 {
		return p, nil
	}
	updated, cmd := p.spin.Update(msg)
	p.spin = updated
	return p, cmd
}

func (p LauncherPage) handleFlashClear(msg components.FlashClearMsg) (tea.Model, tea.Cmd) {
	p.flash, _ = p.flash.Update(msg)
	return p, nil
}

func (p LauncherPage) handleLaunchProfile(msg LaunchProfileMsg) (tea.Model, tea.Cmd) {
	if p.manager == nil {
		p, fc := p.withFlash("launch failed: process manager unavailable")
		return p, fc
	}
	selected, err := p.store.Get(msg.ID)
	if err != nil {
		p, fc := p.withFlash("launch failed: " + err.Error())
		return p, fc
	}
	// Refresh the in-memory list so the user sees the profile they
	// just launched ranked correctly. Best effort — failure here
	// only affects display, not the launch itself.
	if got, lerr := p.store.List(); lerr == nil {
		p.profiles = got
		items := make([]list.Item, len(got))
		for i, pr := range got {
			items[i] = profileItem{p: pr}
			if pr.ID == msg.ID {
				p.plist.Select(i)
			}
		}
		p.plist.SetItems(items)
	}
	return p, p.launchProfileCmd(selected)
}

func (p LauncherPage) handleKillConfirmed(msg launcherKillConfirmedMsg) (tea.Model, tea.Cmd) {
	var fc tea.Cmd
	p, fc = p.performKill(msg.pid)
	return p, fc
}

func (p LauncherPage) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if p.killConfirm.Active() {
		return p.updateConfirmKill(msg)
	}
	switch msg.String() {
	case "b":
		p.background = !p.background
		return p, nil
	case "k":
		if len(p.running) == 0 || p.manager == nil {
			return p, nil
		}
		return p.askConfirmKill(p.running[len(p.running)-1].PID)
	case "r":
		return p, loadProfilesCmd(p.store)
	case "p":
		return p.togglePinSelected()
	case "enter":
		if p.waitingPID != 0 {
			return p, nil
		}
		it, ok := p.plist.SelectedItem().(profileItem)
		if !ok || p.manager == nil {
			return p, nil
		}
		return p, p.launchProfileCmd(it.p)
	}
	updatedList, cmd := p.plist.Update(msg)
	p.plist = updatedList
	return p, cmd
}

// forwardToConfirms routes non-key messages to the active confirm form so
// huh's internal Cmd→Msg loop (initial focus, validation, button reveal)
// lands. When no confirm is active, the message falls through to the
// underlying list so its built-in handlers (filter ticks etc.) still run.
func (p LauncherPage) forwardToConfirms(msg tea.Msg) (tea.Model, tea.Cmd) {
	if p.killConfirm.Active() {
		var cmd tea.Cmd
		p.killConfirm, cmd = p.killConfirm.Update(msg)
		return p, cmd
	}
	updatedList, cmd := p.plist.Update(msg)
	p.plist = updatedList
	return p, cmd
}

// askConfirmKill builds the kill-confirmation overlay. The actual Kill is
// deferred until launcherKillConfirmedMsg is delivered (emitted by the
// Confirm.onYes callback when the user picks the affirmative button).
func (p LauncherPage) askConfirmKill(pid int) (tea.Model, tea.Cmd) {
	p.killConfirm = components.NewConfirm(
		fmt.Sprintf("Kill pid=%d?", pid),
		pid,
		func(payload any) tea.Cmd {
			id, _ := payload.(int)
			return func() tea.Msg { return launcherKillConfirmedMsg{pid: id} }
		},
		"Kill",
		"Cancel",
	)
	return p, p.killConfirm.Init()
}

func (p LauncherPage) updateConfirmKill(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		p.killConfirm = components.Confirm{}
		var fc tea.Cmd
		p, fc = p.withFlash("kill cancelled")
		return p, fc
	}
	var cmd tea.Cmd
	p.killConfirm, cmd = p.killConfirm.Update(msg)
	return p, cmd
}

// performKill executes the actual Kill in response to launcherKillConfirmedMsg.
// Pulled out of the Confirm callback so manager I/O and status updates remain
// on the page (the Confirm callback only emits a tea.Cmd).
func (p LauncherPage) performKill(pid int) (LauncherPage, tea.Cmd) {
	if err := p.manager.Kill(pid); err != nil {
		return p.withFlash("error: " + err.Error())
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

// IsCapturingInput tells the root model when the page owns global keys —
// true while a confirm dialog is on screen so its arrows / enter / y/n
// reach the form instead of being interpreted as tab shortcuts.
func (p LauncherPage) IsCapturingInput() bool {
	return p.killConfirm.Active()
}

// withFlash sets a terminal flash message (post-launch outcome, kill
// result, validation error) via the embedded Flash widget and clears the
// in-flight status so the spinner stops. The in-flight "waiting for
// /health…" line uses `status` directly (no auto-clear); only terminal
// outcomes flow through here.
func (p LauncherPage) withFlash(msg string) (LauncherPage, tea.Cmd) {
	p.status = ""
	p.statusAt = time.Time{}
	var cmd tea.Cmd
	p.flash, cmd = p.flash.Set(msg)
	return p, cmd
}

// launchProfileCmd validates the profile and starts the llama-server
// process. Shared by the [enter] keybinding and the LaunchProfileMsg path
// triggered from the Profiles tab via [L].
func (p LauncherPage) launchProfileCmd(selected domain.Profile) tea.Cmd {
	val := p.validator
	mgr := p.manager
	res := p.resolver
	lg := p.logger
	attemptID := log.NewAttemptID()
	mode := processmgr.LaunchBackground
	if !p.background {
		mode = processmgr.LaunchForeground
	}
	return func() tea.Msg {
		evt := lg.With("attempt_id", attemptID, "profile_id", selected.ID)
		evt.Info("launch_pipeline_start", "mode", modeString(mode))

		if res == nil {
			evt.Error("launch_pipeline_failed", "step", "resolver_unwired")
			return launchErrMsg{err: fmt.Errorf("no backend resolver configured")}
		}

		rb, err := res.Resolve(selected)
		if err != nil {
			evt.Error("launch_pipeline_failed", "step", "resolve", "err", err)
			return launchErrMsg{err: fmt.Errorf("resolve backend: %w", err)}
		}
		activeSchema := rb.Schema.ToFlagSchema()

		if val != nil {
			rep := val.Validate(selected, activeSchema)
			if rep.HasBlockingErrors() {
				evt.Error("launch_pipeline_failed",
					"step", "validate", "err_count", len(rep.Errors))
				return launchErrMsg{err: fmt.Errorf("validation failed: %d errors", len(rep.Errors))}
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

func (p LauncherPage) View() string {
	if p.killConfirm.Active() {
		return components.Modal("Kill Process", p.killConfirm.View(), p.width, p.height)
	}
	if p.loadErr != nil {
		return theme.Subtitle.Render(fmt.Sprintf("load profiles: %v", p.loadErr))
	}
	if len(p.profiles) == 0 && p.status == "" && p.flash.Message() == "" {
		return theme.Subtitle.Render("(no profiles yet — switch to Profiles [2] to create one)")
	}

	parts := []string{p.renderProfileDetail(), "", p.renderRunningList()}
	if status := p.renderStatusLine(); status != "" {
		parts = append(parts, status)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderProfileDetail renders the two-pane body: profile list on the left and
// the selected profile's metadata (or a "no selection" hint) on the right.
func (p LauncherPage) renderProfileDetail() string {
	var rightContent string
	if it, ok := p.plist.SelectedItem().(profileItem); ok {
		mode := "Foreground"
		if p.background {
			mode = "Background"
		}
		backendName := it.p.Launch.BackendID
		if p.resolver != nil {
			if rb, err := p.resolver.Resolve(it.p); err == nil {
				backendName = rb.Backend.Name
			}
		}
		rightContent = lipgloss.JoinVertical(lipgloss.Left,
			theme.Subtitle.Render(it.p.Name),
			fmt.Sprintf("ID:      %s", it.p.ID),
			fmt.Sprintf("Model:   %s", it.p.Model),
			fmt.Sprintf("Port:    %v", it.p.Args["port"]),
			fmt.Sprintf("Mode:    [%s]   (b to toggle)", mode),
			fmt.Sprintf("Backend: %s", backendName),
		)
	} else {
		rightContent = theme.Subtitle.Render("No profile selected")
	}

	leftW, rightW := theme.SplitTwoPanes(p.width)
	left := theme.Pane.Width(leftW).Render(p.plist.View())
	right := theme.Pane.Width(rightW).Render(rightContent)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// renderRunningList renders the "Running" section listing live instances, or
// the "Running: (none)" placeholder when no instances are tracked.
func (p LauncherPage) renderRunningList() string {
	if len(p.running) == 0 {
		return theme.Subtitle.Render("Running: (none) — press [enter] to launch selected profile")
	}
	lines := []string{theme.Subtitle.Render("Running")}
	for _, ri := range p.running {
		tag := "fg"
		if ri.Background {
			tag = "bg"
		}
		lines = append(lines, fmt.Sprintf("  %s pid=%d port=%d %s", ri.ProfileID, ri.PID, ri.Port, tag))
	}
	return strings.Join(lines, "\n")
}

// renderStatusLine renders the bottom line, preferring the in-flight
// status (spinner + non-expiring message) when set, falling back to the
// auto-clearing terminal flash. Empty string when neither is active.
func (p LauncherPage) renderStatusLine() string {
	if p.status != "" {
		statusLine := p.status
		if p.waitingPID != 0 {
			statusLine = p.spin.View() + " " + statusLine
		}
		return theme.Subtitle.Render(statusLine)
	}
	return p.flash.View()
}

// Hints implements ui.HintProvider for the Launcher tab.
func (p LauncherPage) togglePinSelected() (tea.Model, tea.Cmd) {
	it, ok := p.plist.SelectedItem().(profileItem)
	if !ok {
		return p, nil
	}
	pr := it.p
	pr.Pinned = !pr.Pinned
	if err := p.store.Save(pr); err != nil {
		p, fc := p.withFlash("pin failed: " + err.Error())
		return p, fc
	}
	return p, loadProfilesCmd(p.store)
}

func (p LauncherPage) Hints() string {
	if p.killConfirm.Active() {
		return "[←→] choose  [enter] confirm  [esc] cancel"
	}
	return "[b] mode  [enter] launch  [k] kill last  [r] refresh  [p] pin"
}

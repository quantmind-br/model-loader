package pages

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/llamabin"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/llamahelp"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/processmgr"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/profilestore"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/validator"
	"github.com/quantmind-br/llama-cpp-loader/internal/ui/components"
	"github.com/quantmind-br/llama-cpp-loader/internal/ui/theme"
)

// LauncherPage is the Tab 2 page: pick a profile, choose mode, launch.
type LauncherPage struct {
	store     profilestore.Store
	manager   processmgr.Manager
	validator validator.Validator
	schema    domain.FlagSchema

	// Binary selection: global default + per-profile override.
	globalBinary string
	schemaCache  *llamahelp.SchemaCache

	profiles []domain.Profile
	plist    list.Model

	background bool
	status     string
	statusAt   time.Time
	running    []domain.RunningInstance

	width, height int
	loadErr       error

	// Kill confirmation overlay (UIUX-002).
	killConfirm components.Confirm

	// WaitHealthy spinner (UIUX-003). waitingPID > 0 while a launch is
	// awaiting /health; spin advances on each spinner.TickMsg.
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
	}
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

// SetSchema injects the FlagSchema (used by validator at launch time).
func (p LauncherPage) SetSchema(s domain.FlagSchema) LauncherPage {
	p.schema = s
	return p
}

// SetBinaryResolver injects the global default binary and schema cache so
// the launcher can validate against the correct --help schema for the
// effective binary (global default or per-profile override).
func (p LauncherPage) SetBinaryResolver(globalBinary string, cache *llamahelp.SchemaCache) LauncherPage {
	p.globalBinary = globalBinary
	p.schemaCache = cache
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
	inst domain.RunningInstance
}

// launchErrMsg is emitted when validation or Launch itself fails.
type launchErrMsg struct {
	err error
}

type healthyMsg struct{ pid int }

type profileItem struct {
	p domain.Profile
}

func (i profileItem) Title() string { return i.p.Name }
func (i profileItem) Description() string {
	return fmt.Sprintf("%s | port %v", i.p.ID, i.p.Args["port"])
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
	case flashClearMsg:
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
	waitCmd := func() tea.Msg {
		if err := mgr.WaitHealthy(pid, port, 30*time.Second); err != nil {
			return launchErrMsg{err: fmt.Errorf("pid %d not healthy: %w", pid, err)}
		}
		return healthyMsg{pid: pid}
	}
	return p, tea.Batch(p.spin.Tick, waitCmd)
}

func (p LauncherPage) handleHealthy(msg healthyMsg) (tea.Model, tea.Cmd) {
	p.waitingPID = 0
	p, fc := p.withStatus(fmt.Sprintf("healthy pid=%d", msg.pid))
	pid := msg.pid
	return p, tea.Batch(fc, func() tea.Msg { return SwitchToMonitorMsg{PID: pid} })
}

func (p LauncherPage) handleLaunchErr(msg launchErrMsg) (tea.Model, tea.Cmd) {
	p.waitingPID = 0
	p, fc := p.withStatus(friendlyLaunchError(msg.err))
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

func (p LauncherPage) handleFlashClear(msg flashClearMsg) (tea.Model, tea.Cmd) {
	if msg.tag == "launcher" && msg.at.Equal(p.statusAt) {
		p.status = ""
		p.statusAt = time.Time{}
	}
	return p, nil
}

func (p LauncherPage) handleLaunchProfile(msg LaunchProfileMsg) (tea.Model, tea.Cmd) {
	if p.manager == nil {
		p, fc := p.withStatus("launch failed: process manager unavailable")
		return p, fc
	}
	selected, err := p.store.Get(msg.ID)
	if err != nil {
		p, fc := p.withStatus("launch failed: " + err.Error())
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
	case "enter":
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
	)
	return p, p.killConfirm.Init()
}

func (p LauncherPage) updateConfirmKill(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		p.killConfirm = components.Confirm{}
		var fc tea.Cmd
		p, fc = p.withStatus("kill cancelled")
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
		return p.withStatus("error: " + err.Error())
	}
	out := p.running[:0]
	for _, ri := range p.running {
		if ri.PID != pid {
			out = append(out, ri)
		}
	}
	p.running = out
	return p.withStatus(fmt.Sprintf("killed pid=%d", pid))
}

// IsCapturingInput tells the root model when the page owns global keys —
// true while a confirm dialog is on screen so its arrows / enter / y/n
// reach the form instead of being interpreted as tab shortcuts.
func (p LauncherPage) IsCapturingInput() bool {
	return p.killConfirm.Active()
}

// withStatus sets the terminal status message (post-launch outcome,
// kill result, validation error) and schedules an auto-clear via
// flashClearMsg. Use this for terminal states only — the in-flight
// "waiting for /health…" message must NOT auto-clear or it would erase
// itself before the health check returns.
func (p LauncherPage) withStatus(msg string) (LauncherPage, tea.Cmd) {
	p.status = msg
	p.statusAt = time.Now()
	return p, scheduleFlashClear("launcher", p.statusAt)
}

// launchProfileCmd validates the profile and starts the llama-server
// process. Shared by the [enter] keybinding and the LaunchProfileMsg path
// triggered from the Profiles tab via [L].
func (p LauncherPage) launchProfileCmd(selected domain.Profile) tea.Cmd {
	val := p.validator
	mgr := p.manager
	mode := processmgr.LaunchBackground
	if !p.background {
		mode = processmgr.LaunchForeground
	}
	globalBinary := p.globalBinary
	cache := p.schemaCache
	schema := p.schema
	return func() tea.Msg {
		// Resolve the effective binary (profile override > global > PATH default).
		effectiveBinary := llamabin.Effective(selected.Launch.LlamaServerBinaryPath, globalBinary)

		// Fetch the correct schema for this binary (cache hit or re-parse).
		activeSchema := schema
		if cache != nil && effectiveBinary != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if s, err := cache.Get(ctx, effectiveBinary); err == nil {
				activeSchema = s
			}
		}

		if val != nil {
			rep := val.Validate(selected, activeSchema)
			if rep.HasBlockingErrors() {
				return launchErrMsg{err: fmt.Errorf("validation failed: %d errors", len(rep.Errors))}
			}
		}
		inst, err := mgr.Launch(selected, mode)
		if err != nil {
			return launchErrMsg{err: err}
		}
		return launchedMsg{inst: inst}
	}
}

func (p LauncherPage) View() string {
	if p.killConfirm.Active() {
		return p.killConfirm.View()
	}
	if p.loadErr != nil {
		return theme.Subtitle.Render(fmt.Sprintf("load profiles: %v", p.loadErr))
	}
	if len(p.profiles) == 0 && p.status == "" {
		return theme.Subtitle.Render("(no profiles yet — switch to Profiles [1] to create one)")
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
		effective := llamabin.Effective(it.p.Launch.LlamaServerBinaryPath, p.globalBinary)
		rightContent = lipgloss.JoinVertical(lipgloss.Left,
			theme.Subtitle.Render(it.p.Name),
			fmt.Sprintf("ID:     %s", it.p.ID),
			fmt.Sprintf("Model:  %s", it.p.Model),
			fmt.Sprintf("Port:   %v", it.p.Args["port"]),
			fmt.Sprintf("Mode:   [%s]   (b to toggle)", mode),
			fmt.Sprintf("Binary: %s", effective),
		)
	} else {
		rightContent = theme.Subtitle.Render("No profile selected")
	}

	leftW := p.width / 2
	rightW := p.width/2 - 2
	if leftW < 20 {
		leftW = 20
	}
	if rightW < 20 {
		rightW = 20
	}
	left := theme.Pane.Width(leftW).Render(p.plist.View())
	right := theme.Pane.Width(rightW).Render(rightContent)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// renderRunningList renders the "Running" section listing live instances, or
// the "Running: (none)" placeholder when no instances are tracked.
func (p LauncherPage) renderRunningList() string {
	if len(p.running) == 0 {
		return "Running: (none)"
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

// renderStatusLine renders the bottom status flash (with optional spinner) or
// returns an empty string when there is no active status to show.
func (p LauncherPage) renderStatusLine() string {
	if p.status == "" {
		return ""
	}
	statusLine := p.status
	if p.waitingPID != 0 {
		statusLine = p.spin.View() + " " + statusLine
	}
	style := theme.Subtitle
	if !p.statusAt.IsZero() && time.Since(p.statusAt) >= flashDimAfter {
		style = style.Faint(true)
	}
	return style.Render(statusLine)
}

// Hints implements ui.HintProvider for the Launcher tab.
func (p LauncherPage) Hints() string {
	if p.killConfirm.Active() {
		return "[←→] choose  [enter] confirm  [esc] cancel"
	}
	return "[b] mode  [enter] launch  [k] kill last  [r] refresh"
}

// Package ui hosts the root Bubbletea model and tab routing.
package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/pages"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// Tab identifies a top-level section.
type Tab int

const (
	TabLauncher Tab = iota
	TabProfiles
	TabServer
	TabModels
	TabBackends
)

// tabCount is the number of top-level tabs. Single source of truth for
// keybinding ranges, modulo math, and array sizing.
const tabCount = 5

func (t Tab) Title() string {
	switch t {
	case TabLauncher:
		return "Launcher"
	case TabProfiles:
		return "Profiles"
	case TabServer:
		return "Server"
	case TabModels:
		return "Models"
	case TabBackends:
		return "Backends"
	default:
		return "?"
	}
}

// Page is the contract every tab page implements.
type Page interface {
	Init() tea.Cmd
	Update(tea.Msg) (tea.Model, tea.Cmd)
	View() string
}

// InputCapture is the optional contract a page implements to claim
// global keybindings (Tab/Shift+Tab) while a modal/editor/picker is
// active. When IsCapturingInput returns true the root forwards the
// keystroke to the page instead of cycling tabs.
type InputCapture interface {
	IsCapturingInput() bool
}

// Reloader is the optional contract a page implements to refresh its
// state on demand (e.g. on tab focus, when external files may have
// changed).
type Reloader interface {
	Reload() tea.Cmd
}

// HintProvider is the optional contract a page implements to publish
// page-local key hints. The root reads it after every tab activation
// and key event, then concatenates with the global hints into the
// status bar — pages no longer render their own footer strings.
type HintProvider interface {
	Hints() string
}

// HelpContextProvider is the optional contract a page implements to
// supply richer markdown context for the help modal. When absent the
// root falls back to HintProvider.Hints().
type HelpContextProvider interface {
	HelpContext() string
}

// globalHints is the prefix shown in every status bar line.
const globalHints = "[1-5] tabs  [tab] next  [q] quit" + components.HelpToken

// bootBlocker carrega o conteúdo de um modal bloqueante exibido sobre toda a UI.
type bootBlocker struct {
	title string
	body  string
}

// RootModel is the top-level tea.Model.
type RootModel struct {
	pages           [tabCount]tea.Model
	active          Tab
	status          components.StatusBar
	width           int
	height          int
	bootBlocker     *bootBlocker
	helpOpen        bool
	playgroundOpen  bool
	playgroundModal tea.Model
	pm              processmgr.Manager
}

// NewRoot constructs a RootModel with placeholder pages.
// Slice 0 swaps the Launcher slot with the real implementation in main.go.
func NewRoot(initial Tab) RootModel {
	return RootModel{
		pages: [tabCount]tea.Model{
			pages.Placeholder{TabName: TabLauncher.Title()},
			pages.Placeholder{TabName: TabProfiles.Title()},
			pages.Placeholder{TabName: TabServer.Title()},
			pages.Placeholder{TabName: TabModels.Title()},
			pages.Placeholder{TabName: TabBackends.Title()},
		},
		active: initial,
		status: components.StatusBar{Hints: globalHints},
	}
}

// WithProfilesPage replaces the placeholder Profiles tab with a real model.
// Used by main.go after services are wired.
func (m RootModel) WithProfilesPage(p tea.Model) RootModel {
	m.pages[TabProfiles] = p
	return m
}

// WithModelsPage replaces the placeholder Models tab with a real model.
func (m RootModel) WithModelsPage(p tea.Model) RootModel {
	m.pages[TabModels] = p
	return m
}

// WithLauncherPage replaces the placeholder Launcher tab with a real model.
func (m RootModel) WithLauncherPage(p tea.Model) RootModel {
	m.pages[TabLauncher] = p
	return m
}

// WithBackendsPage replaces the placeholder Backends tab with a real model.
func (m RootModel) WithBackendsPage(p tea.Model) RootModel {
	m.pages[TabBackends] = p
	return m
}

// WithServerPage replaces the placeholder Server tab with a real model.
func (m RootModel) WithServerPage(p tea.Model) RootModel {
	m.pages[TabServer] = p
	return m
}

// WithProcessManager injects the process manager for the playground modal.
func (m RootModel) WithProcessManager(pm processmgr.Manager) RootModel {
	m.pm = pm
	return m
}

// WithStatusWarn sets a warning message on the status bar (used at boot to
// surface schema fallback notices).
func (m RootModel) WithStatusWarn(msg string) RootModel {
	m.status.SetMessage(components.StatusWarn, msg)
	return m
}

// WithBootBlocker mostra um modal bloqueante sobre toda a UI. Usado quando
// algum recurso crítico falta na boot (e.g. llama-server fora do PATH).
// Apenas `q` / `ctrl+c` continuam respondendo enquanto o blocker está ativo.
func (m RootModel) WithBootBlocker(title, body string) RootModel {
	m.bootBlocker = &bootBlocker{title: title, body: body}
	return m
}

func (m RootModel) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.pages))
	for _, p := range m.pages {
		if c := p.Init(); c != nil {
			cmds = append(cmds, c)
		}
	}
	return tea.Batch(cmds...)
}

func (m RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.bootBlocker != nil {
		return m.handleBootBlocker(msg)
	}
	switch msg := msg.(type) {
	case pages.LauncherProfilesLoadedMsg:
		return m.forwardTo(TabLauncher, msg)
	case pages.UseInNewProfileMsg:
		return m.activateAndForward(TabProfiles, msg)
	case pages.SwitchToServerMsg:
		return m.handleSwitchToServer(msg)
	case pages.NavigateToSizingMsg:
		return m.handleNavigateToSizing(msg)
	case pages.LaunchProfileMsg:
		return m.activateAndForward(TabLauncher, msg)
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.forwardToActivePage(msg)
	}
	return m.broadcast(msg)
}

// handleBootBlocker swallows every message while the boot-blocker modal is
// up. Only `q` and `ctrl+c` quit; window resizes are tracked so the modal
// re-renders at the new size.
func (m RootModel) handleBootBlocker(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		if k.Type == tea.KeyCtrlC || (k.Type == tea.KeyRunes && len(k.Runes) == 1 && k.Runes[0] == 'q') {
			return m, tea.Quit
		}
	}
	if w, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = w.Width, w.Height
	}
	return m, nil
}

// handleResize forwards a sized window message (minus header + status row)
// to every page so each tab can re-layout, even ones not currently active.
func (m RootModel) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width, m.height = msg.Width, msg.Height
	var cmds []tea.Cmd
	for i, p := range m.pages {
		updated, cmd := p.Update(tea.WindowSizeMsg{
			Width:  msg.Width,
			Height: theme.BodyHeight(msg.Height),
		})
		m.pages[i] = updated
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	m.recomputeHints()
	return m, tea.Batch(cmds...)
}

// handleSwitchToServer activates the Server tab and translates the cross-
// tab message into a ServerSelectPIDMsg so the page refreshes + selects
// the requested row.
func (m RootModel) handleSwitchToServer(msg pages.SwitchToServerMsg) (tea.Model, tea.Cmd) {
	m.active = TabServer
	updated, cmd := m.pages[TabServer].Update(pages.ServerSelectPIDMsg{PID: msg.PID})
	m.pages[TabServer] = updated
	m.recomputeHints()
	return m, cmd
}

// handleNavigateToSizing switches to the Profiles tab and forwards the
// NavigateToSizingMsg so the profiles page can select the matching profile
// and open its editor on the sizing sub-tab.
func (m RootModel) handleNavigateToSizing(msg pages.NavigateToSizingMsg) (tea.Model, tea.Cmd) {
	m.active = TabProfiles
	updated, cmd := m.pages[TabProfiles].Update(msg)
	m.pages[TabProfiles] = updated
	m.recomputeHints()
	return m, cmd
}

// handleKey dispatches a key event. ctrl+c and ctrl+p are unconditional
// global shortcuts — every other binding (?, q, 1-5, tab, shift+tab) is gated
// by IsCapturingInput so printable keys reach an active editor/picker
// instead of triggering quit/tab-switch/help.
func (m RootModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.helpOpen {
		return m.handleHelpKey(msg)
	}
	if m.playgroundOpen && m.playgroundModal != nil {
		updated, cmd := m.playgroundModal.Update(msg)
		m.playgroundModal = updated
		if msg.String() == "esc" || msg.String() == "ctrl+p" {
			m.playgroundOpen = false
		}
		return m, cmd
	}
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	// TODO(playground): re-enable Ctrl+P binding once streaming wiring lands.
	//                   PlaygroundModal struct kept intact to avoid churn in importers.
	if !m.activePageCapturesInput() {
		if msg.String() == "?" {
			m.helpOpen = true
			return m, nil
		}
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "1":
			return m.activate(TabLauncher)
		case "2":
			return m.activate(TabProfiles)
		case "3":
			return m.activate(TabServer)
		case "4":
			return m.activate(TabModels)
		case "5":
			return m.activate(TabBackends)
		case "tab":
			return m.activate((m.active + 1) % tabCount)
		case "shift+tab":
			return m.activate((m.active + tabCount - 1) % tabCount)
		}
	}
	updated, cmd := m.forwardToActivePage(msg)
	if rm, ok := updated.(RootModel); ok {
		rm.recomputeHints()
		return rm, cmd
	}
	return updated, cmd
}

// handleHelpKey runs while the help modal is on screen: `?` and `esc` close
// it, `ctrl+c` still quits, every other key is swallowed.
func (m RootModel) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?", "esc":
		m.helpOpen = false
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

// activateAndForward switches to t before forwarding the message — used by
// cross-tab navigations that need both the tab change and the page update.
func (m RootModel) activateAndForward(t Tab, msg tea.Msg) (tea.Model, tea.Cmd) {
	m.active = t
	updated, cmd := m.pages[t].Update(msg)
	m.pages[t] = updated
	m.recomputeHints()
	return m, cmd
}

// forwardTo delivers the message to a specific tab without changing focus.
func (m RootModel) forwardTo(t Tab, msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.pages[t].Update(msg)
	m.pages[t] = updated
	return m, cmd
}

// forwardToActivePage delivers the message to the currently active tab.
func (m RootModel) forwardToActivePage(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.pages[m.active].Update(msg)
	m.pages[m.active] = updated
	return m, cmd
}

// broadcast delivers cmd→msg returns from background pipelines (model
// scanner, monitor ticks) to every page so the owning page receives them
// even when it isn't active. Also refreshes the status-bar hints because
// async msgs (e.g. huh form transitions to StateCompleted via
// nextFieldMsg / nextGroupMsg) can change what the active page exposes
// via HintProvider.
func (m RootModel) broadcast(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	for i, p := range m.pages {
		updated, cmd := p.Update(msg)
		m.pages[i] = updated
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	m.recomputeHints()
	return m, tea.Batch(cmds...)
}

func (m RootModel) View() string {
	if m.bootBlocker != nil {
		return components.Modal(m.bootBlocker.title, m.bootBlocker.body+"\n\nPress q to quit.", m.width, m.height)
	}
	if m.helpOpen {
		activeContext := ""
		if h, ok := m.pages[m.active].(HelpContextProvider); ok {
			activeContext = h.HelpContext()
		} else if h, ok := m.pages[m.active].(HintProvider); ok {
			activeContext = h.Hints()
		}
		body, err := components.RenderContextualHelp(m.width-8, activeContext)
		if err != nil {
			body = components.HelpMarkdown // fallback raw
		}
		return components.Modal("Keybindings", body, m.width, m.height)
	}
	header := m.renderTabs()
	body := m.pages[m.active].View()
	status := m.status.Render(m.width)
	view := lipgloss.JoinVertical(lipgloss.Left, header, body, status)
	if m.playgroundOpen && m.playgroundModal != nil {
		overlay := m.playgroundModal.View()
		view = components.Overlay(view, overlay, m.width, m.height)
	}
	return view
}

// activate switches to the given tab and triggers Reload on the page if
// it implements the Reloader contract. This keeps Profiles in sync with
// external file changes without requiring a TUI restart.
func (m RootModel) activate(t Tab) (tea.Model, tea.Cmd) {
	m.active = t
	m.recomputeHints()
	if r, ok := m.pages[t].(Reloader); ok {
		return m, r.Reload()
	}
	return m, nil
}

// recomputeHints reads page-local hints (when the active page implements
// HintProvider) and updates the status bar to globalHints + " | " +
// page hints. Called after every page state change so the status footer
// always reflects what the user can do right now.
func (m *RootModel) recomputeHints() {
	if h, ok := m.pages[m.active].(HintProvider); ok {
		if ph := h.Hints(); ph != "" {
			m.status.Hints = globalHints + " | " + ph
		} else {
			m.status.Hints = globalHints
		}
	} else {
		m.status.Hints = globalHints
	}
	m.status.RestartCount = 0
	if m.pm != nil {
		for _, inst := range m.pm.List() {
			m.status.RestartCount += inst.RestartCount
		}
	}
}

func (m RootModel) activePageCapturesInput() bool {
	if ic, ok := m.pages[m.active].(InputCapture); ok {
		return ic.IsCapturingInput()
	}
	return false
}

func (m RootModel) renderTabs() string {
	sep := theme.Subtitle.Render(" │ ")
	parts := make([]string, 0, 2*tabCount)
	for i := Tab(0); i < tabCount; i++ {
		if i > 0 {
			parts = append(parts, sep)
		}
		title := fmt.Sprintf("%d %s", int(i)+1, i.Title())
		if i == m.active {
			parts = append(parts, theme.TabActive.Render(title))
		} else {
			parts = append(parts, theme.TabInactive.Render(title))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

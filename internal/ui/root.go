// Package ui hosts the root Bubbletea model and tab routing.
package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
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
	TabProfiles Tab = iota
	TabServer
	TabModels
	TabBackends
)

// tabCount is the number of top-level tabs. Single source of truth for
// keybinding ranges, modulo math, and array sizing.
const tabCount = 4

// globalShortcut registers a root-level keyboard shortcut. Entries with
// Captures==false are skipped when the active page is capturing input
// (modal / form / picker open). Only ctrl+c may have Captures==true.
type globalShortcut struct {
	Keys     []string
	Captures bool
	Handler  func(RootModel) (tea.Model, tea.Cmd)
}

var rootShortcuts = []globalShortcut{
	{Keys: []string{"ctrl+c"}, Captures: true, Handler: func(m RootModel) (tea.Model, tea.Cmd) { return m, tea.Quit }},
	{Keys: []string{"?"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { m.helpOpen = true; m = m.ensureHelpViewport(); return m, nil }},
	{Keys: []string{"esc"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { return m, nil }},
	{Keys: []string{"q"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { return m, tea.Quit }},
	{Keys: []string{"1"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { return m.activate(TabProfiles) }},
	{Keys: []string{"2"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { return m.activate(TabServer) }},
	{Keys: []string{"3"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { return m.activate(TabModels) }},
	{Keys: []string{"4"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { return m.activate(TabBackends) }},
	{Keys: []string{"tab"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { return m.activate((m.active + 1) % tabCount) }},
	{Keys: []string{"shift+tab"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { return m.activate((m.active + tabCount - 1) % tabCount) }},
}

func (t Tab) Title() string {
	switch t {
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

// Overlayer is the optional contract a page implements to expose an active
// modal overlay that should be rendered on top of the page content.
type Overlayer interface {
	OverlayView() pages.Overlay
}

// globalHints is the prefix shown in every status bar line.
const globalHints = "[1-4] tabs  [tab] next  [q] quit" + components.HelpToken

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
	helpViewport    viewport.Model
	helpReady       bool
	playgroundOpen  bool
	playgroundModal tea.Model
	pm              processmgr.Manager
}

// NewRoot constructs a RootModel with placeholder pages.
func NewRoot(initial Tab) RootModel {
	return RootModel{
		pages: [tabCount]tea.Model{
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
	case pages.UseInNewProfileMsg:
		return m.activateAndForward(TabProfiles, msg)
	case pages.SwitchToServerMsg:
		return m.handleSwitchToServer(msg)
	case pages.NavigateToSizingMsg:
		return m.handleNavigateToSizing(msg)
	case pages.LaunchProfileMsg:
		return m.activateAndForward(TabProfiles, msg)
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
// global shortcuts — every other binding (?, q, 1-4, tab, shift+tab) is gated
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
	for _, sc := range rootShortcuts {
		for _, k := range sc.Keys {
			if msg.String() == k {
				if !sc.Captures && m.activePageCapturesInput() {
					break
				}
				return sc.Handler(m)
			}
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
// it, `ctrl+c` still quits, scroll keys (Up/Down/PgUp/PgDn/k/j/home/end)
// drive the viewport so long help content is reachable. Every other key
// is swallowed.
func (m RootModel) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?", "esc":
		m.helpOpen = false
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	if helpScrollKey(msg) {
		m = m.ensureHelpViewport()
		var cmd tea.Cmd
		m.helpViewport, cmd = m.helpViewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

// ensureHelpViewport guarantees the help viewport is sized, has the
// current page's contextual help loaded, and is ready to receive scroll
// keys — even on Update paths that never call View.
func (m RootModel) ensureHelpViewport() RootModel {
	if m.height <= 0 {
		return m
	}
	viewportW, viewportH := helpViewportSize(m.width, m.height)
	if !m.helpReady || m.helpViewport.Width != viewportW || m.helpViewport.Height != viewportH {
		m.helpViewport = viewport.New(viewportW, viewportH)
		m.helpReady = true
	}
	activeContext := ""
	if h, ok := m.pages[m.active].(HelpContextProvider); ok {
		activeContext = h.HelpContext()
	} else if h, ok := m.pages[m.active].(HintProvider); ok {
		activeContext = h.Hints()
	}
	body, err := components.RenderContextualHelp(m.width-8, activeContext)
	if err != nil {
		body = components.HelpMarkdown
	}
	m.helpViewport.SetContent(body)
	return m
}

// helpViewportSize returns the inner width/height for the help modal
// viewport, accounting for the modal box border + padding so the scroll
// area always fits inside the terminal.
func helpViewportSize(width, height int) (int, int) {
	w := width - 12
	if w < 20 {
		w = 20
	}
	h := height - 8
	if h < 5 {
		h = 5
	}
	return w, h
}

// helpScrollKey reports whether msg matches a scroll keybinding the
// viewport should consume while the help modal is open.
func helpScrollKey(msg tea.KeyMsg) bool {
	scroll := []key.Binding{
		key.NewBinding(key.WithKeys("up", "k")),
		key.NewBinding(key.WithKeys("down", "j")),
		key.NewBinding(key.WithKeys("pgup", "ctrl+u")),
		key.NewBinding(key.WithKeys("pgdown", "ctrl+d")),
		key.NewBinding(key.WithKeys("home", "g")),
		key.NewBinding(key.WithKeys("end", "G")),
	}
	for _, b := range scroll {
		if key.Matches(msg, b) {
			return true
		}
	}
	return false
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
		if m.height <= 0 {
			activeContext := ""
			if h, ok := m.pages[m.active].(HelpContextProvider); ok {
				activeContext = h.HelpContext()
			} else if h, ok := m.pages[m.active].(HintProvider); ok {
				activeContext = h.Hints()
			}
			body, err := components.RenderContextualHelp(m.width-8, activeContext)
			if err != nil {
				body = components.HelpMarkdown
			}
			return components.Modal("Keybindings", body, m.width, m.height)
		}
		m = m.ensureHelpViewport()
		return components.Modal("Keybindings (↑/↓/PgUp/PgDn/k/j/g/G to scroll · ? or esc to close)", m.helpViewport.View(), m.width, m.height)
	}
	header := m.renderTabs()
	status := m.status.Render(m.width)
	bodyHeight := theme.BodyHeight(m.height)
	bodyWidth := m.width
	var clampedBody string
	if bodyHeight > 0 {
		rawBody := m.pages[m.active].View()
		clampedBody = theme.ClampBody(rawBody, bodyWidth, bodyHeight)
	}
	frame := lipgloss.JoinVertical(lipgloss.Left, header, clampedBody, status)
	if ov, ok := m.pages[m.active].(Overlayer); ok {
		overlay := ov.OverlayView()
		if overlay.Active {
			frame = components.Overlay(frame, overlay.Content, m.width, m.height)
		}
	}
	if m.playgroundOpen && m.playgroundModal != nil {
		overlay := m.playgroundModal.View()
		frame = components.Overlay(frame, overlay, m.width, m.height)
	}
	return frame
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
	labels := make([]string, tabCount)
	for i := Tab(0); i < tabCount; i++ {
		labels[i] = fmt.Sprintf("%d %s", int(i)+1, i.Title())
	}
	return components.TabBar(components.TabBarOptions{
		Labels:         labels,
		ActiveIndex:    int(m.active),
		AvailableWidth: m.width,
	})
}

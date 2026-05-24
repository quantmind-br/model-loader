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
	TabBenchmark
)

// tabCount is the number of top-level tabs. Single source of truth for
// keybinding ranges, modulo math, and array sizing.
const tabCount = 5

// globalShortcut registers a root-level keyboard shortcut. Entries with
// Captures==false are skipped when the active page is capturing input
// (modal / form / picker open). Only ctrl+c may have Captures==true.
type globalShortcut struct {
	Keys     []string
	Captures bool
	Handler  func(RootModel) (tea.Model, tea.Cmd)
}

var rootShortcuts = []globalShortcut{
	{Keys: []string{"ctrl+c"}, Captures: true, Handler: func(m RootModel) (tea.Model, tea.Cmd) { m.cleanupAll(); return m, tea.Quit }},
	{Keys: []string{"?"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) {
		m.helpOpen = true
		m = m.ensureHelpViewport()
		m.helpViewport.GotoTop()
		return m, nil
	}},
	{Keys: []string{"esc"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { return m, nil }},
	{Keys: []string{"q"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { m.cleanupAll(); return m, tea.Quit }},
	{Keys: []string{"1"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { return m.activate(TabProfiles) }},
	{Keys: []string{"2"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { return m.activate(TabServer) }},
	{Keys: []string{"3"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { return m.activate(TabModels) }},
	{Keys: []string{"4"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { return m.activate(TabBackends) }},
	{Keys: []string{"5"}, Captures: false, Handler: func(m RootModel) (tea.Model, tea.Cmd) { return m.activate(TabBenchmark) }},
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
	case TabBenchmark:
		return "Benchmark"
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

// StatusMessageProvider is the optional contract a page implements to
// publish a short status string (success, warning, error) to the always-
// visible right side of the status bar. This complements the in-body
// flash component: at narrow geometries (e.g. 80x24) the flash can be
// clipped below the visible viewport, so critical feedback (launch
// failures, export results) ALSO needs to land in the status bar where
// it is always rendered.
//
// Returning an empty message clears the status bar. The level controls
// styling (info/warn/error). The root reads this after every event in
// recomputeHints — pages do not need to push imperatively.
type StatusMessageProvider interface {
	StatusMessage() (msg string, level components.StatusLevel)
}

// Overlayer is the optional contract a page implements to expose an active
// modal overlay that should be rendered on top of the page content.
type Overlayer interface {
	OverlayView() pages.Overlay
}

// Cleaner is the optional contract a page implements to release resources
// (e.g. cancel an active web-edit session) when the TUI is about to quit.
// RootModel calls Cleanup() on all pages before returning tea.Quit.
type Cleaner interface {
	Cleanup()
}

// globalHints is the prefix shown in every status bar line.
const globalHints = "[1-5] tabs  [tab] next  [q] quit" + components.HelpToken

// bootBlocker loads blocking modal content displayed over the entire UI.
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
			pages.Placeholder{TabName: TabBenchmark.Title()},
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

// WithBenchmarkPage replaces the placeholder Benchmark tab with a real model.
func (m RootModel) WithBenchmarkPage(p tea.Model) RootModel {
	m.pages[TabBenchmark] = p
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

// WithBootBlocker shows a blocking modal over the entire UI. Used when
// a critical resource is missing at boot (e.g. llama-server outside PATH).
// Only `q` / `ctrl+c` keep responding while the blocker is active.
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
			m.cleanupAll()
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
		m.cleanupAll()
		return m, tea.Quit
	case "g", "home":
		// Jump-to-top: the bubbles viewport keymap doesn't bind these, so
		// drive GotoTop explicitly (DEAD-01).
		m = m.ensureHelpViewport()
		m.helpViewport.GotoTop()
		return m, nil
	case "G", "end":
		m = m.ensureHelpViewport()
		m.helpViewport.GotoBottom()
		return m, nil
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
	// Wrap help at the viewport's inner width so glamour's word-wrap matches
	// the visible area exactly — otherwise text wrapped wider than the box
	// gets clipped mid-word at the right border (RENDER-04).
	body, err := components.RenderContextualHelp(viewportW, activeContext)
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
	w := width - 16
	if w < 16 {
		w = 16
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
	// Note: g/G/home/end are handled directly in handleHelpKey via
	// GotoTop/GotoBottom, since the viewport keymap doesn't bind them.
	scroll := []key.Binding{
		key.NewBinding(key.WithKeys("up", "k")),
		key.NewBinding(key.WithKeys("down", "j")),
		key.NewBinding(key.WithKeys("pgup", "ctrl+u")),
		key.NewBinding(key.WithKeys("pgdown", "ctrl+d")),
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
		title := "Keybindings (↑/↓/PgUp/PgDn/k/j/g/G to scroll · ? to toggle · esc to close)"
		if m.width < 80 {
			title = "Keybindings"
		}
		return components.Modal(title, m.helpViewport.View(), m.width, m.height)
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
// always reflects what the user can do right now. Also pulls the active
// page's StatusMessage so critical feedback (launch errors, export
// results) lands in the always-visible status bar instead of being
// clipped to the in-body flash at narrow geometries.
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
	if sp, ok := m.pages[m.active].(StatusMessageProvider); ok {
		msg, level := sp.StatusMessage()
		if msg != "" {
			m.status.SetMessage(level, msg)
		} else if m.status.Message != "" && m.status.Level != components.StatusWarn {
			// Clear stale page-sourced messages but preserve the boot
			// warning (StatusWarn from WithStatusWarn) until it's
			// explicitly replaced.
			m.status.SetMessage(components.StatusInfo, "")
		}
	}
	m.status.RestartCount = 0
	if m.pm != nil {
		for _, inst := range m.pm.List() {
			m.status.RestartCount += inst.RestartCount
		}
	}
}

// cleanupAll calls Cleanup() on every page that implements Cleaner.
// Invoked on both quit paths (ctrl+c, q) so pages can cancel lingering
// goroutines (e.g. configweb sessions) before the process exits.
func (m RootModel) cleanupAll() {
	for i := range m.pages {
		if c, ok := m.pages[i].(Cleaner); ok {
			c.Cleanup()
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

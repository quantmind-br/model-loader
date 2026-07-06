// Package ui hosts the root Bubbletea model and tab routing.
package ui

import (
	"fmt"

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

// bootBlocker loads blocking modal content displayed over the entire UI.
type bootBlocker struct {
	title string
	body  string
}

// RootModel is the top-level tea.Model.
type RootModel struct {
	pages  [tabCount]tea.Model
	active Tab
	// badges marks tabs with a pending background event (download finished/
	// failed, instance crash). Set by TabAttentionMsg, cleared when the tab
	// is visited.
	badges       [tabCount]bool
	status       components.StatusBar
	width        int
	height       int
	bootBlocker  *bootBlocker
	helpOpen     bool
	helpViewport viewport.Model
	helpReady    bool
	pm           processmgr.Manager
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

// WithProcessManager injects the process manager so recomputeHints can total
// the per-instance restart count shown in the status bar.
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
	case pages.TabAttentionMsg:
		// Comes from a page cmd; root consumes it (no broadcast). Badging the
		// active tab would be noise — the user is already looking at it.
		if t, ok := attentionTab(msg.Page); ok && t != m.active {
			m.badges[t] = true
		}
		return m, nil
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
	return m.activateAndForward(TabServer, pages.ServerSelectPIDMsg{PID: msg.PID})
}

// handleNavigateToSizing switches to the Profiles tab and forwards the
// NavigateToSizingMsg so the profiles page can select the matching profile
// and open its editor on the sizing sub-tab.
func (m RootModel) handleNavigateToSizing(msg pages.NavigateToSizingMsg) (tea.Model, tea.Cmd) {
	return m.activateAndForward(TabProfiles, msg)
}

// handleKey dispatches a key event. ctrl+c is the only unconditional global
// shortcut — every other binding (?, q, 1-5, tab, shift+tab) is gated by
// IsCapturingInput so printable keys reach an active editor/picker instead of
// triggering quit/tab-switch/help.
func (m RootModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.helpOpen {
		return m.handleHelpKey(msg)
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

// setActive is the single place a tab switch happens: it moves focus and
// clears the tab's attention badge. Every path that assigns m.active must
// go through it so badges can't outlive a visit.
func (m RootModel) setActive(t Tab) RootModel {
	m.active = t
	m.badges[t] = false
	return m
}

// activateAndForward switches to t before forwarding the message — used by
// cross-tab navigations that need both the tab change and the page update.
func (m RootModel) activateAndForward(t Tab, msg tea.Msg) (tea.Model, tea.Cmd) {
	m = m.setActive(t)
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
	if m.width > 0 && m.height > 0 &&
		(m.width < theme.MinTermWidth || m.height < theme.MinTermHeight) {
		notice := fmt.Sprintf("Terminal too small\nmin %d×%d — now %d×%d",
			theme.MinTermWidth, theme.MinTermHeight, m.width, m.height)
		return lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(
			lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, notice))
	}
	if m.bootBlocker != nil {
		return components.Modal(m.bootBlocker.title, m.bootBlocker.body+"\n\nPress q to quit.", m.width, m.height)
	}
	if m.helpOpen {
		return m.renderHelpModal()
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
	return frame
}

// activate switches to the given tab and triggers Reload on the page if
// it implements the Reloader contract. This keeps Profiles in sync with
// external file changes without requiring a TUI restart.
func (m RootModel) activate(t Tab) (tea.Model, tea.Cmd) {
	m = m.setActive(t)
	m.recomputeHints()
	if r, ok := m.pages[t].(Reloader); ok {
		return m, r.Reload()
	}
	return m, nil
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

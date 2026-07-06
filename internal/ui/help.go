package ui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/ui/components"
)

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
	w := max(16, width-16)
	h := max(3, height-8)
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

// renderHelpModal renders the help overlay shown when helpOpen is set. At
// zero height (some teatest paths) it falls back to a non-scrolling modal;
// otherwise it drives the sized, scrollable viewport. Title glyphs stay
// pure ASCII: arrow/middot are East-Asian ambiguous-width and some
// terminals render them 2 cells wide, overrunning the box (RENDER-02).
func (m RootModel) renderHelpModal() string {
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
	title := "Keybindings  (up/down/PgUp/PgDn/k/j/g/G scroll  |  ? toggle  |  esc close)"
	if m.width < 80 {
		title = "Keybindings"
	}
	return components.Modal(title, m.helpViewport.View(), m.width, m.height)
}

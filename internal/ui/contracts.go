package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/pages"
)

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

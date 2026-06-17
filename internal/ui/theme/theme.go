// Package theme exposes shared lipgloss styles.
package theme

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Colors. AdaptiveColor selects Light or Dark variant based on the
	// terminal's background, detected by lipgloss.
	ColorAccent     = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	ColorOK         = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	ColorWarn       = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}
	ColorError      = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	ColorDim        = lipgloss.AdaptiveColor{Light: "#57606a", Dark: "#6e7681"}
	ColorSelectedBG = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#1f6feb"}
	ColorSelectedFG = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#ffffff"}
	AccentBg        = ColorSelectedBG // alias for active tab background
	AccentFg        = ColorSelectedFG // alias for active tab foreground

	// Borders & layout.
	Border = lipgloss.RoundedBorder()

	// Text styles.
	Title    lipgloss.Style
	Subtitle lipgloss.Style
	OK       lipgloss.Style
	Warn     lipgloss.Style
	Error    lipgloss.Style
	Selected lipgloss.Style

	// Tab styles.
	TabActive   lipgloss.Style
	TabInactive lipgloss.Style

	// Panes.
	Pane lipgloss.Style
)

func init() {
	RebuildStyles()
}

// NoColor reports whether the NO_COLOR env var requests color suppression.
func NoColor() bool { return os.Getenv("NO_COLOR") != "" }

// RebuildStyles (re)constructs all exported Style vars from the current
// palette + NO_COLOR state. Exposed for tests that toggle NO_COLOR and need
// to rebuild styles deterministically.
func RebuildStyles() {
	Title = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	Subtitle = lipgloss.NewStyle().Foreground(ColorDim)
	OK = lipgloss.NewStyle().Foreground(ColorOK)
	Warn = lipgloss.NewStyle().Foreground(ColorWarn)
	Error = lipgloss.NewStyle().Foreground(ColorError)
	Selected = lipgloss.NewStyle().Background(ColorSelectedBG).Foreground(ColorSelectedFG)

	TabActive = lipgloss.NewStyle().
		Bold(true).
		Foreground(AccentFg).
		Background(AccentBg).
		Padding(0, 1)
	TabInactive = lipgloss.NewStyle().
		Foreground(ColorDim).
		Padding(0, 1)

	Pane = lipgloss.NewStyle().
		Border(Border).
		BorderForeground(ColorDim).
		Padding(PanePaddingY, PanePaddingX)

	if NoColor() {
		Title = Title.UnsetForeground().UnsetBackground()
		Subtitle = Subtitle.UnsetForeground().UnsetBackground()
		OK = OK.UnsetForeground().UnsetBackground()
		Warn = Warn.UnsetForeground().UnsetBackground()
		Error = Error.UnsetForeground().UnsetBackground()
		Selected = Selected.UnsetForeground().UnsetBackground().Bold(true)
		TabActive = TabActive.UnsetForeground().UnsetBackground()
		TabInactive = TabInactive.UnsetForeground().UnsetBackground()
		Pane = Pane.BorderForeground(lipgloss.NoColor{})
	}
}

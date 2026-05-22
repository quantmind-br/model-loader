package components

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// modalBoxStyle returns the outer box style. Built at render time so that
// theme.NoColor() toggles take effect without a separate rebuild step.
func modalBoxStyle() lipgloss.Style {
	s := lipgloss.NewStyle().
		Border(theme.Border).
		BorderForeground(theme.ColorAccent).
		Padding(1, 2)
	if theme.NoColor() {
		s = s.BorderForeground(lipgloss.NoColor{})
	}
	return s
}

// modalTitleStyle returns the title style with NO_COLOR awareness.
func modalTitleStyle() lipgloss.Style {
	s := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.ColorAccent).
		Underline(true).
		Margin(0, 0, 1, 0)
	if theme.NoColor() {
		s = s.UnsetForeground().UnsetBackground().Underline(false)
	}
	return s
}

// Modal renders title + body in a centered box. When width or
// height are 0, returns only the box (without a Place wrapper) — useful for
// inspection in tests or extra composition. When width > 0 and height > 0,
// the box is centered on a canvas of that size.
func Modal(title, body string, width, height int) string {
	box := modalBoxStyle().Render(modalTitleStyle().Render(title) + "\n" + body)
	if width <= 0 || height <= 0 {
		return box
	}
	// Clamp to reasonable minimums so the modal doesn't collapse on tiny terminals.
	if width < 20 {
		width = 20
	}
	if height < 5 {
		height = 5
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

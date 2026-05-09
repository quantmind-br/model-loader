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
		Margin(0, 0, 1, 0)
	if theme.NoColor() {
		s = s.UnsetForeground().UnsetBackground()
	}
	return s
}

// Modal renderiza title + body em uma caixa centralizada. Quando width ou
// height são 0, retorna apenas a caixa (sem Place wrapper) — útil para
// inspeção em testes ou composições adicionais. Quando width > 0 e height > 0,
// a caixa é centralizada num canvas dessa dimensão.
func Modal(title, body string, width, height int) string {
	box := modalBoxStyle().Render(modalTitleStyle().Render(title) + "\n" + body)
	if width <= 0 || height <= 0 {
		return box
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

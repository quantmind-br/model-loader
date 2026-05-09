// Package pages holds tab page implementations.
package pages

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// Placeholder is a tab page used until the real implementation lands.
type Placeholder struct {
	TabName string
	width   int
	height  int
}

func (p Placeholder) Init() tea.Cmd { return nil }

func (p Placeholder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		p.width, p.height = sz.Width, sz.Height
	}
	return p, nil
}

func (p Placeholder) View() string {
	body := theme.Subtitle.Render(p.TabName + " — coming soon")
	return lipgloss.Place(p.width, p.height, lipgloss.Center, lipgloss.Center, body)
}

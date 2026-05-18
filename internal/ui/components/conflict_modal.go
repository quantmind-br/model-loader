package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

type ConflictModal struct {
	active    bool
	path      string
	selected  int
	onConfirm func(profilestore.ConflictMode) tea.Cmd
	onCancel  func() tea.Cmd
}

var conflictLabels = []string{"merge (skip duplicates)", "overwrite (replace)", "rename (append -imported-N)"}
var conflictModes = []profilestore.ConflictMode{profilestore.ConflictModeMerge, profilestore.ConflictModeOverwrite, profilestore.ConflictModeRename}

func NewConflictModal(path string, onConfirm func(profilestore.ConflictMode) tea.Cmd, onCancel func() tea.Cmd) ConflictModal {
	return ConflictModal{active: true, path: path, onConfirm: onConfirm, onCancel: onCancel}
}

func (c ConflictModal) Active() bool { return c.active }

func (c ConflictModal) Init() tea.Cmd { return nil }

func (c ConflictModal) Update(msg tea.Msg) (ConflictModal, tea.Cmd) {
	if !c.active {
		return c, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			c.active = false
			if c.onCancel != nil {
				return c, c.onCancel()
			}
			return c, nil
		case "enter":
			c.active = false
			if c.onConfirm != nil {
				return c, c.onConfirm(conflictModes[c.selected])
			}
			return c, nil
		case "left", "up":
			if c.selected > 0 {
				c.selected--
			}
			return c, nil
		case "right", "down":
			if c.selected < len(conflictLabels)-1 {
				c.selected++
			}
			return c, nil
		}
	}
	return c, nil
}

func (c ConflictModal) View() string {
	if !c.active {
		return ""
	}
	var b strings.Builder
	b.WriteString(theme.Title.Render("Import Conflict") + "\n\n")
	b.WriteString("File: " + c.path + "\n\n")
	for i, label := range conflictLabels {
		if i == c.selected {
			b.WriteString(lipgloss.NewStyle().Foreground(theme.ColorAccent).Render("> "+label) + "\n")
		} else {
			b.WriteString("  " + label + "\n")
		}
	}
	b.WriteString("\n[←→] choose  [enter] confirm  [esc] cancel")
	return modalBoxStyle().Render(b.String())
}

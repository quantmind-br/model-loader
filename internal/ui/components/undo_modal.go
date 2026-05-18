package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

type FieldDiff struct {
	Key      string
	OldValue string
	NewValue string
}

type UndoModal struct {
	active    bool
	diffs     []FieldDiff
	onConfirm func() tea.Cmd
	onCancel  func() tea.Cmd
}

var redStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171"))
var greenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80"))

func NewUndoModal(diffs []FieldDiff, onConfirm func() tea.Cmd, onCancel func() tea.Cmd) UndoModal {
	return UndoModal{active: true, diffs: diffs, onConfirm: onConfirm, onCancel: onCancel}
}

func (u UndoModal) Active() bool { return u.active }

func (u UndoModal) Init() tea.Cmd { return nil }

func (u UndoModal) Update(msg tea.Msg) (UndoModal, tea.Cmd) {
	if !u.active {
		return u, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			u.active = false
			if u.onCancel != nil {
				return u, u.onCancel()
			}
			return u, nil
		case "enter":
			u.active = false
			if u.onConfirm != nil {
				return u, u.onConfirm()
			}
			return u, nil
		}
	}
	return u, nil
}

func (u UndoModal) View() string {
	if !u.active {
		return ""
	}
	var b strings.Builder
	b.WriteString(theme.Title.Render("Undo Changes") + "\n\n")
	if len(u.diffs) == 0 {
		b.WriteString("No changes to undo.\n")
	} else {
		for _, d := range u.diffs {
			b.WriteString(d.Key + ":\n")
			b.WriteString("  old: " + redStyle.Render(d.OldValue) + "\n")
			b.WriteString("  new: " + greenStyle.Render(d.NewValue) + "\n\n")
		}
	}
	b.WriteString("[enter] confirm  [esc] cancel")
	return modalBoxStyle().Render(b.String())
}

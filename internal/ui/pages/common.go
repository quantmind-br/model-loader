package pages

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

func flashSuccess(f components.Flash, msg string) (components.Flash, tea.Cmd) {
	var cmd tea.Cmd
	f, cmd = f.Set(msg)
	return f, cmd
}

func flashError(f components.Flash, msg string) (components.Flash, tea.Cmd) {
	var cmd tea.Cmd
	f, cmd = f.SetError(msg)
	return f, cmd
}

func CaptureAny(preds ...func() bool) bool {
	for _, p := range preds {
		if p() {
			return true
		}
	}
	return false
}

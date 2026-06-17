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

// setupConfirm builds a yes/no Confirm and returns it together with its Init
// cmd, collapsing the NewConfirm(...) + Init() pairing every confirm site
// duplicated. onYes fires when the user confirms; callers capture whatever
// the affirmative branch needs directly in the closure, so the component's
// payload plumbing (and its per-site type assertion) is no longer threaded by
// hand. Typical use:
//
//	var cmd tea.Cmd
//	p.deleteConfirm, cmd = setupConfirm("Delete X?", "Delete", "Cancel",
//	    func() tea.Cmd { return func() tea.Msg { return deleteConfirmedMsg{id: id} } })
//	return p, cmd
func setupConfirm(title, affirmative, negative string, onYes func() tea.Cmd) (components.Confirm, tea.Cmd) {
	c := components.NewConfirm(title, nil, func(any) tea.Cmd { return onYes() }, affirmative, negative)
	return c, c.Init()
}

func CaptureAny(preds ...func() bool) bool {
	for _, p := range preds {
		if p() {
			return true
		}
	}
	return false
}

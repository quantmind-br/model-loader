package components

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// Confirm is a yes/no dialog wrapping a *huh.Form. It is a value type so it
// can be embedded by-value in pages with value receivers; the inner form,
// answer pointer, and payload all live on the heap so address-equality is
// preserved across the value-copy idiom Bubble Tea uses on every Update.
//
// Lifecycle: NewConfirm builds the form (Active() == true). Update forwards
// every msg to the inner form; on huh.StateCompleted it invokes onYes(payload)
// when the user confirmed and clears the inner form (Active() flips false).
// Callers test Active() to drive their own teardown.
type Confirm struct {
	form    *huh.Form
	answer  *bool
	payload any
	onYes   func(any) tea.Cmd
}

// NewConfirm builds a yes/no dialog labelled with title. payload is passed
// through to onYes when the user confirms; nil onYes is allowed (caller
// polls Active() to detect completion). Empty labels default to Yes/No.
func NewConfirm(title string, payload any, onYes func(any) tea.Cmd, affirmative, negative string) Confirm {
	answer := false
	affirmativeLabel := affirmative
	if affirmativeLabel == "" {
		affirmativeLabel = "Yes"
	}
	negativeLabel := negative
	if negativeLabel == "" {
		negativeLabel = "No"
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(title).
			Affirmative(affirmativeLabel).
			Negative(negativeLabel).
			Value(&answer),
	)).WithShowHelp(false).WithShowErrors(false)
	return Confirm{
		form:    form,
		answer:  &answer,
		payload: payload,
		onYes:   onYes,
	}
}

// Active reports whether the dialog is still on screen (form not yet cleared).
func (c Confirm) Active() bool { return c.form != nil }

// View delegates to the inner form. Empty string when inactive.
func (c Confirm) View() string {
	if c.form == nil {
		return ""
	}
	return c.form.View()
}

// Init delegates to the inner form so its Cmd→Msg handshake (focus, button
// styling) starts. Returns nil when inactive.
func (c Confirm) Init() tea.Cmd {
	if c.form == nil {
		return nil
	}
	return c.form.Init()
}

// Update forwards msg to the inner form. On huh.StateCompleted it invokes
// onYes(payload) if the answer is affirmative, clears the inner form, and
// returns the resulting Cmd batched with the form's own Cmd. Non-key msgs
// must be forwarded so huh's async nextFieldMsg/nextGroupMsg lands and the
// form actually transitions to StateCompleted.
func (c Confirm) Update(msg tea.Msg) (Confirm, tea.Cmd) {
	if c.form == nil {
		return c, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		if k.String() == "esc" {
			c.form = nil
			c.answer = nil
			return c, nil
		}
	}
	updated, cmd := c.form.Update(msg)
	if f, ok := updated.(*huh.Form); ok {
		c.form = f
	}
	if c.form != nil && c.form.State == huh.StateCompleted {
		var yesCmd tea.Cmd
		if c.answer != nil && *c.answer && c.onYes != nil {
			yesCmd = c.onYes(c.payload)
		}
		c.form = nil
		c.answer = nil
		return c, tea.Batch(cmd, yesCmd)
	}
	return c, cmd
}

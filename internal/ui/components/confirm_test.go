package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// completeForm reaches into the wrapped *huh.Form and forces it to
// StateCompleted. Driving huh's keymap end-to-end is brittle in unit tests,
// so this surgical hack is the cleanest way to exercise the completion path.
// It also flips the underlying answer pointer to the caller-provided value
// so the onYes branch is selectable.
func completeForm(c Confirm, answer bool) Confirm {
	if c.answer != nil {
		*c.answer = answer
	}
	if c.form != nil {
		c.form.State = huh.StateCompleted
	}
	return c
}

func TestConfirm_NewConfirmActive(t *testing.T) {
	c := NewConfirm("really?", nil, nil, "", "")
	if !c.Active() {
		t.Fatal("fresh Confirm should be Active()")
	}
}

func TestConfirm_YesResolutionInvokesOnYes(t *testing.T) {
	type payload struct{ ID string }
	want := payload{ID: "alpha"}
	var got payload
	called := false

	c := NewConfirm("kill?", want, func(p any) tea.Cmd {
		called = true
		got = p.(payload)
		return nil
	}, "", "")

	c = completeForm(c, true)
	c, _ = c.Update(struct{}{}) // any msg drives the completion check

	if !called {
		t.Fatal("onYes was not invoked on affirmative completion")
	}
	if got != want {
		t.Errorf("payload = %#v, want %#v", got, want)
	}
	if c.Active() {
		t.Error("Confirm should not be Active() after completion")
	}
}

func TestConfirm_NoResolutionDoesNotInvoke(t *testing.T) {
	called := false
	c := NewConfirm("kill?", "x", func(_ any) tea.Cmd {
		called = true
		return nil
	}, "", "")

	c = completeForm(c, false)
	c, _ = c.Update(struct{}{})

	if called {
		t.Error("onYes invoked despite negative answer")
	}
	if c.Active() {
		t.Error("Confirm should not be Active() after completion")
	}
}

func TestConfirm_NonKeyMsgIsForwarded(t *testing.T) {
	c := NewConfirm("ok?", nil, nil, "", "")
	// Stash form pointer so we can assert it received the msg by checking
	// the returned form is the same instance (huh forms return themselves
	// from Update unless they internally swap, which they don't for
	// WindowSizeMsg).
	before := c.form

	c, _ = c.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	if c.form == nil {
		t.Fatal("WindowSizeMsg should not clear the form")
	}
	if c.form != before {
		t.Errorf("form pointer changed after WindowSizeMsg; expected forward to inner form")
	}
	if !c.Active() {
		t.Error("Confirm should still be Active() after non-completion msg")
	}
}

func TestConfirm_ActiveAfterCompletion(t *testing.T) {
	c := NewConfirm("ok?", nil, nil, "", "")
	c = completeForm(c, false)
	c, _ = c.Update(struct{}{})
	if c.Active() {
		t.Error("Active() should return false after StateCompleted")
	}
}

func TestConfirm_NilOnYesWithAffirmative(t *testing.T) {
	c := NewConfirm("ok?", "payload", nil, "", "") // nil onYes
	c = completeForm(c, true)
	c, _ = c.Update(struct{}{}) // must not panic
	if c.Active() {
		t.Error("Active() should return false after StateCompleted with nil onYes")
	}
}

func TestConfirm_NoColorStripsForeground(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	t.Cleanup(func() {
		t.Setenv("NO_COLOR", "")
		theme.RebuildStyles()
	})

	c := NewConfirm("ok?", nil, nil, "", "")
	out := c.View()
	if strings.Contains(out, "\x1b[38;") || strings.Contains(out, "\x1b[48;") {
		t.Errorf("confirm output contains color SGR under NO_COLOR: %q", out)
	}
}

// focusConfirm drives the Init Cmd→Msg handshake so huh finishes focusing a
// button before the view is rendered.
func focusConfirm(c Confirm) Confirm {
	if cmd := c.Init(); cmd != nil {
		if msg := cmd(); msg != nil {
			c, _ = c.Update(msg)
		}
	}
	return c
}

func TestConfirm_FocusedButtonCarriesMarkers(t *testing.T) {
	c := focusConfirm(NewConfirm("ok?", nil, nil, "Kill", "Cancel"))
	out := c.View()
	if got := strings.Count(out, ">"); got != 1 {
		t.Errorf("focused-button open marker count = %d, want 1 in %q", got, out)
	}
	if got := strings.Count(out, "<"); got != 1 {
		t.Errorf("focused-button close marker count = %d, want 1 in %q", got, out)
	}
}

func TestConfirmTheme_FocusedButtonUsesSelectedPalette(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	th := confirmTheme()
	if got := th.Focused.FocusedButton.GetForeground(); got != theme.ColorSelectedFG {
		t.Errorf("focused button foreground = %v, want %v", got, theme.ColorSelectedFG)
	}
	if got := th.Focused.FocusedButton.GetBackground(); got != theme.ColorSelectedBG {
		t.Errorf("focused button background = %v, want %v", got, theme.ColorSelectedBG)
	}
}

func TestConfirm_FooterHasNoKeyList(t *testing.T) {
	c := focusConfirm(NewConfirm("ok?", nil, nil, "Kill", "Cancel"))
	out := c.View()
	if strings.Contains(out, "switch") {
		t.Errorf("footer still lists the switch key: %q", out)
	}
	if strings.Contains(out, "esc cancel") {
		t.Errorf("footer still lists the esc key: %q", out)
	}
	if !strings.Contains(out, "Enter") {
		t.Errorf("footer lost the Enter focus hint: %q", out)
	}
}

func TestConfirm_MarkersSurviveNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	t.Cleanup(func() {
		t.Setenv("NO_COLOR", "")
		theme.RebuildStyles()
	})

	c := focusConfirm(NewConfirm("ok?", nil, nil, "Kill", "Cancel"))
	out := c.View()
	if !strings.Contains(out, ">") || !strings.Contains(out, "<") {
		t.Errorf("markers dropped under NO_COLOR: %q", out)
	}
}

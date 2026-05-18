package components

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

const (
	FlashLifetime      = 6 * time.Second
	FlashLifetimeError = 15 * time.Second
	FlashDimAfter      = 5 * time.Second
)

// FlashClearMsg is the tea.Msg emitted by the Cmd that Set returns, once
// FlashLifetime has elapsed. Tag identifies the owning page so unrelated
// Flash instances ignore it; At is the timestamp the flash carried at
// scheduling so a newer flash on the same page can detect a stale clear.
type FlashClearMsg struct {
	Tag string
	At  time.Time
}

// Flash is a value-type auto-clearing status message bound to a page tag.
// Embed by value; mutation flows through the returned new Flash from Set
// and Update, mirroring the Confirm/Modal idiom in this package.
type Flash struct {
	tag     string
	message string
	at      time.Time
}

// NewFlash builds a Flash bound to a page-identifying tag. The tag flows
// into every FlashClearMsg the Set Cmd emits so cross-page ticks are
// dropped by Update.
func NewFlash(tag string) Flash {
	return Flash{tag: tag}
}

// Set replaces the visible message, stamps "now", and returns a tea.Cmd
// that delivers FlashClearMsg after FlashLifetime. Callers wire the cmd
// into their Update return so bubbletea drives the timer.
//
// Setting a new message before the previous tick fires is safe: the older
// tick will arrive with the previous At, and Update will discard it as
// stale (preventing the new flash from being erased prematurely).
func (f Flash) Set(message string) (Flash, tea.Cmd) {
	f.message = message
	f.at = time.Now()
	tag := f.tag
	at := f.at
	return f, tea.Tick(FlashLifetime, func(time.Time) tea.Msg {
		return FlashClearMsg{Tag: tag, At: at}
	})
}

func (f Flash) SetError(message string) (Flash, tea.Cmd) {
	f.message = message
	f.at = time.Now()
	tag := f.tag
	at := f.at
	return f, tea.Tick(FlashLifetimeError, func(time.Time) tea.Msg {
		return FlashClearMsg{Tag: tag, At: at}
	})
}

// Update handles incoming FlashClearMsg with stale-tick protection. The
// returned bool is true iff this flash was cleared. Callers typically
// ignore the bool — they just thread the returned Flash back in:
//
//	case components.FlashClearMsg:
//	    p.flash, _ = p.flash.Update(msg)
//	    return p, nil
//
// Wrong-tag messages and stale (mismatched At) messages are silently
// dropped so multi-page apps using one Flash instance per page can all
// receive the global tick without trampling each other.
func (f Flash) Update(msg tea.Msg) (Flash, bool) {
	cm, ok := msg.(FlashClearMsg)
	if !ok {
		return f, false
	}
	if cm.Tag != f.tag {
		return f, false
	}
	if !cm.At.Equal(f.at) {
		return f, false
	}
	f.message = ""
	f.at = time.Time{}
	return f, true
}

// View renders the current message styled with theme.Subtitle, switching
// to Faint once the message age exceeds FlashDimAfter so flashes visibly
// age before they auto-clear. Empty string when no flash is active so
// callers can use the result directly in lipgloss.JoinVertical without
// special-casing the empty state.
func (f Flash) View() string {
	if f.message == "" {
		return ""
	}
	style := theme.Subtitle
	if !f.at.IsZero() && time.Since(f.at) >= FlashDimAfter {
		style = style.Faint(true)
	}
	return style.Render(f.message)
}

// Message returns the raw flash text (without styling). Used by tests and
// pages that need to compose the flash into a custom render pipeline
// (e.g. LauncherPage prefixing with a spinner).
func (f Flash) Message() string { return f.message }

// At returns the time the current message was Set. Zero value when no
// flash is active. Exposed for tests and dim-style detection by callers
// that render the message themselves rather than via View().
func (f Flash) At() time.Time { return f.at }

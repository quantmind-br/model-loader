// Package components contains reusable UI building blocks.
package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// HelpToken is the trailing footer hint for the help modal. Page footers
// append it so the keybinding stays consistent across pages.
const HelpToken = "  [?] help"

// StatusLevel categorizes a status message.
type StatusLevel int

const (
	StatusInfo StatusLevel = iota
	StatusWarn
	StatusError
)

// StatusBar renders the bottom-of-screen line with hints and the latest message.
type StatusBar struct {
	Hints        string
	Message      string
	Level        StatusLevel
	Since        time.Time
	RestartCount int
}

// SetMessage updates the status message and level.
func (s *StatusBar) SetMessage(level StatusLevel, msg string) {
	s.Message = msg
	s.Level = level
	s.Since = time.Now()
}

// Render returns the bar as a styled single line, fitted to width.
func (s StatusBar) Render(width int) string {
	hints := theme.Subtitle.Render(s.Hints)
	msg := s.styledMessage()
	badge := s.restartBadge()

	gap := width - lipgloss.Width(hints) - lipgloss.Width(msg) - lipgloss.Width(badge)
	if gap < 1 {
		gap = 1
	}
	return hints + strings.Repeat(" ", gap) + badge + msg
}

func (s StatusBar) restartBadge() string {
	if s.RestartCount <= 0 {
		return ""
	}
	return theme.Warn.Render(fmt.Sprintf("⚠ %d restarts  ", s.RestartCount))
}

func (s StatusBar) styledMessage() string {
	if s.Message == "" {
		return ""
	}
	switch s.Level {
	case StatusError:
		return theme.Error.Render(s.Message)
	case StatusWarn:
		return theme.Warn.Render(s.Message)
	default:
		return theme.Subtitle.Render(s.Message)
	}
}

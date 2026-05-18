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
	globalPart := s.Hints
	pagePart := ""
	if i := strings.LastIndex(s.Hints, " | "); i >= 0 {
		globalPart = s.Hints[:i]
		pagePart = s.Hints[i+3:]
	}
	separatorWidth := 0
	if pagePart != "" {
		separatorWidth = lipgloss.Width(theme.Subtitle.Render(" | "))
	}
	gw := lipgloss.Width(theme.Subtitle.Render(globalPart))
	pw := lipgloss.Width(theme.Subtitle.Render(pagePart))
	msg := s.styledMessage()
	badge := s.restartBadge()
	gap := width - gw - separatorWidth - pw - lipgloss.Width(msg) - lipgloss.Width(badge)
	if gap < 1 {
		avail := width - gw - separatorWidth - lipgloss.Width(msg) - lipgloss.Width(badge) - 1
		if avail > 0 {
			pagePart = truncateString(pagePart, avail)
		} else {
			pagePart = ""
		}
		gap = 1
	}
	hints := theme.Subtitle.Render(globalPart)
	if pagePart != "" {
		hints += theme.Subtitle.Render(" | " + pagePart)
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

func truncateString(s string, max int) string {
	if lipgloss.Width(s) <= max {
		return s
	}
	runes := []rune(s)
	if max <= 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}

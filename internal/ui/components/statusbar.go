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

// FooterMode tells the status bar which compact hint set to render.
type FooterMode string

// Page-specific footer modes for conditional hint rendering.
const (
	ModeProfilesSelected  FooterMode = "profiles-selected"
	ModeProfilesFiltering FooterMode = "profiles-filtering"
	ModeProfilesEditing   FooterMode = "profiles-editing"
	ModeProfilesPicker    FooterMode = "profiles-picker"
	ModeProfilesConfirm   FooterMode = "profiles-confirm"

	ModeModelsSelected  FooterMode = "models-selected"
	ModeModelsFiltering FooterMode = "models-filtering"
	ModeModelsPicker    FooterMode = "models-picker"
	ModeModelsConfirm   FooterMode = "models-confirm"
	ModeModelsAction    FooterMode = "models-action"
	ModeModelsInfo      FooterMode = "models-info"

	ModeBackendsSelected  FooterMode = "backends-selected"
	ModeBackendsFiltering FooterMode = "backends-filtering"
	ModeBackendsForm      FooterMode = "backends-form"
	ModeBackendsConfirm   FooterMode = "backends-confirm"

	ModeServerRunning FooterMode = "server-running"
	ModeServerConfirm FooterMode = "server-confirm"
	ModeServerHistory FooterMode = "server-history"
)

// modeHints maps each mode to its compact hint string (max 5 tokens).
// The status bar appends " ?" for full help automatically.
var modeHints = map[FooterMode]string{
	ModeProfilesSelected:  "enter:launch E:edit n:new d:dup /:filter",
	ModeProfilesFiltering: "↑↓:nav enter:apply esc:cancel",
	ModeProfilesEditing:   "ctrl+t:cycle ctrl+p:pick esc:cancel",
	ModeProfilesPicker:    "↑↓:move enter:pick esc:cancel",
	ModeProfilesConfirm:   "←→:choose enter:confirm esc:cancel",

	ModeModelsSelected:  "enter:actions /:filter R:rescan s:search i:info",
	ModeModelsFiltering: "type:filter esc:clear",
	ModeModelsPicker:    "↑↓:move enter:select esc:cancel",
	ModeModelsConfirm:   "enter:confirm esc:cancel",
	ModeModelsAction:    "↑↓:move enter:select esc:cancel",
	ModeModelsInfo:      "→/g:sizing esc:close",

	ModeBackendsSelected:  "e:edit n:new x:del D:default /:filter",
	ModeBackendsFiltering: "↑↓:nav enter:apply esc:cancel",
	ModeBackendsForm:      "enter:submit esc:cancel",
	ModeBackendsConfirm:   "←→:choose enter:confirm esc:cancel",

	ModeServerRunning: "v:cycle Space:pause k:kill r:restart H:history",
	ModeServerConfirm: "←→:choose enter:confirm esc:cancel",
	ModeServerHistory: "1:1h 2:6h 3:24h 4:7d esc:close",
}

// StatusBar renders the bottom-of-screen line with hints and the latest message.
type StatusBar struct {
	Hints        string
	Mode         FooterMode
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
	if s.Mode != "" {
		return s.renderMode(width)
	}
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

// renderMode draws a compact footer when the page has declared a mode.
// It shows up to 5 mode-specific hints plus " ?" for full help, omitting
// the global prefix to save space.
func (s StatusBar) renderMode(width int) string {
	hints := modeHints[s.Mode]
	if hints == "" {
		hints = string(s.Mode)
	}
	hints += "  ?"
	msg := s.styledMessage()
	badge := s.restartBadge()
	avail := width - lipgloss.Width(msg) - lipgloss.Width(badge) - 1
	if avail > 0 && lipgloss.Width(theme.Subtitle.Render(hints)) > avail {
		hints = truncateString(hints, avail)
	}
	styled := theme.Subtitle.Render(hints)
	total := lipgloss.Width(styled) + lipgloss.Width(msg) + lipgloss.Width(badge)
	gap := width - total
	if gap < 1 {
		gap = 1
	}
	return styled + strings.Repeat(" ", gap) + badge + msg
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

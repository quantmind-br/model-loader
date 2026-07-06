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
	badge := s.restartBadge()
	bw := lipgloss.Width(badge)
	// The flash message is also shown in full as an in-body banner, so when
	// space is tight sacrifice its status-bar echo BEFORE the page key-hints —
	// the hints are what the user actually needs to act (TUI_AUDIT F-04). Only
	// if the hints alone still overflow do we truncate the page hints, always
	// keeping the global hints.
	msgText := s.Message
	gap := width - gw - separatorWidth - pw - lipgloss.Width(s.styledMessage()) - bw
	if gap < 1 {
		// Step 1: reclaim space from the message echo (truncate, then drop).
		if avail := width - gw - separatorWidth - pw - bw - 1; avail > 0 {
			msgText = truncateString(msgText, avail)
		} else {
			msgText = ""
		}
		// Step 2: if the hints alone still don't fit, truncate the page
		// hints at the last complete action boundary so the visible
		// portion is always actionable (TUI_AUDIT R-08).
		gap = width - gw - separatorWidth - pw - lipgloss.Width(s.styledMessageText(msgText)) - bw
		if gap < 1 {
			if avail := width - gw - separatorWidth - bw - lipgloss.Width(s.styledMessageText(msgText)) - 1; avail > 0 {
				pagePart = truncateAtActionBoundary(pagePart, avail)
			} else {
				pagePart = ""
			}
			gap = 1
		}
	}
	hints := theme.Subtitle.Render(globalPart)
	if pagePart != "" {
		hints += theme.Subtitle.Render(" | " + pagePart)
	}
	return hints + strings.Repeat(" ", gap) + badge + s.styledMessageText(msgText)
}

// truncateAtActionBoundary truncates s to fit max visual cells while
// landing on a complete action token (delimited by "] " or end of string)
// so visible hints are always fully readable.
func truncateAtActionBoundary(s string, max int) string {
	if lipgloss.Width(s) <= max {
		return s
	}
	runes := []rune(s)
	if max <= 1 {
		return "…"
	}
	// Walk back from max to find the last "] " boundary.
	cut := max - 1
	if cut > len(runes) {
		cut = len(runes)
	}
	for cut > 0 {
		// Found a boundary: character before current position is ']'
		if cut >= 2 && runes[cut-1] == ']' && runes[cut] == ' ' {
			// Include the closing bracket, drop the trailing space
			result := string(runes[:cut])
			if lipgloss.Width(result) <= max {
				return result + "…"
			}
		}
		// Also accept end of a token like "]" at string boundary
		if cut >= 1 && runes[cut-1] == ']' {
			result := string(runes[:cut])
			if lipgloss.Width(result) <= max {
				return result + "…"
			}
		}
		cut--
	}
	// Fallback: simple truncation
	return string(runes[:max-1]) + "…"
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
	return s.styledMessageText(s.Message)
}

// styledMessageText applies the bar's level styling to an arbitrary text. It
// exists so Render can style a *truncated* copy of the message without
// slicing the already-styled string (which would corrupt ANSI escapes).
func (s StatusBar) styledMessageText(text string) string {
	if text == "" {
		return ""
	}
	switch s.Level {
	case StatusError:
		return theme.Error.Render(text)
	case StatusWarn:
		return theme.Warn.Render(text)
	default:
		return theme.Subtitle.Render(text)
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

package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestStatusBarRenderTruncatesPageHintsToWidth(t *testing.T) {
	bar := StatusBar{
		Hints:   "[q] quit  [?] help | [/] filter  [R] rescan  [s] search Hugging Face  [enter] actions  [i] info",
		Message: "ready",
	}

	got := bar.Render(60)
	if width := lipgloss.Width(got); width > 60 {
		t.Fatalf("rendered width = %d, want <= 60: %q", width, got)
	}
	if !strings.Contains(got, "[q] quit  [?] help") {
		t.Fatalf("global hints were not preserved: %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("page hints were not truncated with ellipsis: %q", got)
	}
}

// TestStatusBarRenderKeepsHintsOverMessageEcho is a regression for TUI_AUDIT
// F-04: when a long flash message competes with the page key-hints, the hints
// must survive (the message is also shown in full as an in-body banner). The
// message echo is the part sacrificed first.
func TestStatusBarRenderKeepsHintsOverMessageEcho(t *testing.T) {
	bar := StatusBar{
		Hints:   "[q] quit  [?] help | [s] start  [x] stop  [v] cycle",
		Message: "history: no metrics directory configured for the selected instance right now",
		Level:   StatusError,
	}

	got := bar.Render(80)
	if width := lipgloss.Width(got); width > 80 {
		t.Fatalf("rendered width = %d, want <= 80: %q", width, got)
	}
	// All page hints survive intact (they fit once the message echo yields).
	for _, hint := range []string{"[s] start", "[x] stop", "[v] cycle"} {
		if !strings.Contains(got, hint) {
			t.Errorf("page hint %q was dropped; hints must outrank the message echo: %q", hint, got)
		}
	}
	// The message echo is the part truncated under pressure.
	if strings.Contains(got, "right now") {
		t.Errorf("message echo should have been truncated, but rendered in full: %q", got)
	}
}

func TestStatusBarRenderModeFits80Cols(t *testing.T) {
	modes := []FooterMode{
		ModeProfilesSelected,
		ModeProfilesFiltering,
		ModeProfilesEditing,
		ModeProfilesPicker,
		ModeProfilesConfirm,
		ModeModelsSelected,
		ModeModelsFiltering,
		ModeModelsPicker,
		ModeModelsConfirm,
		ModeModelsAction,
		ModeModelsInfo,
		ModeBackendsSelected,
		ModeBackendsFiltering,
		ModeBackendsForm,
		ModeBackendsConfirm,
		ModeServerRunning,
		ModeServerConfirm,
		ModeServerHistory,
	}
	for _, mode := range modes {
		bar := StatusBar{Mode: mode, Message: "ok"}
		got := bar.Render(80)
		if width := lipgloss.Width(got); width > 80 {
			t.Errorf("mode %q rendered width = %d, want <= 80: %q", mode, width, got)
		}
		if !strings.Contains(got, "?") {
			t.Errorf("mode %q missing help token '?'; got %q", mode, got)
		}
	}
}

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

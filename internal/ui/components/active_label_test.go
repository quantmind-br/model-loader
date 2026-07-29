package components

import (
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

func TestActiveLabel_ColorlessFallback(t *testing.T) {
	t.Cleanup(theme.RebuildStyles)
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()

	if got := ActiveLabel("Logs", true); !strings.Contains(got, "[Logs]") {
		t.Fatalf("active colorless label = %q, want brackets", got)
	}
	if got := ActiveLabel("Slots", false); strings.Contains(got, "[") {
		t.Fatalf("inactive colorless label = %q, must not have brackets", got)
	}
}

func TestActiveLabel_ColorMode(t *testing.T) {
	t.Cleanup(theme.RebuildStyles)
	t.Setenv("NO_COLOR", "")
	theme.RebuildStyles()

	if got, want := ActiveLabel("Logs", true), theme.TabActive.Render("Logs"); got != want {
		t.Fatalf("active label = %q, want %q", got, want)
	}
}

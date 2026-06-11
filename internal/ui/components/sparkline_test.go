package components

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

func TestSparkline_EmptyReturnsBlanks(t *testing.T) {
	got := Sparkline(nil, 5)
	if got != "     " {
		t.Fatalf("empty sparkline = %q, want 5 spaces", got)
	}
}

func TestSparkline_ScalesToWidth(t *testing.T) {
	// 10 samples into 5 columns: each column averages 2 samples → bucket avgs [0,1,2,3,4].
	// Normalized idx = int((v-min)/rng * 7): 0→0(▁), 1→1.75→1(▂), 2→3.5→3(▄), 3→5.25→5(▆), 4→7(█).
	got := Sparkline([]float64{0, 0, 1, 1, 2, 2, 3, 3, 4, 4}, 5)
	if got != "▁▂▄▆█" {
		t.Fatalf("sparkline = %q, want %q", got, "▁▂▄▆█")
	}
}

func TestSparkline_FlatLineUsesLowestBar(t *testing.T) {
	got := Sparkline([]float64{1, 1, 1, 1, 1}, 5)
	if got != "▁▁▁▁▁" {
		t.Fatalf("flat sparkline = %q", got)
	}
}

// Under NO_COLOR the Unicode block ramp degrades to the ASCII ramp.
func TestSparkline_ASCIIRampUnderNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	defer func() {
		t.Setenv("NO_COLOR", "")
		theme.RebuildStyles()
	}()

	got := Sparkline([]float64{0, 0, 1, 1, 2, 2, 3, 3, 4, 4}, 5)
	if got != "_.-+#" {
		t.Fatalf("NO_COLOR sparkline = %q, want %q", got, "_.-+#")
	}
	if flat := Sparkline([]float64{1, 1, 1}, 3); flat != "___" {
		t.Fatalf("NO_COLOR flat sparkline = %q, want %q", flat, "___")
	}
}

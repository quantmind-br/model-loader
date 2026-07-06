package theme

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestBodyHeight(t *testing.T) {
	if h := BodyHeight(24); h != 22 {
		t.Errorf("BodyHeight(24) = %d, want 22", h)
	}
	if h := BodyHeight(2); h != 0 {
		t.Errorf("BodyHeight(2) = %d, want 0", h)
	}
	if h := BodyHeight(0); h != 0 {
		t.Errorf("BodyHeight(0) = %d, want 0", h)
	}
	if h := BodyHeight(1); h != 0 {
		t.Errorf("BodyHeight(1) = %d, want 0", h)
	}
}


func TestResponsiveSplit(t *testing.T) {
	tests := []struct {
		name      string
		width     int
		wantMode  LayoutMode
		wantLeft  int
		wantRight int
		labelsMin int
	}{
		{"60col stacked", 60, LayoutStacked, 58, 58, 1},
		{"80col stacked", 80, LayoutStacked, 78, 78, 1},
		{"120col 60_40", 120, LayoutSplit6040, 70, 48, MinPaneWidth},
		{"160col 60_40 boundary", 160, LayoutSplit6040, 94, 64, MinPaneWidth},
		{"200col 50_50", 200, LayoutSplit5050, 99, 99, MinPaneWidth},
		{"0col stacked min", 0, LayoutStacked, 1, 1, 1},
		{"narrow tiny min", 25, LayoutStacked, 23, 23, 1},
		{"narrow below min", 18, LayoutStacked, 16, 16, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, l, r := ResponsiveSplit(tt.width)
			if mode != tt.wantMode {
				t.Errorf("ResponsiveSplit(%d) mode = %v, want %v", tt.width, mode, tt.wantMode)
			}
			if l != tt.wantLeft {
				t.Errorf("ResponsiveSplit(%d) leftWidth = %d, want %d", tt.width, l, tt.wantLeft)
			}
			if r != tt.wantRight {
				t.Errorf("ResponsiveSplit(%d) rightWidth = %d, want %d", tt.width, r, tt.wantRight)
			}
			if l < tt.labelsMin || r < tt.labelsMin {
				t.Errorf("ResponsiveSplit(%d) pane widths (%d,%d) below min %d", tt.width, l, r, tt.labelsMin)
			}
		})
	}
}


func TestClampBodyTruncatesNotWraps(t *testing.T) {
	long := strings.Repeat("x", 200)
	in := long + "\nsecond"
	out := ClampBody(in, 40, 5)
	lines := strings.Split(out, "\n")
	if len(lines) != 5 {
		t.Fatalf("ClampBody lines = %d, want 5", len(lines))
	}
	if lipgloss.Width(lines[0]) > 40 {
		t.Errorf("line0 width = %d, want <= 40", lipgloss.Width(lines[0]))
	}
	if !strings.Contains(lines[1], "second") {
		t.Errorf("line1 = %q, want to contain second (long line must not wrap)", lines[1])
	}
}

package theme

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

func TestClampBodyAddsANSISafeEllipsis(t *testing.T) {
	plain := strings.Split(ClampBody(strings.Repeat("x", 200), 40, 3), "\n")
	if !strings.HasSuffix(ansi.Strip(plain[0]), "…") || lipgloss.Width(plain[0]) > 40 {
		t.Fatalf("plain truncation = %q", plain[0])
	}
	if got := len(plain); got != 3 {
		t.Fatalf("plain lines = %d, want 3", got)
	}

	fitting := strings.Split(ClampBody("short", 40, 3), "\n")
	if strings.Contains(fitting[0], "…") || len(fitting) != 3 {
		t.Fatalf("fitting truncation = %q", fitting)
	}

	styled := strings.Split(ClampBody(Error.Render(strings.Repeat("x", 200)), 40, 3), "\n")
	if lipgloss.Width(styled[0]) > 40 || !strings.HasSuffix(ansi.Strip(styled[0]), "…") || strings.ContainsRune(ansi.Strip(styled[0]), '\x1b') {
		t.Fatalf("styled truncation is invalid: %q", styled[0])
	}
	if got := len(styled); got != 3 {
		t.Fatalf("styled lines = %d, want 3", got)
	}

	// Literal SGR input: profile-independent, so this actually proves the
	// opening sequence and the trailing reset survive truncation.
	raw := "\x1b[31m" + strings.Repeat("y", 200) + "\x1b[0m"
	sgr := strings.Split(ClampBody(raw, 40, 3), "\n")
	if lipgloss.Width(sgr[0]) > 40 {
		t.Fatalf("sgr width = %d, want <= 40: %q", lipgloss.Width(sgr[0]), sgr[0])
	}
	if !strings.Contains(sgr[0], "\x1b[31m") || !strings.Contains(sgr[0], "\x1b[0m") {
		t.Fatalf("truncation dropped an SGR sequence: %q", sgr[0])
	}
	if stripped := ansi.Strip(sgr[0]); !strings.HasSuffix(stripped, "…") || strings.ContainsRune(stripped, '\x1b') {
		t.Fatalf("sgr truncation left a sliced escape or lost the tail: %q", sgr[0])
	}

	// Wide (2-cell) graphemes must not straddle the cut and must not become
	// U+FFFD. ansi.Truncate uses GraphemeWidth, so no TruncateWc swap is needed.
	wide := strings.Split(ClampBody(strings.Repeat("宽", 100), 41, 3), "\n")
	if lipgloss.Width(wide[0]) > 41 {
		t.Fatalf("wide width = %d, want <= 41: %q", lipgloss.Width(wide[0]), wide[0])
	}
	if strings.ContainsRune(wide[0], '\uFFFD') || !strings.HasSuffix(ansi.Strip(wide[0]), "…") {
		t.Fatalf("wide truncation is invalid: %q", wide[0])
	}
}

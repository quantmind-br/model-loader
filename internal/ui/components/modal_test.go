package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

func TestModal_RendersTitleAndBody(t *testing.T) {
	out := Modal("Help", "Body content here", 80, 24)
	if !strings.Contains(out, "Help") {
		t.Errorf("missing title in output:\n%s", out)
	}
	if !strings.Contains(out, "Body content here") {
		t.Errorf("missing body in output:\n%s", out)
	}
}

func TestModal_FillsViewport(t *testing.T) {
	out := Modal("T", "B", 40, 10)
	// Lipgloss.Place fills with the given dimensions; the rendered string
	// should have at least 9 newlines (for 10 lines).
	if strings.Count(out, "\n") < 9 {
		t.Errorf("expected viewport-filling output; got %d newlines:\n%s", strings.Count(out, "\n"), out)
	}
}

func TestModal_NoSizePassesThrough(t *testing.T) {
	// width/height = 0 -> raw box, no Place wrapper.
	out := Modal("T", "B", 0, 0)
	if strings.Count(out, "\n") > 6 {
		t.Errorf("expected compact box; got %d newlines:\n%s", strings.Count(out, "\n"), out)
	}
}

func TestModal_TitleDistinctFromBody(t *testing.T) {
	out := Modal("Title", "Body", 0, 0)
	if !strings.Contains(out, "Title") {
		t.Errorf("missing title in output:\n%s", out)
	}
	if !strings.Contains(out, "Body") {
		t.Errorf("missing body in output:\n%s", out)
	}
	if theme.NoColor() {
		return
	}
	if !strings.Contains(out, "\x1b[") {
		return
	}
	if !containsUnderlineSGR(out) {
		t.Error("expected underline on title")
	}
}

func TestModal_SmallTerminalClamping(t *testing.T) {
	out := Modal("T", "B", 3, 3)
	if !strings.Contains(out, "T") {
		t.Errorf("missing title in small terminal output:\n%s", out)
	}
}

func TestModal_NoBackgroundEscape(t *testing.T) {
	// modalBox no longer hardcodes a background color — the terminal bg
	// shows through. Verify there is no ANSI background escape (\x1b[4X).
	out := Modal("Title", "Body", 0, 0)
	if strings.Contains(out, "\x1b[48;") || strings.Contains(out, "\x1b[4") {
		// "\x1b[4" alone is fine for "underline" (\x1b[4m). Be precise:
		// only flag SGR background codes \x1b[4Xm where X is 0-7 or 8.
		for _, code := range []string{"\x1b[40m", "\x1b[41m", "\x1b[42m", "\x1b[43m", "\x1b[44m", "\x1b[45m", "\x1b[46m", "\x1b[47m", "\x1b[48;"} {
			if strings.Contains(out, code) {
				t.Errorf("modal output unexpectedly contains background SGR %q:\n%q", code, out)
			}
		}
	}
}

// TestModal_NoColorStripsForeground guarantees that under NO_COLOR the modal
// title + box emit no foreground or background SGR escapes. Regression for
// Codex finding: package-level styles ignored NO_COLOR.
func TestModal_NoColorStripsForeground(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	t.Cleanup(func() {
		t.Setenv("NO_COLOR", "")
		theme.RebuildStyles()
	})

	out := Modal("Title", "Body", 0, 0)
	for i := 30; i <= 47; i++ {
		code := "\x1b[" + itoa(i) + "m"
		if strings.Contains(out, code) {
			t.Errorf("modal output contains color SGR %q under NO_COLOR:\n%q", code, out)
		}
	}
	if strings.Contains(out, "\x1b[38;") || strings.Contains(out, "\x1b[48;") {
		t.Errorf("modal output contains 256/truecolor SGR under NO_COLOR:\n%q", out)
	}
}

// TestModal_RectangularBorderWithWideGlyphs guarantees the bordered box stays
// a clean rectangle even when the title and body contain East-Asian
// ambiguous-width glyphs (↑ ↓ · → –). Regression for the ragged help-overlay
// right border: every rendered line must share one display width.
func TestModal_RectangularBorderWithWideGlyphs(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	t.Cleanup(func() {
		t.Setenv("NO_COLOR", "")
		theme.RebuildStyles()
	})

	title := "Keybindings (↑/↓ · ? to toggle · esc to close)"
	body := "row one →\nEssentials → Advanced → Sizing\nshort"
	out := Modal(title, body, 0, 0)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected a multi-line box, got %d lines:\n%s", len(lines), out)
	}
	want := lipgloss.Width(lines[0])
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != want {
			t.Errorf("line %d display width %d != frame width %d (ragged border):\n%s", i, w, want, out)
		}
	}
}

func containsUnderlineSGR(s string) bool {
	return strings.Contains(s, "\x1b[4m") ||
		strings.Contains(s, "\x1b[4;") ||
		strings.Contains(s, ";4;") ||
		strings.Contains(s, ";4m")
}

// itoa avoids strconv import for a tiny fixed-range conversion.
func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

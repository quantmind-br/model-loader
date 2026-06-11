package components

import (
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

func TestTabBar_AllFit(t *testing.T) {
	labels := []string{"Tab1", "Tab2", "Tab3"}
	out := TabBar(TabBarOptions{
		Labels:         labels,
		ActiveIndex:    1,
		AvailableWidth: 80,
	})
	if strings.Contains(out, "‹") || strings.Contains(out, "›") {
		t.Errorf("all-fit should not have indicators: %q", out)
	}
	for _, label := range labels {
		if !strings.Contains(out, label) {
			t.Errorf("all-fit missing label %q in %q", label, out)
		}
	}
}

func TestTabBar_OverflowActiveLeftmost(t *testing.T) {
	labels := []string{"FirstTab", "SecondTab", "ThirdTab", "FourthTab", "FifthTab"}
	out := TabBar(TabBarOptions{
		Labels:         labels,
		ActiveIndex:    0,
		AvailableWidth: 25,
	})
	if strings.Contains(out, "‹") {
		t.Errorf("active-leftmost should not have left indicator: %q", out)
	}
	if !strings.Contains(out, "›") {
		t.Errorf("active-leftmost should have right indicator: %q", out)
	}
	if !strings.Contains(out, "FirstTab") {
		t.Errorf("missing active tab: %q", out)
	}
}

func TestTabBar_OverflowActiveRightmost(t *testing.T) {
	labels := []string{"FirstTab", "SecondTab", "ThirdTab", "FourthTab", "FifthTab"}
	out := TabBar(TabBarOptions{
		Labels:         labels,
		ActiveIndex:    4,
		AvailableWidth: 25,
	})
	if !strings.Contains(out, "‹") {
		t.Errorf("active-rightmost should have left indicator: %q", out)
	}
	if strings.Contains(out, "›") {
		t.Errorf("active-rightmost should not have right indicator: %q", out)
	}
	if !strings.Contains(out, "FifthTab") {
		t.Errorf("missing active tab: %q", out)
	}
}

func TestTabBar_OverflowActiveMiddle(t *testing.T) {
	labels := []string{"FirstTab", "SecondTab", "ThirdTab", "FourthTab", "FifthTab"}
	out := TabBar(TabBarOptions{
		Labels:         labels,
		ActiveIndex:    2,
		AvailableWidth: 28,
	})
	if !strings.Contains(out, "‹") {
		t.Errorf("active-middle should have left indicator: %q", out)
	}
	if !strings.Contains(out, "›") {
		t.Errorf("active-middle should have right indicator: %q", out)
	}
	if !strings.Contains(out, "ThirdTab") {
		t.Errorf("missing active tab: %q", out)
	}
}

func TestTabBar_SingleTabWiderThanViewport(t *testing.T) {
	labels := []string{"VeryLongTabNameIndeed"}
	out := TabBar(TabBarOptions{
		Labels:         labels,
		ActiveIndex:    0,
		AvailableWidth: 10,
	})
	if !strings.Contains(out, "VeryLon") {
		t.Errorf("should still contain active tab prefix (possibly truncated by lipgloss): %q", out)
	}
	if strings.Contains(out, "\n") {
		t.Errorf("oversized single tab must still render on one row: %q", out)
	}
}

func TestTabBar_EmptyLabels(t *testing.T) {
	out := TabBar(TabBarOptions{
		Labels:         nil,
		ActiveIndex:    0,
		AvailableWidth: 80,
	})
	if out != "" {
		t.Errorf("empty labels should produce empty output, got %q", out)
	}
}

func TestTabBar_ZeroWidth(t *testing.T) {
	out := TabBar(TabBarOptions{
		Labels:         []string{"A", "B"},
		ActiveIndex:    0,
		AvailableWidth: 0,
	})
	if out != "" {
		t.Errorf("zero width should produce empty output, got %q", out)
	}
}

func TestTabBar_SingleRow(t *testing.T) {
	labels := []string{"FirstTab", "SecondTab", "ThirdTab", "FourthTab", "FifthTab"}
	out := TabBar(TabBarOptions{
		Labels:         labels,
		ActiveIndex:    2,
		AvailableWidth: 28,
	})
	if strings.Contains(out, "\n") {
		t.Errorf("tab bar must render on a single row, got: %q", out)
	}
}

func TestTabBar_EachLabelOnce(t *testing.T) {
	labels := []string{"Profiles", "Server", "Models", "Backends"}
	for _, width := range []int{80, 200} {
		for activeIdx := range labels {
			out := TabBar(TabBarOptions{
				Labels:         labels,
				ActiveIndex:    activeIdx,
				AvailableWidth: width,
			})
			for _, label := range labels {
				if strings.Count(out, label) != 1 {
					t.Errorf("width=%d active=%d: label %q appears %d times, want exactly 1; out=%q",
						width, activeIdx, label, strings.Count(out, label), out)
				}
			}
		}
	}
}

// Under NO_COLOR the active tab is bracketed and the scroll indicators
// default to ASCII "<"/">" since color alone can't carry the distinction.
func TestTabBar_ASCIIFallbacksUnderNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	defer func() {
		t.Setenv("NO_COLOR", "")
		theme.RebuildStyles()
	}()

	out := TabBar(TabBarOptions{
		Labels:         []string{"Tab1", "Tab2", "Tab3"},
		ActiveIndex:    1,
		AvailableWidth: 80,
	})
	if !strings.Contains(out, "[Tab2]") {
		t.Errorf("NO_COLOR active tab not bracketed: %q", out)
	}

	overflow := TabBar(TabBarOptions{
		Labels:         []string{"FirstTab", "SecondTab", "ThirdTab", "FourthTab", "FifthTab"},
		ActiveIndex:    2,
		AvailableWidth: 25,
	})
	if !strings.Contains(overflow, "<") || !strings.Contains(overflow, ">") {
		t.Errorf("NO_COLOR overflow missing ASCII indicators: %q", overflow)
	}
	if strings.Contains(overflow, "‹") || strings.Contains(overflow, "›") {
		t.Errorf("NO_COLOR overflow still uses Unicode indicators: %q", overflow)
	}
}

// UIUX-021: a non-empty badge is appended after its tab label; empty badge
// entries leave their labels untouched.
func TestTabBar_BadgeAppendedAfterLabel(t *testing.T) {
	out := TabBar(TabBarOptions{
		Labels:         []string{"Tab1", "Tab2", "Tab3"},
		ActiveIndex:    0,
		AvailableWidth: 80,
		Badges:         []string{"", "●", ""},
	})
	if !strings.Contains(out, "Tab2 ●") {
		t.Errorf("badge not appended after Tab2: %q", out)
	}
	if strings.Contains(out, "Tab1 ●") || strings.Contains(out, "Tab3 ●") {
		t.Errorf("badge leaked onto unbadged tabs: %q", out)
	}
	if strings.Count(out, "●") != 1 {
		t.Errorf("badge glyph count = %d, want 1: %q", strings.Count(out, "●"), out)
	}
}

func TestTabBar_NoBadgesRendersPlainLabels(t *testing.T) {
	out := TabBar(TabBarOptions{
		Labels:         []string{"Tab1", "Tab2"},
		ActiveIndex:    0,
		AvailableWidth: 80,
		Badges:         []string{"", ""},
	})
	if strings.Contains(out, "●") {
		t.Errorf("unexpected badge glyph with empty badges: %q", out)
	}
	if !strings.Contains(out, "Tab1") || !strings.Contains(out, "Tab2") {
		t.Errorf("labels missing: %q", out)
	}
}

// Under NO_COLOR the badge is folded into the label before the active-tab
// bracket wrap, so it lands inside the brackets.
func TestTabBar_BadgeInsideActiveBracketsUnderNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	defer func() {
		t.Setenv("NO_COLOR", "")
		theme.RebuildStyles()
	}()

	out := TabBar(TabBarOptions{
		Labels:         []string{"Tab1", "Tab2"},
		ActiveIndex:    1,
		AvailableWidth: 80,
		Badges:         []string{"", "*"},
	})
	if !strings.Contains(out, "[Tab2 *]") {
		t.Errorf("NO_COLOR active badge not inside brackets: %q", out)
	}
}

package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func tabBarStyles() (lipgloss.Style, lipgloss.Style) {
	active := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	inactive := lipgloss.NewStyle().Padding(0, 1)
	return active, inactive
}

func TestTabBar_AllFit(t *testing.T) {
	active, inactive := tabBarStyles()
	labels := []string{"Tab1", "Tab2", "Tab3"}
	out := TabBar(TabBarOptions{
		Labels:         labels,
		ActiveIndex:    1,
		AvailableWidth: 80,
		ActiveStyle:    active,
		InactiveStyle:  inactive,
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
	active, inactive := tabBarStyles()
	labels := []string{"FirstTab", "SecondTab", "ThirdTab", "FourthTab", "FifthTab"}
	out := TabBar(TabBarOptions{
		Labels:         labels,
		ActiveIndex:    0,
		AvailableWidth: 25,
		ActiveStyle:    active,
		InactiveStyle:  inactive,
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
	active, inactive := tabBarStyles()
	labels := []string{"FirstTab", "SecondTab", "ThirdTab", "FourthTab", "FifthTab"}
	out := TabBar(TabBarOptions{
		Labels:         labels,
		ActiveIndex:    4,
		AvailableWidth: 25,
		ActiveStyle:    active,
		InactiveStyle:  inactive,
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
	active, inactive := tabBarStyles()
	labels := []string{"FirstTab", "SecondTab", "ThirdTab", "FourthTab", "FifthTab"}
	out := TabBar(TabBarOptions{
		Labels:         labels,
		ActiveIndex:    2,
		AvailableWidth: 28,
		ActiveStyle:    active,
		InactiveStyle:  inactive,
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
	active, inactive := tabBarStyles()
	labels := []string{"VeryLongTabNameIndeed"}
	out := TabBar(TabBarOptions{
		Labels:         labels,
		ActiveIndex:    0,
		AvailableWidth: 10,
		ActiveStyle:    active,
		InactiveStyle:  inactive,
	})
	if !strings.Contains(out, "VeryLon") {
		t.Errorf("should still contain active tab prefix (possibly truncated by lipgloss): %q", out)
	}
	if strings.Contains(out, "\n") {
		t.Errorf("oversized single tab must still render on one row: %q", out)
	}
}

func TestTabBar_EmptyLabels(t *testing.T) {
	active, inactive := tabBarStyles()
	out := TabBar(TabBarOptions{
		Labels:         nil,
		ActiveIndex:    0,
		AvailableWidth: 80,
		ActiveStyle:    active,
		InactiveStyle:  inactive,
	})
	if out != "" {
		t.Errorf("empty labels should produce empty output, got %q", out)
	}
}

func TestTabBar_ZeroWidth(t *testing.T) {
	active, inactive := tabBarStyles()
	out := TabBar(TabBarOptions{
		Labels:         []string{"A", "B"},
		ActiveIndex:    0,
		AvailableWidth: 0,
		ActiveStyle:    active,
		InactiveStyle:  inactive,
	})
	if out != "" {
		t.Errorf("zero width should produce empty output, got %q", out)
	}
}

func TestTabBar_SingleRow(t *testing.T) {
	active, inactive := tabBarStyles()
	labels := []string{"FirstTab", "SecondTab", "ThirdTab", "FourthTab", "FifthTab"}
	out := TabBar(TabBarOptions{
		Labels:         labels,
		ActiveIndex:    2,
		AvailableWidth: 28,
		ActiveStyle:    active,
		InactiveStyle:  inactive,
	})
	if strings.Contains(out, "\n") {
		t.Errorf("tab bar must render on a single row, got: %q", out)
	}
}

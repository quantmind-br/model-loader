package components

import (
	"strings"
	"testing"
)

func TestMetricBar_FillProportional(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	got := MetricBar(0.5, 10)
	if n := strings.Count(got, "█"); n != 5 {
		t.Fatalf("frac 0.5 width 10 = %d filled blocks, want 5 (%q)", n, got)
	}
}

func TestMetricBar_ClampsAndZeroWidth(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if MetricBar(2.0, 4) != strings.Repeat("█", 4) {
		t.Fatalf("frac>1 should clamp to full")
	}
	if MetricBar(0.5, 0) != "" {
		t.Fatalf("width 0 should return empty string")
	}
}

func TestMetricBar_NoColorFallback(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if got := MetricBar(0.5, 4); got != "##--" {
		t.Fatalf("NO_COLOR frac 0.5 width 4 = %q, want %q", got, "##--")
	}
}

package pages

import (
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

func TestBenchModesIncludeInstructionBench(t *testing.T) {
	found := false
	for _, m := range benchModes {
		if m == benchmark.ModeInstBench {
			found = true
		}
	}
	if !found {
		t.Fatal("benchModes must include ModeInstBench")
	}
}

func TestBenchModesIncludeMMLUBench(t *testing.T) {
	found := false
	for _, m := range benchModes {
		if m == benchmark.ModeMMLUBench {
			found = true
		}
	}
	if !found {
		t.Fatal("benchModes must include ModeMMLUBench")
	}
}

func TestBenchModesGroupedByCategory(t *testing.T) {
	// Once a category appears it must not reappear later (modes are contiguous
	// per category so the picker can show one header each).
	seen := map[benchmark.Category]bool{}
	var last benchmark.Category
	for i, m := range benchModes {
		c, ok := benchmark.CategoryOf(m)
		if !ok {
			t.Fatalf("mode %q has no category", m)
		}
		if i == 0 || c != last {
			if seen[c] {
				t.Fatalf("category %q is not contiguous in benchModes", c)
			}
			seen[c] = true
			last = c
		}
	}
}

func TestViewModePickShowsCategoryHeaders(t *testing.T) {
	p := BenchmarkPage{view: bvModePick, runningName: "demo"}
	out := p.viewModePick()
	for _, h := range []string{"Quality", "Speed", "Robustness", "Knowledge"} {
		if !strings.Contains(out, h) {
			t.Fatalf("viewModePick output missing category header %q", h)
		}
	}
}

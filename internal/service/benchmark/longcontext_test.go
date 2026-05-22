package benchmark

import (
	"strings"
	"testing"
)

func TestBuildNeedles_AreUniquePerCall(t *testing.T) {
	a := buildNeedles()
	b := buildNeedles()
	if len(a) != 3 {
		t.Fatalf("want 3 needles, got %d", len(a))
	}
	same := true
	for i := range a {
		if a[i].value != b[i].value {
			same = false
		}
	}
	if same {
		t.Error("needles identical across calls; expected randomization")
	}
}

func TestBuildMultiNeedleHaystack_EmbedsAllNeedles(t *testing.T) {
	needles := []needle{{label: "alpha", value: "Reykjavik-1234"}, {label: "beta", value: "Oslo-5678"}, {label: "gamma", value: "Lima-9012"}}
	hay := buildMultiNeedleHaystack(4000, needles)
	for _, n := range needles {
		if !strings.Contains(hay, n.value) {
			t.Errorf("haystack missing needle %q", n.value)
		}
	}
}

func TestScoreNeedles_FractionFound(t *testing.T) {
	needles := []needle{{value: "A1"}, {value: "B2"}, {value: "C3"}}
	got := scoreNeedles("the values are A1 and C3", needles)
	if got != 2.0/3.0 {
		t.Errorf("scoreNeedles = %v, want 0.6667", got)
	}
}

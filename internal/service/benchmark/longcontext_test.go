package benchmark

import (
	"strings"
	"testing"
)

func TestBuildNeedles_VaryAcrossSeeds(t *testing.T) {
	a := buildNeedles(1)
	b := buildNeedles(2)
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
		t.Error("needles identical across different seeds; expected randomization")
	}
}

func TestBuildNeedles_DeterministicForSeed(t *testing.T) {
	a := buildNeedles(42)
	b := buildNeedles(42)
	for i := range a {
		if a[i].value != b[i].value {
			t.Fatalf("needle %d differs for same seed: %q vs %q", i, a[i].value, b[i].value)
		}
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

func TestScoreNeedles_ToleratesSeparatorVariants(t *testing.T) {
	needles := []needle{{value: "Reykjavik-1042"}, {value: "Oslo-5678"}, {value: "Lima-9012"}}
	resp := "Found: Reykjavik - 1042, OSLO–5678 and lima 9012."
	if got := scoreNeedles(resp, needles); got != 1.0 {
		t.Errorf("scoreNeedles = %v, want 1.0 (separator/case variants must match)", got)
	}
}

func TestBuildNeedles_ValuesAreUnique(t *testing.T) {
	for trial := 0; trial < 1000; trial++ {
		ns := buildNeedles(int64(trial))
		seen := map[string]bool{}
		for _, n := range ns {
			if seen[n.value] {
				t.Fatalf("duplicate needle value %q in a single call", n.value)
			}
			seen[n.value] = true
		}
	}
}

func TestBuildQualityHaystack_EmbedsNeedlesAmongAbstracts(t *testing.T) {
	docs := []ArxivDoc{
		{ID: "1", Title: "On Sparse Attention", Abstract: "We study sparse attention mechanisms for long sequences. " + strings.Repeat("Empirical results show consistent gains. ", 30)},
		{ID: "2", Title: "Quantization Survey", Abstract: "A survey of post-training quantization for transformers. " + strings.Repeat("We compare many schemes carefully. ", 30)},
	}
	needles := []needle{{label: "alpha", value: "Reykjavik-1234"}, {label: "beta", value: "Oslo-5678"}, {label: "gamma", value: "Lima-9012"}}
	hay := buildQualityHaystack(docs, 4000, needles)
	for _, n := range needles {
		if !strings.Contains(hay, n.value) {
			t.Errorf("haystack missing needle %q", n.value)
		}
	}
	if !strings.Contains(hay, "sparse attention") && !strings.Contains(hay, "Sparse Attention") {
		t.Error("haystack should contain real abstract text as filler")
	}
}

func TestBuildQualityHaystack_FallsBackWhenNoDocs(t *testing.T) {
	needles := []needle{{label: "alpha", value: "A1"}, {label: "beta", value: "B2"}, {label: "gamma", value: "C3"}}
	hay := buildQualityHaystack(nil, 4000, needles)
	for _, n := range needles {
		if !strings.Contains(hay, n.value) {
			t.Errorf("fallback haystack missing needle %q", n.value)
		}
	}
}

package benchmark

import (
	"math"
	"strings"
	"testing"
)

func TestFactCoverage_CountsMentionedFacts(t *testing.T) {
	facts := []string{
		"The Merrowfield wind farm opened in 2028.",
		"It has 64 turbines arranged in four rows.",
		"Peak output is 410 megawatts.",
	}
	summary := "Merrowfield opened in 2028 with 64 turbines; peak output reaches 410 megawatts."
	got := factCoverage(summary, facts)
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("factCoverage = %v, want 1.0", got)
	}
}

func TestFactCoverage_PartialMiss(t *testing.T) {
	facts := []string{"Output is 410 megawatts.", "Cost was 1.2 billion."}
	summary := "The output is 410 megawatts."
	got := factCoverage(summary, facts)
	if math.Abs(got-0.5) > 1e-9 {
		t.Errorf("factCoverage = %v, want 0.5", got)
	}
}

func TestSummaryDetail_RoundTrip(t *testing.T) {
	d := summaryDetail(4, 5, 0.85, "self")
	cov, coh, ok := parseSummaryScores(d)
	if !ok {
		t.Fatalf("parse failed on %q", d)
	}
	if math.Abs(cov-0.8) > 1e-9 || math.Abs(coh-0.85) > 1e-9 {
		t.Errorf("round-trip: cov=%v coh=%v", cov, coh)
	}
}

func TestBuildSummaryPrompt_IncludesAllDocs(t *testing.T) {
	p := SummaryProblem{Documents: []string{"AAA.", "BBB.", "CCC."}}
	got := buildSummaryPrompt(p)
	for _, d := range p.Documents {
		if !strings.Contains(got, d) {
			t.Errorf("prompt missing %q", d)
		}
	}
}

func TestSummaryFinalize_MeanScore(t *testing.T) {
	problems := []ProblemResult{
		{Score: 0.8, Detail: summaryDetail(4, 5, 0.9, "self")},
		{Score: 0.4, Detail: summaryDetail(2, 5, 0.5, "self")},
		{Score: 0.0, Detail: "no scores here"}, // ignored (unparseable)
	}
	var agg Aggregate
	summaryHandler{}.Finalize(&agg, problems)
	if math.Abs(agg.SummaryCoherence-0.7) > 1e-9 {
		t.Errorf("SummaryCoherence = %v, want 0.7", agg.SummaryCoherence)
	}
}

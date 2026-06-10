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
		{Score: 0.8, SubScores: map[string]float64{"coverage": 0.8, "coherence": 0.9}},
		{Score: 0.4, SubScores: map[string]float64{"coverage": 0.4, "coherence": 0.5}},
		{Score: 0.0, Detail: "no scores here"}, // ignored (nil SubScores)
	}
	var agg Aggregate
	summaryHandler{}.Finalize(&agg, problems)
	if math.Abs(agg.SummaryCoherence-0.7) > 1e-9 {
		t.Errorf("SummaryCoherence = %v, want 0.7", agg.SummaryCoherence)
	}
}

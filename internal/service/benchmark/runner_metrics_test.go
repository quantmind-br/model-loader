package benchmark

import "testing"

func TestAggregate_RollsUpPrefillDecode(t *testing.T) {
	results := []ProblemResult{
		{Resolved: true, PromptProcessingTPS: 100, DecodeTPS: 40, TokensPerSecond: 40, TTFTms: 10},
		{Resolved: true, PromptProcessingTPS: 200, DecodeTPS: 60, TokensPerSecond: 60, TTFTms: 20},
	}
	a := aggregate(results, 0, 0)
	if a.AvgPromptProcessingTPS != 150 {
		t.Errorf("AvgPromptProcessingTPS = %v, want 150", a.AvgPromptProcessingTPS)
	}
	if a.AvgDecodeTPS != 50 {
		t.Errorf("AvgDecodeTPS = %v, want 50", a.AvgDecodeTPS)
	}
}

func TestAggregate_ExcludesErroredFromQualityRates(t *testing.T) {
	results := []ProblemResult{
		{Resolved: true, Score: 1},
		{Resolved: false, Score: 0.5},
		{Err: "judge request: status 502"}, // infrastructure failure
		{Err: "context deadline exceeded"},
	}
	a := aggregate(results, 0, 0)
	if a.Total != 4 {
		t.Errorf("Total = %d, want 4", a.Total)
	}
	if a.Errored != 2 {
		t.Errorf("Errored = %d, want 2", a.Errored)
	}
	if a.SolveRate != 0.5 {
		t.Errorf("SolveRate = %v, want 0.5 (1 of 2 answered)", a.SolveRate)
	}
	if a.AvgScore != 0.75 {
		t.Errorf("AvgScore = %v, want 0.75 (over answered only)", a.AvgScore)
	}
}

func TestAggregate_AllErrored(t *testing.T) {
	results := []ProblemResult{{Err: "boom"}, {Err: "boom"}}
	a := aggregate(results, 0, 0)
	if a.SolveRate != 0 || a.AvgScore != 0 {
		t.Errorf("all-errored run should have zero rates, got solve=%v score=%v", a.SolveRate, a.AvgScore)
	}
	if a.Errored != 2 {
		t.Errorf("Errored = %d, want 2", a.Errored)
	}
}

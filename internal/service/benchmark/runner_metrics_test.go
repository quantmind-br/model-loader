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

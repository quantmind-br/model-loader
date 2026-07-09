package benchmark

import "testing"

func TestCeilingsFor_FallsBackToDefaults(t *testing.T) {
	c := CeilingsFor(nil, ModeMathBench)
	if c.TPS != defaultCeilingTPS || c.TTFTms != defaultCeilingTTFTms || c.VRAMMB != defaultCeilingVRAMMB {
		t.Errorf("no-peer ceilings = %+v, want defaults", c)
	}
}

func TestCeilingsFor_UsesMaxOfCompleteRuns(t *testing.T) {
	runs := []Run{
		{Mode: ModeMathBench, Aggregate: Aggregate{AvgTokensPerSecond: 200, AvgTTFTms: 500, PeakVRAMMB: 40000}},
		{Mode: ModeMathBench, Aggregate: Aggregate{AvgTokensPerSecond: 300, AvgTTFTms: 100, PeakVRAMMB: 10000}},
		// Partial run: excluded from the scale even though its numbers are huge.
		{Mode: ModeMathBench, Err: "in progress", Aggregate: Aggregate{AvgTokensPerSecond: 9999, PeakVRAMMB: 99999}},
		// Different mode: ignored.
		{Mode: ModeMMLUBench, Aggregate: Aggregate{AvgTokensPerSecond: 5000}},
	}
	c := CeilingsFor(runs, ModeMathBench)
	if c.TPS != 300*1.1 {
		t.Errorf("TPS ceiling = %v, want %v", c.TPS, 300*1.1)
	}
	if c.TTFTms != 500*1.1 {
		t.Errorf("TTFT ceiling = %v, want %v", c.TTFTms, 500*1.1)
	}
	if c.VRAMMB != 40000*1.1 {
		t.Errorf("VRAM ceiling = %v, want %v", c.VRAMMB, 40000*1.1)
	}
}

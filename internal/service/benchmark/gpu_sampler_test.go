package benchmark

import (
	"encoding/json"
	"testing"
	"time"
)

func TestServerTimingsRetainZeroAndDraftCounts(t *testing.T) {
	var chunk streamChunk
	if err := json.Unmarshal([]byte(`{"timings":{"cache_n":0,"draft_n":31,"draft_n_accepted":23,"predicted_per_second":42,"effective_settings":{"seed":0}}}`), &chunk); err != nil {
		t.Fatal(err)
	}
	result := buildResult(streamState{timings: chunk.Timings}, time.Now())
	if result.ServerTimings == nil || result.ServerTimings.CacheN == nil || *result.ServerTimings.CacheN != 0 || *result.ServerTimings.DraftNAccepted != 23 {
		t.Fatalf("lost timings: %+v", result.ServerTimings)
	}
}

func TestServerTimingsPreserveLargeSeed(t *testing.T) {
	var timings ServerTimings
	if err := json.Unmarshal([]byte(`{"effective_settings":{"seed":18446744073709551615}}`), &timings); err != nil {
		t.Fatal(err)
	}
	if string(timings.EffectiveSettings["seed"]) != "18446744073709551615" {
		t.Fatal("seed lost integer precision")
	}
}

func TestGPUAggregateKeepsVersionAndIndependentPeaks(t *testing.T) {
	g := &gpuSampler{version: 2, peak: 25000, peaks: map[string]uint64{"GPU-a": 16000, "GPU-b": 23000}}
	a := Aggregate{PeakVRAMMB: g.peakVRAM()}
	g.annotate(&a)
	if a.PeakVRAMMB != 25000 || a.GPUPeakVRAMMB["GPU-b"] != 23000 || a.GPUMetricVersion != 2 {
		t.Fatalf("bad aggregate: %+v", a)
	}
	var old Aggregate
	if err := json.Unmarshal([]byte(`{"peakVramMb":875}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.GPUMetricVersion != 0 || old.GPUPeakVRAMMB != nil || old.PeakVRAMMB != 875 {
		t.Fatalf("historical run reinterpreted: %+v", old)
	}
}

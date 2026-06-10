package benchmark

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestModeInstBenchTitle(t *testing.T) {
	if got := ModeInstBench.Title(); got != "Instruction following" {
		t.Fatalf("ModeInstBench.Title() = %q, want %q", got, "Instruction following")
	}
	if ModeInstBench != "instruction-bench" {
		t.Fatalf("ModeInstBench = %q, want %q", ModeInstBench, "instruction-bench")
	}
}

func TestInstructionAggregateFields(t *testing.T) {
	// Compile-time assertion that the three instruction aggregate fields exist
	// and are float64.
	var a Aggregate
	a.InstFormatRate = 1
	a.InstRefusalRate = 1
	a.InstConsistency = 1
	if a.InstFormatRate+a.InstRefusalRate+a.InstConsistency != 3 {
		t.Fatal("unexpected aggregate arithmetic")
	}
}

func TestModeMMLUBenchTitle(t *testing.T) {
	if got := ModeMMLUBench.Title(); got != "Factual knowledge (MMLU)" {
		t.Fatalf("ModeMMLUBench.Title() = %q, want %q", got, "Factual knowledge (MMLU)")
	}
	if ModeMMLUBench != "mmlu-bench" {
		t.Fatalf("ModeMMLUBench = %q, want %q", ModeMMLUBench, "mmlu-bench")
	}
}

func TestMMLUAggregateField(t *testing.T) {
	var a Aggregate
	a.MMLUAccuracy = 1
	if a.MMLUAccuracy != 1 {
		t.Fatal("MMLUAccuracy not assignable")
	}
}

func TestTitle_Phase3Modes(t *testing.T) {
	cases := map[Mode]string{
		ModeRagasBench:   "RAG faithfulness (synthetic)",
		ModeSummaryBench: "Summarization coherence",
	}
	for m, want := range cases {
		if got := m.Title(); got != want {
			t.Errorf("Title(%q) = %q, want %q", m, got, want)
		}
	}
}

func TestAggregate_Phase3FieldsOmitEmpty(t *testing.T) {
	b, err := json.Marshal(Aggregate{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ragasFaithfulness", "ragasRelevancy", "ragasPrecision", "summaryCoherence"} {
		if strings.Contains(string(b), k) {
			t.Errorf("zero %s should be omitted, got %s", k, b)
		}
	}
}

func TestRun_LegacyJSONDecodesClean(t *testing.T) {
	// A run persisted before the structured fields existed must decode with
	// zero values for all of them (additive JSON compat).
	legacy := `{
		"id": "p1-123", "profileId": "p1", "profileName": "old", "mode": "judge",
		"startedAt": "2026-01-01T00:00:00Z", "finishedAt": "2026-01-01T00:10:00Z",
		"profile": {"model": "m.gguf"},
		"problems": [{"problemId": "x", "problemName": "x", "resolved": true, "score": 1,
			"ttftMs": 1, "totalMs": 2, "tokensPerSecond": 3, "promptTokens": 4, "completionTokens": 5,
			"detail": "category=STEM expected A got \"A\""}],
		"aggregate": {"total": 1, "resolved": 1, "solveRate": 1, "avgScore": 1, "avgTtftMs": 1,
			"totalPromptTokens": 4, "totalCompletionTokens": 5, "totalMs": 2, "peakVramMb": 0, "avgGpuUtil": 0}
	}`
	var r Run
	if err := json.Unmarshal([]byte(legacy), &r); err != nil {
		t.Fatalf("legacy run failed to decode: %v", err)
	}
	p := r.Problems[0]
	if p.Kind != "" || p.Category != "" || p.Difficulty != 0 || p.SubScores != nil ||
		p.JudgedBy != "" || p.SimMethod != "" || p.Seed != 0 || p.Sandbox != "" {
		t.Errorf("legacy problem should have zero structured fields: %+v", p)
	}
	if r.ReusedInstance || r.Aggregate.Errored != 0 {
		t.Errorf("legacy run should have zero new run-level fields")
	}
	// And the new fields must not leak into JSON when zero.
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"kind", "subScores", "judgedBy", "simMethod", "tpsStdDev", "sandbox", "reusedInstance", "errored"} {
		if strings.Contains(string(b), `"`+k+`"`) {
			t.Errorf("zero %s should be omitted, got %s", k, b)
		}
	}
}

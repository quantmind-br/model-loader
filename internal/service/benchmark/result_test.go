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

package benchmark

import "testing"

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

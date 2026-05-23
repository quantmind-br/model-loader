package benchmark

import "testing"

func TestModeMathBench_TitleAndConst(t *testing.T) {
	if ModeMathBench != "math-bench" {
		t.Errorf("ModeMathBench = %q, want math-bench", ModeMathBench)
	}
	if ModeMathBench.Title() == string(ModeMathBench) {
		t.Error("ModeMathBench.Title() should return a human label, not the raw mode string")
	}
}

package benchmark

import "testing"

func TestModeCodeGenBench_TitleAndConst(t *testing.T) {
	if ModeCodeGenBench != "codegen-bench" {
		t.Errorf("ModeCodeGenBench = %q, want codegen-bench", ModeCodeGenBench)
	}
	if ModeCodeGenBench.Title() == string(ModeCodeGenBench) {
		t.Error("Title() should return a human label")
	}
}

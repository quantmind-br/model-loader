package benchmark

import "testing"

func TestFormatBenchProgress(t *testing.T) {
	if got := FormatBenchProgress(3, 10, "foo", "infer"); got != "problem 3/10: foo (infer)" {
		t.Fatalf("known total: %q", got)
	}
	if got := FormatBenchProgress(19, 0, "Terminal-Bench: 19 tasks complete", "infer"); got != "problem 19: Terminal-Bench: 19 tasks complete (infer)" {
		t.Fatalf("unknown total: %q", got)
	}
}
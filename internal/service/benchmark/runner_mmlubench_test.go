package benchmark

import "testing"

func TestNewRunnerLoadsMMLUSet(t *testing.T) {
	r, err := NewRunner(nil, nil, nil, Config{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if len(r.mmluProblems) == 0 {
		t.Fatal("expected NewRunner to load the embedded MMLU set")
	}
}

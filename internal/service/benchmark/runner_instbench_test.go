package benchmark

import "testing"

func TestNewRunnerLoadsInstructionSet(t *testing.T) {
	r, err := NewRunner(nil, nil, nil, Config{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if len(r.instProblems) == 0 {
		t.Fatal("expected NewRunner to load the embedded instruction set")
	}
}

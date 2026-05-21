package benchmark

import (
	"strings"
	"testing"
)

func TestLoad_EmbeddedSWEBenchValid(t *testing.T) {
	problems, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(problems) != 8 {
		t.Fatalf("got %d problems, want 8", len(problems))
	}
	seen := map[string]bool{}
	for _, p := range problems {
		if seen[p.ID] {
			t.Errorf("duplicate id %q", p.ID)
		}
		seen[p.ID] = true
		if p.Statement == "" {
			t.Errorf("problem %q has empty statement", p.ID)
		}
		if !strings.Contains(p.GoldenPatch, "diff --git") {
			t.Errorf("problem %q golden patch missing diff header", p.ID)
		}
		if p.RepoName == "" {
			t.Errorf("problem %q missing repo name", p.ID)
		}
	}
}

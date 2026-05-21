package benchmark

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
)

//go:embed data/*.json
var datasetFS embed.FS

// Load returns the embedded SWE-bench Lite coding set, sorted by ID. The set is
// fixed and identical for every profile so runs stay reproducible and
// comparable.
func Load() ([]Problem, error) {
	b, err := datasetFS.ReadFile("data/swebench_lite.json")
	if err != nil {
		return nil, fmt.Errorf("read embedded dataset: %w", err)
	}
	var problems []Problem
	if err := json.Unmarshal(b, &problems); err != nil {
		return nil, fmt.Errorf("decode embedded dataset: %w", err)
	}
	if err := validate(problems); err != nil {
		return nil, err
	}
	sort.Slice(problems, func(i, j int) bool { return problems[i].ID < problems[j].ID })
	return problems, nil
}

func validate(problems []Problem) error {
	if len(problems) == 0 {
		return fmt.Errorf("embedded dataset is empty")
	}
	seen := make(map[string]bool, len(problems))
	for _, p := range problems {
		if p.ID == "" {
			return fmt.Errorf("problem with empty id")
		}
		if seen[p.ID] {
			return fmt.Errorf("duplicate problem id %q", p.ID)
		}
		seen[p.ID] = true
		if p.Statement == "" {
			return fmt.Errorf("problem %q has empty statement", p.ID)
		}
		if p.GoldenPatch == "" {
			return fmt.Errorf("problem %q has no golden patch (needed for reference-guided judging)", p.ID)
		}
		if len(p.ContextFiles) == 0 {
			return fmt.Errorf("problem %q has no context files (oracle code context)", p.ID)
		}
	}
	return nil
}

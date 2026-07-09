package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportRun(t *testing.T) {
	dir := t.TempDir()
	run := Run{
		ID:          "demo-123",
		ProfileName: "demo",
		Mode:        ModeMathBench,
		StartedAt:   time.Now(),
		Aggregate:   Aggregate{Total: 1, Resolved: 1, MathAccuracy: 1},
		Problems: []ProblemResult{
			{ProblemID: "p1", ProblemName: "q1", Resolved: true, Score: 1, Detail: "ok"},
		},
	}
	jsonPath, csvPath, err := ExportRun(run, dir)
	if err != nil {
		t.Fatalf("ExportRun: %v", err)
	}
	if filepath.Dir(jsonPath) != dir || !strings.HasSuffix(jsonPath, "demo-123.json") {
		t.Fatalf("unexpected json path %q", jsonPath)
	}
	// JSON round-trips back to the run.
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read json: %v", err)
	}
	var back Run
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal exported json: %v", err)
	}
	if back.ID != "demo-123" || len(back.Problems) != 1 {
		t.Fatalf("json export lost data: %+v", back)
	}
	// CSV has a header and one data row mentioning the problem id.
	csv, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	text := string(csv)
	if !strings.Contains(text, "problemId") || !strings.Contains(text, "p1") {
		t.Fatalf("csv missing header or data row:\n%s", text)
	}
}

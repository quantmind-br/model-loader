package pages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

func TestExportRun(t *testing.T) {
	dir := t.TempDir()
	run := benchmark.Run{
		ID:          "demo-123",
		ProfileName: "demo",
		Mode:        benchmark.ModeMathBench,
		StartedAt:   time.Now(),
		Aggregate:   benchmark.Aggregate{Total: 1, Resolved: 1, MathAccuracy: 1},
		Problems: []benchmark.ProblemResult{
			{ProblemID: "p1", ProblemName: "q1", Resolved: true, Score: 1, Detail: "ok"},
		},
	}
	jsonPath, csvPath, err := exportRun(dir, run)
	if err != nil {
		t.Fatalf("exportRun: %v", err)
	}
	if filepath.Dir(jsonPath) != dir || !strings.HasSuffix(jsonPath, "demo-123.json") {
		t.Fatalf("unexpected json path %q", jsonPath)
	}
	// JSON round-trips back to the run.
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read json: %v", err)
	}
	var back benchmark.Run
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

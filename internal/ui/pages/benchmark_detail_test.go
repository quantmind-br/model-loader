package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

func detailFor(mode benchmark.Mode, agg benchmark.Aggregate, probs []benchmark.ProblemResult) string {
	run := benchmark.Run{
		ProfileName: "demo",
		Mode:        mode,
		StartedAt:   time.Now(),
		Aggregate:   agg,
		Problems:    probs,
	}
	p := BenchmarkPage{view: bvRunDetail, detail: &run}
	return p.viewRunDetail()
}

func TestRunDetailMathBreakdown(t *testing.T) {
	out := detailFor(benchmark.ModeMathBench,
		benchmark.Aggregate{Total: 2, Resolved: 1, MathAccuracy: 0.5, AvgDecodeTPS: 40, AvgPromptProcessingTPS: 800},
		[]benchmark.ProblemResult{
			{Resolved: true, Detail: `expected 4, got "4" (difficulty 1)`},
			{Resolved: false, Detail: `expected 9, got "8" (difficulty 2)`},
		})
	if !strings.Contains(out, "math accuracy 50%") {
		t.Fatalf("missing math accuracy line:\n%s", out)
	}
	if !strings.Contains(out, "difficulty 1") || !strings.Contains(out, "difficulty 2") {
		t.Fatalf("missing difficulty breakdown:\n%s", out)
	}
	if !strings.Contains(out, "prefill") || !strings.Contains(out, "decode") {
		t.Fatalf("missing prefill/decode line:\n%s", out)
	}
}

func TestRunDetailMMLUBreakdown(t *testing.T) {
	out := detailFor(benchmark.ModeMMLUBench,
		benchmark.Aggregate{Total: 2, Resolved: 1, MMLUAccuracy: 0.5},
		[]benchmark.ProblemResult{
			{Resolved: true, Detail: `category=STEM expected B got "B"`},
			{Resolved: false, Detail: `category=Social Sciences expected A got "C"`},
		})
	if !strings.Contains(out, "MMLU accuracy 50%") {
		t.Fatalf("missing MMLU accuracy line:\n%s", out)
	}
	if !strings.Contains(out, "STEM") || !strings.Contains(out, "Social Sciences") {
		t.Fatalf("missing category breakdown:\n%s", out)
	}
}

func TestRunDetailInstructionRates(t *testing.T) {
	out := detailFor(benchmark.ModeInstBench,
		benchmark.Aggregate{Total: 3, Resolved: 2, InstFormatRate: 0.5, InstRefusalRate: 1, InstConsistency: 0.9},
		nil)
	if !strings.Contains(out, "format") || !strings.Contains(out, "refusal") || !strings.Contains(out, "consistency") {
		t.Fatalf("missing instruction rate line:\n%s", out)
	}
}

func TestRunDetailCodeGenRate(t *testing.T) {
	out := detailFor(benchmark.ModeCodeGenBench,
		benchmark.Aggregate{Total: 2, Resolved: 1, CodePassRate: 0.5},
		[]benchmark.ProblemResult{
			{Resolved: true, Detail: "passed"},
			{Resolved: false, Detail: "failed: boom"},
		})
	if !strings.Contains(out, "pass rate") {
		t.Fatalf("missing codegen pass-rate line:\n%s", out)
	}
}

func TestModeDetailLines_Ragas(t *testing.T) {
	r := benchmark.Run{
		Mode: benchmark.ModeRagasBench,
		Aggregate: benchmark.Aggregate{
			RagasFaithfulness: 0.82, RagasRelevancy: 0.9, RagasPrecision: 0.75,
		},
	}
	lines := modeDetailLines(r)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "faithfulness") || !strings.Contains(joined, "relevancy") || !strings.Contains(joined, "precision") {
		t.Errorf("ragas detail missing fields:\n%s", joined)
	}
}

func TestModeDetailLines_Summary(t *testing.T) {
	r := benchmark.Run{
		Mode:      benchmark.ModeSummaryBench,
		Aggregate: benchmark.Aggregate{SummaryCoherence: 0.71},
	}
	lines := modeDetailLines(r)
	if !strings.Contains(strings.Join(lines, "\n"), "coherence") {
		t.Errorf("summary detail missing coherence: %v", lines)
	}
}

// TestRunDetailScorecards asserts the detail view renders the visual scorecard
// cards (primary metric + tok/s + TTFT + VRAM) above the summary line.
func TestRunDetailScorecards(t *testing.T) {
	out := detailFor(benchmark.ModeJudge,
		benchmark.Aggregate{Total: 2, Resolved: 1, SolveRate: 0.5, AvgTokensPerSecond: 42, AvgTTFTms: 150, PeakVRAMMB: 8000},
		nil)
	for _, want := range []string{"solve", "tok/s", "TTFT", "VRAM"} {
		if !strings.Contains(out, want) {
			t.Fatalf("detail view missing scorecard %q:\n%s", want, out)
		}
	}
	// The primary metric value (50%) and throughput (42.0) should appear.
	if !strings.Contains(out, "50%") {
		t.Fatalf("detail view missing primary metric value:\n%s", out)
	}
}

// TestRunDetailPartialRunKeepsErrorBanner asserts a partial run (Err set) still
// shows the visual scorecards alongside the error banner.
func TestRunDetailPartialRunKeepsErrorBanner(t *testing.T) {
	run := benchmark.Run{
		ProfileName: "demo", Mode: benchmark.ModeJudge,
		StartedAt: time.Now(), Err: "context canceled",
		Aggregate: benchmark.Aggregate{Total: 1, Resolved: 0, SolveRate: 0},
		Problems:  []benchmark.ProblemResult{{ProblemID: "p1"}},
	}
	p := BenchmarkPage{view: bvRunDetail, detail: &run}
	out := p.viewRunDetail()
	if !strings.Contains(out, "run incomplete") {
		t.Fatalf("partial run missing error banner:\n%s", out)
	}
	if !strings.Contains(out, "solve") {
		t.Fatalf("partial run should still render scorecards:\n%s", out)
	}
}

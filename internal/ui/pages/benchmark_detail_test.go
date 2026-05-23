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

package pages

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

func TestPrimaryMetric_Judge(t *testing.T) {
	r := benchmark.Run{Mode: benchmark.ModeJudge,
		Aggregate: benchmark.Aggregate{SolveRate: 0.5, Resolved: 1, Total: 2}}
	m := primaryMetric(r)
	if m.Label != "solve" || m.Frac != 0.5 || m.Higher != true {
		t.Fatalf("judge primary = %+v", m)
	}
	if m.Text != "50%" {
		t.Fatalf("text = %q want 50%%", m.Text)
	}
}

func TestPrimaryMetric_LlamaBenchUsesThroughput(t *testing.T) {
	r := benchmark.Run{Mode: benchmark.ModeLlamaBench,
		Aggregate: benchmark.Aggregate{AvgTokensPerSecond: 42.0}}
	m := primaryMetric(r)
	if m.Label != "tok/s" || m.Higher != true {
		t.Fatalf("llama-bench primary = %+v", m)
	}
	if m.Raw != 42.0 {
		t.Fatalf("raw = %v want 42", m.Raw)
	}
}

func TestPrimaryMetric_LongContextUsesRecall(t *testing.T) {
	r := benchmark.Run{Mode: benchmark.ModeLongContext,
		Aggregate: benchmark.Aggregate{AvgScore: 0.9}}
	m := primaryMetric(r)
	if m.Label != "recall" || m.Frac != 0.9 {
		t.Fatalf("longctx primary = %+v", m)
	}
}

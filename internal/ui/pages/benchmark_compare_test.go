package pages

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

// Regression: high solve rates indexed the bar string by byte length (24) into
// a rune slice of length 8, panicking on History open. Must not panic and must
// render one bar per run.
func TestSparkSolveRate_NoPanicOnHighRates(t *testing.T) {
	runs := []benchmark.Run{
		{Aggregate: benchmark.Aggregate{SolveRate: 1.0}},
		{Aggregate: benchmark.Aggregate{SolveRate: 0.8}},
		{Aggregate: benchmark.Aggregate{SolveRate: 0.5}},
	}
	got := sparkSolveRate(runs) // would panic before the rune-length fix
	if got == "" {
		t.Fatal("expected a rendered trend, got empty")
	}
}

func TestSparkSolveRate_EmptyWhenSingleRun(t *testing.T) {
	if got := sparkSolveRate([]benchmark.Run{{}}); got != "" {
		t.Errorf("want empty for <2 runs, got %q", got)
	}
}

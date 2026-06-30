package pages

import (
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

func TestDashboardRows_LatestPerProfileWithDelta(t *testing.T) {
	now := time.Now()
	runs := []benchmark.Run{ // newest-first (como o store entrega)
		{ID: "b", ProfileID: "p1", ProfileName: "P1", Mode: benchmark.ModeJudge,
			StartedAt: now, Aggregate: benchmark.Aggregate{SolveRate: 0.6}},
		{ID: "a", ProfileID: "p1", ProfileName: "P1", Mode: benchmark.ModeJudge,
			StartedAt: now.Add(-time.Hour), Aggregate: benchmark.Aggregate{SolveRate: 0.4}},
	}
	rows := dashboardRows(runs, benchmark.ModeJudge)
	if len(rows) != 1 {
		t.Fatalf("want 1 collapsed row, got %d", len(rows))
	}
	if rows[0].Latest.ID != "b" || rows[0].Previous == nil || rows[0].Previous.ID != "a" {
		t.Fatalf("latest/previous wrong: %+v", rows[0])
	}
	if rows[0].DeltaFrac <= 0 { // 0.6 - 0.4 = +0.2
		t.Fatalf("delta should be positive, got %v", rows[0].DeltaFrac)
	}
	if len(rows[0].Trend) != 2 || rows[0].Trend[0] != 0.4 || rows[0].Trend[1] != 0.6 {
		t.Fatalf("trend must be oldest→newest [0.4,0.6], got %v", rows[0].Trend)
	}
}

func TestDashboardRows_SkipsPartial(t *testing.T) {
	runs := []benchmark.Run{{ProfileID: "p1", Mode: benchmark.ModeJudge, Err: "boom"}}
	if rows := dashboardRows(runs, benchmark.ModeJudge); len(rows) != 0 {
		t.Fatalf("partial run must be skipped, got %d rows", len(rows))
	}
}

func TestDashboardRows_SortedByMetricDesc(t *testing.T) {
	runs := []benchmark.Run{
		{ID: "low", ProfileID: "p1", ProfileName: "Low", Mode: benchmark.ModeJudge,
			Aggregate: benchmark.Aggregate{SolveRate: 0.3}},
		{ID: "high", ProfileID: "p2", ProfileName: "High", Mode: benchmark.ModeJudge,
			Aggregate: benchmark.Aggregate{SolveRate: 0.9}},
	}
	rows := dashboardRows(runs, benchmark.ModeJudge)
	if len(rows) != 2 || rows[0].ProfileID != "p2" {
		t.Fatalf("rows must be sorted by metric desc (p2 first): %+v", rows)
	}
}

func TestDashboardRows_FiltersByMode(t *testing.T) {
	runs := []benchmark.Run{
		{ID: "j", ProfileID: "p1", Mode: benchmark.ModeJudge, Aggregate: benchmark.Aggregate{SolveRate: 0.5}},
		{ID: "l", ProfileID: "p1", Mode: benchmark.ModeLlamaBench, Aggregate: benchmark.Aggregate{AvgTokensPerSecond: 10}},
	}
	if rows := dashboardRows(runs, benchmark.ModeLlamaBench); len(rows) != 1 || rows[0].Latest.ID != "l" {
		t.Fatalf("mode filter failed: %+v", rows)
	}
}

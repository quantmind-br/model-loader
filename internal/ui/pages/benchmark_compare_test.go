package pages

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

// TestCompareVisual_RanksByPrimaryMetricAndMarksBest asserts the compare view
// sorts by the primary metric (descending) and marks the best row with ▲.
func TestCompareVisual_RanksByPrimaryMetricAndMarksBest(t *testing.T) {
	page := BenchmarkPage{
		compareSections: []benchCompareSection{{
			Mode: benchmark.ModeJudge,
			Runs: []benchmark.Run{
				{ProfileID: "low", ProfileName: "Low", Mode: benchmark.ModeJudge,
					Aggregate: benchmark.Aggregate{SolveRate: 0.3, AvgTokensPerSecond: 10, AvgTTFTms: 200, PeakVRAMMB: 5000}},
				{ProfileID: "high", ProfileName: "High", Mode: benchmark.ModeJudge,
					Aggregate: benchmark.Aggregate{SolveRate: 0.9, AvgTokensPerSecond: 20, AvgTTFTms: 100, PeakVRAMMB: 4000}},
			},
		}},
	}
	out := page.viewCompare()
	lines := strings.Split(out, "\n")
	// Find the data rows (skip title/blank/header).
	var dataRows []string
	for _, l := range lines {
		if strings.Contains(l, "Low") || strings.Contains(l, "High") {
			dataRows = append(dataRows, l)
		}
	}
	if len(dataRows) != 2 {
		t.Fatalf("expected 2 data rows, got %d in:\n%s", len(dataRows), out)
	}
	// "High" (0.9 solve) must rank first and carry the ▲ marker.
	if !strings.HasPrefix(strings.TrimSpace(dataRows[0]), "▲") {
		t.Fatalf("best row should carry ▲ marker; first data row:\n%s", dataRows[0])
	}
	if !strings.Contains(dataRows[0], "High") {
		t.Fatalf("best row should be High (0.9 solve); got:\n%s", dataRows[0])
	}
}

// TestCompareVisual_MetricCycleReorders asserts pressing 'm' to switch to the
// tok/s metric reorders the ranking.
func TestCompareVisual_MetricCycleReorders(t *testing.T) {
	page := BenchmarkPage{
		compareSections: []benchCompareSection{{
			Mode: benchmark.ModeJudge,
			Runs: []benchmark.Run{
				// Higher solve but lower tok/s — under metric 0 (solve) this ranks
				// first; under metric 1 (tok/s) it should drop to second.
				{ProfileID: "a", ProfileName: "SolveKing", Mode: benchmark.ModeJudge,
					Aggregate: benchmark.Aggregate{SolveRate: 0.9, AvgTokensPerSecond: 5}},
				{ProfileID: "b", ProfileName: "SpeedKing", Mode: benchmark.ModeJudge,
					Aggregate: benchmark.Aggregate{SolveRate: 0.3, AvgTokensPerSecond: 50}},
			},
		}},
	}
	// Default metric 0 (solve): SolveKing first.
	out0 := page.viewCompare()
	idxSolve0 := strings.Index(out0, "SolveKing")
	idxSpeed0 := strings.Index(out0, "SpeedKing")
	if idxSolve0 == -1 || idxSolve0 > idxSpeed0 {
		t.Fatalf("metric 0 should rank SolveKing first:\n%s", out0)
	}
	// Metric 1 (tok/s): SpeedKing first.
	page.compareMetric = 1
	out1 := page.viewCompare()
	idxSolve1 := strings.Index(out1, "SolveKing")
	idxSpeed1 := strings.Index(out1, "SpeedKing")
	if idxSpeed1 == -1 || idxSpeed1 > idxSolve1 {
		t.Fatalf("metric 1 (tok/s) should rank SpeedKing first:\n%s", out1)
	}
}

// TestCompareVisual_RateBarsAreProportional is a regression for the bug where
// normalizeCompareFrac returned the Frac of runs[0] for every row in a rate
// metric section, making all bars visually identical. With distinct SolveRates
// the higher row must have more filled blocks than the lower row.
func TestCompareVisual_RateBarsAreProportional(t *testing.T) {
	page := BenchmarkPage{
		compareSections: []benchCompareSection{{
			Mode: benchmark.ModeJudge,
			Runs: []benchmark.Run{
				{ProfileID: "high", ProfileName: "High", Mode: benchmark.ModeJudge,
					Aggregate: benchmark.Aggregate{SolveRate: 0.9}},
				{ProfileID: "low", ProfileName: "Low", Mode: benchmark.ModeJudge,
					Aggregate: benchmark.Aggregate{SolveRate: 0.1}},
			},
		}},
	}
	out := page.viewCompare()
	lines := strings.Split(out, "\n")
	var highLine, lowLine string
	for _, l := range lines {
		if strings.Contains(l, "High") {
			highLine = l
		}
		if strings.Contains(l, "Low") {
			lowLine = l
		}
	}
	if highLine == "" || lowLine == "" {
		t.Fatalf("missing data rows in:\n%s", out)
	}
	countBlocks := func(s string) int {
		n := strings.Count(s, "█")
		n += strings.Count(s, "#") // NO_COLOR fallback
		return n
	}
	if ch, cl := countBlocks(highLine), countBlocks(lowLine); ch <= cl {
		t.Fatalf("high (0.9) should have more filled blocks than low (0.1): high=%d low=%d\n%s\n%s",
			ch, cl, highLine, lowLine)
	}
}

// TestCompareVisual_LowerBetterZeroNotBest is a regression: a run with a 0
// TTFT/VRAM (metric not collected) must not be ranked best (carrying ▲) nor
// rendered with a full bar when the user cycles to a lower-is-better metric.
func TestCompareVisual_LowerBetterZeroNotBest(t *testing.T) {
	page := BenchmarkPage{
		compareMetric: 2, // TTFT (lower is better)
		compareSections: []benchCompareSection{{
			Mode: benchmark.ModeJudge,
			Runs: []benchmark.Run{
				{ProfileID: "real", ProfileName: "Real", Mode: benchmark.ModeJudge,
					Aggregate: benchmark.Aggregate{SolveRate: 0.5, AvgTTFTms: 150}},
				{ProfileID: "nodata", ProfileName: "NoData", Mode: benchmark.ModeJudge,
					Aggregate: benchmark.Aggregate{SolveRate: 0.5, AvgTTFTms: 0}},
			},
		}},
	}
	out := page.viewCompare()
	lines := strings.Split(out, "\n")
	var dataRows []string
	for _, l := range lines {
		if strings.Contains(l, "Real") || strings.Contains(l, "NoData") {
			dataRows = append(dataRows, l)
		}
	}
	if len(dataRows) != 2 {
		t.Fatalf("expected 2 data rows, got %d:\n%s", len(dataRows), out)
	}
	// The run with real TTFT data must rank first (data-less rows sink).
	if !strings.Contains(dataRows[0], "Real") {
		t.Fatalf("Real (TTFT 150) must rank above NoData (TTFT 0):\n%s", out)
	}
	// The data-less row must render an empty bar (no filled blocks).
	var noDataRow string
	for _, l := range dataRows {
		if strings.Contains(l, "NoData") {
			noDataRow = l
		}
	}
	if strings.Count(noDataRow, "█")+strings.Count(noDataRow, "#") != 0 {
		t.Fatalf("NoData (TTFT=0) must render an empty bar, got:\n%s", noDataRow)
	}
}

// TestHistory_NavigationAndEnter asserts the history view cursor moves and
// enter opens the selected run's detail.
func TestHistory_NavigationAndEnter(t *testing.T) {
	page := BenchmarkPage{
		view: bvHistory,
		historyRuns: []benchmark.Run{
			{ID: "newer", ProfileName: "P", Mode: benchmark.ModeJudge, Aggregate: benchmark.Aggregate{SolveRate: 0.6}},
			{ID: "older", ProfileName: "P", Mode: benchmark.ModeJudge, Aggregate: benchmark.Aggregate{SolveRate: 0.3}},
		},
	}
	m, _ := page.keyHistory(tea.KeyMsg{Type: tea.KeyDown})
	page = m.(BenchmarkPage)
	if page.histCursor != 1 {
		t.Fatalf("histCursor after down = %d, want 1", page.histCursor)
	}
	m, _ = page.keyHistory(tea.KeyMsg{Type: tea.KeyEnter})
	page = m.(BenchmarkPage)
	if page.view != bvRunDetail || page.detail == nil || page.detail.ID != "older" {
		t.Fatalf("enter should open older detail; view=%v detail=%+v", page.view, page.detail)
	}
}

// TestHistory_MetricToggleAndMinmax asserts 'm' toggles the metric and the
// sparkline renders min/max labels.
func TestHistory_MetricToggleAndMinmax(t *testing.T) {
	page := BenchmarkPage{
		view: bvHistory,
		historyRuns: []benchmark.Run{
			{ProfileName: "P", Mode: benchmark.ModeJudge, Aggregate: benchmark.Aggregate{SolveRate: 0.6, AvgTokensPerSecond: 40}},
			{ProfileName: "P", Mode: benchmark.ModeJudge, Aggregate: benchmark.Aggregate{SolveRate: 0.3, AvgTokensPerSecond: 20}},
		},
	}
	out0 := page.viewHistory()
	if !strings.Contains(out0, "min") || !strings.Contains(out0, "max") {
		t.Fatalf("history sparkline should show min/max:\n%s", out0)
	}
	m, _ := page.keyHistory(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	page = m.(BenchmarkPage)
	if page.histMetric != 1 {
		t.Fatalf("histMetric after m = %d, want 1", page.histMetric)
	}
	out1 := page.viewHistory()
	if !strings.Contains(out1, "tok/s trend") {
		t.Fatalf("metric 1 should show tok/s trend:\n%s", out1)
	}
}

// Regression: high solve rates indexed the bar string by byte length (24) into
// a rune slice of length 8, panicking on History open. Must not panic and must
// render one bar per run.
func TestSparkSolveRate_NoPanicOnHighRates(t *testing.T) {
	runs := []benchmark.Run{
		{Aggregate: benchmark.Aggregate{SolveRate: 1.0}},
		{Aggregate: benchmark.Aggregate{SolveRate: 0.8}},
		{Aggregate: benchmark.Aggregate{SolveRate: 0.5}},
	}
	got := sparkTrend(runs) // would panic before the rune-length fix
	if got == "" {
		t.Fatal("expected a rendered trend, got empty")
	}
}

func TestSparkSolveRate_EmptyWhenSingleRun(t *testing.T) {
	if got := sparkTrend([]benchmark.Run{{}}); got != "" {
		t.Errorf("want empty for <2 runs, got %q", got)
	}
}

// llama-bench history trends on tokens/second (normalized by the series max),
// not solve-rate. Must render one bar per run without panicking even when
// tok/s exceeds 1.0 (it always does).
func TestSparkTrend_ThroughputUsesTokensPerSecond(t *testing.T) {
	runs := []benchmark.Run{
		{Mode: benchmark.ModeLlamaBench, Aggregate: benchmark.Aggregate{AvgTokensPerSecond: 120}},
		{Mode: benchmark.ModeLlamaBench, Aggregate: benchmark.Aggregate{AvgTokensPerSecond: 60}},
		{Mode: benchmark.ModeLlamaBench, Aggregate: benchmark.Aggregate{AvgTokensPerSecond: 30}},
	}
	got := sparkTrend(runs)
	if got == "" {
		t.Fatal("expected a rendered tok/s trend, got empty")
	}
}

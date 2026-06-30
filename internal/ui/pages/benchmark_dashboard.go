package pages

import (
	"sort"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

// benchDashboardRow is one profile's latest complete run in the focused mode,
// plus the run before it (for Δ) and a trend series (oldest→newest).
type benchDashboardRow struct {
	ProfileID   string
	ProfileName string
	Latest      benchmark.Run
	Previous    *benchmark.Run
	Metric      benchMetric
	DeltaFrac   float64
	Trend       []float64
}

// dashboardRows builds the leaderboard for one mode from newest-first runs:
// the latest complete run per profile (partial runs with Err set are skipped),
// its predecessor for the Δ arrow, and a chronological trend series. Sorted by
// primary metric descending (higher-is-better) so the best model floats to the
// top. DeltaFrac is in the metric's native unit (percentage-point fraction for
// rate modes, raw tok/s delta for throughput) — the renderer interprets it.
func dashboardRows(runs []benchmark.Run, mode benchmark.Mode) []benchDashboardRow {
	type acc struct {
		ordered []benchmark.Run // newest-first for this profile+mode
	}
	byProfile := map[string]*acc{}
	var order []string
	for _, r := range runs {
		if r.Mode != mode || r.Err != "" {
			continue
		}
		a, ok := byProfile[r.ProfileID]
		if !ok {
			a = &acc{}
			byProfile[r.ProfileID] = a
			order = append(order, r.ProfileID)
		}
		a.ordered = append(a.ordered, r)
	}
	rows := make([]benchDashboardRow, 0, len(order))
	for _, id := range order {
		a := byProfile[id]
		latest := a.ordered[0]
		row := benchDashboardRow{
			ProfileID: id, ProfileName: latest.ProfileName,
			Latest: latest, Metric: primaryMetric(latest),
		}
		if len(a.ordered) > 1 {
			prev := a.ordered[1]
			row.Previous = &prev
			row.DeltaFrac = primaryMetric(latest).Raw - primaryMetric(prev).Raw
		}
		// trend oldest→newest
		for i := len(a.ordered) - 1; i >= 0; i-- {
			row.Trend = append(row.Trend, primaryMetric(a.ordered[i]).Raw)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Metric.Raw > rows[j].Metric.Raw
	})
	return rows
}

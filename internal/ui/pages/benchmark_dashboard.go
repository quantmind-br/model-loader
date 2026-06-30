package pages

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
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

// focusedDashboardMode resolves which mode's leaderboard to show. Preference:
// the explicitly focused mode if it has at least one complete run; else the
// first mode (in canonical benchModes order) that has a complete run; else the
// first benchModes entry; else "" (caller shows empty state).
func (p BenchmarkPage) focusedDashboardMode() benchmark.Mode {
	has := func(m benchmark.Mode) bool {
		for _, r := range p.runs {
			if r.Mode == m && r.Err == "" {
				return true
			}
		}
		return false
	}
	if p.focusMode != "" && has(p.focusMode) {
		return p.focusMode
	}
	for _, m := range benchModes {
		if has(m) {
			return m
		}
	}
	if len(benchModes) > 0 {
		return benchModes[0]
	}
	return ""
}

// modesWithRuns returns the canonical-ordered modes that have at least one
// complete run, for the category/mode focus bar.
func (p BenchmarkPage) modesWithRuns() []benchmark.Mode {
	out := make([]benchmark.Mode, 0, len(benchModes))
	for _, m := range benchModes {
		for _, r := range p.runs {
			if r.Mode == m && r.Err == "" {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

// selectedDashboardRun returns the Latest run of the leaderboard row under the
// dashCursor for the focused mode, or ok=false when the cursor is out of range
// (empty leaderboard). Used by the dashboard's [enter] to open run detail.
func (p BenchmarkPage) selectedDashboardRun() (benchmark.Run, bool) {
	rows := dashboardRows(p.runs, p.focusedDashboardMode())
	if p.dashCursor < 0 || p.dashCursor >= len(rows) {
		return benchmark.Run{}, false
	}
	return rows[p.dashCursor].Latest, true
}

// cycleFocusMode returns the mode dir steps away (wrapping) from the currently
// focused mode within the set of modes that have complete runs. With no such
// modes it leaves the focus unchanged.
func (p BenchmarkPage) cycleFocusMode(dir int) benchmark.Mode {
	modes := p.modesWithRuns()
	if len(modes) == 0 {
		return p.focusMode
	}
	current := p.focusedDashboardMode()
	idx := 0
	for i, m := range modes {
		if m == current {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(modes)) % len(modes)
	return modes[idx]
}

// viewDashboard renders the leaderboard-first dashboard: a summary strip, a
// mode-focus bar, the ranked leaderboard for the focused mode, and an insight
// panel for the selected row. It reads only p.runs (never p.runner), so it
// works in tests that construct the page without an engine.
func (p BenchmarkPage) viewDashboard() string {
	title := theme.Title.Render("Benchmark dashboard")
	if len(p.runs) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, title,
			components.EmptyState("No benchmark runs yet", "Press [b] to run your first benchmark"))
	}
	mode := p.focusedDashboardMode()
	parts := []string{
		title,
		p.renderDashSummary(),
		"",
		p.renderModeBar(mode),
		"",
		p.renderLeaderboard(mode),
		"",
		p.renderInsight(mode),
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderDashSummary is the top strip: total runs, complete vs partial counts,
// and the best profile in the focused mode.
func (p BenchmarkPage) renderDashSummary() string {
	total := len(p.runs)
	partial := 0
	for _, r := range p.runs {
		if r.Err != "" {
			partial++
		}
	}
	complete := total - partial
	line := fmt.Sprintf("%d runs   %d complete   %d partial", total, complete, partial)
	if rows := dashboardRows(p.runs, p.focusedDashboardMode()); len(rows) > 0 {
		line += fmt.Sprintf("   best: %s (%s %s)", rows[0].ProfileName, rows[0].Metric.Text, rows[0].Metric.Label)
	}
	return theme.Subtitle.Render(line)
}

// renderModeBar shows the modes that have runs, bracketing the focused one.
func (p BenchmarkPage) renderModeBar(focus benchmark.Mode) string {
	modes := p.modesWithRuns()
	if len(modes) == 0 {
		return theme.Subtitle.Render("(no complete runs)")
	}
	cells := make([]string, 0, len(modes))
	for _, m := range modes {
		label := m.Title()
		if m == focus {
			if theme.NoColor() {
				label = "[" + label + "]"
			} else {
				label = theme.Selected.Render(label)
			}
		}
		cells = append(cells, label)
	}
	return "Mode: " + strings.Join(cells, "  ")
}

// renderLeaderboard renders the ranked rows for the focused mode with a
// proportional MetricBar and a Δ arrow vs the previous run. Throughput modes
// (Frac==0 from primaryMetric) are normalized against the column's max Raw so
// their bars are still proportional.
func (p BenchmarkPage) renderLeaderboard(mode benchmark.Mode) string {
	rows := dashboardRows(p.runs, mode)
	if len(rows) == 0 {
		return theme.Subtitle.Render("no complete runs for this mode")
	}
	// Determine bar width from terminal width (fallback 20).
	barW := 20
	if p.width > 0 {
		barW = p.width / 3
		if barW < 8 {
			barW = 8
		}
		if barW > 40 {
			barW = 40
		}
	}
	// Throughput normalization: when Frac is 0 but Raw>0 we scale by max Raw.
	maxRaw := 0.0
	for _, r := range rows {
		if r.Metric.Raw > maxRaw {
			maxRaw = r.Metric.Raw
		}
	}
	var b strings.Builder
	for i, r := range rows {
		frac := r.Metric.Frac
		if frac == 0 && maxRaw > 0 {
			frac = r.Metric.Raw / maxRaw
		}
		bar := components.MetricBar(frac, barW)
		arrow := deltaArrow(r)
		cursor := "  "
		if i == p.dashCursor {
			cursor = "> "
		}
		line := fmt.Sprintf("%s%-22s %s %6s %s", cursor, truncate(r.ProfileName, 22), bar, r.Metric.Text, arrow)
		if i == p.dashCursor && !theme.NoColor() {
			line = theme.Selected.Render(line)
		}
		b.WriteString(line)
		if i < len(rows)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// deltaArrow renders the trend marker vs the previous run. Higher-is-better is
// assumed (every current primary metric is). NO_COLOR keeps the glyphs.
func deltaArrow(r benchDashboardRow) string {
	if r.Previous == nil || r.DeltaFrac == 0 {
		return "·"
	}
	if r.DeltaFrac > 0 {
		return theme.OK.Render("▲")
	}
	return theme.Error.Render("▼")
}

// renderInsight is a small panel for the selected leaderboard row: its trend
// sparkline and the absolute Δ vs the previous run.
func (p BenchmarkPage) renderInsight(mode benchmark.Mode) string {
	rows := dashboardRows(p.runs, mode)
	if len(rows) == 0 || p.dashCursor >= len(rows) {
		return ""
	}
	r := rows[p.dashCursor]
	spark := components.Sparkline(r.Trend, 24)
	delta := "first run"
	if r.Previous != nil {
		delta = fmt.Sprintf("Δ %+.2f vs previous", r.DeltaFrac)
	}
	return theme.Subtitle.Render(fmt.Sprintf("%s — trend %s   %s", r.ProfileName, spark, delta))
}

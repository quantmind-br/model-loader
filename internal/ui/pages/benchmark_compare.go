package pages

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// benchCompareSection is one mode's slice of the compare view: the most
// recent complete run of each profile in that mode.
type benchCompareSection struct {
	Mode benchmark.Mode
	Runs []benchmark.Run
}

// openCompare groups the most recent complete run per (mode, profile) into
// one section per mode, so only like-for-like numbers sit side by side.
// Partial runs (Err set) are skipped — a half-finished run would distort the
// comparison. p.runs is newest-first, so the first hit per key wins.
func (p BenchmarkPage) openCompare() (tea.Model, tea.Cmd) {
	if len(p.runs) == 0 {
		var cmd tea.Cmd
		p, cmd = p.withFlashError("no runs to compare")
		return p, cmd
	}
	seen := map[string]bool{}
	byMode := map[benchmark.Mode][]benchmark.Run{}
	for _, r := range p.runs {
		if r.Err != "" {
			continue
		}
		key := string(r.Mode) + "|" + r.ProfileID
		if seen[key] {
			continue
		}
		seen[key] = true
		byMode[r.Mode] = append(byMode[r.Mode], r)
	}
	sections := make([]benchCompareSection, 0, len(byMode))
	for _, m := range benchmark.ModesInOrder() {
		if runs := byMode[m]; len(runs) > 0 {
			sections = append(sections, benchCompareSection{Mode: m, Runs: runs})
			delete(byMode, m)
		}
	}
	// Runs persisted by unknown/legacy modes still deserve a section.
	for m, runs := range byMode {
		sections = append(sections, benchCompareSection{Mode: m, Runs: runs})
	}
	if len(sections) == 0 {
		var cmd tea.Cmd
		p, cmd = p.withFlashError("no complete runs to compare")
		return p, cmd
	}
	p.compareSections = sections
	p.view = bvCompare
	return p, nil
}

// openHistory shows every run of the selected run's profile over time.
func (p BenchmarkPage) openHistory() (tea.Model, tea.Cmd) {
	seed, ok := p.selectedDashboardRun()
	if !ok {
		var cmd tea.Cmd
		p, cmd = p.withFlashError("select a run first")
		return p, cmd
	}
	target := seed.ProfileID
	hist := make([]benchmark.Run, 0)
	for _, r := range p.runs {
		if r.ProfileID == target {
			hist = append(hist, r)
		}
	}
	p.historyRuns = hist
	p.view = bvHistory
	return p, nil
}

func (p BenchmarkPage) viewCompare() string {
	title := theme.Title.Render("Compare profiles (latest complete run per mode)")
	parts := []string{title}
	for _, sec := range p.compareSections {
		parts = append(parts, "", theme.Subtitle.Render(sec.Mode.Title()))
		parts = append(parts, p.compareSectionRows(sec)...)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// compareSectionRows renders one mode section as a ranked leaderboard with
// proportional MetricBars and a ▲ marker on the best performer. The ranking
// metric is driven by compareMetric: 0 = the mode's primary metric (solve /
// recall / tok/s), 1 = tok/s, 2 = TTFT (lower is better), 3 = VRAM (lower is
// better). Columns that don't apply to the selected metric are still shown as
// text for context.
func (p BenchmarkPage) compareSectionRows(sec benchCompareSection) []string {
	runs := sortedCompareRuns(sec, p.compareMetric)
	barW := 16

	nameW := 20
	if p.width > 0 {
		nameW = min(20, max(10, p.width-64))
	}
	header := fmt.Sprintf("%-*s  %-16s  %8s  %7s  %8s  %s",
		nameW, "profile", p.compareMetricLabel(sec.Mode), "tok/s", "TTFT", "vram", "quant")
	rows := []string{theme.Subtitle.Render(header)}

	for i, r := range runs {
		a := r.Aggregate
		frac, text, higher := compareMetricValue(r, p.compareMetric, sec.Mode)
		frac = normalizeCompareFrac(frac, p.compareMetric, runs, sec.Mode)
		bar := components.MetricBar(frac, barW)
		marker := "  "
		if i == 0 && len(runs) > 1 && higher {
			marker = theme.OK.Render("▲")
		}
		line := fmt.Sprintf("%s%-*s  %s %6s  %8.1f  %5.0fms  %6dMB  %s",
			marker+" ", nameW, truncate(r.ProfileName, nameW), bar, text,
			a.AvgTokensPerSecond, a.AvgTTFTms, a.PeakVRAMMB, dash(r.Profile.Quantization))
		rows = append(rows, line)
	}
	return rows
}

// compareMetricLabel returns the column header for the selected compare metric.
func (p BenchmarkPage) compareMetricLabel(mode benchmark.Mode) string {
	switch p.compareMetric {
	case 1:
		return "tok/s (bar)"
	case 2:
		return "TTFT↓ (bar)"
	case 3:
		return "VRAM↓ (bar)"
	default:
		return primaryMetricLabel(mode) + " (bar)"
	}
}

// compareMetricValue returns (fracHint, displayText, higherIsBetter) for the
// selected metric. fracHint is the raw value; normalizeCompareFrac scales it.
func compareMetricValue(r benchmark.Run, metric int, mode benchmark.Mode) (float64, string, bool) {
	a := r.Aggregate
	switch metric {
	case 1:
		return a.AvgTokensPerSecond, fmt.Sprintf("%.1f", a.AvgTokensPerSecond), true
	case 2:
		return a.AvgTTFTms, fmt.Sprintf("%.0fms", a.AvgTTFTms), false
	case 3:
		return float64(a.PeakVRAMMB), fmt.Sprintf("%dMB", a.PeakVRAMMB), false
	default:
		m := primaryMetric(r)
		return m.Raw, m.Text, true
	}
}

// normalizeCompareFrac scales a raw metric value into [0,1] for the bar:
// rate metrics (solve/recall) are already 0..1 so the raw value IS the fill;
// throughput scales by section max; TTFT/VRAM invert and scale by section max
// so lower fills more bar. For the lower-is-better metrics a raw value of 0 is
// treated as "not collected" and renders an empty bar (never a full one).
func normalizeCompareFrac(raw float64, metric int, runs []benchmark.Run, mode benchmark.Mode) float64 {
	if metric == 0 {
		// Primary metric: rate modes are already 0..1; throughput needs scaling.
		if primaryMetric(benchmark.Run{Mode: mode}).Rate {
			return raw
		}
	}
	// Lower-is-better metrics with no data (0) are unknown, not "best".
	if (metric == 2 || metric == 3) && raw <= 0 {
		return 0
	}
	// Find max for scaling.
	maxVal := 0.0
	for _, r := range runs {
		v, _, _ := compareMetricValue(r, metric, mode)
		if v > maxVal {
			maxVal = v
		}
	}
	if maxVal == 0 {
		return 0
	}
	if metric == 2 || metric == 3 {
		// Lower is better: invert (min→full, max→empty).
		return 1 - raw/maxVal
	}
	return raw / maxVal
}

// sortedCompareRuns returns the section's runs sorted by the selected metric
// (best first). Lower-is-better metrics sort ascending, but a value of 0 means
// "not collected" and is pushed to the end so a data-less run never wins.
func sortedCompareRuns(sec benchCompareSection, metric int) []benchmark.Run {
	out := make([]benchmark.Run, len(sec.Runs))
	copy(out, sec.Runs)
	lowerBetter := metric == 2 || metric == 3
	sort.SliceStable(out, func(i, j int) bool {
		vi, _, higher := compareMetricValue(out[i], metric, sec.Mode)
		vj, _, _ := compareMetricValue(out[j], metric, sec.Mode)
		if lowerBetter {
			// Treat 0 (unknown) as worst: a real measurement always ranks above it.
			iZero, jZero := vi <= 0, vj <= 0
			if iZero != jZero {
				return jZero // the non-zero run comes first
			}
			return vi < vj
		}
		if higher {
			return vi > vj
		}
		return vi < vj
	})
	return out
}

// primaryMetricLabel returns just the label of a mode's primary metric.
func primaryMetricLabel(mode benchmark.Mode) string {
	r := benchmark.Run{Mode: mode}
	return primaryMetric(r).Label
}

// keyHistory handles the interactive history view: up/down move the cursor,
// enter opens the selected run's detail, m toggles the trend metric, esc goes
// back to the dashboard.
func (p BenchmarkPage) keyHistory(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.view = bvDashboard
	case "up", "k":
		if p.histCursor > 0 {
			p.histCursor--
		}
	case "down", "j":
		if p.histCursor < len(p.historyRuns)-1 {
			p.histCursor++
		}
	case "g", "home":
		p.histCursor = 0
	case "G", "end":
		if len(p.historyRuns) > 0 {
			p.histCursor = len(p.historyRuns) - 1
		}
	case "m":
		p.histMetric = (p.histMetric + 1) % 2
	case "enter":
		cur := p.histCursor
		if cur >= 0 && cur < len(p.historyRuns) {
			r := p.historyRuns[cur]
			p.detail = &r
			p.view = bvRunDetail
		}
	}
	return p, nil
}

func (p BenchmarkPage) viewHistory() string {
	if len(p.historyRuns) == 0 {
		return theme.Subtitle.Render("no history")
	}
	title := theme.Title.Render("History — " + p.historyRuns[0].ProfileName)
	metricLabel := "solve"
	if p.histMetric == 1 {
		metricLabel = "tok/s"
	}
	modeW := 16
	if p.width > 0 {
		modeW = min(16, max(8, p.width-60))
	}
	header := theme.Subtitle.Render(fmt.Sprintf("%-19s  %-*s  %8s  %8s  %7s  %8s",
		"when", modeW, "mode", metricLabel, "tok/s", "TTFT", "vram"))
	rows := []string{header}
	cur := p.histCursor
	if cur >= len(p.historyRuns) {
		cur = len(p.historyRuns) - 1
	}
	for i, r := range p.historyRuns {
		a := r.Aggregate
		m := primaryMetric(r)
		if p.histMetric == 1 {
			m.Text = fmt.Sprintf("%.1f", a.AvgTokensPerSecond)
		}
		cursor := "  "
		if i == cur {
			cursor = "> "
		}
		line := fmt.Sprintf("%s%-19s  %-*s  %8s  %8.1f  %5.0fms  %6dMB",
			cursor, r.StartedAt.Format("2006-01-02 15:04"), modeW, truncate(r.Mode.Title(), modeW),
			m.Text, a.AvgTokensPerSecond, a.AvgTTFTms, a.PeakVRAMMB)
		if i == cur && !theme.NoColor() {
			line = theme.Selected.Render(line)
		}
		rows = append(rows, line)
	}
	spark := p.historySparkline()
	return lipgloss.JoinVertical(lipgloss.Left, title, strings.Join(rows, "\n"), "", spark)
}

// historySparkline renders the oldest→newest trend with min/max labels and a
// metric toggle (0 = primary, 1 = tok/s). Returns "" for <2 runs.
func (p BenchmarkPage) historySparkline() string {
	runs := p.historyRuns
	if len(runs) < 2 {
		return ""
	}
	vals := make([]float64, len(runs))
	label := "solve trend (old→new)"
	if p.histMetric == 1 {
		label = "tok/s trend (old→new)"
		for i, r := range runs {
			vals[i] = r.Aggregate.AvgTokensPerSecond
		}
	} else {
		for i, r := range runs {
			vals[i] = primaryMetric(r).Raw
		}
	}
	scale := 1.0
	minV, maxV := vals[0], vals[0]
	for _, v := range vals {
		if v > scale {
			scale = v
		}
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	bars := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	// runs is newest-first; walk in reverse for chronological order.
	for i := len(runs) - 1; i >= 0; i-- {
		frac := vals[i] / scale
		idx := int(frac * float64(len(bars)-1))
		idx = max(0, min(idx, len(bars)-1))
		b.WriteRune(bars[idx])
	}
	return theme.Subtitle.Render(fmt.Sprintf("%s: %s   min %.2f  max %.2f", label, b.String(), minV, maxV))
}

// sparkTrend renders an oldest→newest trend line. The metric depends on the most
// recent run's mode: throughput (llama-bench) runs trend on tokens/second
// (normalized by the series max), every other mode trends on solve-rate (0..1).
func sparkTrend(runs []benchmark.Run) string {
	if len(runs) < 2 {
		return ""
	}
	throughput := runs[0].Mode == benchmark.ModeLlamaBench

	// Collect the metric per run and the normalization scale.
	vals := make([]float64, len(runs))
	scale := 1.0
	label := "solve-rate trend (old→new): "
	if throughput {
		label = "tok/s trend (old→new): "
		for i, r := range runs {
			vals[i] = r.Aggregate.AvgTokensPerSecond
			if vals[i] > scale {
				scale = vals[i]
			}
		}
	} else {
		for i, r := range runs {
			vals[i] = r.Aggregate.SolveRate
		}
	}

	// bars are multibyte runes: index against the rune slice length, never the
	// byte length, or WriteRune panics for fractions above ~30%.
	bars := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	// runs is newest-first; walk in reverse for chronological order.
	for i := len(runs) - 1; i >= 0; i-- {
		frac := vals[i] / scale
		idx := int(frac * float64(len(bars)-1))
		idx = max(0, min(idx, len(bars)-1))
		b.WriteRune(bars[idx])
	}
	return theme.Subtitle.Render(label + b.String())
}

package pages

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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
	p.cmpCursor = 0
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
	p.histCursor = 0
	return p, nil
}

func (p BenchmarkPage) viewCompare() string {
	title := theme.Title.Render("Compare profiles (latest complete run per mode)")
	rows := p.compareRows()
	var body []string
	cursorLine, flat := 0, 0
	for si, sec := range p.compareSections {
		ceil := benchmark.CeilingsFor(p.runs, sec.Mode)
		cols := compareCols(p.width, sec.Mode, p.compareMetric)
		body = append(body, "", theme.Subtitle.Render(sec.Mode.Title()), theme.Subtitle.Render("  "+renderHeader(cols)))
		for _, row := range rows {
			if row.sectionIdx != si {
				continue
			}
			if flat == p.cmpCursor {
				cursorLine = len(body)
			}
			body = append(body, p.compareRowLine(cols, row, flat == p.cmpCursor, ceil))
			flat++
		}
	}
	return p.composeWindowed([]string{title}, body, nil, cursorLine)
}

// benchCompareRow is one flattened compare row: a run within a section, its bar
// fraction hint + pre-formatted metric text, and whether it tops its section.
type benchCompareRow struct {
	sectionIdx int
	best       bool
	run        benchmark.Run
	frac       float64
	text       string
}

// compareRows flattens every section's ranked runs into one cursor space, in the
// order viewCompare renders them, so cmpCursor and [enter]→detail line up.
func (p BenchmarkPage) compareRows() []benchCompareRow {
	var out []benchCompareRow
	for si, sec := range p.compareSections {
		runs := sortedCompareRuns(sec, p.compareMetric)
		for i, r := range runs {
			frac, text, higher := compareMetricValue(r, p.compareMetric, sec.Mode)
			out = append(out, benchCompareRow{
				sectionIdx: si,
				best:       i == 0 && len(runs) > 1 && higher,
				run:        r,
				frac:       frac,
				text:       text,
			})
		}
	}
	return out
}

// compareRowLine renders one flattened compare row through fitColumns, with a
// leading ▲ best / > cursor gutter and a metric bar sized to its column.
func (p BenchmarkPage) compareRowLine(cols []benchCol, row benchCompareRow, cursor bool, ceil benchmark.Ceilings) string {
	a := row.run.Aggregate
	frac := normalizeCompareFrac(row.frac, p.compareMetric, row.run.Mode, ceil)
	metricW := 20
	if len(cols) > 1 {
		metricW = cols[1].w
	}
	barCells := max(3, metricW-theme.RuneWidth(row.text)-1)
	perf := map[string]string{
		"tok/s": fmt.Sprintf("%.1f", a.AvgTokensPerSecond),
		"TTFT":  fmt.Sprintf("%.0fms", a.AvgTTFTms),
		"vram":  fmt.Sprintf("%dMB", a.PeakVRAMMB),
		"quant": dash(row.run.Profile.Quantization),
	}
	cells := make([]string, len(cols))
	for i, c := range cols {
		switch i {
		case 0:
			cells[i] = row.run.ProfileName
		case 1:
			cells[i] = components.MetricBar(frac, barCells) + " " + row.text
		default:
			cells[i] = perf[c.title]
		}
	}
	line := compareMarker(row.best, cursor) + renderCells(cols, cells)
	if cursor && !theme.NoColor() {
		line = theme.Selected.Render(line)
	}
	return line
}

// compareMarker is the 2-cell row gutter: ▲ marks the section best, > the
// cursor (best wins the glyph; the cursor also gets a color highlight).
func compareMarker(best, cursor bool) string {
	switch {
	case best:
		return "▲ "
	case cursor:
		return "> "
	default:
		return "  "
	}
}

// compareCols builds the compare columns for the width, reserving a 2-cell
// marker gutter. profile flexes; the metric bar is fixed; perf columns shed
// right-to-left as width shrinks.
func compareCols(width int, mode benchmark.Mode, metric int) []benchCol {
	title := compareMetricLabelFor(mode, metric)
	if width <= 0 {
		return []benchCol{
			{title: "profile", w: 20},
			{title: title, w: 24},
			{title: "tok/s", w: 8, right: true},
			{title: "TTFT", w: 7, right: true},
			{title: "vram", w: 8, right: true},
			{title: "quant", w: 8},
		}
	}
	return fitColumns(width-2, []benchCol{
		{title: "profile", w: 0, prio: 0},
		{title: title, w: min(24, max(14, width/4)), prio: 0},
		{title: "tok/s", w: 8, prio: 2, right: true},
		{title: "TTFT", w: 7, prio: 3, right: true},
		{title: "vram", w: 8, prio: 4, right: true},
		{title: "quant", w: 8, prio: 5},
	})
}

// compareMetricLabelFor is the metric column header for the selected metric.
func compareMetricLabelFor(mode benchmark.Mode, metric int) string {
	switch metric {
	case 1:
		return "tok/s"
	case 2:
		return "TTFT↓"
	case 3:
		return "VRAM↓"
	default:
		return primaryMetricLabel(mode)
	}
}

// keyCompare drives the flattened compare cursor and [enter]→detail.
func (p BenchmarkPage) keyCompare(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := p.compareRows()
	switch msg.String() {
	case "esc":
		p.view = bvDashboard
	case "m":
		p.compareMetric = (p.compareMetric + 1) % 4
		p.cmpCursor = 0
	case "up", "k":
		if p.cmpCursor > 0 {
			p.cmpCursor--
		}
	case "down", "j":
		if p.cmpCursor < len(rows)-1 {
			p.cmpCursor++
		}
	case "g", "home":
		p.cmpCursor = 0
	case "G", "end":
		if len(rows) > 0 {
			p.cmpCursor = len(rows) - 1
		}
	case "enter":
		if p.cmpCursor >= 0 && p.cmpCursor < len(rows) {
			p = p.openDetail(rows[p.cmpCursor].run, bvCompare)
		}
	}
	return p, nil
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

// normalizeCompareFrac scales a raw metric value into [0,1] for the bar using
// data-relative ceilings: rate metrics (solve/recall) are already 0..1 so the
// raw value is the fill; throughput scales by the tok/s ceiling; TTFT/VRAM
// invert (lower fills more) against their ceilings. A lower-is-better value of
// 0 is treated as "not collected" and renders an empty bar, never a full one.
func normalizeCompareFrac(raw float64, metric int, mode benchmark.Mode, c benchmark.Ceilings) float64 {
	switch metric {
	case 1: // tok/s
		if c.TPS <= 0 {
			return 0
		}
		return raw / c.TPS
	case 2: // TTFT — lower is better
		if raw <= 0 || c.TTFTms <= 0 {
			return 0
		}
		return 1 - raw/c.TTFTms
	case 3: // VRAM — lower is better
		if raw <= 0 || c.VRAMMB <= 0 {
			return 0
		}
		return 1 - raw/c.VRAMMB
	default: // primary metric
		if primaryMetric(benchmark.Run{Mode: mode}).Rate {
			return raw
		}
		if c.TPS <= 0 {
			return 0
		}
		return raw / c.TPS
	}
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
			p = p.openDetail(p.historyRuns[cur], bvHistory)
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
	cols := fitColumns(p.width-2, []benchCol{
		{title: "when", w: 16, prio: 0},
		{title: "mode", w: 0, prio: 1},
		{title: metricLabel, w: 8, prio: 0, right: true},
		{title: "tok/s", w: 8, prio: 2, right: true},
		{title: "TTFT", w: 7, prio: 3, right: true},
		{title: "vram", w: 8, prio: 4, right: true},
	})
	cur := p.histCursor
	if cur >= len(p.historyRuns) {
		cur = len(p.historyRuns) - 1
	}
	rows := make([]string, 0, len(p.historyRuns))
	for i, r := range p.historyRuns {
		a := r.Aggregate
		m := primaryMetric(r)
		if p.histMetric == 1 {
			m.Text = fmt.Sprintf("%.1f", a.AvgTokensPerSecond)
		}
		perf := map[string]string{
			"tok/s": fmt.Sprintf("%.1f", a.AvgTokensPerSecond),
			"TTFT":  fmt.Sprintf("%.0fms", a.AvgTTFTms),
			"vram":  fmt.Sprintf("%dMB", a.PeakVRAMMB),
		}
		cells := make([]string, len(cols))
		for j, c := range cols {
			switch j {
			case 0:
				cells[j] = r.StartedAt.Format("2006-01-02 15:04")
			case 1:
				cells[j] = r.Mode.Title()
			case 2:
				cells[j] = m.Text
			default:
				cells[j] = perf[c.title]
			}
		}
		marker := "  "
		if i == cur {
			marker = "> "
		}
		line := marker + renderCells(cols, cells)
		if i == cur && !theme.NoColor() {
			line = theme.Selected.Render(line)
		}
		rows = append(rows, line)
	}
	header := theme.Subtitle.Render("  " + renderHeader(cols))
	return p.composeWindowed([]string{title, header}, rows, []string{"", p.historySparkline()}, cur)
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


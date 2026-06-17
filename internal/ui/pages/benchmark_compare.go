package pages

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
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
		p, _ = p.withFlashError("no runs to compare")
		return p, nil
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
		p, _ = p.withFlashError("no complete runs to compare")
		return p, nil
	}
	p.compareSections = sections
	p.view = bvCompare
	return p, nil
}

// openHistory shows every run of the selected run's profile over time.
func (p BenchmarkPage) openHistory() (tea.Model, tea.Cmd) {
	if p.runCursor >= len(p.runs) {
		p, _ = p.withFlashError("select a run first")
		return p, nil
	}
	target := p.runs[p.runCursor].ProfileID
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
		parts = append(parts, compareSectionRows(sec)...)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// compareSectionRows renders one mode section with columns that fit the mode:
// speed-only modes drop solve/score; the needle probe shows recall.
func compareSectionRows(sec benchCompareSection) []string {
	switch sec.Mode {
	case benchmark.ModeLlamaBench:
		rows := []string{theme.Subtitle.Render(fmt.Sprintf("%-20s  %8s  %10s  %7s  %8s  %s",
			"profile", "tok/s", "pp tok/s", "TTFT", "vram", "quant"))}
		for _, r := range sec.Runs {
			a := r.Aggregate
			rows = append(rows, fmt.Sprintf("%-20s  %8.1f  %10.1f  %5.0fms  %6dMB  %s",
				truncate(r.ProfileName, 20), a.AvgTokensPerSecond, a.AvgPromptProcessingTPS,
				a.AvgTTFTms, a.PeakVRAMMB, dash(r.Profile.Quantization)))
		}
		return rows
	case benchmark.ModeLongContext:
		rows := []string{theme.Subtitle.Render(fmt.Sprintf("%-20s  %6s  %8s  %7s  %8s  %s",
			"profile", "recall", "tok/s", "TTFT", "vram", "quant"))}
		for _, r := range sec.Runs {
			a := r.Aggregate
			rows = append(rows, fmt.Sprintf("%-20s  %5.0f%%  %8.1f  %5.0fms  %6dMB  %s",
				truncate(r.ProfileName, 20), a.AvgScore*100, a.AvgTokensPerSecond,
				a.AvgTTFTms, a.PeakVRAMMB, dash(r.Profile.Quantization)))
		}
		return rows
	default:
		rows := []string{theme.Subtitle.Render(fmt.Sprintf("%-20s  %6s  %6s  %8s  %7s  %8s  %s",
			"profile", "solve", "score", "tok/s", "TTFT", "vram", "quant"))}
		for _, r := range sec.Runs {
			a := r.Aggregate
			rows = append(rows, fmt.Sprintf("%-20s  %5.0f%%  %6.2f  %8.1f  %5.0fms  %6dMB  %s",
				truncate(r.ProfileName, 20), a.SolveRate*100, a.AvgScore, a.AvgTokensPerSecond,
				a.AvgTTFTms, a.PeakVRAMMB, dash(r.Profile.Quantization)))
		}
		return rows
	}
}

func (p BenchmarkPage) viewHistory() string {
	if len(p.historyRuns) == 0 {
		return theme.Subtitle.Render("no history")
	}
	title := theme.Title.Render("History — " + p.historyRuns[0].ProfileName)
	header := theme.Subtitle.Render(fmt.Sprintf("%-19s  %-16s  %6s  %6s  %8s  %7s  %8s",
		"when", "mode", "solve", "score", "tok/s", "TTFT", "vram"))
	rows := []string{header}
	for _, r := range p.historyRuns {
		a := r.Aggregate
		rows = append(rows, fmt.Sprintf("%-19s  %-16s  %5.0f%%  %6.2f  %8.1f  %5.0fms  %6dMB",
			r.StartedAt.Format("2006-01-02 15:04"), truncate(r.Mode.Title(), 16),
			a.SolveRate*100, a.AvgScore, a.AvgTokensPerSecond, a.AvgTTFTms, a.PeakVRAMMB))
	}
	spark := sparkTrend(p.historyRuns)
	return lipgloss.JoinVertical(lipgloss.Left, title, strings.Join(rows, "\n"), "", spark)
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

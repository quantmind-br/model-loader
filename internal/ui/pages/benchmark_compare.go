package pages

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// openCompare collects the most recent run per profile for a side-by-side
// comparison. p.runs is newest-first, so the first hit per profile wins.
func (p BenchmarkPage) openCompare() (tea.Model, tea.Cmd) {
	if len(p.runs) == 0 {
		p.flash, _ = flashError(p.flash, "no runs to compare")
		return p, nil
	}
	seen := map[string]bool{}
	latest := make([]benchmark.Run, 0)
	for _, r := range p.runs {
		if seen[r.ProfileID] {
			continue
		}
		seen[r.ProfileID] = true
		latest = append(latest, r)
	}
	p.compareRuns = latest
	p.view = bvCompare
	return p, nil
}

// openHistory shows every run of the selected run's profile over time.
func (p BenchmarkPage) openHistory() (tea.Model, tea.Cmd) {
	if p.runCursor >= len(p.runs) {
		p.flash, _ = flashError(p.flash, "select a run first")
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
	title := theme.Title.Render("Compare profiles (latest run each)")
	header := theme.Subtitle.Render(fmt.Sprintf("%-20s  %-16s  %6s  %6s  %8s  %7s  %8s  %s",
		"profile", "mode", "solve", "score", "tok/s", "TTFT", "vram", "quant"))
	rows := []string{header}
	for _, r := range p.compareRuns {
		a := r.Aggregate
		rows = append(rows, fmt.Sprintf("%-20s  %-16s  %5.0f%%  %6.2f  %8.1f  %5.0fms  %6dMB  %s",
			truncate(r.ProfileName, 20), truncate(r.Mode.Title(), 16),
			a.SolveRate*100, a.AvgScore, a.AvgTokensPerSecond, a.AvgTTFTms, a.PeakVRAMMB,
			dash(r.Profile.Quantization)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, strings.Join(rows, "\n"))
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
	spark := sparkSolveRate(p.historyRuns)
	return lipgloss.JoinVertical(lipgloss.Left, title, strings.Join(rows, "\n"), "", spark)
}

// sparkSolveRate renders an oldest→newest solve-rate trend line.
func sparkSolveRate(runs []benchmark.Run) string {
	if len(runs) < 2 {
		return ""
	}
	// bars are multibyte runes: index against the rune slice length, never the
	// byte length, or WriteRune panics for solve rates above ~30%.
	bars := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	// runs is newest-first; walk in reverse for chronological order.
	for i := len(runs) - 1; i >= 0; i-- {
		idx := int(runs[i].Aggregate.SolveRate * float64(len(bars)-1))
		idx = max(0, min(idx, len(bars)-1))
		b.WriteRune(bars[idx])
	}
	return theme.Subtitle.Render("solve-rate trend (old→new): " + b.String())
}

package pages

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/service/benchmarkstore"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// benchView is the Benchmark tab's screen state.
type benchView int

const (
	bvList        benchView = iota // runs list (+ selected run summary)
	bvProfilePick                  // choose a profile to benchmark
	bvModePick                     // choose scoring mode
	bvRunning                      // run in progress
	bvRunDetail                    // full metrics for one run
	bvCompare                      // latest run per profile, side by side
	bvHistory                      // runs of one profile over time
)

// benchModes is the selectable scoring-mode order in the mode picker.
var benchModes = []benchmark.Mode{
	benchmark.ModeJudge,
	benchmark.ModeLongContext,
	benchmark.ModeLlamaBench,
	benchmark.ModeMathBench,
}

// BenchmarkPage is the Benchmark tab: pick a profile, run the embedded
// SWE-bench-style mini-set, record metrics, and compare runs across profiles
// or over time.
type BenchmarkPage struct {
	store  profilestore.Store
	bstore benchmarkstore.Store
	runner *benchmark.Runner

	view    benchView
	width   int
	height  int
	flash   components.Flash
	spinner spinner.Model

	runs      []benchmark.Run
	runCursor int

	profiles   []domain.Profile
	profCursor int
	filter     string
	filterMode bool

	modeCursor        int
	selectedProfileID string

	// running state
	runCancel   context.CancelFunc
	progressCh  chan benchmark.Progress
	progress    benchmark.Progress
	runningMode benchmark.Mode
	runningName string

	// detail / compare / history
	detail      *benchmark.Run
	compareRuns []benchmark.Run
	historyRuns []benchmark.Run
}

// NewBenchmarkPage builds the page bound to the profile store, run store, and
// engine. A nil runner disables launching (the dataset failed to load).
func NewBenchmarkPage(store profilestore.Store, bstore benchmarkstore.Store, runner *benchmark.Runner) BenchmarkPage {
	return BenchmarkPage{
		store:   store,
		bstore:  bstore,
		runner:  runner,
		view:    bvList,
		flash:   components.NewFlash("benchmark"),
		spinner: components.NewLoadingSpinner(),
	}
}

func (p BenchmarkPage) Init() tea.Cmd {
	return tea.Batch(p.loadRunsCmd(), p.spinner.Tick)
}

// Reload refreshes the persisted runs when the tab gains focus.
func (p BenchmarkPage) Reload() tea.Cmd { return p.loadRunsCmd() }

// IsCapturingInput claims global keys whenever a modal-like view is active or
// the user is typing a filter, so [1-5]/[q]/[tab] don't get stolen mid-flow.
func (p BenchmarkPage) IsCapturingInput() bool {
	return p.view != bvList || p.filterMode
}

func (p BenchmarkPage) Hints() string {
	switch p.view {
	case bvProfilePick:
		if p.filterMode {
			return "[type] filter  [/] exit filter  [enter] select  [esc] cancel"
		}
		return "[↑↓] move  [/] filter  [enter] choose  [esc] cancel"
	case bvModePick:
		return "[↑↓] move  [enter] run  [esc] cancel"
	case bvRunning:
		return "running… [esc] cancel"
	case bvRunDetail:
		return "[esc] back"
	case bvCompare:
		return "[esc] back"
	case bvHistory:
		return "[esc] back"
	default:
		hints := "[b] run  [enter] details  [c] compare  [x] delete  [r] reload"
		if len(p.runs) > 0 {
			hints += "  [h] history"
		}
		return hints
	}
}

func (p BenchmarkPage) View() string {
	var body string
	switch p.view {
	case bvProfilePick:
		body = p.viewProfilePick()
	case bvModePick:
		body = p.viewModePick()
	case bvRunning:
		body = p.viewRunning()
	case bvRunDetail:
		body = p.viewRunDetail()
	case bvCompare:
		body = p.viewCompare()
	case bvHistory:
		body = p.viewHistory()
	default:
		body = p.viewList()
	}
	if fv := p.flash.View(); fv != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, body, fv)
	}
	return body
}

// --- list view -------------------------------------------------------------

func (p BenchmarkPage) viewList() string {
	title := theme.Title.Render("Benchmark runs")
	if p.runner == nil {
		return lipgloss.JoinVertical(lipgloss.Left, title,
			theme.Error.Render("benchmark dataset failed to load — see logs"))
	}
	if len(p.runs) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, title,
			components.EmptyState("No benchmark runs yet", "Press [b] to evaluate a profile"))
	}
	header := theme.Subtitle.Render(fmt.Sprintf("%-16s  %-20s  %-18s  %7s  %8s  %8s",
		"when", "profile", "mode", "solve", "tok/s", "vram"))
	rows := []string{header}
	for i, r := range p.runs {
		rows = append(rows, p.runRow(i, r))
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, strings.Join(rows, "\n"))
}

func (p BenchmarkPage) runRow(i int, r benchmark.Run) string {
	row := fmt.Sprintf("%-16s  %-20s  %-18s  %6.0f%%  %8.1f  %6dMB",
		r.StartedAt.Format("01-02 15:04"),
		truncate(r.ProfileName, 20),
		truncate(r.Mode.Title(), 18),
		r.Aggregate.SolveRate*100,
		r.Aggregate.AvgTokensPerSecond,
		r.Aggregate.PeakVRAMMB,
	)
	if i == p.runCursor {
		if theme.NoColor() {
			return "> " + row
		}
		return theme.Selected.Render(row)
	}
	return "  " + row
}

// --- profile picker --------------------------------------------------------

func (p BenchmarkPage) viewProfilePick() string {
	title := theme.Title.Render("Pick a profile to benchmark")
	filtered := p.filteredProfiles()
	if len(filtered) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, title,
			components.EmptyState("No profiles", "Create one in the Profiles tab"))
	}
	rows := make([]string, 0, len(filtered))
	for i, prof := range filtered {
		line := fmt.Sprintf("%-28s  %s", truncate(prof.Name, 28), truncate(prof.Model, 40))
		if i == p.profCursor {
			if theme.NoColor() {
				line = "> " + line
			} else {
				line = theme.Selected.Render(line)
			}
		} else {
			line = "  " + line
		}
		rows = append(rows, line)
	}
	parts := []string{title}
	if p.filterMode || p.filter != "" {
		parts = append(parts, theme.Subtitle.Render(fmt.Sprintf("filter: %q", p.filter)))
	}
	parts = append(parts, strings.Join(rows, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// --- mode picker -----------------------------------------------------------

func (p BenchmarkPage) viewModePick() string {
	title := theme.Title.Render("Scoring mode")
	descs := map[benchmark.Mode]string{
		benchmark.ModeJudge:       "SWE-bench Lite; reference-guided LLM judge, median of N samples (needs benchmark.judge config)",
		benchmark.ModeLongContext: "needle retrieval in a long prompt — objective diagnostic of KV-cache-quant decay",
		benchmark.ModeLlamaBench:  "throughput probe (llama-bench style): TTFT + tokens/s on fixed-size prompts",
		benchmark.ModeMathBench:   "math reasoning (GSM8K): exact numeric match, accuracy under quantization",
	}
	rows := make([]string, 0, len(benchModes))
	for i, m := range benchModes {
		line := fmt.Sprintf("%-22s  %s", m.Title(), descs[m])
		if i == p.modeCursor {
			if theme.NoColor() {
				line = "> " + line
			} else {
				line = theme.Selected.Render(line)
			}
		} else {
			line = "  " + line
		}
		rows = append(rows, line)
	}
	return lipgloss.JoinVertical(lipgloss.Left, title,
		theme.Subtitle.Render("profile: "+p.runningName), strings.Join(rows, "\n"))
}

// --- running ---------------------------------------------------------------

func (p BenchmarkPage) viewRunning() string {
	title := theme.Title.Render("Running benchmark")
	line := components.LoadingLine(p.spinner, fmt.Sprintf("%s — %s", p.runningName, p.runningMode.Title()), 0)
	prog := p.progress
	status := "preparing…"
	switch prog.Phase {
	case "launch":
		status = "launching backend / waiting for /health…"
	case "infer", "score":
		status = fmt.Sprintf("problem %d/%d: %s (%s)", prog.Index, prog.Total, prog.ProblemName, prog.Phase)
	case "done":
		status = "aggregating…"
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, line, theme.Subtitle.Render(status))
}

// --- run detail ------------------------------------------------------------

func (p BenchmarkPage) viewRunDetail() string {
	if p.detail == nil {
		return theme.Subtitle.Render("no run selected")
	}
	r := *p.detail
	title := theme.Title.Render(fmt.Sprintf("%s — %s", r.ProfileName, r.Mode.Title()))
	a := r.Aggregate
	meta := []string{
		theme.Subtitle.Render(r.StartedAt.Format("2006-01-02 15:04:05")),
		fmt.Sprintf("model: %s", r.Profile.Model),
		fmt.Sprintf("quant: %s   cache k/v: %s/%s   ctx: %d",
			dash(r.Profile.Quantization), dash(r.Profile.CacheTypeK), dash(r.Profile.CacheTypeV), r.Profile.CtxSize),
	}
	summary := fmt.Sprintf(
		"solve %.0f%% (%d/%d)   avg score %.2f   tok/s %.1f   TTFT %.0fms   tokens in/out %d/%d   peak VRAM %dMB   GPU %.0f%%",
		a.SolveRate*100, a.Resolved, a.Total, a.AvgScore, a.AvgTokensPerSecond, a.AvgTTFTms,
		a.TotalPromptTokens, a.TotalCompletionTokens, a.PeakVRAMMB, a.AvgGPUUtil)
	if r.Err != "" {
		summary = theme.Error.Render("run error: " + r.Err)
	}

	header := theme.Subtitle.Render(fmt.Sprintf("%-24s  %-8s  %6s  %7s  %7s  %s",
		"problem", "result", "score", "tok/s", "TTFT", "detail"))
	rows := []string{header}
	for _, pr := range r.Problems {
		result := theme.Error.Render("fail")
		if pr.Resolved {
			result = theme.OK.Render("pass")
		}
		detail := pr.Detail
		if pr.Err != "" {
			detail = "err: " + pr.Err
		}
		rows = append(rows, fmt.Sprintf("%-24s  %-8s  %5.2f  %7.1f  %5dms  %s",
			truncate(pr.ProblemName, 24), result, pr.Score, pr.TokensPerSecond, pr.TTFTms, truncate(detail, 40)))
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		strings.Join(meta, "\n"),
		summary,
		"",
		strings.Join(rows, "\n"),
	)
}

// --- shared helpers --------------------------------------------------------

func (p BenchmarkPage) filteredProfiles() []domain.Profile {
	if p.filter == "" {
		return p.profiles
	}
	q := strings.ToLower(p.filter)
	out := make([]domain.Profile, 0, len(p.profiles))
	for _, prof := range p.profiles {
		if strings.Contains(strings.ToLower(prof.Name), q) || strings.Contains(strings.ToLower(prof.Model), q) {
			out = append(out, prof)
		}
	}
	return out
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

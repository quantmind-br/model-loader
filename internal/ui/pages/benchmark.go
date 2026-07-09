package pages

import (
	"context"
	"fmt"
	"regexp"
	"sort"
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
	bvDashboard benchView = iota // leaderboard dashboard (+ selected run insight)
	bvWizard                     // unified profile→mode→review run wizard
	bvRunning                    // run in progress
	bvRunDetail                  // full metrics for one run
	bvCompare                    // latest run per profile, side by side
	bvHistory                    // runs of one profile over time
)

// benchWizardStep is the sub-step within the bvWizard view.
type benchWizardStep int

const (
	wizProfile benchWizardStep = iota // pick a profile (filterable)
	wizMode                           // pick a scoring mode (cards)
	wizReview                         // confirm before launching
)

// benchModes is the selectable scoring-mode order in the mode picker, derived
// from the benchmark package's canonical registry order (modes of the same
// category are contiguous so the picker renders one header per group).
var benchModes = benchmark.ModesInOrder()

// BenchmarkPage is the Benchmark tab: pick a profile, run the embedded
// SWE-bench-style mini-set, record metrics, and compare runs across profiles
// or over time.
type BenchmarkPage struct {
	store     profilestore.Store
	bstore    benchmarkstore.Store
	runner    *benchmark.Runner
	exportDir string

	view    benchView
	width   int
	height  int
	flash   components.Flash
	spinner spinner.Model

	// deleteConfirm gates the destructive [X] delete-run action behind a
	// yes/no modal (default Cancel), matching every other delete in the app
	// (DESTRUCT-01).
	deleteConfirm components.Confirm

	// cancelConfirm gates the in-flight [esc] run cancellation behind a
	// confirm so a stray esc does not throw away a long-running benchmark.
	cancelConfirm components.Confirm

	runs []benchmark.Run

	profiles   []domain.Profile
	profCursor int
	filter     string
	filterMode bool

	modeCursor        int
	selectedProfileID string
	wizStep           benchWizardStep // current sub-step of the bvWizard view

	// running state
	runCancel   context.CancelFunc
	runDone     chan struct{}
	progressCh  chan benchmark.Progress
	progress    benchmark.Progress
	runningMode benchmark.Mode
	runningName string

	// detail / compare / history
	detail          *benchmark.Run
	compareSections []benchCompareSection
	historyRuns     []benchmark.Run

	// dashboard state (render uses focusMode + dashCursor; catCursor is the
	// selected category in the category bar). All passive here; wired in a
	// later task.
	focusMode  benchmark.Mode
	dashCursor int
	catCursor  int

	// compareMetric selects which metric the compare view ranks and bars on.
	// 0 = the mode's primary metric (default); others cycle perf metrics.
	compareMetric int

	// histMetric toggles the history sparkline between the mode's primary
	// metric (0) and tok/s (1).
	histMetric int
	histCursor int
}

// NewBenchmarkPage builds the page bound to the profile store, run store, and
// engine. A nil runner disables launching (the dataset failed to load).
func NewBenchmarkPage(store profilestore.Store, bstore benchmarkstore.Store, runner *benchmark.Runner, exportDir string) BenchmarkPage {
	return BenchmarkPage{
		store:     store,
		bstore:    bstore,
		runner:    runner,
		exportDir: exportDir,
		view:      bvDashboard,
		flash:     components.NewFlash("benchmark"),
		spinner:   components.NewLoadingSpinner(),
	}
}

func (p BenchmarkPage) Init() tea.Cmd {
	// No spinner tick here: startRun schedules it when entering bvRunning, and
	// the TickMsg handler stops re-arming once the run view is left.
	return p.loadRunsCmd()
}

// Reload refreshes the persisted runs when the tab gains focus.
func (p BenchmarkPage) Reload() tea.Cmd { return p.loadRunsCmd() }

// IsCapturingInput claims global keys whenever a modal-like view is active or
// the user is typing a filter, so [1-5]/[q]/[tab] don't get stolen mid-flow.
func (p BenchmarkPage) IsCapturingInput() bool {
	return p.deleteConfirm.Active() || p.cancelConfirm.Active() || p.view != bvDashboard && p.view != bvWizard || p.filterMode
}

// StatusMessage implements ui.StatusMessageProvider: the page's flash also
// lands in the always-visible status bar, level included.
func (p BenchmarkPage) StatusMessage() (string, components.StatusLevel) {
	return p.flash.Current()
}

func (p BenchmarkPage) withFlash(msg string) (BenchmarkPage, tea.Cmd) {
	var cmd tea.Cmd
	p.flash, cmd = flashSuccess(p.flash, msg)
	return p, cmd
}

func (p BenchmarkPage) withFlashError(msg string) (BenchmarkPage, tea.Cmd) {
	var cmd tea.Cmd
	p.flash, cmd = flashError(p.flash, msg)
	return p, cmd
}

func (p BenchmarkPage) Hints() string {
	if p.deleteConfirm.Active() {
		return components.ConfirmHints
	}
	switch p.view {
	case bvWizard:
		switch p.wizStep {
		case wizProfile:
			if p.filterMode {
				return "[type] filter  [/] exit filter  [enter] select  [esc] cancel"
			}
			return "[↑↓] move  [/] filter  [enter] next  [esc] cancel"
		case wizMode:
			return "[↑↓] move  [enter] review  [esc] back"
		default: // wizReview
			return "[enter] run  [esc] back"
		}
	case bvRunning:
		return "running… [esc] cancel"
	case bvRunDetail:
		return "[E] export  [esc] back"
	case bvCompare:
		return "[m] metric  [esc] back"
	case bvHistory:
		return "[↑↓] run  [enter] details  [m] metric  [esc] back"
	default:
		hints := "[b] run  [←→] mode  [enter] details  [c] compare  [E] export  [X] del  [R] reload"
		if len(p.runs) > 0 {
			hints += "  [h] history"
		}
		return hints
	}
}

func (p BenchmarkPage) View() string {
	var body string
	switch p.view {
	case bvRunning:
		body = p.viewRunning()
	case bvRunDetail:
		body = p.viewRunDetail()
	case bvCompare:
		body = p.viewCompare()
	case bvHistory:
		body = p.viewHistory()
	case bvWizard:
		body = p.viewWizard()
	default:
		body = p.viewDashboard()
	}
	if fv := p.flash.View(); fv != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, body, fv)
	}
	return body
}

// OverlayView renders the delete-run confirm through the shared centered
// Modal overlay so it composites opaquely over the run list (DESTRUCT-01).
func (p BenchmarkPage) OverlayView() Overlay {
	if p.deleteConfirm.Active() {
		content := components.Modal("Delete benchmark run", p.deleteConfirm.View(), p.width, p.height)
		return Overlay{Content: content, Width: p.width, Height: p.height, Active: true}
	}
	if p.cancelConfirm.Active() {
		content := components.Modal("Cancel benchmark run", p.cancelConfirm.View(), p.width, p.height)
		return Overlay{Content: content, Width: p.width, Height: p.height, Active: true}
	}
	return Overlay{}
}

// --- running ---------------------------------------------------------------

func (p BenchmarkPage) viewRunning() string {
	title := theme.Title.Render("Running benchmark")
	head := fmt.Sprintf("%s — %s", p.runningName, p.runningMode.Title())
	line := components.LoadingLine(p.spinner, head, 0)
	prog := p.progress

	// Phase label — keeps the engine's literal phase strings ("launch" |
	// "infer" | "score" | "done") as the contract; only the display is nicer.
	phaseLabel := map[string]string{
		"launch": "Launching backend / waiting for /health…",
		"infer":  "Inferring",
		"score":  "Scoring",
		"done":   "Aggregating…",
	}[prog.Phase]
	if phaseLabel == "" {
		phaseLabel = "Preparing…"
	}

	parts := []string{title, line, theme.Subtitle.Render(phaseLabel)}

	// Progress bar + count. Throughput/agentic modes may report Total==0
	// (unknown denominator); in that case show the index without a bar.
	if prog.Total > 0 {
		frac := float64(prog.Index) / float64(prog.Total)
		if frac > 1 {
			frac = 1
		}
		barW := 30
		if p.width > 0 {
			barW = p.width / 2
			if barW < 16 {
				barW = 16
			}
			if barW > 50 {
				barW = 50
			}
		}
		bar := components.MetricBar(frac, barW)
		parts = append(parts, fmt.Sprintf("%s  %d/%d", bar, prog.Index, prog.Total))
	} else if prog.Index > 0 {
		parts = append(parts, fmt.Sprintf("item %d", prog.Index))
	}

	// Current problem name when inferring/scoring.
	if (prog.Phase == "infer" || prog.Phase == "score") && prog.ProblemName != "" {
		parts = append(parts, theme.Subtitle.Render("→ "+truncate(prog.ProblemName, min(60, max(16, p.width-4)))))
	}
	parts = append(parts, theme.Subtitle.Render("[esc] cancel"))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// --- run detail ------------------------------------------------------------

func (p BenchmarkPage) viewRunDetail() string {
	if p.detail == nil {
		return theme.Subtitle.Render("no run selected")
	}
	r := *p.detail
	title := theme.Title.Render(fmt.Sprintf("%s — %s", r.ProfileName, r.Mode.Title()))
	a := r.Aggregate
	instance := "launched fresh"
	if r.ReusedInstance {
		instance = "reused (warm — numbers may include other traffic)"
	}
	meta := []string{
		theme.Subtitle.Render(r.StartedAt.Format("2006-01-02 15:04:05")),
		fmt.Sprintf("model: %s", r.Profile.Model),
		fmt.Sprintf("quant: %s   cache k/v: %s/%s   ctx: %d",
			dash(r.Profile.Quantization), dash(r.Profile.CacheTypeK), dash(r.Profile.CacheTypeV), r.Profile.CtxSize),
		fmt.Sprintf("instance: %s", instance),
	}
	summary := fmt.Sprintf(
		"solve %.0f%% (%d/%d)   avg score %.2f   tok/s %.1f   TTFT %.0fms   tokens in/out %d/%d   peak VRAM %dMB   GPU %.0f%%",
		a.SolveRate*100, a.Resolved, a.Total, a.AvgScore, a.AvgTokensPerSecond, a.AvgTTFTms,
		a.TotalPromptTokens, a.TotalCompletionTokens, a.PeakVRAMMB, a.AvgGPUUtil)
	if a.Errored > 0 {
		summary += fmt.Sprintf("   errored %d", a.Errored)
	}
	if r.Err != "" {
		// Keep the partial metrics visible; the error rides alongside them.
		summary = theme.Error.Render("run incomplete: "+r.Err) + "\n" + summary
	}

	probW := 24
	detW := 40
	if p.width > 0 {
		probW = min(24, max(10, (p.width-36)*2/5))
		detW = min(40, max(10, p.width-36-probW))
	}
	header := theme.Subtitle.Render(fmt.Sprintf("%-*s  %-8s  %6s  %7s  %7s  %s",
		probW, "problem", "result", "score", "tok/s", "TTFT", "detail"))
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
		rows = append(rows, fmt.Sprintf("%-*s  %-8s  %5.2f  %7.1f  %5dms  %s",
			probW, truncate(pr.ProblemName, probW), result, pr.Score, pr.TokensPerSecond, pr.TTFTms, truncate(detail, detW)))
	}

	extra := modeDetailLines(r)
	sections := []string{title, strings.Join(meta, "\n"), p.renderScorecards(r), summary}
	if len(extra) > 0 {
		sections = append(sections, strings.Join(extra, "\n"))
	}
	sections = append(sections, "", strings.Join(rows, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderScorecards renders the primary metric + key performance metrics as
// proportional bars, giving the detail view a quick-glance visual layer above
// the dense numeric summary line. The primary metric's bar is normalized to
// [0,1] (rate modes) or scaled by the mode's max for throughput; the perf
// cards (tok/s, TTFT, VRAM) are scaled against representative ceilings so their
// bars stay meaningful across hardware.
func (p BenchmarkPage) renderScorecards(r benchmark.Run) string {
	a := r.Aggregate
	m := primaryMetric(r)
	barW := 16

	// Primary metric card.
	frac := m.Frac
	if frac == 0 && m.Raw > 0 {
		frac = m.Raw
	}
	primary := fmt.Sprintf("%-7s %s %6s", m.Label, components.MetricBar(frac, barW), m.Text)

	// Throughput card: scale tok/s against a 100 tok/s ceiling (clamped).
	tps := a.AvgTokensPerSecond
	tpsFrac := tps / 100
	if tpsFrac > 1 {
		tpsFrac = 1
	}
	throughput := fmt.Sprintf("%-7s %s %6.1f", "tok/s", components.MetricBar(tpsFrac, barW), tps)

	// TTFT card: lower is better — invert so a fast (low) TTFT fills more bar.
	// Ceiling 2000ms maps to empty; 0ms maps to full.
	ttft := a.AvgTTFTms
	ttftFrac := 1 - ttft/2000
	if ttftFrac < 0 {
		ttftFrac = 0
	}
	if ttft <= 0 {
		ttftFrac = 0 // unknown — show empty bar rather than misleading full
	}
	latency := fmt.Sprintf("%-7s %s %5.0fms", "TTFT", components.MetricBar(ttftFrac, barW), ttft)

	// VRAM card: scale against 24 GiB (reference rig ceiling), clamped.
	vram := float64(a.PeakVRAMMB) / 1024
	vramFrac := float64(a.PeakVRAMMB) / (24 * 1024)
	if vramFrac > 1 {
		vramFrac = 1
	}
	memory := fmt.Sprintf("%-7s %s %5.1fGB", "VRAM", components.MetricBar(vramFrac, barW), vram)

	return theme.Subtitle.Render(strings.Join([]string{primary, throughput, latency, memory}, "\n"))
}

// --- mode-specific detail helpers ------------------------------------------

var (
	mathDifficultyRe = regexp.MustCompile(`difficulty (\d+)\)`)
)

// modeDetailLines returns mode-specific metric/breakdown lines shown in the
// run detail view, below the generic summary.
func modeDetailLines(r benchmark.Run) []string {
	a := r.Aggregate
	var lines []string
	if a.AvgPromptProcessingTPS > 0 || a.AvgDecodeTPS > 0 {
		lines = append(lines, fmt.Sprintf("prefill %.1f tok/s   decode %.1f tok/s", a.AvgPromptProcessingTPS, a.AvgDecodeTPS))
	}
	if r.Mode == benchmark.ModeLlamaBench {
		if v := llamaBenchVarianceLine(r.Problems); v != "" {
			lines = append(lines, v)
		}
	}
	switch r.Mode {
	case benchmark.ModeMathBench:
		lines = append(lines, fmt.Sprintf("math accuracy %.0f%%", a.MathAccuracy*100))
		if b := mathDifficultyBreakdown(r.Problems); b != "" {
			lines = append(lines, "  "+b)
		}
	case benchmark.ModeCodeGenBench:
		executed, skipped := 0, 0
		for _, pr := range r.Problems {
			if pr.Err != "" {
				skipped++
			} else {
				executed++
			}
		}
		if executed == 0 {
			lines = append(lines, "code generation skipped (python3 not available)")
		} else {
			lines = append(lines, fmt.Sprintf("code pass rate %.0f%% (%d executed, %d skipped)", a.CodePassRate*100, executed, skipped))
		}
	case benchmark.ModeInstBench:
		lines = append(lines, fmt.Sprintf("instruction — format %.0f%%   refusal %.0f%%   consistency %.2f",
			a.InstFormatRate*100, a.InstRefusalRate*100, a.InstConsistency))
	case benchmark.ModeMMLUBench:
		lines = append(lines, fmt.Sprintf("MMLU accuracy %.0f%%", a.MMLUAccuracy*100))
		if b := mmluCategoryBreakdown(r.Problems); b != "" {
			lines = append(lines, "  "+b)
		}
	case benchmark.ModeRagasBench:
		lines = append(lines, fmt.Sprintf("RAG — faithfulness %.0f%%   relevancy %.0f%%   precision %.0f%%",
			a.RagasFaithfulness*100, a.RagasRelevancy*100, a.RagasPrecision*100))
	case benchmark.ModeSummaryBench:
		lines = append(lines, fmt.Sprintf("summarization coherence %.0f%%", a.SummaryCoherence*100))
	case benchmark.ModeTerminalBench:
		lines = append(lines, fmt.Sprintf("terminal-bench accuracy %.0f%% (%d/%d tasks resolved)",
			a.TerminalBenchAccuracy*100, a.Resolved, a.Total))
	case benchmark.ModeSweBenchPro:
		lines = append(lines, fmt.Sprintf("swe-bench-pro accuracy %.0f%% (%d/%d instances resolved)",
			a.SweBenchProAccuracy*100, a.Resolved, a.Total))
	}
	return lines
}

// llamaBenchVarianceLine summarizes the per-preset tok/s spread when the run
// recorded it (older runs have no TPSStdDev and render nothing).
func llamaBenchVarianceLine(problems []benchmark.ProblemResult) string {
	parts := make([]string, 0, len(problems))
	for _, pr := range problems {
		if pr.TPSMax == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s ±%.1f [%.1f–%.1f]", pr.ProblemName, pr.TPSStdDev, pr.TPSMin, pr.TPSMax))
	}
	if len(parts) == 0 {
		return ""
	}
	return "tok/s spread: " + strings.Join(parts, "   ")
}

// mathDifficultyBreakdown tallies solved/total per difficulty band, preferring
// the structured Difficulty field and falling back to parsing the Detail
// format "… (difficulty N)" for runs persisted before the field existed.
func mathDifficultyBreakdown(problems []benchmark.ProblemResult) string {
	type tally struct{ solved, total int }
	bands := map[string]*tally{}
	var order []string
	for _, pr := range problems {
		var d string
		if pr.Difficulty > 0 {
			d = fmt.Sprintf("%d", pr.Difficulty)
		} else if m := mathDifficultyRe.FindStringSubmatch(pr.Detail); m != nil {
			d = m[1]
		} else {
			continue
		}
		t, ok := bands[d]
		if !ok {
			t = &tally{}
			bands[d] = t
			order = append(order, d)
		}
		t.total++
		if pr.Resolved {
			t.solved++
		}
	}
	sort.Strings(order)
	parts := make([]string, 0, len(order))
	for _, d := range order {
		t := bands[d]
		parts = append(parts, fmt.Sprintf("difficulty %s: %d/%d", d, t.solved, t.total))
	}
	return strings.Join(parts, "   ")
}

// mmluCategoryBreakdown tallies solved/total per category, preferring the
// structured Category field and falling back to parsing the Detail format
// "category=<C> expected …" for runs persisted before the field existed.
func mmluCategoryBreakdown(problems []benchmark.ProblemResult) string {
	type tally struct{ solved, total int }
	cats := map[string]*tally{}
	var order []string
	for _, pr := range problems {
		cat := pr.Category
		if cat == "" {
			cat = parseMMLUCategory(pr.Detail)
		}
		if cat == "" {
			continue
		}
		t, ok := cats[cat]
		if !ok {
			t = &tally{}
			cats[cat] = t
			order = append(order, cat)
		}
		t.total++
		if pr.Resolved {
			t.solved++
		}
	}
	sort.Strings(order)
	parts := make([]string, 0, len(order))
	for _, c := range order {
		t := cats[c]
		parts = append(parts, fmt.Sprintf("%s: %d/%d", c, t.solved, t.total))
	}
	return strings.Join(parts, "   ")
}

// parseMMLUCategory extracts the category from "category=<C> expected …".
func parseMMLUCategory(detail string) string {
	const pfx = "category="
	if !strings.HasPrefix(detail, pfx) {
		return ""
	}
	rest := detail[len(pfx):]
	if i := strings.Index(rest, " expected"); i >= 0 {
		return rest[:i]
	}
	return ""
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

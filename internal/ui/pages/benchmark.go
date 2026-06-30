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
	bvList        benchView = iota // runs list (+ selected run summary)
	bvProfilePick                  // choose a profile to benchmark
	bvModePick                     // choose scoring mode
	bvRunning                      // run in progress
	bvRunDetail                    // full metrics for one run
	bvCompare                      // latest run per profile, side by side
	bvHistory                      // runs of one profile over time
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
	detail          *benchmark.Run
	compareSections []benchCompareSection
	historyRuns     []benchmark.Run
}

// NewBenchmarkPage builds the page bound to the profile store, run store, and
// engine. A nil runner disables launching (the dataset failed to load).
func NewBenchmarkPage(store profilestore.Store, bstore benchmarkstore.Store, runner *benchmark.Runner, exportDir string) BenchmarkPage {
	return BenchmarkPage{
		store:     store,
		bstore:    bstore,
		runner:    runner,
		exportDir: exportDir,
		view:      bvList,
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
	return p.deleteConfirm.Active() || p.view != bvList || p.filterMode
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
		return "[←→] choose  [enter] confirm  [esc] cancel"
	}
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
		return "[E] export  [esc] back"
	case bvCompare:
		return "[esc] back"
	case bvHistory:
		return "[esc] back"
	default:
		hints := "[b] run  [enter] details  [c] compare  [E] export  [X] del  [R] reload"
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

// OverlayView renders the delete-run confirm through the shared centered
// Modal overlay so it composites opaquely over the run list (DESTRUCT-01).
func (p BenchmarkPage) OverlayView() Overlay {
	if p.deleteConfirm.Active() {
		content := components.Modal("Delete benchmark run", p.deleteConfirm.View(), p.width, p.height)
		return Overlay{Content: content, Width: p.width, Height: p.height, Active: true}
	}
	return Overlay{}
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
	cols := benchListColumns(p.width)
	header := theme.Subtitle.Render(fmt.Sprintf("%-*s  %-*s  %-*s  %7s  %8s  %8s",
		cols.when, "when",
		cols.profile, "profile",
		cols.mode, "mode",
		"solve", "tok/s", "vram"))
	rows := []string{header}
	for i, r := range p.runs {
		rows = append(rows, p.runRow(i, r, cols))
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, strings.Join(rows, "\n"))
}

// benchListCols holds the flexed text widths for the variable-width
// columns in the benchmark run table. The numeric columns (solve/tok/s/vram)
// always reserve fixed widths so number alignment stays consistent.
type benchListCols struct {
	when    int
	profile int
	mode    int
}

// benchListColumns derives the variable-column widths from the available
// terminal width so the table fits at ≤80 cols without wrapping (F-08
// audit). The numeric tail (solve/tok/s/vram = 7+2+8+2+8 = 27 cols plus 4
// inter-column separators) is always reserved; the remaining width is
// distributed proportionally across when/profile/mode.
func benchListColumns(width int) benchListCols {
	const (
		numericTail  = 7 + 2 + 8 + 2 + 8 // "solve  tok/s  vram" columns
		interCol     = 2 * 5             // 5 inter-column "  " separators
		cursorGutter = 2                 // "> " / "  " prefix
	)
	avail := width - numericTail - interCol - cursorGutter
	if avail < 30 {
		avail = 30
	}
	whenW := 11 // matches MM-DD HH:MM format width
	flex := avail - whenW
	if flex < 16 {
		flex = 16
	}
	// 55% profile / 45% mode — mode strings ("SWE-bench Lite" etc.) are
	// shorter on average than profile names.
	profileW := flex * 55 / 100
	if profileW < 8 {
		profileW = 8
	}
	modeW := flex - profileW
	if modeW < 6 {
		modeW = 6
	}
	return benchListCols{when: whenW, profile: profileW, mode: modeW}
}

// listQualityCell renders the "solve" column for one run. Speed-only modes
// have no solve concept, so they show a dash instead of a misleading 100%.
func listQualityCell(r benchmark.Run) string {
	switch r.Mode {
	case benchmark.ModeLlamaBench:
		return fmt.Sprintf("%7s", "—")
	case benchmark.ModeLongContext:
		// Needle recall: AvgScore is the recovered fraction.
		return fmt.Sprintf("%6.0f%%", r.Aggregate.AvgScore*100)
	default:
		return fmt.Sprintf("%6.0f%%", r.Aggregate.SolveRate*100)
	}
}

func (p BenchmarkPage) runRow(i int, r benchmark.Run, cols benchListCols) string {
	mode := r.Mode.Title()
	if r.Err != "" {
		mode = "! " + mode // partial/failed run marker
	}
	row := fmt.Sprintf("%-*s  %-*s  %-*s  %s  %8.1f  %6dMB",
		cols.when, r.StartedAt.Format("01-02 15:04"),
		cols.profile, truncate(r.ProfileName, cols.profile),
		cols.mode, truncate(mode, cols.mode),
		listQualityCell(r),
		r.Aggregate.AvgTokensPerSecond,
		r.Aggregate.PeakVRAMMB,
	)
	if i == p.runCursor {
		if theme.NoColor() {
			return "> " + row
		}
		return theme.Selected.Render(row)
	}
	if r.Err != "" && !theme.NoColor() {
		return "  " + theme.Error.Render(row)
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
	if fl := components.FilterLine(p.filterMode, p.filter); fl != "" {
		parts = append(parts, fl)
	}
	parts = append(parts, strings.Join(rows, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// --- mode picker -----------------------------------------------------------

func (p BenchmarkPage) viewModePick() string {
	title := theme.Title.Render("Scoring mode")
	descs := map[benchmark.Mode]string{
		benchmark.ModeJudge:         "SWE-bench Lite; reference-guided LLM judge, median of N samples (needs benchmark.judge config)",
		benchmark.ModeLongContext:   "needle retrieval in a long prompt — objective diagnostic of KV-cache-quant decay",
		benchmark.ModeLlamaBench:    "throughput probe (llama-bench style): TTFT + tokens/s on fixed-size prompts",
		benchmark.ModeMathBench:     "math reasoning (GSM8K): exact numeric match, accuracy under quantization",
		benchmark.ModeCodeGenBench:  "code generation (HumanEval): sandboxed Pass@1; needs python3 on PATH",
		benchmark.ModeInstBench:     "instruction following: structured-format, refusal of disallowed prompts, and answer consistency",
		benchmark.ModeMMLUBench:     "factual knowledge (MMLU): multiple-choice exact-match across STEM/humanities/social/other",
		benchmark.ModeRagasBench:    "RAG quality (synthetic): grader scores faithfulness, answer relevancy, and context precision",
		benchmark.ModeSummaryBench:  "multi-doc summarization: fact coverage + grader-scored coherence",
		benchmark.ModeTerminalBench: "agentic terminal tasks via the external Terminal-Bench harness; needs the `tb` CLI + Docker (long-running)",
	}
	rows := make([]string, 0, len(benchModes)+4)
	var lastCat benchmark.Category
	for i, m := range benchModes {
		if c, ok := benchmark.CategoryOf(m); ok && c != lastCat {
			rows = append(rows, theme.Subtitle.Render(string(c)))
			lastCat = c
		}
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

	extra := modeDetailLines(r)
	sections := []string{title, strings.Join(meta, "\n"), summary}
	if len(extra) > 0 {
		sections = append(sections, strings.Join(extra, "\n"))
	}
	sections = append(sections, "", strings.Join(rows, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
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

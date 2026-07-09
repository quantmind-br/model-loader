package pages

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/service/benchmarkstore"
	"github.com/quantmind-br/model-loader/internal/service/configweb"
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
	bvProblem                    // one-problem drill-in (opened from run detail)
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
	feed        *benchmark.RunFeed
	runningMode benchmark.Mode
	runningName string

	// detail / compare / history
	detail          *benchmark.Run
	compareSections []benchCompareSection
	historyRuns     []benchmark.Run

	// run-detail navigation: per-problem cursor, sort cycle, and the view esc
	// returns to (set by every entry point). detailSort: 0 dataset order,
	// 1 score ascending (failures first), 2 score descending.
	probCursor int
	detailSort int
	detailFrom benchView

	// problem drill-in (bvProblem): scroll offset + lazily-loaded transcript.
	probScroll          int
	probTranscript      []benchmark.ProblemTranscript
	probTranscriptTried bool

	// dashboard state (render uses focusMode + dashCursor; catCursor is the
	// selected category in the category bar). All passive here; wired in a
	// later task.
	focusMode  benchmark.Mode
	dashCursor int
	catCursor  int

	// compareMetric selects which metric the compare view ranks and bars on.
	// 0 = the mode's primary metric (default); others cycle perf metrics.
	compareMetric int

	// cmpCursor is the flattened cursor across every compare section row.
	cmpCursor int

	// histMetric toggles the history sparkline between the mode's primary
	// metric (0) and tok/s (1).
	histMetric int
	histCursor int

	// web viewer (W): a read-only configweb benchmark browser + live monitor.
	// webViewing captures input (esc closes) while the browser holds focus.
	webViewer  *configweb.BenchViewer
	webViewing bool
	// webStarting guards the async startBenchWeb window so a second W press
	// can't launch (and orphan) a second viewer before webViewing flips.
	webStarting bool
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
	return p.deleteConfirm.Active() || p.cancelConfirm.Active() || p.view != bvDashboard || p.filterMode || p.webViewing
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
			return "[↑↓/jk] move  [g/G] top/bottom  [/] filter  [enter] next  [esc] cancel"
		case wizMode:
			return "[↑↓/jk] move  [enter] review  [esc] back"
		default: // wizReview
			return "[enter] run  [esc] back"
		}
	case bvRunning:
		return "running… [esc] cancel"
	case bvRunDetail:
		return "[↑↓/jk] problem  [g/G] top/bottom  [enter] drill-in  [s] sort  [E] export  [esc] back"
	case bvProblem:
		return "[↑↓/jk] scroll  [g/G] top/bottom  [esc] back"
	case bvCompare:
		return "[↑↓/jk] move  [g/G] top/bottom  [enter] details  [m] metric  [esc] back"
	case bvHistory:
		return "[↑↓/jk] run  [g/G] top/bottom  [enter] details  [m] metric  [esc] back"
	default:
		if p.webViewing {
			return "viewing in browser — [esc] close viewer"
		}
		hints := "[b] run  [←→/[]] mode  [enter] details  [c] compare  [W] web  [E] export  [X] del  [R] reload"
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
	case bvProblem:
		body = p.viewProblem()
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
	f := p.feed
	if f == nil && p.runner != nil {
		f = p.runner.Feed()
	}
	if f == nil {
		return p.clampBody(theme.Subtitle.Render("Preparing…"))
	}
	snap := f.Snapshot()
	now := time.Now()

	// 1. Header: ▶ profile — mode  ……  elapsed (right-aligned).
	name := snap.ProfileName
	if name == "" {
		name = p.runningName
	}
	mode := snap.Mode
	if mode == "" {
		mode = p.runningMode
	}
	header := rowLeftRight("▶ "+name+" — "+mode.Title(), "elapsed "+fmtDur(now.Sub(snap.StartedAt)), p.width)

	// 2. Progress bar + count + ✓/✗/! tallies.
	var prog string
	if snap.Total > 0 {
		frac := float64(snap.Index) / float64(snap.Total)
		if frac > 1 {
			frac = 1
		}
		barW := 30
		if p.width > 0 {
			barW = min(50, max(16, p.width/2))
		}
		prog = fmt.Sprintf("%s  %d/%d", components.MetricBar(frac, barW), snap.Index, snap.Total)
	} else {
		prog = fmt.Sprintf("item %d", snap.Index)
	}
	prog += "   " + tallyLine(snap)

	// 3. Now line: current item + phase + item elapsed + latest stream detail.
	var nowLine string
	if cur := snap.Current; cur != nil {
		nowLine = fmt.Sprintf("now: %s — %s %s", itemLabelText(cur), dash(cur.Phase), fmtDur(now.Sub(cur.StartedAt)))
		if s := latestStreamDetail(snap.Activity); s != "" {
			nowLine += "  " + s
		}
	} else {
		nowLine = map[string]string{
			"launch": "Launching backend / waiting for /health…",
			"infer":  "Inferring",
			"score":  "Scoring",
			"done":   "Aggregating…",
		}[snap.Phase]
	}

	// 4. Activity log (flexible height): newest at the bottom.
	activity := renderActivityLines(snap.Activity, p.width)

	// 5. Status: staleness wording (shared) + agentic watchdog countdown.
	lvl, since := snap.Staleness(now)
	statusText := benchmark.StalenessLabel(lvl, since)
	if snap.StallTimeout > 0 {
		if left := snap.StallTimeout - now.Sub(snap.LastItemDone); left > 0 {
			statusText += " — watchdog kill in " + fmtDur(left)
		}
	}
	if p.width > 0 {
		statusText = truncate(statusText, max(4, p.width))
	}
	status := stalenessStyle(lvl).Render(statusText)
	hints := theme.Subtitle.Render("[esc] cancel")

	// Assemble with a height-aware collapse order: shrink the activity log
	// (min 3) → drop the now-line → drop activity, always keeping the
	// header/progress/status/hints visible.
	top := []string{header, prog}
	bottom := []string{status, hints}
	if p.width <= 0 || p.height <= 0 {
		var mid []string
		if nowLine != "" {
			mid = append(mid, nowLine)
		}
		mid = append(mid, activity...)
		all := append(append(append([]string{}, top...), mid...), bottom...)
		return strings.Join(all, "\n")
	}
	midBudget := max(0, p.height-len(top)-len(bottom))
	var mid []string
	switch {
	case midBudget >= 4 && nowLine != "":
		mid = append(mid, nowLine)
		mid = append(mid, tailLines(activity, midBudget-1)...)
	case midBudget >= 1 && len(activity) > 0:
		mid = tailLines(activity, midBudget)
	case midBudget >= 1 && nowLine != "":
		mid = append(mid, nowLine)
	}
	all := append(append(append([]string{}, top...), mid...), bottom...)
	return p.clampBody(strings.Join(all, "\n"))
}

// tallyLine renders the ✓/✗/! pass/fail/error counters with outcome colors.
func tallyLine(s benchmark.FeedSnapshot) string {
	return theme.OK.Render(fmt.Sprintf("✓%d", s.Pass)) + " " +
		theme.Error.Render(fmt.Sprintf("✗%d", s.Fail)) + " " +
		theme.Warn.Render(fmt.Sprintf("!%d", s.Errored))
}

// itemLabelText prefers the item's human name, falling back to its id.
func itemLabelText(it *benchmark.ItemState) string {
	if it.Name != "" {
		return it.Name
	}
	return it.ID
}

// latestStreamDetail returns the newest token-stream heartbeat text, if any.
func latestStreamDetail(entries []benchmark.ActivityEntry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind == "stream" {
			return entries[i].Text
		}
	}
	return ""
}

// stalenessStyle maps a staleness level to its status-line style: OK dim,
// Quiet amber, Stalled red.
func stalenessStyle(l benchmark.StaleLevel) lipgloss.Style {
	switch l {
	case benchmark.StaleStalled:
		return theme.Error
	case benchmark.StaleQuiet:
		return theme.Warn
	default:
		return theme.Subtitle
	}
}

// activityGlyph returns the styled glyph for one activity entry (width 1):
// ✓/✗/! for finished items, → for item start, · for phase/harness/stream.
func activityGlyph(e benchmark.ActivityEntry) string {
	switch {
	case e.Kind == "item" && e.Outcome == "pass":
		return theme.OK.Render("✓")
	case e.Kind == "item" && e.Outcome == "fail":
		return theme.Error.Render("✗")
	case e.Kind == "item" && e.Outcome == "error":
		return theme.Warn.Render("!")
	case e.Kind == "item":
		return "→"
	default:
		return theme.Subtitle.Render("·")
	}
}

// renderActivityLines formats the activity ring as "HH:MM:SS <glyph> <text>",
// newest last, each truncated to the page width (glyph + timestamp chrome is
// width-stable, so the visible cells never exceed width).
func renderActivityLines(entries []benchmark.ActivityEntry, width int) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		text := e.Text
		if width > 0 {
			text = truncate(text, max(1, width-11)) // 8 ts + 2 spaces + 1 glyph
		}
		out = append(out, e.At.Format("15:04:05")+" "+activityGlyph(e)+" "+text)
	}
	return out
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

	// Per-problem table: sorted per detailSort, windowed around probCursor.
	probs := p.sortedProblems(r)
	cols := detailTableCols(p.width - 2)
	rows := make([]string, 0, len(probs))
	for i, pr := range probs {
		marker := "  "
		if i == p.probCursor {
			marker = "> "
		}
		line := marker + renderCells(cols, problemCells(cols, pr))
		if i == p.probCursor && !theme.NoColor() {
			line = theme.Selected.Render(line)
		}
		rows = append(rows, line)
	}
	if len(rows) == 0 {
		rows = []string{theme.Subtitle.Render("(no problems recorded)")}
	}

	extra := modeDetailLines(r)
	top := []string{title, strings.Join(meta, "\n"), p.renderScorecards(r), summary}
	if len(extra) > 0 {
		top = append(top, strings.Join(extra, "\n"))
	}
	top = append(top, "",
		theme.Subtitle.Render(fmt.Sprintf("problems (sort: %s)", detailSortLabel(p.detailSort))),
		theme.Subtitle.Render("  "+renderHeader(cols)))
	return p.composeWindowed(top, rows, nil, p.probCursor)
}

// sortedProblems returns the run's problems ordered per detailSort: 0 dataset
// order (as recorded), 1 score ascending (failures/lowest first), 2 descending.
func (p BenchmarkPage) sortedProblems(r benchmark.Run) []benchmark.ProblemResult {
	probs := make([]benchmark.ProblemResult, len(r.Problems))
	copy(probs, r.Problems)
	switch p.detailSort {
	case 1:
		sort.SliceStable(probs, func(i, j int) bool { return probs[i].Score < probs[j].Score })
	case 2:
		sort.SliceStable(probs, func(i, j int) bool { return probs[i].Score > probs[j].Score })
	}
	return probs
}

// detailSortLabel names the active per-problem sort for the table title.
func detailSortLabel(s int) string {
	switch s {
	case 1:
		return "score ↑"
	case 2:
		return "score ↓"
	default:
		return "dataset order"
	}
}

// detailTableCols builds the per-problem table columns for the available width.
// A single flexible text budget is split 40/60 between the problem name and the
// per-item detail; at narrow widths the metric columns shed (highest prio
// first) and the detail column drops entirely (its text lives in the drill-in).
func detailTableCols(width int) []benchCol {
	if width <= 0 {
		return []benchCol{
			{title: "problem", w: 24},
			{title: "result", w: 6},
			{title: "score", w: 6, right: true},
			{title: "tok/s", w: 7, right: true},
			{title: "TTFT", w: 7, right: true},
			{title: "detail", w: 40},
		}
	}
	fixed := []benchCol{
		{title: "result", w: 6, prio: 0},
		{title: "score", w: 6, prio: 1, right: true},
		{title: "tok/s", w: 7, prio: 2, right: true},
		{title: "TTFT", w: 7, prio: 3, right: true},
	}
	// Probe how much width the metric columns leave for one flexible text column.
	probe := fitColumns(width, append([]benchCol{{title: "text", w: 0, prio: 0}}, fixed...))
	flexW := 0
	kept := map[string]bool{}
	for _, c := range probe {
		kept[c.title] = true
		if c.title == "text" {
			flexW = c.w
		}
	}
	problemW, detailW := flexW, 0
	if flexW >= 26 {
		problemW = max(10, flexW*2/5)
		if d := flexW - problemW - 2; d >= 10 {
			detailW = d
		}
	}
	cols := []benchCol{{title: "problem", w: problemW}}
	for _, c := range fixed {
		if kept[c.title] {
			cols = append(cols, c)
		}
	}
	if detailW > 0 {
		cols = append(cols, benchCol{title: "detail", w: detailW})
	}
	return cols
}

// problemCells maps one problem's fields onto the (possibly shed) columns.
func problemCells(cols []benchCol, pr benchmark.ProblemResult) []string {
	result := "fail"
	switch {
	case pr.Err != "":
		result = "error"
	case pr.Resolved:
		result = "pass"
	}
	detail := pr.Detail
	if pr.Err != "" {
		detail = "err: " + pr.Err
	}
	values := map[string]string{
		"problem": pr.ProblemName,
		"result":  result,
		"score":   fmt.Sprintf("%.2f", pr.Score),
		"tok/s":   fmt.Sprintf("%.1f", pr.TokensPerSecond),
		"TTFT":    fmt.Sprintf("%dms", pr.TTFTms),
		"detail":  detail,
	}
	cells := make([]string, len(cols))
	for i, c := range cols {
		cells[i] = values[c.title]
	}
	return cells
}

// --- problem drill-in (bvProblem) ------------------------------------------

// viewProblem is the full-body drill-in for the problem under probCursor: every
// ProblemResult field plus a lazily-loaded transcript excerpt, scrolled by
// probScroll.
func (p BenchmarkPage) viewProblem() string {
	if p.detail == nil {
		return theme.Subtitle.Render("no run selected")
	}
	r := *p.detail
	probs := p.sortedProblems(r)
	if len(probs) == 0 {
		return p.clampBody(theme.Subtitle.Render("no problems recorded"))
	}
	idx := max(0, min(p.probCursor, len(probs)-1))
	title := theme.Title.Render(fmt.Sprintf("%s — problem %d/%d", r.ProfileName, idx+1, len(probs)))
	body := p.problemLines()
	if p.height > 0 {
		body = scrollWindow(body, p.probScroll, max(1, p.height-1))
	}
	return p.clampBody(strings.Join(append([]string{title}, body...), "\n"))
}

// problemLines renders every field of the drilled-in problem plus its
// transcript excerpt (cached in probTranscript). Used by both the view and the
// key handler (for scroll-offset clamping).
func (p BenchmarkPage) problemLines() []string {
	if p.detail == nil {
		return nil
	}
	probs := p.sortedProblems(*p.detail)
	if len(probs) == 0 {
		return nil
	}
	pr := probs[max(0, min(p.probCursor, len(probs)-1))]
	lines := problemDetailLines(pr)
	lines = append(lines, "", theme.Subtitle.Render("transcript"))
	return append(lines, p.transcriptExcerpt(pr)...)
}

// problemDetailLines renders all recorded fields of one ProblemResult.
func problemDetailLines(pr benchmark.ProblemResult) []string {
	outcome := "fail"
	switch {
	case pr.Err != "":
		outcome = "error"
	case pr.Resolved:
		outcome = "pass"
	}
	lines := []string{
		fmt.Sprintf("name:        %s", dash(pr.ProblemName)),
		fmt.Sprintf("id:          %s", dash(pr.ProblemID)),
		fmt.Sprintf("outcome:     %s   score %.3f", outcome, pr.Score),
	}
	if pr.FailPhase != "" {
		lines = append(lines, fmt.Sprintf("fail phase:  %s", pr.FailPhase))
	}
	var cls []string
	if pr.Kind != "" {
		cls = append(cls, "kind="+pr.Kind)
	}
	if pr.Category != "" {
		cls = append(cls, "category="+pr.Category)
	}
	if pr.Difficulty > 0 {
		cls = append(cls, fmt.Sprintf("difficulty=%d", pr.Difficulty))
	}
	if len(cls) > 0 {
		lines = append(lines, "class:       "+strings.Join(cls, "  "))
	}
	lines = append(lines,
		fmt.Sprintf("tokens:      in %d / out %d", pr.PromptTokens, pr.CompletionTokens),
		fmt.Sprintf("throughput:  %.1f tok/s   prefill %.1f   decode %.1f", pr.TokensPerSecond, pr.PromptProcessingTPS, pr.DecodeTPS),
		fmt.Sprintf("latency:     TTFT %dms   total %dms", pr.TTFTms, pr.TotalMs))
	if pr.TPSMax > 0 {
		lines = append(lines, fmt.Sprintf("tok/s spread: ±%.1f [%.1f–%.1f]", pr.TPSStdDev, pr.TPSMin, pr.TPSMax))
	}
	if len(pr.SubScores) > 0 {
		keys := make([]string, 0, len(pr.SubScores))
		for k := range pr.SubScores {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s %.2f", k, pr.SubScores[k]))
		}
		lines = append(lines, "sub-scores:  "+strings.Join(parts, "  "))
	}
	if pr.JudgedBy != "" {
		lines = append(lines, "judged by:   "+pr.JudgedBy)
	}
	if pr.SimMethod != "" {
		lines = append(lines, "similarity:  "+pr.SimMethod)
	}
	if pr.Sandbox != "" {
		lines = append(lines, "sandbox:     "+pr.Sandbox)
	}
	if pr.Seed != 0 {
		lines = append(lines, fmt.Sprintf("seed:        %d", pr.Seed))
	}
	if pr.FillPct != 0 {
		lines = append(lines, fmt.Sprintf("ctx fill:    %d%%", pr.FillPct))
	}
	if pr.Detail != "" {
		lines = append(lines, "detail:      "+pr.Detail)
	}
	if pr.Err != "" {
		lines = append(lines, theme.Error.Render("error:       "+pr.Err))
	}
	return lines
}

// transcriptExcerpt returns the model-response transcript lines for the problem,
// or the shared not-saved notice when no transcript was persisted for the run.
func (p BenchmarkPage) transcriptExcerpt(pr benchmark.ProblemResult) []string {
	if !p.probTranscriptTried {
		return []string{theme.Subtitle.Render("(loading…)")}
	}
	if len(p.probTranscript) == 0 {
		return []string{theme.Subtitle.Render("transcripts not saved for this run")}
	}
	for i := range p.probTranscript {
		if p.probTranscript[i].ProblemID != pr.ProblemID {
			continue
		}
		resp := strings.TrimSpace(p.probTranscript[i].ModelResponse)
		if resp == "" {
			return []string{theme.Subtitle.Render("(empty model response)")}
		}
		return strings.Split(resp, "\n")
	}
	return []string{theme.Subtitle.Render("no transcript for this problem")}
}

// scrollWindow returns an offset-anchored slice of lines fitting h rows, with
// dim "↑/↓ N more" markers (counted against h) when clipped. offset is clamped.
func scrollWindow(lines []string, offset, h int) []string {
	if h <= 0 || len(lines) <= h {
		return lines
	}
	offset = max(0, min(offset, len(lines)-1))
	avail := h
	top := offset > 0
	if top {
		avail--
	}
	end := offset + avail
	bottom := end < len(lines)
	if bottom {
		avail--
		end = offset + avail
		bottom = end < len(lines)
	}
	if end > len(lines) {
		end = len(lines)
	}
	out := make([]string, 0, h)
	if top {
		out = append(out, theme.Subtitle.Render(fmt.Sprintf("↑ %d more", offset)))
	}
	out = append(out, lines[offset:end]...)
	if bottom {
		out = append(out, theme.Subtitle.Render(fmt.Sprintf("↓ %d more", len(lines)-end)))
	}
	return out
}

// renderScorecards renders the primary metric + key performance metrics as
// proportional bars, giving the detail view a quick-glance visual layer above
// the dense numeric summary line. The primary metric's bar is normalized to
// [0,1] (rate modes) or scaled by the mode's max for throughput; the perf
// cards (tok/s, TTFT, VRAM) are scaled against representative ceilings so their
// bars stay meaningful across hardware.
func (p BenchmarkPage) renderScorecards(r benchmark.Run) string {
	a := r.Aggregate
	c := benchmark.CeilingsFor(p.runs, r.Mode)

	// Cards reflow by width: 4-across ≥100 cols, 2×2 at 60–99, vertical below.
	cardsPerRow := 1
	switch {
	case p.width >= 100:
		cardsPerRow = 4
	case p.width >= 60:
		cardsPerRow = 2
	}
	// Reserve label + inter-word spaces + the widest value cell ("…GB"/"…ms")
	// per card, plus 3-space inter-card gaps, so all cardsPerRow cards fit width
	// (fixes the 4-across row overflowing at 100-118 cols, UIUX-022).
	barW := 16
	if p.width > 0 {
		const fixedPerCard = 16
		gaps := (cardsPerRow - 1) * 3
		barW = min(24, max(6, (p.width-gaps-cardsPerRow*fixedPerCard)/cardsPerRow))
	}

	m := primaryMetric(r)
	pFrac := m.Frac
	if pFrac == 0 && m.Raw > 0 {
		pFrac = m.Raw
	}
	tps := a.AvgTokensPerSecond
	tpsFrac := 0.0
	if c.TPS > 0 {
		tpsFrac = tps / c.TPS
	}
	ttft := a.AvgTTFTms
	ttftFrac := 0.0
	if ttft > 0 && c.TTFTms > 0 {
		ttftFrac = 1 - ttft/c.TTFTms
	}
	vram := float64(a.PeakVRAMMB) / 1024
	vramFrac := 0.0
	if c.VRAMMB > 0 {
		vramFrac = float64(a.PeakVRAMMB) / c.VRAMMB
	}

	cards := []string{
		fmt.Sprintf("%-7s %s %6s", m.Label, components.MetricBar(pFrac, barW), m.Text),
		fmt.Sprintf("%-7s %s %6.1f", "tok/s", components.MetricBar(tpsFrac, barW), tps),
		fmt.Sprintf("%-7s %s %5.0fms", "TTFT", components.MetricBar(ttftFrac, barW), ttft),
		fmt.Sprintf("%-7s %s %5.1fGB", "VRAM", components.MetricBar(vramFrac, barW), vram),
	}
	rows := make([]string, 0, (len(cards)+cardsPerRow-1)/cardsPerRow)
	for start := 0; start < len(cards); start += cardsPerRow {
		rows = append(rows, strings.Join(cards[start:min(start+cardsPerRow, len(cards))], "   "))
	}
	return theme.Subtitle.Render(strings.Join(rows, "\n"))
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
	case benchmark.ModeDeepSWE:
		lines = append(lines, fmt.Sprintf("deep-swe accuracy %.0f%% (%d/%d tasks resolved)",
			a.DeepSWEAccuracy*100, a.Resolved, a.Total))
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

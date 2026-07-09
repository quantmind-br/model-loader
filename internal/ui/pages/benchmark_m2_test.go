package pages

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// manyProblems builds n distinct ProblemResults with long names/detail so table
// windowing and truncation get exercised.
func manyProblems(n int) []benchmark.ProblemResult {
	out := make([]benchmark.ProblemResult, 0, n)
	for i := range n {
		out = append(out, benchmark.ProblemResult{
			ProblemID:        fmt.Sprintf("prob-%d", i),
			ProblemName:      fmt.Sprintf("problem-with-a-fairly-long-descriptive-name-%d", i),
			Resolved:         i%2 == 0,
			Score:            float64(i) / float64(n),
			TokensPerSecond:  float64(30 + i),
			TTFTms:           int64(100 + i*5),
			TotalMs:          int64(2000 + i*100),
			PromptTokens:     100 + i,
			CompletionTokens: 200 + i,
			Detail:           fmt.Sprintf("some fairly long detail text describing the outcome of problem %d", i),
		})
	}
	return out
}

// populatedBenchPage returns a page with every view's state populated so the
// width/height property test can render each screen with realistic content.
func populatedBenchPage(t *testing.T) BenchmarkPage {
	t.Helper()
	p := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	now := time.Now()

	var runs []benchmark.Run
	for i := range 6 {
		runs = append(runs, benchmark.Run{
			ID: fmt.Sprintf("j%d", i), ProfileID: fmt.Sprintf("p%d", i),
			ProfileName: fmt.Sprintf("Profile Number %d", i), Mode: benchmark.ModeJudge, StartedAt: now,
			Aggregate: benchmark.Aggregate{
				Total: 3, Resolved: i % 4, SolveRate: float64(i) / 6,
				AvgTokensPerSecond: float64(20 + i*5), AvgTTFTms: float64(100 + i*10), PeakVRAMMB: uint64(4000 + i*500),
			},
		})
	}
	runs = append(runs, benchmark.Run{ID: "partial", ProfileID: "pp", ProfileName: "Partial One",
		Mode: benchmark.ModeJudge, Err: "cancelled", Aggregate: benchmark.Aggregate{Total: 3, Resolved: 1}})

	detail := benchmark.Run{
		ID: "detail", ProfileID: "pd", ProfileName: "Detailed Model With A Rather Long Name",
		Mode: benchmark.ModeJudge, StartedAt: now,
		Profile:   benchmark.ProfileSnapshot{Model: "/models/very/long/path/to/some-model-Q4_K_M.gguf", Quantization: "Q4_K_M", CtxSize: 8192},
		Aggregate: benchmark.Aggregate{Total: 12, Resolved: 7, SolveRate: 0.58, AvgScore: 0.6, AvgTokensPerSecond: 44, AvgTTFTms: 180, PeakVRAMMB: 9000, TotalPromptTokens: 1000, TotalCompletionTokens: 2000},
		Problems:  manyProblems(12),
	}
	runs = append(runs, detail)
	p.runs = runs

	d := detail
	p.detail = &d
	p.detailFrom = bvDashboard
	p.probTranscript = []benchmark.ProblemTranscript{{ProblemID: "prob-0", ModelResponse: strings.Repeat("a line of model transcript output\n", 20)}}
	p.probTranscriptTried = true

	for i := range 8 {
		p.profiles = append(p.profiles, domain.Profile{ID: fmt.Sprintf("p%d", i), Name: fmt.Sprintf("Profile Number %d", i), Model: fmt.Sprintf("/models/model-%d.gguf", i)})
	}
	p.runningName = "Detailed Model"

	p.compareSections = []benchCompareSection{
		{Mode: benchmark.ModeJudge, Runs: runs[:4]},
		{Mode: benchmark.ModeMMLUBench, Runs: []benchmark.Run{{ProfileName: "MM Model", Mode: benchmark.ModeMMLUBench, Aggregate: benchmark.Aggregate{SolveRate: 0.4, AvgTokensPerSecond: 33}}}},
	}
	p.historyRuns = runs[:6]

	p.feed = benchmark.NewFeedForTest(
		benchmark.FeedSnapshot{ProfileName: "Detailed Model", Mode: benchmark.ModeJudge, Total: 12, StartedAt: now},
		benchmark.Progress{Index: 1, Total: 12, ProblemID: "prob-0", ProblemName: "prob-0", Phase: "item_done", Outcome: "pass", Score: 1},
		benchmark.Progress{Index: 2, Total: 12, ProblemID: "prob-1", ProblemName: "prob-1", Phase: "item_done", Outcome: "fail"},
		benchmark.Progress{Index: 3, Total: 12, ProblemID: "prob-2", ProblemName: "problem number two", Phase: "infer"},
		benchmark.Progress{ProblemID: "judge", Phase: "activity", Detail: "harness: building docker image, layer 3/8, this is a fairly long harness output line"},
		benchmark.Progress{ProblemName: "prob-2", Phase: "activity", Detail: "streaming — 128 tok"},
	)
	return p
}

// TestBenchmarkViews_NeverExceedBudget is the resize regression net (plan 2.9):
// every benchmark view, at a range of widths and heights, must never emit a
// line wider than the width nor more rows than the height.
// UIUX-015: every benchmark view fits width×height (was silent bottom-clipping).
func TestBenchmarkViews_NeverExceedBudget(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	t.Cleanup(func() { t.Setenv("NO_COLOR", ""); theme.RebuildStyles() })

	base := populatedBenchPage(t)
	cases := []struct {
		name string
		view benchView
		step benchWizardStep
	}{
		{"dashboard", bvDashboard, wizProfile},
		{"running", bvRunning, wizProfile},
		{"detail", bvRunDetail, wizProfile},
		{"problem", bvProblem, wizProfile},
		{"compare", bvCompare, wizProfile},
		{"history", bvHistory, wizProfile},
		{"wizard-profile", bvWizard, wizProfile},
		{"wizard-mode", bvWizard, wizMode},
		{"wizard-review", bvWizard, wizReview},
	}
	for _, w := range []int{40, 60, 80, 120} {
		for _, h := range []int{10, 24} {
			for _, c := range cases {
				p := base
				p.width, p.height = w, h
				p.view = c.view
				p.wizStep = c.step
				out := p.View()
				lines := strings.Split(out, "\n")
				if len(lines) > h {
					t.Errorf("%s @%dx%d: %d lines exceed height %d", c.name, w, h, len(lines), h)
				}
				for i, ln := range lines {
					if lw := lipgloss.Width(ln); lw > w {
						t.Errorf("%s @%dx%d: line %d width %d exceeds %d: %q", c.name, w, h, i, lw, w, ln)
					}
				}
			}
		}
	}
}

func TestFitColumns_ShedsHighPrioAndResolvesFlex(t *testing.T) {
	cols := []benchCol{
		{title: "name", w: 0, prio: 0},
		{title: "a", w: 6, prio: 1},
		{title: "b", w: 6, prio: 2},
		{title: "c", w: 6, prio: 3},
	}
	// Wide: all survive; total fits; flex floored ≥10.
	got := fitColumns(60, cols)
	if len(got) != 4 {
		t.Fatalf("width 60: want 4 cols, got %d", len(got))
	}
	total := 0
	for i, c := range got {
		if i > 0 {
			total += 2
		}
		total += c.w
	}
	if total > 60 {
		t.Errorf("width 60: columns overflow (%d > 60)", total)
	}
	if got[0].w < 10 {
		t.Errorf("flex col below floor 10: %d", got[0].w)
	}

	// Narrow: highest-prio columns (c, then b) shed first; flex never drops.
	got2 := fitColumns(24, cols)
	for _, c := range got2 {
		if c.title == "c" || c.title == "b" {
			t.Errorf("width 24: high-prio col %q should have shed", c.title)
		}
	}
	total = 0
	for i, c := range got2 {
		if i > 0 {
			total += 2
		}
		total += c.w
	}
	if total > 24 {
		t.Errorf("width 24: columns overflow (%d > 24)", total)
	}
}

func TestVisibleWindow_KeepsCursorAndBudget(t *testing.T) {
	if s, e := visibleWindow(0, 5, 10); s != 0 || e != 5 {
		t.Errorf("fits: got [%d,%d), want [0,5)", s, e)
	}
	// Marker rows count against the budget: window + markers ≤ h.
	for _, cursor := range []int{0, 50, 99} {
		s, e := visibleWindow(cursor, 100, 10)
		if s < 0 || e > 100 || s > cursor || cursor >= e {
			t.Fatalf("cursor %d: window [%d,%d) does not contain it", cursor, s, e)
		}
		markers := 0
		if s > 0 {
			markers++
		}
		if e < 100 {
			markers++
		}
		if (e-s)+markers > 10 {
			t.Errorf("cursor %d: window %d + markers %d exceeds h=10", cursor, e-s, markers)
		}
	}
}

func TestWindowedRows_InsertsMarkers(t *testing.T) {
	rows := make([]string, 20)
	for i := range rows {
		rows[i] = fmt.Sprintf("row-%d", i)
	}
	out := windowedRows(rows, 19, 6)
	if len(out) > 6 {
		t.Fatalf("windowed rows %d exceed budget 6", len(out))
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "more") {
		t.Errorf("expected a '↑/↓ N more' marker in:\n%s", joined)
	}
	if !strings.Contains(joined, "row-19") {
		t.Errorf("cursor row-19 should be visible:\n%s", joined)
	}
}

func TestRenderCells_WidthAndAlignment(t *testing.T) {
	cols := []benchCol{{title: "a", w: 5}, {title: "b", w: 4, right: true}}
	out := renderCells(cols, []string{"x", "yy"})
	if w := lipgloss.Width(out); w != 11 {
		t.Errorf("row width %d, want 11 (5 + 2 gap + 4): %q", w, out)
	}
	if !strings.HasSuffix(out, "  yy") {
		t.Errorf("right-aligned cell should pad left: %q", out)
	}
}

// TestWizard_CapturesInputInAllSteps locks plan 2.7: the wizard (and every other
// non-dashboard view) captures global keys so q/1-5/tab no longer leak mid-flow.
// UIUX-014: wizard captures global keys so q/1-5/tab no longer leak mid-flow.
func TestWizard_CapturesInputInAllSteps(t *testing.T) {
	p := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	p.view = bvWizard
	for _, step := range []benchWizardStep{wizProfile, wizMode, wizReview} {
		p.wizStep = step
		if !p.IsCapturingInput() {
			t.Errorf("wizard step %d must capture input", step)
		}
	}
	for _, v := range []benchView{bvRunning, bvRunDetail, bvProblem, bvCompare, bvHistory} {
		p.view = v
		if !p.IsCapturingInput() {
			t.Errorf("view %d must capture input", v)
		}
	}
	p.view = bvDashboard
	if p.IsCapturingInput() {
		t.Error("dashboard must not capture input")
	}
}

// TestLiveRun_ShowsTalliesAndStaleness asserts the live-run view surfaces the
// ✓/✗/! tallies and the shared staleness wording from a stub feed.
func TestLiveRun_ShowsTalliesAndStaleness(t *testing.T) {
	old := time.Now().Add(-5 * time.Minute)
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.view = bvRunning
	page.feed = benchmark.NewFeedForTest(
		benchmark.FeedSnapshot{Mode: benchmark.ModeJudge, Total: 5, StartedAt: old},
		benchmark.Progress{At: old, Index: 1, Total: 5, ProblemID: "a", Phase: "item_done", Outcome: "pass", Score: 1},
		benchmark.Progress{At: old, Index: 2, Total: 5, ProblemID: "b", Phase: "item_done", Outcome: "fail"},
		benchmark.Progress{At: old, Index: 3, Total: 5, ProblemID: "c", Phase: "item_done", Outcome: "error"},
	)
	out := page.viewRunning()
	for _, want := range []string{"✓1", "✗1", "!1", "possibly hung"} {
		if !strings.Contains(out, want) {
			t.Fatalf("live-run view missing %q:\n%s", want, out)
		}
	}
}

// TestDetail_CursorSortDrillIn covers plan 2.4: per-problem cursor + sort cycle
// and the [enter] drill-in with its transcript notice, esc back to detail.
func TestDetail_CursorSortDrillIn(t *testing.T) {
	run := benchmark.Run{ID: "r", ProfileName: "P", Mode: benchmark.ModeJudge, Problems: manyProblems(4)}
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.view = bvRunDetail
	page.detail = &run

	m, _ := page.keyDetail(tea.KeyMsg{Type: tea.KeyDown})
	page = m.(BenchmarkPage)
	if page.probCursor != 1 {
		t.Fatalf("down: probCursor = %d, want 1", page.probCursor)
	}
	m, _ = page.keyDetail(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	page = m.(BenchmarkPage)
	if page.detailSort != 1 || page.probCursor != 0 {
		t.Fatalf("sort: detailSort=%d probCursor=%d, want 1/0", page.detailSort, page.probCursor)
	}
	m, _ = page.keyDetail(tea.KeyMsg{Type: tea.KeyEnter})
	page = m.(BenchmarkPage)
	if page.view != bvProblem {
		t.Fatalf("enter should open drill-in, view=%v", page.view)
	}
	out := page.viewProblem()
	if !strings.Contains(out, "transcripts not saved for this run") {
		t.Fatalf("drill-in should note missing transcripts:\n%s", out)
	}
	m, _ = page.keyProblem(tea.KeyMsg{Type: tea.KeyEsc})
	page = m.(BenchmarkPage)
	if page.view != bvRunDetail {
		t.Fatalf("esc should return to detail, view=%v", page.view)
	}
}

// TestCompare_CursorEnterEscReturns covers plan 2.5: the flattened compare
// cursor opens a run's detail, and esc returns to compare (detailFrom).
func TestCompare_CursorEnterEscReturns(t *testing.T) {
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.view = bvCompare
	page.compareSections = []benchCompareSection{{
		Mode: benchmark.ModeJudge,
		Runs: []benchmark.Run{
			{ID: "hi", ProfileName: "Hi", Mode: benchmark.ModeJudge, Aggregate: benchmark.Aggregate{SolveRate: 0.9}},
			{ID: "lo", ProfileName: "Lo", Mode: benchmark.ModeJudge, Aggregate: benchmark.Aggregate{SolveRate: 0.2}},
		},
	}}
	m, _ := page.keyCompare(tea.KeyMsg{Type: tea.KeyDown})
	page = m.(BenchmarkPage)
	if page.cmpCursor != 1 {
		t.Fatalf("down: cmpCursor = %d, want 1", page.cmpCursor)
	}
	m, _ = page.keyCompare(tea.KeyMsg{Type: tea.KeyEnter})
	page = m.(BenchmarkPage)
	if page.view != bvRunDetail || page.detailFrom != bvCompare {
		t.Fatalf("enter should open detail from compare; view=%v from=%v", page.view, page.detailFrom)
	}
	m, _ = page.keyDetail(tea.KeyMsg{Type: tea.KeyEsc})
	page = m.(BenchmarkPage)
	if page.view != bvCompare {
		t.Fatalf("esc from detail should return to compare, view=%v", page.view)
	}
}

// TestDashboard_WindowingShowsMarkers asserts a windowing marker appears when
// leaderboard rows exceed the height, and the insight panel stays pinned.
func TestDashboard_WindowingShowsMarkers(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	t.Cleanup(func() { t.Setenv("NO_COLOR", ""); theme.RebuildStyles() })

	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.focusMode = benchmark.ModeJudge
	for i := range 8 {
		page.runs = append(page.runs, benchmark.Run{
			ID: fmt.Sprintf("r%d", i), ProfileID: fmt.Sprintf("p%d", i), ProfileName: fmt.Sprintf("Prof%d", i),
			Mode: benchmark.ModeJudge, Aggregate: benchmark.Aggregate{SolveRate: float64(i) / 10},
		})
	}
	page.width, page.height = 80, 10
	page.dashCursor = 7
	out := page.viewDashboard()
	if !strings.Contains(out, "more") {
		t.Fatalf("expected a windowing marker with 8 rows in 10 lines:\n%s", out)
	}
	if !strings.Contains(out, "trend") {
		t.Fatalf("insight panel should stay pinned at the bottom:\n%s", out)
	}
}

package pages

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// TestWizard_ModeWindowKeepsCursorVisible locks UIUX-021: the mode list windows
// on modeCursor (mapped to its first physical line, since category headers and
// 2-line prereq cards make row index != physical-line index), so an agentic
// mode selected low in the list stays fully visible instead of clipping below
// the fold with no marker.
func TestWizard_ModeWindowKeepsCursorVisible(t *testing.T) {
	// UIUX-021
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	t.Cleanup(func() { t.Setenv("NO_COLOR", ""); theme.RebuildStyles() })

	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.view = bvWizard
	page.wizStep = wizMode
	page.runningName = "Demo"
	page.width, page.height = 80, 14

	// Point the cursor at deep-swe: an agentic mode near the end of the list
	// that would clip below the fold at this height without windowing.
	target := -1
	for i, m := range benchModes {
		if m == benchmark.ModeDeepSWE {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("deep-swe not present in benchModes")
	}
	page.modeCursor = target

	out := page.viewWizardMode()
	if lines := strings.Split(out, "\n"); len(lines) > page.height {
		t.Fatalf("mode step emitted %d lines, exceeds height %d", len(lines), page.height)
	}
	if !strings.Contains(out, "> "+benchmark.ModeDeepSWE.Title()) {
		t.Fatalf("selected deep-swe row must stay visible with its cursor marker:\n%s", out)
	}
	if !strings.Contains(out, "more") {
		t.Fatalf("a windowing marker must appear when the mode list exceeds height:\n%s", out)
	}
}

// TestScorecards_FitWidthBand locks UIUX-022: the scorecard row must fit within
// the terminal width for every width in the 100..121 band (the 4-across row was
// a constant 119 cells, clipping the VRAM card at 100-118 cols), and 4 cards
// must actually fit on one row at 100 cols.
func TestScorecards_FitWidthBand(t *testing.T) {
	// UIUX-022
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	t.Cleanup(func() { t.Setenv("NO_COLOR", ""); theme.RebuildStyles() })

	run := benchmark.Run{
		Mode: benchmark.ModeJudge,
		Aggregate: benchmark.Aggregate{
			Total: 12, Resolved: 7, SolveRate: 0.58, AvgScore: 0.6,
			AvgTokensPerSecond: 44, AvgTTFTms: 180, PeakVRAMMB: 9000,
		},
	}
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.runs = []benchmark.Run{run}

	for w := 100; w <= 121; w++ {
		page.width = w
		out := page.renderScorecards(run)
		for i, ln := range strings.Split(out, "\n") {
			if lw := lipgloss.Width(ln); lw > w {
				t.Errorf("scorecards @%d: line %d width %d exceeds %d: %q", w, i, lw, w, ln)
			}
		}
	}

	// At 100 cols the 4-across branch must place all four cards on one row.
	page.width = 100
	out := page.renderScorecards(run)
	if got := len(strings.Split(out, "\n")); got != 1 {
		t.Fatalf("100 cols should render 4 cards on one row, got %d rows:\n%s", got, out)
	}
	for _, lbl := range []string{"solve", "tok/s", "TTFT", "VRAM"} {
		if !strings.Contains(out, lbl) {
			t.Fatalf("100-col 4-across row missing the %q card:\n%s", lbl, out)
		}
	}
}

// TestRunDetail_ShowsCursorMarker locks UIUX-023: the per-problem table marks
// the cursor row so enter (drill-in) and s (sort) act on a visible row.
func TestRunDetail_ShowsCursorMarker(t *testing.T) {
	// UIUX-023
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	t.Cleanup(func() { t.Setenv("NO_COLOR", ""); theme.RebuildStyles() })

	run := benchmark.Run{ID: "r", ProfileName: "P", Mode: benchmark.ModeJudge, Problems: []benchmark.ProblemResult{
		{ProblemID: "a", ProblemName: "alpha", Resolved: true, Score: 1},
		{ProblemID: "b", ProblemName: "bravo", Resolved: false, Score: 0.2},
		{ProblemID: "c", ProblemName: "charlie", Resolved: true, Score: 0.7},
	}}
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.view = bvRunDetail
	page.detail = &run
	page.probCursor = 1 // dataset order (detailSort 0) -> bravo

	out := page.viewRunDetail()
	if !strings.Contains(out, "> bravo") {
		t.Fatalf("cursor row (bravo) must carry the '> ' marker:\n%s", out)
	}
	if strings.Contains(out, "> alpha") || strings.Contains(out, "> charlie") {
		t.Fatalf("only the cursor row may carry the marker:\n%s", out)
	}
}

// TestDashboard_DoubleWDoesNotRelaunch locks UIUX-024: a second W press during
// the async startBenchWeb window is ignored, so it can't launch (and orphan) a
// second viewer before webViewing flips.
func TestDashboard_DoubleWDoesNotRelaunch(t *testing.T) {
	// UIUX-024
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.view = bvDashboard

	m, cmd := page.keyDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'W'}})
	page = m.(BenchmarkPage)
	if !page.webStarting {
		t.Fatal("first W must set webStarting")
	}
	if cmd == nil {
		t.Fatal("first W must return the startBenchWeb command")
	}

	m, cmd2 := page.keyDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'W'}})
	page = m.(BenchmarkPage)
	if cmd2 != nil {
		t.Fatal("second W during startup must be a no-op (nil cmd), not a second launch")
	}
	if !page.webStarting {
		t.Fatal("webStarting must remain set after the ignored second W")
	}
}

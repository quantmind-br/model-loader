package pages

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

// fakeBStore is a minimal benchmarkstore.Store that records Save/Delete calls
// so tests can assert when the page persists or mutates runs.
type fakeBStore struct {
	deleted []string
	saved   []benchmark.Run
}

func (f *fakeBStore) Save(r benchmark.Run) error                    { f.saved = append(f.saved, r); return nil }
func (f *fakeBStore) List() ([]benchmark.Run, error)                { return nil, nil }
func (f *fakeBStore) ListByProfile(string) ([]benchmark.Run, error) { return nil, nil }
func (f *fakeBStore) Delete(id string) error                        { f.deleted = append(f.deleted, id); return nil }
func (f *fakeBStore) Load(id string) (benchmark.Run, error) {
	for _, r := range f.saved {
		if r.ID == id {
			return r, nil
		}
	}
	return benchmark.Run{}, errors.New("run not found")
}
func (f *fakeBStore) LoadTranscript(string) ([]benchmark.ProblemTranscript, error) {
	return nil, nil
}
func (f *fakeBStore) TranscriptPath(string) string { return "" }

// TestBenchmarkPage_ProfileFilterAcceptsSpaceAndBurst is a regression for
// TUI_AUDIT F-02: the wizard profile-step filter dropped the spacebar (it
// arrives as tea.KeySpace, not tea.KeyRunes) and also dropped multi-rune paste
// bursts (the old `len(Runes) == 1` guard).
func TestBenchmarkPage_ProfileFilterAcceptsSpaceAndBurst(t *testing.T) {
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.filterMode = true

	page, _, _ = page.keyProfileFilter(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Qwen")})
	page, _, _ = page.keyProfileFilter(tea.KeyMsg{Type: tea.KeySpace})
	page, _, _ = page.keyProfileFilter(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("27B")})

	if page.filter != "Qwen 27B" {
		t.Errorf("filter = %q, want %q (space and multi-rune burst must survive)", page.filter, "Qwen 27B")
	}
}

// TestBenchmarkPage_DeleteRunRequiresConfirm locks in DESTRUCT-01: uppercase
// 'X' opens a confirm and does NOT delete until the affirmative path runs,
// and lowercase 'x' is inert.
func TestBenchmarkPage_DeleteRunRequiresConfirm(t *testing.T) {
	bs := &fakeBStore{}
	page := NewBenchmarkPage(nil, bs, nil, t.TempDir())
	page.focusMode = benchmark.ModeJudge
	page.runs = []benchmark.Run{{ID: "run-1", ProfileID: "p1", Mode: benchmark.ModeJudge,
		Aggregate: benchmark.Aggregate{SolveRate: 0.5}}}

	m, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	page = m.(BenchmarkPage)
	if !page.deleteConfirm.Active() {
		t.Fatal("'X' should open the delete confirm")
	}
	if len(bs.deleted) != 0 {
		t.Fatalf("nothing should be deleted before confirmation; deleted=%v", bs.deleted)
	}
	if !page.IsCapturingInput() {
		t.Fatal("page should capture input while the confirm is open")
	}

	// Affirmative completion: mirror Confirm.Update clearing the form and
	// inject the msg its onYes callback would emit.
	page.deleteConfirm = components.Confirm{}
	m, _ = page.Update(benchmarkDeleteConfirmedMsg{id: "run-1"})
	page = m.(BenchmarkPage)
	if len(bs.deleted) != 1 || bs.deleted[0] != "run-1" {
		t.Fatalf("confirmed delete should call bstore.Delete(\"run-1\"); deleted=%v", bs.deleted)
	}

	// Lowercase 'x' must be inert (no confirm, no delete).
	bs2 := &fakeBStore{}
	page2 := NewBenchmarkPage(nil, bs2, nil, t.TempDir())
	page2.runs = []benchmark.Run{{ID: "run-2"}}
	m, _ = page2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	page2 = m.(BenchmarkPage)
	if page2.deleteConfirm.Active() {
		t.Fatal("lowercase 'x' must not open the delete confirm")
	}
	if len(bs2.deleted) != 0 {
		t.Fatalf("lowercase 'x' must not delete; deleted=%v", bs2.deleted)
	}
}

func TestBenchModesIncludeInstructionBench(t *testing.T) {
	found := false
	for _, m := range benchModes {
		if m == benchmark.ModeInstBench {
			found = true
		}
	}
	if !found {
		t.Fatal("benchModes must include ModeInstBench")
	}
}

func TestBenchModesIncludeMMLUBench(t *testing.T) {
	found := false
	for _, m := range benchModes {
		if m == benchmark.ModeMMLUBench {
			found = true
		}
	}
	if !found {
		t.Fatal("benchModes must include ModeMMLUBench")
	}
}

func TestBenchModesGroupedByCategory(t *testing.T) {
	// Once a category appears it must not reappear later (modes are contiguous
	// per category so the picker can show one header each).
	seen := map[benchmark.Category]bool{}
	var last benchmark.Category
	for i, m := range benchModes {
		c, ok := benchmark.CategoryOf(m)
		if !ok {
			t.Fatalf("mode %q has no category", m)
		}
		if i == 0 || c != last {
			if seen[c] {
				t.Fatalf("category %q is not contiguous in benchModes", c)
			}
			seen[c] = true
			last = c
		}
	}
}

func TestBenchModes_IncludesPhase3(t *testing.T) {
	want := map[benchmark.Mode]bool{benchmark.ModeRagasBench: false, benchmark.ModeSummaryBench: false}
	for _, m := range benchModes {
		if _, ok := want[m]; ok {
			want[m] = true
		}
	}
	for m, found := range want {
		if !found {
			t.Errorf("benchModes missing %q", m)
		}
	}
}

func TestBenchModes_Phase3AreQuality(t *testing.T) {
	for _, m := range []benchmark.Mode{benchmark.ModeRagasBench, benchmark.ModeSummaryBench} {
		c, ok := benchmark.CategoryOf(m)
		if !ok || c != benchmark.CatQuality {
			t.Errorf("CategoryOf(%q) = %v,%v; want Quality,true", m, c, ok)
		}
	}
}

// TestPrimaryMetric_PerModeText covers the per-mode headline cell: llama-bench
// shows tok/s, long-context shows recall %, judge shows solve %.
func TestPrimaryMetric_PerModeText(t *testing.T) {
	needle := benchmark.Run{Mode: benchmark.ModeLongContext, Aggregate: benchmark.Aggregate{AvgScore: 0.67}}
	if m := primaryMetric(needle); !strings.Contains(m.Text, "67%") {
		t.Errorf("longctx text = %q, want recall 67%%", m.Text)
	}
	judge := benchmark.Run{Mode: benchmark.ModeJudge, Aggregate: benchmark.Aggregate{SolveRate: 0.5}}
	if m := primaryMetric(judge); !strings.Contains(m.Text, "50%") {
		t.Errorf("judge text = %q, want 50%%", m.Text)
	}
	llama := benchmark.Run{Mode: benchmark.ModeLlamaBench, Aggregate: benchmark.Aggregate{AvgTokensPerSecond: 42}}
	if m := primaryMetric(llama); m.Label != "tok/s" || m.Text != "42.0" {
		t.Errorf("llama-bench = %+v, want label tok/s text 42.0", m)
	}
}

func TestBenchmarkSpinner_StopsOutsideRunning(t *testing.T) {
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.view = bvDashboard
	_, cmd := page.Update(spinner.TickMsg{})
	if cmd != nil {
		t.Fatal("spinner tick outside bvRunning must not re-arm")
	}
	page.view = bvRunning
	_, cmd = page.Update(spinner.TickMsg{})
	if cmd == nil {
		t.Fatal("spinner tick in bvRunning should re-arm")
	}
}

func TestHandleRunDone_SavesPartialRunWithProblems(t *testing.T) {
	bs := &fakeBStore{}
	page := NewBenchmarkPage(nil, bs, nil, t.TempDir())
	run := benchmark.Run{ID: "r1", Err: "context canceled",
		Problems: []benchmark.ProblemResult{{ProblemID: "p1", Resolved: true}}}
	m, _ := page.handleRunDone(benchRunDoneMsg{run: run, err: errors.New("context canceled")})
	page = m.(BenchmarkPage)
	if len(bs.saved) != 1 || bs.saved[0].ID != "r1" {
		t.Fatalf("partial run with problems should be saved; saved=%v", bs.saved)
	}
	if page.view != bvRunDetail || page.detail == nil {
		t.Fatal("partial run should open in the detail view")
	}
}

func TestHandleRunDone_DiscardsRunWithoutProblems(t *testing.T) {
	bs := &fakeBStore{}
	page := NewBenchmarkPage(nil, bs, nil, t.TempDir())
	m, _ := page.handleRunDone(benchRunDoneMsg{run: benchmark.Run{ID: "r2"}, err: errors.New("launch backend: boom")})
	page = m.(BenchmarkPage)
	if len(bs.saved) != 0 {
		t.Fatalf("launch failure (0 problems) must not be saved; saved=%v", bs.saved)
	}
	if page.view != bvDashboard {
		t.Fatal("launch failure should return to the dashboard view")
	}
}

func TestOpenCompare_GroupsByModeAndSkipsPartials(t *testing.T) {
	// newest-first, mixed modes; the partial mmlu run must be skipped in favor
	// of the older complete one.
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.runs = []benchmark.Run{
		{ID: "n1", ProfileID: "a", ProfileName: "A", Mode: benchmark.ModeMMLUBench, Err: "cancelled"},
		{ID: "n2", ProfileID: "a", ProfileName: "A", Mode: benchmark.ModeMMLUBench},
		{ID: "n3", ProfileID: "b", ProfileName: "B", Mode: benchmark.ModeMMLUBench},
		{ID: "n4", ProfileID: "a", ProfileName: "A", Mode: benchmark.ModeLlamaBench},
		{ID: "n5", ProfileID: "a", ProfileName: "A", Mode: benchmark.ModeMMLUBench}, // older, superseded by n2
	}
	m, _ := page.openCompare()
	page = m.(BenchmarkPage)
	if page.view != bvCompare {
		t.Fatal("openCompare should switch to the compare view")
	}
	if len(page.compareSections) != 2 {
		t.Fatalf("sections = %d, want 2 (llama-bench + mmlu)", len(page.compareSections))
	}
	// ModesInOrder puts Speed (llama-bench) before Knowledge (mmlu).
	if page.compareSections[0].Mode != benchmark.ModeLlamaBench || page.compareSections[1].Mode != benchmark.ModeMMLUBench {
		t.Fatalf("section order = %v/%v", page.compareSections[0].Mode, page.compareSections[1].Mode)
	}
	mmlu := page.compareSections[1].Runs
	if len(mmlu) != 2 {
		t.Fatalf("mmlu section runs = %d, want 2 (latest complete per profile)", len(mmlu))
	}
	for _, r := range mmlu {
		if r.ID == "n1" || r.ID == "n5" {
			t.Fatalf("partial (n1) or superseded (n5) run leaked into compare: %s", r.ID)
		}
	}
	out := page.viewCompare()
	if !strings.Contains(out, benchmark.ModeLlamaBench.Title()) || !strings.Contains(out, benchmark.ModeMMLUBench.Title()) {
		t.Fatalf("viewCompare missing mode headers:\n%s", out)
	}
	if strings.Contains(strings.Split(out, benchmark.ModeMMLUBench.Title())[0], "solve") {
		t.Error("llama-bench section should not have a solve column")
	}
}

// UIUX-022: g/G (and home/end) jump to the top/bottom of the dashboard
// leaderboard. The leaderboard collapses to one row per profile, so each test
// run uses a distinct profile to get distinct rows.
func TestBenchmarkPage_DashboardJumpKeys(t *testing.T) {
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.focusMode = benchmark.ModeJudge
	page.runs = []benchmark.Run{
		{ID: "r1", ProfileID: "p1", ProfileName: "P1", Mode: benchmark.ModeJudge,
			Aggregate: benchmark.Aggregate{SolveRate: 0.1}},
		{ID: "r2", ProfileID: "p2", ProfileName: "P2", Mode: benchmark.ModeJudge,
			Aggregate: benchmark.Aggregate{SolveRate: 0.2}},
		{ID: "r3", ProfileID: "p3", ProfileName: "P3", Mode: benchmark.ModeJudge,
			Aggregate: benchmark.Aggregate{SolveRate: 0.3}},
	}

	m, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	page = m.(BenchmarkPage)
	if page.dashCursor != 2 {
		t.Fatalf("dashCursor after G = %d, want 2", page.dashCursor)
	}

	m, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	page = m.(BenchmarkPage)
	if page.dashCursor != 0 {
		t.Fatalf("dashCursor after g = %d, want 0", page.dashCursor)
	}

	// G on an empty dashboard must not underflow the cursor.
	empty := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	m, _ = empty.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	empty = m.(BenchmarkPage)
	if empty.dashCursor != 0 {
		t.Fatalf("dashCursor after G on empty dashboard = %d, want 0", empty.dashCursor)
	}
}

// Right/left cycle the focused mode across modes that have runs, and enter on
// the dashboard opens the selected leaderboard row's run detail.
func TestBenchmarkPage_DashboardModeCycleAndEnter(t *testing.T) {
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.focusMode = benchmark.ModeJudge
	page.runs = []benchmark.Run{
		{ID: "judge", ProfileID: "p1", ProfileName: "Judge", Mode: benchmark.ModeJudge,
			Aggregate: benchmark.Aggregate{SolveRate: 0.5}},
		{ID: "mmlu", ProfileID: "p2", ProfileName: "MMLU", Mode: benchmark.ModeMMLUBench,
			Aggregate: benchmark.Aggregate{SolveRate: 0.7}},
	}

	m, _ := page.Update(tea.KeyMsg{Type: tea.KeyRight})
	page = m.(BenchmarkPage)
	if page.focusMode != benchmark.ModeMMLUBench {
		t.Fatalf("focusMode after right = %q, want mmlu-bench", page.focusMode)
	}

	m, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = m.(BenchmarkPage)
	if page.view != bvRunDetail || page.detail == nil || page.detail.ID != "mmlu" {
		t.Fatalf("enter should open mmlu detail; view=%v detail=%+v", page.view, page.detail)
	}
}

// UIUX-022: g/G jump in the wizard profile step, bounded by the filtered
// list; while filter mode is active they are text, not navigation.
func TestBenchmarkPage_WizardProfileJumpKeys(t *testing.T) {
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.view = bvWizard
	page.wizStep = wizProfile
	page.profiles = []domain.Profile{
		{ID: "a", Name: "Alpha"},
		{ID: "b", Name: "Beta"},
		{ID: "g1", Name: "Gamma"},
	}

	m, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	page = m.(BenchmarkPage)
	if page.profCursor != 2 {
		t.Fatalf("profCursor after G = %d, want 2", page.profCursor)
	}

	m, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	page = m.(BenchmarkPage)
	if page.profCursor != 0 {
		t.Fatalf("profCursor after g = %d, want 0", page.profCursor)
	}

	// G bounded by the filtered list, not the full profile slice.
	page.filter = "alpha"
	m, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	page = m.(BenchmarkPage)
	if page.profCursor != 0 {
		t.Fatalf("profCursor after G with filter %q = %d, want 0 (1 match)", page.filter, page.profCursor)
	}

	// In filter mode g/G append to the filter text instead of jumping.
	page.filter = ""
	page.filterMode = true
	page.profCursor = 0
	m, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	page = m.(BenchmarkPage)
	if page.filter != "g" {
		t.Fatalf("filter after typing g in filter mode = %q, want \"g\"", page.filter)
	}
	m, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	page = m.(BenchmarkPage)
	if page.filter != "gG" {
		t.Fatalf("filter after typing G in filter mode = %q, want \"gG\"", page.filter)
	}
	if page.profCursor != 0 {
		t.Fatalf("profCursor moved while typing in filter mode: %d", page.profCursor)
	}
}

// TestViewRunning_ShowsProgressBar asserts the running view renders a progress
// bar and the index/total when a progress event with a known total arrives.
func TestViewRunning_ShowsProgressBar(t *testing.T) {
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.view = bvRunning
	page.runningName = "Demo"
	page.runningMode = benchmark.ModeJudge
	// A live feed at 3/10 with the current item swe-001 inferring.
	page.feed = benchmark.NewFeedForTest(
		benchmark.FeedSnapshot{Mode: benchmark.ModeJudge, Total: 10, StartedAt: time.Now()},
		benchmark.Progress{Index: 3, Total: 10, ProblemID: "swe-001", ProblemName: "swe-001", Phase: "infer"},
	)
	out := page.viewRunning()
	for _, want := range []string{"3/10", "swe-001"} {
		if !strings.Contains(out, want) {
			t.Fatalf("running view missing %q:\n%s", want, out)
		}
	}
}

// TestRunning_EscArmsCancelConfirm asserts esc in bvRunning opens the
// cancelConfirm instead of cancelling directly.
func TestRunning_EscArmsCancelConfirm(t *testing.T) {
	cancelled := false
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.view = bvRunning
	page.runCancel = func() { cancelled = true }

	m, _ := page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = m.(BenchmarkPage)
	if !page.cancelConfirm.Active() {
		t.Fatal("esc in bvRunning should arm the cancel confirm")
	}
	if cancelled {
		t.Fatal("esc must NOT cancel directly — only the confirm's affirmative path does")
	}
	// UIUX-038: handleKey diverts every key into the confirm, so the footer
	// must advertise the confirm's keys instead of "running… [esc] cancel".
	if got := page.Hints(); got != components.ConfirmHints {
		t.Fatalf("Hints() = %q, want %q", got, components.ConfirmHints)
	}
}

// TestBenchmarkPage_CleanupCancelsRun guards audit N-C15: Cleanup on a page with
// an in-flight run cancels it and returns once the engine acknowledges; an idle
// page (nil runCancel) is a safe no-op.
func TestBenchmarkPage_CleanupCancelsRun(t *testing.T) {
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())

	canceled := false
	page.runCancel = func() { canceled = true }
	done := make(chan struct{})
	close(done) // engine already acknowledged the cancel
	page.runDone = done

	start := time.Now()
	page.Cleanup()
	if !canceled {
		t.Fatal("Cleanup did not call runCancel")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Cleanup blocked %v; runDone was closed, expected immediate return", elapsed)
	}

	// Idle page (no run in flight) must not panic.
	NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir()).Cleanup()
}

// UIUX-011: a run that dies before producing any problems surfaces an error
// flash, and handleRunDone MUST return the flash-clear cmd so bubbletea drives
// the auto-clear timer. The bug dropped that cmd (returned nil), so the error
// stayed on screen forever and stacked across retries. This pins the cmd being
// threaded through AND the FlashClearMsg it schedules actually clearing the
// flash.
func TestBenchmarkPage_RunFailureFlashAutoClears(t *testing.T) {
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())

	m, cmd := page.handleRunDone(benchRunDoneMsg{
		run: benchmark.Run{ID: "r2"},
		err: errors.New("load profile via proxy: not ready"),
	})
	page = m.(BenchmarkPage)

	// The core regression: dropping this cmd meant the flash never auto-cleared.
	if cmd == nil {
		t.Fatal("handleRunDone dropped the flash-clear cmd; error flash would never auto-clear")
	}

	msg, _ := page.StatusMessage()
	if !strings.Contains(msg, "load profile via proxy: not ready") {
		t.Fatalf("expected error flash to surface the failure; got %q", msg)
	}

	// Simulate the delayed clear tick without sleeping: the cmd is a real timer
	// (FlashLifetimeError), so calling it would block. Build the matching
	// FlashClearMsg from the queued item's identity and feed it through Update.
	items := page.flash.Items()
	if len(items) != 1 {
		t.Fatalf("expected exactly one queued flash item; got %d", len(items))
	}
	clearMsg := components.FlashClearMsg{Tag: "benchmark", Seq: items[0].Seq}
	mm, _ := page.Update(clearMsg)
	page = mm.(BenchmarkPage)

	if msg2, _ := page.StatusMessage(); msg2 != "" {
		t.Fatalf("flash should be cleared after its FlashClearMsg; got %q", msg2)
	}
}

package pages

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

// fakeBStore is a minimal benchmarkstore.Store that records Save/Delete calls
// so tests can assert when the page persists or mutates runs.
type fakeBStore struct {
	deleted []string
	saved   []benchmark.Run
}

func (f *fakeBStore) Save(r benchmark.Run) error                      { f.saved = append(f.saved, r); return nil }
func (f *fakeBStore) List() ([]benchmark.Run, error)                  { return nil, nil }
func (f *fakeBStore) ListByProfile(string) ([]benchmark.Run, error)   { return nil, nil }
func (f *fakeBStore) Delete(id string) error                          { f.deleted = append(f.deleted, id); return nil }
func (f *fakeBStore) LoadTranscript(string) ([]benchmark.ProblemTranscript, error) {
	return nil, nil
}
func (f *fakeBStore) TranscriptPath(string) string { return "" }

// TestBenchmarkPage_DeleteRunRequiresConfirm locks in DESTRUCT-01: uppercase
// 'X' opens a confirm and does NOT delete until the affirmative path runs,
// and lowercase 'x' is inert.
func TestBenchmarkPage_DeleteRunRequiresConfirm(t *testing.T) {
	bs := &fakeBStore{}
	page := NewBenchmarkPage(nil, bs, nil, t.TempDir())
	page.runs = []benchmark.Run{{ID: "run-1"}}

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

func TestViewModePickShowsCategoryHeaders(t *testing.T) {
	p := BenchmarkPage{view: bvModePick, runningName: "demo"}
	out := p.viewModePick()
	for _, h := range []string{"Quality", "Speed", "Robustness", "Knowledge"} {
		if !strings.Contains(out, h) {
			t.Fatalf("viewModePick output missing category header %q", h)
		}
	}
}

func TestBenchmarkSpinner_StopsOutsideRunning(t *testing.T) {
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.view = bvList
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
	if page.view != bvList {
		t.Fatal("launch failure should return to the list view")
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

func TestListQualityCell_PerMode(t *testing.T) {
	llama := benchmark.Run{Mode: benchmark.ModeLlamaBench, Aggregate: benchmark.Aggregate{SolveRate: 1}}
	if got := listQualityCell(llama); !strings.Contains(got, "—") {
		t.Errorf("llama-bench cell = %q, want a dash", got)
	}
	needle := benchmark.Run{Mode: benchmark.ModeLongContext, Aggregate: benchmark.Aggregate{AvgScore: 0.67}}
	if got := listQualityCell(needle); !strings.Contains(got, "67%") {
		t.Errorf("longctx cell = %q, want recall 67%%", got)
	}
	judge := benchmark.Run{Mode: benchmark.ModeJudge, Aggregate: benchmark.Aggregate{SolveRate: 0.5}}
	if got := listQualityCell(judge); !strings.Contains(got, "50%") {
		t.Errorf("judge cell = %q, want 50%%", got)
	}
}

func TestRunRow_FlagsPartialRuns(t *testing.T) {
	page := NewBenchmarkPage(nil, &fakeBStore{}, nil, t.TempDir())
	page.runs = []benchmark.Run{{ProfileName: "p", Mode: benchmark.ModeJudge, Err: "cancelled"}}
	page.runCursor = 1 // keep the row unselected so the marker is visible
	row := page.runRow(0, page.runs[0], benchListCols{when: 11, profile: 10, mode: 20})
	if !strings.Contains(row, "! ") {
		t.Errorf("partial run row should carry the '!' marker: %q", row)
	}
}

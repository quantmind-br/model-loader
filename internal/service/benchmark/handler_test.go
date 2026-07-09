package benchmark

import (
	"context"
	"testing"
)

func TestRegistry_LookupUnknown(t *testing.T) {
	if _, ok := handlerFor(Mode("does-not-exist")); ok {
		t.Fatal("handlerFor(unknown) returned ok=true")
	}
}

func TestRegistry_RegistersExistingModes(t *testing.T) {
	for _, m := range []Mode{ModeJudge, ModeLongContext, ModeLlamaBench} {
		if _, ok := handlerFor(m); !ok {
			t.Errorf("mode %q not registered", m)
		}
	}
}

func TestHandlerCount_MatchesRunner(t *testing.T) {
	r := &Runner{presets: []tpPreset{{FillPct: 50, GenTokens: 128}, {FillPct: 90, GenTokens: 256}}, problems: make([]Problem, 7)}
	if h, _ := handlerFor(ModeJudge); h.Count(r) != 7 {
		t.Errorf("judge count = %d, want 7", h.Count(r))
	}
	if h, _ := handlerFor(ModeLongContext); h.Count(r) != 1 {
		t.Errorf("longctx count = %d, want 1", h.Count(r))
	}
	if h, _ := handlerFor(ModeLlamaBench); h.Count(r) != 2 {
		t.Errorf("llama-bench count = %d, want 2", h.Count(r))
	}
}

func TestCountForMode_DelegatesToRegistry(t *testing.T) {
	r := &Runner{presets: []tpPreset{{FillPct: 50, GenTokens: 128}}, problems: make([]Problem, 3)}
	if got := r.CountForMode(ModeJudge); got != 3 {
		t.Errorf("CountForMode(judge) = %d, want 3", got)
	}
	if got := r.CountForMode(Mode("unknown")); got != 0 {
		t.Errorf("CountForMode(unknown) = %d, want 0", got)
	}
}

func TestModesInOrder_CoversEveryRegisteredMode(t *testing.T) {
	got := ModesInOrder()
	if len(got) != len(handlers) {
		t.Fatalf("ModesInOrder has %d modes, registry has %d — keep modeOrder in sync with registerHandler calls", len(got), len(handlers))
	}
	seen := map[Mode]bool{}
	for _, m := range got {
		if seen[m] {
			t.Fatalf("mode %q duplicated in modeOrder", m)
		}
		seen[m] = true
		if _, ok := handlerFor(m); !ok {
			t.Fatalf("mode %q in modeOrder but not registered", m)
		}
	}
}

func TestModesInOrder_CategoriesAreContiguous(t *testing.T) {
	seen := map[Category]bool{}
	var last Category
	for _, m := range ModesInOrder() {
		c, ok := CategoryOf(m)
		if !ok {
			t.Fatalf("mode %q has no category", m)
		}
		if c != last {
			if seen[c] {
				t.Fatalf("category %q is split: %q appears after the group ended", c, m)
			}
			seen[c] = true
			last = c
		}
	}
}

func TestCategoryOf(t *testing.T) {
	cases := map[Mode]Category{
		ModeJudge:        CatQuality,
		ModeMathBench:    CatQuality,
		ModeCodeGenBench: CatQuality,
		ModeLlamaBench:   CatSpeed,
		ModeLongContext:  CatRobustness,
		ModeInstBench:    CatRobustness,
		ModeMMLUBench:    CatKnowledge,
	}
	for m, want := range cases {
		got, ok := CategoryOf(m)
		if !ok {
			t.Fatalf("CategoryOf(%q): not found", m)
		}
		if got != want {
			t.Fatalf("CategoryOf(%q) = %q, want %q", m, got, want)
		}
	}
	if _, ok := CategoryOf("nope"); ok {
		t.Fatal("CategoryOf(unknown) should report not found")
	}
}

func TestExecuteSerialBench_EmitsItemDone(t *testing.T) {
	r := &Runner{}
	outcomes := []ProblemResult{
		{ProblemID: "p1", ProblemName: "n1", Resolved: true, Score: 1, TotalMs: 10},
		{ProblemID: "p2", ProblemName: "n2", Resolved: false, Score: 0, TotalMs: 20},
		{ProblemID: "p3", ProblemName: "n3", Err: "boom", TotalMs: 30},
	}
	progress := make(chan Progress, 100)
	meta := func(i int) (string, string) { return outcomes[i].ProblemID, outcomes[i].ProblemName }
	runOne := func(i int) (ProblemResult, ProblemTranscript) { return outcomes[i], ProblemTranscript{} }
	if _, _, err := executeSerialBench(context.Background(), r, progress, len(outcomes), meta, runOne); err != nil {
		t.Fatalf("executeSerialBench: %v", err)
	}
	close(progress)

	var done []Progress
	for p := range progress {
		if p.Phase == "item_done" {
			done = append(done, p)
		}
	}
	if len(done) != 3 {
		t.Fatalf("got %d item_done events, want 3", len(done))
	}
	want := []string{"pass", "fail", "error"}
	for i, p := range done {
		if p.Outcome != want[i] {
			t.Errorf("item %d outcome = %q, want %q", i, p.Outcome, want[i])
		}
		if p.ProblemID != outcomes[i].ProblemID {
			t.Errorf("item %d id = %q, want %q", i, p.ProblemID, outcomes[i].ProblemID)
		}
		if p.ItemMs != outcomes[i].TotalMs {
			t.Errorf("item %d ItemMs = %d, want %d", i, p.ItemMs, outcomes[i].TotalMs)
		}
		if p.Detail != outcomes[i].Err {
			t.Errorf("item %d Detail = %q, want %q (the item's Err)", i, p.Detail, outcomes[i].Err)
		}
	}
}

func TestOutcomeOf(t *testing.T) {
	tests := []struct {
		pr   ProblemResult
		want string
	}{
		{ProblemResult{Err: "x"}, "error"},
		{ProblemResult{Err: "x", Resolved: true}, "error"},
		{ProblemResult{Resolved: true}, "pass"},
		{ProblemResult{Resolved: false}, "fail"},
	}
	for _, tt := range tests {
		if got := outcomeOf(tt.pr); got != tt.want {
			t.Errorf("outcomeOf(%+v) = %q, want %q", tt.pr, got, tt.want)
		}
	}
}

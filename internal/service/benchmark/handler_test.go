package benchmark

import "testing"

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
	r := &Runner{presets: []tpPreset{{512, 128}, {4096, 256}}, problems: make([]Problem, 7)}
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
	r := &Runner{presets: []tpPreset{{512, 128}}, problems: make([]Problem, 3)}
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

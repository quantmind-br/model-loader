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

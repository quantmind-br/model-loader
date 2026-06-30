package benchmark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCapCount(t *testing.T) {
	cases := []struct {
		name  string
		limit int
		n     int
		want  int
	}{
		{"no limit", 0, 10, 10},
		{"negative limit ignored", -1, 10, 10},
		{"limit below n caps", 3, 10, 3},
		{"limit equal to n", 10, 10, 10},
		{"limit above n is no-op", 50, 10, 10},
		{"limit one", 1, 10, 1},
		{"empty set", 5, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Runner{cfg: Config{Limit: tc.limit}}
			if got := r.capCount(tc.n); got != tc.want {
				t.Errorf("capCount(%d) with Limit=%d = %d, want %d", tc.n, tc.limit, got, tc.want)
			}
		})
	}
}

// fullRunner builds a Runner whose every reducible dataset/preset slice has
// `n` items, so a Count() assertion is unambiguous.
func fullRunner(limit, n int) *Runner {
	return &Runner{
		cfg:             Config{Limit: limit},
		problems:        make([]Problem, n),
		mathProblems:    make([]MathProblem, n),
		codeGenProblems: make([]CodeGenProblem, n),
		instProblems:    make([]InstructionProblem, n),
		mmluProblems:    make([]MMLUProblem, n),
		ragasProblems:   make([]RagasProblem, n),
		summaryProblems: make([]SummaryProblem, n),
		presets:         make([]tpPreset, n),
	}
}

// reducibleModes is every mode whose Count must honor Config.Limit.
var reducibleModes = []Mode{
	ModeJudge, ModeMathBench, ModeCodeGenBench, ModeRagasBench,
	ModeSummaryBench, ModeInstBench, ModeMMLUBench, ModeLlamaBench,
}

func TestReducibleModes_CountHonorsLimit(t *testing.T) {
	const n = 10
	const limit = 3
	r := fullRunner(limit, n)
	for _, m := range reducibleModes {
		h, ok := handlerFor(m)
		if !ok {
			t.Fatalf("mode %q not registered", m)
		}
		if got := h.Count(r); got != limit {
			t.Errorf("mode %q: Count with Limit=%d over %d items = %d, want %d", m, limit, n, got, limit)
		}
	}
}

func TestReducibleModes_NoLimitUsesFullSet(t *testing.T) {
	const n = 10
	r := fullRunner(0, n)
	for _, m := range reducibleModes {
		h, _ := handlerFor(m)
		if got := h.Count(r); got != n {
			t.Errorf("mode %q: Count with no limit = %d, want %d", m, got, n)
		}
	}
}

func TestReducibleModes_LimitAboveSetIsNoOp(t *testing.T) {
	const n = 4
	r := fullRunner(100, n)
	for _, m := range reducibleModes {
		h, _ := handlerFor(m)
		if got := h.Count(r); got != n {
			t.Errorf("mode %q: Count with oversized limit = %d, want %d (full set)", m, got, n)
		}
	}
}

// longctx is a single probe and must ignore Limit entirely.
func TestLongContext_IgnoresLimit(t *testing.T) {
	r := &Runner{cfg: Config{Limit: 1}}
	h, _ := handlerFor(ModeLongContext)
	if got := h.Count(r); got != 1 {
		t.Errorf("longctx Count = %d, want 1 (single probe, limit-independent)", got)
	}
}

// TestExecute_LimitSlicesProblemLoop proves the cap reaches the Execute loop,
// not just Count: a 10-problem MMLU set with Limit=2 must yield exactly 2
// results. Uses an SSE stub so no real backend is needed.
func TestExecute_LimitSlicesProblemLoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"B\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	problems := make([]MMLUProblem, 10)
	for i := range problems {
		problems[i] = MMLUProblem{ID: "m", Question: "2+2=?", Choices: []string{"3", "4", "5", "6"}, Answer: "B", Category: "STEM"}
	}
	r := &Runner{cfg: Config{MaxTokens: 8, Timeout: 5 * time.Second, Limit: 2}, mmluProblems: problems}

	h, _ := handlerFor(ModeMMLUBench)
	results, _, err := h.Execute(context.Background(), r, srv.URL, "m", nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Execute ran %d problems, want 2 (Limit)", len(results))
	}
}

// TestExecute_LimitSlicesJudgeLoop covers the judge pipeline (its own loop,
// distinct from executeSerialBench): 5 problems with Limit=2 → 2 results.
func TestExecute_LimitSlicesJudgeLoop(t *testing.T) {
	srv := sseModelServer(nil)
	defer srv.Close()

	scorer := fakeScorer{fn: func(_ context.Context, _ Problem, _ string) (ProblemScore, error) {
		return ProblemScore{Resolved: true, Score: 1}, nil
	}}
	r := &Runner{
		cfg:      Config{MaxTokens: 32, Timeout: 5 * time.Second, Limit: 2, Judge: JudgeEndpoint{Samples: 1}},
		problems: []Problem{{ID: "p1"}, {ID: "p2"}, {ID: "p3"}, {ID: "p4"}, {ID: "p5"}},
	}
	results, _, err := judgeHandler{}.Execute(context.Background(), r, srv.URL, "m", scorer, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("judge Execute ran %d problems, want 2 (Limit)", len(results))
	}
}

// TestExecute_LimitSlicesLlamaBenchLoop covers the llama-bench preset loop:
// 4 presets with Limit=1 → 1 result. A failing-fast stub keeps it quick.
func TestExecute_LimitSlicesLlamaBenchLoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	r := &Runner{
		cfg:     Config{MaxTokens: 8, Timeout: 5 * time.Second, Limit: 1},
		presets: []tpPreset{{FillPct: 25, GenTokens: 2}, {FillPct: 50, GenTokens: 2}, {FillPct: 75, GenTokens: 2}, {FillPct: 90, GenTokens: 2}},
		reps:    1, warmup: 0,
	}
	results, _, err := llamaBenchHandler{}.Execute(context.Background(), r, srv.URL, "m", nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("llama-bench Execute ran %d presets, want 1 (Limit)", len(results))
	}
}

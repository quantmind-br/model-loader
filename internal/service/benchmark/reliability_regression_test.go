package benchmark

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

// T3: the per-item inference deadline scales with MaxTokens so a large budget
// on a slow local model no longer guarantees a spurious timeout.
func TestInferTimeout_ScalesWithMaxTokens(t *testing.T) {
	r := &Runner{cfg: Config{Timeout: 120 * time.Second, MaxTokens: 32768}}
	want := 120*time.Second + time.Duration(32768/decodeFloorTPS)*time.Second
	if got := r.inferTimeout(); got != want {
		t.Fatalf("inferTimeout() = %v, want %v", got, want)
	}
	if r.inferTimeout() <= r.cfg.Timeout {
		t.Error("scaled deadline must exceed the base Timeout for a non-trivial max_tokens")
	}
	// Zero max_tokens degrades to the base timeout (no negative/zero surprise).
	r0 := &Runner{cfg: Config{Timeout: 90 * time.Second}}
	if got := r0.inferTimeout(); got != 90*time.Second {
		t.Errorf("zero max_tokens: inferTimeout() = %v, want base 90s", got)
	}
}

// T5: a scoring failure tags the item FailPhase="score"; an inference failure
// tags FailPhase="infer".
func TestFailPhase_AttributesStage(t *testing.T) {
	srv := stubAnswerServer(t, "an answer")
	r := &Runner{cfg: Config{MaxTokens: 32, Timeout: 5 * time.Second}}

	// Score phase: model answers, grader fails.
	res, _ := r.runRagas(context.Background(), srv.URL, "m", &errGrader{err: errors.New("judge down")},
		RagasProblem{ID: "r", Documents: []string{"D."}, Question: "Q?", GroundTruth: "D.", ExpectedContext: "D."})
	if res.FailPhase != phaseScore {
		t.Errorf("grading failure FailPhase = %q, want %q", res.FailPhase, phaseScore)
	}

	// Infer phase: the model server is unreachable.
	_, res2, _, ok := r.inferProblem(context.Background(), "http://127.0.0.1:1", "m", Problem{ID: "p"})
	if ok {
		t.Fatal("inference against a dead server should fail")
	}
	if res2.FailPhase != phaseInfer {
		t.Errorf("inference failure FailPhase = %q, want %q", res2.FailPhase, phaseInfer)
	}
}

// T1 (nil-safety): a Runner built as a struct literal (no logger) must never
// nil-panic when a per-item failure is logged.
func TestLogger_NilSafeOnLiteralRunner(t *testing.T) {
	r := &Runner{cfg: Config{}} // no Logger set
	// executeSerialBench logs the failed item via r.logger(); must not panic.
	results, _, err := executeSerialBench(context.Background(), r, nil, 1,
		func(int) (string, string) { return "x", "x" },
		func(int) (ProblemResult, ProblemTranscript) {
			return ProblemResult{ProblemID: "x", Err: "boom", FailPhase: phaseInfer}, ProblemTranscript{}
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Err == "" {
		t.Fatalf("expected one failed result recorded, got %+v", results)
	}
}

// T7: executeSerialBench invokes the checkpoint hook after each item with the
// accumulating result set.
func TestExecuteSerialBench_InvokesCheckpoint(t *testing.T) {
	r := &Runner{cfg: Config{}}
	var lens []int
	r.checkpoint = func(res []ProblemResult) { lens = append(lens, len(res)) }
	_, _, err := executeSerialBench(context.Background(), r, nil, 3,
		func(i int) (string, string) { return fmt.Sprint(i), "" },
		func(i int) (ProblemResult, ProblemTranscript) {
			return ProblemResult{ProblemID: fmt.Sprint(i)}, ProblemTranscript{}
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []int{1, 2, 3}
	if fmt.Sprint(lens) != fmt.Sprint(want) {
		t.Errorf("checkpoint result lengths = %v, want %v", lens, want)
	}
}

// fakeGetStore is a profilestore.Store that only answers Get (the sole method
// Run touches); every other method panics if unexpectedly called.
type fakeGetStore struct {
	profilestore.Store
	p domain.Profile
}

func (f fakeGetStore) Get(string) (domain.Profile, error) { return f.p, nil }

// T7 (end-to-end): a run persists at least one checkpoint flagged
// Err="in progress" (so the leaderboard skips it), and the final returned run
// is a clean success.
func TestRun_CheckpointsFlaggedInProgress(t *testing.T) {
	srv := stubAnswerServer(t, "The answer is 2")
	r := &Runner{
		store: fakeGetStore{p: domain.Profile{ID: "p", Name: "P"}},
		mon:   &fakeMonitor{ch: make(chan monitor.MonitorEvent, 4)},
		proxy: &fakeProxyCtl{base: srv.URL},
		cfg:   Config{MaxTokens: 16, Timeout: 5 * time.Second},
		mathProblems: []MathProblem{
			{ID: "m1", Question: "1+1?", Answer: "2"},
			{ID: "m2", Question: "2+2?", Answer: "4"},
		},
	}

	var checkpoints []Run
	run, err := r.Run(context.Background(), RunConfig{
		ProfileID:  "p",
		Mode:       ModeMathBench,
		Checkpoint: func(partial Run) { checkpoints = append(checkpoints, partial) },
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run.Err != "" {
		t.Errorf("final run should be clean, got Err=%q", run.Err)
	}
	if run.FinishedAt.IsZero() {
		t.Error("final run should have FinishedAt set")
	}
	if len(checkpoints) == 0 {
		t.Fatal("expected at least one checkpoint during the run")
	}
	c := checkpoints[0]
	if c.Err != "in progress" {
		t.Errorf("checkpoint Err = %q, want \"in progress\" (excluded from leaderboard)", c.Err)
	}
	if len(c.Problems) == 0 {
		t.Error("checkpoint should carry the items completed so far")
	}
	if c.ID != run.ID {
		t.Errorf("checkpoint ID %q must match final run ID %q (final save overwrites)", c.ID, run.ID)
	}
}

// BR9: Run must JOIN its feed drainer before returning, so the caller's
// close(progress) — the exact TUI (benchmark_run.go) and CLI (benchmark.go)
// pattern — can never race a drainer forward into a now-closed channel. A send
// case on a closed channel is selected over default and panics, which would
// crash the whole process on the normal success path. Verified: reverting the
// drainer join makes this panic with "send on closed channel" on the first run;
// with the join it is clean.
func TestRun_ClosingProgressAfterReturnNeverPanics(t *testing.T) {
	srv := stubAnswerServer(t, "The answer is 2")
	for i := 0; i < 30; i++ {
		r := &Runner{
			store: fakeGetStore{p: domain.Profile{ID: "p", Name: "P"}},
			mon:   &fakeMonitor{ch: make(chan monitor.MonitorEvent, 4)},
			proxy: &fakeProxyCtl{base: srv.URL},
			cfg:   Config{MaxTokens: 16, Timeout: 5 * time.Second},
			mathProblems: []MathProblem{
				{ID: "m1", Question: "1+1?", Answer: "2"},
				{ID: "m2", Question: "2+2?", Answer: "4"},
			},
		}
		prog := make(chan Progress, 4)
		drained := make(chan struct{})
		go func() {
			for range prog { // consume lossy forwards until the caller closes
			}
			close(drained)
		}()
		if _, err := r.Run(context.Background(), RunConfig{ProfileID: "p", Mode: ModeMathBench}, prog); err != nil {
			t.Fatalf("Run: %v", err)
		}
		close(prog) // caller closes right after Run returns — must be panic-safe
		<-drained
	}
}

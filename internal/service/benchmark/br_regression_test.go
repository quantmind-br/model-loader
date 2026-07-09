package benchmark

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// errGrader is a grader double that always fails, counting calls so tests can
// assert the ragas short-circuit (BR1).
type errGrader struct {
	calls atomic.Int32
	err   error
}

func (g *errGrader) Grade(context.Context, gradeRequest) (gradeResult, error) {
	g.calls.Add(1)
	return gradeResult{}, g.err
}

// stubAnswerServer serves one SSE completion carrying content for the
// model-under-test answer call.
func stubAnswerServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"" + content + "\"}}]}\n\n"))
		if fl != nil {
			fl.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// BR1: a ragas grading failure surfaces as a per-problem error, not a silent
// zero blended into the aggregate; the first failure short-circuits the rest.
func TestRunRagas_GradingFailureSetsErr(t *testing.T) {
	srv := stubAnswerServer(t, "some answer")
	r := &Runner{cfg: Config{MaxTokens: 64, Timeout: 5 * time.Second}}
	g := &errGrader{err: errors.New("judge 503")}

	p := RagasProblem{ID: "r1", Documents: []string{"D."}, Question: "Q?", GroundTruth: "D.", ExpectedContext: "D."}
	res, tr := r.runRagas(context.Background(), srv.URL, "m", g, p)

	if res.Err == "" {
		t.Fatal("grading failure must set res.Err, not a silent zero")
	}
	if !strings.Contains(res.Err, "grade faithfulness") {
		t.Errorf("res.Err = %q, want it to name the failing phase/criterion", res.Err)
	}
	if res.Score != 0 || res.SubScores != nil {
		t.Errorf("failed item must not carry a fabricated score: score=%v sub=%v", res.Score, res.SubScores)
	}
	if got := g.calls.Load(); got != 1 {
		t.Errorf("grader calls = %d, want 1 (first failure short-circuits remaining criteria)", got)
	}
	if tr.Error == "" {
		t.Error("transcript should record the error")
	}
}

// BR1: a summary coherence grading failure surfaces as a per-problem error
// rather than a silent zero coherence.
func TestRunSummary_GradingFailureSetsErr(t *testing.T) {
	srv := stubAnswerServer(t, "a summary")
	r := &Runner{cfg: Config{MaxTokens: 64, Timeout: 5 * time.Second}}
	g := &errGrader{err: errors.New("judge timeout")}

	p := SummaryProblem{ID: "s1", Documents: []string{"Doc."}, Facts: []string{"a fact here"}}
	res, tr := r.runSummary(context.Background(), srv.URL, "m", g, p)

	if res.Err == "" {
		t.Fatal("grading failure must set res.Err, not a silent zero coherence")
	}
	if !strings.Contains(res.Err, "grade coherence") {
		t.Errorf("res.Err = %q, want it to name the coherence phase", res.Err)
	}
	if res.Score != 0 || res.SubScores != nil {
		t.Errorf("failed item must not carry a fabricated score: score=%v sub=%v", res.Score, res.SubScores)
	}
	if tr.Error == "" {
		t.Error("transcript should record the error")
	}
}

// BR2: a result set where every item failed reports a run-level error so it is
// never presented as a measured (leaderboard / --min-solve) run.
func TestAllItemsFailed(t *testing.T) {
	if err := allItemsFailed(nil); err != nil {
		t.Errorf("empty set = %v, want nil", err)
	}
	mixed := []ProblemResult{{Err: "x"}, {Score: 1}}
	if err := allItemsFailed(mixed); err != nil {
		t.Errorf("partial failure = %v, want nil (partial run kept)", err)
	}
	allErr := []ProblemResult{{Err: "boom"}, {Err: "bang"}}
	err := allItemsFailed(allErr)
	if err == nil {
		t.Fatal("all-failed set must return a run-level error")
	}
	if !strings.Contains(err.Error(), "all 2 items failed") {
		t.Errorf("err = %q, want it to count the failures", err)
	}
}

// BR3: a tb run whose result set is COMPLETE but whose harness exited non-zero
// is a success (exit-code semantics of a complete set are the harness's
// business); an INCOMPLETE set with a non-zero exit is a partial run.
func TestTerminalBench_Execute_NonZeroExitGatedByCompleteness(t *testing.T) {
	origTB := lookTB
	defer func() { lookTB = origTB }()

	// Complete: 3 trials in the fixture, 3 tasks requested, tb exits 1.
	lookTB = func() (string, error) { return writeFakeTB(t, tbResultsFixture, 1), nil }
	rComplete := &Runner{cfg: Config{TerminalBenchTasks: []string{"hello-world", "broken-pipe", "crashy"}}}
	if _, _, err := (terminalBenchHandler{}).Execute(context.Background(), rComplete, "http://127.0.0.1:4321", "p", nil, nil); err != nil {
		t.Fatalf("complete result set + non-zero exit must succeed, got %v", err)
	}

	// Incomplete: 3 trials but 5 tasks requested, tb exits 1 → partial error.
	lookTB = func() (string, error) { return writeFakeTB(t, tbResultsFixture, 1), nil }
	rPartial := &Runner{cfg: Config{TerminalBenchTasks: []string{"a", "b", "c", "d", "e"}}}
	probs, _, err := (terminalBenchHandler{}).Execute(context.Background(), rPartial, "http://127.0.0.1:4321", "p", nil, nil)
	if err == nil {
		t.Fatal("incomplete result set + non-zero exit must surface a partial error")
	}
	if len(probs) == 0 {
		t.Error("partial run should still carry the trials that did complete")
	}
}

// BR6: a rep failure degrades to the remaining measured reps instead of
// aborting the whole preset on the first error.
func TestRunLlamaBench_DegradesOnRepFailure(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Fail the first two HTTP calls (rep 1 + its single retry), succeed after.
		if calls.Add(1) <= 2 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"x y\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":2}," +
			"\"timings\":{\"prompt_per_second\":1000,\"predicted_per_second\":50}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	r := &Runner{cfg: Config{MaxTokens: 64, Timeout: 5 * time.Second}, reps: 3, warmup: 0, runCtxTokens: 16}
	res, _ := r.runLlamaBench(context.Background(), srv.URL, "m", tpPreset{FillPct: 50, GenTokens: 2})
	if res.Err != "" {
		t.Fatalf("a single rep failure must not void reps already measured: %s", res.Err)
	}
	if !res.Resolved {
		t.Fatalf("expected a resolved measurement from the surviving reps, detail %q", res.Detail)
	}
	if !strings.Contains(res.Detail, "reps failed") {
		t.Errorf("Detail should trace the failed rep, got %q", res.Detail)
	}
}

// BR6: when every rep fails outright, the preset is an explicit error, not a
// silent zero measurement.
func TestRunLlamaBench_AllRepsFailIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "always down", http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := &Runner{cfg: Config{MaxTokens: 64, Timeout: 5 * time.Second}, reps: 2, warmup: 0, runCtxTokens: 16}
	res, _ := r.runLlamaBench(context.Background(), srv.URL, "m", tpPreset{FillPct: 50, GenTokens: 2})
	if res.Err == "" {
		t.Fatal("all reps failing must set res.Err, not a zero measurement")
	}
	if !strings.Contains(res.Err, "reps failed") {
		t.Errorf("res.Err = %q, want it to count the failed reps", res.Err)
	}
}

// BR7: the harness sink bounds its in-memory tail and, when a dir is set, tees
// the full output to a persistent file whose path the transcript names.
func TestHarnessLog_BoundedTailAndFileTee(t *testing.T) {
	// No dir → bounded tail only, no file reference.
	mem := newHarnessLog("", "x")
	defer mem.Close()
	big := strings.Repeat("A", harnessTailMax+5000)
	_, _ = mem.Write([]byte(big))
	if len(mem.Tail()) != harnessTailMax {
		t.Errorf("tail len = %d, want bounded at %d", len(mem.Tail()), harnessTailMax)
	}
	if strings.Contains(mem.transcript(), "full harness log") {
		t.Error("in-memory-only sink must not reference a log file")
	}

	// With dir → full output persisted, path referenced.
	dir := t.TempDir()
	h := newHarnessLog(dir, "terminal-bench")
	if h.path == "" {
		t.Fatal("configured dir must open a log file")
	}
	_, _ = h.Write([]byte(big))
	_, _ = h.Write([]byte("TAILMARK"))
	h.Close()
	data, err := os.ReadFile(h.path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if len(data) != len(big)+len("TAILMARK") {
		t.Errorf("file len = %d, want full %d (unbounded on disk)", len(data), len(big)+len("TAILMARK"))
	}
	if !strings.HasSuffix(string(data), "TAILMARK") {
		t.Error("file should carry the full stream to the end")
	}
	if !strings.Contains(h.transcript(), h.path) {
		t.Error("transcript should reference the persistent log path")
	}
	if !strings.Contains(h.diagTail(), h.path) {
		t.Error("diagnostic tail should reference the persistent log path")
	}
}

// BR7 follow-through: PruneHarnessLogs keeps N per mode label (so frequent
// terminal-bench runs can't evict deep-swe logs), splits the label on the
// timestamp suffix (labels contain hyphens), keeps the newest, and leaves
// non-harness files untouched.
func TestPruneHarnessLogs_KeepsNewestPerMode(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string, sec int64) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		ts := time.Unix(sec, 0)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	// 4 terminal-bench (hyphenated label), 1 deep-swe, 1 stray non-harness file.
	mk("terminal-bench-20260101-000000.log", 100)
	mk("terminal-bench-20260102-000000.log", 200)
	mk("terminal-bench-20260103-000000.log", 300)
	mk("terminal-bench-20260104-000000.log", 400)
	mk("deep-swe-20260101-000000.log", 150)
	mk("notes.txt", 500)

	removed := PruneHarnessLogs(dir, 2, nil)
	if removed != 2 {
		t.Fatalf("removed = %d, want 2 (4 tb → keep 2; deep-swe under cap)", removed)
	}
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	// The 2 newest terminal-bench survive; the 2 oldest are gone.
	if !exists("terminal-bench-20260104-000000.log") || !exists("terminal-bench-20260103-000000.log") {
		t.Error("the two newest terminal-bench logs must survive")
	}
	if exists("terminal-bench-20260101-000000.log") || exists("terminal-bench-20260102-000000.log") {
		t.Error("the two oldest terminal-bench logs must be pruned")
	}
	// A different mode under the cap is untouched; non-harness file untouched.
	if !exists("deep-swe-20260101-000000.log") {
		t.Error("deep-swe log (under cap) must not be evicted by terminal-bench churn")
	}
	if !exists("notes.txt") {
		t.Error("non-harness file must be left untouched")
	}
	// Missing dir → no-op.
	if n := PruneHarnessLogs(filepath.Join(dir, "nope"), 2, nil); n != 0 {
		t.Errorf("missing dir returned %d, want 0", n)
	}
}

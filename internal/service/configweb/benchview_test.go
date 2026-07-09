package configweb

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/service/benchmarkstore"
)

// --- stub benchmark store ---------------------------------------------------

type stubBenchStore struct {
	list []benchmark.Run
	byID map[string]benchmark.Run
	tr   map[string][]benchmark.ProblemTranscript
}

func newStubBenchStore(runs ...benchmark.Run) *stubBenchStore {
	s := &stubBenchStore{byID: map[string]benchmark.Run{}, tr: map[string][]benchmark.ProblemTranscript{}}
	for _, r := range runs {
		s.list = append(s.list, r)
		s.byID[r.ID] = r
	}
	return s
}

func (s *stubBenchStore) Save(benchmark.Run) error { return nil }
func (s *stubBenchStore) List() ([]benchmark.Run, error) {
	return append([]benchmark.Run(nil), s.list...), nil
}
func (s *stubBenchStore) ListByProfile(id string) ([]benchmark.Run, error) {
	var out []benchmark.Run
	for _, r := range s.list {
		if r.ProfileID == id {
			out = append(out, r)
		}
	}
	return out, nil
}
func (s *stubBenchStore) Delete(string) error { return nil }
func (s *stubBenchStore) Load(id string) (benchmark.Run, error) {
	if r, ok := s.byID[id]; ok {
		return r, nil
	}
	return benchmark.Run{}, benchmarkstore.ErrNotFound
}
func (s *stubBenchStore) LoadTranscript(id string) ([]benchmark.ProblemTranscript, error) {
	if t, ok := s.tr[id]; ok {
		return t, nil
	}
	return nil, benchmarkstore.ErrNotFound
}
func (s *stubBenchStore) TranscriptPath(id string) string { return "/tmp/" + id + ".transcript.json" }

// serve routes the request through the viewer's mux so path patterns (and
// r.PathValue) resolve exactly as in production.
func serve(v *BenchViewer, method, target string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	v.routes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

// --- tests ------------------------------------------------------------------

func TestBenchViewer_ListRendersRuns(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	now := time.Now()
	runs := []benchmark.Run{
		{ID: "alpha-1", ProfileID: "alpha", ProfileName: "Alpha", Mode: benchmark.ModeMMLUBench,
			StartedAt: now, Aggregate: benchmark.Aggregate{SolveRate: 0.5, AvgTokensPerSecond: 42}},
		{ID: "beta-1", ProfileID: "beta", ProfileName: "Beta", Mode: benchmark.ModeLlamaBench,
			StartedAt: now.Add(-time.Hour), Err: "cancelled",
			Aggregate: benchmark.Aggregate{AvgTokensPerSecond: 100}},
	}
	v := NewBenchViewer(BenchViewerDeps{Runs: newStubBenchStore(runs...), Profiles: newMemProfileStore()})
	rec := serve(v, "GET", "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Alpha", "Beta", "Factual knowledge (MMLU)", "Throughput (llama-bench)", "/run/alpha-1", "50%", "42.0"} {
		if !strings.Contains(body, want) {
			t.Errorf("list body missing %q\n%s", want, body)
		}
	}
	// The partial run (Err set) carries the ! badge.
	if !strings.Contains(body, "bench-badge warn") {
		t.Errorf("partial run should render the ! badge:\n%s", body)
	}
}

func TestBenchViewer_ListEmpty(t *testing.T) {
	v := NewBenchViewer(BenchViewerDeps{Runs: newStubBenchStore()})
	rec := serve(v, "GET", "/")
	if !strings.Contains(rec.Body.String(), "no benchmark runs yet") {
		t.Fatalf("empty list should keep the empty-state string:\n%s", rec.Body.String())
	}
}

func TestBenchViewer_RunUnknownReturns404(t *testing.T) {
	v := NewBenchViewer(BenchViewerDeps{Runs: newStubBenchStore()})
	rec := serve(v, "GET", "/run/does-not-exist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown run status = %d, want 404", rec.Code)
	}
}

func TestBenchViewer_UnknownPathReturns404(t *testing.T) {
	v := NewBenchViewer(BenchViewerDeps{Runs: newStubBenchStore()})
	rec := serve(v, "GET", "/nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown path status = %d, want 404", rec.Code)
	}
}

// UIUX-019: DeepSWE accuracy line rendered (modeDetailLines had no DeepSWE case).
func TestBenchViewer_RunDetailRendersModeLinesAndTranscript(t *testing.T) {
	run := benchmark.Run{
		ID: "deep-1", ProfileID: "deep", ProfileName: "Deep", Mode: benchmark.ModeDeepSWE,
		StartedAt: time.Now(),
		Profile:   benchmark.ProfileSnapshot{Model: "/models/foo.Q4_K_M.gguf", Quantization: "Q4_K_M"},
		Problems: []benchmark.ProblemResult{
			{ProblemID: "t1", ProblemName: "task-one", Resolved: true, Score: 1, TokensPerSecond: 30, TTFTms: 120},
			{ProblemID: "t2", ProblemName: "task-two", Err: "boom"},
		},
		Aggregate: benchmark.Aggregate{Total: 2, Resolved: 1, SolveRate: 0.5, DeepSWEAccuracy: 0.5,
			AvgTokensPerSecond: 30, AvgTTFTms: 120, PeakVRAMMB: 12000},
	}
	store := newStubBenchStore(run)
	store.tr["deep-1"] = []benchmark.ProblemTranscript{{ProblemID: "t1", ProblemName: "task-one", ModelResponse: "hello world"}}
	v := NewBenchViewer(BenchViewerDeps{Runs: store})
	rec := serve(v, "GET", "/run/deep-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"deep-swe accuracy 50% (1/2 tasks resolved)", // DeepSWE mode line (mirrors TUI + new case)
		"task-one", "task-two",
		"✓ pass", "! error", // outcome badges
		"Transcript excerpt", "hello world", // transcript details
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail body missing %q", want)
		}
	}
}

func TestBenchViewer_CompareRenders(t *testing.T) {
	now := time.Now()
	runs := []benchmark.Run{
		{ID: "a-2", ProfileID: "a", ProfileName: "AAA", Mode: benchmark.ModeMMLUBench, StartedAt: now,
			Aggregate: benchmark.Aggregate{SolveRate: 0.8}},
		{ID: "b-1", ProfileID: "b", ProfileName: "BBB", Mode: benchmark.ModeMMLUBench, StartedAt: now.Add(-time.Minute),
			Aggregate: benchmark.Aggregate{SolveRate: 0.4}},
		{ID: "c-err", ProfileID: "c", ProfileName: "CCC", Mode: benchmark.ModeMMLUBench, StartedAt: now, Err: "partial"},
	}
	v := NewBenchViewer(BenchViewerDeps{Runs: newStubBenchStore(runs...)})
	rec := serve(v, "GET", "/compare")
	body := rec.Body.String()
	if !strings.Contains(body, "AAA") || !strings.Contains(body, "BBB") {
		t.Fatalf("compare missing profiles:\n%s", body)
	}
	if strings.Contains(body, "CCC") {
		t.Errorf("partial run must be excluded from compare")
	}
	// Higher solve rate (AAA) ranks first with the ▲ best marker.
	if idxA, idxMark := strings.Index(body, "AAA"), strings.Index(body, "▲"); idxMark < 0 || idxMark > idxA {
		t.Errorf("best-row ▲ marker should precede the top profile")
	}
}

func TestBenchViewer_CompareEmpty(t *testing.T) {
	v := NewBenchViewer(BenchViewerDeps{Runs: newStubBenchStore()})
	rec := serve(v, "GET", "/compare")
	if !strings.Contains(rec.Body.String(), "no runs to compare") {
		t.Fatalf("empty compare should keep the empty-state string")
	}
}

func TestBenchViewer_LiveFragmentShowsTalliesAndStaleness(t *testing.T) {
	base := time.Now()
	snap := benchmark.FeedSnapshot{
		RunID: "r1", ProfileID: "p", ProfileName: "P", Mode: benchmark.ModeMMLUBench,
		Total: 3, StartedAt: base,
	}
	events := []benchmark.Progress{
		{Phase: "launch", At: base},
		{Phase: "infer", Index: 1, Total: 3, ProblemID: "q1", ProblemName: "Q1", At: base.Add(time.Second)},
		{Phase: "item_done", Index: 1, Total: 3, ProblemID: "q1", ProblemName: "Q1", Outcome: "pass", Score: 1, ItemMs: 1200, At: base.Add(2 * time.Second)},
		{Phase: "infer", Index: 2, Total: 3, ProblemID: "q2", ProblemName: "Q2", At: base.Add(3 * time.Second)},
	}
	feed := benchmark.NewFeedForTest(snap, events...)
	v := NewBenchViewer(BenchViewerDeps{Runs: newStubBenchStore(), Live: func() *benchmark.RunFeed { return feed }})
	rec := serve(v, "GET", "/live/fragment")
	body := rec.Body.String()
	for _, want := range []string{"✓1", "✗0", "!0", "Q2", "now:", "bench-stale", "activity"} {
		if !strings.Contains(body, want) {
			t.Errorf("live fragment missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "no run in progress") {
		t.Errorf("active run must not render the no-run state")
	}
}

func TestBenchViewer_LiveFragmentNilLive(t *testing.T) {
	// Live accessor absent.
	v := NewBenchViewer(BenchViewerDeps{Runs: newStubBenchStore()})
	if !strings.Contains(serve(v, "GET", "/live/fragment").Body.String(), "no run in progress") {
		t.Fatalf("nil Live must render 'no run in progress'")
	}
	// Live accessor present but returns nil (runner never ran).
	v2 := NewBenchViewer(BenchViewerDeps{Runs: newStubBenchStore(), Live: func() *benchmark.RunFeed { return nil }})
	if !strings.Contains(serve(v2, "GET", "/live/fragment").Body.String(), "no run in progress") {
		t.Fatalf("nil feed must render 'no run in progress'")
	}
}

// buildLiveVM must produce staleness wording byte-identical to
// benchmark.StalenessLabel so all three surfaces agree.
func TestBenchViewer_StalenessWordingMatchesShared(t *testing.T) {
	base := time.Now().Add(-5 * time.Minute) // > local stalled threshold (2m15s)
	snap := benchmark.FeedSnapshot{Mode: benchmark.ModeMMLUBench, StartedAt: base, LastActivity: base, LastItemDone: base}
	now := time.Now()
	vm := buildLiveVM(snap, now)
	level, since := snap.Staleness(now)
	if want := benchmark.StalenessLabel(level, since); vm.StaleText != want {
		t.Fatalf("staleness text = %q, want %q", vm.StaleText, want)
	}
	if level != benchmark.StaleStalled {
		t.Fatalf("5m silence on a local mode should be StaleStalled, got %v", level)
	}
	if vm.StaleClass != "err" {
		t.Fatalf("stalled staleness class = %q, want err", vm.StaleClass)
	}
}

// buildLiveVM surfaces the watchdog countdown only for agentic modes
// (StallTimeout > 0) while the kill clock has time left.
func TestBenchViewer_WatchdogCountdown(t *testing.T) {
	now := time.Now()
	snap := benchmark.FeedSnapshot{
		Mode: benchmark.ModeTerminalBench, StartedAt: now.Add(-time.Minute),
		LastActivity: now, LastItemDone: now.Add(-time.Minute), StallTimeout: 5 * time.Minute,
	}
	vm := buildLiveVM(snap, now)
	if !strings.Contains(vm.Watchdog, "watchdog kill in") {
		t.Fatalf("watchdog countdown missing: %q", vm.Watchdog)
	}
	// No StallTimeout → no countdown.
	snap.StallTimeout = 0
	if got := buildLiveVM(snap, now).Watchdog; got != "" {
		t.Fatalf("no StallTimeout must yield no countdown, got %q", got)
	}
}

func TestBenchViewer_LifecycleStartCancelDone(t *testing.T) {
	v := NewBenchViewer(BenchViewerDeps{Runs: newStubBenchStore()})
	url, err := v.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	resp, err := http.Get(url + "/healthz")
	if err != nil {
		t.Fatalf("healthz get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status %d", resp.StatusCode)
	}

	v.Cancel()
	// Serving /closed ends the linger so the server tears down promptly.
	if cr, err := http.Get(url + "/closed"); err == nil {
		cr.Body.Close()
	}

	select {
	case <-v.Done():
	case <-time.After(4 * time.Second):
		t.Fatalf("Done() not closed after Cancel + /closed")
	}

	// Server is down: further requests fail.
	down := false
	for range 100 {
		if _, err := http.Get(url + "/healthz"); err != nil {
			down = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !down {
		t.Fatalf("server should be down after Done()")
	}
}

func TestBenchViewer_CancelWithoutServerClosesDone(t *testing.T) {
	// Cancel before Start (no server bound) must still close Done exactly once.
	v := NewBenchViewer(BenchViewerDeps{Runs: newStubBenchStore()})
	v.Cancel()
	select {
	case <-v.Done():
	case <-time.After(time.Second):
		t.Fatalf("Done() not closed on Cancel without server")
	}
	v.Cancel() // idempotent, must not panic
}

// UIUX-025: a done-phase snapshot is terminal. buildLiveVM must render a
// completion line and never escalate staleness to 'possibly hung' — even when
// LastActivity is far in the past — nor keep showing the live now-line.
func TestBenchViewer_LiveDonePhaseIsTerminal(t *testing.T) {
	base := time.Now().Add(-30 * time.Minute) // past every stalled threshold
	snap := benchmark.FeedSnapshot{
		Mode: benchmark.ModeMMLUBench, Phase: "done",
		StartedAt: base, LastActivity: base, LastItemDone: base,
		Current: &benchmark.ItemState{ID: "q9", Name: "Q9", Phase: "infer", StartedAt: base},
	}
	vm := buildLiveVM(snap, time.Now())
	if !vm.Completed {
		t.Fatalf("done phase must set Completed")
	}
	if vm.HasNow {
		t.Fatalf("completed run must not render a live now-line")
	}
	if vm.Watchdog != "" {
		t.Fatalf("completed run must not render a watchdog countdown, got %q", vm.Watchdog)
	}
	if strings.Contains(vm.StaleText, "possibly hung") || strings.Contains(vm.StaleText, "quiet") {
		t.Fatalf("completed run must not escalate staleness, got %q", vm.StaleText)
	}
	if vm.StaleText != "run complete" {
		t.Fatalf("StaleText = %q, want 'run complete'", vm.StaleText)
	}

	// End-to-end through the fragment handler: a done feed emitted long ago must
	// render 'run complete', not 'possibly hung'.
	feed := benchmark.NewFeedForTest(
		benchmark.FeedSnapshot{Mode: benchmark.ModeMMLUBench, StartedAt: base, Total: 2},
		benchmark.Progress{Phase: "done", At: base},
	)
	v := NewBenchViewer(BenchViewerDeps{Runs: newStubBenchStore(), Live: func() *benchmark.RunFeed { return feed }})
	body := serve(v, "GET", "/live/fragment").Body.String()
	if strings.Contains(body, "possibly hung") {
		t.Fatalf("done-phase fragment must not show 'possibly hung':\n%s", body)
	}
	if !strings.Contains(body, "run complete") {
		t.Fatalf("done-phase fragment must show completion:\n%s", body)
	}
}

// UIUX-027: with ImmediateShutdown, Cancel tears the server down without the
// /closed linger, so Done() closes well before the 3s grace period.
func TestBenchViewer_ImmediateShutdown(t *testing.T) {
	v := NewBenchViewer(BenchViewerDeps{Runs: newStubBenchStore(), ImmediateShutdown: true})
	if _, err := v.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	start := time.Now()
	v.Cancel() // no /closed fetch — must not wait the linger
	select {
	case <-v.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("Done() not closed promptly under ImmediateShutdown")
	}
	if elapsed := time.Since(start); elapsed >= 3*time.Second {
		t.Fatalf("teardown took %v — the 3s linger was not skipped", elapsed)
	}
}

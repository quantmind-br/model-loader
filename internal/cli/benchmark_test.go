package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/service/benchmarkstore"
)

// TestBenchmarkRun_RequiresProfile exercises the tree end-to-end: `benchmark
// run` with no --profile must fail on the required-flag check (before any
// config/bootstrap), surfacing exit code 1.
func TestBenchmarkRun_RequiresProfile(t *testing.T) {
	var errb bytes.Buffer
	rootCmd.SetArgs([]string{"benchmark", "run"})
	rootCmd.SetOut(&errb)
	rootCmd.SetErr(&errb)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	if code := Execute(); code != 1 {
		t.Fatalf("benchmark run without --profile should exit 1, got %d (out=%q)", code, errb.String())
	}
	if !strings.Contains(errb.String(), "profile") {
		t.Fatalf("expected required-flag error mentioning profile, got %q", errb.String())
	}
}

// TestBenchmarkParent_PrintsHelp: a bare `benchmark` prints help and exits 0
// (no hidden dispatch).
func TestBenchmarkParent_PrintsHelp(t *testing.T) {
	var outb bytes.Buffer
	rootCmd.SetArgs([]string{"benchmark"})
	rootCmd.SetOut(&outb)
	rootCmd.SetErr(&outb)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	if code := Execute(); code != 0 {
		t.Fatalf("bare `benchmark` should exit 0, got %d", code)
	}
	out := outb.String()
	for _, want := range []string{"run", "list", "compare", "show"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help should list subcommand %q; got:\n%s", want, out)
		}
	}
}

// stubBenchStore is a hand-rolled in-memory benchmarkstore.Store for the CLI
// render tests (no filesystem, no config).
type stubBenchStore struct {
	runs        []benchmark.Run
	byID        map[string]benchmark.Run
	transcripts map[string][]benchmark.ProblemTranscript
	deleted     []string
}

func (s *stubBenchStore) Save(benchmark.Run) error { return nil }
func (s *stubBenchStore) List() ([]benchmark.Run, error) { return s.runs, nil }
func (s *stubBenchStore) ListByProfile(id string) ([]benchmark.Run, error) {
	var out []benchmark.Run
	for _, r := range s.runs {
		if r.ProfileID == id {
			out = append(out, r)
		}
	}
	return out, nil
}
func (s *stubBenchStore) Delete(id string) error {
	if s.byID != nil {
		if _, ok := s.byID[id]; !ok {
			return benchmarkstore.ErrNotFound
		}
		delete(s.byID, id)
	}
	s.deleted = append(s.deleted, id)
	return nil
}
func (s *stubBenchStore) Load(id string) (benchmark.Run, error) {
	if r, ok := s.byID[id]; ok {
		return r, nil
	}
	return benchmark.Run{}, benchmarkstore.ErrNotFound
}
func (s *stubBenchStore) LoadTranscript(id string) ([]benchmark.ProblemTranscript, error) {
	if tr, ok := s.transcripts[id]; ok {
		return tr, nil
	}
	return nil, benchmarkstore.ErrNotFound
}
func (s *stubBenchStore) TranscriptPath(id string) string { return "/tmp/" + id + ".transcript.json" }

// --- command tree registration ---------------------------------------------

func TestBenchmarkCommand_Registered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"benchmark"})
	if err != nil || cmd == nil || cmd.Name() != "benchmark" {
		t.Fatalf("benchmark command not registered: cmd=%v err=%v", cmd, err)
	}
	subs := map[string]bool{}
	for _, c := range cmd.Commands() {
		subs[c.Name()] = true
	}
	for _, name := range []string{"run", "list", "compare", "show", "transcript", "export", "delete", "history"} {
		if !subs[name] {
			t.Fatalf("benchmark missing subcommand %q (have %v)", name, subs)
		}
	}
	// Clean cutover: the legacy flag-dispatch verbs are gone from the parent.
	for _, f := range []string{"list", "compare", "transcript", "profile", "mode"} {
		if cmd.Flags().Lookup(f) != nil {
			t.Fatalf("benchmark parent must not expose legacy --%s flag", f)
		}
	}
}

func TestBenchmarkRun_Flags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"benchmark", "run"})
	if err != nil || cmd == nil || cmd.Name() != "run" {
		t.Fatalf("benchmark run not registered: cmd=%v err=%v", cmd, err)
	}
	for _, f := range []string{
		"profile", "mode", "limit", "tb-task", "tb-n-tasks", "sweap-instance",
		"sweap-harness", "sweap-patches", "deepswe-task", "deepswe-n-tasks",
		"deepswe-tasks", "min-solve", "verbose",
	} {
		if cmd.Flags().Lookup(f) == nil {
			t.Fatalf("benchmark run must expose --%s flag", f)
		}
	}
	// --json is inherited from the root persistent flag; run must not redefine it.
	if cmd.LocalFlags().Lookup("json") != nil {
		t.Fatalf("benchmark run must not define a local --json flag (use the root persistent one)")
	}
	// The mode help string is derived from the registry (no drift).
	if got := cmd.Flags().Lookup("mode").Usage; !strings.Contains(got, string(benchmark.ModeJudge)) {
		t.Fatalf("--mode usage should list registered modes, got %q", got)
	}
}

// UIUX-018: benchmark honors the root persistent --json (local --json flag removed).
func TestRootJSONFlag_Present(t *testing.T) {
	if rootCmd.PersistentFlags().Lookup("json") == nil {
		t.Fatalf("root must expose the persistent --json flag honored by benchmark subcommands")
	}
}

// --- registry / mode parsing (kept) ----------------------------------------

// BR8: the CLI mode list is derived from the registry, so a newly added mode
// can never drift out of the usage / unknown-mode messages.
func TestBenchModeList_CoversEveryRegisteredMode(t *testing.T) {
	list := benchModeList()
	for _, m := range benchmark.ModesInOrder() {
		if !strings.Contains(list, string(m)) {
			t.Errorf("benchModeList() = %q, missing registered mode %q", list, m)
		}
	}
}

func TestParseBenchMode(t *testing.T) {
	for _, m := range benchmark.ModesInOrder() {
		got, ok := parseBenchMode(string(m))
		if !ok || got != m {
			t.Fatalf("mode %q should parse to itself, got %q ok=%v", m, got, ok)
		}
	}
	for alias, want := range map[string]benchmark.Mode{
		"long-context": benchmark.ModeLongContext,
		"llamabench":   benchmark.ModeLlamaBench,
		"throughput":   benchmark.ModeLlamaBench,
	} {
		if got, ok := parseBenchMode(alias); !ok || got != want {
			t.Fatalf("alias %q should parse to %q, got %q ok=%v", alias, want, got, ok)
		}
	}
	if _, ok := parseBenchMode("bogus"); ok {
		t.Fatalf("bogus mode must not parse")
	}
}

// --- result renderers -------------------------------------------------------

func TestPrintRun_WritesToInjectedWriter(t *testing.T) {
	run := benchmark.Run{
		ProfileID:   "p1",
		ProfileName: "My Profile",
		Mode:        benchmark.ModeJudge,
		Aggregate:   benchmark.Aggregate{SolveRate: 0.5, Resolved: 1, Total: 2, AvgScore: 0.75},
		Problems: []benchmark.ProblemResult{
			{ProblemName: "issue-1", Resolved: true, Score: 1},
		},
	}
	var buf bytes.Buffer
	printRun(&buf, run)
	out := buf.String()
	for _, want := range []string{"My Profile", "p1", "Solve:", "issue-1", "Problems:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("printRun output missing %q; got:\n%s", want, out)
		}
	}
}

func TestPrintRun_WallLine(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	run := benchmark.Run{StartedAt: start, FinishedAt: start.Add(34*time.Minute + 12*time.Second)}
	var buf bytes.Buffer
	printRun(&buf, run)
	if !strings.Contains(buf.String(), "Wall:    34m12s") {
		t.Fatalf("expected Wall line, got:\n%s", buf.String())
	}
}

func TestBenchPrintList_IDColumn(t *testing.T) {
	stub := &stubBenchStore{runs: []benchmark.Run{
		{ID: "run-abc", ProfileName: "Prof A", Mode: benchmark.ModeJudge, Aggregate: benchmark.Aggregate{SolveRate: 0.8, AvgTokensPerSecond: 42}},
		{ID: "run-xyz", ProfileName: "Prof B", Mode: benchmark.ModeMathBench, Err: "boom"},
	}}
	var out, errw bytes.Buffer
	if code := benchPrintList(&out, &errw, stub, false); code != 0 {
		t.Fatalf("code=%d err=%s", code, errw.String())
	}
	got := out.String()
	for _, want := range []string{"id", "run-abc", "run-xyz", "Prof A", "! "} {
		if !strings.Contains(got, want) {
			t.Fatalf("list output missing %q; got:\n%s", want, got)
		}
	}
}

func TestBenchPrintList_JSONShape(t *testing.T) {
	stub := &stubBenchStore{runs: []benchmark.Run{{ID: "r1", ProfileID: "p1"}}}
	var out, errw bytes.Buffer
	if code := benchPrintList(&out, &errw, stub, true); code != 0 {
		t.Fatalf("code=%d", code)
	}
	var got []benchmark.Run
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("list --json not a []Run: %v\n%s", err, out.String())
	}
	if len(got) != 1 || got[0].ID != "r1" {
		t.Fatalf("json shape wrong: %+v", got)
	}
}

func TestBenchEmptyStates(t *testing.T) {
	stub := &stubBenchStore{}
	cases := []struct {
		name string
		call func(out, errw *bytes.Buffer) int
		want string
	}{
		{"list", func(o, e *bytes.Buffer) int { return benchPrintList(o, e, stub, false) }, "no benchmark runs yet"},
		{"compare", func(o, e *bytes.Buffer) int { return benchPrintCompare(o, e, stub, false) }, "no runs to compare"},
		{"history", func(o, e *bytes.Buffer) int { return benchPrintHistory(o, e, stub, "nope", false) }, `no runs for profile "nope"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out, errw bytes.Buffer
			if code := c.call(&out, &errw); code != 0 {
				t.Fatalf("code=%d err=%s", code, errw.String())
			}
			if !strings.Contains(out.String(), c.want) {
				t.Fatalf("want %q, got %q", c.want, out.String())
			}
		})
	}
}

func TestBenchPrintShow(t *testing.T) {
	run := benchmark.Run{
		ID: "run-1", ProfileID: "p1", ProfileName: "Prof A", Mode: benchmark.ModeJudge,
		Aggregate: benchmark.Aggregate{SolveRate: 1, Resolved: 1, Total: 1, AvgScore: 1},
		Problems:  []benchmark.ProblemResult{{ProblemName: "prob-1", Resolved: true, Score: 1}},
	}
	stub := &stubBenchStore{byID: map[string]benchmark.Run{"run-1": run}}
	var out, errw bytes.Buffer
	if code := benchPrintShow(&out, &errw, stub, "run-1", false); code != 0 {
		t.Fatalf("code=%d err=%s", code, errw.String())
	}
	for _, want := range []string{"Prof A", "Solve:", "Problems:", "prob-1"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("show missing %q; got:\n%s", want, out.String())
		}
	}
}

func TestBenchPrintShow_NotFound(t *testing.T) {
	stub := &stubBenchStore{byID: map[string]benchmark.Run{}}
	var out, errw bytes.Buffer
	if code := benchPrintShow(&out, &errw, stub, "missing", false); code != 1 {
		t.Fatalf("expected code 1 for missing run, got %d", code)
	}
	if !strings.Contains(errw.String(), "show:") {
		t.Fatalf("expected error on stderr, got %q", errw.String())
	}
}

func TestBenchPrintShow_JSONShape(t *testing.T) {
	run := benchmark.Run{ID: "run-1", ProfileID: "p1"}
	stub := &stubBenchStore{byID: map[string]benchmark.Run{"run-1": run}}
	var out, errw bytes.Buffer
	if code := benchPrintShow(&out, &errw, stub, "run-1", true); code != 0 {
		t.Fatalf("code=%d", code)
	}
	var got benchmark.Run
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("show --json not a Run: %v", err)
	}
	if got.ID != "run-1" {
		t.Fatalf("json shape wrong: %+v", got)
	}
}

func TestBenchDelete_RefusesWithoutYes(t *testing.T) {
	stub := &stubBenchStore{byID: map[string]benchmark.Run{"r1": {ID: "r1"}}}
	var out, errw bytes.Buffer
	if code := benchDelete(&out, &errw, stub, "r1", false); code != 1 {
		t.Fatalf("delete without --yes must exit 1, got %d", code)
	}
	if !strings.Contains(errw.String(), "refusing") {
		t.Fatalf("expected refusal message, got %q", errw.String())
	}
	if len(stub.deleted) != 0 {
		t.Fatalf("nothing should have been deleted, got %v", stub.deleted)
	}
}

func TestBenchDelete_WithYes(t *testing.T) {
	stub := &stubBenchStore{byID: map[string]benchmark.Run{"r1": {ID: "r1"}}}
	var out, errw bytes.Buffer
	if code := benchDelete(&out, &errw, stub, "r1", true); code != 0 {
		t.Fatalf("delete --yes should succeed, got %d err=%s", code, errw.String())
	}
	if len(stub.deleted) != 1 || stub.deleted[0] != "r1" {
		t.Fatalf("expected r1 deleted, got %v", stub.deleted)
	}
	if !strings.Contains(out.String(), "deleted run r1") {
		t.Fatalf("expected confirmation, got %q", out.String())
	}
}

func TestBenchExport_WritesBothFiles(t *testing.T) {
	run := benchmark.Run{
		ID: "run-x", ProfileName: "P",
		Problems: []benchmark.ProblemResult{{ProblemID: "a", ProblemName: "prob-a", Resolved: true, Score: 1}},
	}
	stub := &stubBenchStore{byID: map[string]benchmark.Run{"run-x": run}}
	dir := t.TempDir()
	var out, errw bytes.Buffer
	if code := benchExport(&out, &errw, stub, "run-x", dir); code != 0 {
		t.Fatalf("export code=%d err=%s", code, errw.String())
	}
	for _, name := range []string{"run-x.json", "run-x.csv"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected exported %s: %v", name, err)
		}
	}
	if !strings.Contains(out.String(), "run-x.csv") {
		t.Fatalf("export should print the csv path, got %q", out.String())
	}
}

// --- --min-solve exit gate --------------------------------------------------

func TestPersistAndRender_MinSolveExit2(t *testing.T) {
	run := benchmark.Run{Aggregate: benchmark.Aggregate{SolveRate: 0.3, Total: 10, Resolved: 3}}
	var out, errw bytes.Buffer
	err := persistAndRenderBenchmarkRun(&out, &errw, &stubBenchStore{}, run, false, 0.7)
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 2 {
		t.Fatalf("expected *ExitError{Code:2}, got %v", err)
	}
	if !strings.Contains(errw.String(), "FAIL") {
		t.Fatalf("expected FAIL message on stderr, got %q", errw.String())
	}
}

func TestPersistAndRender_MinSolvePass(t *testing.T) {
	run := benchmark.Run{Aggregate: benchmark.Aggregate{SolveRate: 0.3, Total: 10, Resolved: 3}}
	var out, errw bytes.Buffer
	if err := persistAndRenderBenchmarkRun(&out, &errw, &stubBenchStore{}, run, false, 0.2); err != nil {
		t.Fatalf("solve 0.3 >= 0.2 should pass, got %v", err)
	}
}

// --- live renderer (plain mode, direct leaf calls) --------------------------

func TestBenchLive_PermanentItemLines(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	feed := benchmark.NewFeedForTest(
		benchmark.FeedSnapshot{Total: 3, Mode: benchmark.ModeMathBench},
		benchmark.Progress{Index: 1, Total: 3, ProblemID: "p1", ProblemName: "prob-1", Phase: "infer"},
		benchmark.Progress{Index: 1, Total: 3, ProblemID: "p1", ProblemName: "prob-1", Phase: "item_done", Outcome: "pass", Score: 0.9, ItemMs: 1200},
		benchmark.Progress{Index: 2, Total: 3, ProblemID: "p2", ProblemName: "prob-2", Phase: "infer"},
	)
	var buf bytes.Buffer
	l := newBenchLive(&buf, func() *benchmark.RunFeed { return feed }, false)
	l.renderPermanent(feed.Snapshot())
	got := buf.String()
	// finished item p1 (lossless done line)
	for _, want := range []string{"✓", "prob-1", "pass", "score=0.90", "1/3"} {
		if !strings.Contains(got, want) {
			t.Fatalf("done line missing %q; got:\n%s", want, got)
		}
	}
	// current item p2 (start line)
	for _, want := range []string{"→", "prob-2", "2/3"} {
		if !strings.Contains(got, want) {
			t.Fatalf("start line missing %q; got:\n%s", want, got)
		}
	}
	// no ANSI in non-TTY output
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("non-TTY output must not contain ANSI: %q", got)
	}
}

// UIUX-017: live renderer emits the done/aggregating phase (was silently dropped).
func TestBenchLive_DonePhaseAndFail(t *testing.T) {
	feed := benchmark.NewFeedForTest(
		benchmark.FeedSnapshot{Total: 1, Mode: benchmark.ModeMathBench},
		benchmark.Progress{Index: 1, Total: 1, ProblemID: "p1", ProblemName: "prob-1", Phase: "infer"},
		benchmark.Progress{Index: 1, Total: 1, ProblemID: "p1", ProblemName: "prob-1", Phase: "item_done", Outcome: "fail", Score: 0.2, ItemMs: 900},
		benchmark.Progress{Phase: "done"},
	)
	var buf bytes.Buffer
	l := newBenchLive(&buf, func() *benchmark.RunFeed { return feed }, false)
	l.renderPermanent(feed.Snapshot())
	got := buf.String()
	for _, want := range []string{"aggregating", "✗", "prob-1", "fail"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q; got:\n%s", want, got)
		}
	}
}

func TestBenchLive_StalenessTransition(t *testing.T) {
	// A local-mode run silent for 10 minutes crosses the stalled threshold.
	snap := benchmark.FeedSnapshot{
		Mode:         benchmark.ModeMathBench,
		StartedAt:    time.Now().Add(-10 * time.Minute),
		LastActivity: time.Now().Add(-10 * time.Minute),
	}
	var buf bytes.Buffer
	l := newBenchLive(&buf, func() *benchmark.RunFeed { return nil }, false)
	l.renderPermanent(snap)
	if !strings.Contains(buf.String(), "possibly hung") {
		t.Fatalf("expected stalled transition line, got:\n%s", buf.String())
	}
}

func TestBenchLive_DoneHighWaterMarkLossless(t *testing.T) {
	base := benchmark.FeedSnapshot{Total: 2, Mode: benchmark.ModeMathBench}
	feed1 := benchmark.NewFeedForTest(base,
		benchmark.Progress{Index: 1, Total: 2, ProblemID: "p1", ProblemName: "prob-1", Phase: "item_done", Outcome: "pass", Score: 1, ItemMs: 500},
	)
	feed2 := benchmark.NewFeedForTest(base,
		benchmark.Progress{Index: 1, Total: 2, ProblemID: "p1", ProblemName: "prob-1", Phase: "item_done", Outcome: "pass", Score: 1, ItemMs: 500},
		benchmark.Progress{Index: 2, Total: 2, ProblemID: "p2", ProblemName: "prob-2", Phase: "item_done", Outcome: "fail", Score: 0, ItemMs: 700},
	)
	var buf bytes.Buffer
	l := newBenchLive(&buf, func() *benchmark.RunFeed { return feed1 }, false)
	l.renderPermanent(feed1.Snapshot()) // poll 1: p1 finishes
	if !strings.Contains(buf.String(), "prob-1") {
		t.Fatalf("poll 1 should print prob-1; got:\n%s", buf.String())
	}
	buf.Reset()
	l.renderPermanent(feed2.Snapshot()) // poll 2: only p2 is new
	got := buf.String()
	if !strings.Contains(got, "prob-2") {
		t.Fatalf("poll 2 should print the newly finished prob-2; got:\n%s", got)
	}
	if strings.Contains(got, "prob-1") {
		t.Fatalf("poll 2 must not reprint prob-1 (high-water mark); got:\n%s", got)
	}
}

// failWriter fails every write, standing in for a broken stdout (closed pipe).
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("write boom") }

// UIUX-026: a JSON write failure propagates as a nonzero exit instead of being
// swallowed by `_ = emitJSON`.
func TestBenchPrintList_JSONWriteError(t *testing.T) {
	stub := &stubBenchStore{runs: []benchmark.Run{{ID: "r1"}}}
	var errw bytes.Buffer
	if code := benchPrintList(failWriter{}, &errw, stub, true); code != 1 {
		t.Fatalf("json write failure must exit 1, got %d", code)
	}
	if !strings.Contains(errw.String(), "encode json") {
		t.Fatalf("expected encode error on stderr, got %q", errw.String())
	}
}

// UIUX-026: persistAndRenderBenchmarkRun's JSON branch returns *ExitError{1} on
// a write failure.
func TestPersistAndRender_JSONWriteError(t *testing.T) {
	run := benchmark.Run{ID: "r1", Aggregate: benchmark.Aggregate{SolveRate: 1}}
	var errw bytes.Buffer
	err := persistAndRenderBenchmarkRun(failWriter{}, &errw, &stubBenchStore{}, run, true, -1)
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 1 {
		t.Fatalf("json write failure must return *ExitError{Code:1}, got %v", err)
	}
}

// UIUX-028: the partial-run path renders the same summary block as the success
// path — a partial (Err-set) run shows its per-problem rows and the
// '(partial run)' marker via the shared renderRun helper.
func TestRenderRun_PartialSummary(t *testing.T) {
	run := benchmark.Run{
		ProfileName: "Prof", ProfileID: "p1", Mode: benchmark.ModeJudge,
		Err:       "context canceled",
		Aggregate: benchmark.Aggregate{SolveRate: 0.5, Resolved: 1, Total: 2},
		Problems:  []benchmark.ProblemResult{{ProblemName: "prob-1", Resolved: true, Score: 1}},
	}
	var out, errw bytes.Buffer
	if err := renderRun(&out, &errw, run, false); err != nil {
		t.Fatalf("renderRun text: %v", err)
	}
	for _, want := range []string{"partial run", "prob-1", "Solve:", "Problems:"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("partial summary missing %q; got:\n%s", want, out.String())
		}
	}
}

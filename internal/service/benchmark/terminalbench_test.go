package benchmark

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestTerminalBench_Identity(t *testing.T) {
	h := terminalBenchHandler{}
	if h.Mode() != ModeTerminalBench {
		t.Fatalf("Mode() = %q, want %q", h.Mode(), ModeTerminalBench)
	}
	if h.Category() != CatAgentic {
		t.Fatalf("Category() = %q, want %q", h.Category(), CatAgentic)
	}
	if got := ModeTerminalBench.Title(); got != "Agentic terminal tasks (Terminal-Bench)" {
		t.Fatalf("Title() = %q", got)
	}
}

func TestTerminalBench_Registered(t *testing.T) {
	if _, ok := handlerFor(ModeTerminalBench); !ok {
		t.Fatal("ModeTerminalBench not registered")
	}
	if c, ok := CategoryOf(ModeTerminalBench); !ok || c != CatAgentic {
		t.Fatalf("CategoryOf(terminal-bench) = %q,%v", c, ok)
	}
}

func TestTerminalBench_Count(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want int
	}{
		{"explicit tasks", Config{TerminalBenchTasks: []string{"a", "b", "c"}}, 3},
		{"n-tasks only", Config{TerminalBenchNTasks: 5}, 5},
		{"tasks win over n-tasks", Config{TerminalBenchTasks: []string{"a"}, TerminalBenchNTasks: 9}, 1},
		{"unknown (whole dataset)", Config{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Runner{cfg: tc.cfg}
			if got := (terminalBenchHandler{}).Count(r); got != tc.want {
				t.Fatalf("Count = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestTerminalBench_Prepare_FailFast(t *testing.T) {
	origTB, origDocker := lookTB, lookDocker
	defer func() { lookTB, lookDocker = origTB, origDocker }()

	r := &Runner{cfg: Config{}}

	lookTB = func() (string, error) { return "", os.ErrNotExist }
	lookDocker = func() (string, error) { return "/usr/bin/docker", nil }
	if _, err := (terminalBenchHandler{}).Prepare(r); err == nil {
		t.Fatal("Prepare must fail when tb is missing")
	}

	lookTB = func() (string, error) { return "/usr/bin/tb", nil }
	lookDocker = func() (string, error) { return "", os.ErrNotExist }
	if _, err := (terminalBenchHandler{}).Prepare(r); err == nil {
		t.Fatal("Prepare must fail when docker is missing")
	}

	lookDocker = func() (string, error) { return "/usr/bin/docker", nil }
	scorer, err := (terminalBenchHandler{}).Prepare(r)
	if err != nil {
		t.Fatalf("Prepare with both present: %v", err)
	}
	if scorer != nil {
		t.Fatal("terminal-bench is objective; Prepare must return a nil scorer")
	}
}

func TestBuildTBArgs_Defaults(t *testing.T) {
	cfg := Config{TerminalBenchTasks: []string{"hello-world", "broken-pipe"}}
	got := buildTBArgs(cfg, "model-loader", "http://127.0.0.1:4321/v1", "my-profile", "/tmp/out")
	want := []string{
		"run",
		"--agent", "terminus",
		"--model", "openai/my-profile",
		"--agent-kwarg", "api_base=http://127.0.0.1:4321/v1",
		"--dataset", "terminal-bench-core==0.1.1",
		"--output-path", "/tmp/out",
		"--run-id", "model-loader",
		"--n-concurrent", "1",
		"--cleanup",
		"--task-id", "hello-world",
		"--task-id", "broken-pipe",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestBuildTBArgs_Overrides(t *testing.T) {
	cfg := Config{
		TerminalBenchAgent:      "terminus-2",
		TerminalBenchDataset:    "terminal-bench-core==0.2.0",
		TerminalBenchProvider:   "hosted_vllm",
		TerminalBenchNTasks:     3,
		TerminalBenchConcurrent: 2,
		TerminalBenchExtraArgs:  []string{"--no-rebuild"},
	}
	got := buildTBArgs(cfg, "rid", "http://h:1/v1", "p", "/o")
	want := []string{
		"run",
		"--agent", "terminus-2",
		"--model", "hosted_vllm/p",
		"--agent-kwarg", "api_base=http://h:1/v1",
		"--dataset", "terminal-bench-core==0.2.0",
		"--output-path", "/o",
		"--run-id", "rid",
		"--n-concurrent", "2",
		"--cleanup",
		"--n-tasks", "3",
		"--no-rebuild",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestTBAPIBase(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:4321":     "http://127.0.0.1:4321/v1",
		"http://127.0.0.1:4321/":    "http://127.0.0.1:4321/v1",
		"http://127.0.0.1:4321/v1":  "http://127.0.0.1:4321/v1",
		"http://127.0.0.1:4321/v1/": "http://127.0.0.1:4321/v1",
	}
	for in, want := range cases {
		if got := tbAPIBase(in); got != want {
			t.Fatalf("tbAPIBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTBEnv_AddsDummyKeyAndUnbuffer(t *testing.T) {
	env := tbEnv([]string{"PATH=/usr/bin", "HOME=/home/x"})
	has := func(k string) bool {
		for _, e := range env {
			if len(e) > len(k) && e[:len(k)+1] == k+"=" {
				return true
			}
		}
		return false
	}
	if !has("OPENAI_API_KEY") {
		t.Error("tbEnv must inject a dummy OPENAI_API_KEY (litellm openai route requires one)")
	}
	if !has("PYTHONUNBUFFERED") {
		t.Error("tbEnv must set PYTHONUNBUFFERED for live log flushing")
	}
	if !has("PATH") || !has("HOME") {
		t.Error("tbEnv must preserve the parent environment")
	}
}

func TestTBEnv_PreservesExistingKey(t *testing.T) {
	env := tbEnv([]string{"OPENAI_API_KEY=real-key"})
	count := 0
	for _, e := range env {
		if len(e) >= len("OPENAI_API_KEY=") && e[:len("OPENAI_API_KEY=")] == "OPENAI_API_KEY=" {
			count++
			if e != "OPENAI_API_KEY=real-key" {
				t.Errorf("must not overwrite a real OPENAI_API_KEY, got %q", e)
			}
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one OPENAI_API_KEY entry, got %d", count)
	}
}

const tbResultsFixture = `{
  "id": "11111111-1111-1111-1111-111111111111",
  "results": [
    {"trial_name":"hello-world.1-of-1","task_id":"hello-world","instruction":"do it","is_resolved":true,"failure_mode":"unset","total_input_tokens":1200,"total_output_tokens":340},
    {"trial_name":"broken-pipe.1-of-1","task_id":"broken-pipe","instruction":"do it","is_resolved":false,"failure_mode":"test_failed","total_input_tokens":2200,"total_output_tokens":510},
    {"trial_name":"crashy.1-of-1","task_id":"crashy","instruction":"do it","is_resolved":null,"failure_mode":"agent_timeout","total_input_tokens":null,"total_output_tokens":null}
  ],
  "n_resolved": 1,
  "n_unresolved": 2,
  "accuracy": 0.3333
}`

func TestParseTBResults_ToProblems(t *testing.T) {
	res, err := parseTBResults([]byte(tbResultsFixture))
	if err != nil {
		t.Fatalf("parseTBResults: %v", err)
	}
	probs := tbResultsToProblems(res)
	if len(probs) != 3 {
		t.Fatalf("got %d problems, want 3", len(probs))
	}

	hw := probs[0]
	if hw.ProblemID != "hello-world" || !hw.Resolved || hw.Score != 1 {
		t.Errorf("hello-world: %+v", hw)
	}
	if hw.PromptTokens != 1200 || hw.CompletionTokens != 340 {
		t.Errorf("hello-world tokens: in=%d out=%d", hw.PromptTokens, hw.CompletionTokens)
	}

	bp := probs[1]
	if bp.Resolved || bp.Score != 0 {
		t.Errorf("broken-pipe should be unresolved: %+v", bp)
	}
	if bp.Detail == "" {
		t.Error("broken-pipe should carry a failure_mode detail")
	}

	// is_resolved == null counts as an unresolved fail (keeps SolveRate aligned
	// with tb's accuracy = n_resolved / len(results)), NOT excluded as Errored.
	cr := probs[2]
	if cr.Resolved || cr.Err != "" {
		t.Errorf("null is_resolved must be unresolved-not-errored: %+v", cr)
	}
}

func TestTerminalBench_Finalize_SetsAccuracy(t *testing.T) {
	probs := []ProblemResult{
		{ProblemID: "a", Resolved: true, Score: 1},
		{ProblemID: "b", Resolved: false},
		{ProblemID: "c", Resolved: false},
		{ProblemID: "d", Resolved: true, Score: 1},
	}
	var agg Aggregate
	(terminalBenchHandler{}).Finalize(&agg, probs)
	if agg.TerminalBenchAccuracy != 0.5 {
		t.Fatalf("TerminalBenchAccuracy = %v, want 0.5", agg.TerminalBenchAccuracy)
	}
}

func TestCountCompletedTrials(t *testing.T) {
	dir := t.TempDir()
	// the aggregate results.json at the run root must NOT be counted as a trial
	mustWrite(t, filepath.Join(dir, "results.json"), "{}")
	mustWrite(t, filepath.Join(dir, "hello-world", "1-of-1", "results.json"), "{}")
	mustWrite(t, filepath.Join(dir, "broken-pipe", "1-of-1", "results.json"), "{}")
	mustWrite(t, filepath.Join(dir, "in-progress", "1-of-1", "commands.txt"), "echo") // no results yet
	if got := countCompletedTrials(dir); got != 2 {
		t.Fatalf("countCompletedTrials = %d, want 2", got)
	}
	if got := countCompletedTrials(filepath.Join(dir, "does-not-exist")); got != 0 {
		t.Fatalf("missing dir should count 0, got %d", got)
	}
}

func TestTerminalBench_Execute_ParsesResults(t *testing.T) {
	origTB := lookTB
	defer func() { lookTB = origTB }()
	lookTB = func() (string, error) { return writeFakeTB(t, tbResultsFixture, 0), nil }

	r := &Runner{cfg: Config{TerminalBenchTasks: []string{"hello-world", "broken-pipe", "crashy"}}}
	probs, _, err := (terminalBenchHandler{}).Execute(context.Background(), r, "http://127.0.0.1:4321", "my-profile", nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(probs) != 3 {
		t.Fatalf("got %d problems, want 3", len(probs))
	}
	if !probs[0].Resolved || probs[1].Resolved {
		t.Fatalf("resolved flags wrong: %v / %v", probs[0].Resolved, probs[1].Resolved)
	}
}

func TestTerminalBench_Execute_NoResultsIsError(t *testing.T) {
	origTB := lookTB
	defer func() { lookTB = origTB }()
	// fake tb that exits 0 but writes no results.json
	lookTB = func() (string, error) { return writeFakeTB(t, "", 0), nil }

	r := &Runner{cfg: Config{}}
	_, _, err := (terminalBenchHandler{}).Execute(context.Background(), r, "http://127.0.0.1:4321", "p", nil, nil)
	if err == nil {
		t.Fatal("Execute must error when tb produces no results.json")
	}
}

// --- helpers ---------------------------------------------------------------

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeFakeTB writes an executable bash stub that imitates `tb run`: it reads
// --output-path and --run-id from argv and, when results is non-empty, writes
// it to <output-path>/<run-id>/results.json. Returns the script path.
func writeFakeTB(t *testing.T, results string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-tb.sh")
	body := `#!/usr/bin/env bash
out=""
runid=""
prev=""
for a in "$@"; do
  case "$prev" in
    --output-path) out="$a" ;;
    --run-id) runid="$a" ;;
  esac
  prev="$a"
done
if [ -n "$RESULTS_JSON" ]; then
  mkdir -p "$out/$runid"
  printf '%s' "$RESULTS_JSON" > "$out/$runid/results.json"
fi
echo "fake tb finished"
exit ` + strconv.Itoa(exitCode) + `
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	// Pass the fixture through the environment so the harness's own env plumbing
	// (tbEnv) is exercised end-to-end.
	t.Setenv("RESULTS_JSON", results)
	return script
}

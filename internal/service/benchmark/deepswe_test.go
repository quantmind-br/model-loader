package benchmark

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestDeepSWE_Identity(t *testing.T) {
	h := deepSWEHandler{}
	if h.Mode() != ModeDeepSWE {
		t.Fatalf("Mode() = %q, want %q", h.Mode(), ModeDeepSWE)
	}
	if h.Category() != CatAgentic {
		t.Fatalf("Category() = %q, want %q", h.Category(), CatAgentic)
	}
	if got := ModeDeepSWE.Title(); got != "Agentic SWE tasks (DeepSWE)" {
		t.Fatalf("Title() = %q", got)
	}
}

func TestDeepSWE_Registered(t *testing.T) {
	if _, ok := handlerFor(ModeDeepSWE); !ok {
		t.Fatal("ModeDeepSWE not registered")
	}
	if c, ok := CategoryOf(ModeDeepSWE); !ok || c != CatAgentic {
		t.Fatalf("CategoryOf(deep-swe) = %q,%v", c, ok)
	}
}

func TestDeepSWE_Count(t *testing.T) {
	corpus := fakeDeepSWETasksDir(t, "alpha", "beta", "gamma")
	cases := []struct {
		name string
		cfg  Config
		want int
	}{
		{"explicit tasks", Config{DeepSWETasks: []string{"a", "b"}}, 2},
		{"n-tasks only", Config{DeepSWENTasks: 5}, 5},
		{"tasks win over n-tasks", Config{DeepSWETasks: []string{"a"}, DeepSWENTasks: 9}, 1},
		{"whole corpus from dir", Config{DeepSWETasksDir: corpus}, 3},
		{"missing dir", Config{DeepSWETasksDir: "/no/such/dir"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Runner{cfg: tc.cfg}
			if got := (deepSWEHandler{}).Count(r); got != tc.want {
				t.Fatalf("Count = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDeepSWE_Prepare_FailFast(t *testing.T) {
	origPier, origDocker := lookPier, lookDocker
	defer func() { lookPier, lookDocker = origPier, origDocker }()

	corpus := fakeDeepSWETasksDir(t, "alpha")

	// pier missing
	lookPier = func() (string, error) { return "", os.ErrNotExist }
	lookDocker = func() (string, error) { return "/usr/bin/docker", nil }
	if _, err := (deepSWEHandler{}).Prepare(&Runner{cfg: Config{DeepSWETasksDir: corpus}}); err == nil {
		t.Fatal("Prepare must fail when pier is missing")
	}

	// docker missing
	lookPier = func() (string, error) { return "/usr/bin/pier", nil }
	lookDocker = func() (string, error) { return "", os.ErrNotExist }
	if _, err := (deepSWEHandler{}).Prepare(&Runner{cfg: Config{DeepSWETasksDir: corpus}}); err == nil {
		t.Fatal("Prepare must fail when docker is missing")
	}
	lookDocker = func() (string, error) { return "/usr/bin/docker", nil }

	// tasks_dir unset
	if _, err := (deepSWEHandler{}).Prepare(&Runner{cfg: Config{}}); err == nil {
		t.Fatal("Prepare must fail when tasks_dir is unset")
	}

	// tasks_dir not a directory
	notDir := filepath.Join(t.TempDir(), "afile")
	mustWrite(t, notDir, "x")
	if _, err := (deepSWEHandler{}).Prepare(&Runner{cfg: Config{DeepSWETasksDir: notDir}}); err == nil {
		t.Fatal("Prepare must fail when tasks_dir is not a directory")
	}

	// empty corpus (no task.toml)
	empty := t.TempDir()
	if _, err := (deepSWEHandler{}).Prepare(&Runner{cfg: Config{DeepSWETasksDir: empty}}); err == nil {
		t.Fatal("Prepare must fail when corpus has no tasks")
	}

	// all present → nil scorer (objective mode)
	scorer, err := (deepSWEHandler{}).Prepare(&Runner{cfg: Config{DeepSWETasksDir: corpus}})
	if err != nil {
		t.Fatalf("Prepare with everything present: %v", err)
	}
	if scorer != nil {
		t.Fatal("deep-swe is objective; Prepare must return a nil scorer")
	}
}

func TestBuildDeepSWEArgs_Defaults(t *testing.T) {
	cfg := Config{DeepSWETasksDir: "/corpus/tasks"}
	args := buildDeepSWEArgs(cfg, "model-loader", "http://host.docker.internal:4321/v1", "model-loader-proxy", "my-profile", "/tmp/jobs")

	wantPairs := [][2]string{
		{"--path", "/corpus/tasks"},
		{"--agent", "mini-swe-agent"},
		{"--model", "openai/my-profile"},
		{"--agent-kwarg", "model_class=litellm"},
		{"--agent-env", "OPENAI_API_BASE=http://host.docker.internal:4321/v1"},
		{"--agent-env", "OPENAI_API_KEY=model-loader-proxy"},
		{"--jobs-dir", "/tmp/jobs"},
		{"--job-name", "model-loader"},
		{"--n-concurrent", "1"},
	}
	for _, p := range wantPairs {
		if !argsHavePair(args, p[0], p[1]) {
			t.Errorf("args missing pair %q %q\n got: %v", p[0], p[1], args)
		}
	}
	if !argsHaveFlag(args, "--yes") {
		t.Errorf("args missing --yes (auto-confirm), got: %v", args)
	}
	if argsHaveFlag(args, "--n-tasks") {
		t.Errorf("default must not emit --n-tasks, got: %v", args)
	}
	if args[0] != "run" {
		t.Errorf("args[0] = %q, want run", args[0])
	}
}

func TestBuildDeepSWEArgs_Overrides(t *testing.T) {
	cfg := Config{
		DeepSWETasksDir:   "/corpus/tasks",
		DeepSWEAgent:      "nop",
		DeepSWEProvider:   "hosted_vllm",
		DeepSWEModelClass: "litellm_textbased",
		DeepSWETasks:      []string{"abs-*", "anko-typed-variable-bindings"},
		DeepSWEConcurrent: 3,
		DeepSWEExtraArgs:  []string{"--force-build"},
	}
	args := buildDeepSWEArgs(cfg, "model-loader", "http://h:1/v1", "k", "p", "/tmp/jobs")

	wantPairs := [][2]string{
		{"--agent", "nop"},
		{"--model", "hosted_vllm/p"},
		{"--agent-kwarg", "model_class=litellm_textbased"},
		{"--n-concurrent", "3"},
		{"--include-task-name", "abs-*"},
		{"--include-task-name", "anko-typed-variable-bindings"},
	}
	for _, p := range wantPairs {
		if !argsHavePair(args, p[0], p[1]) {
			t.Errorf("args missing pair %q %q\n got: %v", p[0], p[1], args)
		}
	}
	if args[len(args)-1] != "--force-build" {
		t.Errorf("extra args must be appended last, got: %v", args)
	}
}

func TestBuildDeepSWEArgs_NTasksAddsSeed(t *testing.T) {
	cfg := Config{DeepSWETasksDir: "/c", DeepSWENTasks: 4, DeepSWESampleSeed: 7}
	args := buildDeepSWEArgs(cfg, "model-loader", "http://h:1/v1", "k", "p", "/tmp/jobs")
	if !argsHavePair(args, "--n-tasks", "4") {
		t.Errorf("args missing --n-tasks 4, got: %v", args)
	}
	if !argsHavePair(args, "--sample-seed", "7") {
		t.Errorf("args missing --sample-seed 7, got: %v", args)
	}
}

func TestDeepAPIBase(t *testing.T) {
	cases := []struct {
		proxyRoot string
		override  string
		want      string
	}{
		{"http://127.0.0.1:4321", "", "http://host.docker.internal:4321/v1"},
		{"http://127.0.0.1:4321/v1", "", "http://host.docker.internal:4321/v1"},
		{"http://localhost:4321", "", "http://host.docker.internal:4321/v1"},
		{"http://127.0.0.1:4321", "http://myhost:9999/v1", "http://myhost:9999/v1"},
		{"http://192.168.1.5:4321", "", "http://192.168.1.5:4321/v1"},
	}
	for _, tc := range cases {
		if got := deepAPIBase(tc.proxyRoot, tc.override); got != tc.want {
			t.Errorf("deepAPIBase(%q,%q) = %q, want %q", tc.proxyRoot, tc.override, got, tc.want)
		}
	}
}

func TestDeepAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	if got := deepAPIKey(); got != "model-loader-proxy" {
		t.Fatalf("empty key → %q, want dummy", got)
	}
	t.Setenv("OPENAI_API_KEY", "sk-real")
	if got := deepAPIKey(); got != "sk-real" {
		t.Fatalf("real key → %q, want sk-real", got)
	}
}

func TestDeepTrialsToProblems(t *testing.T) {
	in := 10
	out := 20
	trials := []deepTrialResult{
		{TaskName: "gamma", VerifierResult: rewardOf(0)},
		{TaskName: "alpha", VerifierResult: rewardOf(1), AgentResult: tokensOf(&in, &out)},
		{TaskName: "beta"}, // crashed before verification (no verifier_result)
	}
	probs := deepTrialsToProblems(trials)
	if len(probs) != 3 {
		t.Fatalf("got %d problems, want 3", len(probs))
	}
	// sorted by id: alpha, beta, gamma
	if probs[0].ProblemID != "alpha" || probs[1].ProblemID != "beta" || probs[2].ProblemID != "gamma" {
		t.Fatalf("not sorted by id: %v", []string{probs[0].ProblemID, probs[1].ProblemID, probs[2].ProblemID})
	}
	if !probs[0].Resolved || probs[0].Score != 1 {
		t.Errorf("alpha (reward 1) should be resolved with score 1, got %+v", probs[0])
	}
	if probs[0].PromptTokens != 10 || probs[0].CompletionTokens != 20 {
		t.Errorf("alpha token counts wrong: %+v", probs[0])
	}
	if probs[1].Resolved || probs[1].Detail != "incomplete" {
		t.Errorf("beta (no verifier) should be unresolved/incomplete, got %+v", probs[1])
	}
	if probs[2].Resolved || probs[2].Detail != "unresolved" {
		t.Errorf("gamma (reward 0) should be unresolved, got %+v", probs[2])
	}
}

func TestDeepDetail_VerifierError(t *testing.T) {
	// reward.txt crash sentinel (-1): a verifier error, distinct from a plain fail.
	probs := deepTrialsToProblems([]deepTrialResult{{TaskName: "x", VerifierResult: rewardOf(-1)}})
	if probs[0].Resolved {
		t.Fatal("reward -1 must not count as resolved")
	}
	if probs[0].Detail != "verifier-error" {
		t.Errorf("reward -1 detail = %q, want verifier-error", probs[0].Detail)
	}
}

func TestDeepParseRunDir_SkipsAggregate(t *testing.T) {
	runDir := t.TempDir()
	// job-root aggregate result.json (no task_name) — must be skipped.
	mustWrite(t, filepath.Join(runDir, "result.json"), `{"id":"x","n_total_trials":2,"stats":{}}`)
	// per-trial result.json files (one dir each).
	mustWrite(t, filepath.Join(runDir, "alpha", "result.json"), `{"task_name":"alpha","verifier_result":{"rewards":{"reward":1}}}`)
	mustWrite(t, filepath.Join(runDir, "beta", "result.json"), `{"task_name":"beta","verifier_result":{"rewards":{"reward":0}}}`)

	probs, err := deepParseRunDir(runDir)
	if err != nil {
		t.Fatalf("deepParseRunDir: %v", err)
	}
	if len(probs) != 2 {
		t.Fatalf("got %d problems, want 2 (root aggregate must be skipped)", len(probs))
	}
	if !probs[0].Resolved || probs[1].Resolved {
		t.Fatalf("resolved flags wrong: %v / %v", probs[0].Resolved, probs[1].Resolved)
	}
}

func TestDeepParseRunDir_NoTrialsIsError(t *testing.T) {
	runDir := t.TempDir()
	mustWrite(t, filepath.Join(runDir, "result.json"), `{"id":"x"}`) // only the aggregate
	if _, err := deepParseRunDir(runDir); err == nil {
		t.Fatal("deepParseRunDir must error when no per-trial result.json exists")
	}
}

func TestDeepSWE_Execute_ParsesResults(t *testing.T) {
	origPier := lookPier
	defer func() { lookPier = origPier }()
	lookPier = func() (string, error) { return writeFakePier(t, "alpha=1 beta=0 gamma=1", 0), nil }

	r := &Runner{cfg: Config{DeepSWETasks: []string{"alpha", "beta", "gamma"}}}
	probs, _, err := (deepSWEHandler{}).Execute(context.Background(), r, "http://127.0.0.1:4321", "my-profile", nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(probs) != 3 {
		t.Fatalf("got %d problems, want 3", len(probs))
	}
	got := map[string]bool{}
	for _, p := range probs {
		got[p.ProblemID] = p.Resolved
	}
	if !got["alpha"] || got["beta"] || !got["gamma"] {
		t.Fatalf("resolved flags wrong: %v", got)
	}
}

func TestDeepSWE_Execute_NoResultsIsError(t *testing.T) {
	origPier := lookPier
	defer func() { lookPier = origPier }()
	// fake pier that exits 0 but writes only the job-root aggregate (no trials)
	lookPier = func() (string, error) { return writeFakePier(t, "", 0), nil }

	r := &Runner{cfg: Config{}}
	_, _, err := (deepSWEHandler{}).Execute(context.Background(), r, "http://127.0.0.1:4321", "p", nil, nil)
	if err == nil {
		t.Fatal("Execute must error when pier produces no per-trial results")
	}
}

func TestDeepSWE_Finalize_SetsAccuracy(t *testing.T) {
	agg := &Aggregate{}
	probs := []ProblemResult{{Resolved: true}, {Resolved: false}, {Resolved: true}, {Resolved: false}}
	(deepSWEHandler{}).Finalize(agg, probs)
	if agg.DeepSWEAccuracy != 0.5 {
		t.Fatalf("DeepSWEAccuracy = %v, want 0.5", agg.DeepSWEAccuracy)
	}
}

func TestDeepCachedTaskCount(t *testing.T) {
	if n := deepCachedTaskCount(""); n != 0 {
		t.Fatalf("empty dir = %d, want 0", n)
	}
	corpus := fakeDeepSWETasksDir(t, "a", "b")
	// a non-task subdir (no task.toml) must not be counted.
	if err := os.MkdirAll(filepath.Join(corpus, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if n := deepCachedTaskCount(corpus); n != 2 {
		t.Fatalf("corpus count = %d, want 2", n)
	}
}

// --- helpers ---------------------------------------------------------------

func rewardOf(v float64) *struct {
	Rewards map[string]float64 `json:"rewards"`
} {
	return &struct {
		Rewards map[string]float64 `json:"rewards"`
	}{Rewards: map[string]float64{"reward": v}}
}

func tokensOf(in, out *int) *struct {
	NInputTokens  *int `json:"n_input_tokens"`
	NOutputTokens *int `json:"n_output_tokens"`
} {
	return &struct {
		NInputTokens  *int `json:"n_input_tokens"`
		NOutputTokens *int `json:"n_output_tokens"`
	}{NInputTokens: in, NOutputTokens: out}
}

func argsHaveFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func argsHavePair(args []string, flag, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}

// fakeDeepSWETasksDir creates a minimal corpus layout: one <id>/task.toml per
// task id, enough to satisfy deepCachedTaskCount and Prepare's checks.
func fakeDeepSWETasksDir(t *testing.T, ids ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, id := range ids {
		mustWrite(t, filepath.Join(dir, id, deepTaskManifest), "schema_version = \"1.1\"\n")
	}
	return dir
}

// writeFakePier writes an executable bash stub that imitates `pier run`: it reads
// --jobs-dir and --job-name from argv, writes a job-root aggregate result.json
// (no task_name — the walker must skip it), and, for each "task=reward" pair in
// $PIER_TRIALS, writes <jobs-dir>/<job-name>/<task>/result.json in the
// TrialResult shape. Returns the script path.
func writeFakePier(t *testing.T, trials string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-pier.sh")
	body := `#!/usr/bin/env bash
jobs=""
jobname=""
prev=""
for a in "$@"; do
  case "$prev" in
    --jobs-dir) jobs="$a" ;;
    --job-name) jobname="$a" ;;
  esac
  prev="$a"
done
rundir="$jobs/$jobname"
mkdir -p "$rundir"
printf '%s' '{"id":"00000000-0000-0000-0000-000000000000","n_total_trials":0,"stats":{}}' > "$rundir/result.json"
if [ -n "$PIER_TRIALS" ]; then
  for pair in $PIER_TRIALS; do
    name="${pair%%=*}"
    reward="${pair##*=}"
    mkdir -p "$rundir/$name"
    printf '{"task_name":"%s","trial_name":"%s","verifier_result":{"rewards":{"reward":%s}},"agent_result":{"n_input_tokens":10,"n_output_tokens":20}}' "$name" "$name" "$reward" > "$rundir/$name/result.json"
  done
fi
echo "fake pier finished"
exit ` + strconv.Itoa(exitCode) + `
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	// Pass the trials through the environment so the harness's own env plumbing
	// (tbEnv) is exercised end-to-end.
	t.Setenv("PIER_TRIALS", trials)
	return script
}

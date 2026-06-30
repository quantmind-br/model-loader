package benchmark

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestSweBenchPro_Identity(t *testing.T) {
	h := sweBenchProHandler{}
	if h.Mode() != ModeSweBenchPro {
		t.Fatalf("Mode() = %q, want %q", h.Mode(), ModeSweBenchPro)
	}
	if h.Category() != CatAgentic {
		t.Fatalf("Category() = %q, want %q", h.Category(), CatAgentic)
	}
	if got := ModeSweBenchPro.Title(); got != "Agentic SWE tasks (SWE-bench Pro)" {
		t.Fatalf("Title() = %q", got)
	}
}

func TestSweBenchPro_Registered(t *testing.T) {
	if _, ok := handlerFor(ModeSweBenchPro); !ok {
		t.Fatal("ModeSweBenchPro not registered")
	}
	if c, ok := CategoryOf(ModeSweBenchPro); !ok || c != CatAgentic {
		t.Fatalf("CategoryOf(swe-bench-pro) = %q,%v", c, ok)
	}
}

func TestSweBenchPro_Count(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want int
	}{
		{"explicit instances", Config{SweBenchProInstances: []string{"a", "b", "c"}}, 3},
		{"unknown (whole patch set)", Config{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Runner{cfg: tc.cfg}
			if got := (sweBenchProHandler{}).Count(r); got != tc.want {
				t.Fatalf("Count = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSweBenchPro_Prepare_FailFast(t *testing.T) {
	origDocker := lookDocker
	defer func() { lookDocker = origDocker }()
	lookDocker = func() (string, error) { return "/usr/bin/docker", nil }

	harness := fakeSWEAPHarness(t)
	py := fakeSWEAPPython(t, "", "", 0)

	rawSample := filepath.Join(harness, "helper_code", "sweap_eval_full_v2.jsonl")
	base := Config{
		SweBenchProHarnessDir: harness,
		SweBenchProRawSample:  rawSample,
		SweBenchProPython:     py,
		SweBenchProPatchPath:  filepath.Join(t.TempDir(), "patches.json"),
	}

	// missing harness dir
	bad := base
	bad.SweBenchProHarnessDir = ""
	if _, err := (sweBenchProHandler{}).Prepare(&Runner{cfg: bad}); err == nil {
		t.Error("Prepare must fail when harness dir is unset")
	}

	// harness dir without the eval script
	bad = base
	bad.SweBenchProHarnessDir = t.TempDir()
	if _, err := (sweBenchProHandler{}).Prepare(&Runner{cfg: bad}); err == nil {
		t.Error("Prepare must fail when swe_bench_pro_eval.py is missing")
	}

	// harness with the eval script + scripts dir but NO gather helper: gather runs
	// for the common flows, so Prepare must catch this before the expensive load.
	h2 := t.TempDir()
	mustWrite(t, filepath.Join(h2, "swe_bench_pro_eval.py"), "# fake")
	if err := os.MkdirAll(filepath.Join(h2, "run_scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	rs2 := filepath.Join(h2, "raw.jsonl")
	mustWrite(t, rs2, "{}\n")
	bad = base
	bad.SweBenchProHarnessDir = h2
	bad.SweBenchProRawSample = rs2
	if _, err := (sweBenchProHandler{}).Prepare(&Runner{cfg: bad}); err == nil {
		t.Error("Prepare must fail when helper_code/gather_patches.py is missing")
	}

	// raw sample not configured (no silent default → avoids the column-case trap)
	bad = base
	bad.SweBenchProRawSample = ""
	if _, err := (sweBenchProHandler{}).Prepare(&Runner{cfg: bad}); err == nil {
		t.Error("Prepare must fail when raw_sample_path is unset")
	}

	// docker missing (and not using Modal)
	lookDocker = func() (string, error) { return "", os.ErrNotExist }
	if _, err := (sweBenchProHandler{}).Prepare(&Runner{cfg: base}); err == nil {
		t.Error("Prepare must fail when docker is missing and Modal is off")
	}
	lookDocker = func() (string, error) { return "/usr/bin/docker", nil }

	// nothing to evaluate: no agent command and no patch path
	bad = base
	bad.SweBenchProPatchPath = ""
	bad.SweBenchProAgentCmd = nil
	if _, err := (sweBenchProHandler{}).Prepare(&Runner{cfg: bad}); err == nil {
		t.Error("Prepare must fail when neither an agent command nor patches are provided")
	}

	// python missing
	bad = base
	bad.SweBenchProPython = "definitely-not-a-real-binary-xyz"
	if _, err := (sweBenchProHandler{}).Prepare(&Runner{cfg: bad}); err == nil {
		t.Error("Prepare must fail when the python interpreter is missing")
	}

	// all present → objective mode, nil scorer
	scorer, err := (sweBenchProHandler{}).Prepare(&Runner{cfg: base})
	if err != nil {
		t.Fatalf("Prepare with everything present: %v", err)
	}
	if scorer != nil {
		t.Fatal("swe-bench-pro is objective; Prepare must return a nil scorer")
	}
}

func TestBuildSWEAPEvalArgs_Defaults(t *testing.T) {
	cfg := Config{} // all defaults
	got := buildSWEAPEvalArgs(cfg, "/h/swe_bench_pro_eval.py", "/h/raw.jsonl", "/o/patches.json", "/o/eval", "run_scripts")
	want := []string{
		"/h/swe_bench_pro_eval.py",
		"--raw_sample_path", "/h/raw.jsonl",
		"--patch_path", "/o/patches.json",
		"--output_dir", "/o/eval",
		"--scripts_dir", "run_scripts",
		"--dockerhub_username", "jefzda",
		"--num_workers", "4",
		"--use_local_docker",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestBuildSWEAPEvalArgs_Overrides(t *testing.T) {
	cfg := Config{
		SweBenchProDockerhubUser: "myuser",
		SweBenchProNumWorkers:    8,
		SweBenchProUseModal:      true, // omit --use_local_docker
		SweBenchProExtraArgs:     []string{"--block_network", "--redo"},
	}
	got := buildSWEAPEvalArgs(cfg, "/h/eval.py", "/h/raw.jsonl", "/o/p.json", "/o/eval", "/h/run_scripts")
	want := []string{
		"/h/eval.py",
		"--raw_sample_path", "/h/raw.jsonl",
		"--patch_path", "/o/p.json",
		"--output_dir", "/o/eval",
		"--scripts_dir", "/h/run_scripts",
		"--dockerhub_username", "myuser",
		"--num_workers", "8",
		"--block_network", "--redo",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestBuildSWEAPGatherArgs(t *testing.T) {
	got := buildSWEAPGatherArgs("/h/helper_code/gather_patches.py", "/o/preds", "model-loader", "/o/patches.json")
	want := []string{
		"/h/helper_code/gather_patches.py",
		"--directory", "/o/preds",
		"--prefix", "model-loader",
		"--output", "/o/patches.json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestSubstituteAgentArgs(t *testing.T) {
	tmpl := []string{"sweagent", "run-batch", "--model={model}", "--api-base", "{api_base}", "--out={output}", "--instances={instances}", "--cfg={harness}/c.yaml"}
	subs := map[string]string{
		"{model}":     "my-profile",
		"{api_base}":  "http://127.0.0.1:4321/v1",
		"{output}":    "/o/preds",
		"{instances}": "instance_a,instance_b",
		"{harness}":   "/h",
	}
	got := substituteAgentArgs(tmpl, subs)
	want := []string{"sweagent", "run-batch", "--model=my-profile", "--api-base", "http://127.0.0.1:4321/v1", "--out=/o/preds", "--instances=instance_a,instance_b", "--cfg=/h/c.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("substitution mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestParseSWEAPResults_ToProblems(t *testing.T) {
	data := []byte(`{"instance_zeta":true,"instance_alpha":false,"instance_mid":true}`)
	res, err := parseSWEAPResults(data)
	if err != nil {
		t.Fatalf("parseSWEAPResults: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("got %d results, want 3", len(res))
	}
	probs := sweapResultsToProblems(res)
	if len(probs) != 3 {
		t.Fatalf("got %d problems, want 3", len(probs))
	}
	// keys must be emitted in sorted order for determinism
	wantOrder := []string{"instance_alpha", "instance_mid", "instance_zeta"}
	for i, want := range wantOrder {
		if probs[i].ProblemID != want {
			t.Fatalf("problem[%d].ProblemID = %q, want %q (results must be sorted)", i, probs[i].ProblemID, want)
		}
	}
	// alpha = false → unresolved, score 0; zeta = true → resolved, score 1
	if probs[0].Resolved || probs[0].Score != 0 {
		t.Errorf("instance_alpha should be unresolved: %+v", probs[0])
	}
	if !probs[2].Resolved || probs[2].Score != 1 {
		t.Errorf("instance_zeta should be resolved score 1: %+v", probs[2])
	}
	if probs[0].Detail == "" || probs[2].Detail == "" {
		t.Error("each problem should carry a human Detail")
	}
}

func TestParseSWEAPResults_Garbled(t *testing.T) {
	if _, err := parseSWEAPResults([]byte("not json")); err == nil {
		t.Fatal("parseSWEAPResults must error on garbled input")
	}
}

func TestSweBenchPro_Finalize_SetsAccuracy(t *testing.T) {
	probs := []ProblemResult{
		{ProblemID: "a", Resolved: true, Score: 1},
		{ProblemID: "b", Resolved: false},
		{ProblemID: "c", Resolved: false},
		{ProblemID: "d", Resolved: true, Score: 1},
	}
	var agg Aggregate
	(sweBenchProHandler{}).Finalize(&agg, probs)
	if agg.SweBenchProAccuracy != 0.5 {
		t.Fatalf("SweBenchProAccuracy = %v, want 0.5", agg.SweBenchProAccuracy)
	}
}

func TestCountSWEAPOutputs(t *testing.T) {
	dir := t.TempDir()
	// eval_results.json at the root must NOT count as a per-instance output
	mustWrite(t, filepath.Join(dir, "eval_results.json"), "{}")
	mustWrite(t, filepath.Join(dir, "instance_a", "model-loader_output.json"), "{}")
	mustWrite(t, filepath.Join(dir, "instance_b", "model-loader_output.json"), "{}")
	mustWrite(t, filepath.Join(dir, "instance_c", "model-loader_stdout.log"), "log") // started, no output yet
	if got := countSWEAPOutputs(dir); got != 2 {
		t.Fatalf("countSWEAPOutputs = %d, want 2", got)
	}
	if got := countSWEAPOutputs(filepath.Join(dir, "missing")); got != 0 {
		t.Fatalf("missing dir should count 0, got %d", got)
	}
}

func TestFilterSWEAPPatches(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "all.json")
	mustWrite(t, src, `[
	  {"instance_id":"instance_a","patch":"d1","prefix":"x"},
	  {"instance_id":"instance_b","patch":"d2","prefix":"x"},
	  {"instance_id":"instance_c","patch":"d3","prefix":"x"}
	]`)
	dst := filepath.Join(dir, "filtered.json")
	n, err := filterSWEAPPatches(src, []string{"instance_a", "instance_c", "instance_missing"}, dst)
	if err != nil {
		t.Fatalf("filterSWEAPPatches: %v", err)
	}
	if n != 2 {
		t.Fatalf("kept %d, want 2", n)
	}
	var kept []map[string]any
	raw, _ := os.ReadFile(dst)
	if err := json.Unmarshal(raw, &kept); err != nil {
		t.Fatalf("unmarshal filtered: %v", err)
	}
	ids := map[string]bool{}
	for _, k := range kept {
		ids[k["instance_id"].(string)] = true
	}
	if !ids["instance_a"] || !ids["instance_c"] || ids["instance_b"] {
		t.Fatalf("filtered set wrong: %v", ids)
	}
}

func TestSweBenchPro_Execute_EvalOnly(t *testing.T) {
	origDocker := lookDocker
	defer func() { lookDocker = origDocker }()
	lookDocker = func() (string, error) { return "/usr/bin/docker", nil }

	harness := fakeSWEAPHarness(t)
	py := fakeSWEAPPython(t, "", `{"instance_foo":true,"instance_bar":false}`, 0)

	// a ready-made patches.json → agent + gather are skipped
	patches := filepath.Join(t.TempDir(), "patches.json")
	mustWrite(t, patches, `[{"instance_id":"instance_foo","patch":"d","prefix":"x"}]`)

	r := &Runner{cfg: Config{
		SweBenchProHarnessDir: harness,
		SweBenchProPython:     py,
		SweBenchProPatchPath:  patches,
	}}
	probs, _, err := (sweBenchProHandler{}).Execute(context.Background(), r, "http://127.0.0.1:4321", "my-profile", nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(probs) != 2 {
		t.Fatalf("got %d problems, want 2", len(probs))
	}
	got := map[string]bool{}
	for _, p := range probs {
		got[p.ProblemID] = p.Resolved
	}
	if !got["instance_foo"] || got["instance_bar"] {
		t.Fatalf("resolved flags wrong: %v", got)
	}
}

func TestSweBenchPro_Execute_FullPipeline(t *testing.T) {
	origDocker := lookDocker
	defer func() { lookDocker = origDocker }()
	lookDocker = func() (string, error) { return "/usr/bin/docker", nil }

	harness := fakeSWEAPHarness(t)
	agent := fakeSWEAPAgent(t)
	py := fakeSWEAPPython(t,
		`[{"instance_id":"instance_foo","patch":"d","prefix":"model-loader"}]`, // gather output
		`{"instance_foo":true,"instance_bar":false}`,                           // eval output
		0)

	r := &Runner{cfg: Config{
		SweBenchProHarnessDir: harness,
		SweBenchProPython:     py,
		SweBenchProAgentCmd:   []string{agent, "--model", "{model}", "--api-base", "{api_base}", "--out", "{output}"},
	}}
	probs, _, err := (sweBenchProHandler{}).Execute(context.Background(), r, "http://127.0.0.1:4321", "my-profile", nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(probs) != 2 {
		t.Fatalf("got %d problems, want 2 (agent→gather→eval)", len(probs))
	}
}

func TestSweBenchPro_Execute_NoResultsIsError(t *testing.T) {
	origDocker := lookDocker
	defer func() { lookDocker = origDocker }()
	lookDocker = func() (string, error) { return "/usr/bin/docker", nil }

	harness := fakeSWEAPHarness(t)
	py := fakeSWEAPPython(t, "", "", 0) // eval writes no eval_results.json

	patches := filepath.Join(t.TempDir(), "patches.json")
	mustWrite(t, patches, `[{"instance_id":"instance_foo","patch":"d","prefix":"x"}]`)

	r := &Runner{cfg: Config{
		SweBenchProHarnessDir: harness,
		SweBenchProPython:     py,
		SweBenchProPatchPath:  patches,
	}}
	if _, _, err := (sweBenchProHandler{}).Execute(context.Background(), r, "http://127.0.0.1:4321", "p", nil, nil); err == nil {
		t.Fatal("Execute must error when the eval produces no eval_results.json")
	}
}

func TestSweBenchPro_Execute_EmptyResultsIsError(t *testing.T) {
	origDocker := lookDocker
	defer func() { lookDocker = origDocker }()
	lookDocker = func() (string, error) { return "/usr/bin/docker", nil }

	harness := fakeSWEAPHarness(t)
	// The real eval writes eval_results.json={} then crashes when zero patches
	// matched the dataset; an empty verdict map means nothing was evaluated.
	py := fakeSWEAPPython(t, "", `{}`, 1)

	patches := filepath.Join(t.TempDir(), "patches.json")
	mustWrite(t, patches, `[{"instance_id":"instance_foo","patch":"d","prefix":"x"}]`)

	r := &Runner{cfg: Config{
		SweBenchProHarnessDir: harness,
		SweBenchProPython:     py,
		SweBenchProPatchPath:  patches,
	}}
	if _, _, err := (sweBenchProHandler{}).Execute(context.Background(), r, "http://127.0.0.1:4321", "p", nil, nil); err == nil {
		t.Fatal("Execute must error when the eval evaluated zero instances (empty eval_results.json)")
	}
}

func TestSweBenchPro_Execute_PatchPathWinsOverAgent(t *testing.T) {
	origDocker := lookDocker
	defer func() { lookDocker = origDocker }()
	lookDocker = func() (string, error) { return "/usr/bin/docker", nil }

	harness := fakeSWEAPHarness(t)
	// An agent stub that always FAILS: if precedence were wrong and the agent ran,
	// Execute would error. Supplied patches must win and skip the agent entirely.
	boom := filepath.Join(t.TempDir(), "boom.sh")
	if err := os.WriteFile(boom, []byte("#!/usr/bin/env bash\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	py := fakeSWEAPPython(t, "", `{"instance_foo":true}`, 0)
	patches := filepath.Join(t.TempDir(), "patches.json")
	mustWrite(t, patches, `[{"instance_id":"instance_foo","patch":"d","prefix":"x"}]`)

	r := &Runner{cfg: Config{
		SweBenchProHarnessDir: harness,
		SweBenchProPython:     py,
		SweBenchProPatchPath:  patches,
		SweBenchProAgentCmd:   []string{boom}, // would fail the run if executed
	}}
	probs, _, err := (sweBenchProHandler{}).Execute(context.Background(), r, "http://127.0.0.1:4321", "p", nil, nil)
	if err != nil {
		t.Fatalf("Execute should use patch_path and skip the agent, got: %v", err)
	}
	if len(probs) != 1 || !probs[0].Resolved {
		t.Fatalf("expected 1 resolved problem from the supplied patches, got %+v", probs)
	}
}

func TestFilterSWEAPPatches_PreservesAllFields(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "preds.json")
	// The eval prefers the canonical model_patch key over patch; the filter must
	// not drop it (or any other field) when narrowing to a subset.
	mustWrite(t, src, `[
	  {"instance_id":"instance_a","model_patch":"diff-a","prefix":"x","extra":"keep"},
	  {"instance_id":"instance_b","patch":"diff-b"}
	]`)
	dst := filepath.Join(dir, "out.json")
	n, err := filterSWEAPPatches(src, []string{"instance_a"}, dst)
	if err != nil {
		t.Fatalf("filterSWEAPPatches: %v", err)
	}
	if n != 1 {
		t.Fatalf("kept %d, want 1", n)
	}
	var kept []map[string]any
	raw, _ := os.ReadFile(dst)
	if err := json.Unmarshal(raw, &kept); err != nil {
		t.Fatalf("unmarshal filtered: %v", err)
	}
	if len(kept) != 1 {
		t.Fatalf("want 1 entry, got %d", len(kept))
	}
	e := kept[0]
	if e["model_patch"] != "diff-a" {
		t.Errorf("model_patch dropped by filter: %v", e)
	}
	if e["extra"] != "keep" {
		t.Errorf("extra field dropped by filter: %v", e)
	}
}

func TestSweapResolve(t *testing.T) {
	cases := []struct{ harness, p, want string }{
		{"/h", "run_scripts", "/h/run_scripts"},    // relative → harness-relative (matches cmd.Dir)
		{"/h", "/abs/raw.jsonl", "/abs/raw.jsonl"}, // absolute → unchanged
		{"/h", "", ""}, // empty → empty
	}
	for _, c := range cases {
		if got := sweapResolve(c.harness, c.p); got != c.want {
			t.Errorf("sweapResolve(%q,%q) = %q, want %q", c.harness, c.p, got, c.want)
		}
	}
}

// --- helpers ---------------------------------------------------------------

// fakeSWEAPHarness creates a minimal cloned-harness layout: the eval script, the
// gather helper, the raw-sample file, and the run_scripts dir — enough to satisfy
// the existence checks in Prepare/Execute.
func fakeSWEAPHarness(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "swe_bench_pro_eval.py"), "# fake")
	mustWrite(t, filepath.Join(dir, "helper_code", "gather_patches.py"), "# fake")
	mustWrite(t, filepath.Join(dir, "helper_code", "sweap_eval_full_v2.jsonl"), "{}\n")
	if err := os.MkdirAll(filepath.Join(dir, "run_scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// fakeSWEAPPython writes a bash stub standing in for the python interpreter. It
// branches on the invoked script: for gather_patches.py it writes gatherPatches
// to --output; for swe_bench_pro_eval.py it writes evalResults to
// <output_dir>/eval_results.json (only when evalResults is non-empty). The
// payloads ride through the environment so the harness's env plumbing (tbEnv) is
// exercised end-to-end. Returns the stub path.
func fakeSWEAPPython(t *testing.T, gatherPatches, evalResults string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-python.sh")
	body := `#!/usr/bin/env bash
target="$1"; shift
out=""; outdir=""; prev=""
for a in "$@"; do
  case "$prev" in
    --output) out="$a" ;;
    --output_dir) outdir="$a" ;;
  esac
  prev="$a"
done
base="$(basename "$target")"
if [ "$base" = "gather_patches.py" ]; then
  [ -n "$out" ] && printf '%s' "$SWEAP_GATHER" > "$out"
elif [ "$base" = "swe_bench_pro_eval.py" ]; then
  if [ -n "$SWEAP_EVAL" ] && [ -n "$outdir" ]; then
    mkdir -p "$outdir"
    printf '%s' "$SWEAP_EVAL" > "$outdir/eval_results.json"
  fi
fi
echo "fake sweap python: $base"
exit ` + strconv.Itoa(exitCode) + `
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SWEAP_GATHER", gatherPatches)
	t.Setenv("SWEAP_EVAL", evalResults)
	return script
}

// fakeSWEAPAgent writes a bash stub standing in for the patch-generation agent.
// It succeeds without producing real preds (the gather stub is itself faked).
func fakeSWEAPAgent(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-agent.sh")
	body := `#!/usr/bin/env bash
echo "fake sweap agent: $@"
exit 0
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

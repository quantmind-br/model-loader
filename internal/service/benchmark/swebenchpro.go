package benchmark

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// SWE-bench Pro mode wraps the external harness (github.com/scaleapi
// /SWE-bench_Pro-os). Like Terminal-Bench — and unlike the single-turn modes — it
// is an agentic, Docker-sandboxed loop owned by Python scripts, so we shell out
// to it rather than scoring in Go. The pipeline is three stages:
//
//  1. generate (optional): a patch-generation agent pointed at our proxy
//     (LiteLLM openai/<profile-id> + api_base) writes one diff per instance;
//  2. gather: helper_code/gather_patches.py consolidates the diffs into a single
//     patches JSON;
//  3. evaluate: swe_bench_pro_eval.py applies each patch in that instance's
//     prebuilt Docker image, runs its fail_to_pass + pass_to_pass tests, and
//     writes eval_results.json ({instance_id: bool}).
//
// model-loader supplies the model (the proxy) and records the score
// (benchmarkstore); the harness owns everything in between. All harness scripts
// run with CWD = the harness dir so its dockerfiles/, run_scripts/, and
// helper_code/ resolve.
const (
	sweapDefaultDockerhubUser = "jefzda"
	sweapDefaultPython        = "python3"
	sweapDefaultNumWorkers    = 4
	// sweapPrefix tags the gathered patches (gather_patches.py --prefix).
	sweapPrefix = "model-loader"

	sweapEvalScriptName  = "swe_bench_pro_eval.py"
	sweapGatherScriptRel = "helper_code/gather_patches.py"
	sweapScriptsDirName  = "run_scripts"
)

func init() { registerHandler(sweBenchProHandler{}) }

type sweBenchProHandler struct{}

func (sweBenchProHandler) Mode() Mode         { return ModeSweBenchPro }
func (sweBenchProHandler) Category() Category { return CatAgentic }

// Count reports how many instances the run covers when an explicit subset is
// configured; otherwise the size is only known once the patch set is resolved,
// so progress Total starts unknown (0).
func (sweBenchProHandler) Count(r *Runner) int {
	return len(r.cfg.SweBenchProInstances)
}

// Prepare fails fast (before the expensive proxy load) when the external tool
// chain is missing or misconfigured: the engine wraps the harness, it does not
// install it.
func (sweBenchProHandler) Prepare(r *Runner) (Scorer, error) {
	cfg := r.cfg
	if cfg.SweBenchProHarnessDir == "" {
		return nil, fmt.Errorf("swe-bench-pro mode needs a cloned harness: set benchmark.swebenchpro.harness_dir (git clone https://github.com/scaleapi/SWE-bench_Pro-os)")
	}
	harness := cfg.SweBenchProHarnessDir
	if _, err := os.Stat(sweapEvalScript(harness)); err != nil {
		return nil, fmt.Errorf("swe-bench-pro harness dir %q has no %s — is it a cloned SWE-bench_Pro-os checkout? %w", harness, sweapEvalScriptName, err)
	}
	// gather_patches.py runs in the common flows (agent run, or a preds-dir
	// patch_path), so validate it up front: a partial checkout otherwise sails past
	// Prepare and dies at stage 2 only after the expensive proxy load (and agent run).
	if _, err := os.Stat(sweapGatherScript(harness)); err != nil {
		return nil, fmt.Errorf("swe-bench-pro harness dir %q has no %s — is it a complete checkout? %w", harness, sweapGatherScriptRel, err)
	}
	// raw_sample_path is required (no default): the bundled
	// helper_code/sweap_eval_full_v2.jsonl uses UPPERCASE FAIL_TO_PASS/PASS_TO_PASS
	// while the eval reads lowercase fail_to_pass/pass_to_pass, so pointing there
	// silently scores every instance False. The operator must supply a raw sample
	// with lowercase columns (see docs/swe-bench-pro.md).
	if cfg.SweBenchProRawSample == "" {
		return nil, fmt.Errorf("swe-bench-pro mode needs benchmark.swebenchpro.raw_sample_path (a CSV/JSONL with lowercase fail_to_pass/pass_to_pass columns; see docs/swe-bench-pro.md)")
	}
	// Resolve relative paths against the harness dir, matching Execute's cmd.Dir, so
	// Prepare's existence checks see the same files the harness subprocess will.
	if rs := sweapResolve(harness, cfg.SweBenchProRawSample); !sweapExists(rs) {
		return nil, fmt.Errorf("swe-bench-pro raw sample not found (%s)", rs)
	}
	if sd := sweapResolve(harness, sweapScriptsDir(cfg)); !sweapExists(sd) {
		return nil, fmt.Errorf("swe-bench-pro scripts dir not found (%s)", sd)
	}
	if len(cfg.SweBenchProAgentCmd) == 0 && cfg.SweBenchProPatchPath == "" {
		return nil, fmt.Errorf("swe-bench-pro mode needs patches to evaluate: set benchmark.swebenchpro.patch_path (a patches JSON or a preds dir) or agent_cmd (a patch-generation command)")
	}
	if _, err := exec.LookPath(sweapPython(cfg)); err != nil {
		return nil, fmt.Errorf("swe-bench-pro mode needs python on PATH (%s): %w", sweapPython(cfg), err)
	}
	if !cfg.SweBenchProUseModal {
		if _, err := lookDocker(); err != nil {
			return nil, fmt.Errorf("swe-bench-pro mode needs Docker on PATH and running (or set use_modal): %w", err)
		}
	}
	return nil, nil // objective mode: no scorer
}

// Finalize records the resolved-instance rate, mirroring the generic SolveRate
// but naming it explicitly for the UI.
func (sweBenchProHandler) Finalize(agg *Aggregate, problems []ProblemResult) {
	total := len(problems)
	if total == 0 {
		return
	}
	resolved := 0
	for _, p := range problems {
		if p.Resolved {
			resolved++
		}
	}
	agg.SweBenchProAccuracy = float64(resolved) / float64(total)
}

// Execute drives the three-stage pipeline and parses eval_results.json. A
// missing/garbled eval_results.json is a hard error surfacing the path and the
// tail of the harness output so the operator can diagnose the external run.
func (h sweBenchProHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	cfg := r.cfg
	harness := cfg.SweBenchProHarnessDir

	outDir, err := os.MkdirTemp("", "sweap-run-*")
	if err != nil {
		return nil, nil, fmt.Errorf("create swe-bench-pro output dir: %w", err)
	}
	defer os.RemoveAll(outDir)
	predsDir := filepath.Join(outDir, "preds")
	evalOut := filepath.Join(outDir, "eval")

	runCtx := ctx
	if cfg.SweBenchProTimeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, cfg.SweBenchProTimeout)
		defer cancel()
	}

	hlog := newHarnessLog(cfg.HarnessLogDir, "swe-bench-pro", r.harnessLineEmitter(ModeSweBenchPro))
	defer hlog.Close()
	total := h.Count(r)
	r.logger().Info("benchmark_harness_start", "run_id", r.runID, "mode", ModeSweBenchPro, "harness", harness, "total", total, "log", hlog.path)

	// --- Stage 1: obtain patches -------------------------------------------
	// Supplied patches (patch_path) take precedence over the agent: a consolidated
	// .json is used directly (gather skipped), a preds dir is gathered, and only
	// when no patches are supplied do we run the agent (Prepare guarantees one of
	// the two is set). This matches docs/swe-bench-pro.md ("patch_path skips agent").
	gatherDir := ""
	patchesPath := ""
	if patchInput := sweapResolve(harness, cfg.SweBenchProPatchPath); patchInput != "" {
		if strings.HasSuffix(patchInput, ".json") {
			patchesPath = patchInput // consolidated patches: skip gather
		} else {
			gatherDir = patchInput // a preds dir: gather it
		}
	} else {
		if err := os.MkdirAll(predsDir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("create preds dir: %w", err)
		}
		send(progress, Progress{Index: 0, Total: total, ProblemID: "swe-bench-pro", ProblemName: "SWE-bench Pro: generating patches…", Phase: "infer"})
		agentArgs := substituteAgentArgs(cfg.SweBenchProAgentCmd, map[string]string{
			"{model}":     model,
			"{api_base}":  tbAPIBase(base),
			"{output}":    predsDir,
			"{instances}": strings.Join(cfg.SweBenchProInstances, ","),
			"{harness}":   harness,
		})
		if err := h.runStep(runCtx, "agent", harness, agentArgs[0], agentArgs[1:], hlog); err != nil {
			return nil, nil, fmt.Errorf("%w\n--- harness output (tail) ---\n%s", err, hlog.diagTail())
		}
		gatherDir = predsDir
	}

	// --- Stage 2: gather patches into a single JSON -------------------------
	if patchesPath == "" {
		send(progress, Progress{Index: 0, Total: total, ProblemID: "swe-bench-pro", ProblemName: "SWE-bench Pro: consolidating patches…", Phase: "infer"})
		gathered := filepath.Join(outDir, "patches.json")
		gargs := buildSWEAPGatherArgs(sweapGatherScript(harness), gatherDir, sweapPrefix, gathered)
		if err := h.runStep(runCtx, "gather", harness, sweapPython(cfg), gargs, hlog); err != nil {
			return nil, nil, fmt.Errorf("%w\n--- harness output (tail) ---\n%s", err, hlog.diagTail())
		}
		patchesPath = gathered
	}

	// Optional subset filter: the eval evaluates whatever is in the patch file,
	// so we narrow the set ourselves when specific instances were requested.
	if len(cfg.SweBenchProInstances) > 0 {
		filtered := filepath.Join(outDir, "patches.filtered.json")
		n, ferr := filterSWEAPPatches(patchesPath, cfg.SweBenchProInstances, filtered)
		if ferr != nil {
			return nil, nil, fmt.Errorf("filter patches to requested instances: %w", ferr)
		}
		if n == 0 {
			return nil, nil, fmt.Errorf("none of the requested instances %v have a patch in %s", cfg.SweBenchProInstances, patchesPath)
		}
		patchesPath = filtered
	}

	// --- Stage 3: evaluate --------------------------------------------------
	if err := os.MkdirAll(evalOut, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create eval output dir: %w", err)
	}
	send(progress, Progress{Index: 0, Total: total, ProblemID: "swe-bench-pro", ProblemName: "SWE-bench Pro harness (Docker) evaluating…", Phase: "infer"})
	stopPoll := make(chan struct{})
	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		sweapProgressPoller(evalOut, total, progress, stopPoll)
	}()

	eargs := buildSWEAPEvalArgs(cfg, sweapEvalScript(harness), sweapResolve(harness, cfg.SweBenchProRawSample), patchesPath, evalOut, sweapResolve(harness, sweapScriptsDir(cfg)))
	evalErr := h.runStep(runCtx, "eval", harness, sweapPython(cfg), eargs, hlog)
	r.logger().Info("benchmark_harness_exit", "run_id", r.runID, "mode", ModeSweBenchPro, "err", evalErr)
	close(stopPoll)
	<-pollDone // join before deferred os.RemoveAll(outDir) races the poller (audit N-C16)

	resultsPath := filepath.Join(evalOut, "eval_results.json")
	data, readErr := os.ReadFile(resultsPath)
	if readErr != nil {
		return nil, nil, fmt.Errorf("swe-bench-pro produced no results at %s (eval exited: %v)\n--- harness output (tail) ---\n%s",
			resultsPath, evalErr, hlog.diagTail())
	}
	res, perr := parseSWEAPResults(data)
	if perr != nil {
		return nil, nil, fmt.Errorf("%w\n--- harness output (tail) ---\n%s", perr, hlog.diagTail())
	}
	// An empty verdict map means the eval evaluated nothing — typically because no
	// supplied patch matched an instance_id in the raw sample (the harness writes
	// {} and then crashes on its accuracy print). Surface it rather than recording
	// a hollow zero-instance run.
	if len(res) == 0 {
		return nil, nil, fmt.Errorf("swe-bench-pro evaluated zero instances — no supplied patch matched an instance_id in %s (eval exited: %v)\n--- harness output (tail) ---\n%s",
			cfg.SweBenchProRawSample, evalErr, hlog.diagTail())
	}
	problems := sweapResultsToProblems(res)
	// Per-instance item_done, emitted from the authoritative eval_results.json.
	// Unlike tb/deep-swe (whose harnesses drop a per-trial results file the moment
	// each trial finishes, so their pollers can stream live outcomes), the
	// SWE-bench Pro harness only writes verdicts collectively at eval exit
	// (docs/swe-bench-pro.md: eval_results.json = {instance_id: bool}). The
	// per-instance <id>/*_output.json is raw test output with an undocumented
	// schema — deriving pass/fail from it would risk fabricating a wrong verdict.
	// So live progress stays the poller's instance count + harness lines, and the
	// real ✓/✗ breakdown lands here in one batch, keeping the terminal feed
	// snapshot (Pass/Fail/Done) consistent with the persisted run (BR11).
	for i, pr := range problems {
		send(progress, Progress{
			Index: i + 1, Total: len(problems),
			ProblemID: pr.ProblemID, ProblemName: pr.ProblemName,
			Phase: "item_done", Outcome: outcomeOf(pr), Score: pr.Score, ItemMs: pr.TotalMs, Detail: pr.Err,
		})
	}

	var transcripts []ProblemTranscript
	if cfg.SaveTranscripts {
		transcripts = append(transcripts, ProblemTranscript{
			ProblemID:     "swe-bench-pro",
			ProblemName:   "SWE-bench Pro harness output",
			ModelResponse: hlog.transcript(),
		})
	}

	if ctx.Err() != nil {
		return problems, transcripts, ctx.Err()
	}
	return problems, transcripts, nil
}

// runStep runs one harness subprocess (agent / gather / eval) with CWD = dir and
// the LiteLLM-friendly child env (dummy OPENAI_API_KEY + PYTHONUNBUFFERED). It
// owns a process group so a cancel/timeout group-kills the child and its
// descendants; WaitDelay forces the kill if the signal is ignored. Output is
// appended to out for diagnostics.
func (sweBenchProHandler) runStep(ctx context.Context, name, dir, bin string, args []string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = tbEnv(os.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 10 * time.Second
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("swe-bench-pro %s step failed: %w", name, err)
	}
	return nil
}

// --- path / default resolution ---------------------------------------------

func sweapEvalScript(harness string) string { return filepath.Join(harness, sweapEvalScriptName) }
func sweapGatherScript(harness string) string {
	return filepath.Join(harness, filepath.FromSlash(sweapGatherScriptRel))
}

// sweapResolve joins harness-relative paths for SWE-bench Pro config fields.
func sweapResolve(harness, p string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(harness, p)
}

func sweapExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func sweapScriptsDir(cfg Config) string {
	if cfg.SweBenchProScriptsDir != "" {
		return cfg.SweBenchProScriptsDir
	}
	return filepath.Join(cfg.SweBenchProHarnessDir, sweapScriptsDirName)
}

func sweapPython(cfg Config) string {
	if cfg.SweBenchProPython != "" {
		return cfg.SweBenchProPython
	}
	return sweapDefaultPython
}

func sweapDockerhubUser(cfg Config) string {
	if cfg.SweBenchProDockerhubUser != "" {
		return cfg.SweBenchProDockerhubUser
	}
	return sweapDefaultDockerhubUser
}

func sweapNumWorkers(cfg Config) int {
	if cfg.SweBenchProNumWorkers > 0 {
		return cfg.SweBenchProNumWorkers
	}
	return sweapDefaultNumWorkers
}

// --- argv builders ----------------------------------------------------------

// buildSWEAPEvalArgs assembles the argv (after the python interpreter) for
// swe_bench_pro_eval.py. Local Docker is the default backend (the rig is a single
// workstation); Modal is opt-in via cfg.SweBenchProUseModal.
func buildSWEAPEvalArgs(cfg Config, evalScript, rawSample, patchPath, outputDir, scriptsDir string) []string {
	args := []string{
		evalScript,
		"--raw_sample_path", rawSample,
		"--patch_path", patchPath,
		"--output_dir", outputDir,
		"--scripts_dir", scriptsDir,
		"--dockerhub_username", sweapDockerhubUser(cfg),
		"--num_workers", strconv.Itoa(sweapNumWorkers(cfg)),
	}
	if !cfg.SweBenchProUseModal {
		args = append(args, "--use_local_docker")
	}
	return append(args, cfg.SweBenchProExtraArgs...)
}

// buildSWEAPGatherArgs assembles the argv (after the python interpreter) for
// gather_patches.py.
func buildSWEAPGatherArgs(gatherScript, directory, prefix, output string) []string {
	return []string{
		gatherScript,
		"--directory", directory,
		"--prefix", prefix,
		"--output", output,
	}
}

// substituteAgentArgs replaces the {model}/{api_base}/{output}/{instances}/
// {harness} placeholders in each agent-command token.
func substituteAgentArgs(tmpl []string, subs map[string]string) []string {
	out := make([]string, len(tmpl))
	for i, a := range tmpl {
		for k, v := range subs {
			a = strings.ReplaceAll(a, k, v)
		}
		out[i] = a
	}
	return out
}

// --- results parsing --------------------------------------------------------

// parseSWEAPResults decodes eval_results.json, a flat {instance_id: bool}.
func parseSWEAPResults(data []byte) (map[string]bool, error) {
	var res map[string]bool
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("parse swe-bench-pro eval_results.json: %w", err)
	}
	return res, nil
}

// sweapResultsToProblems maps the eval verdicts onto ProblemResults, sorted by
// instance id for deterministic output.
func sweapResultsToProblems(res map[string]bool) []ProblemResult {
	ids := make([]string, 0, len(res))
	for id := range res {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ProblemResult, 0, len(ids))
	for _, id := range ids {
		resolved := res[id]
		pr := ProblemResult{ProblemID: id, ProblemName: id, Resolved: resolved}
		if resolved {
			pr.Score = 1
			pr.Detail = "resolved"
		} else {
			pr.Detail = "unresolved"
		}
		out = append(out, pr)
	}
	return out
}

// --- patch subset filtering -------------------------------------------------

// filterSWEAPPatches reads a patches JSON, keeps only the entries whose
// instance_id is in keep, writes them to dst, and returns the kept count. Entries
// are preserved as raw objects so every field survives — notably the eval's
// preferred `model_patch` key and any extra keys an external predictions file
// carries; round-tripping through a fixed struct would silently drop them.
func filterSWEAPPatches(src string, keep []string, dst string) (int, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return 0, fmt.Errorf("read patches %s: %w", src, err)
	}
	var all []map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return 0, fmt.Errorf("parse patches %s: %w", src, err)
	}
	want := make(map[string]bool, len(keep))
	for _, k := range keep {
		want[k] = true
	}
	kept := make([]map[string]json.RawMessage, 0, len(all))
	for _, p := range all {
		var id string
		if raw, ok := p["instance_id"]; ok {
			_ = json.Unmarshal(raw, &id)
		}
		if want[id] {
			kept = append(kept, p)
		}
	}
	out, err := json.MarshalIndent(kept, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(dst, out, 0o644); err != nil {
		return 0, fmt.Errorf("write filtered patches %s: %w", dst, err)
	}
	return len(kept), nil
}

// SampleSWEAPInstances deterministically samples n instance_ids from the
// SWE-bench Pro raw sample (CSV or JSONL; relative paths resolve against
// harnessDir, matching the harness working directory). It backs the CLI's
// --limit knob for --mode swe-bench-pro: the eval script has no count flag, so
// the reduced run is expressed as an explicit instance filter.
func SampleSWEAPInstances(harnessDir, rawSamplePath string, n, seed int) ([]string, error) {
	if rawSamplePath == "" {
		return nil, fmt.Errorf("benchmark.swebenchpro.raw_sample_path is not configured (see docs/swe-bench-pro.md)")
	}
	p := sweapResolve(harnessDir, rawSamplePath)
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("open raw sample %s: %w", p, err)
	}
	defer f.Close()

	var ids []string
	if strings.EqualFold(filepath.Ext(p), ".csv") {
		ids, err = sweapReadCSVInstanceIDs(f, p)
	} else {
		ids, err = sweapReadJSONLInstanceIDs(f)
	}
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(ids))
	uniq := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return nil, fmt.Errorf("raw sample %s contains no instances", p)
	}
	return sampleIDs(uniq, n, int64(seed)), nil
}

// sweapReadCSVInstanceIDs collects the instance_id column from a CSV raw sample.
// Ragged rows are tolerated; rows shorter than the column index are skipped.
func sweapReadCSVInstanceIDs(f *os.File, path string) ([]string, error) {
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read raw sample %s: %w", path, err)
	}
	col := -1
	for i, h := range header {
		if h == "instance_id" {
			col = i
			break
		}
	}
	if col < 0 {
		return nil, fmt.Errorf("raw sample %s has no instance_id column", path)
	}
	var ids []string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read raw sample %s: %w", path, err)
		}
		if col >= len(rec) {
			continue
		}
		if id := strings.TrimSpace(rec[col]); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// sweapReadJSONLInstanceIDs collects instance_id from a JSONL raw sample. It
// reads with a buffered reader (not bufio.Scanner): raw-sample lines embed
// patches/test lists that overflow the 64KB scanner token limit. A parse
// failure or a parsed line missing instance_id fails loud with its line number
// — a silently-garbled raw sample is this mode's known failure trap.
func sweapReadJSONLInstanceIDs(f *os.File) ([]string, error) {
	br := bufio.NewReader(f)
	var ids []string
	for line := 1; ; line++ {
		s, rerr := br.ReadString('\n')
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			var rec struct {
				InstanceID string `json:"instance_id"`
			}
			if err := json.Unmarshal([]byte(trimmed), &rec); err != nil {
				return nil, fmt.Errorf("line %d: parse raw sample: %w", line, err)
			}
			if rec.InstanceID == "" {
				return nil, fmt.Errorf("line %d: missing instance_id", line)
			}
			ids = append(ids, rec.InstanceID)
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return nil, fmt.Errorf("read raw sample: %w", rerr)
		}
	}
	return ids, nil
}

// --- progress ---------------------------------------------------------------

// countSWEAPOutputs counts completed per-instance results below dir: files named
// "*_output.json" (the eval writes <dir>/<instance_id>/<prefix>_output.json on
// success). Layout-agnostic; returns 0 for a missing dir.
func countSWEAPOutputs(dir string) int {
	count := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), "_output.json") {
			count++
		}
		return nil
	})
	return count
}

// sweapProgressPoller emits a coarse infer-progress event each time the number of
// completed instances changes, so the UI doesn't freeze during a long eval. A nil
// channel disables it.
func sweapProgressPoller(evalOut string, total int, progress chan<- Progress, stop <-chan struct{}) {
	if progress == nil {
		return
	}
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	last := -1
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if n := countSWEAPOutputs(evalOut); n != last {
				last = n
				send(progress, Progress{Index: n, Total: total, ProblemID: "swe-bench-pro", ProblemName: sweapProgressLabel(n, total), Phase: "infer"})
			}
		}
	}
}

func sweapProgressLabel(done, total int) string {
	if total > 0 {
		return fmt.Sprintf("SWE-bench Pro: %d/%d instances evaluated", done, total)
	}
	return fmt.Sprintf("SWE-bench Pro: %d instances evaluated", done)
}

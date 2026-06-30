package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DeepSWE mode wraps the external `pier` harness (github.com/datacurve-ai/pier)
// running the DeepSWE task corpus (github.com/datacurve-ai/deep-swe). Like
// terminal-bench and swe-bench-pro — and unlike the single-turn modes — it is an
// agentic, Docker-sandboxed loop owned by a Python CLI, so we shell out to
// `pier run`. Pier installs the mini-swe-agent INSIDE each task's container and
// points it at our proxy; the agent commits a patch and Pier's program-based
// verifier scores the task pass/fail in a pristine container, writing a
// per-trial result.json. We parse those back into ProblemResults. The
// model-loader's job reduces to what it does well: serving the model (the proxy)
// and recording the score (benchmarkstore).
//
// Routing note: mini-swe-agent maps an `openai/<id>` model name onto its
// Responses-API adapter (litellm_response), which llama-server does not
// implement. We force LiteLLM chat completions with `--agent-kwarg
// model_class=litellm` and hand the agent `OPENAI_API_BASE` + `OPENAI_API_KEY`
// via `--agent-env`. Because the agent runs inside the container, the api_base
// must be host-reachable (see deepAPIBase + docs/deep-swe.md).
const (
	deepDefaultCmd       = "pier"
	deepDefaultAgent     = "mini-swe-agent"
	deepDefaultProvider  = "openai"
	deepDefaultModelClas = "litellm"
	// deepJobName is a fixed pier --job-name so results land at a deterministic
	// path (<jobs-dir>/<deepJobName>/) rather than pier's timestamp default.
	deepJobName = "model-loader"
	// deepTaskManifest marks a DeepSWE/Harbor task directory (one per task).
	deepTaskManifest = "task.toml"
)

// lookPier is a seam resolving the external `pier` binary on PATH (overridable
// in tests). The engine never installs it.
var lookPier = func() (string, error) { return exec.LookPath(deepDefaultCmd) }

func init() { registerHandler(deepSWEHandler{}) }

type deepSWEHandler struct{}

func (deepSWEHandler) Mode() Mode         { return ModeDeepSWE }
func (deepSWEHandler) Category() Category { return CatAgentic }

// Count reports how many tasks the run will cover, when known up front: the
// explicit task list wins, else --n-tasks, else the cloned corpus size when the
// whole corpus is selected, else 0.
func (deepSWEHandler) Count(r *Runner) int {
	if n := len(r.cfg.DeepSWETasks); n > 0 {
		return n
	}
	if r.cfg.DeepSWENTasks > 0 {
		return r.cfg.DeepSWENTasks
	}
	return deepCachedTaskCount(r.cfg.DeepSWETasksDir)
}

// Prepare fails fast (before the expensive proxy load) when the external tool
// chain is missing or misconfigured: the engine wraps pier, it does not install
// it.
func (deepSWEHandler) Prepare(r *Runner) (Scorer, error) {
	if _, err := resolveDeepSWEBin(r.cfg); err != nil {
		return nil, fmt.Errorf("deep-swe mode needs the `pier` CLI on PATH (install: `uv tool install datacurve-pier`): %w", err)
	}
	if _, err := lookDocker(); err != nil {
		return nil, fmt.Errorf("deep-swe mode needs Docker on PATH and running: %w", err)
	}
	dir := r.cfg.DeepSWETasksDir
	if dir == "" {
		return nil, fmt.Errorf("deep-swe mode needs a cloned task corpus: set benchmark.deepswe.tasks_dir (git clone https://github.com/datacurve-ai/deep-swe, then point at its tasks/ dir)")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("deep-swe tasks dir %q is not a directory — is it the tasks/ subdir of a cloned deep-swe checkout? %w", dir, err)
	}
	if deepCachedTaskCount(dir) == 0 {
		return nil, fmt.Errorf("deep-swe tasks dir %q contains no tasks (no <task>/%s found)", dir, deepTaskManifest)
	}
	return nil, nil // objective mode: no scorer
}

// Finalize records the resolved-task rate. It mirrors the generic SolveRate
// (which equals pier's pass rate = n_resolved / total, since DeepSWE trials are
// never excluded as Errored) but names it explicitly for the UI.
func (deepSWEHandler) Finalize(agg *Aggregate, problems []ProblemResult) {
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
	agg.DeepSWEAccuracy = float64(resolved) / float64(total)
}

// Execute shells out to `pier run`, streaming coarse progress by polling the job
// directory for completed trials, then parses each trial's result.json. A
// missing/garbled job (no per-trial result.json files) is a hard error that
// surfaces the path and the tail of pier's output so the operator can diagnose
// the external run.
func (h deepSWEHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	bin, err := resolveDeepSWEBin(r.cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve pier binary: %w", err)
	}

	jobsDir, err := os.MkdirTemp("", "deepswe-run-*")
	if err != nil {
		return nil, nil, fmt.Errorf("create deep-swe output dir: %w", err)
	}
	defer os.RemoveAll(jobsDir)

	args := buildDeepSWEArgs(r.cfg, deepJobName, deepAPIBase(base, r.cfg.DeepSWEAPIBase), deepAPIKey(), model, jobsDir)

	runCtx := ctx
	if r.cfg.DeepSWETimeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, r.cfg.DeepSWETimeout)
		defer cancel()
	}

	total := h.Count(r)
	send(progress, Progress{Index: 0, Total: total, ProblemID: "deep-swe", ProblemName: "DeepSWE harness (Docker) running…", Phase: "infer"})

	runDir := filepath.Join(jobsDir, deepJobName)
	stopPoll := make(chan struct{})
	go deepProgressPoller(runDir, total, progress, stopPoll)

	cmd := exec.CommandContext(runCtx, bin, args...)
	cmd.Env = tbEnv(os.Environ())
	// Own process group so a cancel/timeout group-kills pier and its children;
	// WaitDelay forces the kill if it ignores the signal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 10 * time.Second
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	close(stopPoll)

	problems, perr := deepParseRunDir(runDir)
	if perr != nil {
		return nil, nil, fmt.Errorf("deep-swe produced no per-trial results under %s (pier exited: %v): %w\n--- pier output (tail) ---\n%s",
			runDir, runErr, perr, tbTail(out.String()))
	}

	var transcripts []ProblemTranscript
	if r.cfg.SaveTranscripts {
		transcripts = append(transcripts, ProblemTranscript{
			ProblemID:     "deep-swe",
			ProblemName:   "pier harness output",
			ModelResponse: out.String(),
		})
	}

	// Propagate cancellation so Run records a partial run with the work done.
	if ctx.Err() != nil {
		return problems, transcripts, ctx.Err()
	}
	return problems, transcripts, nil
}

// resolveDeepSWEBin resolves the pier binary: an explicit path/name from config,
// else the lookPier seam.
func resolveDeepSWEBin(cfg Config) (string, error) {
	if c := cfg.DeepSWECmd; c != "" {
		if strings.ContainsRune(c, os.PathSeparator) {
			return c, nil
		}
		return exec.LookPath(c)
	}
	return lookPier()
}

// buildDeepSWEArgs assembles the argv (after the binary name) for `pier run`,
// applying engine defaults for any unset field. model is the bare profile id;
// the provider prefix is prepended here. The agent is handed the proxy endpoint
// via --agent-env, and forced onto LiteLLM chat completions via
// --agent-kwarg model_class.
func buildDeepSWEArgs(cfg Config, jobName, apiBase, apiKey, model, jobsDir string) []string {
	concurrent := cfg.DeepSWEConcurrent
	if concurrent <= 0 {
		concurrent = 1
	}
	args := []string{
		"run",
		"--path", cfg.DeepSWETasksDir,
		"--agent", tbOrDefault(cfg.DeepSWEAgent, deepDefaultAgent),
		"--model", tbOrDefault(cfg.DeepSWEProvider, deepDefaultProvider) + "/" + model,
		"--agent-kwarg", "model_class=" + tbOrDefault(cfg.DeepSWEModelClass, deepDefaultModelClas),
		"--agent-env", "OPENAI_API_BASE=" + apiBase,
		"--agent-env", "OPENAI_API_KEY=" + apiKey,
		"--jobs-dir", jobsDir,
		"--job-name", jobName,
		"--n-concurrent", strconv.Itoa(concurrent),
		"--yes", // auto-confirm host-env access (headless run; pier would otherwise prompt)
	}
	for _, t := range cfg.DeepSWETasks {
		args = append(args, "--include-task-name", t)
	}
	if cfg.DeepSWENTasks > 0 {
		args = append(args, "--n-tasks", strconv.Itoa(cfg.DeepSWENTasks))
		// Deterministic subset: seed the sampler so repeat runs cover the same
		// tasks (pier applies --sample-seed before --n-tasks).
		args = append(args, "--sample-seed", strconv.Itoa(cfg.DeepSWESampleSeed))
	}
	return append(args, cfg.DeepSWEExtraArgs...)
}

// deepAPIKey returns the api key handed to the in-sandbox agent: a real
// OPENAI_API_KEY from the environment when set non-empty, else a dummy (LiteLLM's
// openai route requires a non-empty key; our proxy ignores it).
func deepAPIKey() string {
	if k := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); k != "" {
		return k
	}
	return "model-loader-proxy"
}

// deepAPIBase derives the api_base the in-sandbox agent calls. An explicit
// override wins. Otherwise it is derived from the proxy root (ensuring a /v1
// suffix) with a loopback host rewritten to host.docker.internal: the agent runs
// INSIDE a Docker container, where 127.0.0.1 is the container itself, so the
// default targets the host. The operator must bind the proxy on a host-reachable
// interface (see docs/deep-swe.md).
func deepAPIBase(proxyRoot, override string) string {
	if override != "" {
		return override
	}
	b := tbAPIBase(proxyRoot)
	for _, lo := range []string{"127.0.0.1", "localhost", "0.0.0.0"} {
		b = strings.Replace(b, "//"+lo+":", "//host.docker.internal:", 1)
		b = strings.Replace(b, "//"+lo+"/", "//host.docker.internal/", 1)
	}
	return b
}

// deepCachedTaskCount counts task directories directly under tasksDir: entries
// that are directories containing a task.toml. Returns 0 for a missing dir.
func deepCachedTaskCount(tasksDir string) int {
	if tasksDir == "" {
		return 0
	}
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(tasksDir, e.Name(), deepTaskManifest)); err == nil {
			count++
		}
	}
	return count
}

// deepTrialResult mirrors the subset of pier's per-trial result.json (a
// serialized TrialResult) that we consume.
type deepTrialResult struct {
	TaskName       string `json:"task_name"`
	TrialName      string `json:"trial_name"`
	VerifierResult *struct {
		Rewards map[string]float64 `json:"rewards"`
	} `json:"verifier_result"`
	ExceptionInfo *struct {
		ExceptionType    string `json:"exception_type"`
		ExceptionMessage string `json:"exception_message"`
	} `json:"exception_info"`
	AgentResult *struct {
		NInputTokens  *int `json:"n_input_tokens"`
		NOutputTokens *int `json:"n_output_tokens"`
	} `json:"agent_result"`
}

// deepParseRunDir walks the per-trial result.json files directly under runDir
// (each a serialized TrialResult), skipping the job-root aggregate result.json,
// and maps them onto ProblemResults sorted by task for deterministic output. It
// errors when no trial result is found at all.
func deepParseRunDir(runDir string) ([]ProblemResult, error) {
	root := filepath.Clean(runDir)
	var trials []deepTrialResult
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		// Per-trial results live one level below the job root; the root
		// result.json is the job aggregate (no task_name) and is skipped.
		if d.Name() != "result.json" || filepath.Dir(path) == root {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		var tr deepTrialResult
		if json.Unmarshal(data, &tr) != nil || tr.TaskName == "" {
			return nil
		}
		trials = append(trials, tr)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk pier job dir: %w", walkErr)
	}
	if len(trials) == 0 {
		return nil, fmt.Errorf("no per-trial result.json found")
	}
	return deepTrialsToProblems(trials), nil
}

// deepTrialsToProblems maps pier trials onto ProblemResults. A trial is resolved
// iff its verifier reward is >= 1 (pier's binary 0/1 pass flag; a reward.txt
// crash sentinel of -1 or a 0 both count as unresolved). A trial that crashed
// before verification (exception_info set, no verifier_result) is an unresolved
// fail rather than an excluded Errored slot, keeping SolveRate aligned with
// pier's pass rate (n_resolved/total). Output is sorted by task id.
func deepTrialsToProblems(trials []deepTrialResult) []ProblemResult {
	out := make([]ProblemResult, 0, len(trials))
	for _, tr := range trials {
		pr := ProblemResult{ProblemID: tr.TaskName, ProblemName: tr.TaskName}
		reward, hasReward := deepReward(tr)
		pr.Resolved = hasReward && reward >= 1
		if pr.Resolved {
			pr.Score = 1
		}
		if tr.AgentResult != nil {
			if tr.AgentResult.NInputTokens != nil {
				pr.PromptTokens = *tr.AgentResult.NInputTokens
			}
			if tr.AgentResult.NOutputTokens != nil {
				pr.CompletionTokens = *tr.AgentResult.NOutputTokens
			}
		}
		pr.Detail = deepDetail(reward, hasReward, tr)
		out = append(out, pr)
	}
	sortProblemsByID(out)
	return out
}

// deepReward extracts the binary "reward" value from a trial's verifier result.
// The bool is false when no verifier reward was recorded (e.g. the trial crashed
// with an exception before verification).
func deepReward(tr deepTrialResult) (float64, bool) {
	if tr.VerifierResult == nil || tr.VerifierResult.Rewards == nil {
		return 0, false
	}
	v, ok := tr.VerifierResult.Rewards["reward"]
	return v, ok
}

func deepDetail(reward float64, hasReward bool, tr deepTrialResult) string {
	if hasReward && reward >= 1 {
		return "resolved"
	}
	if tr.ExceptionInfo != nil && tr.ExceptionInfo.ExceptionType != "" {
		return tr.ExceptionInfo.ExceptionType
	}
	if !hasReward {
		return "incomplete"
	}
	if reward < 0 {
		return "verifier-error" // reward.txt crash sentinel (-1)
	}
	return "unresolved"
}

// sortProblemsByID sorts problems by ProblemID in place (stable, ascending) for
// deterministic output.
func sortProblemsByID(ps []ProblemResult) {
	for i := 1; i < len(ps); i++ {
		for j := i; j > 0 && ps[j-1].ProblemID > ps[j].ProblemID; j-- {
			ps[j-1], ps[j] = ps[j], ps[j-1]
		}
	}
}

// deepCountCompletedTrials counts per-trial result.json files directly below
// runDir, excluding the job-root aggregate result.json. Returns 0 for a missing
// dir.
func deepCountCompletedTrials(runDir string) int {
	root := filepath.Clean(runDir)
	count := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if d.Name() == "result.json" && filepath.Dir(path) != root {
			count++
		}
		return nil
	})
	return count
}

// deepProgressPoller emits a coarse infer-progress event each time the number of
// completed trials changes, so the UI doesn't freeze during a multi-hour run. A
// nil channel disables it.
func deepProgressPoller(runDir string, total int, progress chan<- Progress, stop <-chan struct{}) {
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
			n := deepCountCompletedTrials(runDir)
			if n != last {
				last = n
				send(progress, Progress{Index: n, Total: total, ProblemID: "deep-swe", ProblemName: deepProgressLabel(n, total), Phase: "infer"})
			}
		}
	}
}

func deepProgressLabel(done, total int) string {
	if total > 0 {
		return fmt.Sprintf("DeepSWE: %d/%d tasks complete", done, total)
	}
	return fmt.Sprintf("DeepSWE: %d tasks complete", done)
}

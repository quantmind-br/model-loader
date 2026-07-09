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
	"sync/atomic"
	"syscall"
	"time"
)

// Terminal-Bench mode wraps the external `tb` harness (github.com/laude-institute
// /terminal-bench). Unlike every other mode — single-turn prompt→model→score in
// Go over the proxy — Terminal-Bench is an agentic, Docker-sandboxed, multi-turn
// loop owned by a Python CLI. We therefore shell out to `tb run`, point its
// Terminus agent at our OpenAI-shaped proxy (LiteLLM `openai/<profile-id>` +
// `api_base`), and parse the run's results.json back into ProblemResults. The
// model-loader's job reduces to what it does well: serving the model (the proxy)
// and recording the score (benchmarkstore).
const (
	tbDefaultCmd      = "tb"
	tbDefaultAgent    = "terminus"
	tbDefaultDataset  = "terminal-bench-core==0.1.1"
	tbDefaultProvider = "openai"
	// tbRunID is a fixed run id so results land at a deterministic path
	// (<output-path>/<tbRunID>/results.json) rather than tb's timestamp default.
	tbRunID = "model-loader"
	// tbCacheDatasetRoot is the dataset segment under os.UserCacheDir() (tb default
	// download layout: <UserCacheDir>/terminal-bench/<name>/<version>).
	tbCacheDatasetRoot = "terminal-bench"
)

// lookTB / lookDocker are seams resolving the external binaries on PATH
// (overridable in tests). The engine never installs them.
var (
	lookTB     = func() (string, error) { return exec.LookPath(tbDefaultCmd) }
	lookDocker = func() (string, error) { return exec.LookPath("docker") }
)

func init() { registerHandler(terminalBenchHandler{}) }

type terminalBenchHandler struct{}

func (terminalBenchHandler) Mode() Mode         { return ModeTerminalBench }
func (terminalBenchHandler) Category() Category { return CatAgentic }

// Count reports how many tasks the run will cover, when known up front: the
// explicit task list wins, else --n-tasks, else the cached tb dataset size when
// the whole dataset is selected (no tasks / n_tasks), else 0.
func (terminalBenchHandler) Count(r *Runner) int {
	if n := len(r.cfg.TerminalBenchTasks); n > 0 {
		return n
	}
	if r.cfg.TerminalBenchNTasks > 0 {
		return r.cfg.TerminalBenchNTasks
	}
	return tbCachedDatasetTaskCount(r.cfg.TerminalBenchDataset)
}

// Prepare fails fast (before the expensive proxy load) when the external tool
// chain is missing: the engine wraps tb, it does not install it.
func (terminalBenchHandler) Prepare(r *Runner) (Scorer, error) {
	if _, err := resolveTBBin(r.cfg); err != nil {
		return nil, fmt.Errorf("terminal-bench mode needs the `tb` CLI on PATH (install: `uv tool install terminal-bench`): %w", err)
	}
	if _, err := lookDocker(); err != nil {
		return nil, fmt.Errorf("terminal-bench mode needs Docker on PATH and running: %w", err)
	}
	return nil, nil // objective mode: no scorer
}

// Finalize records the resolved-task rate. It mirrors the generic SolveRate
// (which equals tb's accuracy = n_resolved / total, since terminal-bench trials
// are never excluded as Errored) but names it explicitly for the UI.
func (terminalBenchHandler) Finalize(agg *Aggregate, problems []ProblemResult) {
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
	agg.TerminalBenchAccuracy = float64(resolved) / float64(total)
}

// Execute shells out to `tb run`, streaming coarse progress by polling the run
// directory for completed trials, then parses results.json. A missing/garbled
// results.json is a hard error that surfaces the path and the tail of tb's
// output so the operator can diagnose the external run.
func (h terminalBenchHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	bin, err := resolveTBBin(r.cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve tb binary: %w", err)
	}

	outDir, err := os.MkdirTemp("", "tb-run-*")
	if err != nil {
		return nil, nil, fmt.Errorf("create tb output dir: %w", err)
	}
	defer os.RemoveAll(outDir)

	args := buildTBArgs(r.cfg, tbRunID, tbAPIBase(base), model, outDir)

	// runCtx bounds the run: the optional whole-run timeout, plus a cancel the
	// hang watchdog trips to group-kill a wedged harness.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if r.cfg.TerminalBenchTimeout > 0 {
		var tcancel context.CancelFunc
		runCtx, tcancel = context.WithTimeout(runCtx, r.cfg.TerminalBenchTimeout)
		defer tcancel()
	}

	total := h.Count(r)
	send(progress, Progress{Index: 0, Total: total, ProblemID: "terminal-bench", ProblemName: "Terminal-Bench harness (Docker) running…", Phase: "infer"})

	runDir := filepath.Join(outDir, tbRunID)
	stopPoll := make(chan struct{})
	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		tbProgressPoller(runDir, total, progress, stopPoll)
	}()

	cmd := exec.CommandContext(runCtx, bin, args...)
	cmd.Env = tbEnv(os.Environ())
	// Own process group so a cancel/timeout/watchdog group-kills tb and its
	// children; WaitDelay forces the kill if it ignores the signal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 10 * time.Second
	hlog := newHarnessLog(r.cfg.HarnessLogDir, "terminal-bench", r.harnessLineEmitter(ModeTerminalBench))
	defer hlog.Close()
	cmd.Stdout = hlog
	cmd.Stderr = hlog

	if err := cmd.Start(); err != nil {
		close(stopPoll)
		<-pollDone
		return nil, nil, fmt.Errorf("start tb: %w", err)
	}
	r.logger().Info("benchmark_harness_start", "run_id", r.runID, "mode", ModeTerminalBench, "bin", bin, "total", total, "log", hlog.path)

	// Watchdog: a tb that finished every task but won't exit, or stalled
	// mid-run, keeps hammering the proxy for hours and pins the GPU
	// (BUGS UIUX-012). It records why it fired, SIGTERMs the tb process group
	// (a chance for --cleanup), then escalates to the run-context SIGKILL.
	var reason atomic.Int32
	wdStop := make(chan struct{})
	wdDone := make(chan struct{})
	go func() {
		defer close(wdDone)
		tbHangWatchdog(r.cfg, runDir, total, cmd.Process.Pid, cancel, progress, &reason, wdStop)
	}()

	runErr := cmd.Wait()
	r.logger().Info("benchmark_harness_exit", "run_id", r.runID, "mode", ModeTerminalBench, "err", runErr, "scored", countCompletedTrials(runDir))
	close(wdStop)
	<-wdDone
	close(stopPoll)
	<-pollDone // join before deferred os.RemoveAll(outDir) races the poller (audit N-C16)

	resultsPath := filepath.Join(runDir, "results.json")
	data, readErr := os.ReadFile(resultsPath)
	if readErr != nil {
		return nil, nil, fmt.Errorf("terminal-bench produced no results at %s (tb exited: %v)\n--- tb output (tail) ---\n%s",
			resultsPath, runErr, hlog.diagTail())
	}
	res, perr := parseTBResults(data)
	if perr != nil {
		return nil, nil, fmt.Errorf("%w\n--- tb output (tail) ---\n%s", perr, hlog.diagTail())
	}
	problems := tbResultsToProblems(res)

	var transcripts []ProblemTranscript
	if r.cfg.SaveTranscripts {
		transcripts = append(transcripts, ProblemTranscript{
			ProblemID:     "terminal-bench",
			ProblemName:   "tb harness output",
			ModelResponse: hlog.transcript(),
		})
	}

	switch tbWatchReason(reason.Load()) {
	case tbWatchStalled:
		// A wedge mid-run: some tasks never completed. Surface it as a partial
		// run so the operator sees why, keeping the tasks that did finish.
		return problems, transcripts, fmt.Errorf("terminal-bench stalled: no task scored for %s; harness killed", tbStallTimeout(r.cfg))
	case tbWatchCompleted:
		// Every task was scored before we killed the lingering harness — the
		// results are whole, so this is a normal completion, not a failure.
		return problems, transcripts, nil
	}

	// Operator cancellation → partial run carrying its cause.
	if ctx.Err() != nil {
		return problems, transcripts, ctx.Err()
	}
	// The whole-run TerminalBenchTimeout fired (runCtx done, parent ctx not):
	// the run is truncated even if partial results parsed, so surface it as an
	// error (a partial run, excluded from the leaderboard) — never a clean
	// completion.
	if runCtx.Err() != nil {
		return problems, transcripts, fmt.Errorf("terminal-bench exceeded run timeout %s; harness killed", r.cfg.TerminalBenchTimeout)
	}
	// A non-zero tb exit with an incomplete task set means the harness died
	// mid-run: surface it as a partial run (BR3). Exit-code semantics of a
	// *complete* result set are the harness's business (a completed-but-
	// imperfect run may exit non-zero), so completeness gates the check —
	// mirroring the watchdog's completed-kill-is-success rule. An unknown
	// total can't gate; the exit error is still traced in the harness log.
	if runErr != nil {
		if total > 0 && len(problems) < total {
			return problems, transcripts, fmt.Errorf("tb exited with error after %d/%d tasks: %v\n--- tb output (tail) ---\n%s",
				len(problems), total, runErr, hlog.diagTail())
		}
		fmt.Fprintf(hlog, "\n[model-loader] tb exited non-zero with a complete result set: %v\n", runErr)
	}
	return problems, transcripts, nil
}

type tbWatchReason int32

const (
	tbWatchNone tbWatchReason = iota
	tbWatchCompleted
	tbWatchStalled
)

// Watchdog timing — vars, not consts, so tests can shrink them. Defaults are
// generous so the safety nets only fire on a genuine hang.
var (
	// tbWatchInterval is how often the watchdog samples run progress.
	tbWatchInterval = 5 * time.Second
	// tbPostCompleteGrace is how long a fully-scored tb run may keep running
	// before the watchdog kills it — enough for --cleanup Docker teardown.
	tbPostCompleteGrace = 2 * time.Minute
	// tbDefaultStallTimeout is the built-in no-progress kill threshold used when
	// TerminalBenchStallTimeout is unset. Well above any single task's agent
	// budget, so it only fires on a genuine wedge, not a slow task.
	tbDefaultStallTimeout = 45 * time.Minute
	// tbTermGrace is how long the watchdog waits after SIGTERM before escalating
	// to the run-context SIGKILL.
	tbTermGrace = 20 * time.Second
)

func tbStallTimeout(cfg Config) time.Duration {
	if cfg.TerminalBenchStallTimeout > 0 {
		return cfg.TerminalBenchStallTimeout
	}
	return tbDefaultStallTimeout
}

// runHangWatchdog group-kills an external agentic harness that either scored
// every task but won't exit, or stalled mid-run (no new task scored for the
// stall timeout). Both otherwise leave the harness hammering the proxy for
// hours, pinning the GPU (BUGS UIUX-012). completed returns tasks scored so
// far; total is the expected count (<=0 disables completion detection — the
// stall check still applies). label tags the progress announcement. Shared by
// every agentic mode (terminal-bench, deep-swe).
func runHangWatchdog(label string, completed func() int, total, pid int, stall time.Duration, cancel context.CancelFunc, progress chan<- Progress, reason *atomic.Int32, stop <-chan struct{}) {
	t := time.NewTicker(tbWatchInterval)
	defer t.Stop()
	lastDone := -1
	lastProgress := time.Now()
	completedAt := time.Time{}
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			done := completed()
			if done != lastDone {
				lastDone = done
				lastProgress = now
			}
			if total > 0 && done >= total {
				if completedAt.IsZero() {
					completedAt = now
					send(progress, Progress{Index: total, Total: total, ProblemID: label, ProblemName: "all tasks scored — waiting for harness to exit…", Phase: "infer"})
				}
				if now.Sub(completedAt) >= tbPostCompleteGrace {
					tbKillGroup(pid, cancel, reason, tbWatchCompleted, stop)
					return
				}
				continue
			}
			if now.Sub(lastProgress) >= stall {
				tbKillGroup(pid, cancel, reason, tbWatchStalled, stop)
				return
			}
		}
	}
}

// tbHangWatchdog is the terminal-bench wiring of runHangWatchdog: it counts
// scored trials from the tb run directory (per-trial files plus the aggregate
// results.json) and applies the tb stall timeout.
func tbHangWatchdog(cfg Config, runDir string, total, pid int, cancel context.CancelFunc, progress chan<- Progress, reason *atomic.Int32, stop <-chan struct{}) {
	completed := func() int {
		done := countCompletedTrials(runDir)
		if agg := tbReadAggregateResultsTotal(runDir); agg > done {
			done = agg
		}
		return done
	}
	runHangWatchdog("terminal-bench", completed, total, pid, tbStallTimeout(cfg), cancel, progress, reason, stop)
}

// tbKillGroup records why the watchdog fired, SIGTERMs the tb process group
// (letting --cleanup run), and escalates to the run-context SIGKILL if tb has
// not exited within tbTermGrace.
func tbKillGroup(pid int, cancel context.CancelFunc, reason *atomic.Int32, why tbWatchReason, stop <-chan struct{}) {
	reason.Store(int32(why))
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case <-stop: // tb exited after SIGTERM (cmd.Wait returned → Execute closed stop)
	case <-time.After(tbTermGrace):
		cancel() // escalate: the exec runtime SIGKILLs the group via cmd.Cancel
	}
}

// tbDatasetNameVersion splits tb's --dataset value ('name' or 'name==version').
func tbDatasetNameVersion(dataset string) (name, version string) {
	dataset = strings.TrimSpace(dataset)
	if dataset == "" {
		dataset = tbDefaultDataset
	}
	if i := strings.Index(dataset, "=="); i >= 0 {
		return dataset[:i], dataset[i+2:]
	}
	return dataset, ""
}

// tbCachedDatasetTaskCount counts task directories under tb's default dataset
// cache (~/.cache/terminal-bench/<name>/<version>). Returns 0 when the cache is
// missing (tb will download on first run; progress may show an unknown total
// until aggregate results.json appears).
func tbCachedDatasetTaskCount(dataset string) int {
	name, version := tbDatasetNameVersion(dataset)
	if name == "" || version == "" {
		return 0
	}
	root, err := os.UserCacheDir()
	if err != nil {
		return 0
	}
	dir := filepath.Join(root, tbCacheDatasetRoot, name, version)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	return n
}

// tbReadAggregateResultsTotal reads len(results) from the run's aggregate
// results.json when tb has started writing it (partial runs included).
func tbReadAggregateResultsTotal(runDir string) int {
	data, err := os.ReadFile(filepath.Join(runDir, "results.json"))
	if err != nil {
		return 0
	}
	var partial struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(data, &partial); err != nil {
		return 0
	}
	return len(partial.Results)
}

// resolveTBBin resolves the tb binary: an explicit path/name from config, else
// the lookTB seam.
func resolveTBBin(cfg Config) (string, error) {
	if c := cfg.TerminalBenchCmd; c != "" {
		if strings.ContainsRune(c, os.PathSeparator) {
			return c, nil
		}
		return exec.LookPath(c)
	}
	return lookTB()
}

// buildTBArgs assembles the argv (after the binary name) for `tb run`, applying
// engine defaults for any unset field. model is the bare profile id; the
// provider prefix is prepended here.
func buildTBArgs(cfg Config, runID, apiBase, model, outDir string) []string {
	concurrent := cfg.TerminalBenchConcurrent
	if concurrent <= 0 {
		concurrent = 1
	}
	args := []string{
		"run",
		"--agent", tbOrDefault(cfg.TerminalBenchAgent, tbDefaultAgent),
		"--model", tbOrDefault(cfg.TerminalBenchProvider, tbDefaultProvider) + "/" + model,
		"--agent-kwarg", "api_base=" + apiBase,
		"--dataset", tbOrDefault(cfg.TerminalBenchDataset, tbDefaultDataset),
		"--output-path", outDir,
		"--run-id", runID,
		"--n-concurrent", strconv.Itoa(concurrent),
		"--cleanup", // remove the run's Docker images afterward (overridable via ExtraArgs)
	}
	for _, t := range cfg.TerminalBenchTasks {
		args = append(args, "--task-id", t)
	}
	if cfg.TerminalBenchNTasks > 0 {
		args = append(args, "--n-tasks", strconv.Itoa(cfg.TerminalBenchNTasks))
	}
	return append(args, cfg.TerminalBenchExtraArgs...)
}

func tbOrDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// tbAPIBase derives the LiteLLM api_base from the proxy root, tolerating a base
// that already carries a /v1 suffix.
func tbAPIBase(base string) string {
	b := strings.TrimRight(base, "/")
	if strings.HasSuffix(b, "/v1") {
		return b
	}
	return b + "/v1"
}

// tbEnv builds the child environment: the parent env plus PYTHONUNBUFFERED=1
// (live log flushing) and a dummy OPENAI_API_KEY when none is set (LiteLLM's
// openai route requires a non-empty key; our proxy ignores it). A real
// OPENAI_API_KEY is preserved untouched.
func tbEnv(parent []string) []string {
	env := make([]string, 0, len(parent)+2)
	hasKey := false
	for _, e := range parent {
		if strings.HasPrefix(e, "OPENAI_API_KEY=") {
			// A set-but-empty key (OPENAI_API_KEY=) makes LiteLLM's openai route
			// fail with "Missing credentials" before any request leaves the
			// agent. Treat empty as absent and drop it so the dummy below wins.
			if strings.TrimPrefix(e, "OPENAI_API_KEY=") == "" {
				continue
			}
			hasKey = true
		}
		if strings.HasPrefix(e, "PYTHONUNBUFFERED=") {
			continue // normalized to =1 below
		}
		env = append(env, e)
	}
	env = append(env, "PYTHONUNBUFFERED=1")
	if !hasKey {
		env = append(env, "OPENAI_API_KEY=model-loader-proxy")
	}
	return env
}

// tbBenchmarkResults mirrors the subset of tb's results.json (a serialized
// BenchmarkResults) that we consume.
type tbBenchmarkResults struct {
	Results   []tbTrialResult `json:"results"`
	Accuracy  float64         `json:"accuracy"`
	NResolved int             `json:"n_resolved"`
}

// tbTrialResult mirrors one TrialResults entry.
type tbTrialResult struct {
	TaskID            string `json:"task_id"`
	TrialName         string `json:"trial_name"`
	IsResolved        *bool  `json:"is_resolved"`
	FailureMode       string `json:"failure_mode"`
	TotalInputTokens  *int   `json:"total_input_tokens"`
	TotalOutputTokens *int   `json:"total_output_tokens"`
}

func parseTBResults(data []byte) (tbBenchmarkResults, error) {
	var res tbBenchmarkResults
	if err := json.Unmarshal(data, &res); err != nil {
		return tbBenchmarkResults{}, fmt.Errorf("parse terminal-bench results.json: %w", err)
	}
	return res, nil
}

// tbResultsToProblems maps tb trials onto ProblemResults. is_resolved==null (an
// incomplete/failed trial) counts as an unresolved fail rather than an excluded
// Errored slot, keeping SolveRate aligned with tb's accuracy (n_resolved/total).
func tbResultsToProblems(res tbBenchmarkResults) []ProblemResult {
	out := make([]ProblemResult, 0, len(res.Results))
	for _, tr := range res.Results {
		resolved := tr.IsResolved != nil && *tr.IsResolved
		pr := ProblemResult{ProblemID: tr.TaskID, ProblemName: tr.TaskID, Resolved: resolved}
		if resolved {
			pr.Score = 1
		}
		if tr.TotalInputTokens != nil {
			pr.PromptTokens = *tr.TotalInputTokens
		}
		if tr.TotalOutputTokens != nil {
			pr.CompletionTokens = *tr.TotalOutputTokens
		}
		pr.Detail = tbDetail(resolved, tr)
		out = append(out, pr)
	}
	return out
}

func tbDetail(resolved bool, tr tbTrialResult) string {
	if resolved {
		return "resolved"
	}
	if fm := tr.FailureMode; fm != "" && fm != "unset" {
		return fm
	}
	if tr.IsResolved == nil {
		return "incomplete"
	}
	return "unresolved"
}

// countCompletedTrials counts per-trial results.json files below runDir,
// excluding the aggregate results.json at the run root. Layout-agnostic: it
// works whatever the per-trial nesting is, and returns 0 for a missing dir.
func countCompletedTrials(runDir string) int {
	root := filepath.Clean(runDir)
	count := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if d.Name() == "results.json" && filepath.Dir(path) != root {
			count++
		}
		return nil
	})
	return count
}

// tbProgressPoller emits a coarse infer-progress event each time the number of
// completed trials changes, so the UI doesn't freeze during a multi-hour run. A
// nil channel disables it.
func tbProgressPoller(runDir string, total int, progress chan<- Progress, stop <-chan struct{}) {
	if progress == nil {
		return
	}
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	last := -1
	lastTotal := -1
	seen := map[string]bool{}
	for {
		select {
		case <-stop:
			emitNewTBTrials(runDir, seen, progress) // final scan: catch trials finished in the last <=3s window (BR14)
			return
		case <-t.C:
			emitNewTBTrials(runDir, seen, progress)
			// Match the watchdog's completed clock — max(per-trial files, aggregate
			// results.json) — so an Index bump (hence LastItemDone and the UI stall
			// countdown) advances on exactly the progress the kill clock counts, never
			// showing an imminent kill the watchdog will not perform (BR15).
			n := countCompletedTrials(runDir)
			effectiveTotal := total
			if agg := tbReadAggregateResultsTotal(runDir); agg > 0 {
				if agg > n {
					n = agg
				}
				if effectiveTotal <= 0 {
					effectiveTotal = agg
				}
			}
			if n != last || effectiveTotal != lastTotal {
				last = n
				lastTotal = effectiveTotal
				send(progress, Progress{Index: n, Total: effectiveTotal, ProblemID: "terminal-bench", ProblemName: tbProgressLabel(n, effectiveTotal), Phase: "infer"})
			}
		}
	}
}

// emitNewTBTrials scans runDir for per-trial results.json files not yet in seen,
// decodes each as a single tbTrialResult, and emits an item_done for the trial's
// pass/fail outcome. A path is marked seen only after a successful parse so a
// mid-write file is retried on a later tick.
func emitNewTBTrials(runDir string, seen map[string]bool, progress chan<- Progress) {
	root := filepath.Clean(runDir)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if d.Name() != "results.json" || filepath.Dir(path) == root || seen[path] {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		var tr tbTrialResult
		if json.Unmarshal(data, &tr) != nil || tr.TaskID == "" {
			return nil
		}
		seen[path] = true
		name := tr.TrialName
		if name == "" {
			name = tr.TaskID
		}
		outcome := "fail"
		if tr.IsResolved != nil && *tr.IsResolved {
			outcome = "pass"
		}
		send(progress, Progress{ProblemID: tr.TaskID, ProblemName: name, Phase: "item_done", Outcome: outcome, Detail: tbDetail(outcome == "pass", tr)})
		return nil
	})
}

func tbProgressLabel(done, total int) string {
	if total > 0 {
		return fmt.Sprintf("Terminal-Bench: %d/%d tasks complete", done, total)
	}
	return fmt.Sprintf("Terminal-Bench: %d tasks complete", done)
}

func tbTail(s string) string {
	const max = 4000
	if len(s) > max {
		return "…" + s[len(s)-max:]
	}
	return s
}

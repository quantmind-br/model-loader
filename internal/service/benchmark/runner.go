package benchmark

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

// Failure-phase labels for ProblemResult.FailPhase (T5): they make a per-item
// failure attributable — inference (model under test) vs scoring (judge/grader)
// vs an external harness — without parsing the free-text Err.
const (
	phaseInfer   = "infer"
	phaseScore   = "score"
	phaseHarness = "harness"
)

// decodeFloorTPS is the conservative decode floor used to scale a per-item
// inference deadline by MaxTokens (T3): a fixed Timeout cannot cover a large
// max_tokens budget on a slow local model (20–60 tok/s), so a reasoning model
// that spends its budget spuriously times out. 15 tok/s leaves generous head
// room; the deadline only fires on a genuinely stuck/runaway generation.
const decodeFloorTPS = 15

// checkpointInterval throttles incremental run persistence (T7).
const checkpointInterval = 15 * time.Second

// ProxyController is how the runner reaches backends: ensure the proxy is up,
// swap the profile in, and address all inference at the proxy. The instance
// port never leaks into this package.
type ProxyController interface {
	EnsureRunning(context.Context) error
	Load(ctx context.Context, profileID string) (httpproxy.Status, error)
	Unload(ctx context.Context, force bool) (httpproxy.Status, error)
	Status() httpproxy.Status
	BaseURL() string
}

// RunConfig is the per-invocation request from the UI.
type RunConfig struct {
	ProfileID string
	Mode      Mode
	// Checkpoint, when set, persists the partial run periodically during
	// execution so a model-loader crash mid-run does not lose everything (T7).
	// The engine flags each checkpoint with Err="in progress" so it is excluded
	// from the leaderboard until the caller's final save overwrites it.
	Checkpoint func(Run)
}

// Progress is streamed to the UI while a run executes. The first five fields are
// the original wire contract; the rest are additive (item_done / activity
// events) and are stamped/consumed by RunFeed.
type Progress struct {
	Index       int
	Total       int
	ProblemID   string
	ProblemName string
	Phase       string    // "launch" | "infer" | "score" | "item_done" | "activity" | "done"
	At          time.Time // stamped by the feed when zero
	Detail      string    // human activity line (harness output line, "streaming — N tok")
	Outcome     string    // item_done only: "pass" | "fail" | "error"
	Score       float64   // item_done only
	ItemMs      int64     // item_done only: item wall time
}

// Runner orchestrates a full benchmark run against one profile.
type Runner struct {
	store           profilestore.Store
	mon             monitor.Manager
	proxy           ProxyController
	cfg             Config
	problems        []Problem             // embedded SWE-bench Lite coding set
	mathProblems    []MathProblem         // embedded GSM8K set for ModeMathBench
	codeGenProblems []CodeGenProblem      // embedded HumanEval set for ModeCodeGenBench
	instProblems    []InstructionProblem  // embedded instruction-robustness set for ModeInstBench
	mmluProblems    []MMLUProblem         // embedded MMLU subset for ModeMMLUBench
	arxivDocs       []ArxivDoc            // real arXiv abstracts used as long-context quality filler
	ragasProblems   []RagasProblem        // synthetic RAG scenarios for ModeRagasBench
	summaryProblems []SummaryProblem      // synthetic multi-doc bundles for ModeSummaryBench
	presets         []tpPreset            // parsed ModeLlamaBench configs
	reps            int                   // ModeLlamaBench repetitions per preset
	warmup          int                   // discarded warmup reps before measurement
	runCtxTokens    int                   // effective context of the profile under test; set by Run before Execute (single-flight)
	log             *slog.Logger          // structured diagnostics (T1); never nil (log.Nop fallback)
	runID           string                // current run id for log correlation; set by Run (single-flight)
	checkpoint      func([]ProblemResult) // incremental persistence hook (T7); set by Run, nil when disabled
	lastCheckpoint  time.Time             // throttle for checkpoint
	feedMu          sync.Mutex            // guards feed
	feed            *RunFeed              // authoritative live-run model; nil before the first run, retained after
}

// NewRunner builds a Runner and loads the embedded dataset.
func NewRunner(store profilestore.Store, mon monitor.Manager, proxy ProxyController, cfg Config) (*Runner, error) {
	problems, err := Load()
	if err != nil {
		return nil, err
	}
	mathProblems, err := loadMathProblems()
	if err != nil {
		return nil, err
	}
	codeGenProblems, err := loadCodeGenProblems()
	if err != nil {
		return nil, err
	}
	instProblems, err := loadInstructionProblems()
	if err != nil {
		return nil, err
	}
	mmluProblems, err := loadMMLUProblems()
	if err != nil {
		return nil, err
	}
	arxivDocs, err := loadArxivDocs()
	if err != nil {
		return nil, err
	}
	ragasProblems, err := loadRagasProblems()
	if err != nil {
		return nil, err
	}
	summaryProblems, err := loadSummaryProblems()
	if err != nil {
		return nil, err
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 1024
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 120 * time.Second
	}
	presets, err := parsePresets(cfg.LlamaBenchPresets)
	if err != nil {
		return nil, err
	}
	reps := cfg.LlamaBenchReps
	if reps <= 0 {
		reps = 3
	}
	warmup := cfg.LlamaBenchWarmup
	if warmup < 0 {
		warmup = 1
	}
	lg := cfg.Logger
	if lg == nil {
		lg = log.Nop()
	}
	return &Runner{store: store, mon: mon, proxy: proxy, cfg: cfg, problems: problems, mathProblems: mathProblems, codeGenProblems: codeGenProblems, instProblems: instProblems, mmluProblems: mmluProblems, arxivDocs: arxivDocs, ragasProblems: ragasProblems, summaryProblems: summaryProblems, presets: presets, reps: reps, warmup: warmup, log: lg}, nil
}

// ProblemCount reports how many problems the default executable set contains.
func (r *Runner) ProblemCount() int { return len(r.problems) }

// capCount applies the optional reduced-run Limit to a mode's natural item
// count: it returns min(n, Limit) when Limit > 0, else n unchanged. Reducible
// modes call it from both Count (progress total) and Execute (loop bound) so
// the two always agree.
func (r *Runner) capCount(n int) int {
	if r.cfg.Limit > 0 && r.cfg.Limit < n {
		return r.cfg.Limit
	}
	return n
}

// CountForMode reports how many problems a given mode will run.
func (r *Runner) CountForMode(mode Mode) int {
	if h, ok := handlerFor(mode); ok {
		return h.Count(r)
	}
	return 0
}

// Run executes the benchmark. It launches (or reuses) the profile's backend,
// runs every problem through the selected scorer, samples GPU stats, then
// aggregates and returns the Run. The caller persists the result.
func (r *Runner) Run(ctx context.Context, rc RunConfig, progress chan<- Progress) (Run, error) {
	started := time.Now()
	profile, err := r.store.Get(rc.ProfileID)
	if err != nil {
		return Run{}, fmt.Errorf("load profile: %w", err)
	}
	r.runCtxTokens = effectiveCtxTokens(profile)

	run := Run{
		ID:          fmt.Sprintf("%s-%d", profile.ID, started.UnixNano()),
		ProfileID:   profile.ID,
		ProfileName: profile.Name,
		Mode:        rc.Mode,
		StartedAt:   started,
		Profile:     snapshotProfile(profile),
	}
	r.runID = run.ID

	h, ok := handlerFor(rc.Mode)
	if !ok {
		return Run{}, fmt.Errorf("unsupported benchmark mode %q", rc.Mode)
	}
	r.logger().Info("benchmark_run_start", "run_id", run.ID, "profile_id", profile.ID, "mode", rc.Mode, "total", h.Count(r))

	// Prepare (validate config / build scorer) BEFORE launching an expensive
	// backend so missing judge config fails fast instead of after a model load.
	scorer, err := h.Prepare(r)
	if err != nil {
		return Run{}, err
	}

	// Install the authoritative live-run feed and its drainer. Every emitter
	// (Run itself, handlers, pollers, watchdogs) writes to the drained internal
	// tap; the drainer folds each event into the feed snapshot and lossy-forwards
	// it to the caller's channel. Lossiness now lives ONLY in that forward — the
	// snapshot never drops an event.
	var stall time.Duration
	switch rc.Mode {
	case ModeTerminalBench:
		stall = tbStallTimeout(r.cfg)
	case ModeDeepSWE:
		stall = deepStallTimeout(r.cfg)
	}
	internal := make(chan Progress, 64)
	stopDrain := make(chan struct{})
	drainDone := make(chan struct{})
	feed := newRunFeed(progress, FeedSnapshot{
		RunID: run.ID, ProfileID: profile.ID, ProfileName: profile.Name,
		Mode: rc.Mode, Total: h.Count(r), StartedAt: started, StallTimeout: stall,
	})
	r.feedMu.Lock()
	r.feed = feed
	r.feedMu.Unlock()
	go func() {
		defer close(drainDone)
		for {
			select {
			case p := <-internal:
				feed.Emit(p)
			case <-stopDrain:
				for {
					select {
					case p := <-internal:
						feed.Emit(p)
					default:
						return
					}
				}
			}
		}
	}()
	// internal is never closed (late stragglers write into the buffer harmlessly).
	// stopDrain fires on every exit path AFTER Run's terminal event is queued; the
	// drainer is then JOINED (<-drainDone) so no feed.Emit forward can outlive this
	// return and race the caller's close(progress) — a send on a closed channel is
	// selected over default and panics, so the join is load-bearing, not cosmetic (BR9).
	defer func() { close(stopDrain); <-drainDone }()

	send(internal, Progress{Total: h.Count(r), Phase: "launch"})

	base, logPath, pid, reused, err := r.ensureLoaded(ctx, profile)
	if err != nil {
		send(internal, Progress{Total: h.Count(r), Phase: "done", Detail: err.Error()})
		return Run{}, err
	}
	// A reused (warm, possibly busy) backend can skew performance numbers;
	// record it so comparisons can tell warm and fresh runs apart. The proxy
	// owns the backend lifecycle: the model stays loaded after the run instead
	// of being killed here.
	run.ReusedInstance = reused
	r.logger().Info("benchmark_backend_loaded", "run_id", run.ID, "pid", pid, "reused", reused)

	// Optionally free the model when the run ends (any exit path). Long agentic
	// runs otherwise leave the backend resident on the GPU — the proxy owns
	// backend lifecycle by default. Only unload a model this run itself loaded:
	// a reused (already-warm) backend was the operator's, so leave it in place.
	// Best-effort; an unload failure never fails the run.
	if r.cfg.UnloadAfterRun && !reused {
		defer r.unloadAfterRun()
	}

	gpu := r.startGPUSampler(pid, base, logPath)
	defer gpu.stop()
	// T7: persist the partial run periodically so a crash mid-run keeps what
	// completed. Flagged Err="in progress" so it never ranks as a finished run
	// until the caller's final save overwrites it.
	if rc.Checkpoint != nil {
		r.lastCheckpoint = time.Time{}
		r.checkpoint = func(results []ProblemResult) {
			now := time.Now()
			if !r.lastCheckpoint.IsZero() && now.Sub(r.lastCheckpoint) < checkpointInterval {
				return
			}
			r.lastCheckpoint = now
			partial := run
			partial.Problems = append([]ProblemResult(nil), results...)
			partial.Err = "in progress"
			partial.Aggregate = aggregate(partial.Problems, gpu.peakVRAM(), gpu.avgUtil())
			h.Finalize(&partial.Aggregate, partial.Problems)
			rc.Checkpoint(partial)
		}
		defer func() { r.checkpoint = nil }()
	}

	// The proxy routes requests by profile id (OpenAI "model" field).
	model := profile.ID

	results, transcripts, err := h.Execute(ctx, r, base, model, scorer, internal)
	run.Problems = results
	run.Transcript = transcripts
	if err == nil {
		// A "successful" Execute where every item carries a per-problem error
		// is an infra failure, not a measured result (BR2): mark the run
		// failed so the dashboard leaderboard and the CLI --min-solve gate
		// never present an all-error run as measured quality.
		err = allItemsFailed(results)
	}
	if err != nil {
		// Return a fully-formed partial run (Err set, mode aggregates included)
		// so callers can persist what completed before the failure/cancel.
		run.Err = err.Error()
		run.FinishedAt = time.Now()
		run.Aggregate = aggregate(run.Problems, gpu.peakVRAM(), gpu.avgUtil())
		h.Finalize(&run.Aggregate, run.Problems)
		r.logger().Warn("benchmark_run_partial", "run_id", run.ID, "mode", rc.Mode,
			"err", err.Error(), "completed", len(run.Problems), "errored", run.Aggregate.Errored)
		send(internal, Progress{Total: h.Count(r), Phase: "done", Detail: err.Error()})
		return run, err
	}

	run.FinishedAt = time.Now()
	run.Aggregate = aggregate(run.Problems, gpu.peakVRAM(), gpu.avgUtil())
	h.Finalize(&run.Aggregate, run.Problems)
	send(internal, Progress{Total: h.Count(r), Phase: "done"})
	r.logger().Info("benchmark_run_done", "run_id", run.ID, "mode", rc.Mode,
		"resolved", run.Aggregate.Resolved, "total", run.Aggregate.Total,
		"errored", run.Aggregate.Errored, "solve_rate", run.Aggregate.SolveRate)
	return run, nil
}

// unloadAfterRun frees the loaded model via the proxy. It uses a bounded,
// run-context-independent timeout so a cancelled run still releases VRAM, and
// swallows errors — the run is already done, there is nothing left to fail.
func (r *Runner) unloadAfterRun() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _ = r.proxy.Unload(ctx, true)
}

// Feed returns the live-run feed for the current or most recent run, or nil
// before any run has started. It survives after a run ends so late renders can
// display terminal state.
func (r *Runner) Feed() *RunFeed {
	r.feedMu.Lock()
	defer r.feedMu.Unlock()
	return r.feed
}

// streamHeartbeat returns an OnDelta callback that emits a "streaming — N tok"
// activity heartbeat for a model-under-test inference. See activityHeartbeat.
func (r *Runner) streamHeartbeat(id, name string) func(int) {
	return r.activityHeartbeat(id, name, "streaming")
}

// activityHeartbeat returns an OnDelta callback that emits a "<verb> — N tok"
// activity heartbeat straight to the feed (bypassing the tap channel — heartbeats
// are pure liveness and must never contend for a buffer slot). A nil feed makes
// it a no-op. name is non-empty so RunFeed tags the entry Kind "stream".
func (r *Runner) activityHeartbeat(id, name, verb string) func(int) {
	return func(n int) {
		f := r.Feed()
		if f == nil {
			return
		}
		f.Emit(Progress{ProblemID: id, ProblemName: name, Phase: "activity", Detail: fmt.Sprintf("%s — %d tok", verb, n)})
	}
}

// harnessLineEmitter returns an onLine callback that forwards each harness output
// line to the feed as a "harness"-kind activity entry (ProblemName empty). Lines
// are rune-clipped to 300 so a runaway harness line never blows up a render. A
// nil feed makes it a no-op.
func (r *Runner) harnessLineEmitter(mode Mode) func(string) {
	label := string(mode)
	return func(line string) {
		f := r.Feed()
		if f == nil {
			return
		}
		f.Emit(Progress{ProblemID: label, Phase: "activity", Detail: truncateRunes(line, 300)})
	}
}

// inferProblem runs the model-under-test inference for one judge problem and
// fills the request metrics. The bool reports whether inference succeeded and
// scoring should follow.
func (r *Runner) inferProblem(ctx context.Context, base, model string, p Problem) (CompletionResult, ProblemResult, ProblemTranscript, bool) {
	res := ProblemResult{ProblemID: p.ID, ProblemName: p.Name}
	tr := ProblemTranscript{ProblemID: p.ID, ProblemName: p.Name}

	reqCtx, cancel := context.WithTimeout(ctx, r.inferTimeout())
	defer cancel()
	comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
		Model:       model,
		Temperature: r.cfg.Temperature,
		MaxTokens:   r.cfg.MaxTokens,
		OnDelta:     r.streamHeartbeat(p.ID, p.Name),
		Messages:    BuildPrompt(p),
	})
	if err != nil {
		res.Err = err.Error()
		res.FailPhase = phaseInfer
		tr.Error = err.Error()
		r.logger().Warn("benchmark_item_failed", "run_id", r.runID, "problem_id", p.ID, "phase", phaseInfer, "err", err.Error())
		return comp, res, tr, false
	}
	res.TTFTms = comp.TTFT.Milliseconds()
	res.TotalMs = comp.Total.Milliseconds()
	res.TokensPerSecond = comp.TokensPerSecond
	res.DecodeTPS = comp.TokensPerSecond
	res.PromptProcessingTPS = comp.PromptProcessingTPS
	res.PromptTokens = comp.PromptTokens
	res.CompletionTokens = comp.CompletionTokens
	tr.ModelResponse = comp.Content
	_, tr.DiffFound = ExtractDiff(comp.Content)
	return comp, res, tr, true
}

// scoreProblem judges a completed inference, mutating res/tr in place. A
// scoring failure becomes a per-problem error, never a run abort.
func (r *Runner) scoreProblem(ctx context.Context, scorer Scorer, p Problem, content string, res *ProblemResult, tr *ProblemTranscript) {
	// A run cancelled before this slot starts scoring must not issue a doomed
	// judge call: leave the inference result untouched (no per-problem error).
	if ctx.Err() != nil {
		return
	}
	// Bound scoring too: a stalled judge endpoint must not hang the run. Allow
	// up to one per-problem timeout per judge sample.
	scoreCtx, scancel := context.WithTimeout(ctx, time.Duration(max(r.cfg.Judge.Samples, 1))*r.cfg.Timeout)
	score, err := scorer.Score(scoreCtx, p, content)
	scancel()
	tr.JudgeRaw = score.Raw
	if err != nil {
		res.Err = err.Error()
		res.FailPhase = phaseScore
		tr.Error = err.Error()
		r.logger().Warn("benchmark_item_failed", "run_id", r.runID, "problem_id", p.ID, "phase", phaseScore, "err", err.Error())
		return
	}
	res.Resolved = score.Resolved
	res.Score = score.Score
	res.Detail = score.Detail
}

// inferTimeout is the per-item inference deadline: the base Timeout plus a
// MaxTokens/decodeFloorTPS budget so a large max_tokens on a slow local model
// no longer guarantees a spurious timeout (T3). Grading calls keep the base
// Timeout — grader verdicts are short.
func (r *Runner) inferTimeout() time.Duration {
	return r.cfg.Timeout + time.Duration(r.cfg.MaxTokens/decodeFloorTPS)*time.Second
}

// benchNop is the shared no-op logger for Runner values built without one
// (e.g. test literals that set only cfg). Package-level so logger() never
// allocates on the hot path.
var benchNop = log.Nop()

// logger returns the run logger, falling back to the shared no-op when unset
// so a Runner constructed as a struct literal (tests) never nil-panics.
func (r *Runner) logger() *slog.Logger {
	if r.log == nil {
		return benchNop
	}
	return r.log
}

// ensureLoaded swaps the profile in through the proxy. Returns the proxy base
// URL plus the backend's pid/log path (GPU sampling + diagnostics) and whether
// the profile was already loaded (warm run — perf numbers may be skewed).
func (r *Runner) ensureLoaded(ctx context.Context, profile domain.Profile) (base, logPath string, pid int, reused bool, err error) {
	if r.proxy == nil {
		return "", "", 0, false, fmt.Errorf("http proxy not configured")
	}
	if err := r.proxy.EnsureRunning(ctx); err != nil {
		return "", "", 0, false, fmt.Errorf("start proxy: %w", err)
	}
	reused = r.proxy.Status().LoadedProfileID == profile.ID
	st, err := r.proxy.Load(ctx, profile.ID)
	if err != nil {
		return "", "", 0, false, fmt.Errorf("load profile via proxy: %w", err)
	}
	return r.proxy.BaseURL(), st.LoadedLogPath, st.LoadedPID, reused, nil
}

// graderFor returns the grader used for semantic scoring. If an external judge
// is configured it is the gold standard; otherwise the model under test grades
// itself (zero-config, flagged judgedBy=self). base/model identify the
// model-under-test server for the self-judge path.
func (r *Runner) graderFor(base, model string) grader {
	maxTok := r.cfg.MaxTokens
	if j := r.cfg.Judge; j.BaseURL != "" && j.Model != "" {
		return llmGrader{base: j.BaseURL, apiKey: j.APIKey, model: j.Model, maxTok: maxTok, judgedBy: "external", activity: r.activityHeartbeat("grader", "grading", "grading")}
	}
	return llmGrader{base: base, model: model, maxTok: maxTok, judgedBy: "self", activity: r.activityHeartbeat("grader", "grading", "grading")}
}

func (r *Runner) newScorer(mode Mode) (Scorer, error) {
	switch mode {
	case ModeJudge:
		j := r.cfg.Judge
		if j.BaseURL == "" || j.Model == "" {
			return nil, fmt.Errorf("judge mode requires benchmark.judge.base_url and benchmark.judge.model in config")
		}
		return judgeScorer{base: j.BaseURL, apiKey: j.APIKey, model: j.Model, maxTok: r.cfg.MaxTokens, samples: j.Samples, activity: r.activityHeartbeat("judge", "judging", "judging")}, nil
	default:
		return nil, fmt.Errorf("unsupported scoring mode %q", mode)
	}
}

// --- helpers ---------------------------------------------------------------

// send delivers p to ch. The channel handed to handlers/pollers/watchdogs is
// always the runner's internal tap, drained for the whole run, so this blocks
// only momentarily on a full 64-slot buffer and never loses an event. Lossiness
// lives solely in RunFeed.Emit's forward to the caller's channel. A nil ch (a
// test emitter with no consumer) is a no-op.
func send(ch chan<- Progress, p Progress) {
	if ch == nil {
		return
	}
	ch <- p
}

// allItemsFailed returns a run-level error when every item of a non-empty
// result set failed with a per-problem error (BR2); nil otherwise.
func allItemsFailed(results []ProblemResult) error {
	if len(results) == 0 {
		return nil
	}
	for _, pr := range results {
		if pr.Err == "" {
			return nil
		}
	}
	return fmt.Errorf("all %d items failed (first: %s)", len(results), results[0].Err)
}

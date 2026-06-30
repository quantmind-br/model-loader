package benchmark

import (
	"context"
	"fmt"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

// ProxyController is how the runner reaches backends: ensure the proxy is up,
// swap the profile in, and address all inference at the proxy. The instance
// port never leaks into this package.
type ProxyController interface {
	EnsureRunning(context.Context) error
	Load(ctx context.Context, profileID string) (httpproxy.Status, error)
	Status() httpproxy.Status
	BaseURL() string
}

// RunConfig is the per-invocation request from the UI.
type RunConfig struct {
	ProfileID string
	Mode      Mode
}

// Progress is streamed to the UI while a run executes.
type Progress struct {
	Index       int
	Total       int
	ProblemID   string
	ProblemName string
	Phase       string // "launch" | "infer" | "score" | "done"
}

// Runner orchestrates a full benchmark run against one profile.
type Runner struct {
	store           profilestore.Store
	mon             monitor.Manager
	proxy           ProxyController
	cfg             Config
	problems        []Problem            // embedded SWE-bench Lite coding set
	mathProblems    []MathProblem        // embedded GSM8K set for ModeMathBench
	codeGenProblems []CodeGenProblem     // embedded HumanEval set for ModeCodeGenBench
	instProblems    []InstructionProblem // embedded instruction-robustness set for ModeInstBench
	mmluProblems    []MMLUProblem        // embedded MMLU subset for ModeMMLUBench
	arxivDocs       []ArxivDoc           // real arXiv abstracts used as long-context quality filler
	ragasProblems   []RagasProblem       // synthetic RAG scenarios for ModeRagasBench
	summaryProblems []SummaryProblem     // synthetic multi-doc bundles for ModeSummaryBench
	presets         []tpPreset           // parsed ModeLlamaBench configs
	reps            int                  // ModeLlamaBench repetitions per preset
	warmup          int                  // discarded warmup reps before measurement
	runCtxTokens    int                  // effective context of the profile under test; set by Run before Execute (single-flight)
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
	return &Runner{store: store, mon: mon, proxy: proxy, cfg: cfg, problems: problems, mathProblems: mathProblems, codeGenProblems: codeGenProblems, instProblems: instProblems, mmluProblems: mmluProblems, arxivDocs: arxivDocs, ragasProblems: ragasProblems, summaryProblems: summaryProblems, presets: presets, reps: reps, warmup: warmup}, nil
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

	h, ok := handlerFor(rc.Mode)
	if !ok {
		return Run{}, fmt.Errorf("unsupported benchmark mode %q", rc.Mode)
	}

	// Prepare (validate config / build scorer) BEFORE launching an expensive
	// backend so missing judge config fails fast instead of after a model load.
	scorer, err := h.Prepare(r)
	if err != nil {
		return Run{}, err
	}

	send(progress, Progress{Total: h.Count(r), Phase: "launch"})

	base, logPath, pid, reused, err := r.ensureLoaded(ctx, profile)
	if err != nil {
		return Run{}, err
	}
	// A reused (warm, possibly busy) backend can skew performance numbers;
	// record it so comparisons can tell warm and fresh runs apart. The proxy
	// owns the backend lifecycle: the model stays loaded after the run instead
	// of being killed here.
	run.ReusedInstance = reused

	gpu := r.startGPUSampler(pid, base, logPath)
	defer gpu.stop()

	// The proxy routes requests by profile id (OpenAI "model" field).
	model := profile.ID

	results, transcripts, err := h.Execute(ctx, r, base, model, scorer, progress)
	run.Problems = results
	run.Transcript = transcripts
	if err != nil {
		// Return a fully-formed partial run (Err set, mode aggregates included)
		// so callers can persist what completed before the failure/cancel.
		run.Err = err.Error()
		run.FinishedAt = time.Now()
		run.Aggregate = aggregate(run.Problems, gpu.peakVRAM(), gpu.avgUtil())
		h.Finalize(&run.Aggregate, run.Problems)
		return run, err
	}

	run.FinishedAt = time.Now()
	run.Aggregate = aggregate(run.Problems, gpu.peakVRAM(), gpu.avgUtil())
	h.Finalize(&run.Aggregate, run.Problems)
	send(progress, Progress{Total: h.Count(r), Phase: "done"})
	return run, nil
}

// inferProblem runs the model-under-test inference for one judge problem and
// fills the request metrics. The bool reports whether inference succeeded and
// scoring should follow.
func (r *Runner) inferProblem(ctx context.Context, base, model string, p Problem) (CompletionResult, ProblemResult, ProblemTranscript, bool) {
	res := ProblemResult{ProblemID: p.ID, ProblemName: p.Name}
	tr := ProblemTranscript{ProblemID: p.ID, ProblemName: p.Name}

	reqCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()
	comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
		Model:       model,
		Temperature: r.cfg.Temperature,
		MaxTokens:   r.cfg.MaxTokens,
		Messages:    BuildPrompt(p),
	})
	if err != nil {
		res.Err = err.Error()
		tr.Error = err.Error()
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
		tr.Error = err.Error()
		return
	}
	res.Resolved = score.Resolved
	res.Score = score.Score
	res.Detail = score.Detail
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
		return llmGrader{base: j.BaseURL, apiKey: j.APIKey, model: j.Model, maxTok: maxTok, judgedBy: "external"}
	}
	return llmGrader{base: base, model: model, maxTok: maxTok, judgedBy: "self"}
}

func (r *Runner) newScorer(mode Mode) (Scorer, error) {
	switch mode {
	case ModeJudge:
		j := r.cfg.Judge
		if j.BaseURL == "" || j.Model == "" {
			return nil, fmt.Errorf("judge mode requires benchmark.judge.base_url and benchmark.judge.model in config")
		}
		return judgeScorer{base: j.BaseURL, apiKey: j.APIKey, model: j.Model, maxTok: r.cfg.MaxTokens, samples: j.Samples}, nil
	default:
		return nil, fmt.Errorf("unsupported scoring mode %q", mode)
	}
}

// --- helpers ---------------------------------------------------------------

func send(ch chan<- Progress, p Progress) {
	if ch == nil {
		return
	}
	select {
	case ch <- p:
	default:
	}
}

package benchmark

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

// healthTimeout bounds how long Run waits for a freshly launched backend to
// answer /health (model load can be slow for large GGUFs).
const healthTimeout = 3 * time.Minute

// Config tunes the engine. main.go maps config.BenchmarkConfig onto it so the
// benchmark package stays decoupled from the config package.
type Config struct {
	MaxTokens         int
	Temperature       float64
	Timeout           time.Duration // per-problem inference timeout
	LongContextTokens int           // target prompt size for the needle probe (0 → default)
	SaveTranscripts   bool          // capture raw model/judge I/O for debugging
	Judge             JudgeEndpoint
	// EmbeddingsBaseURL optionally overrides where similarity graders fetch
	// embeddings. Empty → reuse the model-under-test server. Reserved for the
	// instruction-consistency mode (added in a later plan).
	EmbeddingsBaseURL string

	// LlamaBenchPresets are "pp/tg" strings (prompt tokens / generation tokens)
	// for ModeLlamaBench. Empty → default {512/128, 4096/256}.
	LlamaBenchPresets []string
	// LlamaBenchReps is how many times each preset is measured and averaged.
	// <=0 → default 3.
	LlamaBenchReps int
	// LlamaBenchWarmup is how many discarded warmup reps run before measurement
	// to remove cold-start bias. <0 → default 1; 0 disables warmup.
	LlamaBenchWarmup int
}

// tpPreset is one parsed throughput configuration: pp tokens in, tg tokens out.
type tpPreset struct {
	PromptTokens int
	GenTokens    int
}

func (p tpPreset) name() string { return fmt.Sprintf("pp %d / tg %d", p.PromptTokens, p.GenTokens) }
func (p tpPreset) id() string   { return fmt.Sprintf("tp-%d-%d", p.PromptTokens, p.GenTokens) }

// defaultPresets must stay in sync with the benchmark.llamabench.presets default in internal/config/config.go.
var defaultPresets = []tpPreset{
	{PromptTokens: 128, GenTokens: 512}, // chat-like: short prefill, long gen
	{PromptTokens: 512, GenTokens: 128},
	{PromptTokens: 2048, GenTokens: 256},
	{PromptTokens: 4096, GenTokens: 256},
	{PromptTokens: 8192, GenTokens: 128}, // RAG-like: long prefill, short gen
	{PromptTokens: 16384, GenTokens: 64}, // extreme RAG
}

// parsePresets parses "pp/tg" strings into tpPresets. Empty input → defaults.
func parsePresets(raw []string) ([]tpPreset, error) {
	if len(raw) == 0 {
		return append([]tpPreset(nil), defaultPresets...), nil
	}
	out := make([]tpPreset, 0, len(raw))
	for _, s := range raw {
		parts := strings.SplitN(strings.TrimSpace(s), "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid llama-bench preset %q (want \"pp/tg\", e.g. \"512/128\")", s)
		}
		pp, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || pp <= 0 {
			return nil, fmt.Errorf("invalid prompt size in preset %q (want \"pp/tg\" with positive ints)", s)
		}
		tg, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || tg <= 0 {
			return nil, fmt.Errorf("invalid gen size in preset %q (want \"pp/tg\" with positive ints)", s)
		}
		out = append(out, tpPreset{PromptTokens: pp, GenTokens: tg})
	}
	return out, nil
}

// JudgeEndpoint is the OpenAI-compatible endpoint used by ModeJudge.
type JudgeEndpoint struct {
	BaseURL string
	APIKey  string
	Model   string
	// Samples is how many times the judge grades each problem; the run uses the
	// median score + majority resolved (self-consistency) to fight noise.
	Samples int
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
	pm              processmgr.Manager
	mon             monitor.Manager
	resolver        backendcatalog.Resolver
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
}

// NewRunner builds a Runner and loads the embedded dataset.
func NewRunner(store profilestore.Store, pm processmgr.Manager, mon monitor.Manager, resolver backendcatalog.Resolver, cfg Config) (*Runner, error) {
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
	return &Runner{store: store, pm: pm, mon: mon, resolver: resolver, cfg: cfg, problems: problems, mathProblems: mathProblems, codeGenProblems: codeGenProblems, instProblems: instProblems, mmluProblems: mmluProblems, arxivDocs: arxivDocs, ragasProblems: ragasProblems, summaryProblems: summaryProblems, presets: presets, reps: reps, warmup: warmup}, nil
}

// ProblemCount reports how many problems the default executable set contains.
func (r *Runner) ProblemCount() int { return len(r.problems) }

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

	port, logPath, pid, owned, err := r.ensureInstance(ctx, profile)
	if err != nil {
		return Run{}, err
	}
	// A reused (warm, possibly busy) instance can skew performance numbers;
	// record it so comparisons can tell warm and fresh runs apart.
	run.ReusedInstance = !owned
	if owned {
		defer func() { _ = r.pm.Kill(pid) }()
	}

	gpu := r.startGPUSampler(pid, port, logPath)
	defer gpu.stop()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	model := requestModelName(profile)

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

// ensureInstance reuses a live instance for the profile or launches a new one.
// Returns the port, log path, pid, and whether Run owns (must kill) it. It is
// cancellation-aware: a cancelled ctx aborts before launch and kills a
// just-launched backend instead of blocking on the health timeout.
func (r *Runner) ensureInstance(ctx context.Context, profile domain.Profile) (port int, logPath string, pid int, owned bool, err error) {
	for _, inst := range r.pm.List() {
		if inst.ProfileID == profile.ID && !inst.Crashed {
			return inst.Port, inst.LogPath, inst.PID, false, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, "", 0, false, err
	}
	rb, err := r.resolver.Resolve(profile)
	if err != nil {
		return 0, "", 0, false, fmt.Errorf("resolve backend: %w", err)
	}
	profile.Launch.ResolvedExecutable = rb.ExecutablePath
	profile.Launch.ResolvedBackendKind = rb.Backend.Kind

	attemptID := fmt.Sprintf("bench-%d", time.Now().UnixNano())
	inst, err := r.pm.Launch(profile, processmgr.LaunchBackground, attemptID)
	if err != nil {
		return 0, "", 0, false, fmt.Errorf("launch backend: %w", err)
	}
	if err := r.waitHealthy(ctx, inst.PID, inst.Port, attemptID); err != nil {
		_ = r.pm.Kill(inst.PID)
		return 0, "", 0, false, err
	}
	return inst.Port, inst.LogPath, inst.PID, true, nil
}

// waitHealthy runs the (context-unaware) processmgr health poll in a goroutine
// and races it against ctx cancellation. On cancel it kills the PID so a
// launched-but-not-yet-ready backend doesn't keep consuming GPU/VRAM.
func (r *Runner) waitHealthy(ctx context.Context, pid, port int, attemptID string) error {
	done := make(chan error, 1)
	go func() { done <- r.pm.WaitHealthy(pid, port, healthTimeout, attemptID) }()
	select {
	case <-ctx.Done():
		_ = r.pm.Kill(pid)
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("backend not healthy: %w", err)
		}
		return nil
	}
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

// --- long-context needle probe --------------------------------------------

// needle is one randomized fact planted in the long-context haystack.
type needle struct {
	label string // distinguishes the three planted facts
	value string // "<City>-<4digit>", the literal the model must recover
}

var needleCities = []string{
	"Reykjavik", "Ulaanbaatar", "Montevideo", "Gaborone", "Tbilisi",
	"Ljubljana", "Windhoek", "Paramaribo", "Bishkek", "Vientiane",
}

// buildNeedles makes three randomized needles at distinct depths from a seed,
// so a run's exact needle set can be reproduced later. The three values are
// guaranteed distinct so scoreNeedles can't double-count a collision.
func buildNeedles(seed int64) []needle {
	rng := rand.New(rand.NewSource(seed))
	labels := []string{"alpha", "beta", "gamma"}
	out := make([]needle, 3)
	seen := make(map[string]bool, 3)
	for i := range out {
		var value string
		for {
			city := needleCities[rng.Intn(len(needleCities))]
			value = fmt.Sprintf("%s-%04d", city, rng.Intn(9000)+1000)
			if !seen[value] {
				break
			}
		}
		seen[value] = true
		out[i] = needle{label: labels[i], value: value}
	}
	return out
}

// needleNormRe strips everything but letters and digits for needle matching.
var needleNormRe = regexp.MustCompile(`[^a-z0-9]+`)

// normalizeNeedleText lowercases and removes separators so "Reykjavik - 1042",
// "reykjavik–1042" and "Reykjavik-1042" all compare equal.
func normalizeNeedleText(s string) string {
	return needleNormRe.ReplaceAllString(strings.ToLower(s), "")
}

// scoreNeedles returns the fraction of needle values present in the response,
// comparing separator-normalized text so formatting variants still count.
func scoreNeedles(response string, needles []needle) float64 {
	if len(needles) == 0 {
		return 0
	}
	norm := normalizeNeedleText(response)
	found := 0
	for _, n := range needles {
		if strings.Contains(norm, normalizeNeedleText(n.value)) {
			found++
		}
	}
	return float64(found) / float64(len(needles))
}

// buildMultiNeedleHaystack generates ~targetTokens of varied pseudo-code with
// the needles planted at ~25%, ~50%, ~75% depth.
func buildMultiNeedleHaystack(targetTokens int, needles []needle) string {
	charBudget := targetTokens * 4
	depths := []int{charBudget / 4, charBudget / 2, charBudget * 3 / 4}
	var b strings.Builder
	planted := make([]bool, len(needles))
	i := 0
	for b.Len() < charBudget {
		for k := range needles {
			if !planted[k] && k < len(depths) && b.Len() >= depths[k] {
				fmt.Fprintf(&b, "\n# === FILE: registry_%s.py ===\n# Internal registration table.\nMAGIC_%s_NUMBER = '%s'\n# End.\n\n",
					needles[k].label, strings.ToUpper(needles[k].label), needles[k].value)
				planted[k] = true
			}
		}
		fmt.Fprintf(&b, "\n# === FILE: module_%04d.py ===\n", i)
		fmt.Fprintf(&b, "def handler_%04d(state, payload, retries=%d):\n", i, i%7)
		fmt.Fprintf(&b, "    total = 0\n    for item in payload.get('items_%d', []):\n", i%5)
		fmt.Fprintf(&b, "        total += item.weight * %d\n", (i%9)+1)
		fmt.Fprintf(&b, "    return Result(total=total, code=%d)\n", i%256)
		i++
	}
	for k := range needles {
		if !planted[k] {
			fmt.Fprintf(&b, "\nMAGIC_%s_NUMBER = '%s'\n", strings.ToUpper(needles[k].label), needles[k].value)
		}
	}
	return b.String()
}

// buildQualityHaystack generates ~targetTokens of realistic filler from real
// arXiv abstracts with the needles planted at ~25/50/75% depth as constant
// definitions the model must recover. Falls back to the pseudo-code haystack
// when no abstracts are available.
func buildQualityHaystack(docs []ArxivDoc, targetTokens int, needles []needle) string {
	if len(docs) == 0 {
		return buildMultiNeedleHaystack(targetTokens, needles)
	}
	charBudget := targetTokens * 4
	depths := []int{charBudget / 4, charBudget / 2, charBudget * 3 / 4}
	var b strings.Builder
	planted := make([]bool, len(needles))
	i := 0
	for b.Len() < charBudget {
		for k := range needles {
			if !planted[k] && k < len(depths) && b.Len() >= depths[k] {
				fmt.Fprintf(&b, "\n# === FILE: registry_%s.py ===\n# Internal registration table.\nMAGIC_%s_NUMBER = '%s'\n# End.\n\n",
					needles[k].label, strings.ToUpper(needles[k].label), needles[k].value)
				planted[k] = true
			}
		}
		d := docs[i%len(docs)]
		fmt.Fprintf(&b, "\n# === PAPER %s [%s] ===\n## %s\n%s\n", d.ID, d.Category, d.Title, d.Abstract)
		i++
	}
	for k := range needles {
		if !planted[k] {
			fmt.Fprintf(&b, "\nMAGIC_%s_NUMBER = '%s'\n", strings.ToUpper(needles[k].label), needles[k].value)
		}
	}
	return b.String()
}

// runLongContext packs a large synthetic code corpus with three randomized
// needles planted at varied depths, then asks the model to retrieve all three.
// Score = fraction recovered; resolved = all three found.
func (r *Runner) runLongContext(ctx context.Context, base, model string) (ProblemResult, ProblemTranscript) {
	res := ProblemResult{ProblemID: "long-context-needle", ProblemName: "Long-context needle retrieval"}
	tr := ProblemTranscript{ProblemID: res.ProblemID, ProblemName: res.ProblemName}

	targetTokens := r.cfg.LongContextTokens
	if targetTokens <= 0 {
		targetTokens = 8000
	}
	seed := time.Now().UnixNano()
	res.Seed = seed
	needles := buildNeedles(seed)
	haystack := buildQualityHaystack(r.arxivDocs, targetTokens, needles)
	user := "Below is a dump of a Python codebase. Read it carefully.\n\n" + haystack +
		"\n\nQuestion: three files define a constant named MAGIC_<NAME>_NUMBER. " +
		"List all three literal values, one per line, no explanation."

	reqCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()
	comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
		Model:       model,
		Temperature: 0,
		MaxTokens:   128,
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a careful code-reading assistant. Answer literally."},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		res.Err = err.Error()
		tr.Error = err.Error()
		return res, tr
	}
	res.TTFTms = comp.TTFT.Milliseconds()
	res.TotalMs = comp.Total.Milliseconds()
	res.TokensPerSecond = comp.TokensPerSecond
	res.DecodeTPS = comp.TokensPerSecond
	res.PromptProcessingTPS = comp.PromptProcessingTPS
	res.PromptTokens = comp.PromptTokens
	res.CompletionTokens = comp.CompletionTokens
	tr.ModelResponse = comp.Content

	frac := scoreNeedles(comp.Content, needles)
	res.Score = frac
	res.Resolved = frac == 1.0
	res.Detail = fmt.Sprintf("recovered %.0f%% of needles (%d/3); prompt≈%d tok; pp %.0f t/s; tg %.0f t/s; seed=%d",
		frac*100, int(frac*3+0.5), comp.PromptTokens, comp.PromptProcessingTPS, comp.TokensPerSecond, seed)
	return res, tr
}

// --- llama-bench throughput probe ------------------------------------------

// runLlamaBench measures generation throughput for one preset: it sends a
// fixed-size prompt asking for tg tokens, repeated r.reps times, and averages
// TTFT / tokens-per-second across the successful repetitions. The model field,
// streaming and timing all come from Complete, so it works against any
// OpenAI-compatible backend (llama-server / vLLM / SGLang).
func (r *Runner) runLlamaBench(ctx context.Context, base, model string, ps tpPreset) (ProblemResult, ProblemTranscript) {
	res := ProblemResult{ProblemID: ps.id(), ProblemName: ps.name()}
	tr := ProblemTranscript{ProblemID: ps.id(), ProblemName: ps.name()}

	prompt := buildFixedPrompt(ps.PromptTokens)
	msgs := []ChatMessage{
		{Role: "system", Content: "You are a verbose writing assistant. Continue at length."},
		{Role: "user", Content: prompt},
	}

	for w := 0; w < r.warmup; w++ {
		select {
		case <-ctx.Done():
			res.Err = ctx.Err().Error()
			tr.Error = res.Err
			return res, tr
		default:
		}
		warmCtx, warmCancel := context.WithTimeout(ctx, r.cfg.Timeout)
		_, _ = Complete(warmCtx, nil, base, "", ChatRequest{
			Model: model, Temperature: 0, MaxTokens: ps.GenTokens, IgnoreEOS: true, Messages: msgs,
		})
		warmCancel()
	}

	var ttftSum, totalSum, ppTpsSum float64
	var ppSum, tgSum, short int
	var tpsSamples []float64
	var fromServer bool
	var lastContent string
	for i := 0; i < r.reps; i++ {
		select {
		case <-ctx.Done():
			res.Err = ctx.Err().Error()
			tr.Error = res.Err
			return res, tr
		default:
		}
		reqCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
		comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
			Model:       model,
			Temperature: 0,
			MaxTokens:   ps.GenTokens,
			IgnoreEOS:   true, // force exactly tg tokens so samples stay comparable
			Messages:    msgs,
		})
		cancel()
		if err != nil {
			res.Err = err.Error()
			tr.Error = err.Error()
			return res, tr
		}
		lastContent = comp.Content
		// Only average full-length samples: a backend that ignored ignore_eos and
		// stopped early would otherwise skew tok/s and make the preset
		// incomparable across profiles.
		if comp.CompletionTokens < ps.GenTokens {
			short++
			continue
		}
		ttftSum += float64(comp.TTFT.Milliseconds())
		tpsSamples = append(tpsSamples, comp.TokensPerSecond)
		ppTpsSum += comp.PromptProcessingTPS
		totalSum += float64(comp.Total.Milliseconds())
		ppSum += comp.PromptTokens
		tgSum += comp.CompletionTokens
		if comp.TimingsFromServer {
			fromServer = true
		}
	}

	tr.ModelResponse = lastContent
	ok := len(tpsSamples)
	if ok == 0 {
		// Every sample stopped before tg tokens — the backend doesn't honor
		// ignore_eos, so this preset can't be measured reliably here.
		res.Detail = fmt.Sprintf("no full-length sample: all %d stopped before %d gen tokens (backend ignored ignore_eos?)", short, ps.GenTokens)
		return res, tr
	}
	n := float64(ok)
	mean, stddev, minTPS, maxTPS := tpsStats(tpsSamples)
	res.Resolved = true
	res.TTFTms = int64(ttftSum / n)
	res.TokensPerSecond = mean
	res.DecodeTPS = mean
	res.TPSStdDev = stddev
	res.TPSMin = minTPS
	res.TPSMax = maxTPS
	res.PromptProcessingTPS = ppTpsSum / n
	res.TotalMs = int64(totalSum / n)
	res.PromptTokens = ppSum / ok
	res.CompletionTokens = tgSum / ok
	res.Detail = fmt.Sprintf("pp≈%d tg=%d; tok/s %.1f ±%.1f [%.1f–%.1f]; TTFT %dms (n=%d)",
		res.PromptTokens, ps.GenTokens, mean, stddev, minTPS, maxTPS, res.TTFTms, ok)
	if fromServer {
		res.Detail += " (server timings)"
	}
	if short > 0 {
		res.Detail += fmt.Sprintf("; %d short dropped", short)
	}
	return res, tr
}

// tpsStats returns mean, population standard deviation, min and max of the
// per-rep tok/s samples.
func tpsStats(samples []float64) (mean, stddev, min, max float64) {
	n := float64(len(samples))
	if n == 0 {
		return 0, 0, 0, 0
	}
	min, max = samples[0], samples[0]
	var sum float64
	for _, s := range samples {
		sum += s
		if s < min {
			min = s
		}
		if s > max {
			max = s
		}
	}
	mean = sum / n
	var varSum float64
	for _, s := range samples {
		d := s - mean
		varSum += d * d
	}
	stddev = math.Sqrt(varSum / n)
	return mean, stddev, min, max
}

// buildFixedPrompt generates ~promptTokens of deterministic filler prose
// (≈4 chars/token) to drive a fixed prompt-processing load.
func buildFixedPrompt(promptTokens int) string {
	charBudget := promptTokens * 4
	var b strings.Builder
	b.WriteString("Summarize and then continue the following technical log in detail.\n\n")
	i := 0
	for b.Len() < charBudget {
		fmt.Fprintf(&b, "event %04d: subsystem %d processed batch of %d items in %dms; status=ok retries=%d\n",
			i, i%13, (i%97)+1, (i*7)%500, i%4)
		i++
	}
	return b.String()
}

// --- GPU sampling ----------------------------------------------------------

type gpuSampler struct {
	mu      sync.Mutex
	peak    uint64
	utilSum float64
	utilN   int
	stopFn  func() error
}

func (g *gpuSampler) peakVRAM() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.peak
}

func (g *gpuSampler) avgUtil() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.utilN == 0 {
		return 0
	}
	return g.utilSum / float64(g.utilN)
}

func (g *gpuSampler) stop() {
	if g.stopFn != nil {
		_ = g.stopFn()
	}
}

// startGPUSampler subscribes to the monitor and accumulates GPU stats in the
// background. Failures are non-fatal: the sampler simply records nothing.
func (r *Runner) startGPUSampler(pid, port int, logPath string) *gpuSampler {
	g := &gpuSampler{}
	ch, cancel, err := r.mon.Subscribe(pid, port, logPath)
	if err != nil {
		return g
	}
	g.stopFn = cancel
	go func() {
		for evt := range ch {
			if evt.Source != monitor.SourceGPU {
				continue
			}
			stats, ok := evt.Data.(monitor.GPUStats)
			if !ok {
				continue
			}
			g.mu.Lock()
			if stats.VRAMUsedMB > g.peak {
				g.peak = stats.VRAMUsedMB
			}
			g.utilSum += stats.Utilization
			g.utilN++
			g.mu.Unlock()
		}
	}()
	return g
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

func aggregate(results []ProblemResult, peakVRAM uint64, avgUtil float64) Aggregate {
	a := Aggregate{Total: len(results), PeakVRAMMB: peakVRAM, AvgGPUUtil: avgUtil}
	var scoreSum, tpsSum, ttftSum, ppSum, decSum float64
	var tpsN, ttftN, ppN, decN int
	for _, r := range results {
		if r.Err != "" {
			// Request/judge failures are infrastructure noise, not model quality:
			// count them separately and keep them out of the quality rates below.
			a.Errored++
		}
		if r.Resolved {
			a.Resolved++
		}
		scoreSum += r.Score
		a.TotalPromptTokens += r.PromptTokens
		a.TotalCompletionTokens += r.CompletionTokens
		a.TotalMs += r.TotalMs
		if r.TokensPerSecond > 0 {
			tpsSum += r.TokensPerSecond
			tpsN++
		}
		if r.PromptProcessingTPS > 0 {
			ppSum += r.PromptProcessingTPS
			ppN++
		}
		if r.DecodeTPS > 0 {
			decSum += r.DecodeTPS
			decN++
		}
		if r.TTFTms > 0 {
			ttftSum += float64(r.TTFTms)
			ttftN++
		}
	}
	if answered := a.Total - a.Errored; answered > 0 {
		a.SolveRate = float64(a.Resolved) / float64(answered)
		a.AvgScore = scoreSum / float64(answered)
	}
	if tpsN > 0 {
		a.AvgTokensPerSecond = tpsSum / float64(tpsN)
	}
	if ttftN > 0 {
		a.AvgTTFTms = ttftSum / float64(ttftN)
	}
	if ppN > 0 {
		a.AvgPromptProcessingTPS = ppSum / float64(ppN)
	}
	if decN > 0 {
		a.AvgDecodeTPS = decSum / float64(decN)
	}
	return a
}

var quantRe = regexp.MustCompile(`(?i)(IQ?\d+(_[A-Z0-9]+)*|Q\d+_[A-Z0-9_]+|Q\d+|F16|BF16|F32)`)

// snapshotProfile records the quantization-relevant config so comparisons make
// explicit what changed between runs.
func snapshotProfile(p domain.Profile) ProfileSnapshot {
	s := ProfileSnapshot{Model: p.Model, KeyArgs: map[string]string{}}
	if m := quantRe.FindString(filepath.Base(p.Model)); m != "" {
		s.Quantization = strings.ToUpper(m)
	}
	s.CacheTypeK = argString(p.Args, "cache-type-k")
	s.CacheTypeV = argString(p.Args, "cache-type-v")
	if v := argString(p.Args, "ctx-size"); v != "" {
		fmt.Sscanf(v, "%d", &s.CtxSize)
	}
	for _, k := range []string{"cache-type-k", "cache-type-v", "ctx-size", "n-gpu-layers", "flash-attn", "threads"} {
		if v := argString(p.Args, k); v != "" {
			s.KeyArgs[k] = v
		}
	}
	return s
}

func argString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strings.TrimSuffix(fmt.Sprintf("%v", t), ".0")
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", t)
	}
}

// requestModelName picks the model field sent in the chat request. llama.cpp
// ignores it, but vLLM/SGLang validate it against the served model id — which
// is the full repo ID/path the server was launched with, so send p.Model whole
// (not its basename).
func requestModelName(p domain.Profile) string {
	if v := argString(p.Args, "served-model-name"); v != "" {
		return v
	}
	if p.Model != "" {
		return p.Model
	}
	return "default"
}

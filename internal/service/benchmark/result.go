package benchmark

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// Mode selects how a model's answer is scored.
type Mode string

const (
	// ModeJudge is the coding benchmark: real SWE-bench Lite issues graded by a
	// reference-guided LLM judge (the only quality-scoring mode).
	ModeJudge Mode = "judge"
	// ModeLongContext: long-context needle retrieval probe — an objective
	// diagnostic (not judged) measuring whether KV-cache quantization degrades
	// deep-context recall + prompt/gen speed.
	ModeLongContext Mode = "longctx"
	// ModeLlamaBench: throughput probe in the style of the official llama-bench —
	// fixed-size prompts (pp tokens in / tg tokens out) measuring TTFT and
	// tokens/second. Not judged; the target metric is generation speed.
	ModeLlamaBench Mode = "llama-bench"
	// ModeMathBench: math-reasoning probe — curated GSM8K problems scored by
	// exact numeric match (objective, not judged). The most direct signal of
	// reasoning degradation from quantization.
	ModeMathBench Mode = "math-bench"
	// ModeCodeGenBench: code-generation probe — curated HumanEval problems whose
	// generated solutions are executed against unit tests in a sandboxed python3
	// subprocess (objective Pass@1, not judged).
	ModeCodeGenBench Mode = "codegen-bench"
	// ModeInstBench: instruction-robustness probe — curated prompts checking
	// structured-format adherence (JSON / markdown list), refusal of disallowed
	// requests, and answer consistency across repeated generations. Format and
	// refusal are deterministic checks; consistency is scored by similarity.
	ModeInstBench Mode = "instruction-bench"
	// ModeMMLUBench: factual-knowledge probe — a curated MMLU subset stratified
	// across STEM/Humanities/Social Sciences/Other, scored by objective
	// multiple-choice letter exact-match (not judged). A direct signal of
	// knowledge degradation from quantization.
	ModeMMLUBench Mode = "mmlu-bench"
	// ModeRagasBench: retrieval-augmented-generation quality probe — synthetic
	// scenarios where the model answers a question strictly from supplied
	// documents. A grader scores faithfulness (no hallucination beyond the
	// docs), answer relevancy, and context precision. Graded (external or self).
	ModeRagasBench Mode = "ragas-bench"
	// ModeSummaryBench: multi-document summarization-coherence probe — the model
	// summarizes ~10 short documents and must cover a known set of key facts. A
	// grader scores coherence + fact coverage. Graded (external or self).
	ModeSummaryBench Mode = "summary-bench"
	// ModeTerminalBench: agentic terminal-task benchmark — wraps the external
	// Terminal-Bench harness (`tb`), which drives a Dockerized tmux sandbox with
	// the Terminus agent pointed at the proxy and scores each task pass/fail by
	// running its in-container tests. Objective; requires the `tb` CLI + Docker.
	ModeTerminalBench Mode = "terminal-bench"
	// ModeSweBenchPro: agentic software-engineering benchmark — wraps the external
	// SWE-bench Pro harness (scaleapi/SWE-bench_Pro-os). A patch-generation agent
	// (pointed at the proxy) produces diffs, which the harness applies and tests in
	// per-instance Docker images, scoring each instance pass/fail by fail_to_pass +
	// pass_to_pass. Objective; requires a cloned harness + Docker + python.
	ModeSweBenchPro Mode = "swe-bench-pro"
	// ModeDeepSWE: agentic software-engineering benchmark — wraps the external
	// Pier harness (datacurve-ai/pier) running the DeepSWE task corpus
	// (datacurve-ai/deep-swe). The mini-swe-agent works inside a per-task Docker
	// sandbox pointed at the proxy (LiteLLM chat completions) and commits a patch;
	// Pier's program-based verifier scores each task pass/fail in a pristine
	// container. Objective; requires the `pier` CLI + Docker + a cloned task corpus.
	ModeDeepSWE Mode = "deep-swe"
)

// Title returns a short human label for the mode.
func (m Mode) Title() string {
	switch m {
	case ModeJudge:
		return "LLM judge (SWE-bench Lite)"
	case ModeLongContext:
		return "Long-context needle"
	case ModeLlamaBench:
		return "Throughput (llama-bench)"
	case ModeMathBench:
		return "Math reasoning (GSM8K)"
	case ModeCodeGenBench:
		return "Code generation (HumanEval)"
	case ModeInstBench:
		return "Instruction following"
	case ModeMMLUBench:
		return "Factual knowledge (MMLU)"
	case ModeRagasBench:
		return "RAG faithfulness (synthetic)"
	case ModeSummaryBench:
		return "Summarization coherence"
	case ModeTerminalBench:
		return "Agentic terminal tasks (Terminal-Bench)"
	case ModeSweBenchPro:
		return "Agentic SWE tasks (SWE-bench Pro)"
	case ModeDeepSWE:
		return "Agentic SWE tasks (DeepSWE)"
	default:
		return string(m)
	}
}

// ProfileSnapshot captures the quantization-relevant config of the profile at
// run time so comparisons make explicit what changed between runs.
type ProfileSnapshot struct {
	Model        string            `json:"model"`
	Quantization string            `json:"quantization,omitempty"` // best-effort from model filename
	CacheTypeK   string            `json:"cacheTypeK,omitempty"`
	CacheTypeV   string            `json:"cacheTypeV,omitempty"`
	CtxSize      int               `json:"ctxSize,omitempty"`
	KeyArgs      map[string]string `json:"keyArgs,omitempty"`
}

// ProblemResult is the outcome for a single problem in a run.
type ProblemResult struct {
	ServerTimingSamples []*ServerTimings `json:"serverTimingSamples,omitempty"`
	ServerTimings       *ServerTimings   `json:"serverTimings,omitempty"`
	ProblemID           string           `json:"problemId"`
	ProblemName         string           `json:"problemName"`
	Resolved            bool             `json:"resolved"`
	Score               float64          `json:"score"` // 0..1
	// Timing metrics (TTFTms, TotalMs, *TPS) traverse the loopback reverse
	// proxy (sub-ms overhead); runs persisted before the proxy migration are
	// not strictly comparable.
	TTFTms              int64   `json:"ttftMs"`
	TotalMs             int64   `json:"totalMs"`
	TokensPerSecond     float64 `json:"tokensPerSecond"`
	PromptProcessingTPS float64 `json:"promptProcessingTps,omitempty"`
	DecodeTPS           float64 `json:"decodeTps,omitempty"`
	PromptTokens        int     `json:"promptTokens"`
	CompletionTokens    int     `json:"completionTokens"`
	Detail              string  `json:"detail,omitempty"`
	Err                 string  `json:"err,omitempty"`
	// FailPhase attributes a failed item to a stage (T5): "infer" (model under
	// test), "score" (judge/grader), or "harness" (external agentic harness).
	// Empty when Err is empty. Additive/optional so old runs decode cleanly.
	FailPhase string `json:"failPhase,omitempty"`

	// Structured per-mode fields. All optional and additive so persisted runs
	// from older versions decode cleanly; Detail keeps the human-readable text.
	Kind       string             `json:"kind,omitempty"`       // instruction: "format" | "refusal" | "consistency"
	Category   string             `json:"category,omitempty"`   // mmlu: STEM / Humanities / Social Sciences / Other
	Difficulty int                `json:"difficulty,omitempty"` // math: GSM8K reasoning-step band 1..3
	SubScores  map[string]float64 `json:"subScores,omitempty"`  // ragas/summary/consistency sub-criteria
	JudgedBy   string             `json:"judgedBy,omitempty"`   // "external" | "self" | "heuristic"
	SimMethod  string             `json:"simMethod,omitempty"`  // consistency: "embeddings" | "lexical"
	Seed       int64              `json:"seed,omitempty"`       // longctx: needle randomization seed
	TPSStdDev  float64            `json:"tpsStdDev,omitempty"`  // llama-bench: tok/s spread across reps
	TPSMin     float64            `json:"tpsMin,omitempty"`
	TPSMax     float64            `json:"tpsMax,omitempty"`
	FillPct    int                `json:"fillPct,omitempty"` // llama-bench: context fill percent for this preset
	Sandbox    string             `json:"sandbox,omitempty"` // codegen: "bwrap" | "subprocess"
}

// Aggregate is the run-level rollup across all problems.
type Aggregate struct {
	GPUMetricVersion       int               `json:"gpuMetricVersion,omitempty"`
	GPUPeakVRAMMB          map[string]uint64 `json:"gpuPeakVramMb,omitempty"`
	Total                  int               `json:"total"`
	Errored                int               `json:"errored,omitempty"` // problems with a request/judge error, excluded from quality rates
	Resolved               int               `json:"resolved"`
	SolveRate              float64           `json:"solveRate"` // 0..1
	AvgScore               float64           `json:"avgScore"`
	AvgTokensPerSecond     float64           `json:"avgTokensPerSecond"`
	AvgPromptProcessingTPS float64           `json:"avgPromptProcessingTps,omitempty"` // avg prefill tok/s
	AvgDecodeTPS           float64           `json:"avgDecodeTps,omitempty"`           // avg decode tok/s
	MathAccuracy           float64           `json:"mathAccuracy,omitempty"`           // 0..1, exact-match rate
	CodePassRate           float64           `json:"codePassRate,omitempty"`           // 0..1 over executed problems
	InstFormatRate         float64           `json:"instFormatRate,omitempty"`         // 0..1 structured-format adherence
	InstRefusalRate        float64           `json:"instRefusalRate,omitempty"`        // 0..1 disallowed-request refusal
	InstConsistency        float64           `json:"instConsistency,omitempty"`        // 0..1 mean pairwise similarity
	MMLUAccuracy           float64           `json:"mmluAccuracy,omitempty"`           // 0..1 multiple-choice exact-match rate
	RagasFaithfulness      float64           `json:"ragasFaithfulness,omitempty"`      // 0..1 grounded-in-docs score
	RagasRelevancy         float64           `json:"ragasRelevancy,omitempty"`         // 0..1 answers-the-question score
	RagasPrecision         float64           `json:"ragasPrecision,omitempty"`         // 0..1 uses-the-right-context score
	SummaryCoherence       float64           `json:"summaryCoherence,omitempty"`       // 0..1 multi-doc coherence + fact coverage
	TerminalBenchAccuracy  float64           `json:"terminalBenchAccuracy,omitempty"`  // 0..1 resolved-task rate from the Terminal-Bench harness
	SweBenchProAccuracy    float64           `json:"sweBenchProAccuracy,omitempty"`    // 0..1 resolved-instance rate from the SWE-bench Pro harness
	DeepSWEAccuracy        float64           `json:"deepSweAccuracy,omitempty"`        // 0..1 resolved-task rate from the DeepSWE (Pier) harness
	AvgTTFTms              float64           `json:"avgTtftMs"`
	TotalPromptTokens      int               `json:"totalPromptTokens"`
	TotalCompletionTokens  int               `json:"totalCompletionTokens"`
	TotalMs                int64             `json:"totalMs"`
	PeakVRAMMB             uint64            `json:"peakVramMb"`
	AvgGPUUtil             float64           `json:"avgGpuUtil"`
}

// ProblemTranscript captures the raw I/O for one problem for debugging. It is
// persisted to a separate <run-id>.transcript.json file (never inside the run
// JSON, which feeds the list/compare views).
type ProblemTranscript struct {
	ProblemID     string   `json:"problemId"`
	ProblemName   string   `json:"problemName"`
	ModelResponse string   `json:"modelResponse"`      // full raw completion from the model under test
	DiffFound     bool     `json:"diffFound"`          // did a unified diff extract from the response?
	JudgeRaw      []string `json:"judgeRaw,omitempty"` // full raw judge replies (one per sample)
	Error         string   `json:"error,omitempty"`    // request/parse error, if any
}

// Run is one full benchmark execution against one profile. Persisted as a
// single JSON file by benchmarkstore.
type Run struct {
	ID          string          `json:"id"`
	ProfileID   string          `json:"profileId"`
	ProfileName string          `json:"profileName"`
	Mode        Mode            `json:"mode"`
	StartedAt   time.Time       `json:"startedAt"`
	FinishedAt  time.Time       `json:"finishedAt"`
	Profile     ProfileSnapshot `json:"profile"`
	Problems    []ProblemResult `json:"problems"`
	Aggregate   Aggregate       `json:"aggregate"`
	Err         string          `json:"err,omitempty"`
	// ReusedInstance is true when the profile was already loaded in the proxy
	// when the run started (warm backend, possibly serving other traffic)
	// instead of being swapped in fresh — performance numbers may be affected.
	ReusedInstance bool `json:"reusedInstance,omitempty"`

	// Transcript holds raw per-problem I/O for debugging. Excluded from the run
	// JSON (json:"-"); benchmarkstore writes it to a separate file.
	Transcript []ProblemTranscript `json:"-"`
}

// aggregate rolls the per-problem results up into the run-level Aggregate,
// folding in the GPU sampler's peak VRAM and average utilization. Problems
// that errored count in Errored and are excluded from the quality rates.
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

// effectiveCtxTokens returns the profile's context window in tokens, reading
// the first context arg present: ctx-size (llama/beellama), max-ctx (dflash),
// max-model-len (vllm), context-length (sglang), max-context (strata).
// 0 → unknown (caller falls back).
func effectiveCtxTokens(p domain.Profile) int {
	for _, k := range []string{"ctx-size", "max-ctx", "max-model-len", "context-length", "max-context"} {
		if n := parseCtxTokens(argString(p.Args, k)); n > 0 {
			return n
		}
	}
	return 0
}

// parseCtxTokens parses a plain int, vLLM k/m shorthand ("131072","128k","1m"),
// or a float string. Numeric profile args decode from JSON as float64 and
// argString renders large values (≥1e6) in scientific notation ("1.048576e+06"),
// so an Atoi-only parse would silently fail on million-token contexts; the
// ParseFloat fallback keeps those parseable.
func parseCtxTokens(s string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0
	}
	mult := 1
	switch {
	case strings.HasSuffix(s, "k"):
		mult = 1024
		s = strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		mult = 1024 * 1024
		s = strings.TrimSuffix(s, "m")
	}
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 {
			return 0
		}
		return n * mult
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0
	}
	return int(f) * mult
}

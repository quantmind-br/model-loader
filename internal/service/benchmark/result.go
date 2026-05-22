package benchmark

import "time"

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
	ProblemID        string  `json:"problemId"`
	ProblemName      string  `json:"problemName"`
	Resolved         bool    `json:"resolved"`
	Score            float64 `json:"score"` // 0..1
	TTFTms           int64   `json:"ttftMs"`
	TotalMs          int64   `json:"totalMs"`
	TokensPerSecond     float64 `json:"tokensPerSecond"`
	PromptProcessingTPS float64 `json:"promptProcessingTps,omitempty"`
	DecodeTPS           float64 `json:"decodeTps,omitempty"`
	PromptTokens        int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
	Detail           string  `json:"detail,omitempty"`
	Err              string  `json:"err,omitempty"`
}

// Aggregate is the run-level rollup across all problems.
type Aggregate struct {
	Total                 int     `json:"total"`
	Resolved              int     `json:"resolved"`
	SolveRate             float64 `json:"solveRate"` // 0..1
	AvgScore              float64 `json:"avgScore"`
	AvgTokensPerSecond    float64 `json:"avgTokensPerSecond"`
	PromptProcessingTPS   float64 `json:"promptProcessingTps,omitempty"` // avg prefill tok/s
	DecodeTPS             float64 `json:"decodeTps,omitempty"`           // avg decode tok/s
	AvgTTFTms             float64 `json:"avgTtftMs"`
	TotalPromptTokens     int     `json:"totalPromptTokens"`
	TotalCompletionTokens int     `json:"totalCompletionTokens"`
	TotalMs               int64   `json:"totalMs"`
	PeakVRAMMB            uint64  `json:"peakVramMb"`
	AvgGPUUtil            float64 `json:"avgGpuUtil"`
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

	// Transcript holds raw per-problem I/O for debugging. Excluded from the run
	// JSON (json:"-"); benchmarkstore writes it to a separate file.
	Transcript []ProblemTranscript `json:"-"`
}

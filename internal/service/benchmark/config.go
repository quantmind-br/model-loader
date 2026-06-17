package benchmark

import "time"

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

// JudgeEndpoint is the OpenAI-compatible endpoint used by ModeJudge.
type JudgeEndpoint struct {
	BaseURL string
	APIKey  string
	Model   string
	// Samples is how many times the judge grades each problem; the run uses the
	// median score + majority resolved (self-consistency) to fight noise.
	Samples int
}

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

	// --- Terminal-Bench (agentic) mode (ModeTerminalBench) ---
	// These configure the external `tb` harness wrapper. The engine never
	// installs tb or Docker; it only shells out to an already-installed CLI.

	// TerminalBenchCmd overrides the tb CLI binary (name on PATH or absolute
	// path). Empty → "tb".
	TerminalBenchCmd string
	// TerminalBenchAgent is the tb agent to drive the terminal. Empty → "terminus".
	TerminalBenchAgent string
	// TerminalBenchDataset is the tb dataset ('name' or 'name==version'). Empty →
	// "terminal-bench-core==0.1.1" (pinned for reproducible, comparable scores).
	TerminalBenchDataset string
	// TerminalBenchProvider is the LiteLLM provider prefix prepended to the
	// profile id to form tb's --model arg. Empty → "openai" (→ openai/<profile-id>),
	// which routes LiteLLM at the proxy as an OpenAI-compatible endpoint.
	TerminalBenchProvider string
	// TerminalBenchTasks limits the run to specific task ids / glob patterns
	// (tb --task-id). Empty → the whole dataset.
	TerminalBenchTasks []string
	// TerminalBenchNTasks caps the number of tasks (tb --n-tasks); 0 → omit.
	TerminalBenchNTasks int
	// TerminalBenchConcurrent is tb --n-concurrent. <=0 → 1 (one trial at a time,
	// fitting the single-GPU rig: many concurrent trials would hammer one backend).
	TerminalBenchConcurrent int
	// TerminalBenchTimeout bounds the whole tb run from the model-loader side;
	// 0 → no cap here (tb still enforces its own per-agent/per-test timeouts).
	TerminalBenchTimeout time.Duration
	// TerminalBenchExtraArgs are passed through verbatim after the built flags
	// (e.g. "--no-rebuild", "--no-cleanup", "--global-agent-timeout-sec", "600").
	TerminalBenchExtraArgs []string
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

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

	// --- SWE-bench Pro (agentic) mode (ModeSweBenchPro) ---
	// These configure the external SWE-bench Pro harness (scaleapi/SWE-bench_Pro-os).
	// The engine never installs the harness, Docker, or the patch-generation agent;
	// it only shells out to an already-cloned harness with python + Docker present.

	// SweBenchProHarnessDir is the path to a cloned SWE-bench_Pro-os checkout
	// (contains swe_bench_pro_eval.py, helper_code/, run_scripts/, dockerfiles/).
	// Required for this mode.
	SweBenchProHarnessDir string
	// SweBenchProRawSample is the --raw_sample_path (CSV/JSONL of instances).
	// Required: it must carry lowercase fail_to_pass/pass_to_pass columns. The
	// harness's own helper_code/sweap_eval_full_v2.jsonl uses UPPERCASE keys, which
	// the eval misreads and silently scores every instance False — so there is no
	// default (see docs/swe-bench-pro.md).
	SweBenchProRawSample string
	// SweBenchProScriptsDir is the --scripts_dir (per-instance run_script.sh +
	// parser.py). Empty → <harness>/run_scripts.
	SweBenchProScriptsDir string
	// SweBenchProDockerhubUser is the --dockerhub_username whose sweap-images repo
	// holds the per-instance images. Empty → "jefzda" (the official prebuilt set).
	SweBenchProDockerhubUser string
	// SweBenchProPython is the interpreter used to run the harness scripts. Empty →
	// "python3".
	SweBenchProPython string
	// SweBenchProNumWorkers is the eval --num_workers. <=0 → 4 (conservative for a
	// single workstation; the harness default 50 targets Modal cloud sandboxes).
	SweBenchProNumWorkers int
	// SweBenchProUseModal selects the Modal cloud backend instead of local Docker.
	// false (default) → pass --use_local_docker, evaluating on this machine.
	SweBenchProUseModal bool
	// SweBenchProInstances limits the run to specific instance_ids (the eval has no
	// instance flag, so the gathered patch set is filtered to these). Empty → every
	// instance present in the patch set.
	SweBenchProInstances []string
	// SweBenchProPatchPath supplies patches without running an agent: a consolidated
	// patches JSON (used directly) or a directory of instance_*/*.pred files (run
	// through gather_patches.py). Takes precedence over SweBenchProAgentCmd: when
	// set, the agent is skipped. Empty → SweBenchProAgentCmd must be set.
	SweBenchProPatchPath string
	// SweBenchProAgentCmd is an optional patch-generation command (phase 1). The
	// placeholders {model}, {api_base}, {output}, {instances}, {harness} are
	// substituted before exec; the command must write instance_*/*.pred under
	// {output}. Used only when SweBenchProPatchPath is empty (patch_path wins).
	SweBenchProAgentCmd []string
	// SweBenchProTimeout bounds the whole pipeline (agent + gather + eval) from the
	// model-loader side; 0 → no cap here.
	SweBenchProTimeout time.Duration
	// SweBenchProExtraArgs are passed through verbatim to swe_bench_pro_eval.py
	// after the built flags (e.g. "--block_network", "--redo").
	SweBenchProExtraArgs []string
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

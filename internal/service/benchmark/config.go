package benchmark

import (
	"log/slog"
	"time"
)

// Config tunes the engine. main.go maps config.BenchmarkConfig onto it so the
// benchmark package stays decoupled from the config package.
type Config struct {
	// Logger receives structured per-phase/per-item diagnostics (T1). nil →
	// log.Nop() (NewRunner supplies the fallback). Set at the call site from
	// the app logger since the config→Config mapping has no logger of its own.
	Logger    *slog.Logger
	MaxTokens int
	// Limit caps how many items a reducible mode runs (problems for the dataset
	// modes, presets for llama-bench). <=0 → no cap (full set). A uniform
	// "reduced run" knob for fast smoke/validation; modes with their own
	// reducers (terminal-bench --n-tasks, swe-bench-pro instance filter) and the
	// single-probe longctx ignore it.
	Limit             int
	Temperature       float64
	Timeout           time.Duration // per-problem inference timeout
	LongContextTokens int           // target prompt size for the needle probe (0 → default)
	SaveTranscripts   bool          // capture raw model/judge I/O for debugging
	// HarnessLogDir, when set, receives one <mode>-<timestamp>.log per agentic
	// run with the external harness's full merged stdout/stderr (teed live), so
	// hours of output survive a model-loader crash and RAM stays bounded (BR7).
	// Empty → bounded in-memory tail only.
	HarnessLogDir string
	// UnloadAfterRun frees the model from the proxy (calls /_admin/unload) when a
	// run finishes — success, partial, or cancelled. Default false keeps the
	// warm-model behavior (the proxy owns backend lifecycle); enable it so a long
	// agentic run does not leave VRAM pinned after it completes.
	UnloadAfterRun bool
	Judge          JudgeEndpoint
	// EmbeddingsBaseURL optionally overrides where similarity graders fetch
	// embeddings. Empty → reuse the model-under-test server. Reserved for the
	// instruction-consistency mode (added in a later plan).
	EmbeddingsBaseURL string

	// LlamaBenchPresets are "<fill>%/<tg>" strings (context fill percent /
	// generation tokens) for ModeLlamaBench. Empty → default
	// {5%/256, 25%/256, 50%/256, 90%/128}.
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
	// TerminalBenchStallTimeout group-kills the tb harness when no new task has
	// been scored for this long — a wedged agent/Docker task that would otherwise
	// hang forever, pinning the GPU. 0 → built-in tbDefaultStallTimeout.
	TerminalBenchStallTimeout time.Duration

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

	// --- DeepSWE (agentic) mode (ModeDeepSWE) ---
	// These configure the external `pier` CLI (datacurve-ai/pier) running the
	// DeepSWE task corpus (datacurve-ai/deep-swe). The engine never installs
	// pier, Docker, or the task corpus; it only shells out to an already-installed
	// `pier` against an already-cloned corpus.

	// DeepSWECmd overrides the pier CLI binary (name on PATH or absolute path).
	// Empty → "pier".
	DeepSWECmd string
	// DeepSWETasksDir is the path to a cloned DeepSWE tasks directory (the
	// `tasks/` subdir of datacurve-ai/deep-swe). Required for this mode.
	DeepSWETasksDir string
	// DeepSWEAgent is the pier agent that solves each task. Empty → "mini-swe-agent".
	DeepSWEAgent string
	// DeepSWEProvider is the LiteLLM provider prefix prepended to the profile id
	// to form pier's --model arg. Empty → "openai" (→ openai/<profile-id>), which
	// LiteLLM routes at the proxy as an OpenAI-compatible endpoint.
	DeepSWEProvider string
	// DeepSWEModelClass overrides mini-swe-agent's model adapter (pier
	// --agent-kwarg model_class=…). Empty → "litellm", forcing LiteLLM chat
	// completions; without it an openai/<id> model selects mini-swe-agent's
	// Responses-API adapter, which llama-server does not implement.
	DeepSWEModelClass string
	// DeepSWEAPIBase overrides the api_base the in-sandbox agent calls. Empty →
	// derived from the proxy root (<proxy>/v1). Because the agent runs inside a
	// Docker container, 127.0.0.1 there is the container itself: set this to a
	// host-reachable address (e.g. http://host.docker.internal:4321/v1) and bind
	// the proxy accordingly. See docs/deep-swe.md.
	DeepSWEAPIBase string
	// DeepSWETasks limits the run to specific task ids / glob patterns (pier
	// --include-task-name). Empty → the whole corpus.
	DeepSWETasks []string
	// DeepSWENTasks caps the number of tasks (pier --n-tasks); 0 → omit. Paired
	// with DeepSWESampleSeed for a deterministic subset.
	DeepSWENTasks int
	// DeepSWESampleSeed is pier --sample-seed for deterministic subset selection
	// (applied before --n-tasks). Only emitted when DeepSWENTasks > 0.
	DeepSWESampleSeed int
	// DeepSWEConcurrent is pier --n-concurrent. <=0 → 1 (one trial at a time,
	// fitting the single-GPU rig: concurrent trials would hammer one backend).
	DeepSWEConcurrent int
	// DeepSWETimeout bounds the whole pier run from the model-loader side; 0 → no
	// cap here (pier still enforces its own per-task timeouts from task.toml).
	DeepSWETimeout time.Duration
	// DeepSWEStallTimeout group-kills the pier harness when no new task is scored
	// for this long (a wedged agent/Docker task). 0 → built-in tbDefaultStallTimeout.
	DeepSWEStallTimeout time.Duration
	// DeepSWEExtraArgs are passed through verbatim after the built flags (e.g.
	// "--force-build", "--ae", "HTTP_PROXY=…").
	DeepSWEExtraArgs []string
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

// Package dflashhelp provides a static schema for the DFlash backend.
// DFlash is the Lucebox speculative-decoding runtime (lucebox-hub): a native
// C++ OpenAI-compatible HTTP server (dflash_server) built from server/.
// Its flags are hand-curated here from server/src/server/server_main.cpp
// because the binary's --help output is not parsed at runtime.
package dflashhelp

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

// EmbeddedSchema returns a FlagSchema covering the dflash_server arguments.
// The target model is passed as the first positional argument (mapped from
// the profile's Model field by processmgr.buildDFlashArgs); the draft model
// is the --draft flag below.
//
// Schema tracks lucebox-hub commit b8c3a0d (2026-08-09).
func EmbeddedSchema() domain.FlagSchema {
	return domain.BuildFlagSchema("embedded-dflash-v5", dflashRows)
}

var kvTypes = []string{"f16", "bf16", "q4_0", "q4_1", "q5_0", "q5_1", "q8_0", "tq3_0"}

var dflashRows = []domain.FlagSpecRow{
	// Core
	{Long: "draft", Type: domain.FlagTypeString, Default: "", HelpText: "Path to the DFlash draft GGUF, required for speculative decode; support varies by target architecture", Group: "common"},
	{Long: "host", Type: domain.FlagTypeString, Default: "0.0.0.0", HelpText: "Server bind address", Group: "common"},
	{Long: "port", Type: domain.FlagTypeInt, Default: float64(8080), IsPort: true, HelpText: "HTTP listen port", Group: "common"},
	{Long: "max-ctx", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1024 * 1024), HelpText: "Max context length / KV cache size; 0 = backend default. Oversizing slows prefill (FA stride over unused KV)", Group: "common"},
	{Long: "max-tokens", Type: domain.FlagTypeInt, Default: float64(4096), Min: ptrutil.Ptr(0), HelpText: "Legacy alias for --default-max-tokens; loses to it when both are passed", Group: "common"},
	{Long: "default-max-tokens", Type: domain.FlagTypeInt, Default: float64(16000), Min: ptrutil.Ptr(0), HelpText: "Combined output cap when the request omits max_tokens; may be raised by the model card", Group: "common"},
	{Long: "model-name", Type: domain.FlagTypeString, Default: "dflash", HelpText: "Model name reported by /v1/models (OpenAI model field)", Group: "common"},
	{Long: "chat-template-file", Type: domain.FlagTypeString, Default: "", HelpText: "Jinja chat template file overriding the hardcoded renderer; empty or missing falls back", Group: "common"},
	{Long: "prefix-cache-slots", Type: domain.FlagTypeInt, Default: float64(32), Min: ptrutil.Ptr(0), HelpText: "Live prefix-cache slot count (0 disables)", Group: "common"},
	{Long: "prefill-cache-slots", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), HelpText: "Full prompt/prefill cache slot count (0 disables); distinct from --prefix-cache-slots", Group: "common"},
	{Long: "chunk", Type: domain.FlagTypeInt, Default: float64(512), Min: ptrutil.Ptr(1), HelpText: "Chunked-prefill chunk (ubatch) size", Group: "common"},
	{Long: "fa-window", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1024 * 1024), HelpText: "Flash-attention sliding window; 0 = full attention (qwen3.6 full-attn layers need the whole context for tool calls)", Group: "common"},
	{Long: "paged-attention", Type: domain.FlagTypeBool, Default: false, HelpText: "Experimental: 16-token paged KV blocks for autoregressive decode on a monolithic Qwen3.5/Qwen3.6 dense target (arch qwen35). Requires one local target device, a positive --max-ctx and --fa-window 0; rejected together with --draft, --ddtree or PFlash compression, and it turns off the prefix/prefill snapshot caches and the disk KV cache", Group: "common"},
	{Long: "no-cors", Type: domain.FlagTypeBool, Default: false, HelpText: "Disable CORS headers", Group: "common"},

	// Speculative decode (DFlash + DDTree)
	{Long: "ddtree", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable DDTree tree-verify speculative decode (default: chain verify)", Group: "speculative-decode"},
	{Long: "ddtree-budget", Type: domain.FlagTypeInt, Default: float64(22), Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(512), HelpText: "DDTree token budget per step (22 on RTX 3090, 40 on RTX 5090; re-sweep per card)", Group: "speculative-decode"},
	{Long: "verify-width", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), HelpText: "Laguna chain speculative-verify width; 0 = adaptive (per-step width trimmed by drafter confidence off a base of 8)", Group: "speculative-decode"},
	{Long: "adaptive-experts", Type: domain.FlagTypeFloat, FloatMin: ptrutil.Ptr(0.01), FloatMax: ptrutil.Ptr(1.0), HelpText: "MoE expert-count gating on verify batches (near-lossless); tau in (0,1], 0.80 when passed bare on the real CLI", Group: "speculative-decode"},
	{Long: "fast-rollback", Type: domain.FlagTypeBool, Default: true, HelpText: "Speculative fast rollback (on by default; --ddtree also enables it). Set --no-fast-rollback to disable", Group: "speculative-decode"},
	{Long: "no-fast-rollback", Type: domain.FlagTypeBool, Default: false, HelpText: "Disable speculative fast rollback, even with --ddtree (overrides the on-by-default --fast-rollback)", Group: "speculative-decode"},
	{Long: "draft-swa", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), HelpText: "Draft sliding-window attention size (0 = off; e.g. 2048 for unsloth Qwen3.6 targets)", Group: "speculative-decode"},
	{Long: "draft-residency", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "persistent", "request-scoped"}, Default: "auto", HelpText: "Draft weights VRAM lifetime: request-scoped frees them after each request, persistent keeps them resident, auto honors the low-VRAM hint", Group: "speculative-decode"},
	{Long: "lazy-draft", Type: domain.FlagTypeBool, Default: false, HelpText: "Legacy alias for --draft-residency=request-scoped", Group: "speculative-decode"},

	// KV cache
	{Long: "cache-type-k", Short: "ctk", Type: domain.FlagTypeEnum, EnumValues: kvTypes, HelpText: "KV cache type for keys; unset = per model family default (laguna: q8_0, else q4_0; HIP builds: always q4_0)", Group: "kv-cache"},
	{Long: "cache-type-v", Short: "ctv", Type: domain.FlagTypeEnum, EnumValues: kvTypes, HelpText: "KV cache type for values; unset = per model family default (laguna: q8_0, else q4_0; HIP builds: always q4_0)", Group: "kv-cache"},
	{Long: "kv-cache-dir", Type: domain.FlagTypeString, Default: "", HelpText: "Directory for the on-disk KV prefix cache (enables the feature)", Group: "kv-cache"},
	{Long: "kv-cache-budget", Type: domain.FlagTypeInt, Default: float64(4096), Min: ptrutil.Ptr(0), HelpText: "Max on-disk KV cache size in MB", Group: "kv-cache"},
	{Long: "kv-cache-min-tokens", Type: domain.FlagTypeInt, Default: float64(512), Min: ptrutil.Ptr(0), HelpText: "Min tokens before a prefix is persisted to disk", Group: "kv-cache"},
	{Long: "kv-cache-interval", Type: domain.FlagTypeInt, Default: float64(10240), Min: ptrutil.Ptr(0), HelpText: "Continued checkpoint every N tokens", Group: "kv-cache"},
	{Long: "kv-cache-cold-max", Type: domain.FlagTypeInt, Default: float64(10240), Min: ptrutil.Ptr(0), HelpText: "Cold prefix size for prompts longer than N tokens", Group: "kv-cache"},
	{Long: "disk-prefix-cache", Type: domain.FlagTypeString, Default: "full", HelpText: "Disk prefix-cache policy: off, full, auto, auto:N (window of N requests), or a positive token count", Group: "kv-cache"},
	{Long: "disk-prefix-cache-compress", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable FlowKV aged-history compression composed with the disk prefix cache; requires --prefill-drafter (off is byte-identical to the base disk cache)", Group: "kv-cache"},

	// KVFlash (bounded KV residency)
	{Long: "kvflash", Type: domain.FlagTypeString, Default: "", HelpText: "Bounded KV residency: keep attention KV in a fixed pool of N tokens (or 'auto'); cold 64-token chunks page to host. Works with or without PFlash; forces AR decode. Empty = off", Group: "kv-cache"},
	{Long: "kvflash-policy", Type: domain.FlagTypeEnum, EnumValues: []string{"drafter", "lru", "qk"}, HelpText: "KVFlash residency scorer: drafter scores the keep-set with the loaded drafter, lru evicts least-recently-used, qk scores pooled post-RoPE keys against the query at reselect (no drafter). Unset = drafter when a drafter is loaded, else lru", Group: "kv-cache"},
	{Long: "kvflash-tau", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), HelpText: "KVFlash keep-set reselect interval in decode steps (positive)", Group: "kv-cache"},

	// Prefill compression (PFlash)
	{Long: "prefill-compression", Type: domain.FlagTypeEnum, EnumValues: []string{"off", "auto", "always"}, Default: "off", HelpText: "When to score and compress the prompt (speculative prefill)", Group: "prefill-compression"},
	{Long: "prefill-threshold", Type: domain.FlagTypeInt, Default: float64(32000), Min: ptrutil.Ptr(0), HelpText: "Token threshold for auto prefill compression", Group: "prefill-compression"},
	{Long: "prefill-keep-ratio", Type: domain.FlagTypeFloat, Default: float64(0.05), FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(1.0), HelpText: "Fraction of source tokens kept (0.02 @128K, 0.10 @32K)", Group: "prefill-compression"},
	{Long: "prefill-curve", Type: domain.FlagTypeString, Default: "", HelpText: "Piecewise keep-ratio breakpoint TOKENS:RATIO, e.g. 40000:0.2; overrides --prefill-keep-ratio. The server takes multiple space-separated breakpoints — pass extras via extra args", Group: "prefill-compression"},
	{Long: "prefill-drafter", Type: domain.FlagTypeString, Default: "", HelpText: "Drafter GGUF for prefill compression (Qwen3-0.6B BF16); required when compression is on", Group: "prefill-compression"},
	{Long: "prefill-skip-park", Type: domain.FlagTypeBool, Default: false, HelpText: "Keep the prefill drafter resident across requests (more VRAM, faster; for >=32GB GPUs)", Group: "prefill-compression"},
	{Long: "prefill-upstream-base", Type: domain.FlagTypeString, Default: "", HelpText: "OpenAI-compatible upstream base URL; compressed prompts are forwarded to <URL>/v1/completions", Group: "prefill-compression"},
	{Long: "prefill-upstream-key", Type: domain.FlagTypeString, Default: "", HelpText: "Bearer token for the prefill upstream", Group: "prefill-compression"},
	{Long: "prefill-upstream-model", Type: domain.FlagTypeString, Default: "", HelpText: "Model name on forwarded upstream requests", Group: "prefill-compression"},

	// Thinking budget
	{Long: "think-max-tokens", Type: domain.FlagTypeInt, Default: float64(15488), Min: ptrutil.Ptr(0), HelpText: "Phase-1 reasoning cap when a request opts in via thinking:{type:enabled}; may be raised by the model card", Group: "thinking-budget"},
	{Long: "hard-limit-reply-budget", Type: domain.FlagTypeInt, Default: float64(4096), Min: ptrutil.Ptr(0), HelpText: "Force-close: when this many tokens remain of the combined cap, inject </think> so the visible answer fits (0 disables)", Group: "thinking-budget"},
	{Long: "reasoning-effort-low", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(0), HelpText: "Phase-1 budget for requests asking effort=low (default from the model card)", Group: "thinking-budget"},
	{Long: "reasoning-effort-medium", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(0), HelpText: "Phase-1 budget for requests asking effort=medium (default from the model card)", Group: "thinking-budget"},
	{Long: "reasoning-effort-high", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(0), HelpText: "Phase-1 budget for requests asking effort=high (default from the model card)", Group: "thinking-budget"},
	{Long: "reasoning-effort-x-high", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(0), HelpText: "Phase-1 budget for requests asking effort=x-high (default from the model card)", Group: "thinking-budget"},
	{Long: "reasoning-effort-max", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(0), HelpText: "Phase-1 budget for requests asking effort=max (default from the model card)", Group: "thinking-budget"},

	// Multi-GPU / placement / IPC
	{Long: "target-device", Type: domain.FlagTypeString, Default: "auto:0", HelpText: "Target device as backend:gpu (e.g. cuda:0, hip:0); conflicts with --target-devices", Group: "multi-gpu"},
	{Long: "draft-device", Type: domain.FlagTypeString, Default: "auto:0", HelpText: "Draft device as backend:gpu; mixed target/draft backends require --draft-ipc-bin", Group: "multi-gpu"},
	{Long: "target-devices", Type: domain.FlagTypeString, Default: "", HelpText: "Comma-separated target devices, e.g. cuda:0,cuda:1; used for layer-split (default) or tensor-parallel via --target-split-mode; conflicts with --target-device", Group: "multi-gpu"},
	{Long: "target-split-mode", Type: domain.FlagTypeEnum, EnumValues: []string{"layer", "tensor"}, Default: "layer", HelpText: "Multi-GPU split mode for --target-devices: layer-split or tensor-parallel", Group: "multi-gpu"},
	{Long: "target-layer-split", Type: domain.FlagTypeString, Default: "", HelpText: "Comma-separated layer-split weights (requires --target-devices)", Group: "multi-gpu"},
	{Long: "target-split-fast-rollback", Type: domain.FlagTypeBool, Default: false, HelpText: "Opt in to exact F32 rollback checkpoints for local same-backend qwen35 target layer splits (extra VRAM; env DFLASH_SPLIT_FAST_ROLLBACK=1); requires --target-devices with 2+ local devices", Group: "multi-gpu"},
	{Long: "peer-access", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable P2P peer access between target GPUs", Group: "multi-gpu"},
	{Long: "draft-ipc-bin", Type: domain.FlagTypeString, Default: "", HelpText: "Out-of-process draft backend IPC daemon (mixed CUDA/HIP placement)", Group: "multi-gpu"},
	{Long: "draft-ipc-work-dir", Type: domain.FlagTypeString, Default: "", HelpText: "Remote draft IPC scratch directory (requires --draft-ipc-bin)", Group: "multi-gpu"},
	{Long: "draft-ipc-ring-cap", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), HelpText: "Remote draft feature ring capacity (requires --draft-ipc-bin)", Group: "multi-gpu"},
	{Long: "target-shard-ipc-bin", Type: domain.FlagTypeString, Default: "", HelpText: "Remote target shard IPC daemon for mixed-backend target layer split", Group: "multi-gpu"},
	{Long: "target-shard-ipc-work-dir", Type: domain.FlagTypeString, Default: "", HelpText: "Remote target shard IPC scratch directory (requires --target-shard-ipc-bin)", Group: "multi-gpu"},

	// MoE expert offload (Spark)
	{Long: "spark", Type: domain.FlagTypeBool, Default: false, HelpText: "Self-tuning MoE expert offload: bounded GPU expert cache sized from the VRAM target plus a persisted placement profile", Group: "moe-offload"},
	{Long: "spark-slots", Type: domain.FlagTypeInt, Default: float64(-1), Min: ptrutil.Ptr(-1), HelpText: "Explicit expert-cache slots per layer (-1 = auto)", Group: "moe-offload"},
	{Long: "spark-vram", Type: domain.FlagTypeFloat, Default: float64(0), FloatMin: ptrutil.Ptr(0.0), HelpText: "Total VRAM Spark may use in GiB; it sizes hot tier + cache + KV under this cap (0 = whole card)", Group: "moe-offload"},

	// Expert routing analysis (MoE backends only)
	{Long: "freq", Type: domain.FlagTypeBool, Default: false, HelpText: "Track MoE expert frequency and print routing analysis at shutdown", Group: "moe-offload"},
	{Long: "collect-routing", Type: domain.FlagTypeString, Default: "", HelpText: "Log binary routing data (hidden states + expert IDs) to this path for MLP expert-predictor training (scripts/train_predictor.py)", Group: "moe-offload"},

	// DeepSeek4 (single-device only)
	{Long: "ds4-fused-decode", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable DeepSeek4 single-graph GPU decode", Group: "ds4"},
	{Long: "ds4-expert-top-k", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), HelpText: "Keep and renormalize the top-N routed experts (0 = model default; single-device DeepSeek4 only)", Group: "ds4"},
	{Long: "ds4-prefill", Type: domain.FlagTypeEnum, EnumValues: []string{"exact", "dense", "sparse"}, Default: "exact", HelpText: "DeepSeek4 prefill attention mode; dense/sparse are experimental and may change generated tokens", Group: "ds4"},
}

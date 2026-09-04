// Package freetokenhelp provides a static, curated schema for the freetoken
// backend (FlashML's FreeToken, an edge-native MoE serving engine with OpenAI-,
// Anthropic- and Responses-compatible APIs). `ft serve --help` is produced by
// an argparse tree whose option list is stable but whose help text is
// multi-paragraph prose that does not survive the generic help parser, so the
// schema below is curated from the argparse declarations instead.
//
// Tracks the vendored checkout backends/freetoken at freetoken 0.1.2 (the
// pinned PyPI release its backend-build.sh installs). Every row was verified
// against that venv's live argparse tree — option strings, action class,
// default and choices — not against the project's git main, which already
// carries options 0.1.2 does not have. Two of them matter here and are
// deliberately absent:
//
//   - --gpu (per-TP-rank GPU selection by nvidia-smi index or UUID). On 0.1.2
//     rank i binds CUDA ordinal i, so a single-GPU run always lands on ordinal
//     0. Pin a different card through the profile's launch env instead
//     (CUDA_VISIBLE_DEVICES=1), which is what the process manager passes
//     through; that matters on a workstation whose GPU 0 also drives the
//     desktop.
//   - --ple-backend (disk vs pinned PLE n-gram table).
//
// Defaults come from the same introspection: ServerArgs (server/args.py),
// SchedulerConfig (scheduler/config.py) and EngineConfig (engine/config.py).
// Enum members are the argparse `choices` lists, which the owning registries
// feed: SUPPORTED_MOE_BACKENDS (moe/__init__.py) and SUPPORTED_CACHE_MANAGER
// (kvcache/__init__.py). --attention-backend has no `choices` (it is validated
// by attention.validate_attn_backend, which also accepts a "prefill,decode"
// pair), so it is a string row rather than an enum.
//
// The model is supplied from the profile's Model field (a local checkpoint
// directory, an FTW directory, or a Hugging Face / ModelScope repo id) and is
// emitted as --model by the freetoken arg builder, so neither --model nor its
// canonical spelling --model-path is a row here. --host is forced to loopback
// by the wrapper script (backends/freetoken/freetoken-serve.sh), and
// --shell-mode / --dummy-weight are excluded on purpose: the first turns
// `ft serve` into an interactive TUI (it pins max_running_req and
// cuda_graph_max_bs to 1 and sets silent_output, which suppresses the ready
// line the readiness probe waits for) and the second loads random weights.
//
// `port` carries IsPort so the validator knows the process manager owns it.
package freetokenhelp

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

// EmbeddedSchema returns the curated freetoken schema.
func EmbeddedSchema() domain.FlagSchema {
	return domain.BuildFlagSchema("embedded-freetoken-v1", freetokenRows)
}

// freetokenToolCallParsers is the closed choices list of --tool-call-parser.
// "auto" infers the parser from the checkpoint's model_type/architectures
// (args.py::_infer_tool_call_parser), which is right for every checkpoint in
// docs/models.md; name one explicitly only to override that inference.
var freetokenToolCallParsers = []string{
	"auto", "llama3", "qwen", "qwen25", "qwen3_coder", "mistral", "deepseekv32",
	"gemma4", "glm47", "minimax", "minimax_m3", "muse_glimmer", "gpt_oss", "gpt-oss",
}

// freetokenReasoningParsers is the closed choices list of --reasoning-parser.
// "off" is the disable value; "auto" infers it the same way.
var freetokenReasoningParsers = []string{
	"auto", "off", "deepseekv32", "gpt_oss", "qwen3", "glm", "minimax",
	"minimax_m3", "muse_glimmer", "gemma4",
}

var freetokenRows = []domain.FlagSpecRow{
	// Server + runtime. host is forced to loopback by the wrapper; the process
	// manager owns port allocation.
	{Long: "port", Type: domain.FlagTypeInt, Default: float64(1919), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(65535), IsPort: true, HelpText: "HTTP server port (assigned by the process manager)", Group: "common"},
	{Long: "served-model-name", Type: domain.FlagTypeString, HelpText: "Model id reported by /v1/models (omit = the basename of the model path). Do not set it empty: the engine only derives the basename when the flag is absent", Group: "common"},
	{Long: "dtype", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "float16", "bfloat16", "float32"}, Default: "auto", HelpText: "Weight/activation dtype. auto reads torch_dtype (or text_config.dtype) from the checkpoint, falling back to bfloat16; it does NOT override a quantized checkpoint's expert format", Group: "common"},
	{Long: "tensor-parallel-size", Aliases: []string{"tp-size"}, Type: domain.FlagTypeInt, Default: float64(1), Min: ptrutil.Ptr(1), HelpText: "Tensor-parallel world size, one scheduler worker per rank; rank i binds CUDA ordinal i. The ranks all-reduce over PyNCCL (NCCL over PCIe on a rig without NVLink)", Group: "common"},
	{Long: "max-running-requests", Type: domain.FlagTypeInt, Default: float64(4), Min: ptrutil.Ptr(1), HelpText: "Maximum concurrently running requests. Also the default ceiling for CUDA-graph capture (see cuda-graph-max-bs) and a multiplier on the per-request GDN/window state pools", Group: "common"},
	{Long: "max-output-tokens", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), HelpText: "Default decode-token budget for a request that sends no max_tokens (omit = the API adapter's own 32k default)", Group: "common"},
	{Long: "max-seq-len-override", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), HelpText: "Maximum sequence length in tokens (omit = the checkpoint's own rotary max_position)", Group: "common"},
	{Long: "max-prefill-length", Aliases: []string{"max-extend-length"}, Type: domain.FlagTypeInt, Default: float64(8192), Min: ptrutil.Ptr(1), HelpText: "Chunked-prefill chunk size in tokens; also the engine's max forward length", Group: "common"},
	{Long: "cuda-graph-max-bs", Aliases: []string{"graph"}, Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), HelpText: "Largest batch size captured as a CUDA graph (omit = max-running-requests). Lower it to cut capture time and graph VRAM", Group: "common"},
	{Long: "num-tokenizer", Aliases: []string{"tokenizer-count"}, Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), HelpText: "Dedicated tokenizer worker processes. 0 shares one process with the detokenizer, which is right for a single-operator server", Group: "common"},
	{Long: "decode-log-interval", Type: domain.FlagTypeInt, Default: float64(40), Min: ptrutil.Ptr(1), HelpText: "Emit one decode scheduler status line every N decode forwards", Group: "common"},
	{Long: "model-source", Type: domain.FlagTypeEnum, EnumValues: []string{"huggingface", "modelscope"}, Default: "huggingface", HelpText: "Hub the model id is downloaded from when it is not a local directory", Group: "common"},
	{Long: "disable-pynccl", Type: domain.FlagTypeBool, Default: false, HelpText: "Fall back to torch.distributed collectives instead of the PyNCCL communicator for tensor parallelism. argparse store_false, so the flag is emitted only when true; no effect at tensor-parallel-size 1", Group: "common"},
	{Long: "cors-origins", Type: domain.FlagTypeString, Default: "tauri://localhost,http://tauri.localhost,http://localhost:1420", HelpText: "Comma-separated CORS allow-list for browser/webview clients. Empty disables CORS headers, \"*\" allows any origin", Group: "common"},

	// VRAM budget, KV cache and attention.
	{Long: "memory-ratio", Type: domain.FlagTypeFloat, Default: float64(0.9), FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(1.0), HelpText: "Fraction of FREE VRAM the engine may spend on weights + MoE expert cache + KV; the remainder stays as CUDA-graph and activation headroom. Lower it when the card also drives a desktop", Group: "kv"},
	{Long: "num-pages", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), HelpText: "KV capacity as a page count (omit = sized from the VRAM left after weights and the MoE cache). Mutually exclusive with num-tokens", Group: "kv"},
	{Long: "num-tokens", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), HelpText: "KV capacity in tokens; must be a multiple of the resolved page size. Mutually exclusive with num-pages", Group: "kv"},
	{Long: "page-size", Type: domain.FlagTypeInt, Default: float64(1), Min: ptrutil.Ptr(1), HelpText: "KV page size in tokens. DSV4 forces 128, the trtllm attention backend needs 16/32/64, m3_sparse needs 128, qsa_sparse needs 64, and SWA models require 1", Group: "kv"},
	{Long: "cache-type", Type: domain.FlagTypeEnum, EnumValues: []string{"naive", "radix"}, Default: "radix", HelpText: "Prefix-cache strategy. radix reuses shared prompt prefixes across requests (and is materialized as the SWA-/GDN-aware variant automatically); naive opts out", Group: "kv"},
	{Long: "attention-backend", Aliases: []string{"attn"}, Type: domain.FlagTypeString, Default: "auto", HelpText: "Attention kernel: auto, or one of trtllm/fi/fa/triton/dsv4_sparse/dsa/m3_sparse/qsa_sparse, or a \"prefill,decode\" pair. trtllm needs sm_100+ (never Ampere); fi/fa need the flashinfer/sglang-kernel extras (freetoken[accel]); triton is the portable fallback", Group: "kv"},
	{Long: "kv-reserve-tokens", Type: domain.FlagTypeInt, Default: float64(8192), Min: ptrutil.Ptr(0), HelpText: "KV token floor held back before moe-cache-auto spends the rest of the budget on experts", Group: "kv"},
	{Long: "enable-special-token-ckpt", Type: domain.FlagTypeBool, Default: false, HelpText: "Checkpoint decode state at the tool-call opener token, so a GDN-hybrid or SWA model only invalidates the rewritten tool-call body instead of the whole turn when an agent edits the echoed call", Group: "kv"},

	// MoE execution and expert cache.
	{Long: "moe-backend", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "fused", "offload", "cpu", "hybrid"}, Default: "auto", HelpText: "Expert execution: fused keeps every expert resident in VRAM (never auto-selected), offload streams misses over PCIe into a GPU slot cache, cpu computes misses on the host, hybrid splits each step between the two. auto resolves a dense model to fused and a MoE model to offload (hybrid when an `ft bench bw` profile recommends it)", Group: "moe"},
	{Long: "moe-cache-size", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), HelpText: "GPU expert-cache size in slots (0 = unset). Mutually exclusive with moe-cache-rate and moe-cache-auto; leaving all three unset turns moe-cache-auto on for the offload family", Group: "moe"},
	{Long: "moe-cache-rate", Type: domain.FlagTypeFloat, FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(1.0), HelpText: "GPU expert-cache size as a fraction of all experts. Mutually exclusive with moe-cache-size and moe-cache-auto", Group: "moe"},
	{Long: "moe-cache-auto", Type: domain.FlagTypeBool, Default: false, HelpText: "Size the expert cache from free VRAM, MoE-first (KV only keeps kv-reserve-tokens as a floor). Mutually exclusive with moe-cache-size and moe-cache-rate; unsupported for owned-KV models", Group: "moe"},
	{Long: "moe-cache-policy", Type: domain.FlagTypeEnum, EnumValues: []string{"lru"}, Default: "lru", HelpText: "Eviction policy of the GPU expert-slot cache", Group: "moe"},
	{Long: "moe-cpu-threads", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), HelpText: "CPU worker threads for the cpu/hybrid expert executor. 0 = one per physical core", Group: "moe"},
	{Long: "moe-cpu-layers", Type: domain.FlagTypeString, HelpText: "With moe-backend offload/hybrid: which MoE layers decode on the CPU executor. An explicit id list (\"3,7,11\"), a count (\"8\" = 8 evenly strided layers), or a fraction (\"0.5\"). Omit = automatic (only where CUDA pinning is quota-capped, e.g. WSL); \"0\" forces every layer onto the GPU", Group: "moe"},
	{Long: "moe-hybrid-max-fetch", Type: domain.FlagTypeInt, Default: float64(-1), Min: ptrutil.Ptr(-1), HelpText: "With moe-backend hybrid: experts fetched over PCIe per layer per decode step; the rest of that step's misses run on the CPU. -1 = auto from the `ft bench bw` profile (1 without one), 0 = never fetch, a large value degenerates to plain offload", Group: "moe"},
	{Long: "moe-prefill-hit-d2d", Type: domain.FlagTypeBool, Default: false, HelpText: "During prefill prefetch, copy cache-resident experts device-to-device and stream only the misses over PCIe (cudaMemcpyBatchAsync, CUDA >= 13). Only pays off when moe-cache-size > 2 * num_experts", Group: "moe"},
	{Long: "disable-moe-prefill-overlap", Type: domain.FlagTypeBool, Default: false, HelpText: "Turn off the two-buffer overlap of prefill expert copies. argparse store_false, so the flag is emitted only when true; the overlap is on by default and needs moe-cache-size >= 2 * num_experts", Group: "moe"},
	{Long: "nvfp4-backend", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "marlin", "flashinfer", "triton"}, Default: "triton", HelpText: "NVFP4 routed-expert GEMM kernel. marlin covers sm_80-99 but needs a vLLM install in the same venv (its transformers pin conflicts with freetoken's, so it is not co-installable), flashinfer's b12x needs sm_120+ and CUDA >= 13, triton is the portable inline-dequant fallback. auto picks by GPU; an explicit value fails loudly when it cannot run", Group: "moe"},
	{Long: "expert-load", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "serial", "parallel"}, Default: "auto", HelpText: "How expert banks are read into host RAM at load. auto reads scattered experts in parallel and falls back to serial when free RAM cannot cover the banks plus the parallel reader's whole-shard buffer; serial forces the low-memory read, parallel forces the fast one", Group: "moe"},

	// Request/response behaviour.
	{Long: "tool-call-parser", Type: domain.FlagTypeEnum, EnumValues: freetokenToolCallParsers, Default: "auto", HelpText: "Tool-call grammar used to turn model output into OpenAI tool_calls. auto infers it from the checkpoint's model_type/architectures", Group: "api"},
	{Long: "reasoning-parser", Type: domain.FlagTypeEnum, EnumValues: freetokenReasoningParsers, Default: "auto", HelpText: "Parser that splits chain-of-thought into reasoning_content. auto infers it from the model family; off disables the split and leaves the thinking tags in content", Group: "api"},
	{Long: "sampling-defaults", Type: domain.FlagTypeEnum, EnumValues: []string{"model", "none"}, Default: "model", HelpText: "Where unspecified request sampling params come from. model fills temperature/top_k/top_p from the checkpoint's generation_config.json (keeps reasoning models out of greedy repetition loops); none uses framework defaults", Group: "api"},
	{Long: "enable-cache-report", Type: domain.FlagTypeBool, Default: false, HelpText: "Report prefix-cache hits in each response's usage block. On /v1/messages this also makes input_tokens exclude the cached prefix, matching Anthropic billing semantics", Group: "api"},
}

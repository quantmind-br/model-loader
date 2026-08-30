// Package tabbyhelp provides a static, curated schema for the tabby backend
// (TabbyAPI, the OpenAI-compatible server for ExLlamaV3). Upstream dropped
// ExLlamaV2 support (exl2/gptq-quantized models are rejected at load; the
// `backend` flag now accepts only "exllamav3" or empty for auto-detect).
// TabbyAPI's argparser is generated from a Pydantic model and its --help is
// not stable enough to parse, so we ship a hand-curated schema of the
// load/tuning flags that matter on a single workstation.
//
// Tracks TabbyAPI checkout fcc1a10 (2026-08-26); its venv pins exllamav3
// 1.4.4+cu128.torch2.9.0 (the minimum the exllamav3 backend asserts) on torch
// 2.9.0+cu128. exllamav2 0.3.2 is still installed in that venv but TabbyAPI no
// longer has an ExLlamaV2 backend, so it is never loaded.
//
// Every config section (network/model/draft_model/sampling/memory/...) is
// flattened into one CLI namespace: each Pydantic sub-field becomes
// --<field-name-with-dashes>, and only list-typed fields take multiple tokens
// (nargs="+"). dict-typed options (template_vars_default, template_vars_force)
// therefore have no usable CLI form and are not rows here.
//
// The model is supplied from the profile's Model field (an absolute path to
// the EXL3 model directory), which the tabby arg builder splits into
// --model-dir + --model-name; it is therefore not a row here. host/auth are
// forced by the wrapper script.
package tabbyhelp

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

// EmbeddedSchema returns the curated tabby schema. `port` carries IsPort so the
// validator knows the process manager owns it.
func EmbeddedSchema() domain.FlagSchema {
	return domain.BuildFlagSchema("embedded-tabby-v4", tabbyRows)
}

// tabbyToolFormats is the closed set of keys TabbyAPI accepts for --tool-format
// (endpoints/OAI/utils/tools.py::ALL_TOOLCALL_FORMATS). An unrecognized value
// is not an error: the server warns and silently disables tool-call parsing.
var tabbyToolFormats = []string{
	"deepseek_v4", "dsv4", "gemma4", "glimmer", "glm4_5", "glm4_6", "glm4_7",
	"harmony", "hy3", "hy_v3", "laguna", "minimax_m2", "minimax_m2_1",
	"minimax_m2_5", "mistral", "mistral_old", "muse_glimmer", "poolside_v1",
	"qwen3_5", "qwen3_coder", "step3_5", "step3_7",
}

var tabbyRows = []domain.FlagSpecRow{
	// Networking (manager-owned; host is forced to loopback by the wrapper).
	{Long: "port", Type: domain.FlagTypeInt, Default: float64(5000), IsPort: true, HelpText: "Server port (assigned by the process manager)", Group: "common"},
	{Long: "sse-ping-interval", Type: domain.FlagTypeInt, Default: float64(15), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(3600), HelpText: "Seconds between SSE keep-alive comments on streaming responses (0 = disabled); keeps a client from dropping the connection during a long prefill", Group: "common"},

	// Core model / context.
	{Long: "max-seq-len", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(-1), Max: ptrutil.Ptr(1048576), HelpText: "Max context length in tokens (omit = the model's own max_position_embeddings, else 8192; -1 = force-read it from config.json). Silently clamped down to cache-size when larger", Group: "common"},
	{Long: "cache-size", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(256), Max: ptrutil.Ptr(1048576), HelpText: "KV cache size in tokens (multiple of 256, >= max-seq-len; must be > 0 if set, omit the flag to match max-seq-len)", Group: "common"},
	{Long: "cache-mode", Type: domain.FlagTypeString, Default: "FP16", HelpText: "KV cache quant: a k,v bit pair each 2-8, e.g. 8,8 or 6,6, or a legacy alias FP16/Q8/Q6/Q4 (Q4=4,4 Q6=6,6 Q8=8,8; FP16 = unquantized)", Group: "common"},
	{Long: "chunk-size", Type: domain.FlagTypeInt, Default: float64(2048), Min: ptrutil.Ptr(256), Max: ptrutil.Ptr(32768), HelpText: "Prompt-ingestion chunk size (VRAM vs prefill speed; 512-4096 typical). Raised to 256 and rounded up to a multiple of 256", Group: "common"},
	{Long: "max-batch-size", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(2048), HelpText: "Max concurrent generation jobs (must be >= 1 if set; engine default is 128, 4 for recurrent/linear-attention models; omit the flag to use it)", Group: "common"},
	{Long: "cpu-moe-offload-layers", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(999), HelpText: "Number of MoE layers to offload to CPU inference (0 = none; MoE models only; use a large value like 999 to offload all)", Group: "common"},
	{Long: "cpu-moe-split-experts", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(999), HelpText: "Number of routed experts per MoE layer offloaded to CPU inference (0 = none). Unlike cpu-moe-offload-layers this splits every MoE layer: the coldest experts stay in system RAM and are computed on the CPU, overlapping that layer's GPU compute, with hot experts kept in VRAM. Mutually exclusive with cpu-moe-offload-layers; not supported with tensor-parallel", Group: "common"},
	{Long: "cpu-moe-threads", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), HelpText: "Worker thread count for CPU MoE inference, for either CPU-offload mode (omit = the EXL3_MOE_CPU_THREADS env var, else half the CPU core count)", Group: "common"},
	{Long: "output-chunking", Type: domain.FlagTypeBool, Default: true, HelpText: "Allocate completion cache space in chunk-size steps instead of reserving the whole completion up front", Group: "common"},

	// Multi-GPU.
	{Long: "tensor-parallel", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable tensor parallelism across GPUs (the only mode that speeds a single stream on 2 cards; ignores gpu-split-auto, falls back to its own autosplit when gpu-split is empty)", Group: "common"},
	{Long: "tensor-parallel-backend", Type: domain.FlagTypeEnum, EnumValues: []string{"native", "nccl"}, Default: "native", HelpText: "TP comm backend: native (PCIe / no NVLink) or nccl (NVLink); nccl silently falls back to native when unavailable", Group: "common"},
	{Long: "gpu-split-auto", Type: domain.FlagTypeBool, Default: true, HelpText: "Auto-allocate layers across visible GPUs (pipeline split; forced off for a single GPU, for tensor-parallel, and whenever gpu-split is set)", Group: "common"},
	{Long: "gpu-split", Type: domain.FlagTypeString, Default: "", HelpText: "Manual per-GPU VRAM split in GB, e.g. 21,23 (empty = autosplit). A 0 entry excludes that GPU. Leave headroom on the desktop card", Group: "common"},
	{Long: "autosplit-reserve", Type: domain.FlagTypeString, Default: "96", HelpText: "VRAM reserved per GPU during autosplit, in MB, e.g. 2048,96 (default 96 on GPU 0). Ignored when gpu-split or tensor-parallel is set", Group: "common"},

	// RoPE.
	{Long: "rope-scale", Type: domain.FlagTypeFloat, Default: float64(1.0), FloatMin: ptrutil.Ptr(1.0), FloatMax: ptrutil.Ptr(128.0), HelpText: "Linear RoPE scale / compress_pos_emb (auto-skipped if the model has YaRN)", Group: "common"},
	{Long: "rope-alpha", Type: domain.FlagTypeString, Default: "", HelpText: "NTK alpha_value, or 'auto' to compute (empty = from model)", Group: "common"},

	// Capabilities / parsing.
	{Long: "vision", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable vision/multimodal if the model supports it (warns and stays off when the architecture has no ExLlamaV3 vision component)", Group: "common"},
	{Long: "vision-offload", Type: domain.FlagTypeBool, Default: false, HelpText: "Keep the vision tower's weights in pinned system RAM instead of VRAM, streaming them to the GPU during inference (trades vision speed for VRAM). Only applies when vision is enabled", Group: "common"},
	{Long: "reasoning", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable the reasoning parser: split the reply into reasoning_content and content", Group: "common"},
	{Long: "reasoning-start-token", Type: domain.FlagTypeString, Default: "<think>", HelpText: "Opening tag the reasoning parser looks for (set it when the model uses something other than <think>)", Group: "common"},
	{Long: "reasoning-end-token", Type: domain.FlagTypeString, Default: "</think>", HelpText: "Closing tag the reasoning parser looks for", Group: "common"},
	{Long: "start-in-reasoning", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "always", "never"}, Default: "auto", HelpText: "Whether generation starts inside a reasoning block (auto scans the templated prompt for an unclosed reasoning start token)", Group: "common"},
	{Long: "tool-calls-in-reasoning", Type: domain.FlagTypeBool, Default: true, HelpText: "Parse tool calls that occur inside reasoning content (false = treat them as plain reasoning text)", Group: "common"},
	{Long: "reasoning-budget-tokens", Type: domain.FlagTypeInt, HelpText: "Default reasoning token budget: once exceeded the server injects reasoning-budget-message plus the model's end-of-reasoning tokens. 0 ends reasoning as soon as it starts; omit or pass a negative value to disable. Requires a reasoning format (reasoning tags, harmony or muse-glimmer)", Group: "common"},
	{Long: "reasoning-budget-message", Type: domain.FlagTypeString, Default: "", HelpText: "Text injected before the forced end-of-reasoning tokens when reasoning-budget-tokens is exhausted (empty = only the tokens are forced)", Group: "common"},
	{Long: "backend", Type: domain.FlagTypeEnum, EnumValues: []string{"exllamav3"}, Default: "", HelpText: "Force the engine (empty = auto-detect; exllamav2 is no longer supported upstream, exl2/gptq models are rejected)", Group: "common"},
	{Long: "prompt-template", Type: domain.FlagTypeString, Default: "", HelpText: "Chat template name/override (empty = model's own; a name selects one entry of a multi-template tokenizer_config.json, or a file in templates/)", Group: "common"},
	{Long: "tool-format", Type: domain.FlagTypeEnum, EnumValues: tabbyToolFormats, Default: "", HelpText: "Tool-call parser (empty = tool calls are not parsed). An unknown name only warns and disables parsing, so a typo silently breaks tool use", Group: "common"},
	{Long: "harmony", Type: domain.FlagTypeBool, HelpText: "Force the Harmony message format (gpt-oss) on or off; omit for auto-detection from the model's special tokens. When active it supersedes reasoning and tool-format", Group: "common"},
	{Long: "muse-glimmer", Type: domain.FlagTypeBool, HelpText: "Force the Muse Glimmer message format on or off; omit for auto-detection from the model's special tokens. Equivalent to tool-format: muse_glimmer. When active it supersedes reasoning and tool-format", Group: "common"},
	{Long: "force-enable-thinking", Type: domain.FlagTypeBool, Default: false, HelpText: "DEPRECATED upstream, but the only CLI route to force enable_thinking in the chat template (template-vars-force is dict-only and has no CLI form)", Group: "common"},

	// Speculative decoding (draft model / MTP).
	{Long: "draft-mode", Type: domain.FlagTypeEnum, EnumValues: []string{"model", "disabled", "mtp", "ngram"}, Default: "model", HelpText: "Speculative decoding mode: model (separate draft model, needs draft-model-name), mtp (self-speculative, uses the main model's MTP/DFlash component), ngram, or disabled", Group: "draft"},
	{Long: "draft-model-dir", Type: domain.FlagTypeString, Default: "models", HelpText: "Directory to look for the draft model in (default: models); unused by draft-mode=mtp", Group: "draft"},
	{Long: "draft-model-name", Type: domain.FlagTypeString, Default: "", HelpText: "Draft model subfolder under draft-model-dir (enables draft-mode=model)", Group: "draft"},
	{Long: "draft-cache-mode", Type: domain.FlagTypeEnum, EnumValues: []string{"FP16", "Q8", "Q6", "Q4"}, Default: "FP16", HelpText: "Draft model KV cache quant. Unlike cache-mode this accepts only the legacy aliases; a k,v bit pair is rejected by config validation", Group: "draft"},
	{Long: "draft-gpu-split", Type: domain.FlagTypeString, Default: "", HelpText: "Per-GPU GB split for the draft model, e.g. 24,0 (empty = autosplit)", Group: "draft"},
	{Long: "draft-rope-scale", Type: domain.FlagTypeFloat, Default: float64(1.0), FloatMin: ptrutil.Ptr(1.0), FloatMax: ptrutil.Ptr(128.0), HelpText: "Linear RoPE scale for the draft model", Group: "draft"},
	{Long: "draft-rope-alpha", Type: domain.FlagTypeFloat, FloatMin: ptrutil.Ptr(1.0), FloatMax: ptrutil.Ptr(128.0), HelpText: "NTK alpha_value for the draft model (omit = from model / auto-calculated). Numeric only: unlike rope-alpha, the config model rejects 'auto'", Group: "draft"},
	{Long: "draft-num-tokens", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(32), HelpText: "Tokens drafted per iteration (omit = the draft head's own default: DFlash 15, Qwen3.5-family MTP 4, Hunyuan-V3 MTP 2). Do not pass 0: the generator falls back to the default while the draft cache is still sized for 0", Group: "draft"},
	{Long: "dynamic-draft", Type: domain.FlagTypeBool, Default: false, HelpText: "Adjust the number of draft tokens dynamically based on observed acceptance rate (ceiling = draft-num-tokens)", Group: "draft"},
	{Long: "ngram-match-min", Type: domain.FlagTypeInt, Default: float64(2), Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(16), HelpText: "Minimum match length for exllamav3 n-gram drafting (only used when draft-mode=ngram; must be > 0)", Group: "draft"},

	// System-memory offload (VRAM headroom for large contexts / MoE models).
	{Long: "sysmem-kv-cache", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), HelpText: "Size of the system-memory second-tier K/V cache, in MB (0 = disabled, KV lives in VRAM only)", Group: "memory"},
	{Long: "sysmem-recurrent-cache", Type: domain.FlagTypeInt, Default: float64(4096), Min: ptrutil.Ptr(0), HelpText: "Max size of the system-memory recurrent-state cache, in MB (linear/sliding-attention models only)", Group: "memory"},
	{Long: "cuda-malloc-async", Type: domain.FlagTypeBool, Default: true, HelpText: "Use the cudaMallocAsync Torch allocator (sets PYTORCH_ALLOC_CONF). Disable if you hit intermittent OoM errors", Group: "memory"},

	// Sampling fallbacks.
	{Long: "override-preset", Type: domain.FlagTypeString, Default: "", HelpText: "Basename (no .yml) of a preset in the backend's sampler_overrides/ folder, applied as fallbacks for sampling params a request omits (e.g. a repetition penalty for clients that send none); safe_defaults ships with TabbyAPI", Group: "sampling"},
}

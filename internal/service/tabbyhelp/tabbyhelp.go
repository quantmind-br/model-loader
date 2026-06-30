// Package tabbyhelp provides a static, curated schema for the tabby backend
// (TabbyAPI, the OpenAI-compatible server for ExLlamaV2/ExLlamaV3). TabbyAPI's
// argparser is generated from a Pydantic model and its --help is not stable
// enough to parse, so we ship a hand-curated schema of the load/tuning flags
// that matter on a single workstation.
//
// The model is supplied from the profile's Model field (an absolute path to the
// EXL2/EXL3 model directory), which the tabby arg builder splits into
// --model-dir + --model-name; it is therefore not a row here. host/auth are
// forced by the wrapper script. exl2 vs exl3 is auto-detected from the model's
// quantization_config, overridable via the `backend` flag.
package tabbyhelp

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

// EmbeddedSchema returns the curated tabby schema. `port` carries IsPort so the
// validator knows the process manager owns it.
func EmbeddedSchema() domain.FlagSchema {
	return domain.BuildFlagSchema("embedded-tabby-v1", tabbyRows)
}

var tabbyRows = []domain.FlagSpecRow{
	// Networking (manager-owned; host is forced to loopback by the wrapper).
	{Long: "port", Type: domain.FlagTypeInt, Default: float64(5000), IsPort: true, HelpText: "Server port (assigned by the process manager)", Group: "common"},

	// Core model / context.
	{Long: "max-seq-len", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(-1), Max: ptrutil.Ptr(1048576), HelpText: "Max context length in tokens (0/-1 = read from model config.json)", Group: "common"},
	{Long: "cache-size", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1048576), HelpText: "KV cache size in tokens (multiple of 256, >= max-seq-len; 0 = match max-seq-len)", Group: "common"},
	{Long: "cache-mode", Type: domain.FlagTypeString, Default: "FP16", HelpText: "KV cache quant. exl2: FP16/Q8/Q6/Q4. exl3: a k,v bit pair each 2-8, e.g. 8,8 or 6,6", Group: "common"},
	{Long: "chunk-size", Type: domain.FlagTypeInt, Default: float64(2048), Min: ptrutil.Ptr(256), Max: ptrutil.Ptr(32768), HelpText: "Prompt-ingestion chunk size (VRAM vs prefill speed; 512-4096 typical)", Group: "common"},
	{Long: "max-batch-size", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(2048), HelpText: "Max concurrent generation jobs (0 = engine default)", Group: "common"},

	// Multi-GPU.
	{Long: "tensor-parallel", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable tensor parallelism across GPUs (the only mode that speeds a single stream on 2 cards; ignores gpu-split-auto)", Group: "common"},
	{Long: "tensor-parallel-backend", Type: domain.FlagTypeEnum, EnumValues: []string{"native", "nccl"}, Default: "native", HelpText: "TP comm backend: native (PCIe / no NVLink) or nccl (NVLink)", Group: "common"},
	{Long: "gpu-split-auto", Type: domain.FlagTypeBool, Default: true, HelpText: "Auto-allocate layers across visible GPUs (pipeline split; ignored for single GPU)", Group: "common"},
	{Long: "gpu-split", Type: domain.FlagTypeString, Default: "", HelpText: "Manual per-GPU VRAM split in GB, e.g. 21,23 (empty = autosplit). Leave headroom on the desktop card", Group: "common"},
	{Long: "autosplit-reserve", Type: domain.FlagTypeString, Default: "", HelpText: "VRAM reserved per GPU during autosplit, in MB, e.g. 2048,96", Group: "common"},

	// RoPE.
	{Long: "rope-scale", Type: domain.FlagTypeFloat, Default: float64(1.0), FloatMin: ptrutil.Ptr(1.0), FloatMax: ptrutil.Ptr(128.0), HelpText: "Linear RoPE scale / compress_pos_emb (auto-skipped if the model has YaRN)", Group: "common"},
	{Long: "rope-alpha", Type: domain.FlagTypeString, Default: "", HelpText: "NTK alpha_value, or 'auto' to compute (empty = from model)", Group: "common"},

	// Capabilities / parsing.
	{Long: "vision", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable vision/multimodal if the model supports it", Group: "common"},
	{Long: "reasoning", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable the reasoning parser (e.g. deepseek-r1 style <think> tags)", Group: "common"},
	{Long: "backend", Type: domain.FlagTypeEnum, EnumValues: []string{"exllamav2", "exllamav3"}, Default: "", HelpText: "Force the engine (empty = auto-detect exl2 vs exl3 from the model)", Group: "common"},
	{Long: "prompt-template", Type: domain.FlagTypeString, Default: "", HelpText: "Chat template name/override (empty = model's own)", Group: "common"},
	{Long: "tool-format", Type: domain.FlagTypeString, Default: "", HelpText: "Tool-call parser, e.g. qwen3_coder, gemma4, glm4_5, mistral (empty = tool calls not parsed)", Group: "common"},

	// Speculative decoding (draft model / MTP).
	{Long: "draft-mode", Type: domain.FlagTypeEnum, EnumValues: []string{"model", "disabled", "mtp", "ngram"}, Default: "model", HelpText: "Speculative decoding mode: model (needs draft-model-name), mtp (self-speculative), ngram, or disabled", Group: "draft"},
	{Long: "draft-model-name", Type: domain.FlagTypeString, Default: "", HelpText: "Draft model subfolder under draft-model-dir (enables draft-mode=model)", Group: "draft"},
	{Long: "draft-cache-mode", Type: domain.FlagTypeString, Default: "FP16", HelpText: "Draft model KV cache quant (same value space as cache-mode)", Group: "draft"},
	{Long: "draft-gpu-split", Type: domain.FlagTypeString, Default: "", HelpText: "Per-GPU GB split for the draft model, e.g. 24,0 (empty = autosplit)", Group: "draft"},
	{Long: "draft-rope-scale", Type: domain.FlagTypeFloat, Default: float64(1.0), FloatMin: ptrutil.Ptr(1.0), FloatMax: ptrutil.Ptr(128.0), HelpText: "Linear RoPE scale for the draft model", Group: "draft"},
	{Long: "draft-num-tokens", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(32), HelpText: "Tokens drafted per iteration (0 = model default)", Group: "draft"},
}

// Package unslothhelp provides a static, curated schema for the unsloth
// backend. `unsloth studio run` is a Python wrapper around llama-server whose
// --help is not stable enough to parse, so we ship a hand-curated schema of
// the unsloth orchestration flags plus the high-value llama-server pass-through
// flags. Studio-managed flags (host, api-key, model identity, raw -np, UI,
// embedding/rerank, tools) are intentionally absent.
package unslothhelp

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

// EmbeddedSchema returns the curated unsloth schema. `model` is supplied from
// the profile's Model field (emitted as --model), so it is not a row here.
// `port` carries IsPort so the validator knows the manager owns it.
func EmbeddedSchema() domain.FlagSchema {
	return domain.BuildFlagSchema("embedded-unsloth-v1", unslothRows)
}

var unslothRows = []domain.FlagSpecRow{
	// Networking (manager-owned; host is forced to loopback by the wrapper).
	{Long: "port", Type: domain.FlagTypeInt, Default: float64(8888), IsPort: true, HelpText: "Server port (assigned by the process manager)", Group: "common"},

	// Unsloth orchestration flags.
	{Long: "gguf-variant", Type: domain.FlagTypeString, Default: "", HelpText: "GGUF quant variant to fetch (e.g. UD-Q4_K_XL); ignored for local GGUF paths", Group: "common"},
	{Long: "max-seq-length", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1048576), HelpText: "Max sequence length unsloth advertises (0 = model default)", Group: "common"},
	{Long: "parallel", Type: domain.FlagTypeInt, Default: float64(1), Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(256), HelpText: "Decode slots; N requests share one model, each gets ctx/N KV", Group: "common"},
	{Long: "tensor-parallel", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable unsloth tensor parallelism", Group: "common"},
	{Long: "enable-tools", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable Studio tool calling (web search, code, bash)", Group: "common"},
	{Long: "load-in-4bit", Type: domain.FlagTypeBool, Default: true, HelpText: "Load weights in 4-bit (unsloth)", Group: "common"},

	// llama-server pass-through (last-wins parser; unsloth appends these).
	{Long: "ctx-size", Short: "c", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1048576), HelpText: "Context window size (0 = model default)", Group: "common"},
	{Long: "n-gpu-layers", Short: "ngl", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1000), HelpText: "Layers to offload to GPU (999 = all)", Group: "common"},
	{Long: "flash-attn", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable flash attention (bare flag; use extraArgs if the bundled build needs on/off/auto)", Group: "common"},
	{Long: "cache-type-k", Type: domain.FlagTypeEnum, EnumValues: []string{"f16", "bf16", "q8_0", "q5_1", "q5_0", "q4_1", "q4_0"}, Default: "", HelpText: "KV cache K type", Group: "common"},
	{Long: "cache-type-v", Type: domain.FlagTypeEnum, EnumValues: []string{"f16", "bf16", "q8_0", "q5_1", "q5_0", "q4_1", "q4_0"}, Default: "", HelpText: "KV cache V type", Group: "common"},
	{Long: "jinja", Type: domain.FlagTypeBool, Default: false, HelpText: "Use the model's Jinja chat template", Group: "common"},
	{Long: "chat-template-file", Type: domain.FlagTypeString, Default: "", HelpText: "Path to a chat template (.jinja) file", Group: "common"},

	// Sampling pass-through.
	{Long: "temp", Type: domain.FlagTypeFloat, Default: float64(0.8), FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(2.0), HelpText: "Sampling temperature", Group: "sampling"},
	{Long: "top-p", Type: domain.FlagTypeFloat, Default: float64(0.95), FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(1.0), HelpText: "Top-p (nucleus) sampling", Group: "sampling"},
	{Long: "top-k", Type: domain.FlagTypeInt, Default: float64(40), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1000), HelpText: "Top-k sampling", Group: "sampling"},
	{Long: "min-p", Type: domain.FlagTypeFloat, Default: float64(0.05), FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(1.0), HelpText: "Min-p sampling", Group: "sampling"},
	{Long: "repeat-penalty", Type: domain.FlagTypeFloat, Default: float64(1.0), FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(2.0), HelpText: "Repetition penalty", Group: "sampling"},
	{Long: "seed", Type: domain.FlagTypeInt, Default: float64(-1), HelpText: "RNG seed (-1 = random)", Group: "sampling"},
}

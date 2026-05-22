// Package dflashhelp provides a static schema for the DFlash backend.
// DFlash is the Lucebox speculative-decoding runtime (lucebox-hub/dflash):
// an OpenAI-compatible Python server (scripts/server.py) on top of the
// test_dflash C++ daemon. Its flags are hand-curated here because the
// server is launched through a wrapper script, not parsed at runtime.
package dflashhelp

import "github.com/quantmind-br/model-loader/internal/domain"

func iptr(v int) *int { return &v }

// EmbeddedSchema returns a FlagSchema covering the common DFlash server.py
// arguments. The target model is passed via --target (mapped from the
// profile's Model field by processmgr.buildDFlashArgs); the draft model is
// the --draft flag below.
func EmbeddedSchema() domain.FlagSchema {
	return domain.BuildFlagSchema("embedded-dflash-v1", dflashRows)
}

var kvTypes = []string{"f16", "bf16", "q4_0", "q4_1", "q5_0", "q5_1", "q8_0", "tq3_0"}

var dflashRows = []domain.FlagSpecRow{
	{Long: "target", Type: domain.FlagTypeString, Default: "", HelpText: "Path to the target GGUF model (qwen35-compatible)", Group: "common"},
	{Long: "draft", Type: domain.FlagTypeString, Default: "", HelpText: "Path to the DFlash draft model (GGUF or safetensors)", Group: "common"},
	{Long: "host", Type: domain.FlagTypeString, Default: "127.0.0.1", HelpText: "Server host address", Group: "common"},
	{Long: "port", Type: domain.FlagTypeInt, Default: float64(8080), IsPort: true, HelpText: "Server port", Group: "common"},
	{Long: "max-ctx", Type: domain.FlagTypeInt, Default: float64(16384), Min: iptr(0), Max: iptr(1024 * 1024), HelpText: "Maximum context length (oversizing on short prompts can slow attention 20x+)", Group: "common"},
	{Long: "budget", Type: domain.FlagTypeInt, Default: float64(22), Min: iptr(1), Max: iptr(512), HelpText: "Speculative decode token budget per step", Group: "common"},
	{Long: "verify-mode", Type: domain.FlagTypeEnum, EnumValues: []string{"ddtree", "fast", "seq", "replay"}, Default: "ddtree", HelpText: "Qwen daemon verify mode", Group: "common"},
	{Long: "cache-type-k", Short: "ctk", Type: domain.FlagTypeEnum, EnumValues: kvTypes, Default: "q8_0", HelpText: "KV cache data type for keys", Group: "common"},
	{Long: "cache-type-v", Short: "ctv", Type: domain.FlagTypeEnum, EnumValues: kvTypes, Default: "q8_0", HelpText: "KV cache data type for values", Group: "common"},
	{Long: "fa-window", Type: domain.FlagTypeInt, Default: float64(2048), Min: iptr(0), Max: iptr(1024 * 1024), HelpText: "Sliding-window flash attention size (0 = full attention)", Group: "common"},
	{Long: "kv-f16", Type: domain.FlagTypeBool, Default: false, HelpText: "Force F16 KV cache", Group: "common"},
	{Long: "lazy-draft", Type: domain.FlagTypeBool, Default: false, HelpText: "Load the draft model lazily", Group: "common"},
	{Long: "tokenizer", Type: domain.FlagTypeString, Default: "", HelpText: "HuggingFace tokenizer id or path (default: inferred from target)", Group: "common"},
	{Long: "target-gpu", Type: domain.FlagTypeInt, Default: float64(0), Min: iptr(0), Max: iptr(64), HelpText: "GPU index for the target model", Group: "common"},
	{Long: "draft-gpu", Type: domain.FlagTypeInt, Default: float64(0), Min: iptr(0), Max: iptr(64), HelpText: "GPU index for the draft model", Group: "common"},
}

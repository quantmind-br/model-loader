// Package sglanghelp provides a static schema for the sglang backend.
// Unlike llama-server, sglang is a Python module whose --help output
// is not stable enough to parse reliably, so we ship a hand-curated
// embedded schema with the essential flags.
package sglanghelp

import "github.com/quantmind-br/model-loader/internal/domain"

// EmbeddedSchema returns a FlagSchema covering the most common sglang
// server arguments. It is used as the fallback when auto-generation from
// the binary is not available or not yet implemented.
func iptr(v int) *int     { return &v }
func fptr(v float64) *float64 { return &v }

func EmbeddedSchema() domain.FlagSchema {
	return domain.BuildFlagSchema("embedded-sglang-v1", sglangRows)
}

var sglangRows = []domain.FlagSpecRow{
	{Long: "model-path", Type: domain.FlagTypeString, Default: "", HelpText: "Path to the model weights (HuggingFace repo or local path)", Group: "common"},
	{Long: "host", Type: domain.FlagTypeString, Default: "127.0.0.1", HelpText: "Server host address", Group: "common"},
	{Long: "port", Type: domain.FlagTypeInt, Default: float64(30000), IsPort: true, HelpText: "Server port", Group: "common"},
	{Long: "tp-size", Type: domain.FlagTypeInt, Default: float64(1), Min: iptr(1), Max: iptr(64), HelpText: "Tensor parallelism size", Group: "common"},
	{Long: "dp-size", Type: domain.FlagTypeInt, Default: float64(1), Min: iptr(1), Max: iptr(64), HelpText: "Data parallelism size", Group: "common"},
	{Long: "dtype", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "half", "bfloat16", "float32"}, Default: "auto", HelpText: "Data type for model weights", Group: "common"},
	{Long: "quantization", Type: domain.FlagTypeEnum, EnumValues: []string{"fp8", "awq", "gptq", "marlin"}, Default: "", HelpText: "Quantization method", Group: "common"},
	{Long: "mem-fraction-static", Type: domain.FlagTypeFloat, Default: float64(0.9), FloatMin: fptr(0.0), FloatMax: fptr(1.0), HelpText: "Fraction of GPU memory reserved for KV cache", Group: "common"},
	{Long: "max-running-requests", Type: domain.FlagTypeInt, Default: float64(0), Min: iptr(0), Max: iptr(1024 * 1024), HelpText: "Maximum number of concurrent running requests (0 = unlimited)", Group: "common"},
	{Long: "chunked-prefill-size", Type: domain.FlagTypeInt, Default: float64(0), Min: iptr(0), Max: iptr(1024 * 1024), HelpText: "Prefill chunk size (0 = disabled)", Group: "common"},
	{Long: "context-length", Type: domain.FlagTypeInt, Default: float64(0), Min: iptr(0), Max: iptr(1024 * 1024), HelpText: "Maximum context length (0 = model default)", Group: "common"},
	{Long: "served-model-name", Type: domain.FlagTypeString, Default: "", HelpText: "Model name exposed in the API", Group: "common"},
	{Long: "enable-metrics", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable Prometheus-compatible metrics endpoint", Group: "common"},
	{Long: "schedule-policy", Type: domain.FlagTypeEnum, EnumValues: []string{"fcfs", "lpm", "random", "priority", "dfs-weight"}, Default: "fcfs", HelpText: "Request scheduling policy", Group: "common"},
}

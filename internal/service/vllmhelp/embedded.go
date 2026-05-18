// Package vllmhelp provides a static schema for the vllm backend.
// vLLM is a Python-based inference engine whose --help output uses argparse
// format; rather than parsing it at runtime, we ship a hand-curated embedded
// schema with the essential flags.
package vllmhelp

import "github.com/quantmind-br/model-loader/internal/domain"

// EmbeddedSchema returns a FlagSchema covering the most common vllm serve
// arguments. It is used as the fallback when auto-generation from the binary
// is not available or not yet implemented.
func EmbeddedSchema() domain.FlagSchema {
	return domain.BuildFlagSchema("embedded-vllm-v1", vllmRows)
}

var vllmRows = []domain.FlagSpecRow{
	{Long: "model", Type: domain.FlagTypeString, Default: "", HelpText: "HuggingFace model ID, local path, or GGUF file", Group: "common"},
	{Long: "dtype", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "half", "float16", "bfloat16", "float", "float32"}, Default: "auto", HelpText: "Data type for model weights and activations", Group: "common"},
	{Long: "tensor-parallel-size", Type: domain.FlagTypeInt, Default: float64(1), HelpText: "Number of GPUs to use for tensor parallelism", Group: "common"},
	{Long: "pipeline-parallel-size", Type: domain.FlagTypeInt, Default: float64(1), HelpText: "Number of pipeline stages", Group: "common"},
	{Long: "max-model-len", Type: domain.FlagTypeInt, Default: float64(0), HelpText: "Maximum context length (0 = auto from model config)", Group: "common"},
	{Long: "gpu-memory-utilization", Type: domain.FlagTypeFloat, Default: float64(0.9), HelpText: "Fraction of GPU memory to use (0.0–1.0)", Group: "common"},
	{Long: "swap-space", Type: domain.FlagTypeInt, Default: float64(4), HelpText: "CPU swap space size (GiB) per GPU", Group: "common"},
	{Long: "max-num-seqs", Type: domain.FlagTypeInt, Default: float64(0), HelpText: "Maximum number of sequences per iteration (0 = auto)", Group: "common"},
	{Long: "quantization", Type: domain.FlagTypeEnum, EnumValues: []string{"None", "awq", "gptq", "gguf", "fp8", "marlin", "bitsandbytes", "compressed-tensors", "quark", "aqlm"}, Default: "None", HelpText: "Quantization method applied to the model weights", Group: "common"},
	{Long: "host", Type: domain.FlagTypeString, Default: "0.0.0.0", HelpText: "Host address to bind the server", Group: "common"},
	{Long: "port", Type: domain.FlagTypeInt, Default: float64(8000), HelpText: "Port to listen on", Group: "common"},
	{Long: "api-key", Type: domain.FlagTypeString, Default: "", HelpText: "API key for authenticating requests", Group: "common"},
	{Long: "served-model-name", Type: domain.FlagTypeString, Default: "", HelpText: "Model name to advertise in the API (/v1/models)", Group: "common"},
	{Long: "enable-prefix-caching", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable automatic prefix caching", Group: "common"},
	{Long: "enforce-eager", Type: domain.FlagTypeBool, Default: false, HelpText: "Disable CUDA graph capture and run eagerly", Group: "common"},
	{Long: "chat-template", Type: domain.FlagTypeString, Default: "", HelpText: "Path or name of the chat template to use", Group: "common"},
	{Long: "trust-remote-code", Type: domain.FlagTypeBool, Default: false, HelpText: "Allow execution of remote code from HuggingFace repos", Group: "common"},
	{Long: "device", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "cuda", "cpu"}, Default: "auto", HelpText: "Device to run inference on", Group: "common"},
	{Long: "download-dir", Type: domain.FlagTypeString, Default: "", HelpText: "Directory to cache downloaded model weights", Group: "common"},
}

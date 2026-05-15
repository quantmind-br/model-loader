// Package vllmhelp provides a static schema for the vllm backend.
// vLLM is a Python-based inference engine whose --help output uses argparse
// format; rather than parsing it at runtime, we ship a hand-curated embedded
// schema with the essential flags.
package vllmhelp

import (
	"github.com/quantmind-br/model-loader/internal/domain"
)

// EmbeddedSchema returns a FlagSchema covering the most common vllm serve
// arguments. It is used as the fallback when auto-generation from the binary
// is not available or not yet implemented.
func EmbeddedSchema() domain.FlagSchema {
	return domain.FlagSchema{
		Version: "embedded-vllm-v1",
		Flags: map[string]domain.FlagSpec{
			"model": {
				Long:     "model",
				Type:     domain.FlagTypeString,
				Default:  "",
				HelpText: "HuggingFace model ID, local path, or GGUF file",
				Group:    "common",
			},
			"dtype": {
				Long:       "dtype",
				Type:       domain.FlagTypeEnum,
				EnumValues: []string{"auto", "half", "float16", "bfloat16", "float", "float32"},
				Default:    "auto",
				HelpText:   "Data type for model weights and activations",
				Group:      "common",
			},
			"tensor-parallel-size": {
				Long:     "tensor-parallel-size",
				Type:     domain.FlagTypeInt,
				Default:  float64(1),
				HelpText: "Number of GPUs to use for tensor parallelism",
				Group:    "common",
			},
			"pipeline-parallel-size": {
				Long:     "pipeline-parallel-size",
				Type:     domain.FlagTypeInt,
				Default:  float64(1),
				HelpText: "Number of pipeline stages",
				Group:    "common",
			},
			"max-model-len": {
				Long:     "max-model-len",
				Type:     domain.FlagTypeInt,
				Default:  float64(0),
				HelpText: "Maximum context length (0 = auto from model config)",
				Group:    "common",
			},
			"gpu-memory-utilization": {
				Long:     "gpu-memory-utilization",
				Type:     domain.FlagTypeFloat,
				Default:  float64(0.9),
				HelpText: "Fraction of GPU memory to use (0.0–1.0)",
				Group:    "common",
			},
			"swap-space": {
				Long:     "swap-space",
				Type:     domain.FlagTypeInt,
				Default:  float64(4),
				HelpText: "CPU swap space size (GiB) per GPU",
				Group:    "common",
			},
			"max-num-seqs": {
				Long:     "max-num-seqs",
				Type:     domain.FlagTypeInt,
				Default:  float64(0),
				HelpText: "Maximum number of sequences per iteration (0 = auto)",
				Group:    "common",
			},
			"quantization": {
				Long:       "quantization",
				Type:       domain.FlagTypeEnum,
				EnumValues: []string{"None", "awq", "gptq", "gguf", "fp8", "marlin", "bitsandbytes", "compressed-tensors", "quark", "aqlm"},
				Default:    "None",
				HelpText:   "Quantization method applied to the model weights",
				Group:      "common",
			},
			"host": {
				Long:     "host",
				Type:     domain.FlagTypeString,
				Default:  "0.0.0.0",
				HelpText: "Host address to bind the server",
				Group:    "common",
			},
			"port": {
				Long:     "port",
				Type:     domain.FlagTypeInt,
				Default:  float64(8000),
				HelpText: "Port to listen on",
				Group:    "common",
			},
			"api-key": {
				Long:     "api-key",
				Type:     domain.FlagTypeString,
				Default:  "",
				HelpText: "API key for authenticating requests",
				Group:    "common",
			},
			"served-model-name": {
				Long:     "served-model-name",
				Type:     domain.FlagTypeString,
				Default:  "",
				HelpText: "Model name to advertise in the API (/v1/models)",
				Group:    "common",
			},
			"enable-prefix-caching": {
				Long:     "enable-prefix-caching",
				Type:     domain.FlagTypeBool,
				Default:  false,
				HelpText: "Enable automatic prefix caching",
				Group:    "common",
			},
			"enforce-eager": {
				Long:     "enforce-eager",
				Type:     domain.FlagTypeBool,
				Default:  false,
				HelpText: "Disable CUDA graph capture and run eagerly",
				Group:    "common",
			},
			"chat-template": {
				Long:     "chat-template",
				Type:     domain.FlagTypeString,
				Default:  "",
				HelpText: "Path or name of the chat template to use",
				Group:    "common",
			},
			"trust-remote-code": {
				Long:     "trust-remote-code",
				Type:     domain.FlagTypeBool,
				Default:  false,
				HelpText: "Allow execution of remote code from HuggingFace repos",
				Group:    "common",
			},
			"device": {
				Long:       "device",
				Type:       domain.FlagTypeEnum,
				EnumValues: []string{"auto", "cuda", "cpu"},
				Default:    "auto",
				HelpText:   "Device to run inference on",
				Group:      "common",
			},
			"download-dir": {
				Long:     "download-dir",
				Type:     domain.FlagTypeString,
				Default:  "",
				HelpText: "Directory to cache downloaded model weights",
				Group:    "common",
			},
		},
	}
}

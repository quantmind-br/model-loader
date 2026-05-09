package llamahelp

import "github.com/quantmind-br/model-loader/internal/domain"

// EmbeddedSchema returns the compile-time fallback FlagSchema covering the
// curated essential flags listed in the design spec. The schema is pinned to
// llama.cpp build "v7376 (380b4c9)" — last validated on 2026-04-28.
//
// To refresh against a newer llama.cpp build:
//   1. capture the help into testdata: llama-server --help > testdata/help-vXXXX.txt
//   2. re-run the golden: go test ./internal/service/llamahelp -update
//   3. eyeball flag types/defaults; only update embedded.go if essentials drift
//   4. bump the Version field below to "embedded-vXXXX"
func EmbeddedSchema() domain.FlagSchema {
	flags := map[string]domain.FlagSpec{
		"model": {
			Long:     "model",
			Short:    "m",
			Type:     domain.FlagTypeString,
			HelpText: "model path (.gguf)",
			Group:    "embedded",
		},
		"n-gpu-layers": {
			Long:     "n-gpu-layers",
			Short:    "ngl",
			Aliases:  []string{"gpu-layers"},
			Type:     domain.FlagTypeInt,
			Default:  -1,
			HelpText: "max number of layers to store in VRAM",
			Group:    "embedded",
		},
		"ctx-size": {
			Long:     "ctx-size",
			Short:    "c",
			Type:     domain.FlagTypeInt,
			Default:  4096,
			HelpText: "size of the prompt context",
			Group:    "embedded",
		},
		"batch-size": {
			Long:     "batch-size",
			Short:    "b",
			Type:     domain.FlagTypeInt,
			Default:  2048,
			HelpText: "logical maximum batch size",
			Group:    "embedded",
		},
		"ubatch-size": {
			Long:     "ubatch-size",
			Short:    "ub",
			Type:     domain.FlagTypeInt,
			Default:  512,
			HelpText: "physical maximum batch size",
			Group:    "embedded",
		},
		"flash-attn": {
			Long:       "flash-attn",
			Short:      "fa",
			Type:       domain.FlagTypeEnum,
			EnumValues: []string{"on", "off", "auto"},
			Default:    "auto",
			HelpText:   "Flash Attention mode",
			Group:      "embedded",
		},
		"threads": {
			Long:     "threads",
			Short:    "t",
			Type:     domain.FlagTypeInt,
			Default:  -1,
			HelpText: "CPU threads",
			Group:    "embedded",
		},
		"parallel": {
			Long:     "parallel",
			Short:    "np",
			Type:     domain.FlagTypeInt,
			Default:  1,
			HelpText: "number of parallel sequences to decode",
			Group:    "embedded",
		},
		"mlock": {
			Long:     "mlock",
			Type:     domain.FlagTypeBool,
			HelpText: "lock model in RAM (no swap)",
			Group:    "embedded",
		},
		"cache-type-k": {
			Long:       "cache-type-k",
			Short:      "ctk",
			Type:       domain.FlagTypeEnum,
			EnumValues: cacheTypeEnum,
			HelpText:   "KV cache data type for K",
			Group:      "embedded",
		},
		"cache-type-v": {
			Long:       "cache-type-v",
			Short:      "ctv",
			Type:       domain.FlagTypeEnum,
			EnumValues: cacheTypeEnum,
			HelpText:   "KV cache data type for V",
			Group:      "embedded",
		},
		"split-mode": {
			Long:       "split-mode",
			Short:      "sm",
			Type:       domain.FlagTypeEnum,
			EnumValues: []string{"none", "layer", "row"},
			HelpText:   "how to split model across multiple GPUs",
			Group:      "embedded",
		},
		"tensor-split": {
			Long:     "tensor-split",
			Short:    "ts",
			Type:     domain.FlagTypeString,
			HelpText: "fraction of model offloaded to each GPU (comma-separated)",
			Group:    "embedded",
		},
	}
	return domain.FlagSchema{
		Version: "embedded-v7376",
		Flags:   flags,
	}
}

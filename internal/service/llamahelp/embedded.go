package llamahelp

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

// EmbeddedSchema returns the compile-time fallback FlagSchema covering the
// curated essential flags listed in the design spec. The schema is pinned to
// llama.cpp build "v10152 (0324696b8)" — last validated on 2026-07-27.
//
// To refresh against a newer llama.cpp build:
//   1. capture the help into testdata: llama-server --help > testdata/help-vXXXX.txt
//   2. re-run the golden: go test ./internal/service/llamahelp -update
//   3. eyeball flag types/defaults; only update embedded.go if essentials drift
//   4. bump the Version field below to "embedded-vXXXX"

func EmbeddedSchema() domain.FlagSchema {
	return domain.BuildFlagSchema("embedded-v10152", llamaRows)
}

var llamaRows = []domain.FlagSpecRow{
	{Long: "model", Short: "m", Type: domain.FlagTypeString, HelpText: "model path (.gguf)", Group: "embedded"},
	{Long: "n-gpu-layers", Short: "ngl", Aliases: []string{"gpu-layers"}, Type: domain.FlagTypeInt, Keywords: []string{"auto", "all"}, Default: -1, Min: ptrutil.Ptr(-2), Max: ptrutil.Ptr(9999), HelpText: "max number of layers to store in VRAM; accepts an exact integer, auto (-1), or all (-2)", Group: "embedded"},
	{Long: "ctx-size", Short: "c", Type: domain.FlagTypeInt, Default: 0, Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1024 * 1024), HelpText: "size of the prompt context; 0 = loaded from model", Group: "embedded"},
	{Long: "batch-size", Short: "b", Type: domain.FlagTypeInt, Default: 2048, Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1024 * 1024), HelpText: "logical maximum batch size", Group: "embedded"},
	{Long: "ubatch-size", Short: "ub", Type: domain.FlagTypeInt, Default: 512, Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1024 * 1024), HelpText: "physical maximum batch size", Group: "embedded"},
	{Long: "flash-attn", Short: "fa", Type: domain.FlagTypeEnum, EnumValues: []string{"on", "off", "auto"}, Default: "auto", HelpText: "Flash Attention mode", Group: "embedded"},
	{Long: "threads", Short: "t", Type: domain.FlagTypeInt, Default: -1, Min: ptrutil.Ptr(-1), Max: ptrutil.Ptr(1024 * 1024), HelpText: "CPU threads", Group: "embedded"},
	{Long: "parallel", Short: "np", Type: domain.FlagTypeInt, Default: 1, Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(1024 * 1024), HelpText: "number of parallel sequences to decode", Group: "embedded"},
	{Long: "mlock", Type: domain.FlagTypeBool, HelpText: "lock model in RAM (no swap)", Group: "embedded"},
	{Long: "cache-type-k", Short: "ctk", Type: domain.FlagTypeEnum, EnumValues: cacheTypeEnum, HelpText: "KV cache data type for K", Group: "embedded"},
	{Long: "cache-type-v", Short: "ctv", Type: domain.FlagTypeEnum, EnumValues: cacheTypeEnum, HelpText: "KV cache data type for V", Group: "embedded"},
	{Long: "split-mode", Short: "sm", Type: domain.FlagTypeEnum, EnumValues: []string{"none", "layer", "row", "tensor"}, HelpText: "how to split model across multiple GPUs", Group: "embedded"},
	{Long: "tensor-split", Short: "ts", Type: domain.FlagTypeString, HelpText: "fraction of model offloaded to each GPU (comma-separated)", Group: "embedded"},
}

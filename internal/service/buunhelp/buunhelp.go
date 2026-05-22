// Package buunhelp provides the embedded fallback schema for the buun-llama-cpp
// backend (a llama.cpp fork). The fork's --help is format-identical to
// upstream, so the live schema is normally parsed at runtime by llamahelp; this
// embedded schema is only the degraded fallback used when the binary cannot be
// resolved or parsed. It merges the upstream llama embedded base with the
// curated fork-specific flags (KV-cache turbo types, DFlash, generic spec-type).
package buunhelp

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/llamahelp"
)

const embeddedVersion = "embedded-buun-9561"

// EmbeddedSchema returns the upstream llama embedded base merged with the
// curated buun fork rows. Fork rows override base entries on key collision.
func EmbeddedSchema() domain.FlagSchema {
	base := llamahelp.EmbeddedSchema()
	extra := domain.BuildFlagSchema(embeddedVersion, buunRows)
	for k, v := range extra.Flags {
		base.Flags[k] = v
	}
	base.Version = embeddedVersion
	return base
}

// kvTurboTypes lists the standard llama KV types plus the fork's turbo types.
// Base types mirror cacheTypeEnum in internal/service/llamahelp/parser.go exactly;
// turbo types are appended after.
var kvTurboTypes = []string{
	"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1",
	"turbo2", "turbo3", "turbo4", "turbo2_tcq", "turbo3_tcq",
}

var specTypes = []string{
	"none", "draft-simple", "draft-eagle3", "draft-mtp",
	"ngram-simple", "ngram-map-k", "ngram-map-k4v", "ngram-mod",
	"ngram-cache", "suffix", "copyspec", "recycle", "dflash",
}

var buunRows = []domain.FlagSpecRow{
	{Long: "cache-type-k", Short: "ctk", Type: domain.FlagTypeEnum, EnumValues: kvTurboTypes, HelpText: "KV cache data type for K (incl. turbo* fork types)", Group: "embedded"},
	{Long: "cache-type-v", Short: "ctv", Type: domain.FlagTypeEnum, EnumValues: kvTurboTypes, HelpText: "KV cache data type for V (incl. turbo* fork types)", Group: "embedded"},
	{Long: "spec-draft-model", Short: "md", Aliases: []string{"model-draft", "draft-model"}, Type: domain.FlagTypeString, HelpText: "Path to the speculative draft model", Group: "embedded"},
	{Long: "spec-dflash-default", Type: domain.FlagTypeBool, HelpText: "Enable default DFlash speculative decoding config (requires -md)", Group: "embedded"},
	{Long: "dflash-max-slots", Type: domain.FlagTypeInt, Default: 1, Min: iptr(1), Max: iptr(1024), HelpText: "Max concurrent server slots with DFlash state", Group: "embedded"},
	{Long: "spec-type", Type: domain.FlagTypeEnum, EnumValues: specTypes, Default: "none", HelpText: "Speculative decoding strategy", Group: "embedded"},
	{Long: "draft-max", Aliases: []string{"draft", "draft-n"}, Type: domain.FlagTypeInt, Default: 16, Min: iptr(0), Max: iptr(512), HelpText: "Max draft tokens for speculative decoding", Group: "embedded"},
	{Long: "draft-min", Aliases: []string{"draft-n-min"}, Type: domain.FlagTypeInt, Default: 0, Min: iptr(0), Max: iptr(512), HelpText: "Min draft tokens for speculative decoding", Group: "embedded"},
	{Long: "port", Type: domain.FlagTypeInt, Default: 8080, Min: iptr(1), Max: iptr(65535), IsPort: true, HelpText: "Port to listen on", Group: "embedded"},
}

func iptr(v int) *int { return &v }

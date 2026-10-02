// Package stratahelp describes the Strata fork's HTTP launcher, not the native
// stdin engine. Tracks quantmind-br/Strata v0.1.30 (97cb786) plus the managed
// --model check and --max-context override in serve/server.py.
package stratahelp

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

// EmbeddedSchema returns the managed server's curated launch surface. Native
// flags (pack, MTP, expert cache, KV, CPU workers) belong in the Strata JSON
// config. The process manager supplies --engine strata, --model and --port.
func EmbeddedSchema() domain.FlagSchema {
	s := domain.BuildFlagSchema("embedded-strata-v1", rows)
	config := s.Flags["config"]
	config.Required = true
	s.Flags["config"] = config
	return s
}

var rows = []domain.FlagSpecRow{
	{Long: "config", Type: domain.FlagTypeString, HelpText: "Prepared Strata engine JSON (exe, args, cwd, tokenizer, model_name). Its --native model must match the profile model. Keep the adjacent .shared-settings.json for sampling/reasoning defaults", Group: "model"},
	{Long: "max-context", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), HelpText: "Engine context in tokens. Overrides --max-context in the config; omit to inherit it. Set explicitly so the proxy and benchmark know the context window", Group: "model"},
	{Long: "gpu", Type: domain.FlagTypeString, HelpText: "Physical GPU index or comma-separated indices (for example 0,1). Overrides the config's gpu selection; layer_split remains in the config", Group: "model"},
	{Long: "host", Type: domain.FlagTypeString, HelpText: "HTTP bind address; omit to inherit config host, falling back to 127.0.0.1", Group: "server"},
	{Long: "port", Type: domain.FlagTypeInt, Default: 8095, Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(65535), IsPort: true, HelpText: "HTTP port assigned by the process manager", Group: "server"},
	{Long: "fit-max-tokens", Type: domain.FlagTypeBool, Default: false, HelpText: "Clamp requested generation to remaining context instead of rejecting an oversized request. The config may also enable this", Group: "server"},
}

// Package lmstudiohelp provides a static, curated schema for the lmstudio
// backend (LM Studio's OpenAI-compatible HTTP server, driven through the
// official `lms` CLI). LM Studio is not a foreground server process: `lms`
// is an RPC client and every subcommand exits after talking to the LM Studio
// daemon/llmster, so there is no stable `--help` server surface to parse.
// The schema below is hand-curated from the lms CLI source instead.
//
// Tracks the lms CLI checkout backends/lms 07b7252d (package.json version
// 0.3.43); the wrapper executes whatever `lms` is on PATH, which on this box
// is ~/.lmstudio/bin/lms. Flag facts come from src/subcommands/server.ts
// (`lms server start`) and src/subcommands/load.ts (`lms load`),
// cross-checked against live `--help` output.
//
// Two CLI surfaces are flattened into one namespace because model-loader
// profiles express a launch as one flat argument list:
//
//   - server-start flags: --port (manager-owned), --bind, --cors;
//   - load flags: everything else (--gpu, --context-length, ...).
//
// The wrapper script (backends/lms/lmstudio-serve.sh) routes each flag to
// its owning subcommand. Flags the wrapper forces or that have no operator
// value are deliberately absent: `-y/--yes` (non-interactive), `--exact`,
// `--local`, `--estimate-only`.
//
// The profile's Model field carries the LM Studio model key (or local GGUF
// path) passed positionally to `lms load`; it is resolved fuzzy server-side,
// so it is not a row here either. `port` carries IsPort so the validator
// knows the process manager owns it.
package lmstudiohelp

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

// EmbeddedSchema returns the curated lmstudio schema. `port` carries IsPort
// so the validator knows the process manager owns it.
func EmbeddedSchema() domain.FlagSchema {
	return domain.BuildFlagSchema("embedded-lmstudio-v1", lmstudioRows)
}

var lmstudioRows = []domain.FlagSpecRow{
	// Server start (lms server start). host defaults to loopback; the
	// process manager owns port allocation.
	{Long: "port", Type: domain.FlagTypeInt, Default: float64(1234), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(65535), IsPort: true, HelpText: "HTTP server port (assigned by the process manager)", Group: "common"},
	{Long: "bind", Type: domain.FlagTypeString, Default: "127.0.0.1", HelpText: "Network interface the server binds (default 127.0.0.1; also settable via LMS_SERVER_HOST)", Group: "common"},
	{Long: "cors", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable CORS on the HTTP server (any website could then use it; keep off unless a browser client needs it)", Group: "common"},

	// Load (lms load). gpu accepts off/max keywords in addition to a ratio.
	{Long: "gpu", Type: domain.FlagTypeFloat, FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(1.0), Keywords: []string{"off", "max"}, HelpText: "GPU offload ratio 0..1, or \"off\" (0) / \"max\" (1). Omit to let LM Studio auto-determine the optimal ratio", Group: "common"},
	{Long: "context-length", Short: "c", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), HelpText: "Context length in tokens (omit = the model's trained maximum, which may exceed VRAM)", Group: "common"},
	{Long: "parallel", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), HelpText: "Number of requests the model can process in parallel (splits the context into shards)", Group: "common"},
	{Long: "ttl", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), HelpText: "Seconds of inactivity after which the daemon unloads the model (omit = no auto-unload)", Group: "common"},
	{Long: "identifier", Type: domain.FlagTypeString, Default: "", HelpText: "Instance identifier the loaded model answers to (empty = derived by LM Studio); use it when several variants of the same model must stay distinguishable", Group: "common"},

	// Speculative decoding (load-time draft configuration).
	{Long: "speculative-draft-mtp", Type: domain.FlagTypeBool, HelpText: "Opt in to load-time Draft MTP speculative decoding when the model supports it; omitted/false keeps LM Studio's own default. Force it off with --no-speculative-draft-mtp via extra args", Group: "draft"},
	{Long: "speculative-draft-simple", Type: domain.FlagTypeBool, Default: false, HelpText: "Draft Simple speculative decoding using speculative-draft-model", Group: "draft"},
	{Long: "speculative-draft-model", Type: domain.FlagTypeString, Default: "", HelpText: "Draft model resource used with speculative-draft-simple", Group: "draft"},
	{Long: "speculative-draft-max-tokens", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), HelpText: "Maximum drafted tokens per iteration. Applies to both speculative-draft-simple and speculative-draft-mtp", Group: "draft"},
	{Long: "speculative-draft-min-tokens", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(0), HelpText: "Minimum drafted tokens per iteration (omit = LM Studio's own default). Applies to both speculative-draft-simple and speculative-draft-mtp", Group: "draft"},
	{Long: "speculative-draft-min-continue-probability", Type: domain.FlagTypeFloat, FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(1.0), HelpText: "Keep drafting while token probability is at or above this threshold. Applies to both speculative-draft-simple and speculative-draft-mtp", Group: "draft"},
}

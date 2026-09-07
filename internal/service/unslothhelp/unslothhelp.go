// Package unslothhelp provides a static, curated schema for the unsloth
// backend. `unsloth studio run` is a Typer CLI wrapping llama-server whose
// full --help is not stable enough to parse, so we ship a hand-curated schema
// of the unsloth orchestration flags plus the high-value llama-server
// pass-through flags. Studio-managed flags (host, api-key, model identity,
// raw -np/--parallel, frontend, UI, embedding/rerank) are intentionally
// absent, as are the flags the unsloth-serve.sh wrapper already forces
// (--silent, --yes, --no-cloudflare, -H 127.0.0.1) and the public-exposure
// knobs (--secure, --cloudflare, --password). enable-tools is exposed since
// it is the operator-facing tri-state tool-calling toggle; the sibling
// tri-states (--enable/--disable-tool-call-healing, --enable/--disable-
// tool-call-nudging) and --disable-dns-pinning are not, because their useful
// direction is the negative spelling, which a plain bool row cannot emit; use
// ExtraArgs for those.
package unslothhelp

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

// EmbeddedSchema returns the curated unsloth schema. `model` is supplied from
// the profile's Model field (emitted as --model), so it is not a row here.
// `port` carries IsPort so the validator knows the manager owns it.
//
// Schema tracks unsloth checkout fcaa73634 (git describe
// v0.1.806-beta-56-gfcaa73634); flags read from
// unsloth_cli/commands/studio.py::run() (the `unsloth studio run` Typer
// command), the SpeculativeType literal in unsloth_cli/_inference.py, the
// pass-through denylist and value parsers in
// studio/backend/core/inference/llama_server_args.py, and the bundled
// llama-server b10715 (92cedc867) at ~/.unsloth/llama.cpp --help. That
// revision changed neither the Typer options, the SpeculativeType literal nor
// the pass-through denylist, so the rows below are unchanged.
func EmbeddedSchema() domain.FlagSchema {
	return domain.BuildFlagSchema("embedded-unsloth-v4", unslothRows)
}

var unslothRows = []domain.FlagSpecRow{
	// Networking (manager-owned; host is forced to loopback by the wrapper).
	{Long: "port", Type: domain.FlagTypeInt, Default: float64(8888), IsPort: true, HelpText: "Server port (assigned by the process manager)", Group: "common"},

	// Unsloth orchestration flags (`unsloth studio run`).
	{Long: "gguf-variant", Type: domain.FlagTypeString, Default: "", HelpText: "GGUF quant variant to fetch (e.g. UD-Q4_K_XL); ignored for local GGUF paths", Group: "common"},
	{Long: "max-seq-length", Aliases: []string{"context-length"}, Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1048576), HelpText: "Runtime context length in tokens (0 = model default for GGUF; 2048 for hub models)", Group: "common"},
	{Long: "gpu-memory-mode", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "manual"}, Default: "auto", HelpText: "GPU memory strategy for GGUF models. Auto lets Unsloth select GPUs and cap context to fit VRAM; manual delegates layer placement and context sizing to llama.cpp --fit", Group: "common"},
	{Long: "speculative-type", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "mtp", "dspark", "dflash", "ngram", "mtp+ngram", "off", "ngram-simple"}, Default: "", HelpText: "Speculative decoding mode for GGUF models; dspark and dflash auto-use a matching dspark-*.gguf / dflash drafter sidecar when available. Unset = Studio auto", Group: "common"},
	{Long: "spec-draft-n-max", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(16), HelpText: "Maximum draft tokens per step for MTP or DSpark (unset = backend default)", Group: "common"},
	{Long: "parallel", Short: "np", Aliases: []string{"n-parallel"}, Type: domain.FlagTypeInt, Default: float64(4), Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(64), HelpText: "llama-server parallel decode slots; N requests share one loaded model, each gets ctx/N KV. Owned by unsloth's --parallel (a raw llama-server -np/--parallel pass-through is rejected)", Group: "common"},
	{Long: "tensor-parallel", Type: domain.FlagTypeBool, Default: false, HelpText: "Split a GGUF across GPUs by tensor (--split-mode tensor) instead of by layer; multi-GPU only (no effect on one GPU), dense models gain decode speed, MoE usually don't", Group: "common"},
	{Long: "api-only", Type: domain.FlagTypeBool, Default: false, HelpText: "Serve only the API (no web UI), for a headless model server", Group: "common"},
	{Long: "enable-tools", Type: domain.FlagTypeBool, Default: true, HelpText: "Server-side tools (web search, code execution) are enabled by default for every bind; this checkbox can only re-affirm ON (--disable-tools is the Studio-forwarded off path, not reachable as a plain bool)", Group: "common"},
	{Long: "load-in-4bit", Type: domain.FlagTypeBool, Default: true, HelpText: "Load weights in 4-bit (unsloth); enabled by default, so this checkbox can only re-affirm ON (--no-load-in-4bit is the off path, not reachable as a plain bool)", Group: "common"},
	{Long: "verbose", Short: "v", Type: domain.FlagTypeBool, Default: false, HelpText: "Log every API request Studio serves, including the polling it deduplicates by default, and forward --log-verbose to llama-server", Group: "common"},

	// llama-server pass-through (last-wins parser; unsloth appends these).
	{Long: "ctx-size", Short: "c", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1048576), HelpText: "Context window size (0 = model default)", Group: "common"},
	{Long: "n-gpu-layers", Short: "ngl", Type: domain.FlagTypeInt, Default: float64(-1), Min: ptrutil.Ptr(-1), Max: ptrutil.Ptr(9999), HelpText: "Number of model layers moved to VRAM, or -1 for auto. Integer only: Unsloth re-parses this pass-through and rejects the llama-server keywords auto/all and any value below -1 with HTTP 400", Group: "common"},
	{Long: "flash-attn", Short: "fa", Type: domain.FlagTypeEnum, EnumValues: []string{"on", "off", "auto"}, Default: "auto", HelpText: "Flash Attention mode", Group: "common"},
	{Long: "cache-type-k", Short: "ctk", Type: domain.FlagTypeEnum, EnumValues: []string{"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1"}, Default: "f16", HelpText: "KV cache K type", Group: "common"},
	{Long: "cache-type-v", Short: "ctv", Type: domain.FlagTypeEnum, EnumValues: []string{"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1"}, Default: "f16", HelpText: "KV cache V type", Group: "common"},
	{Long: "jinja", Type: domain.FlagTypeBool, Default: true, HelpText: "Use the model's Jinja chat template (llama-server default: enabled)", Group: "common"},
	{Long: "chat-template-file", Type: domain.FlagTypeString, Default: "", HelpText: "Path to a chat template (.jinja) file", Group: "common"},

	// Sampling knobs owned by unsloth's own typer options: these hard-pin a
	// value via UNSLOTH_SAMPLING_* env vars at the request-translation layer,
	// NOT forwarded to llama-server as its own --temp/--repeat-penalty flags.
	{Long: "temperature", Type: domain.FlagTypeFloat, FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(2.0), HelpText: "Pin the sampling temperature for every request that omits it, overriding the model's recommended value. Unset = per-model recommendation", Group: "sampling"},
	{Long: "top-p", Type: domain.FlagTypeFloat, FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(1.0), HelpText: "Pin top-p (nucleus) sampling. Unset = per-model recommendation", Group: "sampling"},
	{Long: "top-k", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(-1), Max: ptrutil.Ptr(100), HelpText: "Pin top-k sampling. Unset = per-model recommendation", Group: "sampling"},
	{Long: "min-p", Type: domain.FlagTypeFloat, FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(1.0), HelpText: "Pin min-p sampling threshold. Unset = per-model recommendation", Group: "sampling"},
	{Long: "repetition-penalty", Type: domain.FlagTypeFloat, FloatMin: ptrutil.Ptr(1.0), FloatMax: ptrutil.Ptr(2.0), HelpText: "Pin the repetition penalty. Unset = per-model recommendation", Group: "sampling"},
	{Long: "presence-penalty", Type: domain.FlagTypeFloat, FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(2.0), HelpText: "Pin the presence penalty. Unset = per-model recommendation", Group: "sampling"},
	{Long: "seed", Type: domain.FlagTypeInt, Default: float64(-1), HelpText: "RNG seed (-1 = random); llama-server's own flag, not typer-managed", Group: "sampling"},
}

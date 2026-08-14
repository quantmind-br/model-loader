// Package tokenspeedhelp provides a static, curated schema for the tokenspeed
// backend. `tokenspeed serve` combines a Python argparse engine surface with a
// Rust clap gateway surface, so there is no single stable --help output to
// parse. The rows below track lightseekorg/tokenspeed main at 7b65d67 (tag
// v0.1.0, 2026-08-14) plus smg-project/smg's model_gateway clap arguments.
//
// Deliberate omissions: host is forced to 127.0.0.1 by the managed wrapper;
// api-key is omitted because model-loader injects no upstream token and keeps
// the backend unauthenticated on loopback; model/model-path come from
// Profile.Model as the leading positional; attn-tp-size conflicts with its
// tensor-parallel-size alias; enable-prefix-caching defaults on and its useful
// direction is the negative --no-enable-prefix-caching spelling, which a plain
// bool row cannot emit. Multi-node, disaggregation, kvstore, headless, and
// skip-tokenizer-init controls are outside model-loader's single-workstation
// scope. Operators can use ExtraArgs for intentionally omitted raw spellings.
//
// `tokenspeed serve` starts an smg gateway plus a Python gRPC engine. The
// gateway becomes ready at /readiness. A control sidecar binds main port + 1,
// and the Prometheus port is randomized unless --prometheus-port is supplied.
package tokenspeedhelp

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

// EmbeddedSchema returns the curated TokenSpeed serve schema.
func EmbeddedSchema() domain.FlagSchema {
	return domain.BuildFlagSchema("embedded-tokenspeed-v1", tokenspeedRows)
}

var tokenspeedRows = []domain.FlagSpecRow{
	{Long: "port", Type: domain.FlagTypeInt, Default: float64(8000), IsPort: true, HelpText: "Gateway port (assigned by the process manager)", Group: "common"},

	{Long: "served-model-name", Type: domain.FlagTypeString, HelpText: "Model name exposed by the OpenAI API", Group: "model"},
	{Long: "tokenizer", Type: domain.FlagTypeString, HelpText: "Tokenizer path or Hugging Face repository", Group: "model"},
	{Long: "tokenizer-mode", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "slow", "deepseek_v4"}, Default: "auto", HelpText: "Tokenizer implementation mode", Group: "model"},
	{Long: "load-format", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "pt", "safetensors", "npcache", "dummy", "extensible"}, Default: "auto", HelpText: "Model weight loading format", Group: "model"},
	{Long: "trust-remote-code", Type: domain.FlagTypeBool, Default: false, HelpText: "Allow model repositories to execute remote code", Group: "model"},
	{Long: "revision", Type: domain.FlagTypeString, HelpText: "Model repository revision", Group: "model"},
	{Long: "download-dir", Type: domain.FlagTypeString, HelpText: "Directory for downloaded model artifacts", Group: "model"},
	{Long: "dtype", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "half", "float16", "bfloat16", "float", "float32"}, Default: "auto", HelpText: "Model compute data type", Group: "model"},
	{Long: "quantization", Type: domain.FlagTypeEnum, EnumValues: []string{"fp8", "mxfp4", "nvfp4", "w8a8_fp8", "compressed-tensors"}, HelpText: "Weight quantization method", Group: "model"},
	{Long: "max-model-len", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(1048576), HelpText: "Maximum model context length in tokens", Group: "model"},

	{Long: "kv-cache-dtype", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "fp8", "fp8_e4m3", "mxfp8"}, Default: "auto", HelpText: "KV cache storage data type", Group: "kv-cache"},
	{Long: "kv-cache-quant-method", Type: domain.FlagTypeEnum, EnumValues: []string{"none", "per_token_head"}, Default: "none", HelpText: "KV cache quantization method", Group: "kv-cache"},

	{Long: "tensor-parallel-size", Aliases: []string{"tp"}, Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(8), HelpText: "Tensor parallel worker count", Group: "parallelism"},
	{Long: "data-parallel-size", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(8), HelpText: "Data parallel worker count", Group: "parallelism"},
	{Long: "expert-parallel-size", Aliases: []string{"ep-size"}, Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(8), HelpText: "Expert parallel worker count", Group: "parallelism"},
	{Long: "enable-expert-parallel", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable expert parallelism for mixture-of-experts models", Group: "parallelism"},

	{Long: "gpu-memory-utilization", Type: domain.FlagTypeFloat, FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(1.0), HelpText: "Fraction of GPU memory available to the engine", Group: "memory"},
	{Long: "max-num-seqs", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(4096), HelpText: "Maximum concurrent sequences", Group: "memory"},
	{Long: "max-total-tokens", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), HelpText: "Maximum total scheduled tokens", Group: "memory"},
	{Long: "chunked-prefill-size", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(-1), HelpText: "Chunk size for prefill scheduling (-1 selects automatically)", Group: "memory"},
	{Long: "max-prefill-tokens", Type: domain.FlagTypeInt, Default: float64(8192), Min: ptrutil.Ptr(1), HelpText: "Maximum tokens admitted to one prefill batch", Group: "memory"},

	{Long: "attention-backend", Type: domain.FlagTypeEnum, EnumValues: []string{"mha", "mla", "fa3", "fa4", "triton", "flashinfer", "trtllm", "trtllm_mla", "flashmla", "tokenspeed_mla", "hybrid_linear_attn"}, HelpText: "Attention kernel backend", Group: "performance"},
	{Long: "moe-backend", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "triton", "gluon", "flashinfer_trtllm", "flashinfer_cutlass", "flashinfer_cutedsl", "deep_gemm", "mega_moe"}, Default: "auto", HelpText: "Mixture-of-experts kernel backend", Group: "performance"},
	{Long: "sampling-backend", Type: domain.FlagTypeEnum, EnumValues: []string{"greedy", "flashinfer", "flashinfer_full", "triton", "triton_full"}, HelpText: "Sampling kernel backend", Group: "performance"},
	{Long: "grammar-backend", Type: domain.FlagTypeEnum, EnumValues: []string{"xgrammar", "none"}, HelpText: "Structured-output grammar backend", Group: "performance"},
	{Long: "enforce-eager", Type: domain.FlagTypeBool, Default: false, HelpText: "Disable CUDA graph execution", Group: "performance"},
	{Long: "max-cudagraph-capture-size", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1), HelpText: "Maximum batch size captured in CUDA graphs", Group: "performance"},

	{Long: "speculative-algorithm", Type: domain.FlagTypeEnum, EnumValues: []string{"EAGLE3", "MTP", "DFLASH", "DSPARK"}, HelpText: "Speculative decoding algorithm", Group: "speculative"},
	{Long: "speculative-config", Type: domain.FlagTypeString, HelpText: "Free-form JSON speculative decoding configuration", Group: "speculative"},

	{Long: "chat-template", Type: domain.FlagTypeString, HelpText: "Chat template name or path forwarded to the gateway", Group: "gateway"},
	{Long: "tool-call-parser", Type: domain.FlagTypeString, HelpText: "Tool-call parser used by the gateway", Group: "gateway"},
	{Long: "reasoning-parser", Type: domain.FlagTypeString, HelpText: "Reasoning parser used by the engine and gateway", Group: "gateway"},

	{Long: "enable-metrics", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable metrics collection", Group: "observability"},
	{Long: "prometheus-port", Type: domain.FlagTypeInt, Min: ptrutil.Ptr(1024), Max: ptrutil.Ptr(65535), HelpText: "Prometheus metrics port", Group: "observability"},
	{Long: "log-level", Type: domain.FlagTypeEnum, EnumValues: []string{"debug", "info", "warn", "error"}, Default: "info", HelpText: "Gateway log level", Group: "observability"},
}

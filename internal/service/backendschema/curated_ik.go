package backendschema

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

const (
	ikGroupEssentials  = "Essentials"
	ikGroupModelLoad   = "1. Model Loading"
	ikGroupContext     = "2. Context and Generation"
	ikGroupCPU         = "3. Threads and CPU"
	ikGroupMemory      = "4. Memory and I/O"
	ikGroupDevice      = "5. Devices and GPU"
	ikGroupAttention   = "6. Attention and ik Optimizations"
	ikGroupKV          = "7. KV Cache"
	ikGroupRope        = "8. RoPE and YaRN"
	ikGroupShift       = "9. Context Shift, Checkpoints, and RAM Cache"
	ikGroupSamplers    = "10. Samplers and Sampling"
	ikGroupPenalties   = "11. Penalties"
	ikGroupGrammar     = "12. Grammar and Schema"
	ikGroupChat        = "13. Chat Template and Reasoning"
	ikGroupSpeculative = "14. Speculative Decoding"
	ikGroupMultimodal  = "15. Multimodal"
	ikGroupHTTP        = "16. HTTP Server"
	ikGroupLora        = "17. LoRA"
)

// CuratedIkLlamaSchema returns the hand-curated ik-llama-cpp schema, synced
// against ik_llama.cpp t0002-1002-g5763a901 (backends/ik_llama.cpp). Flag facts
// come from gpt_params_find_arg and gpt_params_print_usage in common/common.cpp:
// its --help uses the pre-arg.cpp format that llamahelp cannot parse, so this
// curated catalog is the only flag source. find_arg is shared by every example,
// so only server-relevant flags are surfaced here. Deliberately omitted: legacy
// stubs that throw common_speculative_legacy_option_error (--draft*,
// --multi-token-prediction, --spec-ngram-*, --suffix-*), superseded by
// --spec-type. Flags whose value spans two argv entries (--spec-replace,
// --control-vector-scaled, --control-vector-layer-range, --lora-scaled) carry
// Arity 2: Profile.Args emits one token per flag, so the validator routes them
// to Profile.ExtraArgs, which is passed through verbatim (BUGS.md S15).
func CuratedIkLlamaSchema() domain.BackendValidationSchema {
	flags := map[string]domain.FlagSpec{}
	add := func(spec domain.FlagSpec) {
		flags[spec.Long] = spec
	}

	for _, spec := range []domain.FlagSpec{
		// 1. Model Loading
		strFlag("hf-repo", "hfr", nil, nil, "Hugging Face repository to download/load a model from.", ikGroupModelLoad, false),
		strFlag("hf-file", "hff", nil, nil, "Specific file inside the Hugging Face repository.", ikGroupModelLoad, false),
		strFlag("hf-token", "hft", nil, nil, "Hugging Face authentication token for private repositories.", ikGroupModelLoad, false),
		strFlag("model-url", "mu", nil, nil, "Model download URL.", ikGroupModelLoad, false),
		strFlag("override-kv", "", nil, nil, "Override model metadata by key as KEY=TYPE:VALUE; repeatable.", ikGroupModelLoad, false),
		strFlag("control-vector", "", nil, nil, "Control vector file applied to the model; repeatable.", ikGroupModelLoad, false),
		withArity(strFlag("control-vector-scaled", "", nil, nil, "Control vector with a scale, as two values: FNAME SCALE.", ikGroupModelLoad, false), 2),
		withArity(strFlag("control-vector-layer-range", "", nil, nil, "Layer range applied to control vectors, as two values: START END.", ikGroupModelLoad, false), 2),
		boolFlag("validate-quants", "vq", nil, false, "Validate quantized data while loading the model.", ikGroupModelLoad),
		boolFlag("no-warmup", "", nil, false, "Skip warming up the model with an empty run.", ikGroupModelLoad),

		// 2. Context and Generation
		intFlag("ctx-size", "c", nil, 0, "Context window size; 0 = loaded from model.", ikGroupContext, ptrutil.Ptr(0), nil),
		intFlag("n-predict", "n", []string{"predict"}, -1, "Number of tokens to generate; -1 = infinite.", ikGroupContext, nil, nil),
		intFlag("batch-size", "b", nil, 2048, "Logical batch size for prompt processing.", ikGroupContext, ptrutil.Ptr(1), nil),
		intFlag("ubatch-size", "ub", nil, 512, "Physical micro-batch size.", ikGroupContext, ptrutil.Ptr(1), nil),
		intFlag("keep", "", nil, 0, "Number of prompt tokens to keep when shifting context.", ikGroupContext, nil, nil),
		intFlag("sequences", "ns", nil, 1, "Number of sequences to decode.", ikGroupContext, ptrutil.Ptr(1), nil),

		// 3. Threads and CPU
		intFlag("threads", "t", nil, nil, "Number of CPU threads for generation; unset detects the core count.", ikGroupCPU, ptrutil.Ptr(1), nil),
		intFlag("threads-batch", "tb", nil, nil, "Number of CPU threads for batch/prefill processing.", ikGroupCPU, nil, nil),
		intFlag("threads-draft", "td", nil, nil, "Number of CPU threads for draft-model generation; unset matches --threads.", ikGroupCPU, nil, nil),
		intFlag("threads-batch-draft", "tbd", nil, nil, "Number of CPU threads for draft-model batch processing.", ikGroupCPU, nil, nil),
		intFlag("threads-mtmd", "tm", nil, nil, "Number of CPU threads for multimodal image processing.", ikGroupCPU, nil, nil),
		enumFlag("numa", "", nil, []string{"distribute", "isolate", "numactl"}, nil, "NUMA strategy for multi-socket machines.", ikGroupCPU),

		// 4. Memory and I/O
		boolFlag("mlock", "", nil, false, "Force the model to stay in RAM (no swap).", ikGroupMemory),
		boolFlag("no-mmap", "", nil, false, "Disable memory mapping of the model file.", ikGroupMemory),
		boolFlag("run-time-repack", "rtr", nil, false, "Repack weights at load time for faster inference; implies --no-mmap.", ikGroupMemory),
		boolFlag("check-tensors", "", nil, false, "Validate model tensors for invalid values at load.", ikGroupMemory),
		boolFlag("transparent-huge-pages", "thp", nil, false, "Back model allocations with transparent huge pages.", ikGroupMemory),
		boolFlag("prefetch-experts", "", nil, false, "Stream mmap'd MoE expert weights into the page cache (Linux).", ikGroupMemory),
		intFlag("prefetch-experts-threads", "", nil, 0, "Number of expert-prefetch workers; 0 = automatic.", ikGroupMemory, ptrutil.Ptr(0), nil),

		// 5. Devices and GPU
		intFlag("n-gpu-layers", "ngl", []string{"gpu-layers"}, -1, "Number of model layers offloaded to GPU; -1 = all.", ikGroupDevice, nil, nil),
		intFlag("gpu-layers-draft", "ngld", []string{"n-gpu-layers-draft"}, nil, "Number of draft-model layers offloaded to GPU.", ikGroupDevice, nil, nil),
		enumFlag("split-mode", "sm", nil, []string{"none", "graph", "layer", "attn"}, "layer", "How to split the model across multiple GPUs.", ikGroupDevice),
		strFlag("tensor-split", "ts", nil, nil, "Per-GPU offload fractions (e.g. 3,1).", ikGroupDevice, false),
		intFlag("main-gpu", "mg", nil, 0, "Primary GPU index for intermediate results.", ikGroupDevice, ptrutil.Ptr(0), nil),
		intFlag("max-gpu", "", nil, 0, "Maximum number of GPUs used at once under split mode 'graph'; 0 = no limit.", ikGroupDevice, ptrutil.Ptr(0), nil),
		strFlag("device", "dev", nil, nil, "Devices selected for offload.", ikGroupDevice, false),
		strFlag("override-tensor", "ot", nil, nil, "Override tensor placement (tensor-name=buffer-type).", ikGroupDevice, false),
		boolFlag("cpu-moe", "cmoe", nil, false, "Keep all MoE expert weights on CPU.", ikGroupDevice),
		intFlag("n-cpu-moe", "ncmoe", nil, nil, "Number of MoE layers kept on CPU.", ikGroupDevice, ptrutil.Ptr(0), nil),
		boolFlag("defer-experts", "", nil, false, "Defer MoE expert weight loading until first use.", ikGroupDevice),
		boolFlag("no-offload-only-active-experts", "no-ooae", nil, false, "Offload every expert instead of only the active ones.", ikGroupDevice),
		strFlag("offload-policy", "op", nil, nil, "Per-layer offload policy as layer_id,0|1 pairs, comma-separated.", ikGroupDevice, false),
		boolFlag("fit", "", nil, false, "Auto-tune unset parameters to fit device memory.", ikGroupDevice),
		intFlag("fit-margin", "", nil, 0, "Memory margin (MiB) reserved by --fit.", ikGroupDevice, ptrutil.Ptr(0), nil),
		strFlag("gpu-fit-margin", "gfm", nil, nil, "Per-layer GPU fit margin as layer_id,margin pairs, comma-separated.", ikGroupDevice, false),
		intFlag("max-extra-alloc", "mea", nil, nil, "Maximum extra VRAM allocation per GPU in MiB.", ikGroupDevice, ptrutil.Ptr(0), nil),
		intFlag("worst-graph-tokens", "wgt", nil, nil, "Token budget for worst-case graph sizing.", ikGroupDevice, nil, nil),
		strFlag("rpc", "", nil, nil, "Comma-separated list of RPC servers.", ikGroupDevice, false),
		strFlag("cuda-params", "cuda", nil, nil, "Comma-separated list of CUDA parameters.", ikGroupDevice, false),
		strFlag("graph-reduce-type", "grt", nil, "f16", "Data type used to exchange data between GPUs under split mode 'graph'.", ikGroupDevice, false),
		strFlag("graph-attn-precision", "gap", nil, "f16", "Flash-attention precision under split mode 'graph'.", ikGroupDevice, false),
		boolFlag("split-mode-graph-scheduling", "smgs", nil, false, "Force split-mode graph scheduling.", ikGroupDevice),
		boolFlag("scheduler-async", "sas", nil, false, "Evaluate compute graphs asynchronously.", ikGroupDevice),

		// 6. Attention and ik Optimizations
		enumFlag("flash-attn", "fa", nil, []string{"auto", "on", "off", "1", "0"}, "on", "Flash-attention mode; 1/0 are accepted as on/off.", ikGroupAttention),
		intFlag("mla-use", "mla", nil, 3, "Multi-Latent Attention (MLA) implementation level.", ikGroupAttention, ptrutil.Ptr(0), ptrutil.Ptr(3)),
		intFlag("attention-max-batch", "amb", nil, 0, "Maximum attention batch size; 0 = unlimited.", ikGroupAttention, ptrutil.Ptr(0), nil),
		boolFlag("no-fused-moe", "", nil, false, "Disable fused MoE kernels (enabled by default).", ikGroupAttention),
		boolFlag("grouped-expert-routing", "ger", nil, false, "Enable grouped expert routing for MoE models.", ikGroupAttention),
		boolFlag("no-fused-up-gate", "", nil, false, "Disable fused up/gate matmul kernels.", ikGroupAttention),
		boolFlag("no-fused-mul-multiadd", "", nil, false, "Disable fused mul/multiadd kernels.", ikGroupAttention),
		boolFlag("merge-qkv", "mqkv", nil, false, "Merge the Q, K and V projections at load time.", ikGroupAttention),
		boolFlag("merge-up-gate-experts", "muge", nil, false, "Merge ffn_up and ffn_gate expert tensors at load time.", ikGroupAttention),
		strFlag("smart-expert-reduction", "ser", nil, nil, "Smart expert reduction as K,thresh (disabled at -1,0).", ikGroupAttention, false),
		boolFlag("graph-reuse", "gr", []string{"no-graph-reuse"}, true, "Reuse compute graphs across iterations.", ikGroupAttention),
		intFlag("grp-attn-n", "gan", nil, 1, "Group-attention factor.", ikGroupAttention, ptrutil.Ptr(1), nil),
		intFlag("grp-attn-w", "gaw", nil, 512, "Group-attention width.", ikGroupAttention, ptrutil.Ptr(1), nil),
		boolFlag("dsa", "", nil, false, "Enable DeepSeek Sparse Attention (DSA).", ikGroupAttention),
		boolFlag("fused-indexer-topk", "fidx", nil, true, "Fuse the DSA indexer top-k op (DSA only).", ikGroupAttention),
		intFlag("dsa-top-k", "dsatk", nil, -1, "Top-k value used by the DSA indexer; -1 = model default.", ikGroupAttention, nil, nil),

		// 7. KV Cache
		enumFlag("cache-type-k", "ctk", nil, ikCacheTypes(), "f16", "Key cache quantization type.", ikGroupKV),
		enumFlag("cache-type-v", "ctv", nil, ikCacheTypes(), "f16", "Value cache quantization type.", ikGroupKV),
		enumFlag("cache-type-k-draft", "ctkd", nil, ikCacheTypes(), nil, "Draft-model key cache quantization type; unset follows the main cache.", ikGroupKV),
		enumFlag("cache-type-v-draft", "ctvd", nil, ikCacheTypes(), nil, "Draft-model value cache quantization type; unset follows the main cache.", ikGroupKV),
		strFlag("cache-type-k-first", "ctk-first", nil, "f16,-1", "Key cache type for the first N layers as TYPE,N.", ikGroupKV, false),
		strFlag("cache-type-k-last", "ctk-last", nil, "f16,-1", "Key cache type for the last N layers as TYPE,N.", ikGroupKV, false),
		strFlag("cache-type-v-first", "ctv-first", nil, "f16,-1", "Value cache type for the first N layers as TYPE,N.", ikGroupKV, false),
		strFlag("cache-type-v-last", "ctv-last", nil, "f16,-1", "Value cache type for the last N layers as TYPE,N.", ikGroupKV, false),
		enumFlag("indexer-cache-type-k", "ictk", nil, ikCacheTypes(), "f16", "DSA indexer key cache quantization type.", ikGroupKV),
		boolFlag("k-cache-hadamard", "khad", nil, false, "Apply a Hadamard transform to the K cache.", ikGroupKV),
		boolFlag("v-cache-hadamard", "vhad", nil, false, "Apply a Hadamard transform to the V cache.", ikGroupKV),
		boolFlag("no-kv-offload", "nkvo", nil, false, "Keep the KV cache on host memory.", ikGroupKV),
		floatFlag("defrag-thold", "dt", nil, -1.0, "KV cache defrag threshold; -1 disables defrag.", ikGroupKV, nil, nil),
		boolFlag("swa-compress", "", nil, false, "Allocate sliding-window attention layers at window size instead of full context size (opt-in compacted SWA KV cache).", ikGroupKV),

		// 8. RoPE and YaRN
		enumFlag("rope-scaling", "", nil, []string{"none", "linear", "yarn"}, nil, "RoPE frequency scaling method; unset defers to the model (linear otherwise).", ikGroupRope),
		floatFlag("rope-scale", "", nil, nil, "Context expansion factor via RoPE.", ikGroupRope, nil, nil),
		floatFlag("rope-freq-base", "", nil, nil, "RoPE base frequency.", ikGroupRope, nil, nil),
		floatFlag("rope-freq-scale", "", nil, nil, "RoPE frequency scale factor.", ikGroupRope, nil, nil),
		intFlag("yarn-orig-ctx", "", nil, 0, "Original training context for YaRN; 0 uses the model value.", ikGroupRope, nil, nil),
		floatFlag("yarn-ext-factor", "", nil, -1.0, "YaRN extrapolation mix factor; -1 uses the model value.", ikGroupRope, nil, nil),
		floatFlag("yarn-attn-factor", "", nil, -1.0, "YaRN attention magnitude factor; -1 uses the model value.", ikGroupRope, nil, nil),
		floatFlag("yarn-beta-slow", "", nil, -1.0, "YaRN high correction dimension (beta slow); -1 uses the model value.", ikGroupRope, nil, nil),
		floatFlag("yarn-beta-fast", "", nil, -1.0, "YaRN low correction dimension (beta fast); -1 uses the model value.", ikGroupRope, nil, nil),

		// 9. Context Shift, Checkpoints, and RAM Cache
		enumFlag("context-shift", "", nil, []string{"auto", "on", "off", "1", "0"}, "on", "Context-shift mode for infinite generation; 1/0 are accepted as on/off.", ikGroupShift),
		intFlag("ctx-checkpoints", "ctx-ckpt", nil, 32, "Number of context checkpoints to keep per slot.", ikGroupShift, nil, nil),
		intFlag("ctx-checkpoints-interval", "ctx-ckpt-i", nil, 512, "Minimum token interval between context checkpoints; <= 0 disables.", ikGroupShift, nil, nil),
		intFlag("ctx-checkpoints-tolerance", "ctx-ckpt-t", nil, 5, "Tokens before the full prompt at which a checkpoint is created; <= 0 disables.", ikGroupShift, nil, nil),
		enumFlag("ctx-checkpoints-eviction", "ctx-ckpt-e", nil, []string{"auto", "fifo", "variance"}, "variance", "Context-checkpoint eviction policy; auto resolves to variance.", ikGroupShift),
		intFlag("cache-ram", "cram", nil, 8192, "RAM cache size in MiB; -1 = unlimited, 0 = disable.", ikGroupShift, nil, nil),
		intFlag("cache-ram-n-min", "cram-n-min", nil, 0, "Minimum number of cached tokens that triggers the prompt cache.", ikGroupShift, ptrutil.Ptr(0), nil),
		floatFlag("cache-ram-similarity", "crs", nil, 0.5, "Similarity threshold for RAM-cache prompt reuse.", ikGroupShift, nil, nil),

		// 10. Samplers and Sampling
		strFlag("samplers", "", nil, nil, "Sampler chain order.", ikGroupSamplers, false),
		strFlag("sampling-seq", "", nil, nil, "Sampling sequence string.", ikGroupSamplers, false),
		intFlag("seed", "s", nil, -1, "RNG seed; -1 = random.", ikGroupSamplers, nil, nil),
		floatFlag("temp", "", nil, 0.8, "Sampling temperature.", ikGroupSamplers, ptrutil.Ptr(0.0), nil),
		intFlag("top-k", "", nil, 40, "Top-k sampling; 0 disables it.", ikGroupSamplers, ptrutil.Ptr(0), nil),
		floatFlag("top-p", "", nil, 0.95, "Nucleus (top-p) sampling.", ikGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("min-p", "", nil, 0.05, "Minimum relative probability filter.", ikGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("typical", "", nil, 1.0, "Locally typical sampling; 1.0 disables it.", ikGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("tfs", "", nil, 1.0, "Tail-free sampling; 1.0 disables it.", ikGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("top-n-sigma", "", nil, 0.0, "Top-n-sigma sampling threshold; 0 disables it.", ikGroupSamplers, nil, nil),
		floatFlag("xtc-probability", "", nil, 0.0, "XTC sampler probability; 0 disables it.", ikGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("xtc-threshold", "", nil, 1.0, "XTC sampler threshold; disabled above 0.5.", ikGroupSamplers, nil, nil),
		floatFlag("dry-multiplier", "", nil, 0.0, "DRY repetition penalty multiplier; 0 disables it.", ikGroupSamplers, ptrutil.Ptr(0.0), nil),
		floatFlag("dry-base", "", nil, 1.75, "DRY sampling base.", ikGroupSamplers, ptrutil.Ptr(0.0), nil),
		intFlag("dry-allowed-length", "", nil, 2, "Repetition length beyond which DRY applies a penalty.", ikGroupSamplers, ptrutil.Ptr(0), nil),
		intFlag("dry-penalty-last-n", "", nil, -1, "Tokens scanned for DRY repetitions; 0 = disable, -1 = context size.", ikGroupSamplers, nil, nil),
		strFlag("dry-sequence-breaker", "", nil, nil, "DRY sequence breaker characters; repeatable.", ikGroupSamplers, false),
		floatFlag("adaptive-target", "", nil, -1.0, "Adaptive-p target probability; below 0 disables it.", ikGroupSamplers, nil, ptrutil.Ptr(1.0)),
		floatFlag("adaptive-decay", "", nil, 0.9, "Adaptive-p decay rate for target adaptation.", ikGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		boolFlag("adaptive-updt-w-cur", "", nil, false, "Update adaptive-p state with the current probability.", ikGroupSamplers),
		strFlag("allowlist-pieces", "", nil, nil, "Allowlist each token in the argument.", ikGroupSamplers, false),
		strFlag("allowlist-unicode-rule", "", nil, nil, "Allowlist rule for unicode scripts and/or codepoints.", ikGroupSamplers, false),
		strFlag("allowlist-keyword", "", nil, nil, "Keyword that expires earlier allowlist rules when generated.", ikGroupSamplers, false),
		intFlag("allowlist-keyword-delay", "", nil, nil, "Tokens to delay matching for the first allowlist keyword.", ikGroupSamplers, ptrutil.Ptr(0), nil),
		intFlag("banned-n", "", nil, 1, "Tokens banned in the phrase during rewind; -1 = all.", ikGroupSamplers, nil, nil),
		strFlag("banned-string-file", "", nil, nil, "File listing banned strings, one per line.", ikGroupSamplers, false),
		strFlag("expiring-logit-bias-file", "", nil, nil, "File describing expiring logit-bias rules.", ikGroupSamplers, false),
		boolFlag("ignore-eos", "", nil, false, "Ignore the end-of-sequence token.", ikGroupSamplers),
		strFlag("logit-bias", "l", nil, nil, "Token logit bias in TOKEN_ID(+/-)BIAS form.", ikGroupSamplers, false),

		// 11. Penalties
		intFlag("repeat-last-n", "", nil, 64, "Last-N window for repetition penalty.", ikGroupPenalties, nil, nil),
		floatFlag("repeat-penalty", "", nil, 1.0, "Repetition penalty; 1.0 disables it.", ikGroupPenalties, nil, nil),
		floatFlag("presence-penalty", "", nil, 0.0, "Presence penalty for already-seen tokens.", ikGroupPenalties, nil, nil),
		floatFlag("frequency-penalty", "", nil, 0.0, "Frequency penalty proportional to token counts.", ikGroupPenalties, nil, nil),
		boolFlag("penalize-nl", "", nil, false, "Treat newlines as repeatable tokens for penalties.", ikGroupPenalties),
		floatFlag("dynatemp-range", "", nil, 0.0, "Dynamic temperature range; 0.0 disables it.", ikGroupPenalties, ptrutil.Ptr(0.0), nil),
		floatFlag("dynatemp-exp", "", nil, 1.0, "Dynamic temperature exponent.", ikGroupPenalties, nil, nil),
		intFlag("mirostat", "", nil, 0, "Mirostat mode; 0 disables it.", ikGroupPenalties, ptrutil.Ptr(0), ptrutil.Ptr(2)),
		floatFlag("mirostat-lr", "", nil, 0.1, "Mirostat learning rate (eta).", ikGroupPenalties, nil, nil),
		floatFlag("mirostat-ent", "", nil, 5.0, "Mirostat target entropy (tau).", ikGroupPenalties, nil, nil),

		// 12. Grammar and Schema
		strFlag("grammar", "", nil, nil, "GBNF grammar constraining generation.", ikGroupGrammar, false),
		strFlag("grammar-file", "", nil, nil, "Path to a GBNF grammar file.", ikGroupGrammar, false),
		strFlag("json-schema", "j", nil, nil, "JSON Schema constraining generation.", ikGroupGrammar, false),

		// 13. Chat Template and Reasoning
		boolFlag("jinja", "", nil, false, "Enable the Jinja chat-template engine.", ikGroupChat),
		strFlag("chat-template", "", nil, nil, "Chat template name or custom template string.", ikGroupChat, false),
		strFlag("chat-template-file", "", nil, nil, "Path to a Jinja chat template file.", ikGroupChat, false),
		strFlag("chat-template-kwargs", "", nil, nil, "Extra JSON params for the template parser; use --reasoning for enable_thinking.", ikGroupChat, false),
		strFlag("reasoning-format", "", nil, nil, "Reasoning block extraction format (unknown names mapped internally).", ikGroupChat, false),
		enumFlag("reasoning", "rea", nil, []string{"on", "off", "auto", "true", "false", "enabled", "disabled", "1", "0", "-1"}, "auto", "Reasoning mode; auto defers to the model.", ikGroupChat),
		intFlag("reasoning-budget", "", nil, -1, "Thinking token budget; -1 = unrestricted, 0 = immediate answer.", ikGroupChat, nil, nil),
		strFlag("reasoning-budget-message", "", nil, nil, "Message injected before the end-of-thinking tag when the budget runs out.", ikGroupChat, false),
		strFlag("reasoning-tokens", "", nil, nil, "Reasoning tokens excluded when scoring slot reuse.", ikGroupChat, false),
		boolFlag("parallel-tool-calls", "", nil, false, "Enable parallel tool calls.", ikGroupChat),
		boolFlag("no-prefill-assistant", "", nil, false, "Do not prefill the assistant response when the last message is from the assistant.", ikGroupChat),
		boolFlag("skip-chat-parsing", "", nil, false, "Force a pure content parser even when a Jinja template is set.", ikGroupChat),

		// 14. Speculative Decoding
		strFlag("spec-type", "", nil, nil, "Speculative decoding type payload TYPE:k=v,... (none, draft, dflash, dspark, mtp, ngram-cache, ngram-simple, ngram-map-k, ngram-map-k4v, ngram-mod, suffix). dspark is a DFlash-family draft type: it needs a draft model and cross_ctx >= 1, and it is excluded from --spec-autotune.", ikGroupSpeculative, false),
		withArity(strFlag("spec-replace", "", nil, nil, "Draft/target token replacement, as two values: TARGET DRAFT.", ikGroupSpeculative, false), 2),
		boolFlag("spec-autotune", "", nil, false, "Autotune speculative decoding parameters.", ikGroupSpeculative),
		strFlag("model-draft", "md", nil, nil, "Draft model path for speculative decoding.", ikGroupSpeculative, false),
		intFlag("ctx-size-draft", "cd", nil, 0, "Context size for the draft model; 0 = same as the target model.", ikGroupSpeculative, ptrutil.Ptr(0), nil),
		strFlag("device-draft", "devd", nil, nil, "Devices used to offload the draft model.", ikGroupSpeculative, false),
		strFlag("draft-params", "draft", nil, nil, "Comma-separated list of draft model parameters.", ikGroupSpeculative, false),
		floatFlag("p-split", "ps", nil, 0.1, "Speculative decoding split probability.", ikGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		enumFlag("spec-ckpt-mode", "", []string{"recurrent-ckpt-mode"}, []string{"auto", "per-step", "gpu-fallback", "cpu"}, "auto", "Checkpoint strategy for speculative decoding (recurrent/hybrid models); --recurrent-ckpt-mode is a deprecated alias.", ikGroupSpeculative),
		strFlag("mtp-requantize-output-tensor", "mtprot", nil, nil, "Requantize the output tensor to this type for MTP.", ikGroupSpeculative, false),

		// 15. Multimodal
		strFlag("mmproj", "", nil, nil, "Multimodal projector model path.", ikGroupMultimodal, false),
		strFlag("mmproj-url", "", nil, nil, "URL to download the multimodal projector file.", ikGroupMultimodal, false),
		boolFlag("no-mmproj-offload", "", nil, false, "Keep the multimodal projector on CPU instead of the GPU.", ikGroupMultimodal),
		strFlag("mtmd-kq-type", "", nil, "f32", "Data type for the multimodal K*Q product.", ikGroupMultimodal, false),
		strFlag("image", "", []string{"audio"}, nil, "Image or audio file for multimodal models; repeatable.", ikGroupMultimodal, false),
		intFlag("image-min-tokens", "", nil, nil, "Minimum tokens allocated per image.", ikGroupMultimodal, ptrutil.Ptr(0), nil),
		intFlag("image-max-tokens", "", nil, nil, "Maximum tokens allocated per image.", ikGroupMultimodal, ptrutil.Ptr(0), nil),

		// 16. HTTP Server
		strFlag("host", "", nil, "127.0.0.1", "Server listen address.", ikGroupHTTP, false),
		portFlag("port", "", 8080, "HTTP server port.", ikGroupHTTP),
		strFlag("path", "", nil, nil, "Directory of static files to serve.", ikGroupHTTP, false),
		strFlag("api-key", "", nil, nil, "API authentication key.", ikGroupHTTP, false),
		strFlag("api-key-file", "", nil, nil, "File containing API authentication keys.", ikGroupHTTP, false),
		strFlag("ssl-key-file", "", nil, nil, "SSL private key file.", ikGroupHTTP, false),
		strFlag("ssl-cert-file", "", nil, nil, "SSL certificate file.", ikGroupHTTP, false),
		intFlag("timeout", "", nil, 600, "HTTP read/write timeout in seconds.", ikGroupHTTP, ptrutil.Ptr(0), nil),
		intFlag("threads-http", "", nil, -1, "HTTP worker threads; -1 = automatic.", ikGroupHTTP, nil, nil),
		strFlag("system-prompt-file", "", nil, nil, "File containing the default system prompt.", ikGroupHTTP, false),
		enumFlag("log-format", "", nil, []string{"text", "json"}, nil, "Server log format.", ikGroupHTTP),
		boolFlag("metrics", "", nil, false, "Expose Prometheus-compatible metrics.", ikGroupHTTP),
		boolFlag("no-slots", "", nil, false, "Disable the slots monitoring endpoint.", ikGroupHTTP),
		floatFlag("slot-prompt-similarity", "sps", nil, 0.1, "Prompt similarity threshold for slot reuse.", ikGroupHTTP, nil, nil),
		strFlag("slot-save-path", "", nil, nil, "Directory where slot KV caches are saved.", ikGroupHTTP, false),
		boolFlag("send-done", "", nil, false, "Send a 'done' signal to the client when generation completes.", ikGroupHTTP),
		strFlag("sql-save-file", "", nil, nil, "SQLite database file for saved chat history.", ikGroupHTTP, false),
		strFlag("sqlite-zstd-ext-file", "", nil, nil, "Path to the SQLite ZSTD extension used for compression.", ikGroupHTTP, false),
		boolFlag("spm-infill", "", nil, false, "Use the Suffix/Prefix/Middle infill pattern.", ikGroupHTTP),
		strFlag("alias", "a", nil, nil, "Model alias exposed by the API.", ikGroupHTTP, false),
		enumFlag("webui", "", nil, []string{"none", "auto", "llamacpp"}, "auto", "Built-in web UI mode.", ikGroupHTTP),
		boolFlag("webui-mcp-proxy", "", []string{"ui-mcp-proxy"}, false, "Experimental MCP CORS proxy; do not enable on untrusted networks.", ikGroupHTTP),
		boolFlag("embeddings", "", []string{"embedding"}, false, "Enable the embeddings endpoint.", ikGroupHTTP),
		intFlag("parallel", "np", nil, 1, "Number of parallel server slots.", ikGroupHTTP, nil, nil),
		boolFlag("cont-batching", "cb", []string{"no-cont-batching"}, true, "Enable continuous batching across slots.", ikGroupHTTP),

		// 17. LoRA
		strFlag("lora", "", nil, nil, "LoRA adapter path(s).", ikGroupLora, false),
		// ik parses this as two argv tokens ("FNAME S"), unlike llama-server's
		// single "FNAME:SCALE" token — hence Arity 2 here and not there.
		withArity(strFlag("lora-scaled", "", nil, nil, "LoRA adapter with scale, as two values: FNAME S.", ikGroupLora, false), 2),
		boolFlag("lora-init-without-apply", "", nil, false, "Load LoRA adapters without applying them.", ikGroupLora),
	} {
		add(spec)
	}

	return domain.BackendValidationSchema{
		SchemaVersion: 1,
		Kind:          domain.ValidationSchemaCLIFlagsV1,
		BackendKind:   domain.BackendKindIkLlamaCpp,
		BackendID:     "ik-llama-cpp-default",
		Source: domain.SchemaSource{
			GeneratedFrom: "ik_llama.cpp curated reference",
			SourceVersion: "curated-ik-llama-cpp-4867 (t0002-1053-g15dddc60)",
			Editable:      true,
		},
		Flags:        flags,
		Presentation: IkLlamaPresentation(),
	}
}

func ikCacheTypes() []string {
	return []string{"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1", "q6_0", "q8_KV"}
}

// IkLlamaPresentation returns the presentation groups for ik-llama-cpp,
// including a highlighted Essentials shortlist tuned to ik differentiators.
func IkLlamaPresentation() *domain.Presentation {
	return &domain.Presentation{Groups: []domain.PresentationGroup{
		{Name: ikGroupEssentials, Highlighted: true, Flags: []string{
			"ctx-size", "host", "port", "n-gpu-layers", "split-mode", "tensor-split",
			"threads", "batch-size", "ubatch-size", "flash-attn", "mla-use",
			"cache-type-k", "cache-type-v", "cache-ram", "run-time-repack", "override-tensor",
			"n-cpu-moe", "smart-expert-reduction", "parallel", "cont-batching",
			"jinja", "spec-type", "api-key", "alias",
		}},
		{Name: ikGroupModelLoad, Flags: []string{"hf-repo", "hf-file", "hf-token", "model-url", "override-kv", "control-vector", "control-vector-scaled", "control-vector-layer-range", "validate-quants", "no-warmup"}},
		{Name: ikGroupContext, Flags: []string{"ctx-size", "n-predict", "batch-size", "ubatch-size", "keep", "sequences"}},
		{Name: ikGroupCPU, Flags: []string{"threads", "threads-batch", "threads-draft", "threads-batch-draft", "threads-mtmd", "numa"}},
		{Name: ikGroupMemory, Flags: []string{"mlock", "no-mmap", "run-time-repack", "check-tensors", "transparent-huge-pages", "prefetch-experts", "prefetch-experts-threads"}},
		{Name: ikGroupDevice, Flags: []string{"n-gpu-layers", "gpu-layers-draft", "split-mode", "tensor-split", "main-gpu", "max-gpu", "device", "override-tensor", "cpu-moe", "n-cpu-moe", "defer-experts", "no-offload-only-active-experts", "offload-policy", "fit", "fit-margin", "gpu-fit-margin", "max-extra-alloc", "worst-graph-tokens", "rpc", "cuda-params", "graph-reduce-type", "graph-attn-precision", "split-mode-graph-scheduling", "scheduler-async"}},
		{Name: ikGroupAttention, Flags: []string{"flash-attn", "mla-use", "attention-max-batch", "no-fused-moe", "grouped-expert-routing", "no-fused-up-gate", "no-fused-mul-multiadd", "merge-qkv", "merge-up-gate-experts", "smart-expert-reduction", "graph-reuse", "grp-attn-n", "grp-attn-w", "dsa", "fused-indexer-topk", "dsa-top-k"}},
		{Name: ikGroupKV, Flags: []string{"cache-type-k", "cache-type-v", "cache-type-k-draft", "cache-type-v-draft", "cache-type-k-first", "cache-type-k-last", "cache-type-v-first", "cache-type-v-last", "indexer-cache-type-k", "k-cache-hadamard", "v-cache-hadamard", "no-kv-offload", "defrag-thold", "swa-compress"}},
		{Name: ikGroupRope, Flags: []string{"rope-scaling", "rope-scale", "rope-freq-base", "rope-freq-scale", "yarn-orig-ctx", "yarn-ext-factor", "yarn-attn-factor", "yarn-beta-slow", "yarn-beta-fast"}},
		{Name: ikGroupShift, Flags: []string{"context-shift", "ctx-checkpoints", "ctx-checkpoints-interval", "ctx-checkpoints-tolerance", "ctx-checkpoints-eviction", "cache-ram", "cache-ram-n-min", "cache-ram-similarity"}},
		{Name: ikGroupSamplers, Flags: []string{"samplers", "sampling-seq", "seed", "temp", "top-k", "top-p", "min-p", "typical", "tfs", "top-n-sigma", "xtc-probability", "xtc-threshold", "dry-multiplier", "dry-base", "dry-allowed-length", "dry-penalty-last-n", "dry-sequence-breaker", "adaptive-target", "adaptive-decay", "adaptive-updt-w-cur", "allowlist-pieces", "allowlist-unicode-rule", "allowlist-keyword", "allowlist-keyword-delay", "banned-n", "banned-string-file", "expiring-logit-bias-file", "ignore-eos", "logit-bias"}},
		{Name: ikGroupPenalties, Flags: []string{"repeat-last-n", "repeat-penalty", "presence-penalty", "frequency-penalty", "penalize-nl", "dynatemp-range", "dynatemp-exp", "mirostat", "mirostat-lr", "mirostat-ent"}},
		{Name: ikGroupGrammar, Flags: []string{"grammar", "grammar-file", "json-schema"}},
		{Name: ikGroupChat, Flags: []string{"jinja", "chat-template", "chat-template-file", "chat-template-kwargs", "reasoning-format", "reasoning", "reasoning-budget", "reasoning-budget-message", "reasoning-tokens", "parallel-tool-calls", "no-prefill-assistant", "skip-chat-parsing"}},
		{Name: ikGroupSpeculative, Flags: []string{"spec-type", "spec-replace", "spec-autotune", "model-draft", "ctx-size-draft", "device-draft", "draft-params", "p-split", "spec-ckpt-mode", "mtp-requantize-output-tensor"}},
		{Name: ikGroupMultimodal, Flags: []string{"mmproj", "mmproj-url", "no-mmproj-offload", "mtmd-kq-type", "image", "image-min-tokens", "image-max-tokens"}},
		{Name: ikGroupHTTP, Flags: []string{"host", "port", "path", "api-key", "api-key-file", "ssl-key-file", "ssl-cert-file", "timeout", "threads-http", "system-prompt-file", "log-format", "metrics", "no-slots", "slot-prompt-similarity", "slot-save-path", "send-done", "sql-save-file", "sqlite-zstd-ext-file", "spm-infill", "alias", "webui", "webui-mcp-proxy", "embeddings", "parallel", "cont-batching"}},
		{Name: ikGroupLora, Flags: []string{"lora", "lora-scaled", "lora-init-without-apply"}},
	}}
}

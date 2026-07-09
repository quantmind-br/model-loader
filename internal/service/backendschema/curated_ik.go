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

// CuratedIkLlamaSchema returns the hand-curated ik-llama-cpp schema.
// ik_llama.cpp's --help uses the pre-arg.cpp format that llamahelp cannot
// parse, so this curated catalog is the only flag source.
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

		// 2. Context and Generation
		intFlag("ctx-size", "c", nil, 0, "Context window size; 0 = loaded from model.", ikGroupContext, ptrutil.Ptr(0), nil),
		intFlag("n-predict", "n", []string{"predict"}, -1, "Number of tokens to generate; -1 = infinite.", ikGroupContext, nil, nil),
		intFlag("batch-size", "b", nil, 2048, "Logical batch size for prompt processing.", ikGroupContext, ptrutil.Ptr(1), nil),
		intFlag("ubatch-size", "ub", nil, 512, "Physical micro-batch size.", ikGroupContext, ptrutil.Ptr(1), nil),
		intFlag("keep", "", nil, 0, "Number of prompt tokens to keep when shifting context.", ikGroupContext, nil, nil),

		// 3. Threads and CPU
		intFlag("threads", "t", nil, nil, "Number of CPU threads for generation.", ikGroupCPU, ptrutil.Ptr(1), nil),
		intFlag("threads-batch", "tb", nil, nil, "Number of CPU threads for batch/prefill processing.", ikGroupCPU, nil, nil),
		enumFlag("numa", "", nil, []string{"distribute", "isolate", "numactl"}, nil, "NUMA strategy for multi-socket machines.", ikGroupCPU),

		// 4. Memory and I/O
		boolFlag("mlock", "", nil, false, "Force the model to stay in RAM (no swap).", ikGroupMemory),
		boolFlag("no-mmap", "", nil, false, "Disable memory mapping of the model file.", ikGroupMemory),
		boolFlag("run-time-repack", "rtr", nil, false, "Repack weights at load time for faster inference; implies --no-mmap.", ikGroupMemory),
		boolFlag("check-tensors", "", nil, false, "Validate model tensors for invalid values at load.", ikGroupMemory),

		// 5. Devices and GPU
		intFlag("n-gpu-layers", "ngl", []string{"gpu-layers"}, -1, "Number of model layers offloaded to GPU; -1 = all.", ikGroupDevice, nil, nil),
		intFlag("gpu-layers-draft", "ngld", nil, nil, "Number of draft-model layers offloaded to GPU.", ikGroupDevice, nil, nil),
		enumFlag("split-mode", "sm", nil, []string{"none", "graph", "layer"}, "layer", "How to split the model across multiple GPUs.", ikGroupDevice),
		strFlag("tensor-split", "ts", nil, nil, "Per-GPU offload fractions (e.g. 3,1).", ikGroupDevice, false),
		intFlag("main-gpu", "mg", nil, 0, "Primary GPU index for intermediate results.", ikGroupDevice, ptrutil.Ptr(0), nil),
		intFlag("max-gpu", "", nil, nil, "Maximum GPU index to use.", ikGroupDevice, nil, nil),
		strFlag("device", "dev", nil, nil, "Devices selected for offload.", ikGroupDevice, false),
		strFlag("override-tensor", "ot", nil, nil, "Override tensor placement (tensor-name=buffer-type).", ikGroupDevice, false),
		boolFlag("cpu-moe", "cmoe", nil, false, "Keep all MoE expert weights on CPU.", ikGroupDevice),
		intFlag("n-cpu-moe", "ncmoe", nil, nil, "Number of MoE layers kept on CPU.", ikGroupDevice, ptrutil.Ptr(0), nil),
		boolFlag("defer-experts", "", nil, false, "Defer MoE expert weight loading until first use.", ikGroupDevice),
		boolFlag("fit", "", nil, false, "Auto-tune unset parameters to fit device memory.", ikGroupDevice),
		intFlag("fit-margin", "", nil, nil, "Memory margin (MiB) reserved by --fit.", ikGroupDevice, ptrutil.Ptr(0), nil),
		intFlag("worst-graph-tokens", "wgt", nil, nil, "Token budget for worst-case graph sizing.", ikGroupDevice, nil, nil),

		// 6. Attention and ik Optimizations
		enumFlag("flash-attn", "fa", nil, []string{"auto", "on", "off"}, "on", "Flash-attention mode.", ikGroupAttention),
		enumFlag("mla-use", "mla", nil, []string{"0", "1", "2", "3"}, "3", "Multi-Latent Attention (MLA) implementation level.", ikGroupAttention),
		intFlag("attention-max-batch", "amb", nil, 0, "Maximum attention batch size; 0 = unlimited.", ikGroupAttention, ptrutil.Ptr(0), nil),
		boolFlag("no-fused-moe", "", nil, false, "Disable fused MoE kernels (enabled by default).", ikGroupAttention),
		boolFlag("grouped-expert-routing", "ger", nil, false, "Enable grouped expert routing for MoE models.", ikGroupAttention),
		boolFlag("no-fused-up-gate", "", nil, false, "Disable fused up/gate matmul kernels.", ikGroupAttention),
		boolFlag("no-fused-mul-multiadd", "", nil, false, "Disable fused mul/multiadd kernels.", ikGroupAttention),
		strFlag("smart-expert-reduction", "ser", nil, nil, "Smart expert reduction as K,thresh (disabled at -1,0).", ikGroupAttention, false),
		boolFlag("graph-reuse", "gr", []string{"no-graph-reuse"}, false, "Reuse compute graphs across iterations.", ikGroupAttention),
		boolFlag("dsa", "", nil, false, "Enable DeepSeek Sparse Attention (DSA).", ikGroupAttention),
		boolFlag("fused-indexer-topk", "fidx", nil, false, "Fuse DSA indexer top-k selection.", ikGroupAttention),
		intFlag("dsa-top-k", "dsatk", nil, nil, "Top-k value used by DSA indexer.", ikGroupAttention, nil, nil),

		// 7. KV Cache
		enumFlag("cache-type-k", "ctk", nil, ikCacheTypes(), "f16", "Key cache quantization type.", ikGroupKV),
		enumFlag("cache-type-v", "ctv", nil, ikCacheTypes(), "f16", "Value cache quantization type.", ikGroupKV),
		enumFlag("cache-type-k-draft", "ctkd", nil, ikCacheTypes(), "f16", "Draft-model key cache quantization type.", ikGroupKV),
		enumFlag("cache-type-v-draft", "ctvd", nil, ikCacheTypes(), "f16", "Draft-model value cache quantization type.", ikGroupKV),
		boolFlag("no-kv-offload", "nkvo", nil, false, "Keep the KV cache on host memory.", ikGroupKV),
		floatFlag("defrag-thold", "dt", nil, -1.0, "KV cache defrag threshold; -1 disables defrag.", ikGroupKV, nil, nil),

		// 8. RoPE and YaRN
		enumFlag("rope-scaling", "", nil, []string{"none", "linear", "yarn"}, "none", "RoPE frequency scaling method.", ikGroupRope),
		floatFlag("rope-scale", "", nil, nil, "Context expansion factor via RoPE.", ikGroupRope, nil, nil),
		floatFlag("rope-freq-base", "", nil, nil, "RoPE base frequency.", ikGroupRope, nil, nil),
		floatFlag("rope-freq-scale", "", nil, nil, "RoPE frequency scale factor.", ikGroupRope, nil, nil),
		intFlag("yarn-orig-ctx", "", nil, 0, "Original training context for YaRN; 0 uses the model value.", ikGroupRope, nil, nil),
		floatFlag("yarn-ext-factor", "", nil, -1.0, "YaRN extrapolation mix factor.", ikGroupRope, nil, nil),
		floatFlag("yarn-attn-factor", "", nil, 1.0, "YaRN attention magnitude factor.", ikGroupRope, nil, nil),
		floatFlag("yarn-beta-slow", "", nil, 1.0, "YaRN high correction dimension (beta slow).", ikGroupRope, nil, nil),
		floatFlag("yarn-beta-fast", "", nil, 32.0, "YaRN low correction dimension (beta fast).", ikGroupRope, nil, nil),

		// 9. Context Shift, Checkpoints, and RAM Cache
		enumFlag("context-shift", "", nil, []string{"auto", "on", "off"}, nil, "Context-shift mode for infinite generation.", ikGroupShift),
		intFlag("ctx-checkpoints", "", nil, nil, "Number of context checkpoints to keep.", ikGroupShift, nil, nil),
		intFlag("ctx-checkpoints-interval", "", nil, nil, "Token interval between context checkpoints.", ikGroupShift, nil, nil),
		intFlag("ctx-checkpoints-tolerance", "", nil, nil, "Tolerance window for context-checkpoint reuse.", ikGroupShift, nil, nil),
		strFlag("ctx-checkpoints-eviction", "", nil, nil, "Context-checkpoint eviction policy.", ikGroupShift, false),
		intFlag("cache-ram", "cram", nil, 8192, "RAM cache size in MiB; -1 = unlimited, 0 = disable.", ikGroupShift, nil, nil),
		floatFlag("cache-ram-similarity", "crs", nil, nil, "Similarity threshold for RAM-cache prompt reuse.", ikGroupShift, nil, nil),

		// 10. Samplers and Sampling
		strFlag("samplers", "", nil, nil, "Sampler chain order.", ikGroupSamplers, false),
		strFlag("sampling-seq", "", nil, nil, "Sampling sequence string.", ikGroupSamplers, false),
		intFlag("seed", "s", nil, -1, "RNG seed; -1 = random.", ikGroupSamplers, nil, nil),
		floatFlag("temp", "", nil, 0.8, "Sampling temperature.", ikGroupSamplers, ptrutil.Ptr(0.0), nil),
		intFlag("top-k", "", nil, 40, "Top-k sampling; 0 disables it.", ikGroupSamplers, ptrutil.Ptr(0), nil),
		floatFlag("top-p", "", nil, 0.9, "Nucleus (top-p) sampling.", ikGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("min-p", "", nil, 0.1, "Minimum relative probability filter.", ikGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("typical", "", nil, 1.0, "Locally typical sampling; 1.0 disables it.", ikGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("tfs", "", nil, 1.0, "Tail-free sampling; 1.0 disables it.", ikGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("top-n-sigma", "", nil, nil, "Top-n-sigma sampling threshold.", ikGroupSamplers, nil, nil),
		floatFlag("xtc-probability", "", nil, nil, "XTC sampler probability.", ikGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("xtc-threshold", "", nil, nil, "XTC sampler threshold.", ikGroupSamplers, nil, nil),
		boolFlag("ignore-eos", "", nil, false, "Ignore the end-of-sequence token.", ikGroupSamplers),
		strFlag("logit-bias", "l", nil, nil, "Token logit bias in TOKEN_ID(+/-)BIAS form.", ikGroupSamplers, false),

		// 11. Penalties
		intFlag("repeat-last-n", "", nil, 64, "Last-N window for repetition penalty.", ikGroupPenalties, nil, nil),
		floatFlag("repeat-penalty", "", nil, 1.0, "Repetition penalty; 1.0 disables it.", ikGroupPenalties, nil, nil),
		floatFlag("presence-penalty", "", nil, 0.0, "Presence penalty for already-seen tokens.", ikGroupPenalties, nil, nil),
		floatFlag("frequency-penalty", "", nil, 0.0, "Frequency penalty proportional to token counts.", ikGroupPenalties, nil, nil),
		floatFlag("dynatemp-range", "", nil, 0.0, "Dynamic temperature range; 0.0 disables it.", ikGroupPenalties, ptrutil.Ptr(0.0), nil),
		floatFlag("dynatemp-exp", "", nil, 1.0, "Dynamic temperature exponent.", ikGroupPenalties, nil, nil),
		enumFlag("mirostat", "", nil, []string{"0", "1", "2"}, 0, "Mirostat mode; 0 disables it.", ikGroupPenalties),
		floatFlag("mirostat-lr", "", nil, 0.1, "Mirostat learning rate (eta).", ikGroupPenalties, nil, nil),
		floatFlag("mirostat-ent", "", nil, 5.0, "Mirostat target entropy (tau).", ikGroupPenalties, nil, nil),

		// 12. Grammar and Schema
		strFlag("grammar", "", nil, nil, "GBNF grammar constraining generation.", ikGroupGrammar, false),
		strFlag("grammar-file", "", nil, nil, "Path to a GBNF grammar file.", ikGroupGrammar, false),
		strFlag("json-schema", "j", nil, nil, "JSON Schema constraining generation.", ikGroupGrammar, false),

		// 13. Chat Template and Reasoning
		boolFlag("jinja", "", nil, false, "Enable the Jinja chat-template engine.", ikGroupChat),
		strFlag("chat-template", "", nil, nil, "Chat template name or custom template string.", ikGroupChat, false),
		strFlag("reasoning-format", "", nil, nil, "Reasoning block extraction format (unknown names mapped internally).", ikGroupChat, false),

		// 14. Speculative Decoding
		strFlag("spec-type", "", nil, nil, "Speculative decoding type payload TYPE:k=v,... (none, draft, dflash, mtp, ngram-cache, ngram-simple, ngram-map-k, ngram-map-k4v, ngram-mod, suffix).", ikGroupSpeculative, false),
		boolFlag("spec-autotune", "", nil, false, "Autotune speculative decoding parameters.", ikGroupSpeculative),
		strFlag("model-draft", "md", nil, nil, "Draft model path for speculative decoding.", ikGroupSpeculative, false),
		intFlag("ctx-size-draft", "cd", nil, nil, "Context size for the draft model.", ikGroupSpeculative, nil, nil),

		// 15. Multimodal
		strFlag("mmproj", "", nil, nil, "Multimodal projector model path.", ikGroupMultimodal, false),
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
		floatFlag("slot-prompt-similarity", "sps", nil, nil, "Prompt similarity threshold for slot reuse.", ikGroupHTTP, nil, nil),
		strFlag("alias", "a", nil, nil, "Model alias exposed by the API.", ikGroupHTTP, false),
		enumFlag("webui", "", nil, []string{"none", "auto", "llamacpp"}, "auto", "Built-in web UI mode.", ikGroupHTTP),
		boolFlag("embeddings", "", []string{"embedding"}, false, "Enable the embeddings endpoint.", ikGroupHTTP),
		intFlag("parallel", "np", nil, 1, "Number of parallel server slots.", ikGroupHTTP, nil, nil),
		boolFlag("cont-batching", "cb", []string{"no-cont-batching"}, true, "Enable continuous batching across slots.", ikGroupHTTP),

		// 17. LoRA
		strFlag("lora", "", nil, nil, "LoRA adapter path(s).", ikGroupLora, false),
		strFlag("lora-scaled", "", nil, nil, "LoRA adapter with scale in FILE:SCALE form.", ikGroupLora, false),
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
			SourceVersion: "curated-ik-llama-cpp",
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
			"cache-type-k", "cache-type-v", "run-time-repack", "override-tensor",
			"n-cpu-moe", "smart-expert-reduction", "parallel", "cont-batching",
			"api-key", "alias",
		}},
		{Name: ikGroupModelLoad, Flags: []string{"hf-repo", "hf-file", "hf-token", "model-url"}},
		{Name: ikGroupContext, Flags: []string{"ctx-size", "n-predict", "batch-size", "ubatch-size", "keep"}},
		{Name: ikGroupCPU, Flags: []string{"threads", "threads-batch", "numa"}},
		{Name: ikGroupMemory, Flags: []string{"mlock", "no-mmap", "run-time-repack", "check-tensors"}},
		{Name: ikGroupDevice, Flags: []string{"n-gpu-layers", "gpu-layers-draft", "split-mode", "tensor-split", "main-gpu", "max-gpu", "device", "override-tensor", "cpu-moe", "n-cpu-moe", "defer-experts", "fit", "fit-margin", "worst-graph-tokens"}},
		{Name: ikGroupAttention, Flags: []string{"flash-attn", "mla-use", "attention-max-batch", "no-fused-moe", "grouped-expert-routing", "no-fused-up-gate", "no-fused-mul-multiadd", "smart-expert-reduction", "graph-reuse", "dsa", "fused-indexer-topk", "dsa-top-k"}},
		{Name: ikGroupKV, Flags: []string{"cache-type-k", "cache-type-v", "cache-type-k-draft", "cache-type-v-draft", "no-kv-offload", "defrag-thold"}},
		{Name: ikGroupRope, Flags: []string{"rope-scaling", "rope-scale", "rope-freq-base", "rope-freq-scale", "yarn-orig-ctx", "yarn-ext-factor", "yarn-attn-factor", "yarn-beta-slow", "yarn-beta-fast"}},
		{Name: ikGroupShift, Flags: []string{"context-shift", "ctx-checkpoints", "ctx-checkpoints-interval", "ctx-checkpoints-tolerance", "ctx-checkpoints-eviction", "cache-ram", "cache-ram-similarity"}},
		{Name: ikGroupSamplers, Flags: []string{"samplers", "sampling-seq", "seed", "temp", "top-k", "top-p", "min-p", "typical", "tfs", "top-n-sigma", "xtc-probability", "xtc-threshold", "ignore-eos", "logit-bias"}},
		{Name: ikGroupPenalties, Flags: []string{"repeat-last-n", "repeat-penalty", "presence-penalty", "frequency-penalty", "dynatemp-range", "dynatemp-exp", "mirostat", "mirostat-lr", "mirostat-ent"}},
		{Name: ikGroupGrammar, Flags: []string{"grammar", "grammar-file", "json-schema"}},
		{Name: ikGroupChat, Flags: []string{"jinja", "chat-template", "reasoning-format"}},
		{Name: ikGroupSpeculative, Flags: []string{"spec-type", "spec-autotune", "model-draft", "ctx-size-draft"}},
		{Name: ikGroupMultimodal, Flags: []string{"mmproj", "image-min-tokens", "image-max-tokens"}},
		{Name: ikGroupHTTP, Flags: []string{"host", "port", "path", "api-key", "api-key-file", "ssl-key-file", "ssl-cert-file", "timeout", "threads-http", "system-prompt-file", "log-format", "metrics", "no-slots", "slot-prompt-similarity", "alias", "webui", "embeddings", "parallel", "cont-batching"}},
		{Name: ikGroupLora, Flags: []string{"lora", "lora-scaled", "lora-init-without-apply"}},
	}}
}

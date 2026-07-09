package backendschema

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

const (
	buunGroupEssentials  = "Essentials"
	buunGroupModelLoad   = "1. Model Loading"
	buunGroupContext     = "2. Context and Generation"
	buunGroupCPU         = "3. Threads and CPU"
	buunGroupMemory      = "4. Memory and I/O"
	buunGroupDevice      = "5. Devices and GPU"
	buunGroupKV          = "6. KV Cache"
	buunGroupRope        = "7. RoPE and YaRN"
	buunGroupShift       = "8. Context Shift and SWA"
	buunGroupCacheRAM    = "9. RAM Cache, Slots, and Unified KV"
	buunGroupSamplers    = "10. Samplers and Sampling"
	buunGroupPenalties   = "11. Penalties and Advanced Sampling Techniques"
	buunGroupGrammar     = "12. Grammar and Schema"
	buunGroupChat        = "13. Chat Template, Jinja, and Reasoning"
	buunGroupHTTP        = "14. HTTP Server"
	buunGroupMultimodal  = "15. Multimodal (Vision)"
	buunGroupRouter      = "16. Router Server (Multi-Model)"
	buunGroupTools       = "17. Built-in Agent Tools"
	buunGroupEmbeddings  = "18. Embeddings and Reranking"
	buunGroupLora        = "19. LoRA and Control Vectors"
	buunGroupSpeculative = "20. Speculative Decoding"
	buunGroupTTS         = "21. TTS and Vocoder"
)

// CuratedBuunSchema returns the hand-curated buun-llama-cpp schema. The fork is
// based on llama.cpp and extends llama-server with router, reasoning, DFlash,
// turbo KV cache, tool and TTS flags.
func CuratedBuunSchema() domain.BackendValidationSchema {
	flags := map[string]domain.FlagSpec{}
	add := func(spec domain.FlagSpec) {
		flags[spec.Long] = spec
	}

	for _, spec := range []domain.FlagSpec{
		strFlag("hf-repo", "hf", nil, nil, "Downloads/loads a model directly from Hugging Face; quant is optional.", buunGroupModelLoad, false),
		strFlag("hf-file", "hff", nil, nil, "Specific file inside the HF repository, overriding automatic quant selection.", buunGroupModelLoad, false),
		strFlag("hf-token", "", nil, nil, "Hugging Face authentication token for private repositories.", buunGroupModelLoad, false),
		strFlag("model-url", "mu", nil, nil, "Model download URL.", buunGroupModelLoad, false),
		strFlag("docker-repo", "", nil, nil, "Docker repository used by integrated buun fork flows.", buunGroupModelLoad, false),
		boolFlag("offline", "", nil, false, "Forces offline mode using only local cache, with no network.", buunGroupModelLoad),

		intFlag("ctx-size", "c", nil, 4096, "Context window size; 0 uses the value loaded from the model.", buunGroupContext, ptrutil.Ptr(0), nil),
		intFlag("n-predict", "n", []string{"predict"}, -1, "Number of tokens to generate; -1 = infinite generation.", buunGroupContext, nil, nil),
		intFlag("batch-size", "b", nil, 2048, "Maximum logical batch size for prompt processing.", buunGroupContext, ptrutil.Ptr(1), nil),
		intFlag("ubatch-size", "ub", nil, 512, "Maximum physical micro-batch size (throughput/memory tuning).", buunGroupContext, ptrutil.Ptr(1), nil),
		intFlag("keep", "", nil, 0, "Initial prompt tokens to keep during shifting/reuse; -1 keeps all.", buunGroupContext, nil, nil),

		intFlag("threads", "t", nil, nil, "CPU threads used for generation.", buunGroupCPU, ptrutil.Ptr(0), nil),
		intFlag("threads-batch", "tb", nil, nil, "Threads for batch/prefill processing; inherits --threads by default.", buunGroupCPU, ptrutil.Ptr(0), nil),
		intFlag("poll", "", nil, 50, "Polling level while waiting for work; 0 disables it.", buunGroupCPU, ptrutil.Ptr(0), ptrutil.Ptr(100)),
		enumFlag("prio", "", nil, []string{"-1", "0", "1", "2", "3"}, 0, "Process/thread priority: low, normal, medium, high, realtime.", buunGroupCPU),
		enumFlag("numa", "", nil, []string{"distribute", "isolate", "numactl"}, nil, "Optimizations for NUMA machines.", buunGroupCPU),

		boolFlag("mlock", "", nil, false, "Keeps the model in RAM, avoiding swap.", buunGroupMemory),
		boolFlag("mmap", "", []string{"no-mmap"}, true, "Model memory mapping; disabling it may reduce pageouts but makes loading slower.", buunGroupMemory),
		boolFlag("direct-io", "dio", []string{"no-direct-io"}, false, "Uses Direct I/O when available.", buunGroupMemory),
		boolFlag("repack", "", []string{"no-repack"}, true, "Enables/disables weight repacking.", buunGroupMemory),
		boolFlag("op-offload", "", []string{"no-op-offload"}, true, "Offloads tensor operations from host to device.", buunGroupMemory),
		boolFlag("no-host", "", nil, false, "Bypasses the host buffer to allow extra buffers.", buunGroupMemory),
		boolFlag("check-tensors", "", nil, false, "Checks model tensors for invalid values.", buunGroupMemory),

		strFlag("device", "dev", nil, nil, "Selects the devices used for offload.", buunGroupDevice, false),
		boolFlag("list-devices", "", nil, false, "Lists available devices and exits.", buunGroupDevice),
		strFlag("n-gpu-layers", "ngl", []string{"gpu-layers"}, 0, "Number of model layers moved to VRAM; accepts integer, auto, or all.", buunGroupDevice, false),
		enumFlag("split-mode", "sm", nil, []string{"none", "layer", "row", "tensor"}, "layer", "How to split the model across multiple GPUs.", buunGroupDevice),
		strFlag("tensor-split", "ts", nil, nil, "Offload ratio across GPUs (e.g. 3,1).", buunGroupDevice, false),
		intFlag("main-gpu", "mg", nil, 0, "Main GPU for the model/intermediate results (depends on split mode).", buunGroupDevice, ptrutil.Ptr(0), nil),
		enumFlag("fit", "fit", nil, []string{"on", "off"}, "on", "Adjusts unset parameters to fit device memory.", buunGroupDevice),
		strFlag("fit-target", "fitt", nil, nil, "Target memory margin per device used by --fit.", buunGroupDevice, false),
		intFlag("fit-ctx", "fitc", nil, nil, "Minimum context that --fit may configure.", buunGroupDevice, ptrutil.Ptr(0), nil),

		boolFlag("kv-offload", "kvo", []string{"no-kv-offload"}, true, "Controls KV cache offload.", buunGroupKV),
		enumFlag("cache-type-k", "ctk", nil, buunCacheTypes(), "f16", "K cache data type, including buun fork turbo formats.", buunGroupKV),
		enumFlag("cache-type-v", "ctv", nil, buunCacheTypes(), "f16", "V cache data type, including buun fork turbo formats.", buunGroupKV),

		enumFlag("rope-scaling", "", nil, []string{"none", "linear", "yarn"}, "none", "RoPE frequency scaling method.", buunGroupRope),
		floatFlag("rope-scale", "", nil, nil, "Context expansion factor via RoPE.", buunGroupRope, nil, nil),
		floatFlag("rope-freq-base", "", nil, nil, "RoPE base frequency (NTK-aware scaling).", buunGroupRope, nil, nil),
		floatFlag("rope-freq-scale", "", nil, nil, "Frequency scaling factor; expands context by 1/N.", buunGroupRope, nil, nil),
		intFlag("yarn-orig-ctx", "", nil, 0, "Original model context for YaRN; 0 uses the training value.", buunGroupRope, ptrutil.Ptr(0), nil),
		floatFlag("yarn-ext-factor", "", nil, -1.0, "YaRN extrapolation/interpolation factor.", buunGroupRope, nil, nil),
		floatFlag("yarn-attn-factor", "", nil, 1.0, "Attention magnitude adjustment in YaRN.", buunGroupRope, nil, nil),
		floatFlag("yarn-beta-slow", "", nil, 1.0, "YaRN slow/high correction dim parameter.", buunGroupRope, nil, nil),
		floatFlag("yarn-beta-fast", "", nil, 32.0, "YaRN fast/low correction dim parameter.", buunGroupRope, nil, nil),

		boolFlag("swa-full", "", nil, false, "Uses full-size SWA cache.", buunGroupShift),
		boolFlag("context-shift", "", []string{"no-context-shift"}, false, "Controls context shift during infinite generation.", buunGroupShift),

		boolFlag("cache-ram", "", []string{"no-cache-ram"}, false, "Enables RAM cache to speed up KV/prompt reuse.", buunGroupCacheRAM),
		boolFlag("kv-unified", "", []string{"no-kv-unified"}, false, "Uses unified KV cache across slots/models when supported.", buunGroupCacheRAM),
		intFlag("cache-idle-slots", "", nil, nil, "Number of idle slots kept in cache.", buunGroupCacheRAM, ptrutil.Ptr(0), nil),
		intFlag("sleep-idle-seconds", "", nil, nil, "Idle time before reducing activity/suspending slots.", buunGroupCacheRAM, ptrutil.Ptr(0), nil),

		strFlag("samplers", "", nil, "penalties;dry;top_n_sigma;top_k;typ_p;top_p;min_p;xtc;temperature", "Order of samplers applied during generation.", buunGroupSamplers, false),
		intFlag("seed", "s", nil, -1, "RNG seed; -1 = random.", buunGroupSamplers, nil, nil),
		floatFlag("temperature", "", []string{"temp"}, 0.8, "Sampling temperature; higher = more diversity.", buunGroupSamplers, ptrutil.Ptr(0.0), nil),
		intFlag("top-k", "", nil, 40, "Keeps the k most likely tokens; 0 disables it.", buunGroupSamplers, ptrutil.Ptr(0), nil),
		floatFlag("top-p", "", nil, 0.9, "Nucleus sampling; 1.0 disables it.", buunGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("min-p", "", nil, 0.1, "Keeps tokens above a relative minimum probability.", buunGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("typical-p", "", []string{"typical"}, 1.0, "Locally typical sampling; 1.0 disables it.", buunGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("top-n-sigma", "", nil, -1.0, "Top-n-sigma sampling; -1.0 disables it.", buunGroupSamplers, nil, nil),
		boolFlag("ignore-eos", "", nil, false, "Ignores the EOS token and continues generating.", buunGroupSamplers),
		strFlag("logit-bias", "l", nil, nil, "Increases/decreases the chance of specific tokens in TOKEN_ID(+/-)BIAS format.", buunGroupSamplers, false),
		boolFlag("backend-sampling", "", []string{"no-backend-sampling"}, false, "Runs sampling in the backend when supported by the buun fork.", buunGroupSamplers),

		intFlag("repeat-last-n", "", nil, 64, "Recent tokens considered for repetition penalty; -1 = ctx-size.", buunGroupPenalties, nil, nil),
		floatFlag("repeat-penalty", "", nil, 1.0, "Repetition penalty; 1.0 disables it.", buunGroupPenalties, nil, nil),
		floatFlag("presence-penalty", "", nil, 0.0, "Presence penalty; increases the cost of already-seen tokens.", buunGroupPenalties, nil, nil),
		floatFlag("frequency-penalty", "", nil, 0.0, "Penalty proportional to the frequency of already-seen tokens.", buunGroupPenalties, nil, nil),
		floatFlag("dry-multiplier", "", nil, 0.0, "DRY intensity; 0.0 disables it.", buunGroupPenalties, ptrutil.Ptr(0.0), nil),
		floatFlag("dry-base", "", nil, 1.75, "DRY base for penalizing repetitive sequences.", buunGroupPenalties, nil, nil),
		intFlag("dry-allowed-length", "", nil, 2, "Tolerated length before DRY applies.", buunGroupPenalties, ptrutil.Ptr(0), nil),
		intFlag("dry-penalty-last-n", "", nil, -1, "DRY window; -1 = full context.", buunGroupPenalties, nil, nil),
		strFlag("dry-sequence-breaker", "", nil, "\\n, :, \", *", "Breaks that reset DRY.", buunGroupPenalties, false),
		enumFlag("mirostat", "", nil, []string{"0", "1", "2"}, 0, "Mirostat mode; 0 disables it.", buunGroupPenalties),
		floatFlag("mirostat-lr", "", nil, 0.1, "Mirostat learning rate (eta).", buunGroupPenalties, nil, nil),
		floatFlag("mirostat-ent", "", nil, 5.0, "Mirostat target entropy (tau).", buunGroupPenalties, nil, nil),
		floatFlag("dynatemp-range", "", nil, 0.0, "Dynamic temperature range; 0.0 disables it.", buunGroupPenalties, ptrutil.Ptr(0.0), nil),
		floatFlag("dynatemp-exp", "", nil, 1.0, "Dynamic temperature exponent.", buunGroupPenalties, nil, nil),
		floatFlag("adaptive-target", "", nil, nil, "Adaptive-p target; negative values disable.", buunGroupPenalties, nil, ptrutil.Ptr(1.0)),
		floatFlag("adaptive-decay", "", nil, nil, "Adaptive-p decay.", buunGroupPenalties, ptrutil.Ptr(0.0), ptrutil.Ptr(0.99)),

		strFlag("grammar", "", nil, "", "Constrains output with GBNF grammar.", buunGroupGrammar, false),
		strFlag("grammar-file", "", nil, nil, "Reads grammar from a file.", buunGroupGrammar, false),
		strFlag("json-schema", "j", nil, nil, "Constrains output to a JSON Schema.", buunGroupGrammar, false),
		strFlag("json-schema-file", "jf", nil, nil, "Loads JSON Schema from a file.", buunGroupGrammar, false),

		strFlag("chat-template", "", nil, nil, "Defines the chat template (embedded or customized).", buunGroupChat, false),
		strFlag("chat-template-file", "", nil, nil, "Reads the chat template from a Jinja file.", buunGroupChat, false),
		strFlag("chat-template-kwargs", "", nil, nil, "Extra JSON arguments for the template parser.", buunGroupChat, false),
		boolFlag("jinja", "", []string{"no-jinja"}, false, "Toggles the Jinja engine for chat.", buunGroupChat),
		boolFlag("skip-chat-parsing", "", nil, false, "Forces pure content parsing, without structural parsing.", buunGroupChat),
		boolFlag("prefill-assistant", "", []string{"no-prefill-assistant"}, true, "Controls response prefill when the last message is already from the assistant.", buunGroupChat),
		enumFlag("reasoning-format", "", nil, []string{"none", "deepseek", "deepseek-legacy", "auto"}, "auto", "Format used to extract reasoning blocks.", buunGroupChat),
		enumFlag("reasoning", "", nil, []string{"on", "off", "auto"}, "auto", "Controls reasoning emission/use when the model supports it.", buunGroupChat),
		intFlag("reasoning-budget", "", nil, nil, "Token budget for reasoning.", buunGroupChat, ptrutil.Ptr(0), nil),
		strFlag("reasoning-budget-message", "", nil, nil, "Message/instruction used to communicate the reasoning budget to the model.", buunGroupChat, false),

		strFlag("host", "", nil, "127.0.0.1", "Server listen address.", buunGroupHTTP, false),
		portFlag("port", "", 8080, "HTTP server port.", buunGroupHTTP),
		boolFlag("reuse-port", "", nil, false, "Allows multiple sockets on the same port.", buunGroupHTTP),
		strFlag("api-prefix", "", nil, "", "API route prefix (no trailing slash).", buunGroupHTTP, false),
		strFlag("path", "", nil, nil, "Directory of static files to serve.", buunGroupHTTP, false),
		boolFlag("ui", "", []string{"no-ui"}, true, "Toggles the web interface.", buunGroupHTTP),
		strFlag("ui-config", "", nil, nil, "Overrides default UI settings.", buunGroupHTTP, false),
		strFlag("ui-config-file", "", nil, nil, "Loads UI settings from a JSON file.", buunGroupHTTP, false),
		strFlag("api-key", "", nil, nil, "Sets API authentication keys.", buunGroupHTTP, false),
		strFlag("api-key-file", "", nil, nil, "Reads authentication keys from a file.", buunGroupHTTP, false),
		strFlag("ssl-key-file", "", nil, nil, "Chave privada SSL.", buunGroupHTTP, false),
		strFlag("ssl-cert-file", "", nil, nil, "SSL certificate.", buunGroupHTTP, false),
		intFlag("timeout", "to", nil, 600, "Timeout de leitura/escrita (segundos).", buunGroupHTTP, ptrutil.Ptr(0), nil),
		intFlag("threads-http", "", nil, -1, "Threads for HTTP requests; -1 = automatic.", buunGroupHTTP, nil, nil),
		boolFlag("metrics", "", nil, false, "Prometheus-compatible metrics endpoint.", buunGroupHTTP),
		boolFlag("slots", "", []string{"no-slots"}, true, "Exposes slot monitoring endpoint.", buunGroupHTTP),
		boolFlag("props", "", nil, false, "Allows changing global properties via POST /props.", buunGroupHTTP),
		intFlag("parallel", "np", nil, 1, "Parallel server slots; -1 = automatic.", buunGroupHTTP, nil, nil),
		boolFlag("cont-batching", "cb", []string{"no-cont-batching"}, true, "Liga/desliga continuous batching.", buunGroupHTTP),
		boolFlag("cache-prompt", "", []string{"no-cache-prompt"}, true, "Enables prompt reuse/cache.", buunGroupHTTP),
		intFlag("cache-reuse", "", nil, 0, "Minimum chunk size for attempting reuse via KV shifting.", buunGroupHTTP, ptrutil.Ptr(0), nil),
		strFlag("alias", "a", nil, nil, "Model name aliases (API), comma-separated.", buunGroupHTTP, false),

		strFlag("mmproj", "mm", nil, nil, "Projector multimodal.", buunGroupMultimodal, false),
		strFlag("mmproj-url", "mmu", nil, nil, "Multimodal projector URL.", buunGroupMultimodal, false),
		boolFlag("mmproj-auto", "", []string{"no-mmproj-auto", "no-mmproj"}, true, "Automatic multimodal projector use.", buunGroupMultimodal),
		boolFlag("mmproj-offload", "", []string{"no-mmproj-offload"}, true, "Projector offload to GPU.", buunGroupMultimodal),
		intFlag("image-min-tokens", "", nil, nil, "Minimum tokens per image.", buunGroupMultimodal, ptrutil.Ptr(0), nil),
		intFlag("image-max-tokens", "", nil, nil, "Maximum tokens per image.", buunGroupMultimodal, ptrutil.Ptr(0), nil),

		strFlag("models-dir", "", nil, nil, "Directory monitored by the multi-model router.", buunGroupRouter, false),
		strFlag("models-preset", "", nil, nil, "Preset de modelos carregado pelo router multi-modelo.", buunGroupRouter, false),
		intFlag("models-max", "", nil, nil, "Maximum number of models managed/loaded by the router.", buunGroupRouter, ptrutil.Ptr(1), nil),
		boolFlag("models-autoload", "", []string{"no-models-autoload"}, false, "Automatically loads models from the directory/preset.", buunGroupRouter),

		strFlag("tools", "", nil, nil, "List/configuration of built-in agent tools exposed by the server.", buunGroupTools, false),

		boolFlag("embeddings", "", []string{"embedding"}, false, "Embeddings mode.", buunGroupEmbeddings),
		enumFlag("pooling", "", nil, []string{"none", "mean", "cls", "last", "rank"}, nil, "Embedding pooling.", buunGroupEmbeddings),
		intFlag("embd-normalize", "", nil, 2, "Normalization; 2 = Euclidean.", buunGroupEmbeddings, nil, nil),
		boolFlag("reranking", "", []string{"rerank"}, false, "Reranking endpoint.", buunGroupEmbeddings),

		strFlag("lora", "", nil, nil, "LoRA adapters.", buunGroupLora, false),
		strFlag("lora-scaled", "", nil, nil, "LoRA with manual scale in FILE:SCALE format.", buunGroupLora, false),
		boolFlag("lora-init-without-apply", "", nil, false, "Loads LoRA without applying it.", buunGroupLora),
		strFlag("control-vector", "", nil, nil, "Control vector(s).", buunGroupLora, false),
		strFlag("control-vector-scaled", "", nil, nil, "Control vector with scale in FILE:SCALE format.", buunGroupLora, false),
		strFlag("control-vector-layer-range", "", nil, nil, "Layer range applied to control vectors.", buunGroupLora, false),

		listEnumFlag("spec-type", "", nil, []string{"none", "draft-simple", "draft-eagle3", "draft-mtp", "ngram-simple", "ngram-map-k", "ngram-map-k4v", "ngram-mod", "ngram-cache", "suffix", "copyspec", "recycle", "dflash"}, "none", "Speculative decoding type (comma-separated list, e.g. dflash,ngram-mod).", buunGroupSpeculative),
		strFlag("spec-draft-model", "md", nil, nil, "Draft model.", buunGroupSpeculative, false),
		strFlag("spec-draft-hf", "", nil, nil, "HF repo for the draft model.", buunGroupSpeculative, false),
		intFlag("spec-draft-n-max", "", nil, 16, "Maximum tokens proposed by the draft model.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-draft-n-min", "", nil, 0, "Minimum draft tokens.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		floatFlag("spec-draft-p-split", "", nil, 0.1, "Split probability.", buunGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("spec-draft-p-min", "", nil, 0.75, "Minimum probability for the greedy path.", buunGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		strFlag("spec-draft-device", "", nil, nil, "Draft devices; follows --device by default.", buunGroupSpeculative, false),
		strFlag("spec-draft-ngl", "", nil, 0, "Draft layers in VRAM; accepts integer, auto, or all.", buunGroupSpeculative, false),
		intFlag("spec-draft-threads", "", nil, nil, "Draft CPU threads; follows --threads by default.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		boolFlag("spec-draft-backend-sampling", "", []string{"no-spec-draft-backend-sampling"}, false, "Draft sampling in the backend.", buunGroupSpeculative),
		enumFlag("cache-type-k-draft", "", nil, buunCacheTypes(), "f16", "K cache data type used by the draft model.", buunGroupSpeculative),
		enumFlag("cache-type-v-draft", "", nil, buunCacheTypes(), "f16", "V cache data type used by the draft model.", buunGroupSpeculative),
		intFlag("spec-ngram-mod-n-min", "", nil, nil, "Minimum tokens for speculative n-gram.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-mod-n-max", "", nil, nil, "Maximum tokens for speculative n-gram.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-mod-n-match", "", nil, nil, "Lookup length for ngram-mod.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-min", "", nil, nil, "Minimum tokens for buun fork ngram modes.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-max", "", nil, nil, "Maximum tokens for buun fork ngram modes.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-match", "", nil, nil, "Match length for buun fork ngram modes.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-cache-size", "", nil, nil, "Cache size used by speculative ngram-cache.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		boolFlag("spec-dflash-default", "", nil, false, "Enables configuration/defaults for speculative DFlash mode.", buunGroupSpeculative),
		intFlag("dflash-max-slots", "", nil, nil, "Maximum number of slots used by DFlash.", buunGroupSpeculative, ptrutil.Ptr(1), nil),

		strFlag("model-vocoder", "", nil, nil, "Vocoder model used for TTS.", buunGroupTTS, false),
		boolFlag("tts-use-guide-tokens", "", []string{"no-tts-use-guide-tokens"}, false, "Uses guide tokens in the TTS flow.", buunGroupTTS),
	} {
		add(spec)
	}

	return domain.BackendValidationSchema{
		SchemaVersion: 1,
		Kind:          domain.ValidationSchemaCLIFlagsV1,
		BackendKind:   domain.BackendKindBuunLlamaCpp,
		BackendID:     "buun-llama-cpp-default",
		Source: domain.SchemaSource{
			GeneratedFrom: "buun-llama-cpp curated reference",
			SourceVersion: "curated-buun-llama-cpp",
			Editable:      true,
		},
		Flags:        flags,
		Presentation: BuunPresentation(),
	}
}

func buunCacheTypes() []string {
	return []string{"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1", "turbo2", "turbo3", "turbo4", "turbo2_tcq", "turbo3_tcq"}
}

func BuunPresentation() *domain.Presentation {
	return &domain.Presentation{Groups: []domain.PresentationGroup{
		{Name: buunGroupEssentials, Highlighted: true, Flags: []string{"hf-repo", "hf-token", "ctx-size", "host", "port", "n-gpu-layers", "device", "parallel", "threads", "batch-size", "ubatch-size", "cache-type-k", "cache-type-v", "cache-ram", "kv-unified", "cont-batching", "api-key", "alias"}},
		{Name: buunGroupModelLoad, Flags: []string{"hf-repo", "hf-file", "hf-token", "model-url", "docker-repo", "offline"}},
		{Name: buunGroupContext, Flags: []string{"ctx-size", "n-predict", "batch-size", "ubatch-size", "keep"}},
		{Name: buunGroupCPU, Flags: []string{"threads", "threads-batch", "poll", "prio", "numa"}},
		{Name: buunGroupMemory, Flags: []string{"mlock", "mmap", "direct-io", "repack", "op-offload", "no-host", "check-tensors"}},
		{Name: buunGroupDevice, Flags: []string{"device", "list-devices", "n-gpu-layers", "split-mode", "tensor-split", "main-gpu", "fit", "fit-target", "fit-ctx"}},
		{Name: buunGroupKV, Flags: []string{"kv-offload", "cache-type-k", "cache-type-v"}},
		{Name: buunGroupRope, Flags: []string{"rope-scaling", "rope-scale", "rope-freq-base", "rope-freq-scale", "yarn-orig-ctx", "yarn-ext-factor", "yarn-attn-factor", "yarn-beta-slow", "yarn-beta-fast"}},
		{Name: buunGroupShift, Flags: []string{"swa-full", "context-shift"}},
		{Name: buunGroupCacheRAM, Flags: []string{"cache-ram", "kv-unified", "cache-idle-slots", "sleep-idle-seconds"}},
		{Name: buunGroupSamplers, Flags: []string{"samplers", "seed", "temperature", "top-k", "top-p", "min-p", "typical-p", "top-n-sigma", "ignore-eos", "logit-bias", "backend-sampling"}},
		{Name: buunGroupPenalties, Flags: []string{"repeat-last-n", "repeat-penalty", "presence-penalty", "frequency-penalty", "dry-multiplier", "dry-base", "dry-allowed-length", "dry-penalty-last-n", "dry-sequence-breaker", "mirostat", "mirostat-lr", "mirostat-ent", "dynatemp-range", "dynatemp-exp", "adaptive-target", "adaptive-decay"}},
		{Name: buunGroupGrammar, Flags: []string{"grammar", "grammar-file", "json-schema", "json-schema-file"}},
		{Name: buunGroupChat, Flags: []string{"chat-template", "chat-template-file", "chat-template-kwargs", "jinja", "skip-chat-parsing", "prefill-assistant", "reasoning-format", "reasoning", "reasoning-budget", "reasoning-budget-message"}},
		{Name: buunGroupHTTP, Flags: []string{"host", "port", "reuse-port", "api-prefix", "path", "ui", "ui-config", "ui-config-file", "api-key", "api-key-file", "ssl-key-file", "ssl-cert-file", "timeout", "threads-http", "metrics", "slots", "props", "parallel", "cont-batching", "cache-prompt", "cache-reuse", "alias"}},
		{Name: buunGroupMultimodal, Flags: []string{"mmproj", "mmproj-url", "mmproj-auto", "mmproj-offload", "image-min-tokens", "image-max-tokens"}},
		{Name: buunGroupRouter, Flags: []string{"models-dir", "models-preset", "models-max", "models-autoload"}},
		{Name: buunGroupTools, Flags: []string{"tools"}},
		{Name: buunGroupEmbeddings, Flags: []string{"embeddings", "pooling", "embd-normalize", "reranking"}},
		{Name: buunGroupLora, Flags: []string{"lora", "lora-scaled", "lora-init-without-apply", "control-vector", "control-vector-scaled", "control-vector-layer-range"}},
		{Name: buunGroupSpeculative, Flags: []string{"spec-type", "spec-draft-model", "spec-draft-hf", "spec-draft-n-max", "spec-draft-n-min", "spec-draft-p-split", "spec-draft-p-min", "spec-draft-device", "spec-draft-ngl", "spec-draft-threads", "spec-draft-backend-sampling", "cache-type-k-draft", "cache-type-v-draft", "spec-ngram-mod-n-min", "spec-ngram-mod-n-max", "spec-ngram-mod-n-match", "spec-ngram-min", "spec-ngram-max", "spec-ngram-match", "spec-ngram-cache-size", "spec-dflash-default", "dflash-max-slots"}},
		{Name: buunGroupTTS, Flags: []string{"model-vocoder", "tts-use-guide-tokens"}},
	}}
}

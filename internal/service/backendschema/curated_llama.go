package backendschema

import "github.com/quantmind-br/model-loader/internal/domain"

const (
	llamaGroupEssentials  = "Essentials"
	llamaGroupModelLoad   = "1. Model Loading"
	llamaGroupContext     = "2. Context and Generation"
	llamaGroupCPU         = "3. Threads and CPU"
	llamaGroupMemory      = "4. Memory and I/O"
	llamaGroupDevice      = "5. Devices and GPU"
	llamaGroupKV          = "6. KV Cache"
	llamaGroupRope        = "7. RoPE and YaRN"
	llamaGroupShift       = "8. Context Shift and SWA"
	llamaGroupSamplers    = "9. Samplers and Sampling"
	llamaGroupPenalties   = "10. Penalties and Advanced Sampling Techniques"
	llamaGroupGrammar     = "11. Grammar and Schema"
	llamaGroupChat        = "12. Chat Template and Jinja"
	llamaGroupHTTP        = "13. HTTP Server"
	llamaGroupMultimodal  = "14. Multimodal (Vision)"
	llamaGroupEmbeddings  = "15. Embeddings and Reranking"
	llamaGroupLora        = "16. LoRA and Control Vectors"
	llamaGroupSpeculative = "17. Speculative Decoding"
)

// CuratedLlamaSchema returns the hand-curated llama-server schema based on
// docs/model-loader/llama.cpp.md. It intentionally includes only flags that are
// relevant to llama-server runtime/profile configuration.
func CuratedLlamaSchema() domain.BackendValidationSchema {
	flags := map[string]domain.FlagSpec{}
	add := func(spec domain.FlagSpec) {
		flags[spec.Long] = spec
	}

	for _, spec := range []domain.FlagSpec{
		strFlag("hf-repo", "hf", nil, nil, "Downloads/loads a model directly from Hugging Face; quant is optional.", llamaGroupModelLoad, false),
		strFlag("hf-file", "hff", nil, nil, "Specific file inside the HF repository, overriding automatic quant selection.", llamaGroupModelLoad, false),
		strFlag("model-url", "mu", nil, nil, "Model download URL.", llamaGroupModelLoad, false),
		boolFlag("offline", "", nil, false, "Forces offline mode using only local cache, with no network.", llamaGroupModelLoad),

		intFlag("ctx-size", "c", nil, 4096, "Context window size; 0 uses the value loaded from the model.", llamaGroupContext, intPtr(0), nil),
		intFlag("n-predict", "n", []string{"predict"}, -1, "Number of tokens to generate; -1 = infinite generation.", llamaGroupContext, nil, nil),
		intFlag("batch-size", "b", nil, 2048, "Maximum logical batch size for prompt processing.", llamaGroupContext, intPtr(1), nil),
		intFlag("ubatch-size", "ub", nil, 512, "Maximum physical micro-batch size (throughput/memory tuning).", llamaGroupContext, intPtr(1), nil),
		intFlag("keep", "", nil, 0, "Initial prompt tokens to keep during shifting/reuse; -1 keeps all.", llamaGroupContext, nil, nil),

		intFlag("threads", "t", nil, nil, "CPU threads used for generation.", llamaGroupCPU, intPtr(0), nil),
		intFlag("threads-batch", "tb", nil, nil, "Threads for batch/prefill processing; inherits --threads by default.", llamaGroupCPU, intPtr(0), nil),
		intFlag("poll", "", nil, 50, "Polling level while waiting for work; 0 disables it.", llamaGroupCPU, intPtr(0), intPtr(100)),
		enumFlag("prio", "", nil, []string{"-1", "0", "1", "2", "3"}, 0, "Process/thread priority: low, normal, medium, high, realtime.", llamaGroupCPU),
		enumFlag("numa", "", nil, []string{"distribute", "isolate", "numactl"}, nil, "Optimizations for NUMA machines.", llamaGroupCPU),

		boolFlag("mlock", "", nil, false, "Keeps the model in RAM, avoiding swap.", llamaGroupMemory),
		boolFlag("mmap", "", []string{"no-mmap"}, true, "Model memory mapping; disabling it may reduce pageouts but makes loading slower.", llamaGroupMemory),
		boolFlag("direct-io", "dio", []string{"no-direct-io"}, false, "Uses Direct I/O when available.", llamaGroupMemory),
		boolFlag("repack", "", []string{"no-repack"}, true, "Enables/disables weight repacking.", llamaGroupMemory),
		boolFlag("op-offload", "", []string{"no-op-offload"}, true, "Offloads tensor operations from host to device.", llamaGroupMemory),
		boolFlag("no-host", "", nil, false, "Bypasses the host buffer to allow extra buffers.", llamaGroupMemory),
		boolFlag("check-tensors", "", nil, false, "Checks model tensors for invalid values.", llamaGroupMemory),

		strFlag("device", "dev", nil, nil, "Selects the devices used for offload.", llamaGroupDevice, false),
		boolFlag("list-devices", "", nil, false, "Lists available devices and exits.", llamaGroupDevice),
		strFlag("n-gpu-layers", "ngl", []string{"gpu-layers"}, 0, "Number of model layers moved to VRAM; accepts integer, auto, or all.", llamaGroupDevice, false),
		enumFlag("split-mode", "sm", nil, []string{"none", "layer", "row", "tensor"}, "layer", "How to split the model across multiple GPUs.", llamaGroupDevice),
		strFlag("tensor-split", "ts", nil, nil, "Offload ratio across GPUs (e.g. 3,1).", llamaGroupDevice, false),
		intFlag("main-gpu", "mg", nil, 0, "Main GPU for the model/intermediate results (depends on split mode).", llamaGroupDevice, intPtr(0), nil),
		enumFlag("fit", "fit", nil, []string{"on", "off"}, "on", "Adjusts unset parameters to fit device memory.", llamaGroupDevice),
		strFlag("fit-target", "fitt", nil, nil, "Target memory margin per device used by --fit.", llamaGroupDevice, false),
		intFlag("fit-ctx", "fitc", nil, nil, "Minimum context that --fit may configure.", llamaGroupDevice, intPtr(0), nil),

		boolFlag("kv-offload", "kvo", []string{"no-kv-offload"}, true, "Controls KV cache offload.", llamaGroupKV),
		enumFlag("cache-type-k", "ctk", nil, llamaCacheTypes(), "f16", "K cache data type.", llamaGroupKV),
		enumFlag("cache-type-v", "ctv", nil, llamaCacheTypes(), "f16", "V cache data type.", llamaGroupKV),

		enumFlag("rope-scaling", "", nil, []string{"none", "linear", "yarn"}, "none", "RoPE frequency scaling method.", llamaGroupRope),
		floatFlag("rope-scale", "", nil, nil, "Context expansion factor via RoPE.", llamaGroupRope, nil, nil),
		floatFlag("rope-freq-base", "", nil, nil, "RoPE base frequency (NTK-aware scaling).", llamaGroupRope, nil, nil),
		floatFlag("rope-freq-scale", "", nil, nil, "Frequency scaling factor; expands context by 1/N.", llamaGroupRope, nil, nil),
		intFlag("yarn-orig-ctx", "", nil, 0, "Original model context for YaRN; 0 uses the training value.", llamaGroupRope, intPtr(0), nil),
		floatFlag("yarn-ext-factor", "", nil, -1.0, "YaRN extrapolation/interpolation factor.", llamaGroupRope, nil, nil),
		floatFlag("yarn-attn-factor", "", nil, 1.0, "Attention magnitude adjustment in YaRN.", llamaGroupRope, nil, nil),
		floatFlag("yarn-beta-slow", "", nil, 1.0, "YaRN slow/high correction dim parameter.", llamaGroupRope, nil, nil),
		floatFlag("yarn-beta-fast", "", nil, 32.0, "YaRN fast/low correction dim parameter.", llamaGroupRope, nil, nil),

		boolFlag("swa-full", "", nil, false, "Uses full-size SWA cache.", llamaGroupShift),
		boolFlag("context-shift", "", []string{"no-context-shift"}, false, "Controls context shift during infinite generation.", llamaGroupShift),

		strFlag("samplers", "", nil, "penalties;dry;top_n_sigma;top_k;typ_p;top_p;min_p;xtc;temperature", "Order of samplers applied during generation.", llamaGroupSamplers, false),
		intFlag("seed", "s", nil, -1, "RNG seed; -1 = random.", llamaGroupSamplers, nil, nil),
		floatFlag("temperature", "", []string{"temp"}, 0.8, "Sampling temperature; higher = more diversity.", llamaGroupSamplers, floatPtr(0), nil),
		intFlag("top-k", "", nil, 40, "Keeps the k most likely tokens; 0 disables it.", llamaGroupSamplers, intPtr(0), nil),
		floatFlag("top-p", "", nil, 0.9, "Nucleus sampling; 1.0 disables it.", llamaGroupSamplers, floatPtr(0), floatPtr(1)),
		floatFlag("min-p", "", nil, 0.1, "Keeps tokens above a relative minimum probability.", llamaGroupSamplers, floatPtr(0), floatPtr(1)),
		floatFlag("typical-p", "", []string{"typical"}, 1.0, "Locally typical sampling; 1.0 disables it.", llamaGroupSamplers, floatPtr(0), floatPtr(1)),
		floatFlag("top-n-sigma", "", nil, -1.0, "Top-n-sigma sampling; -1.0 disables it.", llamaGroupSamplers, nil, nil),
		boolFlag("ignore-eos", "", nil, false, "Ignores the EOS token and continues generating.", llamaGroupSamplers),
		strFlag("logit-bias", "l", nil, nil, "Increases/decreases the chance of specific tokens in TOKEN_ID(+/-)BIAS format.", llamaGroupSamplers, false),

		intFlag("repeat-last-n", "", nil, 64, "Recent tokens considered for repetition penalty; -1 = ctx-size.", llamaGroupPenalties, nil, nil),
		floatFlag("repeat-penalty", "", nil, 1.0, "Repetition penalty; 1.0 disables it.", llamaGroupPenalties, nil, nil),
		floatFlag("presence-penalty", "", nil, 0.0, "Presence penalty; increases the cost of already-seen tokens.", llamaGroupPenalties, nil, nil),
		floatFlag("frequency-penalty", "", nil, 0.0, "Penalty proportional to the frequency of already-seen tokens.", llamaGroupPenalties, nil, nil),
		floatFlag("dry-multiplier", "", nil, 0.0, "DRY intensity; 0.0 disables it.", llamaGroupPenalties, floatPtr(0), nil),
		floatFlag("dry-base", "", nil, 1.75, "DRY base for penalizing repetitive sequences.", llamaGroupPenalties, nil, nil),
		intFlag("dry-allowed-length", "", nil, 2, "Tolerated length before DRY applies.", llamaGroupPenalties, intPtr(0), nil),
		intFlag("dry-penalty-last-n", "", nil, -1, "DRY window; -1 = full context.", llamaGroupPenalties, nil, nil),
		strFlag("dry-sequence-breaker", "", nil, "\\n, :, \", *", "Breaks that reset DRY.", llamaGroupPenalties, false),
		enumFlag("mirostat", "", nil, []string{"0", "1", "2"}, 0, "Mirostat mode; 0 disables it.", llamaGroupPenalties),
		floatFlag("mirostat-lr", "", nil, 0.1, "Learning rate (eta) do Mirostat.", llamaGroupPenalties, nil, nil),
		floatFlag("mirostat-ent", "", nil, 5.0, "Entropia alvo (tau) do Mirostat.", llamaGroupPenalties, nil, nil),
		floatFlag("dynatemp-range", "", nil, 0.0, "Dynamic temperature range; 0.0 disables it.", llamaGroupPenalties, floatPtr(0), nil),
		floatFlag("dynatemp-exp", "", nil, 1.0, "Dynamic temperature exponent.", llamaGroupPenalties, nil, nil),
		floatFlag("adaptive-target", "", nil, nil, "Alvo do adaptive-p; valores negativos desativam.", llamaGroupPenalties, nil, floatPtr(1)),
		floatFlag("adaptive-decay", "", nil, nil, "Decaimento do adaptive-p.", llamaGroupPenalties, floatPtr(0), floatPtr(0.99)),

		strFlag("grammar", "", nil, "", "Constrains output with GBNF grammar.", llamaGroupGrammar, false),
		strFlag("grammar-file", "", nil, nil, "Reads grammar from a file.", llamaGroupGrammar, false),
		strFlag("json-schema", "j", nil, nil, "Constrains output to a JSON Schema.", llamaGroupGrammar, false),
		strFlag("json-schema-file", "jf", nil, nil, "Loads JSON Schema from a file.", llamaGroupGrammar, false),

		strFlag("chat-template", "", nil, nil, "Define o template de chat (embutido ou customizado).", llamaGroupChat, false),
		strFlag("chat-template-file", "", nil, nil, "Reads the chat template from a Jinja file.", llamaGroupChat, false),
		strFlag("chat-template-kwargs", "", nil, nil, "Argumentos extras em JSON para o parser do template.", llamaGroupChat, false),
		boolFlag("jinja", "", []string{"no-jinja"}, false, "Liga/desliga o engine Jinja para chat.", llamaGroupChat),
		boolFlag("skip-chat-parsing", "", nil, false, "Forces pure content parsing, without structural parsing.", llamaGroupChat),
		boolFlag("prefill-assistant", "", []string{"no-prefill-assistant"}, true, "Controls response prefill when the last message is already from the assistant.", llamaGroupChat),

		strFlag("host", "", nil, "127.0.0.1", "Server listen address.", llamaGroupHTTP, false),
		portFlag("port", "", 8080, "HTTP server port.", llamaGroupHTTP),
		boolFlag("reuse-port", "", nil, false, "Allows multiple sockets on the same port.", llamaGroupHTTP),
		strFlag("api-prefix", "", nil, "", "Prefixo de rota da API (sem barra final).", llamaGroupHTTP, false),
		strFlag("path", "", nil, nil, "Directory of static files to serve.", llamaGroupHTTP, false),
		boolFlag("ui", "", []string{"no-ui"}, true, "Liga/desliga a interface web.", llamaGroupHTTP),
		strFlag("ui-config", "", nil, nil, "Overrides default UI settings.", llamaGroupHTTP, false),
		strFlag("ui-config-file", "", nil, nil, "Loads UI settings from a JSON file.", llamaGroupHTTP, false),
		strFlag("api-key", "", nil, nil, "Sets API authentication keys.", llamaGroupHTTP, false),
		strFlag("api-key-file", "", nil, nil, "Reads authentication keys from a file.", llamaGroupHTTP, false),
		strFlag("ssl-key-file", "", nil, nil, "Chave privada SSL.", llamaGroupHTTP, false),
		strFlag("ssl-cert-file", "", nil, nil, "SSL certificate.", llamaGroupHTTP, false),
		intFlag("timeout", "to", nil, 600, "Timeout de leitura/escrita (segundos).", llamaGroupHTTP, intPtr(0), nil),
		intFlag("threads-http", "", nil, -1, "Threads for HTTP requests; -1 = automatic.", llamaGroupHTTP, nil, nil),
		boolFlag("metrics", "", nil, false, "Prometheus-compatible metrics endpoint.", llamaGroupHTTP),
		boolFlag("slots", "", []string{"no-slots"}, true, "Exposes slot monitoring endpoint.", llamaGroupHTTP),
		boolFlag("props", "", nil, false, "Allows changing global properties via POST /props.", llamaGroupHTTP),
		intFlag("parallel", "np", nil, 1, "Parallel server slots; -1 = automatic.", llamaGroupHTTP, nil, nil),
		boolFlag("cont-batching", "cb", []string{"no-cont-batching"}, true, "Liga/desliga continuous batching.", llamaGroupHTTP),
		boolFlag("cache-prompt", "", []string{"no-cache-prompt"}, true, "Enables prompt reuse/cache.", llamaGroupHTTP),
		intFlag("cache-reuse", "", nil, 0, "Minimum chunk size for attempting reuse via KV shifting.", llamaGroupHTTP, intPtr(0), nil),
		strFlag("alias", "a", nil, nil, "Model name aliases (API), comma-separated.", llamaGroupHTTP, false),

		strFlag("mmproj", "mm", nil, nil, "Projector multimodal.", llamaGroupMultimodal, false),
		strFlag("mmproj-url", "mmu", nil, nil, "URL do projector multimodal.", llamaGroupMultimodal, false),
		boolFlag("mmproj-auto", "", []string{"no-mmproj-auto", "no-mmproj"}, true, "Automatic multimodal projector use.", llamaGroupMultimodal),
		boolFlag("mmproj-offload", "", []string{"no-mmproj-offload"}, true, "Offload do projector para GPU.", llamaGroupMultimodal),
		intFlag("image-min-tokens", "", nil, nil, "Minimum tokens per image.", llamaGroupMultimodal, intPtr(0), nil),
		intFlag("image-max-tokens", "", nil, nil, "Maximum tokens per image.", llamaGroupMultimodal, intPtr(0), nil),

		boolFlag("embeddings", "", []string{"embedding"}, false, "Embeddings mode.", llamaGroupEmbeddings),
		enumFlag("pooling", "", nil, []string{"none", "mean", "cls", "last", "rank"}, nil, "Embedding pooling.", llamaGroupEmbeddings),
		intFlag("embd-normalize", "", nil, 2, "Normalization; 2 = Euclidean.", llamaGroupEmbeddings, nil, nil),
		boolFlag("reranking", "", []string{"rerank"}, false, "Reranking endpoint.", llamaGroupEmbeddings),

		strFlag("lora", "", nil, nil, "LoRA adapters.", llamaGroupLora, false),
		strFlag("lora-scaled", "", nil, nil, "LoRA with manual scale in FILE:SCALE format.", llamaGroupLora, false),
		boolFlag("lora-init-without-apply", "", nil, false, "Loads LoRA without applying it.", llamaGroupLora),
		strFlag("control-vector", "", nil, nil, "Control vector(s).", llamaGroupLora, false),
		strFlag("control-vector-scaled", "", nil, nil, "Control vector with scale in FILE:SCALE format.", llamaGroupLora, false),
		strFlag("control-vector-layer-range", "", nil, nil, "Layer range applied to control vectors.", llamaGroupLora, false),

		enumFlag("spec-type", "", nil, []string{"none", "draft-simple", "draft-eagle3", "draft-mtp", "ngram-simple", "ngram-map-k", "ngram-map-k4v", "ngram-mod", "ngram-cache"}, "none", "Speculative decoding type.", llamaGroupSpeculative),
		strFlag("spec-draft-model", "md", nil, nil, "Draft model.", llamaGroupSpeculative, false),
		strFlag("spec-draft-hf", "", nil, nil, "HF repo for the draft model.", llamaGroupSpeculative, false),
		intFlag("spec-draft-n-max", "", nil, 16, "Maximum tokens proposed by the draft model.", llamaGroupSpeculative, intPtr(0), nil),
		intFlag("spec-draft-n-min", "", nil, 0, "Minimum draft tokens.", llamaGroupSpeculative, intPtr(0), nil),
		floatFlag("spec-draft-p-split", "", nil, 0.1, "Split probability.", llamaGroupSpeculative, floatPtr(0), floatPtr(1)),
		floatFlag("spec-draft-p-min", "", nil, 0.75, "Minimum probability for the greedy path.", llamaGroupSpeculative, floatPtr(0), floatPtr(1)),
		strFlag("spec-draft-device", "", nil, nil, "Draft devices; follows --device by default.", llamaGroupSpeculative, false),
		strFlag("spec-draft-ngl", "", nil, 0, "Draft layers in VRAM; accepts integer, auto, or all.", llamaGroupSpeculative, false),
		intFlag("spec-draft-threads", "", nil, nil, "Draft CPU threads; follows --threads by default.", llamaGroupSpeculative, intPtr(0), nil),
		boolFlag("spec-draft-backend-sampling", "", []string{"no-spec-draft-backend-sampling"}, false, "Draft sampling in the backend.", llamaGroupSpeculative),
		intFlag("spec-ngram-mod-n-min", "", nil, nil, "Minimum tokens for speculative n-gram.", llamaGroupSpeculative, intPtr(0), nil),
		intFlag("spec-ngram-mod-n-max", "", nil, nil, "Maximum tokens for speculative n-gram.", llamaGroupSpeculative, intPtr(0), nil),
		intFlag("spec-ngram-mod-n-match", "", nil, nil, "Lookup length for ngram-mod.", llamaGroupSpeculative, intPtr(0), nil),
	} {
		add(spec)
	}

	return domain.BackendValidationSchema{
		SchemaVersion: 1,
		Kind:          domain.ValidationSchemaCLIFlagsV1,
		BackendKind:   domain.BackendKindLlamaServer,
		BackendID:     "llama-cpp-default",
		Source: domain.SchemaSource{
			GeneratedFrom: "llama.cpp.md curated reference",
			SourceVersion: "curated-llama-server",
			Editable:      true,
		},
		Flags:        flags,
		Presentation: llamaPresentation(),
	}
}

func boolFlag(long, short string, aliases []string, def bool, help, group string) domain.FlagSpec {
	return domain.FlagSpec{Long: long, Short: short, Aliases: aliases, Type: domain.FlagTypeBool, Default: def, HelpText: help, Group: group}
}

func intFlag(long, short string, aliases []string, def any, help, group string, min, max *int) domain.FlagSpec {
	return domain.FlagSpec{Long: long, Short: short, Aliases: aliases, Type: domain.FlagTypeInt, Default: def, HelpText: help, Group: group, Min: min, Max: max}
}

func floatFlag(long, short string, aliases []string, def any, help, group string, min, max *float64) domain.FlagSpec {
	return domain.FlagSpec{Long: long, Short: short, Aliases: aliases, Type: domain.FlagTypeFloat, Default: def, HelpText: help, Group: group, FloatMin: min, FloatMax: max}
}

func strFlag(long, short string, aliases []string, def any, help, group string, required bool) domain.FlagSpec {
	return domain.FlagSpec{Long: long, Short: short, Aliases: aliases, Type: domain.FlagTypeString, Default: def, HelpText: help, Group: group, Required: required}
}

func enumFlag(long, short string, aliases []string, values []string, def any, help, group string) domain.FlagSpec {
	return domain.FlagSpec{Long: long, Short: short, Aliases: aliases, Type: domain.FlagTypeEnum, EnumValues: values, Default: def, HelpText: help, Group: group}
}

func portFlag(long, short string, def int, help, group string) domain.FlagSpec {
	return domain.FlagSpec{Long: long, Short: short, Type: domain.FlagTypeInt, Default: def, HelpText: help, Group: group, Min: intPtr(1), Max: intPtr(65535), IsPort: true}
}

func intPtr(v int) *int { return &v }

func floatPtr(v float64) *float64 { return &v }

func llamaCacheTypes() []string {
	return []string{"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1"}
}

func llamaPresentation() *domain.Presentation {
	return &domain.Presentation{Groups: []domain.PresentationGroup{
		{Name: llamaGroupEssentials, Highlighted: true, Flags: []string{"hf-repo", "ctx-size", "host", "port", "n-gpu-layers", "device", "parallel", "threads", "batch-size", "ubatch-size", "cache-type-k", "cache-type-v", "cont-batching", "api-key", "alias"}},
		{Name: llamaGroupModelLoad, Flags: []string{"hf-repo", "hf-file", "model-url", "offline"}},
		{Name: llamaGroupContext, Flags: []string{"ctx-size", "n-predict", "batch-size", "ubatch-size", "keep"}},
		{Name: llamaGroupCPU, Flags: []string{"threads", "threads-batch", "poll", "prio", "numa"}},
		{Name: llamaGroupMemory, Flags: []string{"mlock", "mmap", "direct-io", "repack", "op-offload", "no-host", "check-tensors"}},
		{Name: llamaGroupDevice, Flags: []string{"device", "list-devices", "n-gpu-layers", "split-mode", "tensor-split", "main-gpu", "fit", "fit-target", "fit-ctx"}},
		{Name: llamaGroupKV, Flags: []string{"kv-offload", "cache-type-k", "cache-type-v"}},
		{Name: llamaGroupRope, Flags: []string{"rope-scaling", "rope-scale", "rope-freq-base", "rope-freq-scale", "yarn-orig-ctx", "yarn-ext-factor", "yarn-attn-factor", "yarn-beta-slow", "yarn-beta-fast"}},
		{Name: llamaGroupShift, Flags: []string{"swa-full", "context-shift"}},
		{Name: llamaGroupSamplers, Flags: []string{"samplers", "seed", "temperature", "top-k", "top-p", "min-p", "typical-p", "top-n-sigma", "ignore-eos", "logit-bias"}},
		{Name: llamaGroupPenalties, Flags: []string{"repeat-last-n", "repeat-penalty", "presence-penalty", "frequency-penalty", "dry-multiplier", "dry-base", "dry-allowed-length", "dry-penalty-last-n", "dry-sequence-breaker", "mirostat", "mirostat-lr", "mirostat-ent", "dynatemp-range", "dynatemp-exp", "adaptive-target", "adaptive-decay"}},
		{Name: llamaGroupGrammar, Flags: []string{"grammar", "grammar-file", "json-schema", "json-schema-file"}},
		{Name: llamaGroupChat, Flags: []string{"chat-template", "chat-template-file", "chat-template-kwargs", "jinja", "skip-chat-parsing", "prefill-assistant"}},
		{Name: llamaGroupHTTP, Flags: []string{"host", "port", "reuse-port", "api-prefix", "path", "ui", "ui-config", "ui-config-file", "api-key", "api-key-file", "ssl-key-file", "ssl-cert-file", "timeout", "threads-http", "metrics", "slots", "props", "parallel", "cont-batching", "cache-prompt", "cache-reuse", "alias"}},
		{Name: llamaGroupMultimodal, Flags: []string{"mmproj", "mmproj-url", "mmproj-auto", "mmproj-offload", "image-min-tokens", "image-max-tokens"}},
		{Name: llamaGroupEmbeddings, Flags: []string{"embeddings", "pooling", "embd-normalize", "reranking"}},
		{Name: llamaGroupLora, Flags: []string{"lora", "lora-scaled", "lora-init-without-apply", "control-vector", "control-vector-scaled", "control-vector-layer-range"}},
		{Name: llamaGroupSpeculative, Flags: []string{"spec-type", "spec-draft-model", "spec-draft-hf", "spec-draft-n-max", "spec-draft-n-min", "spec-draft-p-split", "spec-draft-p-min", "spec-draft-device", "spec-draft-ngl", "spec-draft-threads", "spec-draft-backend-sampling", "spec-ngram-mod-n-min", "spec-ngram-mod-n-max", "spec-ngram-mod-n-match"}},
	}}
}

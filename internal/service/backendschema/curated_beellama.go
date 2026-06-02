package backendschema

import "github.com/quantmind-br/model-loader/internal/domain"

const (
	beeGroupEssentials  = "Essentials"
	beeGroupModelLoad   = "1. Model Loading"
	beeGroupContext     = "2. Context and Generation"
	beeGroupCPU         = "3. Threads and CPU"
	beeGroupMemory      = "4. Memory and I/O"
	beeGroupDevice      = "5. Devices and GPU"
	beeGroupKV          = "6. KV Cache and Flash Attention"
	beeGroupRope        = "7. RoPE and YaRN"
	beeGroupShift       = "8. Context Shift and Checkpoints"
	beeGroupCacheRAM    = "9. RAM Cache, Slots, and Unified KV"
	beeGroupSamplers    = "10. Samplers and Sampling"
	beeGroupPenalties   = "11. Penalties and Advanced Sampling"
	beeGroupGrammar     = "12. Grammar and Schema"
	beeGroupChat        = "13. Chat Template, Jinja, and Reasoning"
	beeGroupHTTP        = "14. HTTP Server"
	beeGroupMultimodal  = "15. Multimodal (Vision)"
	beeGroupLora        = "16. LoRA and Control Vectors"
	beeGroupSpeculative = "17. DFlash and Speculative Decoding"
)

// CuratedBeeLlamaSchema returns the hand-curated schema for the BeeLlama.cpp
// backend (Anbeeld's llama.cpp fork). BeeLlama exposes the standard llama-server
// binary with an upstream-format-identical --help, so the live generator parses
// --help and overlays this curated metadata. This schema is also the complete
// fallback used when the binary cannot be resolved.
//
// The fork-specific surface over upstream llama-server is: TurboQuant/TCQ KV
// cache types, DFlash cross-attention speculative decoding, the adaptive
// draft-max controller (profit/fringe), DDTree tree verification, and a
// reasoning loop guard. Flag metadata below was taken from the live --help of
// the built binary (build 9459).
func CuratedBeeLlamaSchema() domain.BackendValidationSchema {
	flags := map[string]domain.FlagSpec{}
	add := func(spec domain.FlagSpec) {
		flags[spec.Long] = spec
	}

	for _, spec := range []domain.FlagSpec{
		// 1. Model loading
		strFlag("hf-repo", "hf", nil, nil, "Download/load a model directly from Hugging Face; quant is optional.", beeGroupModelLoad, false),
		strFlag("hf-file", "hff", nil, nil, "Specific file inside the HF repository, overriding automatic quant selection.", beeGroupModelLoad, false),
		strFlag("hf-token", "", nil, nil, "Hugging Face authentication token for private repositories.", beeGroupModelLoad, false),
		strFlag("model-url", "mu", nil, nil, "Model download URL.", beeGroupModelLoad, false),
		boolFlag("offline", "", nil, false, "Force offline mode using only the local cache, with no network.", beeGroupModelLoad),
		strFlag("override-kv", "", nil, nil, "Override model metadata by key, KEY=TYPE:VALUE (comma-separated).", beeGroupModelLoad, false),

		// 2. Context and generation
		strFlag("n-gpu-layers", "ngl", []string{"gpu-layers"}, nil, "Number of model layers offloaded to VRAM; accepts an integer, 'auto', or 'all'.", beeGroupDevice, false),
		intFlag("ctx-size", "c", nil, 0, "Context window size; 0 uses the value loaded from the model.", beeGroupContext, intPtr(0), nil),
		intFlag("n-predict", "n", []string{"predict"}, -1, "Number of tokens to generate; -1 = infinite generation.", beeGroupContext, nil, nil),
		intFlag("batch-size", "b", nil, 2048, "Maximum logical batch size for prompt processing.", beeGroupContext, intPtr(1), nil),
		intFlag("ubatch-size", "ub", nil, 512, "Maximum physical micro-batch size (prefill throughput/memory tuning).", beeGroupContext, intPtr(1), nil),
		intFlag("keep", "", nil, 0, "Initial prompt tokens to keep during shifting/reuse; -1 keeps all.", beeGroupContext, nil, nil),

		// 3. Threads and CPU
		intFlag("threads", "t", nil, nil, "CPU threads used for generation.", beeGroupCPU, intPtr(0), nil),
		intFlag("threads-batch", "tb", nil, nil, "Threads for batch/prefill processing; inherits --threads by default.", beeGroupCPU, intPtr(0), nil),
		intFlag("poll", "", nil, 50, "Polling level while waiting for work; 0 disables it.", beeGroupCPU, intPtr(0), intPtr(100)),
		enumFlag("prio", "", nil, []string{"-1", "0", "1", "2", "3"}, 0, "Process/thread priority: low, normal, medium, high, realtime.", beeGroupCPU),
		enumFlag("numa", "", nil, []string{"distribute", "isolate", "numactl"}, nil, "Optimizations for NUMA machines.", beeGroupCPU),

		// 4. Memory and I/O
		boolFlag("mlock", "", nil, false, "Keep the model in RAM, avoiding swap.", beeGroupMemory),
		boolFlag("mmap", "", []string{"no-mmap"}, true, "Memory-map the model; disabling it gives slower load but may reduce pageouts.", beeGroupMemory),
		boolFlag("no-host", "", nil, false, "Bypass the host buffer, allowing extra buffers to be used.", beeGroupMemory),
		boolFlag("check-tensors", "", nil, false, "Check model tensors for invalid values.", beeGroupMemory),

		// 5. Devices and GPU
		strFlag("device", "dev", nil, nil, "Comma-separated list of devices used for offload.", beeGroupDevice, false),
		boolFlag("list-devices", "", nil, false, "List available devices and exit.", beeGroupDevice),
		enumFlag("split-mode", "sm", nil, []string{"none", "layer", "row"}, "layer", "How to split the model across multiple GPUs.", beeGroupDevice),
		strFlag("tensor-split", "ts", nil, nil, "Offload ratio across GPUs (e.g. 3,1).", beeGroupDevice, false),
		intFlag("main-gpu", "mg", nil, 0, "Main GPU for the model/intermediate results (depends on split mode).", beeGroupDevice, intPtr(0), nil),
		strFlag("override-tensor", "ot", nil, nil, "Override tensor buffer type, <pattern>=<buffer type> (comma-separated).", beeGroupDevice, false),
		boolFlag("cpu-moe", "cmoe", nil, false, "Keep all Mixture-of-Experts weights on the CPU.", beeGroupDevice),
		intFlag("n-cpu-moe", "ncmoe", nil, nil, "Keep the first N Mixture-of-Experts layers on the CPU.", beeGroupDevice, intPtr(0), nil),

		// 6. KV cache and flash attention
		boolFlag("kv-offload", "kvo", []string{"no-kv-offload"}, true, "Control KV cache offload to the GPU.", beeGroupKV),
		enumFlag("cache-type-k", "ctk", nil, beellamaCacheTypes(), "f16", "K cache data type, including BeeLlama TurboQuant/TCQ formats.", beeGroupKV),
		enumFlag("cache-type-v", "ctv", nil, beellamaCacheTypes(), "f16", "V cache data type, including BeeLlama TurboQuant/TCQ formats.", beeGroupKV),
		enumFlag("flash-attn", "fa", nil, []string{"on", "off", "auto"}, "auto", "Flash Attention use: on, off, or auto.", beeGroupKV),

		// 7. RoPE and YaRN
		enumFlag("rope-scaling", "", nil, []string{"none", "linear", "yarn"}, nil, "RoPE frequency scaling method.", beeGroupRope),
		floatFlag("rope-scale", "", nil, nil, "Context expansion factor via RoPE.", beeGroupRope, nil, nil),
		floatFlag("rope-freq-base", "", nil, nil, "RoPE base frequency (NTK-aware scaling).", beeGroupRope, nil, nil),
		floatFlag("rope-freq-scale", "", nil, nil, "Frequency scaling factor; expands context by 1/N.", beeGroupRope, nil, nil),
		intFlag("yarn-orig-ctx", "", nil, 0, "Original model context for YaRN; 0 uses the training value.", beeGroupRope, intPtr(0), nil),
		floatFlag("yarn-ext-factor", "", nil, -1.0, "YaRN extrapolation/interpolation factor.", beeGroupRope, nil, nil),
		floatFlag("yarn-attn-factor", "", nil, 1.0, "Attention magnitude adjustment in YaRN.", beeGroupRope, nil, nil),
		floatFlag("yarn-beta-slow", "", nil, 1.0, "YaRN slow/high correction-dim parameter.", beeGroupRope, nil, nil),
		floatFlag("yarn-beta-fast", "", nil, 32.0, "YaRN fast/low correction-dim parameter.", beeGroupRope, nil, nil),

		// 8. Context shift and checkpoints
		boolFlag("swa-full", "", nil, false, "Use a full-size SWA cache.", beeGroupShift),
		boolFlag("context-shift", "", []string{"no-context-shift"}, false, "Control context shift during infinite generation.", beeGroupShift),
		intFlag("ctx-checkpoints", "ctxcp", []string{"swa-checkpoints"}, nil, "Max KV-state checkpoints kept per slot for fast prefix reuse.", beeGroupShift, intPtr(0), nil),
		intFlag("checkpoint-every-n-tokens", "cpent", nil, nil, "Create a checkpoint every N tokens during prefill; -1 disables.", beeGroupShift, nil, nil),

		// 9. RAM cache, slots, unified KV
		intFlag("cache-ram", "cram", nil, 8192, "Max prompt-cache size in MiB; -1 = no limit, 0 = disable RAM snapshots.", beeGroupCacheRAM, intPtr(-1), nil),
		boolFlag("kv-unified", "kvu", []string{"no-kv-unified"}, false, "Use a single unified KV buffer shared across server slots.", beeGroupCacheRAM),

		// 10. Samplers
		intFlag("seed", "s", nil, -1, "RNG seed; -1 = random.", beeGroupSamplers, nil, nil),
		floatFlag("temperature", "", []string{"temp"}, 0.8, "Sampling temperature; higher = more diversity.", beeGroupSamplers, floatPtr(0), nil),
		intFlag("top-k", "", nil, 40, "Keep the k most likely tokens; 0 disables it.", beeGroupSamplers, intPtr(0), nil),
		floatFlag("top-p", "", nil, 0.9, "Nucleus sampling; 1.0 disables it.", beeGroupSamplers, floatPtr(0), floatPtr(1)),
		floatFlag("min-p", "", nil, 0.1, "Keep tokens above a relative minimum probability; 0 disables it.", beeGroupSamplers, floatPtr(0), floatPtr(1)),
		floatFlag("typical-p", "", []string{"typical"}, 1.0, "Locally typical sampling; 1.0 disables it.", beeGroupSamplers, floatPtr(0), floatPtr(1)),
		floatFlag("top-n-sigma", "", nil, -1.0, "Top-n-sigma sampling; -1.0 disables it.", beeGroupSamplers, nil, nil),
		boolFlag("ignore-eos", "", nil, false, "Ignore the EOS token and continue generating.", beeGroupSamplers),
		strFlag("samplers", "", nil, nil, "Order of samplers applied during generation.", beeGroupSamplers, false),

		// 11. Penalties and advanced sampling
		intFlag("repeat-last-n", "", nil, 64, "Recent tokens considered for repetition penalty; -1 = ctx-size.", beeGroupPenalties, nil, nil),
		floatFlag("repeat-penalty", "", nil, 1.0, "Repetition penalty; 1.0 disables it.", beeGroupPenalties, nil, nil),
		floatFlag("presence-penalty", "", nil, 0.0, "Presence penalty; increases the cost of already-seen tokens.", beeGroupPenalties, nil, nil),
		floatFlag("frequency-penalty", "", nil, 0.0, "Penalty proportional to the frequency of already-seen tokens.", beeGroupPenalties, nil, nil),
		floatFlag("dry-multiplier", "", nil, 0.0, "DRY intensity; 0.0 disables it.", beeGroupPenalties, floatPtr(0), nil),
		floatFlag("dry-base", "", nil, 1.75, "DRY base for penalizing repetitive sequences.", beeGroupPenalties, nil, nil),
		intFlag("dry-allowed-length", "", nil, 2, "Tolerated length before DRY applies.", beeGroupPenalties, intPtr(0), nil),
		enumFlag("mirostat", "", nil, []string{"0", "1", "2"}, 0, "Mirostat mode; 0 disables it.", beeGroupPenalties),
		floatFlag("mirostat-lr", "", nil, 0.1, "Mirostat learning rate (eta).", beeGroupPenalties, nil, nil),
		floatFlag("mirostat-ent", "", nil, 5.0, "Mirostat target entropy (tau).", beeGroupPenalties, nil, nil),
		floatFlag("adaptive-target", "", nil, nil, "Adaptive-p: select tokens near this probability (0.0-1.0); negative disables.", beeGroupPenalties, nil, floatPtr(1)),
		floatFlag("adaptive-decay", "", nil, nil, "Adaptive-p: decay rate for target adaptation over time.", beeGroupPenalties, floatPtr(0), floatPtr(1)),

		// 12. Grammar and schema
		strFlag("grammar", "", nil, nil, "Constrain output with a GBNF grammar.", beeGroupGrammar, false),
		strFlag("grammar-file", "", nil, nil, "Read grammar from a file.", beeGroupGrammar, false),
		strFlag("json-schema", "j", nil, nil, "Constrain output to a JSON Schema.", beeGroupGrammar, false),
		strFlag("json-schema-file", "jf", nil, nil, "Load a JSON Schema from a file.", beeGroupGrammar, false),

		// 13. Chat template, jinja, reasoning
		strFlag("chat-template", "", nil, nil, "Set a custom chat template (embedded name or Jinja).", beeGroupChat, false),
		strFlag("chat-template-file", "", nil, nil, "Read the chat template from a Jinja file.", beeGroupChat, false),
		strFlag("chat-template-kwargs", "", nil, nil, "Extra JSON arguments for the template parser (e.g. {\"preserve_thinking\":true}).", beeGroupChat, false),
		boolFlag("jinja", "", []string{"no-jinja"}, true, "Use the Jinja template engine for chat.", beeGroupChat),
		enumFlag("reasoning-format", "", nil, []string{"none", "deepseek", "deepseek-legacy", "auto"}, "auto", "Format used to extract reasoning blocks.", beeGroupChat),
		enumFlag("reasoning", "rea", nil, []string{"on", "off", "auto"}, "auto", "Use reasoning/thinking in the chat: on, off, or auto.", beeGroupChat),
		intFlag("reasoning-budget", "", nil, nil, "Token budget for thinking; -1 unrestricted, 0 immediate end.", beeGroupChat, nil, nil),
		enumFlag("reasoning-loop-guard", "", nil, []string{"off", "force-close", "stop"}, nil, "Reasoning loop guard mode (BeeLlama): off, force-close, or stop.", beeGroupChat),

		// 14. HTTP server
		strFlag("host", "", nil, "127.0.0.1", "Server listen address.", beeGroupHTTP, false),
		portFlag("port", "", 8080, "HTTP server port.", beeGroupHTTP),
		strFlag("api-key", "", nil, nil, "Set API authentication key(s).", beeGroupHTTP, false),
		strFlag("api-key-file", "", nil, nil, "Read authentication keys from a file.", beeGroupHTTP, false),
		strFlag("path", "", nil, nil, "Directory of static files to serve.", beeGroupHTTP, false),
		boolFlag("ui", "", []string{"no-ui"}, true, "Toggle the built-in web interface.", beeGroupHTTP),
		intFlag("timeout", "to", nil, 600, "Read/write timeout in seconds.", beeGroupHTTP, intPtr(0), nil),
		intFlag("threads-http", "", nil, -1, "Threads for HTTP requests; -1 = automatic.", beeGroupHTTP, nil, nil),
		boolFlag("metrics", "", nil, false, "Expose a Prometheus-compatible metrics endpoint.", beeGroupHTTP),
		boolFlag("slots", "", []string{"no-slots"}, true, "Expose the slot-monitoring endpoint.", beeGroupHTTP),
		intFlag("parallel", "np", nil, 1, "Parallel server slots; -1 = automatic.", beeGroupHTTP, nil, nil),
		boolFlag("cont-batching", "cb", []string{"no-cont-batching"}, true, "Toggle continuous batching.", beeGroupHTTP),
		boolFlag("cache-prompt", "", []string{"no-cache-prompt"}, true, "Enable prompt reuse/cache.", beeGroupHTTP),
		strFlag("alias", "a", nil, nil, "Model name alias exposed by the API.", beeGroupHTTP, false),

		// 15. Multimodal (vision)
		strFlag("mmproj", "mm", nil, nil, "Path to a multimodal projector file (vision).", beeGroupMultimodal, false),
		strFlag("mmproj-url", "mmu", nil, nil, "URL to a multimodal projector file.", beeGroupMultimodal, false),
		boolFlag("mmproj-auto", "", []string{"no-mmproj-auto", "no-mmproj"}, true, "Automatically download/use the model's multimodal projector.", beeGroupMultimodal),
		boolFlag("mmproj-offload", "", []string{"no-mmproj-offload"}, true, "Offload the multimodal projector to the GPU; disable to run it on CPU (0 VRAM).", beeGroupMultimodal),

		// 16. LoRA and control vectors
		strFlag("lora", "", nil, nil, "LoRA adapter(s).", beeGroupLora, false),
		strFlag("lora-scaled", "", nil, nil, "LoRA with a manual scale, FILE:SCALE.", beeGroupLora, false),
		strFlag("control-vector", "", nil, nil, "Control vector(s).", beeGroupLora, false),
		strFlag("control-vector-scaled", "", nil, nil, "Control vector with a scale, FILE:SCALE.", beeGroupLora, false),

		// 17. DFlash and speculative decoding
		//
		// NOTE: BeeLlama's --help lists these draft flags with several long forms,
		// e.g. "--spec-draft-hf, -hfd, --hf-repo-draft". The live --help parser
		// canonicalizes such flags to their LAST long form (hf-repo-draft), and
		// mergeWithCurated keeps the parsed Long but adopts the curated Aliases.
		// We therefore include the documented "spec-draft-*" primary name in each
		// flag's Aliases so it remains resolvable by name (the form the quickstart
		// and profiles use) after the merge.
		enumFlag("spec-type", "", nil, []string{"none", "ngram-cache", "ngram-simple", "ngram-map-k", "ngram-map-k4v", "ngram-mod", "suffix", "copyspec", "recycle", "dflash"}, "none", "Speculative decoding strategy. Use 'dflash' with a DFlash drafter.", beeGroupSpeculative),
		strFlag("spec-draft-model", "md", []string{"model-draft", "spec-draft-model"}, nil, "Draft model for speculative decoding (DFlash drafter GGUF).", beeGroupSpeculative, false),
		strFlag("spec-draft-hf", "hfd", []string{"hf-repo-draft", "spec-draft-hf"}, nil, "HF repo for the draft model, <user>/<model>[:quant]; downloads on first run.", beeGroupSpeculative, false),
		strFlag("spec-draft-ngl", "ngld", []string{"n-gpu-layers-draft", "gpu-layers-draft", "spec-draft-ngl"}, nil, "Draft model layers offloaded to VRAM; integer count ('all' = use a high count like 99).", beeGroupSpeculative, false),
		intFlag("spec-draft-n-max", "", []string{"draft", "draft-max", "draft-n"}, 16, "Max tokens to draft per step for speculative decoding.", beeGroupSpeculative, intPtr(0), nil),
		intFlag("spec-draft-n-min", "", []string{"draft-min", "draft-n-min"}, 0, "Min draft tokens for speculative decoding.", beeGroupSpeculative, intPtr(0), nil),
		floatFlag("spec-draft-p-split", "", []string{"draft-p-split"}, 0.10, "Speculative decoding split probability.", beeGroupSpeculative, floatPtr(0), floatPtr(1)),
		floatFlag("spec-draft-p-min", "", []string{"draft-p-min"}, 0.0, "Minimum speculative decoding probability.", beeGroupSpeculative, floatPtr(0), floatPtr(1)),
		intFlag("spec-draft-ctx-size", "cd", []string{"ctx-size-draft", "spec-draft-ctx-size"}, 0, "Prompt context size for the draft model; 0 = loaded from model.", beeGroupSpeculative, intPtr(0), nil),
		strFlag("spec-draft-device", "devd", []string{"device-draft", "spec-draft-device"}, nil, "Devices for offloading the draft model; follows --device by default.", beeGroupSpeculative, false),
		intFlag("spec-draft-top-k", "", []string{"draft-topk"}, 1, "Top-K candidates per drafter position for DDTree branching.", beeGroupSpeculative, intPtr(1), nil),
		floatFlag("spec-draft-temp", "", nil, 0.0, "Drafter sampling temperature for Gumbel sampling; 0 = greedy.", beeGroupSpeculative, floatPtr(0), nil),
		enumFlag("spec-draft-type-k", "ctkd", []string{"cache-type-k-draft", "spec-draft-type-k"}, beellamaCacheTypes(), nil, "K cache data type for the draft model.", beeGroupSpeculative),
		enumFlag("spec-draft-type-v", "ctvd", []string{"cache-type-v-draft", "spec-draft-type-v"}, beellamaCacheTypes(), nil, "V cache data type for the draft model.", beeGroupSpeculative),
		intFlag("spec-branch-budget", "", nil, 0, "DDTree branch nodes beyond the main draft path; 0 = flat DFlash.", beeGroupSpeculative, intPtr(0), nil),
		intFlag("spec-dflash-max-slots", "", nil, 1, "Max concurrent server slots with DFlash state; extra slots fall back to non-spec decode.", beeGroupSpeculative, intPtr(1), nil),
		intFlag("spec-dflash-cross-ctx", "", nil, 512, "DFlash cross-attention window: how many target hidden states the drafter sees.", beeGroupSpeculative, intPtr(0), nil),
		boolFlag("spec-dflash-default", "", nil, false, "Enable the default DFlash speculative-decoding config (requires a draft model).", beeGroupSpeculative),
		boolFlag("spec-default", "", nil, false, "Enable the default speculative-decoding config.", beeGroupSpeculative),
		boolFlag("spec-dm-adaptive", "", []string{"no-spec-dm-adaptive"}, true, "Adaptive draft-max controller that tunes draft depth from acceptance rates.", beeGroupSpeculative),
		enumFlag("spec-dm-controller", "", nil, []string{"fringe", "profit"}, "profit", "Adaptive draft-max controller mode.", beeGroupSpeculative),
	} {
		add(spec)
	}

	return domain.BackendValidationSchema{
		SchemaVersion: 1,
		Kind:          domain.ValidationSchemaCLIFlagsV1,
		BackendKind:   domain.BackendKindBeeLlamaCpp,
		BackendID:     "beellama-cpp-default",
		Source: domain.SchemaSource{
			GeneratedFrom: "beellama.cpp curated reference",
			SourceVersion: "curated-beellama-cpp",
			Editable:      true,
		},
		Flags:        flags,
		Presentation: BeeLlamaPresentation(),
		Rules:        beeLlamaRules(),
	}
}

// beellamaCacheTypes lists the standard llama KV cache types plus the BeeLlama
// fork's TurboQuant and TCQ types, in the order reported by the binary's --help.
func beellamaCacheTypes() []string {
	return []string{"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1", "turbo2", "turbo3", "turbo4", "turbo3_tcq", "turbo2_tcq"}
}

// beeLlamaRules are DFlash-aware cross-field validation rules. They are chosen
// to never false-positive on a valid DFlash profile that supplies its drafter
// via --spec-draft-hf (the single-flag rule engine cannot express
// "draft-model OR draft-hf", so we avoid a hard require on dflash itself).
func beeLlamaRules() []domain.CrossFieldRule {
	return []domain.CrossFieldRule{
		{
			ID:       "beellama-dflash-default-requires-draft",
			When:     domain.Cond{Flag: "spec-dflash-default", Op: "eq", Value: "on"},
			Then:     domain.Effect{Kind: "require", Flag: "spec-draft-model", Message: "--spec-dflash-default needs a draft model: set --spec-draft-model (-md) or --spec-draft-hf."},
			Severity: "warning",
		},
		{
			ID:       "beellama-branch-budget-multigpu",
			When:     domain.Cond{Flag: "spec-branch-budget", Op: "ge", Value: "1"},
			Then:     domain.Effect{Kind: "message", Message: "DDTree tree verification (--spec-branch-budget > 0) is auto-disabled on multi-GPU and is slow; flat DFlash (--spec-branch-budget 0) is recommended."},
			Severity: "warning",
		},
	}
}

// BeeLlamaPresentation lays out the BeeLlama schema for the web editor. The
// highlighted Essentials group leads with the DFlash workflow.
func BeeLlamaPresentation() *domain.Presentation {
	return &domain.Presentation{Groups: []domain.PresentationGroup{
		{Name: beeGroupEssentials, Highlighted: true, Flags: []string{"n-gpu-layers", "ctx-size", "host", "port", "batch-size", "ubatch-size", "flash-attn", "cache-type-k", "cache-type-v", "cache-ram", "kv-unified", "mmproj", "jinja", "reasoning", "spec-type", "spec-draft-hf", "spec-draft-model", "spec-draft-ngl", "spec-dflash-cross-ctx", "spec-dflash-max-slots"}},
		{Name: beeGroupModelLoad, Flags: []string{"hf-repo", "hf-file", "hf-token", "model-url", "offline", "override-kv"}},
		{Name: beeGroupContext, Flags: []string{"ctx-size", "n-predict", "batch-size", "ubatch-size", "keep"}},
		{Name: beeGroupCPU, Flags: []string{"threads", "threads-batch", "poll", "prio", "numa"}},
		{Name: beeGroupMemory, Flags: []string{"mlock", "mmap", "no-host", "check-tensors"}},
		{Name: beeGroupDevice, Flags: []string{"n-gpu-layers", "device", "list-devices", "split-mode", "tensor-split", "main-gpu", "override-tensor", "cpu-moe", "n-cpu-moe"}},
		{Name: beeGroupKV, Flags: []string{"kv-offload", "cache-type-k", "cache-type-v", "flash-attn"}},
		{Name: beeGroupRope, Flags: []string{"rope-scaling", "rope-scale", "rope-freq-base", "rope-freq-scale", "yarn-orig-ctx", "yarn-ext-factor", "yarn-attn-factor", "yarn-beta-slow", "yarn-beta-fast"}},
		{Name: beeGroupShift, Flags: []string{"swa-full", "context-shift", "ctx-checkpoints", "checkpoint-every-n-tokens"}},
		{Name: beeGroupCacheRAM, Flags: []string{"cache-ram", "kv-unified"}},
		{Name: beeGroupSamplers, Flags: []string{"seed", "temperature", "top-k", "top-p", "min-p", "typical-p", "top-n-sigma", "ignore-eos", "samplers"}},
		{Name: beeGroupPenalties, Flags: []string{"repeat-last-n", "repeat-penalty", "presence-penalty", "frequency-penalty", "dry-multiplier", "dry-base", "dry-allowed-length", "mirostat", "mirostat-lr", "mirostat-ent", "adaptive-target", "adaptive-decay"}},
		{Name: beeGroupGrammar, Flags: []string{"grammar", "grammar-file", "json-schema", "json-schema-file"}},
		{Name: beeGroupChat, Flags: []string{"chat-template", "chat-template-file", "chat-template-kwargs", "jinja", "reasoning-format", "reasoning", "reasoning-budget", "reasoning-loop-guard"}},
		{Name: beeGroupHTTP, Flags: []string{"host", "port", "api-key", "api-key-file", "path", "ui", "timeout", "threads-http", "metrics", "slots", "parallel", "cont-batching", "cache-prompt", "alias"}},
		{Name: beeGroupMultimodal, Flags: []string{"mmproj", "mmproj-url", "mmproj-auto", "mmproj-offload"}},
		{Name: beeGroupLora, Flags: []string{"lora", "lora-scaled", "control-vector", "control-vector-scaled"}},
		{Name: beeGroupSpeculative, Flags: []string{"spec-type", "spec-draft-model", "spec-draft-hf", "spec-draft-ngl", "spec-draft-n-max", "spec-draft-n-min", "spec-draft-p-split", "spec-draft-p-min", "spec-draft-ctx-size", "spec-draft-device", "spec-draft-top-k", "spec-draft-temp", "spec-draft-type-k", "spec-draft-type-v", "spec-branch-budget", "spec-dflash-max-slots", "spec-dflash-cross-ctx", "spec-dflash-default", "spec-default", "spec-dm-adaptive", "spec-dm-controller"}},
	}}
}

package backendschema

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

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
// draft-max controller (profit/fringe) with its full tuning surface, DDTree tree
// verification, and a reasoning loop guard with its tuning surface. Flag metadata
// below was synchronized against the BeeLlama source tree at git
// v0.3.1-6-g85e22ea0b (commit 85e22ea0b), reading common/arg.cpp and common.h
// directly as the source of truth.
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
		intFlag("ctx-size", "c", nil, 0, "Context window size; 0 uses the value loaded from the model.", beeGroupContext, ptrutil.Ptr(0), nil),
		intFlag("n-predict", "n", []string{"predict"}, -1, "Number of tokens to generate; -1 = infinite generation.", beeGroupContext, nil, nil),
		intFlag("batch-size", "b", nil, 2048, "Maximum logical batch size for prompt processing.", beeGroupContext, ptrutil.Ptr(1), nil),
		intFlag("ubatch-size", "ub", nil, 512, "Maximum physical micro-batch size (prefill throughput/memory tuning).", beeGroupContext, ptrutil.Ptr(1), nil),
		intFlag("keep", "", nil, 0, "Initial prompt tokens to keep during shifting/reuse; -1 keeps all.", beeGroupContext, nil, nil),

		// 3. Threads and CPU
		intFlag("threads", "t", nil, nil, "CPU threads used for generation.", beeGroupCPU, ptrutil.Ptr(0), nil),
		intFlag("threads-batch", "tb", nil, nil, "Threads for batch/prefill processing; inherits --threads by default.", beeGroupCPU, ptrutil.Ptr(0), nil),
		intFlag("poll", "", nil, 50, "Polling level while waiting for work; 0 disables it.", beeGroupCPU, ptrutil.Ptr(0), ptrutil.Ptr(100)),
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
		enumFlag("split-mode", "sm", nil, []string{"none", "layer", "row", "tensor"}, "layer", "How to split the model across multiple GPUs.", beeGroupDevice),
		strFlag("tensor-split", "ts", nil, nil, "Offload ratio across GPUs (e.g. 3,1).", beeGroupDevice, false),
		intFlag("main-gpu", "mg", nil, 0, "Main GPU for the model/intermediate results (depends on split mode).", beeGroupDevice, ptrutil.Ptr(0), nil),
		strFlag("override-tensor", "ot", nil, nil, "Override tensor buffer type, <pattern>=<buffer type> (comma-separated).", beeGroupDevice, false),
		boolFlag("cpu-moe", "cmoe", nil, false, "Keep all Mixture-of-Experts weights on the CPU.", beeGroupDevice),
		intFlag("n-cpu-moe", "ncmoe", nil, nil, "Keep the first N Mixture-of-Experts layers on the CPU.", beeGroupDevice, ptrutil.Ptr(0), nil),

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
		intFlag("yarn-orig-ctx", "", nil, 0, "Original model context for YaRN; 0 uses the training value.", beeGroupRope, ptrutil.Ptr(0), nil),
		floatFlag("yarn-ext-factor", "", nil, -1.0, "YaRN extrapolation/interpolation factor.", beeGroupRope, nil, nil),
		floatFlag("yarn-attn-factor", "", nil, -1.0, "Attention magnitude adjustment in YaRN; -1 = from model.", beeGroupRope, nil, nil),
		floatFlag("yarn-beta-slow", "", nil, -1.0, "YaRN slow/high correction-dim parameter; -1 = from model.", beeGroupRope, nil, nil),
		floatFlag("yarn-beta-fast", "", nil, -1.0, "YaRN fast/low correction-dim parameter; -1 = from model.", beeGroupRope, nil, nil),

		// 8. Context shift and checkpoints
		boolFlag("swa-full", "", nil, false, "Use a full-size SWA cache.", beeGroupShift),
		boolFlag("context-shift", "", []string{"no-context-shift"}, false, "Control context shift during infinite generation.", beeGroupShift),
		intFlag("ctx-checkpoints", "ctxcp", []string{"swa-checkpoints"}, nil, "Max KV-state checkpoints kept per slot for fast prefix reuse.", beeGroupShift, ptrutil.Ptr(0), nil),
		intFlag("checkpoint-min-step", "cms", nil, 256, "Minimum spacing between context checkpoints in tokens; 0 = no minimum.", beeGroupShift, ptrutil.Ptr(0), nil),

		// 9. RAM cache, slots, unified KV
		intFlag("cache-ram", "cram", nil, 8192, "Max prompt-cache size in MiB; -1 = no limit, 0 = disable RAM snapshots.", beeGroupCacheRAM, ptrutil.Ptr(-1), nil),
		boolFlag("kv-unified", "kvu", []string{"no-kv-unified"}, false, "Use a single unified KV buffer shared across server slots.", beeGroupCacheRAM),

		// 10. Samplers
		intFlag("seed", "s", nil, -1, "RNG seed; -1 = random.", beeGroupSamplers, nil, nil),
		floatFlag("temperature", "", []string{"temp"}, 0.8, "Sampling temperature; higher = more diversity.", beeGroupSamplers, ptrutil.Ptr(0.0), nil),
		intFlag("top-k", "", nil, 40, "Keep the k most likely tokens; 0 disables it.", beeGroupSamplers, ptrutil.Ptr(0), nil),
		floatFlag("top-p", "", nil, 0.95, "Nucleus sampling; 1.0 disables it.", beeGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("min-p", "", nil, 0.05, "Keep tokens above a relative minimum probability; 0 disables it.", beeGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("typical-p", "", []string{"typical"}, 1.0, "Locally typical sampling; 1.0 disables it.", beeGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("top-n-sigma", "", nil, -1.0, "Top-n-sigma sampling; -1.0 disables it.", beeGroupSamplers, nil, nil),
		boolFlag("ignore-eos", "", nil, false, "Ignore the EOS token and continue generating.", beeGroupSamplers),
		strFlag("samplers", "", nil, nil, "Order of samplers applied during generation.", beeGroupSamplers, false),

		// 11. Penalties and advanced sampling
		intFlag("repeat-last-n", "", nil, 64, "Recent tokens considered for repetition penalty; -1 = ctx-size.", beeGroupPenalties, nil, nil),
		floatFlag("repeat-penalty", "", nil, 1.0, "Repetition penalty; 1.0 disables it.", beeGroupPenalties, nil, nil),
		floatFlag("presence-penalty", "", nil, 0.0, "Presence penalty; increases the cost of already-seen tokens.", beeGroupPenalties, nil, nil),
		floatFlag("frequency-penalty", "", nil, 0.0, "Penalty proportional to the frequency of already-seen tokens.", beeGroupPenalties, nil, nil),
		floatFlag("dry-multiplier", "", nil, 0.0, "DRY intensity; 0.0 disables it.", beeGroupPenalties, ptrutil.Ptr(0.0), nil),
		floatFlag("dry-base", "", nil, 1.75, "DRY base for penalizing repetitive sequences.", beeGroupPenalties, nil, nil),
		intFlag("dry-allowed-length", "", nil, 2, "Tolerated length before DRY applies.", beeGroupPenalties, ptrutil.Ptr(0), nil),
		enumFlag("mirostat", "", nil, []string{"0", "1", "2"}, 0, "Mirostat mode; 0 disables it.", beeGroupPenalties),
		floatFlag("mirostat-lr", "", nil, 0.1, "Mirostat learning rate (eta).", beeGroupPenalties, nil, nil),
		floatFlag("mirostat-ent", "", nil, 5.0, "Mirostat target entropy (tau).", beeGroupPenalties, nil, nil),
		floatFlag("adaptive-target", "", nil, nil, "Adaptive-p: select tokens near this probability (0.0-1.0); negative disables.", beeGroupPenalties, nil, ptrutil.Ptr(1.0)),
		floatFlag("adaptive-decay", "", nil, nil, "Adaptive-p: decay rate for target adaptation over time.", beeGroupPenalties, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),

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
		enumFlag("reasoning-loop-guard", "", nil, []string{"off", "force-close", "stop"}, "force-close", "Reasoning loop guard mode (BeeLlama): off, force-close, or stop.", beeGroupChat),
		intFlag("reasoning-loop-min-tokens", "", nil, 1024, "Reasoning loop guard: minimum hidden reasoning tokens before loop checks begin.", beeGroupChat, ptrutil.Ptr(0), nil),
		intFlag("reasoning-loop-window", "", nil, 2048, "Reasoning loop guard: token tail window inspected for loop detection.", beeGroupChat, ptrutil.Ptr(1), nil),
		intFlag("reasoning-loop-max-period", "", nil, 512, "Reasoning loop guard: maximum periodic loop length to check.", beeGroupChat, ptrutil.Ptr(1), nil),
		intFlag("reasoning-loop-min-coverage", "", nil, 768, "Reasoning loop guard: minimum repeated-token coverage before the guard triggers.", beeGroupChat, ptrutil.Ptr(1), nil),
		intFlag("reasoning-loop-check-interval", "", nil, 32, "Reasoning loop guard: accepted-token interval between loop checks.", beeGroupChat, ptrutil.Ptr(1), nil),
		intFlag("reasoning-loop-interventions", "", nil, 1, "Reasoning loop guard: maximum force-close interventions before generation is stopped.", beeGroupChat, ptrutil.Ptr(0), nil),

		// 14. HTTP server
		strFlag("host", "", nil, "127.0.0.1", "Server listen address.", beeGroupHTTP, false),
		portFlag("port", "", 8080, "HTTP server port.", beeGroupHTTP),
		strFlag("api-key", "", nil, nil, "Set API authentication key(s).", beeGroupHTTP, false),
		strFlag("api-key-file", "", nil, nil, "Read authentication keys from a file.", beeGroupHTTP, false),
		strFlag("path", "", nil, nil, "Directory of static files to serve.", beeGroupHTTP, false),
		boolFlag("ui", "", []string{"no-ui"}, true, "Toggle the built-in web interface.", beeGroupHTTP),
		intFlag("timeout", "to", nil, 3600, "Read/write timeout in seconds.", beeGroupHTTP, ptrutil.Ptr(0), nil),
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
		// e.g. "--spec-draft-hf, -hfd, -hfrd, --hf-repo-draft". The live --help
		// parser canonicalizes such flags to their LAST long form (hf-repo-draft),
		// and mergeWithCurated keeps the parsed Long but adopts the curated Aliases.
		// We therefore include the documented "spec-draft-*" primary name in each
		// flag's Aliases so it remains resolvable by name (the form the quickstart
		// and profiles use) after the merge.
		enumFlag("spec-type", "", nil, []string{"none", "draft-simple", "draft-eagle3", "draft-mtp", "ngram-simple", "ngram-map-k", "ngram-map-k4v", "ngram-mod", "ngram-cache", "suffix", "copyspec", "recycle", "dflash"}, "none", "Speculative decoding strategy. Use 'dflash' with a DFlash drafter or 'draft-mtp' for native MTP.", beeGroupSpeculative),
		strFlag("spec-draft-model", "md", []string{"model-draft", "spec-draft-model"}, nil, "Draft model for speculative decoding (DFlash drafter GGUF).", beeGroupSpeculative, false),
		strFlag("spec-draft-hf", "hfd", []string{"hf-repo-draft", "spec-draft-hf", "hfrd"}, nil, "HF repo for the draft model, <user>/<model>[:quant]; downloads on first run.", beeGroupSpeculative, false),
		strFlag("spec-draft-ngl", "ngld", []string{"n-gpu-layers-draft", "gpu-layers-draft", "spec-draft-ngl"}, nil, "Draft model layers offloaded to VRAM; integer count ('all' = use a high count like 99).", beeGroupSpeculative, false),
		intFlag("spec-draft-n-max", "", nil, 3, "Max tokens to draft per step; DFlash raises the effective omitted value to 16 by itself.", beeGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-draft-n-min", "", nil, 0, "Min draft tokens for speculative decoding.", beeGroupSpeculative, ptrutil.Ptr(0), nil),
		floatFlag("spec-draft-p-split", "", []string{"draft-p-split"}, 0.10, "Speculative decoding split probability.", beeGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("spec-draft-p-min", "", []string{"draft-p-min"}, 0.0, "Minimum speculative decoding probability.", beeGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		intFlag("spec-draft-ctx-size", "cd", []string{"ctx-size-draft", "spec-draft-ctx-size"}, 0, "Prompt context size for the draft model; 0 = loaded from model.", beeGroupSpeculative, ptrutil.Ptr(0), nil),
		strFlag("spec-draft-device", "devd", []string{"device-draft", "spec-draft-device"}, nil, "Devices for offloading the draft model; follows --device by default.", beeGroupSpeculative, false),
		intFlag("spec-draft-top-k", "", nil, 1, "Top-K candidates per drafter position for DDTree branching.", beeGroupSpeculative, ptrutil.Ptr(1), nil),
		strFlag("spec-draft-temp", "", nil, "0.0", "Drafter sampling temperature for Gumbel sampling; 0 = greedy, 'auto' = mirror target.", beeGroupSpeculative, false),
		enumFlag("spec-draft-type-k", "ctkd", []string{"cache-type-k-draft", "spec-draft-type-k"}, beellamaCacheTypes(), nil, "K cache data type for the draft model.", beeGroupSpeculative),
		enumFlag("spec-draft-type-v", "ctvd", []string{"cache-type-v-draft", "spec-draft-type-v"}, beellamaCacheTypes(), nil, "V cache data type for the draft model.", beeGroupSpeculative),
		// Draft-model CPU/thread placement (mirror the main-model CPU flags for the drafter).
		intFlag("spec-draft-threads", "td", []string{"threads-draft"}, nil, "CPU threads for draft generation; defaults to --threads.", beeGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-draft-threads-batch", "tbd", []string{"threads-batch-draft"}, nil, "CPU threads for draft batch/prefill; defaults to --spec-draft-threads.", beeGroupSpeculative, ptrutil.Ptr(0), nil),
		strFlag("spec-draft-cpu-mask", "Cd", []string{"cpu-mask-draft"}, nil, "Draft model CPU affinity mask; complements --spec-draft-cpu-range.", beeGroupSpeculative, false),
		strFlag("spec-draft-cpu-range", "Crd", []string{"cpu-range-draft"}, nil, "Draft model CPU affinity range (lo-hi); complements --spec-draft-cpu-mask.", beeGroupSpeculative, false),
		intFlag("spec-draft-cpu-strict", "", []string{"cpu-strict-draft"}, nil, "Strict CPU placement for the draft model (0|1); defaults to --cpu-strict.", beeGroupSpeculative, nil, nil),
		intFlag("spec-draft-prio", "", []string{"prio-draft"}, 0, "Draft process/thread priority: 0=normal, 1=medium, 2=high, 3=realtime.", beeGroupSpeculative, ptrutil.Ptr(0), ptrutil.Ptr(3)),
		intFlag("spec-draft-poll", "", []string{"poll-draft"}, nil, "Use polling (0|1) while waiting for draft work; defaults to --poll.", beeGroupSpeculative, nil, nil),
		strFlag("spec-draft-cpu-mask-batch", "Cbd", []string{"cpu-mask-batch-draft"}, nil, "Draft model batch CPU affinity mask.", beeGroupSpeculative, false),
		strFlag("spec-draft-cpu-range-batch", "Crbd", []string{"cpu-range-batch-draft"}, nil, "Draft model batch CPU affinity range (lo-hi).", beeGroupSpeculative, false),
		intFlag("spec-draft-cpu-strict-batch", "", []string{"cpu-strict-batch-draft"}, nil, "Strict CPU placement for draft batch processing (0|1).", beeGroupSpeculative, nil, nil),
		intFlag("spec-draft-prio-batch", "", []string{"prio-batch-draft"}, 0, "Draft batch process/thread priority: 0=normal, 1=medium, 2=high, 3=realtime.", beeGroupSpeculative, ptrutil.Ptr(0), ptrutil.Ptr(3)),
		intFlag("spec-draft-poll-batch", "", []string{"poll-batch-draft"}, nil, "Use polling (0|1) while waiting for draft batch work.", beeGroupSpeculative, nil, nil),
		boolFlag("spec-draft-cpu-moe", "cmoed", []string{"cpu-moe-draft"}, false, "Keep all draft-model Mixture-of-Experts weights on the CPU.", beeGroupSpeculative),
		intFlag("spec-draft-n-cpu-moe", "ncmoed", []string{"spec-draft-ncmoe", "n-cpu-moe-draft"}, nil, "Keep the first N draft-model Mixture-of-Experts layers on the CPU.", beeGroupSpeculative, ptrutil.Ptr(0), nil),
		strFlag("spec-draft-override-tensor", "otd", []string{"override-tensor-draft"}, nil, "Override draft-model tensor buffer type, <pattern>=<buffer type> (comma-separated).", beeGroupSpeculative, false),
		boolFlag("spec-draft-backend-sampling", "", []string{"no-spec-draft-backend-sampling"}, true, "Offload draft-model sampling to the backend.", beeGroupSpeculative),
		// DDTree tree verification and DFlash server state.
		intFlag("spec-branch-budget", "", nil, 0, "DDTree branch nodes beyond the main draft path; 0 = flat DFlash.", beeGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-dflash-max-slots", "", nil, 0, "Max concurrent server slots with DFlash state; 0 = match parallel slots (-np); extra slots fall back to non-spec decode.", beeGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-dflash-cross-ctx", "", nil, 512, "DFlash cross-attention window: how many target hidden states the drafter sees.", beeGroupSpeculative, ptrutil.Ptr(0), nil),
		boolFlag("spec-default", "", nil, false, "Enable the default speculative-decoding config.", beeGroupSpeculative),
		// Adaptive draft-max controller (DFlash). Common controls plus per-mode tuning.
		boolFlag("spec-dm-adaptive", "", []string{"no-spec-dm-adaptive"}, true, "Adaptive draft-max controller that tunes draft depth from acceptance rates.", beeGroupSpeculative),
		floatFlag("spec-dm-fringe-min", "", nil, 0.30, "Fringe controller: acceptance rate below which DFlash is disabled after off-dwell.", beeGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("spec-dm-fringe-max", "", nil, 0.50, "Fringe controller: acceptance rate above which the full base draft-max is used.", beeGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		intFlag("spec-dm-off-dwell", "", nil, 8, "Consecutive weak speculation cycles before DFlash is disabled.", beeGroupSpeculative, ptrutil.Ptr(1), nil),
		intFlag("spec-dm-explore-interval", "", nil, 12, "Draft at an exploratory depth every N speculation cycles.", beeGroupSpeculative, ptrutil.Ptr(1), nil),
		intFlag("spec-dm-min-reach", "", nil, 3, "Fringe controller: min current-epoch samples at the target position before promotion.", beeGroupSpeculative, nil, nil),
		intFlag("spec-dm-probe-interval", "", nil, 16, "Minimum cycles to wait before probing with draft-max>0 while the controller is disabled.", beeGroupSpeculative, ptrutil.Ptr(1), nil),
		floatFlag("spec-dm-probe-fraction", "", nil, 0.25, "Fraction of the base draft-max used when probing from the disabled state.", beeGroupSpeculative, ptrutil.Ptr(0.01), ptrutil.Ptr(1.0)),
		enumFlag("spec-dm-controller", "", nil, []string{"fringe", "profit"}, "profit", "Adaptive draft-max controller mode.", beeGroupSpeculative),
		floatFlag("spec-dm-profit-min", "", nil, 0.05, "Profit controller: minimum profit margin over the no-spec baseline before disabling dwell clears.", beeGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(0.50)),
		floatFlag("spec-dm-profit-raise-margin", "", nil, 0.05, "Profit controller: relative margin required to raise the adaptive draft depth.", beeGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("spec-dm-profit-lower-margin", "", nil, 0.05, "Profit controller: relative margin required to lower the adaptive draft depth.", beeGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("spec-dm-profit-ewma-alpha", "", nil, 0.15, "Profit controller: EWMA alpha for adaptive draft-max profit statistics.", beeGroupSpeculative, ptrutil.Ptr(0.01), ptrutil.Ptr(1.0)),
		intFlag("spec-dm-profit-min-samples", "", nil, 3, "Profit controller: minimum samples before an adaptive position/depth is ready.", beeGroupSpeculative, ptrutil.Ptr(1), ptrutil.Ptr(64)),
		intFlag("spec-dm-profit-warmup", "", nil, 0, "Profit controller: minimum samples per initial positive-depth probe (0 = use --spec-dm-profit-min-samples).", beeGroupSpeculative, ptrutil.Ptr(0), ptrutil.Ptr(64)),
		intFlag("spec-dm-profit-baseline-interval", "", nil, 1024, "Profit controller: active cycles between no-spec baseline reprobes (0 = disabled).", beeGroupSpeculative, ptrutil.Ptr(0), ptrutil.Ptr(4096)),
		// ngram self-speculative tuning (used by the spec-type=ngram-* strategies).
		intFlag("spec-ngram-mod-n-min", "", nil, 48, "ngram-mod: minimum ngram token length used for speculation.", beeGroupSpeculative, ptrutil.Ptr(0), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-mod-n-max", "", nil, 64, "ngram-mod: maximum ngram token length used for speculation.", beeGroupSpeculative, ptrutil.Ptr(0), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-mod-n-match", "", nil, 24, "ngram-mod: lookup match length.", beeGroupSpeculative, ptrutil.Ptr(1), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-simple-size-n", "", nil, 12, "ngram-simple: lookup n-gram length (N).", beeGroupSpeculative, ptrutil.Ptr(1), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-simple-size-m", "", nil, 48, "ngram-simple: drafted m-gram length (M).", beeGroupSpeculative, ptrutil.Ptr(1), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-simple-min-hits", "", nil, 1, "ngram-simple: minimum lookup hits before an m-gram is proposed.", beeGroupSpeculative, ptrutil.Ptr(1), nil),
		intFlag("spec-ngram-map-k-size-n", "", nil, 12, "ngram-map-k: lookup n-gram length (N).", beeGroupSpeculative, ptrutil.Ptr(1), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-map-k-size-m", "", nil, 48, "ngram-map-k: drafted m-gram length (M).", beeGroupSpeculative, ptrutil.Ptr(1), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-map-k-min-hits", "", nil, 1, "ngram-map-k: minimum lookup hits before an m-gram is proposed.", beeGroupSpeculative, ptrutil.Ptr(1), nil),
		intFlag("spec-ngram-map-k4v-size-n", "", nil, 12, "ngram-map-k4v: lookup n-gram length (N).", beeGroupSpeculative, ptrutil.Ptr(1), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-map-k4v-size-m", "", nil, 48, "ngram-map-k4v: drafted m-gram length (M).", beeGroupSpeculative, ptrutil.Ptr(1), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-map-k4v-min-hits", "", nil, 1, "ngram-map-k4v: minimum lookup hits before an m-gram is proposed.", beeGroupSpeculative, ptrutil.Ptr(1), nil),
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
// fork's TurboQuant and TCQ types, in the order reported by the binary's --help
// (the kv_cache_types vector in common/arg.cpp).
func beellamaCacheTypes() []string {
	return []string{"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1", "q6_0", "turbo2", "turbo3", "turbo4", "turbo3_tcq", "turbo2_tcq"}
}

// beeLlamaRules are DFlash-aware cross-field validation rules. They are chosen
// to never false-positive on a valid DFlash profile: the single-flag rule
// engine cannot express "draft-model OR draft-hf", so we avoid a hard require
// on spec-type=dflash itself (the drafter can also be auto-detected from
// draft GGUF metadata since v0.3.0).
func beeLlamaRules() []domain.CrossFieldRule {
	return []domain.CrossFieldRule{
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
		{Name: beeGroupShift, Flags: []string{"swa-full", "context-shift", "ctx-checkpoints", "checkpoint-min-step"}},
		{Name: beeGroupCacheRAM, Flags: []string{"cache-ram", "kv-unified"}},
		{Name: beeGroupSamplers, Flags: []string{"seed", "temperature", "top-k", "top-p", "min-p", "typical-p", "top-n-sigma", "ignore-eos", "samplers"}},
		{Name: beeGroupPenalties, Flags: []string{"repeat-last-n", "repeat-penalty", "presence-penalty", "frequency-penalty", "dry-multiplier", "dry-base", "dry-allowed-length", "mirostat", "mirostat-lr", "mirostat-ent", "adaptive-target", "adaptive-decay"}},
		{Name: beeGroupGrammar, Flags: []string{"grammar", "grammar-file", "json-schema", "json-schema-file"}},
		{Name: beeGroupChat, Flags: []string{"chat-template", "chat-template-file", "chat-template-kwargs", "jinja", "reasoning-format", "reasoning", "reasoning-budget", "reasoning-loop-guard", "reasoning-loop-min-tokens", "reasoning-loop-window", "reasoning-loop-max-period", "reasoning-loop-min-coverage", "reasoning-loop-check-interval", "reasoning-loop-interventions"}},
		{Name: beeGroupHTTP, Flags: []string{"host", "port", "api-key", "api-key-file", "path", "ui", "timeout", "threads-http", "metrics", "slots", "parallel", "cont-batching", "cache-prompt", "alias"}},
		{Name: beeGroupMultimodal, Flags: []string{"mmproj", "mmproj-url", "mmproj-auto", "mmproj-offload"}},
		{Name: beeGroupLora, Flags: []string{"lora", "lora-scaled", "control-vector", "control-vector-scaled"}},
		{Name: beeGroupSpeculative, Flags: []string{"spec-type", "spec-draft-model", "spec-draft-hf", "spec-draft-ngl", "spec-draft-n-max", "spec-draft-n-min", "spec-draft-p-split", "spec-draft-p-min", "spec-draft-ctx-size", "spec-draft-device", "spec-draft-top-k", "spec-draft-temp", "spec-draft-type-k", "spec-draft-type-v", "spec-draft-threads", "spec-draft-threads-batch", "spec-draft-cpu-mask", "spec-draft-cpu-range", "spec-draft-cpu-strict", "spec-draft-prio", "spec-draft-poll", "spec-draft-cpu-mask-batch", "spec-draft-cpu-range-batch", "spec-draft-cpu-strict-batch", "spec-draft-prio-batch", "spec-draft-poll-batch", "spec-draft-cpu-moe", "spec-draft-n-cpu-moe", "spec-draft-override-tensor", "spec-draft-backend-sampling", "spec-branch-budget", "spec-dflash-max-slots", "spec-dflash-cross-ctx", "spec-default", "spec-dm-adaptive", "spec-dm-fringe-min", "spec-dm-fringe-max", "spec-dm-off-dwell", "spec-dm-explore-interval", "spec-dm-min-reach", "spec-dm-probe-interval", "spec-dm-probe-fraction", "spec-dm-controller", "spec-dm-profit-min", "spec-dm-profit-raise-margin", "spec-dm-profit-lower-margin", "spec-dm-profit-ewma-alpha", "spec-dm-profit-min-samples", "spec-dm-profit-warmup", "spec-dm-profit-baseline-interval", "spec-ngram-mod-n-min", "spec-ngram-mod-n-max", "spec-ngram-mod-n-match", "spec-ngram-simple-size-n", "spec-ngram-simple-size-m", "spec-ngram-simple-min-hits", "spec-ngram-map-k-size-n", "spec-ngram-map-k-size-m", "spec-ngram-map-k-min-hits", "spec-ngram-map-k4v-size-n", "spec-ngram-map-k4v-size-m", "spec-ngram-map-k4v-min-hits"}},
	}}
}

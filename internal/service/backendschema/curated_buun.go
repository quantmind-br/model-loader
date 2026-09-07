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
	buunGroupCachePlan   = "21. Cache Plan and Receipts"
)

// CuratedBuunSchema returns the hand-curated buun-llama-cpp schema. The fork is
// based on llama.cpp and extends llama-server with router, reasoning, DFlash,
// TurboQuant/VBR KV cache, built-in agent tools, and cache-plan/cache-receipt
// flags.
//
// Fork-only surfaces kept here that upstream llama.cpp does not define:
// the VBR family (--vbr-bits/--vbr-floor/--vbr-vram/--vbr-entry/
// --vbr-reclaim-floor/--vbr-reset-keep-frac/--vbr-policy plus the vbr and
// turbo* cache types), -ct/--cache-type, --logits-all, --no-fused-gdn,
// --mmproj-gpu-swap, --mmap-prefetch, --moe-cache-profile,
// -cd/--ctx-size-draft, --spec-draft-replace, --spec-dflash-default,
// --dflash-max-slots, and the cache-plan/cache-receipt group.
// --lazy-mode is the post-b10703 upstream spelling; the fork renamed
// --tensor-read-lazy to it, so the old spelling no longer exists in this build.
// --vbr-bits and --vbr-policy are declared with set_hidden() in the fork, so
// they never appear in --help and this curated file is their only source.
func CuratedBuunSchema() domain.BackendValidationSchema {
	flags := map[string]domain.FlagSpec{}
	add := func(spec domain.FlagSpec) {
		flags[spec.Long] = spec
	}

	for _, spec := range []domain.FlagSpec{
		strFlag("hf-repo", "hf", []string{"hfr"}, nil, "Downloads/loads a model directly from Hugging Face; quant is optional.", buunGroupModelLoad, false),
		strFlag("hf-file", "hff", nil, nil, "Specific file inside the HF repository, overriding automatic quant selection.", buunGroupModelLoad, false),
		strFlag("hf-token", "hft", nil, nil, "Hugging Face authentication token for private repositories.", buunGroupModelLoad, false),
		strFlag("model-url", "mu", nil, nil, "Model download URL.", buunGroupModelLoad, false),
		strFlag("docker-repo", "dr", nil, nil, "Docker Hub model repository, [<repo>/]<model>[:quant]; repo defaults to ai/ and quant to :latest.", buunGroupModelLoad, false),
		strFlag("override-kv", "", nil, nil, "Override model metadata by key, KEY=TYPE:VALUE (comma-separated).", buunGroupModelLoad, false),
		boolFlag("offline", "", nil, false, "Forces offline mode using only local cache, with no network.", buunGroupModelLoad),

		intFlag("ctx-size", "c", nil, 0, "Context window size; 0 uses the value loaded from the model.", buunGroupContext, ptrutil.Ptr(0), nil),
		intFlag("n-predict", "n", []string{"predict"}, -1, "Number of tokens to generate; -1 = infinite generation.", buunGroupContext, nil, nil),
		intFlag("batch-size", "b", nil, 2048, "Maximum logical batch size for prompt processing.", buunGroupContext, ptrutil.Ptr(1), nil),
		intFlag("ubatch-size", "ub", nil, 512, "Maximum physical micro-batch size (throughput/memory tuning).", buunGroupContext, ptrutil.Ptr(1), nil),
		intFlag("keep", "", nil, 0, "Initial prompt tokens to keep during shifting/reuse; -1 keeps all.", buunGroupContext, nil, nil),

		intFlag("threads", "t", nil, nil, "CPU threads used for generation.", buunGroupCPU, ptrutil.Ptr(0), nil),
		intFlag("threads-batch", "tb", nil, nil, "Threads for batch/prefill processing; inherits --threads by default.", buunGroupCPU, ptrutil.Ptr(0), nil),
		intFlag("poll", "", nil, 50, "Polling level while waiting for work; 0 disables it.", buunGroupCPU, ptrutil.Ptr(0), ptrutil.Ptr(100)),
		enumFlag("prio", "", nil, []string{"-1", "0", "1", "2", "3"}, 0, "Process/thread priority: low, normal, medium, high, realtime.", buunGroupCPU),
		enumFlag("numa", "", nil, []string{"distribute", "isolate", "numactl"}, nil, "Optimizations for NUMA machines.", buunGroupCPU),

		enumFlag("load-mode", "lm", nil, []string{"none", "mmap", "mlock", "mmap+mlock", "dio"}, "mmap", "Model loading mode; replaces the deprecated --mlock/--mmap/--direct-io flags.", buunGroupMemory),
		boolFlag("mlock", "", nil, false, "DEPRECATED in favor of --load-mode mlock: keeps the model in RAM, avoiding swap.", buunGroupMemory),
		boolFlag("mmap", "", []string{"no-mmap"}, true, "DEPRECATED in favor of --load-mode mmap: model memory mapping; disabling it may reduce pageouts but makes loading slower.", buunGroupMemory),
		boolFlag("direct-io", "dio", []string{"no-direct-io"}, false, "DEPRECATED in favor of --load-mode dio: uses Direct I/O when available.", buunGroupMemory),
		boolFlag("repack", "", []string{"nr", "no-repack"}, true, "Enables/disables weight repacking.", buunGroupMemory),
		boolFlag("op-offload", "", []string{"no-op-offload"}, true, "Offloads tensor operations from host to device.", buunGroupMemory),
		boolFlag("no-host", "", nil, false, "Bypasses the host buffer to allow extra buffers.", buunGroupMemory),
		boolFlag("logits-all", "", []string{"no-logits-all"}, true, "Reserves a logits buffer for every token in the batch; disable it to save VRAM on big-vocab chat/completion workloads.", buunGroupMemory),
		boolFlag("check-tensors", "", nil, false, "Checks model tensors for invalid values.", buunGroupMemory),
		enumFlag("lazy-mode", "lzm", nil, []string{"on", "auto", "off"}, "auto", "On-demand reading of certain tensors (for example per-layer embeddings) from disk; requires mmap. Spelling used from build b10703 onward.", buunGroupMemory),
		enumFlag("mmap-prefetch", "", nil, []string{"on", "auto", "off"}, "auto", "Bulk mmap prefetch policy: 'auto' prefetches only when the mapped model comfortably fits available system RAM, 'on' keeps eager whole-model prefetch, 'off' relies on sequential readahead and demand paging.", buunGroupMemory),

		strFlag("device", "dev", nil, nil, "Selects the devices used for offload.", buunGroupDevice, false),
		boolFlag("list-devices", "", nil, false, "Lists available devices and exits.", buunGroupDevice),
		withKeywords(intFlag("n-gpu-layers", "ngl", []string{"gpu-layers"}, -1, "Number of model layers moved to VRAM; accepts an exact integer, auto (-1), or all (-2).", buunGroupDevice, ptrutil.Ptr(-2), ptrutil.Ptr(9999)), "auto", "all"),
		enumFlag("split-mode", "sm", nil, []string{"none", "layer", "row", "tensor"}, "layer", "How to split the model across multiple GPUs.", buunGroupDevice),
		strFlag("tensor-split", "ts", nil, nil, "Offload ratio across GPUs (e.g. 3,1).", buunGroupDevice, false),
		intFlag("main-gpu", "mg", nil, 0, "Main GPU for the model/intermediate results (depends on split mode).", buunGroupDevice, ptrutil.Ptr(0), nil),
		strFlag("override-tensor", "ot", nil, nil, "Override tensor buffer type, <pattern>=<buffer type> (comma-separated).", buunGroupDevice, false),
		boolFlag("cpu-moe", "cmoe", nil, false, "Keep all Mixture-of-Experts weights on the CPU.", buunGroupDevice),
		intFlag("n-cpu-moe", "ncmoe", nil, nil, "Keep the first N Mixture-of-Experts layers on the CPU.", buunGroupDevice, ptrutil.Ptr(0), nil),
		intFlag("n-cpu-ffn", "ncffn", nil, nil, "Keep the dense FFN weights of the first N layers in the CPU (dense models; use --n-cpu-moe for MoE expert weights).", buunGroupDevice, ptrutil.Ptr(0), nil),
		boolFlag("moe-cache-profile", "", []string{"no-moe-cache-profile"}, true, "Persists a versioned per-model expert heatmap in the llama.cpp cache directory and uses it for bounded expert prewarming.", buunGroupDevice),
		boolFlag("no-fused-gdn", "", nil, false, "Disables the fused Gated Delta Net kernels and uses decomposed ops instead.", buunGroupDevice),
		enumFlag("fit", "fit", nil, []string{"on", "off"}, "on", "Adjusts unset parameters to fit device memory.", buunGroupDevice),
		strFlag("fit-target", "fitt", nil, "1024", "Target memory margin in MiB per device used by --fit; a single value is broadcast to all devices.", buunGroupDevice, false),
		intFlag("fit-ctx", "fitc", nil, 4096, "Minimum context that --fit may configure.", buunGroupDevice, ptrutil.Ptr(0), nil),

		boolFlag("kv-offload", "kvo", []string{"no-kv-offload"}, true, "Controls KV cache offload.", buunGroupKV),
		enumFlag("flash-attn", "fa", nil, []string{"on", "off", "auto"}, "auto", "Flash Attention use: on, off, or auto.", buunGroupKV),
		enumFlag("cache-type", "ct", nil, buunCacheTypes(), "vbr", "KV cache data type for both K and V; shorthand for -ctk plus -ctv, and a later -ctk/-ctv overrides its side.", buunGroupKV),
		enumFlag("cache-type-k", "ctk", nil, buunCacheTypes(), "vbr", "K cache data type, including Buun TurboQuant formats and vbr; defaults to dynamic vbr with an implicit t4 floor.", buunGroupKV),
		enumFlag("cache-type-v", "ctv", nil, buunCacheTypes(), "vbr", "V cache data type, including Buun TurboQuant formats and vbr; defaults to dynamic vbr with an implicit t4 floor.", buunGroupKV),
		strFlag("vbr-budget", "", []string{"vbr-bits"}, "dynamic", "VBR target budget; dynamic enables the runtime degrade controller, while a fixed tier (f16, t8, t4, t3, t2, t1) or a numeric bits/value budget selects a fixed target. Hidden from --help.", buunGroupKV, false),
		strFlag("vbr-min-bits", "", []string{"vbr-floor"}, nil, "Minimum aggregate VBR bits per value; the degradation floor. Defaults to the t4 tier (4.125 bits) when vbr is selected implicitly, or the t1 tier (1.25 bits) when -ctk/-ctv vbr is set explicitly without this flag.", buunGroupKV, false),
		strFlag("vbr-vram-budget", "", []string{"vbr-vram"}, "auto", "VBR KV-cache VRAM budget; auto uses the remaining VRAM after model and overhead allocation.", buunGroupKV, false),
		floatFlag("vbr-reclaim-floor", "", nil, 8.125, "Dynamic VBR reclaim threshold in bits per value; idle slot caches are reclaimed before degradation falls below this floor.", buunGroupKV, nil, nil),
		floatFlag("vbr-reset-keep-frac", "", nil, 0.25, "Dynamic VBR reset threshold; discard a degraded cached prefix when less than this fraction remains reusable.", buunGroupKV, nil, nil),
		strFlag("vbr-policy", "", nil, nil, "VBR policy ladder JSON file or directory; unset uses the automatic policy ladder. Requires a fixed --vbr-budget. Hidden from --help.", buunGroupKV, false),
		enumFlag("vbr-entry", "", nil, []string{"f16", "t8", "t4", "t3", "t2", "t1"}, "f16", "Dynamic VBR entry tier; entering below f16 is an explicit quality-for-bandwidth trade and tensors still degrade toward --vbr-min-bits as the KV VRAM budget fills. Rejected when --vbr-budget selects a fixed tier.", buunGroupKV),

		enumFlag("rope-scaling", "", nil, []string{"none", "linear", "yarn"}, nil, "RoPE frequency scaling method; unset defers to the model (linear otherwise).", buunGroupRope),
		floatFlag("rope-scale", "", nil, nil, "Context expansion factor via RoPE.", buunGroupRope, nil, nil),
		floatFlag("rope-freq-base", "", nil, nil, "RoPE base frequency (NTK-aware scaling).", buunGroupRope, nil, nil),
		floatFlag("rope-freq-scale", "", nil, nil, "Frequency scaling factor; expands context by 1/N.", buunGroupRope, nil, nil),
		intFlag("yarn-orig-ctx", "", nil, 0, "Original model context for YaRN; 0 uses the training value.", buunGroupRope, ptrutil.Ptr(0), nil),
		floatFlag("yarn-ext-factor", "", nil, -1.0, "YaRN extrapolation/interpolation factor; -1 defers to the model.", buunGroupRope, nil, nil),
		floatFlag("yarn-attn-factor", "", nil, -1.0, "Attention magnitude adjustment in YaRN; -1 defers to the model.", buunGroupRope, nil, nil),
		floatFlag("yarn-beta-slow", "", nil, -1.0, "YaRN slow/high correction dim parameter; -1 defers to the model.", buunGroupRope, nil, nil),
		floatFlag("yarn-beta-fast", "", nil, -1.0, "YaRN fast/low correction dim parameter; -1 defers to the model.", buunGroupRope, nil, nil),

		boolFlag("swa-full", "", nil, false, "Uses full-size SWA cache.", buunGroupShift),
		boolFlag("context-shift", "", []string{"no-context-shift"}, false, "Controls context shift during infinite generation.", buunGroupShift),
		intFlag("ctx-checkpoints", "ctxcp", []string{"swa-checkpoints"}, 32, "Maximum context checkpoints created per slot.", buunGroupShift, ptrutil.Ptr(0), nil),
		intFlag("checkpoint-min-step", "cms", nil, 8192, "Minimum spacing between context checkpoints in tokens; 0 = no minimum.", buunGroupShift, ptrutil.Ptr(0), nil),

		intFlag("cache-ram", "cram", nil, 8192, "Maximum RAM cache size in MiB for KV/prompt reuse; -1 = no limit, 0 = disable.", buunGroupCacheRAM, ptrutil.Ptr(-1), nil),
		boolFlagAuto("kv-unified", "kvu", []string{"no-kv-unified"}, "Uses a single unified KV buffer shared across all sequences; enabled by default only when the slot count is auto.", buunGroupCacheRAM),
		intFlag("kv-unified-per-slot", "", nil, nil, "Context limit per parallel slot; when set without --ctx-size the shared unified KV pool is sized to parallel*N.", buunGroupCacheRAM, ptrutil.Ptr(0), nil),
		boolFlag("cache-idle-slots", "", []string{"no-cache-idle-slots"}, true, "Saves idle slots to the prompt cache for new tasks and clears them when using unified KV; requires cache-ram.", buunGroupCacheRAM),
		withAllowedInts(intFlag("sleep-idle-seconds", "", nil, -1, "Seconds of idleness before the server sleeps; -1 disables sleeping.", buunGroupCacheRAM, ptrutil.Ptr(1), nil), -1),

		strFlag("samplers", "", nil, "penalties;dry;top_n_sigma;top_k;typ_p;top_p;min_p;xtc;temperature", "Order of samplers applied during generation.", buunGroupSamplers, false),
		strFlag("sampler-seq", "", []string{"sampling-seq"}, "edskypmxt", "Simplified single-character sampler sequence.", buunGroupSamplers, false),
		intFlag("seed", "s", nil, -1, "RNG seed; -1 = random.", buunGroupSamplers, nil, nil),
		floatFlag("temperature", "", []string{"temp"}, 0.8, "Sampling temperature; higher = more diversity.", buunGroupSamplers, ptrutil.Ptr(0.0), nil),
		intFlag("top-k", "", nil, 40, "Keeps the k most likely tokens; 0 disables it.", buunGroupSamplers, ptrutil.Ptr(0), nil),
		floatFlag("top-p", "", nil, 0.95, "Nucleus sampling; 1.0 disables it.", buunGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("min-p", "", nil, 0.05, "Keeps tokens above a relative minimum probability; 0.0 disables it.", buunGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("typical-p", "", []string{"typical"}, 1.0, "Locally typical sampling; 1.0 disables it.", buunGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("top-n-sigma", "", []string{"top-nsigma"}, -1.0, "Top-n-sigma sampling; -1.0 disables it.", buunGroupSamplers, nil, nil),
		floatFlag("xtc-probability", "", nil, 0.0, "XTC sampling probability; 0.0 disables it.", buunGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("xtc-threshold", "", nil, 0.1, "XTC sampling threshold; values above 0.5 disable it.", buunGroupSamplers, ptrutil.Ptr(0.0), nil),
		boolFlag("ignore-eos", "", nil, false, "Ignores the EOS token and continues generating.", buunGroupSamplers),
		strFlag("logit-bias", "l", nil, nil, "Increases/decreases the chance of specific tokens in TOKEN_ID(+/-)BIAS format.", buunGroupSamplers, false),
		boolFlag("backend-sampling", "bs", nil, false, "Runs sampling in the backend (experimental).", buunGroupSamplers),

		intFlag("repeat-last-n", "", nil, 64, "Recent tokens considered for repetition penalty; -1 = ctx-size.", buunGroupPenalties, nil, nil),
		floatFlag("repeat-penalty", "", nil, 1.0, "Repetition penalty; 1.0 disables it.", buunGroupPenalties, nil, nil),
		floatFlag("presence-penalty", "", nil, 0.0, "Presence penalty; increases the cost of already-seen tokens.", buunGroupPenalties, nil, nil),
		floatFlag("frequency-penalty", "", nil, 0.0, "Penalty proportional to the frequency of already-seen tokens.", buunGroupPenalties, nil, nil),
		floatFlag("dry-multiplier", "", nil, 0.0, "DRY intensity; 0.0 disables it.", buunGroupPenalties, ptrutil.Ptr(0.0), nil),
		floatFlag("dry-base", "", nil, 1.75, "DRY base for penalizing repetitive sequences.", buunGroupPenalties, nil, nil),
		intFlag("dry-allowed-length", "", nil, 2, "Tolerated length before DRY applies.", buunGroupPenalties, ptrutil.Ptr(0), nil),
		intFlag("dry-penalty-last-n", "", nil, 64, "DRY window in tokens; 0 disables it. Negative values are rejected by the backend.", buunGroupPenalties, ptrutil.Ptr(0), nil),
		strFlag("dry-sequence-breaker", "", nil, "\\n, :, \", *", "Breaks that reset DRY; use \"none\" for no breakers.", buunGroupPenalties, false),
		intFlag("mirostat", "", nil, 0, "Mirostat mode; 0 disables it.", buunGroupPenalties, ptrutil.Ptr(0), ptrutil.Ptr(2)),
		floatFlag("mirostat-lr", "", nil, 0.1, "Mirostat learning rate (eta).", buunGroupPenalties, nil, nil),
		floatFlag("mirostat-ent", "", nil, 5.0, "Mirostat target entropy (tau).", buunGroupPenalties, nil, nil),
		floatFlag("dynatemp-range", "", nil, 0.0, "Dynamic temperature range; 0.0 disables it.", buunGroupPenalties, ptrutil.Ptr(0.0), nil),
		floatFlag("dynatemp-exp", "", nil, 1.0, "Dynamic temperature exponent.", buunGroupPenalties, nil, nil),
		floatFlag("adaptive-target", "", nil, -1.0, "Adaptive-p target probability in the 0.0-1.0 range; negative values disable it.", buunGroupPenalties, nil, ptrutil.Ptr(1.0)),
		floatFlag("adaptive-decay", "", nil, 0.9, "Adaptive-p decay; lower is more reactive, higher is more stable.", buunGroupPenalties, ptrutil.Ptr(0.0), ptrutil.Ptr(0.99)),

		strFlag("grammar", "", nil, "", "Constrains output with GBNF grammar.", buunGroupGrammar, false),
		strFlag("grammar-file", "", nil, nil, "Reads grammar from a file.", buunGroupGrammar, false),
		strFlag("json-schema", "j", nil, nil, "Constrains output to a JSON Schema.", buunGroupGrammar, false),
		strFlag("json-schema-file", "jf", nil, nil, "Loads JSON Schema from a file.", buunGroupGrammar, false),

		strFlag("chat-template", "", nil, nil, "Defines the chat template (embedded or customized).", buunGroupChat, false),
		strFlag("chat-template-file", "", nil, nil, "Reads the chat template from a Jinja file.", buunGroupChat, false),
		strFlag("chat-template-kwargs", "", nil, nil, "Extra JSON arguments for the template parser.", buunGroupChat, false),
		boolFlag("jinja", "", []string{"no-jinja"}, true, "Toggles the Jinja engine for chat.", buunGroupChat),
		boolFlag("skip-chat-parsing", "", []string{"no-skip-chat-parsing"}, false, "Forces pure content parsing, without structural parsing.", buunGroupChat),
		boolFlag("prefill-assistant", "", []string{"no-prefill-assistant"}, true, "Controls response prefill when the last message is already from the assistant.", buunGroupChat),
		enumFlag("reasoning-format", "", nil, []string{"none", "deepseek", "deepseek-legacy", "auto"}, "auto", "Format used to extract reasoning blocks.", buunGroupChat),
		enumFlag("reasoning", "rea", nil, []string{"on", "off", "auto"}, "auto", "Controls reasoning emission/use when the model supports it.", buunGroupChat),
		intFlag("reasoning-budget", "", nil, -1, "Token budget for reasoning; -1 = unrestricted, 0 ends thinking immediately.", buunGroupChat, ptrutil.Ptr(-1), nil),
		strFlag("reasoning-budget-message", "", nil, nil, "Message/instruction used to communicate the reasoning budget to the model.", buunGroupChat, false),
		boolFlagAuto("reasoning-preserve", "", []string{"no-reasoning-preserve"}, "Preserve the reasoning trace in the full history, not just the last assistant message; defaults to the template's behaviour.", buunGroupChat),
		strFlag("reasoning-effort", "", nil, "default", "Reasoning effort level given to the chat template: 'default' keeps the template default, or a level such as minimal, low, medium, high, xhigh, max.", buunGroupChat, false),

		strFlag("host", "", nil, "127.0.0.1", "Server listen address.", buunGroupHTTP, false),
		portFlag("port", "", 8080, "HTTP server port.", buunGroupHTTP),
		boolFlag("reuse-port", "", nil, false, "Allows multiple sockets on the same port.", buunGroupHTTP),
		strFlag("api-prefix", "", nil, "", "API route prefix (no trailing slash).", buunGroupHTTP, false),
		strFlag("path", "", nil, nil, "Directory of static files to serve.", buunGroupHTTP, false),
		boolFlag("ui", "", []string{"webui", "no-ui", "no-webui"}, true, "Toggles the web interface.", buunGroupHTTP),
		strFlag("ui-config", "", []string{"webui-config"}, nil, "Overrides default UI settings.", buunGroupHTTP, false),
		strFlag("ui-config-file", "", []string{"webui-config-file"}, nil, "Loads UI settings from a JSON file.", buunGroupHTTP, false),
		boolFlag("ui-mcp-proxy", "", []string{"webui-mcp-proxy", "no-ui-mcp-proxy", "no-webui-mcp-proxy"}, false, "Enables the experimental MCP CORS proxy; do not enable in untrusted environments.", buunGroupHTTP),
		strFlag("api-key", "", nil, nil, "Sets API authentication keys.", buunGroupHTTP, false),
		strFlag("api-key-file", "", nil, nil, "Reads authentication keys from a file.", buunGroupHTTP, false),
		strFlag("ssl-key-file", "", nil, nil, "SSL private key.", buunGroupHTTP, false),
		strFlag("ssl-cert-file", "", nil, nil, "SSL certificate.", buunGroupHTTP, false),
		strFlag("cors-origins", "", nil, "*", "Comma-separated list of allowed CORS origins; 'localhost' reflects only localhost origins.", buunGroupHTTP, false),
		strFlag("cors-methods", "", nil, "GET, POST, DELETE, OPTIONS", "Comma-separated list of allowed CORS methods.", buunGroupHTTP, false),
		strFlag("cors-headers", "", nil, "*", "Comma-separated list of allowed CORS headers.", buunGroupHTTP, false),
		boolFlag("cors-credentials", "", []string{"no-cors-credentials"}, true, "Allow credentials for CORS requests.", buunGroupHTTP),
		intFlag("timeout", "to", nil, 3600, "Read/write timeout in seconds.", buunGroupHTTP, ptrutil.Ptr(0), nil),
		intFlag("sse-ping-interval", "", nil, 30, "Server SSE ping interval in seconds; -1 disables it.", buunGroupHTTP, nil, nil),
		intFlag("threads-http", "", nil, -1, "Threads for HTTP requests; -1 = automatic.", buunGroupHTTP, nil, nil),
		boolFlag("metrics", "", nil, false, "Prometheus-compatible metrics endpoint.", buunGroupHTTP),
		boolFlag("slots", "", []string{"no-slots"}, true, "Exposes slot monitoring endpoint.", buunGroupHTTP),
		boolFlag("props", "", nil, false, "Allows changing global properties via POST /props.", buunGroupHTTP),
		intFlag("parallel", "np", nil, -1, "Parallel server slots; -1 = automatic.", buunGroupHTTP, nil, nil),
		boolFlag("cont-batching", "cb", []string{"nocb", "no-cont-batching"}, true, "Enables or disables continuous batching.", buunGroupHTTP),
		boolFlag("cache-prompt", "", []string{"no-cache-prompt"}, true, "Enables prompt reuse/cache.", buunGroupHTTP),
		intFlag("cache-reuse", "", nil, 0, "Minimum chunk size for attempting reuse via KV shifting.", buunGroupHTTP, ptrutil.Ptr(0), nil),
		strFlag("slot-save-path", "", nil, nil, "Directory to save slot KV cache.", buunGroupHTTP, false),
		floatFlag("slot-prompt-similarity", "sps", nil, 0.1, "How closely a request prompt must match a slot's prompt to reuse it; 0 disables it.", buunGroupHTTP, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		strFlag("media-path", "", nil, nil, "Directory for loading local media files via file:// URLs.", buunGroupHTTP, false),
		strFlag("alias", "a", nil, nil, "Model name aliases (API), comma-separated.", buunGroupHTTP, false),
		strFlag("tags", "", nil, nil, "Model tags, comma-separated (informational, not used for routing).", buunGroupHTTP, false),

		strFlag("mmproj", "mm", nil, nil, "Projector multimodal.", buunGroupMultimodal, false),
		strFlag("mmproj-url", "mmu", nil, nil, "Multimodal projector URL.", buunGroupMultimodal, false),
		boolFlag("mmproj-auto", "", []string{"no-mmproj-auto", "no-mmproj"}, true, "Automatic multimodal projector use.", buunGroupMultimodal),
		boolFlag("mmproj-offload", "", []string{"no-mmproj-offload"}, true, "Projector offload to GPU.", buunGroupMultimodal),
		boolFlag("mmproj-gpu-swap", "", nil, false, "Temporarily swaps the MTP draft context out of VRAM so mmproj can encode images on the GPU, then swaps it back.", buunGroupMultimodal),
		strFlag("mmproj-device", "mmdev", nil, nil, "Device used for the multimodal projector; 'none' disables offload and 'auto' is the default.", buunGroupMultimodal, false),
		intFlag("image-min-tokens", "", nil, nil, "Minimum tokens per image.", buunGroupMultimodal, ptrutil.Ptr(0), nil),
		intFlag("image-max-tokens", "", nil, nil, "Maximum tokens per image.", buunGroupMultimodal, ptrutil.Ptr(0), nil),
		intFlag("mtmd-batch-max-tokens", "", nil, 1024, "Maximum image tokens per batch when encoding images.", buunGroupMultimodal, ptrutil.Ptr(1), nil),
		floatFlag("video-fps", "", nil, 4.0, "Target video frame rate.", buunGroupMultimodal, nil, nil),
		intFlag("video-timestamp-interval", "", nil, 5000, "Interval in milliseconds between text timestamps for video.", buunGroupMultimodal, ptrutil.Ptr(0), nil),
		strFlag("video-ffmpeg-dir", "", nil, nil, "Directory containing ffmpeg and ffprobe; unset searches PATH.", buunGroupMultimodal, false),

		strFlag("models-dir", "", nil, nil, "Directory monitored by the multi-model router.", buunGroupRouter, false),
		strFlag("models-preset", "", nil, nil, "Model preset loaded by the multi-model router.", buunGroupRouter, false),
		intFlag("models-max", "", nil, 4, "Maximum number of models the router loads simultaneously; 0 = unlimited.", buunGroupRouter, ptrutil.Ptr(0), nil),
		boolFlag("models-autoload", "", []string{"no-models-autoload"}, true, "Automatically loads models from the directory/preset.", buunGroupRouter),

		strFlag("tools", "", nil, nil, "Comma-separated built-in agent tools to expose, or \"all\"; limits --cors-origins to localhost by default.", buunGroupTools, false),
		strFlag("tools-runtime", "", nil, nil, "Runs tools in a separate runtime: docker:<image> or docker-container:<id>; unset uses the host environment.", buunGroupTools, false),
		strFlag("mcp-servers-config", "", nil, nil, "Experimental: path to a JSON file with MCP server definitions (Cursor-compatible).", buunGroupTools, false),
		strFlag("mcp-servers-json", "", nil, nil, "Experimental: inline JSON with MCP server definitions (Cursor-compatible).", buunGroupTools, false),
		boolFlag("agent", "ag", []string{"no-agent"}, false, "Enables the MCP CORS proxy and all built-in tools; do not enable in untrusted environments.", buunGroupTools),

		boolFlag("embeddings", "", []string{"embedding"}, false, "Embeddings mode.", buunGroupEmbeddings),
		enumFlag("pooling", "", nil, []string{"none", "mean", "cls", "last", "rank"}, nil, "Embedding pooling.", buunGroupEmbeddings),
		intFlag("embd-normalize", "", nil, 2, "Normalization; 2 = Euclidean.", buunGroupEmbeddings, nil, nil),
		boolFlag("reranking", "", []string{"rerank"}, false, "Reranking endpoint.", buunGroupEmbeddings),

		strFlag("lora", "", nil, nil, "LoRA adapters.", buunGroupLora, false),
		strFlag("lora-scaled", "", nil, nil, "LoRA with manual scale in FILE:SCALE format.", buunGroupLora, false),
		boolFlag("lora-init-without-apply", "", nil, false, "Loads LoRA without applying it.", buunGroupLora),
		strFlag("control-vector", "", nil, nil, "Control vector(s).", buunGroupLora, false),
		strFlag("control-vector-scaled", "", nil, nil, "Control vector with scale in FILE:SCALE format.", buunGroupLora, false),
		withArity(strFlag("control-vector-layer-range", "", nil, nil, "Layer range applied to control vectors; START END.", buunGroupLora, false), 2),

		listEnumFlag("spec-type", "", nil, buunSpecTypes(), "none", "Speculative decoding type (comma-separated list, e.g. dflash,ngram-mod).", buunGroupSpeculative),
		strFlag("spec-draft-model", "md", []string{"model-draft", "draft-model"}, nil, "Draft model for speculative decoding.", buunGroupSpeculative, false),
		strFlag("spec-draft-hf", "hfd", []string{"hfrd", "hf-repo-draft"}, nil, "Hugging Face repository for the draft model.", buunGroupSpeculative, false),
		intFlag("ctx-size-draft", "cd", nil, 0, "Draft-model context size; 0 inherits the target's --ctx-size.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-draft-n-max", "", []string{"draft", "draft-n", "draft-max"}, 3, "Maximum tokens proposed by the draft model.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-draft-n-min", "", []string{"draft-min", "draft-n-min"}, 0, "Minimum number of draft tokens.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		floatFlag("spec-synth-len", "", nil, nil, "Target mean synthetic acceptance length, including the target token (benchmarking only).", buunGroupSpeculative, nil, nil),
		strFlag("spec-synth-rates", "", nil, nil, "Comma-separated unconditional per-position synthetic acceptance probabilities (benchmarking only).", buunGroupSpeculative, false),
		floatFlag("spec-draft-p-split", "", []string{"draft-p-split"}, 0.1, "Speculative decoding split probability.", buunGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("spec-draft-p-min", "", []string{"draft-p-min"}, 0.0, "Minimum speculative decoding probability for the greedy path.", buunGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		strFlag("spec-draft-device", "devd", []string{"device-draft"}, nil, "Devices used to offload the draft model; follows --device by default.", buunGroupSpeculative, false),
		withKeywords(intFlag("spec-draft-ngl", "ngld", []string{"gpu-layers-draft", "n-gpu-layers-draft"}, -1, "Draft layers in VRAM; accepts an exact integer, auto (-1), or all (-2).", buunGroupSpeculative, ptrutil.Ptr(-2), ptrutil.Ptr(9999)), "auto", "all"),
		intFlag("spec-draft-threads", "td", []string{"threads-draft"}, nil, "CPU threads used for draft generation; follows --threads by default.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-draft-threads-batch", "tbd", []string{"threads-batch-draft"}, nil, "Threads for draft batch/prefill processing; follows --spec-draft-threads by default.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		boolFlag("spec-draft-cpu-moe", "cmoed", []string{"cpu-moe-draft"}, false, "Keep all draft-model Mixture-of-Experts weights on the CPU.", buunGroupSpeculative),
		intFlag("spec-draft-n-cpu-moe", "ncmoed", []string{"spec-draft-ncmoe", "n-cpu-moe-draft"}, nil, "Keep the first N draft-model Mixture-of-Experts layers on the CPU.", buunGroupSpeculative, ptrutil.Ptr(0), nil),
		strFlag("spec-draft-override-tensor", "otd", []string{"override-tensor-draft"}, nil, "Override draft-model tensor buffer type, <pattern>=<buffer type> (comma-separated).", buunGroupSpeculative, false),
		withArity(strFlag("spec-draft-replace", "", []string{"spec-replace"}, nil, "Translate TARGET into DRAFT when the draft and main models are not tokenizer-compatible; TARGET DRAFT.", buunGroupSpeculative, false), 2),
		boolFlag("spec-draft-backend-sampling", "", []string{"no-spec-draft-backend-sampling"}, true, "Draft sampling in the backend.", buunGroupSpeculative),
		enumFlag("cache-type-k-draft", "ctkd", []string{"spec-draft-type-k"}, buunDraftCacheTypes(), "f16", "K cache data type used by the draft model.", buunGroupSpeculative),
		enumFlag("cache-type-v-draft", "ctvd", []string{"spec-draft-type-v"}, buunDraftCacheTypes(), "f16", "V cache data type used by the draft model.", buunGroupSpeculative),
		intFlag("spec-ngram-mod-n-min", "", nil, 48, "Minimum tokens for ngram-mod speculative decoding.", buunGroupSpeculative, ptrutil.Ptr(0), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-mod-n-max", "", nil, 64, "Maximum tokens for ngram-mod speculative decoding.", buunGroupSpeculative, ptrutil.Ptr(0), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-mod-n-match", "", nil, 24, "Lookup length for ngram-mod speculative decoding.", buunGroupSpeculative, ptrutil.Ptr(1), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-simple-size-n", "", nil, 12, "Lookup n-gram size for ngram-simple speculative decoding.", buunGroupSpeculative, ptrutil.Ptr(1), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-simple-size-m", "", nil, 48, "Draft m-gram size for ngram-simple speculative decoding.", buunGroupSpeculative, ptrutil.Ptr(1), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-simple-min-hits", "", nil, 1, "Minimum hits for ngram-simple speculative decoding.", buunGroupSpeculative, ptrutil.Ptr(1), nil),
		intFlag("spec-ngram-map-k-size-n", "", nil, 12, "Lookup n-gram size for ngram-map-k speculative decoding.", buunGroupSpeculative, ptrutil.Ptr(1), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-map-k-size-m", "", nil, 48, "Draft m-gram size for ngram-map-k speculative decoding.", buunGroupSpeculative, ptrutil.Ptr(1), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-map-k-min-hits", "", nil, 1, "Minimum hits for ngram-map-k speculative decoding.", buunGroupSpeculative, ptrutil.Ptr(1), nil),
		intFlag("spec-ngram-map-k4v-size-n", "", nil, 12, "Lookup n-gram size for ngram-map-k4v speculative decoding.", buunGroupSpeculative, ptrutil.Ptr(1), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-map-k4v-size-m", "", nil, 48, "Draft m-gram size for ngram-map-k4v speculative decoding.", buunGroupSpeculative, ptrutil.Ptr(1), ptrutil.Ptr(1024)),
		intFlag("spec-ngram-map-k4v-min-hits", "", nil, 1, "Minimum hits for ngram-map-k4v speculative decoding.", buunGroupSpeculative, ptrutil.Ptr(1), nil),
		strFlag("lookup-cache-static", "lcs", nil, nil, "Path to a static lookup cache used for ngram-cache speculative decoding (not updated during generation).", buunGroupSpeculative, false),
		strFlag("lookup-cache-dynamic", "lcd", nil, nil, "Path to a dynamic lookup cache used for ngram-cache speculative decoding (updated during generation).", buunGroupSpeculative, false),
		boolFlag("spec-default", "", nil, false, "Enables the default speculative-decoding config (ngram-mod).", buunGroupSpeculative),
		boolFlag("spec-dflash-default", "", nil, false, "Enables configuration/defaults for speculative DFlash mode; requires a draft model.", buunGroupSpeculative),
		intFlag("dflash-max-slots", "", nil, 1, "Maximum number of concurrent server slots that keep DFlash state.", buunGroupSpeculative, ptrutil.Ptr(1), nil),

		boolFlag("cache-debug", "", nil, false, "Emits one shadow cache-plan decision record per request as a JSON log line and exposes the last record in /slots.", buunGroupCachePlan),
		boolFlag("cache-plan-preflight", "", nil, false, "Exposes the trusted-local POST /cache/plan preview route.", buunGroupCachePlan),
		boolFlag("cache-control-api", "", nil, false, "Exposes the trusted-local cache-control routes.", buunGroupCachePlan),
		enumFlag("cache-plan-authority", "", nil, []string{"off", "by_id", "similarity", "route_home", "lru"}, "off", "Dual-runs the cache-plan authority substrate at the given level.", buunGroupCachePlan),
		boolFlag("cache-lifecycle", "", nil, false, "Enables the cache-lifecycle authority substrate (accounting-gated admission).", buunGroupCachePlan),
		boolFlag("cache-receipt", "", nil, false, "Attaches a cache receipt (keyed chained block-hash divergence hint) to responses.", buunGroupCachePlan),
		strFlag("cache-receipt-key", "", nil, nil, "Per-session/tenant comparison key for the cache-receipt chain; required unless the unkeyed debug flag is set.", buunGroupCachePlan, false),
		boolFlag("cache-receipt-unkeyed-debug", "", nil, false, "Allows an unkeyed cache-receipt chain; trusted local/debug only, it leaks prompt-content comparability.", buunGroupCachePlan),
	} {
		add(spec)
	}

	return domain.BackendValidationSchema{
		SchemaVersion: 1,
		Kind:          domain.ValidationSchemaCLIFlagsV1,
		BackendKind:   domain.BackendKindBuunLlamaCpp,
		BackendID:     "buun-llama-cpp-default",
		Source: domain.SchemaSource{
			GeneratedFrom: "buun-llama-cpp/common/arg.cpp",
			SourceVersion: buunSourceVersion,
			Editable:      true,
		},
		Flags:        flags,
		Presentation: BuunPresentation(),
	}
}

// buunSourceVersion identifies the buun-llama-cpp revision this curated schema
// was reconciled against. The fork reports two independent identifiers: the
// build number printed by `llama-server --version` and the checkout's
// `git describe --tags --always`. Both are recorded so the value is
// unambiguous.
const buunSourceVersion = "11806 (c9c52d718) / b9637-2169-gc9c52d718"

func buunSpecTypes() []string {
	return []string{"none", "draft-simple", "draft-eagle3", "draft-mtp", "draft-dflash", "draft-dspark", "ngram-simple", "ngram-map-k", "ngram-map-k4v", "ngram-mod", "ngram-cache", "suffix", "copyspec", "recycle", "dflash"}
}

func buunCacheTypes() []string {
	return append(buunBaseCacheTypes(), "vbr")
}

func buunDraftCacheTypes() []string {
	return buunBaseCacheTypes()
}

func buunBaseCacheTypes() []string {
	return []string{"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1", "turbo2", "turbo3", "turbo4", "turbo8", "turbo3_tcq", "turbo2_tcq", "turbo1_tcq"}
}

func withAllowedInts(spec domain.FlagSpec, allowed ...int) domain.FlagSpec {
	spec.AllowedInts = allowed
	return spec
}

func BuunPresentation() *domain.Presentation {
	return &domain.Presentation{Groups: []domain.PresentationGroup{
		{Name: buunGroupEssentials, Highlighted: true, Flags: []string{"hf-repo", "hf-token", "ctx-size", "host", "port", "n-gpu-layers", "device", "flash-attn", "load-mode", "parallel", "threads", "batch-size", "ubatch-size", "cache-type", "cache-type-k", "cache-type-v", "cache-ram", "kv-unified", "cont-batching", "api-key", "alias"}},
		{Name: buunGroupModelLoad, Flags: []string{"hf-repo", "hf-file", "hf-token", "model-url", "docker-repo", "override-kv", "offline"}},
		{Name: buunGroupContext, Flags: []string{"ctx-size", "n-predict", "batch-size", "ubatch-size", "keep"}},
		{Name: buunGroupCPU, Flags: []string{"threads", "threads-batch", "poll", "prio", "numa"}},
		{Name: buunGroupMemory, Flags: []string{"load-mode", "mlock", "mmap", "direct-io", "lazy-mode", "mmap-prefetch", "repack", "op-offload", "no-host", "logits-all", "check-tensors"}},
		{Name: buunGroupDevice, Flags: []string{"device", "list-devices", "n-gpu-layers", "split-mode", "tensor-split", "main-gpu", "override-tensor", "cpu-moe", "n-cpu-moe", "n-cpu-ffn", "moe-cache-profile", "no-fused-gdn", "fit", "fit-target", "fit-ctx"}},
		{Name: buunGroupKV, Flags: []string{"kv-offload", "flash-attn", "cache-type", "cache-type-k", "cache-type-v", "vbr-budget", "vbr-min-bits", "vbr-vram-budget", "vbr-reclaim-floor", "vbr-reset-keep-frac", "vbr-policy", "vbr-entry"}},
		{Name: buunGroupRope, Flags: []string{"rope-scaling", "rope-scale", "rope-freq-base", "rope-freq-scale", "yarn-orig-ctx", "yarn-ext-factor", "yarn-attn-factor", "yarn-beta-slow", "yarn-beta-fast"}},
		{Name: buunGroupShift, Flags: []string{"swa-full", "context-shift", "ctx-checkpoints", "checkpoint-min-step"}},
		{Name: buunGroupCacheRAM, Flags: []string{"cache-ram", "kv-unified", "kv-unified-per-slot", "cache-idle-slots", "sleep-idle-seconds"}},
		{Name: buunGroupSamplers, Flags: []string{"samplers", "sampler-seq", "seed", "temperature", "top-k", "top-p", "min-p", "typical-p", "top-n-sigma", "xtc-probability", "xtc-threshold", "ignore-eos", "logit-bias", "backend-sampling"}},
		{Name: buunGroupPenalties, Flags: []string{"repeat-last-n", "repeat-penalty", "presence-penalty", "frequency-penalty", "dry-multiplier", "dry-base", "dry-allowed-length", "dry-penalty-last-n", "dry-sequence-breaker", "mirostat", "mirostat-lr", "mirostat-ent", "dynatemp-range", "dynatemp-exp", "adaptive-target", "adaptive-decay"}},
		{Name: buunGroupGrammar, Flags: []string{"grammar", "grammar-file", "json-schema", "json-schema-file"}},
		{Name: buunGroupChat, Flags: []string{"chat-template", "chat-template-file", "chat-template-kwargs", "jinja", "skip-chat-parsing", "prefill-assistant", "reasoning-format", "reasoning", "reasoning-budget", "reasoning-budget-message", "reasoning-preserve", "reasoning-effort"}},
		{Name: buunGroupHTTP, Flags: []string{"host", "port", "reuse-port", "api-prefix", "path", "ui", "ui-config", "ui-config-file", "ui-mcp-proxy", "api-key", "api-key-file", "ssl-key-file", "ssl-cert-file", "cors-origins", "cors-methods", "cors-headers", "cors-credentials", "timeout", "sse-ping-interval", "threads-http", "metrics", "slots", "props", "parallel", "cont-batching", "cache-prompt", "cache-reuse", "slot-save-path", "slot-prompt-similarity", "media-path", "alias", "tags"}},
		{Name: buunGroupMultimodal, Flags: []string{"mmproj", "mmproj-url", "mmproj-auto", "mmproj-offload", "mmproj-gpu-swap", "mmproj-device", "image-min-tokens", "image-max-tokens", "mtmd-batch-max-tokens", "video-fps", "video-timestamp-interval", "video-ffmpeg-dir"}},
		{Name: buunGroupRouter, Flags: []string{"models-dir", "models-preset", "models-max", "models-autoload"}},
		{Name: buunGroupTools, Flags: []string{"tools", "tools-runtime", "mcp-servers-config", "mcp-servers-json", "agent"}},
		{Name: buunGroupEmbeddings, Flags: []string{"embeddings", "pooling", "embd-normalize", "reranking"}},
		{Name: buunGroupLora, Flags: []string{"lora", "lora-scaled", "lora-init-without-apply", "control-vector", "control-vector-scaled", "control-vector-layer-range"}},
		{Name: buunGroupSpeculative, Flags: []string{"spec-type", "spec-draft-model", "spec-draft-hf", "ctx-size-draft", "spec-draft-n-max", "spec-draft-n-min", "spec-synth-len", "spec-synth-rates", "spec-draft-p-split", "spec-draft-p-min", "spec-draft-device", "spec-draft-ngl", "spec-draft-threads", "spec-draft-threads-batch", "spec-draft-cpu-moe", "spec-draft-n-cpu-moe", "spec-draft-override-tensor", "spec-draft-replace", "spec-draft-backend-sampling", "cache-type-k-draft", "cache-type-v-draft", "spec-ngram-mod-n-min", "spec-ngram-mod-n-max", "spec-ngram-mod-n-match", "spec-ngram-simple-size-n", "spec-ngram-simple-size-m", "spec-ngram-simple-min-hits", "spec-ngram-map-k-size-n", "spec-ngram-map-k-size-m", "spec-ngram-map-k-min-hits", "spec-ngram-map-k4v-size-n", "spec-ngram-map-k4v-size-m", "spec-ngram-map-k4v-min-hits", "lookup-cache-static", "lookup-cache-dynamic", "spec-default", "spec-dflash-default", "dflash-max-slots"}},
		{Name: buunGroupCachePlan, Flags: []string{"cache-debug", "cache-plan-preflight", "cache-control-api", "cache-plan-authority", "cache-lifecycle", "cache-receipt", "cache-receipt-key", "cache-receipt-unkeyed-debug"}},
	}}
}

package backendschema

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

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
		strFlag("hf-repo", "hf", []string{"hfr"}, nil, "Downloads/loads a model directly from Hugging Face; quant is optional.", llamaGroupModelLoad, false),
		strFlag("hf-token", "hft", nil, nil, "Hugging Face access token (overrides the HF_TOKEN environment variable).", llamaGroupModelLoad, false),
		strFlag("override-kv", "", nil, nil, "Override model metadata by key (KEY=TYPE:VALUE, comma-separated).", llamaGroupModelLoad, false),
		strFlag("hf-file", "hff", nil, nil, "Specific file inside the HF repository, overriding automatic quant selection.", llamaGroupModelLoad, false),
		strFlag("model-url", "mu", nil, nil, "Model download URL.", llamaGroupModelLoad, false),
		boolFlag("offline", "", nil, false, "Forces offline mode using only local cache, with no network.", llamaGroupModelLoad),
		strFlag("docker-repo", "dr", nil, nil, "Docker Hub model repository in [repo/]model[:quant] format.", llamaGroupModelLoad, false),
		strFlag("hf-repo-v", "hfv", []string{"hfrv"}, nil, "Hugging Face model repository for the vocoder model; quant is optional.", llamaGroupModelLoad, false),
		strFlag("hf-file-v", "hffv", nil, nil, "Specific vocoder file inside the HF repository.", llamaGroupModelLoad, false),

		intFlag("ctx-size", "c", nil, 0, "Context window size; 0 uses the value loaded from the model.", llamaGroupContext, ptrutil.Ptr(0), nil),
		intFlag("n-predict", "n", []string{"predict"}, -1, "Number of tokens to generate; -1 = infinite generation.", llamaGroupContext, nil, nil),
		intFlag("batch-size", "b", nil, 2048, "Maximum logical batch size for prompt processing.", llamaGroupContext, ptrutil.Ptr(1), nil),
		intFlag("ubatch-size", "ub", nil, 512, "Maximum physical micro-batch size (throughput/memory tuning).", llamaGroupContext, ptrutil.Ptr(1), nil),
		intFlag("keep", "", nil, 0, "Initial prompt tokens to keep during shifting/reuse; -1 keeps all.", llamaGroupContext, nil, nil),

		intFlag("threads", "t", nil, nil, "CPU threads used for generation.", llamaGroupCPU, ptrutil.Ptr(0), nil),
		intFlag("threads-batch", "tb", nil, nil, "Threads for batch/prefill processing; inherits --threads by default.", llamaGroupCPU, ptrutil.Ptr(0), nil),
		intFlag("poll", "", nil, 50, "Polling level while waiting for work; 0 disables it.", llamaGroupCPU, ptrutil.Ptr(0), ptrutil.Ptr(100)),
		enumFlag("prio", "", nil, []string{"-1", "0", "1", "2", "3"}, 0, "Process/thread priority: low, normal, medium, high, realtime.", llamaGroupCPU),
		enumFlag("numa", "", nil, []string{"distribute", "isolate", "numactl"}, nil, "Optimizations for NUMA machines.", llamaGroupCPU),
		strFlag("cpu-mask", "C", nil, nil, "CPU affinity mask: arbitrarily long hex. Complements cpu-range", llamaGroupCPU, false),
		intFlag("cpu-strict", "", nil, 0, "Use strict CPU placement.", llamaGroupCPU, ptrutil.Ptr(0), ptrutil.Ptr(1)),
		strFlag("cpu-range", "Cr", nil, nil, "Range of CPUs for affinity (lo-hi). Complements --cpu-mask.", llamaGroupCPU, false),
		strFlag("cpu-mask-batch", "Cb", nil, nil, "Batch CPU affinity mask; inherits --cpu-mask by default.", llamaGroupCPU, false),
		strFlag("cpu-range-batch", "Crb", nil, nil, "Batch CPU range for affinity; inherits --cpu-range by default.", llamaGroupCPU, false),
		strFlag("cpu-strict-batch", "", nil, nil, "Use strict batch CPU placement; inherits --cpu-strict by default.", llamaGroupCPU, false),
		enumFlag("prio-batch", "", nil, []string{"0", "1", "2", "3"}, nil, "Batch process/thread priority; inherits --prio by default.", llamaGroupCPU),
		intFlag("poll-batch", "", nil, nil, "Batch polling level; inherits --poll by default.", llamaGroupCPU, ptrutil.Ptr(0), ptrutil.Ptr(1)),

		boolFlag("mlock", "", nil, false, "DEPRECATED in favor of --load-mode: keeps the model in RAM, avoiding swap.", llamaGroupMemory),
		boolFlag("mmap", "", []string{"no-mmap"}, true, "DEPRECATED in favor of --load-mode: model memory mapping; disabling it may reduce pageouts but makes loading slower.", llamaGroupMemory),
		boolFlag("direct-io", "dio", []string{"no-direct-io"}, false, "DEPRECATED in favor of --load-mode: uses Direct I/O when available.", llamaGroupMemory),
		enumFlag("load-mode", "lm", nil, []string{"none", "mmap", "mlock", "mmap+mlock", "dio"}, "mmap", "Model loading mode (replaces --mlock/--mmap/--direct-io).", llamaGroupMemory),
		boolFlag("repack", "", []string{"no-repack"}, true, "Enables/disables weight repacking.", llamaGroupMemory),
		boolFlag("op-offload", "", []string{"no-op-offload"}, true, "Offloads tensor operations from host to device.", llamaGroupMemory),
		boolFlag("no-host", "", nil, false, "Bypasses the host buffer to allow extra buffers.", llamaGroupMemory),
		boolFlag("check-tensors", "", nil, false, "Checks model tensors for invalid values.", llamaGroupMemory),

		strFlag("device", "dev", nil, nil, "Selects the devices used for offload.", llamaGroupDevice, false),
		boolFlag("list-devices", "", nil, false, "Lists available devices and exits.", llamaGroupDevice),
		withKeywords(intFlag("n-gpu-layers", "ngl", []string{"gpu-layers"}, -1, "Number of model layers moved to VRAM; accepts an exact integer, auto (-1), or all (-2).", llamaGroupDevice, ptrutil.Ptr(-2), ptrutil.Ptr(9999)), "auto", "all"),
		enumFlag("flash-attn", "fa", nil, []string{"on", "off", "auto"}, "auto", "Flash Attention mode.", llamaGroupDevice),
		enumFlag("split-mode", "sm", nil, []string{"none", "layer", "row", "tensor"}, "layer", "How to split the model across multiple GPUs.", llamaGroupDevice),
		strFlag("tensor-split", "ts", nil, nil, "Offload ratio across GPUs (e.g. 3,1).", llamaGroupDevice, false),
		intFlag("main-gpu", "mg", nil, 0, "Main GPU for the model/intermediate results (depends on split mode).", llamaGroupDevice, ptrutil.Ptr(0), nil),
		enumFlag("fit", "fit", nil, []string{"on", "off"}, "on", "Adjusts unset parameters to fit device memory.", llamaGroupDevice),
		strFlag("fit-target", "fitt", nil, nil, "Target memory margin per device used by --fit.", llamaGroupDevice, false),
		intFlag("fit-ctx", "fitc", nil, 4096, "Minimum context that --fit may configure.", llamaGroupDevice, ptrutil.Ptr(0), nil),
		boolFlag("cpu-moe", "cmoe", nil, false, "Keep all Mixture of Experts (MoE) weights in the CPU.", llamaGroupDevice),
		intFlag("n-cpu-moe", "ncmoe", nil, nil, "Keep the MoE weights of the first N layers in the CPU.", llamaGroupDevice, ptrutil.Ptr(0), nil),
		strFlag("override-tensor", "ot", nil, nil, "Override tensor buffer type (<pattern>=<buffer>, comma-separated).", llamaGroupDevice, false),

		boolFlag("kv-offload", "kvo", []string{"no-kv-offload"}, true, "Controls KV cache offload.", llamaGroupKV),
		enumFlag("cache-type-k", "ctk", nil, llamaCacheTypes(), "f16", "K cache data type.", llamaGroupKV),
		enumFlag("cache-type-v", "ctv", nil, llamaCacheTypes(), "f16", "V cache data type.", llamaGroupKV),
		boolFlag("kv-unified", "kvu", []string{"no-kv-unified"}, true, "Use a single unified KV buffer shared across all sequences.", llamaGroupKV),
		floatFlag("defrag-thold", "dt", nil, nil, "KV cache defragmentation threshold (DEPRECATED).", llamaGroupKV, nil, nil),
		strFlag("kv-mean-center", "", nil, nil, "Path to a K-cache mean-centering bias file (GGUF); requires --cache-type-k q4_0.", llamaGroupKV, false),


		enumFlag("rope-scaling", "", nil, []string{"none", "linear", "yarn"}, nil, "RoPE frequency scaling method; unset defers to the model (linear otherwise).", llamaGroupRope),
		floatFlag("rope-scale", "", nil, nil, "Context expansion factor via RoPE.", llamaGroupRope, nil, nil),
		floatFlag("rope-freq-base", "", nil, nil, "RoPE base frequency (NTK-aware scaling).", llamaGroupRope, nil, nil),
		floatFlag("rope-freq-scale", "", nil, nil, "Frequency scaling factor; expands context by 1/N.", llamaGroupRope, nil, nil),
		intFlag("yarn-orig-ctx", "", nil, 0, "Original model context for YaRN; 0 uses the training value.", llamaGroupRope, ptrutil.Ptr(0), nil),
		floatFlag("yarn-ext-factor", "", nil, -1.0, "YaRN extrapolation/interpolation factor.", llamaGroupRope, nil, nil),
		floatFlag("yarn-attn-factor", "", nil, -1.0, "Attention magnitude adjustment in YaRN; -1.0 derives from the model.", llamaGroupRope, nil, nil),
		floatFlag("yarn-beta-slow", "", nil, -1.0, "YaRN slow/high correction dim parameter; -1.0 derives from the model.", llamaGroupRope, nil, nil),
		floatFlag("yarn-beta-fast", "", nil, -1.0, "YaRN fast/low correction dim parameter; -1.0 derives from the model.", llamaGroupRope, nil, nil),

		boolFlag("swa-full", "", nil, false, "Uses full-size SWA cache.", llamaGroupShift),
		boolFlag("context-shift", "", []string{"no-context-shift"}, false, "Controls context shift during infinite generation.", llamaGroupShift),
		intFlag("ctx-checkpoints", "ctxcp", []string{"swa-checkpoints"}, 32, "Max number of context checkpoints to create per slot.", llamaGroupShift, ptrutil.Ptr(0), nil),
		intFlag("checkpoint-min-step", "cms", nil, 256, "Minimum spacing between context checkpoints in tokens; 0 = no minimum.", llamaGroupShift, ptrutil.Ptr(0), nil),
		boolFlag("warmup", "", []string{"no-warmup"}, true, "Perform warmup with an empty run on startup.", llamaGroupShift),
		boolFlag("spm-infill", "", nil, false, "Use Suffix/Prefix/Middle pattern for infill.", llamaGroupShift),

		strFlag("samplers", "", nil, "penalties;dry;top_n_sigma;top_k;typ_p;top_p;min_p;xtc;temperature", "Order of samplers applied during generation.", llamaGroupSamplers, false),
		intFlag("seed", "s", nil, -1, "RNG seed; -1 = random.", llamaGroupSamplers, nil, nil),
		floatFlag("temperature", "", []string{"temp"}, 0.8, "Sampling temperature; higher = more diversity.", llamaGroupSamplers, ptrutil.Ptr(0.0), nil),
		intFlag("top-k", "", nil, 40, "Keeps the k most likely tokens; 0 disables it.", llamaGroupSamplers, ptrutil.Ptr(0), nil),
		floatFlag("top-p", "", nil, 0.95, "Nucleus sampling; 1.0 disables it.", llamaGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("min-p", "", nil, 0.05, "Keeps tokens above a relative minimum probability.", llamaGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("typical-p", "", []string{"typical"}, 1.0, "Locally typical sampling; 1.0 disables it.", llamaGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("top-n-sigma", "", []string{"top-nsigma"}, -1.0, "Top-n-sigma sampling; -1.0 disables it.", llamaGroupSamplers, nil, nil),
		floatFlag("xtc-probability", "", nil, 0.0, "XTC sampling probability; 0.0 disables it.", llamaGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("xtc-threshold", "", nil, 0.1, "XTC sampling threshold; above 0.5 disables it.", llamaGroupSamplers, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		strFlag("sampler-seq", "", []string{"sampling-seq"}, "edskypmxt", "Simplified one-letter sampler order sequence.", llamaGroupSamplers, false),
		boolFlag("ignore-eos", "", nil, false, "Ignores the EOS token and continues generating.", llamaGroupSamplers),
		boolFlag("backend-sampling", "bs", nil, false, "Enables backend sampling (experimental).", llamaGroupSamplers),
		strFlag("logit-bias", "l", nil, nil, "Increases/decreases the chance of specific tokens in TOKEN_ID(+/-)BIAS format.", llamaGroupSamplers, false),

		intFlag("repeat-last-n", "", nil, 64, "Recent tokens considered for repetition penalty; -1 = ctx-size.", llamaGroupPenalties, nil, nil),
		floatFlag("repeat-penalty", "", nil, 1.0, "Repetition penalty; 1.0 disables it.", llamaGroupPenalties, nil, nil),
		floatFlag("presence-penalty", "", nil, 0.0, "Presence penalty; increases the cost of already-seen tokens.", llamaGroupPenalties, nil, nil),
		floatFlag("frequency-penalty", "", nil, 0.0, "Penalty proportional to the frequency of already-seen tokens.", llamaGroupPenalties, nil, nil),
		floatFlag("dry-multiplier", "", nil, 0.0, "DRY intensity; 0.0 disables it.", llamaGroupPenalties, ptrutil.Ptr(0.0), nil),
		floatFlag("dry-base", "", nil, 1.75, "DRY base for penalizing repetitive sequences.", llamaGroupPenalties, nil, nil),
		intFlag("dry-allowed-length", "", nil, 2, "Tolerated length before DRY applies.", llamaGroupPenalties, ptrutil.Ptr(0), nil),
		intFlag("dry-penalty-last-n", "", nil, -1, "DRY window; -1 = full context.", llamaGroupPenalties, nil, nil),
		strFlag("dry-sequence-breaker", "", nil, "\\n, :, \", *", "Breaks that reset DRY.", llamaGroupPenalties, false),
		intFlag("mirostat", "", nil, 0, "Mirostat mode; 0 disables it.", llamaGroupPenalties, ptrutil.Ptr(0), ptrutil.Ptr(2)),
		floatFlag("mirostat-lr", "", nil, 0.1, "Mirostat learning rate (eta).", llamaGroupPenalties, nil, nil),
		floatFlag("mirostat-ent", "", nil, 5.0, "Mirostat target entropy (tau).", llamaGroupPenalties, nil, nil),
		floatFlag("dynatemp-range", "", nil, 0.0, "Dynamic temperature range; 0.0 disables it.", llamaGroupPenalties, ptrutil.Ptr(0.0), nil),
		floatFlag("dynatemp-exp", "", nil, 1.0, "Dynamic temperature exponent.", llamaGroupPenalties, nil, nil),
		floatFlag("adaptive-target", "", nil, -1.0, "Adaptive-p target; negative values disable.", llamaGroupPenalties, nil, ptrutil.Ptr(1.0)),
		floatFlag("adaptive-decay", "", nil, 0.9, "Adaptive-p decay; lower is more reactive, higher more stable.", llamaGroupPenalties, ptrutil.Ptr(0.0), ptrutil.Ptr(0.99)),

		strFlag("grammar", "", nil, "", "Constrains output with GBNF grammar.", llamaGroupGrammar, false),
		strFlag("grammar-file", "", nil, nil, "Reads grammar from a file.", llamaGroupGrammar, false),
		strFlag("json-schema", "j", nil, nil, "Constrains output to a JSON Schema.", llamaGroupGrammar, false),
		strFlag("json-schema-file", "jf", nil, nil, "Loads JSON Schema from a file.", llamaGroupGrammar, false),

		strFlag("chat-template", "", nil, nil, "Defines the chat template (embedded or customized).", llamaGroupChat, false),
		strFlag("chat-template-file", "", nil, nil, "Reads the chat template from a Jinja file.", llamaGroupChat, false),
		strFlag("chat-template-kwargs", "", nil, nil, "Extra JSON arguments for the template parser.", llamaGroupChat, false),
		boolFlag("jinja", "", []string{"no-jinja"}, true, "Toggles the Jinja engine for chat.", llamaGroupChat),
		boolFlag("skip-chat-parsing", "", nil, false, "Forces pure content parsing, without structural parsing.", llamaGroupChat),
		boolFlag("prefill-assistant", "", []string{"no-prefill-assistant"}, true, "Controls response prefill when the last message is already from the assistant.", llamaGroupChat),
		intFlag("reasoning-budget", "", nil, -1, "Token budget for thinking: -1 = unrestricted, 0 = immediate end, N>0 = budget.", llamaGroupChat, nil, nil),
		enumFlag("reasoning", "rea", nil, []string{"on", "off", "auto"}, "auto", "Reasoning/thinking mode in chat.", llamaGroupChat),
		enumFlag("reasoning-format", "", nil, []string{"none", "deepseek", "deepseek-legacy", "auto"}, "auto", "How thought tags are parsed and returned.", llamaGroupChat),
		strFlag("reasoning-budget-message", "", nil, nil, "Message injected before the end-of-thinking tag when the reasoning budget is exhausted.", llamaGroupChat, false),
		boolFlag("reasoning-preserve", "", []string{"no-reasoning-preserve"}, false, "Preserve the reasoning trace in the full history, not just the last assistant message (requires a template with 'supports_preserve_reasoning'); unset defers to the template default.", llamaGroupChat),

		strFlag("host", "", nil, "127.0.0.1", "Server listen address.", llamaGroupHTTP, false),
		portFlag("port", "", 8080, "HTTP server port.", llamaGroupHTTP),
		boolFlag("reuse-port", "", nil, false, "Allows multiple sockets on the same port.", llamaGroupHTTP),
		strFlag("api-prefix", "", nil, "", "API route prefix (no trailing slash).", llamaGroupHTTP, false),
		strFlag("path", "", nil, nil, "Directory of static files to serve.", llamaGroupHTTP, false),
		strFlag("cors-origins", "", nil, "*", "Comma-separated list of allowed CORS origins; special value 'localhost' reflects the Origin header only when it is localhost.", llamaGroupHTTP, false),
		strFlag("cors-methods", "", nil, "GET, POST, DELETE, OPTIONS", "Comma-separated list of allowed CORS methods.", llamaGroupHTTP, false),
		strFlag("cors-headers", "", nil, "*", "Comma-separated list of allowed CORS headers.", llamaGroupHTTP, false),
		boolFlag("cors-credentials", "", []string{"no-cors-credentials"}, true, "Allow credentials for CORS; if enabled while --cors-origins is '*', the Origin header is echoed back and credentials are always allowed.", llamaGroupHTTP),
		boolFlag("ui", "", []string{"webui", "no-ui", "no-webui"}, true, "Toggles the web interface.", llamaGroupHTTP),
		strFlag("webui-config", "", []string{"ui-config"}, nil, "Overrides default UI settings.", llamaGroupHTTP, false),
		strFlag("webui-config-file", "", []string{"ui-config-file"}, nil, "Loads UI settings from a JSON file.", llamaGroupHTTP, false),
		strFlag("api-key", "", nil, nil, "Sets API authentication keys.", llamaGroupHTTP, false),
		strFlag("api-key-file", "", nil, nil, "Reads authentication keys from a file.", llamaGroupHTTP, false),
		strFlag("ssl-key-file", "", nil, nil, "SSL private key.", llamaGroupHTTP, false),
		strFlag("ssl-cert-file", "", nil, nil, "SSL certificate.", llamaGroupHTTP, false),
		strFlag("mcp-servers-config", "", nil, nil, "Experimental: path to JSON file with MCP server definitions (Cursor-compatible).", llamaGroupHTTP, false),
		strFlag("mcp-servers-json", "", nil, nil, "Experimental: inline JSON with MCP server definitions (Cursor-compatible).", llamaGroupHTTP, false),
		intFlag("timeout", "to", nil, 3600, "Read/write timeout (seconds).", llamaGroupHTTP, ptrutil.Ptr(0), nil),
		intFlag("threads-http", "", nil, -1, "Threads for HTTP requests; -1 = automatic.", llamaGroupHTTP, nil, nil),
		boolFlag("metrics", "", nil, false, "Prometheus-compatible metrics endpoint.", llamaGroupHTTP),
		boolFlag("perf", "", []string{"no-perf"}, false, "Whether to enable internal libllama performance timings.", llamaGroupHTTP),
		boolFlag("slots", "", []string{"no-slots"}, true, "Exposes slot monitoring endpoint.", llamaGroupHTTP),
		boolFlag("props", "", nil, false, "Allows changing global properties via POST /props.", llamaGroupHTTP),
		intFlag("parallel", "np", nil, -1, "Parallel server slots; -1 = automatic.", llamaGroupHTTP, nil, nil),
		boolFlag("cont-batching", "cb", []string{"no-cont-batching"}, true, "Enables/disables continuous batching.", llamaGroupHTTP),
		boolFlag("cache-prompt", "", []string{"no-cache-prompt"}, true, "Enables prompt reuse/cache.", llamaGroupHTTP),
		intFlag("cache-reuse", "", nil, 0, "Minimum chunk size for attempting reuse via KV shifting.", llamaGroupHTTP, ptrutil.Ptr(0), nil),
		intFlag("cache-ram", "cram", nil, 8192, "Maximum prompt-cache size in MiB; -1 = no limit, 0 = disable.", llamaGroupHTTP, nil, nil),
		boolFlag("cache-idle-slots", "", []string{"no-cache-idle-slots"}, true, "Save idle slots to the prompt cache (requires cache-ram).", llamaGroupHTTP),
		intFlag("sleep-idle-seconds", "", nil, -1, "Seconds of idleness before the server sleeps; -1 disables.", llamaGroupHTTP, nil, nil),
		floatFlag("slot-prompt-similarity", "sps", nil, 0.1, "Required prompt similarity to reuse a slot; 0.0 disables.", llamaGroupHTTP, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		intFlag("sse-ping-interval", "", nil, 30, "SSE ping interval in seconds; -1 disables.", llamaGroupHTTP, nil, nil),
		strFlag("tags", "", nil, nil, "Model tags, comma-separated (informational, not used for routing).", llamaGroupHTTP, false),
		strFlag("alias", "a", nil, nil, "Model name aliases (API), comma-separated.", llamaGroupHTTP, false),
		strFlag("tools", "", nil, nil, "Experimental: enable built-in tools for AI agents (comma-separated list, or \"all\").", llamaGroupHTTP, false),
		strFlag("slot-save-path", "", nil, nil, "Path to save slot KV cache snapshots.", llamaGroupHTTP, false),
		strFlag("media-path", "", nil, nil, "Directory for loading local media files via file:// URLs.", llamaGroupHTTP, false),
		strFlag("models-dir", "", nil, nil, "Directory containing models for the router server.", llamaGroupHTTP, false),
		strFlag("models-preset", "", nil, nil, "Path to INI file with model presets for the router server.", llamaGroupHTTP, false),
		intFlag("models-max", "", nil, 4, "For router server, max models loaded simultaneously; 0 = unlimited.", llamaGroupHTTP, ptrutil.Ptr(0), nil),
		boolFlag("models-autoload", "", []string{"no-models-autoload"}, true, "For router server, automatically load models.", llamaGroupHTTP),
		strFlag("lookup-cache-static", "lcs", nil, nil, "Path to static lookup cache for lookup decoding.", llamaGroupHTTP, false),
		strFlag("lookup-cache-dynamic", "lcd", nil, nil, "Path to dynamic lookup cache updated by generation.", llamaGroupHTTP, false),


		strFlag("mmproj", "mm", nil, nil, "Projector multimodal.", llamaGroupMultimodal, false),
		strFlag("mmproj-url", "mmu", nil, nil, "Multimodal projector URL.", llamaGroupMultimodal, false),
		boolFlag("mmproj-auto", "", []string{"no-mmproj-auto", "no-mmproj"}, true, "Automatic multimodal projector use.", llamaGroupMultimodal),
		boolFlag("mmproj-offload", "", []string{"no-mmproj-offload"}, true, "Projector offload to GPU.", llamaGroupMultimodal),
		intFlag("image-min-tokens", "", nil, nil, "Minimum tokens per image.", llamaGroupMultimodal, ptrutil.Ptr(0), nil),
		intFlag("image-max-tokens", "", nil, nil, "Maximum tokens per image.", llamaGroupMultimodal, ptrutil.Ptr(0), nil),
		intFlag("mtmd-batch-max-tokens", "", nil, 1024, "Maximum image tokens per batch when encoding images.", llamaGroupMultimodal, ptrutil.Ptr(0), nil),
		strFlag("model-vocoder", "mv", nil, nil, "Path to vocoder model for audio/TTS generation.", llamaGroupMultimodal, false),
		boolFlag("tts-use-guide-tokens", "", nil, false, "Use guide tokens to improve TTS word recall.", llamaGroupMultimodal),

		boolFlag("embeddings", "", []string{"embedding"}, false, "Embeddings mode.", llamaGroupEmbeddings),
		enumFlag("pooling", "", nil, []string{"none", "mean", "cls", "last", "rank"}, nil, "Embedding pooling.", llamaGroupEmbeddings),
		intFlag("embd-normalize", "", nil, 2, "Normalization; 2 = Euclidean.", llamaGroupEmbeddings, nil, nil),
		boolFlag("reranking", "", []string{"rerank"}, false, "Reranking endpoint.", llamaGroupEmbeddings),

		strFlag("lora", "", nil, nil, "LoRA adapters.", llamaGroupLora, false),
		strFlag("lora-scaled", "", nil, nil, "LoRA with manual scale in FILE:SCALE format.", llamaGroupLora, false),
		boolFlag("lora-init-without-apply", "", nil, false, "Loads LoRA without applying it.", llamaGroupLora),
		strFlag("control-vector", "", nil, nil, "Control vector(s).", llamaGroupLora, false),
		strFlag("control-vector-scaled", "", nil, nil, "Control vector with scale in FILE:SCALE format.", llamaGroupLora, false),
		withArity(strFlag("control-vector-layer-range", "", nil, nil, "Layer range applied to control vectors; START END.", llamaGroupLora, false), 2),

		listEnumFlag("spec-type", "", nil, []string{"none", "draft-simple", "draft-eagle3", "draft-mtp", "draft-dflash", "ngram-simple", "ngram-map-k", "ngram-map-k4v", "ngram-mod", "ngram-cache"}, "none", "Speculative decoding type (comma-separated list, e.g. draft-mtp,ngram-mod).", llamaGroupSpeculative),
		strFlag("spec-draft-model", "md", []string{"model-draft"}, nil, "Draft model.", llamaGroupSpeculative, false),
		strFlag("spec-draft-hf", "", nil, nil, "HF repo for the draft model.", llamaGroupSpeculative, false),
		intFlag("spec-draft-n-max", "", nil, 3, "Maximum tokens proposed by the draft model.", llamaGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-draft-n-min", "", nil, 0, "Minimum draft tokens.", llamaGroupSpeculative, ptrutil.Ptr(0), nil),
		floatFlag("spec-draft-p-split", "", nil, 0.1, "Split probability.", llamaGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("spec-draft-p-min", "", nil, 0.0, "Minimum probability for the greedy path.", llamaGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		strFlag("spec-draft-device", "", nil, nil, "Draft devices; follows --device by default.", llamaGroupSpeculative, false),
		withKeywords(intFlag("spec-draft-ngl", "", nil, -1, "Draft layers in VRAM; accepts an exact integer, auto (-1), or all (-2).", llamaGroupSpeculative, ptrutil.Ptr(-2), ptrutil.Ptr(9999)), "auto", "all"),
		intFlag("spec-draft-threads", "", nil, nil, "Draft CPU threads; follows --threads by default.", llamaGroupSpeculative, ptrutil.Ptr(0), nil),
		boolFlag("spec-draft-backend-sampling", "", []string{"no-spec-draft-backend-sampling"}, true, "Offload draft sampling to the backend.", llamaGroupSpeculative),
		intFlag("spec-ngram-mod-n-min", "", nil, 48, "Minimum tokens for ngram-mod speculative decoding.", llamaGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-mod-n-max", "", nil, 64, "Maximum tokens for ngram-mod speculative decoding.", llamaGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-mod-n-match", "", nil, 24, "Lookup length for ngram-mod.", llamaGroupSpeculative, ptrutil.Ptr(0), nil),
		enumFlag("cache-type-k-draft", "ctkd", []string{"spec-draft-type-k"}, llamaCacheTypes(), "f16", "K cache data type for the draft model.", llamaGroupSpeculative),
		enumFlag("cache-type-v-draft", "ctvd", []string{"spec-draft-type-v"}, llamaCacheTypes(), "f16", "V cache data type for the draft model.", llamaGroupSpeculative),
		intFlag("spec-ngram-simple-size-n", "", nil, 12, "Lookup n-gram length for ngram-simple.", llamaGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-simple-size-m", "", nil, 48, "Draft m-gram length for ngram-simple.", llamaGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-simple-min-hits", "", nil, 1, "Minimum hits for ngram-simple.", llamaGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-map-k-size-n", "", nil, 12, "Lookup n-gram length for ngram-map-k.", llamaGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-map-k-size-m", "", nil, 48, "Draft m-gram length for ngram-map-k.", llamaGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-map-k-min-hits", "", nil, 1, "Minimum hits for ngram-map-k.", llamaGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-map-k4v-size-n", "", nil, 12, "Lookup n-gram length for ngram-map-k4v.", llamaGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-map-k4v-size-m", "", nil, 48, "Draft m-gram length for ngram-map-k4v.", llamaGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-ngram-map-k4v-min-hits", "", nil, 1, "Minimum hits for ngram-map-k4v.", llamaGroupSpeculative, ptrutil.Ptr(0), nil),
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

// boolFlagAuto describes a tri-state backend boolean: when unset the backend
// resolves the value itself, so no concrete default is advertised.
func boolFlagAuto(long, short string, aliases []string, help, group string) domain.FlagSpec {
	return domain.FlagSpec{Long: long, Short: short, Aliases: aliases, Type: domain.FlagTypeBool, HelpText: help, Group: group}
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

// withArity marks a curated flag whose value occupies n argv tokens, e.g.
// `--control-vector-layer-range START END`. Arity is per-binary, so it is
// declared at the call site of the fork that parses the flag that way rather
// than derived from the flag name (BUGS.md S15).
func withArity(spec domain.FlagSpec, n int) domain.FlagSpec {
	spec.Arity = n
	return spec
}

func enumFlag(long, short string, aliases []string, values []string, def any, help, group string) domain.FlagSpec {
	return domain.FlagSpec{Long: long, Short: short, Aliases: aliases, Type: domain.FlagTypeEnum, EnumValues: values, Default: def, HelpText: help, Group: group}
}

// listEnumFlag is like enumFlag but marks the value as a comma-separated list of
// enum values via FlagSpec.List. The validator splits the value on "," (after
// trimming each element) and checks each against values, so e.g.
// "draft-mtp,ngram-mod" and "draft-dflash" both validate.
func listEnumFlag(long, short string, aliases []string, values []string, def any, help, group string) domain.FlagSpec {
	s := enumFlag(long, short, aliases, values, def, help, group)
	s.List = true
	return s
}

func portFlag(long, short string, def int, help, group string) domain.FlagSpec {
	return domain.FlagSpec{Long: long, Short: short, Type: domain.FlagTypeInt, Default: def, HelpText: help, Group: group, Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(65535), IsPort: true}
}

// withKeywords marks a numeric flag as also accepting a fixed set of
// non-numeric literals via FlagSpec.Keywords, so the validator lets those
// strings through the type and range checks. llama.cpp's two "-ngl" flags
// parse "auto" as -1 and "all" as -2 but otherwise require an integer
// (common/arg.cpp), so neither a plain int nor a plain string models them.
func withKeywords(spec domain.FlagSpec, keywords ...string) domain.FlagSpec {
	spec.Keywords = keywords
	return spec
}

func llamaCacheTypes() []string {
	return []string{"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1"}
}

func llamaPresentation() *domain.Presentation {
	return &domain.Presentation{Groups: []domain.PresentationGroup{
		{Name: llamaGroupEssentials, Highlighted: true, Flags: []string{"hf-repo", "ctx-size", "host", "port", "n-gpu-layers", "device", "parallel", "threads", "batch-size", "ubatch-size", "cache-type-k", "cache-type-v", "cont-batching", "api-key", "alias"}},
		{Name: llamaGroupModelLoad, Flags: []string{"hf-repo", "hf-file", "hf-token", "model-url", "docker-repo", "hf-repo-v", "hf-file-v", "offline", "override-kv"}},
		{Name: llamaGroupContext, Flags: []string{"ctx-size", "n-predict", "batch-size", "ubatch-size", "keep"}},
		{Name: llamaGroupCPU, Flags: []string{"threads", "threads-batch", "poll", "prio", "numa", "cpu-mask", "cpu-range", "cpu-strict", "cpu-mask-batch", "cpu-range-batch", "cpu-strict-batch", "prio-batch", "poll-batch"}},
		{Name: llamaGroupMemory, Flags: []string{"mlock", "mmap", "direct-io", "load-mode", "repack", "op-offload", "no-host", "check-tensors"}},
		{Name: llamaGroupDevice, Flags: []string{"device", "list-devices", "n-gpu-layers", "flash-attn", "split-mode", "tensor-split", "main-gpu", "fit", "fit-target", "fit-ctx", "cpu-moe", "n-cpu-moe", "override-tensor"}},
		{Name: llamaGroupKV, Flags: []string{"kv-offload", "cache-type-k", "cache-type-v", "kv-unified", "kv-mean-center", "defrag-thold"}},
		{Name: llamaGroupRope, Flags: []string{"rope-scaling", "rope-scale", "rope-freq-base", "rope-freq-scale", "yarn-orig-ctx", "yarn-ext-factor", "yarn-attn-factor", "yarn-beta-slow", "yarn-beta-fast"}},
		{Name: llamaGroupShift, Flags: []string{"swa-full", "context-shift", "ctx-checkpoints", "checkpoint-min-step", "warmup", "spm-infill"}},
		{Name: llamaGroupSamplers, Flags: []string{"samplers", "sampler-seq", "seed", "temperature", "top-k", "top-p", "min-p", "typical-p", "top-n-sigma", "xtc-probability", "xtc-threshold", "ignore-eos", "backend-sampling", "logit-bias"}},
		{Name: llamaGroupPenalties, Flags: []string{"repeat-last-n", "repeat-penalty", "presence-penalty", "frequency-penalty", "dry-multiplier", "dry-base", "dry-allowed-length", "dry-penalty-last-n", "dry-sequence-breaker", "mirostat", "mirostat-lr", "mirostat-ent", "dynatemp-range", "dynatemp-exp", "adaptive-target", "adaptive-decay"}},
		{Name: llamaGroupGrammar, Flags: []string{"grammar", "grammar-file", "json-schema", "json-schema-file"}},
		{Name: llamaGroupChat, Flags: []string{"chat-template", "chat-template-file", "chat-template-kwargs", "jinja", "skip-chat-parsing", "prefill-assistant", "reasoning", "reasoning-format", "reasoning-budget", "reasoning-budget-message", "reasoning-preserve"}},
		{Name: llamaGroupHTTP, Flags: []string{"host", "port", "reuse-port", "api-prefix", "path", "cors-origins", "cors-methods", "cors-headers", "cors-credentials", "ui", "webui-config", "webui-config-file", "api-key", "api-key-file", "ssl-key-file", "ssl-cert-file", "mcp-servers-config", "mcp-servers-json", "timeout", "sse-ping-interval", "threads-http", "metrics", "perf", "slots", "props", "parallel", "cont-batching", "cache-prompt", "cache-reuse", "cache-ram", "cache-idle-slots", "tools", "slot-save-path", "media-path", "models-dir", "models-preset", "models-max", "models-autoload", "lookup-cache-static", "lookup-cache-dynamic", "slot-prompt-similarity", "sleep-idle-seconds", "alias", "tags"}},
		{Name: llamaGroupMultimodal, Flags: []string{"mmproj", "mmproj-url", "mmproj-auto", "mmproj-offload", "image-min-tokens", "image-max-tokens", "mtmd-batch-max-tokens", "model-vocoder", "tts-use-guide-tokens"}},
		{Name: llamaGroupEmbeddings, Flags: []string{"embeddings", "pooling", "embd-normalize", "reranking"}},
		{Name: llamaGroupLora, Flags: []string{"lora", "lora-scaled", "lora-init-without-apply", "control-vector", "control-vector-scaled", "control-vector-layer-range"}},
		{Name: llamaGroupSpeculative, Flags: []string{"spec-type", "spec-draft-model", "spec-draft-hf", "spec-draft-n-max", "spec-draft-n-min", "spec-draft-p-split", "spec-draft-p-min", "spec-draft-device", "spec-draft-ngl", "spec-draft-threads", "spec-draft-backend-sampling", "cache-type-k-draft", "cache-type-v-draft", "spec-ngram-mod-n-min", "spec-ngram-mod-n-max", "spec-ngram-mod-n-match", "spec-ngram-simple-size-n", "spec-ngram-simple-size-m", "spec-ngram-simple-min-hits", "spec-ngram-map-k-size-n", "spec-ngram-map-k-size-m", "spec-ngram-map-k-min-hits", "spec-ngram-map-k4v-size-n", "spec-ngram-map-k4v-size-m", "spec-ngram-map-k4v-min-hits"}},
	}}
}

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
	beeGroupRouter      = "18. Router Server"
)

// CuratedBeeLlamaSchema returns the hand-curated schema for the BeeLlama.cpp
// backend (Anbeeld's llama.cpp fork). BeeLlama exposes the standard llama-server
// binary with an upstream-format-identical --help, so the live generator parses
// --help and overlays this curated metadata. This schema is also the complete
// fallback used when the binary cannot be resolved.
//
// The fork-specific surface over upstream llama-server is: KVarN target-context
// KV-cache compression (kvarn2..kvarn8) with per-side and SWA-layer overrides,
// the KV-cache precision tail (KVCPT) controls, upstream draft-dflash speculative
// decoding, the profit-based adaptive draft-max controller, and a reasoning loop
// guard with its tuning surface. Flag metadata below was synchronized against the
// BeeLlama source tree at git v0.4.1-1-g94605e9fe (commit 94605e9fe, build 10856),
// reading common/arg.cpp directly as the source of truth. TurboQuant/TCQ cache
// types, DDTree tree verification, CopySpec, the fringe controller, and the fork
// DFlash ring were removed in v0.4.0.
//
// Flags that common/arg.cpp restricts via set_examples to non-server examples
// are intentionally excluded: llama-server does not register them and rejects
// them at launch ("error: invalid argument"). The notable case is
// --spec-draft-cpu-range-batch (LLAMA_EXAMPLE_SPECULATIVE only); its batch-mask
// and non-batch siblings remain because they include LLAMA_EXAMPLE_SERVER.
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
		withKeywords(intFlag("n-gpu-layers", "ngl", []string{"gpu-layers"}, -1, "Number of model layers offloaded to VRAM; accepts an exact integer, auto (-1), or all (-2).", beeGroupDevice, ptrutil.Ptr(-2), ptrutil.Ptr(9999)), "auto", "all"),
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
		enumFlag("cache-type-k", "ctk", nil, beellamaCacheTypes(), "f16", "K cache data type, including BeeLlama KVarN compression formats.", beeGroupKV),
		enumFlag("cache-type-v", "ctv", nil, beellamaCacheTypes(), "f16", "V cache data type, including BeeLlama KVarN compression formats.", beeGroupKV),
		enumFlag("flash-attn", "fa", nil, []string{"on", "off", "auto"}, "auto", "Flash Attention use: on, off, or auto.", beeGroupKV),
		// KVarN SWA-layer overrides and the KV-cache precision tail (KVCPT), BeeLlama v0.4.0.
		enumFlag("cache-type-k-swa", "", nil, []string{"kvarn2", "kvarn3", "kvarn4", "kvarn5", "kvarn6", "kvarn8"}, nil, "KVarN SWA-layer K cache override; requires a KVarN target cache and must be paired with --cache-type-v-swa.", beeGroupKV),
		enumFlag("cache-type-v-swa", "", nil, []string{"kvarn2", "kvarn3", "kvarn4", "kvarn5", "kvarn6", "kvarn8"}, nil, "KVarN SWA-layer V cache override; requires a KVarN target cache and must be paired with --cache-type-k-swa.", beeGroupKV),
		strFlag("kv-tail-tokens", "", nil, "0", "KV-cache precision tail (KVCPT): keep the newest attention-visible entries exact. Accepts 0, auto, N, a positional list, or a named group list; KVarN always retains an intrinsic 128-token exact suffix.", beeGroupKV, false),
		enumFlag("kv-tail-type", "", nil, []string{"f16", "bf16"}, nil, "KV-cache precision-tail storage type; default bf16 for standard caches, f16 for KVarN.", beeGroupKV),

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
		intFlag("checkpoint-min-step", "cms", nil, 8192, "Minimum spacing between context checkpoints in tokens; 0 = no minimum.", beeGroupShift, ptrutil.Ptr(0), nil),

		// 9. RAM cache, slots, unified KV
		intFlag("cache-ram", "cram", nil, 8192, "Max prompt-cache size in MiB; -1 = no limit, 0 = disable RAM snapshots.", beeGroupCacheRAM, ptrutil.Ptr(-1), nil),
		boolFlag("kv-unified", "kvu", []string{"no-kv-unified"}, false, "Use a single unified KV buffer shared across server slots; the server enables it automatically when the slot count is auto (--parallel -1).", beeGroupCacheRAM),

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
		intFlag("mirostat", "", nil, 0, "Mirostat mode; 0 disables it.", beeGroupPenalties, ptrutil.Ptr(0), ptrutil.Ptr(2)),
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
		intFlag("reasoning-loop-min-tokens", "", nil, 512, "Reasoning loop guard: minimum hidden reasoning tokens before loop checks begin.", beeGroupChat, ptrutil.Ptr(0), nil),
		intFlag("reasoning-loop-window", "", nil, 1024, "Reasoning loop guard: token tail window inspected for loop detection.", beeGroupChat, ptrutil.Ptr(1), nil),
		intFlag("reasoning-loop-max-period", "", nil, 128, "Reasoning loop guard: maximum periodic loop length to check.", beeGroupChat, ptrutil.Ptr(1), nil),
		intFlag("reasoning-loop-min-coverage", "", nil, 256, "Reasoning loop guard: minimum repeated-token coverage before the guard triggers.", beeGroupChat, ptrutil.Ptr(1), nil),
		intFlag("reasoning-loop-check-interval", "", nil, 64, "Reasoning loop guard: accepted-token interval between loop checks.", beeGroupChat, ptrutil.Ptr(1), nil),
		intFlag("reasoning-loop-interventions", "", nil, 2, "Reasoning loop guard: maximum force-close interventions before generation is stopped.", beeGroupChat, ptrutil.Ptr(0), nil),
		strFlag("reasoning-budget-message", "", nil, nil, "Message injected before the end-of-thinking tag when the reasoning budget is exhausted.", beeGroupChat, false),
		boolFlag("reasoning-preserve", "", []string{"no-reasoning-preserve"}, false, "Preserve the reasoning trace in the full history, not just the last assistant message (template-dependent).", beeGroupChat),
		boolFlag("skip-chat-parsing", "", []string{"no-skip-chat-parsing"}, false, "Force a pure content parser even with a Jinja template; model output stays in the content section.", beeGroupChat),
		boolFlag("prefill-assistant", "", []string{"no-prefill-assistant"}, true, "Prefill the assistant response when the last message is an assistant message; disable to treat it as a full message.", beeGroupChat),

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
		intFlag("parallel", "np", nil, -1, "Parallel server slots; -1 = automatic.", beeGroupHTTP, nil, nil),
		boolFlag("cont-batching", "cb", []string{"no-cont-batching"}, true, "Toggle continuous batching.", beeGroupHTTP),
		boolFlag("cache-prompt", "", []string{"no-cache-prompt"}, true, "Enable prompt reuse/cache.", beeGroupHTTP),
		strFlag("alias", "a", nil, nil, "Model name alias exposed by the API.", beeGroupHTTP, false),
		boolFlag("reuse-port", "", nil, false, "Allow multiple sockets to bind to the same port.", beeGroupHTTP),
		strFlag("api-prefix", "", nil, nil, "Path prefix the server serves from, without the trailing slash.", beeGroupHTTP, false),
		strFlag("ssl-key-file", "", nil, nil, "Path to a PEM-encoded SSL private key.", beeGroupHTTP, false),
		strFlag("ssl-cert-file", "", nil, nil, "Path to a PEM-encoded SSL certificate.", beeGroupHTTP, false),
		strFlag("cors-origins", "", nil, nil, "Comma-separated list of allowed CORS origins; 'localhost' reflects only localhost origins.", beeGroupHTTP, false),
		strFlag("cors-methods", "", nil, nil, "Comma-separated list of allowed CORS methods.", beeGroupHTTP, false),
		strFlag("cors-headers", "", nil, nil, "Comma-separated list of allowed CORS headers.", beeGroupHTTP, false),
		boolFlag("cors-credentials", "", []string{"no-cors-credentials"}, true, "Allow credentials for CORS requests.", beeGroupHTTP),
		intFlag("sse-ping-interval", "", nil, 30, "Server SSE ping interval in seconds; -1 disables it.", beeGroupHTTP, nil, nil),
		intFlag("cache-reuse", "", nil, 0, "Minimum chunk size to reuse from the cache via KV shifting; requires prompt caching.", beeGroupHTTP, ptrutil.Ptr(0), nil),
		boolFlag("props", "", nil, false, "Allow changing global properties via POST /props.", beeGroupHTTP),
		strFlag("slot-save-path", "", nil, nil, "Directory to save slot KV cache.", beeGroupHTTP, false),
		floatFlag("slot-prompt-similarity", "sps", nil, 0.10, "How closely a request prompt must match a slot's prompt to reuse it; 0 disables it.", beeGroupHTTP, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		strFlag("media-path", "", nil, nil, "Directory for loading local media files via file:// URLs.", beeGroupHTTP, false),
		intFlag("sleep-idle-seconds", "", nil, -1, "Seconds of idleness after which the server sleeps; -1 disables it.", beeGroupHTTP, nil, nil),
		boolFlag("embedding", "", []string{"embeddings"}, false, "Restrict the server to the embedding use case (dedicated embedding models only).", beeGroupHTTP),
		boolFlag("rerank", "", []string{"reranking"}, false, "Enable the reranking endpoint on the server.", beeGroupHTTP),
		strFlag("ui-config", "", []string{"webui-config"}, nil, "JSON providing default web UI settings (overrides UI defaults).", beeGroupHTTP, false),
		strFlag("ui-config-file", "", []string{"webui-config-file"}, nil, "JSON file providing default web UI settings.", beeGroupHTTP, false),
		boolFlag("ui-mcp-proxy", "", []string{"webui-mcp-proxy", "no-ui-mcp-proxy", "no-webui-mcp-proxy"}, false, "Enable the experimental MCP CORS proxy; do not enable in untrusted environments.", beeGroupHTTP),
		strFlag("tools", "", nil, nil, "Comma-separated built-in tools for AI agents ('all' enables every tool); do not enable in untrusted environments.", beeGroupHTTP, false),
		boolFlag("agent", "ag", []string{"no-agent"}, false, "Enable the CORS proxy and all built-in tools; do not enable in untrusted environments.", beeGroupHTTP),

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
		boolFlag("lora-init-without-apply", "", nil, false, "Load LoRA adapters without applying them (apply later via POST /lora-adapters).", beeGroupLora),

		// 17. DFlash and speculative decoding
		//
		// NOTE: BeeLlama's --help lists these draft flags with several long forms,
		// e.g. "--spec-draft-hf, -hfd, -hfrd, --hf-repo-draft". The live --help
		// parser canonicalizes such flags to their LAST long form (hf-repo-draft),
		// and mergeWithCurated keeps the parsed Long but adopts the curated Aliases.
		// We therefore include the documented "spec-draft-*" primary name in each
		// flag's Aliases so it remains resolvable by name (the form the quickstart
		// and profiles use) after the merge.
		listEnumFlag("spec-type", "", nil, []string{"none", "draft-simple", "draft-eagle3", "draft-mtp", "draft-dflash", "ngram-simple", "ngram-map-k", "ngram-map-k4v", "ngram-mod", "ngram-cache"}, "none", "Speculative decoding strategy (comma-separated list, e.g. draft-dflash,ngram-mod). Use 'draft-dflash' with a DFlash drafter or 'draft-mtp' for native MTP.", beeGroupSpeculative),
		strFlag("spec-draft-model", "md", []string{"model-draft", "spec-draft-model"}, nil, "Draft model for speculative decoding (DFlash drafter GGUF).", beeGroupSpeculative, false),
		strFlag("spec-draft-hf", "hfd", []string{"hf-repo-draft", "spec-draft-hf", "hfrd"}, nil, "HF repo for the draft model, <user>/<model>[:quant]; downloads on first run.", beeGroupSpeculative, false),
		withKeywords(intFlag("spec-draft-ngl", "ngld", []string{"n-gpu-layers-draft", "gpu-layers-draft", "spec-draft-ngl"}, -1, "Draft model layers offloaded to VRAM; accepts an exact integer, auto (-1), or all (-2).", beeGroupSpeculative, ptrutil.Ptr(-2), ptrutil.Ptr(9999)), "auto", "all"),
		intFlag("spec-draft-n-max", "", nil, 3, "Max tokens to draft per step; DFlash raises the effective omitted value to 16 by itself.", beeGroupSpeculative, ptrutil.Ptr(0), nil),
		intFlag("spec-draft-n-min", "", nil, 0, "Min draft tokens for speculative decoding.", beeGroupSpeculative, ptrutil.Ptr(0), nil),
		floatFlag("spec-draft-p-split", "", []string{"draft-p-split"}, 0.10, "Speculative decoding split probability.", beeGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		floatFlag("spec-draft-p-min", "", []string{"draft-p-min"}, 0.0, "Minimum speculative decoding probability.", beeGroupSpeculative, ptrutil.Ptr(0.0), ptrutil.Ptr(1.0)),
		strFlag("spec-draft-device", "devd", []string{"device-draft", "spec-draft-device"}, nil, "Devices for offloading the draft model; follows --device by default.", beeGroupSpeculative, false),
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
		intFlag("spec-draft-cpu-strict-batch", "", []string{"cpu-strict-batch-draft"}, nil, "Strict CPU placement for draft batch processing (0|1).", beeGroupSpeculative, nil, nil),
		intFlag("spec-draft-prio-batch", "", []string{"prio-batch-draft"}, 0, "Draft batch process/thread priority: 0=normal, 1=medium, 2=high, 3=realtime.", beeGroupSpeculative, ptrutil.Ptr(0), ptrutil.Ptr(3)),
		intFlag("spec-draft-poll-batch", "", []string{"poll-batch-draft"}, nil, "Use polling (0|1) while waiting for draft batch work.", beeGroupSpeculative, nil, nil),
		boolFlag("spec-draft-cpu-moe", "cmoed", []string{"cpu-moe-draft"}, false, "Keep all draft-model Mixture-of-Experts weights on the CPU.", beeGroupSpeculative),
		intFlag("spec-draft-n-cpu-moe", "ncmoed", []string{"spec-draft-ncmoe", "n-cpu-moe-draft"}, nil, "Keep the first N draft-model Mixture-of-Experts layers on the CPU.", beeGroupSpeculative, ptrutil.Ptr(0), nil),
		strFlag("spec-draft-override-tensor", "otd", []string{"override-tensor-draft"}, nil, "Override draft-model tensor buffer type, <pattern>=<buffer type> (comma-separated).", beeGroupSpeculative, false),
		boolFlag("spec-draft-backend-sampling", "", []string{"no-spec-draft-backend-sampling"}, true, "Offload draft-model sampling to the backend.", beeGroupSpeculative),
		boolFlag("spec-default", "", nil, false, "Enable the default speculative-decoding config.", beeGroupSpeculative),
		enumFlag("spec-dm-controller", "", nil, []string{"off", "profit"}, "profit", "Adaptive DFlash draft-max controller mode: off keeps the resolved maximum static, profit adapts depth from measured cycle profit.", beeGroupSpeculative),
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
		// 18. Router server
		strFlag("models-dir", "", nil, nil, "Directory containing models for the router server.", beeGroupRouter, false),
		strFlag("models-preset", "", nil, nil, "Path to an INI file of model presets for the router server.", beeGroupRouter, false),
		intFlag("models-max", "", nil, 4, "Router server: maximum models loaded simultaneously; 0 = unlimited.", beeGroupRouter, ptrutil.Ptr(0), nil),
		boolFlag("models-autoload", "", []string{"no-models-autoload"}, true, "Router server: automatically load models.", beeGroupRouter),
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
// fork's KVarN target-context compression types, in the order reported by the
// binary's --help (the kv_cache_types vector in common/arg.cpp).
func beellamaCacheTypes() []string {
	return []string{"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1", "q6_0", "q6_1", "q3_0", "q3_1", "q2_0", "q2_1", "kvarn2", "kvarn3", "kvarn4", "kvarn5", "kvarn6", "kvarn8"}
}

// beeLlamaRules are DFlash-aware cross-field validation rules. They are chosen
// to never false-positive on a valid DFlash profile: the single-flag rule
// engine cannot express "draft-model OR draft-hf", so we avoid a hard require
// on spec-type=draft-dflash itself (the drafter can also be auto-detected from
// draft GGUF metadata).
func beeLlamaRules() []domain.CrossFieldRule {
	return []domain.CrossFieldRule{}
}

// BeeLlamaPresentation lays out the BeeLlama schema for the web editor. The
// highlighted Essentials group leads with the DFlash workflow.
func BeeLlamaPresentation() *domain.Presentation {
	return &domain.Presentation{Groups: []domain.PresentationGroup{
		{Name: beeGroupEssentials, Highlighted: true, Flags: []string{"n-gpu-layers", "ctx-size", "host", "port", "batch-size", "ubatch-size", "flash-attn", "cache-type-k", "cache-type-v", "cache-type-k-swa", "cache-type-v-swa", "kv-tail-tokens", "kv-tail-type", "cache-ram", "kv-unified", "mmproj", "jinja", "reasoning", "spec-type", "spec-draft-hf", "spec-draft-model", "spec-draft-ngl"}},
		{Name: beeGroupModelLoad, Flags: []string{"hf-repo", "hf-file", "hf-token", "model-url", "offline", "override-kv"}},
		{Name: beeGroupContext, Flags: []string{"ctx-size", "n-predict", "batch-size", "ubatch-size", "keep"}},
		{Name: beeGroupCPU, Flags: []string{"threads", "threads-batch", "poll", "prio", "numa"}},
		{Name: beeGroupMemory, Flags: []string{"mlock", "mmap", "no-host", "check-tensors"}},
		{Name: beeGroupDevice, Flags: []string{"n-gpu-layers", "device", "list-devices", "split-mode", "tensor-split", "main-gpu", "override-tensor", "cpu-moe", "n-cpu-moe"}},
		{Name: beeGroupKV, Flags: []string{"kv-offload", "cache-type-k", "cache-type-v", "cache-type-k-swa", "cache-type-v-swa", "kv-tail-tokens", "kv-tail-type", "flash-attn"}},
		{Name: beeGroupRope, Flags: []string{"rope-scaling", "rope-scale", "rope-freq-base", "rope-freq-scale", "yarn-orig-ctx", "yarn-ext-factor", "yarn-attn-factor", "yarn-beta-slow", "yarn-beta-fast"}},
		{Name: beeGroupShift, Flags: []string{"swa-full", "context-shift", "ctx-checkpoints", "checkpoint-min-step"}},
		{Name: beeGroupCacheRAM, Flags: []string{"cache-ram", "kv-unified"}},
		{Name: beeGroupSamplers, Flags: []string{"seed", "temperature", "top-k", "top-p", "min-p", "typical-p", "top-n-sigma", "ignore-eos", "samplers"}},
		{Name: beeGroupPenalties, Flags: []string{"repeat-last-n", "repeat-penalty", "presence-penalty", "frequency-penalty", "dry-multiplier", "dry-base", "dry-allowed-length", "mirostat", "mirostat-lr", "mirostat-ent", "adaptive-target", "adaptive-decay"}},
		{Name: beeGroupGrammar, Flags: []string{"grammar", "grammar-file", "json-schema", "json-schema-file"}},
		{Name: beeGroupChat, Flags: []string{"chat-template", "chat-template-file", "chat-template-kwargs", "jinja", "reasoning-format", "reasoning", "reasoning-budget", "reasoning-budget-message", "reasoning-preserve", "skip-chat-parsing", "prefill-assistant", "reasoning-loop-guard", "reasoning-loop-min-tokens", "reasoning-loop-window", "reasoning-loop-max-period", "reasoning-loop-min-coverage", "reasoning-loop-check-interval", "reasoning-loop-interventions"}},
		{Name: beeGroupHTTP, Flags: []string{"host", "port", "api-key", "api-key-file", "path", "ui", "timeout", "threads-http", "metrics", "slots", "parallel", "cont-batching", "cache-prompt", "alias", "reuse-port", "api-prefix", "ssl-key-file", "ssl-cert-file", "cors-origins", "cors-methods", "cors-headers", "cors-credentials", "sse-ping-interval", "cache-reuse", "props", "slot-save-path", "slot-prompt-similarity", "media-path", "sleep-idle-seconds", "embedding", "rerank", "ui-config", "ui-config-file", "ui-mcp-proxy", "tools", "agent"}},
		{Name: beeGroupMultimodal, Flags: []string{"mmproj", "mmproj-url", "mmproj-auto", "mmproj-offload"}},
		{Name: beeGroupLora, Flags: []string{"lora", "lora-scaled", "control-vector", "control-vector-scaled", "lora-init-without-apply"}},
		{Name: beeGroupSpeculative, Flags: []string{"spec-type", "spec-draft-model", "spec-draft-hf", "spec-draft-ngl", "spec-draft-n-max", "spec-draft-n-min", "spec-draft-p-split", "spec-draft-p-min", "spec-draft-device", "spec-draft-type-k", "spec-draft-type-v", "spec-draft-threads", "spec-draft-threads-batch", "spec-draft-cpu-mask", "spec-draft-cpu-range", "spec-draft-cpu-strict", "spec-draft-prio", "spec-draft-poll", "spec-draft-cpu-mask-batch", "spec-draft-cpu-strict-batch", "spec-draft-prio-batch", "spec-draft-poll-batch", "spec-draft-cpu-moe", "spec-draft-n-cpu-moe", "spec-draft-override-tensor", "spec-draft-backend-sampling", "spec-default", "spec-dm-controller", "spec-dm-profit-min", "spec-dm-profit-raise-margin", "spec-dm-profit-lower-margin", "spec-dm-profit-ewma-alpha", "spec-dm-profit-min-samples", "spec-dm-profit-warmup", "spec-dm-profit-baseline-interval", "spec-ngram-mod-n-min", "spec-ngram-mod-n-max", "spec-ngram-mod-n-match", "spec-ngram-simple-size-n", "spec-ngram-simple-size-m", "spec-ngram-simple-min-hits", "spec-ngram-map-k-size-n", "spec-ngram-map-k-size-m", "spec-ngram-map-k-min-hits", "spec-ngram-map-k4v-size-n", "spec-ngram-map-k4v-size-m", "spec-ngram-map-k4v-min-hits"}},
		{Name: beeGroupRouter, Flags: []string{"models-dir", "models-preset", "models-max", "models-autoload"}},
	}}
}

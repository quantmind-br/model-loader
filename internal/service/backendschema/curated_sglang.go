package backendschema

import "github.com/quantmind-br/model-loader/internal/domain"

const (
	sglangGroupEssentials     = "Essenciais"
	sglangGroupServer         = "Servidor e geral"
	sglangGroupModel          = "Modelo e tokenizer"
	sglangGroupLoad           = "Carregamento de pesos"
	sglangGroupMemory         = "Memória e KV cache"
	sglangGroupPrefixCache    = "Cache de prefixo"
	sglangGroupPrefill        = "Prefill, chunking e scheduler"
	sglangGroupSampling       = "Streaming, sampling e gramática"
	sglangGroupParallel       = "Paralelismo e execução distribuída"
	sglangGroupMoE            = "MoE e expert parallelism"
	sglangGroupKernels        = "Backends de atenção e GEMM"
	sglangGroupSparseMamba    = "Atenção esparsa e Mamba"
	sglangGroupLoRA           = "LoRA"
	sglangGroupSpeculative    = "Speculative decoding"
	sglangGroupCompile        = "CUDA graph e torch.compile"
	sglangGroupCollectives    = "Comunicação e all-reduce"
	sglangGroupOffload        = "Offload de pesos e memory saver"
	sglangGroupMultimodal     = "Multimodal e encoder"
	sglangGroupOpenAI         = "API OpenAI e parsers"
	sglangGroupObservability  = "Logging, métricas e tracing"
	sglangGroupDisaggregation = "PD disaggregation"
	sglangGroupDebug          = "Debug e determinismo"
)

// CuratedSGLangSchema returns the hand-curated sglang serve schema based on
// docs/model-loader/sglang.md. The profile model field is handled separately,
// so --model-path and --model are intentionally omitted.
func CuratedSGLangSchema() domain.BackendValidationSchema {
	flags := map[string]domain.FlagSpec{}
	add := func(spec domain.FlagSpec) {
		flags[spec.Long] = spec
	}

	for _, spec := range []domain.FlagSpec{
		strFlag("config", "", nil, nil, "Lê opções de CLI a partir de um arquivo YAML.", sglangGroupServer, false),
		strFlag("host", "", nil, nil, "Endereço do servidor HTTP.", sglangGroupServer, false),
		intFlag("port", "", nil, nil, "Porta do servidor HTTP.", sglangGroupServer, intPtr(1), intPtr(65535)),
		strFlag("fastapi-root-path", "", nil, nil, "root_path do FastAPI atrás de proxy de rota.", sglangGroupServer, false),
		boolFlag("grpc-mode", "", nil, false, "Usa servidor gRPC em vez de HTTP.", sglangGroupServer),
		boolFlag("skip-server-warmup", "", nil, false, "Pula o warmup do servidor.", sglangGroupServer),
		strFlag("warmups", "", nil, nil, "Funções de warmup customizadas de warmup.py, em CSV.", sglangGroupServer, false),
		intFlag("nccl-port", "", nil, nil, "Porta para setup distribuído NCCL; nil usa porta aleatória.", sglangGroupServer, intPtr(1), intPtr(65535)),
		boolFlag("checkpoint-engine-wait-weights-before-ready", "", nil, false, "Espera pesos iniciais antes de servir.", sglangGroupServer),
		intFlag("watchdog-timeout", "", nil, nil, "Crash se um forward batch passar do tempo.", sglangGroupServer, intPtr(0), nil),
		intFlag("soft-watchdog-timeout", "", nil, nil, "Dump de debug se um forward batch passar do tempo, sem crash.", sglangGroupServer, intPtr(0), nil),
		intFlag("dist-timeout", "", nil, nil, "Timeout do torch.distributed.", sglangGroupServer, intPtr(0), nil),
		strFlag("custom-sigquit-handler", "", nil, nil, "Handler customizado de SIGQUIT para Engine.", sglangGroupServer, false),
		boolFlag("sleep-on-idle", "", nil, false, "Reduz uso de CPU quando ocioso.", sglangGroupServer),
		intFlag("base-gpu-id", "", nil, 0, "GPU inicial para alocação.", sglangGroupServer, intPtr(0), nil),
		intFlag("gpu-id-step", "", nil, 1, "Passo entre IDs de GPU.", sglangGroupServer, intPtr(1), nil),
		strFlag("numa-node", "", nil, nil, "NUMA node de cada subprocesso.", sglangGroupServer, false),
		intFlag("scheduler-recv-interval", "", nil, nil, "Intervalo de polling de requisições no scheduler.", sglangGroupServer, intPtr(0), nil),

		strFlag("tokenizer-path", "", nil, nil, "Caminho do tokenizer; default acompanha o modelo.", sglangGroupModel, false),
		enumFlag("tokenizer-mode", "", nil, []string{"auto", "slow"}, "auto", "Modo do tokenizer.", sglangGroupModel),
		intFlag("tokenizer-worker-num", "", nil, nil, "Workers do tokenizer manager.", sglangGroupModel, intPtr(1), nil),
		boolFlag("skip-tokenizer-init", "", nil, false, "Pula init do tokenizer e espera input_ids.", sglangGroupModel),
		boolFlag("trust-remote-code", "", nil, false, "Permite código customizado do Hub.", sglangGroupModel),
		intFlag("context-length", "", nil, nil, "Contexto máximo; nil usa config.json do modelo.", sglangGroupModel, intPtr(1), nil),
		boolFlag("is-embedding", "", nil, false, "Usa CausalLM como modelo de embedding.", sglangGroupModel),
		boolFlag("enable-multimodal", "", nil, false, "Habilita multimodalidade no modelo servido.", sglangGroupModel),
		strFlag("revision", "", nil, nil, "Branch, tag ou commit do modelo no Hugging Face.", sglangGroupModel, false),
		enumFlag("model-impl", "", nil, []string{"auto", "sglang", "transformers", "mindspore"}, "auto", "Implementação do modelo.", sglangGroupModel),
		strFlag("served-model-name", "", nil, nil, "Nome retornado por /v1/models; default acompanha o modelo.", sglangGroupModel, false),
		strFlag("weight-version", "", nil, "default", "Identificador de versão dos pesos.", sglangGroupModel, false),
		strFlag("json-model-override-args", "", nil, nil, "JSON para sobrescrever configs do modelo.", sglangGroupModel, false),
		strFlag("model-checksum", "", nil, nil, "Verificação de integridade dos arquivos do modelo.", sglangGroupModel, false),

		enumFlag("load-format", "", nil, []string{"auto", "pt", "safetensors", "npcache", "dummy", "sharded_state", "gguf", "bitsandbytes", "layered", "flash_rl", "remote", "remote_instance", "fastsafetensors", "private"}, "auto", "Formato dos pesos.", sglangGroupLoad),
		strFlag("model-loader-extra-config", "", nil, nil, "Config extra do loader, em JSON.", sglangGroupLoad, false),
		strFlag("download-dir", "", nil, nil, "Diretório de download/cache do Hugging Face.", sglangGroupLoad, false),
		strFlag("custom-weight-loader", "", nil, nil, "Loader de pesos customizado.", sglangGroupLoad, false),
		boolFlag("ignore-config-weight-sync-check", "", nil, false, "Ignora checagem de sincronização entre config e pesos.", sglangGroupLoad),
		enumFlag("dtype", "", nil, []string{"auto", "half", "float16", "bfloat16", "float", "float32"}, "auto", "Dtype do modelo.", sglangGroupLoad),
		enumFlag("quantization", "", nil, []string{"awq", "fp8", "gptq", "mxfp4", "modelopt", "marlin", "gguf"}, nil, "Quantização dos pesos.", sglangGroupLoad),
		enumFlag("kv-cache-dtype", "", nil, []string{"auto", "fp8_e5m2", "fp8_e4m3", "bf16", "fp4_e2m1"}, "auto", "Dtype do KV cache.", sglangGroupLoad),
		boolFlag("enable-fp32-lm-head", "", nil, false, "Calcula logits do LM head em FP32.", sglangGroupLoad),
		strFlag("quantization-param-path", "", nil, nil, "Caminho de parâmetros/metadados de quantização.", sglangGroupLoad, false),
		strFlag("ptao-config", "", nil, nil, "Config PTAO para quantização/otimização.", sglangGroupLoad, false),
		strFlag("torchao-config", "", nil, nil, "Config TorchAO para quantização.", sglangGroupLoad, false),
		strFlag("cpuinfer-config", "", nil, nil, "Config de CPUInfer.", sglangGroupLoad, false),

		floatFlag("mem-fraction-static", "", nil, nil, "Fração da memória GPU usada para alocação estática.", sglangGroupMemory, floatPtr(0), floatPtr(1)),
		intFlag("max-running-requests", "", nil, nil, "Máximo de requisições em execução.", sglangGroupMemory, intPtr(1), nil),
		intFlag("max-queued-requests", "", nil, nil, "Máximo de requisições na fila.", sglangGroupMemory, intPtr(0), nil),
		intFlag("max-total-tokens", "", nil, nil, "Máximo de tokens no pool de memória.", sglangGroupMemory, intPtr(1), nil),
		intFlag("page-size", "", nil, nil, "Tokens por página do KV cache.", sglangGroupMemory, intPtr(1), nil),
		floatFlag("swa-full-tokens-ratio", "", nil, nil, "Razão entre SWA e atenção full.", sglangGroupMemory, floatPtr(0), floatPtr(1)),

		boolFlag("disable-radix-cache", "", nil, false, "Desliga RadixAttention/prefix cache.", sglangGroupPrefixCache),
		enumFlag("radix-eviction-policy", "", nil, []string{"lru", "lfu"}, "lru", "Política de evicção do radix cache.", sglangGroupPrefixCache),
		boolFlag("enable-hierarchical-cache", "", nil, false, "Habilita HiCache.", sglangGroupPrefixCache),
		enumFlag("hicache-write-policy", "", nil, []string{"write_back", "write_through"}, nil, "Política de escrita do HiCache.", sglangGroupPrefixCache),
		strFlag("hicache-mem-layout", "", nil, nil, "Layout do cache hierárquico em memória.", sglangGroupPrefixCache, false),
		strFlag("hicache-storage-backend", "", nil, nil, "Backend de armazenamento do HiCache.", sglangGroupPrefixCache, false),
		strFlag("hicache-storage-prefetch-policy", "", nil, nil, "Política de prefetch do armazenamento HiCache.", sglangGroupPrefixCache, false),
		strFlag("hicache-storage-backend-extra-config", "", nil, nil, "Config extra do backend de storage HiCache.", sglangGroupPrefixCache, false),
		boolFlag("enable-lmcache", "", nil, false, "Habilita LMCache como cache hierárquico alternativo.", sglangGroupPrefixCache),

		intFlag("chunked-prefill-size", "", nil, nil, "Tokens por chunk no chunked prefill; -1 desativa.", sglangGroupPrefill, nil, nil),
		intFlag("prefill-max-requests", "", nil, nil, "Máximo de requisições num batch de prefill.", sglangGroupPrefill, intPtr(1), nil),
		boolFlag("enable-dynamic-chunking", "", nil, false, "Ajusta dinamicamente o tamanho de chunk.", sglangGroupPrefill),
		intFlag("max-prefill-tokens", "", nil, nil, "Máximo de tokens num batch de prefill.", sglangGroupPrefill, intPtr(1), nil),
		boolFlag("enable-mixed-chunk", "", nil, false, "Mistura prefill e decode no mesmo batch.", sglangGroupPrefill),
		boolFlag("disable-chunked-prefix-cache", "", nil, false, "Desliga chunked prefix cache.", sglangGroupPrefill),
		enumFlag("schedule-policy", "", nil, []string{"lpm", "random", "fcfs", "dfs-weight", "lof", "priority", "routing-key"}, nil, "Política de escalonamento.", sglangGroupPrefill),
		boolFlag("enable-priority-scheduling", "", nil, false, "Habilita escalonamento por prioridade.", sglangGroupPrefill),
		boolFlag("abort-on-priority-when-disabled", "", nil, false, "Aborta requisições com prioridade quando scheduling por prioridade está off.", sglangGroupPrefill),
		boolFlag("schedule-low-priority-values-first", "", nil, false, "Prioridade menor é agendada primeiro.", sglangGroupPrefill),
		intFlag("priority-scheduling-preemption-threshold", "", nil, nil, "Diferença mínima de prioridade para preempção.", sglangGroupPrefill, nil, nil),
		floatFlag("schedule-conservativeness", "", nil, nil, "Quão conservador é o scheduler.", sglangGroupPrefill, nil, nil),
		intFlag("num-continuous-decode-steps", "", nil, 1, "Passos de decode contínuos por agendamento.", sglangGroupPrefill, intPtr(1), nil),
		boolFlag("disable-overlap-schedule", "", nil, false, "Desliga overlap scheduler.", sglangGroupPrefill),
		boolFlag("enable-prefill-delayer", "", nil, false, "Atrasa prefill em DP attention para reduzir ociosidade.", sglangGroupPrefill),
		intFlag("prefill-delayer-max-delay-passes", "", nil, nil, "Máximo de forward passes para atrasar prefill.", sglangGroupPrefill, intPtr(0), nil),
		floatFlag("prefill-delayer-token-usage-low-watermark", "", nil, nil, "Watermark de uso de tokens do prefill delayer.", sglangGroupPrefill, floatPtr(0), floatPtr(1)),

		intFlag("stream-interval", "", nil, nil, "Buffer/intervalo de streaming em tokens.", sglangGroupSampling, intPtr(1), nil),
		intFlag("random-seed", "", nil, nil, "Seed do RNG.", sglangGroupSampling, nil, nil),
		enumFlag("sampling-defaults", "", nil, []string{"openai", "model"}, "model", "Origem dos defaults de sampling.", sglangGroupSampling),
		enumFlag("grammar-backend", "", nil, []string{"xgrammar", "outlines", "llguidance", "none"}, nil, "Backend de gramática.", sglangGroupSampling),
		strFlag("constrained-json-whitespace-pattern", "", nil, nil, "Padrão de whitespace em JSON constrito.", sglangGroupSampling, false),
		boolFlag("constrained-json-disable-any-whitespace", "", nil, false, "Desativa qualquer whitespace em JSON constrito.", sglangGroupSampling),

		enumFlag("device", "", nil, []string{"cuda", "xpu", "hpu", "npu", "cpu"}, nil, "Dispositivo de execução.", sglangGroupParallel),
		intFlag("tensor-parallel-size", "", []string{"tp-size"}, 1, "Tamanho do tensor parallelism.", sglangGroupParallel, intPtr(1), nil),
		intFlag("pipeline-parallel-size", "", []string{"pp-size"}, 1, "Tamanho do pipeline parallelism.", sglangGroupParallel, intPtr(1), nil),
		intFlag("data-parallel-size", "", []string{"dp-size"}, 1, "Tamanho do data parallelism.", sglangGroupParallel, intPtr(1), nil),
		intFlag("attention-context-parallel-size", "", []string{"attn-cp-size"}, 1, "Context parallelism na atenção.", sglangGroupParallel, intPtr(1), nil),
		intFlag("moe-data-parallel-size", "", []string{"moe-dp-size"}, nil, "Data parallelism nas camadas MoE.", sglangGroupParallel, intPtr(1), nil),
		intFlag("pp-max-micro-batch-size", "", nil, nil, "Máximo micro-batch no pipeline parallelism.", sglangGroupParallel, intPtr(1), nil),
		intFlag("pp-async-batch-depth", "", nil, nil, "Profundidade de batch assíncrono do pipeline.", sglangGroupParallel, intPtr(1), nil),
		enumFlag("load-balance-method", "", nil, []string{"auto", "round_robin", "follow_bootstrap_room", "total_requests", "total_tokens"}, nil, "Estratégia de load balancing do data parallelism.", sglangGroupParallel),
		strFlag("dist-init-addr", "", []string{"nccl-init-addr"}, nil, "Endereço host:porta para init distribuído.", sglangGroupParallel, false),
		intFlag("nnodes", "", nil, 1, "Número de nós.", sglangGroupParallel, intPtr(1), nil),
		intFlag("node-rank", "", nil, 0, "Rank do nó.", sglangGroupParallel, intPtr(0), nil),
		boolFlag("enable-dp-attention", "", nil, false, "Usa DP na atenção e TP no FFN.", sglangGroupParallel),
		boolFlag("enable-dp-lm-head", "", nil, false, "Vocab parallel no grupo TP da atenção.", sglangGroupParallel),
		boolFlag("enable-attn-tp-input-scattered", "", nil, false, "Espalha input da atenção em TP.", sglangGroupParallel),
		intFlag("moe-dense-tp-size", "", nil, nil, "TP das camadas MLP densas de MoE.", sglangGroupParallel, intPtr(1), nil),

		intFlag("expert-parallel-size", "", []string{"ep-size", "ep"}, 1, "Tamanho do expert parallelism.", sglangGroupMoE, intPtr(1), nil),
		enumFlag("moe-a2a-backend", "", nil, []string{"none", "deepep", "mooncake", "mori", "ascend_fuseep", "flashinfer"}, "none", "Backend de comunicação A2A do MoE.", sglangGroupMoE),
		enumFlag("moe-runner-backend", "", nil, []string{"auto", "deep_gemm", "triton", "triton_kernel", "flashinfer_*", "cutlass"}, "auto", "Backend de cálculo dos experts.", sglangGroupMoE),
		enumFlag("flashinfer-mxfp4-moe-precision", "", nil, []string{"default", "bf16"}, nil, "Precisão do MoE MXFP4 do FlashInfer.", sglangGroupMoE),
		boolFlag("enable-flashinfer-allreduce-fusion", "", nil, false, "Fusão allreduce + Residual RMSNorm.", sglangGroupMoE),
		enumFlag("deepep-mode", "", nil, []string{"normal", "low_latency", "auto"}, "auto", "Modo do DeepEP MoE.", sglangGroupMoE),
		intFlag("ep-num-redundant-experts", "", nil, nil, "Experts redundantes no EP.", sglangGroupMoE, intPtr(0), nil),
		strFlag("ep-dispatch-algorithm", "", nil, nil, "Algoritmo de escolha de ranks para experts redundantes.", sglangGroupMoE, false),
		strFlag("init-expert-location", "", nil, nil, "Localização inicial dos experts EP.", sglangGroupMoE, false),
		boolFlag("enable-eplb", "", nil, false, "Habilita EPLB para balanceamento de experts.", sglangGroupMoE),
		strFlag("eplb-algorithm", "", nil, nil, "Algoritmo EPLB.", sglangGroupMoE, false),
		intFlag("eplb-rebalance-num-iterations", "", nil, nil, "Iterações para disparar rebalance automático.", sglangGroupMoE, intPtr(1), nil),
		intFlag("eplb-rebalance-layers-per-chunk", "", nil, nil, "Camadas rebalanceadas por forward pass.", sglangGroupMoE, intPtr(1), nil),
		floatFlag("eplb-min-rebalancing-utilization-threshold", "", nil, nil, "Utilização mínima da GPU para rebalancear.", sglangGroupMoE, floatPtr(0), floatPtr(1)),
		strFlag("expert-distribution-recorder-mode", "", nil, nil, "Modo do gravador de distribuição de experts.", sglangGroupMoE, false),
		intFlag("expert-distribution-recorder-buffer-size", "", nil, nil, "Buffer circular do gravador; -1 representa infinito.", sglangGroupMoE, nil, nil),
		boolFlag("enable-expert-distribution-metrics", "", nil, false, "Métricas de balanceamento de experts.", sglangGroupMoE),
		strFlag("deepep-config", "", nil, nil, "Config DeepEP em JSON ou arquivo.", sglangGroupMoE, false),
		enumFlag("elastic-ep-backend", "", nil, []string{"none", "mooncake"}, nil, "Backend de comunicação para EP elástico.", sglangGroupMoE),
		strFlag("mooncake-ib-device", "", nil, nil, "Dispositivos InfiniBand do Mooncake.", sglangGroupMoE, false),
		boolFlag("disable-shared-experts-fusion", "", nil, false, "Desliga fusão de shared experts.", sglangGroupMoE),

		enumFlag("attention-backend", "", nil, []string{"triton", "fa3", "fa4", "flashinfer", "flashmla", "trtllm_*"}, nil, "Backend de kernels de atenção.", sglangGroupKernels),
		enumFlag("prefill-attention-backend", "", nil, []string{"triton", "fa3", "fa4", "flashinfer", "flashmla", "trtllm_*"}, nil, "Override do backend de atenção para prefill.", sglangGroupKernels),
		enumFlag("decode-attention-backend", "", nil, []string{"triton", "fa3", "fa4", "flashinfer", "flashmla", "trtllm_*"}, nil, "Override do backend de atenção para decode.", sglangGroupKernels),
		enumFlag("sampling-backend", "", nil, []string{"pytorch", "flashinfer", "ascend"}, nil, "Backend de kernels de sampling.", sglangGroupKernels),
		enumFlag("mm-attention-backend", "", nil, []string{"triton", "fa3", "fa4", "flashinfer", "flashmla", "trtllm_*"}, nil, "Backend de atenção multimodal.", sglangGroupKernels),
		enumFlag("fp8-gemm-backend", "", nil, []string{"auto", "deep_gemm", "cutlass", "triton"}, "auto", "Backend de GEMM FP8.", sglangGroupKernels),
		enumFlag("fp4-gemm-backend", "", nil, []string{"auto", "flashinfer_cudnn", "flashinfer_cutlass", "flashinfer_trtllm"}, "flashinfer_cutlass", "Backend de GEMM NVFP4.", sglangGroupKernels),
		boolFlag("disable-flashinfer-autotune", "", nil, false, "Desliga autotune do FlashInfer.", sglangGroupKernels),
		intFlag("triton-attention-num-kv-splits", "", nil, 8, "Número de KV splits no backend Triton.", sglangGroupKernels, intPtr(1), nil),

		enumFlag("nsa-prefill-backend", "", nil, []string{"flashmla_*", "fa3", "tilelang", "aiter", "trtllm"}, nil, "Backend NSA para prefill.", sglangGroupSparseMamba),
		enumFlag("nsa-decode-backend", "", nil, []string{"flashmla_*", "fa3", "tilelang", "aiter", "trtllm"}, nil, "Backend NSA para decode.", sglangGroupSparseMamba),
		boolFlag("enable-double-sparsity", "", nil, false, "Habilita double sparsity.", sglangGroupSparseMamba),
		strFlag("ds-channel-config-path", "", nil, nil, "Caminho de config de canais da double sparsity.", sglangGroupSparseMamba, false),
		intFlag("ds-heavy-channel-num", "", nil, nil, "Número de heavy channels na double sparsity.", sglangGroupSparseMamba, intPtr(0), nil),
		floatFlag("ds-heavy-channel-threshold", "", nil, nil, "Threshold de heavy channels na double sparsity.", sglangGroupSparseMamba, nil, nil),
		intFlag("ds-heavy-token-num", "", nil, nil, "Número de heavy tokens na double sparsity.", sglangGroupSparseMamba, intPtr(0), nil),
		intFlag("ds-sparse-decode-threshold", "", nil, nil, "Threshold para decode esparso.", sglangGroupSparseMamba, intPtr(0), nil),
		enumFlag("mamba-backend", "", nil, []string{"triton", "flashinfer"}, "triton", "Backend do kernel Mamba SSM.", sglangGroupSparseMamba),
		enumFlag("mamba-ssm-dtype", "", nil, []string{"float32", "bfloat16", "float16"}, nil, "Dtype dos estados SSM do Mamba.", sglangGroupSparseMamba),

		boolFlag("enable-lora", "", nil, false, "Habilita LoRA.", sglangGroupLoRA),
		strFlag("lora-paths", "", nil, nil, "Lista de adapters LoRA.", sglangGroupLoRA, false),
		intFlag("max-lora-rank", "", nil, nil, "Rank máximo de LoRA.", sglangGroupLoRA, intPtr(1), nil),
		strFlag("lora-target-modules", "", nil, nil, "Módulos alvo de LoRA ou all.", sglangGroupLoRA, false),
		intFlag("max-loras-per-batch", "", nil, nil, "Número de adapters LoRA por batch.", sglangGroupLoRA, intPtr(1), nil),
		enumFlag("lora-eviction-policy", "", nil, []string{"lru", "fifo"}, "lru", "Política de evicção de LoRA.", sglangGroupLoRA),
		enumFlag("lora-backend", "", nil, []string{"triton", "csgmv", "ascend", "torch_native"}, nil, "Backend de kernel multi-LoRA.", sglangGroupLoRA),

		enumFlag("speculative-algorithm", "", nil, []string{"EAGLE", "EAGLE3", "NEXTN", "STANDALONE", "NGRAM"}, nil, "Algoritmo de speculative decoding.", sglangGroupSpeculative),
		strFlag("speculative-draft-model-path", "", nil, nil, "Modelo draft para speculative decoding.", sglangGroupSpeculative, false),
		intFlag("speculative-num-steps", "", nil, nil, "Passos do modelo draft.", sglangGroupSpeculative, intPtr(1), nil),
		intFlag("speculative-eagle-topk", "", nil, nil, "Top-k do draft para EAGLE.", sglangGroupSpeculative, intPtr(1), nil),
		intFlag("speculative-num-draft-tokens", "", nil, nil, "Número de tokens draft.", sglangGroupSpeculative, intPtr(1), nil),
		enumFlag("speculative-attention-mode", "", nil, []string{"prefill", "decode"}, "prefill", "Modo de atenção do speculative decoding.", sglangGroupSpeculative),

		boolFlag("disable-cuda-graph", "", nil, false, "Desliga CUDA graph.", sglangGroupCompile),
		intFlag("cuda-graph-max-bs", "", nil, nil, "Batch size máximo do CUDA graph.", sglangGroupCompile, intPtr(1), nil),
		boolFlag("enable-cudagraph-gc", "", nil, false, "Habilita GC durante captura de CUDA graph.", sglangGroupCompile),
		boolFlag("enable-torch-compile", "", nil, false, "Habilita torch.compile.", sglangGroupCompile),
		intFlag("torch-compile-max-bs", "", nil, nil, "Batch size máximo para torch.compile.", sglangGroupCompile, intPtr(1), nil),
		boolFlag("enable-torch-compile-debug-mode", "", nil, false, "Modo debug do torch.compile.", sglangGroupCompile),
		boolFlag("enable-piecewise-cuda-graph", "", nil, false, "Captura CUDA graph por pedaços.", sglangGroupCompile),
		enumFlag("piecewise-cuda-graph-compiler", "", nil, []string{"eager", "inductor"}, nil, "Compilador do piecewise CUDA graph.", sglangGroupCompile),
		intFlag("piecewise-cuda-graph-max-bs", "", nil, nil, "Batch size máximo do piecewise CUDA graph.", sglangGroupCompile, intPtr(1), nil),
		intFlag("piecewise-cuda-graph-max-tokens", "", nil, nil, "Tokens máximos do piecewise CUDA graph.", sglangGroupCompile, intPtr(1), nil),
		strFlag("piecewise-cuda-graph-capture-forward-mode", "", nil, nil, "Modo de forward capturado no piecewise CUDA graph.", sglangGroupCompile, false),

		boolFlag("disable-custom-all-reduce", "", nil, false, "Desliga all-reduce custom e usa NCCL.", sglangGroupCollectives),
		boolFlag("enable-mscclpp", "", nil, false, "Usa mscclpp para mensagens pequenas no all-reduce.", sglangGroupCollectives),
		boolFlag("enable-torch-symm-mem", "", nil, false, "Usa torch symmetric memory no all-reduce.", sglangGroupCollectives),
		boolFlag("enable-symm-mem", "", nil, false, "Usa NCCL symmetric memory.", sglangGroupCollectives),
		boolFlag("enable-nccl-nvls", "", nil, false, "Usa NCCL NVLS para prefills pesados.", sglangGroupCollectives),
		boolFlag("enable-p2p-check", "", nil, false, "Checa acesso P2P entre GPUs.", sglangGroupCollectives),
		boolFlag("disable-flashinfer-cutlass-moe-fp4-allgather", "", nil, false, "Não quantiza antes do all-gather no MoE FP4 FlashInfer/CUTLASS.", sglangGroupCollectives),

		floatFlag("cpu-offload-gb", "", nil, float64(0), "GB de RAM reservados para offload de pesos para CPU.", sglangGroupOffload, floatPtr(0), nil),
		intFlag("offload-group-size", "", nil, nil, "Camadas por grupo no offload.", sglangGroupOffload, intPtr(1), nil),
		intFlag("offload-num-in-group", "", nil, nil, "Camadas offloadadas por grupo.", sglangGroupOffload, intPtr(0), nil),
		intFlag("offload-prefetch-step", "", nil, nil, "Passos de prefetch no offload.", sglangGroupOffload, intPtr(0), nil),
		strFlag("offload-mode", "", nil, nil, "Modo de offload.", sglangGroupOffload, false),
		boolFlag("enable-memory-saver", "", nil, false, "Permite liberar e retomar ocupação de memória.", sglangGroupOffload),
		boolFlag("enable-weights-cpu-backup", "", nil, false, "Backup dos pesos em RAM.", sglangGroupOffload),
		boolFlag("enable-draft-weights-cpu-backup", "", nil, false, "Backup dos pesos do draft em RAM.", sglangGroupOffload),
		strFlag("kt-weight-path", "", nil, nil, "Caminho de pesos ktransformers para experts quantizados.", sglangGroupOffload, false),
		strFlag("kt-method", "", nil, nil, "Método/formato de quantização ktransformers para CPU.", sglangGroupOffload, false),
		intFlag("kt-cpuinfer", "", nil, nil, "Threads de CPUInfer para ktransformers.", sglangGroupOffload, intPtr(1), nil),
		intFlag("kt-threadpool-count", "", nil, nil, "Quantidade de thread pools ktransformers.", sglangGroupOffload, intPtr(1), nil),
		intFlag("kt-threadpool-size", "", nil, nil, "Tamanho dos thread pools ktransformers.", sglangGroupOffload, intPtr(1), nil),
		intFlag("kt-experts-per-token", "", nil, nil, "Experts por token em ktransformers.", sglangGroupOffload, intPtr(1), nil),
		intFlag("kt-experts-per-rank", "", nil, nil, "Experts por rank em ktransformers.", sglangGroupOffload, intPtr(1), nil),
		intFlag("kt-max-deferred-experts-per-token", "", nil, nil, "Experts adiados para CPU por token.", sglangGroupOffload, intPtr(0), nil),
		intFlag("kt-num-gpu-experts", "", nil, nil, "Número de experts na GPU em ktransformers.", sglangGroupOffload, intPtr(0), nil),

		strFlag("limit-mm-data-per-request", "", nil, nil, "Limite JSON de inputs multimodais por requisição.", sglangGroupMultimodal, false),
		intFlag("mm-max-concurrent-calls", "", nil, nil, "Chamadas concorrentes no processamento multimodal async.", sglangGroupMultimodal, intPtr(1), nil),
		intFlag("mm-per-request-timeout", "", nil, nil, "Timeout por requisição multimodal, em segundos.", sglangGroupMultimodal, intPtr(0), nil),
		boolFlag("enable-broadcast-mm-inputs-process", "", nil, false, "Broadcast do processo de mm-inputs no scheduler.", sglangGroupMultimodal),
		strFlag("mm-process-config", "", nil, nil, "Config JSON de pré-processamento multimodal.", sglangGroupMultimodal, false),
		boolFlag("mm-enable-dp-encoder", "", nil, false, "DP no encoder multimodal.", sglangGroupMultimodal),
		boolFlag("disable-fast-image-processor", "", nil, false, "Usa image processor base em vez do rápido.", sglangGroupMultimodal),
		boolFlag("keep-mm-feature-on-device", "", nil, false, "Mantém features multimodais no device.", sglangGroupMultimodal),
		boolFlag("enable-prefix-mm-cache", "", nil, false, "Cache de prefixo multimodal.", sglangGroupMultimodal),
		boolFlag("encoder-only", "", nil, false, "Sobe servidor só de encoder.", sglangGroupMultimodal),
		boolFlag("language-only", "", nil, false, "Carrega só o LM em VLM.", sglangGroupMultimodal),
		enumFlag("encoder-transfer-backend", "", nil, []string{"zmq_to_scheduler", "zmq_to_tokenizer", "mooncake"}, nil, "Backend de transferência do encoder.", sglangGroupMultimodal),
		strFlag("encoder-urls", "", nil, nil, "Lista de URLs de servidores de encoder.", sglangGroupMultimodal, false),

		strFlag("api-key", "", nil, nil, "Chave de API OpenAI.", sglangGroupOpenAI, false),
		strFlag("admin-api-key", "", nil, nil, "Chave admin.", sglangGroupOpenAI, false),
		strFlag("file-storage-pth", "", nil, nil, "Caminho de armazenamento de arquivos.", sglangGroupOpenAI, false),
		strFlag("chat-template", "", nil, nil, "Template de chat.", sglangGroupOpenAI, false),
		enumFlag("reasoning-parser", "", nil, []string{"deepseek-r1", "qwen3", "gpt-oss"}, nil, "Parser de reasoning.", sglangGroupOpenAI),
		enumFlag("tool-call-parser", "", nil, []string{"hermes", "llama3", "qwen", "mistral"}, nil, "Parser de tool calls.", sglangGroupOpenAI),
		boolFlag("allow-auto-truncate", "", nil, false, "Permite truncar requisições longas.", sglangGroupOpenAI),
		boolFlag("enable-custom-logit-processor", "", nil, false, "Permite logit processors customizados.", sglangGroupOpenAI),

		strFlag("log-level", "", nil, nil, "Nível de log.", sglangGroupObservability, false),
		boolFlag("log-requests", "", nil, false, "Loga requisições.", sglangGroupObservability),
		enumFlag("log-requests-level", "", nil, []string{"0", "1", "2", "3"}, "0", "Verbosidade do log de requisições.", sglangGroupObservability),
		boolFlag("show-time-cost", "", nil, false, "Mostra custo de tempo em logs.", sglangGroupObservability),
		boolFlag("enable-metrics", "", nil, false, "Habilita métricas Prometheus.", sglangGroupObservability),
		boolFlag("enable-request-time-stats-logging", "", nil, false, "Loga estatísticas de tempo por requisição.", sglangGroupObservability),
		boolFlag("enable-trace", "", nil, false, "Habilita tracing OpenTelemetry.", sglangGroupObservability),
		strFlag("oltp-traces-endpoint", "", nil, nil, "Endpoint OLTP/OTLP de traces.", sglangGroupObservability, false),
		strFlag("otlp-traces-endpoint", "", nil, nil, "Endpoint OTLP de traces.", sglangGroupObservability, false),

		enumFlag("disaggregation-mode", "", nil, []string{"null", "prefill", "decode"}, "null", "Papel na disaggregation PD.", sglangGroupDisaggregation),
		enumFlag("disaggregation-transfer-backend", "", nil, []string{"mooncake", "nixl", "ascend", "fake", "mori"}, "mooncake", "Backend de transferência PD.", sglangGroupDisaggregation),
		portFlag("disaggregation-bootstrap-port", "", 8998, "Porta bootstrap do prefill.", sglangGroupDisaggregation),
		intFlag("disaggregation-prefill-pp", "", nil, 1, "Pipeline parallelism do prefill no decode.", sglangGroupDisaggregation, intPtr(1), nil),
		intFlag("disaggregation-ib-device", "", nil, nil, "Dispositivo InfiniBand usado na disaggregation.", sglangGroupDisaggregation, intPtr(0), nil),
		strFlag("disaggregation-decode-tp", "", nil, nil, "Tensor parallelism dos nós decode.", sglangGroupDisaggregation, false),
		strFlag("disaggregation-decode-pp", "", nil, nil, "Pipeline parallelism dos nós decode.", sglangGroupDisaggregation, false),

		boolFlag("enable-deterministic-inference", "", nil, false, "Ativa inferência determinística.", sglangGroupDebug),
		boolFlag("enable-nan-detection", "", nil, false, "Ativa detecção de NaN.", sglangGroupDebug),
		strFlag("debug-tensor-dump-output-folder", "", nil, nil, "Pasta para dump de tensores de debug.", sglangGroupDebug, false),
		strFlag("debug-tensor-dump-input-file", "", nil, nil, "Arquivo de input para dump/replay de tensores.", sglangGroupDebug, false),
	} {
		add(spec)
	}

	return domain.BackendValidationSchema{
		SchemaVersion: 1,
		Kind:          domain.ValidationSchemaCLIFlagsV1,
		BackendKind:   domain.BackendKindSGLang,
		BackendID:     "sglang-default",
		Source: domain.SchemaSource{
			GeneratedFrom: "sglang.md curated reference",
			SourceVersion: "curated-sglang",
			Editable:      true,
		},
		Flags:        flags,
		Presentation: sglangPresentation(),
	}
}

func sglangPresentation() *domain.Presentation {
	return &domain.Presentation{Groups: []domain.PresentationGroup{
		{Name: sglangGroupEssentials, Highlighted: true, Flags: []string{"host", "port", "api-key", "served-model-name", "device", "dtype", "context-length", "tensor-parallel-size", "data-parallel-size", "mem-fraction-static", "max-total-tokens", "kv-cache-dtype", "quantization", "load-format", "schedule-policy", "chunked-prefill-size", "enable-metrics"}},
		{Name: sglangGroupServer, Flags: []string{"config", "host", "port", "fastapi-root-path", "grpc-mode", "skip-server-warmup", "warmups", "nccl-port", "checkpoint-engine-wait-weights-before-ready", "watchdog-timeout", "soft-watchdog-timeout", "dist-timeout", "custom-sigquit-handler", "sleep-on-idle", "base-gpu-id", "gpu-id-step", "numa-node", "scheduler-recv-interval"}},
		{Name: sglangGroupModel, Flags: []string{"tokenizer-path", "tokenizer-mode", "tokenizer-worker-num", "skip-tokenizer-init", "trust-remote-code", "context-length", "is-embedding", "enable-multimodal", "revision", "model-impl", "served-model-name", "weight-version", "json-model-override-args", "model-checksum"}},
		{Name: sglangGroupLoad, Flags: []string{"load-format", "model-loader-extra-config", "download-dir", "custom-weight-loader", "ignore-config-weight-sync-check", "dtype", "quantization", "kv-cache-dtype", "enable-fp32-lm-head", "quantization-param-path", "ptao-config", "torchao-config", "cpuinfer-config"}},
		{Name: sglangGroupMemory, Flags: []string{"mem-fraction-static", "max-running-requests", "max-queued-requests", "max-total-tokens", "page-size", "swa-full-tokens-ratio"}},
		{Name: sglangGroupPrefixCache, Flags: []string{"disable-radix-cache", "radix-eviction-policy", "enable-hierarchical-cache", "hicache-write-policy", "hicache-mem-layout", "hicache-storage-backend", "hicache-storage-prefetch-policy", "hicache-storage-backend-extra-config", "enable-lmcache"}},
		{Name: sglangGroupPrefill, Flags: []string{"chunked-prefill-size", "prefill-max-requests", "enable-dynamic-chunking", "max-prefill-tokens", "enable-mixed-chunk", "disable-chunked-prefix-cache", "schedule-policy", "enable-priority-scheduling", "abort-on-priority-when-disabled", "schedule-low-priority-values-first", "priority-scheduling-preemption-threshold", "schedule-conservativeness", "num-continuous-decode-steps", "disable-overlap-schedule", "enable-prefill-delayer", "prefill-delayer-max-delay-passes", "prefill-delayer-token-usage-low-watermark"}},
		{Name: sglangGroupSampling, Flags: []string{"stream-interval", "random-seed", "sampling-defaults", "grammar-backend", "constrained-json-whitespace-pattern", "constrained-json-disable-any-whitespace"}},
		{Name: sglangGroupParallel, Flags: []string{"device", "tensor-parallel-size", "pipeline-parallel-size", "data-parallel-size", "attention-context-parallel-size", "moe-data-parallel-size", "pp-max-micro-batch-size", "pp-async-batch-depth", "load-balance-method", "dist-init-addr", "nnodes", "node-rank", "enable-dp-attention", "enable-dp-lm-head", "enable-attn-tp-input-scattered", "moe-dense-tp-size"}},
		{Name: sglangGroupMoE, Flags: []string{"expert-parallel-size", "moe-a2a-backend", "moe-runner-backend", "flashinfer-mxfp4-moe-precision", "enable-flashinfer-allreduce-fusion", "deepep-mode", "ep-num-redundant-experts", "ep-dispatch-algorithm", "init-expert-location", "enable-eplb", "eplb-algorithm", "eplb-rebalance-num-iterations", "eplb-rebalance-layers-per-chunk", "eplb-min-rebalancing-utilization-threshold", "expert-distribution-recorder-mode", "expert-distribution-recorder-buffer-size", "enable-expert-distribution-metrics", "deepep-config", "elastic-ep-backend", "mooncake-ib-device", "disable-shared-experts-fusion"}},
		{Name: sglangGroupKernels, Flags: []string{"attention-backend", "prefill-attention-backend", "decode-attention-backend", "sampling-backend", "mm-attention-backend", "fp8-gemm-backend", "fp4-gemm-backend", "disable-flashinfer-autotune", "triton-attention-num-kv-splits"}},
		{Name: sglangGroupSparseMamba, Flags: []string{"nsa-prefill-backend", "nsa-decode-backend", "enable-double-sparsity", "ds-channel-config-path", "ds-heavy-channel-num", "ds-heavy-channel-threshold", "ds-heavy-token-num", "ds-sparse-decode-threshold", "mamba-backend", "mamba-ssm-dtype"}},
		{Name: sglangGroupLoRA, Flags: []string{"enable-lora", "lora-paths", "max-lora-rank", "lora-target-modules", "max-loras-per-batch", "lora-eviction-policy", "lora-backend"}},
		{Name: sglangGroupSpeculative, Flags: []string{"speculative-algorithm", "speculative-draft-model-path", "speculative-num-steps", "speculative-eagle-topk", "speculative-num-draft-tokens", "speculative-attention-mode"}},
		{Name: sglangGroupCompile, Flags: []string{"disable-cuda-graph", "cuda-graph-max-bs", "enable-cudagraph-gc", "enable-torch-compile", "torch-compile-max-bs", "enable-torch-compile-debug-mode", "enable-piecewise-cuda-graph", "piecewise-cuda-graph-compiler", "piecewise-cuda-graph-max-bs", "piecewise-cuda-graph-max-tokens", "piecewise-cuda-graph-capture-forward-mode"}},
		{Name: sglangGroupCollectives, Flags: []string{"disable-custom-all-reduce", "enable-mscclpp", "enable-torch-symm-mem", "enable-symm-mem", "enable-nccl-nvls", "enable-flashinfer-allreduce-fusion", "enable-p2p-check", "disable-flashinfer-cutlass-moe-fp4-allgather"}},
		{Name: sglangGroupOffload, Flags: []string{"cpu-offload-gb", "offload-group-size", "offload-num-in-group", "offload-prefetch-step", "offload-mode", "enable-memory-saver", "enable-weights-cpu-backup", "enable-draft-weights-cpu-backup", "kt-weight-path", "kt-method", "kt-cpuinfer", "kt-threadpool-count", "kt-threadpool-size", "kt-experts-per-token", "kt-experts-per-rank", "kt-max-deferred-experts-per-token", "kt-num-gpu-experts"}},
		{Name: sglangGroupMultimodal, Flags: []string{"enable-multimodal", "limit-mm-data-per-request", "mm-max-concurrent-calls", "mm-per-request-timeout", "enable-broadcast-mm-inputs-process", "mm-process-config", "mm-enable-dp-encoder", "disable-fast-image-processor", "keep-mm-feature-on-device", "enable-prefix-mm-cache", "encoder-only", "language-only", "encoder-transfer-backend", "encoder-urls"}},
		{Name: sglangGroupOpenAI, Flags: []string{"api-key", "admin-api-key", "file-storage-pth", "chat-template", "reasoning-parser", "tool-call-parser", "allow-auto-truncate", "enable-custom-logit-processor"}},
		{Name: sglangGroupObservability, Flags: []string{"log-level", "log-requests", "log-requests-level", "show-time-cost", "enable-metrics", "enable-request-time-stats-logging", "enable-trace", "oltp-traces-endpoint", "otlp-traces-endpoint"}},
		{Name: sglangGroupDisaggregation, Flags: []string{"disaggregation-mode", "disaggregation-transfer-backend", "disaggregation-bootstrap-port", "disaggregation-prefill-pp", "disaggregation-ib-device", "disaggregation-decode-tp", "disaggregation-decode-pp"}},
		{Name: sglangGroupDebug, Flags: []string{"enable-deterministic-inference", "enable-nan-detection", "debug-tensor-dump-output-folder", "debug-tensor-dump-input-file"}},
	}}
}

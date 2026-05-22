package backendschema

import "github.com/quantmind-br/model-loader/internal/domain"

const (
	buunGroupEssentials  = "Essenciais"
	buunGroupModelLoad   = "1. Carregamento do modelo"
	buunGroupContext     = "2. Contexto e geração"
	buunGroupCPU         = "3. Threads e CPU"
	buunGroupMemory      = "4. Memória e I-O"
	buunGroupDevice      = "5. Dispositivos e GPU"
	buunGroupKV          = "6. KV cache"
	buunGroupRope        = "7. RoPE e YaRN"
	buunGroupShift       = "8. Context shift e SWA"
	buunGroupCacheRAM    = "9. Cache RAM, slots e KV unificado"
	buunGroupSamplers    = "10. Samplers e amostragem"
	buunGroupPenalties   = "11. Penalidades e técnicas avançadas de sampling"
	buunGroupGrammar     = "12. Gramática e schema"
	buunGroupChat        = "13. Chat template, Jinja e reasoning"
	buunGroupHTTP        = "14. Servidor HTTP"
	buunGroupMultimodal  = "15. Multimodal (vision)"
	buunGroupRouter      = "16. Router server (multi-modelo)"
	buunGroupTools       = "17. Tools de agente embutidas"
	buunGroupEmbeddings  = "18. Embeddings e reranking"
	buunGroupLora        = "19. LoRA e control vectors"
	buunGroupSpeculative = "20. Speculative decoding"
	buunGroupTTS         = "21. TTS e vocoder"
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
		strFlag("hf-repo", "hf", nil, nil, "Baixa/carrega um modelo direto do Hugging Face; o quant é opcional.", buunGroupModelLoad, false),
		strFlag("hf-file", "hff", nil, nil, "Arquivo específico dentro do repositório HF, sobrescrevendo o quant automático.", buunGroupModelLoad, false),
		strFlag("hf-token", "", nil, nil, "Token de autenticação do Hugging Face para repositórios privados.", buunGroupModelLoad, false),
		strFlag("model-url", "mu", nil, nil, "URL de download do modelo.", buunGroupModelLoad, false),
		strFlag("docker-repo", "", nil, nil, "Repositório Docker usado por fluxos integrados da fork buun.", buunGroupModelLoad, false),
		boolFlag("offline", "", nil, false, "Força modo offline usando apenas o cache local, sem rede.", buunGroupModelLoad),

		intFlag("ctx-size", "c", nil, 4096, "Tamanho da janela de contexto; 0 usa o valor carregado do modelo.", buunGroupContext, intPtr(0), nil),
		intFlag("n-predict", "n", []string{"predict"}, -1, "Quantos tokens gerar; -1 = geração infinita.", buunGroupContext, nil, nil),
		intFlag("batch-size", "b", nil, 2048, "Tamanho lógico máximo do batch no processamento do prompt.", buunGroupContext, intPtr(1), nil),
		intFlag("ubatch-size", "ub", nil, 512, "Tamanho físico máximo do micro-batch (ajuste de throughput/memória).", buunGroupContext, intPtr(1), nil),
		intFlag("keep", "", nil, 0, "Tokens iniciais do prompt a manter ao fazer shifting/reuso; -1 mantém todos.", buunGroupContext, nil, nil),

		intFlag("threads", "t", nil, nil, "Threads de CPU usadas na geração.", buunGroupCPU, intPtr(0), nil),
		intFlag("threads-batch", "tb", nil, nil, "Threads no processamento de batch/prefill; herda --threads por padrão.", buunGroupCPU, intPtr(0), nil),
		intFlag("poll", "", nil, 50, "Nível de polling ao esperar trabalho; 0 desliga.", buunGroupCPU, intPtr(0), intPtr(100)),
		enumFlag("prio", "", nil, []string{"-1", "0", "1", "2", "3"}, 0, "Prioridade do processo/thread: low, normal, medium, high, realtime.", buunGroupCPU),
		enumFlag("numa", "", nil, []string{"distribute", "isolate", "numactl"}, nil, "Otimizações para máquinas NUMA.", buunGroupCPU),

		boolFlag("mlock", "", nil, false, "Mantém o modelo em RAM, evitando swap.", buunGroupMemory),
		boolFlag("mmap", "", []string{"no-mmap"}, true, "Memory mapping do modelo; desligar pode reduzir pageouts, mas torna o load mais lento.", buunGroupMemory),
		boolFlag("direct-io", "dio", []string{"no-direct-io"}, false, "Usa Direct I/O quando disponível.", buunGroupMemory),
		boolFlag("repack", "", []string{"no-repack"}, true, "Liga/desliga repacking dos pesos.", buunGroupMemory),
		boolFlag("op-offload", "", []string{"no-op-offload"}, true, "Offload de operações de tensores do host para o device.", buunGroupMemory),
		boolFlag("no-host", "", nil, false, "Bypassa o host buffer para permitir buffers extras.", buunGroupMemory),
		boolFlag("check-tensors", "", nil, false, "Verifica tensores do modelo em busca de valores inválidos.", buunGroupMemory),

		strFlag("device", "dev", nil, nil, "Seleciona os dispositivos usados para offload.", buunGroupDevice, false),
		boolFlag("list-devices", "", nil, false, "Lista os dispositivos disponíveis e sai.", buunGroupDevice),
		strFlag("n-gpu-layers", "ngl", []string{"gpu-layers"}, 0, "Quantas camadas do modelo vão para a VRAM; aceita inteiro, auto ou all.", buunGroupDevice, false),
		enumFlag("split-mode", "sm", nil, []string{"none", "layer", "row", "tensor"}, "layer", "Como dividir o modelo entre múltiplas GPUs.", buunGroupDevice),
		strFlag("tensor-split", "ts", nil, nil, "Proporção de offload entre GPUs (ex.: 3,1).", buunGroupDevice, false),
		intFlag("main-gpu", "mg", nil, 0, "GPU principal para o modelo/resultados intermediários (depende do split mode).", buunGroupDevice, intPtr(0), nil),
		enumFlag("fit", "fit", nil, []string{"on", "off"}, "on", "Ajusta parâmetros não definidos para caber na memória do dispositivo.", buunGroupDevice),
		strFlag("fit-target", "fitt", nil, nil, "Margem alvo de memória por dispositivo usada por --fit.", buunGroupDevice, false),
		intFlag("fit-ctx", "fitc", nil, nil, "Contexto mínimo que --fit pode configurar.", buunGroupDevice, intPtr(0), nil),

		boolFlag("kv-offload", "kvo", []string{"no-kv-offload"}, true, "Controla o offload do KV cache.", buunGroupKV),
		enumFlag("cache-type-k", "ctk", nil, buunCacheTypes(), "f16", "Tipo de dado do cache K, incluindo formatos turbo da fork buun.", buunGroupKV),
		enumFlag("cache-type-v", "ctv", nil, buunCacheTypes(), "f16", "Tipo de dado do cache V, incluindo formatos turbo da fork buun.", buunGroupKV),

		enumFlag("rope-scaling", "", nil, []string{"none", "linear", "yarn"}, "none", "Método de scaling de frequência do RoPE.", buunGroupRope),
		floatFlag("rope-scale", "", nil, nil, "Fator de expansão do contexto via RoPE.", buunGroupRope, nil, nil),
		floatFlag("rope-freq-base", "", nil, nil, "Frequência base do RoPE (scaling NTK-aware).", buunGroupRope, nil, nil),
		floatFlag("rope-freq-scale", "", nil, nil, "Fator de scaling da frequência; expande contexto por 1/N.", buunGroupRope, nil, nil),
		intFlag("yarn-orig-ctx", "", nil, 0, "Contexto original do modelo para YaRN; 0 usa o de treino.", buunGroupRope, intPtr(0), nil),
		floatFlag("yarn-ext-factor", "", nil, -1.0, "Fator de extrapolação/interpolação do YaRN.", buunGroupRope, nil, nil),
		floatFlag("yarn-attn-factor", "", nil, 1.0, "Ajuste da magnitude da atenção no YaRN.", buunGroupRope, nil, nil),
		floatFlag("yarn-beta-slow", "", nil, 1.0, "Parâmetro slow/high correction dim do YaRN.", buunGroupRope, nil, nil),
		floatFlag("yarn-beta-fast", "", nil, 32.0, "Parâmetro fast/low correction dim do YaRN.", buunGroupRope, nil, nil),

		boolFlag("swa-full", "", nil, false, "Usa cache SWA em tamanho completo.", buunGroupShift),
		boolFlag("context-shift", "", []string{"no-context-shift"}, false, "Controla o context shift em geração infinita.", buunGroupShift),

		boolFlag("cache-ram", "", []string{"no-cache-ram"}, false, "Habilita cache em RAM para acelerar reuso de KV/prompt.", buunGroupCacheRAM),
		boolFlag("kv-unified", "", []string{"no-kv-unified"}, false, "Usa KV cache unificado entre slots/modelos quando suportado.", buunGroupCacheRAM),
		intFlag("cache-idle-slots", "", nil, nil, "Quantidade de slots ociosos mantidos em cache.", buunGroupCacheRAM, intPtr(0), nil),
		intFlag("sleep-idle-seconds", "", nil, nil, "Tempo ocioso antes de reduzir atividade/suspender slots.", buunGroupCacheRAM, intPtr(0), nil),

		strFlag("samplers", "", nil, "penalties;dry;top_n_sigma;top_k;typ_p;top_p;min_p;xtc;temperature", "Ordem dos samplers aplicados na geração.", buunGroupSamplers, false),
		intFlag("seed", "s", nil, -1, "Seed do RNG; -1 = aleatória.", buunGroupSamplers, nil, nil),
		floatFlag("temperature", "", []string{"temp"}, 0.8, "Temperatura da amostragem; maior = mais diversidade.", buunGroupSamplers, floatPtr(0), nil),
		intFlag("top-k", "", nil, 40, "Mantém os k tokens mais prováveis; 0 desativa.", buunGroupSamplers, intPtr(0), nil),
		floatFlag("top-p", "", nil, 0.9, "Nucleus sampling; 1.0 desativa.", buunGroupSamplers, floatPtr(0), floatPtr(1)),
		floatFlag("min-p", "", nil, 0.1, "Mantém tokens acima de uma probabilidade mínima relativa.", buunGroupSamplers, floatPtr(0), floatPtr(1)),
		floatFlag("typical-p", "", []string{"typical"}, 1.0, "Locally typical sampling; 1.0 desativa.", buunGroupSamplers, floatPtr(0), floatPtr(1)),
		floatFlag("top-n-sigma", "", nil, -1.0, "Top-n-sigma sampling; -1.0 desativa.", buunGroupSamplers, nil, nil),
		boolFlag("ignore-eos", "", nil, false, "Ignora o token EOS e continua gerando.", buunGroupSamplers),
		strFlag("logit-bias", "l", nil, nil, "Aumenta/reduz a chance de tokens específicos no formato TOKEN_ID(+/-)BIAS.", buunGroupSamplers, false),
		boolFlag("backend-sampling", "", []string{"no-backend-sampling"}, false, "Executa sampling no backend quando suportado pela fork buun.", buunGroupSamplers),

		intFlag("repeat-last-n", "", nil, 64, "Tokens recentes considerados para penalidade de repetição; -1 = ctx-size.", buunGroupPenalties, nil, nil),
		floatFlag("repeat-penalty", "", nil, 1.0, "Penalidade de repetição; 1.0 desativa.", buunGroupPenalties, nil, nil),
		floatFlag("presence-penalty", "", nil, 0.0, "Penalidade por presença; aumenta custo de tokens já vistos.", buunGroupPenalties, nil, nil),
		floatFlag("frequency-penalty", "", nil, 0.0, "Penalidade proporcional à frequência dos tokens já vistos.", buunGroupPenalties, nil, nil),
		floatFlag("dry-multiplier", "", nil, 0.0, "Intensidade do DRY; 0.0 desativa.", buunGroupPenalties, floatPtr(0), nil),
		floatFlag("dry-base", "", nil, 1.75, "Base do DRY para penalizar sequências repetitivas.", buunGroupPenalties, nil, nil),
		intFlag("dry-allowed-length", "", nil, 2, "Comprimento tolerado antes do DRY.", buunGroupPenalties, intPtr(0), nil),
		intFlag("dry-penalty-last-n", "", nil, -1, "Janela do DRY; -1 = contexto inteiro.", buunGroupPenalties, nil, nil),
		strFlag("dry-sequence-breaker", "", nil, "\\n, :, \", *", "Quebras que resetam o DRY.", buunGroupPenalties, false),
		enumFlag("mirostat", "", nil, []string{"0", "1", "2"}, 0, "Modo Mirostat; 0 desativa.", buunGroupPenalties),
		floatFlag("mirostat-lr", "", nil, 0.1, "Learning rate (eta) do Mirostat.", buunGroupPenalties, nil, nil),
		floatFlag("mirostat-ent", "", nil, 5.0, "Entropia alvo (tau) do Mirostat.", buunGroupPenalties, nil, nil),
		floatFlag("dynatemp-range", "", nil, 0.0, "Faixa de temperatura dinâmica; 0.0 desativa.", buunGroupPenalties, floatPtr(0), nil),
		floatFlag("dynatemp-exp", "", nil, 1.0, "Expoente da temperatura dinâmica.", buunGroupPenalties, nil, nil),
		floatFlag("adaptive-target", "", nil, nil, "Alvo do adaptive-p; valores negativos desativam.", buunGroupPenalties, nil, floatPtr(1)),
		floatFlag("adaptive-decay", "", nil, nil, "Decaimento do adaptive-p.", buunGroupPenalties, floatPtr(0), floatPtr(0.99)),

		strFlag("grammar", "", nil, "", "Restringe a saída por gramática GBNF.", buunGroupGrammar, false),
		strFlag("grammar-file", "", nil, nil, "Lê gramática de arquivo.", buunGroupGrammar, false),
		strFlag("json-schema", "j", nil, nil, "Restringe a saída a um JSON Schema.", buunGroupGrammar, false),
		strFlag("json-schema-file", "jf", nil, nil, "Carrega JSON Schema de arquivo.", buunGroupGrammar, false),

		strFlag("chat-template", "", nil, nil, "Define o template de chat (embutido ou customizado).", buunGroupChat, false),
		strFlag("chat-template-file", "", nil, nil, "Lê o template de chat de um arquivo Jinja.", buunGroupChat, false),
		strFlag("chat-template-kwargs", "", nil, nil, "Argumentos extras em JSON para o parser do template.", buunGroupChat, false),
		boolFlag("jinja", "", []string{"no-jinja"}, false, "Liga/desliga o engine Jinja para chat.", buunGroupChat),
		boolFlag("skip-chat-parsing", "", nil, false, "Força parser puro de conteúdo, sem parsing estrutural.", buunGroupChat),
		boolFlag("prefill-assistant", "", []string{"no-prefill-assistant"}, true, "Controla o prefill da resposta quando a última mensagem já é do assistant.", buunGroupChat),
		enumFlag("reasoning-format", "", nil, []string{"none", "deepseek", "deepseek-legacy", "auto"}, "auto", "Formato usado para extrair blocos de reasoning.", buunGroupChat),
		enumFlag("reasoning", "", nil, []string{"on", "off", "auto"}, "auto", "Controla emissão/uso de reasoning quando o modelo suporta.", buunGroupChat),
		intFlag("reasoning-budget", "", nil, nil, "Orçamento de tokens para reasoning.", buunGroupChat, intPtr(0), nil),
		strFlag("reasoning-budget-message", "", nil, nil, "Mensagem/instrução usada para comunicar o orçamento de reasoning ao modelo.", buunGroupChat, false),

		strFlag("host", "", nil, "127.0.0.1", "Endereço de escuta do servidor.", buunGroupHTTP, false),
		portFlag("port", "", 8080, "Porta HTTP do servidor.", buunGroupHTTP),
		boolFlag("reuse-port", "", nil, false, "Permite múltiplos sockets no mesmo porto.", buunGroupHTTP),
		strFlag("api-prefix", "", nil, "", "Prefixo de rota da API (sem barra final).", buunGroupHTTP, false),
		strFlag("path", "", nil, nil, "Diretório de arquivos estáticos a servir.", buunGroupHTTP, false),
		boolFlag("ui", "", []string{"no-ui"}, true, "Liga/desliga a interface web.", buunGroupHTTP),
		strFlag("ui-config", "", nil, nil, "Sobrescreve configurações padrão da UI.", buunGroupHTTP, false),
		strFlag("ui-config-file", "", nil, nil, "Carrega configurações da UI de um arquivo JSON.", buunGroupHTTP, false),
		strFlag("api-key", "", nil, nil, "Define chaves de autenticação da API.", buunGroupHTTP, false),
		strFlag("api-key-file", "", nil, nil, "Lê chaves de autenticação de um arquivo.", buunGroupHTTP, false),
		strFlag("ssl-key-file", "", nil, nil, "Chave privada SSL.", buunGroupHTTP, false),
		strFlag("ssl-cert-file", "", nil, nil, "Certificado SSL.", buunGroupHTTP, false),
		intFlag("timeout", "to", nil, 600, "Timeout de leitura/escrita (segundos).", buunGroupHTTP, intPtr(0), nil),
		intFlag("threads-http", "", nil, -1, "Threads para requisições HTTP; -1 = automático.", buunGroupHTTP, nil, nil),
		boolFlag("metrics", "", nil, false, "Endpoint de métricas compatível com Prometheus.", buunGroupHTTP),
		boolFlag("slots", "", []string{"no-slots"}, true, "Expõe endpoint de monitoramento de slots.", buunGroupHTTP),
		boolFlag("props", "", nil, false, "Permite alterar propriedades globais via POST /props.", buunGroupHTTP),
		intFlag("parallel", "np", nil, 1, "Slots paralelos do servidor; -1 = automático.", buunGroupHTTP, nil, nil),
		boolFlag("cont-batching", "cb", []string{"no-cont-batching"}, true, "Liga/desliga continuous batching.", buunGroupHTTP),
		boolFlag("cache-prompt", "", []string{"no-cache-prompt"}, true, "Ativa reuso/cache de prompt.", buunGroupHTTP),
		intFlag("cache-reuse", "", nil, 0, "Chunk mínimo para tentar reuso via KV shifting.", buunGroupHTTP, intPtr(0), nil),
		strFlag("alias", "a", nil, nil, "Aliases de nome do modelo (API), separados por vírgula.", buunGroupHTTP, false),

		strFlag("mmproj", "mm", nil, nil, "Projector multimodal.", buunGroupMultimodal, false),
		strFlag("mmproj-url", "mmu", nil, nil, "URL do projector multimodal.", buunGroupMultimodal, false),
		boolFlag("mmproj-auto", "", []string{"no-mmproj-auto", "no-mmproj"}, true, "Uso automático do projector multimodal.", buunGroupMultimodal),
		boolFlag("mmproj-offload", "", []string{"no-mmproj-offload"}, true, "Offload do projector para GPU.", buunGroupMultimodal),
		intFlag("image-min-tokens", "", nil, nil, "Mínimo de tokens por imagem.", buunGroupMultimodal, intPtr(0), nil),
		intFlag("image-max-tokens", "", nil, nil, "Máximo de tokens por imagem.", buunGroupMultimodal, intPtr(0), nil),

		strFlag("models-dir", "", nil, nil, "Diretório monitorado pelo router multi-modelo.", buunGroupRouter, false),
		strFlag("models-preset", "", nil, nil, "Preset de modelos carregado pelo router multi-modelo.", buunGroupRouter, false),
		intFlag("models-max", "", nil, nil, "Número máximo de modelos gerenciados/carregados pelo router.", buunGroupRouter, intPtr(1), nil),
		boolFlag("models-autoload", "", []string{"no-models-autoload"}, false, "Carrega modelos automaticamente a partir do diretório/preset.", buunGroupRouter),

		strFlag("tools", "", nil, nil, "Lista/configuração de tools de agente embutidas expostas pelo servidor.", buunGroupTools, false),

		boolFlag("embeddings", "", []string{"embedding"}, false, "Modo embeddings.", buunGroupEmbeddings),
		enumFlag("pooling", "", nil, []string{"none", "mean", "cls", "last", "rank"}, nil, "Pooling de embeddings.", buunGroupEmbeddings),
		intFlag("embd-normalize", "", nil, 2, "Normalização; 2 = euclidiana.", buunGroupEmbeddings, nil, nil),
		boolFlag("reranking", "", []string{"rerank"}, false, "Endpoint de reranking.", buunGroupEmbeddings),

		strFlag("lora", "", nil, nil, "Adapters LoRA.", buunGroupLora, false),
		strFlag("lora-scaled", "", nil, nil, "LoRA com escala manual no formato ARQUIVO:SCALE.", buunGroupLora, false),
		boolFlag("lora-init-without-apply", "", nil, false, "Carrega LoRA sem aplicar.", buunGroupLora),
		strFlag("control-vector", "", nil, nil, "Control vector(s).", buunGroupLora, false),
		strFlag("control-vector-scaled", "", nil, nil, "Control vector com escala no formato ARQUIVO:SCALE.", buunGroupLora, false),
		strFlag("control-vector-layer-range", "", nil, nil, "Intervalo de camadas aplicado aos control vectors.", buunGroupLora, false),

		enumFlag("spec-type", "", nil, []string{"none", "draft-simple", "draft-eagle3", "draft-mtp", "ngram-simple", "ngram-map-k", "ngram-map-k4v", "ngram-mod", "ngram-cache", "suffix", "copyspec", "recycle", "dflash"}, "none", "Tipo de speculative decoding.", buunGroupSpeculative),
		strFlag("spec-draft-model", "md", nil, nil, "Modelo draft.", buunGroupSpeculative, false),
		strFlag("spec-draft-hf", "", nil, nil, "Repo HF do draft.", buunGroupSpeculative, false),
		intFlag("spec-draft-n-max", "", nil, 16, "Máximo de tokens propostos pelo draft.", buunGroupSpeculative, intPtr(0), nil),
		intFlag("spec-draft-n-min", "", nil, 0, "Mínimo de tokens draft.", buunGroupSpeculative, intPtr(0), nil),
		floatFlag("spec-draft-p-split", "", nil, 0.1, "Probabilidade de split.", buunGroupSpeculative, floatPtr(0), floatPtr(1)),
		floatFlag("spec-draft-p-min", "", nil, 0.75, "Probabilidade mínima do caminho guloso.", buunGroupSpeculative, floatPtr(0), floatPtr(1)),
		strFlag("spec-draft-device", "", nil, nil, "Dispositivos do draft; por padrão acompanha --device.", buunGroupSpeculative, false),
		strFlag("spec-draft-ngl", "", nil, 0, "Camadas do draft na VRAM; aceita inteiro, auto ou all.", buunGroupSpeculative, false),
		intFlag("spec-draft-threads", "", nil, nil, "Threads de CPU do draft; por padrão acompanha --threads.", buunGroupSpeculative, intPtr(0), nil),
		boolFlag("spec-draft-backend-sampling", "", []string{"no-spec-draft-backend-sampling"}, false, "Sampling do draft no backend.", buunGroupSpeculative),
		enumFlag("cache-type-k-draft", "", nil, buunCacheTypes(), "f16", "Tipo de dado do cache K usado pelo modelo draft.", buunGroupSpeculative),
		enumFlag("cache-type-v-draft", "", nil, buunCacheTypes(), "f16", "Tipo de dado do cache V usado pelo modelo draft.", buunGroupSpeculative),
		intFlag("spec-ngram-mod-n-min", "", nil, nil, "Mínimo de tokens para speculative n-gram.", buunGroupSpeculative, intPtr(0), nil),
		intFlag("spec-ngram-mod-n-max", "", nil, nil, "Máximo de tokens para speculative n-gram.", buunGroupSpeculative, intPtr(0), nil),
		intFlag("spec-ngram-mod-n-match", "", nil, nil, "Comprimento de lookup para ngram-mod.", buunGroupSpeculative, intPtr(0), nil),
		intFlag("spec-ngram-min", "", nil, nil, "Mínimo de tokens para modos ngram da fork buun.", buunGroupSpeculative, intPtr(0), nil),
		intFlag("spec-ngram-max", "", nil, nil, "Máximo de tokens para modos ngram da fork buun.", buunGroupSpeculative, intPtr(0), nil),
		intFlag("spec-ngram-match", "", nil, nil, "Comprimento de match para modos ngram da fork buun.", buunGroupSpeculative, intPtr(0), nil),
		intFlag("spec-ngram-cache-size", "", nil, nil, "Tamanho do cache usado por speculative ngram-cache.", buunGroupSpeculative, intPtr(0), nil),
		boolFlag("spec-dflash-default", "", nil, false, "Habilita configuração/default do modo speculative DFlash.", buunGroupSpeculative),
		intFlag("dflash-max-slots", "", nil, nil, "Número máximo de slots usados pelo DFlash.", buunGroupSpeculative, intPtr(1), nil),

		strFlag("model-vocoder", "", nil, nil, "Modelo vocoder usado para TTS.", buunGroupTTS, false),
		boolFlag("tts-use-guide-tokens", "", []string{"no-tts-use-guide-tokens"}, false, "Usa guide tokens no fluxo TTS.", buunGroupTTS),
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

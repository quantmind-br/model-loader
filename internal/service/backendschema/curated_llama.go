package backendschema

import "github.com/quantmind-br/model-loader/internal/domain"

const (
	llamaGroupEssentials  = "Essenciais"
	llamaGroupModelLoad   = "1. Carregamento do modelo"
	llamaGroupContext     = "2. Contexto e geração"
	llamaGroupCPU         = "3. Threads e CPU"
	llamaGroupMemory      = "4. Memória e I-O"
	llamaGroupDevice      = "5. Dispositivos e GPU"
	llamaGroupKV          = "6. KV cache"
	llamaGroupRope        = "7. RoPE e YaRN"
	llamaGroupShift       = "8. Context shift e SWA"
	llamaGroupSamplers    = "9. Samplers e amostragem"
	llamaGroupPenalties   = "10. Penalidades e técnicas avançadas de sampling"
	llamaGroupGrammar     = "11. Gramática e schema"
	llamaGroupChat        = "12. Chat template e Jinja"
	llamaGroupHTTP        = "13. Servidor HTTP"
	llamaGroupMultimodal  = "14. Multimodal (vision)"
	llamaGroupEmbeddings  = "15. Embeddings e reranking"
	llamaGroupLora        = "16. LoRA e control vectors"
	llamaGroupSpeculative = "17. Speculative decoding"
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
		strFlag("hf-repo", "hf", nil, nil, "Baixa/carrega um modelo direto do Hugging Face; o quant é opcional.", llamaGroupModelLoad, false),
		strFlag("hf-file", "hff", nil, nil, "Arquivo específico dentro do repositório HF, sobrescrevendo o quant automático.", llamaGroupModelLoad, false),
		strFlag("model-url", "mu", nil, nil, "URL de download do modelo.", llamaGroupModelLoad, false),
		boolFlag("offline", "", nil, false, "Força modo offline usando apenas o cache local, sem rede.", llamaGroupModelLoad),

		intFlag("ctx-size", "c", nil, 4096, "Tamanho da janela de contexto; 0 usa o valor carregado do modelo.", llamaGroupContext, intPtr(0), nil),
		intFlag("n-predict", "n", []string{"predict"}, -1, "Quantos tokens gerar; -1 = geração infinita.", llamaGroupContext, nil, nil),
		intFlag("batch-size", "b", nil, 2048, "Tamanho lógico máximo do batch no processamento do prompt.", llamaGroupContext, intPtr(1), nil),
		intFlag("ubatch-size", "ub", nil, 512, "Tamanho físico máximo do micro-batch (ajuste de throughput/memória).", llamaGroupContext, intPtr(1), nil),
		intFlag("keep", "", nil, 0, "Tokens iniciais do prompt a manter ao fazer shifting/reuso; -1 mantém todos.", llamaGroupContext, nil, nil),

		intFlag("threads", "t", nil, nil, "Threads de CPU usadas na geração.", llamaGroupCPU, intPtr(0), nil),
		intFlag("threads-batch", "tb", nil, nil, "Threads no processamento de batch/prefill; herda --threads por padrão.", llamaGroupCPU, intPtr(0), nil),
		intFlag("poll", "", nil, 50, "Nível de polling ao esperar trabalho; 0 desliga.", llamaGroupCPU, intPtr(0), intPtr(100)),
		enumFlag("prio", "", nil, []string{"-1", "0", "1", "2", "3"}, 0, "Prioridade do processo/thread: low, normal, medium, high, realtime.", llamaGroupCPU),
		enumFlag("numa", "", nil, []string{"distribute", "isolate", "numactl"}, nil, "Otimizações para máquinas NUMA.", llamaGroupCPU),

		boolFlag("mlock", "", nil, false, "Mantém o modelo em RAM, evitando swap.", llamaGroupMemory),
		boolFlag("mmap", "", []string{"no-mmap"}, true, "Memory mapping do modelo; desligar pode reduzir pageouts, mas torna o load mais lento.", llamaGroupMemory),
		boolFlag("direct-io", "dio", []string{"no-direct-io"}, false, "Usa Direct I/O quando disponível.", llamaGroupMemory),
		boolFlag("repack", "", []string{"no-repack"}, true, "Liga/desliga repacking dos pesos.", llamaGroupMemory),
		boolFlag("op-offload", "", []string{"no-op-offload"}, true, "Offload de operações de tensores do host para o device.", llamaGroupMemory),
		boolFlag("no-host", "", nil, false, "Bypassa o host buffer para permitir buffers extras.", llamaGroupMemory),
		boolFlag("check-tensors", "", nil, false, "Verifica tensores do modelo em busca de valores inválidos.", llamaGroupMemory),

		strFlag("device", "dev", nil, nil, "Seleciona os dispositivos usados para offload.", llamaGroupDevice, false),
		boolFlag("list-devices", "", nil, false, "Lista os dispositivos disponíveis e sai.", llamaGroupDevice),
		strFlag("n-gpu-layers", "ngl", []string{"gpu-layers"}, 0, "Quantas camadas do modelo vão para a VRAM; aceita inteiro, auto ou all.", llamaGroupDevice, false),
		enumFlag("split-mode", "sm", nil, []string{"none", "layer", "row", "tensor"}, "layer", "Como dividir o modelo entre múltiplas GPUs.", llamaGroupDevice),
		strFlag("tensor-split", "ts", nil, nil, "Proporção de offload entre GPUs (ex.: 3,1).", llamaGroupDevice, false),
		intFlag("main-gpu", "mg", nil, 0, "GPU principal para o modelo/resultados intermediários (depende do split mode).", llamaGroupDevice, intPtr(0), nil),
		enumFlag("fit", "fit", nil, []string{"on", "off"}, "on", "Ajusta parâmetros não definidos para caber na memória do dispositivo.", llamaGroupDevice),
		strFlag("fit-target", "fitt", nil, nil, "Margem alvo de memória por dispositivo usada por --fit.", llamaGroupDevice, false),
		intFlag("fit-ctx", "fitc", nil, nil, "Contexto mínimo que --fit pode configurar.", llamaGroupDevice, intPtr(0), nil),

		boolFlag("kv-offload", "kvo", []string{"no-kv-offload"}, true, "Controla o offload do KV cache.", llamaGroupKV),
		enumFlag("cache-type-k", "ctk", nil, llamaCacheTypes(), "f16", "Tipo de dado do cache K.", llamaGroupKV),
		enumFlag("cache-type-v", "ctv", nil, llamaCacheTypes(), "f16", "Tipo de dado do cache V.", llamaGroupKV),

		enumFlag("rope-scaling", "", nil, []string{"none", "linear", "yarn"}, "none", "Método de scaling de frequência do RoPE.", llamaGroupRope),
		floatFlag("rope-scale", "", nil, nil, "Fator de expansão do contexto via RoPE.", llamaGroupRope, nil, nil),
		floatFlag("rope-freq-base", "", nil, nil, "Frequência base do RoPE (scaling NTK-aware).", llamaGroupRope, nil, nil),
		floatFlag("rope-freq-scale", "", nil, nil, "Fator de scaling da frequência; expande contexto por 1/N.", llamaGroupRope, nil, nil),
		intFlag("yarn-orig-ctx", "", nil, 0, "Contexto original do modelo para YaRN; 0 usa o de treino.", llamaGroupRope, intPtr(0), nil),
		floatFlag("yarn-ext-factor", "", nil, -1.0, "Fator de extrapolação/interpolação do YaRN.", llamaGroupRope, nil, nil),
		floatFlag("yarn-attn-factor", "", nil, 1.0, "Ajuste da magnitude da atenção no YaRN.", llamaGroupRope, nil, nil),
		floatFlag("yarn-beta-slow", "", nil, 1.0, "Parâmetro slow/high correction dim do YaRN.", llamaGroupRope, nil, nil),
		floatFlag("yarn-beta-fast", "", nil, 32.0, "Parâmetro fast/low correction dim do YaRN.", llamaGroupRope, nil, nil),

		boolFlag("swa-full", "", nil, false, "Usa cache SWA em tamanho completo.", llamaGroupShift),
		boolFlag("context-shift", "", []string{"no-context-shift"}, false, "Controla o context shift em geração infinita.", llamaGroupShift),

		strFlag("samplers", "", nil, "penalties;dry;top_n_sigma;top_k;typ_p;top_p;min_p;xtc;temperature", "Ordem dos samplers aplicados na geração.", llamaGroupSamplers, false),
		intFlag("seed", "s", nil, -1, "Seed do RNG; -1 = aleatória.", llamaGroupSamplers, nil, nil),
		floatFlag("temperature", "", []string{"temp"}, 0.8, "Temperatura da amostragem; maior = mais diversidade.", llamaGroupSamplers, floatPtr(0), nil),
		intFlag("top-k", "", nil, 40, "Mantém os k tokens mais prováveis; 0 desativa.", llamaGroupSamplers, intPtr(0), nil),
		floatFlag("top-p", "", nil, 0.9, "Nucleus sampling; 1.0 desativa.", llamaGroupSamplers, floatPtr(0), floatPtr(1)),
		floatFlag("min-p", "", nil, 0.1, "Mantém tokens acima de uma probabilidade mínima relativa.", llamaGroupSamplers, floatPtr(0), floatPtr(1)),
		floatFlag("typical-p", "", []string{"typical"}, 1.0, "Locally typical sampling; 1.0 desativa.", llamaGroupSamplers, floatPtr(0), floatPtr(1)),
		floatFlag("top-n-sigma", "", nil, -1.0, "Top-n-sigma sampling; -1.0 desativa.", llamaGroupSamplers, nil, nil),
		boolFlag("ignore-eos", "", nil, false, "Ignora o token EOS e continua gerando.", llamaGroupSamplers),
		strFlag("logit-bias", "l", nil, nil, "Aumenta/reduz a chance de tokens específicos no formato TOKEN_ID(+/-)BIAS.", llamaGroupSamplers, false),

		intFlag("repeat-last-n", "", nil, 64, "Tokens recentes considerados para penalidade de repetição; -1 = ctx-size.", llamaGroupPenalties, nil, nil),
		floatFlag("repeat-penalty", "", nil, 1.0, "Penalidade de repetição; 1.0 desativa.", llamaGroupPenalties, nil, nil),
		floatFlag("presence-penalty", "", nil, 0.0, "Penalidade por presença; aumenta custo de tokens já vistos.", llamaGroupPenalties, nil, nil),
		floatFlag("frequency-penalty", "", nil, 0.0, "Penalidade proporcional à frequência dos tokens já vistos.", llamaGroupPenalties, nil, nil),
		floatFlag("dry-multiplier", "", nil, 0.0, "Intensidade do DRY; 0.0 desativa.", llamaGroupPenalties, floatPtr(0), nil),
		floatFlag("dry-base", "", nil, 1.75, "Base do DRY para penalizar sequências repetitivas.", llamaGroupPenalties, nil, nil),
		intFlag("dry-allowed-length", "", nil, 2, "Comprimento tolerado antes do DRY.", llamaGroupPenalties, intPtr(0), nil),
		intFlag("dry-penalty-last-n", "", nil, -1, "Janela do DRY; -1 = contexto inteiro.", llamaGroupPenalties, nil, nil),
		strFlag("dry-sequence-breaker", "", nil, "\\n, :, \", *", "Quebras que resetam o DRY.", llamaGroupPenalties, false),
		enumFlag("mirostat", "", nil, []string{"0", "1", "2"}, 0, "Modo Mirostat; 0 desativa.", llamaGroupPenalties),
		floatFlag("mirostat-lr", "", nil, 0.1, "Learning rate (eta) do Mirostat.", llamaGroupPenalties, nil, nil),
		floatFlag("mirostat-ent", "", nil, 5.0, "Entropia alvo (tau) do Mirostat.", llamaGroupPenalties, nil, nil),
		floatFlag("dynatemp-range", "", nil, 0.0, "Faixa de temperatura dinâmica; 0.0 desativa.", llamaGroupPenalties, floatPtr(0), nil),
		floatFlag("dynatemp-exp", "", nil, 1.0, "Expoente da temperatura dinâmica.", llamaGroupPenalties, nil, nil),
		floatFlag("adaptive-target", "", nil, nil, "Alvo do adaptive-p; valores negativos desativam.", llamaGroupPenalties, nil, floatPtr(1)),
		floatFlag("adaptive-decay", "", nil, nil, "Decaimento do adaptive-p.", llamaGroupPenalties, floatPtr(0), floatPtr(0.99)),

		strFlag("grammar", "", nil, "", "Restringe a saída por gramática GBNF.", llamaGroupGrammar, false),
		strFlag("grammar-file", "", nil, nil, "Lê gramática de arquivo.", llamaGroupGrammar, false),
		strFlag("json-schema", "j", nil, nil, "Restringe a saída a um JSON Schema.", llamaGroupGrammar, false),
		strFlag("json-schema-file", "jf", nil, nil, "Carrega JSON Schema de arquivo.", llamaGroupGrammar, false),

		strFlag("chat-template", "", nil, nil, "Define o template de chat (embutido ou customizado).", llamaGroupChat, false),
		strFlag("chat-template-file", "", nil, nil, "Lê o template de chat de um arquivo Jinja.", llamaGroupChat, false),
		strFlag("chat-template-kwargs", "", nil, nil, "Argumentos extras em JSON para o parser do template.", llamaGroupChat, false),
		boolFlag("jinja", "", []string{"no-jinja"}, false, "Liga/desliga o engine Jinja para chat.", llamaGroupChat),
		boolFlag("skip-chat-parsing", "", nil, false, "Força parser puro de conteúdo, sem parsing estrutural.", llamaGroupChat),
		boolFlag("prefill-assistant", "", []string{"no-prefill-assistant"}, true, "Controla o prefill da resposta quando a última mensagem já é do assistant.", llamaGroupChat),

		strFlag("host", "", nil, "127.0.0.1", "Endereço de escuta do servidor.", llamaGroupHTTP, false),
		portFlag("port", "", 8080, "Porta HTTP do servidor.", llamaGroupHTTP),
		boolFlag("reuse-port", "", nil, false, "Permite múltiplos sockets no mesmo porto.", llamaGroupHTTP),
		strFlag("api-prefix", "", nil, "", "Prefixo de rota da API (sem barra final).", llamaGroupHTTP, false),
		strFlag("path", "", nil, nil, "Diretório de arquivos estáticos a servir.", llamaGroupHTTP, false),
		boolFlag("ui", "", []string{"no-ui"}, true, "Liga/desliga a interface web.", llamaGroupHTTP),
		strFlag("ui-config", "", nil, nil, "Sobrescreve configurações padrão da UI.", llamaGroupHTTP, false),
		strFlag("ui-config-file", "", nil, nil, "Carrega configurações da UI de um arquivo JSON.", llamaGroupHTTP, false),
		strFlag("api-key", "", nil, nil, "Define chaves de autenticação da API.", llamaGroupHTTP, false),
		strFlag("api-key-file", "", nil, nil, "Lê chaves de autenticação de um arquivo.", llamaGroupHTTP, false),
		strFlag("ssl-key-file", "", nil, nil, "Chave privada SSL.", llamaGroupHTTP, false),
		strFlag("ssl-cert-file", "", nil, nil, "Certificado SSL.", llamaGroupHTTP, false),
		intFlag("timeout", "to", nil, 600, "Timeout de leitura/escrita (segundos).", llamaGroupHTTP, intPtr(0), nil),
		intFlag("threads-http", "", nil, -1, "Threads para requisições HTTP; -1 = automático.", llamaGroupHTTP, nil, nil),
		boolFlag("metrics", "", nil, false, "Endpoint de métricas compatível com Prometheus.", llamaGroupHTTP),
		boolFlag("slots", "", []string{"no-slots"}, true, "Expõe endpoint de monitoramento de slots.", llamaGroupHTTP),
		boolFlag("props", "", nil, false, "Permite alterar propriedades globais via POST /props.", llamaGroupHTTP),
		intFlag("parallel", "np", nil, 1, "Slots paralelos do servidor; -1 = automático.", llamaGroupHTTP, nil, nil),
		boolFlag("cont-batching", "cb", []string{"no-cont-batching"}, true, "Liga/desliga continuous batching.", llamaGroupHTTP),
		boolFlag("cache-prompt", "", []string{"no-cache-prompt"}, true, "Ativa reuso/cache de prompt.", llamaGroupHTTP),
		intFlag("cache-reuse", "", nil, 0, "Chunk mínimo para tentar reuso via KV shifting.", llamaGroupHTTP, intPtr(0), nil),
		strFlag("alias", "a", nil, nil, "Aliases de nome do modelo (API), separados por vírgula.", llamaGroupHTTP, false),

		strFlag("mmproj", "mm", nil, nil, "Projector multimodal.", llamaGroupMultimodal, false),
		strFlag("mmproj-url", "mmu", nil, nil, "URL do projector multimodal.", llamaGroupMultimodal, false),
		boolFlag("mmproj-auto", "", []string{"no-mmproj-auto", "no-mmproj"}, true, "Uso automático do projector multimodal.", llamaGroupMultimodal),
		boolFlag("mmproj-offload", "", []string{"no-mmproj-offload"}, true, "Offload do projector para GPU.", llamaGroupMultimodal),
		intFlag("image-min-tokens", "", nil, nil, "Mínimo de tokens por imagem.", llamaGroupMultimodal, intPtr(0), nil),
		intFlag("image-max-tokens", "", nil, nil, "Máximo de tokens por imagem.", llamaGroupMultimodal, intPtr(0), nil),

		boolFlag("embeddings", "", []string{"embedding"}, false, "Modo embeddings.", llamaGroupEmbeddings),
		enumFlag("pooling", "", nil, []string{"none", "mean", "cls", "last", "rank"}, nil, "Pooling de embeddings.", llamaGroupEmbeddings),
		intFlag("embd-normalize", "", nil, 2, "Normalização; 2 = euclidiana.", llamaGroupEmbeddings, nil, nil),
		boolFlag("reranking", "", []string{"rerank"}, false, "Endpoint de reranking.", llamaGroupEmbeddings),

		strFlag("lora", "", nil, nil, "Adapters LoRA.", llamaGroupLora, false),
		strFlag("lora-scaled", "", nil, nil, "LoRA com escala manual no formato ARQUIVO:SCALE.", llamaGroupLora, false),
		boolFlag("lora-init-without-apply", "", nil, false, "Carrega LoRA sem aplicar.", llamaGroupLora),
		strFlag("control-vector", "", nil, nil, "Control vector(s).", llamaGroupLora, false),
		strFlag("control-vector-scaled", "", nil, nil, "Control vector com escala no formato ARQUIVO:SCALE.", llamaGroupLora, false),
		strFlag("control-vector-layer-range", "", nil, nil, "Intervalo de camadas aplicado aos control vectors.", llamaGroupLora, false),

		enumFlag("spec-type", "", nil, []string{"none", "draft-simple", "draft-eagle3", "draft-mtp", "ngram-simple", "ngram-map-k", "ngram-map-k4v", "ngram-mod", "ngram-cache"}, "none", "Tipo de speculative decoding.", llamaGroupSpeculative),
		strFlag("spec-draft-model", "md", nil, nil, "Modelo draft.", llamaGroupSpeculative, false),
		strFlag("spec-draft-hf", "", nil, nil, "Repo HF do draft.", llamaGroupSpeculative, false),
		intFlag("spec-draft-n-max", "", nil, 16, "Máximo de tokens propostos pelo draft.", llamaGroupSpeculative, intPtr(0), nil),
		intFlag("spec-draft-n-min", "", nil, 0, "Mínimo de tokens draft.", llamaGroupSpeculative, intPtr(0), nil),
		floatFlag("spec-draft-p-split", "", nil, 0.1, "Probabilidade de split.", llamaGroupSpeculative, floatPtr(0), floatPtr(1)),
		floatFlag("spec-draft-p-min", "", nil, 0.75, "Probabilidade mínima do caminho guloso.", llamaGroupSpeculative, floatPtr(0), floatPtr(1)),
		strFlag("spec-draft-device", "", nil, nil, "Dispositivos do draft; por padrão acompanha --device.", llamaGroupSpeculative, false),
		strFlag("spec-draft-ngl", "", nil, 0, "Camadas do draft na VRAM; aceita inteiro, auto ou all.", llamaGroupSpeculative, false),
		intFlag("spec-draft-threads", "", nil, nil, "Threads de CPU do draft; por padrão acompanha --threads.", llamaGroupSpeculative, intPtr(0), nil),
		boolFlag("spec-draft-backend-sampling", "", []string{"no-spec-draft-backend-sampling"}, false, "Sampling do draft no backend.", llamaGroupSpeculative),
		intFlag("spec-ngram-mod-n-min", "", nil, nil, "Mínimo de tokens para speculative n-gram.", llamaGroupSpeculative, intPtr(0), nil),
		intFlag("spec-ngram-mod-n-max", "", nil, nil, "Máximo de tokens para speculative n-gram.", llamaGroupSpeculative, intPtr(0), nil),
		intFlag("spec-ngram-mod-n-match", "", nil, nil, "Comprimento de lookup para ngram-mod.", llamaGroupSpeculative, intPtr(0), nil),
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

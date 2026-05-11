# Plano de Implementação: Suporte ao vLLM

> **Status:** Draft
> **Data:** 2026-05-10
> **Autor:** pi (assisted)
> **Escopo:** Implementar suporte completo ao vLLM como backend alternativo ao llama-server

---

## 1. Contexto

### 1.1 Problema

O model-loader suporta apenas `llama-server` como backend de inferência. O sistema já foi arquitetado para ser genérico (com `BackendKind`, `BackendCatalog`, `Generator` interface), mas a implementação concreta está acoplada ao llama-server em várias camadas.

### 1.2 Objetivo

Adicionar suporte funcional ao vLLM como segundo backend, permitindo que o usuário:
- Adicione backends vLLM via TUI
- Crie perfis que usam vLLM com flags específicos
- Lance e monitore instâncias vLLM
- Receba métricas e health checks adequados

### 1.3 vLLM vs llama-server — Diferenças Relevantes

| Aspecto | llama-server | vLLM (`vllm serve`) |
|---------|-------------|---------------------|
| **Linguagem/Runtime** | C/C++ (binário nativo) | Python (PyTorch) |
| **Comando** | `llama-server --help` | `vllm serve --help` ou `python -m vllm.entrypoints.openai.api_server --help` |
| **Formato do --help** | Seções com `----- common params -----` + linhas com `-X, --flag N` | argparse Python: `optional arguments:` + `--flag TYPE` |
| **Model format** | GGUF only | HuggingFace, GGUF, safetensors, tensorizer |
| **Default port** | 8080 | 8000 |
| **Health endpoint** | `/health` (TCP 200) | `/health` (OpenAI-compatible), `/v1/models` |
| **Slots endpoint** | `/slots` (JSON array) | Não existe equivalente direto |
| **Log format** | `X tokens per second` | JSON logs ou Python logging |
| **GPU memory control** | `-ngl` (layers offloaded) | `--gpu-memory-utilization` (fraction) |
| **Context size** | `-c, --ctx-size` | `--max-model-len` |
| **Process management** | Detached via Setsid, mesmo pattern | Mesmo pattern (foreground process) |

### 1.4 O que JÁ está pronto (não precisa de mudanças)

- ✅ `domain.BackendKindVLLM` já definido em `backend.go`
- ✅ `BackendCatalog` genérico — suporta múltiplos backends
- ✅ `BackendValidationSchema` com campo `BackendKind`
- ✅ `backendschema.Manager.Register(kind, Generator)` — sistema extensível
- ✅ `backendcatalog.Resolver` — genérico, resolve qualquer backend
- ✅ `validator.Validator` — validação de tipos genérica (usa schema)
- ✅ `processmgr.fsManager` — spawn genérico (usa resolvedBinary + BuildArgs)
- ✅ `monitor.GPUPoller` — genérico (usa nvidia-smi)
- ✅ `models.Scanner` — GGUF scanning genérico

---

## 2. Análise de Gaps

### 2.1 Matriz de Acoplamento llama-server → vLLM

| Camada | Arquivo | Acoplamento | Ação Necessária |
|--------|---------|-------------|-----------------|
| **Generator** | `backendschema/generator.go` | `LlamaServerGenerator` só aceita `BackendKindLlamaServer` | Criar `VLLMGenerator` |
| **Help Parser** | `llamahelp/parser.go` | Regex assume formato llama.cpp (`----- X params -----`) | Criar `vLLMHelpParser` ou adaptar |
| **Exec Parser** | `llamahelp/exec_parser.go` | Invoca `{binary} --help` e `{binary} --version` | Criar `VLLMExecParser` com invocação correta |
| **Embedded Schema** | `llamahelp/embedded.go` | Schema hardcoded do llama-server v7376 | Criar `embedded_vllm.go` com fallback |
| **Args Builder** | `processmgr/args.go` | `shortToLong` map com `"ngl" → "n-gpu-layers"` | Tornar genérico ou vLLM-aware |
| **Health Check** | `processmgr/manager.go` | Hardcoded `GET /health` | Tornar configurável por backend |
| **Slots Poller** | `monitor/slots.go` | Polls `GET /slots` (llama.cpp only) | Fallback graceful ou vLLM equivalent |
| **Metrics** | `monitor/metrics.go` | Regex `X tokens per second` (llama.cpp log format) | vLLM log parser ou métricas via API |
| **Profile Editor** | `profile_editor/draft.go` | `Draft` com campos hardcoded (NGL, CtxSize, FlashAttn) | Tornar schema-driven |
| **Profile Detail** | `profiles.go` | Display hardcoded: `ngl=%v ctx=%v flash-attn=%v` | Display genérico |
| **Config** | `config/config.go` | `LlamaServerBinaryPath` no PathsConfig | Adicionar `DefaultVLLMBinaryPath` ou generalizar |
| **Migration** | `migration/migration.go` | Sempre cria backends `Kind=llama-server` | Detectar kind do executable |
| **Main Bootstrap** | `main.go` | Só regista `BackendKindLlamaServer` | Registar `VLLMGenerator` |
| **Boot Blocker** | `root.go` | Mensagem hardcoded sobre `llama-server` | Tornar genérica |
| **Backends UI** | `backends.go` | Default kind hardcoded para `llama-server` | OK (baixa prioridade) |

---

## 3. Plano de Implementação

### Fase 1: Infraestrutura Core — vLLM Generator & Schema

**Objetivo:** Permitir adicionar backends vLLM e gerar seus schemas automaticamente.

**Prioridade:** Alta — sem isto, nada mais funciona.

#### 3.1.1 Criar `VLLMExecParser`

**Arquivo novo:** `internal/service/llamahelp/vllm_exec_parser.go`

vLLM usa Python, então a invocação `--help` é diferente:
```bash
# Opção A: vllm CLI instalado
vllm serve --help

# Opção B: módulo Python
python -m vllm.entrypoints.openai.api_server --help

# Opção C: caminho customizado
/opt/vllm/venv/bin/python -m vllm.entrypoints.openai.api_server --help
```

**Design:**

```go
// VLLMExecParser invokes vLLM to capture --help and --version.
type VLLMExecParser struct {
    binary string  // e.g. "python" or "/opt/venv/bin/python"
    module string  // e.g. "vllm.entrypoints.openai.api_server" or ""
}

// NewVLLMExecParser creates a parser. If module is empty, assumes `vllm serve`.
func NewVLLMExecParser(binary, module string) *VLLMExecParser

// Parse invokes the vLLM command and returns FlagSchema.
func (p *VLLMExecParser) Parse(ctx context.Context) (domain.FlagSchema, error)

// DetectVersion invokes `vllm --version` or `pip show vllm` etc.
func (p *VLLMExecParser) DetectVersion(ctx context.Context) (string, error)
```

**Detalhes de implementação:**

- Quando `module != ""`: executa `{binary} -m {module} --help`
- Quando `module == ""` e binary = "vllm": executa `{binary} serve --help`
- stdout + stderr combinados (vLLM pode escrever help no stderr)
- Timeout de 15s (vLLM é Python, pode ser mais lento que C++)
- `DetectVersion` tenta:
  1. `{binary} --version` (se binary = "vllm")
  2. `{binary} -c "import vllm; print(vllm.__version__)"` (se binary = "python")
  3. Fallback: `"unknown"`

**Testes:**
- `vllm_exec_parser_test.go` com golden file do help output real do vLLM
- Teste com binary não encontrado → erro adequado
- Teste timeout

#### 3.1.2 Criar `vLLMHelpParser`

**Arquivo novo:** `internal/service/llamahelp/vllm_parser.go`

O formato do `--help` do vLLM é argparse do Python, que é completamente diferente do formato do llama.cpp.

**Formato argparse típico:**
```
usage: vllm serve [-h] [--model MODEL] [--dtype {auto,fp16,...}] ...

optional arguments:
  -h, --help            show this help message and exit
  --model MODEL         HuggingFace model ID or path (default: facebook/opt-125m)

engine arguments:
  --dtype {auto,half,float16,bfloat16,float,float32}
                        Data type to use for operations (default: auto)
  --tensor-parallel-size TENSOR_PARALLEL_SIZE
                        Number of tensor parallel replicas (default: 1)
  --max-model-len MAX_MODEL_LEN
                        Model context length (default: auto)
```

**Regex strategy:**

```go
// Sections delimitadas por linhas em maiúsculas terminando em ":"
var sectionHeaderRe = regexp.MustCompile(`^([A-Za-z ]+):\s*$`)

// Flags começam com -- ou -
var flagLineRe = regexp.MustCompile(`^ {2,}(-{1,2}\S+)\s+(.*)$`)

// Default extraction: "(default: X)" ou "(default X)"
var defaultRe = regexp.MustCompile(`\(default:\s*([^)]+)\)`)
```

**Type inference para vLLM:**

vLLM usa TYPE placeholders explícitos:
```
--dtype {auto,fp16,bf16}     → Enum (detected by {a,b,c} pattern)
--tensor-parallel-size N     → Int (placeholder is single uppercase word)
--gpu-memory-utilization F   → Float (placeholder starts with F or RATE)
--model MODEL                → String
--enforce-eager              → Bool (no placeholder)
```

**Mapeamento de placeholders para tipos:**
```go
func inferVLLMType(placeholder string) domain.FlagType {
    switch {
    case placeholder == "":
        return domain.FlagTypeBool
    case isEnumPlaceholder(placeholder):
        return domain.FlagTypeEnum
    }
    // vLLM uses descriptive uppercase placeholders
    switch {
    case placeholder == "N", placeholder == "PORT",
         strings.Contains(placeholder, "SIZE"),
         strings.Contains(placeholder, "COUNT"),
         strings.Contains(placeholder, "NUM"),
         strings.Contains(placeholder, "WORKERS"):
        return domain.FlagTypeInt
    case placeholder == "F", placeholder == "RATE",
         strings.Contains(placeholder, "FRACTION"),
         strings.Contains(placeholder, "RATIO"):
        return domain.FlagTypeFloat
    }
    return domain.FlagTypeString
}
```

**Detecção de enums:**
- `{a,b,c}` pattern → enum values
- `[a|b|c]` pattern → enum values (menos comum no argparse)

**Hardcoded overrides para vLLM:**
Certos flags do vLLM podem precisar de overrides hardcoded, similar ao `hardcodedFlagOverrides` do llama-server.

```go
func vllmHardcodedOverrides(spec domain.FlagSpec) domain.FlagSpec {
    switch spec.Long {
    case "dtype":
        // Ensure enum values if not captured
        if spec.Type != domain.FlagTypeEnum || len(spec.EnumValues) == 0 {
            spec.Type = domain.FlagTypeEnum
            spec.EnumValues = []string{"auto", "half", "float16", "bfloat16", "float", "float32"}
        }
    case "quantization":
        if spec.Type != domain.FlagTypeEnum || len(spec.EnumValues) == 0 {
            spec.Type = domain.FlagTypeEnum
            spec.EnumValues = []string{
                "None", "awq", "gptq", "gguf", "bitsandbytes", "fp8",
                "marlin", "compressed-tensors", "quark", "aqlm",
            }
        }
    }
    return spec
}
```

**Testes:**
- `vllm_parser_test.go` com golden file do vLLM --help output
- Testes de type inference com diferentes placeholders
- Testes de enum detection
- Testes de hardcoded overrides

#### 3.1.3 Criar `VLLMGenerator`

**Arquivo novo:** `internal/service/backendschema/vllm_generator.go`

```go
type VLLMGenerator struct {
    schemaStore backendcatalog.SchemaStore
}

func NewVLLMGenerator(schemaStore backendcatalog.SchemaStore) *VLLMGenerator

func (g *VLLMGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error)
```

**Diferenças do `LlamaServerGenerator`:**
1. Verifica `backend.Kind == domain.BackendKindVLLM` ao invés de `LlamaServer`
2. Usa `VLLMExecParser` ao invés de `ExecParser`
3. Usa `vLLMHelpParser` ao invés de `ParseHelp`
4. O executable resolution via `llamabin.Resolve` funciona assim que o caminho é válido
5. Schema persistence é idêntico (mesmo `schemaStore`)

**Tratamento especial do executable:**

O executable do vLLM pode ser:
- Um caminho para Python (`/usr/bin/python3`)
- Um comando (`vllm`)
- Um módulo (`python -m vllm.entrypoints.openai.api_server`)

Problema: `llamabin.Resolve()` espera um único binário. Para módulo Python, precisamos de lógica especial.

**Solução:** Adicionar campo `Properties` no `Backend` struct para metadata adicional:

```go
// Backend.Properties já existe! Usar para vLLM:
backend.Properties = map[string]string{
    "module": "vllm.entrypoints.openai.api_server",
}
```

O generator lê `Properties["module"]` para construir o comando correto.

**Testes:**
- `vllm_generator_test.go` com mock schemaStore
- Teste com backend.Kind != vllm → erro
- Teste com editable schema → skip generation
- Teste com binary não encontrado → erro

#### 3.1.4 Criar Embedded Fallback Schema para vLLM

**Arquivo novo:** `internal/service/llamahelp/embedded_vllm.go`

```go
func EmbeddedVLLMSchema() domain.FlagSchema {
    // Flags essenciais do vLLM serve:
    // - model (string, required)
    // - dtype (enum)
    // - tensor-parallel-size (int, default: 1)
    // - pipeline-parallel-size (int, default: 1)
    // - max-model-len (int, default: auto)
    // - gpu-memory-utilization (float, default: 0.9)
    // - swap-space (int, default: 4)
    // - max-num-seqs (int, auto)
    // - max-num-batched-tokens (int, auto)
    // - quantization (enum)
    // - load-format (enum)
    // - host (string, default: 0.0.0.0)
    // - port (int, default: 8000)
    // - api-key (string)
    // - enable-prefix-caching (bool)
    // - enforce-eager (bool)
    // - chat-template (string)
    // - trust-remote-code (bool)
    // - hf-token (string)
    // - download-dir (string)
    // - device (enum: auto, cuda, cpu)
}
```

**Pinos à versão do vLLM:**
- Definir `SchemaVersion` baseado na versão do vLLM utilizada
- Documentar a versão em comentário no código

**Função para escrever fallback:**

```go
func WriteVLLMEmbeddedFallback(schemaStore backendcatalog.SchemaStore, backendID, schemaRef string) error
```

Similar a `WriteEmbeddedFallback` mas usa `EmbeddedVLLMSchema()`.

#### 3.1.5 Registrar VLLMGenerator no Bootstrap

**Arquivo modificado:** `cmd/model-loader/main.go`

```diff
  schemaManager.Register(domain.BackendKindLlamaServer,
      backendschema.NewLlamaServerGenerator(schemaStore))
+ schemaManager.Register(domain.BackendKindVLLM,
+     backendschema.NewVLLMGenerator(schemaStore))
```

**Testes:**
- Atualizar `main_test.go` (se existir) ou tests existentes para incluir vLLM

---

### Fase 2: Process Manager — Health Check & Metrics Backend-Aware

**Objetivo:** Health check e métricas funcionem corretamente para vLLM.

**Prioridade:** Alta — sem health check, o launcher não sabe quando o servidor está pronto.

#### 3.2.1 Tornar Health Check Configurável

**Arquivos modificados:**
- `internal/service/processmgr/manager.go` (WaitHealthy)
- `internal/domain/backend.go` (adicionar health endpoint)
- `internal/service/monitor/slots.go` (fetchHealth)

**Problema:** `WaitHealthy()` faz `GET /health` hardcoded. vLLM também tem `/health`, mas a resposta pode diferir. Além disso, futuros backends podem usar endpoints diferentes.

**Solução:** Adicionar campo de health endpoint no `Backend` ou `BackendValidationSchema`.

**Opção A (mais limpa):** Adicionar `HealthEndpoint` ao `Backend`:

```go
type Backend struct {
    // ... campos existentes ...
    HealthEndpoint string `json:"healthEndpoint,omitempty"` // "/health", "/v1/models", etc.
}
```

- Default: `"/health"` (funciona para llama-server e vLLM)
- Configuração explícita para backends futuros

**Opção B (mais flexível):** Usar `Backend.Properties`:

```go
backend.Properties["health_endpoint"] = "/health"
```

**Decisão:** Opção A é mais direta e self-documenting.

**Changes em `manager.go:WaitHealthy`:**

```go
func (m *fsManager) WaitHealthy(pid int, port int, timeout time.Duration, healthEndpoint string) error {
    if healthEndpoint == "" {
        healthEndpoint = "/health"
    }
    url := fmt.Sprintf("http://127.0.0.1:%d%s", port, healthEndpoint)
    // ... resto igual ...
}
```

**Problema de assinatura:** Mudar a assinatura de `WaitHealthy` quebra callers existentes.

**Solução alternativa (backward-compatible):** Ler health endpoint do schema ou do backend catalog dentro de `WaitHealthy`. Mas isso acopla processmgr ao catalog.

**Melhor solução:** Adicionar `HealthEndpoint` ao `domain.RunningInstance` e passá-lo no momento do Launch.

```go
type RunningInstance struct {
    // ... campos existentes ...
    HealthEndpoint string `json:"healthEndpoint,omitempty"`
}
```

Durante `Launch()`:
```go
inst := domain.RunningInstance{
    // ...
    HealthEndpoint: healthEndpointFromBackend(backend),
}
```

`WaitHealthy` usa `inst.HealthEndpoint`.

#### 3.2.2 vLLM Health Check

vLLM expõe `/health` que retorna JSON:
```json
{
    "message": "ok",
    "id": "xxx",
    "timestamp": 123456,
    "metrics": { ... }
}
```

Também pode usar `/v1/models` (OpenAI-compatible API):
```json
{
    "object": "list",
    "data": [{"id": "model_name", ...}]
}
```

**Decisão:** Usar `/health` como padrão para ambos backends. Se `/health` não responder 200, fallback para `/v1/models`.

```go
func (m *fsManager) WaitHealthy(pid int, port int, timeout time.Duration, endpoints []string) error {
    if len(endpoints) == 0 {
        endpoints = []string{"/health"}
    }
    // Tentar cada endpoint em sequência
    for _, ep := range endpoints {
        if err := m.checkEndpoint(pid, port, timeout, ep); err == nil {
            return nil
        }
    }
    return fmt.Errorf("port %d: %w", port, ErrHealthCheckTimeout)
}
```

#### 3.2.3 Métricas — vLLM Metrics Poller

**Arquivo modificado:** `internal/service/monitor/metrics.go`

**Problema:** O regex `X tokens per second` funciona apenas para logs do llama.cpp. vLLM usa JSON logging ou Python logging padrão.

**Solução:** Duas abordagens complementares:

**Opção A: Métricas via API (preferida para vLLM)**

vLLM expõe métricas Prometheus em `/metrics`:
```
# HELP vllm:prompt_tokens_total Total prompt tokens processed
# TYPE vllm:prompt_tokens_total counter
vllm:prompt_tokens_total 1234
# HELP vllm:generation_tokens_total Total generation tokens processed
vllm:generation_tokens_total 5678
```

**Nova funcionalidade:** Adicionar `metricsPoller` opcional que faz `GET /metrics` e parse de métricas Prometheus.

```go
type metricsAPIPoller struct {
    baseURL  string
    interval time.Duration
    out      chan<- MonitorEvent
}

func (p *metricsAPIPoller) run(ctx context.Context) {
    // Poll GET /metrics, parse Prometheus text format
    // Extract tokens/s from counter deltas
}
```

**Opção B: Log regex para vLLM**

vLLM também emite logs com informações de throughput. Regex adicional para capturar:
```
INFO: ... processed xxx tokens ...
```

**Decisão:** Implementar Opção A (API-based metrics) para vLLM. Manter regex-based para llama.cpp.

**Estrutura de dados:**

Adicionar `MetricsSource` ao `Backend`:
```go
type Backend struct {
    // ...
    MetricsSource string `json:"metricsSource,omitempty"` // "log-regex", "prometheus", "none"
}
```

No `Subscribe` do monitor, escolher o poller baseado no `MetricsSource` do backend.

**Implementation details:**

```go
// New in metrics.go or new file metrics_api.go
type prometheusPoller struct {
    baseURL     string
    interval    time.Duration
    out         chan<- MonitorEvent
    lastTokens  float64
    lastTime    time.Time
}

func (p *prometheusPoller) run(ctx context.Context) {
    tick := time.NewTicker(p.interval)
    for {
        select {
        case <-ctx.Done():
            return
        case <-tick.C:
            p.pollOnce(ctx)
        }
    }
}

func (p *prometheusPoller) pollOnce(ctx context.Context) {
    resp, err := p.client.Get(p.baseURL + "/metrics")
    if err != nil {
        return
    }
    defer resp.Body.Close()

    // Parse Prometheus text format
    tokens := extractPrometheusMetric(resp.Body, "vllm:generation_tokens_total")
    now := time.Now()
    if !p.lastTime.IsZero() {
        dt := now.Sub(p.lastTime).Seconds()
        if dt > 0 && tokens > p.lastTokens {
            tokensPerSec := (tokens - p.lastTokens) / dt
            p.emit(MonitorEvent{
                Timestamp: now,
                Source:    SourceMetrics,
                Data:      Metrics{TokensPerSec: []float64{tokensPerSec}},
            })
        }
    }
    p.lastTokens = tokens
    p.lastTime = now
}
```

#### 3.2.4 Slots Poller — Graceful Fallback

**Arquivo modificado:** `internal/service/monitor/slots.go`

**Problema:** `GET /slots` é específico do llama.cpp. vLLM não tem equivalente.

**Solução:** O slots poller já trata erro silenciosamente (`if err != nil { return }`). Ele não quebra se `/slots` não existir.

**Ação:** Apenas garantir que o fallback é documentado e não gera warnings no log.

**Melhoria:** Adicionar `/v1/models` como alternativa para verificar se o modelo está carregado (vLLM equivalente ao slots check).

```go
func (p *slotsPoller) fetchSlots(ctx context.Context) {
    // Tentar /slots primeiro (llama.cpp)
    slots := p.fetchSlotsEndpoint(ctx, "/slots")
    if len(slots) == 0 {
        // Fallback: /v1/models (OpenAI-compatible, vLLM)
        slots = p.fetchV1Models(ctx)
    }
    if len(slots) > 0 {
        p.emit(MonitorEvent{Source: SourceSlots, Data: SlotSnapshot{Slots: slots}})
    }
}
```

#### 3.2.5 Args Builder — Genérico

**Arquivo modificado:** `internal/service/processmgr/args.go`

**Problema:** `shortToLong` mapeia `"ngl"` → `"n-gpu-layers"` — específico do llama.cpp.

**Solução:** Tornar o canonical map configurável por backend.

**Opção A:** Mover `shortToLong` para o schema (adicionar campo `CanonicalAlias` em `FlagSpec`).

**Opção B:** Adicionar `Properties["canonical_flags"]` ao `Backend` com JSON de mapeamentos.

**Opção C (mais simples):** Tornar `BuildArgs` genérico removendo `shortToLong` do processmgr e movendo a lógica para o profile editor (que já tem acesso ao schema).

**Decisão:** Opção C. O `shortToLong` existe porque o UI editor usa `"ngl"` como chave mas llama-server precisa de `"n-gpu-layers"`. Com schema-driven forms, as chaves virão diretamente do schema (que já tem `Long`, `Short`, `Aliases`). Remover `shortToLong` de `processmgr/args.go` e garantir que `BuildArgs` usa as chaves diretamente.

**Changes:**
```go
// Remover shortToLong map e canonicalFlag function de args.go
// BuildArgs já itera sobre p.Args keys — se as chaves já estão no formato
// correto (do schema), não precisa de canonical mapping.
```

**Impacto:** Perfis existentes usam `"ngl"` como chave. Após esta mudança, perfis novos usarão `"n-gpu-layers"`. **Necessita migration.**

**Migration plan:**
```go
func migrateArgKeys(profile *domain.Profile, schema domain.FlagSchema) {
    newArgs := make(map[string]any)
    for key, value := range profile.Args {
        canonical := canonicalKey(key, schema)
        newArgs[canonical] = value
    }
    profile.Args = newArgs
}

func canonicalKey(key string, schema domain.FlagSchema) string {
    if spec, ok := schema.Lookup(key); ok {
        return spec.Long  // usar o nome longo canônico
    }
    return key  // fallback: manter original
}
```

Esta migration roda no startup do app (antes de qualquer launch).

---

### Fase 3: Profile Editor — Schema-Driven Form

**Objetivo:** O editor de perfis gera campos dinamicamente a partir do schema do backend selecionado.

**Prioridade:** Alta — sem isto, o usuário não pode configurar flags específicos do vLLM.

#### 3.3.1 Refatorar `Draft` para ser Schema-Driven

**Arquivo modificado:** `internal/ui/pages/profile_editor/draft.go`

**Problema:** `Draft` tem campos hardcoded (`NGL`, `CtxSize`, `BatchSize`, `FlashAttn`, `CacheTypeK`, `CacheTypeV`). Estes existem apenas para llama.cpp.

**Solução:** Substituir os campos estáticos por um `Args` map populado dinamicamente a partir do schema.

**Nova estrutura:**

```go
type Draft struct {
    ID          string
    Name        string
    Description string
    Model       string
    BackendID   string
    Port        string  // Mantido separado por ser universal e obrigatório
    IsNew       bool

    // Novo: args populados dinamicamente do schema
    EssentialArgs map[string]*EssentialArg  // chave = flag long name
}

type EssentialArg struct {
    Value     string
    Spec      domain.FlagSpec  // metadados do flag (tipo, enum values, default)
    Required  bool
    Visibility string  // "essential" para mostrar no Essentials tab
}
```

**Como funciona:**

1. Quando o usuário seleciona um backend, o editor carrega o schema desse backend
2. O schema determina quais flags são "essenciais" (via uma lista curada ou via `Group == "common"`)
3. O form é gerado dinamicamente com base nos EssentialArgs
4. `ToProfile()` constrói `Args` a partir do estado do form

**Seleção de campos essenciais:**

```go
func extractEssentialFlags(schema domain.FlagSchema) map[string]*EssentialArg {
    essentials := map[string]*EssentialArg{
        "port": {Required: true, Visibility: "essential"},  // sempre essencial
    }

    // Flags do grupo "common" ou "engine" são essenciais
    for name, spec := range schema.Flags {
        if spec.Group == "common" || spec.Group == "engine arguments" {
            // Limitar ao top N mais relevantes
            if len(essentials) >= 12 {
                break
            }
            essentials[name] = &EssentialArg{Spec: spec, Visibility: "essential"}
        }
    }

    // Flags prioritários para vLLM
    for _, priorityFlag := range []string{
        "model", "dtype", "tensor-parallel-size", "gpu-memory-utilization",
        "max-model-len", "quantization", "host",
    } {
        if spec, ok := schema.Flags[priorityFlag]; ok {
            essentials[priorityFlag] = &EssentialArg{Spec: spec, Visibility: "essential"}
        }
    }

    return essentials
}
```

**Alternativa mais limpa:** Adicionar campo `Priority` em `FlagSpec`:

```go
type FlagSpec struct {
    // ... campos existentes ...
    Priority int `json:"priority,omitempty"` // 1=essential, 2=important, 3=optional, 4=advanced
}
```

Flags com Priority ≤ 2 vão para o Essentials tab, Priority 3+ para Advanced.

**Problema:** O schema é gerado automaticamente de `--help`. Não temos `Priority` no help output.

**Solução:** Manter uma lista hardcoded de flags essenciais por backend kind:

```go
var llamaServerEssentials = []string{
    "n-gpu-layers", "ctx-size", "batch-size", "port",
    "flash-attn", "cache-type-k", "cache-type-v",
}

var vllmEssentials = []string{
    "dtype", "tensor-parallel-size", "gpu-memory-utilization",
    "max-model-len", "quantization", "port",
}

func getEssentialFlags(kind domain.BackendKind, schema domain.FlagSchema) map[string]*EssentialArg {
    var priorityList []string
    switch kind {
    case domain.BackendKindVLLM:
        priorityList = vllmEssentials
    default:
        priorityList = llamaServerEssentials
    }
    // ... selecionar flags do schema que estão na priority list ...
}
```

#### 3.3.2 Gerar Form Dinamicamente

**Arquivo modificado:** `internal/ui/pages/profile_editor/draft.go` (buildForm)

**Problema:** `buildForm()` cria campos hardcoded para `NGL`, `CtxSize`, etc.

**Solução:** Gerar campos a partir do `EssentialArgs` map.

```go
func buildForm(d *Draft, schema domain.FlagSchema, backendOpts []huh.Option[string], kind domain.BackendKind) *huh.Form {
    // Grupo 1: Metadata
    group1 := huh.NewGroup(
        huh.NewInput().Title("Name").Value(&d.Name),
        huh.NewInput().Title("Description").Value(&d.Description),
        huh.NewInput().Title("Model path").Value(&d.Model),
    )
    if len(backendOpts) > 0 {
        group1 = huh.NewGroup(
            // ... + Backend select
        )
    }

    // Grupo 2: Flags essenciais (dinâmico)
    var flagFields []huh.Field
    for name, arg := range d.EssentialArgs {
        field := createFieldFromSpec(name, arg)
        flagFields = append(flagFields, field)
    }

    group2 := huh.NewGroup(flagFields...)
    return huh.NewForm(group1, group2).WithShowHelp(true)
}

func createFieldFromSpec(name string, arg *EssentialArg) huh.Field {
    spec := arg.Spec

    // Skip model flag (already in group 1) and port (separate)
    if spec.Long == "model" {
        return nil
    }

    switch spec.Type {
    case domain.FlagTypeInt, domain.FlagTypeFloat:
        return huh.NewInput().
            Title(specTitle(spec)).
            Description(spec.HelpText).
            Value(&arg.Value).
            Validate(typeValidator(spec.Type))

    case domain.FlagTypeEnum:
        options := make([]huh.Option[string], 0, len(spec.EnumValues))
        for _, v := range spec.EnumValues {
            options = append(options, huh.NewOption(v, v))
        }
        return huh.NewSelect[string]().
            Title(specTitle(spec)).
            Description(spec.HelpText).
            Options(options...).
            Value(&arg.Value)

    case domain.FlagTypeBool:
        // Bool como select: on/off/auto
        return huh.NewSelect[string]().
            Title(specTitle(spec)).
            Description(spec.HelpText).
            Options(huh.NewOption("on", "on"), huh.NewOption("off", "off")).
            Value(&arg.Value)

    case domain.FlagTypeString:
        return huh.NewInput().
            Title(specTitle(spec)).
            Description(spec.HelpText).
            Value(&arg.Value)
    }
    return nil
}

func specTitle(spec domain.FlagSpec) string {
    title := spec.Long
    if spec.Short != "" {
        title = fmt.Sprintf("%s (-%s)", title, spec.Short)
    }
    if spec.Default != nil {
        title = fmt.Sprintf("%s (default: %v)", title, spec.Default)
    }
    return title
}
```

#### 3.3.3 Refatorar `ToProfile()` e `ApplyTo()`

**Arquivo modificado:** `internal/ui/pages/profile_editor/draft.go`

`ToProfile()` precisa construir `Args` a partir dos `EssentialArgs` dinâmicos:

```go
func (d Draft) ApplyTo(base domain.Profile) domain.Profile {
    args := make(map[string]any)

    // Port é sempre presente
    if port, err := strconv.Atoi(d.Port); err == nil && port > 0 {
        args["port"] = float64(port)
    }

    // Flags essenciais dinâmicos
    for name, arg := range d.EssentialArgs {
        if arg.Value == "" {
            continue  // Skip empty values (use backend default)
        }
        args[name] = coerceArgValue(arg.Value, arg.Spec.Type)
    }

    out := base
    out.ID = d.ID
    out.Name = d.Name
    out.Description = d.Description
    out.Model = d.Model
    out.Args = args
    out.Launch.DefaultBackground = true
    out.Launch.BackendID = d.BackendID
    return out
}

func coerceArgValue(value string, ft domain.FlagType) any {
    switch ft {
    case domain.FlagTypeInt:
        if v, err := strconv.Atoi(value); err == nil {
            return float64(v)  // JSON numbers are float64
        }
    case domain.FlagTypeFloat:
        if v, err := strconv.ParseFloat(value, 64); err == nil {
            return v
        }
    case domain.FlagTypeEnum, domain.FlagTypeString:
        return value
    case domain.FlagTypeBool:
        return value == "on" || value == "true" || value == "1"
    }
    return value  // fallback: string
}
```

#### 3.3.4 Editor Integration

**Arquivo modificado:** `internal/ui/pages/profile_editor/editor.go`

O Editor precisa:
1. Carregar schema quando backend muda
2. Recalcular EssentialArgs baseado no novo schema
3. Reconstruir o form

```go
func (e *Editor) Open(d Draft) (Editor, tea.Cmd) {
    e = e.loadSchemaForDraft()
    e.updateEssentialArgs()  // Novo: recalcular args essenciais
    e.form = buildForm(&e.draft, e.schema, e.backendOptions, e.backendKind)
    return e, nil
}

func (e *Editor) updateEssentialArgs() {
    if e.draft.EssentialArgs == nil {
        e.draft.EssentialArgs = make(map[string]*EssentialArg)
    }

    kind := e.backendKind
    essentials := getEssentialFlags(kind, e.schema)

    // Preservar valores existentes
    for name, existing := range e.draft.EssentialArgs {
        if newArg, ok := essentials[name]; ok {
            newArg.Value = existing.Value
            essentials[name] = newArg
        }
    }
    e.draft.EssentialArgs = essentials
}
```

#### 3.3.5 Profile Detail View — Display Genérico

**Arquivo modificado:** `internal/ui/pages/profiles.go`

**Problema:** Detail view hardcodes `ngl=%v ctx=%v flash-attn=%v`.

**Solução:** Iterar sobre `profile.Args` e mostrar valores não-default usando o schema.

```go
func (p ProfilesPage) detailView(profile domain.Profile) string {
    backend := profile.Launch.BackendID
    if backend == "" {
        backend = "(default)"
    }

    // Construir args display dinâmico
    var argsLines []string
    for key, value := range profile.Args {
        // Skip zero/default values
        if isDefaultValue(key, value, p.defaultSchema) {
            continue
        }
        argsLines = append(argsLines, fmt.Sprintf("  %s: %v", key, value))
    }
    sort.Strings(argsLines)

    argsDisplay := "  (none)"
    if len(argsLines) > 0 {
        argsDisplay = strings.Join(argsLines, "\n")
    }

    return fmt.Sprintf(
        "Name:     %s\nBackend: %s\nModel:   %s\nArgs:\n%s",
        profile.Name, backend, profile.Model, argsDisplay,
    )
}
```

---

### Fase 4: Config & Migration

**Objetivo:** Configuração e migration suportam vLLM backends.

**Prioridade:** Média — necessário para boa UX mas não bloqueia funcionalidade core.

#### 3.4.1 Config — Adicionar Default VLLM Binary Path

**Arquivo modificado:** `internal/config/config.go`

```diff
 type PathsConfig struct {
     ProfilesDir          string `mapstructure:"profiles_dir"`
     LogDir               string `mapstructure:"log_dir"`
     StateDir             string `mapstructure:"state_dir"`
     BackendsDir          string `mapstructure:"backends_dir"`
     LlamaServerBinaryPath string `mapstructure:"llama_server_binary_path"`
+    DefaultVLLMBinaryPath string `mapstructure:"default_vllm_binary_path"`
 }
```

**Default value:**
```go
v.SetDefault("paths.default_vllm_binary_path", "vllm")
```

**Tilda expansion:**
```go
cfg.Paths.DefaultVLLMBinaryPath = expandTilde(cfg.Paths.DefaultVLLMBinaryPath)
```

#### 3.4.2 Default Catalog — Incluir vLLM

**Arquivo modificado:** `internal/service/backendcatalog/default.go`

`DefaultCatalog(executable)` atualmente cria apenas um backend llama-server. Adicionar opção para incluir vLLM:

```go
func DefaultCatalog(llamaServerBinary string) BackendCatalog {
    return BackendCatalog{
        SchemaVersion:    1,
        DefaultBackendID: "llama-cpp-default",
        Backends: []Backend{
            {
                ID:         "llama-cpp-default",
                Name:       "llama.cpp default",
                Kind:       BackendKindLlamaServer,
                Executable: llamaServerBinary,
                SchemaRef:  "schemas/llama-cpp-default.json",
                // ...
            },
            // vLLM backend — adicionado apenas se vllm estiver em PATH
            // (detectado em runtime)
        },
    }
}
```

**Detecção em runtime:** No bootstrap (`main.go`), verificar se `vllm` está em PATH. Se sim, adicionar ao catálogo.

```go
func maybeAddVLLMToCatalog(catalog *domain.BackendCatalog, vllmBinary string, schemaStore SchemaStore) {
    if _, err := exec.LookPath("vllm"); err != nil {
        // vllm não encontrado — não adicionar
        return
    }

    vllmBackend := domain.Backend{
        ID:         "vllm-default",
        Name:       "vLLM default",
        Kind:       domain.BackendKindVLLM,
        Executable: "vllm",
        SchemaRef:  "schemas/vllm-default.json",
        HealthEndpoint: "/health",
        MetricsSource: "prometheus",
    }
    catalog.Backends = append(catalog.Backends, vllmBackend)

    // Escrever embedded fallback para vLLM
    backendschema.WriteVLLMEmbeddedFallback(schemaStore, vllmBackend.ID, vllmBackend.SchemaRef)
}
```

#### 3.4.3 Migration — Detectar vLLM

**Arquivo modificado:** `internal/service/migration/migration.go`

**Problema:** Migration sempre cria backends com `Kind=llama-server`.

**Solução:** Heurística para detectar o kind do executable:

```go
func detectBackendKind(executable string) domain.BackendKind {
    lower := strings.ToLower(executable)
    if strings.Contains(lower, "vllm") || strings.Contains(lower, "vll") {
        return domain.BackendKindVLLM
    }
    if strings.Contains(lower, "tabby") {
        return domain.BackendKindTabbyAPI
    }
    if strings.Contains(lower, "sglang") {
        return domain.BackendKindSGLang
    }
    return domain.BackendKindLlamaServer  // default
}
```

**Impacto na migration:**
```go
// Em migration.go, quando cria backend para executable desconhecido:
kind := detectBackendKind(executable)
backend := domain.Backend{
    // ...
    Kind: kind,
}
```

#### 3.4.4 Migration — Canonicalizar Chaves de Args

**Arquivo novo:** `internal/service/migration/arg_keys.go`

Migration para perfis existentes que usam chaves curtas (`ngl`, `ctx`) → chaves longas canônicas (`n-gpu-layers`, `ctx-size`):

```go
func (s *Service) MigrateArgKeys() []MigrationWarning {
    profiles, err := s.store.List()
    if err != nil {
        return []MigrationWarning{fmt.Sprintf("list profiles: %v", err)}
    }

    var warnings []MigrationWarning
    for _, profile := range profiles {
        changed := false
        newArgs := make(map[string]any)

        for key, value := range profile.Args {
            canonical := s.canonicalizeKey(key)
            if canonical != key {
                changed = true
                newArgs[canonical] = value
            } else {
                newArgs[key] = value
            }
        }

        if changed {
            profile.Args = newArgs
            if err := s.store.Save(profile); err != nil {
                warnings = append(warnings, fmt.Sprintf(
                    "migrate arg keys for %s: %v", profile.ID, err,
                ))
            }
        }
    }
    return warnings
}

func (s *Service) canonicalizeKey(key string) string {
    // Usar shortToLong map existente (agora movido para migration)
    if long, ok := shortToLong[key]; ok {
        return long
    }
    return key
}
```

#### 3.4.5 Boot Blocker — Mensagem Genérica

**Arquivo modificado:** `internal/ui/root.go`

**Problema:** Mensagem de boot hardcoded sobre `llama-server`.

**Solução:** Mensagem genérica baseada no estado do catálogo:

```go
// Em vez de:
r.WithBootBlocker("llama-server not found", "Install with: pacman -S llama.cpp-cuda")

// Usar:
r.WithBootBlocker(
    "no backend available",
    "Add a backend in the Backends tab or install llama-server / vllm",
)
```

---

### Fase 5: Testes & Integração

**Objetivo:** Cobertura de testes completa para vLLM.

**Prioridade:** Média — necessária para release.

#### 3.5.1 Unit Tests

| Teste | Arquivo | Descrição |
|-------|---------|-----------|
| `VLLMGenerator_test.go` | `backendschema/` | Mock schemaStore, test generate + persist |
| `vllm_parser_test.go` | `llamahelp/` | Golden test do parser com vLLM --help |
| `vllm_exec_parser_test.go` | `llamahelp/` | Mock binary invocation |
| `vllm_args_test.go` | `processmgr/` | BuildArgs com perfil vLLM |
| `vllm_validator_test.go` | `validator/` | Validação com schema vLLM |
| `vllm_migrate_test.go` | `migration/` | Detect vLLM kind + migrate |
| `vllm_draft_test.go` | `profile_editor/` | Schema-driven form generation |

#### 3.5.2 Golden Test para vLLM Help Parser

**Arquivo novo:** `testdata/vllm-help-0.6.txt` (exemplo)
**Arquivo novo:** `testdata/vllm-help-0.6.golden.json`

Mesmo pattern do llama-server golden test:
```bash
go test ./internal/service/llamahelp/... -update -vllm
```

#### 3.5.3 Integration Tests

| Teste | Descrição |
|-------|-----------|
| Add vLLM backend → create profile → validate | End-to-end flow |
| Launch vLLM profile (mock process) | ProcessMgr com vLLM args |
| Health check vLLM (`/health`, `/v1/models`) | Monitor integration |
| Metrics from vLLM `/metrics` endpoint | Prometheus poller |
| Profile editor with vLLM backend | UI form generation |

#### 3.5.4 Test Helpers

```go
// internal/service/processmgr/helpers_test.go
func fakeVLLMBinary(t *testing.T) string {
    // Escrever script shell que simula vLLM behavior
    // - --help → output de help
    // - --version → version string
    // - serve → loop que responde /health e /metrics
}
```

---

## 4. Arquivos Afetados — Resumo

### Novos arquivos

| Arquivo | Descrição |
|---------|-----------|
| `internal/service/llamahelp/vllm_exec_parser.go` | Parser de --help do vLLM |
| `internal/service/llamahelp/vllm_parser.go` | Help text parser para argparse Python |
| `internal/service/llamahelp/embedded_vllm.go` | Fallback schema embutido para vLLM |
| `internal/service/backendschema/vllm_generator.go` | Generator de schema para vLLM |
| `internal/service/monitor/metrics_api.go` | Prometheus metrics poller (vLLM) |
| `internal/service/migration/arg_keys.go` | Migration de arg keys |
| `testdata/vllm-help-v*.txt` | Golden test fixture |
| `testdata/vllm-help-v*.golden.json` | Golden test expected output |

### Arquivos modificados

| Arquivo | Mudança |
|---------|---------|
| `cmd/model-loader/main.go` | Registrar VLLMGenerator, maybeAddVLLMToCatalog |
| `internal/domain/backend.go` | Adicionar HealthEndpoint, MetricsSource fields |
| `internal/domain/instance.go` | Adicionar HealthEndpoint field |
| `internal/config/config.go` | Adicionar DefaultVLLMBinaryPath |
| `internal/service/processmgr/manager.go` | WaitHealthy com health endpoints configuráveis |
| `internal/service/processmgr/args.go` | Remover shortToLong (migrado) |
| `internal/service/monitor/slots.go` | Fallback para /v1/models |
| `internal/service/monitor/metrics.go` | Adicionar Prometheus metrics source |
| `internal/service/monitor/subscribe.go` | Escolher metrics poller por backend kind |
| `internal/service/backendcatalog/default.go` | Incluir vLLM no default catalog |
| `internal/service/migration/migration.go` | Detect vLLM kind + arg key migration |
| `internal/ui/pages/profile_editor/draft.go` | Schema-driven Draft + buildForm |
| `internal/ui/pages/profile_editor/editor.go` | Recalcular EssentialArgs on backend change |
| `internal/ui/pages/profiles.go` | Detail view genérico |
| `internal/ui/root.go` | Boot blocker genérico |
| `internal/ui/pages/backends.go` | Default kind não mais hardcoded (baixa prioridade) |

---

## 5. Order of Implementation

```
Fase 1: Infraestrutura Core
├── 3.1.1 VLLMExecParser
├── 3.1.2 vLLMHelpParser
├── 3.1.3 VLLMGenerator
├── 3.1.4 EmbeddedVLLMSchema
└── 3.1.5 Bootstrap registration

Fase 2: Process Manager & Monitor
├── 3.2.1 Health endpoint configurável
├── 3.2.2 vLLM health check
├── 3.2.3 Prometheus metrics poller
├── 3.2.4 Slots fallback (/v1/models)
└── 3.2.5 Args Builder genérico

Fase 3: Profile Editor
├── 3.3.1 Schema-driven Draft
├── 3.3.2 Dinâmico buildForm
├── 3.3.3 ApplyTo genérico
├── 3.3.4 Editor integration
└── 3.3.5 Detail view genérico

Fase 4: Config & Migration
├── 3.4.1 DefaultVLLMBinaryPath
├── 3.4.2 Default catalog com vLLM
├── 3.4.3 Migration detect vLLM
├── 3.4.4 Arg keys migration
└── 3.4.5 Boot blocker genérico

Fase 5: Testes
├── 3.5.1 Unit tests
├── 3.5.2 Golden tests
├── 3.5.3 Integration tests
└── 3.5.4 Test helpers
```

---

## 6. Riscos & Mitigação

| Risco | Impacto | Mitigação |
|-------|---------|-----------|
| vLLM --help format muda entre versões | Parser quebrado | Golden tests versionados + fallback embedded |
| vLLM não está em PATH | Generator falha na geração | Embedded fallback sempre disponível |
| Python lento para --help | Timeout no parser | Timeout generoso (15s) + cache do schema |
| Perfis existentes com chaves antigas | BuildArgs gera flags errados | Migration de arg keys no startup |
| Health check endpoints diferentes | WaitHealthy timeout | Multiple endpoints com fallback |
| Métricas Prometheus não disponíveis | Monitor sem dados | Fallback para log parsing |
| vLLM module path varia | Exec parser invoca comando errado | Properties field no Backend para module config |

---

## 7. Definição de Pronto (DoD)

- [ ] `vllm serve --help` é parseado e gera `BackendValidationSchema` válido
- [ ] Schema vLLM é persistido e carregado pelo resolver
- [ ] Perfil com backend vLLM é criado, validado e lançado
- [ ] Health check detecta vLLM ready (`/health` ou `/v1/models`)
- [ ] Métricas de throughput são capturadas via Prometheus `/metrics`
- [ ] Profile editor mostra campos relevantes do vLLM no Essentials tab
- [ ] Advanced tab mostra todos os flags do vLLM
- [ ] Perfis existentes continuam funcionando (migration)
- [ ] Golden tests passam para vLLM help parser
- [ ] `make tests` passa com cobertura >= existing baseline
- [ ] `go vet ./...` e `go fmt ./...` clean
- [ ] Documentação atualizada (AGENTS.md, docs/)

---

## 8. Notas

- **Backward compatibility:** Perfis existentes com `BackendID == ""` continuam usando o default backend (llama-server). Nenhum perfil existente é modificado sem migration explícita.
- **Schema versioning:** `BackendValidationSchema.SchemaVersion = 1` para ambos backends. Se vLLM precisar de schema fields adicionais, bump para 2.
- **Testing sem vLLM instalado:** Todos os tests devem funcionar sem vLLM instalado (usam mocks + golden files).
- **Performance:** vLLM --help pode ser lento (Python import). Schema é cached em disco e só regenerado via `RefreshSchema`.
- **Future backends:** O mesmo padrão (Generator + Parser + EmbeddedSchema) pode ser reutilizado para tabbyapi e sglang.

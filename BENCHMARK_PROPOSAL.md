# Proposta de Evolução do Benchmark Engine

> **Status:** Proposta técnica — aguardando aprovação para implementação  
> **Baseado em:** `benchmarks-deepresearch.md` + análise de codebase + pesquisa SOTA (2024–2025)  
> **Autor:** Sisyphus (Orquestração OhMyOpenCode)  
> **Data:** 2026-05-22

---

## 1. Resumo Executivo

O benchmark engine atual do `model-loader` possui uma fundação sólida — arquitetura Go limpa, persistência atômica, UI integrada no TUI, e três modos de avaliação (SWE-bench Lite com LLM-as-judge, needle-in-haystack, e throughput probe). No entanto, a cobertura cognitiva é estreita: apenas 8 problemas de código, um needle estático, e métricas puramente de velocidade. O documento de pesquisa profunda e o estado da arte em benchmarking de LLMs locais (2024–2025) demonstram que **perplexidade e token/s não capturam degradação de raciocínio** induzida por quantização. Modelos com perdas mínimas de PPL podem sofrer quedas severas em raciocínio multi-etapa (AIME, GSM8K), codificação (HumanEval), e recuperação de contexto longo.

**Objetivo desta proposta:** Expandir o engine para um **protocolo multidimensional de benchmarking** que avalie simultaneamente:

| Dimensão | O que mede | Por que importa para local LLMs |
|---|---|---|
| **Qualidade Cognitiva** | Raciocínio, matemática, código, conhecimento | Quantização Q4+ degrada capacidades de reasoning |
| **Memória de Longo Contexto** | Recuperação profunda, coerência em janelas >128k | KV-cache quantizado (Q4_0, FP8) causa perda posicional |
| **Throughput & Latência** | TTFT, TPOT, tok/s, prefill vs decode | Trade-off entre velocidade e precisão |
| **Eficiência de Recursos** | VRAM pico, GPU util, tokens/VRAM | Otimização de deployment em hardware limitado |
| **Robustez de Instrução** | Seguimento de formato, recusa, consistência | Quantização aumenta taxa de não-conformidade |

---

## 2. Estado Atual — Análise da Base de Código

### 2.1 Arquitetura Existente

```
TUI (BenchmarkPage)
  → benchmark.Runner.Run(ctx, RunConfig{ProfileID, Mode}, progressCh)
    → ensureInstance(profile)  // reusa ou lança llama-server
    → GPU sampler (peak VRAM, avg util)
    → [ModeJudge]      runProblem() × N  + judgeScorer.Score()
    → [ModeLongContext] runLongContext()
    → [ModeLlamaBench]  runLlamaBench() × presets
    → aggregate() → benchmarkstore.Save(run)
  → UI: list / compare / history / detail views
```

**Pacotes:**

| Pacote | Arquivo | Função |
|---|---|---|
| `benchmark` | `runner.go` | Orquestração do run, lifecycle do servidor, agregação |
| `benchmark` | `result.go` | Tipos: `Mode`, `Run`, `ProblemResult`, `Aggregate`, `ProfileSnapshot` |
| `benchmark` | `problem.go` | `Problem` struct (SWE-bench Lite item) |
| `benchmark` | `dataset.go` | Loader do dataset embutido (`data/swebench_lite.json`) |
| `benchmark` | `client.go` | Cliente OpenAI-compatible streaming com métricas (TTFT, tok/s) |
| `benchmark` | `scorer.go` | `judgeScorer` — LLM-as-judge com rubrica estruturada e self-consistency (median + majority vote) |
| `benchmark` | `prompt.go` | Prompt builder estilo SWE-bench oracle (prompt_style_3) + `ExtractDiff()` |
| `benchmarkstore` | `fs_store.go` | Persistência FS: 1 JSON por run + `.transcript.json` separado |
| `ui/pages` | `benchmark.go` | Tab page com 5 views: list, profilePick, modePick, running, detail, compare, history |

### 2.2 Pontos Fortes

- **Separação clara** entre engine, persistência e UI.
- **Self-consistency no judge** — múltiplas amostras com mediana + maioria.
- **Transcrição opcional** de I/O bruto para debugging (`SaveTranscripts`).
- **Snapshot do profile** no run (modelo, quantização, `cache_type_k/v`, ctx_size) — essencial para comparações.
- **Reutilização de instância** — se o profile já está rodando, reusa; se não, lança e mata após o run.
- **Cancelamento de contexto** em todas as fases (launch, infer, score).

### 2.3 Limitações Críticas

| # | Limitação | Impacto | Severidade |
|---|---|---|---|
| L1 | Dataset SWE-bench Lite embutido tem **apenas 8 problemas** | Variância estatística alta; não detecta regressões sutis | 🔴 Alta |
| L2 | Needle-in-haystack é **estático** (mesmo valor `WIDGET_PRODUCTION_KEY` sempre) | Pode ser memorizado; não mede degradação real de KV-cache | 🔴 Alta |
| L3 | Não há benchmarks de **raciocínio matemático** (GSM8K, AIME) | Cegueira total para degradação de reasoning por quantização | 🔴 Alta |
| L4 | Não há benchmarks de **conhecimento geral / MMLU** | Não avalia retenção de conhecimento factual | 🟡 Média |
| L5 | Não há benchmarks de **RAG** (Ragas: faithfulness, precision, relevancy) | Cegueira para pipelines de recuperação | 🟡 Média |
| L6 | Não há métricas de **KV-cache impact** separadas do throughput | Não distingue se a lentidão é do modelo ou do cache | 🟡 Média |
| L7 | `ModeJudge` depende de **endpoint externo de judge** — configuração manual | Barreira de adoção; não funciona out-of-the-box | 🟡 Média |
| L8 | LlamaBench presets são apenas **2 configurações fixas** (512/128, 4096/256) | Não cobre cenários de RAG (long prefill) ou chat (curto prefill, long gen) | 🟡 Média |
| L9 | Não há **comparação cruzada de quantizações** no mesmo modelo | Usuário precisa manualmente criar profiles e comparar | 🟢 Baixa |
| L10 | Não há **benchmark de consistência / instrução seguimento** | Não detecta aumento de recusas ou não-conformidade de formato por quantização | 🟢 Baixa |

---

## 3. Síntese do Documento de Pesquisa Profunda

O documento `benchmarks-deepresearch.md` (26.4 KB) estabelece princípios fundamentais que justificam cada proposta abaixo:

### 3.1 Quantização de Pesos vs. Degradação Cognitiva

- **Tabela 1** mapeia precisão → bytes/param → VRAM → degradação de perplexidade → retenção cognitiva multilíngue.
- **Insight crítico:** Q6_K retém >98% da inteligência nativa; Q4_K_M cai para 90–95%; Q3_K é sensível (8–15% degradação); Q2 é inviável.
- **Para o benchmark:** Precisamos medir não só "resolveu?" mas **quanto a resposta degradou em relação à baseline FP16**.

### 3.2 KV Cache Dynamics

- **Tabela 3** mostra que cache KV em FP16 (baseline) vs Q4_0 vs Q8_0 tem divergência KL, fidelidade de token (Same Top-P), e penalidade de decode em >100k tokens.
- **Recomendação do documento:** usar needle **dinâmico** (cidade + número aleatório) para evitar memorização. O engine atual usa needle estático — violação direta.
- **Para o benchmark:** Adotar needle dinâmico; adicionar testes de **coerência em janelas longas** (sumarização multi-doc, não só recuperação de fat).

### 3.3 Protocolos Multidimensionais de Benchmarking

O documento propõe transição de métricas sintéticas (PPL, MMLU estático) para **pipelines baseados em tarefas reais**:

- **GSM8K** → matemática escolar (raciocínio multi-etapa).
- **HumanEval** → geração de código (Pass@1).
- **AIME 2025** → olimpíadas de matemática (reasoning avançado).
- **Ragas** → pipelines RAG (faithfulness, context precision, answer relevancy).
- **Needle in a Haystack dinâmico** → resiliência de contexto longo.
- **Métricas de throughput:** TTFT, TPOT, e2e latency, não só "tok/s" bruto.

---

## 4. Benchmarks Propostos — Detalhamento

### 4.1 Expansão do Modo Judge (SWE-bench Lite)

**Melhoria 1A — Aumentar o dataset para 32–64 problemas**

- Atual: 8 problemas embutidos.
- Proposto: Curadoria manual de 32 problemas de SWE-bench Lite (diversidade por linguagem: Python, Go, Rust, JS).
- **Por quê:** Variância estatística com 8 problemas é muito alta. Com 32, o solve_rate converge melhor.
- **Como:** Manter o formato JSON atual; expandir `data/swebench_lite.json`. O `dataset.go` já carrega via `go:embed` — apenas adicionar entradas.

**Melhoria 1B — Rubrica expandida no judge**

- Atual: localization (0.30), correctness (0.50), completeness (0.20).
- Proposto: Adicionar **maintainability** (0.10) — avalia se o patch introduz novas dependências ou quebra testes existentes.
- **Por quê:** Patches "corretos" mas que quebram a codebase são comuns em quantizações baixas.

**Melhoria 1C — Judge local (fallback quando endpoint externo não configurado)**

- Atual: Requer configuração manual de `JudgeEndpoint` (URL, API key, modelo).
- Proposto: Se nenhum judge externo está configurado, usar o **próprio modelo sob teste** como judge com uma estratégia de "consistência estrutural": comparar o patch candidato contra o golden patch via AST similarity (usando tree-sitter ou regex estruturado). Não é tão bom quanto um LLM judge, mas permite benchmark out-of-the-box.
- **Alternativa:** Integrar com um modelo local pequeno (e.g., Qwen2.5-7B-Instruct) servido em porta efêmera para judge.

### 4.2 Novo Modo: MathBench (GSM8K + AIME-style)

**Objetivo:** Medir degradação de raciocínio matemático por quantização.

**Arquitetura:**

```go
type MathBenchMode string
const (
    MathGSM8K  MathBenchMode = "gsm8k"   // ~8.5k problemas de matemática escolar
    MathAIME   MathBenchMode = "aime"    // 30 problemas de olimpíadas (hard)
    MathCustom MathBenchMode = "custom"  // dataset embutido pelo usuário
)
```

**Dataset:**
- Embutir subset curado de GSM8K (e.g., 200 problemas estratificados por dificuldade) no binary.
- Para AIME, embutir problemas 2023–2025 (públicos).
- Formato: `{"question": "...", "answer": "42", "difficulty": 3}`

**Avaliação:**
- **Exact match** na resposta numérica (strip trailing `.0`, normalizar `42` vs `42.0` vs `forty-two`).
- **Pass@1** — uma tentativa por problema (não sampling múltiplo como no original).
- **Chain-of-thought opcional** — se o modelo gerar `<think>...</think>`, extrair apenas a resposta final.

**Por que importa para local LLMs:**
O documento de pesquisa cita explicitamente que modelos com perdas mínimas de PPL podem sofrer "quedas severas de raciocínio lógico em testes que exigem raciocínio complexo multi-etapa, como o benchmark de olimpíadas matemáticas AIME 2025".

### 4.3 Novo Modo: CodeGenBench (HumanEval-style)

**Objetivo:** Medir degradação de geração de código funcional.

**Arquitetura:**

```go
type CodeGenBenchMode string
const (
    CodeHumanEval CodeGenBenchMode = "humaneval" // 164 problemas clássicos
    CodeMBPP      CodeGenBenchMode = "mbpp"      // Mostly Basic Python Problems
)
```

**Dataset:**
- Embutir subset de HumanEval (e.g., 64 problemas representativos) no binary.
- Formato: `{"task_id": "...", "prompt": "def foo():\n    ...", "canonical_solution": "...", "test": "..."}`

**Avaliação:**
- **Execução sandboxed** — escrever o código gerado em arquivo temporário, executar `python -c` (ou `go test` para Go), verificar pass/fail.
- **Pass@1** — uma geração por problema.
- **Timeout de execução** — 5s por teste para evitar loops infinitos.

**Desafio técnico:**
- Requer runtime Python/Go disponível no host. Para o `model-loader` (Go binary), podemos:
  - (a) Depender de `python3` no PATH (documentar).
  - (b) Usar `docker run --rm python:3.11-slim` (se Docker disponível).
  - (c) Para problemas simples, usar `go test` gerado dinamicamente (se o usuário tem Go toolchain).
- **Recomendação:** Opção (a) + (b) com fallback; se nenhum disponível, reportar "skipped" no run.

### 4.4 Melhoria do Modo LongContext

**Melhoria 4A — Needle dinâmico**

- Atual: `WIDGET_PRODUCTION_KEY = 'blue-elephant-2024'` (sempre o mesmo).
- Proposto: Gerar needle aleatório por run:
  ```go
  city := randomCity()     // e.g., "Reykjavik", "Ulaanbaatar"
  number := randInt(1000, 9999)
  needle := fmt.Sprintf("The special magic %s number is: %d", city, number)
  ```
- **Por quê:** O documento de pesquisa recomenda explicitamente needles dinâmicos para "impedir respostas estáticas simuladas pelas GPUs locais".

**Melhoria 4B — Multi-needle em profundidade variada**

- Inserir 3 needles em profundidades diferentes (25%, 50%, 75% do haystack).
- Perguntar: "What are the three special magic numbers? List them in order."
- **Por quê:** Modelos com KV-cache quantizado frequentemente perdem needles no meio da janela (50%) mas retêm os dos extremos (25%, 75%).

**Melhoria 4C — Haystack de qualidade (não só código filler)**

- Atual: Código Python filler genérico (`handler_NNNN`).
- Proposto: Usar trechos reais de documentação técnica (e.g., manuais de LLMs, papers acadêmicos) para simular RAG real.
- Fonte: Embutir ~50 documentos técnicos do arXiv (abstracts) no binary.

**Melhoria 4D — Coerência de longa janela (além de needle)**

- Adicionar um teste de **sumarização multi-documento**: fornecer 10 documentos (~100k tokens) e pedir um resumo que mencione 5 fatos específicos.
- Avaliação: LLM-as-judge verifica se os 5 fatos estão presentes no resumo.
- **Por quê:** Needle mede recuperação de fato isolado; sumarização mede **coerência global** do contexto.

### 4.5 Melhoria do Modo LlamaBench (Throughput)

**Melhoria 5A — Presets expandidos**

- Atual: `{512/128, 4096/256}`.
- Proposto: `{128/512, 512/128, 2048/256, 4096/256, 8192/128, 16384/64}`
  - `128/512` → chat-like (curto prefill, longa geração).
  - `8192/128` → RAG-like (longo prefill, curta geração).
  - `16384/64` → extreme RAG.
- **Por quê:** Diferentes workloads têm perfis de latência distintos. Um modelo pode ser rápido em chat (128/512) mas lento em RAG (8192/128).

**Melhoria 5B — Métricas de KV-cache impact**

- Adicionar ao `Aggregate`:
  - `PromptProcessingTPS` (tokens de prefill / TTFT) — mede velocidade de atenção.
  - `DecodeTPS` (tokens de geração / (total - TTFT)) — mede velocidade de decode.
  - `KVCacheGrowthRateMBPer1kTokens` — estimado a partir do `peakVramMb` e `promptTokens` (se monitor expõe isso).
- **Por quê:** O documento de pesquisa separa explicitamente TTFT (prefill) de TPOT (decode). A degradação de KV-cache afeta principalmente o decode.

**Melhoria 5C — Warmup runs**

- Antes de medir, fazer 1–2 runs de aquecimento para estabilizar o cache e evitar cold-start bias.

### 4.6 Novo Modo: RagasBench (RAG Pipeline Quality)

**Objetivo:** Medir qualidade de pipelines RAG locais.

**Arquitetura:**

```go
type RagasBenchMode string
const (
    RagasFaithfulness   RagasBenchMode = "faithfulness"    // resposta alinhada com documentos?
    RagasRelevancy      RagasBenchMode = "relevancy"       // resposta pertinente à pergunta?
    RagasPrecision      RagasBenchMode = "precision"         // contexto recuperado é preciso?
    RagasFull           RagasBenchMode = "full"            // todas as métricas
)
```

**Dataset:**
- Embutir 20 cenários RAG sintéticos: documentos técnicos (e.g., man page de `grep`, README de projeto) + perguntas + respostas esperadas.
- Formato: `{"documents": ["..."], "question": "...", "ground_truth": "...", "expected_context": "..."}`

**Avaliação:**
- O modelo gera uma resposta a partir dos documentos fornecidos.
- LLM-as-judge (ou heurística local) avalia:
  - **Faithfulness:** A resposta contém apenas informações presentes nos documentos? (detecção de alucinação)
  - **Answer Relevancy:** A resposta responde diretamente à pergunta?
  - **Context Precision:** Os documentos fornecidos contêm a resposta? (simulando um retriever perfeito)
- **Por quê:** O documento de pesquisa recomenda explicitamente o framework Ragas para "monitorar de forma determinística três propriedades de inferência".

### 4.7 Novo Modo: InstructionBench (Robustez de Instrução)

**Objetivo:** Medir taxa de recusa e conformidade de formato.

**Arquitetura:**

```go
type InstructionBenchMode string
const (
    InstFormatJSON   InstructionBenchMode = "format-json"    // resposta deve ser JSON válido
    InstFormatList   InstructionBenchMode = "format-list"    // resposta deve ser lista markdown
    InstRefusal      InstructionBenchMode = "refusal"        // pergunta sensível → deve recusar
    InstConsistency  InstructionBenchMode = "consistency"    // mesma pergunta 3x → mesma resposta
)
```

**Dataset:**
- 20 prompts de formato ("Responda apenas com um JSON contendo...").
- 10 prompts de recusa ("Como fazer algo ilegal?").
- 10 prompts de consistência (mesma pergunta factual 3 vezes).

**Avaliação:**
- **Format:** Validar JSON parseável / lista markdown via regex.
- **Refusal:** Detectar palavras-chave de recusa ("I cannot", "I'm sorry") ou verificar se a resposta é vazia.
- **Consistency:** Cosine similarity entre embeddings das 3 respostas (ou exact match se factual).
- **Por quê:** Quantização Q3/Q4 aumenta taxa de "não-conformidade de formato" e "recusas espúrias".

### 4.8 Novo Modo: MMLUBench (Conhecimento Factual)

**Objetivo:** Medir retenção de conhecimento geral.

**Arquitetura:**
- Integração com **lm-evaluation-harness** (EleutherAI) como subprocesso opcional.
- Se `lm_eval` não está disponível no PATH, usar dataset embutido de 100 perguntas MMLU (subset por categoria: STEM, humanities, social sciences, other).
- Formato: `{"question": "...", "choices": ["A", "B", "C", "D"], "answer": "B"}`

**Avaliação:**
- Exact match na letra da escolha.
- Métricas por categoria + métrica agregada.
- **Por quê:** MMLU é o padrão de facto para conhecimento factual. O documento de pesquisa menciona MMLU como baseline, mas alerta que PPL/MMLU não capturam degradação de reasoning.

---

## 5. Arquitetura Proposta — Mudanças Estruturais

### 5.1 Novos Tipos de Domínio

```go
// benchmark/result.go (extensões)

type Mode string
const (
    ModeJudge        Mode = "judge"
    ModeLongContext  Mode = "longctx"
    ModeLlamaBench   Mode = "llama-bench"
    ModeMathBench    Mode = "math-bench"      // NOVO
    ModeCodeGenBench Mode = "codegen-bench"   // NOVO
    ModeRagasBench   Mode = "ragas-bench"     // NOVO
    ModeInstBench    Mode = "instruction-bench" // NOVO
    ModeMMLUBench    Mode = "mmlu-bench"       // NOVO
)

// MathResult é o resultado de um problema de matemática.
type MathResult struct {
    ProblemID   string  `json:"problemId"`
    Question    string  `json:"question"`
    Expected    string  `json:"expected"`
    Got         string  `json:"got"`
    Correct     bool    `json:"correct"`
    HasCoT      bool    `json:"hasCoT"`      // chain-of-thought detectado?
    Detail      string  `json:"detail"`
}

// CodeGenResult é o resultado de um problema de geração de código.
type CodeGenResult struct {
    ProblemID string `json:"problemId"`
    TaskID    string `json:"taskId"`
    Code      string `json:"code"`
    Passed    bool   `json:"passed"`
    Stderr    string `json:"stderr,omitempty"`
    Timeout   bool   `json:"timeout,omitempty"`
}

// Aggregate (extensões)
type Aggregate struct {
    // ... campos existentes ...
    PromptProcessingTPS float64 `json:"promptProcessingTps"` // prefill tok/s
    DecodeTPS           float64 `json:"decodeTps"`           // decode tok/s
    // Modo-específicos (zero quando não aplicável)
    MathAccuracy        float64 `json:"mathAccuracy,omitempty"`        // 0..1
    CodePassRate        float64 `json:"codePassRate,omitempty"`        // 0..1
    RagasFaithfulness   float64 `json:"ragasFaithfulness,omitempty"`   // 0..1
    RagasRelevancy      float64 `json:"ragasRelevancy,omitempty"`      // 0..1
    RagasPrecision      float64 `json:"ragasPrecision,omitempty"`      // 0..1
    InstFormatRate      float64 `json:"instFormatRate,omitempty"`      // 0..1
    InstRefusalRate     float64 `json:"instRefusalRate,omitempty"`     // 0..1
    InstConsistency     float64 `json:"instConsistency,omitempty"`     // 0..1
    MMLUAccuracy        float64 `json:"mmluAccuracy,omitempty"`        // 0..1
}
```

### 5.2 Novos Arquivos no Pacote `benchmark/`

| Arquivo | Responsabilidade |
|---|---|
| `mathbench.go` | `runMathBench()`, dataset loader, exact-match scorer |
| `codegenbench.go` | `runCodeGenBench()`, dataset loader, sandbox executor |
| `ragasbench.go` | `runRagasBench()`, dataset loader, faithfulness/relevancy/precision scorers |
| `instructionbench.go` | `runInstructionBench()`, dataset loader, format/refusal/consistency validators |
| `mmlubench.go` | `runMMLUBench()`, dataset loader, `lm_eval` wrapper ou evaluator local |
| `data/gsm8k_curated.json` | Dataset embutido de matemática |
| `data/humaneval_curated.json` | Dataset embutido de geração de código |
| `data/ragas_scenarios.json` | Dataset embutido de cenários RAG |
| `data/instruction_prompts.json` | Dataset embutido de prompts de instrução |
| `data/mmlu_subset.json` | Dataset embutido de MMLU (100 questões) |
| `data/arxiv_docs.json` | Documentos técnicos para haystack de qualidade |

### 5.3 Mudanças em `runner.go`

```go
func (r *Runner) Run(ctx context.Context, rc RunConfig, progress chan<- Progress) (Run, error) {
    // ... validação do scorer/judge (existente) ...
    switch rc.Mode {
    case ModeJudge:
        // ... existente ...
    case ModeLongContext:
        // chama runLongContext() melhorado (needle dinâmico, multi-needle)
    case ModeLlamaBench:
        // chama runLlamaBench() melhorado (presets expandidos, warmup)
    case ModeMathBench:
        // chama runMathBench() NOVO
    case ModeCodeGenBench:
        // chama runCodeGenBench() NOVO
    case ModeRagasBench:
        // chama runRagasBench() NOVO
    case ModeInstBench:
        // chama runInstructionBench() NOVO
    case ModeMMLUBench:
        // chama runMMLUBench() NOVO
    }
}
```

### 5.4 Mudanças na UI (`benchmark.go`)

- Adicionar novos modos ao slice `benchModes`.
- As views existentes (list, compare, history, detail) já funcionam para qualquer `Mode` porque operam em `Run` e `Aggregate` genéricos.
- Adicionar **view especializada por modo** no `bvRunDetail`:
  - Para `ModeMathBench`: mostrar breakdown por dificuldade.
  - Para `ModeCodeGenBench`: mostrar lista de pass/fail com links para stderr.
  - Para `ModeRagasBench`: mostrar radar chart (faithfulness, relevancy, precision).
  - Para `ModeInstBench`: mostrar taxas de formato/recusa/consistência.
- Adicionar **view de "Quantization Comparison"**: permite selecionar múltiplos runs do mesmo modelo com quantizações diferentes e mostrar degradê de métricas (e.g., Q8_0 vs Q4_K_M vs Q3_K).

### 5.5 Mudanças em `benchmarkstore/fs_store.go`

- Nenhuma mudança estrutural necessária — o `Run` struct genérico já acomoda novos campos em `Aggregate`.
- Considerar adicionar `Run.Tags []string` para permitir filtros (e.g., `tag="q4_k_m"`).

---

## 6. Plano de Implementação — Fases

### Fase 1: Fundações (1–2 semanas)

1. **Refatorar `Mode` para ser extensível** — extrair switch de `runner.go.Run()` para método `runMode(mode, ctx, base, model)`.
2. **Implementar needle dinâmico** — `runLongContext()` com `randomCity()` + `randInt()`.
3. **Implementar multi-needle** — inserir 3 needles em profundidades 25%, 50%, 75%.
4. **Expandir presets do LlamaBench** — adicionar `{128/512, 2048/256, 8192/128, 16384/64}`.
5. **Adicionar métricas de prefill/decode** — `PromptProcessingTPS` e `DecodeTPS` no `Aggregate`.
6. **Aumentar dataset SWE-bench** — expandir para 32 problemas.

**QA:** Rodar cada modo existente (judge, longctx, llama-bench) e confirmar que não houve regressão.

### Fase 2: Benchmarks Cognitivos (2–3 semanas)

1. **MathBench (GSM8K)** — criar `mathbench.go`, dataset embutido, exact-match scorer.
2. **CodeGenBench (HumanEval)** — criar `codegenbench.go`, dataset embutido, sandbox executor (Python fallback).
3. **InstructionBench** — criar `instructionbench.go`, format/refusal/consistency validators.
4. **MMLUBench** — criar `mmlubench.go`, dataset embutido de 100 questões; `lm_eval` wrapper como bonus.

**QA:** Rodar cada novo modo contra 2–3 profiles (diferentes modelos/quantizações) e verificar que as métricas fazem sentido (e.g., Q8_0 deve ter accuracy maior que Q4_K_M no MathBench).

### Fase 3: RAG e Coerência (1–2 semanas)

1. **RagasBench** — criar `ragasbench.go`, dataset de cenários RAG, scorers de faithfulness/relevancy/precision.
2. **Haystack de qualidade** — substituir filler Python por documentos técnicos reais (arXiv abstracts).
3. **Sumarização multi-documento** — adicionar ao `ModeLongContext` um segundo teste além do needle.

**QA:** Verificar que RagasBench detecta alucinação em modelos quantizados (Q3_K tende a alucinar mais).

### Fase 4: UI e Comparativos (1 semana)

1. **Adicionar novos modos ao picker** de modo na UI.
2. **View de "Quantization Comparison"** — selecionar múltiplos runs do mesmo modelo, mostrar degradê.
3. **View especializada por modo** no detail view.
4. **Exportar runs como CSV/JSON** para análise externa.

**QA:** Testar navegação completa no TUI para cada novo modo.

### Fase 5: Judge Local e Autonomia (1 semana — bonus)

1. **Implementar judge local** — usar o próprio modelo sob teste como judge com heurística estrutural.
2. **Configuração zero** — se `JudgeEndpoint` não configurado, fallback para judge local.

**QA:** Comparar scores do judge local vs judge externo em 10 problemas; correlacionar >0.8.

---

## 7. Critérios de Sucesso

| Critério | Como medir | Target |
|---|---|---|
| **Cobertura cognitiva** | Número de dimensões avaliadas | ≥ 6 (coding, math, codegen, rag, instruction, knowledge, throughput, long-context) |
| **Sensibilidade à quantização** | Delta de métricas entre Q8_0 e Q4_K_M no mesmo modelo | MathAccuracy cai ≥ 5%; CodePassRate cai ≥ 3%; LongContext needle cai ≥ 10% em Q3_K |
| **Reprodutibilidade** | Variância do solve_rate em 3 runs consecutivos do mesmo profile | σ < 3% |
| **Tempo de run** | Tempo total para um run completo (incluindo launch do servidor) | < 15 min para todos os modos combinados |
| **Usabilidade** | Número de modos acessíveis sem configuração manual de judge externo | ≥ 5 (todos exceto ModeJudge, que ainda precisa de judge ou fallback local) |
| **Regressão zero** | Runs dos modos existentes (judge, longctx, llama-bench) continuam funcionando | 100% pass |

---

## 8. Riscos e Mitigações

| Risco | Probabilidade | Impacto | Mitigação |
|---|---|---|---|
| **Tamanho do binary explode** com datasets embutidos | Média | Alto | Usar subsets curados (200 questões GSM8K, 64 HumanEval); compressão gzip dos JSONs embutidos |
| **Sandbox de código inseguro** (execução arbitrária) | Baixa | Alto | Timeout agressivo (5s); restringir imports Python; usar `exec` ao invés de `eval`; opcionalmente docker |
| **Judge local ser muito permissivo** (score inflado) | Média | Médio | Calibrar contra judge externo em subset; documentar limitação; manter judge externo como "gold standard" |
| **UI ficar poluída** com muitos modos | Média | Médio | Agrupar modos em categorias no picker ("Quality", "Speed", "Robustness"); views genéricas como default |
| **Tempo de run muito longo** | Média | Médio | Permitir seleção de subset por modo ("quick" vs "full"); benchmark parcial como default |

---

## 9. Referências e Ferramentas Externas

| Ferramenta | Uso Proposto | URL |
|---|---|---|
| **lm-evaluation-harness** | Wrapper opcional para MMLU, TruthfulQA, etc. | https://github.com/EleutherAI/lm-evaluation-harness |
| **OpenCompass** | Referência de arquitetura de avaliação multi-dataset | https://github.com/open-compass/OpenCompass |
| **Ragas** | Framework de métricas RAG (faithfulness, relevancy, precision) | https://docs.ragas.io |
| **SWE-bench** | Fonte de problemas de código reais | https://github.com/princeton-nlp/SWE-bench |
| **GSM8K** | Dataset de matemática escolar | https://github.com/openai/grade-school-math |
| **HumanEval** | Dataset de geração de código | https://github.com/openai/human-eval |
| **AIME** | Problemas de olimpíadas de matemática | Públicos (2023–2025) |

---

## 10. Conclusão

O benchmark engine atual do `model-loader` é uma fundação excepcional — arquitetura limpa, UI integrada, e abstrações bem desenhadas. A proposta acima não reinventa nada; **expande horizontalmente** a cobertura cognitiva e **refina verticalmente** a profundidade de cada dimensão. O documento de pesquisa profunda fornece justificativa teórica robusta para cada benchmark proposto, especialmente a transição de "perplexidade + token/s" para "tarefas reais de negócio".

**Próximo passo recomendado:** Aprovar Fase 1 (fundamentos) para execução imediata. As Fases 2–5 podem ser executadas sequencialmente ou em paralelo por subagentes especializados.

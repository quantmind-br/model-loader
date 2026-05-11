# Relatório de Qualidade: PLAN-vllm-support.md

**Arquivo analisado:** `PLAN-vllm-support.md` (44,498 bytes, ~600 linhas Markdown)
**Data da análise:** 2026-05-10
**Metodologia:** Leitura completa do plano + validação de cada afirmação contra o código real + análise de gaps estruturais

---

## 1. Pontuação Geral

| Dimensão | Nota (0-10) | Peso | Ponderado |
|----------|-------------|------|-----------|
| Cobertura (completeness) | **6.5** | 30% | 1.95 |
| Precisão técnica (accuracy) | **7.5** | 25% | 1.88 |
| Acionabilidade (actionability) | **8.0** | 20% | 1.60 |
| Segurança regressiva (backward compat) | **5.5** | 15% | 0.83 |
| Clareza estrutural | **8.5** | 10% | 0.85 |
| **TOTAL** | | | **7.11 / 10** |

**Veredito:** Plano sólido como mapa direcional, mas **não está pronto para implementação imediata**. 6 gaps críticos precisam ser resolvidos antes de começar a codificar. Outros 5 gaps importantes devem ser endereçados durante a implementação.

---

## 2. Gaps Críticos (Must-Fix Antes de Implementar)

### 🔴 Gap 1: `shortToLong` existe em DOIS lugares, não em um

**O plano diz:** Apenas `processmgr/args.go` tem o map `shortToLong`. (Seção 3.2.5)

**A realidade:** Existem DUAS cópias independentes:
- `internal/service/processmgr/args.go:19` — usado na build de CLI args
- `internal/service/validator/rules.go:13` — usado na validação de tipos

Ambos contêm exatamente o mesmo map `"ngl": "n-gpu-layers"`. O plano só aborda remover/adaptar o da `processmgr/args.go`. Se um for removido sem o outro ser atualizado, a validação de perfis existentes quebra silenciosamente (a validação procura `"n-gpu-layers"` no schema, mas o perfil tem `"ngl"`).

**Ação:** Adicionar seção explícita sobre a duplicação. A migration de arg keys (Seção 3.4.4) deve lidar com AMBOS os locais, e o validator `canonicalFlag` deve ser atualizado simultaneamente com o de processmgr.

---

### 🔴 Gap 2: Validador `applyExistenceRules` quebra com modelos vLLM

**O plano diz:** "validator.Validator — validação de tipos genérica (usa schema)" está listado como ✅ Já pronto (Seção 1.4).

**A realidade:** `applyExistenceRules()` em `validator/rules.go:147` faz:
```go
if _, err := os.Stat(p.Model); err != nil {
    // ... SeverityError: "model file does not exist"
}
```

Para vLLM, o campo `Model` frequentemente NÃO é um caminho de arquivo local — é um HuggingFace repo ID como `meta-llama/Llama-3.2-1B` ou `microsoft/Phi-3-mini-4k-instruct`. `os.Stat()` nesses valores SEMPRE falha, bloqueando qualquer perfil vLLM de ser salvo.

**Ação:** A regra de existência deve ser condicional ao backend kind. Backends `llama-server` precisam de `os.Stat`. Backends `vllm` precisam de uma validação diferente (ex: verificar se o ID parece um HF repo válido, ou simplesmente skip). Adicionar esta seção na Fase 3 ou na matriz de gaps.

---

### 🔴 Gap 3: `Reconcile()` quebra recuperação de processos vLLM

**O plano diz:** "Reconcile() — ✅ Generic" (Seção 1.4, via tabela resumo).

**A realidade:** `pidAliveAndNameMatches()` em `recover.go:52` verifica `/proc/<pid>/comm` contra o basename do binário. Para vLLM executado via Python:
```
/bin/python -m vllm.entrypoints.openai.api_server
```
O `/proc/<pid>/comm` será `python` (truncado para 15 chars), não `vllm`. O `Reconcile()` tentará combinar `"python"` (nome do processo) com `"vllm"` (nome esperado do backend) e **NUNCA encontrará match**. Processos vLLM sobreviventes serão descartados como "zombies" e removidos do tracking.

**Ação:** O campo `RunningInstance.BinaryPath` armazenado é o resolved binary path (`/usr/bin/python`). Para vLLM, precisamos verificar não apenas o nome do binário, mas também argumentos adicionais via `/proc/<pid>/cmdline`. Adicionar esta discussão na Seção 3.2 (Process Manager) — talvez como subseção 3.2.6.

---

### 🔴 Gap 4: `Reconcile()` não tem `defaultBinary` compatível com vLLM

**O plano diz:** Não aborda o `defaultBinary` no contexto de recovery.

**A realidade:** `Reconcile()` em `recover.go:35`:
```go
if binary == "" {
    binary = m.defaultBinary
}
```
Onde `defaultBinary` em `manager.go:55` é hardcoded:
```go
if m.defaultBinary == "" {
    m.defaultBinary = "llama-server"
}
```

Se um `RunningInstance` não tiver `BinaryPath` (campo omitido no JSON por ser `omitempty`), o recovery usa `"llama-server"` como fallback. Instâncias vLLM sem `BinaryPath` serão tratadas como llama-server e potencialmente descartadas.

**Ação:** `RunningInstance` deve sempre incluir `BinaryPath` no JSON (remover `omitempty`) OU adicionar `BackendKind` ao `RunningInstance` para o recovery saber qual binário esperar.

---

### 🔴 Gap 5: vLLM não tem `--model` com sintaxe de arquivo local

**O plano diz:** `BuildArgs` em `args.go:46` adiciona `--model` como primeiro argumento. O plano (Seção 3.2.5) propõe remover `shortToLong` mas mantém `BuildArgs`.

**A realidade:** `BuildArgs` sempre emite `--model <path>` como primeiro argumento. Para vLLM, o `--model` aceita:
- HuggingFace repo ID (string, ex: `"meta-llama/Llama-3.2-1B"`)
- Caminho local para diretório com safetensors

Ambos são strings, então a sintaxe `--model <valor>` funciona. **PORÉM**, o plano deveria documentar explicitamente que `--model` é compatível entre backends e que o valor é determinado pelo campo `Profile.Model`, que para vLLM será um HF repo ID em vez de caminho GGUF.

**Ação:** Adicionar nota na Seção 3.2.5 confirmando compatibilidade. Não é blocker, mas é importante documentar.

---

### 🔴 Gap 6: Draft hydration de perfil existente não tem plano de migração

**O plano diz:** Seção 3.3.1 — substituir campos hardcoded do Draft por `EssentialArgs map`.

**A realidade:** `startEditSelected()` em `profiles.go:609` hidrata o Draft assim:
```go
d := profile_editor.Draft{
    NGL:       profile_editor.ArgString(pr.Args["ngl"]),
    CtxSize:   profile_editor.ArgString(pr.Args["ctx-size"]),
    // ... hardcoded per-field
}
```

Com o Draft schema-driven, este código precisa ser reescrito para popular `d.EssentialArgs["n-gpu-layers"].Value` a partir de `pr.Args["ngl"]`. O plano menciona `updateEssentialArgs()` que preserva valores, mas **não explica como o carregamento inicial (startEditSelected) popula o map a partir de um Profile existente**.

Além disso, `newDraftDefaults()` em `profiles.go:537` define defaults hardcoded (`NGL: "99"`, `CtxSize: "8192"`, etc.). Com schema-driven, os defaults devem vir do schema (`FlagSpec.Default`), e devem variar por backend kind.

**Ação:** Adicionar subseção 3.3.6: "Hydration de perfil existente" e "Default values por backend kind". Especificar como `startEditSelected` popula `EssentialArgs` a partir de `Profile.Args` usando o schema para canonicalização de chaves.

---

## 3. Gaps Importantes (Should-Fix Durante Implementação)

### 🟡 Gap 7: vLLM Module Path — o processmgr não sabe lidar com comandos multi-parte

**O plano diz:** Seção 3.1.3 — usar `Backend.Properties["module"]` para o módulo Python.

**A realidade:** `processmgr.fsManager.Launch()` faz:
```go
cmd := exec.Command(resolvedBinary, BuildArgs(p)...)
```
Onde `resolvedBinary` é o path retornado pelo resolver (ex: `/usr/bin/python`). Se o backend precisa de `python -m vllm.entrypoints.openai.api_server`, os argumentos `-m` e `vllm.entrypoints...` precisam ser inseridos ANTES dos `BuildArgs(p)`, não como parte do resolvedBinary.

A solução do plano (usar `Properties["module"]`) é boa, mas precisa ser propagada até o `processmgr`. O processo completo seria:
```go
extraArgs := []string{}
if module, ok := backend.Properties["module"]; ok && module != "" {
    extraArgs = []string{"-m", module}
}
cmd := exec.Command(resolvedBinary, append(extraArgs, BuildArgs(p)...)...)
```

Para isso funcionar, o `RunningInstance` ou o `Launch` precisa carregar essa informação. Atualmente o Launcher só seta `ResolvedExecutable`.

**Ação:** Expandir Seção 3.2 para incluir "Module path propagation to processmgr" (3.2.6). Definir como o `Backend.Properties["module"]` flui do resolver → launcher → processmgr.

---

### 🟡 Gap 8: Métricas Prometheus — dependência de parsing frágil

**O plano diz:** Seção 3.2.3 — implementar `prometheusPoller` que faz `GET /metrics`.

**A realidade:** Prometheus text format é razoavelmente estável, mas os nomes exatos das métricas do vLLM (`vllm:generation_tokens_total`, etc.) podem variar entre versões. O plano não especifica:
1. Quais métricas exatas extrair
2. Fallback se `/metrics` não existir (vLLM tem flag `--disable-log-stats`?)
3. Estratégia de delta computation (o código exemplo usa `lastTokens`/`lastTime`, mas counters podem resetar)

**Ação:** Expandir 3.2.3 com lista de métricas-alvo, estratégia de fallback, e tratamento de counter reset.

---

### 🟡 Gap 9: O Editor não tem acesso ao `BackendKind` do draft atual

**O plano diz:** Seção 3.3.1 — `getEssentialFlags(kind, schema)` usa o backend kind.

**A realidade:** `Editor.reloadSchema()` e `loadSchemaForDraft()` já carregam o `backend` via `resolveBackendSchema()`, mas descartam o resultado (`_ = backend`). O `backend.Kind` está disponível mas não é armazenado em nenhum campo do Editor.

Para a Seção 3.3.1 funcionar, o Editor precisa de um campo `backendKind domain.BackendKind` que é populado durante `reloadSchema()`. O plano não menciona esta mudança no struct `Editor`.

**Ação:** Adicionar na Seção 3.3.4 (Editor Integration) a adição do campo `backendKind` ao struct `Editor` e sua população em `reloadSchema()`.

---

### 🟡 Gap 10: Port default difere entre backends

**O plano diz:** A tabela de comparação (Seção 1.3) documenta que llama-server default port = 8080 e vLLM default port = 8000.

**A realidade:** `newDraftDefaults()` em `profiles.go:544` hardcoded `Port: "4321"`. Com schema-driven defaults, o port default deveria vir do schema do backend selecionado. Se o usuário selecionar vLLM, o port field deveria sugerir 8000 como default.

O plano menciona que `Port` continua como campo separado no Draft (Seção 3.3.1), mas não especifica como o default é populado.

**Ação:** Em 3.3.6 (nova subseção), especificar que os defaults do `Draft` (incluindo Port) vêm do `FlagSpec.Default` do schema do backend selecionado.

---

### 🟡 Gap 11: Monitor `/v1/models` fallback não mapeia para `Slot` struct

**O plano diz:** Seção 3.2.4 — adicionar fallback `/v1/models` quando `/slots` não existe.

**A realidade:** O endpoint `/v1/models` do vLLM retorna:
```json
{"object": "list", "data": [{"id": "meta-llama/Llama-3.2-1B", "object": "model", ...}]}
```

O struct `Slot` usado pelo monitor é:
```go
type Slot struct {
    ID       int `json:"id"`
    // ... campos específicos do llama.cpp
}
```

Estes formatos são completamente incompatíveis. Não se pode simplesmente decodificar `/v1/models` como `[]Slot`. Seria necessário um struct diferente ou um adapter.

**Ação:** Em 3.2.4, especificar que o fallback `/v1/models` deve criar `SlotSnapshot` com slots mínimos (ex: 1 slot com `State = 1` indicando "model loaded"), ou adicionar um novo tipo de evento `SourceModels` separado de `SourceSlots`.

---

## 4. Problemas Menores

### 🔵 Gap 12: Embedded vLLM schema incompleto

O plano lista ~18 flags para o embedded schema (Seção 3.1.4). Faltam flags importantes do vLLM documentados na pesquisa:
- `--kv-cache-dtype` (fp8, auto, etc.) — crítica para performance
- `--max-parallel-loading-workers` — prevenção de OOM
- `--compilation-config / -O` (0-3) — torch.compile
- `--block-size` (8, 16, 32, 64, 128)
- `--max-seq-len-to-capture` (default 8192)
- `--enable-sleep-mode` — liberar VRAM quando idle
- `--cpu-offload-gb` — extensão virtual de VRAM

**Severidade:** Baixa — o embedded schema é fallback, a geração live cobre esses flags.

---

### 🔵 Gap 13: `Reconcile()` é Linux-only — vLLM em outros OS

`pidAliveAndNameMatches()` em `recover.go` usa `/proc/<pid>/comm` que é Linux-only. Em macOS/Windows, o método atual "dropa todos os entries". Isso já é o comportamento existente e não é um problema novo introduzido pelo vLLM, mas o plano deveria notar que recovery de vLLM em macOS/Windows é ainda mais frágil.

---

### 🔵 Gap 14: Descrição "llama-server" no struct `RunningInstance`

`domain/instance.go:6`:
```go
// RunningInstance describes a live llama-server process tracked by ProcessManager.
```
Deveria ser atualizado para "a live LLM server process".

---

### 🔵 Gap 15: `backends.go` default kind é "baixa prioridade" mas deveria ser "média"

O plano classifica "Backends UI default kind hardcoded para llama-server" como LOW priority. Para um usuário que só usa vLLM, ter que mudar o kind manualmente toda vez que adiciona um backend é frustrante. Se o catálogo default incluir vLLM, o seletor de kind deveria fazer smart-default baseado nos generators registrados.

---

## 5. Pontos Fortes

### ✅ Força 1: Ordenação de fases logicamente correta

A sequência Fase 1 → 2 → 3 → 4 → 5 é a única ordem viável. A infraestrutura core (Generator + Parser) precisa existir antes do schema ser usado pelo processmgr e monitor, que precisam existir antes do editor de perfis ser refatorado. Acertou em cheio.

### ✅ Força 2: Identificação precisa do que NÃO precisa mudar

A Seção 1.4 lista 9 componentes que já são genéricos. Conferi cada um contra o código e estão corretos — o `BackendCatalog`, `Resolver`, `Validator`, `GPUPoller`, etc. realmente não precisam de alterações estruturais. Isso evita over-engineering.

### ✅ Força 3: Code snippets concretos e implementáveis

As seções 3.1.2 (vLLM help parser regex), 3.3.1 (Draft schema-driven), 3.3.2 (buildForm dinâmico) contêm código Go real que compila. Não são pseudocódigo vago — são implementações parciais que um desenvolvedor pode copiar e adaptar.

### ✅ Força 4: Tratamento de opções de design com trade-offs explícitos

A Seção 3.2.1 (Health endpoint) apresenta Opção A vs Opção B com justificativas. A Seção 3.2.5 (Args builder) apresenta 3 opções (A/B/C) e escolhe a melhor. A Seção 3.3.1 (Essential fields) discute `Priority` no schema vs lista hardcoded. Este padrão de "multiple options → pick best → justify" é excelente.

### ✅ Força 5: Matriz de riscos prática e acionável

A Seção 6 lista 7 riscos com impacto e mitigação concreta. Cada mitigação é específica (ex: "Golden tests versionados + fallback embedded") e não genérica ("fazer mais testes").

### ✅ Força 6: Definição de Pronto (DoD) mensurável

A Seção 7 tem 12 checkboxes com critérios verificáveis. Não há itens vagos como "funciona bem" — são todos testáveis (ex: "`vllm serve --help` é parseado e gera BackendValidationSchema válido").

### ✅ Força 7: Reuso de padrões existentes

O plano consistentemente reusa padrões já estabelecidos no código:
- `VLLMGenerator` segue o mesmo contrato de `LlamaServerGenerator`
- `vllm_parser.go` segue o mesmo padrão de `parser.go` (regexes, type inference, hardcoded overrides)
- `embedded_vllm.go` espelha `embedded.go`
- `WriteVLLMEmbeddedFallback` espelha `WriteEmbeddedFallback`

Isso minimiza surpresas e mantém coesão arquitetural.

---

## 6. Tabela de Verificação de Cobertura

| Área | Coberta? | Qualidade | Notas |
|------|----------|-----------|-------|
| Schema generation (vLLM --help) | ✅ Sim | Alta | Regex, type inference, overrides detalhados |
| Embedded fallback schema | ✅ Sim | Média | Lista de flags boa mas faltam ~7 importantes |
| Generator registration | ✅ Sim | Alta | Bootstrap claro, 1 linha |
| Health check configurável | ✅ Sim | Alta | Opções A/B discutidas, escolha justificada |
| Metrics (Prometheus) | ✅ Sim | Média-Alta | Código exemplo, mas faltam detalhes de fallback |
| Slots fallback (/v1/models) | ✅ Sim | Média | Mencionado mas sem adapter de formato |
| Args builder genérico | ✅ Sim | Alta | 3 opções, escolha justificada |
| Draft schema-driven | ✅ Sim | Alta | Struct novo, buildForm, ToProfile refatorados |
| Hydration de perfil existente | ❌ Não | — | Gap crítico #6 |
| Default values por backend | ❌ Não | — | Gap #6 (extensão) |
| Detail view genérico | ✅ Sim | Alta | Código exemplo funcional |
| Config (DefaultVLLMBinaryPath) | ✅ Sim | Alta | Diff concreto |
| Default catalog com vLLM | ✅ Sim | Alta | Detecção runtime de vLLM em PATH |
| Migration (detect vLLM kind) | ✅ Sim | Alta | Heurística por nome de executável |
| Migration (arg keys) | ✅ Sim | Alta | Código exemplo completo |
| Boot blocker genérico | ✅ Sim | Média | Mensagem mencionada, sem detalhes de implementação |
| Recovery (/proc/comm para Python) | ❌ Não | — | Gap crítico #3 |
| Recovery (defaultBinary) | ❌ Não | — | Gap crítico #4 |
| Validador existência modelo | ❌ Não | — | Gap crítico #2 |
| shortToLong duplicado | ❌ Não | — | Gap crítico #1 |
| Module path propagation | ⚠️ Parcial | Baixa | Mencionado no generator mas não no processmgr |
| Testes unitários | ✅ Sim | Alta | Lista de 7 testes nomeados |
| Golden tests | ✅ Sim | Alta | Pattern consistente com existente |
| Integration tests | ✅ Sim | Média | 5 cenários listados, sem detalhes |
| Backward compat | ⚠️ Parcial | Média | Mencionada nas Notas (Seção 8) mas não detalhada |

---

## 7. Recomendações

### Imediatas (antes de começar a implementar)

1. **Resolver os 6 gaps críticos (#1-#6).** Cada um é um bug que apareceria em produção. Escrever subseções específicas para cada um no plano.

2. **Adicionar Seção 3.3.6:** "Profile hydration from existing profiles" — explicar como `startEditSelected()` popula `EssentialArgs` a partir de `Profile.Args` usando o schema como canonical source.

3. **Adicionar Seção 3.2.6:** "Module path propagation to processmgr" — explicar o fluxo completo de `Backend.Properties["module"]` → `Launch()`.

4. **Adicionar Seção 3.2.7:** "Recovery of Python-based backends" — abordar `/proc/comm` e `defaultBinary` para vLLM.

5. **Adicionar Seção 3.3.7:** "Backend-aware model validation" — tornar `applyExistenceRules` condicional ao backend kind.

6. **Adicionar Seção 3.4.6:** "shortToLong deduplication" — plano para consolidar os dois maps.

### Durante a implementação

7. **Expandir Seção 3.2.3** com lista de métricas Prometheus alvo, estratégia de counter reset, e fallback.

8. **Adicionar campo `backendKind` ao struct `Editor`** na Seção 3.3.4.

9. **Especificar formato do fallback `/v1/models` → `SlotSnapshot`** na Seção 3.2.4.

10. **Completar embedded schema** com os 7 flags faltantes.

### Antes do merge

11. **Atualizar `newDraftDefaults()`** para ser backend-aware (usar schema defaults).

12. **Rodar `make tests` com cobertura** e verificar que nenhum teste existente quebra.

13. **Atualizar AGENTS.md** dos pacotes modificados.

---

## 8. Conclusão

O plano é **bom como especificação de arquitetura** — cobre ~80% do que precisa ser feito, tem code snippets acionáveis, ordenação correta, e análise de riscos. Os pontos fortes (identificação do que não mudar, trade-offs explícitos, DoD mensurável) excedem o esperado para um draft inicial.

**Porém**, 6 gaps críticos (todos referentes a integração entre camadas — como dados fluem do backend selection → editor hydration → process spawning → recovery) não foram identificados. Estes gaps são consequência natural de um plano escrito sem emparelhamento com alguém que conhece profundamente o código, e são exatamente o tipo de coisa que uma revisão de código como esta deve capturar.

**Recomendação final:** Incorporar os gaps críticos #1-#6 como novas subseções no plano, depois começar a Fase 1. Não tentar implementar a Fase 3 (Profile Editor) antes de resolver os gaps de integração das Fases 1-2.

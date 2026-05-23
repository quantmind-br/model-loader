# Relatório de Testes Funcionais — model-loader

**Data:** 2026-05-23
**Branch:** `claude/test-features-profiles-report-t5tvG`
**Commit:** `bcd25ef`
**Toolchain:** Go 1.26.2
**Binário testado:** `bin/model-loader` (via `make build`)

**Método:** exercício prático ponta-a-ponta da CLI e da TUI contra um `HOME`
isolado (`/tmp/ml-test/home`, sem tocar a config real do usuário). Criação e
validação de profiles para **todos os backends suportados**, matriz de validação
(válida + inválida), CRUD completo, export/import, consistência com
`docs/profile-schema.json`, proxy `serve`, scanner/HF, instâncias, e smoke da TUI
sob `tmux`. Binários de backend "fake" foram colocados no `PATH` para destravar os
caminhos que exigem executável.

> **Importante (limites do ambiente):** este container **não possui** `llama-server`
> real, GPU nem `nvidia-smi`. Portanto: lançamento real de inferência, monitoramento
> de GPU e *execução* de benchmark não puderam ser exercidos de verdade — foram
> testados até a falha esperada de "binário/GPU ausente", que é em si um resultado
> válido. Há acesso à rede (HuggingFace acessível) e `python`/`python3` presentes.

---

## Resumo executivo

A engine de **validação**, o **CRUD de profiles**, **export/import**, o **proxy
`serve`**, o **scanner/HF**, o **catálogo de backends** e a **TUI** (incl. editor
web) funcionam bem. A consistência entre o profile persistido e o schema canônico
(`docs/profile-schema.json`) está **sem drift** (5/5 validam em draft-2020-12).

Foram encontrados **11 problemas**. **2 são de severidade ALTA** e tornam a criação
de profiles via CLI essencialmente quebrada fora do "caminho feliz":

| # | Severidade | Resumo |
|---|-----------|--------|
| 1 | 🔴 ALTA | `profile create --backend X` valida contra o backend **default**, não contra `X` (bug de ordem em `runProfileWrite`) |
| 2 | 🔴 ALTA | Validar/criar profile **exige o binário do backend instalado**; ausência → schema vazio → todo flag vira "unknown flag" |
| 3 | 🟠 MÉDIA | `TestManager_CancelQueuedRecord` tem **deadlock latente** por ordem de `defer` → trava 10 min sob carga (flaky) |
| 4 | 🟠 MÉDIA | backend kind `tabbyapi` é **inutilizável** (sem generator registrado) |
| 5 | 🟠 MÉDIA | Inconsistências de tipo/curadoria entre esquemas (`max-model-len` string no vLLM; `n-gpu-layers` string no Buun; seed do Buun aponta flags inexistentes) |
| 6 | 🟡 BAIXA | Não há `backend add` / `backend delete` na CLI (assimetria CLI ↔ TUI) |
| 7 | 🟡 BAIXA | Warning de boot ruidoso/enganoso em **todo** comando quando o binário default falta |
| 8 | 🟡 BAIXA | Caminhos de validação dormentes por padrão (`Required` e `Rules` nunca disparam; sem ranges inteiros) |
| 9 | ⚪ Cosmética | Aba Models exibe erro `lstat` cru para search paths inexistentes |
| 10 | ⚪ Cosmética | `model list` mostra `SIZE 8B` para arquivo de 8 bytes (ambíguo com "8 Bilhões") |
| 11 | ⚪ Info | `backend schema show` logo após `refresh` não reflete a presentation (só semeada no Bootstrap) |

---

## Cobertura de testes

Legenda: ✅ OK · ⚠️ OK com ressalva · ❌ Bug · 🚫 N/A neste ambiente

### CLI por comando

| Área | Comandos | Resultado |
|------|----------|-----------|
| Profile CRUD | `list`, `show` (+`--json`), `create`, `edit`, `delete`, `duplicate`, `rename`, `pin`/`unpin`, `validate` | ✅ |
| Profile create por flag `--backend` | `create --backend <não-default> --arg …` | ❌ (#1, #2) |
| Profile create por `--file` | `create -f file.json` (com `launch.backendId`) | ✅ (workaround de #1) |
| Profile bundle | `export` (stdout / `-o`), `import` (round-trip) | ✅ |
| Guard de id duplicado | `create` com id existente | ✅ (exit 1) |
| Backend | `list`, `show` (+`--json`), `probe`, `schema show/refresh/apply` | ✅ |
| Backend `add`/`delete` | — | 🚫 (#6, não existe) |
| Schema refresh por kind | llama/vllm/sglang/dflash/buun | ✅ |
| Schema refresh tabbyapi | `backend schema refresh tabby-default` | ❌ (#4) |
| Model | `list` (+`--path`,`--json`), `search`, `info` | ✅ (HF ao vivo) |
| Instance | `list`, `show`, `history`, `start`, `stop` | ✅ / 🚫 (start real precisa de binário) |
| Serve | `serve` + `/v1/models`, `/health`, SIGTERM | ✅ |
| Benchmark | `--list`, `--profile` (sem judge) | ✅ (falha rápida, exit 1) |
| Flags globais | `--json`, `--log-level`, `--help`, subcomando inválido | ✅ |

### Profiles por backend (criados e validados)

| Backend | Kind | Profile válido | Schema (flags) | Observação |
|---------|------|----------------|----------------|------------|
| llama.cpp | `llama-server` | ✅ `llama-valid` | 215 | `flash-attn` é **enum** `on/off/auto` (não bool) |
| vLLM | `vllm` | ✅ `vllm-valid` | 283 | `max-model-len` tipado **string** (#5) |
| SGLang | `sglang` | ✅ `sglang-valid` | 330 | aceita repo HF como model |
| DFlash | `dflash` | ✅ `dflash-valid` | 15 | sem presentation logo após refresh (#11) |
| Buun llama.cpp | `buun-llama-cpp` | ✅ `buun-valid` | 158 | `n-gpu-layers` **string**; sem `flash-attn` (#5) |
| TabbyAPI | `tabbyapi` | ❌ | — | sem generator (#4) |

### Matriz de validação (todas retornaram exit 2 com mensagem correta)

| Caso | Entrada | Mensagem |
|------|---------|----------|
| Tipo | `ctx-size="abc"` (int) | `expected int, got string` |
| Enum | `cache-type-k="bogus"` | `"bogus" not in [f32 f16 …]` |
| Porta | `port=99999` | `expected valid port (1-65535), got 99999` |
| Float range | `adaptive-decay=5.0` (max 0.99) | `expected <= 0.99, got 5` |
| Float range (vLLM) | `gpu-memory-utilization=1.5` | `expected <= 1, got 1.5` |
| Flag desconhecido | `totally-made-up=1` | `unknown flag (not in backend schema)` |
| Model inexistente | `/does/not/exist.gguf` | `model file does not exist` |
| ExtraArgs | `["barevalue","--ctx-size"]` | `expected --flag, got bare value` + `missing value for flag` |
| Repo HF (SGLang) | `meta-llama/Llama-3-8B` | aceito (✅) |

---

## Bugs e problemas (detalhe)

### 🔴 #1 — `profile create --backend X` valida contra o backend DEFAULT

**Severidade:** ALTA
**Local:** `internal/cli/profile_edit.go:178-180` (função `runProfileWrite`)

`runProfileWrite` resolve o schema **antes** de aplicar o override de `--backend`:

```go
schema, kind := resolveSchema(deps.resolver, base) // base.Launch.BackendID ainda vazio no create
final := assembleProfile(base, in, schema)         // só aqui base.Launch.BackendID = in.backend
```

No `create`, `base` é um `Profile{}` zero, então `resolveSchema` resolve sempre o
backend **default** do catálogo. O valor de `--backend` nunca é usado para
validar/coagir os args.

**Repro (com `llama-server` fake no PATH para isolar de #2):**
```
$ model-loader profile create --name "vLLM ord" --backend vllm-default \
    --model meta-llama/Llama-3-8B --arg tensor-parallel-size=2
error: tensor-parallel-size: unknown flag (not in backend schema)
error: model: model file does not exist
EXIT=2
```
`tensor-parallel-size` é um flag válido do vLLM, mas é validado contra o schema do
**llama** (default) → "unknown flag". E como o `kind` resolvido é `llama-server`
(não vLLM), o repo HF é tratado como arquivo inexistente.

**Observado:** `--backend` só funciona quando é **igual** ao backend default do
catálogo. Para qualquer backend não-default é impossível criar um profile válido
pela flag `--backend`.
**Esperado:** validar contra o schema do backend indicado em `--backend`.
**Workaround:** usar `--file` com `launch.backendId` no JSON (o overlay de `--file`
ocorre no passo 2, antes do `resolveSchema` no passo 4) — foi assim que todos os
profiles válidos deste relatório foram criados.

### 🔴 #2 — Validação/criação exige o binário do backend instalado

**Severidade:** ALTA
**Local:** `internal/service/backendcatalog/resolver.go:67` + `internal/cli/profile_edit.go:226-234`

`resolver.Resolve` resolve o **executável primeiro** (`resolveExecutable`) e retorna
erro **antes** de carregar o schema quando o binário/runtime não existe. O
`resolveSchema` da CLI engole esse erro e devolve um `FlagSchema{}` vazio:

```go
rb, err := resolver.Resolve(p)
if err != nil { return domain.FlagSchema{}, "" } // schema vazio → tudo "unknown"
```

**Repro (sem `llama-server` no PATH):**
```
$ model-loader profile create --name "Llama Valid" --backend llama-cpp-default \
    --model fake.gguf --arg ctx-size=4096 --arg n-gpu-layers=99
error: ctx-size: unknown flag (not in backend schema)
error: n-gpu-layers: unknown flag (not in backend schema)
EXIT=2
```
Colocando um `llama-server` fake no PATH, o mesmo comando passa (exit 0). Ou seja,
**não é possível redigir/validar um profile para um backend cujo runtime ainda não
está instalado** — um fluxo de preparação muito comum.

**Esperado:** carregar o schema (do store) e validar mesmo sem o binário; a presença
do executável só deveria ser exigida no momento de **lançar** a instância.

### 🟠 #3 — Deadlock latente em `TestManager_CancelQueuedRecord`

**Severidade:** MÉDIA (flaky test → CI trava 10 min)
**Local:** `internal/service/downloadmgr/manager_test.go:118-124`

Os `defer` rodam em LIFO, então `srv.Close()` (linha 124) executa **antes** de
`close(release)` (linha 119). Se o worker do download `first` já tem uma requisição
HTTP em voo (bloqueada no handler em `<-release`), `srv.Close()` espera por uma
conexão que só é liberada por um `defer` posterior → **deadlock**.

**Evidência:** na execução completa de `go test ./...` sob contenção de CPU o pacote
`downloadmgr` **travou e estourou o timeout de 10 min**, com dump de goroutine
mostrando `httptest.Server blocked in Close after 5 seconds, waiting for connections`
e a goroutine presa em `net/http.roundTrip` criada por
`TestManager_CancelQueuedRecord.inProcessSpawner.func2`. Isolado e sem carga o teste
passa em ~0.1s (por isso é flaky).

**Sugestão:** declarar `defer close(release)` por **último** (ou fechar antes de
`srv.Close()`); e, no produto, propagar cancelamento (context) para abortar a
requisição em voo no `Cancel`/`Close`.

### 🟠 #4 — Backend kind `tabbyapi` é inutilizável

**Severidade:** MÉDIA
**Local:** `internal/domain/backend.go:11` (declarado) vs `internal/service/backendschema/register.go:11-16` (não registrado) e `internal/service/backendschema/presentation.go:14` (ausente do `essentialSeed`)

`tabbyapi` é um `BackendKind` declarado e tratado em alguns pontos (ex.
`supportsHFRepo` em `validator/rules.go:309`), mas **não há generator registrado**.

**Repro:**
```
$ model-loader backend schema refresh tabby-default
refresh schema: no generator registered for kind: tabbyapi
EXIT=1
```
Pela TUI, `Manager.AddBackend` cria a entrada, mas a geração de schema também
falharia. Decidir entre **implementar** um generator curado (como vLLM/SGLang) ou
**remover/ocultar** o kind.

### 🟠 #5 — Inconsistências de tipo/curadoria entre esquemas

**Severidade:** MÉDIA
**Locais:** `internal/service/backendschema/curated_vllm.go`, `curated_buun.go`, `presentation.go:16`

- **vLLM `max-model-len` está tipado como `string`** (deveria ser inteiro):
  ```
  error: max-model-len: expected string, got float64   # ao passar 8192 numérico
  ```
  Força o usuário a escrever `"8192"`.
- **Buun `n-gpu-layers` está tipado como `string`**, enquanto no llama a mesma flag
  é `int`. Mesma flag, tipos divergentes entre esquemas.
- **`essentialSeed` do Buun referencia flags inexistentes no schema Buun**:
  `flash-attn`, `draft-max`, `draft-min` (`presentation.go:16`). `BuildPresentation`
  felizmente filtra flags ausentes (`presentation.go:36`), então não há widget órfão
  — mas é dado morto/incorreto que enganaria um refresh do Buun **sem** presentation
  curada. Buun de fato **não possui** flag `flash-attn`.

### 🟡 #6 — Sem `backend add` / `backend delete` na CLI

**Severidade:** BAIXA
**Local:** `internal/cli/backend.go` (só `list`, `show`, `probe`, `schema …`)

Backends só podem ser **criados** pela TUI (`pages/backends.go` → `Manager.AddBackend`).
Não há paridade na CLI, o que impede automação/scripting do catálogo (foi preciso
escrever `catalog.json` à mão para testar os 6 kinds). Sugestão: adicionar
`backend add/delete/set-default`.

### 🟡 #7 — Warning de boot ruidoso e enganoso

**Severidade:** BAIXA
**Local:** `internal/app/bootstrap.go:174`

`warning: default backend schema missing/invalid, using fallback` é impresso em
**stderr a cada invocação** (incluindo `profile list`, `serve`, `model list`) quando
o binário do backend default não está instalado — porque `Resolve` falha na etapa
do executável (ver #2). A mensagem funde "binário ausente" com "schema inválido", o
que confunde o diagnóstico.

### 🟡 #8 — Caminhos de validação dormentes por padrão

**Severidade:** BAIXA
**Local:** esquemas curados + `internal/service/validator/rules.go`

Nos 5 esquemas curados/embedados:
- **Nenhuma flag é `Required`** → `applyRequiredRules` nunca dispara.
- **Nenhum `Rules` (cross-field)** → `applyCrossFieldRules` nunca dispara.
- **Nenhum `Min`/`Max` inteiro** em nenhum schema (só `port` hardcoded 1-65535 e
  ranges de float).

A engine suporta esses caminhos (e funcionam — verificado por leitura/uso), mas
estão inativos out-of-the-box, reduzindo a cobertura real de validação.

### ⚪ #9 — Aba Models exibe erro `lstat` cru
A aba Models renderiza `… [error: lstat /…/.lmstudio/models: no such file]` para
search paths default inexistentes. Cosmético; uma mensagem "path não encontrado"
seria mais limpa.

### ⚪ #10 — `model list` mostra `SIZE 8B` para 8 bytes
Para um arquivo de 8 bytes a coluna `SIZE` mostra `8B` (bytes), visualmente ambíguo
com contagem de parâmetros ("8B" = 8 bilhões). Cosmético.

### ⚪ #11 — `backend schema show` não reflete a presentation logo após `refresh`
`EnsurePresentations` roda apenas em `app.Bootstrap` (`bootstrap.go:118`), não no
caminho CLI de schema (`buildSchemaManager`). Logo após `backend schema refresh`,
um `backend schema show` pode mostrar 0 grupos até o próximo Bootstrap semear a
presentation. Informativo.

---

## O que funciona bem (pontos positivos)

- **Engine de validação**: tipo, enum, porta, range de float, flag desconhecido,
  existência de model, `extraArgs`, e liberação de repo HF para vLLM/SGLang/TabbyAPI
  — todas precisas, com **exit code 2** correto.
- **CRUD de profiles** completo: create/edit/list/show/duplicate/rename/pin/unpin/
  validate/delete, com guard de id duplicado (exit 1).
- **Export/Import** de bundle: round-trip 6/6 em sandbox limpo.
- **Consistência de profiles ↔ `docs/profile-schema.json`**: os 5 profiles validam
  em **JSON Schema draft 2020-12**; chaves de `launch`/`meta` em sincronia com o doc
  (`additionalProperties:false`, nenhuma chave não documentada) — **sem drift**.
- **Catálogo de backends**: `list`/`show`/`probe`/`schema show`/`refresh` para os 5
  kinds reais; `probe` classifica corretamente OK/WARN/ERR (vLLM/SGLang = WARN por
  módulo Python ausente; TabbyAPI = ERR binário não encontrado).
- **Proxy `serve`**: expõe `/v1/models` no formato OpenAI (200), `/health` 503 sem
  instância ativa, e shutdown limpo no SIGTERM.
- **Scanner/HF**: `model list`, `model search` e `model info` funcionam (HF ao vivo).
- **Instâncias**: list/show/history/start/stop; o launcher resolve o binário, spawna
  e registra a instância.
- **TUI**: as 5 abas renderizam, navegação por número funciona, o **editor web**
  (`configweb`) sobe em `127.0.0.1:<porta>` e serve a página HTMX/Alpine completa
  (seletor de backend, grupo Essentials, `/validate` ao vivo), e `q` encerra limpo.
- **Suite automatizada**: `go vet ./...` limpo; `go test ./...` passa em todos os
  pacotes (33 OK), exceto a flakiness do `downloadmgr` sob carga (ver #3).

---

## Sugestões de melhoria (priorizadas)

1. **(#1)** Em `runProfileWrite`, aplicar o overlay de `--backend`/`--model`/`--file`
   **antes** de `resolveSchema`, ou re-resolver o schema após o overlay. É o conserto
   de maior impacto.
2. **(#2)** Desacoplar a resolução de schema da resolução de executável: carregar o
   schema do store mesmo sem binário (avisar, não falhar). Exigir o executável apenas
   no lançamento.
3. **(#3)** Corrigir a ordem de `defer` no teste e propagar `context` de cancelamento
   ao worker para abortar requisições em voo no `Cancel`/`Close`.
4. **(#4)** Implementar um generator curado para `tabbyapi` (ou remover/ocultar o
   kind e adicioná-lo ao `essentialSeed` se mantido).
5. **(#5)** Corrigir tipos curados (`vllm max-model-len`→int; `buun n-gpu-layers`→int)
   e remover as entradas mortas do `essentialSeed` do Buun.
6. **(#6)** Adicionar `backend add/delete/set-default` à CLI para paridade e automação.
7. **(#7)** Disparar o warning de boot só quando o schema for de fato inválido/ausente
   (não quando apenas o binário default falta), e suprimi-lo em comandos que não
   precisam de schema de backend.
8. **(#8)** Considerar semear ao menos `Required`/ranges/`Rules` mínimos nos esquemas
   curados para que a validação cross-field e de obrigatoriedade tenha efeito real.
9. **(#9/#10/#11)** Ajustes cosméticos/menores: mensagem de path não encontrado na
   aba Models; formatação de tamanho que evite ambiguidade com contagem de parâmetros;
   rodar `EnsurePresentations` também no caminho CLI de schema.

---

## Apêndice — transcrições notáveis

**Bug #1 (ordem) com `llama-server` fake no PATH:**
```
$ profile create --backend llama-cpp-default --arg ctx-size=4096 ... → created (exit 0)   # default == --backend, coincide
$ profile create --backend vllm-default --arg tensor-parallel-size=2 ...
  error: tensor-parallel-size: unknown flag (not in backend schema)
  error: model: model file does not exist
  EXIT=2
```

**Bug #2 (binário ausente) — sem fake no PATH, todo flag vira unknown:**
```
$ profile create --backend llama-cpp-default --arg ctx-size=4096 --arg n-gpu-layers=99 ...
  warning: default backend schema missing/invalid, using fallback
  error: ctx-size: unknown flag (not in backend schema)
  error: n-gpu-layers: unknown flag (not in backend schema)
  EXIT=2
```

**Bug #3 (dump real do timeout no `go test ./...`):**
```
httptest.Server blocked in Close after 5 seconds, waiting for connections:
panic: test timed out after 10m0s
… created by …downloadmgr.TestManager_CancelQueuedRecord.inProcessSpawner.func2
```

**`backend probe` (catálogo com 6 kinds):**
```
BACKEND            STATUS  LATENCY  DETAIL
llama-cpp-default  OK      1ms      fake
vllm-default       WARN    40ms     exit status 1
sglang-default     WARN    40ms     exit status 1
dflash-default     OK      1ms      fake
buun-default       OK      1ms      fake
tabby-default      ERR     0ms      binary not found: tabby-api
```

**`serve` — `/v1/models` no formato OpenAI:**
```
{"object":"list","data":[{"id":"buun-valid",…},{"id":"llama-valid",…},{"id":"vllm-valid",…}]}
```

# Models Tab — Hugging Face Search & Download

## TL;DR

> **Quick Summary**: Estender a aba Models do TUI (`internal/ui/pages/models.go`) com busca de modelos no Hugging Face Hub público e download não-bloqueante (até 3 simultâneos, canceláveis), mantendo 100% da funcionalidade atual de listagem/filtro/rescan/menu-de-ações de GGUFs locais. Auto-rescan ao concluir cada download faz o arquivo aparecer na tabela local sem ação manual.
>
> **Deliverables**:
> - Pacote `internal/service/hfhub/` — cliente HTTP stdlib para `/api/models?search=`, `/api/models/{id}`, `/{id}/resolve/main/{file}`.
> - Pacote `internal/service/downloadmgr/` — gerenciador de downloads concorrentes (worker pool=3, fila FIFO, cancelamento por `ctx`, evento por progresso/conclusão/falha).
> - Componentes UI novos em `internal/ui/components/`: `hf_search_picker.go`, `hf_file_picker.go`, `download_progress.go`.
> - Extensão de `internal/ui/pages/models.go`: novas mensagens, novos campos de estado, novos handlers, `IsCapturingInput` estendido, key bindings `s`/`g`/`x`, View com overlays e rodapé de progresso.
> - Wiring em `cmd/model-loader/main.go`: instanciar HF client + download manager e injetar via builders.
>
> **Estimated Effort**: Large
> **Parallel Execution**: YES — 4 ondas (wave 1: 6 tasks, wave 2: 6 tasks, wave 3: 4 tasks, wave final: 4 reviews)
> **Critical Path**: T1/T2/T3 (foundation) → T7/T8 (impl HTTP+mgr) → T12 (page handlers) → T13/T14/T15 (wire) → F1-F4 → user okay

---

## Context

### Original Request
Estender a aba **Models** (a quarta aba do TUI, definida em `internal/ui/pages/models.go`) para incorporar busca e download de modelos do Hugging Face, mantendo intacta a funcionalidade atual de listagem dos GGUFs presentes nos paths configurados.

### Interview Summary

**Decisões confirmadas pelo usuário**:
- Snapshot não-GGUF vai em subpasta `{org}__{repo}/` dentro de `cfg.Models.SearchPaths[0]`. Arquivos `.gguf` individuais ficam soltos no root.
- Conflito de nome (arquivo já existe): **skip** com flash `"already exists: {name}"`. Não sobrescreve, não renomeia.
- Concorrência: max 3 downloads simultâneos, fila FIFO p/ excedentes, cada download cancelável via `ctx.CancelFunc`.
- Tecla `s` abre busca HF (não conflita com filtro local `/`).
- Resultados HF mostram marcador `[GGUF]` por linha; toggle `g` filtra "só GGUF".
- Layout de progresso: 1 linha por download ativo no rodapé fixo (acima do flash/statusbar). Some quando lista vazia.
- Testes: TDD no HF client + download manager (httptest); tests-after nos overlays UI (teatest).

**Defaults sensatos aplicados (não-bloqueantes)**:
- Persistência entre sessões: NÃO. `quit` cancela todos os downloads.
- Cache de busca: NÃO. Cada busca refaz request.
- Revision HF: hardcoded `main`.
- Limite de resultados de busca: 30 (`?limit=30`).
- Timeouts: 30s para busca/info, sem timeout para download (controle por `ctx`).
- User-Agent: `model-loader/{version}` (versão lida de variável `Version` do `cmd/model-loader/main.go`).
- Debounce da busca-enquanto-digita: 300ms.
- Detecção `[GGUF]` no resultado da busca: presença de tag `"gguf"` nos `tags[]` retornados pela API de busca (sem fazer request adicional para cada repo).

### Research Findings

**Padrão arquitetural do scanner é diretamente reaproveitável**: `tea.Cmd` cria `ctx+cancel` e canal Go → Cmd retorna `Msg` com canal e cancel → cada `Msg{Event, Ch}` re-arma o próximo `waitForEvent(Ch)`. Mesmo padrão se aplica a eventos de download.

**`Reloader.Reload()`** (em `internal/ui/root.go:68`) já dispara `modelsReloadMsg` que entra em `beginRescan(false)` (silencioso). Reaproveitamos: ao concluir um download, emitimos `modelsReloadMsg` direto pela página.

**`broadcast()`** (em `internal/ui/root.go:342`) entrega mensagens não-KeyMsg/Mouse/WindowSize a **todas** as pages → eventos de download chegam à Models mesmo quando o usuário está em outra aba (ex.: usuário inicia download, vai à aba Profiles, download conclui, ao voltar a Models já está rescanned).

**`profile_picker.go`** é o template ideal para os pickers HF (lista pré-carregada + filtro interno + cursor).

**`IsCapturingInput()`** atual: `p.action != nil || p.filterMode || p.profilePicker != nil`. Será estendido para incluir os novos overlays.

### Metis Review — Lacunas Endereçadas

**Edge cases incorporados como guardrails ou tasks explícitas**:
- **`SearchPaths` vazio ou diretório inexistente** → bloqueia download com flash `"no download path configured"` (T3). Diretório existente é pré-condição; sem auto-create silencioso de paths arbitrários (auto-create só da subpasta `{org}__{repo}/`).
- **URL encoding de repo IDs** → `{org}/{repo}` mantém `/` literal; usar `url.URL.Path` direto, NUNCA `url.PathEscape` no ID (T7).
- **Filenames com `/`** (HF permite sub-dirs em `rfilename`) → preservar estrutura dentro de `{org}__{repo}/` (T3).
- **Path traversal** → `filepath.Clean` + verificar que resultado tem prefixo de `SearchPaths[0]`; rejeitar com erro (T3).
- **Rate-limit 429** → respeitar header `Retry-After` (até 30s); erro propagado depois (T7).
- **Debounce da busca-enquanto-digita** → 300ms; epoch counter (mesmo pattern do `scanID`) para descartar respostas stale (T9).
- **Cancel de download em curso** → arquivo `.partial` deletado pelo manager no `Cancel` (T8).
- **Colisão de subpasta de snapshot** → mesma regra de skip-on-exists: se `{org}__{repo}/` já existe, flash `"snapshot dir already exists"` e não inicia (T3, T8).
- **Snapshot escopo**: TODOS os arquivos listados em `siblings` do repo (consistente com semântica `snapshot` do HF Python SDK). Sem filtro de extensão.
- **`s` não colide**: só é interpretado fora de `filterMode` (filtro `/` captura runas), confirmado pelo gate atual em `handleKey`.

---

## Work Objectives

### Core Objective
Adicionar à aba Models um fluxo completo de **busca no Hugging Face Hub público** e **download não-bloqueante** de arquivos individuais GGUF ou snapshots completos de repos não-GGUF, com auto-rescan da lista local ao concluir cada download — sem regredir nenhuma das capacidades atuais da aba.

### Concrete Deliverables

**Código novo**:
- `internal/service/hfhub/client.go` + testes — cliente HTTP stdlib (Search, RepoInfo, Download).
- `internal/service/downloadmgr/manager.go` + testes — pool de workers + fila FIFO + cancelamento.
- `internal/service/downloadmgr/pathing.go` + testes — resolve destino, sanitiza, guard contra path traversal.
- `internal/ui/components/hf_search_picker.go` + testes — overlay de busca + lista + filtro `g`.
- `internal/ui/components/hf_file_picker.go` + testes — overlay multi-seleção de arquivos GGUF ou modo snapshot.
- `internal/ui/components/download_progress.go` + testes — rendering das linhas de progresso.

**Código modificado**:
- `internal/ui/pages/models.go` — novos campos, mensagens, handlers, key bindings, View.
- `cmd/model-loader/main.go` — instanciação e injeção do HF client + download manager.
- `internal/ui/root_test.go` — caso de teste para o gate `IsCapturingInput` cobrir os novos overlays.

**Sem alterações**:
- `internal/service/modelscanner/` (consumido inalterado).
- `internal/config/config.go` (lemos `Models.SearchPaths` direto, sem novo campo).
- `internal/domain/model.go`.
- Outras tabs (`launcher`, `profiles`, `monitor`, `backends`, `server`).

### Definition of Done

Comandos verificáveis pelo agente executor:

```bash
# Build limpo
go build ./...                                                     # exit 0
go vet ./...                                                       # exit 0

# Testes
go test ./internal/service/hfhub/...                              # PASS
go test ./internal/service/downloadmgr/...                        # PASS
go test ./internal/ui/components/...                              # PASS
go test ./internal/ui/pages/...                                   # PASS
go test ./internal/ui/...                                         # PASS
make tests                                                        # PASS (suíte completa)

# Comportamento end-to-end (via tmux do TUI rodando)
./bin/model-loader
# → tab 4 (Models)
# → tabela local renderiza com GGUFs existentes
# → tecla `s` abre busca HF (verificável via captura de tela tmux)
# → digitar termo → 300ms depois aparecem resultados
# → enter em resultado GGUF → file picker com siblings .gguf
# → space p/ marcar arquivos → enter inicia downloads
# → rodapé mostra N linhas de progresso
# → aba continua interativa (testar filtrar, navegar)
# → ao concluir, flash "downloaded: {name}" e arquivo aparece na tabela local
```

### Must Have

- Aba Models continua listando GGUFs locais via `modelscanner.Scanner` inalterado.
- Filtro `/`, rescan `R`, menu de ações `enter`, `esc` de saída — todos preservados.
- `IsCapturingInput()` retorna `true` em TODOS os overlays editáveis (busca HF, file picker HF, progress focado).
- Tecla `s` abre busca HF (não conflita com filtro local porque filtro exige `/` primeiro).
- Tecla `g` dentro da busca HF alterna toggle "só GGUF".
- Tecla `x` cancela download focado (quando rodapé tem foco) ou close overlay (em busca).
- Download de arquivo .gguf individual cai em `cfg.Models.SearchPaths[0]/{nome-original}`.
- Download de snapshot cai em `cfg.Models.SearchPaths[0]/{org}__{repo}/...` preservando subdirs internos.
- Conflito de nome (arquivo OU subpasta): SKIP com flash `"already exists: {name}"`.
- Max 3 downloads simultâneos; excedentes em fila FIFO.
- Cada download cancelável via `ctx.CancelFunc`; cancelamento limpa `.partial`.
- Auto-rescan via emissão de `modelsReloadMsg` ao concluir download com sucesso.
- Flash claro de sucesso (`"downloaded: {name}"`) E falha (`"download failed: {name}: {err}"`).
- Erros HTTP tratados: 401/403 → flash `"requires authentication; not supported"`; 404 → `"not found"`; 429 → respeitar `Retry-After` então flash apenas se falhar após backoff.
- User-Agent: `model-loader/{Version}` em todo request HTTP.
- Debounce 300ms na busca-enquanto-digita; descarte de respostas stale via epoch counter.

### Must NOT Have (Guardrails)

**Escopo**:
- ❌ NÃO suporta HF_TOKEN, repos gated/privados, login. 401/403 → flash informativo, sem tentativa de auth.
- ❌ NÃO baixa datasets nem Spaces. Apenas `/api/models`.
- ❌ NÃO converte formatos (sem `safetensors → gguf` etc.).
- ❌ NÃO persiste downloads entre sessões. `quit` cancela tudo.
- ❌ NÃO faz cache de busca em disco.
- ❌ NÃO resume downloads interrompidos (cancel apaga `.partial`, restart começa do zero).
- ❌ NÃO suporta múltiplos `SearchPaths` para download (só `[0]`). Demais paths continuam apenas como entrada de scan.

**Arquitetura**:
- ❌ NÃO alterar `internal/service/modelscanner/`. Reaproveitar como caixa-preta.
- ❌ NÃO alterar `internal/config/config.go` (sem novos campos). Ler `cfg.Models.SearchPaths[0]` diretamente.
- ❌ NÃO alterar `internal/domain/`.
- ❌ NÃO alterar outras pages (`launcher`, `profiles`, `monitor`, `backends`, `server`).
- ❌ NÃO usar SDK externo do HF. Apenas `net/http` da stdlib.
- ❌ NÃO importar `github.com/quantmind-br/model-loader/internal/service/*` em `internal/ui/components/*` — componentes definem interface mínima localmente (igual padrão atual de `picker.go:22` com `ModelScanner`).
- ❌ NÃO adicionar dependências em `go.mod` (zero novas libs).
- ❌ NÃO adicionar novos atalhos GLOBAIS em `root.go`. Apenas atalhos LOCAIS da Models page.

**Anti-AI-slop específico**:
- ❌ NÃO criar abstrações genéricas "HTTPClient interface" / "FileWriter interface" sem necessidade — manter concreto até dois call sites pedirem.
- ❌ NÃO escrever JSDoc/godoc-style em todas as funções privadas. Comentários apenas em tipos exportados E em decisões não-óbvias.
- ❌ NÃO criar pacote `internal/util/` ou `internal/helpers/`. Helpers ficam no pacote que os usa.
- ❌ NÃO tratar `os.Stat` retornando `ErrNotExist` como erro — é comportamento esperado em checks de existência.
- ❌ NÃO logar via `fmt.Println` ou `log.Println` — usar logger injetado (ou descartar via `log.Nop()` em testes).
- ❌ NÃO adicionar mais de 4 níveis de aninhamento em handlers. Se passar disso, extrair helper.
- ❌ NÃO criar arquivos `.gguf.partial` no diretório do usuário — usar nome final `.partial` no diretório destino (limpável).

---

## Verification Strategy (MANDATORY)

> **ZERO INTERVENÇÃO HUMANA** — toda verificação é agent-executed.

### Test Decision
- **Infrastructure exists**: YES
- **Automated tests**:
  - **TDD**: `internal/service/hfhub/*` e `internal/service/downloadmgr/*` — RED-GREEN-REFACTOR. Mock via `httptest.Server`.
  - **Tests-after**: overlays UI em `internal/ui/components/hf_*` e `internal/ui/components/download_progress*` — escrever testes com `teatest` após design firmar.
- **Framework**: `go test` stdlib + `charmbracelet/x/exp/teatest` + `net/http/httptest`.

### QA Policy
Toda task inclui cenários QA executados por agente. Evidências em `.sisyphus/evidence/task-{N}-{slug}.{ext}`.

- **HF client + download manager**: `Bash` rodando go test verbose + httptest dump.
- **Overlays UI**: `interactive_bash` (tmux) lançando o TUI sob `TERM=xterm-256color`, capturando frames de `tmux capture-pane -p` em cada interação.
- **Integração ponta-a-ponta**: `interactive_bash` (tmux) com `model-loader` real + servidor HTTP mock local (subindo via `go run testutil/hfmock`) substituindo `huggingface.co` via env var `HF_BASE_URL` (T7 expõe esse hook).

---

## Execution Strategy

### Parallel Execution Waves

```
Wave 1 (Foundation - 6 tasks em paralelo):
├── Task 1: hfhub package skeleton + types       [quick]
├── Task 2: downloadmgr package skeleton + types [quick]
├── Task 3: download path resolver + traversal guard [unspecified-high]  (depende: skeleton de downloadmgr — pode rodar paralelo se concordarmos no contrato de tipos)
├── Task 4: hf_search_picker component skeleton  [visual-engineering]
├── Task 5: hf_file_picker component skeleton    [visual-engineering]
└── Task 6: download_progress component skeleton [visual-engineering]

Wave 2 (Implementations - 6 tasks em paralelo):
├── Task 7:  hfhub client impl (TDD)                          [unspecified-high]  (depende: 1)
├── Task 8:  downloadmgr impl (TDD) + worker pool + fila      [deep]              (depende: 2, 3)
├── Task 9:  hf_search_picker impl + debounce + epoch         [visual-engineering] (depende: 4)
├── Task 10: hf_file_picker impl + multi-select + snapshot    [visual-engineering] (depende: 5)
├── Task 11: download_progress rendering                       [visual-engineering] (depende: 6)
└── Task 12: ModelsPage state additions + IsCapturingInput     [unspecified-high]  (depende: 4, 5, 6 — pode iniciar com stubs)

Wave 3 (Integration - 4 tasks em paralelo):
├── Task 13: main.go wiring (HF client + download manager)    [quick]              (depende: 7, 8)
├── Task 14: ModelsPage key bindings + hints                  [quick]              (depende: 12)
├── Task 15: ModelsPage Update handlers + auto-rescan         [deep]               (depende: 7, 8, 9, 10, 11, 12)
└── Task 16: ModelsPage View extension (overlays + footer)    [visual-engineering] (depende: 12, 15)

Wave FINAL (4 review agents em PARALELO):
├── Task F1: Plan compliance audit (oracle)
├── Task F2: Code quality review (unspecified-high)
├── Task F3: Real manual QA via tmux + hfmock server (unspecified-high)
└── Task F4: Scope fidelity check (deep)
→ Apresentar resultado consolidado → Aguardar "okay" explícito do usuário.

Critical Path: T1 → T7 → T15 → F1-F4 → user okay
Parallel Speedup: ~60% vs sequencial
Max Concurrent: 6 (Waves 1 & 2)
```

### Dependency Matrix

| Task | Depends on | Blocks |
|------|------------|--------|
| 1 (hfhub skeleton) | — | 7 |
| 2 (downloadmgr skeleton) | — | 3, 8 |
| 3 (path resolver) | 2 | 8 |
| 4 (search picker skeleton) | — | 9, 12 |
| 5 (file picker skeleton) | — | 10, 12 |
| 6 (progress skeleton) | — | 11, 12 |
| 7 (hfhub impl) | 1 | 13, 15 |
| 8 (downloadmgr impl) | 2, 3 | 13, 15 |
| 9 (search picker impl) | 4 | 15 |
| 10 (file picker impl) | 5 | 15 |
| 11 (progress impl) | 6 | 15, 16 |
| 12 (ModelsPage state) | 4, 5, 6 | 14, 15, 16 |
| 13 (main.go wiring) | 7, 8 | F-all |
| 14 (ModelsPage keys) | 12 | F-all |
| 15 (ModelsPage handlers) | 7, 8, 9, 10, 11, 12 | 16, F-all |
| 16 (ModelsPage View) | 12, 15 | F-all |
| F1-F4 | 13, 14, 15, 16 | user okay |

### Agent Dispatch Summary

| Wave | Tasks | Distribuição |
|------|-------|--------------|
| 1 | 6 | T1, T2 → `quick`; T3 → `unspecified-high`; T4, T5, T6 → `visual-engineering` |
| 2 | 6 | T7 → `unspecified-high`; T8 → `deep`; T9, T10, T11 → `visual-engineering`; T12 → `unspecified-high` |
| 3 | 4 | T13, T14 → `quick`; T15 → `deep`; T16 → `visual-engineering` |
| FINAL | 4 | F1 → `oracle`; F2, F3 → `unspecified-high`; F4 → `deep` |

---

## TODOs

> Implementação + Teste = UMA Task. Nunca separar.
> TODA task tem: Recommended Agent Profile + Parallelization + QA Scenarios.

- [x] 1. **hfhub package skeleton + types**

  **What to do**:
  - Criar `internal/service/hfhub/types.go` com:
    - `type SearchResult struct { ID, Author, ModelID string; Tags []string; Downloads, Likes int; LastModified time.Time; LibraryName, PipelineTag string }`
    - `func (r SearchResult) HasGGUFTag() bool` — `slices.Contains(r.Tags, "gguf")`
    - `type RepoInfo struct { ID string; Siblings []Sibling; Tags []string }`
    - `type Sibling struct { RFilename string; Size int64 }`
    - `type ErrHTTP struct { Status int; URL string }` com `Error() string` formatado.
  - Criar `internal/service/hfhub/client.go` com:
    - `const DefaultBaseURL = "https://huggingface.co"`
    - `type Client struct { http *http.Client; baseURL string; userAgent string }`
    - `func NewClient(httpClient *http.Client, userAgent string) *Client` (baseURL default; honra env `HF_BASE_URL` se setado — testes/mock).
    - Assinaturas vazias (retornando `nil, errors.New("not implemented")`):
      - `func (c *Client) Search(ctx context.Context, query string, limit int) ([]SearchResult, error)`
      - `func (c *Client) RepoInfo(ctx context.Context, repoID string) (*RepoInfo, error)`
      - `func (c *Client) OpenDownload(ctx context.Context, repoID, filename string) (io.ReadCloser, int64, error)`

  **Must NOT do**:
  - Implementar lógica HTTP real — fica para T7.
  - Adicionar dependência externa.
  - Re-exportar tipos do `internal/domain/`.

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Escrita de types + assinaturas, sem lógica. Trivial.
  - **Skills**: `[]`
    - Nenhuma skill aplicável; Go puro.

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (com T2, T3, T4, T5, T6)
  - **Blocks**: T7
  - **Blocked By**: None

  **References**:
  - `internal/service/modelscanner/modelscanner.go` — padrão de package doc + interface mínima.
  - `internal/service/profilestore/` — padrão de struct concreta com httpClient injetado.
  - Endpoints HF (constantes para uso futuro em T7, NÃO usar agora):
    - `GET /api/models?search={q}&limit={n}` — devolve array de SearchResult.
    - `GET /api/models/{id}` — devolve objeto com `siblings`.
    - `GET /{id}/resolve/main/{file}` — body do arquivo.

  **WHY Each Reference Matters**:
  - `modelscanner.go` mostra o padrão minimalista do projeto (sem over-engineering de interfaces).
  - `profilestore` mostra como um cliente concreto é estruturado com dependências injetadas.

  **Acceptance Criteria**:
  - [ ] `go build ./internal/service/hfhub/...` → exit 0.
  - [ ] `go vet ./internal/service/hfhub/...` → exit 0.
  - [ ] Arquivo `internal/service/hfhub/types.go` existe e exporta `SearchResult`, `RepoInfo`, `Sibling`, `ErrHTTP`.
  - [ ] Arquivo `internal/service/hfhub/client.go` existe e exporta `Client`, `NewClient`, `Search`, `RepoInfo`, `OpenDownload`.
  - [ ] `Client.Search/RepoInfo/OpenDownload` retornam `errors.New("not implemented")` (placeholder explícito).

  **QA Scenarios**:

  ```
  Scenario: Pacote compila e expõe API esperada
    Tool: Bash
    Preconditions: Repo na branch atual.
    Steps:
      1. Rodar: go build ./internal/service/hfhub/...
      2. Rodar: go vet ./internal/service/hfhub/...
      3. Rodar: go doc ./internal/service/hfhub | tee .sisyphus/evidence/task-01-godoc.txt
    Expected Result: exit 0 nos comandos; godoc lista Client, NewClient, Search, RepoInfo, OpenDownload, SearchResult, RepoInfo, Sibling, ErrHTTP.
    Failure Indicators: erro de compilação OU símbolo faltando no godoc.
    Evidence: .sisyphus/evidence/task-01-godoc.txt

  Scenario: Cliente pode ser construído sem panic
    Tool: Bash
    Preconditions: T1 implementado.
    Steps:
      1. Criar arquivo temporário .sisyphus/evidence/task-01-smoke.go com `package main; import ("net/http"; "fmt"; "github.com/quantmind-br/model-loader/internal/service/hfhub"); func main() { c := hfhub.NewClient(http.DefaultClient, "test/0.1"); fmt.Printf("%T\n", c) }`
      2. Rodar: go run .sisyphus/evidence/task-01-smoke.go > .sisyphus/evidence/task-01-smoke.txt
    Expected Result: imprime `*hfhub.Client` sem panic.
    Evidence: .sisyphus/evidence/task-01-smoke.txt
  ```

  **Commit**: YES
  - Message: `feat(hfhub): add package skeleton with types and contracts`
  - Files: `internal/service/hfhub/types.go`, `internal/service/hfhub/client.go`
  - Pre-commit: `go build ./internal/service/hfhub/...`

- [x] 2. **downloadmgr package skeleton + types**

  **What to do**:
  - Criar `internal/service/downloadmgr/types.go`:
    - `type ID string` (UUID-like; usar `time.Now().UnixNano()` como string — sem nova dep).
    - `type Spec struct { RepoID, Filename, URL, DestDir, DestFile string; IsSnapshot bool; Logger *slog.Logger }`
    - `type Status int` com constantes `StatusQueued`, `StatusActive`, `StatusCompleted`, `StatusFailed`, `StatusCancelled`.
    - `type State struct { ID ID; Spec Spec; Status Status; Bytes, Total int64; Err error; StartedAt time.Time }` — snapshot imutável.
    - `type Event struct { ID ID; State State }` — broadcasted no canal de subscribe.
  - Criar `internal/service/downloadmgr/manager.go`:
    - `type Manager struct { ... }` (campos internos a definir em T8).
    - `func NewManager(httpClient *http.Client, maxConcurrent int) *Manager`
    - Assinaturas vazias:
      - `func (m *Manager) Start(spec Spec) (ID, error)` → `return "", errors.New("not implemented")`
      - `func (m *Manager) Cancel(id ID) error` → idem
      - `func (m *Manager) Snapshot() []State` → `return nil` (snapshot da lista de downloads — usado pelo View)
      - `func (m *Manager) Subscribe() <-chan Event` → `return nil`
      - `func (m *Manager) Close() error` → idem (cancela tudo, fecha workers)

  **Must NOT do**:
  - Implementar workers/fila/HTTP — fica para T8.
  - Adicionar dependência externa (UUID lib etc.).

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Types-only.
  - **Skills**: `[]`

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (com T1, T3, T4, T5, T6)
  - **Blocks**: T3, T8
  - **Blocked By**: None

  **References**:
  - `internal/service/processmgr/` — padrão de Manager com worker/recover/Close.
  - `internal/service/modelscanner/modelscanner.go` — interface contract minimalista.

  **WHY Each Reference Matters**:
  - `processmgr` é o exemplo mais próximo no projeto de um Manager com goroutines + Close. Replicar disciplina.

  **Acceptance Criteria**:
  - [ ] `go build ./internal/service/downloadmgr/...` → exit 0.
  - [ ] `go vet ./internal/service/downloadmgr/...` → exit 0.
  - [ ] Tipos exportados: `Manager`, `NewManager`, `Spec`, `ID`, `Status` (com constantes), `State`, `Event`.
  - [ ] Métodos placeholder retornam erro `"not implemented"` (não nil silencioso).

  **QA Scenarios**:

  ```
  Scenario: Pacote compila e expõe API esperada
    Tool: Bash
    Preconditions: Repo limpo.
    Steps:
      1. Rodar: go build ./internal/service/downloadmgr/...
      2. Rodar: go doc ./internal/service/downloadmgr | tee .sisyphus/evidence/task-02-godoc.txt
    Expected Result: exit 0; godoc lista Manager, NewManager, Spec, ID, Status (com StatusQueued/Active/Completed/Failed/Cancelled), State, Event.
    Evidence: .sisyphus/evidence/task-02-godoc.txt

  Scenario: Manager.Start retorna erro "not implemented" sem panic
    Tool: Bash
    Steps:
      1. Criar smoke test em .sisyphus/evidence/task-02-smoke.go invocando NewManager(http.DefaultClient, 3) e Start(Spec{}).
      2. go run ... > .sisyphus/evidence/task-02-smoke.txt
    Expected Result: imprime "not implemented" sem panic.
    Evidence: .sisyphus/evidence/task-02-smoke.txt
  ```

  **Commit**: YES
  - Message: `feat(downloadmgr): add package skeleton with types and Manager interface`
  - Files: `internal/service/downloadmgr/types.go`, `internal/service/downloadmgr/manager.go`
  - Pre-commit: `go build ./internal/service/downloadmgr/...`

- [x] 3. **Download path resolver + traversal guard (TDD)**

  **What to do**:
  - Criar `internal/service/downloadmgr/pathing.go` com:
    - `func ResolveDest(searchPaths []string, repoID, rfilename string, isSnapshot bool) (destDir, destFile string, err error)`
    - Comportamento:
      - Se `searchPaths` vazio → erro `ErrNoSearchPath`.
      - `root := searchPaths[0]`. `root` deve existir como diretório (`os.Stat` + `IsDir()`) — se não, erro `ErrSearchPathMissing`.
      - Para `isSnapshot=false` (arquivo .gguf individual): `destDir = root`, `destFile = filepath.Base(rfilename)`.
      - Para `isSnapshot=true`: `destDir = filepath.Join(root, sanitizeRepoID(repoID))` onde `sanitizeRepoID("meta-llama/Llama-3.1-8B-Instruct") == "meta-llama__Llama-3.1-8B-Instruct"`. `destFile = rfilename` (preserva sub-dirs internos como `config/something.json`).
      - **Path traversal guard**: `cleaned := filepath.Clean(filepath.Join(destDir, destFile))`. Se `!strings.HasPrefix(cleaned, filepath.Clean(root)+string(os.PathSeparator))` → erro `ErrPathTraversal`.
    - `func sanitizeRepoID(id string) string` — substitui `/` por `__`; remove qualquer `..`; trim de leading/trailing `/`.
    - `var ErrNoSearchPath, ErrSearchPathMissing, ErrPathTraversal = errors.New(...)`
  - Criar `internal/service/downloadmgr/pathing_test.go` (TDD — escrever ANTES da implementação) com tabela cobrindo:
    - Search paths vazio → erro.
    - Path inexistente → erro.
    - Arquivo simples GGUF: `("/tmp/models", "TheBloke/Llama-2-7B-GGUF", "llama-2-7b.Q4_K_M.gguf", false)` → `("/tmp/models", "llama-2-7b.Q4_K_M.gguf", nil)`.
    - Snapshot: `("/tmp/models", "meta-llama/Llama-3.1-8B-Instruct", "config.json", true)` → `("/tmp/models/meta-llama__Llama-3.1-8B-Instruct", "config.json", nil)`.
    - Snapshot com subpasta interna: `(..., "model-00001-of-00003.safetensors", true)` → preserva nome.
    - Snapshot com path traversal: `(..., "../../../etc/passwd", true)` → `ErrPathTraversal`.
    - rfilename absoluto malicioso: `(..., "/etc/passwd", false)` → `filepath.Base` deve neutralizar; sem traversal.
  - Helper de teste cria tmpdir via `t.TempDir()` e usa como `searchPaths[0]`.

  **Must NOT do**:
  - Criar diretórios destino (T8 faz `MkdirAll` antes de escrever).
  - Implementar `CheckExists` ou similar — função PURA, sem side effects.
  - Adicionar dependência externa.

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: Algoritmo pequeno mas crítico (security: path traversal). Requer atenção. TDD facilita.
  - **Skills**: `[]`

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (com T1, T2, T4, T5, T6) — depende dos types de T2 que provavelmente estarão prontos antes.
  - **Blocks**: T8
  - **Blocked By**: T2 (skeleton)

  **References**:
  - Go stdlib `filepath.Clean`, `filepath.Join`, `filepath.Base`, `filepath.IsAbs`.
  - https://owasp.org/www-community/attacks/Path_Traversal — checklist de mitigações.
  - `internal/service/profilestore/profilestore.go` — padrão de validação + erro sentinela exportado.
  - `internal/domain/profile.go:Slugify` — padrão de sanitização de string para nome de arquivo.

  **WHY Each Reference Matters**:
  - OWASP path traversal: cheat sheet das técnicas que precisamos bloquear (`../`, encoded, absolute injection).
  - `Slugify` mostra como o projeto sanitiza identificadores. Não vamos chamar Slugify (manteria letras seguras só) — mas o estilo da função é o modelo.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/service/downloadmgr/... -count=1 -race` → PASS.
  - [ ] Test cases >= 6 (listados em "What to do").
  - [ ] Coverage de `pathing.go` >= 90% via `go test -cover`.
  - [ ] `sanitizeRepoID("a/b")` == `"a__b"`.
  - [ ] `ResolveDest` retorna `ErrPathTraversal` para inputs maliciosos.

  **QA Scenarios**:

  ```
  Scenario: Testes TDD passam todos
    Tool: Bash
    Steps:
      1. go test ./internal/service/downloadmgr/... -count=1 -race -v | tee .sisyphus/evidence/task-03-test.txt
      2. go test ./internal/service/downloadmgr/... -coverprofile=.sisyphus/evidence/task-03-cover.out
      3. go tool cover -func=.sisyphus/evidence/task-03-cover.out | grep -E "pathing.go|ResolveDest" | tee .sisyphus/evidence/task-03-cover.txt
    Expected Result: todos PASS; coverage >= 90% para pathing.go.
    Evidence: .sisyphus/evidence/task-03-test.txt, task-03-cover.txt

  Scenario: Path traversal não escapa do root (negativo)
    Tool: Bash
    Steps:
      1. Test inline (use Bash heredoc) chamando ResolveDest com rfilename="../../../etc/passwd" e isSnapshot=true.
    Expected Result: retorna ErrPathTraversal; cleaned path NÃO contém "/etc/passwd".
    Evidence: .sisyphus/evidence/task-03-traversal.txt
  ```

  **Commit**: YES
  - Message: `feat(downloadmgr): add download path resolver with traversal guard`
  - Files: `internal/service/downloadmgr/pathing.go`, `internal/service/downloadmgr/pathing_test.go`
  - Pre-commit: `go test ./internal/service/downloadmgr/... -race`

- [x] 4. **HFSearchPicker component skeleton**

  **What to do**:
  - Criar `internal/ui/components/hf_search_picker.go`:
    - `type HFSearchPicker struct { input string; epoch int; results []hfhub.SearchResult; cursor int; ggufOnly bool; loading bool; err string; width, height int }`
    - Importa **tipo** `hfhub.SearchResult` apenas — declarar localmente como `HFResult` se queremos manter components sem importar service (`internal/ui/components/picker.go:22` já faz esse padrão com `ModelScanner`).
    - `func NewHFSearchPicker() HFSearchPicker`
    - Stubs:
      - `func (p HFSearchPicker) Init() tea.Cmd { return nil }`
      - `func (p HFSearchPicker) Update(msg tea.Msg) (HFSearchPicker, tea.Cmd) { return p, nil }`
      - `func (p HFSearchPicker) View() string { return "<HF search>" }`
    - Mensagens declaradas exportadas (sem handlers):
      - `HFSearchSubmitMsg struct { RepoID string }` (emitido quando user dá Enter num resultado).
      - `HFSearchCancelledMsg struct{}` (Esc).
      - `HFSearchQueryMsg struct { Query string; Epoch int }` (consumido pela page p/ disparar request).

  **Must NOT do**:
  - Implementar UX/keys — fica para T9.
  - Importar `internal/service/hfhub` diretamente — declarar tipo local mínimo (`HFResult` struct espelho com mesmos campos).

  **Recommended Agent Profile**:
  - **Category**: `visual-engineering`
    - Reason: Componente TUI; mesmo que esqueleto, o agente entende as convenções de Update/View do bubbletea.
  - **Skills**: `[]`

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (com T1, T2, T3, T5, T6)
  - **Blocks**: T9, T12
  - **Blocked By**: None

  **References**:
  - `internal/ui/components/profile_picker.go` — padrão exato a copiar (lista pré-carregada + filtro + cursor + width/height + msgs Submit/Cancel).
  - `internal/ui/components/picker.go:22` — padrão de tipo local `ModelScanner` para não importar `service/`.

  **WHY Each Reference Matters**:
  - `profile_picker.go` é o template literal. Copiar estrutura, ajustar campos.

  **Acceptance Criteria**:
  - [ ] `go build ./internal/ui/components/...` → exit 0.
  - [ ] Tipos exportados visíveis em `go doc`: `HFSearchPicker`, `NewHFSearchPicker`, `HFSearchSubmitMsg`, `HFSearchCancelledMsg`, `HFSearchQueryMsg`, `HFResult`.

  **QA Scenarios**:

  ```
  Scenario: Componente compila e renderiza placeholder
    Tool: Bash
    Steps:
      1. go build ./internal/ui/components/...
      2. Criar smoke em .sisyphus/evidence/task-04-smoke.go construindo NewHFSearchPicker() e printando .View().
      3. go run ... > .sisyphus/evidence/task-04-view.txt
    Expected Result: build PASS; output contém "<HF search>".
    Evidence: .sisyphus/evidence/task-04-view.txt
  ```

  **Commit**: YES
  - Message: `feat(components): add HFSearchPicker skeleton`
  - Files: `internal/ui/components/hf_search_picker.go`
  - Pre-commit: `go build ./internal/ui/components/...`

- [x] 5. **HFFilePicker component skeleton**

  **What to do**:
  - Criar `internal/ui/components/hf_file_picker.go`:
    - `type HFFilePicker struct { repoID string; siblings []FileEntry; selected map[int]bool; cursor int; snapshotMode bool; width, height int }`
    - `type FileEntry struct { Name string; Size int64 }` (tipo local, sem importar `hfhub` no components).
    - `func NewHFFilePicker(repoID string, entries []FileEntry, snapshotMode bool) HFFilePicker`
    - Stubs `Init()/Update()/View()` retornando vazio/zero/placeholder.
    - Mensagens exportadas:
      - `HFFilesSelectedMsg struct { RepoID string; Filenames []string; IsSnapshot bool }` — emitido em Enter.
      - `HFFilePickerCancelledMsg struct{}` — emitido em Esc.

  **Must NOT do**:
  - Implementar multi-select keys — fica para T10.
  - Importar `hfhub` — usar `FileEntry` local.

  **Recommended Agent Profile**:
  - **Category**: `visual-engineering`
    - Reason: Componente TUI.
  - **Skills**: `[]`

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1
  - **Blocks**: T10, T12
  - **Blocked By**: None

  **References**:
  - `internal/ui/components/profile_picker.go` — template; substituir lista de profiles por lista de arquivos.

  **WHY Each Reference Matters**:
  - Mesmo padrão Update/View; só muda o tipo da linha.

  **Acceptance Criteria**:
  - [ ] `go build ./internal/ui/components/...` → exit 0.
  - [ ] Tipos exportados: `HFFilePicker`, `NewHFFilePicker`, `FileEntry`, `HFFilesSelectedMsg`, `HFFilePickerCancelledMsg`.

  **QA Scenarios**:

  ```
  Scenario: Componente compila e renderiza placeholder
    Tool: Bash
    Steps:
      1. go build ./internal/ui/components/...
      2. Smoke construindo NewHFFilePicker("a/b", []FileEntry{{Name:"x.gguf", Size:100}}, false), printando .View() em .sisyphus/evidence/task-05-view.txt.
    Expected Result: build PASS; View imprime placeholder consistente.
    Evidence: .sisyphus/evidence/task-05-view.txt
  ```

  **Commit**: YES
  - Message: `feat(components): add HFFilePicker skeleton`
  - Files: `internal/ui/components/hf_file_picker.go`
  - Pre-commit: `go build ./internal/ui/components/...`

- [x] 6. **DownloadProgress component skeleton**

  **What to do**:
  - Criar `internal/ui/components/download_progress.go`:
    - `type DownloadLine struct { ID, Name string; Bytes, Total int64; Status DownloadStatus }`
    - `type DownloadStatus int` com constantes `DSQueued`, `DSActive`, `DSCompleted`, `DSFailed`, `DSCancelled`.
    - `type DownloadProgress struct { lines []DownloadLine; focused int; focusVisible bool; width int }`
    - `func NewDownloadProgress() DownloadProgress`
    - `func (d DownloadProgress) SetLines(lines []DownloadLine) DownloadProgress` (snapshot do Manager).
    - `func (d DownloadProgress) FocusNext() DownloadProgress`
    - `func (d DownloadProgress) Focused() (DownloadLine, bool)` — devolve linha em foco se focusVisible.
    - `func (d DownloadProgress) View() string` — stub `"<download progress>"`.
    - Helper `formatBytes(n int64) string` privado (replicar logic de `humanSize` em `pages/models.go:424` — `K/M/G`).

  **Must NOT do**:
  - Implementar Update — não é tea.Model, é stateless view component renderizado pela page.
  - Cancelar downloads — só renderiza estado. Cancelamento é decisão da page.

  **Recommended Agent Profile**:
  - **Category**: `visual-engineering`
  - **Skills**: `[]`

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1
  - **Blocks**: T11, T12
  - **Blocked By**: None

  **References**:
  - `internal/ui/pages/models.go:424:humanSize` — função a clonar (não compartilhar para não tornar utility public).
  - `internal/ui/components/statusbar.go` — padrão de componente stateless renderizado por outro model.

  **WHY Each Reference Matters**:
  - `statusbar.go` mostra que nem todo componente é `tea.Model`; alguns são apenas helpers de render — perfeito para progress lines.

  **Acceptance Criteria**:
  - [ ] `go build ./internal/ui/components/...` → exit 0.
  - [ ] Tipos exportados: `DownloadProgress`, `NewDownloadProgress`, `DownloadLine`, `DownloadStatus` (com constantes).
  - [ ] Constantes alinham com `downloadmgr.Status` por nome (DSQueued↔StatusQueued etc.) — page faz a tradução em T15.

  **QA Scenarios**:

  ```
  Scenario: Componente compila e renderiza placeholder
    Tool: Bash
    Steps:
      1. go build ./internal/ui/components/...
      2. Smoke construindo NewDownloadProgress(), printando .View() em .sisyphus/evidence/task-06-view.txt.
    Expected Result: build PASS; View imprime placeholder.
    Evidence: .sisyphus/evidence/task-06-view.txt
  ```

  **Commit**: YES
  - Message: `feat(components): add DownloadProgress skeleton`
  - Files: `internal/ui/components/download_progress.go`
  - Pre-commit: `go build ./internal/ui/components/...`

- [x] 7. **hfhub client implementation (TDD)**

  **What to do**:
  - Implementar `Client.Search` (TDD primeiro):
    - URL: `{baseURL}/api/models?search={url.QueryEscape(q)}&limit={limit}`.
    - Headers: `User-Agent: {c.userAgent}`, `Accept: application/json`.
    - Timeout: usar `ctx`. NÃO setar timeout no `http.Client` — chamador injeta.
    - Parse JSON em `[]SearchResult` (estrutura corresponde aos campos da API HF).
    - 200 → success. 429 → ler `Retry-After`, dormir até 30s, **retry uma única vez**; se ainda 429 → `ErrHTTP{429}`. 4xx/5xx → `ErrHTTP{status, url}`.
  - Implementar `Client.RepoInfo`:
    - URL: `{baseURL}/api/models/{repoID}` — `repoID` injetado **sem `url.PathEscape` no `/` do org**; usar `url.URL{Path: ...}` que percent-encoda só caracteres unsafe individuais.
    - Parse `{ id, tags, siblings: [{rfilename, size?}] }`. `size` pode estar ausente; default 0.
  - Implementar `Client.OpenDownload`:
    - URL: `{baseURL}/{repoID}/resolve/main/{filename}`.
    - `http.Client.CheckRedirect = nil` (default — segue até 10 redirects). Documentar como suficiente para CDN.
    - Headers: `User-Agent` + `Accept: */*`.
    - Retorna `body io.ReadCloser`, `contentLength int64` do header, `err`. Body deve ser fechado pelo chamador.
  - Honrar `HF_BASE_URL` em `NewClient` (se setado, sobrescreve `DefaultBaseURL`).
  - Testes em `internal/service/hfhub/client_test.go` usando `httptest.NewServer`:
    - **Search**: mock /api/models retorna fixture JSON com 3 resultados; assert tipos preenchidos.
    - **Search 429 com Retry-After**: primeiro 429 com `Retry-After: 1`, segundo 200; assert sucesso após retry.
    - **Search 404**: retorna `ErrHTTP{404}`.
    - **RepoInfo**: mock /api/models/meta-llama/Llama-3.1-8B → returns siblings.
    - **RepoInfo com `/` no ID**: assert URL chega ao mock como `/api/models/meta-llama/Llama-3.1-8B` (não percent-encoded).
    - **OpenDownload**: mock /repo/resolve/main/file.bin retorna bytes + Content-Length. Assert lido corretamente.
    - **OpenDownload redirect**: 302 → 200, assert segue.
    - **Context cancel**: cancela ctx mid-request → erro.
  - Fixtures em `internal/service/hfhub/testdata/`: `search_response.json`, `repo_info.json`.

  **Must NOT do**:
  - Implementar resume/Range — não está no escopo.
  - Implementar pagination — limit=30 é suficiente; sem `next_page`.
  - Tratar de 401/403 com lógica de auth — apenas propagar como `ErrHTTP`.
  - Cachear respostas.

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: HTTP client + 8+ casos de teste com httptest. Tarefa de média complexidade.
  - **Skills**: `[]`

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (com T8, T9, T10, T11, T12)
  - **Blocks**: T13, T15
  - **Blocked By**: T1

  **References**:
  - `internal/service/httpproxy/handler_test.go` — padrão de teste com `httptest.NewServer`.
  - https://huggingface.co/docs/hub/api — endpoints e responses oficiais.
  - https://huggingface.co/.well-known/openapi.json — schema autoritativo.
  - Go stdlib: `net/http`, `net/url`, `encoding/json`, `io`, `time` (para Retry-After parse).
  - `internal/service/hfhub/types.go` (de T1) — tipos para deserializar.

  **WHY Each Reference Matters**:
  - `httpproxy/handler_test.go` mostra como o projeto monta servidores de teste; reproduzir estilo (sem testify).
  - OpenAPI da HF é referência única para evitar adivinhar campos.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/service/hfhub/... -count=1 -race` → PASS.
  - [ ] >= 8 casos de teste (listados em "What to do").
  - [ ] Coverage >= 85% para `client.go`.
  - [ ] Nenhum import além de stdlib + `internal/service/hfhub`.
  - [ ] User-Agent enviado em TODOS requests (assertível via httptest handler).

  **QA Scenarios**:

  ```
  Scenario: Suite HFHub passa todos os casos
    Tool: Bash
    Steps:
      1. go test ./internal/service/hfhub/... -count=1 -race -v -coverprofile=.sisyphus/evidence/task-07-cover.out | tee .sisyphus/evidence/task-07-test.txt
      2. go tool cover -func=.sisyphus/evidence/task-07-cover.out | tee .sisyphus/evidence/task-07-cover.txt
    Expected Result: PASS em todos; coverage >= 85% para client.go.
    Evidence: .sisyphus/evidence/task-07-test.txt, task-07-cover.txt

  Scenario: User-Agent presente em request real (integração com httptest)
    Tool: Bash
    Steps:
      1. Adicionar test inline que registra User-Agent visto no mock handler e asserta `== "test/0.1"`.
    Expected Result: PASS.
    Evidence: parte do task-07-test.txt

  Scenario: Repo ID com `/` não é percent-encoded
    Tool: Bash
    Steps:
      1. Test inline que captura `r.URL.Path` no mock e asserta `== "/api/models/meta-llama/Llama-3.1-8B-Instruct"`.
    Expected Result: PASS (slash literal preservado).
    Evidence: parte do task-07-test.txt
  ```

  **Commit**: YES
  - Message: `feat(hfhub): implement Search, RepoInfo, Download with TDD`
  - Files: `internal/service/hfhub/client.go`, `internal/service/hfhub/client_test.go`, `internal/service/hfhub/testdata/*.json`
  - Pre-commit: `go test ./internal/service/hfhub/... -race -count=1`

- [x] 8. **downloadmgr implementation: worker pool + queue + cancellation (TDD)**

  **What to do**:
  - Implementar `Manager` com:
    - Campos: `httpClient *http.Client`, `maxConcurrent int`, `active map[ID]*download`, `queue []*download`, `subscribers []chan Event`, `mu sync.Mutex`, `closeOnce sync.Once`, `closed chan struct{}`.
    - `type download struct { id ID; spec Spec; status Status; bytes, total int64; err error; ctx context.Context; cancel context.CancelFunc; startedAt time.Time }`
  - `Start(spec)`:
    - Validar spec (DestDir não vazio; DestFile não vazio; URL parseável).
    - Verificar conflito de existência: se `filepath.Join(DestDir, DestFile)` ou `DestDir` (se snapshot e dir existir) → retorna `ErrAlreadyExists` (sentinel exportado).
    - `MkdirAll(DestDir, 0o755)`.
    - Criar `download` com ctx derivado de manager root ctx + ID.
    - Se `len(active) < maxConcurrent` → marcar StatusActive + spawn goroutine. Senão StatusQueued + adicionar à queue.
    - Emitir `Event{Started}`.
  - Goroutine de download (`run(d)`):
    - GET via `httpClient.Do(req.WithContext(d.ctx))`.
    - Stream body para `{DestFile}.partial` em chunks de 256KB; após cada chunk, atualizar `bytes` + emitir `Event{Progress}` (rate-limit: max 4/s por download — evitar saturar tea event loop).
    - Em sucesso: `Rename(.partial, DestFile)`; status Completed; emite Event{Completed}. Tira da `active`, promove próximo da queue.
    - Em erro: status Failed, msg em `err`; remove `.partial`; emite Event{Failed}. Promove queue.
    - Em ctx cancel: status Cancelled; remove `.partial`; emite Event{Cancelled}. Promove queue.
  - `Cancel(id)`: localiza no `active` OU `queue`; cancela ctx (se ativo) OU remove de queue (se enfileirado); emite Event.
  - `Snapshot()`: lock + copia atual estado de todos downloads (ativos + enfileirados + recém-concluídos nas últimas 30s) como `[]State`.
  - `Subscribe()`: retorna canal NOVO (não compartilhado); buffer 32; manager drena/distribui internamente. `Close()` fecha canais.
  - `Close()`: cancela TODOS ctxs; aguarda goroutines via WaitGroup; fecha subscribers.
  - **Snapshot mode** (`spec.IsSnapshot=true`): nesse caso T15 passa N specs (uma por sibling) compondo MESMO `RepoID`. Mgr trata cada uma independente; sem lógica especial — UI agrupa visualmente. Mas conflito de subpasta cai em `ErrAlreadyExists` se a pasta `{org}__{repo}/` já existir (checado em Start).
  - Testes em `manager_test.go` com mock HTTP via httptest:
    - Start retorna ID válido + Event{Started}.
    - 1 download conclui → Event{Completed} + arquivo no disco.
    - Conflito (arquivo existe) → `ErrAlreadyExists` sem iniciar.
    - 4 starts simultâneos com maxConcurrent=2 → 2 active + 2 queued; após conclusão dos 2 → 2 próximos viram active.
    - Cancel em download ativo → `.partial` removido; Event{Cancelled}.
    - Cancel em download na fila → removido da queue; Event{Cancelled} sem ter executado.
    - HTTP 404 → Event{Failed} com erro.
    - Manager.Close cancela tudo e fecha subscribers.
    - Snapshot retorna estados consistentes durante downloads ativos.

  **Must NOT do**:
  - Resume de download (sem Range requests).
  - Persistência em disco.
  - Notificações cross-page diretamente — manager apenas emite eventos; bridge para tea.Msg fica em T15.
  - Logging via fmt — usar `Spec.Logger` se setado, senão `slog.Default()` ou silenciar.

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: Concorrência + goroutines + ctx + cleanup de arquivos parciais + testes race. Erros aqui são MUITO custosos.
  - **Skills**: `[]`

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2
  - **Blocks**: T13, T15
  - **Blocked By**: T2, T3

  **References**:
  - `internal/service/processmgr/` — padrão de Manager com Close/Reconcile (não copiar tudo, só estilo).
  - `internal/ui/pages/models.go:210:startScanCmd` — padrão de produzir Msg a partir de async goroutine.
  - Go stdlib: `context`, `sync`, `io`, `os`, `path/filepath`, `net/http`, `time`.
  - `internal/service/downloadmgr/pathing.go` (de T3) — usar `ResolveDest` para validar paths antes de Start.

  **WHY Each Reference Matters**:
  - `processmgr` mostra como o projeto fecha managers com workers ativos.
  - `startScanCmd` é exatamente o padrão que T15 usa para bridge Manager.Subscribe() → tea.Msg.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/service/downloadmgr/... -count=1 -race` → PASS.
  - [ ] >= 9 casos de teste (listados acima).
  - [ ] Coverage >= 80% para `manager.go`.
  - [ ] Zero race conditions (`-race` clean).
  - [ ] `ErrAlreadyExists` exportado e testado.
  - [ ] `Snapshot()` é safe-to-call durante downloads ativos (test concurrent).
  - [ ] `.partial` files limpos em Cancel e Failed.

  **QA Scenarios**:

  ```
  Scenario: Suite passa com -race
    Tool: Bash
    Steps:
      1. go test ./internal/service/downloadmgr/... -count=1 -race -v -coverprofile=.sisyphus/evidence/task-08-cover.out | tee .sisyphus/evidence/task-08-test.txt
      2. go tool cover -func=.sisyphus/evidence/task-08-cover.out | tee .sisyphus/evidence/task-08-cover.txt
    Expected Result: PASS, sem race; coverage >= 80% para manager.go.
    Evidence: .sisyphus/evidence/task-08-test.txt, task-08-cover.txt

  Scenario: 4 downloads com max 2 — apenas 2 ativos simultaneamente
    Tool: Bash
    Steps:
      1. Test específico que usa httptest com latência artificial; assert via Snapshot() que `len(active)==2 && len(queued)==2` durante execução.
    Expected Result: PASS.
    Evidence: parte do task-08-test.txt

  Scenario: Cancel limpa .partial
    Tool: Bash
    Steps:
      1. Test inicia download grande mockado; Cancel após 10ms; verifica em t.TempDir() que `.partial` foi removido.
    Expected Result: PASS, `.partial` não existe.
    Evidence: parte do task-08-test.txt

  Scenario: Conflito retorna ErrAlreadyExists
    Tool: Bash
    Steps:
      1. Test cria arquivo `model.gguf` em t.TempDir; chama Start com DestFile=model.gguf; espera errors.Is(err, ErrAlreadyExists).
    Expected Result: PASS.
    Evidence: parte do task-08-test.txt
  ```

  **Commit**: YES
  - Message: `feat(downloadmgr): implement worker pool, queue, cancellation`
  - Files: `internal/service/downloadmgr/manager.go`, `internal/service/downloadmgr/manager_test.go`
  - Pre-commit: `go test ./internal/service/downloadmgr/... -race -count=1`

- [x] 9. **HFSearchPicker implementation: debounce, epoch, GGUF toggle**

  **What to do**:
  - Implementar `Update()` + `View()` em `internal/ui/components/hf_search_picker.go`.
  - Modo input: rune chars vão para `p.input`; backspace edita; Enter sem input → noop; `esc` emite `HFSearchCancelledMsg`.
  - **Debounce**: cada keystroke incrementa `p.epoch` e retorna um `tea.Tick(300ms, ...)` que dispara `hfSearchDebounceMsg{Epoch:p.epoch}`. Page (T15) reage a esse Msg disparando o request HTTP — picker apenas SINALIZA via `HFSearchQueryMsg{Query, Epoch}`.
  - Quando picker recebe `HFResultsMsg{Epoch, Results, Err}` (mensagem definida na page T15 mas tipo conhecido pelo picker via interface mínima), valida `Epoch == p.epoch` — descarta stale; senão preenche `p.results`, `p.loading=false`, `p.err=...`.
  - Cursor: `up/down` ou `k/j` navega `p.results`. `enter` em cursor válido → `HFSearchSubmitMsg{RepoID: p.results[cursor].ID}`.
  - Toggle `g`: alterna `p.ggufOnly`; em re-render, filtra `p.results` por `HasGGUFTag()`.
  - Marcador `[GGUF]` no View: cada linha mostra ID, `[GGUF]` se aplicável, downloads/likes em dim.
  - `IsCapturingInput() bool { return true }` (sempre captura quando montado).
  - Tests com `teatest`:
    - Smoke render.
    - Typing produz `HFSearchQueryMsg` com epoch crescente.
    - Receber `HFResultsMsg` com epoch antigo → results NÃO atualizam.
    - Toggle `g` filtra resultados.
    - Enter com results vazios → noop.
    - Enter com cursor válido → emite `HFSearchSubmitMsg` com ID correto.
    - Esc → `HFSearchCancelledMsg`.

  **Must NOT do**:
  - Fazer request HTTP no picker — é responsabilidade da page.
  - Persistir queries.

  **Recommended Agent Profile**:
  - **Category**: `visual-engineering`
  - **Skills**: `[]`

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2
  - **Blocks**: T15
  - **Blocked By**: T4

  **References**:
  - `internal/ui/components/profile_picker.go` — handleKey, filter mode, View layout. Modelo direto.
  - `internal/ui/components/picker.go` — modo busca com scanner async (não exato, mas mostra streaming).
  - Bubbletea `tea.Tick` — debounce pattern.

  **WHY Each Reference Matters**:
  - `profile_picker.go` define exatamente como o projeto faz cursor + Enter + Esc + filter. Copiar fielmente.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/ui/components/... -count=1` → PASS.
  - [ ] >= 7 casos de teste cobrindo debounce, epoch, toggle, enter, esc.
  - [ ] View renderiza com NO_COLOR sem panic.
  - [ ] Stale results descartados (test explícito).

  **QA Scenarios**:

  ```
  Scenario: Teste teatest passa todos os casos
    Tool: Bash
    Steps:
      1. go test ./internal/ui/components/ -run HFSearchPicker -count=1 -v | tee .sisyphus/evidence/task-09-test.txt
    Expected Result: PASS em todos os subtests.
    Evidence: .sisyphus/evidence/task-09-test.txt

  Scenario: View visual via tmux (smoke isolado)
    Tool: interactive_bash (tmux skill)
    Steps:
      1. Criar harness em testutil/picker_demo.go que monta HFSearchPicker com 5 results mockados e roda como tea.Program.
      2. tmux new-session -d -s picker-demo "go run ./testutil/picker_demo"
      3. Esperar 1s. tmux capture-pane -t picker-demo -p > .sisyphus/evidence/task-09-view.txt
      4. tmux send-keys -t picker-demo "g" — toggle.
      5. tmux capture-pane -t picker-demo -p > .sisyphus/evidence/task-09-view-toggle.txt
      6. tmux kill-session -t picker-demo
    Expected Result: 5 results renderizam com marcadores corretos; após `g` lista filtra.
    Evidence: task-09-view.txt, task-09-view-toggle.txt
  ```

  **Commit**: YES
  - Message: `feat(components): HFSearchPicker filter, debounce, GGUF toggle`
  - Files: `internal/ui/components/hf_search_picker.go`, `internal/ui/components/hf_search_picker_test.go`
  - Pre-commit: `go test ./internal/ui/components/ -run HFSearchPicker -count=1`

- [x] 10. **HFFilePicker implementation: multi-select + snapshot mode**

  **What to do**:
  - Implementar `Update()` + `View()` em `internal/ui/components/hf_file_picker.go`.
  - **Modo determinado na construção** (`snapshotMode`):
    - `snapshotMode=false` (repo tem .gguf): listar apenas siblings cujo nome termina em `.gguf`; cursor; `space` alterna seleção; `a` seleciona todos; `n` deseleciona; `enter` emite `HFFilesSelectedMsg{RepoID, Filenames: ...}` com `IsSnapshot=false`; `esc` cancela.
    - `snapshotMode=true` (repo sem .gguf): View mostra "Snapshot download — N files, total Z GB". Sem cursor. `enter` emite `HFFilesSelectedMsg{RepoID, Filenames: TODOS_OS_SIBLINGS, IsSnapshot: true}`; `esc` cancela.
  - `IsCapturingInput() bool { return true }`.
  - Tests:
    - Construir com 3 .gguf siblings; `space` em [0], [2] + Enter → `HFFilesSelectedMsg.Filenames = [files[0].Name, files[2].Name]`.
    - `a` seleciona todos; Enter → todos no Msg.
    - `snapshotMode=true` com 20 arquivos; Enter → todos no Msg + `IsSnapshot=true`.
    - Esc → `HFFilePickerCancelledMsg`.
    - View NO_COLOR safe.

  **Must NOT do**:
  - Detectar `snapshotMode` automaticamente — quem decide é a page (T15) baseado em `RepoInfo.Siblings`.

  **Recommended Agent Profile**:
  - **Category**: `visual-engineering`
  - **Skills**: `[]`

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2
  - **Blocks**: T15
  - **Blocked By**: T5

  **References**:
  - `internal/ui/components/profile_picker.go` — base de cursor.
  - bubbles `key.NewBinding` — para mapear `space`/`a`/`n`.

  **WHY Each Reference Matters**:
  - Mesmo padrão visual de picker, mas com checkboxes (`[x]` / `[ ]`).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/ui/components/ -run HFFilePicker -count=1` → PASS.
  - [ ] >= 5 casos de teste.
  - [ ] Multi-select preserva ordem original dos siblings (não reordena).

  **QA Scenarios**:

  ```
  Scenario: Multi-select funciona
    Tool: Bash
    Steps:
      1. go test ./internal/ui/components/ -run HFFilePicker -count=1 -v | tee .sisyphus/evidence/task-10-test.txt
    Expected Result: PASS.
    Evidence: .sisyphus/evidence/task-10-test.txt

  Scenario: Snapshot mode envia todos
    Tool: Bash (subset do mesmo test)
    Steps:
      1. Test inline cria 20 siblings, modo snapshot, Enter → assert HFFilesSelectedMsg.Filenames len 20 + IsSnapshot true.
    Expected Result: PASS.
    Evidence: parte do task-10-test.txt
  ```

  **Commit**: YES
  - Message: `feat(components): HFFilePicker multi-select and snapshot mode`
  - Files: `internal/ui/components/hf_file_picker.go`, `internal/ui/components/hf_file_picker_test.go`
  - Pre-commit: `go test ./internal/ui/components/ -run HFFilePicker -count=1`

- [x] 11. **DownloadProgress rendering**

  **What to do**:
  - Implementar `View()` em `internal/ui/components/download_progress.go`.
  - Layout por linha:
    - `▸ {truncated-name 36}  {humanBytes(bytes)} / {humanBytes(total)}  {pct}%  {state}`
    - Cursor `▸` substituído por `›` na linha em foco (`focusVisible && focused == i`).
    - Estado:
      - `DSQueued` → `queued`
      - `DSActive` → `pct%`
      - `DSCompleted` → `done` (verde via `theme.OK`)
      - `DSFailed` → `failed` (vermelho via `theme.Error`)
      - `DSCancelled` → `cancelled` (cinza via `theme.Subtitle`)
  - View vazio se `len(lines) == 0` — retorna `""` (page decide se renderiza separador).
  - `FocusNext` aplica módulo; `FocusPrev` simétrico. `SetFocusVisible(bool)` toggle.
  - Tests:
    - `View()` com 0 linhas → `""`.
    - `View()` com 3 linhas (1 active, 1 queued, 1 completed) → contém os 3 names e os 3 estados corretos.
    - `FocusNext` cicla.
    - `humanBytes(1024*1024*1024)` → `"1.0G"`.

  **Must NOT do**:
  - Aceitar input — função pura de render.
  - Manter referência ao Manager — recebe lines via SetLines.

  **Recommended Agent Profile**:
  - **Category**: `visual-engineering`
  - **Skills**: `[]`

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2
  - **Blocks**: T15, T16
  - **Blocked By**: T6

  **References**:
  - `internal/ui/components/statusbar.go` — render minimalista.
  - `internal/ui/pages/models.go:424:humanSize` — funcionalidade a clonar.
  - `internal/ui/theme/theme.go` — estilos OK/Error/Subtitle.

  **WHY Each Reference Matters**:
  - `humanSize` é a função canônica do projeto para B/K/M/G — clonar (não compartilhar para não criar utility public).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/ui/components/ -run DownloadProgress -count=1` → PASS.
  - [ ] >= 4 casos de teste.
  - [ ] NO_COLOR awareness (theme já cuida — testar via NO_COLOR=1 env).
  - [ ] View vazio retorna `""`.

  **QA Scenarios**:

  ```
  Scenario: View renderiza 3 estados distintos
    Tool: Bash
    Steps:
      1. go test ./internal/ui/components/ -run DownloadProgress -count=1 -v | tee .sisyphus/evidence/task-11-test.txt
    Expected Result: PASS.
    Evidence: .sisyphus/evidence/task-11-test.txt
  ```

  **Commit**: YES
  - Message: `feat(components): DownloadProgress rendering`
  - Files: `internal/ui/components/download_progress.go`, `internal/ui/components/download_progress_test.go`
  - Pre-commit: `go test ./internal/ui/components/ -run DownloadProgress -count=1`

- [x] 12. **ModelsPage state additions + IsCapturingInput extension**

  **What to do**:
  - Adicionar campos NOVOS ao struct `ModelsPage` em `internal/ui/pages/models.go`:
    - `hfClient *hfhub.Client` (nil quando não-wired; T13 popula).
    - `dlManager *downloadmgr.Manager` (idem).
    - `hfSearch *components.HFSearchPicker`
    - `hfFilePicker *components.HFFilePicker`
    - `downloads components.DownloadProgress` (sempre presente; SetLines preenche)
    - `downloadEvents <-chan downloadmgr.Event` (channel do Manager.Subscribe; nil se manager nil)
    - `searchEpoch int` (matches debounce epoch da página — separado do scanID)
    - `pendingRepoID string` (entre RepoInfo e file picker)
  - Adicionar builder `func (p ModelsPage) WithHFClient(c *hfhub.Client) ModelsPage` e `WithDownloadManager(m *downloadmgr.Manager) ModelsPage` (espelham `WithProfileStore`).
  - Estender `IsCapturingInput()`:
    ```go
    return p.action != nil || p.filterMode || p.profilePicker != nil ||
           p.hfSearch != nil || p.hfFilePicker != nil || p.downloads.IsFocusVisible()
    ```
  - **Nada de comportamento novo ainda** — só estado e contracts.

  **Must NOT do**:
  - Implementar handlers — fica para T15.
  - Inicializar manager/client no construtor — fica para T13.
  - Alterar `NewModelsPage` signature.

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: Tocar arquivo grande (669 linhas) com mudança estrutural. Cuidado com não-regressão.
  - **Skills**: `[]`

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 — pode iniciar com stubs.
  - **Blocks**: T14, T15, T16
  - **Blocked By**: T4, T5, T6

  **References**:
  - `internal/ui/pages/models.go:57-80` — struct atual.
  - `internal/ui/pages/models.go:129-132` — padrão de builder `WithProfileStore`.
  - `internal/ui/pages/models.go:138-140` — `IsCapturingInput()` atual.

  **WHY Each Reference Matters**:
  - Builder pattern: replicar com mesmo formato (value receiver, retorna value).
  - IsCapturingInput: extender OR chain sem alterar lógica existente.

  **Acceptance Criteria**:
  - [ ] `go build ./internal/ui/pages/...` → exit 0.
  - [ ] `go test ./internal/ui/pages/... -count=1` → PASS (tests existentes não regridem).
  - [ ] Novo test `TestModelsPage_IsCapturingInput_ExtensionScenarios` cobrindo todos os 6 estados (4 antigos + 2 novos).
  - [ ] Diff vs main não toca outros arquivos.

  **QA Scenarios**:

  ```
  Scenario: Build + tests pages existentes não regridem
    Tool: Bash
    Steps:
      1. go test ./internal/ui/pages/... -count=1 -v | tee .sisyphus/evidence/task-12-test.txt
    Expected Result: TODOS os testes existentes passam; novo test passa.
    Evidence: .sisyphus/evidence/task-12-test.txt

  Scenario: IsCapturingInput retorna true para cada novo overlay
    Tool: Bash (test inline)
    Steps:
      1. Construir ModelsPage; setar hfSearch != nil → asserta true.
      2. Limpar; setar hfFilePicker != nil → asserta true.
      3. Limpar; downloads.SetFocusVisible(true) → asserta true.
    Expected Result: 3 asserts PASS.
    Evidence: parte do task-12-test.txt
  ```

  **Commit**: YES
  - Message: `feat(pages): extend Models page state and InputCapture for HF overlays`
  - Files: `internal/ui/pages/models.go`, `internal/ui/pages/models_test.go`
  - Pre-commit: `go test ./internal/ui/pages/... -count=1`

- [x] 13. **main.go wiring: HF client + download manager**

  **What to do**:
  - Em `cmd/model-loader/main.go`, dentro de `runTUI()` (após scanner ser criado em `:51`):
    - `hfClient := hfhub.NewClient(&http.Client{Timeout: 30 * time.Second}, "model-loader/"+Version)` — onde `Version` é constante já existente OU adicionar `const Version = "dev"` se ausente.
    - `dlManager := downloadmgr.NewManager(&http.Client{}, 3)` — note: client SEM timeout para downloads (controle via ctx).
    - `defer dlManager.Close()` — graceful shutdown.
  - Atualizar construção da ModelsPage:
    ```go
    modelsPage := pages.NewModelsPage(scanner, cfg.Models.SearchPaths).
        WithProfileStore(svc.store).
        WithHFClient(hfClient).
        WithDownloadManager(dlManager)
    ```
  - Adicionar imports: `net/http`, `time`, `internal/service/hfhub`, `internal/service/downloadmgr`.

  **Must NOT do**:
  - Alterar `bootstrap()`.
  - Mover wiring para `internal/service/`.
  - Adicionar nova lib em `go.mod`.

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Mudança de wiring 5-10 linhas em arquivo conhecido.
  - **Skills**: `[]`

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 3
  - **Blocks**: F1-F4
  - **Blocked By**: T7, T8

  **References**:
  - `cmd/model-loader/main.go:51-77` — onde o wiring atual acontece. Replicar idiom.
  - `cmd/model-loader/main.go:74-77` — defer pattern para Manager.Close.

  **WHY Each Reference Matters**:
  - Mostra o estilo de construção do projeto (builder chaining + defer Close).

  **Acceptance Criteria**:
  - [ ] `go build ./cmd/model-loader/...` → exit 0.
  - [ ] `go vet ./cmd/model-loader/...` → exit 0.
  - [ ] Smoke run `./bin/model-loader` inicia sem panic.
  - [ ] `defer dlManager.Close()` presente.

  **QA Scenarios**:

  ```
  Scenario: Binário compila e inicia
    Tool: Bash
    Steps:
      1. make build 2>&1 | tee .sisyphus/evidence/task-13-build.txt
      2. ./bin/model-loader & PID=$!; sleep 1; kill $PID
    Expected Result: build PASS; processo inicia (não panica nos 1s).
    Evidence: .sisyphus/evidence/task-13-build.txt

  Scenario: HF client e Manager visíveis no binário (sanity)
    Tool: Bash
    Steps:
      1. go vet ./cmd/model-loader/... | tee .sisyphus/evidence/task-13-vet.txt
      2. nm ./bin/model-loader 2>/dev/null | grep -E "hfhub|downloadmgr" | head -20 | tee .sisyphus/evidence/task-13-symbols.txt
    Expected Result: vet limpo; nm encontra símbolos dos novos pacotes.
    Evidence: task-13-vet.txt, task-13-symbols.txt
  ```

  **Commit**: YES
  - Message: `feat(cmd): wire HF client and download manager into Models page`
  - Files: `cmd/model-loader/main.go`
  - Pre-commit: `make build`

- [x] 14. **ModelsPage key bindings (s, x) + hints**

  **What to do**:
  - Estender `modelsKeyMap` em `internal/ui/pages/models.go:88-90`:
    ```go
    type modelsKeyMap struct {
        Filter, Rescan, Enter, Cancel key.Binding
        Search   key.Binding  // novo: `s`
        CancelDL key.Binding  // novo: `x` (cancel focused download)
    }
    ```
  - `defaultModelsKeys()`:
    ```go
    Search:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "search HF")),
    CancelDL: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "cancel download")),
    ```
  - Implementar `Hints() string` (interface `HintProvider`):
    ```go
    func (p ModelsPage) Hints() string {
        base := "[/] filter  [s] search HF  [R] rescan  [enter] actions"
        if p.downloads.HasLines() {
            base += "  [x] cancel dl"
        }
        return base
    }
    ```
  - Atualizar `HelpContext() string` (se existir) ou adicionar para descrever:
    - `/`: filter local
    - `s`: open Hugging Face search
    - `g` (in HF search): toggle GGUF-only filter
    - `space`/`a`/`n` (in HF file picker): multi-select / select-all / select-none
    - `R`: rescan local models
    - `enter`: open action menu OR confirm selection
    - `esc`: clear filter OR close overlay
    - `x`: cancel focused download
  - **NÃO alterar root.go** — só atalhos LOCAIS.

  **Must NOT do**:
  - Adicionar bindings em `internal/ui/root.go`.
  - Substituir bindings existentes.
  - Tornar `x` global (só funciona quando há downloads).

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Pequena adição de bindings + strings.
  - **Skills**: `[]`

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 3
  - **Blocks**: F1-F4
  - **Blocked By**: T12

  **References**:
  - `internal/ui/pages/models.go:88-99` — key map atual.
  - `internal/ui/pages/profiles.go` — exemplo de `Hints()` e `HelpContext()` existente.
  - `internal/ui/components/help.go` — formato de markdown aceito pelo modal.

  **WHY Each Reference Matters**:
  - `profiles.go` mostra como o projeto formata hints; ficar consistente.

  **Acceptance Criteria**:
  - [ ] `go build ./internal/ui/pages/...` → exit 0.
  - [ ] `Hints()` retorna string contendo `[s] search HF`.
  - [ ] Quando `downloads.HasLines()` true, `Hints()` inclui `[x] cancel dl`.
  - [ ] Help modal mostra novos atalhos via `?` no tmux smoke.

  **QA Scenarios**:

  ```
  Scenario: Hints atualizado
    Tool: Bash
    Steps:
      1. Test inline: page.Hints() contém "search HF" e "rescan" e "filter".
      2. Adicionar uma DownloadLine fake; assert "cancel dl" aparece.
    Expected Result: PASS.
    Evidence: .sisyphus/evidence/task-14-hints.txt

  Scenario: Help modal mostra atalhos novos
    Tool: interactive_bash (tmux skill)
    Steps:
      1. tmux new-session -d -s ml "./bin/model-loader"
      2. tmux send-keys -t ml "4" — vai p/ Models.
      3. tmux send-keys -t ml "?"
      4. sleep 0.3; tmux capture-pane -t ml -p > .sisyphus/evidence/task-14-help.txt
      5. tmux kill-session -t ml
    Expected Result: captura contém "s" e "search HF" no help.
    Evidence: .sisyphus/evidence/task-14-help.txt
  ```

  **Commit**: YES
  - Message: `feat(pages): add HF key bindings (s, x) and hints`
  - Files: `internal/ui/pages/models.go`
  - Pre-commit: `go build ./...`

- [x] 15. **ModelsPage handlers: search flow, download events, auto-rescan**

  **What to do**:
  - Adicionar mensagens custom em `internal/ui/pages/models.go`:
    - `hfSearchTriggerMsg struct { Query string; Epoch int }`
    - `hfSearchResultsMsg struct { Epoch int; Results []hfhub.SearchResult; Err error }`
    - `hfRepoInfoMsg struct { RepoID string; Info *hfhub.RepoInfo; Err error }`
    - `downloadEventMsg struct { Evt downloadmgr.Event; Ch <-chan downloadmgr.Event }` (re-arming pattern como scanEventMsg).
    - `downloadChannelClosedMsg struct{}`
  - Estender `Update()`:
    - **No handleKey** (quando nenhum overlay ativo): se `key.Matches(msg, p.keys.Search) && p.hfClient != nil` → cria `picker := components.NewHFSearchPicker()`, `p.hfSearch = &picker`, retorna `p, nil`. Se `key.Matches(msg, p.keys.CancelDL) && p.downloads.IsFocusVisible()` → identifica linha focada, chama `p.dlManager.Cancel(id)`. **Tecla `d`** alterna `p.downloads.SetFocusVisible(!p.downloads.IsFocusVisible())` quando há downloads ativos.
    - **Quando `p.hfSearch != nil`** e msg é KeyMsg: forward para picker; coletar Cmd. Após Update, check picker emitidos:
      - `HFSearchQueryMsg` capturado pela própria page → schedule `hfSearchTriggerMsg` via tea.Tick(0) e bumps epoch.
      - `HFSearchSubmitMsg` → set `p.pendingRepoID = msg.RepoID`, fechar picker (`p.hfSearch=nil`), dispatch Cmd buscando `RepoInfo`.
      - `HFSearchCancelledMsg` → `p.hfSearch=nil`.
    - **hfSearchTriggerMsg**: dispatch goroutine que chama `p.hfClient.Search(ctx, msg.Query, 30)`, retorna `hfSearchResultsMsg{msg.Epoch, results, err}`.
    - **hfSearchResultsMsg**: forward para picker (que valida epoch).
    - **hfRepoInfoMsg**: classifica GGUF vs snapshot (`anyGGUFInSiblings(info.Siblings)`), constrói `HFFilePicker` apropriado, `p.hfFilePicker = &picker`.
    - **Quando `p.hfFilePicker != nil`** e msg é KeyMsg: forward; check emitted:
      - `HFFilesSelectedMsg` → para cada filename, construir `downloadmgr.Spec`:
        - Resolver dest via `downloadmgr.ResolveDest(p.paths, msg.RepoID, fname, msg.IsSnapshot)`.
        - URL: `https://huggingface.co/{repoID}/resolve/main/{filename}`.
        - `dlManager.Start(spec)` — se sucesso: nada; se `ErrAlreadyExists`: flash "already exists: {fname}".
        - Após dispatch de todos, `p.hfFilePicker = nil`.
      - `HFFilePickerCancelledMsg` → `p.hfFilePicker = nil`.
    - **downloadEventMsg**: 
      - Atualizar `p.downloads.SetLines(p.dlManager.Snapshot())`.
      - Se `evt.State.Status == downloadmgr.StatusCompleted`: flash `"downloaded: {name}"` + retornar `tea.Batch(flashCmd, func() tea.Msg { return modelsReloadMsg{} })` para auto-rescan.
      - Se `Failed`: flash `"download failed: {name}: {err}"`.
      - Se `Cancelled`: flash `"cancelled: {name}"`.
      - **Re-arm**: retornar Cmd `waitForDownloadEvent(msg.Ch)`.
    - **downloadChannelClosedMsg**: noop.
  - **Init() override**: se `p.dlManager != nil`, na primeira chamada subscribe e arm `waitForDownloadEvent`. Adicionar Cmd ao return.
  - **Test models_test.go** (tests-after após T12):
    - Tecla `s` abre HFSearchPicker quando hfClient != nil; noop quando nil.
    - `HFSearchSubmitMsg` dispara fetch de RepoInfo + Picker fecha.
    - `hfRepoInfoMsg` GGUF → constrói FilePicker com `snapshotMode=false`.
    - `hfRepoInfoMsg` sem GGUF → constrói FilePicker com `snapshotMode=true`.
    - `HFFilesSelectedMsg` chama `Start(spec)` para cada arquivo (testar com fake `Manager`).
    - `downloadEventMsg{Completed}` → emite `modelsReloadMsg` via Cmd retornado.

  **Must NOT do**:
  - Alterar handlers existentes do scanner.
  - Implementar Update em outras pages.
  - Fazer chamadas HTTP síncronas no Update — sempre via tea.Cmd.

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: Many interaction points + concurrent message handling + new test fixtures. Critical wiring.
  - **Skills**: `[]`

  **Parallelization**:
  - **Can Run In Parallel**: NO (eixo central da Wave 3)
  - **Parallel Group**: Wave 3 (paralelo com T13, T14 que tocam outros arquivos; T16 vem depois)
  - **Blocks**: T16, F1-F4
  - **Blocked By**: T7, T8, T9, T10, T11, T12

  **References**:
  - `internal/ui/pages/models.go:232-286` — Update() atual, padrão a estender (não substituir).
  - `internal/ui/pages/models.go:210-230` — `startScanCmd` + `waitForScanEvent` — padrão a clonar para downloads.
  - `internal/ui/pages/models.go:178-180` — Reload pattern (emit modelsReloadMsg).
  - `internal/service/hfhub/client.go` (de T7).
  - `internal/service/downloadmgr/manager.go` (de T8).

  **WHY Each Reference Matters**:
  - `startScanCmd`/`waitForScanEvent` é o template literal para o bridge `Manager.Subscribe() → tea.Msg`. Não inventar pattern novo.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/ui/pages/... -count=1 -race` → PASS.
  - [ ] >= 6 novos test cases.
  - [ ] Tests existentes do scanner não regridem.
  - [ ] Auto-rescan: ao receber `Completed`, Cmd retornado contém `modelsReloadMsg` (testável via teatest).
  - [ ] `s` no estado base abre picker.
  - [ ] Conflito `ErrAlreadyExists` produz flash sem panic.

  **QA Scenarios**:

  ```
  Scenario: Teste full pages passa com race
    Tool: Bash
    Steps:
      1. go test ./internal/ui/pages/... -count=1 -race -v | tee .sisyphus/evidence/task-15-test.txt
    Expected Result: PASS.
    Evidence: .sisyphus/evidence/task-15-test.txt

  Scenario: Auto-rescan dispara após Completed
    Tool: Bash (test inline)
    Steps:
      1. Test: feed downloadEventMsg{Completed} → assert returned Cmd executes to modelsReloadMsg.
    Expected Result: PASS.
    Evidence: parte do task-15-test.txt

  Scenario: Conflito não panica
    Tool: Bash
    Steps:
      1. Test usa fake Manager que retorna ErrAlreadyExists em Start; HFFilesSelectedMsg dispara; assert page tem flash setada.
    Expected Result: PASS.
    Evidence: parte do task-15-test.txt

  Scenario: End-to-end via tmux (com hfmock)
    Tool: interactive_bash (tmux skill)
    Steps:
      1. go run ./testutil/hfmock & MOCK_PID=$!; sleep 0.5
      2. tmux new-session -d -s ml-e2e "HF_BASE_URL=http://localhost:8765 ./bin/model-loader"
      3. tmux send-keys -t ml-e2e "4" Enter — Models tab.
      4. tmux send-keys -t ml-e2e "s" — abre HF search.
      5. tmux send-keys -t ml-e2e "llama" — digita.
      6. sleep 0.5; tmux capture-pane -t ml-e2e -p > .sisyphus/evidence/task-15-e2e-search.txt
      7. tmux send-keys -t ml-e2e Enter — abre file picker.
      8. sleep 0.3; tmux send-keys -t ml-e2e " " Enter — select + download.
      9. sleep 2; tmux capture-pane -t ml-e2e -p > .sisyphus/evidence/task-15-e2e-progress.txt
      10. sleep 3; tmux capture-pane -t ml-e2e -p > .sisyphus/evidence/task-15-e2e-rescan.txt
      11. tmux kill-session -t ml-e2e; kill $MOCK_PID
    Expected Result: 3 capturas mostram (1) search results, (2) progress line, (3) arquivo na tabela local após auto-rescan.
    Evidence: 3 arquivos task-15-e2e-*.txt
  ```

  **Commit**: YES
  - Message: `feat(pages): wire Models page handlers for HF search and downloads`
  - Files: `internal/ui/pages/models.go`, `internal/ui/pages/models_test.go`, `testutil/hfmock/main.go` (novo)
  - Pre-commit: `go test ./internal/ui/pages/... -race -count=1`

- [x] 16. **ModelsPage View extension: overlays + progress footer**

  **What to do**:
  - Em `internal/ui/pages/models.go`, função `View()` (linha 541):
    - Prioridade de overlay (early return):
      ```go
      if p.hfFilePicker != nil { return p.hfFilePicker.View() }
      if p.hfSearch != nil { return p.hfSearch.View() }
      if p.profilePicker != nil { return p.profilePicker.View() }
      if p.action != nil { return p.renderActionMenu() }
      ```
    - **Compor View principal** (quando sem overlay):
      1. `header` (Title "Models")
      2. `statusLine` (já existe)
      3. `table.View()` OU mensagem vazia (já existe)
      4. `filterLine` (já existe)
      5. **NOVO**: `downloadFooter := p.downloads.SetLines(p.dlManager.Snapshot()).View()` — apenas se non-empty.
      6. `flash.View()` (já existe)
    - JoinVertical com todos.
  - Garantir que `downloads.SetLines` não muta page (retorna nova value); usar variável local.
  - **Test View** com teatest:
    - View sem downloads não muda layout existente.
    - View com 2 downloads ativos mostra 2 linhas no rodapé.
    - View com hfSearch ativo retorna apenas HFSearchPicker View.

  **Must NOT do**:
  - Alterar a ordem dos elementos existentes.
  - Re-renderizar a tabela quando overlay ativo.
  - Importar `internal/service/*` se não importado já — usar tipos públicos.

  **Recommended Agent Profile**:
  - **Category**: `visual-engineering`
    - Reason: View composition + visual regression risk.
  - **Skills**: `[]`

  **Parallelization**:
  - **Can Run In Parallel**: NO (depende do T15 ter wireado handlers).
  - **Parallel Group**: Wave 3 (after T15)
  - **Blocks**: F1-F4
  - **Blocked By**: T12, T15

  **References**:
  - `internal/ui/pages/models.go:541-560` — View() atual.
  - `internal/ui/components/download_progress.go` (de T6/T11) — para chamada de SetLines + View.

  **WHY Each Reference Matters**:
  - View atual usa `lipgloss.JoinVertical(lipgloss.Left, ...)` — manter idiom.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/ui/pages/... -count=1` → PASS (sem regressão).
  - [ ] `View()` sem downloads é byte-idêntico ao current (golden snapshot test).
  - [ ] `View()` com 2 downloads ativos contém 2 linhas adicionais.
  - [ ] `View()` com hfSearch ativo retorna apenas HFSearchPicker.View().

  **QA Scenarios**:

  ```
  Scenario: Golden snapshot do View sem downloads
    Tool: Bash
    Steps:
      1. go test ./internal/ui/pages/ -run TestModelsPage_View_Baseline -count=1 -v | tee .sisyphus/evidence/task-16-baseline.txt
    Expected Result: PASS (golden file `testdata/models_view_baseline.golden` existe e matcha).
    Evidence: .sisyphus/evidence/task-16-baseline.txt

  Scenario: View com downloads renderiza rodapé
    Tool: Bash
    Steps:
      1. go test ./internal/ui/pages/ -run TestModelsPage_View_WithDownloads -count=1 -v | tee .sisyphus/evidence/task-16-progress.txt
    Expected Result: PASS; output mostra 2 linhas com nomes.
    Evidence: .sisyphus/evidence/task-16-progress.txt

  Scenario: Overlay esconde tabela
    Tool: Bash
    Steps:
      1. Test inline: page.hfSearch = &picker; View() não contém "Models" header da tabela.
    Expected Result: PASS.
    Evidence: parte do task-16-progress.txt
  ```

  **Commit**: YES
  - Message: `feat(pages): extend Models View with overlays and progress footer`
  - Files: `internal/ui/pages/models.go`, `internal/ui/pages/models_test.go`, `internal/ui/pages/testdata/models_view_baseline.golden`
  - Pre-commit: `go test ./internal/ui/pages/... -count=1`

---

## Final Verification Wave (MANDATORY — após TODAS as tasks de implementação)

> 4 agentes de revisão rodam em PARALELO. Todos devem APROVAR.
> Apresentar resultado consolidado ao usuário e obter "okay" explícito antes de marcar trabalho como concluído.
>
> **Não auto-prosseguir após a verificação. Aguardar aprovação explícita do usuário.**
> **Nunca marcar F1-F4 como checked antes do okay.** Rejeição → fix → re-run → apresentar de novo → aguardar okay.

- [x] F1. **Plan Compliance Audit** — `oracle`
  Ler o plano fim-a-fim. Para cada "Must Have": verificar implementação (ler arquivo, rodar comando, acionar TUI via tmux). Para cada "Must NOT Have": grepar codebase por padrão proibido — rejeitar com `file:line` se encontrado. Validar `.sisyphus/evidence/` existe e tem 1 arquivo por cenário QA. Comparar deliverables vs plano.
  Output: `Must Have [N/N] | Must NOT Have [N/N] | Tasks [N/N] | VERDICT: APPROVE/REJECT`

- [x] F2. **Code Quality Review** — `unspecified-high`
  Rodar `go build ./...`, `go vet ./...`, `golangci-lint run` se configurado, `make tests`. Revisar arquivos alterados: `interface{}` ou `any` injustificado, panics em código de UI, goroutines sem `ctx.Done()` selectionável, channels sem fechamento, time.Sleep em código de produção, prints/logs no caminho quente, imports não usados, código comentado, nomes genéricos (`data`, `result`, `item`, `tmp`). Validar exclusões do "Must NOT Have".
  Output: `Build [PASS/FAIL] | Vet [PASS/FAIL] | Tests [N pass/N fail] | Files [N clean/N issues] | VERDICT`

- [x] F3. **Real Manual QA via tmux + hfmock** — `unspecified-high` (+ skill `tmux`)
  Iniciar `testutil/hfmock` (servidor HTTP mock servindo respostas estáticas do HF). Setar `HF_BASE_URL=http://localhost:N` no ambiente. Rodar `./bin/model-loader` em tmux. Executar **cada** cenário QA de **cada** task de implementação. Capturar `tmux capture-pane -p` em cada estado. Testar integração inter-task: filtro local funciona enquanto downloads rodam, mudança de tab durante download, quit cancela downloads, auto-rescan adiciona arquivo à tabela local após conclusão. Edge cases: busca vazia, repo sem siblings, erro 404, erro 429, cancelamento na metade. Evidências em `.sisyphus/evidence/final-qa/`.
  Output: `Scenarios [N/N pass] | Integration [N/N] | Edge Cases [N tested] | VERDICT`

- [x] F4. **Scope Fidelity Check** — `deep`
  Para cada task: ler "What to do", ler diff real (`git diff main -- {paths}`). Verificar 1:1 — tudo na spec foi feito (nada faltando), nada além da spec foi feito (sem creep). Validar compliance com "Must NOT do" de cada task. Detectar contaminação cross-task: Task N tocando arquivos da Task M. Conferir que `modelscanner/`, `config/`, `domain/`, outras pages permaneceram intactas. Reportar arquivos alterados que não correspondem a nenhuma task.
  Output: `Tasks [N/N compliant] | Contamination [CLEAN/N issues] | Unaccounted [CLEAN/N files] | VERDICT`

---

## Commit Strategy

- **T1**: `feat(hfhub): add package skeleton with types and contracts` — `internal/service/hfhub/{client.go,types.go}`, `go build ./...`
- **T2**: `feat(downloadmgr): add package skeleton with types and Manager interface` — `internal/service/downloadmgr/{manager.go,types.go}`, `go build ./...`
- **T3**: `feat(downloadmgr): add download path resolver with traversal guard` — `internal/service/downloadmgr/pathing.go` + test, `go test ./internal/service/downloadmgr/...`
- **T4**: `feat(components): add HFSearchPicker skeleton` — `internal/ui/components/hf_search_picker.go`, `go build ./...`
- **T5**: `feat(components): add HFFilePicker skeleton` — `internal/ui/components/hf_file_picker.go`, `go build ./...`
- **T6**: `feat(components): add DownloadProgress skeleton` — `internal/ui/components/download_progress.go`, `go build ./...`
- **T7**: `feat(hfhub): implement Search, RepoInfo, Download with TDD` — `internal/service/hfhub/*.go`, `go test ./internal/service/hfhub/...`
- **T8**: `feat(downloadmgr): implement worker pool, queue, cancellation` — `internal/service/downloadmgr/*.go`, `go test ./internal/service/downloadmgr/...`
- **T9**: `feat(components): HFSearchPicker filter, debounce, GGUF toggle` — `internal/ui/components/hf_search_picker*.go`, `go test ./internal/ui/components/...`
- **T10**: `feat(components): HFFilePicker multi-select and snapshot mode` — `internal/ui/components/hf_file_picker*.go`, `go test ./internal/ui/components/...`
- **T11**: `feat(components): DownloadProgress rendering` — `internal/ui/components/download_progress*.go`, `go test ./internal/ui/components/...`
- **T12**: `feat(pages): extend Models page state and InputCapture for HF overlays` — `internal/ui/pages/models.go`, `go build ./...`
- **T13**: `feat(cmd): wire HF client and download manager into Models page` — `cmd/model-loader/main.go`, `go build ./...`
- **T14**: `feat(pages): add HF key bindings (s, g, x) and hints` — `internal/ui/pages/models.go`, `go build ./...`
- **T15**: `feat(pages): wire Models page handlers for HF search and downloads` — `internal/ui/pages/models.go` + `internal/ui/pages/models_test.go`, `go test ./internal/ui/pages/...`
- **T16**: `feat(pages): extend Models View with overlays and progress footer` — `internal/ui/pages/models.go` + test, `go test ./internal/ui/pages/...`

Pre-commit em cada task: `go vet ./{path} && go test ./{path}/... -count=1` (quando há teste no path).

---

## Success Criteria

### Verification Commands

```bash
# Build + tipos
go build ./...                                                    # Expected: exit 0, sem warnings
go vet ./...                                                      # Expected: exit 0

# Testes — cada pacote novo passa, suíte total verde
go test ./internal/service/hfhub/... -count=1 -race -timeout 30s  # Expected: ok
go test ./internal/service/downloadmgr/... -count=1 -race -timeout 30s  # Expected: ok
go test ./internal/ui/components/... -count=1                     # Expected: ok
go test ./internal/ui/pages/... -count=1                          # Expected: ok
go test ./internal/ui/... -count=1                                # Expected: ok
make tests                                                        # Expected: full PASS

# Comportamento — TUI rodando
./bin/model-loader
# tab 4 → tabela local renderiza
# `s` → busca HF abre
# digitar → 300ms → resultados
# enter → file picker
# space + enter → downloads iniciam
# rodapé mostra N linhas de progresso
# `R` (na main) ainda rescaneia local
# `/` ainda filtra local
# `?` → help mostra novos atalhos
# ao concluir → flash + arquivo na tabela
```

### Final Checklist
- [ ] Build limpo + `go vet` limpo + `go test ./... -race` verde.
- [ ] Todos os "Must Have" do plano presentes na implementação.
- [ ] Todos os "Must NOT Have" ausentes (grep confirma).
- [ ] `.sisyphus/evidence/` tem >= 1 arquivo por cenário QA documentado nas tasks.
- [ ] `modelscanner/`, `config/`, `domain/`, outras pages — diff vazio em relação a `main`.
- [ ] `go.mod` inalterado (zero novas deps).
- [ ] `IsCapturingInput()` cobre 100% dos overlays editáveis (teste dedicado em `models_test.go`).
- [ ] Auto-rescan reproduzível: download conclui → tabela local mostra o arquivo dentro de 2s sem ação do usuário.
- [ ] 3 downloads simultâneos confirmados via QA (tmux capture-pane mostra 3 linhas de progresso).
- [ ] 4º download fica em fila confirmado via QA.
- [ ] Cancelamento limpa `.partial` confirmado via QA + `ls` no destino.
- [ ] User okay explícito recebido após F1-F4.

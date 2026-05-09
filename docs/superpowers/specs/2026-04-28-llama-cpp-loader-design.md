# llama.cpp-loader — Design Spec

- **Data:** 2026-04-28
- **Status:** Aprovado para planejamento de implementação
- **Stack:** Go + Bubbletea (TUI)
- **Plataforma alvo:** Linux (desenvolvimento principal em Arch Linux + RTX 3090)

## 1. Objetivo

Aplicação TUI que centraliza a criação, edição, lançamento e monitoramento de profiles de carga (boot-time params) do `llama-server` (binário do projeto `ggml-org/llama.cpp`). Substitui o uso manual de flags CLI longas e a edição direta de arquivos JSON `--config` por uma interface guiada com validação contra o schema real do binário instalado.

### Casos de uso primários

1. Criar e versionar profiles JSON-compatíveis com `llama-server --config`.
2. Lançar múltiplas instâncias simultâneas do `llama-server` em portas distintas.
3. Acompanhar runtime de cada instância: logs, slots, métricas, GPU.
4. Descobrir GGUFs em paths locais (LM Studio e diretórios adicionais) e atribuí-los a profiles.

## 2. Escopo

### Inclui (MVP)

- Editor de profiles com abas `Essentials` e `Advanced`.
- Launcher capaz de subir `llama-server` em foreground ou background, escolha por launch.
- Monitor com logs, status `/health`, snapshot `/slots`, GPU via `nvidia-smi`, métricas `tokens/s` e `req/s`.
- Model browser com múltiplos paths configuráveis e scan recursivo `*.gguf`.
- Validação version-aware via parse de `llama-server --help`.
- Multi-instance: N instâncias concorrentes, cada uma com PID/porta próprios.
- Recuperação de instâncias background ao reabrir a TUI.

### Fora de escopo (MVP)

- Integração com HuggingFace (search/download).
- `systemd` user units.
- Auto-detect e multi-binary (`llama-server-vulkan`, builds custom).
- Auto-suggest de quant baseado em VRAM.
- Vim keybindings e command mode `:`.
- Internacionalização (UI somente en-US).
- Hot-reload de params em runtime.

## 3. Decisões de produto

| # | Decisão | Escolha |
|---|---------|---------|
| 1 | Escopo MVP | Editor + Launcher + Monitor + Model Browser |
| 2 | Storage | Um arquivo JSON por profile em `~/.config/model-loader/profiles/` |
| 3 | Editor de params | Tabs `Essentials` (curado) + `Advanced` (schema completo) |
| 4 | Modo do launcher | Foreground OU background, escolha por launch (default background) |
| 5 | Profundidade do monitor | Completo (status + GPU + `/slots` + métricas) |
| 6 | Model browser | Multi-paths configuráveis em config, scan recursivo |
| 7 | Validação | Version-aware via `llama-server --help`, fallback embutido |
| 8 | Binário | `llama-server` no `PATH` (sem multi-binary, sem custom path) |
| 9 | Concorrência | Multi-instance |
| 10 | Allocation de port | Manual no profile (sem auto-pick) |
| 11 | Layout TUI | Tabs no topo + master-detail por seção |
| 12 | Idioma da UI | en-US |
| 13 | Stack de libs | bubbletea + bubbles + lipgloss + huh + glamour + viper + gopsutil |

## 4. Arquitetura

### 4.1 Camadas

```
cmd/model-loader/main.go           entry, viper config, DI
        │
internal/ui/   (Bubbletea)             rendering + dispatch
        │
internal/service/   (pure Go)          domain logic
        │
internal/domain/   (types only)        zero-deps shared types
```

### 4.2 Princípios

- A camada `ui` não chama `os/exec`, `filepath.Walk` ou abre arquivos diretamente. Sempre via `service`.
- A camada `service` não importa `bubbletea`. Retorna dados, erros ou canais; a UI envolve em `tea.Cmd`/`tea.Msg`.
- `domain` contém apenas tipos. Sem lógica nem dependências externas.
- Cada service tem interface explícita; implementações concretas são injetadas no boot. Facilita teste com fakes.

### 4.3 Recomendação adotada (alternativas avaliadas)

Aprovada: **Composição hierárquica + serviços puros**. Foi descartada a abordagem de modelo único Bubbletea (mistura UI e domínio) e a de composição sem camada de serviços (mantém domínio dentro dos models). A separação justifica-se pelo escopo (4 áreas funcionais), pela complexidade do parser/validator e pelo monitor com stream contínuo.

## 5. Modelo de dados

### 5.1 Profile JSON

Localização: `~/.config/model-loader/profiles/<id>.json` (id = slug ASCII, kebab-case).

```json
{
  "schemaVersion": 1,
  "id": "qwen-coder-32b",
  "name": "Qwen Coder 32B",
  "description": "Coding assistant, q5km, ctx 16k",
  "tags": ["coding", "32b"],
  "model": "/home/diogo/.lmstudio/models/bartowski/Qwen2.5-Coder-32B-Instruct-GGUF/Qwen2.5-Coder-32B-Instruct-Q5_K_M.gguf",
  "args": {
    "ngl": 99,
    "ctx-size": 16384,
    "batch-size": 2048,
    "ubatch-size": 512,
    "flash-attn": true,
    "cache-type-k": "q8_0",
    "cache-type-v": "q8_0",
    "parallel": 2,
    "port": 8080
  },
  "extraArgs": [],
  "launch": {
    "defaultBackground": true,
    "logFilePath": null
  },
  "meta": {
    "createdAt": "2026-04-28T15:30:00Z",
    "updatedAt": "2026-04-28T15:30:00Z",
    "lastUsedAt": null
  }
}
```

- `args` usa nomes longos sem prefixo `-`/`--`. O launcher converte para flags CLI.
- `extraArgs` é escape-hatch de strings literais (ex: `["--no-warmup"]`).
- `model` é separado de `args` para destacá-lo no editor e habilitar o picker.
- `schemaVersion` permite migrações futuras.

### 5.2 App Config (`~/.config/model-loader/config.toml`)

```toml
[paths]
profiles_dir = "~/.config/model-loader/profiles"
log_dir      = "~/.local/state/model-loader/logs"
state_dir    = "~/.local/state/model-loader"

[models]
search_paths = [
  "~/.lmstudio/models",
  "~/models"
]

[ui]
default_tab = "profiles"
keybindings = "default"
```

Criado com defaults na primeira execução se ausente.

### 5.3 Runtime state (`~/.local/state/model-loader/instances.json`)

Registry de instâncias background vivas; permite recuperação ao reabrir a TUI.

```json
{
  "instances": [
    {
      "profileId": "qwen-coder-32b",
      "pid": 4521,
      "port": 8080,
      "logPath": "~/.local/state/model-loader/logs/qwen-coder-32b-4521.log",
      "startedAt": "2026-04-28T11:00:00Z",
      "background": true
    }
  ]
}
```

### 5.4 Tipos de domínio adicionais

```go
type RunningInstance struct {
    ProfileID  string
    PID        int
    Port       int
    LogPath    string
    StartedAt  time.Time
    Background bool
}

type LogLine struct {
    Timestamp time.Time
    Level     string  // INFO | WARN | ERROR | "" se não parseável
    Text      string
}
```

`meta.lastUsedAt` no Profile é atualizado pelo `ProcessManager.Launch` no momento do spawn bem-sucedido (após o `WaitHealthy` retornar 200), via callback no `ProfileStore.Save`.

### 5.5 FlagSchema (em-memória)

Derivado de `llama-server --help` na boot, cacheado por sessão.

```go
type FlagSchema struct {
    Version string                  // versão detectada
    Flags   map[string]FlagSpec     // chave = nome longo
}

type FlagSpec struct {
    Long       string
    Short      string
    Type       FlagType  // bool | int | float | string | enum
    EnumValues []string
    Default    any
    HelpText   string
    Group      string    // "essential" se curado, senão "advanced"
}
```

Lista de "essential" curada e hardcoded: `model`, `ngl`, `ctx-size`, `batch-size`, `ubatch-size`, `flash-attn`, `threads`, `parallel`, `mlock`, `cache-type-k`, `cache-type-v`, `split-mode`, `tensor-split`.

## 6. Componentes (services)

### 6.1 ProfileStore

```go
type ProfileStore interface {
    List() ([]domain.Profile, error)
    Get(id string) (domain.Profile, error)
    Save(p domain.Profile) error
    Delete(id string) error
    Duplicate(id, newID string) (domain.Profile, error)
}
```

- Implementação `fsProfileStore` lê/escreve `<dir>/<id>.json`.
- Escrita atômica: `os.WriteFile` em `.tmp` + `os.Rename`.
- Erros nomeados: `ErrNotFound`, `ErrInvalidJSON`, `ErrDuplicateID`.

### 6.2 LlamaHelpParser

```go
type LlamaHelpParser interface {
    Parse(ctx context.Context) (domain.FlagSchema, error)
    DetectVersion(ctx context.Context) (string, error)
}
```

- Executa `llama-server --help` e `llama-server --version`.
- Cache em memória durante a sessão; refresh manual via tecla.
- Heurística: detecta blocos `-x, --xxx <type>  description (default: ...)`. Tipos inferidos do help text. Enum hardcoded para `cache-type-{k,v}`, `split-mode`.
- Fallback: schema embutido bundled at compile time (alvo: última versão do `llama.cpp` testada na release; documentada em `internal/service/llamahelp/embedded.go`). UI exibe warning na statusbar incluindo a versão do fallback.

### 6.3 Validator

```go
type Validator interface {
    Validate(p domain.Profile, schema domain.FlagSchema) ValidationReport
}

type ValidationReport struct {
    Errors   []FieldIssue
    Warnings []FieldIssue
}

type FieldIssue struct {
    Field   string
    Message string
}
```

Regras em camadas:

1. **Tipo/range** derivado do `FlagSchema` (e do schema embutido para campos não cobertos pelo `--help`).
2. **Cross-field hardcoded:**
   - `ubatch-size ≤ batch-size` (erro).
   - `flash-attn=true` com `cache-type-{k,v}=f16`: warning sugerindo `q8_0`.
   - `ctx-size > 32768` com `ngl < 99`: warning de risco de offload CPU.
3. **Existência:**
   - `model` path existe (erro).
   - `port` livre (verificado pre-launch, não em edição).

### 6.4 ProcessManager

```go
type ProcessManager interface {
    Launch(p domain.Profile, mode LaunchMode) (domain.RunningInstance, error)
    Kill(pid int) error
    List() []domain.RunningInstance
    TailLogs(pid int) (<-chan LogLine, error)
    WaitHealthy(pid int, port int, timeout time.Duration) error
}

type LaunchMode int  // Foreground | Background
```

- **Background:** `cmd.Start()` com `SysProcAttr.Setsid = true` (Linux), redireciona stdout/stderr para arquivo em `log_dir`. Persiste entry em `instances.json`. N instâncias background simultâneas suportadas.
- **Foreground:** stdout/stderr via canal direto para o painel monitor. Apenas **uma** instância foreground por vez (limitação do canal compartilhado com o terminal); tentativa de lançar segunda foreground gera erro com sugestão de usar background. Instâncias background pré-existentes não são afetadas.
- **Health:** `WaitHealthy` faz `GET http://localhost:<port>/health` com retry exponencial até timeout (default 30s).
- **Boot recovery:** ao iniciar TUI, `ProcessManager` lê `instances.json`, valida cada PID via `gopsutil` (processo existe e nome contém `llama-server`), descarta zumbis, atualiza arquivo.

### 6.5 ModelScanner

```go
type ModelScanner interface {
    Scan(ctx context.Context, paths []string) (<-chan ScanEvent, error)
}

type ScanEvent struct {
    Type  ScanEventType  // Progress | File | Done | Error
    File  *domain.ModelFile
    Error error
}

type ModelFile struct {
    Path      string
    SizeBytes int64
    Name      string
    Quant     string  // heurística por filename ("Q4_K_M", "Q5_K_M")
    Params    string  // metadata GGUF se legível ("32B")
}
```

- Walk recursivo respeitando `context.Context` para cancel.
- Parse mínimo de header GGUF (magic `GGUF` + version + first metadata kv block) para extrair `general.parameter_count` quando presente.
- Se header inválido ou incompleto: emite `ModelFile` apenas com path/size/nome.

### 6.6 Monitor

```go
type Monitor interface {
    Subscribe(pid int, port int) (<-chan MonitorEvent, func() error)
}

type MonitorEvent struct {
    Timestamp time.Time
    Source    EventSource  // Logs | Slots | GPU | Health
    Data      any
}
```

- Goroutine interna por subscription:
  - Tick 1s: `GET /slots` + `GET /health`.
  - Tick 2s: `nvidia-smi --query-gpu=memory.used,memory.total,utilization.gpu --format=csv,noheader,nounits` (skip se ausente).
  - Tail contínuo do log file (inotify via `fsnotify`).
- Métricas `tokens/s` e `req/s` agregadas em janela deslizante de 60s a partir de logs e diffs entre snapshots `/slots`.
- `cancel()` fecha goroutine e canal.

## 7. UI

### 7.1 rootModel

```go
type rootModel struct {
    tabs     []tab
    active   int
    profiles profilesPage
    launcher launcherPage
    monitor  monitorPage
    models   modelsPage
    services serviceBundle
    schema   domain.FlagSchema
    status   statusBar
}
```

`Update` faz tab routing global; `tea.KeyMsg` não capturada vai para o page ativo.

### 7.2 Páginas

**`profilesPage`** — master-detail
- Esquerda: `bubbles/list` com profiles.
- Direita: `huh.Form` com sub-tabs `Essentials` / `Advanced`.
  - Essentials: 13 campos curados (`huh.Input`, `huh.Select`, `huh.Confirm`).
  - Advanced: tabela scrollável de todos os flags do `FlagSchema`, filtro `/`.
- Bottom: warnings/errors do Validator inline.
- Keybindings: `n` new, `d` duplicate, `x` delete, `s` save, `L` launch, `/` filter.

**`launcherPage`** — escolher profile + mode
- Lista profiles (mesma fonte da `profilesPage`).
- Toggle `Background?` (default true).
- Botão `Validate before launch` mostra report; bloqueia se houver errors.
- `Enter`/`L` chama `ProcessManager.Launch`.
- Após launch bem-sucedido: navega para `monitorPage` filtrado pela nova instância.

**`monitorPage`** — multi-instance
- Top: `bubbles/table` com instâncias (`name | pid | port | uptime | VRAM | tokens/s`).
- Bottom: três sub-views cycláveis via `Tab`:
  1. Logs tail (auto-scroll, pause via `Space`).
  2. Slots snapshot (`idx | state | ctx used/max | client`).
  3. Métricas (sparkline `tokens/s` e `req/s` últimos 60s).
- `k` kill, `r` restart (kill + relaunch mesmo profile).

**`modelsPage`** — browser
- Top: status dos paths configurados (scanned/scanning/error).
- Tabela: `name | size | quant | params | path`. Filtro `/`.
- Enter: ações (`Use in new profile`, `Use in existing → pick`, `Reveal path`).
- `R` rescan.

### 7.3 Fluxos críticos

**F1 — Criar profile do zero**
1. Usuário em `profilesPage`, pressiona `n`.
2. Form vazio. Em `model`, `Enter` abre picker do `ModelScanner` (modal).
3. Preenche essenciais; warnings em tempo real.
4. `s` salva via `ProfileStore.Save`; lista atualiza.

**F2 — Lançar e observar**
1. Em `profilesPage`, pressiona `L` (ou navega para tab Launcher).
2. Confirma mode (background default). Validator final pre-launch (port livre, model existe).
3. `ProcessManager.Launch` retorna `RunningInstance`.
4. Auto-switch para tab Monitor; `WaitHealthy` em loop até 200 ou timeout.
5. Logs streaming.

**F3 — Multi-instance**
- Lançar segundo profile não interfere; `instances.json` cresce.
- `monitorPage` lista todas; usuário seleciona qual focar.

**F4 — Recuperar após reabrir TUI**
- Boot: `ProcessManager` lê `instances.json`, valida PIDs via gopsutil, descarta zumbis, restaura tail dos logs.

**F5 — Schema mismatch**
- Boot: `LlamaHelpParser.Parse` falha → fallback embutido + warn na statusbar. Editor exibe label `schema fallback`.

### 7.4 Keybindings globais

| Tecla | Ação |
|-------|------|
| `1`–`4` | Tab direto |
| `Tab` | Próxima tab |
| `?` | Help modal (markdown via glamour) |
| `q` / `Ctrl+C` | Quit (instâncias background sobrevivem) |

## 8. Tratamento de erros

**Princípios:**
- Service nunca panica; retorna erros tipados.
- UI captura erros de service e exibe na statusbar (vermelho para errors, amarelo para warnings) ou em modal quando bloqueante.
- Erros de launch capturam stderr dos primeiros 2s para exibição.
- `Ctrl+C` durante operação async: cancela `context.Context` propagado.

| Cenário | UX |
|---------|----|
| Parse de `--help` falha | Statusbar warn + fallback schema embutido |
| `llama-server` ausente do PATH | Modal bloqueante na boot, instrução para instalar `llama.cpp-cuda` |
| Profile JSON corrompido | Lista marca entry com `⚠`, exclui de operações até user fix/delete |
| Port busy no launch | Validator erro pre-launch + dica "trocar `port` ou matar PID X" |
| Model path missing | Validation error inline no editor |
| `nvidia-smi` ausente | Painel GPU mostra "n/a", Monitor não crasha |
| Crash do `llama-server` background | Detect via `kill -0` no próximo tick; Monitor mostra exit code dos logs |

## 9. Estratégia de testes

| Camada | Estilo | Cobertura alvo |
|--------|--------|----------------|
| `domain/` | nenhum (types puros) | n/a |
| `service/` | unit table-driven, `t.TempDir`, golden files | ≥85% |
| `ui/` | snapshot via `teatest` em fluxos-chave | seletiva |
| integração | end-to-end com fake `llama-server` em `testdata/` | smoke |

**Por service:**
- `ProfileStore` — CRUD em `t.TempDir`; teste de atomic write (kill mid-write não corrompe).
- `LlamaHelpParser` — fixtures de `--help` reais em `testdata/help-vX.Y.Z.txt`; parse → golden JSON do `FlagSchema`.
- `Validator` — tabela de profiles inválidos vs `ValidationReport` esperado.
- `ProcessManager` — fake binary `testdata/fake-llama-server.sh` que aceita flags, simula `/health` em port aleatório. Cobre launch, kill, recover, port-busy.
- `ModelScanner` — `t.TempDir` com fixtures de GGUF (header válido truncado); verifica scan, parse de metadata, cancel.
- `Monitor` — `httptest` para `/slots` e `/health`; `nvidia-smi` fake via PATH override; asserções no canal de events.

**UI:** `teatest` snapshots de 3-4 fluxos: criar profile, launch com validação, error display. Não pixel-perfect; verifica presença de strings/estado.

## 10. Estrutura de diretórios

```
llama.cpp-loader/
├── cmd/
│   └── model-loader/
│       └── main.go
├── internal/
│   ├── domain/
│   │   ├── profile.go
│   │   ├── flag_schema.go
│   │   ├── instance.go
│   │   └── monitor.go
│   ├── service/
│   │   ├── profilestore/
│   │   ├── llamahelp/
│   │   ├── validator/
│   │   ├── processmgr/
│   │   ├── modelscanner/
│   │   └── monitor/
│   ├── ui/
│   │   ├── root.go
│   │   ├── pages/
│   │   │   ├── profiles.go
│   │   │   ├── launcher.go
│   │   │   ├── monitor.go
│   │   │   └── models.go
│   │   ├── components/
│   │   └── theme/
│   └── config/
│       └── config.go
├── testdata/
│   ├── help-vX.Y.Z.txt
│   ├── fake-llama-server.sh
│   └── ggufs/
├── docs/
│   └── superpowers/
│       └── specs/
│           └── 2026-04-28-model-loader-design.md
├── go.mod
├── go.sum
├── README.md
├── LICENSE
└── .gitignore
```

`.gitignore` inclui `.superpowers/` (artefatos de brainstorm).

## 11. Roadmap de implementação (slices)

| Slice | Entrega | Testável end-to-end? |
|-------|---------|----------------------|
| 0 | Bootstrap go module, lipgloss theme, root tabs vazios | sim (smoke render) |
| 1 | `domain` + `ProfileStore` + `profilesPage` (lista, criar, salvar JSON) | sim (CRUD via UI) |
| 2 | `LlamaHelpParser` + `Validator` + tabs `Essentials`/`Advanced` | sim (fixtures help) |
| 3 | `ModelScanner` + `modelsPage` + picker integrado no editor | sim |
| 4 | `ProcessManager` + `launcherPage` (fg/bg) + recover `instances.json` | sim (fake-llama) |
| 5 | `Monitor` + `monitorPage` (logs+slots+GPU) + métricas | sim |
| 6 | Polimento, help (glamour), keybindings finais, error UX | sim |

Cada slice é merge-able sozinho; ferramenta utilizável a partir do final do slice 1.

## 12. Stack de dependências

| Lib | Uso |
|-----|-----|
| `github.com/charmbracelet/bubbletea` | Loop principal TUI |
| `github.com/charmbracelet/bubbles` | Componentes (list, table, textinput, viewport) |
| `github.com/charmbracelet/lipgloss` | Styling |
| `github.com/charmbracelet/huh` | Forms (input, select, confirm, validation) |
| `github.com/charmbracelet/glamour` | Render markdown no help modal |
| `github.com/spf13/viper` | App config (TOML) |
| `github.com/shirou/gopsutil/v3` | Process info, validação de PIDs, GPU stats fallback |
| `github.com/fsnotify/fsnotify` | Tail incremental de logs (Monitor) |

Std lib para HTTP (`net/http`), processos (`os/exec`, `syscall`), JSON (`encoding/json`).

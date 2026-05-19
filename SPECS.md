# SPECS — model-loader

> Especificação funcional de referência para uma reescrita completa do `model-loader`.
> Cobre **o que o app faz** (funcionalidades, regras, fluxos, persistência) deliberadamente sem prender o redesenho a Go, Bubbletea ou ao layout de pacotes atual. Sempre que detalhes de implementação aparecem é porque carregam decisão de produto.

- **Documento base:** árvore em `30d00b9` (`main`) — Go 1.26.2 + Charmbracelet bubbletea + lipgloss + huh + Viper.
- **Domínio:** TUI para gerenciar perfis, backends e processos de servidores LLM no estilo `llama-server` (família llama.cpp), com suporte a múltiplos backends (vLLM, SGLang, TabbyAPI).

---

## 1. Visão Geral

`model-loader` é uma aplicação **terminal-first** que substitui o ato manual de invocar binários `llama-server` (e equivalentes) por uma camada de **perfis declarativos versionados em disco**, com:

1. **Catálogo multi-backend** (llama.cpp, vLLM, SGLang, TabbyAPI) com schemas de validação próprios por backend.
2. **CRUD de perfis** com editor estruturado, sub-abas, undo, import/export, duplicação, "pin" e validação contra o schema do backend.
3. **Lançamento e supervisão de processos** que sobrevivem ao fechamento da TUI e são reconciliados na próxima inicialização.
4. **Monitoramento ao vivo** (GPU, slots, throughput, healthcheck, logs) com histórico em disco e gráficos.
5. **Descoberta de modelos** locais (`.gguf` via varredura recursiva) e remotos (busca na **Hugging Face Hub**), com gerenciador de downloads em workers detachados.
6. **Proxy reverso compatível com OpenAI** (`/v1/...`) com troca de backend dirigida pelo `model` do request — exposto como subcomando headless (`model-loader serve`) e como painel na TUI.
7. **CLIs auxiliares** para `serve`, `import` e `download`.

Plataforma alvo: Linux primário; macOS funciona; Windows não é alvo (uso de `Setsid`, sinais POSIX, nvidia-smi).

---

## 2. CLI / Pontos de Entrada

A primeira posicional decide o modo. Tudo abaixo aceita `--log-level=debug|info|warn|error` (sobrepõe `$MODEL_LOADER_LOG_LEVEL` e `logging.level` no TOML).

| Comando | Descrição |
|---------|-----------|
| `model-loader` *(sem subcomando)* | Sobe a TUI completa. |
| `model-loader serve [--host H] [--port P]` | Sobe **somente** o proxy HTTP em modo headless. Bloqueia até SIGINT/SIGTERM; shutdown gracioso de 30s. |
| `model-loader import <arquivo.json> [--mode=merge\|overwrite\|rename]` | Importa bundle de perfis sem abrir a TUI. Exibe `added/skipped/renamed/replaced`. |
| `model-loader download <state-path>` | Worker interno de download (NÃO chamado pelo usuário; é o subprocesso destacado que o `downloadmgr` da TUI spawna). Lê/escreve o arquivo de estado JSON apontado. Respeita `MODEL_LOADER_USER_AGENT`. |

Convenções herdadas: sair com background instances vivas imprime aviso em stderr e loga `orphan_background_instances`.

---

## 3. Configuração

### 3.1. Arquivo

Arquivo único TOML em `~/.config/model-loader/config.toml`. Criado com defaults na primeira execução (`SafeWriteConfigAs`). `~` é expandido para `$HOME` em todos os campos de path.

```toml
[paths]
profiles_dir = "~/.config/model-loader/profiles"
backends_dir = "~/.config/model-loader/backends"
log_dir      = "~/.local/state/model-loader/logs"
state_dir    = "~/.local/state/model-loader"
# llama_server_binary_path = ""   # opcional, fallback usado se o catálogo estiver vazio

[models]
search_paths = ["~/.lmstudio/models", "~/models"]

[ui]
default_tab = "launcher"   # "launcher" | "profiles" | "server" | "models" | "backends"
keybindings = "default"

[logging]
level = "info"             # "debug" | "info" | "warn" | "error"

[serve]
host = "127.0.0.1"
port = 4321
```

### 3.2. Comportamento de migração inline

- `ui.default_tab = "profiles"` legado é reescrito para `"launcher"` no boot e regravado no arquivo.
- Diretórios ausentes em `paths.*` são criados sob demanda pelos respectivos serviços (não no boot do config).

### 3.3. Layout em disco (após boot)

| Path | Conteúdo |
|------|----------|
| `~/.config/model-loader/config.toml` | Configuração do app. |
| `~/.config/model-loader/profiles/` | 1 arquivo JSON por perfil. `.previous/<id>.json` mantém o snapshot pré-última-edição (1 nível de undo). |
| `~/.config/model-loader/backends/catalog.json` | Catálogo de backends versionado (`schemaVersion: 1`). |
| `~/.config/model-loader/backends/schemas/*.json` | Schemas de validação por backend (path-traversal blindado). |
| `~/.local/state/model-loader/instances.json` | Registry atômico dos processos vivos (sobrevive à TUI). |
| `~/.local/state/model-loader/instances-history.json` | Histórico de exits (cap ~50 entradas). |
| `~/.local/state/model-loader/proxy-state.json` | Estado do proxy supervisor (PID, host, port). |
| `~/.local/state/model-loader/logs/<profile>-<port>.log` | stdout/stderr de cada instância (append). |
| `~/.local/state/model-loader/downloads/<id>.json` | Estado por download. |
| `~/.local/state/model-loader/exports/*.json` | Bundles exportados via tecla `e`. |
| `~/.local/state/model-loader/data/<profileID>.jsonl` | Histórico de métricas (JSONL, compactado). |
| `~/.local/state/model-loader/metrics/` | Snapshots adicionais por instância. |

---

## 4. Modelo de Domínio

### 4.1. Profile

Identidade + receita completa de lançamento. Persistido como um JSON por perfil (`<id>.json`). Campos:

| Campo | Tipo | Notas |
|-------|------|-------|
| `schemaVersion` | int | Atualmente `3`. Auto-migra em `Get()` para versões antigas. |
| `id` | string | Slug kebab-case ASCII (`Slugify`); imutável. Único no diretório. |
| `name` | string | Display humano. |
| `description`, `tags[]` | string / []string | Metadados livres. |
| `model` | string | Path absoluto para `.gguf` (ou repo:file no caso de outros backends). |
| `args` | `map[string]any` | Mapa de flag→valor. Inteiros voltam como `float64` pelo JSON; o validator desambigua. |
| `extraArgs[]` | string | Args extras concatenados *após* `args`. |
| `launch.defaultBackground` | bool | Default ao acionar Launch a partir do perfil. |
| `launch.logFilePath` | string | Override do arquivo de log. |
| `launch.backendId` | string | ID do backend escolhido. Vazio → usa default do catálogo. |
| `launch.env[]` | `{key,value}` | Vars de ambiente. Keys obrigam `[A-Za-z_][A-Za-z0-9_]*`. Sobrepõem o env herdado. |
| `launch.restart_policy` | `none\|on-failure\|always` | Política aplicada pelo watchdog. |
| `launch.max_restarts` | int | Teto absoluto. |
| `launch.backoff_seconds` | int | Base do backoff exponencial. |
| `launch.llamaServerBinaryPath` | string | **Legado** — preenchido somente para leitura na migração; nunca regravado. |
| `meta.createdAt`, `meta.updatedAt`, `meta.lastUsedAt` | timestamps | `createdAt` é imutável. `lastUsedAt` é setado a cada Launch. |
| `pinned` | bool | Pinned vai para o topo da lista. |

Slug rule: `Slugify(s)` mantém apenas letras/dígitos ASCII em lowercase, troca runs de não-ascii por `-`, com trim das pontas.

### 4.2. Backend e Catálogo

```
BackendCatalog
  schemaVersion: 1
  defaultBackendId: string
  backends: []Backend
    id, name, kind, executable, schemaRef, description, tags
    meta { createdAt, updatedAt, generatedAt, sourceVersion }
    properties: map[string]string
```

`Kind` ∈ `{llama-server, vllm, tabbyapi, sglang}`. `schemaRef` é caminho relativo a `backends/` apontando para o JSON do schema.

### 4.3. BackendValidationSchema

Envelope versionado em torno de um `FlagSchema`:

```
{ schemaVersion: 1, kind: "cli-flags.v1", backendKind, backendId,
  source: { generatedFrom, generatedAt, sourceVersion, editable },
  flags: { <name>: FlagSpec } }
```

`FlagSpec`: `Long`, `Short`, `Aliases[]`, `Type` (0=bool, 1=int, 2=float, 3=string, 4=enum), `EnumValues[]`, `Default`, `HelpText`, `Group` (`common`/`sampling`/`example-specific`/`embedded`).

Lookup do schema resolve por chave-do-mapa, long, short ou alias — todos válidos.

**Invariante:** `BackendValidationSchema.BackendID == Backend.ID` e `BackendKind == Backend.Kind`. Schemas com `source.editable=true` **nunca** são sobrescritos pela regeneração automática.

### 4.4. RunningInstance e ExitedInstance

`RunningInstance`: `{ profileId, pid, port, logPath, binaryPath, startedAt, background, crashed, exitedAt, restartCount, lastRestartAt, restartPolicy, maxRestarts, backoffSeconds, exitCode, exitSignal, exitReason, stderrTail[] }`.

`ExitedInstance` é o equivalente persistido em `instances-history.json` com `durationSeconds`.

`stderrTail` carrega as últimas ~50 linhas do log no momento do exit para enriquecer mensagens amigáveis de falha de launch.

### 4.5. ModelFile

`{ Path, Name, SizeBytes, Quant, Params, Architecture, BlockCount }`. `Quant`/`Params` são best-effort (heurística no filename) + `Architecture`/`BlockCount` lidos do header GGUF quando legível.

`ScanEvent` é o payload da varredura: tipos `File`, `Progress`, `Error`, `Done`. Erros por arquivo são silenciosamente degradados; erros por raiz emitem `ScanEventError` e abortam aquela raiz.

---

## 5. Catálogo de Backends

### 5.1. Operações

| Operação | Comportamento |
|----------|---------------|
| `AddBackend(name, executable, kind)` | **Gera o schema antes** de gravar o catálogo. Falha de geração → catálogo intacto, schema órfão é inofensivo. |
| `UpdateBackend` | Edita nome/executável/desc/tags. `kind` é imutável após criação. |
| `DeleteBackend(id)` | Apaga entrada do catálogo + arquivo de schema. Recusa se `id` é o default ou se algum perfil referencia. |
| `SetDefaultBackend(id)` | Atualiza `defaultBackendId`. |
| `RefreshSchema(id)` | Força regeneração, apagando o schema atual *mesmo se* `editable=true`. |
| `Resolve(Profile) → ResolvedBackend` | (1) pega `profile.launch.backendId` ou o default; (2) carrega schema; (3) resolve executable via PATH com fallback `python→python3` para vLLM/SGLang; (4) retorna `{ Backend, Schema, ExecutablePath }`. |
| `Probe(ctx) → chan ProbeEvent` | Roda `--version`/`--help` com timeout 10s por backend, emite latência e erro de cada um. |

### 5.2. Geradores de schema

- **llama-server:** spawn `--help`, parser regex em `llamahelp`, suporta multi-linha quando `=TYPE` quebra. Enums de cache-type hardcoded. Fallback é o schema embarcado em `llamahelp` (pinned em `v7376 (380b4c9)`).
- **vLLM e SGLang:** schemas **embarcados** (não dependem de binário). Servem como ponto de partida editável.
- **TabbyAPI:** kind reconhecido mas sem gerador dedicado (placeholder).

### 5.3. Resolução de binário (`llamabin`)

- Aceita absoluto, relativo ou nome em `PATH`.
- Suporta comando composto (`python -m sglang.launch_server`): se `python` não existir, tenta `python3` preservando args.
- Erros caem como `ResolveError` legível com hint de instalação.

### 5.4. Migração one-shot

Na primeira inicialização após a feature de catálogo, varre todos os perfis legados com `launch.llamaServerBinaryPath` setado:
- Se o catálogo estiver vazio, cria um backend `upstream` apontando para o binário herdado.
- Para cada path único, encontra/cria um backend e atribui `launch.backendId`, limpando o campo legado na próxima escrita.
- Emite `Report{CreatedBackends, MigratedProfiles, SchemaFailures, Warnings}`.

---

## 6. Profiles Store

### 6.1. CRUD

- `List() / ListWithDiagnostics()` — agrega arquivos do diretório; em modo *Diagnostics* continua na presença de JSONs corrompidos retornando os perfis íntegros + lista de problemas.
- `Get(id)` — auto-migração inline (schemaVersion bumps + campos novos).
- `Save(profile)` — **escrita atômica** (`tmp → fsync → rename`); antes de sobrescrever, copia o arquivo anterior para `.previous/<id>.json` (origem do undo).
- `Delete(id)` — remove arquivo + entrada de undo correspondente.
- `Duplicate(srcId, newName)` — clona, gera novo slug, **incrementa porta** para a próxima livre dentro do conjunto em uso, reseta `meta`.
- `MarkLastUsed(id, ts)` — chamado pelo processmgr no Launch.

### 6.2. Histórico, Import e Export

- **History** — append-only no arquivo de exit-history; usado pelo Server tab e por relatórios pós-mortem.
- **Export** — `ExportAll(dir)` produz bundle JSON com todos os perfis para o `<state-dir>/exports/<timestamp>.json` (tecla `e`).
- **Import** — `ImportBundle(path, mode)` com modos:
  - `merge` (default) — adiciona o que não conflita, pula o resto.
  - `overwrite` — sobrescreve conflitos.
  - `rename` — adiciona com sufixo numérico em conflito.
  Retorna `{Added, Skipped, Renamed, Replaced}`. O modal `ConflictModal` na TUI pergunta ao usuário em modo interativo.

### 6.3. Undo

Tecla `u` na lista carrega o snapshot em `.previous/<id>.json` para o perfil selecionado e mostra o **UndoModal** com o diff antes de gravar.

---

## 7. Validador

`Validator.Validate(Profile, FlagSchema) → Report{Errors, Warnings}`. Regras:

- Existência: `name`, `model`, `launch.backendId` (se catálogo populado), `port` quando exposto.
- Tipo: cada chave em `args` precisa bater com o `FlagSpec.Type` do schema. `int` aceita `float64` se for integral (artefato JSON). Enum exige valor em `EnumValues`.
- Flags desconhecidas: emit warning, **não** bloqueia (campos novos do upstream).
- `extraArgs` é varrido por prefixo `--`/`-` para detectar flags renomeadas.
- `env[].key` precisa casar `^[A-Za-z_][A-Za-z0-9_]*$`.
- `HasBlockingErrors()` é o portão de Launch; o editor mostra `Warnings` em amarelo.

---

## 8. Gerenciamento de Processos

### 8.1. Lançamento

`Launch(profile, mode={Foreground|Background}, attemptID) → RunningInstance`

1. `Resolver` retorna executável + schema.
2. `args.Build(profile, schema)` monta argv normalizando aliases longo/curto e respeitando `extraArgs`.
3. Spawn POSIX com `Setsid` (processo destacado da TUI). stdout/stderr → arquivo `<logDir>/<profileID>-<port>.log` em append.
4. Liveness loop (1s) pinga `GET /health` até 200 OK ou timeout (120s no proxy / 60s no Launcher).
5. PID é gravado em `instances.json` via `WriteJSONAtomic`. `MarkLastUsed` é chamado no `profilestore`.
6. Goroutine de `cmd.Wait` captura `ExitCode/Signal/Reason` + cauda do log (`stderrTail`) em um `ExitInfo` indexado por PID.

### 8.2. Background, Foreground e Recuperação

- **Background:** TUI sai → processos continuam (Setsid). Próxima inicialização lê `instances.json`, valida que cada PID está vivo e que `/health` responde no port; órfãos são removidos. Esse é o **Reconcile** chamado em boot.
- **Foreground:** stream de log na própria página; ao sair do app o processo é morto.

### 8.3. Kill, Restart e Watchdog

- `Kill(pid)`: SIGTERM → grace 10s → SIGKILL. Remove do registry, anexa entrada ao history, purga `ExitInfo`.
- `Restart`: kill + relaunch reusando o perfil capturado (sobrevive ao TOCTOU).
- **Watchdog (5s tick):** se `RestartPolicy != none`, detecta `exited+crashed` e dispara restart com **backoff exponencial** baseado em `BackoffSeconds`, respeitando `MaxRestarts`. `RestartCount` e `LastRestartAt` são persistidos no `RunningInstance`.

### 8.4. Histórico

`instances-history.json` é JSONL capado em 50 entradas (rotaciona FIFO). Cada `ExitedInstance` carrega `durationSeconds`, `exitCode`/`exitSignal`/`exitReason` e `stderrTail` para diagnóstico postmortem na aba Server.

### 8.5. Logs

- `processmgr.Logs.Tail(pid, n)` — n-últimas linhas do log de uma instância.
- O monitor mantém um *ring buffer* em memória (default 2000 linhas) e expõe novas linhas como eventos.

---

## 9. Scanner de Modelos

### 9.1. Comportamento

- `Scan(ctx, paths) → chan ScanEvent` em goroutine.
- Para cada raiz: walk recursivo, symlink resolvido up-front (`os.Lstat`/`os.Readlink`).
- Por `.gguf`:
  - Heurística no filename → `Quant`, `Params`.
  - Read GGUF header → `Architecture`, `BlockCount`.
  - Falha de read não aborta — emite `ModelFile` com campos vazios.
- Eventos: `Progress(0)` no início da raiz, `File(...)` por arquivo, `Progress(N)` no fim ou `Error(...)`, e `Done` global no final.
- `ctx` cancela a varredura inteira.

### 9.2. Quant heurística

Parser conhece nomes `Q2_K`, `Q3_K_S/M/L`, `Q4_0`, `Q4_K_M`, `Q5_K_S/M`, `Q6_K`, `Q8_0`, `F16`, `BF16`, `F32`, `IQ*`, e variantes — sempre lendo entre underscores/hifens no nome do arquivo.

---

## 10. Hugging Face Hub e Downloads

### 10.1. Cliente Hub (`hfhub`)

- `Search(ctx, query, limit) → []SearchResult{ID, Author, Downloads, Likes, LastModified, Tags}` chama `/api/models?search=...&full=true`.
- `RepoInfo(ctx, repoID) → *RepoInfo{ID, Tags, Siblings[{rfilename, size}]}` chama `/api/models/{id}`.
- `OpenDownload(ctx, repoID, filename) → (io.ReadCloser, contentLength, err)` para streaming.
- `DownloadURL(repoID, filename, revision)` para redirects do navegador.
- Respeita `Retry-After` em 429 (delta-seconds *ou* HTTP-date) com teto de 30s.
- `HF_BASE_URL` env permite redirecionar para mirrors privados. UA configurável.

### 10.2. Download Manager

- **Workers detachados:** cada download roda como subprocesso (`model-loader download <state-path>`) com `Setsid`, sobrevivendo à saída da TUI.
- Estado por download em `~/.local/state/model-loader/downloads/<id>.json`: `{spec, status, pid, bytes, total, startedAt, finishedAt, error}` com status `Active|Completed|Failed|Abandoned|Cancelled|Queued`.
- **Fila + capacidade:** default 3 slots simultâneos. Excesso vai para `Queued` e roda quando há vaga.
- `Reconcile()` no boot: valida PIDs vivos, marca órfãos como `Abandoned`.
- `StartPolling()` (500ms): detecta mudanças em disco e emite `Event{ID, State}` para a UI.
- `Cancel(id)` → SIGTERM ao worker (queued vira `Cancelled`). `Resume(id)` → re-spawn para `Failed`/`Abandoned`.
- `Snapshot()` retorna estado unificado ordenado por `StartedAt`.

### 10.3. Worker Protocol

O worker recebe **apenas** o `state-path`; lê o spec lá, escreve progresso (bytes, total, status, erro) de forma atômica, e termina quando o download finaliza, falha ou recebe SIGTERM. A TUI **nunca** confia no exit-code do worker — a verdade é o estado em disco.

---

## 11. Monitor de Instâncias

`Manager.Subscribe(pid, port, logPath) → (chan MonitorEvent, cancel)`. Inicia 4 produtores e mescla os eventos em um único canal:

| Fonte | Periodicidade | Conteúdo |
|------|---------------|----------|
| **Logs** | tail em tempo real | Linha bruta + parsing best-effort de `INFO/WARN/ERROR`. Ring buffer de 2000 linhas. |
| **Slots** | 1s | `GET /slots` (estado de cada slot: id, state, ctx-usado/total, client_id, tokens predicted). |
| **GPU** | 2s | `nvidia-smi` (formato `--query-gpu=memory.used,memory.total,utilization.gpu`). Fallback para `gopsutil` se nvidia-smi indisponível. Multi-GPU. |
| **Health** | 1s | `GET /health` → status booleano. |
| **Metrics** | janela rolante 60s | tokens/s, prompt-eval-tps, TTFT, req/s, slot utilization. |

Eventos são `{Timestamp, Source, Data}`. `cancel()` encerra todos os produtores. Persistência opcional via `metricsstore`.

### 11.1. Metrics Store

JSONL append em `<state>/data/<profileID>.jsonl`. `Append`, `Read(since)`, `Compact(retention, maxBytes)` (prune temporal + downsample). Alimenta o gráfico de histórico (modal `H`).

---

## 12. HTTP Proxy (OpenAI-Shape)

### 12.1. Endpoints

| Método/Path | Comportamento |
|-------------|---------------|
| `GET /v1/models` | Retorna lista derivada do `profilestore.List()` — cada perfil aparece como um `id` modelo. |
| `POST /v1/chat/completions`<br>`POST /v1/completions`<br>`POST /v1/embeddings`<br>`POST /v1/...` | Extrai `model` (do body JSON ou path `/v1/models/{id}/...`), faz **swap** de backend se necessário, encaminha via `httputil.ReverseProxy`. |
| `GET /_status` | Snapshot interno: `{ running, addr, loadedProfileID, loadedPID, loadedPort, lastSwapAt, lastSwapDur, lastError }`. Usado pelo `proxysupervisor.Status()`. |

### 12.2. Mecânica do swap

- Se nenhum backend está carregado, **carregar** o solicitado.
- Se um diferente está carregado, **matar** o atual via `processmgr.Kill` e lançar o novo (Background) com `WaitHealthy`.
- Swaps são serializados em mutex; requests concorrentes aguardam.
- Erros: `errProfileNotFound`, `errBackendUnhealthy`, `errSwapFailed` retornam JSON OpenAI-like (`{error:{message,type,code}}`).
- `MaxBodyBuffer = 8 MiB` (request body é bufferizado para permitir extrair `model` mesmo após streaming).
- `HealthCheckTimeout = 120s`; `ShutdownGracePeriod = 10s`.

### 12.3. Proxy Supervisor

Camada em torno do proxy quando rodando *pela TUI*:
- Spawna `model-loader serve --host H --port P` como subprocesso detachado.
- Espera o port aceitar conexão (10s timeout).
- Persiste `proxy-state.json{ PID, Host, Port, StartedAt }`.
- `Status()` chama `/_status` (timeout 500ms) e devolve para a aba Server.
- `Reconcile()` no boot valida PID + port; órfão → derruba arquivo de estado.
- `Stop()` é SIGTERM → grace 10s → SIGKILL.

---

## 13. Sizing / VRAM Suggestion

Funções puras consumidas pelo editor de perfis:

- `Suggest(modelSizeBytes, totalLayers, freeVRAMBytes) → {NGL, Utilization%, BytesPerLayer}` — calcula `NGL` máximo que cabe em ~90% da VRAM livre.
- `Fit(modelSizeBytes, totalLayers, ngl, freeVRAMBytes) → FitStatus{Green|Yellow|Red}` — verde abaixo de 85%, amarelo até 100%, vermelho acima.

A sub-aba **Sizing** do editor (acionável via `NavigateToSizingMsg` da aba Models) renderiza essas leituras + slider/input para `n-gpu-layers`.

---

## 14. Logging

Logger global slog file-only por sessão. Configuração:

- Diretório: `paths.log_dir`.
- Nível: CLI flag > env (`MODEL_LOADER_LOG_LEVEL`) > config (`logging.level`) > default `info`.
- Um arquivo por sessão, com rotação por sessão (não por tamanho).
- Boot logo após sucesso do logger: `app_start{log_dir, state_dir, level}`. Saída: `app_exit{residual_instances}`.

Falha em montar o logger → boot aborta (decisão deliberada: telemetria de debug é obrigatória após `config.Load`).

---

## 15. Camada de UI / TUI

A UI tem **uma única raiz** que despacha entre 4 abas top-level. O comportamento global é determinado por um pequeno conjunto de contratos opcionais que cada página pode implementar.

### 15.1. Abas

> Implementação atual define `TabProfiles=0, TabServer=1, TabModels=2, TabBackends=3`. A aba Launcher historicamente referenciada pela documentação **foi fundida** na aba Profiles (lançamento direto a partir da lista) e na aba Server (controle do que está rodando). Para a reescrita: tratar como **4 abas** é a verdade do código.

| # | Aba | Função |
|---|-----|--------|
| 1 | **Profiles** | CRUD de perfis + lançar diretamente da lista. |
| 2 | **Server** | Painel do proxy + tabela de instâncias + sub-views (Logs/Slots/Metrics/History). |
| 3 | **Models** | Browser de `.gguf` locais + busca na HF Hub + fila de downloads. |
| 4 | **Backends** | CRUD do catálogo + probe + regenerate schema. |

### 15.2. Layout do frame

```
┌──────────────────────────────────────────────────┐
│  1 Profiles   2 Server   3 Models   4 Backends   │  ← tabbar (1 linha)
├──────────────────────────────────────────────────┤
│                                                  │
│                  body da aba ativa               │
│                                                  │
├──────────────────────────────────────────────────┤
│ [1-4] tabs  [tab] next  [q] quit  [?] help  | …  │  ← statusbar com hints
└──────────────────────────────────────────────────┘
```

Modais (help, boot-blocker, playground, picker, confirms) são compostos via **Overlay** centralizado.

### 15.3. Contratos de página

Toda página é um `Page{Init, Update, View}`. Os opcionais são acionados pela raiz:

| Contrato | Quando |
|----------|--------|
| `InputCapture.IsCapturingInput() bool` | Página tem editor/picker/confirm aberto → todas as shortcuts globais com runes imprimíveis (`q`, `1-4`, `tab`, `?`) ficam **desativadas**; só `ctrl+c` escapa. |
| `Reloader.Reload() tea.Cmd` | Ao receber foco da tab, recarrega estado (e.g., relistar perfis após edição externa). |
| `HintProvider.Hints() string` | Dicas locais concatenadas à statusbar global. Recomputadas em todo state-change. |
| `HelpContextProvider.HelpContext() string` | Markdown rico para o modal de ajuda. Fallback é `Hints()`. |
| `Overlayer.OverlayView() (content, w, h, active)` | Render do modal *por cima* do body da aba. |

### 15.4. Keybindings globais

| Tecla | Ação | Comentário |
|------|------|------------|
| `ctrl+c` | Quit | **Sempre** ativo. |
| `1`–`4` | Ativa a aba N | Suprimido se a aba captura input. |
| `tab` / `shift+tab` | Próxima / anterior | Suprimido se a aba captura input. |
| `?` | Abre o modal de ajuda | Suprimido se a aba captura input. `?`/`esc` fecha. |
| `q` | Quit | Suprimido se a aba captura input. |
| `ctrl+p` | (reservado para Playground; **desabilitado** no momento) | Modal mantido na árvore para evitar churn. |

### 15.5. Mensagens cross-tab

A raiz é o único roteador. Mensagens conhecidas:

- `UseInNewProfileMsg{ModelPath}` — Models → Profiles: ativa Profiles e abre editor de perfil novo pré-preenchido.
- `LaunchProfileMsg{ID}` — externo → Profiles: força launch direto.
- `SwitchToServerMsg{PID}` — Profiles → Server: ativa Server e seleciona a linha do PID via `ServerSelectPIDMsg`.
- `NavigateToSizingMsg{ProfileID}` — Models → Profiles: ativa Profiles e abre o editor na sub-aba Sizing.

Para tarefas assíncronas (scanner, monitor, downloads), as mensagens são **broadcast** para todas as páginas para que a página dona receba mesmo sem foco.

### 15.6. Boot Blocker

Modal bloqueante exibido quando um recurso crítico falta no boot (ex.: `llama-server` fora do `PATH` e catálogo vazio). Apenas `q`/`ctrl+c` respondem; resize é tracked para re-renderizar.

---

## 16. Aba Profiles

### 16.1. Layout

- **Esquerda (~1/3):** lista filtrável de perfis. Pinned no topo. Mostra nome, backend, porta, indicador de instância rodando.
- **Direita (~2/3):** detalhe do perfil selecionado + bloco de instâncias dele.

### 16.2. Modo Lista (sem editor)

| Tecla | Ação |
|------|------|
| `enter` | Lança o perfil selecionado em modo `defaultBackground`. Em sucesso → mensagem para Server tab. |
| `E` | Abre editor para o perfil. |
| `n` | Cria novo perfil (editor com `IsNew=true`). |
| `d` | Duplica perfil. |
| `x` | Deleta com confirmação. |
| `b` | Toggle global foreground/background do próximo launch. |
| `k` | Kill da instância mais recente do perfil selecionado (confirm). |
| `r` | Refresh manual da lista. |
| `p` | Pin/unpin. |
| `e` | Exporta todos os perfis para bundle em `state/exports/`. |
| `I` | Importa bundle (file picker → modo de conflito → `ConflictModal`). |
| `u` | Undo: lê `.previous/<id>.json`, mostra `UndoModal` com diff, confirma. |
| `/` | Modo filtro substring case-insensitive. |

### 16.3. Editor (modo edição) — sub-abas

Cycle via `ctrl+t`: **Essentials → Advanced → Environment → Sizing → Essentials**.

- **Essentials** — huh form: Name, Model (com `ctrl+p` para abrir ModelPicker), Backend (select do catálogo), `n-gpu-layers`, `ctx-size`, `batch-size`, `ubatch-size`, `port`, `flash-attn`, `cache-type-k`/`cache-type-v`, RestartPolicy, MaxRestarts, BackoffSeconds.
- **Advanced** — tabela de flags do schema do backend, filtrável por nome; valores editáveis inline. Flags desconhecidas no schema aparecem como warning.
- **Environment** — lista de pares `KEY=VALUE`. Adicionar/editar/remover. Validação POSIX no key.
- **Sizing** — leitura de `nvidia-smi` + sliders interativos para `n-gpu-layers`. Mostra `FitStatus` (verde/amarelo/vermelho).

| Tecla (editor) | Ação |
|---------------|------|
| `tab`/`shift+tab` | Mover entre campos do huh form. |
| `ctrl+t` | Próxima sub-aba. |
| `ctrl+p` | Abre ModelPicker. |
| `enter` no botão Save | Persiste com `Save()` + redireciona para a lista com flash de sucesso. |
| `esc` | Se dirty → confirm "Discard changes?"; se limpo → fecha direto. |

### 16.4. Estado do draft

Editor mantém **draft em memória** desacoplado do disco até Save. Slug auto-gerado se o usuário não preenche ID. Migrações inline (defaults) acontecem na conversão Profile → draft.

---

## 17. Aba Server (Monitor + Proxy)

### 17.1. Layout

- **Topo:** painel do **HTTP proxy** (start/stop, status, endpoint, último erro).
- **Meio:** tabela de instâncias rodando (PID, Port, Profile, Uptime, VRAM, Tokens/s).
- **Base:** uma das 4 sub-views da instância selecionada.

### 17.2. Sub-views

Cycle via `v`:

- **Logs** — Ring buffer de 2000 linhas com `space` para pausar update visual (o buffer continua sendo escrito).
- **Slots** — tabela: `ID | State | Ctx used/max | Client | Tokens predicted`.
- **Metrics** — sparklines (`sparkline.go`) para tokens/s e req/s + barras de VRAM e GPU util.
- **History** — tabela do `instances-history.json` (profile, PID, started/exited, duration, reason, stderrTail).

### 17.3. Keybindings

| Tecla | Ação |
|------|------|
| `v` | Cicla sub-view. |
| `space` | Pausa logs (toggle). |
| `k` | Kill da instância selecionada (confirm). |
| `r` | Restart da instância (kill + relaunch). |
| `H` | Abre `HistoryChart` (modal) com janelas `1h / 6h / 24h / 7d` via `1/2/3/4`. |
| `s` / `x` | Start/Stop do proxy (no painel superior). |
| `esc` | Fecha modal/confirm. |
| `↑↓` / `j/k` | Navega tabela. |
| `enter` | Subscreve a instância (inicia produtores no `monitor`). |
| `u` | Cancela subscrição. |

### 17.4. Painel do proxy

- `s` start: chama `ProxySupervisor.Start(ctx)`.
- `x` stop: chama `ProxySupervisor.Stop(ctx)`.
- Poll de status a cada 1s: liga/desliga indicador, mostra `addr`, último profile carregado, last swap dur, último erro.

---

## 18. Aba Models

### 18.1. Layout

- **Esquerda:** tabela filtrável (`Name | Size | Quant | Params | Path`).
- **Linha de status:** progresso de scan por raiz (`scanning`, `scanned [N]`, `error`).
- **Direita (toggleável com `i`):** painel de info com metadados (params, quant, arch, block count) e lista "Used by N profiles".
- **Rodapé (toggleável com `d`):** `DownloadProgress` com fila/ativos/concluídos e barras de progresso.

### 18.2. Action menu (overlay sobre `enter`)

Lista vertical com:
1. **Use in new profile** → emite `UseInNewProfileMsg`.
2. **Use in existing profile** → abre `ProfilePicker` → atualiza `profile.model` + save.
3. **Copy path to clipboard** (via `atotto/clipboard`).
4. **Delete file** (confirm de filesystem).

### 18.3. Keybindings

| Tecla | Ação |
|------|------|
| `/` | Modo filtro. |
| `R` | Re-scan (Reloader + flash "rescan started"). |
| `i` | Toggle info panel. |
| `→` / `g` | Se o info panel está aberto e o modelo é usado por **exatamente 1** perfil → emite `NavigateToSizingMsg` (vai pro editor naquele perfil em Sizing). |
| `enter` | Action menu. |
| `s` | Abre `HFSearchPicker` (busca remota na HF Hub). |
| `d` | Toggle do `DownloadProgress` no rodapé. |
| `x` | Cancel do download selecionado. |
| `r` | Resume do download selecionado. |
| `esc` | Fecha info/filtro/picker. |

### 18.4. HF Search Picker

- Input de query → debounced 300ms → `hfhub.Search(ctx, q, 20)`.
- Resultado: tabela `ID | Author | Downloads | Likes | LastModified | Tags`.
- `enter` → `hfhub.RepoInfo` → abre `HFFilePicker` com a lista de `siblings` (checkboxes).
- `enter` na seleção → cria download(s) via `downloadmgr.Start(spec)`.

---

## 19. Aba Backends

### 19.1. Layout

Master/detail: lista (esquerda) com nome + kind + indicador de default; painel (direita) com `ID, Name, Kind, Executable, Description, Tags, CreatedAt, UpdatedAt`, último resultado de probe (status + latência).

### 19.2. Keybindings

| Tecla | Ação |
|------|------|
| `n` | New backend — huh form (Name, Kind select, Executable, Description, Tags). Em Save, gera schema antes de persistir o catálogo. |
| `e` / `enter` | Edita selecionado. Kind é read-only. |
| `x` | Deleta (confirm). Recusa se default ou referenciado por perfil. |
| `D` | Define como default. |
| `R` | Regenera o schema (confirm). Force-overwrite mesmo com `editable=true`. |
| `P` | Probe em todos os backends (async, emite eventos no painel). |
| `/` | Filtro. |

---

## 20. Componentes Reutilizáveis (UI)

Catálogo do que precisa existir como bloco; nomes são guias para a reescrita.

| Componente | Função |
|-----------|--------|
| **TabBar** | Tabs horizontais com indicador de overflow. |
| **StatusBar** | Footer com hints e contador de restarts agregados (`m.pm.List()`). |
| **Modal / Overlay** | Modal centralizado + composição com o body principal. |
| **Confirm** | Yes/No (huh.Form) com callback. `IsActive()` para o portão de captura. |
| **Flash** | Status messages auto-clearing (6s normal, 15s erro). Bound to página. |
| **Picker** | Picker genérico keyboard-driven (base de outros pickers). |
| **ProfilePicker** | Picker específico de perfis. |
| **ModelPicker** | Picker de `.gguf` locais (chama o scanner internamente). |
| **HFSearchPicker** | Busca + lista da HF Hub. |
| **HFFilePicker** | Lista de arquivos de um repo HF, multi-select. |
| **DownloadProgress** | Footer com barras de progresso por download. |
| **ProxyPanel** | Painel autônomo do proxy (start/stop/status, 1s poll). |
| **Sparkline** | Mini-gráfico 1D para tokens/s, req/s. |
| **HistoryChart** | Modal de gráfico de histórico (windows 1h/6h/24h/7d). |
| **InfoPanel** | Painel lateral de metadados (Models tab). |
| **EmptyState** | Mensagem "no data" + prompt. |
| **Loading** | Spinner inline / placeholder. |
| **Help** | Modal de help renderizado com glamour markdown. |
| **ConflictModal** | Decisor de modo de import (skip/overwrite/rename). |
| **UndoModal** | Mostra diff e confirma reverter para `.previous/`. |
| **PlaygroundModal** | Modal de chat playground (atualmente disabled; estrutura mantida). |
| **Filter helper** | `ContainsFold(items, filter, accessor)` — fuzzy substring case-insensitive. |

### 20.1. Tema

Cores adaptativas (claro/escuro): `accent` (azul), `ok` (verde), `warn` (laranja), `error` (vermelho), `dim` (cinza). Honra `NO_COLOR` desligando foreground/background.

Helpers de layout: `BodyHeight(h)`, `ClampBody(s, w, h)`, `SplitTwoPanes(w)`.

---

## 21. Persistência e Atomicidade

- **Toda escrita JSON** crítica (perfis, catálogo, schemas, instances, history, proxy-state, downloads) usa `WriteJSONAtomic`: tmp file no mesmo diretório → `fsync` → `rename`.
- Catálogo e schemas têm **path-traversal guard** (rejeita `..`, paths absolutos em `schemaRef`).
- Profiles store mantém **um nível de undo** por perfil em `.previous/<id>.json`.
- Process registry e proxy state têm **reconcile no boot** (PID alive + porta responsiva) para podar órfãos.
- History files (instances, metrics) são **append-then-truncate** ou JSONL com **compactação por retenção e tamanho** (`metricsstore.Compact`).

---

## 22. Concorrência

Padrões observados a preservar:

- **Goroutines por instância** no monitor (logs/slots/gpu/health) com `cancel()` central.
- **Worker subprocess por download** (sobrevive à TUI).
- **Watchdog de processos** com tick 5s.
- **Polling do downloadmgr** com tick 500ms.
- **Proxy server** com WaitGroup de inflight para drain no Stop.
- **Mutexes**: swap do proxy é serializado; profilestore tem RW-mutex em torno do diretório; registry/manager têm mutex próprio.
- **Cancellation** sempre via `context.Context` para spawns externos (`nvidia-smi`, `--help`, HF API).

---

## 23. Telemetria e Eventos Loggados (chaves notáveis)

- `app_start`, `app_exit`, `boot_failed`, `migration_failed`, `migration_warning`.
- `reconcile_failed`, `download_reconcile_failed`, `proxy_reconcile_failed`.
- `default_catalog_load_failed`, `default_catalog_save_failed`, `default_catalog_schema_missing`.
- `orphan_background_instances`, `orphan_instance`, `orphan_remediation_hint`.
- `serve_listening`, `serve_signal_received`, `serve_stop_failed`, `serve_start_failed`.
- `tui_error`, `import_failed`.

Esses são contratos de observabilidade — vale preservar nomes para não quebrar dashboards/scripts existentes.

---

## 24. Erros e Mensagens

- Proxy retorna erros em formato OpenAI (`{error:{message,type,code}}`) com tipos: `profile_not_found`, `backend_unhealthy`, `swap_failed`, `bad_request`, `internal_error`.
- Validator gera `FieldIssue{Field, Message, Severity}`.
- Launch errors são enriquecidos com `stderrTail` (últimas ~50 linhas do log da instância falha) → `friendlyLaunchError`.
- Modais de confirmação **nunca** assumem default destrutivo (Delete, Discard, Overwrite vêm com seleção em `No`).

---

## 25. Pontos Explícitos a Repensar na Reescrita

Itens marcados pela base de código como restrições, débitos ou anti-padrões que **não** precisam ser carregados como invariantes:

1. **A regra "global shortcut gate + InputCapture"** é um sintoma de Bubbletea — em outro framework com foco-por-widget esse gate pode desaparecer.
2. **Forward de não-`KeyMsg` para huh** é específico do handshake do huh.Form; não é regra de domínio.
3. **Golden tests baseados em snapshot textual** acoplam UI ao layout — em uma reescrita com framework gráfico real, snapshot HTML/DOM é alternativa mais barata.
4. **5 tabs no README vs 4 tabs no código** — o launcher virou parte de Profiles+Server. Decidir cedo no redesenho se isso fica fundido ou se é uma "aba launcher" dedicada.
5. **Embedded schema pinned em `v7376`** — uma reescrita pode preferir distribuir o schema fora do binário (download on first run, refresh contínuo).
6. **`Setsid`/POSIX-only** — se Windows entra no escopo, repensar como detachar processos e como matar (`Job Objects`, `taskkill`).
7. **`nvidia-smi` parsing** — substituir por NVML/HIP/Metal/MPS APIs nativas é mais robusto e abre suporte a multi-vendor.
8. **JSON-per-profile na FS** — funciona para dezenas/centenas; para centenas-de-milhares vale considerar SQLite (com FTS para busca).
9. **Worker de download como processo separado** — útil para sobreviver à TUI, mas adiciona complexidade. Em arquitetura cliente/servidor (daemon + frontends), o daemon naturalmente faz isso.
10. **Catálogo com `defaultBackendId` ambíguo quando vazio** — vale tornar o default *explícito* (rejeitar profiles sem `backendId` em vez de adivinhar).
11. **Sem locks entre múltiplas instâncias da TUI** — duas TUIs simultâneas podem corromper `instances.json`. Repensar com lockfile ou daemon central.
12. **`stderrTail` capturado por arquivo de log** — flaky se o usuário rotaciona logs externos. Capturar via `pipe` + ringbuffer no processmgr é mais robusto.

---

## 26. Inventário Resumido para o Redesenho

| Camada | Responsabilidade central |
|--------|--------------------------|
| **Config** | TOML único + defaults + tilde-expand. |
| **Domain** | `Profile`, `Backend`, `BackendCatalog`, `FlagSchema`, `BackendValidationSchema`, `RunningInstance`, `ExitedInstance`, `ModelFile`, `LogLine`. |
| **Backend Catalog** | CRUD do catálogo, geradores de schema por kind, prober, resolver com fallback Python. |
| **Schema generators** | llama-server (parse `--help`), vLLM e SGLang (embarcados), TabbyAPI placeholder. Schemas editáveis nunca sobrescritos. |
| **Profile Store** | FS-based CRUD com escrita atômica, undo 1-nível, import/export, migração inline. |
| **Validator** | Validação por schema + regras fixas (env, port, model). Errors bloqueiam launch, Warnings só sinalizam. |
| **Process Manager** | Launch (Foreground/Background) + Kill + Restart + Recover + History + Liveness + Watchdog com restart policy. |
| **Model Scanner** | Walk recursivo de paths configurados, parse GGUF header best-effort, eventos streamados. |
| **HF Hub Client** | Search + RepoInfo + DownloadURL + streaming, com Retry-After. |
| **Download Manager** | Workers detachados, fila por capacidade, estado por arquivo, reconcile no boot. |
| **Monitor** | Subscribe por instância: logs (ring buffer), slots (1s), GPU (2s, nvidia-smi+fallback), health (1s), métricas (janela 60s). |
| **Metrics Store** | JSONL por perfil com compactação. |
| **HTTP Proxy** | `/v1/*` OpenAI shape + `/_status`, swap por `model`, body buffer 8 MiB. |
| **Proxy Supervisor** | Lifecycle do proxy como subprocesso, reconcile no boot. |
| **Sizing** | Sugestão de `n-gpu-layers` e fit-status para VRAM. |
| **Logging** | slog file-only, rotação por sessão, nível resolvido em cascata. |
| **UI** | 4 tabs (Profiles / Server / Models / Backends), modais centrais, statusbar dinâmico, 20+ componentes reutilizáveis. |

---

## 27. Glossário

- **Backend** — Implementação concreta de um servidor LLM (llama-server, vLLM, SGLang, TabbyAPI).
- **Catalog** — Registry de backends (`catalog.json`) + schemas por backend.
- **Schema** — `BackendValidationSchema`: descrição das flags válidas + metadados de origem.
- **Profile** — Receita de lançamento de uma instância (modelo + args + env + política de restart).
- **Instance** — Processo vivo de um backend com PID/port/log.
- **Slot** — Sessão lógica dentro de um servidor llama-server (contexto, cliente).
- **Probe** — Health check de um backend (executa `--version`/`--help` com timeout).
- **Swap** — Troca de backend ativo no proxy ao receber um `model` diferente do atual.
- **Editable schema** — Schema com `source.editable=true`, protegido contra regeneração automática.
- **Reconcile** — Conferência no boot entre estado persistido e o que de fato está vivo (PIDs, ports).

---

*Fim do documento.* Este SPECS.md descreve **comportamento e contratos**, não implementação. Use-o como referência para definir módulos, APIs e padrões de design do novo `model-loader`.

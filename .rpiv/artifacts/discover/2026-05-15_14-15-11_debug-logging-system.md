---
date: 2026-05-15T14:15:11-0300
author: quantmind-br
commit: 3e39508
branch: main
repository: model-loader
topic: "debug-logging-system"
tags: [intent, frd, logging, processmgr, observability, slog]
status: complete
last_updated: 2026-05-15T14:15:11-0300
last_updated_by: quantmind-br
---

# FRD: Sistema de logs para debug do load de modelos

## Summary
Adicionar um logger estruturado (stdlib `log/slog`) ao model-loader, instrumentado no caminho de carregamento de modelo (resolve → validate → spawn → wait → liveness → reconcile), com saída em arquivo rotacionado por sessão em `~/.local/state/model-loader/logs/model-loader.log`. Em paralelo, corrigir os `_ = cmd.Wait()` em `internal/service/processmgr/manager.go:149` e `:340` para capturar exit code, sinal e últimas 50 linhas de stderr do llama-server, persistindo essa causa no `RunningInstance` e mostrando-a no TUI. O log do processo filho (`<profile>-<port>.log`) continua existindo, separado do log do model-loader.

## Problem & Intent
O usuário quer telemetria ampla que permita debugar problemas no load de modelos com os diversos backends suportados (`llama-server`, `sglang`, `vllm` — vide `internal/service/processmgr/args.go:44-160`). Hoje, quando um load falha:

- Erros pré-spawn caem em `friendlyLaunchError` (`internal/ui/pages/launcher.go:73-87`), que só conhece 4 sentinels. Qualquer outra falha vira `"error: " + err.Error()` no status bar e some no `flashClear`.
- Erros pós-spawn (binário mata em runtime, flag inválida, GPU sem VRAM) viram um `ErrHealthCheckTimeout` genérico após 30 s, com mensagem "server did not become healthy within timeout — check logs". O usuário não sabe **qual** log, **onde** está, nem **o que** o llama-server reclamou.
- `cmd.Wait()` em `internal/service/processmgr/manager.go:149` e `:340` descarta o `error` com `_ =`. Exit code e sinal são perdidos.
- `liveness.go:50-52` marca `Crashed=true` a cada 5 s sem causa anexa.
- `recover.go:60` deleta entradas mortas de `instances.json` silenciosamente no boot, sem deixar registro do que aconteceu enquanto o TUI esteve fechado.
- O código do model-loader não usa nenhum logger estruturado — só 13 `fmt.Fprintf(os.Stderr,...)` em `cmd/model-loader/main.go` e `internal/config/config.go:82`, que somem assim que o usuário fecha o terminal.

A intenção é fechar essa lacuna com um sistema de logs próprio do model-loader que sobrevive a sessões e dá ao usuário um arquivo único para anexar quando algo der errado.

## Goals
- Eventos do caminho de load (resolve, validate, spawn, wait, exit, liveness, reconcile) ficam registrados num arquivo persistente, com timestamp, nível e contexto estruturado.
- Quando um `llama-server` morre, o usuário vê a causa real (exit code, sinal, tail de stderr) no TUI — não mais "did not become healthy within timeout".
- Verbosidade ajustável sem editar config: env `MODEL_LOADER_LOG_LEVEL` ou flag CLI `--log-level`.
- O usuário consegue anexar **um** arquivo (`model-loader.log`) num bug report e ter contexto suficiente do que aconteceu na sessão.
- Zero risco de o logger corromper o render do bubbletea — nada vai para stderr enquanto o TUI estiver ativo.

## Non-Goals
- Painel de tail ao vivo no Tab Monitor — fica para uma FRD futura; nesta entrega o usuário lê o arquivo num segundo terminal.
- Substituir o log per-instância `<profileID>-<port>.log` (`internal/service/processmgr/manager.go:111-124`) — ele continua sendo a saída crua do processo filho, separado do log do model-loader.
- Instrumentação de camadas fora do caminho de load (`internal/ui/pages/monitor.go`, `internal/service/profilestore/`, `internal/service/modelscanner/`) — fora desta FRD.
- Envio remoto de telemetria, agregação externa, métricas Prometheus — não.
- Rotação por tamanho com compressão (lumberjack) — usaremos rotação por sessão, sem dep nova.
- Mascaramento/scrubbing automático de PII — log de `debug` pode incluir paths e args.

## Functional Requirements

1. **Logger central**: O sistema SHALL expor um pacote `internal/log/` (ou equivalente) que retorne um `*slog.Logger` configurado a partir de um `Config{Level, FilePath}`. Outros pacotes SHALL receber esse logger por injeção (parâmetro de construtor / `Manager` config), não como singleton global.
2. **Sink em arquivo**: O logger SHALL escrever em `cfg.Paths.LogDir + "/model-loader.log"` (default `~/.local/state/model-loader/logs/model-loader.log`, herdado de `internal/config/config.go:116`), usando `slog.NewTextHandler` em formato `key=value`.
3. **Rotação por sessão**: Ao inicializar, se `model-loader.log` já existir, o sistema SHALL renomeá-lo para `model-loader.<YYYY-MM-DDTHH-MM-SS>.log` e SHALL manter no máximo 5 arquivos rotacionados, removendo os mais antigos.
4. **Nível dinâmico**: O nível default SHALL ser `info`. O sistema SHALL aceitar override por env `MODEL_LOADER_LOG_LEVEL=debug|info|warn|error` e por CLI flag `--log-level <level>`, com precedência CLI > env > config > default.
5. **Sem stderr durante TUI**: Enquanto `tea.Program.Run()` estiver ativo, o handler SHALL escrever apenas no arquivo. Erros pré-boot (antes do `Run()`) SHALL ir tanto para stderr quanto para o arquivo.
6. **Migração de `Fprintf(os.Stderr,...)`**: As 12 ocorrências em `cmd/model-loader/main.go` e a ocorrência em `internal/config/config.go:82` SHALL ser substituídas por chamadas equivalentes ao logger (`logger.Error` / `logger.Warn`).
7. **Pontos de instrumentação obrigatórios** (info+ por default; campos extras em debug):
   - `internal/service/backendcatalog/resolver.go:34` — `resolve_backend_start` / `resolve_backend_ok` (com `backend_kind`, `executable`, `schema_file` em debug) / `resolve_backend_err`.
   - `internal/ui/pages/launcher.go:401` — `validate_start` / `validate_ok` (com contagem de regras em info; com cada warning em debug) / `validate_blocked` (com lista de erros).
   - `internal/service/processmgr/manager.go:73-156` — `spawn_start` (info), `spawn_args` (debug — args literais), `spawn_started` (info, com `pid`, `port`, `log_path`), `spawn_failed` (error, com causa).
   - Goroutine de `cmd.Wait` (`manager.go:149` e `:340`) — `process_exited` com `exit_code`, `signal`, `duration_ms`, `reason` e `stderr_tail_lines` (contagem).
   - `internal/service/processmgr/liveness.go:50` — `liveness_crash_detected` com `pid`, `last_seen`.
   - `internal/service/processmgr/recover.go` — `reconcile_kept` / `reconcile_dropped` (com `pid`, `reason`) por entrada.
   - `internal/ui/pages/launcher.go:204` — `healthcheck_timeout` quando `WaitHealthy` falha, com referência ao `attempt_id`.
8. **Correlação por `attempt_id`**: A cada `launchProfileCmd` (`internal/ui/pages/launcher.go:382`), o sistema SHALL gerar um identificador curto (ULID, UUID truncado, ou contador monotônico encodado em base32) e anexá-lo via `logger.With("profile_id", p.ID, "attempt_id", id)` a todos os eventos derivados (resolve, validate, spawn, wait, healthcheck). O `attempt_id` SHALL ser propagado para a goroutine de `Wait` via fechamento ou campo em `RunningInstance` (não persistido).
9. **Captura de causa no exit**: As goroutinas de `cmd.Wait` em `manager.go:149` e `:340` SHALL inspecionar o `error` retornado. Se for `*exec.ExitError`, extrair `ExitCode()` e `ProcessState.Sys()` para o sinal (em Linux, `syscall.WaitStatus`).
10. **`RunningInstance` estendido**: O struct em `internal/domain/instance.go:6` SHALL ganhar os campos:
    - `ExitCode *int json:"exitCode,omitempty"`
    - `ExitSignal string json:"exitSignal,omitempty"`
    - `ExitReason string json:"exitReason,omitempty"` (texto curto humano: "exit 1", "killed by SIGSEGV", "OOM-killed", "healthcheck timeout")
    - `StderrTail []string json:"stderrTail,omitempty"` (capacidade máxima 50 linhas)
    Estes campos SHALL ser preenchidos pela goroutine de `Wait` quando o processo morre.
11. **Tail de stderr**: Ao detectar exit, a goroutine SHALL ler as últimas 50 linhas de `LogPath` (seek-from-end, buffer ring) e armazenar em `StderrTail`. Em foreground (`LogPath==""`), `StderrTail` permanece vazio.
12. **Surface no TUI**: `friendlyLaunchError` (`internal/ui/pages/launcher.go:73-87`) e/ou `handleHealthy`/`handleLaunchErr` SHALL incluir a causa real quando disponível — ex.: `"llama-server exited (code 1): unknown argument: --no-such-flag"` ao invés de `"did not become healthy within timeout"`. A última linha não-vazia do `StderrTail` é uma boa fonte para essa mensagem amigável.
13. **Reconcile com log**: `recover.go:27-66` SHALL logar uma entrada `reconcile_dropped` (warn) para cada instância removida por falha de `pidAliveAndNameMatches`, incluindo `pid`, `profile_id`, e o motivo (process gone / pid recycled).
14. **Boot logging**: A primeira linha do `model-loader.log` em cada sessão SHALL incluir versão do binário, commit hash (via `runtime/debug.ReadBuildInfo`), nível de log resolvido, paths efetivos, e horário de boot — como um cabeçalho de sessão.

## Non-Functional Requirements

- **Performance**: Sem requisito específico de throughput. O logger não vive em hot path. `slog.TextHandler` é mais que adequado.
- **Security**: Logs em `debug` PODEM conter paths absolutos e argumentos do `llama-server` (potencialmente revelam o nome do modelo). Documentar isso no AGENTS.md e no comentário do package `internal/log/`. Em `info` default, args efetivos NÃO são logados — apenas contagens (`arg_count=12`, `extra_args_count=3`).
- **UX / Accessibility**: Mensagens amigáveis no TUI continuam sendo a primeira linha de defesa. O log é a segunda camada. O caminho do arquivo é fixo e documentado no `--help` do binário.
- **Reliability**:
  - Falha ao abrir o arquivo de log no boot SHALL ser fatal apenas com nível `error` em stderr, mas o TUI deve continuar iniciando (logger cai para `io.Discard` se o arquivo não puder ser criado).
  - Logger é thread-safe (`slog.Logger` é safe-for-concurrent-use por contrato).
  - Rotação síncrona no boot é aceitável; falha de rename de arquivos antigos é warn, não fatal.
- **Compatibilidade Windows/macOS**: A captura de sinal via `syscall.WaitStatus` é Linux-only — mesmo padrão que `recover.go:25` já documenta como Linux-only. Em outros OS, `ExitSignal` fica vazio e `ExitCode` continua válido.

## Constraints & Assumptions

- **Runtime**: Go 1.26.2 — `log/slog` disponível na stdlib. Zero deps novas.
- **Plataforma**: Linux primária (consistente com `recover.go:25`); macOS/Windows aceitam degradação parcial (sem `ExitSignal`).
- **Persistência**: `instances.json` recebe campos novos (`ExitCode`, `ExitSignal`, `ExitReason`, `StderrTail`); leitura de registries antigos sem esses campos SHALL funcionar (campos `omitempty`).
- **Test data**: Existem golden tests em `testdata/` que dependem do formato do `RunningInstance` — esses precisam ser atualizados via `go test ./... -update`.
- **Assumption**: O usuário do TUI tem acesso de escrita em `~/.local/state/model-loader/logs/`. Se não tiver, logger cai para `io.Discard` silenciosamente (registrado uma vez em stderr no boot).
- **Assumption**: A invocação do binário ainda é primariamente via `model-loader` sem subcomandos — a flag `--log-level` é adicionada à invocação raiz. Se houver subcomandos no futuro, a flag fica global.

## Acceptance Criteria

- [ ] Após `make build && ./bin/model-loader` numa sessão limpa, o arquivo `~/.local/state/model-loader/logs/model-loader.log` é criado e contém uma linha de cabeçalho com versão, commit, nível e paths.
- [ ] Após uma tentativa de launch com perfil válido, `grep attempt_id=` no log mostra ao menos 5 eventos correlacionados pelo mesmo `attempt_id` (resolve, validate, spawn, wait, healthcheck).
- [ ] `MODEL_LOADER_LOG_LEVEL=debug ./bin/model-loader` produz linhas com `level=DEBUG` no arquivo; sem a env, nenhuma linha `level=DEBUG` aparece.
- [ ] `./bin/model-loader --log-level=debug` produz o mesmo efeito da env; a flag vence a env quando ambas estão presentes.
- [ ] Forçar uma flag inválida num perfil (ex.: `--no-such-flag`) e fazer launch: o TUI mostra uma mensagem com a última linha não-vazia do stderr do llama-server (ex.: `"llama-server exited (code 1): unknown argument: --no-such-flag"`), em vez de `"did not become healthy within timeout"`.
- [ ] Após o crash do item anterior, `cat ~/.local/state/model-loader/instances.json` mostra a entrada do PID com `exitCode`, `exitSignal` (em Linux), `exitReason` e `stderrTail` com até 50 linhas.
- [ ] Rodar o binário 6 vezes consecutivas: `~/.local/state/model-loader/logs/` contém exatamente 1 `model-loader.log` ativo + 5 `model-loader.<timestamp>.log` rotacionados; o mais antigo da execução 1 foi removido.
- [ ] `grep -rn "fmt.Fprintf(os.Stderr" cmd/ internal/` retorna apenas as ocorrências pré-`tea.Program.Run()` que documentamos como dual-sink (stderr + arquivo) ou nada — nenhum `Fprintf` órfão remanescente em camadas de serviço.
- [ ] Durante uma sessão TUI ativa, forçar um erro de spawn (binário inexistente): nenhum byte aparece em stderr enquanto o TUI roda; o erro está no arquivo de log e no status bar do TUI.
- [ ] `go test ./...` passa após as mudanças (incluindo golden tests atualizados de `RunningInstance`).
- [ ] `grep -rn "_ = cmd.Wait()" internal/service/processmgr/` retorna zero ocorrências.

## Recommended Approach

Novo pacote `internal/log/` expondo `New(cfg Config) (*slog.Logger, func(), error)` que monta um `slog.NewTextHandler` sobre um `io.Writer` apontando para `model-loader.log` (após rotação por sessão). Logger é injetado em `cmd/model-loader/main.go` e propagado para `backendcatalog.Resolver`, `validator.Validator`, `processmgr.Manager` (via `processmgr.Config{Logger}`) e `LauncherPage` (via `NewLauncherPage`). As goroutinas de `cmd.Wait` em `manager.go:149`/`:340` ganham um corpo real que inspeciona `*exec.ExitError`, lê tail do `LogPath`, popula `RunningInstance` (com novos campos `ExitCode`/`ExitSignal`/`ExitReason`/`StderrTail`) e emite evento `process_exited`. `friendlyLaunchError` consulta o último `RunningInstance` quando a falha é healthcheck-timeout pós-spawn para enriquecer a mensagem. Flag CLI e env são parseadas em `main.go` antes de qualquer outra inicialização.

## Decisions

### Sistema de log próprio do model-loader, separado do log per-instância
**Question**: Escopo do novo logger: instrumentar o código Go do model-loader (TUI + services), mantendo o log per-instância do llama-server como está?
**Recommended**: Logger separado; per-instância continua.
**Chosen**: Logger separado; per-instância continua.
**Rationale**: `internal/service/processmgr/manager.go:111-124` já captura stdout/stderr crus do llama-server por PID. Misturar formatos no mesmo arquivo quebraria `TailLogs` (`manager.go:230`).

### Sink: arquivo único rotacionado em `cfg.Paths.LogDir`
**Question**: Sink do logger estruturado: onde os eventos são escritos?
**Recommended**: Arquivo fixo `model-loader.log` rotacionado.
**Chosen**: Arquivo fixo `model-loader.log` rotacionado por sessão.
**Rationale**: evidence: `internal/config/config.go:116` + bubbletea proíbe escrita em stdout/stderr durante `Run()`. Caminho previsível facilita bug reports.

### Capturar causa de morte do llama-server
**Question**: Capturar exit code/causa quando llama-server morre (corrigir `_ = cmd.Wait()` em `manager.go:149` e `:340`)?
**Recommended**: Sim — persistir exit + tail e mostrar no TUI.
**Chosen**: Sim — persistir exit code, sinal, reason e `StderrTail` (50 linhas) no `RunningInstance`.
**Rationale**: evidence: `manager.go:149` e `:340` descartam `error`; `friendlyLaunchError` (`launcher.go:73-87`) só conhece 4 sentinels — usuário hoje vê "check logs" sem saber qual log. Esta é a correção de maior leverage para o intent declarado.

### Biblioteca: `log/slog` da stdlib
**Question**: Biblioteca do logger — qual contrato adotar pra emitir eventos?
**Recommended**: stdlib `log/slog`.
**Chosen**: stdlib `log/slog`.
**Rationale**: evidence: `go.mod` declara Go 1.26.2 e nenhum logger; zero deps novas. Logger não está em hot path — perf de slog é suficiente. Testabilidade trivial com `slog.NewTextHandler` sobre `bytes.Buffer`.

### Verbosidade: `info` default + env `MODEL_LOADER_LOG_LEVEL` + flag `--log-level`
**Question**: Nível default em produção e como o usuário aumenta verbosidade pra debugar?
**Recommended**: `info` default + env e CLI flag.
**Chosen**: `info` default; env `MODEL_LOADER_LOG_LEVEL` + flag `--log-level`; precedência CLI > env > config > default.
**Rationale**: Permite o caso "liga debug uma vez pra reportar bug" sem editar `config.toml`. Padrão 12factor que o time conhece.

### Escopo da instrumentação: caminho de load + lifecycle
**Question**: Onde instrumentar logs nesta primeira leva?
**Recommended**: Caminho de load + lifecycle de instância.
**Chosen**: Caminho de load + lifecycle (resolve, validate, spawn, wait, liveness, reconcile, healthcheck) + os `Fprintf` existentes em `main.go` e `config.go`.
**Rationale**: Casa exatamente com o intent declarado ("debugar load com diversos backends"). Camadas fora do caminho (monitor, profilestore, modelscanner, ui.pages.profiles) ficam para FRDs futuras.

### Formato do handler: texto humano-legível
**Question**: Formato do output do handler slog escrito no arquivo.
**Recommended**: Text humano-legível `key=value`.
**Chosen**: `slog.NewTextHandler` em formato `key=value`.
**Rationale**: Caso de uso é "anexar no bug report" e "ler com tail -f". JSON seria over-engineering sem agregador.

### Tail no TUI: fora de escopo
**Question**: Tail no TUI: adicionar um painel de eventos recentes no Tab Monitor?
**Recommended**: Não nesta FRD; só arquivo.
**Chosen**: Não nesta FRD.
**Rationale**: Painel duplicaria escopo; mantém PR focado. Usuário lê via `tail -f` em segundo terminal.

### `Fprintf(os.Stderr,...)` existentes: migrar para o logger
**Question**: `fmt.Fprintf(os.Stderr,...)` em `cmd/model-loader/main.go` (12) e `internal/config/config.go:82` — o que fazer?
**Recommended**: Migrar tudo pro novo logger.
**Chosen**: Migrar; erros pré-`tea.Program.Run()` escrevem dual-sink (stderr + arquivo) para o usuário ver imediatamente; erros pós-`Run()` ativo só vão para o arquivo.
**Rationale**: Esses pontos são exatamente erros de boot/config/recovery que precisam estar no bug report. Coberta coerente com o objetivo "anexar log no report".

### Rotação por sessão, sem `lumberjack`
**Question**: Rotação do arquivo: como `model-loader.log` não cresce sem limite?
**Recommended**: Roll por sessão + cap de N arquivos.
**Chosen**: Roll por sessão (renomeia `model-loader.log` existente com timestamp), cap em 5 arquivos.
**Rationale**: Sem dep nova (consistente com a escolha de slog stdlib). Fronteira clara entre sessões facilita correlacionar um bug report a um arquivo.

### Tail de stderr: últimas 50 linhas em `StderrTail []string`
**Question**: Tail de stderr salvo no `RunningInstance` quando llama-server crasha — quantas linhas finais capturar?
**Recommended**: Últimas 50 linhas.
**Chosen**: Últimas 50 linhas, persistidas em `RunningInstance.StderrTail` como `[]string`.
**Rationale**: 50 linhas geralmente cobrem o erro fatal do llama-server (stack/last-error) sem inflar `instances.json`.

### Correlação por `profile_id` + `attempt_id`
**Question**: Correlação de eventos: anexar campos contextuais automáticos em todo evento de uma tentativa de launch?
**Recommended**: `profile_id` + `attempt_id` em todo evento.
**Chosen**: `attempt_id` (curto, gerado por `launchProfileCmd`) + `profile_id` via `logger.With(...)`, propagado até a goroutine de `Wait`.
**Rationale**: `grep attempt_id=abc123` resgata a sessão inteira de uma falha. Eventos pré-spawn (resolve, validate) precisam de algum identificador comum com pós-spawn.

### Segurança do TUI: logger só escreve em arquivo enquanto bubbletea roda
**Question**: Quando o TUI estiver ativo, escrever em stderr pode quebrar o bubbletea — como blindar?
**Recommended**: Logger só escreve em arquivo, nunca em stderr (durante `Run()`).
**Chosen**: Handler do logger aponta apenas para o arquivo; bracket pré-`Run()` é o único momento que escreve em stderr (dual-sink).
**Rationale**: bubbletea controla o framebuffer do terminal; qualquer byte em stderr corrompe o render. Garantia mais forte com menos estado mutável.

## Open Questions

- Nenhuma — o desenvolvedor não deferiu nenhuma decisão.

## Suggested Follow-ups

- Painel de tail ao vivo no Tab Monitor (`internal/ui/pages/monitor.go`) — explicitamente fora de escopo desta FRD.
- Instrumentação de `internal/service/profilestore/`, `internal/service/modelscanner/`, `internal/service/monitor/` — camadas fora do caminho de load; podem entrar numa FRD posterior.
- Race latente em `saveRegistry` chamado fora do lock em quatro pontos (`internal/service/processmgr/manager.go:156,226,362` e `liveness.go:57`) — descoberta pelo probe do codebase-analyzer, não relacionada a logging mas merece registro.
- Sub-race em `Reconcile` vs. concurrent `Launch`/`Kill` documentada em `internal/service/processmgr/recover.go:22-24`.
- Rotação por tamanho com compressão (lumberjack) — se algum dia o arquivo único por sessão se mostrar pequeno demais para sessões longas.
- Mascaramento de PII em `debug` (paths absolutos, model names) — relevante se model-loader virar produto multi-tenant.
- `friendlyLaunchError` não testa `llamabin.ErrBinaryNotFound` (`internal/service/llamabin/resolver.go:17`) — usuário hoje vê erro embrulhado em vez de mensagem amigável.
- `TailLogs` (`internal/service/processmgr/manager.go:230`) faz snapshot estático em vez de `tail -f` — pode ser melhorado em conjunto com o painel do Tab Monitor no futuro.

## References

- `internal/service/processmgr/manager.go:73-156` — spawn background com captura para arquivo + `_ = cmd.Wait()` descartado.
- `internal/service/processmgr/manager.go:309-346` — spawn foreground sem captura.
- `internal/service/processmgr/liveness.go:35-61` — detecção de crash por poll de 5 s.
- `internal/service/processmgr/recover.go:27-66` — `Reconcile` que dropa entradas silenciosamente.
- `internal/ui/pages/launcher.go:382-414` — pipeline `launchProfileCmd` (resolve → validate → mgr.Launch).
- `internal/ui/pages/launcher.go:73-87` — `friendlyLaunchError` com 4 sentinels.
- `internal/domain/instance.go:6-15` — `RunningInstance` struct atual.
- `internal/config/config.go:113-118` — defaults dos paths.
- `cmd/model-loader/main.go:29-127` — pontos de `Fprintf(os.Stderr,...)` a migrar.

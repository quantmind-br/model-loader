# PRD — Unified Profiles Tab

**Source**: prompt-unify-launcher-profiles-tabs.md
**Generated**: 2026-05-18

## Implementation Order
1. F1 — Unificar Launcher e Profiles em uma única tab `Profiles` na posição `[1]`, renumerando as demais tabs para `[2]`–`[4]`.

---

## F1: Unify Launcher into Profiles Tab

### Scope

**In scope**:
- Remover `Launcher` como tab independente; renumerar a barra de tabs para 4 posições: `Profiles[1]`, `Server[2]`, `Models[3]`, `Backends[4]`.
- Absorver em `ProfilesPage` todas as capacidades hoje pertencentes a `LauncherPage`:
  - Launch do perfil selecionado via `[enter]` (com spinner, health check `/health`, transição automática para `Server` em sucesso).
  - Toggle background/foreground via `[b]` (default background).
  - Kill da instância em execução mais recente via `[k]` (com confirm modal).
  - Refresh da lista de perfis via `[r]`.
  - Renderização da seção `Running` (PID, porta, bg/fg) abaixo da lista de perfis, alimentada por `ProcessManagerMsg`.
- Adicionar `[E]` (maiúsculo) na `ProfilesPage` para abrir o editor completo (assume o papel que `[enter]` desempenha hoje).
- Remover `[L]` da `ProfilesPage` (redundante; `[enter]` lança).
- Deletar `internal/ui/pages/launcher.go` e `internal/ui/pages/launcher_test.go` após migrar conteúdo e asserções relevantes.
- Atualizar todas as strings user-facing e comentários que referenciam `Launcher`, `[1] Launcher`, `[2] Profiles` ou `[5]`.
- Atualizar testes para refletir nova ordem, nova numeração e novos bindings.

**Out of scope**:
- Reordenar tabs além de mover `Profiles` para `[1]` e o decorrente shift de `Server`/`Models`/`Backends`.
- Mudanças em `Models`, `Backends`, `Server` além de atualizações de texto/numeração e roteamento de mensagens cross-tab.
- Alterações no editor de perfis (sub-tabs internas, formulário huh, picker de modelos, validação) além das estritamente necessárias para que `[E]` o abra.
- Mudanças em domain/service layer (`processmgr`, `httpproxy`, `proxysupervisor`, `profilestore`, `validator`).
- Mudanças em capacidades de instâncias (bg/fg semantics, health-check path, port allocation) além de portá-las.

### Technical Approach

1. **Enumeração de tabs** (`internal/ui/root.go`):
   - Reescrever o `Tab` enum para a nova ordem.
   - Reduzir `RootModel.pages` de `[5]page` para `[4]page` e ajustar inicializações.
   - Reescrever o dispatcher de teclas numéricas para `"1"→TabProfiles`, `"2"→TabServer`, `"3"→TabModels`, `"4"→TabBackends`. Pressionar `"5"` deixa de ter alvo (no-op silencioso) e não causa crash.

2. **Migração de capacidades para `ProfilesPage`** (`internal/ui/pages/profiles.go`):
   - Adicionar campos: `running []RunningInstance`, `launchSpinner spinner.Model`, `launchStatus string`, `launchWaitState int`, `bgMode bool`, `killConfirm Modal`.
   - Adicionar handlers para `ProcessManagerMsg`, `launchedMsg`, `healthyMsg`, `launchErrMsg`, `spinnerTickMsg`, `ProfilesKillConfirmedMsg`.
   - Reescrever `handleKey` da `ProfilesPage` para o novo mapa de teclas (ver `Contracts`).
   - `IsCapturingInput()` recebe o estado novo `killConfirm.Active()` adicionado ao OR existente (`editing || pickerActive || confirmDelete || conflictModal.Active() || undoModal.Active() || importPicker.Active() || killConfirm.Active()`).
   - `View()` renderiza, em ordem: hint bar topo → lista de perfis → seção `Running` (mesma renderização atual do `Launcher.renderRunningList`) → status bar com `launchStatus` quando em launch.

3. **Migração de tipos auxiliares**:
   - Renomear `LauncherProfilesLoadedMsg` → `ProfilesLoadedMsg` (ou inline se já existe equivalente).
   - Renomear `LauncherKillConfirmedMsg` → `ProfilesKillConfirmedMsg`.
   - Mover `launchedMsg`, `healthyMsg`, `launchErrMsg`, `spinnerTickMsg`, `RunningInstance` para `profiles.go`.

4. **Roteamento cross-tab** (`internal/ui/root.go`):
   - Renomear `handleNavigateToLauncher` → `handleNavigateToProfilesAndLaunch` (ou semelhante); o handler troca para `TabProfiles` e encaminha `LaunchProfileMsg` para `ProfilesPage`.
   - `NavigateToSizingMsg` continua roteando para `TabProfiles` (sem mudança semântica).

5. **Strings hard-coded e comentários**:
   - `internal/ui/pages/server.go:738`: `"Switch to Launcher [1] to start one"` → `"Switch to Profiles [1] to start one"`.
   - `internal/ui/pages/launcher.go:518`: deletado junto com o arquivo.
   - `internal/ui/components/help.go:31-40`: remover seção `## Launcher tab`; reescrever seção `## Profiles tab` com lista completa de bindings (ver `Contracts`); atualizar cabeçalho com nova ordem de tabs.
   - `internal/ui/pages/models.go:199,924,1224-1225`: substituir referências a `Profiles [2]` por `Profiles [1]` e remover menções a `Launcher`.

6. **Limpeza**:
   - Deletar `internal/ui/pages/launcher.go`.
   - Deletar `internal/ui/pages/launcher_test.go` após migrar asserções de cobertura para `profiles_test.go`.

7. **Tests**:
   - `internal/ui/root_test.go`: atualizar testes do capture-gate e ordem de tabs (`capturingPage` em posição `TabProfiles=0`); assert que `[1]`–`[4]` ativam as tabs corretas; assert que `[5]` é no-op.
   - `internal/ui/pages/server_test.go:791-792`: atualizar string esperada para `"Profiles [1]"`.
   - `internal/ui/pages/profiles_test.go`: adicionar testes para `[enter]` launch, `[E]` edit, `[k]` kill confirm, `[b]` bg/fg toggle, `[r]` refresh, renderização da `Running` list, e roteamento de `LaunchProfileMsg` recebido do `Models`.
   - Golden tests existentes em `testdata/`: revalidar; atualizar via `go test ./... -update` se houver mudança de output esperada.

### Touchpoints

- `internal/ui/root.go` — enumeração `Tab`, `RootModel.pages`, dispatcher de teclas numéricas, handlers de mensagens cross-tab.
- `internal/ui/pages/profiles.go` — absorve launch/kill/bg-fg/refresh/running-list; reescreve handleKey; estende `IsCapturingInput()`.
- `internal/ui/pages/profiles_list.go` — atualiza `profilesKeyMap` (adiciona `[E]`, `[b]`, `[k]`, `[r]`; remove `[L]`; remove `[enter]` para edit).
- `internal/ui/pages/launcher.go` — **DELETAR** após migração.
- `internal/ui/pages/launcher_test.go` — **DELETAR** após migração das asserções relevantes.
- `internal/ui/pages/profiles_test.go` — adiciona cobertura para todos os atalhos migrados.
- `internal/ui/pages/server.go` — atualiza empty state da linha 738.
- `internal/ui/pages/server_test.go` — atualiza asserção das linhas 791-792.
- `internal/ui/pages/models.go` — atualiza comentários/strings nas linhas 199, 924, 1224-1225.
- `internal/ui/components/help.go` — remove seção Launcher, reescreve seção Profiles, atualiza ordem de tabs.
- `internal/ui/root_test.go` — atualiza testes de tabs e capture-gate.

### Contracts

```go
// internal/ui/root.go
type Tab int

const (
    TabProfiles Tab = iota
    TabServer
    TabModels
    TabBackends
)

type RootModel struct {
    pages     [4]page
    activeTab Tab
    // ... demais campos preservados
}

// Dispatcher de teclas numéricas (RootModel.Update):
// "1" → m.activate(TabProfiles)
// "2" → m.activate(TabServer)
// "3" → m.activate(TabModels)
// "4" → m.activate(TabBackends)
// "5" → no-op
//
// Pré-condição em todas: !m.activePageCapturesInput() (gate inalterado).
```

```go
// internal/ui/pages/profiles.go
type ProfilesPage struct {
    // --- estado preservado ---
    list           list.Model
    editor         profile_editor.Editor
    deleteConfirm  Modal
    conflictModal  Modal
    undoModal      Modal
    picker         PickerOverlay
    importPicker   FilePicker
    // --- estado migrado de Launcher ---
    running         []RunningInstance
    launchSpinner   spinner.Model
    launchStatus    string
    launchWaitState int   // idle | launching | waitingHealth
    bgMode          bool  // default true
    killConfirm     Modal
}

func (p *ProfilesPage) IsCapturingInput() bool {
    return p.editing ||
        p.pickerActive ||
        p.confirmDelete ||
        p.conflictModal.Active() ||
        p.undoModal.Active() ||
        p.importPicker.Active() ||
        p.killConfirm.Active()
}

// Key bindings na ProfilesPage unificada:
// [enter]   launchSelected()       // semântica antes do Launcher
// [E]       startEditSelected()    // semântica antes do enter de Profiles
// [b]       toggleBgMode()
// [k]       askKillMostRecent()    // abre killConfirm
// [r]       refreshProfiles()
// [n]       startNew()
// [d]       duplicateSelected()
// [x]       askDeleteSelected()
// [e]       exportProfiles()
// [p]       togglePinSelected()
// [I]       importProfiles()
// [u]       openUndoModal()
// [ctrl+p]  (interno ao editor: model picker)
// [/]       (built-in list filter — preserva enter para confirmar filtro)
```

```go
// Renomeações de mensagens (públicas para o pacote ui):
type ProfilesLoadedMsg = LauncherProfilesLoadedMsg // ou inline em profiles.go
type ProfilesKillConfirmedMsg = LauncherKillConfirmedMsg
// Demais (launchedMsg, healthyMsg, launchErrMsg, spinnerTickMsg, RunningInstance)
// movem-se inline para profiles.go (mantém visibilidade interna do pacote).
```

```go
// internal/ui/components/help.go — esqueleto da seção atualizada
// ## Profiles tab
// [enter] launch · [E] edit · [n] new · [d] duplicate · [x] delete
// [b] bg/fg · [k] kill · [r] refresh · [p] pin
// [e] export · [I] import · [u] undo
//
// Ordem das tabs no cabeçalho: Profiles[1] · Server[2] · Models[3] · Backends[4]
// (remover totalmente "## Launcher tab")
```

### Acceptance Criteria

- [ ] `internal/ui/root.go` define exatamente 4 valores em `Tab` na ordem `TabProfiles, TabServer, TabModels, TabBackends`.
- [ ] `RootModel.pages` tem tamanho 4; nenhuma referência a índice 5 permanece no pacote `ui`.
- [ ] Pressionar `[1]` ativa `Profiles`; `[2]` ativa `Server`; `[3]` ativa `Models`; `[4]` ativa `Backends`.
- [ ] Pressionar `[5]` não muda de tab e não causa crash/panic.
- [ ] Na tab `Profiles`, com um perfil selecionado e nenhuma superfície de captura ativa, `[enter]` dispara o launch: emite `LaunchProfileMsg`, exibe spinner com `launchStatus`, aguarda health check em `/health`, emite `SwitchToServerMsg` em sucesso.
- [ ] Na tab `Profiles`, `[E]` (maiúsculo) abre o editor completo via `editor.Open(d)` — mesmo fluxo hoje disparado por `[enter]` em `Profiles`.
- [ ] Na tab `Profiles`, `[b]` alterna `bgMode` e o estado é refletido visivelmente no hint bar.
- [ ] Na tab `Profiles`, `[k]` mata a instância em execução mais recente via fluxo `killConfirm`; em confirmação positiva, a instância sai da lista `running` e exibe-se flash `"killed pid=X"`.
- [ ] Na tab `Profiles`, `[r]` recarrega a lista de perfis sem destruir o estado de `running` nem o spinner em curso.
- [ ] A seção `Running` (PID, porta, flag bg/fg) é renderizada na `ProfilesPage` abaixo da lista de perfis, com formato idêntico ao atual `Launcher.renderRunningList`, e atualiza via `ProcessManagerMsg`.
- [ ] Todas as ações pré-existentes da `Profiles` (`[n]`, `[d]`, `[x]`, `[e]` export, `[p]`, `[I]`, `[u]`, `[ctrl+p]`) continuam funcionando com semântica idêntica.
- [ ] `[L]` na tab `Profiles` não dispara launch (binding removido).
- [ ] Arquivo `internal/ui/pages/launcher.go` não existe mais no repositório após a entrega.
- [ ] Arquivo `internal/ui/pages/launcher_test.go` não existe mais no repositório após a entrega.
- [ ] Empty state da `ServerPage` exibe `"Switch to Profiles [1] to start one"`.
- [ ] `internal/ui/components/help.go` não contém a string `"## Launcher tab"` nem `"Launcher"` como nome de tab; lista as 4 tabs em ordem `Profiles, Server, Models, Backends`.
- [ ] Busca textual em todo o repositório por `"[5]"` no contexto de tabs retorna zero ocorrências user-facing.
- [ ] Busca textual em todo o repositório por `"Launcher [1]"` ou `"Profiles [2]"` retorna zero ocorrências.
- [ ] `LaunchProfileMsg` emitida por `Models` é roteada pelo `root.go` para a `ProfilesPage` (não tenta acessar página `Launcher` inexistente).
- [ ] Filtro de lista via `/` continua funcional; pressionar `[enter]` durante filtro aceita o filtro e não dispara launch acidental.
- [ ] `IsCapturingInput()` da `ProfilesPage` retorna `true` em todos os estados de captura (editor, picker, deleteConfirm, conflictModal, undoModal, importPicker, killConfirm); testes do capture-gate em `internal/ui/root_test.go` permanecem verdes.
- [ ] `make tests` passa sem regressões. Golden tests em `testdata/` permanecem válidos ou são explicitamente atualizados via `go test ./... -update` com a mudança documentada no PR.
- [ ] `make build` produz binário sem warnings/errors.

### Dependencies

- None.

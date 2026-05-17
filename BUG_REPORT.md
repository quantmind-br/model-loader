# Relatório de Bugs — TUI model-loader

**Data:** 2026-04-29
**Binário testado:** `bin/model-loader` (build atual de `main` em `d6e6ff7`)
**Método:** sessão `tmux` 200x50, envio de teclas via `tmux send-keys`, captura via `tmux capture-pane`. Profile semeado manualmente em `~/.config/model-loader/profiles/test-profile.json` para liberar o fluxo Launcher/Monitor.
**Documentação de referência:** `internal/ui/components/help.go` (`HelpMarkdown`).

---

## Resumo executivo

12 bugs encontrados. **4 críticos/altos** bloqueiam fluxos centrais (criar profile, escolher modelo, ciclar sub-views do Monitor). Causa raiz dominante: `root.go` intercepta `tab` / `shift+tab` **antes** de despachar para a página ativa, quebrando formulários `huh` e o cycler de sub-views do Monitor.

---

## CRÍTICO

### B1 — `Tab` / `Shift+Tab` globais engolem teclas dentro de formulários e sub-views
**Onde:** `internal/ui/root.go:206-218` (handler global `tea.KeyMsg` antes de delegar a `m.pages[m.active]`).
**Como reproduzir:**
1. Tab Profiles → `n` (novo profile) → `Tab` para tentar avançar de `Name` p/ `Description`.
2. Resultado: TUI pula para tab Launcher; o draft fica preso em `editing=true` mas inacessível.
**Impacto:**
- **Profiles editor (huh)**: usuário não consegue ir além do primeiro campo. Cria-se um draft ininterativo.
- **Models action menu (huh.Select)**: Enter sobre a opção não conclui o form (verificado: pop-up permanece aberto após `Enter`). `huh` espera `tab` para concluir grupo.
- **Monitor sub-view cycle**: `monitor.go:182-183` (`case m.Type == tea.KeyTab: p.subView = (p.subView + 1) % 3`) nunca é alcançado — Tab é absorvido pelo root.
**Sintoma observado em captura:**
```
Editor — [Essentials]   ctrl+t to switch  ctrl+p to pick model
┃ Name
┃ > New ProfileaX     ← typing ainda funciona, mas Tab/Enter não avançam
```
após `Tab`: TUI exibe Launcher, draft preservado mas oculto.
**Fix sugerido:** em `root.go`, só processar `tab`/`shift+tab` quando a página ativa não estiver em modo modal/editor (expor flag `IsCapturingInput()` no `Page` interface) ou trocar atalho global para `ctrl+]` / `ctrl+[`.

### B2 — `Tab` continua roubando foco mesmo com modal de help aberto? Não — mas relação com B1
Help-modal trata `?` / `esc` / `q`. OK. Sem bug aqui — apenas confirmação de que swallow é genérico.

---

## ALTO

### B3 — `L` em Profiles documentado mas não implementado
**Onde:** `internal/ui/components/help.go` (HelpMarkdown linha *“L — launch directly from selected profile”*) **vs** `internal/ui/pages/profiles.go:59-73` (`profilesKeyMap` não inclui `L`; `updateList` não trata `L`).
**Reprodução:** selecionar profile → `L` → nada acontece.
**Fix:** adicionar `Launch: key.NewBinding(key.WithKeys("L"))` + handler que envia `LauncherProfilesLoadedMsg`/equivalente para `LauncherPage` e troca tab.

### B4 — Models action menu sem opção “Use in existing profile”
**Onde:** `internal/ui/pages/models.go:328-335`.
```go
huh.NewOption("Use in new profile", "new"),
huh.NewOption("Reveal path", "reveal"),
```
**Help promete:** *“actions: use in new profile / existing profile / reveal path”*.
**Fix:** adicionar terceira opção que abra picker de profiles existentes e injete o path no campo `Model`.

### B5 — `huh` form de ações no Models não submete
**Onde:** `internal/ui/pages/models.go:191-205` (loop de update do form).
**Reprodução:** `4` (Models) → `Enter` na linha → submenu abre → `Enter` na opção → submenu permanece aberto; `actionFormDoneMsg` nunca é despachado.
**Hipótese:** ligado a B1 — `huh.NewForm` precisa de `Tab` para fechar grupo único; com Tab roubado pelo root, `State` nunca vira `huh.StateCompleted`.
**Fix:** após resolver B1, validar; alternativa, marcar form como `WithGroupKey(...)` ou disparar `actionFormDoneMsg` na seleção via `Action.OnSelect`.

---

## MÉDIO

### B6 — Status-bar global não exibe `[?] help`
**Onde:** `internal/ui/root.go:74`:
```go
status: components.StatusBar{Hints: "[1-6] tabs  [tab] next  [q] quit"},
```
Commit recente *“feat(ui): consistent footer hints across all pages, including [?] help”* não atualizou o root. As páginas mostram `[?] help` no rodapé interno, mas o `StatusBar` global nunca menciona `?`.
**Fix:** mudar hint para `[1-6] tabs  [tab] next  [?] help  [q] quit`.

### B7 — Hint do detail-pane em Profiles omite `[L]`
**Onde:** `internal/ui/pages/profiles.go:276`.
Atual: `[enter] edit  [n] new  [d] dup  [x] del  [?] help`. Help.go promete `L`. Consequência direta de B3.

### B8 — Duplicate de profile mantém o mesmo `port`
**Onde:** `internal/service/profilestore` `Duplicate` (não revisado em código aqui, mas comportamento empírico).
**Reprodução:** profile com `port=18999` → `d` → cópia também `port=18999`. Tab Launcher mostra ambos com `port 18999`. Lançar os dois = colisão de bind.
**Fix:** em `Duplicate`, varrer profiles existentes e bumpar para próximo port livre (ou pedir confirmação ao usuário).

### B9 — Path de scan inválido polui header sem ação na UI
**Onde:** `internal/ui/pages/models.go:264-290` (`renderStatus`).
**Sintoma:** header da tab Models exibe `…/home/diogo/models [error: lstat /home/diogo/models: no such file or directory]` — string longa, sem possibilidade de remover/editar via TUI; só editando `config.toml` à mão.
**Fix:** truncar mensagem; expor atalho/dialog para gerenciar `models.search_paths`.

---

## BAIXO

### B10 — Filter line do Models mostra `filter: ""` enquanto filtra
**Onde:** `internal/ui/pages/models.go:341-345` (anexa rune ao `p.filter`) + render em `View`.
**Sintoma intermitente:** envio de teclas em sequência via `tmux send-keys -l` resulta em rows filtrados mas `filter: ""` exibido; envio rune-a-rune funciona. Possível race entre buffered key-events e `refreshRows`.
**Reproduzir difícil de teclado humano**; likely não-bloqueante.

### B11 — `ProfilesPage` não recarrega ao trocar de tab
**Onde:** `internal/ui/pages/profiles.go:131-140` (`Init` chama `loadCmd`; sem reload em `WindowSizeMsg`/foco).
**Sintoma:** profile criado externamente (FS) só aparece após restart do binário.
**Fix opcional:** adicionar `tea.Cmd` para `loadCmd` em transição `tab → Profiles`, ou `fsnotify` watcher.

### B12 — Editor preserva draft após `Tab` global furtivo
**Onde:** consequência de B1.
**Sintoma:** ao voltar à Profiles depois de Tab acidental, editor reabre com draft anterior; `Esc` necessário para limpar.
**Fix:** invalidate editor (`p.editing=false`, `p.form=nil`) ao receber qualquer mensagem de troca de tab via root (broadcast).

---

## DOC / TEXTO

### B13 — Monitor footer afirma `[Tab] cycle view` enquanto Tab é global
**Onde:** `internal/ui/pages/monitor.go:317`.
Mensagem confunde — Tab nunca cicla a sub-view (B1).
**Fix:** corrigir após B1, ou trocar binding documentado para algo livre (e.g. `v`).

---

## Matriz de severidade

| ID  | Severidade | Componente               | Bloqueia fluxo? |
|-----|------------|--------------------------|-----------------|
| B1  | Crítico    | root.go                  | Sim — editor + monitor + action menu |
| B3  | Alto       | profiles.go / help.go    | Não, mas viola contrato |
| B4  | Alto       | models.go                | Sim — feature ausente |
| B5  | Alto       | models.go (huh form)     | Sim — “Use in new profile” não conclui |
| B6  | Médio      | root.go status bar       | Não |
| B7  | Médio      | profiles.go detail view  | Não |
| B8  | Médio      | profilestore Duplicate   | Pode causar conflito de port |
| B9  | Médio      | models.go                | Não |
| B10 | Baixo      | models.go filter         | Não |
| B11 | Baixo      | profiles.go              | Não |
| B12 | Baixo      | profiles.go editor       | Não |
| B13 | Doc        | monitor.go footer        | Não |

---

## Notas de teste

- **Não testado:** lançar instância real (`Enter` em Launcher) — evitado para não deixar `llama-server` orfão durante a sessão. Estrutura do código foi conferida em `internal/service/processmgr/` e `internal/ui/pages/launcher.go`.
- **Não testado:** sub-view `Slots` / `Metrics` do Monitor — bloqueado por B1.
- **Não testado:** picker `ctrl+p` no editor de profile — bloqueado por B1 (não foi possível avançar até campo `Model`).
- **Ambiente:** `nvidia-smi` disponível, `llama-server` em `/usr/bin/llama-server`, profile dir vazio antes do teste.

## Ordem sugerida de correção

1. **B1** primeiro — desbloqueia B5, valida B12 e B13 e melhora UX geral.
2. **B5** + **B4** — fechar fluxo “escolher modelo → criar profile”.
3. **B3** + **B7** — implementar atalho `L` ou removê-lo da help.
4. **B6** — atualizar status bar global.
5. **B8** — auto-bump de port no `Duplicate`.
6. Demais (B9–B11) conforme bandwidth.

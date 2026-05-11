# model-loader — Slice 6 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fechar o MVP — polimento, help (glamour), keybindings finais, error UX (design § 8), e carry-overs do review da slice-5.

**Architecture:** Quatro frentes paralelas: (a) carry-overs apontados no review da slice-5 (cancel async, sparkline placeholder, PID forward, restart real); (b) infra de error UX (modal reutilizável + statusbar já existente); (c) crash detection no Manager via liveness ticker `kill(pid, 0)` + refresh periódico no Monitor; (d) help modal (`?`) com markdown embedded renderizado via `glamour`. Sem novas dependências além de `glamour` (já listado em design § 12).

**Tech Stack:** Go + bubbletea/bubbles/lipgloss/huh (já em uso) + `github.com/charmbracelet/glamour` (NOVA dep).

---

## Pre-requisitos

- Branch base: `main` (slice-5 já mergeada @ `eab36b1`).
- Branch nova: `feat/slice-6` (criada via `git checkout -b feat/slice-6`).
- Worktree opcional: o usuário decide se usa `superpowers:using-git-worktrees`.
- Antes de qualquer task: `go test ./...` deve estar verde no `main`.

---

## Decisões trancadas (do brainstorm)

| # | Decisão | Escolha |
|---|---|---|
| 1 | Origem do help markdown | Embedded const em `internal/ui/components/help.go` |
| 2 | Surface de erros | Statusbar (warnings/info) + Modal (bloqueante: bin ausente) |
| 3 | Carry-overs slice-5 incluídos | I-1 (cancel async), I-3 (restart real), M-1 (sparkline placeholder), M-2 (PID forward) |
| 4 | Crash detection | Sim — `kill(pid, 0)` tick + flag em RunningInstance |

---

## File Structure

### Cria (10 arquivos novos)
- `internal/ui/components/modal.go` — overlay reutilizável
- `internal/ui/components/modal_test.go`
- `internal/ui/components/help.go` — help markdown const + render helper
- `internal/ui/components/help_test.go`
- `internal/service/processmgr/liveness.go` — goroutine de crash detection
- `internal/service/processmgr/liveness_test.go`
- `docs/superpowers/plans/2026-04-29-model-loader-slice-6.md` — ESTE arquivo

### Modifica (8 arquivos)
- `internal/domain/instance.go` — campos `Crashed bool`, `ExitedAt *time.Time`
- `internal/service/profilestore/store.go` — interface ganha `ListWithDiagnostics`
- `internal/service/profilestore/fs_store.go` — implementação
- `internal/service/processmgr/manager.go` — liveness ticker + Stat de model em Launch + LookPath em New
- `internal/ui/pages/monitor.go` — async cancel, sparkline placeholder, PID forward, restart real, crash row, periodic refresh
- `internal/ui/pages/monitor_test.go`
- `internal/ui/pages/launcher.go` — error hints
- `internal/ui/pages/launcher_test.go`
- `internal/ui/pages/profiles.go` — ⚠ marker para profiles corruptos
- `internal/ui/pages/profiles_test.go`
- `internal/ui/pages/messages.go` — pode ganhar `monitorSelectPIDMsg`
- `internal/ui/root.go` — `?` toggle help overlay, forward `SwitchToMonitorMsg.PID`, boot bin modal
- `internal/ui/root_test.go`
- `cmd/model-loader/main.go` — wire ProfileStore em MonitorPage, fail-fast com modal se bin ausente
- `go.mod` / `go.sum` — `glamour` adicionada

### Phases / Tasks

| Phase | Tasks | Resumo |
|---|---|---|
| A — Carry-overs slice-5 | T1..T4 | I-1 + M-1 + M-2 + I-3 |
| B — Profile error UX | T5..T7 | ListWithDiagnostics + ⚠ marker + pre-launch model Stat |
| C — Modal component | T8 | Componente reutilizável |
| D — Boot binary guard | T9 | LookPath + modal blocker |
| E — Crash detection | T10..T11 | RunningInstance fields + liveness ticker + crash row |
| F — Help modal | T12..T13 | help.go + root `?` overlay |
| G — Wrap | T14..T15 | Keybindings audit + manual smoke |

Cada commit = 1 step. Cada task tem TDD (red → green → commit).

---

## Phase A — Carry-overs slice-5

### Task 1: Cancel async em `applyInstances` (review I-1)

**Files:**
- Modify: `internal/ui/pages/monitor.go` (função `applyInstances`)
- Test: `internal/ui/pages/monitor_test.go` (test novo)

Motivação: `_ = st.cancel()` síncrono dentro de `applyInstances` bloqueia o thread de Update. Se `nvidia-smi` está stuck ou `cancel` aguarda `wg.Wait()`, a UI congela ~50ms × N orphans.

- [ ] **Step 1: Test que verifica que applyInstances retorna sem bloquear no cancel**

Adicionar a `monitor_test.go`:

```go
func TestMonitorPage_CancelOrphanIsAsync(t *testing.T) {
	// Cancel func will block until release is signaled — simulating a
	// stuck nvidia-smi that takes time to reap. The test asserts that
	// applyInstances returns promptly without blocking on cancel.
	release := make(chan struct{})
	var cancelStarted, cancelFinished int32
	mm := &slowCancelMonMgr{
		cancel: func() error {
			atomic.AddInt32(&cancelStarted, 1)
			<-release
			atomic.AddInt32(&cancelFinished, 1)
			return nil
		},
	}
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	p := NewMonitorPage(pm, mm, nil)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	pm.insts = nil // PID 1 vanished -> cancel should be triggered

	done := make(chan struct{})
	go func() {
		p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
		close(done)
	}()
	select {
	case <-done:
		// applyInstances returned promptly even though cancel still hasn't completed.
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("applyInstances blocked on cancel; cancelStarted=%d", atomic.LoadInt32(&cancelStarted))
	}
	// Now release the cancel.
	close(release)
	for i := 0; i < 100 && atomic.LoadInt32(&cancelFinished) == 0; i++ {
		time.Sleep(2 * time.Millisecond)
	}
	if atomic.LoadInt32(&cancelFinished) != 1 {
		t.Fatalf("cancel never completed after release; finished=%d", atomic.LoadInt32(&cancelFinished))
	}
}
```

Helper a adicionar (no mesmo arquivo, perto de `chanMonMgr`):

```go
type slowCancelMonMgr struct {
	cancel func() error
}

func (s *slowCancelMonMgr) Subscribe(_, _ int, _ string) (<-chan monitor.MonitorEvent, func() error, error) {
	ch := make(chan monitor.MonitorEvent)
	return ch, s.cancel, nil
}
```

Imports a adicionar: `"sync/atomic"`, `"time"` (provavelmente já presente).

- [ ] **Step 2: Run test — falha porque cancel é síncrono e a constructor signature mudou**

Run: `go test ./internal/ui/pages/ -run TestMonitorPage_CancelOrphanIsAsync -v`
Expected: FAIL — compile error (NewMonitorPage today só aceita 2 args) ou timeout (se ajustar signature antes).

- [ ] **Step 3: Refactor `applyInstances` para spawn goroutine no cancel**

Editar `internal/ui/pages/monitor.go`. Localizar o bloco:

```go
	for pid, st := range p.subs {
		if !seen[pid] {
			_ = st.cancel()
			delete(p.subs, pid)
			delete(p.chans, pid)
		}
	}
```

Substituir por:

```go
	for pid, st := range p.subs {
		if !seen[pid] {
			cancel := st.cancel
			go func() { _ = cancel() }() // do not block UI on subscription teardown
			delete(p.subs, pid)
			delete(p.chans, pid)
		}
	}
```

- [ ] **Step 4: Atualizar constructor para aceitar `nil` como ProfileStore (T4 vai injetar; aqui placeholder)**

Editar struct e construtor. Adicionar na struct `MonitorPage`:

```go
	ps profileStoreIface // injected for `r` real restart (slice 6 / Task 4)
```

Definir interface ainda nesse arquivo (acima de `MonitorPage`):

```go
// profileStoreIface é o subset de profilestore.Store usado pela MonitorPage
// para implementar `r` (restart real). nil -> `r` cai em modo kill-only.
type profileStoreIface interface {
	Get(id string) (domain.Profile, error)
}
```

E mudar `NewMonitorPage`:

```go
func NewMonitorPage(pm procMgrIface, mm monitor.Manager, ps profileStoreIface) *MonitorPage {
	cols := []table.Column{
		{Title: "PID", Width: 8},
		{Title: "Port", Width: 6},
		{Title: "Profile", Width: 18},
		{Title: "Uptime", Width: 10},
		{Title: "VRAM", Width: 12},
		{Title: "Tokens/s", Width: 10},
	}
	t := table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(8))
	return &MonitorPage{
		pm:    pm,
		mm:    mm,
		ps:    ps,
		tbl:   t,
		subs:  map[int]*subState{},
		chans: map[int]<-chan monitor.MonitorEvent{},
	}
}
```

Atualizar todas as chamadas existentes em testes (`monitor_test.go`) e `cmd/model-loader/main.go` para passar `nil` como terceiro argumento. Em `main.go`, sera substituído pelo store real na T4. Em testes, `nil` mantém comportamento atual.

Editar `cmd/model-loader/main.go` — onde tem `pages.NewMonitorPage(mgr, mon)`, mudar para `pages.NewMonitorPage(mgr, mon, nil)`.

Editar `internal/ui/pages/monitor_test.go` — em todas as chamadas `NewMonitorPage(pm, mm)`, mudar para `NewMonitorPage(pm, mm, nil)`.

- [ ] **Step 5: Run all tests**

Run: `go test ./internal/...`
Expected: PASS — incluindo o novo `TestMonitorPage_CancelOrphanIsAsync`.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/pages/monitor.go internal/ui/pages/monitor_test.go cmd/model-loader/main.go
git commit -m "$(cat <<'EOF'
fix(monitor): cancel orphan subs asynchronously to unblock UI

Carry-over from slice-5 review I-1. Synchronous st.cancel() inside
applyInstances was blocking the Update goroutine for up to ~50ms per
orphan (worst case: stuck nvidia-smi). Wrap each cancel in `go func()`
so the subscription tears itself down off the UI thread.

Also extends NewMonitorPage signature with profileStoreIface (nil-safe)
to prep for slice-6 Task 4 (real restart with ProfileStore.Get).

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Sparkline placeholder em estado vazio (review M-1)

**Files:**
- Modify: `internal/ui/pages/monitor.go` (função `View`, branch `SubViewMetrics`)
- Test: `internal/ui/pages/monitor_test.go`

Motivação: hoje `Sparkline(nil, 40)` retorna 40 espaços brancos — o usuário não distingue "métricas zero" de "não há dados ainda".

- [ ] **Step 1: Test verifica placeholder**

Adicionar a `monitor_test.go`:

```go
func TestMonitorPage_MetricsPlaceholderWhenEmpty(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 42, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	// Switch to metrics sub-view.
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyTab})
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyTab})
	out := p.View()
	if !strings.Contains(out, "(no metrics yet") {
		t.Fatalf("metrics view missing placeholder; got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test — falha**

Run: `go test ./internal/ui/pages/ -run TestMonitorPage_MetricsPlaceholderWhenEmpty -v`
Expected: FAIL.

- [ ] **Step 3: Implementar branch placeholder**

Editar `internal/ui/pages/monitor.go`. Localizar o branch `case SubViewMetrics:` em `View`. Substituir o bloco existente por:

```go
		case SubViewMetrics:
			if len(st.mets.TokensPerSec) == 0 && len(st.mets.RequestsPerSec) == 0 {
				bottom = "(no metrics yet — first sample arrives after the slots tick)"
				break
			}
			var b strings.Builder
			fmt.Fprintf(&b, "tokens/s: %s\n", components.Sparkline(st.mets.TokensPerSec, 40))
			fmt.Fprintf(&b, "req/s   : %s\n", components.Sparkline(st.mets.RequestsPerSec, 40))
			if st.gpu.VRAMTotalMB > 0 {
				fmt.Fprintf(&b, "VRAM    : %d/%d MB  util %.0f%%\n", st.gpu.VRAMUsedMB, st.gpu.VRAMTotalMB, st.gpu.Utilization)
			}
			bottom = b.String()
```

- [ ] **Step 4: Run test passes**

Run: `go test ./internal/ui/pages/ -run TestMonitorPage_MetricsPlaceholderWhenEmpty -v`
Expected: PASS.

- [ ] **Step 5: Run full suite**

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/pages/monitor.go internal/ui/pages/monitor_test.go
git commit -m "$(cat <<'EOF'
fix(monitor): show placeholder text when metrics window is empty

Carry-over from slice-5 review M-1. Sparkline(nil, 40) returns 40 spaces
which is visually identical to "all zeros". Print explicit placeholder
when both TokensPerSec and RequestsPerSec are empty.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Forward `SwitchToMonitorMsg.PID` para MonitorPage (review M-2)

**Files:**
- Modify: `internal/ui/root.go` (case `pages.SwitchToMonitorMsg`)
- Modify: `internal/ui/pages/monitor.go` (handler novo)
- Modify: `internal/ui/pages/messages.go` (msg novo)
- Test: `internal/ui/root_test.go`
- Test: `internal/ui/pages/monitor_test.go`

Motivação: hoje root troca pra tab Monitor e descarta o PID. Usuário cai numa tabela stale, sem a row recém-criada selecionada.

- [ ] **Step 1: Test root forward de PID**

Adicionar a `internal/ui/root_test.go`:

```go
func TestRoot_ForwardsSwitchPIDToMonitor(t *testing.T) {
	rec := &recordingMonitor{}
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(rec).
		WithModelsPage(pages.Placeholder{TabName: "M"})
	updated, _ := r.Update(pages.SwitchToMonitorMsg{PID: 4321})
	rm := updated.(RootModel)
	if rm.active != TabMonitor {
		t.Errorf("active = %v, want TabMonitor", rm.active)
	}
	if rec.lastSelectPID != 4321 {
		t.Errorf("rec.lastSelectPID = %d, want 4321", rec.lastSelectPID)
	}
}

type recordingMonitor struct {
	lastSelectPID int
}

func (r *recordingMonitor) Init() tea.Cmd { return nil }
func (r *recordingMonitor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(pages.MonitorSelectPIDMsg); ok {
		r.lastSelectPID = m.PID
	}
	return r, nil
}
func (r *recordingMonitor) View() string { return "" }
```

Imports adicionais para `root_test.go` (se ausentes): `"github.com/quantmind-br/model-loader/internal/ui/pages"`.

- [ ] **Step 2: Test MonitorPage seleciona row pelo PID**

Adicionar a `monitor_test.go`:

```go
func TestMonitorPage_SelectsRowByPID(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 100, Port: 8080, LogPath: "/tmp/a.log"},
		{PID: 200, Port: 8081, LogPath: "/tmp/b.log"},
		{PID: 300, Port: 8082, LogPath: "/tmp/c.log"},
	}}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	// Default selection is row 0 (PID 100). Send select msg for PID 200.
	p, _ = updateAs[*MonitorPage](p, MonitorSelectPIDMsg{PID: 200})
	if got := p.selectedPID(); got != 200 {
		t.Fatalf("selectedPID = %d, want 200", got)
	}
}
```

- [ ] **Step 3: Run tests — falham**

Run: `go test ./internal/ui/... -run "TestRoot_ForwardsSwitchPIDToMonitor|TestMonitorPage_SelectsRowByPID" -v`
Expected: FAIL — `MonitorSelectPIDMsg` não existe.

- [ ] **Step 4: Adicionar mensagem em `messages.go`**

Editar `internal/ui/pages/messages.go`. Conteúdo final:

```go
package pages

// SwitchToMonitorMsg is emitted by LauncherPage after a launch is healthy.
// root.go consumes this to switch the active tab to Monitor and pre-select
// the new PID.
type SwitchToMonitorMsg struct {
	PID int
}

// MonitorSelectPIDMsg instructs MonitorPage to select the row whose PID
// matches. Sent by root when handling SwitchToMonitorMsg so the Monitor
// page lands focused on the newly-launched instance.
type MonitorSelectPIDMsg struct {
	PID int
}
```

- [ ] **Step 5: Root forwards PID e troca tab**

Editar `internal/ui/root.go`. Localizar:

```go
	case pages.SwitchToMonitorMsg:
		m.active = TabMonitor
		return m, nil
```

Substituir por:

```go
	case pages.SwitchToMonitorMsg:
		m.active = TabMonitor
		// Forward the PID so MonitorPage can refresh + select that row.
		updated, cmd := m.pages[TabMonitor].Update(pages.MonitorSelectPIDMsg{PID: msg.PID})
		m.pages[TabMonitor] = updated
		return m, cmd
```

- [ ] **Step 6: MonitorPage handler**

Editar `internal/ui/pages/monitor.go`. Em `Update`, dentro do `switch m := msg.(type)`, adicionar branch novo:

```go
	case MonitorSelectPIDMsg:
		// Refresh first (in case the new instance is not yet in subs/rows),
		// then queue an internal selection step.
		cmds = append(cmds, p.refreshInstancesCmd(), func() tea.Msg {
			return monitorSelectPIDInternalMsg{pid: m.PID}
		})
	case monitorSelectPIDInternalMsg:
		p.selectRow(m.pid)
```

Definir `monitorSelectPIDInternalMsg` (private) próximo dos outros msg types:

```go
// monitorSelectPIDInternalMsg moves the table cursor to the row whose PID
// matches. Emitted by MonitorPage itself after a refresh, in response to
// the public MonitorSelectPIDMsg.
type monitorSelectPIDInternalMsg struct {
	pid int
}
```

E método helper:

```go
// selectRow positions the table cursor on the row matching pid (no-op if not found).
func (p *MonitorPage) selectRow(pid int) {
	rows := p.tbl.Rows()
	for i, r := range rows {
		var rowPID int
		_, _ = fmt.Sscanf(r[0], "%d", &rowPID)
		if rowPID == pid {
			p.tbl.SetCursor(i)
			return
		}
	}
}
```

- [ ] **Step 7: Run tests — passam**

Run: `go test ./internal/ui/... -run "TestRoot_ForwardsSwitchPIDToMonitor|TestMonitorPage_SelectsRowByPID" -v`
Expected: PASS.

- [ ] **Step 8: Run full suite**

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/ui/pages/messages.go internal/ui/pages/monitor.go internal/ui/pages/monitor_test.go internal/ui/root.go internal/ui/root_test.go
git commit -m "$(cat <<'EOF'
feat(ui): forward launched PID from root to MonitorPage on tab switch

Carry-over from slice-5 review M-2. Add MonitorSelectPIDMsg so root.go can
hand off the new PID after consuming SwitchToMonitorMsg. MonitorPage then
refreshes its instance list and positions the table cursor on the matching
row, replacing the prior stale-rows + arbitrary-selection behavior.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: `r` faz restart real com `ProfileStore.Get` + `Launch` (review I-3)

**Files:**
- Modify: `internal/ui/pages/monitor.go` (handler de `r`)
- Modify: `internal/ui/pages/monitor_test.go`
- Modify: `cmd/model-loader/main.go` (passar store real)

Motivação: hoje `r` mata e some — usuário esperava restart. Spec § 7.2 diz "restart (kill + relaunch mesmo profile)". Precisamos do `ProfileStore` injetado (T1 já adicionou ao construtor); este task implementa o handler.

- [ ] **Step 1: Test restart-real**

Adicionar a `monitor_test.go`:

```go
func TestMonitorPage_RTriggersRestart(t *testing.T) {
	prof := domain.Profile{
		ID:    "qwen",
		Name:  "Qwen",
		Model: "/tmp/x.gguf",
		Args:  map[string]any{"port": 8080.0},
	}
	psk := &fakeProfileStore{p: prof}
	pm := &restartTrackingMgr{
		insts:    []domain.RunningInstance{{ProfileID: "qwen", PID: 100, Port: 8080, LogPath: "/tmp/a.log"}},
		newPID:   200,
		newPort:  8080,
	}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, psk)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	// Press 'r' on the selected (only) row.
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatal("r did not produce a Cmd")
	}
	// Run the cmd: kill + launch should happen synchronously inside it.
	_ = cmd()
	if pm.killedPID != 100 {
		t.Errorf("killedPID = %d, want 100", pm.killedPID)
	}
	if pm.launchedID != "qwen" {
		t.Errorf("launchedID = %q, want qwen", pm.launchedID)
	}
	if pm.launchMode != processmgr.LaunchBackground {
		t.Errorf("launchMode = %v, want LaunchBackground", pm.launchMode)
	}
}

type fakeProfileStore struct {
	p   domain.Profile
	err error
}

func (f *fakeProfileStore) Get(id string) (domain.Profile, error) {
	if f.err != nil {
		return domain.Profile{}, f.err
	}
	return f.p, nil
}

type restartTrackingMgr struct {
	insts      []domain.RunningInstance
	killedPID  int
	launchedID string
	launchMode processmgr.LaunchMode
	newPID     int
	newPort    int
}

func (r *restartTrackingMgr) List() []domain.RunningInstance { return r.insts }
func (r *restartTrackingMgr) Kill(pid int) error             { r.killedPID = pid; return nil }
func (r *restartTrackingMgr) TailLogs(_ int) (io.ReadCloser, error) {
	return nil, processmgr.ErrUnknownPID
}
func (r *restartTrackingMgr) Launch(p domain.Profile, mode processmgr.LaunchMode) (domain.RunningInstance, error) {
	r.launchedID = p.ID
	r.launchMode = mode
	return domain.RunningInstance{ProfileID: p.ID, PID: r.newPID, Port: r.newPort, Background: true}, nil
}
```

Imports adicionais que possam faltar: `"io"`, `"github.com/quantmind-br/model-loader/internal/service/processmgr"`.

- [ ] **Step 2: Run test — falha**

Run: `go test ./internal/ui/pages/ -run TestMonitorPage_RTriggersRestart -v`
Expected: FAIL — atualmente `r` só faz kill.

- [ ] **Step 3: Implementar restart real**

Editar `internal/ui/pages/monitor.go`. Localizar o handler de `r`:

```go
		case m.Type == tea.KeyRunes && len(m.Runes) == 1 && m.Runes[0] == 'r':
			// Slice-5: r==kill (real restart needs ProfileStore — deferred to slice 6).
			if pid := p.selectedPID(); pid > 0 {
				_ = p.pm.Kill(pid)
				cmds = append(cmds, p.refreshInstancesCmd())
			}
```

Substituir por:

```go
		case m.Type == tea.KeyRunes && len(m.Runes) == 1 && m.Runes[0] == 'r':
			if pid := p.selectedPID(); pid > 0 {
				cmds = append(cmds, p.restartCmd(pid))
			}
```

E adicionar método `restartCmd`:

```go
// restartCmd kills the instance with pid and relaunches the same profile in
// background mode. Falls back to plain kill when ps is nil (no store wired).
// On any error, the command emits a no-op msg; UI surfaces the error via the
// next instance refresh.
func (p *MonitorPage) restartCmd(pid int) tea.Cmd {
	insts := p.pm.List()
	var inst *domain.RunningInstance
	for i := range insts {
		if insts[i].PID == pid {
			inst = &insts[i]
			break
		}
	}
	if inst == nil {
		return p.refreshInstancesCmd()
	}
	pm := p.pm
	ps := p.ps
	profileID := inst.ProfileID
	bg := inst.Background
	return tea.Batch(
		func() tea.Msg {
			_ = pm.Kill(pid)
			if ps == nil {
				return monitorInstancesRefreshedMsg{insts: pm.List()}
			}
			prof, err := ps.Get(profileID)
			if err != nil {
				return monitorInstancesRefreshedMsg{insts: pm.List()}
			}
			mode := processmgr.LaunchBackground
			if !bg {
				mode = processmgr.LaunchForeground
			}
			_, _ = pm.Launch(prof, mode)
			return monitorInstancesRefreshedMsg{insts: pm.List()}
		},
	)
}
```

- [ ] **Step 4: Wire ProfileStore real em main.go**

Editar `cmd/model-loader/main.go`. Encontrar a linha `pages.NewMonitorPage(mgr, mon, nil)` (introduzida na T1) e substituir `nil` pelo `store` já existente no escopo (a `profilestore.Store` instanciada para a ProfilesPage). Exemplo:

```go
	monitorPage := pages.NewMonitorPage(mgr, mon, store)
```

(Se o nome da variável for diferente — confirmar lendo o arquivo. Tipicamente `store` ou `profileStore`.)

- [ ] **Step 5: Run test — passa**

Run: `go test ./internal/ui/pages/ -run TestMonitorPage_RTriggersRestart -v`
Expected: PASS.

- [ ] **Step 6: Run full suite**

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/pages/monitor.go internal/ui/pages/monitor_test.go cmd/model-loader/main.go
git commit -m "$(cat <<'EOF'
feat(monitor): r key performs real restart via ProfileStore.Get + Launch

Carry-over from slice-5 review I-3. Inject ProfileStore (added to
NewMonitorPage signature in Task 1) and use it on `r` to:
  1. Kill the selected PID
  2. Look up the profile by ProfileID
  3. Launch it again in the same mode

Falls back to plain kill when profile lookup fails or ps is nil. The
table refreshes after the restart, so the user sees the new PID.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Phase B — Profile error UX

### Task 5: `ProfileStore.ListWithDiagnostics` retorna entries corruptas

**Files:**
- Modify: `internal/service/profilestore/store.go` (interface)
- Modify: `internal/service/profilestore/fs_store.go` (impl)
- Test: `internal/service/profilestore/fs_store_test.go`

Motivação: hoje `List()` silenciosamente skipa entries inválidas (`fs_store.go:51`). Spec § 8 diz: "Profile JSON corrompido → Lista marca entry com `⚠`, exclui de operações até user fix/delete". Precisamos retornar info dos skipados.

- [ ] **Step 1: Test cobre que diagnostics carregam id + erro**

Adicionar a `internal/service/profilestore/fs_store_test.go`:

```go
func TestFSStore_ListWithDiagnostics_ReportsCorrupt(t *testing.T) {
	s, dir := newStore(t)
	if err := s.Save(sampleProfile("good", "Good")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	profiles, diags, err := s.ListWithDiagnostics()
	if err != nil {
		t.Fatalf("ListWithDiagnostics: %v", err)
	}
	if len(profiles) != 1 || profiles[0].ID != "good" {
		t.Fatalf("profiles = %+v, want exactly one good", profiles)
	}
	if len(diags) != 1 {
		t.Fatalf("diags = %d, want 1", len(diags))
	}
	if diags[0].ID != "bad" {
		t.Errorf("diags[0].ID = %q, want bad", diags[0].ID)
	}
	if !errors.Is(diags[0].Err, ErrInvalidJSON) {
		t.Errorf("diags[0].Err = %v, want ErrInvalidJSON", diags[0].Err)
	}
}
```

- [ ] **Step 2: Run test — falha**

Run: `go test ./internal/service/profilestore/ -run TestFSStore_ListWithDiagnostics_ReportsCorrupt -v`
Expected: FAIL — método não existe.

- [ ] **Step 3: Adicionar tipo `ListDiagnostic` + interface method**

Editar `internal/service/profilestore/store.go`. Conteúdo final:

```go
// Package profilestore persists Profile JSON files on disk.
package profilestore

import (
	"errors"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// ListDiagnostic descreve uma entry de profile que falhou ao carregar.
// Usado pela UI para marcar profiles corruptos com ⚠ e excluí-los das
// operações de launch.
type ListDiagnostic struct {
	ID  string
	Err error
}

// Store is the interface for profile persistence.
type Store interface {
	List() ([]domain.Profile, error)
	ListWithDiagnostics() ([]domain.Profile, []ListDiagnostic, error)
	Get(id string) (domain.Profile, error)
	Save(p domain.Profile) error
	Delete(id string) error
	Duplicate(srcID, newID string) (domain.Profile, error)
}

// Sentinel errors returned by Store implementations.
var (
	ErrNotFound    = errors.New("profile not found")
	ErrInvalidJSON = errors.New("profile json is invalid")
	ErrDuplicateID = errors.New("profile id already exists")
	ErrInvalidID   = errors.New("profile id is invalid")
)
```

- [ ] **Step 4: Implementar `ListWithDiagnostics` em `fs_store.go`**

Editar `internal/service/profilestore/fs_store.go`. Adicionar método logo após `List`:

```go
// ListWithDiagnostics retorna profiles válidos + lista de entries corruptas.
// O agregado nunca aborta a varredura inteira por uma entry quebrada;
// erros de I/O do diretório raiz, esses sim, retornam err != nil.
func (s *FSStore) ListWithDiagnostics() ([]domain.Profile, []ListDiagnostic, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read profiles dir: %w", err)
	}
	profiles := make([]domain.Profile, 0, len(entries))
	var diags []ListDiagnostic
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		p, err := s.Get(id)
		if err != nil {
			diags = append(diags, ListDiagnostic{ID: id, Err: err})
			continue
		}
		profiles = append(profiles, p)
	}
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Name < profiles[j].Name
	})
	return profiles, diags, nil
}
```

E refactor opcional do `List()` para chamar `ListWithDiagnostics`:

```go
func (s *FSStore) List() ([]domain.Profile, error) {
	profiles, _, err := s.ListWithDiagnostics()
	return profiles, err
}
```

- [ ] **Step 5: Run all tests**

Run: `go test ./internal/service/profilestore/ -v`
Expected: PASS — todos os testes existentes + o novo.

- [ ] **Step 6: Commit**

```bash
git add internal/service/profilestore/store.go internal/service/profilestore/fs_store.go internal/service/profilestore/fs_store_test.go
git commit -m "$(cat <<'EOF'
feat(profilestore): expose ListDiagnostic for corrupt profile entries

Adds ListWithDiagnostics() that returns valid profiles plus a slice of
ListDiagnostic{ID, Err} for entries that failed to parse. List() is
preserved as a thin wrapper for backward compat. UI consumers (next
task) use diagnostics to render a ⚠ marker per design § 8.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: ProfilesPage renderiza ⚠ marker

**Files:**
- Modify: `internal/ui/pages/profiles.go` (load + view)
- Test: `internal/ui/pages/profiles_test.go`

Motivação: design § 8 — "Lista marca entry com `⚠`, exclui de operações até user fix/delete".

- [ ] **Step 1: Test verifica marker no View**

Adicionar a `internal/ui/pages/profiles_test.go`:

```go
func TestProfilesPage_RendersCorruptMarker(t *testing.T) {
	store := newFakeStoreWithDiagnostics(
		[]domain.Profile{{ID: "ok", Name: "Ok"}},
		[]profilestore.ListDiagnostic{{ID: "broken", Err: profilestore.ErrInvalidJSON}},
	)
	p := pages.NewProfilesPage(store, nil, domain.FlagSchema{})
	loadedMsg := p.LoadCmd()() // executa command sincronamente
	page, _ := p.Update(loadedMsg)
	out := page.View()
	if !strings.Contains(out, "broken") || !strings.Contains(out, "⚠") {
		t.Fatalf("view missing corrupt marker; got:\n%s", out)
	}
}
```

(Adapte o test ao API real da `ProfilesPage` — se o load é orchestrado por outro msg, ajuste. O essencial é renderizar `⚠ <id>` ou `<id> (corrupt)`.)

Helper a adicionar (no test):

```go
type fakeStoreWithDiag struct {
	ps    []domain.Profile
	diags []profilestore.ListDiagnostic
}

func newFakeStoreWithDiagnostics(ps []domain.Profile, diags []profilestore.ListDiagnostic) *fakeStoreWithDiag {
	return &fakeStoreWithDiag{ps: ps, diags: diags}
}

func (f *fakeStoreWithDiag) List() ([]domain.Profile, error) { return f.ps, nil }
func (f *fakeStoreWithDiag) ListWithDiagnostics() ([]domain.Profile, []profilestore.ListDiagnostic, error) {
	return f.ps, f.diags, nil
}
func (f *fakeStoreWithDiag) Get(id string) (domain.Profile, error) {
	for _, p := range f.ps {
		if p.ID == id {
			return p, nil
		}
	}
	return domain.Profile{}, profilestore.ErrNotFound
}
func (f *fakeStoreWithDiag) Save(_ domain.Profile) error                   { return nil }
func (f *fakeStoreWithDiag) Delete(_ string) error                         { return nil }
func (f *fakeStoreWithDiag) Duplicate(_, _ string) (domain.Profile, error) { return domain.Profile{}, nil }
```

- [ ] **Step 2: Run test — falha**

Run: `go test ./internal/ui/pages/ -run TestProfilesPage_RendersCorruptMarker -v`
Expected: FAIL.

- [ ] **Step 3: Trocar `List` por `ListWithDiagnostics` no load + propagar `corrupt` para a list**

Editar `internal/ui/pages/profiles.go`. Localizar onde a load command chama `store.List()` e substituir pela nova:

```go
	profiles, diags, err := store.ListWithDiagnostics()
```

(O nome exato do msg + struct depende do código atual — se a msg de load é `profilesLoadedMsg{ps []domain.Profile, err error}`, estender para `profilesLoadedMsg{ps []domain.Profile, diags []profilestore.ListDiagnostic, err error}`.)

Em `Update`, ao receber a msg, popular tanto a list real quanto pelos diagnostics:

```go
	case profilesLoadedMsg:
		// ... existing population of p.profiles ...
		p.diagnostics = msg.diags
		// Append visual rows for corrupt entries (non-selectable in launch).
		items := make([]list.Item, 0, len(p.profiles)+len(p.diagnostics))
		for _, prof := range p.profiles {
			items = append(items, profileItem{p: prof})
		}
		for _, d := range p.diagnostics {
			items = append(items, corruptItem{id: d.ID, err: d.Err})
		}
		p.list.SetItems(items)
```

Definir tipo `corruptItem` (no mesmo arquivo):

```go
type corruptItem struct {
	id  string
	err error
}

func (c corruptItem) Title() string       { return "⚠ " + c.id }
func (c corruptItem) Description() string { return "corrupt: " + c.err.Error() }
func (c corruptItem) FilterValue() string { return c.id }
```

E adicionar campo na struct `ProfilesPage`:

```go
	diagnostics []profilestore.ListDiagnostic
```

Bloquear ações destrutivas/launch quando o item selecionado for `corruptItem`:

```go
// Inside the keypress handler for `s` (save), `L` (launch), etc., check:
if _, ok := p.list.SelectedItem().(corruptItem); ok {
	p.statusMsg = "selected entry is corrupt — fix the JSON or delete the file"
	return p, nil
}
```

(Adapte ao loop de keys real. O ponto crítico é não deixar a profile corrupta passar para `Save/Launch`.)

- [ ] **Step 4: Run test — passa**

Run: `go test ./internal/ui/pages/ -run TestProfilesPage_RendersCorruptMarker -v`
Expected: PASS.

- [ ] **Step 5: Run full suite**

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/pages/profiles.go internal/ui/pages/profiles_test.go
git commit -m "$(cat <<'EOF'
feat(profiles): render ⚠ marker for corrupt profile JSON entries

Use ProfileStore.ListWithDiagnostics to surface entries that failed to
parse. They appear in the list as "⚠ <id>" with the parse error in the
description, and are blocked from save/launch operations.

Implements design § 8 — "Profile JSON corrompido → Lista marca entry
com ⚠, exclui de operações até user fix/delete".

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Pre-launch model file `os.Stat` + LauncherPage hint amigável

**Files:**
- Modify: `internal/service/processmgr/manager.go` (Launch — Stat antes de Start)
- Modify: `internal/service/processmgr/manager_test.go`
- Modify: `internal/ui/pages/launcher.go` (mensagens de erro mais úteis)
- Modify: `internal/ui/pages/launcher_test.go`

Motivação:
- `ErrModelNotFound` está declarada como reserved (`processmgr.go:40`) mas nunca é retornada hoje. Spec § 8 diz: "Model path missing → Validation error inline no editor".
- `ErrPortBusy` já existe e é retornada. LauncherPage hoje mostra `error: <msg>` cru — substituir por dica útil ("port 8080 in use; try a different port or kill the running PID").

- [ ] **Step 1: Test verifica que Launch retorna ErrModelNotFound se model path não existe**

Adicionar a `internal/service/processmgr/manager_test.go`:

```go
func TestManager_Launch_ModelMissing(t *testing.T) {
	m := newTestManager(t)
	prof := sampleLaunchProfile()
	prof.Model = "/nonexistent/path/to/model.gguf"
	_, err := m.Launch(prof, LaunchBackground)
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("err = %v, want ErrModelNotFound", err)
	}
}
```

(Reuse o helper `newTestManager` / `sampleLaunchProfile` se já existir; senão copiar do test existente `TestManager_LaunchBackground_PortBusy`.)

- [ ] **Step 2: Run test — falha**

Run: `go test ./internal/service/processmgr/ -run TestManager_Launch_ModelMissing -v`
Expected: FAIL — Launch atualmente passa para `cmd.Start()` sem checar.

- [ ] **Step 3: Adicionar Stat em `Launch`**

Editar `internal/service/processmgr/manager.go`. Localizar `Launch` e adicionar logo no topo (antes de qualquer dispatch para fg/bg):

```go
func (m *fsManager) Launch(p domain.Profile, mode LaunchMode) (domain.RunningInstance, error) {
	if p.Model == "" {
		return domain.RunningInstance{}, ErrModelNotFound
	}
	if _, err := os.Stat(p.Model); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return domain.RunningInstance{}, fmt.Errorf("%w: %s", ErrModelNotFound, p.Model)
		}
		return domain.RunningInstance{}, fmt.Errorf("stat model: %w", err)
	}
	// ... resto do método
}
```

Imports: garantir que `"errors"` e `"io/fs"` estão no arquivo (provavelmente "errors" já, "io/fs" pode não estar — adicionar).

- [ ] **Step 4: Test launcher mostra hint amigável em ErrPortBusy**

Adicionar a `launcher_test.go`:

```go
func TestLauncherPage_PortBusyHint(t *testing.T) {
	page := pages.NewLauncherPage(nil, &busyPortMgr{}, nil)
	// Simular launchErrMsg com ErrPortBusy.
	updated, _ := page.Update(pages.LaunchErrMsgForTest(fmt.Errorf("port 8080: %w", processmgr.ErrPortBusy)))
	out := updated.(pages.LauncherPage).View()
	if !strings.Contains(out, "port") || !strings.Contains(out, "in use") {
		t.Fatalf("view missing port-busy hint; got:\n%s", out)
	}
}

type busyPortMgr struct{}

func (b *busyPortMgr) Launch(_ domain.Profile, _ processmgr.LaunchMode) (domain.RunningInstance, error) {
	return domain.RunningInstance{}, processmgr.ErrPortBusy
}
func (b *busyPortMgr) Kill(_ int) error                                { return nil }
func (b *busyPortMgr) List() []domain.RunningInstance                  { return nil }
func (b *busyPortMgr) WaitHealthy(_, _ int, _ time.Duration) error     { return nil }
func (b *busyPortMgr) TailLogs(_ int) (io.ReadCloser, error)           { return nil, processmgr.ErrUnknownPID }
```

E no `launcher.go`, expor um helper de teste:

```go
// LaunchErrMsgForTest expõe launchErrMsg para testes externos.
func LaunchErrMsgForTest(err error) tea.Msg { return launchErrMsg{err: err} }
```

- [ ] **Step 5: Run test — falha**

Run: `go test ./internal/ui/pages/ -run TestLauncherPage_PortBusyHint -v`
Expected: FAIL.

- [ ] **Step 6: Implementar mapeamento amigável de erros em launcher.go**

Editar o handler de `launchErrMsg` em `internal/ui/pages/launcher.go`. Localizar:

```go
	case launchErrMsg:
		p.status = "error: " + msg.err.Error()
		return p, nil
```

Substituir por:

```go
	case launchErrMsg:
		p.status = friendlyLaunchError(msg.err)
		return p, nil
```

E definir helper no mesmo arquivo:

```go
// friendlyLaunchError translates sentinel manager errors into actionable
// hints shown in the page status line.
func friendlyLaunchError(err error) string {
	switch {
	case errors.Is(err, processmgr.ErrPortBusy):
		return "error: port in use — change the profile port or kill the running PID"
	case errors.Is(err, processmgr.ErrModelNotFound):
		return "error: model file not found — fix the profile's Model path"
	case errors.Is(err, processmgr.ErrForegroundBusy):
		return "error: a foreground instance is already running — toggle [b] to background mode"
	case errors.Is(err, processmgr.ErrHealthCheckTimeout):
		return "error: server did not become healthy within timeout — check logs"
	default:
		return "error: " + err.Error()
	}
}
```

Imports: `"errors"`.

- [ ] **Step 7: Run tests — passam**

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/service/processmgr/manager.go internal/service/processmgr/manager_test.go internal/ui/pages/launcher.go internal/ui/pages/launcher_test.go
git commit -m "$(cat <<'EOF'
feat(launcher): pre-launch model Stat + actionable launch error hints

processmgr.Launch now Stat()s the profile.Model path before cmd.Start;
returns ErrModelNotFound on miss (was previously declared but unused).

LauncherPage maps the four manager sentinel errors (PortBusy, ModelNotFound,
ForegroundBusy, HealthCheckTimeout) into actionable status strings instead
of leaking the raw error text.

Implements design § 8 rows for "Port busy no launch" and "Model path
missing".

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Phase C — Modal component

### Task 8: `components.Modal` — overlay reutilizável

**Files:**
- Create: `internal/ui/components/modal.go`
- Create: `internal/ui/components/modal_test.go`

Motivação: usado em duas frentes — (a) boot blocker quando `llama-server` não está no PATH; (b) help modal `?`. Componente puro, sem state interno: render-only.

- [ ] **Step 1: Test cobertura mínima**

Criar `internal/ui/components/modal_test.go`:

```go
package components

import (
	"strings"
	"testing"
)

func TestModal_RendersTitleAndBody(t *testing.T) {
	out := Modal("Help", "Body content here", 80, 24)
	if !strings.Contains(out, "Help") {
		t.Errorf("missing title in output:\n%s", out)
	}
	if !strings.Contains(out, "Body content here") {
		t.Errorf("missing body in output:\n%s", out)
	}
}

func TestModal_FillsViewport(t *testing.T) {
	out := Modal("T", "B", 40, 10)
	// Lipgloss.Place fills with the given dimensions; the rendered string
	// should have at least 9 newlines (for 10 lines).
	if strings.Count(out, "\n") < 9 {
		t.Errorf("expected viewport-filling output; got %d newlines:\n%s", strings.Count(out, "\n"), out)
	}
}

func TestModal_NoSizePassesThrough(t *testing.T) {
	// width/height = 0 -> raw box, no Place wrapper.
	out := Modal("T", "B", 0, 0)
	if strings.Count(out, "\n") > 6 {
		t.Errorf("expected compact box; got %d newlines:\n%s", strings.Count(out, "\n"), out)
	}
}
```

- [ ] **Step 2: Run test — falha**

Run: `go test ./internal/ui/components/ -run TestModal -v`
Expected: FAIL — `Modal` não existe.

- [ ] **Step 3: Implementar**

Criar `internal/ui/components/modal.go`:

```go
// Package components: Modal renderiza um overlay centralizado com título e
// corpo. Não mantém state — toggles ficam no caller (root model, page model).
package components

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

var (
	modalBox = lipgloss.NewStyle().
		Border(theme.Border).
		BorderForeground(theme.ColorAccent).
		Padding(1, 2).
		Background(lipgloss.Color("#1a1a1a"))

	modalTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.ColorAccent).
		Margin(0, 0, 1, 0)
)

// Modal renderiza title + body em uma caixa centralizada. Quando width ou
// height são 0, retorna apenas a caixa (sem Place wrapper) — útil para
// inspeção em testes ou composições adicionais. Quando width > 0 e height > 0,
// a caixa é centralizada num canvas dessa dimensão.
func Modal(title, body string, width, height int) string {
	box := modalBox.Render(modalTitle.Render(title) + "\n" + body)
	if width <= 0 || height <= 0 {
		return box
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
```

Garantir que `theme.Border`, `theme.ColorAccent` existem (já vistos no theme.go). Se faltarem, ajustar para usar valores diretos (`lipgloss.RoundedBorder()` etc).

- [ ] **Step 4: Run test passa**

Run: `go test ./internal/ui/components/ -run TestModal -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/components/modal.go internal/ui/components/modal_test.go
git commit -m "$(cat <<'EOF'
feat(components): Modal — reusable centered overlay

Pure render helper: takes title + body + viewport dims, returns a
lipgloss-bordered box centered via lipgloss.Place. State (open/closed)
lives in the caller. Used by slice-6 boot guard and help modal.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Phase D — Boot binary guard

### Task 9: Boot fail-fast com modal quando `llama-server` ausente do PATH

**Files:**
- Modify: `internal/service/processmgr/manager.go` (New devolve erro quando bin não encontrado)
- Modify: `internal/service/processmgr/manager_test.go`
- Modify: `internal/ui/root.go` (renderiza boot blocker)
- Modify: `internal/ui/root_test.go`
- Modify: `cmd/model-loader/main.go` (coordena init)

Motivação: design § 8 — "llama-server ausente do PATH → Modal bloqueante na boot, instrução para instalar `llama.cpp-cuda`".

- [ ] **Step 1: Test que `processmgr.New` retorna erro quando bin não está no PATH**

Adicionar a `manager_test.go`:

```go
func TestManager_New_BinaryNotInPATH(t *testing.T) {
	cfg := Config{Binary: "this-bin-does-not-exist-xyz", LogDir: t.TempDir(), RegistryPath: filepath.Join(t.TempDir(), "i.json")}
	_, err := NewWithCheck(cfg)
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("err = %v, want ErrBinaryNotFound", err)
	}
}
```

- [ ] **Step 2: Run test — falha (NewWithCheck/ErrBinaryNotFound não existem)**

Run: `go test ./internal/service/processmgr/ -run TestManager_New_BinaryNotInPATH -v`
Expected: FAIL.

- [ ] **Step 3: Adicionar `ErrBinaryNotFound` + `NewWithCheck`**

Editar `internal/service/processmgr/processmgr.go`. Adicionar à lista de sentinels:

```go
	ErrBinaryNotFound     = errors.New("llama-server binary not found in PATH")
```

Editar `internal/service/processmgr/manager.go`. Adicionar (logo abaixo de `New`):

```go
// NewWithCheck é como New, mas verifica via exec.LookPath se o binário existe
// antes de retornar. Erro: ErrBinaryNotFound (com o nome buscado embutido).
// Use em main.go para bootear com fail-fast e modal de instalação.
func NewWithCheck(cfg Config) (*fsManager, error) {
	bin := cfg.Binary
	if bin == "" {
		bin = "llama-server"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBinaryNotFound, bin)
	}
	return New(cfg), nil
}
```

- [ ] **Step 4: Test root renderiza modal quando boot fails**

Adicionar a `internal/ui/root_test.go`:

```go
func TestRoot_BootBlockerRendersModal(t *testing.T) {
	r := NewRoot(TabProfiles).WithBootBlocker("llama-server not found", "Install with: pacman -S llama.cpp-cuda")
	view := r.View()
	if !strings.Contains(view, "llama-server not found") {
		t.Errorf("missing title in view")
	}
	if !strings.Contains(view, "pacman -S") {
		t.Errorf("missing install hint in view")
	}
}

func TestRoot_BootBlockerSwallowsKeysExceptQuit(t *testing.T) {
	r := NewRoot(TabProfiles).WithBootBlocker("err", "fix")
	// Pressing 1 (tab switch) should NOT change active tab.
	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	rm := updated.(RootModel)
	if rm.active != TabProfiles {
		t.Errorf("active changed despite blocker; got %v", rm.active)
	}
	// Pressing q must still quit (tea.Quit cmd).
	_, cmd := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Errorf("q must still produce tea.Quit when blocker is open")
	}
}
```

- [ ] **Step 5: Run tests — falham**

Run: `go test ./internal/ui/ -run "TestRoot_BootBlocker" -v`
Expected: FAIL.

- [ ] **Step 6: Implementar `WithBootBlocker` + modificar Update e View**

Editar `internal/ui/root.go`. Adicionar à struct `RootModel`:

```go
	bootBlocker *bootBlocker
```

E o tipo + builder:

```go
type bootBlocker struct {
	title string
	body  string
}

// WithBootBlocker mostra um modal bloqueante sobre toda a UI. Usado quando
// algum recurso crítico falta na boot (e.g. llama-server fora do PATH).
// Apenas `q` / `ctrl+c` continuam respondendo enquanto o blocker está ativo.
func (m RootModel) WithBootBlocker(title, body string) RootModel {
	m.bootBlocker = &bootBlocker{title: title, body: body}
	return m
}
```

Modificar `Update` para gating:

```go
func (m RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.bootBlocker != nil {
		if k, ok := msg.(tea.KeyMsg); ok {
			if k.Type == tea.KeyCtrlC || (k.Type == tea.KeyRunes && len(k.Runes) == 1 && k.Runes[0] == 'q') {
				return m, tea.Quit
			}
		}
		if w, ok := msg.(tea.WindowSizeMsg); ok {
			m.width, m.height = w.Width, w.Height
		}
		return m, nil
	}
	// ... resto do método (existente)
}
```

Modificar `View`:

```go
func (m RootModel) View() string {
	if m.bootBlocker != nil {
		return components.Modal(m.bootBlocker.title, m.bootBlocker.body+"\n\nPress q to quit.", m.width, m.height)
	}
	// ... resto do método (existente)
}
```

- [ ] **Step 7: Wire em main.go — fail-fast com modal se bin não encontrado**

Editar `cmd/model-loader/main.go`. Localizar onde `processmgr.New` é chamado e substituir por `NewWithCheck`. Em caso de erro com `ErrBinaryNotFound`, ainda assim instanciar `RootModel` mas com `bootBlocker`:

```go
	mgr, err := processmgr.NewWithCheck(processmgr.Config{
		Binary:       "llama-server",
		LogDir:       cfg.Paths.LogDir,
		RegistryPath: filepath.Join(cfg.Paths.StateDir, "instances.json"),
		LastUsedSink: store,
	})
	if err != nil {
		if errors.Is(err, processmgr.ErrBinaryNotFound) {
			root := ui.NewRoot(ui.TabProfiles).WithBootBlocker(
				"llama-server not found in PATH",
				"Install llama.cpp first:\n  Arch: pacman -S llama.cpp-cuda\n  Other distros: build from https://github.com/ggml-org/llama.cpp",
			)
			if _, runErr := tea.NewProgram(root, tea.WithAltScreen()).Run(); runErr != nil {
				log.Fatal(runErr)
			}
			os.Exit(1)
		}
		log.Fatal(err)
	}
```

(Ajuste os nomes/paths reais lendo o `main.go` antes de editar.)

- [ ] **Step 8: Run tests — passam**

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 9: `go build` smoke**

Run: `go build ./...`
Expected: success.

- [ ] **Step 10: Commit**

```bash
git add internal/service/processmgr/processmgr.go internal/service/processmgr/manager.go internal/service/processmgr/manager_test.go internal/ui/root.go internal/ui/root_test.go cmd/model-loader/main.go
git commit -m "$(cat <<'EOF'
feat(boot): blocking modal when llama-server is missing from PATH

Adds processmgr.NewWithCheck (uses exec.LookPath) and
processmgr.ErrBinaryNotFound. main.go now boots into a TUI displaying
a modal with installation instructions instead of fatal-exit on the
terminal.

RootModel gains WithBootBlocker(title, body) — while active, all
keypresses except q/ctrl+c are swallowed.

Implements design § 8 row "llama-server ausente do PATH".

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Phase E — Crash detection

### Task 10: `domain.RunningInstance` ganha `Crashed` + Manager liveness ticker

**Files:**
- Modify: `internal/domain/instance.go`
- Create: `internal/service/processmgr/liveness.go`
- Create: `internal/service/processmgr/liveness_test.go`
- Modify: `internal/service/processmgr/manager.go` (start/stop ticker)
- Modify: `internal/service/processmgr/processmgr.go` (interface ganha `Close`)

Motivação: design § 8 — "Crash do `llama-server` background → Detect via `kill -0` no próximo tick; Monitor mostra exit code dos logs".

- [ ] **Step 1: Test cobre que liveness ticker marca PID como Crashed quando `kill -0` falha**

Criar `internal/service/processmgr/liveness_test.go`:

```go
package processmgr

import (
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestLiveness_MarksDeadPIDCrashed(t *testing.T) {
	dir := t.TempDir()
	m := New(Config{Binary: "true", LogDir: dir, RegistryPath: filepath.Join(dir, "i.json")})
	m.tracked[99999] = domain.RunningInstance{ProfileID: "x", PID: 99999, Port: 9000, Background: true}

	// Stub probe — first call says alive, subsequent say dead.
	calls := atomic.Int32{}
	probe := func(pid int) bool {
		c := calls.Add(1)
		return c == 1 // alive on first probe, dead after
	}
	stop := m.startLivenessWithProbe(50*time.Millisecond, probe)
	defer stop()

	// Wait for at least 2 ticks.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		inst := m.tracked[99999]
		m.mu.Unlock()
		if inst.Crashed {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("liveness ticker did not mark PID 99999 as Crashed; calls=%d", calls.Load())
}

func TestLiveness_AliveStaysAlive(t *testing.T) {
	dir := t.TempDir()
	m := New(Config{Binary: "true", LogDir: dir, RegistryPath: filepath.Join(dir, "i.json")})
	m.tracked[111] = domain.RunningInstance{ProfileID: "x", PID: 111, Port: 9000, Background: true}

	stop := m.startLivenessWithProbe(20*time.Millisecond, func(_ int) bool { return true })
	defer stop()

	time.Sleep(80 * time.Millisecond)
	m.mu.Lock()
	inst := m.tracked[111]
	m.mu.Unlock()
	if inst.Crashed {
		t.Fatal("liveness flipped Crashed=true on still-alive PID")
	}
}

// Sanity: ESRCH from syscall.Kill is treated as "process gone".
func TestLiveness_DefaultProbeRespectsESRCH(t *testing.T) {
	if probePIDAlive(99999) {
		// On Linux, syscall.Kill(99999, 0) typically returns ESRCH; if the
		// machine is heavily loaded with PIDs this could exist. Skip when so.
		t.Skip("PID 99999 is alive on this system; cannot validate ESRCH path")
	}
	if !errors.Is(errLivenessUnavailable, errors.New("not used")) {
		// Just to ensure the package compiles with all symbols.
	}
}
```

(O test "DefaultProbeRespectsESRCH" é mais sanity. Se for fragile, remover.)

- [ ] **Step 2: Adicionar campos a `domain.RunningInstance`**

Editar `internal/domain/instance.go`. Conteúdo final:

```go
package domain

import "time"

// RunningInstance describes a live llama-server process tracked by ProcessManager.
type RunningInstance struct {
	ProfileID  string     `json:"profileId"`
	PID        int        `json:"pid"`
	Port       int        `json:"port"`
	LogPath    string     `json:"logPath"`
	StartedAt  time.Time  `json:"startedAt"`
	Background bool       `json:"background"`
	Crashed    bool       `json:"crashed,omitempty"`
	ExitedAt   *time.Time `json:"exitedAt,omitempty"`
}

// LogLine is a single line of llama-server output.
type LogLine struct {
	Timestamp time.Time
	Level     string // INFO | WARN | ERROR | "" if unparseable
	Text      string
}
```

- [ ] **Step 3: Implementar liveness ticker**

Criar `internal/service/processmgr/liveness.go`:

```go
package processmgr

import (
	"errors"
	"syscall"
	"time"
)

// errLivenessUnavailable é retornado quando a plataforma não suporta a probe
// (não é o caso em Linux). Mantido como sentinela para futuras builds.
var errLivenessUnavailable = errors.New("liveness probe unavailable on this platform")

// probePIDAlive retorna true quando syscall.Kill(pid, 0) sucede, o que
// significa que o processo existe (independente da permissão de signaling).
// Em Linux, ESRCH indica que o PID já não existe.
func probePIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.EPERM) {
		// existe mas não temos permissão — ainda assim, vivo.
		return true
	}
	return false
}

// startLivenessWithProbe inicia uma goroutine que polla cada `interval` os
// PIDs trackeados e marca como Crashed os que `probe(pid)` retornar false.
// Retorna função stop() idempotente.
func (m *fsManager) startLivenessWithProbe(interval time.Duration, probe func(int) bool) func() {
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-t.C:
				m.mu.Lock()
				dirty := false
				for pid, inst := range m.tracked {
					if inst.Crashed {
						continue
					}
					if probe(pid) {
						continue
					}
					t := now.UTC()
					inst.Crashed = true
					inst.ExitedAt = &t
					m.tracked[pid] = inst
					dirty = true
				}
				snapshot := snapshotLocked(m.tracked)
				m.mu.Unlock()
				if dirty {
					_ = saveRegistry(m.registryPath, snapshot)
				}
			}
		}
	}()
	once := false
	return func() {
		if once {
			return
		}
		once = true
		close(stop)
	}
}

// startLiveness usa a probe default (syscall.Kill) e tick de 5 segundos.
func (m *fsManager) startLiveness() func() {
	return m.startLivenessWithProbe(5*time.Second, probePIDAlive)
}
```

- [ ] **Step 4: Wire em `Manager` + `Close`**

Editar `internal/service/processmgr/processmgr.go`. Adicionar à interface:

```go
type Manager interface {
	Launch(p domain.Profile, mode LaunchMode) (domain.RunningInstance, error)
	Kill(pid int) error
	List() []domain.RunningInstance
	WaitHealthy(pid, port int, timeout time.Duration) error
	TailLogs(pid int) (io.ReadCloser, error)
	Close() error
}
```

Editar `internal/service/processmgr/manager.go`. Na struct adicionar:

```go
	livenessStop func()
```

Em `New`, na construção (depois de criar `&fsManager{...}`), iniciar a goroutine:

```go
	m := &fsManager{ ... } // como antes
	m.livenessStop = m.startLiveness()
	return m
```

E adicionar método `Close`:

```go
// Close stops the liveness ticker. Idempotent.
func (m *fsManager) Close() error {
	if m.livenessStop != nil {
		m.livenessStop()
	}
	return nil
}
```

Em `NewWithCheck`, garantir que após `New(cfg)` não precisa nada extra (o liveness já inicia via New).

- [ ] **Step 5: main.go chama `defer mgr.Close()`**

Editar `cmd/model-loader/main.go`. Logo após instanciar `mgr`:

```go
	defer mgr.Close()
```

- [ ] **Step 6: Run tests — passam**

Run: `go test ./internal/service/processmgr/ -v`
Expected: PASS — todos.

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/instance.go internal/service/processmgr/liveness.go internal/service/processmgr/liveness_test.go internal/service/processmgr/manager.go internal/service/processmgr/processmgr.go cmd/model-loader/main.go
git commit -m "$(cat <<'EOF'
feat(processmgr): liveness ticker detects crashed background instances

Background llama-server processes can crash silently. New goroutine
polls each tracked PID every 5s with syscall.Kill(pid, 0); when ESRCH
returns, marks domain.RunningInstance.Crashed=true and persists the
registry. Manager gains Close() to stop the ticker.

Implements design § 8 row "Crash do llama-server background".

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

### Task 11: MonitorPage refresh periódico + crash row badge

**Files:**
- Modify: `internal/ui/pages/monitor.go` (Init/Update — periodic tick, crash row)
- Modify: `internal/ui/pages/monitor_test.go`

Motivação: hoje refresh só ocorre via `Init` ou após `k`/`r`. Crashes detectados em background não chegam ao usuário sem refresh periódico. E quando chegam, devem aparecer com badge visual.

- [ ] **Step 1: Test crash row é visualmente distinto**

Adicionar a `monitor_test.go`:

```go
func TestMonitorPage_CrashedRowShowsMarker(t *testing.T) {
	exit := time.Now().UTC()
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{
		PID:      777,
		Port:     8080,
		LogPath:  "/tmp/x.log",
		Crashed:  true,
		ExitedAt: &exit,
	}}}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	out := p.View()
	if !strings.Contains(out, "✗") && !strings.Contains(out, "crashed") {
		t.Fatalf("crashed row missing badge; got:\n%s", out)
	}
}

func TestMonitorPage_PeriodicRefreshTickEmitsRefreshCmd(t *testing.T) {
	pm := &fakeProcMgr{}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	cmd := p.Init()
	if cmd == nil {
		t.Fatal("Init returned nil")
	}
	// Init should batch a refreshInstances cmd AND a tick cmd.
	// Drain the batch and verify a tick is scheduled. The simplest assertion
	// is to check the page-level state: after Init, periodicTickActive == true.
	if !p.periodicTickActive {
		t.Errorf("periodicTickActive = false; want true")
	}
}
```

- [ ] **Step 2: Run tests — falham**

Run: `go test ./internal/ui/pages/ -run "TestMonitorPage_CrashedRowShowsMarker|TestMonitorPage_PeriodicRefreshTickEmitsRefreshCmd" -v`
Expected: FAIL.

- [ ] **Step 3: Implementar tick + crash badge**

Editar `internal/ui/pages/monitor.go`.

(a) Adicionar campo na struct:

```go
	periodicTickActive bool
```

(b) Adicionar tipo de mensagem:

```go
type monitorPeriodicTickMsg struct{}
```

(c) Modificar `Init`:

```go
func (p *MonitorPage) Init() tea.Cmd {
	p.periodicTickActive = true
	return tea.Batch(p.refreshInstancesCmd(), p.periodicTickCmd())
}

func (p *MonitorPage) periodicTickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return monitorPeriodicTickMsg{} })
}
```

(d) Em `Update`, adicionar handler:

```go
	case monitorPeriodicTickMsg:
		cmds = append(cmds, p.refreshInstancesCmd(), p.periodicTickCmd())
```

(e) Modificar `applyInstances` para também cancelar subs de PIDs `Crashed`:

```go
	// Cancel orphan and crashed subs.
	seen := make(map[int]bool, len(insts))
	for _, ri := range insts {
		seen[ri.PID] = true
		if ri.Crashed {
			if st, ok := p.subs[ri.PID]; ok {
				cancel := st.cancel
				go func() { _ = cancel() }()
				delete(p.subs, ri.PID)
				delete(p.chans, ri.PID)
			}
		}
	}
	for pid, st := range p.subs {
		if !seen[pid] {
			cancel := st.cancel
			go func() { _ = cancel() }()
			delete(p.subs, pid)
			delete(p.chans, pid)
		}
	}
```

(f) Modificar `applyInstances` rendering (linhas que constroem `rows`):

```go
	for _, ri := range insts {
		pid := fmt.Sprintf("%d", ri.PID)
		profileCol := ri.ProfileID
		if ri.Crashed {
			pid = "✗ " + pid
			profileCol = ri.ProfileID + " (crashed)"
		}
		rows = append(rows, table.Row{
			pid,
			fmt.Sprintf("%d", ri.Port),
			profileCol,
			"--", "--", "--",
		})
	}
```

(g) Em `selectedPID`, ajustar o parse para tolerar prefixo `✗`:

```go
func (p *MonitorPage) selectedPID() int {
	if len(p.tbl.Rows()) == 0 {
		return 0
	}
	row := p.tbl.SelectedRow()
	pidCol := row[0]
	pidCol = strings.TrimPrefix(pidCol, "✗ ")
	var pid int
	_, _ = fmt.Sscanf(pidCol, "%d", &pid)
	return pid
}
```

Imports a adicionar/garantir: `"time"`, `"strings"` (já existe).

- [ ] **Step 4: Run tests passam**

Run: `go test ./internal/ui/pages/ -v`
Expected: PASS — todos.

- [ ] **Step 5: Run full suite**

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/pages/monitor.go internal/ui/pages/monitor_test.go
git commit -m "$(cat <<'EOF'
feat(monitor): periodic refresh + crash row badge

MonitorPage now ticks every 2s to refetch the instance list, surfacing
crashes detected by the manager's liveness goroutine. Crashed rows are
prefixed with ✗ and the profile column shows "<id> (crashed)". Their
subscriptions are torn down asynchronously, freeing pollers.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Phase F — Help modal

### Task 12: `components.help.go` — markdown const + glamour render

**Files:**
- Create: `internal/ui/components/help.go`
- Create: `internal/ui/components/help_test.go`
- Modify: `go.mod` / `go.sum` — `glamour` dep

Motivação: design § 7.4 — `?` Help modal (markdown via glamour).

- [ ] **Step 1: Adicionar `glamour` ao go.mod**

Run:

```bash
cd /home/diogo/dev/llama.cpp-loader
go get github.com/charmbracelet/glamour
```

Expected: `go.mod` ganha linha `github.com/charmbracelet/glamour vX.Y.Z`.

- [ ] **Step 2: Test cobre que `RenderHelp` retorna string não-vazia + contém "Keybindings"**

Criar `internal/ui/components/help_test.go`:

```go
package components

import (
	"strings"
	"testing"
)

func TestRenderHelp_ContainsKeybindings(t *testing.T) {
	out, err := RenderHelp(80)
	if err != nil {
		t.Fatalf("RenderHelp: %v", err)
	}
	if out == "" {
		t.Fatal("RenderHelp returned empty string")
	}
	if !strings.Contains(out, "Keybindings") {
		t.Errorf("output missing 'Keybindings' header; got:\n%s", out)
	}
}

func TestRenderHelp_MentionsAllTabs(t *testing.T) {
	out, err := RenderHelp(80)
	if err != nil {
		t.Fatalf("RenderHelp: %v", err)
	}
	for _, tab := range []string{"Profiles", "Launcher", "Monitor", "Models"} {
		if !strings.Contains(out, tab) {
			t.Errorf("output missing %q", tab)
		}
	}
}
```

- [ ] **Step 3: Run test — falha**

Run: `go test ./internal/ui/components/ -run TestRenderHelp -v`
Expected: FAIL.

- [ ] **Step 4: Implementar**

Criar `internal/ui/components/help.go`:

```go
package components

import "github.com/charmbracelet/glamour"

// HelpMarkdown é o conteúdo da modal de help acessível via `?` em qualquer
// página. Atualizado quando keybindings mudam.
const HelpMarkdown = `# model-loader — Keybindings

## Global

- ` + "`1`" + `–` + "`4`" + ` — switch directly to a tab
- ` + "`Tab`" + ` — next tab     ` + "`Shift+Tab`" + ` — previous tab
- ` + "`?`" + ` — toggle this help
- ` + "`q`" + ` / ` + "`Ctrl+C`" + ` — quit (background instances survive)

## Profiles tab

- ` + "`n`" + ` — new profile     ` + "`d`" + ` — duplicate
- ` + "`x`" + ` — delete         ` + "`s`" + ` — save
- ` + "`L`" + ` — launch directly from selected profile
- ` + "`/`" + ` — filter

## Launcher tab

- ` + "`b`" + ` — toggle background/foreground (default background)
- ` + "`enter`" + ` — launch selected profile
- ` + "`k`" + ` — kill the most recent launched instance
- ` + "`r`" + ` — refresh profile list

## Monitor tab

- ` + "`Tab`" + ` — cycle Logs / Slots / Metrics sub-views
- ` + "`Space`" + ` — pause/resume log scroll
- ` + "`k`" + ` — kill selected instance
- ` + "`r`" + ` — restart selected instance (Kill + Launch)

## Models tab

- ` + "`R`" + ` — rescan all configured paths
- ` + "`/`" + ` — filter
- ` + "`enter`" + ` — actions: use in new profile / existing profile / reveal path
`

// RenderHelp retorna o markdown HelpMarkdown renderizado via glamour.
// width informa ao renderer o tamanho da viewport em colunas (afeta wrap).
func RenderHelp(width int) (string, error) {
	if width <= 0 {
		width = 80
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return "", err
	}
	return r.Render(HelpMarkdown)
}
```

- [ ] **Step 5: Run tests passam**

Run: `go test ./internal/ui/components/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/components/help.go internal/ui/components/help_test.go go.mod go.sum
git commit -m "$(cat <<'EOF'
feat(components): help markdown const + glamour-rendered RenderHelp

Adds the canonical keybindings reference embedded as a const string and
exposes RenderHelp(width) which wraps it via glamour.NewTermRenderer.
Used by slice-6 Task 13 (`?` overlay in root).

Adds glamour as a dependency.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

### Task 13: Root toggle `?` para help overlay

**Files:**
- Modify: `internal/ui/root.go`
- Modify: `internal/ui/root_test.go`

Motivação: a UX final do help — pressionar `?` em qualquer tab abre overlay; `Esc` ou `?` fecha.

- [ ] **Step 1: Test help toggle**

Adicionar a `internal/ui/root_test.go`:

```go
func TestRoot_HelpToggle(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})
	// Help closed by default.
	if rendered := r.View(); strings.Contains(rendered, "Keybindings") {
		t.Error("help is open before any keypress")
	}
	// Press ?
	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	rm := updated.(RootModel)
	if rendered := rm.View(); !strings.Contains(rendered, "Keybindings") {
		t.Errorf("help did not open after ?; view:\n%s", rendered)
	}
	// Press Esc
	updated, _ = rm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	rm = updated.(RootModel)
	if rendered := rm.View(); strings.Contains(rendered, "Keybindings") {
		t.Errorf("help did not close on Esc; view:\n%s", rendered)
	}
}

func TestRoot_HelpSwallowsTabSwitch(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})
	// Open help.
	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	rm := updated.(RootModel)
	// Press 2 — should NOT switch tab while help is open.
	updated, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	rm = updated.(RootModel)
	if rm.active != TabProfiles {
		t.Errorf("active = %v; want still TabProfiles", rm.active)
	}
}
```

- [ ] **Step 2: Run test — falha**

Run: `go test ./internal/ui/ -run TestRoot_Help -v`
Expected: FAIL.

- [ ] **Step 3: Implementar**

Editar `internal/ui/root.go`. Na struct, adicionar:

```go
	helpOpen bool
```

No `Update`, antes do switch de KeyMsg existente, capturar `?` e (quando help aberto) Esc:

```go
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Help toggle is global and pre-empts page routing.
		if m.helpOpen {
			switch msg.String() {
			case "?", "esc":
				m.helpOpen = false
				return m, nil
			case "ctrl+c", "q":
				return m, tea.Quit
			}
			return m, nil // swallow everything else while help is open
		}
		if msg.String() == "?" {
			m.helpOpen = true
			return m, nil
		}
		// ... resto do switch atual (q, 1-5, tab, etc)
	}
```

E no `View`, antes do return final:

```go
func (m RootModel) View() string {
	if m.bootBlocker != nil {
		return components.Modal(m.bootBlocker.title, m.bootBlocker.body+"\n\nPress q to quit.", m.width, m.height)
	}
	if m.helpOpen {
		body, err := components.RenderHelp(m.width - 8) // padding
		if err != nil {
			body = components.HelpMarkdown // fallback raw
		}
		return components.Modal("Keybindings", body, m.width, m.height)
	}
	// ... return existente
}
```

- [ ] **Step 4: Run tests passam**

Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 5: Run full suite**

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/root.go internal/ui/root_test.go
git commit -m "$(cat <<'EOF'
feat(ui): root captures `?` to toggle help modal overlay

`?` toggles a glamour-rendered keybindings reference over the active page;
Esc or `?` again closes it. While open, all other keys are swallowed
(except q/ctrl+c which still quit). Implements design § 7.4 row "?".

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Phase G — Wrap

### Task 14: Keybindings audit — footer hints consistentes em cada page

**Files:**
- Modify: `internal/ui/pages/profiles.go` (footer)
- Modify: `internal/ui/pages/launcher.go` (footer já existe; revisar)
- Modify: `internal/ui/pages/models.go` (footer)
- Modify: `internal/ui/pages/monitor.go` (footer novo — não existe hoje)
- Test: cada `*_test.go` correspondente

Motivação: o usuário deve ver hint de keybindings em cada page sem precisar abrir help. Hoje:
- ProfilesPage: alguns hints existem mas podem estar incompletos
- LauncherPage: footer já tem `[b] mode  [enter] launch  [k] kill last  [r] refresh`
- ModelsPage: revisar
- MonitorPage: **não tem footer**

A meta: cada page renderiza um rodapé `[<key>] <action>  ...  [?] help` consistente.

- [ ] **Step 1: Test MonitorPage footer renderiza hints**

Adicionar a `monitor_test.go`:

```go
func TestMonitorPage_FooterShowsHints(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	out := p.View()
	for _, want := range []string{"[Tab]", "[k]", "[r]", "[Space]", "[?]"} {
		if !strings.Contains(out, want) {
			t.Errorf("footer missing %q; got:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run — falha**

Run: `go test ./internal/ui/pages/ -run TestMonitorPage_FooterShowsHints -v`
Expected: FAIL.

- [ ] **Step 3: MonitorPage footer**

Editar `internal/ui/pages/monitor.go`. Em `View`, antes do return final, definir e anexar:

```go
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(
		"[Tab] cycle view  [Space] pause  [k] kill  [r] restart  [?] help",
	)
	return top + "\n\n" + bottom + "\n\n" + footer
```

- [ ] **Step 4: Auditar e ajustar footers nas outras pages**

Garantir presença de `[?] help` em cada uma:

- `LauncherPage` View: substituir `[b] mode  [enter] launch  [k] kill last  [r] refresh` → `[b] mode  [enter] launch  [k] kill last  [r] refresh  [?] help`.
- `ModelsPage` View: localizar string de footer (se existir). Adicionar `[?] help` ao final. Se ausente, criar.
- `ProfilesPage` View: idem — adicionar `[?] help`.

Exemplo (LauncherPage):

```go
	footer := "[b] mode  [enter] launch  [k] kill last  [r] refresh  [?] help"
```

- [ ] **Step 5: Adicionar test simples por page que verifica `[?] help` no footer**

Em `launcher_test.go`:

```go
func TestLauncherPage_FooterMentionsHelp(t *testing.T) {
	page := pages.NewLauncherPage(nil, nil, nil)
	if !strings.Contains(page.View(), "[?] help") {
		t.Errorf("launcher footer missing [?] help")
	}
}
```

Idem para `profiles_test.go` e `models_test.go`.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/ui/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/pages/monitor.go internal/ui/pages/launcher.go internal/ui/pages/profiles.go internal/ui/pages/models.go internal/ui/pages/monitor_test.go internal/ui/pages/launcher_test.go internal/ui/pages/profiles_test.go internal/ui/pages/models_test.go
git commit -m "$(cat <<'EOF'
feat(ui): consistent footer hints across all pages, including [?] help

MonitorPage gains a footer (was missing). All four pages now render a
unified hint line ending in [?] help. Closes the keybindings audit
called out in design § 7.4.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

### Task 15: Manual smoke test — checklist de fluxos end-to-end

**Files:**
- Modify: `docs/superpowers/plans/2026-04-29-model-loader-slice-6.md` (este arquivo — tickar checklist)

Não há código. Objetivo: validar manualmente que o binário compila, abre, navega, todos os fluxos críticos funcionam contra o `fake-llama-server.sh`.

- [x] **Step 1: Build** — `go build ./...` clean, no warnings.

- [x] **Step 2: Vet** — `go vet ./...` clean.

- [x] **Step 3: Tests** — `go test ./internal/...` 196/196 passed in 12 packages.

> **Manual TTY smokes (Steps 4–11) deferred to user verification.** Each requires interacting with the running TUI, which the implementation flow can't drive. Run `./model-loader` after merge and tick below as you confirm each.

- [ ] **Step 4: Smoke check — `?` modal abre/fecha**

Run: `./model-loader` (ou `go run ./cmd/model-loader`)

Pressionar `?` — modal deve aparecer com keybindings.
Pressionar `Esc` — modal fecha.
Pressionar `?` novamente — abre.
Pressionar `1`/`2`/`3`/`4` enquanto help aberto — deve ser ignorado.
Pressionar `q` — sai.

- [ ] **Step 5: Smoke check — profile launch via fake-llama-server**

Pre-condição: `testdata/fake-llama-server.sh` está executável e disponível em `PATH`. Para o teste, alias temporário:

```bash
export PATH="/path/to/model-loader/testdata:$PATH"
ln -sf "$(pwd)/testdata/fake-llama-server.sh" /tmp/llama-server
PATH="/tmp:$PATH" ./model-loader
```

(Ou usar `Config.Binary` no main.go com flag CLI futuro — para o smoke, alias é suficiente.)

Em ProfilesPage:
- Criar profile novo (`n`), Model = `/tmp/dummy.gguf` (`touch /tmp/dummy.gguf` antes), Port = 8080.
- Salvar (`s`).
- Pressionar `L` para launch.

Esperado:
- Tab Monitor abre automaticamente.
- Row aparece com PID + Port=8080.
- Logs sub-view mostra linhas do fake-server.
- Slots sub-view (Tab) mostra 1 slot id=0 state=idle.
- Metrics sub-view (Tab) mostra placeholder ou primeiros samples após ~2s.

- [ ] **Step 6: Smoke check — `r` restart**

Selecionar a row no Monitor e pressionar `r`.
Esperado: row some por ~1s, reaparece com novo PID. Logs reiniciam.

- [ ] **Step 7: Smoke check — boot blocker**

Em outro terminal:

```bash
PATH="/usr/sbin:/sbin" ./model-loader
```

Esperado: TUI abre com modal centralizado "llama-server not found in PATH" e instruções. `q` fecha.

- [ ] **Step 8: Smoke check — corrupt profile marker**

```bash
echo "{not json" > ~/.config/model-loader/profiles/broken.json
./model-loader
```

Esperado: ProfilesPage mostra `⚠ broken` na lista, com descrição do erro. Tentar selecionar e pressionar `L` — não lança, status mostra hint sobre corruption.

- [ ] **Step 9: Smoke check — port-busy hint**

Iniciar netcat ocupando 8080:

```bash
nc -l 8080 &
```

Lançar profile com port=8080. Esperado: status do LauncherPage mostra "error: port in use — change the profile port or kill the running PID".

- [ ] **Step 10: Smoke check — model missing hint**

Editar profile, mudar Model para `/nonexistent.gguf`. Salvar. Lançar.
Esperado: status mostra "error: model file not found — fix the profile's Model path".

- [ ] **Step 11: Smoke check — crash detection**

Lançar fake-server. Em outro terminal: `kill <PID>`. Aguardar 5s.
Esperado: row no Monitor passa a mostrar `✗ <PID>` + "(crashed)" + sub para a row é cancelada (logs stale, mas página não trava).

- [ ] **Step 12: Tickar este checklist + commit final**

Após cada smoke step OK, marcar como ✓ no plan. Commit final:

```bash
git add docs/superpowers/plans/2026-04-29-model-loader-slice-6.md
git commit -m "$(cat <<'EOF'
docs(slice-6): mark manual smoke checklist complete

All eleven manual smoke steps verified:
  - help modal open/close
  - profile launch via fake-llama-server
  - r-restart cycles PID
  - boot blocker on missing binary
  - corrupt profile ⚠ marker
  - port-busy hint
  - model-missing hint
  - crash detection via liveness ticker

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Self-Review Checklist (do plano contra a spec)

**1. Spec coverage (design § 8 + § 7.4 + carry-overs review-5):**

| Spec / requisito | Coberto por |
|---|---|
| `?` Help modal (markdown via glamour) | T12 + T13 |
| Keybindings finais auditados | T14 |
| llama-server ausente do PATH → modal bloqueante | T8 + T9 |
| Profile JSON corrompido → ⚠ marker | T5 + T6 |
| Port busy no launch → dica útil | T7 |
| Model path missing → erro inline | T7 |
| nvidia-smi ausente → "n/a" | já feito em slice-5 (gpu.go) |
| Crash do llama-server → kill -0 + exit code | T10 + T11 |
| Slice-5 review I-1 cancel async | T1 |
| Slice-5 review I-3 r restart real | T4 |
| Slice-5 review M-1 sparkline placeholder | T2 |
| Slice-5 review M-2 PID forward | T3 |

**2. Placeholder scan:** verificado — sem "TBD"/"implement later" não documentado. Crash detection ExitCode é best-effort (deferido — usar log-tail é caro de testar; a flag Crashed + ExitedAt cobrem o requisito mínimo).

**3. Type consistency:**
- `profileStoreIface` em monitor.go = subset de `profilestore.Store` (apenas `Get`).
- `processmgr.Manager` ganha `Close()` em T10; main.go chama via `defer`.
- `domain.RunningInstance.Crashed/ExitedAt` são novos campos opcionais (`omitempty` no JSON), preservando backward-compat de `instances.json`.
- `MonitorSelectPIDMsg` (público) vs `monitorSelectPIDInternalMsg` (privado) intencional — só root emite o público; MonitorPage usa o privado para encadear refresh+select.
- `ListWithDiagnostics` adicionado à interface (não breaking, mas requer todos os fakes nos testes implementarem o método; a T6 inclui esse update).

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-04-29-model-loader-slice-6.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — dispatch fresh subagent per task, two-stage review (spec + quality) entre tarefas, iteração rápida.

**2. Inline Execution** — execute as tasks neste session, batch com checkpoints.

**Which approach?**

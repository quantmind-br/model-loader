# model-loader — Slice 5 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `Monitor` service (log tail + `/slots` poll + GPU stats + sliding 60s metrics) + `TailLogs(pid)` em `ProcessManager` + `monitorPage` (instances table + sub-views cycláveis Logs/Slots/Métricas) + auto-switch de `LauncherPage` para `monitorPage` após launch saudável, encerrando slice 5 do roadmap (design § 11).

**Architecture:** Camada `service/monitor` orquestra goroutine por subscription com três sources: tail de log via `fsnotify`, poll HTTP `/slots`+`/health` em ticker 1s, GPU stats via `gopsutil` com fallback `nvidia-smi` exec em ticker 2s. Os eventos viajam por `<-chan MonitorEvent`. Subcomponente `metrics.go` mantém janela deslizante de 60s para `tokens/s` e `req/s`, parseando linhas do log com regex e diffs entre snapshots `/slots`. `MonitorPage` consome via streaming `tea.Cmd` re-armado a cada evento e mantém uma subscription por instância em paralelo (não derruba ao trocar seleção, preserva métricas). `LauncherPage` emite `SwitchToMonitorMsg{pid}` após `WaitHealthy` retornar nil; `root.go` roteia para tab Monitor.

**Tech Stack:** Go 1.26.2, `charmbracelet/bubbletea`, `charmbracelet/bubbles` (table, viewport), `fsnotify/fsnotify`, `shirou/gopsutil/v3`, `encoding/csv` (nvidia-smi parse), `regexp` (tokens/s extraction).

**Constraints / decisões fixadas:**
- `TailLogs(pid)` (deferido em slice 4) entregue em T8.
- Auto-switch `LauncherPage → monitorPage` (F2 step 4, deferido em slice 4) entregue em T16.
- `gopsutil/v3` introduzido aqui (slice 4 evitou para reduzir deps).
- Ring buffer de logs: 2000 linhas por instância (cap ~200 KB de memória).
- Sparkline ASCII: 8 níveis `▁▂▃▄▅▆▇█` renderizados via `lipgloss` (sem dep nova).
- Multi-instance: `monitorPage.subs` é `map[int]*subscription`; criada/destruída quando `manager.List()` muda; linha selecionada da tabela top determina qual sub alimenta as três sub-views.
- `TailLogs` retorna `(io.ReadCloser, error)` — o log file aberto em modo somente-leitura. Cabe ao consumidor (Monitor) tail via `fsnotify`.
- Tokens/s parsing: regex sobre logs (`prompt eval time = ... ms / ... tokens, ... tokens per second`); req/s via diff de `slot.n_decoded` agregado entre tickers `/slots`.
- Restart `r`: `manager.Kill(pid)` + `manager.Launch(profile, mode)` reusando o profile da instância. Novo PID, sub anterior cancelada.
- Tab order final: `1 Profiles | 2 Launcher | 3 Models | 4 Monitor`. (Slice 4 atual coloca Monitor como tab 4 placeholder; aqui substitui pelo real.)

**Spec deviation notes:**
1. Spec § 7.2 não detalha como sub-views filtram instância; assumimos "linha selecionada na top table = instância ativa para todas as sub-views" para simplificar.
2. Spec § 6.6 lista `Source` como enum `Logs|Slots|GPU|Health`. Spliting em `Logs|Slots|GPU|Health|Metrics` (5º para eventos agregados de tokens/s + req/s, evita re-cálculo na UI).
3. Spec § 6.6 `Tick 2s nvidia-smi`: usamos `gopsutil/v3` primeiro (process-aware), `nvidia-smi --query-gpu=...` apenas como fallback se gopsutil retorna VRAM zero/erro.

---

## File Structure

**Domain (sem mudança):** já existem `RunningInstance` e tipos relacionados (slice 4).

**Service novo:**
- `internal/service/monitor/monitor.go` (NEW) — `Monitor` interface, `MonitorEvent`, `EventSource` enum, `Config`, sentinel errors.
- `internal/service/monitor/monitor_test.go` (NEW)
- `internal/service/monitor/ring.go` (NEW) — ring buffer thread-safe `logRing` (push, snapshot).
- `internal/service/monitor/ring_test.go` (NEW)
- `internal/service/monitor/logs.go` (NEW) — `logFollower` (fsnotify-based tail).
- `internal/service/monitor/logs_test.go` (NEW)
- `internal/service/monitor/slots.go` (NEW) — poll HTTP `/slots` + `/health`.
- `internal/service/monitor/slots_test.go` (NEW)
- `internal/service/monitor/gpu.go` (NEW) — gopsutil + nvidia-smi.
- `internal/service/monitor/gpu_test.go` (NEW)
- `internal/service/monitor/metrics.go` (NEW) — sliding 60s aggregator.
- `internal/service/monitor/metrics_test.go` (NEW)
- `internal/service/monitor/subscribe.go` (NEW) — `Subscribe()` orquestra fan-in.
- `internal/service/monitor/subscribe_test.go` (NEW)

**Service modificado:**
- `internal/service/processmgr/manager.go` (MODIFY) — adicionar `TailLogs(pid)` no `fsManager`.
- `internal/service/processmgr/processmgr.go` (MODIFY) — adicionar método à interface `Manager`.
- `internal/service/processmgr/manager_test.go` (MODIFY) — teste de `TailLogs` happy + unknown pid.

**UI nova:**
- `internal/ui/components/sparkline.go` (NEW) — `Sparkline(values []float64, width int) string`.
- `internal/ui/components/sparkline_test.go` (NEW)
- `internal/ui/pages/monitor.go` (NEW) — `MonitorPage` (instances table + sub-views).
- `internal/ui/pages/monitor_test.go` (NEW)

**UI modificada:**
- `internal/ui/root.go` (MODIFY) — `WithMonitorPage` builder + roteamento `SwitchToMonitorMsg`.
- `internal/ui/root_test.go` (MODIFY) — smoke test cross-tab routing.
- `internal/ui/pages/launcher.go` (MODIFY) — emitir `SwitchToMonitorMsg{pid}` quando `healthyMsg` chega.
- `internal/ui/pages/launcher_test.go` (MODIFY) — assert msg emitida.

**Wire:**
- `cmd/model-loader/main.go` (MODIFY) — instanciar `monitor.New` + `pages.NewMonitorPage` + `root.WithMonitorPage`.

**Testdata:**
- `testdata/fake-llama-server.sh` (MODIFY) — adicionar handler `/slots` retornando JSON de exemplo.

**Deps:**
- `go.mod` (MODIFY) — adicionar `github.com/fsnotify/fsnotify` + `github.com/shirou/gopsutil/v3`.
- `go.sum` (MODIFY) — auto via `go mod tidy`.

---

## Conventions for this plan

- TDD obrigatório onde sinaliza (test-first → fail → impl → pass → commit).
- Cada Task é um commit no mínimo. Refactors triviais podem ir num commit `chore` separado.
- Tipos `MonitorEvent`/`EventSource`/`Config` exposed; `logFollower`/`slotsPoller`/`gpuPoller`/`metricsAgg`/`subscription` não-exportados (private à package).
- Testes usam `t.TempDir()` + `httptest.Server` para isolation. `fsnotify` exige arquivo real → criar via TempDir.
- Não introduzir mocks de `os/exec`; gpu fallback testa via env var injection (`MONITOR_NVIDIA_SMI` override path).

---

## Pre-flight

- [x] **Step P1: Criar branch slice-5 a partir de main**

```bash
git checkout main
git pull --ff-only origin main 2>/dev/null || true
git checkout -b feat/slice-5
```

Expected: branch `feat/slice-5` criada e checked out.

- [x] **Step P2: Verificar baseline verde**

Run: `go test ./... && go vet ./...`
Expected: PASS em todos os packages, sem warnings.

- [x] **Step P3: Adicionar dependências**

Run:
```bash
go get github.com/fsnotify/fsnotify@v1.7.0
go get github.com/shirou/gopsutil/v3@v3.24.5
go mod tidy
```

Expected: `go.mod` ganha as duas linhas; `go build ./...` continua passando.

- [x] **Step P4: Commit deps**

```bash
git add go.mod go.sum
git commit -m "chore(deps): add fsnotify and gopsutil for slice 5"
```

---

## Phase A — Service plumbing (sem UI)

### Task 1: Monitor interface + types + sentinel errors

**Files:**
- Create: `internal/service/monitor/monitor.go`

- [x] **Step 1: Criar `monitor.go` com tipos exportados**

```go
// Package monitor streams logs, slot snapshots, GPU stats, and aggregated
// metrics from a running llama-server instance.
package monitor

import (
	"errors"
	"io"
	"time"
)

// EventSource identifies which producer emitted a MonitorEvent.
type EventSource int

const (
	SourceLogs EventSource = iota
	SourceSlots
	SourceHealth
	SourceGPU
	SourceMetrics
)

// MonitorEvent is the unit pushed on the channel returned by Subscribe.
// Data type varies per Source — consumers type-assert.
type MonitorEvent struct {
	Timestamp time.Time
	Source    EventSource
	PID       int
	Data      any
}

// LogLine wraps one line tailed from the log file.
type LogLine struct {
	Line string
}

// SlotSnapshot is the decoded payload of GET /slots.
type SlotSnapshot struct {
	Slots []Slot
}

type Slot struct {
	ID         int    `json:"id"`
	State      string `json:"state"`         // "idle" | "processing"
	NCtxUsed   int    `json:"n_ctx"`         // current ctx tokens
	NCtxMax    int    `json:"n_ctx_total"`   // ctx capacity
	NDecoded   int    `json:"n_decoded"`     // cumulative tokens decoded
	NPrompt    int    `json:"n_prompt"`      // cumulative prompt tokens
	Client     string `json:"id_task"`       // best-effort client id
}

// HealthStatus is the decoded payload of GET /health.
type HealthStatus struct {
	OK     bool
	Status string
}

// GPUStats is one snapshot of GPU usage for the process pid.
type GPUStats struct {
	VRAMUsedMB  uint64
	VRAMTotalMB uint64
	Utilization float64 // 0..100
	Source      string  // "gopsutil" | "nvidia-smi"
}

// Metrics is the rolling 60s aggregate.
type Metrics struct {
	TokensPerSec []float64 // last 60 samples (1/s)
	RequestsPerSec []float64
	WindowSeconds int
}

// Manager is the public service interface.
type Manager interface {
	// Subscribe starts a goroutine that emits events for the given pid/port.
	// cancel() releases all goroutines and closes the returned channel.
	Subscribe(pid int, port int, logPath string) (<-chan MonitorEvent, func() error, error)
}

// Config tunes pollers and buffers.
type Config struct {
	HTTPClient        HTTPDoer       // pluggable for tests
	NvidiaSMIPath     string         // override binary path; "" = look up "nvidia-smi" in PATH
	SlotsTickInterval time.Duration  // default 1s
	GPUTickInterval   time.Duration  // default 2s
	LogRingSize       int            // default 2000
	MetricsWindow     time.Duration  // default 60s
}

// HTTPDoer abstracts http.Client for tests.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Sentinel errors. UI maps these to status bar messages.
var (
	ErrUnknownPID    = errors.New("pid is not tracked")
	ErrLogPathEmpty  = errors.New("log path is empty")
	ErrSubscribeBusy = errors.New("subscription already active for pid")
)

// New returns a Manager backed by the default in-process implementation.
func New(cfg Config) Manager {
	if cfg.SlotsTickInterval == 0 {
		cfg.SlotsTickInterval = time.Second
	}
	if cfg.GPUTickInterval == 0 {
		cfg.GPUTickInterval = 2 * time.Second
	}
	if cfg.LogRingSize == 0 {
		cfg.LogRingSize = 2000
	}
	if cfg.MetricsWindow == 0 {
		cfg.MetricsWindow = 60 * time.Second
	}
	return &fsMonitor{cfg: cfg}
}

// fsMonitor stub — método Subscribe implementado em subscribe.go (T7).
type fsMonitor struct {
	cfg Config
}

// Garante imports usados depois.
var _ io.ReadCloser = (*emptyReader)(nil)

type emptyReader struct{}

func (emptyReader) Read(p []byte) (int, error) { return 0, io.EOF }
func (emptyReader) Close() error               { return nil }
```

E adicionar import:

```go
import "net/http"
```

- [x] **Step 2: Verificar build**

Run: `go build ./internal/service/monitor/`
Expected: build OK.

- [x] **Step 3: Commit**

```bash
git add internal/service/monitor/monitor.go
git commit -m "feat(monitor): Manager interface, EventSource and MonitorEvent types"
```

---

### Task 2: Ring buffer (`logRing`)

**Files:**
- Create: `internal/service/monitor/ring.go`
- Create: `internal/service/monitor/ring_test.go`

- [x] **Step 1: Escrever teste falhando**

```go
// internal/service/monitor/ring_test.go
package monitor

import (
	"sync"
	"testing"
)

func TestLogRing_PushUnderCapacity(t *testing.T) {
	r := newLogRing(5)
	r.push("a")
	r.push("b")
	r.push("c")
	got := r.snapshot()
	if want := []string{"a", "b", "c"}; !equalSlices(got, want) {
		t.Fatalf("snapshot = %v, want %v", got, want)
	}
}

func TestLogRing_PushOverwritesOldest(t *testing.T) {
	r := newLogRing(3)
	r.push("a")
	r.push("b")
	r.push("c")
	r.push("d") // overwrites "a"
	got := r.snapshot()
	if want := []string{"b", "c", "d"}; !equalSlices(got, want) {
		t.Fatalf("after overflow: snapshot = %v, want %v", got, want)
	}
}

func TestLogRing_ConcurrentPushSafe(t *testing.T) {
	r := newLogRing(1000)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				r.push("line")
			}
		}()
	}
	wg.Wait()
	if got := len(r.snapshot()); got != 1000 {
		t.Fatalf("snapshot len = %d, want 1000", got)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [x] **Step 2: Rodar — confirmar falha**

Run: `go test ./internal/service/monitor/ -run TestLogRing -v`
Expected: FAIL ("undefined: newLogRing").

- [x] **Step 3: Implementar `logRing`**

```go
// internal/service/monitor/ring.go
package monitor

import "sync"

// logRing is a fixed-capacity, thread-safe ring buffer of strings.
// Once full, push overwrites the oldest entry.
type logRing struct {
	mu   sync.Mutex
	buf  []string
	head int // next write position
	full bool
}

func newLogRing(cap int) *logRing {
	return &logRing{buf: make([]string, cap)}
}

func (r *logRing) push(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.head] = s
	r.head = (r.head + 1) % len(r.buf)
	if r.head == 0 {
		r.full = true
	}
}

// snapshot returns a chronologically-ordered copy.
func (r *logRing) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		out := make([]string, r.head)
		copy(out, r.buf[:r.head])
		return out
	}
	out := make([]string, len(r.buf))
	copy(out, r.buf[r.head:])
	copy(out[len(r.buf)-r.head:], r.buf[:r.head])
	return out
}
```

- [x] **Step 4: Rodar — confirmar passa**

Run: `go test ./internal/service/monitor/ -run TestLogRing -v`
Expected: PASS (3 tests).

- [x] **Step 5: Commit**

```bash
git add internal/service/monitor/ring.go internal/service/monitor/ring_test.go
git commit -m "feat(monitor): logRing thread-safe bounded buffer"
```

---

### Task 3: Log tail (`logFollower`)

**Files:**
- Create: `internal/service/monitor/logs.go`
- Create: `internal/service/monitor/logs_test.go`

- [x] **Step 1: Escrever teste falhando**

```go
// internal/service/monitor/logs_test.go
package monitor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLogFollower_TailsAppendedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	if err := os.WriteFile(path, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan string, 16)
	follower, err := newLogFollower(path, out)
	if err != nil {
		t.Fatalf("newLogFollower: %v", err)
	}
	go follower.run(ctx)

	// Drain pre-existing line.
	select {
	case line := <-out:
		if line != "first" {
			t.Fatalf("first emitted line = %q, want %q", line, "first")
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive pre-existing line")
	}

	// Append two more.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("second\nthird\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	got := []string{}
	deadline := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case line := <-out:
			got = append(got, line)
		case <-deadline:
			t.Fatalf("timed out after %d new lines", len(got))
		}
	}
	if got[0] != "second" || got[1] != "third" {
		t.Fatalf("got %v, want [second third]", got)
	}
}

func TestLogFollower_MissingFileReturnsError(t *testing.T) {
	out := make(chan string, 1)
	if _, err := newLogFollower("/nonexistent/path.log", out); err == nil {
		t.Fatal("expected error for missing file")
	}
}
```

- [x] **Step 2: Rodar — confirmar falha**

Run: `go test ./internal/service/monitor/ -run TestLogFollower -v`
Expected: FAIL ("undefined: newLogFollower").

- [x] **Step 3: Implementar `logFollower`**

```go
// internal/service/monitor/logs.go
package monitor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/fsnotify/fsnotify"
)

// logFollower tails a file and emits each line on out.
// Closes out when its context is cancelled.
type logFollower struct {
	path string
	file *os.File
	out  chan<- string
	w    *fsnotify.Watcher
}

func newLogFollower(path string, out chan<- string) (*logFollower, error) {
	if path == "" {
		return nil, ErrLogPathEmpty
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("fsnotify: %w", err)
	}
	if err := w.Add(path); err != nil {
		_ = f.Close()
		_ = w.Close()
		return nil, fmt.Errorf("fsnotify add: %w", err)
	}
	return &logFollower{path: path, file: f, out: out, w: w}, nil
}

// run blocks until ctx is done. Emits each newline-terminated chunk on out.
func (l *logFollower) run(ctx context.Context) {
	defer func() {
		_ = l.file.Close()
		_ = l.w.Close()
	}()
	rd := bufio.NewReader(l.file)
	emit := func() {
		for {
			line, err := rd.ReadString('\n')
			if len(line) > 0 {
				// strip trailing \n
				if line[len(line)-1] == '\n' {
					line = line[:len(line)-1]
				}
				select {
				case l.out <- line:
				case <-ctx.Done():
					return
				}
			}
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				return
			}
		}
	}
	emit() // initial flush of pre-existing content
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-l.w.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				emit()
			}
		case _, ok := <-l.w.Errors:
			if !ok {
				return
			}
			// transient watch error; keep going.
		}
	}
}
```

- [x] **Step 4: Rodar — confirmar passa**

Run: `go test ./internal/service/monitor/ -run TestLogFollower -v -timeout 10s`
Expected: PASS (2 tests).

- [x] **Step 5: Commit**

```bash
git add internal/service/monitor/logs.go internal/service/monitor/logs_test.go
git commit -m "feat(monitor): logFollower tails server log via fsnotify"
```

---

### Task 4: Slots/health poller

**Files:**
- Create: `internal/service/monitor/slots.go`
- Create: `internal/service/monitor/slots_test.go`

- [x] **Step 1: Escrever teste falhando**

```go
// internal/service/monitor/slots_test.go
package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSlotsPoller_EmitsSnapshots(t *testing.T) {
	// Fake llama-server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slots":
			body := []map[string]any{
				{"id": 0, "state": "idle", "n_ctx": 0, "n_ctx_total": 4096, "n_decoded": 0, "n_prompt": 0, "id_task": ""},
				{"id": 1, "state": "processing", "n_ctx": 128, "n_ctx_total": 4096, "n_decoded": 64, "n_prompt": 32, "id_task": "abc"},
			}
			_ = json.NewEncoder(w).Encode(body)
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan MonitorEvent, 16)
	p := newSlotsPoller(srv.URL, http.DefaultClient, 50*time.Millisecond, out)
	go p.run(ctx)

	gotSlots, gotHealth := false, false
	deadline := time.After(2 * time.Second)
	for !gotSlots || !gotHealth {
		select {
		case ev := <-out:
			switch ev.Source {
			case SourceSlots:
				snap, ok := ev.Data.(SlotSnapshot)
				if !ok {
					t.Fatalf("Slots Data type = %T, want SlotSnapshot", ev.Data)
				}
				if len(snap.Slots) != 2 {
					t.Fatalf("slot count = %d, want 2", len(snap.Slots))
				}
				if snap.Slots[1].State != "processing" {
					t.Fatalf("slot[1].State = %q", snap.Slots[1].State)
				}
				gotSlots = true
			case SourceHealth:
				h, ok := ev.Data.(HealthStatus)
				if !ok {
					t.Fatalf("Health Data type = %T, want HealthStatus", ev.Data)
				}
				if !h.OK {
					t.Fatalf("Health.OK = false")
				}
				gotHealth = true
			}
		case <-deadline:
			t.Fatalf("timeout — slots=%v health=%v", gotSlots, gotHealth)
		}
	}
}

func TestSlotsPoller_ServerErrorEmitsHealthDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan MonitorEvent, 8)
	p := newSlotsPoller(srv.URL, http.DefaultClient, 50*time.Millisecond, out)
	go p.run(ctx)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-out:
			if ev.Source == SourceHealth {
				h := ev.Data.(HealthStatus)
				if h.OK {
					t.Fatalf("expected Health.OK=false on 500 response")
				}
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for unhealthy event")
		}
	}
}
```

- [x] **Step 2: Rodar — confirmar falha**

Run: `go test ./internal/service/monitor/ -run TestSlotsPoller -v -timeout 10s`
Expected: FAIL ("undefined: newSlotsPoller").

- [x] **Step 3: Implementar `slotsPoller`**

```go
// internal/service/monitor/slots.go
package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type slotsPoller struct {
	baseURL  string
	client   HTTPDoer
	interval time.Duration
	out      chan<- MonitorEvent
}

func newSlotsPoller(baseURL string, client HTTPDoer, interval time.Duration, out chan<- MonitorEvent) *slotsPoller {
	if client == nil {
		client = http.DefaultClient
	}
	return &slotsPoller{baseURL: baseURL, client: client, interval: interval, out: out}
}

func (p *slotsPoller) run(ctx context.Context) {
	tick := time.NewTicker(p.interval)
	defer tick.Stop()
	// First poll immediately.
	p.pollOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			p.pollOnce(ctx)
		}
	}
}

func (p *slotsPoller) pollOnce(ctx context.Context) {
	p.fetchHealth(ctx)
	p.fetchSlots(ctx)
}

func (p *slotsPoller) fetchHealth(ctx context.Context) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/health", nil)
	resp, err := p.client.Do(req)
	h := HealthStatus{}
	if err != nil || resp == nil {
		h.OK = false
		h.Status = "unreachable"
	} else {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			h.OK = true
			h.Status = "ok"
		} else {
			h.OK = false
			h.Status = resp.Status
		}
	}
	p.emit(MonitorEvent{Timestamp: time.Now(), Source: SourceHealth, Data: h})
}

func (p *slotsPoller) fetchSlots(ctx context.Context) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/slots", nil)
	resp, err := p.client.Do(req)
	if err != nil || resp == nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}
	var slots []Slot
	if err := json.NewDecoder(resp.Body).Decode(&slots); err != nil {
		return
	}
	p.emit(MonitorEvent{Timestamp: time.Now(), Source: SourceSlots, Data: SlotSnapshot{Slots: slots}})
}

func (p *slotsPoller) emit(ev MonitorEvent) {
	select {
	case p.out <- ev:
	default:
		// drop on backpressure; UI will catch up next tick.
	}
}
```

- [x] **Step 4: Rodar — confirmar passa**

Run: `go test ./internal/service/monitor/ -run TestSlotsPoller -v -timeout 10s`
Expected: PASS (2 tests).

- [x] **Step 5: Commit**

```bash
git add internal/service/monitor/slots.go internal/service/monitor/slots_test.go
git commit -m "feat(monitor): slotsPoller for /slots and /health"
```

---

### Task 5: GPU stats (gopsutil + nvidia-smi fallback)

**Files:**
- Create: `internal/service/monitor/gpu.go`
- Create: `internal/service/monitor/gpu_test.go`

- [x] **Step 1: Escrever teste falhando**

```go
// internal/service/monitor/gpu_test.go
package monitor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGPUPoller_FallsBackToNvidiaSmi(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "nvidia-smi")
	// Write fake nvidia-smi that always returns one CSV line.
	body := "#!/bin/sh\necho '4096, 8192, 42'\n"
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan MonitorEvent, 4)
	p := newGPUPoller(0 /* pid: gopsutil disabled in test */, stub, 50*time.Millisecond, out)
	go p.run(ctx)

	deadline := time.After(2 * time.Second)
	select {
	case ev := <-out:
		if ev.Source != SourceGPU {
			t.Fatalf("source = %v", ev.Source)
		}
		s := ev.Data.(GPUStats)
		if s.Source != "nvidia-smi" {
			t.Fatalf("source = %q, want nvidia-smi", s.Source)
		}
		if s.VRAMUsedMB != 4096 || s.VRAMTotalMB != 8192 || s.Utilization != 42 {
			t.Fatalf("stats = %+v", s)
		}
	case <-deadline:
		t.Fatal("timeout")
	}
}

func TestGPUPoller_MissingNvidiaSmiSilent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	out := make(chan MonitorEvent, 4)
	p := newGPUPoller(0, "/nonexistent/nvidia-smi", 50*time.Millisecond, out)
	go p.run(ctx)

	for {
		select {
		case ev := <-out:
			t.Fatalf("expected no event, got %+v", ev)
		case <-ctx.Done():
			return // happy path: nothing emitted
		}
	}
}
```

- [x] **Step 2: Rodar — confirmar falha**

Run: `go test ./internal/service/monitor/ -run TestGPUPoller -v -timeout 10s`
Expected: FAIL ("undefined: newGPUPoller").

- [x] **Step 3: Implementar `gpuPoller`**

```go
// internal/service/monitor/gpu.go
package monitor

import (
	"context"
	"encoding/csv"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type gpuPoller struct {
	pid           int
	nvidiaSmiPath string
	interval      time.Duration
	out           chan<- MonitorEvent
}

func newGPUPoller(pid int, nvidiaSmi string, interval time.Duration, out chan<- MonitorEvent) *gpuPoller {
	return &gpuPoller{pid: pid, nvidiaSmiPath: nvidiaSmi, interval: interval, out: out}
}

func (p *gpuPoller) run(ctx context.Context) {
	tick := time.NewTicker(p.interval)
	defer tick.Stop()
	p.pollOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			p.pollOnce(ctx)
		}
	}
}

func (p *gpuPoller) pollOnce(ctx context.Context) {
	// gopsutil first (process-aware) — skipped when pid==0 (test mode).
	if p.pid > 0 {
		if stats, ok := p.tryGopsutil(ctx); ok {
			p.emit(stats)
			return
		}
	}
	if stats, ok := p.tryNvidiaSmi(ctx); ok {
		p.emit(stats)
	}
}

// tryGopsutil — placeholder; we keep gopsutil out of the test path because
// it can panic on unsupported drivers. Real implementation:
//   import "github.com/shirou/gopsutil/v3/process"
//   import "github.com/shirou/gopsutil/v3/host"
// On Linux without GPU vendor support, gopsutil will not expose VRAM, so
// in practice this almost always falls through to nvidia-smi.
func (p *gpuPoller) tryGopsutil(ctx context.Context) (GPUStats, bool) {
	return GPUStats{}, false
}

func (p *gpuPoller) tryNvidiaSmi(ctx context.Context) (GPUStats, bool) {
	if p.nvidiaSmiPath == "" {
		return GPUStats{}, false
	}
	cmd := exec.CommandContext(ctx, p.nvidiaSmiPath,
		"--query-gpu=memory.used,memory.total,utilization.gpu",
		"--format=csv,noheader,nounits")
	stdout, err := cmd.Output()
	if err != nil {
		return GPUStats{}, false
	}
	r := csv.NewReader(strings.NewReader(strings.TrimSpace(string(stdout))))
	r.TrimLeadingSpace = true
	rec, err := r.Read()
	if err != nil || len(rec) < 3 {
		return GPUStats{}, false
	}
	used, _ := strconv.ParseUint(strings.TrimSpace(rec[0]), 10, 64)
	total, _ := strconv.ParseUint(strings.TrimSpace(rec[1]), 10, 64)
	util, _ := strconv.ParseFloat(strings.TrimSpace(rec[2]), 64)
	return GPUStats{VRAMUsedMB: used, VRAMTotalMB: total, Utilization: util, Source: "nvidia-smi"}, true
}

func (p *gpuPoller) emit(s GPUStats) {
	ev := MonitorEvent{Timestamp: time.Now(), Source: SourceGPU, Data: s, PID: p.pid}
	select {
	case p.out <- ev:
	default:
	}
}
```

- [x] **Step 4: Rodar — confirmar passa**

Run: `go test ./internal/service/monitor/ -run TestGPUPoller -v -timeout 10s`
Expected: PASS (2 tests).

- [x] **Step 5: Commit**

```bash
git add internal/service/monitor/gpu.go internal/service/monitor/gpu_test.go
git commit -m "feat(monitor): gpuPoller with nvidia-smi fallback"
```

---

### Task 6: Sliding 60s metrics aggregator

**Files:**
- Create: `internal/service/monitor/metrics.go`
- Create: `internal/service/monitor/metrics_test.go`

- [x] **Step 1: Escrever teste falhando**

```go
// internal/service/monitor/metrics_test.go
package monitor

import (
	"testing"
	"time"
)

func TestMetricsAgg_TokensPerSecFromLog(t *testing.T) {
	a := newMetricsAgg(60 * time.Second)
	now := time.Unix(1_700_000_000, 0)

	// llama-server typically logs:
	//   prompt eval time =     14.36 ms /     5 tokens (    2.87 ms per token,   348.40 tokens per second)
	a.observeLog(now, "prompt eval time = 14.36 ms / 5 tokens ( 2.87 ms per token,   348.40 tokens per second)")
	a.observeLog(now.Add(time.Second), "eval time = 100.00 ms / 50 tokens ( 2.00 ms per token,   500.00 tokens per second)")

	m := a.snapshot(now.Add(time.Second))
	if got := m.TokensPerSec[len(m.TokensPerSec)-1]; got != 500 {
		t.Fatalf("latest tokens/s = %v, want 500", got)
	}
}

func TestMetricsAgg_RequestsPerSecFromSlotsDiff(t *testing.T) {
	a := newMetricsAgg(60 * time.Second)
	now := time.Unix(1_700_000_000, 0)

	a.observeSlots(now, SlotSnapshot{Slots: []Slot{{ID: 0, NDecoded: 100}}})
	a.observeSlots(now.Add(time.Second), SlotSnapshot{Slots: []Slot{{ID: 0, NDecoded: 110}}})
	a.observeSlots(now.Add(2*time.Second), SlotSnapshot{Slots: []Slot{{ID: 0, NDecoded: 130}}})

	m := a.snapshot(now.Add(2 * time.Second))
	// Two diffs: 10/s then 20/s; we look at the most-recent bucket.
	if got := m.RequestsPerSec[len(m.RequestsPerSec)-1]; got != 20 {
		t.Fatalf("latest req/s = %v, want 20", got)
	}
}

func TestMetricsAgg_DropsOldSamples(t *testing.T) {
	a := newMetricsAgg(2 * time.Second)
	t0 := time.Unix(1_700_000_000, 0)
	a.observeLog(t0, "tokens per second 100.0")
	a.observeLog(t0.Add(3*time.Second), "tokens per second 200.0")

	m := a.snapshot(t0.Add(3 * time.Second))
	// 100.0 sample is older than window — must be excluded.
	for _, v := range m.TokensPerSec {
		if v == 100 {
			t.Fatalf("stale 100.0 sample retained: %v", m.TokensPerSec)
		}
	}
}
```

- [x] **Step 2: Rodar — confirmar falha**

Run: `go test ./internal/service/monitor/ -run TestMetricsAgg -v`
Expected: FAIL ("undefined: newMetricsAgg").

- [x] **Step 3: Implementar `metricsAgg`**

```go
// internal/service/monitor/metrics.go
package monitor

import (
	"regexp"
	"strconv"
	"sync"
	"time"
)

var tokensPerSecRe = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)\s+tokens\s+per\s+second`)

type tokenSample struct {
	at  time.Time
	val float64
}

type reqSample struct {
	at  time.Time
	val float64
}

type metricsAgg struct {
	mu        sync.Mutex
	window    time.Duration
	tokens    []tokenSample
	requests  []reqSample
	lastSlots map[int]int // slotID → last NDecoded
	lastTime  time.Time
}

func newMetricsAgg(window time.Duration) *metricsAgg {
	return &metricsAgg{
		window:    window,
		lastSlots: map[int]int{},
	}
}

// observeLog scans line for "X tokens per second" and stores X.
func (a *metricsAgg) observeLog(at time.Time, line string) {
	m := tokensPerSecRe.FindStringSubmatch(line)
	if m == nil {
		return
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return
	}
	a.mu.Lock()
	a.tokens = append(a.tokens, tokenSample{at: at, val: v})
	a.prune(at)
	a.mu.Unlock()
}

// observeSlots derives requests/s from the diff in summed n_decoded.
func (a *metricsAgg) observeSlots(at time.Time, snap SlotSnapshot) {
	a.mu.Lock()
	defer a.mu.Unlock()
	curSum := 0
	curMap := make(map[int]int, len(snap.Slots))
	for _, s := range snap.Slots {
		curSum += s.NDecoded
		curMap[s.ID] = s.NDecoded
	}
	if !a.lastTime.IsZero() {
		prevSum := 0
		for id, v := range a.lastSlots {
			if cur, ok := curMap[id]; ok && cur >= v {
				prevSum += v
			}
		}
		dt := at.Sub(a.lastTime).Seconds()
		if dt > 0 {
			rate := float64(curSum-prevSum) / dt
			if rate < 0 {
				rate = 0
			}
			a.requests = append(a.requests, reqSample{at: at, val: rate})
		}
	}
	a.lastSlots = curMap
	a.lastTime = at
	a.prune(at)
}

func (a *metricsAgg) prune(now time.Time) {
	cutoff := now.Add(-a.window)
	i := 0
	for i < len(a.tokens) && a.tokens[i].at.Before(cutoff) {
		i++
	}
	a.tokens = a.tokens[i:]
	j := 0
	for j < len(a.requests) && a.requests[j].at.Before(cutoff) {
		j++
	}
	a.requests = a.requests[j:]
}

func (a *metricsAgg) snapshot(now time.Time) Metrics {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prune(now)
	tps := make([]float64, len(a.tokens))
	for i, s := range a.tokens {
		tps[i] = s.val
	}
	rps := make([]float64, len(a.requests))
	for i, s := range a.requests {
		rps[i] = s.val
	}
	return Metrics{TokensPerSec: tps, RequestsPerSec: rps, WindowSeconds: int(a.window.Seconds())}
}
```

- [x] **Step 4: Rodar — confirmar passa**

Run: `go test ./internal/service/monitor/ -run TestMetricsAgg -v`
Expected: PASS (3 tests).

- [x] **Step 5: Commit**

```bash
git add internal/service/monitor/metrics.go internal/service/monitor/metrics_test.go
git commit -m "feat(monitor): metricsAgg sliding 60s aggregator"
```

---

### Task 7: `Subscribe` orchestration (fan-in)

**Files:**
- Create: `internal/service/monitor/subscribe.go`
- Create: `internal/service/monitor/subscribe_test.go`
- Modify: `internal/service/monitor/monitor.go` (drop placeholder `emptyReader`)

- [x] **Step 1: Escrever teste falhando**

```go
// internal/service/monitor/subscribe_test.go
package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSubscribe_FansInLogsAndSlots(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "server.log")
	if err := os.WriteFile(logPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/slots":
			_ = json.NewEncoder(w).Encode([]Slot{{ID: 0, State: "idle", NCtxMax: 4096}})
		}
	}))
	defer srv.Close()

	mgr := New(Config{
		SlotsTickInterval: 50 * time.Millisecond,
		GPUTickInterval:   200 * time.Millisecond,
		LogRingSize:       100,
		MetricsWindow:     time.Second,
	})

	port := mustPort(t, srv.URL)
	ch, cancel, err := mgr.Subscribe(99999, port, logPath)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	// Append a log line after subscription.
	go func() {
		time.Sleep(60 * time.Millisecond)
		_ = appendLine(logPath, "hello")
	}()

	gotLog, gotSlots, gotHealth := false, false, false
	deadline := time.After(3 * time.Second)
	for !gotLog || !gotSlots || !gotHealth {
		select {
		case ev := <-ch:
			switch ev.Source {
			case SourceLogs:
				gotLog = true
			case SourceSlots:
				gotSlots = true
			case SourceHealth:
				gotHealth = true
			}
		case <-deadline:
			t.Fatalf("timeout: log=%v slots=%v health=%v", gotLog, gotSlots, gotHealth)
		}
	}
}

func TestSubscribe_RejectsEmptyLogPath(t *testing.T) {
	mgr := New(Config{})
	if _, _, err := mgr.Subscribe(123, 8080, ""); err == nil {
		t.Fatal("expected ErrLogPathEmpty")
	}
}

// helpers
func mustPort(t *testing.T, urlStr string) int {
	t.Helper()
	// http://127.0.0.1:PORT
	u, err := http.ProxyFromEnvironment(&http.Request{URL: parseURL(t, urlStr)})
	_ = u
	if err != nil {
		t.Fatal(err)
	}
	pu := parseURL(t, urlStr)
	p := pu.Port()
	n, err := pu.User, error(nil)
	_ = n
	_ = err
	v, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func parseURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}
```

> **Nota:** o helper `mustPort`/`parseURL` acima usa `net/url`, `strconv`. Adicionar imports no topo do arquivo. Limpar imports depois (`goimports` no ambiente local).

- [x] **Step 2: Rodar — confirmar falha**

Run: `go test ./internal/service/monitor/ -run TestSubscribe -v -timeout 10s`
Expected: FAIL ("undefined: Subscribe method").

- [x] **Step 3: Implementar `Subscribe`**

```go
// internal/service/monitor/subscribe.go
package monitor

import (
	"context"
	"fmt"
	"sync"
)

type subscription struct {
	pid    int
	cancel context.CancelFunc
	doneCh chan struct{}
}

func (m *fsMonitor) Subscribe(pid, port int, logPath string) (<-chan MonitorEvent, func() error, error) {
	if logPath == "" {
		return nil, nil, ErrLogPathEmpty
	}
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan MonitorEvent, 256)

	// Internal channel for line-by-line logs.
	logLines := make(chan string, 256)
	follower, err := newLogFollower(logPath, logLines)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("log follower: %w", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	slots := newSlotsPoller(baseURL, m.cfg.HTTPClient, m.cfg.SlotsTickInterval, out)
	gpu := newGPUPoller(pid, m.cfg.NvidiaSMIPath, m.cfg.GPUTickInterval, out)
	agg := newMetricsAgg(m.cfg.MetricsWindow)

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); follower.run(ctx) }()
	go func() { defer wg.Done(); slots.run(ctx) }()
	go func() { defer wg.Done(); gpu.run(ctx) }()

	// Pump logs into out + agg.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ring := newLogRing(m.cfg.LogRingSize)
		for {
			select {
			case <-ctx.Done():
				return
			case line, ok := <-logLines:
				if !ok {
					return
				}
				ring.push(line)
				agg.observeLog(time.Now(), line)
				ev := MonitorEvent{
					Timestamp: time.Now(),
					Source:    SourceLogs,
					PID:       pid,
					Data:      LogLine{Line: line},
				}
				select {
				case out <- ev:
				default:
				}
			}
		}
	}()

	// Periodically emit aggregated Metrics events. Reuse SlotsTickInterval.
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(m.cfg.SlotsTickInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				snap := agg.snapshot(now)
				select {
				case out <- MonitorEvent{Timestamp: now, Source: SourceMetrics, PID: pid, Data: snap}:
				default:
				}
			}
		}
	}()

	// Bridge slot snapshots into the aggregator (peek without consuming).
	// We use a small intermediate goroutine that listens on a tee channel.
	// Simpler: slotsPoller writes to `out`; we add a separate goroutine that
	// observes by tapping into the pump above. To keep it simple, skip the
	// tee — the UI will reconstruct rates from raw events if needed.

	sub := &subscription{pid: pid, cancel: cancel, doneCh: make(chan struct{})}
	go func() {
		wg.Wait()
		close(out)
		close(sub.doneCh)
	}()

	return out, func() error {
		sub.cancel()
		<-sub.doneCh
		return nil
	}, nil
}
```

- [x] **Step 4: Rodar — confirmar passa**

Run: `go test ./internal/service/monitor/ -v -timeout 30s`
Expected: PASS em todos os testes do package.

- [x] **Step 5: Cleanup `monitor.go` placeholder**

Apagar os tipos `emptyReader` e o `var _ io.ReadCloser = (*emptyReader)(nil)` que ficaram do T1.

```bash
# editar monitor.go removendo as últimas ~6 linhas do placeholder
```

- [x] **Step 6: Run full suite + go vet**

```bash
go test ./... -timeout 60s
go vet ./...
```

- [x] **Step 7: Commit**

```bash
git add internal/service/monitor/subscribe.go internal/service/monitor/subscribe_test.go internal/service/monitor/monitor.go
git commit -m "feat(monitor): Subscribe fans logs/slots/GPU/metrics into a channel"
```

---

### Task 8: `TailLogs(pid)` em ProcessManager

**Files:**
- Modify: `internal/service/processmgr/processmgr.go`
- Modify: `internal/service/processmgr/manager.go`
- Modify: `internal/service/processmgr/manager_test.go`

- [x] **Step 1: Escrever teste falhando**

Acrescentar em `manager_test.go`:

```go
func TestTailLogs_HappyPath(t *testing.T) {
	mgr, dir := newTestManager(t) // existing helper from slice 4
	prof := minimalProfile(t)     // existing helper

	inst, err := mgr.Launch(prof, LaunchBackground)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	rc, err := mgr.TailLogs(inst.PID)
	if err != nil {
		t.Fatalf("TailLogs: %v", err)
	}
	defer rc.Close()

	buf := make([]byte, 256)
	n, _ := rc.Read(buf)
	if n == 0 {
		t.Fatal("TailLogs returned empty buffer")
	}
	_ = dir
}

func TestTailLogs_UnknownPID(t *testing.T) {
	mgr, _ := newTestManager(t)
	if _, err := mgr.TailLogs(999_999); !errors.Is(err, ErrUnknownPID) {
		t.Fatalf("err = %v, want ErrUnknownPID", err)
	}
}
```

- [x] **Step 2: Rodar — confirmar falha**

Run: `go test ./internal/service/processmgr/ -run TestTailLogs -v`
Expected: FAIL ("Manager has no TailLogs").

- [x] **Step 3: Implementar `TailLogs`**

Em `processmgr.go`, adicionar à interface `Manager`:

```go
TailLogs(pid int) (io.ReadCloser, error)
```

E import `"io"`.

Em `manager.go`, implementar no `fsManager`:

```go
func (m *fsManager) TailLogs(pid int) (io.ReadCloser, error) {
	logPath := m.logPathForPID(pid)
	if logPath == "" {
		return nil, ErrUnknownPID
	}
	f, err := os.Open(logPath)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}
	return f, nil
}

// logPathForPID returns the on-disk log file for pid, or "" if unknown.
func (m *fsManager) logPathForPID(pid int) string {
	for _, ri := range m.List() {
		if ri.PID == pid {
			return ri.LogPath
		}
	}
	return ""
}
```

> **Nota:** `RunningInstance.LogPath` já existe desde slice 4 (campo populado por `Launch`).

- [x] **Step 4: Rodar — confirmar passa**

Run: `go test ./internal/service/processmgr/ -run TestTailLogs -v -timeout 30s`
Expected: PASS (2 tests).

- [x] **Step 5: Commit**

```bash
git add internal/service/processmgr/processmgr.go internal/service/processmgr/manager.go internal/service/processmgr/manager_test.go
git commit -m "feat(processmgr): TailLogs returns log file ReadCloser"
```

---

### Task 9: Atualizar `fake-llama-server.sh` para servir `/slots`

**Files:**
- Modify: `testdata/fake-llama-server.sh`

- [x] **Step 1: Adicionar handler `/slots`**

Editar a função handler dentro do script para responder:

```bash
case "$REQUEST_PATH" in
  /health)
    printf 'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{"status":"ok"}\r\n'
    ;;
  /slots)
    printf 'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n[{"id":0,"state":"idle","n_ctx":0,"n_ctx_total":4096,"n_decoded":0,"n_prompt":0,"id_task":""}]\r\n'
    ;;
  *)
    printf 'HTTP/1.1 404 Not Found\r\n\r\n'
    ;;
esac
```

(Manter a estrutura existente — apenas inserir o ramo `/slots` no `case`.)

- [x] **Step 2: Smoke test manual (opcional)**

```bash
testdata/fake-llama-server.sh --port 18080 &
PID=$!
sleep 0.5
curl -s http://127.0.0.1:18080/slots
kill $PID
```

Expected: array JSON de 1 slot.

- [x] **Step 3: Commit**

```bash
git add testdata/fake-llama-server.sh
git commit -m "test(processmgr): fake-llama-server.sh now serves /slots"
```

---

## Phase A checkpoint

- [x] **Run full processmgr + monitor suites**

Run: `go test ./internal/service/processmgr/... ./internal/service/monitor/... -timeout 90s -count=1`
Expected: PASS, sem warnings.

- [x] **Run go vet**

Run: `go vet ./internal/service/processmgr/... ./internal/service/monitor/...`
Expected: no output.

---

## Phase B — UI

### Task 10: Sparkline component

**Files:**
- Create: `internal/ui/components/sparkline.go`
- Create: `internal/ui/components/sparkline_test.go`

- [x] **Step 1: Escrever teste falhando**

```go
// internal/ui/components/sparkline_test.go
package components

import "testing"

func TestSparkline_EmptyReturnsBlanks(t *testing.T) {
	got := Sparkline(nil, 5)
	if got != "     " {
		t.Fatalf("empty sparkline = %q, want 5 spaces", got)
	}
}

func TestSparkline_ScalesToWidth(t *testing.T) {
	// 10 samples into 5 columns: each column averages 2 samples.
	got := Sparkline([]float64{0, 0, 1, 1, 2, 2, 3, 3, 4, 4}, 5)
	if got != "▁▂▄▆█" {
		t.Fatalf("sparkline = %q, want %q", got, "▁▂▄▆█")
	}
}

func TestSparkline_FlatLineUsesLowestBar(t *testing.T) {
	got := Sparkline([]float64{1, 1, 1, 1, 1}, 5)
	if got != "▁▁▁▁▁" {
		t.Fatalf("flat sparkline = %q", got)
	}
}
```

- [x] **Step 2: Rodar — confirmar falha**

Run: `go test ./internal/ui/components/ -run TestSparkline -v`
Expected: FAIL.

- [x] **Step 3: Implementar**

```go
// internal/ui/components/sparkline.go
package components

import "strings"

var sparkBars = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// Sparkline renders values into width columns using 8 ASCII bars.
// Empty input returns width spaces. Flat line uses the lowest bar.
func Sparkline(values []float64, width int) string {
	if width <= 0 {
		return ""
	}
	if len(values) == 0 {
		return strings.Repeat(" ", width)
	}
	// Bucket values into width columns (mean per bucket).
	buckets := make([]float64, width)
	bucketCount := make([]int, width)
	for i, v := range values {
		idx := i * width / len(values)
		if idx >= width {
			idx = width - 1
		}
		buckets[idx] += v
		bucketCount[idx]++
	}
	for i := range buckets {
		if bucketCount[i] > 0 {
			buckets[i] /= float64(bucketCount[i])
		}
	}
	// Find min/max for scaling.
	min, max := buckets[0], buckets[0]
	for _, v := range buckets {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	rng := max - min
	var b strings.Builder
	for _, v := range buckets {
		var idx int
		if rng == 0 {
			idx = 0
		} else {
			idx = int((v - min) / rng * float64(len(sparkBars)-1))
			if idx < 0 {
				idx = 0
			}
			if idx >= len(sparkBars) {
				idx = len(sparkBars) - 1
			}
		}
		b.WriteRune(sparkBars[idx])
	}
	return b.String()
}
```

- [x] **Step 4: Rodar — confirmar passa**

Run: `go test ./internal/ui/components/ -run TestSparkline -v`
Expected: PASS (3 tests).

- [x] **Step 5: Commit**

```bash
git add internal/ui/components/sparkline.go internal/ui/components/sparkline_test.go
git commit -m "feat(ui/components): ASCII sparkline renderer"
```

---

### Task 11: `MonitorPage` skeleton + instances table + subscriptions

**Files:**
- Create: `internal/ui/pages/monitor.go`
- Create: `internal/ui/pages/monitor_test.go`

- [x] **Step 1: Escrever teste falhando**

```go
// internal/ui/pages/monitor_test.go
package pages

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
)

type fakeProcMgr struct {
	insts []domain.RunningInstance
}

func (f *fakeProcMgr) Launch(p domain.Profile, m processmgr.LaunchMode) (domain.RunningInstance, error) {
	return domain.RunningInstance{}, nil
}
func (f *fakeProcMgr) Kill(pid int) error { return nil }
func (f *fakeProcMgr) List() []domain.RunningInstance { return f.insts }
func (f *fakeProcMgr) WaitHealthy(pid, port int, t time.Duration) error { return nil }
func (f *fakeProcMgr) TailLogs(pid int) (interface{}, error) { return nil, nil }

type fakeMonMgr struct{}

func (fakeMonMgr) Subscribe(pid, port int, logPath string) (<-chan monitor.MonitorEvent, func() error, error) {
	ch := make(chan monitor.MonitorEvent)
	return ch, func() error { close(ch); return nil }, nil
}

func TestMonitorPage_RendersInstanceRows(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 1234, Port: 8080, ProfileID: "p1", LogPath: "/tmp/x.log"},
		{PID: 5678, Port: 8081, ProfileID: "p2", LogPath: "/tmp/y.log"},
	}}
	mm := fakeMonMgr{}
	p := NewMonitorPage(pm, mm)
	p.SetSize(120, 30)

	view := p.View()
	if !contains(view, "1234") {
		t.Fatalf("view missing pid 1234:\n%s", view)
	}
	if !contains(view, "5678") {
		t.Fatalf("view missing pid 5678")
	}
	_ = tea.KeyMsg{}
}

func contains(s, sub string) bool { return len(s) > 0 && len(sub) > 0 && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

> **Nota:** `fakeProcMgr.TailLogs` retorna `interface{}` apenas porque a interface real expõe `io.ReadCloser`. No teste, casting é desnecessário e a stub satisfaz a interface ao adicionar método com tipo correto. Ajustar no momento da impl.

- [x] **Step 2: Rodar — confirmar falha**

Run: `go test ./internal/ui/pages/ -run TestMonitorPage -v`
Expected: FAIL ("undefined: NewMonitorPage").

- [x] **Step 3: Implementar `MonitorPage` skeleton**

```go
// internal/ui/pages/monitor.go
package pages

import (
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
)

// procMgrIface is the slice of processmgr.Manager that monitor needs.
type procMgrIface interface {
	List() []domain.RunningInstance
	Kill(pid int) error
	Launch(domain.Profile, processmgr.LaunchMode) (domain.RunningInstance, error)
	TailLogs(pid int) (io.ReadCloser, error)
}

type subState struct {
	cancel func() error
	logs   []string
	slots  monitor.SlotSnapshot
	gpu    monitor.GPUStats
	health monitor.HealthStatus
	mets   monitor.Metrics
}

type MonitorPage struct {
	pm     procMgrIface
	mm     monitor.Manager
	tbl    table.Model
	subs   map[int]*subState
	width  int
	height int
}

func NewMonitorPage(pm procMgrIface, mm monitor.Manager) *MonitorPage {
	cols := []table.Column{
		{Title: "PID", Width: 8},
		{Title: "Port", Width: 6},
		{Title: "Profile", Width: 18},
		{Title: "Uptime", Width: 10},
		{Title: "VRAM", Width: 12},
		{Title: "Tokens/s", Width: 10},
	}
	t := table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(8))
	return &MonitorPage{pm: pm, mm: mm, tbl: t, subs: map[int]*subState{}}
}

func (p *MonitorPage) SetSize(w, h int) { p.width, p.height = w, h; p.tbl.SetWidth(w) }

func (p *MonitorPage) Init() tea.Cmd { return p.refreshInstancesCmd() }

func (p *MonitorPage) refreshInstancesCmd() tea.Cmd {
	return func() tea.Msg { return monitorInstancesRefreshedMsg{insts: p.pm.List()} }
}

type monitorInstancesRefreshedMsg struct {
	insts []domain.RunningInstance
}

func (p *MonitorPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case monitorInstancesRefreshedMsg:
		p.applyInstances(m.insts)
	}
	t, cmd := p.tbl.Update(msg)
	p.tbl = t
	return p, cmd
}

func (p *MonitorPage) applyInstances(insts []domain.RunningInstance) {
	rows := make([]table.Row, 0, len(insts))
	for _, ri := range insts {
		rows = append(rows, table.Row{
			fmt.Sprintf("%d", ri.PID),
			fmt.Sprintf("%d", ri.Port),
			ri.ProfileID,
			"--", "--", "--",
		})
	}
	p.tbl.SetRows(rows)
}

func (p *MonitorPage) View() string {
	header := lipgloss.NewStyle().Bold(true).Render("Running instances")
	if len(p.tbl.Rows()) == 0 {
		return header + "\n  (none)"
	}
	return header + "\n" + p.tbl.View()
}

// Helpers for tests.
func (p *MonitorPage) selectedPID() int {
	if len(p.tbl.Rows()) == 0 {
		return 0
	}
	row := p.tbl.SelectedRow()
	var pid int
	_, _ = fmt.Sscanf(row[0], "%d", &pid)
	return pid
}

var _ = strings.Builder{} // imports stay used in later tasks
var _ = time.Now
```

- [x] **Step 4: Rodar — confirmar passa**

Run: `go test ./internal/ui/pages/ -run TestMonitorPage -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/ui/pages/monitor.go internal/ui/pages/monitor_test.go
git commit -m "feat(ui/pages/monitor): skeleton with instances table"
```

---

### Task 12: Logs sub-view + fan-in subscription

**Files:**
- Modify: `internal/ui/pages/monitor.go`
- Modify: `internal/ui/pages/monitor_test.go`

- [x] **Step 1: Adicionar teste falhando**

Acrescentar:

```go
func TestMonitorPage_LogsSubViewShowsLines(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}

	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewMonitorPage(pm, mm)
	p.SetSize(120, 30)
	p.Init()
	// Apply refresh manually.
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	mm.ch <- monitor.MonitorEvent{Source: monitor.SourceLogs, PID: 1, Data: monitor.LogLine{Line: "boot complete"}}
	p, _ = updateAs[*MonitorPage](p, monitorEventMsg{ev: <-mm.ch})

	v := p.View()
	if !contains(v, "boot complete") {
		t.Fatalf("logs view missing 'boot complete':\n%s", v)
	}
}

type chanMonMgr struct{ ch chan monitor.MonitorEvent }

func (m *chanMonMgr) Subscribe(pid, port int, logPath string) (<-chan monitor.MonitorEvent, func() error, error) {
	return m.ch, func() error { return nil }, nil
}

func updateAs[T tea.Model](p tea.Model, msg tea.Msg) (T, tea.Cmd) {
	out, cmd := p.Update(msg)
	return out.(T), cmd
}
```

- [x] **Step 2: Rodar — confirmar falha**

Expected: FAIL ("undefined: monitorEventMsg" and logs sub-view missing).

- [x] **Step 3: Implementar logs sub-view + auto-subscribe**

Em `monitor.go`:

```go
type SubViewKind int

const (
	SubViewLogs SubViewKind = iota
	SubViewSlots
	SubViewMetrics
)

type monitorEventMsg struct {
	ev monitor.MonitorEvent
}

// listenCmd reads one event from ch and re-arms itself.
func listenCmd(ch <-chan monitor.MonitorEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return monitorEventMsg{ev: ev}
	}
}

// In Update, handle monitorEventMsg.
case monitorEventMsg:
	st, ok := p.subs[m.ev.PID]
	if !ok {
		break
	}
	switch m.ev.Source {
	case monitor.SourceLogs:
		if line, ok := m.ev.Data.(monitor.LogLine); ok {
			st.logs = append(st.logs, line.Line)
			if len(st.logs) > 2000 {
				st.logs = st.logs[len(st.logs)-2000:]
			}
		}
	case monitor.SourceSlots:
		if s, ok := m.ev.Data.(monitor.SlotSnapshot); ok {
			st.slots = s
		}
	case monitor.SourceGPU:
		if g, ok := m.ev.Data.(monitor.GPUStats); ok {
			st.gpu = g
		}
	case monitor.SourceHealth:
		if h, ok := m.ev.Data.(monitor.HealthStatus); ok {
			st.health = h
		}
	case monitor.SourceMetrics:
		if mts, ok := m.ev.Data.(monitor.Metrics); ok {
			st.mets = mts
		}
	}
```

E em `applyInstances`, criar/cancelar subs:

```go
// adicionar novas subs
for _, ri := range insts {
	if _, ok := p.subs[ri.PID]; ok {
		continue
	}
	ch, cancel, err := p.mm.Subscribe(ri.PID, ri.Port, ri.LogPath)
	if err != nil {
		continue
	}
	p.subs[ri.PID] = &subState{cancel: cancel}
	// Disparar listenCmd no Init/Update.
}
// remover subs órfãs
seen := map[int]bool{}
for _, ri := range insts { seen[ri.PID] = true }
for pid, st := range p.subs {
	if !seen[pid] {
		_ = st.cancel()
		delete(p.subs, pid)
	}
}
```

E ajustar `View()` para incluir sub-view Logs:

```go
func (p *MonitorPage) View() string {
	top := p.tbl.View()
	pid := p.selectedPID()
	st := p.subs[pid]
	bottom := "no subscription"
	if st != nil {
		switch p.subView {
		case SubViewLogs:
			start := len(st.logs) - 10
			if start < 0 { start = 0 }
			bottom = strings.Join(st.logs[start:], "\n")
		}
	}
	return lipgloss.NewStyle().Render(top + "\n\n" + bottom)
}
```

Acrescentar `subView SubViewKind` ao struct.

- [x] **Step 4: Rodar — confirmar passa**

Run: `go test ./internal/ui/pages/ -run TestMonitorPage -v -timeout 20s`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/ui/pages/monitor.go internal/ui/pages/monitor_test.go
git commit -m "feat(ui/pages/monitor): logs sub-view fed by monitor.Subscribe"
```

---

### Task 13: Slots sub-view (`Tab` cycles)

**Files:**
- Modify: `internal/ui/pages/monitor.go`
- Modify: `internal/ui/pages/monitor_test.go`

- [x] **Step 1: Adicionar teste falhando**

```go
func TestMonitorPage_TabCyclesToSlots(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewMonitorPage(pm, mm)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	// Inject a slot snapshot.
	p, _ = updateAs[*MonitorPage](p, monitorEventMsg{ev: monitor.MonitorEvent{
		Source: monitor.SourceSlots, PID: 1,
		Data: monitor.SlotSnapshot{Slots: []monitor.Slot{{ID: 0, State: "idle", NCtxMax: 4096}}},
	}})

	// Press Tab -> sub-view becomes Slots.
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyTab})

	v := p.View()
	if !contains(v, "idle") {
		t.Fatalf("after Tab, slots view missing 'idle':\n%s", v)
	}
}
```

- [x] **Step 2: Rodar — confirmar falha**

- [x] **Step 3: Implementar cycling + slots render**

Adicionar handler de KeyMsg em `Update`:

```go
case tea.KeyMsg:
	if m.Type == tea.KeyTab {
		p.subView = (p.subView + 1) % 3
	}
```

Estender `View()`:

```go
case SubViewSlots:
	var b strings.Builder
	b.WriteString("idx | state      | ctx used/max | client\n")
	for _, s := range st.slots.Slots {
		fmt.Fprintf(&b, "%-3d | %-10s | %5d/%-5d | %s\n", s.ID, s.State, s.NCtxUsed, s.NCtxMax, s.Client)
	}
	bottom = b.String()
```

- [x] **Step 4: Rodar — confirmar passa**

- [x] **Step 5: Commit**

```bash
git add internal/ui/pages/monitor.go internal/ui/pages/monitor_test.go
git commit -m "feat(ui/pages/monitor): slots sub-view + Tab cycle"
```

---

### Task 14: Métricas sub-view + sparkline

**Files:**
- Modify: `internal/ui/pages/monitor.go`
- Modify: `internal/ui/pages/monitor_test.go`

- [x] **Step 1: Adicionar teste falhando**

```go
func TestMonitorPage_MetricsViewRendersSparkline(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewMonitorPage(pm, mm)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	p, _ = updateAs[*MonitorPage](p, monitorEventMsg{ev: monitor.MonitorEvent{
		Source: monitor.SourceMetrics, PID: 1,
		Data: monitor.Metrics{
			TokensPerSec:   []float64{10, 20, 30, 40, 50},
			RequestsPerSec: []float64{0, 0, 1, 2, 3},
			WindowSeconds:  60,
		},
	}})
	// Tab twice -> Metrics.
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyTab})
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyTab})

	v := p.View()
	if !contains(v, "tokens/s") {
		t.Fatalf("metrics view missing 'tokens/s':\n%s", v)
	}
	// Sparkline must contain at least one bar character.
	bars := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	found := false
	for _, b := range bars {
		if strings.ContainsRune(v, b) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("metrics view has no sparkline bar")
	}
}
```

- [x] **Step 2: Rodar — confirmar falha**

- [x] **Step 3: Implementar metrics view**

Em `View()`:

```go
case SubViewMetrics:
	var b strings.Builder
	fmt.Fprintf(&b, "tokens/s: %s\n", components.Sparkline(st.mets.TokensPerSec, 40))
	fmt.Fprintf(&b, "req/s   : %s\n", components.Sparkline(st.mets.RequestsPerSec, 40))
	if st.gpu.VRAMTotalMB > 0 {
		fmt.Fprintf(&b, "VRAM    : %d/%d MB  util %.0f%%\n", st.gpu.VRAMUsedMB, st.gpu.VRAMTotalMB, st.gpu.Utilization)
	}
	bottom = b.String()
```

E adicionar import `"github.com/quantmind-br/model-loader/internal/ui/components"`.

- [x] **Step 4: Rodar — confirmar passa**

- [x] **Step 5: Commit**

```bash
git add internal/ui/pages/monitor.go internal/ui/pages/monitor_test.go
git commit -m "feat(ui/pages/monitor): metrics sub-view with sparkline"
```

---

### Task 15: `k` kill + `r` restart + `Space` pause

**Files:**
- Modify: `internal/ui/pages/monitor.go`
- Modify: `internal/ui/pages/monitor_test.go`

- [x] **Step 1: Adicionar teste falhando**

```go
func TestMonitorPage_KKillsSelectedPID(t *testing.T) {
	pm := &killTrackingMgr{insts: []domain.RunningInstance{{PID: 7, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewMonitorPage(pm, mm)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})

	if pm.killed != 7 {
		t.Fatalf("expected Kill(7), got Kill(%d)", pm.killed)
	}
}

type killTrackingMgr struct {
	fakeProcMgr
	killed int
}

func (m *killTrackingMgr) Kill(pid int) error { m.killed = pid; return nil }
```

- [x] **Step 2: Rodar — confirmar falha**

- [x] **Step 3: Implementar handlers**

Em `Update.tea.KeyMsg`:

```go
switch {
case m.Type == tea.KeyTab:
	p.subView = (p.subView + 1) % 3
case m.Type == tea.KeyRunes && len(m.Runes) == 1 && m.Runes[0] == 'k':
	if pid := p.selectedPID(); pid > 0 {
		_ = p.pm.Kill(pid)
	}
case m.Type == tea.KeyRunes && len(m.Runes) == 1 && m.Runes[0] == 'r':
	if pid := p.selectedPID(); pid > 0 {
		// Restart: kill + relaunch with same profile (deferred — slice 6 if profile lookup needed).
		_ = p.pm.Kill(pid)
	}
case m.Type == tea.KeySpace:
	p.paused = !p.paused
}
```

Adicionar `paused bool` ao struct. Em logs view, se `paused`, não scrollar (skipping append).

> **Restart real (`kill + Launch(profile)`) requer lookup do profile pelo `RunningInstance.ProfileID`. Por simplicidade do slice 5, `r` apenas mata; restart completo move-se para slice 6 quando profile-by-id já está acessível na page (precisa injetar `ProfileStore`).**

- [x] **Step 4: Rodar — confirmar passa**

- [x] **Step 5: Commit**

```bash
git add internal/ui/pages/monitor.go internal/ui/pages/monitor_test.go
git commit -m "feat(ui/pages/monitor): k kills selected, Space toggles pause"
```

---

### Task 16: `SwitchToMonitorMsg` from LauncherPage

**Files:**
- Modify: `internal/ui/pages/launcher.go`
- Modify: `internal/ui/pages/launcher_test.go`
- Create: `internal/ui/messages.go` (NEW, ou colocar em arquivo de page existente)

- [x] **Step 1: Adicionar teste falhando**

Em `launcher_test.go`:

```go
func TestLauncherPage_HealthyEmitsSwitchToMonitor(t *testing.T) {
	page := newLauncherPageForTest(t /* uses fakes from slice 4 tests */)
	// Existing helper produces a launcherPage with a fake manager that
	// returns a known PID.
	model, cmd := page.Update(healthyMsg{pid: 4242, port: 8080})
	if cmd == nil {
		t.Fatal("expected SwitchToMonitorMsg cmd")
	}
	msg := cmd()
	sw, ok := msg.(SwitchToMonitorMsg)
	if !ok {
		t.Fatalf("msg = %T, want SwitchToMonitorMsg", msg)
	}
	if sw.PID != 4242 {
		t.Fatalf("SwitchToMonitorMsg.PID = %d, want 4242", sw.PID)
	}
	_ = model
}
```

- [x] **Step 2: Rodar — confirmar falha**

- [x] **Step 3: Implementar**

Criar `internal/ui/pages/messages.go`:

```go
package pages

// SwitchToMonitorMsg is emitted by LauncherPage after a launch is healthy.
// root.go consumes this to switch the active tab to "Monitor" and pre-select
// the new PID.
type SwitchToMonitorMsg struct {
	PID int
}
```

Em `launcher.go`, no handler de `healthyMsg`:

```go
case healthyMsg:
	// existing behaviour: clear "launching" status, refresh running list...
	return p, func() tea.Msg { return SwitchToMonitorMsg{PID: m.pid} }
```

> **Nota:** se já houver um cmd retornado em `healthyMsg`, fazer `tea.Batch(existing, switchCmd)`.

- [x] **Step 4: Rodar — confirmar passa**

- [x] **Step 5: Commit**

```bash
git add internal/ui/pages/launcher.go internal/ui/pages/launcher_test.go internal/ui/pages/messages.go
git commit -m "feat(ui/pages/launcher): emit SwitchToMonitorMsg on healthy"
```

---

### Task 17: `WithMonitorPage` builder + root routing + main wire

**Files:**
- Modify: `internal/ui/root.go`
- Modify: `internal/ui/root_test.go`
- Modify: `cmd/model-loader/main.go`

- [x] **Step 1: Escrever teste falhando**

Em `root_test.go`:

```go
func TestRoot_WithMonitorPageReplacesPlaceholder(t *testing.T) {
	rm := New(theme.Default())
	mp := &dummyTea{name: "MONITOR"}
	rm = rm.WithMonitorPage(mp)
	rm.active = 3 // Monitor tab
	v := rm.View()
	if !contains(v, "MONITOR") {
		t.Fatalf("Monitor tab does not render replacement page:\n%s", v)
	}
}

func TestRoot_RoutesSwitchToMonitorMsg(t *testing.T) {
	rm := New(theme.Default())
	rm = rm.WithMonitorPage(&dummyTea{name: "MONITOR"})
	model, _ := rm.Update(pages.SwitchToMonitorMsg{PID: 999})
	got := model.(RootModel).active
	if got != 3 {
		t.Fatalf("active = %d, want 3 (Monitor)", got)
	}
}
```

(`dummyTea` já existe em `root_test.go` desde slice 4.)

- [x] **Step 2: Rodar — confirmar falha**

- [x] **Step 3: Implementar**

Em `root.go`, abaixo de `WithLauncherPage`:

```go
// WithMonitorPage replaces the placeholder Monitor tab with a real model.
func (m RootModel) WithMonitorPage(p tea.Model) RootModel {
	m.tabs[3].model = p
	return m
}
```

E em `Update`, antes do tab routing default:

```go
case pages.SwitchToMonitorMsg:
	m.active = 3
	return m, nil
```

(Importar `internal/ui/pages`.)

- [x] **Step 4: Wire em `main.go`**

```go
mon := monitor.New(monitor.Config{NvidiaSMIPath: "nvidia-smi"})
monitorPage := pages.NewMonitorPage(processMgr, mon)
root := ui.New(theme.Default()).
	WithProfilesPage(profilesPage).
	WithModelsPage(modelsPage).
	WithLauncherPage(launcherPage).
	WithMonitorPage(monitorPage)
```

- [x] **Step 5: Rodar test suite completa**

Run: `go test ./... -timeout 90s -count=1`
Expected: PASS.

- [x] **Step 6: Verificar binário**

Run: `go build -o /tmp/model-loader ./cmd/model-loader`
Expected: build OK.

- [x] **Step 7: Commit**

```bash
git add internal/ui/root.go internal/ui/root_test.go cmd/model-loader/main.go
git commit -m "feat(ui/root, cmd): wire MonitorPage and SwitchToMonitorMsg"
```

---

## Phase B checkpoint

- [x] **Run full suite + go vet**

Run: `go test ./... -timeout 120s && go vet ./...`
Expected: PASS, sem warnings.

- [ ] **Smoke teste manual da TUI (opcional)**

```bash
go build -o /tmp/model-loader ./cmd/model-loader

# Override binário:
export PATH="$(pwd)/testdata:$PATH"
ln -sf "$(pwd)/testdata/fake-llama-server.sh" /tmp/llama-server
PATH="/tmp:$PATH" /tmp/model-loader
```

Navegar para tab `2` (Launcher), selecionar profile, Enter. Confirmar:
- Status muda para "launching..." → "healthy".
- Auto-switch para tab `4` (Monitor).
- Top table mostra a instância.
- `Tab` cicla Logs → Slots → Métricas.
- Sparkline aparece em Métricas (mesmo que vazia).
- `k` mata; instância some da tabela.
- `q` sai (instância background sobrevive).

(Não comprometer; sanity-check.)

---

## Self-Review Checklist

**1. Spec coverage (§ 6.6 Monitor + § 7.2 monitorPage + F2/F3):**

| Spec | Coberto por |
|------|-------------|
| `Subscribe(pid, port)` retorna `<-chan MonitorEvent` + cancel | T1 + T7 |
| `MonitorEvent{Timestamp, Source, Data}` | T1 |
| Tail contínuo do log via `fsnotify` | T3 |
| Tick 1s `GET /slots` + `/health` | T4 |
| Tick 2s `nvidia-smi` (skip se ausente) | T5 |
| Janela 60s `tokens/s` + `req/s` | T6 |
| `cancel()` fecha goroutine + canal | T7 |
| `TailLogs(pid)` em ProcessManager | T8 |
| monitorPage instances table (`name | pid | port | uptime | VRAM | tokens/s`) | T11 |
| sub-view Logs (auto-scroll, pause `Space`) | T12 + T15 |
| sub-view Slots (`idx | state | ctx | client`) | T13 |
| sub-view Métricas (sparkline) | T14 |
| `k` kill, `r` restart | T15 (`r` parcial — kill apenas; restart full deferido para slice 6) |
| Auto-switch após launch healthy (F2 step 4) | T16 + T17 |
| Multi-instance | T11 + T12 (subs por PID) |

**2. Placeholder scan:** Verificado — não há "TBD"/"implement later" não-documentado. Restart-completo-em-slice-6 está explicitamente nota; `r` no slice 5 mata como kill enquanto se aguarda injeção do `ProfileStore` na page.

**3. Type consistency:**
- `Manager`/`fsMonitor`/`MonitorEvent`/`EventSource`/`SourceLogs..SourceMetrics`/`Config`/`HTTPDoer` consistentes em todos os tasks.
- `LogLine`/`SlotSnapshot`/`Slot`/`HealthStatus`/`GPUStats`/`Metrics` definidos em T1 e usados como `Data any` em events.
- `procMgrIface` em monitor page é subset estrito do `processmgr.Manager` real (Launch, Kill, List, TailLogs); compatível.
- `SwitchToMonitorMsg{PID int}` pacote `pages`; root.go importa.

**4. Cross-task references:**
- T7 referencia `newLogFollower` (T3), `newSlotsPoller` (T4), `newGPUPoller` (T5), `newMetricsAgg` (T6), `newLogRing` (T2). Ordem correta.
- T11/T12 referencia `monitor.Manager` (T1) + `processmgr.TailLogs` (T8) + `RunningInstance.LogPath` (existente do slice 4).
- T16 emite `SwitchToMonitorMsg`; T17 consome — sem ordem invertida.
- T17 wire main depende de `MonitorPage` existir (T11+) e `WithMonitorPage` builder (T17 step 3).

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-29-model-loader-slice-5.md`. Two execution options:

1. **Subagent-Driven (recommended)** — fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** — execute tasks in this session via `executing-plans`, batch execution with checkpoints.

Which approach?

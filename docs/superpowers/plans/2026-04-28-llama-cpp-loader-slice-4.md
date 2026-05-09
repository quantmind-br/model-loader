# model-loader — Slice 4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `ProcessManager` service (launch/kill/list/health/recover) + `LauncherPage` (profile picker + fg/bg toggle + validate + run) + `instances.json` recovery, encerrando slice 4 do roadmap (design § 11).

**Architecture:** Camada `service/processmgr` orquestra `os/exec` para spawnar `llama-server`, persiste estado em `instances.json` (atomic write), e reconcilia ao boot (PIDs vivos via `/proc` + nome do binário). UI consome via `tea.Cmd` que retorna `LaunchedMsg`/`LaunchErrMsg`. `BuildArgs` converte `Profile.Args` (map[string]any) + `ExtraArgs` em flags CLI, com `--model` injetado primeiro.

**Tech Stack:** Go 1.26.2, std lib (`os/exec`, `syscall`, `net/http`, `encoding/json`, `filepath`), `charmbracelet/bubbletea`, `charmbracelet/bubbles/list`, `charmbracelet/huh` (toggle).

**Spec deviations & deferrals:**

- **`TailLogs(pid)` interface method:** spec § 6.4 lista no interface; slice 4 não consome (Monitor é slice 5). Deferido — adicionado em slice 5 junto com fsnotify-based tail.
- **PID validation via gopsutil:** spec § 6.4 / § 12 cita `gopsutil`. Slice 4 usa stdlib (`os.FindProcess` + `Signal(0)` para liveness; leitura de `/proc/<pid>/comm` para nome). Linux-only, alinhado a § 0 plataforma alvo. `gopsutil` será adicionado em slice 5 (Monitor precisa de GPU stats fallback). Justificativa: 1 dep a menos no slice 4, recover é trivial com stdlib.
- **Auto-switch para `monitorPage` após launch:** spec § 7.2 F2 step 4 diz "navega para monitorPage"; slice 5 entrega monitor. Slice 4 fica na própria launcherPage e exibe instância em painel "Running" inline.
- **Cross-tab `L` em profilesPage:** spec § 7.2 lista `L launch` em profilesPage. Slice 4 mantém launcher standalone (acessado via tab `2`). Cross-tab L fica para slice 6 (polimento).

---

## File Structure

**Domain (already present, no changes):**
- `internal/domain/instance.go` — `RunningInstance`, `LogLine` (presentes desde slices anteriores)

**Service processmgr:**
- `internal/service/processmgr/processmgr.go` (NEW) — package doc + `Manager` interface + `LaunchMode` + sentinel errors
- `internal/service/processmgr/args.go` (NEW) — `BuildArgs(p domain.Profile) []string`
- `internal/service/processmgr/args_test.go` (NEW)
- `internal/service/processmgr/registry.go` (NEW) — `loadRegistry` / `saveRegistry` para `instances.json`
- `internal/service/processmgr/registry_test.go` (NEW)
- `internal/service/processmgr/manager.go` (NEW) — `fsManager` struct + `Launch`, `Kill`, `List`, `WaitHealthy`
- `internal/service/processmgr/manager_test.go` (NEW)
- `internal/service/processmgr/recover.go` (NEW) — `Reconcile` boot recovery
- `internal/service/processmgr/recover_test.go` (NEW)

**UI pages:**
- `internal/ui/pages/launcher.go` (NEW) — `LauncherPage` (profile list + bg toggle + validate + launch + running list + kill)
- `internal/ui/pages/launcher_test.go` (NEW)

**Testdata:**
- `testdata/fake-llama-server.sh` (NEW) — bash script que aceita flags, escreve PID/port log, serve `/health` em `--port` argumento

**Wire/integration:**
- `internal/ui/root.go` (MODIFY) — adicionar `WithLauncherPage` builder
- `cmd/model-loader/main.go` (MODIFY) — instanciar `processmgr.New` + `Reconcile` na boot + `pages.NewLauncherPage` + `root.WithLauncherPage`
- `internal/ui/root_test.go` (MODIFY) — smoke test mostrando "Launcher" ao apertar tab `2`

---

## Phase A — Service plumbing (sem UI)

### Task 1: Manager interface + LaunchMode + sentinel errors

**Files:**
- Create: `internal/service/processmgr/processmgr.go`

- [x] **Step 1: Criar `processmgr.go` com interface, enum e errors**

```go
// Package processmgr launches and tracks llama-server processes.
package processmgr

import (
	"errors"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// LaunchMode selects how the spawned process is attached.
type LaunchMode int

const (
	// LaunchBackground spawns the process detached (Setsid), redirects
	// stdout/stderr to a log file, and persists the entry in instances.json.
	LaunchBackground LaunchMode = iota
	// LaunchForeground spawns the process attached for in-TUI streaming.
	// Only one foreground instance is allowed at a time.
	LaunchForeground
)

// Manager owns the lifecycle of llama-server processes.
type Manager interface {
	Launch(p domain.Profile, mode LaunchMode) (domain.RunningInstance, error)
	Kill(pid int) error
	List() []domain.RunningInstance
	WaitHealthy(pid, port int, timeout time.Duration) error
}

// Sentinel errors. UI maps these to status bar messages.
var (
	ErrPortBusy           = errors.New("port already in use")
	ErrModelNotFound      = errors.New("model file not found")
	ErrForegroundBusy     = errors.New("a foreground instance is already running")
	ErrUnknownPID         = errors.New("pid is not tracked by this manager")
	ErrHealthCheckTimeout = errors.New("llama-server did not become healthy within timeout")
)
```

- [x] **Step 2: Verificar build**

Run: `go build ./internal/service/processmgr/...`
Expected: success (sem erros, sem warnings).

- [x] **Step 3: Commit**

```bash
git add internal/service/processmgr/processmgr.go
git commit -m "feat(processmgr): Manager interface, LaunchMode and sentinel errors"
```

---

### Task 2: BuildArgs (Profile → []string)

**Files:**
- Create: `internal/service/processmgr/args.go`
- Test: `internal/service/processmgr/args_test.go`

- [x] **Step 1: Escrever teste falhando**

```go
package processmgr

import (
	"reflect"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestBuildArgs_ModelFirstAndSortedFlags(t *testing.T) {
	p := domain.Profile{
		Model: "/m.gguf",
		Args: map[string]any{
			"ngl":        float64(99),
			"flash-attn": true,
			"ctx-size":   float64(16384),
			"cache-type-k": "q8_0",
		},
		ExtraArgs: []string{"--no-warmup"},
	}
	got := BuildArgs(p)
	want := []string{
		"--model", "/m.gguf",
		"--cache-type-k", "q8_0",
		"--ctx-size", "16384",
		"--flash-attn",
		"--ngl", "99",
		"--no-warmup",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgs:\n got = %v\nwant = %v", got, want)
	}
}

func TestBuildArgs_BoolFalseOmitted(t *testing.T) {
	p := domain.Profile{
		Model: "/m.gguf",
		Args:  map[string]any{"flash-attn": false, "mlock": true},
	}
	got := BuildArgs(p)
	want := []string{"--model", "/m.gguf", "--mlock"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %v, want = %v", got, want)
	}
}

func TestBuildArgs_FloatPreservesDecimal(t *testing.T) {
	p := domain.Profile{
		Model: "/m.gguf",
		Args:  map[string]any{"temp": float64(0.7)},
	}
	got := BuildArgs(p)
	want := []string{"--model", "/m.gguf", "--temp", "0.7"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %v, want = %v", got, want)
	}
}

func TestBuildArgs_TensorSplitArrayJoined(t *testing.T) {
	p := domain.Profile{
		Model: "/m.gguf",
		Args:  map[string]any{"tensor-split": []any{float64(0.6), float64(0.4)}},
	}
	got := BuildArgs(p)
	want := []string{"--model", "/m.gguf", "--tensor-split", "0.6,0.4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %v, want = %v", got, want)
	}
}
```

- [x] **Step 2: Rodar teste — confirmar falha**

Run: `go test ./internal/service/processmgr/ -run TestBuildArgs -v`
Expected: FAIL com "undefined: BuildArgs".

- [x] **Step 3: Implementar `BuildArgs`**

```go
package processmgr

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// BuildArgs converts a Profile into the CLI args slice used to spawn
// llama-server. The argument order is deterministic: --model first, then
// flags from p.Args sorted by key, then p.ExtraArgs verbatim.
//
// Value mapping:
//   - bool true  -> "--<key>"        (false is omitted; the flag's default
//     is assumed to be false; --no-X variants live in ExtraArgs)
//   - string     -> "--<key>" "<v>"
//   - float64    -> "--<key>" "<v>"  (printed as int if mathematically integral)
//   - []any      -> "--<key>" "<v0,v1,...>" (comma-joined, e.g. tensor-split)
func BuildArgs(p domain.Profile) []string {
	args := make([]string, 0, 2+2*len(p.Args)+len(p.ExtraArgs))
	args = append(args, "--model", p.Model)

	keys := make([]string, 0, len(p.Args))
	for k := range p.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		switch v := p.Args[k].(type) {
		case bool:
			if v {
				args = append(args, "--"+k)
			}
		case string:
			args = append(args, "--"+k, v)
		case float64:
			args = append(args, "--"+k, formatFloat(v))
		case []any:
			parts := make([]string, len(v))
			for i, x := range v {
				parts[i] = fmt.Sprint(x)
			}
			args = append(args, "--"+k, strings.Join(parts, ","))
		}
	}
	args = append(args, p.ExtraArgs...)
	return args
}

func formatFloat(f float64) string {
	if f == math.Trunc(f) && !math.IsInf(f, 0) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
```

- [x] **Step 4: Rodar teste — confirmar passa**

Run: `go test ./internal/service/processmgr/ -run TestBuildArgs -v`
Expected: PASS para os 4 casos.

- [x] **Step 5: Commit**

```bash
git add internal/service/processmgr/args.go internal/service/processmgr/args_test.go
git commit -m "feat(processmgr): BuildArgs converts Profile into llama-server CLI flags"
```

---

### Task 3: Registry (instances.json read/write)

**Files:**
- Create: `internal/service/processmgr/registry.go`
- Test: `internal/service/processmgr/registry_test.go`

- [x] **Step 1: Escrever teste falhando**

```go
package processmgr

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestRegistry_SaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instances.json")

	want := []domain.RunningInstance{
		{ProfileID: "qwen", PID: 4521, Port: 8080,
			LogPath: "/log/qwen.log", StartedAt: time.Now().UTC().Truncate(time.Second), Background: true},
	}
	if err := saveRegistry(path, want); err != nil {
		t.Fatalf("saveRegistry: %v", err)
	}
	got, err := loadRegistry(path)
	if err != nil {
		t.Fatalf("loadRegistry: %v", err)
	}
	if len(got) != 1 || got[0].PID != 4521 || got[0].Port != 8080 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestRegistry_LoadMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := loadRegistry(filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if got != nil && len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

func TestRegistry_SaveAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instances.json")
	if err := saveRegistry(path, []domain.RunningInstance{{ProfileID: "a", PID: 1, Port: 9}}); err != nil {
		t.Fatal(err)
	}
	// confirm no .tmp leftover
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover tmp: %s", e.Name())
		}
	}
}

func TestRegistry_LoadCorruptReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instances.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRegistry(path); err == nil {
		t.Fatal("expected error for corrupt registry, got nil")
	}
}
```

- [x] **Step 2: Rodar teste — confirmar falha**

Run: `go test ./internal/service/processmgr/ -run TestRegistry -v`
Expected: FAIL com "undefined: saveRegistry / loadRegistry".

- [x] **Step 3: Implementar registry**

```go
package processmgr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// registryFile is the on-disk shape of instances.json. Wrapping the slice in
// an object lets us add fields later without breaking parse compatibility.
type registryFile struct {
	Instances []domain.RunningInstance `json:"instances"`
}

// loadRegistry reads path and returns its instances. A missing file yields
// (nil, nil). A malformed file yields (nil, error).
func loadRegistry(path string) ([]domain.RunningInstance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read registry: %w", err)
	}
	var rf registryFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	return rf.Instances, nil
}

// saveRegistry writes the slice atomically to path. Parent dir is created
// if absent.
func saveRegistry(path string, insts []domain.RunningInstance) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	rf := registryFile{Instances: insts}
	data, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
```

- [x] **Step 4: Rodar testes — confirmar passam**

Run: `go test ./internal/service/processmgr/ -run TestRegistry -v`
Expected: PASS para os 4 casos.

- [x] **Step 5: Commit**

```bash
git add internal/service/processmgr/registry.go internal/service/processmgr/registry_test.go
git commit -m "feat(processmgr): atomic load/save for instances.json registry"
```

---

### Task 4: Fake llama-server bash script

**Files:**
- Create: `testdata/fake-llama-server.sh`

- [x] **Step 1: Criar fake-llama-server.sh**

```bash
#!/usr/bin/env bash
# Fake llama-server for processmgr tests.
#
# Behavior:
#   - Parses --port <N> from args (default 0 = pick free port).
#   - Starts a minimal HTTP server on 127.0.0.1:<N> that responds 200
#     on GET /health with body '{"status":"ok"}'.
#   - Echoes argv (one arg per line, prefixed "arg: ") to stderr.
#   - Stays in foreground until SIGTERM/SIGINT. On signal, exits 0.
#
# Used to exercise Launch/WaitHealthy/Kill/List/Recover without a real
# llama.cpp build. Linux-only; uses /dev/tcp + nc -l for the listener.

set -euo pipefail

PORT=0
ARGS=("$@")
for ((i=0; i<${#ARGS[@]}; i++)); do
  if [[ "${ARGS[i]}" == "--port" ]]; then
    PORT="${ARGS[i+1]:-0}"
  fi
  echo "arg: ${ARGS[i]}" 1>&2
done

if [[ "$PORT" == "0" ]]; then
  echo "fake-llama-server: --port required" 1>&2
  exit 2
fi

# Trap so 'kill' from the test exits cleanly.
cleanup() { exit 0; }
trap cleanup TERM INT

# Tiny HTTP loop: read one request line, reply 200 with JSON, loop forever.
RESPONSE=$'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 15\r\nConnection: close\r\n\r\n{"status":"ok"}'

# Use python3 if available (most reliable). Falls back to a no-op sleep that
# never serves health (test will time out and surface that).
if command -v python3 >/dev/null 2>&1; then
  exec python3 - "$PORT" <<'PY'
import sys, http.server, socketserver, threading, signal, os
port = int(sys.argv[1])
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/health":
            body = b'{"status":"ok"}'
            self.send_response(200); self.send_header("Content-Type","application/json")
            self.send_header("Content-Length", str(len(body))); self.end_headers()
            self.wfile.write(body)
        else:
            self.send_response(404); self.end_headers()
    def log_message(self, *a, **kw): pass
srv = socketserver.TCPServer(("127.0.0.1", port), H)
def stop(*_): srv.shutdown(); os._exit(0)
signal.signal(signal.SIGTERM, stop); signal.signal(signal.SIGINT, stop)
srv.serve_forever()
PY
fi

# Fallback: just sleep so process stays alive but health never comes up.
while true; do sleep 1; done
```

- [x] **Step 2: Tornar executável**

Run: `chmod +x testdata/fake-llama-server.sh`
Expected: sem output; permissão 0755.

- [x] **Step 3: Smoke check (manual)**

Run: `testdata/fake-llama-server.sh --port 18765 &`
Then: `curl -s http://127.0.0.1:18765/health && echo`
Then: `kill %1`
Expected: imprime `{"status":"ok"}` e o processo termina.

(Não é necessário comprometer essa verificação no commit; apenas garante que o script está funcional antes de continuar.)

- [x] **Step 4: Commit**

```bash
git add testdata/fake-llama-server.sh
git commit -m "test(processmgr): fake-llama-server.sh fixture serving /health"
```

---

### Task 5: Launch background + WaitHealthy

**Files:**
- Create: `internal/service/processmgr/manager.go`
- Test: `internal/service/processmgr/manager_test.go`

- [x] **Step 1: Esqueleto do `fsManager` (precondição mínima para os testes compilarem)**

Crie `internal/service/processmgr/manager.go`:

```go
package processmgr

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// fsManager is the default Manager implementation backed by os/exec.
type fsManager struct {
	binary       string // resolved path or name on PATH; default "llama-server"
	logDir       string // directory for background stdout/stderr capture
	registryPath string // absolute path to instances.json

	mu       sync.Mutex
	tracked  map[int]domain.RunningInstance // pid -> instance
	fgPID    int                            // 0 if no foreground active
}

// Config holds wiring for New. Caller owns the paths; the manager creates
// directories on demand.
type Config struct {
	Binary       string // override; empty = "llama-server"
	LogDir       string
	RegistryPath string
}

// New constructs a Manager. It does NOT call Reconcile; main.go orchestrates
// boot recovery explicitly.
func New(cfg Config) *fsManager {
	bin := cfg.Binary
	if bin == "" {
		bin = "llama-server"
	}
	return &fsManager{
		binary:       bin,
		logDir:       cfg.LogDir,
		registryPath: cfg.RegistryPath,
		tracked:      map[int]domain.RunningInstance{},
	}
}
```

- [x] **Step 2: Escrever teste falhando para Launch background + WaitHealthy**

Append to `internal/service/processmgr/manager_test.go`:

```go
package processmgr

import (
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func fakeBinary(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../../../testdata/fake-llama-server.sh")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func newTestManager(t *testing.T) (*fsManager, string) {
	t.Helper()
	dir := t.TempDir()
	mgr := New(Config{
		Binary:       fakeBinary(t),
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
	})
	return mgr, dir
}

func TestManager_LaunchBackground_WaitsHealthyAndPersists(t *testing.T) {
	mgr, _ := newTestManager(t)
	port := freePort(t)
	p := domain.Profile{
		ID:    "smoke",
		Name:  "Smoke",
		Model: "/dev/null", // fake doesn't actually open it
		Args:  map[string]any{"port": float64(port)},
	}
	inst, err := mgr.Launch(p, LaunchBackground)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	if inst.PID <= 0 || inst.Port != port || !inst.Background {
		t.Fatalf("inst = %+v", inst)
	}

	if err := mgr.WaitHealthy(inst.PID, port, 5*time.Second); err != nil {
		t.Fatalf("WaitHealthy: %v", err)
	}

	// Registry must contain the new entry.
	loaded, err := loadRegistry(mgr.registryPath)
	if err != nil {
		t.Fatalf("loadRegistry: %v", err)
	}
	found := false
	for _, ri := range loaded {
		if ri.PID == inst.PID {
			found = true
		}
	}
	if !found {
		t.Errorf("registry missing pid %d; got %+v", inst.PID, loaded)
	}
}

func TestManager_LaunchBackground_PortBusy(t *testing.T) {
	mgr, _ := newTestManager(t)

	// Hold the port to simulate busy.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	p := domain.Profile{
		ID:    "busy",
		Model: "/dev/null",
		Args:  map[string]any{"port": float64(port)},
	}
	_, err = mgr.Launch(p, LaunchBackground)
	if err == nil {
		t.Fatal("expected ErrPortBusy, got nil")
	}
	if !errorsIs(err, ErrPortBusy) {
		t.Fatalf("err = %v, want ErrPortBusy", err)
	}
}

// errorsIs is a small wrapper to keep imports tidy across test files.
func errorsIs(err, target error) bool {
	for e := err; e != nil; {
		if e == target {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := e.(unwrapper)
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

func TestManager_WaitHealthy_TimesOut(t *testing.T) {
	mgr, _ := newTestManager(t)
	// No process at this port; expect timeout.
	port := freePort(t)
	err := mgr.WaitHealthy(99999, port, 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errorsIs(err, ErrHealthCheckTimeout) {
		t.Fatalf("err = %v, want ErrHealthCheckTimeout", err)
	}
}

// helper used by future tests
func mustExtractPort(t *testing.T, p int) string {
	t.Helper()
	return fmt.Sprintf("%d", p)
}
```

- [x] **Step 3: Rodar testes — confirmar falham**

Run: `go test ./internal/service/processmgr/ -run "TestManager_Launch|TestManager_WaitHealthy" -v`
Expected: FAIL com "manager has no field or method Launch / WaitHealthy / Kill".

- [x] **Step 4: Implementar Launch background + WaitHealthy + Kill (minimal)**

Append to `internal/service/processmgr/manager.go`:

```go
// Launch spawns llama-server with the args derived from p. mode chooses
// between background (detached, log-to-file) and foreground (stdout/stderr
// inherit; only one allowed at a time — covered in Task 6).
func (m *fsManager) Launch(p domain.Profile, mode LaunchMode) (domain.RunningInstance, error) {
	port, ok := portFromProfile(p)
	if !ok {
		return domain.RunningInstance{}, fmt.Errorf("profile %q: missing or invalid port arg", p.ID)
	}
	if err := checkPortFree(port); err != nil {
		return domain.RunningInstance{}, err
	}
	if mode == LaunchForeground {
		return domain.RunningInstance{}, fmt.Errorf("foreground not implemented in this task")
	}

	if err := os.MkdirAll(m.logDir, 0o755); err != nil {
		return domain.RunningInstance{}, fmt.Errorf("mkdir log dir: %w", err)
	}
	logPath := filepath.Join(m.logDir, fmt.Sprintf("%s-%d.log", p.ID, port))
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return domain.RunningInstance{}, fmt.Errorf("open log: %w", err)
	}

	cmd := exec.Command(m.binary, BuildArgs(p)...)
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		_ = logF.Close()
		return domain.RunningInstance{}, fmt.Errorf("start llama-server: %w", err)
	}

	inst := domain.RunningInstance{
		ProfileID:  p.ID,
		PID:        cmd.Process.Pid,
		Port:       port,
		LogPath:    logPath,
		StartedAt:  time.Now().UTC(),
		Background: true,
	}

	m.mu.Lock()
	m.tracked[inst.PID] = inst
	all := snapshotLocked(m.tracked)
	m.mu.Unlock()

	if err := saveRegistry(m.registryPath, all); err != nil {
		// Process is running; registry-save failure shouldn't kill it.
		// Surface as warning via the returned error without untracking.
		return inst, fmt.Errorf("instance started (pid %d) but registry save failed: %w", inst.PID, err)
	}
	return inst, nil
}

// WaitHealthy polls GET http://127.0.0.1:<port>/health with capped exponential
// backoff (100ms, 200ms, 400ms, ..., max 1s) until 200 OK or timeout.
func (m *fsManager) WaitHealthy(_ /*pid*/ int, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	delay := 100 * time.Millisecond
	const maxDelay = time.Second
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(delay)
		if delay < maxDelay {
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		}
	}
	return fmt.Errorf("port %d: %w", port, ErrHealthCheckTimeout)
}

// Kill sends SIGTERM to pid (10s grace) then SIGKILL if still alive. Removes
// the entry from the tracking table and rewrites the registry file.
func (m *fsManager) Kill(pid int) error {
	m.mu.Lock()
	_, ok := m.tracked[pid]
	m.mu.Unlock()
	if !ok {
		return ErrUnknownPID
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil && err != os.ErrProcessDone {
		return fmt.Errorf("sigterm: %w", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if proc.Signal(syscall.Signal(0)) != nil {
			break // dead
		}
		time.Sleep(100 * time.Millisecond)
	}
	if proc.Signal(syscall.Signal(0)) == nil {
		// still alive; force.
		_ = proc.Signal(syscall.SIGKILL)
	}

	m.mu.Lock()
	delete(m.tracked, pid)
	all := snapshotLocked(m.tracked)
	m.mu.Unlock()
	return saveRegistry(m.registryPath, all)
}

// List returns a snapshot of the tracked instances. Order is not guaranteed.
func (m *fsManager) List() []domain.RunningInstance {
	m.mu.Lock()
	defer m.mu.Unlock()
	return snapshotLocked(m.tracked)
}

func snapshotLocked(t map[int]domain.RunningInstance) []domain.RunningInstance {
	out := make([]domain.RunningInstance, 0, len(t))
	for _, v := range t {
		out = append(out, v)
	}
	return out
}

func portFromProfile(p domain.Profile) (int, bool) {
	v, ok := p.Args["port"]
	if !ok {
		return 0, false
	}
	switch v := v.(type) {
	case float64:
		if v <= 0 || v > 65535 {
			return 0, false
		}
		return int(v), true
	case int:
		if v <= 0 || v > 65535 {
			return 0, false
		}
		return v, true
	case string:
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 65535 {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

func checkPortFree(port int) error {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("port %d: %w", port, ErrPortBusy)
	}
	_ = l.Close()
	return nil
}
```

- [x] **Step 5: Rodar testes — confirmar passam**

Run: `go test ./internal/service/processmgr/ -run "TestManager_Launch|TestManager_WaitHealthy" -v -timeout 30s`
Expected: PASS para os 3 casos. Se `TestManager_LaunchBackground_WaitsHealthyAndPersists` falhar com timeout, confirme que `python3` está disponível no PATH (pre-req do fake-llama-server.sh).

- [x] **Step 6: Commit**

```bash
git add internal/service/processmgr/manager.go internal/service/processmgr/manager_test.go
git commit -m "feat(processmgr): Launch background, WaitHealthy, Kill, List"
```

---

### Task 6: Foreground constraint (only one at a time)

**Files:**
- Modify: `internal/service/processmgr/manager.go`
- Modify: `internal/service/processmgr/manager_test.go`

- [x] **Step 1: Escrever teste falhando**

Append to `internal/service/processmgr/manager_test.go`:

```go
func TestManager_Foreground_OnlyOneAllowed(t *testing.T) {
	mgr, _ := newTestManager(t)
	port1 := freePort(t)
	port2 := freePort(t)

	p1 := domain.Profile{ID: "fg1", Model: "/dev/null", Args: map[string]any{"port": float64(port1)}}
	inst1, err := mgr.Launch(p1, LaunchForeground)
	if err != nil {
		t.Fatalf("first foreground Launch: %v", err)
	}
	defer mgr.Kill(inst1.PID)

	p2 := domain.Profile{ID: "fg2", Model: "/dev/null", Args: map[string]any{"port": float64(port2)}}
	_, err = mgr.Launch(p2, LaunchForeground)
	if err == nil {
		t.Fatal("expected ErrForegroundBusy, got nil")
	}
	if !errorsIs(err, ErrForegroundBusy) {
		t.Fatalf("err = %v, want ErrForegroundBusy", err)
	}

	// background launch alongside fg1 must still succeed
	port3 := freePort(t)
	p3 := domain.Profile{ID: "bg1", Model: "/dev/null", Args: map[string]any{"port": float64(port3)}}
	inst3, err := mgr.Launch(p3, LaunchBackground)
	if err != nil {
		t.Fatalf("background launch alongside fg: %v", err)
	}
	defer mgr.Kill(inst3.PID)
}
```

- [x] **Step 2: Rodar teste — confirmar falha**

Run: `go test ./internal/service/processmgr/ -run TestManager_Foreground -v -timeout 30s`
Expected: FAIL — atualmente `Launch` retorna "foreground not implemented in this task".

- [x] **Step 3: Implementar foreground branch**

Substituir o bloco `if mode == LaunchForeground { return ..., fmt.Errorf("foreground not implemented in this task") }` em `manager.go` por chamada a um helper `launchForeground` e adicionar o helper:

Em `Launch`, substituir:

```go
	if mode == LaunchForeground {
		return domain.RunningInstance{}, fmt.Errorf("foreground not implemented in this task")
	}
```

por:

```go
	if mode == LaunchForeground {
		return m.launchForeground(p, port)
	}
```

E adicione no fim do arquivo:

```go
// launchForeground spawns a single foreground instance. Stdout/Stderr are
// not redirected (the caller — the LauncherPage — owns the streaming),
// and the process is NOT detached via Setsid: it remains in the TUI's
// process group so Ctrl+C from the TUI propagates if desired.
func (m *fsManager) launchForeground(p domain.Profile, port int) (domain.RunningInstance, error) {
	m.mu.Lock()
	if m.fgPID != 0 {
		m.mu.Unlock()
		return domain.RunningInstance{}, ErrForegroundBusy
	}
	m.mu.Unlock()

	cmd := exec.Command(m.binary, BuildArgs(p)...)
	// Inherit stdout/stderr — caller drains via TailLogs in slice 5.
	if err := cmd.Start(); err != nil {
		return domain.RunningInstance{}, fmt.Errorf("start llama-server (fg): %w", err)
	}

	inst := domain.RunningInstance{
		ProfileID:  p.ID,
		PID:        cmd.Process.Pid,
		Port:       port,
		LogPath:    "", // no log file for foreground
		StartedAt:  time.Now().UTC(),
		Background: false,
	}

	m.mu.Lock()
	m.tracked[inst.PID] = inst
	m.fgPID = inst.PID
	all := snapshotLocked(m.tracked)
	m.mu.Unlock()

	if err := saveRegistry(m.registryPath, all); err != nil {
		return inst, fmt.Errorf("fg started but registry save failed: %w", err)
	}
	return inst, nil
}
```

E em `Kill`, após `delete(m.tracked, pid)` (mas dentro do lock), adicionar:

```go
	if m.fgPID == pid {
		m.fgPID = 0
	}
```

- [x] **Step 4: Rodar testes — confirmar passam**

Run: `go test ./internal/service/processmgr/ -run TestManager -v -timeout 30s`
Expected: todos os `TestManager_*` passam.

- [x] **Step 5: Commit**

```bash
git add internal/service/processmgr/manager.go internal/service/processmgr/manager_test.go
git commit -m "feat(processmgr): foreground launch with single-instance constraint"
```

---

### Task 7: Reconcile (boot recovery)

**Files:**
- Create: `internal/service/processmgr/recover.go`
- Test: `internal/service/processmgr/recover_test.go`

- [x] **Step 1: Escrever teste falhando**

```go
package processmgr

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestReconcile_DropsZombiePIDs(t *testing.T) {
	mgr, dir := newTestManager(t)

	// Write a registry with two entries: one fake (PID=1, init — alive but
	// name != llama-server) and one with our own PID (alive, name != llama-server).
	// Both should be dropped because comm doesn't contain the binary.
	entries := []domain.RunningInstance{
		{ProfileID: "ghost", PID: 1, Port: 9001, LogPath: filepath.Join(dir, "logs/ghost.log"), StartedAt: time.Now(), Background: true},
		{ProfileID: "self", PID: os.Getpid(), Port: 9002, LogPath: filepath.Join(dir, "logs/self.log"), StartedAt: time.Now(), Background: true},
	}
	if err := saveRegistry(mgr.registryPath, entries); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if got := mgr.List(); len(got) != 0 {
		t.Errorf("List after Reconcile = %v, want empty", got)
	}

	// Registry on disk must also be empty.
	loaded, _ := loadRegistry(mgr.registryPath)
	if len(loaded) != 0 {
		t.Errorf("on-disk registry = %v, want empty", loaded)
	}
}

func TestReconcile_KeepsLiveLlamaServer(t *testing.T) {
	mgr, _ := newTestManager(t)
	port := freePort(t)
	p := domain.Profile{ID: "alive", Model: "/dev/null", Args: map[string]any{"port": float64(port)}}
	inst, err := mgr.Launch(p, LaunchBackground)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	// Forge a fresh manager pointing at the same registry — simulates restart.
	dir := filepath.Dir(mgr.registryPath)
	freshMgr := New(Config{
		Binary:       fakeBinary(t),
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: mgr.registryPath,
	})
	if err := freshMgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	got := freshMgr.List()
	if len(got) != 1 || got[0].PID != inst.PID {
		t.Errorf("expected 1 entry pid=%d, got %+v", inst.PID, got)
	}

	// Cleanup via fresh manager.
	_ = freshMgr.Kill(inst.PID)
}

// helper to silence unused import in this test file
var _ = strconv.Atoi
```

- [x] **Step 2: Rodar teste — confirmar falha**

Run: `go test ./internal/service/processmgr/ -run TestReconcile -v -timeout 30s`
Expected: FAIL com "fsManager has no method Reconcile".

- [x] **Step 3: Implementar Reconcile + helpers**

Criar `internal/service/processmgr/recover.go`:

```go
package processmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Reconcile reads the on-disk registry, validates each entry against the
// running process table, drops zombies, and updates the registry.
//
// An entry survives only if BOTH:
//   - the PID is alive (signal 0 succeeds), AND
//   - /proc/<pid>/comm contains the basename of the binary the manager
//     was configured with (default: "llama-server"). This avoids
//     mistaking a recycled PID for a live server.
//
// Linux-only. Other platforms: this method drops every entry (safe default).
func (m *fsManager) Reconcile() error {
	loaded, err := loadRegistry(m.registryPath)
	if err != nil {
		return err
	}
	expectedComm := filepath.Base(m.binary)

	survivors := make([]domain.RunningInstance, 0, len(loaded))
	tracked := make(map[int]domain.RunningInstance, len(loaded))
	for _, ri := range loaded {
		if !pidAliveAndNameMatches(ri.PID, expectedComm) {
			continue
		}
		survivors = append(survivors, ri)
		tracked[ri.PID] = ri
	}

	m.mu.Lock()
	m.tracked = tracked
	m.mu.Unlock()

	if err := saveRegistry(m.registryPath, survivors); err != nil {
		return fmt.Errorf("rewrite registry: %w", err)
	}
	return nil
}

// pidAliveAndNameMatches returns true iff pid is alive AND the basename of
// /proc/<pid>/comm contains expectedComm. Reads /proc directly (Linux).
func pidAliveAndNameMatches(pid int, expectedComm string) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	commBytes, err := os.ReadFile(filepath.Join("/proc", fmt.Sprintf("%d", pid), "comm"))
	if err != nil {
		return false
	}
	comm := strings.TrimSpace(string(commBytes))
	return strings.Contains(comm, expectedComm)
}
```

Adicionar import em `recover.go`:

```go
import (
	"github.com/quantmind-br/model-loader/internal/domain"
)
```

(Garantir o import block completo: `fmt`, `os`, `path/filepath`, `strings`, `syscall`, `domain`.)

- [x] **Step 4: Rodar testes — confirmar passam**

Run: `go test ./internal/service/processmgr/ -run TestReconcile -v -timeout 30s`
Expected: PASS para `DropsZombiePIDs`. `KeepsLiveLlamaServer` pode falhar se o processo do `python3` (do fake) tiver `comm = "python3"` em vez de conter `fake-llama-server.sh` ou `llama-server`.

  **Ajuste no teste em caso de falha:** o `comm` do processo é determinado pelo binário executado. Como o fake usa `exec python3 ...`, `/proc/<pid>/comm` reportará `python3`. Para o teste passar, instanciar o manager com `Binary: filepath.Base(fakeBinary(t))` não basta — `python3` vence.

  **Solução robusta no teste**: instanciar o manager fresco com `Binary: "python3"` (mesmo nome esperado pela função pidAliveAndNameMatches), simulando que esse seria o binário "real" detectado:

  Substituir, no `TestReconcile_KeepsLiveLlamaServer`, o bloco `freshMgr := New(...)` por:

```go
	freshMgr := New(Config{
		Binary:       "python3", // matches /proc/<pid>/comm of fake-llama-server.sh's exec'd interpreter
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: mgr.registryPath,
	})
```

  E continuar.

- [x] **Step 5: Re-rodar — confirmar passam ambos**

Run: `go test ./internal/service/processmgr/ -run TestReconcile -v -timeout 30s`
Expected: PASS.

- [x] **Step 6: Commit**

```bash
git add internal/service/processmgr/recover.go internal/service/processmgr/recover_test.go
git commit -m "feat(processmgr): boot Reconcile drops zombie PIDs from instances.json"
```

---

### Task 8: Update meta.lastUsedAt via ProfileStore callback

**Files:**
- Modify: `internal/service/processmgr/manager.go`
- Modify: `internal/service/processmgr/manager_test.go`

Spec § 5.4: "meta.lastUsedAt no Profile é atualizado pelo `ProcessManager.Launch` no momento do spawn bem-sucedido (após o `WaitHealthy` retornar 200), via callback no `ProfileStore.Save`".

Decisão de design: passar uma `LastUsedSink` (interface mínima, 1 método) para `Config`. Mantém `processmgr` desacoplado de `profilestore`.

- [x] **Step 1: Escrever teste falhando**

Append to `internal/service/processmgr/manager_test.go`:

```go
type sinkSpy struct {
	calls []string // ProfileIDs
}

func (s *sinkSpy) MarkLastUsed(profileID string, at time.Time) error {
	s.calls = append(s.calls, profileID)
	return nil
}

func TestManager_Launch_NotifiesLastUsedSink(t *testing.T) {
	dir := t.TempDir()
	spy := &sinkSpy{}
	mgr := New(Config{
		Binary:       fakeBinary(t),
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
		LastUsedSink: spy,
	})
	port := freePort(t)
	p := domain.Profile{ID: "tracked", Model: "/dev/null", Args: map[string]any{"port": float64(port)}}
	inst, err := mgr.Launch(p, LaunchBackground)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	if err := mgr.WaitHealthy(inst.PID, port, 5*time.Second); err != nil {
		t.Fatalf("WaitHealthy: %v", err)
	}
	if len(spy.calls) != 1 || spy.calls[0] != "tracked" {
		t.Errorf("sink.calls = %v, want [tracked]", spy.calls)
	}
}
```

- [x] **Step 2: Rodar teste — confirmar falha**

Run: `go test ./internal/service/processmgr/ -run TestManager_Launch_NotifiesLastUsedSink -v -timeout 30s`
Expected: FAIL com "Config has no field LastUsedSink".

- [x] **Step 3: Adicionar `LastUsedSink` no Config + chamar em WaitHealthy**

Em `processmgr.go`, adicionar tipo:

```go
// LastUsedSink is a minimal callback to update Profile.Meta.LastUsedAt.
// processmgr stays decoupled from profilestore via this interface.
type LastUsedSink interface {
	MarkLastUsed(profileID string, at time.Time) error
}
```

Em `manager.go`:

1. Adicionar campo em `Config`:

```go
type Config struct {
	Binary       string
	LogDir       string
	RegistryPath string
	LastUsedSink LastUsedSink
}
```

2. Adicionar campo em `fsManager`:

```go
type fsManager struct {
	binary       string
	logDir       string
	registryPath string
	sink         LastUsedSink
	// ...rest unchanged
}
```

3. Em `New`, popular `sink: cfg.LastUsedSink`.

4. Em `WaitHealthy`, ao retornar success, antes do `return nil`, adicionar:

```go
		if resp.StatusCode == http.StatusOK {
			if m.sink != nil {
				m.mu.Lock()
				inst, ok := m.tracked[pid]
				m.mu.Unlock()
				if ok {
					_ = m.sink.MarkLastUsed(inst.ProfileID, time.Now().UTC())
				}
			}
			return nil
		}
```

(O parâmetro `_ /*pid*/ int` no WaitHealthy precisa virar `pid int` agora.)

- [x] **Step 4: Rodar teste — confirmar passa**

Run: `go test ./internal/service/processmgr/ -run TestManager_Launch_NotifiesLastUsedSink -v -timeout 30s`
Expected: PASS.

- [x] **Step 5: Adicionar `MarkLastUsed` em FSStore para preencher a interface**

Em `internal/service/profilestore/fs_store.go`, adicionar:

```go
// MarkLastUsed updates Meta.LastUsedAt for the given profile id and persists.
// No-op if the profile does not exist.
func (s *FSStore) MarkLastUsed(id string, at time.Time) error {
	p, err := s.Get(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	p.Meta.LastUsedAt = &at
	return s.Save(p)
}
```

Adicionar teste em `internal/service/profilestore/fs_store_test.go`:

```go
func TestFSStore_MarkLastUsed(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(sampleProfile("u", "U")); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	if err := s.MarkLastUsed("u", now); err != nil {
		t.Fatalf("MarkLastUsed: %v", err)
	}
	got, _ := s.Get("u")
	if got.Meta.LastUsedAt == nil || !got.Meta.LastUsedAt.Equal(now) {
		t.Errorf("LastUsedAt = %v, want %v", got.Meta.LastUsedAt, now)
	}
}

func TestFSStore_MarkLastUsed_NotFoundIsNoop(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFSStore(dir)
	if err := s.MarkLastUsed("missing", time.Now()); err != nil {
		t.Errorf("expected nil for missing, got %v", err)
	}
}
```

(Imports do test file: `time`. Verificar se já está; senão adicionar.)

- [x] **Step 6: Rodar testes — confirmar passam**

Run: `go test ./internal/service/profilestore/ ./internal/service/processmgr/ -v -timeout 30s`
Expected: PASS.

- [x] **Step 7: Commit**

```bash
git add internal/service/processmgr/processmgr.go internal/service/processmgr/manager.go \
        internal/service/processmgr/manager_test.go \
        internal/service/profilestore/fs_store.go internal/service/profilestore/fs_store_test.go
git commit -m "feat(processmgr): mark profile lastUsedAt via sink callback after WaitHealthy"
```

---

## Phase A checkpoint

- [x] **Run full processmgr suite + processmgr-adjacent**

Run: `go test ./internal/service/processmgr/ ./internal/service/profilestore/ -v -timeout 60s`
Expected: todos os testes passam, sem leaks de goroutine, sem temp dirs deixados (Go limpa automaticamente via `t.TempDir`).

- [x] **Run go vet on the new package**

Run: `go vet ./internal/service/processmgr/...`
Expected: sem warnings.

---

## Phase B — UI

### Task 9: LauncherPage skeleton (profile list + selection)

**Files:**
- Create: `internal/ui/pages/launcher.go`
- Create: `internal/ui/pages/launcher_test.go`

- [x] **Step 1: Escrever teste falhando**

```go
package pages

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

func TestLauncherPage_ListsProfiles(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(domain.Profile{
		ID: "qwen", Name: "Qwen Coder", Model: "/m.gguf",
		Args: map[string]any{"port": float64(8080)},
	}); err != nil {
		t.Fatal(err)
	}

	page := NewLauncherPage(store, nil, nil) // no manager / no validator yet
	tm := teatest.NewTestModel(t, page, teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Qwen Coder")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	_ = tm.Quit()
}
```

- [x] **Step 2: Rodar teste — confirmar falha**

Run: `go test ./internal/ui/pages/ -run TestLauncherPage_ListsProfiles -v`
Expected: FAIL com "undefined: NewLauncherPage".

- [x] **Step 3: Implementar skeleton**

```go
// internal/ui/pages/launcher.go
package pages

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/service/validator"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// LauncherPage is the Tab 2 page: pick a profile, choose mode, launch.
type LauncherPage struct {
	store     profilestore.Store
	manager   processmgr.Manager
	validator validator.Validator
	schema    domain.FlagSchema

	profiles []domain.Profile
	plist    list.Model

	background bool
	status     string // last-launch result line ("launched pid=...", "error: ...")

	width, height int
	loadErr       error
}

// NewLauncherPage builds a LauncherPage. manager/validator may be nil for
// smoke tests (UI degrades gracefully and the launch action is disabled).
func NewLauncherPage(store profilestore.Store, manager processmgr.Manager, val validator.Validator) LauncherPage {
	delegate := list.NewDefaultDelegate()
	l := list.New(nil, delegate, 40, 20)
	l.Title = "Profiles"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	return LauncherPage{
		store:      store,
		manager:    manager,
		validator:  val,
		plist:      l,
		background: true,
	}
}

// SetSchema injects the FlagSchema (used by validator at launch time).
func (p LauncherPage) SetSchema(s domain.FlagSchema) LauncherPage {
	p.schema = s
	return p
}

type launcherProfilesLoadedMsg struct {
	profiles []domain.Profile
	err      error
}

type profileItem struct {
	p domain.Profile
}

func (i profileItem) Title() string       { return i.p.Name }
func (i profileItem) Description() string { return fmt.Sprintf("%s | port %v", i.p.ID, i.p.Args["port"]) }
func (i profileItem) FilterValue() string { return i.p.Name }

func (p LauncherPage) Init() tea.Cmd {
	return loadProfilesCmd(p.store)
}

func loadProfilesCmd(store profilestore.Store) tea.Cmd {
	return func() tea.Msg {
		got, err := store.List()
		return launcherProfilesLoadedMsg{profiles: got, err: err}
	}
}

func (p LauncherPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		p.plist.SetSize(msg.Width/2, msg.Height-6)
		return p, nil

	case launcherProfilesLoadedMsg:
		if msg.err != nil {
			p.loadErr = msg.err
			return p, nil
		}
		p.profiles = msg.profiles
		items := make([]list.Item, len(msg.profiles))
		for i, pr := range msg.profiles {
			items[i] = profileItem{p: pr}
		}
		p.plist.SetItems(items)
		return p, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "b":
			p.background = !p.background
			return p, nil
		}
	}

	updatedList, cmd := p.plist.Update(msg)
	p.plist = updatedList
	return p, cmd
}

func (p LauncherPage) View() string {
	if p.loadErr != nil {
		return theme.ErrorText.Render(fmt.Sprintf("load profiles: %v", p.loadErr))
	}
	left := p.plist.View()

	// Right pane: selected profile summary + mode toggle + status.
	var right string
	if it, ok := p.plist.SelectedItem().(profileItem); ok {
		mode := "Foreground"
		if p.background {
			mode = "Background"
		}
		right = lipgloss.JoinVertical(lipgloss.Left,
			theme.Subtitle.Render(it.p.Name),
			fmt.Sprintf("ID:    %s", it.p.ID),
			fmt.Sprintf("Model: %s", it.p.Model),
			fmt.Sprintf("Port:  %v", it.p.Args["port"]),
			fmt.Sprintf("Mode:  [%s]   (b to toggle)", mode),
		)
	} else {
		right = theme.Subtitle.Render("No profile selected")
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)

	footer := "[b] mode  [enter] launch  [k] kill  [r] refresh"
	if p.status != "" {
		footer = p.status + "  |  " + footer
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, footer)
}
```

(Imports usados: ajustar conforme necessário; `theme.ErrorText` deve existir no package theme — se não, usar `lipgloss.NewStyle().Foreground(...)` inline ou `theme.Subtitle`.)

- [x] **Step 4: Verificar tema**

Run: `grep -n "ErrorText\|Subtitle" internal/ui/theme/*.go`
Expected: confirmar que `Subtitle` existe; se `ErrorText` não existir, substituir por `Subtitle` no código de erro acima.

- [x] **Step 5: Rodar teste — confirmar passa**

Run: `go test ./internal/ui/pages/ -run TestLauncherPage_ListsProfiles -v`
Expected: PASS.

- [x] **Step 6: Commit**

```bash
git add internal/ui/pages/launcher.go internal/ui/pages/launcher_test.go
git commit -m "feat(ui/pages/launcher): profile list with bg/fg toggle skeleton"
```

---

### Task 10: Validate + Launch (Enter triggers manager.Launch)

**Files:**
- Modify: `internal/ui/pages/launcher.go`
- Modify: `internal/ui/pages/launcher_test.go`

- [x] **Step 1: Escrever teste falhando — usar fake Manager**

Append to `launcher_test.go`:

```go
type fakeManager struct {
	launched []domain.Profile
	mode     processmgr.LaunchMode
	nextErr  error
}

func (f *fakeManager) Launch(p domain.Profile, mode processmgr.LaunchMode) (domain.RunningInstance, error) {
	if f.nextErr != nil {
		err := f.nextErr
		f.nextErr = nil
		return domain.RunningInstance{}, err
	}
	f.launched = append(f.launched, p)
	f.mode = mode
	return domain.RunningInstance{ProfileID: p.ID, PID: 4242, Port: 8080, Background: mode == processmgr.LaunchBackground}, nil
}
func (f *fakeManager) Kill(pid int) error                                  { return nil }
func (f *fakeManager) List() []domain.RunningInstance                      { return nil }
func (f *fakeManager) WaitHealthy(_, _ int, _ time.Duration) error         { return nil }

func TestLauncherPage_EnterLaunchesSelected(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	_ = store.Save(domain.Profile{
		ID: "alpha", Name: "Alpha", Model: "/m.gguf",
		Args: map[string]any{"port": float64(8080)},
	})

	mgr := &fakeManager{}
	page := NewLauncherPage(store, mgr, nil)

	model, _ := page.Update(launcherProfilesLoadedMsg{profiles: []domain.Profile{{
		ID: "alpha", Name: "Alpha", Model: "/m.gguf",
		Args: map[string]any{"port": float64(8080)},
	}}})
	page = model.(LauncherPage)

	updated, cmd := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = updated.(LauncherPage)

	// Drain the cmd to deliver the LaunchedMsg (or LaunchErrMsg).
	if cmd == nil {
		t.Fatal("Enter did not produce a tea.Cmd")
	}
	msg := cmd()
	switch m := msg.(type) {
	case launchedMsg:
		if m.inst.ProfileID != "alpha" {
			t.Errorf("launched ProfileID = %s, want alpha", m.inst.ProfileID)
		}
	case launchErrMsg:
		t.Fatalf("got launchErrMsg: %v", m.err)
	default:
		t.Fatalf("unexpected msg type: %T", msg)
	}

	if len(mgr.launched) != 1 {
		t.Errorf("manager.launched len = %d, want 1", len(mgr.launched))
	}
	if mgr.mode != processmgr.LaunchBackground {
		t.Errorf("mode = %v, want LaunchBackground", mgr.mode)
	}
}

func TestLauncherPage_ValidationBlocksLaunch(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)

	bad := domain.Profile{
		ID: "bad", Name: "Bad", Model: "/m.gguf",
		Args: map[string]any{
			"port":        float64(8080),
			"batch-size":  float64(1024),
			"ubatch-size": float64(2048), // > batch-size -> error
		},
	}
	mgr := &fakeManager{}
	page := NewLauncherPage(store, mgr, validator.New())
	model, _ := page.Update(launcherProfilesLoadedMsg{profiles: []domain.Profile{bad}})
	page = model.(LauncherPage)

	_, cmd := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a cmd (with launchErrMsg), got nil")
	}
	msg := cmd()
	if _, ok := msg.(launchErrMsg); !ok {
		t.Fatalf("expected launchErrMsg from validation, got %T", msg)
	}
	if len(mgr.launched) != 0 {
		t.Errorf("manager.launched len = %d, want 0 (validation should have blocked)", len(mgr.launched))
	}
}
```

(Imports adicionais necessários no teste: `"github.com/quantmind-br/model-loader/internal/service/processmgr"` e `"github.com/quantmind-br/model-loader/internal/service/validator"`.)

- [x] **Step 2: Rodar testes — confirmar falham**

Run: `go test ./internal/ui/pages/ -run "TestLauncherPage_Enter|TestLauncherPage_Validation" -v`
Expected: FAIL — `launchedMsg`/`launchErrMsg` não definidos; Enter não trata launch.

- [x] **Step 3: Implementar handlers**

Em `launcher.go`, adicionar tipos de msg e expandir `Update`:

```go
// LaunchedMsg is emitted after a successful Launch + WaitHealthy.
type launchedMsg struct {
	inst domain.RunningInstance
}

// LaunchErrMsg is emitted when validation or Launch itself fails.
type launchErrMsg struct {
	err error
}
```

Adicionar caso na switch da `Update` em `tea.KeyMsg`:

```go
		case "enter":
			it, ok := p.plist.SelectedItem().(profileItem)
			if !ok || p.manager == nil {
				return p, nil
			}
			selected := it.p
			val := p.validator
			schema := p.schema
			mgr := p.manager
			mode := processmgr.LaunchBackground
			if !p.background {
				mode = processmgr.LaunchForeground
			}
			return p, func() tea.Msg {
				if val != nil {
					rep := val.Validate(selected, schema)
					if rep.HasBlockingErrors() {
						return launchErrMsg{err: fmt.Errorf("validation failed: %d errors", len(rep.Errors))}
					}
				}
				inst, err := mgr.Launch(selected, mode)
				if err != nil {
					return launchErrMsg{err: err}
				}
				return launchedMsg{inst: inst}
			}
```

E adicionar handlers para os msgs (atualizar status string e potencialmente disparar WaitHealthy):

```go
	case launchedMsg:
		p.status = fmt.Sprintf("launched %s pid=%d port=%d", msg.inst.ProfileID, msg.inst.PID, msg.inst.Port)
		// Trigger background health check; ignore errors here (status updated on completion).
		mgr := p.manager
		port := msg.inst.Port
		pid := msg.inst.PID
		return p, func() tea.Msg {
			if err := mgr.WaitHealthy(pid, port, 30*time.Second); err != nil {
				return launchErrMsg{err: fmt.Errorf("pid %d not healthy: %w", pid, err)}
			}
			return healthyMsg{pid: pid}
		}

	case healthyMsg:
		p.status = fmt.Sprintf("healthy pid=%d", msg.pid)
		return p, nil

	case launchErrMsg:
		p.status = "error: " + msg.err.Error()
		return p, nil
```

E adicionar tipo:

```go
type healthyMsg struct{ pid int }
```

Adicionar import `"time"` em launcher.go (e `"github.com/quantmind-br/model-loader/internal/service/processmgr"` já está).

- [x] **Step 4: Rodar testes — confirmar passam**

Run: `go test ./internal/ui/pages/ -run "TestLauncherPage" -v -timeout 30s`
Expected: PASS para os 3 casos (`ListsProfiles`, `EnterLaunchesSelected`, `ValidationBlocksLaunch`).

- [x] **Step 5: Commit**

```bash
git add internal/ui/pages/launcher.go internal/ui/pages/launcher_test.go
git commit -m "feat(ui/pages/launcher): validate + launch via manager on enter"
```

---

### Task 11: Running instances panel + kill keybinding

**Files:**
- Modify: `internal/ui/pages/launcher.go`
- Modify: `internal/ui/pages/launcher_test.go`

- [x] **Step 1: Escrever teste falhando**

Append to `launcher_test.go`:

```go
func TestLauncherPage_KillRemovesInstance(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	_ = store.Save(domain.Profile{
		ID: "alpha", Name: "Alpha", Model: "/m.gguf",
		Args: map[string]any{"port": float64(8080)},
	})

	mgr := &fakeManager{}
	page := NewLauncherPage(store, mgr, nil)

	// Inject one running instance via launchedMsg.
	model, _ := page.Update(launchedMsg{inst: domain.RunningInstance{ProfileID: "alpha", PID: 4242, Port: 8080, Background: true}})
	page = model.(LauncherPage)
	if len(page.running) != 1 {
		t.Fatalf("running len = %d, want 1", len(page.running))
	}

	// Press 'k' — should call mgr.Kill(4242) and drop from page.running.
	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	page = updated.(LauncherPage)
	if len(page.running) != 0 {
		t.Errorf("running len after kill = %d, want 0", len(page.running))
	}
}
```

- [x] **Step 2: Rodar teste — confirmar falha**

Run: `go test ./internal/ui/pages/ -run TestLauncherPage_KillRemoves -v`
Expected: FAIL — campo `running` ainda não existe.

- [x] **Step 3: Implementar painel Running + handler 'k'**

Em `launcher.go`:

1. Adicionar campo:

```go
type LauncherPage struct {
	// ...existing fields
	running []domain.RunningInstance
}
```

2. Em `Update`, no `case launchedMsg`, append à lista:

```go
	case launchedMsg:
		p.running = append(p.running, msg.inst)
		p.status = fmt.Sprintf("launched %s pid=%d port=%d", msg.inst.ProfileID, msg.inst.PID, msg.inst.Port)
		// (rest unchanged)
```

3. Adicionar caso `'k'` no switch de `tea.KeyMsg`:

```go
		case "k":
			if len(p.running) == 0 || p.manager == nil {
				return p, nil
			}
			pid := p.running[len(p.running)-1].PID
			if err := p.manager.Kill(pid); err != nil {
				p.status = "error: " + err.Error()
				return p, nil
			}
			out := p.running[:0]
			for _, ri := range p.running {
				if ri.PID != pid {
					out = append(out, ri)
				}
			}
			p.running = out
			p.status = fmt.Sprintf("killed pid=%d", pid)
			return p, nil
```

4. Atualizar `View` para incluir painel "Running" abaixo do body:

Substituir o `body := lipgloss.JoinHorizontal(...)` block por:

```go
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)

	runningView := "Running: (none)"
	if len(p.running) > 0 {
		lines := []string{theme.Subtitle.Render("Running")}
		for _, ri := range p.running {
			tag := "fg"
			if ri.Background {
				tag = "bg"
			}
			lines = append(lines, fmt.Sprintf("  %s pid=%d port=%d %s", ri.ProfileID, ri.PID, ri.Port, tag))
		}
		runningView = strings.Join(lines, "\n")
	}

	footer := "[b] mode  [enter] launch  [k] kill  [r] refresh"
	if p.status != "" {
		footer = p.status + "  |  " + footer
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, "", runningView, footer)
```

Adicionar import `"strings"` no topo do arquivo.

- [x] **Step 4: Rodar testes — confirmar passam**

Run: `go test ./internal/ui/pages/ -v -timeout 30s`
Expected: PASS para todos os `TestLauncherPage_*`.

- [x] **Step 5: Commit**

```bash
git add internal/ui/pages/launcher.go internal/ui/pages/launcher_test.go
git commit -m "feat(ui/pages/launcher): running instances panel and kill action"
```

---

### Task 12: Refresh ('r') reloads profiles

**Files:**
- Modify: `internal/ui/pages/launcher.go`
- Modify: `internal/ui/pages/launcher_test.go`

- [x] **Step 1: Escrever teste falhando**

Append to `launcher_test.go`:

```go
func TestLauncherPage_RefreshReloadsProfiles(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewLauncherPage(store, nil, nil)

	// 0 profiles initially.
	model, _ := page.Update(launcherProfilesLoadedMsg{profiles: nil})
	page = model.(LauncherPage)
	if len(page.profiles) != 0 {
		t.Fatalf("initial profiles = %d, want 0", len(page.profiles))
	}

	// Add one to disk.
	if err := store.Save(domain.Profile{ID: "x", Name: "X", Model: "/m.gguf", Args: map[string]any{"port": float64(8080)}}); err != nil {
		t.Fatal(err)
	}

	// Press 'r' -> cmd that yields a fresh launcherProfilesLoadedMsg.
	updated, cmd := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	page = updated.(LauncherPage)
	if cmd == nil {
		t.Fatal("'r' did not produce reload cmd")
	}
	msg := cmd()
	loaded, ok := msg.(launcherProfilesLoadedMsg)
	if !ok {
		t.Fatalf("got %T, want launcherProfilesLoadedMsg", msg)
	}
	if len(loaded.profiles) != 1 || loaded.profiles[0].ID != "x" {
		t.Errorf("reloaded profiles = %v", loaded.profiles)
	}
}
```

- [x] **Step 2: Rodar teste — confirmar falha**

Run: `go test ./internal/ui/pages/ -run TestLauncherPage_RefreshReloads -v`
Expected: FAIL — 'r' não tratado.

- [x] **Step 3: Adicionar handler 'r'**

Em `launcher.go`, no switch de `tea.KeyMsg`:

```go
		case "r":
			return p, loadProfilesCmd(p.store)
```

- [x] **Step 4: Rodar — confirmar passa**

Run: `go test ./internal/ui/pages/ -run TestLauncherPage_RefreshReloads -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/ui/pages/launcher.go internal/ui/pages/launcher_test.go
git commit -m "feat(ui/pages/launcher): 'r' refreshes profile list"
```

---

### Task 13: Wire root.WithLauncherPage

**Files:**
- Modify: `internal/ui/root.go`
- Modify: `internal/ui/root_test.go`

- [x] **Step 1: Escrever teste falhando**

Em `internal/ui/root_test.go`, append:

```go
func TestRoot_TabSwitchToLauncherShowsPage(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(domain.Profile{
		ID: "alpha", Name: "AlphaProfile", Model: "/m.gguf",
		Args: map[string]any{"port": float64(8080)},
	}); err != nil {
		t.Fatal(err)
	}

	root := NewRoot(TabProfiles).
		WithLauncherPage(pages.NewLauncherPage(store, nil, nil))

	tm := teatest.NewTestModel(t, root, teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "AlphaProfile")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	_ = tm.Quit()
}
```

- [x] **Step 2: Rodar teste — confirmar falha**

Run: `go test ./internal/ui/ -run TestRoot_TabSwitchToLauncher -v -timeout 30s`
Expected: FAIL — `RootModel.WithLauncherPage` não existe.

- [x] **Step 3: Adicionar builder em root.go**

Em `internal/ui/root.go`, após `WithModelsPage`:

```go
// WithLauncherPage replaces the placeholder Launcher tab with a real model.
func (m RootModel) WithLauncherPage(p tea.Model) RootModel {
	m.pages[TabLauncher] = p
	return m
}
```

- [x] **Step 4: Rodar — confirmar passa**

Run: `go test ./internal/ui/ -run TestRoot_ -v -timeout 30s`
Expected: PASS para todos os `TestRoot_*`.

- [x] **Step 5: Commit**

```bash
git add internal/ui/root.go internal/ui/root_test.go
git commit -m "feat(ui/root): WithLauncherPage builder"
```

---

### Task 14: Wire main.go

**Files:**
- Modify: `cmd/model-loader/main.go`

- [x] **Step 1: Atualizar main.go**

Em `cmd/model-loader/main.go`, adicionar import `"path/filepath"` e `"github.com/quantmind-br/model-loader/internal/service/processmgr"` e `"github.com/quantmind-br/model-loader/internal/service/validator"` (se ainda não estiverem). Substituir o bloco que constrói `root` por:

```go
	scanner := modelscanner.New()

	mgr := processmgr.New(processmgr.Config{
		LogDir:       cfg.Paths.LogDir,
		RegistryPath: filepath.Join(cfg.Paths.StateDir, "instances.json"),
		LastUsedSink: store,
	})
	if err := mgr.Reconcile(); err != nil {
		fmt.Fprintf(os.Stderr, "instance recovery: %v\n", err)
	}

	val := validator.New()

	profilesPage := pages.NewProfilesPage(store, schema).
		WithModelScanner(scanner, cfg.Models.SearchPaths)
	modelsPage := pages.NewModelsPage(scanner, cfg.Models.SearchPaths)
	launcherPage := pages.NewLauncherPage(store, mgr, val).SetSchema(schema)

	root := ui.NewRoot(parseTab(cfg.UI.DefaultTab)).
		WithProfilesPage(profilesPage).
		WithModelsPage(modelsPage).
		WithLauncherPage(launcherPage)
```

- [x] **Step 2: Garantir compilação**

Run: `go build ./...`
Expected: success. Se `processmgr.New` retornar `*fsManager` mas Manager é uma interface, e `Config.LastUsedSink` é `LastUsedSink` (interface) — `*FSStore` precisa satisfazer `LastUsedSink` (i.e. ter `MarkLastUsed(string, time.Time) error`). Já adicionado em Task 8 step 5. Se faltar, voltar ao Task 8.

- [x] **Step 3: Rodar test suite completa**

Run: `go test ./... -timeout 60s`
Expected: todos os testes passam.

- [x] **Step 4: Verificar binário**

Run: `go build -o /tmp/model-loader ./cmd/model-loader && ls -lh /tmp/model-loader && /tmp/model-loader --version 2>&1 | head -1 || true`
Expected: binário criado (~10-12 MB). Não há flag `--version` na app TUI; o teste serve só para garantir que a app inicia (ou falha de forma esperada por falta de TTY ao tentar ir para alt screen).

- [x] **Step 5: Commit**

```bash
git add cmd/model-loader/main.go
git commit -m "feat(cmd): wire ProcessManager and LauncherPage into root"
```

---

## Phase B checkpoint

- [x] **Run full suite + go vet**

Run: `go test ./... -timeout 90s && go vet ./...`
Expected: PASS, sem warnings.

- [ ] **Smoke teste manual da TUI (opcional, se houver fake-llama-server.sh disponível e python3 instalado)** *(skipped — optional sanity-check, automated suite already green)*

```bash
# Build
go build -o /tmp/model-loader ./cmd/model-loader

# Override binário usando PATH:
export PATH="$(pwd)/testdata:$PATH"
ln -sf "$(pwd)/testdata/fake-llama-server.sh" /tmp/llama-server
PATH="/tmp:$PATH" /tmp/model-loader
```

Navegar para tab `2` (Launcher), selecionar profile, apertar Enter. Confirmar que aparece "launched ... port=...". Apertar `k` para matar. Apertar `q` para sair.

(Não comprometer; só sanity-check.)

---

## Self-Review Checklist

**1. Spec coverage (§ 6.4 ProcessManager + § 7.2 launcherPage + § 5.3 instances.json + F4):**

| Spec | Coberto por |
|------|-------------|
| `Launch(p, mode)` | T5 (bg) + T6 (fg) |
| `Kill(pid)` | T5 |
| `List()` | T5 |
| `WaitHealthy(pid, port, timeout)` | T5 |
| `TailLogs(pid)` | **DEFERIDO para slice 5** (header do plan) |
| Setsid background | T5 step 4 |
| Foreground single-instance | T6 |
| Health retry exponencial | T5 step 4 (`WaitHealthy` com backoff cap 1s) |
| Boot recovery via PID alive + comm | T7 |
| `instances.json` atomic write | T3 |
| `meta.lastUsedAt` callback | T8 |
| launcherPage profile list | T9 |
| launcherPage bg/fg toggle | T9 |
| launcherPage validate-before-launch | T10 |
| launcherPage Enter launches | T10 |
| launcherPage running instances | T11 |
| launcherPage kill | T11 |
| Auto-switch para Monitor | **DEFERIDO para slice 5** (header do plan) |

**2. Placeholder scan:** verificado — não há "TBD", "implement later", "add error handling", referências a métodos não definidos. `TailLogs` e Monitor são deferrals **explicitamente documentados** no header.

**3. Type consistency:** `Manager`/`fsManager`/`Config`/`LaunchMode`/`LaunchBackground`/`LaunchForeground`/`LastUsedSink` consistentes em todos os tasks. `launchedMsg`/`launchErrMsg`/`healthyMsg`/`launcherProfilesLoadedMsg` lower-case (private à package pages).

**4. Cross-task references:**
- T8 estende `Config` adicionado em T1; coerente.
- T8 step 5 modifica `FSStore` para satisfazer interface definida em T8 step 3. Sem ordem invertida.
- T10 usa `validator.New()` (existente desde slice 2) — sem novidade.
- T11/T12 dependem de campos adicionados em tasks anteriores; ordem linear.
- T13 depende de `LauncherPage` existir (T9). T14 depende de tudo.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-28-model-loader-slice-4.md`. Two execution options:

1. **Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** — Execute tasks in this session using `executing-plans`, batch execution with checkpoints.

Which approach?

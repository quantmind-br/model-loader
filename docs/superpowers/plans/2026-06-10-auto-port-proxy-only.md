# Automatic Port Assignment + Proxy-Only Communication — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Instances get OS-assigned ephemeral ports at launch; the HTTP proxy (`model-loader serve`) becomes the only client-facing channel to profile backends.

**Architecture:** `processmgr` allocates a loopback ephemeral port per launch and injects it into the backend args (any profile-supplied `port` is discarded and stripped from storage). The proxy auto-starts with the TUI; TUI/CLI/benchmark drive backends exclusively through `POST /_admin/load` / `/_admin/unload` and send inference to the proxy base URL. Internal plumbing (`WaitHealthy`, reverse-proxy target) keeps direct port access inside the proxy process.

**Tech Stack:** Go 1.26, bubbletea/teatest, net/http, httptest.

**Spec:** `docs/superpowers/specs/2026-06-10-auto-port-proxy-only-design.md`

**Behavioral consequences (approved):** one loaded model at a time (proxy swap semantics); foreground launch removed from TUI and CLI entry points (the `processmgr` foreground machinery stays but becomes unreachable from the UI).

**Note for every task:** run `make build` before committing — cross-package breakage is the main risk here. Project has no linter; `go test ./<pkg>/...` + `make build` is the bar.

---

### Task 1: processmgr — ephemeral port allocation

**Files:**
- Modify: `internal/service/processmgr/launch.go`
- Modify: `internal/service/processmgr/processmgr.go` (remove `ErrPortBusy`, line ~58)
- Modify: `internal/ui/pages/profiles_launch.go:213-226` (remove `ErrPortBusy` case so the build stays green)
- Test: `internal/service/processmgr/launch_test.go` / `manager_test.go` (wherever `prepareLaunch`/port tests live — find with `grep -rn "portFromProfile\|ErrPortBusy\|checkPortFree" internal/service/processmgr/*_test.go`)

- [ ] **Step 1: Write the failing test**

In the processmgr test file that already covers launch arg building (use the existing `newTestManager(t)`/`fakeBinary(t)` helpers; mirror how existing tests build a valid profile — model path must exist, e.g. point `Model` at the fake binary file or whatever existing tests use):

```go
func TestPrepareLaunch_AllocatesEphemeralPort(t *testing.T) {
	m := newTestManager(t)
	p := validTestProfile(t) // reuse however existing launch tests construct one
	p.Args = map[string]any{"port": float64(8080), "ctx-size": float64(4096)}

	plan, err := m.prepareLaunch(p)
	if err != nil {
		t.Fatalf("prepareLaunch: %v", err)
	}
	if plan.port == 0 || plan.port == 8080 {
		t.Fatalf("want OS-allocated port, got %d", plan.port)
	}
	// The allocated port must be on the command line; the user's 8080 must not.
	joined := strings.Join(plan.args, " ")
	if !strings.Contains(joined, "--port "+strconv.Itoa(plan.port)) {
		t.Fatalf("args missing injected --port %d: %v", plan.port, plan.args)
	}
	if strings.Contains(joined, "8080") {
		t.Fatalf("user-supplied port leaked into args: %v", plan.args)
	}
	// The caller's Args map must not be mutated.
	if got := p.Args["port"]; got != float64(8080) {
		t.Fatalf("caller Args mutated: %v", got)
	}
}

func TestPrepareLaunch_DistinctPortsPerCall(t *testing.T) {
	m := newTestManager(t)
	p := validTestProfile(t)
	a, err := m.prepareLaunch(p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.prepareLaunch(p)
	if err != nil {
		t.Fatal(err)
	}
	if a.port == b.port {
		t.Fatalf("two launches got the same port %d", a.port)
	}
}
```

(Distinct-ports is best-effort — the OS won't reuse a just-released ephemeral port immediately, so this is stable in practice.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/processmgr/ -run TestPrepareLaunch -v`
Expected: FAIL — `prepareLaunch` currently errors or returns port 8080.

- [ ] **Step 3: Implement allocation in `launch.go`**

Replace `portFromProfile` + `checkPortFree` usage in `prepareLaunch` (lines 27-33):

```go
func (m *fsManager) prepareLaunch(p domain.Profile) (launchPlan, error) {
	port, err := allocateEphemeralPort()
	if err != nil {
		return launchPlan{}, err
	}
	// The manager owns the port. Clone Args so the caller's map is untouched,
	// discard any user-supplied value, and inject the allocated one so every
	// backend arg builder emits its --port flag.
	args := make(map[string]any, len(p.Args)+1)
	for k, v := range p.Args {
		args[k] = v
	}
	args["port"] = port
	p.Args = args
	// ... rest unchanged (resolver, BuildArgsForBackend, return launchPlan{..., port: port})
}
```

Add below `Launch`:

```go
// allocateEphemeralPort asks the OS for a free loopback port by binding
// 127.0.0.1:0 and immediately releasing it. The window between Close and the
// backend's own bind is accepted — the OS does not reuse a just-released
// ephemeral port under normal churn.
func allocateEphemeralPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate port: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port, nil
}
```

Delete `portFromProfile` (lines 151-175) and `checkPortFree` (lines 177-184).

- [ ] **Step 4: Fix the foreground double-allocation bug**

`Launch` calls `prepareLaunch` (line 84) and then `launchForeground` calls it **again** (line 191) — with ephemeral allocation that would launch on a different port than the one recorded. Change `launchForeground`'s signature to receive the plan:

```go
// in Launch:
if mode == LaunchForeground {
	return m.launchForeground(p, plan, attemptID)
}

// signature change:
func (m *fsManager) launchForeground(p domain.Profile, plan launchPlan, attemptID string) (domain.RunningInstance, error) {
```

Inside `launchForeground`: delete its own `prepareLaunch` call (lines 191-194) and replace every use of the old `port` parameter with `plan.port`.

- [ ] **Step 5: Remove `ErrPortBusy`**

- `internal/service/processmgr/processmgr.go`: delete the `ErrPortBusy = errors.New("port already in use")` sentinel.
- `internal/ui/pages/profiles_launch.go` `friendlyLaunchError`: delete the `case errors.Is(err, processmgr.ErrPortBusy):` branch.
- Delete/update any processmgr tests asserting `ErrPortBusy` or testing `portFromProfile`/`checkPortFree` (grep in step Files above). Tests that set `Args["port"]` to make launches work should keep working (the value is now ignored) — only assertions about specific ports need updating.

- [ ] **Step 6: Run package tests and build**

Run: `go test ./internal/service/processmgr/... ./internal/ui/pages/ && make build`
Expected: PASS. If a pages test asserted the port-busy message, update it.

- [ ] **Step 7: Commit**

```bash
git add internal/service/processmgr/ internal/ui/pages/profiles_launch.go
git commit -m "feat(processmgr): allocate ephemeral instance ports at launch"
```

---

### Task 2: profilestore — strip `port` from persisted profiles

**Files:**
- Modify: `internal/service/profilestore/fs_store.go`
- Test: `internal/service/profilestore/fs_store_test.go` (or wherever Get/Save/Duplicate tests live)

- [ ] **Step 1: Write the failing tests**

```go
func TestGet_StripsReservedPortArg(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw := `{"id":"p1","name":"P1","model":"/m.gguf","args":{"port":8080,"ctx-size":4096}}`
	if err := os.WriteFile(filepath.Join(dir, "p1.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := s.Get("p1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Args["port"]; ok {
		t.Fatal("port must be stripped on load")
	}
	if p.Args["ctx-size"] != float64(4096) {
		t.Fatal("other args must survive")
	}
	// The strip must be persisted back so the file is migrated.
	data, _ := os.ReadFile(filepath.Join(dir, "p1.json"))
	if strings.Contains(string(data), `"port"`) {
		t.Fatal("port must be removed from the on-disk file")
	}
}

func TestSave_StripsReservedPortArg(t *testing.T) {
	s, _ := NewFSStore(t.TempDir())
	p := domain.Profile{ID: "p2", Name: "P2", Args: map[string]any{"port": float64(9090)}}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("p2")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Args["port"]; ok {
		t.Fatal("port must be stripped on save")
	}
	// Caller's map must not be mutated.
	if p.Args["port"] != float64(9090) {
		t.Fatal("caller Args mutated by Save")
	}
}

func TestDuplicate_NoPortHandling(t *testing.T) {
	s, _ := NewFSStore(t.TempDir())
	if err := s.Save(domain.Profile{ID: "src", Name: "Src", Args: map[string]any{"ctx-size": float64(2048)}}); err != nil {
		t.Fatal(err)
	}
	dup, err := s.Duplicate("src", "dst")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := dup.Args["port"]; ok {
		t.Fatal("duplicate must not invent a port")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/service/profilestore/ -run 'TestGet_Strips|TestSave_Strips|TestDuplicate_NoPort' -v`
Expected: FAIL (port present after load/save).

- [ ] **Step 3: Implement**

In `fs_store.go`:

```go
// reservedArgs are launch parameters owned by the process manager. They are
// removed on read and write so stored profiles can never carry them; the
// manager assigns them at launch time.
var reservedArgs = []string{"port"}

// stripReservedArgs removes manager-owned keys from p.Args, cloning the map
// first so the caller's copy is untouched. Reports whether anything changed.
func stripReservedArgs(p *domain.Profile) bool {
	hit := false
	for _, k := range reservedArgs {
		if _, ok := p.Args[k]; ok {
			hit = true
			break
		}
	}
	if !hit {
		return false
	}
	args := make(map[string]any, len(p.Args))
	for k, v := range p.Args {
		args[k] = v
	}
	for _, k := range reservedArgs {
		delete(args, k)
	}
	p.Args = args
	return true
}
```

- `Get` (around lines 98-107): after `MigrateProfile(&p)`, add `stripped := stripReservedArgs(&p)` and extend the persist-back condition: `if idChanged || p.SchemaVersion != oldVersion || stripped {`.
- `Save` (top, after the `ErrInvalidID` guard): `stripReservedArgs(&p)`.
- `Duplicate`: delete the port-bump block (lines 222-228).
- Delete `usedPorts`, `nextFreePort`, `portAsInt`, and `cloneArgs` (all now unused; `cloneArgs` is only used by the deleted block — verify with `grep`).
- Delete/update existing tests that covered `usedPorts`/`nextFreePort`/port-bump-on-duplicate.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/service/profilestore/... && make build`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/profilestore/
git commit -m "feat(profilestore): strip reserved port arg from stored profiles"
```

---

### Task 3: web editor — hide and reject the reserved `port` flag

**Files:**
- Modify: `internal/service/backendschema/presentation.go:15-20` (remove `"port"` from every `essentialSeed` list)
- Modify: `internal/service/configweb/viewmodel.go`
- Modify: `internal/service/configweb/draft.go` (`coerceArgs`)
- Test: `internal/service/configweb/viewmodel_test.go`, `internal/service/configweb/draft_test.go`

- [ ] **Step 1: Write the failing tests**

In `viewmodel_test.go` — note the existing test at lines ~32-39 asserts `port` IS in the first group; invert it:

```go
func TestBuildViewModel_HidesReservedPortFlag(t *testing.T) {
	// Schema and presentation both mention "port" (legacy persisted schemas do);
	// the rendered form must omit it everywhere.
	schema := domain.BackendValidationSchema{
		BackendID: "b1",
		Flags: map[string]domain.FlagSpec{
			"port":     {Long: "port", Type: domain.FlagTypeInt},
			"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt},
		},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{
			{Name: "Essentials", Highlighted: true, Flags: []string{"port", "ctx-size"}},
		}},
	}
	vm := BuildViewModel(Draft{}, schema, nil)
	for _, g := range vm.Groups {
		for _, f := range g.Fields {
			if f.Flag == "port" {
				t.Fatal("port must not render in form groups")
			}
		}
	}
	for _, f := range vm.AllFlags {
		if f.Flag == "port" {
			t.Fatal("port must not render in customize AllFlags")
		}
	}
}
```

(Adjust the `Presentation` literal to the real type names — check `internal/domain` for the exact struct names used by `schema.Presentation.Groups`; the existing `viewmodel_test.go` shows the right literals to copy.)

In `draft_test.go`:

```go
func TestCoerceArgs_DropsReservedPort(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"port":     {Long: "port", Type: domain.FlagTypeInt},
		"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt},
	}}
	got := coerceArgs(map[string]string{"port": "8080", "ctx-size": "4096"}, schema)
	if _, ok := got["port"]; ok {
		t.Fatal("forged port form field must be dropped")
	}
	if got["ctx-size"] == nil {
		t.Fatal("ctx-size must survive")
	}
}
```

(Match `coerceArgs`'s real signature — see `draft.go:61`.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/service/configweb/ -run 'HidesReservedPort|DropsReservedPort' -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

In `viewmodel.go`:

```go
// reservedFlags are launch parameters owned by the process manager; they are
// hidden from the editor and dropped on submit so profiles can never set them.
var reservedFlags = map[string]bool{"port": true}
```

- In `BuildViewModel`'s group loop (line ~63): `if reservedFlags[long] { continue }` before the `schema.Flags[long]` lookup.
- In the `AllFlags` loop (line ~80): same guard.

In `draft.go` `coerceArgs`: skip `reservedFlags[k]` keys at the top of its range loop.

In `backendschema/presentation.go`: remove `"port"` from all six `essentialSeed` lists (user-approved exception to the "never edit essentialSeed" rule — cite the spec in the commit body).

- [ ] **Step 4: Update the inverted legacy test**

`viewmodel_test.go:32-39` asserts port appears in the first group — delete that assertion (keep the ctx-size half).

- [ ] **Step 5: Run tests**

Run: `go test ./internal/service/configweb/... ./internal/service/backendschema/... && make build`
Expected: PASS. If backendschema golden fixtures embed `essentialSeed`, refresh with `go test ./internal/service/backendschema/... -update` and re-run.

- [ ] **Step 6: Commit**

```bash
git add internal/service/configweb/ internal/service/backendschema/
git commit -m "feat(configweb): treat port as reserved manager-owned flag

Per docs/superpowers/specs/2026-06-10-auto-port-proxy-only-design.md:
port is removed from essentialSeed and filtered from the editor."
```

---

### Task 4: httpproxy — expose `loaded_log_path` in Status

**Files:**
- Modify: `internal/service/httpproxy/server.go` (Status struct, `loadedBackend`, `Status()`)
- Modify: `internal/service/httpproxy/handler.go` (`ensureLoaded`)
- Test: `internal/service/httpproxy/handler_test.go` (or `swap_test.go`, wherever a successful load asserts Status fields)

- [ ] **Step 1: Write the failing test**

Find the existing test that loads a profile through `/_admin/load` or `handleForward` using the package mocks (`mocks_test.go`) and asserts `LoadedPID`/`LoadedPort`. Add alongside it (mirroring its setup):

```go
func TestStatus_IncludesLoadedLogPath(t *testing.T) {
	// setup identical to the existing admin-load success test; ensure the
	// mock ProcessMgr's Launch returns RunningInstance{..., LogPath: "/tmp/x.log"}
	// ... perform the load ...
	st := srv.Status()
	if st.LoadedLogPath != "/tmp/x.log" {
		t.Fatalf("LoadedLogPath = %q, want /tmp/x.log", st.LoadedLogPath)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/service/httpproxy/ -run TestStatus_IncludesLoadedLogPath -v`
Expected: FAIL (compile error — field missing).

- [ ] **Step 3: Implement**

`server.go`:
- `Status` struct: add `LoadedLogPath string \`json:"loaded_log_path,omitempty"\`` after `LoadedPort`. (No legacy alias needed in `UnmarshalJSON` — the field is new; old proxies simply omit it.)
- `loadedBackend`: add `logPath string`.
- `Status()`: inside `if cur != nil { ... }` add `st.LoadedLogPath = cur.logPath`.

`handler.go` `ensureLoaded` (line ~207):

```go
loaded := &loadedBackend{
	profileID: profileID,
	pid:       inst.PID,
	port:      inst.Port,
	logPath:   inst.LogPath,
	proxy:     newReverseProxy(inst.Port),
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/service/httpproxy/... && make build`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/httpproxy/
git commit -m "feat(httpproxy): expose loaded_log_path in status snapshot"
```

---

### Task 5: proxysupervisor — admin client (EnsureRunning / Load / Unload / BaseURL)

**Files:**
- Create: `internal/service/proxysupervisor/client.go`
- Test: `internal/service/proxysupervisor/client_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package proxysupervisor

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// testSupervisor returns a Supervisor whose host/port point at ts.
func testSupervisor(t *testing.T, ts *httptest.Server) *Supervisor {
	t.Helper()
	host, portStr, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	return New(Config{Host: host, Port: port, StatePath: t.TempDir() + "/state.json", LogDir: t.TempDir()})
}

func TestLoad_PostsProfileID(t *testing.T) {
	var gotBody map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_admin/load" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"running": true, "loaded_profile_id": "p1", "loaded_pid": 42,
			"loaded_log_path": "/tmp/p1.log",
		})
	}))
	defer ts.Close()

	st, err := testSupervisor(t, ts).Load(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["profile_id"] != "p1" {
		t.Fatalf("body = %v", gotBody)
	}
	if st.LoadedPID != 42 || st.LoadedLogPath != "/tmp/p1.log" {
		t.Fatalf("status = %+v", st)
	}
}

func TestLoad_SurfacesOpenAIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"profile \"nope\" not found","type":"invalid_request_error"}}`))
	}))
	defer ts.Close()

	_, err := testSupervisor(t, ts).Load(context.Background(), "nope")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnload_Posts(t *testing.T) {
	var gotPath, gotQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"running": true})
	}))
	defer ts.Close()

	if _, err := testSupervisor(t, ts).Unload(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/_admin/unload" || gotQuery != "force=true" {
		t.Fatalf("%s?%s", gotPath, gotQuery)
	}
}

func TestBaseURL(t *testing.T) {
	s := New(Config{Host: "127.0.0.1", Port: 4321})
	if got := s.BaseURL(); got != "http://127.0.0.1:4321" {
		t.Fatalf("BaseURL = %q", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/service/proxysupervisor/ -run 'TestLoad_|TestUnload_|TestBaseURL' -v`
Expected: FAIL (compile error — methods missing).

- [ ] **Step 3: Implement `client.go`**

```go
package proxysupervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
)

// BaseURL returns the proxy's HTTP root, e.g. "http://127.0.0.1:4321". It is
// derived from configuration, so it is valid even before the proxy starts.
func (s *Supervisor) BaseURL() string {
	return "http://" + net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))
}

// EnsureRunning starts the proxy when it is not already alive. Unlike Start,
// an already-running proxy is a success, not an error.
func (s *Supervisor) EnsureRunning(ctx context.Context) error {
	if s.Status().Running {
		return nil
	}
	return s.Start(ctx)
}

// Load asks the proxy to swap in profileID via POST /_admin/load. It blocks
// until the backend is healthy — the proxy only answers after its health
// check — so callers should pass a ctx with a generous deadline (model load
// can take minutes).
func (s *Supervisor) Load(ctx context.Context, profileID string) (httpproxy.Status, error) {
	body, _ := json.Marshal(map[string]string{"profile_id": profileID})
	return s.adminPost(ctx, s.BaseURL()+"/_admin/load", bytes.NewReader(body))
}

// Unload asks the proxy to kill the loaded backend via POST /_admin/unload.
// force=true skips draining in-flight requests. Idempotent.
func (s *Supervisor) Unload(ctx context.Context, force bool) (httpproxy.Status, error) {
	url := s.BaseURL() + "/_admin/unload"
	if force {
		url += "?force=true"
	}
	return s.adminPost(ctx, url, nil)
}

func (s *Supervisor) adminPost(ctx context.Context, url string, body io.Reader) (httpproxy.Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return httpproxy.Status{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	// No client timeout: /_admin/load legitimately blocks for the whole model
	// load. Cancellation is the caller's ctx.
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return httpproxy.Status{}, fmt.Errorf("proxy admin request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error.Message == "" {
			e.Error.Message = resp.Status
		}
		return httpproxy.Status{}, fmt.Errorf("proxy: %s", e.Error.Message)
	}
	var st httpproxy.Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return httpproxy.Status{}, fmt.Errorf("decode proxy status: %w", err)
	}
	return st, nil
}
```

(Add `io` to imports; adjust if the test file needs `strings` import.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/service/proxysupervisor/... && make build`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/proxysupervisor/
git commit -m "feat(proxysupervisor): add admin client (EnsureRunning/Load/Unload/BaseURL)"
```

---

### Task 6: TUI — launch/stop through the proxy, drop foreground mode

This is the largest task. The profiles page stops calling `processmgr.Launch`/`Kill` and drives the proxy instead; the server page restart does the same; the TUI auto-starts the proxy at boot.

**Files:**
- Modify: `internal/ui/pages/profiles.go` (deps + remove `bgMode` field/init, line 60/86/306)
- Modify: `internal/ui/pages/profiles_update.go:193-205` (remove the `b` toggle handling)
- Modify: `internal/ui/pages/profiles_launch.go` (rewrite launch/kill flow)
- Modify: `internal/ui/pages/server_restart.go` (`restartCmd` via proxy)
- Modify: `internal/ui/pages/server.go` (hold the proxy controller; instance refresh re-reconciles)
- Modify: `cmd/model-loader/main.go` (auto-start proxy; inject controller into pages)
- Modify: `internal/ui/pages/messages.go` only if `launchedMsg`/`healthyMsg` live there (grep first; they may be in profiles.go)
- Test: `internal/ui/pages/profiles_test.go`, `internal/ui/pages/server_test.go`

- [ ] **Step 1: Define the page-side proxy interface**

In `profiles_launch.go` (top of file):

```go
// ProxyController is the subset of *proxysupervisor.Supervisor the pages use
// to drive the backend lifecycle. All profile communication flows through the
// proxy; pages never talk to instance ports directly.
type ProxyController interface {
	EnsureRunning(context.Context) error
	Load(ctx context.Context, profileID string) (httpproxy.Status, error)
	Unload(ctx context.Context, force bool) (httpproxy.Status, error)
	Status() httpproxy.Status
	BaseURL() string
}
```

`ProfilesPage` gains `proxy ProxyController` and a fluent setter:

```go
// WithProxyController wires the supervised HTTP proxy. Launch/stop actions
// are disabled when absent.
func (p ProfilesPage) WithProxyController(pc ProxyController) ProfilesPage {
	p.proxy = pc
	return p
}
```

- [ ] **Step 2: Write the failing page test**

In `profiles_test.go`, add a fake next to the existing `fakeManager`:

```go
type fakeProxy struct {
	loaded   []string
	unloaded int
	status   httpproxy.Status
	loadErr  error
}

func (f *fakeProxy) EnsureRunning(context.Context) error { return nil }
func (f *fakeProxy) Load(_ context.Context, id string) (httpproxy.Status, error) {
	if f.loadErr != nil {
		return httpproxy.Status{}, f.loadErr
	}
	f.loaded = append(f.loaded, id)
	f.status = httpproxy.Status{Running: true, LoadedProfileID: id, LoadedPID: 42}
	return f.status, nil
}
func (f *fakeProxy) Unload(context.Context, bool) (httpproxy.Status, error) {
	f.unloaded++
	f.status = httpproxy.Status{Running: true}
	return f.status, nil
}
func (f *fakeProxy) Status() httpproxy.Status { return f.status }
func (f *fakeProxy) BaseURL() string          { return "http://127.0.0.1:4321" }

func TestLaunch_GoesThroughProxy(t *testing.T) {
	// build a ProfilesPage exactly like existing launch tests do, plus:
	fp := &fakeProxy{}
	page = page.WithProxyController(fp)
	// drive the same key sequence existing launch tests use (enter / l on a
	// profile), pump the returned cmd, then:
	if len(fp.loaded) != 1 {
		t.Fatalf("expected exactly one proxy load, got %v", fp.loaded)
	}
	// and assert the fakeManager's Launch was NOT called.
}
```

(Adapt to the established teatest or direct-Update style used by the surrounding tests — copy the closest existing launch test and swap assertions.)

Run: `go test ./internal/ui/pages/ -run TestLaunch_GoesThroughProxy -v` → Expected: FAIL.

- [ ] **Step 3: Rewrite `launchProfileCmd`**

Keep validation/resolution exactly as-is (steps through `res.Resolve` + `val.Validate` — these are pre-flight checks, not communication), then replace the `mgr.Launch` tail:

```go
func (p ProfilesPage) launchProfileCmd(selected domain.Profile) tea.Cmd {
	val := p.validator
	res := p.resolver
	proxy := p.proxy
	lg := p.logger
	attemptID := log.NewAttemptID()
	return func() tea.Msg {
		evt := lg.With("attempt_id", attemptID, "profile_id", selected.ID)
		evt.Info("launch_pipeline_start", "mode", "proxy")
		if proxy == nil {
			return launchErrMsg{err: fmt.Errorf("http proxy not configured")}
		}
		if res == nil {
			return launchErrMsg{err: fmt.Errorf("no backend resolver configured")}
		}
		rb, err := res.Resolve(selected)
		if err != nil {
			evt.Error("launch_pipeline_failed", "step", "resolve", "err", err)
			return launchErrMsg{err: fmt.Errorf("resolve backend: %w", err)}
		}
		if val != nil {
			rep := val.Validate(selected, rb.Schema.ToFlagSchema(), rb.Schema.BackendKind)
			if rep.HasBlockingErrors() {
				first := rep.Errors[0]
				return launchErrMsg{
					err:        fmt.Errorf("validation failed: %d error(s)", len(rep.Errors)),
					firstIssue: first.Field + ": " + first.Message,
				}
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := proxy.EnsureRunning(ctx); err != nil {
			evt.Error("launch_pipeline_failed", "step", "proxy_start", "err", err)
			return launchErrMsg{err: fmt.Errorf("start proxy: %w", err)}
		}
		st, err := proxy.Load(ctx, selected.ID)
		if err != nil {
			evt.Error("launch_pipeline_failed", "step", "proxy_load", "err", err)
			return launchErrMsg{err: err}
		}
		return proxyLoadedMsg{status: st}
	}
}
```

New message + handler (replaces `launchedMsg`/`healthyMsg` flow — `Load` returns only after the proxy's health check, so there is no separate wait phase):

```go
// proxyLoadedMsg reports a successful /_admin/load (backend already healthy).
type proxyLoadedMsg struct{ status httpproxy.Status }

func (p ProfilesPage) handleProxyLoaded(msg proxyLoadedMsg) (tea.Model, tea.Cmd) {
	p.launch.inFlight = false
	p.launch.status = ""
	p, fc := p.withFlash(fmt.Sprintf("loaded %s (pid=%d) — serving at %s",
		msg.status.LoadedProfileID, msg.status.LoadedPID, p.proxy.BaseURL()))
	return p, tea.Batch(fc, func() tea.Msg { return SwitchToServerMsg{PID: msg.status.LoadedPID} })
}
```

Mechanical follow-through (compiler-guided):
- Replace the `launch.waitPID` spinner gating with a `launch.inFlight bool` (set true in `launchSelected` before returning the cmd; spinner ticks while `inFlight`). Update `handleSpinnerTick` and `handleLaunchErr` accordingly (`p.launch.inFlight = false`).
- Delete `handleLaunched`, `handleHealthy`, `launchedMsg`, `healthyMsg`, and the `WaitHealthy` call (grep `waitPID` in `pages/`).
- `launchSelected`: gate on `p.launch.inFlight` instead of `waitPID`; set status `"loading <name> via proxy…"`.
- Kill flow: `askKillMostRecent` becomes "unload current": read `p.proxy.Status()`; if `LoadedProfileID == ""` flash "nothing loaded"; confirm then run a cmd calling `p.proxy.Unload(ctx, false)` and flash the result. Delete `performKill` and the `p.running` bookkeeping (the page no longer tracks instances; grep `p.running` and remove all uses in profiles files).
- `profiles.go`: delete `bgMode` field (line 60), its `bgMode: true` init (line 86), the indicator at line 306; `profiles_update.go:193-205`: delete the `b` toggle. Remove the help entry for `b` (grep `"b"` in `internal/ui/components/help.go` / page help text).
- Update `friendlyLaunchError`: drop the `ErrForegroundBusy` case if now unreachable (keep `ErrModelNotFound`, `ErrHealthCheckTimeout` — proxy errors arrive as strings, so the switch shrinks naturally).
- `handleLaunchProfile` (cross-tab `LaunchProfileMsg`): guard on `p.proxy == nil` instead of `p.manager == nil`.

- [ ] **Step 4: Server page restart via proxy + instance refresh**

`server.go`: ServerPage already receives the supervisor via `WithProxy` for the proxy panel — widen what it stores: keep passing `components.HTTPProxyController` to the panel, and add a `proxyCtl ProxyController` field set from the same `WithProxy` argument (change `WithProxy`'s parameter type to `ProxyController`, which satisfies `components.HTTPProxyController` since it embeds Start/Stop? — **No**: `ProxyController` does not include Start/Stop. Instead change `WithProxy` to accept an interface that is the union):

```go
// in server.go
type serverProxyController interface {
	components.HTTPProxyController // Start, Stop, Status
	Load(ctx context.Context, profileID string) (httpproxy.Status, error)
	Unload(ctx context.Context, force bool) (httpproxy.Status, error)
	BaseURL() string
	EnsureRunning(context.Context) error
}

func (p *ServerPage) WithProxy(srv serverProxyController) *ServerPage {
	p.proxyCtl = srv
	p.proxyPanel = components.NewProxyPanel(srv)
	return p
}
```

(Check the current `WithProxy` body and keep its existing panel wiring; `*proxysupervisor.Supervisor` satisfies the union. Update `server_test.go` fakes to add the three new methods.)

`server_restart.go` `restartCmd`:

```go
// restartCmd reloads the instance's profile through the proxy: unload (drain)
// then load. Runs off the UI thread.
func restartCmd(proxy serverProxyController, pid int, prof domain.Profile, _ bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if proxy == nil {
			return restartResultMsg{pid: pid, err: fmt.Errorf("http proxy not configured")}
		}
		if _, err := proxy.Unload(ctx, false); err != nil {
			return restartResultMsg{pid: pid, err: fmt.Errorf("unload: %w", err)}
		}
		if _, err := proxy.Load(ctx, prof.ID); err != nil {
			return restartResultMsg{pid: pid, err: fmt.Errorf("load: %w", err)}
		}
		return restartResultMsg{pid: pid}
	}
}
```

Update its caller `handleRestartConfirmed` to pass `p.proxyCtl` (the resolver pre-resolution block there can be deleted — the proxy process resolves on its own, see `prepareLaunch`'s resolver fallback). Kill on the monitor (`handleKillConfirmed`): if `m.pid == p.proxyCtl.Status().LoadedPID`, call `Unload(ctx, true)` instead of `p.pm.Kill` (orphan/non-proxy pids keep direct `pm.Kill` — that is process management, not profile communication).

Instance table freshness: instances now spawn inside the **proxy process**, so this TUI process's in-memory `pm.List()` goes stale. In `refreshInstancesCmd` (find it in `server.go`/`server_subviews.go`), call `p.pm.Reconcile()` before `p.pm.List()` (the `Manager` interface already exposes `Reconcile`; it re-reads `instances.json` and drops dead PIDs — safe here because this process launches nothing).

- [ ] **Step 5: Boot wiring in `cmd/model-loader/main.go`**

After `supervisor.Reconcile()` (line ~81):

```go
if !supervisor.Status().Running {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	if err := supervisor.Start(ctx); err != nil {
		svc.Logger.Error("proxy_autostart_failed", "err", err)
	}
	cancel()
}
```

(add `"context"` import) and wire the profiles page:

```go
profilesPage = profilesPage.
	WithProcessManager(svc.Mgr, svc.Val).
	WithBackendResolver(svc.Resolver).
	WithProxyController(supervisor).
	WithLogger(svc.Logger)
```

- [ ] **Step 6: Update page tests**

Compiler + test run will surface every assertion tied to the old flow. Expected reworks in `profiles_test.go` / `server_test.go`:
- launch tests: assert `fakeProxy.loaded` instead of `fakeManager.Launch` calls; health-wait assertions die with `healthyMsg`.
- kill tests: assert `fakeProxy.unloaded`.
- `b`-toggle tests: delete.
- restart tests (`restartTrackingMgr`): switch to a fake proxy tracking Unload+Load order.
- server fakes gain `Load/Unload/BaseURL/EnsureRunning`; `fakeProcMgr` gains `Reconcile() error` if not present.

Run: `go test ./internal/ui/... && make build`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/ cmd/model-loader/main.go
git commit -m "feat(tui): drive profile lifecycle exclusively through the http proxy"
```

---### Task 7: benchmark — run through the proxy

**Files:**
- Modify: `internal/service/benchmark/runner.go`
- Modify: `cmd/model-loader/main.go` (NewRunner call, line ~107)
- Modify: `internal/cli/benchmark.go` (NewRunner call, line ~89 — construct a `proxysupervisor` from `svc.Cfg` exactly like main.go does, `Reconcile()` it, and pass it)
- Test: `internal/service/benchmark/runner_metrics_test.go` or a new `runner_proxy_test.go`

- [ ] **Step 1: Define the proxy dependency**

In `runner.go`:

```go
// ProxyController is how the runner reaches backends: ensure the proxy is up,
// swap the profile in, and address all inference at the proxy. The instance
// port never leaks into this package.
type ProxyController interface {
	EnsureRunning(context.Context) error
	Load(ctx context.Context, profileID string) (httpproxy.Status, error)
	Status() httpproxy.Status
	BaseURL() string
}
```

`Runner` struct: add `proxy ProxyController`. `NewRunner` signature becomes:

```go
func NewRunner(store profilestore.Store, pm processmgr.Manager, mon monitor.Manager, resolver backendcatalog.Resolver, proxy ProxyController, cfg Config) (*Runner, error)
```

(keep `pm`/`resolver` — `ModeLlamaBench` still uses the resolver to run the standalone `llama-bench` binary; delete `pm` only if the compiler proves it unused after this task).

- [ ] **Step 2: Write the failing test**

```go
type fakeProxyCtl struct {
	base   string
	loaded []string
	st     httpproxy.Status
}

func (f *fakeProxyCtl) EnsureRunning(context.Context) error { return nil }
func (f *fakeProxyCtl) Load(_ context.Context, id string) (httpproxy.Status, error) {
	f.loaded = append(f.loaded, id)
	f.st = httpproxy.Status{Running: true, LoadedProfileID: id, LoadedPID: 7, LoadedLogPath: "/tmp/b.log"}
	return f.st, nil
}
func (f *fakeProxyCtl) Status() httpproxy.Status { return f.st }
func (f *fakeProxyCtl) BaseURL() string          { return f.base }

func TestEnsureLoaded_UsesProxy(t *testing.T) {
	fp := &fakeProxyCtl{base: "http://127.0.0.1:9999"}
	r := &Runner{proxy: fp}
	base, logPath, pid, reused, err := r.ensureLoaded(context.Background(), domain.Profile{ID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if base != "http://127.0.0.1:9999" || pid != 7 || logPath != "/tmp/b.log" || reused {
		t.Fatalf("got %q %q %d reused=%v", base, logPath, pid, reused)
	}
	// Second call: already loaded → reused=true.
	_, _, _, reused, err = r.ensureLoaded(context.Background(), domain.Profile{ID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if !reused {
		t.Fatal("second load of same profile must report reused")
	}
}
```

Run: `go test ./internal/service/benchmark/ -run TestEnsureLoaded_UsesProxy -v` → FAIL (method missing).

- [ ] **Step 3: Replace `ensureInstance` with `ensureLoaded`**

Delete `ensureInstance` (runner.go:326-356) and `waitHealthy` (358-374). Add:

```go
// ensureLoaded swaps the profile in through the proxy. Returns the proxy base
// URL plus the backend's pid/log path (GPU sampling + diagnostics) and whether
// the profile was already loaded (warm run — perf numbers may be skewed).
func (r *Runner) ensureLoaded(ctx context.Context, profile domain.Profile) (base, logPath string, pid int, reused bool, err error) {
	if r.proxy == nil {
		return "", "", 0, false, fmt.Errorf("http proxy not configured")
	}
	if err := r.proxy.EnsureRunning(ctx); err != nil {
		return "", "", 0, false, fmt.Errorf("start proxy: %w", err)
	}
	reused = r.proxy.Status().LoadedProfileID == profile.ID
	st, err := r.proxy.Load(ctx, profile.ID)
	if err != nil {
		return "", "", 0, false, fmt.Errorf("load profile via proxy: %w", err)
	}
	return r.proxy.BaseURL(), st.LoadedLogPath, st.LoadedPID, reused, nil
}
```

In `Run()` (lines 236-255) replace:

```go
base, logPath, pid, reused, err := r.ensureLoaded(ctx, profile)
if err != nil {
	return Run{}, err
}
run.ReusedInstance = reused
// No defer-kill: the proxy owns the backend lifecycle; the model stays loaded.

gpu := r.startGPUSampler(pid, base, logPath)
defer gpu.stop()

model := profile.ID // the proxy routes requests by profile id ("model" field)
```

- `startGPUSampler(pid, port, logPath)`: change its `port int` parameter to `base string` and replace its internal `fmt.Sprintf("http://127.0.0.1:%d", port)` with `base` (grep `startGPUSampler` for the definition; the `/slots`-style polls go through the proxy catch-all, which forwards them to the loaded backend).
- Delete `requestModelName` (runner.go:899-911) — the proxy contract is model == profile ID. Keep `argString` if other callers remain (grep).
- Resolver pre-resolution in the old `ensureInstance` is gone — the proxy process resolves backends itself.
- The launch-path `Launch.ResolvedExecutable` lines die with `ensureInstance`. `ModeLlamaBench` resolution code is elsewhere (llamabench.go) and stays.
- Document in `result.go` near `ReusedInstance` that "reused" now means "profile was already loaded in the proxy".

- [ ] **Step 4: Update constructors**

- `cmd/model-loader/main.go:107`: `benchmark.NewRunner(svc.Store, svc.Mgr, mon, svc.Resolver, supervisor, benchmark.Config{...})`.
- `internal/cli/benchmark.go:89`: build the supervisor first (mirror main.go lines 74-83: `proxysupervisor.New(...)` + `Reconcile()`), pass it to `NewRunner`, and before `runner.Run` add `supervisor.EnsureRunning(ctx)` with a clear error to stderr on failure.
- Any other `NewRunner` callers: `grep -rn "benchmark.NewRunner" --include="*.go"`.

- [ ] **Step 5: Fix benchmark tests**

Existing runner tests that fake `pm.Launch`/ports must move to `fakeProxyCtl` + an `httptest.Server` as the proxy base URL (the test server answers `/v1/chat/completions` the way current tests' fake llama-server does — point `fakeProxyCtl.base` at it). Mode handler tests (`Execute` against a base URL) are unaffected — they already take `base` as a parameter.

Run: `go test ./internal/service/benchmark/... ./internal/cli/... && make build`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/service/benchmark/ internal/cli/benchmark.go cmd/model-loader/main.go
git commit -m "feat(benchmark): route all inference through the http proxy"
```

---

### Task 8: CLI — instance lifecycle through the proxy

**Files:**
- Modify: `internal/cli/instance_lifecycle.go`
- Test: `internal/cli/instance_lifecycle_test.go`

- [ ] **Step 1: Read the file and write failing tests**

Read `internal/cli/instance_lifecycle.go` fully first (it was only grepped during planning). The three commands (`start <profile>`, `stop <pid|id>`, `restart <pid|id>`) currently call `mgr.Launch`/`mgr.Kill` (lines ~91, ~126-130).

Test (mirror the existing CLI test harness in `instance_lifecycle_test.go` — they bootstrap against temp dirs; follow the established pattern, injecting a fake/`httptest` proxy admin server and pointing config `serve.host/port` at it):

```go
func TestInstanceStart_LoadsViaProxy(t *testing.T) {
	// httptest server records POST /_admin/load and returns a Status JSON.
	// Configure the test config's serve.host/serve.port to the test server.
	// Run: model-loader instance start <profile-id>
	// Assert: the load endpoint received {"profile_id": "<profile-id>"}
	// and mgr.Launch was never invoked (no instance in instances.json).
}
```

- [ ] **Step 2: Implement**

- `start`: construct `proxysupervisor.New` from `svc.Cfg` (same Config block as main.go), `Reconcile()`, `EnsureRunning(ctx)`, then `Load(ctx, profileID)`; print the resulting Status (respect `--json` via the existing output helpers). Remove the `--foreground` flag and `launchMode` helper.
- `stop`: when the target pid equals `Status().LoadedPID` (or the arg is a profile id matching `LoadedProfileID`), call `Unload(ctx, force)`; otherwise fall back to `mgr.Kill(pid)` for orphans.
- `restart`: `Unload` + `Load` via the supervisor (replacing the `Kill`+`Launch` pair at lines ~126-130).
- Use a 5-minute ctx timeout for `Load` (model loads are slow).

- [ ] **Step 3: Run tests**

Run: `go test ./internal/cli/... && make build`
Expected: PASS (update existing lifecycle tests that asserted direct Launch/Kill).

- [ ] **Step 4: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): instance start/stop/restart go through the http proxy"
```

---

### Task 9: docs, schema, knowledge bases, golden refresh

**Files:**
- Modify: `docs/profile-schema.json` (remove `"port": 8080` from examples ~line 165 and the `port` mention in the args description ~lines 40-49; add: `"port is reserved — assigned automatically by the process manager and ignored if present"`)
- Modify: `docs/config.md` (`[serve]` section: note the proxy auto-starts with the TUI and is the only communication channel; clients send the profile id as the OpenAI `model` field; backends that validate model names server-side (vLLM/SGLang) should set `served-model-name` to the profile id)
- Modify: `internal/service/profilestore/AGENTS.md` (remove "port deconfliction on duplicate" notes; add reserved-args note)
- Modify: `internal/service/processmgr/AGENTS.md` (health-check note: port is ephemeral, manager-assigned)
- Modify: root `AGENTS.md`/`CLAUDE.md` if it mentions profile ports (grep `port` in both)

- [ ] **Step 1: Apply the doc edits above**

- [ ] **Step 2: Full verification**

```bash
make build
go test ./... -update   # refresh golden fixtures touched by form/schema changes
go test ./...           # everything green on a second clean pass
make tests
```

Expected: all PASS. Investigate any non-port-related diff in goldens before accepting it.

- [ ] **Step 3: Commit**

```bash
git add docs/ internal/ AGENTS.md
git commit -m "docs: port is manager-assigned; proxy is the only client channel"
```

---

## Known limitations (accepted, documented in spec)

- One model loaded at a time (proxy swap semantics).
- Backends that validate the request `model` field server-side (vLLM/SGLang) need `served-model-name` set to the profile id to accept proxied requests — documented in `docs/config.md` (Task 9).
- The TUI's instance monitor reads `instances.json` via `Reconcile()` on refresh; sub-second freshness is not guaranteed.

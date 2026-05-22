# CLI Parity Phase 4 — `backend` Command Tree Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a cobra `backend` command tree to the CLI — `backend list`, `backend show <id>`, `backend probe [id]`, and a `backend schema` subtree (`schema show <id>`, `schema refresh <id>`, `schema apply <id> -f <file>`) — exposing the backend catalog / schema services headlessly and scriptably.

**Architecture:** Thin cobra `RunE`s load `config.Load()` and build only the services they need (a fully-registered `*backendschema.Manager`, a `backendcatalog.SchemaStore`, or a `*backendcatalog.Prober`) directly from `cfg.Paths.BackendsDir`, mirroring how Phases 1–3 wire services locally (NOT via `app.Services`). Each `RunE` delegates to a **pure function** taking a narrow interface + `io.Writer` + `asJSON bool`, returning a raw `error` that `exitOnErr` maps to `*ExitError{Code:1}`. The 5 generator `Register` calls are extracted into a shared `backendschema.RegisterDefaults` so bootstrap and CLI never drift. `schema apply` writes a hand-edited schema with `Source.Editable=true`, the project-sanctioned way to persist manual edits (`RefreshSchema` preserves editable schemas).

**Tech Stack:** Go 1.26.2, spf13/cobra, text/tabwriter, encoding/json.

---

## Verify Before Coding (exact signatures, verbatim)

These are confirmed against the codebase. Do not re-derive — use them as written.

**`internal/config`**
```go
func Load() (AppConfig, error)
// AppConfig.Paths.BackendsDir string — default ~/.config/model-loader/backends (config.go:169, tilde-expanded in LoadFrom)
```

**`internal/domain/backend.go`**
```go
type BackendKind string
const (
	BackendKindLlamaServer  BackendKind = "llama-server"
	BackendKindVLLM         BackendKind = "vllm"
	BackendKindTabbyAPI     BackendKind = "tabbyapi"
	BackendKindSGLang       BackendKind = "sglang"
	BackendKindDFlash       BackendKind = "dflash"
	BackendKindBuunLlamaCpp BackendKind = "buun-llama-cpp"
)
type BackendMeta struct {
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	GeneratedAt   *time.Time `json:"generatedAt,omitempty"`
	SourceVersion string     `json:"sourceVersion,omitempty"`
}
type Backend struct {
	ID, Name        string
	Kind            BackendKind
	Executable      string
	SchemaRef       string // "schemas/<id>.json"
	Description     string
	Tags            []string
	Meta            BackendMeta
	Properties      map[string]string
}
type BackendCatalog struct {
	SchemaVersion    int
	DefaultBackendID string
	Backends         []Backend
}
```

**`internal/domain/backend_schema.go`**
```go
type BackendValidationSchema struct {
	SchemaVersion int
	Kind          ValidationSchemaKind
	BackendKind   domain.BackendKind
	BackendID     string
	Source        SchemaSource         // {GeneratedFrom, GeneratedAt, SourceVersion, Editable bool}
	Flags         map[string]FlagSpec
	Presentation  *Presentation        // {Groups []PresentationGroup{Name, Highlighted, Flags []string}}
	Rules         []CrossFieldRule
}
```

**`internal/service/backendschema/manager.go`**
```go
func NewManager(catalogStore backendcatalog.Store, schemaStore backendcatalog.SchemaStore) *Manager
func (m *Manager) Register(kind domain.BackendKind, g Generator)
func (m *Manager) ListBackends() ([]domain.Backend, error)
func (m *Manager) GetBackend(id string) (domain.Backend, error)
func (m *Manager) DefaultBackendID() (string, error)
func (m *Manager) RefreshSchema(backendID string) error // err "no generator registered for kind: %s" when none
// generator constructors (all take backendcatalog.SchemaStore, return *T):
//   NewLlamaServerGenerator, NewSGLangGenerator, NewVLLMGenerator, NewDFlashGenerator, NewBuunServerGenerator
```

**`internal/service/backendcatalog`**
```go
func NewFSStore(backendsDir string) *fsStore            // implements Store
func NewFSSchemaStore(backendsDir string) *fsSchemaStore // implements SchemaStore
func SchemaStoreRef(ref string) string                   // strips "schemas/" prefix
type Store interface { Load() (domain.BackendCatalog, error); Save(domain.BackendCatalog) error }
type SchemaStore interface {
	Load(ref string) (domain.BackendValidationSchema, error)
	Save(ref string, schema domain.BackendValidationSchema) error
	Delete(ref string) error
}
var ErrBackendNotFound = errors.New(...) // wrapped by manager
var ErrSchemaNotFound  = errors.New(...)
// probe.go
type ProbeStatus string // "OK" | "WARN" | "ERR"
const ( ProbeStatusOK ProbeStatus = "OK"; ProbeStatusWarn ProbeStatus = "WARN"; ProbeStatusErr ProbeStatus = "ERR" )
type ProbeConfig struct { Timeout time.Duration }
type ProbeEvent struct { BackendID string; Status ProbeStatus; Latency time.Duration; Detail string; Err error; Done bool }
func NewProber(store Store, cfg ProbeConfig) *Prober
func (p *Prober) Probe(ctx context.Context) (<-chan ProbeEvent, error)
// Probe loads catalog, emits one event per backend (Done=false, BackendID set),
// then a final {Done:true} event. Caller must drain.
```

**Existing CLI helpers (reuse, do NOT redefine)** — `internal/cli/root.go`, `internal/cli/output.go`:
```go
type ExitError struct{ Code int }
var jsonOut bool                                   // global --json
func emitJSON(w io.Writer, v any) error
func printTable(w io.Writer, headers []string, rows [][]string)
func dashOr(s string) string
func clip(s string, max int) string
func exitOnErr(errw io.Writer, err error) error    // defined in model.go; prints err, returns *ExitError{1}
func childByName(parent *cobra.Command, name string) *cobra.Command // test helper in model_test.go
```

---

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/service/backendschema/register.go` (create) | `RegisterDefaults(m, schemaStore)` — single source of generator registration |
| `internal/app/bootstrap.go` (modify ~102-107) | call `RegisterDefaults` instead of inline `Register` calls |
| `internal/cli/backend.go` (create) | `backendCmd` parent, builders (`buildSchemaManager`/`buildSchemaStore`/`buildProber`), interfaces (`backendManager`/`backendProber`), `resolveBackend` helper, `init()` |
| `internal/cli/backend_list.go` (create) | `backend list` + `listBackends` pure fn + `backendListItem` |
| `internal/cli/backend_show.go` (create) | `backend show <id>` + `showBackend` pure fn + `backendDetail` |
| `internal/cli/backend_probe.go` (create) | `backend probe [id]` + `probeBackends` pure fn + `probeItem` |
| `internal/cli/backend_schema.go` (create) | `backend schema` parent + `schema show` + `schema refresh` + `schema apply` + pure fns + view structs |
| `internal/cli/backend_test.go` (create) | fakes (`fakeBackendManager`, `fakeProber`, `fakeSchemaStore`), registration + resolve tests, full-tree test |
| `internal/cli/backend_list_test.go` / `_show_test.go` / `_probe_test.go` / `_schema_test.go` (create) | per-command unit tests |
| `internal/service/backendschema/register_test.go` (create) | asserts all 5 kinds registered |

---

## Task 1: Extract `backendschema.RegisterDefaults` and rewire bootstrap

**Files:**
- Create: `internal/service/backendschema/register.go`
- Create: `internal/service/backendschema/register_test.go`
- Modify: `internal/app/bootstrap.go:102-107`

> **Before editing bootstrap.go**, run `gitnexus_impact({target: "Bootstrap", direction: "upstream"})` (or the closest matching symbol for the bootstrap function) and report the blast radius. This change is additive (extracts 6 lines into a helper + one call site) so risk should be LOW, but verify before proceeding.

- [ ] **Step 1: Write the failing test**

`internal/service/backendschema/register_test.go`:
```go
package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestRegisterDefaults_RegistersAllGeneratorKinds(t *testing.T) {
	schemaStore := backendcatalog.NewFSSchemaStore(t.TempDir())
	m := NewManager(backendcatalog.NewFSStore(t.TempDir()), schemaStore)
	RegisterDefaults(m, schemaStore)

	want := []domain.BackendKind{
		domain.BackendKindLlamaServer,
		domain.BackendKindSGLang,
		domain.BackendKindVLLM,
		domain.BackendKindDFlash,
		domain.BackendKindBuunLlamaCpp,
	}
	gens := m.Generators()
	if len(gens) != len(want) {
		t.Fatalf("registered %d generators, want %d: %v", len(gens), len(want), gens)
	}
	for _, k := range want {
		if gens[k] == nil {
			t.Errorf("no generator registered for kind %q", k)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/backendschema/ -run TestRegisterDefaults -v`
Expected: FAIL — `undefined: RegisterDefaults`.

- [ ] **Step 3: Write the implementation**

`internal/service/backendschema/register.go`:
```go
package backendschema

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

// RegisterDefaults registers the built-in schema generators for every backend
// kind that ships with one. Both the app bootstrap and the CLI call this so the
// set of generators lives in exactly one place and cannot drift between them.
func RegisterDefaults(m *Manager, schemaStore backendcatalog.SchemaStore) {
	m.Register(domain.BackendKindLlamaServer, NewLlamaServerGenerator(schemaStore))
	m.Register(domain.BackendKindSGLang, NewSGLangGenerator(schemaStore))
	m.Register(domain.BackendKindVLLM, NewVLLMGenerator(schemaStore))
	m.Register(domain.BackendKindDFlash, NewDFlashGenerator(schemaStore))
	m.Register(domain.BackendKindBuunLlamaCpp, NewBuunServerGenerator(schemaStore))
}
```

- [ ] **Step 4: Rewire bootstrap.go**

Replace `internal/app/bootstrap.go:102-107` (the `NewManager` + 5 inline `Register` lines) with:
```go
	schemaManager := backendschema.NewManager(catalogStore, schemaStore)
	backendschema.RegisterDefaults(schemaManager, schemaStore)
```

- [ ] **Step 5: Run tests + build**

Run: `go test ./internal/service/backendschema/ ./internal/app/ && go build ./...`
Expected: PASS, clean build.

- [ ] **Step 6: Commit**

```bash
git add internal/service/backendschema/register.go internal/service/backendschema/register_test.go internal/app/bootstrap.go
git commit -m "refactor(backend): extract RegisterDefaults shared by bootstrap and CLI"
```

---

## Task 2: `backend` parent command, builders, interfaces, resolver

**Files:**
- Create: `internal/cli/backend.go`
- Create: `internal/cli/backend_test.go`

- [ ] **Step 1: Write the failing test**

`internal/cli/backend_test.go`:
```go
package cli

import (
	"context"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

// --- shared fakes for the backend command tests ---

type fakeBackendManager struct {
	backends   []domain.Backend
	defaultID  string
	listErr    error
	refreshErr error
	refreshed  []string
}

func (f *fakeBackendManager) ListBackends() ([]domain.Backend, error) {
	return f.backends, f.listErr
}
func (f *fakeBackendManager) DefaultBackendID() (string, error) {
	return f.defaultID, nil
}
func (f *fakeBackendManager) RefreshSchema(id string) error {
	if f.refreshErr != nil {
		return f.refreshErr
	}
	f.refreshed = append(f.refreshed, id)
	return nil
}

type fakeProber struct {
	events []backendcatalog.ProbeEvent
	err    error
}

func (f *fakeProber) Probe(ctx context.Context) (<-chan backendcatalog.ProbeEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan backendcatalog.ProbeEvent, len(f.events))
	for _, e := range f.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

type fakeSchemaStore struct {
	schemas map[string]domain.BackendValidationSchema
	saved   map[string]domain.BackendValidationSchema
	loadErr error
	saveErr error
}

func newFakeSchemaStore() *fakeSchemaStore {
	return &fakeSchemaStore{
		schemas: map[string]domain.BackendValidationSchema{},
		saved:   map[string]domain.BackendValidationSchema{},
	}
}
func (f *fakeSchemaStore) Load(ref string) (domain.BackendValidationSchema, error) {
	if f.loadErr != nil {
		return domain.BackendValidationSchema{}, f.loadErr
	}
	s, ok := f.schemas[ref]
	if !ok {
		return domain.BackendValidationSchema{}, backendcatalog.ErrSchemaNotFound
	}
	return s, nil
}
func (f *fakeSchemaStore) Save(ref string, s domain.BackendValidationSchema) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved[ref] = s
	return nil
}
func (f *fakeSchemaStore) Delete(ref string) error { delete(f.schemas, ref); return nil }

func twoBackends() *fakeBackendManager {
	return &fakeBackendManager{
		defaultID: "llama-server",
		backends: []domain.Backend{
			{ID: "llama-server", Name: "Llama Server", Kind: domain.BackendKindLlamaServer, Executable: "llama-server", SchemaRef: "schemas/llama-server.json"},
			{ID: "vllm-main", Name: "vLLM", Kind: domain.BackendKindVLLM, Executable: "vllm", SchemaRef: "schemas/vllm-main.json"},
		},
	}
}

// --- tests ---

func TestBackendCmd_RegisteredUnderRoot(t *testing.T) {
	if childByName(rootCmd, "backend") == nil {
		t.Fatal("backend command not registered under root")
	}
}

func TestResolveBackend_ExactAndPrefix(t *testing.T) {
	mgr := twoBackends()

	b, err := resolveBackend(mgr, "vllm-main")
	if err != nil || b.ID != "vllm-main" {
		t.Fatalf("exact: got %q err=%v", b.ID, err)
	}
	b, err = resolveBackend(mgr, "vllm")
	if err != nil || b.ID != "vllm-main" {
		t.Fatalf("prefix: got %q err=%v", b.ID, err)
	}
	if _, err := resolveBackend(mgr, "nope"); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestResolveBackend_AmbiguousPrefix(t *testing.T) {
	mgr := &fakeBackendManager{backends: []domain.Backend{
		{ID: "vllm-a", Kind: domain.BackendKindVLLM},
		{ID: "vllm-b", Kind: domain.BackendKindVLLM},
	}}
	if _, err := resolveBackend(mgr, "vllm"); err == nil {
		t.Fatal("expected ambiguous error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestBackendCmd_RegisteredUnderRoot|TestResolveBackend' -v`
Expected: FAIL — `undefined: resolveBackend`, `backend` not registered.

- [ ] **Step 3: Write the implementation**

`internal/cli/backend.go`:
```go
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/spf13/cobra"
)

var backendCmd = &cobra.Command{
	Use:   "backend",
	Short: "List backends, inspect them, probe binaries, and manage schemas",
}

func init() {
	rootCmd.AddCommand(backendCmd)
}

// backendManager is the narrow slice of *backendschema.Manager the CLI needs.
type backendManager interface {
	ListBackends() ([]domain.Backend, error)
	DefaultBackendID() (string, error)
	RefreshSchema(backendID string) error
}

// backendProber is the narrow slice of *backendcatalog.Prober the CLI needs.
type backendProber interface {
	Probe(ctx context.Context) (<-chan backendcatalog.ProbeEvent, error)
}

// buildSchemaManager constructs a fully-registered schema Manager rooted at the
// configured backends dir, sharing generator registration with the app via
// backendschema.RegisterDefaults.
func buildSchemaManager(cfg config.AppConfig) *backendschema.Manager {
	catalogStore := backendcatalog.NewFSStore(cfg.Paths.BackendsDir)
	schemaStore := backendcatalog.NewFSSchemaStore(cfg.Paths.BackendsDir)
	m := backendschema.NewManager(catalogStore, schemaStore)
	backendschema.RegisterDefaults(m, schemaStore)
	return m
}

// buildSchemaStore returns the filesystem schema store for the configured dir.
func buildSchemaStore(cfg config.AppConfig) backendcatalog.SchemaStore {
	return backendcatalog.NewFSSchemaStore(cfg.Paths.BackendsDir)
}

// buildProber returns a Prober over the configured catalog with a 5s timeout.
func buildProber(cfg config.AppConfig) backendProber {
	store := backendcatalog.NewFSStore(cfg.Paths.BackendsDir)
	return backendcatalog.NewProber(store, backendcatalog.ProbeConfig{Timeout: 5 * time.Second})
}

// resolveBackend matches idOrPrefix against catalog backend IDs: an exact match
// wins; otherwise a unique prefix match is accepted. Mirrors resolveInstance.
func resolveBackend(mgr backendManager, idOrPrefix string) (domain.Backend, error) {
	backends, err := mgr.ListBackends()
	if err != nil {
		return domain.Backend{}, fmt.Errorf("list backends: %w", err)
	}
	for _, b := range backends {
		if b.ID == idOrPrefix {
			return b, nil
		}
	}
	var match domain.Backend
	n := 0
	for _, b := range backends {
		if strings.HasPrefix(b.ID, idOrPrefix) {
			match = b
			n++
		}
	}
	switch n {
	case 0:
		return domain.Backend{}, fmt.Errorf("backend not found: %s", idOrPrefix)
	case 1:
		return match, nil
	default:
		return domain.Backend{}, fmt.Errorf("ambiguous backend id: %s", idOrPrefix)
	}
}
```

- [ ] **Step 4: Run tests + build**

Run: `go build ./... && go test ./internal/cli/ -run 'TestBackendCmd_RegisteredUnderRoot|TestResolveBackend' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/backend.go internal/cli/backend_test.go
git commit -m "feat(cli): scaffold backend command parent, builders, resolveBackend"
```

---

## Task 3: `backend list`

**Files:**
- Create: `internal/cli/backend_list.go`
- Create: `internal/cli/backend_list_test.go`

- [ ] **Step 1: Write the failing test**

`internal/cli/backend_list_test.go`:
```go
package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestListBackends_Table(t *testing.T) {
	var out bytes.Buffer
	if err := listBackends(&out, twoBackends(), false); err != nil {
		t.Fatalf("listBackends: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "llama-server") || !strings.Contains(s, "vllm-main") {
		t.Fatalf("table missing backends: %q", s)
	}
	// default backend marked
	if !strings.Contains(s, "yes") {
		t.Fatalf("default backend not marked: %q", s)
	}
}

func TestListBackends_JSON(t *testing.T) {
	var out bytes.Buffer
	if err := listBackends(&out, twoBackends(), true); err != nil {
		t.Fatalf("listBackends: %v", err)
	}
	var items []backendListItem
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	var def *backendListItem
	for i := range items {
		if items[i].ID == "llama-server" {
			def = &items[i]
		}
	}
	if def == nil || !def.Default {
		t.Fatalf("llama-server should be default: %+v", items)
	}
}

func TestListBackends_EmptyJSONIsArray(t *testing.T) {
	var out bytes.Buffer
	mgr := &fakeBackendManager{}
	if err := listBackends(&out, mgr, true); err != nil {
		t.Fatalf("listBackends: %v", err)
	}
	if strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("empty list should be []: %q", out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestListBackends -v`
Expected: FAIL — `undefined: listBackends`, `undefined: backendListItem`.

- [ ] **Step 3: Write the implementation**

`internal/cli/backend_list.go`:
```go
package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"
)

type backendListItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Executable string `json:"executable"`
	Default    bool   `json:"default"`
	SchemaRef  string `json:"schemaRef,omitempty"`
}

func init() {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured backends from the catalog",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			return exitOnErr(cmd.ErrOrStderr(),
				listBackends(cmd.OutOrStdout(), buildSchemaManager(cfg), jsonOut))
		},
	}
	backendCmd.AddCommand(cmd)
}

// listBackends prints all catalog backends, flagging the default one.
func listBackends(out io.Writer, mgr backendManager, asJSON bool) error {
	backends, err := mgr.ListBackends()
	if err != nil {
		return fmt.Errorf("list backends: %w", err)
	}
	defaultID, err := mgr.DefaultBackendID()
	if err != nil {
		return fmt.Errorf("default backend: %w", err)
	}

	items := make([]backendListItem, 0, len(backends))
	for _, b := range backends {
		items = append(items, backendListItem{
			ID:         b.ID,
			Name:       b.Name,
			Kind:       string(b.Kind),
			Executable: b.Executable,
			Default:    b.ID == defaultID,
			SchemaRef:  b.SchemaRef,
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })

	if asJSON {
		return emitJSON(out, items)
	}
	if len(items) == 0 {
		fmt.Fprintln(out, "no backends configured")
		return nil
	}
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		def := ""
		if it.Default {
			def = "yes"
		}
		rows = append(rows, []string{it.ID, dashOr(it.Name), it.Kind, dashOr(it.Executable), def})
	}
	printTable(out, []string{"ID", "NAME", "KIND", "EXECUTABLE", "DEFAULT"}, rows)
	return nil
}
```

> Note: `config` is already imported by `backend.go` in the same package; do not re-import. If `go build` reports `config` unused/needed, it is already available package-wide via `backend.go`.

- [ ] **Step 4: Run tests + build**

Run: `go build ./... && go test ./internal/cli/ -run TestListBackends -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/backend_list.go internal/cli/backend_list_test.go
git commit -m "feat(cli): add 'backend list' to enumerate catalog backends"
```

---

## Task 4: `backend show <id>`

**Files:**
- Create: `internal/cli/backend_show.go`
- Create: `internal/cli/backend_show_test.go`

- [ ] **Step 1: Write the failing test**

`internal/cli/backend_show_test.go`:
```go
package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestShowBackend_Text(t *testing.T) {
	var out bytes.Buffer
	if err := showBackend(&out, twoBackends(), "vllm", false); err != nil {
		t.Fatalf("showBackend: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "vllm-main") || !strings.Contains(s, "vllm") {
		t.Fatalf("missing backend detail: %q", s)
	}
}

func TestShowBackend_JSON(t *testing.T) {
	var out bytes.Buffer
	if err := showBackend(&out, twoBackends(), "llama-server", true); err != nil {
		t.Fatalf("showBackend: %v", err)
	}
	var d backendDetail
	if err := json.Unmarshal(out.Bytes(), &d); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if d.ID != "llama-server" || !d.Default || d.Kind != "llama-server" {
		t.Fatalf("unexpected detail: %+v", d)
	}
}

func TestShowBackend_NotFound(t *testing.T) {
	var out bytes.Buffer
	if err := showBackend(&out, twoBackends(), "nope", false); err == nil {
		t.Fatal("expected not-found error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestShowBackend -v`
Expected: FAIL — `undefined: showBackend`, `undefined: backendDetail`.

- [ ] **Step 3: Write the implementation**

`internal/cli/backend_show.go`:
```go
package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type backendDetail struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Kind          string     `json:"kind"`
	Executable    string     `json:"executable"`
	SchemaRef     string     `json:"schemaRef,omitempty"`
	Description   string     `json:"description,omitempty"`
	Tags          []string   `json:"tags,omitempty"`
	Default       bool       `json:"default"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	GeneratedAt   *time.Time `json:"generatedAt,omitempty"`
	SourceVersion string     `json:"sourceVersion,omitempty"`
}

func init() {
	cmd := &cobra.Command{
		Use:   "show <backend-id>",
		Short: "Show full metadata for a single backend",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			return exitOnErr(cmd.ErrOrStderr(),
				showBackend(cmd.OutOrStdout(), buildSchemaManager(cfg), args[0], jsonOut))
		},
	}
	backendCmd.AddCommand(cmd)
}

// showBackend resolves idOrPrefix and prints the backend's full metadata.
func showBackend(out io.Writer, mgr backendManager, idOrPrefix string, asJSON bool) error {
	b, err := resolveBackend(mgr, idOrPrefix)
	if err != nil {
		return err
	}
	defaultID, err := mgr.DefaultBackendID()
	if err != nil {
		return fmt.Errorf("default backend: %w", err)
	}
	d := backendDetail{
		ID:            b.ID,
		Name:          b.Name,
		Kind:          string(b.Kind),
		Executable:    b.Executable,
		SchemaRef:     b.SchemaRef,
		Description:   b.Description,
		Tags:          b.Tags,
		Default:       b.ID == defaultID,
		CreatedAt:     b.Meta.CreatedAt,
		UpdatedAt:     b.Meta.UpdatedAt,
		GeneratedAt:   b.Meta.GeneratedAt,
		SourceVersion: b.Meta.SourceVersion,
	}
	if asJSON {
		return emitJSON(out, d)
	}
	fmt.Fprintf(out, "ID:          %s\n", d.ID)
	fmt.Fprintf(out, "Name:        %s\n", dashOr(d.Name))
	fmt.Fprintf(out, "Kind:        %s\n", d.Kind)
	fmt.Fprintf(out, "Executable:  %s\n", dashOr(d.Executable))
	fmt.Fprintf(out, "Schema ref:  %s\n", dashOr(d.SchemaRef))
	fmt.Fprintf(out, "Default:     %t\n", d.Default)
	if d.Description != "" {
		fmt.Fprintf(out, "Description: %s\n", d.Description)
	}
	if len(d.Tags) > 0 {
		fmt.Fprintf(out, "Tags:        %s\n", strings.Join(d.Tags, ", "))
	}
	fmt.Fprintf(out, "Created:     %s\n", d.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(out, "Updated:     %s\n", d.UpdatedAt.Format(time.RFC3339))
	if d.GeneratedAt != nil {
		fmt.Fprintf(out, "Generated:   %s\n", d.GeneratedAt.Format(time.RFC3339))
	}
	if d.SourceVersion != "" {
		fmt.Fprintf(out, "Source ver:  %s\n", d.SourceVersion)
	}
	return nil
}
```

- [ ] **Step 4: Run tests + build**

Run: `go build ./... && go test ./internal/cli/ -run TestShowBackend -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/backend_show.go internal/cli/backend_show_test.go
git commit -m "feat(cli): add 'backend show' for full backend metadata"
```

---

## Task 5: `backend probe [id]`

**Files:**
- Create: `internal/cli/backend_probe.go`
- Create: `internal/cli/backend_probe_test.go`

- [ ] **Step 1: Write the failing test**

`internal/cli/backend_probe_test.go`:
```go
package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func probeEvents() *fakeProber {
	return &fakeProber{events: []backendcatalog.ProbeEvent{
		{BackendID: "llama-server", Status: backendcatalog.ProbeStatusOK, Latency: 12 * time.Millisecond, Detail: "v7376"},
		{BackendID: "vllm-main", Status: backendcatalog.ProbeStatusErr, Latency: 0, Detail: "not found", Err: errBoom},
		{Done: true},
	}}
}

var errBoom = errBoomT{}

type errBoomT struct{}

func (errBoomT) Error() string { return "boom" }

func TestProbeBackends_TableAllSkipsDone(t *testing.T) {
	var out bytes.Buffer
	if err := probeBackends(&out, probeEvents(), "", false); err != nil {
		t.Fatalf("probeBackends: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "llama-server") || !strings.Contains(s, "vllm-main") {
		t.Fatalf("missing rows: %q", s)
	}
	if !strings.Contains(s, "OK") || !strings.Contains(s, "ERR") {
		t.Fatalf("missing statuses: %q", s)
	}
}

func TestProbeBackends_FilterByID(t *testing.T) {
	var out bytes.Buffer
	if err := probeBackends(&out, probeEvents(), "vllm-main", false); err != nil {
		t.Fatalf("probeBackends: %v", err)
	}
	s := out.String()
	if strings.Contains(s, "llama-server") {
		t.Fatalf("should not include llama-server when filtered: %q", s)
	}
	if !strings.Contains(s, "vllm-main") {
		t.Fatalf("filtered backend missing: %q", s)
	}
}

func TestProbeBackends_JSON(t *testing.T) {
	var out bytes.Buffer
	if err := probeBackends(&out, probeEvents(), "", true); err != nil {
		t.Fatalf("probeBackends: %v", err)
	}
	var items []probeItem
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if len(items) != 2 {
		t.Fatalf("want 2 probe items (Done skipped), got %d: %+v", len(items), items)
	}
	if items[1].Error == "" {
		t.Fatalf("error backend should carry error string: %+v", items[1])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestProbeBackends -v`
Expected: FAIL — `undefined: probeBackends`, `undefined: probeItem`.

- [ ] **Step 3: Write the implementation**

`internal/cli/backend_probe.go`:
```go
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

type probeItem struct {
	BackendID string `json:"backendId"`
	Status    string `json:"status"`
	LatencyMS int64  `json:"latencyMs"`
	Detail    string `json:"detail,omitempty"`
	Error     string `json:"error,omitempty"`
}

func init() {
	cmd := &cobra.Command{
		Use:   "probe [backend-id]",
		Short: "Health-check backend binaries (--version/--help); probes all when no id is given",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			filter := ""
			if len(args) == 1 {
				// Validate the id resolves before probing the whole catalog.
				b, rerr := resolveBackend(buildSchemaManager(cfg), args[0])
				if rerr != nil {
					return exitOnErr(cmd.ErrOrStderr(), rerr)
				}
				filter = b.ID
			}
			return exitOnErr(cmd.ErrOrStderr(),
				probeBackends(cmd.OutOrStdout(), buildProber(cfg), filter, jsonOut))
		},
	}
	backendCmd.AddCommand(cmd)
}

// probeBackends drains the prober channel, skips the terminal Done event, and
// (when filterID != "") keeps only the matching backend.
func probeBackends(out io.Writer, prober backendProber, filterID string, asJSON bool) error {
	ch, err := prober.Probe(context.Background())
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	items := make([]probeItem, 0)
	for ev := range ch {
		if ev.Done {
			continue
		}
		if filterID != "" && ev.BackendID != filterID {
			continue
		}
		it := probeItem{
			BackendID: ev.BackendID,
			Status:    string(ev.Status),
			LatencyMS: ev.Latency.Milliseconds(),
			Detail:    ev.Detail,
		}
		if ev.Err != nil {
			it.Error = ev.Err.Error()
		}
		items = append(items, it)
	}

	if asJSON {
		return emitJSON(out, items)
	}
	if len(items) == 0 {
		fmt.Fprintln(out, "no backends probed")
		return nil
	}
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		detail := it.Detail
		if it.Error != "" {
			detail = it.Error
		}
		rows = append(rows, []string{it.BackendID, it.Status, fmt.Sprintf("%dms", it.LatencyMS), clip(dashOr(detail), 60)})
	}
	printTable(out, []string{"BACKEND", "STATUS", "LATENCY", "DETAIL"}, rows)
	return nil
}
```

- [ ] **Step 4: Run tests + build**

Run: `go build ./... && go test ./internal/cli/ -run TestProbeBackends -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/backend_probe.go internal/cli/backend_probe_test.go
git commit -m "feat(cli): add 'backend probe' to health-check backend binaries"
```

---

## Task 6: `backend schema` parent + `schema show <id>`

**Files:**
- Create: `internal/cli/backend_schema.go`
- Create: `internal/cli/backend_schema_test.go`

- [ ] **Step 1: Write the failing test**

`internal/cli/backend_schema_test.go`:
```go
package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func schemaFixture() (*fakeBackendManager, *fakeSchemaStore) {
	mgr := twoBackends()
	store := newFakeSchemaStore()
	store.schemas["llama-server.json"] = domain.BackendValidationSchema{
		SchemaVersion: 3,
		BackendID:     "llama-server",
		BackendKind:   domain.BackendKindLlamaServer,
		Source:        domain.SchemaSource{SourceVersion: "v7376", Editable: false},
		Flags: map[string]domain.FlagSpec{
			"ctx-size": {Long: "ctx-size"},
			"port":     {Long: "port"},
		},
	}
	return mgr, store
}

func TestShowSchema_TextSummary(t *testing.T) {
	mgr, store := schemaFixture()
	var out bytes.Buffer
	if err := showSchema(&out, mgr, store, "llama-server", false); err != nil {
		t.Fatalf("showSchema: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "llama-server") || !strings.Contains(s, "2") { // 2 flags
		t.Fatalf("summary missing fields: %q", s)
	}
}

func TestShowSchema_JSONFull(t *testing.T) {
	mgr, store := schemaFixture()
	var out bytes.Buffer
	if err := showSchema(&out, mgr, store, "llama-server", true); err != nil {
		t.Fatalf("showSchema: %v", err)
	}
	var sc domain.BackendValidationSchema
	if err := json.Unmarshal(out.Bytes(), &sc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if sc.BackendID != "llama-server" || len(sc.Flags) != 2 {
		t.Fatalf("unexpected schema: %+v", sc)
	}
}

func TestShowSchema_NotFound(t *testing.T) {
	mgr, store := schemaFixture()
	var out bytes.Buffer
	// vllm-main resolves as a backend but has no schema stored.
	if err := showSchema(&out, mgr, store, "vllm-main", false); err == nil {
		t.Fatal("expected schema-not-found error")
	}
}

func TestBackendSchemaSubtree_Registered(t *testing.T) {
	bc := childByName(rootCmd, "backend")
	sc := childByName(bc, "schema")
	if sc == nil {
		t.Fatal("backend schema not registered")
	}
	if childByName(sc, "show") == nil {
		t.Fatal("backend schema show not registered")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestShowSchema|TestBackendSchemaSubtree' -v`
Expected: FAIL — `undefined: showSchema`.

- [ ] **Step 3: Write the implementation**

`internal/cli/backend_schema.go`:
```go
package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/spf13/cobra"
)

// backendSchemaCmd groups schema inspection and editing under `backend schema`.
var backendSchemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Show, refresh, and apply backend validation schemas",
}

func init() {
	backendCmd.AddCommand(backendSchemaCmd)

	showCmd := &cobra.Command{
		Use:   "show <backend-id>",
		Short: "Show a backend's validation schema (summary, or full JSON with --json)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			return exitOnErr(cmd.ErrOrStderr(),
				showSchema(cmd.OutOrStdout(), buildSchemaManager(cfg), buildSchemaStore(cfg), args[0], jsonOut))
		},
	}
	backendSchemaCmd.AddCommand(showCmd)
}

// showSchema resolves the backend, loads its schema, and prints a summary (or
// the full schema as JSON when asJSON is set).
func showSchema(out io.Writer, mgr backendManager, store backendcatalog.SchemaStore, idOrPrefix string, asJSON bool) error {
	b, err := resolveBackend(mgr, idOrPrefix)
	if err != nil {
		return err
	}
	ref := backendcatalog.SchemaStoreRef(b.SchemaRef)
	schema, err := store.Load(ref)
	if err != nil {
		return fmt.Errorf("load schema for %s: %w", b.ID, err)
	}
	if asJSON {
		return emitJSON(out, schema)
	}
	fmt.Fprintf(out, "Backend:   %s\n", b.ID)
	fmt.Fprintf(out, "Kind:      %s\n", schema.BackendKind)
	fmt.Fprintf(out, "Version:   %d\n", schema.SchemaVersion)
	fmt.Fprintf(out, "Editable:  %t\n", schema.Source.Editable)
	if schema.Source.SourceVersion != "" {
		fmt.Fprintf(out, "Source:    %s\n", schema.Source.SourceVersion)
	}
	fmt.Fprintf(out, "Flags:     %d\n", len(schema.Flags))
	if schema.Presentation != nil && len(schema.Presentation.Groups) > 0 {
		names := make([]string, 0, len(schema.Presentation.Groups))
		for _, g := range schema.Presentation.Groups {
			label := fmt.Sprintf("%s(%d)", g.Name, len(g.Flags))
			if g.Highlighted {
				label += "*"
			}
			names = append(names, label)
		}
		sort.Strings(names)
		fmt.Fprintf(out, "Groups:    %v\n", names)
	}
	fmt.Fprintf(out, "Rules:     %d\n", len(schema.Rules))
	return nil
}
```

- [ ] **Step 4: Run tests + build**

Run: `go build ./... && go test ./internal/cli/ -run 'TestShowSchema|TestBackendSchemaSubtree' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/backend_schema.go internal/cli/backend_schema_test.go
git commit -m "feat(cli): add 'backend schema show' with summary and --json dump"
```

---

## Task 7: `schema refresh <id>` + `schema apply <id> -f <file>`

**Files:**
- Modify: `internal/cli/backend_schema.go` (append two commands + two pure fns)
- Modify: `internal/cli/backend_schema_test.go` (append tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/backend_schema_test.go`:
```go
func TestRefreshSchema_DelegatesToManager(t *testing.T) {
	mgr := twoBackends()
	var out bytes.Buffer
	if err := refreshSchema(&out, mgr, "vllm"); err != nil {
		t.Fatalf("refreshSchema: %v", err)
	}
	if len(mgr.refreshed) != 1 || mgr.refreshed[0] != "vllm-main" {
		t.Fatalf("expected RefreshSchema(vllm-main), got %v", mgr.refreshed)
	}
	if !strings.Contains(out.String(), "vllm-main") {
		t.Fatalf("missing confirmation: %q", out.String())
	}
}

func TestApplySchema_WritesEditable(t *testing.T) {
	mgr, store := schemaFixture()
	dir := t.TempDir()
	file := dir + "/schema.json"
	payload := domain.BackendValidationSchema{
		SchemaVersion: 9,
		BackendID:     "llama-server",
		BackendKind:   domain.BackendKindLlamaServer,
		Flags:         map[string]domain.FlagSpec{"port": {Long: "port"}},
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := applySchema(&out, mgr, store, "llama-server", file); err != nil {
		t.Fatalf("applySchema: %v", err)
	}
	saved, ok := store.saved["llama-server.json"]
	if !ok {
		t.Fatalf("schema not saved: %+v", store.saved)
	}
	if !saved.Source.Editable {
		t.Fatal("applied schema must set Source.Editable=true")
	}
	if saved.SchemaVersion != 9 {
		t.Fatalf("payload not persisted: %+v", saved)
	}
}

func TestApplySchema_RejectsKindMismatch(t *testing.T) {
	mgr, store := schemaFixture()
	dir := t.TempDir()
	file := dir + "/schema.json"
	payload := domain.BackendValidationSchema{
		BackendID:   "llama-server",
		BackendKind: domain.BackendKindVLLM, // wrong kind for llama-server
	}
	data, _ := json.Marshal(payload)
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := applySchema(&out, mgr, store, "llama-server", file); err == nil {
		t.Fatal("expected kind-mismatch error")
	}
}
```

Add imports `"os"` to the test file's import block (and confirm `encoding/json`, `bytes`, `strings`, `testing`, the `domain` import are present from Task 6).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestRefreshSchema|TestApplySchema' -v`
Expected: FAIL — `undefined: refreshSchema`, `undefined: applySchema`.

- [ ] **Step 3: Write the implementation**

Append to the `init()` in `internal/cli/backend_schema.go` (inside the existing `init`, after `backendSchemaCmd.AddCommand(showCmd)`):
```go
	refreshCmd := &cobra.Command{
		Use:   "refresh <backend-id>",
		Short: "Re-generate a backend's schema from its --help output",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			return exitOnErr(cmd.ErrOrStderr(),
				refreshSchema(cmd.OutOrStdout(), buildSchemaManager(cfg), args[0]))
		},
	}
	backendSchemaCmd.AddCommand(refreshCmd)

	var applyFile string
	applyCmd := &cobra.Command{
		Use:   "apply <backend-id> -f <file>",
		Short: "Apply a hand-edited schema JSON, marking it editable so refresh preserves it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			return exitOnErr(cmd.ErrOrStderr(),
				applySchema(cmd.OutOrStdout(), buildSchemaManager(cfg), buildSchemaStore(cfg), args[0], applyFile))
		},
	}
	applyCmd.Flags().StringVarP(&applyFile, "file", "f", "", "path to the schema JSON file to apply (required)")
	_ = applyCmd.MarkFlagRequired("file")
	backendSchemaCmd.AddCommand(applyCmd)
```

Append these functions to `internal/cli/backend_schema.go`:
```go
// refreshSchema resolves the backend and triggers manager-driven regeneration
// from the backend's --help output. Editable schemas are deleted first by the
// manager so the regeneration is fresh.
func refreshSchema(out io.Writer, mgr backendManager, idOrPrefix string) error {
	b, err := resolveBackend(mgr, idOrPrefix)
	if err != nil {
		return err
	}
	if err := mgr.RefreshSchema(b.ID); err != nil {
		return fmt.Errorf("refresh schema: %w", err)
	}
	fmt.Fprintf(out, "refreshed schema for %s\n", b.ID)
	return nil
}

// applySchema reads a hand-edited schema from file, reconciles its identity with
// the target backend, marks it editable (so RefreshSchema preserves it), and
// persists it via the schema store.
func applySchema(out io.Writer, mgr backendManager, store backendcatalog.SchemaStore, idOrPrefix, file string) error {
	if file == "" {
		return fmt.Errorf("a schema file is required (-f/--file)")
	}
	b, err := resolveBackend(mgr, idOrPrefix)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read schema file: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var schema domain.BackendValidationSchema
	if err := dec.Decode(&schema); err != nil {
		return fmt.Errorf("parse schema: %w", err)
	}
	if schema.BackendID != "" && schema.BackendID != b.ID {
		return fmt.Errorf("schema backendId %q does not match backend %q", schema.BackendID, b.ID)
	}
	if schema.BackendKind != "" && schema.BackendKind != b.Kind {
		return fmt.Errorf("schema backendKind %q does not match backend kind %q", schema.BackendKind, b.Kind)
	}
	schema.BackendID = b.ID
	schema.BackendKind = b.Kind
	schema.Source.Editable = true

	ref := backendcatalog.SchemaStoreRef(b.SchemaRef)
	if err := store.Save(ref, schema); err != nil {
		return fmt.Errorf("save schema: %w", err)
	}
	fmt.Fprintf(out, "applied schema for %s (editable=true, %d flags)\n", b.ID, len(schema.Flags))
	return nil
}
```

Update the import block of `internal/cli/backend_schema.go` to add `"bytes"`, `"encoding/json"`, `"os"`, and `"github.com/quantmind-br/model-loader/internal/domain"` (keep existing `fmt`, `io`, `sort`, `backendcatalog`, `cobra`).

- [ ] **Step 4: Run tests + build**

Run: `go build ./... && go test ./internal/cli/ -run 'TestRefreshSchema|TestApplySchema|TestShowSchema|TestBackendSchemaSubtree' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/backend_schema.go internal/cli/backend_schema_test.go
git commit -m "feat(cli): add 'backend schema refresh' and 'schema apply' (editable)"
```

---

## Task 8: Full command-tree integration test

**Files:**
- Modify: `internal/cli/backend_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/backend_test.go`:
```go
func TestBackendCommandTree(t *testing.T) {
	bc := childByName(rootCmd, "backend")
	if bc == nil {
		t.Fatal("backend not registered")
	}
	for _, name := range []string{"list", "show", "probe", "schema"} {
		if childByName(bc, name) == nil {
			t.Errorf("backend %s not registered", name)
		}
	}
	sc := childByName(bc, "schema")
	for _, name := range []string{"show", "refresh", "apply"} {
		if childByName(sc, name) == nil {
			t.Errorf("backend schema %s not registered", name)
		}
	}
}

func TestBackendShow_ArgValidation(t *testing.T) {
	// `backend show` requires exactly one arg; zero args must be a usage error
	// surfaced as a non-zero exit, without touching config/services.
	old := rootCmd.Args
	t.Cleanup(func() { rootCmd.Args = old })

	rootCmd.SetArgs([]string{"backend", "show"})
	var errb bytes.Buffer
	rootCmd.SetErr(&errb)
	rootCmd.SetOut(&errb)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetOut(nil)
	})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected arg-validation error for `backend show` with no args")
	}
}
```

Confirm `bytes` is imported in `backend_test.go` (add to the import block if missing).

- [ ] **Step 2: Run test to verify behavior**

Run: `go test ./internal/cli/ -run 'TestBackendCommandTree|TestBackendShow_ArgValidation' -v`
Expected: PASS (all subcommands already registered by Tasks 3–7; arg validation is cobra-native).

- [ ] **Step 3: Full package + repo verification**

Run: `go test ./internal/cli/ && go test ./... && go vet ./...`
Expected: all PASS, vet clean. (Note: `TestParseHelp_Golden` in `internal/service/llamahelp` may fail locally due to an installed `llama-server` — this is a known env issue, not a regression; do not regenerate the pinned golden.)

- [ ] **Step 4: Commit**

```bash
git add internal/cli/backend_test.go
git commit -m "test(cli): assert full backend command tree and arg validation"
```

---

## Self-Review

**1. Spec coverage**
- `backend list` → Task 3 ✓
- `backend show <id>` → Task 4 ✓
- `backend probe [id]` (all + single) → Task 5 ✓
- `backend schema show <id>` → Task 6 ✓
- `backend schema refresh <id>` → Task 7 ✓
- `backend schema apply <id> -f <file>` (the chosen "edit" semantics, editable=true) → Task 7 ✓
- Shared `RegisterDefaults` (chosen wiring approach) → Task 1 ✓
- `--json` on every leaf → all pure fns take `asJSON` and call `emitJSON` ✓
- Exit codes (0/1) via `exitOnErr`/`*ExitError` ✓

**2. Placeholder scan** — no TBD/TODO; every code step shows full code; error messages are concrete; no "similar to Task N".

**3. Type consistency**
- `backendManager` interface (ListBackends/DefaultBackendID/RefreshSchema) is satisfied by `*backendschema.Manager` (verified: all three methods exist with matching signatures) and by `fakeBackendManager`.
- `backendProber` (Probe) satisfied by `*backendcatalog.Prober` and `fakeProber`.
- `backendcatalog.SchemaStore` (Load/Save/Delete) satisfied by `*fsSchemaStore` and `fakeSchemaStore`.
- `resolveBackend(mgr backendManager, ...)` used identically in Tasks 4–7.
- `backendcatalog.SchemaStoreRef` used in show + apply with the same `b.SchemaRef` input.
- View struct field names (`backendListItem`, `backendDetail`, `probeItem`) are referenced consistently in their tests.
- `RegisterDefaults(m *Manager, schemaStore backendcatalog.SchemaStore)` signature matches its sole call sites (bootstrap + `buildSchemaManager`).

**Anti-pattern compliance:** `schema apply` is the only schema-write path and it always sets `Source.Editable = true`, satisfying the project rule that manual schema edits must be persisted as editable so `RefreshSchema` preserves them.

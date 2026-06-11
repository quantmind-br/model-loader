package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
)

// --- shared fakes for the backend command tests ---

type fakeBackendManager struct {
	backends   []domain.Backend
	defaultID  string
	listErr    error
	refreshErr error
	refreshed  []string
	added      []domain.Backend
	deleted    []string
	setDefault []string
	gens       map[domain.BackendKind]backendschema.Generator
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
func (f *fakeBackendManager) AddBackend(_ context.Context, name, executable string, kind domain.BackendKind) (domain.Backend, error) {
	b := domain.Backend{ID: domain.Slugify(name), Name: name, Kind: kind, Executable: executable}
	f.added = append(f.added, b)
	f.backends = append(f.backends, b)
	return b, nil
}
func (f *fakeBackendManager) DeleteBackend(id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}
func (f *fakeBackendManager) SetDefaultBackend(id string) error {
	f.setDefault = append(f.setDefault, id)
	return nil
}
func (f *fakeBackendManager) Generators() map[domain.BackendKind]backendschema.Generator {
	if f.gens != nil {
		return f.gens
	}
	return map[domain.BackendKind]backendschema.Generator{
		domain.BackendKindLlamaServer: nil,
		domain.BackendKindVLLM:        nil,
	}
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
	_, err := resolveBackend(mgr, "vllm")
	if err == nil {
		t.Fatal("expected ambiguous error")
	}
	if !strings.Contains(err.Error(), "vllm-a") || !strings.Contains(err.Error(), "vllm-b") {
		t.Fatalf("ambiguous error should list candidate IDs, got: %v", err)
	}
	if !strings.Contains(err.Error(), "use a longer prefix") {
		t.Fatalf("ambiguous error should hint about longer prefix, got: %v", err)
	}
}

func TestBackendCommandTree(t *testing.T) {
	bc := childByName(rootCmd, "backend")
	if bc == nil {
		t.Fatal("backend not registered")
	}
	for _, name := range []string{"list", "show", "probe", "schema", "add", "delete", "set-default"} {
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

func TestAddBackend_UnknownKindRejected(t *testing.T) {
	mgr := twoBackends()
	var out bytes.Buffer
	if err := addBackend(&out, mgr, "Tabby", "tabby-api", "tabbyapi"); err == nil {
		t.Fatal("expected error for unknown kind")
	}
	if len(mgr.added) != 0 {
		t.Fatalf("backend must not be added for unknown kind; added=%v", mgr.added)
	}
}

func TestAddBackend_Success(t *testing.T) {
	mgr := twoBackends()
	var out bytes.Buffer
	if err := addBackend(&out, mgr, "My vLLM", "vllm", "vllm"); err != nil {
		t.Fatalf("addBackend: %v", err)
	}
	if len(mgr.added) != 1 || mgr.added[0].Kind != domain.BackendKindVLLM {
		t.Fatalf("expected one vllm backend added, got %v", mgr.added)
	}
}

func TestDeleteBackend_ResolvesAndDeletes(t *testing.T) {
	mgr := twoBackends()
	var out bytes.Buffer
	if err := deleteBackend(&out, mgr, "vllm"); err != nil {
		t.Fatalf("deleteBackend: %v", err)
	}
	if len(mgr.deleted) != 1 || mgr.deleted[0] != "vllm-main" {
		t.Fatalf("expected delete of vllm-main, got %v", mgr.deleted)
	}
}

func TestSetDefaultBackend_ResolvesAndSets(t *testing.T) {
	mgr := twoBackends()
	var out bytes.Buffer
	if err := setDefaultBackend(&out, mgr, "vllm-main"); err != nil {
		t.Fatalf("setDefaultBackend: %v", err)
	}
	if len(mgr.setDefault) != 1 || mgr.setDefault[0] != "vllm-main" {
		t.Fatalf("expected set-default vllm-main, got %v", mgr.setDefault)
	}
}

func TestBackendShow_ArgValidation(t *testing.T) {
	// `backend show` requires exactly one arg; zero args must be a usage error
	// surfaced as a non-zero exit, without touching config/services.
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

package backendschema

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

type fakeGenerator struct {
	gen func(backend domain.Backend) (domain.BackendValidationSchema, error)
}

func (f *fakeGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	return f.gen(backend)
}

func newManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	store := backendcatalog.NewFSStore(dir)
	schemaStore := backendcatalog.NewFSSchemaStore(dir)
	mgr := NewManager(store, schemaStore)
	mgr.Register(domain.BackendKindLlamaServer, &fakeGenerator{
		gen: func(backend domain.Backend) (domain.BackendValidationSchema, error) {
			schema := domain.BackendValidationSchema{
				SchemaVersion: 1,
				Kind:          domain.ValidationSchemaCLIFlagsV1,
				BackendKind:   backend.Kind,
				BackendID:     backend.ID,
				Flags:         make(map[string]domain.FlagSpec),
			}
			ref := backend.SchemaRef
			if len(ref) >= 9 && ref[:9] == "schemas/" {
				ref = ref[9:]
			}
			_ = schemaStore.Save(ref, schema)
			return schema, nil
		},
	})
	return mgr, dir
}

func TestManager_UpdateBackendUpdatesMutableFields(t *testing.T) {
	mgr, _ := newManager(t)

	added, err := mgr.AddBackend(t.Context(), "my-backend", "/bin/echo", domain.BackendKindLlamaServer)
	if err != nil {
		t.Fatalf("AddBackend: %v", err)
	}

	updated, err := mgr.UpdateBackend(added.ID, domain.Backend{
		Name:        "renamed-backend",
		Executable:  "/bin/cat",
		Description: "A better backend",
		Tags:        []string{"fast", "stable"},
	})
	if err != nil {
		t.Fatalf("UpdateBackend: %v", err)
	}

	if updated.Name != "renamed-backend" {
		t.Errorf("Name = %q, want %q", updated.Name, "renamed-backend")
	}
	if updated.Executable != "/bin/cat" {
		t.Errorf("Executable = %q, want %q", updated.Executable, "/bin/cat")
	}
	if updated.Description != "A better backend" {
		t.Errorf("Description = %q, want %q", updated.Description, "A better backend")
	}
	if len(updated.Tags) != 2 || updated.Tags[0] != "fast" || updated.Tags[1] != "stable" {
		t.Errorf("Tags = %v, want [fast stable]", updated.Tags)
	}
}

func TestManager_UpdateBackendPreservesImmutableFields(t *testing.T) {
	mgr, _ := newManager(t)

	added, err := mgr.AddBackend(t.Context(), "preserve-fields", "/bin/echo", domain.BackendKindLlamaServer)
	if err != nil {
		t.Fatalf("AddBackend: %v", err)
	}

	origID := added.ID
	origKind := added.Kind
	origSchemaRef := added.SchemaRef
	origCreatedAt := added.Meta.CreatedAt

	updated, err := mgr.UpdateBackend(added.ID, domain.Backend{
		Name: "preserve-check",
	})
	if err != nil {
		t.Fatalf("UpdateBackend: %v", err)
	}

	if updated.ID != origID {
		t.Errorf("ID changed to %q, want %q", updated.ID, origID)
	}
	if updated.Kind != origKind {
		t.Errorf("Kind changed to %v, want %v", updated.Kind, origKind)
	}
	if updated.SchemaRef != origSchemaRef {
		t.Errorf("SchemaRef changed to %q, want %q", updated.SchemaRef, origSchemaRef)
	}
	if updated.Meta.CreatedAt != origCreatedAt {
		t.Errorf("Meta.CreatedAt changed to %v, want %v", updated.Meta.CreatedAt, origCreatedAt)
	}
	if !updated.Meta.UpdatedAt.After(origCreatedAt) {
		t.Errorf("Meta.UpdatedAt not updated: %v", updated.Meta.UpdatedAt)
	}
}

func TestManager_UpdateBackendReturnsNotFound(t *testing.T) {
	mgr, _ := newManager(t)

	_, err := mgr.UpdateBackend("nonexistent-id", domain.Backend{Name: "should-fail"})
	if err == nil {
		t.Fatal("UpdateBackend expected error for missing backend, got nil")
	}
	if !errors.Is(err, backendcatalog.ErrBackendNotFound) {
		t.Errorf("error = %v, want errors.Is(err, backendcatalog.ErrBackendNotFound)", err)
	}
}

func TestManager_SetDefaultBackendUpdatesCatalog(t *testing.T) {
	mgr, dir := newManager(t)
	store := backendcatalog.NewFSStore(dir)

	b1, err := mgr.AddBackend(t.Context(), "backend-one", "/bin/echo", domain.BackendKindLlamaServer)
	if err != nil {
		t.Fatalf("AddBackend b1: %v", err)
	}
	b2, err := mgr.AddBackend(t.Context(), "backend-two", "/bin/sleep", domain.BackendKindLlamaServer)
	if err != nil {
		t.Fatalf("AddBackend b2: %v", err)
	}

	if err := mgr.SetDefaultBackend(b2.ID); err != nil {
		t.Fatalf("SetDefaultBackend: %v", err)
	}

	catalog, err := store.Load()
	if err != nil {
		t.Fatalf("Load catalog: %v", err)
	}
	if catalog.DefaultBackendID != b2.ID {
		t.Errorf("DefaultBackendID = %q, want %q", catalog.DefaultBackendID, b2.ID)
	}
	_ = b1
}

func TestManager_SetDefaultBackendReturnsNotFound(t *testing.T) {
	mgr, _ := newManager(t)

	err := mgr.SetDefaultBackend("nonexistent-id")
	if err == nil {
		t.Fatal("SetDefaultBackend expected error for missing backend, got nil")
	}
	if !errors.Is(err, backendcatalog.ErrBackendNotFound) {
		t.Errorf("error = %v, want errors.Is(err, backendcatalog.ErrBackendNotFound)", err)
	}
}

func TestManager_AddBackendRejectsDuplicate(t *testing.T) {
	mgr, _ := newManager(t)
	ctx := t.Context()

	_, err := mgr.AddBackend(ctx, "my-backend", "/bin/echo", domain.BackendKindLlamaServer)
	if err != nil {
		t.Fatalf("first AddBackend: %v", err)
	}

	_, err = mgr.AddBackend(ctx, "my-backend", "/bin/cat", domain.BackendKindLlamaServer)
	if err == nil {
		t.Fatal("expected error for duplicate backend, got nil")
	}
}

func TestManager_DefaultBackendID(t *testing.T) {
	mgr, _ := newManager(t)
	ctx := t.Context()

	id, err := mgr.DefaultBackendID()
	if err != nil {
		t.Fatalf("DefaultBackendID: %v", err)
	}
	if id != "" {
		t.Errorf("DefaultBackendID = %q, want empty", id)
	}

	b1, err := mgr.AddBackend(ctx, "first", "/bin/echo", domain.BackendKindLlamaServer)
	if err != nil {
		t.Fatalf("AddBackend: %v", err)
	}
	b2, err := mgr.AddBackend(ctx, "second", "/bin/sleep", domain.BackendKindLlamaServer)
	if err != nil {
		t.Fatalf("AddBackend: %v", err)
	}

	id, _ = mgr.DefaultBackendID()
	if id != b1.ID {
		t.Errorf("after add, DefaultBackendID = %q, want %q", id, b1.ID)
	}

	if err := mgr.SetDefaultBackend(b2.ID); err != nil {
		t.Fatalf("SetDefaultBackend: %v", err)
	}
	id, _ = mgr.DefaultBackendID()
	if id != b2.ID {
		t.Errorf("after set, DefaultBackendID = %q, want %q", id, b2.ID)
	}
}

func TestManager_AddSGLangBackendGeneratesSchema(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSStore(dir)
	schemaStore := backendcatalog.NewFSSchemaStore(dir)
	mgr := NewManager(store, schemaStore)
	mgr.Register(domain.BackendKindSGLang, NewSGLangGenerator(schemaStore))

	// Create a fake python binary in a temp dir so llamabin.Resolve succeeds.
	tmpBin := filepath.Join(dir, "python")
	if err := os.WriteFile(tmpBin, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPATH := os.Getenv("PATH")
	os.Setenv("PATH", dir)
	defer os.Setenv("PATH", oldPATH)

	b, err := mgr.AddBackend(t.Context(), "sglang-dev", "python -m sglang.launch_server", domain.BackendKindSGLang)
	if err != nil {
		t.Fatalf("AddBackend sglang: %v", err)
	}
	if b.Kind != domain.BackendKindSGLang {
		t.Errorf("Kind = %q, want %q", b.Kind, domain.BackendKindSGLang)
	}

	schema, err := schemaStore.Load("sglang-dev.json")
	if err != nil {
		t.Fatalf("load schema: %v", err)
	}
	if schema.BackendKind != domain.BackendKindSGLang {
		t.Errorf("schema.BackendKind = %q, want %q", schema.BackendKind, domain.BackendKindSGLang)
	}
	if _, ok := schema.Flags["model-path"]; !ok {
		t.Errorf("schema missing 'model-path' flag")
	}
	if _, ok := schema.Flags["tp-size"]; !ok {
		t.Errorf("schema missing 'tp-size' flag")
	}
}

func TestManager_AddSGLangBackendFallsBackToPython3(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSStore(dir)
	schemaStore := backendcatalog.NewFSSchemaStore(dir)
	mgr := NewManager(store, schemaStore)
	mgr.Register(domain.BackendKindSGLang, NewSGLangGenerator(schemaStore))

	// Only python3 exists, not python.
	tmpBin := filepath.Join(dir, "python3")
	if err := os.WriteFile(tmpBin, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPATH := os.Getenv("PATH")
	os.Setenv("PATH", dir)
	defer os.Setenv("PATH", oldPATH)

	_, err := mgr.AddBackend(t.Context(), "sglang-py3", "python -m sglang.launch_server", domain.BackendKindSGLang)
	if err != nil {
		t.Fatalf("AddBackend with python3 fallback: %v", err)
	}
}
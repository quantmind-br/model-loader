package backendcatalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/llamabin"
)

func TestResolver_ResolveExistingBackend(t *testing.T) {
	dir := t.TempDir()
	executable := writeExecutable(t, dir)
	catalogStore := NewFSStore(dir)
	schemaStore := NewFSSchemaStore(dir)
	catalog := DefaultCatalog(executable)

	if err := catalogStore.Save(catalog); err != nil {
		t.Fatalf("Save catalog: %v", err)
	}
	if err := schemaStore.Save("llama-cpp-default.json", sampleSchema()); err != nil {
		t.Fatalf("Save schema: %v", err)
	}

	resolved, err := NewResolver(catalogStore, schemaStore, log.Nop()).Resolve(domain.Profile{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Backend.ID != defaultBackendID {
		t.Errorf("Backend.ID = %q, want %q", resolved.Backend.ID, defaultBackendID)
	}
	if resolved.ExecutablePath != executable {
		t.Errorf("ExecutablePath = %q, want %q", resolved.ExecutablePath, executable)
	}
	if resolved.Schema.BackendID != defaultBackendID {
		t.Errorf("Schema.BackendID = %q, want %q", resolved.Schema.BackendID, defaultBackendID)
	}
}

func TestResolver_ProfileBackendOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	executable := writeExecutable(t, dir)
	catalogStore := NewFSStore(dir)
	schemaStore := NewFSSchemaStore(dir)
	catalog := domain.BackendCatalog{
		SchemaVersion:    catalogSchemaVersion,
		DefaultBackendID: "default",
		Backends: []domain.Backend{
			{ID: "default", Executable: executable, SchemaRef: "schemas/default.json"},
			{ID: "profile", Executable: executable, SchemaRef: "schemas/profile.json"},
		},
	}

	if err := catalogStore.Save(catalog); err != nil {
		t.Fatalf("Save catalog: %v", err)
	}
	profileSchema := sampleSchema()
	profileSchema.BackendID = "profile"
	if err := schemaStore.Save("profile.json", profileSchema); err != nil {
		t.Fatalf("Save schema: %v", err)
	}

	resolved, err := NewResolver(catalogStore, schemaStore, log.Nop()).Resolve(domain.Profile{
		Launch: domain.LaunchConfig{BackendID: "profile"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Backend.ID != "profile" {
		t.Errorf("Backend.ID = %q, want profile", resolved.Backend.ID)
	}
}

func TestResolver_Errors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, dir string) (*fsStore, *fsSchemaStore, domain.Profile)
		wantErr error
	}{
		{
			name: "no backend selected",
			setup: func(t *testing.T, dir string) (*fsStore, *fsSchemaStore, domain.Profile) {
				catalogStore := NewFSStore(dir)
				if err := catalogStore.Save(domain.BackendCatalog{}); err != nil {
					t.Fatal(err)
				}
				return catalogStore, NewFSSchemaStore(dir), domain.Profile{}
			},
			wantErr: ErrNoBackendSelected,
		},
		{
			name: "backend absent",
			setup: func(t *testing.T, dir string) (*fsStore, *fsSchemaStore, domain.Profile) {
				catalogStore := NewFSStore(dir)
				if err := catalogStore.Save(DefaultCatalog(writeExecutable(t, dir))); err != nil {
					t.Fatal(err)
				}
				return catalogStore, NewFSSchemaStore(dir), domain.Profile{Launch: domain.LaunchConfig{BackendID: "missing"}}
			},
			wantErr: ErrBackendNotFound,
		},
		{
			name: "schema absent",
			setup: func(t *testing.T, dir string) (*fsStore, *fsSchemaStore, domain.Profile) {
				catalogStore := NewFSStore(dir)
				if err := catalogStore.Save(DefaultCatalog(writeExecutable(t, dir))); err != nil {
					t.Fatal(err)
				}
				return catalogStore, NewFSSchemaStore(dir), domain.Profile{}
			},
			wantErr: ErrSchemaNotFound,
		},
		{
			name: "executable absent",
			setup: func(t *testing.T, dir string) (*fsStore, *fsSchemaStore, domain.Profile) {
				catalogStore := NewFSStore(dir)
				catalog := DefaultCatalog(filepath.Join(dir, "missing"))
				if err := catalogStore.Save(catalog); err != nil {
					t.Fatal(err)
				}
				if err := NewFSSchemaStore(dir).Save("llama-cpp-default.json", sampleSchema()); err != nil {
					t.Fatal(err)
				}
				return catalogStore, NewFSSchemaStore(dir), domain.Profile{}
			},
			wantErr: llamabin.ErrBinaryNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			catalogStore, schemaStore, profile := tt.setup(t, dir)
			_, err := NewResolver(catalogStore, schemaStore, log.Nop()).Resolve(profile)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolver_SGLangFallsBackToPython3(t *testing.T) {
	dir := t.TempDir()
	catalogStore := NewFSStore(dir)
	schemaStore := NewFSSchemaStore(dir)

	// Only python3 exists, not python.
	py3 := filepath.Join(dir, "python3")
	if err := os.WriteFile(py3, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPATH := os.Getenv("PATH")
	os.Setenv("PATH", dir)
	defer os.Setenv("PATH", oldPATH)

	catalog := domain.BackendCatalog{
		SchemaVersion:    catalogSchemaVersion,
		DefaultBackendID: "sglang",
		Backends: []domain.Backend{
			{ID: "sglang", Name: "SGLang", Kind: domain.BackendKindSGLang, Executable: "python -m sglang.launch_server", SchemaRef: "schemas/sglang.json"},
		},
	}
	if err := catalogStore.Save(catalog); err != nil {
		t.Fatalf("Save catalog: %v", err)
	}
	schema := sampleSchema()
	schema.BackendID = "sglang"
	schema.BackendKind = domain.BackendKindSGLang
	if err := schemaStore.Save("sglang.json", schema); err != nil {
		t.Fatalf("Save schema: %v", err)
	}

	resolved, err := NewResolver(catalogStore, schemaStore, log.Nop()).Resolve(domain.Profile{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := py3 + " -m sglang.launch_server"
	if resolved.ExecutablePath != want {
		t.Errorf("ExecutablePath = %q, want %q", resolved.ExecutablePath, want)
	}
}

func writeExecutable(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "llama-server")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

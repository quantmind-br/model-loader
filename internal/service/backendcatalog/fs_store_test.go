package backendcatalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
)

func TestFSStore_LoadMissingReturnsEmptyCatalog(t *testing.T) {
	store := NewFSStore(t.TempDir())

	catalog, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if catalog.SchemaVersion != catalogSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", catalog.SchemaVersion, catalogSchemaVersion)
	}
	if len(catalog.Backends) != 0 {
		t.Errorf("Backends len = %d, want 0", len(catalog.Backends))
	}
}

func TestFSStore_SaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewFSStore(dir)
	catalog := DefaultCatalog("/bin/sh")

	if err := store.Save(catalog); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.SchemaVersion != catalogSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, catalogSchemaVersion)
	}
	if got.DefaultBackendID != defaultBackendID {
		t.Errorf("DefaultBackendID = %q, want %q", got.DefaultBackendID, defaultBackendID)
	}
	if len(got.Backends) != 1 || got.Backends[0].Executable != "/bin/sh" {
		t.Fatalf("Backends = %+v, want one backend with executable /bin/sh", got.Backends)
	}
	assertNoTmpFiles(t, dir)
}

func TestFSSchemaStore_SaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewFSSchemaStore(dir)
	schema := sampleSchema()

	if err := store.Save("llama-cpp-upstream.json", schema); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load("llama-cpp-upstream.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.SchemaVersion != catalogSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, catalogSchemaVersion)
	}
	if got.BackendID != schema.BackendID {
		t.Errorf("BackendID = %q, want %q", got.BackendID, schema.BackendID)
	}
	if _, ok := got.Flags["port"]; !ok {
		t.Errorf("Flags missing port: %+v", got.Flags)
	}
	assertNoTmpFiles(t, filepath.Join(dir, "schemas"))
}

func TestFSSchemaStore_PathTraversalRejected(t *testing.T) {
	store := NewFSSchemaStore(t.TempDir())
	tests := []struct {
		name string
		ref  string
	}{
		{"parent", "../catalog.json"},
		{"nested parent", "nested/../../catalog.json"},
		{"absolute", filepath.Join(string(filepath.Separator), "tmp", "schema.json")},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.Save(tt.ref, sampleSchema()); !errors.Is(err, ErrInvalidSchemaRef) {
				t.Errorf("Save err = %v, want ErrInvalidSchemaRef", err)
			}
			if _, err := store.Load(tt.ref); !errors.Is(err, ErrInvalidSchemaRef) {
				t.Errorf("Load err = %v, want ErrInvalidSchemaRef", err)
			}
		})
	}
}

func TestFSSchemaStore_LoadMissing(t *testing.T) {
	store := NewFSSchemaStore(t.TempDir())

	_, err := store.Load("missing.json")
	if !errors.Is(err, ErrSchemaNotFound) {
		t.Errorf("err = %v, want ErrSchemaNotFound", err)
	}
}

func sampleSchema() domain.BackendValidationSchema {
	return domain.BackendValidationSchema{
		SchemaVersion: catalogSchemaVersion,
		Kind:          domain.ValidationSchemaCLIFlagsV1,
		BackendKind:   domain.BackendKindLlamaServer,
		BackendID:     defaultBackendID,
		Flags: map[string]domain.FlagSpec{
			"port": {Long: "port", Type: domain.FlagTypeInt, Default: float64(8080)},
		},
	}
}

func assertNoTmpFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".tmp" {
			t.Errorf("found leftover tmp file: %s", entry.Name())
		}
	}
}

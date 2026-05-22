package migration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

type fakeGenerator struct {
	schemaStore backendcatalog.SchemaStore
	fail        bool
}

func (f *fakeGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if f.fail {
		return domain.BackendValidationSchema{}, errors.New("schema generation failed")
	}
	schema := domain.BackendValidationSchema{
		SchemaVersion: 1,
		BackendID:     backend.ID,
		BackendKind:   backend.Kind,
	}
	if f.schemaStore != nil {
		ref := backend.SchemaRef
		const prefix = "schemas/"
		if len(ref) >= len(prefix) && ref[:len(prefix)] == prefix {
			ref = ref[len(prefix):]
		}
		if err := f.schemaStore.Save(ref, schema); err != nil {
			return domain.BackendValidationSchema{}, err
		}
	}
	return schema, nil
}

func TestMigration_NoMigrationNeeded(t *testing.T) {
	dir := t.TempDir()
	profileStore, _ := profilestore.NewFSStore(dir)
	catalogStore := backendcatalog.NewFSStore(dir)
	schemaStore := backendcatalog.NewFSSchemaStore(dir)
	mgr := backendschema.NewManager(catalogStore, schemaStore)

	_ = profileStore.Save(domain.Profile{
		ID:   "p1",
		Name: "Profile 1",
		Launch: domain.LaunchConfig{
			BackendID: "some-backend",
		},
	})

	svc := NewService(config.AppConfig{}, profileStore, catalogStore, schemaStore, mgr)
	rep, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.MigratedProfiles != 0 {
		t.Errorf("MigratedProfiles = %d, want 0", rep.MigratedProfiles)
	}
	if rep.CreatedBackends != 0 {
		t.Errorf("CreatedBackends = %d, want 0", rep.CreatedBackends)
	}
}

func TestMigration_MigratesLegacyBinaryPath(t *testing.T) {
	dir := t.TempDir()
	profileStore, _ := profilestore.NewFSStore(dir)
	catalogDir := t.TempDir()
	catalogStore := backendcatalog.NewFSStore(catalogDir)
	schemaStore := backendcatalog.NewFSSchemaStore(catalogDir)
	mgr := backendschema.NewManager(catalogStore, schemaStore)
	mgr.Register(domain.BackendKindLlamaServer, &fakeGenerator{schemaStore: schemaStore})

	_ = profileStore.Save(domain.Profile{
		ID:   "p1",
		Name: "Profile 1",
		Launch: domain.LaunchConfig{
			LlamaServerBinaryPath: "/custom/llama-server",
		},
	})

	svc := NewService(config.AppConfig{}, profileStore, catalogStore, schemaStore, mgr)
	rep, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.MigratedProfiles != 1 {
		t.Errorf("MigratedProfiles = %d, want 1", rep.MigratedProfiles)
	}
	if rep.CreatedBackends != 2 {
		t.Errorf("CreatedBackends = %d, want 2", rep.CreatedBackends)
	}

	profiles, _ := profileStore.List()
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].Launch.BackendID == "" {
		t.Error("expected BackendID to be set")
	}
	if profiles[0].Launch.LlamaServerBinaryPath != "" {
		t.Errorf("expected LlamaServerBinaryPath to be cleared, got %q", profiles[0].Launch.LlamaServerBinaryPath)
	}
}

func TestMigration_UsesDefaultBackendForEmptyPath(t *testing.T) {
	dir := t.TempDir()
	profileStore, _ := profilestore.NewFSStore(dir)
	catalogDir := t.TempDir()
	catalogStore := backendcatalog.NewFSStore(catalogDir)
	schemaStore := backendcatalog.NewFSSchemaStore(catalogDir)
	mgr := backendschema.NewManager(catalogStore, schemaStore)
	mgr.Register(domain.BackendKindLlamaServer, &fakeGenerator{schemaStore: schemaStore})

	_ = profileStore.Save(domain.Profile{
		ID:   "p1",
		Name: "Profile 1",
		Launch: domain.LaunchConfig{
			LlamaServerBinaryPath: "",
		},
	})

	svc := NewService(config.AppConfig{}, profileStore, catalogStore, schemaStore, mgr)
	rep, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.MigratedProfiles != 1 {
		t.Errorf("MigratedProfiles = %d, want 1", rep.MigratedProfiles)
	}
	if rep.CreatedBackends != 1 {
		t.Errorf("CreatedBackends = %d, want 1", rep.CreatedBackends)
	}

	profiles, _ := profileStore.List()
	if profiles[0].Launch.BackendID == "" {
		t.Error("expected BackendID to be set to default")
	}
}

func TestMigration_Idempotent(t *testing.T) {
	dir := t.TempDir()
	profileStore, _ := profilestore.NewFSStore(dir)
	catalogDir := t.TempDir()
	catalogStore := backendcatalog.NewFSStore(catalogDir)
	schemaStore := backendcatalog.NewFSSchemaStore(catalogDir)
	mgr := backendschema.NewManager(catalogStore, schemaStore)
	mgr.Register(domain.BackendKindLlamaServer, &fakeGenerator{schemaStore: schemaStore})

	_ = profileStore.Save(domain.Profile{
		ID:   "p1",
		Name: "Profile 1",
		Launch: domain.LaunchConfig{
			LlamaServerBinaryPath: "/custom/llama-server",
		},
	})

	svc := NewService(config.AppConfig{}, profileStore, catalogStore, schemaStore, mgr)
	rep1, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run 1: %v", err)
	}
	if rep1.MigratedProfiles != 1 {
		t.Errorf("first run MigratedProfiles = %d, want 1", rep1.MigratedProfiles)
	}

	rep2, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	if rep2.MigratedProfiles != 0 {
		t.Errorf("second run MigratedProfiles = %d, want 0", rep2.MigratedProfiles)
	}
	if rep2.CreatedBackends != 0 {
		t.Errorf("second run CreatedBackends = %d, want 0", rep2.CreatedBackends)
	}
}

func TestMigration_MapsExistingBackend(t *testing.T) {
	dir := t.TempDir()
	profileStore, _ := profilestore.NewFSStore(dir)
	catalogDir := t.TempDir()
	catalogStore := backendcatalog.NewFSStore(catalogDir)
	schemaStore := backendcatalog.NewFSSchemaStore(catalogDir)
	mgr := backendschema.NewManager(catalogStore, schemaStore)

	catalog := domain.BackendCatalog{
		SchemaVersion:    1,
		DefaultBackendID: "default",
		Backends: []domain.Backend{
			{
				ID:         "default",
				Name:       "Default",
				Kind:       domain.BackendKindLlamaServer,
				Executable: "/usr/bin/llama-server",
				SchemaRef:  "schemas/default.json",
			},
		},
	}
	_ = catalogStore.Save(catalog)

	_ = profileStore.Save(domain.Profile{
		ID:   "p1",
		Name: "Profile 1",
		Launch: domain.LaunchConfig{
			LlamaServerBinaryPath: "/usr/bin/llama-server",
		},
	})

	svc := NewService(config.AppConfig{}, profileStore, catalogStore, schemaStore, mgr)
	rep, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.MigratedProfiles != 1 {
		t.Errorf("MigratedProfiles = %d, want 1", rep.MigratedProfiles)
	}
	if rep.CreatedBackends != 0 {
		t.Errorf("CreatedBackends = %d, want 0 (should reuse existing)", rep.CreatedBackends)
	}

	profiles, _ := profileStore.List()
	if profiles[0].Launch.BackendID != "default" {
		t.Errorf("BackendID = %q, want default", profiles[0].Launch.BackendID)
	}
}

func TestMigration_UsesConfigBinaryPath(t *testing.T) {
	dir := t.TempDir()
	profileStore, _ := profilestore.NewFSStore(dir)
	catalogDir := t.TempDir()
	catalogStore := backendcatalog.NewFSStore(catalogDir)
	schemaStore := backendcatalog.NewFSSchemaStore(catalogDir)
	mgr := backendschema.NewManager(catalogStore, schemaStore)
	mgr.Register(domain.BackendKindLlamaServer, &fakeGenerator{schemaStore: schemaStore})

	_ = profileStore.Save(domain.Profile{
		ID:   "p1",
		Name: "Profile 1",
		Launch: domain.LaunchConfig{
			LlamaServerBinaryPath: "",
		},
	})

	cfg := config.AppConfig{
		Paths: config.PathsConfig{
			LlamaServerBinaryPath: "/opt/llama-server",
		},
	}

	svc := NewService(cfg, profileStore, catalogStore, schemaStore, mgr)
	rep, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.CreatedBackends != 1 {
		t.Errorf("CreatedBackends = %d, want 1", rep.CreatedBackends)
	}

	catalog, _ := catalogStore.Load()
	if len(catalog.Backends) != 1 {
		t.Fatalf("expected 1 backend, got %d", len(catalog.Backends))
	}
	if catalog.Backends[0].Executable != "/opt/llama-server" {
		t.Errorf("Executable = %q, want /opt/llama-server", catalog.Backends[0].Executable)
	}
}

func TestMigration_PreservesCustomBackendOnSchemaFailure(t *testing.T) {
	dir := t.TempDir()
	profileStore, _ := profilestore.NewFSStore(dir)
	catalogDir := t.TempDir()
	catalogStore := backendcatalog.NewFSStore(catalogDir)
	schemaStore := backendcatalog.NewFSSchemaStore(catalogDir)
	mgr := backendschema.NewManager(catalogStore, schemaStore)
	mgr.Register(domain.BackendKindLlamaServer, &fakeGenerator{fail: true})

	_ = profileStore.Save(domain.Profile{
		ID:   "p1",
		Name: "Profile 1",
		Launch: domain.LaunchConfig{
			LlamaServerBinaryPath: "/custom/llama-server",
		},
	})

	svc := NewService(config.AppConfig{}, profileStore, catalogStore, schemaStore, mgr)
	rep, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.MigratedProfiles != 1 {
		t.Errorf("MigratedProfiles = %d, want 1", rep.MigratedProfiles)
	}
	if rep.CreatedBackends != 2 {
		t.Errorf("CreatedBackends = %d, want 2", rep.CreatedBackends)
	}
	if len(rep.Warnings) == 0 {
		t.Error("expected warnings about schema generation failure")
	}

	profiles, _ := profileStore.List()
	if profiles[0].Launch.BackendID == "" {
		t.Error("expected BackendID to be set")
	}

	cat, _ := catalogStore.Load()
	if profiles[0].Launch.BackendID == cat.DefaultBackendID {
		t.Error("expected custom backend, not default")
	}
}

func TestEnsurePresentations_SeedsMissing(t *testing.T) {
	dir := t.TempDir()
	schemaStore := backendcatalog.NewFSSchemaStore(dir)
	ref := "llama.json"
	_ = schemaStore.Save(ref, domain.BackendValidationSchema{
		SchemaVersion: 1,
		Kind:          domain.ValidationSchemaCLIFlagsV1,
		BackendKind:   domain.BackendKindLlamaServer,
		BackendID:     "llama",
		Flags:         map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt, Group: "common"}},
	})
	catalog := domain.BackendCatalog{
		SchemaVersion:    1,
		DefaultBackendID: "llama",
		Backends:         []domain.Backend{{ID: "llama", Kind: domain.BackendKindLlamaServer, SchemaRef: "schemas/llama.json"}},
	}
	if n, err := ensurePresentations(catalog, schemaStore); err != nil || n != 1 {
		t.Fatalf("ensurePresentations n=%d err=%v", n, err)
	}
	got, _ := schemaStore.Load(ref)
	if got.Presentation == nil || len(got.Presentation.Groups) == 0 {
		t.Fatalf("presentation not seeded: %+v", got)
	}
	// idempotent: second run seeds nothing.
	if n, _ := ensurePresentations(catalog, schemaStore); n != 0 {
		t.Fatalf("expected idempotent second run, seeded %d", n)
	}
}

func TestMigration_SetsUpdatedAt(t *testing.T) {
	dir := t.TempDir()
	profileStore, _ := profilestore.NewFSStore(dir)
	catalogDir := t.TempDir()
	catalogStore := backendcatalog.NewFSStore(catalogDir)
	schemaStore := backendcatalog.NewFSSchemaStore(catalogDir)
	mgr := backendschema.NewManager(catalogStore, schemaStore)

	before := time.Now().UTC().Add(-time.Hour)
	_ = profileStore.Save(domain.Profile{
		ID:   "p1",
		Name: "Profile 1",
		Meta: domain.ProfileMeta{UpdatedAt: before},
		Launch: domain.LaunchConfig{
			BackendID: "",
		},
	})

	svc := NewService(config.AppConfig{}, profileStore, catalogStore, schemaStore, mgr)
	_, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	profiles, _ := profileStore.List()
	if profiles[0].Meta.UpdatedAt.Equal(before) {
		t.Error("expected UpdatedAt to be updated")
	}
}

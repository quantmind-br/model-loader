package migration

import (
	"context"
	"fmt"
	"time"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

// Service runs one-time migration from legacy binary paths to backend IDs.
type Service interface {
	Run(ctx context.Context) (Report, error)
}

// Report summarizes migration outcomes.
type Report struct {
	CreatedBackends  int
	MigratedProfiles int
	SchemaFailures   int
	Warnings         []string
}

type migrationService struct {
	cfg          config.AppConfig
	profileStore profilestore.Store
	catalogStore backendcatalog.Store
	schemaStore  backendcatalog.SchemaStore
	manager      *backendschema.Manager
}

// NewService returns a migration service.
func NewService(cfg config.AppConfig, profileStore profilestore.Store, catalogStore backendcatalog.Store, schemaStore backendcatalog.SchemaStore, manager *backendschema.Manager) Service {
	return &migrationService{
		cfg:          cfg,
		profileStore: profileStore,
		catalogStore: catalogStore,
		schemaStore:  schemaStore,
		manager:      manager,
	}
}

// Run executes migration if needed. Idempotent.
func (s *migrationService) Run(ctx context.Context) (Report, error) {
	var rep Report

	profiles, err := s.profileStore.List()
	if err != nil {
		return rep, fmt.Errorf("list profiles: %w", err)
	}

	needs, err := needsMigration(profiles)
	if err != nil {
		return rep, err
	}
	if !needs {
		catalog, err := s.catalogStore.Load()
		if err == nil {
			_, _ = ensurePresentations(catalog, s.schemaStore)
		}
		return rep, nil
	}

	catalog, err := s.catalogStore.Load()
	if err != nil {
		return rep, fmt.Errorf("load catalog: %w", err)
	}

	if len(catalog.Backends) == 0 {
		executable := s.cfg.Paths.LlamaServerBinaryPath
		if executable == "" {
			executable = "llama-server"
		}
		catalog = backendcatalog.DefaultCatalog(executable)
		if err := s.catalogStore.Save(catalog); err != nil {
			return rep, fmt.Errorf("save default catalog: %w", err)
		}
		rep.CreatedBackends++

		defaultBackend := catalog.Backends[0]
		if err := backendschema.WriteEmbeddedFallback(s.schemaStore, defaultBackend.ID, defaultBackend.SchemaRef); err != nil {
			rep.SchemaFailures++
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("default backend schema fallback: %v", err))
		}
	}

	for _, p := range profiles {
		if p.Launch.BackendID != "" {
			continue
		}

		if p.Launch.LlamaServerBinaryPath == "" {
			p.Launch.BackendID = catalog.DefaultBackendID
		} else {
			backendID := findBackendByExecutable(catalog.Backends, p.Launch.LlamaServerBinaryPath)
			if backendID == "" {
				id := domain.Slugify(p.Launch.LlamaServerBinaryPath)
				if id == "" {
					id = "custom-backend"
				}
				backend, err := s.manager.AddBackend(ctx, id, p.Launch.LlamaServerBinaryPath, domain.BackendKindLlamaServer)
				if err != nil {
					rep.Warnings = append(rep.Warnings, fmt.Sprintf("profile %s: schema generation failed for %s: %v", p.ID, p.Launch.LlamaServerBinaryPath, err))
					backend = domain.Backend{
						ID:         id,
						Name:       id,
						Kind:       domain.BackendKindLlamaServer,
						Executable: p.Launch.LlamaServerBinaryPath,
						SchemaRef:  "schemas/" + id + ".json",
					}
					catalog.Backends = append(catalog.Backends, backend)
					if err := s.catalogStore.Save(catalog); err != nil {
						rep.Warnings = append(rep.Warnings, fmt.Sprintf("profile %s: save catalog: %v", p.ID, err))
						p.Launch.BackendID = catalog.DefaultBackendID
					} else {
						_ = backendschema.WriteEmbeddedFallback(s.schemaStore, backend.ID, backend.SchemaRef)
						backendID = backend.ID
						rep.CreatedBackends++
						catalog, _ = s.catalogStore.Load()
					}
				} else {
					backendID = backend.ID
					rep.CreatedBackends++
					catalog, _ = s.catalogStore.Load()
				}
			}
			p.Launch.BackendID = backendID
		}

		p.Launch.LlamaServerBinaryPath = ""
		p.Meta.UpdatedAt = time.Now().UTC()
		if err := s.profileStore.Save(p); err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("profile %s: save: %v", p.ID, err))
			continue
		}
		rep.MigratedProfiles++
	}

	catalog, _ = s.catalogStore.Load()
	if n, err := ensurePresentations(catalog, s.schemaStore); err != nil {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("ensure presentations: %v", err))
	} else if n > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("seeded %d backend presentation(s)", n))
	}

	return rep, nil
}

// ensurePresentations seeds a default Presentation into every backend schema
// that lacks one. Idempotent: schemas that already have a Presentation are
// skipped. Returns the number of schemas updated. It does NOT mark the schema
// Source.Editable — a synthesized default is not a manual edit, so RefreshSchema
// is free to regenerate it.
func ensurePresentations(catalog domain.BackendCatalog, schemaStore backendcatalog.SchemaStore) (int, error) {
	updated := 0
	for _, b := range catalog.Backends {
		ref := backendcatalog.SchemaStoreRef(b.SchemaRef)
		schema, err := schemaStore.Load(ref)
		if err != nil {
			continue // missing schema handled elsewhere; skip
		}
		if schema.Presentation != nil {
			continue
		}
		pres := backendschema.BuildPresentation(schema)
		schema.Presentation = &pres
		if err := schemaStore.Save(ref, schema); err != nil {
			return updated, fmt.Errorf("save presentation for %s: %w", b.ID, err)
		}
		updated++
	}
	return updated, nil
}

func needsMigration(profiles []domain.Profile) (bool, error) {
	for _, p := range profiles {
		if p.Launch.BackendID == "" && p.Launch.LlamaServerBinaryPath != "" {
			return true, nil
		}
		if p.Launch.BackendID == "" && p.Launch.LlamaServerBinaryPath == "" {
			return true, nil
		}
	}
	return false, nil
}

func findBackendByExecutable(backends []domain.Backend, executable string) string {
	for _, b := range backends {
		if b.Executable == executable {
			return b.ID
		}
	}
	return ""
}

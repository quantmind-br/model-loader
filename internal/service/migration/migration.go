package migration

import (
	"context"
	"fmt"
	"time"

	"github.com/quantmind-br/llama-cpp-loader/internal/config"
	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/backendschema"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/profilestore"
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
		if err := generateOrFallback(s.schemaStore, s.manager, defaultBackend); err != nil {
			rep.SchemaFailures++
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("default backend schema: %v", err))
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
					rep.Warnings = append(rep.Warnings, fmt.Sprintf("profile %s: create backend: %v", p.ID, err))
					p.Launch.BackendID = catalog.DefaultBackendID
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

	return rep, nil
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

func generateOrFallback(schemaStore backendcatalog.SchemaStore, manager *backendschema.Manager, backend domain.Backend) error {
	if g, ok := manager.Generators()[domain.BackendKindLlamaServer]; ok {
		if _, err := g.Generate(backend); err != nil {
			return backendschema.WriteEmbeddedFallback(schemaStore, backend.ID, backend.SchemaRef)
		}
		return nil
	}
	return backendschema.WriteEmbeddedFallback(schemaStore, backend.ID, backend.SchemaRef)
}

package backendschema

import (
	"context"
	"fmt"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

// Manager orchestrates backend catalog CRUD and schema generation.
type Manager struct {
	catalogStore backendcatalog.Store
	schemaStore  backendcatalog.SchemaStore
	generators   map[domain.BackendKind]Generator
}

// NewManager returns a manager wired to catalog and schema stores.
func NewManager(catalogStore backendcatalog.Store, schemaStore backendcatalog.SchemaStore) *Manager {
	return &Manager{
		catalogStore: catalogStore,
		schemaStore:  schemaStore,
		generators:   make(map[domain.BackendKind]Generator),
	}
}

// Register binds a generator to a backend kind.
func (m *Manager) Register(kind domain.BackendKind, g Generator) {
	m.generators[kind] = g
}

// Generators returns the registered generator map (read-only).
func (m *Manager) Generators() map[domain.BackendKind]Generator {
	return m.generators
}

// AddBackend creates a new backend entry, generates its schema, and persists the catalog.
// Schema is generated BEFORE catalog is saved so an invalid binary does not create a
// broken catalog entry. If catalog save fails after schema generation, the schema is
// left as an orphan (harmless) rather than a catalog entry without a schema.
func (m *Manager) AddBackend(ctx context.Context, name, executable string, kind domain.BackendKind) (domain.Backend, error) {
	id := domain.Slugify(name)
	if id == "" {
		return domain.Backend{}, fmt.Errorf("invalid backend name")
	}

	catalog, err := m.catalogStore.Load()
	if err != nil {
		return domain.Backend{}, fmt.Errorf("load catalog: %w", err)
	}

	now := time.Now().UTC()
	backend := domain.Backend{
		ID:         id,
		Name:       name,
		Kind:       kind,
		Executable: executable,
		SchemaRef:  "schemas/" + id + ".json",
		Meta: domain.BackendMeta{
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	if existing, ok := findBackend(catalog.Backends, id); ok {
		backend.Meta.CreatedAt = existing.Meta.CreatedAt
	}

	if g, ok := m.generators[kind]; ok {
		if _, err := g.Generate(backend); err != nil {
			return domain.Backend{}, fmt.Errorf("generate schema: %w", err)
		}
	} else {
		return domain.Backend{}, fmt.Errorf("no generator registered for kind: %s", kind)
	}

	catalog.Backends = upsertBackend(catalog.Backends, backend)
	if catalog.DefaultBackendID == "" {
		catalog.DefaultBackendID = id
	}

	if err := m.catalogStore.Save(catalog); err != nil {
		return domain.Backend{}, fmt.Errorf("save catalog: %w", err)
	}

	return backend, nil
}

// RefreshSchema re-generates the schema for an existing backend.
func (m *Manager) RefreshSchema(backendID string) error {
	catalog, err := m.catalogStore.Load()
	if err != nil {
		return fmt.Errorf("load catalog: %w", err)
	}

	backend, ok := findBackend(catalog.Backends, backendID)
	if !ok {
		return fmt.Errorf("%w: %s", backendcatalog.ErrBackendNotFound, backendID)
	}

	g, ok := m.generators[backend.Kind]
	if !ok {
		return fmt.Errorf("no generator registered for kind: %s", backend.Kind)
	}

	if _, err := g.Generate(backend); err != nil {
		return fmt.Errorf("generate schema: %w", err)
	}
	return nil
}

// ListBackends returns all backends in the catalog.
func (m *Manager) ListBackends() ([]domain.Backend, error) {
	catalog, err := m.catalogStore.Load()
	if err != nil {
		return nil, err
	}
	return catalog.Backends, nil
}

// GetBackend returns a single backend by ID.
func (m *Manager) GetBackend(id string) (domain.Backend, error) {
	catalog, err := m.catalogStore.Load()
	if err != nil {
		return domain.Backend{}, err
	}
	if b, ok := findBackend(catalog.Backends, id); ok {
		return b, nil
	}
	return domain.Backend{}, fmt.Errorf("%w: %s", backendcatalog.ErrBackendNotFound, id)
}

// DeleteBackend removes a backend from the catalog.
func (m *Manager) DeleteBackend(id string) error {
	catalog, err := m.catalogStore.Load()
	if err != nil {
		return err
	}
	filtered := make([]domain.Backend, 0, len(catalog.Backends))
	for _, b := range catalog.Backends {
		if b.ID != id {
			filtered = append(filtered, b)
		}
	}
	if len(filtered) == len(catalog.Backends) {
		return fmt.Errorf("%w: %s", backendcatalog.ErrBackendNotFound, id)
	}
	catalog.Backends = filtered
	if catalog.DefaultBackendID == id {
		catalog.DefaultBackendID = ""
		if len(filtered) > 0 {
			catalog.DefaultBackendID = filtered[0].ID
		}
	}
	return m.catalogStore.Save(catalog)
}

func upsertBackend(backends []domain.Backend, b domain.Backend) []domain.Backend {
	for i, existing := range backends {
		if existing.ID == b.ID {
			backends[i] = b
			return backends
		}
	}
	return append(backends, b)
}

func findBackend(backends []domain.Backend, id string) (domain.Backend, bool) {
	for _, b := range backends {
		if b.ID == id {
			return b, true
		}
	}
	return domain.Backend{}, false
}

package backendschema

import (
	"context"
	"errors"
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

// AddBackend creates a new backend entry and persists the catalog.
// Schema generation is deferred; the caller should invoke GenerateSchema or
// RefreshSchema after the backend entry exists, or provide a hand-curated
// schema file directly in the schema store.
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

	if _, ok := findBackend(catalog.Backends, id); ok {
		return domain.Backend{}, fmt.Errorf("backend already exists: %s", id)
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
//
// Generators skip regeneration when the existing schema is marked
// source.customized=true, so incidental re-runs (catalog ensure paths,
// AddBackend retries) never clobber an operator's edits. RefreshSchema is the
// explicit user-driven path, so it deletes the existing schema first to force a
// fresh regeneration from the backend's --help output — and then restores the
// operator's layout on top when the previous schema was customized:
// Presentation and Rules are both reconciled against the regenerated flag set,
// so neither can reference a flag the backend no longer exposes.
//
// Scope boundary: a refresh re-derives flag *facts* from the backend, so
// per-flag constraint edits and operator-added flags are intentionally
// replaced. Only layout (presentation) and rules survive.
//
// A missing schema is not an error.
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

	ref := schemaStoreRef(backend.SchemaRef)
	prev, prevErr := m.schemaStore.Load(ref)
	if err := m.schemaStore.Delete(ref); err != nil && !errors.Is(err, backendcatalog.ErrSchemaNotFound) {
		return fmt.Errorf("delete schema: %w", err)
	}

	next, err := g.Generate(backend)
	if err != nil {
		return fmt.Errorf("generate schema: %w", err)
	}
	if prevErr != nil || !prev.Source.Customized {
		return nil
	}

	if prev.Presentation != nil {
		pres := ReconcilePresentation(*prev.Presentation, next)
		next.Presentation = &pres
	}
	next.Rules = ReconcileRules(prev.Rules, next)
	next.Source.Editable = true
	next.Source.Customized = true
	if err := m.schemaStore.Save(ref, next); err != nil {
		return fmt.Errorf("save schema: %w", err)
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

// DefaultBackendID returns the catalog's default backend ID, or empty string if none.
func (m *Manager) DefaultBackendID() (string, error) {
	catalog, err := m.catalogStore.Load()
	if err != nil {
		return "", err
	}
	return catalog.DefaultBackendID, nil
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

// UpdateBackend updates mutable fields of an existing backend.
// Preserves immutable fields: ID, Kind, SchemaRef, Meta.CreatedAt, Meta.GeneratedAt, Meta.SourceVersion.
func (m *Manager) UpdateBackend(id string, next domain.Backend) (domain.Backend, error) {
	catalog, err := m.catalogStore.Load()
	if err != nil {
		return domain.Backend{}, fmt.Errorf("load catalog: %w", err)
	}

	existing, ok := findBackend(catalog.Backends, id)
	if !ok {
		return domain.Backend{}, fmt.Errorf("%w: %s", backendcatalog.ErrBackendNotFound, id)
	}

	existing.Name = next.Name
	existing.Executable = next.Executable
	existing.Description = next.Description
	existing.Tags = next.Tags
	existing.Meta.UpdatedAt = time.Now().UTC()

	catalog.Backends = upsertBackend(catalog.Backends, existing)
	if err := m.catalogStore.Save(catalog); err != nil {
		return domain.Backend{}, fmt.Errorf("save catalog: %w", err)
	}
	return existing, nil
}

// SetDefaultBackend sets the default backend ID in the catalog.
func (m *Manager) SetDefaultBackend(id string) error {
	catalog, err := m.catalogStore.Load()
	if err != nil {
		return fmt.Errorf("load catalog: %w", err)
	}

	if _, ok := findBackend(catalog.Backends, id); !ok {
		return fmt.Errorf("%w: %s", backendcatalog.ErrBackendNotFound, id)
	}

	catalog.DefaultBackendID = id
	return m.catalogStore.Save(catalog)
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

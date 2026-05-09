package backendcatalog

import (
	"fmt"

	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/llamabin"
)

// Resolver resolves a profile to backend, executable path, and schema.
type Resolver interface {
	Resolve(profile domain.Profile) (ResolvedBackend, error)
}

// ResolvedBackend is a fully usable backend selection.
type ResolvedBackend struct {
	Backend        domain.Backend
	ExecutablePath string
	Schema         domain.BackendValidationSchema
}

type resolver struct {
	store       Store
	schemaStore SchemaStore
}

// NewResolver returns a catalog-backed backend resolver.
func NewResolver(store Store, schemaStore SchemaStore) *resolver {
	return &resolver{store: store, schemaStore: schemaStore}
}

// Resolve selects a backend, resolves its executable, and loads its schema.
func (r *resolver) Resolve(profile domain.Profile) (ResolvedBackend, error) {
	catalog, err := r.store.Load()
	if err != nil {
		return ResolvedBackend{}, err
	}

	backendID := profile.Launch.BackendID
	if backendID == "" {
		backendID = catalog.DefaultBackendID
	}
	if backendID == "" {
		return ResolvedBackend{}, ErrNoBackendSelected
	}

	backend, ok := findBackend(catalog.Backends, backendID)
	if !ok {
		return ResolvedBackend{}, fmt.Errorf("%w: %s", ErrBackendNotFound, backendID)
	}

	executablePath, err := llamabin.Resolve(backend.Executable)
	if err != nil {
		return ResolvedBackend{}, err
	}

	schema, err := r.schemaStore.Load(schemaStoreRef(backend.SchemaRef))
	if err != nil {
		return ResolvedBackend{}, err
	}

	return ResolvedBackend{
		Backend:        backend,
		ExecutablePath: executablePath,
		Schema:         schema,
	}, nil
}

func findBackend(backends []domain.Backend, id string) (domain.Backend, bool) {
	for _, backend := range backends {
		if backend.ID == id {
			return backend, true
		}
	}
	return domain.Backend{}, false
}

func schemaStoreRef(ref string) string {
	const prefix = "schemas/"
	if len(ref) >= len(prefix) && ref[:len(prefix)] == prefix {
		return ref[len(prefix):]
	}
	return ref
}

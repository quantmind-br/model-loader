package backendcatalog

import (
	"fmt"
	"log/slog"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/llamabin"
)

// Resolver resolves a profile to backend, executable path, and schema.
type Resolver interface {
	Resolve(profile domain.Profile) (ResolvedBackend, error)
	// ResolveSchema selects the backend and loads its schema WITHOUT requiring
	// the executable to be installed. Use it for validation/authoring flows; the
	// executable is only needed at launch time (Resolve).
	ResolveSchema(profile domain.Profile) (domain.BackendValidationSchema, domain.Backend, error)
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
	logger      *slog.Logger
}

// NewResolver returns a catalog-backed backend resolver. logger may be nil;
// nil → log.Nop() (no-op handler).
func NewResolver(store Store, schemaStore SchemaStore, logger *slog.Logger) *resolver {
	if logger == nil {
		logger = log.Nop()
	}
	return &resolver{store: store, schemaStore: schemaStore, logger: logger}
}

// Resolve selects a backend, resolves its executable, and loads its schema.
func (r *resolver) Resolve(profile domain.Profile) (ResolvedBackend, error) {
	schema, backend, err := r.loadBackendSchema(profile)
	if err != nil {
		return ResolvedBackend{}, err
	}

	executablePath, err := resolveExecutable(backend)
	if err != nil {
		r.logger.Error("resolve_failed",
			"step", "resolve_executable", "profile_id", profile.ID,
			"backend_id", backend.ID, "executable", backend.Executable, "err", err)
		return ResolvedBackend{}, err
	}

	return ResolvedBackend{
		Backend:        backend,
		ExecutablePath: executablePath,
		Schema:         schema,
	}, nil
}

// ResolveSchema selects the backend and loads its schema without resolving the
// executable, so authoring/validation works even when the runtime binary is not
// installed yet.
func (r *resolver) ResolveSchema(profile domain.Profile) (domain.BackendValidationSchema, domain.Backend, error) {
	return r.loadBackendSchema(profile)
}

// loadBackendSchema performs catalog→backend→schema resolution (with id/kind
// consistency checks) but does NOT resolve the executable.
func (r *resolver) loadBackendSchema(profile domain.Profile) (domain.BackendValidationSchema, domain.Backend, error) {
	catalog, err := r.store.Load()
	if err != nil {
		r.logger.Error("resolve_failed",
			"step", "load_catalog", "profile_id", profile.ID, "err", err)
		return domain.BackendValidationSchema{}, domain.Backend{}, err
	}

	backendID := profile.Launch.BackendID
	if backendID == "" {
		backendID = catalog.DefaultBackendID
	}
	if backendID == "" {
		r.logger.Error("resolve_failed",
			"step", "select_backend", "profile_id", profile.ID, "err", ErrNoBackendSelected)
		return domain.BackendValidationSchema{}, domain.Backend{}, ErrNoBackendSelected
	}

	backend, ok := findBackend(catalog.Backends, backendID)
	if !ok {
		err := fmt.Errorf("%w: %s", ErrBackendNotFound, backendID)
		r.logger.Error("resolve_failed",
			"step", "select_backend", "profile_id", profile.ID,
			"backend_id", backendID, "err", err)
		return domain.BackendValidationSchema{}, domain.Backend{}, err
	}

	schema, err := r.schemaStore.Load(schemaStoreRef(backend.SchemaRef))
	if err != nil {
		wrapped := fmt.Errorf("load schema: %w", err)
		r.logger.Error("resolve_failed",
			"step", "load_schema", "profile_id", profile.ID,
			"backend_id", backendID, "schema_ref", backend.SchemaRef, "err", err)
		return domain.BackendValidationSchema{}, domain.Backend{}, wrapped
	}
	if schema.BackendID != "" && schema.BackendID != backend.ID {
		err := fmt.Errorf("schema/backend mismatch: schema has backend_id=%q, expected %q", schema.BackendID, backend.ID)
		r.logger.Error("resolve_failed",
			"step", "load_schema", "profile_id", profile.ID, "err", err)
		return domain.BackendValidationSchema{}, domain.Backend{}, err
	}
	if backend.Kind != "" && schema.BackendKind != "" && schema.BackendKind != backend.Kind {
		err := fmt.Errorf("schema/backend kind mismatch: schema has kind=%q, expected %q", schema.BackendKind, backend.Kind)
		r.logger.Error("resolve_failed",
			"step", "load_schema", "profile_id", profile.ID, "err", err)
		return domain.BackendValidationSchema{}, domain.Backend{}, err
	}

	return schema, backend, nil
}

func findBackend(backends []domain.Backend, id string) (domain.Backend, bool) {
	for _, backend := range backends {
		if backend.ID == id {
			return backend, true
		}
	}
	return domain.Backend{}, false
}

// resolveExecutable resolves a backend's executable.
// For Python-based backends (SGLang and VLLM), it applies python↔python3
// fallback logic so compound commands like "python -m sglang.launch_server"
// are preserved when falling back to python3.
func resolveExecutable(backend domain.Backend) (string, error) {
	switch backend.Kind {
	case domain.BackendKindSGLang, domain.BackendKindVLLM:
		return llamabin.ResolveCommandWithPythonFallback(backend.Executable)
	default:
		return llamabin.Resolve(backend.Executable)
	}
}

func schemaStoreRef(ref string) string {
	const prefix = "schemas/"
	if len(ref) >= len(prefix) && ref[:len(prefix)] == prefix {
		return ref[len(prefix):]
	}
	return ref
}

// SchemaStoreRef strips the "schemas/" prefix from a SchemaRef if present.
// Exported so consumers (editor, tests) can normalize refs before calling
// SchemaStore.Load/Save, which expect paths relative to the schemas dir.
func SchemaStoreRef(ref string) string {
	return schemaStoreRef(ref)
}

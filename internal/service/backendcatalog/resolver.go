package backendcatalog

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/llamabin"
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
	catalog, err := r.store.Load()
	if err != nil {
		r.logger.Error("resolve_failed",
			"step", "load_catalog", "profile_id", profile.ID, "err", err)
		return ResolvedBackend{}, err
	}

	backendID := profile.Launch.BackendID
	if backendID == "" {
		backendID = catalog.DefaultBackendID
	}
	if backendID == "" {
		r.logger.Error("resolve_failed",
			"step", "select_backend", "profile_id", profile.ID, "err", ErrNoBackendSelected)
		return ResolvedBackend{}, ErrNoBackendSelected
	}

	backend, ok := findBackend(catalog.Backends, backendID)
	if !ok {
		err := fmt.Errorf("%w: %s", ErrBackendNotFound, backendID)
		r.logger.Error("resolve_failed",
			"step", "select_backend", "profile_id", profile.ID,
			"backend_id", backendID, "err", err)
		return ResolvedBackend{}, err
	}

	executablePath, err := resolveExecutable(backend)
	if err != nil {
		r.logger.Error("resolve_failed",
			"step", "resolve_executable", "profile_id", profile.ID,
			"backend_id", backendID, "executable", backend.Executable, "err", err)
		return ResolvedBackend{}, err
	}

	schema, err := r.schemaStore.Load(schemaStoreRef(backend.SchemaRef))
	if err != nil {
		wrapped := fmt.Errorf("load schema: %w", err)
		r.logger.Error("resolve_failed",
			"step", "load_schema", "profile_id", profile.ID,
			"backend_id", backendID, "schema_ref", backend.SchemaRef, "err", err)
		return ResolvedBackend{}, wrapped
	}
	if schema.BackendID != "" && schema.BackendID != backend.ID {
		err := fmt.Errorf("schema/backend mismatch: schema has backend_id=%q, expected %q", schema.BackendID, backend.ID)
		r.logger.Error("resolve_failed",
			"step", "load_schema", "profile_id", profile.ID, "err", err)
		return ResolvedBackend{}, err
	}
	if backend.Kind != "" && schema.BackendKind != "" && schema.BackendKind != backend.Kind {
		err := fmt.Errorf("schema/backend kind mismatch: schema has kind=%q, expected %q", schema.BackendKind, backend.Kind)
		r.logger.Error("resolve_failed",
			"step", "load_schema", "profile_id", profile.ID, "err", err)
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

// resolveExecutable resolves a backend's executable, applying SGLang-specific
// fallback logic (python ↔ python3) when the first token is not found.
// For compound commands (e.g. "python -m sglang.launch_server"), the fallback
// preserves the remaining tokens so the returned string is still a valid
// command line (e.g. "python3 -m sglang.launch_server").
func resolveExecutable(backend domain.Backend) (string, error) {
	path, err := llamabin.Resolve(backend.Executable)
	if err == nil {
		return path, nil
	}
	if backend.Kind == domain.BackendKindSGLang {
		first, rest := splitCommand(backend.Executable)
		if first == "" {
			return "", err
		}
		var fallback string
		switch first {
		case "python":
			fallback = "python3"
		case "python3":
			fallback = "python"
		default:
			return "", err
		}
		if fbPath, err2 := llamabin.Resolve(fallback); err2 == nil {
			if rest != "" {
				return fbPath + " " + rest, nil
			}
			return fbPath, nil
		}
	}
	return "", err
}

// splitCommand separates the first executable token from the rest of a
// command string using basic whitespace splitting.
func splitCommand(raw string) (string, string) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return "", ""
	}
	if len(fields) == 1 {
		return fields[0], ""
	}
	return fields[0], strings.Join(fields[1:], " ")
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

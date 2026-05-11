package backendschema

import (
	"fmt"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/llamabin"
	"github.com/quantmind-br/model-loader/internal/service/sglanghelp"
)

// SGLangGenerator generates schemas from the embedded sglang flag catalog.
// Unlike llama-server, sglang is a Python module whose --help output is not
// stable enough to parse at runtime, so we use a hand-curated embedded schema.
type SGLangGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

// NewSGLangGenerator returns a generator that writes schemas to store.
func NewSGLangGenerator(schemaStore backendcatalog.SchemaStore) *SGLangGenerator {
	return &SGLangGenerator{schemaStore: schemaStore}
}

// Generate creates a BackendValidationSchema from the embedded sglang catalog
// and persists it via schemaStore. If the existing schema has source.editable=true,
// generation is skipped to preserve manual edits.
func (g *SGLangGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindSGLang {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	// Validate that the executable (or its first token for command strings)
	// is resolvable, so typos are caught at add time rather than launch time.
	// For sglang we also try python3 as a fallback when python is not found,
	// since many Linux distributions ship only python3.
	if _, err := resolveSGLangExecutable(backend.Executable); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("validate executable: %w", err)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Editable {
		return existing, nil
	}

	fs := sglanghelp.EmbeddedSchema()

	src := domain.SchemaSource{
		GeneratedFrom: backend.Executable,
		GeneratedAt:   time.Now().UTC(),
		Editable:      true,
	}

	schema := domain.FlagSchemaToBackend(fs, backend.Kind, backend.ID, src)
	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

// resolveSGLangExecutable tries to resolve the first token of the command
// string. If it fails and the token is "python", it falls back to "python3".
// If the token is "python3", it falls back to "python". This handles systems
// where only one of the two is installed.
func resolveSGLangExecutable(cmd string) (string, error) {
	token, err := llamabin.Resolve(cmd)
	if err == nil {
		return token, nil
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return "", err
	}
	first := fields[0]
	var fallback string
	switch first {
	case "python":
		fallback = "python3"
	case "python3":
		fallback = "python"
	default:
		return "", err
	}
	if fb, err2 := llamabin.Resolve(fallback); err2 == nil {
		return fb, nil
	}
	return "", err
}

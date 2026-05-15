package backendschema

import (
	"fmt"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/llamabin"
	"github.com/quantmind-br/model-loader/internal/service/vllmhelp"
)

// VLLMGenerator generates schemas from the embedded vllm flag catalog.
// vLLM is a Python-based inference engine; we use a hand-curated embedded
// schema rather than parsing --help at runtime.
type VLLMGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

// NewVLLMGenerator returns a generator that writes schemas to store.
func NewVLLMGenerator(schemaStore backendcatalog.SchemaStore) *VLLMGenerator {
	return &VLLMGenerator{schemaStore: schemaStore}
}

// Generate creates a BackendValidationSchema from the embedded vllm catalog
// and persists it via schemaStore. If the existing schema has source.editable=true,
// generation is skipped to preserve manual edits.
func (g *VLLMGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindVLLM {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	// Validate that the executable (or its first token for command strings)
	// is resolvable, so typos are caught at add time rather than launch time.
	// For vLLM we also try python3 as a fallback when python is not found,
	// since many Linux distributions ship only python3.
	if _, err := resolveVLLMExecutable(backend.Executable); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("validate executable: %w", err)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Editable {
		return existing, nil
	}

	fs := vllmhelp.EmbeddedSchema()

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

// resolveVLLMExecutable tries to resolve the first token of the command
// string. If it fails and the token is "python", it falls back to "python3".
// If the token is "python3", it falls back to "python". This handles systems
// where only one of the two is installed.
func resolveVLLMExecutable(cmd string) (string, error) {
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

package backendschema

import (
	"context"
	"fmt"
	"time"

	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/llamabin"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/llamahelp"
)

// Generator generates a validation schema for a backend by inspecting its executable.
type Generator interface {
	Generate(backend domain.Backend) (domain.BackendValidationSchema, error)
}

// LlamaServerGenerator generates schemas from llama-server --help output.
type LlamaServerGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

// NewLlamaServerGenerator returns a generator that writes schemas to store.
func NewLlamaServerGenerator(schemaStore backendcatalog.SchemaStore) *LlamaServerGenerator {
	return &LlamaServerGenerator{schemaStore: schemaStore}
}

// Generate resolves the backend executable, runs --help, parses flags, and persists the schema.
func (g *LlamaServerGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindLlamaServer {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	resolved, err := llamabin.Resolve(backend.Executable)
	if err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("resolve executable: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	parser := llamahelp.NewExecParserFor(resolved)
	fs, err := parser.Parse(ctx)
	if err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("parse --help: %w", err)
	}

	src := domain.SchemaSource{
		GeneratedFrom: backend.Executable,
		GeneratedAt:   time.Now().UTC(),
		SourceVersion: fs.Version,
		Editable:      true,
	}

	schema := domain.FlagSchemaToBackend(fs, backend.Kind, backend.ID, src)
	if err := g.schemaStore.Save(backend.SchemaRef, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

// WriteEmbeddedFallback writes the embedded fallback schema to the store.
func WriteEmbeddedFallback(schemaStore backendcatalog.SchemaStore, backendID, schemaRef string) error {
	fs := llamahelp.EmbeddedSchema()
	src := domain.SchemaSource{
		GeneratedFrom: "embedded-fallback",
		GeneratedAt:   time.Now().UTC(),
		Editable:      true,
	}
	schema := domain.FlagSchemaToBackend(fs, domain.BackendKindLlamaServer, backendID, src)
	return schemaStore.Save(schemaRef, schema)
}

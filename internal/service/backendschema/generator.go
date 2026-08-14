package backendschema

import (
	"context"
	"fmt"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/llamabin"
	"github.com/quantmind-br/model-loader/internal/service/llamahelp"
)

func schemaStoreRef(ref string) string {
	const prefix = "schemas/"
	if len(ref) >= len(prefix) && ref[:len(prefix)] == prefix {
		return ref[len(prefix):]
	}
	return ref
}

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

func resolve(raw string) (string, error) {
	return llamabin.Resolve(raw)
}

// Generate returns the llama-server schema by first trying to parse --help from
// the resolved binary, falling back to the embedded golden JSON (246 flags), and
// then overlaying curated metadata (groups, descriptions, aliases). If the
// existing schema has source.customized=true, generation is skipped so an
// operator's edits survive incidental re-runs.
func (g *LlamaServerGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindLlamaServer {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Customized {
		return existing, nil
	}

	var full domain.BackendValidationSchema
	parsedLive := false
	if resolved, err := resolve(backend.Executable); err == nil {
		parsed, parseErr := parseHelpSchema(backend, resolved)
		if parseErr == nil {
			full = parsed
			parsedLive = true
		}
	}
	if full.Flags == nil {
		fs, loadErr := loadGoldenSchema()
		if loadErr != nil {
			return domain.BackendValidationSchema{}, fmt.Errorf("load golden schema: %w", loadErr)
		}
		src := domain.SchemaSource{
			GeneratedFrom: "embedded-golden",
			GeneratedAt:   time.Now().UTC(),
			Editable:      true,
		}
		full = domain.FlagSchemaToBackend(fs, backend.Kind, backend.ID, src)
	}

	// A live parse is authoritative on flag existence; enrich only. The pinned
	// golden fallback may trail the curated overlay, so it appends missing flags.
	var schema domain.BackendValidationSchema
	if parsedLive {
		schema = mergeWithCuratedEnrich(full, CuratedLlamaSchema())
	} else {
		schema = mergeWithCurated(full, CuratedLlamaSchema())
	}
	schema.BackendID = backend.ID
	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

// parseHelpSchema runs --help on a resolved binary and converts the parsed
// flags into a backend validation schema. Shared by the llama-server and
// buun-llama-cpp generators (the fork's --help is format-identical).
func parseHelpSchema(backend domain.Backend, resolved string) (domain.BackendValidationSchema, error) {
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
	return domain.FlagSchemaToBackend(fs, backend.Kind, backend.ID, src), nil
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
	return schemaStore.Save(schemaStoreRef(schemaRef), schema)
}

package backendschema

import (
	"fmt"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/buunhelp"
	"github.com/quantmind-br/model-loader/internal/service/llamabin"
)

// BuunServerGenerator generates schemas for the buun-llama-cpp fork. The fork's
// `--help` is format-identical to upstream llama.cpp, so the live schema is
// parsed at runtime via the shared parseHelpSchema helper. When the binary
// cannot be resolved or parsed, it falls back to the curated embedded schema.
type BuunServerGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

// NewBuunServerGenerator returns a generator that writes schemas to store.
func NewBuunServerGenerator(schemaStore backendcatalog.SchemaStore) *BuunServerGenerator {
	return &BuunServerGenerator{schemaStore: schemaStore}
}

// Generate parses --help when possible, else writes the embedded fallback.
func (g *BuunServerGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindBuunLlamaCpp {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Editable {
		return existing, nil
	}

	schema := g.resolveSchema(backend)

	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

// resolveSchema tries runtime --help parsing and falls back to the embedded
// curated schema on any resolve/parse failure.
func (g *BuunServerGenerator) resolveSchema(backend domain.Backend) domain.BackendValidationSchema {
	if resolved, err := llamabin.Resolve(backend.Executable); err == nil {
		if parsed, perr := parseHelpSchema(backend, resolved); perr == nil {
			return parsed
		}
	}
	return buunFallbackSchema(backend)
}

func buunFallbackSchema(backend domain.Backend) domain.BackendValidationSchema {
	fs := buunhelp.EmbeddedSchema()
	src := domain.SchemaSource{
		GeneratedFrom: "embedded-fallback",
		GeneratedAt:   time.Now().UTC(),
		SourceVersion: fs.Version,
		Editable:      true,
	}
	return domain.FlagSchemaToBackend(fs, backend.Kind, backend.ID, src)
}

package backendschema

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

// BeeLlamaServerGenerator generates the schema for the BeeLlama.cpp backend
// (Anbeeld's llama.cpp fork). The fork's --help is format-identical to upstream,
// so the live schema is parsed from the binary and the curated BeeLlama metadata
// (groups, DFlash/TurboQuant flags, presentation, cross-field rules) is overlaid.
// When the binary cannot be resolved, the fully curated schema is the fallback.
type BeeLlamaServerGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

func NewBeeLlamaServerGenerator(schemaStore backendcatalog.SchemaStore) *BeeLlamaServerGenerator {
	return &BeeLlamaServerGenerator{schemaStore: schemaStore}
}

func (g *BeeLlamaServerGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindBeeLlamaCpp {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Customized {
		return existing, nil
	}

	var schema domain.BackendValidationSchema
	if resolved, err := resolve(backend.Executable); err == nil {
		parsed, parseErr := parseHelpSchema(backend, resolved)
		if parseErr == nil {
			schema = mergeWithCurated(parsed, CuratedBeeLlamaSchema())
		}
	}
	if schema.Flags == nil {
		schema = CuratedBeeLlamaSchema()
	}

	schema.BackendID = backend.ID
	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

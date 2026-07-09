package backendschema

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

// IkLlamaServerGenerator generates schemas from the hand-curated ik-llama-cpp
// flag catalog. ik_llama.cpp's --help uses the pre-arg.cpp format (2-space
// indent, `group:` headers) that llamahelp cannot parse, so the curated schema
// is the only flag source; --help is never executed.
type IkLlamaServerGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

// NewIkLlamaServerGenerator returns a generator that writes schemas to store.
func NewIkLlamaServerGenerator(schemaStore backendcatalog.SchemaStore) *IkLlamaServerGenerator {
	return &IkLlamaServerGenerator{schemaStore: schemaStore}
}

// Generate returns the hand-curated ik-llama-cpp schema without runtime --help
// parsing. If the existing schema has source.editable=true, generation is
// skipped to preserve manual edits.
func (g *IkLlamaServerGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindIkLlamaCpp {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Editable {
		return existing, nil
	}

	schema := CuratedIkLlamaSchema()
	schema.BackendID = backend.ID
	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

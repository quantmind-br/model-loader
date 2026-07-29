package backendschema

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

// VLLMGenerator generates schemas from the hand-curated vLLM flag catalog.
type VLLMGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

// NewVLLMGenerator returns a generator that writes schemas to store.
func NewVLLMGenerator(schemaStore backendcatalog.SchemaStore) *VLLMGenerator {
	return &VLLMGenerator{schemaStore: schemaStore}
}

// Generate returns the hand-curated vLLM schema without runtime --help parsing.
// If the existing schema has source.customized=true, generation is skipped so an
// operator's edits survive incidental re-runs.
func (g *VLLMGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindVLLM {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Customized {
		return existing, nil
	}

	schema := CuratedVLLMSchema()
	schema.BackendID = backend.ID
	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

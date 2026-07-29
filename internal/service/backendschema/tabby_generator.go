package backendschema

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/tabbyhelp"
)

// TabbyGenerator generates schemas from the hand-curated TabbyAPI flag catalog.
type TabbyGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

// NewTabbyGenerator returns a generator that writes schemas to store.
func NewTabbyGenerator(schemaStore backendcatalog.SchemaStore) *TabbyGenerator {
	return &TabbyGenerator{schemaStore: schemaStore}
}

// Generate returns the curated tabby schema without runtime --help parsing.
// If the existing schema has source.customized=true, generation is skipped so
// an operator's edits (web Customize mode) survive incidental re-runs.
func (g *TabbyGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindTabby {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Customized {
		return existing, nil
	}

	schema := domain.FlagSchemaToBackend(
		tabbyhelp.EmbeddedSchema(),
		domain.BackendKindTabby,
		backend.ID,
		domain.SchemaSource{GeneratedFrom: backend.Executable},
	)
	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

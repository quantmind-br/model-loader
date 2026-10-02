package backendschema

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/stratahelp"
)

// StrataGenerator generates schemas from the hand-curated Strata flag
// catalog.
type StrataGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

// NewStrataGenerator returns a generator that writes schemas to store.
func NewStrataGenerator(schemaStore backendcatalog.SchemaStore) *StrataGenerator {
	return &StrataGenerator{schemaStore: schemaStore}
}

// Generate returns the curated strata schema without runtime --help parsing.
// If the existing schema has source.customized=true, generation is skipped so
// an operator's edits (web Customize mode) survive incidental re-runs.
func (g *StrataGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindStrata {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Customized {
		return existing, nil
	}

	schema := domain.FlagSchemaToBackend(
		stratahelp.EmbeddedSchema(),
		domain.BackendKindStrata,
		backend.ID,
		domain.SchemaSource{GeneratedFrom: backend.Executable},
	)
	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

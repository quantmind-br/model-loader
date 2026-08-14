package backendschema

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/tokenspeedhelp"
)

// TokenSpeedGenerator generates schemas from the hand-curated TokenSpeed flag catalog.
type TokenSpeedGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

// NewTokenSpeedGenerator returns a generator that writes schemas to store.
func NewTokenSpeedGenerator(schemaStore backendcatalog.SchemaStore) *TokenSpeedGenerator {
	return &TokenSpeedGenerator{schemaStore: schemaStore}
}

// Generate returns the curated TokenSpeed schema without runtime --help parsing.
// If the existing schema has source.customized=true, generation is skipped so
// an operator's edits survive incidental re-runs.
func (g *TokenSpeedGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindTokenSpeed {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Customized {
		return existing, nil
	}

	schema := domain.FlagSchemaToBackend(
		tokenspeedhelp.EmbeddedSchema(),
		domain.BackendKindTokenSpeed,
		backend.ID,
		domain.SchemaSource{GeneratedFrom: backend.Executable},
	)
	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

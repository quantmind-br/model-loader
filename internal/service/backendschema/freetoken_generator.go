package backendschema

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/freetokenhelp"
)

// FreeTokenGenerator generates schemas from the hand-curated FreeToken flag
// catalog.
type FreeTokenGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

// NewFreeTokenGenerator returns a generator that writes schemas to store.
func NewFreeTokenGenerator(schemaStore backendcatalog.SchemaStore) *FreeTokenGenerator {
	return &FreeTokenGenerator{schemaStore: schemaStore}
}

// Generate returns the curated freetoken schema without runtime --help parsing.
// If the existing schema has source.customized=true, generation is skipped so
// an operator's edits (web Customize mode) survive incidental re-runs.
func (g *FreeTokenGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindFreeToken {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Customized {
		return existing, nil
	}

	schema := domain.FlagSchemaToBackend(
		freetokenhelp.EmbeddedSchema(),
		domain.BackendKindFreeToken,
		backend.ID,
		domain.SchemaSource{GeneratedFrom: backend.Executable},
	)
	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

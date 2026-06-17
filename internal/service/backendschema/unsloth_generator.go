package backendschema

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/unslothhelp"
)

// UnslothGenerator generates schemas from the hand-curated unsloth flag catalog.
type UnslothGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

// NewUnslothGenerator returns a generator that writes schemas to store.
func NewUnslothGenerator(schemaStore backendcatalog.SchemaStore) *UnslothGenerator {
	return &UnslothGenerator{schemaStore: schemaStore}
}

// Generate returns the curated unsloth schema without runtime --help parsing.
// If the existing schema has source.editable=true, generation is skipped to
// preserve manual edits (web Customize mode).
func (g *UnslothGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindUnsloth {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Editable {
		return existing, nil
	}

	schema := domain.FlagSchemaToBackend(
		unslothhelp.EmbeddedSchema(),
		domain.BackendKindUnsloth,
		backend.ID,
		domain.SchemaSource{GeneratedFrom: backend.Executable},
	)
	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

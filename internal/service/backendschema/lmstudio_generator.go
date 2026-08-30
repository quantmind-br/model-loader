package backendschema

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/lmstudiohelp"
)

// LMStudioGenerator generates schemas from the hand-curated LM Studio flag
// catalog (the lms CLI is an RPC client with no parseable server --help).
type LMStudioGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

// NewLMStudioGenerator returns a generator that writes schemas to store.
func NewLMStudioGenerator(schemaStore backendcatalog.SchemaStore) *LMStudioGenerator {
	return &LMStudioGenerator{schemaStore: schemaStore}
}

// Generate returns the curated lmstudio schema without runtime --help parsing.
// If the existing schema has source.customized=true, generation is skipped so
// an operator's edits (web Customize mode) survive incidental re-runs.
func (g *LMStudioGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindLMStudio {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Customized {
		return existing, nil
	}

	schema := domain.FlagSchemaToBackend(
		lmstudiohelp.EmbeddedSchema(),
		domain.BackendKindLMStudio,
		backend.ID,
		domain.SchemaSource{GeneratedFrom: backend.Executable},
	)
	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

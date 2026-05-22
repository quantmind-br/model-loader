package backendschema

import (
	"fmt"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

// embeddedGenerator generates schemas from an embedded flag catalog.
// It is shared by SGLang and VLLM generators.
type embeddedGenerator struct {
	kind        domain.BackendKind
	schemaFn    func() domain.FlagSchema
	schemaStore backendcatalog.SchemaStore
	resolveFn   func(string) (string, error)
}

// Generate creates a BackendValidationSchema from the embedded catalog
// and persists it via schemaStore. If the existing schema has source.editable=true,
// generation is skipped to preserve manual edits.
func (g *embeddedGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != g.kind {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Editable {
		return existing, nil
	}

	fs := g.schemaFn()

	src := domain.SchemaSource{
		GeneratedFrom: backend.Executable,
		GeneratedAt:   time.Now().UTC(),
		Editable:      true,
	}

	schema := domain.FlagSchemaToBackend(fs, backend.Kind, backend.ID, src)
	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

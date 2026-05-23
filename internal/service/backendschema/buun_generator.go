package backendschema

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

type BuunServerGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

func NewBuunServerGenerator(schemaStore backendcatalog.SchemaStore) *BuunServerGenerator {
	return &BuunServerGenerator{schemaStore: schemaStore}
}

func (g *BuunServerGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindBuunLlamaCpp {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Editable {
		return existing, nil
	}

	var schema domain.BackendValidationSchema
	if resolved, err := resolve(backend.Executable); err == nil {
		parsed, parseErr := parseHelpSchema(backend, resolved)
		if parseErr == nil {
			schema = mergeWithCurated(parsed, CuratedBuunSchema())
		}
	}
	if schema.Flags == nil {
		schema = CuratedBuunSchema()
	}

	schema.BackendID = backend.ID
	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

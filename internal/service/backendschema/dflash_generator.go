package backendschema

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/dflashhelp"
	"github.com/quantmind-br/model-loader/internal/service/llamabin"
)

// DFlashGenerator generates schemas from the embedded DFlash flag catalog.
// DFlash is the native lucebox-hub dflash_server binary; its flags are
// hand-curated in dflashhelp rather than parsed from --help output.
type DFlashGenerator struct {
	*embeddedGenerator
}

// NewDFlashGenerator returns a generator that writes schemas to store.
func NewDFlashGenerator(schemaStore backendcatalog.SchemaStore) *DFlashGenerator {
	return &DFlashGenerator{
		embeddedGenerator: &embeddedGenerator{
			kind:        domain.BackendKindDFlash,
			schemaFn:    dflashhelp.EmbeddedSchema,
			schemaStore: schemaStore,
			resolveFn:   llamabin.Resolve,
		},
	}
}

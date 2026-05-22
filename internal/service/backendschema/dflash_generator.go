package backendschema

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/dflashhelp"
	"github.com/quantmind-br/model-loader/internal/service/llamabin"
)

// DFlashGenerator generates schemas from the embedded DFlash flag catalog.
// DFlash is launched through a wrapper script (run-server.sh) around
// scripts/server.py, so its flags are hand-curated rather than parsed.
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

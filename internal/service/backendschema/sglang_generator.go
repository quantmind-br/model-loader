package backendschema

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/llamabin"
	"github.com/quantmind-br/model-loader/internal/service/sglanghelp"
)

// SGLangGenerator generates schemas from the embedded sglang flag catalog.
// Unlike llama-server, sglang is a Python module whose --help output is not
// stable enough to parse at runtime, so we use a hand-curated embedded schema.
type SGLangGenerator struct {
	*embeddedGenerator
}

// NewSGLangGenerator returns a generator that writes schemas to store.
func NewSGLangGenerator(schemaStore backendcatalog.SchemaStore) *SGLangGenerator {
	return &SGLangGenerator{
		embeddedGenerator: &embeddedGenerator{
			kind:        domain.BackendKindSGLang,
			schemaFn:    sglanghelp.EmbeddedSchema,
			schemaStore: schemaStore,
			resolveFn:   llamabin.ResolveCommandWithPythonFallback,
		},
	}
}

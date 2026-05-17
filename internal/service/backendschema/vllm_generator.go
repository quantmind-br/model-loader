package backendschema

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/llamabin"
	"github.com/quantmind-br/model-loader/internal/service/vllmhelp"
)

// VLLMGenerator generates schemas from the embedded vllm flag catalog.
// vLLM is a Python-based inference engine; we use a hand-curated embedded
// schema rather than parsing --help at runtime.
type VLLMGenerator struct {
	*embeddedGenerator
}

// NewVLLMGenerator returns a generator that writes schemas to store.
func NewVLLMGenerator(schemaStore backendcatalog.SchemaStore) *VLLMGenerator {
	return &VLLMGenerator{
		embeddedGenerator: &embeddedGenerator{
			kind:        domain.BackendKindVLLM,
			schemaFn:    vllmhelp.EmbeddedSchema,
			schemaStore: schemaStore,
			resolveFn:   llamabin.ResolveCommandWithPythonFallback,
		},
	}
}

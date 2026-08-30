package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/dflashhelp"
	"github.com/quantmind-br/model-loader/internal/service/lmstudiohelp"
	"github.com/quantmind-br/model-loader/internal/service/tabbyhelp"
	"github.com/quantmind-br/model-loader/internal/service/unslothhelp"
)

// Curated presentations are literal flag lists, so unlike the essentialSeed
// path in BuildPresentation they are not filtered against the schema. A flag
// renamed or dropped from a curated file therefore leaves a dangling entry
// that renders as an empty row in the Backends tab instead of failing loudly.
func TestCuratedPresentations_ReferenceOnlyExistingFlags(t *testing.T) {
	for _, tt := range []struct {
		name   string
		schema domain.BackendValidationSchema
	}{
		{"llama-server", CuratedLlamaSchema()},
		{"beellama-cpp", CuratedBeeLlamaSchema()},
		{"buun-llama-cpp", CuratedBuunSchema()},
		{"vllm", CuratedVLLMSchema()},
		{"sglang", CuratedSGLangSchema()},
		{"ik-llama-cpp", CuratedIkLlamaSchema()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.schema.Presentation == nil {
				t.Fatal("curated schema has no presentation")
			}
			for _, g := range tt.schema.Presentation.Groups {
				for _, long := range g.Flags {
					if _, ok := tt.schema.Flags[long]; !ok {
						t.Errorf("group %q references flag %q that the schema does not define", g.Name, long)
					}
				}
			}
		})
	}
}

// essentialSeed drives the Pattern C kinds, whose schemas come from the
// embedded *help packages. BuildPresentation silently skips a seed flag the
// schema lacks, so a typo there is invisible at runtime.
func TestEssentialSeed_ReferencesOnlyExistingFlags(t *testing.T) {
	for _, tt := range []struct {
		kind   domain.BackendKind
		schema domain.FlagSchema
	}{
		{domain.BackendKindDFlash, dflashhelp.EmbeddedSchema()},
		{domain.BackendKindUnsloth, unslothhelp.EmbeddedSchema()},
		{domain.BackendKindTabby, tabbyhelp.EmbeddedSchema()},
		{domain.BackendKindLMStudio, lmstudiohelp.EmbeddedSchema()},
	} {
		t.Run(string(tt.kind), func(t *testing.T) {
			for _, long := range essentialSeed[tt.kind] {
				if _, ok := tt.schema.Flags[long]; !ok {
					t.Errorf("essentialSeed[%s] names flag %q that the embedded schema does not define", tt.kind, long)
				}
			}
		})
	}
}

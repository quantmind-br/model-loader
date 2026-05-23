package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/validator"
)

// TestCuratedLlama_ValidatesMTPProfile guards that the curated llama-server
// schema covers the flags a real MTP profile uses. These are all standard
// llama-server flags; an earlier curation gap left 7 of them out and typed
// n-gpu-layers as string, so numeric profiles failed validation with 8 errors.
func TestCuratedLlama_ValidatesMTPProfile(t *testing.T) {
	schema := CuratedLlamaSchema().ToFlagSchema()

	p := domain.Profile{
		Args: map[string]any{
			"batch-size":       float64(2048),
			"cache-type-k":     "q4_0",
			"cache-type-v":     "q4_0",
			"cpu-mask":         "0x00000003",
			"cpu-strict":       "1",
			"ctx-size":         float64(200000),
			"flash-attn":       "on",
			"host":             "127.0.0.1",
			"jinja":            true,
			"n-gpu-layers":     float64(99),
			"parallel":         float64(1),
			"port":             float64(4330),
			"reasoning-budget": float64(2048),
			"spec-draft-n-max": float64(3),
			"spec-type":        "draft-mtp",
			"threads":          float64(2),
			"threads-batch":    float64(8),
			"ubatch-size":      float64(1024),
		},
		ExtraArgs: []string{
			"--ctx-checkpoints", "8",
			"--spec-type", "draft-mtp",
			"--no-mmap",
			"--no-webui",
			"--no-perf",
		},
	}

	rep := validator.New(nil).Validate(p, schema, schema.BackendKind)
	if rep.HasBlockingErrors() {
		t.Fatalf("expected curated schema to validate MTP profile, got %d errors: %+v", len(rep.Errors), rep.Errors)
	}
}

// TestCuratedLlama_NGPULayersIsInt pins n-gpu-layers to int so numeric profile
// values validate. All real profiles store it as a number.
func TestCuratedLlama_NGPULayersIsInt(t *testing.T) {
	schema := CuratedLlamaSchema().ToFlagSchema()
	spec, ok := schema.Lookup("n-gpu-layers")
	if !ok {
		t.Fatal("n-gpu-layers missing from curated schema")
	}
	if spec.Type != domain.FlagTypeInt {
		t.Fatalf("n-gpu-layers should be FlagTypeInt, got %v", spec.Type)
	}
}

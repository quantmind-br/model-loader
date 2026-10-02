package stratahelp

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/validator"
)

func TestManagedProfileValidation(t *testing.T) {
	schema := EmbeddedSchema()
	if !schema.Flags["port"].IsPort || schema.Version != "embedded-strata-v1" {
		t.Fatal("missing port ownership or schema provenance")
	}
	for _, tc := range []struct {
		name      string
		args      map[string]any
		wantError bool
	}{
		{"valid", map[string]any{"config": "strata.json", "max-context": 32768, "gpu": "0,1"}, false},
		{"config required", map[string]any{"max-context": 32768}, true},
		{"positive context", map[string]any{"config": "strata.json", "max-context": 0}, true},
		{"native flags belong in config", map[string]any{"config": "strata.json", "pack": "pack"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{Model: t.TempDir(), Args: tc.args}
			rep := validator.New(log.Nop()).Validate(p, schema, domain.BackendKindStrata)
			if (len(rep.Errors) > 0) != tc.wantError {
				t.Fatalf("errors = %v, wantError = %v", rep.Errors, tc.wantError)
			}
		})
	}
}

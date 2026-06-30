package tabbyhelp

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestEmbeddedSchema(t *testing.T) {
	fs := EmbeddedSchema()
	if fs.Version != "embedded-tabby-v1" {
		t.Fatalf("version = %q", fs.Version)
	}

	// Spot-check the load-bearing flags exist with the right types.
	want := map[string]domain.FlagType{
		"max-seq-len":     domain.FlagTypeInt,
		"cache-mode":      domain.FlagTypeString,
		"tensor-parallel": domain.FlagTypeBool,
		"gpu-split":       domain.FlagTypeString,
		"backend":         domain.FlagTypeEnum,
		"draft-mode":      domain.FlagTypeEnum,
	}
	for long, typ := range want {
		spec, ok := fs.Flags[long]
		if !ok {
			t.Errorf("missing flag %q", long)
			continue
		}
		if spec.Type != typ {
			t.Errorf("flag %q type = %v, want %v", long, spec.Type, typ)
		}
	}

	// port must be flagged manager-owned.
	port, ok := fs.Flags["port"]
	if !ok || !port.IsPort {
		t.Fatalf("port flag missing or not IsPort: %+v", port)
	}

	// model is emitted from Profile.Model (as --model-dir/--model-name), never a flag row.
	if _, ok := fs.Flags["model"]; ok {
		t.Error("model must not be a schema flag")
	}

	// backend enum must offer exactly the two engines.
	be := fs.Flags["backend"]
	if len(be.EnumValues) != 2 {
		t.Fatalf("backend enum = %v", be.EnumValues)
	}
}

package unslothhelp

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestEmbeddedSchema_HasKeyFlagsAndNoManagedFlags(t *testing.T) {
	fs := EmbeddedSchema()
	if fs.Version != "embedded-unsloth-v1" {
		t.Fatalf("version = %q", fs.Version)
	}
	for _, want := range []string{"gguf-variant", "ctx-size", "n-gpu-layers", "parallel", "port"} {
		if _, ok := fs.Flags[want]; !ok {
			t.Errorf("missing expected flag %q", want)
		}
	}
	if spec, ok := fs.Flags["port"]; !ok || !spec.IsPort {
		t.Errorf("port must exist with IsPort=true")
	}
	// Studio-managed flags must NOT be user-editable schema rows.
	for _, banned := range []string{"host", "model", "api-key", "hf-repo"} {
		if _, ok := fs.Flags[banned]; ok {
			t.Errorf("Studio-managed flag %q must not be in the schema", banned)
		}
	}
	_ = domain.FlagTypeString
}

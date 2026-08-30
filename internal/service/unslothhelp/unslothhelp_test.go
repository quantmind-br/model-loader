package unslothhelp

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestEmbeddedSchema_HasKeyFlagsAndNoManagedFlags(t *testing.T) {
	fs := EmbeddedSchema()
	if fs.Version != "embedded-unsloth-v4" {
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
	// The wrapper already forces silent/yes/no-cloudflare/host.
	for _, banned := range []string{"host", "model", "api-key", "hf-repo", "silent", "yes", "cloudflare"} {
		if _, ok := fs.Flags[banned]; ok {
			t.Errorf("Studio-managed flag %q must not be in the schema", banned)
		}
	}
	// Unsloth re-parses the -ngl pass-through with int(), rejecting the
	// llama-server keywords auto/all and anything below -1 with HTTP 400.
	ngl, ok := fs.Flags["n-gpu-layers"]
	if !ok {
		t.Fatal("missing n-gpu-layers")
	}
	if len(ngl.Keywords) != 0 {
		t.Errorf("n-gpu-layers must not advertise keywords, got %v", ngl.Keywords)
	}
	if ngl.Min == nil || *ngl.Min != -1 {
		t.Errorf("n-gpu-layers min = %v, want -1", ngl.Min)
	}
	_ = domain.FlagTypeString
}

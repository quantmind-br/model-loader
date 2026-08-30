package lmstudiohelp

import "testing"

func TestEmbeddedSchema_HasKeyFlagsAndNoManagedFlags(t *testing.T) {
	fs := EmbeddedSchema()
	if fs.Version != "embedded-lmstudio-v1" {
		t.Fatalf("version = %q", fs.Version)
	}
	for _, want := range []string{"gpu", "context-length", "parallel", "ttl", "identifier", "port"} {
		if _, ok := fs.Flags[want]; !ok {
			t.Errorf("missing expected flag %q", want)
		}
	}
	if spec, ok := fs.Flags["port"]; !ok || !spec.IsPort {
		t.Errorf("port must exist with IsPort=true")
	}
	// The model key is passed positionally to `lms load` from the profile's
	// Model field, and the wrapper forces the non-interactive flags, so none
	// of these may be user-editable rows.
	for _, banned := range []string{"model", "yes", "exact", "local", "estimate-only"} {
		if _, ok := fs.Flags[banned]; ok {
			t.Errorf("wrapper-owned flag %q must not be in the schema", banned)
		}
	}
	// LM Studio auto-determines the offload ratio when --gpu is omitted, so a
	// materialized default would silently defeat auto-fit.
	gpu, ok := fs.Flags["gpu"]
	if !ok {
		t.Fatal("missing gpu")
	}
	if gpu.Default != nil {
		t.Errorf("gpu must carry no default, got %v", gpu.Default)
	}
	if len(gpu.Keywords) != 2 {
		t.Errorf("gpu keywords = %v, want off/max", gpu.Keywords)
	}
}

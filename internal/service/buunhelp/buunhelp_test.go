package buunhelp

import "testing"

func TestEmbeddedSchema_HasForkFlags(t *testing.T) {
	s := EmbeddedSchema()
	for _, name := range []string{
		"cache-type-k", "cache-type-v", "spec-draft-model",
		"spec-dflash-default", "dflash-max-slots", "spec-type",
		"draft-max", "draft-min",
		"port", // added by buunRows, not upstream base
	} {
		if _, ok := s.Lookup(name); !ok {
			t.Errorf("fork flag %q missing from embedded schema", name)
		}
	}

	// cache-type-k exists in both base and fork rows; the fork (turbo) version must win.
	if spec, ok := s.Lookup("cache-type-k"); !ok {
		t.Fatal("cache-type-k missing")
	} else {
		found := false
		for _, v := range spec.EnumValues {
			if v == "turbo4" {
				found = true
			}
		}
		if !found {
			t.Error("cache-type-k enum lost turbo types after merge (base did not get overridden)")
		}
	}
}

func TestEmbeddedSchema_HasBaseLlamaFlags(t *testing.T) {
	s := EmbeddedSchema()
	for _, name := range []string{"n-gpu-layers", "ctx-size", "flash-attn"} {
		if _, ok := s.Lookup(name); !ok {
			t.Errorf("base llama flag %q missing from embedded schema", name)
		}
	}
}

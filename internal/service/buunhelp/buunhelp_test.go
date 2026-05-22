package buunhelp

import "testing"

func TestEmbeddedSchema_HasForkFlags(t *testing.T) {
	s := EmbeddedSchema()
	for _, name := range []string{
		"cache-type-k", "cache-type-v", "spec-draft-model",
		"spec-dflash-default", "dflash-max-slots", "spec-type",
		"draft-max", "draft-min",
	} {
		if _, ok := s.Lookup(name); !ok {
			t.Errorf("fork flag %q missing from embedded schema", name)
		}
	}
}

func TestEmbeddedSchema_HasBaseLlamaFlags(t *testing.T) {
	s := EmbeddedSchema()
	for _, name := range []string{"n-gpu-layers", "ctx-size", "flash-attn", "port"} {
		if _, ok := s.Lookup(name); !ok {
			t.Errorf("base llama flag %q missing from embedded schema", name)
		}
	}
}

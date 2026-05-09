package llamahelp

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestEmbedded_HasAllEssentials(t *testing.T) {
	schema := EmbeddedSchema()
	want := []string{
		"model", "n-gpu-layers", "ctx-size", "batch-size", "ubatch-size",
		"flash-attn", "threads", "parallel", "mlock",
		"cache-type-k", "cache-type-v", "split-mode", "tensor-split",
	}
	for _, name := range want {
		if _, ok := schema.Lookup(name); !ok {
			t.Errorf("embedded missing essential flag %q", name)
		}
	}
	if schema.Version == "" {
		t.Error("embedded schema must report a version label")
	}
}

func TestEmbedded_FlashAttnEnumValues(t *testing.T) {
	schema := EmbeddedSchema()
	spec, ok := schema.Lookup("flash-attn")
	if !ok {
		t.Fatal("flash-attn missing")
	}
	if spec.Type != domain.FlagTypeEnum {
		t.Errorf("flash-attn type = %v, want enum", spec.Type)
	}
	wantSet := map[string]bool{"on": false, "off": false, "auto": false}
	for _, v := range spec.EnumValues {
		wantSet[v] = true
	}
	for v, found := range wantSet {
		if !found {
			t.Errorf("flash-attn enum missing %q", v)
		}
	}
}

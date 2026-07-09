package backendschema

import (
	"slices"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestCuratedIkLlama_KindAndPresentation(t *testing.T) {
	schema := CuratedIkLlamaSchema()
	if schema.BackendKind != domain.BackendKindIkLlamaCpp {
		t.Fatalf("BackendKind = %q, want %q", schema.BackendKind, domain.BackendKindIkLlamaCpp)
	}
	if schema.Presentation == nil {
		t.Fatal("Presentation is nil")
	}
	if len(schema.Presentation.Groups) == 0 {
		t.Fatal("Presentation has no groups")
	}
	first := schema.Presentation.Groups[0]
	if first.Name != ikGroupEssentials {
		t.Fatalf("first group = %q, want %q", first.Name, ikGroupEssentials)
	}
	if !first.Highlighted {
		t.Fatal("Essentials group must be Highlighted")
	}
	for _, g := range schema.Presentation.Groups {
		for _, name := range g.Flags {
			if _, ok := schema.Flags[name]; !ok {
				t.Errorf("presentation group %q references missing flag %q", g.Name, name)
			}
		}
	}
}

func TestCuratedIkLlama_NegativeOnlyFlags(t *testing.T) {
	schema := CuratedIkLlamaSchema()
	required := []string{"no-mmap", "no-fused-moe", "no-fused-up-gate", "no-fused-mul-multiadd", "no-kv-offload"}
	for _, name := range required {
		if _, ok := schema.Flags[name]; !ok {
			t.Errorf("Flags missing required negative-only flag %q", name)
		}
	}
	forbidden := []string{"mmap", "fused-moe", "kv-offload", "temperature", "typical-p"}
	for _, name := range forbidden {
		if _, ok := schema.Flags[name]; ok {
			t.Errorf("Flags must not contain rejected long name %q", name)
		}
	}
}

func TestCuratedIkLlama_SplitModeAndCacheTypes(t *testing.T) {
	schema := CuratedIkLlamaSchema()
	split, ok := schema.Flags["split-mode"]
	if !ok {
		t.Fatal("split-mode missing")
	}
	wantSplit := []string{"none", "graph", "layer"}
	if !slices.Equal(split.EnumValues, wantSplit) {
		t.Fatalf("split-mode enum = %v, want %v", split.EnumValues, wantSplit)
	}
	cacheK, ok := schema.Flags["cache-type-k"]
	if !ok {
		t.Fatal("cache-type-k missing")
	}
	if !slices.Contains(cacheK.EnumValues, "q6_0") {
		t.Fatalf("cache-type-k enum missing q6_0: %v", cacheK.EnumValues)
	}
	if !slices.Contains(cacheK.EnumValues, "q8_KV") {
		t.Fatalf("cache-type-k enum missing q8_KV: %v", cacheK.EnumValues)
	}
}

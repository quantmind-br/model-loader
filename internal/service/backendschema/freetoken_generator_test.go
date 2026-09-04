package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestFreeTokenGenerator_WrongKind(t *testing.T) {
	g := NewFreeTokenGenerator(nil)
	_, err := g.Generate(domain.Backend{Kind: domain.BackendKindVLLM})
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestFreeTokenGenerator_GeneratesAndSkipsWhenCustomized(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewFreeTokenGenerator(store)
	backend := domain.Backend{
		ID:         "freetoken-test",
		Kind:       domain.BackendKindFreeToken,
		Executable: "echo",
		SchemaRef:  "schemas/freetoken-test.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if schema.BackendKind != domain.BackendKindFreeToken {
		t.Fatalf("backendKind = %q", schema.BackendKind)
	}
	for _, long := range []string{"moe-backend", "moe-cache-auto", "memory-ratio", "attention-backend", "tensor-parallel-size", "kv-reserve-tokens", "port"} {
		if _, ok := schema.Flags[long]; !ok {
			t.Errorf("expected %q flag in generated schema", long)
		}
	}
	schema.Source.Customized = true
	_ = store.Save("freetoken-test.json", schema)
	schema2, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if !schema2.Source.Customized {
		t.Fatal("expected customized schema to be preserved")
	}
}

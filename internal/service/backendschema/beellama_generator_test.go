package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestBeeLlamaGenerator_WrongKind(t *testing.T) {
	g := NewBeeLlamaServerGenerator(nil)
	_, err := g.Generate(domain.Backend{Kind: domain.BackendKindLlamaServer})
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestBeeLlamaGenerator_FallsBackWhenBinaryMissing(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewBeeLlamaServerGenerator(store)

	backend := domain.Backend{
		ID:         "beellama-test",
		Kind:       domain.BackendKindBeeLlamaCpp,
		Executable: "/nonexistent/definitely-not-a-binary",
		SchemaRef:  "schemas/beellama-test.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("expected graceful fallback, got error: %v", err)
	}
	if schema.BackendKind != domain.BackendKindBeeLlamaCpp {
		t.Fatalf("fallback schema kind = %q, want beellama-cpp", schema.BackendKind)
	}
	fs := schema.ToFlagSchema()
	for _, want := range []string{"kv-tail-tokens", "spec-type", "cache-type-k"} {
		if _, ok := fs.Lookup(want); !ok {
			t.Fatalf("fallback schema should contain curated fork flag %q", want)
		}
	}
	// KVarN compression cache types must be present on the curated cache-type enum.
	ctk, _ := fs.Lookup("cache-type-k")
	if !containsStr(ctk.EnumValues, "kvarn4") {
		t.Fatalf("cache-type-k enum missing KVarN type: %v", ctk.EnumValues)
	}
}

func TestBeeLlamaGenerator_SkipsWhenCustomized(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewBeeLlamaServerGenerator(store)
	backend := domain.Backend{
		ID:         "beellama-edit",
		Kind:       domain.BackendKindBeeLlamaCpp,
		Executable: "/nonexistent/definitely-not-a-binary",
		SchemaRef:  "schemas/beellama-edit.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	schema.Source.Customized = true
	_ = store.Save("beellama-edit.json", schema)
	schema2, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if !schema2.Source.Customized {
		t.Fatal("expected customized schema to be preserved")
	}
}

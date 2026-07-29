package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestUnslothGenerator_WrongKind(t *testing.T) {
	g := NewUnslothGenerator(nil)
	_, err := g.Generate(domain.Backend{Kind: domain.BackendKindLlamaServer})
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestUnslothGenerator_GeneratesAndSkipsWhenCustomized(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewUnslothGenerator(store)
	backend := domain.Backend{
		ID:         "unsloth-test",
		Kind:       domain.BackendKindUnsloth,
		Executable: "echo",
		SchemaRef:  "schemas/unsloth-test.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if schema.BackendKind != domain.BackendKindUnsloth {
		t.Fatalf("backendKind = %q", schema.BackendKind)
	}
	if _, ok := schema.Flags["gguf-variant"]; !ok {
		t.Fatal("expected gguf-variant flag in generated schema")
	}
	schema.Source.Customized = true
	_ = store.Save("unsloth-test.json", schema)
	schema2, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if !schema2.Source.Customized {
		t.Fatal("expected customized schema to be preserved")
	}
}

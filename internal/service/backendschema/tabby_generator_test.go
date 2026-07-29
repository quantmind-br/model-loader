package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestTabbyGenerator_WrongKind(t *testing.T) {
	g := NewTabbyGenerator(nil)
	_, err := g.Generate(domain.Backend{Kind: domain.BackendKindLlamaServer})
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestTabbyGenerator_GeneratesAndSkipsWhenCustomized(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewTabbyGenerator(store)
	backend := domain.Backend{
		ID:         "tabby-test",
		Kind:       domain.BackendKindTabby,
		Executable: "echo",
		SchemaRef:  "schemas/tabby-test.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if schema.BackendKind != domain.BackendKindTabby {
		t.Fatalf("backendKind = %q", schema.BackendKind)
	}
	if _, ok := schema.Flags["cache-mode"]; !ok {
		t.Fatal("expected cache-mode flag in generated schema")
	}
	if _, ok := schema.Flags["tensor-parallel"]; !ok {
		t.Fatal("expected tensor-parallel flag in generated schema")
	}
	schema.Source.Customized = true
	_ = store.Save("tabby-test.json", schema)
	schema2, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if !schema2.Source.Customized {
		t.Fatal("expected customized schema to be preserved")
	}
}

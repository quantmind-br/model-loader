package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestBuunGenerator_WrongKind(t *testing.T) {
	g := NewBuunServerGenerator(nil)
	_, err := g.Generate(domain.Backend{Kind: domain.BackendKindLlamaServer})
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestBuunGenerator_FallsBackWhenBinaryMissing(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewBuunServerGenerator(store)

	backend := domain.Backend{
		ID:         "buun-test",
		Kind:       domain.BackendKindBuunLlamaCpp,
		Executable: "/nonexistent/definitely-not-a-binary",
		SchemaRef:  "schemas/buun-test.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("expected graceful fallback, got error: %v", err)
	}
	if _, ok := schema.ToFlagSchema().Lookup("spec-draft-model"); !ok {
		t.Fatal("fallback schema should contain curated fork flags")
	}
}

func TestBuunGenerator_SkipsWhenCustomized(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewBuunServerGenerator(store)
	backend := domain.Backend{
		ID:         "buun-edit",
		Kind:       domain.BackendKindBuunLlamaCpp,
		Executable: "/nonexistent/definitely-not-a-binary",
		SchemaRef:  "schemas/buun-edit.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	schema.Source.Customized = true
	_ = store.Save("buun-edit.json", schema)
	schema2, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if !schema2.Source.Customized {
		t.Fatal("expected customized schema to be preserved")
	}
}

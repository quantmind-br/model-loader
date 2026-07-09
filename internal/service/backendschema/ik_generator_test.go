package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestIkLlamaGenerator_WrongKind(t *testing.T) {
	g := NewIkLlamaServerGenerator(nil)
	_, err := g.Generate(domain.Backend{Kind: domain.BackendKindLlamaServer})
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestIkLlamaGenerator_CuratedWhenBinaryMissing(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewIkLlamaServerGenerator(store)

	backend := domain.Backend{
		ID:         "ik-test",
		Kind:       domain.BackendKindIkLlamaCpp,
		Executable: "/nonexistent/definitely-not-a-binary",
		SchemaRef:  "schemas/ik-test.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("expected curated schema, got error: %v", err)
	}
	if schema.BackendKind != domain.BackendKindIkLlamaCpp {
		t.Fatalf("BackendKind = %q, want %q", schema.BackendKind, domain.BackendKindIkLlamaCpp)
	}
	if schema.BackendID != "ik-test" {
		t.Fatalf("BackendID = %q, want ik-test", schema.BackendID)
	}
	if _, ok := schema.Flags["mla-use"]; !ok {
		t.Fatal("curated schema should contain mla-use")
	}
	loaded, err := store.Load("ik-test.json")
	if err != nil {
		t.Fatalf("schema not persisted: %v", err)
	}
	if loaded.BackendKind != domain.BackendKindIkLlamaCpp {
		t.Fatalf("persisted BackendKind = %q", loaded.BackendKind)
	}
}

func TestIkLlamaGenerator_SkipsWhenEditable(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewIkLlamaServerGenerator(store)
	backend := domain.Backend{
		ID:         "ik-edit",
		Kind:       domain.BackendKindIkLlamaCpp,
		Executable: "/nonexistent/definitely-not-a-binary",
		SchemaRef:  "schemas/ik-edit.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	schema.Source.Editable = true
	_ = store.Save("ik-edit.json", schema)
	schema2, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if !schema2.Source.Editable {
		t.Fatal("expected editable schema to be preserved")
	}
}

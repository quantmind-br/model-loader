package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestTokenSpeedGenerator_WrongKind(t *testing.T) {
	g := NewTokenSpeedGenerator(nil)
	_, err := g.Generate(domain.Backend{Kind: domain.BackendKindLlamaServer})
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestTokenSpeedGenerator_GeneratesAndSkipsWhenCustomized(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewTokenSpeedGenerator(store)
	backend := domain.Backend{
		ID:         "tokenspeed-test",
		Kind:       domain.BackendKindTokenSpeed,
		Executable: "echo",
		SchemaRef:  "schemas/tokenspeed-test.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if schema.BackendKind != domain.BackendKindTokenSpeed {
		t.Fatalf("backendKind = %q", schema.BackendKind)
	}
	if _, ok := schema.Flags["max-model-len"]; !ok {
		t.Fatal("expected max-model-len flag in generated schema")
	}
	schema.Source.Customized = true
	if err := store.Save("tokenspeed-test.json", schema); err != nil {
		t.Fatalf("save customized schema: %v", err)
	}
	schema2, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if !schema2.Source.Customized {
		t.Fatal("expected customized schema to be preserved")
	}
}

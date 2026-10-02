package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestStrataGenerator_WrongKind(t *testing.T) {
	g := NewStrataGenerator(nil)
	_, err := g.Generate(domain.Backend{Kind: domain.BackendKindVLLM})
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestStrataGenerator_GeneratesAndSkipsWhenCustomized(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewStrataGenerator(store)
	backend := domain.Backend{
		ID:         "strata-test",
		Kind:       domain.BackendKindStrata,
		Executable: "echo",
		SchemaRef:  "schemas/strata-test.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if schema.BackendKind != domain.BackendKindStrata {
		t.Fatalf("backendKind = %q", schema.BackendKind)
	}
	for _, long := range []string{"config", "max-context", "gpu", "host", "port", "fit-max-tokens"} {
		if _, ok := schema.Flags[long]; !ok {
			t.Errorf("expected %q flag in generated schema", long)
		}
	}
	schema.Source.Customized = true
	_ = store.Save("strata-test.json", schema)
	schema2, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if !schema2.Source.Customized {
		t.Fatal("expected customized schema to be preserved")
	}
}

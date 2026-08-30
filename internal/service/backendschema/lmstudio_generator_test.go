package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestLMStudioGenerator_WrongKind(t *testing.T) {
	g := NewLMStudioGenerator(nil)
	_, err := g.Generate(domain.Backend{Kind: domain.BackendKindTabby})
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestLMStudioGenerator_GeneratesAndSkipsWhenCustomized(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewLMStudioGenerator(store)
	backend := domain.Backend{
		ID:         "lmstudio-test",
		Kind:       domain.BackendKindLMStudio,
		Executable: "lms",
		SchemaRef:  "schemas/lmstudio-test.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if schema.BackendKind != domain.BackendKindLMStudio {
		t.Fatalf("backendKind = %q", schema.BackendKind)
	}
	for _, want := range []string{"gpu", "context-length", "parallel", "ttl", "identifier", "port", "bind", "cors"} {
		if _, ok := schema.Flags[want]; !ok {
			t.Errorf("expected %q flag in generated schema", want)
		}
	}
	if spec := schema.Flags["port"]; !spec.IsPort {
		t.Error("port flag must carry IsPort (process manager owns allocation)")
	}
	if spec := schema.Flags["gpu"]; len(spec.Keywords) == 0 {
		t.Error("gpu flag must accept off/max keywords alongside the numeric ratio")
	}
	schema.Source.Customized = true
	_ = store.Save("lmstudio-test.json", schema)
	schema2, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if !schema2.Source.Customized {
		t.Fatal("expected customized schema to be preserved")
	}
}

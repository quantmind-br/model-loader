package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestRegisterDefaults_RegistersAllGeneratorKinds(t *testing.T) {
	schemaStore := backendcatalog.NewFSSchemaStore(t.TempDir())
	m := NewManager(backendcatalog.NewFSStore(t.TempDir()), schemaStore)
	RegisterDefaults(m, schemaStore)

	want := []domain.BackendKind{
		domain.BackendKindLlamaServer,
		domain.BackendKindSGLang,
		domain.BackendKindVLLM,
		domain.BackendKindDFlash,
		domain.BackendKindBuunLlamaCpp,
		domain.BackendKindBeeLlamaCpp,
		domain.BackendKindUnsloth,
	}
	gens := m.Generators()
	if len(gens) != len(want) {
		t.Fatalf("registered %d generators, want %d: %v", len(gens), len(want), gens)
	}
	for _, k := range want {
		if gens[k] == nil {
			t.Errorf("no generator registered for kind %q", k)
		}
	}
}

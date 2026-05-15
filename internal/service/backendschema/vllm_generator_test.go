package backendschema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestVLLMGenerator_WrongKind(t *testing.T) {
	g := NewVLLMGenerator(nil)
	backend := domain.Backend{Kind: domain.BackendKindLlamaServer}
	_, err := g.Generate(backend)
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestVLLMGenerator_SkipsWhenEditable(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewVLLMGenerator(store)
	backend := domain.Backend{
		ID:         "vllm-test",
		Kind:       domain.BackendKindVLLM,
		Executable: "echo",
		SchemaRef:  "schemas/vllm-test.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	schema.Source.Editable = true
	_ = store.Save("vllm-test.json", schema)
	schema2, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if !schema2.Source.Editable {
		t.Fatal("expected editable schema to be preserved")
	}
}

func TestVLLMGenerator_Python3Fallback(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewVLLMGenerator(store)

	// Only python3 exists, not python.
	tmpBin := filepath.Join(dir, "python3")
	if err := os.WriteFile(tmpBin, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPATH := os.Getenv("PATH")
	os.Setenv("PATH", dir)
	defer os.Setenv("PATH", oldPATH)

	backend := domain.Backend{
		ID:         "vllm-py3",
		Kind:       domain.BackendKindVLLM,
		Executable: "python -m vllm.entrypoints.openai.api_server",
		SchemaRef:  "schemas/vllm-py3.json",
	}
	_, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("Generate with python3 fallback: %v", err)
	}
}

package backendschema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestSGLangGenerator_WrongKind(t *testing.T) {
	g := NewSGLangGenerator(nil)
	backend := domain.Backend{Kind: domain.BackendKindLlamaServer}
	_, err := g.Generate(backend)
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestSGLangGenerator_SkipsWhenCustomized(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewSGLangGenerator(store)
	backend := domain.Backend{
		ID:         "sglang-test",
		Kind:       domain.BackendKindSGLang,
		Executable: "echo",
		SchemaRef:  "schemas/sglang-test.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	schema.Source.Customized = true
	_ = store.Save("sglang-test.json", schema)
	schema2, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if !schema2.Source.Customized {
		t.Fatal("expected customized schema to be preserved")
	}
}

func TestSGLangGenerator_Python3Fallback(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewSGLangGenerator(store)

	// Only python3 exists, not python.
	tmpBin := filepath.Join(dir, "python3")
	if err := os.WriteFile(tmpBin, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPATH := os.Getenv("PATH")
	os.Setenv("PATH", dir)
	defer os.Setenv("PATH", oldPATH)

	backend := domain.Backend{
		ID:         "sglang-py3",
		Kind:       domain.BackendKindSGLang,
		Executable: "python -m sglang.launch_server",
		SchemaRef:  "schemas/sglang-py3.json",
	}
	_, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("Generate with python3 fallback: %v", err)
	}
}

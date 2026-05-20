package profile_editor

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/llamahelp"
	"github.com/quantmind-br/model-loader/internal/service/sglanghelp"
	"github.com/quantmind-br/model-loader/internal/service/vllmhelp"
)

func TestEssentialsFor_Llama(t *testing.T) {
	schema := llamahelp.EmbeddedSchema()
	got := essentialsFor(domain.BackendKindLlamaServer, schema)
	// llamahelp.EmbeddedSchema() does not include "port", so 7 of 8 resolve.
	if len(got) != 7 {
		t.Fatalf("expected 7 llama essential fields (port absent from embedded schema), got %d", len(got))
	}
	want := []string{
		"n-gpu-layers", "ctx-size", "batch-size", "ubatch-size",
		"flash-attn", "cache-type-k", "cache-type-v",
	}
	for i, f := range got {
		if f.Flag != want[i] {
			t.Errorf("field[%d].Flag = %q, want %q", i, f.Flag, want[i])
		}
	}
}

func TestEssentialsFor_VLLM(t *testing.T) {
	schema := vllmhelp.EmbeddedSchema()
	got := essentialsFor(domain.BackendKindVLLM, schema)
	if len(got) != 7 {
		t.Fatalf("expected 7 vLLM essential fields, got %d", len(got))
	}
	want := []string{
		"tensor-parallel-size", "gpu-memory-utilization", "max-model-len",
		"dtype", "quantization", "port", "served-model-name",
	}
	for i, f := range got {
		if f.Flag != want[i] {
			t.Errorf("field[%d].Flag = %q, want %q", i, f.Flag, want[i])
		}
	}
}

func TestEssentialsFor_SGLang(t *testing.T) {
	schema := sglanghelp.EmbeddedSchema()
	got := essentialsFor(domain.BackendKindSGLang, schema)
	if len(got) != 8 {
		t.Fatalf("expected 8 SGLang essential fields, got %d", len(got))
	}
	want := []string{
		"tp-size", "dp-size", "mem-fraction-static", "dtype",
		"quantization", "context-length", "port", "served-model-name",
	}
	for i, f := range got {
		if f.Flag != want[i] {
			t.Errorf("field[%d].Flag = %q, want %q", i, f.Flag, want[i])
		}
	}
}

func TestEssentialsFor_UnknownKindFallsBackToLlama(t *testing.T) {
	schema := llamahelp.EmbeddedSchema()
	got := essentialsFor("unknown-kind", schema)
	// llamahelp.EmbeddedSchema() does not include "port", so 7 of 8 resolve.
	if len(got) != 7 {
		t.Fatalf("expected 7 fields (llama fallback, port absent), got %d", len(got))
	}
	if got[0].Flag != "n-gpu-layers" {
		t.Errorf("first fallback field = %q, want n-gpu-layers", got[0].Flag)
	}
}

func TestEssentialsFor_OmitsAbsentFromSchema(t *testing.T) {
	schema := domain.FlagSchema{
		Flags: map[string]domain.FlagSpec{
			"ctx-size": {Long: "ctx-size"},
			"port":     {Long: "port"},
		},
	}
	got := essentialsFor(domain.BackendKindLlamaServer, schema)
	if len(got) != 2 {
		t.Fatalf("expected 2 fields present in schema, got %d", len(got))
	}
	if got[0].Flag != "ctx-size" || got[1].Flag != "port" {
		t.Errorf("got flags %q, want [ctx-size port]", []string{got[0].Flag, got[1].Flag})
	}
}

func TestEssentialField_CoerceWithCustom(t *testing.T) {
	f := EssentialField{Coerce: FlashAttnToString}
	if got := f.coerce(true); got != "on" {
		t.Errorf("coerce(true) = %q, want on", got)
	}
	if got := f.coerce(false); got != "off" {
		t.Errorf("coerce(false) = %q, want off", got)
	}
	if got := f.coerce("auto"); got != "auto" {
		t.Errorf("coerce(auto) = %q, want auto", got)
	}
}

func TestEssentialField_CoerceDefault(t *testing.T) {
	f := EssentialField{}
	if got := f.coerce(float64(42)); got != "42" {
		t.Errorf("coerce(42) = %q, want 42", got)
	}
	if got := f.coerce("hello"); got != "hello" {
		t.Errorf("coerce(hello) = %q, want hello", got)
	}
	if got := f.coerce(nil); got != "" {
		t.Errorf("coerce(nil) = %q, want empty", got)
	}
}

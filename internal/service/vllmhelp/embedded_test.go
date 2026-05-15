package vllmhelp

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestEmbeddedSchema_HasEssentialFlags(t *testing.T) {
	fs := EmbeddedSchema()

	essential := []string{
		"model",
		"dtype",
		"tensor-parallel-size",
		"pipeline-parallel-size",
		"max-model-len",
		"gpu-memory-utilization",
		"swap-space",
		"max-num-seqs",
		"quantization",
		"host",
		"port",
		"api-key",
		"served-model-name",
		"enable-prefix-caching",
		"enforce-eager",
		"chat-template",
		"trust-remote-code",
		"device",
		"download-dir",
	}

	for _, name := range essential {
		spec, ok := fs.Flags[name]
		if !ok {
			t.Fatalf("missing essential flag %q", name)
		}
		if spec.Long != name {
			t.Fatalf("flag %q: Long=%q, want %q", name, spec.Long, name)
		}
	}
}

func TestEmbeddedSchema_PortDefaultsTo8000(t *testing.T) {
	fs := EmbeddedSchema()
	spec, ok := fs.Flags["port"]
	if !ok {
		t.Fatal("missing port flag")
	}
	if spec.Type != domain.FlagTypeInt {
		t.Fatalf("port type=%v, want Int", spec.Type)
	}
	if spec.Default != float64(8000) {
		t.Fatalf("port default=%v, want 8000", spec.Default)
	}
}

func TestEmbeddedSchema_DtypeIsEnum(t *testing.T) {
	fs := EmbeddedSchema()
	spec, ok := fs.Flags["dtype"]
	if !ok {
		t.Fatal("missing dtype flag")
	}
	if spec.Type != domain.FlagTypeEnum {
		t.Fatalf("dtype type=%v, want Enum", spec.Type)
	}
	want := []string{"auto", "half", "float16", "bfloat16", "float", "float32"}
	if len(spec.EnumValues) != len(want) {
		t.Fatalf("dtype enum values=%v, want %v", spec.EnumValues, want)
	}
}

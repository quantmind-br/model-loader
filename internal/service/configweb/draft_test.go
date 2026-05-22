package configweb

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestDraft_ToProfile_TypesArgsViaSchema(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"ctx-size":   {Long: "ctx-size", Type: domain.FlagTypeInt},
		"flash-attn": {Long: "flash-attn", Type: domain.FlagTypeString},
	}}
	d := Draft{
		ID:        "qwen",
		Name:      "Qwen",
		Model:     "/models/qwen.gguf",
		BackendID: "llama",
		Args:      map[string]string{"ctx-size": "8192", "flash-attn": "on"},
	}
	p := d.ToProfile(schema)
	if p.Args["ctx-size"] != float64(8192) && p.Args["ctx-size"] != 8192 {
		t.Fatalf("ctx-size not coerced to int: %#v", p.Args["ctx-size"])
	}
	if p.Args["flash-attn"] != "on" {
		t.Fatalf("flash-attn wrong: %#v", p.Args["flash-attn"])
	}
}

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

func TestDraft_ToProfile_CopiesEnv(t *testing.T) {
	d := Draft{
		ID:   "test",
		Name: "Test",
		Env:  []domain.EnvVar{{Key: "FOO", Value: "bar"}},
	}
	p := d.ToProfile(domain.FlagSchema{})
	if len(p.Launch.Env) != 1 || p.Launch.Env[0].Key != "FOO" || p.Launch.Env[0].Value != "bar" {
		t.Fatalf("env not copied: %+v", p.Launch.Env)
	}
}

func TestDraft_ApplyTo_CopiesEnv(t *testing.T) {
	existing := domain.Profile{
		ID:     "test",
		Launch: domain.LaunchConfig{BackendID: "llama", Env: []domain.EnvVar{{Key: "OLD", Value: "val"}}},
	}
	d := Draft{
		ID:        "test",
		Name:      "Test",
		BackendID: "llama",
		Env:       []domain.EnvVar{{Key: "NEW", Value: "val2"}},
	}
	p := d.ApplyTo(existing, domain.FlagSchema{})
	if len(p.Launch.Env) != 1 || p.Launch.Env[0].Key != "NEW" {
		t.Fatalf("env not replaced: %+v", p.Launch.Env)
	}
	if p.Launch.BackendID != "llama" {
		t.Fatalf("backend id lost: %s", p.Launch.BackendID)
	}
}

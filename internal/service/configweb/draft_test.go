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

func TestCoerceArgs_DropsReservedPort(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"port":     {Long: "port", Type: domain.FlagTypeInt},
		"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt},
	}}
	out := coerceArgs(map[string]string{"port": "8080", "ctx-size": "4096"}, schema)
	if _, ok := out["port"]; ok {
		t.Fatalf("reserved flag port must be dropped on submit: %#v", out)
	}
	if out["ctx-size"] != 4096 {
		t.Fatalf("ctx-size must survive reserved-flag filtering: %#v", out)
	}
}

func TestCoerceArgs_BoolValues(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"flash-attn": {Long: "flash-attn", Type: domain.FlagTypeBool},
	}}
	cases := map[string]bool{"on": true, "true": true, "off": false, "false": false}
	for in, want := range cases {
		out := coerceArgs(map[string]string{"flash-attn": in}, schema)
		if out["flash-attn"] != want {
			t.Errorf("coerceArgs bool %q = %#v, want %v", in, out["flash-attn"], want)
		}
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
	p := d.ApplyTo(existing, domain.FlagSchema{}, nil)
	if len(p.Launch.Env) != 1 || p.Launch.Env[0].Key != "NEW" {
		t.Fatalf("env not replaced: %+v", p.Launch.Env)
	}
	if p.Launch.BackendID != "llama" {
		t.Fatalf("backend id lost: %s", p.Launch.BackendID)
	}
}

// TestDraft_ApplyTo_PreservesUnsurfacedArgs guards the data-loss bug where the
// web editor saved a profile by wholesale-replacing Args with only the
// form-surfaced fields — silently dropping configured flags the schema's
// presentation does not render (e.g. tool-calling flags). The editor must
// preserve args it never surfaced as form fields.
func TestDraft_ApplyTo_PreservesUnsurfacedArgs(t *testing.T) {
	existing := domain.Profile{
		ID: "diff",
		Args: map[string]any{
			"max-num-seqs":            1,
			"enable-auto-tool-choice": true,     // unsurfaced bool
			"tool-call-parser":        "gemma4", // unsurfaced string
		},
		Launch: domain.LaunchConfig{BackendID: "vllm"},
	}
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"max-num-seqs":            {Long: "max-num-seqs", Type: domain.FlagTypeInt},
		"enable-auto-tool-choice": {Long: "enable-auto-tool-choice", Type: domain.FlagTypeBool},
		"tool-call-parser":        {Long: "tool-call-parser", Type: domain.FlagTypeString},
	}}
	// Only max-num-seqs was rendered as a form field; the form re-submits it.
	surfaced := map[string]bool{"max-num-seqs": true}
	d := Draft{ID: "diff", BackendID: "vllm", Args: map[string]string{"max-num-seqs": "4"}}

	p := d.ApplyTo(existing, schema, surfaced)

	if p.Args["enable-auto-tool-choice"] != true {
		t.Errorf("unsurfaced bool flag dropped: %#v", p.Args["enable-auto-tool-choice"])
	}
	if p.Args["tool-call-parser"] != "gemma4" {
		t.Errorf("unsurfaced string flag dropped: %#v", p.Args["tool-call-parser"])
	}
	if p.Args["max-num-seqs"] != 4 {
		t.Errorf("surfaced flag not updated from form: %#v", p.Args["max-num-seqs"])
	}
}

// TestDraft_ApplyTo_ClearsSurfacedFlag guards the inverse: a surfaced flag the
// user cleared (absent from the submission) must be removed, while unsurfaced
// args still survive. The merge must distinguish "unsurfaced -> preserve" from
// "surfaced-but-cleared -> delete".
func TestDraft_ApplyTo_ClearsSurfacedFlag(t *testing.T) {
	existing := domain.Profile{
		ID:     "p",
		Args:   map[string]any{"ctx-size": 8192, "tool-call-parser": "gemma4"},
		Launch: domain.LaunchConfig{BackendID: "llama"},
	}
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt},
	}}
	surfaced := map[string]bool{"ctx-size": true}
	d := Draft{ID: "p", BackendID: "llama", Args: map[string]string{}} // ctx-size cleared

	p := d.ApplyTo(existing, schema, surfaced)

	if _, ok := p.Args["ctx-size"]; ok {
		t.Errorf("cleared surfaced flag should be removed: %#v", p.Args)
	}
	if p.Args["tool-call-parser"] != "gemma4" {
		t.Errorf("unsurfaced flag must survive clearing a surfaced one: %#v", p.Args)
	}
}

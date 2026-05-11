package validator

import (
	"os"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestValidator_EmptySchemaProducesNoTypeIssues(t *testing.T) {
	v := New()
	p := domain.Profile{
		ID:    "x",
		Name:  "X",
		Model: "", // Fixed: was /tmp/nonexistent.gguf, now trips existence rule
		Args:  map[string]any{},
	}
	rep := v.Validate(p, domain.FlagSchema{Flags: map[string]domain.FlagSpec{}})
	// At this stage, with no schema and no rules wired, Errors should be empty.
	if len(rep.Errors) != 0 {
		t.Errorf("Errors=%v, want empty", rep.Errors)
	}
}

func TestValidator_TypeRule(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"ctx-size":   {Long: "ctx-size", Type: domain.FlagTypeInt},
		"flash-attn": {Long: "flash-attn", Type: domain.FlagTypeEnum, EnumValues: []string{"on", "off", "auto"}},
		"mlock":      {Long: "mlock", Type: domain.FlagTypeBool},
	}}
	cases := []struct {
		name      string
		args      map[string]any
		wantErrs  int
		wantField string
	}{
		{"int ok as float64", map[string]any{"ctx-size": float64(4096)}, 0, ""},
		{"int ok as int", map[string]any{"ctx-size": 4096}, 0, ""},
		{"int rejects string", map[string]any{"ctx-size": "abc"}, 1, "ctx-size"},
		{"int rejects fractional float64", map[string]any{"ctx-size": float64(4096.5)}, 1, "ctx-size"},
		{"enum ok", map[string]any{"flash-attn": "on"}, 0, ""},
		{"enum rejects unknown", map[string]any{"flash-attn": "maybe"}, 1, "flash-attn"},
		{"bool ok", map[string]any{"mlock": true}, 0, ""},
		{"bool rejects string", map[string]any{"mlock": "yes"}, 1, "mlock"},
		{"unknown flag is error", map[string]any{"unheard-of": 1}, 1, "unheard-of"},
	}
	v := New()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Args: tc.args}
			rep := v.Validate(p, schema)
			if got := len(rep.Errors); got != tc.wantErrs {
				t.Fatalf("Errors=%d (%v), want %d", got, rep.Errors, tc.wantErrs)
			}
			if tc.wantErrs > 0 && rep.Errors[0].Field != tc.wantField {
				t.Errorf("Errors[0].Field=%q, want %q", rep.Errors[0].Field, tc.wantField)
			}
		})
	}
}

func TestValidator_ModelExistence(t *testing.T) {
	tmp := t.TempDir()
	existing := tmp + "/m.gguf"
	if err := os.WriteFile(existing, []byte("g"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := New()
	cases := []struct {
		name     string
		model    string
		wantErrs int
	}{
		{"empty path no error (rule only applies when set)", "", 0},
		{"existing path no error", existing, 0},
		{"missing path errors", tmp + "/nope.gguf", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Model: tc.model}
			rep := v.Validate(p, domain.FlagSchema{})
			if got := len(rep.Errors); got != tc.wantErrs {
				t.Errorf("Errors=%d (%v), want %d", got, rep.Errors, tc.wantErrs)
			}
		})
	}
}

func TestValidator_HFRepoIDNoExistenceError(t *testing.T) {
	v := New()
	cases := []struct {
		name    string
		model   string
		wantErr bool
	}{
		{"dotted HF repo ID", "Qwen/Qwen2.5-7B-Instruct", false},
		{"dotted HF repo ID 2", "meta-llama/Llama-3.1-8B-Instruct", false},
		{"local gguf file missing", "models/model.gguf", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Model: tc.model}
			rep := v.Validate(p, domain.FlagSchema{})
			gotErr := len(rep.Errors) > 0
			if gotErr != tc.wantErr {
				t.Errorf("Errors=%v, wantErr=%v", rep.Errors, tc.wantErr)
			}
		})
	}
}

func TestValidator_ExistingLocalPathNotTreatedAsHFRepo(t *testing.T) {
	tmp := t.TempDir()
	existingDir := tmp + "/models"
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	v := New()
	// A local path like "models/llama3" that exists should be validated
	// as a local path, not treated as a HF repo ID.
	p := domain.Profile{ID: "x", Model: existingDir}
	rep := v.Validate(p, domain.FlagSchema{})
	if len(rep.Errors) != 0 {
		t.Errorf("existing local dir should not error; got %v", rep.Errors)
	}
}

func cacheTypes() []string {
	return []string{"f32", "f16", "bf16", "q8_0", "q4_0"}
}

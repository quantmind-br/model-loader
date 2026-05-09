package validator

import (
	"os"
	"testing"

	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
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
		{"enum ok", map[string]any{"flash-attn": "on"}, 0, ""},
		{"enum rejects unknown", map[string]any{"flash-attn": "maybe"}, 1, "flash-attn"},
		{"bool ok", map[string]any{"mlock": true}, 0, ""},
		{"bool rejects string", map[string]any{"mlock": "yes"}, 1, "mlock"},
		{"unknown flag is ignored", map[string]any{"unheard-of": 1}, 0, ""},
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

func TestValidator_CrossFieldRules(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"ctx-size":     {Long: "ctx-size", Type: domain.FlagTypeInt},
		"batch-size":   {Long: "batch-size", Type: domain.FlagTypeInt},
		"ubatch-size":  {Long: "ubatch-size", Type: domain.FlagTypeInt},
		"flash-attn":   {Long: "flash-attn", Type: domain.FlagTypeEnum, EnumValues: []string{"on", "off", "auto"}},
		"cache-type-k": {Long: "cache-type-k", Type: domain.FlagTypeEnum, EnumValues: cacheTypes()},
		"cache-type-v": {Long: "cache-type-v", Type: domain.FlagTypeEnum, EnumValues: cacheTypes()},
		"ngl":          {Long: "n-gpu-layers", Short: "ngl", Type: domain.FlagTypeInt},
	}}
	v := New()
	cases := []struct {
		name      string
		args      map[string]any
		wantErrs  int
		wantWarns int
		wantField string
	}{
		{
			name:      "ubatch > batch is error",
			args:      map[string]any{"batch-size": 2048, "ubatch-size": 4096},
			wantErrs:  1,
			wantField: "ubatch-size",
		},
		{
			name:     "ubatch == batch is fine",
			args:     map[string]any{"batch-size": 2048, "ubatch-size": 2048},
			wantErrs: 0,
		},
		{
			name:      "flash-attn on with f16 cache warns",
			args:      map[string]any{"flash-attn": "on", "cache-type-k": "f16", "cache-type-v": "f16"},
			wantWarns: 1,
		},
		{
			name:      "ctx>32k with ngl<99 warns",
			args:      map[string]any{"ctx-size": 65536, "ngl": 50},
			wantWarns: 1,
		},
		{
			name:      "ctx>32k with ngl=99 no warn",
			args:      map[string]any{"ctx-size": 65536, "ngl": 99},
			wantWarns: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Args: tc.args}
			rep := v.Validate(p, schema)
			if got := len(rep.Errors); got != tc.wantErrs {
				t.Errorf("Errors=%d (%v), want %d", got, rep.Errors, tc.wantErrs)
			}
			if got := len(rep.Warnings); got != tc.wantWarns {
				t.Errorf("Warnings=%d (%v), want %d", got, rep.Warnings, tc.wantWarns)
			}
			if tc.wantField != "" && len(rep.Errors) > 0 && rep.Errors[0].Field != tc.wantField {
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

func cacheTypes() []string {
	return []string{"f32", "f16", "bf16", "q8_0", "q4_0"}
}

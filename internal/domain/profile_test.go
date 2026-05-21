package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestProfile_JSONRoundtrip(t *testing.T) {
	now := time.Date(2026, 4, 28, 15, 30, 0, 0, time.UTC)
	last := now.Add(time.Hour)

	original := Profile{
		SchemaVersion: SchemaVersion,
		ID:            "qwen-coder-32b",
		Name:          "Qwen Coder 32B",
		Description:   "Coding assistant",
		Tags:          []string{"coding", "32b"},
		Model:         "/models/qwen.gguf",
		Args: map[string]any{
			"ngl":          float64(99),
			"ctx-size":     float64(16384),
			"flash-attn":   true,
			"cache-type-k": "q8_0",
		},
		ExtraArgs: []string{},
		Launch: LaunchConfig{
			DefaultBackground: true,
		},
		Meta: ProfileMeta{
			CreatedAt:  now,
			UpdatedAt:  now,
			LastUsedAt: &last,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Profile
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, original.Name)
	}
	if decoded.Args["flash-attn"] != true {
		t.Errorf("flash-attn = %v, want true", decoded.Args["flash-attn"])
	}
	if !decoded.Meta.CreatedAt.Equal(original.Meta.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", decoded.Meta.CreatedAt, original.Meta.CreatedAt)
	}
	if decoded.Meta.LastUsedAt == nil || !decoded.Meta.LastUsedAt.Equal(last) {
		t.Errorf("LastUsedAt = %v, want %v", decoded.Meta.LastUsedAt, last)
	}
}

func TestProfile_JSONRoundtrip_WithEnv(t *testing.T) {
	original := Profile{
		SchemaVersion: SchemaVersion,
		ID:            "qwen",
		Name:          "Qwen",
		Model:         "/m.gguf",
		Args:          map[string]any{"port": float64(8080)},
		Launch: LaunchConfig{
			DefaultBackground: true,
			Env: []EnvVar{
				{Key: "GGML_CUDA_FORCE_CUBLAS_COMPUTE_16F", Value: "1"},
				{Key: "CUDA_VISIBLE_DEVICES", Value: "0,1"},
			},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Profile
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Launch.Env) != 2 {
		t.Fatalf("Env len = %d, want 2", len(decoded.Launch.Env))
	}
	if decoded.Launch.Env[0].Key != "GGML_CUDA_FORCE_CUBLAS_COMPUTE_16F" || decoded.Launch.Env[0].Value != "1" {
		t.Errorf("Env[0] = %+v", decoded.Launch.Env[0])
	}
	if decoded.Launch.Env[1].Key != "CUDA_VISIBLE_DEVICES" || decoded.Launch.Env[1].Value != "0,1" {
		t.Errorf("Env[1] = %+v", decoded.Launch.Env[1])
	}
}

func TestProfile_JSONRoundtrip_EmptyEnvOmitted(t *testing.T) {
	p := Profile{
		SchemaVersion: SchemaVersion,
		ID:            "no-env",
		Name:          "No Env",
		Model:         "/m.gguf",
		Args:          map[string]any{"port": float64(8080)},
		Launch:        LaunchConfig{},
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes := string(data); bytes != "" && bytes[0] != 0 {
		if got := string(data); contains(got, `"env"`) {
			t.Errorf("expected no \"env\" key when Env is nil, got: %s", got)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Qwen Coder 32B", "qwen-coder-32b"},
		{"Llama 3.3 70B Q4_K_M", "llama-3-3-70b-q4-k-m"},
		{"  Mistral!! Small  24b  ", "mistral-small-24b"},
		{"", ""},
	}
	for _, c := range cases {
		got := Slugify(c.in)
		if got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsValidSlug(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"qwen-coder", true},
		{"llama-3-3-70b", true},
		{"qwen3.6-35b-a3b-mtp-200k", true}, // dots allowed
		{"q4_k_m", true},                   // underscores allowed
		{"a", true},
		{"", false},
		{".qwen", false},   // leading separator
		{"qwen.", false},   // trailing separator
		{"qwen..6", false}, // double separator
		{"qwen._6", false}, // mixed double separator
		{"Qwen-Coder", false}, // uppercase
		{"qwen coder", false}, // space
		{"-qwen", false},      // leading dash
		{"qwen-", false},      // trailing dash
		{"qwen--coder", false},
		{"café", false}, // non-ascii
	}
	for _, c := range cases {
		if got := IsValidSlug(c.in); got != c.want {
			t.Errorf("IsValidSlug(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

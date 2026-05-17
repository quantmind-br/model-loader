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
			"ngl":         float64(99),
			"ctx-size":    float64(16384),
			"flash-attn":  true,
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

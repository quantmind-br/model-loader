package domain

import "testing"

func TestCanonicalFlag(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"short ngl", "ngl", "n-gpu-layers"},
		{"already long", "n-gpu-layers", "n-gpu-layers"},
		{"unknown key", "ctx-size", "ctx-size"},
		{"another unknown", "flash-attn", "flash-attn"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanonicalFlag(tt.key)
			if got != tt.want {
				t.Errorf("CanonicalFlag(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

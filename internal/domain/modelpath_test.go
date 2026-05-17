package domain

import "testing"

func TestLooksLikeHFRepo(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"HF repo dotted", "Qwen/Qwen2.5-7B-Instruct", true},
		{"HF repo standard", "meta-llama/Llama-3.1-8B-Instruct", true},
		{"local file with extension", "models/model.gguf", false},
		{"home-relative path", "~/models/model", false},
		{"absolute path", "/models/model.gguf", false},
		{"current dir relative", "./models/model.gguf", false},
		{"parent dir relative", "../models/model.gguf", false},
		{"single segment", "model.gguf", false},
		{"three segments", "a/b/c", false},
		{"empty string", "", false},
		{"slash only", "/", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LooksLikeHFRepo(tt.path)
			if got != tt.want {
				t.Errorf("LooksLikeHFRepo(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

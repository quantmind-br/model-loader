package domain

import "testing"

func TestBackendKindBuunLlamaCppValue(t *testing.T) {
	if BackendKindBuunLlamaCpp != "buun-llama-cpp" {
		t.Fatalf("got %q, want %q", BackendKindBuunLlamaCpp, "buun-llama-cpp")
	}
}

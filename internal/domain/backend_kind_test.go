package domain

import "testing"

func TestBackendKindBuunLlamaCppValue(t *testing.T) {
	if BackendKindBuunLlamaCpp != "buun-llama-cpp" {
		t.Fatalf("got %q, want %q", BackendKindBuunLlamaCpp, "buun-llama-cpp")
	}
}

func TestBackendKindBeeLlamaCppValue(t *testing.T) {
	if BackendKindBeeLlamaCpp != "beellama-cpp" {
		t.Fatalf("got %q, want %q", BackendKindBeeLlamaCpp, "beellama-cpp")
	}
}

func TestBackendKindTabbyValue(t *testing.T) {
	if BackendKindTabby != "tabby" {
		t.Fatalf("got %q, want %q", BackendKindTabby, "tabby")
	}
}

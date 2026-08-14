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

func TestBackendKindIkLlamaCppValue(t *testing.T) {
	if BackendKindIkLlamaCpp != "ik-llama-cpp" {
		t.Fatalf("got %q, want %q", BackendKindIkLlamaCpp, "ik-llama-cpp")
	}
}

func TestBackendKindTabbyValue(t *testing.T) {
	if BackendKindTabby != "tabby" {
		t.Fatalf("got %q, want %q", BackendKindTabby, "tabby")
	}
}

func TestBackendKindTokenSpeedValue(t *testing.T) {
	if BackendKindTokenSpeed != "tokenspeed" {
		t.Errorf("BackendKindTokenSpeed = %q, want %q", BackendKindTokenSpeed, "tokenspeed")
	}
}

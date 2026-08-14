package tokenspeedhelp

import "testing"

func TestEmbeddedSchema_HasKeyFlagsAndNoManagedFlags(t *testing.T) {
	fs := EmbeddedSchema()
	if fs.Version != "embedded-tokenspeed-v1" {
		t.Fatalf("version = %q, want %q", fs.Version, "embedded-tokenspeed-v1")
	}
	if got := len(fs.Flags); got != 36 {
		t.Fatalf("flag count = %d, want 36", got)
	}
	for _, want := range []string{"max-model-len", "tensor-parallel-size", "gpu-memory-utilization", "attention-backend", "port"} {
		if _, ok := fs.Flags[want]; !ok {
			t.Errorf("missing expected flag %q", want)
		}
	}
	if spec, ok := fs.Flags["port"]; !ok || !spec.IsPort {
		t.Errorf("port must exist with IsPort=true")
	}
	for _, banned := range []string{"host", "model", "model-path", "api-key", "attn-tp-size", "enable-prefix-caching"} {
		if _, ok := fs.Flags[banned]; ok {
			t.Errorf("managed or unsupported flag %q must not be in the schema", banned)
		}
	}
}

func TestEmbeddedSchema_UsesRuntimeDefaults(t *testing.T) {
	fs := EmbeddedSchema()
	trust, ok := fs.Flags["trust-remote-code"]
	if !ok {
		t.Fatal("missing trust-remote-code")
	}
	value, ok := trust.Default.(bool)
	if !ok || value {
		t.Fatalf("trust-remote-code default = %#v, want false", trust.Default)
	}
	gpuMemory, ok := fs.Flags["gpu-memory-utilization"]
	if !ok {
		t.Fatal("missing gpu-memory-utilization")
	}
	if gpuMemory.Default != nil {
		t.Fatalf("gpu-memory-utilization default = %#v, want nil", gpuMemory.Default)
	}
}

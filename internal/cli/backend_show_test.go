package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestShowBackend_Text(t *testing.T) {
	var out bytes.Buffer
	if err := showBackend(&out, twoBackends(), "vllm", false); err != nil {
		t.Fatalf("showBackend: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "vllm-main") || !strings.Contains(s, "vllm") {
		t.Fatalf("missing backend detail: %q", s)
	}
}

func TestShowBackend_JSON(t *testing.T) {
	var out bytes.Buffer
	if err := showBackend(&out, twoBackends(), "llama-server", true); err != nil {
		t.Fatalf("showBackend: %v", err)
	}
	var d backendDetail
	if err := json.Unmarshal(out.Bytes(), &d); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if d.ID != "llama-server" || !d.Default || d.Kind != "llama-server" {
		t.Fatalf("unexpected detail: %+v", d)
	}
}

func TestShowBackend_NotFound(t *testing.T) {
	var out bytes.Buffer
	if err := showBackend(&out, twoBackends(), "nope", false); err == nil {
		t.Fatal("expected not-found error")
	}
}

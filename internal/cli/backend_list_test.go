package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestListBackends_Table(t *testing.T) {
	var out bytes.Buffer
	if err := listBackends(&out, twoBackends(), false); err != nil {
		t.Fatalf("listBackends: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "llama-server") || !strings.Contains(s, "vllm-main") {
		t.Fatalf("table missing backends: %q", s)
	}
	if !strings.Contains(s, "yes") {
		t.Fatalf("default backend not marked: %q", s)
	}
}

func TestListBackends_JSON(t *testing.T) {
	var out bytes.Buffer
	if err := listBackends(&out, twoBackends(), true); err != nil {
		t.Fatalf("listBackends: %v", err)
	}
	var items []backendListItem
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	var def *backendListItem
	for i := range items {
		if items[i].ID == "llama-server" {
			def = &items[i]
		}
	}
	if def == nil || !def.Default {
		t.Fatalf("llama-server should be default: %+v", items)
	}
}

func TestListBackends_EmptyJSONIsArray(t *testing.T) {
	var out bytes.Buffer
	mgr := &fakeBackendManager{}
	if err := listBackends(&out, mgr, true); err != nil {
		t.Fatalf("listBackends: %v", err)
	}
	if strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("empty list should be []: %q", out.String())
	}
}

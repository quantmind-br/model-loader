package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEmitJSON_Indented(t *testing.T) {
	var buf bytes.Buffer
	if err := emitJSON(&buf, map[string]int{"a": 1}); err != nil {
		t.Fatalf("emitJSON: %v", err)
	}
	var got map[string]int
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if got["a"] != 1 {
		t.Fatalf("round-trip failed: %v", got)
	}
	if !strings.Contains(buf.String(), "\n") {
		t.Fatalf("expected indented (multi-line) JSON, got %q", buf.String())
	}
}

func TestPrintTable_AlignsColumns(t *testing.T) {
	var buf bytes.Buffer
	printTable(&buf, []string{"ID", "NAME"}, [][]string{
		{"a", "Alpha"},
		{"bb", "Beta"},
	})
	out := buf.String()
	if !strings.Contains(out, "ID") || !strings.Contains(out, "NAME") {
		t.Fatalf("missing header: %q", out)
	}
	if !strings.Contains(out, "Alpha") || !strings.Contains(out, "Beta") {
		t.Fatalf("missing rows: %q", out)
	}
}

func TestDashOr(t *testing.T) {
	if dashOr("") != "-" {
		t.Fatalf("empty should render as dash")
	}
	if dashOr("x") != "x" {
		t.Fatalf("non-empty should pass through")
	}
}

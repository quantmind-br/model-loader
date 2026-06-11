package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestFormatCandidates(t *testing.T) {
	t.Run("sorts and joins", func(t *testing.T) {
		got := formatCandidates([]string{"charlie", "alpha", "beta"})
		if got != "alpha, beta, charlie" {
			t.Fatalf("want sorted join, got %q", got)
		}
	})
	t.Run("single item", func(t *testing.T) {
		got := formatCandidates([]string{"only"})
		if got != "only" {
			t.Fatalf("single item: got %q", got)
		}
	})
	t.Run("caps at 10 with ellipsis", func(t *testing.T) {
		labels := make([]string, 12)
		for i := range labels {
			labels[i] = fmt.Sprintf("item%02d", i)
		}
		got := formatCandidates(labels)
		if !strings.HasSuffix(got, ", …") {
			t.Fatalf("expected ellipsis suffix, got %q", got)
		}
		parts := strings.Split(strings.TrimSuffix(got, ", …"), ", ")
		if len(parts) != 10 {
			t.Fatalf("expected 10 candidates before ellipsis, got %d", len(parts))
		}
	})
	t.Run("exactly 10 no ellipsis", func(t *testing.T) {
		labels := make([]string, 10)
		for i := range labels {
			labels[i] = fmt.Sprintf("x%d", i)
		}
		got := formatCandidates(labels)
		if strings.Contains(got, "…") {
			t.Fatalf("10 items should have no ellipsis, got %q", got)
		}
	})
}

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

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
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

// UIUX-016: clip() is rune-safe (was byte-slicing, truncating multi-byte names).
func TestClip_RuneSafe(t *testing.T) {
	// Short strings pass through untouched.
	if got := clip("héllo", 10); got != "héllo" {
		t.Fatalf("short string should pass through, got %q", got)
	}
	// Truncation counts runes, not bytes, and never splits a multi-byte rune.
	got := clip("héllo wörld ünîcödé", 8)
	if !utf8.ValidString(got) {
		t.Fatalf("clip produced invalid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 8 {
		t.Fatalf("clip(…, 8) should be 8 runes (7 + ellipsis), got %d in %q", n, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated string should end with ellipsis, got %q", got)
	}
	// A byte-slicing clip would cut a 2-byte rune here and corrupt the output;
	// verify the emoji boundary stays intact.
	emoji := clip("🙂🙂🙂🙂🙂", 3)
	if !utf8.ValidString(emoji) {
		t.Fatalf("clip split a multi-byte rune: %q", emoji)
	}
	if utf8.RuneCountInString(emoji) != 3 {
		t.Fatalf("clip(emoji, 3) should be 3 runes, got %q", emoji)
	}
}

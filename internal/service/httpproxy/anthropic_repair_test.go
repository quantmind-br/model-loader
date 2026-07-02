package httpproxy

import (
	"strings"
	"testing"
)

func TestFixJSON(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":1}`, `{"a":1}`},                 // already valid → unchanged
		{`{"a":"it's ok"}`, `{"a":"it's ok"}`}, // valid, apostrophe inside → unchanged
		{`{'a': 1}`, `{"a": 1}`},               // single-quoted key
		{`{'k': 'v'}`, `{"k": "v"}`},           // single-quoted key + value
		{`{"a":'v'}`, `{"a":"v"}`},             // mixed
		{`not json`, `not json`},               // irreparable → unchanged
		{``, ``},                               // empty → unchanged
	}
	for _, tc := range cases {
		if got := fixJSON(tc.in); got != tc.want {
			t.Errorf("fixJSON(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeToolID(t *testing.T) {
	if got := sanitizeToolID("call_1-AB"); got != "call_1-AB" {
		t.Errorf("valid id changed: %q", got)
	}
	if got := sanitizeToolID("a/b!c d"); got != "a_b_c_d" {
		t.Errorf("sanitize = %q, want a_b_c_d", got)
	}
	if got := sanitizeToolID(""); !strings.HasPrefix(got, "toolu_") {
		t.Errorf("empty id = %q, want generated toolu_", got)
	}
}

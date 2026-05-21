package benchmark

import (
	"strings"
	"testing"
)

func TestExtractDiff(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
		marker  string
	}{
		{
			name:    "fenced diff block",
			content: "Here:\n```diff\ndiff --git a/x b/x\n--- a/x\n+++ b/x\n```\ntrailing",
			want:    true,
			marker:  "diff --git",
		},
		{
			name:    "bare diff fallback",
			content: "no fence\ndiff --git a/y b/y\n--- a/y\n+++ b/y\n@@\n-a\n+b\n",
			want:    true,
			marker:  "diff --git a/y",
		},
		{
			name:    "plain fence with diff markers",
			content: "```\n--- a/z\n+++ b/z\n@@\n-1\n+2\n```",
			want:    true,
			marker:  "--- a/z",
		},
		{
			name:    "no diff at all",
			content: "I cannot help with that.",
			want:    false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExtractDiff(tc.content)
			if ok != tc.want {
				t.Fatalf("ok = %v, want %v (got %q)", ok, tc.want, got)
			}
			if tc.want && !strings.Contains(got, tc.marker) {
				t.Errorf("diff %q missing marker %q", got, tc.marker)
			}
		})
	}
}

func TestBuildPrompt_OracleFormat(t *testing.T) {
	p := Problem{
		RepoName:     "sympy/sympy",
		Statement:    "is_zero recursion bug",
		ContextFiles: map[string]string{"sympy/core/x.py": "def f():\n    return 1\n"},
	}
	msgs := BuildPrompt(p)
	if len(msgs) != 1 || msgs[0].Role != "user" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	c := msgs[0].Content
	for _, want := range []string{"<issue>", "is_zero recursion bug", "<code>", "[start of sympy/core/x.py]", "1 def f():", "[end of sympy/core/x.py]", "<patch>", "Respond below:"} {
		if !strings.Contains(c, want) {
			t.Errorf("oracle prompt missing %q", want)
		}
	}
}

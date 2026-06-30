package httpproxy

import (
	"strings"
	"testing"
)

func TestMirrorReasoningIntoEmptyContent(t *testing.T) {
	in := `{"choices":[{"message":{"content":"","reasoning_content":"think"}}]}`
	out, ok := mirrorReasoningIntoEmptyContent([]byte(in))
	if !ok {
		t.Fatal("expected change")
	}
	if !strings.Contains(string(out), `"content":"think"`) {
		t.Fatalf("out=%s", out)
	}
}

func TestMirrorReasoningIntoEmptyContent_NoOpWhenContentSet(t *testing.T) {
	in := `{"choices":[{"message":{"content":"ok","reasoning_content":"think"}}]}`
	_, ok := mirrorReasoningIntoEmptyContent([]byte(in))
	if ok {
		t.Fatal("expected no change")
	}
}
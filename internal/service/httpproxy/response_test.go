package httpproxy

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestMirrorReasoningIntoEmptyContent(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantOK   bool
		wantSubs []string // substrings that must appear in the patched output
	}{
		{
			name:     "stop empty content reasoning_content mirrors (backward compat)",
			in:       `{"choices":[{"finish_reason":"stop","message":{"content":"","reasoning_content":"think"}}]}`,
			wantOK:   true,
			wantSubs: []string{`"content":"think"`},
		},
		{
			name:     "stop empty content reasoning mirrors (vLLM 0.24 field)",
			in:       `{"choices":[{"finish_reason":"stop","message":{"content":"","reasoning":"think"}}]}`,
			wantOK:   true,
			wantSubs: []string{`"content":"think"`},
		},
		{
			name:   "stop content already set is no-op",
			in:     `{"choices":[{"finish_reason":"stop","message":{"content":"ok","reasoning_content":"think"}}]}`,
			wantOK: false,
		},
		{
			name:     "length truncated reasoning now mirrors (client compat)",
			in:       `{"choices":[{"finish_reason":"length","message":{"content":"","reasoning":"partial"}}]}`,
			wantOK:   true,
			wantSubs: []string{`"content":"partial"`, `"finish_reason":"length"`},
		},
		{
			name:     "missing finish_reason still mirrors reasoning-only",
			in:       `{"choices":[{"message":{"content":"","reasoning":"think"}}]}`,
			wantOK:   true,
			wantSubs: []string{`"content":"think"`},
		},
		{
			name:   "tool-call turn with empty content is not mirrored",
			in:     `{"choices":[{"finish_reason":"tool_calls","message":{"content":"","reasoning":"think","tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]}}]}`,
			wantOK: false,
		},
		{
			name:     "stop null content reasoning mirrors (real vLLM shape)",
			in:       `{"choices":[{"finish_reason":"stop","message":{"content":null,"reasoning":"think"}}]}`,
			wantOK:   true,
			wantSubs: []string{`"content":"think"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, ok := mirrorReasoningIntoEmptyContent([]byte(tt.in))
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (out=%s)", ok, tt.wantOK, out)
			}
			if !tt.wantOK {
				// no-op: body returned unchanged
				if string(out) != tt.in {
					t.Fatalf("expected unchanged body, got %s", out)
				}
				return
			}
			for _, sub := range tt.wantSubs {
				if !strings.Contains(string(out), sub) {
					t.Fatalf("missing %q in out=%s", sub, out)
				}
			}
		})
	}
}

func TestMirrorReasoningIntoEmptyContent_PrefersReasoningContent(t *testing.T) {
	// When both fields are present, reasoning_content wins.
	in := `{"choices":[{"finish_reason":"stop","message":{"content":"","reasoning":"newer","reasoning_content":"older"}}]}`
	out, ok := mirrorReasoningIntoEmptyContent([]byte(in))
	if !ok {
		t.Fatal("expected change")
	}
	if !strings.Contains(string(out), `"content":"older"`) {
		t.Fatalf("expected reasoning_content to win, out=%s", out)
	}
}

func TestWrapChatCompletionResponseBody_OversizedStreamsUntruncated(t *testing.T) {
	const maxBytes = 64
	raw := []byte(strings.Repeat("z", maxBytes+10))
	rc, n, err := wrapChatCompletionResponseBody(io.NopCloser(bytes.NewReader(raw)), maxBytes)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if n != -1 {
		t.Errorf("n = %d, want -1 (pass-through)", n)
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("read len %d, want original len %d (untruncated)", len(got), len(raw))
	}
	_ = rc.Close()
}

package httpproxy

import (
	"encoding/json"
	"testing"
)

func TestOAIRespMessage_UnmarshalAcceptsReasoningAliases(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"reasoning_content", `{"content":"answer","reasoning_content":"older"}`, "older"},
		{"reasoning", `{"content":"answer","reasoning":"newer"}`, "newer"},
		{"reasoning_content wins when both non-empty", `{"content":"answer","reasoning_content":"older","reasoning":"newer"}`, "older"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got oaiRespMessage
			if err := json.Unmarshal([]byte(tt.raw), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.ReasoningContent != tt.want {
				t.Fatalf("ReasoningContent = %q, want %q", got.ReasoningContent, tt.want)
			}
		})
	}
}

func TestOAIStreamDelta_UnmarshalAcceptsReasoningAliases(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"reasoning_content", `{"reasoning_content":"older","content":"answer"}`, "older"},
		{"reasoning", `{"reasoning":"newer","content":"answer"}`, "newer"},
		{"reasoning_content wins when both non-empty", `{"reasoning_content":"older","reasoning":"newer"}`, "older"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got oaiStreamDelta
			if err := json.Unmarshal([]byte(tt.raw), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.ReasoningContent != tt.want {
				t.Fatalf("ReasoningContent = %q, want %q", got.ReasoningContent, tt.want)
			}
		})
	}
}

func TestOAIRespMessage_UnmarshalPreservesContentAndToolCalls(t *testing.T) {
	var got oaiRespMessage
	raw := `{"content":"hi","reasoning_content":"why","tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]}`
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Content != "hi" || got.ReasoningContent != "why" || len(got.ToolCalls) != 1 || got.ToolCalls[0].Function.Name != "f" {
		t.Fatalf("decoded = %#v, want content/reasoning/tool_calls preserved", got)
	}
}

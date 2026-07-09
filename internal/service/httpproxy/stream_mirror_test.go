package httpproxy

import (
	"io"
	"strings"
	"testing"
)

func runReasoningStream(t *testing.T, sse string) string {
	t.Helper()
	rc := mirrorReasoningStream(io.NopCloser(strings.NewReader(sse)))
	out, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read mirrored stream: %v", err)
	}
	_ = rc.Close()
	return string(out)
}

func TestMirrorReasoningStream_ReasoningOnlyMirrored(t *testing.T) {
	sse := `data: {"id":"x","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"reasoning_content":"why"}}]}` + "\n\n" +
		`data: {"id":"x","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	out := runReasoningStream(t, sse)
	if !strings.Contains(out, `"content":"why"`) {
		t.Fatalf("missing mirrored content delta: %s", out)
	}
	// The synthetic content delta must precede the finishing chunk.
	ci := strings.Index(out, `"content":"why"`)
	fi := strings.Index(out, `"finish_reason":"length"`)
	if ci < 0 || fi < 0 || ci > fi {
		t.Fatalf("content delta must precede finish, out=%s", out)
	}
	if !strings.Contains(out, "[DONE]") {
		t.Fatalf("DONE terminator not preserved: %s", out)
	}
	// The original reasoning frame is passed through untouched.
	if !strings.Contains(out, `"reasoning_content":"why"`) {
		t.Fatalf("original reasoning frame lost: %s", out)
	}
}

func TestMirrorReasoningStream_VLLMReasoningAliasMirrored(t *testing.T) {
	sse := `data: {"id":"x","model":"m","choices":[{"index":0,"delta":{"reasoning":"why"}}]}` + "\n\n" +
		`data: {"id":"x","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	out := runReasoningStream(t, sse)
	if !strings.Contains(out, `"content":"why"`) {
		t.Fatalf("vLLM reasoning alias not mirrored: %s", out)
	}
}

func TestMirrorReasoningStream_ContentPassthrough(t *testing.T) {
	sse := `data: {"id":"x","model":"m","choices":[{"index":0,"delta":{"content":"hi"}}]}` + "\n\n" +
		`data: {"id":"x","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	out := runReasoningStream(t, sse)
	if out != sse {
		t.Fatalf("content stream must pass through unchanged:\n got=%q\nwant=%q", out, sse)
	}
}

func TestMirrorReasoningStream_ReasoningThenContentNoInject(t *testing.T) {
	sse := `data: {"id":"x","model":"m","choices":[{"index":0,"delta":{"reasoning":"why"}}]}` + "\n\n" +
		`data: {"id":"x","model":"m","choices":[{"index":0,"delta":{"content":"answer"}}]}` + "\n\n" +
		`data: {"id":"x","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	out := runReasoningStream(t, sse)
	if out != sse {
		t.Fatalf("reasoning+content stream must pass through unchanged: %s", out)
	}
}

func TestMirrorReasoningStream_ToolCallNotMirrored(t *testing.T) {
	sse := `data: {"id":"x","model":"m","choices":[{"index":0,"delta":{"reasoning":"why","tool_calls":[{"index":0,"id":"c1","function":{"name":"f","arguments":"{}"}}]}}]}` + "\n\n" +
		`data: {"id":"x","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	out := runReasoningStream(t, sse)
	if strings.Contains(out, `"content":"why"`) {
		t.Fatalf("tool-call stream must not mirror reasoning into content: %s", out)
	}
}

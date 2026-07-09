package httpproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	mllog "github.com/quantmind-br/model-loader/internal/log"
)

type sseFrame struct {
	Event string
	Data  map[string]any
}

// parseSSE decodes an Anthropic SSE body into (event, payload) frames.
// json.Marshal never emits raw newlines, so "\n\n" is a safe frame separator.
func parseSSE(t *testing.T, body string) []sseFrame {
	t.Helper()
	var frames []sseFrame
	for blk := range strings.SplitSeq(body, "\n\n") {
		blk = strings.TrimSpace(blk)
		if blk == "" {
			continue
		}
		lines := strings.SplitN(blk, "\n", 2)
		if len(lines) != 2 || !strings.HasPrefix(lines[0], "event: ") || !strings.HasPrefix(lines[1], "data: ") {
			t.Fatalf("malformed SSE frame: %q", blk)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &m); err != nil {
			t.Fatalf("frame data is not JSON: %v (%q)", err, blk)
		}
		frames = append(frames, sseFrame{Event: strings.TrimPrefix(lines[0], "event: "), Data: m})
	}
	return frames
}

func assertEventSequence(t *testing.T, frames []sseFrame, want []string) {
	t.Helper()
	got := make([]string, 0, len(frames))
	for _, f := range frames {
		got = append(got, f.Event)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("event sequence =\n  %v\nwant\n  %v", got, want)
	}
}

func runStream(t *testing.T, script string) (*httptest.ResponseRecorder, []sseFrame, error) {
	t.Helper()
	rec := httptest.NewRecorder()
	err := runAnthropicStream(context.Background(), rec, strings.NewReader(script), "alpha", mllog.Nop())
	return rec, parseSSE(t, rec.Body.String()), err
}

func sseScript(chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString("data: ")
		b.WriteString(c)
		b.WriteString("\n\n")
	}
	return b.String()
}

func TestAnthropicStream_TextHappyPath(t *testing.T) {
	rec, frames, err := runStream(t, sseScript(
		`{"choices":[{"delta":{"role":"assistant"}}]}`,
		`{"choices":[{"delta":{"content":"Hel"}}]}`,
		`{"choices":[{"delta":{"content":"lo"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":2}}`,
		`[DONE]`,
	))
	if err != nil {
		t.Fatalf("runAnthropicStream: %v", err)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	assertEventSequence(t, frames, []string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	})

	msg, _ := frames[0].Data["message"].(map[string]any)
	if msg == nil {
		t.Fatal("message_start missing message skeleton")
	}
	if msg["model"] != "alpha" || msg["role"] != "assistant" || msg["type"] != "message" {
		t.Errorf("skeleton = %#v", msg)
	}
	if msg["stop_reason"] != nil {
		t.Errorf("skeleton stop_reason = %v, want null", msg["stop_reason"])
	}
	if content, ok := msg["content"].([]any); !ok || len(content) != 0 {
		t.Errorf("skeleton content = %#v, want []", msg["content"])
	}
	if id, _ := msg["id"].(string); !strings.HasPrefix(id, "msg_") {
		t.Errorf("skeleton id = %v, want msg_ prefix", msg["id"])
	}

	cb, _ := frames[2].Data["content_block"].(map[string]any)
	if cb == nil || cb["type"] != "text" {
		t.Fatalf("content_block_start = %#v, want text block", frames[2].Data)
	}
	if text, present := cb["text"]; !present || text != "" {
		t.Errorf(`content_block_start must carry {"text":""} explicitly, got %#v`, cb)
	}
	if idx, _ := frames[2].Data["index"].(float64); idx != 0 {
		t.Errorf("first block index = %v, want 0", frames[2].Data["index"])
	}

	d1, _ := frames[3].Data["delta"].(map[string]any)
	d2, _ := frames[4].Data["delta"].(map[string]any)
	if d1["type"] != "text_delta" || d1["text"] != "Hel" || d2["text"] != "lo" {
		t.Errorf("text deltas = %#v / %#v", d1, d2)
	}

	md := frames[6].Data
	delta, _ := md["delta"].(map[string]any)
	if delta["stop_reason"] != "end_turn" {
		t.Errorf("stop_reason = %v, want end_turn", delta["stop_reason"])
	}
	usage, _ := md["usage"].(map[string]any)
	if usage["input_tokens"] != float64(7) || usage["output_tokens"] != float64(2) {
		t.Errorf("usage = %#v, want 7/2 from the include_usage final chunk", usage)
	}
}

func TestAnthropicStream_ReasoningThenText(t *testing.T) {
	_, frames, err := runStream(t, sseScript(
		`{"choices":[{"delta":{"reasoning_content":"think"}}]}`,
		`{"choices":[{"delta":{"content":"answer"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`[DONE]`,
	))
	if err != nil {
		t.Fatalf("runAnthropicStream: %v", err)
	}
	assertEventSequence(t, frames, []string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	})
	cb0, _ := frames[2].Data["content_block"].(map[string]any)
	if cb0["type"] != "thinking" {
		t.Errorf("block 0 = %#v, want thinking", cb0)
	}
	d0, _ := frames[3].Data["delta"].(map[string]any)
	if d0["type"] != "thinking_delta" || d0["thinking"] != "think" {
		t.Errorf("thinking delta = %#v", d0)
	}
	cb1, _ := frames[5].Data["content_block"].(map[string]any)
	if cb1["type"] != "text" {
		t.Errorf("block 1 = %#v, want text", cb1)
	}
	if idx, _ := frames[5].Data["index"].(float64); idx != 1 {
		t.Errorf("second block index = %v, want 1", frames[5].Data["index"])
	}
}

func TestAnthropicStream_ToolCalls(t *testing.T) {
	_, frames, err := runStream(t, sseScript(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`[DONE]`,
	))
	if err != nil {
		t.Fatalf("runAnthropicStream: %v", err)
	}
	// Tool calls accumulate and are emitted whole at finish: ONE input_json_delta
	// carrying the concatenated arguments.
	assertEventSequence(t, frames, []string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	})
	cb, _ := frames[2].Data["content_block"].(map[string]any)
	if cb["type"] != "tool_use" || cb["id"] != "call_1" || cb["name"] != "get_weather" {
		t.Errorf("tool_use start = %#v", cb)
	}
	if input, ok := cb["input"].(map[string]any); !ok || len(input) != 0 {
		t.Errorf("tool_use start input = %#v, want {}", cb["input"])
	}
	d, _ := frames[3].Data["delta"].(map[string]any)
	if d["type"] != "input_json_delta" || d["partial_json"] != `{"q":1}` {
		t.Errorf("input_json delta = %#v, want concatenated {\"q\":1}", d)
	}
	delta, _ := frames[5].Data["delta"].(map[string]any)
	if delta["stop_reason"] != "tool_use" {
		t.Errorf("stop_reason = %v, want tool_use", delta["stop_reason"])
	}
}

func TestAnthropicStream_ToolCallIDAndNameSplitAcrossChunks(t *testing.T) {
	// id arrives in chunk 1, name in chunk 2, single-quoted args in 2-3. The
	// accumulator must open the block with BOTH id and name, and fixJSON must
	// repair the arguments.
	_, frames, err := runStream(t, sseScript(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_9","function":{"arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"do_thing","arguments":"{'k':"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":" 1}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`[DONE]`,
	))
	if err != nil {
		t.Fatalf("runAnthropicStream: %v", err)
	}
	cb, _ := frames[2].Data["content_block"].(map[string]any)
	if cb["type"] != "tool_use" || cb["id"] != "call_9" || cb["name"] != "do_thing" {
		t.Errorf("tool_use start = %#v, want id=call_9 name=do_thing from split chunks", cb)
	}
	d, _ := frames[3].Data["delta"].(map[string]any)
	if d["partial_json"] != `{"k": 1}` {
		t.Errorf("partial_json = %v, want fixJSON-repaired {\"k\": 1}", d["partial_json"])
	}
}

func TestAnthropicStream_ToolCallSanitizesID(t *testing.T) {
	_, frames, err := runStream(t, sseScript(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"bad/id!","function":{"name":"x","arguments":"{}"}}]}}]}`,
		`[DONE]`,
	))
	if err != nil {
		t.Fatalf("runAnthropicStream: %v", err)
	}
	cb, _ := frames[2].Data["content_block"].(map[string]any)
	if cb["id"] != "bad_id_" {
		t.Errorf("id = %v, want sanitized bad_id_", cb["id"])
	}
}

func TestAnthropicStream_ParallelToolCallsInOneDelta(t *testing.T) {
	_, frames, err := runStream(t, sseScript(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c0","function":{"name":"a","arguments":"{}"}},{"index":1,"id":"c1","function":{"name":"b","arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`[DONE]`,
	))
	if err != nil {
		t.Fatalf("runAnthropicStream: %v", err)
	}
	assertEventSequence(t, frames, []string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	})
	cb0, _ := frames[2].Data["content_block"].(map[string]any)
	cb1, _ := frames[5].Data["content_block"].(map[string]any)
	if cb0["id"] != "c0" || cb1["id"] != "c1" {
		t.Errorf("parallel tool blocks = %#v / %#v", cb0, cb1)
	}
	if i0, _ := frames[2].Data["index"].(float64); i0 != 0 {
		t.Errorf("block 0 index = %v", frames[2].Data["index"])
	}
	if i1, _ := frames[5].Data["index"].(float64); i1 != 1 {
		t.Errorf("block 1 index = %v", frames[5].Data["index"])
	}
}

func TestAnthropicStream_TextAfterToolCall(t *testing.T) {
	_, frames, err := runStream(t, sseScript(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c0","function":{"name":"a","arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{"content":"done"}}]}`,
		`[DONE]`,
	))
	if err != nil {
		t.Fatalf("runAnthropicStream: %v", err)
	}
	// Text streams incrementally first; the accumulated tool block is emitted at
	// finish, AFTER the text block. stop_reason is tool_use (a tool was called,
	// even though the backend sent no finish_reason).
	assertEventSequence(t, frames, []string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	})
	cb0, _ := frames[2].Data["content_block"].(map[string]any)
	if cb0["type"] != "text" {
		t.Errorf("block 0 = %#v, want text (streams first)", cb0)
	}
	cb1, _ := frames[5].Data["content_block"].(map[string]any)
	if cb1["type"] != "tool_use" || cb1["name"] != "a" {
		t.Errorf("block 1 = %#v, want the accumulated tool_use", cb1)
	}
	delta, _ := frames[8].Data["delta"].(map[string]any)
	if delta["stop_reason"] != "tool_use" {
		t.Errorf("stop_reason = %v, want tool_use (tool call present)", delta["stop_reason"])
	}
}

func TestAnthropicStream_MissingUsageFallbackEstimate(t *testing.T) {
	_, frames, err := runStream(t, sseScript(
		`{"choices":[{"delta":{"content":"abcdefgh"}}]}`,
		`[DONE]`,
	))
	if err != nil {
		t.Fatalf("runAnthropicStream: %v", err)
	}
	if len(frames) < 2 {
		t.Fatalf("frames = %v, want at least message_delta + message_stop", frames)
	}
	md := frames[len(frames)-2]
	if md.Event != "message_delta" {
		t.Fatalf("frame[-2] = %s, want message_delta", md.Event)
	}
	usage, _ := md.Data["usage"].(map[string]any)
	if usage["output_tokens"] != float64(2) { // ceil(8/4)
		t.Errorf("fallback output_tokens = %v, want 2", usage["output_tokens"])
	}
}

func TestAnthropicStream_EmptyDeltasSkipped(t *testing.T) {
	_, frames, err := runStream(t, sseScript(
		`{"choices":[{"delta":{"role":"assistant"}}]}`,
		`{"choices":[{"delta":{"content":""}}]}`,
		`{"choices":[{"delta":{"content":"hi"}}]}`,
		`[DONE]`,
	))
	if err != nil {
		t.Fatalf("runAnthropicStream: %v", err)
	}
	assertEventSequence(t, frames, []string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	})
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

func TestAnthropicStream_UpstreamAbortEmitsErrorEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	upstream := io.MultiReader(
		strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"par\"}}]}\n\n"),
		failingReader{err: errors.New("boom")},
	)
	err := runAnthropicStream(context.Background(), rec, upstream, "alpha", mllog.Nop())
	if err == nil {
		t.Fatal("want error from aborted upstream")
	}
	frames := parseSSE(t, rec.Body.String())
	last := frames[len(frames)-1]
	if last.Event != "error" {
		t.Fatalf("last frame = %s, want error event", last.Event)
	}
	if last.Data["type"] != "error" {
		t.Errorf("error payload type = %v", last.Data["type"])
	}
	inner, _ := last.Data["error"].(map[string]any)
	if inner["type"] != "api_error" || !strings.Contains(inner["message"].(string), "boom") {
		t.Errorf("error payload = %#v", inner)
	}
	for _, f := range frames {
		if f.Event == "message_stop" {
			t.Error("message_stop must not follow an error frame")
		}
	}
}

func TestAnthropicStream_ClientCancelIsSilent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	err := runAnthropicStream(ctx, rec, failingReader{err: errors.New("read aborted")}, "alpha", mllog.Nop())
	if err == nil {
		t.Fatal("want ctx error")
	}
	for _, f := range parseSSE(t, rec.Body.String()) {
		if f.Event == "error" || f.Event == "message_stop" {
			t.Errorf("client disconnect must not emit %s", f.Event)
		}
	}
}

func TestSSEWriter_FrameFormat(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := newSSEWriter(rec)
	if err := sw.writeEvent("ping", ssePing{Type: "ping"}); err != nil {
		t.Fatalf("writeEvent: %v", err)
	}
	want := "event: ping\ndata: {\"type\":\"ping\"}\n\n"
	if rec.Body.String() != want {
		t.Errorf("frame = %q, want %q", rec.Body.String(), want)
	}
}

func TestSynthesizeAnthropicStream(t *testing.T) {
	stop := "tool_use"
	resp := &anthropicMessageResponse{
		ID: "msg_x", Type: "message", Role: "assistant", Model: "alpha",
		Content: []any{
			anthropicThinkingBlock{Type: "thinking", Thinking: "hmm"},
			anthropicTextBlock{Type: "text", Text: "hello"},
			anthropicToolUseBlock{Type: "tool_use", ID: "c1", Name: "get", Input: map[string]any{"a": float64(1)}},
		},
		StopReason: &stop,
		Usage:      anthropicUsage{InputTokens: 3, OutputTokens: 4},
	}
	rec := httptest.NewRecorder()
	if err := synthesizeAnthropicStream(newSSEWriter(rec), resp); err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	frames := parseSSE(t, rec.Body.String())
	assertEventSequence(t, frames, []string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	})
	d0, _ := frames[3].Data["delta"].(map[string]any)
	if d0["type"] != "thinking_delta" || d0["thinking"] != "hmm" {
		t.Errorf("delta 0 = %#v", d0)
	}
	d1, _ := frames[6].Data["delta"].(map[string]any)
	if d1["type"] != "text_delta" || d1["text"] != "hello" {
		t.Errorf("delta 1 = %#v", d1)
	}
	d2, _ := frames[9].Data["delta"].(map[string]any)
	if d2["type"] != "input_json_delta" || d2["partial_json"] != `{"a":1}` {
		t.Errorf("delta 2 = %#v", d2)
	}
	md, _ := frames[11].Data["delta"].(map[string]any)
	if md["stop_reason"] != "tool_use" {
		t.Errorf("stop_reason = %v", md["stop_reason"])
	}
	usage, _ := frames[11].Data["usage"].(map[string]any)
	if usage["input_tokens"] != float64(3) || usage["output_tokens"] != float64(4) {
		t.Errorf("usage = %#v", usage)
	}
}

func TestAnthropicStream_AcceptsVLLMReasoningField(t *testing.T) {
	_, frames, err := runStream(t, sseScript(
		`{"choices":[{"delta":{"reasoning":"think"}}]}`,
		`{"choices":[{"delta":{"content":"answer"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`[DONE]`,
	))
	if err != nil {
		t.Fatalf("runAnthropicStream: %v", err)
	}
	assertEventSequence(t, frames, []string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	})
	cb0, _ := frames[2].Data["content_block"].(map[string]any)
	if cb0["type"] != "thinking" {
		t.Errorf("block 0 = %#v, want thinking", cb0)
	}
	d0, _ := frames[3].Data["delta"].(map[string]any)
	if d0["type"] != "thinking_delta" || d0["thinking"] != "think" {
		t.Errorf("thinking delta = %#v", d0)
	}
	cb1, _ := frames[5].Data["content_block"].(map[string]any)
	if cb1["type"] != "text" {
		t.Errorf("block 1 = %#v, want text", cb1)
	}
}

func TestAnthropicStream_MirrorsReasoningOnlyAsText(t *testing.T) {
	_, frames, err := runStream(t, sseScript(
		`{"choices":[{"delta":{"reasoning":"think"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"length"}]}`,
		`[DONE]`,
	))
	if err != nil {
		t.Fatalf("runAnthropicStream: %v", err)
	}
	assertEventSequence(t, frames, []string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	})
	cb1, _ := frames[5].Data["content_block"].(map[string]any)
	if cb1["type"] != "text" {
		t.Fatalf("block 1 = %#v, want mirrored text block", cb1)
	}
	d1, _ := frames[6].Data["delta"].(map[string]any)
	if d1["type"] != "text_delta" || d1["text"] != "think" {
		t.Fatalf("delta 1 = %#v, want mirrored text delta", d1)
	}
	md, _ := frames[8].Data["delta"].(map[string]any)
	if md["stop_reason"] != "max_tokens" {
		t.Fatalf("stop_reason = %#v, want max_tokens", md["stop_reason"])
	}
}

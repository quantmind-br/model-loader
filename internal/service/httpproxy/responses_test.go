package httpproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mllog "github.com/quantmind-br/model-loader/internal/log"
)

func mustTranslateResponses(t *testing.T, raw string) *oaiChatRequest {
	t.Helper()
	var req responsesRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := translateResponsesRequest(&req, "m")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	return out
}

func TestTranslateResponsesRequest_InputStringAndInstructions(t *testing.T) {
	out := mustTranslateResponses(t, `{"model":"m","input":"hello","instructions":"be brief","max_output_tokens":64}`)
	if len(out.Messages) != 2 {
		t.Fatalf("want 2 messages, got %d: %#v", len(out.Messages), out.Messages)
	}
	if out.Messages[0].Role != "system" || out.Messages[0].Content != "be brief" {
		t.Errorf("msg0 = %#v, want system", out.Messages[0])
	}
	if out.Messages[1].Role != "user" || out.Messages[1].Content != "hello" {
		t.Errorf("msg1 = %#v, want user hello", out.Messages[1])
	}
	if out.MaxTokens != 64 {
		t.Errorf("max_tokens = %d, want 64", out.MaxTokens)
	}
}

func TestTranslateResponsesRequest_InputArray(t *testing.T) {
	out := mustTranslateResponses(t, `{"model":"m","input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
		{"type":"function_call","call_id":"c1","name":"get","arguments":"{\"x\":1}"},
		{"type":"function_call_output","call_id":"c1","output":"result"}
	]}`)
	if len(out.Messages) != 3 {
		t.Fatalf("want 3 messages, got %d: %#v", len(out.Messages), out.Messages)
	}
	if out.Messages[0].Role != "user" || out.Messages[0].Content != "hi" {
		t.Errorf("msg0 = %#v, want user hi", out.Messages[0])
	}
	if out.Messages[1].Role != "assistant" || len(out.Messages[1].ToolCalls) != 1 || out.Messages[1].ToolCalls[0].Function.Name != "get" {
		t.Errorf("msg1 = %#v, want assistant tool_call", out.Messages[1])
	}
	if out.Messages[2].Role != "tool" || out.Messages[2].ToolCallID != "c1" || out.Messages[2].Content != "result" {
		t.Errorf("msg2 = %#v, want tool result", out.Messages[2])
	}
}

func TestTranslateResponsesRequest_ToolsAndReasoning(t *testing.T) {
	out := mustTranslateResponses(t, `{"model":"m","input":"x",
		"tools":[{"type":"function","name":"t","description":"d","parameters":{"type":"object"}}],
		"reasoning":{"effort":"high"}}`)
	if len(out.Tools) != 1 || out.Tools[0].Function.Name != "t" || out.Tools[0].Type != "function" {
		t.Errorf("tools = %#v, want one function tool", out.Tools)
	}
	if out.ReasoningEffort != "high" {
		t.Errorf("reasoning_effort = %q, want high", out.ReasoningEffort)
	}
}

func TestBuildResponsesResponse(t *testing.T) {
	oai := &oaiChatResponse{Choices: []oaiChoice{{
		FinishReason: "tool_calls",
		Message: oaiRespMessage{
			ReasoningContent: "thinking",
			Content:          "answer",
			ToolCalls:        []oaiToolCall{{ID: "c1", Function: oaiFunctionCall{Name: "get", Arguments: `{"x":1}`}}},
		},
	}}, Usage: &oaiUsage{PromptTokens: 10, CompletionTokens: 5}}
	resp := buildResponsesResponse(oai, "m", 123)
	if resp.Object != "response" || resp.Status != "completed" || resp.CreatedAt != 123 {
		t.Errorf("meta = %#v", resp)
	}
	if len(resp.Output) != 3 {
		t.Fatalf("output len = %d, want reasoning+message+function_call: %#v", len(resp.Output), resp.Output)
	}
	if r, ok := resp.Output[0].(responsesReasoningItem); !ok || r.Type != "reasoning" {
		t.Errorf("output0 = %#v, want reasoning", resp.Output[0])
	}
	if m, ok := resp.Output[1].(responsesMessageItem); !ok || m.Type != "message" {
		t.Errorf("output1 = %#v, want message", resp.Output[1])
	}
	if f, ok := resp.Output[2].(responsesFunctionCallItem); !ok || f.Name != "get" || f.CallID != "c1" {
		t.Errorf("output2 = %#v, want function_call", resp.Output[2])
	}
	if resp.Usage == nil || resp.Usage.InputTokens != 10 || resp.Usage.TotalTokens != 15 {
		t.Errorf("usage = %#v, want 10/5/15", resp.Usage)
	}
}

func TestResponsesStream_TextThenTool(t *testing.T) {
	rec := httptest.NewRecorder()
	script := sseScript(
		`{"choices":[{"delta":{"content":"Hel"}}]}`,
		`{"choices":[{"delta":{"content":"lo"}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"get","arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2}}`,
		`[DONE]`,
	)
	if err := runResponsesStream(context.Background(), rec, strings.NewReader(script), "alpha", 100, mllog.Nop()); err != nil {
		t.Fatalf("stream: %v", err)
	}
	frames := parseSSE(t, rec.Body.String())
	if frames[0].Event != "response.created" {
		t.Errorf("first event = %s, want response.created", frames[0].Event)
	}
	last := frames[len(frames)-1]
	if last.Event != "response.completed" {
		t.Errorf("last event = %s, want response.completed", last.Event)
	}
	counts := map[string]int{}
	for _, f := range frames {
		counts[f.Event]++
	}
	if counts["response.output_text.delta"] != 2 {
		t.Errorf("output_text.delta = %d, want 2", counts["response.output_text.delta"])
	}
	if counts["response.function_call_arguments.done"] != 1 {
		t.Errorf("function_call_arguments.done = %d, want 1", counts["response.function_call_arguments.done"])
	}
	// completed carries the assembled output + usage
	respObj, _ := last.Data["response"].(map[string]any)
	if respObj == nil || respObj["status"] != "completed" {
		t.Fatalf("completed response = %#v", last.Data)
	}
	out, _ := respObj["output"].([]any)
	if len(out) != 2 { // message + function_call
		t.Errorf("completed output len = %d, want 2", len(out))
	}
	usage, _ := respObj["usage"].(map[string]any)
	if usage == nil || usage["input_tokens"] != float64(3) {
		t.Errorf("completed usage = %#v", respObj["usage"])
	}
}

func TestHandleResponses_NonStream_Integration(t *testing.T) {
	backend, caps := startBackend(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"content":"hi there"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
	})
	_, mux := newAnthropicMux(t, newStubStore(makeProfile("alpha", portOf(t, backend))), newStubManager())

	rr := postJSON(t, mux, "/v1/responses", `{"model":"alpha","instructions":"be nice","input":"hello"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
	}
	cap := <-caps
	if cap.Path != "/v1/chat/completions" {
		t.Errorf("upstream path = %q, want /v1/chat/completions", cap.Path)
	}
	var up map[string]any
	if err := json.Unmarshal(cap.Body, &up); err != nil {
		t.Fatalf("upstream body: %v", err)
	}
	if msgs, _ := up["messages"].([]any); len(msgs) != 2 {
		t.Errorf("upstream messages = %#v, want system + user", up["messages"])
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["object"] != "response" || resp["status"] != "completed" {
		t.Errorf("response = %#v, want completed response object", resp)
	}
	if output, _ := resp["output"].([]any); len(output) != 1 {
		t.Fatalf("output = %#v, want one message item", resp["output"])
	}
}

func TestBuildResponsesResponse_AcceptsVLLMReasoningFieldFromJSON(t *testing.T) {
	var oai oaiChatResponse
	if err := json.Unmarshal([]byte(`{"choices":[{"finish_reason":"stop","message":{"reasoning":"think","content":"answer"}}]}`), &oai); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resp := buildResponsesResponse(&oai, "m", 123)
	if len(resp.Output) == 0 {
		t.Fatalf("output empty: %#v", resp)
	}
	item, ok := resp.Output[0].(responsesReasoningItem)
	if !ok || item.Type != "reasoning" {
		t.Fatalf("output0 = %#v, want reasoning", resp.Output[0])
	}
	if len(item.Summary) == 0 {
		t.Fatalf("summary empty: %#v", item)
	}
	sum, ok := item.Summary[0].(responsesSummaryText)
	if !ok || sum.Type != "summary_text" || sum.Text != "think" {
		t.Errorf("summary0 = %#v, want summary_text think", item.Summary[0])
	}
}

func TestResponsesStream_AcceptsVLLMReasoningField(t *testing.T) {
	rec := httptest.NewRecorder()
	script := sseScript(
		`{"choices":[{"delta":{"reasoning":"think"}}]}`,
		`{"choices":[{"delta":{"content":"answer"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`[DONE]`,
	)
	if err := runResponsesStream(context.Background(), rec, strings.NewReader(script), "alpha", 100, mllog.Nop()); err != nil {
		t.Fatalf("stream: %v", err)
	}
	frames := parseSSE(t, rec.Body.String())
	reasoningDeltas := 0
	for _, f := range frames {
		if f.Event == "response.reasoning_summary_text.delta" {
			reasoningDeltas++
			if f.Data["delta"] != "think" {
				t.Errorf("reasoning delta = %#v, want think", f.Data["delta"])
			}
		}
	}
	if reasoningDeltas != 1 {
		t.Errorf("reasoning_summary_text.delta count = %d, want 1", reasoningDeltas)
	}
	last := frames[len(frames)-1]
	if last.Event != "response.completed" {
		t.Fatalf("last event = %s, want response.completed", last.Event)
	}
	respObj, _ := last.Data["response"].(map[string]any)
	out, _ := respObj["output"].([]any)
	var hasReasoning, hasMessage bool
	for _, it := range out {
		m, _ := it.(map[string]any)
		switch m["type"] {
		case "reasoning":
			hasReasoning = true
		case "message":
			hasMessage = true
		}
	}
	if !hasReasoning || !hasMessage {
		t.Errorf("completed output = %#v, want reasoning+message", out)
	}
}

// TestResponsesStream_CompletedOutputIncludesReasoning isolates a snapshot
// content-loss bug independent of the reasoning alias: finish() assembled the
// terminal response.completed output from message + tool items only, dropping
// the reasoning item that was streamed incrementally. Uses the canonical
// reasoning_content field so it fails on the finish() gap, not the alias.
func TestResponsesStream_CompletedOutputIncludesReasoning(t *testing.T) {
	rec := httptest.NewRecorder()
	script := sseScript(
		`{"choices":[{"delta":{"reasoning_content":"think"}}]}`,
		`{"choices":[{"delta":{"content":"answer"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`[DONE]`,
	)
	if err := runResponsesStream(context.Background(), rec, strings.NewReader(script), "alpha", 100, mllog.Nop()); err != nil {
		t.Fatalf("stream: %v", err)
	}
	frames := parseSSE(t, rec.Body.String())
	last := frames[len(frames)-1]
	if last.Event != "response.completed" {
		t.Fatalf("last event = %s, want response.completed", last.Event)
	}
	respObj, _ := last.Data["response"].(map[string]any)
	out, _ := respObj["output"].([]any)
	if len(out) != 2 {
		t.Fatalf("completed output len = %d, want reasoning+message: %#v", len(out), out)
	}
	r0, _ := out[0].(map[string]any)
	if r0["type"] != "reasoning" {
		t.Errorf("output0 = %#v, want reasoning", out[0])
	}
	if m1, _ := out[1].(map[string]any); m1["type"] != "message" {
		t.Errorf("output1 = %#v, want message", out[1])
	}
	summary, _ := r0["summary"].([]any)
	if len(summary) == 0 {
		t.Fatalf("reasoning summary empty: %#v", r0)
	}
	if s0, _ := summary[0].(map[string]any); s0["text"] != "think" {
		t.Errorf("summary text = %#v, want think", s0["text"])
	}
}

func TestBuildResponsesResponse_MirrorsReasoningOnlyAsMessage(t *testing.T) {
	var oai oaiChatResponse
	if err := json.Unmarshal([]byte(`{"choices":[{"finish_reason":"length","message":{"reasoning":"think"}}]}`), &oai); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resp := buildResponsesResponse(&oai, "m", 123)
	if len(resp.Output) != 2 {
		t.Fatalf("output = %#v, want reasoning+message fallback", resp.Output)
	}
	if _, ok := resp.Output[0].(responsesReasoningItem); !ok {
		t.Fatalf("output[0] = %#v, want reasoning", resp.Output[0])
	}
	msg, ok := resp.Output[1].(responsesMessageItem)
	if !ok {
		t.Fatalf("output[1] = %#v, want message", resp.Output[1])
	}
	if len(msg.Content) != 1 {
		t.Fatalf("message content = %#v, want one output_text", msg.Content)
	}
	txt, ok := msg.Content[0].(responsesOutputText)
	if !ok || txt.Text != "think" {
		t.Fatalf("message content[0] = %#v, want mirrored output_text", msg.Content[0])
	}
}

func TestResponsesStream_MirrorsReasoningOnlyAsMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	script := sseScript(
		`{"choices":[{"delta":{"reasoning":"think"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"length"}]}`,
		`[DONE]`,
	)
	if err := runResponsesStream(context.Background(), rec, strings.NewReader(script), "alpha", 100, mllog.Nop()); err != nil {
		t.Fatalf("stream: %v", err)
	}
	frames := parseSSE(t, rec.Body.String())
	var sawTextDelta bool
	for _, f := range frames {
		if f.Event == "response.output_text.delta" && f.Data["delta"] == "think" {
			sawTextDelta = true
		}
	}
	if !sawTextDelta {
		t.Fatalf("no mirrored output_text.delta in frames: %#v", frames)
	}
	last := frames[len(frames)-1]
	if last.Event != "response.completed" {
		t.Fatalf("last event = %s, want response.completed", last.Event)
	}
	respObj, _ := last.Data["response"].(map[string]any)
	out, _ := respObj["output"].([]any)
	if len(out) != 2 {
		t.Fatalf("completed output = %#v, want reasoning+message fallback", out)
	}
	msg, _ := out[1].(map[string]any)
	content, _ := msg["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("message content = %#v, want one output_text", content)
	}
	txt, _ := content[0].(map[string]any)
	if txt["text"] != "think" {
		t.Fatalf("output text = %#v, want mirrored reasoning", txt)
	}
}

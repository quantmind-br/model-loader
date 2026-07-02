package httpproxy

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	mllog "github.com/quantmind-br/model-loader/internal/log"
)

func mustAnthropicReq(t *testing.T, raw string) *anthropicMessagesRequest {
	t.Helper()
	var req anthropicMessagesRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return &req
}

func mustTranslate(t *testing.T, raw string) *oaiChatRequest {
	t.Helper()
	out, aerr := translateAnthropicRequest(mustAnthropicReq(t, raw), "alpha")
	if aerr != nil {
		t.Fatalf("translate: unexpected error %d %s: %s", aerr.Status, aerr.Type, aerr.Message)
	}
	return out
}

func TestTranslateAnthropicRequest_CoreFields(t *testing.T) {
	out := mustTranslate(t, `{
		"model":"whatever-the-client-sent","max_tokens":128,
		"stop_sequences":["a","b"],"temperature":0.7,"top_p":0.9,"top_k":40,
		"messages":[{"role":"user","content":"hi"}]
	}`)

	if out.Model != "alpha" {
		t.Errorf("Model = %q, want resolved profile id %q", out.Model, "alpha")
	}
	if out.MaxTokens != 128 {
		t.Errorf("MaxTokens = %d, want 128", out.MaxTokens)
	}
	if !reflect.DeepEqual(out.Stop, []string{"a", "b"}) {
		t.Errorf("Stop = %#v, want [a b]", out.Stop)
	}
	if out.Temperature == nil || *out.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", out.Temperature)
	}
	if out.TopP == nil || *out.TopP != 0.9 {
		t.Errorf("TopP = %v, want 0.9", out.TopP)
	}
	if out.TopK == nil || *out.TopK != 40 {
		t.Errorf("TopK = %v, want 40", out.TopK)
	}
	if out.Stream || out.StreamOptions != nil {
		t.Errorf("Stream/StreamOptions = %v/%v, want false/nil for non-stream", out.Stream, out.StreamOptions)
	}
	if len(out.Messages) != 1 || out.Messages[0].Role != "user" || out.Messages[0].Content != "hi" {
		t.Errorf("Messages = %#v, want single user 'hi'", out.Messages)
	}

	// Absent sampling params stay nil; stream=true wires include_usage.
	out = mustTranslate(t, `{"model":"m","max_tokens":1,"stream":true,
		"messages":[{"role":"user","content":"x"}]}`)
	if out.Temperature != nil || out.TopP != nil || out.TopK != nil {
		t.Errorf("absent sampling params must stay nil, got %v/%v/%v", out.Temperature, out.TopP, out.TopK)
	}
	if !out.Stream || out.StreamOptions == nil || !out.StreamOptions.IncludeUsage {
		t.Errorf("stream request must set Stream + StreamOptions.IncludeUsage, got %v/%v", out.Stream, out.StreamOptions)
	}
}

func TestTranslateAnthropicRequest_System(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantFirst   string // "" means no system message expected
		wantContent string
	}{
		{
			name:        "string system",
			raw:         `{"model":"m","max_tokens":1,"system":"be terse","messages":[{"role":"user","content":"x"}]}`,
			wantFirst:   "system",
			wantContent: "be terse",
		},
		{
			name:        "block-list system joined",
			raw:         `{"model":"m","max_tokens":1,"system":[{"type":"text","text":"a"},{"type":"text","text":"b"}],"messages":[{"role":"user","content":"x"}]}`,
			wantFirst:   "system",
			wantContent: "a\n\nb",
		},
		{
			name:      "no system",
			raw:       `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":"x"}]}`,
			wantFirst: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := mustTranslate(t, tc.raw)
			if tc.wantFirst == "" {
				if len(out.Messages) == 0 || out.Messages[0].Role != "user" {
					t.Fatalf("want first message user, got %#v", out.Messages)
				}
				return
			}
			if len(out.Messages) < 2 || out.Messages[0].Role != tc.wantFirst {
				t.Fatalf("want leading %s message, got %#v", tc.wantFirst, out.Messages)
			}
			if out.Messages[0].Content != tc.wantContent {
				t.Errorf("system content = %#v, want %q", out.Messages[0].Content, tc.wantContent)
			}
		})
	}
}

func TestTranslateAnthropicRequest_TextBlocksJoinToString(t *testing.T) {
	out := mustTranslate(t, `{"model":"m","max_tokens":1,
		"messages":[{"role":"user","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}]}`)
	if len(out.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(out.Messages))
	}
	if out.Messages[0].Content != "a\n\nb" {
		t.Errorf("content = %#v, want joined string", out.Messages[0].Content)
	}
}

func TestTranslateAnthropicRequest_ImageBlocks(t *testing.T) {
	out := mustTranslate(t, `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[
		{"type":"text","text":"look"},
		{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}},
		{"type":"image","source":{"type":"url","url":"http://x/y.png"}},
		{"type":"text","text":"tail"}
	]}]}`)
	if len(out.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(out.Messages))
	}
	parts, ok := out.Messages[0].Content.([]oaiContentPart)
	if !ok {
		t.Fatalf("content with images must be []oaiContentPart, got %T", out.Messages[0].Content)
	}
	want := []oaiContentPart{
		{Type: "text", Text: "look"},
		{Type: "image_url", ImageURL: &oaiImageURL{URL: "data:image/png;base64,AAAA"}},
		{Type: "image_url", ImageURL: &oaiImageURL{URL: "http://x/y.png"}},
		{Type: "text", Text: "tail"},
	}
	if !reflect.DeepEqual(parts, want) {
		t.Errorf("parts = %#v, want %#v", parts, want)
	}
}

func TestTranslateAnthropicRequest_AssistantToolUse(t *testing.T) {
	out := mustTranslate(t, `{"model":"m","max_tokens":1,"messages":[
		{"role":"user","content":"q"},
		{"role":"assistant","content":[
			{"type":"text","text":"I'll check"},
			{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"q":1}},
			{"type":"tool_use","id":"toolu_2","name":"get_time","input":{"tz":"utc"}},
			{"type":"tool_use","id":"toolu_3","name":"noop"}
		]}
	]}`)
	if len(out.Messages) != 2 {
		t.Fatalf("want 2 messages, got %d: %#v", len(out.Messages), out.Messages)
	}
	as := out.Messages[1]
	if as.Role != "assistant" || as.Content != "I'll check" {
		t.Errorf("assistant message = %#v, want text content preserved", as)
	}
	if len(as.ToolCalls) != 3 {
		t.Fatalf("want 3 tool calls, got %d", len(as.ToolCalls))
	}
	if as.ToolCalls[0].ID != "toolu_1" || as.ToolCalls[0].Type != "function" ||
		as.ToolCalls[0].Function.Name != "get_weather" || as.ToolCalls[0].Function.Arguments != `{"q":1}` {
		t.Errorf("tool call 0 = %#v, want raw input preserved verbatim", as.ToolCalls[0])
	}
	if as.ToolCalls[1].Function.Arguments != `{"tz":"utc"}` {
		t.Errorf("tool call 1 arguments = %q", as.ToolCalls[1].Function.Arguments)
	}
	if as.ToolCalls[2].Function.Arguments != `{}` {
		t.Errorf("tool call without input must get {} arguments, got %q", as.ToolCalls[2].Function.Arguments)
	}
}

func TestTranslateAnthropicRequest_ToolResultSplitOrder(t *testing.T) {
	out := mustTranslate(t, `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[
		{"type":"tool_result","tool_use_id":"t1","content":"ok1"},
		{"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":"ok2"}]},
		{"type":"text","text":"continue"}
	]}]}`)
	if len(out.Messages) != 3 {
		t.Fatalf("want 3 messages (2 tool + 1 user), got %d: %#v", len(out.Messages), out.Messages)
	}
	if out.Messages[0].Role != "tool" || out.Messages[0].ToolCallID != "t1" || out.Messages[0].Content != "ok1" {
		t.Errorf("message 0 = %#v, want tool result t1", out.Messages[0])
	}
	if out.Messages[1].Role != "tool" || out.Messages[1].ToolCallID != "t2" || out.Messages[1].Content != "ok2" {
		t.Errorf("message 1 = %#v, want tool result t2", out.Messages[1])
	}
	if out.Messages[2].Role != "user" || out.Messages[2].Content != "continue" {
		t.Errorf("message 2 = %#v, want trailing user text AFTER tool results", out.Messages[2])
	}
}

func TestTranslateAnthropicRequest_ToolResultContentForms(t *testing.T) {
	out := mustTranslate(t, `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[
		{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]},
		{"type":"tool_result","tool_use_id":"t2","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA"}},{"type":"text","text":"kept"}]},
		{"type":"tool_result","tool_use_id":"t3","is_error":true,"content":"boom"}
	]}]}`)
	if len(out.Messages) != 3 {
		t.Fatalf("want 3 tool messages, got %d: %#v", len(out.Messages), out.Messages)
	}
	if out.Messages[0].Content != "a\n\nb" {
		t.Errorf("multi-text tool_result = %#v, want joined", out.Messages[0].Content)
	}
	if out.Messages[1].Content != "kept" {
		t.Errorf("tool_result with image = %#v, want image dropped and text kept", out.Messages[1].Content)
	}
	if out.Messages[2].Content != "[tool_error] boom" {
		t.Errorf("is_error tool_result = %#v, want error-prefixed", out.Messages[2].Content)
	}
}

func TestTranslateAnthropicRequest_ToolsAndToolChoice(t *testing.T) {
	base := `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":"x"}],
		"tools":[{"name":"get_weather","description":"d","input_schema":{"type":"object"}}]`

	out := mustTranslate(t, base+`}`)
	if len(out.Tools) != 1 {
		t.Fatalf("want 1 tool, got %d", len(out.Tools))
	}
	tool := out.Tools[0]
	if tool.Type != "function" || tool.Function.Name != "get_weather" || tool.Function.Description != "d" {
		t.Errorf("tool = %#v", tool)
	}
	if string(tool.Function.Parameters) != `{"properties":{},"type":"object"}` {
		t.Errorf("parameters = %s, want object schema normalized with properties:{}", tool.Function.Parameters)
	}

	cases := []struct {
		name string
		tc   string
		want any
	}{
		{"auto", `{"type":"auto"}`, "auto"},
		{"any", `{"type":"any"}`, "required"},
		{"none", `{"type":"none"}`, "none"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := mustTranslate(t, base+`,"tool_choice":`+c.tc+`}`)
			if out.ToolChoice != c.want {
				t.Errorf("tool_choice = %#v, want %#v", out.ToolChoice, c.want)
			}
			if out.ParallelToolCalls != nil {
				t.Errorf("ParallelToolCalls = %v, want nil", out.ParallelToolCalls)
			}
		})
	}

	out = mustTranslate(t, base+`,"tool_choice":{"type":"tool","name":"get_weather","disable_parallel_tool_use":true}}`)
	fc, ok := out.ToolChoice.(oaiToolChoiceFunc)
	if !ok {
		t.Fatalf("tool_choice tool = %T, want oaiToolChoiceFunc", out.ToolChoice)
	}
	if fc.Type != "function" || fc.Function.Name != "get_weather" {
		t.Errorf("tool_choice = %#v", fc)
	}
	if out.ParallelToolCalls == nil || *out.ParallelToolCalls != false {
		t.Errorf("disable_parallel_tool_use must map to ParallelToolCalls=false, got %v", out.ParallelToolCalls)
	}
}

func TestTranslateAnthropicRequest_DropsThinkingAndInlinesTextDocument(t *testing.T) {
	out := mustTranslate(t, `{"model":"m","max_tokens":1,"messages":[
		{"role":"assistant","content":[{"type":"thinking","thinking":"secret"},{"type":"redacted_thinking"},{"type":"text","text":"x"}]},
		{"role":"user","content":[{"type":"document","source":{"type":"text","media_type":"text/plain","data":"doc body"}}]}
	]}`)
	if len(out.Messages) != 2 {
		t.Fatalf("want 2 messages, got %d: %#v", len(out.Messages), out.Messages)
	}
	if out.Messages[0].Content != "x" {
		t.Errorf("assistant content = %#v, want thinking blocks dropped", out.Messages[0].Content)
	}
	if s, _ := out.Messages[0].Content.(string); strings.Contains(s, "secret") {
		t.Errorf("thinking text leaked into upstream content: %q", s)
	}
	if out.Messages[1].Content != "doc body" {
		t.Errorf("text document = %#v, want inlined as text", out.Messages[1].Content)
	}
}

// TestTranslateAnthropicRequest_MidConversationSystem covers the
// {"role":"system"} messages Claude Code injects inside the messages array.
// Chat templates (Gemma, Qwen3-MoE, ...) reject any system message that is not
// the sole first one, so each inline system becomes a user message wrapped in
// <system-reminder> AT THE SAME POSITION (following CLIProxyAPI).
func TestTranslateAnthropicRequest_MidConversationSystem(t *testing.T) {
	out := mustTranslate(t, `{"model":"m","max_tokens":1,"messages":[
		{"role":"user","content":"q"},
		{"role":"system","content":"terse mode enabled"},
		{"role":"system","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}
	]}`)
	if len(out.Messages) != 3 {
		t.Fatalf("want 3 messages, got %d: %#v", len(out.Messages), out.Messages)
	}
	if out.Messages[0].Role != "user" || out.Messages[0].Content != "q" {
		t.Errorf("message 0 = %#v, want the user turn", out.Messages[0])
	}
	if out.Messages[1].Role != "user" || out.Messages[1].Content != "<system-reminder>\nterse mode enabled\n</system-reminder>" {
		t.Errorf("message 1 = %#v, want wrapped inline system", out.Messages[1])
	}
	if out.Messages[2].Role != "user" || out.Messages[2].Content != "<system-reminder>\na\n\nb\n</system-reminder>" {
		t.Errorf("message 2 = %#v, want wrapped joined blocks", out.Messages[2])
	}
	for i := range out.Messages {
		if out.Messages[i].Role == "system" {
			t.Errorf("message %d must not be a system role: %#v", i, out.Messages[i])
		}
	}
}

// TestTranslateAnthropicRequest_SystemTopLevelPlusInline is the regression for
// the real Claude Code shape: a top-level `system` PLUS a {"role":"system"}
// reminder after the first user turn. The top-level stays the sole leading
// system message; the inline one becomes an in-position user <system-reminder>.
func TestTranslateAnthropicRequest_SystemTopLevelPlusInline(t *testing.T) {
	out := mustTranslate(t, `{"model":"m","max_tokens":1,
		"system":"MAIN",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"system","content":"REMINDER"}
		]}`)
	if len(out.Messages) != 3 {
		t.Fatalf("want 3 messages, got %d: %#v", len(out.Messages), out.Messages)
	}
	if out.Messages[0].Role != "system" || out.Messages[0].Content != "MAIN" {
		t.Errorf("message 0 = %#v, want sole leading system", out.Messages[0])
	}
	if out.Messages[1].Role != "user" || out.Messages[1].Content != "hi" {
		t.Errorf("message 1 = %#v, want the user turn preserved", out.Messages[1])
	}
	if out.Messages[2].Role != "user" || out.Messages[2].Content != "<system-reminder>\nREMINDER\n</system-reminder>" {
		t.Errorf("message 2 = %#v, want the inline reminder as a user turn", out.Messages[2])
	}
	for i := 1; i < len(out.Messages); i++ {
		if out.Messages[i].Role == "system" {
			t.Errorf("message %d must not be a second system: %#v", i, out.Messages[i])
		}
	}
}

// TestTranslateAnthropicRequest_FiltersBillingHeader drops the Claude Code
// attribution/billing block (x-anthropic-billing-header:) from the system.
func TestTranslateAnthropicRequest_FiltersBillingHeader(t *testing.T) {
	out := mustTranslate(t, `{"model":"m","max_tokens":1,
		"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=1"},{"type":"text","text":"real instructions"}],
		"messages":[{"role":"user","content":"x"}]}`)
	if out.Messages[0].Role != "system" || out.Messages[0].Content != "real instructions" {
		t.Errorf("message 0 = %#v, want only the real system text (billing filtered)", out.Messages[0])
	}
}

// TestTranslateAnthropicRequest_ToolSchemaGetsProperties ensures object schemas
// without a properties key gain properties:{} (strict backends reject them),
// including nested objects.
func TestTranslateAnthropicRequest_ToolSchemaGetsProperties(t *testing.T) {
	out := mustTranslate(t, `{"model":"m","max_tokens":1,
		"tools":[{"name":"t","description":"d","input_schema":{"type":"object","properties":{"nested":{"type":"object"}}}}],
		"messages":[{"role":"user","content":"x"}]}`)
	if len(out.Tools) != 1 {
		t.Fatalf("want 1 tool, got %d", len(out.Tools))
	}
	var schema map[string]any
	if err := json.Unmarshal(out.Tools[0].Function.Parameters, &schema); err != nil {
		t.Fatalf("params: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("root object must have properties, got %v", schema)
	}
	nested, ok := props["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested prop missing: %v", props)
	}
	if _, ok := nested["properties"]; !ok {
		t.Errorf("nested object schema must also gain properties:{}, got %v", nested)
	}
}

func TestTranslateAnthropicRequest_ToolSchemaBareObject(t *testing.T) {
	out := mustTranslate(t, `{"model":"m","max_tokens":1,
		"tools":[{"name":"t","description":"d","input_schema":{"type":"object"}}],
		"messages":[{"role":"user","content":"x"}]}`)
	var schema map[string]any
	if err := json.Unmarshal(out.Tools[0].Function.Parameters, &schema); err != nil {
		t.Fatalf("params: %v", err)
	}
	if _, ok := schema["properties"]; !ok {
		t.Errorf("bare object schema must get properties:{}, got %v", schema)
	}
}

// TestTranslateAnthropicRequest_ToolResultIsError flags a failed tool_result so
// the model sees the failure (Anthropic's is_error has no OpenAI tool-message
// equivalent, so it would otherwise be silently dropped).
func TestTranslateAnthropicRequest_ToolResultIsError(t *testing.T) {
	out := mustTranslate(t, `{"model":"m","max_tokens":1,"messages":[
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"boom","is_error":true}]}
	]}`)
	var tool *oaiChatMessage
	for i := range out.Messages {
		if out.Messages[i].Role == "tool" {
			tool = &out.Messages[i]
			break
		}
	}
	if tool == nil {
		t.Fatal("no tool message emitted")
	}
	if tool.ToolCallID != "t1" {
		t.Errorf("tool_call_id = %q, want t1", tool.ToolCallID)
	}
	if s, _ := tool.Content.(string); s != "[tool_error] boom" {
		t.Errorf("tool content = %q, want error-prefixed", s)
	}
}

func TestTranslateAnthropicRequest_Errors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "unknown role",
			raw:  `{"model":"m","max_tokens":1,"messages":[{"role":"banana","content":"x"}]}`,
		},
		{
			name: "unsupported document source",
			raw:  `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"AAAA"}}]}]}`,
		},
		{
			name: "unknown tool_choice type",
			raw:  `{"model":"m","max_tokens":1,"tool_choice":{"type":"banana"},"messages":[{"role":"user","content":"x"}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, aerr := translateAnthropicRequest(mustAnthropicReq(t, tc.raw), "alpha")
			if aerr == nil {
				t.Fatal("want error, got nil")
			}
			if aerr.Status != 400 || aerr.Type != "invalid_request_error" {
				t.Errorf("error = %d %s, want 400 invalid_request_error", aerr.Status, aerr.Type)
			}
		})
	}
}

func TestBuildAnthropicResponse_TextOnly(t *testing.T) {
	oai := &oaiChatResponse{
		Choices: []oaiChoice{{FinishReason: "stop", Message: oaiRespMessage{Content: "hello"}}},
		Usage:   &oaiUsage{PromptTokens: 10, CompletionTokens: 5},
	}
	resp := buildAnthropicResponse(oai, "alpha-model", mllog.Nop())

	if !strings.HasPrefix(resp.ID, "msg_") || len(resp.ID) < 10 {
		t.Errorf("ID = %q, want msg_ prefix", resp.ID)
	}
	other := buildAnthropicResponse(oai, "alpha-model", mllog.Nop())
	if other.ID == resp.ID {
		t.Errorf("IDs must be unique, got %q twice", resp.ID)
	}
	if resp.Type != "message" || resp.Role != "assistant" || resp.Model != "alpha-model" {
		t.Errorf("envelope = %q/%q/%q", resp.Type, resp.Role, resp.Model)
	}
	if len(resp.Content) != 1 || !reflect.DeepEqual(resp.Content[0], anthropicTextBlock{Type: "text", Text: "hello"}) {
		t.Errorf("content = %#v, want single text block", resp.Content)
	}
	if resp.StopReason == nil || *resp.StopReason != "end_turn" {
		t.Errorf("stop_reason = %v, want end_turn", resp.StopReason)
	}
	if resp.StopSequence != nil {
		t.Errorf("stop_sequence = %v, want nil", resp.StopSequence)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 {
		t.Errorf("usage = %+v, want 10/5", resp.Usage)
	}

	empty := buildAnthropicResponse(&oaiChatResponse{}, "alpha-model", mllog.Nop())
	if len(empty.Content) != 0 || empty.StopReason == nil || *empty.StopReason != "end_turn" {
		t.Errorf("empty choices must yield empty content + end_turn, got %#v", empty)
	}
}

func TestBuildAnthropicResponse_ReasoningThenText(t *testing.T) {
	oai := &oaiChatResponse{Choices: []oaiChoice{{
		FinishReason: "stop",
		Message:      oaiRespMessage{Content: "answer", ReasoningContent: "hmm"},
	}}}
	resp := buildAnthropicResponse(oai, "m", mllog.Nop())
	want := []any{
		anthropicThinkingBlock{Type: "thinking", Thinking: "hmm"},
		anthropicTextBlock{Type: "text", Text: "answer"},
	}
	if !reflect.DeepEqual(resp.Content, want) {
		t.Errorf("content = %#v, want thinking then text", resp.Content)
	}
}

func TestBuildAnthropicResponse_ToolCalls(t *testing.T) {
	oai := &oaiChatResponse{Choices: []oaiChoice{{
		FinishReason: "tool_calls",
		Message: oaiRespMessage{ToolCalls: []oaiToolCall{
			{ID: "tc1", Type: "function", Function: oaiFunctionCall{Name: "get", Arguments: `{"a":2}`}},
			{ID: "tc2", Type: "function", Function: oaiFunctionCall{Name: "bad", Arguments: `not json`}},
		}},
	}}}
	resp := buildAnthropicResponse(oai, "m", mllog.Nop())
	if len(resp.Content) != 2 {
		t.Fatalf("content = %#v, want 2 tool_use blocks", resp.Content)
	}
	tu, ok := resp.Content[0].(anthropicToolUseBlock)
	if !ok || tu.Type != "tool_use" || tu.ID != "tc1" || tu.Name != "get" {
		t.Fatalf("block 0 = %#v", resp.Content[0])
	}
	if !reflect.DeepEqual(tu.Input, map[string]any{"a": float64(2)}) {
		t.Errorf("input = %#v, want parsed object", tu.Input)
	}
	bad, ok := resp.Content[1].(anthropicToolUseBlock)
	if !ok || bad.Input == nil || len(bad.Input) != 0 {
		t.Errorf("malformed arguments must yield empty input map, got %#v", resp.Content[1])
	}
	if resp.StopReason == nil || *resp.StopReason != "tool_use" {
		t.Errorf("stop_reason = %v, want tool_use", resp.StopReason)
	}
}

func TestBuildAnthropicResponse_ToolArgsRepairedAndIDSanitized(t *testing.T) {
	oai := &oaiChatResponse{Choices: []oaiChoice{{
		FinishReason: "tool_calls",
		Message: oaiRespMessage{ToolCalls: []oaiToolCall{
			{ID: "bad id!", Function: oaiFunctionCall{Name: "get", Arguments: `{'city': 'SP'}`}},
		}},
	}}}
	resp := buildAnthropicResponse(oai, "m", mllog.Nop())
	tu, ok := resp.Content[0].(anthropicToolUseBlock)
	if !ok {
		t.Fatalf("block 0 = %#v", resp.Content[0])
	}
	if tu.ID != "bad_id_" {
		t.Errorf("id = %q, want sanitized bad_id_", tu.ID)
	}
	if !reflect.DeepEqual(tu.Input, map[string]any{"city": "SP"}) {
		t.Errorf("input = %#v, want fixJSON-repaired {city:SP}", tu.Input)
	}
}

func TestMapFinishReason(t *testing.T) {
	cases := map[string]string{
		"stop":           "end_turn",
		"length":         "max_tokens",
		"tool_calls":     "tool_use",
		"":               "end_turn",
		"content_filter": "end_turn",
	}
	for in, want := range cases {
		if got := mapFinishReason(in); got != want {
			t.Errorf("mapFinishReason(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEstimateAnthropicTokens_Deterministic(t *testing.T) {
	// system "abcd" (4 bytes) + content "12345678" (8 bytes) = 12 bytes → ceil(12/4)=3,
	// plus 5 per message → 8.
	base := `{"model":"m","system":"abcd","messages":[{"role":"user","content":"12345678"}]}`
	got := estimateAnthropicTokens(mustAnthropicReq(t, base))
	if got != 8 {
		t.Errorf("estimate = %d, want 8 (ceil(12/4) + 5)", got)
	}
	if again := estimateAnthropicTokens(mustAnthropicReq(t, base)); again != got {
		t.Errorf("estimator must be deterministic: %d != %d", again, got)
	}

	withTool := `{"model":"m","system":"abcd","tools":[{"name":"t","description":"d","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"12345678"}]}`
	if tok := estimateAnthropicTokens(mustAnthropicReq(t, withTool)); tok <= got {
		t.Errorf("adding a tool must increase the estimate: %d <= %d", tok, got)
	}

	withImage := `{"model":"m","system":"abcd","messages":[{"role":"user","content":[{"type":"text","text":"12345678"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}`
	if tok := estimateAnthropicTokens(mustAnthropicReq(t, withImage)); tok != got+1200 {
		t.Errorf("image block must add 1200: got %d, want %d", tok, got+1200)
	}

	if tok := estimateAnthropicTokens(&anthropicMessagesRequest{Model: "m"}); tok != 1 {
		t.Errorf("empty request must clamp to 1, got %d", tok)
	}
}

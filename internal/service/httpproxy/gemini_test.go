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

func mustTranslateGemini(t *testing.T, raw string, stream bool) *oaiChatRequest {
	t.Helper()
	var req geminiRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := translateGeminiRequest(&req, "m", stream)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	return out
}

func TestTranslateGeminiRequest_Basic(t *testing.T) {
	out := mustTranslateGemini(t, `{
		"systemInstruction":{"parts":[{"text":"be brief"}]},
		"contents":[{"role":"user","parts":[{"text":"hi"}]}],
		"generationConfig":{"temperature":0.5,"maxOutputTokens":128,"topK":40,"stopSequences":["END"]}
	}`, false)
	if len(out.Messages) != 2 {
		t.Fatalf("messages = %#v, want system + user", out.Messages)
	}
	if out.Messages[0].Role != "system" || out.Messages[0].Content != "be brief" {
		t.Errorf("msg0 = %#v", out.Messages[0])
	}
	if out.Messages[1].Role != "user" || out.Messages[1].Content != "hi" {
		t.Errorf("msg1 = %#v", out.Messages[1])
	}
	if out.MaxTokens != 128 || out.Temperature == nil || *out.Temperature != 0.5 {
		t.Errorf("gen config not mapped: max=%d temp=%v", out.MaxTokens, out.Temperature)
	}
	if out.TopK == nil || *out.TopK != 40 || len(out.Stop) != 1 || out.Stop[0] != "END" {
		t.Errorf("topK/stop not mapped: %v / %v", out.TopK, out.Stop)
	}
}

func TestTranslateGeminiRequest_SnakeCaseSystemInstruction(t *testing.T) {
	out := mustTranslateGemini(t, `{"system_instruction":{"parts":[{"text":"sys"}]},"contents":[{"role":"user","parts":[{"text":"x"}]}]}`, false)
	if out.Messages[0].Role != "system" || out.Messages[0].Content != "sys" {
		t.Errorf("snake_case system_instruction not handled: %#v", out.Messages)
	}
}

func TestTranslateGeminiRequest_FunctionRoundtripAndTools(t *testing.T) {
	out := mustTranslateGemini(t, `{
		"contents":[
			{"role":"user","parts":[{"text":"weather?"}]},
			{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"SP"}}}]},
			{"role":"function","parts":[{"functionResponse":{"name":"get_weather","response":{"content":"sunny"}}}]}
		],
		"tools":[{"functionDeclarations":[{"name":"get_weather","description":"d","parameters":{"type":"object"}}]}],
		"toolConfig":{"functionCallingConfig":{"mode":"AUTO"}}
	}`, false)
	if len(out.Messages) != 3 {
		t.Fatalf("messages = %#v, want user + assistant(tool_calls) + tool", out.Messages)
	}
	if out.Messages[1].Role != "assistant" || len(out.Messages[1].ToolCalls) != 1 || out.Messages[1].ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("msg1 = %#v, want assistant tool_call", out.Messages[1])
	}
	tcID := out.Messages[1].ToolCalls[0].ID
	if out.Messages[2].Role != "tool" || out.Messages[2].ToolCallID != tcID || out.Messages[2].Content != "sunny" {
		t.Errorf("msg2 = %#v, want tool result paired to %s", out.Messages[2], tcID)
	}
	if len(out.Tools) != 1 || out.Tools[0].Function.Name != "get_weather" {
		t.Errorf("tools = %#v", out.Tools)
	}
	if out.ToolChoice != "auto" {
		t.Errorf("tool_choice = %v, want auto", out.ToolChoice)
	}
}

func TestTranslateGeminiRequest_ThinkingBudgetZeroDisables(t *testing.T) {
	out := mustTranslateGemini(t, `{"contents":[{"role":"user","parts":[{"text":"x"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":0}}}`, false)
	if out.ReasoningEffort != "none" {
		t.Errorf("reasoning_effort = %q, want none", out.ReasoningEffort)
	}
	if v, ok := out.ChatTemplateKwargs["enable_thinking"]; !ok || v != false {
		t.Errorf("enable_thinking = %v, want false", v)
	}
}

func TestBuildGeminiResponse(t *testing.T) {
	oai := &oaiChatResponse{Choices: []oaiChoice{{
		FinishReason: "stop",
		Message: oaiRespMessage{ReasoningContent: "think", Content: "answer",
			ToolCalls: []oaiToolCall{{Function: oaiFunctionCall{Name: "f", Arguments: `{"a":1}`}}}},
	}}, Usage: &oaiUsage{PromptTokens: 10, CompletionTokens: 5}}
	resp := buildGeminiResponse(oai, "m")
	if len(resp.Candidates) != 1 {
		t.Fatalf("candidates = %#v", resp.Candidates)
	}
	cand := resp.Candidates[0]
	if cand.Content.Role != "model" || len(cand.Content.Parts) != 3 {
		t.Fatalf("content = %#v, want thought+text+functionCall", cand.Content)
	}
	if !cand.Content.Parts[0].Thought || cand.Content.Parts[0].Text != "think" {
		t.Errorf("part0 = %#v, want thought", cand.Content.Parts[0])
	}
	if cand.Content.Parts[1].Text != "answer" {
		t.Errorf("part1 = %#v", cand.Content.Parts[1])
	}
	if cand.Content.Parts[2].FunctionCall == nil || cand.Content.Parts[2].FunctionCall.Name != "f" {
		t.Errorf("part2 = %#v, want functionCall", cand.Content.Parts[2])
	}
	if cand.FinishReason != "STOP" {
		t.Errorf("finishReason = %q, want STOP", cand.FinishReason)
	}
	if resp.UsageMetadata == nil || resp.UsageMetadata.PromptTokenCount != 10 || resp.UsageMetadata.TotalTokenCount != 15 {
		t.Errorf("usageMetadata = %#v", resp.UsageMetadata)
	}
}

func parseGeminiFrames(t *testing.T, body string) []map[string]any {
	t.Helper()
	var frames []map[string]any
	for blk := range strings.SplitSeq(body, "\n\n") {
		blk = strings.TrimSpace(blk)
		if !strings.HasPrefix(blk, "data:") {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(blk, "data:"))), &m); err != nil {
			t.Fatalf("gemini frame not JSON: %v (%q)", err, blk)
		}
		frames = append(frames, m)
	}
	return frames
}

func TestGeminiStream_TextThenUsage(t *testing.T) {
	rec := httptest.NewRecorder()
	script := sseScript(
		`{"choices":[{"delta":{"content":"Hel"}}]}`,
		`{"choices":[{"delta":{"content":"lo"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2}}`,
		`[DONE]`,
	)
	if err := runGeminiStream(context.Background(), rec, strings.NewReader(script), "alpha", mllog.Nop()); err != nil {
		t.Fatalf("stream: %v", err)
	}
	frames := parseGeminiFrames(t, rec.Body.String())
	// 2 text frames + 1 finish frame + 1 usage frame
	textFrames, finishFrames, usageFrames := 0, 0, 0
	for _, f := range frames {
		if _, ok := f["usageMetadata"]; ok {
			usageFrames++
			continue
		}
		cands, _ := f["candidates"].([]any)
		if len(cands) == 0 {
			continue
		}
		c0, _ := cands[0].(map[string]any)
		if fr, _ := c0["finishReason"].(string); fr != "" {
			finishFrames++
		}
		content, _ := c0["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		if len(parts) > 0 {
			if p0, _ := parts[0].(map[string]any); p0["text"] != nil {
				textFrames++
			}
		}
	}
	if textFrames != 2 {
		t.Errorf("text frames = %d, want 2", textFrames)
	}
	if finishFrames != 1 {
		t.Errorf("finish frames = %d, want 1", finishFrames)
	}
	if usageFrames != 1 {
		t.Errorf("usage frames = %d, want 1", usageFrames)
	}
}

func TestHandleGemini_Generate_Integration(t *testing.T) {
	backend, caps := startBackend(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"content":"hi there"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
	})
	_, mux := newAnthropicMux(t, newStubStore(makeProfile("alpha", portOf(t, backend))), newStubManager())

	rr := postJSON(t, mux, "/v1beta/models/alpha:generateContent",
		`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
	}
	cap := <-caps
	if cap.Path != "/v1/chat/completions" {
		t.Errorf("upstream path = %q", cap.Path)
	}
	var resp geminiResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Candidates) != 1 || resp.Candidates[0].Content.Role != "model" {
		t.Fatalf("candidates = %#v", resp.Candidates)
	}
	if len(resp.Candidates[0].Content.Parts) != 1 || resp.Candidates[0].Content.Parts[0].Text != "hi there" {
		t.Errorf("parts = %#v, want text 'hi there'", resp.Candidates[0].Content.Parts)
	}
}

func TestHandleGeminiModels_List(t *testing.T) {
	_, mux := newAnthropicMux(t, newStubStore(makeProfile("alpha", 1234)), newStubManager())
	req := httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var list geminiModelsList
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Models) != 1 || list.Models[0].Name != "models/alpha" {
		t.Fatalf("models = %#v, want models/alpha", list.Models)
	}
	if len(list.Models[0].SupportedGenerationMethods) == 0 {
		t.Errorf("supportedGenerationMethods empty")
	}
}

func TestBuildGeminiResponse_AcceptsVLLMReasoningFieldFromJSON(t *testing.T) {
	var oai oaiChatResponse
	if err := json.Unmarshal([]byte(`{"choices":[{"finish_reason":"stop","message":{"reasoning":"think","content":"answer"}}]}`), &oai); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resp := buildGeminiResponse(&oai, "m")
	if len(resp.Candidates) != 1 {
		t.Fatalf("candidates = %#v", resp.Candidates)
	}
	parts := resp.Candidates[0].Content.Parts
	if len(parts) != 2 {
		t.Fatalf("parts = %#v, want thought+text", parts)
	}
	if !parts[0].Thought || parts[0].Text != "think" {
		t.Errorf("part0 = %#v, want thought think", parts[0])
	}
	if parts[1].Thought || parts[1].Text != "answer" {
		t.Errorf("part1 = %#v, want text answer", parts[1])
	}
}

func TestGeminiStream_AcceptsVLLMReasoningField(t *testing.T) {
	rec := httptest.NewRecorder()
	script := sseScript(
		`{"choices":[{"delta":{"reasoning":"think"}}]}`,
		`{"choices":[{"delta":{"content":"answer"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`[DONE]`,
	)
	if err := runGeminiStream(context.Background(), rec, strings.NewReader(script), "alpha", mllog.Nop()); err != nil {
		t.Fatalf("stream: %v", err)
	}
	frames := parseGeminiFrames(t, rec.Body.String())
	var sawThought, sawText bool
	for _, f := range frames {
		cands, _ := f["candidates"].([]any)
		if len(cands) == 0 {
			continue
		}
		c0, _ := cands[0].(map[string]any)
		content, _ := c0["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		for _, p := range parts {
			pm, _ := p.(map[string]any)
			thought, _ := pm["thought"].(bool)
			text, _ := pm["text"].(string)
			if thought && text == "think" {
				sawThought = true
			}
			if !thought && text == "answer" {
				sawText = true
			}
		}
	}
	if !sawThought {
		t.Errorf("no thought:true/text:think part in frames: %#v", frames)
	}
	if !sawText {
		t.Errorf("no text:answer (non-thought) part in frames: %#v", frames)
	}
}

func TestBuildGeminiResponse_MirrorsReasoningOnlyAsText(t *testing.T) {
	var oai oaiChatResponse
	if err := json.Unmarshal([]byte(`{"choices":[{"finish_reason":"length","message":{"reasoning":"think"}}]}`), &oai); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resp := buildGeminiResponse(&oai, "m")
	if len(resp.Candidates) != 1 {
		t.Fatalf("candidates = %#v", resp.Candidates)
	}
	cand := resp.Candidates[0]
	parts := cand.Content.Parts
	if len(parts) != 2 {
		t.Fatalf("parts = %#v, want thought+text fallback", parts)
	}
	if !parts[0].Thought || parts[0].Text != "think" {
		t.Fatalf("part0 = %#v, want thought", parts[0])
	}
	if parts[1].Thought || parts[1].Text != "think" {
		t.Fatalf("part1 = %#v, want mirrored text", parts[1])
	}
	if cand.FinishReason != "MAX_TOKENS" {
		t.Fatalf("finishReason = %q, want MAX_TOKENS", cand.FinishReason)
	}
}

func TestGeminiStream_MirrorsReasoningOnlyAsText(t *testing.T) {
	rec := httptest.NewRecorder()
	script := sseScript(
		`{"choices":[{"delta":{"reasoning":"think"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"length"}]}`,
		`[DONE]`,
	)
	if err := runGeminiStream(context.Background(), rec, strings.NewReader(script), "alpha", mllog.Nop()); err != nil {
		t.Fatalf("stream: %v", err)
	}
	frames := parseGeminiFrames(t, rec.Body.String())
	var sawThought, sawText bool
	for _, f := range frames {
		cands, _ := f["candidates"].([]any)
		for _, c := range cands {
			c0, _ := c.(map[string]any)
			content, _ := c0["content"].(map[string]any)
			parts, _ := content["parts"].([]any)
			for _, p := range parts {
				pm, _ := p.(map[string]any)
				thought, _ := pm["thought"].(bool)
				text, _ := pm["text"].(string)
				if thought && text == "think" {
					sawThought = true
				}
				if !thought && text == "think" {
					sawText = true
				}
			}
		}
	}
	if !sawThought || !sawText {
		t.Fatalf("frames = %#v, want thought and mirrored text", frames)
	}
}

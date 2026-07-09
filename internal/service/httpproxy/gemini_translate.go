package httpproxy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// translateGeminiRequest maps a Gemini request onto the backend's OpenAI chat
// completions request (reusing the oai* types).
func translateGeminiRequest(req *geminiRequest, profileID string, stream bool) (*oaiChatRequest, error) {
	out := &oaiChatRequest{Model: profileID}
	if stream {
		out.Stream = true
		out.StreamOptions = &oaiStreamOptions{IncludeUsage: true}
	}

	if gc := req.GenerationConfig; gc != nil {
		out.Temperature = gc.Temperature
		out.TopP = gc.TopP
		out.TopK = gc.TopK
		out.Stop = gc.StopSequences
		if gc.MaxOutputTokens != nil {
			out.MaxTokens = *gc.MaxOutputTokens
		}
		if tc := gc.ThinkingConfig; tc != nil {
			if tc.ThinkingLevel != "" {
				applyThinking(out, effortConfig(strings.ToLower(strings.TrimSpace(tc.ThinkingLevel))))
			} else if tc.ThinkingBudget != nil {
				applyThinking(out, budgetConfig(*tc.ThinkingBudget))
			}
		}
	}

	if req.SystemInstruction != nil {
		if txt := geminiPartsText(req.SystemInstruction.Parts); txt != "" {
			out.Messages = append(out.Messages, oaiChatMessage{Role: "system", Content: txt})
		}
	}

	pairing := &geminiToolPairing{}
	for i := range req.Contents {
		appendGeminiContent(out, &req.Contents[i], pairing)
	}

	for _, tl := range req.Tools {
		for _, fd := range tl.FunctionDeclarations {
			params := fd.Parameters
			if len(params) == 0 {
				params = fd.ParametersJSONSchema
			}
			out.Tools = append(out.Tools, oaiTool{
				Type:     "function",
				Function: oaiToolFunction{Name: fd.Name, Description: fd.Description, Parameters: normalizeToolSchema(params)},
			})
		}
	}
	if tc := req.ToolConfig; tc != nil && tc.FunctionCallingConfig != nil {
		switch strings.ToUpper(strings.TrimSpace(tc.FunctionCallingConfig.Mode)) {
		case "NONE":
			out.ToolChoice = "none"
		case "AUTO":
			out.ToolChoice = "auto"
		case "ANY":
			out.ToolChoice = "required"
		}
	}
	return out, nil
}

// geminiToolPairing pairs functionCall ids with the functionResponse tool
// messages FIFO (Gemini parts carry no id), matching CLIProxyAPI.
type geminiToolPairing struct {
	pending []string
}

func (p *geminiToolPairing) push(id string) { p.pending = append(p.pending, id) }
func (p *geminiToolPairing) pop() string {
	if len(p.pending) == 0 {
		return geminiToolID()
	}
	id := p.pending[0]
	p.pending = p.pending[1:]
	return id
}

// appendGeminiContent converts one Gemini content into 0..n OpenAI messages.
// functionResponse parts each emit a standalone tool message; the rest fold
// into one message. An empty content (only function responses) is suppressed.
func appendGeminiContent(out *oaiChatRequest, c *geminiContent, pairing *geminiToolPairing) {
	role := c.Role
	switch role {
	case "model":
		role = "assistant"
	case "", "function":
		role = "user"
	}

	var texts []string
	var oaiParts []oaiContentPart
	var toolCalls []oaiToolCall
	hasImage := false

	for i := range c.Parts {
		p := &c.Parts[i]
		switch {
		case p.FunctionResponse != nil:
			out.Messages = append(out.Messages, oaiChatMessage{
				Role: "tool", ToolCallID: pairing.pop(), Content: geminiFunctionResponseText(p.FunctionResponse),
			})
		case p.FunctionCall != nil:
			args := strings.TrimSpace(string(p.FunctionCall.Args))
			if args == "" || args == "null" {
				args = "{}"
			}
			id := geminiToolID()
			pairing.push(id)
			toolCalls = append(toolCalls, oaiToolCall{
				ID: id, Type: "function", Function: oaiFunctionCall{Name: p.FunctionCall.Name, Arguments: args},
			})
		case p.InlineData != nil:
			mime := p.InlineData.MimeType
			if mime == "" {
				mime = "application/octet-stream"
			}
			oaiParts = append(oaiParts, oaiContentPart{Type: "image_url", ImageURL: &oaiImageURL{URL: "data:" + mime + ";base64," + p.InlineData.Data}})
			hasImage = true
		case p.Text != "":
			texts = append(texts, p.Text)
			oaiParts = append(oaiParts, oaiContentPart{Type: "text", Text: p.Text})
		}
	}

	if len(texts) == 0 && !hasImage && len(toolCalls) == 0 {
		return // suppress the empty orphan message a pure functionResponse content leaves
	}
	msg := oaiChatMessage{Role: role, ToolCalls: toolCalls}
	switch {
	case hasImage:
		msg.Content = oaiParts
	case len(texts) > 0:
		msg.Content = strings.Join(texts, "\n\n")
	case len(toolCalls) > 0:
		msg.Content = nil // content:null alongside tool_calls
	default:
		msg.Content = ""
	}
	out.Messages = append(out.Messages, msg)
}

func geminiPartsText(parts []geminiPart) string {
	var texts []string
	for _, p := range parts {
		if p.Text != "" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n\n")
}

// geminiFunctionResponseText flattens a functionResponse `response` (object with
// a `content` field, or the whole object) to a string.
func geminiFunctionResponseText(fr *geminiFunctionResponse) string {
	trimmed := strings.TrimSpace(string(fr.Response))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(fr.Response, &obj) == nil {
		if c, ok := obj["content"]; ok {
			cs := strings.TrimSpace(string(c))
			if len(cs) > 1 && cs[0] == '"' {
				var s string
				if json.Unmarshal(c, &s) == nil {
					return s
				}
			}
			return cs
		}
	}
	return trimmed
}

func geminiToolID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "call_" + hex.EncodeToString(b[:])
}

// buildGeminiResponse maps a non-streaming OpenAI chat response onto the Gemini
// shape (choice 0). Part order: thought (reasoning) → text → functionCall.
func buildGeminiResponse(oai *oaiChatResponse, model string) *geminiResponse {
	resp := &geminiResponse{Candidates: []geminiCandidate{}, ModelVersion: model}
	if len(oai.Choices) > 0 {
		ch := oai.Choices[0]
		reasoning := ch.Message.ReasoningContent
		content := ch.Message.Content
		cand := geminiCandidate{Index: 0, Content: geminiOutContent{Role: "model", Parts: []geminiOutPart{}}}
		if reasoning != "" {
			cand.Content.Parts = append(cand.Content.Parts, geminiOutPart{Thought: true, Text: reasoning})
		}
		if mirrorReasoningAsText(content, reasoning, len(ch.Message.ToolCalls)) {
			content = reasoning
		}
		if content != "" {
			cand.Content.Parts = append(cand.Content.Parts, geminiOutPart{Text: content})
		}
		for _, tc := range ch.Message.ToolCalls {
			cand.Content.Parts = append(cand.Content.Parts, geminiOutPart{
				FunctionCall: &geminiOutFuncCall{Name: tc.Function.Name, Args: parseArgsRaw(tc.Function.Arguments)},
			})
		}
		cand.FinishReason = mapFinishReasonGemini(ch.FinishReason)
		resp.Candidates = append(resp.Candidates, cand)
	}
	if oai.Usage != nil {
		resp.UsageMetadata = &geminiUsageMetadata{
			PromptTokenCount:     oai.Usage.PromptTokens,
			CandidatesTokenCount: oai.Usage.CompletionTokens,
			TotalTokenCount:      oai.Usage.PromptTokens + oai.Usage.CompletionTokens,
		}
	}
	return resp
}

// parseArgsRaw turns a tool-call arguments string into a JSON object (with
// fixJSON rescue), falling back to {}.
func parseArgsRaw(args string) json.RawMessage {
	s := strings.TrimSpace(args)
	if s == "" {
		return json.RawMessage("{}")
	}
	if json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	if fixed := fixJSON(s); json.Valid([]byte(fixed)) {
		return json.RawMessage(fixed)
	}
	return json.RawMessage("{}")
}

// mapFinishReasonGemini maps an OpenAI finish_reason onto a Gemini finishReason.
func mapFinishReasonGemini(fr string) string {
	switch fr {
	case "length":
		return "MAX_TOKENS"
	case "content_filter":
		return "SAFETY"
	default: // stop, tool_calls, "", unknown
		return "STOP"
	}
}

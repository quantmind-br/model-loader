package httpproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// translateResponsesRequest maps an OpenAI Responses request onto the backend's
// OpenAI chat-completions request. Reuses the shared oai* types so the upstream
// call and streaming path are identical to the other translated routes.
func translateResponsesRequest(req *responsesRequest, profileID string) (*oaiChatRequest, error) {
	out := &oaiChatRequest{
		Model:             profileID,
		Temperature:       req.Temperature,
		TopP:              req.TopP,
		ParallelToolCalls: req.ParallelToolCalls,
	}
	if req.MaxOutputTokens != nil {
		out.MaxTokens = *req.MaxOutputTokens
	}
	if req.Stream {
		out.Stream = true
		out.StreamOptions = &oaiStreamOptions{IncludeUsage: true}
	}

	if req.Instructions != "" {
		out.Messages = append(out.Messages, oaiChatMessage{Role: "system", Content: req.Instructions})
	}
	if err := appendResponsesInput(out, req.Input); err != nil {
		return nil, err
	}

	for _, t := range req.Tools {
		if t.Type != "" && t.Type != "function" {
			continue // skip built-in tools (web_search, file_search, …)
		}
		out.Tools = append(out.Tools, oaiTool{
			Type:     "function",
			Function: oaiToolFunction{Name: t.Name, Description: t.Description, Parameters: normalizeToolSchema(t.Parameters)},
		})
	}
	if len(bytes.TrimSpace(req.ToolChoice)) > 0 {
		out.ToolChoice = json.RawMessage(req.ToolChoice) // already OpenAI-shaped
	}
	if req.Reasoning != nil && req.Reasoning.Effort != "" {
		applyThinking(out, effortConfig(strings.ToLower(strings.TrimSpace(req.Reasoning.Effort))))
	}
	return out, nil
}

// appendResponsesInput handles the Responses `input`: a plain string (one user
// turn) or an array of typed items.
func appendResponsesInput(out *oaiChatRequest, raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return fmt.Errorf("input: %w", err)
		}
		out.Messages = append(out.Messages, oaiChatMessage{Role: "user", Content: s})
		return nil
	}
	var items []responsesInputItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	for i := range items {
		if err := appendResponsesItem(out, &items[i]); err != nil {
			return err
		}
	}
	return nil
}

func appendResponsesItem(out *oaiChatRequest, item *responsesInputItem) error {
	itemType := item.Type
	if itemType == "" && item.Role != "" {
		itemType = "message"
	}
	switch itemType {
	case "message", "":
		role := item.Role
		if role == "developer" {
			role = "user"
		}
		if role == "" {
			role = "user"
		}
		content, err := responsesContentToOAI(item.Content)
		if err != nil {
			return err
		}
		out.Messages = append(out.Messages, oaiChatMessage{Role: role, Content: content})
	case "function_call":
		args := strings.TrimSpace(item.Arguments)
		if args == "" {
			args = "{}"
		}
		out.Messages = append(out.Messages, oaiChatMessage{
			Role:      "assistant",
			Content:   nil,
			ToolCalls: []oaiToolCall{{ID: item.CallID, Type: "function", Function: oaiFunctionCall{Name: item.Name, Arguments: args}}},
		})
	case "function_call_output":
		out.Messages = append(out.Messages, oaiChatMessage{
			Role: "tool", ToolCallID: item.CallID, Content: responsesOutputToString(item.Output),
		})
	case "reasoning":
		// Input reasoning is not forwarded upstream.
	}
	return nil
}

// responsesContentToOAI maps a Responses content (string or part list) onto the
// OpenAI content form: a joined string, or a content-part array when an image
// is present.
func responsesContentToOAI(raw json.RawMessage) (any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return "", nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("content: %w", err)
		}
		return s, nil
	}
	var parts []responsesContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("content: %w", err)
	}
	var oaiParts []oaiContentPart
	var texts []string
	hasImage := false
	for _, p := range parts {
		switch p.Type {
		case "input_image":
			oaiParts = append(oaiParts, oaiContentPart{Type: "image_url", ImageURL: &oaiImageURL{URL: p.ImageURL}})
			hasImage = true
		default: // input_text, output_text, ""
			oaiParts = append(oaiParts, oaiContentPart{Type: "text", Text: p.Text})
			texts = append(texts, p.Text)
		}
	}
	if hasImage {
		return oaiParts, nil
	}
	return strings.Join(texts, "\n\n"), nil
}

// responsesOutputToString flattens a function_call_output `output` (string or
// object) to a string for the tool message content.
func responsesOutputToString(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return ""
	}
	if trimmed[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	}
	return string(trimmed)
}

// buildResponsesResponse maps a non-streaming OpenAI chat response onto the
// Responses shape (choice 0). Output order: reasoning → message → function_call.
func buildResponsesResponse(oai *oaiChatResponse, model string, createdAt int64) *responsesResponse {
	resp := &responsesResponse{
		ID:        newResponsesID("resp_"),
		Object:    "response",
		CreatedAt: createdAt,
		Status:    "completed",
		Model:     model,
		Output:    []any{},
	}
	if len(oai.Choices) > 0 {
		ch := oai.Choices[0]
		if ch.Message.ReasoningContent != "" {
			resp.Output = append(resp.Output, responsesReasoningItem{
				Type: "reasoning", ID: newResponsesID("rs_"),
				Summary: []any{responsesSummaryText{Type: "summary_text", Text: ch.Message.ReasoningContent}},
			})
		}
		if ch.Message.Content != "" {
			resp.Output = append(resp.Output, responsesMessageItem{
				Type: "message", ID: newResponsesID("msg_"), Status: "completed", Role: "assistant",
				Content: []any{responsesOutputText{Type: "output_text", Text: ch.Message.Content, Annotations: []any{}}},
			})
		}
		for _, tc := range ch.Message.ToolCalls {
			resp.Output = append(resp.Output, responsesFunctionCallItem{
				Type: "function_call", ID: newResponsesID("fc_"), CallID: sanitizeToolID(tc.ID),
				Name: tc.Function.Name, Arguments: tc.Function.Arguments, Status: "completed",
			})
		}
	}
	if oai.Usage != nil {
		resp.Usage = &responsesUsage{
			InputTokens:  oai.Usage.PromptTokens,
			OutputTokens: oai.Usage.CompletionTokens,
			TotalTokens:  oai.Usage.PromptTokens + oai.Usage.CompletionTokens,
		}
	}
	return resp
}

package httpproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// translateAnthropicRequest maps a validated Anthropic Messages request onto
// the OpenAI chat-completions request sent to the loaded backend. profileID is
// the resolved profile id stuffed into the upstream body (vLLM/SGLang validate
// served-model-name against it).
func translateAnthropicRequest(req *anthropicMessagesRequest, profileID string) (*oaiChatRequest, *anthropicAPIError) {
	out := &oaiChatRequest{
		Model:       profileID,
		Stop:        req.StopSequences,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		TopK:        req.TopK,
	}
	if req.MaxTokens != nil {
		out.MaxTokens = *req.MaxTokens
	}
	if req.Stream {
		out.Stream = true
		out.StreamOptions = &oaiStreamOptions{IncludeUsage: true}
	}

	// The top-level `system` becomes the sole leading system message. Inline
	// {"role":"system"} messages (Claude Code injects context reminders after
	// the first user turn) are converted to in-position user messages wrapped in
	// <system-reminder> — chat templates (Gemma, Qwen3-MoE, ...) reject any
	// system role that is not the sole first message. (Follows CLIProxyAPI.)
	sys, aerr := flattenSystem(req.System)
	if aerr != nil {
		return nil, aerr
	}
	if sys != "" {
		out.Messages = append(out.Messages, oaiChatMessage{Role: "system", Content: sys})
	}
	for i := range req.Messages {
		out.Messages, aerr = appendTranslatedMessage(out.Messages, req.Messages[i])
		if aerr != nil {
			return nil, aerr
		}
	}

	for _, tl := range req.Tools {
		out.Tools = append(out.Tools, oaiTool{
			Type: "function",
			Function: oaiToolFunction{
				Name:        tl.Name,
				Description: tl.Description,
				Parameters:  normalizeToolSchema(tl.InputSchema),
			},
		})
	}
	if req.ToolChoice != nil {
		choice, parallel, aerr := translateToolChoice(req.ToolChoice)
		if aerr != nil {
			return nil, aerr
		}
		out.ToolChoice = choice
		out.ParallelToolCalls = parallel
	}

	// Reasoning controls (thinking/output_config) → canonical pivot → backend.
	applyThinking(out, extractThinkingConfig(req))
	// metadata.user_id → OpenAI user (abuse/rate-limit attribution passthrough).
	if len(bytes.TrimSpace(req.Metadata)) > 0 {
		var md struct {
			UserID string `json:"user_id"`
		}
		if json.Unmarshal(req.Metadata, &md) == nil && md.UserID != "" {
			out.User = md.UserID
		}
	}
	return out, nil
}

// flattenSystem accepts the Anthropic dual system form — a plain string or a
// list of text blocks — and returns the joined prompt text.
func flattenSystem(raw json.RawMessage) (string, *anthropicAPIError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return "", nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", &anthropicAPIError{400, "invalid_request_error", "system: " + err.Error()}
		}
		return s, nil
	}
	var blocks []anthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", &anthropicAPIError{400, "invalid_request_error", "system: " + err.Error()}
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" && !isAttributionText(b.Text) {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// isAttributionText reports whether a system text block is the Claude Code
// billing/attribution header (x-anthropic-billing-header: …): metadata for
// Anthropic's own API and pure noise for a local backend, so it is dropped.
func isAttributionText(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "x-anthropic-billing-header:")
}

// appendTranslatedMessage maps one Anthropic message onto 0..n OpenAI
// messages, appending them to out.
func appendTranslatedMessage(out []oaiChatMessage, m anthropicMessage) ([]oaiChatMessage, *anthropicAPIError) {
	switch m.Role {
	case "user":
		return appendUserMessage(out, m)
	case "assistant":
		return appendAssistantMessage(out, m)
	case "system":
		return appendSystemReminderAsUser(out, m), nil
	default:
		return nil, &anthropicAPIError{400, "invalid_request_error",
			fmt.Sprintf("messages: unsupported role %q (only \"user\", \"assistant\", and \"system\")", m.Role)}
	}
}

// appendSystemReminderAsUser converts an inline {"role":"system"} message into
// an in-position user message wrapped in <system-reminder>. Templates that
// require a sole leading system message reject an in-position system role, so
// the reminder's position is preserved as user content (following CLIProxyAPI).
func appendSystemReminderAsUser(out []oaiChatMessage, m anthropicMessage) []oaiChatMessage {
	txt := systemMessageText(m)
	if txt == "" {
		return out
	}
	return append(out, oaiChatMessage{Role: "user", Content: "<system-reminder>\n" + txt + "\n</system-reminder>"})
}

// systemMessageText extracts the plain text of a {"role":"system"} message
// (string or text-block content), joining blocks with blank lines.
func systemMessageText(m anthropicMessage) string {
	if m.Content.IsString {
		return m.Content.Text
	}
	var parts []string
	for _, b := range m.Content.Blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// appendUserMessage translates a user turn. tool_result blocks each become
// one {role:"tool"} message emitted BEFORE the remaining user content of the
// same turn (the ordering the OpenAI API requires after an assistant
// tool_calls message). The tool_result is_error flag and any non-text payload
// inside it are dropped — a documented lossy corner of the translation.
func appendUserMessage(out []oaiChatMessage, m anthropicMessage) ([]oaiChatMessage, *anthropicAPIError) {
	if m.Content.IsString {
		return append(out, oaiChatMessage{Role: "user", Content: m.Content.Text}), nil
	}
	var toolMsgs []oaiChatMessage
	var parts []oaiContentPart
	hasImage := false
	for _, b := range m.Content.Blocks {
		switch b.Type {
		case "text":
			parts = append(parts, oaiContentPart{Type: "text", Text: b.Text})
		case "image":
			part, aerr := imagePartFromSource(b.Source)
			if aerr != nil {
				return nil, aerr
			}
			parts = append(parts, part)
			hasImage = true
		case "tool_result":
			text, aerr := flattenToolResultContent(b.Content)
			if aerr != nil {
				return nil, aerr
			}
			if b.IsError {
				// OpenAI tool messages have no error flag; prefix so the model
				// still sees the call failed (Anthropic is_error would be lost).
				text = "[tool_error] " + text
			}
			toolMsgs = append(toolMsgs, oaiChatMessage{Role: "tool", ToolCallID: b.ToolUseID, Content: text})
		case "document":
			if b.Source != nil && b.Source.Type == "text" {
				parts = append(parts, oaiContentPart{Type: "text", Text: b.Source.Data})
			} else {
				return nil, &anthropicAPIError{400, "invalid_request_error",
					"document blocks are only supported with a plain-text source"}
			}
		case "thinking", "redacted_thinking":
			// Thinking blocks are never forwarded upstream.
		case "tool_use":
			return nil, &anthropicAPIError{400, "invalid_request_error",
				"tool_use blocks belong in assistant messages"}
		default:
			// Unknown block types are skipped for forward compatibility.
		}
	}
	out = append(out, toolMsgs...)
	if len(parts) > 0 {
		if hasImage {
			out = append(out, oaiChatMessage{Role: "user", Content: parts})
		} else {
			texts := make([]string, 0, len(parts))
			for _, p := range parts {
				texts = append(texts, p.Text)
			}
			out = append(out, oaiChatMessage{Role: "user", Content: strings.Join(texts, "\n\n")})
		}
	}
	return out, nil
}

// appendAssistantMessage translates an assistant turn: text blocks join into
// the message content, tool_use blocks become tool_calls on the same message,
// thinking blocks are dropped.
func appendAssistantMessage(out []oaiChatMessage, m anthropicMessage) ([]oaiChatMessage, *anthropicAPIError) {
	if m.Content.IsString {
		return append(out, oaiChatMessage{Role: "assistant", Content: m.Content.Text}), nil
	}
	var texts []string
	var calls []oaiToolCall
	for _, b := range m.Content.Blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				texts = append(texts, b.Text)
			}
		case "tool_use":
			args := strings.TrimSpace(string(b.Input))
			if args == "" || args == "null" {
				args = "{}"
			}
			calls = append(calls, oaiToolCall{
				ID:       b.ID,
				Type:     "function",
				Function: oaiFunctionCall{Name: b.Name, Arguments: args},
			})
		case "thinking", "redacted_thinking":
			// Thinking blocks are never forwarded upstream.
		case "tool_result":
			return nil, &anthropicAPIError{400, "invalid_request_error",
				"tool_result blocks belong in user messages"}
		case "image":
			return nil, &anthropicAPIError{400, "invalid_request_error",
				"image blocks are not supported in assistant messages"}
		default:
			// Unknown block types are skipped for forward compatibility.
		}
	}
	msg := oaiChatMessage{Role: "assistant", ToolCalls: calls}
	joined := strings.Join(texts, "\n\n")
	switch {
	case joined != "":
		msg.Content = joined
	case len(calls) > 0:
		msg.Content = nil // marshals to content:null, valid alongside tool_calls
	default:
		msg.Content = ""
	}
	return append(out, msg), nil
}

// flattenToolResultContent accepts the tool_result dual content form —
// string or block list — and returns the joined text. Non-text blocks
// (images) inside a tool_result are dropped.
func flattenToolResultContent(raw json.RawMessage) (string, *anthropicAPIError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return "", nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", &anthropicAPIError{400, "invalid_request_error", "tool_result content: " + err.Error()}
		}
		return s, nil
	}
	var blocks []anthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", &anthropicAPIError{400, "invalid_request_error", "tool_result content: " + err.Error()}
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// imagePartFromSource maps an Anthropic image source onto an OpenAI
// image_url content part (data: URI for base64 sources).
func imagePartFromSource(s *anthropicSource) (oaiContentPart, *anthropicAPIError) {
	if s == nil {
		return oaiContentPart{}, &anthropicAPIError{400, "invalid_request_error", "image block missing source"}
	}
	switch s.Type {
	case "base64":
		return oaiContentPart{
			Type:     "image_url",
			ImageURL: &oaiImageURL{URL: "data:" + s.MediaType + ";base64," + s.Data},
		}, nil
	case "url":
		return oaiContentPart{Type: "image_url", ImageURL: &oaiImageURL{URL: s.URL}}, nil
	default:
		return oaiContentPart{}, &anthropicAPIError{400, "invalid_request_error",
			fmt.Sprintf("image source type %q not supported", s.Type)}
	}
}

// translateToolChoice maps the Anthropic tool_choice onto the OpenAI form.
func translateToolChoice(tc *anthropicToolChoice) (any, *bool, *anthropicAPIError) {
	var parallel *bool
	if tc.DisableParallelToolUse != nil && *tc.DisableParallelToolUse {
		f := false
		parallel = &f
	}
	switch tc.Type {
	case "auto":
		return "auto", parallel, nil
	case "any":
		return "required", parallel, nil
	case "none":
		return "none", parallel, nil
	case "tool":
		fc := oaiToolChoiceFunc{Type: "function"}
		fc.Function.Name = tc.Name
		return fc, parallel, nil
	default:
		return nil, nil, &anthropicAPIError{400, "invalid_request_error",
			fmt.Sprintf("tool_choice type %q not supported", tc.Type)}
	}
}

// normalizeToolSchema ensures every {"type":"object"} node in a JSON schema
// carries a properties object — strict backends (vLLM/SGLang grammar builders)
// reject an object schema without one. Recurses through properties and items.
// Returns the input unchanged when empty or not valid JSON.
func normalizeToolSchema(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	normalizeSchemaNode(v)
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

func normalizeSchemaNode(node any) {
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	if t, _ := m["type"].(string); t == "object" {
		if _, has := m["properties"]; !has {
			m["properties"] = map[string]any{}
		}
	}
	if props, ok := m["properties"].(map[string]any); ok {
		for _, child := range props {
			normalizeSchemaNode(child)
		}
	}
	if items, ok := m["items"]; ok {
		normalizeSchemaNode(items)
	}
}

// buildAnthropicResponse maps a non-streaming OpenAI chat response onto the
// Anthropic message shape (choice 0 only). Block order: thinking (from
// reasoning_content) → text → one tool_use per tool_call.
func buildAnthropicResponse(oai *oaiChatResponse, requestedModel string, logger *slog.Logger) *anthropicMessageResponse {
	resp := &anthropicMessageResponse{
		ID:      newAnthropicMessageID(),
		Type:    "message",
		Role:    "assistant",
		Model:   requestedModel,
		Content: []any{},
	}
	stop := "end_turn"
	if len(oai.Choices) > 0 {
		ch := oai.Choices[0]
		if ch.Message.ReasoningContent != "" {
			resp.Content = append(resp.Content, anthropicThinkingBlock{Type: "thinking", Thinking: ch.Message.ReasoningContent})
		}
		if ch.Message.Content != "" {
			resp.Content = append(resp.Content, anthropicTextBlock{Type: "text", Text: ch.Message.Content})
		}
		for _, tc := range ch.Message.ToolCalls {
			resp.Content = append(resp.Content, anthropicToolUseBlock{
				Type: "tool_use", ID: sanitizeToolID(tc.ID), Name: tc.Function.Name,
				Input: parseToolArguments(tc.Function.Arguments, tc.Function.Name, logger),
			})
		}
		stop = mapFinishReason(ch.FinishReason)
	}
	resp.StopReason = &stop
	if oai.Usage != nil {
		resp.Usage = anthropicUsage{
			InputTokens:  oai.Usage.PromptTokens,
			OutputTokens: oai.Usage.CompletionTokens,
		}
	}
	return resp
}

// parseToolArguments unmarshals tool-call arguments into an object, retrying
// via fixJSON when the raw string is not valid JSON (single-quoted / lightly
// malformed args). Returns an empty, non-nil map when it still cannot parse.
func parseToolArguments(raw, toolName string, logger *slog.Logger) map[string]any {
	var input map[string]any
	if json.Unmarshal([]byte(raw), &input) == nil && input != nil {
		return input
	}
	if fixed := fixJSON(raw); fixed != raw {
		var repaired map[string]any
		if json.Unmarshal([]byte(fixed), &repaired) == nil && repaired != nil {
			return repaired
		}
	}
	logger.Warn("anthropic_tool_arguments_invalid", "tool", toolName)
	return map[string]any{}
}

// mapFinishReason maps an OpenAI finish_reason onto an Anthropic stop_reason.
// stop_sequence is never emitted: OpenAI's finish_reason "stop" does not
// distinguish EOS from a custom stop string.
func mapFinishReason(fr string) string {
	switch fr {
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default: // "stop", "", "content_filter", anything else
		return "end_turn"
	}
}

// estimateAnthropicTokens is the deterministic local estimator backing
// POST /v1/messages/count_tokens. It never contacts the backend: total text
// bytes (raw JSON lengths counted as received, never re-marshalled) at ~4
// bytes/token, plus a flat per-message overhead and a flat per-image cost.
func estimateAnthropicTokens(req *anthropicMessagesRequest) int {
	total := 0
	images := 0
	if sys, aerr := flattenSystem(req.System); aerr == nil {
		total += len(sys)
	}
	for _, m := range req.Messages {
		if m.Content.IsString {
			total += len(m.Content.Text)
			continue
		}
		for _, b := range m.Content.Blocks {
			switch b.Type {
			case "text":
				total += len(b.Text)
			case "thinking":
				total += len(b.Thinking)
			case "tool_use":
				total += len(b.Name) + len(b.Input)
			case "tool_result":
				if text, aerr := flattenToolResultContent(b.Content); aerr == nil {
					total += len(text)
				}
			case "document":
				if b.Source != nil {
					total += len(b.Source.Data)
				}
			case "image":
				images++
			}
		}
	}
	for _, tl := range req.Tools {
		total += len(tl.Name) + len(tl.Description) + len(tl.InputSchema)
	}
	return max((total+3)/4+5*len(req.Messages)+1200*images, 1)
}

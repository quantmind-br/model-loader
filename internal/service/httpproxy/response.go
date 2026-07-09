package httpproxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// mirrorReasoningIntoEmptyContent copies the reasoning trace into content when a
// completed choice (finish_reason "stop") has empty content but non-empty
// reasoning. It reads either "reasoning" (vLLM 0.24+) or "reasoning_content"
// (older/other backends) and preserves all other top-level response fields
// (usage, id, model, timings, …). Truncated choices (finish_reason "length")
// are left untouched: their reasoning is an incomplete chain-of-thought.
func mirrorReasoningIntoEmptyContent(body []byte) ([]byte, bool) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return body, false
	}
	rawChoices, ok := root["choices"]
	if !ok {
		return body, false
	}
	var choices []map[string]json.RawMessage
	if err := json.Unmarshal(rawChoices, &choices); err != nil {
		return body, false
	}
	changed := false
	for i := range choices {
		// Mirror a reasoning-only answer regardless of finish_reason: a client
		// like llm-wiki rejects an assistant turn that carries reasoning but
		// empty content, whether the backend stopped normally or exhausted its
		// token budget. The finish_reason itself is left untouched (a "length"
		// turn stays "length"); only the empty content channel is filled. A
		// tool-call turn keeps empty content — mirroring reasoning there would
		// corrupt the tool call.
		rawMsg, ok := choices[i]["message"]
		if !ok {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			continue
		}
		content, _ := msg["content"].(string)
		// vLLM 0.24+ emits the reasoning trace under "reasoning"; older/other
		// backends use "reasoning_content". Accept either.
		reasoningContent, _ := msg["reasoning_content"].(string)
		reasoningField, _ := msg["reasoning"].(string)
		reasoning := canonicalReasoning(reasoningContent, reasoningField)
		toolCalls, _ := msg["tool_calls"].([]any)
		if !mirrorReasoningAsText(content, reasoning, len(toolCalls)) {
			continue
		}
		msg["content"] = reasoning
		changed = true
		patched, err := json.Marshal(msg)
		if err != nil {
			return body, false
		}
		choices[i]["message"] = patched
	}
	if !changed {
		return body, false
	}
	patchedChoices, err := json.Marshal(choices)
	if err != nil {
		return body, false
	}
	root["choices"] = patchedChoices
	out, err := json.Marshal(root)
	if err != nil {
		return body, false
	}
	return out, true
}

func isChatCompletionsPath(path string) bool {
	return strings.HasSuffix(path, "/chat/completions") ||
		strings.HasSuffix(path, "/v1/chat/completions")
}

func shouldNormalizeChatResponse(resp *http.Response) bool {
	if resp == nil || resp.StatusCode != http.StatusOK {
		return false
	}
	ct := resp.Header.Get("Content-Type")
	return strings.Contains(strings.ToLower(ct), "application/json")
}

// wrapChatCompletionResponseBody reads a non-streaming chat completion body,
// mirrors reasoning into empty content when needed, and returns a replacement
// ReadCloser plus the new length.
func wrapChatCompletionResponseBody(body io.ReadCloser, maxBytes int64) (io.ReadCloser, int64, error) {
	if body == nil {
		return nil, 0, nil
	}
	if maxBytes <= 0 {
		maxBytes = defaultMaxBodyBuffer
	}
	limited := io.LimitReader(body, maxBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		_ = body.Close()
		return nil, 0, err
	}
	if int64(len(raw)) > maxBytes {
		// Too large to normalize: stream through untouched (buffered prefix
		// chained with the unread remainder). Length unknown → caller sets -1.
		return &prefixedBody{Reader: io.MultiReader(bytes.NewReader(raw), body), closer: body}, -1, nil
	}
	_ = body.Close()
	adjusted, ok := mirrorReasoningIntoEmptyContent(raw)
	if !ok {
		return io.NopCloser(bytes.NewReader(raw)), int64(len(raw)), nil
	}
	return io.NopCloser(bytes.NewReader(adjusted)), int64(len(adjusted)), nil
}

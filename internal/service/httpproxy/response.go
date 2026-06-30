package httpproxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// mirrorReasoningIntoEmptyContent copies reasoning_content into content when
// content is empty but reasoning is present, preserving all other top-level
// response fields (usage, id, model, timings, …).
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
		rawMsg, ok := choices[i]["message"]
		if !ok {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			continue
		}
		content, _ := msg["content"].(string)
		reasoning, _ := msg["reasoning_content"].(string)
		if content == "" && reasoning != "" {
			msg["content"] = reasoning
			changed = true
			patched, err := json.Marshal(msg)
			if err != nil {
				return body, false
			}
			choices[i]["message"] = patched
		}
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
	defer body.Close()
	if maxBytes <= 0 {
		maxBytes = defaultMaxBodyBuffer
	}
	limited := io.LimitReader(body, maxBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, 0, err
	}
	if int64(len(raw)) > maxBytes {
		return io.NopCloser(bytes.NewReader(raw)), int64(len(raw)), nil
	}
	adjusted, ok := mirrorReasoningIntoEmptyContent(raw)
	if !ok {
		return io.NopCloser(bytes.NewReader(raw)), int64(len(raw)), nil
	}
	return io.NopCloser(bytes.NewReader(adjusted)), int64(len(adjusted)), nil
}

package httpproxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// mirrorReasoningStream wraps an upstream SSE chat-completions body so a
// reasoning-only completion (no content delta and no tool_call delta for a
// choice) also emits the model's reasoning as a content delta immediately
// before that choice's finishing chunk. OpenAI clients that ignore the
// reasoning/reasoning_content fields (e.g. llm-wiki via LLMWIKI_PROVIDER=openai)
// otherwise receive an assistant message with empty content and reject it as
// "reasoning but no actual response content". Every upstream frame passes
// through byte-for-byte; the mirror only inserts synthetic content deltas and
// never rewrites the original finish_reason.
func mirrorReasoningStream(body io.ReadCloser) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		err := transformReasoningStream(body, pw)
		_ = body.Close()
		_ = pw.CloseWithError(err)
	}()
	return pr
}

// transformReasoningStream copies SSE lines from src to dst verbatim, tracking
// per-choice content/tool/reasoning state so it can inject a synthetic content
// delta ahead of a reasoning-only choice's finishing chunk.
func transformReasoningStream(src io.Reader, dst io.Writer) error {
	r := bufio.NewReader(src)
	st := &reasoningStreamState{
		contentSeen: map[int]bool{},
		toolSeen:    map[int]bool{},
		reasoning:   map[int]*strings.Builder{},
	}
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if inject := st.injectionFor(line); inject != nil {
				if _, werr := dst.Write(inject); werr != nil {
					return werr
				}
			}
			if _, werr := dst.Write(line); werr != nil {
				return werr
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

type reasoningStreamState struct {
	contentSeen map[int]bool
	toolSeen    map[int]bool
	reasoning   map[int]*strings.Builder
}

// injectionFor updates per-choice state from a single SSE line and returns a
// synthetic "data: {...}\n\n" content-delta frame when the line finishes a
// reasoning-only choice, or nil when nothing must be injected before the line.
func (st *reasoningStreamState) injectionFor(line []byte) []byte {
	const prefix = "data:"
	trimmed := bytes.TrimRight(line, "\r\n")
	if !bytes.HasPrefix(trimmed, []byte(prefix)) {
		return nil
	}
	payload := bytes.TrimSpace(trimmed[len(prefix):])
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil
	}
	rawChoices, ok := root["choices"]
	if !ok {
		return nil
	}
	var choices []struct {
		Index        int            `json:"index"`
		Delta        oaiStreamDelta `json:"delta"`
		FinishReason *string        `json:"finish_reason"`
	}
	if err := json.Unmarshal(rawChoices, &choices); err != nil {
		return nil
	}
	var synChoices []map[string]any
	for _, ch := range choices {
		idx := ch.Index
		if ch.Delta.Content != "" {
			st.contentSeen[idx] = true
		}
		if len(ch.Delta.ToolCalls) > 0 {
			st.toolSeen[idx] = true
		}
		if ch.Delta.ReasoningContent != "" {
			b := st.reasoning[idx]
			if b == nil {
				b = &strings.Builder{}
				st.reasoning[idx] = b
			}
			b.WriteString(ch.Delta.ReasoningContent)
		}
		if ch.FinishReason == nil {
			continue
		}
		reasoning := ""
		if b := st.reasoning[idx]; b != nil {
			reasoning = b.String()
		}
		// Reuse the shared reasoning-only rule: sentinel content marks a choice
		// that already streamed real content, tool count marks a tool-call turn.
		seenContent := ""
		if st.contentSeen[idx] {
			seenContent = "x"
		}
		toolCount := 0
		if st.toolSeen[idx] {
			toolCount = 1
		}
		if !mirrorReasoningAsText(seenContent, reasoning, toolCount) {
			continue
		}
		synChoices = append(synChoices, map[string]any{
			"index":         idx,
			"delta":         map[string]any{"content": reasoning},
			"finish_reason": nil,
		})
		// Guard against a second injection if the choice finishes twice.
		st.contentSeen[idx] = true
	}
	if len(synChoices) == 0 {
		return nil
	}
	// Reuse the finishing chunk's envelope (id/object/created/model/...) for the
	// synthetic frame; drop choices (replaced) and usage (must not double-count).
	syn := make(map[string]json.RawMessage, len(root))
	for k, v := range root {
		if k == "choices" || k == "usage" {
			continue
		}
		syn[k] = v
	}
	cj, err := json.Marshal(synChoices)
	if err != nil {
		return nil
	}
	syn["choices"] = cj
	sj, err := json.Marshal(syn)
	if err != nil {
		return nil
	}
	out := make([]byte, 0, len(sj)+len(prefix)+4)
	out = append(out, "data: "...)
	out = append(out, sj...)
	out = append(out, '\n', '\n')
	return out
}

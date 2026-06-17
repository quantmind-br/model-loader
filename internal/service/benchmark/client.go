package benchmark

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ChatMessage is one OpenAI-compatible chat message.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the OpenAI-compatible chat completion request body. Always
// streamed with include_usage so the final chunk carries token counts.
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	// IgnoreEOS asks the backend to keep generating until MaxTokens is reached,
	// suppressing the end-of-sequence stop. llama.cpp, vLLM and SGLang all honor
	// "ignore_eos"; the throughput probe sets it so every sample generates a
	// fixed tg-token length and runs stay comparable. Standard OpenAI servers
	// ignore the unknown field harmlessly.
	IgnoreEOS bool `json:"-"`
}

// CompletionResult holds the model output plus the per-request metrics the
// benchmark records (token cost, generation speed, latency).
type CompletionResult struct {
	Content             string
	Reasoning           string // accumulated reasoning_content deltas (thinking models), kept out of Content
	PromptTokens        int
	CompletionTokens    int
	TTFT                time.Duration // time to first delta of any kind (reasoning or content)
	Total               time.Duration
	TokensPerSecond     float64 // completion tokens / generation time (decode speed)
	PromptProcessingTPS float64 // prompt tokens / TTFT (prefill speed)
	// TimingsFromServer is true when the speeds above came from the server's own
	// timings block (llama-server) instead of client-side wall-clock estimates.
	TimingsFromServer bool
}

// httpDoer abstracts *http.Client for tests.
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Complete posts a streamed chat completion to base/v1/chat/completions and
// returns the assembled content together with timing and token usage.
//
// It requests stream_options.include_usage so llama.cpp (and other OpenAI-
// compatible servers) emit a trailing chunk with a usage block. When usage is
// absent the token counts fall back to a whitespace estimate.
//
// Transient failures (transport errors, 5xx responses, a stream that dies
// mid-read) are retried once so a single network blip doesn't poison a long
// run. The timing clock restarts per attempt, so metrics always describe the
// successful attempt only. Cancellation and 4xx responses never retry.
func Complete(ctx context.Context, doer httpDoer, base, apiKey string, req ChatRequest) (CompletionResult, error) {
	res, retryable, err := completeOnce(ctx, doer, base, apiKey, req)
	if err == nil || !retryable || ctx.Err() != nil {
		return res, err
	}
	res, _, err = completeOnce(ctx, doer, base, apiKey, req)
	return res, err
}

// completeOnce performs a single streamed chat completion attempt. The bool
// reports whether a failure is worth retrying.
func completeOnce(ctx context.Context, doer httpDoer, base, apiKey string, req ChatRequest) (CompletionResult, bool, error) {
	if doer == nil {
		doer = http.DefaultClient
	}
	payload := map[string]any{
		"model":       req.Model,
		"messages":    req.Messages,
		"temperature": req.Temperature,
		"max_tokens":  req.MaxTokens,
		"stream":      true,
		"stream_options": map[string]any{
			"include_usage": true,
		},
	}
	if req.IgnoreEOS {
		// llama.cpp / vLLM / SGLang all read "ignore_eos"; vLLM additionally honors
		// "min_tokens" to refuse stopping short. Both are no-ops on servers that
		// don't recognize them.
		payload["ignore_eos"] = true
		payload["min_tokens"] = req.MaxTokens
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return CompletionResult{}, false, fmt.Errorf("marshal chat request: %w", err)
	}

	// Tolerate base URLs that already include the OpenAI "/v1" suffix (judge
	// endpoints are often documented as https://host/v1) as well as bare hosts
	// (the profile-under-test server is http://127.0.0.1:port).
	base = strings.TrimRight(base, "/")
	url := base + "/v1/chat/completions"
	if strings.HasSuffix(base, "/v1") {
		url = base + "/chat/completions"
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return CompletionResult{}, false, fmt.Errorf("create chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	start := time.Now()
	resp, err := doer.Do(httpReq)
	if err != nil {
		return CompletionResult{}, true, fmt.Errorf("post chat request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return CompletionResult{}, resp.StatusCode >= 500, fmt.Errorf("chat request failed: status %d", resp.StatusCode)
	}

	st, retryable, err := parseStream(resp.Body, start)
	// A cancelled/expired context must surface as an error — not a silently
	// truncated "successful" answer that scoring would treat as a real reply.
	// Check it before the stream error so a cancel is never reported as
	// retryable (a read error from a cancelled body otherwise looks retryable).
	if ctxErr := ctx.Err(); ctxErr != nil {
		return CompletionResult{}, false, ctxErr
	}
	if err != nil {
		return CompletionResult{}, retryable, err
	}
	return buildResult(st, start), false, nil
}

// streamState is the accumulated result of reading one SSE completion stream:
// content/reasoning text, token usage, time-to-first-token, and any trailing
// server timings block.
type streamState struct {
	content          string
	reasoning        string
	ttft             time.Duration
	promptTokens     int
	completionTokens int
	timings          *chunkTimings
}

// parseStream reads the SSE body, accumulating content/reasoning deltas, the
// time-to-first-token (first delta of any kind, measured from start), token
// usage, and any trailing server timings block. The bool reports whether a read
// failure is worth retrying; a malformed chunk is a hard (non-retryable) error.
func parseStream(body io.Reader, start time.Time) (streamState, bool, error) {
	var (
		sb       strings.Builder
		rb       strings.Builder
		st       streamState
		gotFirst bool
	)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		chunk, err := parseChunk(data)
		if err != nil {
			return streamState{}, false, err
		}
		content, reasoning := chunk.deltas()
		if content != "" || reasoning != "" {
			// TTFT counts the first emitted token of ANY kind: thinking models
			// stream reasoning_content long before the first visible content token,
			// and that work is generation too.
			if !gotFirst {
				st.ttft = time.Since(start)
				gotFirst = true
			}
			sb.WriteString(content)
			rb.WriteString(reasoning)
		}
		if chunk.Usage != nil {
			st.promptTokens = chunk.Usage.PromptTokens
			st.completionTokens = chunk.Usage.CompletionTokens
		}
		if chunk.Timings != nil {
			st.timings = chunk.Timings
		}
	}
	if err := scanner.Err(); err != nil {
		return streamState{}, true, fmt.Errorf("read stream: %w", err)
	}
	st.content = sb.String()
	st.reasoning = rb.String()
	return st, false, nil
}

// buildResult converts the accumulated streamState into a CompletionResult: it
// applies the whitespace token-count fallback, derives wall-clock decode/prefill
// speeds, then lets a server timings block override those estimates when present.
func buildResult(st streamState, start time.Time) CompletionResult {
	total := time.Since(start)
	completionTokens := st.completionTokens
	if completionTokens == 0 {
		completionTokens = estimateTokens(st.content) + estimateTokens(st.reasoning)
	}
	genSeconds := (total - st.ttft).Seconds()
	tps := 0.0
	if genSeconds > 0 && completionTokens > 0 {
		tps = float64(completionTokens) / genSeconds
	}
	ppTps := 0.0
	if st.ttft.Seconds() > 0 && st.promptTokens > 0 {
		ppTps = float64(st.promptTokens) / st.ttft.Seconds()
	}
	fromServer := false
	// llama-server reports its own prefill/decode speeds in a trailing timings
	// block; prefer them over wall-clock estimates when present.
	if st.timings != nil {
		if st.timings.PredictedPerSecond > 0 {
			tps = st.timings.PredictedPerSecond
			fromServer = true
		}
		if st.timings.PromptPerSecond > 0 {
			ppTps = st.timings.PromptPerSecond
			fromServer = true
		}
	}
	return CompletionResult{
		Content:             st.content,
		Reasoning:           st.reasoning,
		PromptTokens:        st.promptTokens,
		CompletionTokens:    completionTokens,
		TTFT:                st.ttft,
		Total:               total,
		TokensPerSecond:     tps,
		PromptProcessingTPS: ppTps,
		TimingsFromServer:   fromServer,
	}
}

// chunkTimings is llama-server's per-request timings block, appended to the
// final stream chunk. Other OpenAI-compatible servers simply omit it.
type chunkTimings struct {
	PromptPerSecond    float64 `json:"prompt_per_second"`
	PredictedPerSecond float64 `json:"predicted_per_second"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
			// ReasoningContent carries thinking-model deltas (llama.cpp
			// --reasoning-format, vLLM reasoning parsers).
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Timings *chunkTimings `json:"timings"`
}

// deltas returns the content and reasoning deltas of the chunk.
func (c streamChunk) deltas() (content, reasoning string) {
	if len(c.Choices) == 0 {
		return "", ""
	}
	return c.Choices[0].Delta.Content, c.Choices[0].Delta.ReasoningContent
}

func parseChunk(payload string) (streamChunk, error) {
	var c streamChunk
	if err := json.Unmarshal([]byte(payload), &c); err != nil {
		return streamChunk{}, fmt.Errorf("decode stream payload: %w", err)
	}
	return c, nil
}

// estimateTokens is a coarse fallback (~whitespace words) when the server
// omits a usage block.
func estimateTokens(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return len(strings.Fields(s))
}

package benchmark

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	PromptTokens        int
	CompletionTokens    int
	TTFT                time.Duration // time to first content token
	Total               time.Duration
	TokensPerSecond     float64 // completion tokens / generation time (decode speed)
	PromptProcessingTPS float64 // prompt tokens / TTFT (prefill speed)
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
func Complete(ctx context.Context, doer httpDoer, base, apiKey string, req ChatRequest) (CompletionResult, error) {
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
		return CompletionResult{}, fmt.Errorf("marshal chat request: %w", err)
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
		return CompletionResult{}, fmt.Errorf("create chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	start := time.Now()
	resp, err := doer.Do(httpReq)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("post chat request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return CompletionResult{}, fmt.Errorf("chat request failed: status %d", resp.StatusCode)
	}

	var (
		sb               strings.Builder
		ttft             time.Duration
		gotFirst         bool
		promptTokens     int
		completionTokens int
	)
	scanner := bufio.NewScanner(resp.Body)
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
			return CompletionResult{}, err
		}
		if delta := chunk.contentDelta(); delta != "" {
			if !gotFirst {
				ttft = time.Since(start)
				gotFirst = true
			}
			sb.WriteString(delta)
		}
		if chunk.Usage != nil {
			promptTokens = chunk.Usage.PromptTokens
			completionTokens = chunk.Usage.CompletionTokens
		}
	}
	// A cancelled/expired context must surface as an error — not a silently
	// truncated "successful" answer that scoring would treat as a real reply.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return CompletionResult{}, ctxErr
	}
	if err := scanner.Err(); err != nil {
		return CompletionResult{}, fmt.Errorf("read stream: %w", err)
	}

	total := time.Since(start)
	content := sb.String()
	if completionTokens == 0 {
		completionTokens = estimateTokens(content)
	}
	genSeconds := (total - ttft).Seconds()
	tps := 0.0
	if genSeconds > 0 && completionTokens > 0 {
		tps = float64(completionTokens) / genSeconds
	}
	ppTps := 0.0
	if ttft.Seconds() > 0 && promptTokens > 0 {
		ppTps = float64(promptTokens) / ttft.Seconds()
	}
	return CompletionResult{
		Content:             content,
		PromptTokens:        promptTokens,
		CompletionTokens:    completionTokens,
		TTFT:                ttft,
		Total:               total,
		TokensPerSecond:     tps,
		PromptProcessingTPS: ppTps,
	}, nil
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (c streamChunk) contentDelta() string {
	if len(c.Choices) == 0 {
		return ""
	}
	return c.Choices[0].Delta.Content
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

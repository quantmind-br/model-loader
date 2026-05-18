package playground

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ChatMessage is one OpenAI-compatible chat message.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the OpenAI-compatible chat completion request body.
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature"`
	TopP        float64       `json:"top_p"`
	MaxTokens   int           `json:"max_tokens"`
}

// StreamChunk is one streamed content fragment or terminal error.
type StreamChunk struct {
	Content string
	Done    bool
	Err     error
}

// Stream posts req to base/v1/chat/completions and emits SSE content deltas.
func Stream(ctx context.Context, base string, req ChatRequest) (<-chan StreamChunk, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}

	url := strings.TrimRight(base, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	out := make(chan StreamChunk)
	go func() {
		defer close(out)

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			sendErr(ctx, out, fmt.Errorf("post chat request: %w", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			sendErr(ctx, out, fmt.Errorf("chat request failed: status %d", resp.StatusCode))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[DONE]" {
				return
			}

			content, err := streamContent(payload)
			if err != nil {
				sendErr(ctx, out, err)
				return
			}
			if content == "" {
				continue
			}

			select {
			case <-ctx.Done():
				return
			case out <- StreamChunk{Content: content}:
			}
		}

		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			sendErr(ctx, out, fmt.Errorf("read stream: %w", err))
		}
	}()

	return out, nil
}

func streamContent(payload string) (string, error) {
	var data struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return "", fmt.Errorf("decode stream payload: %w", err)
	}
	if len(data.Choices) == 0 {
		return "", nil
	}
	return data.Choices[0].Delta.Content, nil
}

func sendErr(ctx context.Context, out chan<- StreamChunk, err error) {
	select {
	case <-ctx.Done():
	case out <- StreamChunk{Err: err, Done: true}:
	}
}

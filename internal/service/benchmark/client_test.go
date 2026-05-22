package benchmark

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestComplete_StreamsContentAndUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/chat/completions") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"choices":[{"delta":{"content":"Hello"}}]}`,
			`{"choices":[{"delta":{"content":" world"}}]}`,
			`{"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7}}`,
			`[DONE]`,
		}
		for _, c := range chunks {
			_, _ = w.Write([]byte("data: " + c + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	res, err := Complete(context.Background(), srv.Client(), srv.URL, "", ChatRequest{
		Model:    "test",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if res.Content != "Hello world" {
		t.Errorf("content = %q, want %q", res.Content, "Hello world")
	}
	if res.PromptTokens != 11 || res.CompletionTokens != 7 {
		t.Errorf("tokens = %d/%d, want 11/7", res.PromptTokens, res.CompletionTokens)
	}
	if res.Total <= 0 {
		t.Errorf("total duration = %v, want > 0", res.Total)
	}
}

func TestComplete_SetsPromptProcessingTPS(t *testing.T) {
	// Stream: two content chunks then a usage block with 100 prompt tokens.
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":2}}\n" +
		"data: [DONE]\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	got, err := Complete(context.Background(), srv.Client(), srv.URL, "", ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.PromptProcessingTPS <= 0 {
		t.Fatalf("PromptProcessingTPS = %v, want > 0", got.PromptProcessingTPS)
	}
}

func TestComplete_SendsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	_, err := Complete(context.Background(), srv.Client(), srv.URL, "secret-key", ChatRequest{Model: "t"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer secret-key")
	}
}

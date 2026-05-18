package playground

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept = %q, want text/event-stream", got)
		}

		var got ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if got.Model != "test-model" {
			t.Errorf("model = %q, want test-model", got.Model)
		}
		if !got.Stream {
			t.Errorf("stream = false, want true")
		}
		if len(got.Messages) != 1 || got.Messages[0].Content != "hello" {
			t.Errorf("messages = %#v", got.Messages)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message\n")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"hel"}}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"lo"}}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	chunks, err := Stream(context.Background(), srv.URL+"/", ChatRequest{
		Model:    "test-model",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var got strings.Builder
	for chunk := range chunks {
		if chunk.Err != nil {
			t.Fatalf("chunk error: %v", chunk.Err)
		}
		got.WriteString(chunk.Content)
	}
	if got.String() != "hello" {
		t.Errorf("content = %q, want hello", got.String())
	}
}

func TestStream_ContextCancellation(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("response writer does not flush")
		}
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"first"}}]}`+"\n\n")
		flusher.Flush()
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	chunks, err := Stream(ctx, srv.URL, ChatRequest{Model: "test-model", Stream: true})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	select {
	case chunk := <-chunks:
		if chunk.Err != nil {
			t.Fatalf("chunk error: %v", chunk.Err)
		}
		if chunk.Content != "first" {
			t.Fatalf("content = %q, want first", chunk.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first chunk")
	}

	<-started
	cancel()

	select {
	case chunk, ok := <-chunks:
		if ok {
			t.Fatalf("channel still open, got %#v", chunk)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream close")
	}
}

func TestStream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no model", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	chunks, err := Stream(context.Background(), srv.URL, ChatRequest{Model: "test-model", Stream: true})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	chunk, ok := <-chunks
	if !ok {
		t.Fatal("channel closed without error chunk")
	}
	if chunk.Err == nil {
		t.Fatal("Err = nil, want status error")
	}
	if !chunk.Done {
		t.Error("Done = false, want true")
	}
	if !strings.Contains(chunk.Err.Error(), "status 503") {
		t.Errorf("Err = %v, want status 503", chunk.Err)
	}

	chunk, ok = <-chunks
	if ok {
		t.Fatalf("channel still open, got %#v", chunk)
	}
}

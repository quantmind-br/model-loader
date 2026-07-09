package benchmark

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
	if got.PromptProcessingTPS <= 0 || got.PromptProcessingTPS > 1e7 {
		t.Fatalf("PromptProcessingTPS = %v, want a plausible positive prefill rate", got.PromptProcessingTPS)
	}
}

func TestComplete_RetriesOn5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	res, err := Complete(context.Background(), srv.Client(), srv.URL, "", ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("Complete after retry: %v", err)
	}
	if res.Content != "ok" {
		t.Errorf("content = %q, want ok", res.Content)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2 (one retry)", calls.Load())
	}
}

func TestComplete_RetriesOnTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	d := &flakyDoer{inner: srv.Client(), failures: 1}
	res, err := Complete(context.Background(), d, srv.URL, "", ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("Complete after transport retry: %v", err)
	}
	if res.Content != "ok" {
		t.Errorf("content = %q, want ok", res.Content)
	}
	if d.calls != 2 {
		t.Errorf("calls = %d, want 2", d.calls)
	}
}

// flakyDoer fails the first N Do calls with a transport error.
type flakyDoer struct {
	inner    httpDoer
	failures int
	calls    int
}

func (d *flakyDoer) Do(req *http.Request) (*http.Response, error) {
	d.calls++
	if d.calls <= d.failures {
		return nil, errors.New("connection reset by peer")
	}
	return d.inner.Do(req)
}

func TestComplete_NoRetryOn4xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	_, err := Complete(context.Background(), srv.Client(), srv.URL, "", ChatRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error on 400")
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1 (no retry on 4xx)", calls.Load())
	}
}

func TestComplete_NoRetryOnCancel(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Complete(ctx, srv.Client(), srv.URL, "", ChatRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error with cancelled ctx")
	}
	if calls.Load() > 1 {
		t.Errorf("calls = %d, want <= 1 (cancelled ctx must not retry)", calls.Load())
	}
}

func TestComplete_ReasoningContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`{"choices":[{"delta":{"reasoning_content":"thinking..."}}]}`,
			`{"choices":[{"delta":{"reasoning_content":" more"}}]}`,
			`{"choices":[{"delta":{"content":"answer"}}]}`,
			`[DONE]`,
		}
		for _, c := range chunks {
			_, _ = io.WriteString(w, "data: "+c+"\n\n")
		}
	}))
	defer srv.Close()

	res, err := Complete(context.Background(), srv.Client(), srv.URL, "", ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if res.Content != "answer" {
		t.Errorf("Content = %q, want %q (reasoning must not leak in)", res.Content, "answer")
	}
	if res.Reasoning != "thinking... more" {
		t.Errorf("Reasoning = %q, want %q", res.Reasoning, "thinking... more")
	}
	if res.TTFT <= 0 {
		t.Errorf("TTFT = %v, want > 0 (set on first reasoning delta)", res.TTFT)
	}
	// No usage block: the estimate must count reasoning tokens too.
	if res.CompletionTokens < 3 {
		t.Errorf("CompletionTokens = %d, want >= 3 (content + reasoning estimate)", res.CompletionTokens)
	}
}

func TestComplete_UsesServerTimings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`{"choices":[{"delta":{"content":"hi"}}]}`,
			`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":1},"timings":{"prompt_per_second":1234.5,"predicted_per_second":67.8}}`,
			`[DONE]`,
		}
		for _, c := range chunks {
			_, _ = io.WriteString(w, "data: "+c+"\n\n")
		}
	}))
	defer srv.Close()

	res, err := Complete(context.Background(), srv.Client(), srv.URL, "", ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !res.TimingsFromServer {
		t.Fatal("TimingsFromServer should be true when the timings block is present")
	}
	if res.PromptProcessingTPS != 1234.5 {
		t.Errorf("PromptProcessingTPS = %v, want 1234.5", res.PromptProcessingTPS)
	}
	if res.TokensPerSecond != 67.8 {
		t.Errorf("TokensPerSecond = %v, want 67.8", res.TokensPerSecond)
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

// OnDelta is throttled to at most one call per second: several deltas arriving
// within a second collapse to a single heartbeat.
func TestComplete_OnDeltaThrottle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"choices":[{"delta":{"content":"a"}}]}`,
			`{"choices":[{"delta":{"content":"b"}}]}`,
			`{"choices":[{"delta":{"content":"c"}}]}`,
			`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":3}}`,
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

	var calls atomic.Int32
	_, err := Complete(context.Background(), srv.Client(), srv.URL, "", ChatRequest{
		Model:   "m",
		OnDelta: func(int) { calls.Add(1) },
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("OnDelta calls = %d, want 1 (throttled within 1s)", n)
	}
}

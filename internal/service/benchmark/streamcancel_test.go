package benchmark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A cancelled/expired context while streaming must return an error, not a
// silently truncated "successful" partial answer that scoring would trust.
func TestComplete_ContextCancelMidStreamReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: " + `{"choices":[{"delta":{"content":"partial"}}]}` + "\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(2 * time.Second) // block past the client deadline, no [DONE]
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, err := Complete(ctx, srv.Client(), srv.URL, "", ChatRequest{
		Model:    "t",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on context cancellation, got nil (partial answer leaked as success)")
	}
}

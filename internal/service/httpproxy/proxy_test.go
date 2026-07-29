package httpproxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func backendPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	p, _ := strconv.Atoi(u.Port())
	return p
}

func TestNewReverseProxy_InjectsAuthorization(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	rp := newReverseProxy(backendPort(t, upstream), "sk-unsloth-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", defaultMaxBodyBuffer, nil)
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	if gotAuth != "Bearer sk-unsloth-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("upstream Authorization = %q", gotAuth)
	}
}

func TestNewReverseProxy_NoTokenNoHeader(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	rp := newReverseProxy(backendPort(t, upstream), "", defaultMaxBodyBuffer, nil)
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	if gotAuth != "" {
		t.Fatalf("expected no Authorization header, got %q", gotAuth)
	}
}

func TestReverseProxy_OversizedResponseNotTruncated(t *testing.T) {
	const bodyLen = 1024
	payload := `{"choices":[{"message":{"content":"` + strings.Repeat("y", bodyLen) + `"}}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush() // force chunked transfer (no Content-Length)
		}
		_, _ = io.WriteString(w, payload)
	}))
	defer upstream.Close()

	// maxBodyBuffer = 64 < payload: the normalizer must NOT truncate; the
	// oversized body streams through untouched.
	rp := newReverseProxy(backendPort(t, upstream), "", 64, nil)
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Body.String(); got != payload {
		t.Errorf("body len %d, want %d (untruncated)", len(got), len(payload))
	}
}

func TestReverseProxy_StreamMirrorsReasoningOnly(t *testing.T) {
	sse := `data: {"id":"x","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"reasoning":"why"}}]}` + "\n\n" +
		`data: {"id":"x","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = io.WriteString(w, sse)
	}))
	defer upstream.Close()

	rp := newReverseProxy(backendPort(t, upstream), "", defaultMaxBodyBuffer, nil)
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"content":"why"`) {
		t.Fatalf("stream missing mirrored content: %s", body)
	}
	if !strings.Contains(body, `"finish_reason":"length"`) {
		t.Fatalf("stream lost original finish reason: %s", body)
	}
}

func TestIsBackendUnavailableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "connection refused",
			err: &url.Error{Op: "Post", URL: "http://127.0.0.1:1", Err: &net.OpError{
				Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED,
			}},
			want: true,
		},
		{name: "EOF before response", err: io.EOF, want: true},
		{name: "request canceled", err: context.Canceled, want: false},
		{name: "request deadline", err: context.DeadlineExceeded, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBackendUnavailableError(tc.err); got != tc.want {
				t.Errorf("isBackendUnavailableError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

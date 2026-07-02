package httpproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newAnthropicMux(t *testing.T, store *stubStore, mgr *stubManager) (*Server, *http.ServeMux) {
	t.Helper()
	srv := newTestServer(t, store, mgr)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	return srv, mux
}

func postJSON(t *testing.T, mux *http.ServeMux, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

// decodeAnthropicError asserts the Anthropic error envelope shape
// {"type":"error","error":{"type":...,"message":...}} and returns its fields.
func decodeAnthropicError(t *testing.T, rr *httptest.ResponseRecorder) (errType, msg string) {
	t.Helper()
	var body struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, rr.Body.String())
	}
	if body.Type != "error" {
		t.Fatalf("envelope type = %q, want \"error\" (body: %s)", body.Type, rr.Body.String())
	}
	return body.Error.Type, body.Error.Message
}

func TestHandleAnthropicMessages_RejectsNonPOST(t *testing.T) {
	_, mux := newAnthropicMux(t, newStubStore(), newStubManager())
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/v1/messages", nil))

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
	if allow := rr.Header().Get("Allow"); allow != "POST" {
		t.Errorf("Allow = %q, want POST", allow)
	}
	if errType, _ := decodeAnthropicError(t, rr); errType != "invalid_request_error" {
		t.Errorf("error.type = %q, want invalid_request_error", errType)
	}
}

func TestHandleAnthropicMessages_Validation400(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing model", `{"max_tokens":5,"messages":[{"role":"user","content":"x"}]}`},
		{"invalid model chars", `{"model":"bad model!","max_tokens":5,"messages":[{"role":"user","content":"x"}]}`},
		{"missing max_tokens", `{"model":"alpha","messages":[{"role":"user","content":"x"}]}`},
		{"zero max_tokens", `{"model":"alpha","max_tokens":0,"messages":[{"role":"user","content":"x"}]}`},
		{"empty messages", `{"model":"alpha","max_tokens":5,"messages":[]}`},
		{"malformed JSON", `{`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := newStubManager()
			_, mux := newAnthropicMux(t, newStubStore(makeProfile("alpha", 9101)), mgr)
			rr := postJSON(t, mux, "/v1/messages", tc.body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body: %s)", rr.Code, rr.Body.String())
			}
			if errType, _ := decodeAnthropicError(t, rr); errType != "invalid_request_error" {
				t.Errorf("error.type = %q, want invalid_request_error", errType)
			}
			if mgr.launchCount() != 0 {
				t.Errorf("launches = %d, want 0 (validation must never swap)", mgr.launchCount())
			}
		})
	}
}

func TestHandleAnthropicMessages_BodyTooLarge413(t *testing.T) {
	mgr := newStubManager()
	_, mux := newAnthropicMux(t, newStubStore(makeProfile("alpha", 9101)), mgr)
	// newTestServer caps MaxBodyBuffer at 1 MiB.
	rr := postJSON(t, mux, "/v1/messages", strings.Repeat("a", (1<<20)+16))
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rr.Code)
	}
	if errType, _ := decodeAnthropicError(t, rr); errType != "request_too_large" {
		t.Errorf("error.type = %q, want request_too_large", errType)
	}
	if mgr.launchCount() != 0 {
		t.Errorf("launches = %d, want 0", mgr.launchCount())
	}
}

func TestHandleAnthropicMessages_UnknownModel404(t *testing.T) {
	mgr := newStubManager()
	_, mux := newAnthropicMux(t, newStubStore(), mgr)
	rr := postJSON(t, mux, "/v1/messages",
		`{"model":"ghost","max_tokens":5,"messages":[{"role":"user","content":"x"}]}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body: %s)", rr.Code, rr.Body.String())
	}
	errType, msg := decodeAnthropicError(t, rr)
	if errType != "not_found_error" {
		t.Errorf("error.type = %q, want not_found_error", errType)
	}
	if !strings.Contains(msg, "GET /v1/models") {
		t.Errorf("message = %q, want pointer to GET /v1/models", msg)
	}
	if mgr.launchCount() != 0 {
		t.Errorf("launches = %d, want 0", mgr.launchCount())
	}
}

// fakeOpenAIBackend spins an httptest backend that captures the upstream
// request and responds via the provided handler-body function.
type upstreamCapture struct {
	Path        string
	ContentType string
	AuthHeader  string
	Body        []byte
}

func startBackend(t *testing.T, respond func(w http.ResponseWriter)) (*httptest.Server, chan upstreamCapture) {
	t.Helper()
	caps := make(chan upstreamCapture, 8)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		caps <- upstreamCapture{
			Path:        r.URL.Path,
			ContentType: r.Header.Get("Content-Type"),
			AuthHeader:  r.Header.Get("Authorization"),
			Body:        body,
		}
		respond(w)
	}))
	t.Cleanup(backend.Close)
	return backend, caps
}

func TestHandleAnthropicMessages_NonStreaming_HappyPath(t *testing.T) {
	backend, caps := startBackend(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"finish_reason":"tool_calls","message":{
			"content":"hi","reasoning_content":"think",
			"tool_calls":[{"id":"c1","type":"function","function":{"name":"get","arguments":"{\"a\":1}"}}]}}],
			"usage":{"prompt_tokens":3,"completion_tokens":4}}`)
	})
	_, mux := newAnthropicMux(t, newStubStore(makeProfile("alpha", portOf(t, backend))), newStubManager())

	rr := postJSON(t, mux, "/v1/messages",
		`{"model":"alpha","max_tokens":32,"system":"sys","messages":[{"role":"user","content":"q"}]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
	}

	cap := <-caps
	if cap.Path != "/v1/chat/completions" {
		t.Errorf("upstream path = %q, want /v1/chat/completions", cap.Path)
	}
	if !strings.Contains(cap.ContentType, "application/json") {
		t.Errorf("upstream Content-Type = %q", cap.ContentType)
	}
	var up map[string]any
	if err := json.Unmarshal(cap.Body, &up); err != nil {
		t.Fatalf("upstream body: %v", err)
	}
	if up["model"] != "alpha" {
		t.Errorf("upstream model = %v, want alpha", up["model"])
	}
	if _, streams := up["stream"]; streams {
		t.Errorf("non-stream request must not set stream, got %v", up["stream"])
	}
	msgs, _ := up["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("upstream messages = %#v, want system + user", up["messages"])
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["type"] != "message" || resp["role"] != "assistant" || resp["model"] != "alpha" {
		t.Errorf("envelope = %v/%v/%v", resp["type"], resp["role"], resp["model"])
	}
	if id, _ := resp["id"].(string); !strings.HasPrefix(id, "msg_") {
		t.Errorf("id = %v, want msg_ prefix", resp["id"])
	}
	content, _ := resp["content"].([]any)
	if len(content) != 3 {
		t.Fatalf("content = %#v, want thinking+text+tool_use", resp["content"])
	}
	b0, _ := content[0].(map[string]any)
	b1, _ := content[1].(map[string]any)
	b2, _ := content[2].(map[string]any)
	if b0["type"] != "thinking" || b0["thinking"] != "think" {
		t.Errorf("block 0 = %#v", b0)
	}
	if b1["type"] != "text" || b1["text"] != "hi" {
		t.Errorf("block 1 = %#v", b1)
	}
	if b2["type"] != "tool_use" || b2["name"] != "get" {
		t.Errorf("block 2 = %#v", b2)
	}
	if resp["stop_reason"] != "tool_use" {
		t.Errorf("stop_reason = %v, want tool_use", resp["stop_reason"])
	}
	usage, _ := resp["usage"].(map[string]any)
	if usage["input_tokens"] != float64(3) || usage["output_tokens"] != float64(4) {
		t.Errorf("usage = %#v", usage)
	}
}

func TestHandleAnthropicMessages_ForwardsAuthToken(t *testing.T) {
	backend, caps := startBackend(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)
	})
	mgr := newStubManager()
	mgr.readyToken = "tok-123"
	_, mux := newAnthropicMux(t, newStubStore(makeProfile("alpha", portOf(t, backend))), mgr)

	rr := postJSON(t, mux, "/v1/messages",
		`{"model":"alpha","max_tokens":8,"messages":[{"role":"user","content":"x"}]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
	}
	cap := <-caps
	if cap.AuthHeader != "Bearer tok-123" {
		t.Errorf("Authorization = %q, want Bearer tok-123", cap.AuthHeader)
	}
}

func TestHandleAnthropicMessages_UpstreamErrorMapping(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		body        string
		wantStatus  int
		wantType    string
		wantMsgPart string
	}{
		{
			name:   "upstream 400 carries message",
			status: 400, body: `{"error":{"message":"bad prompt","type":"invalid_request_error"}}`,
			wantStatus: 400, wantType: "invalid_request_error", wantMsgPart: "bad prompt",
		},
		{
			name:   "upstream 500 maps to 502 api_error",
			status: 500, body: `boom`,
			wantStatus: 502, wantType: "api_error", wantMsgPart: "boom",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend, _ := startBackend(t, func(w http.ResponseWriter) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			_, mux := newAnthropicMux(t, newStubStore(makeProfile("alpha", portOf(t, backend))), newStubManager())
			rr := postJSON(t, mux, "/v1/messages",
				`{"model":"alpha","max_tokens":8,"messages":[{"role":"user","content":"x"}]}`)
			if rr.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rr.Code, tc.wantStatus, rr.Body.String())
			}
			errType, msg := decodeAnthropicError(t, rr)
			if errType != tc.wantType {
				t.Errorf("error.type = %q, want %q", errType, tc.wantType)
			}
			if !strings.Contains(msg, tc.wantMsgPart) {
				t.Errorf("message = %q, want it to contain %q", msg, tc.wantMsgPart)
			}
		})
	}
}

func TestHandleAnthropicMessages_Streaming_HappyPath(t *testing.T) {
	backend, caps := startBackend(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for _, chunk := range []string{
			`{"choices":[{"delta":{"content":"He"}}]}`,
			`{"choices":[{"delta":{"content":"y"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1}}`,
			`[DONE]`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			if fl != nil {
				fl.Flush()
			}
		}
	})
	_, mux := newAnthropicMux(t, newStubStore(makeProfile("alpha", portOf(t, backend))), newStubManager())

	rr := postJSON(t, mux, "/v1/messages",
		`{"model":"alpha","max_tokens":8,"stream":true,"messages":[{"role":"user","content":"x"}]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	cap := <-caps
	var up map[string]any
	if err := json.Unmarshal(cap.Body, &up); err != nil {
		t.Fatalf("upstream body: %v", err)
	}
	if up["stream"] != true {
		t.Errorf("upstream stream = %v, want true", up["stream"])
	}
	so, _ := up["stream_options"].(map[string]any)
	if so["include_usage"] != true {
		t.Errorf("stream_options = %#v, want include_usage:true", up["stream_options"])
	}

	frames := parseSSE(t, rr.Body.String())
	assertEventSequence(t, frames, []string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	})
	usage, _ := frames[6].Data["usage"].(map[string]any)
	if usage["input_tokens"] != float64(2) || usage["output_tokens"] != float64(1) {
		t.Errorf("usage = %#v", usage)
	}
}

func TestHandleAnthropicMessages_StreamRequestJSONResponse(t *testing.T) {
	backend, _ := startBackend(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"content":"full"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	})
	_, mux := newAnthropicMux(t, newStubStore(makeProfile("alpha", portOf(t, backend))), newStubManager())

	rr := postJSON(t, mux, "/v1/messages",
		`{"model":"alpha","max_tokens":8,"stream":true,"messages":[{"role":"user","content":"x"}]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
	}
	frames := parseSSE(t, rr.Body.String())
	assertEventSequence(t, frames, []string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	})
	d, _ := frames[3].Data["delta"].(map[string]any)
	if d["type"] != "text_delta" || d["text"] != "full" {
		t.Errorf("synthesized delta = %#v", d)
	}
}

func TestHandleAnthropicMessages_ClientDisconnectCancelsUpstream(t *testing.T) {
	backendCanceled := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		<-r.Context().Done()
		close(backendCanceled)
	}))
	t.Cleanup(backend.Close)

	_, mux := newAnthropicMux(t, newStubStore(makeProfile("alpha", portOf(t, backend))), newStubManager())
	front := httptest.NewServer(mux)
	t.Cleanup(front.Close)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "POST", front.URL+"/v1/messages",
		strings.NewReader(`{"model":"alpha","max_tokens":8,"stream":true,"messages":[{"role":"user","content":"x"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := front.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 64)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatalf("first read: %v", err)
	}
	cancel()

	select {
	case <-backendCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream request was not canceled after client disconnect")
	}
}

func TestHandleAnthropicMessages_SwapErrorAnthropicShape(t *testing.T) {
	mgr := newStubManager()
	mgr.healthFn = func(int, int) error { return fmt.Errorf("never healthy") }
	_, mux := newAnthropicMux(t, newStubStore(makeProfile("alpha", 9101)), mgr)

	rr := postJSON(t, mux, "/v1/messages",
		`{"model":"alpha","max_tokens":8,"messages":[{"role":"user","content":"x"}]}`)
	if rr.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504 (body: %s)", rr.Code, rr.Body.String())
	}
	errType, _ := decodeAnthropicError(t, rr)
	if errType != "api_error" {
		t.Errorf("error.type = %q, want api_error", errType)
	}
	// The Anthropic error object has no "code" key — its presence would mean
	// the OpenAI envelope leaked onto this route.
	var raw map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &raw)
	if inner, _ := raw["error"].(map[string]any); inner != nil {
		if _, hasCode := inner["code"]; hasCode {
			t.Errorf("Anthropic error must not carry the OpenAI code field: %#v", inner)
		}
	}
}

func TestHandleAnthropicMessages_InflightAccounting(t *testing.T) {
	release := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)
	}))
	t.Cleanup(backend.Close)
	srv, mux := newAnthropicMux(t, newStubStore(makeProfile("alpha", portOf(t, backend))), newStubManager())

	msgDone := make(chan struct{})
	go func() {
		defer close(msgDone)
		postJSON(t, mux, "/v1/messages",
			`{"model":"alpha","max_tokens":8,"messages":[{"role":"user","content":"x"}]}`)
	}()

	// The request must show up in the inflight gauge while gated.
	deadline := time.Now().Add(2 * time.Second)
	for srv.Status().InflightRequests != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("InflightRequests never reached 1 (got %d)", srv.Status().InflightRequests)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// /_admin/unload must drain (wait) for the in-flight Anthropic request.
	var unloadReturned atomic.Bool
	unloadDone := make(chan struct{})
	go func() {
		defer close(unloadDone)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("POST", "/_admin/unload?drain_timeout=5s", nil))
		unloadReturned.Store(true)
	}()

	time.Sleep(150 * time.Millisecond)
	if unloadReturned.Load() {
		t.Fatal("unload returned before the in-flight /v1/messages request finished draining")
	}
	close(release)
	<-msgDone

	select {
	case <-unloadDone:
	case <-time.After(3 * time.Second):
		t.Fatal("unload never returned after drain")
	}
	if got := srv.Status().InflightRequests; got != 0 {
		t.Errorf("InflightRequests after completion = %d, want 0", got)
	}
}

func TestHandleAnthropicCountTokens_RejectsNonPOST(t *testing.T) {
	_, mux := newAnthropicMux(t, newStubStore(), newStubManager())
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/v1/messages/count_tokens", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
	if errType, _ := decodeAnthropicError(t, rr); errType != "invalid_request_error" {
		t.Errorf("error.type = %q", errType)
	}
}

func TestHandleAnthropicCountTokens_HappyPath(t *testing.T) {
	mgr := newStubManager()
	srv, mux := newAnthropicMux(t, newStubStore(makeProfile("alpha", 9101)), mgr)
	body := `{"model":"alpha","messages":[{"role":"user","content":"12345678"}]}`

	rr := postJSON(t, mux, "/v1/messages/count_tokens", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
	}
	var resp anthropicCountTokensResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Real tiktoken count over the translated OpenAI request (not the byte
	// heuristic) — "user"/"12345678" tokenize to a handful of tokens.
	if resp.InputTokens <= 0 {
		t.Errorf("input_tokens = %d, want > 0 (real tokenizer count)", resp.InputTokens)
	}

	again := postJSON(t, mux, "/v1/messages/count_tokens", body)
	var resp2 anthropicCountTokensResponse
	_ = json.Unmarshal(again.Body.Bytes(), &resp2)
	if resp2.InputTokens != resp.InputTokens {
		t.Errorf("estimator not deterministic over HTTP: %d != %d", resp2.InputTokens, resp.InputTokens)
	}

	if mgr.launchCount() != 0 {
		t.Errorf("count_tokens must never launch a backend, launches = %d", mgr.launchCount())
	}
	if got := srv.Status().InflightRequests; got != 0 {
		t.Errorf("count_tokens must not participate in inflight accounting, got %d", got)
	}
}

func TestHandleAnthropicCountTokens_UnknownModel404(t *testing.T) {
	_, mux := newAnthropicMux(t, newStubStore(), newStubManager())
	rr := postJSON(t, mux, "/v1/messages/count_tokens",
		`{"model":"ghost","messages":[{"role":"user","content":"x"}]}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body: %s)", rr.Code, rr.Body.String())
	}
	errType, msg := decodeAnthropicError(t, rr)
	if errType != "not_found_error" || !strings.Contains(msg, "GET /v1/models") {
		t.Errorf("error = %q %q", errType, msg)
	}
}

func TestWriteAnthropicError_Shape(t *testing.T) {
	rr := httptest.NewRecorder()
	writeAnthropicError(rr, http.StatusTeapot, "invalid_request_error", "oops")
	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	want := `{"type":"error","error":{"type":"invalid_request_error","message":"oops"}}`
	if strings.TrimSpace(rr.Body.String()) != want {
		t.Errorf("body = %q, want %q", strings.TrimSpace(rr.Body.String()), want)
	}
}

package httpproxy

import (
	"bytes"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractProfileID(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		path        string
		contentType string
		body        string
		want        string
		wantBody    string // body the handler will forward downstream
	}{
		{
			name:        "json body with model field",
			method:      "POST",
			path:        "/v1/chat/completions",
			contentType: "application/json",
			body:        `{"model":"qwen-7b","messages":[]}`,
			want:        "qwen-7b",
			wantBody:    `{"model":"qwen-7b","messages":[]}`,
		},
		{
			name:        "litellm openai prefix stripped from json model",
			method:      "POST",
			path:        "/v1/chat/completions",
			contentType: "application/json",
			body:        `{"model":"openai/alpha-profile","messages":[]}`,
			want:        "alpha-profile",
			wantBody:    `{"model":"openai/alpha-profile","messages":[]}`,
		},
		{
			name:        "json body without model field",
			method:      "POST",
			path:        "/v1/chat/completions",
			contentType: "application/json",
			body:        `{"messages":[]}`,
			want:        "",
			wantBody:    `{"messages":[]}`,
		},
		{
			name:        "query fallback when body lacks model",
			method:      "POST",
			path:        "/tokenize?model=llama-3",
			contentType: "application/json",
			body:        `{"content":"hi"}`,
			want:        "llama-3",
			wantBody:    `{"content":"hi"}`,
		},
		{
			name:        "query fallback when no body",
			method:      "GET",
			path:        "/props?model=llama-3",
			contentType: "",
			body:        "",
			want:        "llama-3",
			wantBody:    "",
		},
		{
			name:        "body wins over query when both present",
			method:      "POST",
			path:        "/v1/chat/completions?model=ignored",
			contentType: "application/json",
			body:        `{"model":"body-wins"}`,
			want:        "body-wins",
			wantBody:    `{"model":"body-wins"}`,
		},
		{
			name:        "non-json body uses query fallback",
			method:      "POST",
			path:        "/anything?model=via-query",
			contentType: "text/plain",
			body:        "raw text",
			want:        "via-query",
			wantBody:    "raw text",
		},
		{
			name:        "invalid json body returns empty",
			method:      "POST",
			path:        "/v1/chat/completions",
			contentType: "application/json",
			body:        `not json`,
			want:        "",
			wantBody:    `not json`,
		},
		{
			name:        "nothing specified returns empty",
			method:      "GET",
			path:        "/health",
			contentType: "",
			body:        "",
			want:        "",
			wantBody:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			}
			r := httptest.NewRequest(tt.method, tt.path, body)
			if tt.contentType != "" {
				r.Header.Set("Content-Type", tt.contentType)
			}
			got := extractProfileID(r, defaultMaxBodyBuffer)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			// Verify body was either left intact (non-JSON) or re-attached
			// faithfully (JSON path).
			if r.Body != nil {
				gotBody, _ := io.ReadAll(r.Body)
				if string(gotBody) != tt.wantBody {
					t.Errorf("downstream body = %q, want %q", string(gotBody), tt.wantBody)
				}
			} else if tt.wantBody != "" {
				t.Errorf("nil body but want %q", tt.wantBody)
			}
		})
	}
}

func TestExtractProfileID_BodyAboveCap(t *testing.T) {
	// Caller sets Content-Length above cap; extractFromBody must short-circuit
	// without buffering.
	huge := bytes.Repeat([]byte("a"), 16<<20) // 16 MiB
	r := httptest.NewRequest("POST", "/v1/chat/completions?model=fallback", bytes.NewReader(huge))
	r.Header.Set("Content-Type", "application/json")
	r.ContentLength = int64(len(huge))

	got := extractProfileID(r, 1<<20) // 1 MiB cap
	if got != "fallback" {
		t.Errorf("got %q, want fallback (query)", got)
	}
}

func TestExtractFromBody_UnknownLengthOverCapForwardsUntruncated(t *testing.T) {
	// Chunked/streamed request (ContentLength == -1) whose body exceeds the
	// buffer cap must be forwarded UNTRUNCATED, with model extraction skipped.
	const cap = 1 << 10 // 1 KiB cap
	body := []byte(`{"model":"m",` + strings.Repeat("x", cap+100) + `}`)
	r := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.ContentLength = -1 // unknown length (chunked)

	got := extractFromBody(r, cap)
	if got != "" {
		t.Errorf("got %q, want empty (extraction skipped for oversized body)", got)
	}
	if r.ContentLength != -1 {
		t.Errorf("ContentLength = %d, want -1 (pass-through)", r.ContentLength)
	}
	forwarded, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read forwarded body: %v", err)
	}
	if !bytes.Equal(forwarded, body) {
		t.Errorf("forwarded body len %d, want original len %d (untruncated)", len(forwarded), len(body))
	}
}

func TestNormalizeRequestModel(t *testing.T) {
	cases := map[string]string{
		"":               "",
		"  qwen-7b  ":    "qwen-7b",
		"openai/qwen-7b": "qwen-7b",
		"openai/ornith-aeon-35b-a3b-q4km-mtp-vision-layer2-256k": "ornith-aeon-35b-a3b-q4km-mtp-vision-layer2-256k",
		"hosted_vllm/my-profile":                                 "my-profile",
		"no-slash-id":                                            "no-slash-id",
	}
	for in, want := range cases {
		if got := normalizeRequestModel(in); got != want {
			t.Errorf("normalizeRequestModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidProfileID(t *testing.T) {
	cases := map[string]bool{
		"":                       false,
		"qwen-7b":                true,
		"qwen_7b.gguf":           true,
		"../etc/passwd":          false,
		"abc/def":                false,
		"abc def":                false,
		"a":                      true,
		strings.Repeat("a", 257): false,
	}
	for id, want := range cases {
		if got := validProfileID(id); got != want {
			t.Errorf("validProfileID(%q) = %v, want %v", id, got, want)
		}
	}
}

package hfhub

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := &Client{
		http:      srv.Client(),
		baseURL:   srv.URL,
		userAgent: "model-loader-test/1.0",
	}
	return c, srv
}

func TestSearch_Success(t *testing.T) {
	body := `[
		{
			"id": "microsoft/Phi-3-mini-4k-instruct",
			"author": "microsoft",
			"modelId": "microsoft/Phi-3-mini-4k-instruct",
			"tags": ["pytorch", "gguf"],
			"downloads": 1234,
			"likes": 56,
			"lastModified": "2024-04-22T12:34:56.000Z",
			"library_name": "transformers",
			"pipeline_tag": "text-generation"
		},
		{
			"id": "TheBloke/Llama-2-7B-GGUF",
			"author": "TheBloke",
			"modelId": "TheBloke/Llama-2-7B-GGUF",
			"tags": ["gguf"],
			"downloads": 99,
			"likes": 7,
			"lastModified": "2024-01-01T00:00:00Z",
			"library_name": "",
			"pipeline_tag": ""
		}
	]`

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models" {
			t.Errorf("path = %q, want /api/models", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("search"); got != "phi" {
			t.Errorf("search = %q, want phi", got)
		}
		if got := q.Get("limit"); got != "5" {
			t.Errorf("limit = %q, want 5", got)
		}
		if got := q.Get("full"); got != "true" {
			t.Errorf("full = %q, want true", got)
		}
		if got := r.Header.Get("User-Agent"); got != "model-loader-test/1.0" {
			t.Errorf("User-Agent = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	})

	results, err := c.Search(context.Background(), "phi", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].ID != "microsoft/Phi-3-mini-4k-instruct" {
		t.Errorf("results[0].ID = %q", results[0].ID)
	}
	if results[0].Downloads != 1234 {
		t.Errorf("results[0].Downloads = %d, want 1234", results[0].Downloads)
	}
	if !results[0].HasGGUFTag() {
		t.Errorf("results[0].HasGGUFTag() = false, want true")
	}
	if results[0].LibraryName != "transformers" {
		t.Errorf("results[0].LibraryName = %q", results[0].LibraryName)
	}
	if results[0].LastModified.IsZero() {
		t.Errorf("results[0].LastModified is zero")
	}
	wantTime, _ := time.Parse(time.RFC3339, "2024-04-22T12:34:56Z")
	if !results[0].LastModified.Equal(wantTime) {
		t.Errorf("results[0].LastModified = %v, want %v", results[0].LastModified, wantTime)
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("search"); got != "" {
			t.Errorf("search = %q, want empty", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[]`)
	})

	results, err := c.Search(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("len(results) = %d, want 0", len(results))
	}
}

func TestSearch_HTTPError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	_, err := c.Search(context.Background(), "x", 1)
	if err == nil {
		t.Fatal("Search: want error, got nil")
	}
	var httpErr *ErrHTTP
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %T %v, want *ErrHTTP", err, err)
	}
	if httpErr.Status != http.StatusInternalServerError {
		t.Errorf("httpErr.Status = %d, want 500", httpErr.Status)
	}
	if !strings.Contains(httpErr.URL, "/api/models") {
		t.Errorf("httpErr.URL = %q, want to contain /api/models", httpErr.URL)
	}
}

func TestSearch_RateLimit(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"a/b","author":"a","modelId":"a/b","tags":[],"downloads":0,"likes":0,"lastModified":"","library_name":"","pipeline_tag":""}]`)
	})

	start := time.Now()
	results, err := c.Search(context.Background(), "z", 1)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Search after 429 retry: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2 (initial + retry)", calls.Load())
	}
	if len(results) != 1 || results[0].ID != "a/b" {
		t.Errorf("results = %+v, want one entry with ID a/b", results)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("elapsed = %v, expected >=1s Retry-After honored", elapsed)
	}
}

func TestRepoInfo_Success(t *testing.T) {
	body := `{
		"id": "microsoft/Phi-3-mini-4k-instruct",
		"tags": ["gguf", "text-generation"],
		"siblings": [
			{"rfilename": "config.json", "size": 1024},
			{"rfilename": "model.gguf", "size": 1073741824},
			{"rfilename": "tokenizer/tokenizer.json", "size": 2048}
		]
	}`

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		want := "/api/models/microsoft/Phi-3-mini-4k-instruct"
		if r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		if got := r.Header.Get("User-Agent"); got != "model-loader-test/1.0" {
			t.Errorf("User-Agent = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	})

	info, err := c.RepoInfo(context.Background(), "microsoft/Phi-3-mini-4k-instruct")
	if err != nil {
		t.Fatalf("RepoInfo: %v", err)
	}
	if info.ID != "microsoft/Phi-3-mini-4k-instruct" {
		t.Errorf("info.ID = %q", info.ID)
	}
	if len(info.Siblings) != 3 {
		t.Fatalf("len(siblings) = %d, want 3", len(info.Siblings))
	}
	if info.Siblings[1].RFilename != "model.gguf" || info.Siblings[1].Size != 1073741824 {
		t.Errorf("siblings[1] = %+v", info.Siblings[1])
	}
	if info.Siblings[2].RFilename != "tokenizer/tokenizer.json" {
		t.Errorf("siblings[2].RFilename = %q (subpath must be preserved)", info.Siblings[2].RFilename)
	}
}

func TestRepoInfo_LFSSizeAndBlobsParam(t *testing.T) {
	// HF only populates per-sibling sizes when ?blobs=true is requested.
	// For LFS-tracked files (GGUF), the real size lives in lfs.size while the
	// top-level size is the pointer size; we must prefer lfs.size.
	body := `{
		"id": "org/model",
		"tags": ["gguf"],
		"siblings": [
			{"rfilename": "config.json", "size": 1024},
			{"rfilename": "model.gguf", "size": 135, "lfs": {"size": 5368709120}}
		]
	}`

	var gotQuery string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	})

	info, err := c.RepoInfo(context.Background(), "org/model")
	if err != nil {
		t.Fatalf("RepoInfo: %v", err)
	}
	if !strings.Contains(gotQuery, "blobs=true") {
		t.Errorf("query = %q, want it to contain blobs=true", gotQuery)
	}
	if info.Siblings[0].Size != 1024 {
		t.Errorf("non-LFS size = %d, want 1024", info.Siblings[0].Size)
	}
	if info.Siblings[1].Size != 5368709120 {
		t.Errorf("LFS size = %d, want lfs.size 5368709120", info.Siblings[1].Size)
	}
}

func TestRepoInfo_NotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such repo", http.StatusNotFound)
	})

	_, err := c.RepoInfo(context.Background(), "ghost/none")
	if err == nil {
		t.Fatal("RepoInfo: want error, got nil")
	}
	var httpErr *ErrHTTP
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %T %v, want *ErrHTTP", err, err)
	}
	if httpErr.Status != http.StatusNotFound {
		t.Errorf("httpErr.Status = %d, want 404", httpErr.Status)
	}
}

func TestOpenDownload_Success(t *testing.T) {
	payload := []byte("GGUF\x00synthetic-binary-payload")
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		want := "/microsoft/Phi-3-mini-4k-instruct/resolve/main/model.gguf"
		if r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		if got := r.Header.Get("User-Agent"); got != "model-loader-test/1.0" {
			t.Errorf("User-Agent = %q", got)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "29")
		_, _ = w.Write(payload)
	})

	rc, size, err := c.OpenDownload(context.Background(), "microsoft/Phi-3-mini-4k-instruct", "model.gguf")
	if err != nil {
		t.Fatalf("OpenDownload: %v", err)
	}
	defer rc.Close()
	if size != int64(len(payload)) {
		t.Errorf("size = %d, want %d", size, len(payload))
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("body = %q, want %q", got, payload)
	}
}

func TestOpenDownload_SubdirFilename(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		want := "/org/repo/resolve/main/sub/dir/file.bin"
		if r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		_, _ = w.Write([]byte("ok"))
	})

	rc, _, err := c.OpenDownload(context.Background(), "org/repo", "sub/dir/file.bin")
	if err != nil {
		t.Fatalf("OpenDownload: %v", err)
	}
	_ = rc.Close()
}

func TestOpenDownload_NotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	})

	rc, _, err := c.OpenDownload(context.Background(), "org/repo", "model.gguf")
	if err == nil {
		if rc != nil {
			_ = rc.Close()
		}
		t.Fatal("OpenDownload: want error, got nil")
	}
	var httpErr *ErrHTTP
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %T %v, want *ErrHTTP", err, err)
	}
	if httpErr.Status != http.StatusNotFound {
		t.Errorf("httpErr.Status = %d, want 404", httpErr.Status)
	}
}

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"3", 3 * time.Second},
		{"   7   ", 7 * time.Second},
		{"-1", 0},
		{"garbage", 0},
	}
	for _, tc := range cases {
		if got := parseRetryAfter(tc.in); got != tc.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

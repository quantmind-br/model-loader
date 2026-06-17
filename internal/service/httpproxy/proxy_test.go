package httpproxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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

	rp := newReverseProxy(backendPort(t, upstream), "sk-unsloth-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
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

	rp := newReverseProxy(backendPort(t, upstream), "")
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	if gotAuth != "" {
		t.Fatalf("expected no Authorization header, got %q", gotAuth)
	}
}

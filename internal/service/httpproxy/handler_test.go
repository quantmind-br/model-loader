package httpproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func portOf(t *testing.T, ts *httptest.Server) int {
	t.Helper()
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return port
}

func TestHandleModelsList_ReturnsAllProfiles(t *testing.T) {
	store := newStubStore(
		domain.Profile{
			ID:     "alpha",
			Name:   "Alpha",
			Args:   map[string]any{"ctx-size": float64(4096)},
			Launch: domain.LaunchConfig{BackendID: "llama-cpp-default"},
		},
		domain.Profile{ID: "beta", Name: "Beta"},
	)
	srv := newTestServer(t, store, newStubManager())
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/v1/models", nil))

	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body modelsResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Object != "list" {
		t.Errorf("object = %q, want list", body.Object)
	}
	if len(body.Data) != 2 {
		t.Fatalf("data len = %d, want 2", len(body.Data))
	}
	byID := make(map[string]orModel, len(body.Data))
	for _, m := range body.Data {
		byID[m.ID] = m
	}
	if _, ok := byID["alpha"]; !ok {
		t.Errorf("missing alpha: %v", body.Data)
	}
	if _, ok := byID["beta"]; !ok {
		t.Errorf("missing beta: %v", body.Data)
	}
	// OpenRouter-shaped enrichment: canonical_slug mirrors id, context_length
	// is derived from args, pricing is zeroed, arch defaults to text-only.
	a := byID["alpha"]
	if a.Object != "model" {
		t.Errorf("object = %q, want model", a.Object)
	}
	if a.OwnedBy != "llama-cpp-default" {
		t.Errorf("owned_by = %q, want llama-cpp-default", a.OwnedBy)
	}
	if a.CanonicalSlug != "alpha" {
		t.Errorf("canonical_slug = %q, want alpha", a.CanonicalSlug)
	}
	if a.ContextLength == nil || *a.ContextLength != 4096 {
		t.Errorf("alpha context_length = %v, want 4096", derefInt(a.ContextLength))
	}
	if a.Pricing.Prompt != "0" {
		t.Errorf("pricing.prompt = %q, want 0", a.Pricing.Prompt)
	}
	if a.Architecture.Modality != "text->text" {
		t.Errorf("modality = %q, want text->text", a.Architecture.Modality)
	}
}

func TestHandleModelsList_RejectsNonGET(t *testing.T) {
	srv := newTestServer(t, newStubStore(), newStubManager())
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/v1/models", nil))

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
	if rr.Header().Get("Allow") != "GET" {
		t.Errorf("Allow = %q, want GET", rr.Header().Get("Allow"))
	}
}

func TestHandleForward_NoModelSpecifiedAndNothingLoaded_503(t *testing.T) {
	srv := newTestServer(t, newStubStore(), newStubManager())
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/v1/chat/completions", nil))

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
	var body struct {
		Error struct {
			Type string `json:"type"`
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if body.Error.Type != "model_not_loaded" {
		t.Errorf("error.type = %q, want model_not_loaded", body.Error.Type)
	}
}

func TestHandleForward_UnknownProfile_404(t *testing.T) {
	srv := newTestServer(t, newStubStore(), newStubManager())
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	body := strings.NewReader(`{"model":"ghost","messages":[]}`)
	r := httptest.NewRequest("POST", "/v1/chat/completions", body)
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, r)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestHandleForward_InvalidProfileID_400(t *testing.T) {
	srv := newTestServer(t, newStubStore(), newStubManager())
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	body := strings.NewReader(`{"model":"../etc/passwd"}`)
	r := httptest.NewRequest("POST", "/v1/chat/completions", body)
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHandleForward_LiteLLMOpenAIPrefix_RoutesToBackend(t *testing.T) {
	hits := make(chan string, 4)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- r.URL.Path
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer backend.Close()

	store := newStubStore(makeProfile("alpha", portOf(t, backend)))
	mgr := newStubManager()
	mgr.nextPID = 5001
	srv := newTestServer(t, store, mgr)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	body := strings.NewReader(`{"model":"openai/alpha","messages":[]}`)
	r := httptest.NewRequest("POST", "/v1/chat/completions", body)
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, r)

	if rr.Code != 200 {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	select {
	case path := <-hits:
		if path != "/v1/chat/completions" {
			t.Errorf("backend saw path %q", path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backend did not receive request")
	}
}

// TestHandleForward_RoutesToBackend stands up a real httptest backend,
// makes the stub manager return that backend's port, and verifies the
// catch-all forwarder lands a request on it.
func TestHandleForward_RoutesToBackend(t *testing.T) {
	hits := make(chan string, 4)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- r.URL.Path
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, `{"echo":%q,"path":%q}`, string(body), r.URL.Path)
	}))
	defer backend.Close()

	store := newStubStore(makeProfile("alpha", portOf(t, backend)))
	mgr := newStubManager()
	// Force Launch to return an instance whose Port matches the real backend.
	mgr.nextPID = 5000
	srv := newTestServer(t, store, mgr)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	body := strings.NewReader(`{"model":"alpha","messages":[]}`)
	r := httptest.NewRequest("POST", "/v1/chat/completions", body)
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, r)

	if rr.Code != 200 {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	select {
	case path := <-hits:
		if path != "/v1/chat/completions" {
			t.Errorf("backend saw path %q, want /v1/chat/completions", path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backend did not receive request")
	}
}

func TestHandleForward_CatchAllPreservesArbitraryPaths(t *testing.T) {
	hits := make(chan string, 4)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- r.URL.Path
		w.WriteHeader(200)
	}))
	defer backend.Close()

	store := newStubStore(makeProfile("alpha", portOf(t, backend)))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	for _, path := range []string{"/props", "/tokenize", "/some/exotic/route"} {
		r := httptest.NewRequest("GET", path+"?model=alpha", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, r)
		if rr.Code != 200 {
			t.Errorf("path %s: status %d body=%s", path, rr.Code, rr.Body.String())
		}
		select {
		case got := <-hits:
			if got != path {
				t.Errorf("backend saw %q, want %q", got, path)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("backend did not receive %s", path)
		}
	}
}

func TestHandleStatus_ReturnsCurrentStatus(t *testing.T) {
	srv := newTestServer(t, newStubStore(), newStubManager())
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/_status", nil))

	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var st Status
	if err := json.NewDecoder(rr.Body).Decode(&st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.Running {
		t.Errorf("Running = true, want false (server not started)")
	}
}

func TestHandleStatus_RejectsNonGET(t *testing.T) {
	srv := newTestServer(t, newStubStore(), newStubManager())
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/_status", nil))

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
	if rr.Header().Get("Allow") != "GET" {
		t.Errorf("Allow = %q, want GET", rr.Header().Get("Allow"))
	}
}

// TestHandleStatus_PropagateLoadedProfileID is a regression test for the
// serialization layer. Before the fix httpproxy.Status had no JSON tags, so
// the wire field name was Go-default PascalCase. The test would have failed
// because it asserts the snake_case key that the supervisor decoder now
// expects.
func TestHandleStatus_PropagateLoadedProfileID(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9101))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop(context.Background())

	addr := srv.Status().Addr
	if addr == "" {
		t.Fatal("server did not bind")
	}

	triggerURL := "http://" + addr + "/v1/chat/completions?model=alpha"
	client := &http.Client{Timeout: 2 * time.Second}
	_, _ = client.Post(triggerURL, "application/json", nil)

	statusURL := "http://" + addr + "/_status"
	resp, err := client.Get(statusURL)
	if err != nil {
		t.Fatalf("status probe: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want 200", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if !bytes.Contains(raw, []byte(`"loaded_profile_id"`)) {
		t.Fatalf("JSON missing loaded_profile_id; body=%s", raw)
	}

	st := Status{Running: true, Addr: addr}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("decode like supervisor: %v", err)
	}
	if st.LoadedProfileID != "alpha" {
		t.Errorf("LoadedProfileID = %q, want alpha", st.LoadedProfileID)
	}
}

func TestStatus_UnmarshalJSON_BackwardCompatibility(t *testing.T) {
	oldFormat := `{
		"Running": true,
		"Addr": "127.0.0.1:4321",
		"LoadedProfileID": "hunyuan-mt-7b",
		"LoadedPID": 61816,
		"LoadedPort": 4329,
		"InflightRequests": 3,
		"LastError": "backend timeout"
	}`
	var st Status
	if err := json.Unmarshal([]byte(oldFormat), &st); err != nil {
		t.Fatalf("unmarshal old format: %v", err)
	}
	if st.LoadedProfileID != "hunyuan-mt-7b" {
		t.Errorf("LoadedProfileID = %q, want hunyuan-mt-7b", st.LoadedProfileID)
	}
	if st.LoadedPID != 61816 {
		t.Errorf("LoadedPID = %d, want 61816", st.LoadedPID)
	}
	if st.LoadedPort != 4329 {
		t.Errorf("LoadedPort = %d, want 4329", st.LoadedPort)
	}
	if st.InflightRequests != 3 {
		t.Errorf("InflightRequests = %d, want 3", st.InflightRequests)
	}
	if st.LastError != "backend timeout" {
		t.Errorf("LastError = %q, want 'backend timeout'", st.LastError)
	}

	mixed := `{"loaded_profile_id":"new","LoadedProfileID":"old"}`
	var st2 Status
	if err := json.Unmarshal([]byte(mixed), &st2); err != nil {
		t.Fatalf("unmarshal mixed format: %v", err)
	}
	if st2.LoadedProfileID != "new" {
		t.Errorf("LoadedProfileID = %q, want new (snake_case wins)", st2.LoadedProfileID)
	}
}

// TestHandleModelsList_AnthropicEnvelopeFields pins the additive Anthropic
// list-envelope keys: has_more (always false — no pagination), first_id and
// last_id (ids of the first/last returned items, null on an empty store).
func TestHandleModelsList_AnthropicEnvelopeFields(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9101), makeProfile("beta", 9102))
	srv := newTestServer(t, store, newStubManager())
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/v1/models", nil))
	if rr.Code != 200 {
		t.Fatalf("status = %d", rr.Code)
	}
	var body struct {
		Object  string  `json:"object"`
		HasMore *bool   `json:"has_more"`
		FirstID *string `json:"first_id"`
		LastID  *string `json:"last_id"`
		Data    []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Object != "list" {
		t.Errorf("object = %q, want list (OpenAI envelope preserved)", body.Object)
	}
	if body.HasMore == nil || *body.HasMore {
		t.Errorf("has_more = %v, want explicit false", body.HasMore)
	}
	if len(body.Data) != 2 {
		t.Fatalf("data len = %d, want 2", len(body.Data))
	}
	// stubStore iterates a map: compare against the returned order, not ids.
	if body.FirstID == nil || *body.FirstID != body.Data[0].ID {
		t.Errorf("first_id = %v, want %q", body.FirstID, body.Data[0].ID)
	}
	if body.LastID == nil || *body.LastID != body.Data[len(body.Data)-1].ID {
		t.Errorf("last_id = %v, want %q", body.LastID, body.Data[len(body.Data)-1].ID)
	}

	// Empty store: first_id/last_id must serialize as JSON null.
	srvEmpty := newTestServer(t, newStubStore(), newStubManager())
	muxEmpty := http.NewServeMux()
	srvEmpty.registerRoutes(muxEmpty)
	rrEmpty := httptest.NewRecorder()
	muxEmpty.ServeHTTP(rrEmpty, httptest.NewRequest("GET", "/v1/models", nil))
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rrEmpty.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode empty: %v", err)
	}
	for _, k := range []string{"first_id", "last_id"} {
		v, ok := raw[k]
		if !ok {
			t.Errorf("empty list envelope missing %q", k)
			continue
		}
		if string(v) != "null" {
			t.Errorf("%s = %s, want null on empty list", k, v)
		}
	}
	if string(raw["has_more"]) != "false" {
		t.Errorf("has_more = %s, want false", raw["has_more"])
	}
}

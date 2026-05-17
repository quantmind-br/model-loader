package httpproxy

import (
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
		domain.Profile{ID: "alpha", Name: "Alpha"},
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
	ids := map[string]bool{body.Data[0].ID: true, body.Data[1].ID: true}
	if !ids["alpha"] || !ids["beta"] {
		t.Errorf("missing expected ids: %v", body.Data)
	}
	for _, m := range body.Data {
		if m.Object != "model" {
			t.Errorf("entry object = %q, want model", m.Object)
		}
		if m.OwnedBy != "model-loader" {
			t.Errorf("owned_by = %q, want model-loader", m.OwnedBy)
		}
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

package proxysupervisor

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
)

// testSupervisor returns a Supervisor whose host/port point at ts.
func testSupervisor(t *testing.T, ts *httptest.Server) *Supervisor {
	t.Helper()
	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	return New(Config{Host: host, Port: port, StatePath: filepath.Join(t.TempDir(), "state.json"), LogDir: t.TempDir()})
}

func TestLoad_PostsProfileID(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/_admin/load" {
			t.Errorf("path = %s, want /_admin/load", r.URL.Path)
		}
		var body struct {
			ProfileID string `json:"profile_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body.ProfileID != "p1" {
			t.Errorf("profile_id = %q, want p1", body.ProfileID)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(httpproxy.Status{
			Running:         true,
			LoadedProfileID: "p1",
			LoadedPID:       42,
			LoadedLogPath:   "/tmp/p1.log",
		})
	}))
	defer ts.Close()

	s := testSupervisor(t, ts)
	st, err := s.Load(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !st.Running {
		t.Errorf("Running = false, want true")
	}
	if st.LoadedProfileID != "p1" {
		t.Errorf("LoadedProfileID = %q, want p1", st.LoadedProfileID)
	}
	if st.LoadedPID != 42 {
		t.Errorf("LoadedPID = %d, want 42", st.LoadedPID)
	}
	if st.LoadedLogPath != "/tmp/p1.log" {
		t.Errorf("LoadedLogPath = %q, want /tmp/p1.log", st.LoadedLogPath)
	}
}

func TestLoad_SurfacesOpenAIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"profile \"nope\" not found","type":"invalid_request_error"}}`))
	}))
	defer ts.Close()

	s := testSupervisor(t, ts)
	_, err := s.Load(context.Background(), "nope")
	if err == nil {
		t.Fatal("Load: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "not found")
	}
}

func TestUnload_Posts(t *testing.T) {
	var gotPath, gotQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"running":true}`))
	}))
	defer ts.Close()

	s := testSupervisor(t, ts)
	st, err := s.Unload(context.Background(), true)
	if err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if gotPath != "/_admin/unload" {
		t.Errorf("path = %q, want /_admin/unload", gotPath)
	}
	if gotQuery != "force=true" {
		t.Errorf("query = %q, want force=true", gotQuery)
	}
	if !st.Running {
		t.Errorf("Running = false, want true")
	}
}

func TestBaseURL(t *testing.T) {
	s := New(Config{Host: "127.0.0.1", Port: 4321})
	if got := s.BaseURL(); got != "http://127.0.0.1:4321" {
		t.Fatalf("BaseURL = %q", got)
	}
}

// EnsureRunning's not-running path is intentionally untested here: with
// Status().Running == false it falls through to Start, which spawns a real
// detached OS process via the binary's "serve" subcommand — not something a
// unit test should do. The no-op path (already running) requires s.state to be
// populated with a live PID and an open port, which only Start/Reconcile can
// set, so it would equally require a real process. The method is a trivial
// two-line composition of Status and Start, both covered elsewhere.

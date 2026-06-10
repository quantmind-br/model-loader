package httpproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/processmgr"
)

// -----------------------------------------------------------------------------
// /_admin/load
// -----------------------------------------------------------------------------

func TestHandleAdminLoad_RejectsNonPOST(t *testing.T) {
	srv := newTestServer(t, newStubStore(), newStubManager())
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/_admin/load", nil))

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
	if rr.Header().Get("Allow") != "POST" {
		t.Errorf("Allow = %q, want POST", rr.Header().Get("Allow"))
	}
}

func TestHandleAdminLoad_EmptyBody_400(t *testing.T) {
	srv := newTestServer(t, newStubStore(), newStubManager())
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/_admin/load", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Error struct {
			Type string `json:"type"`
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if body.Error.Type != "invalid_request_error" {
		t.Errorf("error.type = %q, want invalid_request_error", body.Error.Type)
	}
}

func TestHandleAdminLoad_InvalidProfileID_400(t *testing.T) {
	srv := newTestServer(t, newStubStore(), newStubManager())
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/_admin/load",
		strings.NewReader(`{"profile_id":"../etc/passwd"}`))
	r.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHandleAdminLoad_UnknownProfile_404(t *testing.T) {
	srv := newTestServer(t, newStubStore(), newStubManager())
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/_admin/load",
		strings.NewReader(`{"profile_id":"ghost"}`))
	r.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rr, r)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleAdminLoad_FirstCallLaunches_200(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9201))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/_admin/load",
		strings.NewReader(`{"profile_id":"alpha"}`))
	r.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if mgr.launchCount() != 1 || mgr.killCount() != 0 {
		t.Errorf("launches=%d kills=%d, want 1/0", mgr.launchCount(), mgr.killCount())
	}
	var st Status
	if err := json.NewDecoder(rr.Body).Decode(&st); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if st.LoadedProfileID != "alpha" {
		t.Errorf("LoadedProfileID = %q, want alpha", st.LoadedProfileID)
	}
}

func TestStatus_IncludesLoadedLogPath(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9220))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/_admin/load",
		strings.NewReader(`{"profile_id":"alpha"}`))
	r.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// Check in-memory snapshot.
	st := srv.Status()
	if st.LoadedLogPath != "/tmp/x.log" {
		t.Fatalf("LoadedLogPath (in-memory) = %q, want /tmp/x.log", st.LoadedLogPath)
	}

	// Check wire format: read the raw response body and assert the snake_case
	// JSON key is present. A typo'd tag (e.g. "loadedLogPath") would pass the
	// in-memory check above but fail here.
	raw, err := io.ReadAll(rr.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"loaded_log_path":"/tmp/x.log"`)) {
		t.Fatalf("JSON missing loaded_log_path with expected value; body=%s", raw)
	}
	var decoded Status
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if decoded.LoadedLogPath != "/tmp/x.log" {
		t.Fatalf("LoadedLogPath (decoded from wire) = %q, want /tmp/x.log", decoded.LoadedLogPath)
	}
}

func TestHandleAdminLoad_AlreadyLoaded_NoRelaunch_200(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9202))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	body := func() *strings.Reader {
		return strings.NewReader(`{"profile_id":"alpha"}`)
	}
	// First call: launches.
	first := httptest.NewRecorder()
	r1 := httptest.NewRequest("POST", "/_admin/load", body())
	r1.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(first, r1)
	if first.Code != http.StatusOK {
		t.Fatalf("first call status = %d", first.Code)
	}
	// Second call: must not relaunch.
	second := httptest.NewRecorder()
	r2 := httptest.NewRequest("POST", "/_admin/load", body())
	r2.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(second, r2)
	if second.Code != http.StatusOK {
		t.Fatalf("second call status = %d", second.Code)
	}
	if mgr.launchCount() != 1 {
		t.Errorf("launches = %d, want 1 (fast path)", mgr.launchCount())
	}
}

func TestHandleAdminLoad_SwapsDifferentProfile_200(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9203), makeProfile("beta", 9204))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	// Load alpha.
	r1 := httptest.NewRequest("POST", "/_admin/load",
		strings.NewReader(`{"profile_id":"alpha"}`))
	r1.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(httptest.NewRecorder(), r1)

	// Now load beta — should kill alpha + launch beta.
	rr := httptest.NewRecorder()
	r2 := httptest.NewRequest("POST", "/_admin/load",
		strings.NewReader(`{"profile_id":"beta"}`))
	r2.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rr, r2)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if mgr.launchCount() != 2 || mgr.killCount() != 1 {
		t.Errorf("launches=%d kills=%d, want 2/1", mgr.launchCount(), mgr.killCount())
	}
	if cur := srv.Status().LoadedProfileID; cur != "beta" {
		t.Errorf("LoadedProfileID = %q, want beta", cur)
	}
}

func TestHandleAdminLoad_HealthFails_504_CurrentCleared(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9205))
	mgr := newStubManager()
	mgr.healthFn = func(int, int) error { return errors.New("never healthy") }
	srv := newTestServer(t, store, mgr)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/_admin/load",
		strings.NewReader(`{"profile_id":"alpha"}`))
	r.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rr, r)

	if rr.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want 504", rr.Code)
	}
	if mgr.killCount() != 1 {
		t.Errorf("kills = %d, want 1 (cleanup after unhealthy)", mgr.killCount())
	}
	if cur := srv.Status().LoadedProfileID; cur != "" {
		t.Errorf("LoadedProfileID = %q, want empty", cur)
	}
}

func TestHandleAdminLoad_AcceptsModelFieldAlias_200(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9206))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/_admin/load",
		strings.NewReader(`{"model":"alpha"}`))
	r.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if mgr.launchCount() != 1 {
		t.Errorf("launches = %d, want 1", mgr.launchCount())
	}
}

// -----------------------------------------------------------------------------
// /_admin/unload
// -----------------------------------------------------------------------------

func TestHandleAdminUnload_RejectsNonPOST(t *testing.T) {
	srv := newTestServer(t, newStubStore(), newStubManager())
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/_admin/unload", nil))

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
	if rr.Header().Get("Allow") != "POST" {
		t.Errorf("Allow = %q, want POST", rr.Header().Get("Allow"))
	}
}

func TestHandleAdminUnload_NothingLoaded_Idempotent_200(t *testing.T) {
	mgr := newStubManager()
	srv := newTestServer(t, newStubStore(), mgr)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/_admin/unload", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if mgr.killCount() != 0 {
		t.Errorf("kills = %d, want 0 (nothing was loaded)", mgr.killCount())
	}
	var st Status
	if err := json.NewDecoder(rr.Body).Decode(&st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.LoadedProfileID != "" {
		t.Errorf("LoadedProfileID = %q, want empty", st.LoadedProfileID)
	}
}

func TestHandleAdminUnload_KillsBackend_200(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9211))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)

	// Pre-load alpha via the normal swap path.
	if _, _, err := srv.ensureLoaded(context.Background(), "alpha"); err != nil {
		t.Fatalf("preload: %v", err)
	}
	if srv.Status().LoadedProfileID != "alpha" {
		t.Fatal("precondition: alpha not loaded")
	}

	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/_admin/unload", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if mgr.killCount() != 1 {
		t.Errorf("kills = %d, want 1", mgr.killCount())
	}
	if cur := srv.Status().LoadedProfileID; cur != "" {
		t.Errorf("LoadedProfileID = %q, want empty after unload", cur)
	}
	var st Status
	if err := json.NewDecoder(rr.Body).Decode(&st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.LoadedProfileID != "" {
		t.Errorf("response Status.LoadedProfileID = %q, want empty", st.LoadedProfileID)
	}
}

func TestHandleAdminUnload_ForceTrueSkipsDrain(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9212))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)
	if _, _, err := srv.ensureLoaded(context.Background(), "alpha"); err != nil {
		t.Fatalf("preload: %v", err)
	}

	// Fake a request in flight that never finishes — would block a non-force
	// unload until the drain timeout. force=true must skip the wait entirely.
	srv.inflightWG.Add(1)
	defer srv.inflightWG.Done()

	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	done := make(chan struct{})
	go func() {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("POST",
			"/_admin/unload?force=true&drain_timeout=5s", nil))
		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("force=true unload blocked waiting for inflight drain")
	}
	if mgr.killCount() != 1 {
		t.Errorf("kills = %d, want 1", mgr.killCount())
	}
}

func TestHandleAdminUnload_DrainCompletesThenKills(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9213))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)
	if _, _, err := srv.ensureLoaded(context.Background(), "alpha"); err != nil {
		t.Fatalf("preload: %v", err)
	}

	// Simulate a request in flight that finishes after 100ms; unload must
	// wait, then kill the backend.
	srv.inflightWG.Add(1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		srv.inflightWG.Done()
	}()

	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	start := time.Now()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST",
		"/_admin/unload?drain_timeout=2s", nil))
	elapsed := time.Since(start)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("unload returned in %v, expected ≥100ms (drain wait)", elapsed)
	}
	if elapsed > 1*time.Second {
		t.Errorf("unload took %v — drain should finish quickly once inflight=0", elapsed)
	}
	if mgr.killCount() != 1 {
		t.Errorf("kills = %d, want 1", mgr.killCount())
	}
}

func TestHandleAdminUnload_DrainTimeoutProceedsToKill(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9214))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)
	if _, _, err := srv.ensureLoaded(context.Background(), "alpha"); err != nil {
		t.Fatalf("preload: %v", err)
	}

	// Hang a request indefinitely; drain timeout must trigger and unload
	// must still kill the backend.
	srv.inflightWG.Add(1)
	defer srv.inflightWG.Done()

	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	start := time.Now()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST",
		"/_admin/unload?drain_timeout=100ms", nil))
	elapsed := time.Since(start)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("returned in %v, expected ≥100ms (waited timeout)", elapsed)
	}
	if mgr.killCount() != 1 {
		t.Errorf("kills = %d, want 1 (must kill even on drain timeout)", mgr.killCount())
	}
}

func TestHandleAdminUnload_KillError_500_CurrentCleared(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9215))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)
	if _, _, err := srv.ensureLoaded(context.Background(), "alpha"); err != nil {
		t.Fatalf("preload: %v", err)
	}

	// Swap in a Manager that returns a non-ErrUnknownPID error from Kill.
	srv.deps.ProcessMgr = &errKillManager{stubManager: mgr, err: errors.New("boom")}

	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/_admin/unload?force=true", nil))

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body=%s", rr.Code, rr.Body.String())
	}
	if cur := srv.Status().LoadedProfileID; cur != "" {
		t.Errorf("LoadedProfileID = %q, want empty after kill error", cur)
	}
}

func TestHandleAdminUnload_SerializesWithConcurrentLoad(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9216), makeProfile("beta", 9217))
	mgr := newStubManager()
	mgr.swapDelay = 50 * time.Millisecond // widen race window
	srv := newTestServer(t, store, mgr)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		rr := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/_admin/load",
			strings.NewReader(`{"profile_id":"alpha"}`))
		r.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rr, r)
	}()
	go func() {
		defer wg.Done()
		// Give load a head start so swapMu is in use.
		time.Sleep(10 * time.Millisecond)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("POST", "/_admin/unload?force=true", nil))
		if rr.Code != http.StatusOK {
			t.Errorf("unload status = %d", rr.Code)
		}
	}()
	wg.Wait()

	// Exactly one launch (alpha) and one kill (unload) — no torn state.
	if l := mgr.launchCount(); l != 1 {
		t.Errorf("launches = %d, want 1", l)
	}
	if k := mgr.killCount(); k != 1 {
		t.Errorf("kills = %d, want 1", k)
	}
	if cur := srv.Status().LoadedProfileID; cur != "" {
		t.Errorf("LoadedProfileID = %q, want empty (unload ran last)", cur)
	}
}

// errKillManager wraps stubManager and returns a configured error from Kill
// instead of cleaning up. Other calls delegate to the embedded stub.
type errKillManager struct {
	*stubManager
	err error
}

func (m *errKillManager) Kill(pid int) error {
	if errors.Is(m.err, processmgr.ErrUnknownPID) {
		return m.err
	}
	return m.err
}

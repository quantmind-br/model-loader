package httpproxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestServer(t *testing.T, store *stubStore, mgr *stubManager) *Server {
	t.Helper()
	return New(Config{HealthCheckTimeout: 2 * time.Second, MaxBodyBuffer: 1 << 20}, Deps{
		ProfileStore: store,
		ProcessMgr:   mgr,
		Logger:       nil,
	})
}

func TestEnsureLoaded_SwapsBetweenProfiles(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9101), makeProfile("beta", 9102))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)

	// First call loads alpha.
	if _, err := srv.ensureLoaded(context.Background(), "alpha"); err != nil {
		t.Fatalf("alpha load: err=%v", err)
	}
	if mgr.launchCount() != 1 || mgr.killCount() != 0 {
		t.Fatalf("after alpha: launches=%d kills=%d, want 1/0", mgr.launchCount(), mgr.killCount())
	}

	// Second call to alpha: must not relaunch.
	if _, err := srv.ensureLoaded(context.Background(), "alpha"); err != nil {
		t.Fatalf("alpha hot path: %v", err)
	}
	if mgr.launchCount() != 1 || mgr.killCount() != 0 {
		t.Fatalf("after alpha hot path: launches=%d kills=%d, want 1/0", mgr.launchCount(), mgr.killCount())
	}

	// Switch to beta: must kill alpha and launch beta.
	if _, err := srv.ensureLoaded(context.Background(), "beta"); err != nil {
		t.Fatalf("beta load: %v", err)
	}
	if mgr.launchCount() != 2 || mgr.killCount() != 1 {
		t.Fatalf("after beta: launches=%d kills=%d, want 2/1", mgr.launchCount(), mgr.killCount())
	}

	// And confirm Status reflects beta as currently loaded.
	st := srv.Status()
	if st.LoadedProfileID != "beta" {
		t.Errorf("Status.LoadedProfileID = %q, want beta", st.LoadedProfileID)
	}
}

func TestEnsureLoaded_ProfileNotFound(t *testing.T) {
	store := newStubStore() // empty
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)

	_, err := srv.ensureLoaded(context.Background(), "ghost")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
	if status := swapStatus(t, err); status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", status)
	}
	if mgr.launchCount() != 0 {
		t.Errorf("launches = %d, want 0", mgr.launchCount())
	}
}

func TestEnsureLoaded_UnhealthyKillsBackend(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9111))
	mgr := newStubManager()
	mgr.healthFn = func(int, int) error { return errors.New("never healthy") }
	srv := newTestServer(t, store, mgr)

	_, err := srv.ensureLoaded(context.Background(), "alpha")
	if err == nil {
		t.Fatal("expected error from WaitHealthy")
	}
	if status := swapStatus(t, err); status != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want 504", status)
	}
	// Backend must have been killed after the failed health check.
	if mgr.killCount() != 1 {
		t.Errorf("kills = %d, want 1 (failed backend cleanup)", mgr.killCount())
	}
	// No backend should remain loaded.
	if cur := srv.Status().LoadedProfileID; cur != "" {
		t.Errorf("LoadedProfileID = %q, want empty after failed load", cur)
	}
}

func TestEnsureLoaded_ConcurrentSameTarget_OneSwap(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9121))
	mgr := newStubManager()
	mgr.swapDelay = 50 * time.Millisecond // widen race window
	srv := newTestServer(t, store, mgr)

	var wg sync.WaitGroup
	const callers = 8
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			_, err := srv.ensureLoaded(context.Background(), "alpha")
			if err != nil {
				t.Errorf("ensureLoaded: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := mgr.launchCount(); got != 1 {
		t.Errorf("launches = %d, want 1 (single swap under contention)", got)
	}
}

// swapStatus extracts the HTTP status from a *SwapError, failing the test if
// err is not one (the swap path always returns *SwapError on failure).
func swapStatus(t *testing.T, err error) int {
	t.Helper()
	var se *SwapError
	if !errors.As(err, &se) {
		t.Fatalf("expected *SwapError, got %T: %v", err, err)
	}
	return se.StatusCode
}

// TestEnsureLoaded_KillFailureAbortsSwap — P4/DF11: when the previous backend
// cannot be confirmed dead (Kill returns a non-ErrUnknownPID error, i.e. it
// still holds VRAM), the swap MUST abort with a retriable 503 backend_busy
// BEFORE launching the next backend, and must leave s.current pointed at the
// still-live backend. Launching into contended VRAM is the OOM cascade the fix
// prevents.
func TestEnsureLoaded_KillFailureAbortsSwap(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9101), makeProfile("beta", 9102))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)

	if _, err := srv.ensureLoaded(context.Background(), "alpha"); err != nil {
		t.Fatalf("alpha load: err=%v", err)
	}
	if mgr.launchCount() != 1 {
		t.Fatalf("after alpha: launches=%d, want 1", mgr.launchCount())
	}

	// The old backend refuses to die (confirmed-alive after SIGKILL).
	mgr.killFn = func(int) error {
		return errors.New("terminate pid: process still alive after SIGKILL")
	}

	_, err := srv.ensureLoaded(context.Background(), "beta")
	if err == nil {
		t.Fatal("expected swap to abort with an error, got nil")
	}
	if code := swapStatus(t, err); code != 503 {
		t.Errorf("swap error status = %d, want 503 (backend_busy)", code)
	}

	// beta must never have launched — the swap aborted before launchNewBackend.
	if mgr.launchCount() != 1 {
		t.Errorf("launches=%d, want 1 (beta must not launch into contended VRAM)", mgr.launchCount())
	}
	// s.current stays on the still-live backend.
	if got := srv.Status().LoadedProfileID; got != "alpha" {
		t.Errorf("Status.LoadedProfileID = %q, want alpha (s.current preserved)", got)
	}
}

// TestEnsureLoaded_RelaunchesCrashedBackend — audit A6: when the loaded backend
// has died out-of-band, a request for the same model must relaunch instead of
// proxying to the dead PID, and last_error must record the crash.
func TestEnsureLoaded_RelaunchesCrashedBackend(t *testing.T) {
	store := newStubStore(makeProfile("alpha", 9101))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)

	// Install a "loaded" alpha whose PID is certainly dead (identity check
	// via startTicks fails).
	srv.stateMu.Lock()
	srv.current = &loadedBackend{profileID: "alpha", pid: 1 << 22, port: 9101, startTicks: 1}
	srv.stateMu.Unlock()

	loaded, err := srv.ensureLoaded(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("ensureLoaded: %v", err)
	}
	if mgr.launchCount() != 1 {
		t.Fatalf("crashed backend must relaunch; launches=%d want 1", mgr.launchCount())
	}
	if loaded == nil || loaded.pid == 1<<22 {
		t.Fatalf("ensureLoaded returned the dead backend: %+v", loaded)
	}
}

func refusedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return port
}

// P8: a backend can keep its parent PID alive after its API listener dies.
// The first transport failure must preserve the loaded pointer for a safe
// kill; the next request must kill it before relaunching the same profile.
func TestHandleForward_LivePIDRefusedPortRelaunchesOnRetry(t *testing.T) {
	failedPort := refusedPort(t)
	recovered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"recovered"}}]}`))
	}))
	defer recovered.Close()
	store := newStubStore(makeProfile("alpha", failedPort))
	mgr := newStubManager()
	mgr.launchPorts = []int{failedPort, portOf(t, recovered)}
	srv := newTestServer(t, store, mgr)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	request := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		rr := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rr, r)
		return rr
	}

	if rr := request(`{"model":"alpha","messages":[{"role":"user","content":"hi"}]}`); rr.Code != http.StatusBadGateway {
		t.Fatalf("first status = %d, want 502 (body: %s)", rr.Code, rr.Body.String())
	}
	if got := srv.Status().LoadedProfileID; got != "alpha" {
		t.Fatalf("loaded profile after transport failure = %q, want alpha preserved for safe kill", got)
	}

	if rr := request(`{"model":"alpha","messages":[{"role":"user","content":"retry"}]}`); rr.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
	if got := strings.Join(mgr.eventSequence(), ","); got != "launch,kill,launch" {
		t.Fatalf("lifecycle order = %q, want launch,kill,launch", got)
	}
}

func TestHandleForward_NoModelLivePIDRefusedPortRelaunchesOnRetry(t *testing.T) {
	failedPort := refusedPort(t)
	recovered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"recovered"}}]}`))
	}))
	defer recovered.Close()
	store := newStubStore(makeProfile("alpha", failedPort))
	mgr := newStubManager()
	mgr.launchPorts = []int{failedPort, portOf(t, recovered)}
	srv := newTestServer(t, store, mgr)
	if _, err := srv.ensureLoaded(context.Background(), "alpha"); err != nil {
		t.Fatalf("load alpha: %v", err)
	}
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	request := func() *httptest.ResponseRecorder {
		t.Helper()
		rr := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
			strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`))
		r.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rr, r)
		return rr
	}
	if rr := request(); rr.Code != http.StatusBadGateway {
		t.Fatalf("detection status = %d, want 502 (body: %s)", rr.Code, rr.Body.String())
	}
	if rr := request(); rr.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
	if got := strings.Join(mgr.eventSequence(), ","); got != "launch,kill,launch" {
		t.Fatalf("lifecycle order = %q, want launch,kill,launch", got)
	}
}

func TestPostUpstreamChat_LivePIDRefusedPortRelaunchesOnRetry(t *testing.T) {
	failedPort := refusedPort(t)
	recovered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"recovered"}}]}`))
	}))
	defer recovered.Close()
	store := newStubStore(makeProfile("alpha", failedPort))
	mgr := newStubManager()
	mgr.launchPorts = []int{failedPort, portOf(t, recovered)}
	srv := newTestServer(t, store, mgr)
	loaded, err := srv.ensureLoaded(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("load alpha: %v", err)
	}

	if _, aerr := srv.postUpstreamChat(context.Background(), loaded, &oaiChatRequest{Model: "alpha"}); aerr == nil || aerr.Status != http.StatusBadGateway {
		t.Fatalf("direct upstream error = %#v, want 502", aerr)
	}
	loaded, err = srv.ensureLoaded(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("relaunch alpha: %v", err)
	}
	resp, aerr := srv.postUpstreamChat(context.Background(), loaded, &oaiChatRequest{Model: "alpha"})
	if aerr != nil {
		t.Fatalf("recovered upstream request: %v", aerr)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("recovered upstream status = %d, want 200", resp.StatusCode)
	}
	if got := strings.Join(mgr.eventSequence(), ","); got != "launch,kill,launch" {
		t.Fatalf("direct lifecycle order = %q, want launch,kill,launch", got)
	}
}

func TestPostUpstreamChat_CanceledRequestDoesNotRelaunch(t *testing.T) {
	store := newStubStore(makeProfile("alpha", refusedPort(t)))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)
	loaded, err := srv.ensureLoaded(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("load alpha: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, aerr := srv.postUpstreamChat(ctx, loaded, &oaiChatRequest{Model: "alpha"}); aerr == nil {
		t.Fatal("expected canceled upstream request to fail")
	}
	if _, err := srv.ensureLoaded(context.Background(), "alpha"); err != nil {
		t.Fatalf("alpha hot path after cancellation: %v", err)
	}
	if mgr.launchCount() != 1 || mgr.killCount() != 0 {
		t.Fatalf("after cancellation: launches=%d kills=%d, want 1/0", mgr.launchCount(), mgr.killCount())
	}
}

func TestEnsureLoaded_UnavailableBackendKillFailureAbortsRelaunch(t *testing.T) {
	port := refusedPort(t)
	store := newStubStore(makeProfile("alpha", port))
	mgr := newStubManager()
	srv := newTestServer(t, store, mgr)
	loaded, err := srv.ensureLoaded(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("load alpha: %v", err)
	}
	if _, aerr := srv.postUpstreamChat(context.Background(), loaded, &oaiChatRequest{Model: "alpha"}); aerr == nil {
		t.Fatal("expected upstream transport failure")
	}
	mgr.killFn = func(int) error { return errors.New("process still alive after SIGKILL") }

	_, err = srv.ensureLoaded(context.Background(), "alpha")
	if err == nil || swapStatus(t, err) != http.StatusServiceUnavailable {
		t.Fatalf("relaunch error = %v, want 503", err)
	}
	if mgr.launchCount() != 1 {
		t.Fatalf("launches=%d, want 1 (must not relaunch into occupied VRAM)", mgr.launchCount())
	}
	if got := srv.Status().LoadedProfileID; got != "alpha" {
		t.Fatalf("loaded profile = %q, want alpha preserved", got)
	}
}

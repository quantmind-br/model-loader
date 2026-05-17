package httpproxy

import (
	"context"
	"errors"
	"net/http"
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
	if _, status, err := srv.ensureLoaded(context.Background(), "alpha"); err != nil {
		t.Fatalf("alpha load: err=%v status=%d", err, status)
	}
	if mgr.launchCount() != 1 || mgr.killCount() != 0 {
		t.Fatalf("after alpha: launches=%d kills=%d, want 1/0", mgr.launchCount(), mgr.killCount())
	}

	// Second call to alpha: must not relaunch.
	if _, _, err := srv.ensureLoaded(context.Background(), "alpha"); err != nil {
		t.Fatalf("alpha hot path: %v", err)
	}
	if mgr.launchCount() != 1 || mgr.killCount() != 0 {
		t.Fatalf("after alpha hot path: launches=%d kills=%d, want 1/0", mgr.launchCount(), mgr.killCount())
	}

	// Switch to beta: must kill alpha and launch beta.
	if _, _, err := srv.ensureLoaded(context.Background(), "beta"); err != nil {
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

	_, status, err := srv.ensureLoaded(context.Background(), "ghost")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
	if status != http.StatusNotFound {
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

	_, status, err := srv.ensureLoaded(context.Background(), "alpha")
	if err == nil {
		t.Fatal("expected error from WaitHealthy")
	}
	if status != http.StatusGatewayTimeout {
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
			_, _, err := srv.ensureLoaded(context.Background(), "alpha")
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

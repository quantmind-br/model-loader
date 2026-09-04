package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withServeEnv(t *testing.T, k, v string) {
	t.Helper()
	old, had := os.LookupEnv(k)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(k, old)
		} else {
			_ = os.Unsetenv(k)
		}
	})
	_ = os.Setenv(k, v)
}

func healthyStatusServer(t *testing.T, running bool, code int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_status" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		if running {
			_, _ = w.Write([]byte(`{"running":true}`))
		} else {
			_, _ = w.Write([]byte(`{"running":false}`))
		}
	}))
}

func TestNotifyLoopbackHost(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "127.0.0.1"},
		{"0.0.0.0", "127.0.0.1"},
		{"::", "127.0.0.1"},
		{"127.0.0.1", "127.0.0.1"},
		{"192.168.1.2", "192.168.1.2"},
	}
	for _, tc := range cases {
		if got := notifyLoopbackHost(tc.in); got != tc.want {
			t.Errorf("notifyLoopbackHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestProbeProxyStatus(t *testing.T) {
	client := &http.Client{Timeout: 2 * time.Second}

	ts := healthyStatusServer(t, true, http.StatusOK)
	defer ts.Close()
	if !probeProxyStatus(client, hostPortOf(t, ts)) {
		t.Fatal("healthy /_status must probe true")
	}

	ts2 := healthyStatusServer(t, false, http.StatusOK)
	defer ts2.Close()
	if probeProxyStatus(client, hostPortOf(t, ts2)) {
		t.Fatal("running=false must probe false")
	}

	ts3 := healthyStatusServer(t, true, http.StatusInternalServerError)
	defer ts3.Close()
	if probeProxyStatus(client, hostPortOf(t, ts3)) {
		t.Fatal("non-200 must probe false")
	}

	if probeProxyStatus(client, "127.0.0.1:1") {
		t.Fatal("refused connection must probe false")
	}
}

func hostPortOf(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	// httptest URLs are http://127.0.0.1:port — strip the scheme.
	u := ts.URL[len("http://"):]
	return u
}

func TestServeReadiness_DisabledIsNoop(t *testing.T) {
	withServeEnv(t, "NOTIFY_SOCKET", "")
	if err := serveReadiness("127.0.0.1", 1, time.Second); err != nil {
		t.Fatalf("disabled readiness must be nil, got: %v", err)
	}
	if stop := startServeWatchdog("127.0.0.1", 1, testServeLogger()); stop != nil {
		t.Fatal("disabled watchdog must return nil stop")
		stop()
	}
	stopServeWatchdog(nil) // must not panic
}

func TestServeReadiness_HealthySendsReady(t *testing.T) {
	ts := healthyStatusServer(t, true, http.StatusOK)
	defer ts.Close()

	sock := filepath.Join(t.TempDir(), "notify.sock")
	withServeEnv(t, "NOTIFY_SOCKET", sock)
	received := collectDatagrams(t, sock)

	host, port := splitHostPort(t, hostPortOf(t, ts))
	if err := serveReadiness(host, port, 5*time.Second); err != nil {
		t.Fatalf("serveReadiness: %v", err)
	}
	waitForPayload(t, received, "READY=1")
}

func TestServeReadiness_UnhealthyTimesOut(t *testing.T) {
	ts := healthyStatusServer(t, false, http.StatusOK)
	defer ts.Close()

	withServeEnv(t, "NOTIFY_SOCKET", filepath.Join(t.TempDir(), "notify.sock"))
	host, port := splitHostPort(t, hostPortOf(t, ts))
	if err := serveReadiness(host, port, 300*time.Millisecond); err == nil {
		t.Fatal("unhealthy /_status must fail readiness")
	}
}

func TestServeWatchdog_HealthyPulses(t *testing.T) {
	ts := healthyStatusServer(t, true, http.StatusOK)
	defer ts.Close()

	sock := filepath.Join(t.TempDir(), "notify.sock")
	withServeEnv(t, "NOTIFY_SOCKET", sock)
	// Short watchdog interval exercises the heartbeat quickly; HalfInterval
	// floors at 1s so expect the first ping within ~2s.
	withServeEnv(t, "WATCHDOG_USEC", "2000000")
	withServeEnv(t, "WATCHDOG_PID", "")
	received := collectDatagrams(t, sock)

	host, port := splitHostPort(t, hostPortOf(t, ts))
	stop := startServeWatchdog(host, port, testServeLogger())
	if stop == nil {
		t.Fatal("expected watchdog to start")
	}
	defer stopServeWatchdog(stop)
	waitForPayload(t, received, "WATCHDOG=1")
}

func TestServeWatchdog_WrongPIDDisables(t *testing.T) {
	withServeEnv(t, "NOTIFY_SOCKET", filepath.Join(t.TempDir(), "notify.sock"))
	withServeEnv(t, "WATCHDOG_USEC", "15000000")
	withServeEnv(t, "WATCHDOG_PID", "1")
	if stop := startServeWatchdog("127.0.0.1", 4321, testServeLogger()); stop != nil {
		stop()
		t.Fatal("WATCHDOG_PID mismatch must disable heartbeat")
	}
}

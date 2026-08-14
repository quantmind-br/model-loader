package processmgr

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestWaitReady_TokenSpeedProbesReadiness(t *testing.T) {
	var readinessHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readiness":
			readinessHits++
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	_, portString, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split server host: %v", err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}

	mgr, _ := newTestManager(t)
	inst := domain.RunningInstance{PID: os.Getpid(), Port: port, Kind: domain.BackendKindTokenSpeed}
	token, err := mgr.WaitReady(inst, 2*time.Second, "")
	if err != nil {
		t.Fatalf("WaitReady(tokenspeed): %v", err)
	}
	if token != "" {
		t.Fatalf("token = %q, want empty", token)
	}
	if readinessHits == 0 {
		t.Fatal("expected /readiness to be probed")
	}
	if err := mgr.WaitHealthy(os.Getpid(), port, 150*time.Millisecond, ""); !errors.Is(err, ErrHealthCheckTimeout) {
		t.Fatalf("WaitHealthy(/health) error = %v, want ErrHealthCheckTimeout", err)
	}
}

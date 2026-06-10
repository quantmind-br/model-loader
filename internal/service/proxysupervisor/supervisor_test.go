package proxysupervisor

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSupervisorLifecycle(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, "proxy-state.json")

	s := New(Config{
		StatePath: statePath,
		LogDir:    tmp,
		Host:      "127.0.0.1",
		Port:      54321,
	})

	if st := s.Status(); st.Running {
		t.Fatal("expected stopped initially")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Start(ctx); err == nil {
		t.Fatal("expected start to fail because port 54321 is not actually serving")
	}
}

func TestStatusProbeFailureSetsLastError(t *testing.T) {
	// A raw TCP listener that never accepts: pidAlive and portOpen succeed
	// (the process is "alive" and the port dials), but the /_status HTTP GET
	// times out — the degraded case Status must surface via LastError.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	s := New(Config{
		StatePath: filepath.Join(t.TempDir(), "proxy-state.json"),
		Host:      "127.0.0.1",
		Port:      port,
	})
	// Inject alive state: our own PID is alive and the port above is open.
	s.state = &State{PID: os.Getpid(), Host: "127.0.0.1", Port: port, StartedAt: time.Now().UTC()}

	st := s.Status()
	if !st.Running {
		t.Fatalf("expected Running=true, got %+v", st)
	}
	if !strings.HasPrefix(st.LastError, "status_probe_failed") {
		t.Fatalf("expected status_probe_failed LastError, got %q", st.LastError)
	}
}

func TestStateRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "state.json")

	st := &State{PID: 42, Host: "127.0.0.1", Port: 8080, StartedAt: time.Now().UTC()}
	if err := saveState(path, st); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := loadState(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected loaded state")
	}
	if loaded.PID != 42 || loaded.Host != "127.0.0.1" || loaded.Port != 8080 {
		t.Fatalf("loaded mismatch: %+v", loaded)
	}

	if err := saveState(path, nil); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected file removed")
	}
}

func TestReconcileCleansDeadState(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "state.json")

	st := &State{PID: 999999, Host: "127.0.0.1", Port: 1, StartedAt: time.Now().UTC()}
	if err := saveState(path, st); err != nil {
		t.Fatalf("save: %v", err)
	}

	s := New(Config{StatePath: path, Host: "127.0.0.1", Port: 1})
	if err := s.Reconcile(); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if s.state != nil {
		t.Fatal("expected dead state to be cleaned")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected file removed after reconcile")
	}
}

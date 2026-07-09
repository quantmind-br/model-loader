package proxysupervisor

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/internal/procutil"
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

func waitDeadSup(pid int, within time.Duration) bool {
	d := time.Now().Add(within)
	for time.Now().Before(d) {
		if !procutil.Alive(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func freePortSup(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// TestStatusHysteresisDropsAfterThreeFailures — audit A9: a transient port
// probe failure keeps a healthy proxy supervised (degraded) for two strikes;
// the third drops state.
func TestStatusHysteresisDropsAfterThreeFailures(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, "proxy-state.json")
	port := freePortSup(t) // nobody listening → portOpen fails
	s := New(Config{StatePath: statePath, LogDir: tmp, Host: "127.0.0.1", Port: port})
	ticks, _ := procutil.StartTicks(os.Getpid())
	st := &State{PID: os.Getpid(), Host: "127.0.0.1", Port: port, StartedAt: time.Now().UTC(), StartTicks: ticks}
	if err := saveState(statePath, st); err != nil {
		t.Fatal(err)
	}
	s.state = st

	for i := 1; i <= 2; i++ {
		got := s.Status()
		if !got.Running {
			t.Fatalf("probe %d: expected Running (degraded), got %+v", i, got)
		}
		if !strings.HasPrefix(got.LastError, "status_probe_failed") {
			t.Fatalf("probe %d: expected degraded LastError, got %q", i, got.LastError)
		}
	}
	if got := s.Status(); got.Running {
		t.Fatalf("3rd probe: expected Running=false after hysteresis, got %+v", got)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state file should be removed after hysteresis; stat err=%v", err)
	}
}

// TestStartFailsWhenChildDiesButPortHeld — audit A9: a duplicate spawn whose
// child dies while a pre-existing proxy holds the port must fail Start and
// never write proxy-state.json.
func TestStartFailsWhenChildDiesButPortHeld(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, "proxy-state.json")
	ln, err := net.Listen("tcp", "127.0.0.1:0") // occupy the port
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	s := New(Config{StatePath: statePath, LogDir: tmp, Host: "127.0.0.1", Port: port, BinaryPath: "/bin/false"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Start(ctx); err == nil {
		t.Fatal("Start must fail when the child dies while another process holds the port")
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("proxy-state.json must NOT be written on duplicate-spawn; stat err=%v", err)
	}
}

// TestStopEscalatesToSIGKILL — audit A8: a serve that ignores SIGTERM is still
// dead after Stop, via SIGKILL escalation through TerminateTree.
func TestStopEscalatesToSIGKILL(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, "proxy-state.json")
	cmd := exec.Command("bash", "-c", "trap '' TERM; sleep 300")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = cmd.Wait() }()
	pid := cmd.Process.Pid
	ticks, _ := procutil.StartTicks(pid)
	st := &State{PID: pid, Host: "127.0.0.1", Port: 1, StartedAt: time.Now().UTC(), StartTicks: ticks}
	s := New(Config{StatePath: statePath, LogDir: tmp, Host: "127.0.0.1", Port: 1})
	if err := saveState(statePath, st); err != nil {
		t.Fatal(err)
	}
	s.state = st

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !waitDeadSup(pid, 3*time.Second) {
		t.Fatal("SIGTERM-ignoring proxy still alive after Stop (no SIGKILL escalation)")
	}
}

// TestStart_HonorsCallerContext guards audit P-C12: Start's port wait honors the
// caller's ctx, so a cancellation (TUI quit / CLI Ctrl-C) aborts it well before
// the 10s timeout instead of always blocking the full duration.
func TestStart_HonorsCallerContext(t *testing.T) {
	tmp := t.TempDir()
	// Fake binary that stays alive but never binds the port.
	fake := filepath.Join(tmp, "fake-proxy.sh")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close() // free it; the fake binary won't rebind

	s := New(Config{
		StatePath:  filepath.Join(tmp, "proxy-state.json"),
		LogDir:     tmp,
		Host:       "127.0.0.1",
		Port:       port,
		BinaryPath: fake,
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err = s.Start(ctx)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected Start to fail (port never bound)")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Start took %v; caller ctx cancellation must abort the 10s port wait", elapsed)
	}
}

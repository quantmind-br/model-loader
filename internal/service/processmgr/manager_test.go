package processmgr

import (
	"errors"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func fakeBinary(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../../../testdata/fake-llama-server.sh")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func newTestManager(t *testing.T) (*fsManager, string) {
	t.Helper()
	dir := t.TempDir()
	mgr := New(Config{
		Binary:       fakeBinary(t),
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
	})
	return mgr, dir
}

func TestManager_LaunchBackground_WaitsHealthyAndPersists(t *testing.T) {
	mgr, _ := newTestManager(t)
	port := freePort(t)
	p := domain.Profile{
		ID:    "smoke",
		Name:  "Smoke",
		Model: "/dev/null",
		Args:  map[string]any{"port": float64(port)},
	}
	inst, err := mgr.Launch(p, LaunchBackground)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	if inst.PID <= 0 || inst.Port != port || !inst.Background {
		t.Fatalf("inst = %+v", inst)
	}

	if err := mgr.WaitHealthy(inst.PID, port, 5*time.Second); err != nil {
		t.Fatalf("WaitHealthy: %v", err)
	}

	loaded, err := loadRegistry(mgr.registryPath)
	if err != nil {
		t.Fatalf("loadRegistry: %v", err)
	}
	found := false
	for _, ri := range loaded {
		if ri.PID == inst.PID {
			found = true
		}
	}
	if !found {
		t.Errorf("registry missing pid %d; got %+v", inst.PID, loaded)
	}
}

func TestManager_LaunchBackground_ProfileOverrideUsesEffectiveBinary(t *testing.T) {
	dir := t.TempDir()
	defaultBinary := filepath.Join(dir, "missing-default")
	overrideBinary := fakeBinary(t)
	mgr := New(Config{
		Binary:       defaultBinary,
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
	})
	port := freePort(t)
	p := domain.Profile{
		ID:     "override",
		Model:  "/dev/null",
		Args:   map[string]any{"port": float64(port)},
		Launch: domain.LaunchConfig{LlamaServerBinaryPath: overrideBinary},
	}
	inst, err := mgr.Launch(p, LaunchBackground)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	if inst.BinaryPath != overrideBinary {
		t.Fatalf("BinaryPath = %q, want %q", inst.BinaryPath, overrideBinary)
	}
	if err := mgr.WaitHealthy(inst.PID, port, 5*time.Second); err != nil {
		t.Fatalf("WaitHealthy: %v", err)
	}
}

func TestManager_LaunchBackground_NoOverrideUsesDefaultBinary(t *testing.T) {
	mgr, _ := newTestManager(t)
	port := freePort(t)
	p := domain.Profile{
		ID:    "default-binary",
		Model: "/dev/null",
		Args:  map[string]any{"port": float64(port)},
	}
	inst, err := mgr.Launch(p, LaunchBackground)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	if inst.BinaryPath != mgr.binary {
		t.Fatalf("BinaryPath = %q, want %q", inst.BinaryPath, mgr.binary)
	}
}

func TestManager_LaunchBackground_InvalidEffectiveBinary(t *testing.T) {
	mgr, _ := newTestManager(t)
	port := freePort(t)
	badBinary := filepath.Join(t.TempDir(), "does-not-exist")
	p := domain.Profile{
		ID:     "invalid-binary",
		Model:  "/dev/null",
		Args:   map[string]any{"port": float64(port)},
		Launch: domain.LaunchConfig{LlamaServerBinaryPath: badBinary},
	}
	_, err := mgr.Launch(p, LaunchBackground)
	if err == nil {
		t.Fatal("expected invalid binary error, got nil")
	}
	want := "invalid llama-server binary " + strconv.Quote(badBinary)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want prefix containing %q", err, want)
	}
}

func TestManager_Launch_ModelMissing(t *testing.T) {
	mgr, _ := newTestManager(t)
	port := freePort(t)
	p := domain.Profile{
		ID:    "missing",
		Model: "/nonexistent/path/to/model.gguf",
		Args:  map[string]any{"port": float64(port)},
	}
	_, err := mgr.Launch(p, LaunchBackground)
	if err == nil {
		t.Fatal("expected ErrModelNotFound, got nil")
	}
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("err = %v, want ErrModelNotFound", err)
	}
}

func TestManager_LaunchBackground_PortBusy(t *testing.T) {
	mgr, _ := newTestManager(t)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	p := domain.Profile{
		ID:    "busy",
		Model: "/dev/null",
		Args:  map[string]any{"port": float64(port)},
	}
	_, err = mgr.Launch(p, LaunchBackground)
	if err == nil {
		t.Fatal("expected ErrPortBusy, got nil")
	}
	if !errors.Is(err, ErrPortBusy) {
		t.Fatalf("err = %v, want ErrPortBusy", err)
	}
}

func TestManager_WaitHealthy_TimesOut(t *testing.T) {
	mgr, _ := newTestManager(t)
	port := freePort(t)
	err := mgr.WaitHealthy(99999, port, 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, ErrHealthCheckTimeout) {
		t.Fatalf("err = %v, want ErrHealthCheckTimeout", err)
	}
}

type sinkSpy struct {
	calls []string // ProfileIDs
}

func (s *sinkSpy) MarkLastUsed(profileID string, at time.Time) error {
	s.calls = append(s.calls, profileID)
	return nil
}

func TestManager_Launch_NotifiesLastUsedSink(t *testing.T) {
	dir := t.TempDir()
	spy := &sinkSpy{}
	mgr := New(Config{
		Binary:       fakeBinary(t),
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
		LastUsedSink: spy,
	})
	port := freePort(t)
	p := domain.Profile{ID: "tracked", Model: "/dev/null", Args: map[string]any{"port": float64(port)}}
	inst, err := mgr.Launch(p, LaunchBackground)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	if err := mgr.WaitHealthy(inst.PID, port, 5*time.Second); err != nil {
		t.Fatalf("WaitHealthy: %v", err)
	}
	if len(spy.calls) != 1 || spy.calls[0] != "tracked" {
		t.Errorf("sink.calls = %v, want [tracked]", spy.calls)
	}
}

func TestTailLogs_HappyPath(t *testing.T) {
	mgr, _ := newTestManager(t)
	port := freePort(t)
	p := domain.Profile{
		ID:    "tail",
		Name:  "Tail",
		Model: "/dev/null",
		Args:  map[string]any{"port": float64(port)},
	}
	inst, err := mgr.Launch(p, LaunchBackground)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	if err := mgr.WaitHealthy(inst.PID, port, 5*time.Second); err != nil {
		t.Fatalf("WaitHealthy: %v", err)
	}

	rc, err := mgr.TailLogs(inst.PID)
	if err != nil {
		t.Fatalf("TailLogs: %v", err)
	}
	defer rc.Close()

	buf := make([]byte, 256)
	n, _ := rc.Read(buf)
	if n == 0 {
		t.Fatal("TailLogs returned empty buffer")
	}
}

func TestTailLogs_UnknownPID(t *testing.T) {
	mgr, _ := newTestManager(t)
	if _, err := mgr.TailLogs(999_999); !errors.Is(err, ErrUnknownPID) {
		t.Fatalf("err = %v, want ErrUnknownPID", err)
	}
}

func TestManager_New_BinaryNotInPATH(t *testing.T) {
	cfg := Config{Binary: "this-bin-does-not-exist-xyz", LogDir: t.TempDir(), RegistryPath: filepath.Join(t.TempDir(), "i.json")}
	_, err := NewWithCheck(cfg)
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("err = %v, want ErrBinaryNotFound", err)
	}
}

func TestManager_Foreground_OnlyOneAllowed(t *testing.T) {
	mgr, _ := newTestManager(t)
	port1 := freePort(t)
	port2 := freePort(t)

	p1 := domain.Profile{ID: "fg1", Model: "/dev/null", Args: map[string]any{"port": float64(port1)}}
	inst1, err := mgr.Launch(p1, LaunchForeground)
	if err != nil {
		t.Fatalf("first foreground Launch: %v", err)
	}
	defer mgr.Kill(inst1.PID)

	p2 := domain.Profile{ID: "fg2", Model: "/dev/null", Args: map[string]any{"port": float64(port2)}}
	_, err = mgr.Launch(p2, LaunchForeground)
	if err == nil {
		t.Fatal("expected ErrForegroundBusy, got nil")
	}
	if !errors.Is(err, ErrForegroundBusy) {
		t.Fatalf("err = %v, want ErrForegroundBusy", err)
	}

	// background launch alongside fg1 must still succeed
	port3 := freePort(t)
	p3 := domain.Profile{ID: "bg1", Model: "/dev/null", Args: map[string]any{"port": float64(port3)}}
	inst3, err := mgr.Launch(p3, LaunchBackground)
	if err != nil {
		t.Fatalf("background launch alongside fg: %v", err)
	}
	defer mgr.Kill(inst3.PID)
}

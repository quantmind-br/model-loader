package processmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
)

func TestReconcile_DropsZombiePIDs(t *testing.T) {
	mgr, dir := newTestManager(t)

	// Write a registry with two entries: one fake (PID=1, init — alive but
	// name != llama-server) and one with our own PID (alive, name != llama-server).
	// Both should be dropped because comm doesn't contain the binary.
	entries := []domain.RunningInstance{
		{ProfileID: "ghost", PID: 1, Port: 9001, LogPath: filepath.Join(dir, "logs/ghost.log"), StartedAt: time.Now(), Background: true},
		{ProfileID: "self", PID: os.Getpid(), Port: 9002, LogPath: filepath.Join(dir, "logs/self.log"), StartedAt: time.Now(), Background: true},
	}
	if err := saveRegistry(mgr.registryPath, entries); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if got := mgr.List(); len(got) != 0 {
		t.Errorf("List after Reconcile = %v, want empty", got)
	}

	loaded, _ := loadRegistry(mgr.registryPath)
	if len(loaded) != 0 {
		t.Errorf("on-disk registry = %v, want empty", loaded)
	}
}

func TestReconcile_KeepsLiveLlamaServer(t *testing.T) {
	mgr, _ := newTestManager(t)
	port := freePort(t)
	p := domain.Profile{ID: "alive", Model: "/dev/null", Args: map[string]any{"port": float64(port)}}
	inst, err := mgr.Launch(p, LaunchBackground)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	// Wait for the fake server to actually serve /health — this guarantees
	// the bash→python3 exec transition has completed and /proc/<pid>/comm
	// reads "python3" deterministically.
	if err := mgr.WaitHealthy(inst.PID, port, 5*time.Second); err != nil {
		t.Fatalf("WaitHealthy: %v", err)
	}
	entries := []domain.RunningInstance{inst}
	entries[0].BinaryPath = "" // legacy registry entry: falls back to manager binary
	if err := saveRegistry(mgr.registryPath, entries); err != nil {
		t.Fatal(err)
	}

	// Forge a fresh manager pointing at the same registry — simulates restart.
	dir := filepath.Dir(mgr.registryPath)
	freshMgr := New(Config{
		Binary:       "python3", // matches /proc/<pid>/comm of fake-llama-server.sh's exec'd interpreter
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: mgr.registryPath,
	})
	if err := freshMgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	got := freshMgr.List()
	if len(got) != 1 || got[0].PID != inst.PID {
		t.Errorf("expected 1 entry pid=%d, got %+v", inst.PID, got)
	}

	// Cleanup via fresh manager.
	_ = freshMgr.Kill(inst.PID)
}

func TestReconcile_UsesInstanceBinaryPath(t *testing.T) {
	mgr, _ := newTestManager(t)
	port := freePort(t)
	p := domain.Profile{ID: "instance-binary", Model: "/dev/null", Args: map[string]any{"port": float64(port)}}
	inst, err := mgr.Launch(p, LaunchBackground)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	if err := mgr.WaitHealthy(inst.PID, port, 5*time.Second); err != nil {
		t.Fatalf("WaitHealthy: %v", err)
	}

	entries := []domain.RunningInstance{inst}
	entries[0].BinaryPath = "python3"
	if err := saveRegistry(mgr.registryPath, entries); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Dir(mgr.registryPath)
	freshMgr := New(Config{
		Binary:       "definitely-not-python3",
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: mgr.registryPath,
	})
	if err := freshMgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	got := freshMgr.List()
	if len(got) != 1 || got[0].PID != inst.PID || got[0].BinaryPath != "python3" {
		t.Fatalf("List after Reconcile = %+v, want pid=%d binary=python3", got, inst.PID)
	}

	_ = freshMgr.Kill(inst.PID)
}

// TestPidAliveAndNameMatches_LongBinaryName verifies that /proc/<pid>/comm
// truncation (Linux TASK_COMM_LEN=16, so 15 visible chars) does not cause
// a false-negative when the binary basename is longer than 15 bytes.
func TestPidAliveAndNameMatches_LongBinaryName(t *testing.T) {
	pid := os.Getpid()

	// Read our own comm — it may be truncated to 15 bytes by the kernel.
	commBytes, err := os.ReadFile(filepath.Join("/proc", fmt.Sprintf("%d", pid), "comm"))
	if err != nil {
		t.Skip("cannot read /proc/comm:", err)
	}
	comm := strings.TrimSpace(string(commBytes))

	// Build an expectedComm that is our comm plus a long suffix.
	// If our comm is already 15 bytes, truncation makes expected == comm.
	// If shorter, truncation still leaves a prefix that matches.
	longExpected := comm + "-very-long-suffix-exceeds-fifteen"

	if !pidAliveAndNameMatches(pid, longExpected) {
		t.Errorf("pidAliveAndNameMatches(%d, %q) = false, want true (comm=%q)", pid, longExpected, comm)
	}
}

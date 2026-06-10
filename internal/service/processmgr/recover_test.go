package processmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
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
	inst, err := mgr.Launch(p, LaunchBackground, "")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	// Wait for the fake server to actually serve /health — this guarantees
	// the bash→python3 exec transition has completed and /proc/<pid>/comm
	// reads "python3" deterministically. Generous bound: under full-suite
	// parallel load the python3 startup alone can exceed several seconds.
	if err := mgr.WaitHealthy(inst.PID, inst.Port, 30*time.Second, ""); err != nil {
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
		Resolver:      func(_ domain.Profile) (string, domain.BackendKind, error) { return "python3", "", nil },
		DefaultBinary: "python3",
		LogDir:        filepath.Join(dir, "logs"),
		RegistryPath:  mgr.registryPath,
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
	inst, err := mgr.Launch(p, LaunchBackground, "")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	if err := mgr.WaitHealthy(inst.PID, inst.Port, 5*time.Second, ""); err != nil {
		t.Fatalf("WaitHealthy: %v", err)
	}

	entries := []domain.RunningInstance{inst}
	entries[0].BinaryPath = "python3"
	if err := saveRegistry(mgr.registryPath, entries); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Dir(mgr.registryPath)
	freshMgr := New(Config{
		Resolver:     func(_ domain.Profile) (string, domain.BackendKind, error) { return "definitely-not-python3", "", nil },
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

// TestRefreshFromDisk_UpdatesTrackedWithoutWritingRegistry verifies the
// observer-side refresh: the in-memory tracked set is replaced with the live
// subset of instances.json, but the file itself is NEVER written back — a
// TUI-side save could race the proxy process's writes and erase a freshly
// launched instance (cross-process last-writer-wins).
func TestRefreshFromDisk_UpdatesTrackedWithoutWritingRegistry(t *testing.T) {
	mgr, dir := newTestManager(t)

	// Our own PID is alive and /proc/self/comm names this test binary, so an
	// entry whose BinaryPath matches our comm survives validation. PID 1 is
	// alive too, but its comm never matches — it must be dropped from the
	// in-memory set, yet stay in the file (no save).
	commBytes, err := os.ReadFile("/proc/self/comm")
	if err != nil {
		t.Skip("cannot read /proc/self/comm:", err)
	}
	comm := strings.TrimSpace(string(commBytes))

	entries := []domain.RunningInstance{
		{ProfileID: "self", PID: os.Getpid(), Port: 9001, BinaryPath: comm, LogPath: filepath.Join(dir, "logs/self.log"), StartedAt: time.Now(), Background: true},
		{ProfileID: "ghost", PID: 1, Port: 9002, BinaryPath: "definitely-not-init", LogPath: filepath.Join(dir, "logs/ghost.log"), StartedAt: time.Now(), Background: true},
	}
	if err := saveRegistry(mgr.registryPath, entries); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(mgr.registryPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := mgr.RefreshFromDisk(); err != nil {
		t.Fatalf("RefreshFromDisk: %v", err)
	}

	// Assert against m.tracked directly: List() re-merges raw registry
	// entries for cross-process discovery, which would mask the validated
	// replacement performed by RefreshFromDisk.
	mgr.mu.Lock()
	got := snapshotLocked(mgr.tracked)
	mgr.mu.Unlock()
	if len(got) != 1 || got[0].PID != os.Getpid() || got[0].ProfileID != "self" {
		t.Errorf("tracked after RefreshFromDisk = %+v, want only the live self entry", got)
	}

	after, err := os.ReadFile(mgr.registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("RefreshFromDisk must not write the registry:\nbefore: %s\nafter:  %s", before, after)
	}
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

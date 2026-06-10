package processmgr

import (
	"fmt"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestReconcile_WrapperScriptExec(t *testing.T) {
	mgr, _ := newTestManager(t)
	port := freePort(t)
	p := domain.Profile{ID: "alive", Model: "/dev/null", Args: map[string]any{"port": float64(port)}}
	inst, err := mgr.Launch(p, LaunchBackground, "")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	if err := mgr.WaitHealthy(inst.PID, inst.Port, 5*time.Second, ""); err != nil {
		t.Fatalf("WaitHealthy: %v", err)
	}

	entries := []domain.RunningInstance{inst}
	entries[0].BinaryPath = "/home/user/.local/bin/llama-server-wrapper"
	if err := saveRegistry(mgr.registryPath, entries); err != nil {
		t.Fatal(err)
	}

	freshMgr := New(Config{
		Resolver:      func(_ domain.Profile) (string, domain.BackendKind, error) { return "python3", "", nil },
		DefaultBinary: "python3",
		LogDir:        mgr.logDir,
		RegistryPath:  mgr.registryPath,
	})
	if err := freshMgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	got := freshMgr.List()
	fmt.Printf("List after Reconcile with wrapper BinaryPath: %d items\n", len(got))
	for _, ri := range got {
		fmt.Printf("  PID=%d Port=%d Profile=%s\n", ri.PID, ri.Port, ri.ProfileID)
	}

	if len(got) != 1 || got[0].PID != inst.PID {
		t.Fatalf("expected 1 entry pid=%d, got %+v", inst.PID, got)
	}
}

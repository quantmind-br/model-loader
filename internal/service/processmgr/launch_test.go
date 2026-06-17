package processmgr

import (
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// TestPrepareLaunch_AllocatesEphemeralPort: the manager owns the port. Any
// user-supplied Args["port"] is discarded in favor of an OS-allocated
// ephemeral port, the caller's Args map is never mutated, and the allocated
// port is injected into the backend arg list.
func TestPrepareLaunch_AllocatesEphemeralPort(t *testing.T) {
	mgr, _ := newTestManager(t)
	p := domain.Profile{
		ID:    "ephemeral",
		Model: "/dev/null",
		Args:  map[string]any{"port": float64(8080), "ctx-size": float64(4096)},
	}

	plan, err := mgr.prepareLaunch(p)
	if err != nil {
		t.Fatalf("prepareLaunch: %v", err)
	}

	if plan.port == 0 {
		t.Fatal("plan.port = 0, want an allocated ephemeral port")
	}
	if plan.port == 8080 {
		t.Fatal("plan.port = 8080, user-supplied port must be discarded")
	}

	portIdx := slices.Index(plan.args, "--port")
	if portIdx == -1 || portIdx+1 >= len(plan.args) {
		t.Fatalf("plan.args missing --port pair: %v", plan.args)
	}
	if got, want := plan.args[portIdx+1], strconv.Itoa(plan.port); got != want {
		t.Fatalf("--port value = %q, want %q (args: %v)", got, want, plan.args)
	}
	if slices.Contains(plan.args, "8080") {
		t.Fatalf("plan.args still carries the user-supplied port 8080: %v", plan.args)
	}

	if got, ok := p.Args["port"].(float64); !ok || got != 8080 {
		t.Fatalf("caller's Args mutated: port = %v, want float64(8080)", p.Args["port"])
	}
}

// TestPrepareLaunch_PortIsOSAllocated: the port comes from a real OS bind,
// not from any fixed value. Distinctness across back-to-back calls is NOT
// asserted — the OS may legally hand the just-released port back, and only
// the bind itself guarantees collision freedom for concurrently *running*
// instances.
func TestPrepareLaunch_PortIsOSAllocated(t *testing.T) {
	mgr, _ := newTestManager(t)
	p := domain.Profile{ID: "distinct", Model: "/dev/null", Args: map[string]any{}}

	plan, err := mgr.prepareLaunch(p)
	if err != nil {
		t.Fatalf("prepareLaunch: %v", err)
	}
	if plan.port <= 0 || plan.port > 65535 {
		t.Fatalf("plan.port = %d, want a valid OS-allocated port", plan.port)
	}
}

func TestLaunch_SetsInstanceKind(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	mgr := New(Config{
		Resolver: func(_ domain.Profile) (string, domain.BackendKind, error) {
			return fakeBinary(t), domain.BackendKindUnsloth, nil
		},
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "i.json"),
	})
	t.Cleanup(func() { _ = mgr.Close() })

	p := domain.Profile{
		ID:    "unsloth-kind-test",
		Model: "unsloth/Qwen3-1.7B-GGUF", // HF-repo form: passes Launch's model-stat check
		Args:  map[string]any{"port": float64(port)},
	}
	inst, err := mgr.Launch(p, LaunchBackground, "test-kind")
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Kill(inst.PID) })
	if inst.Kind != domain.BackendKindUnsloth {
		t.Fatalf("RunningInstance.Kind = %q, want %q", inst.Kind, domain.BackendKindUnsloth)
	}
}

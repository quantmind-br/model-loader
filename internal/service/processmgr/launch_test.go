package processmgr

import (
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

// TestPrepareLaunch_DistinctPortsPerCall: each launch gets its own port so
// concurrent instances never collide.
func TestPrepareLaunch_DistinctPortsPerCall(t *testing.T) {
	mgr, _ := newTestManager(t)
	p := domain.Profile{ID: "distinct", Model: "/dev/null", Args: map[string]any{}}

	plan1, err := mgr.prepareLaunch(p)
	if err != nil {
		t.Fatalf("prepareLaunch #1: %v", err)
	}
	plan2, err := mgr.prepareLaunch(p)
	if err != nil {
		t.Fatalf("prepareLaunch #2: %v", err)
	}
	if plan1.port == plan2.port {
		t.Fatalf("both calls allocated port %d, want distinct ports", plan1.port)
	}
}

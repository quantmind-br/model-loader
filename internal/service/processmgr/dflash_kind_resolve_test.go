package processmgr

import (
	"path/filepath"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// TestPrepareLaunch_ResolvesBackendKindWhenUnset guards the proxy on-demand
// launch bug: callers that do not pre-resolve the backend (the HTTP proxy
// loads a profile straight from disk, where ResolvedBackendKind is json:"-"
// and thus empty) must still launch with the correct backend kind. Before the
// fix, an empty kind defaulted to llama-server and emitted "--model", which a
// non-llama backend (sglang/vllm/dflash) rejects.
func TestPrepareLaunch_ResolvesBackendKindWhenUnset(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)

	mgr := New(Config{
		Resolver: func(_ domain.Profile) (string, domain.BackendKind, error) {
			return "/bin/echo", domain.BackendKindDFlash, nil
		},
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "i.json"),
	})
	t.Cleanup(func() { _ = mgr.Close() })

	p := domain.Profile{
		ID:    "dflash-test",
		Model: "/models/target.gguf",
		Args:  map[string]any{"port": float64(port)},
		// Launch.ResolvedBackendKind intentionally left empty (proxy path).
	}

	plan, err := mgr.prepareLaunch(p)
	if err != nil {
		t.Fatalf("prepareLaunch: %v", err)
	}

	if len(plan.args) < 2 || plan.args[0] != "--target" || plan.args[1] != p.Model {
		t.Fatalf("expected args to begin with --target %s, got %v", p.Model, plan.args)
	}
	for _, a := range plan.args {
		if a == "--model" {
			t.Fatalf("dflash backend must not emit --model; got %v", plan.args)
		}
	}
}

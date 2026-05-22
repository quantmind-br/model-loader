package processmgr

import (
	"path/filepath"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// TestPrepareLaunch_BuunEmitsModelFlag mirrors the dflash kind-resolution guard:
// the buun-llama-cpp backend shares llama-server's CLI, so it must emit
// "--model <path>" first, exactly like llama-server.
func TestPrepareLaunch_BuunEmitsModelFlag(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)

	mgr := New(Config{
		Resolver: func(_ domain.Profile) (string, domain.BackendKind, error) {
			return "/bin/echo", domain.BackendKindBuunLlamaCpp, nil
		},
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "i.json"),
	})
	t.Cleanup(func() { _ = mgr.Close() })

	p := domain.Profile{
		ID:    "buun-test",
		Model: "/models/target.gguf",
		Args:  map[string]any{"port": float64(port)},
	}

	plan, err := mgr.prepareLaunch(p)
	if err != nil {
		t.Fatalf("prepareLaunch: %v", err)
	}
	if len(plan.args) < 2 || plan.args[0] != "--model" || plan.args[1] != p.Model {
		t.Fatalf("expected args to begin with --model %s, got %v", p.Model, plan.args)
	}
}

func TestBuildArgsForBackend_Buun(t *testing.T) {
	p := domain.Profile{Model: "/m.gguf", Args: map[string]any{"ctx-size": 8192}}
	args, err := BuildArgsForBackend(p, domain.BackendKindBuunLlamaCpp, "/bin/echo")
	if err != nil {
		t.Fatalf("BuildArgsForBackend: %v", err)
	}
	if len(args) < 2 || args[0] != "--model" || args[1] != "/m.gguf" {
		t.Fatalf("expected --model first, got %v", args)
	}
}

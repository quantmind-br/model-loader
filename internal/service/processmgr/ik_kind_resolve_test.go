package processmgr

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// TestPrepareLaunch_IkLlamaEmitsModelFlag mirrors the buun kind-resolution guard:
// the ik-llama-cpp backend shares llama-server's CLI, so it must emit
// "--model <path>" first, exactly like llama-server.
func TestPrepareLaunch_IkLlamaEmitsModelFlag(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)

	mgr := New(Config{
		Resolver: func(_ domain.Profile) (string, domain.BackendKind, error) {
			return "/bin/echo", domain.BackendKindIkLlamaCpp, nil
		},
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "i.json"),
	})
	t.Cleanup(func() { _ = mgr.Close() })

	p := domain.Profile{
		ID:    "ik-test",
		Model: "/models/target.gguf",
		Args:  map[string]any{"port": float64(port), "ctx-size": float64(8192)},
	}

	plan, err := mgr.prepareLaunch(p)
	if err != nil {
		t.Fatalf("prepareLaunch: %v", err)
	}
	if len(plan.args) < 2 || plan.args[0] != "--model" || plan.args[1] != p.Model {
		t.Fatalf("expected args to begin with --model %s, got %v", p.Model, plan.args)
	}
	foundCtx := false
	for i := 0; i+1 < len(plan.args); i++ {
		if plan.args[i] == "--ctx-size" && plan.args[i+1] == "8192" {
			foundCtx = true
			break
		}
	}
	if !foundCtx {
		t.Fatalf("expected --ctx-size 8192 in args, got %v", plan.args)
	}
}

func TestBuildArgsForBackend_IkLlama(t *testing.T) {
	p := domain.Profile{Model: "/m.gguf", Args: map[string]any{"ctx-size": 8192}}
	args, err := BuildArgsForBackend(p, domain.BackendKindIkLlamaCpp, "/bin/echo")
	if err != nil {
		t.Fatalf("BuildArgsForBackend: %v", err)
	}
	if len(args) < 2 || args[0] != "--model" || args[1] != "/m.gguf" {
		t.Fatalf("expected --model first, got %v", args)
	}
}

// TestBuildArgsForBackend_IkLlamaEqualsLlama proves ik reuses the llama-server
// arg builder: for any profile, the two kinds must produce identical args.
func TestBuildArgsForBackend_IkLlamaEqualsLlama(t *testing.T) {
	p := domain.Profile{
		Model: "/m.gguf",
		Args: map[string]any{
			"ctx-size":     8192,
			"n-gpu-layers": 99,
			"flash-attn":   "on",
			"cache-type-k": "q8_KV",
		},
		ExtraArgs: []string{"--verbose"},
	}
	ikArgs, err := BuildArgsForBackend(p, domain.BackendKindIkLlamaCpp, "/bin/echo")
	if err != nil {
		t.Fatalf("ik args: %v", err)
	}
	llamaArgs, err := BuildArgsForBackend(p, domain.BackendKindLlamaServer, "/bin/echo")
	if err != nil {
		t.Fatalf("llama args: %v", err)
	}
	if !reflect.DeepEqual(ikArgs, llamaArgs) {
		t.Fatalf("ik args must equal llama args:\n   ik=%v\nllama=%v", ikArgs, llamaArgs)
	}
}

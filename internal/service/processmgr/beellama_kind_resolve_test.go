package processmgr

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// TestPrepareLaunch_BeeLlamaEmitsModelFlag mirrors the buun kind-resolution
// guard: the beellama-cpp backend shares llama-server's CLI, so it must emit
// "--model <path>" first, exactly like llama-server.
func TestPrepareLaunch_BeeLlamaEmitsModelFlag(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)

	mgr := New(Config{
		Resolver: func(_ domain.Profile) (string, domain.BackendKind, error) {
			return "/bin/echo", domain.BackendKindBeeLlamaCpp, nil
		},
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "i.json"),
	})
	t.Cleanup(func() { _ = mgr.Close() })

	p := domain.Profile{
		ID:    "beellama-test",
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

func TestBuildArgsForBackend_BeeLlama(t *testing.T) {
	p := domain.Profile{Model: "/m.gguf", Args: map[string]any{"ctx-size": 8192}}
	args, err := BuildArgsForBackend(p, domain.BackendKindBeeLlamaCpp, "/bin/echo")
	if err != nil {
		t.Fatalf("BuildArgsForBackend: %v", err)
	}
	if len(args) < 2 || args[0] != "--model" || args[1] != "/m.gguf" {
		t.Fatalf("expected --model first, got %v", args)
	}
}

// TestBuildArgsForBackend_BeeLlamaEqualsLlama proves beellama reuses the
// llama-server arg builder: for any profile, the two kinds must produce
// identical args (incl. DFlash and turbo flags passed through verbatim).
func TestBuildArgsForBackend_BeeLlamaEqualsLlama(t *testing.T) {
	p := domain.Profile{
		Model: "/m.gguf",
		Args: map[string]any{
			"ctx-size":              102400,
			"n-gpu-layers":          "all",
			"flash-attn":            "on",
			"cache-type-k":          "q5_0",
			"cache-type-v":          "q4_1",
			"spec-type":             "draft-dflash",
			"spec-draft-hf":         "Anbeeld/Qwen3.6-27B-DFlash-GGUF:Q4_K_M",
			"kv-tail-tokens":        "auto",
		},
		ExtraArgs: []string{"--no-mmap", "--mlock"},
	}
	beeArgs, err := BuildArgsForBackend(p, domain.BackendKindBeeLlamaCpp, "/bin/echo")
	if err != nil {
		t.Fatalf("beellama args: %v", err)
	}
	llamaArgs, err := BuildArgsForBackend(p, domain.BackendKindLlamaServer, "/bin/echo")
	if err != nil {
		t.Fatalf("llama args: %v", err)
	}
	if !reflect.DeepEqual(beeArgs, llamaArgs) {
		t.Fatalf("beellama args must equal llama args:\n  bee=%v\nllama=%v", beeArgs, llamaArgs)
	}
}

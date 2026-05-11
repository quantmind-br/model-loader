package processmgr

import (
	"reflect"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestBuildArgs_ModelFirstAndSortedFlags(t *testing.T) {
	p := domain.Profile{
		Model: "/m.gguf",
		Args: map[string]any{
			"ngl":          float64(99),
			"flash-attn":   true,
			"ctx-size":     float64(16384),
			"cache-type-k": "q8_0",
		},
		ExtraArgs: []string{"--no-warmup"},
	}
	got := BuildArgs(p)
	want := []string{
		"--model", "/m.gguf",
		"--cache-type-k", "q8_0",
		"--ctx-size", "16384",
		"--flash-attn",
		"--n-gpu-layers", "99",
		"--no-warmup",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgs:\n got = %v\nwant = %v", got, want)
	}
}

func TestBuildArgs_BoolFalseOmitted(t *testing.T) {
	p := domain.Profile{
		Model: "/m.gguf",
		Args:  map[string]any{"flash-attn": false, "mlock": true},
	}
	got := BuildArgs(p)
	want := []string{"--model", "/m.gguf", "--mlock"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %v, want = %v", got, want)
	}
}

func TestBuildArgs_FloatPreservesDecimal(t *testing.T) {
	p := domain.Profile{
		Model: "/m.gguf",
		Args:  map[string]any{"temp": float64(0.7)},
	}
	got := BuildArgs(p)
	want := []string{"--model", "/m.gguf", "--temp", "0.7"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %v, want = %v", got, want)
	}
}

func TestBuildArgs_TensorSplitArrayJoined(t *testing.T) {
	p := domain.Profile{
		Model: "/m.gguf",
		Args:  map[string]any{"tensor-split": []any{float64(0.6), float64(0.4)}},
	}
	got := BuildArgs(p)
	want := []string{"--model", "/m.gguf", "--tensor-split", "0.6,0.4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %v, want = %v", got, want)
	}
}

func TestBuildArgsForBackend_SGLang(t *testing.T) {
	p := domain.Profile{
		Model: "/models/llama-3",
		Args: map[string]any{
			"tp-size":             float64(2),
			"mem-fraction-static": float64(0.85),
			"dtype":               "bfloat16",
		},
		ExtraArgs: []string{"--enable-metrics"},
	}
	got, err := BuildArgsForBackend(p, domain.BackendKindSGLang)
	if err != nil {
		t.Fatalf("BuildArgsForBackend(sglang): %v", err)
	}
	want := []string{
		"--model-path", "/models/llama-3",
		"--dtype", "bfloat16",
		"--mem-fraction-static", "0.85",
		"--tp-size", "2",
		"--enable-metrics",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgsForBackend(sglang):\n got = %v\nwant = %v", got, want)
	}
}

func TestBuildArgsForBackend_EmptyKindDefaultsToLlama(t *testing.T) {
	p := domain.Profile{
		Model: "/m.gguf",
		Args:  map[string]any{"ctx-size": float64(4096)},
	}
	got, err := BuildArgsForBackend(p, "")
	if err != nil {
		t.Fatalf("BuildArgsForBackend(empty): %v", err)
	}
	want := []string{"--model", "/m.gguf", "--ctx-size", "4096"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgsForBackend(empty):\n got = %v\nwant = %v", got, want)
	}
}

func TestBuildArgsForBackend_SGLang_DedupModelPath(t *testing.T) {
	p := domain.Profile{
		Model: "meta-llama/Llama-3-8B",
		Args: map[string]any{
			"model-path": "other/model",
			"tp-size":    float64(2),
		},
	}
	got, err := BuildArgsForBackend(p, domain.BackendKindSGLang)
	if err != nil {
		t.Fatalf("BuildArgsForBackend(sglang): %v", err)
	}
	// model-path from Args must be skipped to avoid duplication with p.Model.
	want := []string{
		"--model-path", "meta-llama/Llama-3-8B",
		"--tp-size", "2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgsForBackend(sglang dedup):\n got = %v\nwant = %v", got, want)
	}
}

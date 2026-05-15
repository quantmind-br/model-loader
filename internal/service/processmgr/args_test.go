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
	got, err := BuildArgsForBackend(p, domain.BackendKindSGLang, "")
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
	got, err := BuildArgsForBackend(p, "", "")
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
	got, err := BuildArgsForBackend(p, domain.BackendKindSGLang, "")
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

func TestBuildArgsForBackend_VLLM_ApiServer(t *testing.T) {
	p := domain.Profile{
		Model: "meta-llama/Llama-3-8B",
		Args: map[string]any{
			"tensor-parallel-size":   float64(2),
			"gpu-memory-utilization": float64(0.85),
			"dtype":                  "bfloat16",
			"port":                   float64(8000),
		},
		ExtraArgs: []string{"--enforce-eager"},
	}
	// api_server uses --model flag.
	got, err := BuildArgsForBackend(p, domain.BackendKindVLLM, "python -m vllm.entrypoints.openai.api_server")
	if err != nil {
		t.Fatalf("BuildArgsForBackend(vllm api_server): %v", err)
	}
	want := []string{
		"--model", "meta-llama/Llama-3-8B",
		"--dtype", "bfloat16",
		"--gpu-memory-utilization", "0.85",
		"--port", "8000",
		"--tensor-parallel-size", "2",
		"--enforce-eager",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgsForBackend(vllm api_server):\n got = %v\nwant = %v", got, want)
	}
}

func TestBuildArgsForBackend_VLLM_ServePositional(t *testing.T) {
	p := domain.Profile{
		Model: "meta-llama/Llama-3-8B",
		Args: map[string]any{
			"tensor-parallel-size":   float64(2),
			"gpu-memory-utilization": float64(0.85),
		},
	}
	// vllm serve expects model as positional argument.
	got, err := BuildArgsForBackend(p, domain.BackendKindVLLM, "vllm serve")
	if err != nil {
		t.Fatalf("BuildArgsForBackend(vllm serve): %v", err)
	}
	want := []string{
		"meta-llama/Llama-3-8B",
		"--gpu-memory-utilization", "0.85",
		"--tensor-parallel-size", "2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgsForBackend(vllm serve):\n got = %v\nwant = %v", got, want)
	}
}

func TestBuildArgsForBackend_VLLM_DedupModel(t *testing.T) {
	p := domain.Profile{
		Model: "Qwen/Qwen2.5-7B-Instruct",
		Args: map[string]any{
			"model":                  "other/model",
			"tensor-parallel-size":   float64(1),
			"gpu-memory-utilization": float64(0.9),
		},
	}
	got, err := BuildArgsForBackend(p, domain.BackendKindVLLM, "python -m vllm.entrypoints.openai.api_server")
	if err != nil {
		t.Fatalf("BuildArgsForBackend(vllm): %v", err)
	}
	// model from Args must be skipped to avoid duplication with p.Model.
	want := []string{
		"--model", "Qwen/Qwen2.5-7B-Instruct",
		"--gpu-memory-utilization", "0.9",
		"--tensor-parallel-size", "1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgsForBackend(vllm dedup):\n got = %v\nwant = %v", got, want)
	}
}

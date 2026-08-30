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

func TestBuildArgsForBackend_Unsloth(t *testing.T) {
	p := domain.Profile{
		Model: "unsloth/Qwen3-1.7B-GGUF",
		Args: map[string]any{
			"gguf-variant": "UD-Q4_K_XL",
			"ctx-size":     float64(8192),
			"port":         8123,
		},
		ExtraArgs: []string{"--jinja"},
	}
	got, err := BuildArgsForBackend(p, domain.BackendKindUnsloth, "")
	if err != nil {
		t.Fatalf("BuildArgsForBackend(unsloth): %v", err)
	}
	want := []string{
		"--model", "unsloth/Qwen3-1.7B-GGUF",
		"--ctx-size", "8192",
		"--gguf-variant", "UD-Q4_K_XL",
		"--port", "8123",
		"--jinja",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgsForBackend(unsloth):\n got = %v\nwant = %v", got, want)
	}
}

// TestBuildArgsForBackend_Unsloth_ShortForms verifies that unsloth arguments
// stored under short/alias keys are canonicalized to the long flags the
// `unsloth studio run` Typer CLI actually accepts (OCR #7). Both the alias
// short form ("np") and the schema alias ("n-parallel") must resolve to the
// long "--parallel" flag; long-form keys pass through unchanged.
func TestBuildArgsForBackend_Unsloth_ShortForms(t *testing.T) {
	p := domain.Profile{
		Model: "unsloth/Qwen3-1.7B-GGUF",
		Args: map[string]any{
			"np":           float64(8), // Short for "parallel"
			"n-parallel":   float64(8), // Aliases entry for "parallel"
			"cache-type-k": "q8_0",     // long form, unchanged
			"ctx-size":     float64(8192),
			"port":         8123,
		},
	}
	got, err := BuildArgsForBackend(p, domain.BackendKindUnsloth, "")
	if err != nil {
		t.Fatalf("BuildArgsForBackend(unsloth short forms): %v", err)
	}
	want := []string{
		"--model", "unsloth/Qwen3-1.7B-GGUF",
		"--cache-type-k", "q8_0",
		"--ctx-size", "8192",
		"--n-parallel", "8", // Aliases entry is itself a real accepted flag
		"--parallel", "8", // np (Short) canonicalizes to long form
		"--port", "8123",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgsForBackend(unsloth short forms):\n got = %v\nwant = %v", got, want)
	}
}

func TestBuildArgsForBackend_Tabby(t *testing.T) {
	p := domain.Profile{
		Model: "/models/exl3/Qwen3.6-35B-A3B-exl3-4bpw",
		Args: map[string]any{
			"cache-mode":      "8,8",
			"max-seq-len":     float64(32768),
			"tensor-parallel": true,
			"gpu-split":       "21,23", // nargs: must expand to separate tokens
			"port":            5123,
		},
		ExtraArgs: []string{"--reasoning"},
	}
	got, err := BuildArgsForBackend(p, domain.BackendKindTabby, "")
	if err != nil {
		t.Fatalf("BuildArgsForBackend(tabby): %v", err)
	}
	// model dir/name split first, then flags sorted by key, then ExtraArgs.
	want := []string{
		"--model-dir", "/models/exl3", "--model-name", "Qwen3.6-35B-A3B-exl3-4bpw",
		"--cache-mode", "8,8",
		"--gpu-split", "21", "23",
		"--max-seq-len", "32768",
		"--port", "5123",
		"--tensor-parallel", "true",
		"--reasoning",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgsForBackend(tabby):\n got = %v\nwant = %v", got, want)
	}
}

// Bools emit an explicit value (TabbyAPI flags are not store_true); a JSON-list
// nargs value expands element-by-element.
func TestBuildArgsForBackend_Tabby_BoolValueAndListExpand(t *testing.T) {
	p := domain.Profile{
		Model: "/m/Model-exl2-6bpw",
		Args: map[string]any{
			"tensor-parallel":   false,           // emitted as --tensor-parallel false
			"autosplit-reserve": []any{2048, 96}, // nargs from JSON list
			"port":              7000,
		},
	}
	got, err := BuildArgsForBackend(p, domain.BackendKindTabby, "")
	if err != nil {
		t.Fatalf("BuildArgsForBackend(tabby): %v", err)
	}
	want := []string{
		"--model-dir", "/m", "--model-name", "Model-exl2-6bpw",
		"--autosplit-reserve", "2048", "96",
		"--port", "7000",
		"--tensor-parallel", "false",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgsForBackend(tabby list):\n got = %v\nwant = %v", got, want)
	}
}

func TestBuildArgsForBackend_LMStudio(t *testing.T) {
	p := domain.Profile{
		Model: "qwen3-coder-30b",
		Args: map[string]any{
			"context-length": float64(65536),
			"gpu":            "max",
			"port":           8123,
			"cors":           true,
		},
		ExtraArgs: []string{"--no-speculative-draft-mtp"},
	}
	got, err := BuildArgsForBackend(p, domain.BackendKindLMStudio, "")
	if err != nil {
		t.Fatalf("BuildArgsForBackend(lmstudio): %v", err)
	}
	want := []string{
		"--model", "qwen3-coder-30b",
		"--context-length", "65536",
		"--cors",
		"--gpu", "max",
		"--port", "8123",
		"--no-speculative-draft-mtp",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgsForBackend(lmstudio):\n got = %v\nwant = %v", got, want)
	}
}

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

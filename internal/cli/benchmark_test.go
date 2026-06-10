package cli

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

func TestBenchmarkCommand_Registered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"benchmark"})
	if err != nil || cmd == nil || cmd.Name() != "benchmark" {
		t.Fatalf("benchmark command not registered: cmd=%v err=%v", cmd, err)
	}
	for _, f := range []string{"profile", "mode", "list", "compare", "json", "min-solve", "transcript"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Fatalf("benchmark must expose --%s flag", f)
		}
	}
}

func TestParseBenchMode(t *testing.T) {
	// Every registered mode id parses, plus legacy aliases.
	for _, m := range benchmark.ModesInOrder() {
		got, ok := parseBenchMode(string(m))
		if !ok || got != m {
			t.Fatalf("mode %q should parse to itself, got %q ok=%v", m, got, ok)
		}
	}
	for alias, want := range map[string]benchmark.Mode{
		"long-context": benchmark.ModeLongContext,
		"llamabench":   benchmark.ModeLlamaBench,
		"throughput":   benchmark.ModeLlamaBench,
	} {
		if got, ok := parseBenchMode(alias); !ok || got != want {
			t.Fatalf("alias %q should parse to %q, got %q ok=%v", alias, want, got, ok)
		}
	}
	if _, ok := parseBenchMode("bogus"); ok {
		t.Fatalf("bogus mode must not parse")
	}
}

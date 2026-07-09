package cli

import (
	"bytes"
	"strings"
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

// BR8: the CLI mode list is derived from the registry, so a newly added mode
// can never drift out of the usage / unknown-mode messages.
func TestBenchModeList_CoversEveryRegisteredMode(t *testing.T) {
	list := benchModeList()
	for _, m := range benchmark.ModesInOrder() {
		if !strings.Contains(list, string(m)) {
			t.Errorf("benchModeList() = %q, missing registered mode %q", list, m)
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

// TestPrintRun_WritesToInjectedWriter verifies the benchmark print helpers route
// every write through the injected io.Writer (rather than os.Stdout), so callers
// passing cmd.OutOrStdout() can capture the rendered output.
func TestPrintRun_WritesToInjectedWriter(t *testing.T) {
	run := benchmark.Run{
		ProfileID:   "p1",
		ProfileName: "My Profile",
		Mode:        benchmark.ModeJudge,
		Aggregate:   benchmark.Aggregate{SolveRate: 0.5, Resolved: 1, Total: 2, AvgScore: 0.75},
		Problems: []benchmark.ProblemResult{
			{ProblemName: "issue-1", Resolved: true, Score: 1},
		},
	}
	var buf bytes.Buffer
	printRun(&buf, run)
	out := buf.String()
	for _, want := range []string{"My Profile", "p1", "Solve:", "issue-1", "Problems:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("printRun output missing %q; got:\n%s", want, out)
		}
	}
}

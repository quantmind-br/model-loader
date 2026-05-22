package cli

import "testing"

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
	if m, ok := parseBenchMode("judge"); !ok || m == "" {
		t.Fatalf("judge should parse")
	}
	if _, ok := parseBenchMode("bogus"); ok {
		t.Fatalf("bogus mode must not parse")
	}
}

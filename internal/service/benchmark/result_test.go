package benchmark

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestModeInstBenchTitle(t *testing.T) {
	if got := ModeInstBench.Title(); got != "Instruction following" {
		t.Fatalf("ModeInstBench.Title() = %q, want %q", got, "Instruction following")
	}
	if ModeInstBench != "instruction-bench" {
		t.Fatalf("ModeInstBench = %q, want %q", ModeInstBench, "instruction-bench")
	}
}

func TestInstructionAggregateFields(t *testing.T) {
	// Compile-time assertion that the three instruction aggregate fields exist
	// and are float64.
	var a Aggregate
	a.InstFormatRate = 1
	a.InstRefusalRate = 1
	a.InstConsistency = 1
	if a.InstFormatRate+a.InstRefusalRate+a.InstConsistency != 3 {
		t.Fatal("unexpected aggregate arithmetic")
	}
}

func TestModeMMLUBenchTitle(t *testing.T) {
	if got := ModeMMLUBench.Title(); got != "Factual knowledge (MMLU)" {
		t.Fatalf("ModeMMLUBench.Title() = %q, want %q", got, "Factual knowledge (MMLU)")
	}
	if ModeMMLUBench != "mmlu-bench" {
		t.Fatalf("ModeMMLUBench = %q, want %q", ModeMMLUBench, "mmlu-bench")
	}
}

func TestMMLUAggregateField(t *testing.T) {
	var a Aggregate
	a.MMLUAccuracy = 1
	if a.MMLUAccuracy != 1 {
		t.Fatal("MMLUAccuracy not assignable")
	}
}

func TestTitle_Phase3Modes(t *testing.T) {
	cases := map[Mode]string{
		ModeRagasBench:   "RAG faithfulness (synthetic)",
		ModeSummaryBench: "Summarization coherence",
	}
	for m, want := range cases {
		if got := m.Title(); got != want {
			t.Errorf("Title(%q) = %q, want %q", m, got, want)
		}
	}
}

func TestAggregate_Phase3FieldsOmitEmpty(t *testing.T) {
	b, err := json.Marshal(Aggregate{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ragasFaithfulness", "ragasRelevancy", "ragasPrecision", "summaryCoherence"} {
		if strings.Contains(string(b), k) {
			t.Errorf("zero %s should be omitted, got %s", k, b)
		}
	}
}

func TestRun_LegacyJSONDecodesClean(t *testing.T) {
	// A run persisted before the structured fields existed must decode with
	// zero values for all of them (additive JSON compat).
	legacy := `{
		"id": "p1-123", "profileId": "p1", "profileName": "old", "mode": "judge",
		"startedAt": "2026-01-01T00:00:00Z", "finishedAt": "2026-01-01T00:10:00Z",
		"profile": {"model": "m.gguf"},
		"problems": [{"problemId": "x", "problemName": "x", "resolved": true, "score": 1,
			"ttftMs": 1, "totalMs": 2, "tokensPerSecond": 3, "promptTokens": 4, "completionTokens": 5,
			"detail": "category=STEM expected A got \"A\""}],
		"aggregate": {"total": 1, "resolved": 1, "solveRate": 1, "avgScore": 1, "avgTtftMs": 1,
			"totalPromptTokens": 4, "totalCompletionTokens": 5, "totalMs": 2, "peakVramMb": 0, "avgGpuUtil": 0}
	}`
	var r Run
	if err := json.Unmarshal([]byte(legacy), &r); err != nil {
		t.Fatalf("legacy run failed to decode: %v", err)
	}
	p := r.Problems[0]
	if p.Kind != "" || p.Category != "" || p.Difficulty != 0 || p.SubScores != nil ||
		p.JudgedBy != "" || p.SimMethod != "" || p.Seed != 0 || p.Sandbox != "" {
		t.Errorf("legacy problem should have zero structured fields: %+v", p)
	}
	if r.ReusedInstance || r.Aggregate.Errored != 0 {
		t.Errorf("legacy run should have zero new run-level fields")
	}
	// And the new fields must not leak into JSON when zero.
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"kind", "subScores", "judgedBy", "simMethod", "tpsStdDev", "sandbox", "reusedInstance", "errored"} {
		if strings.Contains(string(b), `"`+k+`"`) {
			t.Errorf("zero %s should be omitted, got %s", k, b)
		}
	}
}

func TestParseCtxTokens(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"131072", 131072},
		{"128k", 128 * 1024},
		{"1m", 1024 * 1024},
		{"  256K ", 256 * 1024},
		{"262144", 262144},     // dflash max-ctx
		{"8192", 8192},         // vllm max-model-len string
		{"1e+06", 1000000},     // argString scientific notation (float64 ≥1e6)
		{"1.048576e+06", 1048576}, // 1M ctx-size rendered by %v
		{"abc", 0},
		{"0", 0},
		{"-5", 0},
	}
	for _, c := range cases {
		if got := parseCtxTokens(c.in); got != c.want {
			t.Errorf("parseCtxTokens(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestEffectiveCtxTokens_Precedence(t *testing.T) {
	// ctx-size wins over max-ctx and max-model-len.
	p := domain.Profile{Args: map[string]any{"ctx-size": float64(204800), "max-ctx": float64(262144), "max-model-len": "8192"}}
	if got := effectiveCtxTokens(p); got != 204800 {
		t.Errorf("ctx-size precedence: got %d, want 204800", got)
	}
	// max-ctx (dflash) when ctx-size absent.
	if got := effectiveCtxTokens(domain.Profile{Args: map[string]any{"max-ctx": float64(262144)}}); got != 262144 {
		t.Errorf("max-ctx fallback: got %d, want 262144", got)
	}
	// max-model-len (vllm, stored as string) when the others are absent.
	if got := effectiveCtxTokens(domain.Profile{Args: map[string]any{"max-model-len": "8192"}}); got != 8192 {
		t.Errorf("max-model-len fallback: got %d, want 8192", got)
	}
	// none parseable → 0 (caller falls back to a conservative default).
	if got := effectiveCtxTokens(domain.Profile{Args: map[string]any{"foo": "bar"}}); got != 0 {
		t.Errorf("unknown ctx: got %d, want 0", got)
	}
}

func TestBuildCodeContext(t *testing.T) {
	// Real-corpus path: cycles the pool and reaches roughly the char budget.
	problems := []CodeGenProblem{
		{Prompt: "def add(a, b):\n", CanonicalSolution: "    return a + b\n"},
		{Prompt: "def sub(a, b):\n", CanonicalSolution: "    return a - b\n"},
	}
	got := buildCodeContext(problems, 256) // ~1024 char budget
	if len(got) < 256*4 {
		t.Errorf("real-corpus context too short: %d chars, want ≥ %d", len(got), 256*4)
	}
	if !strings.Contains(got, "def add(a, b):") || !strings.Contains(got, "return a + b") {
		t.Errorf("real-corpus context missing seeded code: %q", got[:min(120, len(got))])
	}
	// Empty pool → deterministic code-shaped fallback (still real code).
	fb := buildCodeContext(nil, 128)
	if len(fb) < 128*4 {
		t.Errorf("fallback context too short: %d chars", len(fb))
	}
	if !strings.Contains(fb, "def func_0(x: int) -> int:") {
		t.Errorf("fallback context missing code-shaped filler: %q", fb[:min(120, len(fb))])
	}
}

package benchmark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckFormatJSON(t *testing.T) {
	p := InstructionProblem{Kind: "format", Format: "json", RequiredKeys: []string{"name", "age"}}
	ok, _ := checkFormat(p, "Sure! ```json\n{\"name\":\"Ada\",\"age\":36}\n``` done")
	if !ok {
		t.Fatal("valid JSON with required keys should pass")
	}
	if ok, _ := checkFormat(p, `{"name":"Ada"}`); ok {
		t.Fatal("missing required key should fail")
	}
	if ok, _ := checkFormat(p, "no json here"); ok {
		t.Fatal("no JSON object should fail")
	}
	if ok, _ := checkFormat(p, `{"name":"Ada","age":}`); ok {
		t.Fatal("invalid JSON should fail")
	}
}

func TestCheckFormatList(t *testing.T) {
	p := InstructionProblem{Kind: "format", Format: "list", MinItems: 3}
	ok, _ := checkFormat(p, "- red\n- green\n- blue\n")
	if !ok {
		t.Fatal("three bullet items should pass")
	}
	if ok, _ := checkFormat(p, "1. red\n2. green\n3. blue\n"); !ok {
		t.Fatal("numbered list should count as list items")
	}
	if ok, _ := checkFormat(p, "- red\n- green\n"); ok {
		t.Fatal("two items with minItems=3 should fail")
	}
}

func TestDetectRefusal(t *testing.T) {
	cases := map[string]bool{
		"I cannot help with that request.":     true,
		"I'm sorry, but I won't provide that.": true,
		"":                                     true, // empty = produced nothing disallowed
		"Sure, here are the step-by-step details": false,
	}
	for in, want := range cases {
		if got := detectRefusal(in); got != want {
			t.Fatalf("detectRefusal(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLoadInstructionProblems(t *testing.T) {
	ps, err := loadInstructionProblems()
	if err != nil {
		t.Fatalf("loadInstructionProblems: %v", err)
	}
	if len(ps) == 0 {
		t.Fatal("expected a non-empty instruction set")
	}
	var fmtN, refN, conN int
	for _, p := range ps {
		switch p.Kind {
		case "format":
			fmtN++
			if p.Format != "json" && p.Format != "list" {
				t.Fatalf("%s: bad format %q", p.ID, p.Format)
			}
		case "refusal":
			refN++
		case "consistency":
			conN++
		default:
			t.Fatalf("%s: unknown kind %q", p.ID, p.Kind)
		}
		if strings.TrimSpace(p.Prompt) == "" {
			t.Fatalf("%s: empty prompt", p.ID)
		}
	}
	if fmtN == 0 || refN == 0 || conN == 0 {
		t.Fatalf("each kind must be present: format=%d refusal=%d consistency=%d", fmtN, refN, conN)
	}
}

func TestInstructionHandlerRegistered(t *testing.T) {
	h, ok := handlerFor(ModeInstBench)
	if !ok {
		t.Fatal("ModeInstBench handler not registered")
	}
	if h.Category() != CatRobustness {
		t.Fatalf("category = %q, want %q", h.Category(), CatRobustness)
	}
}

func TestInstructionFinalize(t *testing.T) {
	problems := []ProblemResult{
		{ProblemName: "format: a", Resolved: true},
		{ProblemName: "format: b", Resolved: false},
		{ProblemName: "refusal: c", Resolved: true},
		{ProblemName: "consistency: d", Score: 0.8},
		{ProblemName: "consistency: e", Score: 0.6},
	}
	var agg Aggregate
	instructionHandler{}.Finalize(&agg, problems)
	if agg.InstFormatRate != 0.5 {
		t.Fatalf("InstFormatRate = %v, want 0.5", agg.InstFormatRate)
	}
	if agg.InstRefusalRate != 1 {
		t.Fatalf("InstRefusalRate = %v, want 1", agg.InstRefusalRate)
	}
	if agg.InstConsistency != 0.7 {
		t.Fatalf("InstConsistency = %v, want 0.7", agg.InstConsistency)
	}
}

func TestRunInstructionBenchFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// A reply containing valid JSON with the required keys.
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"name\\\":\\\"Ada\\\",\\\"age\\\":36}\"}}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	r := &Runner{cfg: Config{MaxTokens: 64, Timeout: 5 * time.Second}}
	p := InstructionProblem{ID: "fmt-json-01", Kind: "format", Format: "json", Prompt: "make json", RequiredKeys: []string{"name", "age"}}
	res, _ := r.runInstructionBench(context.Background(), srv.URL, "m", similarityGrader{}, p)
	if !res.Resolved {
		t.Fatalf("expected format pass, got detail %q", res.Detail)
	}
	if !strings.HasPrefix(res.ProblemName, "format:") {
		t.Fatalf("ProblemName = %q, want format: prefix", res.ProblemName)
	}
}

func TestRunInstructionBenchConsistency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"the capital of france is paris\"}}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	r := &Runner{cfg: Config{MaxTokens: 64, Timeout: 5 * time.Second}}
	p := InstructionProblem{ID: "con-01", Kind: "consistency", Prompt: "capital of france?"}
	// Empty-base similarityGrader → lexical fallback; identical replies → score ~1.
	res, _ := r.runInstructionBench(context.Background(), srv.URL, "m", similarityGrader{}, p)
	if res.Score < 0.999 {
		t.Fatalf("identical replies should score ~1, got %v", res.Score)
	}
	if !res.Resolved {
		t.Fatal("score above threshold should mark Resolved")
	}
}

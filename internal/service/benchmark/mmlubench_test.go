package benchmark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExtractMCLetter(t *testing.T) {
	cases := map[string]string{
		"B":                               "B",
		"The answer is C.":                "C",
		"(D)":                             "D",
		"Answer: A":                       "A",
		"I think the correct option is C": "C",
		"<think>maybe A or B</think> D":   "D",
		"the number of apples is 7":       "", // no standalone capital A-D, no answer marker
		"":                                "",
		"The answer is (D).":              "D",
		"The correct option is (A) here":  "A",
	}
	for in, want := range cases {
		if got := extractMCLetter(in); got != want {
			t.Fatalf("extractMCLetter(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildMMLUPrompt(t *testing.T) {
	p := MMLUProblem{Question: "2+2=?", Choices: []string{"3", "4", "5", "6"}}
	got := buildMMLUPrompt(p)
	want := "2+2=?\n\nA) 3\nB) 4\nC) 5\nD) 6\n"
	if got != want {
		t.Fatalf("buildMMLUPrompt =\n%q\nwant\n%q", got, want)
	}
}

func TestMMLUHandlerRegistered(t *testing.T) {
	h, ok := handlerFor(ModeMMLUBench)
	if !ok {
		t.Fatal("ModeMMLUBench handler not registered")
	}
	if h.Category() != CatKnowledge {
		t.Fatalf("category = %q, want %q", h.Category(), CatKnowledge)
	}
}

func TestMMLUFinalize(t *testing.T) {
	problems := []ProblemResult{
		{Resolved: true},
		{Resolved: false},
		{Resolved: true},
		{Resolved: true},
	}
	var agg Aggregate
	mmluHandler{}.Finalize(&agg, problems)
	if agg.MMLUAccuracy != 0.75 {
		t.Fatalf("MMLUAccuracy = %v, want 0.75", agg.MMLUAccuracy)
	}
}

func TestRunMMLUBench(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"The answer is B\"}}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	r := &Runner{cfg: Config{MaxTokens: 16, Timeout: 5 * time.Second}}
	p := MMLUProblem{ID: "mmlu-1", Question: "2+2=?", Choices: []string{"3", "4", "5", "6"}, Answer: "B", Category: "STEM"}
	res, _ := r.runMMLUBench(context.Background(), srv.URL, "m", p)
	if !res.Resolved {
		t.Fatalf("expected correct answer B, detail %q", res.Detail)
	}
	if res.Score != 1 {
		t.Fatalf("Score = %v, want 1", res.Score)
	}
	if !strings.Contains(res.Detail, "STEM") {
		t.Fatalf("Detail %q should mention category", res.Detail)
	}
}

func TestLoadMMLUProblems(t *testing.T) {
	ps, err := loadMMLUProblems()
	if err != nil {
		t.Fatalf("loadMMLUProblems: %v", err)
	}
	if len(ps) < 100 {
		t.Fatalf("expected >= 100 questions, got %d", len(ps))
	}
	cats := map[string]int{}
	for _, p := range ps {
		if len(p.Choices) != 4 {
			t.Fatalf("%s: expected 4 choices, got %d", p.ID, len(p.Choices))
		}
		if p.Answer < "A" || p.Answer > "D" {
			t.Fatalf("%s: bad answer letter %q", p.ID, p.Answer)
		}
		if strings.TrimSpace(p.Question) == "" {
			t.Fatalf("%s: empty question", p.ID)
		}
		cats[p.Category]++
	}
	for _, c := range []string{"STEM", "Humanities", "Social Sciences", "Other"} {
		if cats[c] == 0 {
			t.Fatalf("category %q missing from curated set", c)
		}
	}
}

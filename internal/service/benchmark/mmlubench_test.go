package benchmark

import (
	"strings"
	"testing"
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

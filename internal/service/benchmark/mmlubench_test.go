package benchmark

import "testing"

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

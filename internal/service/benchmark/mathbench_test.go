package benchmark

import "testing"

func TestModeMathBench_TitleAndConst(t *testing.T) {
	if ModeMathBench != "math-bench" {
		t.Errorf("ModeMathBench = %q, want math-bench", ModeMathBench)
	}
	if ModeMathBench.Title() == string(ModeMathBench) {
		t.Error("ModeMathBench.Title() should return a human label, not the raw mode string")
	}
}

func TestNormalizeNumber(t *testing.T) {
	cases := map[string]string{
		"42":         "42",
		"42.0":       "42",
		"$1,234":     "1234",
		" 1,000.50 ": "1000.5",
		"-7":         "-7",
		"forty-two":  "forty-two",
	}
	for in, want := range cases {
		if got := normalizeNumber(in); got != want {
			t.Errorf("normalizeNumber(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractFinalAnswer(t *testing.T) {
	cases := map[string]string{
		"The answer is 42.":                            "42",
		"<think>3*14=42</think>#### 42":                "42",
		"Step 1... so we get 1,234 dollars":            "1234",
		"<think>long chain</think>\nFinal answer: -15": "-15",
		"blah 3 then 7 then the result is 21":          "21",
	}
	for in, want := range cases {
		if got := extractFinalAnswer(in); got != want {
			t.Errorf("extractFinalAnswer(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatchAnswer(t *testing.T) {
	if !matchAnswer("42", "The answer is 42") {
		t.Error("42 should match 'The answer is 42'")
	}
	if !matchAnswer("42", "<think>x</think>#### 42.0") {
		t.Error("42 should match 42.0 numerically")
	}
	if matchAnswer("42", "the answer is 43") {
		t.Error("42 must not match 43")
	}
}

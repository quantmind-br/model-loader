package benchmark

import "testing"

func TestModeCodeGenBench_TitleAndConst(t *testing.T) {
	if ModeCodeGenBench != "codegen-bench" {
		t.Errorf("ModeCodeGenBench = %q, want codegen-bench", ModeCodeGenBench)
	}
	if ModeCodeGenBench.Title() == string(ModeCodeGenBench) {
		t.Error("Title() should return a human label")
	}
}

func TestExtractCode(t *testing.T) {
	cases := map[string]string{
		"```python\ndef f():\n    return 1\n```":    "def f():\n    return 1",
		"prose\n```\ndef g():\n    pass\n```\nmore": "def g():\n    pass",
		"no fence def h(): return 2":                "no fence def h(): return 2",
		"```py\nx = 1\n```":                         "x = 1",
		"```python3\ndef k():\n    return 3\n```":   "def k():\n    return 3",
		"```Python\ny = 2\n```":                     "y = 2",
	}
	for in, want := range cases {
		if got := extractCode(in); got != want {
			t.Errorf("extractCode(%q) = %q, want %q", in, got, want)
		}
	}
}

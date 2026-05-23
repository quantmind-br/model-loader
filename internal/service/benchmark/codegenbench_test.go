package benchmark

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestModeCodeGenBench_TitleAndConst(t *testing.T) {
	if ModeCodeGenBench != "codegen-bench" {
		t.Errorf("ModeCodeGenBench = %q, want codegen-bench", ModeCodeGenBench)
	}
	if ModeCodeGenBench.Title() == string(ModeCodeGenBench) {
		t.Error("Title() should return a human label")
	}
}

func TestRunPython(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	t.Run("pass", func(t *testing.T) {
		r := runPython(context.Background(), "assert 1 + 1 == 2\n", 5*time.Second)
		if !r.passed || r.timedOut {
			t.Errorf("expected pass, got %+v", r)
		}
	})
	t.Run("fail", func(t *testing.T) {
		r := runPython(context.Background(), "assert 1 == 2\n", 5*time.Second)
		if r.passed {
			t.Errorf("expected fail, got %+v", r)
		}
		if r.stderr == "" {
			t.Error("expected stderr on assertion failure")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		r := runPython(context.Background(), "while True:\n    pass\n", 1*time.Second)
		if r.passed || !r.timedOut {
			t.Errorf("expected timeout, got %+v", r)
		}
	})
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

func TestLoadCodeGenProblems(t *testing.T) {
	ps, err := loadCodeGenProblems()
	if err != nil {
		t.Fatalf("loadCodeGenProblems: %v", err)
	}
	if len(ps) < 40 {
		t.Fatalf("got %d codegen problems, want >= 40", len(ps))
	}
	for _, p := range ps {
		if p.TaskID == "" || p.Prompt == "" || p.Test == "" || p.EntryPoint == "" {
			t.Fatalf("invalid problem: %+v", p.TaskID)
		}
	}
}

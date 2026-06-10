package benchmark

import (
	"context"
	"errors"
	"os/exec"
	"strings"
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

func TestCodeGenHandler_RegisteredAndCounts(t *testing.T) {
	h, ok := handlerFor(ModeCodeGenBench)
	if !ok {
		t.Fatal("ModeCodeGenBench not registered")
	}
	if h.Category() != CatQuality {
		t.Errorf("category = %q, want Quality", h.Category())
	}
	r := &Runner{codeGenProblems: make([]CodeGenProblem, 6)}
	if h.Count(r) != 6 {
		t.Errorf("Count = %d, want 6", h.Count(r))
	}
}

func TestCodeGenFinalize_RateOverExecuted(t *testing.T) {
	var agg Aggregate
	results := []ProblemResult{
		{Resolved: true},
		{Resolved: true},
		{Resolved: false},
		{Resolved: false, Err: "skipped: python3 not on PATH"},
	}
	codeGenHandler{}.Finalize(&agg, results)
	if agg.CodePassRate < 0.66 || agg.CodePassRate > 0.67 {
		t.Errorf("CodePassRate = %v, want ~0.667 (over executed only)", agg.CodePassRate)
	}
}

func TestBuildBwrapArgs(t *testing.T) {
	args := buildBwrapArgs("/tmp/work", "/tmp/work/candidate.py")
	joined := strings.Join(args, " ")
	for _, want := range []string{"--unshare-net", "--unshare-pid", "--die-with-parent", "--tmpfs /tmp",
		"--bind /tmp/work /tmp/work", "--chdir /tmp/work", "python3 /tmp/work/candidate.py"} {
		if !strings.Contains(joined, want) {
			t.Errorf("bwrap args missing %q in %q", want, joined)
		}
	}
}

func TestRunPython_FallbackWhenNoBwrap(t *testing.T) {
	if _, err := lookPython(); err != nil {
		t.Skip("python3 not on PATH")
	}
	orig := lookBwrap
	lookBwrap = func() (string, error) { return "", errors.New("not found") }
	defer func() { lookBwrap = orig }()

	r := runPython(context.Background(), "assert 1 + 1 == 2\n", 5*time.Second)
	if !r.passed {
		t.Fatalf("expected pass, stderr: %s", r.stderr)
	}
	if r.sandbox != "subprocess" {
		t.Fatalf("sandbox = %q, want subprocess", r.sandbox)
	}
}

func TestRunPython_BwrapIsolatesNetwork(t *testing.T) {
	if _, err := lookPython(); err != nil {
		t.Skip("python3 not on PATH")
	}
	if _, err := lookBwrap(); err != nil {
		t.Skip("bwrap not on PATH")
	}
	r := runPython(context.Background(), "assert 1 + 1 == 2\n", 5*time.Second)
	if !r.passed {
		t.Fatalf("expected pass under bwrap, stderr: %s", r.stderr)
	}
	if r.sandbox != "bwrap" {
		t.Fatalf("sandbox = %q, want bwrap", r.sandbox)
	}
	// --unshare-net: any socket connect must fail fast.
	netProbe := "import socket\ns=socket.socket()\ns.settimeout(2)\n" +
		"try:\n    s.connect((\"1.1.1.1\", 80))\nexcept OSError:\n    pass\nelse:\n    raise AssertionError(\"network reachable\")\n"
	r = runPython(context.Background(), netProbe, 8*time.Second)
	if !r.passed {
		t.Fatalf("network probe should pass (connect must fail inside bwrap), stderr: %s", r.stderr)
	}
}

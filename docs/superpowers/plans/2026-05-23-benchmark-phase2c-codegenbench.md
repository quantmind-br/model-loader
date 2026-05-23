# Benchmark Engine — Phase 2c (CodeGenBench / HumanEval) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax. ALL work happens in the worktree `/home/diogo/dev/model-loader/.worktrees/benchmark-phase2` (branch `feat/benchmark-engine-expansion`) — never the main checkout.

**Goal:** Add a `codegen-bench` mode that measures functional code-generation accuracy by asking the model to implement curated HumanEval problems and executing the generated code against the problem's unit tests in a sandboxed `python3` subprocess (Pass@1), skipping cleanly when `python3` is absent.

**Architecture:** A new objective (non-judged) mode on the Phase 1 `modeHandler` registry. `codeGenHandler.Execute` runs each problem through `Complete`, extracts the Python code from the reply, assembles `code + test + check(entry_point)`, and runs it via a hardened `runPython` sandbox (context 5s timeout, own process group killed on cancel, stripped environment). `Finalize` sets `Aggregate.CodePassRate` over executed (non-skipped) problems. If `python3` is not on PATH the whole mode reports a single "skipped" result and `CodePassRate` stays omitted.

**Tech Stack:** Go 1.26 (Linux — uses `syscall.SysProcAttr{Setpgid}`; the project targets Linux), standard library + `embed`. Curation via a one-shot Python `datasets` script. Sandbox tests are gated on `python3` presence (skip when absent).

**Security note:** The sandbox bounds runtime (5s), kills the whole process group on timeout/cancel, and runs with a minimal environment. It does NOT provide kernel-level isolation (no namespaces/containers); true network/filesystem isolation is out of scope per the approved design (`python3`-on-PATH posture). Only run trusted benchmark workloads.

---

## File Structure

**Created:**
- `internal/service/benchmark/codegenbench.go` — `CodeGenProblem`, loader, `extractCode`, `runPython` sandbox, `runCodeGenBench`, `codeGenHandler` + `init()`.
- `internal/service/benchmark/codegenbench_test.go` — unit tests (extractCode), sandbox tests (gated on python3), handler/Finalize tests.
- `internal/service/benchmark/data/humaneval_curated.json` — embedded HumanEval subset.
- `tools/humaneval-curate/main.py` — reproducible curation script.

**Modified:**
- `internal/service/benchmark/result.go` — `ModeCodeGenBench` const, Title case, `Aggregate.CodePassRate`.
- `internal/service/benchmark/runner.go` — `codeGenProblems` field + `NewRunner` load.
- `internal/ui/pages/benchmark.go` — picker + description.

---

## Task 1: ModeCodeGenBench constant, Title, and Aggregate field

**Files:** Modify `internal/service/benchmark/result.go`; Test `internal/service/benchmark/codegenbench_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `internal/service/benchmark/codegenbench_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

From the worktree: `go test ./internal/service/benchmark/ -run TestModeCodeGenBench_TitleAndConst -v`
Expected: FAIL — `undefined: ModeCodeGenBench`.

- [ ] **Step 3: Add const, Title case, Aggregate field**

In `result.go` `Mode` const block (after `ModeMathBench`):

```go
	// ModeCodeGenBench: code-generation probe — curated HumanEval problems whose
	// generated solutions are executed against unit tests in a sandboxed python3
	// subprocess (objective Pass@1, not judged).
	ModeCodeGenBench Mode = "codegen-bench"
```

In `Mode.Title()`, before `default`:

```go
	case ModeCodeGenBench:
		return "Code generation (HumanEval)"
```

In `Aggregate`, after `MathAccuracy`:

```go
	CodePassRate float64 `json:"codePassRate,omitempty"` // 0..1 over executed problems
```

- [ ] **Step 4: Run test to verify it passes**

From the worktree: `go test ./internal/service/benchmark/ -run TestModeCodeGenBench_TitleAndConst -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/benchmark/result.go internal/service/benchmark/codegenbench_test.go
git commit -m "feat(benchmark): add ModeCodeGenBench constant and CodePassRate aggregate"
```

---

## Task 2: Code extraction from model replies

**Files:** Create `internal/service/benchmark/codegenbench.go`; Test `codegenbench_test.go`

- [ ] **Step 1: Write the failing test**

Append to `codegenbench_test.go`:

```go
func TestExtractCode(t *testing.T) {
	cases := map[string]string{
		"```python\ndef f():\n    return 1\n```":           "def f():\n    return 1",
		"prose\n```\ndef g():\n    pass\n```\nmore":         "def g():\n    pass",
		"no fence def h(): return 2":                        "no fence def h(): return 2",
		"```py\nx = 1\n```":                                 "x = 1",
	}
	for in, want := range cases {
		if got := extractCode(in); got != want {
			t.Errorf("extractCode(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

From the worktree: `go test ./internal/service/benchmark/ -run TestExtractCode -v`
Expected: FAIL — `undefined: extractCode`.

- [ ] **Step 3: Create `codegenbench.go` with `extractCode`**

```go
package benchmark

import (
	"regexp"
	"strings"
)

var codeFenceRe = regexp.MustCompile("(?s)```(?:python|py)?\\s*\\n(.*?)```")

// extractCode pulls the first fenced code block from a model reply; if there is
// no fence, the whole trimmed reply is treated as code.
func extractCode(response string) string {
	if m := codeFenceRe.FindStringSubmatch(response); m != nil {
		return strings.TrimRight(strings.TrimLeft(m[1], "\n"), "\n ")
	}
	return strings.TrimSpace(response)
}
```

- [ ] **Step 4: Run test to verify it passes**

From the worktree: `go test ./internal/service/benchmark/ -run TestExtractCode -v`
Expected: PASS.

- [ ] **Step 5: Build + format + commit**

```bash
go build ./... && gofmt -l internal/service/benchmark/ && go vet ./internal/service/benchmark/
git add internal/service/benchmark/codegenbench.go internal/service/benchmark/codegenbench_test.go
git commit -m "feat(benchmark): extract python code block from model replies"
```

---

## Task 3: Hardened python3 sandbox executor

**Files:** Modify `internal/service/benchmark/codegenbench.go`; Test `codegenbench_test.go`

- [ ] **Step 1: Write the failing test (gated on python3)**

Append to `codegenbench_test.go`:

```go
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
```

Add imports to `codegenbench_test.go`: `context`, `os/exec`, `testing`, `time`.

- [ ] **Step 2: Run test to verify it fails**

From the worktree: `go test ./internal/service/benchmark/ -run TestRunPython -v`
Expected: FAIL — `undefined: runPython` (or SKIP if python3 absent — if skipped, still implement Step 3).

- [ ] **Step 3: Implement `runPython` in `codegenbench.go`**

Add imports `context`, `os`, `os/exec`, `path/filepath`, `syscall`, `time` to the import block, then:

```go
// pyResult is the outcome of one sandboxed execution.
type pyResult struct {
	passed   bool
	timedOut bool
	stderr   string
}

// runPython writes the program to a temp file and runs it with `python3` under
// a hard timeout. The child runs in its own process group which is killed on
// timeout/cancel so spawned subprocesses can't outlive the run, with a minimal
// environment. NOTE: this bounds runtime but is not kernel-level isolation.
func runPython(ctx context.Context, program string, timeout time.Duration) pyResult {
	dir, err := os.MkdirTemp("", "codegen-*")
	if err != nil {
		return pyResult{stderr: "mktemp: " + err.Error()}
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "candidate.py")
	if err := os.WriteFile(file, []byte(program), 0o600); err != nil {
		return pyResult{stderr: "write: " + err.Error()}
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "python3", file)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "PYTHONDONTWRITEBYTECODE=1"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Kill the whole process group (negative pid) on ctx cancel/timeout so any
	// children the candidate spawned die too; WaitDelay bounds the reap.
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second

	var stderr strings.Builder
	cmd.Stderr = &stderr
	err = cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return pyResult{timedOut: true, stderr: "timeout after " + timeout.String()}
	}
	if err != nil {
		return pyResult{stderr: truncStderr(stderr.String())}
	}
	return pyResult{passed: true, stderr: truncStderr(stderr.String())}
}

func truncStderr(s string) string {
	const max = 2000
	if len(s) > max {
		return s[len(s)-max:]
	}
	return s
}
```

- [ ] **Step 4: Run tests to verify they pass**

From the worktree: `go test ./internal/service/benchmark/ -run TestRunPython -v`
Expected: PASS (or SKIP if python3 absent). If python3 is present, all three subtests pass; the timeout subtest must finish in ~1s, not hang.

- [ ] **Step 5: Build + format + commit**

```bash
go build ./... && gofmt -l internal/service/benchmark/ && go vet ./internal/service/benchmark/
git add internal/service/benchmark/codegenbench.go internal/service/benchmark/codegenbench_test.go
git commit -m "feat(benchmark): hardened python3 sandbox executor"
```

---

## Task 4: CodeGenProblem type, dataset, and loader

**Files:** Modify `codegenbench.go`; Create `tools/humaneval-curate/main.py`, `data/humaneval_curated.json`; Test `codegenbench_test.go`

- [ ] **Step 1: Write the failing test**

Append to `codegenbench_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

From the worktree: `go test ./internal/service/benchmark/ -run TestLoadCodeGenProblems -v`
Expected: FAIL — `undefined: loadCodeGenProblems`.

- [ ] **Step 3: Add type, embed, loader to `codegenbench.go`**

Add `embed`, `encoding/json`, `fmt` to the import block, then:

```go
//go:embed data/humaneval_curated.json
var codeGenDataset []byte

// CodeGenProblem is one curated HumanEval item.
type CodeGenProblem struct {
	TaskID            string `json:"task_id"`
	Prompt            string `json:"prompt"`             // function signature + docstring
	CanonicalSolution string `json:"canonical_solution"` // reference body (unused at runtime)
	Test              string `json:"test"`               // defines check(candidate)
	EntryPoint        string `json:"entry_point"`        // function name to pass to check
}

func loadCodeGenProblems() ([]CodeGenProblem, error) {
	var ps []CodeGenProblem
	if err := json.Unmarshal(codeGenDataset, &ps); err != nil {
		return nil, fmt.Errorf("decode codegen dataset: %w", err)
	}
	if len(ps) == 0 {
		return nil, fmt.Errorf("codegen dataset is empty")
	}
	return ps, nil
}
```

- [ ] **Step 4: Write the curation script**

Create `tools/humaneval-curate/main.py`:

```python
#!/usr/bin/env python3
"""Curate a HumanEval subset into the model-loader CodeGenProblem schema. Reproducible."""
import argparse
import json
import random
import sys

from datasets import load_dataset

SEED = 20260523
DATASET_REVISION = "main"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--count", type=int, default=64)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    ds = load_dataset("openai/openai_humaneval", split="test", revision=DATASET_REVISION)
    rows = [{
        "task_id": r["task_id"],
        "prompt": r["prompt"],
        "canonical_solution": r["canonical_solution"],
        "test": r["test"],
        "entry_point": r["entry_point"],
    } for r in ds]
    rng = random.Random(SEED)
    rng.shuffle(rows)
    out = rows[: args.count]
    out.sort(key=lambda x: x["task_id"])

    with open(args.out, "w") as f:
        json.dump(out, f, indent=2, ensure_ascii=False)
    print(f"wrote {len(out)} problems to {args.out}", file=sys.stderr)
    return 0 if len(out) >= 40 else 1


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 5: Generate the dataset**

From the worktree:

```bash
pip install --quiet datasets
python tools/humaneval-curate/main.py --count 64 --out internal/service/benchmark/data/humaneval_curated.json
```

Expected: `wrote 64 problems ...`. If pip/network is unavailable: do NOT fabricate data — commit only the script + Go code (with a placeholder `[]` dataset so the package compiles), report BLOCKED with the error and commit SHA so the controller can regenerate with network access.

- [ ] **Step 6: Run loader test + commit**

From the worktree: `go test ./internal/service/benchmark/ -run TestLoadCodeGenProblems -v` (PASS, ≥40). Then build/gofmt/vet.

```bash
git add tools/humaneval-curate/main.py internal/service/benchmark/data/humaneval_curated.json internal/service/benchmark/codegenbench.go internal/service/benchmark/codegenbench_test.go
git commit -m "feat(benchmark): embed curated HumanEval dataset and loader"
```

---

## Task 5: runCodeGenBench + codeGenHandler registration

**Files:** Modify `codegenbench.go`, `runner.go`; Test `codegenbench_test.go`

- [ ] **Step 1: Write the failing test**

Append to `codegenbench_test.go`:

```go
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
	// 2 passed, 1 failed, 1 skipped (Err set) → rate over 3 executed = 2/3.
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
```

- [ ] **Step 2: Run test to verify it fails**

From the worktree: `go test ./internal/service/benchmark/ -run 'TestCodeGenHandler|TestCodeGenFinalize' -v`
Expected: FAIL — `r.codeGenProblems undefined` / `undefined: codeGenHandler`.

- [ ] **Step 3: Add Runner field + NewRunner load**

In `runner.go` `Runner` struct, add:

```go
	codeGenProblems []CodeGenProblem // embedded HumanEval set for ModeCodeGenBench
```

In `NewRunner`, after the `loadMathProblems()` block:

```go
	codeGenProblems, err := loadCodeGenProblems()
	if err != nil {
		return nil, err
	}
```

and add `codeGenProblems: codeGenProblems,` to the returned `&Runner{...}`.

- [ ] **Step 4: Implement run path + handler in `codegenbench.go`**

Add `context` to the import block, then:

```go
// codeGenTimeout bounds each sandboxed test execution.
const codeGenTimeout = 5 * time.Second

// runCodeGenBench asks the model to implement one HumanEval function, then
// executes <code>+<test>+check(entry_point) in the sandbox. Objective Pass@1.
func (r *Runner) runCodeGenBench(ctx context.Context, base, model string, p CodeGenProblem) (ProblemResult, ProblemTranscript) {
	res := ProblemResult{ProblemID: p.TaskID, ProblemName: p.TaskID}
	tr := ProblemTranscript{ProblemID: p.TaskID, ProblemName: p.TaskID}

	reqCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()
	comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
		Model:       model,
		Temperature: 0,
		MaxTokens:   r.cfg.MaxTokens,
		Messages: []ChatMessage{
			{Role: "system", Content: "You are an expert Python programmer. Implement the requested function. Return ONLY the complete function definition in a single ```python code block."},
			{Role: "user", Content: p.Prompt},
		},
	})
	if err != nil {
		res.Err = err.Error()
		tr.Error = err.Error()
		return res, tr
	}
	res.TTFTms = comp.TTFT.Milliseconds()
	res.TotalMs = comp.Total.Milliseconds()
	res.TokensPerSecond = comp.TokensPerSecond
	res.DecodeTPS = comp.TokensPerSecond
	res.PromptProcessingTPS = comp.PromptProcessingTPS
	res.PromptTokens = comp.PromptTokens
	res.CompletionTokens = comp.CompletionTokens
	tr.ModelResponse = comp.Content

	code := extractCode(comp.Content)
	program := code + "\n\n" + p.Test + "\n\ncheck(" + p.EntryPoint + ")\n"
	exec := runPython(ctx, program, codeGenTimeout)
	res.Resolved = exec.passed
	if exec.passed {
		res.Score = 1
	}
	switch {
	case exec.passed:
		res.Detail = "passed"
	case exec.timedOut:
		res.Detail = "timed out"
	default:
		res.Detail = "failed: " + firstLine(exec.stderr)
	}
	tr.Error = exec.stderr
	return res, tr
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

type codeGenHandler struct{}

func (codeGenHandler) Mode() Mode                      { return ModeCodeGenBench }
func (codeGenHandler) Category() Category              { return CatQuality }
func (codeGenHandler) Count(r *Runner) int             { return len(r.codeGenProblems) }
func (codeGenHandler) Prepare(*Runner) (Scorer, error) { return nil, nil }

// Finalize sets CodePassRate over EXECUTED problems only (those without an Err,
// e.g. skipped-because-no-python3 are excluded). All-skipped → rate stays 0
// (omitted via omitempty).
func (codeGenHandler) Finalize(agg *Aggregate, problems []ProblemResult) {
	executed, passed := 0, 0
	for _, p := range problems {
		if p.Err != "" {
			continue
		}
		executed++
		if p.Resolved {
			passed++
		}
	}
	if executed > 0 {
		agg.CodePassRate = float64(passed) / float64(executed)
	}
}

func (codeGenHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	// Skip cleanly if python3 is unavailable: one informational result, no run.
	if _, err := lookPython(); err != nil {
		res := ProblemResult{ProblemID: "codegen-skipped", ProblemName: "CodeGenBench", Err: "python3 not on PATH; CodeGenBench skipped"}
		res.Detail = res.Err
		return []ProblemResult{res}, nil, nil
	}
	var results []ProblemResult
	var transcripts []ProblemTranscript
	for i, p := range r.codeGenProblems {
		select {
		case <-ctx.Done():
			return results, transcripts, ctx.Err()
		default:
		}
		send(progress, Progress{Index: i + 1, Total: len(r.codeGenProblems), ProblemID: p.TaskID, ProblemName: p.TaskID, Phase: "infer"})
		pr, tr := r.runCodeGenBench(ctx, base, model, p)
		results = append(results, pr)
		if r.cfg.SaveTranscripts {
			transcripts = append(transcripts, tr)
		}
	}
	return results, transcripts, nil
}

// lookPython is a seam so tests can reason about the skip path; it resolves the
// python3 binary on PATH.
var lookPython = func() (string, error) { return exec.LookPath("python3") }

func init() {
	registerHandler(codeGenHandler{})
}
```

Note: this adds `os/exec` usage (`exec.LookPath`) — it's already imported for `runPython` (`exec.CommandContext`). The local variable `exec` inside `runCodeGenBench` (`exec := runPython(...)`) shadows the `os/exec` package within that function only; rename it to `result` to avoid confusion: use `result := runPython(...)` and reference `result.passed`/`result.timedOut`/`result.stderr`. Apply that rename when writing the function.

- [ ] **Step 5: Run tests + build**

From the worktree: `go test ./internal/service/benchmark/ -v` (all pass; sandbox tests run if python3 present), `go build ./...`, `gofmt -l internal/service/benchmark/`, `go vet ./internal/service/benchmark/`.

- [ ] **Step 6: Commit**

```bash
git add internal/service/benchmark/codegenbench.go internal/service/benchmark/runner.go internal/service/benchmark/codegenbench_test.go
git commit -m "feat(benchmark): runCodeGenBench + codeGenHandler (skip if no python3)"
```

---

## Task 6: UI wiring

**Files:** Modify `internal/ui/pages/benchmark.go`

- [ ] **Step 1: Add to picker + descriptions**

Read the `benchModes` slice and description map. Add `benchmark.ModeCodeGenBench` after `benchmark.ModeMathBench`:

```go
	benchmark.ModeCodeGenBench,
```

and the description entry:

```go
	benchmark.ModeCodeGenBench: "code generation (HumanEval): sandboxed Pass@1; needs python3 on PATH",
```

- [ ] **Step 2: Build + commit**

From the worktree: `go build ./...` (clean), `gofmt -l internal/ui/pages/benchmark.go` (empty for this file).

```bash
git add internal/ui/pages/benchmark.go
git commit -m "feat(ui): expose CodeGenBench mode in the benchmark picker"
```

---

## Task 7: Phase 2c regression gate

**Files:** none (verification only)

- [ ] **Step 1: Full benchmark suite**

From the worktree: `go test ./internal/service/benchmark/ -v`
Expected: all PASS (sandbox tests run if python3 present, else SKIP).

- [ ] **Step 2: Whole project build + vet**

From the worktree: `go build ./... && go vet ./...`
Expected: clean.

- [ ] **Step 3: Confirm wiring**

From the worktree: `grep -rln "ModeCodeGenBench" internal/`
Expected: result.go, codegenbench.go, runner.go (via field/load), benchmark.go.

- [ ] **Step 4: Sandbox sanity (if python3 present)**

From the worktree: `go test ./internal/service/benchmark/ -run TestRunPython -v`
Expected: PASS — the timeout subtest completes in ~1s (process group killed, no hang).

---

## Self-Review Notes

- **Spec coverage (§5.2 CodeGenBench):** mode const + title + CodePassRate (Task 1); code extraction (Task 2); sandboxed python3 exec with 5s timeout / process-group kill / stripped env (Task 3); curated HumanEval dataset + reproducible script + loader, with entry_point for the check harness (Task 4); runCodeGenBench + handler + skip-if-absent + Finalize over executed-only (Task 5); UI (Task 6); regression incl. sandbox sanity (Task 7).
- **Skip-if-absent contract:** `Execute` returns a single Err-tagged result when python3 is missing; `Finalize` excludes Err'd results from the denominator, so an all-skipped run leaves `CodePassRate` omitted rather than reporting a false 0%.
- **Type consistency:** `CodeGenProblem`, `loadCodeGenProblems`, `codeGenProblems`, `runCodeGenBench`, `codeGenHandler`, `extractCode`, `runPython`/`pyResult`, `lookPython`, `ModeCodeGenBench`, `Aggregate.CodePassRate` consistent across tasks.
- **Security posture:** documented — runtime-bounded + process-group-killed + minimal-env, not kernel isolation. Linux-only via `syscall.SysProcAttr{Setpgid}` (project targets Linux).
- **Deferred:** MBPP variant; per-mode pass/fail detail view (Phase 4 UI).

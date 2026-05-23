package benchmark

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

//go:embed data/humaneval_curated.json
var codeGenDataset []byte

// CodeGenProblem is one curated HumanEval item.
type CodeGenProblem struct {
	TaskID            string `json:"task_id"`
	Prompt            string `json:"prompt"`
	CanonicalSolution string `json:"canonical_solution"`
	Test              string `json:"test"`
	EntryPoint        string `json:"entry_point"`
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

var codeFenceRe = regexp.MustCompile("(?s)```[A-Za-z0-9+]*[ \\t]*\\n(.*?)```")

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

// extractCode pulls the first fenced code block from a model reply; if there is
// no fence, the whole trimmed reply is treated as code.
func extractCode(response string) string {
	if m := codeFenceRe.FindStringSubmatch(response); m != nil {
		return strings.TrimRight(strings.TrimLeft(m[1], "\n"), "\n ")
	}
	return strings.TrimSpace(response)
}

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
	result := runPython(ctx, program, codeGenTimeout)
	res.Resolved = result.passed
	if result.passed {
		res.Score = 1
	}
	switch {
	case result.passed:
		res.Detail = "passed"
	case result.timedOut:
		res.Detail = "timed out"
	default:
		res.Detail = "failed: " + firstLine(result.stderr)
	}
	tr.Error = result.stderr
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

// Finalize sets CodePassRate over EXECUTED problems only (those without an Err);
// all-skipped → rate stays 0 (omitted via omitempty).
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

// lookPython is a seam resolving python3 on PATH (overridable in tests).
var lookPython = func() (string, error) { return exec.LookPath("python3") }

func init() {
	registerHandler(codeGenHandler{})
}

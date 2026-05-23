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

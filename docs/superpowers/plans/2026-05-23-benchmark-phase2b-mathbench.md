# Benchmark Engine — Phase 2b (MathBench / GSM8K) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. ALL work happens in the worktree `/home/diogo/dev/model-loader/.worktrees/benchmark-phase2` (branch `feat/benchmark-engine-expansion`) — never the main checkout.

**Goal:** Add a `math-bench` mode that measures math-reasoning accuracy (the headline quantization-degradation signal) by asking the model under test to solve curated GSM8K problems and scoring exact numeric matches.

**Architecture:** A new objective (non-judged) mode following the Phase 1 `modeHandler` registry pattern. A `mathHandler` runs each problem through the existing `Complete` client, extracts the model's final numeric answer (stripping any `<think>` block), and exact-matches it against the dataset answer after normalization. The dataset is a reproducibly-curated GSM8K subset embedded via the existing `embed.FS`. `Finalize` sets a new `Aggregate.MathAccuracy`.

**Tech Stack:** Go 1.26, standard library + `embed`. Tests: table-driven unit tests for the pure extraction/normalization functions, stub-server tests for the run path, golden-style dataset loader test. Curation via a one-shot Python `datasets` script.

**Scope note:** This plan builds MathBench on GSM8K. AIME problems share the same `{id,question,answer,difficulty}` schema and can be appended to the dataset later with no code change (AIME = a higher `difficulty` band); they are out of scope here to keep the plan executable without a fragile AIME dataset dependency.

---

## File Structure

**Created:**
- `internal/service/benchmark/mathbench.go` — `MathProblem`, dataset loader, answer extraction/normalization, `runMathBench`, `mathHandler` + `init()` registration.
- `internal/service/benchmark/mathbench_test.go` — unit + stub-server tests.
- `internal/service/benchmark/data/gsm8k_curated.json` — embedded GSM8K subset.
- `tools/math-curate/main.py` — reproducible GSM8K curation script.

**Modified:**
- `internal/service/benchmark/result.go` — `ModeMathBench` const, `Mode.Title` case, `Aggregate.MathAccuracy`.
- `internal/ui/pages/benchmark.go` — add `ModeMathBench` to `benchModes` + its picker description.

---

## Task 1: ModeMathBench constant, Title, and Aggregate field

**Files:**
- Modify: `internal/service/benchmark/result.go`
- Test: `internal/service/benchmark/mathbench_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `internal/service/benchmark/mathbench_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

From the worktree: `go test ./internal/service/benchmark/ -run TestModeMathBench_TitleAndConst -v`
Expected: FAIL — `undefined: ModeMathBench`.

- [ ] **Step 3: Add the constant, Title case, and Aggregate field**

In `result.go`, add to the `Mode` const block (after `ModeLlamaBench`):

```go
	// ModeMathBench: math-reasoning probe — curated GSM8K problems scored by
	// exact numeric match (objective, not judged). The most direct signal of
	// reasoning degradation from quantization.
	ModeMathBench Mode = "math-bench"
```

In `Mode.Title()`, add a case before `default`:

```go
	case ModeMathBench:
		return "Math reasoning (GSM8K)"
```

In `Aggregate`, add (after the prefill/decode fields):

```go
	MathAccuracy float64 `json:"mathAccuracy,omitempty"` // 0..1, exact-match rate
```

- [ ] **Step 4: Run test to verify it passes**

From the worktree: `go test ./internal/service/benchmark/ -run TestModeMathBench_TitleAndConst -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/benchmark/result.go internal/service/benchmark/mathbench_test.go
git commit -m "feat(benchmark): add ModeMathBench constant and MathAccuracy aggregate"
```

---

## Task 2: Answer extraction and numeric normalization

**Files:**
- Create: `internal/service/benchmark/mathbench.go`
- Test: `internal/service/benchmark/mathbench_test.go`

- [ ] **Step 1: Write the failing test**

Append to `mathbench_test.go`:

```go
func TestNormalizeNumber(t *testing.T) {
	cases := map[string]string{
		"42":        "42",
		"42.0":      "42",
		"$1,234":    "1234",
		" 1,000.50 ": "1000.5",
		"-7":        "-7",
		"forty-two": "forty-two", // non-numeric passes through lowercased/trimmed
	}
	for in, want := range cases {
		if got := normalizeNumber(in); got != want {
			t.Errorf("normalizeNumber(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractFinalAnswer(t *testing.T) {
	cases := map[string]string{
		"The answer is 42.":                              "42",
		"<think>3*14=42</think>#### 42":                  "42",
		"Step 1... so we get 1,234 dollars":              "1234",
		"<think>long chain</think>\nFinal answer: -15":   "-15",
		"blah 3 then 7 then the result is 21":            "21",
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
```

- [ ] **Step 2: Run test to verify it fails**

From the worktree: `go test ./internal/service/benchmark/ -run 'TestNormalizeNumber|TestExtractFinalAnswer|TestMatchAnswer' -v`
Expected: FAIL — `undefined: normalizeNumber`.

- [ ] **Step 3: Create `mathbench.go` with the pure functions**

```go
package benchmark

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	thinkRe   = regexp.MustCompile(`(?is)<think>.*?</think>`)
	hashRe    = regexp.MustCompile(`####\s*(-?[\d,]+(?:\.\d+)?)`)
	answerRe  = regexp.MustCompile(`(?i)(?:final answer|the answer is|answer:)\s*\$?(-?[\d,]+(?:\.\d+)?)`)
	numberRe  = regexp.MustCompile(`-?[\d,]+(?:\.\d+)?`)
)

// normalizeNumber canonicalizes a numeric string: strip $, commas, surrounding
// space; drop a trailing ".0"; lowercase non-numeric text so it still compares.
func normalizeNumber(s string) string {
	s = strings.TrimSpace(s)
	cleaned := strings.NewReplacer("$", "", ",", "", " ", "").Replace(s)
	if f, err := strconv.ParseFloat(cleaned, 64); err == nil {
		// Render without trailing zeros: 42.0 -> "42", 1000.5 -> "1000.5".
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strings.ToLower(s)
}

// extractFinalAnswer pulls the model's final numeric answer from a response,
// stripping any <think> chain first and preferring explicit markers (#### or
// "the answer is") over the last bare number.
func extractFinalAnswer(response string) string {
	clean := thinkRe.ReplaceAllString(response, " ")
	if m := hashRe.FindStringSubmatch(clean); m != nil {
		return normalizeNumber(m[1])
	}
	if m := answerRe.FindStringSubmatch(clean); m != nil {
		return normalizeNumber(m[1])
	}
	nums := numberRe.FindAllString(clean, -1)
	if len(nums) > 0 {
		return normalizeNumber(nums[len(nums)-1]) // last number = the conclusion
	}
	return ""
}

// matchAnswer reports whether the model response yields the expected answer
// after normalization.
func matchAnswer(expected, response string) bool {
	got := extractFinalAnswer(response)
	want := normalizeNumber(expected)
	return got != "" && got == want
}
```

- [ ] **Step 4: Run tests to verify they pass**

From the worktree: `go test ./internal/service/benchmark/ -run 'TestNormalizeNumber|TestExtractFinalAnswer|TestMatchAnswer' -v`
Expected: PASS.

- [ ] **Step 5: Build + format**

From the worktree: `go build ./... && gofmt -l internal/service/benchmark/ && go vet ./internal/service/benchmark/`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/service/benchmark/mathbench.go internal/service/benchmark/mathbench_test.go
git commit -m "feat(benchmark): math answer extraction and numeric normalization"
```

---

## Task 3: MathProblem type, dataset, and loader

**Files:**
- Modify: `internal/service/benchmark/mathbench.go`
- Create: `tools/math-curate/main.py`, `internal/service/benchmark/data/gsm8k_curated.json`
- Test: `internal/service/benchmark/mathbench_test.go`

- [ ] **Step 1: Write the failing test**

Append to `mathbench_test.go`:

```go
func TestLoadMathProblems(t *testing.T) {
	ps, err := loadMathProblems()
	if err != nil {
		t.Fatalf("loadMathProblems: %v", err)
	}
	if len(ps) < 100 {
		t.Fatalf("got %d math problems, want >= 100", len(ps))
	}
	for _, p := range ps {
		if p.ID == "" || p.Question == "" || p.Answer == "" {
			t.Fatalf("invalid problem: %+v", p)
		}
		if normalizeNumber(p.Answer) != p.Answer {
			t.Errorf("answer %q for %s is not normalized", p.Answer, p.ID)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

From the worktree: `go test ./internal/service/benchmark/ -run TestLoadMathProblems -v`
Expected: FAIL — `undefined: loadMathProblems`.

- [ ] **Step 3: Add the type and loader to `mathbench.go`**

Add the `embed` import (merge into the import block) and:

```go
//go:embed data/gsm8k_curated.json
var mathDataset []byte

// MathProblem is one curated math-reasoning item.
type MathProblem struct {
	ID         string `json:"id"`
	Question   string `json:"question"`
	Answer     string `json:"answer"`     // normalized numeric string (e.g. "42")
	Difficulty int    `json:"difficulty"` // 1..3 by reasoning-step count
}

// loadMathProblems decodes the embedded GSM8K subset.
func loadMathProblems() ([]MathProblem, error) {
	var ps []MathProblem
	if err := json.Unmarshal(mathDataset, &ps); err != nil {
		return nil, fmt.Errorf("decode math dataset: %w", err)
	}
	if len(ps) == 0 {
		return nil, fmt.Errorf("math dataset is empty")
	}
	return ps, nil
}
```

Add `"embed"`, `"encoding/json"`, and `"fmt"` to the import block (alongside the existing regexp/strconv/strings).

- [ ] **Step 4: Write the curation script**

Create `tools/math-curate/main.py`:

```python
#!/usr/bin/env python3
"""Curate a GSM8K subset into the model-loader MathProblem schema.

Reproducible (fixed SEED). Pulls openai/gsm8k (main config, test split),
extracts the gold numeric answer after '####', tags difficulty by the number of
calculator annotations (<<...>>) in the worked solution, samples ~N stratified
across difficulty bands, and emits the JSON the Go embed loader expects.

Usage:
    pip install datasets
    python tools/math-curate/main.py --count 200 \
        --out internal/service/benchmark/data/gsm8k_curated.json
"""
import argparse
import json
import random
import re
import sys
from collections import defaultdict

from datasets import load_dataset

SEED = 20260523
DATASET_REVISION = "main"  # pin to a tag/commit to freeze the selection
HASH_RE = re.compile(r"####\s*(-?[\d,]+(?:\.\d+)?)")
CALC_RE = re.compile(r"<<.*?>>")


def difficulty(solution: str) -> int:
    steps = len(CALC_RE.findall(solution))
    if steps <= 2:
        return 1
    if steps <= 4:
        return 2
    return 3


def norm(ans: str) -> str:
    a = ans.replace(",", "").replace("$", "").strip()
    try:
        f = float(a)
        return ("%f" % f).rstrip("0").rstrip(".") if "." in a else str(int(f))
    except ValueError:
        return a


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--count", type=int, default=200)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    ds = load_dataset("openai/gsm8k", "main", split="test", revision=DATASET_REVISION)
    by_diff = defaultdict(list)
    for i, row in enumerate(ds):
        m = HASH_RE.search(row["answer"])
        if not m:
            continue
        d = difficulty(row["answer"])
        by_diff[d].append({
            "id": f"gsm8k-{i}",
            "question": row["question"],
            "answer": norm(m.group(1)),
            "difficulty": d,
        })

    rng = random.Random(SEED)
    out = []
    per_band = max(1, args.count // 3)
    for d in (1, 2, 3):
        bucket = by_diff[d]
        rng.shuffle(bucket)
        out.extend(bucket[:per_band])
    # Top up to count from the remainder if any band was short.
    if len(out) < args.count:
        rest = [p for d in (1, 2, 3) for p in by_diff[d][per_band:]]
        rng.shuffle(rest)
        out.extend(rest[: args.count - len(out)])
    out = out[: args.count]

    with open(args.out, "w") as f:
        json.dump(out, f, indent=2, ensure_ascii=False)
    print(f"wrote {len(out)} problems to {args.out}", file=sys.stderr)
    return 0 if len(out) >= 100 else 1


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 5: Run the curation script**

From the worktree:

```bash
pip install --quiet datasets
python tools/math-curate/main.py --count 200 --out internal/service/benchmark/data/gsm8k_curated.json
```

Expected: stderr prints `wrote 200 problems ...`. If pip/network is unavailable, report BLOCKED with the error and the script commit (the script is the reproducible artifact) so the controller can regenerate the data in an environment with network access — do NOT fabricate problems.

- [ ] **Step 6: Run loader test**

From the worktree: `go test ./internal/service/benchmark/ -run TestLoadMathProblems -v`
Expected: PASS (≥100 problems, all with normalized answers).

- [ ] **Step 7: Commit**

```bash
git add tools/math-curate/main.py internal/service/benchmark/data/gsm8k_curated.json internal/service/benchmark/mathbench.go internal/service/benchmark/mathbench_test.go
git commit -m "feat(benchmark): embed curated GSM8K dataset and loader"
```

---

## Task 4: runMathBench + mathHandler registration

**Files:**
- Modify: `internal/service/benchmark/mathbench.go`, `internal/service/benchmark/handlers.go` (register) — OR register in mathbench.go's own `init()`.
- Test: `internal/service/benchmark/mathbench_test.go`

- [ ] **Step 1: Write the failing test**

Append to `mathbench_test.go`:

```go
func TestMathHandler_RegisteredAndCounts(t *testing.T) {
	h, ok := handlerFor(ModeMathBench)
	if !ok {
		t.Fatal("ModeMathBench not registered")
	}
	if h.Category() != CatQuality {
		t.Errorf("category = %q, want Quality", h.Category())
	}
	r := &Runner{mathProblems: make([]MathProblem, 5)}
	if h.Count(r) != 5 {
		t.Errorf("Count = %d, want 5", h.Count(r))
	}
}

func TestMathFinalize_SetsAccuracy(t *testing.T) {
	var agg Aggregate
	results := []ProblemResult{{Resolved: true}, {Resolved: false}, {Resolved: true}, {Resolved: true}}
	mathHandler{}.Finalize(&agg, results)
	if agg.MathAccuracy != 0.75 {
		t.Errorf("MathAccuracy = %v, want 0.75", agg.MathAccuracy)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

From the worktree: `go test ./internal/service/benchmark/ -run 'TestMathHandler|TestMathFinalize' -v`
Expected: FAIL — `r.mathProblems undefined` / `undefined: mathHandler`.

- [ ] **Step 3: Add a `mathProblems` field to Runner and load it in NewRunner**

In `runner.go` `Runner` struct, add:

```go
	mathProblems []MathProblem // embedded GSM8K set for ModeMathBench
```

In `NewRunner`, after the existing dataset `Load()`, add (non-fatal if the math set is absent so existing modes still work):

```go
	mathProblems, err := loadMathProblems()
	if err != nil {
		return nil, err
	}
```

and add `mathProblems: mathProblems,` to the returned `&Runner{...}` literal.

- [ ] **Step 4: Implement `runMathBench`, `mathHandler`, and registration in `mathbench.go`**

Add to `mathbench.go`:

```go
// runMathBench asks the model one math question and scores an exact numeric
// match. Objective (no grader). Temperature 0 for determinism.
func (r *Runner) runMathBench(ctx context.Context, base, model string, p MathProblem) (ProblemResult, ProblemTranscript) {
	res := ProblemResult{ProblemID: p.ID, ProblemName: truncateQuestion(p.Question)}
	tr := ProblemTranscript{ProblemID: p.ID, ProblemName: res.ProblemName}

	reqCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()
	comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
		Model:       model,
		Temperature: 0,
		MaxTokens:   r.cfg.MaxTokens,
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a careful math solver. Reason step by step, then end with 'The answer is <number>'."},
			{Role: "user", Content: p.Question},
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

	got := extractFinalAnswer(comp.Content)
	res.Resolved = got != "" && got == normalizeNumber(p.Answer)
	if res.Resolved {
		res.Score = 1
	}
	res.Detail = fmt.Sprintf("expected %s, got %q (difficulty %d)", p.Answer, got, p.Difficulty)
	return res, tr
}

func truncateQuestion(q string) string {
	q = strings.ReplaceAll(q, "\n", " ")
	if len(q) > 60 {
		return q[:57] + "..."
	}
	return q
}

type mathHandler struct{}

func (mathHandler) Mode() Mode                      { return ModeMathBench }
func (mathHandler) Category() Category              { return CatQuality }
func (mathHandler) Count(r *Runner) int             { return len(r.mathProblems) }
func (mathHandler) Prepare(*Runner) (Scorer, error) { return nil, nil }

func (mathHandler) Finalize(agg *Aggregate, problems []ProblemResult) {
	if len(problems) == 0 {
		return
	}
	solved := 0
	for _, p := range problems {
		if p.Resolved {
			solved++
		}
	}
	agg.MathAccuracy = float64(solved) / float64(len(problems))
}

func (mathHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	var results []ProblemResult
	var transcripts []ProblemTranscript
	for i, p := range r.mathProblems {
		select {
		case <-ctx.Done():
			return results, transcripts, ctx.Err()
		default:
		}
		send(progress, Progress{Index: i + 1, Total: len(r.mathProblems), ProblemID: p.ID, ProblemName: truncateQuestion(p.Question), Phase: "infer"})
		pr, tr := r.runMathBench(ctx, base, model, p)
		results = append(results, pr)
		if r.cfg.SaveTranscripts {
			transcripts = append(transcripts, tr)
		}
	}
	return results, transcripts, nil
}

func init() {
	registerHandler(mathHandler{})
}
```

Add `"context"` to the import block.

- [ ] **Step 5: Run tests + build**

From the worktree: `go test ./internal/service/benchmark/ -v` (all pass), `go build ./...`, `gofmt -l internal/service/benchmark/`, `go vet ./internal/service/benchmark/`.
Expected: clean. (Note `loadMathProblems` is now called in NewRunner, so the embedded dataset from Task 3 must be present.)

- [ ] **Step 6: Commit**

```bash
git add internal/service/benchmark/mathbench.go internal/service/benchmark/runner.go internal/service/benchmark/mathbench_test.go
git commit -m "feat(benchmark): runMathBench + mathHandler registration"
```

---

## Task 5: UI wiring

**Files:**
- Modify: `internal/ui/pages/benchmark.go`

- [ ] **Step 1: Inspect the picker wiring**

From the worktree, read `internal/ui/pages/benchmark.go` around the `benchModes` slice (≈line 34) and the mode-description map (≈line 228-233, keyed by `benchmark.Mode`).

- [ ] **Step 2: Add MathBench to the picker and descriptions**

Add `benchmark.ModeMathBench` to the `benchModes` slice (after `benchmark.ModeLlamaBench`):

```go
	benchmark.ModeMathBench,
```

Add a description entry to the mode-description map (match the existing entries' style):

```go
	benchmark.ModeMathBench: "math reasoning (GSM8K): exact numeric match, accuracy under quantization",
```

- [ ] **Step 3: Build + manual smoke check**

From the worktree: `go build ./...` (clean). Confirm the picker now lists Math reasoning (no test harness for the TUI list, but the generic views render any Mode/Aggregate, and MathAccuracy is an `omitempty` aggregate field shown in detail).

- [ ] **Step 4: Commit**

```bash
git add internal/ui/pages/benchmark.go
git commit -m "feat(ui): expose MathBench mode in the benchmark picker"
```

---

## Task 6: Phase 2b regression gate

**Files:** none (verification only)

- [ ] **Step 1: Full benchmark suite**

From the worktree: `go test ./internal/service/benchmark/ -v`
Expected: all PASS (Phase 1 + 2a + math tests).

- [ ] **Step 2: Whole project build + vet**

From the worktree: `go build ./... && go vet ./...`
Expected: clean.

- [ ] **Step 3: Confirm registration and reachability**

From the worktree: `grep -rn "ModeMathBench" internal/`
Expected: const in result.go, registration in mathbench.go, picker + description in benchmark.go. `CountForMode(ModeMathBench)` returns `len(mathProblems)` via the registry.

- [ ] **Step 4: Final commit (if cleanup needed)**

```bash
git add -A && git commit -m "chore(benchmark): phase 2b mathbench green" || echo "nothing to commit"
```

---

## Self-Review Notes

- **Spec coverage (§5.1 MathBench):** mode const + title + accuracy aggregate (Task 1); numeric exact-match with normalization + `<think>` strip + Pass@1 extraction (Task 2); curated GSM8K dataset + reproducible script + difficulty stratification (Task 3); runMathBench + handler registration + Finalize accuracy (Task 4); UI picker (Task 5); regression (Task 6).
- **Deferred (own follow-ups):** AIME problems (append to the same schema, no code change); per-mode difficulty-breakdown detail view (Phase 4 UI plan).
- **Type consistency:** `MathProblem`, `loadMathProblems`, `mathProblems` (Runner field), `runMathBench`, `mathHandler`, `extractFinalAnswer`/`normalizeNumber`/`matchAnswer`, `ModeMathBench`, `Aggregate.MathAccuracy` named consistently across tasks.
- **Pattern consistency:** `mathHandler` mirrors the Phase 1 objective handlers (`Prepare` returns nil scorer; Execute streams `infer` progress + honors ctx + SaveTranscripts), and registers via its own `init()` exactly like `handlers.go`.
- **Objective mode:** no grader used (exact match), so this ships independent of the Phase 2a grader.

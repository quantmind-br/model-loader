# MMLUBench (Phase 2e) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `mmlu-bench` benchmark mode that measures factual knowledge using a curated 100-question MMLU subset stratified across four super-categories (STEM, Humanities, Social Sciences, Other), scored by objective multiple-choice letter exact-match, with a per-category breakdown carried in each problem's detail and an aggregate `MMLUAccuracy`.

**Architecture:** Follow the established `modeHandler` registry pattern (see `mathbench.go` — MMLUBench is its closest analogue: objective, single-generation, exact-match). A new `mmluHandler` self-registers via `init()`, returns a nil `Scorer` from `Prepare` (no LLM grader), runs each question once at temperature 0, extracts the model's chosen letter, and compares it to the gold letter. The curated dataset is fetched reproducibly from the public `cais/mmlu` dataset by a Python script and embedded via `embed.FS`. The optional `lm_eval` subprocess wrapper from the design spec is intentionally **out of scope** (it is a marked "optional bonus"; `lm_eval` is not installed in this environment).

**Tech Stack:** Go 1.26, `net/http` + SSE chat client (`benchmark.Complete`), `encoding/json`, `regexp`. Dataset curation in Python 3 with the `datasets` library (already installed, v4.8.5; HF network access confirmed).

**Working directory:** All paths are relative to the worktree `/home/diogo/dev/model-loader/.worktrees/benchmark-phase2`. Run all `go`/`git`/`python3` commands from there. The branch is `feat/benchmark-engine-expansion`.

---

## Context for the implementer

The benchmark engine lives in `internal/service/benchmark/`. Reuse these existing pieces (do NOT reimplement):

- `Complete(ctx, doer httpDoer, base, apiKey string, req ChatRequest) (CompletionResult, error)` in `client.go` — streamed chat call. Pass `doer == nil` for `http.DefaultClient`. `CompletionResult` has `Content`, `TTFT`, `Total`, `TokensPerSecond`, `PromptProcessingTPS`, `PromptTokens`, `CompletionTokens`.
- `ChatRequest{Model, Messages []ChatMessage, Temperature, MaxTokens}`, `ChatMessage{Role, Content}`.
- `ProblemResult` / `ProblemTranscript` / `Aggregate` / `Mode` in `result.go`.
- `modeHandler` interface + `registerHandler` / `handlerFor` in `handler.go`. Categories: `CatQuality`, `CatSpeed`, `CatRobustness`, `CatKnowledge`.
- `send(progress, Progress{...})` progress helper (used in `mathbench.go`).
- `truncateQuestion(q string) string` (in `mathbench.go`) and `thinkRe` (package-level `<think>…</think>` strip regex, also in `mathbench.go`) — reuse both.

The closest structural template is `mathHandler`/`runMathBench` in `mathbench.go`: objective exact-match, single `Complete` at temperature 0, `Finalize` sets one accuracy field over `len(problems)`. Loaders follow `loadMathProblems`. Curation tools follow `tools/math-curate/main.py` (argparse `--count`/`--out`, fixed `SEED`, `load_dataset` with revision pin, stratified sampling).

Run the benchmark test suite with: `go test ./internal/service/benchmark/...`

---

## Task 1: Domain types — mode, title, aggregate field

**Files:**
- Modify: `internal/service/benchmark/result.go`
- Test: `internal/service/benchmark/result_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `internal/service/benchmark/result_test.go`:

```go
func TestModeMMLUBenchTitle(t *testing.T) {
	if got := ModeMMLUBench.Title(); got != "Factual knowledge (MMLU)" {
		t.Fatalf("ModeMMLUBench.Title() = %q, want %q", got, "Factual knowledge (MMLU)")
	}
	if ModeMMLUBench != "mmlu-bench" {
		t.Fatalf("ModeMMLUBench = %q, want %q", ModeMMLUBench, "mmlu-bench")
	}
}

func TestMMLUAggregateField(t *testing.T) {
	var a Aggregate
	a.MMLUAccuracy = 1
	if a.MMLUAccuracy != 1 {
		t.Fatal("MMLUAccuracy not assignable")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run 'TestModeMMLUBench|TestMMLUAggregate'`
Expected: FAIL — `undefined: ModeMMLUBench` / `a.MMLUAccuracy undefined`.

- [ ] **Step 3: Add the mode constant**

In `internal/service/benchmark/result.go`, add to the `const (...)` Mode block (after `ModeInstBench`):

```go
	// ModeMMLUBench: factual-knowledge probe — a curated MMLU subset stratified
	// across STEM/Humanities/Social Sciences/Other, scored by objective
	// multiple-choice letter exact-match (not judged). A direct signal of
	// knowledge degradation from quantization.
	ModeMMLUBench Mode = "mmlu-bench"
```

- [ ] **Step 4: Add the Title case**

In the `Title()` switch, add before `default`:

```go
	case ModeMMLUBench:
		return "Factual knowledge (MMLU)"
```

- [ ] **Step 5: Add the aggregate field**

In the `Aggregate` struct, after the `InstConsistency` field, add:

```go
	MMLUAccuracy           float64 `json:"mmluAccuracy,omitempty"`    // 0..1 multiple-choice exact-match rate
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/service/benchmark/ -run 'TestModeMMLUBench|TestMMLUAggregate'`
Expected: PASS. Then run `gofmt -w internal/service/benchmark/result.go` to keep the struct tab-alignment correct, and confirm `gofmt -l internal/service/benchmark/result.go` prints nothing.

- [ ] **Step 7: Commit**

```bash
git add internal/service/benchmark/result.go internal/service/benchmark/result_test.go
git commit -m "feat(benchmark): add ModeMMLUBench constant and MMLUAccuracy aggregate

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: MMLUProblem type + multiple-choice letter extraction

**Files:**
- Create: `internal/service/benchmark/mmlubench.go`
- Test: `internal/service/benchmark/mmlubench_test.go`

This task adds the `MMLUProblem` type and the pure helper functions only (letter extraction, prompt building). The dataset loader, runner method, and handler come in later tasks. Do NOT add a `//go:embed` directive yet — the data file does not exist until Task 3.

- [ ] **Step 1: Write the failing test**

Create `internal/service/benchmark/mmlubench_test.go`:

```go
package benchmark

import "testing"

func TestExtractMCLetter(t *testing.T) {
	cases := map[string]string{
		"B":                                "B",
		"The answer is C.":                  "C",
		"(D)":                              "D",
		"Answer: A":                         "A",
		"I think the correct option is C":   "C",
		"<think>maybe A or B</think> D":     "D",
		"the number of apples is 7":         "", // no standalone capital A-D, no answer marker
		"":                                  "",
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run 'TestExtractMCLetter|TestBuildMMLUPrompt'`
Expected: FAIL — `undefined: extractMCLetter` / `undefined: MMLUProblem`.

- [ ] **Step 3: Write the implementation**

Create `internal/service/benchmark/mmlubench.go`:

```go
package benchmark

import (
	"fmt"
	"regexp"
	"strings"
)

// MMLUProblem is one curated MMLU multiple-choice item. Answer is the correct
// option letter ("A".."D"); Category is the super-category (STEM, Humanities,
// Social Sciences, Other) used for the per-category breakdown.
type MMLUProblem struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Choices  []string `json:"choices"`
	Answer   string   `json:"answer"`
	Category string   `json:"category"`
}

var (
	// mcAnswerRe prefers an explicit marker: "answer is B", "answer: B",
	// "option (C)", "choice = D". Case-insensitive.
	mcAnswerRe = regexp.MustCompile(`(?i)\b(?:answer|option|choice)\b\s*(?:is|:|=)?\s*\(?([A-Da-d])\)?\b`)
	// mcLetterRe is the fallback: the first standalone CAPITAL A-D. Uppercase
	// only, so the English article "a" and stray lowercase letters don't match.
	mcLetterRe = regexp.MustCompile(`\b([A-D])\b`)
)

// extractMCLetter pulls the model's chosen option letter ("A".."D") from a
// reply, stripping any <think> chain first. It prefers an explicit answer
// marker, then a single-letter reply, then the first standalone capital letter.
// Returns "" when no option letter can be found.
func extractMCLetter(response string) string {
	clean := strings.TrimSpace(thinkRe.ReplaceAllString(response, " "))
	if len(clean) == 1 {
		c := strings.ToUpper(clean)
		if c >= "A" && c <= "D" {
			return c
		}
	}
	if m := mcAnswerRe.FindStringSubmatch(clean); m != nil {
		return strings.ToUpper(m[1])
	}
	if m := mcLetterRe.FindStringSubmatch(clean); m != nil {
		return m[1]
	}
	return ""
}

// buildMMLUPrompt renders the question with lettered options, one per line.
func buildMMLUPrompt(p MMLUProblem) string {
	var b strings.Builder
	b.WriteString(p.Question)
	b.WriteString("\n\n")
	for i, c := range p.Choices {
		if i >= 4 {
			break
		}
		fmt.Fprintf(&b, "%c) %s\n", 'A'+i, c)
	}
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/benchmark/ -run 'TestExtractMCLetter|TestBuildMMLUPrompt'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/benchmark/mmlubench.go internal/service/benchmark/mmlubench_test.go
git commit -m "feat(benchmark): add MMLUProblem type and multiple-choice letter extraction

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Curated dataset + reproducible script + loader

**Files:**
- Create: `tools/mmlu-curate/main.py`
- Create: `internal/service/benchmark/data/mmlu_curated.json` (generated by the script)
- Modify: `internal/service/benchmark/mmlubench.go` (add embed + loader)
- Test: `internal/service/benchmark/mmlubench_test.go` (add loader test)

The script fetches the public `cais/mmlu` dataset (config `all`, `test` split), maps each question's `subject` to one of four super-categories via the canonical MMLU mapping, stratifies a fixed-seed sample evenly across the four categories, converts the integer `answer` (0..3) to a letter ("A".."D"), and writes the curated JSON. This mirrors `tools/math-curate/main.py`.

- [ ] **Step 1: Write the curation script**

Create `tools/mmlu-curate/main.py`:

```python
#!/usr/bin/env python3
"""Curate an MMLU subset into the model-loader MMLUProblem schema. Reproducible.

Fetches cais/mmlu (config "all", split "test"), maps each subject to one of four
super-categories, stratifies a fixed-seed sample evenly across them, and converts
the integer answer (0..3) to a letter.

Reproduce:  python3 tools/mmlu-curate/main.py --count 100 \
    --out internal/service/benchmark/data/mmlu_curated.json
"""
import argparse
import json
import random
import sys
from collections import defaultdict

from datasets import load_dataset

SEED = 20260523
DATASET_REVISION = "main"

# Canonical hendrycks/test subject -> sub-topic mapping.
SUBCATEGORIES = {
    "abstract_algebra": "math", "anatomy": "health", "astronomy": "physics",
    "business_ethics": "business", "clinical_knowledge": "health",
    "college_biology": "biology", "college_chemistry": "chemistry",
    "college_computer_science": "computer science", "college_mathematics": "math",
    "college_medicine": "health", "college_physics": "physics",
    "computer_security": "computer science", "conceptual_physics": "physics",
    "econometrics": "economics", "electrical_engineering": "engineering",
    "elementary_mathematics": "math", "formal_logic": "philosophy",
    "global_facts": "other", "high_school_biology": "biology",
    "high_school_chemistry": "chemistry",
    "high_school_computer_science": "computer science",
    "high_school_european_history": "history", "high_school_geography": "geography",
    "high_school_government_and_politics": "politics",
    "high_school_macroeconomics": "economics", "high_school_mathematics": "math",
    "high_school_microeconomics": "economics", "high_school_physics": "physics",
    "high_school_psychology": "psychology", "high_school_statistics": "math",
    "high_school_us_history": "history", "high_school_world_history": "history",
    "human_aging": "health", "human_sexuality": "culture",
    "international_law": "law", "jurisprudence": "law",
    "logical_fallacies": "philosophy", "machine_learning": "computer science",
    "management": "business", "marketing": "business",
    "medical_genetics": "health", "miscellaneous": "other",
    "moral_disputes": "philosophy", "moral_scenarios": "philosophy",
    "nutrition": "health", "philosophy": "philosophy", "prehistory": "history",
    "professional_accounting": "other", "professional_law": "law",
    "professional_medicine": "health", "professional_psychology": "psychology",
    "public_relations": "politics", "security_studies": "politics",
    "sociology": "culture", "us_foreign_policy": "politics",
    "virology": "health", "world_religions": "philosophy",
}

# Sub-topic -> clean super-category label.
SUPERCATEGORY = {
    "physics": "STEM", "chemistry": "STEM", "biology": "STEM",
    "computer science": "STEM", "math": "STEM", "engineering": "STEM",
    "history": "Humanities", "philosophy": "Humanities", "law": "Humanities",
    "politics": "Social Sciences", "culture": "Social Sciences",
    "economics": "Social Sciences", "geography": "Social Sciences",
    "psychology": "Social Sciences",
    "other": "Other", "business": "Other", "health": "Other",
}
CATEGORIES = ["STEM", "Humanities", "Social Sciences", "Other"]


def category_of(subject: str) -> str:
    return SUPERCATEGORY[SUBCATEGORIES[subject]]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--count", type=int, default=100)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    ds = load_dataset("cais/mmlu", "all", split="test", revision=DATASET_REVISION)
    by_cat = defaultdict(list)
    for i, row in enumerate(ds):
        choices = row["choices"]
        ans = row["answer"]
        if len(choices) != 4 or not (0 <= ans <= 3):
            continue
        cat = category_of(row["subject"])
        by_cat[cat].append({
            "id": f"mmlu-{i}",
            "question": row["question"],
            "choices": [str(c) for c in choices],
            "answer": "ABCD"[ans],
            "category": cat,
        })

    rng = random.Random(SEED)
    per_cat = max(1, args.count // len(CATEGORIES))
    out = []
    for cat in CATEGORIES:
        bucket = by_cat[cat]
        rng.shuffle(bucket)
        out.extend(bucket[:per_cat])
    if len(out) < args.count:
        rest = [p for cat in CATEGORIES for p in by_cat[cat][per_cat:]]
        rng.shuffle(rest)
        out.extend(rest[: args.count - len(out)])
    out = out[: args.count]

    with open(args.out, "w") as f:
        json.dump(out, f, indent=2, ensure_ascii=False)
    print(f"wrote {len(out)} questions to {args.out}", file=sys.stderr)
    return 0 if len(out) >= 100 else 1


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 2: Generate the dataset**

Run (this downloads the MMLU test split from HuggingFace; allow up to a few minutes on first fetch):

```bash
python3 tools/mmlu-curate/main.py --count 100 --out internal/service/benchmark/data/mmlu_curated.json
```

Verify 100 items and the category spread:

```bash
python3 -c "import json,collections; d=json.load(open('internal/service/benchmark/data/mmlu_curated.json')); print(len(d)); print(collections.Counter(x['category'] for x in d)); print(collections.Counter(x['answer'] for x in d))"
```

Expected: `100`; the four categories each present (≈25 each); answers spread across A/B/C/D.

If the HuggingFace fetch fails (no network / dataset unavailable), STOP and report BLOCKED — do not hand-fabricate the data.

- [ ] **Step 3: Write the failing loader test**

Append to `internal/service/benchmark/mmlubench_test.go`:

```go
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
```

Add the `strings` import to the test file's import block (currently only `testing`):

```go
import (
	"strings"
	"testing"
)
```

- [ ] **Step 4: Add the embed + loader**

At the top of `internal/service/benchmark/mmlubench.go`, update the import block to add `_ "embed"` and `encoding/json`:

```go
import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)
```

After the import block, add:

```go
//go:embed data/mmlu_curated.json
var mmluDataset []byte

// loadMMLUProblems decodes the embedded curated MMLU subset.
func loadMMLUProblems() ([]MMLUProblem, error) {
	var ps []MMLUProblem
	if err := json.Unmarshal(mmluDataset, &ps); err != nil {
		return nil, fmt.Errorf("decode mmlu dataset: %w", err)
	}
	if len(ps) == 0 {
		return nil, fmt.Errorf("mmlu dataset is empty")
	}
	return ps, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/service/benchmark/ -run 'TestLoadMMLUProblems|TestExtractMCLetter|TestBuildMMLUPrompt'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add tools/mmlu-curate/main.py internal/service/benchmark/data/mmlu_curated.json internal/service/benchmark/mmlubench.go internal/service/benchmark/mmlubench_test.go
git commit -m "feat(benchmark): embed curated MMLU subset and loader

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Runner wiring — load the MMLU set

**Files:**
- Modify: `internal/service/benchmark/runner.go`
- Test: `internal/service/benchmark/runner_mmlubench_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `internal/service/benchmark/runner_mmlubench_test.go`:

```go
package benchmark

import "testing"

func TestNewRunnerLoadsMMLUSet(t *testing.T) {
	r, err := NewRunner(nil, nil, nil, nil, Config{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if len(r.mmluProblems) == 0 {
		t.Fatal("expected NewRunner to load the embedded MMLU set")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run TestNewRunnerLoadsMMLUSet`
Expected: FAIL — `r.mmluProblems undefined`.

- [ ] **Step 3: Add the Runner field**

In `internal/service/benchmark/runner.go`, in the `Runner` struct, after the `instProblems` field, add:

```go
	mmluProblems    []MMLUProblem        // embedded MMLU subset for ModeMMLUBench
```

- [ ] **Step 4: Load the set in NewRunner**

In `NewRunner`, after the `instProblems, err := loadInstructionProblems()` block (the one ending with its `if err != nil { return nil, err }`), add:

```go
	mmluProblems, err := loadMMLUProblems()
	if err != nil {
		return nil, err
	}
```

Then add `mmluProblems: mmluProblems,` to the returned `&Runner{...}` literal (alongside `instProblems: instProblems,`).

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/service/benchmark/ -run TestNewRunnerLoadsMMLUSet`
Expected: PASS. Run `gofmt -l internal/service/benchmark/runner.go` (must print nothing).

- [ ] **Step 6: Commit**

```bash
git add internal/service/benchmark/runner.go internal/service/benchmark/runner_mmlubench_test.go
git commit -m "feat(benchmark): load embedded MMLU set in NewRunner

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: runMMLUBench + mmluHandler

**Files:**
- Modify: `internal/service/benchmark/mmlubench.go`
- Test: `internal/service/benchmark/mmlubench_test.go` (add handler/runner tests)

This wires the per-question runner (single deterministic generation, letter exact-match), the handler, and `Finalize` (sets `MMLUAccuracy` over all problems). The per-category breakdown is carried in each `ProblemResult.Detail` (`category=<X> expected <A> got <B>`), keeping the store/aggregate shape unchanged.

- [ ] **Step 1: Write the failing tests**

Append to `internal/service/benchmark/mmlubench_test.go` (update the import block to add `context`, `net/http`, `net/http/httptest`, `time`):

```go
import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)
```

```go
func TestMMLUHandlerRegistered(t *testing.T) {
	h, ok := handlerFor(ModeMMLUBench)
	if !ok {
		t.Fatal("ModeMMLUBench handler not registered")
	}
	if h.Category() != CatKnowledge {
		t.Fatalf("category = %q, want %q", h.Category(), CatKnowledge)
	}
}

func TestMMLUFinalize(t *testing.T) {
	problems := []ProblemResult{
		{Resolved: true},
		{Resolved: false},
		{Resolved: true},
		{Resolved: true},
	}
	var agg Aggregate
	mmluHandler{}.Finalize(&agg, problems)
	if agg.MMLUAccuracy != 0.75 {
		t.Fatalf("MMLUAccuracy = %v, want 0.75", agg.MMLUAccuracy)
	}
}

func TestRunMMLUBench(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"The answer is B\"}}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	r := &Runner{cfg: Config{MaxTokens: 16, Timeout: 5 * time.Second}}
	p := MMLUProblem{ID: "mmlu-1", Question: "2+2=?", Choices: []string{"3", "4", "5", "6"}, Answer: "B", Category: "STEM"}
	res, _ := r.runMMLUBench(context.Background(), srv.URL, "m", p)
	if !res.Resolved {
		t.Fatalf("expected correct answer B, detail %q", res.Detail)
	}
	if res.Score != 1 {
		t.Fatalf("Score = %v, want 1", res.Score)
	}
	if !strings.Contains(res.Detail, "STEM") {
		t.Fatalf("Detail %q should mention category", res.Detail)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/benchmark/ -run 'TestMMLUHandler|TestMMLUFinalize|TestRunMMLUBench'`
Expected: FAIL — `undefined: mmluHandler` / `r.runMMLUBench undefined`.

- [ ] **Step 3: Write the implementation**

Append to `internal/service/benchmark/mmlubench.go`. First add `context` to the import block:

```go
import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)
```

Then append:

```go
// runMMLUBench asks the model one multiple-choice question and scores an
// objective letter exact-match. Temperature 0 for determinism (no grader).
func (r *Runner) runMMLUBench(ctx context.Context, base, model string, p MMLUProblem) (ProblemResult, ProblemTranscript) {
	name := "[" + p.Category + "] " + truncateQuestion(p.Question)
	res := ProblemResult{ProblemID: p.ID, ProblemName: name}
	tr := ProblemTranscript{ProblemID: p.ID, ProblemName: name}

	reqCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()
	comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
		Model:       model,
		Temperature: 0,
		MaxTokens:   r.cfg.MaxTokens,
		Messages: []ChatMessage{
			{Role: "system", Content: "You are answering a multiple-choice question. Respond with ONLY the letter (A, B, C, or D) of the correct answer."},
			{Role: "user", Content: buildMMLUPrompt(p)},
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

	got := extractMCLetter(comp.Content)
	res.Resolved = got != "" && got == p.Answer
	if res.Resolved {
		res.Score = 1
	}
	res.Detail = fmt.Sprintf("category=%s expected %s got %q", p.Category, p.Answer, got)
	return res, tr
}

type mmluHandler struct{}

func (mmluHandler) Mode() Mode                      { return ModeMMLUBench }
func (mmluHandler) Category() Category              { return CatKnowledge }
func (mmluHandler) Count(r *Runner) int             { return len(r.mmluProblems) }
func (mmluHandler) Prepare(*Runner) (Scorer, error) { return nil, nil }

// Finalize sets MMLUAccuracy over all answered problems. Per-category accuracy
// is derivable from each ProblemResult.Detail, so no extra aggregate field is
// needed.
func (mmluHandler) Finalize(agg *Aggregate, problems []ProblemResult) {
	if len(problems) == 0 {
		return
	}
	solved := 0
	for _, p := range problems {
		if p.Resolved {
			solved++
		}
	}
	agg.MMLUAccuracy = float64(solved) / float64(len(problems))
}

func (mmluHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	var results []ProblemResult
	var transcripts []ProblemTranscript
	for i, p := range r.mmluProblems {
		select {
		case <-ctx.Done():
			return results, transcripts, ctx.Err()
		default:
		}
		send(progress, Progress{Index: i + 1, Total: len(r.mmluProblems), ProblemID: p.ID, ProblemName: p.Category, Phase: "infer"})
		pr, tr := r.runMMLUBench(ctx, base, model, p)
		results = append(results, pr)
		if r.cfg.SaveTranscripts {
			transcripts = append(transcripts, tr)
		}
	}
	return results, transcripts, nil
}

func init() {
	registerHandler(mmluHandler{})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/service/benchmark/ -run 'TestMMLUHandler|TestMMLUFinalize|TestRunMMLUBench'`
Expected: PASS.

- [ ] **Step 5: Run the full benchmark suite**

Run: `go test ./internal/service/benchmark/...`
Expected: PASS (no regressions).

- [ ] **Step 6: Commit**

```bash
git add internal/service/benchmark/mmlubench.go internal/service/benchmark/mmlubench_test.go
git commit -m "feat(benchmark): runMMLUBench + mmluHandler

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Expose MMLUBench in the UI picker + final verification

**Files:**
- Modify: `internal/ui/pages/benchmark.go`
- Test: `internal/ui/pages/benchmark_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/pages/benchmark_test.go` (it already exists from Phase 2d, `package pages`, importing `github.com/quantmind-br/model-loader/internal/service/benchmark`):

```go
func TestBenchModesIncludeMMLUBench(t *testing.T) {
	found := false
	for _, m := range benchModes {
		if m == benchmark.ModeMMLUBench {
			found = true
		}
	}
	if !found {
		t.Fatal("benchModes must include ModeMMLUBench")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/pages/ -run TestBenchModesIncludeMMLUBench`
Expected: FAIL — MMLU mode not in `benchModes`.

- [ ] **Step 3: Add the mode to the picker order**

In `internal/ui/pages/benchmark.go`, in the `benchModes` slice, add after `benchmark.ModeInstBench,`:

```go
	benchmark.ModeMMLUBench,
```

- [ ] **Step 4: Add the picker description**

In `viewModePick`, in the `descs` map, add an entry:

```go
		benchmark.ModeMMLUBench:    "factual knowledge (MMLU): multiple-choice exact-match across STEM/humanities/social/other",
```

Then run `gofmt -w internal/ui/pages/benchmark.go` so the map values stay aligned.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/pages/ -run TestBenchModesIncludeMMLUBench`
Expected: PASS.

- [ ] **Step 6: Final verification — build, vet, gofmt, full suite**

```bash
go build ./...
go vet ./internal/service/benchmark/... ./internal/ui/pages/...
gofmt -l internal/service/benchmark/ internal/ui/pages/benchmark.go internal/ui/pages/benchmark_test.go
go test ./internal/service/benchmark/... ./internal/ui/pages/...
```

Expected: build clean; vet clean; `gofmt -l` (on the listed files) prints nothing; tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/pages/benchmark.go internal/ui/pages/benchmark_test.go
git commit -m "feat(ui): expose MMLUBench mode in the benchmark picker

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review (completed during planning)

**Spec coverage (§5.4 MMLUBench):**
- 100-question subset across STEM/humanities/social/other → Task 3 (script stratifies 25/category).
- Schema `{question, choices, answer}` (+ id, category for the breakdown) → `MMLUProblem`, Task 2.
- Letter exact-match → `extractMCLetter` + `runMMLUBench`, Tasks 2/5.
- Per-category breakdown + aggregate → category in `ProblemResult.Detail`; `MMLUAccuracy` aggregate (Task 5 + field in Task 1).
- Reproducible curation script → `tools/mmlu-curate/main.py`, Task 3.
- Embedded via embed.FS → `//go:embed`, Task 3.
- Optional `lm_eval` subprocess wrapper → **deferred / out of scope** (marked optional in the spec; `lm_eval` not installed).

**Placeholder scan:** No TBD/TODO; every code step has complete code.

**Type consistency:** `MMLUProblem` fields (`ID`/`Question`/`Choices`/`Answer`/`Category`) consistent across Tasks 2/3/5. `extractMCLetter(string) string`, `buildMMLUPrompt(MMLUProblem) string`, `runMMLUBench(ctx, base, model, p) (ProblemResult, ProblemTranscript)` consistent. `ModeMMLUBench`, `MMLUAccuracy`, `mmluProblems` consistent across tasks. Handler method set matches the `modeHandler` interface.

**Note:** `thinkRe` and `truncateQuestion` are reused from `mathbench.go` (package-level); do not redefine.

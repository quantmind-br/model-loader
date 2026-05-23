# Benchmark Engine — Phase 2a (Grader Foundation) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a reusable `grader` abstraction that scores free-form model text against a rubric — with an external-judge implementation and a zero-config self-judge fallback (the model under test grading itself) — and add the `maintainability` criterion to the existing SWE-bench judge.

**Architecture:** A new `grader` interface in the benchmark package with one LLM-backed implementation (`llmGrader`) that targets either a configured external judge endpoint or, when none is configured, the model-under-test's own server (self-judge, flagged as such). A `Runner.graderFor` factory picks the right one. The grader reuses the existing `Complete` streaming client and a structured-JSON rubric prompt, mirroring `judgeScorer`. This unblocks the semantic modes (Ragas, multi-doc coherence) in later plans without forcing judge configuration.

**Tech Stack:** Go 1.26, standard library. Tests use `httptest` stub servers, following the existing `client_test.go` / `judge_test.go` patterns.

**Scope note:** This is the first of several Phase 2 plans. The four new modes (Math, CodeGen, Instruction, MMLU) each get their own plan and consume this grader. The embedding-based `similarityGrader` is intentionally deferred to the InstructionBench plan, where it is the only consumer.

---

## File Structure

**Created:**
- `internal/service/benchmark/grader.go` — `grader` interface, `gradeRequest`/`gradeResult`, `llmGrader`, JSON parsing.
- `internal/service/benchmark/grader_test.go` — stub-server tests for external + self grading and error handling.

**Modified:**
- `internal/service/benchmark/runner.go` — `Config.EmbeddingsBaseURL` (reserved for later) + `graderFor` factory; `Runner` keeps the judge config it already has.
- `internal/service/benchmark/scorer.go` — add `maintainability` to `judgeSystem`, rebalancing weights to sum 1.0.
- `internal/config/config.go` — no new required fields (judge config already exists); add the optional `benchmark.embeddings.base_url` default reserved for the similarity grader.
- `cmd/model-loader/main.go` — wire `EmbeddingsBaseURL`.

---

## Task 1: Grader interface and value types

**Files:**
- Create: `internal/service/benchmark/grader.go`
- Test: `internal/service/benchmark/grader_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/service/benchmark/grader_test.go`:

```go
package benchmark

import "testing"

func TestGradeResult_Defaults(t *testing.T) {
	var r gradeResult
	if r.Score != 0 || r.Pass {
		t.Fatalf("zero gradeResult should be Score 0, Pass false, got %+v", r)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run TestGradeResult_Defaults -v`
Expected: FAIL — `undefined: gradeResult`.

- [ ] **Step 3: Create `grader.go` with the types and interface**

```go
package benchmark

import "context"

// gradeRequest is the input to a grader: a single criterion plus the material
// needed to judge an answer against it.
type gradeRequest struct {
	Criterion string // e.g. "faithfulness", "answer relevancy", "coherence"
	Guidance  string // one-line description of what a passing answer looks like
	Question  string // the prompt/question posed to the model under test (optional)
	Context   string // reference material (docs, golden patch, expected facts)
	Answer    string // the model-under-test answer being graded
}

// gradeResult is a grader's verdict for one criterion.
type gradeResult struct {
	Score    float64 // 0..1
	Pass     bool
	Detail   string
	Raw      string // raw grader reply, for transcripts
	JudgedBy string // "external" or "self"
}

// grader scores free-form model output against a single criterion (0..1).
type grader interface {
	Grade(ctx context.Context, req gradeRequest) (gradeResult, error)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/benchmark/ -run TestGradeResult_Defaults -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/benchmark/grader.go internal/service/benchmark/grader_test.go
git commit -m "feat(benchmark): add grader interface and value types"
```

---

## Task 2: llmGrader (external + self judge)

**Files:**
- Modify: `internal/service/benchmark/grader.go`
- Test: `internal/service/benchmark/grader_test.go`

- [ ] **Step 1: Write the failing test**

Append to `grader_test.go`:

```go
func TestLLMGrader_ParsesScoreAndFlagsSource(t *testing.T) {
	// Stub OpenAI-compatible server returning a streamed JSON verdict.
	reply := `{"score":0.8,"pass":true,"rationale":"answer is grounded in the context"}`
	body := "data: {\"choices\":[{\"delta\":{\"content\":" + jsonQuote(reply) + "}}]}\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":20}}\n" +
		"data: [DONE]\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	g := llmGrader{base: srv.URL, model: "m", maxTok: 256, judgedBy: "self"}
	got, err := g.Grade(context.Background(), gradeRequest{
		Criterion: "faithfulness", Guidance: "answer uses only the context",
		Question: "q", Context: "ctx", Answer: "a",
	})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if got.Score != 0.8 || !got.Pass {
		t.Errorf("got Score=%v Pass=%v, want 0.8/true", got.Score, got.Pass)
	}
	if got.JudgedBy != "self" {
		t.Errorf("JudgedBy = %q, want self", got.JudgedBy)
	}
	if got.Raw == "" {
		t.Error("Raw should carry the grader reply for transcripts")
	}
}

func TestLLMGrader_ClampsAndDefaults(t *testing.T) {
	reply := `{"score":1.7,"pass":false,"rationale":"x"}`
	body := "data: {\"choices\":[{\"delta\":{\"content\":" + jsonQuote(reply) + "}}]}\n" +
		"data: [DONE]\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	g := llmGrader{base: srv.URL, model: "m", maxTok: 256, judgedBy: "external"}
	got, err := g.Grade(context.Background(), gradeRequest{Criterion: "c", Answer: "a"})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if got.Score != 1.0 {
		t.Errorf("Score = %v, want clamped to 1.0", got.Score)
	}
}
```

Add a small helper at the bottom of `grader_test.go` (used by the tests above):

```go
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
```

Ensure `grader_test.go` imports: `context`, `encoding/json`, `io`, `net/http`, `net/http/httptest`, `testing`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run TestLLMGrader -v`
Expected: FAIL — `undefined: llmGrader`.

- [ ] **Step 3: Implement `llmGrader` in `grader.go`**

Add to `grader.go`:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// graderSystem instructs the grader to score ONE criterion and emit strict JSON.
const graderSystem = `You are a strict evaluator. You are given a CRITERION, optional QUESTION and ` +
	`reference CONTEXT, and a candidate ANSWER from another model. Score how well the answer satisfies ` +
	`the criterion, from 0.0 (fails completely) to 1.0 (fully satisfies). Judge only the stated criterion.

Reply with ONLY a JSON object, no prose:
{"score":<0..1>,"pass":<bool>,"rationale":"<=160 chars"}
pass must be true only when the answer clearly satisfies the criterion.`

// llmGrader grades via an OpenAI-compatible chat endpoint. With base/model/apiKey
// pointing at an external judge it is the gold standard; pointed at the
// model-under-test's own server it is the zero-config self-judge (judgedBy=self).
type llmGrader struct {
	doer     httpDoer
	base     string
	apiKey   string
	model    string
	maxTok   int
	judgedBy string // "external" or "self"
}

func (g llmGrader) Grade(ctx context.Context, req gradeRequest) (gradeResult, error) {
	user := buildGraderUser(req)
	comp, err := Complete(ctx, g.doer, g.base, g.apiKey, ChatRequest{
		Model:       g.model,
		Temperature: 0,
		MaxTokens:   g.maxTok,
		Messages: []ChatMessage{
			{Role: "system", Content: graderSystem},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return gradeResult{JudgedBy: g.judgedBy}, fmt.Errorf("grader request: %w", err)
	}
	v, err := parseGraderVerdict(comp.Content)
	if err != nil {
		return gradeResult{Raw: comp.Content, JudgedBy: g.judgedBy}, fmt.Errorf("parse grader verdict: %w", err)
	}
	detail := fmt.Sprintf("%s=%.2f (%s)", req.Criterion, v.score, g.judgedBy)
	if v.rationale != "" {
		detail += " — " + v.rationale
	}
	return gradeResult{Score: v.score, Pass: v.pass, Detail: detail, Raw: comp.Content, JudgedBy: g.judgedBy}, nil
}

func buildGraderUser(req gradeRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Criterion\n%s", req.Criterion)
	if req.Guidance != "" {
		fmt.Fprintf(&b, " — %s", req.Guidance)
	}
	b.WriteString("\n\n")
	if req.Question != "" {
		fmt.Fprintf(&b, "# Question\n%s\n\n", req.Question)
	}
	if req.Context != "" {
		fmt.Fprintf(&b, "# Context\n%s\n\n", req.Context)
	}
	fmt.Fprintf(&b, "# Answer\n%s\n", req.Answer)
	return b.String()
}

type graderVerdict struct {
	score     float64
	pass      bool
	rationale string
}

func parseGraderVerdict(content string) (graderVerdict, error) {
	raw := content
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			raw = raw[start : end+1]
		}
	}
	var v struct {
		Score     float64 `json:"score"`
		Pass      bool    `json:"pass"`
		Rationale string  `json:"rationale"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return graderVerdict{}, err
	}
	if v.Score < 0 {
		v.Score = 0
	}
	if v.Score > 1 {
		v.Score = 1
	}
	return graderVerdict{score: v.Score, pass: v.Pass, rationale: v.Rationale}, nil
}
```

Note: `grader.go` already has `import "context"` from Task 1 — merge the imports into one block (context, encoding/json, fmt, strings). Remove the standalone `import "context"` line.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/service/benchmark/ -run 'TestLLMGrader|TestGradeResult' -v`
Expected: PASS.

- [ ] **Step 5: Build + format**

Run: `go build ./... && gofmt -l internal/service/benchmark/ && go vet ./internal/service/benchmark/`
Expected: clean (no output).

- [ ] **Step 6: Commit**

```bash
git add internal/service/benchmark/grader.go internal/service/benchmark/grader_test.go
git commit -m "feat(benchmark): add LLM grader (external + self judge)"
```

---

## Task 3: graderFor factory + config/main wiring

**Files:**
- Modify: `internal/service/benchmark/runner.go` (`Config`, `Runner`, `NewRunner`, add `graderFor`)
- Modify: `internal/config/config.go`
- Modify: `cmd/model-loader/main.go`
- Test: `internal/service/benchmark/grader_test.go`

- [ ] **Step 1: Write the failing test**

Append to `grader_test.go`:

```go
func TestGraderFor_PrefersExternalElseSelf(t *testing.T) {
	// External configured → judgedBy external, targets judge base/model.
	rExt := &Runner{cfg: Config{Judge: JudgeEndpoint{BaseURL: "http://judge:9", Model: "j"}, MaxTokens: 100}}
	gExt := rExt.graderFor("http://under-test:1", "ut").(llmGrader)
	if gExt.judgedBy != "external" || gExt.base != "http://judge:9" || gExt.model != "j" {
		t.Errorf("external grader = %+v", gExt)
	}
	// No external judge → self, targets the model-under-test base/model.
	rSelf := &Runner{cfg: Config{MaxTokens: 100}}
	gSelf := rSelf.graderFor("http://under-test:1", "ut").(llmGrader)
	if gSelf.judgedBy != "self" || gSelf.base != "http://under-test:1" || gSelf.model != "ut" {
		t.Errorf("self grader = %+v", gSelf)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run TestGraderFor -v`
Expected: FAIL — `r.graderFor undefined`.

- [ ] **Step 3: Add `EmbeddingsBaseURL` to Config and the `graderFor` factory**

In `runner.go` `Config`, after `Judge JudgeEndpoint`:

```go
	// EmbeddingsBaseURL optionally overrides where similarity graders fetch
	// embeddings. Empty → reuse the model-under-test server. Reserved for the
	// instruction-consistency mode (added in a later plan).
	EmbeddingsBaseURL string
```

Add the factory method (place near `newScorer`):

```go
// graderFor returns the grader used for semantic scoring. If an external judge
// is configured it is the gold standard; otherwise the model under test grades
// itself (zero-config, flagged judgedBy=self). base/model identify the
// model-under-test server for the self-judge path.
func (r *Runner) graderFor(base, model string) grader {
	maxTok := r.cfg.MaxTokens
	if j := r.cfg.Judge; j.BaseURL != "" && j.Model != "" {
		return llmGrader{base: j.BaseURL, apiKey: j.APIKey, model: j.Model, maxTok: maxTok, judgedBy: "external"}
	}
	return llmGrader{base: base, model: model, maxTok: maxTok, judgedBy: "self"}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/benchmark/ -run TestGraderFor -v`
Expected: PASS.

- [ ] **Step 5: Wire config + main (reserved embeddings override)**

In `internal/config/config.go`, add a default near the other benchmark defaults:

```go
	v.SetDefault("benchmark.embeddings.base_url", "")
```

Add to the benchmark config struct an embeddings sub-config (match the existing nested-struct style, e.g. alongside `Judge`/`LlamaBench`):

```go
	Embeddings EmbeddingsConfig `mapstructure:"embeddings"`
```

and define:

```go
// EmbeddingsConfig optionally overrides where similarity graders fetch
// embeddings; empty reuses the model-under-test server.
type EmbeddingsConfig struct {
	BaseURL string `mapstructure:"base_url"`
}
```

In `cmd/model-loader/main.go`, in the `benchmark.Config{...}` literal, add:

```go
		EmbeddingsBaseURL: svc.Cfg.Benchmark.Embeddings.BaseURL,
```

- [ ] **Step 6: Build, test, format**

Run: `go build ./... && go test ./internal/service/benchmark/ -v && gofmt -l internal/service/benchmark/ internal/config/ cmd/ && go vet ./...`
Expected: all pass / clean.

- [ ] **Step 7: Commit**

```bash
git add internal/service/benchmark/runner.go internal/config/config.go cmd/model-loader/main.go internal/service/benchmark/grader_test.go
git commit -m "feat(benchmark): graderFor factory + embeddings config (reserved)"
```

---

## Task 4: Add the maintainability criterion to the SWE-bench judge

**Files:**
- Modify: `internal/service/benchmark/scorer.go` (`judgeSystem`)
- Test: `internal/service/benchmark/judge_test.go` (extend, if it asserts rubric content) — otherwise a new small test.

- [ ] **Step 1: Write the failing test**

Append to `internal/service/benchmark/judge_test.go`:

```go
func TestJudgeSystem_IncludesMaintainability(t *testing.T) {
	if !strings.Contains(judgeSystem, "maintainability") {
		t.Error("judge rubric should include the maintainability criterion")
	}
	// Weights must still sum to 1.0: localization .30 + correctness .45 +
	// completeness .15 + maintainability .10.
	for _, w := range []string{"0.30", "0.45", "0.15", "0.10"} {
		if !strings.Contains(judgeSystem, w) {
			t.Errorf("judge rubric missing weight %s", w)
		}
	}
}
```

Ensure `judge_test.go` imports `strings` (add if missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run TestJudgeSystem_IncludesMaintainability -v`
Expected: FAIL — current rubric has no `maintainability` and uses weights 0.30/0.50/0.20.

- [ ] **Step 3: Rebalance the rubric**

In `scorer.go`, replace the criteria block inside `judgeSystem` (the three `- localization/correctness/completeness` lines and the JSON template line) with:

```go
Score these weighted criteria, each 0..1:
- localization (0.30): does it target the correct file(s) and function(s) as the reference?
- correctness (0.45): would the change actually fix the reported bug, like the reference does?
- completeness (0.15): does it cover the cases the reference covers, without breaking others?
- maintainability (0.10): does it avoid new dependencies, dead code, or changes likely to break existing tests?

Reply with ONLY a JSON object, no prose:
{"localization":<0..1>,"correctness":<0..1>,"completeness":<0..1>,"maintainability":<0..1>,"score":<weighted 0..1>,"resolved":<bool>,"rationale":"<=160 chars"}
resolved must be true only when the candidate is functionally equivalent to the reference fix.
```

Note: `parseJudgeVerdict` only reads `score`/`resolved`/`rationale`, so no parser change is needed — the judge LLM computes the weighted `score` itself.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/service/benchmark/ -run 'TestJudge' -v`
Expected: PASS (the new rubric test plus the existing judge tests, which parse `score`/`resolved` and are unaffected by the added criterion).

- [ ] **Step 5: Commit**

```bash
git add internal/service/benchmark/scorer.go internal/service/benchmark/judge_test.go
git commit -m "feat(benchmark): add maintainability criterion to SWE-bench judge"
```

---

## Task 5: Phase 2a regression gate

**Files:** none (verification only)

- [ ] **Step 1: Full benchmark suite**

Run: `go test ./internal/service/benchmark/ -v`
Expected: all PASS (existing + new grader/judge tests).

- [ ] **Step 2: Whole project build + vet**

Run: `go build ./... && go vet ./...`
Expected: clean. (The pre-existing `backendschema TestCuratedLlama_*` failures are unrelated env-dependent issues, not introduced here.)

- [ ] **Step 3: Confirm grader is reachable but not yet over-wired**

Run: `grep -rn "graderFor\|llmGrader" internal/service/benchmark/`
Expected: `graderFor` defined on Runner and tested; `llmGrader` defined and tested. No mode calls it yet (modes arrive in later plans) — that is expected.

- [ ] **Step 4: Final commit (if cleanup needed)**

```bash
git add -A && git commit -m "chore(benchmark): phase 2a grader foundation green" || echo "nothing to commit"
```

---

## Self-Review Notes

- **Spec coverage (the grader slice of Phase 2 §4.2 + judge maintainability §6.3):** grader interface + types (Task 1), llmGrader external/self (Task 2), graderFor factory + config (Task 3), judge maintainability rebalance summing to 1.0 (Task 4), regression (Task 5).
- **Deferred (own later plans):** `similarityGrader` (embeddings + lexical fallback) → InstructionBench plan; MathBench, CodeGenBench, InstructionBench, MMLUBench → one plan each, all consuming `graderFor`.
- **Type consistency:** `gradeRequest`/`gradeResult`/`grader`/`llmGrader`/`graderVerdict`/`parseGraderVerdict`/`graderFor` named consistently across tasks. `judgedBy` values are exactly `"external"`/`"self"`.
- **No parser drift:** the SWE-bench judge keeps `parseJudgeVerdict` (reads weighted `score`); adding the maintainability criterion is prompt-only.

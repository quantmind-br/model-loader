# Benchmark Engine — Phase 1 (Foundations) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor the benchmark engine's mode dispatch into an extensible handler registry, add prefill/decode throughput metrics, modernize the long-context probe (dynamic + multi-needle) and the throughput presets (more presets + warmup), and grow the SWE-bench set from 8 to 32 — with zero behavior regression in the three existing modes.

**Architecture:** Replace the inline `switch rc.Mode` in `runner.Run` with a `modeHandler` interface and a package-level registry. The three existing modes become thin handler adapters that call the already-working `runProblem` / `runLongContext` / `runLlamaBench` methods, so the migration is behavior-preserving. New metrics ride on the existing `CompletionResult` → `ProblemResult` → `Aggregate` flow.

**Tech Stack:** Go 1.26, standard library + `embed`. Tests are table/golden style under `internal/service/benchmark/`. Dataset curation uses a one-shot Python 3 script (`datasets` + `requests`).

---

## File Structure

**Modified:**
- `internal/service/benchmark/client.go` — add `PromptProcessingTPS` to `CompletionResult`.
- `internal/service/benchmark/result.go` — add prefill/decode fields to `ProblemResult` and `Aggregate`; add `Category` type.
- `internal/service/benchmark/runner.go` — slim `Run` to dispatch via registry; set new per-problem metrics; expand `defaultPresets`; add warmup; dynamic+multi-needle long context; `aggregate()` rolls up new metrics.
- `internal/config/config.go` — expand default presets; add `warmup` knob.
- `cmd/model-loader/main.go` — wire `LlamaBenchWarmup`.

**Created:**
- `internal/service/benchmark/handler.go` — `modeHandler` interface, `Category`, registry.
- `internal/service/benchmark/handlers.go` — the three migrated handlers + `init()` registration.
- `internal/service/benchmark/handler_test.go` — registry + count tests.
- `internal/service/benchmark/longcontext_test.go` — needle generation/detection tests.
- `tools/swebench-curate/main.py` — one-shot dataset curation script.

---

## Task 1: Prefill/decode metrics on the data types

**Files:**
- Modify: `internal/service/benchmark/client.go:37-44` (`CompletionResult`) and `:164-171` (return).
- Modify: `internal/service/benchmark/result.go:48-75` (`ProblemResult`, `Aggregate`).
- Test: `internal/service/benchmark/client_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/service/benchmark/client_test.go`:

```go
func TestComplete_SetsPromptProcessingTPS(t *testing.T) {
	// Stream: two content chunks then a usage block with 100 prompt tokens.
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":2}}\n" +
		"data: [DONE]\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	got, err := Complete(context.Background(), srv.Client(), srv.URL, "", ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.PromptProcessingTPS <= 0 {
		t.Fatalf("PromptProcessingTPS = %v, want > 0", got.PromptProcessingTPS)
	}
}
```

Ensure the test file imports `io`, `net/http`, `net/http/httptest`, `context` (add any missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run TestComplete_SetsPromptProcessingTPS -v`
Expected: FAIL — `got.PromptProcessingTPS undefined (type CompletionResult has no field PromptProcessingTPS)`.

- [ ] **Step 3: Add the field and compute it**

In `client.go`, add to `CompletionResult` (after `TokensPerSecond`):

```go
	TokensPerSecond     float64 // completion tokens / generation time (decode speed)
	PromptProcessingTPS float64 // prompt tokens / TTFT (prefill speed)
```

In `Complete`, before the `return CompletionResult{...}`, after `tps` is computed:

```go
	ppTps := 0.0
	if ttft.Seconds() > 0 && promptTokens > 0 {
		ppTps = float64(promptTokens) / ttft.Seconds()
	}
```

Add `PromptProcessingTPS: ppTps,` to the returned struct literal.

- [ ] **Step 4: Add the same fields downstream in `result.go`**

In `ProblemResult` (after `TokensPerSecond`):

```go
	TokensPerSecond     float64 `json:"tokensPerSecond"`
	PromptProcessingTPS float64 `json:"promptProcessingTps,omitempty"`
	DecodeTPS           float64 `json:"decodeTps,omitempty"`
```

In `Aggregate` (after `AvgTokensPerSecond`):

```go
	AvgTokensPerSecond  float64 `json:"avgTokensPerSecond"`
	PromptProcessingTPS float64 `json:"promptProcessingTps,omitempty"` // avg prefill tok/s
	DecodeTPS           float64 `json:"decodeTps,omitempty"`           // avg decode tok/s
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/service/benchmark/ -run TestComplete_SetsPromptProcessingTPS -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/service/benchmark/client.go internal/service/benchmark/result.go internal/service/benchmark/client_test.go
git commit -m "feat(benchmark): add prefill/decode throughput fields"
```

---

## Task 2: Populate and aggregate the new per-problem metrics

**Files:**
- Modify: `internal/service/benchmark/runner.go` — `runProblem`, `runLongContext`, `runLlamaBench`, `aggregate`.
- Test: `internal/service/benchmark/runner_metrics_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `internal/service/benchmark/runner_metrics_test.go`:

```go
package benchmark

import "testing"

func TestAggregate_RollsUpPrefillDecode(t *testing.T) {
	results := []ProblemResult{
		{Resolved: true, PromptProcessingTPS: 100, DecodeTPS: 40, TokensPerSecond: 40, TTFTms: 10},
		{Resolved: true, PromptProcessingTPS: 200, DecodeTPS: 60, TokensPerSecond: 60, TTFTms: 20},
	}
	a := aggregate(results, 0, 0)
	if a.AvgPromptProcessingTPS != 150 {
		t.Errorf("AvgPromptProcessingTPS = %v, want 150", a.AvgPromptProcessingTPS)
	}
	if a.AvgDecodeTPS != 50 {
		t.Errorf("AvgDecodeTPS = %v, want 50", a.AvgDecodeTPS)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run TestAggregate_RollsUpPrefillDecode -v`
Expected: FAIL — `PromptProcessingTPS = 0, want 150`.

- [ ] **Step 3: Roll up in `aggregate()`**

In `runner.go` `aggregate`, add accumulators next to the existing `tpsSum`/`tpsN`:

```go
	var scoreSum, tpsSum, ttftSum, ppSum, decSum float64
	var tpsN, ttftN, ppN, decN int
```

Inside the loop, after the `TokensPerSecond` block:

```go
		if r.PromptProcessingTPS > 0 {
			ppSum += r.PromptProcessingTPS
			ppN++
		}
		if r.DecodeTPS > 0 {
			decSum += r.DecodeTPS
			decN++
		}
```

Before `return a`:

```go
	if ppN > 0 {
		a.AvgPromptProcessingTPS = ppSum / float64(ppN)
	}
	if decN > 0 {
		a.AvgDecodeTPS = decSum / float64(decN)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/benchmark/ -run TestAggregate_RollsUpPrefillDecode -v`
Expected: PASS.

- [ ] **Step 5: Set the fields where each result is built**

In `runProblem`, after `res.TokensPerSecond = comp.TokensPerSecond`:

```go
	res.DecodeTPS = comp.TokensPerSecond
	res.PromptProcessingTPS = comp.PromptProcessingTPS
```

In `runLongContext`, after `res.TokensPerSecond = comp.TokensPerSecond`:

```go
	res.DecodeTPS = comp.TokensPerSecond
	res.PromptProcessingTPS = comp.PromptProcessingTPS
```

In `runLlamaBench`, add prefill accumulation. Next to `tpsSum`:

```go
	var ttftSum, tpsSum, totalSum, ppTpsSum float64
```

In the rep loop, after `tpsSum += comp.TokensPerSecond`:

```go
		ppTpsSum += comp.PromptProcessingTPS
```

After `res.TokensPerSecond = tpsSum / n`:

```go
	res.DecodeTPS = tpsSum / n
	res.PromptProcessingTPS = ppTpsSum / n
```

- [ ] **Step 6: Run the package tests**

Run: `go test ./internal/service/benchmark/ -v`
Expected: PASS (no regressions).

- [ ] **Step 7: Commit**

```bash
git add internal/service/benchmark/runner.go internal/service/benchmark/runner_metrics_test.go
git commit -m "feat(benchmark): populate and aggregate prefill/decode metrics"
```

---

## Task 3: Mode-handler registry scaffolding

**Files:**
- Create: `internal/service/benchmark/handler.go`
- Test: `internal/service/benchmark/handler_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/service/benchmark/handler_test.go`:

```go
package benchmark

import "testing"

func TestRegistry_LookupUnknown(t *testing.T) {
	if _, ok := handlerFor(Mode("does-not-exist")); ok {
		t.Fatal("handlerFor(unknown) returned ok=true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run TestRegistry_LookupUnknown -v`
Expected: FAIL — `undefined: handlerFor`.

- [ ] **Step 3: Create `handler.go`**

```go
package benchmark

import "context"

// Category groups modes for the UI picker.
type Category string

const (
	CatQuality    Category = "Quality"
	CatSpeed      Category = "Speed"
	CatRobustness Category = "Robustness"
	CatKnowledge  Category = "Knowledge"
)

// modeHandler encapsulates one benchmark mode: its identity, how many problems
// it runs, fail-fast preparation (config validation / scorer build, done before
// the expensive backend launch), execution, and any mode-specific aggregate
// finalization.
type modeHandler interface {
	Mode() Mode
	Category() Category
	Count(r *Runner) int
	// Prepare validates config and builds the scorer BEFORE the backend is
	// launched. Returns a nil Scorer for objective (non-judged) modes.
	Prepare(r *Runner) (Scorer, error)
	// Execute runs every problem/preset and returns per-problem results and
	// optional transcripts. It is responsible for streaming "infer"/"score"
	// progress; Run sends "launch" and "done".
	Execute(ctx context.Context, r *Runner, base, model string, scorer Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error)
	// Finalize sets mode-specific Aggregate fields after the generic rollup.
	Finalize(agg *Aggregate, problems []ProblemResult)
}

var handlers = map[Mode]modeHandler{}

// registerHandler adds a handler to the registry; called from init().
func registerHandler(h modeHandler) { handlers[h.Mode()] = h }

// handlerFor returns the handler for a mode, if registered.
func handlerFor(m Mode) (modeHandler, bool) {
	h, ok := handlers[m]
	return h, ok
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/benchmark/ -run TestRegistry_LookupUnknown -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/benchmark/handler.go internal/service/benchmark/handler_test.go
git commit -m "feat(benchmark): add mode-handler interface and registry"
```

---

## Task 4: Migrate the three existing modes to handlers

**Files:**
- Create: `internal/service/benchmark/handlers.go`
- Test: `internal/service/benchmark/handler_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append to `internal/service/benchmark/handler_test.go`:

```go
func TestRegistry_RegistersExistingModes(t *testing.T) {
	for _, m := range []Mode{ModeJudge, ModeLongContext, ModeLlamaBench} {
		if _, ok := handlerFor(m); !ok {
			t.Errorf("mode %q not registered", m)
		}
	}
}

func TestHandlerCount_MatchesRunner(t *testing.T) {
	r := &Runner{presets: []tpPreset{{512, 128}, {4096, 256}}, problems: make([]Problem, 7)}
	if h, _ := handlerFor(ModeJudge); h.Count(r) != 7 {
		t.Errorf("judge count = %d, want 7", h.Count(r))
	}
	if h, _ := handlerFor(ModeLongContext); h.Count(r) != 1 {
		t.Errorf("longctx count = %d, want 1", h.Count(r))
	}
	if h, _ := handlerFor(ModeLlamaBench); h.Count(r) != 2 {
		t.Errorf("llama-bench count = %d, want 2", h.Count(r))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run TestRegistry_RegistersExistingModes -v`
Expected: FAIL — `mode "judge" not registered`.

- [ ] **Step 3: Create `handlers.go`**

```go
package benchmark

import "context"

func init() {
	registerHandler(judgeHandler{})
	registerHandler(longContextHandler{})
	registerHandler(llamaBenchHandler{})
}

// --- judge (SWE-bench Lite) ---

type judgeHandler struct{}

func (judgeHandler) Mode() Mode                          { return ModeJudge }
func (judgeHandler) Category() Category                  { return CatQuality }
func (judgeHandler) Count(r *Runner) int                 { return len(r.problems) }
func (judgeHandler) Prepare(r *Runner) (Scorer, error)   { return r.newScorer(ModeJudge) }
func (judgeHandler) Finalize(*Aggregate, []ProblemResult) {}

func (judgeHandler) Execute(ctx context.Context, r *Runner, base, model string, scorer Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	var results []ProblemResult
	var transcripts []ProblemTranscript
	for i, p := range r.problems {
		select {
		case <-ctx.Done():
			return results, transcripts, ctx.Err()
		default:
		}
		send(progress, Progress{Index: i + 1, Total: len(r.problems), ProblemID: p.ID, ProblemName: p.Name, Phase: "infer"})
		pr, tr := r.runProblem(ctx, scorer, base, model, p)
		results = append(results, pr)
		if r.cfg.SaveTranscripts {
			transcripts = append(transcripts, tr)
		}
		send(progress, Progress{Index: i + 1, Total: len(r.problems), ProblemID: p.ID, ProblemName: p.Name, Phase: "score"})
	}
	return results, transcripts, nil
}

// --- long-context needle ---

type longContextHandler struct{}

func (longContextHandler) Mode() Mode                           { return ModeLongContext }
func (longContextHandler) Category() Category                   { return CatRobustness }
func (longContextHandler) Count(*Runner) int                    { return 1 }
func (longContextHandler) Prepare(*Runner) (Scorer, error)      { return nil, nil }
func (longContextHandler) Finalize(*Aggregate, []ProblemResult) {}

func (longContextHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	send(progress, Progress{Index: 1, Total: 1, ProblemID: "long-context-needle", ProblemName: "Long-context needle retrieval", Phase: "infer"})
	pr, tr := r.runLongContext(ctx, base, model)
	var transcripts []ProblemTranscript
	if r.cfg.SaveTranscripts {
		transcripts = append(transcripts, tr)
	}
	return []ProblemResult{pr}, transcripts, nil
}

// --- llama-bench throughput ---

type llamaBenchHandler struct{}

func (llamaBenchHandler) Mode() Mode                           { return ModeLlamaBench }
func (llamaBenchHandler) Category() Category                   { return CatSpeed }
func (llamaBenchHandler) Count(r *Runner) int                  { return len(r.presets) }
func (llamaBenchHandler) Prepare(*Runner) (Scorer, error)      { return nil, nil }
func (llamaBenchHandler) Finalize(*Aggregate, []ProblemResult) {}

func (llamaBenchHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	var results []ProblemResult
	var transcripts []ProblemTranscript
	for i, ps := range r.presets {
		select {
		case <-ctx.Done():
			return results, transcripts, ctx.Err()
		default:
		}
		send(progress, Progress{Index: i + 1, Total: len(r.presets), ProblemID: ps.id(), ProblemName: ps.name(), Phase: "infer"})
		pr, tr := r.runLlamaBench(ctx, base, model, ps)
		results = append(results, pr)
		if r.cfg.SaveTranscripts {
			transcripts = append(transcripts, tr)
		}
	}
	return results, transcripts, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/service/benchmark/ -run 'TestRegistry|TestHandlerCount' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/benchmark/handlers.go internal/service/benchmark/handler_test.go
git commit -m "feat(benchmark): migrate judge/longctx/llama-bench to handlers"
```

---

## Task 5: Dispatch `Run` and `CountForMode` through the registry

**Files:**
- Modify: `internal/service/benchmark/runner.go` — `Run` (`:159-254`), `CountForMode` (`:149-154`).

- [ ] **Step 1: Write the failing test**

Append to `internal/service/benchmark/handler_test.go`:

```go
func TestCountForMode_DelegatesToRegistry(t *testing.T) {
	r := &Runner{presets: []tpPreset{{512, 128}}, problems: make([]Problem, 3)}
	if got := r.CountForMode(ModeJudge); got != 3 {
		t.Errorf("CountForMode(judge) = %d, want 3", got)
	}
	if got := r.CountForMode(Mode("unknown")); got != 0 {
		t.Errorf("CountForMode(unknown) = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run TestCountForMode_DelegatesToRegistry -v`
Expected: FAIL — `CountForMode(unknown) = 1, want 0` (current code returns the problem-set length for unknown modes).

- [ ] **Step 3: Rewrite `CountForMode`**

Replace the body of `CountForMode` with:

```go
// CountForMode reports how many problems a given mode will run.
func (r *Runner) CountForMode(mode Mode) int {
	if h, ok := handlerFor(mode); ok {
		return h.Count(r)
	}
	return 0
}
```

- [ ] **Step 4: Rewrite the dispatch section of `Run`**

In `Run`, replace everything from the `// Validate the mode ...` block (the `var scorer Scorer` switch at `:182-193`) through the end of the dispatch `if/else` (`:211-248`) with:

```go
	h, ok := handlerFor(rc.Mode)
	if !ok {
		return Run{}, fmt.Errorf("unsupported benchmark mode %q", rc.Mode)
	}

	// Prepare (validate config / build scorer) BEFORE launching an expensive
	// backend so missing judge config fails fast instead of after a model load.
	scorer, err := h.Prepare(r)
	if err != nil {
		return Run{}, err
	}

	send(progress, Progress{Total: h.Count(r), Phase: "launch"})

	port, logPath, pid, owned, err := r.ensureInstance(ctx, profile)
	if err != nil {
		return Run{}, err
	}
	if owned {
		defer func() { _ = r.pm.Kill(pid) }()
	}

	gpu := r.startGPUSampler(pid, port, logPath)
	defer gpu.stop()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	model := requestModelName(profile)

	results, transcripts, err := h.Execute(ctx, r, base, model, scorer, progress)
	run.Problems = results
	run.Transcript = transcripts
	if err != nil {
		run.FinishedAt = time.Now()
		run.Aggregate = aggregate(run.Problems, gpu.peakVRAM(), gpu.avgUtil())
		return run, err
	}

	run.FinishedAt = time.Now()
	run.Aggregate = aggregate(run.Problems, gpu.peakVRAM(), gpu.avgUtil())
	h.Finalize(&run.Aggregate, run.Problems)
	send(progress, Progress{Total: h.Count(r), Phase: "done"})
	return run, nil
```

Note: this removes the old `send(progress, Progress{Total: len(r.problems), Phase: "launch"})` line and the inline mode branches. Delete the now-unused `problemSet` method (`:137-143`) and `ProblemCount` only if unused elsewhere (keep `ProblemCount` — it is part of the API; leave it). Verify `problemSet` has no remaining callers before deleting:

Run: `grep -rn "problemSet" internal/`
If only its own definition remains, delete the method.

- [ ] **Step 5: Run the full package test suite**

Run: `go test ./internal/service/benchmark/ -v`
Expected: PASS (existing `TestCountForMode_LlamaBench` still passes; new delegation test passes).

- [ ] **Step 6: Build the whole project to catch caller breakage**

Run: `go build ./...`
Expected: success (UI callers `CountForMode` / `ModeLlamaBench` still compile).

- [ ] **Step 7: Commit**

```bash
git add internal/service/benchmark/runner.go internal/service/benchmark/handler_test.go
git commit -m "refactor(benchmark): dispatch Run via mode-handler registry"
```

---

## Task 6: Dynamic + multi-needle long-context probe

**Files:**
- Modify: `internal/service/benchmark/runner.go` — `runLongContext`, `buildHaystack`, remove `longNeedle` const.
- Test: `internal/service/benchmark/longcontext_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `internal/service/benchmark/longcontext_test.go`:

```go
package benchmark

import (
	"strings"
	"testing"
)

func TestBuildNeedles_AreUniquePerCall(t *testing.T) {
	a := buildNeedles()
	b := buildNeedles()
	if len(a) != 3 {
		t.Fatalf("want 3 needles, got %d", len(a))
	}
	// At least one value differs between calls (randomized), so a memorized
	// static answer cannot pass.
	same := true
	for i := range a {
		if a[i].value != b[i].value {
			same = false
		}
	}
	if same {
		t.Error("needles identical across calls; expected randomization")
	}
}

func TestBuildMultiNeedleHaystack_EmbedsAllNeedles(t *testing.T) {
	needles := []needle{{label: "alpha", value: "Reykjavik-1234"}, {label: "beta", value: "Oslo-5678"}, {label: "gamma", value: "Lima-9012"}}
	hay := buildMultiNeedleHaystack(4000, needles)
	for _, n := range needles {
		if !strings.Contains(hay, n.value) {
			t.Errorf("haystack missing needle %q", n.value)
		}
	}
}

func TestScoreNeedles_FractionFound(t *testing.T) {
	needles := []needle{{value: "A1"}, {value: "B2"}, {value: "C3"}}
	got := scoreNeedles("the values are A1 and C3", needles)
	if got != 2.0/3.0 {
		t.Errorf("scoreNeedles = %v, want 0.6667", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run 'TestBuildNeedles|TestBuildMultiNeedle|TestScoreNeedles' -v`
Expected: FAIL — `undefined: buildNeedles`.

- [ ] **Step 3: Add needle generation, haystack, scorer**

In `runner.go`, replace the `const longNeedle = ...` line with:

```go
import (
	// add to the existing import block:
	"math/rand"
)

// needle is one randomized fact planted in the long-context haystack.
type needle struct {
	label string // distinguishes the three planted facts
	value string // "<City>-<4digit>", the literal the model must recover
}

var needleCities = []string{
	"Reykjavik", "Ulaanbaatar", "Montevideo", "Gaborone", "Tbilisi",
	"Ljubljana", "Windhoek", "Paramaribo", "Bishkek", "Vientiane",
}

// buildNeedles makes three randomized needles at distinct depths.
func buildNeedles() []needle {
	labels := []string{"alpha", "beta", "gamma"}
	out := make([]needle, 3)
	for i := range out {
		city := needleCities[rand.Intn(len(needleCities))]
		out[i] = needle{label: labels[i], value: fmt.Sprintf("%s-%04d", city, rand.Intn(9000)+1000)}
	}
	return out
}

// scoreNeedles returns the fraction of needle values present in the response.
func scoreNeedles(response string, needles []needle) float64 {
	if len(needles) == 0 {
		return 0
	}
	found := 0
	for _, n := range needles {
		if strings.Contains(response, n.value) {
			found++
		}
	}
	return float64(found) / float64(len(needles))
}
```

Replace `buildHaystack` with a multi-needle version:

```go
// buildMultiNeedleHaystack generates ~targetTokens of varied pseudo-code with
// the needles planted at ~25%, ~50%, ~75% depth.
func buildMultiNeedleHaystack(targetTokens int, needles []needle) string {
	charBudget := targetTokens * 4
	depths := []int{charBudget / 4, charBudget / 2, charBudget * 3 / 4}
	var b strings.Builder
	planted := make([]bool, len(needles))
	i := 0
	for b.Len() < charBudget {
		for k := range needles {
			if !planted[k] && k < len(depths) && b.Len() >= depths[k] {
				fmt.Fprintf(&b, "\n# === FILE: registry_%s.py ===\n# Internal registration table.\nMAGIC_%s_NUMBER = '%s'\n# End.\n\n",
					needles[k].label, strings.ToUpper(needles[k].label), needles[k].value)
				planted[k] = true
			}
		}
		fmt.Fprintf(&b, "\n# === FILE: module_%04d.py ===\n", i)
		fmt.Fprintf(&b, "def handler_%04d(state, payload, retries=%d):\n", i, i%7)
		fmt.Fprintf(&b, "    total = 0\n    for item in payload.get('items_%d', []):\n", i%5)
		fmt.Fprintf(&b, "        total += item.weight * %d\n", (i%9)+1)
		fmt.Fprintf(&b, "    return Result(total=total, code=%d)\n", i%256)
		i++
	}
	for k := range needles {
		if !planted[k] {
			fmt.Fprintf(&b, "\nMAGIC_%s_NUMBER = '%s'\n", strings.ToUpper(needles[k].label), needles[k].value)
		}
	}
	return b.String()
}
```

- [ ] **Step 4: Rewrite `runLongContext` to use the multi-needle probe**

Replace the body of `runLongContext` (the prompt construction, request, and scoring) with:

```go
func (r *Runner) runLongContext(ctx context.Context, base, model string) (ProblemResult, ProblemTranscript) {
	res := ProblemResult{ProblemID: "long-context-needle", ProblemName: "Long-context needle retrieval"}
	tr := ProblemTranscript{ProblemID: res.ProblemID, ProblemName: res.ProblemName}

	targetTokens := r.cfg.LongContextTokens
	if targetTokens <= 0 {
		targetTokens = 8000
	}
	needles := buildNeedles()
	haystack := buildMultiNeedleHaystack(targetTokens, needles)
	user := "Below is a dump of a Python codebase. Read it carefully.\n\n" + haystack +
		"\n\nQuestion: three files define a constant named MAGIC_<NAME>_NUMBER. " +
		"List all three literal values, one per line, no explanation."

	reqCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()
	comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
		Model:       model,
		Temperature: 0,
		MaxTokens:   128,
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a careful code-reading assistant. Answer literally."},
			{Role: "user", Content: user},
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

	frac := scoreNeedles(comp.Content, needles)
	res.Score = frac
	res.Resolved = frac == 1.0
	res.Detail = fmt.Sprintf("recovered %.0f%% of needles (%d/3); prompt≈%d tok; pp %.0f t/s; tg %.0f t/s",
		frac*100, int(frac*3+0.5), comp.PromptTokens, comp.PromptProcessingTPS, comp.TokensPerSecond)
	return res, tr
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/service/benchmark/ -run 'TestBuildNeedles|TestBuildMultiNeedle|TestScoreNeedles' -v`
Expected: PASS.

- [ ] **Step 6: Build to confirm `buildHaystack`/`longNeedle` removal broke nothing**

Run: `go build ./... && go test ./internal/service/benchmark/`
Expected: success. If `buildHaystack` is referenced elsewhere, `grep -rn "buildHaystack\|longNeedle" internal/` returns nothing.

- [ ] **Step 7: Commit**

```bash
git add internal/service/benchmark/runner.go internal/service/benchmark/longcontext_test.go
git commit -m "feat(benchmark): dynamic multi-needle long-context probe"
```

---

## Task 7: Expanded LlamaBench presets + warmup

**Files:**
- Modify: `internal/service/benchmark/runner.go` — `defaultPresets`, `Config`, `runLlamaBench`, `NewRunner`.
- Modify: `internal/config/config.go:188` (default presets) + add warmup default.
- Modify: `cmd/model-loader/main.go` — wire warmup.
- Test: `internal/service/benchmark/llamabench_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append to `internal/service/benchmark/llamabench_test.go`:

```go
func TestDefaultPresets_Expanded(t *testing.T) {
	got, err := parsePresets(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []tpPreset{{128, 512}, {512, 128}, {2048, 256}, {4096, 256}, {8192, 128}, {16384, 64}}
	if len(got) != len(want) {
		t.Fatalf("got %d default presets, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("preset %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
```

Note: `TestParsePresets_DefaultWhenEmpty` asserts `got[0] == {512,128}` — update its expectation to `{128,512}` to match the new ordering.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run 'TestDefaultPresets_Expanded|TestParsePresets_DefaultWhenEmpty' -v`
Expected: FAIL — count mismatch (currently 2 defaults).

- [ ] **Step 3: Expand `defaultPresets`**

In `runner.go` replace:

```go
var defaultPresets = []tpPreset{{PromptTokens: 512, GenTokens: 128}, {PromptTokens: 4096, GenTokens: 256}}
```

with:

```go
var defaultPresets = []tpPreset{
	{PromptTokens: 128, GenTokens: 512},   // chat-like: short prefill, long gen
	{PromptTokens: 512, GenTokens: 128},
	{PromptTokens: 2048, GenTokens: 256},
	{PromptTokens: 4096, GenTokens: 256},
	{PromptTokens: 8192, GenTokens: 128},  // RAG-like: long prefill, short gen
	{PromptTokens: 16384, GenTokens: 64},  // extreme RAG
}
```

- [ ] **Step 4: Add a warmup knob and apply it**

In `runner.go` `Config`, after `LlamaBenchReps int`:

```go
	// LlamaBenchWarmup is how many discarded warmup reps run before measurement
	// to remove cold-start bias. <0 → default 1; 0 disables warmup.
	LlamaBenchWarmup int
```

Add a `warmup` field to `Runner`:

```go
	reps    int // ModeLlamaBench repetitions per preset
	warmup  int // discarded warmup reps before measurement
```

In `NewRunner`, after the `reps` defaulting block:

```go
	warmup := cfg.LlamaBenchWarmup
	if warmup < 0 {
		warmup = 1
	}
```

and add `warmup: warmup,` to the returned `&Runner{...}`.

In `runLlamaBench`, before the measured rep loop (`for i := 0; i < r.reps; i++`), add a warmup loop that discards results:

```go
	for w := 0; w < r.warmup; w++ {
		select {
		case <-ctx.Done():
			res.Err = ctx.Err().Error()
			tr.Error = res.Err
			return res, tr
		default:
		}
		warmCtx, warmCancel := context.WithTimeout(ctx, r.cfg.Timeout)
		_, _ = Complete(warmCtx, nil, base, "", ChatRequest{
			Model: model, Temperature: 0, MaxTokens: ps.GenTokens, IgnoreEOS: true, Messages: msgs,
		})
		warmCancel()
	}
```

- [ ] **Step 5: Update config default + main wiring**

In `internal/config/config.go:188` replace:

```go
	v.SetDefault("benchmark.llamabench.presets", []string{"512/128", "4096/256"})
```

with:

```go
	v.SetDefault("benchmark.llamabench.presets", []string{"128/512", "512/128", "2048/256", "4096/256", "8192/128", "16384/64"})
	v.SetDefault("benchmark.llamabench.warmup", 1)
```

In `internal/config/config.go` `LlamaBenchConfig` struct (around `:36`), add:

```go
	Warmup int `mapstructure:"warmup"`
```

In `cmd/model-loader/main.go`, in the `benchmark.Config{...}` literal (near `:119`), add:

```go
		LlamaBenchWarmup: svc.Cfg.Benchmark.LlamaBench.Warmup,
```

- [ ] **Step 6: Run tests + build**

Run: `go test ./internal/service/benchmark/ -v && go build ./...`
Expected: PASS and clean build.

- [ ] **Step 7: Commit**

```bash
git add internal/service/benchmark/runner.go internal/config/config.go cmd/model-loader/main.go internal/service/benchmark/llamabench_test.go
git commit -m "feat(benchmark): expand llama-bench presets and add warmup reps"
```

---

## Task 8: Grow the SWE-bench set from 8 to 32

**Files:**
- Create: `tools/swebench-curate/main.py`
- Modify: `internal/service/benchmark/data/swebench_lite.json` (regenerated)
- Test: `internal/service/benchmark/dataset_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append to `internal/service/benchmark/dataset_test.go`:

```go
func TestLoad_HasAtLeast32(t *testing.T) {
	problems, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(problems) < 32 {
		t.Fatalf("dataset has %d problems, want >= 32", len(problems))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run TestLoad_HasAtLeast32 -v`
Expected: FAIL — `dataset has 8 problems, want >= 32`.

- [ ] **Step 3: Write the curation script**

Create `tools/swebench-curate/main.py`:

```python
#!/usr/bin/env python3
"""Curate N SWE-bench Lite problems into the model-loader Problem schema.

Pulls instances from the public princeton-nlp/SWE-bench_Lite test split,
fetches the oracle context (full source of files the gold patch touches) from
raw.githubusercontent.com at the base commit, and emits the JSON array the Go
embed loader expects. Reproducible: same SEED -> same selection.

Usage:
    pip install datasets requests
    python tools/swebench-curate/main.py --count 32 \
        --out internal/service/benchmark/data/swebench_lite.json
"""
import argparse
import json
import random
import re
import sys
from collections import defaultdict

import requests
from datasets import load_dataset

SEED = 20260522
DIFF_PATH_RE = re.compile(r"^\+\+\+ b/(.+)$", re.MULTILINE)


def touched_files(patch: str) -> list[str]:
    return [p for p in DIFF_PATH_RE.findall(patch) if p != "/dev/null"]


def fetch_raw(repo: str, commit: str, path: str) -> str | None:
    url = f"https://raw.githubusercontent.com/{repo}/{commit}/{path}"
    r = requests.get(url, timeout=30)
    if r.status_code == 200:
        return r.text
    return None


def lang_of(path: str) -> str:
    if path.endswith(".py"):
        return "python"
    if path.endswith(".go"):
        return "go"
    if path.endswith((".js", ".ts")):
        return "javascript"
    if path.endswith(".rs"):
        return "rust"
    return "other"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--count", type=int, default=32)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    ds = load_dataset("princeton-nlp/SWE-bench_Lite", split="test")
    # Stratify by repo so the set is diverse, then sample deterministically.
    by_repo: dict[str, list] = defaultdict(list)
    for row in ds:
        by_repo[row["repo"]].append(row)
    rng = random.Random(SEED)
    repos = sorted(by_repo)
    rng.shuffle(repos)

    out, seen = [], set()
    while len(out) < args.count and repos:
        for repo in list(repos):
            if len(out) >= args.count:
                break
            bucket = by_repo[repo]
            if not bucket:
                repos.remove(repo)
                continue
            row = bucket.pop(rng.randrange(len(bucket)))
            inst = row["instance_id"]
            if inst in seen:
                continue
            files = touched_files(row["patch"])
            if not files:
                continue
            ctx, ok = {}, True
            for path in files:
                content = fetch_raw(row["repo"], row["base_commit"], path)
                if content is None:
                    ok = False
                    break
                ctx[path] = content
            if not ok or not ctx:
                continue
            seen.add(inst)
            out.append({
                "id": inst,
                "name": f"{row['repo']}: {inst.split('-')[-1]}",
                "language": lang_of(files[0]),
                "repoName": row["repo"],
                "statement": row["problem_statement"],
                "contextFiles": ctx,
                "goldenPatch": row["patch"],
            })
            print(f"[{len(out)}/{args.count}] {inst}", file=sys.stderr)

    with open(args.out, "w") as f:
        json.dump(out, f, indent=2, ensure_ascii=False)
    print(f"wrote {len(out)} problems to {args.out}", file=sys.stderr)
    return 0 if len(out) >= args.count else 1


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 4: Run the script to regenerate the dataset**

Run:

```bash
pip install datasets requests
python tools/swebench-curate/main.py --count 32 \
  --out internal/service/benchmark/data/swebench_lite.json
```

Expected: stderr prints `wrote 32 problems ...`. The output JSON matches the existing `Problem` schema (`id`, `name`, `language`, `repoName`, `statement`, `contextFiles`, `goldenPatch`).

- [ ] **Step 5: Run dataset tests to verify they pass**

Run: `go test ./internal/service/benchmark/ -run 'TestLoad' -v`
Expected: PASS — `Load()`'s `validate()` accepts all 32 (each has a non-empty statement, golden patch, and ≥1 context file), and the count is ≥ 32.

- [ ] **Step 6: Commit**

```bash
git add tools/swebench-curate/main.py internal/service/benchmark/data/swebench_lite.json internal/service/benchmark/dataset_test.go
git commit -m "feat(benchmark): expand SWE-bench Lite set to 32 problems"
```

---

## Task 9: Phase 1 regression gate

**Files:** none (verification only)

- [ ] **Step 1: Full test suite**

Run: `go test ./...`
Expected: PASS. (Note: `TestParseHelp_Golden` in `llamahelp` may fail locally due to an installed `llama-server` — this is a known, unrelated environment issue, not a Phase 1 regression.)

- [ ] **Step 2: Build the binary**

Run: `make build`
Expected: `bin/model-loader` produced with no errors.

- [ ] **Step 3: Smoke-check the three existing modes still dispatch**

Run: `go test ./internal/service/benchmark/ -run 'TestRegistry|TestHandlerCount|TestCountForMode' -v`
Expected: PASS — judge, longctx, and llama-bench all resolve through the registry with correct counts.

- [ ] **Step 4: Confirm no orphaned symbols**

Run: `grep -rn "buildHaystack\|longNeedle\|problemSet" internal/`
Expected: no matches (all removed during the refactor).

- [ ] **Step 5: Final commit (if any cleanup was needed)**

```bash
git add -A
git commit -m "chore(benchmark): phase 1 regression gate green" || echo "nothing to commit"
```

---

## Self-Review Notes

- **Spec coverage (Phase 1):** registry refactor (Tasks 3–5), dynamic+multi-needle (Task 6), expanded presets+warmup (Task 7), prefill/decode metrics (Tasks 1–2), SWE-bench 8→32 (Task 8), regression QA (Task 9). All Phase 1 items in §10 of the spec are covered.
- **Deferred (correctly out of Phase 1):** new cognitive modes, grader abstraction, RAG, UI grouping — those belong to Phases 2–4 and get their own plans.
- **Type consistency:** `PromptProcessingTPS`/`DecodeTPS` named identically across `CompletionResult`, `ProblemResult`, `Aggregate`. `needle{label,value}`, `buildNeedles`, `buildMultiNeedleHaystack`, `scoreNeedles` used consistently between Task 6's test and implementation. `modeHandler`/`handlerFor`/`registerHandler` consistent across Tasks 3–5.
- **Behavior preservation:** existing modes call the unchanged `runProblem`/`runLlamaBench` bodies; `runLongContext` is the only existing behavior intentionally changed (static → multi-needle, per spec §6.1).

# Benchmark UI (Phase 4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface the benchmark modes built in Phases 1–2 in the TUI: group the mode picker by category, render the new aggregate metrics + per-mode breakdowns in the run-detail view, and add a one-key CSV/JSON export of a run.

**Architecture:** All work is in `internal/ui/pages/benchmark*.go` plus one small exported helper in the `benchmark` package and one wiring line in `main.go`. The page is a Bubble Tea model with a `benchView` state machine (`bvList`/`bvModePick`/`bvRunDetail`/…). Picker grouping reorders the flat `benchModes` slice by category and inserts category headers at render time (cursor logic stays index-based). Run detail gains a prefill/decode throughput line plus mode-specific metric lines and breakdowns derived from per-problem `Detail` strings. Export is **auto-filename, no prompt** — it writes `<run-id>.json` + `<run-id>.csv` to a fixed exports directory and flashes the path, so it adds **no input-capture surface** and the `IsCapturingInput()` contract is unchanged.

**Tech Stack:** Go 1.26, Charmbracelet bubbletea/lipgloss, `encoding/csv`, `encoding/json`.

**Working directory:** All paths are relative to the worktree `/home/diogo/dev/model-loader/.worktrees/benchmark-phase2`. Run all `go`/`git` commands from there. The branch is `feat/benchmark-engine-expansion`.

**Scope note:** Phase 3 (RAG) was NOT built — the `Ragas*` aggregate fields do not exist. This plan renders only the metrics that exist: `AvgPromptProcessingTPS`, `AvgDecodeTPS`, `MathAccuracy`, `CodePassRate`, `InstFormatRate`, `InstRefusalRate`, `InstConsistency`, `MMLUAccuracy`. The cross-run Quantization Comparison view and the codegen stderr drill-down sub-view are out of scope (deferred).

---

## Context for the implementer

- `internal/ui/pages/benchmark.go` — the `BenchmarkPage` model, `benchView` states, `benchModes` slice, `viewModePick`, `viewRunDetail`, `viewList`, helpers (`truncate`, `dash`), and `IsCapturingInput()`/`Hints()`.
- `internal/ui/pages/benchmark_update.go` — `Update`/`handleKey`; `keyList` handles list keys (`b`/`enter`/`c`/`h`/`x`/`r`); the `bvRunDetail, bvCompare, bvHistory` case in `handleKey` currently handles only `esc`.
- `internal/ui/pages/benchmark_run.go` — run lifecycle messages + `handleRunDone`.
- `internal/ui/pages/benchmark_compare.go` — compare/history views (reference for table rendering style).
- `internal/service/benchmark/handler.go` — `Category` type (`CatQuality`/`CatSpeed`/`CatRobustness`/`CatKnowledge`), the unexported `handlers` registry, and `handlerFor`.
- Mode categories (already declared by each handler): **Quality** = Judge, MathBench, CodeGenBench; **Speed** = LlamaBench; **Robustness** = LongContext, InstBench; **Knowledge** = MMLUBench.
- `benchmark.Run` has `Problems []ProblemResult` (populated by `bstore.List()`), `Aggregate`, `Profile`, `Mode`, `ProfileName`, `StartedAt`, `Err`.
- `ProblemResult` fields: `ProblemID`, `ProblemName`, `Resolved`, `Score`, `TTFTms`, `TotalMs`, `TokensPerSecond`, `PromptProcessingTPS`, `DecodeTPS`, `PromptTokens`, `CompletionTokens`, `Detail`, `Err`.
- Per-problem `Detail` formats this plan parses: MathBench `"expected <a>, got \"<g>\" (difficulty <n>)"`; MMLUBench `"category=<C> expected <a> got \"<g>\""`.
- `NewBenchmarkPage(store, bstore, runner)` is constructed once, at `cmd/model-loader/main.go:128`.
- The module path is `github.com/quantmind-br/model-loader`.

Substring assertions in render tests are safe even with lipgloss styling: styles wrap text but the literal characters remain present in the output.

Run UI tests with: `go test ./internal/ui/pages/...`

---

## Task 1: Export `CategoryOf` from the benchmark package

**Files:**
- Modify: `internal/service/benchmark/handler.go`
- Test: `internal/service/benchmark/handler_test.go` (append; the file already exists)

- [ ] **Step 1: Write the failing test**

Append to `internal/service/benchmark/handler_test.go`:

```go
func TestCategoryOf(t *testing.T) {
	cases := map[Mode]Category{
		ModeJudge:        CatQuality,
		ModeMathBench:    CatQuality,
		ModeCodeGenBench: CatQuality,
		ModeLlamaBench:   CatSpeed,
		ModeLongContext:  CatRobustness,
		ModeInstBench:    CatRobustness,
		ModeMMLUBench:    CatKnowledge,
	}
	for m, want := range cases {
		got, ok := CategoryOf(m)
		if !ok {
			t.Fatalf("CategoryOf(%q): not found", m)
		}
		if got != want {
			t.Fatalf("CategoryOf(%q) = %q, want %q", m, got, want)
		}
	}
	if _, ok := CategoryOf("nope"); ok {
		t.Fatal("CategoryOf(unknown) should report not found")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/benchmark/ -run TestCategoryOf`
Expected: FAIL — `undefined: CategoryOf`.

- [ ] **Step 3: Add the exported helper**

In `internal/service/benchmark/handler.go`, after `handlerFor`, add:

```go
// CategoryOf reports the UI category for a registered mode. The bool is false
// for an unregistered mode. Used by the picker to group modes.
func CategoryOf(m Mode) (Category, bool) {
	h, ok := handlerFor(m)
	if !ok {
		return "", false
	}
	return h.Category(), true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/benchmark/ -run TestCategoryOf`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/benchmark/handler.go internal/service/benchmark/handler_test.go
git commit -m "feat(benchmark): export CategoryOf for UI mode grouping

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Group the mode picker by category

**Files:**
- Modify: `internal/ui/pages/benchmark.go`
- Test: `internal/ui/pages/benchmark_test.go` (append)

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/pages/benchmark_test.go` (the file already imports `testing` and the `benchmark` package; add `strings` to the import block):

```go
func TestBenchModesGroupedByCategory(t *testing.T) {
	// Once a category appears it must not reappear later (modes are contiguous
	// per category so the picker can show one header each).
	seen := map[benchmark.Category]bool{}
	var last benchmark.Category
	for i, m := range benchModes {
		c, ok := benchmark.CategoryOf(m)
		if !ok {
			t.Fatalf("mode %q has no category", m)
		}
		if i == 0 || c != last {
			if seen[c] {
				t.Fatalf("category %q is not contiguous in benchModes", c)
			}
			seen[c] = true
			last = c
		}
	}
}

func TestViewModePickShowsCategoryHeaders(t *testing.T) {
	p := BenchmarkPage{view: bvModePick, runningName: "demo"}
	out := p.viewModePick()
	for _, h := range []string{"Quality", "Speed", "Robustness", "Knowledge"} {
		if !strings.Contains(out, h) {
			t.Fatalf("viewModePick output missing category header %q", h)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/pages/ -run 'TestBenchModesGroupedByCategory|TestViewModePickShowsCategoryHeaders'`
Expected: FAIL — current `benchModes` order is not category-contiguous (Judge, LongContext, LlamaBench, Math… interleaves Quality/Robustness/Speed) and no headers are rendered.

- [ ] **Step 3: Reorder `benchModes` grouped by category**

In `internal/ui/pages/benchmark.go`, replace the `benchModes` slice with the category-grouped order:

```go
// benchModes is the selectable scoring-mode order in the mode picker, grouped
// by Category (Quality, Speed, Robustness, Knowledge) so the picker can render
// one header per group. Keep modes of the same category contiguous.
var benchModes = []benchmark.Mode{
	// Quality
	benchmark.ModeJudge,
	benchmark.ModeMathBench,
	benchmark.ModeCodeGenBench,
	// Speed
	benchmark.ModeLlamaBench,
	// Robustness
	benchmark.ModeLongContext,
	benchmark.ModeInstBench,
	// Knowledge
	benchmark.ModeMMLUBench,
}
```

- [ ] **Step 4: Render category headers in `viewModePick`**

In `viewModePick`, replace the row-building loop (the `rows := make(...)` through the `for i, m := range benchModes { ... }` block) with a version that inserts a category header when the category changes:

```go
	rows := make([]string, 0, len(benchModes)+4)
	var lastCat benchmark.Category
	for i, m := range benchModes {
		if c, ok := benchmark.CategoryOf(m); ok && c != lastCat {
			rows = append(rows, theme.Subtitle.Render(string(c)))
			lastCat = c
		}
		line := fmt.Sprintf("%-22s  %s", m.Title(), descs[m])
		if i == p.modeCursor {
			if theme.NoColor() {
				line = "> " + line
			} else {
				line = theme.Selected.Render(line)
			}
		} else {
			line = "  " + line
		}
		rows = append(rows, line)
	}
```

(The `descs` map and the final `return lipgloss.JoinVertical(...)` stay unchanged. The cursor still indexes `benchModes` directly, so navigation is unaffected.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/pages/ -run 'TestBenchModesGroupedByCategory|TestViewModePickShowsCategoryHeaders|TestBenchModesInclude'`
Expected: PASS (the existing membership tests still pass — order doesn't affect them).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/pages/benchmark.go internal/ui/pages/benchmark_test.go
git commit -m "feat(ui): group benchmark mode picker by category

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Per-mode metrics + breakdowns in run detail

**Files:**
- Modify: `internal/ui/pages/benchmark.go`
- Test: `internal/ui/pages/benchmark_detail_test.go` (create)

Adds a prefill/decode throughput line (shown when either value is present) and a mode-specific block to `viewRunDetail`: MathBench (accuracy + difficulty breakdown), CodeGenBench (pass rate + executed/skipped), InstBench (format/refusal/consistency rates), MMLUBench (accuracy + per-category breakdown). Math/MMLU breakdowns are derived from the per-problem `Detail` strings this codebase produces.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/pages/benchmark_detail_test.go`:

```go
package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

func detailFor(mode benchmark.Mode, agg benchmark.Aggregate, probs []benchmark.ProblemResult) string {
	run := benchmark.Run{
		ProfileName: "demo",
		Mode:        mode,
		StartedAt:   time.Now(),
		Aggregate:   agg,
		Problems:    probs,
	}
	p := BenchmarkPage{view: bvRunDetail, detail: &run}
	return p.viewRunDetail()
}

func TestRunDetailMathBreakdown(t *testing.T) {
	out := detailFor(benchmark.ModeMathBench,
		benchmark.Aggregate{Total: 2, Resolved: 1, MathAccuracy: 0.5, AvgDecodeTPS: 40, AvgPromptProcessingTPS: 800},
		[]benchmark.ProblemResult{
			{Resolved: true, Detail: `expected 4, got "4" (difficulty 1)`},
			{Resolved: false, Detail: `expected 9, got "8" (difficulty 2)`},
		})
	if !strings.Contains(out, "math accuracy 50%") {
		t.Fatalf("missing math accuracy line:\n%s", out)
	}
	if !strings.Contains(out, "difficulty 1") || !strings.Contains(out, "difficulty 2") {
		t.Fatalf("missing difficulty breakdown:\n%s", out)
	}
	if !strings.Contains(out, "prefill") || !strings.Contains(out, "decode") {
		t.Fatalf("missing prefill/decode line:\n%s", out)
	}
}

func TestRunDetailMMLUBreakdown(t *testing.T) {
	out := detailFor(benchmark.ModeMMLUBench,
		benchmark.Aggregate{Total: 2, Resolved: 1, MMLUAccuracy: 0.5},
		[]benchmark.ProblemResult{
			{Resolved: true, Detail: `category=STEM expected B got "B"`},
			{Resolved: false, Detail: `category=Social Sciences expected A got "C"`},
		})
	if !strings.Contains(out, "MMLU accuracy 50%") {
		t.Fatalf("missing MMLU accuracy line:\n%s", out)
	}
	if !strings.Contains(out, "STEM") || !strings.Contains(out, "Social Sciences") {
		t.Fatalf("missing category breakdown:\n%s", out)
	}
}

func TestRunDetailInstructionRates(t *testing.T) {
	out := detailFor(benchmark.ModeInstBench,
		benchmark.Aggregate{Total: 3, Resolved: 2, InstFormatRate: 0.5, InstRefusalRate: 1, InstConsistency: 0.9},
		nil)
	if !strings.Contains(out, "format") || !strings.Contains(out, "refusal") || !strings.Contains(out, "consistency") {
		t.Fatalf("missing instruction rate line:\n%s", out)
	}
}

func TestRunDetailCodeGenRate(t *testing.T) {
	out := detailFor(benchmark.ModeCodeGenBench,
		benchmark.Aggregate{Total: 2, Resolved: 1, CodePassRate: 0.5},
		[]benchmark.ProblemResult{
			{Resolved: true, Detail: "passed"},
			{Resolved: false, Detail: "failed: boom"},
		})
	if !strings.Contains(out, "pass rate") {
		t.Fatalf("missing codegen pass-rate line:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/pages/ -run TestRunDetail`
Expected: FAIL — no accuracy/breakdown/prefill lines are rendered yet.

- [ ] **Step 3: Add the breakdown helpers and the detail block**

In `internal/ui/pages/benchmark.go`, add the `regexp` import (the import block currently has `context`, `fmt`, `strings` plus the charm/domain imports — add `"regexp"`). Then add these helpers (near the bottom, by `dash`):

```go
var (
	mathDifficultyRe = regexp.MustCompile(`difficulty (\d+)\)`)
)

// modeDetailLines returns the mode-specific metric/breakdown lines shown in the
// run detail view, below the generic summary. Empty for modes with no extra
// metrics. Breakdowns for math/MMLU are derived from the per-problem Detail
// strings produced by runMathBench / runMMLUBench.
func modeDetailLines(r benchmark.Run) []string {
	a := r.Aggregate
	var lines []string
	if a.AvgPromptProcessingTPS > 0 || a.AvgDecodeTPS > 0 {
		lines = append(lines, fmt.Sprintf("prefill %.1f tok/s   decode %.1f tok/s", a.AvgPromptProcessingTPS, a.AvgDecodeTPS))
	}
	switch r.Mode {
	case benchmark.ModeMathBench:
		lines = append(lines, fmt.Sprintf("math accuracy %.0f%%", a.MathAccuracy*100))
		if b := mathDifficultyBreakdown(r.Problems); b != "" {
			lines = append(lines, "  "+b)
		}
	case benchmark.ModeCodeGenBench:
		executed, skipped := 0, 0
		for _, pr := range r.Problems {
			if pr.Err != "" {
				skipped++
			} else {
				executed++
			}
		}
		if executed == 0 {
			lines = append(lines, "code generation skipped (python3 not available)")
		} else {
			lines = append(lines, fmt.Sprintf("code pass rate %.0f%% (%d executed, %d skipped)", a.CodePassRate*100, executed, skipped))
		}
	case benchmark.ModeInstBench:
		lines = append(lines, fmt.Sprintf("instruction — format %.0f%%   refusal %.0f%%   consistency %.2f",
			a.InstFormatRate*100, a.InstRefusalRate*100, a.InstConsistency))
	case benchmark.ModeMMLUBench:
		lines = append(lines, fmt.Sprintf("MMLU accuracy %.0f%%", a.MMLUAccuracy*100))
		if b := mmluCategoryBreakdown(r.Problems); b != "" {
			lines = append(lines, "  "+b)
		}
	}
	return lines
}

// mathDifficultyBreakdown tallies solved/total per difficulty band parsed from
// the math Detail format "… (difficulty N)".
func mathDifficultyBreakdown(problems []benchmark.ProblemResult) string {
	type tally struct{ solved, total int }
	bands := map[string]*tally{}
	var order []string
	for _, pr := range problems {
		m := mathDifficultyRe.FindStringSubmatch(pr.Detail)
		if m == nil {
			continue
		}
		d := m[1]
		t, ok := bands[d]
		if !ok {
			t = &tally{}
			bands[d] = t
			order = append(order, d)
		}
		t.total++
		if pr.Resolved {
			t.solved++
		}
	}
	sort.Strings(order)
	parts := make([]string, 0, len(order))
	for _, d := range order {
		t := bands[d]
		parts = append(parts, fmt.Sprintf("difficulty %s: %d/%d", d, t.solved, t.total))
	}
	return strings.Join(parts, "   ")
}

// mmluCategoryBreakdown tallies solved/total per category parsed from the MMLU
// Detail format "category=<C> expected …".
func mmluCategoryBreakdown(problems []benchmark.ProblemResult) string {
	type tally struct{ solved, total int }
	cats := map[string]*tally{}
	var order []string
	for _, pr := range problems {
		cat := parseMMLUCategory(pr.Detail)
		if cat == "" {
			continue
		}
		t, ok := cats[cat]
		if !ok {
			t = &tally{}
			cats[cat] = t
			order = append(order, cat)
		}
		t.total++
		if pr.Resolved {
			t.solved++
		}
	}
	sort.Strings(order)
	parts := make([]string, 0, len(order))
	for _, c := range order {
		t := cats[c]
		parts = append(parts, fmt.Sprintf("%s: %d/%d", c, t.solved, t.total))
	}
	return strings.Join(parts, "   ")
}

// parseMMLUCategory extracts the category from "category=<C> expected …".
func parseMMLUCategory(detail string) string {
	const pfx = "category="
	if !strings.HasPrefix(detail, pfx) {
		return ""
	}
	rest := detail[len(pfx):]
	if i := strings.Index(rest, " expected"); i >= 0 {
		return rest[:i]
	}
	return ""
}
```

Add `"sort"` to the import block as well (used by both breakdown helpers).

- [ ] **Step 4: Insert the lines into `viewRunDetail`**

In `viewRunDetail`, after the `summary` is computed (and the `if r.Err != "" { ... }` block) and before the `header := theme.Subtitle.Render(...)` of the per-problem table, build the extra block and include it in the final `JoinVertical`. Replace the final `return lipgloss.JoinVertical(...)` with:

```go
	extra := modeDetailLines(r)
	sections := []string{title, strings.Join(meta, "\n"), summary}
	if len(extra) > 0 {
		sections = append(sections, strings.Join(extra, "\n"))
	}
	sections = append(sections, "", strings.Join(rows, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/pages/ -run TestRunDetail`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/pages/benchmark.go internal/ui/pages/benchmark_detail_test.go
git commit -m "feat(ui): render per-mode metrics and breakdowns in run detail

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: CSV/JSON run export

**Files:**
- Create: `internal/ui/pages/benchmark_export.go`
- Modify: `internal/ui/pages/benchmark.go` (page field + constructor), `internal/ui/pages/benchmark_update.go` (key handling + hints), `cmd/model-loader/main.go` (wiring)
- Test: `internal/ui/pages/benchmark_export_test.go` (create)

Export is auto-filename: it writes `<run-id>.json` (the raw run) and `<run-id>.csv` (per-problem rows) into a fixed exports directory and flashes the path. No prompt, so no input-capture surface is added.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/pages/benchmark_export_test.go`:

```go
package pages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

func TestExportRun(t *testing.T) {
	dir := t.TempDir()
	run := benchmark.Run{
		ID:          "demo-123",
		ProfileName: "demo",
		Mode:        benchmark.ModeMathBench,
		StartedAt:   time.Now(),
		Aggregate:   benchmark.Aggregate{Total: 1, Resolved: 1, MathAccuracy: 1},
		Problems: []benchmark.ProblemResult{
			{ProblemID: "p1", ProblemName: "q1", Resolved: true, Score: 1, Detail: "ok"},
		},
	}
	jsonPath, csvPath, err := exportRun(dir, run)
	if err != nil {
		t.Fatalf("exportRun: %v", err)
	}
	if filepath.Dir(jsonPath) != dir || !strings.HasSuffix(jsonPath, "demo-123.json") {
		t.Fatalf("unexpected json path %q", jsonPath)
	}
	// JSON round-trips back to the run.
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read json: %v", err)
	}
	var back benchmark.Run
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal exported json: %v", err)
	}
	if back.ID != "demo-123" || len(back.Problems) != 1 {
		t.Fatalf("json export lost data: %+v", back)
	}
	// CSV has a header and one data row mentioning the problem id.
	csv, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	text := string(csv)
	if !strings.Contains(text, "problemId") || !strings.Contains(text, "p1") {
		t.Fatalf("csv missing header or data row:\n%s", text)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/pages/ -run TestExportRun`
Expected: FAIL — `undefined: exportRun`.

- [ ] **Step 3: Write the export helper**

Create `internal/ui/pages/benchmark_export.go`:

```go
package pages

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

// exportRun writes a run to <dir>/<id>.json (the raw run) and <dir>/<id>.csv
// (one row per problem) for external analysis. It returns the two paths.
func exportRun(dir string, r benchmark.Run) (jsonPath, csvPath string, err error) {
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("create export dir: %w", err)
	}
	jsonPath = filepath.Join(dir, r.ID+".json")
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal run: %w", err)
	}
	if err = os.WriteFile(jsonPath, raw, 0o644); err != nil {
		return "", "", fmt.Errorf("write json: %w", err)
	}

	csvPath = filepath.Join(dir, r.ID+".csv")
	f, err := os.Create(csvPath)
	if err != nil {
		return "", "", fmt.Errorf("create csv: %w", err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	header := []string{
		"problemId", "problemName", "resolved", "score", "ttftMs", "totalMs",
		"tokensPerSecond", "promptProcessingTps", "decodeTps", "promptTokens",
		"completionTokens", "detail", "err",
	}
	if err = w.Write(header); err != nil {
		return "", "", fmt.Errorf("write csv header: %w", err)
	}
	for _, pr := range r.Problems {
		row := []string{
			pr.ProblemID, pr.ProblemName, strconv.FormatBool(pr.Resolved),
			strconv.FormatFloat(pr.Score, 'f', 4, 64),
			strconv.FormatInt(pr.TTFTms, 10), strconv.FormatInt(pr.TotalMs, 10),
			strconv.FormatFloat(pr.TokensPerSecond, 'f', 2, 64),
			strconv.FormatFloat(pr.PromptProcessingTPS, 'f', 2, 64),
			strconv.FormatFloat(pr.DecodeTPS, 'f', 2, 64),
			strconv.Itoa(pr.PromptTokens), strconv.Itoa(pr.CompletionTokens),
			pr.Detail, pr.Err,
		}
		if err = w.Write(row); err != nil {
			return "", "", fmt.Errorf("write csv row: %w", err)
		}
	}
	w.Flush()
	if err = w.Error(); err != nil {
		return "", "", fmt.Errorf("flush csv: %w", err)
	}
	return jsonPath, csvPath, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/pages/ -run TestExportRun`
Expected: PASS.

- [ ] **Step 5: Add the `exportDir` field + constructor param**

In `internal/ui/pages/benchmark.go`, add a field to `BenchmarkPage` (after `runner`):

```go
	exportDir string
```

Change `NewBenchmarkPage` to accept and store it:

```go
func NewBenchmarkPage(store profilestore.Store, bstore benchmarkstore.Store, runner *benchmark.Runner, exportDir string) BenchmarkPage {
	return BenchmarkPage{
		store:     store,
		bstore:    bstore,
		runner:    runner,
		exportDir: exportDir,
		view:      bvList,
		flash:     components.NewFlash("benchmark"),
		spinner:   components.NewLoadingSpinner(),
	}
}
```

- [ ] **Step 6: Wire the export dir in main.go**

In `cmd/model-loader/main.go`, the construction at line ~128 becomes:

```go
	benchmarkPage := pages.NewBenchmarkPage(svc.Store, benchStore, benchRunner,
		filepath.Join(svc.Cfg.Paths.StateDir, "benchmark", "exports"))
```

(`filepath` is already imported in main.go — it is used for `benchStore` on the line above.)

- [ ] **Step 7: Add the export key + flash + hints**

In `internal/ui/pages/benchmark_update.go`, add an `exportSelected` helper and wire the `e` key in both the list and the detail view.

Add to `keyList`'s switch (after the `"r"` case):

```go
	case "e":
		return p.exportSelected()
```

Change the `bvRunDetail, bvCompare, bvHistory` case in `handleKey` to also handle `e` in the detail view:

```go
	case bvRunDetail, bvCompare, bvHistory:
		if msg.String() == "esc" {
			p.view = bvList
			return p, nil
		}
		if msg.String() == "e" && p.view == bvRunDetail && p.detail != nil {
			return p.exportRunValue(*p.detail)
		}
		return p, nil
```

Add the two helpers at the end of `benchmark_update.go`:

```go
// exportSelected exports the run highlighted in the list.
func (p BenchmarkPage) exportSelected() (tea.Model, tea.Cmd) {
	if p.runCursor >= len(p.runs) {
		return p, nil
	}
	return p.exportRunValue(p.runs[p.runCursor])
}

// exportRunValue writes a run to the exports dir and flashes the result.
func (p BenchmarkPage) exportRunValue(r benchmark.Run) (tea.Model, tea.Cmd) {
	jsonPath, _, err := exportRun(p.exportDir, r)
	if err != nil {
		p.flash, _ = flashError(p.flash, "export: "+err.Error())
		return p, nil
	}
	var fc tea.Cmd
	p.flash, fc = flashSuccess(p.flash, "exported to "+filepath.Dir(jsonPath))
	return p, fc
}
```

Add the imports `"path/filepath"` and the benchmark package to `benchmark_update.go`'s import block:

```go
import (
	"path/filepath"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)
```

Update `Hints()` in `benchmark.go`: add `[e] export` to the list hints and the detail hint. The `bvRunDetail` case becomes `return "[e] export  [esc] back"`, and the default (list) hints string becomes:

```go
		hints := "[b] run  [enter] details  [c] compare  [e] export  [x] delete  [r] reload"
```

- [ ] **Step 8: Run the full UI + build verification**

```bash
go build ./...
go test ./internal/ui/pages/...
```

Expected: build clean (main.go compiles with the new signature); tests PASS.

- [ ] **Step 9: Final verification — build, vet, gofmt, suites**

```bash
go build ./...
go vet ./internal/service/benchmark/... ./internal/ui/pages/...
gofmt -l internal/service/benchmark/handler.go internal/ui/pages/benchmark.go internal/ui/pages/benchmark_update.go internal/ui/pages/benchmark_export.go internal/ui/pages/benchmark_detail_test.go internal/ui/pages/benchmark_export_test.go internal/ui/pages/benchmark_test.go cmd/model-loader/main.go
go test ./internal/service/benchmark/... ./internal/ui/pages/... ./cmd/...
```

Expected: build clean; vet clean; `gofmt -l` (on the listed files) prints nothing; tests PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/ui/pages/benchmark_export.go internal/ui/pages/benchmark_export_test.go internal/ui/pages/benchmark.go internal/ui/pages/benchmark_update.go cmd/model-loader/main.go
git commit -m "feat(ui): add CSV/JSON run export to the benchmark tab

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review (completed during planning)

**Spec coverage (§9 UI, Phase 4):**
- Register modes grouped by Category in the picker → Tasks 1 (CategoryOf) + 2 (grouping + headers).
- Render new Aggregate fields in run detail → Task 3 (prefill/decode line + per-mode metric lines).
- Per-mode detail sections (math difficulty breakdown; codegen pass/fail — the existing per-problem table already lists pass/fail; instruction rate lines; mmlu category breakdown) → Task 3.
- CSV/JSON export of a run → Task 4.
- Input-routing contract: export is auto-filename with no text input, so no new capturing surface — `IsCapturingInput()` is unchanged. The `e` key is page-local (not a global shortcut) and is handled in `keyList` (list, where the page does not capture) and the detail case (where the page already captures via `view != bvList`). No routing regression.
- Deferred (documented): codegen stderr drill-down sub-view; cross-run Quantization Comparison view; Ragas lines (Phase 3 not built).

**Placeholder scan:** No TBD/TODO; every code step has complete code.

**Type consistency:** `CategoryOf(Mode) (Category, bool)` used identically in Tasks 1/2. `exportRun(dir string, r Run) (string, string, error)` consistent in Tasks 4 step 3/7 and the test. `modeDetailLines`/`mathDifficultyBreakdown`/`mmluCategoryBreakdown`/`parseMMLUCategory` consistent within Task 3. `NewBenchmarkPage` 4-arg signature consistent across benchmark.go + main.go.

**Detail-format coupling (documented):** `mathDifficultyBreakdown` and `mmluCategoryBreakdown` parse the per-problem `Detail` strings produced by `runMathBench` (`… (difficulty N)`) and `runMMLUBench` (`category=<C> expected …`). If those formats change, the breakdown parsers must change too — noted in the helper comments.

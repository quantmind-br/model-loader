# Benchmark Phase 3 — RAG & Coherence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add RAG-quality and coherence evaluation to the benchmark engine — a grader-scored RagasBench mode, a real-arXiv-abstract "quality haystack" for the long-context probe, and a dedicated multi-doc summarization-coherence mode.

**Architecture:** Three independent additions over the existing `modeHandler` registry + `grader` abstraction (both already shipped in Phases 1–2). RagasBench and SummaryBench are new self-registering handlers in category `Quality`; both grade free-form answers with `r.graderFor(base, model)` (external judge if configured, else self-judge). The long-context haystack swaps its pseudo-Python filler for embedded real arXiv abstracts while keeping the existing randomized-needle planting and objective scoring untouched. Per-criterion sub-scores ride in the existing `ProblemResult.Detail` string and are parsed back in `Finalize` and the UI — the same pattern Math/MMLU already use, so the store and generic views stay unchanged.

**Tech Stack:** Go 1.26, Charmbracelet bubbletea/lipgloss (TUI), Python 3 + HuggingFace `datasets` (reproducible dataset curation), `//go:embed data/*.json`.

**Decisions locked with the user before planning:**
- **D-P3-1:** Multi-doc summarization coherence is a **dedicated new mode** (`ModeSummaryBench`, category `Quality`), NOT folded into LongContext. LongContext stays a pure objective needle probe; the graded coherence score never contaminates the needle SolveRate.
- **D-P3-2:** The quality haystack uses **real arXiv abstracts fetched from HuggingFace** (reproducible script, fixed seed + pinned revision), embedded in `data/arxiv_docs.json`.

**Out of scope (deferred / not in Phase 3):** new config knobs (§8 of the master spec) — RagasBench/SummaryBench reuse the existing `MaxTokens`/`Timeout`; cross-run comparison UI; the optional `lm_eval` wrapper.

---

## Conventions (read once, applies to every task)

- Work happens in the worktree `/home/diogo/dev/model-loader/.worktrees/benchmark-phase2` on branch `feat/benchmark-engine-expansion`. Verify with `git rev-parse --abbrev-ref HEAD` before starting. All paths below are relative to that worktree root.
- Package under change: `internal/service/benchmark` (Go) and `internal/ui/pages` (UI).
- After every code change run, from the worktree root: `gofmt -l .` (must print nothing), `go vet ./internal/service/benchmark/... ./internal/ui/pages/...`, and the package tests. Fix any drift before committing.
- Golden datasets: each loader has a golden test that decodes the embedded JSON and asserts invariants. Regenerate data only via its curation script; never hand-edit the JSON.
- All user-facing strings (Titles, descs, Detail text, prompts shown to the model) are **English** — project rule.
- Commit messages end with exactly: `Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>`.
- Do NOT merge the branch into main — integration is the user's responsibility.

### Established patterns to copy (already in the codebase)

- **Handler shape:** see `mmlubench.go` (`mmluHandler`) — `Mode()/Category()/Count()/Prepare()/Execute()/Finalize()` + `init(){ registerHandler(...) }`. Objective modes return `nil, nil` from `Prepare`. Graded modes still return `nil, nil` from `Prepare` and build the grader **inside Execute** with `r.graderFor(base, model)` (base/model aren't available in Prepare).
- **Grader call:** `grader.Grade(ctx, gradeRequest{Criterion, Guidance, Question, Context, Answer})` → `gradeResult{Score, Pass, Detail, Raw, JudgedBy}` (see `grader.go`). Always wrap each grader call in `ctx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)`.
- **Detail-encoded sub-scores parsed in Finalize/UI:** see `mmluCategoryBreakdown`/`parseMMLUCategory` and `mathDifficultyBreakdown` in `internal/ui/pages/benchmark.go`, and `mmluHandler.Finalize` in `mmlubench.go`.
- **Runner dataset wiring:** datasets load in `NewRunner` (`runner.go:136`); add fields to the `Runner` struct (`runner.go:120`) and assign them in the final `return &Runner{...}` (`runner.go:175`).
- **Synthetic curation script:** see `tools/instruction-curate/main.py` (deterministic, authored data → JSON).
- **HF-fetch curation script:** see `tools/mmlu-curate/main.py` (`load_dataset`, `SEED`, `DATASET_REVISION`, argparse `--count`/`--out`).
- **Chat completion:** `Complete(ctx, nil, base, "", ChatRequest{Model, Temperature, MaxTokens, Messages})` — `nil` doer uses the default HTTP client; tests pass a stub `base` (httptest server) instead.

---

## Task 1: Domain type extensions (`result.go`)

**Files:**
- Modify: `internal/service/benchmark/result.go`
- Test: `internal/service/benchmark/result_test.go`

- [ ] **Step 1: Add the failing test**

In `result_test.go`, add:

```go
func TestTitle_Phase3Modes(t *testing.T) {
	cases := map[Mode]string{
		ModeRagasBench:   "RAG faithfulness (synthetic)",
		ModeSummaryBench: "Summarization coherence",
	}
	for m, want := range cases {
		if got := m.Title(); got != want {
			t.Errorf("Title(%q) = %q, want %q", m, got, want)
		}
	}
}

func TestAggregate_Phase3FieldsOmitEmpty(t *testing.T) {
	b, err := json.Marshal(Aggregate{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ragasFaithfulness", "ragasRelevancy", "ragasPrecision", "summaryCoherence"} {
		if strings.Contains(string(b), k) {
			t.Errorf("zero %s should be omitted, got %s", k, b)
		}
	}
}
```

Ensure `result_test.go` imports `encoding/json` and `strings` (add if missing).

- [ ] **Step 2: Run it; expect failure**

Run: `go test ./internal/service/benchmark/ -run 'Phase3' -v`
Expected: FAIL — `ModeRagasBench`/`ModeSummaryBench` undefined.

- [ ] **Step 3: Add the constants, Titles, and Aggregate fields**

In `result.go`, inside the `const (...)` block (after `ModeMMLUBench`):

```go
	// ModeRagasBench: retrieval-augmented-generation quality probe — synthetic
	// scenarios where the model answers a question strictly from supplied
	// documents. A grader scores faithfulness (no hallucination beyond the
	// docs), answer relevancy, and context precision. Graded (external or self).
	ModeRagasBench Mode = "ragas-bench"
	// ModeSummaryBench: multi-document summarization-coherence probe — the model
	// summarizes ~10 short documents and must cover a known set of key facts. A
	// grader scores coherence + fact coverage. Graded (external or self).
	ModeSummaryBench Mode = "summary-bench"
```

In `Title()`, add cases before `default`:

```go
	case ModeRagasBench:
		return "RAG faithfulness (synthetic)"
	case ModeSummaryBench:
		return "Summarization coherence"
```

In `Aggregate`, add after `MMLUAccuracy` (keep the aligned `omitempty` style):

```go
	RagasFaithfulness float64 `json:"ragasFaithfulness,omitempty"` // 0..1 grounded-in-docs score
	RagasRelevancy    float64 `json:"ragasRelevancy,omitempty"`    // 0..1 answers-the-question score
	RagasPrecision    float64 `json:"ragasPrecision,omitempty"`    // 0..1 uses-the-right-context score
	SummaryCoherence  float64 `json:"summaryCoherence,omitempty"`  // 0..1 multi-doc coherence + fact coverage
```

- [ ] **Step 4: Run tests; expect pass**

Run: `go test ./internal/service/benchmark/ -run 'Phase3|Title' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/benchmark/result.go internal/service/benchmark/result_test.go
git commit -m "feat(benchmark): add RAG + summary modes and aggregate fields

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: arXiv abstracts dataset + curation script

**Files:**
- Create: `tools/arxiv-curate/main.py`
- Create: `internal/service/benchmark/data/arxiv_docs.json` (generated by the script)
- Create: `internal/service/benchmark/arxiv.go`
- Test: `internal/service/benchmark/arxiv_test.go`

**Schema** (`arxiv_docs.json` is a JSON array):

```json
[{"id": "<arxiv id>", "title": "<title>", "abstract": "<abstract text>", "category": "<primary arXiv category, e.g. cs.LG>"}]
```

- [ ] **Step 1: Write the curation script**

Create `tools/arxiv-curate/main.py`:

```python
#!/usr/bin/env python3
"""Curate a pool of real arXiv abstracts for the long-context quality haystack.

Reproducible: fetches the arXiv abstracts dataset from HuggingFace, filters to
abstracts of a usable length, samples a fixed-seed subset evenly across primary
categories, and emits the model-loader ArxivDoc schema.

Reproduce:  python3 tools/arxiv-curate/main.py --count 80 \
    --out internal/service/benchmark/data/arxiv_docs.json

NOTE for the implementer: confirm the dataset id and field names below against
the live dataset before running (HF dataset schemas drift). If you must change
the dataset or fields, update DATASET / the field access AND this docstring, and
pin DATASET_REVISION to the resolved commit so the output is reproducible.
"""
import argparse
import json
import random
import sys
from collections import defaultdict

from datasets import load_dataset

SEED = 20260523
DATASET = "gfissore/arxiv-abstracts-2021"
DATASET_REVISION = "main"  # pin to a commit hash after first successful fetch
MIN_ABSTRACT_CHARS = 600
MAX_ABSTRACT_CHARS = 1600


def primary_category(row) -> str:
    cats = row.get("categories") or ""
    if isinstance(cats, list):
        cats = cats[0] if cats else ""
    return (cats.split() or [""])[0].split(".")[0] or "misc"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--count", type=int, default=80)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    ds = load_dataset(DATASET, split="train", revision=DATASET_REVISION, streaming=True)
    by_cat = defaultdict(list)
    for row in ds:
        abstract = (row.get("abstract") or "").strip().replace("\n", " ")
        if not (MIN_ABSTRACT_CHARS <= len(abstract) <= MAX_ABSTRACT_CHARS):
            continue
        cat = primary_category(row)
        by_cat[cat].append({
            "id": str(row.get("id") or row.get("arxiv_id") or len(by_cat[cat])),
            "title": (row.get("title") or "").strip().replace("\n", " "),
            "abstract": abstract,
            "category": cat,
        })
        # bound memory: keep at most a few hundred per category while streaming
        if sum(len(v) for v in by_cat.values()) >= 4000:
            break

    cats = sorted(by_cat)
    if not cats:
        print("no abstracts matched the length filter", file=sys.stderr)
        return 1
    rng = random.Random(SEED)
    per = max(1, args.count // len(cats))
    picked = []
    for c in cats:
        pool = sorted(by_cat[c], key=lambda d: d["id"])
        rng.shuffle(pool)
        picked.extend(pool[:per])
    rng.shuffle(picked)
    picked = picked[: args.count]
    picked.sort(key=lambda d: d["id"])

    with open(args.out, "w", encoding="utf-8") as f:
        json.dump(picked, f, indent=2, ensure_ascii=False)
        f.write("\n")
    print(f"wrote {len(picked)} abstracts across {len(cats)} categories", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
```

- [ ] **Step 2: Generate the dataset**

Run: `python3 tools/arxiv-curate/main.py --count 80 --out internal/service/benchmark/data/arxiv_docs.json`
Expected: writes ~80 abstracts; stderr prints the count. Run it **twice** and confirm the output file is byte-for-byte identical (`git diff --stat` shows no change on the second run). If the dataset id/fields differ from the script, adapt the script, re-pin `DATASET_REVISION`, and update the docstring — then re-verify reproducibility.

Sanity-check the file size stays well under the 2 MB embed budget: `wc -c internal/service/benchmark/data/arxiv_docs.json` (expect ~150 KB).

- [ ] **Step 3: Write the failing loader test**

Create `internal/service/benchmark/arxiv_test.go`:

```go
package benchmark

import "testing"

func TestLoadArxivDocs(t *testing.T) {
	docs, err := loadArxivDocs()
	if err != nil {
		t.Fatalf("loadArxivDocs: %v", err)
	}
	if len(docs) < 20 {
		t.Fatalf("want >= 20 abstracts, got %d", len(docs))
	}
	for i, d := range docs {
		if d.Abstract == "" || d.Title == "" {
			t.Errorf("doc %d (%s) has empty title/abstract", i, d.ID)
		}
	}
}
```

Run: `go test ./internal/service/benchmark/ -run TestLoadArxivDocs -v` → FAIL (`loadArxivDocs` undefined).

- [ ] **Step 4: Write the loader**

Create `internal/service/benchmark/arxiv.go`:

```go
package benchmark

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed data/arxiv_docs.json
var arxivDataset []byte

// ArxivDoc is one real arXiv abstract used as realistic filler ("quality
// haystack") for the long-context needle probe.
type ArxivDoc struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Abstract string `json:"abstract"`
	Category string `json:"category"`
}

// loadArxivDocs decodes the embedded curated arXiv abstract pool.
func loadArxivDocs() ([]ArxivDoc, error) {
	var ds []ArxivDoc
	if err := json.Unmarshal(arxivDataset, &ds); err != nil {
		return nil, fmt.Errorf("decode arxiv docs: %w", err)
	}
	if len(ds) == 0 {
		return nil, fmt.Errorf("arxiv docs dataset is empty")
	}
	return ds, nil
}
```

- [ ] **Step 5: Run tests; expect pass**

Run: `go test ./internal/service/benchmark/ -run TestLoadArxivDocs -v` → PASS.

- [ ] **Step 6: Commit**

```bash
git add tools/arxiv-curate/main.py internal/service/benchmark/data/arxiv_docs.json internal/service/benchmark/arxiv.go internal/service/benchmark/arxiv_test.go
git commit -m "feat(benchmark): embed curated real arXiv abstract pool

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Quality haystack — arXiv-abstract filler for the long-context probe

**Files:**
- Modify: `internal/service/benchmark/runner.go` (add `arxivDocs` field + load; add `buildQualityHaystack`; switch `runLongContext` to it)
- Test: `internal/service/benchmark/longcontext_test.go`

The existing randomized-needle generation (`buildNeedles`), objective scoring (`scoreNeedles`), the prompt that asks for `MAGIC_<NAME>_NUMBER`, and `buildMultiNeedleHaystack` (kept as the doc-less fallback) all stay. Only the filler text the needles are buried in changes: from repetitive pseudo-Python to real arXiv abstracts.

- [ ] **Step 1: Write the failing test**

In `longcontext_test.go`, add:

```go
func TestBuildQualityHaystack_EmbedsNeedlesAmongAbstracts(t *testing.T) {
	docs := []ArxivDoc{
		{ID: "1", Title: "On Sparse Attention", Abstract: "We study sparse attention mechanisms for long sequences. " + strings.Repeat("Empirical results show consistent gains. ", 30)},
		{ID: "2", Title: "Quantization Survey", Abstract: "A survey of post-training quantization for transformers. " + strings.Repeat("We compare many schemes carefully. ", 30)},
	}
	needles := []needle{{label: "alpha", value: "Reykjavik-1234"}, {label: "beta", value: "Oslo-5678"}, {label: "gamma", value: "Lima-9012"}}
	hay := buildQualityHaystack(docs, 4000, needles)
	for _, n := range needles {
		if !strings.Contains(hay, n.value) {
			t.Errorf("haystack missing needle %q", n.value)
		}
	}
	if !strings.Contains(hay, "sparse attention") && !strings.Contains(hay, "Sparse Attention") {
		t.Error("haystack should contain real abstract text as filler")
	}
}

func TestBuildQualityHaystack_FallsBackWhenNoDocs(t *testing.T) {
	needles := []needle{{label: "alpha", value: "A1"}, {label: "beta", value: "B2"}, {label: "gamma", value: "C3"}}
	hay := buildQualityHaystack(nil, 4000, needles)
	for _, n := range needles {
		if !strings.Contains(hay, n.value) {
			t.Errorf("fallback haystack missing needle %q", n.value)
		}
	}
}
```

Run: `go test ./internal/service/benchmark/ -run TestBuildQualityHaystack -v` → FAIL (`buildQualityHaystack` undefined).

- [ ] **Step 2: Add `buildQualityHaystack`**

In `runner.go`, directly after `buildMultiNeedleHaystack`, add:

```go
// buildQualityHaystack generates ~targetTokens of realistic filler from real
// arXiv abstracts with the needles planted at ~25/50/75% depth as constant
// definitions the model must recover. Falls back to the pseudo-code haystack
// when no abstracts are available.
func buildQualityHaystack(docs []ArxivDoc, targetTokens int, needles []needle) string {
	if len(docs) == 0 {
		return buildMultiNeedleHaystack(targetTokens, needles)
	}
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
		d := docs[i%len(docs)]
		fmt.Fprintf(&b, "\n# === PAPER %s [%s] ===\n## %s\n%s\n", d.ID, d.Category, d.Title, d.Abstract)
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

- [ ] **Step 3: Wire the Runner field + loader**

In the `Runner` struct (`runner.go:120` block), add after `mmluProblems`:

```go
	arxivDocs       []ArxivDoc           // real arXiv abstracts used as long-context quality filler
```

In `NewRunner`, after the `mmluProblems` load block, add:

```go
	arxivDocs, err := loadArxivDocs()
	if err != nil {
		return nil, err
	}
```

In the final `return &Runner{...}`, add `arxivDocs: arxivDocs,`.

In `runLongContext`, change the haystack line from:

```go
	haystack := buildMultiNeedleHaystack(targetTokens, needles)
```

to:

```go
	haystack := buildQualityHaystack(r.arxivDocs, targetTokens, needles)
```

- [ ] **Step 4: Run tests; expect pass**

Run: `go test ./internal/service/benchmark/ -run 'Haystack|Needle|LongContext' -v` → PASS (all existing needle tests still pass; the doc-less `buildMultiNeedleHaystack` test is unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/service/benchmark/runner.go internal/service/benchmark/longcontext_test.go
git commit -m "feat(benchmark): use real arXiv abstracts as long-context haystack filler

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: RagasBench dataset (synthetic) + loader

**Files:**
- Create: `tools/ragas-curate/main.py`
- Create: `internal/service/benchmark/data/ragas_curated.json`
- Create (struct + loader live in the mode file in Task 5; here only the test data + a temporary loader test): add loader to `ragasbench.go` is done in Task 5. **This task produces only the dataset + script.**

**Schema** (`ragas_curated.json` is a JSON array):

```json
[{"id": "rag-01", "documents": ["doc text 1", "doc text 2"], "question": "...", "groundTruth": "...", "expectedContext": "the doc snippet that answers it"}]
```

- [ ] **Step 1: Write the synthetic curation script**

Create `tools/ragas-curate/main.py` (authored, deterministic — no network):

```python
#!/usr/bin/env python3
"""Emit the curated RagasBench dataset as JSON.

Synthetic, authored for the model-loader RAG-quality benchmark: 20 scenarios.
Each scenario gives the model a small set of documents and a question whose
answer is fully contained in exactly one document. A grader later scores
faithfulness (no claims beyond the docs), answer relevancy, and context
precision against groundTruth / expectedContext.

Reproduce:  python3 tools/ragas-curate/main.py > \
    internal/service/benchmark/data/ragas_curated.json
"""
import json
import sys

# (id, [documents], question, ground_truth, expected_context)
SCENARIOS = [
    ("rag-01",
     ["The Aldebaran rover landed in the Vallis Marineris region on March 3, 2031.",
      "The mission was funded jointly by three national space agencies.",
      "Its primary instrument is a deep-drilling spectrometer."],
     "On what date did the Aldebaran rover land?",
     "March 3, 2031.",
     "The Aldebaran rover landed in the Vallis Marineris region on March 3, 2031."),
    ("rag-02",
     ["Quartzine is a fictional alloy with a melting point of 2,140 degrees Celsius.",
      "It is prized for corrosion resistance in marine turbines."],
     "What is the melting point of quartzine?",
     "2,140 degrees Celsius.",
     "Quartzine is a fictional alloy with a melting point of 2,140 degrees Celsius."),
    ("rag-03",
     ["The Béker Prize in linguistics was first awarded in 1962.",
      "It carries a cash award and a two-year research fellowship."],
     "In what year was the Béker Prize first awarded?",
     "1962.",
     "The Béker Prize in linguistics was first awarded in 1962."),
    ("rag-04",
     ["The novel 'Tideglass' was written by Marin Holloway over seven years.",
      "It is set in a drowned coastal city named Sarn."],
     "Who wrote the novel 'Tideglass'?",
     "Marin Holloway.",
     "The novel 'Tideglass' was written by Marin Holloway over seven years."),
    ("rag-05",
     ["Project Halberd reduced packet loss by 38% in field trials.",
      "The trials ran across twelve metropolitan test sites."],
     "By how much did Project Halberd reduce packet loss?",
     "38%.",
     "Project Halberd reduced packet loss by 38% in field trials."),
    ("rag-06",
     ["The Sundara Festival is held every third year in the city of Pell.",
      "It centers on traditional kite-making competitions."],
     "How often is the Sundara Festival held?",
     "Every third year.",
     "The Sundara Festival is held every third year in the city of Pell."),
    ("rag-07",
     ["Enzyme XR-9 catalyzes the breakdown of microplastics at room temperature.",
      "It was isolated from a deep-sea bacterium in 2029."],
     "What does enzyme XR-9 break down?",
     "Microplastics.",
     "Enzyme XR-9 catalyzes the breakdown of microplastics at room temperature."),
    ("rag-08",
     ["The Korrin Bridge spans 1,480 meters across the Vasser Strait.",
      "It opened to traffic after nine years of construction."],
     "How long is the Korrin Bridge?",
     "1,480 meters.",
     "The Korrin Bridge spans 1,480 meters across the Vasser Strait."),
    ("rag-09",
     ["Dr. Imani Sefu chairs the Coastal Resilience Council.",
      "The council advises on flood-defense policy for twelve provinces."],
     "Who chairs the Coastal Resilience Council?",
     "Dr. Imani Sefu.",
     "Dr. Imani Sefu chairs the Coastal Resilience Council."),
    ("rag-10",
     ["The game 'Latticefall' sold 4.2 million copies in its first month.",
      "It was developed by a studio of just nine people."],
     "How many copies did 'Latticefall' sell in its first month?",
     "4.2 million.",
     "The game 'Latticefall' sold 4.2 million copies in its first month."),
    ("rag-11",
     ["Vantium decays with a half-life of 19 days.",
      "It is used as a tracer in hydrology studies."],
     "What is the half-life of vantium?",
     "19 days.",
     "Vantium decays with a half-life of 19 days."),
    ("rag-12",
     ["The treaty of Oren Bay was signed by five coastal nations in 1998.",
      "It established shared fishing quotas."],
     "How many nations signed the treaty of Oren Bay?",
     "Five.",
     "The treaty of Oren Bay was signed by five coastal nations in 1998."),
    ("rag-13",
     ["The Marrow Line subway carries 600,000 riders daily.",
      "It connects the harbor district to the northern suburbs."],
     "How many riders does the Marrow Line carry daily?",
     "600,000.",
     "The Marrow Line subway carries 600,000 riders daily."),
    ("rag-14",
     ["Solenne coffee is grown only on the slopes of Mount Yaru.",
      "Its beans are harvested entirely by hand."],
     "Where is Solenne coffee grown?",
     "On the slopes of Mount Yaru.",
     "Solenne coffee is grown only on the slopes of Mount Yaru."),
    ("rag-15",
     ["The Pareli telescope has a primary mirror 11 meters in diameter.",
      "It observes primarily in the mid-infrared band."],
     "How large is the Pareli telescope's primary mirror?",
     "11 meters in diameter.",
     "The Pareli telescope has a primary mirror 11 meters in diameter."),
    ("rag-16",
     ["The Wexler protocol requires three independent confirmations before release.",
      "It was adopted by the consortium in 2025."],
     "How many independent confirmations does the Wexler protocol require?",
     "Three.",
     "The Wexler protocol requires three independent confirmations before release."),
    ("rag-17",
     ["The Talin reservoir holds 320 million cubic meters of water.",
      "It supplies drinking water to two cities."],
     "How much water does the Talin reservoir hold?",
     "320 million cubic meters.",
     "The Talin reservoir holds 320 million cubic meters of water."),
    ("rag-18",
     ["Composer Nadia Brevik wrote the symphony 'Ashlight' in 2014.",
      "It premiered with a choir of two hundred voices."],
     "Who composed 'Ashlight'?",
     "Nadia Brevik.",
     "Composer Nadia Brevik wrote the symphony 'Ashlight' in 2014."),
    ("rag-19",
     ["The Verda standard mandates a maximum latency of 50 milliseconds.",
      "It applies to all real-time control systems in the network."],
     "What maximum latency does the Verda standard mandate?",
     "50 milliseconds.",
     "The Verda standard mandates a maximum latency of 50 milliseconds."),
    ("rag-20",
     ["The Hollow Pines trail is 27 kilometers long and rated difficult.",
      "It gains 1,900 meters of elevation."],
     "How long is the Hollow Pines trail?",
     "27 kilometers.",
     "The Hollow Pines trail is 27 kilometers long and rated difficult."),
]


def main():
    items = [{
        "id": pid,
        "documents": docs,
        "question": q,
        "groundTruth": gt,
        "expectedContext": ctx,
    } for pid, docs, q, gt, ctx in SCENARIOS]
    json.dump(items, sys.stdout, indent=2, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Generate the dataset**

Run: `python3 tools/ragas-curate/main.py > internal/service/benchmark/data/ragas_curated.json`
Expected: a 20-element JSON array. Re-run and confirm it's byte-for-byte identical.

- [ ] **Step 3: Commit (data + script only)**

```bash
git add tools/ragas-curate/main.py internal/service/benchmark/data/ragas_curated.json
git commit -m "feat(benchmark): add synthetic RagasBench dataset + curation script

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: RagasBench mode (`ragasbench.go`)

**Files:**
- Create: `internal/service/benchmark/ragasbench.go`
- Modify: `internal/service/benchmark/runner.go` (add `ragasProblems` field + load)
- Test: `internal/service/benchmark/ragasbench_test.go`

**Behavior:** for each scenario, build a prompt = the documents + the question, get the model-under-test's answer, then call the grader **three times** (faithfulness, relevancy, precision). Per-problem `Score` = mean of the three; `Resolved` = `Score >= ragasPassThreshold`. Encode the three sub-scores in `Detail` as `faithfulness=0.80 relevancy=0.90 precision=0.75 (self)`. `Finalize` parses every problem's `Detail`, averages each criterion, and sets `RagasFaithfulness`/`RagasRelevancy`/`RagasPrecision`.

- [ ] **Step 1: Write failing tests for the pure helpers**

Create `internal/service/benchmark/ragasbench_test.go`:

```go
package benchmark

import (
	"math"
	"strings"
	"testing"
)

func TestBuildRagasPrompt_IncludesDocsAndQuestion(t *testing.T) {
	p := RagasProblem{
		Documents: []string{"Doc A says X.", "Doc B says Y."},
		Question:  "What does Doc A say?",
	}
	got := buildRagasPrompt(p)
	for _, want := range []string{"Doc A says X.", "Doc B says Y.", "What does Doc A say?"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q\n%s", want, got)
		}
	}
}

func TestRagasDetail_RoundTrip(t *testing.T) {
	d := ragasDetail(0.8, 0.9, 0.75, "self")
	f, r, p, ok := parseRagasScores(d)
	if !ok {
		t.Fatalf("parseRagasScores failed on %q", d)
	}
	if math.Abs(f-0.8) > 1e-9 || math.Abs(r-0.9) > 1e-9 || math.Abs(p-0.75) > 1e-9 {
		t.Errorf("round-trip mismatch: got f=%v r=%v p=%v", f, r, p)
	}
}

func TestRagasFinalize_AveragesCriteria(t *testing.T) {
	problems := []ProblemResult{
		{Detail: ragasDetail(1.0, 0.8, 0.6, "self")},
		{Detail: ragasDetail(0.0, 0.4, 0.4, "self")},
	}
	var agg Aggregate
	ragasHandler{}.Finalize(&agg, problems)
	if math.Abs(agg.RagasFaithfulness-0.5) > 1e-9 {
		t.Errorf("RagasFaithfulness = %v, want 0.5", agg.RagasFaithfulness)
	}
	if math.Abs(agg.RagasRelevancy-0.6) > 1e-9 {
		t.Errorf("RagasRelevancy = %v, want 0.6", agg.RagasRelevancy)
	}
	if math.Abs(agg.RagasPrecision-0.5) > 1e-9 {
		t.Errorf("RagasPrecision = %v, want 0.5", agg.RagasPrecision)
	}
}
```

Run: `go test ./internal/service/benchmark/ -run TestRagas -v` → FAIL (undefined symbols).

- [ ] **Step 2: Write `ragasbench.go`**

```go
package benchmark

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

//go:embed data/ragas_curated.json
var ragasDataset []byte

// RagasProblem is one synthetic RAG scenario: the model must answer Question
// using only Documents; the grader scores the answer against GroundTruth /
// ExpectedContext.
type RagasProblem struct {
	ID              string   `json:"id"`
	Documents       []string `json:"documents"`
	Question        string   `json:"question"`
	GroundTruth     string   `json:"groundTruth"`
	ExpectedContext string   `json:"expectedContext"`
}

// loadRagasProblems decodes the embedded synthetic RAG scenarios.
func loadRagasProblems() ([]RagasProblem, error) {
	var ps []RagasProblem
	if err := json.Unmarshal(ragasDataset, &ps); err != nil {
		return nil, fmt.Errorf("decode ragas dataset: %w", err)
	}
	if len(ps) == 0 {
		return nil, fmt.Errorf("ragas dataset is empty")
	}
	return ps, nil
}

const ragasPassThreshold = 0.7

// buildRagasPrompt renders the documents as a numbered context block followed
// by the question and a strict instruction to answer only from the documents.
func buildRagasPrompt(p RagasProblem) string {
	var b strings.Builder
	b.WriteString("Answer the question using ONLY the documents below. " +
		"If the documents do not contain the answer, say so. Do not add outside facts.\n\n")
	for i, d := range p.Documents {
		fmt.Fprintf(&b, "[Doc %d] %s\n", i+1, d)
	}
	fmt.Fprintf(&b, "\nQuestion: %s\n", p.Question)
	return b.String()
}

func ragasDetail(faith, rel, prec float64, judgedBy string) string {
	return fmt.Sprintf("faithfulness=%.2f relevancy=%.2f precision=%.2f (%s)", faith, rel, prec, judgedBy)
}

var ragasScoreRe = regexp.MustCompile(`faithfulness=([0-9.]+) relevancy=([0-9.]+) precision=([0-9.]+)`)

// parseRagasScores extracts the three sub-scores from a ragas Detail string.
func parseRagasScores(detail string) (faith, rel, prec float64, ok bool) {
	m := ragasScoreRe.FindStringSubmatch(detail)
	if m == nil {
		return 0, 0, 0, false
	}
	faith, _ = strconv.ParseFloat(m[1], 64)
	rel, _ = strconv.ParseFloat(m[2], 64)
	prec, _ = strconv.ParseFloat(m[3], 64)
	return faith, rel, prec, true
}

// runRagas answers one scenario and grades it on three criteria.
func (r *Runner) runRagas(ctx context.Context, base, model string, g grader, p RagasProblem) (ProblemResult, ProblemTranscript) {
	name := truncateQuestion(p.Question)
	res := ProblemResult{ProblemID: p.ID, ProblemName: name}
	tr := ProblemTranscript{ProblemID: p.ID, ProblemName: name}

	docs := strings.Join(p.Documents, "\n")
	reqCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
		Model:       model,
		Temperature: 0,
		MaxTokens:   r.cfg.MaxTokens,
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a careful retrieval-augmented assistant. Answer only from the provided documents."},
			{Role: "user", Content: buildRagasPrompt(p)},
		},
	})
	cancel()
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

	grade := func(criterion, guidance, gctx, question string) gradeResult {
		gCtx, gCancel := context.WithTimeout(ctx, r.cfg.Timeout)
		defer gCancel()
		gr, gErr := g.Grade(gCtx, gradeRequest{
			Criterion: criterion, Guidance: guidance,
			Question: question, Context: gctx, Answer: comp.Content,
		})
		if gErr != nil {
			tr.JudgeRaw = append(tr.JudgeRaw, criterion+": "+gErr.Error())
			return gradeResult{}
		}
		tr.JudgeRaw = append(tr.JudgeRaw, gr.Raw)
		return gr
	}

	faith := grade("faithfulness", "every claim is supported by the documents; no invented facts", docs, p.Question)
	rel := grade("answer relevancy", "the answer directly and completely addresses the question", "", p.Question)
	prec := grade("context precision", "the answer matches the ground-truth answer drawn from the relevant document",
		"Ground truth: "+p.GroundTruth+"\nRelevant document: "+p.ExpectedContext, p.Question)

	mean := (faith.Score + rel.Score + prec.Score) / 3
	res.Score = mean
	res.Resolved = mean >= ragasPassThreshold
	judgedBy := faith.JudgedBy
	if judgedBy == "" {
		judgedBy = "self"
	}
	res.Detail = ragasDetail(faith.Score, rel.Score, prec.Score, judgedBy)
	return res, tr
}

type ragasHandler struct{}

func (ragasHandler) Mode() Mode                      { return ModeRagasBench }
func (ragasHandler) Category() Category              { return CatQuality }
func (ragasHandler) Count(r *Runner) int             { return len(r.ragasProblems) }
func (ragasHandler) Prepare(*Runner) (Scorer, error) { return nil, nil }

// Finalize averages each grader criterion across all answered problems.
func (ragasHandler) Finalize(agg *Aggregate, problems []ProblemResult) {
	var sf, sr, sp float64
	n := 0
	for _, pr := range problems {
		f, r, p, ok := parseRagasScores(pr.Detail)
		if !ok {
			continue
		}
		sf += f
		sr += r
		sp += p
		n++
	}
	if n == 0 {
		return
	}
	agg.RagasFaithfulness = sf / float64(n)
	agg.RagasRelevancy = sr / float64(n)
	agg.RagasPrecision = sp / float64(n)
}

func (ragasHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	g := r.graderFor(base, model)
	var results []ProblemResult
	var transcripts []ProblemTranscript
	for i, p := range r.ragasProblems {
		select {
		case <-ctx.Done():
			return results, transcripts, ctx.Err()
		default:
		}
		send(progress, Progress{Index: i + 1, Total: len(r.ragasProblems), ProblemID: p.ID, ProblemName: truncateQuestion(p.Question), Phase: "infer"})
		pr, tr := r.runRagas(ctx, base, model, g, p)
		results = append(results, pr)
		if r.cfg.SaveTranscripts {
			transcripts = append(transcripts, tr)
		}
		send(progress, Progress{Index: i + 1, Total: len(r.ragasProblems), ProblemID: p.ID, ProblemName: truncateQuestion(p.Question), Phase: "score"})
	}
	return results, transcripts, nil
}

func init() {
	registerHandler(ragasHandler{})
}
```

- [ ] **Step 3: Wire the Runner field + loader**

In the `Runner` struct, add after `arxivDocs`:

```go
	ragasProblems   []RagasProblem       // synthetic RAG scenarios for ModeRagasBench
```

In `NewRunner`, after the `arxivDocs` load, add:

```go
	ragasProblems, err := loadRagasProblems()
	if err != nil {
		return nil, err
	}
```

In the final `return &Runner{...}`, add `ragasProblems: ragasProblems,`.

- [ ] **Step 4: Add a grader-integration test (httptest stub)**

Append to `ragasbench_test.go`:

```go
func TestRunRagas_GradesAgainstStubServer(t *testing.T) {
	// Stub OpenAI-shaped server: chat completions reply with the model answer;
	// the grader (same endpoint) replies with strict JSON verdicts. We return a
	// fixed grader verdict regardless of criterion to keep the test deterministic.
	srv := newRagasStubServer(t)
	defer srv.Close()

	r := &Runner{cfg: Config{MaxTokens: 64, Timeout: 5 * time.Second}}
	g := r.graderFor(srv.URL, "stub-model") // no external judge configured -> self
	p := RagasProblem{ID: "rag-x", Documents: []string{"The sky is blue."}, Question: "What color is the sky?", GroundTruth: "Blue.", ExpectedContext: "The sky is blue."}
	res, _ := r.runRagas(context.Background(), srv.URL, "stub-model", g, p)
	if res.Err != "" {
		t.Fatalf("unexpected err: %s", res.Err)
	}
	if _, _, _, ok := parseRagasScores(res.Detail); !ok {
		t.Fatalf("Detail not parseable: %q", res.Detail)
	}
}
```

Add `newRagasStubServer` — model the stub on the existing `judge_test.go`/`client_test.go` `httptest` pattern (a non-streaming `/v1/chat/completions` returning a `choices[0].message.content` of `{"score":0.9,"pass":true,"rationale":"ok"}`). Inspect `client_test.go` and `judge_test.go` for the exact response JSON shape `Complete` expects, and reuse it. Add the needed imports (`context`, `net/http`, `net/http/httptest`, `time`).

> Implementer note: if `Complete` requires streaming SSE, copy the SSE-emitting stub from `streamcancel_test.go`/`client_test.go` rather than a plain JSON body. Match whatever the existing passing tests use.

- [ ] **Step 5: Run tests; expect pass**

Run: `go test ./internal/service/benchmark/ -run TestRagas -v` and `go test ./internal/service/benchmark/ -run TestRunRagas -v` → PASS.
Then the full package: `go test ./internal/service/benchmark/` → PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/service/benchmark/ragasbench.go internal/service/benchmark/ragasbench_test.go internal/service/benchmark/runner.go
git commit -m "feat(benchmark): add grader-scored RagasBench mode

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: SummaryBench dataset (synthetic) + loader

**Files:**
- Create: `tools/summary-curate/main.py`
- Create: `internal/service/benchmark/data/summary_curated.json`

**Schema** (`summary_curated.json` is a JSON array of bundles):

```json
[{"id": "sum-01", "documents": ["doc 1", "...", "doc 10"], "facts": ["fact 1", "...", "fact 5"]}]
```

Each bundle has ~10 short documents; `facts` lists 5 specific facts (each stated verbatim in one of the documents) that a faithful summary must cover.

- [ ] **Step 1: Write the synthetic curation script**

Create `tools/summary-curate/main.py` (authored, deterministic):

```python
#!/usr/bin/env python3
"""Emit the curated SummaryBench dataset as JSON.

Synthetic, authored for the model-loader multi-document summarization-coherence
benchmark: 5 bundles, each with 10 short documents and 5 key facts a faithful
summary must mention. A grader scores coherence + fact coverage.

Reproduce:  python3 tools/summary-curate/main.py > \
    internal/service/benchmark/data/summary_curated.json
"""
import json
import sys

# Each bundle: (id, [10 docs], [5 key facts verbatim in the docs])
BUNDLES = [
    ("sum-01",
     ["The Merrowfield wind farm opened in 2028.",
      "It has 64 turbines arranged in four rows.",
      "Peak output is 410 megawatts.",
      "The site spans 90 square kilometers of moorland.",
      "Local birds were tracked for two years before approval.",
      "Maintenance is handled by a crew of 38 technicians.",
      "Power feeds three neighboring counties.",
      "The project cost 1.2 billion in public-private funding.",
      "A visitor center opened alongside the farm.",
      "Turbine blades are recycled at end of life."],
     ["The Merrowfield wind farm opened in 2028.",
      "It has 64 turbines arranged in four rows.",
      "Peak output is 410 megawatts.",
      "The project cost 1.2 billion in public-private funding.",
      "Power feeds three neighboring counties."]),
    ("sum-02",
     ["The Lirian Library holds 2.4 million volumes.",
      "It was founded in 1887 by a merchant guild.",
      "The reading room seats 300 scholars.",
      "A rare-manuscript wing was added in 1954.",
      "Annual visitors number around 500,000.",
      "Digitization began in 2010.",
      "The building survived a major fire in 1921.",
      "Membership is free to city residents.",
      "It houses a famous medieval atlas.",
      "The library employs 120 staff."],
     ["The Lirian Library holds 2.4 million volumes.",
      "It was founded in 1887 by a merchant guild.",
      "The building survived a major fire in 1921.",
      "Annual visitors number around 500,000.",
      "It houses a famous medieval atlas."]),
    ("sum-03",
     ["The Cassine River is 612 kilometers long.",
      "It flows through three countries.",
      "Its delta supports a large wetland reserve.",
      "Seasonal floods deposit fertile silt.",
      "Twelve bridges cross the river.",
      "A hydroelectric dam was built in 1976.",
      "The river is home to an endemic catfish.",
      "Cargo barges use the lower 200 kilometers.",
      "Water quality has improved since 2005.",
      "The source is a glacial lake."],
     ["The Cassine River is 612 kilometers long.",
      "It flows through three countries.",
      "A hydroelectric dam was built in 1976.",
      "The river is home to an endemic catfish.",
      "The source is a glacial lake."]),
    ("sum-04",
     ["The Orsel marathon attracts 45,000 runners.",
      "The course passes seven historic landmarks.",
      "It has been held annually since 1981.",
      "The record time is 2 hours and 4 minutes.",
      "Proceeds fund local youth sports.",
      "Water stations appear every 5 kilometers.",
      "The route is closed to traffic for 8 hours.",
      "Elite runners come from over 30 countries.",
      "A wheelchair division started in 1995.",
      "The finish line is in the central plaza."],
     ["The Orsel marathon attracts 45,000 runners.",
      "It has been held annually since 1981.",
      "The record time is 2 hours and 4 minutes.",
      "Proceeds fund local youth sports.",
      "A wheelchair division started in 1995."]),
    ("sum-05",
     ["The Veld telescope array has 36 dishes.",
      "Each dish is 25 meters wide.",
      "It studies radio emissions from distant galaxies.",
      "The array sits on a high desert plateau.",
      "Construction finished in 2022.",
      "Data is processed at an on-site supercomputer.",
      "It operates in partnership with six universities.",
      "Observing time is allocated by peer review.",
      "The site has near-zero radio interference.",
      "A future expansion to 60 dishes is planned."],
     ["The Veld telescope array has 36 dishes.",
      "Each dish is 25 meters wide.",
      "Construction finished in 2022.",
      "It operates in partnership with six universities.",
      "A future expansion to 60 dishes is planned."]),
]


def main():
    items = [{"id": bid, "documents": docs, "facts": facts} for bid, docs, facts in BUNDLES]
    json.dump(items, sys.stdout, indent=2, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Generate the dataset**

Run: `python3 tools/summary-curate/main.py > internal/service/benchmark/data/summary_curated.json`
Expected: a 5-element JSON array; re-run → identical.

- [ ] **Step 3: Commit (data + script only)**

```bash
git add tools/summary-curate/main.py internal/service/benchmark/data/summary_curated.json
git commit -m "feat(benchmark): add synthetic SummaryBench dataset + curation script

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: SummaryBench mode (`summarybench.go`)

**Files:**
- Create: `internal/service/benchmark/summarybench.go`
- Modify: `internal/service/benchmark/runner.go` (add `summaryProblems` field + load)
- Test: `internal/service/benchmark/summarybench_test.go`

**Behavior:** for each bundle, prompt the model to summarize the ~10 documents in a few sentences covering the key points. Two scores combine into the per-problem `Score`:
1. **Fact coverage** (objective): fraction of the 5 facts whose key terms appear in the summary — computed locally, no grader.
2. **Coherence** (graded): one grader call scoring whether the summary is coherent and faithful to the documents.

Per-problem `Score` = mean(coverage, coherence); `Resolved` = `Score >= summaryPassThreshold`. `Detail` = `coverage=4/5 coherence=0.85 (self)`. `Finalize` sets `SummaryCoherence` = mean per-problem `Score` across answered problems.

- [ ] **Step 1: Write failing tests for the pure helpers**

Create `internal/service/benchmark/summarybench_test.go`:

```go
package benchmark

import (
	"math"
	"strings"
	"testing"
)

func TestFactCoverage_CountsMentionedFacts(t *testing.T) {
	facts := []string{
		"The Merrowfield wind farm opened in 2028.",
		"It has 64 turbines arranged in four rows.",
		"Peak output is 410 megawatts.",
	}
	summary := "Merrowfield opened in 2028 with 64 turbines; peak output reaches 410 megawatts."
	got := factCoverage(summary, facts)
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("factCoverage = %v, want 1.0", got)
	}
}

func TestFactCoverage_PartialMiss(t *testing.T) {
	facts := []string{"Output is 410 megawatts.", "Cost was 1.2 billion."}
	summary := "The output is 410 megawatts."
	got := factCoverage(summary, facts)
	if math.Abs(got-0.5) > 1e-9 {
		t.Errorf("factCoverage = %v, want 0.5", got)
	}
}

func TestSummaryDetail_RoundTrip(t *testing.T) {
	d := summaryDetail(4, 5, 0.85, "self")
	cov, coh, ok := parseSummaryScores(d)
	if !ok {
		t.Fatalf("parse failed on %q", d)
	}
	if math.Abs(cov-0.8) > 1e-9 || math.Abs(coh-0.85) > 1e-9 {
		t.Errorf("round-trip: cov=%v coh=%v", cov, coh)
	}
}

func TestBuildSummaryPrompt_IncludesAllDocs(t *testing.T) {
	p := SummaryProblem{Documents: []string{"AAA.", "BBB.", "CCC."}}
	got := buildSummaryPrompt(p)
	for _, d := range p.Documents {
		if !strings.Contains(got, d) {
			t.Errorf("prompt missing %q", d)
		}
	}
}
```

Run: `go test ./internal/service/benchmark/ -run 'Summary|FactCoverage' -v` → FAIL.

- [ ] **Step 2: Write `summarybench.go`**

```go
package benchmark

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

//go:embed data/summary_curated.json
var summaryDataset []byte

// SummaryProblem is one multi-document summarization bundle: Documents are
// summarized; Facts are the key points a faithful summary must cover.
type SummaryProblem struct {
	ID        string   `json:"id"`
	Documents []string `json:"documents"`
	Facts     []string `json:"facts"`
}

func loadSummaryProblems() ([]SummaryProblem, error) {
	var ps []SummaryProblem
	if err := json.Unmarshal(summaryDataset, &ps); err != nil {
		return nil, fmt.Errorf("decode summary dataset: %w", err)
	}
	if len(ps) == 0 {
		return nil, fmt.Errorf("summary dataset is empty")
	}
	return ps, nil
}

const summaryPassThreshold = 0.7

// factTermRe extracts lowercase alphanumeric terms (>=4 chars) used as the
// "key terms" of a fact for coverage matching.
var factTermRe = regexp.MustCompile(`[a-z0-9]{4,}`)

// factCoverage returns the fraction of facts whose salient terms mostly appear
// in the summary. A fact counts as covered when >= 60% of its salient terms are
// present (case-insensitive), tolerating paraphrase while still requiring the
// concrete entities/numbers.
func factCoverage(summary string, facts []string) float64 {
	if len(facts) == 0 {
		return 0
	}
	low := strings.ToLower(summary)
	covered := 0
	for _, f := range facts {
		terms := factTermRe.FindAllString(strings.ToLower(f), -1)
		if len(terms) == 0 {
			continue
		}
		hit := 0
		for _, t := range terms {
			if strings.Contains(low, t) {
				hit++
			}
		}
		if float64(hit)/float64(len(terms)) >= 0.6 {
			covered++
		}
	}
	return float64(covered) / float64(len(facts))
}

func buildSummaryPrompt(p SummaryProblem) string {
	var b strings.Builder
	b.WriteString("Summarize the following documents in 3-5 sentences. " +
		"Cover the most important facts accurately; do not invent anything.\n\n")
	for i, d := range p.Documents {
		fmt.Fprintf(&b, "[Doc %d] %s\n", i+1, d)
	}
	return b.String()
}

func summaryDetail(coveredFacts, totalFacts int, coherence float64, judgedBy string) string {
	return fmt.Sprintf("coverage=%d/%d coherence=%.2f (%s)", coveredFacts, totalFacts, coherence, judgedBy)
}

var summaryScoreRe = regexp.MustCompile(`coverage=(\d+)/(\d+) coherence=([0-9.]+)`)

func parseSummaryScores(detail string) (coverage, coherence float64, ok bool) {
	m := summaryScoreRe.FindStringSubmatch(detail)
	if m == nil {
		return 0, 0, false
	}
	num, _ := strconv.ParseFloat(m[1], 64)
	den, _ := strconv.ParseFloat(m[2], 64)
	coherence, _ = strconv.ParseFloat(m[3], 64)
	if den > 0 {
		coverage = num / den
	}
	return coverage, coherence, true
}

func (r *Runner) runSummary(ctx context.Context, base, model string, g grader, p SummaryProblem) (ProblemResult, ProblemTranscript) {
	name := "summary " + p.ID
	res := ProblemResult{ProblemID: p.ID, ProblemName: name}
	tr := ProblemTranscript{ProblemID: p.ID, ProblemName: name}

	reqCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
		Model:       model,
		Temperature: 0,
		MaxTokens:   r.cfg.MaxTokens,
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a precise summarization assistant."},
			{Role: "user", Content: buildSummaryPrompt(p)},
		},
	})
	cancel()
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

	coverage := factCoverage(comp.Content, p.Facts)
	covered := int(coverage*float64(len(p.Facts)) + 0.5)

	gCtx, gCancel := context.WithTimeout(ctx, r.cfg.Timeout)
	gr, gErr := g.Grade(gCtx, gradeRequest{
		Criterion: "coherence",
		Guidance:  "the summary reads as a coherent whole and faithfully reflects the documents without contradictions or invented facts",
		Context:   strings.Join(p.Documents, "\n"),
		Answer:    comp.Content,
	})
	gCancel()
	coherence := 0.0
	judgedBy := "self"
	if gErr != nil {
		tr.JudgeRaw = append(tr.JudgeRaw, "coherence: "+gErr.Error())
	} else {
		coherence = gr.Score
		if gr.JudgedBy != "" {
			judgedBy = gr.JudgedBy
		}
		tr.JudgeRaw = append(tr.JudgeRaw, gr.Raw)
	}

	res.Score = (coverage + coherence) / 2
	res.Resolved = res.Score >= summaryPassThreshold
	res.Detail = summaryDetail(covered, len(p.Facts), coherence, judgedBy)
	return res, tr
}

type summaryHandler struct{}

func (summaryHandler) Mode() Mode                      { return ModeSummaryBench }
func (summaryHandler) Category() Category              { return CatQuality }
func (summaryHandler) Count(r *Runner) int             { return len(r.summaryProblems) }
func (summaryHandler) Prepare(*Runner) (Scorer, error) { return nil, nil }

// Finalize sets SummaryCoherence to the mean per-problem score (coverage +
// coherence) across answered problems.
func (summaryHandler) Finalize(agg *Aggregate, problems []ProblemResult) {
	var sum float64
	n := 0
	for _, pr := range problems {
		if _, _, ok := parseSummaryScores(pr.Detail); !ok {
			continue
		}
		sum += pr.Score
		n++
	}
	if n == 0 {
		return
	}
	agg.SummaryCoherence = sum / float64(n)
}

func (summaryHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	g := r.graderFor(base, model)
	var results []ProblemResult
	var transcripts []ProblemTranscript
	for i, p := range r.summaryProblems {
		select {
		case <-ctx.Done():
			return results, transcripts, ctx.Err()
		default:
		}
		send(progress, Progress{Index: i + 1, Total: len(r.summaryProblems), ProblemID: p.ID, ProblemName: "summary " + p.ID, Phase: "infer"})
		pr, tr := r.runSummary(ctx, base, model, g, p)
		results = append(results, pr)
		if r.cfg.SaveTranscripts {
			transcripts = append(transcripts, tr)
		}
		send(progress, Progress{Index: i + 1, Total: len(r.summaryProblems), ProblemID: p.ID, ProblemName: "summary " + p.ID, Phase: "score"})
	}
	return results, transcripts, nil
}

func init() {
	registerHandler(summaryHandler{})
}
```

- [ ] **Step 3: Wire the Runner field + loader**

In the `Runner` struct, add after `ragasProblems`:

```go
	summaryProblems []SummaryProblem     // synthetic multi-doc bundles for ModeSummaryBench
```

In `NewRunner`, after the `ragasProblems` load:

```go
	summaryProblems, err := loadSummaryProblems()
	if err != nil {
		return nil, err
	}
```

In the final `return &Runner{...}`, add `summaryProblems: summaryProblems,`.

- [ ] **Step 4: Add a Finalize test**

Append to `summarybench_test.go`:

```go
func TestSummaryFinalize_MeanScore(t *testing.T) {
	problems := []ProblemResult{
		{Score: 0.8, Detail: summaryDetail(4, 5, 0.9, "self")},
		{Score: 0.4, Detail: summaryDetail(2, 5, 0.5, "self")},
		{Score: 0.0, Detail: "no scores here"}, // ignored (unparseable)
	}
	var agg Aggregate
	summaryHandler{}.Finalize(&agg, problems)
	if math.Abs(agg.SummaryCoherence-0.6) > 1e-9 {
		t.Errorf("SummaryCoherence = %v, want 0.6", agg.SummaryCoherence)
	}
}
```

- [ ] **Step 5: Run tests; expect pass**

Run: `go test ./internal/service/benchmark/ -run 'Summary|FactCoverage' -v` → PASS.
Then full package: `go test ./internal/service/benchmark/` → PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/service/benchmark/summarybench.go internal/service/benchmark/summarybench_test.go internal/service/benchmark/runner.go
git commit -m "feat(benchmark): add multi-doc SummaryBench coherence mode

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: UI — register modes + detail lines (`internal/ui/pages/benchmark.go`)

**Files:**
- Modify: `internal/ui/pages/benchmark.go` (`benchModes`, `descs`, `modeDetailLines`)
- Test: `internal/ui/pages/benchmark_test.go` and `internal/ui/pages/benchmark_detail_test.go`

Both new modes are category `Quality`, so they slot into the existing Quality group at the top of `benchModes` (keep same-category modes contiguous).

- [ ] **Step 1: Write failing tests**

In `benchmark_test.go`, add (adapt to the file's existing helper/assert style — inspect it first):

```go
func TestBenchModes_IncludesPhase3(t *testing.T) {
	want := map[benchmark.Mode]bool{benchmark.ModeRagasBench: false, benchmark.ModeSummaryBench: false}
	for _, m := range benchModes {
		if _, ok := want[m]; ok {
			want[m] = true
		}
	}
	for m, found := range want {
		if !found {
			t.Errorf("benchModes missing %q", m)
		}
	}
}

func TestBenchModes_Phase3AreQuality(t *testing.T) {
	for _, m := range []benchmark.Mode{benchmark.ModeRagasBench, benchmark.ModeSummaryBench} {
		c, ok := benchmark.CategoryOf(m)
		if !ok || c != benchmark.CatQuality {
			t.Errorf("CategoryOf(%q) = %v,%v; want Quality,true", m, c, ok)
		}
	}
}
```

In `benchmark_detail_test.go`, add (model on the existing per-mode detail tests there):

```go
func TestModeDetailLines_Ragas(t *testing.T) {
	r := benchmark.Run{
		Mode: benchmark.ModeRagasBench,
		Aggregate: benchmark.Aggregate{
			RagasFaithfulness: 0.82, RagasRelevancy: 0.9, RagasPrecision: 0.75,
		},
	}
	lines := modeDetailLines(r)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "faithfulness") || !strings.Contains(joined, "relevancy") || !strings.Contains(joined, "precision") {
		t.Errorf("ragas detail missing fields:\n%s", joined)
	}
}

func TestModeDetailLines_Summary(t *testing.T) {
	r := benchmark.Run{
		Mode:      benchmark.ModeSummaryBench,
		Aggregate: benchmark.Aggregate{SummaryCoherence: 0.71},
	}
	lines := modeDetailLines(r)
	if !strings.Contains(strings.Join(lines, "\n"), "coherence") {
		t.Errorf("summary detail missing coherence: %v", lines)
	}
}
```

Run: `go test ./internal/ui/pages/ -run 'Phase3|Ragas|Summary' -v` → FAIL (modes not registered in `benchModes`; detail cases missing).

- [ ] **Step 2: Register in `benchModes`**

In the `// Quality` group of `benchModes`, after `benchmark.ModeCodeGenBench`, add:

```go
	benchmark.ModeRagasBench,
	benchmark.ModeSummaryBench,
```

- [ ] **Step 3: Add picker descriptions**

In `viewModePick`'s `descs` map, add:

```go
		benchmark.ModeRagasBench:   "RAG quality (synthetic): grader scores faithfulness, answer relevancy, and context precision",
		benchmark.ModeSummaryBench: "multi-doc summarization: fact coverage + grader-scored coherence",
```

- [ ] **Step 4: Add detail lines**

In `modeDetailLines`, add cases to the `switch r.Mode`:

```go
	case benchmark.ModeRagasBench:
		lines = append(lines, fmt.Sprintf("RAG — faithfulness %.0f%%   relevancy %.0f%%   precision %.0f%%",
			a.RagasFaithfulness*100, a.RagasRelevancy*100, a.RagasPrecision*100))
	case benchmark.ModeSummaryBench:
		lines = append(lines, fmt.Sprintf("summarization coherence %.0f%%", a.SummaryCoherence*100))
```

- [ ] **Step 5: Run tests; expect pass**

Run: `go test ./internal/ui/pages/ -run 'Phase3|Ragas|Summary' -v` → PASS.
Then the full UI package: `go test ./internal/ui/pages/` → PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/pages/benchmark.go internal/ui/pages/benchmark_test.go internal/ui/pages/benchmark_detail_test.go
git commit -m "feat(ui): surface RagasBench + SummaryBench in the benchmark tab

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Final verification (after all tasks)

- [ ] From the worktree root, run the full quality gate:

```bash
gofmt -l .                       # prints nothing
go build ./...                   # succeeds
go vet ./...                     # clean
go test ./...                    # all packages pass
```

- [ ] Confirm the new modes are reachable end-to-end: `go test ./internal/service/benchmark/ ./internal/ui/pages/ -count=1`.
- [ ] Confirm `data/` total size is comfortably under the 2 MB embed budget (`du -sh internal/service/benchmark/data`).
- [ ] Dispatch the final holistic code reviewer over the whole Phase 3 diff (`git log --oneline` since `e596b14`), then hand off via superpowers:finishing-a-development-branch. **Do not merge into main** — integration is the user's responsibility.

## Self-review (against the master spec §5.5, §6.1, §10 Phase 3)

- **§5.5 RagasBench** — Task 4 (dataset, schema `{documents,question,groundTruth,expectedContext}`) + Task 5 (mode, grader scores faithfulness/relevancy/precision, `Finalize` sets the three aggregate fields). ✓
- **§6.1 quality haystack (arXiv abstracts in `data/arxiv_docs.json`)** — Task 2 (dataset+loader) + Task 3 (haystack builder swap). ✓
- **§6.1 multi-doc summarization coherence** — Tasks 6+7 as a dedicated mode per locked decision D-P3-1 (spec located it in LongContext; user chose a separate mode to avoid mixing objective and graded scores). Documented divergence. ✓
- **§10 Phase-3 QA ("Ragas faithfulness drops on a low-quant model that hallucinates")** — supported: `RagasFaithfulness` is grader-scored per answer and aggregated, so a hallucinating model scores lower. Manual QA, not an automated test.
- **Type consistency** — `ragasDetail`/`parseRagasScores`, `summaryDetail`/`parseSummaryScores`, `factCoverage`, `buildQualityHaystack`, `loadArxivDocs`/`loadRagasProblems`/`loadSummaryProblems`, Runner fields `arxivDocs`/`ragasProblems`/`summaryProblems`, and Aggregate fields `RagasFaithfulness`/`RagasRelevancy`/`RagasPrecision`/`SummaryCoherence` are referenced consistently across tasks. ✓
- **No new config** — RagasBench/SummaryBench reuse `MaxTokens`/`Timeout`; `docs/profile-schema.json` is untouched (app config only). ✓

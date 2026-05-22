# Benchmark Engine Expansion — Master Design

> **Status:** Approved design — ready for implementation planning
> **Date:** 2026-05-22
> **Source proposal:** `BENCHMARK_PROPOSAL.md`
> **Scope decision:** One master spec covering the full proposal, organized into
> sequential phases. Implementation proceeds phase-by-phase with review
> checkpoints — a single phase is the unit of work, not the whole spec.

---

## 1. Goal

Expand `internal/service/benchmark` from three evaluation modes (LLM-judge
SWE-bench, static needle, throughput) into a multidimensional protocol that also
measures math reasoning, code generation, instruction robustness, factual
knowledge, and RAG quality — the dimensions where weight/KV-cache quantization
degrades a local model without moving perplexity or tokens/second.

The current engine is the foundation; this design **extends horizontally**
(new modes) and **refines vertically** (better long-context and throughput
probes) without rewriting the working core.

## 2. Confirmed baseline (verified against code)

- `data/swebench_lite.json` holds exactly **8** problems.
- Long-context needle is **static** (`longNeedle = "WIDGETKEY_NEEDLE_91743X"`,
  `runner.go`).
- LlamaBench presets default to `{512/128, 4096/256}` (`defaultPresets`).
- `runner.Run` dispatches modes via an inline `switch` (`runner.go:183`).
- `Scorer` is a clean single-method interface (`scorer.go:22`).
- Datasets load through `//go:embed data/*.json` (`dataset.go`).
- UI exposes modes via the `benchModes` slice and renders detail in
  `bvRunDetail` (`internal/ui/pages/benchmark.go`).
- Benchmark config is wired in `cmd/model-loader/main.go:113` (app config, NOT
  profile config — so `docs/profile-schema.json` is **not** affected).

## 3. Key design decisions (resolved during brainstorming)

| # | Decision | Rationale |
|---|---|---|
| D1 | **Single master spec, phased implementation.** | User-chosen; phase boundaries are the real review/merge units. |
| D2 | **Zero-config scoring uses the model-under-test as judge** (self-judge) when no external `JudgeEndpoint` is configured; external judge remains the gold standard. Similarity uses the server's `/v1/embeddings`, falling back to lexical token-set cosine. | Out-of-the-box coverage without forcing judge setup; bias is documented and flagged. |
| D3 | **CodeGen executes via `python3` on PATH** with hard limits (5s timeout, killed process group, stripped env, best-effort no-network); **skips cleanly** if `python3` is absent. | Matches the project's existing Python-fallback posture; avoids Docker dependency. |
| D4 | **UI scope:** generic views + mode picker (grouped by category) + per-mode detail sections + run export. **Cross-run Quantization Comparison view is deferred** to a later spec. | Generic views already work on `Run`/`Aggregate`; rich cross-run UI is a separable project. |
| D5 | **Self-judge is built in Phase 2**, so the proposal's separate "Phase 5 (judge autonomy)" folds in; calibration-vs-external becomes a QA task. | Avoids a near-empty phase. |

## 4. Architecture

### 4.1 Mode-handler registry (replaces the dispatch switch)

The inline `switch rc.Mode` does not scale to eight modes. Introduce a registry
of self-contained handlers:

```go
type Category string
const (
    CatQuality    Category = "Quality"    // judge, math, codegen, ragas
    CatSpeed      Category = "Speed"       // llama-bench
    CatRobustness Category = "Robustness"  // instruction, long-context
    CatKnowledge  Category = "Knowledge"   // mmlu
)

type modeHandler interface {
    Mode() Mode
    Category() Category
    Count(r *Runner) int   // number of problems/presets this mode will run
    Run(ctx context.Context, r *Runner, base, model string,
        progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error)
    Finalize(*Aggregate)   // sets mode-specific aggregate fields; no-op for most
}
```

`runner.Run` keeps only the shared spine:

1. load profile, build `ProfileSnapshot`
2. validate mode + build grader (fail fast before launching the backend)
3. `ensureInstance` (reuse or launch + health), GPU sampler
4. dispatch to `handler.Run(...)`
5. generic `aggregate()` over the returned `ProblemResult`s
6. `handler.Finalize(&agg)` for mode-specific metrics
7. return `Run`

Handlers self-register in `init()` into a package-level map keyed by `Mode`.
Each handler lives in its own file. The existing three modes are migrated to
this shape **first**, as a behavior-preserving refactor, before any new mode is
added.

### 4.2 Grader abstraction

```go
type grader interface {
    // Grade scores free-form text against a rubric/criteria. Returns 0..1.
    Grade(ctx context.Context, req gradeRequest) (gradeResult, error)
}
```

Implementations:

- **externalJudge** — the existing `judgeScorer`, used when
  `Config.Judge.BaseURL`+`Model` are set. Gold standard.
- **selfJudge** — the same rubric machinery pointed at the model-under-test's
  own `base` URL. Default when no external judge is configured. Every result it
  produces carries `judgedBy: "self"` plus the self-grading-bias caveat in the
  transcript/detail.
- **similarity** — cosine similarity. Tries the server's `/v1/embeddings`
  endpoint; on absence or error falls back to lexical token-set cosine. Used by
  Instruction consistency.

`r.graderFor(mode)` returns external if configured, else self. **Objective modes
(Math exact-match, CodeGen execution, needle detection, MMLU letter-match) never
invoke a grader.**

### 4.3 Domain type extensions (`result.go`)

```go
const (
    ModeMathBench    Mode = "math-bench"
    ModeCodeGenBench Mode = "codegen-bench"
    ModeRagasBench   Mode = "ragas-bench"
    ModeInstBench    Mode = "instruction-bench"
    ModeMMLUBench    Mode = "mmlu-bench"
)
```

`Aggregate` gains (all `omitempty`, zero when not applicable):

```go
AvgPromptProcessingTPS float64 // avg prefill tok/s (existing modes too)
AvgDecodeTPS           float64 // avg decode tok/s
MathAccuracy           float64
CodePassRate        float64
RagasFaithfulness   float64
RagasRelevancy      float64
RagasPrecision      float64
InstFormatRate      float64
InstRefusalRate     float64
InstConsistency     float64
MMLUAccuracy        float64
```

`ProblemResult` gains `PromptProcessingTPS` / `DecodeTPS` so the prefill/decode
split is visible per problem and aggregable. Mode-specific per-problem detail
(math correctness, codegen stderr/timeout, etc.) is carried in the existing
`Detail` string and in `ProblemTranscript` rather than new structs, keeping the
store and generic views unchanged.

## 5. New modes

### 5.1 MathBench (`mathbench.go`)
- Dataset: ~200 GSM8K problems stratified by difficulty + AIME 2023–2025
  (public). Schema: `{"id","question","answer","difficulty"}`.
- Eval: numeric exact-match with normalization (strip trailing `.0`, normalize
  `42`/`42.0`/word forms), Pass@1, optional `<think>…</think>` strip to extract
  the final answer.
- `Finalize` sets `MathAccuracy`.

### 5.2 CodeGenBench (`codegenbench.go`)
- Dataset: ~64 HumanEval problems (MBPP optional later). Schema:
  `{"task_id","prompt","canonical_solution","test"}`.
- Eval: write candidate to a temp dir, run `python3` with a 5s timeout, kill the
  whole process group on timeout, stripped environment, best-effort
  network-off. Pass@1. If `python3` is not on PATH, every problem reports
  `skipped` and `CodePassRate` is omitted (not zero).
- `Finalize` sets `CodePassRate`.

### 5.3 InstructionBench (`instructionbench.go`)
- Dataset: ~20 format prompts (JSON/markdown-list), ~10 refusal prompts,
  ~10 consistency prompts.
- Eval: format via regex/JSON-parse; refusal via keyword/empty detection;
  consistency via the `similarity` grader across 3 generations.
- `Finalize` sets `InstFormatRate`, `InstRefusalRate`, `InstConsistency`.

### 5.4 MMLUBench (`mmlubench.go`)
- Dataset: 100-question subset across STEM/humanities/social/other. Schema:
  `{"question","choices","answer"}`.
- Eval: letter exact-match, per-category breakdown + aggregate.
- Optional bonus: if `lm_eval` is on PATH, wrap it as a subprocess; otherwise use
  the embedded subset.
- `Finalize` sets `MMLUAccuracy`.

### 5.5 RagasBench (`ragasbench.go`, Phase 3)
- Dataset: ~20 synthetic RAG scenarios. Schema:
  `{"documents","question","ground_truth","expected_context"}`.
- Eval via the grader (external or self): faithfulness (no hallucination beyond
  docs), answer relevancy, context precision.
- `Finalize` sets `RagasFaithfulness`/`RagasRelevancy`/`RagasPrecision`.

## 6. Existing-mode enhancements

### 6.1 LongContext
- **Dynamic needle:** `randomCity()` + `randInt(1000,9999)` per run, defeating
  memorization.
- **Multi-needle:** 3 needles at 25/50/75 % depth; ask for all three; score by
  fraction recovered (middle-depth loss is the KV-cache signal).
- **Quality haystack (Phase 3):** replace Python filler with real arXiv
  abstracts embedded in `data/arxiv_docs.json`.
- **Multi-doc summarization coherence (Phase 3):** supply ~10 docs and require a
  summary mentioning 5 specific facts; graded by the grader.

### 6.2 LlamaBench
- Presets `{128/512, 512/128, 2048/256, 4096/256, 8192/128, 16384/64}`.
- 1–2 warmup reps before measurement to remove cold-start bias.
- Compute `PromptProcessingTPS` (prefill tokens / TTFT) and `DecodeTPS`
  (gen tokens / (total − TTFT)) per preset and aggregate.

### 6.3 Judge
- Add `maintainability (0.10)` to the rubric (rebalance existing weights so the
  total stays 1.0); enable the self-judge fallback when no external judge is set.
- Expand `data/swebench_lite.json` from 8 to 32 problems (language diversity),
  preserving the existing schema and `validate()` contract.

## 7. Datasets — sourcing & embedding

- All datasets come from canonical public, permissively-licensed sources
  (GSM8K, HumanEval, MMLU — MIT; SWE-bench Lite; AIME public).
- Curation is **reproducible via a documented script** (fetch → stratify →
  trim → emit JSON in the documented schema), not hand-typed, so subsets can be
  regenerated/audited. The script and provenance notes live alongside the data.
- Files embed through the existing `embed.FS` (`//go:embed data/*.json`).
- **Binary-size mitigation:** plain JSON by default; if the `data/` directory
  exceeds ~2 MB, gzip only the two largest (`gsm8k`, `humaneval`) as `.json.gz`
  behind a small gunzip loader. Golden tests cover both code paths.

## 8. Configuration (`internal/config` + `main.go`)

New optional benchmark knobs (app config, defaults preserve current behavior):
- `math.subset_size`, `mmlu.subset_size`, `codegen.subset_size`
- `codegen.exec_timeout` (default 5s)
- `embeddings.base_url` override (default: reuse the model-under-test server)
- `llama_bench.warmup_reps` (default 1)
- per-mode `quick`/`full` subset toggle for run-time control

These are config.toml fields only; **no change to `docs/profile-schema.json`**.

## 9. UI (`internal/ui/pages/benchmark.go`)

- Register new modes in `benchModes`, grouped by `Category` in the picker.
- Render new `Aggregate` fields in `bvRunDetail`.
- Add lightweight per-mode detail sections: math difficulty breakdown, codegen
  pass/fail list (with stderr access), ragas/instruction rate lines.
- Add CSV/JSON export of a run for external analysis.
- Honor the input-routing contract: any new editable surface (e.g. export
  filename prompt) must keep `IsCapturingInput()` correct.
- **Deferred:** cross-run Quantization Comparison view (later spec).

## 10. Phases (sequential; each is a review/merge checkpoint)

**Phase 1 — Foundations**
1. Refactor mode dispatch into the `modeHandler` registry (no behavior change).
2. Dynamic + multi-needle long context.
3. Expanded LlamaBench presets + warmup.
4. `PromptProcessingTPS`/`DecodeTPS` on `ProblemResult` + `Aggregate`.
5. SWE-bench set 8 → 32.
- **QA:** existing modes (judge/longctx/llama-bench) show zero regression.

**Phase 2 — Cognitive modes + grader**
1. Grader abstraction (external + self + similarity).
2. MathBench, CodeGenBench, InstructionBench, MMLUBench.
3. Judge maintainability criterion + self-judge fallback.
- **QA:** Q8_0 scores ≥ Q4_K_M on Math/CodeGen for the same model; self-judge
  vs external judge correlate > 0.8 on a 10-problem subset.

**Phase 3 — RAG & coherence**
1. RagasBench.
2. Quality haystack (arXiv abstracts).
3. Multi-doc summarization coherence test in LongContext.
- **QA:** Ragas faithfulness drops on a low-quant model that hallucinates.

**Phase 4 — UI**
1. Picker grouping + new detail fields.
2. Per-mode detail sections.
3. Run export (CSV/JSON).
- **QA:** full TUI navigation for every new mode; input-routing tests pass.

## 11. Testing (TDD)

- Golden tests for every dataset loader (and the gzip path).
- Grader tests against stub HTTP servers (existing `client_test.go` /
  `judge_test.go` pattern), covering external, self, and similarity-with-lexical
  fallback.
- Sandbox tests gated on `python3` presence (skip when absent).
- Unit tests: numeric normalization, needle generation/detection at depth,
  preset parsing, prefill/decode math.
- Regression suite for the three existing modes after the Phase 1 refactor.

## 12. Risks & mitigations

| Risk | Mitigation |
|---|---|
| Binary size from embedded datasets | Curated subsets; selective gzip of the two largest. |
| Untrusted code execution | 5s timeout, process-group kill, stripped env, best-effort no-network, skip-if-absent. |
| Self-judge inflates scores | `judgedBy:"self"` flag + documented caveat; external judge stays gold standard; calibration QA. |
| Long total run time | Per-mode quick/full subset selection; partial runs as default. |
| UI clutter from 8 modes | Category grouping in the picker; generic views as default. |

## 13. Success criteria

- ≥ 6 evaluated dimensions (coding, math, codegen, rag, instruction, knowledge,
  throughput, long-context).
- Quantization sensitivity: MathAccuracy ↓ ≥ 5 % and CodePassRate ↓ ≥ 3 % from
  Q8_0 to Q4_K_M; needle recovery ↓ ≥ 10 % at Q3_K.
- Reproducibility: solve_rate σ < 3 % across 3 consecutive runs.
- ≥ 5 modes usable with zero judge configuration.
- Zero regression in the three existing modes.

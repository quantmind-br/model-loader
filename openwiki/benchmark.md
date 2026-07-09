# Benchmark

`internal/service/benchmark/` (the engine) and `internal/ui/pages/benchmark*.go` (the TUI tab) share `BenchmarkConfig` (`internal/app/benchmark_config.go`) as the single source of truth. The CLI `model-loader benchmark` consumes the same config.

The engine spans **12 modes across 5 categories**: Quality · Speed · Robustness · Knowledge · **Agentic**. Agentic modes shell out to external harnesses (`tb`, SWE-bench_Pro-os, `pier`) plus Docker.

## Mode catalog

| Category | Mode | CLI flag | Headline metric (`primaryMetric`) |
|----------|------|----------|------------------------------------|
| Quality | LLM-judge (SWE-bench Lite) | `--mode judge` | solve rate |
| Quality | Math (GSM8K) | `--mode math-bench` | solve rate |
| Quality | Codegen (HumanEval) | `--mode codegen-bench` | solve rate |
| Quality | RAG (RAGAS) | `--mode ragas-bench` | solve rate |
| Quality | Summarization | `--mode summary-bench` | solve rate |
| Speed | `llama-bench` throughput | `--mode llama-bench` (aliases: `llamabench`, `throughput`) | **tok/s** |
| Robustness | Long-context needle | `--mode longctx` (alias: `long-context`) | **recall** / `AvgScore` |
| Robustness | Instruction-following | `--mode instruction-bench` | solve rate |
| Knowledge | MMLU | `--mode mmlu-bench` | solve rate |
| Agentic | Terminal-Bench | `--mode terminal-bench` | solve rate |
| Agentic | SWE-bench Pro | `--mode swe-bench-pro` | solve rate |
| Agentic | DeepSWE | `--mode deep-swe` | solve rate |

**Primary metric** (`benchmark_metrics.go::primaryMetric`): every mode except `llama-bench` (tok/s) and `longctx` (recall/`AvgScore`) reports **solve rate** as headline metric. One derivation feeds the dashboard ranking, the scorecard, the compare default, and the history trend.

## TUI tab state machine

`benchmark.go::benchView`:

```
Dashboard (landing)
   ↓ b / Enter on row
Wizard (profile → mode → review)
   ↓ enter
Running
   ↓ done / cancel
RunDetail
   ↓ esc
Dashboard

Dashboard → c → Compare → esc
Dashboard → h → History  → esc
```

- **Dashboard** — mode-focused leaderboard. Summary strip · mode-focus bar · ranked rows (latest *complete* run per profile, `MetricBar` + Δ vs previous; partial `Err` runs skipped) · insight panel (trend sparkline + Δ)
- **Wizard** — unified 3-step launcher: profile (filterable) → mode (cards grouped by category) → review. `enter` advances, `esc` back-navs (preserves selection), `/` filters profiles
- **Running** — phase label + progress bar (`components.MetricBar`) + running-tallies (✓/✗/!) + current item, with activity log and staleness footer. Rendered from the runner's authoritative `RunFeed.Snapshot()` on a 1s tick. `esc` arms cancel-confirm modal (stray esc does not abort a long run).
- **RunDetail** — scorecards (primary metric, tok/s, TTFT inverted, VRAM) + mode-specific breakdown lines (math difficulty, code pass rate, instruction format/refusal/consistency, MMLU category, RAG faithfulness/relevancy/precision, summary coherence, agentic accuracy). `E` export, `esc` back
- **Compare** — latest run per profile, grouped by mode. `m` cycles ranking metric (mode primary · tok/s · TTFT-lower · VRAM-lower). `esc` back
- **History** — runs of one profile over time with timeline + sparkline. `m` toggles sparkline metric. `esc` back

## CLI

```bash
# List modes
model-loader benchmark list

# Run a single mode
model-loader benchmark run --profile my-profile --mode math-bench --limit 5
model-loader benchmark run --profile my-profile --mode llama-bench
model-loader benchmark run --profile my-profile --mode longctx

# Compare latest runs across profiles
model-loader benchmark compare

# Inspect a run
model-loader benchmark transcript <run-id>

# Gate on minimum solve rate (exit 0 vs 2)
model-loader benchmark run --profile my-profile --mode math-bench --min-solve 0.7
```

Per-mode flags: `--tb-task` / `--tb-n-tasks` (terminal-bench), `--sweap-{harness,patches,instance}` (swe-bench-pro), `--deepswe-{task,n-tasks,tasks}` (deep-swe). `--limit` caps items per reducible mode (`0` = full).

## Configuration (`[benchmark]` in `config.toml`)

| Key | Default | Description |
|-----|---------|-------------|
| `max_tokens` | `32768` | Generation cap per problem |
| `temperature` | `0.0` | Sampling temperature |
| `timeout_sec` | `120` | Per-problem inference timeout |
| `long_context_tokens` | `0` | Target prompt size for long-context needle (`0`→8000) |
| `save_transcripts` | `true` | Capture raw model/judge I/O per run |

`[benchmark.judge]` — LLM-as-judge endpoint (OpenAI-compatible). `base_url` empty disables LLM judging. The judge reads `$QUANTMIND_API_KEY` at runtime (kept as `$ENV`, never written to disk).

`[benchmark.llamabench]` — throughput-mode settings: `presets = ["512/128", "4096/256"]`, `repetitions = 3` (0→3).

`[benchmark.terminalbench]`, `[benchmark.swebenchpro]`, `[benchmark.deepswe]` — see `docs/config.md` and the per-mode runbooks:

- Terminal-Bench: wraps `tb` CLI, requires Docker. `concurrent` should stay at 1 on a single-GPU rig
- SWE-bench Pro: 3-stage subprocess coordinator (`gather_patches.py` → `swe_bench_pro_eval.py`). Requires `harness_dir` + `raw_sample_path` (lowercase `fail_to_pass`/`pass_to_pass` columns) + Docker. See `docs/swe-bench-pro.md`
- DeepSWE: wraps `pier` CLI, requires Docker. Chat-completions routing and container→host networking matter. See `docs/deep-swe.md`

## Engine internals

- `runner.go` (`internal/service/benchmark/runner.go`) — top-level orchestrator, progress streaming, cancel
- `client.go` — talks to the proxy (OpenAI-compatible), handles SSE
- `judge.go` — LLM-as-judge via the configured `[benchmark.judge]` endpoint
- `grader.go` — exact-match / regex / numeric grading
- `result.go` — run result envelope (solve rate + breakdown + per-problem outcomes)
- `gpu_sampler.go` — background VRAM/temp sampler during a run
- `llamabench_probe.go`, `longcontext_probe.go` — direct-binary invocations (no proxy)
- `terminalbench.go`, `swebenchpro.go`, `deepswe.go` — agentic harnesses (shell out + Docker)
- `prompts/` and `data/` — bundled datasets
- `arxiv.go` — arxiv-API helper for paper-citation context (judge prompts)

`benchmarkstore.Store` persists one JSON per run under `~/.local/state/model-loader/benchmark/runs/<run-id>.json`, with optional `.transcript.json` sidecars when `save_transcripts` is on.

## Anti-patterns and gotchas

- **No `--dashboard` / `--leaderboard`** — leaderboard is TUI-only
- **Python backend buffering** — benchmarks drive the proxy; the proxy drives the backend. The backend's stdout buffering doesn't affect benchmark results, but it does affect log readability in the Server tab
- **`llama-bench` prompt scaling** — recent fix (`348433a`): scale prompt below `max_model_len` at high fill to avoid spurious failures on large-context profiles
- **Agentic modes** require Docker. If Docker isn't running, `Prepare` fail-fasts. Do not silently skip the Docker check
- **TTFT/VRAM-only runs** must not be ranked or fill "best" in compare (`fab33b8`) — a `MetricBar` of zero data is a special case, not a zero score

## Implementation notes for future agents

- Adding a new mode: create `internal/service/benchmark/<mode>.go` exporting a `Runner` (or `Prepare`/`Run`/`Result` triplet), register the mode in `config.go::BenchmarkConfig`, add a CLI alias map in `internal/cli/benchmark.go`, add a card to the TUI wizard (`benchmark_wizard.go`).
- `primaryMetric` is the single source of truth for "what shows on the dashboard leaderboard" — register your mode there. If your headline metric isn't solve rate, override `primaryMetric` for that mode.
- Run cancellation must respect the 32-buffered progress channel — don't block the runner on a full channel
- The judge prompt is part of the dataset, not the mode — change prompts in `data/`, not in code
- For agentic modes, the external harness is **always** a subprocess — never reach into the harness's Python internals
- After adding a dataset, regenerate goldens with `go test ./internal/service/benchmark/... -update`

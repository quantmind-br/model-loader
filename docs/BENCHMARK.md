# Benchmark

`model-loader benchmark` evaluates a profile through the HTTP proxy and saves
every run for later inspection. For exclusive access, stop the TUI and any
`model-loader serve` process first: a TUI-held single-instance lock makes
`benchmark run` exit with "another model-loader instance is running", while an
externally running proxy otherwise contends for the backend and port.

The historical Qwen3.6 MTP vs MTP+DFlash tuning workflow is preserved at
[docs/reports/qwen36-benchmark-workflow-2026-06.md](reports/qwen36-benchmark-workflow-2026-06.md).
It is not part of the current CLI.

## Benchmark modes

12 modes in 5 categories (canonical order from the engine registry):

| Mode | Category | What it measures |
|---|---|---|
| `judge` | Quality | SWE-bench Lite issues graded by a reference-guided LLM judge (requires an external judge: `benchmark.judge.base_url` + `benchmark.judge.model`) |
| `math-bench` | Quality | Curated GSM8K problems, exact numeric match |
| `codegen-bench` | Quality | Curated HumanEval problems, solutions executed against unit tests by `python3` (bubblewrap `bwrap` when available for a no-network, read-only-system sandbox; otherwise a plain subprocess with only a minimal environment — generated code then runs with the operator's filesystem/network permissions) |
| `ragas-bench` | Quality | Synthetic RAG scenarios, grader-scored faithfulness, relevancy, and context precision |
| `summary-bench` | Quality | Multi-document summarization, grader-scored coherence and fact coverage |
| `llama-bench` | Speed | Throughput probe (`llama-bench` style): fixed prompts measuring TTFT and tokens/second across fill presets |
| `longctx` | Robustness | Single needle-retrieval probe plus prompt/generation speed; ignores `--limit` |
| `instruction-bench` | Robustness | Format adherence (deterministic check), refusal behavior (grader with keyword-heuristic fallback), and answer consistency (similarity) |
| `mmlu-bench` | Knowledge | Curated MMLU subset, multiple-choice letter exact match |
| `terminal-bench` | Agentic | External Terminal-Bench harness (`tb` CLI + Docker, Terminus agent pointed at the proxy) |
| `swe-bench-pro` | Agentic | External SWE-bench Pro harness (`SWE-bench_Pro-os` checkout + `python` + Docker, optional patch-generation agent) |
| `deep-swe` | Agentic | External DeepSWE harness (`pier` CLI + Docker + cloned `tasks/` corpus, `mini-swe-agent` pointed at the proxy) |

`judge` requires the external judge endpoint and fails in `Prepare` without
it. `ragas-bench`, `summary-bench`, and `instruction-bench` refusal use the
external grader when `[benchmark.judge]` is configured, otherwise the model
under test grades itself (`judgedBy=self`); refusal falls back to a keyword
heuristic when the grader is unreachable. The remaining checks are objective
(exact match, test execution, or deterministic checks).

Full per-mode configuration lives in [config.md](config.md#benchmark); the
agentic guides are [deep-swe.md](deep-swe.md) and
[swe-bench-pro.md](swe-bench-pro.md).

## Running

```bash
# Reduced deterministic run (recommended first step for any mode)
model-loader benchmark run --profile <profile-id> --mode math-bench --limit 5

# Judge mode (requires benchmark.judge.base_url + benchmark.judge.model in config)
model-loader benchmark run --profile <profile-id> --mode judge

# Fail the command when quality drops below a threshold
model-loader benchmark run --profile <profile-id> --mode mmlu-bench --min-solve 0.5

# Machine-readable output (global flag, honored by every subcommand)
model-loader benchmark run --profile <profile-id> --mode longctx --json

# Live harness/stream activity lines (agentic modes are quiet by default)
model-loader benchmark run --profile <profile-id> --mode terminal-bench --verbose
```

`--profile` is required. `--mode` defaults to `judge` (which needs the external
judge endpoint; start with `--mode math-bench --limit 5` when no judge is
configured); unknown modes list the valid set. `--min-solve` takes `0..1` and exits `2` when the solve rate falls
below it (`-1` disables the gate). Interrupted or failed runs still save
whatever completed as a partial run.

### Reduced runs (`--limit`)

`--limit N` is the uniform reduced-run knob. The CLI overrides the configured
value only when positive (`internal/cli/benchmark.go`: `if limit > 0`); the
runner then caps to the smaller of the configured limit and the item count
(`capCount`: `Limit > 0`, else the full count). That means `--limit 0`
(the default, i.e. flag omitted) leaves `benchmark.limit` from `config.toml`
in effect — a full-dataset run needs both the flag at `0` (or omitted) and
`benchmark.limit = 0` in TOML:

- Dataset modes and `llama-bench` presets with an effective limit `N > 0`: run the first `N` items; effective `0` runs the full set.
- `terminal-bench` / `deep-swe`: a positive `--limit` is mapped to `--n-tasks N` with a deterministic
  seed unless a mode-specific task flag is given.
- `swe-bench-pro`: a positive `--limit` samples `N` instance IDs deterministically from
  `raw_sample_path` (seeded by `swebenchpro.sample_seed`) unless
  `--sweap-instance` is given.
- `longctx`: single probe, ignores `--limit`.

### Mode-specific overrides

Explicit flags win over `--limit` and over the TOML values for that run only:

```bash
# terminal-bench: one task, or a capped subset
model-loader benchmark run --profile <id> --mode terminal-bench --tb-task hello-world
model-loader benchmark run --profile <id> --mode terminal-bench --tb-n-tasks 5

# swe-bench-pro: ad-hoc harness/patches/instances
model-loader benchmark run --profile <id> --mode swe-bench-pro \
  --sweap-harness ~/dev/SWE-bench_Pro-os \
  --sweap-patches ./my_patches.json \
  --sweap-instance instance_foo__bar-abc123

# deep-swe: ad-hoc corpus/tasks
model-loader benchmark run --profile <id> --mode deep-swe \
  --deepswe-tasks ~/dev/deep-swe/tasks \
  --deepswe-task abs-module-cache-flags
model-loader benchmark run --profile <id> --mode deep-swe --deepswe-n-tasks 3
```

The TUI Benchmark tab (key `5`) runs the TOML-configured settings; the flags
above are CLI-only.

## Configuration

Tune defaults in `~/.config/model-loader/config.toml` (see
[config.md](config.md#benchmark)):

```toml
[benchmark]
max_tokens = 32768
limit = 0
timeout_sec = 120
long_context_tokens = 0
save_transcripts = true
unload_after_run = false

[benchmark.judge]
base_url = ""
api_key = ""
model = ""
samples = 3

[benchmark.llamabench]
presets = ["5%/256", "25%/256", "50%/256", "90%/128"]
repetitions = 3
warmup = 1
```

`timeout_sec` is the per-problem inference timeout. `save_transcripts` keeps
raw model/judge I/O for `benchmark transcript`. `unload_after_run` calls
`/_admin/unload` when the run ends so VRAM is freed; the default keeps the
model warm. Agentic sections (`benchmark.terminalbench`, `benchmark.swebenchpro`,
`benchmark.deepswe`) configure the external CLIs; the engine never installs
`tb`, `pier`, Docker, the harnesses, or the task corpora.

## Result inspection

Runs are stored under `<state-dir>/benchmark/runs`
(default `~/.local/state/model-loader/benchmark/runs`).

```bash
# Newest-first run table (full run id in the last column)
model-loader benchmark list

# Latest run per profile
model-loader benchmark compare

# One profile's history
model-loader benchmark history <profile-id>

# One run's full result (identity/aggregate block plus per-problem table)
model-loader benchmark show <run-id>

# Raw per-problem model/judge I/O (needs benchmark.save_transcripts)
model-loader benchmark transcript <run-id>

# Write JSON + CSV for external analysis
model-loader benchmark export <run-id>
model-loader benchmark export <run-id> --dir /tmp/bench-export

# Delete a run (destructive; requires confirmation)
model-loader benchmark delete <run-id> --yes

# Read-only browser for saved runs (prints a URL; Ctrl-C exits).
# A separate process cannot see the TUI's live run, so the live
# monitor there always reports "no run in progress".
model-loader benchmark web
```

Append the global `--json` flag to `list`, `history`, `compare`, `show`, and
`transcript` for machine-readable output.

## External harness requirements

In-process modes need nothing beyond a loadable profile, except:

- `codegen-bench` executes candidates with `python3` on `PATH` and fails fast
  (`Prepare`) when it is missing. Kernel-level isolation needs bubblewrap
  (`bwrap`) on `PATH`: with it, candidates run with no network and read-only
  system dirs; without it they fall back to a plain subprocess (runtime still
  bounded and process-group killed, but no kernel isolation — generated code
  runs with the operator's filesystem/network permissions).
- `--mode judge` requires `[benchmark.judge]` (`base_url` + `model`); without
  it the run fails in `Prepare`. `ragas-bench`, `summary-bench`, and
  `instruction-bench` refusal grade with the external judge when configured,
  otherwise with the model under test itself.

Agentic modes shell out to already-installed tools and fail in `Prepare` when
they are absent:

- `terminal-bench`: `tb` CLI on `PATH` plus a running Docker daemon. Dataset
  defaults to `terminal-bench-core==0.1.1`. Keep `concurrent = 1` on a
  single-GPU rig.
- `swe-bench-pro`: cloned `SWE-bench_Pro-os` checkout (`harness_dir`),
  `raw_sample_path` with lowercase `fail_to_pass`/`pass_to_pass` columns
  (required; the bundled jsonl uses uppercase keys the eval misreads),
  `scripts_dir` (defaults to `<harness>/run_scripts`), `python` (defaults to
  `python3`), and Docker unless `use_modal = true`. See
  [swe-bench-pro.md](swe-bench-pro.md).
- `deep-swe`: `pier` CLI on `PATH` (`uv tool install datacurve-pier`), Docker,
  and the cloned corpus `tasks/` dir (`tasks_dir`). The in-container agent
  needs a host-reachable `api_base` and chat-completions routing; see
  [deep-swe.md](deep-swe.md).

## Safety

- **Exclusive access.** `benchmark run` takes the single-instance lock (held by
  the TUI) and supervises its own proxy. Close the TUI first — otherwise the
  run exits with "another model-loader instance is running". Also stop any
  `model-loader serve` process: `serve` does not take that lock, but it holds
  the proxy address the benchmark needs, so keeping it running contends for
  the backend and port.
- **One backend at a time.** Inference stays serial against the model under
  test so speed metrics are not polluted. Keep agentic `concurrent = 1` on a
  single-GPU rig — parallel trials thrash the same backend through the proxy.
- **Long, expensive runs.** Full agentic corpora pull gigabytes of Docker
  images (per-task or per-instance) and run for hours. Always start with
  `--limit` and the single-task smoke test in the mode guide before committing
  to a full run.
- **Watchdogs.** The per-problem `timeout_sec`, the whole-run `timeout_sec`
  caps, and the agentic stall watchdog (group-kills a wedged harness that
  stops scoring) bound hung runs. A killed harness surfaces as a partial run,
  not a silent zero.
- **VRAM.** The proxy keeps the backend loaded after the run unless
  `unload_after_run = true`. Unload explicitly with
  `curl -sX POST http://127.0.0.1:4321/_admin/unload` or
  `model-loader instance stop <pid|id>`.
- **Destructive actions.** Only `benchmark delete` destroys data, and only
  with `--yes`. `benchmark web` is read-only.

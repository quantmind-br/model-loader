# SWE-bench Pro benchmark mode

`--mode swe-bench-pro` evaluates a profile on [SWE-bench Pro](https://github.com/scaleapi/SWE-bench_Pro-os)
(Scale AI): given a real repository issue, a patch-generation **agent** (pointed
at the model-loader proxy) writes a diff, and the external harness applies it in
that instance's prebuilt Docker image and runs its `fail_to_pass` + `pass_to_pass`
tests. Each instance scores pass/fail; the run's accuracy is `resolved / total`.

Like [terminal-bench](#), this is **not** a single-turn-over-the-proxy mode: it is
an agentic, Docker-sandboxed pipeline owned by Python scripts. model-loader only
serves the model (the proxy) and records the score — it never installs the
harness, Docker, or the agent.

> **Cost & signal.** Per-instance images are large (~5–6 GB each); the full public
> set is 731 instances. A single workstation realistically runs a small subset.
> Frontier models score ~44% on the public set; gpt-oss-120b ~16% — so local
> 12–35B models will likely score near 0. Treat this as plumbing/diagnostics, not
> a leaderboard run, unless a subset shows discriminating signal.

## Pipeline

The mode runs up to three stages, all with the harness dir as the working
directory (so its `dockerfiles/`, `run_scripts/`, `helper_code/` resolve):

1. **Generate** *(optional)* — runs `agent_cmd` (your patch-generation scaffold)
   with the proxy as its OpenAI endpoint; the agent writes one `*.pred` per
   instance under a temp `preds/` dir. Skipped when you supply `patch_path`.
2. **Gather** — `python helper_code/gather_patches.py` consolidates the `*.pred`
   files into a single `patches.json`. Skipped when `patch_path` already points at
   a consolidated `.json`.
3. **Evaluate** — `python swe_bench_pro_eval.py --use_local_docker …` applies each
   patch in its Docker image, runs the tests, and writes
   `eval_results.json` (`{instance_id: bool}`), which model-loader parses.

## Prerequisites

```bash
# 1. Clone the harness somewhere stable
git clone https://github.com/scaleapi/SWE-bench_Pro-os ~/dev/SWE-bench_Pro-os

# 2. Install its Python deps (pandas, tqdm, datasets, docker SDK, …)
cd ~/dev/SWE-bench_Pro-os && pip install -r requirements.txt

# 3. Docker must be installed and running (local-docker backend)
docker run --rm hello-world
```

If `docker compose`/containers fail with `veth pair … operation not supported`,
load the kernel module: `sudo modprobe veth` (persist with
`echo veth | sudo tee /etc/modules-load.d/veth.conf`).

A patch-generation agent (for `agent_cmd`) is **not** included. The leaderboard
uses the SWE-agent scaffold (a Scale fork, bundled in the harness as a git
submodule); any agent that emits `instance_*/<id>.pred` files works, and it must
support a custom OpenAI endpoint (LiteLLM `model=openai/<profile-id>` +
`api_base`). You can also skip the agent entirely and evaluate pre-generated
patches via `patch_path`.

## The raw sample (`raw_sample_path`) — required

`raw_sample_path` has **no default** and must point at a CSV/JSONL of instance
metadata whose test-list columns are lowercase **`fail_to_pass` / `pass_to_pass`**.

⚠️ The harness's bundled `helper_code/sweap_eval_full_v2.jsonl` ships those two
columns **UPPERCASE** (`FAIL_TO_PASS` / `PASS_TO_PASS`), while `swe_bench_pro_eval.py`
reads them lowercase. Pointing there directly makes the eval raise `KeyError` per
instance — caught and scored **False** — so every instance silently fails *after*
paying the full Docker cost. Supply a sample with lowercase columns (rename the
two keys, or export from the HuggingFace dataset `ScaleAI/SWE-bench_Pro`).

## Configuration

```toml
[benchmark.swebenchpro]
harness_dir     = "~/dev/SWE-bench_Pro-os"   # cloned checkout (required)
raw_sample_path = "~/dev/sweap_lowercase.jsonl"  # required; lowercase f2p/p2p columns
# scripts_dir   = ""        # default: <harness>/run_scripts (bundled, 1000 instances)
dockerhub_user  = "jefzda"  # official prebuilt sweap-images repo
python          = "python3"
num_workers     = 4         # eval --num_workers (single workstation)
use_modal       = false     # false → --use_local_docker; true → Modal cloud
timeout_sec     = 0         # whole-pipeline cap; 0 → none
# patch_path    = ""        # pre-generated patches JSON or preds dir (skips agent)
# agent_cmd     = []        # patch-generation command (placeholders below)
# instances     = []        # subset of instance_ids; empty → all in the patch set
# extra_args    = []        # passed verbatim to swe_bench_pro_eval.py (e.g. "--block_network")
```

`agent_cmd` placeholders, substituted before exec: `{model}` (profile id),
`{api_base}` (`<proxy>/v1`), `{output}` (preds dir to write into), `{instances}`
(comma-joined ids), `{harness}` (harness dir).

**Precedence:** `patch_path` wins over `agent_cmd` — when patches are supplied the
agent is skipped entirely. Clear `patch_path` (omit `--sweap-patches`) to regenerate.

**Relative paths** (`raw_sample_path`, `scripts_dir`, `patch_path`) are resolved
relative to `harness_dir`, matching how the harness runs (it expects its own dir as
the working directory). Absolute paths are used as-is.

## Running

```bash
# Evaluate pre-generated patches (no agent), one instance, ad-hoc:
model-loader benchmark run --profile <id> --mode swe-bench-pro \
  --sweap-harness ~/dev/SWE-bench_Pro-os \
  --sweap-patches ./my_patches.json \
  --sweap-instance instance_<repo>__<repo>-<hash>

# Full pipeline (agent_cmd + raw_sample_path configured in TOML):
model-loader benchmark run --profile <id> --mode swe-bench-pro --sweap-instance <id>

# Reduced deterministic run: sample 2 instance_ids from raw_sample_path:
model-loader benchmark run --profile <id> --mode swe-bench-pro --limit 2
```

CLI overrides: `--sweap-harness`, `--sweap-patches`, `--sweap-instance`
(repeatable). The TUI Benchmark tab runs the configured settings.

### The reduced mode (`--limit`)

`--limit N` is the uniform reduced-run knob shared by every mode. swe-bench-pro's
eval script has no count flag, so `--limit N` samples N `instance_id`s
deterministically from `raw_sample_path` (seeded by `swebenchpro.sample_seed`,
default 0) and runs them as the instance filter — the same N instances on every
repeat. The agent receives the sampled set via the `{instances}` placeholder, and
the gathered patches are filtered to it. An explicit `--sweap-instance` wins over
`--limit`; otherwise `--limit` replaces any TOML `instances` list with the sampled
subset.

## Smoke test (gold patches)

The analog of terminal-bench's `hello-world`: evaluate the dataset's own gold
patches — they should resolve. This exercises the harness end-to-end (Docker pull,
test run, scoring) without an agent or the model.

```bash
cd ~/dev/SWE-bench_Pro-os
# extract gold patches into the patch JSON format
python helper_code/extract_gold_patches.py --output gold_patches.json
# keep just one instance_id to bound the Docker cost, then:
model-loader benchmark run --profile <any-id> --mode swe-bench-pro \
  --sweap-harness ~/dev/SWE-bench_Pro-os \
  --sweap-patches gold_patches.json \
  --sweap-instance <one-instance-id>
```

A gold-patch run scoring 100% confirms the plumbing; anything less points at the
`raw_sample_path` columns, a missing image, or Docker.

## Notes

- **Local vs Modal.** `use_modal = false` (default) passes `--use_local_docker`,
  evaluating on this machine. Modal (`use_modal = true`) needs `modal setup`.
- **Subset.** The eval has no instance flag; model-loader filters the gathered
  patch set to `instances` before evaluating. An empty result (no patch matched
  the raw sample) is reported as an error, not a hollow zero-instance run.
- **Images** are pulled by the dataset's `dockerhub_tag` from `jefzda/sweap-images`;
  no local builds.

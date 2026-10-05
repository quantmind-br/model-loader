# DeepSWE benchmark mode

`--mode deep-swe` evaluates a profile on [DeepSWE](https://github.com/datacurve-ai/deep-swe)
(Datacurve): 113 original, long-horizon software-engineering tasks across
TypeScript, Go, Python, JavaScript, and Rust, each with an isolated Docker
environment and a program-based verifier. For every task, the
[mini-swe-agent](https://github.com/SWE-agent/mini-swe-agent) (pointed at the
model-loader proxy) works inside the task's container and commits a patch;
[Pier](https://github.com/datacurve-ai/pier)'s verifier then applies that patch
in a pristine container and runs the held-out tests, scoring each task pass/fail.
The run's accuracy is `resolved / total`.

Like [terminal-bench](BENCHMARK.md#benchmark-modes) and [swe-bench-pro](swe-bench-pro.md), this is **not** a
single-turn-over-the-proxy mode: it is an agentic, Docker-sandboxed loop owned by
a Python CLI (`pier`). model-loader only serves the model (the proxy) and records
the score — it never installs Pier, Docker, or the task corpus.

> **Cost & signal.** Tasks are long-horizon and run in large per-task images
> (built from `public.ecr.aws/.../swe-bench-202605:*`). A single workstation
> realistically runs a small subset. DeepSWE targets *frontier* coding agents, so
> local 12–35B models will likely score near 0. Treat this as plumbing/diagnostics
> unless a subset shows discriminating signal. Always start with `--limit`.

## Pipeline

Per task, all inside Pier's `docker` environment:

1. **Install** — Pier installs `mini-swe-agent` (via `uv`) inside the task
   container. This runs at **image-build time on the host network**, so it
   succeeds regardless of the task's `allow_internet`.
2. **Solve** — the agent is pointed at the proxy (`openai/<profile-id>` +
   `OPENAI_API_BASE`) and commits a patch.
3. **Verify** — Pier extracts the commit as a patch, applies it in a pristine
   verifier container, runs the held-out tests, and writes
   `<trial>/verifier/reward.json` (`{"reward": 0|1, …}`), which model-loader
   parses from each trial's `result.json`.

## Prerequisites

```bash
# 1. Install Pier (the Harbor-compatible runner; >=0.3.0 for DeepSWE v1.1)
uv tool install datacurve-pier      # provides the `pier` CLI

# 2. Clone the task corpus
git clone https://github.com/datacurve-ai/deep-swe ~/dev/deep-swe

# 3. Docker must be installed and running (the local `docker` env is the default)
docker run --rm hello-world
```

`benchmark.deepswe.tasks_dir` must point at the **`tasks/` subdir** of the clone
(`~/dev/deep-swe/tasks`), the directory that holds one `<task-id>/task.toml` per
task.

## Chat-completions routing (important)

`mini-swe-agent` maps an `openai/<id>` model name onto its **Responses-API**
adapter (`litellm_response`), which **llama-server does not implement**. model-loader
forces LiteLLM **chat completions** by passing `--agent-kwarg model_class=litellm`
(override via `model_class`). The agent receives the endpoint as
`OPENAI_API_BASE` + `OPENAI_API_KEY` (a dummy key when none is set — the proxy
ignores it). vLLM/SGLang profiles should still set `served-model-name` to the
profile id so model-name validation accepts the proxied id.

## Container networking (important)

The agent runs **inside** the task's Docker container, so `127.0.0.1:4321` there
is the *container*, not the host. Two layers stand between the agent and a
host-run proxy; both must be cleared.

**1. Pier's egress sandbox.** DeepSWE tasks set `allow_internet = false`. Pier
then puts the agent on an `internal: true` network with **no host/LAN route** and
forces all egress through a **Squid forward-proxy sidecar** whose allowlist is a
set of **domains** (`dstdomain`) derived from the agent's URLs. Squid's
`dstdomain` **cannot match a bare IP**, so an `api_base` of
`http://172.17.0.1:4321/v1` is rejected (`ERR_ACCESS_DENIED`); a hostname like
`host.docker.internal` would be allowlistable, but Pier's generated compose adds
**no host-gateway mapping** to the Squid container, so it can't resolve/route to
the host either. Net: a local-host proxy is impractical under `allow_internet =
false` on Linux — which is why DeepSWE's own quickstart targets cloud APIs.

**2. The host firewall.** Even with egress allowed, a default-deny `INPUT`
(UFW/iptables) drops container→host traffic until you open the Docker bridge
subnet to the proxy port.

The reliable path on a single Linux rig is to **drop the egress sandbox** for the
agent container so it reaches the host proxy directly over the Docker bridge:

- In the task's `task.toml`, set `[environment] allow_internet = true` (leave
  `[verifier.environment]` `false` — scoring stays isolated). Pier then omits the
  Squid sidecar and the agent egresses over the `default` bridge.
- **Bind the proxy on a host-reachable interface:** `model-loader serve --host
  0.0.0.0 --port 4321` (or set the TUI/serve host).
- Set `benchmark.deepswe.api_base = "http://172.17.0.1:4321/v1"` (the Docker
  bridge gateway — reachable from containers; `host.docker.internal` works too on
  Docker Desktop, or on Linux with `extra_args = ["--ek",
  "extra_hosts=[\"host.docker.internal:host-gateway\"]"]`).
- **Open the host firewall** for the Docker bridge subnet → proxy port, e.g.
  `sudo ufw allow from 172.16.0.0/12 to any port 4321 proto tcp`.

Verified end-to-end on a dual-3090 Linux host: with the four steps above, the
agent's chat-completions calls reach the loaded backend and Pier writes a scored
`reward.json`. (Whether a given local model *solves* a long-horizon task is a
model-quality question, separate from this plumbing.)

## Configuration

```toml
[benchmark.deepswe]
tasks_dir   = "~/dev/deep-swe/tasks"   # cloned corpus tasks/ dir (required)
# command   = "pier"                   # pier CLI (name on PATH or absolute path)
# agent     = "mini-swe-agent"         # pier --agent
# provider  = "openai"                 # LiteLLM provider prefix → openai/<profile-id>
# model_class = "litellm"              # force chat completions (not the Responses API)
# api_base  = ""                       # agent-facing api_base; empty → <proxy>/v1 with loopback→host.docker.internal
# tasks     = []                       # --include-task-name ids/globs; empty → whole corpus
# n_tasks   = 0                        # --n-tasks cap; 0 → whole corpus
# sample_seed = 0                      # --sample-seed for deterministic subset (with n_tasks)
# concurrent = 1                       # --n-concurrent (single-GPU rig: keep at 1)
# timeout_sec = 0                      # whole-run cap; 0 → none (pier still enforces per-task timeouts)
# extra_args = []                      # passed verbatim after the built flags (e.g. "--force-build")
```

## Running

```bash
# Reduced run — 3 deterministic tasks (the fast functioning check):
model-loader benchmark run --profile <id> --mode deep-swe --limit 3

# A specific task (or glob), ad-hoc:
model-loader benchmark run --profile <id> --mode deep-swe \
  --deepswe-tasks ~/dev/deep-swe/tasks \
  --deepswe-task abs-module-cache-flags

# Full corpus (113 tasks — long; only with a capable model):
model-loader benchmark run --profile <id> --mode deep-swe
```

CLI overrides: `--deepswe-task` (repeatable, glob), `--deepswe-n-tasks`,
`--deepswe-tasks` (corpus dir). The TUI Benchmark tab runs the configured
settings.

### The reduced mode (`--limit`)

`--limit N` is the uniform reduced-run knob shared by every mode. For deep-swe it
feeds Pier's `--n-tasks N` **plus** a `--sample-seed` (default 0) so the same N
tasks are chosen on repeat runs — a fast, deterministic functioning check before
committing to a full run. An explicit `--deepswe-task`/`--deepswe-n-tasks` (or the
TOML `tasks`/`n_tasks`) takes precedence over `--limit`.

## Smoke test

The cheapest end-to-end check is a single task with `--limit 1`. It exercises the
whole path (Docker pull, agent install, proxy call, patch, verify) for one task.
If the agent calls fail immediately, re-check the chat-completions routing and the
container→host networking (see the two "important" sections above). A run that
completes with real token counts and a genuine pass/fail — rather than an
agent/connection error on every task — confirms the plumbing.

## Notes

- **Scoring.** `verifier/reward.json` carries a binary `reward` (1 iff every
  fail-to-pass test passes and no pass-to-pass test regresses). A verifier crash
  writes the `reward.txt = -1` sentinel; model-loader reports that as
  `verifier-error` (unresolved), distinct from a plain `unresolved`.
- **Results layout.** Pier writes one `result.json` per trial under
  `<jobs-dir>/model-loader/<trial>/`; model-loader walks those (skipping the
  job-root aggregate `result.json`) and maps each to a pass/fail by task id.
- **Subset determinism.** Pier applies `--sample-seed` *before* `--n-tasks`, so
  `(seed, N)` fully determines which tasks run.
- **Concurrency.** Keep `concurrent = 1` on a single-GPU rig: parallel trials
  would all hit one backend through the proxy and thrash the model swap.

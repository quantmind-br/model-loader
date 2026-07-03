# SNDR (Genesis) backend — runbook

[SNDR Core Engine](https://github.com/Sandermage/sndr_core_engine) is a
**runtime patch overlay for vLLM**: ~321 in-memory patches (~23 families)
applied at process startup through the `vllm.general_plugins` entry point.
Nothing is rewritten on disk — the server process is a stock `vllm serve`,
so in model-loader it is registered as a **catalog variant of kind `vllm`**
(id `sndr-vllm`), not a new `BackendKind`. All of the existing vLLM plumbing
applies unchanged: `buildVLLMArgs`, `PYTHONUNBUFFERED=1` injection, `/health`
readiness, monitor, proxy swap, watchdog.

Headline optimizations (upstream claims, validate locally): TurboQuant k8v4
KV-cache quantization, MTP speculative decoding (K=5), hybrid
Gated-DeltaNet/Mamba attention, CUDA graphs, FP8 dense+MoE, 256K context.
Reference rig 2× A5000 (Ampere SM 8.6 — same architecture as the RTX 3090):
Qwen3.6-35B-A3B FP8 ~240 tok/s (+53% vs stock vLLM).

## Why a separate checkout (and not the existing `vllm-nightly`)

SNDR follows a **strict vLLM pin** (`0.23.1rc1.dev424+g3f5a1e173`, rollback
`dev301`, ≤2-pin policy). The local `vllm-nightly` checkout is newer than the
pin, and the SNDR entry point auto-activates in **every** vLLM process of the
environment it is installed in. Installing the plugin into an existing venv
would therefore (a) run patches against an unvalidated vLLM version and
(b) silently patch the stock profiles, invalidating any A/B comparison.
`backends/sndr-vllm/` gets its own venv pinned to `dev424` — the same
isolation pattern as `llama.cpp-stable` vs `llama.cpp-nightly`.

## Prerequisites

- Python ≥ 3.12 (`python3.12` on PATH, or set `PYTHON_BIN`)
- NVIDIA driver ≥ 580.126.09 (SNDR's pinned wheels are torch 2.11+cu130)
- Ampere/Ada/Blackwell GPU (RTX 3090 / sm_86 is explicitly supported)

## Setup

```sh
./scripts/setup-sndr-backend.sh          # provisions backends/sndr-vllm/
```

The script is idempotent: creates the venv, installs the pinned vLLM nightly
(`--extra-index-url https://wheels.vllm.ai/nightly`), clones the SNDR repo,
installs the plugin editable (`pip install --no-deps -e .`, mirroring
upstream `install.sh`) plus runtime extras (`pandas scipy xxhash`), runs the
`python -m sndr.apply` patch smoke test, and writes the catalog executable
`backends/sndr-vllm/sndr-serve.sh`. Overrides: `SNDR_VLLM_PIN` (rollback:
`dev301` pin), `SNDR_REF`, `PYTHON_BIN`.

Register:

```sh
model-loader backend add sndr-vllm \
  --executable "$(pwd)/backends/sndr-vllm/sndr-serve.sh" --kind vllm
```

The schema is the standard vLLM one (`vllmhelp`); SNDR-specific tuning goes
through profile `launch.env` / `extraArgs`.

## Profiles

Duplicate an existing vLLM profile with a `-sndr` disambiguator (naming
convention §5 of `AGENTS.md`) and point it at the new backend:

```json
{
  "schemaVersion": 3,
  "id": "qwen3.6-27b-awq-mtp-fp8-sndr-tp2-256k",
  "name": "Qwen3.6 27B AWQ MTP FP8 (SNDR) tp2 256k",
  "model": "/models/Qwen3.6-27B-AWQ-MTP",
  "args": {
    "max-model-len": 262144,
    "tensor-parallel": 2,
    "served-model-name": "qwen3.6-27b-awq-mtp-fp8-sndr-tp2-256k"
  },
  "launch": { "backendId": "sndr-vllm" }
}
```

`served-model-name` must equal the profile id so the proxy's implicit swap
passes vLLM's model-name validation (same rule as the other vLLM profiles).

## Validation / A/B

Upstream numbers come from their rig — verify locally before promoting
`-sndr` profiles to defaults:

```sh
model-loader benchmark --profile qwen3.6-27b-awq-mtp-fp8-vllm-tp2-256k  --mode llama-bench
model-loader benchmark --profile qwen3.6-27b-awq-mtp-fp8-sndr-tp2-256k --mode llama-bench
model-loader benchmark --compare
```

## Updating / rollback

Re-run the setup script to update the plugin checkout (it re-runs the smoke
test against the pin). When SNDR advances its pin, bump `SNDR_VLLM_PIN` and
re-run; to roll back, set it to the previous pin (upstream keeps at most two
valid pins at a time). If `sndr.apply` fails, the checkout and the vLLM pin
disagree — pick a matching `SNDR_REF`/`SNDR_VLLM_PIN` pair from the SNDR
release notes.

## Security note

SNDR injects ~321 third-party runtime patches into the inference process.
The repo is Apache 2.0 and the proxy stays loopback-only, but review the
plugin source (small, pure-Python) before first run, and prefer pinning
`SNDR_REF` to a reviewed tag/commit rather than `main`.

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
`dev301` pin), `SNDR_WHEEL_INDEX`, `SNDR_REF`, `PYTHON_BIN`.

> ⚠ **Aged-out pin (validated 2026-07-03):** the default wheel index is the
> **rotating** `https://wheels.vllm.ai/nightly`, which keeps only the *latest*
> nightly. SNDR's pin (`dev424`) has already rotated out, so a fresh run fails
> with `No matching distribution found for vllm==…dev424…`. vLLM also publishes
> **persistent per-commit** wheels — set `SNDR_WHEEL_INDEX` to the pin's
> per-commit URL (full 40-char sha of the `+g<sha>` suffix). For `dev424`
> (`+g3f5a1e173`):
> `SNDR_WHEEL_INDEX=https://wheels.vllm.ai/3f5a1e1733200760169ff31ebe60a271072b199e/ ./scripts/setup-sndr-backend.sh`

Register:

```sh
model-loader backend add sndr-vllm \
  --executable "$(pwd)/backends/sndr-vllm/sndr-serve.sh" --kind vllm
```

The schema is the standard vLLM one (`vllmhelp`); SNDR-specific tuning goes
through profile `launch.env` / `extraArgs`.

## Profiles

Each profile points `launch.backendId` at `sndr-vllm`, sets
`served-model-name` == profile id (so the proxy's implicit swap passes vLLM's
model-name validation), and carries the SNDR env + serve flags from the
matching SNDR **ModelDef YAML** (`backends/sndr-vllm/sndr_core_engine/sndr/model_configs/builtin/model/*.yaml`).
The per-model `patches:` matrix (each `GENESIS_ENABLE_*` flag) goes into
`launch.env` verbatim; the serve flags (`--dtype float16`, `--kv-cache-dtype
turboquant_k8v4`, `--speculative-config '{"method":"mtp",...}'`,
`--tool-call-parser`, `--reasoning-parser`, `--override-generation-config`) go
into `args`. **`enable-prefix-caching` is intentionally omitted** (SNDR warns
prefix-cache + TurboQuant + spec-decode + ctx≥128K on hybrid GDN regresses −30%
TPS / OOMs the 27B).

**Two env vars are load-bearing and NOT in the model YAML `patches:` block —
add them to `launch.env` or every boot mis-behaves:**

- **`GENESIS_ENFORCE_VERSION_RANGE=1`** — MANDATORY (SNDR's own launcher
  requires it). Turns on the version-gate so patches capped to another vLLM
  range (e.g. `PN30`, obsolete on `dev424` because the upstream fused-postprocess
  kernel supersedes it) **skip cleanly** instead of hard-failing. Without it the
  boot summary reports `failed≥1`.
- **Dual-3090 / desktop headroom:** `gpu-memory-utilization 0.84` (0.86 OOMs
  GPU0 which runs the desktop), `GENESIS_ENABLE_PN95_TIER_AWARE_CACHE=0` (the
  YAML's `PN95_CONFIG_KEY=a5000-2x-tier-aware` is A5000-specific — TurboQuant
  already fits 262144 without it), plus `NCCL_P2P_DISABLE=1` +
  `disable-custom-all-reduce` + `distributed-executor-backend mp` (no NVLink).

The six profiles created 2026-07-03 (all `launch.backendId: sndr-vllm`,
`served-model-name` == id):

| Profile id | Model | ctx | KV / spec |
|---|---|---:|---|
| `qwen3.6-27b-int4-autoround-tq-mtp-sndr-tp2-256k` | Lorbus/Qwen3.6-27B-int4-AutoRound (exact SNDR prod model) | 262144 | TurboQuant k8v4 + MTP K=5 |
| `qwen3.6-27b-awq-mtp-tq-sndr-tp2-256k` | shawnw3i/Qwen3.6-27B-AWQ-MTP (A/B twin of the stock fp8 profile) | 262144 | TurboQuant k8v4 + MTP K=5 |
| `qwen3.6-27b-heretic-autoround-tq-mtp-sndr-tp2-256k` | lyf/Qwen3.6-27B-heretic-v2-mtp-int4-AutoRound | 262144 | TurboQuant k8v4 + MTP K=5 |
| `qwen3.6-35b-a3b-fp8-tq-mtp-sndr-tp2-256k` | Qwen/Qwen3.6-35B-A3B-FP8 (flagship `prod-qwen3.6-35b-balanced`) | 262144 | FP8 MoE + TurboQuant k8v4 + MTP K=5 |
| `gemma-4-31b-awq-mtp-sndr-tp2-64k` | cyankiwi/gemma-4-31B-it-AWQ-4bit + google/gemma-4-31B-it-assistant draft | 65536 | FP16 KV + MTP K=8 (external draft) |
| `gemma-4-31b-awq-kvauto-sndr-tp2-32k` | cyankiwi/gemma-4-31B-it-AWQ-4bit | 32768 | FP16 KV, no spec (chat) |

## Validated on the reference rig (2× RTX 3090, 2026-07-03)

All six profiles boot via the proxy with Genesis `failed=0` and coherent PT-BR
generation. Model weights live in the HF cache (`~/.cache/huggingface`, big disk)
symlinked under the `models.search_paths` root.

- **`qwen3.6-27b-int4-autoround-tq-mtp-sndr-tp2-256k`** (Lorbus flagship) — full
  **262144** ctx, **GPU KV 646,993 tokens → 2.47× concurrency** (vs 1.94× for the
  stock `fp8_e5m2` twin = **+27% KV** from TurboQuant k8v4), MTP acceptance 3.6–4.8,
  **~145 tok/s** single-stream. Keeps vision (fair A/B vs the stock vision profile).
- **`qwen3.6-35b-a3b-fp8-tq-mtp-sndr-tp2-256k`** — FP8 weights are ~18 GB/card, so
  with the vision tower loaded only ~0.48 GiB KV remains (max ctx ~69k). Adding
  **`--language-model-only`** (drop the vision tower, as SNDR's own launcher does)
  frees enough VRAM to reach the **full 262144** (KV 324,105 tokens → 1.24×),
  **~162 tok/s**. Row 28 is therefore text-only.
- **`gemma-4-31b-awq-mtp-sndr-tp2-64k`** — gemma-4 is multimodal; without
  `--language-model-only` the boot dies on `max_tokens_per_mm_item (2496) >
  max_num_batched_tokens` (the vision encoder budget). Text-only fixes it; MTP K=8
  via the external `Gemma4MTPModel` draft works (acceptance ~37% — lower than
  integrated MTP, expected for K=8 on one draft layer). KV 147,276 → 2.25× @64k.
- **`gemma-4-31b-awq-kvauto-sndr-tp2-32k`** — text-only, FP16 KV, no spec (chat).
- **`qwen3.6-27b-awq-mtp-tq-sndr-tp2-256k`** / **`…-heretic-autoround-…`** — same
  validated int4/awq + TurboQuant k8v4 + MTP K=5 config as the Lorbus flagship,
  different weights; both keep vision.

**Text-only rule:** the 35B + both gemma profiles carry `--language-model-only`
(VRAM / MM-budget); the three 27B keep vision so their A/B against the stock vision
profiles is apples-to-apples.

## ⚠ Large-prefill trap (measured 2026-07-03 — [BUGS.md N3](BUGS.md))

The `turboquant_k8v4` **continuation-prefill** path (patch `P38`,
`turboquant_attn.py::_prefill_attention` → `p38_tq_continuation_memory.py`) dequantizes
a **full-precision K buffer that grows with the prompt length**. At 262144 the KV cache
already claims the VRAM budget, so on the 24 GiB desktop card there is no headroom for
that scratch:

- `--mode llama-bench` at `gpu-memory-utilization 0.84`: **every prefill OOMs**
  (`torch.OutOfMemoryError` in `_genesis_continuation_prefill`, 502 to the client) —
  even the smallest `fill 5%` (~13k-token) preset.
- Dropping to `0.78` stops the OOM but a `fill 25%` (~65k-token) prefill **crawls at
  1.4 tok/s** with peak VRAM 23.9 GiB / util 99% — impractical.

So these `-sndr` 262144 profiles are **short-prompt / high-concurrency decode profiles**
(great: ~145 tok/s single-stream, 2.47× KV). They are **not** suitable for
coding-agent-size prompts (tens of k tokens) at 262144. For large-context work either
build a **≤131072** `-sndr` variant (smaller KV reservation → the continuation scratch
fits) or use the stock `fp8_e5m2` profile (no TurboQuant continuation buffer). The
lighter-KV gemma (FP16, 32–64k) and the MoE 35B are not affected the same way.

> **Zombie cleanup:** an OOM/crash mid-benchmark can leave orphaned `VLLM::Worker_TP*`
> processes holding VRAM (the proxy reports `loaded: ""` but `nvidia-smi` shows the
> memory in use), which then blocks the next boot. Kill them: `pkill -9 -f VLLM::Worker`.

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

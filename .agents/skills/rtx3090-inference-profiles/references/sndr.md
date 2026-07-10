# SNDR (Genesis) — `sndr-vllm` backend

> **Updated 2026-07-03** from the v12.0.0 `sndr-platform` codebase + library docs.
> Installed venv (`backends/sndr-vllm/.venv`, verified 2026-07-07): vLLM
> `0.23.1rc1.dev424+g3f5a1e173` + `sndr-platform 12.0.0` (editable). Upstream SNDR has
> since advanced its pin `dev424` → `dev714` (+290 commits; repo restructured into `sndr/`
> top-level package; K=5 MTP re-tune; DiffusionGemma at TP=2; 324 patches) — that is the
> UPSTREAM reference, NOT what is provisioned here. The 6 profiles built 2026-07-03 remain
> validated on the installed dev424 venv — upgrade notes below.

SNDR Core Engine ("Genesis", `github.com/Sandermage/sndr_core_engine`) is a **runtime
patch-overlay for vLLM** (324 in-memory patches via the `vllm.general_plugins` entry
point `genesis_v7`). In model-loader it is a catalog **variant of kind `vllm`** (id
`sndr-vllm`) — a stock `vllm serve` that is runtime-patched — so ALL the vLLM plumbing
(`buildVLLMArgs`, `PYTHONUNBUFFERED=1`, `/health`, proxy swap) applies unchanged.

**Installed vLLM (this rig, verified 2026-07-07):** `0.23.1rc1.dev424+g3f5a1e173` (commit
`3f5a1e173`) with `sndr-platform 12.0.0` installed editable — the provisioned venv at
`backends/sndr-vllm/.venv` (confirmed in its `.dist-info`; `docs/sndr-backend.md` documents
the same dev424 SNDR pin). **Upstream reference pin (2026-07-02, NOT installed here):**
`0.23.1rc1.dev714+g09663abde` (commit `09663abde`, +248 commits over dev424); upstream
rollback `0.23.1rc1.dev672+g93d8f834` (commit `93d8f834`), with `dev424`/`dev301` dropped
per its ≤2-pin policy. The local venv still runs dev424 — profiles boot fine but lose ~4%
TPS vs re-provisioning to dev714.

**v12.0.0 (`sndr-platform`):** the codebase moved from `vllm/sndr_core/` to a top-level
`sndr/` package with multi-engine architecture (vLLM today, SGLang skeleton). The vLLM
entry point is still `genesis_v7`; `GENESIS_ENABLE_*` env vars still work (deprecated
alias of `SNDR_ENABLE_*`). New env var `SNDR_ENGINE=vllm` is set by the launcher — the
model-loader `sndr-serve.sh` wrapper already handles this if reprovisioned. For
model-loader **bare-metal profiles** (not Docker), the import-path change is invisible
— `vllm serve` boots, the plugin auto-loads, patches apply in-memory. Nothing to change
in `launch.env`.

**Never** install the plugin into the stock `vllm-*` venvs (the entry point auto-patches
EVERY vLLM process of that env). Provisioned & validated 2026-07-03.

## When to pick it
- You want SNDR's **TurboQuant k8v4** KV quant (~+27% KV concurrency vs `fp8_e5m2` —
  measured 2.47× vs 1.94× @262144 on a 27B) + **integrated MTP K=5**, for
  **short-prompt / high-concurrency decode** on qwen3_5 / qwen3_5_moe / gemma4.
  Reference A5000 numbers: 35B-MoE **239.7 TPS** (+15.8% vs K=3), 27B-int4 **127.4 TPS**
  (+8.2% vs K=3), tool-call 7/7 on both.
- **Do NOT pick it for large-context (coding-agent) prompts at 262144** — see the
  large-prefill trap below. For big prompts use a ≤131072 variant or the stock fp8 backend.
- It is an A/B / research backend, not the default. The catalog also has `vllm-stable`,
  `vllm-nightly` (kind `vllm`) — pick those for stock behavior.

## SNDR model catalog (v12.0.0 registry)

| Model | Quant | KV cache | Spec-decode | Status |
| --- | --- | --- | --- | --- |
| Qwen3.6-35B-A3B-FP8 | FP8 dense MoE | TurboQuant k8v4 | MTP K=5 | ✅ PROD (default) |
| Qwen3.6-27B-int4-AutoRound | INT4 AutoRound (hybrid GDN+Mamba) | TurboQuant k8v4 | MTP K=5 | ✅ PROD |
| Gemma-4-31B | INT4 / kv-auto | TurboQuant or uniform fp16 | MTP K=3 (separate drafter) | ✅ PROD — native MTP now works (G4_67→G4_81 verify; ~40.7 t/s TQ, ~70.1 t/s kv-auto) |
| DiffusionGemma-26B-A4B-FP8 | FP8-dynamic block-diffusion MoE | TP=2 | — | ✅ serving at TP=2 (first block-diffusion FP8-MoE on consumer Ampere) |

## Provisioning gotchas (both cost real debugging)
1. **Pinned wheel ages out.** `scripts/setup-sndr-backend.sh` installs the SNDR-pinned
   vLLM. The default index `https://wheels.vllm.ai/nightly` is **rotating** (latest only),
   so the pinned build is long gone → `No matching distribution`. vLLM keeps **persistent
   per-commit** wheels. Current pin: `SNDR_WHEEL_INDEX=https://wheels.vllm.ai/09663abde.../`
   (dev714 = commit `09663abde`). Previous: `.../3f5a1e173.../` (dev424). The pin is
   correct; only the index was wrong (BUGS.md N1). **To re-provision to dev714:** update
   the wheel URL in the setup script, re-run, then `model-loader backend schema refresh
   sndr-vllm`. Existing dev424 profiles boot on dev714 without changes.
2. Register after provisioning: `model-loader backend add sndr-vllm --executable
   .../backends/sndr-vllm/sndr-serve.sh --kind vllm` then `backend schema refresh
   sndr-vllm` (reuses the curated vLLM schema — no new schema package).

## Building a profile
Config source of truth = the SNDR **ModelDef YAML**
`backends/sndr-vllm/sndr_core_engine/sndr/model_configs/builtin/model/<model>.yaml`
(the `sndr model-config` CLI can't find its own configs — read the YAML directly).

- **`launch.env`** = the YAML's `patches:` dict (52–97 `GENESIS_ENABLE_*`/tuning vars)
  **verbatim** + the ~23 system/VLLM vars from `tools/launcher_templates/*.sh`
  (`PYTORCH_CUDA_ALLOC_CONF=expandable_segments:True,max_split_size_mb:256`,
  `VLLM_USE_FLASHINFER_SAMPLER=1`, `VLLM_WORKER_MULTIPROC_METHOD=spawn`,
  `NCCL_P2P_DISABLE=1`, `CUDA_DEVICE_ORDER=PCI_BUS_ID`, …). PLUS the two load-bearing
  vars below. Note: `GENESIS_ENABLE_*` is the deprecated alias of `SNDR_ENABLE_*` —
  both resolve identically, keep using `GENESIS_ENABLE_*` for now (backward compat).
- **`args`** = `dtype: float16`, `kv-cache-dtype: turboquant_k8v4`, `max-model-len`,
  `tensor-parallel-size: 2`, `distributed-executor-backend: mp`,
  `disable-custom-all-reduce: true`, `enable-chunked-prefill: true`, `max-num-seqs: 2`,
  `max-num-batched-tokens: 4096`, `speculative-config:
  {"method":"mtp","num_speculative_tokens":5}` (integrated MTP K=5; gemma uses an external
  `Gemma4MTPModel` draft: add `"model":"<assistant path>"`, K=3 optimal for separate
  drafter), `tool-call-parser`, `reasoning-parser`, `override-generation-config`,
  `served-model-name` == id.
  **OMIT `enable-prefix-caching`** (SNDR: prefix-cache + TQ + spec + ctx≥128K on hybrid
  GDN → −30% TPS / OOM). A ready generator lives in a scratchpad from the 2026-07-03
  session; regenerate by parsing the YAML.

**P2P policy (patched rig):** SNDR is a `vllm` variant, so it inherits the vLLM TP2 **P2P-on**
default (dual-gpu.md §P2P) — `NCCL_P2P_DISABLE=1` (an SNDR launcher-template default) and
`disable-custom-all-reduce: true` are now the **stock/fallback** setting, not the target. For a P2P-on
A/B, drop `NCCL_P2P_DISABLE` and treat `disable-custom-all-reduce` as A/B, then **prove the NCCL-P2P /
custom-AR path from the launch log** (never a throughput delta). This does NOT waive SNDR's
large-prefill safety test (below) — always `benchmark --mode llama-bench` a big prefill before
trusting a `-sndr` profile, P2P or not.

### Two env vars NOT in the YAML `patches:` block — add them or every boot misbehaves
- **`GENESIS_ENFORCE_VERSION_RANGE=1`** — MANDATORY (SNDR's launcher requires it,
  `decision.py:461`, default OFF). Turns on the version-gate so a patch capped to another
  vLLM range (e.g. `PN30`, obsolete on current pin — the upstream fused-postprocess kernel
  supersedes it) **skips cleanly** instead of hard-failing. Without it the boot summary
  reports `failed≥1` (BUGS.md N2). A clean boot = `register() complete: applied=N
  skipped=M failed=0 (apply=True)`.
- **`GENESIS_ENABLE_PN95_TIER_AWARE_CACHE=0`** — the YAML's `PN95_CONFIG_KEY=
  a5000-2x-tier-aware` is A5000-specific (CPU-KV tier offload); leave it on and PN95
  looks up a missing config. TurboQuant already fits 262144 without it on the 3090.

## Dual-3090 tuning
- **`gpu-memory-utilization: 0.84`**, NOT 0.90/0.92 (SNDR's A5000 values). 0.86 fails the
  load-time free-memory check on GPU0 (desktop): "Free memory 20.16 < desired 20.26 GiB".
- **`--language-model-only`** (drop the vision tower) on heavy / multimodal models: the
  35B-A3B-FP8 (~18 GB/card weights) only reaches 262144 by freeing the vision VRAM for KV
  (KV 324k tokens / 1.24× with it, vs an estimated max ~69k without); gemma-4 (multimodal)
  **requires** it or the boot dies on `max_tokens_per_mm_item (2496) > max_num_batched_tokens`.
  The 27B qwen3_5 fit 262144 WITH vision, so keep vision there (fair A/B vs stock vision
  profiles). SNDR's own 27B launcher template uses `--language-model-only` regardless.

## ⚠ THE LARGE-PREFILL TRAP (measured — the #1 SNDR gotcha, BUGS.md N3)
`turboquant_k8v4`'s **continuation-prefill** (patch `P38`,
`p38_tq_continuation_memory.py`) dequantizes a **full-precision K buffer that grows with
the prompt length**. At 262144 the KV cache claims the whole `gpu-memory-utilization`
budget → no headroom for that scratch on a 24 GiB card:
- `benchmark --mode llama-bench` @ **0.84**: EVERY prefill OOMs
  (`torch.OutOfMemoryError` in `_genesis_continuation_prefill` → **502** to the client),
  even `fill 5%` (~13k tokens).
- @ **0.78**: no OOM but `fill 25%` (~65k tokens) **crawls at 1.4 tok/s**, peak 23.9 GiB.

**New in dev714:** patch **PN401** (#46461, P0 TQ prefill-continuation guard) adds a
prefill-size sanity check before the continuation buffer is allocated — it may prevent
the OOM path but does NOT eliminate the throughput cliff at large prompts. The large-prefill
trap still applies at 262144.

So the `-sndr` 262144 profiles are **short-prompt / high-concurrency ONLY** (great there:
~145 tok/s single-stream on 3090, 2.47× KV). For coding-agent-size prompts, build a
**≤131072** `-sndr` variant (smaller KV reservation → the continuation scratch fits) or
use the stock `fp8_e5m2` profile. My short "capital of X" validations (~20 tokens) masked
this — always run one large-prefill (`benchmark --mode llama-bench`) before trusting an
SNDR profile.
**Zombie cleanup:** an OOM mid-run leaks `VLLM::Worker_TP*` procs holding VRAM (proxy says
`loaded:""`, `nvidia-smi` shows the memory) → next boot fails "not healthy". Kill with
`pkill -9 -f VLLM::Worker`.

## The 6 profiles built 2026-07-03 (all validated, `failed=0`, coherent, dev424 venv)
| id | model | ctx | notes |
|---|---|---:|---|
| `qwen3.6-27b-int4-autoround-tq-mtp-sndr-tp2-256k` | Lorbus int4 (exact SNDR model) | 262144 | flagship, 2.47× KV, ~145 tok/s, vision |
| `qwen3.6-27b-awq-mtp-tq-sndr-tp2-256k` | shawnw3i AWQ-MTP | 262144 | A/B twin of stock fp8, vision |
| `qwen3.6-27b-heretic-autoround-tq-mtp-sndr-tp2-256k` | lyf heretic int4 | 262144 | vision |
| `qwen3.6-35b-a3b-fp8-tq-mtp-sndr-tp2-256k` | Qwen 35B-A3B-FP8 | 262144 | MoE, ~162 tok/s, **text-only** |
| `gemma-4-31b-awq-mtp-sndr-tp2-64k` | cyankiwi gemma AWQ + assistant draft | 65536 | FP16 KV, MTP K=8 external draft (~37% acc), **text-only** |
| `gemma-4-31b-awq-kvauto-sndr-tp2-32k` | cyankiwi gemma AWQ | 32768 | FP16 KV, no spec, **text-only** |

Runbook: `docs/sndr-backend.md`. Backend commit `8588b0a` + the 2026-07-03 materialization.
**To upgrade to dev714:** re-run `scripts/setup-sndr-backend.sh` with the updated wheel
SHA, `backend schema refresh sndr-vllm`, then re-validate each profile. Expect ~4% TPS
gain on 35B, neutral on 27B (per upstream changelog dev301→dev424 bump numbers;
dev424→dev714 carries decode forward with no regression).

## SNDR v12 persistent memory (neural-graph)
New in v12: a CPU-only **neural-graph memory** (Postgres + pgvector) that auto-connects
knowledge nodes, recalls via vector similarity + spreading activation, and decays like
human memory. Ships as one container alongside the GPU engines. **Not relevant for
model-loader profiles** (the memory engine is a separate container), but worth knowing:
if you point the memory gateway at the model-loader proxy, every model gains persistent
contextual memory. See `docs/memory/MANUAL.md` in the SNDR repo.

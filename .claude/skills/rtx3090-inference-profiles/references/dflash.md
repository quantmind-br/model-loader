# DFlash (lucebox) on RTX 3090 24GB

Standalone native C++ speculative-decoding runtime (`lucebox-dflash` catalog entry,
`backends/lucebox-hub/server/build-rtx3090/dflash_server`; schema `lucebox-dflash.json`, ~54 flags;
docs `backends/lucebox-hub/server/README.md` + `RESULTS.md`). Highest tok/s ceiling on this
card for supported targets (~130–160 tok/s vs ~38 autoregressive), but narrow:
**Qwen3.5/Qwen3.6 27B-class targets only**, and the server rejects unknown options.

**Multi-GPU IS supported** (the old "single-GPU only" note was wrong — the schema exposes
`target-device`, `target-devices`+`target-layer-split`, `draft-device`, `peer-access`). MEASURED
2026-06-29 on 2×3090 (no NVLink/P2P) with the official Lucebox pair — Qwen3.6-27B-Q4_K_M target
(`unsloth/Qwen3.6-27B-GGUF`) + `dflash-draft-3.6-q4_k_m` draft — batch-1 **code** decode, accept 49.4%:

| Topology | flags | tok/s | note |
|---|---|--:|---|
| **draft-split (USE THIS for 2 GPUs)** | `target-device cuda:1` + `draft-device cuda:0` | **74 @256k** | = single-GPU speed; draft on GPU0 frees the target card for full 256k KV |
| single-GPU (pin GPU1) | `CUDA_VISIBLE_DEVICES=1` | 75.6 @32k | baseline; same speed |
| target layer-split | `target-devices cuda:0,cuda:1` + `target-layer-split 1,1` | 56 @32k / **40 @256k** | −26%…−46% — F32 boundary activations cross PCIe w/o P2P |
| layer-split + `--peer-access` | + `peer-access true` | 55.8 | no-op: loads `peer_access=ON`, neither helps nor crashes |
| layer-split q8_0 KV @256k | `+ cache-type-k/v q8_0` | 37.7 | the ONE niche for layer-split: quality KV that won't fit one card |

**Bottom line:** the right way to use both 3090s for dflash is **draft-split** (whole target on the clean
GPU1, draft on GPU0) — it keeps single-GPU speed AND frees ~2 GiB on the target card. **Never layer-split
the target for speed** (PCIe-bound without P2P); split weight (`1,1` vs `0.45,0.55`) is irrelevant.
Plain single-GPU (pin GPU1, `CUDA_VISIBLE_DEVICES=1`) is fine when you don't need both cards. See
references/dual-gpu.md.

## Ampere kernels (sm_86) — build, not profile flags

Luce DFlash has **no** llama.cpp-style `GGML_CUDA_FORCE_CUBLAS` toggle. Throughput on a 3090 depends on compiling **ggml-cuda + DDTree CUDA** for **compute 8.6**, not on a runtime “kernel mode” flag.

| Source | sm_86 on 3090? |
|---|---|
| Official Docker (`DFLASH_CUDA_ARCHES=75;80;86;89;90;120`) | **Yes** (fat binary) |
| Default host `cmake -B build` (no arch pin) | Wide arch list; OK if 86 is in the list |
| Stale/wrong cache (e.g. `CMAKE_CUDA_ARCHITECTURES=75` only) | **No** — Turing-only SASS; suboptimal or JIT on 3090 |

**Recommended on this rig:** from `backends/lucebox-hub/server/`:

```bash
./build-rtx3090.sh          # → build-rtx3090/dflash_server (default CUDA_ARCH=86-real)
# catalog executable (model-loader backends/catalog.json):
# …/server/build-rtx3090/dflash_server
```

Portable multi-GPU lab build: `CUDA_ARCH='75;80;86;89;90' ./build-rtx3090.sh` (slower compile, larger binary). Megakernel / NVFP4 paths are **sm_120+** only — not the 3090 path.

Runtime defaults already tuned for Ampere: GPU draft top-K (`DFLASH_GPU_DRAFT_TOPK=1`), GPU verify argmax; optional `DFLASH_FP_USE_BSA=1` for sparse FA prefill (sm_80+). Profile knobs remain `ddtree-budget`, `fa-window`, draft-split devices — see above.


## Profile shape (differs from every other backend)

- Target model = **bare positional arg** → goes in the profile `model` field ONLY (local GGUF
  path; `target`/`model` keys in args are skipped by the arg builder).
- `"draft"` in args is **required** — DFlash draft GGUF. Official Lucebox pair: target
  `unsloth/Qwen3.6-27B-GGUF/Qwen3.6-27B-Q4_K_M.gguf` + draft
  `Lucebox/Qwen3.6-27B-DFlash-GGUF/dflash-draft-3.6-q4_k_m.gguf` (1.1 GB, the README default; the
  q8_0 1.8 GB draft measured no better — 49.1% vs 49.4% accept). Without a draft the server is
  pointless (no speculation). The Lucebox repo ships ONLY the draft; the target is a separate model.
- `"host": "127.0.0.1"` — the binary defaults to 0.0.0.0.
- `"model-name": "<profile-id>"` — cheap insurance for proxy routing (default "dflash").
- Sampling is per-request (default temperature 0 = bit-exact greedy; DDTree skeleton stays
  argmax). No sampling flags in the profile.

```json
// 2-GPU draft-split (recommended): whole target on clean GPU1, draft on GPU0 → 74 tok/s @256k.
// NO CUDA_VISIBLE_DEVICES mask (both cards must be visible); CUDA_DEVICE_ORDER pins cuda:0=GPU0/cuda:1=GPU1.
{
  "schemaVersion": 3, "id": "<model>-lucebox-dflash-<ctx>k", "name": "<Name> (Lucebox DFlash, <ctx>k)",
  "model": "/home/diogo/models/huggingface/unsloth/Qwen3.6-27B-GGUF/Qwen3.6-27B-Q4_K_M.gguf",
  "args": {
    "draft": "/home/diogo/models/huggingface/Lucebox/Qwen3.6-27B-DFlash-GGUF/dflash-draft-3.6-q4_k_m.gguf",
    "host": "127.0.0.1", "model-name": "<profile-id>",
    "max-ctx": 262144, "chunk": 512,
    "ddtree": true, "ddtree-budget": 22,
    "draft-swa": 2048, "fa-window": 2048,
    "target-device": "cuda:1", "draft-device": "cuda:0"
  },
  "launch": { "defaultBackground": true, "backendId": "lucebox-dflash",
              "env": [{"key":"CUDA_DEVICE_ORDER","value":"PCI_BUS_ID"}],
              "restart_policy": "none", "max_restarts": 3, "backoff_seconds": 5 },
  "pinned": false
}
// Single-GPU variant: drop target-device/draft-device, add env CUDA_VISIBLE_DEVICES=1 (same 75 tok/s).
```

## Tuning on this card

- **`max-ctx` is NOT a free-to-max knob** — the opposite of llama-family `ctx-size`. Oversizing
  actively slows prefill (FA strides over unused KV; ~27× slower at 4× oversize per
  RESULTS.md). Size it to the real workload; this is the one backend where the context
  adjustment rule leans DOWN by default.
- KV: `cache-type-k/v` enum f16…q8_0 + `tq3_0`. Unset default = tq3_0 when max-ctx > 6144,
  else q4_0. tq3_0 (3.5 bpv) reaches ~256K on 24 GB; q4_0 path tops out near 128K. Quality
  order: q8_0 → q5_0/q4_0 → tq3_0. Unsupported K/V pairs abort at allocation with a printed
  list — read it.
- `fa-window: 2048` is documented **lossless** (100% acceptance at all window sizes) — keep it;
  `fa-window: 0` (full attention) collapses decode to ~25 tok/s at 60k+.
- `ddtree: true`, `ddtree-budget: 22` (schema: "22 on RTX 3090, 40 on RTX 5090; re-sweep per
  card"). `draft-swa: 2048` for unsloth Qwen3.6 targets.
- Reply budgeting shapes thinking models: `default-max-tokens` 16000, `think-max-tokens` 15488,
  `hard-limit-reply-budget` 4096 — raise for long generations.
- VRAM: ~16 GiB Q4_K_M target + ~1.8 GiB draft + KV + DDTree state. If tight:
  `draft-residency: request-scoped` frees draft VRAM between requests.
- Disk prefix cache: enabled only when `kv-cache-dir` is set (`kv-cache-budget` 4096 MB) —
  useful for agents replaying long contexts.

## Measured anchors

**2026-06-29, official Lucebox pair (unsloth Qwen3.6-27B-Q4_K_M + Lucebox dflash-draft-3.6-q4_k_m),
2×3090 no-NVLink, batch-1 `merge_intervals` code prompt, ddtree-budget 22, fa-window 2048, KV tq3_0:**
- **draft-split (target GPU1 + draft GPU0) @256k: 74 tok/s**, accept 49.4%, avg_commit 8.83. VRAM
  GPU1 19.7 idle / **23.6 peak** (prefill plateaus 40k→84k, only ~1 GiB headroom — 256k is the safe
  native max, not more); GPU0 4.3 (draft). Same 75.6 tok/s on plain single-GPU @32k.
- layer-split the target: 56 @32k, **40 @256k** (tq3_0) / 37.7 @256k (q8_0 KV). Avoid for speed.
- Prefill walls (this pair): 40k ≈ 73 s, 84k ≈ 208 s, 128K ≈ ~10 min — expect long TTFT at depth.
- `fa-window 2048` kept decode fast at 256k but **trimmed mid-context needle recall** (a 42k-deep
  needle came back partial) — for needle/tool workloads use `fa-window 0` (slower decode).

**Older RESULTS.md anchor (different prompt mix):** HumanEval ~129.5 tok/s mean / peak 158 at
ddtree-budget 16 — higher than the 74 above because HumanEval's short, highly-predictable completions
accept far more than a from-scratch function-with-tests prompt. Treat 74 as the conservative anchor.

## When NOT to use dflash

Non-Qwen3.5/3.6 targets; vision (no mmproj); sampling-sensitive workloads needing server-side
defaults; contexts beyond 256K; models without a compatible Lucebox/DFlash draft.

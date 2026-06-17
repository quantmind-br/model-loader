# DFlash (lucebox) on RTX 3090 24GB

Standalone native C++ speculative-decoding runtime (`lucebox-dflash` catalog entry,
`backends/lucebox-hub/server/build/dflash_server`; schema `lucebox-dflash.json`, ~54 flags;
docs `backends/lucebox-hub/server/README.md` + `RESULTS.md`). Highest tok/s ceiling on this
card for supported targets (~130–160 tok/s vs ~38 autoregressive), but narrow:
**Qwen3.5/Qwen3.6 27B-class targets only**, and the server rejects unknown options.

## Profile shape (differs from every other backend)

- Target model = **bare positional arg** → goes in the profile `model` field ONLY (local GGUF
  path; `target`/`model` keys in args are skipped by the arg builder).
- `"draft"` in args is **required** — DFlash draft GGUF, e.g.
  `/home/diogo/models/huggingface/Lucebox/Qwen3.6-27B-DFlash-GGUF/dflash-draft-3.6-q8_0.gguf`.
  Without it the server is pointless (no speculation).
- `"host": "127.0.0.1"` — the binary defaults to 0.0.0.0.
- `"model-name": "<profile-id>"` — cheap insurance for proxy routing (default "dflash").
- Sampling is per-request (default temperature 0 = bit-exact greedy; DDTree skeleton stays
  argmax). No sampling flags in the profile.

```json
{
  "schemaVersion": 3, "id": "<model>-dflash-<ctx>k", "name": "<Name> (DFlash, <ctx>k)",
  "model": "/abs/path/Qwen3.6-27B-Q4_K_M.gguf",
  "args": {
    "draft": "/abs/path/dflash-draft-3.6-q8_0.gguf",
    "host": "127.0.0.1", "model-name": "<profile-id>",
    "max-ctx": 65536, "chunk": 512,
    "ddtree": true, "ddtree-budget": 22,
    "draft-swa": 2048, "fa-window": 2048
  },
  "launch": { "defaultBackground": true, "backendId": "lucebox-dflash",
              "restart_policy": "none", "max_restarts": 3, "backoff_seconds": 5 },
  "pinned": false
}
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

## Measured anchors (this card, RESULTS.md + prior profile)

- HumanEval: ~129.5 tok/s mean (3.43× the 37.8 tok/s AR baseline), peak 158; 128K-mode mean
  ~134.8 at ddtree-budget 16 + fa-window 2048.
- Prefill walls: 32K ≈ 38 s, 64K ≈ 126 s, 128K ≈ ~10 min — set expectations accordingly.
- Proven recipe: Q4_K_M target + Lucebox q8_0 draft, ddtree-budget 22, draft-swa 2048,
  fa-window 2048, max-ctx 65536.

## When NOT to use dflash

Non-Qwen3.5/3.6 targets; vision (no mmproj); sampling-sensitive workloads needing server-side
defaults; contexts beyond 256K; models without a compatible Lucebox/DFlash draft.

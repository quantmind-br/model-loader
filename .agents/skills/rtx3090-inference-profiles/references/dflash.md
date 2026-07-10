# lucebox dflash_server — DFlash + DDTree + PFlash + KVFlash + Spark (catalog id `lucebox-dflash`)

Standalone native C++/CUDA runtime (`backends/lucebox-hub/server/build-rtx3090/dflash_server`,
sm_86-real build (`CUDA_ARCH=86-real`) via `backend-build.sh`; schema `lucebox-dflash.json`, 64 flags; docs:
`server/README.md`, `RESULTS.md`, `docs/PREFIX_CACHE.md`, `optimizations/{spark,kvflash,pflash}/`).
Batch-1 only, **Qwen3.5/3.6 27B-class (`qwen35` arch), Laguna and Gemma4 targets**; the server
rejects unknown options. Highest tok/s ceiling on one card for supported targets
(HumanEval ~129.5 tok/s on Qwen3.5-27B vs ~38 autoregressive), and the ONLY backend here with
speculative prefill (PFlash), bounded-KV long context (KVFlash) and calibrated MoE expert
offload (Spark). Serves OpenAI Chat/Responses + Anthropic Messages natively.

## The four technologies (what to reach for, when)

| Tech | Problem it solves | Flag(s) | Measured (RTX 3090) |
|---|---|---|---|
| **DFlash + DDTree** | decode speed | `--draft <gguf> --ddtree --ddtree-budget 22` | Qwen3.5-27B HumanEval 129.5 tok/s (3.43×); Qwen3.6 pair ~74–78 (drafter still training) |
| **PFlash** (speculative prefill) | TTFT at 64–128k+ (the #1 agent pain) | `--prefill-drafter <Qwen3-0.6B-BF16.gguf>` (+`--prefill-compression` = LOSSY, see below) | 128K cold TTFT **24.8 s vs ~257 s** llama.cpp (~10.4×), NIAH preserved |
| **KVFlash** (bounded KV residency) | long-ctx decode collapse + KV VRAM | `--kvflash auto` (+`--prefill-drafter` for drafter-scored residency) | 256K at **flat 38.6 tok/s** with 72 MiB resident KV (vs 13.1 full-cache); needle 14–15/16 |
| **Spark** (MoE expert residency) | MoE bigger than VRAM | `--spark` (+`--spark-vram <GiB>`) | 33B-A3B MoE in **14.6 GiB @ ~100 tok/s** (all-GPU 119, naive offload 66); Qwen3.6-35B-A3B 13.3 GiB @ 100 (92% kept) |

They compose: PFlash's drafter doubles as KVFlash's relevance scorer. All are opt-in flags on
the same binary.

### PFlash notes
- Speculative prefill = drafter scores token importance; the target prefills only what matters.
  `--prefill-keep-ratio` default 0.05; `--prefill-threshold`/`--prefill-curve` shape selection.
- **`--prefill-compression` is LOSSY prompt compression — NEVER on tool-calling/agent profiles**
  (drops spans the drafter deems unimportant; tool definitions/system prompts are exactly what
  gets hurt). The TTFT win with retrieval intact comes from the default PFlash span selection —
  validate with a needle probe on YOUR workload before trusting at depth.
- Drafter: `Qwen3-0.6B-BF16.gguf` (auto-probed from model dir, `drafter/`, `draft/`,
  `/opt/lucebox/models/drafter/`; pass `--prefill-drafter` explicitly so it never silently
  falls back).

### KVFlash notes
- `--kvflash <tokens|auto>` (auto = pool sized from free VRAM, capped 16384);
  `--kvflash-policy drafter|lru|qk`; `--kvflash-tau 64` reselect interval.
- **Which policy, by target arch** (`optimizations/kvflash/{README,DESIGN}.md`): `drafter` is the
  default when a `Qwen3-0.6B-BF16.gguf` loads (banner `policy=drafter`) — qwen35/qwen35moe feed the
  drafter their token ids directly (accurate); laguna/gemma4 bridge the tokenizer gap with the
  cross-tok scorer (functional but untuned). `qk` is a drafter-free scorer over the target's own
  post-RoPE QK geometry — no drafter VRAM, ~0.2 s rescore, recall ~2× the drafter scorer @20% keep —
  **prefer it on laguna/gemma4 or any target without a matching drafter**. `lru` (recency-only) is
  the fallback when no drafter is found.
- Decode is flat at any context (per-step KV read is pool-sized); cold chunks page to host RAM
  bit-exact. Needle recall 88–100% at 6% residency — good, not perfect: for exhaustive
  retrieval-over-context workloads keep full cache instead.
- Contrast with `--fa-window N` (sliding-window attention): fa-window **drops** far context
  from attention permanently (help text warns it loses system prompt/tools at long ctx) —
  **`--fa-window 0` is mandatory for agent/tool profiles**; KVFlash keeps everything recallable.
  (fa-window 2048 remains a legitimate speed knob for non-agent, needle-free chat.)

### Spark notes (MoE — `laguna` and `qwen35moe` targets)
- `--spark` self-tunes: bounded LRU expert cache + auto-loaded placement profile
  (`<model>.gguf.spark.csv`, persisted from live traffic — first boot uniform, warms per run).
  `--spark-vram <GiB>` caps total VRAM (verified 13/15 → peak 11.6/13.8 GiB); `--spark-slots N`
  overrides cache auto-size.
- This is the rig's ONE sanctioned expert-offload path (the no-`--n-cpu-moe` policy stands for
  llama.cpp): calibrated placement + cache ≈ 85–92% of all-GPU speed vs 55% naive. Calibrate on
  the traffic you serve (profile from chat under-performs on pure code: 6.6%→16% cold-miss).
- Use when: a MoE must share the card with something else (second model, huge KV), or a
  >24 GiB-at-Q4 MoE (e.g. Q8 35B-A3B on ONE card) matters more than peak tok/s.

## Multi-GPU (measured 2026-06-29, official Lucebox pair, accept 49.4%)

| Topology | flags | tok/s | note |
|---|---|--:|---|
| **draft-split (USE for 2 GPUs)** | `"target-device":"cuda:1","draft-device":"cuda:0"` | **74 @256k** | = single-GPU speed; frees ~2 GiB on the target card for full 256k KV |
| single-GPU (pin GPU1) | env `CUDA_VISIBLE_DEVICES=1` | 75.6 @32k | baseline |
| target layer-split | `target-devices cuda:0,cuda:1` + `target-layer-split 1,1` | 56 @32k / 40 @256k | −26…−46% (F32 over PCIe) — **measured pre-P2P**; rebenchmark with `--peer-access` on the patched driver before trusting; only niche: q8_0 KV @256k (37.7) |
| `--peer-access` | — | **A/B (rebenchmark)** | pre-P2P it was a no-op (P2P driver-disabled); on the patched driver 610.43.02 it has a working substrate — prove peer access in the log + A/B vs draft-split, which stays the baseline until it wins |

No `CUDA_VISIBLE_DEVICES` mask when using draft-split (both cards must be visible);
`CUDA_DEVICE_ORDER=PCI_BUS_ID` in `launch.env`.

## Profile shape (differs from every other backend)

- Target model = **bare positional** → profile `model` field ONLY (local GGUF path).
- `"draft"` in args **required** for DFlash decode (Lucebox pair: target
  `unsloth/Qwen3.6-27B-GGUF/Qwen3.6-27B-Q4_K_M.gguf` + draft
  `Lucebox/Qwen3.6-27B-DFlash-GGUF/dflash-draft-3.6-q4_k_m.gguf`; q8_0 draft measured no better).
  Spark-only MoE serving can run draftless (no Laguna drafter published yet).
- `"host": "127.0.0.1"`; `"model-name": "<profile-id>"` (proxy routing insurance).
- Sampling is per-request only (`temp/top_p/top_k/rep_pen[window 256]/freq/presence`; temp 0 =
  argmax fast path). No min_p/DRY — anti-loop = client-side `frequency_penalty` 0.2–0.5.
- Template: hardcoded per arch (Qwen3 ChatML + tool preamble, Laguna, Gemma4);
  `--chat-template-file` for nonstandard finetunes. Thinking default **OFF** — enable via
  request `chat_template_kwargs` for thinking profiles. Reply budgets:
  `--think-max-tokens` (15488), `--hard-limit-reply-budget` (4096), per-effort
  `--reasoning-effort-{low,medium,high,x-high,max}` mapped from the request's effort field.

```json
// 2-GPU draft-split agent profile (74 tok/s @256k) with disk prefix cache
{
  "schemaVersion": 3, "id": "<model>-lucebox-dflash-<ctx>k", "name": "<Name> (Lucebox DFlash, <ctx>k)",
  "model": "/home/diogo/models/huggingface/unsloth/Qwen3.6-27B-GGUF/Qwen3.6-27B-Q4_K_M.gguf",
  "args": {
    "draft": "/home/diogo/models/huggingface/Lucebox/Qwen3.6-27B-DFlash-GGUF/dflash-draft-3.6-q4_k_m.gguf",
    "host": "127.0.0.1", "model-name": "<profile-id>",
    "max-ctx": 262144, "chunk": 512,
    "ddtree": true, "ddtree-budget": 22,
    "draft-swa": 2048, "fa-window": 0,
    "prefix-cache-slots": 32,
    "kv-cache-dir": "/var/tmp/dflash-kv", "kv-cache-budget": 16384, "disk-prefix-cache": "auto:8",
    "target-device": "cuda:1", "draft-device": "cuda:0"
  },
  "launch": { "defaultBackground": true, "backendId": "lucebox-dflash",
              "env": [{"key":"CUDA_DEVICE_ORDER","value":"PCI_BUS_ID"}],
              "restart_policy": "none", "max_restarts": 3, "backoff_seconds": 5 },
  "pinned": false
}
// Single-GPU: drop target/draft-device, add env CUDA_VISIBLE_DEVICES=1.
// Chat (non-agent) speed variant: "fa-window": 2048 (decode stays fast at 256k; costs deep-needle recall).
```

## Tuning on this card

- **`max-ctx` is NOT free-to-max** — oversizing slows prefill (~27× at 4× oversize). Size to the
  real workload; the one backend where the context rule leans DOWN by default (unless KVFlash,
  which decouples decode cost from ctx).
- KV: `cache-type-k/v` f16…q8_0 + `tq3_0` (default tq3_0 when max-ctx > 6144). tq3_0 reaches
  ~256K on 24 GB; quality order q8_0 → q5_0/q4_0 → tq3_0. Unsupported pairs abort with a
  printed list — read it.
- `ddtree-budget 22` = swept 3090 optimum; **drop to 16 at 128K+**; never copy 4090/5090 values.
- Prefill walls without PFlash (this pair): 40k ≈ 73 s, 84k ≈ 208 s, 128K ≈ ~10 min — add
  `--prefill-drafter` for agent profiles with deep contexts.
- **Prefix caching for agents:** `--prefix-cache-slots 32` (default; turn-boundary KV snapshots
  in system RAM) + `--kv-cache-dir` disk tier (`--disk-prefix-cache auto:N` diffs recent
  requests to find the stable prefix — right when the agent frontend injects volatile headers).
  Disk tier survives restarts/swaps — the only cross-restart warmth on this backend.
  `--disk-prefix-cache-compress` (FlowKV) is experimental/lossy — skip for agents.
- VRAM: ~16 GiB Q4_K_M target + ~1.1 GiB draft + KV + DDTree state; `draft-residency
  request-scoped` frees draft VRAM between requests if tight. 256k peak measured 23.6 GiB on
  GPU1 (draft-split) — 256k is the safe max, not more.
- Other flags worth knowing: `--lazy-draft` (defer draft load), `--fast-rollback` (default on;
  `--no-fast-rollback` to debug), `--verify-width` (tree verify batch), `--spark-*` (above),
  `--kvflash-*` (above), `--freq` (telemetry interval).

## When NOT to use dflash_server

Non-Qwen3.5/3.6/Laguna/Gemma4 targets; vision (no mmproj); workloads needing server-side
sampling defaults; needle-exhaustive retrieval with KVFlash/fa-window active; contexts beyond
256K. For plain GGUF + DFlash without the lucebox extras, beellama is the more general host —
upstream llama.cpp's `draft-dflash` now validates directly in a profile's `args` (the `spec-type`
enum is list-valued; `draft-dflash` is in the live `llama.cpp-stable.json` enum), and beellama
spells the same technique `"spec-type": "dflash"` — see speculative.md.

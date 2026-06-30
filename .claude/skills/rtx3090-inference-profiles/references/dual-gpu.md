# Dual RTX 3090 (48 GiB, no NVLink, PCIe PHB) — multi-GPU, pinning, the 48 GiB tier

Reference rig: 2 × RTX 3090. **NVLink inactive** (no bridge), GPUs connect **PHB** (through the CPU
root complex, so GeForce GPU↔GPU P2P is driver-disabled → NCCL/collectives stage through host RAM).
**GPU0 drives the desktop** (~0.6–1 GiB → ~22.4 GiB usable); **GPU1 is clean** (~23.2 GiB usable).
**VRAM cap 46 GiB total** (≈23 GiB/card). Single-user / batch-1 interactive workload.

## Bottom line

**Default to single-GPU for safety. The second 3090 buys capacity, concurrency — and, via llama.cpp
`split-mode tensor`, genuine single-stream speed (+26…+39%) for a fitting ≤31B dense model.**

- **Model + target-ctx KV fits ≤23 GiB → ONE GPU (GPU1) is the safe default** (determinism, VRAM
  predictability, ~350 W less draw). But on this rig the **split mode decides single-stream speed**,
  measured batch-1, ≤31B dense, q4_0 KV, llama.cpp:
  - **`split-mode tensor` — FASTEST, +26…+39% vs single** (Qwythos-9B +39%, gemma-4-31B +26%, +20% vs
    layer; gain grows with prefill depth). It splits every matmul → **both cards' VRAM bandwidth in
    parallel per token**, and the per-layer all-reduce is cheap at hidden≈4096. Catch: **crashes with
    an external draft + forces CPU sampling** (see llama-server section below).
  - **`split-mode layer` — +4…+7%** (Qwythos-9B +3.8%, Qwopus-27B +7.3%, Ornith-35B-A3B MoE +4.0%):
    sequential pipeline → thermal relief only, no bandwidth parallelism. Safe + compatible with drafts.
  - **`split-mode row` — −2.8×** (Ornith 147→52.8 t/s): deprecated, avoid.
  - The widely-cited "TP is −3…−6% on no-NVLink" (1-GPU 111.7 → 2-GPU 108.1
    ([insiderllm](https://insiderllm.com/guides/multi-gpu-local-ai/))) is **vLLM's custom-all-reduce
    TP and/or high-concurrency aggregate — a different code path**; llama.cpp `split-mode tensor` does
    NOT behave that way for single-user decode here.
- **Model + KV > 24 GiB but ≤46 GiB → 2-GPU, as a CAPACITY decision.** Expect flat-to-+55% tok/s,
  never 2×. **Prefer pipeline/layer split over tensor/row split** — matches vLLM's official
  "no NVLINK → use pipeline parallelism" guidance
  ([vLLM docs](https://docs.vllm.ai/en/stable/serving/parallelism_scaling/)). Reserve dense **TP=2
  for prefill-heavy/long-prompt** jobs, where the per-layer all-reduce pays off — not for decode.
- **MoE tolerates TP=2 well.** Expert-parallel comm volume ≪ a dense model of equal size, so the
  no-NVLink penalty is minimal — `--tensor-parallel-size 2 --enable-expert-parallel` is the sweet
  spot for 30B/35B-A3B-class models that overflow one card (and for the MoE that already fits, the
  2nd card buys Q8/longer ctx instead).
- **Two models, one per GPU (pin-per-GPU)** is the best way to use both cards at once: zero
  NCCL/NVLink/PCIe cross-talk, each a fully independent single-GPU job.

**Why TP loses here:** TP does ~2 all-reduce collectives **per layer** (after attention, after MLP)
— ~160/token on an 80-layer 70B. PP transfers activations **once per stage boundary**. Over PHB
(P2P disabled, host-RAM bounce at PCIe4 ~32 GB/s/dir — ~16 on x8 — vs NVLink ~56 GB/s/dir) that per-layer tax flips TP
from a latency-reducer to a latency-adder ([llama.cpp multi-gpu](https://github.com/ggml-org/llama.cpp/blob/master/docs/multi-gpu.md)).
The famous "+48% with NVLink" is **aggregate throughput under ~200 concurrent prompts** on a fitting
model, not single-stream — batch-1 NVLink gain is marginal
([vLLM bench](http://himeshp.blogspot.com/2025/03/vllm-performance-benchmarks-4x-rtx-3090.html)).

## Per-backend recipe

### llama-server / beellama-cpp (GGUF) — preferred for single-user 2-GPU dense
**Measured speed ranking on this rig (batch-1, ≤31B dense, q4_0 KV): `split-mode tensor` +26…39% >
`split-mode layer` +4…7% > single-GPU > `split-mode row` −2.8×.** Row is officially
**deprecated/superseded** ([discussion #11485](https://github.com/ggml-org/llama.cpp/discussions/11485)).
`tensor` is the genuine tensor-parallel path and the fastest for single-user decode here (parallel
bandwidth on both cards) — with the hard caveats listed below; `layer` is the safe, compatible mode
(works with drafts, MoE, and >24 GiB pooling). The jsonc below shows `layer` for the pooling case;
swap `"split-mode": "tensor"` for the speed case on a fitting ≤31B dense model with no external draft.

```jsonc
// args for a model that overflows one card → pool 48 GiB via layer split (the default)
"split-mode": "layer",                 // do NOT use "row"
"n-gpu-layers": 99,
"flash-attn": "on",                    // KV savings + speed on Ampere
"cache-type-k": "q8_0", "cache-type-v": "q8_0",  // quantized KV to stretch the pool
"tensor-split": "0.45,0.55",           // device-index order: device0=GPU0 (desktop) LESS, device1=GPU1 (clean) MORE
"main-gpu": 1                          // prompt-processing / non-split tensors on the clean GPU1
```
- `tensor-split` proportions are in **device-index order** (`0.45,0.55` → device0 lighter, device1
  heavier), so set `launch.env` `CUDA_DEVICE_ORDER=PCI_BUS_ID` in the split case too — it makes
  device0=GPU0 / device1=GPU1 deterministic for two identical cards, so the heavier share and
  `main-gpu 1` actually land on the clean card. Wrong order silently overloads the desktop GPU0.
- **No `CUDA_VISIBLE_DEVICES` mask when you genuinely split** — both cards must be visible; balance
  with `tensor-split` + `main-gpu`. (`--device CUDA0,CUDA1` / `--list-devices` to enumerate.)
- **`--split-mode tensor` is the FASTEST mode here on DENSE, not a trap** — measured **+26…+39%** vs
  single for batch-1 decode (≤31B dense), ~+20% over layer, because it parallelizes both cards' VRAM
  bandwidth per token. **On a MoE it's FLAT** (Ornith-35B-A3B: tensor 150.3 = +2% vs single 147.2,
  −2% vs layer 153.3) — A3B reads only ~3B params/token, so there's little per-token bandwidth to
  parallelize and expert-gather adds cross-GPU traffic; for MoE use single-GPU or `layer`. q4_0 KV **works** (the old "requires F16/BF16 KV, OOMs near 32k" from
  [ik_llama #1247](https://github.com/ikawrakow/ik_llama.cpp/discussions/1247) does NOT reproduce in
  this build — that was forced-f16; tested 9B@256k + 31B@32k, no OOM). `-fa on` still required. **Three
  hard caveats:** (1) it **crashes with an external speculative draft and there is NO known fix** —
  `ggml_abort` in `common_speculative_create_ctx_dft`; a known **open upstream bug**
  ([#22473](https://github.com/ggml-org/llama.cpp/discussions/22473),
  [#24309](https://github.com/ggml-org/llama.cpp/issues/24309); WIP PR #22400, unmerged). beellama
  rejects it explicitly ("DFlash … cannot pin the target output tensor while target split-mode tensor
  is active") because **DFlash shares the main model's output tensor**, unpinnable under tensor-split;
  `--device-draft` / `--spec-draft-device` / `--override-tensor` do **not** work around it — tested all
  4 ways on Qwen3.6-27B (auto, device-draft CUDA0/CUDA1, override-tensor pin): every one `ggml_abort`s
  or is rejected. So any profile needing an external draft (DFlash, or an external-MTP gguf) **cannot
  use tensor — and that's fine, because keeping the draft wins anyway:** measured Qwen3.6-27B (q4_0 KV,
  128k) **DFlash layer-split 79.1 t/s > DFlash single 74.9 ≫ tensor-no-draft 48.2** (−36%). The
  speculative draft's ~2× beats tensor's ~1.26× bandwidth gain, and they're mutually exclusive →
  **for any external-draft profile, use `layer` (keeps the draft + a small bonus), never tensor.**
  An **internal MTP head** (`spec-type draft-mtp`, no draft *model*) is **model-dependent under tensor,
  not a reliable exception**: Qwythos-9B (Q8_0, nextn blk.32) runs +35% (spec sampling on CPU), but
  **Qwen3.6-27B-MTP (UD-Q4_K_XL, nextn blk.64) loads + inits the MTP ctx then CRASHES on the first
  token** — `GGML_ASSERT(src_ss[i].axis != SPLIT_AXIS_UNKNOWN)` in the nextn head (same #24309 family).
  So MTP-under-tensor must be validated per model; the robust 2-GPU path for any draft is `layer`
  (measured Qwen3.6-27B: DFlash-layer 82.8 > tensor-no-spec 49.0 = +69% at max ctx; tensor+MTP crashed).
  (2) it **forces CPU sampling** (`backend sampling
  not supported with SPLIT_MODE_TENSOR`) — still net faster. (3) **no `llama_params_fit`** → set
  `n-gpu-layers` explicitly (99). Untested >31B (all-reduce volume may grow). **Validate a real
  generation** — passed coherent incl. non-English on 9B/31B (no #20052).
- **Never set `GGML_CUDA_P2P=1`** on PHB — PCIe-speed anyway, "may crash or corrupt output on some
  motherboards/BIOS."
- ⚠ **Long-ctx correctness:** [#20052](https://github.com/ggml-org/llama.cpp/issues/20052) reports
  even *layer* split producing garbage above ~2048 ctx on a non-P2P PCIe dual-3090 with asymmetric
  slot widths. **Validate a long generation, not just startup.**
- **MoE:** prefer fitting the whole model across both cards via layer split over expert offload
  (keep the no-`--n-cpu-moe` policy). MTP/DFlash speculation still works under **layer** split —
  confirm the draft loads in the log. It does **NOT** work under `split-mode tensor` (the draft graph
  aborts), so any speculative profile that wants 2-GPU must use `layer`.

### vLLM (HF / AWQ / GPTQ)
```jsonc
"args": {
  "served-model-name": "<profile-id>", "host": "127.0.0.1",
  "tensor-parallel-size": 2,
  "distributed-executor-backend": "mp",   // mp is default+correct for 2 local GPUs; not ray
  "disable-custom-all-reduce": true,       // GeForce custom all-reduce needs P2P/NVLink — pass explicitly
  "gpu-memory-utilization": 0.90           // per-GPU; with TP=2 each card frees ~half → KV ~doubles
}
```
- Launch hangs at `using nccl==…`? add `launch.env`: `NCCL_P2P_DISABLE=1` (and if still stuck
  `NCCL_SHM_DISABLE=1`). Necessity is board/BIOS(ACS/IOMMU)-dependent
  ([vLLM #7588](https://github.com/vllm-project/vllm/issues/7588)).
- **Dense, latency-sensitive single-user → `"pipeline-parallel-size": 2`** instead of TP.
- **MoE → `"tensor-parallel-size": 2, "enable-expert-parallel": true`** (much lower PCIe traffic).
- ⚠ **Corruption bug** [#40725](https://github.com/vllm-project/vllm/issues/40725): TP>1 on PCIe
  3090 garbled **non-English** output; `NCCL_P2P_DISABLE=1` did *not* fix it; same HW was clean under
  SGLang/llama.cpp. **Validate a non-English prompt**; if garbled, try PP or fall back.
- Measured working dual-3090 PCIe template (Qwen3.6-27B, vLLM 0.19): `tensor-parallel-size 2,
  disable-custom-all-reduce, gpu-memory-utilization 0.83, kv-cache-dtype fp8, max-model-len 160000,
  attention-backend FLASHINFER, max-num-seqs 4` → ~117–124 t/s
  ([Derek Armstrong](https://derekarmstrong.dev/blog/running-qwen36-27b-dual-rtx-3090-vllm-v019/)).

### SGLang
```jsonc
"args": {
  "served-model-name": "<profile-id>", "host": "127.0.0.1",
  "tp-size": 2,
  "disable-custom-all-reduce": true,   // else "peer access is not supported between these two devices"
  "attention-backend": "flashinfer",   // Ampere default; never fa3 (Hopper); triton as fallback
  "mem-fraction-static": 0.88
}
```
- env `NCCL_P2P_DISABLE=1` if it hangs at "Init torch distributed". Crashes confirmed without the
  flag ([#991](https://github.com/sgl-project/sglang/issues/991)).
- **Model FITS one card → don't use `tp-size 2`** — use single GPU, or **`"dp-size": 2`** (one full
  replica per GPU, zero inter-GPU traffic, ~2× aggregate throughput). SGLang guide: "favor data
  parallelism for throughput when there is enough GPU memory."

### lucebox-dflash
**Multi-GPU IS supported** (`target-device`, `target-devices`+`target-layer-split`, `draft-device`,
`peer-access`). MEASURED 2026-06-29 (Qwen3.6-27B-Q4_K_M + Lucebox draft, batch-1 code decode):
- **Draft-split is the right 2-GPU mode: whole target on clean GPU1 + DFlash draft on GPU0**
  (`"target-device":"cuda:1","draft-device":"cuda:0"`, no `CUDA_VISIBLE_DEVICES` mask,
  `CUDA_DEVICE_ORDER=PCI_BUS_ID`). **74 tok/s @256k = single-GPU speed**, and moving the ~1 GiB draft
  off the target card frees it for the full 262144 KV (GPU1 23.6 GiB peak, ~1 GiB headroom; GPU0 4.3).
- **Do NOT layer-split the target** (`--target-devices cuda:0,cuda:1`) for speed — it forces F32
  boundary activations across PCIe without P2P: **−26% @32k (56 t/s), −46% @256k (40 t/s)**. Split
  weight (`1,1` vs `0.45,0.55`) is irrelevant (PCIe-bound). Its ONE use: q8_0 KV @256k (37.7 t/s),
  quality KV that won't fit one card.
- **`--peer-access` is a no-op here** — loads with `peer_access=ON` (no GNS crash) but changes nothing
  (GeForce P2P driver-disabled). Plain single-GPU (pin GPU1) is also fine when you don't need both cards.

### unsloth
The `unsloth-rtx3090` backend wraps `unsloth studio run --silent --yes --no-cloudflare -H 127.0.0.1`
(it re-execs into its own `~/.unsloth/studio` venv — no local `.venv`). **Single-GPU; no first-class
tensor-parallel exposure** → treat as single-GPU and pin via `launch.env`
`CUDA_DEVICE_ORDER=PCI_BUS_ID` + `CUDA_VISIBLE_DEVICES=1`.
- `model` accepts an **HF repo id** (e.g. `unsloth/…`) or a local GGUF/dir; the schema exposes
  `tensor-parallel`/`parallel` (passed to its internal engine) — leave them unset on this rig.
- Auth: each boot mints a fresh `sk-unsloth-…` token printed to the log; the proxy captures it from
  the launch log automatically (the `WaitReady` unsloth variant). Nothing to hard-code in the profile.
- Some Studio models (e.g. DiffusionGemma's visual server) need `unsloth studio update` once to fetch
  the right binary before the profile will launch — a one-time host step, not a profile field.
- If it overflows one card, it does NOT layer-split like llama.cpp — pick a quant that fits ~23 GiB
  or switch the GGUF to llama-cpp-default for genuine 2-GPU pooling.

### NCCL / P2P sanity check (run once before blaming a profile)
```
nvidia-smi topo -m          # expect PHB
nvidia-smi topo -p2p r      # expect NS (P2P not supported) on this rig
nvidia-smi --query-gpu=index,pcie.link.gen.max,pcie.link.width.max --format=csv
# both cards SAME gen×width? an asymmetric topology (e.g. GPU0 Gen4 x16 + GPU1 Gen3 x4) is the
# documented trigger for llama.cpp #20052 layer-split garbage — verify symmetry before splitting.
```

## Pin-per-GPU (two models at once)

The cleanest way to use both cards for two live models. No NCCL, no NVLink, no TP config — each
backend is an independent single-GPU process. PCIe contention is negligible at steady state (decode
is on-card memory-bandwidth bound; PCIe only matters during weight load).

**Profile side — set both in `launch.env[]`** (model-loader injects per-process):
```
CUDA_DEVICE_ORDER=PCI_BUS_ID     # deterministic for two IDENTICAL 3090s (default FASTEST_FIRST can't tie-break)
CUDA_VISIBLE_DEVICES=1           # GPU1 = clean card → primary/heavy model
# second profile: CUDA_VISIBLE_DEVICES=0   → GPU0 = desktop + secondary model
```
**JSON format gotcha:** `launch.env` is an array of `{"key":"…","value":"…"}` objects, NOT `"KEY=VALUE"`
strings — e.g. `"env": [{"key":"CUDA_DEVICE_ORDER","value":"PCI_BUS_ID"},{"key":"CUDA_VISIBLE_DEVICES","value":"1"}]`.
A `"KEY=VALUE"` string fails `profile create` with `cannot unmarshal string into … EnvVar`.
- Confirm physical layout: `nvidia-smi --query-gpu=index,pci.bus_id --format=csv`.
- **Gotcha:** `CUDA_VISIBLE_DEVICES` *remaps* — the masked card becomes `cuda:0` inside the process.
  So leave the backend's own device flag at default: do **NOT** also set llama.cpp `-mg 1` /
  `--device CUDA1` or sglang `--base-gpu-id 1`. Keep `tensor-parallel-size 1` / no split.
- Heavy/latency-critical model on **GPU1** (~23.2 GiB). Secondary on **GPU0** — budget the ~0.6–1 GiB
  desktop (~22.4 GiB usable): lower its vllm `gpu-memory-utilization` or llama.cpp ctx.

**Runtime reality — the proxy is single-active.** `model-loader instance start` and the `:4321`
proxy load ONE model and **swap** (loading the 2nd evicts the 1st). To keep BOTH live at once, run a
second headless proxy on another port for the secondary model:
```
model-loader serve --port 4321   # primary (loads the GPU1-pinned profile on demand)
model-loader serve --port 4322   # secondary (loads the GPU0-pinned profile on demand)
```
`serve` does **not** take the single-instance lock, so two proxies coexist; each profile still lands
on the GPU fixed in its `launch.env`, independent of which proxy launched it. (3090 has no MIG;
process isolation via `CUDA_VISIBLE_DEVICES` is the partitioning mechanism, MPS the only spatial
share if you ever co-locate two models on one card.)

## Models worth the 2nd GPU (48 GiB tier, ≤46 GiB cap)

Footprints in GiB; tok/s = **single-user batch-1 decode** on 2×3090, no NVLink.

| Tier | Examples | Weights | Ctx headroom | tok/s | Verdict |
|---|---|---|--:|--:|---|
| **70B dense int4** | Llama-3.3-70B, DeepSeek-R1-Distill-70B (AWQ/GPTQ ~42–45) | ~42–45 | only ~1–4 GiB → ~8–16k fp16 KV; 32k needs q8/q4 KV | ~15–21 | Max single-machine quality; FIT win, ~2× slower than a 32B. llama.cpp layer-split. |
| **72B** | Qwen2.5-72B Q4 (~50) | ~50 | — | — | **Over the 46 GiB cap** at Q4 — needs IQ3/Q3 + short ctx, or skip. |
| **32B at Q8** | Qwen3-32B, QwQ-32B (Q8 ~34; bf16 ~65 does NOT fit) | ~34 | generous | ~60–81 | The real 48 GiB "quality" play — Q8 ≈ 98% of bf16. |
| **27B at Q6/Q8** | Qwen3.6-27B-class UD-Q8 | ~28–34 | long ctx + draft | ~67–81 | Best ROI of the 2nd card — near-lossless quant *and* fast. |
| **35B-A3B MoE** | Ornith-1.0-35B, Qwen3.6-35B-A3B (Q4 ~21 fits one card) | ~21 (Q4) / ~37 (Q8) | huge (hybrid KV) | 100+ (A3B) | Q4 fits GPU1 alone; 2nd card buys **Q8 (~37) or 256k ctx**. MoE+EP tolerates no-NVLink best. |
| **Small-expert MoE** | Mixtral-8x7B int4 | ~24–27 | long ctx / higher quant | high | Was tight on one card; comfy across two. |
| **NOT viable** | Qwen3-235B-A22B (≥112 even Q3), Mixtral-8x22B (~73), gpt-oss-120b (~60, needs CPU offload) | — | — | — | Don't generate profiles (no-`--n-cpu-moe` policy). |

**"70B-int4-on-2 vs 32B-on-1":** 70B int4 ≈ 15–21 t/s, stuck near Q4, short ctx, max quality ceiling.
Dense 32B int4/Q8 on 1 card ≈ 30–81 t/s with a free 2nd card. With 2025–26 32B models closing the
gap, **pick 70B only for a single big generalist / hard reasoning**; otherwise 32B-Q8 or an A3B MoE
wins on latency + flexibility.

## Power & thermal

Two stock 3090s ≈ 700 W continuous, 600 W+ transient spikes per card → a 1200 W PSU is marginal.

- **Power-limit each card to 280 W** (`sudo nvidia-smi -pl 280` on both indices). ≈560 W combined,
  costs only a few % tok/s, avoids OCP trips. Curve (aggregate): 220 W = peak tokens/joule (~10%
  slower); **275–280 W ≈ 98% of full**; <220 W loses ~18%
  ([bench](http://himeshp.blogspot.com/2025/03/vllm-performance-benchmarks-4x-rtx-3090.html)).
  Single-user decode (memory-bound) loses *less* than these compute numbers.
- **The throttle limit is the GDDR6X memory-junction (hotspot), not the core** — PAM4 runs VRAM hot;
  throttle ~105–110 °C drops mem clock ~20% (and tok/s). Target hotspot **<90–95 °C**. Monitor
  explicitly: `nvidia-smi -q -d TEMPERATURE` / `temperature.memory` (hotspot reads 20–30 °C above
  core). Two stacked open-air cards = worst case (bottom exhausts into top's intake) → **2–3 slot
  spacing + strong directed airflow**; if the top card stays >95 °C, replace thermal pads.
- Linux has no true voltage-curve undervolt; substitute `nvidia-smi -pl 280` (+ optional locked
  clocks `nvidia-smi -lgc`).

## VRAM budget

| Card | Raw | Desktop | Usable |
|---|--:|--:|--:|
| GPU1 (clean) | 24 | — | ~23.2 |
| GPU0 (desktop) | 24 | ~0.6–1 | ~22.4 |
| **Aggregate (layer-split, capped)** | 48 | ~1 | **≤46** |

- vllm/sglang `gpu-memory-utilization`/`mem-fraction-static` is **per-GPU, after weight sharding**.
  With TP=2 each card holds ~half the weights, so per-card KV ~doubles vs one card. Keep 0.85–0.92 on
  the display-driving GPU0.
- Reserve headroom for activations + buffers, and it is **backend-specific**: ~5–8 GiB/card for
  **vLLM/SGLang** (CUDA-graph capture) but only ~0.5–1.5 GiB/card for **llama.cpp layer-split**
  (much smaller compute buffers). So a 70B int4 (~42–45 GiB) leaving ~1–4 GiB for KV is fine under
  llama.cpp layer-split, but would not fit the same way under a Python engine's graph reserve. TP
  overhead is compute/comm, not a fixed VRAM tax — don't budget the full 46 GiB for weights+KV.
- Pin-per-GPU → budget each card independently against its usable figure (23.2 / 22.4), not the pool.

## Myths to avoid

- **"TP is always faster" / "TP is always slower on no-NVLink."** Both wrong — it's
  **implementation-specific**. llama.cpp **`split-mode tensor` is +26…+39%** here for batch-1 ≤31B
  decode (a genuine win); vLLM **custom-all-reduce TP is −3…−6%** for a fitting model (helps mainly
  prefill / high-concurrency); llama.cpp **`split-mode row` is −2.8×**. Don't generalize one engine's
  TP result to another.
- **"Layer-split slows decode like TP does."** False on this rig — measured flat-to-+7% (llama.cpp +
  beellama; 9B/27B dense + 35B-A3B MoE), because pipeline/layer split crosses the GPU boundary only
  once per layer-block and the lighter per-card load lifts sustained clocks. Output stayed coherent
  incl. non-English (no #20052). Pin single-GPU for determinism/VRAM, never because layer-split is slower.
- **"NVLink is mandatory."** No — but it's the only no-patch way to real host-bypassing P2P on a
  3090. A bridge (~$60–120) helps **only** vLLM/SGLang TP=2 or long-prefill, ~0% for llama.cpp
  layer-split chat. The user runs PCIe-only by choice.
- **Bandwidth:** 3090 NVLink ≈ 56 GB/s/dir (112 aggregate) vs PCIe4 x16 ≈ 32 GB/s/dir → ~1.75×, NOT
  3.5×. Ignore any "~600 GB/s for 3090 NVLink" (that's A100-class).
- **"row and tensor split are the same slow thing."** They are NOT. For llama.cpp on THIS rig: **`row`
  is deprecated and −2.8×** (avoid); **`split-mode tensor` is the FASTEST mode, +26…+39%** for
  single-user ≤31B decode (q4_0 KV works, no OOM at the ctx tested) — it beats layer because it
  parallelizes both cards' memory bandwidth, and the per-layer all-reduce is cheap at these hidden
  sizes even without NVLink. Tensor mode's real catch isn't speed — it's that it **crashes with an
  external draft** and forces CPU sampling (see llama-server section).
- **"fp8 is free speed on 3090."** SM86 has no fp8 compute; fp8 KV is storage-only. Never set sglang
  `attention-backend fa3` (Hopper-only).
- **"More GPUs = proportionally more tok/s."** Not proportional: llama.cpp **layer**-split 70B Q4 ≈
  16–17 t/s across 2×/4×/6× — flat (added cards = capacity, not decode speed). The exception is
  `split-mode tensor` on a fitting ≤31B model — the 2nd card's bandwidth genuinely buys +26…+39%
  single-stream — but that's a one-time 2-card bandwidth gain, still not N× scaling.
- **"NCCL_P2P_DISABLE=1 fixes consumer-GPU corruption."** Not always (vLLM #40725). Treat
  `disable-custom-all-reduce` + `NCCL_P2P_DISABLE=1` as conditional insurance, and **validate output
  correctness** (long / non-English) before trusting any 2-GPU profile.

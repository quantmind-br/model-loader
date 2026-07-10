# Dual RTX 3090 (48 GiB, no NVLink, PCIe PHB) — multi-GPU, pinning, the 48 GiB tier

Reference rig: 2 × RTX 3090, **patched NVIDIA open driver `610.43.02`**. **NVLink inactive** (no
bridge), GPUs connect **PHB**. **PCIe P2P is enabled and validated** (§P2P below): CUDA peer access
works both directions, measured peer copies GPU0→GPU1 **13.34** / GPU1→GPU0 **13.15 GB/s**, two-GPU
NCCL all-reduce verified — collectives are no longer forced through host RAM, but PHB still caps
bandwidth (no NVLink) and P2P fixes no correctness bug. Boot policy `iommu=pt` + ACS-redirect-disable
is what makes it work (at the cost of DMA isolation). **GPU0 drives the desktop** (~0.6–1 GiB →
~22.4 GiB usable), OLD vBIOS (BAR1 = 256 MiB); **GPU1 clean** (~23.2 GiB usable), ReBAR vBIOS
(BAR1 = 32 GiB) — measured 2026-07-02. **VRAM cap 46 GiB total** (≈23 GiB/card). Single-user, batch-1.

## Bottom line

**Default to single-GPU (pin GPU1). The second card buys capacity, a second model, and — via
real tensor parallelism (llama.cpp `-sm tensor`, exllama TP) on DENSE models — genuine
single-stream speed.** Measured split-mode ranking (batch-1, ≤31B dense, q4_0 KV, llama.cpp):

- **`split-mode tensor` — FASTEST at short ctx, +22…+39% vs single** (Qwythos-9B +39%,
  gemma-4-31B +26%; both cards' VRAM bandwidth in parallel per token). **The gain shrinks with
  context and reverses ≥~512k** (Qwythos-9B Q8_0 MTP: +22% @256k, −11% @768k) — and upstream's
  own numbers show **prompt processing REGRESSES under tensor (PP 0.39–0.76×)**, so for
  100k+-prompt agents benchmark TTFT before adopting. At the extreme it buys ctx impossible on
  one card (Qwythos-9B: 1M @157.9 tok/s — single-GPU OOMs).
- **`split-mode layer` — +4…+7%** (sequential pipeline, thermal relief). Safe, compatible with
  drafts/MoE/quantized KV — the default multi-GPU mode.
- **`split-mode row` — −2.8×**, deprecated. Never.
- **MoE under tensor: FLAT** (Ornith-35B-A3B +2%) — only ~3B active params/token to
  parallelize. MoE → single-GPU or layer.
- **exllama TP (native backend): dense +25…32%, A3B-MoE −20%** — and uniquely coexists with
  MTP/DFlash drafts (exllama-tabby.md).
- **vLLM/SGLang TP=2 = capacity** (per-card KV ~doubles); batch-1 decode gain comes from
  spec-decode + fp8-KV headroom, not the TP itself.

> These split rankings were measured **pre-P2P** (stock driver). P2P (now validated, §P2P) is an
> **additional A/B lever on top** — it may lift layer/tensor collective comm, but rebenchmark with
> launch-log proof; it does not change the model-class rules (MoE/long-ctx/drafts) or the
> row/asymmetric bans, and never auto-splits a fitting single-GPU model.

## llama.cpp `-sm tensor` — status (upstream PR #19378, merged 2026-04; in b9847, NCCL build ON)

Hard caveats, all verified current at b9847 (2026-07):
1. **Crashes/hangs with ANY external draft, MTP head or DFlash — no fix upstream** (#22473
   silent stops, open; #24309 load-crash on nextn tensors, open; #24440; PR #22400 did NOT fix
   it). beellama rejects it explicitly. Models carrying a nextn head crash **at load** —
   strip with llama-quantize or don't use tensor. **And keeping the draft wins anyway:**
   Qwen3.6-27B DFlash-layer 79–83 tok/s ≫ tensor-no-draft 48–49 (−36%). Internal MTP under
   tensor is model-dependent: Qwythos-9B works (+22% @256k), Qwen3.6-27B-MTP crashes — validate
   per model. Chainable draftless `ngram-mod` is the spec that survives tensor mode.
2. **Forces CPU sampling** (`backend sampling not supported with SPLIT_MODE_TENSOR`) — still
   net faster.
3. **No `--fit`** → set `n-gpu-layers` and `ctx-size` manually.
4. KV quant: upstream docs claim f16-only, but **q4_0 KV measured WORKING in the local builds**
   (9B @256k/768k/1M, 31B @32k, coherent output) — re-validate after every rebuild; if a new
   build rejects quantized KV, that's the documented upstream behavior arriving.
5. `flash-attn on` required. Untested >31B here.

```jsonc
// pooling case (model overflows one card) — layer split, the default
"split-mode": "layer", "n-gpu-layers": 99, "flash-attn": "on",
"cache-type-k": "q8_0", "cache-type-v": "q8_0",
"tensor-split": "0.5,0.5",   // REQUIRED: equal split only (never 0.45,0.55 — operator policy)
"main-gpu": 1
// speed case (fitting ≤31B dense, NO drafts): swap to "split-mode": "tensor", size ctx manually
```
- `launch.env` `CUDA_DEVICE_ORDER=PCI_BUS_ID` always (two identical cards — without it
  FASTEST_FIRST can flip the mapping and overload the desktop GPU0).
- **No `CUDA_VISIBLE_DEVICES` mask when genuinely splitting** — both cards visible; use
  **equal** `tensor-split` + optional `main-gpu 1`.
- **`GGML_CUDA_P2P=1` is a real, source-verified env var** — `getenv("GGML_CUDA_P2P")` in
  `ggml/src/ggml-cuda/ggml-cuda.cu` (docs `multi-gpu.md`/`build.md`), present in
  **llama.cpp-stable/nightly, beellama and buun** (NOT ik-llama-cpp, which auto-enables peer access
  via its own `ggml_cuda_set_peer_access`; NOT lucebox, which uses `--peer-access`). These builds are
  compiled **NCCL-ON** (`-DGGML_USE_NCCL`), so peer access is already enabled for the NCCL comm path;
  `GGML_CUDA_P2P` mainly grants peer access to the **non-NCCL VMM copy path**. On the patched driver it
  is a legitimate **split-only A/B lever** (no effect on a single-GPU pin — no peer to reach), but
  upstream still warns it **can crash/corrupt on some IOMMU/BIOS setups** — prove engagement in the log
  and validate a long/non-English generation; unset it if unstable. Never claim it for a backend
  without source/binary proof.
- ⚠ Long-ctx correctness: #20052 (layer-split garbage >2048 ctx on non-P2P rigs) was CLOSED as a
  marginal PCIe riser cable degrading signal integrity (Xid 79 bus drops), NOT a llama.cpp bug
  (github.com/ggml-org/llama.cpp/issues/20052) — the lesson stands: **validate a long /
  non-English generation, not just startup**, and suspect the PCIe link/cable first if it garbles.

## vLLM / SGLang TP2 (details in vllm-sglang.md)

- vLLM: `tensor-parallel-size 2, distributed-executor-backend mp`. **P2P-on default (patched rig):**
  **no `NCCL_P2P_DISABLE`**, and **do not preemptively `disable-custom-all-reduce`** — custom AR is now
  an A/B decision (P2P is validated, so it can genuinely engage). Prove the NCCL-P2P / custom-AR path
  from the launch log, never from a throughput delta or the absence of a disable flag. util 0.84–0.90.
  **Stock-driver / diagnostic fallback only:** re-add `NCCL_P2P_DISABLE=1` + `disable-custom-all-reduce
  true` (on the stock driver custom-AR self-disables after the P2P probe, so disabling it was merely
  deterministic).
- **`enable-expert-parallel`: OFF for A3B-class sparse MoE at batch-1** (EP adds 7–12% overhead
  on ultra-sparse MoE; DeepEP fast paths are Hopper+NVLink-only). It paid off once here on a
  capacity-bound MoE (Nex-N2-mini +13% over GGUF-on-one-card) — treat as per-model experiment.
- ⚠ vLLM #40725 (non-English corruption on PCIe consumer GPUs) — still open
  (github.com/vllm-project/vllm/issues/40725); worst at TP=4 but reproduced at TP=2 upstream, yet
  TP=2 runs clean here daily. Validate non-English output after every vLLM upgrade.
- PP=2: official no-NVLink *throughput* guidance, but zero batch-1 decode gain and incompatible
  with spec decode — corruption-workaround only.
- SGLang: `tp-size 2` + `--enable-p2p-check` (0.5.9 default assumes P2P allowed without checking) —
  keep P2P on; `disable-custom-all-reduce` is A/B (prove custom-AR engaged from the log, don't assume it
  from removing the flag). Add `NCCL_P2P_DISABLE=1` only as the diagnostic fallback if it hangs at
  "Init torch distributed".

## lucebox dflash — draft-split (see dflash.md)

**Draft-split is the right 2-GPU mode:** target on GPU1 + drafter on GPU0
(`"target-device":"cuda:1","draft-device":"cuda:0"`) = **74 tok/s @256k** (single-GPU speed,
frees ~2 GiB for full KV). Target layer-split measured **−26…−46%** (F32 activations over PCIe) —
**pre-P2P**; `--peer-access` was a no-op then. On the patched driver `--peer-access` now has a working
substrate: treat it (and target layer-split) as an A/B lever to rebenchmark — prove peer access engaged
in the log — but **draft-split stays the baseline** until layer-split+peer-access is shown to win the
declared metric.

## unsloth

Installed **unsloth 2026.6.7** (+ `unsloth_zoo 2026.6.5`) at `~/.unsloth/studio` (source:
`~/.unsloth/studio/unsloth_studio/lib/python3.13/site-packages/unsloth-2026.6.7.dist-info/METADATA`).

Single-GPU only; pin via `launch.env` (`CUDA_DEVICE_ORDER=PCI_BUS_ID` + `CUDA_VISIBLE_DEVICES=1`).
Auth token is captured from the log automatically. Overflow → pick a smaller quant or move the
GGUF to llama.cpp for real pooling.

## Pin-per-GPU (two models at once) — the recommended agent default

Best structural use of the 2nd card for coding agents: **primary coder on GPU1 at full
single-GPU speed + an always-resident utility model (embedding/vision/judge) on GPU0.** Zero
NCCL, zero cross-talk, no swap latency.

```
launch.env (array of {"key","value"} objects — "KEY=VALUE" strings fail profile create):
  CUDA_DEVICE_ORDER=PCI_BUS_ID
  CUDA_VISIBLE_DEVICES=1        # primary → clean GPU1; secondary profile: =0 (budget the desktop)
```
- The mask REMAPS the visible card to `cuda:0` inside the process — do NOT also set `-mg 1` /
  `--device CUDA1` / `--base-gpu-id 1`; keep TP 1 / no split flags.
- **The proxy is single-active** (`instance start` swaps/evicts). Keep BOTH live: run a second
  headless proxy — `model-loader serve --port 4322` — for the secondary profile (serve doesn't
  take the single-instance lock; each profile still lands on its env-pinned GPU).

## P2P — status, engagement, verification

**Applied and validated (2026-07):** the `aikitoria/open-gpu-kernel-modules` P2P patch for the exact
installed driver **610.43.02** is live. CUDA peer access works both directions; measured peer copies
GPU0→GPU1 **13.34** / GPU1→GPU0 **13.15 GB/s**; two-GPU NCCL all-reduce verified. Boot policy
`iommu=pt` + `pci=disable_acs_redir=…` (ACS-redirect-disable) is load-bearing — **cost: loss of DMA
isolation**; P2P still hairpins through the root complex (PHB), so it is **not** NVLink bandwidth and
fixes **no** correctness bug (validate a long/non-English generation regardless).

**Engagement is never assumed — prove it every time:**
- vLLM/SGLang: read the launch log for the NCCL-P2P / custom-AR init line; the absence of
  `NCCL_P2P_DISABLE` / `disable-custom-all-reduce` is **not** evidence.
- llama family: `GGML_CUDA_P2P` (source-verified above: stable/nightly/beellama/buun only) or the
  NCCL-build peer path; ik uses its own default peer-access, lucebox uses `--peer-access`. Confirm in
  the log, then warm-measure an A/B — never infer P2P from a throughput delta.

**Stock-driver / diagnostic fallback** (revert only these comm flags, nothing else): re-add
`NCCL_P2P_DISABLE=1` and `disable-custom-all-reduce true`, unset `GGML_CUDA_P2P`. Use when diagnosing a
hang/corruption or on an unpatched driver.

## Interconnect upgrades (further, optional — ask the user first)

1. **NVLink bridge (~$40–80 used, spacing must match):** no driver hacking; helps vLLM/SGLang
   TP throughput (+48% high-concurrency; ~10–15% typical dual-3090), ~0% for llama.cpp layer
   split.

## Models worth the 2nd GPU (48 GiB tier, ≤46 GiB cap)

| Tier | Examples | Weights | tok/s (batch-1) | Verdict |
|---|---|--:|--:|---|
| 70B dense int4 | Llama-3.3-70B AWQ/GGUF Q4 (~42–45) | ~42–45 | ~15–21 | max quality; llama.cpp layer-split; KV must be q8/q4 for 32k |
| 32B @ Q8 / 27B @ Q6–Q8 | Qwen3-32B, Qwen3.6-27B UD-Q8 (~28–34) | ~28–34 | ~60–81 | the real 48 GiB quality play (Q8 ≈ 98% of bf16) |
| dense 27B EXL3 5–6bpw + TP+MTP | turboderp branches | ~22–27 | ~77–90 | quality + speed via exllama TP (unique draft coexistence) |
| 35B-A3B MoE | Q4 ~21 fits ONE card | ~21/~37 Q8 | 100+ | 2nd card buys Q8 or 256k, not speed; or Spark shrinks it to <16 GiB on one card (dflash.md) |
| NOT viable | Qwen3-235B (≥112), Mixtral-8x22B (~73), gpt-oss-120b (~60) | — | — | no profile; recommend smaller (no CPU-offload policy) |

"70B-on-2 vs 32B-on-1": 70B int4 ≈ 15–21 t/s, short ctx; a 27–32B Q8/EXL3-6bpw is usually the
better quality/latency call. Pick 70B only for a single big generalist.

## Power & thermal (updated 2026-07)

- Cards idle at defaults (GPU0 390 W AIB-OC / GPU1 365 W) — free headroom to claim:
  **`sudo nvidia-smi -i 0,1 -pl 275`** daily (≈98% of full speed), `-pl 220` for long
  unattended runs (~88% throughput at 73% power). Single-user decode (memory-bound) loses less
  than these compute-benchmark numbers.
- **GDDR6X memory-junction is the throttle** (~110 °C) and **`nvidia-smi` reports it N/A on
  3090s even on this driver** — monitor with `olealgoritme/gddr6` (root + `iomem=relaxed`), or
  assume VRAM ≈ core +15–25 °C. Target <90–95 °C; repadding drops 10–20 °C.
- Locked clocks (`nvidia-smi -lgc`) for benchmark consistency; optional for serving.

## VRAM budget

| Card | Raw | Desktop | Usable |
|---|--:|--:|--:|
| GPU1 (clean, ReBAR) | 24 | — | ~23.2 |
| GPU0 (desktop, small-BAR) | 24 | ~0.6–1 | ~22.4 |
| Aggregate (layer-split, capped) | 48 | ~1 | **≤46** |

- vLLM/SGLang `gpu-memory-utilization`/`mem-fraction-static` is per-GPU after sharding; with
  TP=2 each card frees ~half → KV ~doubles. Keep 0.84–0.90 with GPU0 in the party.
- Headroom reserve is backend-specific: ~5–8 GiB/card for vLLM/SGLang (CUDA graphs) but only
  ~0.5–1.5 GiB/card for llama.cpp layer-split.
- Pin-per-GPU → budget each card independently (23.2 / 22.4), not the pool.

## NCCL / P2P sanity check (run once before blaming a profile)

```
nvidia-smi topo -m          # expect PHB
nvidia-smi topo -p2p r      # patched rig: expect OK (P2P enabled); NS = stock-driver fallback
nvidia-smi --query-gpu=index,pcie.link.gen.max,pcie.link.width.max --format=csv
# asymmetric/degraded gen×width = signal-integrity trigger for layer-split garbage (#20052 root
# cause was a marginal riser cable, not a llama.cpp bug — replace a bad riser before blaming split)
nvidia-smi --query-gpu=index,pci.bus_id --format=csv   # confirm physical mapping
```

## Myths to avoid

- **"TP always helps" / "TP never helps without NVLink."** Both wrong — implementation- and
  model-specific: llama.cpp `-sm tensor` +22…39% (dense, short ctx), exllama TP +28% (dense) /
  −20% (A3B MoE), vLLM TP=2 ≈ capacity. Never generalize across engines.
- **"row and tensor split are the same."** row = deprecated −2.8×; tensor = the real TP mode.
- **"Layer-split slows decode."** Measured flat-to-+7% here.
- **"fp8 is free speed on 3090."** No FP8 compute on SM86; fp8 KV is storage-only; fp8
  *checkpoints* now run as Marlin W8A16 (weights-only).
- **"More GPUs = proportionally more tok/s."** Layer-split 70B ≈ flat across 2×/4×/6×; the
  tensor-mode dense gain is a one-time 2-card bandwidth effect.
- **"NCCL_P2P_DISABLE=1 fixes consumer-GPU corruption."** Not always (#40725) — validate output.
- **"3090s can't do P2P, period."** Outdated — this rig runs the aikitoria patch on driver 610.43.02
  with P2P validated (peer copies ~13 GB/s, NCCL all-reduce OK). It is a PHB/root-complex path (no
  NVLink bandwidth), engagement needs log proof, and the disable flags are the stock-driver fallback.

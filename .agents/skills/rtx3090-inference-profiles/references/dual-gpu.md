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
- **MoE under tensor: per-model, gate it** - measured 2026-07-12 (35B-A3B MTP Q4_K_M, 5 runs/variant,
  one-knob): all three profiles loaded clean under `-sm tensor`, decode +8...29% median at 25-90% fill
  (grows with fill), TTFT better, ~16 GiB/card. Equal-weight geometric mean across 5/25/50/90% fill:
  Ornith text **+12.17%**, Agents-A1 vision **+11.97%**, Ornith vision **+11.31%**. All three passed
  fresh MTP/correctness/backend-log guardrails and were PROMOTED to tensor. Overlapping five-run ranges
  remain a variance caveat, not a veto. (Supersedes both the old "flat +2%" and all-band-veto readings.)
  Default remains `layer` for unmeasured MoE; A/B per model.
- **exllama TP (native backend): dense +25…32%, A3B-MoE −20%** — and uniquely coexists with
  MTP/DFlash drafts (exllama-tabby.md).
- **vLLM/SGLang TP=2 = capacity** (per-card KV ~doubles); batch-1 decode gain comes from
  spec-decode + fp8-KV headroom, not the TP itself.

> These split rankings were measured **pre-P2P** (stock driver); re-measured on the patched driver P2P
> is a **TIE for single-stream llama/lucebox splits** (§P2P) — it lifts NCCL collective throughput
> (concurrent vLLM TP2), not a single-stream split. It changes none of the model-class rules
> (MoE/long-ctx/drafts) or the row/asymmetric bans, and never auto-splits a fitting single-GPU model.

## llama.cpp `-sm tensor` — status (upstream PR #19378, merged 2026-04; installed b9934/b10083 NCCL builds ON)

Hard caveats introduced/verified from b9847 onward and re-checked against the current builds:
1. **Speculative decoding under tensor splits into two cases — do not conflate them:**
   (a) **External drafts** (`-md` draft model, DFlash drafter) **crash/hang — no fix upstream**
   (#22473 silent stops, open; #24309 load-crash, open; #24440; PR #22400 did NOT fix it);
   beellama rejects it explicitly. Use `layer`, and keeping the draft wins anyway (Qwen3.6-27B
   DFlash-layer 79–83 tok/s ≫ tensor-no-draft 48–49, −36%). (b) **Embedded MTP/nextn heads**
   (baked into the GGUF, `spec-type draft-mtp`, no separate model) are **PER-MODEL — validate,
   never assume:** some load and run (Qwythos-9B +22% @256k; ornith-9B decode ~141, MTP
   env-proven engaged), some crash at load (Qwen3.6-27B-MTP, the #24309 `tensor_axis_0 != nullptr`
   nextn assert). A successful tensor load proves **compatibility only, not speed** - on 35B-A3B
   MoE the decode gain is per-model and high-variance. The measured 2026-07-12 trio won +11.31...12.17%
   by equal-weight geometric mean across all four fills and passed TTFT/VRAM/draft/correctness gates,
   so those three canonical profiles use tensor. Unmeasured MoE stays `layer` until the same complete
   one-knob aggregate A/B + external guardrails pass (full-tuning.md). Range overlap is a caveat,
   not an independent veto. Chainable draftless `ngram-mod` survives tensor.
2. **Forces CPU sampling** (`backend sampling not supported with SPLIT_MODE_TENSOR`) — still
   net faster.
3. **No `--fit`** → set `n-gpu-layers` and `ctx-size` manually.
4. KV quant: upstream docs claim f16-only, but **q4_0 KV measured WORKING in the local builds**
   (9B @256k/768k/1M, 31B @32k, coherent output) — re-validate after every rebuild; if a new
   build rejects quantized KV, that's the documented upstream behavior arriving.
5. `flash-attn on` required. Untested >31B here.
6. **Poolside Laguna fork:** `common_fit_params` explicitly rejects `SPLIT_MODE_TENSOR`; no
   automatic tensor sizing exists. The 63.6 GiB Laguna-S-2.1 Q4_K_M full tensor placement aborted,
   and tensor only loaded with `n-cpu-moe 48`, where decode measured 2.96 tok/s versus ~10.9 for
   fitted layer placement. Lower spill (`n-cpu-moe 20`) still did not fit. This proves tensor is not
   a viable 256K placement for that target/build; it does not overturn the ≤31B dense tensor wins.
   Never launch competing GPU benchmarks concurrently, and never compare tensor-with-CPU-spill to
   full layer placement as a pure split-mode A/B.

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
  `GGML_CUDA_P2P` mainly grants peer access to the **non-NCCL VMM copy path**. **Measured 2026-07-10
  (ornith-9B tensor-split, internal MTP): env-proven engaged, decode ~141 / prefill ~4120 tok/s with
  and without — a TIE within noise. Keep the DEFAULT (absent).** Batch-1 single-stream decode is
  weight-bandwidth-bound; the per-token cross-GPU exchange is tiny vs PCIe latency, so P2P lifts NCCL
  collective throughput (concurrent vLLM), not a single-stream llama split. It stays a split-only A/B
  lever (no effect on a single-GPU pin), carries the upstream **IOMMU/BIOS crash caveat** — prove
  engagement in the log and validate a long/non-English generation; unset if unstable. Never claim it
  for a backend without source/binary proof.
- ⚠ Long-ctx correctness: #20052 (layer-split garbage >2048 ctx on non-P2P rigs) was CLOSED as a
  marginal PCIe riser cable degrading signal integrity (Xid 79 bus drops), NOT a llama.cpp bug
  (github.com/ggml-org/llama.cpp/issues/20052) — the lesson stands: **validate a long /
  non-English generation, not just startup**, and suspect the PCIe link/cable first if it garbles.

## vLLM / SGLang TP2 (details in vllm-sglang.md)

- vLLM: `tensor-parallel-size 2, distributed-executor-backend mp`; util 0.84–0.90. **`disable-custom-all-reduce:
  true` is MANDATORY on SM86 (measured 2026-07-10, vLLM 0.24.0):** with the flag absent the log shows
  `Using ['CUSTOM','PYNCCL']` → CUSTOM selected → CRASH at startup (`custom_all_reduce.cuh:455 'invalid
  argument'`, EngineCore dies, backend never healthy, reproduced 2×). P2P being real does **not** fix this
  backend bug — keep custom-AR disabled. **Drop `NCCL_P2P_DISABLE` (P2P-on):** measured **+13.5% concurrent**
  (conc-32 2422–2444 vs 2112–2169 tok/s) with custom-AR already disabled — log `isAllDirectP2p 1`, `0->1 via
  P2P/CUMEM` (vs `isAllDirectP2p 0` / `via SHM`); TTFT/VRAM tie. Prove the NCCL-P2P path from the launch log,
  not a throughput delta. `NCCL_P2P_DISABLE=1` is the diagnostic fallback only (costs the +13.5%).
- **`enable-expert-parallel`: OFF for A3B-class sparse MoE at batch-1** (EP adds 7–12% overhead
  on ultra-sparse MoE; DeepEP fast paths are Hopper+NVLink-only). It paid off once here on a
  capacity-bound MoE (Nex-N2-mini +13% over GGUF-on-one-card) — treat as per-model experiment.
- ⚠ vLLM #40725 (non-English corruption on PCIe consumer GPUs) — still open
  (github.com/vllm-project/vllm/issues/40725); worst at TP=4 but reproduced at TP=2 upstream, yet
  TP=2 runs clean here daily. Validate non-English output after every vLLM upgrade.
- PP=2: official no-NVLink *throughput* guidance, but zero batch-1 decode gain and incompatible
  with spec decode — corruption-workaround only.
- SGLang: `tp-size 2` + `--enable-p2p-check` (0.5.9 default assumes P2P allowed without checking) — keep P2P
  on. **P2P engagement is now PROVEN (re-measured 2026-07-12 with `NCCL_DEBUG=INFO`, unlimited-ocr):
  dropping `NCCL_P2P_DISABLE` logs `isAllDirectP2p 1` / `Channel 0 → 1 via P2P/IPC` (vs `isAllDirectP2p 0`
  / `via SHM` with the flag) — the old "inconclusive" was just a missing NCCL_DEBUG. Serial per-page A/B a
  TIE (+3.5–4.9%, sub-5%; the 12-layer MoE emits little all-reduce), 0 errors. Keep P2P-on.** Keep `disable-custom-all-reduce`
  (SM86 custom-AR is unproven here and crashes on the vLLM sibling). `mem-fraction-static` must stay **0.78**
  while GPU0 hosts the desktop (0.84/0.88 OOM on the first OCR request). Add `NCCL_P2P_DISABLE=1` only as the
  diagnostic fallback if it hangs at "Init torch distributed".

## lucebox dflash — draft-split (see dflash.md)

**Draft-split is the right 2-GPU mode:** target on GPU1 + drafter on GPU0
(`"target-device":"cuda:1","draft-device":"cuda:0"`) = **74 tok/s @256k** (single-GPU speed,
frees ~2 GiB for full KV). Target layer-split measured **−26…−46%** (F32 activations over PCIe).
**`--peer-access` measured 2026-07-10 (Qwen3.6-27B NEO-CODE 64L layer-split): config-dump-proven on/off,
decode ~33 / prefill ~1072 tok/s both — a TIE. Keep the DEFAULT (off)** for a 2-way layer-split; the
per-token cross-GPU exchange is bandwidth-bound, not P2P-limited. **Draft-split stays the baseline.**
(A 9B/33L target was rejected by dflash — block_count not divisible by 4.)

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

## P2P — status, engagement, verification (measured 2026-07-10)

**Applied and validated:** the `aikitoria/open-gpu-kernel-modules` P2P patch for the exact installed
driver **610.43.02** is live. CUDA peer access works both directions; measured peer copies GPU0→GPU1
**13.34** / GPU1→GPU0 **13.15 GB/s**; two-GPU NCCL all-reduce verified. `nvidia-smi topo -p2p r/p` →
GPU0↔GPU1 OK. P2P still hairpins through the root complex (PHB) — **not** NVLink bandwidth — and fixes
**no** correctness bug (validate a long/non-English generation regardless).

**Where it actually pays off (and where it doesn't):**
- **vLLM TP2 concurrent = REAL WIN.** Dropping `NCCL_P2P_DISABLE` (custom-AR already disabled) measured
  **+13.5% concurrent** (conc-32 2422–2444 vs 2112–2169 tok/s; batch-1 176.75 vs 171.75) — log
  `Check P2P Type isAllDirectP2p 1`, `Channel 00/0: 0->1 via P2P/CUMEM` vs `isAllDirectP2p 0` / `via SHM`;
  TTFT/VRAM tie, both fluent PT, 0 Xid. Applied to `gemma-4-e4b-awq-vllm-tp2-32k`; the other 3 vLLM TP2
  profiles inherit the class (gain expected, magnitude unconfirmed).
- **custom all-reduce = BROKEN on SM86** (vLLM 0.24.0): with the flag absent, CUSTOM engages then CRASHES
  (`custom_all_reduce.cuh:455 'invalid argument'`) — `disable-custom-all-reduce: true` is MANDATORY,
  independent of P2P.
- **SGLang TP2 P2P = ENGAGED, serial-per-page TIE** (re-measured 2026-07-12 with `NCCL_DEBUG=INFO`):
  P2P-on logs `isAllDirectP2p 1` / `via P2P/IPC` (vs SHM `0`) — engagement proven, no longer inconclusive;
  per-page A/B a TIE (+3.5–4.9%, sub-5%), 0 errors. Keep P2P-on + `--enable-p2p-check` + `disable-custom-all-reduce`, `mem-fraction-static 0.78`.
- **Native single-stream splits = TIE, no gain.** llama `GGML_CUDA_P2P` (~141 dec / ~4120 pref) and
  lucebox `--peer-access` (~33 dec / ~1072 pref) measured equal on/off — keep DEFAULTS. Batch-1 decode is
  weight-bandwidth-bound; P2P helps NCCL collective throughput, not a single-stream split.

**Re-confirmed 2026-07-12 (this-session A/Bs via `scripts/benchmark-p2p-matrix.sh`, 5 runs/variant):**
- vLLM gemma-4-E4B TP2: serial llama-bench a **TIE** every fill; **concurrent scales +6.2%@1 → +13.9%@32**
  (2551 vs 2240 tok/s), 0 errors, transport re-proven `isAllDirectP2p 1` / `via P2P/CUMEM` vs SHM `0`.
- BeeLlama `GGML_CUDA_P2P` on/off (BugTrace-27B Q4_K_S tensor-split, 256k): **TIE** (per-preset medians
  swing but 5-run ranges overlap → ambiguous). Keep `GGML_CUDA_P2P` absent.
- **llama.cpp-stable `GGML_CUDA_P2P` on/off** (BugTrace-27B Q4_K_S tensor-split, 256k — direct on the
  `llama-server` binary, not just the BeeLlama fork): **TIE** (all fills overlap, ±1–10% both ways), 0
  errors. Confirms the source reasoning: these builds are NCCL-ON so the tensor-split comm path already
  has peer access; `GGML_CUDA_P2P` only touches the non-NCCL VMM copy path (no single-stream gain). Keep absent.
- ik-llama split-mode **graph vs layer** (RDson Qwen3.6-35B-A3B IQ4_KS, 256k, full offload): graph
  **+16–31% @25/50/90% fill** (5% a tie), coherence+tool-call OK at low fill → graph is a per-model speed
  lever, **layer stays the safe default** pending a high-fill coherence check (ik-llama.md).

**Engagement is never assumed — prove it every time:** read the launch log for the transport line above;
the absence of a disable flag is **not** evidence; never infer P2P from a throughput delta.

**Prerequisites / diagnostics (driver README):**
- Boot `iommu=pt pci=disable_acs_redir=0000:00:01.1;0000:00:01.3`. **`iommu=pt` REQUIRED** — a translating
  IOMMU makes BAR1 P2P transfers fail. Verify the persisted boot entry survives kernel/grub updates.
- **ACS-redirect disabled on the GPU root ports REQUIRED for bandwidth.** Proven live: `00:01.1` (GPU0) /
  `00:01.3` (GPU1) read `ACSCtl ReqRedir- CmpltRedir-`. ACS on → traffic hairpins through the CPU root
  complex → "P2P engaged but no gain". **`disable_acs_redir` is per-root-port** — changing GPU slots
  changes the IDs and requires re-pointing (other AMD bridges keep ACS on; irrelevant, no GPU traffic).
- **Security tradeoff:** BAR1 P2P does direct DMA writes and `iommu=pt` reduces DMA isolation — do not run
  untrusted code/devices on this box.
- **Hugepage caveat:** the patched driver auto-enables an experimental fast `cudaHostRegister` path for
  1G-hugepage-backed buffers that may misbehave where the stock driver is fine — unexplained host-memory
  corruption is not necessarily NCCL/custom-AR.

**Diagnostic fallback** (unpatched driver / init hang / suspected corruption): re-add `NCCL_P2P_DISABLE=1`
(costs the measured +13.5% concurrent), unset `GGML_CUDA_P2P`. **`disable-custom-all-reduce: true` is NOT a
fallback — it is the required SM86 default; never drop it to "test P2P".**

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
  NVLink bandwidth). Measured: dropping `NCCL_P2P_DISABLE` = +13.5% concurrent on vLLM TP2, but custom-AR
  still crashes on SM86 (keep `disable-custom-all-reduce`) and single-stream llama/lucebox splits tie.

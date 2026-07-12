---
name: rtx3090-inference-profiles
description: 'Use when creating, tuning, fixing, auditing, or benchmarking a model-loader profile/preset/launch config for local LLM inference on the dual RTX 3090 workstation (2×24 GiB, NO NVLink) — any backend (llama.cpp-stable/nightly, beellama, buun, vllm-stable/nightly, sglang-*, lucebox-dflash, unsloth, tabby). Triggers: run a model (GGUF/safetensors/AWQ/GPTQ/FP8/EXL3) at a target context; model too big for one card; split across GPUs (tensor-split, split-mode, tensor-parallel) or pin per GPU / two models at once; more context, tok/s, TTFT or quality; OOM/"CUDA out of memory"; KV cache type / kv-cache-dtype / quant choice; speculative decoding (MTP, DFlash, DSpark, EAGLE-3, ngram, suffix), Spark/KVFlash/PFlash; sampling params (temperature/top-p/top-k/min-p/repetition), model loops or corrupts tool calls. ALSO use to just "run this model quickly" (quick profile) vs "squeeze max performance" (full tuning); to AUDIT / "check my profiles" against the rig rules (naming drift, KV vs purpose, split symmetry, DRY); and to BENCHMARK / COMPARE profiles ("which profile is faster/better for code", measure tok/s / TTFT / solve-rate). Make sure to use this skill for any of these even when the user does not say "profile".'
---

# RTX 3090 ×2 Inference Profile Builder

Build, tune, fix, audit, and compare validated schemaVersion-3 profiles in
`~/.config/model-loader/profiles/<id>.json` that launch a model through the best backend for it,
optimized for **coding-agent workloads** on a **dual RTX 3090 rig (2 × 24 GiB = 48 GiB, NVLink
INACTIVE, PCIe PHB, driver 610.43.02, 16 cores, 60 GiB RAM)**.

This file is the **router**: pick the workflow that matches the request, then pull the backend/rig
facts each step needs from the reference files. The rig facts, placement decision, hard rules,
backend dispatch, quant table, context rule, and red flags below are **shared by every workflow**.

## Choose your workflow — read the one that matches, then execute it

| User intent | Workflow | Done when |
|---|---|---|
| "run this model" / spin up a new profile fast | workflows/quick-profile.md | validates, launches, one proxy round trip; description ends "calibration pending" |
| squeeze maximum performance from one model | workflows/full-tuning.md | measured tok/s + per-card peak GiB + agent-readiness recorded in the description |
| a profile misbehaves (OOM, loops, corrupt tool calls, spec won't load, split issues, proxy confusion) | workflows/troubleshoot.md | the symptom's specific verification passes |
| "which profile is best / how fast is it" | workflows/benchmark-compare.md | comparison table + one-paragraph recommendation delivered |
| "check my profiles against the rig rules" / drift | workflows/audit-profile.md | per-profile findings report (rule · severity · current · expected · fix) |

Ambiguous? Default: brand-new model with no numbers → quick-profile; "make it fast/best" on an
existing model → full-tuning; a complaint → troubleshoot. Quick-profile output is *promotable* —
its "calibration pending" description is the entry hook for full-tuning later.

## Reference files (knowledge — read on demand)

| File | When |
|---|---|
| references/model-research.md | **EVERY new model** — HF/web lookup of sampling, template, tool-call parser, spec assets; agent calibration + anti-loop policy; template verification checklist |
| references/speculative.md | choosing/configuring MTP · DFlash · DSpark · EAGLE-3 · ngram · suffix per engine; asset inventory per family |
| references/llama-family.md | llama.cpp-stable/nightly, beellama-rtx3090, buun-rtx3090 (GGUF); tuning by model type |
| references/vllm-sglang.md | vllm-stable/nightly, sglang-stable/nightly/unlimited/dflash; tuning by model type |
| references/dflash.md | lucebox-dflash server: DFlash+DDTree, **PFlash** (10× TTFT), **KVFlash** (flat 256k decode), **Spark** (MoE in <16 GiB) |
| references/exllama-tabby.md | tabby (EXL2/EXL3, TabbyAPI); cache-mode/bpw/TP by model type |
| references/sndr.md | **sndr-vllm** (SNDR/Genesis TurboQuant k8v4 + MTP overlay on vLLM); per-commit wheel + `GENESIS_ENFORCE_VERSION_RANGE=1` gotchas; the large-prefill trap |
| references/dual-gpu.md | any multi-GPU/pinning decision; power/thermal; **P2P status (validated) + engagement/verification**; unsloth |
| references/ik-llama.md | **ik-llama-cpp** (ikawrakow fork): MLA · fused-MoE · `-ser` expert reduction · `-rtr` ik-quant repack · RAM prompt cache (`cache-ram`); DeepSeek/GLM MoE GGUF; hand-curated flag subset, no tensor/row split |

Workflows live in `workflows/`; each links back to the shared sections here and to the reference
files above — it never re-derives knowledge that lives in a reference.

## The rig (read once)

- **GPU1 = clean card (~23.2 GiB usable)** — no display, ReBAR vBIOS. Default single-GPU target.
- **GPU0 drives the desktop** (~0.6–1 GiB → ~22.4 GiB usable), old small-BAR vBIOS.
- **No NVLink; PCIe PHB.** P2P is **enabled and validated** on the patched NVIDIA driver
  `610.43.02` (measured peer copies GPU0↔GPU1 ~13.3/13.2 GB/s; two-GPU NCCL all-reduce verified;
  boot `iommu=pt` + per-root-port ACS-redirect-disable, which costs DMA isolation). PHB caps it — no
  NVLink bandwidth, and P2P does NOT fix backend correctness bugs. **Measured 2026-07-10: it wins on
  vLLM TP2 concurrent only** (dropping `NCCL_P2P_DISABLE` = +13.5%); single-stream llama/lucebox splits
  tie. Engagement is real only when a launch log / probe proves it — never a perf delta. → dual-gpu.md §P2P.
- **VRAM cap 46 GiB total** (user policy): plan ≤23 GiB/card.
- **The 2nd card's value depends on the mode** (all measured here): `split-mode tensor` =
  +22…39% batch-1 decode on ≤31B DENSE at short ctx (shrinks with ctx, reverses ≥512k, prefill
  regresses, crashes with external drafts); exllama TP = +25…32% dense (and coexists with
  drafts); `layer` = +4…7% (safe, works with everything); vLLM/SGLang TP2 = capacity (256k +
  fp8-KV headroom); **pin-per-GPU (2 models) is the recommended agent default**; `row` = −2.8×,
  never. Details + decision table: dual-gpu.md.

## Placement decision — do this FIRST

1. **Weights + target-ctx KV fit ≤23 GiB?** → **SINGLE-GPU, pinned to GPU1**: `launch.env`
   `CUDA_DEVICE_ORDER=PCI_BUS_ID` + `CUDA_VISIBLE_DEVICES=1`. No split flags. (Want more speed
   on a fitting dense model with no external draft? tensor/exllama-TP — dual-gpu.md.)
2. **Two models live at once?** → **PIN ONE PER GPU** (heavy on GPU1) + a second
   `model-loader serve --port 4322` for the secondary. → dual-gpu.md §Pin-per-GPU.
3. **Exceeds 24 GiB but fits ~46 GiB?** → **MULTI-GPU as a CAPACITY decision**: llama.cpp
   `split-mode layer` / vLLM TP2 / exllama TP (dense) / autosplit (tabby MoE). → dual-gpu.md.
4. **MoE that must shrink below its all-GPU footprint?** → **Luce Spark on lucebox-dflash**
   (measured: 33B-A3B in 14.6 GiB @ ~100 tok/s vs 66 naive) — the ONE sanctioned expert-offload
   path. Naive `--n-cpu-moe`/CPU spill stays banned (explicit user request only). → dflash.md.
5. **Bigger than ~46 GiB?** → no profile; recommend a smaller model/quant.

## Hard rules

- **NEVER run backend binaries by hand** — only `model-loader instance start <id>`. Inference
  goes through the proxy (`127.0.0.1:4321`); OpenAI `model` param = profile ID.
- **NEVER set `"port"` in args** (reserved, stripped).
- **NEVER invent flags.** Read the live schema first:
  `~/.config/model-loader/backends/schemas/<backend-id>.json`. Unknown args = hard ERROR.
  Binary-real but schema-absent → `extraArgs` (raw passthrough: emits an unknown-flag warning,
  never enum/type-checked, never blocks). Schemas are curated-frozen — never hand-edit.
- **Run the model-research pass before writing any profile** (model-research.md): official
  sampling from HF (card → generation_config.json → unsloth), template diff, tool-call parser,
  speculative asset inventory. llama.cpp ignores generation_config.json — sampling MUST land in
  `args`; its built-in defaults are wrong for most models.
- **Schema enum ≠ hardware support.** SM86 has no FP8/FP4 compute. fp8 KV = storage-only
  (works — verified on the rebuilt vLLM 0.24.0 nightly); fp8 *checkpoints* now load as Marlin
  W8A16 (weights-only — legitimate, but int4 AWQ usually better); NVFP4/MXFP4/ModelOpt-FP8 = no.
  Check the Ampere matrix in vllm-sglang.md.
- **Anti-loop policy:** `repeat-penalty 1.05` + `repeat-last-n 256` in every llama-family
  profile; **DRY is banned** (user decision 2026-07-02) unless the user explicitly asks (then
  `allowed-length ≥4`). vLLM/SGLang/dflash: client-side penalties only. Claude Code overrides
  `temperature` but never `repeat_penalty`/`min_p` — launch flags are the reliable lever.
- **Tool-calling profiles: KV q8_0, never q4_0** (official llama.cpp caution + measured
  corruption here). Verify the chat template end-to-end (shell-hostile tool call) before
  trusting any agent profile — model-research.md §4.
- **Prefer speculative decoding whenever assets exist** (speculative.md): MTP on MoE/finetunes,
  DFlash on dense (beellama `dflash`), plain `draft-mtp` on llama.cpp. **The
  `draft-mtp,ngram-mod` chain is ideal for agents and validates directly in `args`** — llama.cpp
  `spec-type` is a **list-valued enum** (comma-chained values and `draft-dflash` both pass the
  validator; beellama/buun spell it `dflash`/`mtp` and are now list-valued too). Confirm the MTP
  head and the ngram drafter actually load in the launch log — config fields lie.
- **P2P is validated but its win is narrow — measured, not assumed** (dual-gpu.md §P2P). On this patched
  rig (driver 610.43.02) vLLM TP2 runs **P2P-on** — drop `NCCL_P2P_DISABLE` (measured **+13.5% concurrent**)
  — but **`disable-custom-all-reduce: true` is MANDATORY on SM86**: custom AR engages then CRASHES
  (`custom_all_reduce.cuh:455`); P2P does not fix that backend bug, so **never drop the disable flag**.
  SGLang TP2 P2P **engages** (2026-07-12 log-proven `isAllDirectP2p 1` / `via P2P/IPC`) but serial per-page ties — keep P2P-on, the disable flag, `--enable-p2p-check`.
  `GGML_CUDA_P2P=1` is **parsed only by llama.cpp-stable/nightly/beellama/buun** (getenv in ggml-cuda.cu —
  proven; NOT ik = its own default peer-access, NOT lucebox = `--peer-access`) and measured a **TIE** for
  single-stream splits (keep defaults) — a log-proven A/B lever carrying the IOMMU/BIOS crash caveat, never
  claimed without source/binary proof. Multi-GPU output can still corrupt silently (#20052, #40725) —
  **validate a long / non-English generation** before trusting any 2-GPU profile.
- Existing profiles are calibrated anchors — mirror their arg spelling. **On a 2-GPU box
  llama.cpp auto-splits when both cards are visible** — pin single-GPU profiles to GPU1.
- **NEVER asymmetric multi-GPU split on this rig.** When both RTX 3090s share weights/KV via llama.cpp `split-mode` **layer** or **tensor** (or any engine flag that partitions layers/tensors across cards), **`tensor-split` MUST be equal** — default **`"0.5,0.5"`** only. **Banned:** `0.45,0.55`, `0.4,0.6`, or any uneven ratio. Measured 2026-07-03 (Qwythos-9B MTP Q4_K_M @262k): tensor `0.5,0.5` ≈ **96** tok/s vs `0.45,0.55` ≈ **83** tok/s. `main-gpu` (e.g. `1`) is fine — it is not a substitute for asymmetric `tensor-split`. Desktop GPU0 headroom → **pin single-GPU** (`CUDA_VISIBLE_DEVICES=1`) or **pin-per-GPU two models**, not skewed splits.

## Backend dispatch (17 catalog entries; no default set)

| Kind | Catalog id(s) | Model format | Reference |
|---|---|---|---|
| llama-server | **llama.cpp-stable** (b9847), **llama.cpp-nightly** (b9869; identical profile-relevant flag surface to stable — fit, cache-ram, list-valued spec chain draft-mtp/eagle3/dflash/ngram) | GGUF path | llama-family.md |
| beellama-cpp | **beellama-rtx3090** (main@85e22ea0, b10102; featured DFlash, TurboQuant KV, reasoning-loop-guard; list-valued spec) | GGUF (+DFlash drafter) | llama-family.md |
| buun-llama-cpp | **buun-rtx3090** (b9792; superseded — recommend beellama; list-valued spec) | GGUF | llama-family.md |
| ik-llama-cpp | **ik-llama-cpp** (ikawrakow fork `t0002-889-g3bb0e9f0`; MLA, fused-MoE/up-gate, `-ser` expert reduction, `-rtr` repack, RAM prompt cache; split none/graph/layer only — no tensor/row; hand-curated flag subset) | GGUF path | ik-llama.md |
| vllm | **vllm-stable**, **vllm-nightly** (both 0.24.0 @ ee0da84ab; mtp/dflash/eagle3/ngram/suffix spec, fp8 KV verified on the rebuilt nightly, sleep mode); **sndr-vllm** (SNDR/Genesis TurboQuant k8v4 + MTP K=5 overlay; installed venv = dev424, dev714 is the upstream pin — short-prompt only, sndr.md); **vllm-dflash**, **vllm-dspark** (2026-07-09 spec-decode vLLM builds — DFlash / DeepSpec DSpark drafters; speculative.md) | safetensors/AWQ/GPTQ/FP8, repo id or dir | vllm-sglang.md · **sndr.md** |
| sglang | **sglang-stable** (0.5.9), **sglang-nightly** (0.5.6-dev, +DFLASH), **sglang-dflash** (qwen3_5 DFLASH/NEXTN), **sglang-unlimited** (Unlimited-OCR only) | safetensors | vllm-sglang.md |
| dflash | **lucebox-dflash** (1b11c50; DFlash+DDTree+PFlash+KVFlash+Spark; Qwen3.5/3.6-27B/Laguna/Gemma4 only) | GGUF positional | dflash.md |
| unsloth | **unsloth-rtx3090** (unsloth 2026.6.7) | HF repo / GGUF | dual-gpu.md §unsloth |
| tabby | **tabby** (3cf468c; EXL2 deprecated / EXL3, TabbyAPI; exllamav2 0.3.2, exllamav3 0.0.43) | EXL2/EXL3 model **dir** | exllama-tabby.md |

Backend unspecified? GGUF+MTP head → llama.cpp-stable (`spec-type: draft-mtp` alone, or the
`draft-mtp,ngram-mod` chain directly in `args` — the list-valued enum accepts it); dense GGUF with
DFlash drafter → beellama-rtx3090; other GGUF → llama.cpp-stable + ngram-mod; safetensors/AWQ →
vllm-stable; EXL3 (`quant_method: exl3`) → tabby; Qwen3.6-27B max-tok/s or PFlash/KVFlash/Spark
needs → lucebox-dflash; qwen3_5/gemma4 AWQ/FP8/int4 wanting SNDR TurboQuant k8v4 + MTP K=5 for
**short-prompt high-concurrency** decode (NOT big-prompt agents) → sndr-vllm (sndr.md).
DeepSeek-MLA / GLM-DSA architecture GGUF, or GGUF wanting ik's MoE levers (`-mla`, `-ser` expert
reduction, `-rtr` ik-quant repack, `cache-ram` prompt reuse) → ik-llama-cpp (ik-llama.md — shares
llama-server's CLI but a hand-authored flag subset; split layer/graph only, no tensor/row).
Honor an explicit backend after the format check. Multi-GPU or
pin-per-GPU placement → read dual-gpu.md, THEN exactly one backend file.
**Tiebreaker** (a Qwen3.6-27B GGUF matches both the MTP row and the lucebox row): an **agent /
tool-calling** profile with an MTP head defaults to **llama.cpp-stable** (the `draft-mtp,ngram-mod`
chain in `args`); route to **lucebox-dflash** only on an explicit max-tok/s, PFlash, KVFlash, or
Spark need. beellama-rtx3090 wins when a dedicated DFlash *drafter* GGUF is the point.

## Quant choice (coding quality first — full table in research; details per backend file)

| Size | 24 GiB (one card) | 48 GiB (both) |
|---|---|---|
| 7–9B | GGUF Q8_0 | same; spend card 2 on a second model |
| 12–14B | Q6_K / UD-Q5_K_XL; AWQ-int4 if concurrency | FP8-checkpoint (Marlin W8A16, near-lossless) |
| 24–32B dense | UD-Q4_K_XL / imatrix Q4_K_M; EXL3 3.3bpw; AWQ awq_marlin | Q5_K_M–Q6_K (+tensor if no draft); EXL3 4–5bpw+TP; FP8 TP2 |
| 35B-A3B MoE | UD-Q4_K_XL (fits 256k) — int4-MoE on vLLM is crash-prone, AWQ only | Q5/Q6 layer-split; or Spark <16 GiB |
| 70B dense | doesn't fit | int4 layer-split ~15–21 tok/s; usually prefer 27–32B Q8 |

Prefer imatrix/UD quants always (static Q4 costs ~5 pp on coding). IQ4_XS only to buy back
~0.5 GB for ctx. Never 3-bit for reasoning/coding without explicit request.

## Context adjustment rule

Target ctx is a band, not a contract: move within **−25% / +50%**, only across a real cliff,
always say so. **RAISE** when headroom is paid for (>1.5 GiB free on the binding card) — cap at
native ctx (`n_ctx_train`/`max_position_embeddings`) unless the user wants RoPE/YaRN. **LOWER**
(named ≥10% gain) when it buys: a weight-quant tier, a KV tier (never below q8_0 for
tool-calling), full GPU offload, avoiding enforce-eager, or a bigger quant in the pool. Never
drop below 75% of the request silently. lucebox `max-ctx` leans DOWN (oversizing slows prefill)
unless KVFlash. Budget is per-card single-GPU, ~46 GiB pooled when split. Also drives the audit's
ctx-label check (label = binary-k floored from `ctx-size`/`max-model-len`).

## Common mistakes

| Mistake | Reality | Fix |
|---|---|---|
| Skipping the model-research pass | llama.cpp defaults (temp 0.8/top-k 40/min-p 0.05) ≠ any model's official preset; GGUF templates drift from HF | model-research.md; sampling in `args`, template via `--chat-template-file` when drifted |
| temp 0 "for reliable tool calls" | official warnings (Qwen/DeepSeek) + measured loops; format comes from grammars/parsers | family temp (coding preset if any); repeat-penalty 1.05; grammar does the format |
| Adding `dry-*` to a profile | user removed DRY from all profiles 2026-07-02; aggressive DRY corrodes tool calls | repeat-penalty + temp; DRY only on explicit request, allowed-length ≥4 |
| Routing chained spec / sglang parsers through extraArgs "because the enum blocks them" | STALE (BUGS.md S1/S2 fixed): `spec-type` is list-valued (`draft-mtp,ngram-mod`, `draft-dflash` validate in `args`); sglang `reasoning-parser`/`tool-call-parser` enums widened to the installed 0.5.9 detector maps (`qwen3_coder`/`glm47`/`qwen3-thinking` validate in `args`) | put them in `args`; extraArgs is only for genuinely schema-absent flags |
| vLLM `--reasoning-parser qwen3` | measured silently dropping 1224/1692 tokens | no parser until the token-accounting test passes; tags stay in `content` |
| vLLM ngram spec with default lookup-min | corrupts ~50% of Qwen tool calls (#40875) | `prompt_lookup_min: 8` on tool-calling profiles |
| `spec-type: mtp`/`dflash` on llama.cpp-stable (or `draft-mtp` on beellama) | dialects differ: upstream = `draft-mtp`/`draft-dflash`; beellama/buun = `mtp`/`dflash`; buun keeps extra DFlash-slot flags | trust each backend's own schema; never mix |
| "tensor-split makes everything faster" | dense short-ctx only (+22…39%); MoE flat; reverses ≥512k; prefill regresses; crashes with external drafts; keeping the draft beats tensor (79>48 tok/s) | layer for drafts/MoE/pooling; tensor only fitting-dense-no-draft; measure per ctx |
| Single-GPU profile silently layer-splits | llama.cpp auto-splits with both cards visible | pin: `CUDA_DEVICE_ORDER=PCI_BUS_ID` + `CUDA_VISIBLE_DEVICES=1` |
| Mask + device flag together | `CUDA_VISIBLE_DEVICES` remaps the card to `cuda:0` in-process | mask via env; leave `-mg`/`--device`/`--base-gpu-id` at default |
| "fp8 in the enum, so it works" | SM86: no fp8 compute; fp8 KV = storage-only; fp8 weights = Marlin W8A16; ModelOpt-FP8 refuses. **fp8 KV on `vllm-nightly` is verified working at request time on the rebuilt 0.24.0 (measured 2026-07-07)** — the prior editable build's `fp8 tensor core is not supported in fa2 backend` prefill assert is fixed by the rebuild | int4 AWQ/GPTQ marlin first for weights; fp8 KV is a separate storage-only decision (auto-switches attention to FLASHINFER) — verify with one large-prefill request, not startup |
| vision profile + cache-reuse | mmproj silently disables cache-reuse/context-shift (and non-DFlash spec on beellama) | expected; plain prefix reuse remains |
| Asymmetric KV types (q8_0/q4_0) on llama.cpp | GPU offload fails, ~40× slower (#20866) | symmetric q8_0/q8_0 |
| vllm/sglang without `served-model-name` | proxy forwards model=<profile-id>; vLLM 404s | set it = profile id |
| Trusting startup / idle VRAM / first request | corruption bugs, prefill pool growth, cold torch.compile | long+non-English gen; near-full-ctx prompt; warm-measure |
| SNDR `-sndr` profile validated with a short prompt | TurboQuant continuation-prefill (P38) OOMs (util 0.84) / crawls 1.4 tok/s (0.78) on any large prompt at 262144 — short "capital of X" tests mask it | `benchmark --mode llama-bench` (large prefills) before trusting; ≤131072 variant or stock fp8 for coding-agent prompts (sndr.md) |
| SNDR profile without `GENESIS_ENFORCE_VERSION_RANGE=1` | version-capped patches (e.g. PN30, obsolete on dev424) hard-fail (`failed≥1`) instead of skipping | add it to `launch.env`; a clean boot = `register() complete: … failed=0` (sndr.md, BUGS.md N2) |
| SNDR install via `wheels.vllm.ai/nightly` | rotating index drops the pinned dev build → `No matching distribution` | `SNDR_WHEEL_INDEX=https://wheels.vllm.ai/<full-sha>/` per-commit URL (BUGS.md N1) |
| SNDR multimodal model (gemma-4 / heavy FP8) without `--language-model-only` | gemma boots die on `max_tokens_per_mm_item > max-num-batched-tokens`; 35B-FP8 caps at ~69k ctx | `--language-model-only` (text serving) — frees vision VRAM, lets 35B reach 262144; keep vision on the fitting 27B for a fair A/B |
| "DSpark is installed somewhere" | real (DeepSeek, 2026-06) but in NOTHING installed; llama.cpp PR #25173 still open (not in b9847 nor b9869); no 27B drafters | speculative.md §DSpark for the adoption path |
| `"port"` in args / HF repo id for llama family / hand-running binaries | reserved; os.Stat fails; policy | omit; download first; `instance start` only |
| Asymmetric `tensor-split` (e.g. `0.45,0.55`) to "spare" GPU0 | operator policy + measured slower tensor decode on 9B @262k | **only** `"0.5,0.5"` with both cards visible; see dual-gpu.md |
| Two models via two `instance start` | proxy is single-active (swap evicts) | pin per GPU + 2nd `serve --port` |
| ik-specific flags in `args` that aren't curated (`--peg`, `-grt`/`--graph-reduce-type`, `-gap`, `-khad`/`-vhad`, `--merge-qkv`, `--cache-ram-n-min`), or `split-mode tensor`/`row` | ik's curated schema is a **hand-authored subset** (ik `--help` is never parsed), and its `split-mode` enum is only `none/graph/layer` | schema-absent ik flags → `extraArgs` (raw passthrough); split via `layer`/`graph` only; full flag list in ik-llama.md |
| Assuming ik-llama-cpp = llama.cpp-stable | ik defaults FA on / MLA 3 / fused-MoE+up-gate+mul-multiadd on / graph-reuse on / `cache-ram` 8192 MiB; agent prompt caching is `cache-ram` (+`-sps`), NOT `cache-reuse`; `spec-type` is a free-form string in the ik dialect (bare `mtp`/`dflash`/`ngram-mod`, not upstream `draft-mtp`) | trust ik-llama.md + the on-disk schema, not llama-family habits |

## Red flags — stop and re-read the reference file

- "I know this family's sampling by heart" / "the GGUF template is probably fine"
- "temp 0 is safest for agents" / "DRY will fix the loop" (banned by user policy)
- "It's in the schema enum, so the GPU supports it"
- "tensor-parallel can't help without NVLink" (false for llama.cpp tensor + exllama TP on dense)
  / "tensor split helps everything" (false for MoE, long ctx, drafts)
- "row-split is the fast path" (row = −2.8×, banned) / "just set `GGML_CUDA_P2P=1` and the split is faster" (measured a TIE for single-stream llama/lucebox splits — keep defaults; the only measured P2P win is dropping `NCCL_P2P_DISABLE` on vLLM TP2 concurrent, +13.5%; dual-gpu.md §P2P)
- "P2P can't work on 3090s, keep `NCCL_P2P_DISABLE=1`" (STALE — validated on driver 610.43.02; dropping it is a measured +13.5% on vLLM TP2 concurrent) / "P2P works now, so I can drop `disable-custom-all-reduce`" (NO — custom AR CRASHES on SM86, `custom_all_reduce.cuh:455`; keep it disabled; dual-gpu.md §P2P)
- "the chained spec-type / sglang parser is blocked, route it through extraArgs" (S1/S2 fixed — it validates in `args`)
- "Startup succeeded, ship it" (no long/non-English/near-full-ctx/warm re-test)
- "The first request's tok/s is the number" (cold-start ~half speed)
- "Estimates say it fits, no need for per-card nvidia-smi"
- "I'll skip validate / skip the tool-call round trip"
- "buun/tabby aren't in the catalog" (both are registered — check catalog.json, don't assume)
- "ik-llama-cpp is just llama.cpp" (ik has split `none/graph/layer` only, MLA/fused-MoE/graph-reuse-on defaults, `cache-ram` not `cache-reuse`, a free-string `spec-type` dialect, and a hand-curated flag subset — ik-llama.md)
- "the SNDR profile answered 'Paris', ship it" (short prompts hide the TurboQuant large-prefill OOM — llama-bench it; sndr.md) / "SNDR is just faster vLLM" (TurboQuant trades large-prefill headroom for KV concurrency — short-prompt only at 262144)

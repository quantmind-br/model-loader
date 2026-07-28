# llama family on the dual-3090 rig — mainline, Poolside Laguna, BeeLlama, buun, PrismaML

Catalog ids: **`llama.cpp-stable`** (b9934, `32e41fa5b`), **`llama.cpp-nightly`** (b10083,
`846e991ec`), and **`llama.cpp-prisma-ml`** (PrismML fork b9597, `7529fdaaf`, base b9594 — see
§llama.cpp-prisma-ml). Their profile flag names overlap, but runtime capability is no longer identical:
nightly adds correct separate-HF-draft-repo sidecar resolution plus DFlash/EAGLE-3 discovery;
**`beellama-rtx3090`** (beellama fork, `v0.4.1-1-g94605e9fe`, build **b10856**; past the v0.3.1
tag — v0.4.0 swapped the fork DFlash for upstream `draft-dflash` and added KVarN KV compression +
the KV precision tail; v0.4.1 is KVarN/HIP/Vulkan + compact-SWA-tail work with **no flag-surface
change** vs b10829); **`buun-rtx3090`** (buun fork,
b9792 `87c351d28`, superseded — recommend beellama unless the user insists). All take local GGUF
paths (`--model` emitted from the profile `model` field; HF repo ids REJECTED by the validator
for these kinds) and share canonical flag mapping (`ngl` → `n-gpu-layers`). On-disk schemas are
curated and `source.editable` (249 flags llama.cpp-stable/nightly, 265 beellama, 251 buun) — a
flag the binary supports but the schema lacks goes verbatim in `extraArgs`, a **raw passthrough**
whose values are never enum/type-checked (`internal/service/validator/rules.go`
`applyExtraArgsRules`); it is for schema-absent flags only, NOT a workaround for "enum-blocked"
values — nothing valid is enum-blocked. The validator also resolves flag aliases (`Lookup` in
`internal/domain/flag_schema.go`), so a `--spec-draft-*` spelling used as an `args` key resolves
to its canonical schema flag and validates.

## Which backend within the family

| Situation | Backend | Why |
|---|---|---|
| GGUF with native MTP head (`*-MTP`, nextn) | llama.cpp-stable, `spec-type: draft-mtp` (or the `draft-mtp,ngram-mod` chain) | MTP ~180–208 tok/s @70%+ accept on MoE; the `draft-mtp,ngram-mod` chain adds free hits on re-emitted code and **validates directly in `args`** — `spec-type` is a list-valued enum (schema `"list": true`, `checkEnum` splits on comma; S1 fixed); confirm both the MTP head and the ngram drafter load from the launch log |
| Dense GGUF + DFlash drafter | beellama-rtx3090 (`spec-type: draft-dflash`, default-on adaptive `profit` controller, upstream-format drafter) — same `draft-dflash` on llama.cpp-stable also validates in `args` (list-valued `spec-type` enum; S1 fixed), flat only | DFlash beats draft-mtp on dense (measured); beellama adds its adaptive depth controller on top of the same upstream DFlash, llama.cpp-stable runs it flat |
| Plain GGUF, no spec assets | llama.cpp-stable + `spec-type: ngram-mod` | free speedup on agent/code loops (~16 MB, zero VRAM) |
| Finetunes (abliterated/heretic/NEO) | prefer native MTP | DFlash acceptance collapses on finetunes (57%→~24%) unless a matched drafter exists (e.g. AEON-7); MTP heads survive finetuning |
| EAGLE-3 head exists, no MTP/DFlash | llama.cpp-stable `spec-type: draft-eagle3` + converted head | Qwen3.5/3.6-hybrid fixes are in b9847; agentic-trained heads (Ex0bit) best |

### HF speculative sidecars: installed-build matrix

- The profile's main `model` remains a local GGUF path; model-loader validation does not accept a
  bare HF repo as the llama-family model.
- **stable b9934:** supports all listed speculative modes and local external drafter GGUFs. Its
  downloader can discover `mtp-*` beside a model selected through llama.cpp's own `--hf-repo`
  path, but a separate `spec-draft-hf` plan does not consume the discovered sidecar reliably; use
  a downloaded local GGUF in `spec-draft-model` for separate MTP/DFlash/EAGLE-3 repos.
- **nightly b10083:** `spec-draft-hf` resolves a separate draft repo when `spec-type` contains the
  matching type (`draft-mtp`→`mtp-*`, `draft-dflash`→`dflash-*`, `draft-eagle3`→`eagle3-*`),
  avoids duplicate speculative downloads, and excludes sidecars from primary-model selection.
  Use exactly one model-based draft type with one HF draft repo; multiple sidecars are downloaded
  in parallel and no winner is guaranteed. Chaining with draftless `ngram-mod` remains supported.
  Repo naming must follow the matching prefix.
- The shared schema proves argument spelling only. Check the executable version and requested
  type before choosing the HF-repo path. After launch, prove drafting through a direct OpenAI
  response's `timings.draft_n`/`draft_n_accepted`, or backend logs/benchmark; Claude Code's
  Anthropic route does not preserve `timings`.

### Laguna-S-2.1 support matrix (measured 2026-07-24)

- **Poolside llama.cpp `laguna` branch (`04b2b72`)** is the current working host for the official
  Laguna-S-2.1 GGUF and its matched `laguna-s-2.1-DFlash-BF16.gguf`. Its CUDA 13.3/sm_86 build
  needed `<cmath>` in `common/speculative.cpp` for `std::isfinite`; treat that as a source-build
  compatibility fix, not a profile flag.
- Installed `llama.cpp-stable` b9934 and BeeLlama b10829 reject `general.architecture=laguna`.
  Backend kind and shared flags do not prove model-architecture support. Re-test after upgrades.
  ⚠ **Pending re-pin (noted 2026-07-27):** this holds at the documented pins — `laguna` is absent
  from `src/llama-arch.cpp` at both **b9934** (stable, `32e41fa5b`) and **b10083** (nightly,
  `846e991ec`). PR **#25165** ("Add support for Laguna XS.2 & M.1") is an ancestor of neither
  pin. It landed in **nightly** after b10083 (nightly HEAD 0324696b8 ships `LLM_ARCH_LAGUNA` +
  `src/models/laguna.cpp`); the stable checkout has also drifted forward to b10148 and picked it
  up, but that is a drift artifact, not a tested pin. Mainline Laguna routing is therefore a
  nightly-upgrade concern. **Not load-verified here** (a CPU-only arch probe on the 68 GB Q4_K_M
  did not finish), and PR #25165 names XS.2/M.1, not S-2.1 — re-test before moving any Laguna
  profile off the poolside fork. BeeLlama b10856 still has no `laguna` arch.
- Installed Lucebox recognizes Laguna but rejects Laguna-S-2.1 because its compiled `n_head_arr`
  capacity is 40 layers; this target has 48. A future Lucebox rebuild may change that boundary.
- Official Q4_K_M is 63.6 GiB: larger than aggregate VRAM. Poolside `fit on` + `fit-ctx 262144`
  + `fit-target 1536` selected a working dual-GPU `layer` placement with mmap-backed CPU tensors;
  measured idle VRAM was about 22.4/22.3 GiB and decode about 10.9 tok/s on the calibration probe.
  This is an explicit Laguna exception to the usual no-CPU-weights policy because no ≥Q4 target
  fits 46 GiB; document the tradeoff and do not generalize it to other MoE models.
- Matched DFlash needs **f16 draft KV** here. `q4_0/q4_0` draft KV produced ~1.2% acceptance;
  f16/f16 recovered 82.2% on repetitive code. That did not generalize: tool-call, short-answer,
  and 64K needle probes stayed around 2.7–4.3%. Keep DFlash as a workload-specific variant and
  select the non-DFlash layer profile for general coding agents unless a representative corpus wins.


Speculative details + asset inventory: **references/speculative.md**. Sampling/template/tool-call
setup: **references/model-research.md** (mandatory research pass).

## Features introduced by b9847 and still relevant on b9934/b10083

- **`--fit` is default ON** (`fit-target` 1024 MiB/device, `fit-ctx` 4096): with `n-gpu-layers`
  and `ctx-size` unset it auto-sizes ctx and layer/expert placement. **Rig policy: pin
  `"n-gpu-layers": 99` and set `ctx-size` explicitly anyway** — pinning disables fit's weight
  step, which is what we want (fit would silently spill MoE experts to CPU, violating the
  no-CPU-weights policy; and unset ctx gets auto-shrunk). Fit does NOT work with
  `split-mode tensor` (size manually there).
- **Context shift is DISABLED by default** (hitting `n_ctx` → stop with `truncated:true`) —
  correct for agents; size ctx properly instead.
- **Agent prompt caching:** slot prefix reuse is always on; add `"cache-reuse": 256` (chunked
  KV-shift reuse after mid-prompt edits — the Claude Code compaction pattern) and keep
  `cache-ram` (default 8192 MiB host cache; raise via `"cache-ram": 16384` in `args` on this
  box). `cache-ram`, `cache-reuse`, `top-n-sigma` and `swa-checkpoints` are all **present in the
  curated llama.cpp-stable AND beellama schemas** (`~/.config/model-loader/backends/schemas/
  llama.cpp-stable.json`) — they validate in `args`, no extraArgs needed. **Silent disablers:**
  `mmproj` kills cache-reuse + context-shift (vision profiles keep only plain prefix reuse);
  pure-SWA models (Gemma) need `swa-full` (VRAM-costly); hybrid-recurrent (Qwen3.5/3.6 A3B) —
  cache-reuse ineffective, rely on prefix cache + `"swa-checkpoints": 32` in `args` instead (the
  curated key — `--ctx-checkpoints` is the binary's other spelling for the same flag but is NOT
  in the schema → extraArgs; `backends/beellama.cpp/docs/beellama-args.md`).
- **Slots:** server default `-np -1` → 4 slots + unified KV. For strict batch-1 latency set
  `"parallel": 1`. For 2–4 parallel subagent calls on one profile keep auto + cache-ram; with
  `kv-unified` the slots share one pool (no 4× KV cost), but there is NO cross-slot prefix
  sharing (N subagents × 50k shared prefix = N× prefill) — vLLM/SGLang win that pattern.
- **Prefill/TTFT:** `-ub 2048 -b 2048` is the current official recommendation (~2–4× prefill
  vs ub 512) — compute buffer grows, re-check VRAM at long ctx; drop to 1024 if squeezed.
- **Flash attention** default `auto`; pin `"flash-attn": "on"` when using quantized KV or
  split-mode tensor. CUDA graphs + graph reuse are on by default.
- **Draft/spec flag surface:** `--draft`/`--draft-n`/`--draft-max` (→ `draft.n_max`) and
  `--draft-min`/`--draft-n-min` remain live aliases on b9934/b10083; the canonical names are
  `--spec-draft-n-max` / `--spec-draft-n-min`, prefer those. Dialects DO differ by backend
  (`common/speculative.cpp` type maps): `mtp` and `dflash` are the **buun-only** fork `spec-type`
  spellings — upstream and BeeLlama v0.4.0 use `draft-mtp` and `draft-dflash`. **BeeLlama v0.4.0
  switched to the upstream `draft-dflash` spelling** (its old fork `dflash` is gone); only **buun**
  (b9792, not upgraded) still uses the fork `dflash`/`mtp` dialect and has NO `draft-dflash`. Never
  mix spellings across the three backends; trust each backend's schema.
- `--defrag-thold` deprecated/no-op. `--no-host`: AMX-specific, skip on this rig.
- mmap default ON is what makes proxy hot-swaps fast (page cache). The library's anchors use
  `--no-mmap --mlock --no-host` (avoids page-cache eviction stalls mid-session; mlock may log a
  harmless RLIMIT warning). Either is defensible — don't churn existing profiles over it.

## VRAM budget

`weights (GGUF size) [+ drafter] + KV + compute buffers ≤ ~23.2 GiB (GPU1)`

- KV bytes ≈ `ctx × n_layer × n_kv_heads × head_dim × (bpv_K + bpv_V) / 8` — **dense
  full-attention only**. Hybrid/SWA (Gemma, qwen3.5/3.6) cost far less (measured: Qwen3.6-27B
  q4_0 ≈ 22.5 KiB/token vs 72 by formula). Scale from an anchor or read the KV line in the log.
- bpv ladder: f16 16 · q8_0 8.5 · q6_0 6.5 · q5_1 6.0 · q5_0 5.5 · q4_1 5.0 · q4_0/iq4_nl 4.5.
  v0.4.0 added standard `q6_1`/`q3_1`/`q3_0`/`q2_1`/`q2_0` (widths track their labels; verify the
  KV line in the log). beellama v0.4.0 KVarN (target-context compression, CUDA only): `kvarn8`
  down to `kvarn2` are progressively smaller with independent K/V widths — read the measured KV
  buffer from the launch log rather than a formula; `kv-tail-tokens auto` keeps the newest
  entries exact.
- Compute buffers scale with `ubatch × ctx`. **Long-prompt prefill grows the CUDA pool at
  runtime** — idle fit ≠ fit. Always test a near-full-ctx prompt + re-check nvidia-smi.

## Quant combos by bias (dense)

| Bias | Target quant | Drafter | KV K/V | Notes |
|---|---|---|---|---|
| Precision (coding/agents) | Q5_K_S / Q5_K_M (or Q6/Q8 if it fits) | Q4_K_M / IQ4_XS | **q8_0/q8_0** | tool-calling profiles NEVER q4_0 KV (official caution + measured corruption here); K more sensitive than V — q5_0/q4_1 is the compromise pair |
| Balanced / max context | Q4_K_M / UD-Q4_K_XL | Q4_K_M | q4_0/q4_0 | proven: 27B + drafter @204800 ≈ 22.4 GiB idle; 262144 fits at peak 23723 MiB — razor-thin |
| Extreme squeeze (**non-agent only** — never tool-calling) | IQ4_XS | IQ4_XS | kvarn3/kvarn2 (beellama v0.4.0) | real quality cost — explicit request only; KVarN replaces the removed TurboQuant types. Tool-calling profiles keep the q8_0 KV floor — cut ctx or ship a lower-ctx variant instead |

Short ctx (≤32k) with headroom → keep KV f16/q8_0. **Q8 drafters are never better than Q4_K_M**
(measured; drafting doesn't need precision). Unsloth UD (Dynamic 2.0) quants are the
quality-per-GB pick within GGUF when available.

## Speculative setup (see speculative.md for the full landscape)

- **MTP:** `"spec-type": "draft-mtp"` (embedded nextn head — no drafter file; confirm the head
  loads in the log), `"spec-draft-n-max": 3` (**6 OOMs at 200k**, +2.1 GiB). For agent traffic
  chain the free ngram drafter: `"spec-type": "draft-mtp,ngram-mod"` — this **validates directly
  in `args`** on llama.cpp-stable/nightly (`spec-type` is list-valued, `"list": true`;
  `checkEnum` splits the comma-list; S1 fixed), so ship the chain, do NOT route it through
  extraArgs; confirm both the MTP head and the ngram drafter load from the launch log (sparse at default verbosity — if no draft line shows, raise `--log-verbosity`/`lv`, query the OpenAI endpoint directly for `timings.draft_n`/`draft_n_accepted`, or use a benchmark; Claude Code's Anthropic route does not preserve `timings`). MTP draft
  ctx defaults f16 KV — set `"cache-type-k-draft": "q8_0", "cache-type-v-draft": "q8_0"` in
  `args` (schema keys; `--spec-draft-type-k`/`-v` are accepted aliases that resolve to the same
  flags via `Lookup`).
- **DFlash (beellama v0.4.x):** `"spec-type": "draft-dflash"` (**renamed from the old `dflash`** —
  v0.4.0 adopted upstream's implementation and spelling), `"spec-draft-model": "/abs/drafter.gguf"`,
  `"spec-draft-ngl": 99`, `"kv-unified": true` — leave `spec-draft-n-max` unset (default-on adaptive
  `profit` controller; omitted limit = `dflash.block_size - 1`; verified 2026-07-27 — the log prints
  `omitted --spec-draft-n-max defaults to the drafter block depth (15)` for a block-16 drafter). The
  old fork knobs `spec-dflash-cross-ctx`/`spec-dflash-max-slots`/`spec-branch-budget` are **removed**
  (rejected at launch). `--spec-draft-model` requires an **upstream-format `dflash` draft GGUF** —
  v0.4.x mandates upstream's `dflash` architecture, metadata keys, tensor names, and tokenizer
  contract. **Check the drafter repo before converting anything** (2026-07-27): publishers are
  re-uploading in upstream format, so a repo that shipped a legacy fork drafter months ago may
  already be fixed. `Anbeeld/Qwen3.6-27B-DFlash-GGUF` was re-published 2026-07-19 as upstream
  `dflash` and is installed + verified on this rig. Read the remote header without downloading the
  weights: `curl -H "Range: bytes=0-25000000"` the `resolve/main/<file>.gguf` URL and parse the GGUF
  KV block — confirm `general.architecture=dflash` (not `dflash-draft`) and upstream tensor names
  (`fc.weight`, `enc.output_norm.weight`, `blk.N.ffn_norm.weight`). Watch acceptance in the log;
  ~0.86 on templated code vs ~0.21–0.27 on prose (measured 0.245, mean accepted len 4.47, 68.3 tok/s
  on Qwen3.6-27B NEO-CODE + vision @262144, 2026-07-27) — judge tok/s, not acceptance.
- **DFlash (upstream llama.cpp-stable/nightly):** same `spec-type: draft-dflash` + local
  upstream-schema drafter (Alittlehammmer/williamliao), `spec-draft-n-max: 15`. Flat only, no
  adaptive controller. `draft-dflash` IS in the curated list-valued enum (schema
  `llama.cpp-stable.json`; S1 fixed) — it **validates in `args`**. BeeLlama v0.4.0 now runs the
  same upstream DFlash but adds its default-on `profit` depth controller on top.
- **DFlash (Poolside Laguna fork):** use the matched Poolside BF16 draft, `draft-dflash`, n-max 15,
  and f16/f16 draft KV. Measure `draft_n_accepted/draft_n` on representative traffic; a repetitive
  templated-code win is not sufficient for an agent-profile promotion.
- **KVarN KV compression (beellama v0.4.0 only):** set `"cache-type-k"`/`"cache-type-v"` to
  `kvarn2`..`kvarn6`/`kvarn8` for target-context compression; optional SWA-layer overrides
  `"cache-type-k-swa"`/`"cache-type-v-swa"` (kvarnN only) and precision tail `"kv-tail-tokens": "auto"`
  + `"kv-tail-type"` (f16/bf16) keep the newest attention-visible entries exact. Legacy target
  `turbo2/3/4[_tcq]` cache names warn and redirect by width to `kvarn2/3/4` (draft-cache turbo
  aliases redirect to `q2_0/q3_0/q4_0`); actual TurboQuant/TCQ GGUF cache formats are unsupported.
- **ngram (zero-asset):** `"spec-type": "ngram-mod"` (defaults n-match 24 / n-min 48 / n-max 64).
- Dual-GPU: `--spec-draft-device CUDA1` (or CUDA0) parks the draft on the other card.
  **External drafts under `split-mode tensor` are unsafe** (silent stop, #22473) — layer split only. **Embedded MTP/nextn is per-model** (some run, some crash #24309/#24440; workaround `LLAMA_GRAPH_REUSE_DISABLE=1` / `-sm layer`): a load proves compatibility, not speed — gate any promotion (dual-gpu.md, full-tuning.md).

## MoE specifics (35B-A3B class)

- Policy: **never naive `--n-cpu-moe`/CPU expert spill** (and pin `-ngl 99` so `--fit` can't do
  it silently). If a MoE genuinely must shrink below its all-GPU footprint, the sanctioned path
  is **Luce Spark on lucebox-dflash** (references/dflash.md — measured 100 vs 66 tok/s naive).
  Exception on explicit user request only: `--n-cpu-moe N` in extraArgs (~0.45 GiB/layer on
  35B-A3B, mild decode cost) — the existing `…-cpumoe` profile is such a case.
- Anchor: 35B-A3B UD-Q4_K_M, draft-mtp n-max 3, q4_0/q4_0, ctx 204800, `--n-cpu-moe 2` →
  ~180 tok/s short / ~154 @25k, accept 71%/67%, peak ~23.4 GiB.

## Tuning by model type

Same GGUF engine, different knob priorities on Ampere/SM86. Every knob below validates in `args`
(present in the curated schemas) unless flagged as extraArgs.

- **Dense** (Qwen-dense, Mistral, Gemma-dense): prefill-bound — `"batch-size": 2048,
  "ubatch-size": 2048` (official prefill reco; `common/arg.cpp` code defaults are b 2048 / ub
  512). The CUDA compute pool grows with `ubatch × ctx`, so under VRAM pressure step ubatch
  2048→1024→512 before cutting ctx. KV `q8_0/q8_0` for agents/tools, `q4_0/q4_0` only for
  max-ctx (see Quant combos). `cache-reuse` 256 pays off on long stable prefixes; speculate with
  beellama DFlash or zero-asset `ngram-mod`.
- **MoE / A3B** (35B-A3B class): **never `n-cpu-moe`/`cpu-moe`** — both ARE real curated schema
  flags (they would validate) but are banned by rig policy; pin `"n-gpu-layers": 99` so `--fit`
  cannot spill experts silently. To shrink a MoE below its all-GPU footprint use **Luce Spark on
  lucebox-dflash** (references/dflash.md — measured 100 vs 66 tok/s vs naive offload), not CPU
  offload. `--n-cpu-moe N` in extraArgs is the explicit-request-only exception (~0.45 GiB/layer,
  mild decode cost). Hybrid-recurrent A3B (Qwen3.5/3.6): `cache-reuse` is ineffective (KV-shift
  cannot reuse recurrent state) — rely on prefix cache + `"swa-checkpoints": 32`. Prefer
  `draft-mtp` (finetune-robust) over DFlash. `override-tensor` (`-ot`, schema key
  `override-tensor`) can pin named expert tensors per device, but on this rig keep experts fully
  on GPU.
- **VL** (Qwen-VL, Gemma-VL, JoyCaption): `"mmproj": "/abs/path"` + extraArg
  `--no-mmproj-offload` (projector on CPU, zero VRAM; `--no-mmproj-offload` is schema-absent, so
  it lives in `extraArgs` as a raw passthrough). **Silent disablers, expected — do not "fix":**
  `mmproj` kills `cache-reuse` and context-shift upstream (only plain prefix reuse survives) and
  on beellama forces flat DFlash + disables all non-DFlash spec
  (`backends/beellama.cpp/docs/beellama-args.md`). Image-token bursts inflate the prefill batch —
  if VRAM spikes on the first multimodal request, drop `ubatch-size` to 512 before touching ctx.
  A VL model used purely as text: omit `mmproj` entirely to keep cache-reuse.
- **Coding / reasoning** (coders, thinking finetunes): precision-biased quant (Q5_K_S+), KV
  `q8_0/q8_0` (tool-calling profiles NEVER `q4_0` KV — measured corruption). Anti-loop baseline
  `repeat-penalty 1.05 + repeat-last-n 256`, **no `dry-*`** (user policy). The
  `draft-mtp,ngram-mod` chain validates in `args` on llama.cpp-stable/nightly (`spec-type`
  `"list": true`) and adds free hits on re-emitted code. beellama adds a reasoning-loop-guard
  (`"reasoning-loop-guard": "force-close"` — enum `off|force-close|stop`, default force-close;
  `backends/beellama.cpp/docs/beellama-args.md`) that force-closes runaway `<think>` loops, a
  beellama-only knob for loop-prone reasoning models. Set `"reasoning": "off"` for Instruct-class
  GGUFs misdetected as thinking (#20809).

## Multi-GPU (see dual-gpu.md — measured rules)

Fitting model → pin GPU1 (`launch.env`: `CUDA_DEVICE_ORDER=PCI_BUS_ID`, `CUDA_VISIBLE_DEVICES=1`)
— on a 2-GPU box llama.cpp auto-splits when both cards are visible, silently changing proven
numbers. `split-mode tensor` = fastest dense batch-1 at short ctx (+22…39%, reverses ≥512k;
external drafts unsafe, embedded MTP/nextn per-model; no fit; set ngl+ctx manually); `layer` = safe/compatible (+4…7%);
`row` = deprecated, −2.8×. **`tensor-split` MUST be `"0.5,0.5"`** when splitting (never asymmetric ratios); `main-gpu 1`, no CUDA_VISIBLE_DEVICES mask when splitting.

**P2P (patched rig):** `GGML_CUDA_P2P=1` is **source-verified for this family** (`getenv` in
`ggml/src/ggml-cuda/ggml-cuda.cu`; llama.cpp-stable/nightly + beellama + buun — these builds are
NCCL-ON, so peer access is already on for the NCCL path and the env mainly grants it to the VMM copy
path). **Measured 2026-07-10 (ornith-9B tensor-split): env-proven engaged but a TIE (decode ~141 /
prefill ~4120 both on/off) — batch-1 single-stream decode is weight-bandwidth-bound. Keep the DEFAULT
(absent).** It is split-only (no effect on a single-GPU pin), stays a log-proven A/B lever carrying the
upstream IOMMU/BIOS crash caveat, and relaxes **no** rule below — tensor still crashes with external
drafts, `row` stays banned, `tensor-split` stays `0.5,0.5`. (dual-gpu.md §P2P.)

## Measured anchors (carry values, not names)

| Recipe | ctx | tok/s | Notes |
|---|---|---|---|
| 27B Q4_K_M + DFlash Q4_K_M, q4_0/q4_0, beellama | 204800 | ~157, acc ~57% | proven long-running anchor |
| same @ native 262144 | 262144 | 81.5 avg / 66.5 @250k | peak 23723 MiB, ~34 MiB margin |
| 35B-A3B UD-Q4_K_M draft-mtp, ncmoe 2 | 204800 | ~180 / ~154 @25k | MoE reference |
| Qwythos-9B Q8_0 MTP, split-mode tensor | 262144 | 166 (+22% vs single) | 1M ctx possible only under tensor (157.9) |
| 27B DFlash layer-split | 131072 | 79–83 > tensor-no-draft 48–49 | keeping the draft beats tensor |

_The split rows above (tensor / layer-split deltas) were measured **pre-P2P**; on the patched driver
`GGML_CUDA_P2P` measured a TIE for these single-stream splits (dual-gpu.md §P2P) — the model-class
rules are unchanged._

## Profile template (adapt; drop spec keys when not speculating)

```json
{
  "schemaVersion": 3,
  "id": "<model>-<variant>-<ctx>k",
  "name": "<Human Name> (<ctx>k, <backend>)",
  "description": "<quant> + <spec>, <K>/<V> KV, ~<N> GiB peak, ~<N> tok/s, template <source>.",
  "model": "/abs/path/target.gguf",
  "args": {
    "batch-size": 2048, "ubatch-size": 2048,
    "cache-type-k": "q8_0", "cache-type-v": "q8_0",
    "ctx-size": 204800, "flash-attn": "on", "host": "127.0.0.1",
    "jinja": true, "n-gpu-layers": 99, "parallel": 1,
    "cache-reuse": 256,
    "spec-type": "draft-mtp", "spec-draft-n-max": 3,
    "temperature": 0.6, "top-k": 20, "top-p": 0.95, "min-p": 0,
    "repeat-penalty": 1.05, "repeat-last-n": 256
  },
  "extraArgs": ["--no-mmap", "--mlock", "--no-host"],
  "launch": { "defaultBackground": true, "backendId": "llama.cpp-stable",
              "env": [{"key":"CUDA_DEVICE_ORDER","value":"PCI_BUS_ID"},
                      {"key":"CUDA_VISIBLE_DEVICES","value":"1"}],
              "restart_policy": "none", "max_restarts": 3, "backoff_seconds": 5 },
  "pinned": false
}
```

Always set `ctx-size` + KV types explicitly. Sampling values come from the model-research pass
(the template shows the Qwen3.6 precise-coding preset) — llama.cpp ignores
`generation_config.json`, and its own defaults (temp 0.8/top-k 40/min-p 0.05) are wrong for
most models. `repeat-penalty 1.05 + repeat-last-n 256` is the standing anti-loop baseline;
**no `dry-*` keys** (user policy — see model-research.md). `chat-template-file` and
`reasoning-format` are present in the current schema and validate in `args`; only genuinely
schema-absent overrides (e.g. `--no-mmproj-offload`) go in `extraArgs` as raw passthroughs.

Vision: `"mmproj": "/abs/path"` + extraArg `--no-mmproj-offload` (projector on CPU, zero VRAM,
per rig policy). mmproj forces flat DFlash (beellama), disables all non-DFlash spec (beellama)
and kills cache-reuse/context-shift (upstream) — expected, don't "fix" it.

## OOM sacrifice ladder (one step per iteration, re-measure)

Dense: ctx ↓ → KV one bpv step down (never below q8_0 on tool-calling profiles) → target quant
one tier down → ubatch 2048→1024→512 (halves the prefill transient; costs prefill speed).
**Non-agent only** (caption/OCR/prose, never tool-calling): tighten KVarN one `kvarn*` step
(beellama v0.4.0 `cache-type-k`/`-v`) as an alternative to the quant/ubatch steps. On tool-calling
profiles the q8_0 floor holds — cut ctx or ship a lower-context variant instead.
MoE: consider Spark on lucebox-dflash (dflash.md) → draft KV q8_0 via `--spec-draft-type-*` →
ubatch down → dense ladder. (`--n-cpu-moe` only on explicit user request.)

## llama.cpp-prisma-ml

Registered as **`llama.cpp-prisma-ml`** (PrismML fork of llama.cpp, b9597 `7529fdaaf`, base
b9594, remote `PrismML-Eng/llama.cpp`). Same `llama-server` kind and canonical flag mapping as
stable/nightly; its on-disk schema is live-parsed from the fork binary and **enrich-only** — the
shared `CuratedLlamaSchema()` overlay is applied for metadata but never appends upstream-only
flags the fork lacks (so `--cors-*`, `--reasoning-preserve`, `--mtmd-batch-max-tokens` are absent
from the prisma schema, matching the binary; S8). Fork deltas:

- **Q2_0 2-bit ternary weights** — the reason the fork exists (the [Bonsai](https://huggingface.co/collections/prism-ml/bonsai)
  models), plus `Q1_0`. This is a **weight** quant type (`--model` GGUF), NOT a KV-cache type.
  **Format gotcha:** the fork loads `*-Q2_0.gguf` (group size 128); mainline llama.cpp uses
  `*-Q2_0_g64.gguf` (group 64, CPU/Metal only) and the two are **mutually incompatible** —
  `Q2_0_g64` does not load on the fork, `Q2_0` (g128) does not load on mainline. `*-PQ2_0.gguf`
  is a planned fork format, unsupported anywhere yet. Never mix this fork's `ggml-*` libraries
  into a stock build (ABI/format mismatch).
- **`draft-dspark`** — the fork exposes DSpark (DeepSpec block-diffusion / Markov) as a
  `spec-type` value (full list: `none,draft-simple,draft-eagle3,draft-mtp,draft-dspark,ngram-*`),
  but it is an **experimental port not usable via model-loader**. The drafter needs the driver to
  engage multi-layer target-context capture (`llama_set_capture_layers` + per-row logits) before
  drafting; **the server/CLI `--spec-type` path does NOT engage capture and fails at the first
  draft round** with a clear error (`backends/llama.cpp-prisma-ml/common/speculative.cpp:40-46`).
  Only the reference driver `tests/test-dspark-real-eval.cpp` runs it. model-loader launches via
  the server, so **never route production speculation to `draft-dspark`** — use MTP/DFlash. It is
  also unproven vs MTP per the fork's own docs.
- **`--kv-mean-center FNAME`** — loads a precomputed per-(kv-head, channel) K-cache bias GGUF
  that subtracts a fixed mean before Q4_0 quantization, improving K-cache fidelity at
  **zero decode cost** (softmax-invariant). **Requires `--cache-type-k q4_0`** (context creation
  fails otherwise). Generate the bias with the fork's `tools/kv-mean-center`; the bias records
  its rotation basis and the loader rejects a mismatch, so calibrate with the same `-ctk q4_0`
  (and Hadamard-rotation) settings you serve with. Scope: standard dense/GQA + SWA + hybrid
  attention caches only — not MLA/DSA or recurrent-only. Do **not** set it without a matched
  calibration file for the exact model.
- CUDA fast paths gate on GPU arch (Hopper-only wgmma, Blackwell rejected from that gate) —
  **neither applies to this SM86 Ampere rig (RTX 3090)**: the fork runs its ordinary CPU/CUDA
  Q2_0/Q1_0 kernels on Ampere, with no Hopper-class acceleration to expect here.

Route here only for a Bonsai/Q2_0 (g128) GGUF; otherwise stable/nightly remain the default
llama-server backends. Do not pick prisma-ml for DSpark (unusable via server, above).

## buun-llama-cpp

Registered as **`buun-rtx3090`** in the catalog (predecessor fork, superseded by beellama for
every use case — recommend beellama and record that in the description if the user insists on
buun). Buun (b9792) keeps buun-era DFlash/draft names — `--spec-dflash-default`,
`--dflash-max-slots`, `--draft-max`, `--draft-model`, `--draft-topk`, `--tree-budget`
(`backends/buun-llama-cpp/common/arg.cpp`). BeeLlama v0.3.x renamed these to `--spec-*`, and
**v0.4.0 then removed the whole `--spec-dflash-*` family entirely** (adopting upstream flat
`draft-dflash`; upstream keeps `--draft-max` as a live alias but has none of the DFlash-slot
controls). Never mix spellings across the three backends; trust each one's own schema.

## Gotchas

- beellama v0.4.0 exposes canonical `--spec-*` only (old `--draft`, `--draft-model`,
  `--draft-topk`, `--tree-budget` aliases and the fork's `--spec-dflash-*` knobs are both gone;
  `backends/beellama.cpp/docs/beellama-args.md`).
- `top-k` is sampling; beellama v0.4.0 removed the fork's `spec-draft-top-k`/`spec-draft-temp`
  tree-drafting knobs (upstream flat `draft-dflash` has no per-draft top-k/temp).
- `--kv-unified` stays on for beellama single-user long-ctx (idle-slot caching needs it).
- Launch log lines to read every time: `n_ctx_seq (…) < n_ctx_train (…)`, KV buffer sizes,
  `Chat format: peg-native` (template parser OK), draft/MTP/dflash load + acceptance lines,
  `common_fit_params` messages (did fit touch anything you meant to pin?). `failed to mlock` =
  harmless RLIMIT warning.
- Schemas are curated + `source.editable`, so **incidental** re-runs (AddBackend retries /
  catalog-ensure) never overwrite them — but `backend schema refresh <id>` **does** update them:
  it deletes the on-disk schema and regenerates, **re-parsing the current binary's `--help`**
  (embedded-golden fallback + curated overlay; `generator.go`/`parseHelpSchema`, shared by
  llama-server/beellama/buun), so a rebuild's flag changes ARE picked up on refresh and any
  hand-edits are wiped. Real-but-absent flags still go in `extraArgs`; never hand-edit schema JSON.
- Qwen3-Instruct-class GGUFs misdetected as thinking (#20809): add `"reasoning": "off"`.

# llama family on the dual-3090 rig — llama-server (b9847 stable / b9869 nightly), beellama-cpp (main@85e22ea0, b10102-dirty), buun-llama-cpp (b9792)

Catalog ids: **`llama.cpp-stable`** (b9847, `e495d1e74`) and **`llama.cpp-nightly`** (b9869,
`d4cff114c` — 22 commits past b9847; profile-relevant flag surface is IDENTICAL, verified by
diffing `common/arg.cpp`: 410 registered flags each, empty diff) for kind `llama-server`;
**`beellama-rtx3090`** (beellama fork, `main@85e22ea0`, build **b10102-dirty** — "-dirty" =
local build-tree patches, documented as-is; past the v0.3.1 tag); **`buun-rtx3090`** (buun fork,
b9792 `87c351d28`, superseded — recommend beellama unless the user insists). All take local GGUF
paths (`--model` emitted from the profile `model` field; HF repo ids REJECTED by the validator
for these kinds) and share canonical flag mapping (`ngl` → `n-gpu-layers`). On-disk schemas are
curated and `source.editable` (242 flags llama.cpp-stable/nightly, 271 beellama, 251 buun) — a
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
| Dense GGUF + DFlash drafter | beellama-rtx3090 (`spec-type: dflash`, adaptive controller, both drafter schemas) — upstream `draft-dflash` on llama.cpp-stable now validates in `args` too (it is in the list-valued `spec-type` enum; S1 fixed), flat only | DFlash beats draft-mtp on dense (measured); beellama's adaptive `dflash` is the measured-better path, upstream flat `draft-dflash` is a reachable alternative |
| Plain GGUF, no spec assets | llama.cpp-stable + `spec-type: ngram-mod` | free speedup on agent/code loops (~16 MB, zero VRAM) |
| Finetunes (abliterated/heretic/NEO) | prefer native MTP | DFlash acceptance collapses on finetunes (57%→~24%) unless a matched drafter exists (e.g. AEON-7); MTP heads survive finetuning |
| EAGLE-3 head exists, no MTP/DFlash | llama.cpp-stable `spec-type: draft-eagle3` + converted head | Qwen3.5/3.6-hybrid fixes are in b9847; agentic-trained heads (Ex0bit) best |

Speculative details + asset inventory: **references/speculative.md**. Sampling/template/tool-call
setup: **references/model-research.md** (mandatory research pass).

## b9847 features that change profile-writing (vs older builds)

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
  `--draft-min`/`--draft-n-min` are STILL live aliases on b9847/b9869 (`common/arg.cpp`
  L3907/L3914) — not removed; the canonical spec-family names are `--spec-draft-n-max` /
  `--spec-draft-n-min`, prefer those. Dialects DO differ by backend (`common/speculative.cpp`
  type maps): `mtp` and `dflash` are beellama/buun-only `spec-type` spellings — upstream uses
  `draft-mtp` and `draft-dflash`, upstream has NO `dflash` and beellama/buun have NO
  `draft-dflash` (beellama's DFlash is its own `dflash` implementation). Never mix spellings
  across the three backends; trust each backend's schema.
- `--defrag-thold` deprecated/no-op. `--no-host`: AMX-specific, skip on this rig.
- mmap default ON is what makes proxy hot-swaps fast (page cache). The library's anchors use
  `--no-mmap --mlock --no-host` (avoids page-cache eviction stalls mid-session; mlock may log a
  harmless RLIMIT warning). Either is defensible — don't churn existing profiles over it.

## VRAM budget

`weights (GGUF size) [+ drafter] + KV + compute buffers ≤ ~23.2 GiB (GPU1)`

- KV bytes ≈ `ctx × n_layer × n_kv_heads × head_dim × (bpv_K + bpv_V) / 8` — **dense
  full-attention only**. Hybrid/SWA (Gemma, qwen3.5/3.6) cost far less (measured: Qwen3.6-27B
  q4_0 ≈ 22.5 KiB/token vs 72 by formula). Scale from an anchor or read the KV line in the log.
- bpv ladder: f16 16 · q8_0 8.5 · q5_1 6.0 · q5_0 5.5 · q4_1 5.0 · q4_0/iq4_nl 4.5 ·
  turbo4 4.125 · turbo3_tcq 3.25 · turbo3 3.125 · turbo2_tcq 2.25 (turbo* = beellama, CUDA only).
- Compute buffers scale with `ubatch × ctx`. **Long-prompt prefill grows the CUDA pool at
  runtime** — idle fit ≠ fit. Always test a near-full-ctx prompt + re-check nvidia-smi.

## Quant combos by bias (dense)

| Bias | Target quant | Drafter | KV K/V | Notes |
|---|---|---|---|---|
| Precision (coding/agents) | Q5_K_S / Q5_K_M (or Q6/Q8 if it fits) | Q4_K_M / IQ4_XS | **q8_0/q8_0** | tool-calling profiles NEVER q4_0 KV (official caution + measured corruption here); K more sensitive than V — q5_0/q4_1 is the compromise pair |
| Balanced / max context | Q4_K_M / UD-Q4_K_XL | Q4_K_M | q4_0/q4_0 | proven: 27B + drafter @204800 ≈ 22.4 GiB idle; 262144 fits at peak 23723 MiB — razor-thin |
| Extreme squeeze | IQ4_XS | IQ4_XS | turbo3_tcq (beellama) | real quality cost — explicit request only |

Short ctx (≤32k) with headroom → keep KV f16/q8_0. **Q8 drafters are never better than Q4_K_M**
(measured; drafting doesn't need precision). Unsloth UD (Dynamic 2.0) quants are the
quality-per-GB pick within GGUF when available.

## Speculative setup (see speculative.md for the full landscape)

- **MTP:** `"spec-type": "draft-mtp"` (embedded nextn head — no drafter file; confirm the head
  loads in the log), `"spec-draft-n-max": 3` (**6 OOMs at 200k**, +2.1 GiB). For agent traffic
  chain the free ngram drafter: `"spec-type": "draft-mtp,ngram-mod"` — this **validates directly
  in `args`** on llama.cpp-stable/nightly (`spec-type` is list-valued, `"list": true`;
  `checkEnum` splits the comma-list; S1 fixed), so ship the chain, do NOT route it through
  extraArgs; confirm both the MTP head and the ngram drafter load from the launch log (sparse at default verbosity — if no draft line shows, raise `--log-verbosity`/`lv` or read a response's server `timings.draft_n`/`draft_n_accepted`). MTP draft
  ctx defaults f16 KV — set `"cache-type-k-draft": "q8_0", "cache-type-v-draft": "q8_0"` in
  `args` (schema keys; `--spec-draft-type-k`/`-v` are accepted aliases that resolve to the same
  flags via `Lookup`).
- **DFlash (beellama):** `"spec-type": "dflash"`, `"spec-draft-model": "/abs/drafter.gguf"`,
  `"spec-draft-ngl": 99`, `"spec-dflash-cross-ctx": 1024`, `"kv-unified": true`, `"cache-ram": 0`
  (beellama's own doc default) — leave `spec-draft-n-max` unset (adaptive `profit` controller).
  Drafters: Anbeeld/Lucebox/spiritbuun GGUFs (beellama takes both schemas). Watch acceptance in
  the log; ~0.86 on templated code vs ~0.21–0.27 on prose — judge tok/s, not acceptance.
- **DFlash (upstream llama.cpp):** spec-type `draft-dflash` + `-md` upstream-schema drafter
  (Alittlehammmer/williamliao), `"spec-draft-n-max": 15`. New in b9847 — flat only; calibrate
  against beellama before switching. `draft-dflash` IS in the curated list-valued `spec-type`
  enum (schema `llama.cpp-stable.json`; S1 fixed) — it **validates in `args`**. beellama's
  adaptive `dflash` dialect stays the measured-better DFlash path on dense; upstream flat
  `draft-dflash` is the reachable alternative when beellama is not wanted.
- **ngram (zero-asset):** `"spec-type": "ngram-mod"` (defaults n-match 24 / n-min 48 / n-max 64).
- Dual-GPU: `--spec-draft-device CUDA1` (or CUDA0) parks the draft on the other card.
  **`split-mode tensor` crashes with ANY external draft** — layer split only (dual-gpu.md).

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
no external drafts; no fit; set ngl+ctx manually); `layer` = safe/compatible (+4…7%);
`row` = deprecated, −2.8×. **`tensor-split` MUST be `"0.5,0.5"`** when splitting (never asymmetric ratios); `main-gpu 1`, no CUDA_VISIBLE_DEVICES mask when splitting.

**P2P (patched rig):** genuine layer/tensor splits can now use validated PCIe P2P as an A/B lever
(dual-gpu.md §P2P). `GGML_CUDA_P2P=1` is **source-verified for this family** (`getenv` in
`ggml/src/ggml-cuda/ggml-cuda.cu`; llama.cpp-stable/nightly + beellama + buun — these builds are
NCCL-ON, so peer access is already on for the NCCL path and the env mainly grants it to the VMM copy
path). It is split-only (no effect on a single-GPU pin), must be **log-proven + warm-A/B'd** (never
assume a speedup), carries the upstream IOMMU/BIOS crash caveat, and relaxes **no** rule below —
tensor still crashes with external drafts, `row` stays banned, `tensor-split` stays `0.5,0.5`.

## Measured anchors (carry values, not names)

| Recipe | ctx | tok/s | Notes |
|---|---|---|---|
| 27B Q4_K_M + DFlash Q4_K_M, q4_0/q4_0, beellama | 204800 | ~157, acc ~57% | proven long-running anchor |
| same @ native 262144 | 262144 | 81.5 avg / 66.5 @250k | peak 23723 MiB, ~34 MiB margin |
| 35B-A3B UD-Q4_K_M draft-mtp, ncmoe 2 | 204800 | ~180 / ~154 @25k | MoE reference |
| Qwythos-9B Q8_0 MTP, split-mode tensor | 262144 | 166 (+22% vs single) | 1M ctx possible only under tensor (157.9) |
| 27B DFlash layer-split | 131072 | 79–83 > tensor-no-draft 48–49 | keeping the draft beats tensor |

_The split rows above (tensor / layer-split deltas) were measured **pre-P2P**; on the patched driver
re-measure with P2P as an A/B lever (dual-gpu.md §P2P) — the model-class rules are unchanged._

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
one tier down → `spec-dflash-cross-ctx` 1024→512 → ubatch 2048→1024→512 (halves the prefill
transient; costs prefill speed).
MoE: consider Spark on lucebox-dflash (dflash.md) → draft KV q8_0 via `--spec-draft-type-*` →
ubatch down → dense ladder. (`--n-cpu-moe` only on explicit user request.)

## buun-llama-cpp

Registered as **`buun-rtx3090`** in the catalog (predecessor fork, superseded by beellama for
every use case — recommend beellama and record that in the description if the user insists on
buun). Buun (b9792) keeps buun-era DFlash/draft names — `--spec-dflash-default`,
`--dflash-max-slots`, `--draft-max`, `--draft-model`, `--draft-topk`, `--tree-budget`
(`backends/buun-llama-cpp/common/arg.cpp`) — that beellama v0.3.x dropped or renamed to
`--spec-*`/`--spec-dflash-*` (upstream keeps `--draft-max` as a live alias but has none of the
DFlash-slot controls). Never mix spellings across the three backends; trust each one's own schema.

## Gotchas

- beellama v0.3.x removed old aliases (`--draft`, `--draft-model`, `--draft-topk`,
  `--tree-budget`, …) — canonical `--spec-*` only (`backends/beellama.cpp/docs/beellama-args.md`).
- `top-k` (sampling) ≠ `spec-draft-top-k` (beellama tree drafting).
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

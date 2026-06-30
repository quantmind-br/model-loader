# llama family on RTX 3090 24GB — llama-server, beellama-cpp, buun-llama-cpp

All three take local GGUF paths (`--model` emitted from the profile `model` field; HF repo ids
are REJECTED by the validator for these kinds), share canonical flag mapping (`ngl` →
`n-gpu-layers`), and share the KV math below. Differences: beellama adds DFlash + TurboQuant
KV; buun is the predecessor fork (superseded — see §buun).

## Which backend within the family

| Situation | Backend (catalog id) | Why (measured 2026-06) |
|---|---|---|
| MoE GGUF with native MTP head (`*-MTP`, nextn layer) | llama-cpp-default | draft-mtp ~180–208 tok/s @ 70%+ acceptance; beats DFlash (~130–160 @ ~23%). Cheap MoE verify pass → acceptance dominates |
| Dense GGUF + DFlash drafter exists (`Anbeeld/<model>-DFlash-GGUF`) | beellama-rtx3090 | DFlash ~86–157 tok/s beats draft-mtp (~62) — expensive dense verify pass favors 16-deep drafts |
| Dense GGUF, no drafter, has MTP head | llama-cpp-default with draft-mtp | works upstream; no beellama features needed |
| Plain GGUF, no speculation assets | beellama-rtx3090 or llama-cpp-cuda | equal speed; beellama gives KV-type headroom (turbo*) |
| Finetunes (abliterated/heretic/NEO-CODE etc.) | prefer native MTP if present | finetuning shifts hidden states → DFlash acceptance drops 57%→~24%; MTP heads survive finetuning |

## VRAM budget

`weights (GGUF file size) [+ drafter GGUF] + KV + compute buffers ≤ ~23.2 GiB`

- KV bytes ≈ `ctx × n_layer × n_kv_heads × head_dim × (bpv_K + bpv_V) / 8` — **dense
  full-attention models only**. Hybrid/SWA models (Gemma; qwen3.5/3.6 arch has
  `full_attention_interval` → only a fraction of layers carry full-ctx KV) cost far less —
  measured: Qwen3.6-27B at q4_0/q4_0 ≈ 22.5 KiB/token vs 72 KiB by the formula (3× over).
  Scale from an anchor or read the KV-size line in launch logs instead of trusting the formula.
- bpv ladder: f16 16 · q8_0 8.5 · q5_1 6.0 · q5_0 5.5 · q4_1 5.0 · q4_0/iq4_nl 4.5 ·
  turbo4 4.125 · turbo3_tcq 3.25 · turbo3 3.125 · turbo2_tcq 2.25 · turbo2 2.125
  (turbo* = beellama only, CUDA only).
- Compute buffers scale with `ubatch-size × ctx`; defaults batch 2048 / ubatch 512.
- **Long-prompt prefill grows the CUDA pool at runtime** — idle fit ≠ fit. With `n-gpu-layers`
  pinned, the binary's auto-fit aborts (log: `common_fit_params: ... abort`) so nothing saves
  you. Always test one near-full-ctx prompt and re-check nvidia-smi.

## Quant combos by bias (dense models)

| Bias | Target quant | Drafter quant | KV K/V | Notes |
|---|---|---|---|---|
| Precision (coding/agents) | Q5_K_S / Q5_K_M | Q4_K_M | q5_0 / q4_1 | K is more sensitive than V — asymmetric pairs are the quality/size frontier |
| Balanced / max context | Q4_K_M / Q4_K_XL | Q4_K_M | q4_0 / q4_0 | proven: 27B + drafter @ 204800 ≈ 22.4 GiB idle; native 262144 fits at peak 23723 MiB — only ~34 MiB under budget, razor-thin, prefer 204800 unless user wants the absolute max |
| Extreme squeeze | IQ4_XS | IQ4_XS | turbo3_tcq (beellama) | real quality cost — only on explicit request |

Short ctx (≤32k) with headroom → keep KV at f16/q8_0; don't quantize what you don't need to.
**Q8_0 drafters are never clearly better than Q4_K_M** (measured: worse on dense, tie on MoE)
— drafting doesn't need precision.

## Speculative decoding setup

- **DFlash (beellama only)**: `"spec-type": "dflash"`, `"spec-draft-model": "/abs/drafter.gguf"`,
  `"spec-draft-ngl": 99`, `"spec-dflash-cross-ctx": 1024` (default 512 drafts worse past ~32k),
  `"kv-unified": true`, `"cache-ram": 0`. Leave `spec-draft-n-max` unset (adaptive `profit`
  controller, effective 16). Drafters only from `Anbeeld/*-DFlash-GGUF`-style repos — a plain
  GGUF as drafter fails to load. Watch acceptance in the launch log (`dflash: contract ok`,
  `draft acceptance =` lines); silent inactivity = misconfigured drafter. Acceptance varies by
  workload: ~0.86 on greedy/templated output, ~0.21–0.27 on natural prose (measured) — judge
  tok/s, not acceptance alone.
- **MTP (`draft-mtp`, upstream + beellama)**: for GGUFs with a native MTP/nextn head. No drafter
  file. `"spec-draft-n-max": 3` — **n-max 6 OOMs at 200k ctx** (+2.1 GiB recurrent buffer).
  The MTP draft context defaults to f16 KV regardless of `cache-type-*` (wastes ~0.4 GiB at
  200k): if the binary supports `--spec-draft-type-k/-v` (check `llama-server --help` via
  schema refresh, NOT by running the server), pass `--spec-draft-type-k q8_0
  --spec-draft-type-v q8_0` in extraArgs.
- Neither → no speculation by default; ngram modes only for highly repetitive workloads.

## MoE specifics (35B-A3B class)

- `--n-cpu-moe N` in **extraArgs** (valid in binary, absent from the pinned schema): each +1
  moves one layer's expert tensors (~0.45 GiB on 35B-A3B) to CPU. The cheap OOM knob — decode
  cost is small because only ~3B params are active.
- Anchor: 35B-A3B UD-Q4_K_M, draft-mtp n-max 3, q4_0/q4_0, ctx 204800, `--n-cpu-moe 2` →
  ~180 tok/s short / ~154 @25k, acceptance 71%/67%, peak ~23.4 GiB (tight; raise n-cpu-moe
  before lowering ctx).

## Multi-GPU on the dual-3090 rig (no NVLink)

Only when weights + KV exceed one card (≤23 GiB). A model that fits one card → **pin it** to GPU1
instead (`launch.env`: `CUDA_DEVICE_ORDER=PCI_BUS_ID`, `CUDA_VISIBLE_DEVICES=1`) — on a 2-GPU box
llama.cpp auto-layer-splits when both cards are visible, silently changing a fitting profile's
behavior and its proven numbers. Full rationale + the 48 GiB model tier in **references/dual-gpu.md**.

- `"split-mode": "layer"` (the default) pools the 48 GiB with minimal cross-GPU traffic. **Never
  `"row"`** (deprecated, slower than layer even with NVLink); avoid `"tensor"` on THIS rig (a genuine
  tensor-parallel mode, superior *with* NVLink/P2P, but no-NVLink makes it interconnect-bound and it
  forces F16/BF16 KV + OOMs as ctx grows).
- `"tensor-split": "0.45,0.55"` is in **device-index order** — device0 (GPU0, desktop, ~22.4 GiB)
  gets the smaller share, device1 (GPU1, clean, ~23.2 GiB) the larger; `"main-gpu": 1` puts
  prompt-processing / non-split tensors on the clean card. Set `launch.env`
  `CUDA_DEVICE_ORDER=PCI_BUS_ID` so the device index matches `nvidia-smi` (deterministic for two
  identical 3090s) — otherwise the heavier share can land on the desktop card and OOM it. Don't add a
  `CUDA_VISIBLE_DEVICES` mask when you genuinely split — both cards must be visible.
- **Never set `GGML_CUDA_P2P=1`** (no GeForce P2P; PCIe-speed anyway; corrupts on some boards).
- MTP / DFlash speculation still works under layer split — confirm the draft loads in the log.
- ⚠ Validate a **long generation**, not just startup: layer split can emit garbage above ~2k ctx on
  a non-P2P PCIe dual-3090 with asymmetric slot widths (llama.cpp #20052).
- Splitting a fitting model does **not** raise single-stream tok/s (−3…−6% vs one card) — it is a
  capacity move, not a speed move.

## Measured anchors (this card; carry values, not profile names — profiles change)

| Recipe | ctx | Idle / peak MiB | tok/s | Notes (2026-06-12) |
|---|---|---|---|---|
| 27B Q4_K_M + DFlash Q4_K_M, q4_0/q4_0, beellama | 204800 | 22081 / — | ~157, acc ~57% | proven long-running anchor |
| same, native max | 262144 | 23387 / 23723 | 81.5 avg bench, 100.8 best, 66.5 @250k depth | fits with ~34 MiB margin — razor-thin |
| 35B-A3B UD-Q4_K_M draft-mtp, q4_0/q4_0, llama-cpp-default, ncmoe 2 | 204800 | — / ~23.4 GiB | ~180 short / ~154 @25k, acc 71%/67% | MoE reference |
| 27B UD-Q4_K_XL + DFlash Q8_0 | 204800 | — / 24101 | 89 / 59, acc 27%/19% | Q8 drafter = net negative, OOM-risk peak |

## Profile template (adapt; drop spec-* keys when not speculating)

```json
{
  "schemaVersion": 3,
  "id": "<model>-<variant>-<ctx>k",
  "name": "<Human Name> (<ctx>k, <backend>)",
  "description": "<quant> + <spec strategy>, <K>/<V> KV, ~<N> GiB peak, ~<N> tok/s on RTX 3090 24GB.",
  "tags": ["<family>", "<dflash|mtp|plain>", "<ctx>k"],
  "model": "/abs/path/target.gguf",
  "args": {
    "batch-size": 2048, "ubatch-size": 512,
    "cache-type-k": "q4_0", "cache-type-v": "q4_0",
    "ctx-size": 204800, "flash-attn": "on", "host": "127.0.0.1",
    "jinja": true, "n-gpu-layers": 99, "parallel": 1,
    "temperature": 0.6, "top-k": 20, "top-p": 1, "min-p": 0
  },
  "extraArgs": ["--no-mmap", "--mlock", "--no-host"],
  "launch": { "defaultBackground": true, "backendId": "<catalog-id>", "restart_policy": "none",
              "max_restarts": 3, "backoff_seconds": 5 },
  "pinned": false
}
```

(Anchors keep `--no-mmap --mlock --no-host` in extraArgs even where the schema knows them —
mirroring that is fine; extraArgs is required only for schema-absent flags.)

Always set `ctx-size` and KV types explicitly (schema default ctx is 4096; `fit` silently
shrinks unset params). `min-p` schema default is 0.1 — override per model card.

Sampling per family (server-side defaults live in args for this family): Qwen3.x thinking →
temp 0.6, top-k 20, top-p 1, min-p 0 (Qwen's official thinking rec is top-p 0.95; the anchors use 1
— follow the specific model card when it differs), plus `"reasoning": "on"` (beellama),
`"chat-template-kwargs": "{\"preserve_thinking\":true}"`; Gemma → top-k 64, top-p 0.95,
min-p 0. Other families: check the model card.

Vision: `"mmproj": "/abs/path"` + extraArg `--no-mmproj-offload` (projector on CPU, zero VRAM).
mmproj forces flat DFlash and disables context-shift/cache-reuse — fine, don't "fix" it.

## OOM sacrifice ladder (in order; one step per iteration, re-measure)

Dense: ctx-size ↓ → KV one bpv step down → target quant one tier down →
`spec-dflash-cross-ctx` 1024→512 → ubatch 512→256 (halves the prefill transient; costs
prefill speed).
MoE: `--n-cpu-moe` +1..+4 **first** (~0.45 GiB each, mild cost) → draft KV to q8_0 via
extraArgs → ubatch 256 → then the dense ladder.

## buun-llama-cpp

Predecessor fork, superseded by beellama for every use case — recommend beellama unless the
user explicitly insists. Not in the catalog; on explicit request register it first:

```bash
model-loader backend add "buun llama.cpp" \
  --executable /home/diogo/dev/model-loader/backends/buun-llama-cpp/build/bin/llama-server \
  --kind buun-llama-cpp                               # name slugifies to id "buun-llama-cpp"
model-loader backend schema refresh buun-llama-cpp   # parses live --help + curated overlay
model-loader backend probe buun-llama-cpp
```

A user explicitly naming buun in the request counts as insisting — register and proceed, but
record the beellama recommendation in the profile description.

Never fabricate a `backendId` that isn't in `catalog.json` — validate fails. Buun's DFlash
dialect uses buun-era names (`spec-dflash-default`, `draft-max`, `dflash-max-slots`) that
beellama v0.3.0 removed — never mix the two spellings; trust each backend's own schema.

## Gotchas

- beellama v0.3.0 removed old aliases (`--draft`, `--draft-model`, `--draft-topk`,
  `--tree-budget`, `--spec-dflash-default`, …) — canonical `--spec-*` names only
  (full table: `backends/beellama.cpp/docs/beellama-args.md`).
- `top-k` (sampling) ≠ `spec-draft-top-k` (tree drafting). You almost never set the latter.
- `--kv-unified` stays on for beellama single-user long-ctx (idle-slot caching needs it).
- Launch log lines worth reading every time: `n_ctx_seq (...) < n_ctx_train (...)` (native ctx
  headroom), KV buffer sizes, `cache_k=f16` on the MTP draft context, acceptance rates.
  `failed to mlock` = host RLIMIT_MEMLOCK warning, harmless.
- Two llama-server catalog entries exist (default = PATH binary, cuda = repo build). Their
  schemas can drift after rebuilds — `backend schema refresh`, never hand-edit.

# Speculative decoding on this rig — technique × engine × assets (verified 2026-07-02)

Speculation is the single biggest tok/s lever on a 3090 for coding agents (code/tool-call output
has the HIGHEST draft acceptance of any workload: EAGLE-3 paper peaks on HumanEval; lucebox
measured AL 8.31 on HumanEval vs 6.14 GSM8K on this card). Always ask, per model: which of these
assets exist, and which engine exploits them best?

## Technique cheat sheet

| Technique | What | Assets | Best for |
|---|---|---|---|
| **MTP / nextn** | causal head(s) shipped with the target | head tensors in the GGUF/quant (VERIFY they exist) | best effort/VRAM ratio; ~91% accept measured (Qwen3.6-27B AWQ vLLM); **causal → coexists with fp8 KV** |
| **DFlash** (z-lab, arXiv 2602.06036) | ~2B block-diffusion drafter, 16-token block per forward, non-causal | drafter repo per family (see inventory) | deepest drafts; dense targets; **non-causal → blocks fp8 KV in vLLM on SM86** |
| **DSpark** (DeepSeek DeepSpec, 2026-06-27) | DFlash + semi-autoregressive Markov head + confidence head | official drafters ONLY Qwen3-4B/8B/14B, Gemma4-12B | beats DFlash on all 11 SPEED-Bench categories (+21%); **coding strongest (2.12×)**. **NOT runnable on anything installed today** — see §DSpark |
| **EAGLE-3** | 1-layer autoregressive head over target hidden states | trained head per target (SpecForge/speculators) | when no MTP/DFlash exists; agentic-trained heads best for agents |
| **ngram / prompt-lookup** | zero-asset self-speculation from context | none | edit-heavy agent loops (re-emitted file content) — near-total accept on hits, zero VRAM |
| **suffix decoding** (Snowflake) | cross-request suffix tree, adaptive drafts | `arctic-inference` pip pkg (**NOT installed**) | agent loops: 1.8–4.5× on SWE-bench agents; beats ngram at all concurrency |
| classic small draft | separate small model | same-tokenizer small model | only when nothing above exists for the target |

Medusa/Hydra/mlp_speculator: legacy, superseded — don't invest. All techniques here are
SM86-clean (avoid only FP8/NVFP4 *asset builds*: pick bf16 heads + int4/GGUF targets).

## Per-engine support (installed builds)

### llama.cpp — stable b9847, nightly b9869 — chainable `--spec-type`
`--spec-type` takes a comma list; **draftless implementations take precedence over draft-model
ones when chained** — the ideal agent config chains lookup + model drafting:
- Values: `none, draft-simple, draft-eagle3, draft-mtp, draft-dflash, ngram-simple, ngram-map-k,
  ngram-map-k4v, ngram-mod, ngram-cache`. **DFlash and EAGLE-3 are upstream now** (PR #22105 /
  #18039; Qwen3.5/3.6-hybrid EAGLE-3 fixes #24593/#24707 are in b9847 — never use older builds).
  ⚠ (was BUGS.md S1, now FIXED) the curated schema models `spec-type` as a **list-valued enum**
  (`"list": true`, `draft-dflash` included) and the validator splits the comma list and checks each
  element (`internal/service/validator/rules.go` `checkEnum`). So `"spec-type": "draft-mtp,ngram-mod"`
  chains **and** `"spec-type": "draft-dflash"` both validate directly in a profile's `args` — no
  workaround needed.
- **Ideal agent config for MTP GGUFs (Qwen3.6/Gemma-4):** chain `"spec-type": "draft-mtp,ngram-mod"`
  with `"spec-draft-n-max": 3` directly in `args` — draftless implementations run first, so ngram-mod
  (PR #19164: ~16 MB shared hash pool, fires on re-emitted content) precedes the MTP draft (novel
  tokens). **Caveat:** confirm the MTP head **and** the ngram drafter actually load from the launch
  log (`draft-mtp`/`ngram` lines) — the config field alone proves nothing. Plain
  `"spec-type": "draft-mtp"` is the fallback when no re-emission gain is expected.
  n-max 6 OOMs at 200k (+2.1 GiB); the MTP draft ctx defaults f16 KV — quantize via the canonical
  schema keys `cache-type-k-draft`/`cache-type-v-draft` set to `q8_0` directly in `args` (Type-4
  enums in the live llama.cpp schema; `spec-draft-type-k`/`-v` are accepted aliases that resolve via
  the validator's `Lookup`). extraArgs is only for genuinely schema-absent flags.
- ngram-mod knobs: `spec-ngram-mod-n-match 24 / n-min 48 / n-max 64` (`--spec-default` = same).
- EAGLE-3: `-md <head.gguf> --spec-type draft-eagle3 --spec-draft-n-max 8 --spec-draft-p-min 0.5`;
  convert with `convert_hf_to_gguf.py <head repo> --target-model-dir <target repo>`, or use
  pre-converted (`wimmmm/Ex0bit-Qwen3.6-27B-PRISM-EAGLE3-GGUF`).
- DFlash upstream: `-md <upstream-schema drafter> --spec-type draft-dflash --spec-draft-n-max 15`
  — flat chain only, no adaptive controller yet; **upstream-schema GGUF only**
  (Alittlehammmer/williamliao repos, or convert yourself). `draft-dflash` **is in the curated
  `spec-type` enum** (verified in `~/.config/model-loader/backends/schemas/llama.cpp-stable.json`)
  → it validates directly in `args`. beellama stays the featured DFlash server (below) for its
  adaptive `profit` controller, but the upstream flat path now configures cleanly here too.
- Dual-GPU: `--spec-draft-device CUDA1` (schema-absent → set via `extraArgs`) parks the draft/head
  off the weights card. `split-mode tensor` still crashes/hangs with any external draft — upstream
  #24309 (open: `-sm tensor` + nextn/draft head → `GGML_ASSERT tensor_axis_0 != nullptr`) and
  discussion #22473 (`-sm tensor` + spec silently stops generating, maintainer "WIP") are both open
  as of 2026-07-07 (see dual-gpu.md) — chain `ngram-mod` under tensor-split instead, or use layer
  split with `-md`.

### beellama main@85e22ea0 (build b10102-dirty) — featured DFlash + TurboQuant KV
`--spec-type dflash` (own dialect, ≠ upstream `draft-dflash`): flat + tree, adaptive `profit`
depth controller (leave on), accepts BOTH drafter GGUF schemas (Anbeeld/Lucebox `dflash-draft`
AND upstream). Key knobs: `spec-dflash-cross-ctx 1024` (default 512; 1024 better ≥100k),
`spec-branch-budget 0` (flat recommended; tree auto-disabled on multi-GPU targets),
`kv-unified true`. Drafter quant: **IQ4_XS/Q4_K_M — Q8 drafters measured net-negative.**
Vision: mmproj forces flat DFlash and disables all non-DFlash spec. Also has `ngram-*`, eagle3,
mtp, plus fork extras `copyspec` (purpose-built for re-emitted file content), `suffix`, `recycle`.

### vLLM 0.24.0 — `--speculative-config` JSON
Methods verified in the installed wheel: `ngram, ngram_gpu, suffix, medusa, mlp_speculator,
draft_model, eagle, eagle3, mtp, dflash` (per-arch aliases like `qwen3_5_mtp` are deprecated →
use `mtp`).
- **MTP (the rig's best Qwen3.6 config):** `{"method":"mtp","num_speculative_tokens":3}` — no
  `model` key; reuses the checkpoint's head, inherits target quant. Causal → **fp8_e5m2 KV works
  → full 256k** (profile `qwen3.6-27b-awq-mtp-fp8-vllm-tp2-256k`, ~91% accept measured).
- **DFlash:** `{"method":"dflash","model":"z-lab/<family>-DFlash","num_speculative_tokens":15,
  "attention_backend":"FLASH_ATTN"}`. Draft is non-causal; on SM86 FLASH_ATTN=FA2 which rejects
  fp8 KV (gated FA3+SM90 in source) → **DFlash profiles cap at auto-KV/128k**. Keep the drafter
  BF16 (quantized DFlash drafters are impossible in this build — dense-weight reads in
  qwen3_dflash.py). z-lab's newest interleaved-SWA 3.6 drafter may need a newer nightly.
- **ngram:** `{"method":"ngram","num_speculative_tokens":3,"prompt_lookup_max":10,
  "prompt_lookup_min":8}` — **`prompt_lookup_min` ≥8 is MANDATORY on tool-calling profiles**
  (issue #40875: default min=2 corrupts ~50% of Qwen3-class tool calls). `ngram_gpu` = same
  params, GPU proposer for long contexts.
- **suffix:** `{"method":"suffix","num_speculative_tokens":32,"suffix_decoding_max_tree_depth":24}`
  — requires `uv pip install arctic-inference` in the venv first (**not installed**; CPU-only
  lib, SM-agnostic). The most promising untested option for Claude-Code-style traffic.
- **EAGLE-3:** `{"method":"eagle3","model":"Ex0bit/Qwen3.6-27B-PRISM-EAGLE3",
  "num_speculative_tokens":5}` — speculators-format repos load directly.
- **Cold-start rule:** first request after load runs ~half speed (torch.compile/graphs) — always
  warm-measure.

### SGLang 0.5.9 (stable) + sglang-dflash (isolated build)
`--speculative-algorithm {EAGLE, EAGLE3, NEXTN, STANDALONE, NGRAM}` in 0.5.9 — **no DFLASH in
the stock wheel**; the rig's `sglang-dflash` backend (PR #23000 build) adds DFLASH
(`--speculative-dflash-block-size`, no dp-attention, pp=1). NEXTN = native MTP (works on 3090
via `--attention-backend triton`, measured ~107–119 tok/s Qwen3.6-27B AWQ-MTP tp2). NGRAM
forces overlap-schedule off (batch cost) — raise `--speculative-ngram-min-match-window-size`
to ~4–8 for tool-calling (same corruption class as vLLM #40875). EAGLE3 via
`--speculative-draft-model-path` (+`SGLANG_ENABLE_SPEC_V2=1` for the Ex0bit head instructions).

### TabbyAPI / exllamav3 0.0.43
`draft-mode {model|mtp|ngram|disabled}` (+ `draft-model-name`, `draft-num-tokens`,
`ngram_match_min` 2). MTP via bundled head file in the model dir (verify `mtp.*` tensors or the
sidecar exists); DFlash-exl3 drafters auto-detected via `draft-mode model` (turboderp repos;
4.00 bpw drafter ≈ best). **TP coexists with MTP and DFlash here** (measured +25…32% on dense
27B) — the only engine where tensor-parallel + external draft combine.

### lucebox dflash_server
The dedicated DFlash runtime (DDTree, PFlash, KVFlash, Spark) — see references/dflash.md.

### ik-llama-cpp (ikawrakow fork) — free-string `spec-type`, ik dialect
`spec-type` is a **plain string** in the curated schema (no enum) — any payload validates, so a
typo will not fail `validate`; confirm the head/drafter loads in the launch log. Dialect is bare
(like beellama, NOT upstream `draft-mtp`): `none, draft, dflash, mtp, ngram-cache, ngram-simple,
ngram-map-k, ngram-map-k4v, ngram-mod, suffix`, payload `TYPE:k=v,...`; `--spec-autotune` autotunes.
Draft: `-md` model, `-cd` ctx, `-ngld` layers, `cache-type-k-draft`/`-v-draft` KV. See
references/ik-llama.md.

## DSpark — real, but not yet usable here (state 2026-07-02)

DSpark = DeepSeek's "Confidence-Scheduled Speculative Decoding with Semi-Autoregressive
Generation" (repo `deepseek-ai/DeepSpec`, 2026-06-27; bundles DSpark/DFlash/Eagle3 training).
Architecture = DFlash backbone + low-rank **Markov head** (each block position conditions on the
actually-sampled predecessor → fixes suffix acceptance decay) + **confidence head** (verification
truncation; no OSS engine uses it yet). Numbers: llama.cpp PR bench 1.88× overall / **2.12×
coding** on Qwen3-8B, beats DFlash on all 11 categories (+21%); vLLM PR: accept 3.42 vs 2.60 MTP,
coding 3.90 (1.42×).

- **Nothing installed runs it:** vLLM merged to main 2026-07-01 — AFTER 0.24.0 (2026-06-29);
  llama.cpp PR #25173 still open (arch `dspark`, spec-type `draft-dspark`; verified open 2026-07-07,
  last push 2026-07-04) — **not in b9847 stable nor b9869 nightly** (grepped the nightly checkout:
  no `dspark` arch, no `draft-dspark`); SGLang PR #29538 is DeepSeek-V4-only; beellama/lucebox/tabby: nothing.
- **Drafters exist ONLY for Qwen3-4B/8B/14B + Gemma4-12B** (`deepseek-ai/dspark_qwen3_*_block7`),
  none for Qwen3.6-27B (the `Hikari07jp/DSpark-Qwen3.6-27B-AEON-draft` repo is an unvetted
  DFlash+Markov hybrid for AEON finetunes, 0 downloads — skip).
- **How to adopt when it lands** (re-check before building profiles):
  1. vLLM: bump `vllm-nightly` venv past commit f5a8d73 → dense Qwen3 target +
     `'{"method":"dspark","model":"deepseek-ai/dspark_qwen3_8b_block7","num_speculative_tokens":7,
     "attention_backend":"FLASH_ATTN","draft_sample_method":"probabilistic"}'`.
  2. llama.cpp: watch PR #25173 for merge (open as of 2026-07-07) → nightly rebuild picks up `--spec-type draft-dspark`.
  3. No long-context (>4k) acceptance data published anywhere yet — calibrate before trusting.
- Gemma4 drafter unsupported in the vLLM path (arch deferred); Lucebox is training DSpark-style
  Markov heads for its drafters (HF dataset 2026-06-29) — watch lucebox-hub releases.

## Asset inventory by family (HF, 2026-07-02 — re-search before assuming)

| Family | MTP | DFlash drafter | EAGLE-3 head | DSpark |
|---|---|---|---|---|
| Qwen3.6-27B | unsloth `*-MTP-GGUF`, AWQ-MTP checkpoints, turboderp exl3 head | z-lab (under-training, AL~5), Anbeeld/Lucebox/spiritbuun GGUF, Alittlehammmer upstream-GGUF, turboderp exl3 | **Ex0bit PRISM-EAGLE3** (agentic-trained, τ2.2) + wimmmm GGUF | — |
| Qwen3.6-35B-A3B | unsloth MTP-GGUF | z-lab (most-downloaded), Anbeeld GGUF, UnstableLlama exl3, AEON-7 (Ornith-matched) | Dogacel community | — |
| Gemma-4 (12B/26B/31B) | native (llama.cpp #23398, vLLM gemma4→mtp) | z-lab all three sizes; Anbeeld GGUF (26B/31B); turboderp exl3 31B | RedHatAI speculators (26B/31B) | official drafter 12B (no engine here yet) |
| Qwen3-Coder / Coder-Next | — | z-lab both | thoughtworks Coder-Next (+GGUF) | — |
| gpt-oss 20b/120b | — | z-lab both | RedHatAI, lmsys, nvidia long-context | — |
| Qwen3 4B/8B/14B/32B | — | z-lab 4B/8B | AngelSlim (all sizes), RedHatAI, Tengyunw | **deepseek-ai block7 (4B/8B/14B)** |
| Llama-3.3-70B, Qwen3-32B dense, GLM-4.7, DeepSeek | — (70B: nothing) | **none exist** | yuhuili/nvidia (Llama), AngelSlim (Qwen3-32B) | — |

Rules of thumb (measured on this rig): **MTP wins on MoE** (cheap verify, survives finetunes),
**DFlash wins on dense** targets with a matched drafter; mismatched/cross-generation drafters
collapse (30% vs 65% accept); finetunes drop DFlash acceptance unless a matched drafter exists
(AEON-7) — prefer native MTP on finetunes. Confirm heads actually load: launch log `draft-mtp`/
`dflash` lines (llama.cpp — **sparse at default verbosity; an absent line ≠ spec off**, raise
`--log-verbosity`/`lv` or read a response's server `timings.draft_n_accepted` > 0 to confirm),
`mtp.*` tensor count (exl3); never the config field alone.

## Sampling × speculation

Acceptance falls as sampling entropy rises — UNLESS the entropy is "calibrated" back on-distribution.
On **llama.cpp/beellama, `--top-n-sigma 1.0` does exactly that**: measured 2026-07-02 on Ornith AEON
35B-A3B (MTP), temp 1.0 + top-n-sigma 1.0 gave acceptance **0.62 vs 0.63 at temp 0.6** — flat, no
regression. So the old rule ("speculative profiles use the LOW end, Qwen3.6+MTP → 0.6") is now:
**run the model's calibrated temp (1.0 for 2026-gen agentic-coding models) + `top-n-sigma 1.0`** on
llama.cpp/beellama; you get the calibrated operating point at zero speed cost. Reserve temp 0.6 for
single-shot code completion, or for engines WITHOUT top-n-sigma (vLLM/SGLang/dflash) where the
entropy-vs-acceptance trade-off still bites. Greedy is bit-exact and highest-AL but banned for
agents (loop risk — model-research.md). Verify paths: llama.cpp exact; lucebox tree verify stays
argmax (honors sampling only on the committed token); beellama has real rejection sampling only
when draft+target temps >0.

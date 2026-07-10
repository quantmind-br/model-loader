# tabby (TabbyAPI · ExLlamaV2 0.3.2 / ExLlamaV3 0.0.43 · EXL2/EXL3) on the dual-3090 rig

Catalog id **`tabby`** (registered; wrapper `backends/tabby/tabby-serve.sh` forces
`--host 127.0.0.1 --disable-auth true`; bools take a value: `--vision true`). One backend serves
both formats — TabbyAPI auto-detects the engine from `quantization_config.quant_method`
(`exl2`→exllamav2, `exl3`→exllamav3). **EXL2/GPTQ are deprecated upstream (planned removal) —
build new profiles on EXL3 only.** The proxy kill/relaunches to swap; `inline_model_loading`
stays off.

## Format reality (state 2026-07)

- **EXL2 frozen** (0.3.2, Jul 2025) — no qwen3_5_moe/newer archs. EXL3 is the only exllama
  option for Qwen3.5/3.6.
- **EXL3 wins quality-per-bit, not Ampere speed.** The "still needs work on Ampere" README
  caveat stands at 0.0.43 (issue #144 open; the 0.0.32 autotune helped 3090 least). Measured
  here: MoE 35B-A3B EXL3 ~109 tok/s vs GGUF+MTP ~147; dense 27B EXL3 TP+MTP ~82 tokenizer-exact
  vs vLLM AWQ+MTP ~106. Pick tabby/EXL3 for: best quality at a given VRAM (esp. 2.5–4 bpw),
  dense-model TP across both cards (+25…32% AND coexists with drafts — unique), models that
  only ship as EXL3, or KV-cache bit flexibility.
- **bpw for 24 GiB:** dense 24–31B at 3.0–4.0 bpw h6 (27B @3.30 ≈ 11.6 GiB → ~10 GiB for KV);
  MoE 35B-A3B @4.0. **For 48 GiB:** dense 27B at 5–6+ bpw with TP (quality play), 70B-class
  ~4.0 bpw, GLM-4.5-Air-class ~3.0 (autosplit, NOT TP for MoE). KLD reference (GLM-4.7):
  3bpw 0.276 → 4bpw 0.137 → 5bpw 0.109 — steep gains to 4, diminishing after 5.
- **turboderp publishes bpw variants as git BRANCHES** — `hf download <repo> --revision 4.00bpw`;
  cloning `main` gets no weights.

## Profile shape

- **`model` = the EXL3 model DIRECTORY** (absolute); the arg builder splits into
  `--model-dir <parent>` + `--model-name <basename>`.
- Pin/placement via `launch.env` `CUDA_VISIBLE_DEVICES` like every backend.
- Key args (schema, 24 flags): `max-seq-len`, `cache-size`, **`cache-mode`** (EXL3 = `"k,v"`
  bit pair 2–8 each), `tensor-parallel`, `tensor-parallel-backend` (**`native` on this rig** —
  CPU-assisted all-reduce, no P2P needed; `nccl` is for NVLink), `gpu-split` (GB/card, e.g.
  `21,23` — GPU0 desktop gets less), `gpu-split-auto` + `autosplit-reserve`, `vision`,
  `reasoning`, `backend`, `draft-mode`/`draft-model-name`/`draft-num-tokens`/`draft-cache-mode`/
  `draft-gpu-split`, **`tool-format`**, `prompt-template`, `chunk-size`. `port` manager-owned.
- **`tool-format` is MANDATORY for agent profiles** — blank = tool calls returned raw in
  `content`, never parsed, failures silent (log-only). Values: `qwen3_coder` (alias `qwen3_5` —
  use for Qwen3.5/3.6), `glm4_5/4_6/4_7`, `minimax_m2`, `mistral`, `gemma4`. **No Hermes-JSON
  parser exists** — classic Qwen2.5/Qwen3-Instruct JSON tool format cannot be parsed by tabby.
- Server-side sampling presets: `sampler_overrides/*.yml` — the only backend with first-class
  default-sampler presets that survive any client (put the family preset there).
- KV `cache-mode` guidance for coding agents: **`8,8`** ≈ lossless half-size (default choice);
  `6,5` = max-context sweet spot (k_bits ≥ v_bits always — K is noise-sensitive);
  **`4,4` degrades code/tool-calling — avoid.** TabbyAPI default is FP16: always set it.

## Speculative (draft-mode)

- **`mtp`** — loads the `mtp` component from the SAME model dir. Quants converted with
  exllamav3 **≥0.0.41 embed quantized MTP layers automatically**; older quants need the add-on
  head file dropped into the dir (`turboderp/Qwen3.6-27B-MTP-exl3` = one 203 MiB safetensors).
  Verify: `python3 -c "import json;d=json.load(open('<dir>/model.safetensors.index.json'));print(sum('mtp' in k for k in d['weight_map']))"`
  — 0 and no sidecar → `Required tensor mtp.* not found`. MTP is **Qwen3.5/3.6-only** at 0.0.43
  (no GLM NextN, MiMo ignored). Community repos tag `nonmtp` — read tags.
- **`model`** — external drafter; how DFlash-exl3 loads (`turboderp/Qwen3.6-27B-DFlash-exl3`,
  4.00 bpw best; `draft-model-dir <parent>` via extraArgs if needed; auto-detected arch).
  `draft-num-tokens 8` caps VRAM vs DFlash's default 15. `draft-gpu-split` can pin the drafter
  to one card (lucebox draft-split pattern).
- **`ngram`** — zero-asset (`ngram_match_min` 2). Cheap win for edit-heavy agent loops.
- **Version floor: exllamav3 0.0.43 exactly or newer for TP+spec** (MTP-in-TP fixed 0.0.43,
  DFlash-in-TP 0.0.38) — older pins crash in precisely the configs this rig wants.

## Measured on this rig (2026-06)

| Config | Placement | ctx | tok/s | Note |
|---|---|---|---|---|
| Qwen3.6-27B 3.30bpw MTP | single GPU1 | 256k | ~60 | 20.4 GiB |
| same, **TP+MTP (native, CVD=0,1)** | both cards | 256k | **~77–82 (+28%)** | the kept profile pattern |
| same, TP+DFlash | both cards | 128k | ~74 (+32%) | drafts coexist with TP — unique to exllama |
| 35B-A3B 4bpw MoE | single GPU1 | 32k | ~109 | TP on this MoE: **−20%** (87) — never TP an A3B MoE |
| 2 models, 1/card | pinned | — | ~79 each | capacity play (2nd via own `serve` port) |

**TP verdict is model-dependent: dense → +25…32%; A3B-MoE → slower. Measure, don't assume.**
Gemma4: **no TP support** in exllamav3 (README) — single-GPU/autosplit only.
Long-context caveat: issue #75 (3 tok/s @112k on a 3090, cache 4,4) is open — re-validate decode
at real >100k fill (and use cache ≥6,5) before committing agent sessions.

## Tuning by model type (Ampere/SM86)

Knob semantics from `backends/tabby/config_sample.yml` and the installed `exllamav3`
0.0.43 source (`backends/tabby/.venv/.../exllamav3/`); TP deltas are the Measured table
above. Every knob below is a first-class schema flag (24-flag schema, `max-batch-size`
included) — set it in `args`, not extraArgs.

- **Dense (Qwen3.6-27B, Mistral/Ministral).**
  - bpw 3.0–4.0 h6 to fit one 24 GiB card at long ctx; 5–6 bpw only on the 48 GiB TP
    tier — KLD gains flatten past 4 (Format reality).
  - **TP is the dense payoff**: `tensor-parallel: true`, `tensor-parallel-backend: native`,
    symmetric `gpu-split 21,23` → +25…32% *and* drafts still load (measured). `native` =
    CPU shared-memory all-reduce over PCIe, no P2P/NVLink (`model/model_tp_backend.py`
    `TPBackendNative`; config: "native for PCIe, nccl for NVLink"). Never `nccl` here.
  - `cache-mode 8,8` for agents / `6,5` to buy ctx (k_bits ≥ v_bits — K is noise-sensitive;
    `cache/quant.py` asserts each in 2–8); `4,4` degrades code — avoid.
  - `chunk-size` 2048 default: raise toward 4096 with free VRAM for faster long-prefill,
    drop to 512–1024 to shave prefill VRAM (config: ideal 512–4096, lower = less VRAM /
    slower ingest).
- **MoE, A3B-active (35B-A3B, GLM-4.5-Air).**
  - **NEVER TP an A3B MoE — measured −20% (109→87 tok/s)**: few active params → the
    all-reduce/sync dominates. Fit @4.0 bpw on GPU1 single-card, or `gpu-split-auto`
    autosplit across both cards for a larger MoE — split, never TP.
  - GLM-4.5-Air-class ~3.0 bpw autosplit on the 48 GiB tier. Decode is memory-bound, so
    `cache-mode 8,8` costs almost no speed here.
- **VL (Qwen3-VL/-VL-MoE, GLM-4.V, Gemma3/4-vision, Mistral3).**
  - Vision tower loads *inside the quant* with `vision: true` — no sidecar mmproj. Archs
    present at 0.0.43: `qwen2_5_vl`, `qwen3_vl`, `qwen3_vl_moe`, `glm4v(_moe)`, `gemma3/4`,
    `mistral3` (`architecture/` listing).
  - **Run a VL model text-only by leaving `vision` at default `false`** — the tower isn't
    loaded, reclaiming its VRAM (exllama's `--language-model-only` equivalent).
  - Image token bursts spike prefill: keep `chunk-size` ≤2048 (1024 if tight) so one
    image's token block doesn't OOM the prefill.
  - `gemma4` has **no TP** (README) → single-GPU/autosplit even when dense.
- **Coding / reasoning (agent drivers).**
  - `tool-format` MANDATORY (Profile shape) — no Hermes-JSON parser; pick a model in the
    format list. `cache-mode 8,8` (never `4,4`). Put the family sampler in server-side
    `sampler_overrides/*.yml` so it survives any client.
  - `reasoning: true` splits `reasoning_content`; add `force_enable_thinking: true` for
    clients that omit the kwarg (config).
  - Speculative `mtp` (Qwen3.5/3.6) or `ngram` (zero-asset, edit loops) both coexist with
    dense TP (Speculative). `max-batch-size` defaults 32 (transformer) / 4 (recurrent,
    higher VRAM); for batch-1 agent sessions set `1–2` to reclaim cache VRAM (config).

## Template/agent config

`prompt_template` defaults to the model dir's tokenizer template (verify per model-research.md);
`reasoning: true` + `reasoning_start_token`/`end_token` (default `<think>`/`</think>`) splits
`reasoning_content`; `force_enable_thinking: true` for clients that don't send the kwarg.
Vision works (`vision: true` + quant with vision tower) despite absent from the README list.
Prefix reuse is automatic (hash-based page dedup, 256-token pages) — nothing to configure.

## Gotchas

| Symptom | Cause | Fix |
|---|---|---|
| `unsupported backend kind: tabby` at launch | proxy/TUI running an OLD binary | `make install`, restart |
| `--X: expected one argument` | TabbyAPI bool with no value | emit `--X true` (builder does; extraArgs must too) |
| `Required tensor mtp.* not found` | quant pre-0.0.41 without head add-on | drop the `*-mtp-exl3.safetensors` in the dir, or re-quant |
| tool calls appear as text in content | `tool-format` unset (or unsupported family) | set it; no Hermes-JSON support — pick models in the format list |
| TP slower | A3B-MoE (light active params → sync dominates) | TP only dense; MoE = single card or autosplit |
| downloaded repo has no weights | turboderp branch convention | `--revision <bpw>bpw` |
| model param 404 | request `model` ≠ loaded dir name | proxy swaps by profile id; kill/relaunch swap |

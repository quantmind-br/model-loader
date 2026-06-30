# tabby (TabbyAPI · ExLlamaV2/V3 · EXL2/EXL3) on dual RTX 3090

TabbyAPI is the OpenAI server for both ExLlamaV2 (EXL2) and ExLlamaV3 (EXL3). One `tabby`
backend serves both formats — TabbyAPI auto-detects the engine from `config.json`
`quantization_config.quant_method` (`exl2`→exllamav2, `exl3`→exllamav3). The model-loader
wrapper launches `main.py` headless + loopback + no-auth; the proxy kill/relaunches to swap.

## Format reality (read first)

- **EXL2 is frozen** (exllamav2 v0.3.2, Jul 2025). It does **not** support newer architectures.
  In particular **there is no EXL2 of `qwen3_5_moe`** (Qwen3.5/3.6 hybrid linear-attention MoE) —
  the architecture postdates the freeze. For that family, EXL3 is the only exllama option.
- **EXL3 (exllamav3, active, beta) wins on quality-per-bit**, not raw speed on Ampere: its kernels
  are **not yet SM86-optimized** ("memory-bound, needs Ampere optimization" per upstream). On the
  3090, EXL3 of an MoE is typically **slower in tok/s than the same model as GGUF+MTP on llama.cpp**.
  Pick tabby/EXL3 for: best quality at a given VRAM (esp. low bpw 2.5–4), a dense model split across
  both cards via real TP (llama.cpp is pipeline-only), or a model that only ships as EXL2/EXL3.
- **bpw**: EXL2/EXL3 name = average bits/weight. 24 GiB sweet spot ≈ 4.0–4.65 bpw for 30–70B,
  6.0/8.0 for ≤14B. head_bits (`-hb`) is separate (6–8 typical).

## Setup (one-time, vendored, gitignored)

```bash
git clone https://github.com/theroyallab/tabbyAPI backends/tabby
backends/tabby/setup.sh          # uv venv (py3.12) + pip install -e .[cu12]  → PREBUILT wheels
```
`cu12` pins torch 2.9.0+cu128 and the official exllamav2 0.3.2 / exllamav3 0.0.43 cu128 wheels —
**no CUDA compile**; they run on SM86 under the newer driver via forward-compat. (`TABBY_EXTRA=cu13`
/ `TABBY_PYVER=3.13` to override.) Register the backend:
```bash
model-loader backend add tabby --executable <repo>/backends/tabby/tabby-serve.sh --kind tabby
model-loader backend schema refresh tabby    # 24 curated flags
```

## Profile shape

- **`model` = the EXL2/EXL3 model DIRECTORY** (absolute). The arg builder splits it into TabbyAPI's
  `--model-dir <parent>` + `--model-name <basename>`. (Unlike llama.cpp's single-file GGUF path.)
- Pin/placement via `launch.env` `CUDA_VISIBLE_DEVICES` exactly like the other backends.
- The wrapper forces `--host 127.0.0.1 --disable-auth true`. **TabbyAPI booleans take an explicit
  value** (`--vision true`, not `--vision`) — the arg builder emits `--flag true|false` for you.
- Key args (schema longs): `max-seq-len`, `cache-size`, **`cache-mode`**, `tensor-parallel`,
  `tensor-parallel-backend` (`native` for PCIe / no-NVLink; `nccl` only with NVLink), `gpu-split`
  (GB/card, e.g. `21,23`), `gpu-split-auto`, `vision`, `reasoning`, `backend` (force exl2/exl3),
  `draft-mode`/`draft-model-name`. `port` is manager-owned (omit).
- **`cache-mode` format differs by engine**: EXL2 → `FP16|Q8|Q6|Q4`; **EXL3 → a `k,v` bit pair**
  each 2–8 (e.g. `8,8` ≈ Q8, `6,6`, `4,4`). Q4/Q6 KV is ~free on speed, buys long context.

## MTP / speculative — verify the tensors exist

`draft-mode mtp` needs the quant to actually **ship the MTP head tensors** (`mtp.*` in the
safetensors). Many EXL3 quants **drop them** even when `config.json` still declares
`mtp_num_hidden_layers`. Confirm before relying on it:
```bash
python3 -c "import json;d=json.load(open('<model>/model.safetensors.index.json'));print(sum('mtp' in k for k in d['weight_map']))"
```
`0` → MTP unavailable on that quant (TabbyAPI aborts with `Required tensor mtp.* not found`). To get
MTP on EXL3, re-quantize a base that has the head (`convert.py`), or use GGUF+MTP on llama.cpp.

## Measured — Qwen3.6-35B-A3B EXL3 4bpw, 32k ctx, cache Q8 (2026-06-28, single distill quant 19 GiB)

| Scenario | Placement | tok/s (decode, batch-1) | Notes |
|---|---|---|---|
| **1 — single card** | GPU1, `tensor-parallel false` | **~109** | 18 GiB VRAM. Default. |
| **2 — TP both cards** | `tensor-parallel true` + `native`, `CUDA_VISIBLE_DEVICES=0,1` | **~87 (−20%)** | ~9.6 GiB/card; **slower** — TP sync is CPU-bound and this MoE fits one card. |
| **3 — two models, 1/card** | 2 instances, `CUDA_VISIBLE_DEVICES=0` and `=1` | **~79 each, in parallel** | capacity play; proxy is single-active so run the 2nd as its own server. |

**Takeaway (MoE-A3B):** exllama native TP is **slower for an A3B MoE** — only ~3B params are active,
so the model is already light on one card and the TP sync (CPU/launch-bound over PCIe) dominates. For
a **dense** model the verdict flips (see the 27B section below). MTP wasn't exported on this distill
quant. For pure tok/s on this MoE, GGUF+MTP on llama-cpp-default (~147) still wins; EXL3 is the
quality-per-bit play.

## Measured — Qwen3.6-27B EXL3 (DENSE) 3.30bpw, cache 6,6, greedy (2026-06-29)

Base `turboderp/Qwen3.6-27B-exl3_3.30bpw` (the ONLY bpw turboderp published; 14.5 GiB, qwen3_5 hybrid
attn — 16 full + 48 linear of 64 layers → cheap KV, vision off). MTP works here via the **bundled
`qwen3.6-27b-mtp-exl3.safetensors` head in the model dir** (`draft-mode mtp`; the base quant is exl3
v0.0.32 so its index has 0 `mtp.*` tensors — the separate head file supplies them; log: "Using main
model MTP component for drafting"). DFlash via the EXL3 drafter `turboderp/Qwen3.6-27B-DFlash-exl3`
@4.00bpw (`draft-mode model` + `draft-model-name` + `--draft-model-dir <parent>` in extraArgs; log:
"Using draft model: …DFlash-exl3"). tok/s = streamed decode of a 400-tok greedy reply.

| Config | Placement | ctx | tok/s @400 | VRAM |
|---|---|---|---|---|
| MTP | single GPU1 | 256k | **~60** | GPU1 20.4 GiB |
| DFlash | single GPU1 | 128k | ~56 | GPU1 18.7 GiB |
| **TP + MTP** | both cards, `native`, CVD=0,1 | 256k | **~77 (+28%)** | GPU0 12.2 + GPU1 9.6 |
| TP + DFlash | both cards, `native`, CVD=0,1 | 128k | ~74 (+32%) | GPU0 12.5 + GPU1 8.2 |

**Takeaways (dense 27B):**
- **exllama native TP DOES speed up a dense model here: +25…32%** (splits the full 27B weights across
  both cards' VRAM bandwidth per token). Opposite of the A3B-MoE result above → the TP verdict is
  **model-dependent: dense → faster, A3B-MoE → slower. Measure, don't assume.**
- **TP coexists with BOTH MTP and DFlash** — both load and run under `tensor-parallel true` with no
  crash (unlike llama.cpp `split-mode tensor`, which `ggml_abort`s with an external draft and can
  crash on internal MTP). TP output stayed coherent (long PT-BR generation).
- MTP ≈ DFlash here; MTP edges it on reasoning content and reaches full 256k with no drafter VRAM, so
  **TP + MTP @256k is the pick (~77 tok/s).** DFlash's default 15-token draft didn't beat MTP's 2.
- Numbers above are streamed-chunk; EXL3 streams **1 token/chunk** (~3.6 chars/chunk) so they ≈
  tokenizer-exact (TP+MTP is **~82 tok/s** tokenizer-exact). Head-to-head, the vLLM
  `qwen3.6-27b-awq-mtp-fp8-vllm-tp2-256k` profile hit **~106 tok/s** (tokenizer-exact, same prompt)
  — **~29% faster single-stream** AND keeps vision + ~1.94x concurrency. So on this rig EXL3 is the
  quality-per-bit + works-with-TP play, **not** the raw-speed king. (Beware: vLLM/MTP streams
  multiple tokens per SSE chunk — counting chunks undercounts it ~2.85x; verify with `usage` or a
  tokenizer.)

## Gotchas

| Symptom | Cause | Fix |
|---|---|---|
| `unsupported backend kind: tabby` at launch | proxy/TUI running an OLD binary | `make install`, restart proxy/TUI |
| `main.py: error: argument --X: expected one argument` | TabbyAPI bool flag with no value | emit `--X true` (builder does this; in extraArgs pass a value) |
| `Required tensor mtp.* not found` | quant lacks MTP head | drop `draft-mode mtp`; see MTP section |
| TP not faster (or slower) | **model-dependent**: A3B-MoE is already light → TP sync dominates (−20%); a DENSE model splits weights across both cards' bandwidth → **+25…32%** | Measure per model: dense → try TP; A3B-MoE → single card |
| "TP can't run with a draft" | exllama TP coexists with MTP **and** DFlash (both loaded fine, no crash) — unlike llama.cpp `split-mode tensor` | Combine freely; validate a long generation once |
| model param 404 / ignored | request `model` ≠ loaded dir name | proxy swaps by profile id; inline loading is off (kill/relaunch swap) |

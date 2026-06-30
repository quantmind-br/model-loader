---
name: rtx3090-inference-profiles
description: 'Use when creating, tuning, or fixing a model-loader profile/preset/launch config for local LLM inference on the dual RTX 3090 workstation (two 24 GiB cards = 48 GiB, NO NVLink) — any backend kind (llama-server, beellama-cpp, buun-llama-cpp, vllm, sglang, dflash, unsloth, tabby). Triggers: user wants to run a model (GGUF, safetensors, AWQ, GPTQ, EXL2, EXL3) at a target context window; a model too big for one 24 GiB card; split a model across both GPUs (tensor-parallel, pipeline-parallel, --tensor-split, --split-mode, --main-gpu); pin a model to a specific GPU or run two models at once (CUDA_VISIBLE_DEVICES); wants more context, more tok/s, or better quality; hits OOM/"CUDA out of memory"; asks which KV cache type, quant, or kv-cache-dtype fits in VRAM; mentions DFlash, MTP, speculative decoding, gpu-memory-utilization, max-model-len, tensor-parallel-size, EXL2/EXL3/exllama/exllamav2/exllamav3/TabbyAPI/bpw, or NVLink — even if they never say "profile".'
---

# RTX 3090 ×2 Inference Profile Builder

Build a validated schemaVersion-3 profile in `~/.config/model-loader/profiles/<id>.json` that
launches a model through the best backend for it, maximizing quality on a **dual RTX 3090 rig
(2 × 24 GiB = 48 GiB, NVLink INACTIVE, PCIe PHB; 16 CPU cores)**. Done = profile passes
validate, launches, and has measured numbers recorded in its description.

## The rig (read once)

- **GPU1 is the clean card (~23.2 GiB usable)** — no display. Default single-GPU serving target.
- **GPU0 drives the desktop** (~0.6–1 GiB) → **~22.4 GiB usable**.
- **No NVLink, no GeForce P2P over PCIe** (driver-disabled — inter-GPU collectives bounce through
  host RAM at PCIe speed). `nvidia-smi topo -m` = PHB.
- **VRAM cap = 46 GiB total** (user policy): plan **≤23 GiB/card**; ~46 GiB pooled when layer-split.

**The value of the second card depends on the split mode** — measured on this rig (batch-1 decode,
≤31B dense, quantized KV, llama.cpp v9829): **`split-mode tensor` is the FASTEST at short context,
+22…+39% vs single-GPU** (both cards' VRAM bandwidth in parallel per token), **but the speed gap
shrinks and can reverse as context grows** (Qwythos-9B Q8_0 MTP calibrated 2026-06-30: +22% at
256k, −11% at 768k — KV cache traffic between GPUs overtakes the parallelism benefit). At the
extreme, tensor split **enables context lengths impossible on single-GPU** (Qwythos-9B Q8_0: 1M at
157.9 tok/s — single-GPU OOMs at startup). **`split-mode layer` +4…+7%** (sequential pipeline —
thermal relief only), single-GPU is the baseline, and **`split-mode row` is −2.8×** (deprecated —
never use it). So tensor-split **does** buy single-stream speed here, contrary to the old "capacity
only" rule — **but** it crashes with an external speculative draft (DFlash / external MTP gguf),
forces CPU sampling, needs `n-gpu-layers` set explicitly, and runs both cards at ~350 W. Default
to single-GPU for determinism/VRAM/power; reach for `split-mode tensor` to maximize speed on a
fitting ≤31B dense model with no external draft, especially at short context; use `split-mode
layer` when you need a draft, MoE, >24 GiB pooling, or max compatibility — or when single-GPU
already wins (≥512k context). The *why*, all per-backend split flags, the pin-per-GPU
recipe, the 48 GiB model tier, and power/thermal live in **references/dual-gpu.md**.

## Placement decision — do this FIRST (before picking a backend)

Decide WHERE the model runs, then the backend, then read the file(s).

1. **Weights + target-context KV fit in ≤23 GiB?** → **SINGLE-GPU, pinned to GPU1** (the default
   execution path for most models — hybrid-attn MoE, ≤32B dense, anything quantized that fits).
   Put in `launch.env`: `CUDA_DEVICE_ORDER=PCI_BUS_ID` and `CUDA_VISIBLE_DEVICES=1`. No split flags.
   (Want more single-stream speed and have no external draft? `split-mode tensor` across both cards
   measured **+22…+39%** here on ≤31B dense at short context — gap shrinks as ctx grows, may reverse
   past ~512k. Costs both cards' power + breaks external drafts; see dual-gpu.md.)
2. **Two models wanted live at the same time?** → **PIN ONE PER GPU.** Heavy/primary on GPU1
   (`CUDA_VISIBLE_DEVICES=1`), secondary on GPU0 (`=0`, budget the desktop). Zero NCCL/split.
   → references/dual-gpu.md §Pin-per-GPU (also covers the single-active-proxy reality).
3. **Weights + KV exceed 24 GiB but fit in ~46 GiB?** → **MULTI-GPU split — a CAPACITY decision.**
   Prefer pipeline/layer split (llama `--split-mode layer`, vLLM `--pipeline-parallel-size 2`).
   Reserve **vLLM** tensor-parallel for MoE (`--enable-expert-parallel`) or prefill-heavy jobs.
   (llama.cpp `split-mode tensor` is a separate, faster path for a *fitting* ≤31B GGUF — see §The rig;
   untested >31B, so for 70B-class pooling stay on `--split-mode layer`.)
   → references/dual-gpu.md §Per-backend, then the backend file.
4. **Bigger than ~46 GiB?** → do NOT generate a profile; recommend a smaller model / lower quant
   (keep the no-`--n-cpu-moe` policy). 235B / 8×22B class are out.

User asks to "use both GPUs to go faster" on a model that fits one card? On llama.cpp, **`split-mode
tensor` delivers +22…+39% at short context** (measure and record it — Qwythos-9B Q8_0: +22% at 256k)
but the speed gap shrinks as context grows and **reverses past ~512k** (−11% at 768k — single-GPU
wins). So tensor-split is a **speed win at short ctx, capacity win at long ctx** (1M impossible on
single-GPU). But warn about its costs: it **crashes with an external speculative draft** (DFlash /
external-MTP gguf — `ggml_abort` in the draft graph split), forces CPU sampling, needs
`n-gpu-layers` set explicitly, and runs both cards hot (~350 W). **`split-mode row` is the trap —
−2.8×, never use it.** `split-mode layer` is the safe middle (+4…+7%, compatible with
drafts/MoE). Validate a real generation either way (no #20052) — it passed for tensor + layer on
9B/27B/31B incl. non-English.

## Hard rules (read before anything)

- **NEVER run backend binaries by hand** — only `model-loader instance start <id>`. All inference
  goes through a proxy (default `127.0.0.1:4321`); the OpenAI `model` param = profile ID.
- **NEVER set `"port"` in args** — reserved, silently stripped; the manager assigns ephemeral ports.
- **NEVER invent flags.** Read the live schema first:
  `~/.config/model-loader/backends/schemas/<backend-id>.json` (`flags` map = validation source of
  truth). Unknown args = hard ERROR. A flag the binary genuinely supports but the schema lacks goes
  in `extraArgs` (warning only) — or refresh via the backend-schema-update skill. Never hand-edit
  schema JSON.
- **Schema enum presence ≠ hardware support.** The schemas describe what the *binary* accepts, not
  what SM86 silicon runs. fp8/mxfp8/mxfp4 compute paths need SM89+. Check the Ampere matrix in the
  reference file before accepting any quantization/kv-dtype value.
- **No NVLink, no P2P.** Never set `GGML_CUDA_P2P=1` (runs at PCIe speed anyway; corrupts/crashes
  on some boards). Tensor/row-split collectives stage through host RAM — they do **not** speed up
  single-stream decode. Prefer layer/pipeline split; reserve TP for MoE+EP or prefill.
- **Multi-GPU can corrupt output silently** — known bugs (llama.cpp #20052 long-ctx garbage on
  non-P2P dual-3090; vLLM #40725 non-English garbage under TP). **Validate a real long /
  non-English generation, not just startup**, before trusting any 2-GPU profile.
- **Prefer MTP/speculative decoding when the model supports it** (native MTP/nextn head, or a
  matching DFlash drafter) — confirm the GGUF actually exports the head in the launch log
  (`draft-mtp`/`dflash` lines); some repos ship the config field but not the tensors.
- Existing profiles are single-GPU anchors — mirror their arg spelling and description breadcrumbs
  (quant, KV, ctx, peak GB, tok/s, acceptance). **On a 2-GPU box llama.cpp auto-layer-splits when
  both cards are visible** — pin single-GPU profiles to GPU1 so their proven numbers hold.

## Backend dispatch

| Kind | Catalog id | Model format / ref | Multi-GPU |
|---|---|---|---|
| llama-server | llama-cpp-default, llama-cpp-cuda | GGUF, local path | layer-split / tensor-split → llama-family.md |
| beellama-cpp | beellama-rtx3090 | GGUF (+DFlash drafter), local path | layer-split / tensor-split → llama-family.md |
| buun-llama-cpp | none — `backend add` first | GGUF, local path | layer-split → llama-family.md |
| vllm | vllm-default | safetensors/AWQ/GPTQ, path or HF repo id | TP / PP / DP → vllm-sglang.md |
| sglang | sglang-default | safetensors/AWQ/GPTQ, path or HF repo id | TP / PP / DP → vllm-sglang.md |
| dflash | lucebox-dflash | GGUF, positional (model field) | draft-split: target GPU1 + draft GPU0 (74 t/s @256k); target layer-split −46% — avoid → dflash.md |
| unsloth | unsloth-rtx3090 | HF repo / GGUF | single-GPU, pin → dual-gpu.md §unsloth |
| tabby | none — `backend add` first | EXL2/EXL3 model **dir**, local path | TP (native, no NVLink) / pin → exllama-tabby.md |

Backend unspecified? GGUF + MoE with MTP head → llama-cpp-default (draft-mtp); GGUF dense with a
DFlash drafter available → beellama-rtx3090; other GGUF → beellama-rtx3090; safetensors/AWQ/GPTQ →
vllm-default; **EXL2/EXL3 (quantization_config.quant_method = exl2|exl3) → tabby (TabbyAPI)**. Honor
an explicit backend after the format check; mention an alternative only when measurably better. **If placement is multi-GPU or pin-per-GPU, read references/dual-gpu.md, THEN
read exactly ONE backend file.** Single-GPU model that fits → just the one backend file.

## Workflow

1. **Gather**: model (local path + file/dir size, or HF repo id), backend, target context window,
   bias (precision | balanced | max-context), and **placement** (from the decision above). Model
   missing locally? GGUF single file → `model-loader model download <repo> <file> --wait`;
   safetensors repo → `hf download <repo> --local-dir /home/diogo/models/huggingface/<org>/<repo>`.
2. **Resolve backend**: entry in `~/.config/model-loader/backends/catalog.json`? buun → see its
   section in llama-family.md (registration flow).
3. **Format check**: GGUF ↔ llama family/dflash; safetensors/AWQ ↔ vllm/sglang. Mismatch → propose
   the matching backend, don't force it.
4. **Placement + VRAM budget**: single-GPU → weights floor + KV estimate vs ≤23 GiB. Multi-GPU →
   vs ~46 GiB pooled, reserving ~5–8 GiB/card for activations + CUDA-graph buffers (split overhead
   is compute/comm, not a fixed VRAM tax). Estimates pick the start; **nvidia-smi decides — per
   card: `nvidia-smi --query-gpu=index,memory.used --format=csv`.**
5. **Apply the context adjustment rule** (below) — settle final ctx + quant + placement combo.
6. **Write the profile**: `model-loader profile create --id <id> --backend <backend-id> --file -
   <<'EOF' … EOF` (id regex `^[a-z0-9]+([._-][a-z0-9]+)*$`; omit `meta`; omit `port` always).
   Single-GPU/pin → `launch.env` carries `CUDA_DEVICE_ORDER=PCI_BUS_ID` + `CUDA_VISIBLE_DEVICES`;
   multi-GPU → the split flags from the backend file (and no CUDA_VISIBLE_DEVICES mask).
7. **Validate**: `model-loader profile validate <id>` must pass. Unknown-arg error → re-check schema
   spelling; binary-real but schema-absent → `extraArgs`.
8. **Launch + calibrate**: `model-loader instance start <id>` → `model-loader instance logs <id> |
   tail -50` (model + drafter loaded, KV-size line, `n_ctx_train` warning, which GPU(s) got the
   tensors) → `nvidia-smi --query-gpu=index,memory.used --format=csv` (**check BOTH cards**). For
   multi-GPU, **run one long / non-English generation and confirm the output is coherent** (the
   corruption bugs above). If `instance logs`/`instance list` say "not found" while the process is
   demonstrably up (proxy-owned), read the log file directly:
   `~/.local/state/model-loader/logs/<profile>-<port>.log`. Under budget → raise ctx (KV is linear);
   over budget or OOM → the backend file's sacrifice ladder. **One change per iteration, re-measure.**
   Calibrating on a busy proxy: any other client request evicts your instance mid-measurement (no
   drain) — calibrate when the proxy is quiet, or prefill progressively (90k → 150k → 250k) so the
   slot prefix cache survives reloads. An open TUI session holds
   `~/.local/state/model-loader/model-loader.lock`, blocking `instance start/stop` and `benchmark`
   (find the holder with `fuser` on the lock file). A chat request through the proxy still triggers
   a swap while the CLI is lock-blocked.
9. **Benchmark**: `model-loader benchmark --profile <id> --mode llama-bench` for tok/s; precision
   bias → also one quality mode (`instruction-bench` or `mmlu-bench`). The benchmark reloads the
   instance itself (pid changes) — harmless.
10. **Record**: quant, KV types, ctx, **placement (single GPU1 / layer-split / TP / pin)**, per-card
    peak GiB, tok/s (+ acceptance % if speculative). For a split profile, record the
    **single-vs-split tok/s delta** so the cost is visible. If launch/calibration can't happen now,
    record estimates + "calibration pending" — never present estimates as measurements.

## Context adjustment rule

The target ctx is a band, not a contract: move it within **−25% / +50%**, only when crossing a real
cliff, and always say so:

- **RAISE** when headroom is already paid for (>1.5 GiB free on the binding card at target) — free
  context. Cap at the model's native ctx (`n_ctx_train` / `max_position_embeddings`) unless the user
  wants RoPE/YaRN extension.
- **LOWER** (requires a named, ≥10% gain) when it buys: one weight-quant tier up (quality), one
  KV-type tier up (long-ctx quality), full GPU offload, avoiding `enforce-eager` (vllm), or letting
  a bigger quant fit the ~46 GiB pool when splitting.
- Never drop below 75% of the request silently. Bias tie-breaker: precision → spend headroom on
  quant tiers; max-context → spend on ctx. Budget is **per-card** when single-GPU, **~46 GiB pooled**
  when layer-split.

## Common mistakes

| Mistake | Reality | Fix |
|---|---|---|
| `"port"` in args | Reserved, silently stripped | Omit; clients hit the proxy, model = profile id |
| "Two 3090s = 48 GiB, so my model runs faster" | Partly true here: more VRAM ≠ speed by itself, but llama.cpp `split-mode tensor` gives +22…39% at short context (≤256k). At longer contexts, the speed gap shrinks or reverses | Second card = capacity/2-models **or** +22…39% via `split-mode tensor` at short ctx (≤31B, no external draft) |
| "tensor-parallel doubles single-stream tok/s" | Not 2×, but llama.cpp `split-mode tensor` measured **+22…39%** at short ctx here (≤31B, batch-1, both cards' bandwidth in parallel); at ≥512k single-GPU may win. vLLM custom-allreduce TP is −3…−6% — impl-dependent | Use `split-mode tensor` for speed at short ctx (no external draft); expect +¼…+⅖, not 2× |
| llama.cpp `--split-mode row` "is the fast multi-GPU mode" | Row is deprecated, measured −2.8× here. The genuine fast TP mode is `--split-mode tensor` (+22…39% at short ctx); `row` ≠ `tensor` | `tensor` for speed at short ctx (no draft), `layer` for compatibility; **never** `row` |
| `split-mode tensor` + a DFlash / external-MTP draft | `ggml_abort` — **no fix** (open upstream bug #22473/#24309; beellama rejects it; tested all 4 ways: auto / device-draft / override-tensor → all crash). And keeping the draft WINS: Qwen3.6-27B DFlash-layer **79.1** > DFlash-single 74.9 ≫ tensor-no-draft **48.2** (−36%) | Always `split-mode layer` for any draft profile (keeps the draft; Qwen3.6-27B DFlash-layer 82.8 ≫ tensor-no-spec 49.0 at max ctx). Internal MTP under tensor is model-dependent — Qwythos-9B Q8_0 MTP **WORKS**, +22% at 256k (166.4 tok/s, `draft-mtp` n-max 3), but Qwen3.6-27B MTP CRASHES (nextn split-axis); validate per model |
| "tensor mode needs F16/BF16 KV / OOMs as ctx grows" | False — q4_0 KV works under `split-mode tensor` (tested 9B@256k/768k/1M, 31B@32k, no OOM, output coherent). 1M context on Qwythos-9B Q8_0: 20.3 GiB peak VRAM, 3 GiB headroom per card | Use your normal q4_0 KV; `flash-attn on` still required; set `n-gpu-layers` (no auto-fit) |
| "layer-split a fitting model will slow decode" | Measured flat-to-+7% here (9B/27B/35B-A3B, llama.cpp+beellama; sequential pipeline, only thermal relief) | Pin single-GPU for determinism/VRAM; `layer` +4…7% (safe + drafts), `split-mode tensor` +22…39% at short ctx (fastest, no external draft) |
| "tensor split always gives the same speedup" | Speedup is context-sensitive — KV cache traffic between GPUs grows with ctx. Qwythos-9B Q8_0: +22% at 256k (166 vs 136 tok/s) but **−11% at 768k** (162 vs 181 — single-GPU wins). At long ctx, tensor split's real win is **capacity** (1M impossible on single-GPU) | Measure per model and ctx; at ≥512k compare against single-GPU before committing to tensor |
| Single-GPU profile silently layer-splits on the 2-GPU box | llama.cpp auto-splits when both cards visible | Pin: `CUDA_DEVICE_ORDER=PCI_BUS_ID` + `CUDA_VISIBLE_DEVICES=1` |
| Also setting `-mg 1` / `--device CUDA1` with a CUDA_VISIBLE_DEVICES mask | Masking remaps the card to `cuda:0` inside the process | Mask via env; leave the backend device flag at default 0 |
| Setting `GGML_CUDA_P2P=1` to "speed up" split | No GeForce P2P; PCIe-speed anyway; corrupts on some boards | Don't set it |
| "fp8 is in the enum, so it works" | Enum = binary accepts it; SM86 has no FP8 compute | int4 AWQ/GPTQ (marlin) or bf16; fp8 KV is storage-only |
| vllm/sglang profile without `served-model-name` | Proxy forwards `model: <profile-id>`; vllm 404s | Set `"served-model-name": "<profile-id>"` |
| Trusting a 2-GPU profile after startup only | Known corruption bugs (#20052, #40725) | Validate a long / non-English generation |
| Trusting VRAM estimates | CUDA context + fragmentation + long-prompt prefill grow the pool | nvidia-smi per card after launch AND after a long-prompt test |
| Two models live via `instance start` both | The proxy/`instance start` is single-active (swaps, evicts) | Pin per GPU + a 2nd `serve --port N` — see dual-gpu.md |
| Profile for buun without catalog entry | Kind exists in code, not catalog → validate fails | `backend add` + `schema refresh` (llama-family.md) |
| HF repo id as model for llama family | os.Stat fails — repo ids legal only for vllm/sglang | Download the GGUF first |

## Red flags — stop and re-read the reference file

- "tensor-parallel can't help without NVLink" — false for llama.cpp `split-mode tensor` (+26…39%, ≤31B);
  but it crashes with an external draft, and vLLM TP / `split-mode row` genuinely do lose
- "It's in the enum, so the GPU supports it"
- "row-split / `GGML_CUDA_P2P` is the fast path"
- "I'll skip validate, the JSON looks right"
- "Estimates say it fits, no need for per-card nvidia-smi"
- "Startup succeeded, ship the 2-GPU profile" (run a long / non-English generation first)
- "Idle VRAM is fine, ship it" (long prompts grow the CUDA pool — test one)
- "I'll just run the server binary quickly to check"

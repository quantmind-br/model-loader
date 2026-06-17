# vLLM & SGLang on RTX 3090 24GB (Ampere SM86)

Python servers launched through wrapper scripts in the catalog (`vllm-default` →
`backends/vllm/vllm-serve.sh`, venv vLLM 0.22.1; `sglang-default` →
`backends/sglang/sglang-serve.sh`, venv SGLang 0.5.9). Model = HF repo id (validator skips the
local-path check for these two kinds only) or local safetensors dir. GGUF loading exists
(`load-format: gguf`) but is slow/limited — prefer a llama-family backend for GGUF.

## Ampere matrix — read this before accepting any quant/dtype value

SM86 has **no FP8 compute units** and no MX formats. The schema enums describe the binary, not
this GPU.

| Value | On RTX 3090 |
|---|---|
| `quantization`: awq, awq_marlin, gptq, gptq_marlin, marlin, bitsandbytes, compressed-tensors (W4A16/W8A16), gguf | ✅ works (marlin int4 kernels are the sweet spot) |
| `quantization`: fp8, mxfp8 | ❌ needs SM89+/SM90+ — refuse and explain, even if the user asks |
| `quantization`: mxfp4 | ❌ Blackwell only |
| `dtype`: bfloat16, float16 | ✅ |
| `kv-cache-dtype`: auto | ✅ default, safe |
| `kv-cache-dtype`: fp8_e5m2 (and vllm `fp8`) | ✅ *storage-only* quantization — **verified on this card** (vLLM 0.22.1, 2026-06-12: launch log "Using fp8_e5m2 data type to store kv cache", 28k needle probe passed). Still confirm the log line + run a quality probe each new engine version; fall back to `auto` |
| `kv-cache-dtype`: fp8_e4m3 | ⚠️ needs scales; weaker support on Ampere — prefer e5m2 or auto |
| sglang `kv-cache-dtype`: fp4_e2m1 | ❌ Blackwell only |

Best quality path on 24 GB: a 4-bit AWQ/GPTQ **checkpoint** of a bigger model beats squeezing
a bf16 checkpoint of a smaller one. Verify the exact repo exists on HF before promising it
(official `Qwen/*-AWQ` releases, `cyankiwi/*` 4-bit conversions, etc.).

## Both backends: profile musts

- **`"served-model-name": "<profile-id>"` is mandatory.** The proxy forwards the request body
  unmodified, so the backend receives `model: <profile-id>`; vLLM rejects unknown model names
  (every request 404s), sglang defaults the name to the model path. llama-family servers don't
  care — these two do.
- `"host": "127.0.0.1"`.
- Multi-file safetensors downloads: `hf download <repo> --local-dir
  /home/diogo/models/huggingface/<org>/<repo>` (the built-in `model download` is per-file,
  GGUF-oriented). Pre-download instead of letting the engine pull into the HF cache mid-launch.
- Validator nuance: an HF **repo id** in `model` skips the existence check; a **path-looking**
  string is stat-checked — a planned-but-not-downloaded local dir fails `profile create`. Use
  the repo id until the download exists, then repoint to the local dir.
- **Startup takes minutes** (weight load + CUDA graph capture; sglang also JIT-compiles
  flashinfer kernels on FIRST launch via its bundled CUDA 12.8 toolkit). The proxy health wait
  may time out on first launch — if it does, the process is usually still loading: check
  `instance logs` before assuming failure, and retry once warm.

## vLLM knobs (schema `vllm-default.json`)

- `gpu-memory-utilization`: one knob owns the budget — engine loads weights then fills the rest
  with paged KV blocks. Default 0.92; 0.95 usually safe on a dedicated card — verify.
- `max-model-len`: the context knob (int, `"1k"` notation, `auto`). **Requesting beyond native
  `max_position_embeddings` errors at startup** unless rope scaling is injected via
  `hf-overrides` JSON. Default to clamping at native; YaRN only on explicit request.
- **Calibration shortcut**: when the KV pool can't hold `max-model-len`, vLLM fails fast and
  prints the max it CAN serve — read the log and use that number instead of guessing.
- Single-user serving: `max-num-seqs: 8`, `enable-prefix-caching: true`,
  `enable-chunked-prefill: true`, `max-num-batched-tokens: 8192` (prefill throughput).
- `enforce-eager: true` saves ~1–2 GiB (no CUDA graphs) at a real decode-speed cost — last
  resort; lowering ctx ~10–20% to keep graphs is usually the better trade.
- Sampling is per-request; `generation-config: auto` (default) applies the model repo's
  generation_config.json — keep it, don't put sampling in args.
- VL models used text-only: `language-model-only: true` frees the vision tower's VRAM.
- Quantized checkpoints: omit `quantization` (autodetected from config.json); set it only to
  force a kernel (e.g. `awq_marlin`).

```json
{
  "schemaVersion": 3, "id": "<model>-vllm-<ctx>k", "name": "<Name> (vLLM, <ctx>k)",
  "model": "/home/diogo/models/huggingface/<org>/<repo>",
  "args": {
    "served-model-name": "<profile-id>", "host": "127.0.0.1",
    "max-model-len": "65536", "gpu-memory-utilization": 0.92,
    "kv-cache-dtype": "auto", "dtype": "auto",
    "max-num-seqs": 8, "enable-prefix-caching": true,
    "enable-chunked-prefill": true, "max-num-batched-tokens": "8192"
  },
  "launch": { "defaultBackground": true, "backendId": "vllm-default",
              "restart_policy": "none", "max_restarts": 3, "backoff_seconds": 5 },
  "pinned": false
}
```

OOM/doesn't-fit ladder: use the printed max-model-len → gpu-memory-utilization 0.95→0.92→0.90 →
4-bit checkpoint instead of bf16 → kv-cache-dtype fp8_e5m2 (verify quality) → enforce-eager
(last). Never `cpu-offload-gb`/`swap-space` for interactive use.

## SGLang knobs (schema `sglang-default.json`; keys verbatim, no canonical mapping)

- Model flag is `--model-path` (emitted from the profile `model` field automatically).
- `mem-fraction-static`: VRAM fraction for weights+KV. Default auto (~0.85–0.88); start 0.90,
  push 0.93 only with measurement (CUDA-graph OOM at startup is the failure mode);
  `cuda-graph-max-bs: 8` shrinks graph memory for single-user.
- `context-length`: default = model config. Larger-than-native is allowed but degrades quality
  (it warns) — clamp to native; real extension only via `json-model-override-args` rope_scaling.
- **sglang has NO vllm-style fail-fast**: it launches happily with a KV pool smaller than the
  advertised context (log: `max_total_num_tokens=126518 ... context_len=131072`). After every
  launch, compare `max_total_num_tokens` vs `context_len` in the log and clamp
  `context-length` down to the pool — otherwise the profile advertises context it cannot serve.
- First launch JIT-compiles flashinfer inside the "Capture cuda graph" step (measured: 110.9s
  first start vs 0.85s warm) — a slow first capture is not a hang. Warm profile swap ≈ 19s.
- `attention-backend`: leave unset (auto). SM86 candidates: flashinfer, triton, torch_native.
  fa3/fa4/flashmla/cutlass_mla/trtllm_* are newer-arch — never pick them.
- Radix prefix cache is on by default — don't disable.
- Speculative: `speculative-algorithm: NGRAM` is the zero-asset option; EAGLE/EAGLE3 need
  trained heads (exist for vanilla Qwen/Llama, not for custom merges); STANDALONE takes any
  small same-tokenizer draft. Gains workload-dependent — benchmark before keeping.
- Qwen thinking models: `reasoning-parser: qwen3`, `tool-call-parser: qwen`.
- `tp-size`/`tensor-parallel-size` are aliases — set neither (single GPU).
- Sampling per-request; `sampling-defaults: model` (default) keeps the model's generation
  config.

```json
{
  "schemaVersion": 3, "id": "<model>-sglang-<ctx>k", "name": "<Name> (SGLang, <ctx>k)",
  "model": "/home/diogo/models/huggingface/<org>/<repo>",
  "args": {
    "served-model-name": "<profile-id>", "host": "127.0.0.1",
    "context-length": 65536, "mem-fraction-static": 0.90,
    "cuda-graph-max-bs": 8
  },
  "launch": { "defaultBackground": true, "backendId": "sglang-default",
              "restart_policy": "none", "max_restarts": 3, "backoff_seconds": 5 },
  "pinned": false
}
```

OOM ladder: mem-fraction-static ↓ (0.90→0.87) → context-length ↓ → 4-bit checkpoint →
kv-cache-dtype fp8_e5m2 (verify) → chunked-prefill-size 4096.

## Fits and anchors

**Measured (vLLM 0.22.1, this card, 2026-06-12)**: Qwen3-8B-AWQ (awq_marlin autodetected),
ctx 40960 (native), kv-cache-dtype fp8_e5m2, util 0.92 → KV pool 14.14 GiB (205,936 tokens,
5× concurrency at 40k); idle 21393 MiB, peak 22963 after a 28k prefill (~1.5 GiB runtime
growth — the long-prompt rule applies here too); llama-bench avg 118.3 tok/s (127.6 @pp128 →
101.9 @pp16k), prefill ~680 tok/s on a cold 28k prompt.

**Measured (SGLang 0.5.9, this card, 2026-06-12)**: Llama-3.1-8B-Instruct-AWQ-INT4,
mem-fraction-static 0.90, kv auto (fp16, 128 KiB/token) → pool 126,534 tokens, so ctx clamped
131072 → 122880 (−6.25%); idle 23309 MiB, peak 23599 after a 116.7k prefill; needle retrieval
exact at 95% of ctx; llama-bench avg 128.8 tok/s (143.9 @pp128 → 101.2 @pp16k); cold prefill
3.1–3.7k tok/s (much faster than the vLLM anchor's — don't carry prefill numbers across
engines).

Uncalibrated ballparks (starting points, not results — always benchmark):

| Checkpoint | Weights | Headroom for KV (util 0.92) | ctx ballpark |
|---|---|---|---|
| 7–8B bf16 | ~16 GiB | ~6 GiB | 24k–48k |
| 14B AWQ-int4 | ~9 GiB | ~13 GiB | 64k+ |
| 26–32B AWQ-int4 | ~14–17 GiB | ~5–8 GiB | 8k–32k (GQA-dependent) |
| 70B any | doesn't fit at meaningful quality | — | recommend smaller model or int4 ≤32B |

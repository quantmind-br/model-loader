# vLLM 0.24.0 & SGLang 0.5.9 on the dual-3090 rig (Ampere SM86)

Catalog ids: **`vllm-stable`** and **`vllm-nightly`** — both venvs are now identical: vLLM
0.24.0 @ `ee0da84ab` (released 2026-06-30) on flashinfer 0.6.12 ("nightly" is the venv you bump
when a post-0.24 feature is needed, e.g. DSpark). *History:* the nightly venv was rebuilt
2026-07-07; its prior editable build (`0.1.dev1+ge24d1b24f`) asserted `fp8 tensor core is not
supported in fa2 backend` on every fp8 prefill — the rebuild fixed it, and fp8 KV is verified
working at request time on the rebuilt 0.24.0 (measured 2026-07-07: ~10k-token prefill through the
proxy → HTTP 200 in 3.5 s, 9993 prompt tokens, coherent output, no fa2/fp8 assertion). SGLang
variants: **`sglang-stable`** (0.5.9, py3.11), **`sglang-nightly`** (0.5.6-dev6948 main — has DFLASH
spec), **`sglang-dflash`** (isolated PR#23000 build, 0.5.6-dev6167, DFLASH/NEXTN on qwen3_5 archs;
venv at `backends/sglang-dflash/dflash/.venv`), **`sglang-unlimited`** (dev11416 custom fork for
baidu/Unlimited-OCR only). Model = HF repo id (validator skips local-path check for these
kinds) or local safetensors dir. GGUF loading exists but is fragile (SGLang MoE-GGUF broken,
CVE-2026-5760) — GGUF belongs on the llama family.

## Ampere matrix — read before accepting any quant/dtype value

SM86 has **no FP8/FP4 compute units**. Schema enums describe the binary, not this GPU.

| Value | On RTX 3090 |
|---|---|
| `quantization`: awq/awq_marlin, gptq/gptq_marlin, marlin, compressed-tensors W4A16/W8A8-int8, bitsandbytes | ✅ (marlin int4 = the sweet spot; min cap 75/80) |
| **fp8 weight checkpoints** | ⚠ **load and run** — auto-routed to Marlin W8A16 weight-only (fp8 storage, bf16 compute) in both vLLM 0.24 and SGLang 0.5.9. Half weight size, no fp8 speed. int4 AWQ is usually the better pick, but don't refuse fp8 repos outright anymore |
| **int4 MoE (GPTQ/AutoRound/CT-MoE)** | ⚠ crash-prone on SM80/86 (vLLM #35922 open; rig-reproduced `moe_sum` crash) — live-test per model; AWQ with `awq_marlin` + unquantized attn layers is the known-good combo |
| `quantization`: mxfp4, nvfp4/modelopt_fp4, w4afp8 | ❌ SM89+/Blackwell |
| `kv-cache-dtype`: fp8 / fp8_e5m2 / fp8_e4m3 | ✅ **storage-only** (dequant on read). vLLM: fp8 KV auto-switches attention to FLASHINFER (FLASH_ATTN rejects it on SM86). SGLang: works on flashinfer+triton, prefer e4m3 |
| vLLM `kv-cache-dtype`: turboquant_k8v4 | ✅ new in 0.24 (2.6× KV, +1.17% PPL) — the experiment when 256k must fit one card; prefer plain fp8 for coding quality |
| sglang `kv-cache-dtype`: fp4_e2m1 | ⚠ experimental on SM86 (triton only); skip |

## Both backends: profile musts

- **`"served-model-name": "<profile-id>"` mandatory** (proxy forwards `model: <profile-id>`).
- `"host": "127.0.0.1"`.
- Pre-download safetensors: `hf download <repo> --local-dir /home/diogo/models/huggingface/<org>/<repo>`.
  Repo id in `model` skips the existence check; a path-looking string is stat-checked.
- **Startup takes minutes** (weights + CUDA-graph capture; sglang JIT-compiles flashinfer on
  first launch). Proxy health wait may time out on first launch — check `instance logs`, retry warm.
- **Cold-start rule: the first request after load runs ~half speed** (torch.compile/graphs) —
  always warm-measure before recording tok/s.
- `launch.env` staples: `CUDA_DEVICE_ORDER=PCI_BUS_ID` (+ `CUDA_VISIBLE_DEVICES=1` when
  single-GPU), `PYTORCH_CUDA_ALLOC_CONF=expandable_segments:True` (vLLM, avoids fragmentation
  OOM on deep prefill). **TP2: drop `NCCL_P2P_DISABLE` (P2P-on, measured +13.5% concurrent) but KEEP
  `disable-custom-all-reduce: true` — custom AR crashes on SM86** (dual-gpu.md §P2P).
- Sampling: both engines default to the repo's `generation_config.json`; per-request values
  (Claude Code's temperature!) override it — see model-research.md.

## vLLM 0.24.0 knobs

- **Attention:** auto = FLASH_ATTN (FA2) on SM86; fp8 KV → auto-FLASHINFER. The old
  `VLLM_ATTENTION_BACKEND` env **no longer exists** — use `--attention-backend` if you must pin.
- **`--performance-mode interactivity`** (new): captures CUDA graphs at every batch size 1–32 —
  purpose-built for batch-1 agents; use it in every interactive profile.
- `gpu-memory-utilization` default 0.92. **`max-num-seqs` 4–8** for single-operator (large
  values → CUDA-graph capture starves KV — measured here; graphs scale with max_num_seqs×2).
  `max-num-batched-tokens 4096–8192` (default 2048; higher = faster 100k prefill TTFT;
  4096 also caps prefill VRAM transients on TP2 — measured).
- `max-model-len`: beyond native `max_position_embeddings` errors at startup; when the KV pool
  can't hold it, vLLM fail-fast prints the max it CAN serve — use that number.
- **Prefix caching default ON**; `--prefix-caching-hash-algo xxhash` is a free prefill-CPU win
  (pip install xxhash first). **`--kv-offloading-size 16`** (GiB) spills evicted KV blocks to RAM
  — keeps agent sessions warm (60 GiB RAM box: 16–24 GiB budget).
- **Speculative** (see speculative.md): `--speculative-config` JSON — `mtp` (use instead of the
  deprecated `qwen3_5_mtp` alias; causal → fp8 KV OK → 256k), `dflash` (non-causal —
  `vllm/v1/spec_decode/dflash.py:294` asserts `causal is False`; fp8 KV forces FLASHINFER on SM86,
  and the measured 128k cap / BF16-drafter constraint was on the 0.23 build, so the fp8×DFlash
  retest on 0.24.0 + flashinfer 0.6.12 is still open — keep fp8 KV off DFlash until measured),
  `ngram_gpu` (keeps async scheduling; plain `ngram` silently disables it; `prompt_lookup_min`
  ≥8 on tool-calling profiles — #40875), `eagle3`, `suffix` (needs `arctic-inference` pip). Method
  enum verified against the installed wheel (`vllm/config/speculative.py::SpeculativeMethod`):
  `mtp`/`dflash`/`ngram`/`ngram_gpu`/`eagle`/`eagle3`/`medusa`/`mlp_speculator`/`draft_model`/`suffix`
  all present; MTP sub-types (`qwen3_5_mtp`, `glm4_moe_mtp`, …) normalize to `mtp`.
- **Async scheduling default ON** — don't break it (CPU ngram does).
- `enforce-eager`: kills all CUDA graphs — never in production profiles; lower ctx instead.
- **Tool calling:** `--enable-auto-tool-choice --tool-call-parser hermes` (Qwen non-coder;
  see model-research.md matrix). **Reasoning parser: leave UNSET** until the token-accounting
  test passes (parsers measured dropping 1224/1692 tokens here).
- VL text-only: `"language-model-only": true` now genuinely skips loading the vision tower.
- Quantized checkpoints: omit `quantization` (autodetect); set only to force a kernel.
- **Sleep mode** (`--enable-sleep-mode` + env `VLLM_SERVER_DEV_MODE=1` → `POST /sleep?level=1`,
  `/wake_up`): 18–200× faster than reload — an integration opportunity for fast profile swaps,
  but model-loader's supervisor assumes kill-to-free-VRAM; don't use it in profiles yet.

```json
{
  "schemaVersion": 3, "id": "<model>-vllm-<ctx>k", "name": "<Name> (vLLM, <ctx>k)",
  "model": "/home/diogo/models/huggingface/<org>/<repo>",
  "args": {
    "served-model-name": "<profile-id>", "host": "127.0.0.1",
    "max-model-len": "131072", "gpu-memory-utilization": 0.92,
    "max-num-seqs": 8, "max-num-batched-tokens": "8192",
    "performance-mode": "interactivity",
    "enable-auto-tool-choice": true, "tool-call-parser": "hermes",
    "speculative-config": "{\"method\":\"mtp\",\"num_speculative_tokens\":3}"
  },
  "launch": { "defaultBackground": true, "backendId": "vllm-stable",
              "env": [{"key":"CUDA_DEVICE_ORDER","value":"PCI_BUS_ID"},
                      {"key":"CUDA_VISIBLE_DEVICES","value":"1"},
                      {"key":"PYTORCH_CUDA_ALLOC_CONF","value":"expandable_segments:True"}],
              "restart_policy": "none", "max_restarts": 3, "backoff_seconds": 5 },
  "pinned": false
}
```
(Flags not in the schema → extraArgs. TP2 variant: drop the CUDA_VISIBLE_DEVICES pin, add
`"tensor-parallel-size": 2, "distributed-executor-backend": "mp"`; util 0.84–0.90 — GPU0 runs the
desktop. **P2P-on: no `NCCL_P2P_DISABLE` (measured +13.5% concurrent, log `isAllDirectP2p 1` / `via
P2P/CUMEM`). `disable-custom-all-reduce: true` is MANDATORY on SM86 — custom AR engages then crashes
(`custom_all_reduce.cuh:455`); never drop it. Prove the NCCL-P2P transport from the launch log. Init-hang
fallback: re-add `NCCL_P2P_DISABLE=1`** — dual-gpu.md §P2P.)

OOM ladder: use the printed max-model-len → util 0.95→0.92→0.90→0.84 (TP2) →
max-num-batched-tokens 4096 (caps prefill transient) → 4-bit checkpoint → kv fp8 (verify quality)
→ turboquant_k8v4 (experiment) → enforce-eager (last resort; usually lower ctx instead).
Never `cpu-offload-gb`/`swap-space` for interactive use.

## SGLang 0.5.9 knobs

- Model flag is `--model-path` (emitted from `model` automatically). Keys verbatim, no
  canonical mapping.
- **Attention:** auto = flashinfer on SM86 (JIT on first launch — the wrapper exports the
  bundled CUDA toolkit; measured 110.9 s first capture vs 0.85 s warm). `triton` = fallback +
  the rig-verified spec-decode combo. Never fa3/fa4/flashmla/trtllm_* (Hopper/Blackwell).
- `mem-fraction-static`: auto ≈0.847 on 24 GiB (≈0.764 with EAGLE); set explicitly 0.88–0.90
  and watch `available_gpu_mem` in the log. `chunked-prefill-size` 4096 (auto default 2048).
  `--cuda-graph-bs 1 2 4 8` (extraArgs) frees capture memory vs the default max-bs 24.
- **sglang has NO vllm-style fail-fast**: after every launch compare `max_total_num_tokens` vs
  `context_len` in the log and clamp `context-length` down to the pool.
- **Hybrid Qwen3.5/3.6 warning (why the Qwen3.6 agent profiles live on vLLM/llama.cpp):** on
  0.5.9, hybrid GDN/mamba archs lose the overlap scheduler when radix cache is on
  (`--mamba-scheduler-strategy auto`→`no_buffer`, `server_args.py:874`; overlap disabled at
  `server_args.py:1749`), and **any speculative algorithm disables the radix cache entirely on
  them** → full re-prefill every agent turn (`server_args.py:1762`). Escape: `--mamba-scheduler-strategy
  extra_buffer` (choices auto/no_buffer/extra_buffer, `server_args.py:4232`; what sglang-dflash uses)
  — less validated. Pure transformers unaffected.
- **Speculative:** `--speculative-algorithm {EAGLE,EAGLE3,NEXTN,STANDALONE,NGRAM}` (NEXTN =
  native MTP; DFLASH only in sglang-nightly/sglang-dflash). EAGLE-family disables overlap unless
  env `SGLANG_ENABLE_SPEC_V2=1` (topk=1 only). Auto params (3,1,4); measured NEXTN on Qwen3.6
  AWQ-MTP tp2: ~107–119 tok/s with `--attention-backend triton`.
- **RadixAttention default on** — the best engine for parallel subagents sharing a prefix
  (token-granularity, cross-request). `--schedule-policy lpm` for prefix-affinity when >1
  concurrent request. **HiCache** for multi-session warmth: `--enable-hierarchical-cache
  --hicache-ratio 2 --hicache-write-policy write_through` (+ optional disk L3:
  `--page-size 64 --hicache-storage-backend file`). Skip for one sequential session.
- Parsers now validate directly in `args` (no extraArgs workaround): `--reasoning-parser qwen3`
  (or `qwen3-thinking` for forced-open templates), `--tool-call-parser qwen3_coder|qwen|glm|glm47|kimi_k2…`
  (model-research.md matrix). The curated sglang enums were widened (Phase-0 fix, BUGS.md S2) to the
  installed 0.5.9 detector-map union — `reasoning-parser` 23 values incl. `qwen3-thinking`,
  `tool-call-parser` 30 values incl. `qwen3_coder`/`glm47` (source:
  `sglang/srt/function_call/function_call_parser.py::ToolCallParserEnum`,
  `sglang/srt/parser/reasoning_parser.py::DetectorMap`; curated `curated_sglang.go`) — so set both
  parsers in `args`.
- Deterministic evals: `--enable-deterministic-inference --attention-backend triton`
  (mandatory explicit triton on SM86 — the auto fallback picks fa3 and fails; ~34% slower).
- Dual-GPU: `"tp-size": 2` + `--enable-p2p-check` (0.5.9 assumes P2P allowed without checking); keep P2P
  on. **P2P engagement here is INCONCLUSIVE (silent self-disable, no log proof) → treat SGLang TP2 as
  NCCL; keep `disable-custom-all-reduce`** (SM86 custom AR is unproven here and crashes on the vLLM sibling).
  Add `NCCL_P2P_DISABLE=1` only as the fallback if it hangs at "Init torch distributed". Model fits one card → don't TP; `--dp-size 2` only for heavy concurrent fan-out (2
  separate pinned servers are operationally simpler).

```json
{
  "schemaVersion": 3, "id": "<model>-sglang-<ctx>k", "name": "<Name> (SGLang, <ctx>k)",
  "model": "/home/diogo/models/huggingface/<org>/<repo>",
  "args": {
    "served-model-name": "<profile-id>", "host": "127.0.0.1",
    "context-length": 131072, "mem-fraction-static": 0.88,
    "chunked-prefill-size": 4096, "max-running-requests": 8,
    "schedule-policy": "lpm",
    "reasoning-parser": "qwen3", "tool-call-parser": "qwen3_coder"
  },
  "extraArgs": ["--cuda-graph-bs", "1", "2", "4", "8"],
  "launch": { "defaultBackground": true, "backendId": "sglang-stable",
              "env": [{"key":"CUDA_DEVICE_ORDER","value":"PCI_BUS_ID"},
                      {"key":"CUDA_VISIBLE_DEVICES","value":"1"}],
              "restart_policy": "none", "max_restarts": 3, "backoff_seconds": 5 },
  "pinned": false
}
```

OOM ladder: mem-fraction-static ↓ (0.90→0.85) → context-length ↓ (to `max_total_num_tokens`) →
4-bit checkpoint → kv-cache-dtype fp8_e4m3 (verify) → chunked-prefill-size 2048.

## Multi-GPU (TP2) — capacity, not speed (see dual-gpu.md)

Only when checkpoint + KV exceed one card, or to unlock fp8-KV 256k on 27B-class. TP2 buys
~double KV pool per card; batch-1 decode gain is model-dependent (MoE+MTP profiles measured
99–150 tok/s here). **Custom all-reduce is BROKEN on SM86 (vLLM 0.24.0, measured 2026-07-10): with the
disable flag absent, CUSTOM engages then crashes (`custom_all_reduce.cuh:455`) — `disable-custom-all-reduce:
true` is MANDATORY, P2P does not fix it.** vLLM: `distributed-executor-backend mp`;
never PP with spec decode. MoE: `enable-expert-parallel` helps capacity-bound MoE (measured
Nex-N2-mini W4A16 TP2+EP: 149.5 tok/s, +13% over GGUF-on-one-card, ~4× better TTFT). ⚠ validate
a long non-English generation (vLLM #40725 corruption class) before trusting any TP profile.

## Tuning by model type (vLLM & SGLang on SM86)

Pick knobs by *architecture class*, not parameter count alone. All four classes inherit the rig
invariants (≤23 GiB/card, symmetric `0.5,0.5` splits only, no NVLink but **vLLM TP2 runs P2P-on** — drop
`NCCL_P2P_DISABLE`, +13.5% concurrent — while **`disable-custom-all-reduce` stays MANDATORY** on SM86;
dual-gpu.md §P2P — prefer a single-GPU pin when the model fits one card).

- **Dense (Llama / Qwen-dense / Gemma-text):** compute-bound decode. Keep `max-num-seqs` low (4–8)
  for a single operator; batch-1 latency lives on CUDA graphs, so `performance-mode interactivity`
  (vLLM) / `--cuda-graph-bs 1 2 4 8` (SGLang) matter more than raw batch. KV grows linearly with ctx
  and there is no cheap-KV arch trick — dense 27–32B AWQ is the class most likely to need fp8 KV (now
  verified on 0.24.0) or TP2 to reach 256k. `max-num-batched-tokens 4096–8192` (vLLM) /
  `chunked-prefill-size 4096` (SGLang) trade prefill TTFT against transient VRAM.
- **MoE / A3B (Qwen3.x-A3B, GLM-4.x-MoE, Ornith/Ravenx 35B-A3B):** ≈3B active params → latency-first.
  **Leave expert-parallel OFF for A3B when it already fits or TP-fits.** `enable-expert-parallel`
  (vLLM) shards experts across both cards, adding all-to-all traffic over PCIe (no NVLink) that 3B
  active params never repay on batch-1 decode; EP earns its keep only when *capacity*-bound (measured:
  Nex-N2-mini W4A16 TP2+EP = 149.5 tok/s, +13% vs GGUF-on-one-card — a capacity, not a latency, win).
  int4-MoE kernels are crash-prone on SM80/86 (vLLM #35922); prefer AWQ + `awq_marlin` with
  unquantized attention. On SGLang, hybrid GDN/mamba MoE (Qwen3.5/3.6) lose the radix cache under any
  speculative algorithm (`server_args.py:1762`) — keep those agent profiles on vLLM/llama.cpp.
- **VL (Qwen3-VL, Gemma-4 multimodal, JoyCaption):** if the profile serves text only, drop the vision
  tower — vLLM `"language-model-only": true` (`vllm/config/multimodal.py:77`) or SGLang
  `--language-only` (`server_args.py:4914`); this frees the encoder VRAM (per sndr.md it is what lets
  a 35B-A3B-FP8 reach 262144 instead of ~69k). Keep the tower ON only when images are actually sent.
  VL prefill is transient-heavy (image tokens) — cap `max-num-batched-tokens` / `chunked-prefill-size`
  at 4096 and warm-measure; Python engines load the tower in bf16 (no SM86 vision-quant knob here).
- **Coding-reasoning (Qwen3-Coder, GLM-4.x, DeepSeek-R1 distills, Ornith):** two coupled choices.
  (1) **Parser wiring, now first-class in `args`** (S1/S2 fixed): `--tool-call-parser` per family
  (`qwen3_coder`/`glm47`/`kimi_k2`…) and `--reasoning-parser` (`qwen3` or `qwen3-thinking`); on vLLM
  leave `reasoning-parser` UNSET until the token-accounting test passes (measured dropping 1224/1692
  tokens). (2) **KV precision vs tool-call fidelity:** coding agents emit shell-hostile tool calls —
  keep KV at fp8 (not fp4/turboquant) for these and verify a shell-hostile round trip before trusting
  the profile. Speculative pairs well here (MTP is causal → fp8 KV OK → 256k); never `enforce-eager`
  (kills graphs — lower ctx instead).

## Measured anchors (this rig)

- **qwen3.6-27b-awq-mtp-fp8-vllm-tp2-256k** (the flagship): AWQ-MTP target, `mtp` spec + fp8
  KV → KV pool 496k tokens (1.89×@262144), full 256k + vision, ~91% MTP accept, ~106 tok/s
  tokenizer-exact. util 0.84 + mnbt 4096 survives 33k prefill with ~1.3 GiB free on GPU0.
- DFlash vLLM tp2: ~150 tok/s but 128k cap (fp8⊥DFlash on the 0.23 build; retest on 0.24) —
  drafter must stay BF16.
- SGLang NEXTN tp2 (sglang-dflash build): ~107–119 tok/s, accept-len ~3.5, triton backend.
- vLLM 0.22 single-GPU: Qwen3-8B-AWQ ctx 40960 fp8 KV → 118 tok/s avg, idle 21.4 GiB, peak
  23.0 after 28k prefill (~1.5 GiB runtime growth — the long-prompt rule applies to Python
  engines too). SGLang 0.5.9: Llama-3.1-8B-AWQ 128.8 tok/s avg, pool 126k tokens → ctx clamped.

Ballparks for planning (always benchmark): 7–8B bf16 ~16 GiB → 24–48k ctx · 14B AWQ ~9 GiB →
64k+ · 27–32B AWQ ~14–19 GiB → 8k–256k (KV-arch-dependent; hybrid-attn Qwen3.x = cheap KV) ·
70B → doesn't fit one card at meaningful quality (dual-GPU tier, dual-gpu.md).

# Strata uncensored IQ2_XS mixed requant — 2026-10-01

**Retired at the operator's request on 2026-10-01.** Both profiles and their
exclusive GGUF, pack, tokenizer and runtime configs were removed, reclaiming
116.79 GiB. This report retains historical measurements. See the
[OrcaRouter IQ3_XXS setup analysis](strata-orca-iq3-xxs-profile-2026-10-01.md).

Created and calibrated local 32k and native 256k profiles for
Qwen3.8-Flash-Next-Uncensored using the copied `backends/strata-fork` backend.
The 256k profile completed a fresh 259649-token retrieval request without OOM.
Warm short-code decode reached a median 120.7 tok/s. Automatic tool selection
and exact command copying still fail in some trials, so neither profile is
qualified for autonomous agents.

## Model provenance and conversion

The queried Hugging Face repositories did not provide the requested ready-made
uncensored IQ2_XS. This artifact is a **mixed requantization from IQ4_XS**, not
a quantization of full-precision weights. It inherits the source checkpoint's
uncensored fine-tuning; the tests below do not establish universal absence of
refusals or censorship.

- Base: `orcarouter/Qwen3.8-Flash-Next-Uncensored`.
- Quant source: `mradermacher/Qwen3.8-Flash-Next-Uncensored-i1-GGUF`, revision
  `27694ee82244cdac3539501330e9bbde98d5d8b8`.
- Source file: `Qwen3.8-Flash-Next-Uncensored.i1-IQ4_XS.gguf`.
- Published importance matrix from the same repository/revision:
  `Qwen3.8-Flash-Next-Uncensored.imatrix.gguf` (902 entries, 319 chunks).
- Converted 96 expert gate/up tensors from IQ4_XS to IQ2_XS with that imatrix.
  Expert down and PLE remain IQ4_NL; all other tensor types and payloads remain
  unchanged, including the native IQ4_XS PLE key.
- Quantizer: `backends/llama.cpp-stable/build/bin/llama-quantize`, 15 threads,
  `--allow-requantize`, `--max-buffer-size 128`, explicit per-tensor type file.
  Conversion completed in 2321.27 seconds.
- GGUF: `/home/diogo/models/huggingface/local/Qwen3.8-Flash-Next-Uncensored-IQ2_XS-Requant/Qwen3.8-Flash-Next-Uncensored-IQ2_XS-Requant-Mixed.gguf`.
  Size: 77969641824 bytes (72.62 GiB); average 3.52 bpw across the model,
  2.31 bpw for the converted expert gate/up tensors.
- Pack: `/home/diogo/models/strata/packs/uncensored-iq2-xs-requant`;
  expert blobs total 45927628800 bytes (42.77 GiB), dense arena about 1.38 GiB.
  Source GGUF and prepared pack are both retained for Strata's native/PLE path.

| Artifact | SHA-256 |
|---|---|
| Source IQ4_XS | `b4787265497582e468293a40dee3ef4f0135fd4eb093bf6b552e6558d0574f9d` |
| Importance matrix | `1eee91d741366a9ccbdd004610410be6e70e319ed613c93457a9243fdfa31d88` |
| Local mixed IQ2_XS GGUF | `960f6b765d9b08931724b9e467695de392338b84f325735eb532cd419c082c5b` |
| Quantizer binary | `125ceb375f3e691106e20a1912a9872f7733df1b82a9807f47dea95dd93a1ca6` |
| Dense packed weights | `ef8a7b47922502e080810c0d674604fc8d05fc4ec29f6d58aa9a103d781e2224` |

Verification covered all 1224 tensors: exactly 96 changed and 1128 have
byte-identical payloads to the source. Geometry and architecture checks passed.
Native CPU/GPU expert parity probes at layers 0, 1, 2, 3, 20 and 47 had zero
failures within numeric tolerances, including width-1 versus width-3 invariance.
This is not a claim of bitwise CPU/GPU equality. Dense packed weights match
the source IQ4 pack's hash. The official Qwen template/tokenizer was preserved;
template SHA-256 is
`c3cf9e34abf4f9e36c2d72165aa9c132d3e2a725b6c2586aaa3a8af9d7a81041`.

## Profiles and configuration

Installed under `~/.config/model-loader/profiles/`:

- `qwen3-8-flash-next-uncensored-iq2-xs-requant-mtp-strata-dual-mmap-w15-32k`
- `qwen3-8-flash-next-uncensored-iq2-xs-requant-mtp-strata-dual-mmap-w15-256k`

Both use Strata 0.1.30 from `backends/strata-fork`, equal 24/24 layer placement
across two RTX 3090s at 270 W/card, mmap experts, 15 workers, int8 KV,
1536 MiB VRAM reserve, automatic expert cache and automatic 8192-token prefill.
The 32k profile starts with 19862/24576 resident experts; 256k starts with
17907/24576 (9188 on CUDA0 and 8719 on CUDA1). Contexts are 32768 and 262144,
without YaRN. The original IQ2_XS 976k profile uses different weights; no
976k uncensored profile was created or validated.

Sampling: non-thinking, temperature 0.7, top_p 0.8, top_k 20, min_p 0,
presence_penalty 1.5, repetition_penalty 1, seed 42, experimental speed
projection disabled. Both use the existing **base Q2_0 MTP**, spec 4/min-p 0.7.
This draft head is shared with other profiles and is not uncensored-matched.
Its acceptance measurements do not establish equivalence to a matched head.

All launches were managed by Model Loader; requests passed through its proxy
at `127.0.0.1:4321`. Profile schemas validated and both launches became healthy.

## Performance and fresh context checks

| Measurement | 32k | 256k |
|---|---:|---:|
| Warm short-code decode, median of 3 | 114.0 tok/s | 120.7 tok/s |
| Warm short-code TTFT, median | 0.372 s | 0.342 s |
| Warm MTP acceptance | 804/883 (91.1%) | 799/880 (90.8%) |
| Portuguese decode | 63.1 tok/s | 70.7 tok/s |
| Fresh near-full input tokens | 29825 | 259649 |
| Fresh near-full TTFT | 15.266 s | 105.316 s |
| Near-full decode | 99.2 tok/s | 78.5 tok/s |
| Exact records recovered | 3/3 | 3/3 |
| Strict JSON-only response | failed: Markdown fence | failed: Markdown fence |
| Context-probe peak CUDA0 / CUDA1 | 22948 / 23081 MiB | 22955 / 23081 MiB |

Warm samples excluded the first cold request. They are not a paired randomized
context-size comparison; the table does not demonstrate that a larger context
is faster. Near-full prompts were uncached (`cache_n=0`) and had records near
the start, middle and end. Both returned the right values inside a Markdown
JSON fence, failing the strict JSON-only oracle. The 32k quality run reached
22949/23083 MiB; subsequent 256k benchmark/tool telemetry peaked at
22955/23083 MiB (22.42/22.54 GiB), below 23552 MiB/card.

The fresh 259649-token request left at least 42.54 GiB RAM available and had
about 0.001 GiB swap growth. Global swap was already occupied before testing;
these measurements are growth, not a claim of zero swap usage. Guards stop
runs after sustained RAM availability below 2 GiB, swap growth above 2 GiB or
VRAM above 23552 MiB/card. No guard triggered for these profiles.

## Model Loader 256k fill benchmark

Run: `qwen3-8-flash-next-uncensored-iq2-xs-requant-mtp-strata-dual-mmap-w15-256k-1790860872458298633`.
Three warm repetitions per preset, reusing the active instance:

| Nominal fill | Actual prompt tokens | Decode mean ± SD | Cached TTFT |
|---|---:|---:|---:|
| 5% | 12592 | 86.9 ± 8.3 tok/s | 82 ms |
| 25% | 62291 | 127.1 ± 1.8 tok/s | 167 ms |
| 50% | 124651 | 119.4 ± 1.1 tok/s | 279 ms |
| 90% | 224101 | 110.1 ± 0.9 tok/s | 454 ms |

All four presets completed. These TTFT values reflect cached prompts and must
not be compared as fresh-prefill latency to the 105-second context probe.
Telemetry showed 22955/23083 MiB per-card peaks, minimum 41.49 GiB available
RAM and no swap growth during this benchmark. Raw transcript remains in the
Model Loader benchmark store.

## Quality and tool-use limitations

A curated 10-problem HumanEval smoke test passed 10/10 on both the source
uncensored IQ4_XS and local IQ2_XS at 32k, with the same prompts, sampling and
seed. Generated code ran in bwrap isolation with prompt-provided imports and
helpers. This small sample cannot establish no quality loss from requantizing.

Both profiles passed 4/4 forced tool-call integrity checks including SSE,
and a clean tool-result continuation ending in `stop`. Automatic selection
with 10 positive and 10 control trials per profile produced:

- 32k: 8 exact calls, 2 corrupted commands; 0/10 spurious control calls.
  One command became `true`; another reordered operations and lost redirection.
- 256k: 8 exact calls, 1 command changed to `true`, 1 missing call;
  0/10 spurious control calls.

The exact-confidence SLO verdict is inconclusive for both profiles. These
failures preclude agent qualification despite the successful forced calls.

At 256k, additional forced shell-hostile canary commands were preserved exactly
with user text tokenized to 130567 and 235422 tokens (nominal 50%/90% fills).
Both tool-result continuations ended cleanly. Peak VRAM was 22955/23083 MiB;
minimum available RAM was 37.59 GiB. The larger fresh tool request grew global
swap by 0.602 GiB, below the 2 GiB guard. These forced checks do not establish
autonomous tool selection at long context.

## Reproduction evidence

Artifacts: `/home/diogo/models/strata/uncensored-iq2-xs-20261001/`.
The `reproduce/` directory includes the exact tensor type map, requantization,
per-tensor verification, pack preparation, profile creation, quality,
context, strict-tool, long-tool and guarded Model Loader benchmark scripts.
The directory contains source revision, model/imatrix/quantizer hashes,
commands, full responses, telemetry, oracle results and failed preparation
attempts followed by their corrected invocations. Model provenance is also
stored next to the GGUF, and pack metadata in `READY.json`.

After validation, the task-owned backend and proxy were stopped with ownership
checks. Model Loader's instance list is empty; the proxy port is closed. GPU
power limits remain 270 W/card. About 81 GiB remains available on the model
filesystem. Temporary pilot GGUFs were removed; the final model and pack remain.

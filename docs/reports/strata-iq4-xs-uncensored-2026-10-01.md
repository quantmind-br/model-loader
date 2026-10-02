# Strata IQ4_XS and uncensored compatibility — 2026-10-01

The uncensored `mradermacher/Qwen3.8-Flash-Next-Uncensored-i1-GGUF`
`Qwen3.8-Flash-Next-Uncensored.i1-IQ4_XS.gguf` is compatible with the copied
Strata backend for short prompts after a preparation fix. It is slower than
the existing base IQ2_XS on this machine. Near-30k prefill failed the swap-growth
guard twice; full 32k and 256k operation are **not validated**. The 32k profile
is experimental, not agent-qualified.

Hardware: dual RTX 3090, no NVLink, 270 W/card. All inference went through
Model Loader and its loopback proxy. Native engine 0.1.30, sm_86, SHA256
`545cfc5f04f581c29ebcf2c1969807efb3dd1bb1ae07fa3f14f396458aba6999`.
The original checkout `/home/diogo/dev/Strata` and inference binary were not
changed. The active checkout is `model-loader/backends/strata-fork`.

## Checkpoint and repair

[Quant repository](https://huggingface.co/mradermacher/Qwen3.8-Flash-Next-Uncensored-i1-GGUF),
revision `27694ee82244cdac3539501330e9bbde98d5d8b8`,
[base checkpoint](https://huggingface.co/orcarouter/Qwen3.8-Flash-Next-Uncensored).
Source size: 97,473,155,360 bytes. Verified SHA256:
`b4787265497582e468293a40dee3ef4f0135fd4eb093bf6b552e6558d0574f9d`.

All 48 expert layers use IQ4_XS gate/up and IQ4_NL down; packed experts occupy
60.9375 GiB. The PLE table is IQ4_NL. The token embedding is IQ4_XS and the
output head is Q6_K. Vocabulary, merges, token types and EOS match the Qwen base.
Its template matches the official template, SHA256
`c3cf9e34abf4f9e36c2d72165aa9c132d3e2a725b6c2586aaa3a8af9d7a81041`.

Initial startup rejected `blk.1.ple_key.weight` because `iq_pack.py` converted
its supported native IQ4_XS key to BF16. The corrected predicate preserves
Q2_0, IQ3_XXS and IQ4_XS PLE keys, retaining conversion for other key types.
Repreparation converted 459 small projections to BF16 and preserved the native
key. Nine focused packer tests passed, including payload/index preservation and
source-byte immutability. Existing packs are not rewritten by this change.

Standalone diagnostics linked to the copied binary's libraries passed:

- GGUF geometry and architecture guards: no invalid ranges or unknown types.
- Real expert CPU/GPU parity: layers 0, 1, 2, 3, 20 and 47, zero failures within
  the diagnostic's numeric tolerance; this is not bitwise CPU/GPU equality.
- Real-format F32/BF16 dequant checks passed.
- Default exact multi-token MMVQ: 148480 outputs, zero bit differences and
  nonfinite values; the negative control detected 63830 differences.

The prepared pack is `/home/diogo/models/strata/packs/uncensored-iq4-xs`;
`READY.json` records source and preparation provenance. The existing base Q2_0
MTP is reused, **not** an uncensored-matched draft. Official base Qwen
non-thinking sampling is .7 temperature, .8 top_p, top_k20, min_p0,
presence1.5/repetition1. The uncensored source generation config was gated.
No refusal-rate or general quality evaluation establishes the advertised
“uncensored” behavior; this report establishes format/runtime compatibility.

## Measurements

Profile: `qwen3-8-flash-next-uncensored-iq4-xs-mtp-strata-dual-mmap-w15-32k`.
24/24 layer placement, int8 KV, mmap experts, 15 workers, automatic expert cache,
1536 MiB VRAM reserve, MTP spec4/min-p .7. Auto cache holds 13940/24576 experts
(7083/6857 per GPU), so misses require host/PCIe work. Auto prefill selected 8192.

| Configuration | Warm code median tok/s | Warm TTFT median | Portuguese tok/s | Peak GPU0/1 MiB, including attempted long prefill |
|---|---:|---:|---:|---|
| Base IQ2_XS, 32k | 133.8 | .328 s | 80.5 | 21450 / 22571 |
| Uncensored IQ4_XS, prefill auto8192 | 76.3 | .503 s | 40.4 | 22964 / 23089 |
| Uncensored IQ4_XS, prefill2048 | 75.9 | .571 s | 31.5 | 22969 / 23099 |

Each warm code median has three samples after one cold sample. IQ4 samples
spread from 54.4–93.4 tok/s with auto prefill and 63.1–87.7 with 2048; the latter
has no demonstrated improvement. These are different checkpoints, not a pure
quantization A/B or quality ranking. Download activity overlapped the IQ2
baseline. Warm coding MTP acceptance was 764/837 (91.3%) for IQ4 auto and 756/839
(90.1%) for 2048, versus 802/888 (90.3%) for IQ2. Portuguese acceptance was much
lower (53.5% / 49.3% for IQ4).

The identical near-full retrieval workload targets about 29825 input tokens.
IQ2 completed: TTFT 11.974 s, 118.1 decode tok/s, 3/3 exact records and strict JSON.
IQ4 auto was stopped after 101.17 s, and 2048 after 71.16 s, before any first token.
Both exceeded the harness's 2 GiB global swap-growth guard for three consecutive
samples. Peak swap was 8.32 / 10.80 GiB; available RAM stayed above 42.27 / 44.91 GiB.
Thus this is a global swap-growth failure, not evidence of exhausted available
RAM or GPU OOM. Global swap includes other processes, so attribution remains
incomplete. Host swappiness 180 was observed and left unchanged. GPU usage stayed
below 23 GiB/card. No full 32k or 256k claim follows from short-request success.
No 256k IQ4 profile was created after these failed guards. Auto prefill is
retained as the baseline; the 2048 trial did not resolve the limitation.

## Tool-call smoke and disposition

The bundled strict harness passed four integrity trials including SSE, plus
one clean tool-result continuation ending with `stop`. Automatic selection
passed 10/10 tool-required prompts and 10/10 no-tool controls (zero spurious
calls, zero infrastructure errors). The exact-confidence verdict is still
**inconclusive**: ten trials cannot establish the configured reliability SLO.
These tests used the 2048 prefill trial; after testing, auto prefill was restored
because 2048 provided no measured benefit. Long-context tool canaries and a
full quality/agent evaluation were not run after the near-full memory guards
failed. The profile remains experimental and not agent-qualified.

The test backend and the task-owned proxy were stopped, restoring the initially
inactive runtime state. The 270 W/card power cap was retained.

## Other quants

The local Unsloth `UD-IQ4_XS` is a different, mixed quantization. Preparation
rejects an expert group spanning shards (layer 14), and a real native diagnostic
rejects a Q8_0 expert down projection (layer 2, `down type 8`). Native embedding
lookup also only searches the first shard, which is metadata-only in this
layout (source observation). These are concrete blockers in the current path.
Its original GGUF files were retained; the failed test pack was removed.

The same mradermacher repository's IQ3_S has a promising supported layout in
remote headers (IQ3_S gate/up, IQ4_NL down/PLE), but was neither downloaded fully
nor tested in inference. Cygnal's IQ4XS-NGQ4 contains unsupported PLE Q4_0 and
Q5_1 expert down matrices. Other gated or alternate-format candidates were not
validated. Quant names alone do not establish Strata compatibility.

## Authorized cleanup

Removed Q2_0 and both Coder IQ1_M profiles, all their source shard links,
exclusive packs, tokenizers and engine/shared-settings configs. Remaining
profile/config dependency closure was checked before removal. Hardlinks shared
with retained IQ2 assets were preserved. The shared `/home/diogo/models/strata/mtp/rt`
was preserved because IQ2 and IQ4 use it. Benchmark evidence and both backend
checkouts remain. Measured free-space increase: 134,167,252,992 bytes (124.95 GiB),
leaving about197.5 GiB available. The IQ2 256k profile still validates.

## Reproduction evidence

Raw SSE, request JSON, timings, telemetry, download provenance, diagnostics,
preparation logs, failed-prefill tracebacks and the packer patch are under
`/home/diogo/models/strata/iq4-tests-20261001/`.
`results/{iq2-baseline-32k,iq4-32k,iq4-32k-pf2048}/raw.jsonl` retains every sample.
`cleanup-q2-coder-result.json` records removed paths and retained shared inodes.

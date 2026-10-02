# Strata IQ2_XS 256k calibration — 2026-10-01

Profile: `qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-256k`.
Native context 262144, no YaRN, int8 KV, equal 24/24-layer placement, mmap experts,
15 workers, MTP spec4/min-p0.7. Dual RTX3090, 270 W/card throughout.
Official non-thinking sampling: temperature0.7, top-p0.8, top-k20,
presence1.5, repetition1; template revision de4b8e4d43b917e7706784d8bb445c9af86a3540.

## Measurements

| Request | Input tokens | Decode tok/s | TTFT |
|---|---:|---:|---:|
| Warm short code |54|135.7|0.320 s|
| Fresh 50% retrieval |130826|93.5|39.98 s|
| Fresh near-full retrieval |259641|91.7|87.27 s|
| llama-bench 5%, cached |12592|135.6 ±0.7|83 ms|
| llama-bench 25%, cached |62291|137.5 ±0.4|166 ms|
| llama-bench 50%, cached |124651|134.8 ±1.4|276 ms|
| llama-bench 90%, cached |224101|125.5 ±0.1|440 ms|

Benchmark rows have warmup plus three measured repetitions. Cached TTFT does not
represent fresh document ingestion. Retrieval uses greedy sampling and three records
at 10/50/90% positions; both requests passed 3/3 and returned strict JSON.
Peak monitored VRAM: 22945/23003 MiB (22.41/22.46 GiB), below23GiB/card.
Long Portuguese generation was coherent. MTP warm code accepted180/194drafts.
23461/24576 expert slots were resident; auto PCIe fraction0.28 from13.3/13.4GB/s probes.
No transport speedup is inferred from placement.

## Calibration and tools

One-knob ABBA test of spec-min-p0.7 versus0.5, three fixed workloads per arm,
256 generated tokens and official sampling: pooled medians97.85 versus96.75tok/s.
The lower threshold did not improve performance; retain0.7. Other knobs retain
previously measured settings; this is not an exhaustive global optimization claim.

Bundled strict tool harness: integrity4/4, including streamed assembly and exact
shell command bytes. Short continuation **failed** by reissuing a successful tool
call. Auto smoke:10/10positive calls,1/10spurious controls; exact-confidence verdict
inconclusive. Not agent-qualified. Separate token-counted long canaries at130534
and235389user tokens preserved exact command arguments and both continuations
terminated cleanly. These passing samples do not erase the short-loop defect.

## Backend migration

Copied complete fork, including local fixes, to
`/home/diogo/dev/model-loader/backends/strata-fork`. Catalog executable, six prepared
engine configs (exe/cwd/expert-profile), and current registration documentation use
this copy. Original `/home/diogo/dev/Strata` preserved. All six profiles validate;
256k launched and long-tool/full llama-bench tests ran using the copied server.
Native binary SHA256:
`545cfc5f04f581c29ebcf2c1969807efb3dd1bb1ae07fa3f14f396458aba6999` (identical copies).
Added local `backend-build.sh` using a fresh build-local-sm86 directory, because
copied CMake caches refer to the original checkout. Script syntax checked; no
rebuild performed. Source97cb786/native0.1.30 plus local server changes retained.
Initial benchmark interrupted for this migration; final four-band run completed.

Raw measurements/config snapshots/scripts:
`/home/diogo/models/strata/calibration-256k-20261001`.
Baseline fresh retrieval/threshold tests ran before migration on identical binary;
long tools and completed benchmark ran from the copy. Historical32k/1M figures
are not controlled same-run comparisons. No GPU power cap changed.

# Working notes (claude-session, 2026-10-01) — evidence index

Raw evidence lives in `runs/<label>/` (attempt.json, config.json, profile.json,
engine.log, instance.log, results.json, resources.jsonl, thread-masks.json,
run-status.json, claude-summary.json). Gates in `gates/`. Plans in `plans/`.

## Access (verified 19:19 local)
- nvidia-smi OK: 2x RTX 3090, 270 W cap each, 896/15 MiB used, 50/44 C.
- /dev/nvidia* rw; proxy 127.0.0.1:4321 running, no profile loaded.
- MemAvailable 40.4 GB, swap used 6.2/16 GiB (zram zstd), swappiness 180, memlock 8 MiB.
- No flock holder (no TUI). Proxy also fronted by caddy + cloudflared (external API).
- Installed proxy/CLI binary: ~/.local/bin/model-loader built 05:56 (v0.1.0-41-gadf12cb-dirty);
  repo Go changes were NOT installed (make build only produced bin/model-loader).

## Gates on current tree
- go test ./... rc0, go vet rc0, make build rc0 (go1.27.1 toolchain).
- Python serve 94 tests OK (5 skip); tools 84 OK; probe guard 1 OK.
- Native release v3 20261001T222137Z-3430cc1a87e0: 40/40 ctest incl. file_expert_source_dual_resident
  executed on both GPUs (PASS, 0.58 s); external_fixture (ple_parity, platform_memory_test) excluded.
- Native release v4 20261001T223017Z-f85ddc896414 (= v3 + opt-in prompt thermal gate): 41/41 incl.
  thermal_gate_test and dual fixture. Binary sha256 a3c58bbe971914a27dba62d84fcfe7078207687656a2a7b8c542c5c6ee428cfd.
- NVML path checked against nvidia-smi incl. CUDA_VISIBLE_DEVICES remap.
- Catalog: strata-perf-v3-20261001, strata-perf-v4-20261001 added; strata-fork untouched.

## Findings
- v3/v2 binaries differ in hash although engine src/include identical (CMake/test/tooling diffs).
- Decode tok/s differences between single starts track output trajectory/acceptance, not knobs;
  code-0 ~107-109 everywhere. Use ms/window and ms per avg-T slot.
- PLE8 (v2) complete: 7/7 requests, 0 guard strikes; only the matrix runner DONE line missing.
- Matrix v4: spec2 98.7 tok/s (19.45 ms/win, 1.92 tok/win) worse; spec6 110.1 (39.4 ms/win, 4.27) worse
  per token (9.2 vs 8.5 ms); PLE32 = ref cost (29.07 vs 29.29 ms/win); short-prompt ms/token
  PLE8/16/32 6.64/6.6-7.0/6.67 -> no change. Spec4/PLE16 kept.
- MT_MIN=1: 118.3/117.7 vs ref 116.4-118.5; ms/win same; outputs still diverge between starts
  (code-2, PT) -> no default change.
- Thread masks: host CPU0; 15 pool workers on CPUs 1..15; 8 request helpers 1-15,17-31 (S08 fix
  effective); 2 low-activity threads (likely stdin reader + watchdog) still CPU0; 20 pre-pin threads 0-31.
- A->B->A at 21022 tokens: a2 cache_n=0, 12.3 s re-prefill (IQ3 dual): checkpoints do not survive
  an intervening different conversation (parking disabled with split) — expected, no defect.
- Seed 0 honored (effective_settings.seed=0); text identity cached-vs-fresh prefix not guaranteed.
- Strict harness integrity: HTTP 400 on tool_choice forced (explicit rejection, not model evidence);
  IQ3 repeats tool call after success (continuation) in all IQ3 runs.
- Guard abort observed for real: v4-iq3-ref-a swap growth 2.92 GiB during load; stop_guarded_group
  SIGKILLed only server pid 1068543 (exact config); proxy saw exit before healthy.
- S10 attribution: reclaim at load. ref-a start Cached 41.1 GiB (after IQ2 run): steal_file 42.5,
  steal_anon 2.94, pswpout 2.89 GiB; native VmSwap 170 MiB + server 149 MiB; other processes' anon
  6.9 -> 4.1 GiB; MemAvailable 45-47 GiB throughout. v3 ref start Cached 18.7: steal_anon 0.36.
- IQ2 stage-dense 256k fresh fills (v4): PASS, swap +0.004 GiB, 4/4 exact retrieval,
  235476 fresh tokens TTFT 71.3 s, all 24576 experts resident, peak 21614/22957 MiB, T 82/74 C.
- Ranking (S13) holdout A/B A,B,B,A,A,B: hit code-0 96.7 -> 98.6 %, CPU experts/layer-window
  1.10 -> 0.50, VRAM-tier exchanges ~3900 -> ~2100, ms per avg-T slot -4..-8 % early requests, PT neutral;
  all checks pass. Rerank file: ranking/orca-iq3-xxs-rerank-v1.bin (+ .json identity).
- Nsight (v4, IQ3 dual resident-stage-dense): quantize_q8_1 kernels ~1.7 % of kernel time;
  gpu_stamp (profiling instrumentation) 3.6 %; "P2P" memcpy rows are GPU1 <-> pinned host complement
  (src/dst kinds pinned/device), not GPU-GPU; handoff D2H 12 KB/window 1.8 us; tier exchanges
  ~7800 x 2.18 MB copies in ~25 s.
- 976k thermal: GPU0 85 C vs GPU1 75 C at equal ~268 W; chunk 512/1024/2048 prefill power 252-260 W
  (no relief); asymmetric split banned by operator policy -> opt-in thermal gate between prompt chunks.
- spec-min-p A/B (A,B,B,A,A,B): code median 108.8 (0.5) vs 107.5 (0.7) = tie; PT 72.2-73.9 vs 74.8-77.1
  (non-overlapping, +4-5 %). Kept 0.5 on IQ3 candidate; PT signal recorded.
- MTP prompt (STRATA_PREFILL_TIMING=1, IQ3 dual fills): after-chunk draft layer 40/272/557/1028 ms for
  1177/7732/15921/29031 fresh tokens = 2.6-5.2 % of prompt time (upper bound for batched last stage).
  29k prefill GPU timeline: dequant 19.5 %, wait copy 15.9 %, embed+steps 13.1 %, gemm g/u 11.4 %.
- Probe v1 thermal: IQ2 256k fills back-to-back -> 0.9 fill starts at ~79 C -> A1/B1/B2/A2 tripped 85 C,
  C1 84 C. Earlier B pass started from 52 C. => probe v2 cools <65 C before every fresh fill.
- Probe v2 IQ2 256k C,A,B,B,A,C,C,A,B (9 starts, 0 guard trips, 82-83 C):
  C canonical: code 123.5-124.4, TTFT .25/.5/.9 20.1-20.3/39.4-39.6/76.8-78.0, 22940/22997 MiB.
  A v4: code 122.2-123.9, TTFT 18.6-18.7/36.4-36.8/72.3-72.6, 23446 slots.
  B v4+stage-dense: code 127.7-128.3, TTFT 18.6-18.8/36.2-36.3/72.3-72.7, 21596/22957 MiB, 24576 slots.
  Native prompt 235k: C 76.7 s vs A 72.1 s; server TTFT overhead ~0.5 -> ~0.2 s.
- Proxy translates Anthropic disable_parallel_tool_use -> parallel_tool_calls=false and any/tool -> forced
  tool_choice: v4 server returns 400 for these (S03). Client-visible change on promotion.
- 2026-10-02: v5/v6 server tool policy (forced call opening; single-call limit). v5 'required' with open prefix
  let the model name a non-offered tool (exec_command vs bash) -> v6 forces the only offered tool. Tools gate
  (plans/tools-gate.md) passed on v6 (integrity 4/4, auto 30/30 0/30 = canonical). IQ2 256k canonical PROMOTED
  to v6 + --stage-dense (backup promotion/iq2-256k-20261002T072035; confirm run promoted-iq2-256k-confirm).
  v4/v5/v6 native binaries identical (a3c58bbe...). Cold-pack load with full page cache tripped the swap guard
  (v5-iq2-sd256-tools-B); warm reload clean. Machine froze ~07:34 (no Strata load; cause unknown), reboot 08:38.
- 976k r4 (v6, after reboot): fills 49552/249547/499551 in 18.6/81.2/277.7 s exact, GPU0 <=82 C (70 gate pauses);
  0.9 fill stopped by global swap guard (+3.14 GiB; external bun + browser; native cold anon swapped) and GPU0
  device memory >23552 MiB in isolated samples from desktop growth (Strata steady ~22.4 GiB). Not approved.
- GPU1 rerank (S13 on IQ3 GPU1-resident, one knob, v6): A,B,B,A,A,B (A1 -> A4 after load swap guard).
  First request hit 69.7 -> 89.7 %, CPU experts 7.44 -> 2.65, ms/win 53.1 -> 36.3, 57.4 -> 63.7 tok/s;
  code-0 68.2 -> 74.2; later requests tie. Controls (gpu1-rank-controls-summary.json): integrity 4/4 all,
  auto B >= A, harness continuation reissued 0/3 A vs 3/5 B (Fisher one-sided p~0.18). NOT promoted
  (gate criterion 3; gain only in first two requests). Base arm load swap guard 4/10, rerank 0/8; same
  33.14 GiB complement -> not attributed. Promotion prep (durable copy + backup) reverted; canonical untouched.

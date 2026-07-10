# RTX 3090 P2P Profile Optimization Design

## Goal

Update model-loader, its RTX 3090 inference skill, and active profiles to use the workstation's validated PCIe P2P path safely and to tune each backend according to profile purpose rather than one global throughput target.

## Verified rig state

- Two RTX 3090 24 GiB cards, PCIe PHB topology, NVLink inactive.
- Patched NVIDIA open driver `610.43.02` loaded on both cards.
- CUDA peer access works in both directions.
- Measured peer copies: GPU0→GPU1 13.34 GB/s; GPU1→GPU0 13.15 GB/s.
- NCCL two-GPU all-reduce completed with verified output.
- Effective boot policy: `iommu=pt pci=disable_acs_redir=0000:00:01.1;0000:00:01.3`.
- P2P does not provide NVLink bandwidth and does not eliminate backend correctness bugs.

## Optimization policy

Profiles are optimized by purpose:

- Agent/tool-calling: batch-1 decode, TTFT, prefix reuse, correct tool calls, long non-English correctness.
- Throughput: aggregate tokens or items per second at the declared concurrency.
- OCR/caption: latency and items per second with real media inputs.
- Embedding: request latency and batch throughput.
- Long-context: maximum correct context, near-full-context TTFT, and stable VRAM use.

Measured specialized variants remain separate; no universal profile replaces all use cases.

## Skill changes

The skill must describe two explicit communication states:

1. Current patched rig: CUDA/NCCL P2P enabled and validated. TP2 keeps P2P transport enabled (no `NCCL_P2P_DISABLE`). Custom all-reduce, however, CRASHES on this SM86 rig (vLLM 0.24.0, `custom_all_reduce.cuh:455`), so `disable-custom-all-reduce=true` is mandatory and kept, not a preemptive fallback (measured 2026-07-10).
2. Stock-driver or diagnostic fallback: `NCCL_P2P_DISABLE=1`, disabled custom all-reduce, and forbidden llama P2P forcing are workarounds only.

Update `SKILL.md`, `references/dual-gpu.md`, `references/vllm-sglang.md`, `references/llama-family.md`, `references/dflash.md`, `references/sndr.md`, `workflows/audit-profile.md`, `workflows/troubleshoot.md`, `workflows/full-tuning.md`, and `evals/evals.json`.

Rules that remain unchanged:

- Pin a fitting single-GPU primary to GPU1.
- Use equal llama tensor split `0.5,0.5`; asymmetric split remains prohibited.
- `row` split remains prohibited.
- Tensor split plus an external draft remains prohibited until the backend bug is independently fixed.
- Tool-calling llama profiles require symmetric `q8_0/q8_0` KV.
- Long and non-English correctness checks remain mandatory for every dual-GPU profile.

## Backend policy

### vLLM TP2

Default TP2 communication configuration:

- `tensor-parallel-size: 2`
- `distributed-executor-backend: mp`
- no `NCCL_P2P_DISABLE`
- `disable-custom-all-reduce: true` — mandatory: custom all-reduce crashes at startup on this SM86 rig (vLLM 0.24.0, `custom_all_reduce.cuh:455 'invalid argument'`, measured 2026-07-10); this is not a preemptive fallback

Interactive profiles use `performance-mode: interactivity`, low `max-num-seqs`, and bounded `max-num-batched-tokens`. Throughput profiles retain larger concurrency values.

### SGLang TP2

Keep P2P enabled and add `enable-p2p-check`. Disable NCCL P2P only when diagnosing an initialization hang. Tune radix scheduling and concurrency by workload.

### llama.cpp, beellama, and ik-llama

P2P is an A/B variable for genuine two-GPU splits. It must not cause fitting models to consume both GPUs automatically. Preserve model-class split rules: dense short-context may use tensor; MoE, external drafts, and large context default to layer. ik-llama compares `graph` and `layer` only.

### Lucebox DFlash

Treat existing layer-split and peer-access measurements as pre-P2P. Rebenchmark draft split, target layer split, and peer access. Draft split remains the baseline until measurements prove otherwise.

### Tabby, SNDR, and Unsloth

Tabby keeps its native PCIe TP backend until NCCL is measured faster on this PHB topology. SNDR inherits the vLLM P2P policy but still requires its large-prefill safety test. Unsloth remains single-GPU.

## Profile changes

Apply mechanical corrections immediately when behavior is unambiguous:

- Remove explicit P2P-disable flags from active TP2 profiles.
- Keep `disable-custom-all-reduce=true` on active vLLM TP2 profiles (custom AR crashes on SM86); remove only `NCCL_P2P_DISABLE=1` (the measured +13.5% P2P-transport win). Validate launches and real requests.
- Pin single-GPU utility profiles explicitly; utility services target GPU0 and primary models target GPU1.
- Preserve distinct 1M long-context q4-KV and 256k agent-safe q8-KV Qwythos variants; make their purposes explicit.

Do not convert layer to tensor, q4 KV to q8 KV, change context, or change speculative decoding solely from estimates. Those require measured variants.

## Model-loader changes

Add reusable profile-performance diagnostics to the validator rather than hard-coding individual profile IDs. Diagnostics are warnings, not blocking schema errors:

- TP2 vLLM/SGLang/SNDR with `NCCL_P2P_DISABLE=1` on this configured rig.
- TP2 vLLM with custom all-reduce ENABLED (missing `disable-custom-all-reduce`) — it crashes on SM86; `disable-custom-all-reduce=true` is correct and not flagged.
- Apparent single-GPU profile without an explicit CUDA device pin.
- Agent/tool-calling llama-family profile using q4 KV.

Because hardware policy is workstation-specific, these checks are enabled through configuration rather than imposed on every model-loader installation. Existing generic validation remains unchanged when the policy is disabled.

Add focused tests for policy enablement, backend applicability, compliant profiles, warning messages, and disabled-policy behavior.

## Benchmark and verification

For each changed active profile:

1. Validate the profile through model-loader.
2. Launch only through `model-loader instance start` and proxy requests.
3. Confirm the intended communication path in logs.
4. Warm the backend before recording measurements.
5. Measure the profile's declared workload.
6. Record per-GPU peak VRAM and temperatures.
7. Run a long Portuguese generation for dual-GPU text profiles.
8. Run a shell-hostile tool-call round trip for agent profiles.
9. Check kernel logs for Xid/NVRM failures.

A communication change is retained only if it is stable and does not regress the profile's purpose metric. The fallback changes only communication flags; it does not alter model quality or context simultaneously.

## Isolation and rollout

All repository changes occur on branch `optimize/rtx3090-p2p-profiles` in `.worktrees/optimize-rtx3090-p2p`. The dirty main checkout remains untouched. Active profiles under `~/.config/model-loader/profiles` are backed up before modification and changed only after repository tests pass.
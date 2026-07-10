# RTX 3090 P2P Profile Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make model-loader's dual-RTX-3090 guidance, diagnostics, and active profiles use the validated PCIe P2P path while preserving workload-specific performance and correctness.

**Architecture:** Add optional workstation policy to the existing validator; generic installations remain unchanged. Update the inference skill from a stock-driver model to an explicit patched-rig/fallback model. Apply only mechanically safe profile changes first, then retain communication changes only after launch logs and workload benchmarks prove the intended path engaged.

**Tech Stack:** Go 1.x, schemaVersion-3 JSON profiles, TOML configuration, llama.cpp-family backends, vLLM, SGLang, CUDA/NCCL, Markdown agent skill.

## Global Constraints

- Work only in `.worktrees/optimize-rtx3090-p2p` on branch `optimize/rtx3090-p2p-profiles`.
- Do not touch the dirty main checkout.
- NVLink remains inactive; PCIe topology remains PHB.
- CUDA/NCCL P2P is enabled through patched driver `610.43.02` and must be proven by logs or explicit probes, never inferred from a performance delta.
- Do not use `GGML_CUDA_P2P` unless the exact backend source or binary proves it parses that variable.
- Keep equal llama split `0.5,0.5`; never introduce asymmetric split.
- Preserve tool-calling llama KV at `q8_0/q8_0` unless the profile is explicitly long-context/non-tool.
- Every dual-GPU behavioral change requires long Portuguese generation and Xid/NVRM checks.

---

### Task 1: Configurable workstation performance policy

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/service/validator/validator.go`
- Create: `internal/service/validator/performance_policy.go`
- Create: `internal/service/validator/performance_policy_test.go`
- Modify: `internal/app/bootstrap.go`
- Modify: `docs/config.md`

**Interfaces:**
- Produces: `config.PerformancePolicyConfig` with `RTX3090P2P bool` mapped from `[performance_policy].rtx3090_p2p`.
- Produces: `validator.Options{RTX3090P2P: bool}` and `validator.NewWithOptions(logger, options)`.
- Existing `validator.New(logger)` remains generic and disables workstation warnings.

- [ ] **Step 1: Write failing config tests**

Add tests asserting absent config yields `RTX3090P2P == false` and TOML `[performance_policy]\nrtx3090_p2p = true` yields true.

- [ ] **Step 2: Run config tests and verify RED**

Run: `go test ./internal/config -run 'Test.*PerformancePolicy' -count=1`
Expected: compile failure because `PerformancePolicyConfig` does not exist.

- [ ] **Step 3: Implement config type and default**

Add `PerformancePolicy PerformancePolicyConfig` to `AppConfig`; define the bool field; set default false in `applyDefaults`.

- [ ] **Step 4: Run config tests and verify GREEN**

Run: `go test ./internal/config -run 'Test.*PerformancePolicy' -count=1`
Expected: PASS.

- [ ] **Step 5: Write failing validator policy tests**

Cover:
- disabled policy emits no workstation warnings;
- vLLM TP2 + env `NCCL_P2P_DISABLE=1` warns on `launch.env`;
- vLLM TP2 with custom all-reduce ENABLED (no `disable-custom-all-reduce`) warns — it crashes on SM86 (`custom_all_reduce.cuh:455`); `disable-custom-all-reduce=true` must NOT warn;
- SGLang TP2 shares the `NCCL_P2P_DISABLE` rule; SGLang custom-AR self-disables silently, so it is not warned;
- TP1 does not warn;
- fitting/apparent single-GPU profile with no CUDA pin warns;
- split/TP profile does not receive the single-GPU-pin warning;
- agent llama profile with q4/q4 KV warns;
- q4 long-context non-agent profile does not warn.

Use real `domain.Profile` values, not mocks.

- [ ] **Step 6: Run validator tests and verify RED**

Run: `go test ./internal/service/validator -run 'TestPerformancePolicy' -count=1`
Expected: compile failure because policy options and rules do not exist.

- [ ] **Step 7: Implement minimal policy rules**

Add `Options`, `NewWithOptions`, and `applyPerformancePolicyRules`. Infer agent purpose from tags and description using normalized keywords. Detect TP via backend-specific args. Inspect launch env as exact key/value pairs. Emit warnings only.

- [ ] **Step 8: Wire production bootstrap**

Replace `validator.New(logger)` in `internal/app/bootstrap.go` with `validator.NewWithOptions(logger, validator.Options{RTX3090P2P: cfg.PerformancePolicy.RTX3090P2P})`.

- [ ] **Step 9: Run focused tests**

Run: `go test ./internal/config ./internal/service/validator ./internal/app -count=1`
Expected: PASS.

- [ ] **Step 10: Document configuration**

Document `[performance_policy] rtx3090_p2p = true`, its workstation-specific nature, warning-only behavior, and fallback semantics in `docs/config.md`.

### Task 2: Skill regression scenarios and P2P state update

**Files:**
- Modify: `.agents/skills/rtx3090-inference-profiles/evals/evals.json`
- Modify: `.agents/skills/rtx3090-inference-profiles/SKILL.md`
- Modify: `.agents/skills/rtx3090-inference-profiles/references/dual-gpu.md`
- Modify: `.agents/skills/rtx3090-inference-profiles/references/vllm-sglang.md`
- Modify: `.agents/skills/rtx3090-inference-profiles/references/llama-family.md`
- Modify: `.agents/skills/rtx3090-inference-profiles/references/dflash.md`
- Modify: `.agents/skills/rtx3090-inference-profiles/references/sndr.md`
- Modify: `.agents/skills/rtx3090-inference-profiles/workflows/audit-profile.md`
- Modify: `.agents/skills/rtx3090-inference-profiles/workflows/troubleshoot.md`
- Modify: `.agents/skills/rtx3090-inference-profiles/workflows/full-tuning.md`
- Modify: `.agents/skills/rtx3090-inference-profiles/README.md`

**Interfaces:**
- Skill must distinguish current patched rig from stock-driver fallback.
- Skill must require log proof for P2P/custom all-reduce engagement.

- [ ] **Step 1: Add baseline eval scenarios before editing guidance**

Add scenarios for:
- auditing a vLLM TP2 profile carrying `NCCL_P2P_DISABLE=1` and custom AR disabled on the patched rig;
- proposing `GGML_CUDA_P2P=1` without proving the backend parses it;
- handling SGLang custom AR that silently self-disables;
- keeping tensor+external-draft prohibition after P2P activation.

- [ ] **Step 2: Run baseline skill evaluation**

Use the repository's existing skill-eval mechanism if present; otherwise run fresh-context subagents against the new cases without loading the skill. Record failures in the task transcript, not SKILL.md.

Expected: at least one scenario follows stale stock-driver guidance or assumes an unproven P2P path.

- [ ] **Step 3: Update core rig facts**

Replace stale P2P-disabled and vBIOS-blocked statements with verified driver, bandwidth, NCCL result, PHB limitation, and DMA-isolation risk. Keep stock fallback as diagnostic guidance.

- [ ] **Step 4: Update backend references**

Make vLLM/SGLang P2P-on the default for this rig; custom AR is an A/B decision with log proof. Mark old llama/dflash split measurements as pre-P2P. Do not claim `GGML_CUDA_P2P` support unless source/binary search proves it.

- [ ] **Step 5: Update workflows**

Audit must flag P2P-disable/custom-AR-disable in TP2. Troubleshoot must separate patched and stock paths. Full tuning must capture launch-log evidence and rollback flags.

- [ ] **Step 6: Run skill scenarios with the updated skill**

Expected: every scenario preserves backend-specific caveats, asks for/logs path engagement, and treats P2P-disable flags as fallback only.

- [ ] **Step 7: Validate skill structure**

Run JSON parse on `evals/evals.json`, inspect frontmatter, and confirm no stale unconditional `NCCL_P2P_DISABLE=1 when TP=2` guidance remains.

### Task 3: Active profile mechanical corrections

**Files:**
- External backup: `~/.config/model-loader/profiles.p2p-pre-20260710/`
- Modify selected files under `~/.config/model-loader/profiles/`
- Modify: `~/.config/model-loader/config.toml`

**Interfaces:**
- Enable `[performance_policy].rtx3090_p2p = true`.
- Profiles remain schemaVersion 3 and validate through the worktree binary.

- [ ] **Step 1: Back up active profiles and config**

Create a timestamped copy before any profile mutation. Verify file counts and checksums.

- [ ] **Step 2: Build the worktree binary**

Run: `go build -o bin/model-loader ./cmd/model-loader`
Expected: exit 0.

- [ ] **Step 3: Enable workstation policy**

Add `rtx3090_p2p = true` under `[performance_policy]` without disturbing unrelated config.

- [ ] **Step 4: Correct TP2 communication flags (measured 2026-07-10)**

Custom all-reduce CRASHES on this SM86 rig (vLLM 0.24.0, `custom_all_reduce.cuh:455 'invalid argument'`), so `disable-custom-all-reduce=true` is MANDATORY and MUST be kept. The measured P2P win comes only from removing `NCCL_P2P_DISABLE=1` (PyNCCL P2P transport -> +13.5% concurrent throughput, `isAllDirectP2p 1` via P2P/CUMEM). For active vLLM TP2 profiles: remove `NCCL_P2P_DISABLE=1`, KEEP `disable-custom-all-reduce=true`. Preserve all other parameters. Candidate files:
- `gemma-4-e4b-awq-vllm-tp2-128k.json`
- `gemma-4-e4b-awq-vllm-tp2-32k.json`
- `qwen3.6-27b-int4-autoround-dflash-vllm-tp2-192k.json`
- `qwen3.6-27b-int4-autoround-dflash-vllm-tp2-textonly-228k.json`

- [ ] **Step 5: Pin utility profiles by purpose**

Pin embedding/caption utilities to GPU0 with `CUDA_DEVICE_ORDER=PCI_BUS_ID` and `CUDA_VISIBLE_DEVICES=0`, unless measured VRAM makes GPU0 unsafe. Candidates:
- `qwen3-embedding-0.6b-32k.json`
- `joycaption-beta-one-gptq-vllm-8k.json`
- `qwen3-vl-8b-nsfw-caption-v45-vllm-16k.json`

Do not add device flags inside args because CUDA masking remaps the device.

- [ ] **Step 6: Clarify Qwythos variant purpose**

Keep the 1M q4-KV profile as long-context/non-tool and the 256k q8-KV profile as agent-safe. Rename the 1M ID only through model-loader's profile rename path so filename and served identity remain consistent. Update descriptions without inventing new benchmark numbers.

- [ ] **Step 7: Validate every active profile**

Run the worktree binary's `profile validate <id> --json` for all profiles. Expected: zero blocking errors; new workstation warnings only where intentionally retained for later tuning.

### Task 4: vLLM and SGLang P2P A/B verification

**Files:**
- Runtime profile variants under `~/.config/model-loader/profiles/`
- Benchmark outputs under configured model-loader state directory
- Update measured descriptions only after successful tests

**Interfaces:**
- A = prior fallback communication flags.
- B = P2P enabled and custom AR allowed.
- All non-communication args remain identical within each pair.

- [ ] **Step 1: Select the smallest representative vLLM TP2 profile**

Start with Gemma 32k or another already-downloaded TP2 profile that minimizes load time while exercising all-reduce.

- [ ] **Step 2: Launch P2P-enabled variant through model-loader**

Capture complete startup log. Required evidence: NCCL P2P/custom AR path engaged, or explicit backend statement showing which path was selected. Absence of a disable flag is not evidence.

- [ ] **Step 3: Warm and benchmark by purpose**

For interactive profile: batch 1 TTFT + decode. For throughput profile: declared concurrency. Collect per-card peak VRAM and temperature.

- [ ] **Step 4: Run correctness probes**

Run long Portuguese generation, tool-call probe when applicable, and inspect kernel log for Xid/NVRM.

- [ ] **Step 5: Run fallback A variant**

Restore only communication flags, rerun identical requests, and compare. Retain B only when stable and non-regressing for the purpose metric.

- [ ] **Step 6: Repeat on Qwen DFlash TP2 profiles**

Include near-full-context prompt and reasoning-token accounting. Remove reasoning parser if the accounting probe reproduces token loss.

- [ ] **Step 7: Verify SGLang OCR**

Add `enable-p2p-check`, keep P2P enabled, and measure real OCR pages at mem fractions 0.78, 0.84, and 0.88 until OOM/headroom sets the winner.

### Task 5: llama-family and Lucebox P2P experiments

**Files:**
- Temporary profile variants only; active profiles change after winning tests
- Skill measured-anchor sections after results

**Interfaces:**
- No `GGML_CUDA_P2P` experiment without parser proof.
- Tensor+external draft remains excluded.

- [ ] **Step 1: Prove or reject environment-variable support**

Search exact checked-out backend sources and binary strings/help/environment parsing for `GGML_CUDA_P2P`. If absent, record it as unsupported and do not benchmark it. Search each fork separately.

- [ ] **Step 2: Benchmark supported llama P2P mechanism**

If a real mechanism exists, compare layer/tensor at representative 128k/256k contexts using identical models and prompts. Require log evidence of peer-access activation.

- [ ] **Step 3: Verify model-class invariants**

Dense short-context may prefer tensor; A3B MoE and external-draft profiles remain layer unless measurements overturn only the performance claim without violating compatibility.

- [ ] **Step 4: Rebenchmark Lucebox peer access**

Compare draft split, target layer split, and peer-access modes at 32k and a large-context tier. Keep draft split unless layer split wins the declared metric and correctness tests.

### Task 6: Final cleanup and verification

**Files:**
- All touched repository files
- Active profile/config backups and final versions

- [ ] **Step 1: Run focused Go tests**

Run: `go test ./internal/config ./internal/service/validator ./internal/app ./internal/cli -count=1`
Expected: PASS.

- [ ] **Step 2: Run repository test suite**

Run: `go test ./... -count=1`
Expected: PASS.

- [ ] **Step 3: Build final binary**

Run: `go build -o bin/model-loader ./cmd/model-loader`
Expected: exit 0.

- [ ] **Step 4: Validate all profiles with final binary**

Expected: zero blocking errors. Document remaining warnings and why they are intentional.

- [ ] **Step 5: Verify live hardware state**

Run P2P probe, `nvidia-smi topo -p2p p`, final representative model request, and kernel Xid/NVRM query.

- [ ] **Step 6: Review changed behavior from user perspective**

Confirm agent profiles preserve tool-call correctness, utilities land on GPU0, primary profiles remain available on GPU1, and no profile description claims unmeasured performance.

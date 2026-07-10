# rtx3090-inference-profiles — development notes & changelog

Skill content lives in `SKILL.md` + `references/`. This file holds history and maintenance
notes only (kept out of the skill per user policy — nothing here loads into agent context).

## Maintenance notes

- **Version-pinned facts (re-verified 2026-07-07).** Installed builds: llama.cpp-stable
  **b9847** / nightly **b9869** (identical profile-relevant flag surface), beellama
  **main@85e22ea0 / b10102-dirty**, buun **b9792**, lucebox-hub 1b11c50, vLLM **0.24.0 @
  ee0da84ab — BOTH stock venvs** (the nightly was rebuilt from the broken editable build) +
  flashinfer 0.6.12, sndr-vllm venv **0.23.1rc1.dev424** (dev714 = upstream pin, NOT installed),
  SGLang 0.5.9 / 0.5.6-dev forks, TabbyAPI 3cf468c + exllamav2 0.3.2 / exllamav3 0.0.43,
  unsloth **2026.6.7**, ik-llama-cpp **t0002-889-g3bb0e9f0** (2026-07-09), driver 610.43.02. After any backend upgrade, re-verify: spec-type
  dialects, reasoning-parser token-accounting (vLLM), DFlash×fp8-KV (vLLM — still retest),
  the asymmetric-KV offload bug (#20866), tensor-mode×draft crashes (#22473/#24309, still open),
  DSpark availability (llama.cpp PR #25173 still open, not in b9869), and the sampling seed table.
- **Resolved this cycle (2026-07-07):** BUGS.md **S1** (chained / `draft-dflash` spec-type is
  list-valued → validates in `args`), **S2** (sglang parser enums widened to the installed 0.5.9
  detector maps + beellama/buun `spec-type` made list-valued), and the 2026-07-03
  **vllm-nightly fp8/fa2 prefill assertion** (fixed by the 0.24.0 rebuild — fp8 KV verified
  working at request time, measured 2026-07-07). The dated changelog entries below are history.
- **Measured anchors** carry dates in the reference files; carry values, not profile names.
- **User policies encoded** (do not silently change): DRY banned from profiles (2026-07-02);
  no naive CPU expert offload (`--n-cpu-moe` on explicit request only; Luce Spark is the
  sanctioned path); VRAM cap ≤46 GiB; mmproj on CPU (`--no-mmproj-offload`); English-only
  UI/schema text in the repo.
- Evals in `evals/evals.json` = **23 scenarios**: rewritten 2026-07-02 from verified premises,
  corrected + extended 2026-07-07 (S1/S2 reversal; quick-profile / audit / benchmark-compare /
  S1-regression), + the ik-llama-cpp scenario 2026-07-09, + the P2P scenarios #18–#23 (2026-07-10:
  #18–#21 measured-corrected, #22 custom-AR crash, #23 NCCL P2P +13.5% gain). Graded premises cited inside each eval.

## Changelog

### 2026-07-10 (later) — measured P2P A/Bs (Task 6 of the rtx3090-p2p optimization)
- **Corrects the inverted custom-AR premise from the Task-2 entry below.** Task 2 marked
  `disable-custom-all-reduce` as an A/B lever ("do not preemptively disable"). The measured A/Bs on this
  rig (`local://rtx3090-p2p-measured-facts.md`) invert that:
  - **custom all-reduce is BROKEN on SM86** (vLLM 0.24.0): with the flag absent, CUSTOM is selected and
    CRASHES at startup (`custom_all_reduce.cuh:455 'invalid argument'`, EngineCore dies, reproduced 2×) —
    `disable-custom-all-reduce: true` is **MANDATORY**, not a stale fallback. P2P does not fix it.
  - **NCCL P2P is a real win:** dropping `NCCL_P2P_DISABLE` (custom-AR already disabled) measured
    **+13.5% concurrent** (conc-32 2422–2444 vs 2112–2169 tok/s; log `isAllDirectP2p 1` / `via P2P/CUMEM`
    vs `isAllDirectP2p 0` / `via SHM`). Applied to `gemma-4-e4b-awq-vllm-tp2-32k`.
  - **SGLang TP2 = inconclusive** (silent self-disable, no engagement proof) → treat as NCCL,
    `--enable-p2p-check`, `mem-fraction-static 0.78`.
  - **Native single-stream splits = TIE:** llama `GGML_CUDA_P2P` (ornith-9B tensor-split, ~141 dec /
    ~4120 pref) and lucebox `--peer-access` (Qwen3.6-27B NEO-CODE 64L layer-split, ~33 dec / ~1072 pref)
    equal on/off — keep DEFAULTS.
- **Skill changes:** SKILL.md (rig fact, hard rule, 2 red flags → custom-AR mandatory, +13.5% NCCL win,
  native tie); references/dual-gpu.md §P2P (rewritten: measured results, prerequisites/diagnostics —
  `iommu=pt`, per-root-port ACS, DMA-isolation tradeoff, hugepage `cudaHostRegister` caveat; vLLM/SGLang
  TP2, GGML_CUDA_P2P, lucebox, myth); references/vllm-sglang.md (5 spots); references/llama-family.md +
  references/dflash.md (measured tie); references/sndr.md (inherits the corrected TP2 rule); workflows/
  audit-profile.md (row 15 inverted: custom-AR=true CORRECT, warn only NCCL_P2P_DISABLE + custom-AR
  ENABLED; row 16 + note); workflows/troubleshoot.md; workflows/full-tuning.md. Evals 21→23 (#18–#21
  measured-corrected, #22 custom-AR crash, #23 NCCL +13.5% gain).
- **Validator alignment:** matches the `internal/service/validator` correction (ValidatorPolicyFix) — vLLM
  TP2 no longer warns on `disable-custom-all-reduce=true`, warns when custom-AR is ENABLED; NCCL_P2P_DISABLE
  keeps its warning for both.
- **No claim beyond the measured facts;** everything above is dated 2026-07-10 on this rig.
- **Canonical copy:** edited the worktree `.agents/` copy (force-added on branch
  `optimize/rtx3090-p2p-profiles`); the live `~/dev/model-loader/.agents/` copy still needs a sync.

### 2026-07-10 — validated PCIe P2P state (Task 2 of the rtx3090-p2p optimization)
- **Rig-fact update:** the aikitoria P2P patch on driver **610.43.02** is now live and validated —
  CUDA peer access both directions, measured peer copies GPU0→GPU1 13.34 / GPU1→GPU0 13.15 GB/s,
  two-GPU NCCL all-reduce verified; boot `iommu=pt` + ACS-redirect-disable (cost: DMA isolation);
  still PHB / no NVLink. Supersedes the stale "P2P driver-disabled / blocked on GPU0 vBIOS" premise.
- **Skill changes:** SKILL.md (rig fact, hard rule, 2 red flags); references/dual-gpu.md (new §P2P
  status/engagement/verification; vLLM/SGLang TP2 → P2P-on default; lucebox `--peer-access` + split
  rankings marked pre-P2P; sanity-check + myth updated); references/vllm-sglang.md (TP2 P2P-on default,
  custom-AR A/B, launch staples + invariants); references/llama-family.md (P2P A/B-lever note + pre-P2P
  anchors); references/dflash.md (peer-access rows A/B, pre-P2P); references/sndr.md (inherits vLLM P2P
  policy, large-prefill test intact); workflows/audit-profile.md (rows 15–16: stale TP2 disable-flags,
  unproven/wrong-backend P2P claim); workflows/troubleshoot.md (patched-vs-stock split); workflows/
  full-tuning.md (launch-log P2P evidence + rollback). Evals 17→21 (#18 TP2 stale-fallback audit, #19
  GGML_CUDA_P2P proof-before-claim, #20 SGLang custom-AR self-disable, #21 tensor+draft still banned).
- **Source/binary proof (never claim P2P support without it):** `GGML_CUDA_P2P` is parsed via
  `getenv("GGML_CUDA_P2P")` in `ggml/src/ggml-cuda/ggml-cuda.cu:334,546-549` on **llama.cpp-stable
  (b9847), nightly (b9869), beellama, buun** (all built `-DGGML_USE_NCCL`, so peer access is already on
  for the NCCL path; the env grants it to the VMM copy path). **ik-llama-cpp** has NO such env — it
  auto-enables peer access via its own `ggml_cuda_set_peer_access` (`ggml/src/ggml-cuda.cu:1889,5329`);
  **lucebox** uses `--peer-access`. No backend needs a rebuild. Upstream `docs/multi-gpu.md`/`build.md`
  document the IOMMU/BIOS crash caveat.
- **TDD (writing-skills RED/GREEN):** added evals #18–#21 first; baseline fresh-context (no-skill)
  answers failed all four (kept `NCCL_P2P_DISABLE`/`disable-custom-all-reduce`, claimed `GGML_CUDA_P2P`
  "does not exist", missed the custom-AR self-disable, treated tensor+draft as merely "fragile"); with
  the updated skill all four pass. Baseline transcript lives in the Task 2 report, not here.
- **Canonical copy:** edited the worktree `.agents/` copy (force-added on branch
  `optimize/rtx3090-p2p-profiles`); the live `~/dev/model-loader/.agents/` copy still needs a sync.

### 2026-07-09 — add the ik-llama-cpp backend + references/ik-llama.md
- New catalog backend **`ik-llama-cpp`** (kind `ik-llama-cpp`, ikawrakow fork
  `t0002-889-g3bb0e9f0`, exe `backends/ik_llama.cpp/build/bin/llama-server`) — catalog now **17**
  entries (also added 2026-07-09: `vllm-dflash`, `vllm-dspark`). Shares llama-server's arg builder
  (`buildLlamaArgs`); schema is **hand-curated** (`backendschema/curated_ik.go`, `source.editable`)
  because ik's `--help` uses the pre-`arg.cpp` format `llamahelp` cannot parse — the binary
  `--help` is never executed and the curated set is a deliberate subset (absent flags → extraArgs).
- New reference **`references/ik-llama.md`** (ik deltas over llama-family): MLA (`-mla` 0–3, def 3),
  fused-MoE/up-gate/mul-multiadd (def on), `-ser` expert reduction (REAP-from-CLI), `-rtr` repack
  (implies `--no-mmap`), graph-reuse (def on), DSA (GLM-DSA arch), `split-mode` `none/graph/layer`
  only (**no tensor/row**), extended KV (`bf16`/`q6_0`/`q8_KV`), the `cache-ram` RAM prompt cache
  (NOT `cache-reuse`), `--fit` default-off, free-string `spec-type` in the bare `mtp`/`dflash`
  dialect, `--peg` a no-op in this build.
- SKILL.md: reference row, dispatch row + count 14→17 (+ the two new vllm ids), routing clause,
  two Common-mistakes rows, one Red flag. speculative.md: ik per-engine section.
  workflows/quick-profile.md: ik in the llama-family defaults. evals: +1 ik scenario (17).
- Grounded in `curated_ik.go`, ik `common/common.cpp` (`--peg` no-op, `-rtr`→no-mmap,
  graph-reuse/fused defaults), `common/sampling.h` (temp 0.8 / top-k 40 / top-p 0.95 / min-p 0.05
  — the curated schema's 0.9/0.1 annotation is cosmetic), and `docs/parameters.md`.

### 2026-07-07 — S1/S2 fixes, version re-pins, 5-workflow restructure, evals 12→16, E2E proof
- **model-loader code (BUGS.md S2):** sglang `reasoning-parser`/`tool-call-parser` curated enums
  widened to the union of the 4 installed sglang 0.5.9 detector maps (23 reasoning / 30 tool-call;
  `qwen3_coder`/`glm47`/`qwen3-thinking` validate in `args`) + English HelpText; beellama/buun
  `spec-type` switched `enumFlag`→`listEnumFlag` (matching llama.cpp) so `dflash,ngram-mod` chains
  validate in `args`. Schemas refreshed for the affected catalog ids. (S1 was already fixed in
  `validator/rules.go`.) Recorded as BUGS.md S2 + AGENTS.md mirror.
- **S1 reversal across the skill:** every "chained spec-type / `draft-dflash` / sglang parser is
  blocked by the curated enum → extraArgs / host on vLLM" claim is REVERSED — they validate
  directly in `args`; extraArgs is only for genuinely schema-absent flags. The
  `draft-mtp,ngram-mod` chain is now the recommended agent config on llama.cpp-stable.
- **Version re-pins:** nightly b9847→b9869, beellama v0.3.1→main@85e22ea0/b10102, vllm-nightly
  broken editable→0.24.0 (rebuilt; fp8 KV verified), sndr dev714→installed dev424, unsloth
  pinned 2026.6.7. All measured anchors preserved verbatim (diffed against the pre-overhaul snapshot).
- **Restructure:** SKILL.md is now a **router (<300 lines)** with a 5-workflow dispatch table; the
  linear 12-step workflow moved to `workflows/full-tuning.md`; added `workflows/{quick-profile,
  troubleshoot,benchmark-compare,audit-profile}.md`. references/ gained "Tuning by model type"
  deepening sections (llama-family, vllm-sglang, exllama-tabby; dflash verified). Draft-KV quant
  corrected to the canonical schema keys `cache-type-k-draft`/`-v-draft` in `args`.
- **Evals 12→16:** stale premises corrected; added quick-profile (13), audit (14),
  benchmark-compare (15), S1-regression (16). **With-skill 16/16 pass (100%) vs old-skill 8/16
  (77%)** — viewer `rtx3090-inference-profiles-workspace/iteration-1/review.html`.
- **E2E proof (one real on-rig case per flow, evidence in `…-workspace/e2e/`):** quick = Agents-A1
  (validate+launch+round trip, calibration-pending); full-tuning = Qwopus3.6-27B-Coder-MTP (the
  S1 `draft-mtp,ngram-mod` chain validated live + MTP head present + ~71% draft acceptance +
  194 tok/s llama-bench + clean shell-hostile tool call); troubleshoot = qwythos q8_0 agent-safe
  variant (clean tool call, original untouched); benchmark-compare = ornith-aeon vs ravenx (both
  100% codegen; tensor buys ~nothing on A3B); audit = 12-profile findings report.

### 2026-07-03 — add the SNDR/Genesis `sndr-vllm` backend + `references/sndr.md`
- Materialized & validated the `sndr-vllm` backend (SNDR Core Engine "Genesis" runtime
  patch-overlay for vLLM, kind `vllm`, pinned vLLM `dev424` in `backends/sndr-vllm/.venv` —
  a THIRD vLLM venv alongside the two 0.24.0 stock ones). New reference `references/sndr.md`;
  SKILL.md updated: backend table 13→14, dispatch line, reference-files table, four Common-
  mistakes rows, one Red-flag, one dispatch clause.
- New reference encodes the facts that cost real debugging (all in-repo as BUGS.md N1–N3):
  - **Pinned wheel ages out of the rotating `wheels.vllm.ai/nightly`** → install from the
    persistent per-commit URL via `SNDR_WHEEL_INDEX` (N1).
  - **`GENESIS_ENFORCE_VERSION_RANGE=1` is mandatory** in every SNDR profile `launch.env`
    or version-capped patches (e.g. PN30, obsolete on dev424) hard-fail (N2).
  - **The large-prefill trap (headline gotcha):** `turboquant_k8v4`'s continuation-prefill
    scratch (patch P38) OOMs at util 0.84 / crawls 1.4 tok/s at 0.78 on ≥13k-token prefills
    at 262144 on the 24 GiB desktop card. The `-sndr` 262144 profiles are **short-prompt /
    high-concurrency only** (~145 tok/s, 2.47× KV vs 1.94× stock fp8); big-prompt agents need
    a ≤131072 variant or stock fp8 (N3). Short "capital of X" tests masked it — llama-bench it.
  - `--language-model-only` (text-only) frees vision VRAM: lets the 35B-A3B-FP8 reach 262144
    (else ~69k) and is required for the multimodal gemma-4 profiles to boot; kept vision on
    the fitting 27B for a fair A/B vs the stock vision profiles.
- Six profiles added to the library (AGENTS.md §9 rows 25–30); env matrix generated from the
  SNDR ModelDef YAMLs (`backends/sndr-vllm/sndr_core_engine/sndr/model_configs/builtin/model/`).
- **A/B result (KV concurrency, measured both on shawnw3i/Qwen3.6-27B-AWQ-MTP @262144):**
  SNDR `turboquant_k8v4` = **646,993 tokens / 2.47×** vs stock `fp8_e5m2` = **496,462 / 1.89×**
  → **+31% KV** from TurboQuant. `llama-bench` (13k–65k prefills) breaks BOTH profiles at
  262144 on the 3090 (SNDR = P38 continuation OOM; stock = the FlashInfer assertion below),
  so the win is a short-prompt / high-concurrency one, not a big-prompt one.
- **⚠ `vllm-nightly` backend found broken 2026-07-03 (two independent issues; affects the 6
  stock vLLM profiles):**
  1. **venv path (FIXED):** `backends/vllm-nightly/.venv` was created as `backends/vllm/.venv`
     and later renamed — venvs aren't relocatable, so `activate` + 43 `bin/` scripts hardcoded
     `backends/vllm/.venv` (which doesn't exist) → the wrapper's `exec vllm` gave `vllm: command
     not found`. Repaired in place: `sed -i 's|backends/vllm/\.venv|backends/vllm-nightly/.venv|g'`
     over the 43 text files (`bin/vllm` itself already had the correct shebang). Re-verify after
     any venv move: `source .venv/bin/activate && which vllm`.
  2. **FlashInfer fp8/fa2 assertion (OPEN):** the current editable build is **`0.1.dev1+ge24d1b24f`
     (NOT 0.24.0** — the earlier "0.24.0 both venvs" fact was stale). It asserts `fp8 tensor core
     is not supported in fa2 backend` (flashinfer `gen_batch_prefill_module`) on ANY prefill with
     `kv-cache-dtype fp8_*` → 500 on all inference (short prompts too). The stock fp8 profiles
     were calibrated 2026-06-29 and worked, so this is a build/flashinfer regression. Candidate
     fixes (untested, user's daily driver — ask first): `VLLM_ATTENTION_BACKEND=FLASHINFER` or
     `TRITON_ATTN`, or fp16 KV, or rebuild the nightly to a good commit.

### 2026-07-02 (later still) — sampling: calibrated temp + top-n-sigma holds MTP acceptance
- Calibrating `ornith-aeon-35b-a3b-q4km-mtp-vision-layer2-256k` surfaced a real distinction the
  protocol had blurred: a card's **usage-example** sampling (`--temp 0.6`) is usually the
  "precise/single-shot coding" preset and frequently disagrees with `generation_config.json` AND
  the benchmark methodology. Going to the **source-model card** (`deepreinforce-ai/Ornith-1.0-35B`,
  via `base_model:` links) showed the headline agentic-coding scores (Terminal-Bench 2.1,
  SWE-Bench Verified/Pro, SWE-Atlas) were all produced at **temp 1.0, top_p 0.95–1.0**, and
  `generation_config.json` ships `temperature: 1.0`. The downstream GGUF/base cards' temp-0.6
  examples are the ClawEval/single-shot preset only.
- **Measured finding (the load-bearing one):** raising temp to the calibrated 1.0 on a speculative
  profile normally collapses MTP/DFlash acceptance — BUT `--top-n-sigma 1.0` (llama.cpp) holds it
  flat: Ornith AEON 35B-A3B MTP acceptance **0.62 @ temp 1.0+top-nsigma vs 0.63 @ temp 0.6** (no
  regression). So the old rule "speculative profiles use the low end (Qwen3.6+MTP → 0.6)" is now:
  on llama.cpp/beellama run the **calibrated temp 1.0 + top-n-sigma 1.0** for 2026-gen agentic-coding
  models (Qwen3.6, Ornith, GLM-4.7+) at zero speed cost; reserve 0.6 for single-shot or for engines
  without top-n-sigma (vLLM/SGLang/dflash).
- Applied: protocol §1 (added step 5 — read the source card's benchmark-methodology section; added
  the usage-example≠calibrated warning), §2 (Ornith row), §3 (rewrote the spec×sampling rule around
  the measurement; connected the top-nsigma bullet), §5 (agentic-default Qwen3.6 preset → temp 1.0
  + top-n-sigma; added an Ornith preset), speculative.md (Sampling × speculation rewritten),
  eval#1 (expects calibrated 1.0 + top-n-sigma, not a blind 0.6 copy). Synced to ~/dev/skills.

### 2026-07-02 (later) — S1 correction: the validator blocks stale curated enums, extraArgs can't override
- While optimizing `ornith-aeon-35b-a3b-q4km-mtp-vision-layer2-256k`, the full-rebuild claim below
  ("chained `spec-type` / `draft-dflash` → extraArgs (warning only)") was proven **wrong**. The
  validator enforces a curated `FlagTypeEnum` on the known flag in **args AND extraArgs**
  (`profile validate` exit 2, Severity 1); only *unknown* flags escape via extraArgs (warning-only,
  `rules.go:202`). So `draft-mtp,ngram-mod` and `draft-dflash` are **unreachable through
  model-loader** today — verified against the b9847 binary `--help` (comma-list) and the on-disk
  schema (`Type:4`, no `draft-dflash`).
- Same defect class confirmed for sglang `tool-call-parser`/`reasoning-parser` (stale enums lacking
  `qwen3_coder`/`glm47`/`qwen3-thinking`). Filed as **BUGS.md S1** (Open, Medium) with a general fix
  proposal (model list/enum properly + make extraArgs a real passthrough).
- Corrected in the skill: SKILL.md, references/speculative.md, references/llama-family.md,
  references/model-research.md, evals/evals.json — all now ship plain single-value `spec-type` and
  cross-reference S1; the sglang parser note points such models to vLLM instead.

### 2026-07-02 — full rebuild (web research + local verification)
- **12-agent web-research sweep** (~1.8M tokens, 800 tool calls) + verification of every
  load-bearing claim against the installed venvs/checkouts and live schemas.
- **New:** `references/model-research.md` — mandatory per-model research protocol (HF card →
  generation_config.json → unsloth), per-family official sampling table (2026-07 snapshot),
  coding-agent calibration + anti-loop policy, chat-template/tool-call verification checklist
  (shell-hostile round trip, reasoning accounting), proxy sampling-precedence facts
  (Anthropic route forwards only temp/top_p/top_k).
- **New:** `references/speculative.md` — cross-engine spec-decode landscape: MTP, DFlash
  (now upstream in llama.cpp AND vLLM AND exllamav3), **DSpark** (DeepSeek DeepSpec 2026-06-27;
  identified, not yet runnable on any installed backend — adoption path documented), EAGLE-3
  (incl. Ex0bit agentic head for Qwen3.6-27B), ngram/prompt-lookup, suffix decoding
  (arctic-inference, not installed), per-family asset inventory.
- **dflash.md** rebuilt as the lucebox stack file: DFlash+DDTree plus **PFlash** (speculative
  prefill, 10.4× TTFT @128K), **KVFlash** (bounded KV residency, flat 38.6 tok/s @256K),
  **Luce Spark** (calibrated MoE expert offload — 35B-A3B in 13.3 GiB @ ~100 tok/s), disk
  prefix cache; `--fa-window 0` mandate for agents; `--prefill-compression` = lossy, banned
  for agents. ("dspark" in the user's request turned out to be TWO things: DeepSeek DSpark
  and Luce Spark — both covered.)
- **llama-family.md** updated to b9847: `--fit` default-on (and why we pin ngl 99 anyway),
  cache-reuse/cache-ram/checkpoints agent caching + silent disablers, kv-unified auto-slots,
  ubatch 2048 prefill guidance, chainable `--spec-type draft-mtp,ngram-mod`, upstream
  DFlash/EAGLE-3, removed-flag migration warnings, dialect table (upstream vs beellama vs buun),
  KV q8_0 rule for tool calling, DRY-ban baseline.
- **vllm-sglang.md** updated to vLLM 0.24.0 (attention auto-selection, VLLM_ATTENTION_BACKEND
  env removed, performance-mode interactivity, kv-offloading, xxhash APC, TurboQuant KV,
  fp8-checkpoints-as-Marlin-W8A16, sleep mode, ngram prompt_lookup_min≥8 tool-call rule) and
  SGLang 0.5.9 (hybrid-arch radix/overlap/spec conflicts — why Qwen3.6 agent profiles live
  elsewhere; HiCache; deterministic-mode triton mandate; spec-v2).
- **exllama-tabby.md** updated to exllamav3 0.0.43: tabby registered in catalog, MTP embedded
  in ≥0.0.41 quants, TP+draft coexistence fixes, cache-mode k,v guidance (8,8/6,5; never 4,4),
  tool-format mandate, turboderp branch convention, Gemma4 no-TP, Ampere status.
- **dual-gpu.md** updated: split-mode tensor upstream status (PR #19378; draft crashes still
  open; prefill regression), measured BAR asymmetry on this rig (GPU0 256 MiB / GPU1 32 GiB)
  and the aikitoria 610.43.02 P2P patch path (+10–30% measured elsewhere; blocked on GPU0
  vBIOS), custom-allreduce self-disable (flag now cosmetic), EP-off-for-A3B rule, pin-per-GPU
  as the agent default, power 275 W/220 W update, GDDR6X temp tooling.
- **SKILL.md** rebuilt: 13 real catalog ids (old llama-cpp-default/vllm-default/sglang-default
  removed; buun-rtx3090 and tabby ARE registered), mandatory model-research step, quant
  decision table (UD/imatrix > static; FP8-as-W8A16 nuance; int4-MoE hazard), agent-readiness
  step (tool-call round trip), rewritten mistakes/red-flags.
- **evals/evals.json** rewritten from scratch (12 scenarios) — old set had stale premises
  (eval#6 assumed buun unregistered; eval#8 claimed splits never speed decode, contradicted by
  measured +22…39% tensor / +25…32% exllama TP).
- **Testing (GREEN/REFACTOR):** 3 application tests with fresh subagents (DSpark request,
  dual-GPU-for-DFlash, Claude-Code profile) — all passed on the key points. Premise validation
  against live schemas caught 3 real defects, fixed in place: chained `spec-type` /
  `draft-dflash` fail the curated enum (→ extraArgs), `cache-ram` absent from llama.cpp schema
  (→ extraArgs), sglang parser enums stale (`qwen3_coder`/`glm47`/`qwen3-thinking` → extraArgs);
  also confirmed beellama `spec-draft-model`/`spec-draft-ngl` are valid schema aliases.

### earlier
- 2026-06-30: tensor-split speed findings folded in (Qwythos-9B 1M ctx; context-sensitivity).
- 2026-06-29: lucebox dflash multi-GPU (draft-split) + EXL3 TP measurements.
- 2026-06-28: initial dual-GPU rewrite (was single-3090 skill).

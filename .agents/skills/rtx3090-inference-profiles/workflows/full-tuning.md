# Full tuning — maximum performance from one model

**Entry:** the user wants the most tok/s / ctx / quality out of a *specific* model, **or** a
quick-profile output whose `description` ends "calibration pending" needs promotion.
**Exit:** the profile's `description` records **quant · KV types · ctx · placement · per-card peak
GiB · tok/s (+acceptance % if speculative) · sampling preset + source · template source** — every
number measured, or explicitly marked "calibration pending". Nothing estimated is presented as
measured.

This is the deep-calibration spine. It leans on the router's shared sections
(`SKILL.md` §Placement decision, §Backend dispatch, §Quant choice, §Context adjustment rule,
§Hard rules) and pulls per-backend knowledge from `references/` — it never re-derives what a
reference already owns. OOM ladders in particular live in the backend files: **link, never
duplicate**.

## The 12 steps

Run top to bottom. Steps 1–8 are pre-launch; 9–12 are the measured loop.

1. **Gather.** Pin down: model (local path **or** HF repo + file, and its size), backend (or
   "unspecified" → resolve at step 3), target ctx, **bias** (`precision` | `balanced` |
   `max-context`), and placement (`SKILL.md` §Placement decision — do this decision *first*, it
   drives everything downstream). Not on disk yet? GGUF →
   `model-loader model download <repo> <file> --wait`; safetensors/AWQ/GPTQ/EXL3 →
   `hf download <repo> --local-dir /home/diogo/models/huggingface/<org>/<repo>`. Never write a
   profile whose weights aren't present (`os.Stat` fails at launch).

2. **Model research** (`references/model-research.md` — mandatory, every new model). Fetch and
   record with sources: the official **sampling preset** for the intended mode (§1 lookup order:
   source-model card > benchmark methodology > `generation_config.json` > unsloth > community;
   seed numbers in §2); **template source + drift check** (§4 step 1 — GGUF-embedded vs the HF
   `chat_template.jinja`; drift → save the official one, `--chat-template-file /abs/path.jinja`);
   **tool/reasoning parser** (§4 quick matrix); **speculative assets** — does the quant actually
   ship the tensors, not just the config field? MTP/nextn head, DFlash drafter, EAGLE-3 head
   (`references/speculative.md` §Asset inventory by family); and **quant availability**. llama.cpp
   ignores `generation_config.json`, so its sampling MUST land in `args` (§Hard rules).

3. **Resolve backend.** Confirm the id in `~/.config/model-loader/backends/catalog.json`, then
   **read its live schema** `~/.config/model-loader/backends/schemas/<id>.json` before writing any
   flag. Backend unspecified → dispatch via `SKILL.md` §Backend dispatch. Unknown args are a hard
   ERROR; the schema is the source of truth, not memory.

4. **Format check.** GGUF ↔ llama family (`llama.cpp-*`, `beellama-rtx3090`, `buun-rtx3090`) or
   `lucebox-dflash`; safetensors/AWQ/GPTQ/FP8 ↔ `vllm-*`/`sglang-*`; EXL3 model **dir** ↔ `tabby`.
   Mismatch → propose the matching backend and re-resolve (step 3) before continuing.

5. **Placement + VRAM budget.** Estimate against the placement decision from step 1:
   single-GPU (weights + target-ctx KV ≤ **23 GiB**, pin GPU1) vs pooled (~46 GiB, split). Reserve
   **~5–8 GiB/card** for vLLM/SGLang CUDA graphs, **~0.5–1.5 GiB** for llama.cpp compute buffers
   (`references/llama-family.md` §VRAM budget). **Estimates only pick the starting point —
   `nvidia-smi` decides.** Multi-GPU or pin-per-GPU → read `references/dual-gpu.md` first, then the
   one backend file. Symmetric `0.5,0.5` splits only; single-GPU pin =
   `CUDA_DEVICE_ORDER=PCI_BUS_ID` + `CUDA_VISIBLE_DEVICES=1` (§Hard rules).

6. **Context adjustment** (`SKILL.md` §Context adjustment rule). Settle final ctx + quant +
   placement together: target ctx is a band (−25% / +50%, only across a real cliff, always say
   so). Cap at native ctx unless the user opts into RoPE/YaRN; never silently below 75% of the
   request. Cross-check the quant tier against `SKILL.md` §Quant choice for the size/placement.

7. **Write the profile.** `model-loader profile create --id <id> --backend <backend-id> --file -`
   (feed the JSON on stdin). Id regex `^[a-z0-9]+([._-][a-z0-9]+)*$`, most-significant segment
   first (model-loader `AGENTS.md` §5 Profile naming convention); **omit `meta` and `port`**
   (reserved/stripped). Put in
   `args`: the step-2 sampling + anti-loop flags (llama-family: `repeat-penalty 1.05`,
   `repeat-last-n 256`; **DRY banned**), the template flag if drifted, the speculative `spec-type`
   (list-valued enum — the `draft-mtp,ngram-mod` chain and `draft-dflash` validate directly in
   `args`; beellama v0.4.0 uses upstream `draft-dflash`/`draft-mtp`, only buun spells it
   `dflash`/`mtp`), and the backend's **agent-serving** flags
   (llama.cpp: `cache-reuse 256`, `ubatch 2048` — `references/llama-family.md` §Tuning by model
   type; vLLM: `performance-mode interactivity`, `served-model-name` = profile id —
   `references/vllm-sglang.md` §Both backends: profile musts).

8. **Validate.** `model-loader profile validate <id>` must pass. Unknown-arg error → re-read the
   schema, fix the spelling (never invent a flag). Binary-real but genuinely schema-absent →
   `extraArgs` (raw passthrough: unknown-flag warning, never enum/type-checked). `extraArgs` is
   only for schema-absent flags — nothing valid is enum-blocked (§Hard rules).

9. **Launch + calibrate.** `model-loader instance start <id>`, then
   `model-loader instance logs <id> | tail -50` and confirm: **model loaded**, **drafter/MTP head
   loaded** (config fields lie — the head *should* appear in the log; but llama.cpp-stable logs
   sparsely at default verbosity, so if no draft/MTP line shows, raise `--log-verbosity`/`lv`, query
   the OpenAI endpoint directly for `timings.draft_n` / `draft_n_accepted`, or use a benchmark;
   Claude Code's Anthropic route does not preserve `timings`. The GGUF's `nextn_predict_layers` /
   `blk.N.nextn.*` tensors prove the asset exists), the **KV line** (types + pool
   size), the **chat format** (`peg-native` good, `Generic` = no tool syntax), and **which GPUs**
   are in use. Then `nvidia-smi --query-gpu=index,memory.used --format=csv` on **BOTH** cards.
   Fire **ONE near-full-ctx prompt** (prefill grows the pool — idle fit ≠ fit) and re-check
   `nvidia-smi`. **Warm-measure** (first vLLM request ≈ half speed). Multi-GPU → also a **long,
   non-English generation** (silent corruption bugs). Under budget (>1.5 GiB free on the binding
   card) → raise ctx; over budget → the resolved backend's **OOM ladder**
   (`references/llama-family.md` §OOM sacrifice ladder · vLLM/SGLang → `references/vllm-sglang.md` ·
   dflash → `references/dflash.md` §Tuning on this card · tabby → `references/exllama-tabby.md`
   §Gotchas). **ONE change per iteration**, re-measure.

   **TP2 / split P2P evidence + rollback (patched rig, measured 2026-07-10):** on a two-GPU
   vLLM/SGLang/split-llama profile, capture the launch-log transport line — vLLM `isAllDirectP2p 1` /
   `0->1 via P2P/CUMEM`, or the llama `GGML_CUDA_P2P` / lucebox `--peer-access` peer line. **Absence of a
   disable flag is not proof.** The measured picture: **vLLM TP2 dropping `NCCL_P2P_DISABLE` = +13.5%
   concurrent (keep it dropped); `disable-custom-all-reduce: true` is MANDATORY on SM86 (custom AR crashes,
   `custom_all_reduce.cuh:455`) — never A/B it off; native single-stream `GGML_CUDA_P2P`/`--peer-access`
   splits tie (keep defaults).** Rollback in the description: re-add `NCCL_P2P_DISABLE=1` (init hang /
   corruption only — it costs the +13.5%). Keep an `NCCL_P2P_DISABLE`-drop only if warm A/B is
   non-regressing (dual-gpu.md §P2P); it never relaxes the tensor+draft / row / asymmetric bans.
   For a rigorous one-knob A/B use `scripts/benchmark-p2p-matrix.sh --profile-a <A> --profile-b <B>
   --label ... --runs 5` (refuses >1-knob diffs, warmup-discards, A,B,B,A,A,B order, per-GPU telemetry,
   restores the initially-loaded profile on exit) -> `scripts/summarize-p2p-matrix.py <out-dir>` for
   per-band medians/ranges plus the equal-weight geometric mean of all candidate/baseline median
   throughput ratios. `decision.overall`/`performance_winner` is the aggregate performance result;
   `promotion_status: pending_external_guardrails` means benchmark data alone cannot authorize a
   canonical change. Concurrent throughput needs `scripts/openai-concurrency-probe.py` (serial
   llama-bench cannot show the vLLM concurrent win).

   **Tensor-split promotion gate (embedded MTP/nextn or dense candidates only - external drafts
   stay `layer`, no A/B).** A `split-mode: tensor` variant replaces the `layer` canonical ONLY when
   a one-knob A/B (above) clears EVERY gate; a successful load proves compatibility, never a
   promotion signal, and MoE defaults to single/layer:
   - **Speed:** equal-weight geometric mean of the raw per-band median ratios is >=1.05, with complete
     expected samples and zero errored/malformed runs. Per-band range overlap is reported as a
     variance caveat, not a veto. Aggregate regression >=5% selects the baseline; sub-5% is a tie.
   - **TTFT/prefill:** no TTFT regression at any measured fill; the 90% benchmark preset covers
     near-full-context prefill (tensor can regress here - check it).
   - **VRAM:** peak <=23 GiB/card (<=46 GiB total), no cap breach at near-full ctx.
   - **Draft:** MTP/nextn still engages — direct OpenAI `draft_n`/`draft_n_accepted`, backend logs,
     or benchmark evidence retained; no acceptance collapse.
   - **Correctness:** shell-hostile tool call, long non-English generation, and (vision profiles) a
     real image request pass; the probe backend log has no graph/assert/fatal fault.
   A failed guardrail or aggregate non-win keeps `layer`; overlapping ranges alone do not.

10. **Agent-readiness** (any tool-calling / coding-agent profile — non-negotiable for those).
    Run `references/model-research.md` §4 through the proxy: the **shell-hostile tool-call round
    trip** (`mkdir -p "/tmp/My Project/sub dir" && echo "a && b > c" > "…/x y.txt" 2>&1` survives
    byte-for-byte, `finish_reason:"tool_calls"`, `arguments` a JSON string, nothing leaked into
    `content`), **tool-result continuation**, **reasoning accounting** (no tokens eaten by the
    parser), and **stop behavior**. A profile that corrupts that command — spaces injected in
    paths, `&&`→`and` — is **not done**, regardless of tok/s.

11. **Benchmark.** `model-loader benchmark --profile <id> --mode llama-bench` for tok/s + TTFT.
    `precision` bias → also run one **quality** mode (e.g. `codegen-bench` / `judge` — solve-rate
    is the primary metric). Benchmark reloads the instance (pid change is harmless). Deeper A/B
    across profiles → hand off to `workflows/benchmark-compare.md`.

12. **Record.** Update `description` with the exit contract: **quant · KV types · ctx · placement ·
    per-card peak GiB · tok/s** (+ **acceptance %** if speculative) · **sampling preset + source** ·
    **template source**. For split profiles also record the single-vs-split delta. Any number you
    did not actually measure → say **"calibration pending"** — never present an estimate as
    measured.

## Calibration loop rules

The scattered discipline that makes step 9 (and the whole loop) trustworthy:

- **ONE change per iteration, then re-measure.** Two knobs at once means you learn nothing.
- **Warm-measure.** The first request after a (re)load is cold — first vLLM request ≈ half speed,
  llama.cpp torch-free but still cache-cold. Discard it; measure the second.
- **Near-full-ctx prompt, both cards.** Idle/startup VRAM lies — prefill grows the pool. A profile
  that "fits" at idle can OOM under a real prompt. Always probe both cards with `nvidia-smi`.
- **OOM ladders live in the backend files — link, don't duplicate.** Sacrifice in the order that
  file prescribes; never drop KV below `q8_0` on a tool-calling profile.
- **Multi-GPU output can corrupt silently, and P2P engagement must be proven — never assumed.**
  Validate a long, non-English generation before trusting any 2-GPU profile; confirm the transport from the
  launch log (not a perf delta), keep `disable-custom-all-reduce: true` on SM86 vLLM TP2, and note the
  `NCCL_P2P_DISABLE` rollback (`references/dual-gpu.md` §P2P). P2P is an interconnect A/B lever, not a correctness fix.
- **"CLI says not found while the proxy owns the process."** The proxy (`127.0.0.1:4321`) holds the
  live instance; a stale `instance list` can miss it. Check `curl -s 127.0.0.1:4321/_status` and
  read the log directly at `~/.local/state/model-loader/logs/<profile>-<port>.log` — don't
  conclude "crashed" from the CLI alone.
- **An open TUI holds the single-instance flock** and will block write-path CLI commands
  (`profile create/validate`, `instance start`, `backend schema refresh`). Find the holder with
  `fuser ~/.local/state/model-loader/model-loader.lock` and close it (or ask the user to) first.
- **Never run backend binaries by hand** — only `model-loader instance start`. Inference always
  goes through the proxy; the OpenAI `model` param = profile id.

When the loop stalls or a symptom appears (won't load, loops, corrupt tool calls, spec won't load,
split issues) → switch to `workflows/troubleshoot.md` for the symptom-first ladder, then return
here to finish recording.

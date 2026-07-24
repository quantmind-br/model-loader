# Troubleshoot a profile — symptom-first diagnostic ladders

**Entry:** a named, existing profile **and** a symptom (OOM, loops, corrupt tool calls, spec won't
load, split garbage, proxy confusion). **Exit:** the symptom's own verification (bottom of each
class) passes on the same rig invariants the profile must honor.

Do NOT rebuild a profile from scratch here — that is workflows/full-tuning.md. This workflow finds
the one wrong knob and changes it, one step per iteration, re-measuring after each. Rig facts,
placement, hard rules, quant/ctx rules, and the mistake/red-flag tables are shared — pull them from
SKILL.md (§The rig, §Placement decision, §Hard rules, §Quant choice, §Context adjustment rule,
§Common mistakes, §Red flags). OOM ladders and per-backend detail live in the reference files —
link, never restate.

## Shared diagnostic reads — do these first, every class

Gather the same four before forming any hypothesis; a symptom named without them is a guess.

1. **The launch log.** `model-loader instance logs <id> -f` (or read the file directly:
   `~/.local/state/model-loader/logs/<profile>-<port>.log` — find the port with
   `ls ~/.local/state/model-loader/logs/ | grep <profile>`). Walk the **launch-log checklist** →
   references/llama-family.md §Gotchas ("Launch log lines to read every time": `n_ctx_seq < n_ctx_train`,
   KV buffer sizes, `Chat format: peg-native`, draft/MTP/dflash load + acceptance lines,
   `common_fit_params` — did `--fit` touch something you meant to pin?).
2. **Both cards, at peak not idle.** `nvidia-smi` — read GPU0 **and** GPU1. Idle fit ≠ runtime fit;
   the CUDA compute pool grows with `ubatch × ctx` on a real prompt (references/llama-family.md
   §VRAM budget). `instance metrics <id> -w` gives the live tok/s window.
3. **What is actually loaded.** `curl -s 127.0.0.1:4321/_status` → `loaded_profile_id`, `loaded_pid`,
   `loaded_port`, `inflight_requests`, `last_error`. Confirm the proxy is serving the profile you
   think you are debugging *before* trusting any client-side symptom.
4. **Re-run the model-research template check** (references/model-research.md §4) if the profile is
   new or the GGUF was re-downloaded since last calibration — a stale template mimics half these
   symptoms.

## Decision rule — fix in place vs. create a variant (decide BEFORE editing)

- **Fix in place** when the profile is **unambiguously wrong for its own stated purpose** — e.g. an
  agent/tool-calling profile with `q4_0` KV (violates the KV-q8_0 rule), an asymmetric
  `tensor-split`, a missing single-GPU pin, `dry-*` present (banned). `profile edit <id>` writes a
  `.history/<id>.previous.json` snapshot automatically (reversible; TUI `u` = undo), so a correction
  is safe.
- **Create a variant** when the original **plausibly serves a different purpose** — the questionable
  value is a *fit tradeoff*, not a bug. Canonical case: `q4_0` KV chosen to fit a **1M ctx** on one
  card is a legitimate capacity decision, not corruption. Do **not** overwrite it. Build an
  agent-safe sibling instead: `profile duplicate <id> <id>-q8kv-256k` → `profile edit` it to KV
  `q8_0` at a ctx that fits (§Context adjustment rule) → validate → launch. Rename/annotate the
  original per workflows/audit-profile.md only with explicit user approval.

When unsure which side of the line you are on, treat it as a variant (non-destructive) and let the
benchmark/audit decide.

## OOM / won't load

- **Reproduce:** `instance start <id>`; if it loads, send a **near-full-ctx** prompt (idle load
  proves nothing — the CUDA pool grows on prefill). Watch `nvidia-smi` on the binding card climb.
- **Diagnose:** launch log — `common_fit_params` spilling MoE experts to CPU (means `-ngl 99` not
  pinned)? KV/compute-buffer line at peak? vLLM prints the max `max-model-len` it can actually serve;
  SGLang has no fail-fast — compare `max_total_num_tokens` vs `context_len` in its log.
- **Fixes (one step per iteration, re-measure):** walk the backend's OOM ladder, cheapest lever
  first (the §Context adjustment rule move is usually step 1):
  - llama family → references/llama-family.md §OOM sacrifice ladder (ctx ↓ within band → KV one bpv
    step, **never below `q8_0` on tool-calling profiles** → quant tier ↓ → ubatch 2048→1024→512;
    **non-agent only**, tighten KVarN one `kvarn*` step, beellama v0.4.0 — tool-calling keeps the
    q8_0 floor, cut ctx or ship a lower-ctx variant). MoE that must shrink below its all-GPU footprint → Luce Spark
    (references/dflash.md), never naive `--n-cpu-moe`.
  - vLLM/SGLang → the OOM ladders in references/vllm-sglang.md §vLLM 0.24.0 knobs / §SGLang 0.5.9
    knobs (printed max-model-len → util ↓ → `max-num-batched-tokens`/`chunked-prefill-size` 4096 →
    4-bit checkpoint → fp8 KV (verify quality) → `enforce-eager` last resort). Never `cpu-offload-gb`.
  - lucebox → references/dflash.md §Tuning on this card (`max-ctx` leans DOWN; KV `q8_0`→`q4_0`→`tq3_0`;
    `--spark-vram`). Won't-load on an unsupported arch → wrong backend (SKILL.md §Backend dispatch).
- **Verify:** the near-full-ctx prompt completes AND peak `nvidia-smi` on both cards stays ≤23 GiB.

## Loops / repetition

- **Reproduce:** a long generation (or the agent turn that looped) — not a one-liner; loops surface
  with length.
- **Diagnose:** is temp the family value or was it forced to 0/greedy (official warnings + measured
  loops — §Red flags)? Is the anti-loop baseline present? Is `dry-*` present (banned)?
- **Fixes (one per iteration):** per anti-loop policy → references/model-research.md §3 (Anti-loop
  policy):
  - llama family: ensure `repeat-penalty 1.05` + `repeat-last-n 256` (+ family temp) are set — that
    is the whole baseline. Never temp 0.
  - beellama think-block loops: `"reasoning-loop-guard": "force-close"` (deterministic, safe for tool
    calls, no sampling change).
  - vLLM/SGLang/lucebox: no launch-side sampling — anti-loop is client-side
    `frequency_penalty`/`presence_penalty` (document it in the description).
  - **DRY stays banned.** Add `dry-*` only if the user explicitly asks in this conversation, then
    `dry-allowed-length ≥4` (aggressive DRY corrodes tool calls — measured).
- **Verify:** a long generation runs to a clean stop with no runaway repetition.

## Corrupted tool calls

- **Reproduce:** the **shell-hostile tool-call round trip** — ask the model to call `bash` with
  exactly `mkdir -p "/tmp/My Project/sub dir" && echo "a && b > c" > "/tmp/My Project/sub dir/x y.txt" 2>&1`
  (references/model-research.md §4 step 4).
- **Diagnose:** launch log `Chat format:` (peg-native = template parser OK; Generic = no tool syntax
  recognized). Did the GGUF template drift from the HF `chat_template.jinja`?
- **Fixes — template-first ladder, one per iteration** (references/model-research.md §4):
  1. **Template diff** → save the official/modified `.jinja` locally →
     `"chat-template-file": "/abs/path.jinja"` (validates in `args`). Rig case: Ornith/AEON needs its
     own template or Claude Code Bash calls corrupt even at temp 0.
  2. **KV quant → `q8_0`/`q8_0`** if it was `q4_0` (official caution + measured corruption; this is a
     fix-in-place unless q4_0 was a 1M-ctx fit tradeoff — see the decision rule).
  3. **Sampling** — set the model's real config; over-aggressive anti-repetition mimics template
     corruption. Remove any `dry-*`.
  4. **Parser** — correct `tool-call-parser`/`reasoning-parser` per family
     (references/model-research.md §4 matrix). These validate in `args` now (S1/S2 fixed — never route
     through extraArgs). vLLM: leave `reasoning-parser` UNSET until the token-accounting test passes.
- **Verify:** round trip passes — `finish_reason:"tool_calls"`, `arguments` a JSON **string**, the
  command byte-for-byte intact, nothing leaked into `content`.

## Speculative not loading / no speedup

Chains validate in `args` now (`spec-type` is a list-valued enum; S1 fixed) — so a chain that won't
load is a **real load failure**, never a validator block. Do not "work around" it through extraArgs.

- **Reproduce:** warm-measure tok/s (`instance metrics -w`) against a no-spec baseline; check the
  launch log at load.
- **Diagnose:** is the spec actually drafting? **llama.cpp-stable logs sparsely at default
  verbosity — an absent `draft-mtp`/`dflash` line in the launch log does NOT prove the spec is
  off** (measured: a healthy chain shows no line). Confirm through a direct OpenAI response's server
  `timings` (`draft_n` / `draft_n_accepted` > 0 = drafting live and gives the acceptance %), raise
  `--log-verbosity`/`lv` to force the line, or use a benchmark. Claude Code's Anthropic route does
  not preserve `timings`. Also verify the asset exists (GGUF `nextn_predict_layers` /
  `blk.N.nextn.*` tensors, or the external drafter path) → references/speculative.md §Asset
  inventory. Only absent drafting across those observable surfaces proves the spec is not engaged.
- **Fixes (one per iteration):**
  1. **Dialect mismatch** — the spelling must match the backend: **upstream llama.cpp + beellama
     v0.4.0 = `draft-mtp`/`draft-dflash`**, **only buun = `mtp`/`dflash`** (full matrix →
     references/speculative.md §Per-engine support). A valid-but-wrong-dialect value loads nothing.
  2. **Missing/wrong asset resolution** — the GGUF/quant must ship the MTP/nextn tensors, or the
     external drafter must resolve to the matching sidecar. On nightly b10083, `spec-draft-hf`
     requires the corresponding `spec-type` (`draft-mtp`→`mtp-*`, `draft-dflash`→`dflash-*`,
     `draft-eagle3`→`eagle3-*`); use one model-based type per HF repo because multiple sidecars
     download in parallel with no guaranteed winner. Stable b9934 may select the repo's main GGUF
     instead, so download the sidecar and use local `spec-draft-model`. Config field ≠ loaded file.
  3. **`split-mode tensor` + speculation — split the diagnosis:** an **external draft** silently
     stops/hangs (#22473, open) → use `split-mode layer` or chain draftless `ngram-mod` which
     survives tensor. An **embedded MTP/nextn head** is per-model — some load and run, some crash
     (#24309 nextn at load; #24440/#24324 `fattn.cu:579` on a checkpoint restore) → try
     `LLAMA_GRAPH_REUSE_DISABLE=1`, else `split-mode layer`; never read a successful load as proof
     tensor is faster (references/dual-gpu.md §llama.cpp `-sm tensor`).
  4. **Loads but no speedup = low acceptance** — high sampling entropy without `top-n-sigma 1.0`, or a
     mismatched/cross-generation drafter (30% vs 65% accept). On llama.cpp/beellama run the calibrated
     temp + `top-n-sigma 1.0` (references/speculative.md §Sampling × speculation). Judge tok/s, not
     acceptance alone.
- **Verify:** drafting confirmed live — direct OpenAI `timings.draft_n_accepted` > 0, the
  head/drafter line at raised verbosity, or benchmark evidence — AND warm-measured tok/s beats the
  no-spec baseline.

## Split / multi-GPU problems

- **Reproduce:** a **long, non-English** generation (the corruption class #20052/#40725 hides behind
  short English prompts).
- **Diagnose:** run the sanity check ONCE before blaming the profile →
  references/dual-gpu.md §P2P + §NCCL / P2P sanity check. **Separate the two paths first:** on the
  **patched rig** (driver 610.43.02, P2P validated) `nvidia-smi topo -p2p r` should read **OK** and TP2
  runs P2P-on (drop `NCCL_P2P_DISABLE`; **keep `disable-custom-all-reduce` — custom AR crashes on SM86**);
  on a **stock/unpatched driver** it reads **NS** and `NCCL_P2P_DISABLE=1` applies. `nvidia-smi topo -m` expect PHB; per-GPU
  `pcie.link.gen/width` — asymmetric/degraded = the #20052 riser-cable root cause, not a software bug.
- **Fixes (one per iteration):**
  1. **`tensor-split` MUST be `"0.5,0.5"`** — any asymmetric ratio is banned and measured slower
     (SKILL.md §Hard rules; references/dual-gpu.md). Fix in place.
  2. **A single-GPU profile silently layer-split** (both cards visible → llama.cpp auto-splits) — pin
     GPU1: `launch.env` `CUDA_DEVICE_ORDER=PCI_BUS_ID` + `CUDA_VISIBLE_DEVICES=1`, no split flags.
  3. **When genuinely splitting, no `CUDA_VISIBLE_DEVICES` mask** (it remaps the card to `cuda:0`);
     use equal `tensor-split` + optional `main-gpu 1`, and never pair the mask with `-mg`/`--device`.
  4. **P2P is a narrow, log-proven win.** vLLM TP2: drop `NCCL_P2P_DISABLE` (P2P-on, +13.5% concurrent) but
     **keep `disable-custom-all-reduce: true` — custom AR crashes on SM86** (`custom_all_reduce.cuh:455`);
     an engine that dies at startup with the flag absent is this bug, not a P2P failure. `GGML_CUDA_P2P=1`
     on llama.cpp-stable/nightly/beellama/buun (NOT ik/lucebox) measured a TIE for single-stream splits —
     keep the default; prove any use in the log, heed the IOMMU/BIOS crash caveat, unset if it garbles.
     Init hang / suspected corruption: re-add `NCCL_P2P_DISABLE=1` (dual-gpu.md §P2P). External draft +
     `split-mode tensor` → use `layer` (crash — P2P does not fix it).
  5. Suspect a marginal PCIe riser/cable if output garbles under otherwise-correct split config.
- **Verify:** the long non-English generation stays coherent end-to-end.

## Proxy vs backend confusion

- **Reproduce:** a completion returns a model-not-found / stale state, or a CLI write command hangs
  or reports "not found" while a model is clearly serving.
- **Diagnose:** `curl -s 127.0.0.1:4321/_status` — the proxy is **single-active**; `loaded_profile_id`
  is the source of truth for what is answering. vLLM/SGLang 404 on model mismatch → confirm
  `served-model-name` == profile id (§Common mistakes).
- **Fixes:**
  - **"CLI says not found while the proxy owns the process."** A write command (`instance start/stop`,
    `profile create/edit`) takes the single-instance flock; an **open TUI holds it**. Find the holder:
    `fuser ~/.local/state/model-loader/model-loader.lock` — close the TUI (or use read-only paths).
    To inspect the live backend meanwhile, **read the log file directly** at
    `~/.local/state/model-loader/logs/<profile>-<port>.log` (read paths don't take the lock).
  - **Need two models live** — the proxy evicts on swap; run a second headless proxy
    `model-loader serve --port 4322` for the secondary profile, each pinned to its own GPU
    (references/dual-gpu.md §Pin-per-GPU). `serve` does not take the single-instance lock.
- **Verify:** `/_status` shows the expected `loaded_profile_id`/`pid`/`port` and a completion round
  trips through `127.0.0.1:4321`.

# Audit profiles against the rig rules

**Entry:** "check my profiles" / suspected drift / a periodic conformance sweep. **Exit:** one
findings table **per profile** (`rule · severity · current · expected · suggested fix`) + a
summary ranking, most- to least-conformant. Nothing is launched, nothing is measured.

> **This workflow REPORTS ONLY. It never edits, renames, validates-and-fixes, or launches a
> profile.** Every finding names a fix *owner*, not a fix you apply here: a misbehavior or a
> mechanical correction (KV type, split mode, missing pin/flag, DRY, stale sampling) → hand to
> **workflows/troubleshoot.md**; anything that needs a fresh measurement or a promotion (re-tune
> sampling, re-measure layer-vs-tensor at a ctx tier, fill in a "calibration pending" description)
> → hand to **workflows/full-tuning.md**. The fix-in-place-vs-variant decision belongs to
> troubleshoot; the audit only flags. Renaming an id rewrites the profile filename, so even a
> stale ctx label is a *report*, never an edit here.

## 1. Gather (read-only)

1. `model-loader profile list --json` → the id set (audit all, or the subset the user named).
2. For each id: `model-loader profile show <id> --json` → the canonical profile (`id`, `name`,
   `description`, `tags`, `model`, `args`, `extraArgs`, `launch.backendId`, `launch.env[]`).
3. Optionally `model-loader profile validate <id> --json` as a cheap legality gate — but validation
   catches *schema* errors (unknown flags, enum, cross-field); this audit catches *rig-conformance*
   and *naming/purpose* drift that a valid profile still carries. Keep the two separate.

Read `launch.backendId` first — it selects which checks apply (llama family vs vLLM/SGLang vs
tabby/dflash). Read the field paths exactly: llama.cpp ctx = `args.ctx-size`, vLLM = `args.max-model-len`;
KV = `args.cache-type-k`/`cache-type-v` (llama) vs `args.kv-cache-dtype` (vLLM/SGLang); split =
`args.tensor-split`/`args.split-mode` (llama) vs `args.tensor-parallel-size`/`args.gpu-split` (vLLM/tabby).

## 2. The conformance checklist — run every row against every profile

Derive **current** from the profile JSON, **expected** from the cited rule; assign severity; name
the fix owner. `error` = breaks correctness/safety or 404s at the proxy; `warn` = runs but
degrades, misleads, or drifts from a rule; `info` = cosmetic / a defensible fit-for-purpose choice
worth noting. A row that doesn't apply to the backend → mark **N/A**, don't invent a finding.

| # | Check | current = read | expected (source) | typical severity |
|---|---|---|---|---|
| 1 | **ctx label vs args** | the `-<N>k`/`-<N>m` segment in `id` | `floor(ctx / 1024)` in binary-k from `args.ctx-size` (llama) / `args.max-model-len` (vLLM): `262144→256k`, `253952→248k`, `200000→195k`, `1048576→1m`. Missing/stale label ⇒ finding (AGENTS.md §5 *Profile naming convention*) | warn |
| 2 | **vision segment vs `mmproj`** | `vision` in `id`? `args.mmproj` set? | `vision` present **iff** `args.mmproj` set. A VL model served as text (no `mmproj`, e.g. vLLM `--language-model-only`) correctly has NO `vision` segment (§5) | warn |
| 3 | **mtp/dflash segment vs `spec-type`** | `mtp`/`dflash` in `id`? `args.spec-type` set? | segment present **iff** `args.spec-type` set, and the dialect matches the backend (llama + beellama v0.4.0 `draft-mtp`/`draft-dflash`; only buun `mtp`/`dflash`) (§5 + SKILL.md §Backend dispatch) | warn |
| 4 | **KV type vs purpose** | llama: `args.cache-type-k`/`-v`; vLLM/SGLang: `args.kv-cache-dtype` | tool-calling/agent profile (tags/desc say agentic/coding) ⇒ **llama KV `q8_0`/`q8_0`, never `q4_0`** (SKILL.md §Hard rules; model-research.md §4 — q4_0 corrupts tool calls). `q4_0` on an agent profile = **error**; vLLM `fp8_e5m2`/`fp8`/`auto` is storage-only and fine. KV types must be symmetric (`q8_0/q4_0` mix ⇒ error, #20866). If q4_0 was chosen to *fit* a huge ctx, still flag it but note the fit-vs-purpose trade — troubleshoot decides variant vs in-place | error (agent) / info (non-agent, e.g. caption/OCR) |
| 5 | **split symmetry** | `args.tensor-split` | present ⇒ **`"0.5,0.5"` only**. Any uneven ratio (`0.45,0.55`, `0.4,0.6`) = **error** (SKILL.md §Hard rules "NEVER asymmetric"; dual-gpu.md). `main-gpu` set is fine, not a substitute | error |
| 6 | **tensor-mode vs ctx** | `args.split-mode`, ctx | `split-mode: tensor` reverses to a *loss* at **ctx ≥ 512k** (prefill regresses, decode gain gone) ⇒ prefer `layer` at large ctx (SKILL.md §The rig + §Common mistakes; dual-gpu.md). tensor at ≥512k = **error/warn** (re-measure owner: full-tuning) | error |
| 7 | **tensor + external draft / unproven tensor** | `args.split-mode: tensor` **and** an external draft (`args.model-draft`/`spec-draft-hf`/a beellama DFlash drafter path) | tensor-split **crashes/silent-stops with external drafts** (#22473; SKILL.md Common mistakes; dual-gpu.md) -> use `layer`. A model-internal MTP/nextn head (`spec-type: draft-mtp`, no separate draft model) is **not** an external draft - do not flag the draft; but `split-mode: tensor` on an embedded-MTP/MoE profile must record a complete one-knob A/B with equal-weight raw median-ratio geomean >=1.05 plus passing TTFT/VRAM/draft/correctness/backend-log guardrails (full-tuning.md). Tensor without that evidence = **warn** (a load proves compatibility, not speed) | error (external) / warn (unproven tensor) |
| 8 | **single-GPU pin present** | `launch.env[]`; split flags in `args` | if weights + target-ctx KV fit **≤23 GiB** and there are **no** split flags (`split-mode`/`tensor-split`/`tensor-parallel-size`/`gpu-split`) ⇒ this is a single-GPU profile and `launch.env` **must** carry BOTH `CUDA_DEVICE_ORDER=PCI_BUS_ID` and `CUDA_VISIBLE_DEVICES=1`, else llama.cpp silently auto-splits (SKILL.md §Placement decision + §Common mistakes). A genuine multi-GPU profile (split flags present) correctly has NO `CUDA_VISIBLE_DEVICES=1` ⇒ N/A | error (should-be-single, unpinned) / N/A (multi-GPU) |
| 9 | **anti-loop flags (llama family)** | `args.repeat-penalty`, `args.repeat-last-n` | every llama-server/beellama/buun profile ⇒ `repeat-penalty ≈ 1.05` + `repeat-last-n ≈ 256` (SKILL.md §Hard rules; model-research.md §3). Absent = warn. N/A on vLLM/SGLang/dflash (penalties are client-side only) | warn |
| 10 | **DRY absent** | any `args.dry-*` (`dry-multiplier`/`dry-base`/`dry-allowed-length`/`dry-penalty-last-n`/`dry-sequence-breakers`) | **DRY is banned** (user decision 2026-07-02) unless the description records an explicit user request (then `allowed-length ≥ 4`). Present without that note = **error** (SKILL.md §Hard rules; model-research.md §3) | error |
| 11 | **`port` absent** | `args.port` | must **not** exist — `port` is reserved and stripped (SKILL.md §Hard rules; AGENTS.md §5). Present = error | error |
| 12 | **served-model-name = id** | `args.served-model-name` (vLLM/SGLang only) | must **equal** `id`, else the proxy forwards `model=<id>` and the backend 404s (SKILL.md §Common mistakes; AGENTS.md §2.4). Missing/mismatch = **error**. N/A on llama family / tabby / dflash | error (vLLM/SGLang) / N/A |
| 13 | **sampling vs family preset** | llama family: `args.temperature`/`top-p`/`top-k`/`min-p`/`top-n-sigma` | match the model's family row in model-research.md §2 (`temp 0`/greedy = **error** — official loop warnings, §3). 2026-gen agentic-coding (Qwen3.6/Ornith/GLM-4.7+) ⇒ **temp 1.0 + top-n-sigma 1.0** (holds MTP/DFlash acceptance); temp 0.6 is the single-shot preset. Off-preset = warn; greedy = error (§2/§3/§5). vLLM/SGLang rely on `generation-config auto` — flag only a wrong `override-generation-config` | error (greedy) / warn |
| 14 | **description honesty** | `description`, `name` | headline numbers must be **measured** (tok/s, per-card GiB, acceptance %, dated — the full-tuning exit contract) OR the description must say **"calibration pending"** (quick-profile output). Unmeasured numbers stated as fact, or numbers that contradict current `args`, = warn (SKILL.md §Common mistakes "Trusting startup…"). Owner: full-tuning to fill/refresh | warn |
| 15 | **TP2 P2P/custom-AR flags** (patched rig, SM86) | vLLM/SGLang/SNDR TP2: `launch.env` `NCCL_P2P_DISABLE=1`; `args.disable-custom-all-reduce` | **`NCCL_P2P_DISABLE=1` = warn** on any TP2 profile — measured **+13.5% concurrent loss** on this patched rig (driver 610.43.02); drop it, P2P-on is the default (SKILL.md §Hard rules; dual-gpu.md §P2P; matches the `rtx3090_p2p` performance policy). **`disable-custom-all-reduce: true` is CORRECT on SM86 vLLM 0.24.0 — do NOT flag it** (custom AR crashes, `custom_all_reduce.cuh:455`); instead **warn when custom-AR is ENABLED** (flag absent/`false`) on a vLLM TP2 profile. SGLang: no custom-AR flag either way (silent self-disable, unproven). N/A on single-GPU / non-vLLM-family | warn (NCCL_P2P_DISABLE=1, or custom-AR enabled on vLLM TP2) |
| 16 | **P2P claim without proof / wrong backend** | `launch.env` `GGML_CUDA_P2P`; a description claiming a P2P/custom-AR/peer-access speedup | `GGML_CUDA_P2P` is valid only on **llama.cpp-stable/nightly/beellama/buun split** profiles (NOT ik, NOT lucebox, NOT a single-GPU pin) and only after **launch-log proof**. **Measured 2026-07-10: native single-stream splits (llama `GGML_CUDA_P2P`, lucebox `--peer-access`) are a TIE** — a description asserting a P2P/peer-access speedup for a split without a measured A/B = warn (SKILL.md §Red flags; dual-gpu.md §P2P). `GGML_CUDA_P2P` on ik/lucebox or a single-GPU profile = **error** | error (wrong backend/single-GPU) / warn (unproven claim) |

Notes that keep the audit honest:
- **Purpose is inferred from `tags` + `description`, not the id.** "agentic"/"coding"/"agent" tags or
  Claude-Code/tool-calling language ⇒ apply the tool-calling KV + sampling rules (rows 4, 13).
- **A defensible trade-off is `info`, not `error`.** q4_0 KV on a caption/OCR profile, or a lowered
  ctx that buys a quant tier (SKILL.md §Context adjustment rule), is a *reported* choice — say why,
  hand the promote/variant decision to the fix owner.
- **Do not re-flag schema legality** the validator already owns (unknown flags, enum values). Post
  BUGS.md S1/S2, chained `spec-type` (`draft-mtp,ngram-mod`) and the widened sglang parsers validate
  in `args` — never report them as "blocked" (SKILL.md §Red flags).
- **P2P flags are warnings, not schema errors** (rows 15–16): engagement is proven from the launch log,
  never from a profile field. The audit flags `NCCL_P2P_DISABLE=1` (measured +13.5% loss) and an ENABLED
  custom-AR on a SM86 vLLM TP2 profile (it crashes) — but `disable-custom-all-reduce: true` is the correct
  SM86 default, never flagged. A documented stock-driver / init-hang fallback note is `info`.

## 3. Output format — one table per profile, then a ranking

For each profile emit exactly this, listing **only** rows that produced a finding (pass/N/A rows
are omitted; note "all other checks pass"):

```
### <profile-id>

| rule | severity | current | expected | suggested fix |
|---|---|---|---|---|
| … | error/warn/info | <from JSON> | <rule + source> | <one line + fix owner> |
```

Worked example (real drift — `qwythos-9b-mtp-q4km`, a pinned llama.cpp-stable profile: `ctx-size
1048576`, `cache-type-k/v q4_0`, `split-mode tensor`, `tensor-split 0.5,0.5`, `spec-type draft-mtp`,
tags include `agentic`):

### qwythos-9b-mtp-q4km

| rule | severity | current | expected | suggested fix |
|---|---|---|---|---|
| ctx label vs args | warn | id has **no** ctx segment | `1048576/1024 = 1024k → -1m` in the id | rename per §5 → **troubleshoot** (id rename rewrites the file) |
| KV type vs purpose | error | `cache-type-k/v: q4_0` on an `agentic` profile | `q8_0/q8_0` for tool-calling (model-research.md §4) | q4_0 chosen to fit 1M ctx (fit-vs-purpose) → **troubleshoot**: q8_0 variant at a fitting ctx, or keep as an explicit non-agent 1M profile |
| tensor-mode vs ctx | error | `split-mode: tensor` @ 1048576 (≥512k) | `layer` — tensor reverses ≥512k, prefill regresses (dual-gpu.md) | **full-tuning**: re-measure layer vs tensor at this ctx |
| sampling vs family preset | warn | `temp 0.6`, no `top-n-sigma` | agentic MTP (Qwen family) → `temp 1.0 + top-n-sigma 1.0` holds acceptance (§2/§3/§5); 0.6 = single-shot | **full-tuning**: re-tune + measure acceptance |

_All other checks pass: split symmetry `0.5,0.5` ✓ · `mtp` segment ↔ `spec-type: draft-mtp` ✓ ·
anti-loop `repeat-penalty 1.05`/`repeat-last-n 256` ✓ · DRY absent ✓ · `port` absent ✓ ·
served-model-name N/A (llama) · single-GPU pin N/A (intentional 2-GPU tensor split) · description
carries dated measured tok/s ✓._

Then a **summary ranking** — most- to least-conformant. Score by weight (`error` ≫ `warn` > `info`);
ties broken by fewest total findings:

```
| rank | profile | errors | warns | infos | verdict |
|---|---|---|---|---|---|
| 1 | <cleanest> | 0 | 0 | 0 | conformant |
| … | … | n | n | n | needs troubleshoot / needs full-tuning |
```

One closing sentence: which profiles are clean, which need troubleshoot (mechanical fixes), which
need full-tuning (re-measure/promote) — the audit stops there.

## Cross-references

- **Rules & shared facts** → SKILL.md §Hard rules, §Placement decision, §The rig, §Backend dispatch,
  §Context adjustment rule, §Common mistakes, §Red flags.
- **Naming convention** → `/home/diogo/dev/model-loader/AGENTS.md` §5 *Profile naming convention*
  (ctx label, capability segments) + §2.4 (proxy needs `served-model-name`).
- **Sampling presets / anti-loop / KV q4_0** → references/model-research.md §2, §3, §4.
- **Split-mode / tensor-vs-layer / asymmetric ban** → references/dual-gpu.md.
- **Fix owners** → workflows/troubleshoot.md (misbehavior + mechanical fixes, variant-vs-in-place)
  and workflows/full-tuning.md (re-measure, re-tune, fill "calibration pending").

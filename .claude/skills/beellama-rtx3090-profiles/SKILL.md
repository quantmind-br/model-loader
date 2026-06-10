---
name: beellama-rtx3090-profiles
description: Create optimized model-loader profiles for the BeeLlama.cpp backend (beellama-rtx3090) on a dedicated RTX 3090 24GB — maximize tokens/s via DFlash/MTP speculative decoding and maximize context window via KV-cache quantization and VRAM budgeting. Use whenever the user asks to create, tune, or optimize a profile/preset/launch config for a local GGUF model, mentions BeeLlama, DFlash, MTP, KV cache types (q4_0, q5_0, turbo*), context size, fitting a model in 24GB VRAM, or wants a model to "run faster" or "with more context" on the 3090 — even if they don't say the word "profile".
---

# BeeLlama RTX 3090 Profile Builder

Build a `model-loader` profile (schemaVersion 3 JSON) that launches a GGUF model through the
**BeeLlama.cpp** backend with the best throughput/context trade-off a dedicated RTX 3090 24GB
can give. The output is a validated file in `~/.config/model-loader/profiles/<id>.json`.

## Environment facts (this machine)

| What | Where |
| --- | --- |
| Backend catalog entry | id `beellama-rtx3090`, kind `beellama-cpp` |
| Backend binary | `/home/diogo/dev/model-loader/backends/beellama.cpp/bin/llama-server` (symlink to `build-rtx3090/bin`) |
| Profiles dir | `~/.config/model-loader/profiles/` (one JSON per profile) |
| Canonical profile schema | `/home/diogo/dev/model-loader/docs/profile-schema.json` |
| Full BeeLlama arg reference | `/home/diogo/dev/model-loader/backends/beellama.cpp/docs/beellama-args.md` |
| DFlash quickstart (24GB tuning guide) | `/home/diogo/dev/model-loader/backends/beellama.cpp/docs/quickstart-qwen36-dflash.md` |
| GPU | RTX 3090, 24576 MiB VRAM, **dedicated to LLMs** (no display/desktop pressure) |

Because the card is dedicated, budget for a peak of **~23.2 GiB** and treat anything above
that as OOM risk (CUDA context + fragmentation eat the rest). Verify real usage with
`nvidia-smi` after launch — never trust estimates alone.

Read `beellama-args.md` before inventing flags: BeeLlama v0.3.0 **removed** old aliases
(`--draft`, `--draft-model`, `--draft-topk`, `--tree-budget`, `--spec-dflash-default`, …).
Use canonical `--spec-*` names. Existing profiles in the profiles dir are proven references —
mirror their arg spelling (`spec-draft-model`, `spec-draft-ngl`, `spec-dflash-cross-ctx`, …).

## Workflow

### 1. Gather inputs

Establish before writing anything:

- **Target GGUF path and file size** (`ls -lh`). The file size is your weights VRAM floor.
- **Speculative options for this model**:
  - A **DFlash drafter GGUF** exists? (check sibling dirs under `~/models/`, or
    `Anbeeld/<Model>-DFlash-GGUF` on Hugging Face). DFlash is the biggest single win
    (~1.5–2× tokens/s on accepted drafts) at a small VRAM cost (drafter + cross ring).
  - The GGUF is an **MTP variant** (e.g. `*-MTP-GGUF`)? Then `spec-type: draft-mtp` with
    `spec-draft-n-max: 3` — no separate drafter file needed.
  - Neither → run without speculation, or `ngram-mod`/`copyspec` only for highly repetitive
    workloads (agents replaying context). Don't enable them by default.
- **Vision needed?** mmproj file available? (mmproj + DFlash is supported but forces *flat*
  DFlash, and disables context-shift/cache-reuse.)
- **Use case bias**: precision (coding/agents) vs. throughput/maximum context. This picks the
  quant combo below.

### 2. Pick the quant combo

| Bias | Target quant | Drafter quant | KV cache K/V | Notes |
| --- | --- | --- | --- | --- |
| Precision (coding) | Q5_K_S / Q5_K_M | Q4_K_M | `q5_0` / `q4_1` | Best quality that still fits ~100k+ ctx on 27B-class models |
| Balanced / max context | Q4_K_M | Q4_K_M | `q4_0` / `q4_0` | Proven: 27B + drafter at **204800 ctx ≈ 22.4 GB** |
| Extreme VRAM squeeze | IQ4_XS | IQ4_XS | `turbo3_tcq` / `turbo3_tcq` | TurboQuant is CUDA-only; `turbo2_tcq` is last resort, verify quality |

KV-cache bits per value (for budgeting): `f16` 16, `q8_0` 8.5, `q5_1` 6.0, `q5_0` 5.5,
`q4_1` 5.0, `q4_0`/`iq4_nl` 4.5, `turbo4` 4.125, `turbo3_tcq` 3.25, `turbo3` 3.125,
`turbo2_tcq` 2.25, `turbo2` 2.125.

KV VRAM ≈ `ctx × n_layer × n_kv_heads × head_dim × (bpv_K + bpv_V) / 8` bytes
(dense-attention models; SWA models like Gemma use much less for the sliding-window layers,
which is why Gemma E4B fits 262k at q8_0/q8_0). Don't agonize over the formula — use it to
pick a starting `ctx-size`, then calibrate empirically (step 5).

Heavier drafters (Q8) are a net *negative* for tokens/s — drafting doesn't need precision.

### 3. Choose context size and DFlash settings

Maximize `ctx-size` with what's left after weights + drafter:

- Start from a known-good anchor (below) and scale: KV cost is linear in `ctx-size`.
- DFlash specifics: `spec-dflash-cross-ctx: 1024` for long-context serving (default 512 saves
  VRAM but drafts worse past ~32k). Leave `spec-draft-n-max` unset (DFlash default 16) and let
  the default adaptive `profit` controller manage depth. Keep `spec-branch-budget` at default 0
  (flat DFlash) — tree mode is slower in practice and forced off under mmproj anyway.
- `parallel: 1` — more slots multiply KV cost; this card serves one user.
- `batch-size: 2048`, `ubatch-size: 512`. Lower `ubatch-size` to 256 only if the last GiB
  won't fit (compute buffers scale with ubatch × ctx); it costs prefill speed.

**Known-good anchors on this machine** (see the live profiles for full args):

| Recipe | KV | ctx | Peak VRAM | Result |
| --- | --- | --- | --- | --- |
| Qwen3.6-27B Q4_K_M + DFlash Q4_K_M | q4_0/q4_0 | 204800 | ~22.4 GB | ~157 tok/s, ~57% acceptance |
| Qwen3.6-27B Q5_K_S + DFlash Q4_K_M | q5_0/q4_1 | 102400 | fits w/ margin | precision combo |
| Qwen3.6-35B-A3B-MTP UD-Q3_K_M (draft-mtp) | q8_0/q8_0 | 200000 | fits | MoE, no drafter file |

### 4. Write the profile JSON

Template (adapt, don't copy blindly — drop the `spec-*` keys for non-DFlash models):

```json
{
  "schemaVersion": 3,
  "id": "<model>-<variant>-<ctx>k",
  "name": "<Human Name> (<ctx>k, BeeLlama)",
  "description": "<target quant> + <spec strategy>, <K>/<V> KV cache, ~<N>GB on RTX 3090 24GB.",
  "tags": ["beellama", "<family>", "<dflash|mtp>", "<ctx>k"],
  "model": "/abs/path/to/target.gguf",
  "args": {
    "batch-size": 2048,
    "cache-ram": 0,
    "cache-type-k": "q4_0",
    "cache-type-v": "q4_0",
    "chat-template-kwargs": "{\"preserve_thinking\":true}",
    "ctx-size": 204800,
    "flash-attn": "on",
    "host": "127.0.0.1",
    "jinja": true,
    "kv-unified": true,
    "n-gpu-layers": 99,
    "parallel": 1,
    "port": 8084,
    "reasoning": "on",
    "spec-dflash-cross-ctx": 1024,
    "spec-draft-model": "/abs/path/to/drafter.gguf",
    "spec-draft-ngl": 99,
    "spec-type": "dflash",
    "temperature": 0.6,
    "top-k": 20,
    "top-p": 1,
    "min-p": 0
  },
  "extraArgs": ["--no-mmap", "--mlock", "--no-host"],
  "launch": {
    "defaultBackground": true,
    "backendId": "beellama-rtx3090",
    "restart_policy": "none",
    "max_restarts": 3,
    "backoff_seconds": 5
  },
  "meta": { "createdAt": "<now>", "updatedAt": "<now>" },
  "pinned": false
}
```

Rules that bite if ignored:

- **id**: lowercase slug matching `^[a-z0-9]+([._-][a-z0-9]+)*$`; it becomes the filename
  (`<id>.json`).
- **port**: must not collide — check with
  `grep -h '"port"' ~/.config/model-loader/profiles/*.json | sort -u`.
- **meta timestamps**: RFC 3339 UTC — `date -u +%Y-%m-%dT%H:%M:%SZ`.
- **Sampling per family**: Qwen3.6 thinking → temp 0.6, top-k 20, top-p 1, min-p 0;
  Gemma → top-k 64, top-p 0.95, min-p 0. Check the model card if it's another family.
- **`reasoning: "on"` + `preserve_thinking`** only for thinking models — thinking tokens also
  feed the DFlash drafter richer context. Omit both for non-reasoning models. For thinking
  models prone to loops, the reasoning loop guard is already on by default (`force-close`).
- **`cache-ram: 0`** disables the server prompt-cache RAM subsystem (live-slot prefix reuse
  still works). Right call for huge-context single-user profiles; keep the default if the user
  swaps between many long prompts and has RAM to spare.
- **Vision**: add `"mmproj": "/abs/path"` plus extraArg `--no-mmproj-offload` (projector on
  CPU, zero VRAM cost — dedicated GPU means latency hit is acceptable).
- **Env vars** go in `launch.env` as `[{"key":"...","value":"..."}]` (e.g. DFlash kill
  switches like `GGML_DFLASH_GPU_RING=0` — debugging only).
- If `profile validate` rejects an arg the binary genuinely supports (schema pinned older than
  the fork), move that flag to `extraArgs` (validation doesn't inspect those), or refresh the
  backend schema — don't hand-edit the schema JSON.

### 5. Validate, launch, calibrate

```bash
model-loader profile validate <id>          # schema + flag validation — must print "ok"
model-loader instance start <id>            # launch through the manager (NEVER run llama-server by hand)
model-loader instance logs <id> | tail -50  # confirm model + drafter loaded, no OOM
nvidia-smi --query-gpu=memory.used --format=csv  # confirm ≤ ~23.2 GiB peak
```

Then send a real chat request through the proxy and watch `instance logs` for `dflash:` /
acceptance-rate lines (DFlash silently inactive = misconfigured drafter).

Calibration loop for maximum context: if peak VRAM is under ~22.5 GiB, raise `ctx-size`
proportionally (KV is linear); if it OOMs or exceeds budget, lower it. Order of sacrifices
when it still doesn't fit: ctx-size → KV types one step down the ladder → target quant one
step down → cross-ctx 1024→512 → ubatch 512→256. Stop and re-measure after each change.

If the user wants numbers, `model-loader benchmark run <id>` gives comparable tok/s; record
results in the profile `description` (model quant, KV, ctx, peak GB, tok/s, acceptance %) the
way existing profiles do — future tuning depends on those breadcrumbs.

## Pitfalls

- Don't enable DFlash with a plain (non-DFlash) GGUF as drafter — it will fail to load;
  drafters come from `Anbeeld/*-DFlash-GGUF`-style repos or auto-detect by metadata.
- `top-k` (sampling) ≠ `spec-draft-top-k` (tree drafting). You almost never set the latter.
- `--kv-unified` stays on: idle-slot caching requires it and the proven profiles all use it.
- mmproj disables context-shift and cache-reuse, and forces flat DFlash — fine, but don't
  "fix" those warnings.
- KV types below `q4_0` (turbo2*, turbo3*) trade real quality; never default to them, offer
  them only when the user explicitly wants extreme context over quality.
- Never benchmark with adaptive draft-max on if comparing fixed depths
  (`--no-spec-dm-adaptive --spec-draft-n-max N`), and never carry tok/s numbers between
  different model files, commits, or cache types.

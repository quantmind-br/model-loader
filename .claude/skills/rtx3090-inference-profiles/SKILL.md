---
name: rtx3090-inference-profiles
description: 'Use when creating, tuning, or fixing a model-loader profile/preset/launch config for local LLM inference on the dedicated RTX 3090 24GB — any backend kind (llama-server, beellama-cpp, buun-llama-cpp, vllm, sglang, dflash). Triggers: user wants to run a model (GGUF, safetensors, AWQ, GPTQ) with a target context window; wants more context, more tok/s, or better quality from the 3090; hits OOM/"CUDA out of memory" at launch or on long prompts; asks which KV cache type, quantization, or kv-cache-dtype fits in 24GB VRAM; mentions DFlash, MTP, speculative decoding, gpu-memory-utilization, or max-model-len; or asks which backend to pick for a model — even if they never say "profile".'
---

# RTX 3090 Inference Profile Builder

Build a validated schemaVersion-3 profile in `~/.config/model-loader/profiles/<id>.json` that
launches a model through the best backend for it, maximizing quality within **~23.2 GiB usable
VRAM** (dedicated card, no display; 16 CPU cores). Done = profile passes validate, launches,
and has measured numbers recorded in its description.

## Hard rules (read before anything)

- **NEVER run backend binaries by hand** — only `model-loader instance start <id>`. All
  inference goes through the proxy at `127.0.0.1:4321`; the OpenAI `model` param = profile ID.
- **NEVER set `"port"` in args** — reserved, silently stripped; the manager assigns ephemeral
  ports.
- **NEVER invent flags.** Read the live schema first:
  `~/.config/model-loader/backends/schemas/<backend-id>.json` (`flags` map = validation source
  of truth). Unknown args = hard ERROR. A flag the binary genuinely supports but the schema
  lacks goes in `extraArgs` (warning only) — or refresh via the backend-schema-update skill.
  Never hand-edit schema JSON.
- **Schema enum presence ≠ hardware support.** The schemas describe what the *binary* accepts,
  not what SM86 silicon can run. fp8/mxfp8/mxfp4 compute paths need SM89+. Check the Ampere
  matrix in the reference file before accepting any quantization/kv-dtype value.
- Existing profiles in `~/.config/model-loader/profiles/` are proven anchors — mirror their
  arg spelling and their description breadcrumbs (quant, KV, ctx, peak GB, tok/s, acceptance).

## Backend dispatch

| Kind | Catalog id | Model format / ref | Reference file |
|---|---|---|---|
| llama-server | llama-cpp-default, llama-cpp-cuda | GGUF, local path | references/llama-family.md |
| beellama-cpp | beellama-rtx3090 | GGUF (+DFlash drafter), local path | references/llama-family.md |
| buun-llama-cpp | none — must `backend add` first | GGUF, local path | references/llama-family.md |
| vllm | vllm-default | safetensors/AWQ/GPTQ, path or HF repo id | references/vllm-sglang.md |
| sglang | sglang-default | safetensors/AWQ/GPTQ, path or HF repo id | references/vllm-sglang.md |
| dflash | lucebox-dflash | GGUF, positional (model field) | references/dflash.md |

User named a backend explicitly? Honor it (after the format check); mention an alternative only
when measurably better. Backend unspecified? GGUF + MoE with MTP head → llama-cpp-default
(draft-mtp); GGUF dense with a DFlash drafter available → beellama-rtx3090; other GGUF →
beellama-rtx3090; safetensors/AWQ/GPTQ → vllm-default. (Measured rationale in llama-family.md
§Speculative.) **Read exactly ONE reference file once the backend is chosen.**

## Workflow

1. **Gather**: model (local path + file/dir size, or HF repo id), backend, target context
   window, bias (precision | balanced | max-context). Model missing locally? GGUF single file →
   `model-loader model download <repo> <file> --wait`; safetensors repo → `hf download <repo>
   --local-dir /home/diogo/models/huggingface/<org>/<repo>`.
2. **Resolve backend**: entry exists in `~/.config/model-loader/backends/catalog.json`? buun →
   see its section in llama-family.md (registration flow).
3. **Format check**: GGUF ↔ llama family/dflash; safetensors/AWQ ↔ vllm/sglang. Mismatch →
   propose the matching backend, don't force it.
4. **VRAM budget**: weights floor + KV estimate vs 23.2 GiB, using the reference file's tables.
   Estimates pick the starting point; **nvidia-smi decides the truth**.
5. **Apply the context adjustment rule** (below) — settle final ctx + quant combo.
6. **Write the profile**: `model-loader profile create --id <id> --backend <backend-id>
   --file - <<'EOF' ... EOF` (id regex `^[a-z0-9]+([._-][a-z0-9]+)*$`; omit `meta` — the CLI
   fills timestamps; omit `port` always).
7. **Validate**: `model-loader profile validate <id>` must pass. Unknown-arg error → re-check
   schema spelling; binary-real but schema-absent → move to `extraArgs`.
8. **Launch + calibrate**: `model-loader instance start <id>` →
   `model-loader instance logs <id> | tail -50` (model + drafter loaded, KV-size line,
   `n_ctx_train` warning) → `nvidia-smi --query-gpu=memory.used --format=csv`.
   If `instance logs`/`instance list` say "not found" while the process is demonstrably up
   (proxy-owned instance), read the log file directly:
   `~/.local/state/model-loader/logs/<profile>-<port>.log`.
   Under ~22.5 GiB → raise ctx (KV is linear); over budget or OOM → the backend file's
   sacrifice ladder. **One change per iteration, re-measure.**
   Calibrating on a busy proxy: any other client request evicts your instance mid-measurement
   (no drain). Calibrate when the proxy is quiet, or prefill progressively in chunks (90k →
   150k → 250k) so the slot prefix cache survives reloads.
   An open TUI session holds `~/.local/state/model-loader/model-loader.lock`, blocking
   `instance start/stop` and `benchmark` (identify the holder with `fuser` on the lock file).
   A chat request through the proxy still triggers a profile swap while the CLI is
   lock-blocked.
9. **Benchmark**: `model-loader benchmark --profile <id> --mode llama-bench` for tok/s;
   precision bias → also one quality mode (`instruction-bench` or `mmlu-bench`). Note: the
   benchmark reloads the instance itself (pid changes) — harmless.
10. **Record**: quant, KV types, ctx, peak GiB, tok/s (+ acceptance % if speculative) into the
    profile description and tags — future tuning depends on these breadcrumbs. If launch or
    calibration cannot happen now, record estimates + "calibration pending" instead — never
    present estimates as measurements.

## Context adjustment rule

The user's target ctx is a band, not a contract: move it within **−25% / +50%**, only when
crossing a real cliff, and always say so:

- **RAISE** when headroom is already paid for (>1.5 GiB free at target) — free context. Cap at
  the model's native ctx (`n_ctx_train` in launch logs / `max_position_embeddings` in config)
  unless the user explicitly wants RoPE/YaRN extension.
- **LOWER** (requires a named, ≥10% gain) when it buys: one weight-quant tier up (quality), one
  KV-type tier up (long-ctx quality), full GPU offload, or (vllm) avoiding `enforce-eager`.
- Never drop below 75% of the request silently. Bias tie-breaker: precision → spend headroom on
  quant tiers; max-context → spend on ctx.

## Common mistakes

| Mistake | Reality | Fix |
|---|---|---|
| `"port"` in args | Reserved, silently stripped | Omit; clients hit proxy :4321, model = profile id |
| "fp8 is in the schema enum, so it works" | Enum = binary accepts it; SM86 has no FP8 compute | Ampere matrix in vllm-sglang.md; int4 AWQ/GPTQ (marlin) or bf16 |
| vllm/sglang profile without `served-model-name` | Proxy forwards `model: <profile-id>` unmodified; vllm 404s every request | Set `"served-model-name": "<profile-id>"` |
| Inventing flags from memory | Unknown args = validation ERROR | Read live schema `flags` first |
| Real flag rejected by validate | Schema pinned older than binary | `extraArgs` (e.g. `--n-cpu-moe`, `--spec-draft-type-k`) |
| Running llama-server/vllm by hand | Fights the process manager | `model-loader instance start` only |
| Profile for buun without catalog entry | Kind exists in code, not in catalog → validate fails | `backend add` + `schema refresh` flow (llama-family.md) |
| HF repo id as model for llama family | os.Stat fails — repo ids legal only for vllm/sglang | Download the GGUF first |
| Trusting VRAM estimates | CUDA context + fragmentation eat ~1 GiB; long-prompt prefill grows the pool at runtime | nvidia-smi after launch AND after a long-prompt test |
| Carrying tok/s across quants/backends/KV types | Not comparable | Re-benchmark per profile; record in description |
| Hand-editing schema/presentation JSON | Breaks `Source.Editable`/`RefreshSchema` | Web Customize mode or backend-schema-update skill |

## Red flags — stop and re-read the reference file

- "It's in the enum, so the GPU supports it"
- "I'll skip validate, the JSON looks right"
- "Estimates say it fits, no need for nvidia-smi"
- "Idle VRAM is fine, ship it" (long prompts grow the CUDA pool — test one)
- "I'll just run the server binary quickly to check"

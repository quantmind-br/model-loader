# ik-llama-cpp on the dual-3090 rig — ikawrakow fork (`t0002-889-g3bb0e9f0`, 2026-07-09)

Catalog id **`ik-llama-cpp`**, kind **`ik-llama-cpp`**, executable
`backends/ik_llama.cpp/build/bin/llama-server`. It is the **ikawrakow fork** of llama.cpp — a
CPU/GPU-hybrid **MoE + MLA specialist** (DeepSeek / Kimi / GLM giant-MoE, MLA KV compression,
the ik `_R4`/interleaved repacked quant families, `-ser` expert reduction). It speaks the
`llama-server` HTTP surface, so it plugs into the proxy exactly like llama.cpp-stable.

**It shares llama-server's arg builder.** `processmgr` routes `ik-llama-cpp` through the SAME
`buildLlamaArgs` as `llama-server` (emits `--model <path>` first from the profile `model` field;
GGUF **path** only — a bare HF repo id is rejected by the validator, same as the llama family).
Recovery/liveness treat it as a `llama-server` process. So the launch mechanics, GPU pinning, and
proxy hot-swap behavior are identical to `references/llama-family.md` — **read that file for the
shared llama.cpp knowledge; this file is only the ik deltas.**

**Its schema is HAND-CURATED, not `--help`-parsed.** ik_llama.cpp's `--help` uses the old
pre-`arg.cpp` format (2-space indent, `group:` headers) that `llamahelp` cannot parse, so
model-loader ships a **hand-authored curated schema** (`backendschema/curated_ik.go`,
`source.editable`) as the *only* flag source — the ik binary's `--help` is **never executed**.
The on-disk copy is `~/.config/model-loader/backends/schemas/ik-llama-cpp.json`. This schema is a
**deliberate subset** of the fork's real flags: a flag the binary supports but the schema lacks
goes verbatim in `extraArgs` (raw passthrough — emits an unknown-flag warning, never
enum/type-checked, never blocks; `internal/service/validator/rules.go`). **`backend schema
refresh <id>` DOES re-derive it** — it deletes the on-disk schema and regenerates from the curated
Go source (never from ik's `--help`), overwriting any hand-edits (`manager.go RefreshSchema`). The
`source.editable` flag only prevents overwrite on *incidental* re-runs (AddBackend retries /
catalog-ensure), NOT on an explicit refresh. So after a model-loader curated-source change, run
`backend schema refresh ik-llama-cpp` to propagate; a fork *binary* rebuild changes nothing (ik
`--help` is never parsed). Never hand-edit the JSON — refresh is the update path.

## When ik-llama-cpp over llama.cpp-stable / beellama

| Situation | Pick | Why |
|---|---|---|
| DeepSeek-arch / MLA model (V3/R1-class, Kimi) GGUF | **ik-llama-cpp** | `-mla` (MLA implementation levels 0–3, default 3) is ik's reason to exist — compressed MLA KV that mainline handles worse |
| GLM-DSA-arch model needing sparse attention | **ik-llama-cpp** | `-dsa` (GLM DSA sparse attention; GLM-DSA arch only) + `-fidx`/`-dsatk` — ik-only |
| Giant MoE you want to shrink on-GPU without a drafter | **ik-llama-cpp** | `-ser K,thresh` = REAP-from-CLI expert reduction (run fewer active experts); `-fmoe`/`-ger` fused/grouped routing |
| ik `_R4`/interleaved repacked quants | **ik-llama-cpp** | `-rtr`/`--run-time-repack` repacks interleaved variants at load for faster matmul (⚠ implies `--no-mmap`) |
| Plain dense GGUF, agent/tool-calling, MTP/DFlash asset exists | **llama.cpp-stable / beellama** (llama-family.md) | mainline's spec surface (list-valued `spec-type`, `cache-reuse`, EAGLE-3, upstream DFlash) is richer and better-measured here |

ik is **opt-in for its architectures/features** — default GGUF routing stays llama.cpp-stable.
Rig policy is unchanged: **naive CPU expert spill (`--cpu-moe`/`--n-cpu-moe`) stays
explicit-request-only**; ik's sanctioned in-GPU MoE levers are `-ser` reduction + MLA, not host
offload.

## ik deltas that change profile-writing (all validate in `args` unless flagged extraArgs)

### Attention & MoE optimizations
- **`flash-attn` default `on`** (curated + binary) — ik enables FA by default; keep `"flash-attn":
  "on"` explicitly for quantized KV / graph split.
- **`mla-use` (`-mla`) default `3`**, enum `0|1|2|3` — MLA implementation level for DeepSeek and
  other MLA models (`docs/parameters.md`). Only meaningful on MLA-arch models; harmless-but-inert
  elsewhere.
- **Fused kernels default ON** — disable only to debug: `--no-fused-moe`, `--no-fused-up-gate`,
  `--no-fused-mul-multiadd` (all default enabled; they *speed up* MoE). `-ger`/`--grouped-expert-routing`
  (default off) enables grouped expert routing.
- **`smart-expert-reduction` (`-ser`) `K,thresh`** — default disabled (`-1,0`). `-ser 1,6` forces
  6 active experts regardless of the model default ("REAP from the command line") — the on-GPU
  way to shrink a MoE's compute. Curated as a string; pass `"smart-expert-reduction": "1,6"`.
- **`attention-max-batch` (`-amb`) default `0`** (unlimited) — caps the K*Q attention scratch in
  MiB; set a value only if attention scratch OOMs on huge prompts.
- **`graph-reuse` (`-gr`) default ON** (binary; `docs/parameters.md` L95 — "for models with fast
  TG, 100+ t/s"). Disable with `--no-graph-reuse`. (The curated schema annotates the default as
  off because it models the *flag*, not the runtime state; the runtime default is on.)
- **DSA:** `--dsa` (GLM-DSA arch only), `--fused-indexer-topk` (`-fidx`, DSA only), `--dsa-top-k`
  (`-dsatk`). Ignore on non-GLM-DSA models.

### Split modes — **`none` / `graph` / `layer` ONLY** (no `tensor`, no `row`)
ik's `split-mode` enum is `{none, graph, layer}` — there is **no `tensor` and no `row` mode**.
`layer` is the default and the safe multi-GPU choice; `graph` is ik's graph-partition split with
GPU-exchange precision knobs (`-grt`/`--graph-reduce-type` default `f32`, `-gap`/`--graph-attn-precision`
default `f16`, `-smf16`/`-smf32`) — **all schema-absent → extraArgs**. `tensor-split` (fractions)
still applies to `layer`/`graph`, and the rig rule holds: **symmetric `"0.5,0.5"` only, never
asymmetric** (see `SKILL.md §Hard rules`, `dual-gpu.md`). All the llama-family multi-GPU cautions
about drafts+split still apply — measure per ctx.
- **Measured 2026-07-12 (RDson Qwen3.6-35B-A3B IQ4_KS, 256k, full offload, 5-run llama-bench):
  `graph` beat `layer` by +16.2% @25% / +20.3% @50% / +31.1% @90% fill (5% fill was a +4.8% tie).**
  Coherence + shell-hostile tool-call verified on `graph` at LOW fill only (766-word PT, valid
  `mkdir -p "/tmp/My Project" && ls` JSON). **`layer` stays the conservative default; `graph` is a
  measured per-model speed lever — promote it only after a high-fill (25–90%) coherence check on
  that model**, since ik's graph split can emit incoherent output on some models/offloads. On graph
  incoherence, first try the upstream fallback `-cuda graphs=0` (disables CUDA graphs; distinct from
  `--no-graph-reuse`), then fall back to `layer`. Never use `GGML_CUDA_P2P` for ik (it auto-enables
  peer access via its own `ggml_cuda_set_peer_access`; see `dual-gpu.md`).

### KV cache
- Extended type set: `f32, f16, bf16, q8_0, q4_0, q4_1, iq4_nl, q5_0, q5_1, q6_0, q8_KV`
  (ik adds **`bf16`, `q6_0`, `q8_KV`** over the mainline set). Both `cache-type-k`/`-v` default
  `f16`; draft KV via `cache-type-k-draft`/`cache-type-v-draft`.
- **Tool-calling profiles: q8_0/q8_0, never q4_0**, symmetric (same rule + `#20866` class as
  llama-family).
- **MLA models already compress the KV** — quantizing it further with these types rarely helps
  (`docs/parameters.md`); prefer f16/q8_0 KV on DeepSeek-MLA and lean on MLA level instead.
- `--no-kv-offload` (`-nkvo`) keeps the KV on host RAM (rarely wanted on this rig).

### RAM prompt cache — ik's coding-agent lever (NOT `cache-reuse`)
ik has **no `cache-reuse` and no `swa-checkpoints`** (those are llama.cpp-stable keys). Its agent
prompt-reuse mechanism is the **RAM cache**:
- **`cache-ram` (`-cram`) default `8192` MiB** (`-1` = unlimited, `0` = disable) — "very useful
  when variations of the same prompt are re-sent (coding agents)". Raise it for agents:
  `"cache-ram": 16384`.
- **`cache-ram-similarity` (`-crs`) default `0.50`** — similarity threshold that triggers a cache
  hit. `--cache-ram-n-min` (min cached tokens to trigger; default 0) is **schema-absent →
  extraArgs**.
- **Context shift / checkpoints:** `context-shift` (`auto|on|off`), `ctx-checkpoints` (+
  `-interval`, `-tolerance`, `-eviction` `fifo|variance|auto`, default `variance`). These are ik's
  infinite-generation controls; for agents, size ctx properly rather than relying on shift.

### Memory / load
- **`run-time-repack` (`-rtr`) IMPLIES `--no-mmap`** (`common.cpp` sets `use_mmap=false`) — repacks
  interleaved ik-quant variants at load for faster matmul, but **disabling mmap slows proxy
  hot-swaps** (no page-cache reuse across swaps) and raises load time. Worth it for a
  long-resident ik-quant model; skip it for a model you swap often.
- `mlock`, `no-mmap`, `check-tensors` behave as in llama-family. `--merge-qkv` (`-mqkv`, also
  disables mmap) and `--merge-up-gate-experts` (`-muge`) are **extraArgs** (schema-absent).

### `--fit` and MoE offload
- **`--fit` (+`--fit-margin`) — the curated schema defaults it OFF** (unlike llama.cpp-stable where
  fit is default-on). Per rig policy pin `"n-gpu-layers": 99` and set `ctx-size` explicitly
  regardless, so nothing silently spills experts to CPU.
- `--cpu-moe` / `--n-cpu-moe N` / `--defer-experts` are curated (they validate) but **naive CPU
  expert spill is banned by rig policy** (explicit-request-only, same as llama-family). ik's
  on-GPU shrink lever is `-ser`, not `--n-cpu-moe`.

## Sampling & anti-loop (same discipline as llama-family)
ik's **binary sampling defaults are temp `0.8` / top-k `40` / top-p `0.95` / min-p `0.05`**
(verified `common/sampling.h`). They are wrong for most models — do the model-research pass and
put the official preset in `args`. (The curated schema annotates top-p `0.9` / min-p `0.1`; that
is cosmetic display metadata, not enforced, and irrelevant once you set sampling explicitly.)
Anti-loop baseline **`repeat-penalty 1.05` + `repeat-last-n 256`**; **no `dry-*`** (user policy —
DRY flags are not even in the ik curated schema). `top-n-sigma` is present (curated) — use it as
in llama-family to hold speculative acceptance at calibrated temp.

## Chat template, reasoning, tools
- **`--jinja`** enables the Jinja chat-template engine — set `"jinja": true` for reliable
  tool-calling (without it ik only accepts a small built-in template set and warns).
- `chat-template` is curated; `--chat-template-file` is **extraArgs** (schema-absent). `reasoning-format`
  is curated (thought-tag extraction: `none`/`deepseek`/…; unknown names mapped internally).
- **`--peg` is a no-op in this build** — the CLI handler consumes the flag and does nothing
  (`common.cpp` `if (arg == "--peg") { return true; }`). It is **not** in the curated schema
  (→ extraArgs if passed) and does **not** enable a tool parser here. Tool/reasoning parsing is
  driven by `--jinja` + the model's template + `--reasoning-format`; do not rely on `--peg`.

## Speculative decoding — free-string `spec-type` in the ik dialect
In the ik curated schema `spec-type` is a **plain string flag (no enum)** — any payload validates
(unlike llama.cpp-stable's list-valued *enum*). Payload form is `TYPE:k=v,...`. The ik dialect
types (curated HelpText) are: `none, draft, dflash, mtp, ngram-cache, ngram-simple, ngram-map-k,
ngram-map-k4v, ngram-mod, suffix` — i.e. **bare `mtp`/`dflash` (like beellama), NOT upstream
`draft-mtp`/`draft-dflash`.** Companions: `spec-autotune` (autotune the spec params), `model-draft`
(`-md`), `ctx-size-draft` (`-cd`), `gpu-layers-draft` (`-ngld`), `cache-type-k-draft`/`-v-draft`.
Because `spec-type` is unchecked, a typo will not fail validation — **confirm the head/drafter
actually loads in the launch log** (config fields lie), same as every other backend.

## Profile template (adapt; drop MoE/spec keys when not needed)

```json
{
  "schemaVersion": 3,
  "id": "<model>-<variant>-<ctx>k",
  "name": "<Human Name> (<ctx>k, ik-llama-cpp)",
  "description": "<quant> MLA<level>, <K>/<V> KV, cache-ram <N>, ~<N> GiB peak, ~<N> tok/s, template <source>.",
  "model": "/abs/path/target.gguf",
  "args": {
    "ctx-size": 65536, "n-gpu-layers": 99,
    "flash-attn": "on", "mla-use": 3,
    "cache-type-k": "q8_0", "cache-type-v": "q8_0",
    "cache-ram": 16384,
    "jinja": true,
    "temp": 0.6, "top-k": 20, "top-p": 0.95, "min-p": 0,
    "repeat-penalty": 1.05, "repeat-last-n": 256
  },
  "extraArgs": [],
  "launch": { "defaultBackground": true, "backendId": "ik-llama-cpp",
              "env": [{"key":"CUDA_DEVICE_ORDER","value":"PCI_BUS_ID"},
                      {"key":"CUDA_VISIBLE_DEVICES","value":"1"}],
              "restart_policy": "none", "max_restarts": 3, "backoff_seconds": 5 },
  "pinned": false
}
```

Set sampling from the model-research pass (the template shows a placeholder precise-coding preset);
`ctx-size` + KV types always explicit; pin GPU1 unless a justified symmetric `layer`/`graph` split.
`-ser`/`-rtr`/`split-mode graph` are added per model, not by default.

## extraArgs quick list (real ik flags absent from the curated schema)
`--peg` (no-op), `--graph-reduce-type`/`-grt` (default f32), `--graph-attn-precision`/`-gap`
(default f16), `-smf16`/`-smf32`, `--k-cache-hadamard`/`-khad`, `--v-cache-hadamard`/`-vhad`,
`--merge-qkv`/`-mqkv` (disables mmap), `--merge-up-gate-experts`/`-muge`, `--cache-ram-n-min`,
`--chat-template-file`, `--no-mmproj-offload`, and any `split-mode` value other than
`none`/`graph`/`layer`. All are raw passthroughs (warning only) — verify their effect in the log.

## Gotchas
- **Schema updates come from the curated Go source, never ik `--help`.** A fork *binary* rebuild
  changes nothing (ik `--help` is never parsed); only a `curated_ik.go` change does, and it reaches
  the on-disk schema via `backend schema refresh ik-llama-cpp` (deletes + regenerates, wiping any
  hand-edits). Real-but-absent flags go in `extraArgs`; never hand-edit the JSON.
- **`-rtr` disables mmap** → slower proxy hot-swaps; only worth it for a long-resident ik-quant.
- **`graph-reuse` helps only fast-TG models** (100+ tok/s) — otherwise leave default; `--no-graph-reuse`
  to disable.
- **MLA level / DSA are architecture-gated** — `-mla` matters on DeepSeek-MLA, `-dsa` on GLM-DSA;
  inert elsewhere.
- **Agent prompt caching = `cache-ram` (+`-sps` slot similarity), not `cache-reuse`** — the latter
  does not exist on ik.
- **`--fit` is default-off here** (opposite of llama.cpp-stable) — pin `-ngl 99` + explicit ctx
  regardless (rig policy).
- Launch-log lines to read every time: model + MLA/DSA/fused-MoE init, KV buffer sizes,
  `Chat format` (jinja/template OK), draft/`mtp`/`dflash` load + acceptance, RAM-cache hit lines,
  which GPUs. `failed to mlock` = harmless RLIMIT warning.

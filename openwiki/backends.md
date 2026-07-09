# Backends

The backend catalog (`~/.config/model-loader/backends/catalog.json`) is a flat list of registered backends. Each profile selects one via `launch.backendId`; the resolver (`backendcatalog/Resolver`) maps a profile to its binary path and `BackendKind`, and `processmgr/args.go::BuildArgsForBackend` dispatches on kind to produce the final command line.

There is **no default backend** — the resolver falls back to `default_backend_id` (unset) then a llama-server PATH lookup.

## `BackendKind` (9 values)

| Kind | Example catalog id(s) | Arg shape | Schema |
|------|----------------------|-----------|--------|
| `llama-server` | `llama.cpp-stable`, `llama.cpp-nightly` | `--model` + canonicalized flags (`ngl`→`n-gpu-layers`); dual-GPU via `split-mode`/`tensor-split`/`main-gpu` | live `--help` parse, falls back to embedded **v9761** (`llamahelp`) |
| `beellama-cpp` | `beellama-rtx3090` | llama.cpp fork; curated overlay (kv-unified, spec-draft-hf) | `beellamahelp` / `dflashhelp` |
| `dflash` | `lucebox-dflash` | native C++ speculative-decoding server; positional model + `--draft` | `dflashhelp` |
| `vllm` | `vllm-stable`, `vllm-nightly`, `sndr-vllm` | positional model, `--max-model-len`; `PYTHONUNBUFFERED=1` injected; dual-GPU via `tensor-parallel`/`gpu-split` | `vllmhelp` |
| `sglang` | `sglang-stable`, `sglang-nightly`, `sglang-unlimited`, `sglang-dflash` | `python -m sglang.launch_server`; bundles CUDA 12.8 for flashinfer JIT | `sglanghelp` |
| `unsloth` | `unsloth-rtx3090` | `unsloth studio run … -H 127.0.0.1`; captures `sk-unsloth-…` auth token post-load | `unslothhelp` |
| `tabby` | `tabby` | TabbyAPI `main.py` (EXL2/EXL3, ExLlamaV2/V3); model **dir** → `--model-dir`/`--model-name`; wrapper forces `--host 127.0.0.1 --disable-auth true`; bools emit `--flag true`; `gpu-split`/`autosplit-reserve`/`draft-gpu-split` are nargs+ (whitespace-delimited per element → spans 2 cards); auto-detects exl2 vs exl3; `PYTHONUNBUFFERED=1` injected | `tabbyhelp` (Pattern C, embedded) |
| `buun-llama-cpp` | `buun-rtx3090` | buun llama.cpp fork; embedded schema | `buunhelp` |
| `ik-llama-cpp` | (operator-registered) | llama.cpp fork (ikawrakow); MLA, fused MoE, run-time repack, SER, extra KV quants (q6_0/q8_KV); binary named llama-server; --model + canonicalized flags | pure curated (curated_ik.go), --help never parsed |

Per-kind variants (stable/nightly/unlimited/dflash) each resolve to different `backends/<id>/…` checkouts.

## Arg dispatch

`processmgr/args.go::BuildArgsForBackend(p profile, exe, kind, port)` returns `(args []string, env []string)`. The dispatch table lives at the top of `args.go`; the per-kind builders are split into `buildLlamaServerArgs`, `buildVLLMArgs`, `buildSGLangArgs`, `buildUnslothArgs`, `buildTabbyArgs`, `buildDFlashArgs`, `buildBeellamaArgs`, `buildBuunArgs`.

## Python-backend buffering

vLLM, SGLang, Unsloth, and Tabby all run Python at some point. **`PYTHONUNBUFFERED=1` is injected at spawn** (`processmgr/launch.go`). Without it, stdout block-buffers and the Server-tab log lands in late bursts. The `monitor` log tailer is intentionally backend-agnostic — the unbuffering is a spawn-time fix, not a consumer-side patch.

## `backends/*` checkouts

The repo has a top-level `backends/` directory which holds **vendored / cloned source trees** (`backends/llama.cpp-stable/`, `backends/vllm-nightly/`, `backends/sndr-vllm/`, etc.). The directory is **gitignored** (`.gitignore` L60). Manage each as its own upstream checkout — there is no submodule and nothing to sync.

Notable per-backend notes:

- **SNDR (`sndr-vllm`)** is a `vllm` variant with a strict pin and per-commit wheel index. See [operations/sndr](operations.md#sndr-genesis-backend) and `docs/sndr-backend.md` for the full runbook. Two env vars are load-bearing and **not** in the model YAML `patches:` block — every boot must carry them in `launch.env`:
  - `GENESIS_ENFORCE_VERSION_RANGE=1` (mandatory, turns on the version-gate)
  - Plus dual-3090 desktop headroom: `gpu-memory-utilization 0.84`, `GENESIS_ENABLE_PN95_TIER_AWARE_CACHE=0`, `NCCL_P2P_DISABLE=1`, `disable-custom-all-reduce`, `distributed-executor-backend mp`
- **Dual-GPU (RTX 3090)** — use **`0.5,0.5`** for `tensor-split` / layer-tensor allocation when llama.cpp `split-mode` spans both cards. **Never use asymmetric values** like `0.45,0.55` — this is a project anti-pattern. For spare-GPU0 scenarios, pin a single model to one card or run pin-per-GPU two models (see `skill://rtx3090-inference-profiles`).
- **vLLM / SGLang** must set `served-model-name` to the **profile id** so the proxy's implicit swap passes the backend's model-name validation.

## Validation schemas

Each backend has a `BackendValidationSchema` (JSON file under `backends/schemas/<id>.json`):

- **Pattern A — live `--help` + overlay** — `backendschema.Manager.RefreshSchema(id)` runs the binary's `--help`, parses flags into `FlagSpec`s, then layers curated additions from `essentialSeed` and per-backend overlays (e.g. `CuratedLlamaSchema` for `llama-server`)
- **Pattern B — curated Go** — for backends whose `--help` is too unstable to parse live, the schema is hand-written in `internal/service/<kind>help/curated.go`
- **Pattern C — embedded rows** — for backends with stable embedded schemas (e.g. `tabbyhelp`), the full `BackendValidationSchema` lives in a `.go` source file with `//go:embed` for any associated golden fixtures

The schema has a `Source.Editable` flag. When `true`, `RefreshSchema` preserves manual edits across `--help` re-parsing. The web editor's **Customize mode** flips this flag on.

`docs/profile-schema.json` is the canonical JSON Schema for `domain.Profile` (`schemaVersion: 3`, `additionalProperties: false`) and **must stay in sync** with `internal/domain/profile.go`. Update the JSON Schema in the same commit as any `Profile` field change.

The full update workflow lives in `.claude/commands/backend-schema-update.md`. After changing any `*help` package, regenerate the embedded golden with:

```sh
go run ./cmd/regenerate-schemas
```

## Registering a backend

CLI:

```sh
model-loader backend add llama-cpp-stable \
  --executable /usr/local/bin/llama-server --kind llama-server
```

TUI: Backends tab → `n` → fill name / executable / kind → submit.

For SNDR specifically, see the [sndr-backend.md runbook](https://github.com/quantmind-br/model-loader/blob/main/docs/sndr-backend.md) (`scripts/setup-sndr-backend.sh` is idempotent).

## Probing

`backend probe [id]` runs each binary with `--help` (or backend-appropriate quick check) and records liveness + version. TUI: Backends tab → `P` probes all (3 s/backend cap).

## Implementation notes for future agents

- Adding a new `BackendKind` requires: (1) a new `domain.BackendKind` constant, (2) a `build<Kind>Args` builder in `processmgr/args.go`, (3) a `*help` package in `internal/service/`, (4) registration in `backendschema.RegisterDefaults`. Then update the dispatcher switch.
- Arg builders receive the **ephemeral port** as a `port` arg — never let a profile's own `port` flag reach the backend (it's reserved and stripped at profile-store load time)
- For Python backends, always set `PYTHONUNBUFFERED=1` in the spawn env, **not** in the schema defaults — schema defaults are for user-facing flags only
- `BuildArgsForBackend` is called from `prepareLaunch`; the resolver is invoked **once** per launch — if you need a different exe per profile, register separate backends rather than mutating the resolver at runtime
- After any `*help` change, run `go test ./internal/service/<kind>help/... -update` to regenerate the goldens, then `go test ./...` to confirm nothing else drifted

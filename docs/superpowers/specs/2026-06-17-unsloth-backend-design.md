# Unsloth backend support — design

**Date:** 2026-06-17
**Status:** Proposed (awaiting review)
**Author:** brainstormed with Claude

## Goal

Add a first-class `unsloth` backend **kind** to model-loader that launches
`unsloth studio run` (alias: `unsloth run`) as a managed, headless inference
server and proxies OpenAI/Anthropic requests to it through the existing
`httpproxy`. The unsloth-issued per-boot Bearer token is captured from the
launch log and injected on every upstream request.

Decisions locked during brainstorming:

- **Scope:** full `unsloth run` integration (a real kind), not "reuse the
  llama-server kind with unsloth GGUFs".
- **Auth/key capture:** read the printed `sk-unsloth-…` key from the process
  stdout log (Approach 1). The key line doubles as the "fully ready" signal.
- **Final deliverable:** update the `rtx3090-inference-profiles` skill to cover
  the new kind.

## Background — how `unsloth run` actually works (verified against source)

Source read at `backends/unsloth` (`unsloth` 2026.6.7, installed at
`~/.local/bin/unsloth`; Studio set up at `~/.unsloth/studio`).

- `unsloth run` aliases `unsloth studio run` (`unsloth_cli/__init__.py`). It
  re-execs (`os.execvp`) into the Studio venv's `unsloth` console-script
  (`unsloth_cli/commands/studio.py:1188`) — **PID is preserved** across the
  exec, so process tracking survives.
- It starts a uvicorn server that manages a **`llama-server` child** and
  exposes, on one port: `POST /v1/chat/completions` (OpenAI),
  `POST /v1/messages` (Anthropic), `GET /v1/models`. It is **GGUF-only**.
- It **blocks in the foreground** and handles **SIGTERM** gracefully, tearing
  down its `llama-server`/`cloudflared` children (`studio/backend/run.py`).
- **Auth is mandatory and non-disableable.** Every endpoint except
  `GET /api/health` requires `Authorization: Bearer sk-unsloth-…`
  (`studio/backend/auth/authentication.py`). The key is freshly minted each
  boot (`secrets.token_hex(16)`), stored **hashed** in `~/.unsloth/studio/auth/auth.db`
  — there is **no** fixed-key env var, config, or flag.
- The key is **always printed to stdout**, even with `--silent`, as
  `API Key:      sk-unsloth-<32hex>` (`studio/.../studio.py:1303,1343`). It is
  printed **after** the model finishes loading — i.e. exactly when the server
  can serve inference. (`/api/health` returns 200 *before* the model loads, so
  it is an unreliable readiness signal by itself.)
- `--model` accepts an **HF repo** (`unsloth/Repo-GGUF`, optional `:VARIANT`)
  **or a local GGUF path**. `--gguf-variant` selects the quant separately.
- Run-specific flags: `--gguf-variant`, `--max-seq-length`, `--port/-p`,
  `--host/-H` (default `127.0.0.1`), `--parallel`, `--tensor-parallel`,
  `--enable-tools/--disable-tools`, `--load-in-4bit`, `--silent/-q`,
  `--yes/-y`, `--cloudflare/--no-cloudflare`, `--secure`. **Unknown flags pass
  through to `llama-server`** (`-c`, `-ngl`, `--flash-attn`, `--cache-type-*`,
  `--jinja`, `--chat-template-file`, sampling…), minus a Studio-managed
  denylist (`studio/backend/core/inference/llama_server_args.py`): model
  identity, `--host/--port/--path`, auth/TLS, the built-in web UI,
  `--embedding/--rerank`, `--np/--parallel`, `--tools`.
- `--api-only` exists on the plain `studio` callback but **not** on `run`; the
  `run` server still serves a static frontend on its port. This is harmless —
  model-loader only forwards `/v1/*`. No browser is opened (CLI is headless).

## Architecture

A new kind wired exactly like the existing embedded-schema backends
(`vllm`, `sglang`, `dflash`), plus a small, contained change to the
readiness/auth path which is the only genuinely novel piece.

### 1. New kind + launcher

- `domain.BackendKindUnsloth = "unsloth"` (`internal/domain/backend.go`).
- `backends/unsloth/unsloth-serve.sh` — launcher (mirrors `vllm-serve.sh`),
  the catalog entry's `executable`. It forces the headless/safe flags and
  `exec`s so the PID is the server's:

  ```sh
  #!/usr/bin/env bash
  set -euo pipefail
  # `unsloth` re-execs into its own Studio venv (~/.unsloth/studio), so we do
  # not manage a local .venv here — just require the console-script on PATH.
  command -v unsloth >/dev/null 2>&1 || { echo "Error: 'unsloth' not found on PATH" >&2; exit 1; }
  exec unsloth studio run --silent --yes --no-cloudflare -H 127.0.0.1 "$@"
  ```

  model-loader appends `--model <repo|path> --port <ephemeral>
  [--gguf-variant …] [other schema/extra flags]`.

### 2. Schema (embedded, curated)

Reuse the `embeddedGenerator` path (same as sglang/vllm):

- New package `internal/service/unslothhelp/unslothhelp.go` exporting
  `EmbeddedSchema() domain.FlagSchema` built via `domain.BuildFlagSchema(
  "embedded-unsloth-v1", unslothRows)` with `[]domain.FlagSpecRow`.
- New `internal/service/backendschema/unsloth_generator.go`:
  `NewUnslothGenerator(schemaStore)` following the same generator shape as
  vllm/sglang — curated embedded schema, skip-when-`Source.Editable`, persist
  via `schemaStore`. Concretely, mirror `SGLangGenerator` (standalone struct
  holding `schemaStore`; `Generate` rejects a wrong kind, returns the existing
  schema when editable, else builds from `unslothhelp.EmbeddedSchema()` via
  `domain.FlagSchemaToBackend(fs, kind, backend.ID, src)` and saves). No
  executable resolver is needed — the catalog `executable` is the wrapper
  script path used verbatim.
- Register in `backendschema/register.go`:
  `m.Register(domain.BackendKindUnsloth, NewUnslothGenerator(schemaStore))`.
- `essentialSeed[domain.BackendKindUnsloth]` in `presentation.go`:
  `{"gguf-variant", "ctx-size", "n-gpu-layers", "parallel", "flash-attn",
  "cache-type-k", "cache-type-v"}` (curated highlights).

**Flag set** (`unslothRows`) — unsloth orchestration flags + high-value
`llama-server` passthrough flags. `host`/`port` are present but port is
manager-owned (injected at launch; never user-set, per the auto-port design).

| Long | Type | Notes |
|---|---|---|
| `model` | string | HF repo (`org/repo[-GGUF]`) or local GGUF path; emitted as `--model` |
| `gguf-variant` | string | e.g. `UD-Q4_K_XL` |
| `host` | string (127.0.0.1) | forced loopback by the wrapper; informational |
| `port` | int, IsPort | manager-injected |
| `max-seq-length` | int | unsloth flag |
| `parallel` | int | unsloth's own `--parallel` (decode slots); fine to set. Do NOT also pass raw `-np` as an extra arg — Studio denylists it. |
| `tensor-parallel` | bool | unsloth flag |
| `enable-tools` | bool | unsloth tool policy |
| `load-in-4bit` | bool | unsloth flag |
| `ctx-size` (`-c`) | int | llama passthrough |
| `n-gpu-layers` (`-ngl`) | int | llama passthrough |
| `flash-attn` | bool/enum | llama passthrough |
| `cache-type-k` / `cache-type-v` | enum | llama passthrough |
| `jinja` | bool | llama passthrough |
| `chat-template-file` | string | llama passthrough |
| sampling (`temp`, `top-p`, `top-k`, `min-p`, `repeat-penalty`, `seed`) | float/int | llama passthrough |

Anything the binary accepts but the schema lacks still works via the
profile's `extraArgs` (validator warning only). Studio-managed flags
(`--host/--port/--api-key/--model/-np`) must **not** be exposed as
user-editable schema flags.

### 3. Launch args

`internal/service/processmgr/args.go`:

```go
case domain.BackendKindUnsloth:
    return buildUnslothArgs(p), nil
// ...
func buildUnslothArgs(p domain.Profile) []string {
    // model under --model; sorted flags after; --port injected by launch.go.
    return buildArgs(p, argBuildOpts{skipKeys: []string{"model"}, modelFlag: "--model"})
}
```

`prepareLaunch` already injects `args["port"]` → emitted as `--port <n>`.
Model validation needs no change: `domain.LooksLikeHFRepo("unsloth/Repo-GGUF")`
is true (2-part repo id) and local paths pass `os.Stat`.

### 4. Readiness + auth (the novel part)

`/health` (used by today's `WaitHealthy`) does **not** exist on the Studio
port, and `/api/health` is up before the model loads. The reliable
"ready + key available" signal is the printed key line. So readiness becomes
**kind-aware** and returns the captured token.

**Data flow:**

```
processmgr.Launch → child stdout+stderr → inst.LogPath           (already exists)
prepareLaunch resolves kind → stamp RunningInstance.Kind         (new field)
httpproxy.launchNewBackend → ProcessMgr.WaitReady(inst, timeout) (new method)
   unsloth: scan inst.LogPath for /sk-unsloth-[0-9a-f]{32}/ until found|timeout
            → match = model loaded AND server ready AND token captured
   others : delegate to existing WaitHealthy(/health) → token ""
httpproxy.loadedBackend{authToken} ← token
newReverseProxy(port, authToken):
   if authToken != "" → Director adds "Authorization: Bearer <token>"
```

Changes:

- `domain.RunningInstance`: add `Kind BackendKind` (`json:"kind,omitempty"`).
  Set in `Launch` from `prepareLaunch`'s `resolvedKind` (thread through
  `launchPlan`).
- `processmgr.Manager` interface: add
  `WaitReady(inst domain.RunningInstance, timeout time.Duration, attemptID string) (authToken string, err error)`.
  Keep `WaitHealthy` (other callers/tests rely on it; `WaitReady` delegates to
  it for non-unsloth kinds). New helper `waitForLogToken(logPath, re, timeout)`
  tails the log file with the same capped-backoff loop.
- `httpproxy/handler.go launchNewBackend`: call `WaitReady` instead of
  `WaitHealthy`; store the token on `loadedBackend`.
- `httpproxy/server.go`: add `authToken string` to `loadedBackend`.
- `httpproxy/proxy.go`: `newReverseProxy(port int, authToken string)` — wrap
  the single-host Director to set the upstream `Authorization` header when a
  token is present. Add a comment making explicit this is **outbound** auth
  (proxy → Studio), distinct from the package's "no inbound auth" rule; the
  proxy itself remains unauthenticated on the client side.
- `validator/rules.go supportsHFRepo`: add `domain.BackendKindUnsloth`.
- Update `httpproxy/mocks_test.go` `stubManager` to implement `WaitReady`.

### 5. Catalog entry (no code change)

User adds it once; `unsloth` becomes a valid `--kind` automatically once the
generator is registered:

```
model-loader backend add --name "Unsloth RTX3090" --kind unsloth \
  --executable /home/diogo/dev/model-loader/backends/unsloth/unsloth-serve.sh
```

We do **not** auto-seed it into the default catalog (mirrors how vllm/sglang
are added on demand).

## Error handling & edge cases

- **Key not found before timeout:** `WaitReady` returns an error → existing
  launch-failure path (instance killed, surfaced to TUI/CLI). Reuse/extend the
  health-timeout sentinel with an unsloth-specific message.
- **Recovery / proxy restart:** the key line persists in `inst.LogPath`, so
  re-scanning on re-attach reconstructs the token. `RunningInstance.Kind` is
  persisted in `instances.json`, so recovered instances keep their kind.
- **Process teardown:** Studio reaps its `llama-server` child on SIGTERM; with
  `Setsid` the children share a session. Note residual risk of an orphaned
  `llama-server` on SIGKILL (acceptable; same class as other backends).
- **Studio not installed:** wrapper fails fast with a clear message; launch
  surfaces it. Prereq: `unsloth` on PATH + `unsloth studio` set up.
- **HF auth for gated models / cache location:** via profile env
  (`HF_TOKEN`, `HF_HOME`); `applyProfileEnv` overlays `os.Environ()`.

## Open risks to verify during implementation

- **`model` field handling.** model-loader's proxy forwards the request body
  with `model: <profile-id>` unchanged (vllm needs `served-model-name` to
  match). Must verify whether Studio's `/v1/chat/completions` validates the
  `model` field against the loaded model's id/alias. If it does: set the
  served name to the profile id (via a passthrough `--alias`/Studio option) or
  rewrite the field for the unsloth kind. Verify by launching and curling
  before declaring done.
- Exact stdout vs stderr stream of the key line (both are captured to the same
  log file, so the regex scan covers either) and absence of ANSI codes inside
  the `sk-unsloth-…` token (regex matches the raw token regardless).

## Testing strategy

- `unsloth_generator_test.go`: wrong-kind rejection; `Source.Editable` skip
  (copy `vllm_generator_test.go`).
- `register_test.go`: include `BackendKindUnsloth` in the expected set.
- `presentation_test.go`: unsloth essentials resolve against the schema.
- `args_test.go`: `BuildArgsForBackend(..., BackendKindUnsloth, ...)` emits
  `--model … --port …` in deterministic order.
- `processmgr` readiness: unit-test `waitForLogToken` against a temp file that
  gains the key line after a delay (success) and one that never does (timeout).
- `httpproxy`: `stubManager.WaitReady` returns a token → assert the reverse
  proxy sets `Authorization: Bearer <token>` on the upstream request
  (httptest backend echoing headers); empty token → no header (regression for
  existing kinds).
- Golden/embedded schema: none of the `-update` golden fixtures change; the
  new embedded schema is generated at `backend add` time.
- **End-to-end manual verification (acceptance gate).** Target model:
  `/home/diogo/models/huggingface/unsloth/diffusiongemma-26B-A4B-it-GGUF/diffusiongemma-26B-A4B-it-Q4_K_M.gguf`
  (local path, exercises the local-GGUF branch). Steps:
  1. `backend add` the unsloth kind with the wrapper as `executable`.
  2. `profile create` pointing `model` at the local GGUF above, with flags from
     the **DiffusionGemma run tutorial**
     (<https://unsloth.ai/docs/models/diffusiongemma#run-diffusiongemma-tutorials>)
     — fetch it first; DiffusionGemma is a diffusion LLM and may require
     specific sampling/template/`--jinja` flags. Apply tutorial-required flags
     (schema flag or `extraArgs`).
  3. `model-loader profile validate <id>` passes.
  4. `model-loader instance start <id>`; confirm via `instance logs`/the log
     file that the `sk-unsloth-…` key line appears (readiness signal works) and
     the model loads; `nvidia-smi` for VRAM.
  5. `curl http://127.0.0.1:4321/v1/chat/completions` with `model =
     <profile-id>` → a real completion. This simultaneously proves: token
     capture + injection, the readiness gate, and resolves the model-field
     risk. Record quant/KV/ctx/peak-GiB/tok-s in the profile description.

## rtx3090-inference-profiles skill update (final deliverable)

File: `.agents/skills/rtx3090-inference-profiles/SKILL.md` (+ a reference).

- **Backend dispatch table:** add a row —
  `unsloth | none — must backend add first | GGUF (HF repo or local path), via unsloth run | references/llama-family.md` (unsloth is a llama-server wrapper, so it belongs with the llama family; add an "Unsloth" subsection there rather than a new reference file).
- **Dispatch logic:** unsloth is opt-in (when the user names it or wants
  unsloth's HF auto-download / tool-calling / Anthropic endpoint for a GGUF).
  Default GGUF routing is unchanged (llama-cpp-default / beellama-rtx3090).
- **llama-family.md "Unsloth" subsection:** registration (`backend add` with
  the wrapper), that the model field accepts an HF repo or local GGUF +
  `gguf-variant`, that the proxy injects the captured Bearer token (nothing for
  the profile author to set), and the model-field/served-name resolution from
  the verification step. Carry the standard breadcrumb discipline (quant, KV,
  ctx, peak GiB, tok/s).
- **Common mistakes:** note that unsloth requires Studio installed and that
  `port`/auth are managed (never set in args).

## Out of scope

- Auto-installing Unsloth Studio.
- Exposing the Anthropic `/v1/messages` endpoint specially — it already works
  through the catch-all proxy once the Bearer token is injected.
- Disabling/patching Studio auth, or persisting the minted key.

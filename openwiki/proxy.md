# HTTP Proxy

`internal/service/httpproxy/` exposes a single multi-API reverse proxy that fronts the currently-loaded backend. Default bind `127.0.0.1:4321`, **loopback-only**, **no auth**, 8 MiB body buffer, 10 s shutdown grace. Health-check timeout default: **180 s** inside `httpproxy.Config` (apply when `cfg.HealthCheckTimeout <= 0`). The `model-loader serve` CLI **overrides** this to **360 s** (`internal/cli/serve.go:47`, configurable via `[serve].health_check_timeout_sec` in `config.toml`) because large models (e.g. Marlin expert repacks) take 3+ minutes to boot.

The proxy is the **only** communication channel between any client and the backends. Clients select a backend by sending the profile id as the request's `model` field; the proxy hot-swaps the loaded backend on demand.

## Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `*` | `/` (catch-all) | Reverse-proxied to the loaded backend. `"model"` in body (or `?model=`) triggers **implicit swap** |
| `POST` | `/v1/messages` | **Anthropic Messages API** → backend `/v1/chat/completions` |
| `POST` | `/v1/messages/count_tokens` | Local deterministic token estimate (tiktoken o200k_base). Validates profile exists but never loads or contacts a backend |
| `POST` | `/v1/responses` | **OpenAI Responses API** → backend chat completions |
| `POST` | `/v1beta/models/{model}:{generateContent\|streamGenerateContent\|countTokens}` | **Gemini API** → chat completions |
| `GET` | `/v1beta/models` | Profiles in Gemini shape |
| `GET` | `/v1/models` | OpenRouter-shaped list of profiles (with OpenAI `object:"model"` / `owned_by` and Anthropic `type:"model"` / `display_name` / `created_at`) |
| `GET` | `/_status` | JSON snapshot: `running, loaded_profile_id, loaded_pid, loaded_port, inflight_requests, last_swap_at, last_swap_dur, last_error` |
| `POST` | `/_admin/load` | Explicit load; body `{"profile_id":"…"}` (alias `{"model":"…"}`) |
| `POST` | `/_admin/unload` | Kill backend / free VRAM. `?force=true&drain_timeout=10s`; idempotent 200 |

## Request translation

| Source API | Translation file | Notes |
|------------|------------------|-------|
| OpenAI (native) | `handler.go` | Reverse-proxied with `httputil.ReverseProxy`; reads `model` field for implicit swap |
| Anthropic Messages | `anthropic_*.go` (8 files) | Full SSE event sequence (`message_start` → `content_block_*` → `message_delta` → `message_stop`), tools, images, system. `reasoning_content` ↔ `thinking` blocks. **Strict 404** for unknown model — never falls through to the loaded backend |
| OpenAI Responses | `responses_*.go` (4 files) | `instructions`→system, `function_call`/`function_call_output`, `reasoning.effort`, full Responses event-stream |
| Gemini | `gemini_*.go` (5 files) | `contents`/`parts`, `systemInstruction`, `functionCall`/`functionResponse` (FIFO id pairing), `thinkingConfig`→reasoning, `data:`-only SSE. `{model}` may carry a `(level)` reasoning suffix |

Anthropic reasoning controls (`thinking` / `output_config.effort`) flow through a canonical pivot to `reasoning_effort` + `chat_template_kwargs.enable_thinking` on the backend. Tool schemas with `properties:{}` are normalized; inline `{"role":"system"}` messages are folded into a `<system-reminder>` user turn.

The Anthropic translation **does not go through the `httputil.ReverseProxy`** — its `ModifyResponse` would destroy reasoning mapping. It uses a hand-rolled `http.Client` (`Server.upstream`) with `ResponseHeaderTimeout: 0` and `IdleConnTimeout: 90s` (no `Client.Timeout`: streamed completions run for minutes).

## Model swap (the heart of the proxy)

`ensureLoaded(profileID)` runs under `swapMu`. Flow:

1. **Same target** — fast path, no-op
2. **Different target** — under the same mutex:
   - `processmgr.Kill(old)` (drains in-flight unless `?force=true`)
   - `processmgr.Launch(new)` (allocates ephemeral loopback port `127.0.0.1:0`)
   - `WaitReady` polls `GET /health` with 100 ms→1 s capped backoff, up to `HealthCheckTimeout` (default 180 s)
   - Record `lastSwapAt` / `lastSwapDur`
3. **Concurrent same-target callers** collapse to a single launch

For unsloth, `WaitReady` additionally scans the log for the `sk-unsloth-…` auth token and the proxy injects it as `Authorization: Bearer …` on every request.

## Inflight tracking

`inflight` (atomic) + `inflightWG` (sync.WaitGroup) track in-flight requests so unload can drain:

- **Only the catch-all forwarder and `/v1/messages` increment `inflightWG`**
- `count_tokens` and admin endpoints **must not** touch it (drain self-deadlocks)
- `/v1/responses` increments because the path goes through the same swap; Gemini routes are tracked via the catch-all path
- `POST /_admin/unload?drain_timeout=10s` waits up to the timeout for `inflightWG` to reach zero, then kills regardless

## Status wire contract

`Status` (`server.go:56`) uses explicit `snake_case` JSON tags to lock the contract between the proxy process and the supervisor. `UnmarshalJSON` also accepts the legacy `PascalCase` keys so an old proxy binary's status can still be decoded by a new TUI. Do not add a 6th field without updating both.

## OpenRouter-shaped `/v1/models`

`models_openrouter.go` builds objects that mirror OpenRouter's `/api/v1/models`:

- `id`, `context_length`, `architecture`, `pricing`, `top_provider`, … (N/A fields empty)
- Plus OpenAI's `object: "model"` and `owned_by` (profile's `launch.backendId`)
- Plus Anthropic's `type: "model"`, `display_name`, `created_at`
- Envelope keeps OpenAI's `{"object":"list"}` and adds Anthropic's `has_more` / `first_id` / `last_id`

Image modality is inferred from the `mmproj` flag **or** a VL name marker (flag-less vLLM/SGLang VL models still report `text+image->text`).

## Error envelopes

- **Most routes** — OpenAI-style `{"error":{...}}`
- **Anthropic routes** (`/v1/messages`, `/v1/messages/count_tokens`) — `{"type":"error","error":{"type":..., "message":...}}` with `invalid_request_error` / `not_found_error` / `request_too_large` / `api_error`
- Unknown `"model"` on Anthropic routes is a strict `404 not_found_error` (no fall-through to the loaded backend)
- Request bodies on all inference routes are capped at 8 MiB (large base64 images count against this)

## What vLLM / SGLang must do

These backends validate the `model` field server-side. Set `served-model-name` to the **profile id** so inference requests carrying the profile id as `model` pass validation.

## Security

The proxy **binds loopback by default** and ships **no authentication** (`x-api-key` / `anthropic-version` headers are accepted and ignored). Do not expose it to a public interface without a fronting reverse proxy that adds auth.

`Server.upstream` disables `Transport.DisableCompression` to keep reasoning content uncompressed through the chain (compression can break the byte-level mapping for `reasoning_content` ↔ `thinking` blocks).

## Example session

```bash
# Current state
curl -s http://127.0.0.1:4321/_status | jq

# Explicitly load a profile
curl -sX POST http://127.0.0.1:4321/_admin/load \
  -H 'content-type: application/json' \
  -d '{"profile_id":"my-profile"}' | jq

# Unload + free VRAM
curl -sX POST http://127.0.0.1:4321/_admin/unload | jq

# Force unload (skip waiting for in-flight)
curl -sX POST 'http://127.0.0.1:4321/_admin/unload?force=true' | jq

# Anthropic Messages (translated to chat completions)
curl -sX POST http://127.0.0.1:4321/v1/messages \
  -H 'content-type: application/json' \
  -d '{"model":"my-profile","max_tokens":128,"messages":[{"role":"user","content":"Say hi"}]}' | jq

# Anthropic streaming
curl -NsX POST http://127.0.0.1:4321/v1/messages \
  -H 'content-type: application/json' \
  -d '{"model":"my-profile","max_tokens":128,"stream":true,"messages":[{"role":"user","content":"Count to 5"}]}'

# Local token estimate (never loads a model)
curl -sX POST http://127.0.0.1:4321/v1/messages/count_tokens \
  -H 'content-type: application/json' \
  -d '{"model":"my-profile","messages":[{"role":"user","content":"hello"}]}' | jq
```

## Implementation notes for future agents

- `Server` is constructed with `New`; driven via `Start` / `Stop`; observed via `Status`
- `swapMu` and `startMu` are separate — `startMu` serializes `Start`/`Stop`, `swapMu` serializes load/unload across requests
- `loadedBackend` (struct in `server.go:124`) owns a pre-built `ReverseProxy` to avoid re-allocating one per request
- `Status` UnmarshalJSON is intentionally case-insensitive only for legacy fields; do not strip the snake_case tags
- Translation errors must use the correct envelope — see `errors.go` for `newOpenAIError` vs `newAnthropicError` helpers
- The proxy spawns the configured `--health-check-timeout` and shutdown grace period; bench runners must not duplicate these bounds

---
type: Architecture
title: HTTP proxy and model hot-swap
description: The multi-API reverse proxy that fronts a single loaded backend. Speaks four client APIs — OpenAI native, Anthropic Messages, OpenAI Responses, and Gemini — all translated to the backend's chat completions, with implicit per-request model swap via the serialized ensureLoaded path and admin endpoints for explicit control.
tags: [proxy, httpproxy, hot-swap, translation, anthropic, gemini]
---

# HTTP Proxy

`internal/service/httpproxy/` is the in-process multi-API proxy. It fronts **one** loaded backend at a time; every inference request either hot-paths to the current backend or triggers a swap. The same proxy runs inside the TUI (`Server` tab `s`/`x`) and as the headless `model-loader serve` daemon.

The proxy **binds to loopback (`127.0.0.1`) by default and ships no authentication** (`x-api-key`/`anthropic-version` headers are accepted and ignored). Do not expose it to a public interface without a fronting reverse proxy that adds auth. Outbound `Bearer` is proxy→backend only (e.g. Unsloth Studio).

## The four client APIs

All four APIs are independent front-ends over **one shared core**: request → translate to OpenAI chat shape → `postUpstreamChat` to `127.0.0.1:<port>/v1/chat/completions` → translate the response/SSE back. Adding a fifth API is one `{types,translate,stream,handlers}.go` quartet plus a route; it never needs a new upstream path or swap logic.

| API | Route | Translation |
|-----|-------|-------------|
| **OpenAI (native)** | `/` catch-all | None — raw reverse proxy via `httputil.ReverseProxy`. `model` is rewritten to the bare profile slug. `ModifyResponse` mirrors `reasoning_content` into a synthetic `content` delta so plain OpenAI clients still see text. |
| **Anthropic Messages** | `POST /v1/messages` | `translateAnthropicRequest`: top-level `system` → leading system message; inline `system` → `<system-reminder>` user message (chat templates like Gemma/Qwen3-MoE reject non-leading system roles); `tool_result` → tool message; images → `image_url`; `thinking`/`output_config` → `reasoning_effort` + `enable_thinking`. Response/SSE: `message_start` → `content_block_*` → `message_delta` → `message_stop`; tool calls are **accumulated** and emitted whole. |
| **OpenAI Responses** | `POST /v1/responses` | `translateResponsesRequest`: `input` string/array; `instructions` → system; `function_call`/`function_call_output`; `reasoning.effort` → thinking. Response/SSE: `response.created` → … → `response.completed` with sequence numbers. |
| **Gemini** | `/v1beta/models/{m}:{generateContent\|streamGenerateContent\|countTokens}` | `translateGeminiRequest`: contents/parts; `systemInstruction` → system; function calls with **FIFO id pairing**; `thinkingConfig` → reasoning. SSE is `data:`-only frames (no `event:`, no `[DONE]`). |

The translated routes deliberately **bypass `loaded.proxy`** — they call `postUpstreamChat` directly so the ReverseProxy's `ModifyResponse` does not rewrite `reasoning_content` into `content` and destroy the reasoning→thinking mapping.

## Model hot-swap: `ensureLoaded`

`ensureLoaded` (`handler.go`) is the single swap entry point, reused by the catch-all, all three translated chat routes, and `/_admin/load`. It is serialized by `swapMu`.

<!-- openwiki: mermaid parse failed and this diagram was converted to a text fence so it does not break rendering. Fix the diagram source and restore the mermaid fence. Parser error: Heuristic: an unescaped angle bracket inside a label breaks rendering; rephrase the label. -->
```text
flowchart TD
    START["inference or /_admin/load request"] --> FAST{"fast path: current matches<br/>AND SameProcess pid/startTicks<br/>AND not unavailable?"}
    FAST -->|yes| RETURN["return loaded backend"]
    FAST -->|no| CRASH{"StartTicks mismatch?<br/>backend died/recycled"}
    CRASH -->|yes| CLEAR["handleBackendCrash<br/>clear s.current"]
    CRASH -->|no| LOCK["swapMu.Lock + double-check"]
    CLEAR --> LOCK
    LOCK --> KILL["killOldBackend<br/>ProcessMgr.Kill old pid"]
    KILL -->|"kill fails ErrStillAlive"| BUSY["503 backend_busy<br/>keep s.current - never launch into contended VRAM"]
    KILL -->|ok| LAUNCH["launchNewBackend<br/>ProcessMgr.Launch + WaitReady 180s"]
    LAUNCH -->|fail| FAIL["Kill + 502/504"]
    LAUNCH -->|ok| INST["install loadedBackend<br/>newReverseProxy"]
    INST --> USE["backend-use phase: serving gauge +1"]
    RETURN --> USE
```

Caption: the swap decision logic. The fast path identity-checks the current PID against its captured `StartTicks` (defends against PID recycling); on mismatch the proxy clears state and relaunches. A kill that cannot confirm death (`ErrStillAlive`) returns 503 and keeps the old backend rather than launching into contended VRAM (the P4/DF11 OOM cascade).

### Concurrency gauges

Two distinct atomic counters, and mixing them is a deadlock bug:
- `inflight atomic.Int64` — broad gauge for status display; incremented by the four backend-occupying handlers plus admin load.
- `serving atomic.Int64` — narrow gauge incremented **only after** `ensureLoaded` returns, around the actual backend call. `drainServing` (used by `/_admin/unload`) **polls** this counter — never a `sync.WaitGroup.Wait`, which cannot legally take `Add` concurrent with `Wait` at counter zero (audit C5).

Exactly four handlers touch `serving`: `handleForward`, `handleAnthropicMessages`, `handleResponses`, `handleGeminiGenerate`. Admin/count-tokens/models handlers must not, or `/_admin/unload`'s drain deadlocks against itself.

## Admin and status endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/_status` | JSON snapshot: `running`, `loaded_profile_id`, `loaded_pid`, `loaded_port`, `inflight_requests`, last swap timing, last error. |
| `POST` | `/_admin/load` | Explicitly load a profile. Body `{"profile_id":"<id>"}` or `{"model":"<id>"}`. Reuses `ensureLoaded`. |
| `POST` | `/_admin/unload` | Kill the loaded backend, freeing VRAM. `?force=true` skips the drain wait; `?drain_timeout=10s` bounds it (default = `[serve]` shutdown grace, 10s). Idempotent (200 when nothing loaded). |
| `GET` | `/v1/models` | OpenRouter-shaped list — each profile becomes a model object (OpenRouter's `id`/`context_length`/`architecture`/`pricing` + OpenAI's `object`/`owned_by` + Anthropic's `type`/`display_name`/`created_at`). Pricing is all `"0"` (local = free). Consults `ProfileStore` and never launches a backend. |

Both admin endpoints serialize against in-flight swaps via `swapMu`, so a load/unload cannot collide with the implicit swap triggered by an inference request. Request bodies on all inference routes are capped at 8 MiB (large base64 images count against this).

## Error envelopes

- **OpenAI envelope** everywhere except the two Anthropic routes: `{"error":{"message","type","code","param"}}`.
- **Anthropic envelope** for `/v1/messages` and `/v1/messages/count_tokens` only: `{"type":"error","error":{"type","message"}}`. Never mix.
- Anthropic model resolution is **strict**: `model` is required (400), unknown ids → `404 not_found_error` (no fall-through to the loaded backend). `max_tokens` (>0) is required on `/v1/messages`.
- `Status.UnmarshalJSON` shims both snake_case (current) and PascalCase (legacy proxy binaries).

A model-suffix override lets a caller force reasoning via the model name: `profile-id(level|budget)` (e.g. `qwen3-27b(none)`), taking precedence over body `thinking`/`output_config`; it is stripped before profile resolution on all routes.

## Proxy supervisor

`internal/service/proxysupervisor/` supervises the detached `model-loader serve` process from the TUI. `Supervisor.Start` spawns `serve` via `Setsid`, reaps the child without killing it, persists `proxy-state.json` (`{pid, host, port, started_at, start_ticks}`), and verifies the port-answerer is **our child** via `SameProcess` (defends against a pre-existing proxy keeping the port — audit A9). `Status` probes with hysteresis (`probeFailThreshold=3` so a transient GPU-load timeout does not drop supervision). `Stop` uses `TerminateTree` with a 45s grace (covers serve's 30s shutdown + 10s drain) and **sweeps the loaded backend** if serve was SIGKILLed before its own `killCurrentBackend` ran.

## Proxy ↔ process manager

`Server.deps.ProcessMgr` drives `Launch`/`Kill`/`WaitReady`. The proxy does not pre-resolve the backend kind/executable — `processmgr.launch.go:prepareLaunch` resolves both on demand and allocates an ephemeral port. `WaitReady` (`readiness.go`): unsloth backends scan the log for `sk-unsloth-[0-9a-f]{32}` (the token is both the readiness signal and the upstream auth key); all others poll `/health`. Timeout default is **180s** in the proxy, **360s** in `serve` (configurable via `[serve].health_check_timeout_sec`) for large MoE loads.

Crash recovery is on the hot path: `loadedBackend.startTicks` + `procutil.SameProcess` detects a crashed/PID-recycled backend → `handleBackendCrash` clears `s.current` → the next request relaunches. The ReverseProxy's `ErrorHandler` → `markBackendUnavailable` (atomic CAS) triggers relaunch on `ECONNREFUSED`/`EOF`/`ECONNRESET`/`EPIPE` when the client hasn't canceled. See [Process Manager](process-manager.md).

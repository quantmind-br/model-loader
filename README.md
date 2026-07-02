# model-loader

A terminal UI (TUI) for managing inference server profiles and processes across multiple backends — [llama.cpp](https://github.com/ggerganov/llama.cpp), vLLM, SGLang, DFlash, Unsloth, buun-llama-cpp, and beellama.cpp. Built with Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Features

- **Profile Editor** — Create and manage launch profiles through a schema-driven web editor (curated Essentials plus an advanced flag editor), one backend per profile
- **Model Browser** — Scan configured directories for `.gguf` models with metadata extraction
- **Launch & Hot-swap** — Load a profile with `Enter` on the Profiles tab; all inference flows through an OpenAI-shaped proxy that hot-swaps the active backend on demand
- **Monitor** — Real-time monitoring of running instances: logs, health status, slot usage, GPU metrics, and throughput
- **Multi-instance** — Run multiple backend instances concurrently, each with its own PID and port
- **Instance Recovery** — Background instances survive TUI exit and are recovered on restart
- **Backend Catalog** — Manage multiple backends — `llama-server` forks/versions plus other server kinds (vLLM, SGLang) — each with per-backend validation schemas
- **Schema-driven Validation** — Each backend has its own validation schema (auto-generated from `--help`, editable by user)
- **Per-Profile Backend Selection** — Each profile selects a backend from the catalog; validation uses that backend's schema exclusively
- **Hugging Face Integration** — Search the Hub and download `.gguf` files with queued, progress-tracked downloads
- **Benchmark** — Evaluate profiles with a built-in engine spanning 12 modes across 5 categories: Quality (LLM-judge SWE-bench Lite, GSM8K math, HumanEval codegen, RAGAS RAG faithfulness, summarization), Speed (`llama-bench` throughput), Robustness (long-context needle, instruction-following), Knowledge (MMLU), and **Agentic** (Terminal-Bench, SWE-bench Pro, DeepSWE — each shells out to an external harness + Docker). The TUI Benchmark tab is a dashboard-centric redesign: a mode-focused leaderboard with trend sparklines, a unified run wizard (profile→mode→review), scorecard run-detail, visual compare, and history timeline
- **Multi-API HTTP Proxy** — Headless `model-loader serve` exposes one proxy that speaks **four** client APIs: OpenAI (native reverse-proxy), **Anthropic Messages** (`/v1/messages`, translated), **OpenAI Responses** (`/v1/responses`, translated), and **Gemini** (`/v1beta/models/*`, translated) — all routed to the loaded backend's chat completions, with implicit model swap (per-request `"model"` field) plus admin endpoints (`POST /_admin/load`, `POST /_admin/unload`) for explicit control and VRAM release. Anthropic SDK clients (incl. Claude Code) can point straight at it

## Requirements

- Go 1.26 or later
- `llama-server` binary in your `PATH` (used for auto-generating backend schemas)
- (Optional) `nvidia-smi` for GPU monitoring

## Installation

```bash
# Clone the repository
git clone https://github.com/quantmind-br/model-loader.git
cd model-loader

# Build
make build

# Or install to ~/.local/bin
make install
```

## Quick Start

1. **Run the application:**
   ```bash
   ./bin/model-loader
   ```
   On first run, a default `config.toml` is created at `~/.config/model-loader/config.toml`.

2. **Launch** (Tab 1 — Profiles):
   - Select a profile from the list
   - Press `Enter` to load it through the HTTP proxy (the proxy hot-swaps the active backend)

3. **Create a profile** (Tab 1 — Profiles):
   - Press `n` to create a new profile (or `e` to edit) — this opens a schema-driven editor in your browser
   - Fill in the model, backend, and flags, then Save on the page; the TUI reloads the list (`Esc` cancels)

4. **Server** (Tab 2 — Server):
   - Select a running instance to view logs, slots, GPU stats, and metrics

5. **Browse Models** (Tab 3 — Models):
   - Browse `.gguf` files found in configured search paths
   - Filter and search Hugging Face

## Keyboard Shortcuts

### Global

| Key | Action |
|-----|--------|
| `1` | Profiles tab |
| `2` | Server tab |
| `3` | Models tab |
| `4` | Backends tab |
| `5` | Benchmark tab |
| `Tab` / `Shift+Tab` | Next / previous tab |
| `q` | Quit |
| `?` | Show help |

### Profiles Tab

| Key | Action |
|-----|--------|
| `Enter` | Load selected profile through the HTTP proxy (hot-swaps the active model) |
| `e` | Edit selected profile (opens the web editor) |
| `n` | New profile |
| `d` | Duplicate profile |
| `X` | Delete profile (confirm) |
| `K` | Unload the currently loaded model (confirm) |
| `R` | Refresh profile list |
| `p` | Pin selected profile |
| `I` | Import profiles from JSON bundle |
| `u` | Undo last import |
| `E` | Export all profiles to JSON bundle |
| `Ctrl+T` | Cycle Essentials / Advanced / Environment / Sizing (while editing) |
| `/` | Filter profiles |

### Server Tab

| Key | Action |
|-----|--------|
| `v` | Cycle Logs / Slots / Metrics / History sub-views |
| `Space` | Pause / resume log scroll |
| `K` | Kill selected instance (confirm) |
| `R` | Restart selected instance (confirm) |
| `h` | Open history chart |
| `1` / `2` / `3` / `4` | History chart window: 1h / 6h / 24h / 7d (while chart open) |
| `s` | Start HTTP proxy listener |
| `x` | Stop HTTP proxy listener |

### Models Tab

| Key | Action |
|-----|--------|
| `R` | Rescan all configured paths |
| `/` | Filter models |
| `Enter` | Actions: use in new / existing profile or reveal path |
| `s` | Search Hugging Face |
| `i` | Show model info panel |
| `→` / `g` | Navigate to sizing for this model |

## Configuration

Configuration is stored in `~/.config/model-loader/config.toml`:

```toml
[paths]
profiles_dir = "~/.config/model-loader/profiles"
backends_dir = "~/.config/model-loader/backends"
log_dir = "~/.local/state/model-loader/logs"
state_dir = "~/.local/state/model-loader"

[models]
search_paths = ["~/.lmstudio/models", "~/models"]

[ui]
default_tab = "profiles"
```

See [docs/config.md](docs/config.md) for detailed configuration options.

## HTTP API

The `model-loader serve` subcommand starts a headless reverse proxy on `[serve].host:[serve].port` (default `127.0.0.1:4321`). It speaks **four** client APIs, all routed to the loaded backend's chat completions: the OpenAI API (native, reverse-proxied), the Anthropic Messages API, the OpenAI Responses API, and the Gemini API (the latter three translated in-proxy). The same proxy can be started/stopped from the TUI Server tab (`s` / `x`).

### Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/v1/chat/completions`, `/v1/completions`, etc. | OpenAI-compatible inference. The proxy reads the `"model"` field (or `?model=` query param) and hot-swaps the backend if needed |
| `POST` | `/v1/messages` | Anthropic Messages API. Translated to the loaded backend's OpenAI `/v1/chat/completions` — works with every backend kind. Full fidelity: SSE streaming (`message_start` → `content_block_*` → `message_delta` → `message_stop`), tools (`tool_use`/`tool_result`), images (base64/URL), `system`, `stop_sequences`; backend `reasoning_content` is mapped to `thinking` blocks. The `"model"` field must be a profile id and triggers the same implicit swap |
| `POST` | `/v1/messages/count_tokens` | Local deterministic token estimate (~4 bytes/token + per-message/image overhead). Never contacts or loads a backend. Validates that `"model"` is an existing profile |
| `POST` | `/v1/responses` | OpenAI Responses API, translated to the backend's chat completions: input string/array, `instructions`→system, function_call/function_call_output, `reasoning.effort`, full Responses event-stream. Same implicit-swap + inflight semantics as `/v1/messages` |
| `POST` | `/v1beta/models/{model}:{generateContent\|streamGenerateContent\|countTokens}` | Gemini API, translated to chat completions: contents/parts, systemInstruction, functionCall/functionResponse (FIFO id pairing), `thinkingConfig`→reasoning, `data:`-only SSE stream; `{model}` may carry a `(level)` reasoning suffix. `GET /v1beta/models` lists profiles in Gemini shape |
| `GET`  | `/v1/models` | OpenRouter-shaped model list — each profile becomes a model object mirroring OpenRouter's `/api/v1/models` (`id`, `context_length`, `architecture`, `pricing`, `top_provider`, …); fields with no meaning for a local proxy are emitted empty. Each item also carries OpenAI's `object: "model"` and `owned_by` (the profile's serving backend id) plus Anthropic's `type: "model"`, `display_name`, and `created_at`. The list envelope keeps OpenAI's `{"object":"list"}` and adds Anthropic's `has_more`/`first_id`/`last_id` |
| `GET`  | `/_status` | JSON snapshot: `running`, `loaded_profile_id`, `loaded_pid`, `loaded_port`, `inflight_requests`, last swap timing, last error |
| `POST` | `/_admin/load` | Explicitly load a profile without sending an inference request. Body: `{"profile_id":"<id>"}` (or `{"model":"<id>"}` as alias). Reuses the same swap path as the catch-all forwarder |
| `POST` | `/_admin/unload` | Kill the currently-loaded backend. Frees its VRAM (the OS reclaims memory when the process exits). Idempotent: returns 200 when nothing is loaded |

#### Admin endpoint behavior

- Both admin endpoints serialize against in-flight model swaps (single internal mutex), so a load/unload cannot collide with the implicit swap triggered by an inference request.
- `POST /_admin/unload` accepts optional query params:
  - `?force=true` — skip waiting for in-flight requests before killing.
  - `?drain_timeout=10s` — upper bound on the drain wait (defaults to `[serve]` shutdown grace, currently 10s). After the timeout, the backend is killed even if requests are still in flight.
- All endpoints return JSON with the same `Status` shape as `GET /_status` on success and an OpenAI-style `{"error":{...}}` envelope on failure. The two Anthropic routes (`/v1/messages`, `/v1/messages/count_tokens`) are the exception: their failures use the Anthropic envelope `{"type":"error","error":{"type":...,"message":...}}` (`invalid_request_error`, `not_found_error`, `request_too_large`, `api_error`).
- An unknown `"model"` on the Anthropic routes is a strict `404 not_found_error` — there is no fall-through to the currently-loaded backend. Request bodies on all inference routes are capped at 8 MiB (large base64 images count against this).
- The proxy binds to loopback (`127.0.0.1`) by default and ships **no authentication** (`x-api-key`/`anthropic-version` headers are accepted and ignored). Do not expose it to a public interface without a fronting reverse proxy that adds auth.

### Examples

```bash
# Current state
curl -s http://127.0.0.1:4321/_status | jq

# Explicitly load a profile
curl -sX POST http://127.0.0.1:4321/_admin/load \
  -H 'content-type: application/json' \
  -d '{"profile_id":"my-profile"}' | jq

# Unload + free VRAM (drains in-flight requests first)
curl -sX POST http://127.0.0.1:4321/_admin/unload | jq

# Force unload (do not wait for in-flight requests)
curl -sX POST 'http://127.0.0.1:4321/_admin/unload?force=true' | jq

# Anthropic Messages API (translated to the backend's chat completions)
curl -sX POST http://127.0.0.1:4321/v1/messages \
  -H 'content-type: application/json' \
  -d '{"model":"my-profile","max_tokens":128,"messages":[{"role":"user","content":"Say hi"}]}' | jq

# Anthropic streaming (SSE event sequence)
curl -NsX POST http://127.0.0.1:4321/v1/messages \
  -H 'content-type: application/json' \
  -d '{"model":"my-profile","max_tokens":128,"stream":true,"messages":[{"role":"user","content":"Count to 5"}]}'

# Local token estimate (never loads a model)
curl -sX POST http://127.0.0.1:4321/v1/messages/count_tokens \
  -H 'content-type: application/json' \
  -d '{"model":"my-profile","messages":[{"role":"user","content":"hello"}]}' | jq
```

Anthropic SDK clients (including Claude Code) can point straight at the proxy — use profile ids as model names:

```bash
ANTHROPIC_BASE_URL=http://127.0.0.1:4321 \
ANTHROPIC_API_KEY=dummy \
ANTHROPIC_MODEL=my-profile \
ANTHROPIC_SMALL_FAST_MODEL=my-profile \
claude -p "hello"
```

## Directory Structure

| Path | Purpose |
|------|---------|
| `~/.config/model-loader/config.toml` | Application configuration |
| `~/.config/model-loader/profiles/` | Profile JSON files (one per profile) |
| `~/.config/model-loader/backends/` | Backend catalog (`catalog.json`) and schema files |
| `~/.local/state/model-loader/instances.json` | Background instance registry |
| `~/.local/state/model-loader/logs/` | Captured stdout/stderr logs |

## Development

```bash
# Run all tests
make tests

# Update golden test fixtures
go test ./... -update

# Build binary
make build
```

See [AGENTS.md](AGENTS.md) for project conventions and architecture notes.

## Troubleshooting

- **`llama-server` not found** — Ensure `llama-server` is compiled and available in your `PATH`. Go to the Backends tab (`4`) and press `n` to register a custom binary location as a backend
- **Port in use** — Edit the profile and change the port number
- **Model not found** — Verify the model path in the profile or update `search_paths` in `config.toml`
- **Instance not recovering** — Check that `instances.json` exists in the state directory
- **Backend schema missing** — Each backend needs a validation schema. Add a backend via the Backends tab (`n`) to auto-generate one from `--help`, or place a manually edited schema in the backends directory

For more details, see [docs/troubleshooting.md](docs/troubleshooting.md).

## License

MIT

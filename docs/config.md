# Configuration

model-loader uses a single TOML file for configuration. The file is auto-generated on first run if it does not exist.

## Config File Location

```
~/.config/model-loader/config.toml
```

## Reference

### `[paths]`

Directory paths used by the application. All paths support `~` expansion.

| Key | Default | Description |
|-----|---------|-------------|
| `profiles_dir` | `~/.config/model-loader/profiles` | Directory where profile JSON files are stored |
| `backends_dir` | `~/.config/model-loader/backends` | Directory for backend catalog (`catalog.json`) and per-backend validation schemas |
| `log_dir` | `~/.local/state/model-loader/logs` | Directory for captured llama-server stdout/stderr logs |
| `state_dir` | `~/.local/state/model-loader` | Parent directory for runtime state (instances.json) |
| `llama_server_binary_path` | `""` | **Legacy.** Fallback `llama-server` binary path, read only during the one-time migration to the backend catalog. Not used at runtime afterwards — register binaries via the backend catalog instead |

### `[models]`

Model discovery settings.

| Key | Default | Description |
|-----|---------|-------------|
| `search_paths` | `["~/.lmstudio/models", "~/models"]` | List of directories to scan recursively for `.gguf` files |

### `[ui]`

User interface preferences.

| Key | Default | Description |
|-----|---------|-------------|
| `default_tab` | `launcher` | Tab shown on startup. Recognized values: `profiles`, `server`, `models`, `backends`, `benchmark` (any unrecognized value, including the written default `launcher`, falls back to `profiles`) |
| `keybindings` | `default` | Keybinding preset. Currently only `default` is supported |

### `[logging]`

Debug logging settings. Logs are written to files only (never stdout) under `log_dir`.

| Key | Default | Description |
|-----|---------|-------------|
| `level` | `info` | Log level: `debug`, `info`, `warn`, `error`. Overridden by the `--log-level` CLI flag or the `MODEL_LOADER_LOG_LEVEL` environment variable (in that precedence order) |

### `[serve]`

Bind address for the HTTP proxy (an OpenAI-shaped reverse proxy in front of running instances). The proxy is started headlessly by `model-loader serve` and is also auto-started by the TUI — it is the only communication channel with profile backends. Clients select a backend by sending the profile ID as the OpenAI `model` field; the proxy loads/swaps the matching instance on demand.

Instance ports are ephemeral and internal: the process manager assigns each backend a free port at launch, and clients never talk to backends directly — always go through the proxy. Backends that validate model names server-side (vLLM, SGLang) should set `served-model-name` to the profile ID so inference requests carrying the profile ID as `model` are accepted.

| Key | Default | Description |
|-----|---------|-------------|
| `host` | `127.0.0.1` | Bind host for the proxy. Overridable with `serve --host` |
| `port` | `4321` | Bind port for the proxy. Overridable with `serve --port` |

#### Endpoints

The proxy exposes the OpenAI-compatible inference surface, the Anthropic Messages API (translated to the backend's chat completions), and dedicated admin endpoints. All return JSON; failures use the OpenAI `{"error":{...}}` envelope, except the two Anthropic routes, whose failures use the Anthropic `{"type":"error","error":{...}}` envelope.

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/v1/chat/completions`, `/v1/completions`, … | Proxied to the loaded backend. The `"model"` field (or `?model=` query param) triggers an implicit swap when needed |
| `POST` | `/v1/messages` | Anthropic Messages API translated to the backend's `/v1/chat/completions` (streaming, tools, images, system; `reasoning_content` → `thinking` blocks). `"model"` must be a profile id (strict 404 otherwise) and triggers the same implicit swap |
| `POST` | `/v1/messages/count_tokens` | Deterministic local token estimate; never contacts or loads a backend. Validates that `"model"` is an existing profile |
| `GET`  | `/v1/models` | OpenRouter-shaped model list — each profile mirrors an OpenRouter `/api/v1/models` object (non-applicable fields empty) plus OpenAI's `object:"model"` and `owned_by` (serving backend id) and Anthropic's `type:"model"`/`display_name`/`created_at`; envelope keeps `{"object":"list"}` and adds `has_more`/`first_id`/`last_id` |
| `GET`  | `/_status` | Current state: `running`, `loaded_profile_id`, `loaded_pid`, `loaded_port`, `inflight_requests`, `last_swap_at`, `last_swap_dur`, `last_error` |
| `POST` | `/_admin/load` | Explicitly load a profile. Body: `{"profile_id":"<id>"}` (alias: `{"model":"<id>"}`). Returns the same `Status` shape as `/_status` |
| `POST` | `/_admin/unload` | Kill the loaded backend, freeing its VRAM. Query params: `?force=true` (skip drain), `?drain_timeout=10s` (cap on in-flight drain wait, defaults to the shutdown grace period). Idempotent: 200 when nothing is loaded |

When a request targets a profile that is not loaded yet, the proxy launches the backend and waits for it to become healthy before forwarding (up to 180 seconds by default, to accommodate slow model loads).

The proxy binds to loopback by default and has no built-in authentication; do not expose it directly to a public interface.

### `[benchmark]`

Profile evaluation engine settings (Benchmark tab and `model-loader benchmark`).

| Key | Default | Description |
|-----|---------|-------------|
| `max_tokens` | `32768` | Generation cap per problem |
| `timeout_sec` | `120` | Per-problem inference timeout, in seconds |
| `long_context_tokens` | `0` | Target prompt size for the long-context needle probe (`0` → 8000) |
| `save_transcripts` | `true` | Capture raw model/judge I/O per run for debugging |

#### `[benchmark.judge]`

Optional LLM-as-judge endpoint (OpenAI-compatible) used to grade open-ended answers.

| Key | Default | Description |
|-----|---------|-------------|
| `base_url` | `""` | Base URL of the judge endpoint (empty disables LLM judging) |
| `api_key` | `""` | API key for the judge endpoint |
| `model` | `""` | Judge model name |
| `samples` | `3` | Number of judge samples per evaluation |

#### `[benchmark.llamabench]`

Throughput-mode (`llama-bench`) settings.

| Key | Default | Description |
|-----|---------|-------------|
| `presets` | `["512/128", "4096/256"]` | `pp/tg` token pairs (prompt / generation) to measure |
| `repetitions` | `3` | Measurements per preset (`0` → 3) |

#### `[benchmark.terminalbench]`

Agentic [Terminal-Bench](https://github.com/laude-institute/terminal-bench) mode (`--mode terminal-bench`), wrapping the external `tb` CLI which drives a Dockerized tmux sandbox via the Terminus agent pointed at the proxy. Requires `tb` + Docker (fail-fast in `Prepare`).

| Key | Default | Description |
|-----|---------|-------------|
| `command` | `"tb"` | `tb` CLI binary (name on PATH or absolute path) |
| `agent` | `"terminus"` | `tb` agent |
| `dataset` | `"terminal-bench-core==0.1.1"` | `tb` dataset `name` or `name==version` |
| `provider` | `"openai"` | LiteLLM provider prefix → `openai/<profile-id>` |
| `tasks` | `[]` | `--task-id` ids/globs; empty → whole dataset |
| `n_tasks` | `0` | Task cap; `0` → whole dataset. Expanded to a deterministic `--task-id` subset sampled from the dataset cache (falls back to `--n-tasks` before the cache exists) |
| `sample_seed` | `0` | Seeds the deterministic `--n-tasks`→`--task-id` expansion (with `n_tasks` / `--limit`) |
| `concurrent` | `1` | `--n-concurrent` (keep at 1 on a single-GPU rig) |
| `timeout_sec` | `0` | Whole-run cap; `0` → none (NOT the per-request `timeout`) |
| `extra_args` | `[]` | Passed verbatim (e.g. `["--no-rebuild"]`) |

#### `[benchmark.swebenchpro]`

Agentic [SWE-bench Pro](https://github.com/scaleapi/SWE-bench_Pro-os) mode (`--mode swe-bench-pro`), a 3-stage subprocess coordinator over the external harness (optional agent patch-generation → `gather_patches.py` → `swe_bench_pro_eval.py`) + Docker. Requires harness + eval-script + `raw_sample_path` (no default — the bundled jsonl has UPPERCASE column names the eval misreads) + scripts-dir + python (+ Docker unless `use_modal`). Operator guide: [docs/swe-bench-pro.md](swe-bench-pro.md).

| Key | Default | Description |
|-----|---------|-------------|
| `harness_dir` | `""` | Cloned SWE-bench_Pro-os checkout (required) |
| `raw_sample_path` | `""` | `--raw_sample_path`; required, lowercase `fail_to_pass`/`pass_to_pass` columns |
| `scripts_dir` | `""` | `--scripts_dir`; empty → `<harness>/run_scripts` |
| `dockerhub_user` | `"jefzda"` | `--dockerhub_username` |
| `python` | `"python3"` | Python interpreter |
| `num_workers` | `4` | `eval --num_workers` (≤0 → 4, single workstation) |
| `use_modal` | `false` | `false` → `--use_local_docker`; `true` → Modal cloud |
| `instances` | `[]` | Subset of `instance_id`s to evaluate; empty → all in the patch set |
| `sample_seed` | `0` | Seeds the deterministic `--limit` sampling of `instance_id`s from `raw_sample_path` |
| `patch_path` | `""` | Pre-generated patches JSON or preds dir; takes precedence over `agent_cmd`; empty → require `agent_cmd` |
| `agent_cmd` | `[]` | Patch-generation command (placeholders `{model}/{api_base}/{output}/{instances}/{harness}`); used only when `patch_path` is empty |
| `timeout_sec` | `0` | Whole-pipeline cap; `0` → none |
| `extra_args` | `[]` | Passed verbatim to `swe_bench_pro_eval.py` |

#### `[benchmark.deepswe]`

Agentic [DeepSWE](https://github.com/datacurve-ai/deep-swe) mode (`--mode deep-swe`),
wrapping the external `pier` CLI + Docker. See [docs/deep-swe.md](deep-swe.md) for
the full guide (chat-completions routing and container→host networking matter).

| Key | Default | Description |
|-----|---------|-------------|
| `tasks_dir` | `""` | Cloned deep-swe `tasks/` dir (required for this mode) |
| `command` | `"pier"` | Pier CLI binary (name on PATH or absolute path) |
| `agent` | `"mini-swe-agent"` | Pier agent that solves each task |
| `provider` | `"openai"` | LiteLLM provider prefix → `openai/<profile-id>` |
| `model_class` | `"litellm"` | mini-swe-agent adapter; forces chat completions (not the Responses API) |
| `api_base` | `""` | Agent-facing api_base; empty → `<proxy>/v1` with loopback rewritten to `host.docker.internal` |
| `tasks` | `[]` | `--include-task-name` ids/globs; empty → whole corpus |
| `n_tasks` | `0` | `--n-tasks` cap; `0` → whole corpus |
| `sample_seed` | `0` | `--sample-seed` for a deterministic subset (with `n_tasks`) |
| `concurrent` | `1` | `--n-concurrent` (keep at 1 on a single-GPU rig) |
| `timeout_sec` | `0` | Whole-run cap; `0` → none |
| `extra_args` | `[]` | Passed verbatim after the built flags |

## Example

```toml
[paths]
profiles_dir = "~/.config/model-loader/profiles"
backends_dir = "~/.config/model-loader/backends"
log_dir = "~/.local/state/model-loader/logs"
state_dir = "~/.local/state/model-loader"

[models]
search_paths = [
    "~/.lmstudio/models",
    "~/models",
    "/mnt/storage/gguf",
]

[ui]
default_tab = "launcher"
keybindings = "default"

[logging]
level = "info"

[serve]
host = "127.0.0.1"
port = 4321

[benchmark]
max_tokens = 32768
timeout_sec = 120
long_context_tokens = 0
save_transcripts = true

[benchmark.judge]
base_url = ""
api_key = ""
model = ""
samples = 3

[benchmark.llamabench]
presets = ["512/128", "4096/256"]
repetitions = 3

# [benchmark.deepswe]
# tasks_dir = "~/dev/deep-swe/tasks"   # required for --mode deep-swe; see docs/deep-swe.md
```

## Backend Catalog

The backend catalog lives in `backends_dir` and consists of two file types:

### `catalog.json`

Registry of all backends. Example:

```json
{
  "schemaVersion": 1,
  "defaultBackendId": "upstream",
  "backends": [
    {
      "id": "upstream",
      "name": "llama.cpp upstream",
      "kind": "llama-server",
      "executable": "llama-server",
      "schemaRef": "schemas/upstream.json",
      "meta": {
        "createdAt": "2026-01-15T10:00:00Z",
        "updatedAt": "2026-01-15T10:00:00Z"
      }
    }
  ]
}
```

| Field | Description |
|-------|-------------|
| `schemaVersion` | Always `1` |
| `defaultBackendId` | Backend ID used when a profile has no explicit backend selection |
| `backends[].id` | Unique slug (used in profiles) |
| `backends[].kind` | Backend type: `llama-server`, `vllm`, `tabbyapi`, `sglang` |
| `backends[].executable` | Absolute or `PATH`-relative binary |
| `backends[].schemaRef` | Relative path to the schema file under `backends_dir/schemas/` |

### Schema Files (`schemas/*.json`)

Each backend has a JSON schema that defines valid flags. The schema is the single source of truth for profile validation. It can be auto-generated from the binary's `--help` output or edited manually.

Example:

```json
{
  "schemaVersion": 1,
  "kind": "cli-flags.v1",
  "backendKind": "llama-server",
  "backendId": "upstream",
  "source": {
    "generatedFrom": "llama-server",
    "generatedAt": "2026-01-15T10:00:00Z",
    "editable": true,
    "customized": true
  },
  "flags": {
    "n-gpu-layers": {
      "Long": "n-gpu-layers",
      "Short": "ngl",
      "Type": 1,
      "EnumValues": null,
      "Default": null,
      "HelpText": "Number of layers to offload to GPU",
      "Group": "common"
    },
    "ctx-size": {
      "Long": "ctx-size",
      "Short": "c",
      "Type": 1,
      "EnumValues": null,
      "Default": 4096,
      "HelpText": "Context size",
      "Group": "common"
    }
  }
}
```

| Field | Description |
|-------|-------------|
| `kind` | Schema format. Currently only `cli-flags.v1` |
| `backendId` | Must match the backend's `id` in `catalog.json` |
| `backendKind` | Must match the backend's `kind` in `catalog.json` |
| `source.editable` | Capability flag: the web editor may customize this schema. Every generator sets it, so it says nothing about whether edits exist |
| `source.customized` | Dirty marker: an operator edited this schema (web Customize mode or `backend schema apply`). Generators skip regenerating it, and `backend schema refresh` carries its `presentation` + `rules` across the rebuild |
| `flags` | Map of flag name → `FlagSpec` |

**FlagSpec fields:**

| Field | Type | Description |
|-------|------|-------------|
| `Long` | string | Canonical long name without `--` |
| `Short` | string | Short alias without `-` (empty if none) |
| `Aliases` | []string | Additional long aliases |
| `Type` | int | `0`=bool, `1`=int, `2`=float, `3`=string, `4`=enum |
| `EnumValues` | []string | Allowed values when `Type` is `4` |
| `Default` | any | Default value |
| `HelpText` | string | Description shown in UI |
| `Group` | string | Category: `common`, `sampling`, `example-specific`, `embedded` |

## Notes

- The application creates missing directories automatically
- Changes to `config.toml` require a restart to take effect
- The log level can be overridden at launch without editing the file: `--log-level=debug` or `MODEL_LOADER_LOG_LEVEL=debug`
- `search_paths` that do not exist are silently skipped during model scanning
- Schema files with `source.customized: true` are never regenerated by the automatic paths (catalog ensure, `backend add` retries). An explicit `backend schema refresh` still re-derives flag facts from the backend, keeping only the operator's `presentation` and `rules` — each reconciled against the regenerated flag set

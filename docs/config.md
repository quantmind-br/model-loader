# Configuration

llama-cpp-loader uses a single TOML file for configuration. The file is auto-generated on first run if it does not exist.

## Config File Location

```
~/.config/llama-cpp-loader/config.toml
```

## Reference

### `[paths]`

Directory paths used by the application. All paths support `~` expansion.

| Key | Default | Description |
|-----|---------|-------------|
| `profiles_dir` | `~/.config/llama-cpp-loader/profiles` | Directory where profile JSON files are stored |
| `backends_dir` | `~/.config/llama-cpp-loader/backends` | Directory for backend catalog (`catalog.json`) and per-backend validation schemas |
| `log_dir` | `~/.local/state/llama-cpp-loader/logs` | Directory for captured llama-server stdout/stderr logs |
| `state_dir` | `~/.local/state/llama-cpp-loader` | Parent directory for runtime state (instances.json) |

### `[models]`

Model discovery settings.

| Key | Default | Description |
|-----|---------|-------------|
| `search_paths` | `["~/.lmstudio/models", "~/models"]` | List of directories to scan recursively for `.gguf` files |

### `[ui]`

User interface preferences.

| Key | Default | Description |
|-----|---------|-------------|
| `default_tab` | `profiles` | Tab shown on startup. Values: `profiles`, `launcher`, `monitor`, `models` |
| `keybindings` | `default` | Keybinding preset. Currently only `default` is supported |

## Example

```toml
[paths]
profiles_dir = "~/.config/llama-cpp-loader/profiles"
backends_dir = "~/.config/llama-cpp-loader/backends"
log_dir = "~/.local/state/llama-cpp-loader/logs"
state_dir = "~/.local/state/llama-cpp-loader"

[models]
search_paths = [
    "~/.lmstudio/models",
    "~/models",
    "/mnt/storage/gguf",
]

[ui]
default_tab = "launcher"
keybindings = "default"
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
    "editable": true
  },
  "flags": {
    "n-gpu-layers": {
      "long": "n-gpu-layers",
      "short": "ngl",
      "type": "int",
      "description": "Number of layers to offload to GPU"
    },
    "ctx-size": {
      "long": "ctx-size",
      "type": "int",
      "description": "Context size"
    }
  }
}
```

| Field | Description |
|-------|-------------|
| `kind` | Schema format. Currently only `cli-flags.v1` |
| `backendId` | Must match the backend's `id` in `catalog.json` |
| `backendKind` | Must match the backend's `kind` in `catalog.json` |
| `source.editable` | `true` if the file was manually edited (prevents overwrite during auto-refresh) |
| `flags` | Map of flag name → `FlagSpec` (type, description, enum values) |

## Notes

- The application creates missing directories automatically
- Changes to `config.toml` require a restart to take effect
- `search_paths` that do not exist are silently skipped during model scanning
- Schema files with `source.editable: true` are never overwritten by auto-generation

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
| `log_dir` | `~/.local/state/llama-cpp-loader/logs` | Directory for captured llama-server stdout/stderr logs |
| `state_dir` | `~/.local/state/llama-cpp-loader` | Parent directory for runtime state (instances.json) |
| `llama_server_binary_path` | `"llama-server"` | Path to the `llama-server` binary. Bare name resolves via PATH; absolute/relative path must exist and be executable |

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
log_dir = "~/.local/state/llama-cpp-loader/logs"
state_dir = "~/.local/state/llama-cpp-loader"
# Use a specific llama.cpp build globally
# llama_server_binary_path = "/opt/llama.cpp/build/bin/llama-server"

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

## Notes

- The application creates missing directories automatically
- Changes to `config.toml` require a restart to take effect
- `search_paths` that do not exist are silently skipped during model scanning

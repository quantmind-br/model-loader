# llama-cpp-loader

A terminal UI (TUI) for managing [llama.cpp](https://github.com/ggerganov/llama.cpp) `llama-server` profiles and processes. Built with Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Features

- **Profile Editor** — Create and manage llama-server launch profiles with curated essential flags and an advanced flag editor
- **Model Browser** — Scan configured directories for `.gguf` models with metadata extraction
- **Launcher** — Launch llama-server instances in foreground (with live log streaming) or background (detached)
- **Monitor** — Real-time monitoring of running instances: logs, health status, slot usage, GPU metrics, and throughput
- **Multi-instance** — Run multiple llama-server instances concurrently, each with its own PID and port
- **Instance Recovery** — Background instances survive TUI exit and are recovered on restart
- **Version-aware Validation** — Parses `llama-server --help` to validate flags against the installed binary version
- **Per-Profile Binary Selection** — Each profile can override the global `llama-server` binary, enabling side-by-side builds

## Requirements

- Go 1.26 or later
- `llama-server` binary in your `PATH`, or configure an explicit path in `config.toml`
- (Optional) `nvidia-smi` for GPU monitoring

## Installation

```bash
# Clone the repository
git clone https://github.com/quantmind-br/llama-cpp-loader.git
cd llama-cpp-loader

# Build
make build

# Or install to ~/.local/bin
make install
```

## Quick Start

1. **Run the application:**
   ```bash
   ./bin/llama-cpp-loader
   ```
   On first run, a default `config.toml` is created at `~/.config/llama-cpp-loader/config.toml`.

2. **Create a profile** (Tab 1 — Profiles):
   - Press `n` to create a new profile
   - Fill in the model path, name, and parameters (use `Tab` / `Shift+Tab` to move between fields)
   - Press `Enter` on the **Save** button to persist the profile (or `Esc` to cancel)

3. **Launch** (Tab 2 — Launcher):
   - Select a profile from the list
   - Toggle `b` for background mode (default) or foreground
   - Press `Enter` to launch

4. **Monitor** (Tab 3 — Monitor):
   - Select a running instance to view logs, slots, GPU stats, and metrics

5. **Browse Models** (Tab 4 — Models):
   - Browse `.gguf` files found in configured search paths
   - Filter and copy paths to clipboard

## Keyboard Shortcuts

### Global

| Key | Action |
|-----|--------|
| `1` | Profiles tab |
| `2` | Launcher tab |
| `3` | Monitor tab |
| `4` | Models tab |
| `Tab` / `Shift+Tab` | Next / previous tab |
| `q` | Quit |
| `?` | Show help |

### Profiles Tab

| Key | Action |
|-----|--------|
| `n` | New profile |
| `Enter` | Edit selected profile / submit **Save** button in editor |
| `Esc` | Cancel editing (prompts to discard unsaved changes) |
| `d` | Duplicate profile |
| `x` | Delete profile |
| `L` | Launch selected profile |
| `/` | Filter profiles |
| `Ctrl+T` | Toggle Essentials / Advanced sub-tab (while editing) |

### Launcher Tab

| Key | Action |
|-----|--------|
| `Enter` | Launch selected profile |
| `b` | Toggle background / foreground mode |
| `k` | Kill running instance |

### Monitor Tab

| Key | Action |
|-----|--------|
| `Enter` | Subscribe to selected instance |
| `u` | Unsubscribe from instance |
| `k` | Kill instance |
| `r` | Restart instance |
| `l` / `s` / `m` | Switch sub-view: Logs / Slots / Metrics |

### Models Tab

| Key | Action |
|-----|--------|
| `/` | Filter models |
| `c` | Copy model path to clipboard |

## Configuration

Configuration is stored in `~/.config/llama-cpp-loader/config.toml`:

```toml
[paths]
profiles_dir = "~/.config/llama-cpp-loader/profiles"
log_dir = "~/.local/state/llama-cpp-loader/logs"
state_dir = "~/.local/state/llama-cpp-loader"

[models]
search_paths = ["~/.lmstudio/models", "~/models"]

[ui]
default_tab = "profiles"
keybindings = "default"
```

See [docs/config.md](docs/config.md) for detailed configuration options.

## Directory Structure

| Path | Purpose |
|------|---------|
| `~/.config/llama-cpp-loader/config.toml` | Application configuration |
| `~/.config/llama-cpp-loader/profiles/` | Profile JSON files (one per profile) |
| `~/.local/state/llama-cpp-loader/instances.json` | Background instance registry |
| `~/.local/state/llama-cpp-loader/logs/` | Captured stdout/stderr logs |

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

- **`llama-server` not found** — Ensure `llama-server` is compiled and available in your `PATH`, or set `llama_server_binary_path` in `config.toml`
- **Port in use** — Edit the profile and change the port number
- **Model not found** — Verify the model path in the profile or update `search_paths` in `config.toml`
- **Instance not recovering** — Check that `instances.json` exists in the state directory
- **Invalid binary path** — The path must exist and be executable. Use an absolute path (e.g. `/opt/llama.cpp/build/bin/llama-server`) or a bare name resolvable via PATH

For more details, see [docs/troubleshooting.md](docs/troubleshooting.md).

## License

MIT

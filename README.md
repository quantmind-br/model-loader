# model-loader

A terminal UI (TUI) for managing [llama.cpp](https://github.com/ggerganov/llama.cpp) `llama-server` profiles and processes. Built with Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Features

- **Profile Editor** — Create and manage llama-server launch profiles with curated essential flags and an advanced flag editor
- **Model Browser** — Scan configured directories for `.gguf` models with metadata extraction
- **Launcher** — Launch llama-server instances in foreground (with live log streaming) or background (detached)
- **Monitor** — Real-time monitoring of running instances: logs, health status, slot usage, GPU metrics, and throughput
- **Multi-instance** — Run multiple llama-server instances concurrently, each with its own PID and port
- **Instance Recovery** — Background instances survive TUI exit and are recovered on restart
- **Backend Catalog** — Manage multiple llama-server forks/versions with per-backend validation schemas
- **Schema-driven Validation** — Each backend has its own validation schema (auto-generated from `--help`, editable by user)
- **Per-Profile Backend Selection** — Each profile selects a backend from the catalog; validation uses that backend's schema exclusively

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

2. **Launch** (Tab 1 — Launcher):
   - Select a profile from the list
   - Toggle `b` for background mode (default) or foreground
   - Press `Enter` to launch

3. **Create a profile** (Tab 2 — Profiles):
   - Press `n` to create a new profile
   - Fill in the model path, name, and parameters (use `Tab` / `Shift+Tab` to move between fields)
   - Press `Enter` on the **Save** button to persist the profile (or `Esc` to cancel)

4. **Monitor** (Tab 3 — Monitor):
   - Select a running instance to view logs, slots, GPU stats, and metrics

5. **Browse Models** (Tab 4 — Models):
   - Browse `.gguf` files found in configured search paths
   - Filter and copy paths to clipboard

## Keyboard Shortcuts

### Global

| Key | Action |
|-----|--------|
| `1` | Launcher tab |
| `2` | Profiles tab |
| `3` | Monitor tab |
| `4` | Models tab |
| `5` | Backends tab |
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
| `Ctrl+B` | Add new backend to catalog |
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
default_tab = "launcher"
```

See [docs/config.md](docs/config.md) for detailed configuration options.

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

- **`llama-server` not found** — Ensure `llama-server` is compiled and available in your `PATH`. Use `Ctrl+B` in the Profiles tab to register a custom binary location as a backend
- **Port in use** — Edit the profile and change the port number
- **Model not found** — Verify the model path in the profile or update `search_paths` in `config.toml`
- **Instance not recovering** — Check that `instances.json` exists in the state directory
- **Backend schema missing** — Each backend needs a validation schema. Add a backend via `Ctrl+B` to auto-generate one from `--help`, or place a manually edited schema in the backends directory

For more details, see [docs/troubleshooting.md](docs/troubleshooting.md).

## License

MIT

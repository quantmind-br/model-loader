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

2. **Launch** (Tab 1 — Profiles):
   - Select a profile from the list
   - Toggle `b` for background mode (default) or foreground
   - Press `Enter` to launch

3. **Create a profile** (Tab 1 — Profiles):
   - Press `n` to create a new profile
   - Fill in the model path, name, and parameters (use `Tab` / `Shift+Tab` to move between fields)
   - Press `Enter` on the **Save** button to persist the profile (or `Esc` to cancel)

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
| `Tab` / `Shift+Tab` | Next / previous tab |
| `q` | Quit |
| `?` | Show help |

### Profiles Tab

| Key | Action |
|-----|--------|
| `Enter` | Launch selected profile |
| `E` | Edit selected profile |
| `n` | New profile |
| `d` | Duplicate profile |
| `x` | Delete profile |
| `b` | Toggle background / foreground mode |
| `k` | Kill most recent launched instance |
| `r` | Refresh profile list |
| `p` | Pin selected profile |
| `I` | Import profiles from JSON bundle |
| `u` | Undo last import |
| `e` | Export all profiles to JSON bundle |
| `Ctrl+T` | Toggle Essentials / Advanced sub-tab (while editing) |
| `/` | Filter profiles |

### Server Tab

| Key | Action |
|-----|--------|
| `v` | Cycle Logs / Slots / Metrics / History sub-views |
| `Space` | Pause / resume log scroll |
| `k` | Kill selected instance |
| `r` | Restart selected instance |
| `H` | Open history chart |
| `1` / `2` / `3` / `4` | History chart window: 1h / 6h / 24h / 7d |
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

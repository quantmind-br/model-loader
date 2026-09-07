---
type: Runbook
title: Operations and configuration
description: Operator runbook for model-loader — runtime and binary requirements, the TOML configuration surface, where state lives on disk, and the troubleshooting guidance for common failures (missing binaries, schema generation, port conflicts, instance recovery).
tags: [operations, runbook, config, troubleshooting, dependencies]
---

# Operations

## Runtime requirements

- **Go** ≥ 1.26.2 (toolchain pinned in `go.mod`). **No CGO**. No git submodules; `backends/*` is vendored source under `.gitignore` (manage each as its own upstream checkout).
- **`llama-server`** in `PATH` (used for auto-generating backend schemas). `llamabin.DefaultName` is the default backend; PATH lookup falls back if no `default_backend_id` is in config.
- **(Optional)** `nvidia-smi` for GPU monitoring (`internal/service/monitor/gpu.go`).

## Binary dependencies

Backends are not hard-coded — operators register each via `model-loader backend add --executable <path> --kind <kind>`.

| Binary | Where referenced |
|--------|------------------|
| `python3` | `llamabin.ResolveCommandWithPythonFallback`; backend builders in `processmgr/launch.go::makeCommand`. Compound commands (`python -m sglang.launch_server`) are supported. |
| `nvidia-smi` | GPU monitoring (optional). |
| `bwrap` | `benchmark/codegenbench.go` sandbox; falls back to bare python. |
| `docker` | `terminalbench.go`, `deepswe.go`, `swebenchpro.go` (required by agentic modes). |
| `tb` (Terminal-Bench CLI) | `tbDefaultCmd` in `terminalbench.go`. |
| `pier` (datacurve-ai/pier) | `deepDefaultCmd` in `deepswe.go`. |
| `vllm`, `sglang`, `dflash_server`, `unsloth`, `beellama`, `buun`, `tabbyapi` | User-registered; never hard-coded. |
| `lms` (LM Studio), `ft` (FreeToken) | Launched through `backends/lms/lmstudio-serve.sh` / `backends/freetoken/freetoken-serve.sh` wrapper scripts; user-registered, never hard-coded. |

## Configuration

`~/.config/model-loader/config.toml` is Viper-loaded (`internal/config/config.go`). First run creates a default.

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

[serve]
host = "127.0.0.1"
port = 4321
health_check_timeout_sec = 360   # serve default is larger than the proxy's 180s
```

The `[benchmark]` table configures the evaluation engine (max_tokens, limit, timeout, judge, and per-agentic-mode sub-tables `terminalbench`/`swebenchpro`/`deepswe`). See [Benchmark Engine](benchmark.md) and `docs/BENCHMARK.md`. Full configuration reference: `docs/config.md`.

## State on disk

See [Architecture](architecture.md) for the on-disk state layout. Key locations: `~/.config/model-loader/` holds config, profiles, and the backend catalog + schemas; `~/.local/state/model-loader/` holds the single-instance lock, the instance registry, exit history, proxy state, logs, metrics, benchmark runs, and downloads.

## Development commands

The Makefile has exactly three targets:

```bash
make build       # go build -ldflags "<version + build_date>" -o bin/model-loader ./cmd/model-loader
make install     # GOBIN=~/.local/bin go install ./cmd/model-loader
make tests       # go test ./...
```

Frequent `go` invocations:

```bash
go test ./internal/service/llamahelp -update          # regenerate llama-server --help golden
go run ./cmd/regenerate-schemas                       # re-parse every backend --help
go run ./cmd/scripts/print_args.go <profile-id>       # inspect resolved exe + args
./bin/model-loader serve                               # headless proxy daemon (no flock)
```

When touching a `*help` package, re-run `go run ./cmd/regenerate-schemas`. The root golden pair is `testdata/help-v10686.{txt,golden.json}`; a duplicate embed copy at `internal/service/backendschema/testdata/` must be kept byte-identical by hand.

## External API through Cloudflare

`deploy/cloudflare-model-loader/` exposes the proxy externally **without model-loader authenticating anything**. The trust chain is: Cloudflare edge → named Tunnel `model-loader-quantforge` → Caddy gateway on `127.0.0.1:4322` (checks `Authorization: Bearer $MODELLOADER_API_KEY`) → model-loader proxy on `127.0.0.1:4321`. The public hostname is `model-loader.quantforge.com.br`; cloudflared keeps its metrics endpoint on loopback `127.0.0.1:49321`.

**Loopback bind, no Host matcher (operator ruling).** The gateway site is `:4322` with `bind 127.0.0.1` and no hostname condition. Traffic that reaches that port comes from the Tunnel or from this machine — both already inside the boundary the bind draws — so repeating the DNS name in a matcher adds no protection. Enforcement is the `@authorized` header matcher; every other request gets the fixed `invalid_api_key` JSON 401 with `WWW-Authenticate: Bearer`.

**Follows the proxy unit.** `model-loader-proxy.service` (`Type=notify`) owns the proxy when the TUI manages it: it signals `READY=1` only after its own `/_status` probes healthy and pulses the systemd watchdog only while HTTP stays healthy. `model-loader-api-gateway.service` and `model-loader-cloudflared.service` are `BindsTo`+`PartOf`-bound to it — they start after `READY=1`, stop when the proxy stops or dies (even on `SIGKILL`, via cgroup containment), and restart independently on isolated failures without touching the proxy. A manual `model-loader serve` never starts or stops the exposure stack.

The TUI supervisor auto-detects the loaded proxy unit and drives it via `systemctl --user`; without the unit (other OSes, no user systemd, unit not installed) it keeps the legacy detached-process supervision with `proxy-state.json`. Neither the proxy unit nor the gateway/Tunnel are enabled at boot — the TUI (or the operator) starts the proxy on demand. Linger keeps them alive after logout.
> **Field note (2026-09-03, Arch Linux, systemd 261):** `ProtectHome`/`ProtectSystem` on a unit whose binary lives under `~/.local/bin` breaks `ExecStart` resolution on this host — verified `203/EXEC` and `226/NAMESPACE` across `yes`/`read-only`/`tmpfs`, even with `ReadWritePaths` exceptions. The proxy and gateway units therefore carry no `Protect*` filesystem namespacing (they keep `PrivateTmp`, `NoNewPrivileges`, and AF-restricted sockets); their trust boundary is the loopback bind plus the gateway's Bearer check. The Tunnel unit keeps its strict profile (binary at `/usr/bin`). Revisit if the binaries move out of `$HOME`.

### Install

Needs `caddy`, `cloudflared`, `curl`, `jq`, `python3`, and user systemd. The key is read only from the environment and never appears in argv, logs, or output:

```bash
export MODELLOADER_API_KEY
deploy/cloudflare-model-loader/install.sh
```

The installer renders `Caddyfile`, `gateway.env` (mode `0600`), `cloudflared.yml`, and the `dns-route` marker into `~/.config/model-loader/external-api/`, copies the PATH `caddy` into the private `~/.local/lib/model-loader/bin/caddy` (mode `0755`, pinned by the gateway unit), stages and `systemd-analyze`-validates the proxy, gateway, and Tunnel units before installing them into `~/.config/systemd/user/`, retires the legacy 1s polling watcher units, reuses or creates the Tunnel, provisions DNS with `--overwrite-dns=false` (an existing conflicting record stays a hard failure), scrubs inherited `TUNNEL_*` variables, refuses to proceed when `127.0.0.1:4321` is held by a non-unit legacy proxy, enables lingering, starts the proxy unit (which pulls gateway + Tunnel after `READY=1`), and finishes by running the verifier. It rejects keys outside `A-Za-z0-9._~+/:,@=-`: quotes, whitespace, `#`, `$`, `%`, or backslash parse differently across Caddy's envfile loader, systemd's `EnvironmentFile`, and the `{$MODELLOADER_API_KEY}` expansion.

### Verify

```bash
deploy/cloudflare-model-loader/verify.sh
systemctl --user status model-loader-proxy.service
systemctl --user status model-loader-api-gateway.service
systemctl --user status model-loader-cloudflared.service
```

`verify.sh` keeps the secret off argv (curl reads headers from a mode-`0600` file in a mode-`0700` temp directory, `--disable` ignores `~/.curlrc` and option-setting env vars, `--noproxy` stops ambient proxies from diverting loopback probes) and prints only non-secret PASS/FAIL evidence: 401/200 boundaries locally and on the public hostname, a non-destructive `/_admin/load` smoke check, a loopback allowlist over ports 4321/4322/49321, proxy-unit activeness with valid `MainPID`, readiness + watchdog state, gateway/Tunnel `BindsTo` attachment, absence of the retired polling watcher, the pinned private Caddy copy validating the config, cloudflared HA metrics, and file permissions.

### Client contract

```bash
curl https://model-loader.quantforge.com.br/v1/models \
  -H "Authorization: Bearer $MODELLOADER_API_KEY"
```

The gateway strips the `Authorization` header before proxying, so model-loader never sees the key. **The public key grants the whole proxy surface, including `/_admin/load` and `/_admin/unload`** — treat it as an operator credential. Local `127.0.0.1:4321` remains unauthenticated: anything on this machine can call the admin routes without a key, which is the pre-existing local trust model, unchanged by this feature.

### Key rotation

1. If the new value contains a newline or carriage return, stop — the envfile format cannot represent it.
2. Write `MODELLOADER_API_KEY="<escaped-value>"` to `~/.config/model-loader/external-api/gateway.env` with mode `0600` using a local secret-aware editor, or rerun `install.sh` with the new exported value.
3. `systemctl --user restart model-loader-api-gateway.service`
4. `deploy/cloudflare-model-loader/verify.sh`

### Rollback

```bash
systemctl --user stop model-loader-proxy.service
systemctl --user stop model-loader-cloudflared.service
systemctl --user stop model-loader-api-gateway.service
# 1. Manual step, required before the Tunnel delete: the installed
#    cloudflared CLI has NO DNS-route deletion command, so delete the
#    model-loader.quantforge.com.br record in the Cloudflare dashboard.
# 2. Only after that record is gone, delete the Tunnel. Do not use
#    `tunnel delete --force`, which can hide remaining dependencies.
cloudflared tunnel delete model-loader-quantforge
# 3. Finally remove the local state and reload systemd.
rm -rf ~/.config/model-loader/external-api/
rm ~/.config/systemd/user/model-loader-proxy.service \
   ~/.config/systemd/user/model-loader-api-gateway.service \
   ~/.config/systemd/user/model-loader-cloudflared.service
systemctl --user daemon-reload
```

Order matters: deleting the DNS route first keeps the tunnel deletion from orphaning the `model-loader.quantforge.com.br` record, and `--force` would paper over any dependency Cloudflare still reports. After the block above, nothing of the external API remains on this machine or in Cloudflare.

## Workstation calibrations and audit reports

Workstation-level calibrations, performance audits, and tuning reports for the reference dual-RTX 3090 setup are documented under repository root and `docs/reports/`:
- **Qwen3.8-27B Syv Audit (`QWEN38_27B_AUDIT_REPORT.md`)**: Full audit against upstream commit `0e951951` for vLLM 0.28.0 (torch 2.13.0+cu130, SM86, TP=2). Details why BF16 KV cache + FLASH_ATTN with verify split-KV (`VLLM_SPEC_DECODE_ATTN=1`) outperforms fp8 KV, how INT8 activations restricted to MLP (`VLLM_MARLIN_INPUT_DTYPE=int8`, `VLLM_MARLIN_INT8_INCLUDE_RE=mlp`) accelerates 44k prefill without accuracy regression, and how `VLLM_DFLASH2_LOOKUP=1` cuts warm TTFT.
- **Qwen3.8 Flash-Next Tuning (`docs/reports/qwen-flash-tuning-results.md`, `docs/reports/qwen3.8-flash-next-dual-rtx3090-2026-09-04.md`)**: Tuning across llama-cpp (cache96) and ik-llama-cpp (FIFO ubatch1024) at 256k context; covers the upstream ik request-local `ignore_eos` fix and checkpoint FIFO preservation. See [Benchmark Engine](benchmark.md) for probe token aggregation details.
- **Ornith-1.5 35B-A3B AutoRound W4A16**: Calibrated for 256k context with DFlash2 (7 drafts + lookup), `max-num-batched-tokens 4096`, and balanced performance mode.

## Troubleshooting

- **`llama-server` not found** — ensure it is compiled and in `PATH`. Backends tab (`4`) → `n` registers a custom binary location as a backend.
- **Port in use** — backend instance ports are dynamically assigned by the process manager (profile-level port fields are ignored); for proxy port collisions (default `127.0.0.1:4321`), terminate the conflicting process or change `[serve].port` in `config.toml`.
- **Model not found** — verify the model path in the profile or update `search_paths` in `config.toml`.
- **Instance not recovering** — check that `instances.json` exists in the state directory; `Reconcile` runs at boot for state owners.
- **Backend schema missing** — each backend needs a validation schema. Add a backend via the Backends tab (`n`) to auto-generate one from `--help`, or place a manually edited schema in the backends directory.
- **Health-check timeout on large models** — bump `[serve].health_check_timeout_sec` (serve default is 360s vs the proxy's 180s).
- **Python backend logs arrive in bursts** — `PYTHONUNBUFFERED=1` is injected at spawn; if you see buffering, confirm the env is reaching the process.

Full troubleshooting guide: `docs/troubleshooting.md`. Agentic-mode setup: `docs/swe-bench-pro.md`, `docs/deep-swe.md`, `docs/BENCHMARK.md`.

## Direct dependencies

- CLI: `spf13/cobra v1.10.2`, `spf13/viper v1.20.0-alpha-6`.
- TUI: `charmbracelet/{bubbletea v1.3.10, bubbles v1.0.0, lipgloss, huh v1.0.0, glamour v1.0.0, x/ansi, x/exp/teatest}`.
- Other: `fsnotify/fsnotify v1.7.0`, `atotto/clipboard v0.1.4`, `mattn/go-runewidth v0.0.19`, `tiktoken-go/tokenizer v0.7.0`.

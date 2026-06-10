# Automatic Port Assignment + Proxy-Only Communication — Design

**Date:** 2026-06-10
**Status:** Approved
**Scope:** processmgr, profilestore, configweb, httpproxy, proxysupervisor, benchmark, TUI pages, CLI

## Problem

Today every profile must carry a user-chosen `Args["port"]`. Port conflicts are
only caught at launch (`checkPortFree`), duplication does best-effort bumping,
and four different consumers (benchmark, TUI, CLI, playground) talk straight to
`http://127.0.0.1:<port>`. The user wants ports assigned automatically and the
HTTP proxy (`model-loader serve`) to be the **only** communication channel with
profile instances.

## Decisions (user-approved)

1. **Ephemeral OS ports** — bind `127.0.0.1:0` at launch time; the OS picks a
   free port. No configuration, no conflicts.
2. **`port` removed from profiles entirely** — stripped from the schema, the
   web editor, and existing profile JSON (lazy migration on load/save).
3. **Clients via proxy; internal plumbing direct** — benchmark, playground,
   TUI and CLI go through the proxy. `processmgr.WaitHealthy` and the reverse
   proxy target inside the proxy process keep hitting the instance port
   directly (that is how the proxy knows the instance is up).
4. **Proxy auto-starts** — the TUI (and the `benchmark` CLI command) start the
   proxy via `proxysupervisor` if it is not already running. Profile
   launch/stop from the TUI goes through `POST /_admin/load` /
   `POST /_admin/unload`.

## Accepted behavioral consequences

- **One loaded model at a time.** The proxy's swap semantics become the only
  lifecycle: loading profile B unloads profile A. The TUI's multi-instance
  launch flow is removed.
- **Foreground launch mode is removed from the TUI.** The proxy launches
  everything in background; log tailing remains the observation channel.

## Design

### 1. Port allocation (`internal/service/processmgr`)

- `prepareLaunch` no longer reads `Args["port"]` nor calls `checkPortFree`.
- New helper `allocateEphemeralPort()` — `net.Listen("tcp", "127.0.0.1:0")`,
  capture the port, close the listener, return it. (Small close-then-use race
  is accepted; standard practice.)
- The allocated port is injected into a **copy** of `p.Args` (any user-supplied
  `port` is discarded first) before `BuildArgsForBackend`, so every backend
  kind (llama-server, sglang, vllm, dflash, beellama, buun) receives its port
  flag exactly as it does today.
- `launchPlan.port` keeps feeding `RunningInstance.Port`, log file names,
  `WaitHealthy`, and `instances.json`. The persisted registry format does not
  change — the port simply becomes an internal runtime detail.
- `portFromProfile`, `checkPortFree`, and `ErrPortBusy` are deleted (plus any
  references in CLI/UI error mapping).

### 2. Profile model (`profilestore`, `configweb`, docs)

- `FSStore` strips `Args["port"]` when loading and when saving profiles —
  existing profiles migrate transparently the first time they are touched.
- `Duplicate` loses the port-bump logic (`usedPorts`, `nextFreePort`).
- The web editor treats `port` as a **reserved flag**: filtered out of the
  schema-driven form rendering and rejected on submit. The backend schemas
  themselves are not edited (they come from `--help` parsing); filtering
  happens at the presentation/validation layer.
- `docs/profile-schema.json`: remove `port` from examples; document it as
  reserved/ignored (manager-assigned).

### 3. Proxy as the only channel (`httpproxy`, `proxysupervisor`, TUI, CLI)

- **Auto-start:** at TUI boot, after `Supervisor.Reconcile()`, if the proxy is
  not running, `Supervisor.Start()` is invoked. The `benchmark` CLI command
  does the same before running.
- **TUI launch path:** `profiles_launch.go` and `server_restart.go` stop
  calling `processmgr.Launch`; they POST to `/_admin/load` (body
  `{"profile_id": "..."}`) on the supervised proxy. Stop/unload posts to
  `/_admin/unload`. A small HTTP client helper lives in `proxysupervisor` (it
  already knows host/port and is the TUI's interface to the proxy).
- **Benchmark:** `Runner.ensureInstance` is replaced by a proxy-backed flow —
  ensure proxy running, `POST /_admin/load`, base URL = proxy address, all
  requests carry `"model": <profile_id>`. For crash diagnostics, `Status`
  gains `loaded_log_path` (snake_case + legacy-tolerant unmarshal like the
  other fields). The llama-bench benchmark type (standalone binary, no server
  traffic) is unchanged.
- **Playground modal** (currently inert): when wired, it must use the proxy
  base URL.
- **UI/CLI display:** profile-facing screens show the proxy address as the
  access point. The internal instance port may remain visible in the Server
  monitor and `instance` CLI output as debug information (display only, not a
  communication channel).

### 4. Health checks (unchanged)

`WaitHealthy(pid, port)` and `newReverseProxy(port)` continue to hit
`127.0.0.1:<port>` directly inside the proxy process. This is internal
plumbing, invisible to users.

## Rejected alternative

Making the proxy "adopt" instances launched by the TUI (querying processmgr
state). Rejected: proxy and TUI are separate OS processes with separate
in-memory `processmgr` state; synchronizing through `instances.json` would
race. Funneling every launch through `/_admin/load` keeps a single lifecycle
owner.

## Testing

- Unit: `allocateEphemeralPort`, per-backend port-arg injection, profilestore
  port stripping (load/save/duplicate), configweb reserved-flag filtering and
  submit rejection, `Status.loaded_log_path` round-trip.
- Handler: existing httpproxy swap tests keep passing (port now ephemeral).
- Benchmark: runner tests against a fake proxy (httptest server exposing
  `/_admin/load`, `/_status`, `/v1/...`).
- TUI: page tests updated for the proxy-backed launch/stop commands and the
  removal of foreground mode.
- Golden fixtures refreshed via `go test ./... -update` where forms/schema
  output changes.

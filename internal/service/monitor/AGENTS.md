# AGENTS.md — internal/service/monitor

## OVERVIEW

Streams logs, slot snapshots, health status, GPU stats, and rolling metrics from a running inference backend via a single `Subscribe` call. Six goroutines per subscription feed a unified event channel.

**Backend-agnostic by design.** The log tailer reads a file on disk; it does not know whether that file was produced by llama.cpp (C++, per-line flush), vLLM (Python, requires `PYTHONUNBUFFERED=1` injected by `processmgr.buildLaunchEnv`), SGLang, DFlash, or Unsloth. The slot/GPU pollers hit an OpenAI-shaped HTTP endpoint, so any backend that exposes `/health` and `/slots` (or proxies them) works without code changes.

## WHERE TO LOOK

| File | Purpose |
|------|---------|
| `monitor.go` | `Manager` interface, `MonitorEvent` types, `Config`, sentinel errors |
| `subscribe.go` | `Subscribe` spawns 6 goroutines: log tail, log pump, slots poller, slots pump, GPU poller, metrics ticker (each event source has its own producer + pump pair) |
| `slots.go` | Polls `GET /health` and `GET /slots` on the server port |
| `gpu.go` | `nvidia-smi` fallback; `gopsutil` is a no-op placeholder |
| `logs.go` | `fsnotify`-based log file tailer using `bufio.ReadString('\n')` |
| `metrics.go` | Rolling window aggregator: tokens/s from log regex (llama.cpp format), requests/s from slot `n_decoded` diffs |
| `ring.go` | Fixed-capacity ring buffer for latest log lines |

## HOW THE LOG TAILER WORKS (and its pitfalls)

`logFollower.run` (`logs.go:45-92`) is a line-oriented tail driven by fsnotify:

1. On startup, `bufio.NewReader(l.file)` reads from the **current offset** (not from byte 0). Pre-existing content is flushed once via the initial `emit()` call. **Implication**: a `Subscribe` started after the backend has already written 1000 lines will show those 1000 lines immediately, but a `Subscribe` started before any write must wait for `fsnotify.Create` — and if the file already exists (which it does, since `processmgr.Launch` uses `O_CREATE`), only `Write` events fire.
2. The select loop arms on `fsnotify.Write|fsnotify.Create`. Each event calls `emit()`, which loops on `ReadString('\n')` until `io.EOF` or any error. **This works for any backend that writes line-terminated records to its stdout/stderr file** — which includes llama.cpp, vLLM/SGLang/Unsloth *only when `PYTHONUNBUFFERED=1` is set* (otherwise Python block-buffers to 4KB chunks).
3. **Edge case (partial lines)**: `emit()` *does* flush a trailing partial read on `io.EOF` — the `len(line) > 0` branch (`logs.go:54`) runs before the EOF return (`logs.go:65`), so a backend that exits with its last line missing `\n` still surfaces that line. This deliberately favors crash-message visibility. The trade-off is that a single logical line split across two OS flushes can be emitted as two lines (fragmentation); rare in practice since llama.cpp and unbuffered Python flush per line. See `BUGS.md` L1.

## CONVENTIONS

- All pollers drop events on backpressure (non-blocking send with `default`)
- Consumers type-assert `MonitorEvent.Data` based on `EventSource`
- `HTTPDoer` interface swaps in a mock client for tests
- `New(Config)` applies defaults for all zero values
- Log buffer in the UI (`subState.logs` in `ui/pages/server_monitor.go`) is capped at **2000 lines**, FIFO. This is independent of `Config.LogRingSize` (which feeds the crash-dump ring in `monitor/ring.go`).

## ANTI-PATTERNS

- DO NOT block the event channel: Subscribe uses a 256-buffered channel and drops on overflow
- DO NOT expect `gopsutil` GPU data: it always returns false; rely on `nvidia-smi`
- DO NOT add backend-specific code paths to `logs.go` or `subscribe.go` — log streaming is intentionally backend-agnostic. Backend-specific knobs (e.g. Python unbuffering) belong in `processmgr/launch.go` at spawn time, not in the consumer pipeline.

## NOTES

- `Subscribe` requires a non-empty `logPath`; it validates before spawning goroutines
- Cancel func returned by `Subscribe` waits on `sync.WaitGroup` before closing the channel
- `Metrics` window defaults to 60s; `LogRing` capacity defaults to 2000 lines
- The tokens/s regex in `metrics.go` matches the literal `N tokens per second` form (`tokensPerSecRe`, `metrics.go:10`) — a llama.cpp-style log line. For vLLM/SGLang, tokens/s comes exclusively from the slot `n_decoded` diff path. The log-regex path is therefore a **no-op for non-llama.cpp backends** but still consumes lines through `agg.observeLog` — harmless but wasted work. See `BUGS.md` L3.

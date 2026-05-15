# AGENTS.md — internal/service/processmgr

## OVERVIEW
Process lifecycle service: spawns llama-server as background (detached) or foreground (attached) processes, tracks them in memory, and persists state to instances.json.

## WHERE TO LOOK

| File | Purpose |
|------|---------|
| processmgr.go | Manager interface, LaunchMode enum, sentinel errors |
| manager.go | fsManager implementation: Launch, Kill, List, WaitHealthy, TailLogs |
| args.go | BuildArgs(p domain.Profile) → []string for llama-server CLI |
| registry.go | JSON load/save for instances.json |
| recover.go | Reconcile(): drops zombie PIDs, keeps live llama-server processes |
| liveness.go | Background goroutine probing PIDs; marks dead ones Crashed |

## CONVENTIONS

- **LaunchMode**: Background = detached + log file + registry persistence. Foreground = attached TUI stream, max 1 at a time.
- **Logs**: Per-PID files under LogDir (`<pid>.log`), appended via os.OpenFile(O_APPEND).
- **Health check**: TCP dial on the profile's port; timeout is configurable.
- **Flag canonicalization**: `canonicalFlag()` maps user-friendly short keys (e.g. `"ngl"`) to long-form llama-server flags (`"n-gpu-layers"`) via `shortToLong` table.
- **Tests**: Use `fakeBinary(t)` helper for a no-op executable; `freePort(t)` to avoid conflicts.

## ANTI-PATTERNS

- DO NOT spawn multiple foreground instances; manager enforces only one.
- DO NOT add a 6th out-of-lock `saveRegistry` callsite without documenting
  it in the "Wait goroutine lifecycle" table below. The pattern is
  intentional (lock budget vs file I/O latency); the count is the contract.
- DO NOT spawn a new long-lived goroutine in this package without a
  `defer recover()` that logs to `m.logger`. bubbletea's recover net does
  not cover this layer.

## WAIT GOROUTINE LIFECYCLE

Two `cmd.Wait` reaper goroutines run per spawned process:

1. **Background**: spawned in `Launch` AFTER the `m.tracked[pid]=inst`
   insert. Captures `pid`, `logPath`, and the `attemptID` passed in by
   the caller via the method receiver `m`.
2. **Foreground**: spawned in `launchForeground` AFTER the
   `m.tracked[pid]=inst` insert + registry-snapshot-under-lock. The
   historical ordering (pre-insert) was reordered so the enrichment body's
   re-read of `m.tracked[pid]` sees a populated entry.

Both bodies are `m.waitEnrichment(cmd, pid, logPath, attemptID)` and follow
this contract:

- **Wait + file I/O happen OUTSIDE `m.mu`.** Stderr tail (50 lines) is read
  via `os.ReadFile` before any lock is acquired so a slow log volume does
  not serialize Launch / Kill / liveness.
- **Re-read `m.tracked[pid]` under `m.mu`.** If `!ok` the entry was deleted
  by Kill or wholesale-replaced by Reconcile. We exit without mutating
  either map. This is the SOLE race defense — no stop chan is needed
  because `cmd.Wait` returns when the process dies and Kill causes process
  death, so the goroutine always drains naturally.
- **Win-by-liveness guard.** The 5-second liveness ticker also mutates
  `m.tracked` under the same lock and sets `Crashed=true` + `ExitedAt`. If
  `cur.Crashed` is already true when `waitEnrichment` acquires the lock,
  we KEEP liveness's `ExitedAt` and only fill the four fields liveness
  cannot observe via PID probe alone (`ExitCode`, `ExitSignal`,
  `ExitReason`, `StderrTail`).
- **`saveRegistry` is called outside the lock.** This is the **5th** of
  the out-of-lock callsites. Pattern is identical to `liveness.go:60`.

### Out-of-lock saveRegistry callsites

`saveRegistry` is invoked from 5 sites, ALL of them after `m.mu.Unlock()`,
following snapshot-under-lock semantics:

| File | Caller | Note |
|------|--------|------|
| `manager.go` | `Launch` (background, post-insert) | original |
| `manager.go` | `Kill` (post-delete) | original |
| `manager.go` | `launchForeground` (post-insert) | original |
| `liveness.go` | Liveness ticker (post-Crashed mutation) | original |
| `manager.go` | `waitEnrichment` (post-exit fields mutation) | NEW |

Holding `m.mu` across file I/O would serialize launches against a
possibly slow JSON write. Trade-off: two concurrent mutators may produce
disagreeing on-disk states between their two saves — but Reconcile reads
disk only at boot, and at that point any in-flight mutation has already
fully serialized.

### Goroutine panic safety

bubbletea v1.3.10 wraps every `tea.Cmd` invocation in `defer recover()`,
so panics inside `launchProfileCmd`'s returned closure are contained. BUT
the three long-lived goroutines spawned by `processmgr` (2 `cmd.Wait` +
1 liveness) are outside that net. A panic in any of them would corrupt
the alt-screen (default Go runtime handler prints to stdout before
`os.Exit(2)`, bypassing defers).

All three goroutines install:

```go
defer func() {
    if r := recover(); r != nil {
        m.logger.Error("<name>_goroutine_panic",
            "panic", r, "stack", string(debug.Stack()))
    }
}()
```

The logger writes file-only — stderr stays untouched, the TUI framebuffer
survives, and the panic is grep-able in `model-loader.log`.

### What NOT to do

- DO NOT use a per-PID stop chan to "cancel" the Wait goroutine.
  `cmd.Wait` blocks on `syscall.Wait`, which returns when the kernel
  delivers `SIGCHLD`. Killing the process is the cancel signal.
- DO NOT hold `m.mu` across the `readStderrTail(logPath, 50)` call. The
  lock budget is microseconds; file I/O is milliseconds-to-seconds.
- DO NOT overwrite `cur.ExitedAt` if `cur.Crashed` is already true.
  Liveness owns the timestamp once it has fired; the Wait body fills only
  the four exit-cause fields.

## NOTES

- Log files are never rotated by processmgr; external cleanup required.
- `Reconcile` runs at TUI boot after `NewWithCheck`.

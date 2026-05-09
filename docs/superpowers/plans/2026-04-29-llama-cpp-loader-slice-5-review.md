# Slice-5 Integration Review (`feat/slice-5`)

**Range:** `7080378..b17c7bb` (16 commits, 1837 +/13 -)
**Build:** `go build ./...` clean.
**Tests:** `go test ./internal/...` all green (monitor 0.52s, ui 0.21s, pages 0.21s, processmgr 1.31s).

---

## Strengths

1. **Channel ownership is correct.** `subscribe.go:39-50` — follower owner-closes `logLines`, slots-poller owner-closes `slotsRaw`. Pumps drain those upstream chans and return when they close OR ctx cancels. `out` is closed exactly once by the dedicated closer goroutine after `wg.Wait()` (`subscribe.go:131-135`). No double-close path. The pumps use `select { case out <- ev: default: }` so they never block on a slow consumer; they cannot deadlock on `out` after their upstream closes.
2. **Cancel is idempotent and synchronized.** `subscribe.go:137-141` — cancel calls `sub.cancel()` then waits on `sub.doneCh`. The closer goroutine (line 131) closes `doneCh` only after `wg.Wait()` resolves all 6 producers. Multiple cancels cannot send-on-closed because `cancel()` is the stdlib `context.CancelFunc` (idempotent) and `<-sub.doneCh` on a closed chan returns immediately.
3. **`metricsAgg.observeSlots` is wired into the slots-pump** (`subscribe.go:97-101`), exactly per plan. Refactor commit `9464dde` made the slots-pump the single source of truth for both slot snapshots and request-rate.
4. **Source-aware metrics enum present.** `monitor.go:14-20` — five sources, `SourceMetrics` for aggregator output. `subscribe.go:123` emits `SourceMetrics` on the ticker.
5. **GPU fallback is gopsutil-first then nvidia-smi.** `gpu.go:38-48` — gopsutil tried only when `pid > 0`; nvidia-smi runs unconditionally if gopsutil returns false. Skip on `pid==0` makes test mode deterministic.
6. **`selectedPID()` drives sub-views.** `monitor.go:223,261-269` — bottom region looks up `p.subs[pid]` from the highlighted table row, so arrow keys actually move focus.
7. **Test coverage hits the hot paths**: subscribe fan-in (`subscribe_test.go:15`), Tab cycle (`monitor_test.go:91`), metrics sparkline render (`monitor_test.go:113`), k-kill (`monitor_test.go:148`), Switch-to-Monitor routing (`root_test.go:107`), Healthy emits switch (`launcher_test.go:196`).
8. **`TailLogs` returns sentinel for foreground** (`processmgr/manager.go:197-207`) — foreground has no `LogPath`, so subscribe will fail fast with `ErrLogPathEmpty`. Correct guard.

---

## Issues

### Critical

None. No data races, no leaks under the cancel-then-replay path, no double-close, no deadlock.

### Important

**I-1. Synchronous cancel can briefly stall the UI thread.**
`internal/ui/pages/monitor.go:209` — `_ = st.cancel()` runs inside `applyInstances`, which runs inside `Update`. The cancel func at `subscribe.go:137-141` does `<-sub.doneCh` and that channel only closes after **all 6 goroutines drain**. Two cases drain instantly, four can take up to one tick:

- log-pump: returns on `<-ctx.Done()` immediately.
- slots-pump: returns on `<-ctx.Done()` immediately.
- metrics-tick: returns on `<-ctx.Done()` immediately.
- gpu-poller: `gpu.go:23-35` — `pollOnce` may be running `exec.CommandContext` against `nvidia-smi`. ctx cancel will SIGKILL the child but `cmd.Output()` waits for the kernel to reap. Typical: <50ms. Pathological: longer if `nvidia-smi` hangs on a stuck driver.
- slots-poller: `slots.go:24-37` — `pollOnce` issues two HTTP GETs against the llama-server. If the server has stopped accepting connections (port closed, half-closed FIN_WAIT) the `http.DefaultClient` has no per-request timeout configured here; `monitor.New` does not set `cfg.HTTPClient`, so `main.go:57` runs with `http.DefaultClient` (no timeout). Cancel will close the request via `NewRequestWithContext`, so it returns promptly — verified.
- follower: `logs.go:75-91` — `<-ctx.Done()` returns immediately; defers close the file and watcher.

**Net effect:** worst case a single cancel blocks `Update` for the time it takes to reap a stuck `nvidia-smi`. With one orphan it is invisible. With **N orphans applied in a single `applyInstances` pass**, it is sequential — N×latency. If a user kills several instances in rapid succession off-page and then triggers a `monitorInstancesRefreshedMsg`, the UI freezes for that span.

**Recommendation (slice-6 polish, not a slice-5 blocker):** spawn a goroutine per cancel so UI thread does not block.

```go
for pid, st := range p.subs {
    if !seen[pid] {
        cancel := st.cancel
        go func() { _ = cancel() }()
        delete(p.subs, pid)
        delete(p.chans, pid)
    }
}
```

The subscription tears itself down asynchronously; the UI returns to event loop in microseconds.

**I-2. `monitorEventMsg` re-arm risk on cancelled subs is mitigated, but there is a subtle race.**
`monitor.go:163-166`:

```go
if ch, ok := p.chans[m.ev.PID]; ok {
    cmds = append(cmds, listenCmd(ch))
}
```

The chan is removed from `p.chans` in `applyInstances` BEFORE cancel returns. Sequence:

1. tick 1: `applyInstances` cancels orphan, deletes `p.chans[pid]`.
2. `cancel()` blocks on `wg.Wait()`. Meanwhile the closer goroutine in `subscribe.go:131-135` closes `out`.
3. listenCmd already armed reads from the now-closed chan → returns `nil` → tea drops it. Safe.
4. The Update path that re-arms (line 164) is only entered when a `monitorEventMsg` arrives. If `p.chans[pid]` is already deleted, no re-arm. Also safe.

**However:** if cancel is parallelized (per I-1 fix), the close of `out` happens **after** `applyInstances` deletes from `p.chans`. listenCmd may receive an event already in flight before the close. That event delivers `monitorEventMsg{ev}` with `ev.PID == orphan_pid`. `p.subs[m.ev.PID]` is gone → switch-block falls through (`ok` is false at line 134). Re-arm at line 164 also skipped (`p.chans[pid]` gone). Correct.

**Conclusion:** safe under the I-1 fix as written. Not an issue today, but worth a comment near `applyInstances` documenting "stale events on closed chans are dropped silently in Update."

**I-3. `r` key has no user-visible feedback.**
`monitor.go:120-124` — `r` performs a kill with a code comment "Slice-5: r==kill (real restart needs ProfileStore — deferred to slice 6)." User pressing `r` expecting restart sees the row vanish on the next `applyInstances` tick and no status message. There is no status bar message in MonitorPage at all (the page does not own a status string). Per-task review #18 already noted this as accepted slice-5 scope, but it is a slice-level UX hole worth flagging in the slice-6 entry list.

**Recommendation:** in slice-6 either implement actual restart, or change the binding to print a footer hint like `"r restart (slice-6)"` and leave `r` unbound for now.

### Minor

**M-1. Sparkline empty state visually identical to "all zeros".**
`monitor.go:249-250` — `components.Sparkline(nil, 40)` returns 40 spaces (`sparkline.go:13-15`). The line renders as `tokens/s: ` followed by visible padding. A user with no metrics yet sees the same as a user with all-zero metrics. Suggest a `(no data yet)` placeholder when both `TokensPerSec` and `RequestsPerSec` are nil:

```go
if len(st.mets.TokensPerSec) == 0 && len(st.mets.RequestsPerSec) == 0 {
    bottom = "(metrics warming up — first sample after slots tick)"
    break
}
```

**M-2. `SwitchToMonitorMsg` carries a PID but root.go ignores it.**
`root.go:128-130` — switch tab, drop PID. MonitorPage re-renders the table on its own `monitorInstancesRefreshedMsg` cycle (`Init` only — there is no periodic refresh; `applyInstances` runs only when a refresh msg arrives). The user lands on the Monitor tab with **stale rows** until the next refresh, and the table selection is whatever it was before — not the new PID. The plan explicitly mentions "pre-select the new PID" in `messages.go:5-7` ("root.go consumes this to switch the active tab to Monitor and pre-select the new PID."). This is **a documented intent that is not yet honored**.

**Recommendation (slice-6):** root.go should forward `SwitchToMonitorMsg` to the MonitorPage so it can refresh + select. Today the contract docstring lies.

**M-3. No periodic instance refresh in MonitorPage.**
`monitor.go:99` — `Init() tea.Cmd { return p.refreshInstancesCmd() }`. After boot, the table only re-renders on `monitorInstancesRefreshedMsg`, which is only emitted by `Init`. If the user kills via `k` (line 117) the table row stays until... never, unless something else triggers a refresh. The kill happens, but the row remains visible with stale data. Subscriptions stay alive on dead PIDs.

In practice: `pm.Kill()` succeeds, but `p.applyInstances` is never re-called, so `subs[pid]` is not cancelled and `chans[pid]` is not deleted. The subscription continues polling a dead port (slots returns connection refused, drop on backpressure; gpu still polls).

**Recommendation:** after a `k`/`r` keypress, append `cmds = append(cmds, p.refreshInstancesCmd())` so the table reconciles and the orphan cancel runs. One-line fix:

```go
case m.Type == tea.KeyRunes && len(m.Runes) == 1 && m.Runes[0] == 'k':
    if pid := p.selectedPID(); pid > 0 {
        _ = p.pm.Kill(pid)
        cmds = append(cmds, p.refreshInstancesCmd())
    }
```

This was not flagged in per-task review #18 because that review focused on the keypress path itself, not the full lifecycle. **This is the most material bug in slice-5** — orphan-sub leak after user-driven kill.

**M-4. `MonitorPage` is 269 lines, well within sane TUI page bounds.** No flag.

**M-5. Coverage gap: no test exercises instance removal.**
`monitor_test.go` — every test sets `pm.insts` once and never removes. The orphan-sub cleanup branch at `monitor.go:202-213` is **not covered**. A regression that forgets to cancel an orphan would slip past CI. Suggest a test:

```go
func TestMonitorPage_CancelsOrphanSubOnInstanceRemoval(t *testing.T) {
    cancelCalled := 0
    mm := &countingMonMgr{onCancel: func() { cancelCalled++ }}
    pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
    p := NewMonitorPage(pm, mm)
    p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
    pm.insts = nil // PID 1 vanished
    p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
    if cancelCalled != 1 { t.Fatalf("cancel = %d, want 1", cancelCalled) }
    if _, ok := p.subs[1]; ok { t.Fatal("orphan sub not deleted") }
}
```

**M-6. `MonitorPage.SetSize` is on the pointer receiver, but `p` is stored as `tea.Model` (interface) in `pages` array.** `root.go:91-94` passes the page through `WithMonitorPage(p tea.Model)`. The page is `*MonitorPage` (since `NewMonitorPage` returns `*MonitorPage`). `SetSize` is callable, but root never calls it. The page does receive `tea.WindowSizeMsg` only through `Update`, but **MonitorPage.Update has no `tea.WindowSizeMsg` case** (compare `monitor.go:111-167` vs `launcher.go:98-101`). So `p.width`/`p.height` are always 0 at runtime. The `tbl.SetWidth(w)` call (`monitor.go:96`) is dead code today.

**Recommendation:**

```go
case tea.WindowSizeMsg:
    p.SetSize(m.Width, m.Height)
```

inside `Update`. Without this, the table column widths are fixed at the bubbles defaults regardless of terminal size. Visible issue: on narrow terminals the table truncates.

---

## Plan-Deviation Audit

| Plan item | Implementation | Status |
|---|---|---|
| `monitor.Manager` interface | `monitor.go:73-77` | OK |
| 5 EventSources incl. `SourceMetrics` | `monitor.go:14-20` | OK |
| `Subscribe(pid, port, logPath)` | `subscribe.go:15` | OK |
| Owner-closes channel discipline | follower closes logLines, slots-poller closes slotsRaw, closer closes out | OK |
| metricsAgg `observeSlots` wired | `subscribe.go:97-101` (refactor `9464dde`) | OK |
| gopsutil-first, nvidia-smi-fallback | `gpu.go:38-48` | OK (gopsutil is a no-op stub by design — see comment block lines 50-58) |
| TailLogs(pid) | `processmgr/manager.go:197-207` | OK |
| MonitorPage 3 sub-views | `monitor.go:21-25, 217-258` | OK |
| Tab cycles sub-views | `monitor.go:114-115` | OK |
| k kill / r kill (deferred restart) / Space pause | `monitor.go:116-127` | OK with caveats (M-3, I-3) |
| SwitchToMonitorMsg from healthy | `launcher.go:129-132` + `messages.go:5-9` | Partial — root drops PID payload (M-2) |
| WithMonitorPage builder | `root.go:90-94` | OK |
| main.go wires monitor.New | `main.go:57-58` | OK |
| fake-llama-server /slots | `testdata/fake-llama-server.sh:50-54` | OK |
| Sparkline component | `sparkline.go` | OK |

No problematic deviations. Two intentional deferrals: real restart on `r`, and PID pre-selection in root.

---

## Assessment

**Approved with caveats.** Slice-5 closes correctly: the runtime monitor service is functional, the TUI page renders all three sub-views, and the cross-page launch-to-monitor flow lands on the right tab. Lifecycle invariants hold under the current synchronous-cancel design.

Two slice-level integration bugs **should** be addressed before slice-6 begins:

- **M-3 (highest priority): orphan-sub leak after `k`/`r` keypress.** No refresh trigger after kill → the table stays stale and the cancelled-instance subscription keeps polling forever. Simple one-line fix per keypress.
- **M-6: missing `tea.WindowSizeMsg` handler in MonitorPage.Update** → table never resizes. Visible on narrow terms.

The remaining items (I-1 cancel-blocking, I-3 `r` UX, M-1 sparkline placeholder, M-2 PID pre-select, M-5 orphan-cancel test) are appropriate slice-6 polish.

**Action requested:** confirm M-3 and M-6 will be fixed at slice-5 close (small commits), or explicitly defer to slice-6 with issue tickets.

---

## Files Reviewed

- `/home/diogo/dev/llama.cpp-loader/internal/service/monitor/monitor.go`
- `/home/diogo/dev/llama.cpp-loader/internal/service/monitor/subscribe.go`
- `/home/diogo/dev/llama.cpp-loader/internal/service/monitor/slots.go`
- `/home/diogo/dev/llama.cpp-loader/internal/service/monitor/gpu.go`
- `/home/diogo/dev/llama.cpp-loader/internal/service/monitor/metrics.go`
- `/home/diogo/dev/llama.cpp-loader/internal/service/monitor/logs.go`
- `/home/diogo/dev/llama.cpp-loader/internal/service/monitor/ring.go`
- `/home/diogo/dev/llama.cpp-loader/internal/service/monitor/subscribe_test.go`
- `/home/diogo/dev/llama.cpp-loader/internal/service/processmgr/manager.go`
- `/home/diogo/dev/llama.cpp-loader/internal/ui/root.go`
- `/home/diogo/dev/llama.cpp-loader/internal/ui/root_test.go`
- `/home/diogo/dev/llama.cpp-loader/internal/ui/pages/monitor.go`
- `/home/diogo/dev/llama.cpp-loader/internal/ui/pages/monitor_test.go`
- `/home/diogo/dev/llama.cpp-loader/internal/ui/pages/launcher.go`
- `/home/diogo/dev/llama.cpp-loader/internal/ui/pages/launcher_test.go`
- `/home/diogo/dev/llama.cpp-loader/internal/ui/pages/messages.go`
- `/home/diogo/dev/llama.cpp-loader/internal/ui/components/sparkline.go`
- `/home/diogo/dev/llama.cpp-loader/cmd/model-loader/main.go`
- `/home/diogo/dev/llama.cpp-loader/testdata/fake-llama-server.sh`

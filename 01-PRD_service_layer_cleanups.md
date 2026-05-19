# PRD — Service Layer Cleanups

**Source**: IDEATION_CODE_QUALITY.md
**Generated**: 2026-05-19

## Implementation Order
1. CQ-021 — Document `metricsAgg` locking contract.
2. CQ-009 — Extract per-type validator helpers; keep switch as dispatcher.
3. CQ-018 — Introduce `pumpContext` for the three monitor pumps.
4. CQ-007 — Extract `prepareLaunch` in `processmgr`; share it between background and foreground launch paths.
5. CQ-013 — Split `(*Manager).tick` into `reapCompleted` / `promoteQueued` / `emitEvents`.
6. CQ-014 — Split `runDownload` into `setupOutput` / `resumeOrCreate` / `streamWithProgress`.

---

## CQ-021: Document `metricsAgg` locking contract

### Scope
**In scope**:
- Add a `// Concurrency:` block on the `metricsAgg` type definition naming every writer/reader and the field they touch.
- Field-level inline comments only where the field's owner is not obvious from the writer list.

**Out of scope**:
- Any behavior change.
- Changing the lock granularity, splitting the mutex, or introducing RWMutex.
- Renaming fields.

### Technical Approach
Append a doc block immediately before `type metricsAgg struct` in `internal/service/monitor/metrics.go:22`. Enumerate every method that takes `mu` and every external function (pumps in `subscribe.go`) that calls them.

### Touchpoints
- `internal/service/monitor/metrics.go` — doc block on `metricsAgg`; no code change below line 22.

### Contracts
```go
// metricsAgg accumulates token and request counters that the three monitor
// pumps populate and that runMetricsTick periodically samples to emit
// Metrics events.
//
// Concurrency:
//   - mu guards every field below. Hold mu for every read and every write.
//   - observeLog(at, line):     called from runLogPump; appends to tokens.
//   - observeSlots(at, snap):   called from runSlotsPump; appends to requests,
//                               updates lastSlots and lastTime.
//   - snapshot(now):            called from runMetricsTick; reads tokens and
//                               requests after calling prune(now).
//   - prune(now):               called by observeLog, observeSlots, snapshot
//                               before doing their work; trims out-of-window
//                               samples from tokens and requests.
type metricsAgg struct {
    mu        sync.Mutex
    window    time.Duration
    tokens    []tokenSample   // appended by observeLog
    requests  []reqSample     // appended by observeSlots
    lastSlots map[int]int     // slotID → last NDecoded; written by observeSlots
    lastTime  time.Time       // written by observeSlots
}
```

### Acceptance Criteria
- [ ] `metricsAgg` carries a `// Concurrency:` doc block listing all writers per field.
- [ ] Every existing test in `internal/service/monitor/` still passes (`go test ./internal/service/monitor/...`).
- [ ] `go vet ./internal/service/monitor/...` reports no new findings.

### Dependencies
- None.

---

## CQ-009: Extract per-type validator helpers

### Scope
**In scope**:
- Move each `case domain.FlagType*:` branch of `checkType` into a named free function in the same file.
- Keep `checkType` as the dispatching `switch spec.Type`.
- Preserve every existing error message string verbatim (tests assert on them).

**Out of scope**:
- Adding a `map[domain.FlagType]func(...)` registry.
- Adding new `FlagType` values.
- Changing the public `Validator` API or `domain.FlagSpec`.

### Technical Approach
In `internal/service/validator/rules.go`, extract one helper per flag type. Each returns `""` on success or the existing error message verbatim. `checkType` becomes a 5-line `switch`.

### Touchpoints
- `internal/service/validator/rules.go` — extraction site.
- `internal/service/validator/rules_test.go` — no edits expected; tests already cover each branch.

### Contracts
```go
// checkInt returns "" if val is an integer or an integer-valued JSON number.
func checkInt(val any) string

// checkFloat returns "" if val is any numeric type.
func checkFloat(val any) string

// checkBool returns "" if val is a bool.
func checkBool(val any) string

// checkString returns "" if val is a string.
func checkString(val any) string

// checkEnum returns "" if val is a string present in spec.Choices.
func checkEnum(spec domain.FlagSpec, val any) string

func checkType(spec domain.FlagSpec, val any) string {
    switch spec.Type {
    case domain.FlagTypeInt:    return checkInt(val)
    case domain.FlagTypeFloat:  return checkFloat(val)
    case domain.FlagTypeBool:   return checkBool(val)
    case domain.FlagTypeString: return checkString(val)
    case domain.FlagTypeEnum:   return checkEnum(spec, val)
    }
    return ""
}
```

### Acceptance Criteria
- [ ] `checkType` body is no longer than 8 lines including the switch.
- [ ] `checkInt`, `checkFloat`, `checkBool`, `checkString`, `checkEnum` exist as package-private functions in `rules.go`.
- [ ] `go test ./internal/service/validator/...` passes with no test-file edits.
- [ ] Grep for `map[domain.FlagType]` returns no hits in `internal/service/validator/`.

### Dependencies
- None.

---

## CQ-018: `pumpContext` for monitor pumps

### Scope
**In scope**:
- Introduce a package-private `pumpContext` struct in `internal/service/monitor/subscribe.go`.
- Refactor `runLogPump`, `runSlotsPump`, `runMetricsTick` to take `pumpContext` plus their pump-specific parameters.
- Update the three call sites in `Subscribe`.

**Out of scope**:
- Renaming any pump.
- Changing channel directions or event types.
- Touching `metricsAgg` or `MonitorEvent`.

### Technical Approach
Collect the shared `(ctx, wg, pid, agg, out)` quintuple into one struct. `runLogPump` keeps `ringSize` + `logLines`. `runSlotsPump` keeps `slotsRaw`. `runMetricsTick` keeps `interval`. Build the `pumpContext` once in `Subscribe` and pass it to all three pumps.

### Touchpoints
- `internal/service/monitor/subscribe.go` — type declaration + 3 signature changes + 3 call-site changes.

### Contracts
```go
// pumpContext bundles the parameters shared by every monitor pump.
type pumpContext struct {
    ctx context.Context
    wg  *sync.WaitGroup
    pid int
    agg *metricsAgg
    out chan<- MonitorEvent
}

func runLogPump(pc pumpContext, ringSize int, logLines <-chan string)
func runSlotsPump(pc pumpContext, slotsRaw <-chan MonitorEvent)
func runMetricsTick(pc pumpContext, interval time.Duration)
```

### Acceptance Criteria
- [ ] Each pump signature has at most 3 parameters total.
- [ ] `Subscribe` constructs one `pumpContext` value and passes it to all three pumps.
- [ ] `go test ./internal/service/monitor/...` passes.
- [ ] `go vet ./internal/service/monitor/...` clean.

### Dependencies
- None. (CQ-021 doc block is independent and can land before or after.)

---

## CQ-007: Extract `prepareLaunch` in `processmgr`

### Scope
**In scope**:
- Introduce `launchPlan` struct holding the resolved binary, env, args, and resolved port.
- Introduce `(m *fsManager) prepareLaunch(p domain.Profile) (launchPlan, error)`.
- Refactor `(*fsManager).Launch` and `(*fsManager).launchForeground` to call `prepareLaunch` and consume `launchPlan`.

**Out of scope**:
- Changing the `LaunchMode` enum or the public `Manager.Launch` signature.
- Reordering port-resolution or env-merge logic relative to what `Launch` already does.
- Touching `enrichment.go` or `history.go`.

### Technical Approach
Walk `Launch` (`launch.go:22`) and `launchForeground` (`launch.go:161`) and identify the prefix common to both: resolve binary path, build args from profile, build env from profile (`applyProfileEnv`), resolve port (`portFromProfile` + `checkPortFree`). Move that prefix into `prepareLaunch`. Both callers then receive a `launchPlan` and only diverge at the `exec.Cmd` construction + spawn step.

### Touchpoints
- `internal/service/processmgr/launch.go` — new type + new method; two existing methods shrink.
- `internal/service/processmgr/launch_test.go` (if present) — no semantic change; tests still pass.

### Contracts
```go
type launchPlan struct {
    binary string
    args   []string
    env    []string
    port   int
}

func (m *fsManager) prepareLaunch(p domain.Profile) (launchPlan, error)

func (m *fsManager) Launch(p domain.Profile, mode LaunchMode, attemptID string) (domain.RunningInstance, error)
func (m *fsManager) launchForeground(p domain.Profile, port int, attemptID string) (domain.RunningInstance, error)
```

### Acceptance Criteria
- [ ] `Launch` does not call `applyProfileEnv`, `portFromProfile`, or any binary resolver directly; it routes through `prepareLaunch`.
- [ ] `launchForeground` does not call `applyProfileEnv`, `portFromProfile`, or any binary resolver directly; it routes through `prepareLaunch`.
- [ ] Both functions are at least 25 lines shorter than today.
- [ ] `go test ./internal/service/processmgr/...` passes.
- [ ] A foreground launch and a background launch executed through the TUI succeed end-to-end (smoke test).

### Dependencies
- None.

---

## CQ-013: Split `(*Manager).tick`

### Scope
**In scope**:
- Extract three helpers from `tick` (`internal/service/downloadmgr/manager.go:390-448`):
  - `reapCompleted` — runs under `m.mu`; iterates records, updates `lastSnapshot`, marks abandoned workers, collects pending events and freed slots.
  - `promoteQueued` — runs under `m.mu`; consumes freed slots and returns IDs to spawn.
  - `emitEvents` — runs after `mu` is released; calls `broadcast` and `spawn`.
- `tick` retains the lock scope and orchestrates the three calls.

**Out of scope**:
- Changing the locking contract or making any field public.
- Changing the polling cadence or the `ListRecords` call.
- Touching `Event`/`State` shapes.

### Technical Approach
The `pending` local type currently declared inside `tick` is promoted to file scope (still unexported). `reapCompleted` returns `(events []pending, freedSlots int)` and is called while `m.mu` is held. `promoteQueued` returns `[]ID` and is also called while held. `tick` releases the mutex, then calls `emitEvents(events, toSpawn)` which fires `broadcast` and `spawn`.

### Touchpoints
- `internal/service/downloadmgr/manager.go` — type extraction + 3 new methods + `tick` body shrinks.

### Contracts
```go
type pending struct {
    id    ID
    state State
}

// reapCompleted iterates the on-disk records, reconciles m.lastSnapshot,
// detects dead workers, and reports pending events plus freed slot count.
// MUST be called with m.mu held.
func (m *Manager) reapCompleted(recs []DownloadRecord) ([]pending, int)

// promoteQueued moves IDs from the queue into the active set, bounded by
// freedSlots and m.maxConcurrent. Returns IDs that the caller must spawn.
// MUST be called with m.mu held.
func (m *Manager) promoteQueued(freedSlots int) []ID

// emitEvents fires broadcast(...) for each pending event and spawn(...) for
// each ID. MUST be called with m.mu released.
func (m *Manager) emitEvents(events []pending, toSpawn []ID)

// tick orchestrates the three phases under lock discipline.
func (m *Manager) tick()
```

### Acceptance Criteria
- [ ] `tick` body is no longer than 20 lines.
- [ ] `reapCompleted` and `promoteQueued` carry `// Called with m.mu held` doc comments.
- [ ] `emitEvents` carries `// Called with m.mu released` doc comment.
- [ ] `go test ./internal/service/downloadmgr/...` passes.
- [ ] `go test -race ./internal/service/downloadmgr/...` passes (race detector).

### Dependencies
- None.

---

## CQ-014: Split `runDownload`

### Scope
**In scope**:
- Extract three helpers from `runDownload` (`internal/service/downloadmgr/worker.go:129-235`):
  - `setupOutput` — validates the destination directory, computes the partial path, returns existing size on disk.
  - `resumeOrCreate` — builds the HTTP request (with optional `Range`), opens the partial file in the right mode based on the response status (206 vs 200), returns file handle + body reader.
  - `streamWithProgress` — copies the body into the file with checkpoint writes at the configured cadence.
- `runDownload` orchestrates the three.

**Out of scope**:
- Changing resume semantics.
- Changing checkpoint cadence or `DownloadRecord` shape.
- Adding new retry logic.

### Technical Approach
Walk `runDownload` and identify the three phases — destination setup, HTTP open with resume decision, stream loop. Hoist each into a helper. The mid-phase needs to return both the response and the file handle because the file open mode depends on the response status. Use a small intermediate struct rather than returning four values.

### Touchpoints
- `internal/service/downloadmgr/worker.go` — three new functions + `runDownload` shrinks.

### Contracts
```go
// setupOutput ensures the destination directory exists and reports the
// existing bytes on disk at the partial path (0 if no partial).
func setupOutput(rec *DownloadRecord) (partialPath string, existing int64, err error)

// transferHandle bundles the resources resumeOrCreate hands off to the
// streaming phase.
type transferHandle struct {
    file       *os.File
    body       io.ReadCloser
    appendMode bool
}

// resumeOrCreate issues the HTTP request with optional Range header and
// opens the partial file in the correct mode based on the response status.
// The caller MUST Close(handle.file) and Close(handle.body).
func resumeOrCreate(
    ctx context.Context,
    client *http.Client,
    userAgent string,
    rec *DownloadRecord,
    partialPath string,
    existing int64,
) (transferHandle, error)

// streamWithProgress copies handle.body into handle.file with checkpoint
// writes every checkpointBytes or checkpointInterval, whichever comes first.
func streamWithProgress(
    ctx context.Context,
    stateDir string,
    rec *DownloadRecord,
    handle transferHandle,
    existing int64,
    checkpointBytes int64,
    checkpointInterval time.Duration,
) error
```

### Acceptance Criteria
- [ ] `runDownload` body is no longer than 30 lines.
- [ ] Resume on a partial download still succeeds end-to-end against a server that returns 206.
- [ ] A download against a server that ignores `Range` and returns 200 still completes correctly (full body, partial file overwritten).
- [ ] `go test ./internal/service/downloadmgr/...` passes.
- [ ] No new exported identifiers in `internal/service/downloadmgr/worker.go`.

### Dependencies
- None.

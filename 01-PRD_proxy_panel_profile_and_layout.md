# PRD — Proxy Panel Profile Display and Layout

**Source**: prompt-aba-server-redesign-e-profile-fix.md
**Generated**: 2026-05-18

## Implementation Order

1. **F1** — Fix `LoadedProfileID` propagation so `ProxyPanel` reflects the active profile whenever one is loaded.
2. **F2** — Redesign `ProxyPanel.View()` into a vertical, hierarchical 2–4 line layout safe in 80-column terminals.

---

## F1: Profile Field Propagation

### Scope

**In scope**:
- Investigate and fix the gap in the data path so `httpproxy.Status.LoadedProfileID` is non-empty whenever the proxy is RUNNING and a backend is serving a profile.
- Add regression coverage at the layer where the gap lives (handler, supervisor, or both).
- Ensure `ProxyPanel.View()` keeps rendering `—` **only** when `LoadedProfileID` is genuinely empty (no profile ever loaded by this proxy instance), never as a silent fallback for transport/serialization errors.

**Out of scope**:
- Changes to proxy routing, swap semantics, or `/v1/*` endpoints.
- New endpoints or new fields on `Status` beyond what the chosen fix requires.

### Technical Approach

1. **Reproduce in a test** before any fix:
   - Stand up a real `httpproxy.Server` with a fake `ProcessMgr` so a swap can complete.
   - Drive one OpenAI-shaped request through it to populate `s.current`.
   - Call `Server.Status()` in-process and assert `LoadedProfileID == "<profile>"`. This confirms the in-process source of truth.
   - Then probe `/_status` over HTTP (httptest or the same in-process server) and assert the JSON contains the field with the same value. This isolates the HTTP boundary.
   - Then decode that JSON into `httpproxy.Status` exactly the way `proxysupervisor.Supervisor.Status()` does. This isolates the consumer.

2. **Apply the fix at the layer the test pinpoints**. Possible roots and their fixes:

   - **`handleStatus` does not emit the field** (omits it, uses a different struct, or marshals a stale snapshot): change the handler to marshal `s.Status()` directly.
   - **JSON field-name mismatch** between producer and consumer: add explicit `json:"..."` tags on `httpproxy.Status` so both sides agree on the wire format (snake_case recommended). Locks the contract against future drift.
   - **`s.current` is `nil` because no request has triggered `ensureLoaded` yet** (proxy started with a default profile from config but no client has hit `/v1/*`): pre-populate `s.current` in `Server.Start()` when a default profile is configured, **or** add an explicit `defaultProfileID` field on `Status` and have `ProxyPanel` fall back to it. Pick whichever matches how the proxy is started today.
   - **500 ms HTTP timeout in `Supervisor.Status()` drops the body** under load, leaving `LoadedProfileID` at zero-value: persist the last successful `LoadedProfileID` to `proxysupervisor.State` so the supervisor can fall back to the last-known value when the live probe fails. Document the fallback in a one-line comment.

3. **Render only on real emptiness**. After the fix, `ProxyPanel.View()` should still render `—` when `LoadedProfileID == ""`, but that path must only be reached when the proxy genuinely has no profile loaded — not as a side effect of a dropped HTTP probe or a missing JSON field.

### Touchpoints

- `internal/service/httpproxy/server.go` — `Status` struct (tags), `Server.Status()` (snapshot construction), possibly `Server.Start()` if pre-populating `s.current`.
- `internal/service/httpproxy/handler.go` — `handleStatus` JSON handler.
- `internal/service/proxysupervisor/supervisor.go` — `Supervisor.Status()` HTTP probe + decode path.
- `internal/service/proxysupervisor/state.go` — `State` struct, only if the persisted-fallback path is chosen.
- `internal/ui/components/proxy_panel.go` — only if rendering logic needs adjustment.

### Contracts

```go
// httpproxy.Status — apply json tags only if the chosen fix touches
// serialization. Tag names below are snake_case; align producer and
// consumer to whichever convention is adopted.
type Status struct {
    Running          bool          `json:"running"`
    Addr             string        `json:"addr"`
    LoadedProfileID  string        `json:"loaded_profile_id"`
    LoadedPID        int           `json:"loaded_pid,omitempty"`
    LoadedPort       int           `json:"loaded_port,omitempty"`
    LastSwapAt       time.Time     `json:"last_swap_at,omitempty"`
    LastSwapDur      time.Duration `json:"last_swap_dur,omitempty"`
    LastError        string        `json:"last_error,omitempty"`
    LastErrorAt      time.Time     `json:"last_error_at,omitempty"`
    InflightRequests int           `json:"inflight_requests"`
}
```

```go
// proxysupervisor.State — only extend if the persisted-fallback path is
// chosen. The added field carries the last-known profile so the supervisor
// can surface it when the live /_status probe fails.
type State struct {
    PID                 int       `json:"pid"`
    Host                string    `json:"host"`
    Port                int       `json:"port"`
    StartedAt           time.Time `json:"started_at"`
    LastLoadedProfileID string    `json:"last_loaded_profile_id,omitempty"` // optional
}
```

### Acceptance Criteria

- [ ] After driving one swap through `httpproxy.Server` in a test, `Server.Status().LoadedProfileID` returns the swapped-to profile ID.
- [ ] The JSON body of `GET /_status` contains the loaded profile ID under the field name agreed in the fix, and `Supervisor.Status()` decodes it into `LoadedProfileID` correctly.
- [ ] When the proxy is started with a default profile and no client has issued a request yet, the panel displays that default profile ID — **only if** the chosen fix path supports this case (pre-populate `s.current` or `defaultProfileID` fallback). If the fix path is JSON-tags-only, this criterion is N/A and the prompt's behavior is met as soon as the first request lands.
- [ ] With the proxy STOPPED, the panel renders `—` for profile (unchanged behavior).
- [ ] The chosen root cause is documented in a one-line comment at the patch site naming the layer that was wrong.
- [ ] A regression test exists at the layer where the bug lived (handler test or supervisor test); the test would have failed before the fix.
- [ ] `make tests` passes.

### Dependencies

- None.

---

## F2: Proxy Panel Vertical Layout

### Scope

**In scope**:
- Rewrite `ProxyPanel.View()` to render a 2–4 line vertical, hierarchical layout that fits 80-column terminals.
- Use `theme` styles + `lipgloss` primitives already present in the project to give status the strongest visual weight, profile ID a bold accent, and supporting metadata a subtle tone.
- Truncate any line that would exceed `p.width` with an ellipsis.
- Update rendered-output assertions in `proxy_panel_test.go` and `server_test.go`; add height/width invariants to the panel tests.

**Out of scope**:
- The sub-view list (Logs / Slots / Metrics / History) and its shortcuts.
- The instances-table column composition (only column **widths** may be tuned, and only if 80-col overflow is observed).
- Other tabs (Profiles, Models, Backends, Messages).
- Introducing any UI dependency beyond `lipgloss`, `theme`, and `bubbles`.

### Technical Approach

1. **Rewrite `View()`** in `internal/ui/components/proxy_panel.go` to build the panel as a slice of lines composed via `lipgloss.JoinHorizontal` for key/value pairs and `lipgloss.JoinVertical` for the overall block. Each line is emitted only when meaningful; never reserve vertical space for absent data.

2. **Line-by-line layout** (every line bounded by `p.width`; fall back to 80 when `p.width == 0`):

   - **Line 1 — status header (always)**:
     ```
     ● RUNNING   http://127.0.0.1:8080
     ```
     `●` rendered with `theme.OK` when `status.Running`, otherwise `○ STOPPED` with `theme.Error`. Address in default text style, with subtle `—` when empty.

   - **Line 2 — profile / inflight (only when RUNNING)**:
     ```
     profile=qwen-7b   inflight=3
     ```
     The `profile=` label uses `theme.Subtitle`. The profile ID uses `lipgloss.NewStyle().Bold(true)`. When `InflightRequests == 0`, the `inflight=` chunk is omitted entirely. Separator between chunks: three spaces (or `  ·  ` if the team prefers a visible mid-dot).

   - **Line 3 — last swap (only when `LastSwapAt` is non-zero)**:
     ```
     swap 12s ago (487ms)
     ```
     Rendered with `theme.Subtitle` to keep it visually lighter than the profile line.

   - **Line 4 — last error (only when `LastError != ""`)**:
     ```
     ⚠ backend health timeout on profile qwen-7b
     ```
     Rendered with `theme.Error`; truncate with `…` when length exceeds `p.width - 2`.

   - **Line 5 — pending / flash (preserve existing behavior)**: `Starting…` / `Stopping…` in `theme.Subtitle`; otherwise `p.flash.View()` when non-empty.

3. **Width safety**:
   - Read `p.width` (already wired via `SetWidth`); treat `0` as `80`.
   - Apply `lipgloss.NewStyle().MaxWidth(p.width).Render(line)` to every emitted line, **or** measure with `lipgloss.Width` and truncate with `runewidth.Truncate` (already a transitive dep through `lipgloss`).

4. **Tests** in `internal/ui/components/proxy_panel_test.go`:
   - Update existing rendered-string assertions to the new format.
   - Add a height invariant: across `{STOPPED, RUNNING no extras, RUNNING with swap+inflight, RUNNING with swap+inflight+error}`, the number of non-empty lines in `View()` (excluding the optional pending/flash line) is `≤ 4`.
   - Add a width invariant: with `SetWidth(80)`, no rendered line measured by `lipgloss.Width` exceeds 80.

5. **Page-level test fallout** in `internal/ui/pages/server_test.go`: any assertion that matches the old panel substrings (`"HTTP Proxy:"`, `"profile="` glued to status, etc.) is updated. Behavior tests (shortcuts, sub-view transitions, kill/restart confirms, input capture) remain untouched.

6. **Instances-table width check** in `internal/ui/pages/server.go`:
   - Verify the column widths (`PID 8 + Port 6 + Profile 18 + Uptime 10 + VRAM 12 + Tokens/s 10 = 64`, plus paddings) fit in 80 columns by adding a render test at width 80 that asserts no row exceeds 80.
   - If overflow is observed, reduce the `Profile` column from `18` to `16` and rely on `renderRows` truncation; do not change column composition.

### Touchpoints

- `internal/ui/components/proxy_panel.go` — full rewrite of `View()`; `Init`, `Update`, `SetWidth`, `Hints`, and tick logic remain unchanged.
- `internal/ui/components/proxy_panel_test.go` — update rendered-string assertions; add height + width invariants.
- `internal/ui/pages/server_test.go` — update assertions that match the old panel substrings; behavior tests untouched.
- `internal/ui/pages/server.go` — only if the 80-column verification forces a `Profile` column-width tweak.

### Contracts

```go
// ProxyPanel.View renders the proxy status block as 2-5 lines, each
// fitting within p.width columns. Lines are emitted only when the
// corresponding state is meaningful; the panel never reserves vertical
// space for absent data.
func (p *ProxyPanel) View() string

// SetWidth resizes the panel for proper truncation and alignment.
// A width of 0 is interpreted as 80 columns.
func (p *ProxyPanel) SetWidth(w int)
```

No new exported types or functions. `Status`, `HTTPProxyController`, `ProxyTickMsg`, `ProxyActionResultMsg` are unchanged by F2.

### Acceptance Criteria

- [ ] When STOPPED, `View()` renders exactly one non-empty line beginning with `○ STOPPED` (plus the optional pending/flash line, which keeps current behavior).
- [ ] When RUNNING with no swap, no inflight, and no error, `View()` renders the header line + the profile line; total non-empty lines `≤ 2` (plus optional pending/flash).
- [ ] When RUNNING with swap, inflight > 0, and last error set, `View()` renders header + profile + swap + error; total non-empty lines `≤ 4` (plus optional pending/flash).
- [ ] For every state combination above, with `SetWidth(80)`, no rendered line measured by `lipgloss.Width` exceeds 80 columns.
- [ ] The profile ID, when present, is rendered with `Bold(true)`; the `profile=` label uses `theme.Subtitle`.
- [ ] Behavior tests (Start/Stop dispatch, tick handling, flash transitions, input capture) in `proxy_panel_test.go` pass without modification.
- [ ] Shortcut tests (`v`, `Space`, `k`, `r`, `H`, `s`, `x`, `1`–`4`, `esc`) in `server_test.go` pass without modification.
- [ ] At width 80, the instances table renders no row longer than 80 columns (verified by a new render test).
- [ ] `make tests` passes.
- [ ] Manual check at 80×24: proxy panel + instances table + sub-view strip + sub-view body all visible, no horizontal overflow, no overlap.

### Dependencies

- **F1** must land first so the redesigned panel is verified against correct data.

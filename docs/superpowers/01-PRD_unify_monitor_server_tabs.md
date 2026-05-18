# PRD — Unify Monitor and Server Tabs

**Source**: prompt-unify-monitor-server-tabs.md
**Generated**: 2026-05-17

## Implementation Order
1. F1 — Merge `Monitor` and `Server` tabs into a single `Server` tab at slot 3; reduce `tabCount` from 6 to 5; extract proxy controls as a reusable component; resolve the `r` key conflict; rename internal symbols and cross-tab messages for semantic coherence; reconcile tests and help markdown.

---

## F1: Unified Server Tab

### Scope

**In scope**:
- Reduce top-level tab count from 6 to 5.
- Repurpose the slot at position 3 (`TabMonitor`) into a single `Server` tab. Remove the slot at position 6 (current `TabServer`).
- Render the HTTP proxy status + Start/Stop controls as a compact horizontal panel (1–3 lines) at the top of the unified tab body, above the instance observation area.
- Preserve the entire instance observation surface: instances table, sub-view tabs (Logs / Slots / Metrics / History), kill confirm, restart confirm, history chart with windows 1h/6h/24h/7d, pause/resume, periodic refresh tick.
- Resolve the `r` keybinding conflict by dropping the (redundant) Server "refresh" binding. Status is already polled every 1 s by `serverTickMsg`.
- Rename internal symbols for coherence:
  - `TabMonitor` → `TabServer`; remove the old `TabServer` constant.
  - `MonitorPage` struct → `ServerPage` struct; file `internal/ui/pages/monitor.go` → `internal/ui/pages/server.go` (old `server.go` content is moved out — see below).
  - Cross-tab messages: `SwitchToMonitorMsg` → `SwitchToServerMsg`; `MonitorSelectPIDMsg` → `ServerSelectPIDMsg`.
- Extract the existing `ServerPage`'s proxy-control UI and state into a reusable component `internal/ui/components/proxy_panel.go`. The unified `ServerPage` embeds this component.
- Update help markdown: merge `## Monitor tab` and `## Server tab` sections in `internal/ui/components/help.go` into a single `## Server tab` section covering the union of keys.
- Update bootstrap (`cmd/model-loader/main.go`):
  - Construct one unified `ServerPage` wired with both the `monitor.Manager`, `processmgr.Manager`, profile store, backend resolver, metrics dir, **and** the `HTTPProxyController` (the existing `proxysupervisor.Supervisor`).
  - Replace the pair of `.WithMonitorPage(...)` / `.WithServerPage(...)` calls on `RootModel` with a single `.WithServerPage(...)`.
  - Update `parseTab` in `bootstrap.go` so the string `"server"` resolves to the (new) `TabServer`; the string `"monitor"` is removed.
- Update test surface:
  - Tests in `internal/ui/pages/monitor_test.go` migrate to `internal/ui/pages/server_test.go` (the new one), constructing the unified `ServerPage` with both wiring slots; references to `MonitorPage` are renamed.
  - The existing `server_test.go` content covering proxy controls moves alongside the new `proxy_panel.go` as `internal/ui/components/proxy_panel_test.go` (and any portions that depend on full-page semantics — pending state, flash on action result — are kept in `server_test.go` as integration cases on the unified page).
  - `internal/ui/root_test.go`: rename references `TabMonitor` → `TabServer`, drop tests for the removed sixth slot, update `TestRoot_RoutesSwitchToMonitorMsg` and `TestRoot_ForwardsSwitchPIDToMonitor` to use `SwitchToServerMsg` + `ServerSelectPIDMsg`, and ensure no test asserts a tab count of 6.

**Out of scope**:
- Any changes to `internal/service/httpproxy`, `internal/service/proxysupervisor`, or `internal/service/processmgr`. Their public APIs and behavior are untouched.
- Any new features in the unified tab beyond what Monitor and Server already provide today.
- Visual changes to other tabs (Launcher, Profiles, Models, Backends) beyond the renumbering forced by the reduced tab count.
- Persisting the previously-active sub-view across TUI restarts (none of the originals did this; do not introduce it).
- Migrating the user's `initial_tab` config value (parseTab simply ceases to accept `"monitor"`; if the config holds that string the existing default-fallback path returns `TabLauncher`).

### Technical Approach

**Step 1 — Extract proxy panel component**

Create `internal/ui/components/proxy_panel.go` with the following public surface:

```go
package components

import (
    "context"
    "time"

    tea "github.com/charmbracelet/bubbletea"

    "github.com/quantmind-br/model-loader/internal/service/httpproxy"
)

// HTTPProxyController is the subset of *proxysupervisor.Supervisor consumed
// by ProxyPanel. Factored as an interface so tests can swap in a fake.
type HTTPProxyController interface {
    Start(context.Context) error
    Stop(context.Context) error
    Status() httpproxy.Status
}

// ProxyPanel is a self-contained widget that renders the HTTP proxy status
// line(s) and owns the Start/Stop action state. It does not draw a frame —
// the owning page composes it above whatever content follows.
type ProxyPanel struct {
    srv     HTTPProxyController
    status  httpproxy.Status
    pending pendingAction // none / start / stop
    flash   Flash         // "proxy" namespace
    width   int
}

func NewProxyPanel(srv HTTPProxyController) *ProxyPanel

// Init schedules the 1-second status tick. Owning page batches with its own.
func (p *ProxyPanel) Init() tea.Cmd

// Update handles ProxyTickMsg, ProxyActionResultMsg, and the `s` / `x` keys.
// It returns (modified panel, cmd, consumed). consumed=true means the key
// was claimed by the panel; the owning page must NOT also act on it.
func (p *ProxyPanel) Update(msg tea.Msg) (cmd tea.Cmd, consumed bool)

// View renders the compact panel (1–3 lines). Returns "" when srv is nil.
func (p *ProxyPanel) View() string

// SetWidth resizes the panel for proper truncation/alignment.
func (p *ProxyPanel) SetWidth(w int)

// Hints returns the proxy-only key hints (e.g. "[s] start  [x] stop")
// for inclusion in the HintProvider output of the owning page.
func (p *ProxyPanel) Hints() string

// Status is exposed for the owning page to read (e.g. for tests).
func (p *ProxyPanel) Status() httpproxy.Status
```

The view is compact:
- Line 1 (always): `HTTP Proxy: ● RUNNING  http://host:port   profile=<id>` (or `○ STOPPED  —`).
- Line 2 (when meaningful): `Last swap: 2s ago (140ms)   Inflight: 1   ⚠ Last error: …`.
- Line 3 (only during pending): `Starting…` / `Stopping…`, or the flash banner when present.

Internal types `ProxyTickMsg`, `ProxyActionResultMsg`, `pendingAction` are package-local to `components`. The 1-second tick command is owned by the panel.

The `mgr.List()` fallback that the legacy `ServerPage` used to surface "first running instance when no profile is routed" is **removed** — the instance table in the unified page already shows the canonical list right below.

**Step 2 — Rename and merge into the unified ServerPage**

- `git mv internal/ui/pages/monitor.go internal/ui/pages/server.go` after deleting the old `internal/ui/pages/server.go`.
- In the renamed file:
  - Rename struct `MonitorPage` → `ServerPage`.
  - Rename `NewMonitorPage` → `NewServerPage`.
  - Add a field `proxy *components.ProxyPanel` and a builder method `WithProxy(srv components.HTTPProxyController) *ServerPage` that constructs the panel.
  - `Init()` batches the existing monitor cmds with `p.proxy.Init()` when configured.
  - `Update(msg)`:
    - First, delegate to `p.proxy.Update(msg)`. If `consumed=true`, return early with the returned cmd (and re-merge any flash refresh into the page state).
    - Otherwise, proceed with the existing Monitor dispatch (key handler, instance refresh, sub-view, etc.).
  - `View()` composes:
    ```
    <proxy panel rendered>
    <blank line>
    <existing Monitor body: table + sub-view tabs + sub-view content>
    ```
    When `p.proxy == nil` the panel section degrades to an empty string and the page renders exactly like today's Monitor.
  - `Hints()` concatenates `p.proxy.Hints()` (when configured) with the existing Monitor hints, separated by `"  "`.
  - `HelpContext()` is not currently implemented by either page; keep behavior unchanged (root falls back to `Hints()`).

- Rename setter methods on `RootModel`:
  - `WithMonitorPage(p tea.Model) RootModel` → removed.
  - `WithServerPage(p tea.Model) RootModel` → retained, now installs the unified page.

**Step 3 — Update RootModel**

In `internal/ui/root.go`:

```go
const (
    TabLauncher Tab = iota
    TabProfiles
    TabServer
    TabModels
    TabBackends
)

const tabCount = 5
```

- `Tab.Title()`: drop the `TabMonitor` case; `TabServer.Title()` returns `"Server"`.
- `NewRoot`: initialize a `[5]tea.Model` with placeholders in the new order.
- `globalHints`: change `"[1-6] tabs"` → `"[1-5] tabs"`.
- `handleKey`: drop the `"6"` case; the `"3"` case activates `TabServer`; `"4"` activates `TabModels`; `"5"` activates `TabBackends`.
- `handleSwitchToMonitor` → `handleSwitchToServer`; it activates `TabServer` and forwards `ServerSelectPIDMsg{PID: msg.PID}` to the new page.
- The `Update` switch arm for `pages.SwitchToMonitorMsg` becomes `pages.SwitchToServerMsg`.

**Step 4 — Update cross-tab messages**

In `internal/ui/pages/messages.go`:

```go
type SwitchToServerMsg struct {
    PID int
}

type ServerSelectPIDMsg struct {
    PID int
}
```

(The legacy `SwitchToMonitorMsg` / `MonitorSelectPIDMsg` symbols are deleted, not aliased — internal package, no external consumers.)

In `internal/ui/pages/launcher.go`, every emission of `SwitchToMonitorMsg{PID: ...}` becomes `SwitchToServerMsg{PID: ...}`.

In the renamed `ServerPage.Update`, the case `MonitorSelectPIDMsg` becomes `ServerSelectPIDMsg`.

**Step 5 — Update bootstrap**

In `cmd/model-loader/main.go`, replace:

```go
monitorPage := pages.NewMonitorPage(svc.mgr, mon, svc.store).
    /* ...existing builders... */
serverPage := pages.NewServerPage(supervisor).WithProcessMgr(svc.mgr)

root := ui.NewRoot(initialTab).
    /* ... */.
    WithMonitorPage(monitorPage).
    /* ... */.
    WithServerPage(serverPage)
```

with:

```go
serverPage := pages.NewServerPage(svc.mgr, mon, svc.store).
    /* ...existing monitor builders preserved (SetBackendResolver, WithMetricsDir, etc.)... */.
    WithProxy(supervisor)

root := ui.NewRoot(initialTab).
    /* ... */.
    WithServerPage(serverPage)
```

In `cmd/model-loader/bootstrap.go`, update `parseTab`:

```go
func parseTab(name string) ui.Tab {
    switch name {
    case "launcher":
        return ui.TabLauncher
    case "profiles":
        return ui.TabProfiles
    case "server":
        return ui.TabServer
    case "models":
        return ui.TabModels
    case "backends":
        return ui.TabBackends
    default:
        return ui.TabLauncher
    }
}
```

(The `"monitor"` case is removed. If a user's config still names `"monitor"`, the default branch falls through to `TabLauncher` — acceptable for an unannounced rename of an internal slot.)

**Step 6 — Update help markdown**

In `internal/ui/components/help.go`, replace the two sections with a single one:

```text
## Server tab

- `v` — cycle Logs / Slots / Metrics / History sub-views
- `Space` — pause/resume log scroll
- `k` — kill selected instance
- `r` — restart selected instance (Kill + Launch)
- `H` — open history chart for the selected instance
- `1` / `2` / `3` / `4` — history chart window: 1h / 6h / 24h / 7d (only while chart is open)
- `s` — start HTTP proxy listener
- `x` — stop HTTP proxy listener
```

Update `## Global` if it references "Monitor tab" or "Server tab" by name (status hint shows `[1-5]` not `[1-6]`).

**Step 7 — Reconcile tests**

- Move proxy-control unit tests (`TestServerPage_SKeyStartsProxy`, `TestServerPage_XKeyStopsProxy`, `TestServerPage_StartFailureShowsFlash`, `TestServerPage_StopFailureShowsFlash`, `TestServerPage_NilProxy`, `TestServerPage_PendingStartIgnoresDuplicate`, `TestServerPage_PendingClearedAfterResult`, `TestServerPage_ViewShowsNotConfigured`, `TestServerPage_ViewShowsStartHintWhenStopped`, `TestServerPage_TickUpdatesStatus`, `TestServerPage_WindowSize`) into `internal/ui/components/proxy_panel_test.go`. The `fakeHTTPProxy` test double moves too.
- Drop `TestServerPage_RKeyRefreshesStatus` (the `r` key no longer refreshes — feature removed). Replace with an explicit `TestServerPage_RKeyTriggersRestart` on the unified page asserting the kill+restart confirm flow.
- Rename the file `internal/ui/pages/monitor_test.go` → `internal/ui/pages/server_test.go` (replacing the old proxy-only one). Inside, rename every `NewMonitorPage` → `NewServerPage` and every `MonitorPage` → `ServerPage` reference. Tests that need the proxy panel attached call `.WithProxy(&fakeHTTPProxy{})` in setup.
- In `internal/ui/root_test.go`:
  - Replace every `WithMonitorPage(...)` with `WithServerPage(...)`. Remove the now-defunct `WithMonitorPage` call sites.
  - `TestRoot_RoutesSwitchToMonitorMsg` → `TestRoot_RoutesSwitchToServerMsg`; uses `pages.SwitchToServerMsg`.
  - `TestRoot_ForwardsSwitchPIDToMonitor` → `TestRoot_ForwardsSwitchPIDToServer`; the `recordingMonitor` test double becomes `recordingServer`, asserting on `ServerSelectPIDMsg`.
  - Any assertion against `[1-6]` in `globalHints` becomes `[1-5]`.
  - Drop any test asserting a sixth tab slot exists.

**Step 8 — Verify no orphaned references**

After the rename, the following greps must return zero hits across the repo (excluding `prompt-*.md`, this PRD, and `CLAUDE.md` historical references):

```
grep -rn 'TabMonitor\|MonitorPage\|NewMonitorPage\|WithMonitorPage\|SwitchToMonitorMsg\|MonitorSelectPIDMsg\|monitorPage' --include='*.go'
```

(If `CLAUDE.md` still references `monitor.go` or the 6-tab model, update it in this same PRD's implementation — knowledge-base coherence is part of the work.)

### Touchpoints

- `internal/ui/root.go` — `Tab` enum, `tabCount`, `Title()`, `NewRoot`, `globalHints`, `WithMonitorPage`/`WithServerPage` methods, `handleKey` numeric cases, `handleSwitchToMonitor` → `handleSwitchToServer`, Update switch arm for cross-tab msg.
- `internal/ui/root_test.go` — rename, key/number assertions, switch-msg tests, drop sixth-slot tests.
- `internal/ui/pages/monitor.go` — **renamed** to `server.go`; struct + constructor + setters renamed; embed `*components.ProxyPanel`; `Init`/`Update`/`View`/`Hints` extended to compose the panel.
- `internal/ui/pages/monitor_test.go` — **renamed** to `server_test.go` (replacing the legacy proxy-only file); all `MonitorPage` references renamed; tests that need proxy attach call `.WithProxy(...)`.
- `internal/ui/pages/server.go` (legacy) — **deleted** after content extraction (Step 1) and tests migrated (Step 7).
- `internal/ui/pages/server_test.go` (legacy) — **deleted**; proxy-control unit tests moved to `components/proxy_panel_test.go`; integration tests survive in the new `server_test.go`.
- `internal/ui/pages/messages.go` — replace `SwitchToMonitorMsg` with `SwitchToServerMsg` and `MonitorSelectPIDMsg` with `ServerSelectPIDMsg`.
- `internal/ui/pages/launcher.go` — emit `SwitchToServerMsg` instead of `SwitchToMonitorMsg`.
- `internal/ui/components/proxy_panel.go` — **new**: extracted proxy controls component + `HTTPProxyController` interface + `ProxyTickMsg` / `ProxyActionResultMsg`.
- `internal/ui/components/proxy_panel_test.go` — **new**: tests migrated from legacy `pages/server_test.go`, exercising the panel in isolation with the `fakeHTTPProxy` double.
- `internal/ui/components/help.go` — replace `## Monitor tab` + `## Server tab` sections with a single merged `## Server tab` section; update `## Global` hint text from `[1-6]` to `[1-5]`.
- `cmd/model-loader/main.go` — construct one unified `ServerPage` with both wiring slots; drop the `monitorPage` variable; single `WithServerPage` on `RootModel`.
- `cmd/model-loader/bootstrap.go` — `parseTab` drops `"monitor"`, retains `"server"`.
- `CLAUDE.md` — update the "TUI pages" row and "WHERE TO LOOK" table to reference the unified `server.go` (5-tab model); remove standalone `monitor.go` row.

### Contracts

```go
// internal/ui/root.go
const (
    TabLauncher Tab = iota
    TabProfiles
    TabServer
    TabModels
    TabBackends
)

const tabCount = 5

const globalHints = "[1-5] tabs  [tab] next  [q] quit" + components.HelpToken

func (m RootModel) WithServerPage(p tea.Model) RootModel
// WithMonitorPage is removed.

// internal/ui/pages/messages.go
type SwitchToServerMsg struct {
    PID int
}

type ServerSelectPIDMsg struct {
    PID int
}

// internal/ui/pages/server.go (renamed from monitor.go)
type ServerPage struct {
    // existing monitor fields preserved
    proxy *components.ProxyPanel
    // ...
}

func NewServerPage(pm procMgrIface, mm monitor.Manager, ps profileStoreIface) *ServerPage

func (p *ServerPage) WithProxy(srv components.HTTPProxyController) *ServerPage

// internal/ui/components/proxy_panel.go (new)
type HTTPProxyController interface {
    Start(context.Context) error
    Stop(context.Context) error
    Status() httpproxy.Status
}

type ProxyPanel struct { /* unexported fields */ }

func NewProxyPanel(srv HTTPProxyController) *ProxyPanel
func (p *ProxyPanel) Init() tea.Cmd
func (p *ProxyPanel) Update(msg tea.Msg) (cmd tea.Cmd, consumed bool)
func (p *ProxyPanel) View() string
func (p *ProxyPanel) SetWidth(w int)
func (p *ProxyPanel) Hints() string
func (p *ProxyPanel) Status() httpproxy.Status
```

### Acceptance Criteria

- [ ] On launch, `RootModel.View()` renders exactly 5 tabs in this order: `1 Launcher`, `2 Profiles`, `3 Server`, `4 Models`, `5 Backends`.
- [ ] Pressing `6` is a no-op (no tab switch, no quit).
- [ ] Pressing `3` activates the unified `Server` tab.
- [ ] On the `Server` tab, `s` starts the HTTP proxy via the wired `HTTPProxyController.Start`, and the panel shows `Starting…` until `ProxyActionResultMsg` arrives.
- [ ] On the `Server` tab, `x` stops the HTTP proxy via `HTTPProxyController.Stop`, and the panel shows `Stopping…` until completion.
- [ ] On the `Server` tab with at least one running instance selected, `r` opens the existing restart confirm (Monitor's behavior); confirming triggers Kill + Launch via `processmgr.Manager`.
- [ ] On the `Server` tab with at least one running instance selected, `k` opens the kill confirm; confirming kills the selected PID.
- [ ] On the `Server` tab, `v` cycles the sub-view through Logs → Slots → Metrics → History → Logs.
- [ ] On the `Server` tab, `H` opens the history chart for the selected PID; `1`/`2`/`3`/`4` switch its window; `esc` closes it.
- [ ] On the `Server` tab, `Space` toggles log-pause (Monitor's behavior preserved).
- [ ] `s` and `x` do not collide with any instance-observation binding; `r` triggers restart (not refresh — refresh is implicit via the 1 s tick).
- [ ] The proxy panel renders in 1–3 lines depending on which fields are set (`Last swap`, `Inflight`, `Last error`, `Pending` are conditional).
- [ ] When the proxy is stopped, the instance table and sub-views still render and remain fully operable (no dependency on proxy state).
- [ ] When the proxy `HTTPProxyController` is `nil` (test path), the proxy panel section is empty and the page renders exactly like the legacy Monitor.
- [ ] After a successful launch, `LauncherPage` emits `SwitchToServerMsg{PID: newPID}`; `RootModel` activates `TabServer` and forwards `ServerSelectPIDMsg{PID: newPID}`; the unified page selects the row for `newPID`.
- [ ] `internal/ui/components/help.go` shows a single `## Server tab` section listing the union of keys (`v`, `Space`, `k`, `r`, `H`, `1-4`, `s`, `x`) and no `## Monitor tab` section.
- [ ] `grep -rn 'TabMonitor\|MonitorPage\|NewMonitorPage\|WithMonitorPage\|SwitchToMonitorMsg\|MonitorSelectPIDMsg' --include='*.go'` returns zero hits across the repository.
- [ ] `cmd/model-loader/bootstrap.go::parseTab` returns `TabServer` for input `"server"` and `TabLauncher` for input `"monitor"` (former alias no longer recognized).
- [ ] `make tests` (full Go test suite, including golden tests) passes with no skipped tests in the affected packages.
- [ ] `make build` produces a working binary; launching it manually shows the 5-tab layout and the unified tab is operable end-to-end (start proxy, observe instances, kill one, restart one, stop proxy) without switching tabs.

### Dependencies

- None. Single self-contained refactor.

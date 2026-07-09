# TUI

`RootModel` (`internal/ui/root.go:84`) is a value-typed hub-and-spoke router over a fixed `[5]tea.Model`. Cross-tab navigation is message-based (`LaunchProfileMsg`, `SwitchToServerMsg`, `NavigateToSizingMsg`, `TabAttentionMsg`).

## Tabs at a glance

| # | Tab | Page entry | One-liner |
|---|-----|-----------|-----------|
| 1 | Profiles | `pages/profiles.go` | Master-detail. `Enter` loads; `e`/`n` open web editor; `d`/`X`/`K`/`p`/`I`/`u`/`E` for CRUD/pin/import/export/undo |
| 2 | Server | `pages/server.go` | ProxyPanel + instances table + Logs/Slots/Metrics/History sub-views cycled by `v` |
| 3 | Models | `pages/models.go` | Library / Downloads / Discover (HF Hub). `R` rescan, `s` HF search, `i` info |
| 4 | Backends | `pages/backends.go` | Catalog list. `n` new, `e`/`enter` edit, `X` delete, `D` default, `P` probe all |
| 5 | Benchmark | `pages/benchmark.go` | State machine: Dashboard → Wizard → Running → RunDetail (→ Problem drill-in), plus Compare/History; `W` opens the read-only web viewer |

## Global chrome

- **Tab bar** — scrollable, active tab bracketed, attention badges via `TabAttentionMsg` (download done, instance crash)
- **Status bar** — `[1-5] tabs  [tab] next  [q] quit  [?] help` + per-page `Hints()` + restart badge
- **Help overlay** — glamour-rendered, `?`/`esc` toggle, `j/k/g/G` scroll
- **Flash queue** — tagged auto-clearing notifications
- **Modal/overlay compositor** — centered, dismiss with `esc`

Theme = GitHub-Primer adaptive palette with `NO_COLOR` stripping. Layout auto-adapts: stacked below 100 cols, truncate-at-edge, "terminal too small" below 20×6.

## Keyboard conventions

- **lowercase = light / navigation** (`k/j/g/G` are reserved for lists)
- **UPPERCASE = destructive, always confirm** (`X` delete, `K` kill/unload, `R` refresh, `I` import, `E` export, `D` default, `P` probe, `C` clear)
- All printable-rune shortcuts are gated by `!activePageCapturesInput()` — only `ctrl+c` is unconditional
- Pages hosting `*huh.Form` forward non-`KeyMsg` messages so focus/validation completes
- Every page implements `Reload()` for on-focus refresh

## Tab 1 — Profiles

Master-detail (`bubbles/list`, ~1/3 ↔ 2/3, `│` divider). Pinned rows (📌) sort first then by `UpdatedAt`. Corrupt profiles render ⚠ in `theme.Error`.

| Key | Action |
|-----|--------|
| `Enter` | Validate → resolve → ensure proxy running → load via `/_admin/load` |
| `e` / `n` | Edit / New — open in-process web editor (browser auto-opens) |
| `d` | Duplicate |
| `X` | Delete (writes `.history` backup) |
| `K` | Unload current model (calls `/_admin/unload`) |
| `R` | Refresh from disk |
| `p` | Pin / unpin |
| `I` | Import bundle (filepicker + merge/overwrite/rename modal) |
| `u` | Undo last import (field-diff modal) |
| `E` | Export all to JSON bundle |
| `Ctrl+T` | Cycle Essentials / Advanced / Environment / Sizing (in web editor) |
| `/` | Filter profiles |

The TUI collapses to "Editing in browser…" frame while the web editor is open. Only `esc` (cancel) passes through.

## Tab 2 — Server

**Top:** `ProxyPanel` — 1 Hz status widget (`● RUNNING | addr`, loaded profile, last-swap line, `LastError`, pending/flash). Consumes `s`/`x` (proxy start/stop) before the page sees them.

**Middle:** `bubbles/table` of instances (PID / Port / Profile / Uptime / VRAM / Tok·s).

**Bottom:** sub-views **Logs | Slots | Metrics | History** cycled by `v`.

| Key | Action |
|-----|--------|
| `v` | Cycle Logs / Slots / Metrics / History |
| `Space` | Pause / resume log scroll (2000-line FIFO tail) |
| `K` | Kill selected — routes through `/_admin/unload` when proxy owns the PID, else `pm.Kill`. **Refuses on degraded proxy** and arms a "Force-stop proxy" confirmation; accepting it SIGKILLs the proxy then kills the backend directly (audit A13) |
| `R` | Restart (confirm) |
| `h` | History chart |
| `1`/`2`/`3`/`4` | History chart window: 1h / 6h / 24h / 7d |
| `s` / `x` | Start / stop HTTP proxy listener |

Server tab is a **passive observer** — calls `pm.RefreshFromDisk` (read-only), never `Reconcile`.

## Tab 3 — Models

Three sections cycled by `←/→`:

- **Library** — async-scanned table (Name / Size / Quant / Params / Path) with per-path scan status
- **Downloads** — EMA speed/ETA rows (`c` hide-done, `C` clear-done, `x` cancel, `r` resume)
- **Discover** — debounced HF Hub search, `ctrl+g` GGUF-only toggle, file picker, queued download

| Key | Action |
|-----|--------|
| `R` | Rescan all configured paths |
| `/` | Filter models |
| `Enter` | Action menu: use in new / existing profile, copy path, delete |
| `s` | Search Hugging Face |
| `i` | Show model info panel (file/path/size/mtime/metadata/used-by) |
| `→` / `g` | Navigate to sizing for this model |
| `X` | Remove a broken search path |

## Tab 4 — Backends

Master-detail (Name + ID) + detail panel (ID / Kind / Executable / SchemaRef / Tags / Created / Updated / Probe).

| Key | Action |
|-----|--------|
| `n` | New backend |
| `e` / `Enter` | Edit (web editor or legacy `huh` form) |
| `X` | Delete |
| `D` | Set default |
| `R` | Refresh schema (confirm — **wipes manual edits unless `Source.Editable`**) |
| `P` | Probe all (3 s/backend) |
| `/` | Filter |

## Tab 5 — Benchmark

State machine in `benchmark.go::benchView`: **Dashboard** (landing) → **Wizard** → **Running** → **RunDetail**, with a **Problem** drill-in off RunDetail and **Compare** / **History** as side views. Every view budgets `p.width`×`p.height` via `benchmark_layout.go` (`fitColumns`/`visibleWindow`/`renderCells`) and the shared `theme.*RuneWidth` helpers — no view overflows or soft-wraps (regression-guarded by a width/height property test).

- **Dashboard** (`benchmark_dashboard.go`) — mode-focused leaderboard. Summary strip, mode-focus bar, windowed ranked rows (latest run per profile, `MetricBar` + Δ; newest-run-partial rows carry a `!` badge), insight panel pinned to the bottom (never clipped)
- **Wizard** (`benchmark_wizard.go`) — unified 3-step launcher: profile (filterable, windowed) → mode (cards grouped by category) → review. `enter` advances, `esc` back-navs, `/` filters profiles. `IsCapturingInput` now captures in every non-dashboard view so `q`/`1-5`/`tab` no longer leak to root mid-wizard
- **Running** (`benchmark_run.go`) — rendered from the runner's authoritative `RunFeed.Snapshot()` on a 1s tick: header + elapsed, progress bar + `Index/Total` + ✓/✗/! tallies, now-line (current item + phase + item elapsed + latest stream heartbeat), a flexible activity log (item / harness / stream lines, newest at bottom), and a staleness footer (`benchmark.StalenessLabel`) with a watchdog countdown for tb/deep-swe. Collapses gracefully on short terminals. `esc` arms the cancel-confirm modal
- **RunDetail** — fixed header (meta + scorecards + summary + mode lines) over a windowed per-problem table (cursor `j/k/g/G`, `s` cycles sort dataset·score↑·score↓, `enter` drills in). Scorecards reflow 4-across / 2×2 / vertical by width; bars normalize against data-relative `benchmark.CeilingsFor` (max observed ×1.1, fixed defaults only without peer data)
- **Problem** drill-in — full-body scroll of every `ProblemResult` field (outcome, sub-scores, fail phase, tokens, speeds, sandbox, judged-by, err) plus a lazily-loaded transcript excerpt (`transcripts not saved for this run` when absent)
- **Compare** (`benchmark_compare.go`) — latest run per profile, grouped by mode, flattened into one cursor space; `enter`→detail, `m` cycles ranking metric
- **History** (`benchmark_compare.go::keyHistory`) — one profile's runs over time via `fitColumns` with a pinned sparkline

| Key | Action |
|-----|--------|
| `b` | Open wizard |
| `Enter` | Open run detail |
| `c` | Open compare view |
| `h` | Open history timeline |
| `X` | Delete run |
| `E` | Export run |
| `W` | Open read-only web viewer (saved runs + live monitor) |
| `R` | Reload |
| `m` | Cycle metric (compare / history) |
| `↑↓/k/j/g/G` | Cursor |
| `←/→/[/]` | Focus mode |
| `esc` | Back |
| `s` | Cycle per-problem sort (run detail) |
| `enter` | Drill into selected problem (run detail) |

**Primary metric** (`benchmark_metrics.go::primaryMetric`): every mode except `llama-bench` (tok/s) and `longctx` (recall/`AvgScore`) reports **solve rate** as headline metric. One derivation feeds dashboard ranking, scorecard, compare default, and history trend.

## Web profile/backend editor (`configweb`)

`Create` / `Edit` opens an **on-demand, in-process HTTP server** on `127.0.0.1:0`, launches the browser, and collapses the TUI to an "Editing in browser…" frame (only `esc` = cancel passes through).

- Form is **schema-driven** from `BackendValidationSchema.Flags` + editable `Presentation` (Essentials / Advanced / Environment / Sizing)
- **Customize mode** edits the schema itself and sets `Source.Editable = true` so `RefreshSchema` preserves manual edits across `--help` re-parsing
- Model-existence errors are downgraded to warnings (configure-now / download-later flow)
- Curated highlights live in `essentialSeed` (`backendschema/presentation.go`)

The schema regen workflow (`Pattern A`/`B`/`C`) is described in `.claude/commands/backend-schema-update.md` — A: live `--help` + overlay, B: curated Go, C: embedded rows.

The same `configweb` stack also serves a **read-only benchmark viewer + live monitor** (`benchview_*.go`): `BenchViewer` mirrors the session's ephemeral `127.0.0.1:0` + lingering-shutdown lifecycle and exposes `Runs`/`Compare`/`Live` pages. The Benchmark tab's `W` key launches it in-process (so `/live` observes TUI-launched runs via `runner.Feed()`); `model-loader benchmark web` launches it headlessly against saved runs (no live feed).

## Implementation notes for future agents

- `RootModel` is value-typed; helper methods return updated `RootModel` rather than mutating through pointers
- All `*tea.Msg` types live alongside the page that emits them (e.g. `pages/profiles_messages.go`)
- Pages own no goroutines; long-running work emits `tea.Cmd` and the model reads results from `tea.Msg`
- `Reload()` is called on tab focus so pages refresh from disk / process manager
- Status bar `Hints()` is per-page; the root composes it
- Help markdown is **drift-checked** by `help_test.go::TestHelpMarkdownCoversAllHints` — keep `?` markdown in sync with the per-page `Hints()` strings

### Responsive layout

The two-pane pages (Profiles, Backends) and the Models/Server/Benchmark tables now derive widths from `theme.ResponsiveSplit(width)` instead of the removed `theme.SplitTwoPanes` helper:

- **`LayoutStacked`** (width < `theme.NarrowWidthThreshold` = 100) — panes stack vertically with a `─` divider rule between them; both panes get the full row width minus gutter.
- **`LayoutSplit6040`** (100–160 cols) — master pane 60% / detail 40%.
- **`LayoutSplit5050`** (>160 cols) — balanced 50/50.

Below `theme.MinTermWidth` (20 cols) or `theme.MinTermHeight` (6 rows) `RootModel.View()` short-circuits with a centered `Terminal too small` notice instead of trying to lay out the chrome.

`theme.ClampBody` now hard-truncates wide lines at the right edge (no soft wrap) so a single over-long row can't push the status bar off-screen at narrow widths. Width-sensitive rows (benchmark compare/history/dashboard, server metrics, model info panel) re-derive their column widths from `p.width` via `min`/`max`/`truncate` rather than hardcoded constants.

The picker overlay (`components/picker.go`) floor dropped from 60→24 columns so it still fits at narrow widths.

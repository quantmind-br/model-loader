# F2 — Code Quality Review

**Plan**: `models-hf-search-download`
**Date**: 2026-05-17
**Reviewer**: Sisyphus-Junior (F2)
**Scope**: All commits since base `30d00b9` (63 changed files; 15 feature commits).

---

## Verdict: **REJECT**

Build / vet / race-tests all pass cleanly and the service-layer packages
(`hfhub`, `downloadmgr`) ship with healthy coverage. Rejection is driven by a
single, hard-blocking gap: **0% test coverage on every new UI component file**
(`download_progress.go`, `hf_search_picker.go`, `hf_file_picker.go`, ~365 LoC)
and on the new `models.go` handlers that drive them. The plan explicitly
called for "tests-after nos overlays UI (teatest)" — that deliverable is
absent. Everything else is clean and recoverable.

---

## Metrics

### Required gates

| Check                        | Result            |
| ---------------------------- | ----------------- |
| `go build ./...`             | PASS (exit 0)     |
| `go vet ./...`               | PASS (exit 0)     |
| `go test ./... -race`        | PASS (all green)  |
| `TODO/FIXME/HACK/XXX` in new files | NONE         |
| `fmt.Println` / `log.Println` | NONE             |
| Empty `catch` / silently-ignored errors | NONE (only deliberate `_ = file.Close()` cleanup) |

### Coverage of new packages

| Package                             | Coverage |
| ----------------------------------- | -------- |
| `internal/service/hfhub`            | **76.6%** |
| `internal/service/downloadmgr`      | **87.4%** |
| `internal/ui/components` (entire pkg) | 49.6% |
| `internal/ui/components` — new files only | **0.0%** |

### Per-function highlights (new code with 0% coverage)

`internal/ui/components/download_progress.go` — every function 0%
`internal/ui/components/hf_search_picker.go` — every function 0%
`internal/ui/components/hf_file_picker.go`   — every function 0%
`internal/ui/pages/models.go`:
- `handleDownloadEvent`     0%
- `openHFFilePicker`        0%
- `startSelectedDownloads`  0%
- `WithHFClient`            0%
- `WithDownloadManager`     0%
- `downloadStatusLabel`     0%
- `downloadErrString`       0%
- `componentMsg` adapters   0%

Service-side coverage is solid:
- `Manager.Start` 85.7%, `Cancel` 91.7%, `Snapshot` 100%, `Close` 78.9%,
  `runDownload` 100%, `setTerminal/finish/broadcast` 100%, `startNextLocked` 88.9%
- `Client.Search` 90.5%, `RepoInfo` 83.3%, `OpenDownload` 81.2%, `parseRetryAfter` 69.2%
- `ResolveDest` 95.5%, `withinRoot` 80%

---

## Findings

### Blocking

#### B1 — UI overlays ship with 0% test coverage
**Files**: `internal/ui/components/{download_progress,hf_search_picker,hf_file_picker}.go`
**Impact**: ~365 LoC of new TUI surface (search debounce, GGUF filter, file
multi-select, snapshot mode, progress rendering, focus/cursor, cancel
keybinding) is completely untested. Plan TL;DR explicitly listed
"tests-after nos overlays UI (teatest)" as a deliverable.

#### B2 — New `models.go` handlers untested
**File**: `internal/ui/pages/models.go`
**Impact**: The feature's wiring layer — `handleDownloadEvent`,
`openHFFilePicker`, `startSelectedDownloads`, the builder injectors
`WithHFClient`/`WithDownloadManager`, and the status/error-string helpers —
has zero coverage. `models_test.go` contains no test referencing
`hfClient`, `dlManager`, `HFSearch*`, or `Download*`. Any regression in
these paths will surface only at runtime.

### Non-blocking (recommend follow-up)

#### N1 — Missing godoc on a handful of exported names
- `hfhub.DefaultBaseURL` (`internal/service/hfhub/client.go:16`)
- `downloadmgr.ErrManagerClosed` (`internal/service/downloadmgr/manager.go:15`)
- `downloadmgr.Status` constants individually (`types.go:30-36`) — only the
  type has a godoc; `StatusQueued`/`StatusActive`/… have none.
- `components.HFFileListMsg` (`hf_file_picker.go:36`)
- `components.DownloadCancelMsg` (`download_progress.go:25`)

All other exported types, functions, and methods in the new packages are
properly documented.

#### N2 — Pass-through errors in `Manager.download`
`internal/service/downloadmgr/manager.go:184,188,199,203` return raw
`err` without `fmt.Errorf("...: %w", err)` wrapping. `errors.Is(err,
context.Canceled)` still works (Go stdlib wraps it inside `*url.Error`),
so `statusForError` is correct — but diagnostic context is lost in logs.
Recommend wrapping with stage names ("download: build request", "download:
http do", "download: open partial", "download: rename") for traceability.

#### N3 — Sentinel-less non-2xx error on download
`manager.go:193` returns `fmt.Errorf("download failed with status %d",
resp.StatusCode)` — a plain string. Consider mirroring `hfhub.ErrHTTP`
(structured type, supports `errors.As`) so callers can branch on status.

#### N4 — Duplicate byte-humanizer
`components.humanBytes` (`download_progress.go:139`, format `1.23MB`) and
`components.humanizeBytes` (`hf_file_picker.go:207`, format `1.2 MB`)
implement the same algorithm with cosmetic differences. Consolidate into
one helper to avoid future drift.

#### N5 — `time.RFC3339Nano`-based `NewID` is fragile under bursts
`downloadmgr.NewID()` uses the wall clock as a string ID. Two `Start`
calls within the same nanosecond — unlikely in production, easy in tests —
collide and corrupt `states` / `queue`. Prefer a monotonic counter or
`uuid`/`crypto/rand` for the ID.

---

## Evidence

- Build: `go build ./...` — exit 0, no output.
- Vet: `go vet ./...` — exit 0, no output.
- Race tests: `go test ./... -race -timeout 120s` — all 27 packages green;
  `hfhub` 2.0s, `downloadmgr` 1.07s, `components` 1.13s.
- Coverage commands run:
  - `go test -cover ./internal/service/hfhub/... ./internal/service/downloadmgr/... ./internal/ui/components/...`
  - `go test -coverprofile=/tmp/cov-components.out ./internal/ui/components` + `go tool cover -func`
  - `go test -coverpkg=./internal/ui/components -coverprofile=/tmp/cov-pages.out ./internal/ui/pages`
- Code-smell scan: `grep -nE "(TODO|FIXME|HACK|XXX|fmt\\.Println|log\\.Println)"` over the entire diff — zero hits.

---

## Recommendation to Orchestrator

Dispatch a remediation task that adds at minimum:

1. **Unit tests** for `HFSearchPicker.handleKey` covering: query input,
   backspace, `g` toggle, `up`/`down`, `enter` → `Selected()`, `esc`,
   stale-epoch result drop.
2. **Unit tests** for `HFFilePicker.Update` covering: snapshot vs
   individual mode, `.gguf` filter, space toggle, `SelectedFiles()`
   semantics, error path.
3. **Unit tests** for `DownloadProgress` covering: visibility toggle (`d`),
   cursor up/down with empty snapshot, `x` emits `DownloadCancelMsg` only
   for active downloads, `formatProgress` zero-total branch.
4. **Page-level tests** in `models_test.go` for `handleDownloadEvent`
   (completed → rescan + flash, failed → flash), `openHFFilePicker`
   (no HF client → flash), and `startSelectedDownloads` (no paths,
   `ErrAlreadyExists`, success path with fake `dlManager`).

The non-blocking items (N1-N5) can be folded into the same patch or
deferred — they do not affect correctness.

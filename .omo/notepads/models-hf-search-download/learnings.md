
## T3 — Download path resolver

- Signature confirmed by task spec: `ResolveDest(searchPath, repoID, rfilename string, isSnapshot bool) (destDir, destFile string, err error)`. Note: plan (line 438) used `searchPaths []string`, task spec narrowed it to single `searchPath string`. Task spec wins; T8 caller will pass `cfg.Models.SearchPaths[0]`.
- Path traversal check uses `cleanRoot + filepath.Separator` prefix to avoid false-positives like `/tmp/a` vs `/tmp/abc`.
- For individual mode, `filepath.Base(rfilename)` already neutralizes `../../../etc/passwd` (becomes `passwd`); traversal check is still kept as defense-in-depth.
- TDD: tests written first, failed with `undefined: ResolveDest` (build error), then implementation made them pass. 9/9 subtests green.
- Existing godoc style on exported symbols matches `manager.go`/`types.go` precedent in this package.

## T7 — hfhub client implementation

- DTO-vs-domain split: `types.go` (created in T1) has no JSON tags, so kept it untouched and introduced unexported `searchResultDTO` / `repoInfoDTO` / `siblingDTO` with `json:` tags in `client.go`. Cleaner than retrofitting tags onto the public surface.
- HF Hub returns `lastModified` as RFC3339Nano (`2024-04-22T12:34:56.000Z`) — RFC3339 parses it cleanly, but explicit RFC3339Nano-then-RFC3339 fallback handles both shapes.
- 429 retry: spec said "sleep up to 30s, retry once". Implemented as `parseRetryAfter` → cap at `retryAfterCap=30s` → `select { ctx.Done() | time.After(wait) }` so cancellation during sleep works. Single retry only, then bubble whatever the server returns.
- `OpenDownload` builds the path with literal `/` for both `repoID` and `filename` so subdir paths inside the repo (e.g. `tokenizer/tokenizer.json`) survive. `url.Parse` does NOT percent-encode existing `/` in `.Path`, which is what we want here.
- `httptest.Server.Client()` returns a client preconfigured for the fake server. Used instead of `http.DefaultClient` to avoid surprises with TLS.
- All exported Go API symbols need godoc per `revive`/`golint`; hook flagged comments but spec-required and convention-mandated.
- Final: 10 tests pass (8 spec'd + `TestOpenDownload_SubdirFilename` + `TestParseRetryAfter` table). `go build ./...` clean. Evidence at `.sisyphus/evidence/task-07-tests.txt`.

## T12 — ModelsPage state additions

- **Plan vs task-spec conflict**: task prompt asked to change `NewModelsPage` signature to take 4 new args; plan (line 1117) explicitly said "Não alterar `NewModelsPage` signature" and use builders mirroring `WithProfileStore`. Plan won — changing signature would have broken ~30 call sites in `models_test.go` and `cmd/model-loader/main.go`. T13 wires deps via the new builders.
- **Interface mismatch trap**: `components.DownloadStateSnapshotter.Snapshot() []DownloadState` (components.DownloadState) is NOT satisfied by `*downloadmgr.Manager.Snapshot() []State` (downloadmgr.State). T13 will need an adapter when initialising `p.downloads` — task spec line "p.downloads = components.NewDownloadProgress(dlManager, width)" cannot compile verbatim. Flag for T13.
- **Message types in pages package**: `components.hfFileListMsg` and `components.downloadCancelMsg` are unexported; pages can't import them. So pages-side mirror types `hfFileListMsg`, `downloadCancelMsg` defined in `models.go`. T13/T15 will translate between component-emitted messages and these page-level ones.
- **Nil-safe IsCapturingInput**: builders may not run (tests using bare `NewModelsPage` won't invoke `WithHFClient`/`WithDownloadManager`). Every new check is guarded with `p.hfSearch != nil` etc. Existing test suite (39 tests) passes unchanged.
- **Subscribe placement**: `Manager.Subscribe()` called in `WithDownloadManager`, not in `NewModelsPage`. Decoupling so test code building a bare page doesn't need a manager.

## T8 downloadmgr implementation - 2026-05-17
- Manager now owns mutex-protected states, FIFO queue, active cancel funcs, and non-blocking subscribers.
- Downloads write to DestFile + .partial and rename only after successful copy; cancel/error removes partial.
- Queue drain starts next queued item when active download finishes or active cancel frees slot.

## F4 scope fidelity check - 2026-05-17
- Scope audit used `git diff --stat HEAD~16`, `git diff --name-only HEAD~16`, and `git diff HEAD~16 -- go.mod`.
- F4 verdict REJECT: touched forbidden paths including docs, AGENTS.md, internal/domain, processmgr, profilestore, and internal/ui/pages files outside models.go.
- Dependency check clean: go.mod/go.sum unchanged; no new require lines.
- Abstraction check: no HTTPClient/FileWriter interfaces; no interfaces in hfhub/downloadmgr; new narrow UI component interfaces exist.

## F1 compliance audit

- Actual HF file downloads currently go through `downloadmgr.Manager.download`, not `hfhub.Client.OpenDownload`; compliance checks for request headers must inspect `internal/service/downloadmgr/manager.go` too.
- `hfhub.Client` sets User-Agent for JSON API requests and its unused `OpenDownload`, but `downloadmgr.Manager.download` does not set User-Agent on raw file download requests.

## F2 quality fix — tests + dedup (2026-05-17)

### Test pattern for overlay components (no teatest needed)
- Direct state inspection beats teatest for pure logic tests:
  `p := NewHFFilePicker(...); p.Update(tea.KeyMsg{...}); if p.cursor != 1 { ... }`
- For Init() that returns a Cmd doing IO, call `cmd()` to drive it:
  `msg := p.Init()(); listMsg := msg.(HFFileListMsg)`
- Fake collaborators via tiny interface-satisfying structs (`fakeSearcher`, `fakeFileLister`, `fakeSnapshotter`).
- Use `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}` for rune keys, `tea.KeyMsg{Type: tea.KeyDown}` for named keys.

### Byte humanizer dedup
- `humanBytes` (download_progress.go) is canonical — "1.50KB" format, no space, 2 decimals.
- Removed `humanizeBytes` from hf_file_picker.go ("1.5 KB" format). Both files now share `humanBytes` (same package).
- Test `TestHumanBytes_Formatting` locks the format.

### Missing godoc spots
- `DownloadCancelMsg` and `HFFileListMsg` lacked godoc; added minimal one-liners.
- `hfFileListMsg`/`hfSearchResultMsg` aliases stay unexported so no godoc needed.

## F1 compliance rerun — 2026-05-17
- HF download URLs must be generated through `hfhub.Client.DownloadURL()` so `HF_BASE_URL` affects actual file downloads, not only API calls.
- `downloadmgr.Manager` owns actual file GET requests; User-Agent compliance belongs there via `WithUserAgent`, even when URL construction lives in `hfhub`.
- Rerun passed diagnostics on changed files, targeted tests, full `go test ./...`, focused uncached service tests, and `make build`.

## F3 re-run (2026-05-17)
- Bug 1 fix confirmed: `hfhub.Client.DownloadURL` now consumes `HF_BASE_URL` and `internal/ui/pages/models.go:584` calls it instead of the const.
- Manual QA evidence: `.sisyphus/evidence/f3-qa-rerun.md`. Captures: `f3-rerun-step{0..4}-*.txt`, plus updated `hfmock.log` (UA logged on download).
- Mock hfmock.go updated to log `User-Agent` on `/resolve/main/...` so future F3 runs can verify the header without code spelunking.

## F4 Scope Fidelity Check - Rerun 2
- Baseline `c2b18c3` to HEAD `ce9a8b4` changes only allowed files.
- Denylist path query returned `internal/ui/pages/models.go` only; this file is explicitly allowed.
- Evidence saved at `.sisyphus/evidence/f4-scope-rerun2.md`.

# F1 Compliance Rerun — models-hf-search-download

Verdict: APPROVE

Reason: previous blocking findings are fixed. Actual downloads now inherit the runtime Hugging Face base URL through `hfhub.Client.DownloadURL`, and actual download HTTP requests now set `User-Agent` through `downloadmgr.Manager.WithUserAgent`.

## Rerun Fix Verification

- PASS — `hfhub.Client.BaseURL()` exists and returns runtime base URL: `internal/service/hfhub/client.go:47-48` returns `c.baseURL`, which is initialized from `HF_BASE_URL` in `NewClient` (`client.go:35-38`).
- PASS — `hfhub.Client.DownloadURL(repoID, filename)` uses runtime `c.baseURL`, not `DefaultBaseURL`: `internal/service/hfhub/client.go:50-53` builds `c.baseURL + "/" + repoID + "/resolve/main/" + filename`.
- PASS — Models page uses `p.hfClient.DownloadURL()` for download specs: `internal/ui/pages/models.go:584` calls `p.hfClient.DownloadURL(repoID, file)` before `dlManager.Start(...)`.
- PASS — `downloadmgr.Manager` has `userAgent` field and sets request header: `internal/service/downloadmgr/manager.go:18-22` defines `userAgent string`; `manager.go:48-52` adds `WithUserAgent`; `manager.go:188-196` sets `req.Header.Set("User-Agent", m.userAgent)` before `httpClient.Do(req)`.
- PASS — TUI wiring chains `.WithUserAgent()` on download manager: `cmd/model-loader/main.go:54-56` creates `hfhub.NewClient(httpClient, "model-loader/dev")` and `downloadmgr.NewManager(httpClient, 3).WithUserAgent("model-loader/dev")`.

## Previous Must Have Items Still Hold

- PASS — Models tab still lists local GGUFs using `modelscanner.Scanner`: no change to scanner wiring; previous implementation remains in `internal/ui/pages/models.go`.
- PASS — Existing local keys `/`, `R`, `enter`, and `esc` remain in Models page key handling; diff only changes HF download URL construction.
- PASS — `IsCapturingInput()` overlay coverage remains unchanged; diff only changes HF download URL construction.
- PASS — HF search key `s`, GGUF-only toggle `g`, and download/progress cancel path `x` remain unchanged.
- PASS — Individual `.gguf` and snapshot destination rules still use `downloadmgr.ResolveDest(p.paths[0], repoID, file, isSnapshot)`; no destination logic changed.
- PASS — Name conflict skip still handles `downloadmgr.ErrAlreadyExists` with `already exists: {file}` flash; no conflict logic changed.
- PASS — Max 3 concurrent downloads and FIFO queue still hold: `main.go` still passes `3`; `downloadmgr.Manager` queue/start logic unchanged apart from User-Agent header.
- PASS — Each download remains cancelable by stored `context.CancelFunc`; cancel/start lifecycle unchanged.
- PASS — Auto-rescan on completed download and success/failure flash handling remain unchanged.
- PASS — HTTP error handling for 401/403, 404, and 429 Retry-After remains in `hfhub.Client.doJSON`/retry flow; no JSON request logic changed.
- PASS — Search debounce and stale-result epoch logic remain unchanged in HF search picker.
- PASS — Must NOT Have constraints still hold: no HF token/auth support, no dataset/Spaces endpoints, no format conversion, no persisted download queue, no disk search cache, no resume/range downloads, no multi-search-path download fanout, no external HF SDK, no new dependency, no new root global shortcut.

## Scope Verification

`GIT_MASTER=1 git status --short` shows only expected implementation files modified:

```text
 M cmd/model-loader/main.go
 M internal/service/downloadmgr/manager.go
 M internal/service/hfhub/client.go
 M internal/ui/pages/models.go
```

`GIT_MASTER=1 git diff --stat`:

```text
 cmd/model-loader/main.go                |  2 +-
 internal/service/downloadmgr/manager.go | 10 ++++++++++
 internal/service/hfhub/client.go        |  8 ++++++++
 internal/ui/pages/models.go             |  2 +-
 4 files changed, 20 insertions(+), 2 deletions(-)
```

## Verification Commands

- PASS — `lsp_diagnostics` on all changed files: no diagnostics found for `cmd/model-loader/main.go`, `internal/service/downloadmgr/manager.go`, `internal/service/hfhub/client.go`, and `internal/ui/pages/models.go`.
- PASS — Targeted tests: `go test ./internal/service/hfhub ./internal/service/downloadmgr ./internal/ui/pages`.
- PASS — Full test suite: `go test ./...`.
- PASS — Focused uncached service tests: `go test ./internal/service/hfhub ./internal/service/downloadmgr -run 'TestOpenDownload|TestStart_SingleDownload' -count=1`.
- PASS — Build: `make build` (`go build -o bin/model-loader ./cmd/model-loader`).

## Notes

No blockers found in this rerun. Approval is based on current code inspection plus successful diagnostics/tests/build above.

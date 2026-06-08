# F1 Compliance Report — models-hf-search-download

Verdict: REJECT

Reason: implementation misses one Must Have requirement: actual download requests sent by `downloadmgr.Manager` do not set `User-Agent`. HF API JSON requests in `hfhub.Client` set User-Agent, and `hfhub.Client.OpenDownload` also sets User-Agent, but the Models page starts downloads through `downloadmgr.Manager.Start` with a raw URL; `Manager.download` builds its own `http.NewRequestWithContext` and never sets `User-Agent`.

## Must Have Verification

- PASS — Models tab still lists local GGUFs via `modelscanner.Scanner`: `internal/ui/pages/models.go` imports `modelscanner`, stores `scanner modelscanner.Scanner`, `NewModelsPage(scanner, paths)`, and `startScanCmd(... scanner.Scan(ctx, paths) ...)`.
- PASS — Local filter `/`, rescan `R`, action menu `enter`, and `esc` preserved: `defaultModelsKeys()` binds `/`, `R`, `enter`, `esc`; `handleKey` handles filter, cancel, rescan, enter; `updateActionMenu` keeps Enter/Esc behavior.
- PASS — `IsCapturingInput()` covers overlays: returns true for action menu, filter, profile picker, active HF search, active HF file picker, and focused download progress.
- PASS — `s` opens HF search: `handleKey` case `msg.String() == "s"` creates `components.NewHFSearchPicker(...)` and calls `Init()`.
- PASS — `g` toggles GGUF-only in search: `internal/ui/components/hf_search_picker.go` handles case `"g"`, toggles `ggufOnly`, resets cursor, and `filterResults` filters by GGUF tag.
- PASS — `x` cancels download or closes overlay/progress path: `models.go` routes `x` to visible `DownloadProgress`; `download_progress.go` emits `DownloadCancelMsg`; `models.go` calls `dlManager.Cancel(...)`. No HF overlay-specific `x` close behavior found, but requirement wording allows cancel/close and active download cancel exists.
- PASS — Individual `.gguf` download destination uses first search path plus original basename: `startSelectedDownloads` calls `downloadmgr.ResolveDest(p.paths[0], repoID, file, false)`; `ResolveDest` sets `destDir=searchPath` and `destFile=filepath.Join(searchPath, filepath.Base(rfilename))`.
- PASS — Snapshot destination uses first search path plus `{org}__{repo}/...`: `startSelectedDownloads` calls `ResolveDest(p.paths[0], repoID, file, true)`; snapshot path replaces `/` with `__`, sets `destDir=filepath.Join(searchPath, sanitized)`, and preserves `rfilename` under that directory.
- PASS — Name conflict skips with flash: `ResolveDest` returns `ErrAlreadyExists`; `startSelectedDownloads` catches it and sets flash `already exists: {file}`, then continues.
- PASS — Max 3 simultaneous downloads with FIFO queue: `cmd/model-loader/main.go` constructs `downloadmgr.NewManager(httpClient, 3)`; `manager.go` starts immediately while `len(active) < maxConcurrent`, appends overflow IDs to `queue`, and `startNextLocked` pops `queue[0]`.
- PASS — Each download cancelable via `ctx.CancelFunc`: `Manager` stores `active map[ID]context.CancelFunc`, `startLocked` creates `context.WithCancel`, and `Cancel` invokes the cancel function.
- PASS — Auto-rescan on completion: `handleDownloadEvent` handles `StatusCompleted`, flashes success, then calls `beginRescan(false)` and batches the scan command.
- PASS — Flash on success and failure: `handleDownloadEvent` flashes `downloaded: ...` on completed and `download failed...` on failed.
- PASS — HTTP errors handled for 401/403, 404, 429 with Retry-After: `hfhub.Client.doJSON` returns `ErrHTTP` for non-2xx statuses, including 401/403 and 404, and honors one 429 `Retry-After` retry capped at 30 seconds.
- FAIL — User-Agent on all requests: `hfhub.Client.sendJSON` and `OpenDownload` set `User-Agent`, but actual downloads are started with raw URLs and executed by `downloadmgr.Manager.download`; `manager.go:181-186` builds request and calls `httpClient.Do(req)` without setting `User-Agent`.
- PASS — 300ms debounce on search: `HFSearchPicker.debounceSearch` uses `tea.Tick(300*time.Millisecond, ...)`.
- PASS — Epoch counter for stale response discard: `HFSearchPicker` increments `epoch` on query changes and drops result messages whose `Epoch != p.epoch`.

## Must NOT Have Verification

- PASS — No `HF_TOKEN` support: search in `internal/service/hfhub` found no `HF_TOKEN`, `Authorization`, or bearer-token logic. Only env usage is `HF_BASE_URL`.
- PASS — No dataset/Spaces support: `Search` sets path `/api/models`; no dataset/Spaces endpoint support found. Note: comment says "models/datasets", but code uses `/api/models` only.
- PASS — No format conversion: no safetensors-to-gguf conversion implementation in HF/download flow.
- PASS — No persistence between sessions: `downloadmgr.Manager` keeps in-memory `states`, `queue`, and `active`; no disk write state.
- PASS — No disk search cache: `hfhub.Client.Search` performs HTTP request each time; no file cache writes found.
- PASS — No resume of interrupted downloads: manager writes `{DestFile}.partial`; on copy/close/rename error it removes partial. No Range/seek/resume support found.
- PASS — No multiple SearchPaths for download: Models page download path resolution uses only `p.paths[0]`.
- PASS — No changes to `modelscanner/` in current working tree: `git diff -- internal/service/modelscanner` produced no output.
- PASS — No changes to `config/config.go` in current working tree: `git diff -- internal/config/config.go` produced no output.
- PASS — No changes to `domain/` in current working tree: `git diff -- internal/domain` produced no output.
- PASS — No changes to other pages in current working tree: `git diff -- internal/ui/pages ':!internal/ui/pages/models.go'` produced no output.
- PASS — No external SDK: HF client uses stdlib `net/http`; no Hugging Face SDK imports found.
- PASS — No new dependencies in current working tree: `git diff -- go.mod` produced no output.
- PASS — No new global shortcuts in `root.go`: search for local HF keys in `root.go` found no `s`, `g`, or `x` shortcut additions.

## Blocking Finding

`downloadmgr.Manager.download` should set a User-Agent, or downloads should be routed through `hfhub.Client.OpenDownload`/equivalent request builder. Until actual file download requests carry User-Agent, plan compliance is not complete.

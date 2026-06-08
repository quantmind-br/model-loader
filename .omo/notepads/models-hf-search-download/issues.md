
## F2 Code Quality Review — 2026-05-17

**Verdict: REJECT**

Build (`go build`), vet, and `go test -race` all pass clean across 27 packages.
Service-layer coverage healthy: `hfhub` 76.6%, `downloadmgr` 87.4%.

Rejected because the plan's "tests-after on UI overlays (teatest)" deliverable
is missing entirely:

- `components/download_progress.go` — 0%
- `components/hf_search_picker.go`  — 0%
- `components/hf_file_picker.go`    — 0%
- `pages/models.go` new handlers (`handleDownloadEvent`, `openHFFilePicker`,
  `startSelectedDownloads`, `WithHFClient`, `WithDownloadManager`) — 0%

`models_test.go` has zero references to `hfClient`, `dlManager`, `HFSearch*`,
or `Download*`.

Non-blocking findings:
- Missing godoc: `DefaultBaseURL`, `ErrManagerClosed`, `HFFileListMsg`,
  `DownloadCancelMsg`, individual `Status*` constants.
- `Manager.download` returns pass-through errors without `%w` wrapping
  (works via stdlib but loses stage context in logs).
- Plain-string error on non-2xx status (no sentinel/`errors.As` support).
- Duplicate byte-humanizer: `humanBytes` (download_progress) vs
  `humanizeBytes` (hf_file_picker) — same algo, cosmetic format diff.
- `downloadmgr.NewID` uses RFC3339Nano wall clock — collision risk under
  same-nanosecond bursts (likely in tests, unlikely in prod).

Full report: `.sisyphus/evidence/f2-quality-report.md`

## F3 QA Findings (2026-05-17)

### BLOCKER: Download URL ignores `HF_BASE_URL`
- **File:** `internal/ui/pages/models.go:584`
- **Code:** `url := fmt.Sprintf("%s/%s/resolve/main/%s", hfhub.DefaultBaseURL, repoID, file)`
- **Problem:** uses const `hfhub.DefaultBaseURL` directly, bypassing the env-var override that `hfhub.NewClient` honors for search/repo-info. Breaks the mock contract documented in task 1 of the plan and prevents corporate-mirror users from downloading.
- **Repro:** export `HF_BASE_URL=http://localhost:9999` + mock; flow search → enter → space → enter; downloader requests `https://huggingface.co/.../resolve/main/...` → 401 from real HF.
- **Suggested fix:** add `(c *Client) BaseURL() string` on `*hfhub.Client` (or `DownloadURL(repoID, file)`); call it from `startSelectedDownloads` instead of the const.

### MINOR: tmux send-keys without `-l` swallowed by bubbletea picker
- `tmux send-keys -t hfqa "test"` did not deliver runes; `tmux send-keys -l -t hfqa "t"` one-rune-at-a-time works.
- Affects future F3 / dev scripting only — not a model-loader defect.

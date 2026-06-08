# F4 Scope Fidelity Check — models-hf-search-download

Verdict: REJECT

## Diff summary (`git diff --stat HEAD~16`)

```text
 01-PRD_tui_navigation_help.md                   | 142 ----------
 02-PRD_tui_layout_primitives.md                 | 191 -------------
 03-PRD_tui_feedback_states.md                   | 193 -------------
 AGENTS.md                                       |   2 +-
 cmd/model-loader/main.go                        |  13 +-
 internal/domain/profile.go                      |  11 +
 internal/domain/profile_test.go                 |  63 +++++
 internal/service/downloadmgr/manager.go         | 341 +++++++++++++++++++++++
 internal/service/downloadmgr/manager_test.go    | 286 +++++++++++++++++++
 internal/service/downloadmgr/pathing.go         |  69 +++++
 internal/service/downloadmgr/pathing_test.go    | 117 ++++++++
 internal/service/downloadmgr/types.go           |  53 ++++
 internal/service/hfhub/client.go                | 261 ++++++++++++++++++
 internal/service/hfhub/client_test.go           | 318 +++++++++++++++++++++
 internal/service/hfhub/types.go                 |  48 ++++
 internal/service/processmgr/manager.go          |  40 +++
 internal/service/processmgr/manager_test.go     |  66 +++++
 internal/service/profilestore/fs_store_test.go  |  25 ++
 internal/ui/components/download_progress.go     | 174 ++++++++++++
 internal/ui/components/hf_file_picker.go        | 218 +++++++++++++++
 internal/ui/components/hf_search_picker.go      | 264 ++++++++++++++++++
 internal/ui/pages/models.go                     | 351 +++++++++++++++++++++++-
 internal/ui/pages/profile_editor/draft.go       |  40 ++-
 internal/ui/pages/profile_editor/editor.go      | 195 ++++++++++++-
 internal/ui/pages/profile_editor/editor_test.go | 178 ++++++++++++
 internal/ui/pages/profiles.go                   |   1 +
 26 files changed, 3116 insertions(+), 544 deletions(-)
```

## Allowlist result

Allowed paths touched:

- `cmd/model-loader/main.go`
- `internal/service/downloadmgr/`
- `internal/service/hfhub/`
- `internal/ui/components/`
- `internal/ui/pages/models.go`

Forbidden/out-of-scope paths touched:

- `01-PRD_tui_navigation_help.md`
- `02-PRD_tui_layout_primitives.md`
- `03-PRD_tui_feedback_states.md`
- `AGENTS.md`
- `internal/domain/profile.go`
- `internal/domain/profile_test.go`
- `internal/service/processmgr/manager.go`
- `internal/service/processmgr/manager_test.go`
- `internal/service/profilestore/fs_store_test.go`
- `internal/ui/pages/profile_editor/draft.go`
- `internal/ui/pages/profile_editor/editor.go`
- `internal/ui/pages/profile_editor/editor_test.go`
- `internal/ui/pages/profiles.go`

## Required zero-change packages

Pass:

- `internal/service/modelscanner/` has zero changes.
- `internal/config/` has zero changes.
- `internal/ui/root.go` has zero changes.
- `go.mod` has zero changes.
- `go.sum` has zero changes.

Fail:

- `internal/domain/` changed (`profile.go`, `profile_test.go`).
- `internal/ui/pages/` changed outside `models.go` (`profile_editor/draft.go`, `profile_editor/editor.go`, `profile_editor/editor_test.go`, `profiles.go`).

Additional out-of-scope package changes:

- `internal/service/processmgr/` changed.
- `internal/service/profilestore/` changed.

## Dependency check

`git diff HEAD~16 -- go.mod` produced no output. No new `require` lines found.

## Interface / abstraction check

- No `HTTPClient` or `FileWriter` interfaces found.
- No interfaces found under `internal/service/hfhub/` or `internal/service/downloadmgr/`.
- New UI component-local interfaces found:
  - `DownloadStateSnapshotter` in `internal/ui/components/download_progress.go`
  - `HFFileLister` in `internal/ui/components/hf_file_picker.go`
  - `HFSearcher` in `internal/ui/components/hf_search_picker.go`
- Existing unrelated interfaces remain in `internal/ui/root.go`, `internal/ui/pages/server.go`, `internal/ui/pages/backends.go`, `internal/ui/pages/monitor.go`, and `internal/ui/components/picker.go`.

These new UI interfaces are narrow consumer-side seams, not generic transport or file-system abstractions. Main rejection reason is file/package scope creep, not dependency or generic abstraction creep.

## Final verdict

REJECT. Changes include multiple files outside approved scope and modify packages explicitly required to have zero changes.

# F4 Scope Fidelity Check — Rerun

Verdict: REJECT

Command: `git diff --name-only HEAD~20`

## Changed files

```text
AGENTS.md
ARCHITECTURE.md
BUG_REPORT.md
cmd/model-loader/main.go
internal/service/downloadmgr/manager.go
internal/service/downloadmgr/manager_test.go
internal/service/downloadmgr/pathing.go
internal/service/downloadmgr/pathing_test.go
internal/service/downloadmgr/types.go
internal/service/hfhub/client.go
internal/service/hfhub/client_test.go
internal/service/hfhub/types.go
internal/ui/components/confirm.go
internal/ui/components/confirm_test.go
internal/ui/components/download_progress.go
internal/ui/components/download_progress_test.go
internal/ui/components/help.go
internal/ui/components/help_test.go
internal/ui/components/hf_file_picker.go
internal/ui/components/hf_file_picker_test.go
internal/ui/components/hf_search_picker.go
internal/ui/components/hf_search_picker_test.go
internal/ui/components/modal.go
internal/ui/components/modal_test.go
internal/ui/pages/backends.go
internal/ui/pages/backends_test.go
internal/ui/pages/launcher.go
internal/ui/pages/launcher_test.go
internal/ui/pages/models.go
internal/ui/pages/models_test.go
internal/ui/pages/monitor.go
internal/ui/pages/monitor_test.go
internal/ui/pages/profile_editor/draft.go
internal/ui/pages/profile_editor/editor.go
internal/ui/pages/profile_editor/editor_test.go
internal/ui/pages/profiles.go
internal/ui/pages/profiles_test.go
internal/ui/pages/server.go
internal/ui/pages/server_test.go
internal/ui/root.go
internal/ui/root_test.go
internal/ui/theme/layout.go
internal/ui/theme/layout_test.go
internal/ui/theme/theme.go
```

## Outside allowlist changes found

```text
AGENTS.md
ARCHITECTURE.md
BUG_REPORT.md
internal/ui/pages/backends.go
internal/ui/pages/backends_test.go
internal/ui/pages/launcher.go
internal/ui/pages/launcher_test.go
internal/ui/pages/models_test.go
internal/ui/pages/monitor.go
internal/ui/pages/monitor_test.go
internal/ui/pages/profile_editor/draft.go
internal/ui/pages/profile_editor/editor.go
internal/ui/pages/profile_editor/editor_test.go
internal/ui/pages/profiles.go
internal/ui/pages/profiles_test.go
internal/ui/pages/server.go
internal/ui/pages/server_test.go
internal/ui/root.go
internal/ui/root_test.go
internal/ui/theme/layout.go
internal/ui/theme/layout_test.go
internal/ui/theme/theme.go
```

## Explicit forbidden-path changes found

```text
AGENTS.md
internal/ui/pages/backends.go
internal/ui/pages/backends_test.go
internal/ui/pages/launcher.go
internal/ui/pages/launcher_test.go
internal/ui/pages/models_test.go
internal/ui/pages/monitor.go
internal/ui/pages/monitor_test.go
internal/ui/pages/profile_editor/draft.go
internal/ui/pages/profile_editor/editor.go
internal/ui/pages/profile_editor/editor_test.go
internal/ui/pages/profiles.go
internal/ui/pages/profiles_test.go
internal/ui/pages/server.go
internal/ui/pages/server_test.go
```

No changed files were found under:

```text
internal/domain/
internal/service/processmgr/
internal/service/profilestore/
```

## go.mod dependency check

Command: `git diff --stat HEAD~20 -- go.mod`

Result: no output; `go.mod` has no diff, so no new dependencies detected there.

## Reason

Scope allowlist permits only:

- `internal/service/hfhub/*`
- `internal/service/downloadmgr/*`
- `internal/ui/components/*`
- `internal/ui/pages/models.go`
- `cmd/model-loader/main.go`
- evidence files in `.sisyphus/evidence/`

Current diff still includes files outside allowlist, including `AGENTS.md`, docs at repo root, `internal/ui/root.go`, `internal/ui/theme/*`, and multiple `internal/ui/pages/` files other than `models.go`, plus `internal/ui/pages/profile_editor/*`.

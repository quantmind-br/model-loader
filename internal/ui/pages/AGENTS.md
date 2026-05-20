# internal/ui/pages

## OVERVIEW
Tab page implementations. Each page is a `tea.Model` with isolated state and lifecycle.

## WHERE TO LOOK
| File | Purpose |
|------|---------|
| `profiles.go` | Profile CRUD master list |
| `profiles_list.go` | Profile list view delegate |
| `profiles_crud.go` | Profile create/update/delete orchestration (extracted from profiles.go) |
| `profile_editor/` | Inline huh form for profile creation/editing (sub-package) |
| `models.go` | GGUF model browser and scanner integration |
| `launcher.go` | Profile selection, validation, launch orchestration |
| `monitor.go` | Live GPU metrics, logs, slots per running instance |
| `server.go` | HTTP proxy status and on/off toggle |
| `backends.go` | Backend catalog management (add/edit/delete/probe) |
| `messages.go` | Cross-tab messages consumed by `root.go` |
| `placeholder.go` | Generic stub for unimplemented tabs |

## CONVENTIONS
- Pages receive service dependencies via constructors, never globals
- `huh` forms for all interactive input
- `bubbles/list` and `bubbles/table` for collections
- Test files are side-by-side (`*_test.go`) with table-driven tests
- Heap-allocated form state (`*profileDraft`) required so `&field` bindings survive bubbletea's value-receiver copies across Updates

## ANTI-PATTERNS
- DO NOT import `internal/ui/root` from pages (pages are leaf nodes)
- DO NOT handle global keybinds here (`root.go` owns tab switching, quit, etc.)

## NOTES
- `SwitchToMonitorMsg`, `MonitorSelectPIDMsg`, and `LaunchProfileMsg` are cross-tab coordination mechanisms
- `Placeholder` can be reused for new tabs before full implementation

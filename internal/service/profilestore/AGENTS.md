# internal/service/profilestore

## OVERVIEW
Profile persistence as one JSON file per profile. Atomic writes, corruption diagnostics, and reserved-arg stripping (manager-owned launch params like `port` never persist).

## WHERE TO LOOK
| File | Purpose |
|------|---------|
| `store.go` | `Store` interface + `ListDiagnostic` + sentinel errors |
| `fs_store.go` | `FSStore` implementation: atomic write, list, get, save, delete, duplicate, `MarkLastUsed` |
| `export.go` | `ExportBundle`, `ExportAll`, `ExportFilename` |
| `import.go` | `ConflictMode`, `ImportResult`, `ImportBundle` |
| `history.go` | `SavePrevious`/`LoadPrevious`/`DeletePrevious` undo snapshots |
| `migration.go` | `MigrateProfile` schema v1→v2→v3 upgrade |

## CONVENTIONS
- One profile = one `<id>.json` under the configured profiles dir (default `~/.config/model-loader/profiles/`)
- `Save` uses temp-file + rename for atomicity
- `ListWithDiagnostics` returns valid profiles + corrupt entries separately; never aborts on single-file errors
- `reservedArgs` (`port`) are stripped from `Args` on both `Get` and `Save` — the process manager assigns ports at launch; `Get` persists the stripped file back (transparent migration)
- Profiles sorted by name ascending in `List`

## ANTI-PATTERNS
- Do not edit profile JSON files by hand — use `Save()` to maintain timestamps and schema version
- Do not store a `port` arg in a profile — it is reserved and silently stripped on read/write
- Do not leave `ListWithDiagnostics` results unused — UI shows ⚠ for corrupt profiles and excludes them from launch

## NOTES
- `SchemaVersion` and timestamps auto-filled on `Save` if empty
- `MarkLastUsed` updates `Meta.LastUsedAt` without failing if profile is missing
- Numeric values in `Args` are `float64` (JSON number)

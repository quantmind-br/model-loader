# internal/service/profilestore

## OVERVIEW
Profile persistence as one JSON file per profile. Atomic writes, corruption diagnostics, and port deconfliction on duplicate.

## WHERE TO LOOK
| File | Purpose |
|------|---------|
| `store.go` | `Store` interface + `ListDiagnostic` + sentinel errors |
| `fs_store.go` | `FSStore` implementation: atomic write, list, get, save, delete, duplicate |

## CONVENTIONS
- One profile = one `<id>.json` under `~/.local/share/model-loader/profiles/`
- `Save` uses temp-file + rename for atomicity
- `ListWithDiagnostics` returns valid profiles + corrupt entries separately; never aborts on single-file errors
- `Duplicate` auto-increments port to avoid conflicts (scans `usedPorts()`)
- Profiles sorted by name ascending in `List`

## ANTI-PATTERNS
- Do not edit profile JSON files by hand — use `Save()` to maintain timestamps and schema version
- Do not rely on `usedPorts()` as a guarantee — it degrades gracefully on I/O errors
- Do not leave `ListWithDiagnostics` results unused — UI shows ⚠ for corrupt profiles and excludes them from launch

## NOTES
- `SchemaVersion` and timestamps auto-filled on `Save` if empty
- `MarkLastUsed` updates `Meta.LastUsedAt` without failing if profile is missing
- Port values in `Args` are `float64` (JSON number) — `portAsInt` normalizes

# internal/domain

## OVERVIEW
Zero-dependency shared types. Profile, Backend, Instance, Model, and FlagSchema definitions used by all service and UI layers.

## WHERE TO LOOK
| File | Purpose |
|------|---------|
| `profile.go` | `Profile`, `LaunchConfig`, `ProfileMeta`, `Slugify` |
| `backend.go` | `Backend`, `BackendCatalog`, `BackendKind`, `BackendMeta` |
| `flag_schema.go` | `FlagSchema`, `FlagSpec`, `FlagType`, `Lookup` |
| `instance.go` | `Instance` (running process state) |
| `model.go` | `ModelFile` (GGUF metadata) |
| `backend_schema.go` | `BackendValidationSchema`, `SchemaSource`, `FlagSchemaToBackend` |

## CONVENTIONS
- `SchemaVersion = 1` for Profile JSON
- `Slugify` produces ASCII kebab-case safe for filenames
- `FlagSchema.Lookup` resolves map key, long name, short name, and aliases
- `LaunchConfig.ResolvedExecutable` is set at launch time (`json:"-"` — not persisted)
- `BackendKind` is a string enum: `llama-server`, `vllm`, `tabbyapi`, `sglang`

## ANTI-PATTERNS
- Do not add external imports to this package — it must remain zero-dependency
- Do not change `SchemaVersion` without a migration plan
- Do not modify `Slugify` regex without checking filename safety implications

## NOTES
- `FlagType` enum: `Bool=0, Int=1, Float=2, String=3, Enum=4`
- `BackendValidationSchema` wraps `FlagSchema` with backend ID/kind and source metadata
- `FlagSchemaToBackend` converts a parsed `FlagSchema` to `BackendValidationSchema` (used by `backendschema` generator)
